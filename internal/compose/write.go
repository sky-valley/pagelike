package compose

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

// Selector writes on composed pages (docs/spec/composing.md §13–§15,
// reading-writing R-RW-16). The selector is resolved against the page's
// composed view for the requester; then, by the target's provenance:
//
//	native element of the stored page → ordinary write of that element
//	projected (include / stamp) from D  → write of the origin element in D,
//	                                      authorized against the page only
//	inside a transient element          → the requester's session copy
//	generated (templates, results)      → 416
//
// On a route URL (no literal document) only projected targets are
// writable; the template itself is never written through a concrete URL
// (R-COMP-106). A composition error blocks the write (R-COMP-94).
func (e *Engine) composeWrite(ctx context.Context, eng *engine.Engine, w *engine.WriteCtx) (*engine.Result, error) {
	op := w.Op
	pol := w.Snap.Policy
	areq := authz.Request{Principal: op.Principal, Method: op.Method, HTTPMethod: op.Method, Path: op.Path, Header: op.Header, Query: op.Query}
	if !engine.CanGrant(pol, areq) {
		return nil, w.Refuse() // refused before existence is revealed (R-RW-126)
	}
	doc := w.Doc
	var params []Param
	route := false
	if doc == nil {
		tpl, ps, ok := resolveRoute(w.Snap, op.Path)
		if !ok {
			return nil, nil // 404 from the engine
		}
		d, err := w.Tx.Get(tpl)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if d.IsBlob() || !site.IsMarkup(d.ContentType) {
			return nil, nil
		}
		doc, params, route = d, ps, true
	} else if w.Before == nil {
		return nil, nil // not markup: the engine refuses the selector
	}
	req := &Request{Method: op.Method, Path: op.Path, Query: op.Query, RawQuery: op.Query.Encode(), Header: op.Header,
		Host: op.Host, Principal: op.Principal, Params: params, ContentType: op.ContentType, Body: op.Body}
	res, err := e.compose(ctx, w.Site, w.Snap, doc, nil, req, options{track: true})
	if err != nil {
		return nil, err
	}
	if !route && !res.changed {
		return nil, nil // the composed view is the stored document
	}
	root, spans, err := parseWithSpans(res.text, res.xml)
	if err != nil {
		return nil, compositionError("composed %s does not parse: %v", doc.Path, err)
	}
	sel, err := op.Range.CheckSelector()
	if err != nil {
		return nil, err
	}
	placement := op.Range.Placement
	if op.Method == http.MethodPost && placement == "" {
		placement = "append"
	}
	sibling := op.Method == http.MethodPost && (placement == "before" || placement == "after")
	t := sel.MatchFirst(root)
	if t == nil {
		read := areq
		read.Method, read.HTTPMethod = http.MethodGet, http.MethodGet
		if sibling || !pol.Decide(read, nil).Allowed {
			return nil, w.Refuse()
		}
		return nil, engine.RangeNotSatisfiable(op.Range.Selector)
	}
	sp, ok := spans.By[t]
	if !ok {
		return nil, engine.RangeNotSatisfiable(op.Range.Selector)
	}
	for _, tr := range res.trans {
		if sp.Start >= tr.start && sp.Start < tr.end {
			return e.transientWrite(w, root, spans, tr, t, sel, placement, sibling)
		}
	}
	var a *anchor
	for i := range res.anchors {
		if res.anchors[i].off == sp.Start {
			a = &res.anchors[i]
			break
		}
	}
	if a == nil {
		return nil, engine.RangeNotSatisfiable(op.Range.Selector) // generated content has no writable origin
	}
	idx := a.idx()
	if idx == nil {
		return nil, engine.RangeNotSatisfiable(op.Range.Selector)
	}
	locate := func(before *html.Node) *html.Node {
		if n := nodeAt(before, idx); n != nil && n.Type == html.ElementNode {
			return n
		}
		return nil
	}
	if a.path == doc.Path {
		if route {
			return nil, engine.RangeNotSatisfiable(op.Range.Selector)
		}
		w.Locate(locate)
		return nil, nil
	}
	// Write-through: authorized against the page and the target as it
	// appears there; the origin's own rules are not consulted (R-COMP-92).
	scopeEl := t
	if sibling {
		scopeEl = t.Parent
	}
	if scopeEl == nil || scopeEl.Type != html.ElementNode || !pol.Decide(areq, scopeEl).Allowed {
		return nil, w.Refuse()
	}
	routed := *op
	routed.Path = a.path
	if pd := w.Snap.Docs[a.path]; pd != nil {
		if on := locate(pd.Root); on != nil {
			// Subscribers of the origin get a selector that addresses the
			// element there (R-SSE-19).
			if canon := selector.Path(pd.Root, on); canon != "" {
				if _, err := selector.Compile(canon); err == nil {
					routed.Range.Selector = canon
				}
			}
		}
	}
	out, err := eng.ApplyRouted(ctx, w, &routed, locate)
	if err != nil {
		return nil, err
	}
	if out.Header.Get("Content-Range") != "" {
		out.Header.Set("Content-Range", "selector "+op.Range.Selector)
	}
	return out, nil
}

// transientWrite applies a selector write inside a transient element to
// the requester's session copy (R-COMP-112): no stored document changes
// and no event is emitted.
func (e *Engine) transientWrite(w *engine.WriteCtx, root *html.Node, spans dom.Spans, tr transRec, t *html.Node, sel *selector.Selector, placement string, sibling bool) (*engine.Result, error) {
	op := w.Op
	if op.Principal == nil || op.Principal.Session == "" {
		return nil, engine.NoSession()
	}
	var troot *html.Node
	for n, sp := range spans.By {
		if sp.Start == tr.start {
			troot = n
			break
		}
	}
	if troot == nil || troot.Parent == nil {
		return nil, engine.RangeNotSatisfiable(op.Range.Selector)
	}
	if sibling && t == troot {
		return nil, errdoc.New(http.StatusMethodNotAllowed, "TransientPlacement", "placement=%s would insert outside the transient element", placement)
	}
	scopeEl := t
	if sibling {
		scopeEl = t.Parent
	}
	areq := authz.Request{Principal: op.Principal, Method: op.Method, HTTPMethod: op.Method, Path: op.Path, Header: op.Header, Query: op.Query}
	if !w.Snap.Policy.Decide(areq, scopeEl).Allowed {
		return nil, w.Refuse()
	}
	session := op.Principal.Session
	res := &engine.Result{Header: http.Header{}}
	res.Header.Set("Content-Range", "selector "+op.Range.Selector)
	// The answer is built from the requester's session copy, so it is as
	// private as a read of it (R-COMP-114; pagelove-shop's basket, ACC-SH-2).
	res.Header.Set("Cache-Control", "private")
	newRoot := troot
	switch op.Method {
	case http.MethodDelete:
		if t == troot {
			if err := dropSessionCopy(w.Tx, session, op.Path, tr.key); err != nil {
				return nil, err
			}
			res.Status = http.StatusNoContent
			return res, nil
		}
		t.Parent.RemoveChild(t)
		res.Status = http.StatusNoContent
	case http.MethodPut:
		nodes, err := dom.ParseFragment(string(op.Body), t.Parent)
		if err != nil {
			return nil, errdoc.New(http.StatusBadRequest, "BadBody", "cannot parse body: %v", err)
		}
		first := firstElement(nodes)
		dom.Replace(t, nodes)
		// The replacement must keep the selector's identity (R-COMP-112).
		if first == nil || !sel.Matches(first) {
			return nil, errdoc.New(http.StatusUnprocessableEntity, "TransientIdentity",
				"the replacement no longer matches %s; a transient element keeps its identity", op.Range.Selector)
		}
		if t == troot {
			newRoot = first
		}
		res.Status = http.StatusPartialContent
		res.Body = []byte(renderNodes(nodes))
		res.Header.Set("Content-Type", "text/html")
		res.Header.Set("ETag", engine.ElementETag(first, docVersion(w)))
	case http.MethodPost:
		ctxNode := t
		if sibling {
			ctxNode = t.Parent
		}
		nodes, err := dom.ParseFragment(string(op.Body), ctxNode)
		if err != nil {
			return nil, errdoc.New(http.StatusBadRequest, "BadBody", "cannot parse body: %v", err)
		}
		switch placement {
		case "prepend":
			dom.Prepend(t, nodes)
		case "before":
			dom.InsertBefore(t, nodes)
		case "after":
			dom.InsertAfter(t, nodes)
		default:
			dom.Append(t, nodes)
		}
		res.Status = http.StatusPartialContent
		res.Body = []byte(renderNodes(nodes))
		res.Header.Set("Content-Type", "text/html")
		res.Header.Set("ETag", engine.ElementETag(t, docVersion(w)))
	default:
		return nil, errdoc.New(http.StatusMethodNotAllowed, "MethodNotAllowed", "%s is not supported on transient elements", op.Method)
	}
	copyRoot := dom.Clone(newRoot)
	stripTransientMarker(copyRoot)
	if err := saveSessionCopy(w.Tx, session, op.Path, tr.key, dom.OuterHTML(copyRoot)); err != nil {
		return nil, err
	}
	return res, nil
}

func firstElement(nodes []*html.Node) *html.Node {
	for _, n := range nodes {
		if n.Type == html.ElementNode {
			return n
		}
	}
	return nil
}

func renderNodes(nodes []*html.Node) string {
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(dom.OuterHTML(n))
	}
	return b.String()
}

// docVersion is the stored document's version (transient writes leave it
// unchanged), used in element tags.
func docVersion(w *engine.WriteCtx) int64 {
	if w.Doc != nil {
		return w.Doc.Version
	}
	return 0
}
