# JavaScript: server runtime and client library contract (behavioral spec)

Status: draft. Snapshot of sources: 2026-09-28. Area code: `JS`. Harness cases:
`harness/cases/javascript/*.yaml` (area `javascript`).

This spec has two parts.

- **Part A (server JavaScript runtime).** Everything pagelike needs to evaluate
  server-side JavaScript the way PageLove's `dombase-js` does: the binding item
  and module format, the call signature of every slot that accepts JavaScript,
  `j:` expression bindings, JavaScript method bodies (direct, cross-language and
  composition dispatch), the global environment (what exists and what is
  absent), budgets and limits, marshalling between pagelike values and
  JavaScript, the error model, and the server DOM API as an implementation
  checklist (every interface, member and reflected property).
- **Part B (client library contract).** Every HTTP request the official browser
  client (`beta-js`: `pagelove.mjs`, `pagelove/primitives.mjs`,
  `pagelove/sse.mjs`, `pagelove/component.mjs`, `pagelove/debug.mjs`) issues,
  with method, headers and body, every response property it relies on, and the
  page markup it expects the server to deliver intact.

Out of scope (owned elsewhere, referenced as cross-area dependencies): schema
semantics of each slot (modeling, `docs/spec/modeling.md`), the Sessel
language, Liquid, composition order in general, trigger/processor selection
(reactions), the SSE wire protocol (`docs/spec/sse.md`), OPTIONS/QUERY/MOVE
(`docs/spec/protocol.md`), and selector ranges and ETags
(`docs/spec/reading-writing.md`). The engine and isolation design is in
`docs/decisions/0001-server-js-engine.md` (ADR 0001); this spec is the
behavior that design must deliver.

---

## 0. Conventions

Each requirement has an id `R-JS-<n>` and these fields:

- **Evidence**: `documented` (public docs), `client-source` (beta-js or other
  official client code), `demo-source` (official apps and templates),
  `live-observed` (recorded from real PageLove), `inferred` (reasoned; the
  reason is given). "pagelike decision" marks a choice made where PageLove
  behavior is unknown; each one has a probe in §C.
- **Source**: doc page and section, or `repo/path@commit:line`.
- **Confidence**: high / medium / low.
- Status codes and error documents are stated per requirement. Error bodies are
  HTML Microdata documents; only itemtypes and property names are normative,
  never message text.

MUST / SHOULD / MAY are used in the RFC 2119 sense for pagelike.

### 0.1 Sources and abbreviations

Docs (snapshot `research/docs/2026-09-28/md/`, file prefix `docs.pagelove.com_`):

| Abbrev | Page |
|---|---|
| JSIDX | `languages_javascript` (JavaScript index) |
| JSS | `languages_javascript_server_javascript-in-schemas` |
| DOM | `languages_javascript_server_dom` (overview) |
| DDOC | `languages_javascript_server_dom_document` |
| DELEM | `languages_javascript_server_dom_element` |
| DNODE | `languages_javascript_server_dom_node` |
| DNL | `languages_javascript_server_dom_nodelist` |
| DEX | `languages_javascript_server_dom_dom-exception` |
| DPARSE | `languages_javascript_server_dom_parsing` |
| H:<Name> | `languages_javascript_server_dom_element_<Name>` (per-interface pages) |
| JEB | `reference_composing-pages_JavaScript-Expression-Binding` |
| SEB | `reference_composing-pages_Expression-Binding` |
| ME | `reference_composing-pages_Method-Elements` |
| STAMP | `reference_composing-pages_Stamp` |
| METH / PROP / SCH / RES | `reference_modeling-data_Methods` / `_Property` / `_Schema` / `_Resolvers` |
| TRG / PRC / HREQ | `reference_reacting-to-changes_Trigger` / `_Processor` / `_HTTPRequest` |
| LIQ | `languages_liquid_templating` |
| PLC / PRIM / SSEC / DBG | `languages_javascript_client_the-pagelove-class` / `_primitives` / `_server-sent-events` / `_debug-channels` |
| SIH / SDH / HB / DCMD / WC | `languages_javascript_client_schema-instances-in-html` / `_schema-definitions-in-html` / `_html-bindings` / `_declarative-commands` / `_web-components` |

Code (pinned in `research/COMMITS.txt`):

| Abbrev | File |
|---|---|
| BJ-MAIN | `beta-js/pagelove.mjs@c204746` |
| BJ-PRIM | `beta-js/pagelove/primitives.mjs@c204746` |
| BJ-SSE | `beta-js/pagelove/sse.mjs@c204746` |
| BJ-COMP | `beta-js/pagelove/component.mjs@c204746` |
| BJ-DBG | `beta-js/pagelove/debug.mjs@c204746` |
| BJ-SUB | `beta-js/pagelove/dom-subscriber.mjs@c204746` |
| BJ-CORS | `beta-js/cors.html@c204746` (a real JavaScript Processor action) |
| BJ-TEST | `beta-js/test/*.test.mjs@c204746` |
| PL-README | `pagelove/prompts/PAGELOVE.md@deb2857` (older server config, TransactionBudget example) |
| LIVE | private research notes of 2026-09-28, `read-probes.json` (not published) (read-only probes of docs.pagelove.com) |

No demo app (demo-apps, polls, kanban, ats, shop) uses server-side JavaScript
or imports beta-js; the only upstream server-JS artifact is BJ-CORS. Server-JS
evidence is therefore almost entirely `documented`.

### 0.2 Combined vs individual pages

`docs.pagelove.com_all_languages_javascript.md` was diffed section by section
against the 88 individual `languages_javascript*` pages: apart from the
combined page's navigation header lines, every section is identical, including
all HTML*Element pages. In `docs.pagelove.com_all_reference_composing-pages.md`
the JavaScript-Expression-Binding section is identical to the individual page;
the Method-Elements and Stamp sections only drop the one-line lead-ins before
their examples ("Say `/js-method-element-demo.html` contains", etc.), which is
cosmetic. The JEB page contains an unrendered template placeholder
(`{% example "setup-js-summary", "body" %}`) where the setup document should
be, in both copies; the case reconstructs it from the surrounding text.

### 0.3 Vocabulary

- **Binding**: a unit of server JavaScript: a `JavaScript/Module` item in a
  slot (R-JS-1), or a `j:` attribute expression (R-JS-30).
- **Slot**: where a binding item sits (`default`, `@read`, `@write`,
  `@validate`, `@computed`, `implementation`, `when`, `action`, `otherwise`, an
  HttpRequest property).
- **dombase value**: a pagelike runtime value, with the Sessel types (Null,
  Boolean, Number (integer or float), String, List, Dictionary, Element,
  Instance, Class, Selector, Temporal) plus Document and Blob.
- **Stored tree**: the DOM of a stored or composed document, as opposed to
  nodes a binding constructs.

---

# Part A: server JavaScript runtime

## A1. Binding items and language dispatch

### R-JS-1: JavaScript binding item

A JavaScript binding in a slot is a typed Microdata item:

```html
<div itemprop="<slot>" itemscope itemtype="https://pagelove.org/JavaScript/Module">
  <script itemprop="source" type="module">export default () => "x";</script>
</div>
```

- The wrapper element can be any element. The slot name is its `itemprop`.
- The **module source** is the microdata value of the item's `source` property.
  For `<script>` this is its text content, taken raw (script is a raw-text
  element, so `&&`, `<`, `=>` need no escaping and are not entity-decoded). For
  `<meta itemprop="source" content="…">` it is the `content` attribute; for
  other elements it is `textContent`. pagelike uses the first `source`
  property in document order (inferred).
- The `type` attribute of the `<script>` is irrelevant to the server. It is a
  browser affordance: because the item is a real `<script type="module">`, a
  browser viewing the stored page also evaluates the module. That is harmless
  for modules whose top level only declares an export (informative).
- A binding item with no `source`, or an empty source, is a module with no
  default export → `shape` (R-JS-6). Exception: a Method whose
  `implementation` item is entirely absent returns `null` (ME §Error cases;
  cross-area composition).
- Evidence: documented. Source: JSS §Shape; DOM, ME §JavaScript
  implementations. Confidence: high (form), medium (non-script `source`
  elements, empty source).

### R-JS-2: Language selection by itemtype

The evaluator is chosen by the binding item's `itemtype`, compared as an exact,
case-sensitive string after trimming ASCII whitespace (first token if several):

| itemtype | Language |
|---|---|
| `https://pagelove.org/JavaScript/Module` | JavaScript ES module (this spec) |
| `https://pagelove.org/Sessel`, `https://pagelove.org/Sessel/Lambda` | Sessel (modeling R-MOD-7) |
| any other itemtype on a typed item in a slot | failure, variant `unknown-language` (R-JS-50) |

`<script type="…">` never selects the language.

- Evidence: documented. Source: JSS §Shape ("The dispatcher selects the
  language by the itemtype URL"), JSS §Errors (`unknown-language`); SCH
  §Schema-level @validate. Confidence: high.

### R-JS-3: Where JavaScript is accepted

| Slot | Accepts JavaScript | Spec |
|---|---|---|
| Property `default` | yes | R-JS-11 |
| Property `@read`, `@write` | yes | R-JS-12, R-JS-13 |
| Property `@validate` | yes | R-JS-14 |
| Schema-level `@validate` | yes | R-JS-15 |
| Property `@computed` | yes | R-JS-16 |
| Method `implementation` | yes | R-JS-17 to R-JS-20 |
| Trigger `when`, `action`, `otherwise` | yes | R-JS-21 |
| Processor `when`, `action` | yes | R-JS-22 |
| HttpRequest action properties (`url`, `body`, `method`, header values) | yes; not `retry` (a plain number) | R-JS-23 |
| `j:` attributes (composition) | yes, as an expression, not a module | R-JS-30 to R-JS-36 |
| constraints (ShapeConstraint, GroupConstraint, TransitionConstraint), mutation handlers, HTTP `QUERY` bodies, `e:` attributes | **no** (Sessel-only or declarative) | R-JS-4 |

- Evidence: documented. Source: JSS intro and §When to reach for it; JSS
  §First-cut exclusions; TRG §when, §action; PRC §when, §action; HREQ §Static
  vs dynamic properties, §Retry; JEB. Confidence: high.

### R-JS-4: JavaScript where it is not accepted

- A `JavaScript/Module` item in a Sessel-only expression slot is treated like an
  unregistered language for that slot: the evaluation fails with variant
  `unknown-language`, in that slot's normal failure envelope (pagelike
  decision; the docs only say those slots are Sessel-only).
- A `QUERY` whose `Content-Type` is a JavaScript media type
  (`text/javascript`, `application/javascript`, …) is an unsupported query type:
  `415 Unsupported Media Type` with `Accept-Query` (protocol R-PROTO-43).
  **Superseded (live 2026-09-29):** such a body goes down the Sessel path: it is
  authorized as `QUERY` (401 without a grant), then answered `400` "QUERY method
  requires Content-Type: text/sessel", without `Accept-Query` (R-PROTO-43 as
  reconciled, C-9 reversed; case `javascript.query.javascript-body-415`; docs/compat/decisions-2026-09-29/javascript.md).
- Evidence: documented (Sessel-only list), inferred (failure mode). Source: JSS
  §First-cut exclusions; protocol R-PROTO-41..43. Confidence: high (QUERY 415),
  low (slot failure mode).

---

## A2. The module contract

### R-JS-5: ES module, strict mode, fresh context

- Source is parsed as an **ECMAScript module** (so it is strict mode: no
  implicit globals, `this` is not boxed, top-level `this` is `undefined`). A
  syntax error is variant `parse`.
- Each evaluation runs in a **fresh context**: a new realm with fresh globals
  and a fresh module instance. Module top-level code runs on every evaluation,
  and nothing a previous evaluation did (module-level variables, `globalThis`
  properties, prototype changes) is visible. Two defaults evaluated for two
  writes each see a freshly initialized module. Compiled bytecode MAY be cached
  by source hash (tracing has a compile span per thread), but state MUST NOT be.
- Top-level `await` in the module is permitted (ES modules allow it; pagelike
  settles the module evaluation promise before reading `default`). This is an
  inferred extension; no document shows it.
- Evidence: documented (module, strict, fresh context, compile caching).
  Source: JSS §The export default contract ("Evaluates the module in a fresh
  context"; "ES modules always run in strict mode"), JSS §Tracing
  (`dombase_js.compile` on first use per thread). Confidence: high (fresh
  context, strict), low (top-level await).
- Edge cases: `undeclared = 1` in module code throws `ReferenceError` (a
  `threw` failure). A module that mutates `Array.prototype` affects only its own
  evaluation.

### R-JS-6: The `default` export

1. Evaluate the module; read `default` from its namespace.
2. If there is no `default` export, or its value is not callable (`typeof !==
   "function"`), fail with variant `shape`. Both arrow and `function` forms are
   accepted; so are `async function` and `async` arrows.
3. Call it with the slot's `this` and positional arguments (A3).
4. If the result is a thenable, drive the job queue until it settles (R-JS-8).
5. Convert the result to a dombase value (R-JS-41). Failure is `return-type`.

- `export default 42`, `export const x = 1` (no default), and
  `export default {}` are all `shape`.
- `export default class {}` passes step 2 (a class is a function) but calling it
  without `new` throws `TypeError`, which is `threw` (inferred).
- Evidence: documented. Source: JSS §The export default contract. Confidence:
  high.

### R-JS-7: `this` binding

- `this` is passed exactly as the slot table says (A3). Because modules are
  strict, primitive `this` values are not boxed: the schema-level `@validate`
  `this` is a primitive string (`typeof this === "string"`).
- Arrow functions do not bind `this`; in an arrow, `this` is the module's
  top-level `this`, which is `undefined`. A binding that needs `this` MUST use
  the `function` form; pagelike does not rewrite arrows.
- Slots whose `this` is not documented receive `undefined`.
- Evidence: documented. Source: JSS §The export default contract, §Slot
  semantics, §Computed properties; PROP §Computed properties; SCH §JavaScript.
  Confidence: high.

### R-JS-8: Async results

- A binding function may be `async` or return a Promise in **every** slot; the
  engine drives the job (microtask) queue until the returned promise settles.
  Fulfilment value = the result. Rejection = a throw of the rejection reason
  (variant `threw`, or an HTTPResponse refusal per R-JS-53 if the reason has the
  HTTPResponse shape).
- There are no timers or I/O, so a promise that is still pending when the job
  queue is empty can never settle. pagelike fails it with variant `threw` and a
  message saying the promise did not settle (pagelike decision).
- Trigger and Processor pages describe JavaScript actions as synchronous
  functions. pagelike settles promises there too (a superset), so an `async`
  `when` gate is evaluated on its fulfilment value, not on the truthiness of a
  Promise object (pagelike decision; probe P-JS-9).
- Evidence: documented (method bodies, "the same way an async default export
  settles"; `j:` may `await`). Source: JSS §Method bodies; ME §JavaScript
  implementations ("the pipeline drives its returned promise to settlement");
  JEB §Attribute form. Confidence: high (methods, defaults), medium (other
  schema slots), low (triggers, never-settling promise).

### R-JS-9: Imports

The only import a module may contain is a **schema import**:

```js
import Note from "https://moodboard.pagelove.org/Note" with { type: "https://pagelove.org/Schema" };
```

- The specifier is the schema's governed type URL (an identifier, never
  fetched). The import attribute `type` MUST be exactly
  `https://pagelove.org/Schema`.
- The loader looks the URL up in the host's schema registry (modeling R-MOD-4,
  registered schemas of this site only). Found → the import binds a
  synthesized class (A11). Not found → variant `unknown-schema`.
- Everything else is variant `import-not-allowed`, detected before the module
  body runs (at link time), even if the imported binding is never used:
  relative (`./x.js`), bare (`lodash`), `http:`/`https:` URLs without the schema
  `type` attribute, `pagelove:host` (there is no such module), `data:`/`blob:`
  URLs, re-exports (`export … from`, `export * from`), and dynamic `import()`
  (static detection when the call is visible in the source; a dynamic call that
  runs anyway rejects with an error that is reported as `import-not-allowed`).
- Only the default binding of a schema import is defined. A named or namespace
  import of a schema module (`import { x } from … with {…}`,
  `import * as N from …`) is `import-not-allowed` (pagelike decision; the docs
  show only default imports).
- The legacy `assert { type: … }` syntax is not supported (a `parse` failure in
  engines that dropped it; pagelike decision).
- Evidence: documented. Source: JSS §Importing schemas, §Errors, §First-cut
  exclusions. Confidence: high (rules), low (named imports, `assert`).
- Contradiction C-JS-1: the docs name the allowed import three ways:
  "`pagelove:schema`" (JSS §Supported language features, §Errors),
  "`pagelove-schema:` import" (JSS §Explaining why), and the concrete URL +
  `with { type }` example. Only the last is a concrete syntax. Decision: accept
  only the URL + `type` attribute form; a literal specifier `pagelove:schema`
  is `import-not-allowed`.

---

## A3. Per-slot calling conventions

Summary (details below):

| Slot | `this` | Positional arguments | Result | Ambient `document` | `Context` global |
|---|---|---|---|---|---|
| `default` | read-only plain object of already-set declared fields | `context` = `{ document_html }` | value to inject | writable view of the in-progress element | no |
| `@read` | `undefined` | pipeline value | next pipeline value | read-only | no |
| `@write` | `undefined` | pipeline value | next pipeline value | read-only | no |
| property `@validate` | `undefined` | property value after defaults | truthy = accept | read-only | no |
| schema `@validate` | serialized instance HTML (string) | `null` | truthy = accept | read-only | no |
| `@computed` | instance view (as for `default`) | `context` (pagelike: same shape as `default`) | the property's value | read-only | no |
| method, direct call | receiver (instance, or class for static) | declared parameters, positionally | return value | only if receiver is an element (read-only) | yes (shared) |
| method, composition dispatch | host element (read-only) | declared parameters from host attributes, positionally | spliced into the page | read-only, scoped to the host subtree | yes (subtree-scoped) |
| `doesNotUnderstand` | as above | `messageName`, `parameters` | as above | as above | as above |
| trigger `when`/`action`/`otherwise` | not meaningful (`undefined`) | `ctx` = `{ request }` | `when`: truthy gate; actions: discarded | read-only | no |
| processor `when`/`action` | `undefined` | `ctx` = `{ request, response }` | as for triggers | read-only | no |
| HttpRequest property | `undefined` | `ctx` (as the enclosing trigger/processor) | the property value | no | no |
| `j:` attribute | `undefined` (pagelike) | none (expression) | bound value | read-only, host subtree (pagelike) | yes, plus `request` and earlier bindings |

### R-JS-10: Arguments are exactly the documented ones

pagelike passes exactly the positional arguments listed in the table
(`arguments.length` equals that count). Bindings MUST NOT rely on extra
arguments.
- Evidence: inferred (the docs describe each slot's arguments exhaustively).
  Confidence: medium.

### R-JS-11: `default`

- **When**: at write time, for each affected instance whose property is
  absent, before validation (modeling R-MOD-28, R-MOD-15).
- **`this`**: a plain JavaScript object whose own keys are the schema-declared
  property names that are already set on the instance (values supplied by the
  client, or set earlier in construction), with values marshalled per R-JS-40
  and the list-read rule (explicit `0..n`/`1..n` → array of values; otherwise
  the first value). Nested item properties are omitted ("the fields the
  marshaller currently understands"; pagelike decision: scalars and lists of
  scalars only). Assigning to `this` does not throw, but nothing assigned is
  kept.
- A property whose default is being computed in the same creation is absent
  from `this` (`this.other === undefined`), even if its default ran first.
  Cross-property default order is unspecified.
- **Argument**: `context`, a plain object. Documented key: `document_html`
  (string), the serialized document being written, when available (pagelike:
  always available on HTTP writes: the whole resulting document before defaults
  are injected). Bindings must not enumerate `context` or rely on the key set.
- **`document` global**: a **writable** live view of the in-progress instance
  element. pagelike: a Document whose `documentElement` is the instance element
  (so `document.querySelector` searches the instance subtree, including the
  instance). Every mutation made through it is kept: it is applied to the
  element being written, before validation.
- **Result** (modeling R-MOD-28): a scalar becomes
  `<meta itemprop="<name>" content="<String(value)>">`; an element or instance
  is injected with `itemprop="<name>"` added; `null`/`undefined` injects nothing
  (the property stays absent and cardinality applies; pagelike decision); a
  List injects each item in order (pagelike decision); a Dictionary (including
  the Date quirk of R-JS-42) fails with `return-type` (pagelike decision).
- Errors: "Write error on the affected item": 422 SchemaViolation, check
  `default`, with a nested BindingFailure (modeling R-MOD-29).
- Evidence: documented. Source: JSS §Slot semantics, §`this` and `context` for
  `default`, §The `document` global, §Marshalling; PROP §Defaults. Confidence:
  high (contract), low (document root, null/list/dictionary results).

### R-JS-12: `@read`

- **When**: after the value is fetched from storage, before the response is
  sent (modeling R-MOD-50). Chain root → leaf; Sessel and JavaScript stages mix.
- **Argument**: the pipeline value (the previous stage's output, or the stored
  value). Its JavaScript form follows modeling R-MOD-51: the property's value
  (string) for single-valued properties, an array of values for explicit
  `0..n`/`1..n`.
- **Result**: the next pipeline value, written back into the property element
  (modeling R-MOD-49).
- `document`: read-only view of the document being read.
- A property with `@computed` never runs `@read` (R-JS-16).
- Errors: the read fails, status 500 (modeling R-MOD-52).
- Evidence: documented. Source: JSS §Slot semantics, §Chaining order; RES.
  Confidence: high (contract), medium (value form).

### R-JS-13: `@write`

- **When**: before validation and before storage. Chain leaf → root.
- **Argument / result**: as `@read`.
- **Refusal**: throwing an HTTPResponse-shaped object (R-JS-53) fails the write
  with that status and message. Any other failure (a bug, a `TypeError`, a
  `return-type`) is an internal error: status 500, SchemaViolation envelope
  with BindingFailure (modeling R-MOD-52, C17). A failing transform can never
  choose the client's status.
- `document`: read-only (the doc says `@write` bindings get a read-only
  `document`).
- Evidence: documented. Source: JSS §Slot semantics, §Explaining why a value
  was rejected (postcode example), §The `document` global. Confidence: high.

### R-JS-14: Property-level `@validate`

- **When**: at persistence (PUT and every write that affects instances), after
  defaults and `@write` (modeling R-MOD-15, R-MOD-45; order vs cardinality per
  modeling C2).
- **Argument**: the property's current value after defaults (form as R-JS-12).
- **Result**: JavaScript truthiness decides. Truthy (including `"yes"`, `1`,
  `{}`) accepts; falsy (`false`, `0`, `""`, `null`, `undefined`, `NaN`) or a
  throw rejects.
- Rejection: 422 SchemaViolation, check `@validate`, naming the property
  (modeling R-MOD-45). A throw also adds a BindingFailure (variant `threw` or
  the relevant variant). A thrown HTTPResponse-shaped object uses its status
  and message instead (R-JS-53).
- Only the most-derived property-level validator runs (no chaining).
- Contradiction C-JS-2: PROP §Error cases says a `@validate` that "returns
  anything other than `true`" is a 422; JSS says truthy. Decision: JavaScript
  truthiness (the JavaScript-specific page wins for JavaScript; Sessel keeps its
  own rule). A disputed case pins the losing claim.
- Evidence: documented. Source: JSS §Slot semantics, §Chaining order; PROP
  §Validators, §Error cases. Confidence: high (truthy), medium (C-JS-2).

### R-JS-15: Schema-level `@validate`

- **`this`**: the serialized instance, the outer HTML of the instance element
  as it will be stored (after defaults and `@write`), as a primitive string.
  **Argument**: `null` (explicitly `null`, not `undefined`; one argument).
- Truthy accepts; falsy or thrown rejects: 422 SchemaViolation, check
  `@validate`, schema-level message prefix `[<itemtype>]: ` (modeling R-MOD-46,
  R-MOD-70).
- Order and inheritance: owned by modeling (C1: before group constraints; C3:
  the whole chain runs ancestor-first).
- An arrow function cannot observe `this`; `this` is `undefined` there
  (R-JS-7).
- Evidence: documented. Source: JSS §Slot semantics; SCH §JavaScript.
  Confidence: high.

### R-JS-16: `@computed`

- The property has no stored value; the binding runs on every typed read of the
  property (modeling R-MOD-44). `this` is the owning instance view (same shape
  as R-JS-11 `this`, including the property values the instance holds). The
  `function` form is required to read it.
- Positional argument: pagelike passes the `context` object (as for `default`);
  bindings should not rely on it (inferred from JSS §The export default
  contract, step 4).
- `@computed` wins over `@read` on the same property.
- Errors: the typed read fails (status 500 in composition, 500 for a QUERY per
  protocol R-PROTO-75).
- Evidence: documented. Source: JSS §Computed properties; PROP §Computed
  properties. Confidence: high (contract), low (argument).

### R-JS-17: Method bodies: direct and cross-language calls

When a method is called as `instance.method(...)` from JavaScript (A11), from
Sessel (`p.escalate(2)`, `Class.staticMethod()`), from a trigger/processor
action, from a mutation handler, or from another method:

- **`this`**: the receiver: the schema instance for an instance method, the
  class (constructor) for a `static` method.
- **Arguments**: positional, in the method's declared `parameter` order.
  Missing trailing arguments are `undefined` in a JavaScript-to-JavaScript call,
  `null` when the caller is Sessel and omits them (pagelike decision).
- **`document`**: present (read-only) only when the receiver is an element;
  when the receiver is a typed schema instance, there is no `document` global
  (`typeof document === "undefined"`).
- **`Context`**: a global object shared with the calling pass. Reading
  ambient entries works (`Context.request`); assignments (`Context.foo = …`)
  are written back to the pass's Context, so a trigger or processor that reads
  Context afterwards sees them. Reading per-user `request` members marks the
  response user-varying (R-JS-34).
- **Result**: the method's value, marshalled (R-JS-41). If the result is a
  typed schema instance that the method constructed, it is treated as
  `new Schema { … }` in Sessel: the schema's defaults fill unset properties and
  it is validated; a missing required property with no default, or a type
  error, raises an error at the call site (a `threw` failure of the method).
- A Sessel implementation called from JavaScript runs through the
  cross-language path; its span nests under the JavaScript span.
- Evidence: documented. Source: JSS §Method bodies (third context, Context
  write-back, returned instances); METH §Implementation languages, §Parameters,
  §Fields (`static`). Confidence: high (convention), medium (Context
  write-back details), low (missing-argument values).

### R-JS-18: Method bodies: composition dispatch

When a method is dispatched by a method element `<ns:name …>` or an attribute
`ns:name="…"` (composition area owns element matching, overloads and ordering):

- **`this`**: the host element (the dispatching element for the element form,
  the element carrying the attribute for the attribute form), read-only. The
  docs call it "a microdata object"; pagelike gives a read-only DOM Element
  handle (A10) for that element, so `this.getAttribute(…)` works (pagelike
  decision; probe P-JS-12).
- **Arguments, element form**: for each declared parameter, in declared order,
  the value of the host's attribute with the same name (a string), or `null`
  when the attribute is absent. Attributes that match no parameter are ignored.
  Attribute order in the markup does not matter.
- **Arguments, attribute form**: the attribute value is bound to the **first**
  declared parameter; other parameters are `null`.
- **`document`**: a read-only Document view **scoped to the host element's
  subtree**: queries only see the host element and its descendants. pagelike:
  `documentElement` is the host element; `head`/`body` are `null` unless they
  are inside the subtree. Mutating any node of the stored tree throws
  `NoModificationAllowedError` (R-JS-61). Constructing nodes is allowed
  (R-JS-62).
- **`Context`**: a global, readable and writable. `Context.request` holds the
  request data. Assignments are visible to the host element and its descendants
  only (bindings, templates, stamps, nested methods inside the subtree); later
  siblings and ancestors never see them. This scoping applies to both
  invocation forms.
- **Async**: allowed (R-JS-8).
- **Result**: converted to HTML per R-JS-60 (the composition result table).
- **Errors**: an uncaught throw (including `NoModificationAllowedError` from
  mutating the read-only `document`) fails composition with HTTP 500 and the
  error message (R-JS-52).
- Authoring note (C-JS-19): declare `parameter` items with an element that an
  HTML parser keeps nested inside the Method item (for example `<div
  itemprop="parameter" …>`). A `<li itemprop="parameter">` placed directly
  inside the Method's `<li>`, as two docs pages show, is closed off by the HTML
  parser and becomes a parameter of the Schema instead.
- Contradiction C-JS-3: JSS §Method bodies says composition dispatch binds
  arguments "by name from the dispatch element's attributes (rather than
  positionally)"; ME §JavaScript implementations says positionally in declared
  order, values taken from the matching attributes. They agree on matching by
  name; they differ on what the JavaScript function receives. Decision: ME
  (positional, declared order; the function sees ordinary positional
  parameters, which is what "by name" means for Sessel). Case
  `javascript.methods.params-positional-by-declaration`.
- Contradiction C-JS-4: ME says `Context` mutations are visible only to the
  host subtree; STAMP §How a value reaches Context says the pipeline
  "propagates a method's Context mutations to later dispatches on the same
  page". Decision: subtree-only (ME states it three times and defines the
  mechanism). Disputed case `javascript.methods.context-visible-to-later-siblings`.
- Evidence: documented. Source: ME §Attribute form, §JavaScript
  implementations, §Error cases; JSS §Method bodies. Confidence: high (args,
  document scoping, errors), medium (`this` form).

### R-JS-19: `doesNotUnderstand` in JavaScript

A JavaScript `doesNotUnderstand` implementation receives two positional
arguments: `messageName` (the unmatched local name, lowercase as parsed) and
`parameters`:

| Invocation | `parameters` |
|---|---|
| element form `<t:foo a="1" b="2">` | array of `{ name, value }` plain objects, one per attribute of the element in source order, excluding attributes whose name starts with `xmlns:` (a bare `xmlns="…"` attribute is included) |
| attribute form `<div t:foo="v">` | array with one string, the attribute value: `["v"]` |

`this`, `document`, `Context` and result handling are as R-JS-18. The
built-in `r:`, `e:` and `j:` bindings are `doesNotUnderstand` methods reached by
the attribute form (the attribute value arrives as `parameters[0]`).
- Evidence: documented. Source: ME §doesNotUnderstand fallback; METH
  §doesNotUnderstand. Confidence: high.

### R-JS-20: Method result: returned instances

A method (in any context) that returns a typed schema instance it constructed
gets schema defaults filled in and validation, exactly as a Sessel
`new Schema { … }` (R-JS-17). In composition the validated instance is spliced
as its microdata serialization (R-JS-60).
- Evidence: documented. Source: JSS §Method bodies (last paragraph).
  Confidence: high (rule), low (error status in composition: 500, R-JS-52).

### R-JS-21: Trigger `when`, `action`, `otherwise`

- Called with `this` undefined and one argument `ctx`:

  | Key | Type | Meaning |
  |---|---|---|
  | `ctx.request.method` | string | HTTP method, upper case |
  | `ctx.request.path` | string | request path, without query string |
  | `ctx.request.host` | string | host name (no port, pagelike) |
  | `ctx.request.headers` | object | request headers (R-JS-24) |
  | `ctx.request.query` | object | query parameters, name → value (first value when repeated; pagelike) |
  | `ctx.request.body` | string | request body as text, when available |
  | `ctx.request.auth` | object | authentication claims, when authenticated; for anonymous requests `{claims: {}, roles: []}` (live 2026-09-29, superseding the pagelike decision "absent"; decisions-2026-09-29/reacting.md) |

- `when`: truthy fires `action`s, falsy (`null`, `false`, `0`, `""`, and the
  other JavaScript falsy values) fires `otherwise`s (reactions area owns
  selection). A thrown error in `when` fails the request like an action error.
- `action`/`otherwise`: the return value is discarded. Only a thrown
  HTTPResponse (R-JS-53) or an error has an effect.
- JavaScript actions have **no** `Pagelove` global: `Pagelove.PUT` /
  `Pagelove.DELETE` are Sessel-only. Referencing `Pagelove` is a
  `ReferenceError`, which fails the request. Writes from JavaScript go through
  an outbound HttpRequest action instead.
- A runtime error that is not an HTTPResponse fails the request with an HTML
  Microdata error document (status 500, pagelike decision for the number).
- Evidence: documented. Source: TRG §when, §action, §Context, §Chain
  termination, §Writing from triggers, §Error handling. Confidence: high
  (shape), medium (500), low (anonymous `auth`).

### R-JS-22: Processor `when`, `action`

- As R-JS-21, plus `ctx.response`: `status` (number), `body` (string, fully
  buffered), `headers` (object).
- Assignments to `ctx.response.*` in JavaScript have **no effect** (the
  JavaScript path does not read them back). To change the response a JavaScript
  processor action throws an HTTPResponse, which replaces the response
  entirely: status, body, and any `headers` given. The replaced response does
  not inherit the original `Content-Type` (BJ-CORS sets it explicitly).
- Evidence: documented + client-source. Source: PRC §when, §action, §Context,
  §Response-mutation asymmetry, §Chain termination; BJ-CORS:48-70 (a production
  JavaScript processor that rethrows `ctx.response.body` with
  `Access-Control-Allow-Origin` and `Content-Type` headers). Confidence: high.

### R-JS-23: HttpRequest dynamic properties

Each HttpRequest property may be a `JavaScript/Module` item instead of a
literal. Its default export is called with the enclosing trigger's or
processor's `ctx` and returns the property value (a string; other values are
stringified). Evaluation happens when the action is queued (during the
trigger/processor phase), so a failure fails the request like any action
failure (pagelike decision). Mixed Sessel/JavaScript properties on one action
are allowed. `retry` cannot be an expression.
- Evidence: documented. Source: HREQ §Static vs dynamic properties, §Retry;
  TRG §Static vs dynamic properties. Confidence: high (form), low (timing).

### R-JS-24: Header objects

`ctx.request.headers`, `ctx.response.headers` and `request.headers` (R-JS-34)
are objects whose own enumerable keys are the header names **lower-cased**,
with values joined by `", "` when a header repeats. Property lookup is
**case-insensitive**, so `headers["Authorization"]` and
`headers["authorization"]` both work (the docs use the capitalized form).
Missing headers read as `undefined`.
- Evidence: documented (object type, capitalized access in examples) ·
  inferred (case-insensitive view; HTTP/2 delivers lower-case names).
  Source: TRG §Context, §Chain termination. Confidence: medium.

### R-JS-25: Chaining across inheritance

Pipeline stages chain across the schema inheritance chain: `@read` root → leaf,
`@write` leaf → root, output of one stage is the next stage's input, and
Sessel and JavaScript stages alternate freely. Property `@validate`: the
most-derived only. Owned by modeling (R-MOD-11, R-MOD-51, C3).
- Evidence: documented. Source: JSS §Chaining order; RES §Inheritance
  ordering, §Mixed-language chaining. Confidence: high.


---

## A4. `j:` JavaScript expression bindings

### R-JS-30: Namespace and recognition

- A `j:` binding is an attribute `<prefix>:<name>="<expression>"` on any
  element, where `<prefix>` is bound (by an `xmlns:<prefix>` attribute on the
  element or an ancestor) to exactly `https://pagelove.org/Binding/JavaScript`.
  The prefix is free (`j` by convention). The URI comparison is exact and
  case-sensitive.
- There is **no** element form (`<j:x>` is not a binding) and no standalone
  JavaScript expression type on schemas.
- A prefix bound to any other URI is not a JavaScript binding: no binding is
  created. What happens to the attribute then is the generic attribute-form
  rule (composition area, ME §Error cases): a prefix bound to a URI that names
  no loaded schema → attribute stripped, no error; an unbound prefix →
  attribute left as written.
- Under the hood `j:` is the built-in `Binding/JavaScript` schema's
  `doesNotUnderstand`, dispatched by the attribute form, with the expression as
  `parameters[0]` and the name as `messageName` (R-JS-19).
- Evidence: documented. Source: JEB §Namespace declaration, §Only the
  attribute form, §Error cases; ME §Attribute form. Confidence: high.

### R-JS-31: Evaluation model

- The attribute value is **one JavaScript expression**, evaluated as the body
  of one async function: pagelike compiles it as
  `async function (<scope names>) { return (<expression>); }` in strict mode.
  A statement list or anything that is not one expression is a parse failure.
- `await` is allowed anywhere in the expression. The returned value is
  settled (a returned Promise is adopted), then bound.
- `this` is `undefined` (pagelike decision; not documented).
- The same global environment as modules (A5); no imports are possible.
- Each `j:` attribute is a separate evaluation (fresh context per attribute,
  R-JS-5; pagelike decision).
- Evidence: documented (single expression, function body, `await`). Source:
  JEB §Attribute form, §What the expression can see. Confidence: high (model),
  low (strictness, `this`, fresh context per attribute).

### R-JS-32: Scope

In scope for a `j:` expression:

1. **Earlier bindings on the same element**: every `j:`, `e:` and resolved
   `r:` binding declared before it, as a bare identifier holding its value.
2. **Bindings of ancestors** (nearest ancestor wins on a name clash): a binding
   is visible to its element and all descendants, never to siblings (same rule
   as `e:`, SEB §Scope; inferred for `j:` from "peers").
3. **`request`**: the request context object (R-JS-34).
4. **`Context`**: an object holding every binding above under its name, plus
   `Context.request`.

A name is a bare identifier only if it is a valid JavaScript identifier and not
a reserved word. Otherwise (`j:class`, `j:new`, `j:first-name`) the binding is
still computed and stored, but must be read as `Context["class"]`,
`Context["first-name"]`. Referencing a name that is not in scope is a
`ReferenceError` (a failure, R-JS-36).

**Names are lower case.** Attribute names are lower-cased by the HTML parser,
so `j:totalCount="…"` binds `totalcount`; a later expression must say
`totalcount`. (Inferred from HTML parsing rules; the docs only use lower-case
names.)

- Evidence: documented (items 1, 3, 4, reserved words). Source: JEB §What the
  expression can see; SEB §Scope, §Declaration-order evaluation.
  Confidence: high (1, 3, 4), medium (2, lower-casing).

### R-JS-33: Result exposure and output

- The settled value is marshalled (R-JS-41) and stored as `Context[<name>]`
  for the rest of the element's subtree. Templates (`pagelove:template`),
  `<p:stamp name>` and later bindings read it. Numbers stay numbers for Liquid
  (`{{ total }}` renders `60`).
- A value that cannot be marshalled (a function, a symbol, a cycle) fails the
  binding with `return-type` (R-JS-36).
- Output: `j:` attributes and the `xmlns:<prefix>` declaration for the binding
  namespace are removed from the served HTML (JEB §In action shows a bare
  `<html>` and `<ul>`; LIQ: "All SSPI namespaces and binding attributes have
  been stripped from the output").
- Evidence: documented. Source: JEB §Attribute form, §In action; STAMP; LIQ
  §Examples. Confidence: high.

### R-JS-34: The `request` object and the caching taint

`request` (also `Context.request`) is a plain-looking object:

| Member | Value |
|---|---|
| `request.path` | request path without query string |
| `request.method` | upper-case method |
| `request.query` | object: parameter name → value (first value when repeated) |
| `request.headers` | header object (R-JS-24) |
| `request.auth` | for an authenticated request: `{ claims: { email, name, sub, picture, … }, username, role }` (the Request Document fields, reading-writing R-RW-136); for anonymous: `undefined` (pagelike decision, fail closed) |

**Caching taint.** Reading a per-user member (`request.auth`, `request.headers`,
or any other identity member) marks the response user-varying: it is sent with
`Cache-Control: private` and never stored in a shared cache. Reading only
`request.path`, `request.query` or `request.method` keeps the page shareable.
Merely naming `request` (for example `typeof request`) does not taint.
pagelike implements `request` with accessor properties that set the taint flag
when a per-user member is read (including `typeof request.headers`,
`"auth" in request`, and `Object.keys(request)` enumeration of per-user
members; pagelike decision for the last two).
- Evidence: documented. Source: JEB §What the expression can see, Caching
  note; reading-writing R-RW-103, R-RW-136, R-RW-137. Confidence: high
  (members, taint rule), low (anonymous `auth`, enumeration).

### R-JS-35: Ordering among bindings on one element

On one element: `r:` resource bindings resolve first; then `e:` and `j:`
bindings evaluate one after another in source order; other dispatched prefixed
attributes follow in source order; any `*:template` attribute is always
dispatched last. If a dispatched method whose declared `returns` is
`https://pagelove.org/Element` yields a non-null result, the host element is
replaced and the remaining attributes (including `*:template`) never run
(composition area owns this; ME §Attribute form).
- Evidence: documented. Source: SEB §Declaration-order evaluation, §The three
  bindings compared; JEB §Attribute form; ME §Attribute form. Confidence: high
  (template last, declaration order), medium (r: first, see C-JS-5).
- Contradiction C-JS-5: ME says multiple prefixed attributes "are not
  dispatched in the order they're written" except that `*:template` is last;
  SEB/JEB say declaration order. Decision: declaration order among `e:`/`j:`,
  `r:` first, template last (all three pages agree once "not in written order"
  is read as "template is moved last").

### R-JS-36: `j:` errors

| Condition | Result |
|---|---|
| expression fails to parse | composition fails: HTTP 500, HTML Microdata error document (R-JS-52) |
| expression throws (including `ReferenceError` for an unknown name) | same |
| returned Promise rejects | same |
| value cannot be marshalled | same, BindingFailure variant `return-type` |
| budget exhausted | 503 (R-JS-57) |
| namespace URI not exactly `https://pagelove.org/Binding/JavaScript` | not a binding (R-JS-30); no error |

The error document status is not stated by JEB ("Request fails with an HTML-
Microdata error during composition"); 500 follows ME's rule for dispatched
implementations (j: is a dispatched `doesNotUnderstand`).
- Evidence: documented (rows), inferred (500). Source: JEB §Error cases; ME
  §Error cases. Confidence: high (fails), medium (500).

---

## A5. Global environment

### R-JS-37: Language features and ECMAScript built-ins

- Syntax: at least ES2020: `const`/`let`, arrow and `function` forms,
  destructuring, spread/rest, template literals and tagged templates, default
  parameters, classes, `async`/`await`, optional chaining, nullish
  coalescing, numeric separators. pagelike's engine (QuickJS, ADR 0001) also
  supports later syntax; that is allowed.
- All standard ECMAScript global objects and functions are present: `globalThis`,
  `Object`, `Function`, `Array`, `Number`, `Boolean`, `String`, `Symbol`,
  `BigInt`, `Math`, `Date`, `JSON`, `RegExp`, `Map`, `Set`, `WeakMap`,
  `WeakSet`, `WeakRef`, `FinalizationRegistry`, `Promise`, `Proxy`,
  `Reflect`, `ArrayBuffer`, `SharedArrayBuffer`, `DataView`, the typed arrays,
  `Atomics`, the Error constructors (`Error`, `TypeError`, `RangeError`,
  `ReferenceError`, `SyntaxError`, `EvalError`, `URIError`, `AggregateError`),
  `parseInt`, `parseFloat`, `isNaN`, `isFinite`, `encodeURI(Component)`,
  `decodeURI(Component)`, `escape`, `unescape`, `eval`. QuickJS's
  `InternalError` exists (it is what stack overflow throws).
- `Math.random()` and `Date.now()` / `new Date()` are allowed and are not made
  deterministic; the server's value is the stored one.
- Not guaranteed: `Intl` (ECMA-402) and `Temporal`. pagelike does not provide
  them (probe P-JS-6).
- Evidence: documented. Source: JSS §Supported language features.
  Confidence: high (listed), medium (the rest of the ECMAScript library),
  low (`Intl`, `Temporal`).

### R-JS-38: Absent host globals

The following MUST be absent (`typeof X === "undefined"`; using them is a
`ReferenceError`), in every JavaScript context including `j:`:

| Global | Evidence |
|---|---|
| `fetch`, `process`, `require`, filesystem/environment access | documented (JSS §Supported language features) |
| `setTimeout`, `setInterval` | documented |
| `HTTPResponse` (as a constructor) | documented (JSS §Explaining why) |
| `Pagelove` (the Sessel write API) | documented for trigger/processor actions (TRG §Writing from triggers) |
| `clearTimeout`, `clearInterval`, `setImmediate`, `queueMicrotask`, `XMLHttpRequest`, `WebSocket`, `EventSource`, `module`, `exports`, `Buffer`, `Deno`, `Bun`, `window`, `self`, `navigator`, `location`, `localStorage`, `performance`, `console`, `structuredClone`, `URL`, `URLSearchParams`, `atob`, `btoa` | inferred ("no host I/O", "network and timers not available"; QuickJS defines none of these without the `std` helpers; pagelike decision for `console` and the Web utility classes) |

No host module (`qjs:std`, `qjs:os`, `pagelove:host`) can be imported
(R-JS-9).
- Evidence: as listed. Confidence: high (documented rows), low (inferred rows;
  probe P-JS-5).

### R-JS-39: Platform globals present

| Global | Where | Spec |
|---|---|---|
| `DOMException` | every context | R-JS-76 |
| `DOMParser`, `XMLSerializer` | every context | R-JS-74, R-JS-75 |
| `Node` (with the node-type constants), `Document`, `DocumentFragment`, `Element`, `HTMLElement`, every per-tag `HTML*Element` class, `HTMLMediaElement`, `CharacterData`, `Text`, `Comment`, `NodeList`, `DOMTokenList` | every context | R-JS-64 |
| `document` | contexts that run against a document (A3 table) | R-JS-63 |
| `Context` | method bodies (composition and cross-language calls), `j:` | R-JS-17, R-JS-18, R-JS-32 |
| `request` | `j:` | R-JS-34 |
| `crypto` (`crypto.subtle.digest`, `crypto.getRandomValues`, `crypto.randomUUID`) and `TextEncoder`/`TextDecoder` | every context | R-JS-39a |

**R-JS-39a (crypto).** JEB names `crypto.subtle` as an awaitable built-in a
`j:` expression may use. pagelike provides `crypto.subtle.digest(alg, data)`
(SHA-1, SHA-256, SHA-384, SHA-512; returns `Promise<ArrayBuffer>`),
`crypto.getRandomValues(typedArray)` and `crypto.randomUUID()`, plus
`TextEncoder`/`TextDecoder` (UTF-8 only), which a digest of a string needs.
Other `crypto.subtle` methods are absent.
- Contradiction C-JS-6: JSS §Explaining why says "the only global the sandbox
  defines is `DOMException`"; DOM pages document `DOMParser`, `XMLSerializer`,
  `Node` and the node classes as available, and JEB mentions `crypto.subtle`.
  Decision: provide the DOM globals (documented with examples) and the minimal
  crypto set; read the JSS sentence as "no platform class such as
  HTTPResponse". Probe P-JS-5.
- Evidence: documented (DOM globals), inferred (crypto details, encoders).
  Source: DOM §Interfaces and instanceof; DPARSE; DEX; JEB §Sessel or
  JavaScript?. Confidence: high (DOM), low (crypto shape).

---

## A6. Marshalling

### R-JS-40: dombase → JavaScript (inputs: `this`, `context`, arguments, `Context` values, bindings)

| dombase value | JavaScript value |
|---|---|
| Null | `null` |
| Boolean | boolean |
| Number (integer) | number; integers outside ±(2^53−1) fail with `marshal` (pagelike decision) |
| Number (float) | number |
| String | string |
| List | `Array` (converted recursively) |
| Dictionary | plain `Object` (string keys, insertion order) |
| Element from a stored tree (e.g. an `r:` binding's matches) | read-only DOM element handle (A10); a list of them is an `Array` (pagelike decision) |
| Element constructed in this request | writable DOM element handle |
| Instance of a registered schema | instance of the synthesized class (A11); in `this` for `default`/`@computed`, the placeholder view of R-JS-11 |
| Class | the synthesized class |
| Selector | its selector string (pagelike decision) |
| Temporal | see R-JS-42 |
| Document, Blob, or anything else without a JavaScript form | failure, variant `marshal` |

- Evidence: documented (direction and failure variant), inferred (table
  rows). Source: JSS §Marshalling, §Errors. Confidence: high (rule), low (rows
  other than scalars/lists).

### R-JS-41: JavaScript → dombase (results)

| JavaScript value | dombase value |
|---|---|
| `undefined`, `null` | Null |
| boolean | Boolean |
| number, integral and within ±(2^53−1) | Number (integer) |
| other finite number | Number (float) |
| `NaN`, `±Infinity` | failure `return-type` (pagelike decision) |
| bigint | failure `return-type` (pagelike decision) |
| string | String |
| `Array` | List (holes → Null) |
| element node | Element (serialized when spliced or stored) |
| `NodeList` | List of Elements |
| text, comment, document, fragment node | failure `return-type` (in composition the message tells the fix, R-JS-60) |
| instance of a synthesized schema class | Instance (defaults and validation per R-JS-20) |
| `Map` | Dictionary (keys `String(k)`) (pagelike decision) |
| `Set` | List (pagelike decision) |
| any other object (plain, class instance, `Date`, `Error`, `RegExp`) | Dictionary of its **own enumerable string-keyed** properties (so `new Date()` becomes an empty Dictionary, R-JS-42) |
| a thenable | settled first (R-JS-8) |
| function or symbol, anywhere in the value | failure `return-type` |
| a cycle (an object reachable from itself) | failure `return-type` |
| nesting deeper than 64 levels | failure `return-type` |

Depth counting (pagelike): the returned value is level 1; each Array element or
object property value one container deeper is the next level; a container at
level 65 or deeper fails. Shared, non-cyclic references are allowed.
- Evidence: documented (function, symbol, cycle, depth > 64, Date quirk).
  Source: JSS §Errors (`return-type`), §Marshalling. Confidence: high
  (documented rows), low (pagelike rows, exact depth origin).

### R-JS-42: Temporal values and `Date`

- A JavaScript `Date` returned from a binding does **not** become a temporal
  value. It becomes a Dictionary of the Date object's own properties (normally
  empty). Bindings that need a temporal value must return an ISO 8601 string.
- A dombase Temporal passed into JavaScript: the docs say it "currently
  unmarshalls" through the same Date-to-Map path. pagelike maps
  `Temporal.Instant`, `ZonedDateTime`, `PlainDateTime` and `PlainDate` to a
  JavaScript `Date` (UTC; a PlainDate is midnight UTC), and `PlainTime`,
  `PlainYearMonth`, `PlainMonthDay` and `Duration` to their ISO 8601 string
  (pagelike decision). Passing that Date back out yields the empty
  Dictionary, as documented.
- In practice schema values reach JavaScript as strings (property values are
  text), so this only matters for values produced by Sessel.
- Evidence: documented (Date → Map quirk, ISO advice). Source: JSS
  §Marshalling. Confidence: high (Date out), low (Temporal in).

---

## A7. Errors

### R-JS-50: The BindingFailure item

Every JavaScript binding failure is described by an item:

```html
<div itemprop="failure" itemscope itemtype="https://pagelove.org/BindingFailure">
  <meta itemprop="language" content="https://pagelove.org/JavaScript/Module">
  <meta itemprop="variant" content="threw">
  <p itemprop="message">TypeError: cannot read property 'foo' of undefined</p>
  <pre itemprop="stack">    at default (eval:2:17)</pre>
</div>
```

- `language`: the binding's language itemtype URL (Sessel failures use the
  same structure with the Sessel URL).
- `variant`: one of R-JS-51.
- `message`: for a thrown `Error`-like value `"<name>: <message>"`; for other
  thrown values `String(value)`; for non-`threw` variants a pagelike
  description. Message text is engine-specific (QuickJS wording differs from
  the V8 wording in the docs example) and is never compared by the harness.
- `stack`: present only for `threw` when the engine provided a stack, else
  absent. The module is named `eval` so frames read `at default (eval:L:C)`.
- `itemprop="failure"` is the pagelike nesting property inside the envelope
  (modeling R-MOD-70; undocumented).
- Evidence: documented. Source: JSS §Errors. Confidence: high (item and
  properties), low (nesting property).

### R-JS-51: Variants

| Variant | Trigger | Detected |
|---|---|---|
| `parse` | the source is not a valid ES module | before evaluation |
| `shape` | no `default` export, or it is not a function | after module evaluation |
| `threw` | the module top level or the default function threw, or its promise rejected, or stack overflow (`InternalError: stack overflow`) | during evaluation |
| `timeout` | the time budget ran out | during evaluation |
| `out-of-memory` | the memory budget ran out | during evaluation |
| `marshal` | an input value (a field of `this`, a key of `context`, an argument) cannot be converted | before the call |
| `return-type` | the result cannot be converted (R-JS-41) | after the call |
| `import-not-allowed` | any import other than a schema import (R-JS-9) | at link time |
| `unknown-schema` | a schema import names a schema not in the host registry | at link time |
| `unknown-language` | the slot's typed item has an unregistered itemtype (R-JS-2) | before parsing |

Classification rules for pagelike (ADR 0001): `timeout` is decided by the
host-side deadline flag, not by message text; a thrown `null` from an
evaluation that hit its memory limit is `out-of-memory`.
- Evidence: documented. Source: JSS §Errors, §Resource limits. Confidence:
  high.

### R-JS-52: Envelope and status by context

| Context | Ordinary failure | Status | Body |
|---|---|---|---|
| `default` | "write error on the affected item" | 422 (modeling R-MOD-29) | SchemaViolation, check `default`, + BindingFailure |
| `@write` | internal error | 500 (modeling R-MOD-52) | SchemaViolation + BindingFailure |
| property `@validate` | standard rejection | 422 | SchemaViolation, check `@validate`, + BindingFailure when it threw or failed to evaluate |
| schema `@validate` | standard rejection | 422 | same, schema-level |
| `@validate` that fails to **parse** | every write to the governed type is rejected | 422 (modeling R-MOD-48) | same |
| `@read` / `@write` that fails to **parse** | the stage is skipped, the chain continues | none | (modeling R-MOD-48) |
| `@read`, `@computed` (runtime) | the read fails | 500 | Error document + BindingFailure |
| method (composition), `j:` | composition fails | 500 | `https://pagelove.org/Error` document: `status` 500, `message` (the error message), `failure` BindingFailure |
| method (cross-language call) | propagates to the caller as a thrown error | the caller's rule | the caller's envelope |
| trigger / processor `when`/`action`/`otherwise`, HttpRequest property | "the request fails with an HTML Microdata error body" | 500 (pagelike) | Error document + BindingFailure |
| any context, budget exhausted | R-JS-57 | 503 (public plane), 507 (WebDAV plane) | the context's envelope, variant `timeout`/`out-of-memory` |

All error bodies are `Content-Type: text/html; charset=utf-8` (reading-writing
R-RW-130). The WebDAV plane wraps them in its Error article with `detail`
(modeling R-MOD-75).
- Contradiction C-JS-7: JSS §Errors says "every failure" is a BindingFailure
  "nested inside the SchemaViolation envelope"; ME, JEB and TRG describe
  composition and trigger failures as plain HTTP 500 / HTML Microdata errors.
  Decision: SchemaViolation only where a schema write is being validated (the
  JSS page is about schema slots); the Error document elsewhere, still
  carrying the BindingFailure item.
- Contradiction C-JS-8: RES §Error cases says a compile failure skips the
  resolver stage; JSS lists `parse` as a rendered failure. Decision: modeling's
  (skip for `@read`/`@write`, 422 for `@validate`); `parse` is reported where the
  slot is not skipped (defaults, methods, `j:`, triggers).
- Evidence: documented (rows marked in modeling), inferred (status numbers
  where marked pagelike). Confidence: medium.

### R-JS-53: Thrown HTTPResponse objects

A thrown value is an **HTTPResponse** when it is a non-null object (not a
primitive) with an own property `schema_url` or `itemtype` whose value is
exactly `https://pagelove.org/HTTPResponse`. Fields:

| Field | Meaning | Default |
|---|---|---|
| `status` | response status (integer 100–599) | 500 |
| `message` | response body when `body` is absent | `""` |
| `body` | response body (a string, sent as given) | `message` |
| `headers` | object of extra response headers (name → string value) | none |

- Recognized in: `@write`, property `@validate` (JSS), and trigger/processor
  `action`/`otherwise` (TRG, PRC). The thrown response replaces the whole
  response: status, body, and headers; no `Content-Type` is inherited
  (pagelike: `text/html; charset=utf-8` unless `headers` sets one). Headers with
  invalid names or values containing control characters are dropped (as HREQ
  does for outbound headers; pagelike decision).
- In `@validate`/`@write` the write still fails and nothing is stored.
- `new HTTPResponse(…)` is a `ReferenceError` (no such global), which is an
  ordinary failure: the client gets the standard rejection, not the intended
  status.
- A thrown object without the marker (`{ status: 409 }`), an `Error`, or a
  primitive is an ordinary failure (standard rejection or 500), so a bug can
  never choose the response.
- A thrown HTTPResponse in any other context (`default`, `@read`,
  `@computed`, schema-level `@validate`, method bodies, `j:`, trigger `when`) is
  treated as an ordinary failure (pagelike decision; probe P-JS-10). For
  schema-level `@validate` this is a known gap: the JSS table only mentions the
  property-level slot.
- A status outside 100–599 or a non-integer status → ordinary failure
  (pagelike decision).
- Evidence: documented. Source: JSS §Explaining why a value was rejected;
  TRG §Chain termination (fields, default 500); PRC §action; BJ-CORS:48-70.
  Confidence: high (shape, fields, contexts listed), low (other contexts).

### R-JS-54: Tracing (informative for HTTP behavior)

Every evaluation SHOULD produce an OpenTelemetry span `dombase_js.evaluate`
with attributes `schema.itemtype` (or `"(none)"`), `binding.kind` (`default`,
`read`, `write`, `validate`, `computed`, `schema-@validate`, `method`; other
kinds for triggers, processors and WebDAV auth), `binding.source_hash` (short
hex), `binding.source_len` (bytes), `outcome` (`ok` or the variant),
`budget.memory_delta_bytes`, `budget.time_nanos`. A child span
`dombase_js.compile` wraps the first compile of a source on a worker.
Cross-language Sessel calls nest under the JavaScript span. Not observable over
HTTP; no cases.
- Evidence: documented. Source: JSS §Tracing. Confidence: high.

---

## A8. Budgets and limits

### R-JS-55: One shared request budget

Every JavaScript evaluation runs under the request's single transaction budget,
shared with Sessel, Liquid and composition: a request that runs one Sessel and
one JavaScript binding draws both from one allowance. The budget comes from the
host's `https://pagelove.org/TransactionBudget` Microdata (server-wide default,
per-host override): `operations` (count), `memory` (bytes), `duration`
(milliseconds). The older server README shows 1,000,000 operations,
10,485,760 bytes and 900 ms, with a per-host duration override of 1,500 ms;
pagelike uses these as its defaults, configurable per site (pagelike decision
anchored on PL-README).
- Evidence: documented (shared budget, TransactionBudget), client-source (the
  example numbers). Source: JSS §Resource limits; PL-README §Configuration;
  LIQ §Limits. Confidence: high (sharing), low (numbers).

### R-JS-56: Per-evaluation limits

| Limit | Production source | Default when unbudgeted | Exhaustion |
|---|---|---|---|
| Memory | remaining memory budget of the request | 16 MB per context | `out-of-memory` |
| Stack | per-thread default | 256 KB per context | `threw` (`InternalError: stack overflow`) |
| Time | remaining time budget of the request | per-thread default (pagelike: 1 s) | `timeout` |
| Ops | charged in batches at every periodic interrupt | not charged for short straight-line code | surfaces through the request budget (pagelike: charged as elapsed time slices; ADR 0001 has no op-count hook) |

"Unbudgeted contexts" are evaluations outside an HTTP request (for example
background work). pagelike maps 256 KB of stack to an engine frame limit
calibrated against PageLove's observed depth (ADR 0001 Risk 3; probe P-JS-7):
a recursion depth of at least 500 frames MUST succeed.
- Evidence: documented. Source: JSS §Resource limits. Confidence: high
  (table), low (time default, depth calibration).

### R-JS-57: Exhaustion status

When a JavaScript evaluation exhausts time, memory or operations, the request
fails with **503 Service Unavailable** on the public plane (the status "a
budget failure produces elsewhere", AuthorizationRule page; composition budget
503 in ME) and **507** on the WebDAV plane ("the request exhausted its
allowance", WebDAV page). The body is the context's envelope (R-JS-52) with a
BindingFailure of variant `timeout` or `out-of-memory`. Nothing is stored.
Stack overflow is not a budget failure; it is `threw` with the context's
ordinary status.
- Contradiction C-JS-9: JSS says every failure is rendered inside the
  SchemaViolation envelope (implying 422 for validators); the budget pages say
  503. Decision: 503/507 with the envelope, because a budget failure is not a
  statement about the data. Cases accept `[422, 500, 503]` and assert the
  variant only.
- Evidence: documented (503 for budget failures elsewhere, 507 on WebDAV),
  inferred (JavaScript uses the same). Source: AuthorizationRule §Fields (rule lookups "cannot cause the 503 ... a budget failure
  produces elsewhere"); ME §Error cases; WebDAV §When something goes wrong.
  Confidence: low.

### R-JS-58: Composition dispatch budget

Composition allows 500 method dispatches per request. Each method element and
each dispatched prefixed attribute (including every `j:`, `e:`, `r:`) costs one
unit; results that recurse (spliced fragments containing method elements) cost
more because their dispatches are counted too. Exceeding it: **503** with an
error document whose message is `composition budget exceeded`. DOM operations
performed by JavaScript bindings are also charged to the composition budget
(DOM §Divergences, last item; pagelike: charged against the time/ops axis).
- Evidence: documented. Source: ME §Error cases; DOM §Divergences.
  Confidence: high (500, 503), low (DOM op charging).

### R-JS-59: Budget consumption headers

PageLove reports consumption on responses with `X-Budget-Consumed-Ops`,
`X-Budget-Consumed-Memory` and `X-Budget-Consumed-Time` (integers; observed
values 2 / 215 / 93 for a small GET). pagelike SHOULD send the same three
headers on public-plane responses that ran composition or bindings, with ops
count, bytes, and elapsed time (pagelike: microseconds; the live unit is
unknown). The harness normalizes `x-budget-*` and never compares values.
- Evidence: live-observed. Source: LIVE cases `fragment`, `missing-fragment`,
  `query-css`. Confidence: medium (presence), low (units).


---

## A9. Returning values into a composed page

### R-JS-60: Composition result table

When a JavaScript method is dispatched by composition (element form, or
attribute form with `returns` = `https://pagelove.org/Element` and a non-null
result), the settled return value becomes HTML:

| Returned value | Result |
|---|---|
| an element node (constructed, parsed, cloned, or a read-only stored element) | serialized as HTML, parsed as a fragment in the host's context, spliced in place of the host element; composition recurses into it |
| a `NodeList`, or an `Array` of element nodes | each element serialized and spliced, in order |
| an instance of a synthesized schema class | its microdata serialization, spliced; composition recurses |
| a string | inserted as a text node (HTML-escaped on output: `<b>` is served as `&lt;b&gt;`) |
| a number | its `String()` form as text (`42`, `2.5`) |
| a boolean | `true` / `false` as text |
| `null` or `undefined` | the host element is removed (element form); the attribute is removed and the host kept (attribute form) |
| a text, comment, document or fragment node | **error**: composition fails with HTTP 500; the message names the fix (return `document.documentElement` instead of the document; return `.textContent` instead of a text node) |
| an `Array` mixing elements and scalars | pagelike: scalars become text nodes, elements are spliced, `null`s skipped, in order (pagelike decision) |
| anything that fails R-JS-41 | HTTP 500, variant `return-type` |

Attribute form with a `returns` other than Element, or a null result: the
dispatching attribute is removed and the host element is otherwise unchanged
(its Context changes still apply to its subtree).
- Evidence: documented. Source: DOM §Returning DOM from a binding; ME §How the
  result becomes HTML, §JavaScript implementations, §Attribute form.
  Confidence: high (documented rows), low (mixed arrays).

---

## A10. The server DOM API (implementation checklist)

The DOM is a subset chosen for isomorphic bindings. **Anything not listed in
this section does not exist server-side**: reading it yields `undefined`, and
calling it is a `TypeError` (not a function).

### R-JS-61: The read-only boundary

- Whether the ambient `document` is writable depends on the slot:

  | Context | Ambient `document` |
  |---|---|
  | `default` | writable (mutations are kept, R-JS-11) |
  | `@read`, `@write`, `@validate` (both levels), `@computed`, method bodies (all contexts), trigger/processor `when`/`action`, `j:` (pagelike) | read-only tree, writable construction |
  | a document created by `new DOMParser().parseFromString(…)` | writable |

- **Read-only is a property of the nodes that were in the tree when the binding
  started**, however they are reached (`document.body`, `querySelector`,
  `parentNode`, a `NodeList`, `children`, `classList`, `this` for a composition
  dispatch). Every mutating operation on such a node throws a `DOMException`
  named `NoModificationAllowedError` (code 7) with message exactly
  `document is read-only in this binding context`:
  `setAttribute`/`removeAttribute`/`*NS` variants, reflected property setters,
  `textContent`/`data`/`innerHTML`/`outerHTML` setters, `insertAdjacentHTML`,
  `classList` mutators and `value` setter, `appendChild`/`insertBefore`/
  `removeChild`/`replaceChild` on a read-only parent, `append`/`prepend`/
  `before`/`after`/`replaceWith`/`remove` affecting a read-only node.
- **Moving** a read-only node into a constructed subtree
  (`fresh.appendChild(document.querySelector("li"))`) throws
  `NoModificationAllowedError`, because it would detach the node from the
  read-only tree. `cloneNode(true)` yields a writable copy that can be attached.
- A failed guarded operation has no partial effect.
- Evidence: documented. Source: DOM §Availability and the read-only boundary;
  DEX §toString example; ME §JavaScript implementations. Confidence: high.

### R-JS-62: Constructing nodes against a read-only document

`createElement`, `createElementNS`, `createTextNode`, `createComment`,
`createDocumentFragment` and `cloneNode` on the read-only ambient `document`
**succeed** and return fresh, writable, detached nodes. A binding can build a
subtree from them and return it.
- Contradiction C-JS-10: DOM §Availability says construction is always
  allowed (with the `document.createElement("li")` and `cloneNode(true)`
  examples); DDOC says the factories "are mutation-guarded and throw" on a
  read-only document; DNODE says `cloneNode` is "rejected on a read-only
  document"; ME suggests building return fragments in a fresh DOMParser
  document. Decision: construction allowed (the overview is the normative
  page for the boundary, gives concrete examples, and allowing is a superset
  for code written against either reading). Disputed case
  `javascript.dom.readonly-factories-throw`.
- Evidence: documented (both claims). Confidence: medium.

### R-JS-63: Where `document` exists

The ambient `document` exists whenever the binding runs against a document
(A3 table). It does **not** exist in a method body whose receiver is a typed
schema instance (R-JS-17), nor in HttpRequest property modules. In a
composition dispatch it is scoped to the host subtree (R-JS-18); in `default`
it views the in-progress instance (R-JS-11); in pipeline slots and validators
it is the document being read or written; in triggers/processors it is the
target document when one exists (pagelike decision).
- Evidence: documented (availability, instance receivers). Confidence: medium.

### R-JS-64: Interfaces, `instanceof`, and interface scoping

- Real classes (usable with `instanceof`; `constructor.name` is the interface
  name) in the WHATWG hierarchy:
  `Node` → `Document`, `DocumentFragment`, `CharacterData` (→ `Text`,
  `Comment`), `Element` → `HTMLElement` → per-tag interfaces (R-JS-79), with
  `HTMLMediaElement` → `HTMLVideoElement`, `HTMLAudioElement`. Also `NodeList`
  and `DOMTokenList`. (A doctype node exists in parsed documents with `nodeType`
  10; no `DocumentType` class is documented.)
- An element's class: an element with **no namespace** (HTML-parsed, or made by
  `createElement`) is its per-tag interface when one exists (`<a>` →
  `HTMLAnchorElement`), otherwise plain `HTMLElement` (`<section>`,
  `<article>`, unknown tags such as `<foo>`, hyphenated custom tags, and
  prefixed tags such as `<t:hello>`; there is no `HTMLUnknownElement`). An
  element made by `createElementNS` is plain `Element`, whatever the namespace
  (including the XHTML namespace; pagelike decision).
- HTML-parsed foreign content (`<svg>`, `<math>` and their children) is stored
  without a namespace, so it is `HTMLElement` with an upper-cased `tagName`
  (`svg > g` has `tagName` `"G"`) (inferred from DELEM §Identity example).
- **Interface scoping**: a member that is not part of a node's interface
  chain reads as `undefined` and never throws (`textNode.tagName`,
  `element.data`, `element.length`, `img.href`, `section.href`,
  `audio.poster`). Within its interface, an accessor with no value returns
  `null` (`parentNode` of a detached node).
- Node identity: one node is one object however it is reached
  (`d.querySelector("#b") === b`, `el.closest("x") === el`, `el.classList ===
  el.classList`). Live 2026-09-29: PageLove returns a new object for every node
  access, so such comparisons are false there; pagelike keeps node identity
  (keep-standard; `javascript.dom.node-identity` and its `.live` sibling; docs/compat/decisions-2026-09-29/javascript.md).
- One flattened node type backs all nodes internally; the classes are a
  prototype layer (implementation note).
- Contradiction C-JS-11: DOM §Interfaces says HTML elements are
  `HTMLElement` "and `htmlEl.constructor.name` is `"HTMLElement"`", then says
  every tag with a dedicated interface is that interface (`a.constructor.name
  === "HTMLAnchorElement"`). Decision: the per-tag rule; the first sentence
  describes tags without a dedicated interface.
- Evidence: documented. Source: DOM §Interfaces and instanceof; DELEM
  §Reflected IDL attributes. Confidence: high (hierarchy, scoping), medium
  (foreign content, `createElementNS` in XHTML namespace).

### R-JS-65: `Document` members

| Member | Behavior |
|---|---|
| `querySelector(sel)` | first match in document order, or `null`; invalid selector → `SyntaxError` |
| `querySelectorAll(sel)` | static `NodeList` of all matches |
| `getElementById(id)` | first element whose `id` attribute equals `id` literally (no CSS parsing: `.`, `:`, `[` match literally), or `null` |
| `getElementsByTagNameNS(ns, local)` | static `NodeList` of descendants; `"*"` matches any namespace / any local name |
| `createElement(tag)` | detached element, no namespace; the tag is lower-cased (pagelike, as browsers do for HTML documents); `tagName` reports it upper-cased |
| `createElementNS(ns, qname)` | detached element, `qname` stored verbatim (case preserved), `ns` recorded as `namespaceURI`; `NamespaceError` for an empty name, more than one `:`, a leading or trailing `:`, or a prefix with an empty/null namespace; the browser's reserved `xml`/`xmlns` prefix checks are **not** applied; no `xmlns:` attribute is emitted |
| `createTextNode(data)` | detached text node |
| `createComment(data)` | detached comment node |
| `createDocumentFragment()` | detached, empty fragment |
| `documentElement` | the root element or `null` |
| `head`, `body` | the `<head>` / `<body>` element or `null` |

Plus every Node member (R-JS-66). `document.nodeType` is 9, `nodeName`
`"#document"`, `ownerDocument` `null`.
- Evidence: documented. Source: DDOC. Confidence: high.

### R-JS-66: `Node` members (every node)

| Member | Behavior |
|---|---|
| `parentNode` | parent or `null` |
| `parentElement` | parent if it is an element, else `null` |
| `firstChild`, `lastChild`, `previousSibling`, `nextSibling` | any node kind, or `null` |
| `childNodes` | static `NodeList` of all children |
| `ownerDocument` | owning document; `null` on a document |
| `hasChildNodes()` | boolean |
| `nodeType` | 1 element, 3 text, 8 comment, 9 document, 10 doctype, 11 fragment |
| `nodeName` | element: upper-cased tag (verbatim for `createElementNS`); `#text`, `#comment`, `#document`, `#document-fragment`; the doctype name; a processing-instruction target |
| `textContent` (get) | concatenation of all descendant text; on a comment, its data; on a text node, its data |
| `textContent` (set) | replaces all children with one text node; `""` leaves no children; guarded |
| `appendChild(c)` | appends and returns `c`; `c` being the node itself or an ancestor → `HierarchyRequestError` |
| `insertBefore(n, ref)` | inserts before `ref` and returns `n`; `ref` `null`/omitted → append; `ref` not a child → `NotFoundError` |
| `removeChild(c)` | detaches and returns `c` (it stays usable, `parentNode === null`); not a child → `NotFoundError` |
| `replaceChild(new, old)` | returns `old`; `old` not a child → `NotFoundError` |
| `cloneNode(deep = false)` | detached clone; shallow by default |
| `contains(other)` | inclusive descendant test; `false` across documents |

A document accepts any children: several elements and text (live 2026-09-29:
PageLove raises no `HierarchyRequestError` there, and a `DOMParser` document
usually holds several top-level nodes, R-JS-74). Only a doctype is restricted
(to documents).

Insertion rules shared by all mutators: a `DocumentFragment` argument moves its
children (the fragment is left empty); a node that belongs to **another
document** is adopted by **deep copy** (the original stays where it was); a
node already in this document is moved.

The `Node` global carries the constants `ELEMENT_NODE` 1, `TEXT_NODE` 3,
`COMMENT_NODE` 8, `DOCUMENT_NODE` 9, `DOCUMENT_TYPE_NODE` 10,
`DOCUMENT_FRAGMENT_NODE` 11 (only these).
- Evidence: documented. Source: DNODE. Confidence: high (members), medium
  (adoption by copy, which diverges from browsers).

### R-JS-67: `CharacterData` (`Text`, `Comment`)

- `data` (get/set; set is guarded) and `length` (get). `length` counts
  **Unicode characters (code points)**, not UTF-16 code units (so a text node
  holding `"a\u{1F600}"` has `length` 2, while the JavaScript string has
  length 3).
- On any other interface `data` and `length` are `undefined`.
- Evidence: documented. Source: DNODE §data / length. Confidence: high (scope),
  medium (code-point length).

### R-JS-68: `Element` identity and attributes

| Member | Behavior |
|---|---|
| `tagName` | upper-cased for no-namespace elements, verbatim for `createElementNS` |
| `localName` | the part after the first `:` of the stored name (the whole name if none) |
| `prefix` | the part before the first `:`, or `null` |
| `namespaceURI` | the stored namespace (`createElementNS`), else for a prefixed name the value of the nearest inclusive-ancestor `xmlns:<prefix>` attribute, else `null` (HTML-parsed `<div>` → `null`, a divergence) |
| `id` | the `id` attribute or `""` (read-only accessor: assignment is ignored in sloppy code and a `TypeError` in module code, which is strict; pagelike follows the docs' "read-only accessors") |
| `className` | the whole `class` attribute or `""` (read-only accessor, same rule) |
| `getAttribute(n)` | value or `null` (names match ASCII case-insensitively on no-namespace elements) |
| `hasAttribute(n)` | boolean |
| `setAttribute(n, v)` | sets or replaces (value `String(v)`); guarded |
| `removeAttribute(n)` | no-op when absent; guarded |
| `getAttributeNS(ns, local)` | find an in-scope `xmlns:<p>` attribute (on the element or an ancestor) whose value is `ns`, then read `<p>:<local>`; `null` if no prefix maps `ns` |
| `setAttributeNS(ns, qname, v)` | stores `qname` verbatim; `ns` is only used to validate `qname` (`NamespaceError` as `createElementNS`); no per-attribute namespace map |
| `removeAttributeNS(ns, local)` | resolve `ns` to an in-scope prefix, remove `<p>:<local>` |

- Contradiction C-JS-12: browsers let scripts assign `el.id` and
  `el.className`; DELEM lists them under "Read-only accessors". Decision: read
  only (documented); in strict module code the assignment throws `TypeError`.
  Probe P-JS-13.
- Evidence: documented. Source: DELEM §Identity, §Attributes; DOM
  §Divergences. Confidence: high (members), medium (read-only id/className).

### R-JS-69: `Element` content

| Member | Behavior |
|---|---|
| `innerHTML` (get) | HTML serialization of the children (same serializer as responses, ADR 0003) |
| `innerHTML` (set) | parses **only the assigned string** as a fragment in the element's context and replaces the children; the rest of the document is never re-serialized or re-parsed; guarded |
| `outerHTML` (get) | the element and its children serialized |
| `outerHTML` (set) | parses the string and replaces the element within its parent; `NoModificationAllowedError` if the element has no parent; guarded |
| `insertAdjacentHTML(pos, html)` | `pos` matched ASCII case-insensitively: `beforebegin`, `afterbegin`, `beforeend`, `afterend`; any other value → `SyntaxError`; `beforebegin`/`afterend` on a parentless element are silent no-ops; guarded |

- Live 2026-09-29: an element whose parent is a document (a top-level node of a
  `DOMParser` document) accepts the `outerHTML` setter and `beforebegin`/
  `afterend`; the markup is parsed in a `body` context. Only an element with no
  parent raises `NoModificationAllowedError` (outerHTML) or does nothing
  (insertAdjacentHTML).
- Evidence: documented. Source: DELEM §Content. Confidence: high.

### R-JS-70: `classList` (`DOMTokenList`)

A live view over the `class` attribute, which stays the single source of truth.
Tokens are split on ASCII whitespace and de-duplicated, order preserved. Every
mutator rewrites the attribute as the tokens joined by one space, and **removes
the attribute when the list becomes empty** (a divergence: browsers keep
`class=""`). Iterable (`[...el.classList]`, `Array.from`).

| Member | Behavior |
|---|---|
| `add(...t)` | append missing tokens |
| `remove(...t)` | remove every listed token |
| `toggle(t, force?)` → boolean | add/remove; `force` true only adds, false only removes; returns whether `t` is now present |
| `replace(old, new)` → boolean | replace `old` in place (set semantics: if `new` is already present, `old` is just removed); returns whether `old` was present |
| `contains(t)` → boolean | membership |
| `item(i)` → string or `null` | token at `i`; negative or out of range → `null` |
| `length` | token count |
| `value` (get/set) | the whole class string |

Mutators on a read-only element throw `NoModificationAllowedError`. Empty-string
or whitespace tokens: pagelike throws `SyntaxError` / `InvalidCharacterError`
like browsers (inferred).
- **Superseded in part (live 2026-09-29, adopt-live; docs/compat/decisions-2026-09-29/javascript.md):** tokens are
  **not** de-duplicated: `length`, `item()` and iteration count every copy
  (`"a b a"` has length 3), `add` appends only missing tokens and keeps the
  copies, `remove` and `toggle` drop every copy, and `replace(old, new)` puts
  `new` at the first copy of `old` or `new` and drops the other copies of `new`
  (other copies of `old` stay: `"a b a"` → `"b a"`). `item()` out of range is
  `undefined`, and an index that is not a number is a `TypeError`. Tokens are
  not validated (`add("")` and `add("a b")` throw nothing). Emptying the list
  still removes the attribute. Kept (keep-standard): `String(classList)` is the
  class string (PageLove: `"[object Object]"`).
- Evidence: documented. Source: DELEM §classList. Confidence: high (members),
  medium (attribute removal on empty).

### R-JS-71: Element traversal, `closest`, `matches`, convenience insertion

| Member | Behavior |
|---|---|
| `children` | static `NodeList` of child elements (not a live HTMLCollection) |
| `firstElementChild`, `lastElementChild` | element or `null` |
| `childElementCount` | number |
| `nextElementSibling`, `previousElementSibling` | element or `null` |
| `querySelector(sel)`, `querySelectorAll(sel)` on an element | **scan the whole document** the element belongs to, not the element's subtree (a divergence); on a detached element or constructed subtree: the subtree rooted at its topmost ancestor (pagelike decision) |
| `getElementsByTagNameNS(ns, local)` on an element | the element's descendants only (receiver excluded) |
| `getElementById(id)` on an element | the element's descendants (receiver excluded; pagelike decision) with the literal match of R-JS-65 |
| `closest(sel)` | nearest inclusive ancestor matching, or `null`; `SyntaxError` on an invalid selector |
| `matches(sel)` | boolean; `SyntaxError` on an invalid selector |
| `append(...n)`, `prepend(...n)` | insert at the end / start of the children; string arguments become text nodes |
| `before(...n)`, `after(...n)` | insert as siblings; no-op on a parentless node |
| `replaceWith(...n)` | replace this element (it stays usable, detached); no-op on a parentless node |
| `remove()` | detach from the parent; no-op when parentless |

All mutators are guarded (R-JS-61).
- Evidence: documented. Source: DELEM §Element traversal, §closest,
  §matches, §Insertion convenience; DDOC §querySelector, §getElementById; DOM
  §Divergences. Confidence: high (members), low (detached-scope and
  `getElementById` receiver rules).

### R-JS-72: `NodeList`

- One static type for `childNodes`, `children`, `querySelectorAll` and
  `getElementsByTagNameNS`. It is a **snapshot**: later tree changes are not
  reflected, and removing nodes while iterating does not shorten it.
- Members: `length`; `item(i)` (negative or out of range → `null`). Iterable
  (`for…of`, spread, `Array.from`).
- Not documented: index access (`list[0]`), `forEach`, `entries`, `keys`,
  `values`. pagelike provides numeric index access and `forEach` (browser
  compatibility for isomorphic code; pagelike decision; probe P-JS-14).
- Evidence: documented. Source: DNL. Confidence: high (members), low
  (index access).

### R-JS-73: Selectors in the DOM API

- Every selector argument is parsed with pagelike's selector engine
  (`internal/selector`, ADR 0003), so the full CSS dialect of the rest of the
  platform (including PageLove extensions) applies.
- **Superseded (live 2026-09-29, adopt-live; docs/compat/decisions-2026-09-29/javascript.md):** `*|tag` is `tag` in any
  namespace (`*|*` is `*`, `[*|a]` is `[a]`); `|tag` is `tag` without a
  namespace (every HTML-parsed element, not one made by `createElementNS` with a
  namespace); a named prefix (`svg|g`) is still a `SyntaxError`. The original
  reading follows.
- Namespace-pipe selectors are unsupported: `p|tag` (a named namespace
  prefix) throws `SyntaxError`; `*|tag` parses but matches nothing.
- An invalid selector throws a `DOMException` named `SyntaxError` (code 12).
- Evidence: documented. Source: DOM §Divergences, §Errors. Confidence: high.

### R-JS-74: `DOMParser`

`new DOMParser().parseFromString(html, type)` returns a new **writable**
`Document`. The `type` argument is accepted and ignored: the input is always
parsed as a full HTML document (so `html`, `head` and `body` exist even for
`"application/xml"` or `"image/svg+xml"`). Everything reached from the parsed
document is writable, even in a read-only binding. Parse errors never throw.
- **Superseded in part (live 2026-09-29, adopt-live; docs/compat/decisions-2026-09-29/javascript.md):** the type is
  still ignored, but the document holds only what the markup wrote: the `html`,
  `head` and `body` elements the tree builder implies are left out
  (`parseFromString("<ul>…</ul><p>x</p>")` has the `ul` and the `p` as its
  children; `head`/`body` are `null`, `documentElement` is the first element).
  Explicit `html`/`head`/`body` tags are kept. PageLove's own parser also
  honours `<x/>` on every element, lets a `p` contain a `p` and inserts no
  `tbody`; pagelike keeps the WHATWG tree builder (keep-standard,
  `javascript.dom.probe-0929.kept-divergences`).
- Evidence: documented. Source: DPARSE §DOMParser. Confidence: high.

### R-JS-75: `XMLSerializer`

`new XMLSerializer().serializeToString(node)` returns a string:
- a `Document` serializes whole (doctype included when present);
- a `DocumentFragment` serializes as its children, concatenated;
- any other node serializes as its outer HTML (text is escaped, a comment is
  `<!--…-->`).

This is **HTML** serialization, not XML: `<br>` stays `<br>` (browsers
produce XHTML such as `<br />`) (inferred from "as its outer HTML").
- Evidence: documented. Source: DPARSE §XMLSerializer. Confidence: high
  (rules), medium (HTML void syntax).

### R-JS-76: `DOMException`

- `new DOMException(message = "", name = "Error")`. Members: `name`,
  `message`, `code`: the legacy code for a recognized name, else 0.
- Legacy codes: IndexSizeError 1, DOMStringSizeError 2, HierarchyRequestError 3,
  WrongDocumentError 4, InvalidCharacterError 5, NoDataAllowedError 6,
  NoModificationAllowedError 7, NotFoundError 8, NotSupportedError 9,
  InUseAttributeError 10, InvalidStateError 11, SyntaxError 12,
  InvalidModificationError 13, NamespaceError 14, InvalidAccessError 15,
  ValidationError 16, TypeMismatchError 17, SecurityError 18, NetworkError 19,
  AbortError 20, URLMismatchError 21, QuotaExceededError 22, TimeoutError 23,
  InvalidNodeTypeError 24, DataCloneError 25. Every other name (including
  `"Error"`) → 0.
- `toString()` returns `"DOMException: <name>"` when `message` is empty, else
  `"DOMException: <name>: <message>"` (a divergence from browsers, whose
  `toString` gives `"<name>: <message>"`).
- Every error thrown by the DOM API is a `DOMException` instance.
  `DOMException.prototype` inherits from `Error.prototype` (pagelike, as in
  browsers), so `e instanceof Error` is true.
- Evidence: documented. Source: DEX. Confidence: high (members, table,
  toString), medium (Error inheritance).

### R-JS-77: Error names the DOM raises

| Name | Raised when |
|---|---|
| `SyntaxError` | invalid selector; invalid `insertAdjacentHTML` position; invalid `contentEditable` value; namespace-pipe selector |
| `HierarchyRequestError` | a tree mutation would create a cycle (inserting a node into itself or its descendant) |
| `NotFoundError` | `removeChild` / `insertBefore` / `replaceChild` reference is not a child |
| `NamespaceError` | malformed qualified name in `createElementNS` / `setAttributeNS` |
| `NoModificationAllowedError` | mutating a read-only node, moving one into a constructed subtree, or setting `outerHTML` on a parentless element |

- Evidence: documented. Source: DOM §Errors; H:HTMLElement (contentEditable).
  Confidence: high.

### R-JS-78: Reflected IDL attributes: shared semantics

Per-tag interfaces reflect content attributes as properties. Reading and
writing a reflected property is exactly `getAttribute`/`setAttribute` on the
underlying attribute plus the conversion below. All setters are guarded.

- **string**: get returns the attribute verbatim or `""` when absent; set
  writes `String(value)`.
- **URL string** (`href`, `src`; `form.action` is also a verbatim string):
  **verbatim**, no base-URL resolution (`a.href` of `href="../x"` is `"../x"`, a divergence).
- **boolean (presence)**: get returns whether the attribute is present; set
  `false` removes it; any other value sets it present with value `""`
  (pagelike: the value written is `""`). JavaScript-falsy non-boolean values
  such as `0` or `""`: see C-JS-13.
- **number**: get parses the attribute with the WHATWG integer/float rules,
  applies the per-attribute default when absent or unparsable, and clamps as
  the table says; set writes `String(Number(value))`.
- **enum**: get lower-cases the attribute and returns the canonical keyword; a
  missing attribute returns the *missing default*, an unknown value the
  *invalid default*; set writes the given string verbatim.
- **nullable enum** (`crossOrigin`): missing → `null`; `"use-credentials"`
  (case-insensitive) → `"use-credentials"`; any other present value
  (including `""`) → `"anonymous"`; assigning `null` removes the attribute.
- **string-typed numbers**: `embed`/`object`/`iframe` `width`/`height`,
  `ol.type`, `area.shape`/`coords`, and all deprecated §16.3 attributes reflect
  as verbatim strings.
- Value-like properties (`input.value`, `option.value`, `textarea.value`)
  reflect the **content attribute** (the browser's `defaultValue`), because the
  server DOM has no editing state.
- Contradiction C-JS-13: DELEM says writing "a falsy boolean" removes the
  attribute; every per-interface page says "writing `false` removes it, any
  other value sets it present". Decision: only `false` (and `null`/`undefined`,
  pagelike) removes; `0` and `""` set the attribute present. Probe P-JS-15.
- Contradiction C-JS-14: DOM §Divergences and DELEM list `textarea.value` as
  reflecting the content attribute, but H:HTMLTextAreaElement lists no `value`
  property. Decision: provide `textarea.value` reflecting a `value` content
  attribute (the divergence lists name it twice). Disputed case pins the
  other reading.
- Evidence: documented. Source: DELEM §Reflected IDL attributes, §Reflected
  properties: shared semantics; per-interface pages. Confidence: high.

### R-JS-79: Per-interface reflected properties

Every interface below extends `HTMLElement` → `Element` → `Node` unless noted;
the name in brackets is the content attribute when it differs from the
lower-cased property. † = deprecated (WHATWG §16.3), present only for
`instanceof`, `constructor.name` and reflection. "none" = no
interface-specific properties (use `getAttribute`).

**`HTMLElement`** (every HTML element): `dir` enum {ltr, rtl, auto}, missing or
invalid → `""`; `inputMode` [inputmode] enum {none, text, tel, url, email,
numeric, decimal, search}, missing/invalid → `""`; `enterKeyHint`
[enterkeyhint] enum {enter, done, go, next, previous, search, send},
missing/invalid → `""`; `autocapitalize` enum {"", off, none, on, sentences,
words, characters} where `off` reads as `"none"` and `on` as `"sentences"`,
missing or present-empty → `""`, any other invalid value → `"sentences"`;
`contentEditable` [contenteditable] bespoke: get `"true"`/`"false"`/
`"plaintext-only"`/`"inherit"` (missing or invalid → `"inherit"`,
present-empty → `"true"`, values matched case-insensitively); set accepts only
those four keywords case-insensitively (writes the lower-cased keyword;
`"inherit"` removes the attribute), anything else throws `SyntaxError`;
`isContentEditable` read-only boolean: the nearest inclusive ancestor with a
defined state decides (`true`/`plaintext-only` → true, `false` → false), none
→ false. **Not present**: `hidden`, `title`, `lang`, `tabIndex`, `accessKey`,
`draggable`, `spellcheck`, `translate`, `style`, `dataset`, `innerText`,
`outerText`, `click()`, `focus()`.

| Interface | Tags | Reflected properties |
|---|---|---|
| `HTMLAnchorElement` | `a` | `href` (URL), `target`, `rel`, `download`, `hreflang`, `type`, `referrerPolicy` [referrerpolicy] enum¹ |
| `HTMLAreaElement` | `area` | `alt`, `href` (URL), `target`, `download`, `rel`, `hreflang`, `type`, `shape`, `coords` (verbatim, case preserved), `referrerPolicy` enum¹ |
| `HTMLAudioElement` | `audio` | none (extends `HTMLMediaElement`) |
| `HTMLBRElement` | `br` | none |
| `HTMLBaseElement` | `base` | `href` (URL), `target` |
| `HTMLBodyElement` | `body` | none |
| `HTMLButtonElement` | `button` | `type` enum {submit, reset, button} missing/invalid → `"submit"`; `name`; `value`; `disabled` bool |
| `HTMLCanvasElement` | `canvas` | `width` number (unset → 300), `height` number (unset → 150) |
| `HTMLDListElement` | `dl` | none |
| `HTMLDataElement` | `data` | `value` |
| `HTMLDataListElement` | `datalist` | none |
| `HTMLDetailsElement` | `details` | `open` bool |
| `HTMLDialogElement` | `dialog` | `open` bool |
| `HTMLDirectoryElement` † | `dir` | `compact` bool |
| `HTMLDivElement` | `div` | none |
| `HTMLEmbedElement` | `embed` | `src` (URL), `type`, `width`, `height` (strings) |
| `HTMLFieldSetElement` | `fieldset` | `disabled` bool, `name` |
| `HTMLFontElement` † | `font` | `color`, `face`, `size` |
| `HTMLFormElement` | `form` | `action` (verbatim), `method` enum {get, post, dialog} missing/invalid → `"get"`, `name`, `target`, `enctype` enum {application/x-www-form-urlencoded, multipart/form-data, text/plain} missing/invalid → `"application/x-www-form-urlencoded"`, `acceptCharset` [accept-charset] |
| `HTMLFrameElement` † | `frame` | `name`, `scrolling`, `src` (URL), `frameBorder` [frameborder], `longDesc` [longdesc], `noResize` [noresize] bool, `marginHeight` [marginheight], `marginWidth` [marginwidth] |
| `HTMLFrameSetElement` † | `frameset` | `cols`, `rows` |
| `HTMLHRElement` | `hr` | none |
| `HTMLHeadElement` | `head` | none |
| `HTMLHeadingElement` | `h1`–`h6` | none |
| `HTMLHtmlElement` | `html` | none |
| `HTMLIFrameElement` | `iframe` | `src` (URL), `srcdoc`, `name`, `width`, `height` (strings), `allow`, `loading` enum {lazy, eager} missing/invalid → `"eager"`, `referrerPolicy` enum¹ |
| `HTMLImageElement` | `img` | `src` (URL), `alt`, `width` number (unset → 0), `height` number (unset → 0), `srcset`, `sizes`, `loading` enum {lazy, eager} → `"eager"`, `decoding` enum {sync, async, auto} missing/invalid → `"auto"`, `crossOrigin` nullable enum², `referrerPolicy` enum¹ |
| `HTMLInputElement` | `input` | `type` enum {text, search, tel, url, email, password, date, month, week, time, datetime-local, number, range, color, checkbox, radio, file, submit, image, reset, button, hidden} missing/invalid → `"text"`; `name`; `value` (content attribute); `placeholder`; `required`, `disabled`, `readOnly` [readonly], `checked` bool; `min`, `max`, `step`, `pattern`, `autocomplete` (strings) |
| `HTMLLIElement` | `li` | `value` number (unset → 0; negative allowed) |
| `HTMLLabelElement` | `label` | `htmlFor` [for] |
| `HTMLLegendElement` | `legend` | none |
| `HTMLLinkElement` | `link` | `href` (URL), `rel`, `type`, `media`, `as`, `crossOrigin` nullable enum², `referrerPolicy` enum¹ |
| `HTMLMapElement` | `map` | `name` |
| `HTMLMarqueeElement` † | `marquee` | `behavior`, `bgColor` [bgcolor], `direction`, `height`, `hspace`, `loop`, `scrollAmount` [scrollamount], `scrollDelay` [scrolldelay], `vspace`, `width` (all verbatim strings), `trueSpeed` [truespeed] bool |
| `HTMLMediaElement` | (abstract, no tag) | `src` (URL), `crossOrigin` nullable enum², `preload` (verbatim string), `autoplay`, `loop`, `controls` bool |
| `HTMLMenuElement` | `menu` | none |
| `HTMLMetaElement` | `meta` | `name`, `content`, `httpEquiv` [http-equiv], `charset` |
| `HTMLMeterElement` | `meter` | numbers, read/write: `min` (unset → 0); `max` (unset → 1, never below `min`); `value` (unset → 0, clamped to [min, max]); `low` (unset → min, clamped to [min, max]); `high` (unset → max, clamped to [low, max]); `optimum` (unset → midpoint of min and max, clamped to [min, max]) |
| `HTMLModElement` | `ins`, `del` | `cite`, `dateTime` [datetime] |
| `HTMLOListElement` | `ol` | `reversed` bool, `start` number (unset → 1; negative allowed), `type` (verbatim string) |
| `HTMLObjectElement` | `object` | `data`, `type`, `name`, `width`, `height` (strings) |
| `HTMLOptGroupElement` | `optgroup` | `disabled` bool, `label` |
| `HTMLOptionElement` | `option` | `value` (content attribute), `label`, `selected` bool, `disabled` bool |
| `HTMLOutputElement` | `output` | `name` |
| `HTMLParagraphElement` | `p` | none |
| `HTMLParamElement` † | `param` | `name`, `value` |
| `HTMLPictureElement` | `picture` | none |
| `HTMLPreElement` | `pre` | none |
| `HTMLProgressElement` | `progress` | numbers, read/write: `max` (unset or non-positive → 1); `value` (unset or negative → 0, clamped to [0, max]) |
| `HTMLQuoteElement` | `blockquote`, `q` | `cite` |
| `HTMLScriptElement` | `script` | `src` (URL), `type`, `defer`, `async`, `noModule` [nomodule] bool, `crossOrigin` nullable enum², `referrerPolicy` enum¹ |
| `HTMLSelectElement` | `select` | `name`, `required`, `disabled`, `multiple` bool |
| `HTMLSlotElement` | `slot` | `name` |
| `HTMLSourceElement` | `source` | `src` (URL), `type`, `srcset`, `sizes`, `media` |
| `HTMLSpanElement` | `span` | none |
| `HTMLStyleElement` | `style` | `media`, `type` |
| `HTMLTableCaptionElement` | `caption` | none |
| `HTMLTableCellElement` | `td`, `th` | `colSpan` [colspan] number (unset → 1, clamped to [1, 1000]), `rowSpan` [rowspan] number (unset → 1, clamped to [0, 65534]), `headers`, `scope` enum {row, col, rowgroup, colgroup} missing/invalid → `""`, `abbr` |
| `HTMLTableColElement` | `col`, `colgroup` | `span` number (unset → 1, clamped to [1, 1000]) |
| `HTMLTableElement` | `table` | none |
| `HTMLTableRowElement` | `tr` | none |
| `HTMLTableSectionElement` | `thead`, `tbody`, `tfoot` | none |
| `HTMLTemplateElement` | `template` | none (no `content` property is documented) |
| `HTMLTextAreaElement` | `textarea` | `name`, `placeholder`, `required`, `disabled`, `readOnly` [readonly] bool, `rows` number (unset → 2; min 1, out of range → default), `cols` number (unset → 20; min 1, out of range → default), `wrap` enum {soft, hard} missing/invalid → `"soft"`; `value` per C-JS-14 |
| `HTMLTimeElement` | `time` | `dateTime` [datetime] |
| `HTMLTitleElement` | `title` | none |
| `HTMLTrackElement` | `track` | `src` (URL), `kind` enum {subtitles, captions, descriptions, chapters, metadata} missing → `"subtitles"`, invalid → `"metadata"`, `srclang`, `label`, `default` bool |
| `HTMLUListElement` | `ul` | none |
| `HTMLVideoElement` | `video` | extends `HTMLMediaElement`; `width`, `height` numbers (unset → 0), `poster`, `playsInline` [playsinline] bool |

¹ `referrerPolicy` enum keywords: `""`, `no-referrer`,
`no-referrer-when-downgrade`, `same-origin`, `origin`, `strict-origin`,
`origin-when-cross-origin`, `strict-origin-when-cross-origin`, `unsafe-url`;
missing or invalid → `""`.
² `crossOrigin` per R-JS-78 (nullable enum).

Tags with no dedicated interface are plain `HTMLElement` (for example
`section`, `article`, `nav`, `header`, `footer`, `main`, `aside`, `figure`,
`figcaption`, `em`, `strong`, `b`, `i`, `u`, `small`, `code`, `abbr`, `cite`,
`dfn`, `kbd`, `mark`, `samp`, `sub`, `sup`, `var`, `wbr`, `address`, `hgroup`,
`search`, `noscript`, `summary`, `ruby`, `rt`, `rp`, `bdi`, `bdo`, `s`, and
unknown or custom tags). Numeric reflections of `img`/`video`/`canvas`
`width`/`height` use the non-negative integer rules; a negative or unparsable
value reads as the default. Numeric parsing for `meter`/`progress` uses the
floating-point rules.
- Evidence: documented. Source: DELEM §Reflected IDL attributes table; every
  H:<Name> page. Confidence: high (property lists, defaults), medium (parsing
  of invalid numbers, which follows WHATWG and is not spelled out).

### R-JS-80: Divergences from the browser DOM (checklist)

pagelike MUST reproduce each of these (they are observable to isomorphic
bindings):

1. Only listed members exist; others are `undefined` (R-JS-64).
2. `querySelector`/`querySelectorAll` on an element scan the whole document
   (R-JS-71); `getElementsByTagNameNS`, `closest`, `matches`, `getElementById`
   and `children` are receiver-scoped.
3. `children` and all query results are static `NodeList`s; there is no
   `HTMLCollection`.
4. `tagName` upper-cased for no-namespace elements, verbatim for
   `createElementNS`; parsed SVG/MathML is upper-cased too.
5. `setAttributeNS` stores the qualified name verbatim; `getAttributeNS` needs an
   in-scope `xmlns:` prefix.
6. `createElementNS` emits no `xmlns:` declaration and skips the reserved
   `xml`/`xmlns` prefix checks.
7. `p|tag` throws `SyntaxError`; `*|tag` matches nothing.
8. `DOMParser` ignores the MIME type.
9. Reflected value-like properties reflect content attributes; URL properties
   are not resolved.
10. `classList` removes the `class` attribute when emptied.
11. Adopting a node from another document deep-copies it.
12. `CharacterData.length` counts code points.
13. `DOMException.prototype.toString` has the `DOMException: ` prefix.
14. `getElementById` exists on elements and is subtree-scoped.
15. `XMLSerializer` produces HTML serialization.
16. `id` and `className` are read-only.
17. DOM operations are charged against the request budget (R-JS-58).

- Evidence: documented (1–10, 12–14, 16, 17), inferred (11 wording, 15).
  Source: DOM §Divergences; DNODE; DEX; DPARSE; DELEM. Confidence: high.

---

## A11. Imported schema classes

### R-JS-85: Class synthesis

Each schema import yields a class synthesized at evaluation time:

- `Class.name` is the schema's short name: the last path segment of the
  governed type URL (`https://moodboard.pagelove.org/Note` → `Note`). For a URL
  without a path segment (`urn:Test`) pagelike uses the text after the last `:`
  (pagelike decision). The local import binding name is the author's choice.
- The class extends the class of the schema's `parent` (so `instanceof` walks
  the chain), or `Map` when the chain reaches `https://pagelove.org/Map`, else
  `Object`.
- Declared properties are accessor pairs on the prototype, backed by a
  `Symbol`-keyed store on each instance.
- Declared instance methods are prototype methods; `static` methods are on the
  class. Calling one runs its implementation (JavaScript or Sessel) through the
  cross-language path (R-JS-17).
- Evidence: documented. Source: JSS §Importing schemas, §Working with imported
  classes. Confidence: high (behavior), low (`urn:` naming).

### R-JS-86: Operations on imported classes

| Operation | Behavior |
|---|---|
| `new Foo({...})` | the plain object's keys that are declared property names initialize those properties; undeclared keys are stored on the instance but trigger no pipelines |
| `instance.prop` / `instance.prop = v` | read / write a declared property through the accessors |
| `instance instanceof Bar` | true when `Foo` extends `Bar` (via `parent`) |
| `instance.method(...)` | runs the declared implementation |
| returning the instance from a binding | an Instance (R-JS-41); defaults and validation per R-JS-20 when a method returns it; injected as a nested item when a `default` returns it (R-JS-11) |

Map schemas (whose chain includes `https://pagelove.org/Map`): the class extends
`Map` and exposes `keys`, `values`, `entries` and `merge`; reading an
undeclared property falls through to the Map's backing store.
- Evidence: documented. Source: JSS §Working with imported classes.
  Confidence: high (table), low (Map behavior details, `merge` semantics:
  pagelike merges another Map or plain object's entries into this one).

### R-JS-87: Default constructing an instance (documented example)

A `default` that imports a schema and returns `new Note({ title: "untitled",
x: 0, y: 0 })` injects a nested `Note` item as the property's value
(`itemprop="<name>" itemscope itemtype="<Note URL>"` with the three values).
- Evidence: documented. Source: JSS §Examples (Default that constructs a schema
  instance). Confidence: medium (injected markup is pagelike's).

---

## A12. Browser execution of schema bindings

### R-JS-89: Schema bindings are server-only today

The server never expects a browser to run `default`/`@read`/`@write`/
`@validate`/`@computed`/method bindings. The source contract is portable
(the same module can be imported in a browser), and beta-js does run
`default` and `@read` modules client-side for its own view (R-JS-112), but the
server's value is always the stored one. Nothing in the protocol depends on
client-side execution.
- Evidence: documented. Source: JSS intro, §First-cut exclusions; SDH
  §Client-side dynamic defaults, §Not supported client-side. Confidence: high.


---

# Part B: client library contract (beta-js)

pagelike serves pages that load beta-js unchanged from
`https://pagelove.github.io/beta-js/…`. The server's obligations are the HTTP
behaviors below. Where the client and its docs disagree, the client code is
what runs, so the server satisfies the client (C-JS-20..).

Older libraries used by the demo apps (`pagelove-primitives`, `dom-core`,
`dom-forms`, `dom-subscriber`, `selector-request`, loaded from
`cdn.pagelove.net`) are covered by protocol R-PROTO-4/21 and reading-writing
R-RW-140/141; no demo app imports beta-js.

## B1. Loading and side effects

### R-JS-100: Modules and what importing them does

| Module | Import side effect | Network |
|---|---|---|
| `pagelove.mjs` | constructs `new PLDocument()` bound to the page and immediately issues the discovery `OPTIONS` (the exported `ready` promise); after `ready` and `DOMContentLoaded`, if the page has `<main>` and no `Pagelove` was constructed by app code, runs `document.pagelove = new Pagelove({ view: main })` and `start()` | R1 (then R2, R4–R11 on interaction) |
| `pagelove/sse.mjs` | constructs one `PageloveSSE` for the page URL, which opens an `EventSource` at once | R12 |
| `pagelove/primitives.mjs` | none (classes only) | on use |
| `pagelove/component.mjs` | registers mixins for `[data-draggable]`, `[data-resizable]`, `[data-stackable]`, `[data-droppable]`, `[data-sortable]` | R9–R11 on interaction |
| `pagelove/debug.mjs` | sets `window.Pagelove` to the debug object; reads `localStorage.pagelove_debug` | none |
| `pagelove/dom-subscriber.mjs` | none | none |

The page URL used everywhere is `window.location.href` without its fragment
(query string kept).
- Evidence: client-source. Source: BJ-MAIN:41-43, 1119-1129; BJ-SSE:35-38,
  289; BJ-PRIM:423-432; BJ-COMP:607, 690, 751, 946, 1065; BJ-DBG:63-66.
  Confidence: high.
- Divergence C-JS-20: JSIDX says `sse.mjs` subscribes to `window.location.href`;
  the code strips the fragment. PLC says auto-start runs when `ready` resolves;
  the code also waits for `DOMContentLoaded`. Server impact: none.

### R-JS-101: What the server must do for the library to run

- Serve the page and its writes on the **same origin** (the client uses
  relative same-origin `fetch` with default credentials, so the session cookie
  travels; no CORS is involved for pagelike-hosted pages).
- Not send a `Content-Security-Policy` that blocks `blob:` module scripts on
  pages: beta-js imports schema `default`/`@read` modules from Blob URLs
  (R-JS-112). pagelike sends no CSP on composed pages by default (inferred).
- If a site re-hosts beta-js itself, the module files must be served with a
  JavaScript `Content-Type` (`text/javascript`) and, for cross-origin import,
  `Access-Control-Allow-Origin: *`. PageLove's own deployment does this with a
  JavaScript Processor (BJ-CORS), which pagelike supports (R-JS-22, R-JS-53).
- Evidence: client-source · inferred (CSP). Source: BJ-MAIN:236-249;
  BJ-CORS. Confidence: medium.

## B2. Request inventory

### R-JS-102: Every request beta-js issues

`<gen>` is the generated selector of R-JS-104; `<url>` is the page URL
(R-JS-100). "etag" is the `etag` expando the client keeps on DOM elements
(R-JS-108). All requests use `fetch` defaults (`credentials: same-origin`,
`mode: cors`), so same-origin cookies are sent.

| # | Trigger | Method, URL | Request headers (exact spelling) | Body |
|---|---|---|---|---|
| R1 | import of `pagelove.mjs` | `OPTIONS <url>` | `Prefer: return=representation`, `Accept: multipart/mixed` | none |
| R2 | a capability-bearing element with an `id`, no `data-for`, and no etag comes within 200px of the viewport | `HEAD <url>` | `Range: selector=<gen>` | none |
| R3 | `PLDocument(url).document` for a URL other than the page | `GET <url>` | none | none |
| R4 | `element.GET()` | `GET <url>` | `Range: selector=<gen>`; `If-Match: <etag>` when the etag is a string | none |
| R5 | `element.PUT(body?)` (direct, or by a commit, R-JS-114) | `PUT <url>` | `Range: selector=<gen>`; `Content-Type: text/html;charset=UTF-8`; `If-Match: <etag>` when the etag is a string | `body` string, or the serialization of a node (R-JS-106); default = the element itself |
| R6 | `element.POST(body)` | `POST <url>` | `Range: selector=<gen of the parent/target>`; `Content-Type: text/html;charset=UTF-8`; never `If-Match` | string, or serialized node |
| R7 | `element.DELETE()` | `DELETE <url>` | `Range: selector=<gen>`; `If-Match: <etag>` when the etag is a string | none |
| R8 | commit fallback when no ancestor has `.PUT` (R-JS-114) | `PUT <url>` | `Range: selector=#<CSS.escape(id)>`; `Content-Type: text/html` | `article.outerHTML` (not stripped) |
| R9 | drag-and-drop move (R-JS-116), only when both selectors are id selectors | `MOVE <url>` | `Range: selector=#<src-id>`; `Destination: <url>` (absolute URL); `Destination-Range: selector=#<dest-id>; placement=<append\|before>`; `If-Match: <etag>` when a non-empty string | none |
| R10 | Droppable fallback after MOVE 405 | `POST` as R6 to the container (retried once on a thrown error), then `DELETE <url>` with only `Range: selector=#<source-id>` (id not escaped; retried once) | as listed | serialized clone |
| R11 | Sortable fallback after MOVE 405 | `PUT` as R5 on the nearest ancestor with `.PUT` (retried once) | as R5 | serialized container |
| R12 | import of `sse.mjs`, or `new PageloveSSE(url)` | `GET <url>` via `new EventSource(url, { withCredentials: true })` | added by the browser: `Accept: text/event-stream`, `Cache-Control: no-cache`, and `Last-Event-ID` on automatic reconnect | none |

There are no other requests. In particular beta-js never sends
`Pagelove-Connection`, never uses `QUERY`, never asks for JSON-LD, and never
sends `If-None-Match`.
- Evidence: client-source. Source: BJ-PRIM:180-189 (R1), 474-501 (R2),
  503-517 (R3), 258-273 and 327-416 (R4-R7); BJ-MAIN:932-955 (R8); BJ-COMP:
  767-803 (R9), 924-944 (R10), 1041-1055 and 1119-1130 (R11); BJ-SSE:40-41
  (R12). Confidence: high.

### R-JS-103: Discovery (R1) consumption

The server's OPTIONS contract is protocol R-PROTO-4, R-PROTO-16..21. What the
client does with the response:

1. `response.ok` false → throws. 2xx without a `Content-Type` matching
   `/boundary=(.+)$/` (the boundary must be the last parameter, unquoted) →
   `ready` rejects and `start()` fails (the documented `204` fallback triggers
   this; R-PROTO-21).
2. The body is split on `--<boundary>`; sections that trim to empty or `--` are
   dropped; each section is split at the first `\r\n\r\n`; header lines are
   split on `\r\n`, and each line on `": "` (so a header value containing
   `": "` is truncated); names are lower-cased. Part bodies are ignored.
3. For each part with a `content-range` header: the selector is the value with
   `^selector\s*=?\s*` removed (both `selector <css>` and `selector=<css>`
   work). The part MUST also carry `allow` (a missing `allow` throws). The
   selector is handed to the browser's `querySelectorAll`, now and on every
   later DOM insertion, so it must be a selector the browser accepts; a
   PageLove-only pseudo-class throws in the browser and aborts discovery.
4. For every matching element a `PLCapability` event
   `{ selector, allow: [tokens split on "," and trimmed] }` is dispatched.
- Evidence: client-source. Source: BJ-PRIM:113-221; BJ-SUB. Confidence: high.

### R-JS-104: Selector generation

`<gen>` for an element `el`:

1. If `el.id` is non-empty: `#` + `CSS.escape(el.id)`.
2. Otherwise walk up. At each step with parent `p` (stop if `p` is neither an
   element nor the document):
   - if `p.id` is non-empty, return `#<CSS.escape(p.id)> > ` + the steps joined
     with ` > `;
   - else prepend the step for `el` and continue with `el = p`.
3. If the walk reaches the document, return the steps joined with ` > `
   (starting at the root step, e.g. `html:nth-child(1) > body:nth-child(2) >
   main:nth-child(1)`). On an exception: the lower-cased tag name.

A step for `el` under `p`: if `el` has `itemprop`, use `[itemprop="<v>"]` when it
is the only child of `p` with that selector, else `<tag>[itemprop="<v>"]` when
that is unique among `p`'s children; otherwise `<tag>:nth-child(<k>)` where `k`
is `el`'s 1-based index among `p`'s element children. The `itemprop` value is
`CSS.escape`d inside the double quotes.

Server obligations (cross-area selectors): parse CSS escapes in id selectors
(`#\31 23` for id `123`), attribute selectors with double-quoted values,
the child combinator with surrounding spaces, `:nth-child(<k>)` including on
the root element (`html:nth-child(1)` must match the root, whose parent is the
document), and evaluate them against the **served** page structure (the
composed page the browser sees), since the selector is computed in the
browser's DOM. For elements inside composed regions this is the write-through
routing of the composition area.
- Evidence: client-source. Source: BJ-PRIM:8-64; reading-writing R-RW-140.
  Confidence: high.

### R-JS-105: Request construction for element methods (R4-R7)

- `Range: selector=<gen>` with no space after `=`. The selector is computed at
  request time from the element's current position (a detached element
  without an id yields an empty or partial selector).
- `Content-Type: text/html;charset=UTF-8` (no space before `charset`) when the
  request has a body.
- `If-Match: <etag>` for GET, PUT and DELETE (not POST) when the element's etag
  is a string. **GET carries `If-Match` too**; the server must evaluate it
  (412 on mismatch, reading-writing R-RW-140). **Superseded (live 2026-09-29):**
  the server ignores `If-Match` on a GET (206), as R-RW-140 was reconciled
  (`rw.cond.get-if-match-stale`); writes with a stale tag still get 412 (case
  `javascript.client.head-etag-then-conditional-writes`; docs/compat/decisions-2026-09-29/javascript.md).
- Each request dispatches `PLMethodStarted` `{ method, selector }` before
  sending and `PLMethodCompleted` `{ method, selector, response }` after the
  verb's local DOM work, even when that work throws. `selector` is the Range
  value without the `selector=` prefix; the SSE client uses it for echo
  matching (R-JS-110).
- Evidence: client-source. Source: BJ-PRIM:258-325. Confidence: high.

### R-JS-106: Body serialization

A `Node` body (and the default PUT body) is serialized by cloning it and
removing, on the clone and every descendant element, every attribute whose
name starts with `data-pl-` and the `contenteditable` attribute, then taking
`outerHTML`. String bodies are sent as given. Consequences for the server:
bodies are single elements in browser serialization (`itemscope=""`,
lower-case tags, double-quoted attributes); the server stores what it receives
(modeling and write paths apply). The R8 fallback sends raw `outerHTML`
(with `contenteditable` if present).
- Evidence: client-source. Source: BJ-PRIM:73-90, 343-405; BJ-MAIN:944-953.
  Confidence: high.

### R-JS-107: Response properties the client relies on

| Request | Relied on |
|---|---|
| every R4-R7 | `ETag` header (any status, including 4xx) replaces the element's etag when present |
| R1 | `ok`, `Content-Type` boundary, multipart body (R-JS-103) |
| R2 | `ETag` header (any status; absent → etag `undefined`); the status is not checked |
| R3 | body text (parsed as a whole HTML document) |
| R4 GET | `ok`; body = **exactly one node** after trimming (else it throws) |
| R5 PUT | nothing but `ETag`; failures are only logged |
| R6 POST | `ok`; body = exactly one node after trimming (the inserted element); `ETag` and `Content-Location` (R-JS-108) |
| R7 DELETE | `ok` → the element is removed from the page |
| R8 | nothing |
| R9 MOVE | `ok` → done; **405** → fallback (and MOVE is not tried again for the page's lifetime); any other status → rollback |
| R10 DELETE | `ok` or **416** (treated as already deleted) |
| R12 | the event stream (R-JS-110) |

- Evidence: client-source. Source: BJ-PRIM:295-297, 327-416, 484-494;
  BJ-COMP:791-802, 936. Confidence: high.

### R-JS-108: ETag lifecycle

- An element's etag starts `undefined`. When a `PLCapability` arrives for an
  element with an `id`, no `data-for`, and etag `undefined`, the element is
  registered with an `IntersectionObserver` (`rootMargin: 200px`); when it
  becomes visible the etag is set to `null` (pending) and R2 runs.
- Every R4-R7 response carrying `ETag` sets the element's etag to it.
- POST (RFC 9110 §8.8.3 reading): if the response has `Content-Location` whose
  path differs from the request URL's path, or which has a query or fragment,
  the `ETag` belongs to the new child (the parent's etag is cleared);
  otherwise the `ETag` belongs to the parent (the child's etag stays unknown).
  No `ETag` → the parent's etag is cleared. pagelike sends no
  `Content-Location` on selector POST, so the POST `ETag` MUST be the target
  (parent) element's new ETag (reading-writing R-RW-73).
- A mutation event's `etag` is stored on the element it produced (R-JS-110).
- `--remove-instance` clears the etag before deleting, so that DELETE carries
  no `If-Match`.
- Doc divergence C-JS-21: PRIM says "after a POST, the parent's previous ETag
  is restored and the response ETag is assigned to the new child"; the code
  implements the Content-Location rule above. Server follows the code.
- Evidence: client-source. Source: BJ-PRIM:343-399, 454-501; BJ-MAIN:882.
  Confidence: high.

### R-JS-109: Capability attachment

For each `allow` token, upper-cased, that names an element method (`GET`,
`PUT`, `POST`, `DELETE`), a non-writable, enumerable, configurable property of
that name is defined on the element, bound to a new `PLElement`. Other tokens
(`HEAD`, `OPTIONS`, `MOVE`, `QUERY`) are ignored. Consequently the server's
selector-scoped `Allow` values decide which elements can be written by the
declarative runtime: an element whose nearest capability-bearing ancestor has
`PUT` is committed through that ancestor (R-JS-114).
- Evidence: client-source. Source: BJ-PRIM:434-461. Confidence: high.

## B3. Real-time client

### R-JS-110: SSE consumption

- One `EventSource` per `PageloveSSE`, `withCredentials: true`. Listens to
  events `mutation` and `reset` only (`pagelove-connection` and unnamed
  `message` events are ignored). Reconnection is the browser's.
- `mutation` data is parsed as an HTML document; the item is the first
  element matching `[itemtype="https://pagelove.org/Mutation"]` (exact attribute
  value); missing → the event is dropped. Each property is the **first**
  descendant of the item with `[itemprop="<name>"]`: `method`, `selector`,
  `path`, `host`, `etag`, `destination`, `placement` are read as
  `textContent`; `body` as `innerHTML`. So scalar properties must be element
  text (not `content` attributes), and `method`/`selector`/`path`/`host`/`etag`
  must precede `body` in document order; `placement`/`destination` after
  `body` are safe only because MOVE bodies are empty and POST ignores
  `placement` (SSE R-SSE-8 fixes this order).
- Echo suppression: each `PLMethodStarted` records `{method, selector}`; each
  `PLMethodCompleted` removes one matching record; records expire after
  30,000 ms. An incoming mutation whose upper-cased `method` and exact
  `selector` string match a record is an echo: its DOM step is skipped. The
  server's `selector` must therefore be the request's `Range` selector
  verbatim.
- Application (`PLMutation` is dispatched first and is cancelable): the target
  is `document.querySelector(selector)` (no match → dropped silently).
  `POST`: the trimmed body is parsed and all its nodes appended to the target.
  `PUT`: the body's first element (or first node) replaces the target.
  `DELETE`: the target is removed. `MOVE`: the target is moved relative to
  `document.querySelector(destination)` by `placement` (`before`, `after`,
  `prepend`, `append`; case-insensitive; default `append`). Any other method
  is ignored. Then the mutation's `etag` is set on the resulting element and
  `PLMutationApplied` is dispatched.
- `reset` data: the text of the first `[itemprop="reason"]` element, or
  `"unknown"`. `PLStreamReset` is dispatched (cancelable); unless cancelled the
  page reloads.
- beta-js does not read the connection token or send `Pagelove-Connection`, so
  the server's session-based echo suppression applies: a write from one tab is
  not delivered to another tab of the same session (documented).
- Divergence C-JS-22: SSEC documents POST/PUT/DELETE and `parse` returning
  `{method, selector, path, host, body, etag}`; the code also handles `MOVE`
  and returns `destination` and `placement`. Server: emit MOVE events per SSE
  spec.
- Evidence: client-source · documented. Source: BJ-SSE:40-258; BJ-TEST
  (sse-echo, primitives-completion-order); SSEC. Confidence: high.

## B4. Page markup and client-side modeling

### R-JS-111: Markup the server must deliver intact

For pages using `pagelove.mjs`, composed GET responses MUST keep:

- `<main>` (auto-start target) and any other view roots;
- `<template itemtype="…">` elements with their content unchanged (the client
  clones `template.content.firstElementChild`; `data-bind*`, `command`,
  `commandfor`, `data-schema` are ordinary attributes the server must not
  strip);
- Schema declarations `[itemtype="https://pagelove.org/Schema"]`, including
  `hidden` ones, with their `type`/`parent` metas as **direct children** and
  property items anywhere inside (the client reads `:scope > [itemprop="type"]`);
- client-side JavaScript modules as a wrapper that is a **direct child** of the
  property item (`:scope > [itemprop="default"|"@read"][itemtype=
  "https://pagelove.org/JavaScript/Module"]`) whose `source` is a **direct
  child** `<script itemprop="source">`; static defaults as a direct-child
  `<meta itemprop="default">`;
- schema instances with their `id`s.

The server's microdata reading is less strict (R-JS-1); the direct-child
requirement is a client limitation authors must follow.
- Evidence: client-source. Source: BJ-MAIN:189-305. Confidence: high.

### R-JS-112: Client-side schema discovery (informative; differs from the server)

- `#discoverSchemas` reads every `[itemtype="https://pagelove.org/Schema"]`:
  `type` (required; skipped silently when missing), `parent`, and each
  `[itemprop="property"]` whose itemtype is `https://pagelove.org/Property` or
  a declared subtype (others, including `Method`, are ignored with a warning).
  Per property: `name` (required), `type` (default `""`), `cardinality`
  (default `0..1`, unlike the server's `0..n`), a static or JavaScript
  `default`, and a JavaScript `@read`. `@computed`, methods and schema-level
  `@validate` are not implemented client-side.
- JavaScript modules are loaded with `import(URL.createObjectURL(new Blob([src],
  {type:"text/javascript"})))`; the `default` export becomes the default
  (called with no arguments when an instance is created) or the read
  transform (called with the string value; its result, joined if an array, is
  set as `innerHTML` of display elements or `textContent` of `<time>`).
- Inheritance: parent properties first, child overrides by name; an unknown
  parent throws `Schema "<child>" declares unknown parent "<parent>"`; a cycle
  throws `Schema inheritance cycle detected at "<url>"` (both abort `start()`;
  the server instead stops silently at an unknown parent, modeling C12).
- Evidence: client-source · documented. Source: BJ-MAIN:227-335; SDH.
  Confidence: high.

### R-JS-113: Instances and the markup `create()` sends

- Discovered instances: `[itemscope][itemtype]:not(template):not([itemprop]):not([data-for])`
  whose itemtype has a registered template (and which match `filter`); with an
  explicit `schema` root, its `:scope > [itemscope][itemtype][id]` children.
  (SIH omits `:not([data-for])`; C-JS-23, no server impact.)
- `create(typeUrl, values, { tag = "article" })` builds
  `<article itemscope="" itemtype="<typeUrl>" id="item-<base36 ms><4 base36 chars>">`
  and, per resolved property in order: skip `0..n` without a value;
  `https://pagelove.org/DateTime`/`Date` → `<time itemprop datetime="<value or now ISO>">`;
  other `https://pagelove.org/*` types except Text/Integer/Number/Boolean →
  nested, only if the value is an element; everything else (including
  `https://schema.host/*` types) → `<meta itemprop content="<String(value ?? "")>">`.
- Server impact: these articles arrive as POST bodies (R-JS-115) and PUT
  bodies. Ids start with `item-`. Type URLs on the client use
  `https://pagelove.org/<Type>` while the server's primitive types are
  `https://schema.host/<Type>` (modeling C12): the server treats
  `https://pagelove.org/Text` as an unknown type (no validation).
- Evidence: client-source. Source: BJ-MAIN:162-175, 348-386. Confidence: high.

### R-JS-114: Two-way binding commits

- View → schema: `change` on `[data-bind]` commits immediately for `<select>`
  and checkbox/date/color inputs; `focusout` commits `[contenteditable]`,
  input, select and textarea bindings (`blur`); `data-commit="idle:<ms>"`
  commits after `<ms>` (default 500) of no `input`; `data-commit` overrides
  the default mode. The value is `innerText.trim()` (contenteditable),
  `value`, `"true"`/`"false"` (checkbox), or `textContent.trim()`.
- The value is written into the schema article: an existing `<meta>` gets
  `content`, `<time>` gets `datetime`, others get `textContent`; a missing
  property element is appended as `<time itemprop datetime>` when the value
  looks like an ISO date (`/^\d{4}-\d{2}-\d{2}(T\d{2}:\d{2}:\d{2})?/`), else
  `<meta itemprop content>`. `beforeCommit` can veto; `afterCommit` runs after
  the PUT resolves.
- Persist: wait for a pending create POST of that article; then walk from the
  article up to the first element that has a `.PUT` capability and call
  `PUT()` with no body, i.e. R5 with that element's whole serialized subtree
  and its etag as `If-Match`. If no ancestor has `.PUT`, send R8.
- `data-bind-attr` changes (Draggable, Resizable, Stackable) are batched into
  one persist per article per observer callback.
- Server impact: selector PUTs replacing whole containers (many instances in
  one body, validated together), conditional on the container's element ETag.
  A 412 is not retried; the local edit stays unsaved.
- Evidence: client-source. Source: BJ-MAIN:735-955, 1069-1105; HB §Two-way
  updates. Confidence: high.

### R-JS-115: Declarative commands

- `--create-instance` (button with `data-schema`, optional `data-element` tag):
  target = the schema element of the nearest `[data-for]` ancestor, else the
  `commandfor` element. The new article gets `itemprop` = the name of the
  first property of the target's schema whose `type` equals the new type
  exactly. It is appended to the target, then `target.POST(article)` (R6) runs
  in the background when the target has a POST capability; the promise is kept
  so later commits wait for it.
- `--remove-instance`: the schema element of the nearest `[data-for]`
  ancestor; its etag is cleared, it is removed from the page, then its `DELETE`
  capability (R7, no `If-Match`) is called if present.
- Other `command` values are ignored. Commands inside the view without
  `commandfor` are delegated clicks; buttons with `commandfor` use the native
  `command` event on the target.
- Evidence: client-source · documented. Source: BJ-MAIN:844-908; DCMD.
  Confidence: high.

### R-JS-116: Components, mixins and MOVE

- Auto-defined custom elements (hyphenated template roots) and the binders are
  client-only (WC). The mixins that talk to the server:
  - **Droppable** (`[data-droppable]`): accepts a card whose itemtype is the
    container schema's property type (or a subtype, via declared `parent`s),
    honoring the property's maximum cardinality; tries R9 with `placement=append`;
    on 405 rolls back and does R10 (new id `item-…` for the clone).
  - **Sortable** (`[data-sortable]`, and `attachSortableBehavior` for
    non-component `[data-sortable]` elements such as `<main data-sortable
    data-sort-axis="x">` over `:scope > article[itemscope]` children): tries R9
    with `placement=before` (or `append` to the container); on 405 does R11.
  - **Draggable/Resizable/Stackable** write `data-x`/`data-y`,
    `data-width`/`data-height`, `data-z`, which R-JS-114 persists.
- R9 is attempted only when source and destination selectors match
  `^#[\w-]+(?:\s+#[\w-]+)*$` (ids of word characters and hyphens); otherwise
  the fallback runs directly.
- Server impact: support MOVE per protocol R-PROTO-90..106 with an absolute
  `Destination` URL on the same origin; answer 405 only if MOVE is truly
  unsupported (pagelike supports it, protocol R-PROTO-105); DELETE of a
  missing element returns 416.
- Divergence C-JS-24: WC documents three built-in mixins; the code also has
  Droppable, Sortable, `data-render-stamp`/`data-render-contains` child render
  targets, and `setState` custom states. Server impact: MOVE (above).
- Evidence: client-source. Source: BJ-COMP:753-1134. Confidence: high.

### R-JS-117: The Pagelove class and exports (informative)

Constructor options `view` (required), `filter`, `schema` (legacy),
`beforeCommit`, `afterCommit`, `onRender`, `onPatch`, `ensureSchemaEl`; methods
`start()` (idempotent; awaits `ready`), `stop()`, `populate(viewEl, id)`,
`renderAll()`, `create(typeUrl, values, {tag})`, `flushPendingPatches()`,
`getSchema`, `isTypeOrSubtype`, `renderArticle`, `getTemplate`; getter
`hasPendingPatches`. Exports: `Pagelove`, `ReactiveTemplate` (alias),
`PageloveComponent`, `Draggable`, `registerComponentMixin`, `ready`. Globals:
`window.pagelove` (last constructed instance), `document.pagelove` (a `WeakRef`
to the page `PLDocument`, replaced by the auto-started `Pagelove` instance),
`window.Pagelove` (the debug object, R-JS-118). Remote patches to a focused
bound element are deferred until focus leaves. No server contract beyond B2.
- Evidence: client-source · documented. Source: BJ-MAIN:48-186; PLC.
  Confidence: high.

### R-JS-118: Debug channels (client-only)

`Pagelove.SSE` = 1, `PRIMITIVES` = 2, `SCHEMA` = 4, `ALL` = ~0; `Pagelove.debug`
(get/set, coerced with `| 0`) persisted in `localStorage.pagelove_debug`;
`log`/`warn`/`error(channel, ...args)` print only when `debug & channel`. No
server contract.
- Evidence: client-source · documented. Source: BJ-DBG; DBG. Confidence: high.

### R-JS-119: Known client defects the server must tolerate

1. The documented OPTIONS `204` fallback makes `ready` reject (R-PROTO-21);
   pages need at least one selector-scoped rule for the viewing actor.
   pagelike keeps the documented 204.
2. `PLElement.GET()` on a non-2xx response references an undefined variable
   and throws `ReferenceError` instead of the intended `Response`.
3. Part headers are split on `": "`; values containing `": "` are truncated.
4. `If-Match` is sent on GET.
5. The Droppable fallback DELETE does not escape the id.
6. After `--remove-instance` the element is detached before `DELETE()`
   computes its selector; an instance without an `id` would send an empty
   selector (instances created by the client always have ids).
7. A 412 on a commit PUT is not retried or surfaced.
8. After one MOVE 405, MOVE is never retried on that page.
- Evidence: client-source. Source: BJ-PRIM:124, 133-178, 265-268, 407-416;
  BJ-MAIN:877-887, 932-943; BJ-COMP:761-799, 929-944. Confidence: high.


---

# Part C: contradictions, dependencies, notes, probes, cases

## C1. Contradictions between sources and compatibility decisions

| # | Topic | Claims | Decision |
|---|---|---|---|
| C-JS-1 | Name of the allowed import | JSS calls it "`pagelove:schema`" (§Supported language features, §Errors) and a "`pagelove-schema:` import" (§Explaining why); its only concrete example is a schema URL with `with { type: "https://pagelove.org/Schema" }`. | Accept only the URL + `type` form (R-JS-9). A literal `pagelove:schema` specifier is `import-not-allowed` (low-confidence case). |
| C-JS-2 | Validator result | JSS: "Truthy to accept". PROP §Error cases: anything other than `true` is a 422. | JavaScript truthiness for JavaScript validators (R-JS-14). Disputed case `javascript.slots.validate-strictly-true`. |
| C-JS-3 | Composition-dispatch arguments | JSS §Method bodies: "arguments bind by name ... (rather than positionally)". ME: positional, in declared order, values from the matching attributes. | ME (R-JS-18). |
| C-JS-4 | Scope of Context mutations | ME (three places): host subtree only. STAMP: "propagates a method's Context mutations to later dispatches on the same page". | Subtree only. Disputed case `javascript.methods.context-visible-to-later-siblings`. |
| C-JS-5 | Order of prefixed attributes | ME: "not dispatched in the order they're written", `*:template` last. SEB/JEB: declaration order, `r:` first. | `r:` first, then source order, `*:template` last (R-JS-35). |
| C-JS-6 | Sandbox globals | JSS: "the only global the sandbox defines is `DOMException`". DOM pages: `DOMParser`, `XMLSerializer`, `Node` and node classes; JEB: `crypto.subtle`. | Provide DOM globals and minimal `crypto` + encoders (R-JS-39). Probe P-JS-5. |
| C-JS-7 | Failure envelope | JSS: every failure is a BindingFailure inside the SchemaViolation envelope. ME/JEB/TRG: composition and trigger failures are HTTP 500 HTML Microdata errors. | SchemaViolation only for schema-write validation; Error document elsewhere, still carrying the BindingFailure (R-JS-52). |
| C-JS-8 | Compile failure of resolvers | RES: a resolver that fails to compile is skipped. JSS: `parse` is a rendered failure variant. | Modeling's decision (skip for `@read`/`@write`; 422 for `@validate`); `parse` reported where the slot is not skipped (R-JS-52). |
| C-JS-9 | Budget exhaustion status | JSS: failures render inside SchemaViolation (a 422 for validators). AuthorizationRule/ME: budget failures are 503; WebDAV: 507. | 503 (public) / 507 (WebDAV) with the context's envelope (R-JS-57). Cases accept 422/500/503 and assert the variant. |
| C-JS-10 | Constructing nodes on a read-only `document` | DOM §Availability: always allowed (createElement and `cloneNode(true)` examples). DDOC: factories are mutation-guarded and throw. DNODE: `cloneNode` rejected on a read-only document. ME: build return fragments in a fresh DOMParser document. | Allowed (R-JS-62). Disputed case `javascript.dom.readonly-factories-throw`. |
| C-JS-11 | `constructor.name` of HTML elements | DOM §Interfaces: HTML elements are `HTMLElement` "and `htmlEl.constructor.name` is `"HTMLElement"`"; the next paragraph: per-tag interfaces (`"HTMLAnchorElement"`). | Per-tag interface when one exists (R-JS-64). |
| C-JS-12 | `id` / `className` assignment | DELEM lists them as "read-only accessors"; browsers allow assignment. | Read-only (documented); strict-mode assignment throws `TypeError` (R-JS-68). Probe P-JS-13. |
| C-JS-13 | Boolean reflection of falsy non-booleans | DELEM: "for a falsy boolean, removes". Per-interface pages: "writing `false` removes it, any other value sets it present". | Only `false`/`null`/`undefined` remove (R-JS-78). Probe P-JS-15. |
| C-JS-14 | `textarea.value` | DOM §Divergences and DELEM name `textarea.value` as reflecting the content attribute; H:HTMLTextAreaElement lists no `value`. | Provide it (R-JS-78). Disputed case `javascript.dom.textarea-value-absent`. |
| C-JS-15 | `nodeName` of namespaced elements | DNODE: "Element = uppercased tag". DELEM: `tagName` verbatim for `createElementNS`. | `nodeName` equals `tagName` (verbatim for `createElementNS`), as in browsers (inferred; no case). |
| C-JS-16 | Async trigger/processor code | TRG describes a JavaScript action as "a synchronous `export default function(ctx)`". JSS/ME: async defaults and methods settle. | Settle promises everywhere (R-JS-8). Probe P-JS-9. |
| C-JS-17 | `this` in composition dispatch | JSS: "the host element ... (read-only)". ME: "the dispatched element (the host element), as a microdata object". | A read-only Element handle (R-JS-18). Probe P-JS-12. |
| C-JS-18 | Documented method-element responses | ME §JavaScript example shows the 206 body as bare text ("hello from JavaScript"), while its own result table says an element is spliced. | The element is spliced; the response contains `<p>hello from JavaScript</p>` inside `<main>` (case asserts the element). |
| C-JS-19 | Parameter markup | ME §Passing arguments and METH §Parameters nest `<li itemprop="parameter">` directly inside the Method `<li>`; an HTML5 parser closes the Method `<li>` there, making the parameter a sibling (so the Method has no parameters). JSS uses `<div itemprop="parameter">`. | pagelike parses per HTML5 (no special case); cases use `<div>`. Probe P-JS-17 checks whether PageLove's documented `<li>` form works. |
| C-JS-20 | Client page URL / auto-start timing | JSIDX/SSEC: `window.location.href`; PLC: auto-start after `ready`. Code: fragment stripped; also waits for `DOMContentLoaded`. | Follow the code (no server impact). |
| C-JS-21 | POST ETag bookkeeping | PRIM: "the parent's previous ETag is restored and the response ETag is assigned to the new child". Code: Content-Location rule (RFC 9110 §8.8.3). | Code; server sends no Content-Location on selector POST, so the POST ETag is the parent's (R-JS-108). |
| C-JS-22 | SSE methods | SSEC: POST/PUT/DELETE; `parse` returns six fields. Code: also MOVE, `destination`, `placement`. | Code (R-JS-110). |
| C-JS-23 | Instance discovery selector | SIH omits `:not([data-for])`. | Code (client-only). |
| C-JS-24 | Mixins | WC: three built-in mixins. Code: also Droppable, Sortable, render stamps, custom states. | Code (R-JS-116; server impact is MOVE). |

Contradictions about schema semantics (order of schema-level `@validate`
vs group constraints, validator inheritance, `@write` failure status) are owned
by modeling (`docs/spec/modeling.md` §19 C1, C2, C3, C17) and are not
repeated here.

## C2. Cross-area dependencies

- **modeling** (`docs/spec/modeling.md`): slot semantics and the write
  pipeline (R-MOD-7, R-MOD-15, R-MOD-27..29, R-MOD-44..53), the schema registry
  that resolves imports (R-MOD-4), the SchemaViolation envelope and check names
  (R-MOD-70..75). This spec defines only how JavaScript is called and how its
  results and failures are converted.
- **composition** (`docs/spec/composing.md`): method-element matching,
  overloads, `doesNotUnderstand`, attribute-form ordering and replacement, the
  `r:`/`e:`/`j:` binding namespaces, stamps, Context scoping, output stripping
  of binding attributes, the 500-dispatch budget, and the fragment parsing of
  returned elements (R-JS-60).
- **sessel** (`docs/spec/sessel.md`): cross-language calls (a JavaScript method
  called from Sessel and vice versa), Context sharing, value types (the dombase
  side of marshalling, R-JS-40/41), `new Type {}` construction semantics that
  JavaScript-returned instances reuse (R-JS-20), and the shared budget.
- **liquid** (`docs/spec/liquid.md`): `j:` values rendered in templates
  (numbers stay numbers), and the shared budget.
- **reactions** (`docs/spec/reacting.md`): trigger/processor selection and
  ordering, `when`/`otherwise` semantics, the HTTPResponse object shared with
  Sessel, HttpRequest dispatch; this spec owns the JavaScript `ctx` shape.
- **reading-writing**: Request Document and `request` fields (R-RW-135..137),
  caching taint (R-RW-103), error document shapes (R-RW-130), conditional
  requests including `If-Match` on GET (R-RW-140), POST response and ETag rule
  (R-RW-72/73), 416 for missing elements.
- **protocol**: OPTIONS multipart contract for beta-js (R-PROTO-4, 16..21),
  QUERY media types (R-PROTO-41..43), Sessel QUERY authorization (R-PROTO-74),
  MOVE (R-PROTO-90..106).
- **sse**: mutation article serialization and order (R-SSE-7, R-SSE-8), event
  names, echo suppression (R-SSE-43).
- **selectors / dom** (ADR 0003): the same selector engine and HTML
  serializer back the JavaScript DOM; CSS escapes, `:nth-child` on the root,
  HTML-parsed foreign content without namespaces (R-JS-64).
- **identity / permissions**: `request.auth` claims (R-JS-34), session echo
  suppression used by beta-js (R-JS-110).
- **harness**: `$${` escapes JavaScript template literals inside case bodies;
  `${P}` appears inside schema type URLs.

## C3. Implementation notes (non-normative)

1. **Engine and isolation** follow ADR 0001: QuickJS (`modernc.org/quickjs`) in
   worker processes; fresh VM per evaluation; engine limits give the exact
   variants; the parent enforces hard deadlines. The module loader accepts only
   schema URLs present in the request's schema descriptors and checks the
   `type` import attribute with a lexical pre-scan (the loader does not receive
   attributes).
2. **One call envelope** `{slot, source, this, args, schemas, document?,
   writable, budget}` covers every slot; the host adapts results per slot
   (A3). Keep the slot table in one place (`internal/jsrt/slots.go`) so each
   R-JS-10..23 row is one entry.
3. **Values** cross the boundary in a tagged encoding: `null`, booleans,
   `{"$int": "…"}` for integers (to keep 64-bit values exact), floats, strings,
   arrays, objects, `{"$element": "<outer HTML>", "ro": true}`,
   `{"$instance": {...}}`, `{"$date": "…"}`. Depth and cycle checks for
   `return-type` run in the worker before encoding.
4. **The DOM** lives in the worker (ADR 0001 §DOM across the boundary): the
   worker parses the shipped HTML with `internal/dom`, marks the shipped tree
   read-only, and exposes classes as a JavaScript prelude over a handle table.
   Reflection tables (R-JS-79) are data (one Go/JS table of
   `{iface, prop, attr, kind, default, min, max, keywords, missing, invalid}`),
   generated once and shared by the prelude and tests.
5. **`request`** is a JavaScript object with accessor properties; per-user
   accessors call back a taint flag that the host turns into `Cache-Control:
   private` (R-JS-34).
6. **Context** for method dispatch is copied into the worker per dispatch; the
   worker returns a diff (assignments), which the composer applies to the host
   subtree scope only (R-JS-18).
7. **Budget accounting**: charge wall-clock evaluation time and the worker's
   memory high-water to the request budget; count DOM calls and add them to the
   composition budget as time slices; expose the totals in
   `X-Budget-Consumed-*` (R-JS-59).
8. **HTTPResponse** detection happens in the worker (the thrown value is
   inspected before it is stringified) and is reported as a distinct outcome,
   so only the slots listed in R-JS-53 honor it.
9. **Tracing**: emit `dombase_js.evaluate` spans from the host with the
   attributes of R-JS-54.
10. **beta-js compatibility** is mostly other areas' work; the checklist is
    R-JS-102..110. Serve pages unmodified outside composition regions so
    client-generated selectors match (R-JS-104).
11. **Status at the time of writing** (local run, 2026-09-28): 8 of the 146
    `javascript` cases pass against local pagelike (7 client-contract cases and
    the QUERY 415); the rest need `internal/jsrt`. The client case
    `javascript.client.head-etag-then-conditional-writes` fails only on the
    conditional GET: a stale `If-Match` on a selector GET must be 412
    (reading-writing R-RW-140), and the engine currently answers 206.

## C4. Open questions for live probing

Each probe is a minimal sequence against a disposable host under a probe
prefix `/_probe/<id>/` with an AuthorizationRule granting anonymous GET/PUT
there (and QUERY where marked). "PUT page" means `PUT` with
`Content-Type: text/html`. Record status, all headers, and body.

- **P-JS-1 (composition failure shape, C-JS-7, R-JS-36, R-JS-52).** PUT
  `/j.html` with `<html xmlns:j="https://pagelove.org/Binding/JavaScript"><body><main><div j:v="1 +"></div></main></body></html>`;
  `GET /j.html`. Repeat with `j:v="(()=>{throw new Error('x')})()"` and with a
  method element whose JavaScript implementation throws. Settles: status (500?),
  error itemtype, presence of a BindingFailure item and its nesting property.
- **P-JS-2 (default failure, R-JS-11, R-JS-52).** PUT a schema page whose
  property `d` has a JavaScript `default` `export default (`; PUT an instance
  without `d`. Repeat with `export default () => { throw new Error('x') }` and
  `export default 42`. Settles: status (422?), envelope, `check` name,
  `failure` nesting.
- **P-JS-3 (budget exhaustion, C-JS-9, R-JS-57).** PUT a schema whose
  property `v` has `@validate` `export default () => { for(;;){} }`; PUT an
  instance; record status, variant, `X-Budget-*`. Repeat with the array bomb of
  `javascript.slots.out-of-memory`, and via WebDAV `PUT` of the same instance
  (expect 507?).
- **P-JS-4 (budget headers, R-JS-59).** GET a static page, then a page with
  `<div j:x="(()=>{const t=Date.now(); while(Date.now()-t<200){} return 1})()">`;
  compare `X-Budget-Consumed-Time` (units: ms vs µs) and `-Ops`.
- **P-JS-5 (globals, C-JS-6, R-JS-38, R-JS-39).** PUT a page with
  `<div j:g="[typeof console, typeof crypto, typeof TextEncoder, typeof URL, typeof structuredClone, typeof queueMicrotask, typeof atob, typeof DOMParser, typeof document, typeof Node].join('|')"><p:stamp g></p:stamp></div>`
  (with `xmlns:p`), GET `Range: selector=main`. Repeat the same list inside a
  JavaScript method element (module context).
- **P-JS-6 (Intl, Temporal, Date results, R-JS-37, R-JS-42).** Same as P-JS-5
  with `typeof Intl, typeof Temporal`; plus a schema `default`
  `export default () => new Date()` and GET the stored instance via WebDAV
  (what did the empty Map become?).
- **P-JS-7 (stack depth, R-JS-56).** Validators `f(n)` recursion for
  n = 500, 1000, 2000, 5000, 10000 (`export default (v) => f(N) === N`); find
  the largest accepted n; calibrate ADR 0001's frame limit to it.
- **P-JS-8 (`default` this and document, R-JS-11).** A default returning
  `JSON.stringify(this) + '|' + document.documentElement.tagName + '|' + document.querySelectorAll('*').length`
  for an instance with two other declared properties (one `0..n` with two
  values); GET the stored value via WebDAV.
- **P-JS-9 (async triggers, C-JS-16).** Trigger on `/_probe/9/*` PUT with
  `when` `export default async () => false` and an action throwing 418. PUT
  `/_probe/9/a.html`: 418 means the Promise object was treated as truthy.
- **P-JS-10 (HTTPResponse in other slots, R-JS-53).** Throw
  `{schema_url:'https://pagelove.org/HTTPResponse',status:409,message:'m'}` from:
  a schema-level `@validate`; a `default`; a method element implementation; a
  `j:` expression; a trigger `when`. Record which answer 409.
- **P-JS-11 (absent parameter, R-JS-18).** Method `pair(first, second)` with
  JavaScript `function(p,q){return String(q)}`; page `<t:pair first="A">`:
  `null` (dispatched) vs 500 (no overload matched).
- **P-JS-12 (`this` in dispatch, C-JS-17).** Method returning
  `typeof this + '|' + (this && this.constructor && this.constructor.name) + '|' + JSON.stringify(this)`
  for `<t:who data-k="v">`.
- **P-JS-13 (unlisted members, C-JS-12, C-JS-15).** Method returning the
  `typeof` list of `javascript.dom.unlisted-members-absent`, plus the result of
  `p.id = 'b'` in a try/catch, plus `createElementNS('urn:x','Ab').nodeName`.
- **P-JS-14 (NodeList extras, R-JS-72).** Method returning
  `typeof list[0] + '|' + typeof list.forEach` for a `querySelectorAll` result.
- **P-JS-15 (boolean reflection, C-JS-13).** Method: `i.required = 0;
  i.hasAttribute('required')` and `i.required = ''`.
- **P-JS-16 (construction on read-only, C-JS-10).** Run the two cases
  `javascript.dom.readonly-factories-allowed` / `-throw` and
  `javascript.dom.readonly-move-throws-clone-writable`.
- **P-JS-17 (parameter markup, C-JS-19).** Replay the ME §Passing arguments
  example verbatim (Sessel `greet` with `<li itemprop="parameter">` nested in the
  Method `<li>`); `GET` it: "Hello, Ada!" means PageLove's parser or
  microdata reader keeps the nesting.
- **P-JS-18 (Context propagation, C-JS-4).** Run
  `javascript.methods.context-scoped-to-host-subtree` and the disputed sibling
  case.
- **P-JS-19 (request/ctx shapes, R-JS-21, R-JS-24, R-JS-34).** Trigger action
  throwing `{…, status: 418, message: JSON.stringify({keys: Object.keys(ctx.request), headers: Object.keys(ctx.request.headers), auth: ctx.request.auth === undefined ? 'undef' : typeof ctx.request.auth})}`
  for an anonymous PUT with header `X-Probe: 1`; the same for a `j:` using
  `request`.
- **P-JS-20 (pipeline value form, R-JS-12..14).** Property `tags` with
  explicit `0..n` and a `@validate`
  `export default (v) => { throw {schema_url:'https://pagelove.org/HTTPResponse', status:418, message: JSON.stringify(v)} }`;
  PUT an instance with two `tags` values; repeat for a `0..1` property and for
  a property with no cardinality.
- **P-JS-21 (import forms, C-JS-1, R-JS-9).** `@validate` modules with: a
  named schema import; `assert { type: … }`; `import S from 'pagelove:schema'`;
  `import M from "https://pagelove.org/Map" with {type: "https://pagelove.org/Schema"}`.
  Record variant for each.
- **P-JS-22 (textarea value, C-JS-14).** Run both textarea cases.
- **P-JS-23 (fresh context per j:, R-JS-31).** `<div j:a="(globalThis.k = 5, 1)" j:b="typeof globalThis.k">`
  then stamp `b`: `"undefined"` means a fresh context per attribute.
- **P-JS-24 (DOM in j:, R-JS-31, R-JS-63).** `<div j:d="typeof document + '|' + (typeof document === 'object' ? document.querySelectorAll('p').length : -1)"><p>x</p></div>`
  plus a `<p>` outside the div.
- **P-JS-25 (small DOM divergences).** Run
  `javascript.dom.character-data-length-code-points`,
  `javascript.dom.adoption-deep-copies`, `javascript.dom.xmlserializer`.

## C5. Case index

Case files: `harness/cases/javascript/{jbind,globals,methods,dom,slots,classes,reactions,client}.yaml`
(146 cases; generated by a script but safe to edit by hand). Every server-JS
case is tagged `requires: [server-js]` (plus `liquid`, `sessel`,
`multi-actor` where needed); the client-contract cases need no capability.
All cases are `live: true` except the four Sessel-QUERY cases in
`classes.yaml` (`multi-actor`). Four cases are `status: disputed`:
`methods.context-visible-to-later-siblings` (C-JS-4),
`dom.textarea-value-absent` (C-JS-14), `dom.readonly-factories-throw`
(C-JS-10), `slots.validate-strictly-true` (C-JS-2).

Documented examples already pinned by modeling cases (not duplicated here):
JSS "Random coordinate default" (`modeling.defaults.js-dynamic`), "Normalise on
write" (`modeling.resolvers.js-write-trim-lower`), "Format on read"
(`modeling.resolvers.js-read-uppercase`), "Validate at persistence"
(`modeling.validators.property-js`), the postcode `@write` refusal
(`modeling.resolvers.js-write-httpresponse-postcode`), `new HTTPResponse`
being a ReferenceError (`modeling.validators.js-new-httpresponse-is-generic-rejection`),
the schema-level JavaScript validator (`modeling.validators.schema-level-js-this-is-html`),
mixed-language chains (`modeling.resolvers.mixed-language-chain`), and the
`threw`/`shape`/`import-not-allowed`/`unknown-language` variants
(`modeling.errors.binding-failure-*`). Client documentation examples run in a
browser; the HTTP requests they cause are pinned by `javascript.client.*`.

Requirement → cases (ids without the `javascript.` prefix):

| Requirement | Cases |
|---|---|
| R-JS-4 | `query.javascript-body-415` |
| R-JS-5 | `globals.module-strict-mode`, `globals.fresh-context-per-evaluation`, `slots.default-fresh-context`, `slots.validate-strict-mode-referenceerror` |
| R-JS-6 | `slots.default-shape-not-function` |
| R-JS-7 | `globals.module-strict-mode`, `slots.default-arrow-this-undefined` |
| R-JS-8 | `methods.async-body`, `slots.validate-async` |
| R-JS-9 | `slots.import-bare-specifier`, `slots.import-http-url`, `slots.import-pagelove-host`, `slots.import-dynamic`, `slots.import-schema-without-type-attribute`, `slots.import-literal-pagelove-schema`, `slots.import-unknown-schema`, `classes.import-class-behaviour` |
| R-JS-11 | `slots.default-fresh-context`, `slots.default-context-document-html`, `slots.default-timestamp-docs-example`, `slots.default-document-writable`, `classes.default-constructs-instance` |
| R-JS-14 | `slots.validate-docs-example-hostname`, `slots.validate-truthy-accepts`, `slots.validate-async`, `slots.validate-sees-default` |
| R-JS-15 | `slots.schema-validate-this-is-string-context-null` |
| R-JS-16 | `classes.computed-via-query` |
| R-JS-17 | `classes.method-greet-via-query`, `classes.method-instance-receiver-no-document`, `classes.static-method-this-is-class` |
| R-JS-18 | `methods.docs-js-method-element`, `methods.params-positional-by-declaration`, `methods.absent-parameter-is-null`, `methods.async-body`, `methods.document-scoped-to-host`, `methods.readonly-mutation-fails-500`, `methods.this-is-host-element`, `methods.context-scoped-to-host-subtree`, `methods.attribute-form-replaces-host` |
| R-JS-19 | `methods.dnu-element-form`, `methods.dnu-attribute-form` |
| R-JS-21 | `reactions.trigger-docs-when-and-action`, `reactions.trigger-ctx-request-shape`, `reactions.trigger-when-truthiness`, `reactions.trigger-return-value-discarded`, `reactions.trigger-no-pagelove-global` |
| R-JS-22 | `reactions.processor-docs-custom-404`, `reactions.processor-rethrow-with-headers`, `reactions.processor-response-assignment-ignored` |
| R-JS-24 | `reactions.trigger-header-lookup` |
| R-JS-30 | `jbind.wrong-namespace-ignored` |
| R-JS-31 | `jbind.docs-example-liquid`, `jbind.declaration-order`, `jbind.await`, `jbind.returned-promise-adopted` |
| R-JS-32 | `jbind.docs-example-liquid`, `jbind.declaration-order`, `jbind.later-binding-not-in-scope`, `jbind.reserved-word-name-via-context`, `jbind.hyphenated-name-via-context`, `jbind.names-are-lowercased`, `jbind.scope-descendants-not-siblings`, `jbind.interleave-with-sessel`, `jbind.reads-resource-binding` |
| R-JS-33 | `jbind.docs-example-liquid`, `jbind.stamp-value` |
| R-JS-34 | `jbind.request-fields`, `jbind.request-headers-taint-private`, `jbind.naming-request-does-not-taint` |
| R-JS-35 | `jbind.interleave-with-sessel`, `jbind.reads-resource-binding` |
| R-JS-36 | `jbind.later-binding-not-in-scope`, `jbind.parse-error-500`, `jbind.throw-500`, `jbind.reject-500`, `jbind.unknown-identifier-500` |
| R-JS-37 | `globals.ecmascript-builtins`, `globals.nondeterminism-allowed`, `globals.es2020-syntax-in-modules`, `slots.default-timestamp-docs-example` |
| R-JS-38 | `globals.host-io-absent`, `globals.other-host-apis-absent`, `reactions.trigger-no-pagelove-global` |
| R-JS-39 | `globals.dom-classes-present` |
| R-JS-39a | `globals.crypto-subtle-digest` |
| R-JS-40 | `jbind.reads-resource-binding` |
| R-JS-41 | `methods.return-scalars`, `slots.write-returns-function`, `slots.write-returns-cycle` |
| R-JS-51 | `slots.default-parse-failure`, `slots.default-shape-not-function`, `slots.validate-strict-mode-referenceerror`, `slots.write-returns-function`, `slots.import-bare-specifier`, `slots.import-http-url`, `slots.import-pagelove-host`, `slots.import-dynamic`, `slots.import-schema-without-type-attribute`, `slots.import-literal-pagelove-schema`, `slots.import-unknown-schema` |
| R-JS-52 | `methods.readonly-mutation-fails-500`, `methods.return-text-node-500`, `methods.return-document-500`, `methods.return-fragment-500`, `methods.throw-500`, `methods.shape-failure-500`, `slots.default-parse-failure`, `slots.validate-strict-mode-referenceerror`, `slots.write-returns-function` |
| R-JS-53 | `slots.validate-docs-example-hostname`, `slots.validate-plain-object-throw-is-generic`, `slots.validate-httpresponse-itemtype-key`, `reactions.trigger-docs-when-and-action`, `reactions.trigger-plain-object-is-error`, `reactions.trigger-itemtype-marker`, `reactions.trigger-default-status-500`, `reactions.trigger-body-and-headers`, `reactions.processor-docs-custom-404`, `reactions.processor-rethrow-with-headers` |
| R-JS-56 | `slots.timeout`, `slots.out-of-memory`, `slots.stack-overflow-is-threw`, `slots.recursion-depth-500-ok` |
| R-JS-57 | `slots.timeout`, `slots.out-of-memory` |
| R-JS-58 | `methods.composition-budget-503` |
| R-JS-60 | `methods.docs-js-method-element`, `methods.attribute-form-replaces-host`, `methods.dnu-attribute-form`, `methods.return-elements`, `methods.return-scalars`, `methods.return-null-undefined-removes`, `methods.return-text-node-500`, `methods.return-document-500`, `methods.return-fragment-500`, `methods.throw-500`, `methods.shape-failure-500` |
| R-JS-61 | `methods.readonly-mutation-fails-500`, `dom.readonly-boundary`, `dom.readonly-error-message`, `dom.readonly-move-throws-clone-writable`, `slots.default-document-writable` |
| R-JS-62 | `dom.readonly-move-throws-clone-writable`, `dom.readonly-factories-allowed` |
| R-JS-63 | `methods.document-scoped-to-host`, `classes.method-instance-receiver-no-document` |
| R-JS-64 | `dom.instanceof-hierarchy`, `dom.interface-scoping`, `dom.unlisted-members-absent`, `dom.names-and-node-types` |
| R-JS-65 | `dom.namespace-errors`, `dom.tree-operations`, `dom.get-element-by-id` |
| R-JS-66 | `globals.node-constants`, `dom.names-and-node-types`, `dom.mutation-errors`, `dom.tree-operations`, `dom.adoption-deep-copies`, `dom.character-data` |
| R-JS-67 | `dom.character-data`, `dom.character-data-length-code-points` |
| R-JS-68 | `dom.names-and-node-types`, `dom.namespace-errors`, `dom.attributes-ns`, `dom.id-and-classname-read-only` |
| R-JS-69 | `dom.inner-outer-adjacent-html` |
| R-JS-70 | `dom.classlist` |
| R-JS-71 | `dom.mutation-errors`, `dom.tree-operations`, `dom.insertion-conveniences`, `dom.closest-and-matches`, `dom.element-query-scans-whole-document`, `dom.get-element-by-id` |
| R-JS-72 | `dom.nodelist-static-snapshot` |
| R-JS-73 | `dom.mutation-errors`, `dom.namespace-pipe-selectors` |
| R-JS-74 | `methods.docs-js-method-element`, `dom.domparser-ignores-type` |
| R-JS-75 | `dom.xmlserializer` |
| R-JS-76 | `dom.domexception`, `dom.readonly-error-message` |
| R-JS-77 | `dom.namespace-errors`, `dom.mutation-errors`, `dom.inner-outer-adjacent-html` |
| R-JS-78 | `dom.reflect-strings-and-urls`, `dom.reflect-booleans`, `dom.reflect-numbers`, `dom.reflect-enums`, `dom.crossorigin-nullable-enum`, `dom.textarea-value-reflects-attribute` |
| R-JS-79 | `dom.unlisted-members-absent`, `dom.reflect-strings-and-urls`, `dom.reflect-numbers`, `dom.reflect-enums`, `dom.reflect-htmlelement-globals`, `dom.content-editable` |
| R-JS-80 | `dom.element-query-scans-whole-document` |
| R-JS-85 | `classes.import-class-behaviour`, `classes.static-method-this-is-class` |
| R-JS-86 | `classes.import-class-behaviour`, `classes.map-schema-extends-map` |
| R-JS-87 | `classes.default-constructs-instance` |
| R-JS-101 | `reactions.processor-rethrow-with-headers` |
| R-JS-102 | `client.head-etag-then-conditional-writes`, `client.post-response-is-one-node`, `client.fallback-put-plain-content-type` |
| R-JS-104 | `client.generated-root-path-selector`, `client.css-escaped-id-selector`, `client.itemprop-step-selector` |
| R-JS-105 | `client.head-etag-then-conditional-writes` |
| R-JS-106 | `client.fallback-put-plain-content-type` |
| R-JS-107 | `client.head-etag-then-conditional-writes`, `client.post-response-is-one-node`, `client.delete-missing-is-416` |
| R-JS-108 | `client.head-etag-then-conditional-writes`, `client.post-response-is-one-node` |
| R-JS-113 | `client.create-instance-markup-accepted` |
| R-JS-115 | `client.create-instance-markup-accepted` |
