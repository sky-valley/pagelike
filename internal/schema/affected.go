package schema

import (
	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
)

// writeKind classifies a write for validation (R-MOD-13).
type writeKind int

const (
	kindNone      writeKind = iota
	kindWhole               // whole-document write: every element is affected
	kindSelector            // selector PUT/POST/DELETE, element MOVE
	kindDocDelete           // whole-document DELETE: referential actions only
	kindDocMove             // whole-document MOVE: shapes at the destination only
)

func classify(w *engine.WriteCtx) writeKind {
	op := w.Op
	switch op.Method {
	case "DELETE":
		if op.Range.HasSelector() {
			if w.After == nil {
				return kindNone
			}
			return kindSelector
		}
		return kindDocDelete
	case "MOVE":
		if op.Range.Present() {
			if w.After == nil {
				return kindNone
			}
			return kindSelector
		}
		return kindDocMove
	case "PUT", "POST":
		if w.After == nil {
			return kindNone // blobs and unparsable markup
		}
		if !op.Range.HasSelector() {
			return kindWhole
		}
		if op.Method == "PUT" && w.Target != nil && (w.Target.Parent == nil || w.Target.Parent.Type == html.DocumentNode) {
			return kindWhole // the root element was replaced by a whole document
		}
		return kindSelector
	}
	return kindNone
}

// affectedElements computes the elements of D′ a selector write affects
// (R-MOD-14, R-MOD-63): the inserted subtrees, and the ancestors (inclusive)
// of each position where content was inserted or removed.
func affectedElements(w *engine.WriteCtx) map[*html.Node]bool {
	set := map[*html.Node]bool{}
	subtree := func(n *html.Node) {
		if n == nil {
			return
		}
		dom.Walk(n, func(x *html.Node) bool {
			if x.Type == html.ElementNode {
				set[x] = true
			}
			return true
		})
	}
	chain := func(n *html.Node) {
		for x := n; x != nil && x.Type != html.DocumentNode; x = x.Parent {
			if x.Type == html.ElementNode {
				set[x] = true
			}
		}
	}
	op := w.Op
	switch op.Method {
	case "PUT", "POST":
		var parent *html.Node
		for _, n := range w.Inserted {
			subtree(n)
			if parent == nil && n.Parent != nil {
				parent = n.Parent
			}
		}
		if parent == nil {
			parent = w.Target
		}
		chain(parent)
	case "DELETE":
		if w.Target != nil && w.Target.Parent != nil && w.Before != nil {
			chain(nodeAt(w.After, nodePath(w.Target.Parent)))
		}
	case "MOVE":
		subtree(w.Target)
		if w.Target != nil {
			chain(w.Target.Parent)
		}
		chain(moveSourceParent(w))
	}
	return set
}

// moveSourceParent finds, in w.After, the element the moved element was
// removed from: the move is replayed on a copy of w.Before to follow the
// source parent through the insertion.
func moveSourceParent(w *engine.WriteCtx) *html.Node {
	op := w.Op
	if w.Before == nil || w.After == nil {
		return nil
	}
	srcSel, err := op.Range.CheckSelector()
	if err != nil {
		return nil
	}
	dstSel, err := op.DestinationRange.CheckSelector()
	if err != nil {
		return nil
	}
	src, anchor := srcSel.MatchFirst(w.Before), dstSel.MatchFirst(w.Before)
	if src == nil || anchor == nil || src.Parent == nil {
		return nil
	}
	c := dom.Clone(w.Before)
	s2, a2 := nodeAt(c, nodePath(src)), nodeAt(c, nodePath(anchor))
	if s2 == nil || a2 == nil {
		return nil
	}
	parent := s2.Parent
	parent.RemoveChild(s2)
	switch op.DestinationRange.Placement {
	case "append":
		a2.AppendChild(s2)
	case "prepend":
		a2.InsertBefore(s2, a2.FirstChild)
	case "before":
		if a2.Parent == nil {
			return nil
		}
		a2.Parent.InsertBefore(s2, a2)
	case "after":
		if a2.Parent == nil {
			return nil
		}
		a2.Parent.InsertBefore(s2, a2.NextSibling)
	default:
		return nil
	}
	return nodeAt(w.After, nodePath(parent))
}

// governed reports whether n is an instance of a registered schema.
func (r *Registry) governed(n *html.Node) bool {
	return isItem(n) && r.schemas[itemType(n)] != nil
}

// affectedInstances returns the governed instances among the affected
// elements (every instance of D′ when elems is nil), in document order.
// Only outermost items are instances: PageLove validates no item that lies
// inside another item's markup, whether it is a property value or not and
// whatever the outer item's type (live 2026-09-29; R-MOD-14, R-MOD-24).
func (r *Registry) affectedInstances(root *html.Node, elems map[*html.Node]bool) []*html.Node {
	var out []*html.Node
	for _, n := range instancesUnder(root) {
		if (elems == nil || elems[n]) && r.governed(n) && !insideItem(n) {
			out = append(out, n)
		}
	}
	return out
}

// insideItem reports whether an ancestor of n is an item.
func insideItem(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		if isItem(p) {
			return true
		}
	}
	return false
}
