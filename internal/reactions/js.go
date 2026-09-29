package reactions

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"

	"github.com/sky-valley/pagelike/internal/budget"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

// JSRunner runs the server JavaScript of reactions: `when` gates, actions,
// dynamic HttpRequest properties and j: bindings written as
// https://pagelove.org/JavaScript/Module items (R-REACT-19, R-REACT-25). The
// internal/jsrt package plugs in with SetJSRunner; without it every
// JavaScript evaluation fails the request with ErrNoJavaScript (501, a
// BindingFailure with variant "unavailable").
type JSRunner interface {
	// CallDefault evaluates call.Source as an ES module and calls its
	// default export with this undefined and one argument, call.Arg (plain
	// JSON-like data: {request: {method, path, host, headers, query, body,
	// auth}, response?: {status, body, headers}}; writes to it have no
	// effect). It returns the converted result (nil, bool, float64, int64,
	// string, []any, map[string]any). A thrown HTTPResponse-shaped object
	// (schema_url or itemtype https://pagelove.org/HTTPResponse) is returned
	// as a *ThrownResponse error; any other failure as a *JSError. The module
	// has no Pagelove global (R-REACT-41) and may import only pagelove:schema
	// from call.Site's registry.
	CallDefault(ctx context.Context, call *JSCall) (any, error)
}

// JavaScript slots of reactions (docs/spec/javascript.md A3, R-JS-21..23):
// they decide whether a thrown HTTPResponse chooses the response (actions
// only, R-JS-53) and whether a document exists.
const (
	SlotTriggerWhen         = "trigger-when"
	SlotTriggerAction       = "trigger-action"
	SlotTriggerOtherwise    = "trigger-otherwise"
	SlotProcessorWhen       = "processor-when"
	SlotProcessorAction     = "processor-action"
	SlotHTTPRequestProperty = "httprequest-property"
	// SlotBinding is a j: binding declared on a trigger or processor
	// (R-REACT-24), wrapped as a module whose default returns the value.
	SlotBinding = "binding"
)

// JSCall is one JavaScript evaluation.
type JSCall struct {
	Site   *site.Site
	Snap   *site.Snapshot // the site's current snapshot (schema registry)
	Slot   string         // one of the Slot constants
	Source string
	Arg    map[string]any
	Budget *budget.Request // the request's shared budget
}

// ThrownResponse is an HTTPResponse thrown by JavaScript (R-REACT-30).
type ThrownResponse struct {
	Status     int // 0 means absent (500)
	Message    string
	HasMessage bool
	Body       string
	HasBody    bool
	Headers    [][2]string // one field line per entry, in order
}

func (t *ThrownResponse) Error() string { return "thrown HTTPResponse" }

// JSError is a JavaScript failure other than a thrown HTTPResponse, with
// the JS-in-schemas variant (parse, threw, timeout, out-of-memory, …).
type JSError struct {
	Variant string
	Message string
	Stack   string
}

func (e *JSError) Error() string { return "javascript " + e.Variant + ": " + e.Message }

// ErrNoJavaScript reports that no JSRunner is installed.
var ErrNoJavaScript = &JSError{Variant: "unavailable", Message: "server JavaScript is not available in this build (no JavaScript runtime is installed for reactions)"}

var jsRunner atomic.Pointer[JSRunner]

// SetJSRunner installs the server JavaScript runtime (nil removes it).
func SetJSRunner(r JSRunner) {
	if r == nil {
		jsRunner.Store(nil)
		return
	}
	jsRunner.Store(&r)
}

// runJS calls a JavaScript module's default export in the given slot.
func runJS(ctx context.Context, s *site.Site, snap *site.Snapshot, slot, src string, arg map[string]any) (sessel.Value, error) {
	r := jsRunner.Load()
	if r == nil {
		return nil, ErrNoJavaScript
	}
	v, err := (*r).CallDefault(ctx, &JSCall{Site: s, Snap: snap, Slot: slot, Source: src, Arg: arg, Budget: budget.From(ctx)})
	if err != nil {
		return nil, err
	}
	return sessel.FromGo(v), nil
}

// jsUnavailable reports an ErrNoJavaScript failure (answered 501).
func jsUnavailable(err error) bool {
	var je *JSError
	return errors.As(err, &je) && je == ErrNoJavaScript
}

// jsStatus is the status of a runtime failure: 501 when the JavaScript
// runtime is missing (an unsupported feature, plan §Principles), 503 when
// the evaluation exhausted the request budget (R-JS-57), else 500.
func jsStatus(err error) int {
	if jsUnavailable(err) {
		return http.StatusNotImplemented
	}
	var je *JSError
	if errors.As(err, &je) && (je.Variant == "timeout" || je.Variant == "out-of-memory") {
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}
