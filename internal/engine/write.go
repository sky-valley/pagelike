package engine

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/sse"
	"github.com/sky-valley/pagelike/internal/store"
)

// TemplateCreate handles POST without a selector to an existing document
// (templated resource creation). It is supplied by the composition layer;
// without it such a POST is answered 501.
var TemplateCreate func(ctx context.Context, e *Engine, w *WriteCtx) (*Result, error)

// rangeNotSatisfiable is the read path's 416 for a selector that matches
// nothing, worded like PageLove's (live observation 2026-09-28).
func rangeNotSatisfiable(sel string) *errdoc.Error {
	return errdoc.Read(http.StatusRequestedRangeNotSatisfiable, "RangeNotSatisfiable", "HTML parsing error: No elements matched selector: %s", sel)
}

// authzNoMatch is the 416 PageLove's authorization layer answers when a
// write's grant hangs on selector-scoped rules and the selector matches
// nothing: the read path's document, served with a charset, no Vary and
// Content-Range "selector */" (live 2026-09-28).
func authzNoMatch(sel string) *errdoc.Error {
	e := rangeNotSatisfiable(sel)
	e.Charset, e.NoVary = true, true
	e.WithHeader("Content-Range", "selector */")
	return e
}

// missingDocument is the write path's 404 for a selector write (or MOVE)
// whose document does not exist.
func missingDocument(p string) *errdoc.Error {
	return errdoc.Problems(http.StatusNotFound, "NotFound", "document not found at request path: %s", p)
}

// notHTML refuses selector operations on anything but an HTML document:
// PageLove treats XML documents like blobs here (live 2026-09-28,
// superseding R-RW-39's XML selectors).
func notHTML(d *store.Document) *errdoc.Error {
	return errdoc.Problems(http.StatusUnprocessableEntity, "InvalidPath", "Selector operations require HTML documents, but %s has content type %s", d.Path, d.ContentType)
}

// htmlDocument reports whether selector operations apply to d.
func htmlDocument(d *store.Document) bool {
	return d != nil && !d.IsBlob() && site.IsMarkup(d.ContentType) && !site.IsXML(d.ContentType)
}

// resolve locates the target of a selector write, implementing the
// absence-versus-denial rules (docs/spec/reading-writing.md R-RW-126/127,
// as reconciled with live PageLove): an actor that no rule could grant the
// method is refused whether or not anything exists; a missing document is
// 404; a blob or XML document is 422; a selector matching nothing is
// refused for actors who may not read the page, and otherwise 416 — the
// authorization layer's 416 when the method is granted only by
// selector-scoped rules, the write path's selector-no-match otherwise.
// POST before/after with an absent anchor is no exception (live).
func (e *Engine) resolve(w *WriteCtx, method string) (*html.Node, error) {
	op := w.Op
	if err := e.authorizeAny(w, method); err != nil {
		return nil, err
	}
	if w.Doc == nil {
		return nil, missingDocument(op.Path)
	}
	if w.Before == nil || (op.Plane == Public && !htmlDocument(w.Doc)) {
		return nil, notHTML(w.Doc)
	}
	if t, ok, err := w.located(op.Range.Selector); ok {
		return t, err // resolved in the composed view (compose_hooks.go)
	}
	sel, err := op.Range.CheckSelector()
	if err != nil {
		return nil, err
	}
	target := sel.MatchFirst(w.Before)
	if target == nil {
		if op.Plane == Public {
			if !e.canRead(w) {
				return nil, Denied(op.Principal, op.Path)
			}
			if !e.docGrants(w, method) {
				return nil, authzNoMatch(op.Range.Selector)
			}
		}
		return nil, errdoc.NoMatch(op.Range.Selector)
	}
	return target, nil
}

// docGrants reports whether the method is granted on the whole document
// (not only on elements named by selector-scoped rules).
func (e *Engine) docGrants(w *WriteCtx, method string) bool {
	return w.Op.Plane == Authoring || w.Snap.Policy.Decide(w.authzReq(method), nil).Allowed
}

func (e *Engine) putDocument(ctx context.Context, w *WriteCtx) (*Result, error) {
	op := w.Op
	if err := e.authorize(w, "PUT", nil); err != nil {
		return nil, err
	}
	cur := ""
	if w.Doc != nil {
		cur = w.Doc.ETag
	}
	if err := e.checkPreconditions(w, cur); err != nil {
		return nil, err
	}
	ct := ResolveContentType(op.Path, op.ContentType)
	markup := site.IsMarkup(ct)
	if markup {
		w.After, _ = site.ParseMarkup(ct, op.Body)
	}
	if err := e.runBefore(ctx, w); err != nil {
		return nil, err
	}
	nd := &store.Document{Path: op.Path, ContentType: ct}
	if markup {
		// Triggers may have rewritten the body.
		nd.Body = op.Body
		w.After, _ = site.ParseMarkup(ct, op.Body)
	} else {
		sha, size, err := w.Site.Store.WriteBlob(bytes.NewReader(op.Body))
		if err != nil {
			return nil, err
		}
		nd.BlobSHA, nd.Size = sha, size
	}
	if err := e.validate(ctx, w); err != nil {
		return nil, err
	}
	if markup {
		// Validate hooks may rewrite the body too (schema defaults and @write
		// resolvers; see schema_hooks.go).
		nd.Body = storedForm(ct, op.Body)
	}
	stored, err := e.put(w, nd)
	if err != nil {
		return nil, err
	}
	// 201 on create, 200 on replace; the stored representation is echoed
	// with its type, blobs included (R-RW-61/62, live 2026-09-28).
	res := &Result{Status: http.StatusOK, Header: http.Header{}}
	if w.Doc == nil {
		res.Status = http.StatusCreated
	}
	res.Header.Set("ETag", stored.ETag)
	writeHeaders(res.Header, stored)
	res.Header.Set("Content-Type", ct)
	m := sse.Mutation{Method: "PUT", ETag: eventTag(stored)}
	if markup {
		res.Body = stored.Body
		m.Body = string(stored.Body) // the whole document (live)
	} else {
		res.Body = op.Body
	}
	if err := e.event(w, op.Path, m); err != nil {
		return nil, err
	}
	return e.after(ctx, w, res)
}

func (e *Engine) putSelector(ctx context.Context, w *WriteCtx) (*Result, error) {
	op := w.Op
	target, err := e.resolve(w, "PUT")
	if err != nil {
		return nil, err
	}
	if err := e.authorize(w, "PUT", target); err != nil {
		return nil, err
	}
	if err := e.checkPreconditions(w, w.currentTag(target)); err != nil {
		return nil, err
	}
	w.Target = target
	if err := e.runBefore(ctx, w); err != nil {
		return nil, err
	}
	after := dom.Clone(w.Before)
	tgt := sameNode(w.Before, after, target)
	parent := tgt.Parent
	var nodes []*html.Node
	var stored *store.Document
	if parent == nil || parent.Type != html.ElementNode && soleElement(parent, tgt) {
		// Replacing the root element: the body is a whole document, stored
		// like a whole-document PUT. (A PageLove document may have several
		// top-level elements, LO-15; any other one is replaced in place.)
		d, err := site.ParseMarkup(w.Doc.ContentType, op.Body)
		if err != nil {
			return nil, errdoc.New(http.StatusBadRequest, "BadBody", "cannot parse body: %v", err)
		}
		after = d
		if root := dom.DocumentElement(d); root != nil {
			nodes = []*html.Node{root}
		}
		w.After, w.Inserted, w.Target = after, nodes, firstElement(nodes)
		if err := e.validate(ctx, w); err != nil {
			return nil, err
		}
		if stored, err = e.put(w, &store.Document{Path: op.Path, ContentType: w.Doc.ContentType, Body: storedForm(w.Doc.ContentType, op.Body)}); err != nil {
			return nil, err
		}
	} else {
		nodes, err = dom.ParseFragment(string(op.Body), parent)
		if err != nil {
			return nil, errdoc.New(http.StatusBadRequest, "BadBody", "cannot parse body: %v", err)
		}
		if parent.Parent != nil && parent.Parent.Type == html.DocumentNode {
			// In the root element's context the parser always yields a head
			// and a body; replacing <body> must not add an empty implied
			// <head> next to the existing one (decision 0003 §1).
			nodes = dom.DropEmptyImplied(nodes, string(op.Body))
		}
		fallback := containerOf(w, after, target.Parent)
		dom.Replace(tgt, nodes)
		w.After, w.Inserted = after, nodes
		w.Target = firstElement(nodes)
		if err := e.validate(ctx, w); err != nil {
			return nil, err
		}
		var cands [][]dom.Edit
		for _, text := range insertTexts(dom.TrimHTMLSpace(string(op.Body)), nodes) {
			cands = append(cands, []dom.Edit{{Node: target, Placement: dom.PlaceReplace, Text: text}})
		}
		if stored, err = e.store(w, after, append(cands, fallback.edit())...); err != nil {
			return nil, err
		}
	}
	// The response is the new element as a read now serves it, tagged like
	// one (R-RW-58, live 2026-09-28).
	served := servedAfter(stored, after)
	frag := servedNodes(nodes, served)
	etag := FragmentETag(frag, stored.Version)
	if el := firstElement(nodes); el != nil {
		etag = FragmentETag(served(el), stored.Version)
		if sel, err := op.Range.CheckSelector(); err == nil && !sel.Matches(el) {
			// A replacement the request selector no longer matches is
			// tagged with the document's new tag (live 2026-09-28).
			etag = stored.ETag
		}
	}
	res := &Result{Status: http.StatusPartialContent, Header: http.Header{}, Body: []byte(frag)}
	res.Header.Set("Content-Range", "selector "+op.Range.Selector)
	res.Header.Set("Content-Type", markupType(w.Doc.ContentType))
	res.Header.Set("ETag", etag)
	writeHeaders(res.Header, stored)
	if err := e.event(w, op.Path, sse.Mutation{Method: "PUT", Selector: op.Range.Selector, ETag: eventTag(stored), Body: frag}, dom.Elements(nodes)...); err != nil {
		return nil, err
	}
	return e.after(ctx, w, res)
}

func (e *Engine) postSelector(ctx context.Context, w *WriteCtx) (*Result, error) {
	op := w.Op
	placement := op.Range.Placement
	if !ValidPlacement(placement) {
		// Absent or unknown: an append (live 2026-09-28, R-RW-71).
		placement = "append"
	}
	sibling := placement == "before" || placement == "after"
	target, err := e.resolve(w, "POST")
	if err != nil {
		return nil, err
	}
	// The element whose child list changes: the anchor itself, or its
	// parent for sibling insertion (which a parentless anchor lacks).
	scope := target
	if sibling {
		scope = target.Parent
		if scope == nil || scope.Type != html.ElementNode {
			err := errdoc.Problems(http.StatusUnprocessableEntity, "NoSiblingSlot",
				"placement=%s relative to the document root is not possible: the root has no siblings", strings.ToUpper(placement[:1])+placement[1:])
			err.ItemType = "https://pagelove.org/Error"
			return nil, err
		}
	}
	if err := e.authorize(w, "POST", scope); err != nil {
		return nil, err
	}
	// Conditioned on the anchor's tag (live 2026-09-28).
	if err := e.checkPreconditions(w, w.currentTag(target)); err != nil {
		return nil, err
	}
	w.Target = target
	if err := e.runBefore(ctx, w); err != nil {
		return nil, err
	}
	after := dom.Clone(w.Before)
	tgt := sameNode(w.Before, after, target)
	ctxNode := sameNode(w.Before, after, scope)
	nodes, err := parseInsertion(string(op.Body), ctxNode)
	if err != nil {
		return nil, errdoc.New(http.StatusBadRequest, "BadBody", "cannot parse body: %v", err)
	}
	fallback := containerOf(w, after, scope)
	switch placement {
	case "append":
		dom.Append(tgt, nodes)
	case "prepend":
		dom.Prepend(tgt, nodes)
	case "before":
		dom.InsertBefore(tgt, nodes)
	case "after":
		dom.InsertAfter(tgt, nodes)
	}
	w.After, w.Inserted, w.Target = after, nodes, tgt
	if err := e.validate(ctx, w); err != nil {
		return nil, err
	}
	var cands [][]dom.Edit
	for _, text := range insertTexts(string(op.Body), nodes) {
		cands = append(cands, []dom.Edit{{Node: target, Placement: dom.Placement(placement), Text: text}})
	}
	stored, err := e.store(w, after, append(cands, fallback.edit())...)
	if err != nil {
		return nil, err
	}
	// The response carries the inserted content without the surrounding
	// whitespace (clients parse it as one node), tagged as the inserted
	// child, not the anchor (live 2026-09-28, superseding R-RW-73).
	inserted := trimSpaceNodes(nodes)
	frag := servedNodes(inserted, servedAfter(stored, after))
	res := &Result{Status: http.StatusPartialContent, Header: http.Header{}, Body: []byte(frag)}
	res.Header.Set("Content-Range", "selector "+op.Range.Selector)
	res.Header.Set("Content-Type", markupType(w.Doc.ContentType))
	res.Header.Set("ETag", FragmentETag(frag, stored.Version))
	writeHeaders(res.Header, stored)
	m := sse.Mutation{Method: "POST", Selector: op.Range.Selector, Body: frag, Placement: placement, ETag: eventTag(stored)}
	if err := e.event(w, op.Path, m, dom.Elements(inserted)...); err != nil {
		return nil, err
	}
	return e.after(ctx, w, res)
}

// parseInsertion parses a POST body in the context of the element that
// becomes its parent. Unlike a replacement, every node of the body is
// inserted, including the whitespace around it (the docs' getting-started
// read-back shows the request's trailing newline in the stored list).
func parseInsertion(body string, parent *html.Node) ([]*html.Node, error) {
	nodes, err := dom.ParseFragment(body, parent)
	if err != nil {
		return nil, err
	}
	if parent.Parent != nil && parent.Parent.Type == html.DocumentNode {
		// Children of the root element: the parser synthesizes head/body
		// and moves whitespace around, so keep just what the markup names.
		return dom.DropEmptyImplied(nodes, body), nil
	}
	trimmed := dom.TrimHTMLSpace(body)
	if trimmed == "" {
		return nodes, nil
	}
	i := strings.Index(body, trimmed)
	lead, trail := body[:i], body[i+len(trimmed):]
	if lead != "" {
		nodes = append([]*html.Node{{Type: html.TextNode, Data: lead}}, nodes...)
	}
	if trail != "" {
		nodes = append(nodes, &html.Node{Type: html.TextNode, Data: trail})
	}
	return nodes, nil
}

// trimSpaceNodes drops whitespace-only text nodes at both ends.
func trimSpaceNodes(nodes []*html.Node) []*html.Node {
	blank := func(n *html.Node) bool { return n.Type == html.TextNode && dom.TrimHTMLSpace(n.Data) == "" }
	for len(nodes) > 0 && blank(nodes[0]) {
		nodes = nodes[1:]
	}
	for len(nodes) > 0 && blank(nodes[len(nodes)-1]) {
		nodes = nodes[:len(nodes)-1]
	}
	return nodes
}

// singleElement returns the only node of nodes when it is an element.
func singleElement(nodes []*html.Node) *html.Node {
	if len(nodes) == 1 && nodes[0].Type == html.ElementNode {
		return nodes[0]
	}
	return nil
}

func (e *Engine) deleteSelector(ctx context.Context, w *WriteCtx) (*Result, error) {
	op := w.Op
	target, err := e.resolve(w, "DELETE")
	if err != nil {
		return nil, err
	}
	if err := e.authorize(w, "DELETE", target); err != nil {
		return nil, err
	}
	if err := e.checkPreconditions(w, w.currentTag(target)); err != nil {
		return nil, err
	}
	if target.Parent == nil || target.Parent.Type != html.ElementNode {
		return nil, errdoc.New(http.StatusUnprocessableEntity, "CannotDeleteRoot", "the document root cannot be deleted by selector")
	}
	w.Target = target
	if err := e.runBefore(ctx, w); err != nil {
		return nil, err
	}
	after := dom.Clone(w.Before)
	tgt := sameNode(w.Before, after, target)
	fallback := containerOf(w, after, target.Parent)
	tgt.Parent.RemoveChild(tgt)
	w.After = after
	if err := e.validate(ctx, w); err != nil {
		return nil, err
	}
	stored, err := e.store(w, after, []dom.Edit{{Node: target, Placement: dom.PlaceReplace}}, fallback.edit())
	if err != nil {
		return nil, err
	}
	// No body and no Content-Range; the document's new stored-version tag
	// (R-RW-99; live 2026-09-28, superseding the documented Content-Range).
	res := &Result{Status: http.StatusNoContent, Header: http.Header{}}
	res.Header.Set("ETag", stored.ETag)
	writeHeaders(res.Header, stored)
	if err := e.event(w, op.Path, sse.Mutation{Method: "DELETE", Selector: op.Range.Selector, ETag: eventTag(stored)}, target); err != nil {
		return nil, err
	}
	return e.after(ctx, w, res)
}

// deleted is the answer to a whole-document DELETE: 204 with the tag of
// empty content (live 2026-09-28).
func deleted() *Result {
	res := &Result{Status: http.StatusNoContent, Header: http.Header{}}
	res.Header.Set("ETag", EmptyETag)
	res.Header.Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	return res
}

func (e *Engine) deleteDocument(ctx context.Context, w *WriteCtx) (*Result, error) {
	op := w.Op
	if err := e.authorize(w, "DELETE", nil); err != nil {
		return nil, err
	}
	cur := ""
	if w.Doc != nil {
		cur = w.Doc.ETag
	}
	if err := e.checkPreconditions(w, cur); err != nil {
		return nil, err
	}
	if w.Doc == nil {
		// Deleting a path that holds nothing succeeds and changes nothing
		// (live 2026-09-28, superseding R-RW-80's 404).
		return deleted(), nil
	}
	if err := e.runBefore(ctx, w); err != nil {
		return nil, err
	}
	if err := e.validate(ctx, w); err != nil {
		return nil, err
	}
	if err := e.remove(w, op.Path); err != nil {
		return nil, err
	}
	if op.Plane == Authoring {
		if err := w.Tx.DeleteAuthored(op.Path); err != nil {
			return nil, err
		}
	}
	res := deleted()
	if err := e.event(w, op.Path, sse.Mutation{Method: "DELETE"}); err != nil {
		return nil, err
	}
	return e.after(ctx, w, res)
}

// postCreate handles POST without a selector to a document: templated
// resource creation, supplied by the composition layer (docs, Composing
// pages → Resource Creation). POST to a directory is refused earlier.
func (e *Engine) postCreate(ctx context.Context, w *WriteCtx) (*Result, error) {
	op := w.Op
	if err := e.authorizeAny(w, "POST"); err != nil {
		return nil, err
	}
	if w.Doc == nil {
		return nil, missingDocument(op.Path)
	}
	if TemplateCreate != nil && site.IsMarkup(w.Doc.ContentType) && !w.Doc.IsBlob() {
		return TemplateCreate(ctx, e, w)
	}
	return nil, errdoc.New(http.StatusNotImplemented, "NotImplemented", "POST without a selector range (resource creation) is not implemented for %s", op.Path)
}

// store persists the result of a selector write. PageLove serves the bytes
// it stored (live observation LO-1), so rather than re-serializing the whole
// document, the change is spliced into the stored source: each candidate is
// a set of edits anchored to elements of w.Before, tried in order, and the
// first whose re-parse equals the DOM-mutation result (after) is kept, so
// every untouched byte survives verbatim. When correlation with the source
// is not clean or no candidate verifies, the document is re-serialized
// (htmlser, lossless storage variant; decision 0003 §6).
func (e *Engine) store(w *WriteCtx, after *html.Node, cands ...[]dom.Edit) (*store.Document, error) {
	nd := &store.Document{Path: w.Op.Path, ContentType: w.Doc.ContentType, Body: e.editSource(w, after, cands)}
	return e.put(w, nd)
}

func (e *Engine) editSource(w *WriteCtx, after *html.Node, cands [][]dom.Edit) []byte {
	src, ct := w.Doc.Body, w.Doc.ContentType
	want := dom.RenderStorage(after, nil)
	if !site.IsXML(ct) {
		// PageLove stores every HTML write in its serialized form (LO-15).
		return want
	}
	for _, edits := range cands {
		if len(edits) == 0 {
			continue
		}
		out, err := dom.Splice(src, w.sourceSpans(), edits...)
		if err != nil {
			continue
		}
		got, err := site.ParseMarkup(ct, out)
		if err == nil && bytes.Equal(dom.RenderStorage(got, nil), want) {
			return out
		}
	}
	if site.IsXML(ct) {
		return want
	}
	return dom.RenderStorage(after, dom.ImpliedFor(after, string(src)))
}

// sourceSpans correlates w.Before with the stored source it was parsed from.
func (w *WriteCtx) sourceSpans() dom.Spans {
	if w.spans == nil {
		sp := dom.ComputeSpans(w.Before, string(w.Doc.Body))
		w.spans = &sp
	}
	return *w.spans
}

// insertTexts returns the texts to try when splicing parsed request markup
// into stored source: the markup as sent, then the storage serialization of
// the nodes it parsed to (for bodies that parse differently in place, e.g.
// unclosed elements before siblings).
func insertTexts(raw string, nodes []*html.Node) []string {
	var b strings.Builder
	for _, n := range nodes {
		if n.Type == html.TextNode && dom.TrimHTMLSpace(n.Data) == "" {
			b.WriteString(n.Data) // whitespace kept as sent
			continue
		}
		b.WriteString(dom.StorageHTML(n))
	}
	if ser := b.String(); ser != raw {
		return []string{raw, ser}
	}
	return []string{raw}
}

// soleElement reports whether el is parent's only element child.
func soleElement(parent, el *html.Node) bool {
	for c := parent.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c != el {
			return false
		}
	}
	return true
}

// storedForm is how a whole-document write of markup is stored: HTML in
// PageLove's serialized form (live 2026-09-29, LO-15: both planes store and
// echo it), XML as sent.
func storedForm(ct string, body []byte) []byte {
	if site.IsXML(ct) {
		return body
	}
	doc, err := dom.Parse(body)
	if err != nil {
		return body
	}
	return dom.RenderStorage(doc, nil)
}

// container pairs an element of w.Before with its counterpart in the tree
// being mutated. It is located before the mutation, while child paths in
// that tree still match w.Before.
type container struct{ before, after *html.Node }

func containerOf(w *WriteCtx, after, n *html.Node) container {
	if n == nil || n.Type != html.ElementNode {
		return container{}
	}
	return container{n, sameNode(w.Before, after, n)}
}

// edit is the last splice candidate for a write inside the container:
// replace the container's bytes with its mutated serialization, which keeps
// every byte outside it. Call it after the mutation (and validation).
func (c container) edit() []dom.Edit {
	if c.before == nil {
		return nil
	}
	return []dom.Edit{{Node: c.before, Placement: dom.PlaceReplace, Text: dom.StorageHTML(c.after)}}
}
