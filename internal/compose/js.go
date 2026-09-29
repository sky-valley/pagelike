package compose

import (
	"context"
	"net/http"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

// JSRunner runs server JavaScript for composition: `j:` expression
// bindings (R-COMP-40..43) and JavaScript method implementations
// (R-COMP-66). The bounded runtime lives in internal/jsrt (decision 0001),
// which a later integration step plugs in with Engine.JS or SetJSRunner.
// Without a runner every JavaScript slot fails the request with 501
// JavaScriptUnavailable, never silently.
type JSRunner interface {
	// Binding evaluates one `j:` expression as the body of an async
	// function (a returned promise is settled). Names are the Context
	// names visible at the element, nearest declaration winning, in
	// declaration order; each is a bare identifier when it is one, and all
	// are properties of the `Context` object, which also holds `request`.
	Binding(ctx context.Context, b *JSBinding) (*JSResult, error)
	// Method runs a JavaScript method implementation (a module whose
	// default export is the method; it may be async).
	Method(ctx context.Context, m *JSMethod) (*JSResult, error)
}

// JSBinding is one `j:` expression.
type JSBinding struct {
	Name    string // the binding name (attribute local name)
	Source  string // the expression
	DocPath string // the requested document
	// Host is the element carrying the binding: the read-only `document`
	// is scoped to its subtree (R-JS-31, pagelike).
	Host    *html.Node
	Names   []string
	Values  map[string]sessel.Value
	Request *sessel.Dict
}

// JSMethod is one JavaScript method call from a method element or
// attribute (R-COMP-61, R-COMP-64, R-COMP-66).
type JSMethod struct {
	// Snap is the snapshot being composed (schema imports resolve against
	// its registry).
	Snap    *site.Snapshot
	TypeURL string
	Name    string // the declared method name (doesNotUnderstand for the fallback)
	Source  string // the module source
	DocPath string
	// Self is the dispatched element (`this`, as a microdata object).
	Self *sessel.Element
	// Args are the positional arguments in declaration order (absent →
	// nil); for doesNotUnderstand: messageName, parameters.
	Args    []sessel.Value
	Context *sessel.Dict // visible names plus request; mutations are read back
	Request *sessel.Dict
}

// JSResult is a JavaScript evaluation's outcome. Values are converted to
// Sessel values: objects → Dict, arrays → List, integral numbers within
// ±2^53 → int64 (R-LIQ-59), DOM nodes → constructed Elements.
type JSResult struct {
	Value sessel.Value
	// Private reports that the code read a per-requester member of
	// request (auth, headers…), so the response is Cache-Control: private
	// (R-COMP-43).
	Private bool
	// Context holds the Context entries the code added or changed.
	Context map[string]sessel.Value
}

// errNoJS is the failure of a JavaScript slot without a runtime.
func errNoJS(what string) error {
	return errdoc.New(http.StatusNotImplemented, KindJSUnavailable,
		"%s needs server JavaScript, which this pagelike build does not provide", what)
}
