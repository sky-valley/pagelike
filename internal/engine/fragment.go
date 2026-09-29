package engine

import (
	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

// Fragments as served. PageLove serves an element's stored markup: a
// fragment read, a write response and the fragment hash inside an element
// ETag all show e.g. <div itemscope …> with the bare attribute as stored
// (live 2026-09-28), where a serializer would write itemscope="". pagelike
// stores the uploaded bytes (LO-1), so it serves the element's source text
// whenever that text stands alone, and its serialization otherwise.

// servedHTML returns the markup served for element n of a document parsed
// from src with spans sp: n's exact source text when the source correlates
// cleanly with the tree and n and every element inside it have an explicit
// end tag (or are void), else n's serialization.
func servedHTML(n *html.Node, src []byte, sp dom.Spans) string {
	if n != nil && n.Type == html.ElementNode && sp.Clean {
		if s, ok := sp.By[n]; ok && s.End > s.Start && s.End <= len(src) && standsAlone(n, sp) {
			return string(src[s.Start:s.End])
		}
	}
	return dom.OuterHTML(n)
}

// standsAlone reports whether every element in n's subtree has a source
// span that ends with its own end tag (or needs none).
func standsAlone(n *html.Node, sp dom.Spans) bool {
	if n.Type == html.ElementNode {
		s, ok := sp.By[n]
		if !ok || !(s.ExplicitEnd || s.NoContent) {
			return false
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if !standsAlone(c, sp) {
			return false
		}
	}
	return true
}

// docView is a parsed stored document with lazily computed source spans,
// for serving fragments of it.
type docView struct {
	root  *html.Node
	src   []byte
	xml   bool
	spans *dom.Spans
}

func newDocView(root *html.Node, doc *store.Document) *docView {
	return &docView{root: root, src: doc.Body, xml: site.IsXML(doc.ContentType)}
}

// served returns the markup a read serves for n.
func (v *docView) served(n *html.Node) string {
	if v == nil || v.xml || v.src == nil {
		return dom.OuterHTML(n)
	}
	if v.spans == nil {
		sp := dom.ComputeSpans(v.root, string(v.src))
		v.spans = &sp
	}
	return servedHTML(n, v.src, *v.spans)
}

// servedBefore returns the served markup of an element of w.Before (the
// document as stored before the write).
func (w *WriteCtx) servedBefore(n *html.Node) string {
	if w.Doc == nil || site.IsXML(w.Doc.ContentType) {
		return dom.OuterHTML(n)
	}
	return servedHTML(n, w.Doc.Body, w.sourceSpans())
}

// currentTag is the element tag a precondition on n compares against.
func (w *WriteCtx) currentTag(n *html.Node) string {
	return FragmentETag(w.servedBefore(n), w.version())
}

// servedAfter re-reads a just-stored document and returns a function giving
// the served markup, in the stored document, of nodes of after (the tree
// the write produced, which the stored document re-parses to).
func servedAfter(stored *store.Document, after *html.Node) func(*html.Node) string {
	if stored == nil || site.IsXML(stored.ContentType) {
		return dom.OuterHTML
	}
	root, err := dom.Parse(stored.Body)
	if err != nil {
		return dom.OuterHTML
	}
	view := newDocView(root, stored)
	return func(n *html.Node) string {
		if n.Type != html.ElementNode {
			return dom.OuterHTML(n)
		}
		m := correspondingNode(after, root, n)
		if m == nil || m.Type != html.ElementNode || m.Data != n.Data {
			return dom.OuterHTML(n)
		}
		return view.served(m)
	}
}

// correspondingNode finds, in other, the node at n's position in orig (by
// child indexes), or nil when the trees differ along the way.
func correspondingNode(orig, other, n *html.Node) *html.Node {
	var idx []int
	for x := n; x != nil && x != orig; x = x.Parent {
		if x.Parent == nil {
			return nil
		}
		i := 0
		for c := x.Parent.FirstChild; c != x; c = c.NextSibling {
			i++
		}
		idx = append(idx, i)
	}
	cur := other
	for k := len(idx) - 1; k >= 0; k-- {
		c := cur.FirstChild
		for i := 0; i < idx[k] && c != nil; i++ {
			c = c.NextSibling
		}
		if c == nil {
			return nil
		}
		cur = c
	}
	return cur
}

// servedNodes renders a node list with served markup for elements.
func servedNodes(nodes []*html.Node, served func(*html.Node) string) string {
	var out string
	for _, n := range nodes {
		out += served(n)
	}
	return out
}
