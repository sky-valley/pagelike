package jsrt

import (
	"context"
	"html"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

// Variant is a BindingFailure variant (docs/spec/javascript.md R-JS-51).
type Variant string

// The failure variants. VariantInternal is pagelike's: the runtime itself
// failed (a worker died for a reason that is not a limit, a protocol error,
// a caller error such as a wrong argument count). Callers treat it as a 500.
const (
	VariantParse            Variant = "parse"
	VariantShape            Variant = "shape"
	VariantThrew            Variant = "threw"
	VariantTimeout          Variant = "timeout"
	VariantOutOfMemory      Variant = "out-of-memory"
	VariantMarshal          Variant = "marshal"
	VariantReturnType       Variant = "return-type"
	VariantImportNotAllowed Variant = "import-not-allowed"
	VariantUnknownSchema    Variant = "unknown-schema"
	VariantUnknownLanguage  Variant = "unknown-language"
	VariantInternal         Variant = "internal"
)

// LanguageURL is the itemtype of JavaScript binding items.
const LanguageURL = "https://pagelove.org/JavaScript/Module"

// SchemaImportType is the import attribute type of schema imports.
const SchemaImportType = "https://pagelove.org/Schema"

// HTTPResponseType marks thrown HTTPResponse objects (R-JS-53).
const HTTPResponseType = "https://pagelove.org/HTTPResponse"

// MapSchemaType is the built-in Map schema; a schema whose parent chain
// reaches it yields a class extending Map (R-JS-85).
const MapSchemaType = "https://pagelove.org/Map"

// Failure describes a failed evaluation. It is the BindingFailure item of
// R-JS-50 plus the thrown HTTPResponse of R-JS-53.
type Failure struct {
	Variant Variant
	// Message is "<name>: <message>" for a thrown Error-like value,
	// String(value) for other thrown values, and a pagelike description for
	// the other variants. Message text is engine-specific.
	Message string
	// Stack is the QuickJS stack ("    at default (eval:2:17)"), only for
	// VariantThrew, when the engine provided one.
	Stack string
	// Response is set when the binding threw an HTTPResponse-shaped object
	// and the slot honours it (R-JS-53); Variant is then VariantThrew.
	Response *HTTPResponse
	// Specifier is the offending import for the import variants.
	Specifier string
	// Stats are the evaluation's statistics, when it reached a worker.
	Stats Stats
}

// Error implements error.
func (f *Failure) Error() string {
	if f == nil {
		return "<nil>"
	}
	return "javascript " + string(f.Variant) + ": " + f.Message
}

// Budget reports whether the failure is a budget exhaustion (R-JS-57:
// 503 on the public plane, 507 on WebDAV).
func (f *Failure) Budget() bool {
	return f != nil && (f.Variant == VariantTimeout || f.Variant == VariantOutOfMemory)
}

// HTML renders the failure as the BindingFailure Microdata item of R-JS-50,
// nested with itemprop="failure".
func (f *Failure) HTML() string {
	var b strings.Builder
	b.WriteString(`<div itemprop="failure" itemscope itemtype="https://pagelove.org/BindingFailure">`)
	b.WriteString(`<meta itemprop="language" content="` + LanguageURL + `">`)
	b.WriteString(`<meta itemprop="variant" content="` + html.EscapeString(string(f.Variant)) + `">`)
	b.WriteString(`<p itemprop="message">` + html.EscapeString(f.Message) + `</p>`)
	if f.Stack != "" {
		b.WriteString(`<pre itemprop="stack">` + html.EscapeString(f.Stack) + `</pre>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// HTTPResponse is a thrown HTTPResponse object (R-JS-53). The response it
// describes replaces the whole response; Body is sent as given.
type HTTPResponse struct {
	Status  int
	Message string
	Body    string
	// Headers are the extra response headers, in the order given; names
	// that are not valid header tokens and values with control characters
	// are dropped.
	Headers [][2]string
}

// Budget bounds one evaluation. Zero fields take the unbudgeted defaults.
type Budget struct {
	// Time is the remaining time budget (default 1 s). The caller's context
	// deadline, when earlier, wins.
	Time time.Duration
	// Memory is the remaining memory budget in bytes for the JavaScript heap
	// (default 16 MiB). DOM nodes the binding creates are charged against
	// the same amount separately.
	Memory int64
	// DOMOps caps the DOM operations of the evaluation (0 = no cap). Going
	// over is a budget exhaustion (VariantTimeout).
	DOMOps int
}

// Default budgets for unbudgeted evaluations (R-JS-56).
const (
	DefaultTime   = time.Second
	DefaultMemory = 16 << 20
)

// Stats describe an evaluation, for tracing (R-JS-54) and budget accounting
// (R-JS-59).
type Stats struct {
	Kind       string        // binding.kind
	SourceHash string        // short hex hash of the source
	SourceLen  int           // bytes
	Outcome    string        // "ok" or the variant
	Elapsed    time.Duration // wall time in the worker
	RoundTrip  time.Duration // wall time seen by the host, including IPC
	MemoryUsed int64         // JavaScript heap bytes in use at the end (above the runtime baseline)
	DOMOps     int           // DOM operations performed
	HostCalls  int           // callbacks to the Host
	WorkerPID  int
	WorkerRSS  int64 // the worker's peak RSS, bytes
}

// Document is the document a binding runs against (the ambient `document`).
type Document struct {
	// Node is a document node, or an element whose subtree is the whole view
	// (default: the in-progress instance; composition dispatch: the host
	// element), which becomes documentElement of a Document in JavaScript.
	Node *xhtml.Node
	// HTML is an alternative to Node: a whole HTML (or XML) document.
	HTML string
	// XML parses HTML as an XML document (package xmldom). Nodes built by
	// xmldom are recognized automatically.
	XML bool
	// Source is the document path, reported as the $source of elements the
	// binding returns from this document.
	Source string
}

// CallRequest evaluates one module binding.
type CallRequest struct {
	// Source is the module source (the binding item's `source`).
	Source string
	// Slot selects the calling convention (slots.go).
	Slot Slot
	// This is the receiver; nil means undefined (use Null for null).
	This Value
	// Args are the positional arguments; their number must match the slot.
	Args []Value
	// Document is the ambient document, or nil for none. Whether it is
	// writable is decided by the slot.
	Document *Document
	// Context holds the Context global of method bodies (R-JS-17/18).
	Context *Dict
	// Schemas are descriptors the evaluation may need; misses are looked up
	// through Host.LookupSchema.
	Schemas []*SchemaDescriptor
	// Host serves callbacks; nil uses Options.Host.
	Host Host
	// Budget bounds the evaluation.
	Budget Budget
}

// ExprRequest evaluates one j: expression binding (R-JS-30..36).
type ExprRequest struct {
	// Expression is the attribute value: one JavaScript expression, which
	// may use await.
	Expression string
	// Scope holds the visible bindings (earlier bindings of the element and
	// ancestor bindings, nearest last). Names that are identifiers become
	// bare variables; all of them, and request, are properties of Context.
	Scope *Dict
	// Request is the request object; nil leaves `request` undefined.
	Request *Request
	// Document is the read-only ambient document (the host subtree).
	Document *Document
	Schemas  []*SchemaDescriptor
	Host     Host
	Budget   Budget
}

// Null is the JavaScript null for CallRequest.This.
var Null Value = nullValue{}

type nullValue struct{}

// ContextWrite is one assignment to (or deletion from) the Context global.
type ContextWrite struct {
	Name    string
	Value   Value
	Deleted bool
}

// Result is a successful evaluation.
type Result struct {
	// Value is the settled, marshalled result (R-JS-41).
	Value Value
	// Document is the serialized ambient document (or element, for an
	// element-rooted Document) after the binding ran, when the slot's
	// document is writable and the binding changed it.
	Document        string
	DocumentChanged bool
	// ContextWrites lists the Context assignments in order. They were also
	// sent to Host.WriteContext as they happened; callers use one or the
	// other.
	ContextWrites []ContextWrite
	// Tainted reports that a per-user member of `request` was read
	// (R-JS-34): the response must be Cache-Control: private.
	Tainted bool
	Stats   Stats
}

// Host serves the coarse callbacks a worker makes during an evaluation. The
// schema and compose packages implement it; StubHost is a test double.
// Callbacks run on the calling goroutine of CallModule/EvalExpression while
// the JavaScript thread waits.
type Host interface {
	// LookupSchema returns the descriptor of the registered schema whose
	// governed type URL is typeURL, or nil when there is none
	// (unknown-schema).
	LookupSchema(ctx context.Context, typeURL string) (*SchemaDescriptor, error)
	// CallMethod runs a method implementation the worker cannot run itself
	// (a Sessel body) with the given receiver and arguments. A returned
	// error is thrown into JavaScript as an Error with its message.
	CallMethod(ctx context.Context, call *MethodCall) (*MethodResult, error)
	// WriteContext applies an assignment to (or deletion from) the Context
	// global, synchronously, in program order.
	WriteContext(ctx context.Context, w ContextWrite) error
}

// MethodCall is a cross-language method invocation from JavaScript.
type MethodCall struct {
	Type     string // schema type URL declaring the method
	Method   string
	Static   bool
	Receiver Value // *Instance, or *Class for static methods
	Args     []Value
}

// MethodResult is the outcome of a Host method call.
type MethodResult struct {
	Value Value
	// ContextWrites are Context changes the method made, applied to the
	// JavaScript Context before the call returns.
	ContextWrites []ContextWrite
}

// SchemaDescriptor describes a registered schema for class synthesis
// (R-JS-85).
type SchemaDescriptor struct {
	// Type is the governed type URL (the import specifier).
	Type string
	// Name is the class name; empty means the last path segment of Type
	// (the text after the last ':' for URLs without a path).
	Name string
	// Parent is the parent schema's type URL; MapSchemaType makes the class
	// extend Map.
	Parent     string
	Properties []PropertyDescriptor
	Methods    []MethodDescriptor
}

// PropertyDescriptor is a declared property.
type PropertyDescriptor struct {
	Name string
	// Many marks an explicit 0..n / 1..n cardinality (values are lists).
	Many bool
}

// MethodDescriptor is a declared method.
type MethodDescriptor struct {
	Name   string
	Static bool
	Params []string
	// Language is the implementation itemtype; LanguageURL bodies run in the
	// worker from Source, anything else goes to Host.CallMethod.
	Language string
	Source   string
}

// className returns the class name of a schema (R-JS-85).
func (d *SchemaDescriptor) className() string {
	if d.Name != "" {
		return d.Name
	}
	return shortName(d.Type)
}

func shortName(u string) string {
	s := strings.TrimRight(u, "/")
	if i := strings.Index(s, "://"); i >= 0 {
		rest := s[i+3:]
		if j := strings.LastIndexByte(rest, '/'); j >= 0 {
			return rest[j+1:]
		}
	}
	if j := strings.LastIndexAny(s, ":/"); j >= 0 {
		return s[j+1:]
	}
	return s
}
