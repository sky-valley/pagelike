// Package jsglue connects the server JavaScript runtime (internal/jsrt) to
// the feature packages that evaluate JavaScript: schemas (defaults,
// resolvers, validators, computed properties, methods, Group includes()),
// composition (j: expression bindings, JavaScript method elements) and
// reactions (trigger and processor gates and actions, dynamic HttpRequest
// properties). It is the integration layer of docs/architecture.md: the
// feature packages only declare their JSRunner seams and the language
// runtime only declares jsrt.Host; this package implements both sides.
//
// # Wiring
//
// The package installs itself when it is linked (internal/features imports
// it): schema.SetJSRunner, compose.SetJSRunner, reactions.SetJSRunner, the
// schema registry's method dispatcher for composition
// (compose.SetMethodDispatcher, so method elements see the same schemas,
// overloads and doesNotUnderstand as Sessel) and schema.SetKeyID for
// Pagelove.PUT side effects (reactions.SetKeyIDFunc). Install and
// Uninstall switch the seams on and off (tests that pin the behaviour of a
// build without JavaScript).
//
// # Runtime
//
// One jsrt.Runtime serves the whole process. It is created on first use
// with the pool size set by SetWorkers (pagelike serve --js-workers; 0
// means GOMAXPROCS); workers are the running binary started in worker mode.
//
// # Calls
//
// Every evaluation draws on the request's shared budget (internal/budget):
// its time limit is budget.From(ctx).Remaining(), its memory limit what the
// request has left (at most jsrt.DefaultMemory), and the work it reports
// (one operation per evaluation plus DOM operations and host callbacks, and
// the heap it used) is added with AddJS. An exhausted budget fails before a
// worker is taken, with variant timeout or out-of-memory.
//
// Values cross in the jsrt model. Sessel values (composition, Context,
// cross-language calls) and the schema package's JSON-like model convert
// both ways: elements keep their node identity where the runtime supports
// it (the dispatching element is `this` and the root of `document`),
// elements of registered schemas become instances of the imported classes,
// instances returned by JavaScript become microdata items.
//
// jsrt.Host is served per call: LookupSchema describes schemas of the
// registry of the request's snapshot (class synthesis for schema imports),
// CallMethod runs Sessel method bodies that JavaScript reaches through
// imported classes, and WriteContext writes Context assignments back to the
// calling pass's Context (methods called from triggers, processors and
// composition).
//
// # Failures
//
// Failures map onto each package's error model: schema gets
// *schema.BindingFailure (or *schema.JSThrownResponse for an HTTPResponse
// thrown where the slot honours it), reactions *reactions.JSError (or
// *reactions.ThrownResponse), composition an *errdoc.Error: 500 carrying
// the BindingFailure item, 503 for budget exhaustion (R-JS-52, R-JS-57).
package jsglue
