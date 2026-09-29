package schema

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/budget"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/query"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

// evalHost is the Sessel host of schema evaluations: the read-only
// snapshot host of internal/query. Without a site (policy hooks run while a
// snapshot is being assembled) Pagelove.GET reads the snapshot only.
type evalHost struct{ *query.Host }

func (h evalHost) Resource(ctx context.Context, path string) (sessel.Value, error) {
	if h.Host.Site != nil {
		return h.Host.Resource(ctx, path)
	}
	if d := h.Host.Document(path); d != nil {
		return d.Element(), nil
	}
	return nil, nil
}

func newHost(s *site.Site, snap *site.Snapshot) sessel.Host {
	return evalHost{query.NewHost(s, snap)}
}

// evaluator runs binding slots for one request (write pass, read pass,
// typed access), sharing the request's budget.
type evaluator struct {
	ctx     context.Context
	host    sessel.Host
	reg     *Registry
	budget  *sessel.Budget
	context *sessel.Dict
	webdav  bool
}

func newEvaluator(ctx context.Context, s *site.Site, snap *site.Snapshot, reg *Registry) *evaluator {
	return &evaluator{ctx: ctx, host: newHost(s, snap), reg: reg, budget: budget.From(ctx).Sessel()}
}

// Failure kinds a slot evaluation can end with, besides a value.
type (
	// thrownResponse is a thrown HTTPResponse (Sessel or JavaScript).
	thrownResponse struct {
		status  int
		body    string
		headers [][2]string
	}
	// exhausted is a budget or deadline exhaustion (503 / 507).
	exhausted struct{ f *BindingFailure }
	// unavailable is a slot language this build cannot run (501).
	unavailable struct{ f *BindingFailure }
)

func (t *thrownResponse) Error() string { return fmt.Sprintf("HTTPResponse %d", t.status) }
func (e *exhausted) Error() string      { return e.f.Error() }
func (e *unavailable) Error() string    { return e.f.Error() }

// sesselFailure classifies a Sessel error.
func sesselFailure(err error) error {
	if r, ok := sessel.ResponseOf(err); ok {
		return &thrownResponse{status: r.Status, body: r.HTMLBody(), headers: r.Headers}
	}
	f := &BindingFailure{Language: URLSessel, Variant: "threw", Message: err.Error()}
	if sessel.IsParseError(err) {
		f.Variant = "parse"
		return f
	}
	if e, ok := sessel.AsError(err); ok {
		switch e.Reason {
		case sessel.ReasonTimeout:
			f.Variant = "timeout"
			return &exhausted{f}
		case sessel.ReasonBudget:
			f.Variant = "out-of-memory"
			return &exhausted{f}
		}
	}
	return f
}

// jsFailure classifies a JavaScript runner error.
func jsFailure(err error) error {
	var tr *JSThrownResponse
	if errors.As(err, &tr) {
		st := tr.Status
		if st == 0 {
			st = http.StatusInternalServerError
		}
		return &thrownResponse{status: st, body: tr.Content(), headers: tr.Headers}
	}
	var bf *BindingFailure
	if errors.As(err, &bf) {
		switch bf.Variant {
		case VariantUnavailable:
			return &unavailable{bf}
		case "timeout", "out-of-memory":
			return &exhausted{bf}
		}
		return bf
	}
	return &BindingFailure{Language: URLJavaScript, Variant: "threw", Message: err.Error()}
}

// unknownLanguage is the failure of a slot whose wrapper itemtype names no
// supported language (R-MOD-7).
func unknownLanguage(s *Slot) error {
	return &BindingFailure{Language: s.ItemType, Variant: "unknown-language",
		Message: fmt.Sprintf("no binding language is registered for %s", s.ItemType)}
}

// sesselEnv describes one Sessel evaluation.
type sesselEnv struct {
	self    sessel.Value
	hasSelf bool
	prior   *sessel.Element
	doc     *sessel.Element
	vars    map[string]sessel.Value
}

// runSessel evaluates a Sessel slot. Errors are classified (parse and
// runtime failures as *BindingFailure, thrown responses, exhaustion).
func (ev *evaluator) runSessel(s *Slot, e sesselEnv) (sessel.Value, error) {
	if err := s.compile(); err != nil {
		return nil, sesselFailure(err)
	}
	env := &sessel.Env{Host: ev.host, Self: e.self, HasSelf: e.hasSelf, Prior: e.prior, Document: e.doc,
		Vars: e.vars, Budget: ev.budget, Context: ev.context}
	v, err := s.compiled.Eval(ev.ctx, env)
	if err != nil {
		return nil, sesselFailure(err)
	}
	return v, nil
}

// applySlot is the program that applies a resolver whose source evaluates
// to a lambda (the Sessel/Lambda form) to the pipeline value.
var applySlot = &Slot{Lang: LangSessel, Source: "__pl_fn(self)"}

// runResolver evaluates a Sessel resolver stage: the expression's value,
// or, when it is a lambda, the lambda applied to self.
func (ev *evaluator) runResolver(s *Slot, e sesselEnv) (sessel.Value, error) {
	v, err := ev.runSessel(s, e)
	if err != nil {
		return nil, err
	}
	if l, ok := v.(*sessel.Lambda); ok {
		vars := map[string]sessel.Value{"__pl_fn": l}
		for k, x := range e.vars {
			vars[k] = x
		}
		e.vars = vars
		return ev.runSessel(applySlot, e)
	}
	return v, nil
}

// runJSSlot evaluates a JavaScript slot.
func (ev *evaluator) runJSSlot(s *Slot, call *JSCall) (*JSResult, error) {
	call.Source = s.Source
	if call.Classes == nil {
		call.Classes = ev.reg
	}
	if call.Host == nil {
		call.Host, call.Budget = ev.host, ev.budget
	}
	res, err := runJS(ev.ctx, call)
	if err != nil {
		return nil, jsFailure(err)
	}
	return res, nil
}

// failureOf extracts the BindingFailure of a classified error.
func failureOf(err error) *BindingFailure {
	switch e := err.(type) {
	case *BindingFailure:
		return e
	case *exhausted:
		return e.f
	case *unavailable:
		return e.f
	}
	return &BindingFailure{Variant: "threw", Message: err.Error()}
}

// fatal converts the failures that override the stage's own status
// (thrown responses where allowed, exhaustion, unavailable languages) into
// the request's error; ok is false for ordinary failures.
func (ev *evaluator) fatal(err error, responses bool, vs func(f *BindingFailure) []Violation) (*errdoc.Error, bool) {
	switch e := err.(type) {
	case *thrownResponse:
		if responses {
			return responseError(e.status, e.body, e.headers), true
		}
	case *exhausted:
		st := http.StatusServiceUnavailable
		if ev.webdav {
			st = http.StatusInsufficientStorage
		}
		return schemaError(st, vs(e.f)), true
	case *unavailable:
		return schemaError(http.StatusNotImplemented, vs(e.f)), true
	}
	return nil, false
}

// ---------------------------------------------------------------- marshalling

// elementValues returns property value elements as JavaScript values
// (R-MOD-51): the microdata value, or a tagged element for nested items.
func elementValues(elems []*html.Node) []any {
	out := make([]any, 0, len(elems))
	for _, n := range elems {
		if isItem(n) {
			out = append(out, map[string]any{"$type": "element", "$html": dom.OuterHTML(n)})
			continue
		}
		v := valueOf(n)
		if v.null {
			out = append(out, "")
		} else {
			out = append(out, v.s)
		}
	}
	return out
}

// pipelineValue is the JavaScript form of a property's values: a list for an
// explicit 0..n/1..n, otherwise the single value (R-MOD-20, R-MOD-51).
func pipelineValue(card string, elems []*html.Node) any {
	vals := elementValues(elems)
	if isMulti(card) {
		return vals
	}
	if len(vals) == 0 {
		return nil
	}
	return vals[0]
}

// instanceView is the plain-object view of an instance for JavaScript
// `this` (defaults, @computed): the declared properties already set, as
// scalars or lists of scalars; nested items are omitted (R-JS-11).
func (r *Registry) instanceView(t string, inst *html.Node, skip string) map[string]any {
	out := map[string]any{}
	s := r.schemas[t]
	if s == nil || s.eff == nil {
		return out
	}
	for _, pn := range s.eff.propOrder {
		if pn == skip {
			continue
		}
		ep := s.eff.props[pn]
		elems := propElements(inst, pn)
		var scalars []*html.Node
		for _, e := range elems {
			if !isItem(e) {
				scalars = append(scalars, e)
			}
		}
		if len(scalars) == 0 {
			continue
		}
		out[pn] = pipelineValue(ep.Decl.Cardinality, scalars)
	}
	return out
}

// queried wraps a node of a tree the host owns as an immutable Sessel
// element with provenance.
func queried(n *html.Node, doc *sessel.Document) *sessel.Element {
	return sessel.Queried(n, doc)
}

// docElement returns the root element of a sessel document, or nil.
func docElement(d *sessel.Document) *sessel.Element {
	if d == nil || d.Root == nil {
		return nil
	}
	return d.Element()
}
