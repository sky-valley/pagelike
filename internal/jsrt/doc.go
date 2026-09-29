// Package jsrt runs PageLove-compatible server-side JavaScript
// (docs/spec/javascript.md Part A) in bounded worker processes, following
// docs/decisions/0001-server-js-engine.md.
//
// # Isolation
//
// JavaScript never runs in the serving process. A Runtime owns a pool of
// worker processes: the same binary started as "<binary> jsrt-worker" with a
// marker in its environment. main (cmd/pagelike) and TestMain functions
// call MaybeRunWorker first; this package's init does the same for the
// environment marker, so any binary that links it (the pagelike binary, the
// harness, go test binaries) works as a worker. Limits are enforced in three
// layers:
//
//  1. Engine: every evaluation gets a fresh QuickJS runtime and context
//     (modernc.org/quickjs, pure Go) with a memory limit (the remaining
//     memory budget, default 16 MiB, on top of the runtime's own baseline),
//     a stack limit (DefaultStackSlots; recursion becomes a catchable
//     "InternalError: stack overflow", never a Go stack overflow) and an
//     absolute deadline checked by the engine's interrupt handler. These
//     give the precise failure variants.
//  2. OS: the worker sets RLIMIT_CORE=0, RLIMIT_FSIZE=0, RLIMIT_NOFILE=32
//     and RLIMIT_CPU on itself, clears its environment and changes to /; on
//     Linux it also bounds RLIMIT_AS and sets no_new_privs. It exits when its
//     peak RSS passes Options.RSSLimit.
//  3. Parent: the pool SIGKILLs a worker's process group when a call runs
//     past its budget plus Options.Grace (host callback time excluded) or
//     its RSS passes the ceiling, reports the call as timeout or
//     out-of-memory, and respawns the worker. Workers are also recycled
//     after Options.MaxCalls calls or a high peak RSS.
//
// A worker evaluates on one long-lived goroutine. After each reply it
// prepares the next fresh context (runtime, context, prelude from cached
// bytecode, and the DOM chunk when the last call used the DOM) so the next
// call only pays for its own evaluation; the used context is freed on
// another goroutine. Options.ParallelPrepare moves preparation onto a second
// core for saturated workers.
//
// JavaScript sees no filesystem, network, timers, console or host module:
// the only host functions are the DOM operations and the coarse callbacks
// below.
//
// # Calling JavaScript
//
//	rt := jsrt.New(jsrt.Options{Host: host}) // or jsrt.Default()
//	res, fail := rt.CallModule(ctx, jsrt.CallRequest{
//		Source:   src,               // the binding item's module source
//		Slot:     jsrt.SlotValidate, // the calling convention (slots.go)
//		This:     nil,               // nil = undefined; jsrt.Null for null
//		Args:     []jsrt.Value{value},
//		Document: &jsrt.Document{Node: doc, Source: "/page.html"},
//		Budget:   jsrt.Budget{Time: remaining, Memory: memLeft},
//	})
//	res, fail = rt.EvalExpression(ctx, jsrt.ExprRequest{
//		Expression: `total * 2`,     // a j: attribute value
//		Scope:      jsrt.NewDict("total", 60),
//		Request:    &jsrt.Request{Method: "GET", Path: "/p"},
//	})
//
// Exactly one of res and fail is non-nil. A *Failure carries the
// BindingFailure variant (VariantParse, VariantShape, VariantThrew,
// VariantTimeout, VariantOutOfMemory, VariantMarshal, VariantReturnType,
// VariantImportNotAllowed, VariantUnknownSchema, or VariantInternal for the
// runtime's own failures), the message, the QuickJS stack ("    at default
// (eval:2:17)"), and, when the binding threw an HTTPResponse object in a slot
// that honours one (@write, @validate, trigger/processor actions), the
// Response to send. Failure.HTML renders the BindingFailure item;
// Failure.Budget tells budget exhaustion (503/507) from ordinary failures.
//
// The Slot decides the calling convention (R-JS-10..23): how many arguments
// there are, whether the ambient document exists and is writable (only in
// SlotDefault; construction is always writable), whether the Context global
// exists (methods and j:), and whether a thrown HTTPResponse is honoured.
// The caller supplies `this` and the arguments in the slot's documented
// shape (for example SlotDefault: This = dictionary of set fields, Args =
// [{document_html}]).
//
// A Document is either a document node or an element: an element becomes the
// documentElement of the binding's document (the in-progress instance for
// default, the host subtree for composition dispatch). Element values that
// are nodes of the request's Document keep their identity in JavaScript
// (this === document.documentElement for a dispatch). When the slot's
// document is writable and the binding changed it, Result.Document holds the
// serialized document (or element), for the caller to splice.
//
// # Values
//
// Values cross the boundary in a JSON-compatible model (value.go): nil, bool,
// int64, float64, string, []Value, *Dict (ordered), *Element, *Instance, and
// as inputs also Undefined, *Class, Temporal, *Headers and *Request. Results
// follow R-JS-41: integral numbers within ±(2^53−1) are int64, other finite
// numbers float64; Map becomes a Dict, Set a list, other objects a Dict of
// their own enumerable properties; elements and NodeLists become *Element
// values (outer HTML, and $source when the element belongs to a stored
// document); instances of imported schema classes become *Instance.
// Functions, symbols, cycles, BigInt, non-finite numbers, nesting deeper than
// 64 levels and non-element nodes fail with VariantReturnType. MarshalJSON
// encodes values as application/sessel+json ({"$type":"element","$html":…,
// "$source":…}).
//
// # Host callbacks
//
// The worker serves every DOM operation itself (it parses the shipped HTML
// with internal/dom or internal/xmldom and matches selectors with
// internal/selector). Only coarse operations call back to the Host over the
// same pipe, synchronously, while the JavaScript thread waits:
// LookupSchema (a schema import or instance type not in
// CallRequest.Schemas), CallMethod (a method whose implementation is not
// JavaScript, such as a Sessel body, called from JavaScript) and
// WriteContext (an assignment to Context). Pass the ctx a callback receives
// on to evaluations it starts: a nested evaluation then uses an overflow
// worker instead of waiting for the pool slot its caller holds. StubHost is
// a test double.
//
// # Schema imports
//
//	import Note from "https://x.example/Note" with { type: "https://pagelove.org/Schema" };
//
// is the only import a module may make. A lexical pre-scan checks the import
// forms and attributes (the engine's loader receives only specifiers), and
// the module normalizer admits only the schema URLs the scan approved. An
// import resolves to a generated class (R-JS-85/86): named after the schema,
// extending its parent's class or Map, with accessors for the declared
// properties and methods whose JavaScript bodies run in the same context and
// whose other bodies go to Host.CallMethod.
package jsrt
