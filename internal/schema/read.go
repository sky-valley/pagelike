package schema

import (
	"context"
	"net/http"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

// ApplyRead runs the @read resolvers of every governed instance under root
// (R-MOD-50): for each property with an @read chain and at least one value,
// the chain runs root → leaf over copies of the value elements and the
// result replaces them in root. Computed properties are not materialized
// (R-MOD-44). root must be a tree the caller owns (a per-request parse or
// composition output, never a snapshot tree). It reports whether root
// changed. A failing resolver fails the read: 500 with the BindingFailure
// (501 for a language this build cannot run, 503 when the budget ran out).
//
// Composition calls it once on its final tree; doing so also tells the
// public-plane wrapper installed by Register that the request's @read pass
// is done. Budgets come from ctx (budget.From).
func ApplyRead(ctx context.Context, s *site.Site, snap *site.Snapshot, path string, root *html.Node) (bool, error) {
	if st, ok := ctx.Value(readKey{}).(*readState); ok {
		st.applied = true
	}
	reg := For(snap)
	if !reg.hasRead || root == nil {
		return false, nil
	}
	var todo []*html.Node
	for _, n := range instancesUnder(root) {
		s := reg.schemas[itemType(n)]
		if s == nil || s.eff == nil {
			continue
		}
		for _, pn := range s.eff.propOrder {
			if ep := s.eff.props[pn]; len(ep.ReadChain) > 0 && ep.Decl.Computed == nil {
				todo = append(todo, n)
				break
			}
		}
	}
	if len(todo) == 0 {
		return false, nil
	}
	ev := newEvaluator(ctx, s, snap, reg)
	doc := &sessel.Document{Path: path, Root: root}
	changed := false
	// Inner instances first, so an outer resolver sees their results.
	for i := len(todo) - 1; i >= 0; i-- {
		inst := todo[i]
		if !attached(root, inst) {
			continue
		}
		sc := reg.schemas[itemType(inst)]
		for _, pn := range sc.eff.propOrder {
			ep := sc.eff.props[pn]
			if len(ep.ReadChain) == 0 || ep.Decl.Computed != nil {
				continue
			}
			elems := propElements(inst, pn)
			if len(elems) == 0 {
				continue
			}
			out, err := ev.readChain(ep, elems, sesselEnv{doc: docElement(doc)})
			if err != nil {
				return false, readFailure(err)
			}
			parent := elems[0].Parent
			for _, n := range out {
				parent.InsertBefore(n, elems[0])
			}
			for _, e := range elems {
				e.Parent.RemoveChild(e)
			}
			changed = true
		}
	}
	return changed, nil
}

func readFailure(err error) error {
	f := failureOf(err)
	status := http.StatusInternalServerError
	switch err.(type) {
	case *exhausted:
		status = http.StatusServiceUnavailable
	case *unavailable:
		status = http.StatusNotImplemented
	case *thrownResponse:
		f = &BindingFailure{Language: URLSessel, Variant: "threw", Message: err.Error()}
	}
	return &errdoc.Error{Status: status, Kind: KindBindingFailure, Message: "@read failed: " + f.Message,
		Document: ReadFailureDocument(status, "@read failed: "+f.Message, f)}
}

type readKey struct{}

type readState struct{ applied bool }

// composeWithRead wraps the public plane's Composer so that @read
// resolvers apply to every public-plane representation (GET/HEAD whole and
// by selector, JSON-LD, edge QUERY): unless the wrapped Composer called
// ApplyRead itself, it runs on the composed tree afterwards. Documents
// without governed resolvers stay unchanged, so they are still served as
// their stored bytes.
func composeWithRead(next engine.Composer) engine.Composer {
	return func(ctx context.Context, s *site.Site, snap *site.Snapshot, doc *store.Document, root *html.Node, op *engine.ReadOp) (*engine.Composed, error) {
		st := &readState{}
		ctx = context.WithValue(ctx, readKey{}, st)
		comp := &engine.Composed{Root: root}
		if next != nil {
			var err error
			if comp, err = next(ctx, s, snap, doc, root, op); err != nil {
				return nil, err
			}
		}
		if st.applied || comp == nil || comp.Root == nil {
			return comp, nil
		}
		changed, err := ApplyRead(ctx, s, snap, doc.Path, comp.Root)
		if err != nil {
			return nil, err
		}
		if changed {
			comp.Changed = true
		}
		return comp, nil
	}
}
