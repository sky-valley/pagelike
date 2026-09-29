package dom_test

import (
	"strings"
	"testing"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/htmlser"
)

// TestStoredFormMatchesLive parses each probe body with PageLove's model and
// serializes it in PageLove's form; the result must equal what live
// PageLove stored and echoed (LO-15), and be a fixed point.
func TestStoredFormMatchesLive(t *testing.T) {
	for _, c := range liveStoredForms {
		doc, _ := dom.BuildTree(c.src)
		got := htmlser.String(doc, htmlser.Options{PageLove: true})
		want := c.stored
		if fix, ok := attrLtGt[c.name]; ok {
			// The one deliberate difference: < and > are escaped in
			// attribute values (htmlser.Options.PageLove).
			want = strings.ReplaceAll(want, fix[0], fix[1])
		}
		if got != want {
			t.Errorf("%s:\n src   %q\n got   %q\n want  %q", c.name, c.src, got, want)
			continue
		}
		again, _ := dom.BuildTree(got)
		if s := htmlser.String(again, htmlser.Options{PageLove: true}); s != got {
			t.Errorf("%s: not a fixed point: %q -> %q", c.name, got, s)
		}
	}
}

// attrLtGt maps the live stored forms whose attribute values hold < or > to
// the replacement pagelike's escaping makes.
var attrLtGt = map[string][2]string{
	"attributes":       {`title="<&amp;>"`, `title="&lt;&amp;&gt;"`},
	"put attr-escapes": {"title=\"a\u00a0b<c>d&amp;e\"", "title=\"a\u00a0b&lt;c&gt;d&amp;e\""},
}

// TestNoscriptAttributeCannotBreakOut: a stored noscript whose content
// carries "</noscript>" in an attribute value does not become live markup
// when a browser parses it as raw text.
func TestNoscriptAttributeCannotBreakOut(t *testing.T) {
	doc, _ := dom.BuildTree(`<noscript><img alt="</noscript><img src=x onerror=alert(1)>"></noscript>`)
	got := htmlser.String(doc, htmlser.Options{PageLove: true})
	if strings.Contains(got, "</noscript><img") {
		t.Fatalf("stored form lets the attribute close noscript: %q", got)
	}
}
