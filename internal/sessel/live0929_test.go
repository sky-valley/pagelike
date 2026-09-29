package sessel_test

import (
	"testing"

	"github.com/sky-valley/pagelike/internal/sessel"
)

// Behaviours adopted from the live PageLove probes of 2026-09-29
// (docs/compat/decisions-2026-09-29/sessel.md).

func encode(t *testing.T, v sessel.Value) string {
	t.Helper()
	b, err := sessel.EncodeJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDocumentOnly(t *testing.T) {
	h := sessel.NewMemHost()
	self, err := h.AddHTML("/q/doc.html", `<html lang="en"><body><h1>A</h1><section><li>a</li><li>b</li></section><div id="w" itemscope itemtype="https://e.com/Product"><span itemprop="name">W</span></div></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.AddHTML("/q/other.html", `<html><body><h1>B</h1></body></html>`); err != nil {
		t.Fatal(err)
	}
	env := func(only bool) *sessel.Env {
		return &sessel.Env{Host: h, Self: self.Element(), HasSelf: true, DocumentOnly: only}
	}
	for _, tc := range []struct{ src, only, site string }{
		{`${h1}.map(e => e.text())`, `["A"]`, `["A","B"]`},
		{`[(${h1} from "/q/other.html").count(), (${h1} from "/q/*").count(), (${h1} from "other.html").count(), (${h1} from self).count()]`, `[0,0,0,1]`, `[1,2,1,1]`},
		{`[(${h1} from self).first().path(), self.path(), (${h1} from self).first().document() == null]`, `[null,null,true]`, `["/q/doc.html","/q/doc.html",false]`},
		{`let s = (${section} from self).first(); [(${li} from s).count(), (${li} from [s]).count(), s.${ li }.count(), (${h1} from document).count()]`, `[0,0,2,0]`, `[2,2,2,1]`},
		{`[from "/q/other.html" { ${h1}.count() }, from self { ${h1}.count() }, (${h1} from self, "/q/other.html").count()]`, `[0,1,1]`, `[1,1,2]`},
		{`(${#w} from self).first().microdata()["@id"]`, `null`, `"/q/doc.html#w"`},
		{`@schema Selector url("https://pagelove.org/Selector"); let s = new Selector { "h1" }; [s.execute().count(), s.execute("/q/other.html").count()]`, `[1,0]`, `[1,1]`},
	} {
		for _, only := range []bool{true, false} {
			v, err := eval(t, env(only), tc.src)
			if err != nil {
				t.Errorf("%s (DocumentOnly=%v): %v", tc.src, only, err)
				continue
			}
			want := tc.site
			if only {
				want = tc.only
			}
			if got := encode(t, v); got != want {
				t.Errorf("%s (DocumentOnly=%v): got %s, want %s", tc.src, only, got, want)
			}
		}
	}
	_, err = eval(t, env(true), `@schema Pagelove url("https://pagelove.org/1.0"); Pagelove.GET("/q/other.html")`)
	if e, ok := sessel.AsError(err); !ok || e.Type != sessel.TypeErrorType || e.Message != "unknown function: GET" {
		t.Errorf("Pagelove.GET in a DocumentOnly program: %v", err)
	}
}

func TestFloatText(t *testing.T) {
	for _, tc := range []struct {
		f          float64
		text, json string
	}{
		{42, "42", "42.0"},
		{2.5, "2.5", "2.5"},
		{0.30000000000000004, "0.30000000000000004", "0.30000000000000004"},
		{1e16, "10000000000000000", "1e+16"},
		{1.5e-7, "0.00000015", "1.5e-7"},
		{1e-6, "0.000001", "1e-6"},
		{1e15, "1000000000000000", "1000000000000000.0"},
	} {
		if got := sessel.TextOf(tc.f); got != tc.text {
			t.Errorf("TextOf(%v) = %q, want %q", tc.f, got, tc.text)
		}
		if got := sessel.FormatFloat(tc.f); got != tc.json {
			t.Errorf("FormatFloat(%v) = %q, want %q", tc.f, got, tc.json)
		}
	}
}

func TestStoredAndConstructedElements(t *testing.T) {
	h := sessel.NewMemHost()
	self, err := h.AddHTML("/d.html", `<html><body><h2 id="ws">  a
  b </h2><p id="sp"> </p><meta id="m" itemprop="m" content="c"></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	env := &sessel.Env{Host: h, Self: self.Element(), HasSelf: true}
	for src, want := range map[string]string{
		`[(${#ws} from self).first().text(), (${#ws} from self).first().value(), (${#sp} from self).first().text(), (${#sp} from self).first().value(), (${#m} from self).first().text(), (new p { text: " x " }).text(), (new p {}).text()]`: `["a b","a b","",null,""," x ",""]`,
		`let el = new ul { new li { text: "c" } }; new li { text: "b" }.insertBefore(el.children().last()); el.getHTML()`:                                                                                                                     `"<li>b</li><li>c</li>"`,
		`let el = new div { new p { text: "x" } }; [el.${ p }.first().remove(), el.getHTML()]`:                                                                                                                                                `[null,"<p>x</p>"]`,
		`let el = new ul { new li {} }; try { new li {}.insertBefore(el.${ li }.last()) } catch (e) { e.message }`:                                                                                                                            `"type error: insertBefore() reference must be a mutable child element, not a stored element"`,
		`let el = new div { new p {} }; try { el.${ p }.first().text("y") } catch (e) { e.message }`:                                                                                                                                          `"type error: text(value) setter requires a constructed element, got element"`,
		`let el = new div { new p {} }; try { el.${ p }.first().replaceWith(new b {}) } catch (e) { e.message }`:                                                                                                                              `"type error: replaceWith() cannot be used on stored (immutable) elements"`,
		`let d = Temporal.PlainDate.from("2026-03-23"); [d.dayOfWeek(), d.year(), d.dayOfWeek, Temporal.Duration.from("P1Y2M").months()]`:                                                                                                     `[1,2026,1,2]`,
		`[try { "a" + 1 } catch (e) { e.message }, try { 1 * "5" } catch (e) { e.message }, try { [1, "b"] + 1 } catch (e) { e.message }]`:                                                                                                    `["type error: cannot use 'a' in arithmetic","type error: cannot use '5' in arithmetic","type error: cannot use '1, b' in arithmetic"]`,
	} {
		v, err := eval(t, env, src)
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		if got := encode(t, v); got != want {
			t.Errorf("%s:\n got %s\nwant %s", src, got, want)
		}
	}
	v, err := eval(t, nil, `new Selector { "h1" }`)
	if el, ok := v.(*sessel.Element); err != nil || !ok || el.Node.Data != "selector" {
		t.Errorf("new Selector without @schema: %v %v", v, err)
	}
}
