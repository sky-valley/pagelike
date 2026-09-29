// Package microdata extracts HTML microdata items (itemscope / itemtype /
// itemprop / itemid / itemref) following the WHATWG HTML algorithm, and
// renders them as JSON-LD.
package microdata

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
)

// Item is a microdata item.
type Item struct {
	Node  *html.Node
	Types []string // itemtype tokens
	ID    string   // itemid
	Props []Prop   // in tree order
}

// Prop is one property value: either a string or a nested item.
type Prop struct {
	Name  string
	Node  *html.Node
	Value string
	Item  *Item
}

// Type returns the first itemtype or "".
func (it *Item) Type() string {
	if len(it.Types) == 0 {
		return ""
	}
	return it.Types[0]
}

// HasType reports whether the item declares type t.
func (it *Item) HasType(t string) bool {
	for _, x := range it.Types {
		if x == t {
			return true
		}
	}
	return false
}

// Get returns the first string value of prop name (nested items yield "").
func (it *Item) Get(name string) string {
	for _, p := range it.Props {
		if p.Name == name {
			return p.Value
		}
	}
	return ""
}

// Has reports whether the item has at least one value for name.
func (it *Item) Has(name string) bool {
	for _, p := range it.Props {
		if p.Name == name {
			return true
		}
	}
	return false
}

// All returns every string value of prop name, in order.
func (it *Item) All(name string) []string {
	var out []string
	for _, p := range it.Props {
		if p.Name == name {
			out = append(out, p.Value)
		}
	}
	return out
}

// Items returns nested items for prop name.
func (it *Item) Items(name string) []*Item {
	var out []*Item
	for _, p := range it.Props {
		if p.Name == name && p.Item != nil {
			out = append(out, p.Item)
		}
	}
	return out
}

// PropNodes returns the elements carrying prop name.
func (it *Item) PropNodes(name string) []*html.Node {
	var out []*html.Node
	for _, p := range it.Props {
		if p.Name == name {
			out = append(out, p.Node)
		}
	}
	return out
}

// IsItem reports whether n is an element with itemscope.
func IsItem(n *html.Node) bool {
	return n != nil && n.Type == html.ElementNode && dom.HasAttr(n, "itemscope")
}

// TopLevel returns top-level items (itemscope without itemprop) under root.
func TopLevel(root *html.Node) []*Item {
	var out []*Item
	dom.Walk(root, func(n *html.Node) bool {
		if IsItem(n) && !dom.HasAttr(n, "itemprop") {
			out = append(out, Parse(n))
		}
		return true
	})
	return out
}

// All items (top-level or nested) under root, in document order.
func AllItems(root *html.Node) []*Item {
	var out []*Item
	dom.Walk(root, func(n *html.Node) bool {
		if IsItem(n) {
			out = append(out, Parse(n))
		}
		return true
	})
	return out
}

// OfType returns every item under root declaring itemtype t (nested included).
func OfType(root *html.Node, t string) []*Item {
	var out []*Item
	dom.Walk(root, func(n *html.Node) bool {
		if IsItem(n) {
			for _, x := range strings.Fields(dom.AttrOr(n, "itemtype", "")) {
				if x == t {
					out = append(out, Parse(n))
					break
				}
			}
		}
		return true
	})
	return out
}

// Parse builds the item rooted at an itemscope element.
func Parse(n *html.Node) *Item {
	return parse(n, map[*html.Node]bool{})
}

func parse(n *html.Node, visiting map[*html.Node]bool) *Item {
	it := &Item{Node: n, Types: strings.Fields(dom.AttrOr(n, "itemtype", "")), ID: strings.TrimSpace(dom.AttrOr(n, "itemid", ""))}
	if visiting[n] {
		return it
	}
	visiting[n] = true
	defer delete(visiting, n)
	for _, pn := range propertyElements(n) {
		names := strings.Fields(dom.AttrOr(pn, "itemprop", ""))
		for _, name := range names {
			p := Prop{Name: name, Node: pn}
			if IsItem(pn) {
				p.Item = parse(pn, visiting)
			} else {
				p.Value = Value(pn)
			}
			it.Props = append(it.Props, p)
		}
	}
	return it
}

// propertyElements implements the "crawl the properties" algorithm: the
// item's descendants plus itemref targets, not descending into nested
// itemscope elements, sorted in tree order.
func propertyElements(root *html.Node) []*html.Node {
	var pending []*html.Node
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		pending = append(pending, c)
	}
	if refs := strings.Fields(dom.AttrOr(root, "itemref", "")); len(refs) > 0 {
		top := root
		for top.Parent != nil {
			top = top.Parent
		}
		for _, id := range refs {
			if el := findByID(top, id); el != nil {
				pending = append(pending, el)
			}
		}
	}
	seen := map[*html.Node]bool{}
	var results []*html.Node
	for len(pending) > 0 {
		cur := pending[0]
		pending = pending[1:]
		if seen[cur] || cur == root {
			continue
		}
		seen[cur] = true
		if cur.Type != html.ElementNode {
			continue
		}
		if !IsItem(cur) {
			for c := cur.FirstChild; c != nil; c = c.NextSibling {
				pending = append(pending, c)
			}
		}
		if dom.HasAttr(cur, "itemprop") {
			results = append(results, cur)
		}
	}
	sortTreeOrder(root, results)
	return results
}

func findByID(root *html.Node, id string) *html.Node {
	var found *html.Node
	dom.Walk(root, func(n *html.Node) bool {
		if found != nil {
			return false
		}
		if n.Type == html.ElementNode {
			if v, ok := dom.Attr(n, "id"); ok && v == id {
				found = n
				return false
			}
		}
		return true
	})
	return found
}

func sortTreeOrder(anchor *html.Node, nodes []*html.Node) {
	if len(nodes) < 2 {
		return
	}
	top := anchor
	for top.Parent != nil {
		top = top.Parent
	}
	order := map[*html.Node]int{}
	i := 0
	dom.Walk(top, func(n *html.Node) bool {
		order[n] = i
		i++
		return true
	})
	// insertion sort: lists are short
	for a := 1; a < len(nodes); a++ {
		for b := a; b > 0 && order[nodes[b]] < order[nodes[b-1]]; b-- {
			nodes[b], nodes[b-1] = nodes[b-1], nodes[b]
		}
	}
}

// valueName is the element name the property-value rules switch on: the tag
// name for HTML elements, the local name for XML documents (PageLove runs
// microdata on XML too, e.g. an AuthorizationRule inside an Atom feed,
// where DataAtom is always 0 and names may carry a prefix), and "" for
// SVG/MathML inside HTML, which the rules do not name.
func valueName(n *html.Node) string {
	switch n.Namespace {
	case "":
		return n.Data
	case "svg", "math":
		return ""
	}
	if i := strings.LastIndexByte(n.Data, ':'); i >= 0 {
		return n.Data[i+1:]
	}
	return n.Data
}

// Value returns an element's microdata property value (HTML §5.4).
func Value(n *html.Node) string {
	switch valueName(n) {
	case "meta":
		return dom.AttrOr(n, "content", "")
	case "audio", "embed", "iframe", "img", "source", "track", "video":
		return dom.AttrOr(n, "src", "")
	case "a", "area", "link":
		return dom.AttrOr(n, "href", "")
	case "object":
		return dom.AttrOr(n, "data", "")
	case "data", "meter":
		return dom.AttrOr(n, "value", "")
	case "time":
		if v, ok := dom.Attr(n, "datetime"); ok {
			return v
		}
	}
	return dom.TextContent(n)
}

// SetValue writes a property value into an element using the attribute or
// text slot that Value reads.
func SetValue(n *html.Node, v string) {
	switch valueName(n) {
	case "meta":
		dom.SetAttr(n, "content", v)
	case "audio", "embed", "iframe", "img", "source", "track", "video":
		dom.SetAttr(n, "src", v)
	case "a", "area", "link":
		dom.SetAttr(n, "href", v)
	case "object":
		dom.SetAttr(n, "data", v)
	case "data", "meter":
		dom.SetAttr(n, "value", v)
	case "time":
		if dom.HasAttr(n, "datetime") {
			dom.SetAttr(n, "datetime", v)
			return
		}
		dom.SetTextContent(n, v)
	default:
		dom.SetTextContent(n, v)
	}
}
