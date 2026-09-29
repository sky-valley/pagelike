// Package compose implements server-side page composition on the public
// plane (docs/spec/composing.md, docs/spec/liquid.md §3–§4): namespaces and
// directive stripping, resource (r:), Sessel (e:) and JavaScript (j:)
// bindings, p:template (Liquid), <p:include>, <p:stamp>, schema method
// elements and attributes, transient (session-scoped) elements,
// parameterized routes, pagination, the Request Document, XML documents,
// write-through of projected elements, and templated resource creation.
//
// # Wiring
//
// Register (run from init through server.Extend; the package is linked by
// internal/features) installs:
//
//   - Engine.Compose as httpapi.Public.Compose (engine.Composer): every
//     public read of an HTML/XML document (GET, HEAD, edge QUERY, JSON-LD
//     negotiation) is composed first;
//   - Route as httpapi.Public.Route: parameterized routes;
//   - engine.ComposeWrite: selector writes resolve against the composed
//     view and route to the origin of projected elements, to the session
//     for transient elements, or answer 416 for generated content;
//   - engine.TemplateCreate: POST without Range to a template.
//
// # Source preservation
//
// Composition never re-serializes a document it does not change: the
// composed markup is built by copying the stored bytes and splicing in
// directive results (decision 0003 §6, live observation LO-1). A walk over
// the parsed tree, guided by each element's source span (dom.ComputeSpans),
// copies the source between elements verbatim; a kept element loses only
// its directive attributes (and, in HTML, its xmlns:* declarations), with
// the rest of its start tag untouched. Template output is spliced as the
// text Liquid produced; included and stamped elements are copied from their
// origin document's bytes. Composed output that equals the stored bytes is
// reported unchanged, so the engine serves the stored document. The served
// HTML is then parsed as a whole to give the composed view that selector
// reads, JSON-LD, QUERY and writes use — the same tree a browser builds
// from the served text (R-COMP-180); a single-element selector read serves
// the element's composed bytes (engine.Composed.Fragment). A changed XML
// document is served in PageLove's XML dialect (xmldom serialization:
// empty elements self-close, R-COMP-126).
//
// Markup whose elements cannot be matched to their source text in order
// (foster parenting of elements) is first replaced by its serialization;
// if even that cannot be walked, composition fails with a
// CompositionError rather than dropping directives.
//
// # Provenance
//
// While writing the composed text the walk records, for every element
// copied from a stored document, where it starts in the output and which
// stored element it is (document path and child-index path). Selector
// writes use these anchors: a native element is written in place, a
// projected one in its origin (engine.ApplyRouted: authorized against the
// page only, validated, triggered, ETag-checked and announced on the
// origin; WriteCtx.Via names the page), an element without an anchor
// (template output, constructed values, the Request Document) is not
// writable (416). Transient elements are written to the session store.
//
// # Extension points
//
//   - JSRunner: server JavaScript for j: bindings and JavaScript methods,
//     installed by internal/jsglue (the internal/jsrt integration); without
//     a runner those slots fail with 501 JavaScriptUnavailable.
//   - MethodDispatcher: schema method dispatch. internal/jsglue installs
//     one over the schema registry (schema.DispatchMethod); SchemaMethods,
//     the fallback, reads Schema/Method items from the snapshot and runs
//     Sessel implementations itself.
//
// Budgets: every Sessel evaluation and Liquid render draws on the request
// budget (internal/budget); every directive dispatch costs one of the 500
// dispatch units of a request (R-COMP-27), and exhausting either fails the
// request with 503.
package compose
