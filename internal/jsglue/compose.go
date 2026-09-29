package jsglue

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/compose"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/jsrt"
	"github.com/sky-valley/pagelike/internal/schema"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

// composeRunner implements compose.JSRunner: j: expression bindings and
// JavaScript methods dispatched by composition's own dispatcher.
type composeRunner struct{}

// Binding implements compose.JSRunner (R-JS-30..36).
func (composeRunner) Binding(ctx context.Context, b *compose.JSBinding) (*compose.JSResult, error) {
	scope := jsrt.NewDict()
	for _, n := range b.Names {
		scope.Set(n, fromSesselLenient(b.Values[n], nil))
	}
	req := jsrt.ExprRequest{Expression: b.Source, Scope: scope, Request: requestOf(b.Request), Host: &host{}}
	if b.Host != nil {
		req.Document = &jsrt.Document{Node: b.Host, Source: b.DocPath}
	}
	res, f := evalExpression(ctx, req)
	if f != nil {
		return nil, composeFailure("j:"+b.Name, f)
	}
	return composeResult(res, nil), nil
}

// Method implements compose.JSRunner (R-COMP-66, R-JS-18/19).
func (composeRunner) Method(ctx context.Context, m *compose.JSMethod) (*compose.JSResult, error) {
	var reg *schema.Registry
	if m.Snap != nil {
		reg = schema.For(m.Snap)
	}
	h := &host{reg: reg, cx: m.Context}
	req := jsrt.CallRequest{Source: m.Source, Slot: jsrt.SlotDispatch, Host: h, Context: contextValue(m.Context, reg)}
	if m.Self != nil && m.Self.Node != nil {
		req.This = &jsrt.Element{Node: m.Self.Node, Source: m.DocPath}
		req.Document = &jsrt.Document{Node: m.Self.Node, Source: m.DocPath}
	}
	for _, a := range m.Args {
		req.Args = append(req.Args, fromSesselLenient(a, reg))
	}
	res, f := callModule(ctx, req)
	if f != nil {
		return nil, composeFailure("method "+m.Name, f)
	}
	out := composeResult(res, reg)
	out.Context = nil // written to m.Context as they happened
	return out, nil
}

func composeResult(res *jsrt.Result, reg *schema.Registry) *compose.JSResult {
	out := &compose.JSResult{Value: toSessel(res.Value, reg), Private: res.Tainted}
	for _, w := range res.ContextWrites {
		if w.Deleted || w.Name == "request" {
			continue
		}
		if out.Context == nil {
			out.Context = map[string]sessel.Value{}
		}
		out.Context[w.Name] = toSessel(w.Value, reg)
	}
	return out
}

// composeFailure is the composition error of a failed evaluation (R-JS-36,
// R-JS-52): 503 for budget exhaustion (R-JS-57), else 500; the Error
// document carries the BindingFailure item.
func composeFailure(what string, f *jsrt.Failure) error {
	status, kind := http.StatusInternalServerError, compose.KindComposition
	if f.Budget() {
		status, kind = http.StatusServiceUnavailable, compose.KindBudget
	}
	e := errdoc.New(status, kind, "%s: %s", what, f.Message)
	e.Detail = f.HTML()
	return e
}

// dispatcher is composition's method dispatcher over the schema registry
// (compose.SetMethodDispatcher): method elements and attributes resolve
// schemas, overloads, inheritance and doesNotUnderstand exactly as the
// schema system declares them (schema.DispatchMethod).
type dispatcher struct{}

// HasSchema implements compose.MethodDispatcher.
func (dispatcher) HasSchema(snap *site.Snapshot, typeURL string) bool {
	return schema.For(snap).Schema(typeURL) != nil
}

// Dispatch implements compose.MethodDispatcher.
func (dispatcher) Dispatch(ctx context.Context, call *compose.MethodCall) (*compose.MethodResult, error) {
	var hostNode *xhtml.Node
	path := call.DocPath
	if call.Self != nil {
		hostNode = call.Self.Node
		if call.Self.Doc != nil && call.Self.Doc.Path != "" {
			path = call.Self.Doc.Path
		}
	}
	mc := &schema.MethodCall{Snap: call.Snap, Type: call.TypeURL, Name: call.Name, Host: hostNode, Path: path,
		AttributeForm: call.Attribute, Value: call.Value,
		Env: &sessel.Env{Host: call.Host, Context: call.Context, Request: call.Request, Budget: call.Budget}}
	res, err := schema.DispatchMethod(ctx, mc)
	if errors.Is(err, schema.ErrNoMethod) {
		if schema.For(call.Snap).Schema(call.TypeURL) == nil {
			return nil, compose.ErrNoSchema
		}
		return nil, compose.ErrNoMethod
	}
	if err != nil {
		return nil, dispatchError("method "+call.Name, err)
	}
	out := &compose.MethodResult{Value: res.Value, Private: res.Private}
	if m := res.Method; m != nil {
		out.ReturnsElement = m.Returns == compose.URLElement
		if impl := m.Implementation; impl != nil && impl.Lang == schema.LangSessel && compose.ReadsIdentity(impl.Source) {
			out.Private = true
		}
	}
	return out, nil
}

// dispatchError maps a failed schema method dispatch onto composition's
// errors (R-COMP-67, R-JS-52): JavaScript failures carry their
// BindingFailure (503 on budget exhaustion, 501 without a runtime), Sessel
// failures map as composition's own.
func dispatchError(what string, err error) error {
	var bf *schema.BindingFailure
	if errors.As(err, &bf) {
		status, kind := http.StatusInternalServerError, compose.KindComposition
		switch bf.Variant {
		case string(jsrt.VariantTimeout), string(jsrt.VariantOutOfMemory):
			status, kind = http.StatusServiceUnavailable, compose.KindBudget
		case schema.VariantUnavailable:
			status, kind = http.StatusNotImplemented, compose.KindJSUnavailable
		}
		e := errdoc.New(status, kind, "%s: %s", what, bf.Message)
		e.Detail = failureHTML(bf)
		return e
	}
	var tr *schema.JSThrownResponse
	if errors.As(err, &tr) {
		// A method body cannot choose the response (R-JS-53).
		return errdoc.New(http.StatusInternalServerError, compose.KindComposition, "%s: threw an HTTPResponse, which only @write, @validate and reaction actions honour", what)
	}
	return compose.SesselError(what, err)
}

// failureHTML renders a BindingFailure item (R-JS-50).
func failureHTML(f *schema.BindingFailure) string {
	var b strings.Builder
	b.WriteString(`<div itemprop="failure" itemscope itemtype="https://pagelove.org/BindingFailure">`)
	fmt.Fprintf(&b, `<meta itemprop="language" content="%s">`, html.EscapeString(f.Language))
	fmt.Fprintf(&b, `<meta itemprop="variant" content="%s">`, html.EscapeString(f.Variant))
	fmt.Fprintf(&b, `<p itemprop="message">%s</p>`, html.EscapeString(f.Message))
	if f.Stack != "" && f.Variant == string(jsrt.VariantThrew) {
		fmt.Fprintf(&b, `<pre itemprop="stack">%s</pre>`, html.EscapeString(f.Stack))
	}
	b.WriteString(`</div>`)
	return b.String()
}
