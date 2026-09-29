# Composing pages — behavioral specification

Area: `composing` (requirement prefix `R-COMP`). Harness cases:
`harness/cases/composing/*.yaml` (ids `comp.*`). Package: `internal/compose`
(with `internal/selector`, `internal/liquid`, `internal/sessel`, `internal/jsrt`).

This document specifies server-side page composition on the public plane:
the namespaces and directive syntax, the composition pipeline and its order,
bindings (`r:`, `e:`, `j:`), templates as a pipeline stage (`p:template`),
method elements (element and attribute forms), `<p:stamp>`, `<p:include>`,
write-through to origin resources, transient (session-scoped) elements,
parameterized routes, pagination, templated resource creation, XML
documents, the Request Document as a composition input, caching headers of
composed responses, composition errors, and the PageLove selector extensions
(pseudo-classes and selector functions) that every selector-accepting surface
uses. Liquid's language and filter library, Sessel's language, the server
JavaScript runtime, schema declarations and processors are specified by their
own areas; they appear here only where composition depends on them.

---

## 0. Conventions

**Normative words.** MUST / SHOULD / MAY as in RFC 2119. "pagelike MUST" marks
a compatibility decision where sources are silent or disagree.

**Evidence levels.** `documented` (public docs, snapshot 2026-09-28),
`client-source` (beta-js), `demo-source` (official apps), `live-observed`
(responses captured from real PageLove sites on 2026-09-28, see [OBS]),
`inferred` (reasoned; the reason is given).

**Source keys.** The combined page `docs.pagelove.com_all_reference_composing-pages.md`
is identical to the individual pages except that four individual pages
(Method-Elements, Stamp, XML-Documents, Parameterized-Routes) add a one-line
lead-in before each `PUT` example ("Say `/x.html` contains", "With `/stamp-demo.html`
set to", …). No normative difference exists between them.

| Key | Source |
|---|---|
| [CP] | docs `reference/composing-pages` (group index) |
| [RB] [EB] [JB] | docs `reference/composing-pages/{Resource-Binding,Expression-Binding,JavaScript-Expression-Binding}` |
| [PAG] [RC] [SX] [ME] [ST] | docs `reference/composing-pages/{Pagination,Resource-Creation,Selector-Extensions,Method-Elements,Stamp}` |
| [XML] [INC] [TR] [PR] | docs `reference/composing-pages/{XML-Documents,Includes,Transient-Elements,Parameterized-Routes}` |
| [RD] | docs `reference/reading-and-writing/Request-Document` |
| [TPL] [LQF] | docs `languages/liquid/templating`, `languages/liquid/filters` (+ `/array`, `/data`) |
| [SES] [SEQ] [SEE] | docs `languages/sessel/reference/syntax` §Context variables; `languages/sessel/learn/querying`; `languages/sessel/reference/element` (`.microdata()`, `<template>` inert boundary) |
| [SLC] | docs `languages/sessel/learn/composition` / `learn/construction` (`p|transient` construction) |
| [PROP] [METH] | docs `reference/modeling-data/{Property §Reading a multi-valued property, Methods}` |
| [PROC] | docs `reference/reacting-to-changes/Processor` |
| [SSE] [GET] [POST] [UP] | docs `reference/reading-and-writing/{Server-Sent-Events §Composed resources, GET-method §Caching, POST-method §Concurrency, Uploading-Files}` |
| [QRY] [WD] [AZR] | docs `reference/protocol/{QUERY,WebDAV}`, `reference/permissions/AuthorizationRule` |
| [BLOG] [APP] | docs `learn/build-a-blog`, `learn/your-first-app` |
| [R-PAG] [R-WID] [R-PRIM] | docs `recipes/{paginating-a-list,embedding-a-third-party-widget,reading-from-the-primitives-layer}` |
| [SHOP] | `pagelove-shop@d887054` `site/{index,basket,checkout,partials}.html`, `site/products/:slug.html`, `site/orders/:id.html`, `site/admin/*.html`, `site/js/shop.mjs`, `README.md` |
| [ATS] | `pagelove-ats@8f200fc` `site/roles.html`, `site/roles/:role_name/index.html`, `site/index.html`, `site/admin/auth.html` |
| [POLL] | `pagelove-polls@c9270e5` `site/templates/new-poll.html`, `site/polls/kfd47o4zqd.html`, `site/index.html`, `site/admin/auth.html` |
| [KAN] | `pagelove-kanban@85109ab` `site/app.js` |
| [CURSOR] | `pagelove-cursor@b97c3ef` `plugins/pagelove/skills/pagelove-dev/SKILL.md` (agent skill, April 2026 — older platform) |
| [OBS] | private research notes of 2026-09-28 (not published): `secondary-index.json`/`index.json` (response headers of GETs to `blog.pagelove.com` and `ats.pagelove.com`), saved bodies `blog.pagelove.com_*.html`, `ats.pagelove.com_roles_founding-software-engineer.html` |

Third-party material is cited as data. Quotes are at most two lines.

**Terms.**
- *Document*: a stored HTML or XML resource (area `reading-writing` R-RW-1).
- *Site graph*: every stored HTML/XML document of the site, as **stored**
  (canonical, uncomposed, without session content), plus the per-request
  Request Document (R-COMP-120). Blobs are not part of it.
- *Requested document*: the stored document composition starts from — the
  document at the request path, or the route template (R-COMP-100).
- *Composed view*: the DOM that results from running this area's pipeline
  over the requested document for the current request. Reads serialize it;
  selector reads and writes resolve against it.
- *Directive*: any element or attribute whose prefix resolves to a namespace
  this area recognises (R-COMP-10) or to a registered schema (R-COMP-60).
- *Context*: the name→value environment composition evaluates in (R-COMP-20).
- *Projected element*: an element placed into the composed view by
  `<p:include>` or `<p:stamp>` that keeps a link to its origin (R-COMP-90).

---

## 1. When composition runs

**R-COMP-1 Scope.** Composition runs on the public plane for every read of an
HTML or XML document (GET, HEAD, the edge `QUERY`, content negotiation to
JSON-LD; SSE streams are out of scope, area `sse`), for resolving the target of every
selector write (PUT/POST/DELETE with `Range: selector=`), and for templated
resource creation (POST without `Range`, R-COMP-140). It never runs on the
WebDAV authoring plane (raw bytes), on blobs, or on Sessel/JS reads of stored
documents (`${…}` selectors, `r:` bindings, includes read **stored** documents).
- Evidence: documented ([GET], [PAG] §With Range selectors, [QRY] §On the edge
  proxy, [RB] "evaluated server-side during composition", [TR] "Bindings query
  the canonical document", [UP] "Blobs are not parsed, composed"). Cross-area:
  protocol R-PROTO-113 (dav GET is never composed). Confidence: high.
- Edge: a selector read runs the **whole** pipeline first, then extracts; any
  composition error therefore fails a fragment read too (R-COMP-160).

**R-COMP-2 Per request, never cached.** Bindings, includes, stamps, templates
and routes are evaluated on every request against the current committed state
("At processing time, not cached", [RB] §Semantics). A change to a bound or
included document is visible on the very next read of the composing page.
pagelike evaluates against one consistent read snapshot per request (plan
§Read path).
- Evidence: documented. Confidence: high. Case: `comp.rb.not-cached`.

**R-COMP-3 No opt-in required (compat decision).** pagelike runs the pipeline
for every HTML/XML document, whether or not it declares a Pagelove namespace.
A document with no directives composes to itself (serialization rules of
R-COMP-14 still apply).
- A plain page such as the [APP] shopping list (no directives) is served and
  written exactly as stored; every element is *native* (R-COMP-90).
- Evidence: contradiction C-1. [RB] §Namespace declaration says server-side
  processing "must also be enabled" by declaring `https://pagelove.org/1.0`;
  but [ME] §Error cases makes a page whose only declaration is misspelled
  fail with 500 (so composition ran without any declaration), and [EB]/[JB]
  examples use `pagelove:template` with no visible `xmlns:pagelove`.
  Confidence: low. Probe Q-1.
- Edge: an XML document "with no Pagelove namespaces and no microdata is served
  unchanged" ([XML] §Composition) — pagelike MUST return its stored bytes
  verbatim (R-COMP-128).

---

## 2. Parsing and serialization requirements

**R-COMP-5 Source-preserving parse.** The composition input is parsed with a
source-preserving HTML parser: no parser-implied `<html>/<head>/<body>/<tbody>`
are inserted, comments and inter-element whitespace are kept, and **elements
and text are never foster-parented** out of `<table>` structures. Concretely a
`<p:include>` written as a child of `<table>` resolves in place: the served
output has the included `<tbody>` inside that table.
- Evidence: live-observed ([OBS] ATS roles page lines 75-76:
  `<table id="all-roles-data" hidden>` immediately followed by the included
  `<tbody id="roles-body">`; source [ATS] `roles/:role_name/index.html:75`
  writes `<table …><p:include … selector="#roles-body"></p:include></table>`),
  demo-source ([POLL] `templates/new-poll.html:56-84` puts Liquid `{% for %}`
  inside `<thead><tr>` and the stored result [POLL] `polls/kfd47o4zqd.html:57-72`
  shows the rendered cells inside the row), documented (reading-writing
  R-RW-21: no implied `<head>`). Confidence: medium.
- Contradiction C-2: [CURSOR] ("Liquid templates and HTML `<table>` are
  incompatible … foster parenting moves … Liquid tags outside") describes an
  older platform; the September 2026 apps and live output contradict it.
  Decision: no foster parenting.
- Implementation note: x/net/html's tree builder foster-parents and inserts
  implied elements; `internal/dom` must build its tree from the tokenizer
  (or post-process) so that element positions match the source.

**R-COMP-6 Self-closing prefixed elements.** A start tag whose name contains
a `:` and ends with `/>` (e.g. `<p:include selector="#nav" resource="…" />`)
is an empty element: the following siblings are **not** its children.
- Evidence: documented ([INC] §Basic include: the page
  `<p:include … />` followed by `<main>…</main>` composes to the header
  followed by the intact `<main>`). Confidence: medium (HTML5 would make
  `<main>` a child and lose it). Case: `comp.ns.self-closing-prefixed-element`.

**R-COMP-7 Tag and attribute names.** Directive recognition splits a name at
its first `:` into prefix and local name. HTML element names are compared
ASCII case-insensitively (the parser lowercases them). For attribute-form
directives the local name becomes a variable name (R-COMP-30); pagelike MUST
preserve the source case of that local name (`e:myVal` binds `myVal`).
- Evidence: inferred ([RB] §Trigger context binds `r:apiKey` and reads
  `apiKey`); PageLove behavior unknown. Confidence: low. Probe Q-2.
- Edge: XML documents are case-sensitive throughout ([XML] §The XML dialect).

**R-COMP-8 Template source is raw text.** The Liquid source of a
`p:template` host is the host element's inner markup **as stored** (or as
produced by earlier composition steps), not a re-serialization of a DOM that
could have moved text. `{% … %}`/`{{ … }}` inside `<title>`, `<table>`, `<tr>`
and attribute values are therefore seen by Liquid exactly where written.
- Evidence: demo-source ([POLL] `new-poll.html:6-9` Liquid in `<title>`,
  `:34` Liquid in `itemtype="{{ … }}"`, `:60-84` loops in table rows; stored
  output confirms correct rendering). Confidence: medium.
- Edge: Liquid sees everything inside the host, including `<script>` and
  `<style>` text; JavaScript template literals or `{{` in scripts must be
  wrapped in `{% raw %}…{% endraw %}` ([CURSOR] §Wrap JavaScript). A host with
  no Liquid markup renders to itself.

**R-COMP-9 Serialization of the composed view (HTML).** The response body is
the composed view serialized with the stored document's structure: comments
kept, whitespace text nodes around removed/replaced nodes kept (a removed
element leaves its neighbouring whitespace), attribute order kept, directive
attributes and namespace declarations removed per R-COMP-14. The host element
of a template, binding or pagination directive is kept with its other
attributes (R-COMP-51).
- Evidence: documented ([PAG] example keeps the blank lines of removed `<li>`s;
  [TPL] example keeps whitespace of `{% for %}` lines), live-observed ([OBS]
  blog pages keep HTML comments and the `<section class="excerpts">` host).
  Confidence: high.

---

## 3. Namespaces, prefixes and stripping

**R-COMP-10 Recognised namespace URIs.** Matching is exact string equality on
the `xmlns:<prefix>` attribute value (no trailing-slash, case or scheme
tolerance):

| URI | Conventional prefix | Provides |
|---|---|---|
| `https://pagelove.org/1.0` | `p` (also `pagelove`) | elements `include`, `stamp` (built-in methods); attributes `template`, `paginate`, `transient` |
| `https://pagelove.org/Binding/CSS` | `r` (also `resource`) | resource bindings (R-COMP-30) |
| `https://pagelove.org/Binding/Sessel` | `e` | Sessel expression bindings (R-COMP-35) |
| `https://pagelove.org/Binding/JavaScript` | `j` | JavaScript expression bindings (R-COMP-40) |
| any URI equal to a registered Schema `type` | any | method elements / attributes (R-COMP-60) |

- Evidence: documented ([RB], [EB] "must be exactly", [JB] "must be
  exactly", [INC] §Namespace declaration, [ST] §Form, [ME]). Confidence: high.
- Legacy alias (compat decision): pagelike MUST also accept
  `https://pagelove.org/1.0/Resource` as an alias of `Binding/CSS`
  ([CURSOR] "Resource namespace MUST include `/1.0/`", older platform).
  Confidence: low. Probe Q-3.

**R-COMP-11 Prefixes are free.** Any XML-valid prefix may be bound to any of
these URIs; recognition is by URI, never by prefix name (`xmlns:foo="https://pagelove.org/1.0"`
makes `<foo:stamp>` a stamp). Several prefixes may bind the same URI.
- Evidence: documented ([RB] "The prefix can be any valid XML prefix (`r`,
  `resource`, etc.)"; [EB], [JB], [INC] same). Confidence: high. Case:
  `comp.ns.prefix-is-arbitrary`.

**R-COMP-12 Declaration scope.** An `xmlns:<prefix>` attribute binds the prefix
for the declaring element and its whole subtree; the nearest declaration wins.
Declarations may sit on `<html>`, `<body>` or any ancestor (or on the
directive element itself, e.g. `<ul xmlns:p="…" p:paginate="10">`).
- Evidence: documented ([ME] §Element form "scoped to its declaring element's
  subtree"; [PAG] attribute form; [R-PAG] "on the element (or an ancestor)").
  Confidence: high.
- Undeclared `pagelove:` prefix (compat decision): pagelike SHOULD treat an
  undeclared `pagelove:` prefix as bound to `https://pagelove.org/1.0`, because
  [EB], [JB] and [TPL] examples use `pagelove:template` with no visible
  declaration. All other undeclared prefixes are unbound. Confidence: low.
  Probe Q-1. Case: `comp.tpl.undeclared-pagelove-prefix` (low).
- Content spliced by includes/stamps/templates is evaluated with the
  including page's scope **overlaid by** the origin element's in-scope
  declarations (R-COMP-84) (inferred).

**R-COMP-13 Unbound and unknown prefixes.**

*Row 1, element form, superseded live 2026-09-29: an element whose prefix has
no in-scope declaration is an ordinary element, kept in place (200), like the
attribute form (`comp.ns.unbound-element-500`,
docs/compat/decisions-2026-09-29/serialization.md).*

| Situation | Element form `<x:name>` | Attribute form `x:name="v"` |
|---|---|---|
| prefix `x` has no in-scope declaration | composition fails **500** `no method found for x:name and no doesNotUnderstand defined` | nothing happens; attribute kept verbatim in output |
| `x` bound to a URI that is neither a Pagelove namespace nor a registered schema, or a schema lacking the method and `doesNotUnderstand` | 500 (same message) | attribute silently **stripped**, no dispatch, no error |
| `x` bound to `https://pagelove.org/1.0` with an unknown local name | 500 `unknown method '<name>' on Pagelove namespace` | stripped (inferred, attribute form never errors) |
| `x` bound to a Binding URI | n/a (bindings exist only in attribute form, [JB] §Only the attribute form); an element `<e:foo>` is a method element of that built-in schema → dispatched to its `doesNotUnderstand` (inferred) | binding created |

- Evidence: documented ([ME] §Error cases, §`doesNotUnderstand` fallback; [ST]
  §Error cases). Confidence: high (rows 1-2, element), medium (row 2
  attribute for non-schema URIs: [EB] says a wrong URI means "Attributes are
  ignored — no bindings are created", which does not say stripped or kept),
  low (rows 3-4 attribute).
- Contradiction C-3: [CURSOR] says with a wrong resource namespace
  "attributes won't be stripped". Decision: stripped (current [ME] table).
  Probe Q-4.
- Never dispatched or stripped: `xml:*`, `xlink:*`, `xmlns`, `xmlns:*`
  (the latter are handled by R-COMP-14).
- Risk note: with R-COMP-3 a plain document containing foreign prefixed
  elements (e.g. `<o:p>` pasted from Word) fails with 500. This follows the
  docs; probe Q-1 settles it.

**R-COMP-14 Stripping in the served output.**
1. **HTML**: every `xmlns:<prefix>` attribute is removed from the served
   composed document (not only Pagelove ones). The bare `xmlns` attribute is
   kept.
2. **XML**: all `xmlns:` declarations are preserved, including `xmlns:e`/`xmlns:p`.
3. **Both**: every directive attribute that was recognised (bindings,
   `*:template`, `*:paginate`, `*:transient`, dispatched or stripped method
   attributes) is removed; directive elements are replaced by their results.
   Unbound-prefix attributes stay (R-COMP-13).
4. Stripping is a **response** transformation: stored documents keep their
   declarations; a document stored by templated creation keeps `xmlns:p`
   (R-COMP-143).
- Evidence: documented ([INC] §Basic include: "`xmlns:p` — like every
  `xmlns:*` declaration in the composed document — has been stripped"; [TPL]
  "All SSPI namespaces and binding attributes have been stripped"; [TR]
  example shows `<ul id="cart">` without `p:transient`; [XML] §Differences),
  live-observed ([OBS] blog and ATS pages: `<html lang="en">`, `<body>` with
  no `e:post`), demo-source ([POLL] stored poll keeps `xmlns:p`). Confidence:
  high (Pagelove namespaces, directive attributes), medium (non-Pagelove
  `xmlns:*` in HTML: [XML] says "Pagelove `xmlns:` declarations are removed",
  [INC] says "every"; C-4, decision: every). Cases: `comp.ns.strip-xmlns-and-directives`,
  `comp.ns.strip-foreign-xmlns`, `comp.xml.xmlns-preserved`.
- Contradiction C-5: [QRY] §On the edge proxy shows a composed-page answer
  containing `pagelove:template="text/liquid"` and raw `{{ 2 | plus: 3 }}`,
  but it is labelled `HTTP/1.1 201` and is the setup PUT's echo, not a
  composed read; the next example there shows `<main class="calc">5</main>`
  (attribute stripped, host kept, content rendered). Decision: strip.

---

## 4. Context, scope and the `request` object

**R-COMP-20 Context.** Composition keeps a Context: a stack of scopes that
follows element ancestry. Each binding or method mutation writes a name into
the scope of the element that declared it. A name is visible to that element
and all its descendants (including content spliced into that subtree later),
never to siblings or ancestors. When a name exists at several levels the
nearest ancestor wins.
- Evidence: documented ([EB] §Scope; [ME] §JavaScript implementations and
  §Attribute form "visible only to the host element and its descendants").
  Confidence: high. Cases: `comp.eb.nearest-ancestor-wins`, `comp.eb.sibling-scope`.

**R-COMP-21 Context mutations by element-form methods.** A Context mutation
made by an element-form method is visible only to the dispatched element's
subtree, i.e. to the content that replaces it (pagelike decision).
- Contradiction C-6: [ST] §How a value reaches Context says "The composition
  pipeline propagates a method's `Context` mutations to later dispatches on the
  same page" and suggests stamping "an instance written into Context by an
  upstream method"; [ME] (twice) says sibling and ancestor elements never see
  it. Decision: subtree (the more specific and repeated rule). Confidence:
  low. Probe Q-5.

**R-COMP-22 Root scope.** The root scope holds `request` (R-COMP-24) and
nothing else. `Context.request` is the same object. Liquid, Sessel and
JavaScript see the Context names as top-level variables.
- Evidence: documented ([ME] "Request data is read through
  `Context.request`"; [JB] §What the expression can see; [TPL] §Data sources).
  Confidence: high.

**R-COMP-23 Rule-only names are not in scope.** The authorization-rule names
`auth.claims.*`, `method`, `path`, `query.*` are not bound in composition; a
bare reference (e.g. `e:x="auth.claims.email"`) is an undefined variable and
fails composition (R-COMP-161).
- Evidence: documented ([SES] §Authenticated identity in composition).
  Confidence: high. Case: `comp.eb.bare-auth-name-undefined`.

**R-COMP-24 The `request` object.** Members available to Sessel, Liquid and
JavaScript during composition:

| Member | Value | Cache effect (R-COMP-150) |
|---|---|---|
| `request.path` | decoded request path, no query (the concrete URL for routes) | shared |
| `request.method` | `GET`, `POST`, … | shared |
| `request.query.<name>` | query parameter value | shared |
| `request.params.<name>` | route captures (R-COMP-104); empty object otherwise | shared |
| `request.headers.<lower-case name>` | header value | **private** |
| `request.auth.claims.<claim>` | OIDC claims (`email`, `name`, `sub`, `picture`, …) | **private** |
| `request.auth.username` | the principal's `sub` | **private** |
| `request.auth.roles` / `request.auth.role` | list of roles/groups (both spellings, compat) | **private** |
| `request.body` | for `application/x-www-form-urlencoded` (and `multipart/form-data` text fields, inferred): object of field → string; otherwise the raw body string | shared (only non-GET requests) |

For an anonymous request every `request.auth.*` member is empty/falsy and
reading it is **not** an error.
- Evidence: documented ([SES] context-variable table and §Authenticated
  identity; [JB] §Caching; [PR] §Reading captured parameters; [RD] §Fields),
  demo-source ([SHOP] `partials.html:20` `request.headers.authorization`;
  [POLL] `new-poll.html:7-21` `request.body.<field>`; [KAN] `app.js:653`
  `request.auth.role`). Confidence: high (path/method/query/auth/params),
  medium (headers lower-case, body parsing), low (`role` vs `roles`, C-7).
- Edge: repeated query or form fields — unknown (pagelike: last value wins,
  low; probe Q-6). Missing members read as nil/null, never an error.

---

## 5. Evaluation order

**R-COMP-25 Document walk.** Composition walks the requested document in
document (pre-order) order. At each element:
1. **Element-form directive?** (`<prefix:name>` with a bound prefix, or an
   unbound prefix → R-COMP-13): dispatch it (include, stamp, method); the
   element is replaced by the result and the walk continues **into the
   result** (recursion), then with the next sibling.
2. **Attribute-form directives** on an ordinary element, in this order:
   a. all resource bindings (`Binding/CSS`) in source order;
   b. all other non-template prefixed attributes — Sessel and JavaScript
      bindings and schema method attributes — one after another in source
      order (`e:` and `j:` interleave freely);
   c. every `*:template` attribute last, regardless of where it is written.
   `*:transient` is never dispatched (R-COMP-110); `*:paginate` is recorded
   for the pagination pass (R-COMP-130).
   If a dispatched method whose declared `returns` is
   `https://pagelove.org/Element` yields a non-null result, the host element
   is replaced and the remaining attributes (including `*:template`) are not
   dispatched (R-COMP-63).
3. Walk the element's children (the rendered content if a template ran).
- After the walk: **pagination pass** (R-COMP-130) over the fully resolved
  DOM, then `@read` resolvers (area `modeling` R-MOD-50), then stripping
  (R-COMP-14) and serialization.
- Evidence: documented ([EB] §Declaration-order evaluation, §The three
  bindings compared "Resource bindings resolve first, then expression bindings
  evaluate in declaration order"; [ME] §Attribute form "Any `*:template`
  attribute is always dispatched last"; [ST] "A `<p:stamp>` must appear after
  whatever wrote the value it names, in composition (tree-walk) order"; [PAG]
  "runs after includes, expression bindings, and templates"; [ME] "Composition
  recurses into the spliced fragment"). Confidence: high (a/b/c, pagination
  last), medium (exact interleaving of custom method attributes with `e:`).
- Contradiction C-8: [ME] says multiple prefixed attributes "are **not**
  dispatched in the order they're written" (because of the template/resource
  rules) while [EB] says "Attributes evaluate left to right in source order".
  Both hold under the ordering above.
- Case: `comp.eb.template-attr-order`, `comp.me.template-dispatched-last`.

**R-COMP-26 Transient substitution first.** Before the walk, each transient
element of the requested document is replaced by the requesting session's
copy if one exists (R-COMP-111). Session content is **not** composed
(R-COMP-116).

**R-COMP-27 Budget.** Each element-form or attribute-form dispatch (includes,
stamps, method elements, binding attributes) costs one unit; recursive results
cost more. At most **500 dispatches per request**; exhausting the budget fails
the request with **503** and message `composition budget exceeded`. Template
rendering is also charged per instruction, loop iteration, output byte and
stored value; exhausting any axis fails the whole request (no partial page).
Include/stamp cycles terminate through this budget (pagelike decision).
- Evidence: documented ([ME] §Error cases; [TPL] §Limits; [AZR] "the
  `503 Service Unavailable` a budget failure produces elsewhere"). Confidence:
  high (500/503 for dispatch), medium (template exhaustion status = 503).
- Live responses also carry `X-Budget-Consumed-Ops`, `-Memory`, `-Time`
  headers ([OBS]); pagelike MAY emit them; harness normalizes `x-budget-*`.

---

## 6. Resource binding (`r:`)

**R-COMP-30 Form.** `r:<name>="<css-selector>"` on any element (prefix bound
to `https://pagelove.org/Binding/CSS`). The attribute's local name is the
variable name; the value is a CSS selector (with PageLove extensions, §20).
Several bindings may sit on one element.
- Evidence: documented ([RB] §Attribute form, §Binding placement). Confidence: high. Case:
  `comp.rb.multiple-bindings-one-element`.

**R-COMP-31 Result.** The selector is evaluated against **every document of
the site graph** (stored/canonical markup, including route-template documents
and the page itself, plus the Request Document), and the name is bound to the
list of **all** matching elements, in stable order: documents in ascending
path order, elements in document order within each (pagelike decision for the
cross-document order). No match → empty list (not an error). Elements inside
`<template>` contents are not matched (inert boundary). Transient session
content is not visible.
- Evidence: documented ([RB] §Semantics, §Error cases; [TR] §Reading;
  [SEE] "the inert boundary that prevents selectors from descending into
  `<template>`"), demo-source ([CURSOR] "Resource binding picks up template
  files … with raw Liquid tags as property values"; [POLL] writes the template's
  itemtype as `{{ 'https://pagelove.org/Poll' }}` so the template is not itself
  a Poll). Confidence: high (site-wide, empty), low (order, which apps
  re-sort with `sort`).
- Cases: `comp.rb.contacts-template`, `comp.rb.empty-collection`,
  `comp.rb.template-inert-boundary`, `comp.rb.template-documents-visible`.

**R-COMP-32 No authorization filtering (security).** Bindings read the
**entire host** with the server's authority. A binding returns elements from
documents the current requester could not GET directly; authorization rules
never restrict what a binding (or an include, stamp, `${…}` Sessel query)
reads. Consequence: whoever can author a composed page — including anyone
allowed to write markup into a document where Pagelove prefixes are in scope —
can read the whole host.
- Evidence: documented ([RB] §Security, §Error cases; [BLOG] §Locking the
  data folder), demo-source ([SHOP] orders and settings under denied folders
  rendered by bindings/stamps; [ATS] `roles.html` includes from a private
  admin page). Confidence: high. Case: `comp.rb.bypasses-read-authorization`.
- pagelike note: a public write rule that lets untrusted users insert
  arbitrary markup (e.g. a comment `<li>` with `r:x="…" p:template=…`) into a
  page whose `<html>` declares `xmlns:r`/`xmlns:p` lets them exfiltrate host
  data on the next composed read. PageLove's answer is ShapeConstraints
  ([BLOG] §Making comments safe). pagelike keeps PageLove semantics and
  SHOULD log a warning when a public write adds directive attributes.

**R-COMP-33 Errors.** An invalid selector fails the request during document
processing. pagelike: **500**, error document kind `CompositionError`
(pagelike-chosen name; the status is not documented).
- Evidence: documented (failure), inferred (status). Confidence: low (status).
  Case: `comp.rb.invalid-selector-fails` (accepts 400/422/500).

**R-COMP-34 Other consumers.** `r:` bindings on or above Constraint and
Trigger items supply their variables (`size(admins) >= 1`,
`'Bearer ' + apiKey`); those evaluations belong to areas `modeling` and
`reactions`, which reuse this area's binding evaluator. A binding of the
Request Document (`[itemtype*=Request] …`) makes the page private
(R-COMP-150).
- Evidence: documented ([RB] §Graph constraint, §Trigger context, note).

---

## 7. Sessel expression binding (`e:`)

**R-COMP-35 Form.** `e:<name>="<sessel expression>"` (prefix bound exactly to
`https://pagelove.org/Binding/Sessel`). The value is evaluated at serve time
and bound under `<name>`. Only the attribute form exists.
- Evidence: documented ([EB] §Attribute form). Confidence: high.

**R-COMP-36 What the expression sees.** Bare names resolve to: `let` locals;
earlier bindings on the same element (declaration order) and bindings of
ancestors (R-COMP-20); `request` (R-COMP-24); `self` = the declaring element
(its document for `from self`); `document` = root element of the requested
document. `${…}` selector literals query the **whole site graph** unless
narrowed by `from` (`from self` = the requested document as stored, `from
"/glob"`). Inside `${…}` an unquoted pseudo-class argument or attribute value
is a Sessel expression (`:value-equals(request.params.slug)`).
- Evidence: documented ([EB] §Declaration-order evaluation, §Scope; [SES]
  context variables; [SEQ] §Expressions inside selectors, §The `from`
  clause), demo-source ([SHOP] `products/:slug.html:13`; [BLOG] `posts/:slug.html`).
  Confidence: high (names, site-wide), medium (`self` = element vs document root:
  [SEQ] says `self` is the document root; [SES] lists `self` for expression
  bindings).
- Doc defect: [EB] §Scope example `${div.item} from self).count()` has an
  unbalanced parenthesis; read as `(${div.item} from self).count()`.

**R-COMP-37 Values.** Any Sessel value may be bound: number, string, boolean,
null, list, dictionary, element (queried or constructed), instance. Queried
elements keep their origin identity (for `<p:stamp>` write-through,
R-COMP-90). Liquid sees them per R-COMP-52.
- Evidence: documented ([EB] "returns any value"; [ST]). Example: two items
  with prices summing to 100 render `Count: 2` and `Sum: 100.0` ([EB]
  §Examples — Sessel float formatting, area `sessel`). Confidence: high.

**R-COMP-38 Errors.** Compile failure → composition fails; undefined variable
→ composition fails; namespace URI not exact → attributes ignored and no
binding created (and stripped per R-COMP-13). pagelike status: **500**,
kind `CompositionError`, message naming the attribute and the Sessel error.
- Evidence: documented ([EB] §Error cases), inferred (status; area `sessel`
  owns error classes). Confidence: high (failure), low (status). Cases:
  `comp.eb.undefined-variable-fails`, `comp.eb.compile-error-fails`,
  `comp.ns.wrong-sessel-uri-ignored`.

---

## 8. JavaScript expression binding (`j:`)

**R-COMP-40 Form.** `j:<name>="<one JavaScript expression>"` (prefix bound
exactly to `https://pagelove.org/Binding/JavaScript`); only the attribute form
exists. The expression is evaluated as the body of one function in the server
JS runtime; it MAY use `await` (a returned promise is settled before binding).
- Evidence: documented ([JB]). Confidence: high. Cases: `comp.jb.reduce-example`
  (`[10, 20, 30].reduce(…)` → `Total: 60`, `Doubled: 120`), `comp.jb.await`.

**R-COMP-41 Scope.** In scope: earlier `j:`/`e:`/resolved `r:` bindings of the
same element as bare identifiers; ancestor bindings (inferred, consistent with
R-COMP-20); `request`; `Context` (object holding all visible names). A name
that is not a plain identifier or is a reserved word (`class`, `new`,
`first name`) is still bound but readable only as `Context["class"]`.
- Evidence: documented ([JB] §What the expression can see). Confidence: high
  (same element), medium (ancestors). Case: `comp.jb.keyword-name-context`,
  `comp.jb.interleave-with-sessel`.

**R-COMP-42 Errors.** Parse error, throw, or rejected promise → the request
fails with an HTML-Microdata error document (pagelike **500**, kind
`CompositionError`). Wrong namespace URI → ignored.
- Evidence: documented ([JB] §Error cases). Confidence: high (failure),
  medium (500: [ME] uses 500 for implementation errors). Case: `comp.jb.throw-fails`.

**R-COMP-43 Caching taint.** Reading an identity member of `request`
(`request.auth`, `request.headers`, …) marks the response private; reading
`request.path`, `request.query`, `request.method` does not; naming `request`
without reading an identity member does not taint.
- Evidence: documented ([JB] §Caching). Confidence: high. Cases:
  `comp.jb.request-auth-private`.

---

## 9. Templates (`p:template`) as a pipeline stage

The Liquid language, filters and limits are area `liquid`; this section fixes
how templating participates in composition.

**R-COMP-50 Form.** `p:template="text/liquid"` (any prefix bound to
`https://pagelove.org/1.0`) on any element, including `<html>`, `<body>` and
the root of an XML document. `text/liquid` is the only engine. Unknown MIME
value: pagelike leaves the content unrendered and strips the attribute
(inferred, low).
- Evidence: documented ([TPL] §Enabling templating), demo-source ([POLL] on
  `<html>`; [BLOG] feed on `<feed>`). Confidence: high.
- Templates are side-effect free: they cannot mutate documents, create
  resources, perform I/O or issue HTTP requests ([TPL] §Template scope).
  Their data sources are the Context names (bindings), `request`, and
  template-local variables (`assign`, `capture`).

**R-COMP-51 Output placement.** The host element is **kept** (tag and other
attributes); its **content** is replaced by the rendered output, which is
parsed as markup (HTML fragment in the host's context, or XML for XML
documents) and then composed (the walk continues into it: `<p:include>`,
`<p:stamp>`, bindings inside the output work). The `*:template` attribute is
removed.
- Evidence: demo-source ([KAN] `app.js:139` reads `#whoami-server` text after
  the server rendered it, so the host with its id survives; [SHOP] CSS on
  `.grid` host), live-observed ([OBS] blog `<section class="excerpts">`
  survives), documented ([QRY] `<main class="calc">5</main>`; [RD] example).
  [TPL]'s "Rendered HTML replaces the original template subtree" is read as
  "replaces the content". Confidence: high (host kept), medium (output
  re-composed).

**R-COMP-52 How values look to Liquid.**
- Names in Context become top-level variables; `request` as R-COMP-24.
- A list binding (every `r:` binding) is a Liquid array: `size`, `first`,
  `last`, `for`, and array filters (`where`, `sort`, `reverse`, `map`,
  `join`, …) work on it.
- Each bound **element with `itemscope`** is materialized as an object whose
  keys are its microdata property names and whose values are microdata values
  (same value rules as §22 R-COMP-171: `content` for `meta`, raw `href`/`src`,
  `datetime` for `time`, text otherwise). A property present once is a plain
  string; present several times it is a list (current PageLove; earlier
  releases returned only the first); a nested `itemscope` property is a nested
  object. Typed instances (schema-governed) follow the cardinality rule of
  area `modeling` R-MOD-20 and have `@read` resolvers applied.
- Extra keys: `@id` = `<document path>#<element id>` (e.g.
  `/products/widget.html#widget`); `@type` = the itemtype.
- `{{ x }}` of a number renders Sessel's formatting (`100.0` for a float sum).
- Output is **not** auto-escaped (authors use `| escape`).
- Evidence: documented ([PROP] §Reading a multi-valued property, "on that route
  you do get every value when there is more than one"; [TPL] `user['@id']`;
  [SEE] `.microdata()` `@id` example; [BLOG] `where`/`sort`/`.size`),
  demo-source ([SHOP] `admin/products.html:20` `p.variant | join: ','`,
  `index.html:17-24` single-vs-list comment; [POLL] `index.html:90` `poll['@id']
  | split: "#" | first`; [POLL] escapes every field). Confidence: high (keys,
  lists), medium (`@id` form, no auto-escape), low (`@type` value, elements
  without `itemscope` — pagelike: only `@id`).
- Open: what `{{ element }}` prints; `@id` for an element without `id`
  (Q-7).

**R-COMP-53 Errors.** An error in one output expression (`{{ … }}`, e.g. a
filter failing on a value) renders an inline `https://pagelove.org/Error`
microdata element in place (empty string inside an attribute value); the page
still composes (200). An error in a control-flow tag's expression, or budget
exhaustion, fails composition.
- Evidence: documented ([LQF] §When a filter errors; [TPL] §Limits).
  Confidence: high. Case: `comp.tpl.filter-error-inline`.

**R-COMP-54 Templates in created documents.** In templated creation the
template runs with `request.method = POST` and `request.body` (R-COMP-140).

---

## 10. Method elements

Declarations (Schema, Method, Parameter items, registration timing) are area
`modeling` R-MOD-58..60 and R-MOD-4. This section is invocation.

**R-COMP-60 Element form.** `<t:foo attr="v" …>` where `t` is bound to a URI
equal to a registered Schema `type` and `foo` names a Method of that schema
(or its ancestors). The element is replaced by the method's result.
- Evidence: documented ([ME] §Element form). Confidence: high. Case:
  `comp.me.sessel-element` (`<t:hello>` → `hello from urn:Demo`),
  `comp.me.javascript-element` (→ `hello from JavaScript`).

**R-COMP-61 Arguments (element form).** Each attribute whose name equals a
declared parameter name supplies that parameter; other attributes (except
`xmlns:*`) are ignored. Sessel receives parameters as named locals, JS
positionally in declaration order (absent → `null`). `self`/`this` = the
dispatched element (JS: as a microdata object). Overloads: the overload whose
declared parameters all appear as attributes and that is most specific wins.
- Evidence: documented ([ME] §Passing arguments, §JavaScript implementations).
  Confidence: high. Case: `comp.me.element-args-by-name`.

**R-COMP-62 Result → HTML.**

| Result | Effect |
|---|---|
| Element (constructed, queried, JS node) | serialized, parsed as a fragment, spliced in place; composition recurses into it |
| Instance of a schema | serialized as its microdata, spliced; recurses (with `@key` → root `id`, R-COMP-76) |
| List / `NodeList` / array of elements or instances | each spliced, in order |
| Scalar (string, number, boolean) | stringified (`value_to_html`) and inserted as **text** (escaped) |
| Null / `undefined` / empty implementation | element removed |

- Evidence: documented ([ME] §How the result becomes HTML; §Error cases "no
  `implementation`"). Confidence: high. Case: `comp.me.null-result-removes`.

**R-COMP-63 Attribute form.** `<div t:foo="v">` dispatches method `foo` with
the attribute value bound to the **first declared parameter**. If the method's
declared `returns` is `https://pagelove.org/Element` and the result is non-null,
the **whole host element** is replaced by the result (R-COMP-62) and dispatch
of the host's remaining prefixed attributes stops (even `*:template`).
Otherwise the dispatching attribute is removed, the host stays, and any
Context mutation is visible to the host's subtree only.
- Evidence: documented ([ME] §Attribute form). Confidence: high. Case:
  `comp.me.attribute-form-dnu` (`<div t:greet="Ada">` → `<p>greet: Ada</p>`).

**R-COMP-64 `doesNotUnderstand`.** A method named `doesNotUnderstand` catches
every unmatched name under the schema's prefix, in both forms. It receives
`messageName` (the local name) and `parameters`: element form → list of
`{name, value}` for every attribute not starting with `xmlns:` (a bare `xmlns`
is included); attribute form → a one-element list holding the attribute's
string value. Sessel receives them as named locals; JS positionally
(`messageName`, `parameters`). The built-in binding namespaces are exactly
this: `r:items="li"` → `messageName = "items"`, `parameters = ["li"]`, the
method executes the selector and writes `Context[messageName]`.
- Evidence: documented ([ME] §`doesNotUnderstand` fallback, §Attribute-form
  example). Confidence: high. Case: `comp.me.template-dispatched-last`.

**R-COMP-65 Reserved `transient`.** A `prefix:transient` attribute is never
dispatched, even under a prefix whose schema has `doesNotUnderstand`; it is
the transient marker (R-COMP-110) for any Pagelove-recognised prefix.
- Evidence: documented ([ME] §Attribute form last bullet; [TR] "within a
  Pagelove namespace"; [SLC] constructs `p|transient` with `p` bound to
  `Binding/CSS`). Confidence: high. Case: `comp.me.transient-attr-not-dispatched`.

**R-COMP-66 JavaScript method runtime.** `this` = dispatched element as a
microdata object; a read-only `document` global scoped to that element's
subtree (`querySelector` works; mutation throws `NoModificationAllowedError`);
fragments are built with `new DOMParser().parseFromString(…)`; the default
export may be `async`.
- Evidence: documented ([ME] §JavaScript implementations). Area `jsrt`.

**R-COMP-67 Errors.** Unbound prefix / unregistered schema / undeclared name
(element form) → **500** `no method found for <prefix>:<name> and no
doesNotUnderstand defined`; attribute form → no error (R-COMP-13).
Implementation raises (Sessel error, thrown JS error) → **500** with the error
message. Budget → **503** `composition budget exceeded`. A schema written over
WebDAV may not be registered for up to 60 s; "PUT the document containing the
schema once to register it" (area `modeling` R-MOD-4).
- Evidence: documented ([ME] §Error cases, §Defining the method). Confidence:
  high. Cases: `comp.ns.unbound-element-500`, `comp.me.element-form-undeclared-500`,
  `comp.me.attr-form-undeclared-stripped`, `comp.me.implementation-error-500`.

---

## 11. Stamp (`<p:stamp>`)

**R-COMP-70 Form.** `<p:stamp name1 name2 …></p:stamp>` with `p` bound to
`https://pagelove.org/1.0`. The attribute **names** are Context keys; values
are ignored. `stamp` is a built-in method (declared `returns
https://pagelove.org/Element`); no schema declaration is needed.
- Evidence: documented ([ST] §Form). Confidence: high. Case:
  `comp.stamp.expression-value` (docs example → `hello from a binding`).

**R-COMP-71 Result.** Collect each named key's non-null Context value, in
attribute order: none → the element is removed; one → that value; several →
a list. The result is applied with R-COMP-62 (elements spliced and composed,
scalars inserted as text).
- Evidence: documented ([ST] §How the result is emitted). Confidence: high.
  Cases: `comp.stamp.absent-removed`, `comp.stamp.multiple-names-order`.

**R-COMP-72 Order.** A stamp sees only values written earlier in the walk and
in scope at its position (R-COMP-20, R-COMP-25).
- Evidence: documented ([ST] "must appear *after* whatever wrote the value").

**R-COMP-73 Stamped elements are projected.** A stamped element (a queried
element from another document, e.g. `${…}.first()`) is a copy of the origin
element with **all its attributes** (including `id`) and keeps its origin
identity (document path + element) for write-through (R-COMP-90). A
constructed element or scalar has no origin.
- Evidence: documented ([ST] intro, §Interaction), live-observed ([OBS] blog
  post page: stamped `<article … id="post-the-shape-of-pagelove">` with the
  data file's attributes). Confidence: high. Case: `comp.stamp.element-origin-kept`.

**R-COMP-74 Private origins.** Stamping works for records in documents the
requester may not GET (R-COMP-32).
- Evidence: demo-source ([SHOP] `orders/:id.html:10-13`, `checkout.html:11-13`,
  rules deny GET on `/data/orders/*` and `/data/settings/*`), documented
  ([BLOG] §Locking the data folder). Confidence: high. Case:
  `comp.stamp.from-denied-source`.

**R-COMP-75 Errors.** `p` unbound or bound to another URI → not a stamp →
element-form dispatch fails (R-COMP-67, 500). `<p:unknown>` on the Pagelove
namespace → **500** `unknown method '<name>' on Pagelove namespace`.
- Evidence: documented ([ST] §Error cases). Confidence: high. Cases:
  `comp.stamp.wrong-p-namespace-500`, `comp.stamp.unknown-pagelove-method-500`.

**R-COMP-76 Keyed instances.** When a stamped value is a schema instance whose
schema has a `@key` property, the key value is emitted as the spliced root's
`id`, so `#<key>` addresses it in the page.
- Evidence: documented ([ST] §Routing a stamped instance). Cross-area
  `modeling` R-MOD-33..34. Confidence: medium.

---

## 12. Includes (`<p:include>`)

**R-COMP-80 Form.** `<p:include selector="S" resource="R"></p:include>` (or
self-closing, R-COMP-6), prefix bound to `https://pagelove.org/1.0`.
`selector` is required; `resource` is optional.
- Evidence: documented ([INC] §Element form). Confidence: high.

**R-COMP-81 Resolution.** Candidates = documents whose path matches the
`resource` glob (same glob language as AuthorizationRule `resource`, area
`permissions` R-PERM-20: `*`/`**` span segments, `?` one character) or, when
`resource` is absent, the whole site graph (including the Request Document).
Evaluate `S` in each candidate's **stored** markup; collect all matches.
- Evidence: documented ([INC] §Resolution model, §`resource`). Confidence:
  high (model), medium (glob semantics shared with rules; [INC] names `*`,
  `**`, `?` only). Case: `comp.inc.resource-glob-constrains`,
  `comp.inc.global-selector`.
- `resource` with several values: [INC] says "the value(s)"; pagelike splits
  the attribute on ASCII whitespace and ORs the globs (inferred, low).

**R-COMP-82 Cardinality.** Exactly one match → the matched element (with its
subtree) replaces the `<p:include>` element (and anything between its tags).
**0 matches → the whole request fails `404 Not Found`. >1 matches → `500
Internal Server Error`** (a site-integrity failure, not a client error).
- Evidence: documented ([INC] §Cardinality rules). Confidence: high. Cases:
  `comp.inc.zero-matches-404`, `comp.inc.multiple-matches-500`.
- Error document: generic pagelike error item (reading-writing R-RW-130),
  kinds `IncludeNotFound` / `IncludeAmbiguous` (pagelike names; cases assert
  status only).

**R-COMP-83 `selector` required.** `<p:include resource="/x.html"/>` (no
`selector`) is invalid. pagelike: composition fails **500** (kind
`CompositionError`).
- Evidence: documented (invalid), inferred (status). Confidence: low.

**R-COMP-84 Included content is composed in place.** The spliced element is a
copy of the origin's **stored** element. The walk then continues into it:
directives inside it run in the including page's Context, with prefix
resolution using the origin element's in-scope `xmlns:*` declarations first,
then the including page's.
- Evidence: demo-source ([SHOP] `partials.html:20` — the included `#chrome`
  contains a `p:template` that reads `request.headers.authorization`, and the
  comment says admin links "are decided HERE, during composition"). Whether
  PageLove composes the fragment in the origin's context before selecting it
  is unknown. Confidence: medium (directives run), low (scope overlay).
  Probe Q-8. Case: `comp.inc.nested-template-in-fragment`.

**R-COMP-85 Private sources.** Includes read documents the requester cannot
GET (R-COMP-32).
- Evidence: demo-source ([ATS] `roles.html:8-15` "exposes ONLY the roles by
  including #roles-body from admin/index.html via elevated server-side
  composition"). Confidence: high. Case: `comp.inc.private-source`.

**R-COMP-86 Selector reads of included content.** Selector reads on the
including page address included elements like native ones
(`GET /roles.html` + `Range: selector=#roles-body` returns the included
`<tbody>`).
- Evidence: demo-source ([ATS] `index.html:413-416`). Confidence: high. Case:
  `comp.inc.range-read-included-element`.

**R-COMP-87 SSE.** A change to the origin notifies subscribers of the origin's
path only, never subscribers of including pages (area `sse` R-SSE-20).

---

## 13. Write-through to origin

**R-COMP-90 Provenance.** Every node of the composed view records its
provenance: *native* (a node of the requested stored document), *projected*
(a copy of origin document D's element E, via include or stamp — descendants
of a projected root are projected from E's descendants), or *generated*
(template output, method results built by construction, pagination links,
transient session content).
- Evidence: inferred (needed to implement [INC]/[ST]/[PR] write-through).

**R-COMP-91 Routing.** A selector write (PUT, POST, DELETE with `Range:
selector=`) to page X resolves its selector against X's composed view for the
requester (first match, reading-writing R-RW-15). Then:

| Target provenance | Write applied to |
|---|---|
| native | X's stored document (ordinary write) |
| projected from D/E | the corresponding element of **D** (the origin) — X's stored document is untouched |
| generated | nothing; **416** Range Not Satisfiable (pagelike decision) |
| inside a transient element | the session (R-COMP-112) |

A DELETE addressed at a stamped record deletes the origin element (the record),
not the page and not the whole origin document. A POST append to a projected
anchor inserts into the origin element.
- Evidence: documented ([INC] §Interaction with HTTP Document Mutation; [ST]
  §Interaction; [PR] §Resolution; [BLOG] §Letting readers comment: a comment
  POSTed to `/posts/<slug>.html` "lands in `data/posts/<slug>.html`"; [POST]
  §Concurrency note). Confidence: high (projected), low (generated → 416, by
  analogy with the documented route rule R-COMP-106). Cases:
  `comp.stamp.write-through-put`, `-post`, `-delete`, `comp.inc.write-through-put`.

**R-COMP-92 Authorization of routed writes.** Authorized against **X** (the
path the request addresses) and the target element as it appears in X's
composed view (area `permissions` R-PERM-28). The origin's own rules are not
consulted: a rule on X is both necessary and sufficient.
- Evidence: documented ([INC], [ST], [PR]). Confidence: high. Case:
  `comp.stamp.write-auth-on-page-not-origin`.

**R-COMP-93 Validation and concurrency.** ShapeConstraints are evaluated
against the origin document where the change lands (a shape applies if a glob
matches either X or D, area `modeling` R-MOD-62); schema validation, triggers
and ETag/`If-Match` checks use the origin. A POST append to a projected anchor
retries against the origin's latest version on conflict (appends
accumulate). SSE events go to subscribers of D (area `sse` R-SSE-19).
- Evidence: documented ([INC], [ST], [POST] §Concurrency). Confidence: high.
  Case: `comp.stamp.write-through-if-match-origin`.

**R-COMP-94 Failed composition blocks writes.** If an include of X fails to
resolve uniquely (or any composition error occurs), no write is attempted and
the composition error is returned (404/500).
- Evidence: documented ([INC] "If an include fails to resolve uniquely, no
  write is attempted"). Confidence: high (includes), medium (other errors).
  Case: `comp.inc.failed-include-blocks-write`.

**R-COMP-95 Response.** The routed write returns the same status and body shape
as the corresponding ordinary selector write (reading-writing R-RW-64/72/80:
206, 206, 204); its `ETag` describes the origin's new state (inferred).

---

## 14. Transient elements (`p:transient`)

**R-COMP-110 Marker.** An attribute `transient` under any prefix bound to a
Pagelove namespace (`p:transient`) marks an element as session-scoped. The
value is ignored (reserved for a future per-element TTL in seconds).
- Evidence: documented ([TR] §Attribute form, §Future extensibility; [ME];
  [SLC]). Confidence: high.

**R-COMP-111 Reading.** For the requested document, each transient element is
served as the requesting session's stored copy if one exists, otherwise as the
document default (inert). The marker attribute is stripped from the output.
- Evidence: documented ([TR] §Reading, example). Confidence: high. Case:
  `comp.tr.default-fresh-session`.

**R-COMP-112 Writes go to the session.** A `PUT`, `POST` or `DELETE` whose
selector target is a transient element **or a descendant of one** writes the
session copy, never the stored document:
- The session copy is the whole transient element (outer markup), initialised
  from the current session copy or the document default, with the write
  applied (child mutations are mutations of the transient ancestor).
- **PUT identity rule:** after the replacement, the request selector must still
  match the replaced element; otherwise **422** and nothing changes. The tag
  may change (`<div id="cart">` may replace `<ul id="cart">`); the id may not.
- The transient marker is preserved automatically (not required in the body).
- **DELETE of the transient element** removes the session copy (next read
  serves the default). DELETE of a descendant removes it from the session copy
  (inferred).
- **POST** appends into the session copy (documented; [SHOP] avoids it as
  "not documented", C-9).
- Other methods (e.g. MOVE) targeting a transient element → **405**.
- A mutation with no session established → **409** (normally unreachable: a
  session cookie is minted on first contact, area `identity`).
- Session copies expire **30 days** after their last write, independent of the
  session lifetime.
- Evidence: documented ([TR] §Mutating, §Child mutations, §Default behaviour,
  §Error responses), demo-source ([SHOP] `shop.mjs:56-97`: read-modify-write
  PUT of `#basket`, DELETE to empty). Confidence: high (PUT, DELETE, 422),
  medium (POST, child DELETE). Cases: `comp.tr.put-session-only`,
  `comp.tr.identity-change-rejected-422`, `comp.tr.identity-preserved-tag-change-ok`,
  `comp.tr.delete-reverts`, `comp.tr.child-mutation`, `comp.tr.post-into-transient`.

**R-COMP-113 Isolation.** One session's copy is never visible to other
sessions, to bindings/includes/`${…}` (they read the canonical document), to
SSE subscribers (area `sse` R-SSE-21), or to the WebDAV plane.
- Evidence: documented ([TR] intro, §Reading), demo-source ([SHOP] README
  "Transient content is not available to server-side resource bindings").
  Confidence: high. Cases: `comp.tr.cross-session-isolation` (multi-actor),
  `comp.tr.binding-sees-canonical`.

**R-COMP-114 Caching.** Every response built from a document carrying a
transient marker is `Cache-Control: private` — the whole document **and** any
selector read of it (whatever element the selector matches), with or without a
session.
- Evidence: documented ([TR] §Reading). Confidence: high. Case:
  `comp.tr.private-cache-fragment`.

**R-COMP-115 Authorization.** Ordinary rules govern transient reads and
writes; anonymous visitors have sessions, so they can write transient content
if a rule allows. The key element for a rule selector is the element the
request selector matched (area `permissions` R-PERM-28).
- Evidence: documented ([TR] §Sessions and access control), demo-source
  ([SHOP] `rules.html:19-24` public PUT/DELETE on `#basket`). Confidence: high.
- Cookie name: [TR] says `pagelove_session`; live responses set
  `__Host-session=<uuid>; Path=/; Secure; HttpOnly; SameSite=Lax;
  Max-Age=31536000` ([OBS] blog headers). Area `identity` decides (C-10).

**R-COMP-116 Session content is inert (compat/security decision).** Session
copies are client-written; pagelike MUST NOT run directives found in them
(no bindings, templates, includes), otherwise any anonymous visitor could
read the whole host (R-COMP-32).
- Evidence: inferred. Confidence: low. Probe Q-9.

**R-COMP-117 Whole-document writes.** A whole-document PUT of a document with
transient elements replaces the stored defaults; existing session copies are
kept and keep overriding (inferred).

---

## 15. Parameterized routes

**R-COMP-100 Route templates.** A stored document whose path has one or more
segments beginning with `:` is a route template. The colon is literal in the
stored path. A directory segment `:name` captures one whole request segment;
a filename segment `:name<suffix>` (e.g. `:slug.html`) matches a request
filename that ends with `<suffix>` and captures the part before it; a bare
filename `:name` captures the whole filename. Parameter names are the
characters after `:` up to the first `.` (inferred).
- Evidence: documented ([PR] §Authoring a route). Confidence: high. Cases:
  `comp.route.basic-id`, `comp.route.filename-param`, `comp.route.multi-params`.

**R-COMP-101 When resolution runs.** Only for a **whole-document GET** (and
HEAD, inferred) whose path has **no literal document**, and for selector
writes (R-COMP-106). A literal document at the exact path always wins.
- Evidence: documented ([PR] §Resolution). Confidence: high. Case:
  `comp.route.literal-document-wins`.

**R-COMP-102 Matching and precedence.** A template matches when it has the
same number of segments and every segment matches (literal equality, or a
parameter segment per R-COMP-100). Among matching templates the one with the
**most literal segments** wins; ties are broken by the stored template path in
ascending lexicographic (byte) order.
- Evidence: documented ([PR] §Most-literal-wins; table: `/items/:a_param/view.html`
  beats `/items/:b_param/view.html`). Confidence: high. Cases:
  `comp.route.most-literal-wins`, `comp.route.tie-lexicographic`.
- Contradiction C-11: [PR] also describes a segment-by-segment walk that
  "matches a literal child name first, then any `:param` child", which can
  pick a different winner than a global literal count (request `/a/b/c.html`
  with templates `/a/:x/c.html` and `/:y/b/c.html`: both have 2 literals; the
  walk picks `/a/:x/…`, the tie-break picks `/:y/…` since `:` < `a`).
  Decision: global count + lexicographic tie-break. Probe Q-10.

**R-COMP-103 Directory requests.** A request path ending in `/` is looked up
as `<path>index.html` first (area `reading-writing` R-RW-3), and route
resolution applies to that index path: `/roles/founding-engineer/` resolves
`/roles/:role_name/index.html`.
- Evidence: demo-source ([ATS] `README.html:53`, `index.html:443` links),
  live-observed ([OBS] `https://ats.pagelove.com/roles/founding-software-engineer/`
  served the composed route page). Confidence: high. Case:
  `comp.route.directory-index`.
- Open: slash-less `/roles/x` — redirect (R-RW-4) or 404? (Q-11).

**R-COMP-104 Captures.** Captured values are percent-decoded
(`/pages/hello%20world.html` → `slug = "hello world"`) and exposed as
`request.params.<name>` (Sessel, Liquid, JS; `Context.request.params`).
`request.params` is a shared request member: route pages that read only it
stay publicly cacheable per concrete URL.
- Evidence: documented ([PR] §Resolution, §Reading captured parameters — the
  Liquid bullet is empty in the snapshot, rendering glitch). Confidence: high.
  Cases: `comp.route.percent-decoded`, `comp.route.params-in-liquid`.

**R-COMP-105 `route-parameters` response header.** A response served through a
route carries `route-parameters: <name>=<value>` (live-observed form for one
parameter: `route-parameters: slug=building-our-own-hiring-platform`).
pagelike MUST emit it; for several parameters pagelike joins pairs with `, `
in template order (decision), values as captured (decoded).
- Evidence: live-observed ([OBS] `secondary-index.json` headers of four
  `blog.pagelove.com/posts/*.html` responses). Confidence: high (single),
  low (multi format). Probe Q-12. Case: `comp.route.route-parameters-header`.

**R-COMP-106 Writes to route URLs.**
- Whole-document writes (PUT, POST, DELETE, MOVE without `Range`) are literal:
  `PUT /users/42/profile.html` creates a literal document that then shadows
  the template for that URL; `PUT /users/:id/profile.html` edits the template.
- Selector writes to a concrete route URL: resolve the route, compose the
  template with `request.params`, then route to the origin of a
  **stamped or included** target (R-COMP-91). If the selector matches nothing,
  or matches an element that is not projected, → **416**. The template is
  never written through a concrete URL. Authorization is evaluated against the
  concrete URL (area `permissions` R-PERM-19).
- A concrete path matching no route → **404**.
- Evidence: documented ([PR] §Resolution; [ST] §Interaction last paragraph;
  [BLOG] comments). Confidence: high. Cases: `comp.route.whole-put-literal`,
  `comp.route.selector-write-through-stamp`, `comp.route.selector-write-non-projected-416`.

**R-COMP-107 Selector reads of route URLs.** A `GET` with `Range: selector=…`
to a path with no literal document → **404** (routes resolve only for
whole-document reads).
- Evidence: documented ([PR] §Error cases). Confidence: medium — it
  contradicts the write rule above and beta-js/primitives patterns that read
  elements of the current page by selector (C-12). pagelike follows the
  table. Probe Q-13. Case: `comp.route.selector-get-404`.

**R-COMP-108 Errors.** No literal document and no matching template → 404;
directory segments match but the final document is absent → 404 (no partial
match). A route page whose binding finds no record still composes with 200;
authors change the status with a Processor ([BLOG] §Making the 404 real,
area `reactions`).
- Evidence: documented ([PR] §Error cases; [BLOG]). Confidence: high. Cases:
  `comp.route.no-match-404`, `comp.route.no-partial-match-404`,
  `comp.route.missing-record-200`.

**R-COMP-109 Reading the template by its literal path.** `GET
/users/:id/profile.html` (literal colon) serves the template document itself
with an empty `request.params`; a binding that reads a missing param gets
null (inferred, low; Q-14).

---

## 16. Pagination (`p:paginate`)

**R-COMP-130 Form and pass.** `p:paginate="<n>"` on an element splits its
direct **element** children into pages of `n` (the default page length).
Pagination runs after includes, bindings, templates and methods, on the
fully resolved DOM. Children outside the current page are removed; text nodes
(whitespace) and comments stay.
- Evidence: documented ([PAG] §Attribute form, §When to reach for it; example
  output keeps blank lines). Confidence: high (element children), medium
  (comments not counted). Case: `comp.pag.after-template`.

**R-COMP-131 Query parameters.** Pages are 1-indexed.

| Paginators in document | Page | Length |
|---|---|---|
| one | `paginate:page` | `paginate:length` |
| several | `paginate:<id>:page` | `paginate:<id>:length` |

Defaults: page 1, length = attribute value. Non-numeric, zero or negative
values fall back to the defaults silently. A page beyond the last is clamped to
the last page. With one paginator that has an `id`, pagelike also accepts the
id-prefixed form (inferred, low).
- Evidence: documented ([PAG] §Query parameters; [R-PAG]). Confidence: high.
  Cases: `comp.pag.clamp-beyond-last`, `comp.pag.invalid-query-fallback`,
  `comp.pag.length-override`, `comp.pag.multiple-with-ids`.
- live 2026-09-29 (adopted, docs/compat/decisions-2026-09-29/composing-liquid.md):
  every paginator reads `paginate:<id>:page` / `:length` when it has an `id`
  and the request carries it, and otherwise the unprefixed `paginate:page` /
  `paginate:length`, which every paginator shares (with one or several).
  Probes: `comp.pag.probe-0929.*`.

**R-COMP-132 Navigation links.** When the element has more than one page,
pagination emits for each paginator:
- `<link>` elements appended as the last children of `<head>`, no whitespace
  between them, in order `first`, `prev`, `next`, `last`:
  `<link rel="first" href="?paginate:page=1&amp;paginate:length=3" title="contacts">`
  — `href` is a query-only relative URL, `title` is the paginator's `id`
  (omitted when it has none);
- one HTTP `Link` header value per link (RFC 8288):
  `</contacts.html?paginate:page=1>; rel="first"` — path-absolute URL of the
  request path plus the query.

`prev` is absent on page 1 and `next` on the last page; `first` and `last` are
present whenever there is more than one page (pagelike decision). A single page
or an empty element produces no links. The query of each link = the request's
query with this paginator's page parameter set to the target page, keeping
every other parameter (non-pagination parameters, other paginators' state,
this paginator's `length` if the request had one) in its original position;
new parameters are appended. Colons are not percent-encoded.
- Evidence: documented ([PAG] §Navigation links example: request
  `?paginate:page=2&paginate:length=3` of 7 items gives first/prev =
  page 1, next/last = page 3, each with `&amp;paginate:length=3` and
  `title="contacts"`; [R-PAG] §Navigation links: Link header form, page 1 has
  no prev, last page no next, `q=smith` carried forward). Confidence: high
  (element form, prev/next rules, carry-forward), medium (header URL form,
  first/last always), low (parameter order when adding). Cases:
  `comp.pag.links-and-slice`, `comp.pag.single-page-no-links`,
  `comp.pag.first-page-no-prev`, `comp.pag.last-page-no-next`,
  `comp.pag.preserve-other-params`, `comp.pag.link-header`.
- Contradiction C-13: [PAG] `<link href>` is query-only while [R-PAG]'s `Link`
  header carries the path; both are kept (they are different carriers).
- live 2026-09-29 (adopted, supersedes the forms above;
  docs/compat/decisions-2026-09-29/composing-liquid.md): each link, element
  and header alike, is the request path plus this paginator's page parameter
  only — `paginate:<id>:page=N` with an `id`, `paginate:page=N` without; the
  length, other parameters and other paginators' state are dropped. Order
  `first`, `last`, `prev`, `next`. With an `id` the element has
  `title="<id>"` and the header value `; title="<id>"`. Links go into a
  `<head>` written in the source only; with an implied `<head>` only the
  `Link` header carries them.

**R-COMP-133 With `Range`.** A selector read composes and paginates the whole
document first, then extracts (`Range: selector=ul#contacts` returns the
current page's list, `206`).
- Evidence: documented ([PAG] §With Range selectors). Case: `comp.pag.range-fragment`.

**R-COMP-134 Errors.** `p:paginate` value not a positive integer → **422**;
several paginators in one document and any of them lacks an `id` → **422**; no
children → element returned as-is, no links.
- Evidence: documented ([PAG] §Error cases). Confidence: high. Cases:
  `comp.pag.invalid-attribute-422`, `comp.pag.multiple-without-ids-422`,
  `comp.pag.empty-element`.
- live 2026-09-29 (adopted): several paginators without ids are not an
  error; each is served (200) and driven by the unprefixed parameters.

**R-COMP-135 Interactions.** Pagination (this attribute) is unrelated to the
`entries` range unit and to QUERY result paging (area `protocol`). Selector
writes on a paginated page resolve against the paginated view (inferred;
Q-15).

---

## 17. Templated resource creation

**R-COMP-140 Trigger.** A `POST` **without** a `Range` header to an existing
HTML document is templated creation (area `reading-writing` R-RW-78). Direct
creation is an ordinary whole-document `PUT` (201; one authorization check).
- Evidence: documented ([RC] §Mode comparison, §Direct creation). Case:
  `comp.rc.direct-put`.

**R-COMP-141 Pipeline.**
1. Authorize `POST` on the template path; on failure reject (401/403)
   **before any templating**.
2. Read the whole request body; if it cannot be fully read → **400**, nothing
   written.
3. Compose the template document as for a GET, with `request.method = POST`,
   `request.body` (R-COMP-24), query, headers and identity available.
4. Find the first `<base>` element with a non-empty `href` in the output; none,
   empty `href`, or no `href` → **422** "Template must include a `<base href>`
   element specifying the target resource path".
5. Resolve `href` against the template URL; the result's path (percent-decoded)
   is the target path. A cross-origin `href` → 422 (pagelike decision).
6. Authorize `PUT` (whole document, no selector) at the target path; on failure
   reject, nothing written.
7. Store the output at the target path through the ordinary whole-document PUT
   pipeline (triggers see `request.method = PUT`, `request.path = <target>`, no
   `range` header; schema/shape validation, reserved-namespace check, events).
   An existing document at the target is **replaced**.
8. Respond **`301 Moved Permanently`** with `Location: <target path>`.
- Evidence: documented ([RC] §Templated creation, §Error cases, §Examples:
  `POST /sspi-rc-templates/new-entry.html` url-encoded
  `slug=hello-world&title=Hello+World&content=First+post` → `301`,
  `Location: /sspi-rc-entries/hello-world.html`), demo-source ([POLL]
  `admin/auth.html:36-52` rules, `:130-150` a PUT trigger keyed on
  `request.path` and absent `range` that "refuses" overwriting an existing poll
  — which shows the platform overwrites by default; [POLL] `assets/create.js:2`
  "follows the 301"). Confidence: high (steps 1,4,6,8), medium (7 details),
  low (5 cross-origin). Cases: `comp.rc.post-template-301`,
  `comp.rc.missing-base-422`, `comp.rc.empty-base-href-422`,
  `comp.rc.post-not-authorized`, `comp.rc.put-at-base-not-authorized`,
  `comp.rc.overwrites-existing`.
- Doc defect: [RC] §6 shows `Location:` with an empty value; the example
  gives the path. The `Location` is path-absolute (pagelike; clients follow it).

**R-COMP-142 Request body.** For `application/x-www-form-urlencoded` bodies,
`request.body.<field>` holds the decoded value (`+` → space).
- Evidence: documented ([RC] example), demo-source ([POLL] `new-poll.html`).
  Confidence: high. Case: `comp.rc.request-body-fields`.

**R-COMP-143 What is stored.** The stored document is the composed output:
Liquid rendered, directive attributes consumed (no `p:template`), the `<base>`
element **removed** (its surrounding whitespace kept), `xmlns:*` declarations
**kept** (stripping is response-only).
- Evidence: demo-source ([POLL] `polls/kfd47o4zqd.html:1-11`: `xmlns:p` kept,
  no `p:template`, a blank line where `<base href="/polls/{{ id }}.html">` was,
  `<title>` with the three newlines left by three Liquid tag lines).
  Confidence: medium (rendered, no template attr), low (`<base>` removal).
  Cases: `comp.rc.stored-output`, `comp.rc.base-removed`.

**R-COMP-144 Response body.** Unspecified; pagelike sends an empty body with
the 301 (inferred).

---

## 18. XML documents

**R-COMP-123 XML family.** A document is XML when its stored content type is
`application/xml`, `text/xml` or any `…+xml` type (`application/rss+xml`,
`application/atom+xml`, `image/svg+xml`, `application/xhtml+xml`, sitemaps…).
The type comes from the PUT `Content-Type` or the file extension (area
`reading-writing` R-RW-60). Responses keep the stored type.
- Evidence: documented ([XML] §XML-family content types). Confidence: high.

**R-COMP-124 Dialect.** Parsed as XML: element and attribute names are
case-sensitive; no void or raw-text elements; processing instructions
(including `<?xml …?>`) are recognised.
- Evidence: documented ([XML] §The XML dialect). Confidence: high.

**R-COMP-125 Same pipeline.** `e:`/`r:` bindings, `pagelove:template` (Liquid
output is parsed as XML), `<p:stamp>`, method elements, `<p:include>`,
transient elements and `@read` resolvers all work on XML.
- Evidence: documented ([XML] §Composition; [BLOG] §The feed). Confidence:
  high. Cases: `comp.xml.atom-stamp`, `comp.xml.liquid-feed`.

**R-COMP-126 Serialization.** Well-formed XML: empty elements self-close
(`<entry/>`), name case preserved, processing instructions and the XML
declaration kept, text and attribute values XML-escaped. Directive attributes
are consumed (R-COMP-14.3); **all** `xmlns:` declarations remain.
- Evidence: documented ([XML] §The XML dialect, §Differences, §Example).
  Confidence: high (xmlns kept, self-close), medium (directive attributes
  removed in XML — not shown). Cases: `comp.xml.self-closing-case-preserved`,
  `comp.xml.xmlns-preserved`.

**R-COMP-127 No JSON-LD.** An XML document is always served as XML regardless
of `Accept` (area `reading-writing` R-RW-39).
- Evidence: documented. Case: `comp.xml.no-jsonld`.

**R-COMP-128 Unchanged when inert.** An XML document with no Pagelove
namespaces and no microdata is served unchanged (pagelike: stored bytes).
- Evidence: documented ([XML] §Composition). Confidence: high (unchanged),
  medium (byte-identical). Case: `comp.xml.unchanged-without-directives`.

---

## 19. The Request Document

**R-COMP-120 Composition input.** For each request a transient HTML document
(root item `https://pagelove.org/Request` on `<body>`, shape in area
`reading-writing` R-RW-136: `path`, `method`, raw `query`, raw `body`, a
`query` item of type `https://pagelove.org/Request/HTTP/Query`, a `headers`
item `…/Request/HTTP/Headers` with lower-case names, an `auth` item
`https://pagelove.org/Authorization` with `claims`, `username`, `role`…) is
part of the site graph for `r:` bindings and global `<p:include>`s. It has no
HTTP address and is never stored.
- Evidence: documented ([RD]). Confidence: high. Cases:
  `comp.rb.request-document-binding-private`, `comp.inc.request-document-private`.

**R-COMP-121 Private.** Any page that binds or includes a fragment of it is
`Cache-Control: private` (R-COMP-150).

**R-COMP-122 `request` object instead.** The same data without a selector is
the `request` variable (R-COMP-24). The documented example renders
`Method: GET` and `Path: /<page>`.
- Case: `comp.tpl.request-object`.

---

## 20. Response headers and caching of composed responses

**R-COMP-150 `Cache-Control: private`.** A composed response (whole document
or fragment, 200/206/304) is `Cache-Control: private` when composition read
per-requester state: any `request.auth.*`, `request.headers.*` or other
identity member (Sessel, Liquid or JS); any fragment of the Request Document
(binding or include); or the document carries a transient marker. Reading
only `request.path`, `request.method`, `request.query`, `request.params`
keeps the response shareable (then reading-writing R-RW-102 applies).
- Evidence: documented ([RB] note; [RD] §Caching; [JB] §Caching; [SES];
  [TR]; [PR] §Reading captured parameters), live-observed ([OBS] blog pages
  that read `request.auth.claims.email` for draft gating are served
  `Cache-Control: private`). Confidence: high.
- Taint is per response, propagated from any nested composition (includes,
  stamps, templates inside included fragments).

**R-COMP-151 Other headers.** Composed HTML: `Content-Type: text/html`
(live sends no charset), `Vary: Host, Range, Accept` (live), `ETag`
(live composed whole pages carry **weak** tags `W/"<64 hex>"`, C-14), no
`Cache-Control: public, max-age=300` (that is for static assets). Routes add
`route-parameters` (R-COMP-105); pagination adds `Link` (R-COMP-132).
- Evidence: live-observed ([OBS] blog headers). Confidence: medium.
  Cross-area: reading-writing R-RW-95/96 specify strong tags; decision
  deferred to that area.

---

## 21. Status control and composition errors

**R-COMP-160 Composition failures fail the request.** Any composition error
fails the whole request (whole or fragment read, selector write resolution,
templated creation); no partial page is returned. The body is an HTML error
document (reading-writing R-RW-130, generic `https://pagelove.org/Error` item
with `status`, `kind`, `message`); cases assert the status only.

**R-COMP-161 Status table.**

| Condition | Status | Evidence |
|---|---|---|
| `<p:include>` matches nothing | 404 | documented |
| `<p:include>` matches several | 500 | documented |
| element-form method not found / unbound prefix | 500 `no method found for <prefix>:<name> and no doesNotUnderstand defined` | documented |
| unknown element on Pagelove namespace | 500 `unknown method '<name>' on Pagelove namespace` | documented |
| method implementation error | 500 (message) | documented |
| Sessel binding compile error / undefined variable | 500 (pagelike) | documented failure, inferred status |
| JavaScript binding parse/throw/reject | 500 (pagelike) | documented failure |
| invalid `r:` selector, include without selector | 500 (pagelike) | documented failure / inferred |
| Liquid control-flow error | 500 (pagelike) | documented failure |
| dispatch budget (500) or template budget exhausted | 503 | documented |
| `p:paginate` invalid (multiple paginators without ids: 200, live 2026-09-29, R-COMP-134) | 422 | documented |
| templated creation without usable `<base href>` | 422 | documented |
| templated creation body interrupted | 400 | documented |
| transient: no session / unsupported method / identity changed | 409 / 405 / 422 | documented |
| route: no match, no partial match, selector GET | 404 | documented |
| route or page: selector write to non-projected/generated or unmatched target | 416 | documented (routes) / inferred |

**R-COMP-162 Status is otherwise 200.** Composition itself never picks a
non-error status (a page that finds no record is still 200). Status rewriting
belongs to Processors (`Context.response.status`, area `reactions`); the
[BLOG] tutorial uses a Processor on `/posts/*` GET with `status` 200 and a
`when` on `Context.response.body.contains(…) == false` to set 404, keeping the
body.
- Evidence: documented ([BLOG] §Making the 404 real; [PROC]). Confidence: high.

---

## 22. Selector extensions

These apply to every selector PageLove evaluates: `Range: selector=` (all
methods), `r:` bindings, `<p:include selector>`, Sessel `${…}`, the edge
`QUERY`, AuthorizationRule and ShapeConstraint selectors (package
`internal/selector`). In addition PageLove supports CSS Selectors Level 4
(`:has()`, `:is()`, `:where()`, `:not()`, structural pseudo-classes);
namespace selectors (`prefix|tag`, `[prefix|attr]`) with the `Namespace` header
(area `protocol`).
- Evidence: documented ([SX] intro; [RB] See also; [SX] §Non-ASCII text uses a
  `Range` example). Confidence: high.

**R-COMP-170 Text content.** The concatenated text of the element and its
descendants, **normalized**: (1) Unicode NFC, (2) trim leading/trailing
whitespace, (3) collapse runs of spaces/tabs/newlines to one space.
`<p> Hello World\n</p>` → `Hello World`. The same normalization applies to
the pseudo-class argument? — pagelike normalizes the argument with NFC only
(inferred).
- Evidence: documented ([SX] §Text normalization). Confidence: high. Case:
  `comp.sx.normalization`.

**R-COMP-171 Microdata value.** Per element: `meta` → `content`; `audio`,
`embed`, `iframe`, `img`, `source`, `track`, `video` → `src`; `a`, `area`,
`link` → `href`; `object` → `data`; `data`, `meter` → `value`; `time` →
`datetime`, falling back to text content; others → descendant text content.
A listed element missing its attribute → empty string (except `time`). URL
attributes are compared **as written** (not resolved to absolute URLs):
`[itemprop=url]:value-equals('/about')` matches `<a href="/about">`. Values
used by `:value-*` are normalized like text (inferred, same "all text-based
operations" rule).
- Evidence: documented ([SX] §Text content vs microdata values, §Value
  matching table). Confidence: high (table, raw URLs), medium (normalization
  of attribute values). Cases: `comp.sx.value-matching`, `comp.sx.time-and-missing-attr`.
- Contradiction C-15: WHATWG microdata resolves URL values to absolute URLs;
  the selector table compares the raw attribute. Decision: raw for selectors
  (and Liquid, R-COMP-52); JSON-LD extraction is reading-writing R-RW-51.

**R-COMP-172 Text and value matching.**

| Pseudo-class | Operand | Match |
|---|---|---|
| `:contains(t)` | text content | substring |
| `:equals(t)` | text content | whole string equality |
| `:value-contains(t)` | microdata value | substring |
| `:value-equals(t)` | microdata value | whole string equality |

Case-sensitive by default; a trailing `, i` argument enables **ASCII**
case-insensitivity (A–Z fold to a–z; `é` does not match `É`), the same for all
four. The empty argument: `:contains('')` matches every element (substring),
`:equals('')` matches elements whose normalized text is empty (inferred).
- Evidence: documented ([SX] §Text matching, §Value matching, §Case
  sensitivity; examples `h1:contains('Hello')` matches, `h1:contains('hello')`
  does not, `:contains('hello', i)` matches both; `p:equals('Title')` only the
  exact `<p>`). Confidence: high. Cases: `comp.sx.contains`, `comp.sx.equals`,
  `comp.sx.case-flag-ascii-only`.

**R-COMP-173 Arguments and quoting.** Arguments are CSS strings in single or
double quotes; CSS escapes work (`'caf\E9 '`, trailing space ends the escape);
non-ASCII text may be sent raw in the `Range` header (decoded as UTF-8). An
unquoted identifier argument is accepted as its literal text in plain CSS
contexts (pagelike decision, low); in Sessel `${…}` an unquoted argument is a
Sessel expression (R-COMP-36).
- Evidence: documented ([SX] §Quoting, §Non-ASCII text in selectors).
  Confidence: high (quotes, UTF-8, escapes). Case: `comp.sx.quoting-and-escapes`.

**R-COMP-174 Numeric comparison.** `:less-than(n)`, `:greater-than(n)` compare
the text content; `:value-less-than(n)`, `:value-greater-than(n)` the
microdata value. The operand is trimmed and parsed as a decimal number
(`"9.99"` → 9.99; pagelike accepts `^[+-]?(\d+(\.\d*)?|\.\d+)$`); empty or
non-numeric operands never match (no error). Strict `<`/`>`. No `, i` flag.
The threshold is quoted (`:less-than('10')`); an unparsable threshold makes the
selector invalid (pagelike decision).
- Evidence: documented ([SX] §Numeric comparison). Confidence: high
  (trim/decimal/non-numeric), low (exponents, signs, threshold errors;
  Q-16). Case: `comp.sx.numeric`.

**R-COMP-175 `:only(sel, …)`.** Matches an element when **every descendant**
matches at least one argument selector. Arguments are relative to the subject:
a leading `>` means direct child (`> li`), otherwise any descendant; commas
are OR. An element with no descendants matches (vacuous).
`ul:only(> li)` matches `<ul><li>A</li><li>B</li></ul>`, not
`<ul><li>A</li><div>X</div></ul>`; `ul:only(> li, > li span)` allows spans in
items.
- Evidence: documented ([SX] §Structure validation). Confidence: high. Case:
  `comp.sx.only`.

**R-COMP-176 `:has()` OR.** Comma-separated arguments inside `:has()` are OR
(CSS spec). `[itemtype*=HostConfig]:has([itemprop=hostname]:value-equals('localhost'),
[itemprop=alias]:value-equals('127.0.0.1'))` matches a config having either.
- Evidence: documented ([SX] §Value matching). Case: `comp.sx.has-or`.

**R-COMP-177 `:isa(url)`.** Matches elements whose `itemtype` (any
whitespace-separated token, inferred) equals `url` or a descendant type of
`url` in the site's schema inheritance hierarchy (area `modeling` R-MOD-12).
Reflexive. With **no** schema hierarchy loaded it matches nothing, not even
exact equality.
- Evidence: documented ([SX] §Type inheritance matching). Confidence: high
  (with schemas), medium (no-schema rule). Cases: `comp.sx.isa-with-schema`,
  `comp.sx.isa-no-schema` (local only: a live host may have schemas).
- Open (Q-17): whether reflexive matching of a type that is not itself a
  declared schema works once any schema is loaded; whether system subtypes
  (`PathAuthorizationRule` ⊂ `AuthorizationRule`) are built in.

**R-COMP-178 Selector functions.** Evaluated before the selector is parsed,
over the **whole site graph**, each replaced textually by its result:

| Function | Result |
|---|---|
| `count(sel)` | number of matching elements (unquoted integer) |
| `text-of(sel)` | quoted normalized text content of the single match |
| `value-of(sel)` | quoted microdata value of the single match |
| `attr-of('name', sel)` | quoted value of attribute `name` of the single match |

`div:nth-child(count(h1))` with three `h1` on the site becomes
`div:nth-child(3)`. For the three single-match functions: zero matches →
`""`; more than one → error. pagelike status for that error: the
selector is invalid → as an invalid selector in the calling context (`Range`
→ 422, area `reading-writing` R-RW-17; binding → 500).
- Evidence: documented ([SX] §Selector functions). Confidence: high (results),
  low (error status). Cases: `comp.sx.count-function`,
  `comp.sx.text-value-attr-of`, `comp.sx.function-zero-match-empty`,
  `comp.sx.function-multi-match-error`.
- Security: functions read the whole host (R-COMP-32); a function inside a
  `Range` header lets any reader probe values of documents they cannot GET
  (e.g. `[data-x=text-of(#secret)]`: 206 vs 416 reveals equality). pagelike
  keeps the documented semantics and flags it (Q-18).

---

## 23. Client-visible guarantees (why the details matter)

**R-COMP-180 Composed output must round-trip in a browser.** Clients generate
selectors against the **browser's** DOM of the composed page (beta-js
primitives prefer `#id`, then `itemprop`, classes, positions — [R-PRIM] §Read
a specific element) and send them back in `Range`. The server resolves them
against its composed view. Therefore the served HTML must parse in a browser
to the same tree the server uses: no directive leftovers, no foster-parenting
differences, pagination `<link>` elements present in both.
- Evidence: documented ([R-PRIM]), demo-source ([SHOP] `shop.mjs` reads
  stamped microdata and PUTs selector writes; [ATS] reads included rows).
  Confidence: medium.

**R-COMP-181 Microdata survives composition.** Stamped/included items keep
`itemscope`/`itemtype`/`itemprop`, so client libraries and widgets reading
`[itemprop]` ([R-WID]) and SSE-applied mutations see the same items.

---

## 24. Cross-area dependencies

| Area | Dependency |
|---|---|
| reading-writing | R-RW-15/16 first match on the composed view; R-RW-21 serialization; R-RW-64/72/80 write responses reused for routed writes; R-RW-78 POST without Range → templated creation; R-RW-102/103 caching; R-RW-130 error documents; R-RW-135..137 Request Document shape; ETag strength of composed pages (C-14) |
| permissions | R-PERM-19/28: rules match the concrete route URL and the composed page for write-through; key element for transient writes; glob language (R-PERM-20) reused by `<p:include resource>` |
| identity | session cookie minting and name (`__Host-session` vs `pagelove_session`, C-10); `request.auth.*` claims, roles/groups |
| modeling | schema registry for method dispatch and `:isa()`; `@read` resolvers in composition; `@key` id emission for stamped instances; ShapeConstraint applicability for routed writes (R-MOD-62); registration timing (R-MOD-4) |
| reactions | Processors change status/body after composition ([BLOG] 404); triggers fire on the internal PUT of templated creation and on routed writes (origin) |
| sse | routed writes announce on the origin path (R-SSE-19); includes do not propagate (R-SSE-20); transient writes emit nothing (R-SSE-21) |
| protocol | edge `QUERY` runs on the composed page (R-PROTO-61); dav plane never composes (R-PROTO-113); `Namespace` header for namespaced selectors |
| liquid | language, filters, error markers, limits; value conversion of Sessel values |
| sessel / jsrt | expression languages, `${…}` site-wide queries, `from` scopes, error classes, budgets |

---

## 25. Contradictions and compatibility decisions

| # | Topic | Claims | Decision |
|---|---|---|---|
| C-1 | Is `xmlns:p="https://pagelove.org/1.0"` required to enable composition? | [RB] "must also be enabled by declaring"; [ME] unbound-prefix 500 example; [EB]/[JB] examples with undeclared `pagelove:` | Always compose; accept undeclared `pagelove:` (low). Q-1 |
| C-2 | Liquid / includes inside tables | [CURSOR] (April) foster parenting breaks them; [POLL]/[ATS] (Sept) and live output work | No foster parenting |
| C-3 | Wrong-URI binding attributes | [CURSOR] "won't be stripped"; [ME] table: bound prefix without schema → stripped | Stripped. Q-4 |
| C-4 | Which `xmlns:` are stripped from HTML | [INC] "every `xmlns:*`"; [XML] "Pagelove `xmlns:` declarations" | Every `xmlns:*` |
| C-5 | QUERY composed example shows `pagelove:template` | [QRY] body is the setup PUT echo (status 201) | Ignore; strip |
| C-6 | Scope of method Context mutations | [ST] propagates to later dispatches; [ME] subtree only | Subtree. Q-5 |
| C-7 | Roles member name | [SES] `request.auth.roles`; [KAN] `request.auth.role`; [RD] itemprop `role` | Provide both |
| C-8 | Attribute evaluation order | [EB] "left to right in source order"; [ME] "not dispatched in the order written" | R-COMP-25 order (resource → others in source order → template) |
| C-9 | POST into transient elements | [TR] PUT or POST; [SHOP] treats POST as undocumented | Support POST |
| C-10 | Session cookie name | [TR] `pagelove_session`; live `__Host-session` | Area identity; cases never assert the name |
| C-11 | Route precedence algorithm | [PR] segment walk (literal first) vs global most-literal + lexicographic | Global count + lexicographic. Q-10 |
| C-12 | Selector GET on a route URL | [PR] error table 404; selector writes do resolve routes | Follow docs (404). Q-13 |
| C-13 | Pagination link URL form | [PAG] `href="?…"`; [R-PAG] `Link: </contacts.html?…>` | Both, per carrier |
| C-14 | ETag strength of composed pages | reading-writing R-RW-95 strong; live composed blog pages weak `W/"…"` | Deferred to reading-writing; cases never assert strength |
| C-15 | URL microdata values | WHATWG absolute; [SX] raw `href` | Raw in selectors and Liquid |
| C-16 | Legacy resource namespace | [CURSOR] `https://pagelove.org/1.0/Resource`; docs `…/Binding/CSS` | Accept both (low). Q-3 |

---

## 26. Open questions for live probing

Each probe uses a disposable host, paths under a unique prefix `/_pl/q`, a rule
file allowing `GET, HEAD, PUT, POST, DELETE` for `*` on `/_pl/q/*`, and only
the anonymous identity unless noted. "Store" = WebDAV PUT.

- **Q-1 Opt-in and undeclared prefixes.** Store `a.html` =
  `<html><body><div e:x="'1'">A</div><i:x></i:x><o:p></o:p></body></html>` (no
  declarations). `GET /_pl/q/a.html` → 500 or 200? Store `b.html` =
  `<html xmlns:e="https://pagelove.org/Binding/Sessel"><body><div pagelove:template="text/liquid" e:v="'OK'">{{ v }}</div></body></html>`
  → body contains `OK`, or raw `{{ v }}`, or 500?
- **Q-2 Attribute-name case.** Store
  `<html xmlns:p="https://pagelove.org/1.0" xmlns:e="https://pagelove.org/Binding/Sessel"><body><div e:myVal="'M'" p:template="text/liquid">[{{ myVal }}][{{ myval }}]</div></body></html>`
  → which bracket is filled?
- **Q-3 Legacy namespace.** Same as `comp.rb.contacts-template` but
  `xmlns:r="https://pagelove.org/1.0/Resource"` → list rendered?
- **Q-4 Wrong binding URI.** `xmlns:e="https://pagelove.org/Binding/Sessel/"`
  and `<div e:x="'1'">` → is `e:x` present in the served HTML?
- **Q-5 Element-form Context mutation.** Schema `urn:q5` with Sessel method
  `set` (no `returns`) doing `Context.flag = "F"; null`; page
  `<t:set></t:set><p:stamp flag></p:stamp>` (siblings) → `F` stamped or
  nothing?
- **Q-6 Repeated query/form fields.** `GET /_pl/q/t.html?a=1&a=2` with
  `<div p:template="text/liquid">{{ request.query.a }}</div>` → `1`, `2`,
  or a list?
- **Q-7 Materialization.** Bind `r:x="[data-q]"` over
  `<p data-q>plain</p><div data-q id="k" itemscope><span itemprop="n">v</span></div>`
  and render `{{ x[0] }}|{{ x[0]['@id'] }}|{{ x[1]['@id'] }}|{{ x[1]['@type'] }}|{{ x[1] }}`.
- **Q-8 Include composition context.** Origin `o.html` declares only
  `xmlns:e` and `xmlns:p` on its `<html>` and contains
  `<div id="frag" e:v="'X'"><p:stamp v></p:stamp></div>`; page declares only
  `xmlns:p` and includes `#frag` → `X` rendered? And does a `p:template`
  inside the fragment see bindings of the including page's ancestors?
- **Q-9 Session content inert?** Transient `<ul id="c" p:transient>`; PUT
  `<ul id="c"><li p:template="text/liquid">{{ 1 | plus: 1 }}</li></ul>` →
  next GET shows `2` or `{{ 1 | plus: 1 }}`?
- **Q-10 Route precedence.** Store `/_pl/q/a/:x/c.html` (body `X`) and
  `/_pl/q/:y/b/c.html` (body `Y`); `GET /_pl/q/a/b/c.html` → X or Y?
- **Q-11 Slash-less route directory.** With `/_pl/q/r/:n/index.html`,
  `GET /_pl/q/r/abc` → 301 to `/_pl/q/r/abc/` or 404?
- **Q-12 `route-parameters` with two captures.** Template
  `/_pl/q/o/:org/t/:team.html`; `GET /_pl/q/o/a%20b/t/c.html` → exact header
  value (separator, encoding).
- **Q-13 Selector GET on a route URL.** `GET /_pl/q/users/1/p.html` with
  `Range: selector=main` → 404 (docs) or 206?
- **Q-14 Template by literal path.** `GET /_pl/q/users/:id/p.html` where the
  page binds `e:u="request.params.id"` → 200 with empty stamp, or 500?
- **Q-15 Selector writes on paginated pages.** 7 items, `p:paginate="3"`;
  `PUT ?paginate:page=2` with `Range: selector=li:first-child` → which item
  changes (Dave or Alice)?
- **Q-16 Numeric grammar.** `<span>1e3</span><span>+5</span><span>-2</span><span>1,000</span>`
  with `span:greater-than('100')` and `:less-than('0')` → which match?
- **Q-17 `:isa()` reflexivity.** On a host with at least one schema, element
  `itemtype="https://example.test/NoSchema"` and
  `Range: selector=:isa('https://example.test/NoSchema')` → 206 or 416?
- **Q-18 Selector functions over denied data.** Deny GET on
  `/_pl/q/secret.html` (`<p id="s">abc</p>`); `GET /_pl/q/pub.html` with
  `Range: selector=[data-x=text-of(#s)]` where pub has `<i data-x="abc">` →
  206 (leak) or 4xx?
- **Q-19 Pagination link details.** 7 items `p:paginate="3"`; `GET` page 1 →
  are `rel=first`/`rel=last` present? Exact `Link` header (one or several
  fields, `title` param)? Request `?paginate:length=3` only → order of params
  in the `next` link.
- **Q-20 Templated creation leftovers.** POST to a template with
  `<html xmlns:p=… p:template="text/liquid">…<base href="/_pl/q/n.html">…`;
  WebDAV GET `/_pl/q/n.html` → is `<base>` present? `xmlns:p`? `p:template`?
  Response body of the 301?
- **Q-21 Weak ETags.** `GET` a composed page with a binding and a static page
  → `W/` on either? Does `If-None-Match` with the weak tag give 304?
- **Q-22 Include error body.** `GET` a page whose include matches nothing →
  status 404 and the error item's `itemtype`/properties.

---

## 27. Case index

| File | Cases |
|---|---|
| `namespaces.yaml` | `comp.ns.strip-xmlns-and-directives`, `comp.ns.strip-foreign-xmlns`, `comp.ns.prefix-is-arbitrary`, `comp.ns.unbound-attr-left-as-is`, `comp.ns.unbound-element-500`, `comp.ns.bound-unknown-uri-attr-stripped`, `comp.ns.wrong-sessel-uri-ignored`, `comp.ns.self-closing-prefixed-element`, `comp.ns.no-foster-parenting-include-in-table` |
| `resource-binding.yaml` | `comp.rb.contacts-template`, `comp.rb.multiple-bindings-one-element`, `comp.rb.empty-collection`, `comp.rb.bypasses-read-authorization`, `comp.rb.invalid-selector-fails`, `comp.rb.multi-valued-property-list`, `comp.rb.at-id-path-fragment`, `comp.rb.where-sort-reverse`, `comp.rb.not-cached`, `comp.rb.template-inert-boundary`, `comp.rb.template-documents-visible`, `comp.rb.request-document-binding-private`, `comp.rb.extension-in-binding` |
| `expression-binding.yaml` | `comp.eb.count-sum`, `comp.eb.declaration-order`, `comp.eb.nearest-ancestor-wins`, `comp.eb.sibling-scope`, `comp.eb.undefined-variable-fails`, `comp.eb.compile-error-fails`, `comp.eb.bare-auth-name-undefined`, `comp.eb.from-self-vs-site`, `comp.eb.request-path`, `comp.eb.anonymous-auth-empty-private`, `comp.eb.template-attr-order` |
| `javascript-binding.yaml` | `comp.jb.reduce-example`, `comp.jb.keyword-name-context`, `comp.jb.await`, `comp.jb.interleave-with-sessel`, `comp.jb.throw-fails`, `comp.jb.request-query`, `comp.jb.request-auth-private`, `comp.jb.wrong-namespace-ignored` |
| `templates.yaml` | `comp.tpl.request-object`, `comp.tpl.host-element-kept`, `comp.tpl.html-root-template`, `comp.tpl.liquid-in-table-rows`, `comp.tpl.filter-error-inline`, `comp.tpl.output-is-composed`, `comp.tpl.request-headers-private`, `comp.tpl.undeclared-pagelove-prefix` |
| `method-elements.yaml` | `comp.me.sessel-element`, `comp.me.javascript-element`, `comp.me.attribute-form-dnu`, `comp.me.element-args-by-name`, `comp.me.null-result-removes`, `comp.me.attr-form-undeclared-stripped`, `comp.me.element-form-undeclared-500`, `comp.me.transient-attr-not-dispatched`, `comp.me.template-dispatched-last`, `comp.me.implementation-error-500` |
| `stamp.yaml` | `comp.stamp.expression-value`, `comp.stamp.absent-removed`, `comp.stamp.multiple-names-order`, `comp.stamp.element-origin-kept`, `comp.stamp.from-denied-source`, `comp.stamp.unknown-pagelove-method-500`, `comp.stamp.wrong-p-namespace-500`, `comp.stamp.write-through-put`, `comp.stamp.write-through-post`, `comp.stamp.write-through-delete`, `comp.stamp.write-auth-on-page-not-origin`, `comp.stamp.write-through-if-match-origin` |
| `includes.yaml` | `comp.inc.basic`, `comp.inc.global-selector`, `comp.inc.zero-matches-404`, `comp.inc.multiple-matches-500`, `comp.inc.resource-glob-constrains`, `comp.inc.private-source`, `comp.inc.nested-template-in-fragment`, `comp.inc.range-read-included-element`, `comp.inc.write-through-put`, `comp.inc.request-document-private`, `comp.inc.failed-include-blocks-write` |
| `transient.yaml` | `comp.tr.default-fresh-session`, `comp.tr.private-cache-fragment`, `comp.tr.put-session-only`, `comp.tr.identity-change-rejected-422`, `comp.tr.identity-preserved-tag-change-ok`, `comp.tr.delete-reverts`, `comp.tr.child-mutation`, `comp.tr.post-into-transient`, `comp.tr.binding-sees-canonical`, `comp.tr.cross-session-isolation` |
| `routes.yaml` | `comp.route.basic-id`, `comp.route.filename-param`, `comp.route.multi-params`, `comp.route.percent-decoded`, `comp.route.most-literal-wins`, `comp.route.tie-lexicographic`, `comp.route.literal-document-wins`, `comp.route.no-match-404`, `comp.route.no-partial-match-404`, `comp.route.selector-get-404`, `comp.route.whole-put-literal`, `comp.route.selector-write-through-stamp`, `comp.route.selector-write-non-projected-416`, `comp.route.missing-record-200`, `comp.route.directory-index`, `comp.route.params-in-liquid`, `comp.route.route-parameters-header` |
| `pagination.yaml` | `comp.pag.links-and-slice`, `comp.pag.single-page-no-links`, `comp.pag.range-fragment`, `comp.pag.clamp-beyond-last`, `comp.pag.invalid-query-fallback`, `comp.pag.length-override`, `comp.pag.first-page-no-prev`, `comp.pag.last-page-no-next`, `comp.pag.preserve-other-params`, `comp.pag.multiple-with-ids`, `comp.pag.multiple-without-ids-422`, `comp.pag.invalid-attribute-422`, `comp.pag.empty-element`, `comp.pag.after-template`, `comp.pag.link-header` |
| `resource-creation.yaml` | `comp.rc.post-template-301`, `comp.rc.stored-output`, `comp.rc.base-removed`, `comp.rc.missing-base-422`, `comp.rc.empty-base-href-422`, `comp.rc.post-not-authorized`, `comp.rc.put-at-base-not-authorized`, `comp.rc.overwrites-existing`, `comp.rc.direct-put`, `comp.rc.request-body-fields` |
| `xml.yaml` | `comp.xml.atom-stamp`, `comp.xml.xmlns-preserved`, `comp.xml.no-jsonld`, `comp.xml.unchanged-without-directives`, `comp.xml.self-closing-case-preserved`, `comp.xml.liquid-feed` |
| `selector-extensions.yaml` | `comp.sx.contains`, `comp.sx.equals`, `comp.sx.value-matching`, `comp.sx.time-and-missing-attr`, `comp.sx.numeric`, `comp.sx.only`, `comp.sx.has-or`, `comp.sx.normalization`, `comp.sx.case-flag-ascii-only`, `comp.sx.quoting-and-escapes`, `comp.sx.count-function`, `comp.sx.text-value-attr-of`, `comp.sx.function-zero-match-empty`, `comp.sx.function-multi-match-error`, `comp.sx.isa-with-schema`, `comp.sx.isa-no-schema` |
