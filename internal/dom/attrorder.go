package dom

import (
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// formattingElements are the elements x/net/html (>= v0.55.0) pushes on the
// list of active formatting elements via addFormattingElement, which sorts
// the element's attributes in place (slices.SortFunc(top.Attr, attrCompare))
// to speed up the Noah's Ark comparison. The sort leaks into the DOM, so
// <a itemprop="url" href="/"> re-serializes as <a href="/" itemprop="url">.
// html5ever, browsers and x/net/html <= v0.54.0 keep source order.
var formattingElements = map[atom.Atom]bool{
	atom.A: true, atom.B: true, atom.Big: true, atom.Code: true, atom.Em: true,
	atom.Font: true, atom.I: true, atom.Nobr: true, atom.S: true, atom.Small: true,
	atom.Strike: true, atom.Strong: true, atom.Tt: true, atom.U: true,
}

func attrSig(tag string, attrs []html.Attribute) string {
	parts := make([]string, len(attrs))
	for i, a := range attrs {
		parts[i] = a.Namespace + "\x00" + a.Key + "\x00" + a.Val
	}
	slices.Sort(parts)
	return tag + "\x01" + strings.Join(parts, "\x01")
}

// FixAttrOrder restores source attribute order on formatting elements. It
// re-tokenizes src and indexes each formatting start tag by its (sorted)
// attribute set; elements whose attribute set was written in two different
// orders in the same source take the first order seen.
func FixAttrOrder(root *html.Node, src string) {
	orders := map[string][]html.Attribute{}
	z := html.NewTokenizer(strings.NewReader(src))
	for tt := z.Next(); tt != html.ErrorToken; tt = z.Next() {
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		tok := z.Token()
		if !formattingElements[tok.DataAtom] || len(tok.Attr) < 2 {
			continue
		}
		sig := attrSig(tok.Data, tok.Attr)
		if _, ok := orders[sig]; !ok {
			orders[sig] = tok.Attr
		}
	}
	if len(orders) == 0 {
		return
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Namespace == "" && formattingElements[n.DataAtom] && len(n.Attr) >= 2 {
			if want, ok := orders[attrSig(n.Data, n.Attr)]; ok {
				n.Attr = slices.Clone(want)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}
