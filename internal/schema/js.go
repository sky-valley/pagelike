package schema

import (
	"context"
	"sort"
	"sync/atomic"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/sessel"
)

// Server-side JavaScript seam. The schema system evaluates JavaScript/Module
// binding items (defaults, @read, @write, @validate at both levels,
// @computed, method implementations, Group includes()) through a JSRunner.
// The runtime itself (internal/jsrt, decision 0001) is plugged in by the
// integration step with SetJSRunner; until then every JavaScript slot fails
// clearly: a BindingFailure with variant "unavailable" and HTTP 501.

// JS slot kinds (docs/spec/javascript.md A3).
const (
	JSSlotDefault        = "default"
	JSSlotRead           = "@read"
	JSSlotWrite          = "@write"
	JSSlotValidate       = "@validate"        // property level
	JSSlotSchemaValidate = "schema-@validate" // schema level
	JSSlotComputed       = "@computed"
	JSSlotMethod         = "method"
)

// JSCall is one evaluation of a binding module: the module is evaluated in
// a fresh context, its default export must be a function, and it is called
// with This (when HasThis) and exactly Args (R-JS-10). Values are in the
// JSON-like model shared with Sessel (sessel.ToGo / sessel.FromGo): nil,
// bool, int64, float64, string, []any, map[string]any, and tagged elements
// {"$type":"element","$html":…}.
type JSCall struct {
	Slot    string
	Source  string // ES module source
	This    any
	HasThis bool
	Args    []any
	// Document is the markup the `document` global views ("" = no
	// document global); DocumentWritable marks the default slot's writable
	// view of the in-progress instance (R-JS-11).
	Document         string
	DocumentWritable bool
	// Context is the shared Context object (methods only; writes are kept).
	Context *sessel.Dict
	// Classes is the host registry schema imports resolve against
	// (`import X from "<type URL>" with {type: "https://pagelove.org/Schema"}`,
	// R-JS-85): Schema(url) gives the descriptor (parent, properties,
	// methods with their implementations, JavaScript sources included), and
	// it is a sessel.ClassRegistry for Sessel method bodies called from JS.
	Classes *Registry

	// Receiver is the Sessel receiver of a direct method call (R-JS-17): an
	// instance element, or the Class of a static method. The JSON-like This
	// cannot express either, so a runner prefers Receiver when it is set.
	Receiver sessel.Value
	// HostNode is the dispatching element of a composition dispatch
	// (R-JS-18/19): `this` is that element and the read-only `document` is
	// scoped to its subtree (this === document.documentElement). Path is the
	// path of the document it belongs to.
	HostNode *html.Node
	Path     string
	// Host and Budget serve Sessel method bodies that the JavaScript reaches
	// through imported classes (the cross-language path of R-JS-17); nil
	// Host means such calls see no site documents.
	Host   sessel.Host
	Budget *sessel.Budget
}

// JSResult is a successful evaluation.
type JSResult struct {
	Value any
	// Document is the mutated document markup of a writable default.
	Document string
	// Private reports that the code read a per-requester member of
	// `request` (Context.request.auth, .headers; R-JS-34).
	Private bool
}

// JSRunner evaluates binding modules. Budgets come from ctx
// (budget.From(ctx)). Failures are returned as *BindingFailure (R-JS-51
// variants; Language is filled in by the schema package when empty) or,
// for a thrown HTTPResponse-shaped object (R-JS-53), as *JSThrownResponse.
type JSRunner interface {
	Run(ctx context.Context, call *JSCall) (*JSResult, error)
}

// JSThrownResponse is a thrown plain object whose schema_url (or itemtype)
// is https://pagelove.org/HTTPResponse.
type JSThrownResponse struct {
	Status  int
	Message string
	Body    string
	HasBody bool
	Headers [][2]string
}

func (r *JSThrownResponse) Error() string { return "HTTPResponse " + r.Message }

// Content is the response body: body, else message.
func (r *JSThrownResponse) Content() string {
	if r.HasBody {
		return r.Body
	}
	return r.Message
}

var jsRunner atomic.Pointer[JSRunner]

// SetJSRunner installs the server JavaScript runtime (nil removes it).
func SetJSRunner(r JSRunner) {
	if r == nil {
		jsRunner.Store(nil)
		return
	}
	jsRunner.Store(&r)
}

// VariantUnavailable is the pagelike BindingFailure variant reported when no
// JavaScript runtime is installed.
const VariantUnavailable = "unavailable"

// runJS evaluates a JavaScript slot through the installed runner.
func runJS(ctx context.Context, call *JSCall) (*JSResult, error) {
	p := jsRunner.Load()
	if p == nil || *p == nil {
		return nil, &BindingFailure{Language: URLJavaScript, Variant: VariantUnavailable,
			Message: "server-side JavaScript is not available in this build"}
	}
	res, err := (*p).Run(ctx, call)
	if bf, ok := err.(*BindingFailure); ok && bf.Language == "" {
		bf.Language = URLJavaScript
	}
	return res, err
}

// fromJS converts a runner result (the JSON-like model) to a Sessel value:
// like sessel.FromGo, but tagged elements ({"$type":"element","$html":…})
// become constructed elements rather than dictionaries.
func fromJS(v any) sessel.Value {
	switch x := v.(type) {
	case []any:
		out := make(sessel.List, len(x))
		for i, it := range x {
			out[i] = fromJS(it)
		}
		return out
	case map[string]any:
		if x["$type"] == "element" {
			if h, ok := x["$html"].(string); ok {
				if n := parseElement(h); n != nil {
					return sessel.NewElement(n)
				}
			}
		}
		d := sessel.NewDict()
		for _, k := range sortedKeys(x) {
			d.Set(k, fromJS(x[k]))
		}
		return d
	}
	return sessel.FromGo(v)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
