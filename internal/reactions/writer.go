package reactions

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

// writer is the write provider of trigger and processor Sessel (the
// Pagelove platform interface, R-REACT-36..40).
type writer struct {
	x   *Reactions
	st  *reqState
	env *evalEnv
}

// Put implements Pagelove.PUT(item, path). In the trigger phase of a PUT
// or POST, a path equal to Context.request.path replaces the in-flight
// request body (R-REACT-37); otherwise the item is stored as a whole
// document through the serving-path pipeline, committed before the call
// returns (R-REACT-36).
func (wr *writer) Put(ctx context.Context, item *sessel.Element, path string) error {
	st := wr.st
	st.mu.Lock()
	phase := st.phase
	st.mu.Unlock()
	if phase == phaseTriggers && (st.method == http.MethodPut || st.method == http.MethodPost) && path == st.path {
		body := serialize(item, !st.hasSelector, wr.setKeyID)
		st.mu.Lock()
		st.body, st.transformed = []byte(body), true
		st.mu.Unlock()
		st.req.Set("body", body) // later triggers see it (R-REACT-38)
		return nil
	}
	p, err := engine.NormalizePath(path)
	if err != nil {
		return writeError("Pagelove.PUT", path, err)
	}
	op := wr.op(http.MethodPut, p)
	op.Body = []byte(serialize(item, true, wr.setKeyID))
	op.ContentType = "text/html"
	return wr.run(ctx, "Pagelove.PUT", op)
}

// Delete implements Pagelove.DELETE: a path deletes the document; an
// element of a stored document is deleted from it by its canonical
// selector (R-REACT-39).
func (wr *writer) Delete(ctx context.Context, target sessel.Value) error {
	switch t := target.(type) {
	case string:
		p, err := engine.NormalizePath(t)
		if err != nil {
			return writeError("Pagelove.DELETE", t, err)
		}
		return wr.run(ctx, "Pagelove.DELETE", wr.op(http.MethodDelete, p))
	case *sessel.Element:
		if t.Doc == nil || t.Doc.Path == "" || t.Node == nil {
			return &sessel.Error{Type: sessel.TypeErrorType, Message: "Pagelove.DELETE() takes a path or an element of a stored document"}
		}
		if t.Node.Parent == nil || t.Node.Parent.Type != html.ElementNode {
			op := wr.op(http.MethodDelete, t.Doc.Path)
			return wr.run(ctx, "Pagelove.DELETE", op)
		}
		op := wr.op(http.MethodDelete, t.Doc.Path)
		op.Range = engine.ParseRange("selector=" + selector.Path(t.Doc.Root, t.Node))
		return wr.run(ctx, "Pagelove.DELETE", op)
	}
	return &sessel.Error{Type: sessel.TypeErrorType, Message: fmt.Sprintf("Pagelove.DELETE() takes a path or an element, not %s", sessel.TypeName(target))}
}

// op builds a side-effect write made as the requesting principal. It
// carries no session or connection, so its event reaches the originator
// too (sse R-SSE-23).
func (wr *writer) op(method, path string) *engine.Op {
	st := wr.st
	var p = st.principal
	if p != nil {
		cp := *p
		cp.Session = ""
		p = &cp
	}
	h := st.r.Header.Clone()
	for _, k := range []string{"Range", "If-Match", "If-None-Match", "Content-Type", "Content-Length", "Pagelove-Connection"} {
		h.Del(k)
	}
	// Quiet: PageLove streams no mutation event for a trigger's or
	// processor's side-effect write (live 2026-09-29).
	return &engine.Op{Plane: engine.Public, Method: method, Path: path, Principal: p, Host: st.call.Host, Header: h, Query: st.r.URL.Query(), Quiet: true}
}

func (wr *writer) run(ctx context.Context, fn string, op *engine.Op) error {
	st := wr.st
	st.mu.Lock()
	if st.writes >= MaxSideEffects {
		st.mu.Unlock()
		return &sessel.Error{Type: sessel.RuntimeErrorType, Message: fmt.Sprintf("%s: more than %d side-effect writes in one request", fn, MaxSideEffects)}
	}
	st.writes++
	st.sideDepth++
	st.mu.Unlock()
	res, err := wr.x.engine.Write(ctx, st.site, op)
	st.mu.Lock()
	st.sideDepth--
	st.mu.Unlock()
	st.settle(err == nil)
	if err != nil {
		return writeError(fn, op.Path, err)
	}
	st.mu.Lock()
	_, tracked := st.arrival[op.Path]
	st.mu.Unlock()
	if tracked && res != nil {
		// The request changed its own document: its main write is checked
		// for races against the state the request itself produced.
		v := int64(0)
		if d, e := st.site.Store.Get(ctx, op.Path); e == nil {
			v = d.Version
		}
		st.mu.Lock()
		st.arrival[op.Path] = v
		st.mu.Unlock()
	}
	wr.x.out.wake(st.site)
	if wr.env != nil {
		wr.env.refresh(ctx) // later evaluations see the write
	}
	return nil
}

// types is the requesting site's type information (for @key emission).
func (wr *writer) types() *typeInfo {
	if wr.env != nil && wr.env.host != nil {
		return indexFor(wr.env.host.Snap).types
	}
	return nil
}

// setKeyID writes the honoured @key value of a constructed instance as its
// id (R-MOD-34): through the schema registry when the integration installed
// SetKeyIDFunc (schema.SetKeyID), else from reactions' own reading of the
// declarations.
func (wr *writer) setKeyID(n *html.Node) {
	if f := keyIDFunc.Load(); f != nil && wr.env != nil && wr.env.host != nil && wr.env.host.Snap != nil {
		(*f)(wr.env.host.Snap, n)
		return
	}
	emitKeyID(n, wr.types())
}

var keyIDFunc atomic.Pointer[func(*site.Snapshot, *html.Node)]

// SetKeyIDFunc installs the function that writes a keyed instance's id
// before Pagelove.PUT stores it (schema.SetKeyID); nil restores the
// built-in reading.
func SetKeyIDFunc(f func(snap *site.Snapshot, inst *html.Node)) {
	if f == nil {
		keyIDFunc.Store(nil)
		return
	}
	keyIDFunc.Store(&f)
}

// writeError is the Sessel error raised by a refused side-effect write
// (R-REACT-40): catchable, with the status and description.
func writeError(fn, path string, err error) error {
	msg := err.Error()
	if e, ok := err.(*errdoc.Error); ok {
		msg = fmt.Sprintf("%d %s", e.Status, errdoc.StatusText(e.Status))
		if e.Message != "" {
			msg += ": " + e.Message
		} else if e.Kind != "" {
			msg += " (" + e.Kind + ")"
		}
	}
	return &sessel.Error{Type: sessel.RuntimeErrorType, Message: fmt.Sprintf("%s(%s): %s", fn, path, msg)}
}

// serialize materializes an item for storage: a constructed instance gets
// its @key value as id (R-MOD-34, keyID); a queried element is stored
// verbatim. A whole-document write wraps a fragment in a minimal HTML
// document.
func serialize(item *sessel.Element, document bool, keyID func(*html.Node)) string {
	n := item.Node
	if item.Doc == nil {
		n = dom.Clone(n)
		if keyID != nil && n.Type == html.ElementNode {
			keyID(n)
		}
	}
	if n.Type == html.DocumentNode {
		return string(dom.Render(n))
	}
	out := dom.OuterHTML(n)
	if !document {
		return out
	}
	if n.Type == html.ElementNode && n.Data == "html" && n.Namespace == "" {
		return "<!DOCTYPE html>\n" + out + "\n"
	}
	return "<!DOCTYPE html>\n<html><body>\n" + out + "\n</body></html>\n"
}

// emitKeyID writes the honoured @key value of a constructed instance as its
// id, unless it is empty or contains whitespace (R-MOD-34).
func emitKeyID(n *html.Node, types *typeInfo) {
	if n.Type != html.ElementNode || !dom.HasAttr(n, "itemscope") {
		return
	}
	tokens := strings.Fields(dom.AttrOr(n, "itemtype", ""))
	if len(tokens) == 0 {
		return
	}
	k := types.keyOf(tokens[0])
	if k == "" {
		return
	}
	v := microdata.Parse(n).Get(k)
	if v == "" || strings.ContainsAny(v, " \t\n\r\f") {
		return
	}
	dom.SetAttr(n, "id", v)
}
