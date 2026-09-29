package selector_test

import (
	"testing"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/selector"
)

type markedSel struct{}

func (markedSel) Match(n *html.Node) bool           { return dom.HasAttr(n, "data-mark") }
func (markedSel) Specificity() selector.Specificity { return selector.Specificity{0, 1, 0} }
func (markedSel) PseudoElement() string             { return "" }
func (markedSel) String() string                    { return ":-test-marked" }

// CompileOptions accepts caller-built options, such as PageLove's plus a
// private pseudo-class (the JavaScript DOM's "|x" form, internal/jsrt).
func TestCompileOptionsPrivatePseudo(t *testing.T) {
	opts := selector.PageLoveOptions(selector.ExtOptions{})
	opts.Pseudo["-test-marked"] = func(*selector.PseudoContext) (selector.Sel, error) { return markedSel{}, nil }
	doc, err := dom.Parse([]byte(`<p>a</p><p data-mark>b</p><p data-mark>c</p>`))
	if err != nil {
		t.Fatal(err)
	}
	s, err := selector.CompileOptions("p:-test-marked:contains('c')", opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.MatchAll(doc); len(got) != 1 || dom.TextContent(got[0]) != "c" {
		t.Errorf("matched %d", len(got))
	}
	if _, err := selector.Compile("p:-test-marked"); err == nil {
		t.Error("the default options know no private pseudo-class")
	}
}
