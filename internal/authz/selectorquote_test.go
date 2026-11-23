package authz

import (
	"testing"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/selector"
)

// R-PERM-120: a substituted value is literal text in a selector, quoted or
// not, and never changes the selector's structure.
func TestSelectorQuoteIsLiteral(t *testing.T) {
	values := []string{
		`alice`, `x"], *, [data-x="`, `a b`, `a, *`, `1st`, `-`, `-1`,
		"tab\there", `back\slash`, `é√`, `a}b{c`, `'single'`, "nul\x00",
	}
	for _, v := range values {
		want := v
		if want == "nul\x00" {
			want = "nul�"
		}
		own := &html.Node{Type: html.ElementNode, Data: "p", Attr: []html.Attribute{{Key: "data-owner", Val: want}, {Key: "class", Val: want}}}
		other := &html.Node{Type: html.ElementNode, Data: "p", Attr: []html.Attribute{{Key: "data-owner", Val: "bob"}, {Key: "class", Val: "bob"}}}
		for _, tmpl := range []string{`[data-owner="%s"]`, `[data-owner='%s']`, `p.%s`} {
			src := fmtSel(tmpl, SelectorQuote(v))
			sel, err := selector.Compile(src)
			if err != nil {
				t.Errorf("%q: %s does not parse: %v", v, src, err)
				continue
			}
			if sel.Matches(other) {
				t.Errorf("%q: %s matches another element", v, src)
			}
			if tmpl != `p.%s` || !containsSpace(want) {
				if !sel.Matches(own) {
					t.Errorf("%q: %s does not match the element with that exact value", v, src)
				}
			}
		}
	}
}

func fmtSel(tmpl, v string) string {
	out := []byte{}
	for i := 0; i < len(tmpl); i++ {
		if tmpl[i] == '%' && i+1 < len(tmpl) && tmpl[i+1] == 's' {
			out = append(out, v...)
			i++
			continue
		}
		out = append(out, tmpl[i])
	}
	return string(out)
}

// A class attribute is a space-separated list, so a value with whitespace
// names one class no element can carry.
func containsSpace(s string) bool {
	for _, r := range s {
		if r == ' ' || r == '\t' {
			return true
		}
	}
	return false
}
