package engine

import (
	"context"
	"net/http"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/sse"
)

// move implements the MOVE method (PageLove docs, MOVE method;
// docs/spec/protocol.md §9, reconciled with live PageLove 2026-09-28).
// Conditions are checked in the order of the spec's error table
// (R-PROTO-99): reserved namespace → incomplete requests (422
// MoveMissingHeaders) → cross-document element moves (501) → an unusable
// destination selector (422) → the document-level MOVE grant → missing
// document (404) → selector ranges on a blob (422) → absent source (416;
// an unparsable source selector matches nothing) or anchor (404), reported
// only to actors who may read the page and hold the DELETE (source) or
// POST (anchor) grant on the whole document, refused otherwise → the
// DELETE and POST element checks → If-Match against the source element's
// tag (412) → illegal moves and validation (422).
func (e *Engine) move(ctx context.Context, w *WriteCtx) (*Result, error) {
	op := w.Op
	if op.Destination == "" {
		return nil, errdoc.Problems(http.StatusUnprocessableEntity, "MoveMissingHeaders", "MOVE requires a Destination header")
	}
	dest, err := NormalizePath(op.Destination)
	if err != nil {
		return nil, err
	}
	if op.Plane == Public && strings.HasPrefix(dest, ReservedPrefix) {
		return nil, reserved()
	}
	hasSrc, hasDst := op.Range.Present(), op.DestinationRange.Present()
	if !hasSrc && !hasDst {
		return e.moveDocument(ctx, w, dest)
	}
	incomplete := errdoc.Problems(http.StatusUnprocessableEntity, "MoveMissingHeaders", "MOVE requires Range, Destination, and Destination-Range with placement")
	if hasSrc != hasDst || !op.Range.HasSelector() || !op.DestinationRange.HasSelector() ||
		op.Range.Selector == "" || op.DestinationRange.Selector == "" {
		return nil, incomplete
	}
	placement := op.DestinationRange.Placement
	if !ValidPlacement(placement) {
		return nil, incomplete
	}
	if DocPath(dest) != op.Path {
		return nil, errdoc.Problems(http.StatusNotImplemented, "MoveCrossResource", "Cross-resource MOVE is not supported")
	}
	dstSel, err := op.DestinationRange.CheckSelector()
	if err != nil {
		return nil, errdoc.Problems(http.StatusUnprocessableEntity, "InvalidPath", "Invalid destination selector: invalid selector '%s': %s", op.DestinationRange.Selector, err.(*errdoc.Error).Message)
	}
	// An unparsable source selector matches nothing (live: 416).
	srcSel, _ := op.Range.CheckSelector()
	// Check 1: MOVE on the document (never covered by default-GET).
	if err := e.authorize(w, "MOVE", nil); err != nil {
		return nil, err
	}
	if w.Doc == nil {
		return nil, missingDocument(op.Path)
	}
	if w.Before == nil || (op.Plane == Public && !htmlDocument(w.Doc)) {
		return nil, notHTML(w.Doc)
	}
	var src *html.Node
	if srcSel != nil {
		src = srcSel.MatchFirst(w.Before)
	}
	anchor := dstSel.MatchFirst(w.Before)
	if src == nil || anchor == nil {
		// Absence is reported only to actors who may read the page (fail
		// closed, kept although live PageLove answers 416 there; decisions.md
		// protocol.move-authz.fail-closed-when-unreadable) and who hold the
		// element check's grant on the whole document (live).
		check := "DELETE"
		if src != nil {
			check = "POST"
		}
		if op.Plane == Public && (!e.canRead(w) || !e.docGrants(w, check)) {
			return nil, Denied(op.Principal, op.Path)
		}
		if src == nil {
			return nil, errdoc.NoMatch(op.Range.Selector)
		}
		return nil, errdoc.Problems(http.StatusNotFound, "NotFound", "Destination element not found: %s", op.DestinationRange.Selector)
	}
	// Check 2: DELETE on the source element.
	if err := e.authorize(w, "DELETE", src); err != nil {
		return nil, err
	}
	// Check 3: POST on the element whose child list changes.
	sibling := placement == "before" || placement == "after"
	container := anchor
	if sibling {
		container = anchor.Parent
		if container == nil || container.Type != html.ElementNode {
			return nil, errdoc.Problems(http.StatusUnprocessableEntity, "InvalidPath", "Invalid MOVE: placement=%s relative to the document root is not possible", placement)
		}
	}
	if err := e.authorize(w, "POST", container); err != nil {
		return nil, err
	}
	// Conditioned on the source element's tag only (live 2026-09-28).
	if err := e.checkPreconditions(w, w.currentTag(src)); err != nil {
		return nil, err
	}
	switch {
	case src.Parent == nil || src.Parent.Type != html.ElementNode:
		return nil, errdoc.Problems(http.StatusUnprocessableEntity, "InvalidPath", "Invalid MOVE: the document root cannot be moved")
	case src == anchor && sibling:
		// Before or after itself: nothing changes and nothing is written.
		res := &Result{Status: http.StatusNoContent, Header: http.Header{}}
		res.Header.Set("ETag", w.Doc.ETag)
		writeHeaders(res.Header, w.Doc)
		return res, nil
	case dom.IsAncestor(src, anchor):
		return nil, errdoc.Problems(http.StatusUnprocessableEntity, "InvalidPath", "Invalid MOVE: destination is inside the source element's subtree")
	}
	w.Target = src
	if err := e.runBefore(ctx, w); err != nil {
		return nil, err
	}
	after := dom.Clone(w.Before)
	s2 := sameNode(w.Before, after, src)
	a2 := sameNode(w.Before, after, anchor)
	s2.Parent.RemoveChild(s2)
	switch placement {
	case "append":
		a2.AppendChild(s2)
	case "prepend":
		a2.InsertBefore(s2, a2.FirstChild)
	case "before":
		a2.Parent.InsertBefore(s2, a2)
	case "after":
		a2.Parent.InsertBefore(s2, a2.NextSibling)
	}
	w.After, w.Target = after, s2
	if err := e.validate(ctx, w); err != nil {
		return nil, err
	}
	// Splice: cut the source element's bytes and write them at the
	// destination (then its serialization, for elements whose source text
	// does not stand alone, e.g. an omitted end tag).
	var cands [][]dom.Edit
	texts := []string{dom.StorageHTML(s2)}
	if sp, ok := w.sourceSpans().By[src]; ok {
		texts = append([]string{string(w.Doc.Body[sp.Start:sp.End])}, texts...)
	}
	for _, text := range texts {
		cands = append(cands, []dom.Edit{{Node: src, Placement: dom.PlaceReplace}, {Node: anchor, Placement: dom.Placement(placement), Text: text}})
	}
	stored, err := e.store(w, after, cands...)
	if err != nil {
		return nil, err
	}
	res := &Result{Status: http.StatusNoContent, Header: http.Header{}}
	res.Header.Set("ETag", stored.ETag)
	writeHeaders(res.Header, stored)
	m := sse.Mutation{Method: "MOVE", Selector: op.Range.Selector, ETag: eventTag(stored), Body: servedAfter(stored, after)(s2),
		Placement: placement, Destination: op.DestinationRange.Selector}
	if err := e.event(w, op.Path, m, s2); err != nil {
		return nil, err
	}
	return e.after(ctx, w, res)
}

// moveDocument relocates a whole document (R-PROTO-98). It needs a MOVE
// grant on both paths and nothing else; the destination is created or,
// unless Overwrite: F, replaced. Subscribers of the old path see the
// document deleted and those of the new path see it written.
func (e *Engine) moveDocument(ctx context.Context, w *WriteCtx, dest string) (*Result, error) {
	op := w.Op
	if strings.HasSuffix(op.Path, "/") || strings.HasSuffix(dest, "/") {
		// Whole-document writes address literal document paths (R-RW-63).
		return nil, errdoc.New(http.StatusBadRequest, "BadPath", "a whole-document MOVE needs document paths, not directories")
	}
	if op.Plane == Public {
		for _, p := range []string{op.Path, dest} {
			req := w.authzReq("MOVE")
			req.Path = p
			if !w.Snap.Policy.Decide(req, nil).Allowed {
				// The refusal names the request path, whichever path
				// lacked the grant (live 2026-09-28).
				return nil, Denied(op.Principal, op.Path)
			}
		}
	}
	if w.Doc == nil {
		// Live PageLove answers 500 (an Internal problems item saying the
		// document was not found); pagelike keeps the 404
		// (decisions.md protocol.move.whole-document-missing-source).
		return nil, missingDocument(op.Path)
	}
	if dest == op.Path {
		return nil, errdoc.New(http.StatusForbidden, "SameDestination", "the Destination is the document itself")
	}
	if err := e.checkPreconditions(w, w.Doc.ETag); err != nil {
		return nil, err
	}
	prev, err := w.Tx.Get(dest)
	if err == nil && prev != nil && strings.EqualFold(strings.TrimSpace(op.Overwrite), "F") {
		return nil, errdoc.New(http.StatusPreconditionFailed, "DestinationExists", "%s exists and Overwrite is F", dest)
	}
	if err := e.runBefore(ctx, w); err != nil {
		return nil, err
	}
	if err := e.validate(ctx, w); err != nil {
		return nil, err
	}
	nd := *w.Doc
	nd.Path = dest
	stored, err := e.put(w, &nd)
	if err != nil {
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
	if err := e.event(w, op.Path, sse.Mutation{Method: "DELETE"}); err != nil {
		return nil, err
	}
	m := sse.Mutation{Method: "PUT", ETag: eventTag(stored)}
	if !stored.IsBlob() {
		m.Body = string(stored.Body)
	}
	if err := e.event(w, dest, m); err != nil {
		return nil, err
	}
	res := &Result{Status: http.StatusNoContent, Header: http.Header{}}
	res.Header.Set("ETag", stored.ETag)
	writeHeaders(res.Header, stored)
	return e.after(ctx, w, res)
}
