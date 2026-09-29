package sessel_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/sessel"
)

func eval(t *testing.T, env *sessel.Env, src string) (sessel.Value, error) {
	t.Helper()
	if env == nil {
		env = &sessel.Env{}
	}
	return sessel.Eval(context.Background(), src, env)
}

func TestErrorKinds(t *testing.T) {
	for _, tc := range []struct {
		src    string
		parse  bool
		typ    string
		reason sessel.Reason
		thrown bool
	}{
		{src: "(((", parse: true},
		{src: "let x = 1; x = 2", parse: true},
		{src: "1 / 0", typ: sessel.RuntimeErrorType},
		{src: `"a" + 1`, typ: sessel.TypeErrorType},
		{src: "someVar", typ: sessel.RuntimeErrorType, reason: sessel.ReasonUnresolved},
		{src: "self", typ: sessel.RuntimeErrorType, reason: sessel.ReasonSelfUnbound},
		{src: `throw "x"`, typ: sessel.UserErrorType, reason: sessel.ReasonThrow, thrown: true},
		{src: `@schema Pagelove url("https://pagelove.org/1.0"); Pagelove.DELETE("/a.html")`, typ: sessel.RuntimeErrorType, reason: sessel.ReasonNoWriter},
	} {
		_, err := eval(t, nil, tc.src)
		if err == nil {
			t.Errorf("%s: no error", tc.src)
			continue
		}
		if tc.parse {
			if !sessel.IsParseError(err) {
				t.Errorf("%s: want a parse error, got %v", tc.src, err)
			}
			continue
		}
		e, ok := sessel.AsError(err)
		if !ok || e.Type != tc.typ || e.Reason != tc.reason || e.Thrown != tc.thrown {
			t.Errorf("%s: got %#v", tc.src, err)
		}
	}
	if _, err := eval(t, nil, strings.Repeat("(", 5000)+"1"+strings.Repeat(")", 5000)); !sessel.IsParseError(err) {
		t.Errorf("deep nesting: want a parse error, got %v", err)
	}
}

func TestThrownHTTPResponse(t *testing.T) {
	_, err := eval(t, nil, `
		@schema HTTPResponse url("https://pagelove.org/HTTPResponse");
		@schema Pair url("https://pagelove.org/Pair");
		throw new HTTPResponse {
			status: 401,
			body: "use => wisely",
			header: new Pair { key: "WWW-Authenticate", value: "Basic realm=\"x\"" },
			header: new Pair { key: "X-Two", value: "2" }
		}`)
	r, ok := sessel.ResponseOf(err)
	if !ok {
		t.Fatalf("not an HTTPResponse: %v", err)
	}
	if r.Status != 401 || len(r.Headers) != 2 || r.Headers[0] != [2]string{"WWW-Authenticate", `Basic realm="x"`} {
		t.Errorf("response = %+v", r)
	}
	if got := r.HTMLBody(); got != "use =&gt; wisely" {
		t.Errorf("HTMLBody = %q", got)
	}
	if _, ok := sessel.ResponseOf(nil); ok {
		t.Error("nil error is not a response")
	}
}

func TestBudget(t *testing.T) {
	env := &sessel.Env{Budget: &sessel.Budget{MaxOps: 60}}
	_, err := eval(t, env, `[1, 2, 3, 4, 5, 6, 7, 8, 9, 10].map(x => x * 2).map(x => x + 1).map(x => x - 1)`)
	e, ok := sessel.AsError(err)
	if !ok || e.Reason != sessel.ReasonBudget {
		t.Fatalf("want budget exhaustion, got %v", err)
	}
	// the budget stays exhausted: a catch handler that computes re-raises
	_, err = eval(t, env, `try { 1 } catch (e) { 2 }`)
	if e, ok := sessel.AsError(err); !ok || e.Reason != sessel.ReasonBudget {
		t.Fatalf("exhausted budget must stay exhausted, got %v", err)
	}
	_, err = eval(t, nil, `let f = n => f(n + 1); f(0)`)
	if e, ok := sessel.AsError(err); !ok || e.Reason != sessel.ReasonDepth {
		t.Fatalf("want depth limit, got %v", err)
	}
}

func TestContextResponse(t *testing.T) {
	req := sessel.NewRequest("GET", "/a.html", http.Header{"X-Probe": {"1"}}, nil, nil, nil, nil)
	ctx := sessel.NewContext(req)
	sessel.SetResponse(ctx, 200, "<p>ok</p>", http.Header{"Content-Type": {"text/html"}})
	_, err := eval(t, &sessel.Env{Context: ctx}, `
		if (Context.request.headers["x-probe"] == "1" && Context.response.body.contains("ok")) {
			Context.response.status = 299;
			Context.response.body = "changed"
		}`)
	if err != nil {
		t.Fatal(err)
	}
	st, body, ok := sessel.ResponseFields(ctx)
	if !ok || st != 299 || body != "changed" {
		t.Errorf("response = %d %q %v", st, body, ok)
	}
}

type recordingWriter struct {
	puts    map[string]string
	deletes []sessel.Value
}

func (w *recordingWriter) Put(ctx context.Context, item *sessel.Element, path string) error {
	w.puts[path] = dom.OuterHTML(item.Node)
	return nil
}

func (w *recordingWriter) Delete(ctx context.Context, target sessel.Value) error {
	w.deletes = append(w.deletes, target)
	return nil
}

func TestPlatformWrites(t *testing.T) {
	h := sessel.NewMemHost()
	w := &recordingWriter{puts: map[string]string{}}
	h.W = w
	if _, err := h.AddHTML("/notes/index.html", `<!DOCTYPE html><html><body><h1>Notes</h1></body></html>`); err != nil {
		t.Fatal(err)
	}
	doc, _ := h.AddHTML("/notes/b.html", `<html><body><p>b</p></body></html>`)
	v, err := eval(t, &sessel.Env{Host: h, Self: doc.Element(), HasSelf: true}, `
		@schema Pagelove url("https://pagelove.org/1.0");
		@schema Note url("https://example.com/Note");
		["a", "c"].each(n => Pagelove.PUT(new Note { title: n }, "/notes/" + n + ".html"));
		Pagelove.DELETE("index.html");
		Pagelove.GET("/notes/index.html").metadata()["mimetype"]`)
	if err != nil {
		t.Fatal(err)
	}
	if v != "text/html" {
		t.Errorf("GET metadata = %v", v)
	}
	if got := w.puts["/notes/a.html"]; got != `<div itemscope itemtype="https://example.com/Note"><meta itemprop="title" content="a"></div>` {
		t.Errorf("PUT a = %s", got)
	}
	if len(w.puts) != 2 || len(w.deletes) != 1 || w.deletes[0] != "/notes/index.html" {
		t.Errorf("writes = %v %v", w.puts, w.deletes)
	}
}

func TestMutableWorkingCopies(t *testing.T) {
	h := sessel.NewMemHost()
	doc, _ := h.AddHTML("/u.html", `<html><body><span itemprop="email"> Alice@Example.COM </span></body></html>`)
	els, err := eval(t, &sessel.Env{Host: h}, `${[itemprop="email"]} from "/u.html"`)
	if err != nil {
		t.Fatal(err)
	}
	env := &sessel.Env{Host: h, Self: els, HasSelf: true, Mutable: true}
	out, err := eval(t, env, `self.map((el) => el.set_text(el.text().trim().lowercase()))`)
	if err != nil {
		t.Fatal(err)
	}
	got := out.(sessel.List)[0].(*sessel.Element)
	if dom.OuterHTML(got.Node) != `<span itemprop="email">alice@example.com</span>` {
		t.Errorf("working copy = %s", dom.OuterHTML(got.Node))
	}
	if !strings.Contains(string(dom.Render(doc.Root)), "Alice@Example.COM") {
		t.Error("the stored snapshot was modified")
	}
	// queried elements stay immutable elsewhere
	if _, err := eval(t, &sessel.Env{Host: h}, `(${span} from "/u.html").first().text("x")`); err == nil {
		t.Error("mutating a queried element must fail")
	}
}

func TestEncodeJSON(t *testing.T) {
	h := sessel.NewMemHost()
	h.AddHTML("/d.html", `<html><body><h1 title="a&quot;b">x &lt; y</h1></body></html>`)
	v, err := eval(t, &sessel.Env{Host: h}, `
		@schema Foo url("https://example.com/Foo");
		@schema Selector url("https://pagelove.org/Selector");
		{ f: 100.0, i: 3, s: "<&>", e: (${h1} from "/d.html").first(), c: new Foo { n: 1 },
		  d: Temporal.PlainDate.from("2026-03-23"), sel: new Selector { "li" }, n: null, l: [true] }`)
	if err != nil {
		t.Fatal(err)
	}
	b, err := sessel.EncodeJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"f":100.0,"i":3,"s":"<&>","e":{"$type":"element","$html":"<h1 title=\"a&quot;b\">x &lt; y</h1>","$source":"/d.html"},` +
		`"c":{"$type":"element","$html":"<div itemscope itemtype=\"https://example.com/Foo\"><meta itemprop=\"n\" content=\"1\"></div>"},` +
		`"d":"2026-03-23","sel":"li","n":null,"l":[true]}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
	if _, err := sessel.EncodeJSON(sessel.NativeFunc("f", 0, nil)); err == nil {
		t.Error("lambdas are not encodable")
	}
}

func TestGoConversion(t *testing.T) {
	v := sessel.FromGo(map[string]any{"b": []any{1, 2.5, "x", nil, true}, "a": map[string]string{"k": "v"}})
	d := v.(*sessel.Dict)
	if strings.Join(d.Keys(), ",") != "a,b" {
		t.Errorf("keys = %v", d.Keys())
	}
	b, _ := sessel.EncodeJSON(v)
	if string(b) != `{"a":{"k":"v"},"b":[1,2.5,"x",null,true]}` {
		t.Errorf("FromGo = %s", b)
	}
	g := sessel.ToGo(v).(map[string]any)
	if g["b"].([]any)[0] != int64(1) || g["a"].(map[string]any)["k"] != "v" {
		t.Errorf("ToGo = %#v", g)
	}
}

func TestCompileCache(t *testing.T) {
	src := `1 + 2 * 3`
	p1, err := sessel.Compile(src)
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := sessel.Compile(src)
	v1, _ := p1.Eval(context.Background(), nil)
	v2, _ := p2.Eval(context.Background(), nil)
	if v1 != int64(7) || v2 != int64(7) {
		t.Errorf("got %v %v", v1, v2)
	}
	if d := func() [][3]string {
		p, _ := sessel.Compile(`@schema A url("https://e.com/A") @namespace s url('http://www.w3.org/2000/svg'); A`)
		return p.Declarations()
	}(); len(d) != 2 || d[1] != [3]string{"namespace", "s", "http://www.w3.org/2000/svg"} {
		t.Errorf("declarations = %v", d)
	}
}

func TestSelectorNamespacesAndIsa(t *testing.T) {
	h := sessel.NewMemHost()
	h.AddHTML("/s.html", `<html><body>
		<div itemscope itemtype="https://pagelove.org/Schema"><meta itemprop="type" content="https://e.com/B"><meta itemprop="parent" content="https://e.com/A"></div>
		<div itemscope itemtype="https://e.com/B" id="b"></div><div itemscope itemtype="https://e.com/A" id="a"></div>
		<svg><circle r="1"></circle></svg></body></html>`)
	h.Classes = sessel.MicrodataClasses(h.All())
	v, err := eval(t, &sessel.Env{Host: h}, `
		@namespace svg url("http://www.w3.org/2000/svg");
		[${:isa("https://e.com/A")}.map(e => e.attr("id")), ${svg|circle}.count(), ${circle}.first().attr("r")]`)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := sessel.EncodeJSON(v)
	if string(b) != `[["b","a"],1,"1"]` {
		t.Errorf("got %s", b)
	}
	if _, err := eval(t, &sessel.Env{Host: h}, `${nope|circle}`); err == nil {
		t.Error("an undeclared namespace prefix must fail")
	}
}
