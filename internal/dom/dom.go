// Package dom holds the parsing, serialization and tree helpers the runtime
// needs: whole documents, fragments, outer/inner HTML and text content, and
// the source spans that composition reads template sources through.
//
// HTML follows PageLove's document model (tree.go, live observation LO-15):
// the tree is built straight from x/net/html's tokens, with no HTML5 tree
// construction, and serialized in PageLove's form (htmlser.Options.PageLove),
// which is also how every HTML document is stored. XML-family documents are
// parsed into the same node type by xmldom; the serialization helpers here
// pick the right serializer from the tree.
package dom

import (
	"bytes"
	"errors"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/sky-valley/pagelike/internal/htmlser"
	"github.com/sky-valley/pagelike/internal/xmldom"
)

// Node aliases the parser's node type.
type Node = html.Node

// Parse parses an HTML document with PageLove's model (BuildTree).
func Parse(markup []byte) (*Node, error) {
	doc, _ := BuildTree(string(markup))
	return doc, nil
}

// ParseXML parses an XML-family document into a node tree (package xmldom).
func ParseXML(markup []byte) (*Node, error) {
	return xmldom.Parse(markup, xmldom.Options{})
}

// Implied marks elements a tree builder created without a start tag in the
// source. PageLove's model creates none, so it is always empty; it remains
// for the serializer option that omits such elements.
type Implied map[*Node]bool

// ParseDocument parses an HTML document (BuildTree); the Implied result is
// always empty.
func ParseDocument(src string) (*Node, Implied, error) {
	doc, _ := BuildTree(src)
	return doc, Implied{}, nil
}

// ImpliedFor is kept for callers of the former HTML5 model; PageLove's
// model implies no elements, so it is always empty.
func ImpliedFor(doc *Node, src string) Implied { return Implied{} }

// pageLove is the HTML serialization used for responses and storage alike.
var pageLove = htmlser.Options{PageLove: true}

// Render serializes a whole document (or any node) for a response:
// PageLove's form for HTML trees, well-formed XML for trees built by xmldom.
func Render(n *Node) []byte {
	var b bytes.Buffer
	if xmldom.IsXML(n) {
		xmldom.Render(&b, n)
	} else {
		htmlser.Render(&b, n, pageLove)
	}
	return b.Bytes()
}

// RenderStorage serializes a document for storage. PageLove stores every
// HTML write in its serialized form (LO-15), which parsing reproduces
// exactly, so the stored bytes are a fixed point.
func RenderStorage(doc *Node, _ Implied) []byte { return Render(doc) }

// StorageHTML serializes one node as it is stored (the same as OuterHTML).
func StorageHTML(n *Node) string { return OuterHTML(n) }

// OuterHTML serializes a node including itself.
func OuterHTML(n *Node) string {
	if xmldom.IsXML(n) {
		return xmldom.String(n)
	}
	return htmlser.String(n, pageLove)
}

// InnerHTML serializes a node's children.
func InnerHTML(n *Node) string {
	if xmldom.IsXML(n) {
		return xmldom.InnerXML(n)
	}
	return htmlser.InnerHTML(n, pageLove)
}

// TextContent returns the concatenated text of a node's descendants, like
// the DOM textContent getter.
func TextContent(n *Node) string {
	if n.Type == html.TextNode || n.Type == html.CommentNode {
		return n.Data
	}
	var b strings.Builder
	var walk func(*Node)
	walk = func(n *Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case html.TextNode:
				b.WriteString(c.Data)
			case html.ElementNode:
				walk(c)
			}
		}
	}
	walk(n)
	return b.String()
}

// SetTextContent replaces a node's children with a single text node.
func SetTextContent(n *Node, s string) {
	RemoveChildren(n)
	if s != "" {
		n.AppendChild(&Node{Type: html.TextNode, Data: s})
	}
}

// RemoveChildren detaches all children of n.
func RemoveChildren(n *Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		n.RemoveChild(c)
		c = next
	}
}

// ErrNoContext is returned by ParseFragment without a context element.
var ErrNoContext = errors.New("fragment parsing needs the element the markup will be inserted into")

// TrimHTMLSpace trims ASCII whitespace as HTML defines it (not U+00A0).
func TrimHTMLSpace(s string) string { return strings.Trim(s, " \t\n\f\r") }

// ParseFragment parses markup to be inserted as children of context, the
// live element from the destination tree. In PageLove's model markup parses
// the same everywhere (a POSTed <tr> is a tr under any parent); the context
// matters only for XML documents (parsed as XML, xmldom.ParseFragment) and
// for elements whose content is text (script, style, textarea, title, …).
//
// Leading and trailing whitespace of the markup is trimmed so a request body
// ending in a newline does not add stray text nodes.
func ParseFragment(markup string, context *Node) ([]*Node, error) {
	if context == nil || context.Type != html.ElementNode && context.Type != html.DocumentNode {
		return nil, ErrNoContext
	}
	markup = TrimHTMLSpace(markup)
	if xmldom.IsXML(context) {
		return xmldom.ParseFragment(markup, context)
	}
	return parseHTMLFragment(markup, context)
}

// ParseBodyFragment parses markup on its own (for inspecting and comparing
// markup: the harness, tests).
func ParseBodyFragment(markup string) ([]*Node, error) {
	return parseHTMLFragment(TrimHTMLSpace(markup), &Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
}

func parseHTMLFragment(markup string, context *Node) ([]*Node, error) {
	if context.Namespace == "" {
		switch {
		case rawTextElements[context.Data]:
			return []*Node{{Type: html.TextNode, Data: markup}}, nil
		case context.Data == "textarea" || context.Data == "title":
			return []*Node{{Type: html.TextNode, Data: DecodeReferences(markup)}}, nil
		}
	}
	holder, _ := BuildTree(markup)
	var nodes []*Node
	for c := holder.FirstChild; c != nil; {
		next := c.NextSibling
		holder.RemoveChild(c)
		nodes = append(nodes, c)
		c = next
	}
	return nodes, nil
}

// DropEmptyImplied is kept for callers of the former HTML5 model, whose
// fragment parser synthesized head and body; PageLove's model does not.
func DropEmptyImplied(nodes []*Node, markup string) []*Node { return nodes }

// Elements filters nodes down to element nodes.
func Elements(nodes []*Node) []*Node {
	var out []*Node
	for _, n := range nodes {
		if n.Type == html.ElementNode {
			out = append(out, n)
		}
	}
	return out
}

// Attr returns an attribute value and whether it is present.
func Attr(n *Node, name string) (string, bool) {
	if n == nil {
		return "", false
	}
	for _, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, name) {
			return a.Val, true
		}
	}
	return "", false
}

// AttrOr returns an attribute value or def.
func AttrOr(n *Node, name, def string) string {
	if v, ok := Attr(n, name); ok {
		return v
	}
	return def
}

// SetAttr sets or adds an attribute.
func SetAttr(n *Node, name, val string) {
	for i, a := range n.Attr {
		if a.Namespace == "" && strings.EqualFold(a.Key, name) {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: name, Val: val})
}

// RemoveAttr deletes an attribute if present.
func RemoveAttr(n *Node, name string) {
	out := n.Attr[:0]
	for _, a := range n.Attr {
		if !(a.Namespace == "" && strings.EqualFold(a.Key, name)) {
			out = append(out, a)
		}
	}
	n.Attr = out
}

// HasAttr reports whether n carries attribute name.
func HasAttr(n *Node, name string) bool { _, ok := Attr(n, name); return ok }

// Walk visits n and its descendants in document order; returning false from
// fn skips the node's children.
func Walk(n *Node, fn func(*Node) bool) {
	if !fn(n) {
		return
	}
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		Walk(c, fn)
		c = next
	}
}

// ElementChildren returns the element children of n.
func ElementChildren(n *Node) []*Node {
	var out []*Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			out = append(out, c)
		}
	}
	return out
}

// DocumentElement returns the <html> element of a document node.
func DocumentElement(doc *Node) *Node {
	for c := doc.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			return c
		}
	}
	return nil
}

// FindElement returns the first descendant element with the given tag.
func FindElement(n *Node, tag string) *Node {
	var found *Node
	Walk(n, func(x *Node) bool {
		if found != nil {
			return false
		}
		if x.Type == html.ElementNode && x.Data == tag {
			found = x
			return false
		}
		return true
	})
	return found
}

// Body returns the document's body element.
func Body(doc *Node) *Node { return FindElement(doc, "body") }

// Head returns the document's head element.
func Head(doc *Node) *Node { return FindElement(doc, "head") }

// Clone deep-copies a node (detached).
func Clone(n *Node) *Node {
	c := &Node{Type: n.Type, DataAtom: n.DataAtom, Data: n.Data, Namespace: n.Namespace}
	c.Attr = append([]html.Attribute(nil), n.Attr...)
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		c.AppendChild(Clone(ch))
	}
	return c
}

// Detach removes n from its parent if it has one.
func Detach(n *Node) {
	if n.Parent != nil {
		n.Parent.RemoveChild(n)
	}
}

// InsertBefore inserts nodes before ref under ref's parent.
func InsertBefore(ref *Node, nodes []*Node) {
	for _, x := range nodes {
		Detach(x)
		ref.Parent.InsertBefore(x, ref)
	}
}

// InsertAfter inserts nodes after ref under ref's parent, preserving order.
func InsertAfter(ref *Node, nodes []*Node) {
	next := ref.NextSibling
	for _, x := range nodes {
		Detach(x)
		ref.Parent.InsertBefore(x, next)
	}
}

// Prepend inserts nodes as the first children of parent, preserving order.
func Prepend(parent *Node, nodes []*Node) {
	first := parent.FirstChild
	for _, x := range nodes {
		Detach(x)
		parent.InsertBefore(x, first)
	}
}

// Append appends nodes as the last children of parent.
func Append(parent *Node, nodes []*Node) {
	for _, x := range nodes {
		Detach(x)
		parent.AppendChild(x)
	}
}

// Replace replaces old with nodes in old's parent.
func Replace(old *Node, nodes []*Node) {
	InsertBefore(old, nodes)
	old.Parent.RemoveChild(old)
}

// IsAncestor reports whether a is an ancestor of (or equal to) b.
func IsAncestor(a, b *Node) bool {
	for x := b; x != nil; x = x.Parent {
		if x == a {
			return true
		}
	}
	return false
}

// ElementIndex returns the 1-based index of n among its parent's element
// children (for :nth-child paths).
func ElementIndex(n *Node) int {
	i := 0
	for c := n.Parent.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			i++
		}
		if c == n {
			return i
		}
	}
	return 0
}
