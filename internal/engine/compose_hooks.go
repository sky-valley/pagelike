package engine

import (
	"context"
	"net/http"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/site"
)

// Hooks for page composition (package internal/compose; docs/spec/composing.md
// §13 write-through, §14 transient elements, §15 routes).
//
// A public selector write (PUT/POST/DELETE with Range: selector=) is
// resolved against the composed view of the page it addresses, not against
// the stored markup (docs/spec/reading-writing.md R-RW-16):
//
//   - a target that is a native element of the stored document is written
//     as usual, but the element is the one the composed view matched
//     (WriteCtx.Locate);
//   - a target projected by an include or a stamp is written in its origin
//     document (ApplyRouted), authorized against the page;
//   - a target inside a transient element is written to the requester's
//     session and produces no event;
//   - a generated target (template output, method results) is refused.
//
// ComposeWrite is consulted by apply for every public, non-routed selector
// write, after the stored document (if any) has been loaded and parsed. It
// returns (nil, nil) to let the ordinary write proceed, a result when it
// performed the write itself, or an error.
var ComposeWrite func(ctx context.Context, e *Engine, w *WriteCtx) (*Result, error)

// composeWrite runs the ComposeWrite hook when it applies to w.
func (e *Engine) composeWrite(ctx context.Context, w *WriteCtx) (*Result, bool, error) {
	op := w.Op
	if ComposeWrite == nil || op.Plane != Public || w.sideEffect || w.preauth || !op.Range.HasSelector() {
		return nil, false, nil
	}
	switch op.Method {
	case "PUT", "POST", "DELETE":
	default:
		return nil, false, nil
	}
	res, err := ComposeWrite(ctx, e, w)
	if err != nil || res != nil {
		return res, true, err
	}
	return nil, false, nil
}

// Locate makes the write target the element locate returns from the
// freshly parsed stored document (WriteCtx.Before) instead of the first
// match of the request selector in the stored markup. The composition layer
// uses it when the selector was resolved against the composed view. A nil
// result is answered 416.
func (w *WriteCtx) Locate(locate func(before *html.Node) *html.Node) { w.locate = locate }

// located returns the pre-resolved target, if Locate was used.
func (w *WriteCtx) located(sel string) (*html.Node, bool, error) {
	if w.locate == nil {
		return nil, false, nil
	}
	if t := w.locate(w.Before); t != nil {
		return t, true, nil
	}
	return nil, true, rangeNotSatisfiable(sel)
}

// ApplyRouted performs op, a selector write on an origin document, inside
// w's transaction on behalf of the composed page w addresses (write-through,
// docs/spec/composing.md R-COMP-91..95). The caller has already authorized
// the write against the page (R-COMP-92): the origin's own rules are not
// consulted. locate picks the target in the origin's freshly parsed tree.
// Triggers, validation, preconditions (If-Match against the origin
// element) and the mutation event all concern the origin document.
func (e *Engine) ApplyRouted(ctx context.Context, w *WriteCtx, op *Op, locate func(before *html.Node) *html.Node) (*Result, error) {
	if err := checkRequest(op); err != nil {
		return nil, err
	}
	inner := &WriteCtx{Site: w.Site, Snap: w.Snap, Tx: w.Tx, Op: op, Engine: e, Via: w.Op.Path, ViaSelector: w.Op.Range.Selector, preauth: true, locate: locate}
	return e.apply(ctx, inner)
}

// Refuse returns the refusal an actor gets for a write it may not make on
// the page: 401 for anonymous principals, 403 otherwise.
func (w *WriteCtx) Refuse() *errdoc.Error { return Denied(w.Op.Principal, w.Op.Path) }

// RangeNotSatisfiable is the 416 a selector write gets when its target does
// not exist or cannot be written (generated content, non-projected elements
// of a route page).
func RangeNotSatisfiable(sel string) *errdoc.Error { return rangeNotSatisfiable(sel) }

// NoSession is the 409 of a transient write without an established session.
func NoSession() *errdoc.Error {
	return errdoc.New(http.StatusConflict, "NoSession", "a transient element can only be written within a session")
}

// SelectorFunctions expands the selector functions of a Range selector
// (count(), text-of(), value-of(), attr-of(); docs/spec/composing.md
// R-COMP-178), which are evaluated over the whole site before the selector
// is parsed. Supplied by the composition layer; nil leaves them
// unsupported (the selector then does not parse: 422).
var SelectorFunctions func(ctx context.Context, s *site.Site, snap *site.Snapshot, src string) (string, error)

// ExpandRange returns r with its selector functions expanded over snap.
// Content-Range keeps echoing the selector as sent; an expansion failure
// (a single-value function matching several elements) makes the selector
// invalid (422).
func ExpandRange(ctx context.Context, s *site.Site, snap *site.Snapshot, r Range) Range {
	r = r.BindSnapshot(snap)
	if SelectorFunctions == nil || !r.HasSelector() || r.Selector == "" {
		return r
	}
	out, err := SelectorFunctions(ctx, s, snap, r.Selector)
	if err != nil {
		r.expandErr = err
		return r
	}
	if out != r.Selector {
		r.expanded = out
	}
	return r
}
