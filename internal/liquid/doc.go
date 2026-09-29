// Package liquid renders PageLove-compatible Liquid templates: the content
// of elements that carry `p:template="text/liquid"`. The behavioural
// contract is docs/spec/liquid.md (requirements R-LIQ-*); the engine choice
// and its evidence are docs/decisions/0002-liquid.md.
//
// The package is built on github.com/osteele/liquid v1.9.2, using its
// lower-level packages (parser, render, expressions, values) and none of
// its facade, standard tags or standard filters. pagelike supplies:
//
//   - the tag set (tags.go): Shopify semantics for for/tablerow/case/cycle,
//     budget charging in every tag, and include/render refused because
//     templates perform no I/O;
//   - the PageLove filter set (filters*.go), with R-LIQ-101 coercion;
//   - a source preprocessor (preprocess.go) and expression rewriter
//     (expr.go) for what the library's grammar cannot express: inline error
//     degradation per output, numeric-string comparison, blank/empty,
//     keyword-only filter arguments, right-to-left and/or, `{% liquid %}`;
//   - the value model: *Item, *Hash, *Request, SafeString (values.go);
//   - a per-request Budget.
//
// # API
//
//	eng := liquid.New(liquid.Options{})           // one per process; safe for concurrent use
//	budget := liquid.NewBudget(liquid.Limits{})    // one per HTTP request
//	out, err := eng.RenderFor(ctx, liquid.HostOf(host), src, vars, budget)
//
// Render(ctx, src, vars, budget) is RenderFor for an ordinary HTML host.
// Compiled templates are cached by source and host context.
//
// Values passed in vars:
//
//	nil, bool, string                      as is
//	integers                               int (int64 is accepted)
//	floats                                 float64; integral floats render "100.0"
//	lists                                  []any
//	hashes                                 *Hash keeps key order; map[string]any sorts keys
//	bound elements                         *Item, from NewItem(documentPath, element)
//	`request`                              *Request, from NewRequest or RequestFromHTTP
//
// DecodeJSON converts JSON (a `j:` binding's result) into these values:
// integral numbers become int, as R-LIQ-59 requires.
//
// # How composition calls it
//
//  1. Template source. src is the host element's inner markup exactly as
//     it is written in the source the host came from (the stored document
//     bytes, an included fragment's origin, a transient's session copy):
//     the bytes from the end of the start tag to the start of the end tag
//     (R-LIQ-10). It must be taken before HTML parsing, from dom spans or
//     the tokenizer's raw text, never from a re-serialized DOM: an HTML
//     parse foster-parents `{% for %}` out of <table>/<tr> (the polls app's
//     new-poll template loops between table cells; decision 0003, "Liquid
//     inside tables"), and re-serializing changes entities and quotes that
//     Liquid then sees. Nested hosts are part of the outer source and are
//     not rendered again (R-LIQ-7). Inside `{{ }}` and `{% %}`, character
//     references are decoded where the document's HTML parser would decode
//     them, as live PageLove does (R-LIQ-13); literal text is left as is.
//
//  2. Host. HostOf(host) tells the engine the host's name and whether the
//     document is XML. A failed `{{ }}` renders an inline
//     https://pagelove.org/Error item only where markup may appear; inside
//     attributes, comments, raw-text elements (a <title> or <script> host
//     included) and XML CDATA or processing instructions it renders
//     nothing (R-LIQ-201).
//
//  3. Variables. vars holds the Context names visible at the host —
//     bindings on the host and its ancestors, nearest declaration winning —
//     plus `request` (R-LIQ-40). A resource binding is a []any of *Item in
//     site-graph order, [] when nothing matches (R-LIQ-51). Build one
//     *Request per HTTP request, pass the same one to every template of the
//     page, and after composing check Request.Private: a template that read
//     request.auth or request.headers (or serialized or iterated the whole
//     request) makes the response `Cache-Control: private` (R-LIQ-64).
//     Each call to Render is a fresh scope (R-LIQ-45).
//
//  4. Budget. Create one Budget per request with the site's Limits and pass
//     it to every Render of the request; other runtimes can charge the same
//     Budget (Charge, ChargeMemory) so the request has one allowance
//     (R-LIQ-210). Budget.Usage feeds the X-Budget-Consumed-* headers.
//     The first render fixes the request instant that "now" means.
//
//  5. Output. The result replaces the host's children; the host element
//     stays, minus its template and dispatched directive attributes
//     (R-LIQ-30). Splice the output into the document source in place of
//     the host's content and re-parse (R-LIQ-31): re-serializing a parsed
//     fragment instead turns `&#39;` into `'`, `&copy;` into `©` and
//     `<br />` into `<br>`, which the liquid harness cases check. Then keep
//     composing into the output (R-LIQ-23). Output is never auto-escaped
//     (R-LIQ-90).
//
//  6. Errors. An error in a single output expression never reaches the
//     caller: it degrades inline and the page still composes (R-LIQ-200).
//     Anything else fails the render with an *Error and no partial output:
//     KindTemplate for syntax errors and errors in conditions, loop
//     collections and assign (500), KindBudget for budget exhaustion (503).
//     Error.ErrDoc builds the error document; Error.Line is the line in the
//     template source.
//
// Liquid runs only inside template hosts: never route AuthorizationRule
// fields, binding expressions or other attribute values through this
// engine (R-LIQ-8).
//
// # Dialect notes
//
// Where PageLove's exact behaviour is unconfirmed, the package follows the
// spec's compatibility decisions (docs/spec/liquid.md §24): numbers compare
// with numeric strings; `reversed` applies after offset and limit;
// tablerow writes Shopify's markup; `case` renders every matching `when`;
// `.size` counts code points; undefined names are nil; include and render
// are syntax errors. The inline Error markup, the diceware word list
// (pagelike's own, 1,939 words) and the random symbol set are pagelike's.
package liquid
