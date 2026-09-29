package schema

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/microdata"
)

// System vocabulary (R-MOD-1). Compared as exact strings after trimming.
const (
	URLSchema            = "https://pagelove.org/Schema"
	URLProperty          = "https://pagelove.org/Property"
	URLMethod            = "https://pagelove.org/Method"
	URLParameter         = "https://pagelove.org/Parameter"
	URLEnum              = "https://schema.host/Enum"
	URLShape             = "https://pagelove.org/ShapeConstraint"
	URLSessel            = "https://pagelove.org/Sessel"
	URLSesselLambda      = "https://pagelove.org/Sessel/Lambda"
	URLJavaScript        = "https://pagelove.org/JavaScript/Module"
	URLInstance          = "https://pagelove.org/Instance"
	URLSchemaViolation   = "https://pagelove.org/SchemaViolation"
	URLConstraintViolate = "https://pagelove.org/ConstraintViolation"
	URLViolation         = "https://pagelove.org/Violation"
	URLBindingFailure    = "https://pagelove.org/BindingFailure"
	URLHTTPResponse      = "https://pagelove.org/HTTPResponse"
	URLElement           = "https://pagelove.org/Element"
	URLGroup             = "https://pagelove.org/Group"
	URLAuthorizationRule = "https://pagelove.org/AuthorizationRule"
)

// itemType returns an element's itemtype attribute, trimmed of ASCII
// whitespace: the whole value is the type (R-MOD-9, multi-token itemtypes
// name no schema).
func itemType(n *html.Node) string {
	return dom.TrimHTMLSpace(dom.AttrOr(n, "itemtype", ""))
}

// isItem reports whether n is an element with itemscope.
func isItem(n *html.Node) bool { return microdata.IsItem(n) }

// inTemplate reports whether n lies inside <template> contents, which are
// inert: neither declarations nor instances (R-MOD-2).
func inTemplate(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if p.Type == html.ElementNode && p.Data == "template" && p.Namespace == "" {
			return true
		}
	}
	return false
}

// hasToken reports whether the element's itemprop, split on ASCII
// whitespace, contains name (R-MOD-17).
func hasToken(n *html.Node, attr, name string) bool {
	for _, t := range strings.Fields(dom.AttrOr(n, attr, "")) {
		if t == name {
			return true
		}
	}
	return false
}

// propElements returns the value elements of property name in the item
// rooted at item: its microdata properties (not descending into nested
// items; a nested item that carries the itemprop is one value), in tree
// order.
func propElements(item *html.Node, name string) []*html.Node {
	var out []*html.Node
	walkProps(item, func(n *html.Node) {
		if hasToken(n, "itemprop", name) {
			out = append(out, n)
		}
	})
	return out
}

// walkProps visits the elements carrying itemprop within the item scope of
// item (descendants outside nested items; itemref is not followed for
// validation, pagelike decision).
func walkProps(item *html.Node, fn func(*html.Node)) {
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if c.Data == "template" && c.Namespace == "" {
				continue // inert contents
			}
			if dom.HasAttr(c, "itemprop") {
				fn(c)
			}
			if !isItem(c) {
				walk(c)
			}
		}
	}
	walk(item)
}

// value is a property element's microdata value (R-MOD-19): attribute
// values and text are verbatim; a text-content element with empty text is
// null; a nested item has no string value.
type value struct {
	s    string
	null bool
	item bool
}

func valueOf(n *html.Node) value {
	if isItem(n) {
		return value{item: true}
	}
	switch valueElementName(n) {
	case "meta", "audio", "embed", "iframe", "img", "source", "track", "video", "a", "area", "link", "object", "data", "meter":
		return value{s: microdata.Value(n)}
	case "time":
		if v, ok := dom.Attr(n, "datetime"); ok {
			return value{s: v}
		}
	}
	s := dom.TextContent(n)
	return value{s: s, null: s == ""}
}

// valueElementName mirrors the element-name rule of microdata values (HTML
// tag name, XML local name).
func valueElementName(n *html.Node) string {
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

// valueStrings returns the non-null string values of elems.
func valueStrings(elems []*html.Node) []string {
	var out []string
	for _, e := range elems {
		if v := valueOf(e); !v.null && !v.item {
			out = append(out, v.s)
		}
	}
	return out
}

// nodePath is the child-index path of n from the document node.
func nodePath(n *html.Node) []int {
	var idx []int
	for x := n; x.Parent != nil; x = x.Parent {
		i := 0
		for c := x.Parent.FirstChild; c != x; c = c.NextSibling {
			i++
		}
		idx = append(idx, i)
	}
	for i, j := 0, len(idx)-1; i < j; i, j = i+1, j-1 {
		idx[i], idx[j] = idx[j], idx[i]
	}
	return idx
}

// nodeAt follows a child-index path from root (nil when it leads nowhere).
func nodeAt(root *html.Node, path []int) *html.Node {
	cur := root
	for _, i := range path {
		c := cur.FirstChild
		for k := 0; k < i && c != nil; k++ {
			c = c.NextSibling
		}
		if c == nil {
			return nil
		}
		cur = c
	}
	return cur
}

// attached reports whether n is still part of the tree rooted at root.
func attached(root, n *html.Node) bool {
	for x := n; x != nil; x = x.Parent {
		if x == root {
			return true
		}
	}
	return false
}

// documentOf returns the topmost ancestor of n.
func documentOf(n *html.Node) *html.Node {
	for n.Parent != nil {
		n = n.Parent
	}
	return n
}

// instancesUnder returns the itemscope elements in the subtree of n
// (inclusive, document order), not entering <template> contents.
func instancesUnder(n *html.Node) []*html.Node {
	var out []*html.Node
	dom.Walk(n, func(x *html.Node) bool {
		if x.Type == html.ElementNode {
			if x.Data == "template" && x.Namespace == "" && x != n {
				return false
			}
			if isItem(x) {
				out = append(out, x)
			}
		}
		return true
	})
	return out
}

// setItemprop makes sure n carries name among its itemprop tokens.
func setItemprop(n *html.Node, name string) {
	if n.Type != html.ElementNode || hasToken(n, "itemprop", name) {
		return
	}
	toks := strings.Fields(dom.AttrOr(n, "itemprop", ""))
	dom.SetAttr(n, "itemprop", strings.Join(append(toks, name), " "))
}

// newMeta builds <meta itemprop=name content=v>.
func newMeta(name, v string) *html.Node {
	return &html.Node{Type: html.ElementNode, Data: "meta", DataAtom: metaAtom,
		Attr: []html.Attribute{{Key: "itemprop", Val: name}, {Key: "content", Val: v}}}
}
