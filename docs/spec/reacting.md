# Reacting to changes (behavioral spec)

Status: draft. Snapshot of sources: 2026-09-28. Area code: `REACT`. Harness
cases: `harness/cases/reacting/*.yaml`. Implementation package:
`internal/reactions` (plan §Packages).

This spec covers what pagelike must reproduce for PageLove's automation
primitives:

- **Triggers**, which run before core request processing;
- **Processors**, which run after core processing, before the response;
- **Outbound HTTP requests** (`HttpRequest` actions), which are queued by
  either and sent after the response;
- **Chain termination** by throwing an `HTTPResponse`;
- **Writes from actions** (`Pagelove.PUT` / `Pagelove.DELETE`), including
  in-flight body transformation;
- **TransitionConstraint**, a declarative state machine that validates writes;
- **TransitionHandler**, a notification sent when a permitted state change
  commits, and the `Transition` document it delivers.

Out of scope: the Sessel and JavaScript runtimes themselves (only their
contract with this area is here), schema validation (modeling), authorization
rule evaluation (permissions-identity), the SSE event format (sse), and the
WebDAV plane (protocol). §15 lists these dependencies.

## 0. Conventions

Each requirement has an id `R-REACT-<n>` and these fields:

- **Behavior**: what pagelike must do. "MUST" or "SHOULD" appears only where
  the choice matters for interoperability.
- **Evidence**: `documented` | `client-source` | `demo-source` | `inferred`.
  "pagelike decision" marks a choice made where PageLove behavior is unknown or
  sources disagree. Every decision is listed again in §16 and has a probe in §17.
- **Source**: a doc page and section (tag below), or `repo/path@commit:line`.
- **Confidence**: high / medium / low.
- **Edge cases**, **Status/errors**, **Cross-area**, **Cases** where relevant.

Numbers R-REACT-16, 20, 21 and 28 are unassigned (reserved). Ids are stable
and are not renumbered.

### 0.1 Source tags

| Tag | Source |
|---|---|
| D-IDX | docs `reference/reacting-to-changes` (index page) |
| D-TRIG | docs `reference/reacting-to-changes/Trigger` |
| D-PROC | docs `reference/reacting-to-changes/Processor` |
| D-HTTP | docs `reference/reacting-to-changes/HTTPRequest` ("Outbound HTTP requests") |
| D-TC | docs `reference/reacting-to-changes/TransitionConstraint` |
| D-TH | docs `reference/reacting-to-changes/TransitionHandler` |
| D-SM | docs `recipes/declaring-a-state-machine` |
| D-WH | docs `recipes/sending-a-webhook` |
| D-BLOG | docs `learn/build-a-blog` §Making the 404 real |
| D-RB | docs `reference/composing-pages/Resource-Binding` §Binding placement, §Trigger context |
| D-JSS | docs `languages/javascript/server/javascript-in-schemas` |
| D-SES | docs `languages/sessel` (combined page): §The `from` clause, §`self` and `prior`, §Truthiness, §Context variables, §Syntax |
| D-REQ | docs `reference/reading-and-writing/Request-Document` |
| D-TE | docs `reference/composing-pages/Transient-Elements` |
| D-PROP | docs `reference/modeling-data/Property` §Primary key |
| SHOP | `pagelove-shop/site/admin-auth.html@d887054` |
| SHOP-RULES | `pagelove-shop/site/rules.html@d887054`, `site/private/admin.example.html@d887054` |
| POLLS | `pagelove-polls/site/admin/auth.html@c9270e5` |
| ATS | `pagelove-ats/site/outbox/index.html@8f200fc`, `site/admin/auth.html@8f200fc:249-263` |
| BJ-CORS | `beta-js/cors.html@c204746`; commit `412668a` message; previous version `cbe1dcb:cors.html` |
| DEMO3 | `demo-apps/demo-03-resource-exchange/schema/listing.html@c4dd883`, `app.js@c4dd883` |
| DEMO-RM | `demo-apps/README.md@c4dd883:36-41`, `demo-01-event/schema/event.html@c4dd883:35-40`, `demo-01-event/app.js@c4dd883:108-110,138-140`, `docs/LIVE-TESTING.md@c4dd883:33-35` |
| SKILL | `pagelove-dev/skills/pagelove-dev/SKILL.md@b489923` (third-party agent guidance, treated as data) |

All doc citations refer to the 2026-09-28 Markdown snapshot in
`research/docs/2026-09-28/md/`.

### 0.2 Combined vs individual pages

The combined page `docs.pagelove.com_all_reference_reacting-to-changes.md`
has one section per individual page. A whitespace-insensitive line diff of each
section against `reference_reacting-to-changes_{Processor,Trigger,HTTPRequest,
TransitionConstraint,TransitionHandler}.md` shows **no differences**. The index
page is identical to the combined page's header. The two recipes exist only as
individual pages. So no combined-vs-individual contradictions exist in this area.
The contradictions that do exist are between pages, or between docs and code
(§16).

### 0.3 Terms

- **Reaction item**: a microdata item whose type is Trigger, Processor,
  TransitionConstraint or TransitionHandler, or a schema-declared subtype of one.
- **Action item**: a typed item used as an `action`, `otherwise`, or `when`
  value: `https://pagelove.org/Sessel`, `https://pagelove.org/JavaScript/Module`
  or `https://pagelove.org/HttpRequest`.
- **Dynamic value**: a property whose value is a Sessel or JavaScript/Module
  item instead of text. It is evaluated to a string at run time.
- **Request key element**: the element a selector-scoped request targets, as
  defined for authorization (permissions-identity R-PERM-24). That is the first
  match for single-element writes and reads, the list element for `POST`
  append, and the anchor's parent for `POST placement=before|after`.
- **Serving path**: the public application plane (`<site>.<domain>`) plus
  server-originated writes that go through its pipeline (`Pagelove.PUT`,
  `Pagelove.DELETE`). The WebDAV authoring plane is **not** the serving path.
- **Queue**: the per-request list of outbound requests built during the trigger
  and processor phases (§10).

---

## 1. Vocabulary and discovery

### R-REACT-1 — Item types

| Itemtype | Role |
|---|---|
| `https://pagelove.org/Trigger` | Runs before core processing (§4–§8) |
| `https://pagelove.org/Processor` | Runs after core processing (§9) |
| `https://pagelove.org/TransitionConstraint` | Declares one permitted step of a state machine (§11) |
| `https://pagelove.org/TransitionHandler` | Notifies an endpoint when a permitted change commits (§12) |
| `https://pagelove.org/HttpRequest` | Outbound HTTP action (§10). `https://pagelove.org/HTTPRequest` is accepted as an alias (R-REACT-48) |
| `https://pagelove.org/Sessel` | Sessel code item. Source is its `source` property, a `<script type="text/sessel">` |
| `https://pagelove.org/JavaScript/Module` | JavaScript code item. Source is its `source` property, a `<script type="module">` |
| `https://pagelove.org/Pair` | `key`/`value` pair used for headers (§6, §10) |
| `https://pagelove.org/HTTPResponse` | Thrown response object (§6) |
| `https://pagelove.org/Context` | Name imported with `@schema Context url(…)` to reach `Context.request` / `Context.response` |
| `https://pagelove.org/1.0` | Name imported with `@schema Pagelove url(…)` to reach `Pagelove.PUT` / `Pagelove.DELETE` |
| `https://pagelove.org/Transition` | Payload document of a handler delivery (R-REACT-81) |
| `https://pagelove.org/ConstraintViolation`, `https://pagelove.org/Violation` | 422 envelope for transition violations (R-REACT-72) |

- **Evidence:** documented. **Source:** D-TRIG, D-PROC, D-HTTP, D-TC, D-TH
  (all examples). **Confidence:** high.
- **Edge cases:** Itemtype comparison is exact and case-sensitive, except for
  the `HttpRequest`/`HTTPRequest` alias. An element whose `itemtype` token list
  contains a reaction type among other tokens is a reaction item (inferred;
  same rule as R-PERM-1).

### R-REACT-2 — Discovery is host-wide and automatic
- **Behavior:** Every element with `itemscope` in any stored document of the
  site (HTML, and XML documents parsed as DOM) whose `itemtype` token list
  contains a reaction type is a reaction item. Where it lives does not limit
  what it governs. Its filters and selectors do that. Items may be hidden
  (`hidden`), in any element (`div`, `tr`, `section`), and in the same document
  they govern or a dedicated one. Items are read from **stored** markup, never
  from composed output. A reaction item in a `<template>` element's content is
  not discovered (template contents are not part of the document tree).
  Schema-declared **subtypes** count too: if a `Schema` on the host declares a
  type whose `parent` chain reaches one of the four reaction types, items of
  that subtype are discovered and behave exactly like the base type
  (modeling R-MOD-10 gives the parent chain).
- **Evidence:** documented. **Source:** D-TRIG intro ("Any document on the host
  can contain trigger definitions"; inheritance-aware discovery); D-PROC intro;
  D-TC intro; D-TH intro; D-SM ("Store them in any document on the host");
  BJ-CORS:50 (processor in `/cors.html`); SHOP:19,40 (triggers in
  `/admin-auth.html`); POLLS:99,130 (triggers in a rules page styled as a
  table). **Confidence:** high (anywhere, subtypes), medium (stored-only,
  templates).
- **Cross-area:** plan §Read path (system-item cache keyed by write generation);
  modeling (subtype map); permissions R-PERM-1 (same discovery rule for rules).
- **Cases:** `reacting.discovery.*`.

### R-REACT-3 — Reading properties: static and dynamic values
- **Behavior:** Properties are collected with the HTML microdata algorithm
  (cross-area microdata). A property value is one of:
  1. **Static text**: the microdata value of the property element (`content`
     of `<meta>`, text content of `<span>`/`<td>`/`<code>`, and so on). Filter
     values (`resource`, `method`, `selector`, `status`, `retry`) are trimmed of
     ASCII whitespace, and values that are empty after trimming are dropped.
     State values (`from`, `to`, `becomes`) are **not** trimmed and may be the
     empty string (R-REACT-63).
  2. **Dynamic**: the property element is itself an item of type
     `https://pagelove.org/Sessel` or `https://pagelove.org/JavaScript/Module`.
     The language is chosen **by `itemtype`**, not by the `<script type>`. Its
     source is the text of its `source` property (a `<script>` element).
- Which properties may be dynamic:

| Owner | Property | Static | Sessel | JS |
|---|---|---|---|---|
| Trigger, Processor | `resource`, `method`, `selector`, `status` | yes | no | no |
| Trigger, Processor | `when` | no (a text `when` is malformed, R-REACT-17) | yes | yes |
| Trigger, Processor | `action`, `otherwise` | no (must be an action item) | yes | yes |
| HttpRequest in Trigger/Processor | `url`, `method`, `content-type`, `body`, Pair `value` | yes | yes | yes |
| HttpRequest in Trigger/Processor | Pair `key`, single-line `header` | yes | not documented (pagelike: static only) | same |
| HttpRequest anywhere | `retry` | yes | no ("a plain number") | no |
| TransitionConstraint | all | yes | no | no |
| TransitionHandler | `when` | no | yes | no (a JS gate is malformed, R-REACT-79) |
| TransitionHandler's HttpRequest | `url`, `method`, header values | yes | no (an expression disables the handler, R-REACT-80) | no (same) |

- **Evidence:** documented. **Source:** D-TRIG §Static vs dynamic properties
  ("Declarative by default, imperative when needed"); D-HTTP §Static vs dynamic
  properties ("The language is selected per property by its itemtype"),
  §Retry; D-TRIG §when ("dispatch is by itemtype, not by the `<script>` tag");
  D-TH §The when gate, §Delivery. **Confidence:** high (table rows cited),
  low (static-only Pair `key`, trimming).
- **Edge cases:** A dynamic `method` or `content-type` is covered by "any
  property". A dynamic value that evaluates to `null` counts as absent. Any
  other non-string result is converted to its string form. Sessel numbers use
  their literal form, and a JS value uses `String(v)` (inferred, low).

### R-REACT-4 — Malformed items are skipped, never fatal
- **Behavior:** A reaction item that cannot be interpreted is ignored, and the
  request continues as if it did not exist:
  - a Trigger/Processor with neither an `action` nor an `otherwise` value;
  - a Trigger/Processor whose `when` is present but is not a Sessel or
    JavaScript/Module item, or has no `source` (pagelike decision, the literal
    reading of "skipped". Note that skipping a guard makes the guard fail
    **open**, unlike TransitionHandler gates, which fail closed, R-REACT-79);
  - a TransitionConstraint missing `selector` or `property`, or declaring
    neither `from` nor `to` (R-REACT-62);
  - an action item whose itemtype is not one of the three action types. Only
    that action is dropped, and the rest of the item still runs (pagelike
    decision);
  - an `HttpRequest` without a `url` (only that action is dropped; pagelike
    decision).
  A selector that fails to parse (in `selector`) makes that item never match
  (inferred, same as R-PERM-29). Malformed items MUST NOT produce an error
  response or block writes.
- **Evidence:** documented (skip rule, "a malformed rule never blocks writes"),
  pagelike decision (granularity). **Source:** D-TRIG §Error handling
  ("Malformed Microdata … it is skipped. It does not break the request");
  D-TRIG §action ("a trigger with neither is skipped"); D-TC §Taking effect.
  **Confidence:** high (whole-item skip), low (action-level granularity).
- **Cases:** `reacting.discovery.no-action-skipped`,
  `reacting.gates.malformed-when-skips-trigger`,
  `reacting.tc.missing-property-ignored`.

### R-REACT-4a — Serving-path writes check the platform schema of reaction items
- **Behavior (live 2026-09-29):** PageLove validates reaction items against its
  platform schemas when a **serving-path** write stores them, and refuses the
  write with **422** (`https://pagelove.org/SchemaViolation`). pagelike does the
  same (`internal/reactions/platform.go`):
  - `Handler` (Trigger, Processor, TransitionHandler): `action` exactly one
    value (`[https://pagelove.org/Handler].action: cardinality 1..1 violated:
    expected exactly 1 value, found N`); `when` at most one
    (`…].when: cardinality 0..1 violated: expected at most 1 value, found N`);
    `otherwise` is unrestricted.
  - `RequestHandler` (Trigger, Processor): each `method` value must be one of
    `GET`, `PUT`, `POST`, `DELETE`, `PATCH`, `MOVE`, `*`, compared
    case-sensitively (`[https://pagelove.org/RequestHandler].method: Value
    "put" is not a valid https://pagelove.org/HTTPMethod (expected one of: …)`).
    `HEAD`, `OPTIONS`, `QUERY` and lower-case spellings are refused.
  A whole-document PUT checks every item; a selector PUT or POST checks the
  items it inserts and the handler items enclosing the change; a selector
  DELETE checks the handler items that enclosed the removed element. Other
  items are not checked, so an item stored invalid over WebDAV (never
  validated) does not block unrelated selector writes, and at run time it
  follows R-REACT-4, 13, 17 and 18 as before. PageLove reports
  enclosing-item violations of selector writes in another envelope
  (`https://dombase.pagelove.team/ns/error/Cardinality`, messages like
  `https://pagelove.org/Handler /action: … (cardinality)`); pagelike uses the
  SchemaViolation envelope for all of them.
- **Evidence:** live-observed. **Source:** `harness/observations/live-2026-09-29/`
  (`reacting-discovery-no-action-skipped`, `reacting-actions-actions-run-in-document`,
  `reacting-filters-method-case-insensitive`) and the probe
  `reacting.discovery.probe-0929.handler-shape`; [decisions](../compat/decisions-2026-09-29/reacting.md).
  **Confidence:** high.
- **Cases:** `reacting.discovery.no-action-skipped`,
  `reacting.actions.actions-run-in-document-order`,
  `reacting.filters.method-case-insensitive`,
  `reacting.gates.several-when-all-must-pass`,
  `reacting.discovery.probe-0929.handler-shape`; stored items: the `-stored`
  siblings (local only).

### R-REACT-5 — When declarations take effect
- **Behavior:** pagelike applies every reaction item from the **next request**
  after the commit that stores it, on both planes. The item set is snapshotted
  once per request at arrival, and both the trigger and processor phases of
  that request use the snapshot. A request that stores or deletes a reaction
  item therefore does not change which reactions run for that same request.
  TransitionConstraints are the exception. They are read from committed state
  when validation runs (R-REACT-71).
  PageLove: constraints written through the serving path bind immediately.
  Constraints edited over WebDAV "take effect within 60 seconds". For
  triggers, processors and handlers written over WebDAV, the delay is not
  documented. pagelike's immediate application satisfies "within 60 seconds".
- **Evidence:** documented (constraints), inferred (others, same-request
  snapshot). **Source:** D-TC §Taking effect; D-SM §The repair path ("One
  timing difference"). **Confidence:** high (constraints), low (snapshot
  rule).
- **Harness note:** live cases install reaction documents with a public-plane
  `PUT` step, never through `site.files` (which the runner uploads over
  WebDAV), so that the 60-second delay cannot make them flaky.
- **Cases:** `reacting.tc.webdav-rules-take-effect-within-60s` (slow),
  `reacting.tc.webdav-rules-immediate-pagelike` (local only),
  `reacting.tc.serving-path-rules-bind-immediately`,
  `reacting.discovery.same-request-snapshot`.

### R-REACT-6 — Where reactions run
- **Behavior:**
  - Triggers and processors run for public-plane requests of **any** method
    (GET, HEAD, PUT, POST, DELETE, MOVE, OPTIONS, QUERY, PATCH and extension
    methods), on HTML documents, XML documents, blobs (`.json`, `.mjs`, images)
    and missing paths. Filters decide whether a given item matches.
  - They **never** run on the WebDAV authoring plane, on the pagelike console,
    or on built-in identity endpoints (login, logout, callback).
  - Processors do not run on SSE subscription responses (`Accept:
    text/event-stream`), because such a response cannot be fully buffered.
    Triggers do run on the subscribing GET (pagelike decision).
  - Side-effect writes made by `Pagelove.PUT`/`Pagelove.DELETE` do **not** run
    triggers or processors themselves. They do run authorization, validation,
    transition constraints and transition handlers (R-REACT-36; pagelike
    decision).
  - TransitionConstraints validate serving-path writes only (R-REACT-67).
    TransitionHandlers fire for serving-path commits only (R-REACT-84).
- **Evidence:** documented (WebDAV and transitions), demo/client-source (blobs:
  ATS trigger on `/outbox/E-*.json`, BJ-CORS processor on `/pagelove.mjs`;
  missing paths: D-PROC quick example on 404), client-source (SKILL:213-215
  "WebDAV success does not test … Trigger, or Processor behavior"), inferred
  (the rest). **Source:** as cited; D-TC §WebDAV bypasses constraints; D-TH
  §WebDAV edits never fire handlers. **Confidence:** high (WebDAV,
  transitions), medium (blobs, missing paths), low (SSE, nested writes,
  built-ins).
- **Cross-area:** protocol R-PROTO-112/113 (WebDAV never composes or
  processes), R-PROTO-40 (QUERY is safe: pagelike runs triggers and processors
  for QUERY like for GET, but a QUERY request never performs writes. A
  `Pagelove.PUT` inside a trigger for a QUERY request is still a side-effect
  write, which the author asked for).

---

## 2. Request lifecycle and ordering

### R-REACT-7 — Documented lifecycle
- **Behavior (as documented):** 1. request arrives; 2. trigger phase (matching
  triggers in order); 3. core processing (GET, PUT, POST, DELETE, …);
  4. processor phase (matching processors in order); 5. async dispatch of
  queued outbound requests; 6. response sent. A trigger that throws an
  HTTP response skips steps 3–4, and the thrown response is returned directly.
  The HTTPRequest page and the webhook recipe both say that queued requests are
  dispatched **after** the response is sent.
- **Evidence:** documented. **Source:** D-TRIG §Request lifecycle; D-PROC
  §Request lifecycle; D-HTTP intro; D-WH §Execution timing. **Confidence:** high.
- **Contradiction:** the numbered list places "async dispatch" (5) before
  "response sent" (6), while the prose says requests are sent after the
  response. Decision (§16 C3): the queue is **frozen** at step 5 and the
  network sends start only after the response has been written (R-REACT-56).
  Nothing a client can observe distinguishes the two readings, except that
  the response is never delayed by an outbound call.

### R-REACT-8 — Normative pipeline for pagelike
For each public-plane request (the steps before 3 belong to other areas):

1. Framing and size checks (413), routing to built-in endpoints, reserved
   namespace (reading-writing, permissions).
2. Identity resolution (session cookie → principal, permissions-identity).
3. **Snapshot** the reaction item set (R-REACT-5). Record the version of the
   target document (used by R-REACT-73).
4. **Authorization** of the request (permissions-identity §8–§11). A denial
   produces the denial response D (401/403). Steps 5–6 are skipped and the
   pipeline continues at step 7 with D (**pagelike decision**, §16 C1).
5. **Trigger phase** (§4–§8). The phase ends in one of three ways:
   - normally, possibly with a transformed body (R-REACT-37). Go to 6;
   - with a thrown `HTTPResponse` T. The response is T. **Skip 6 and 7.**
     Go to 8;
   - with a runtime error E. The response is the error document for E
     (R-REACT-34). **Skip 6 and 7.** Go to 8.
6. **Core processing**: GET/HEAD composition, or the write pipeline (modeling
   R-MOD-15, which includes TransitionConstraints at its step 10, R-REACT-71),
   commit, SSE publication. The result is response C. After a commit,
   TransitionHandler firings are computed and enqueued (§12). They do not
   depend on the response.
7. **Processor phase** over C (or D) (§9). The phase may modify or replace
   the response. A runtime error in a processor replaces the response with an
   error document (R-REACT-34).
8. Send the response.
9. Dispatch the frozen queue (trigger-queued and processor-queued outbound
   requests), and then deliver transition notifications (§10, §12).

- **Evidence:** documented (5→6→7→8→9 ordering, trigger throw skipping 6–7),
  inferred (placement of authorization and snapshot). **Source:** R-REACT-7;
  permissions R-PERM-53 (which leaves the trigger/authorization order open as
  its P-18). **Confidence:** high (documented part), low (authorization before
  triggers, processors on denials).
- **Rationale for C1:** PageLove's docs do not say whether authorization runs
  before triggers. If triggers ran first, an unauthorized request could still
  queue outbound requests. For example, anyone could make the ATS outbox relay
  (ATS:10-32) send e-mail with the stored Postmark token, although the ATS
  rules allow `/outbox/*` writes only to admins (ATS auth.html:249-263).
  Running authorization first is the fail-safe choice. Every known app gate
  (SHOP, POLLS) works under either order, because their rules already allow
  the requests their triggers inspect. The probe P-1 settles which order
  PageLove uses.
- **Cases:** `reacting.lifecycle.authz-denial-before-triggers`,
  `reacting.processor.runs-on-authz-denial`.

### R-REACT-9 — Throws from triggers bypass core and processors
- **Behavior:** When a trigger action (or `when`/`otherwise`) throws a
  recognized `HTTPResponse`: no further trigger runs, even in later documents;
  core processing does not run, so nothing is written and no SSE event is
  published; processors do not run; the thrown response is sent as built by
  R-REACT-31. Side-effect writes already committed stay committed
  (R-REACT-36). Body transformations are discarded (R-REACT-37). Outbound
  requests already queued **are still dispatched** (step 9 is not skipped:
  pagelike decision, §16 C4).
- **Evidence:** documented (bypass, write persistence), inferred (queued
  requests). **Source:** D-TRIG §Request lifecycle ("steps 3--4 are skipped"),
  §Execution order ("Chain termination stops all subsequent triggers,
  including those from later documents"), §Error handling for writes.
  **Confidence:** high / low (queue survives).
- **Cases:** `reacting.order.throw-stops-later-documents`,
  `reacting.processor.not-run-after-trigger-throw`,
  `reacting.outbound.queued-before-throw-still-sent`.

### R-REACT-10 — Processors see every core response
- **Behavior:** The processor phase runs on the response produced by core
  processing, whatever its status: 2xx, 3xx redirects (directory redirect),
  404, 412, 416, 422, 5xx, and, under pagelike's order (R-REACT-8), 401/403
  authorization denials. It does not run on trigger-thrown responses, on
  trigger runtime errors, or on 413 framing rejections (which happen before
  step 3).
- **Evidence:** documented (404 example, `4xx`/`5xx` filters "Match all
  errors"), inferred (the rest). **Source:** D-PROC §Quick example, §status.
  **Confidence:** high (404), low (denials, 413).
- **Cases:** `reacting.processor.custom-404`,
  `reacting.processor.runs-on-authz-denial`.

---

## 3. Trigger and processor filters

### R-REACT-11 — Filter combination
- **Behavior:** A Trigger or Processor **matches** a request when every
  present filter matches. Filters on different properties are AND-ed. Several
  values of one filter are OR-ed. An omitted filter (or one whose values are
  all empty after trimming) matches everything. Filters are checked before
  `when` is evaluated.
- **Evidence:** documented. **Source:** D-TRIG §Properties ("All filter
  properties are optional. Omitting a filter means match all"), §resource
  ("OR'd"). **Confidence:** high.
- **Cases:** `reacting.filters.omitted-filters-match-all`.

### R-REACT-12 — `resource`
- **Behavior:** Each value is a path glob with the **same syntax and matching
  target as AuthorizationRule `resource`** (permissions R-PERM-19, R-PERM-20,
  R-PERM-22). The glob is anchored and matched against the percent-decoded
  request path without query string. For a path ending in `/` it may also match
  the canonical `…/index.html` form. For a parameterized route it is matched
  against the concrete URL. `*` matches any character sequence **including
  `/`**. So `/*` and `*` match every path, and `/blog/*` matches
  `/blog/a/b.html`.
- **Evidence:** documented ("Same syntax as trigger resource"; `*` used alone to
  mean every path in D-PROC §Quick example), inferred (reuse of R-PERM-20).
  **Source:** D-TRIG §resource; D-PROC §resource; D-BLOG (processor
  `/posts/*` matches the concrete route URL `/posts/nonsense.html`).
  **Confidence:** high (OR, simple prefix globs), medium (`*` crossing `/`).
- **Contradiction:** D-WH says `/notes/*` "matches any direct child of
  `/notes/`", which suggests `*` does not cross `/`. The AuthorizationRule docs
  and D-PROC's bare `*` need it to cross. Decision (§16 C5): one glob engine for
  rules and reactions, where `*` crosses `/`
  (`reacting.filters.resource-star-crosses-segments`). The recipe's claim is
  kept as `reacting.filters.resource-star-direct-child-only`, marked
  `disputed`.
- **Edge cases:** `*` without a leading slash matches every path (because `*`
  matches the leading `/`). A glob that does not compile is compared as a
  literal (R-PERM-22).
- **Cases:** `reacting.filters.resource-*`.

### R-REACT-13 — `method`
- **Behavior:** Values are compared with the request method
  case-insensitively. A `GET` value also matches `HEAD` requests. A `HEAD`
  value does not match GET. The value `*` matches every method (inferred from
  the rule syntax). Unknown tokens simply never match. The method seen by
  actions (`Context.request.method`) is the method actually received, so a HEAD
  request matched by `GET` sees `"HEAD"`.
- **Evidence:** documented (case-insensitive, GET implies HEAD). **Source:**
  D-TRIG §method; D-PROC §method ("Same rules"); D-BLOG (`curl -sI`, a HEAD
  request, is affected by a processor declared for `GET`). **Confidence:**
  high (documented), low (`*`).
- **Cases:** `reacting.filters.method-case-insensitive`,
  `reacting.filters.get-implies-head`.

- **Live 2026-09-29:** a serving-path write refuses a method outside the
  HTTPMethod enumeration, lower case included (R-REACT-4a). Case-insensitive
  matching, and `HEAD`/`*` handling, apply to items stored over WebDAV
  (`reacting.filters.method-case-insensitive-stored`).

### R-REACT-14 — `selector` (semantic matching)
- **Behavior:**
  1. If the request carries no selector (no `Range: selector=…`; for example a
     plain `GET /page`, a whole-document PUT, or a document DELETE), the filter
     is **not evaluated** and matches.
  2. Otherwise compute the request key element E (§0.3) in the **stored
     document as it was at request arrival**. The filter matches when E is in
     the set of elements that the filter selector selects in that same
     document. This is element identity, not a comparison of selector strings.
     So a trigger selector `article` matches a request for `#post1` when `#post1`
     is an `<article>`.
  3. If the request has a selector but E does not exist (no match, a missing
     document, or a blob), the filter does **not** match (pagelike decision).
  4. Several `selector` values are OR-ed (inferred).
  5. For processors, E is computed at request arrival too, so a DELETE whose
     element is gone after core processing still matches (pagelike decision).
- **Evidence:** documented (1, 2), pagelike decision (3–5). **Source:** D-TRIG
  §selector ("Both the trigger's selector and the request's selector are
  resolved against the actual document (semantic matching)"); D-PROC
  §selector; D-WH ("narrows the match to requests whose target element has the
  Note itemtype"). **Confidence:** high (1–2), low (3–5).
- **Edge cases:** A POST appending into `#list` has key element `#list`, not
  the appended fragment. So a filter `[itemtype='…/Note']` does **not** match
  a POST that appends a Note into a plain list. D-WH's description of that
  recipe is ambiguous about this (§16 C6, probe P-6).
- **Cases:** `reacting.filters.selector-*`.

- **Live 2026-09-29 (supersedes 2–5):** PageLove does not evaluate the
  `selector` filter of triggers or processors. A selector-filtered item matches
  exactly like one without the filter, for PUT, POST, GET and DELETE, whether
  or not the element or any match exists (probe
  `reacting.filters.probe-0929.selector-scope`). pagelike follows it (adopt-live):
  the filter is parsed and ignored. Decision C6 is moot: the webhook recipe
  fires (`reacting.filters.selector-post-append-matches-fragment`, no longer
  disputed).

### R-REACT-15 — `status` (processors only)
- **Behavior:** Each value is either an exact three-digit code (`404`) or a
  class `Nxx` where N is 1–5 (`4xx`). `x` is also accepted in upper case
  (inferred). The processor matches when the **current** response status
  (after earlier processors in the chain, R-REACT-46) equals a code or falls in
  a class. An empty or omitted filter matches any status. Values of any other
  form never match. If every value is invalid, the processor never runs
  (pagelike decision, fail closed). On a Trigger, `status` is ignored.
- **Evidence:** documented (forms, OR, empty). **Source:** D-PROC §status;
  BJ-CORS:54 (`2xx`); D-BLOG (`200`). **Confidence:** high / low (invalid
  values, triggers).
- **Cases:** `reacting.processor.status-*`.

---

## 4. Gates and actions

### R-REACT-17 — `when`
- **Behavior:** Optional. The value must be a Sessel or JavaScript/Module item
  (R-REACT-3). Evaluate it after the declarative filters match:
  - Sessel: the program's final expression is the result. It is falsy per
    Sessel truthiness: `false`, `null`, `0`, `0.0`, `""`, `[]`, `{}`.
  - JavaScript: call the module's default export with `ctx` (R-REACT-25). The
    return value is converted to a dombase value and tested with the same
    truthiness table. So `undefined` is falsy, and an empty array or object is
    falsy too (pagelike decision, §16 C11). A returned Promise is not awaited.
    It converts to an empty object and is therefore falsy.
  - Truthy: run the `action` values. Falsy: run the `otherwise` values, or do
    nothing if there are none.
  - Absent `when`: run the `action` values. `otherwise` is never evaluated.
  - Several `when` values: all must be truthy (AND; pagelike decision).
  - A `when` that throws an `HTTPResponse` terminates the chain (R-REACT-33).
    Any other error is a runtime error (R-REACT-34).
- **Evidence:** documented (Sessel and JS gates, falsy list "null, false, 0,
  empty string", otherwise semantics). **Source:** D-TRIG §when, §otherwise;
  D-PROC §when; D-SES §Truthiness; D-JSS §Errors (variants).
  **Confidence:** high (documented), low (JS truthiness for `[]`/`{}`, several
  gates).
- **Cases:** `reacting.gates.*`.

- **Live 2026-09-29:** a serving-path write refuses a second `when`
  (R-REACT-4a); the AND rule applies to items stored over WebDAV. PageLove
  treats a `when` that fails at run time (for example an undefined name) as
  falsy and runs `otherwise`; pagelike keeps failing the request (R-REACT-34,
  keep-documented-security: a gate that errors must not fail open).

### R-REACT-18 — `action` and `otherwise`
- **Behavior:** Zero or more action items each. The selected set runs in
  **document order** (tree order of the action items within the reaction item).
  Types:
  - **Sessel**: runs synchronously. Its value is discarded. It may throw
    (R-REACT-29), write (R-REACT-36), and in processors assign to
    `Context.response` (R-REACT-44).
  - **JavaScript/Module**: `export default function(ctx)`. Runs synchronously,
    and its return value is discarded. It may throw an `HTTPResponse`
    (R-REACT-30). It has **no** write provider (R-REACT-41). It cannot mutate
    `ctx.response` (R-REACT-45).
  - **HttpRequest**: its dynamic properties are evaluated **now**, in order,
    with the current context. The resulting request is appended to the queue
    (§10).
  A trigger needs at least one `action` or one `otherwise`. Otherwise it is
  skipped (R-REACT-4). `otherwise` is documented for triggers. pagelike also
  accepts it on processors (inferred superset).
- **Evidence:** documented. **Source:** D-TRIG §action, §otherwise; D-PROC
  §action; D-HTTP intro ("queued during trigger or processor execution").
  **Confidence:** high (types, order), medium (evaluation time of dynamic
  properties), low (processor `otherwise`).
- **Cases:** `reacting.actions.*`, `reacting.gates.otherwise-*`.

- **Live 2026-09-29:** a serving-path write refuses a Trigger or Processor
  without exactly one `action` (R-REACT-4a). A gate with only `otherwise` needs
  the documented no-op action `1`. Several actions run in document order only
  in items stored over WebDAV (`reacting.actions.actions-run-in-document-order-stored`).

### R-REACT-19 — JavaScript module contract
- **Behavior:** The source must be an ES module whose default export is a
  function. It is called with one positional argument `ctx`. `this` is not
  meaningful (pass `undefined`). Imports other than `pagelove:schema` are
  refused (`import-not-allowed`). No host I/O globals exist (`fetch`,
  `setTimeout`, `process`). Failures follow the JS-in-schemas variants
  (`parse`, `shape`, `threw`, `timeout`, `out-of-memory`, `marshal`,
  `return-type`, `import-not-allowed`, `unknown-schema`). Each one is a
  runtime error (R-REACT-34) unless the thrown value is a recognized
  `HTTPResponse`. Evaluation shares the request's budget with Sessel
  (`TransactionBudget`).
- **Evidence:** documented. **Source:** D-TRIG §when ("`this` is not bound to
  anything meaningful"), §action; D-JSS §Sandbox (restricted globals),
  §Resource limits, §Errors. **Confidence:** high.
- **Cross-area:** server-js runtime (`internal/jsrt`).

---

## 5. Context

### R-REACT-22 — Sessel `Context.request`
- **Behavior:** Trigger and processor Sessel code that declares
  `@schema Context url("https://pagelove.org/Context");` can read:

| Expression | Type | Value |
|---|---|---|
| `Context.request.method` | String | Method as received, upper case (`"HEAD"` for HEAD) |
| `Context.request.path` | String | Percent-decoded request path without query (`/`, not `/index.html`, for a root request; the concrete URL for a routed request) |
| `Context.request.headers` | Map | Request headers (R-REACT-23) |
| `Context.request.query` | Map | Query parameters, name → first value, both percent-decoded. Absent parameter → `null` |
| `Context.request.body` | String | Request body decoded as UTF-8 text. `""` when there is none. After a transformation it holds the transformed body (R-REACT-38) |
| `Context.request.rawBody` | String | The body bytes as received, interpreted as UTF-8 without charset conversion. Equal to `body` for UTF-8 requests |
| `Context.request.auth` | Map or null | Not documented for Sessel. pagelike exposes the same object as JS `ctx.request.auth` (inferred superset, permissions R-PERM-74) |

  `Context.response` does not exist during triggers. Reading it is a runtime
  error in pagelike (inferred). The documented statement is only "not
  available".
- **Evidence:** documented (fields), inferred (exact values, `auth`).
  **Source:** D-TRIG §Context; D-PROC §Context. **Confidence:** high (field
  set), low (repeated query parameters, `rawBody` semantics, `auth` in Sessel).
- **Cases:** `reacting.context.sessel-request-fields`.

### R-REACT-23 — Header map keys and lookup
- **Behavior:** `Context.request.headers` (and `ctx.request.headers`) holds
  every request header. Keys are **lower-case** field names. Several
  field lines with the same name are joined with `", "` in received order
  (inferred), except `Cookie`, which is joined with `"; "`. pagelike MUST make
  lookup **case-insensitive**: `headers["Authorization"]` and
  `headers["authorization"]` both return the value. Iteration and
  serialization show lower-case keys. A missing header is `null` in Sessel and
  `undefined` in JS.
- **Evidence:** demo-source (lower-case keys), documented (canonical-case
  examples). **Source:** SHOP:16-18 (comment: over HTTP/2 the canonical-case
  lookup always misses, "the gate would let everyone through while looking
  correct"), SHOP:25,50 and POLLS:107,114,137 (`headers["authorization"]`,
  `headers["range"]`); D-TRIG §when, §Chain termination (`headers["Authorization"]`);
  D-REQ §Shape (header itemprops are lower case). **Confidence:** high (lower-case
  keys work), medium (case-insensitive lookup is a pagelike decision).
- **Contradiction:** the docs index `headers["Authorization"]`. The shop
  reports that this lookup is empty on the live platform over HTTP/2. Decision
  (§16 C2): case-insensitive lookup. It is a superset that keeps both the docs
  examples and all real apps working, and it avoids a gate that looks correct
  but fails open. The shop's observation is kept as
  `reacting.context.header-lookup-canonical-case-misses-http2`, marked
  `disputed`. P-2 checks the live behavior over HTTP/1.1 and HTTP/2.
- **Cases:** `reacting.context.header-lookup-lowercase`,
  `reacting.context.header-lookup-canonical-case`,
  `reacting.context.header-lookup-canonical-case-misses-http2` (disputed).

### R-REACT-24 — Store queries from trigger and processor Sessel
- **Behavior:** A bare Sessel selector (`${…}` with no `from`) queries the
  **whole site**. `from "<path or glob>"` narrows the scope. These queries read
  stored documents **without** per-request authorization, exactly like resource
  bindings. So a trigger can compare a request header with a credential kept
  in a document that the requester cannot read. Inside a trigger or processor,
  `self` is the root element of the stored target document at evaluation time,
  or `null` if the target is missing or not a DOM document. `prior` is the
  target document as stored when the request arrived, or `null` if it did not
  exist. So during triggers `prior` equals `self`. In processors after a write,
  `self` is the committed state and `prior` is the pre-write state (pagelike
  decision). Resource bindings (`r:name="…"`) declared on the trigger element
  or its ancestors are bound as Sessel variables of the same name.
- **Evidence:** documented (selector scope; bindings for triggers), demo-source
  (SHOP:25,50 bare selector reads `/private/admin.html`, which the rules deny,
  SHOP-RULES:55-59; POLLS:145 `from "/polls/*"`), inferred (`self`/`prior`).
  **Source:** D-SES §The `from` clause ("By default, a selector queries across
  the entire site"), §`self` and `prior`; D-RB §Binding placement ("For
  triggers, bindings on the trigger element or its ancestors are available"),
  §Security. **Confidence:** high (site-wide bare selector), medium (bindings),
  low (`self`/`prior`).
- **Contradiction:** DEMO-RM claims "A Trigger sees only the incoming request,
  never stored state". SHOP and POLLS read stored state from triggers. Decision
  (§16 C7): store queries work. The demo remark is about the lack of an atomic
  read-compare-write, which pagelike does not provide either (triggers run
  before the write mutex, R-REACT-36).
- **Cases:** `reacting.gates.shop-basic-auth-gate`,
  `reacting.context.resource-binding-on-trigger`,
  `reacting.context.polls-existing-document-guard`.

- **Live 2026-09-29:** resource bindings (`r:`) on or above a trigger or
  processor (the item, an ancestor, or `<html>`) are **not** bound: the name is
  undefined ("undefined variable: secret"), so reading it is a runtime error.
  pagelike follows it (adopt-live) and binds only `e:`/`j:` expression bindings
  (not probed live). A bare Sessel selector still reads any stored document
  (probe `reacting.context.probe-0929.trigger-bindings`).

### R-REACT-25 — JavaScript `ctx`
- **Behavior:** `ctx.request` is a plain object: `method`, `path`, `headers`
  (lower-case keys with case-insensitive `get` behavior, R-REACT-23), `host`
  (the request `Host` without port), `query` (object, first values), `body`
  (string), `auth`. `auth` is `null` for anonymous requests. For a signed-in
  principal it is `{username, claims: {…}, role: [...], roles: [...]}`, the
  same contents as `request.auth` in composition (permissions R-PERM-74). In
  processors `ctx.response` is added: `status` (number), `body` (string, fully
  buffered), `headers` (object with lower-case keys). Writes to `ctx` have no
  effect (R-REACT-45).
- **Evidence:** documented (fields), inferred (shapes). **Source:** D-TRIG
  §Context (JS table: `host`, `auth` "when available"); D-PROC §Context.
  **Confidence:** high (fields), low (`auth` shape for anonymous).
- **Cases:** `reacting.context.js-request-fields`,
  `reacting.context.js-auth-claims` (multi-actor).

- **Live 2026-09-29:** `auth` of an anonymous request is
  `{"claims": {}, "roles": []}`, not `null` (adopt-live). It holds no claim and
  no role. `javascript.md` R-JS-21 still says "absent"; the reactions context
  follows this entry.

### R-REACT-26 — `Context.response` in processors
- **Behavior:** `Context.response.status` (Number), `.body` (String),
  `.headers` (Map, lower-case keys). The body is the **full** representation
  the core produced: the composed page, a selector fragment for a 206, or the
  error document. For a **HEAD** request, processors see the body the equivalent
  GET would have produced. The body is stripped only when the response is sent.
  Blob bodies are exposed as UTF-8 text, and invalid byte sequences are
  replaced (inferred). A processor that re-emits such a body therefore corrupts
  binary data.
- **Evidence:** documented. **Source:** D-PROC §Context ("fully buffered");
  D-BLOG (a `when` that tests `Context.response.body.contains(…)` gives 404
  for the Not-found page and 200 for a real post when fetched with
  `curl -sI`, so HEAD sees the body). **Confidence:** high (fields, HEAD),
  low (binary).
- **Cases:** `reacting.processor.build-a-blog-status-rewrite`.

### R-REACT-27 — Shared `Context` entries
- **Behavior:** `Context` is a per-request dictionary. A schema method called
  from a trigger or processor may assign `Context.foo = …`, and code in the
  same **pass** (the trigger phase, or the processor phase) sees the new
  entry afterwards. pagelike keeps one `Context` for the whole request, so
  entries set in the trigger phase are also visible to processors (inferred,
  low). They are not visible to page composition (pagelike decision).
- **Evidence:** documented (per pass), inferred (across passes). **Source:**
  D-JSS §Method bodies ("any `Context.foo = …` assignment it makes is written
  back to the pass's Context"). **Confidence:** medium / low.

---

## 6. Thrown responses (chain termination)

### R-REACT-29 — Sessel form
- **Behavior:** `@schema HTTPResponse url("https://pagelove.org/HTTPResponse");`
  then `throw new HTTPResponse { … }` with these properties: `status` (integer,
  default 500), `message` (text, used as the body when `body` is absent), `body`
  (text), and `header` (zero or more `new Pair { key: "…", value: "…" }`, which
  needs `@schema Pair url("https://pagelove.org/Pair");`). Repeating `header:`
  adds more headers, in order. Without the `@schema HTTPResponse` declaration,
  `HTTPResponse` is an undefined name, which is a runtime error (inferred). A
  `throw` inside a `try` block can be caught by Sessel `catch` like any other
  error (grammar `try_expr`). Only an uncaught throw terminates the chain.
- **Evidence:** documented, demo-source. **Source:** D-TRIG §Chain
  termination, §Response headers ("0..n cardinality"); SHOP:30-32 (status 401,
  `WWW-Authenticate` Pair, body); POLLS:113-146 (403 and 409 with `message`);
  D-SES §Syntax (`throw_expr`, `try_expr`). **Confidence:** high.
- **Contradiction:** D-JSS mentions a Sessel form `new HTTPResponse(status,
  message)`, which no other page shows. pagelike supports the documented brace
  form and SHOULD also accept the positional form (cross-area Sessel, §16 C12).

### R-REACT-30 — JavaScript form
- **Behavior:** `throw` a plain object whose `schema_url` **or** `itemtype`
  equals `https://pagelove.org/HTTPResponse`. Fields: `status` (default 500),
  `message`, `body`, `headers` (object of name → string. An array value sends
  one field line per element, pagelike decision). `new HTTPResponse(…)` in JS
  is a `ReferenceError`, which is a runtime error. Throwing anything else
  (an `Error`, a string, an object without the marker) is a runtime error.
- **Evidence:** documented, client-source. **Source:** D-TRIG §Chain
  termination ("Supported properties on the thrown object: `status` (defaults
  to `500`), `message` …, `body`, and `headers`"); D-JSS §Explaining why a
  value was rejected; BJ-CORS:57-67. **Confidence:** high.

### R-REACT-31 — The thrown response on the wire
- **Behavior:** The thrown object **replaces** the response entirely:
  - Status: `status`, or 500 if absent. A non-integer or out-of-range
    (not 100–599) status becomes 500 (pagelike decision).
  - Body: `body` if present (even `""`), else `message` if present, else empty.
    For 1xx, 204, 304 and HEAD no body is sent.
  - Headers: exactly the thrown headers plus the platform's generic headers
    (`Date`, `Content-Length`, and pagelike's own diagnostics). Nothing from the
    stored resource or from core processing is carried over: no stored
    `Content-Type`, `ETag`, `Last-Modified`, `Content-Range` or `Vary`.
  - `Content-Type` defaults to `text/html; charset=utf-8` when the thrown
    headers do not set it (pagelike decision; the default is undocumented).
  - Invalid header names, and values containing CR, LF or NUL, are dropped
    (same rule as R-REACT-54). `Content-Length`, `Transfer-Encoding` and
    `Connection` from the thrown object are ignored.
- **Evidence:** documented (fields), client-source ("throwing replaces the
  response, so the stored type is not carried over"), inferred (defaults).
  **Source:** D-TRIG §Chain termination; BJ-CORS:45-48 and commit `412668a`
  message. **Confidence:** high (status, body precedence, replacement),
  low (default Content-Type, dropped headers).
- **Status/errors:** whatever status was thrown. Common in the wild: 401 with
  `WWW-Authenticate` (SHOP), 403 and 409 (POLLS), 303 with `Location` (docs).
- **Cases:** `reacting.actions.*-throw-*`.

- **Live 2026-09-29:** GET, PUT, DELETE and selector POSTs send a thrown
  3xx with its `Location`. A **POST without `Range`** (the templated-creation
  path, composing R-COMP-140) does not: a thrown 3xx makes PageLove read the
  `Location` as a template (404 "Document not found: …" for a missing page,
  422 "Template must include a <base href> …" for a page without one), and a
  thrown 201 loses its `Location`. pagelike keeps sending the thrown response
  (keep-standard); `reacting.actions.sessel-throw-303-location-pair.live`
  measures the difference.

### R-REACT-32 — Sessel-thrown bodies are re-serialized as HTML
- **Behavior:** When a **Sessel** action throws an `HTTPResponse`, its `body`
  (or `message`) string is parsed as HTML and serialized again before it is
  sent. If the string starts (after leading whitespace) with `<!DOCTYPE` or
  `<html`, it is parsed as a document. Otherwise it is parsed as a fragment in
  `<body>` context and the fragment's children are serialized. For HTML input
  this round-trips (apart from normalization). For other text, markup-significant
  characters come back escaped: `=>` becomes `=&gt;`, and `&&` becomes
  `&amp;&amp;`. JavaScript-thrown bodies are sent byte-for-byte. pagelike MUST
  reproduce this difference for compatibility.
- **Evidence:** client-source. **Source:** BJ-CORS:27-35 and commit `412668a`
  message ("Rethrowing the body through Sessel serialises the string as
  HTML"). **Confidence:** medium (effect observed on the live platform by its
  maintainer; exact parser context inferred).
- **Contradiction:** the docs never mention it and treat `body` as a plain
  string (§16 C8). If PageLove fixes it, pagelike follows (probe P-8).
- **Cases:** `reacting.processor.sessel-rethrow-escapes-non-html`,
  `reacting.processor.js-throw-preserves-bytes`.

### R-REACT-33 — Scope of chain termination
- **Behavior:** A throw from a trigger's `when`, `action` or `otherwise` ends
  the whole trigger phase (R-REACT-9). A throw from a processor ends the
  processor phase. Later processors do not run, and the thrown response is
  final. Neither kind of throw rolls back committed writes. In particular, a
  processor throw after a successful write leaves the write committed and its
  SSE event published.
- **Evidence:** documented. **Source:** D-TRIG §Execution order; D-PROC §Chain
  termination ("The thrown response replaces whatever core produced").
  **Confidence:** high.
- **Cases:** `reacting.order.*`, `reacting.processor.chain-termination-order`.

### R-REACT-34 — Runtime errors are surfaced
- **Behavior:** Any error in `when`, `action`, `otherwise`, or a dynamic
  HttpRequest property of a trigger or processor that is not a recognized
  `HTTPResponse` fails the request. Examples: a Sessel type error, an unknown
  function, an undefined name, a JS exception or `ReferenceError`, a budget
  timeout, or a failed `Pagelove.PUT` (R-REACT-40). The failure is never
  swallowed. pagelike responds **500** with the shared error document
  (`internal/errdoc`, reading-writing), `Content-Type: text/html;
  charset=utf-8`. The document embeds one `https://pagelove.org/BindingFailure`
  item with `language` (`https://pagelove.org/Sessel` or
  `https://pagelove.org/JavaScript/Module`), `variant`, `message` and, when
  available, `stack` (modeling R-MOD-73). No later trigger runs. Core and
  processors do not run for a trigger error. A processor error replaces the
  response.
- **Evidence:** documented (surfaced, "HTML Microdata error body"), inferred
  (status 500, envelope). **Source:** D-TRIG §Error handling ("Runtime errors
  are surfaced, not swallowed, regardless of which language raised them");
  D-PROC §Error handling; D-JSS §Errors. **Confidence:** high (fails with
  microdata body), low (500, envelope).
- **Cases:** `reacting.actions.js-plain-error-is-runtime-error`,
  `reacting.gates.when-runtime-error-fails-request`,
  `reacting.writes.js-action-has-no-write-provider`.

---

## 7. Execution order

- **Live 2026-09-29:** errors in actions are surfaced as documented (500,
  `TriggerError` item with the message, e.g. "undefined variable: secret").
  A failing `when` is swallowed as falsy on PageLove; pagelike keeps 500
  (R-REACT-17 note).

### R-REACT-35 — Deterministic total order
- **Behavior:** Triggers are ordered by the **path** of the document that holds
  them, compared lexicographically as byte strings (so `/app/auth` comes before
  `/app/logging`, and upper case sorts before lower case). Within a document
  they run in **document order** (pre-order position of the item element).
  Processors are ordered the same way, independently of triggers. Subtype items
  take their place in the same order. Every matching item runs, subject to
  chain termination.
- **Evidence:** documented. **Source:** D-TRIG §Execution order; D-PROC
  §Execution order ("Same rules as triggers"). **Confidence:** high (rule),
  medium (byte order for non-ASCII and mixed case).
- **Cases:** `reacting.order.*`.

---

## 8. Writes from triggers and processors

### R-REACT-36 — Side-effect writes (`Pagelove.PUT(item, path)`)
- **Behavior:** In Sessel, after `@schema Pagelove url("https://pagelove.org/1.0");`,
  `Pagelove.PUT(item, path)` stores `item` as a **whole document** at `path`,
  unless it is a transformation (R-REACT-37). `item` is usually a constructed
  instance (`new Thing { … }`), materialized with the Sessel rules. Those rules
  inject defaults and emit the `@key` value as `id` (modeling R-MOD-34). It may
  also be a queried element, which is stored verbatim. The write:
  - goes through the serving-path write pipeline **as the requesting
    principal**: authorization of `PUT` on `path`, schema/shape/uniqueness
    validation, **transition constraints**, commit, an SSE event not
    echo-suppressed for the originator (sse R-SSE-23), and **transition
    handler** firing;
  - does **not** run triggers or processors (pagelike decision; recursion is
    otherwise unbounded);
  - is **committed immediately**, in its own transaction, when the call
    returns. A later throw or error in the chain does not roll it back, and
    neither does a later failure of the main request;
  - returns `null` on success (pagelike decision).
  `Pagelove.PUT` in an `.each(el => …)` lambda performs one write per call.
- **Evidence:** documented (pipeline incl. authorization and validation, no
  rollback), inferred (actor, no nested triggers, return). **Source:** D-TRIG
  §Writing from triggers, §Side-effect writes, §Error handling for writes;
  D-PROC §Writing from processors; D-SES §List `.each` example;
  D-PROP §Primary key. **Confidence:** high (documented), low (actor, nested
  triggers).
- **Implementation note:** plan §Write path takes the site write mutex once
  per request and commits everything in one transaction. Side-effect writes
  cannot join that transaction, because they must survive a later throw.
  pagelike therefore runs the trigger phase **before** taking the mutex for
  the main write, against a read snapshot. Each side-effect write takes and
  releases the mutex itself. A processor-phase write runs after the main write
  has committed and released the mutex.
- **Cases:** `reacting.writes.side-effect-write`,
  `reacting.writes.side-effect-survives-later-throw`,
  `reacting.processor.side-effect-audit-log`.

### R-REACT-37 — Body transformation (`Pagelove.PUT(item, Context.request.path)`)
- **Behavior:** When the request method is **PUT or POST** and the `path`
  argument equals the request path (`Context.request.path`, exact string
  comparison), `Pagelove.PUT` performs **no write**. Instead it replaces the
  in-flight request body with the serialization of `item` (content type
  `text/html`). Core processing later runs normally on the new body, with the
  original `Range` header, conditional headers and method. The transformed
  body is discarded if the trigger chain ends in a throw or a runtime error.
  For any other method, the same call is an ordinary side-effect write
  (pagelike decision).
- **Evidence:** documented (PUT), inferred (method split). **Source:** D-TRIG
  §Body transformation ("No `throw` is needed; core processing runs normally
  on the modified body"), §Error handling for writes ("transient").
  **Confidence:** high (PUT whole document), low (POST, selector requests,
  other methods).
- **Cases:** `reacting.writes.body-transformation`,
  `reacting.writes.transformation-discarded-on-throw`.

### R-REACT-38 — Chained transformations
- **Behavior:** Each later trigger sees the body as left by the previous
  transformation, in both `Context.request.body` and `ctx.request.body`. The
  last transformation wins.
- **Evidence:** documented. **Source:** D-TRIG §Chaining. **Confidence:** high.
- **Cases:** `reacting.writes.chained-transformations-last-wins`.

### R-REACT-39 — `Pagelove.DELETE`
- **Behavior:** `Pagelove.DELETE(path)` deletes the document at `path` through
  the serving-path pipeline: authorization of `DELETE`, referential actions,
  **exit** transitions, SSE, and no triggers or processors. It commits
  immediately and returns `null`. A missing document is an error (404,
  R-REACT-40). The signature is not documented. pagelike takes one path
  argument. If the first argument is an element, pagelike deletes that element
  from its document (a selector DELETE using the element's stable selector)
  (pagelike decision).
- **Evidence:** documented (existence only). **Source:** D-TRIG §Writing from
  triggers. **Confidence:** low.
- **Cases:** `reacting.writes.pagelove-delete`.

### R-REACT-40 — Failed writes inside actions
- **Behavior:** If a side-effect write is refused (401/403 authorization, 404,
  409, 412, 413, 422 validation or transition), `Pagelove.PUT`/`DELETE` raises
  a Sessel error whose message contains the status and the refusal description.
  Uncaught, it is a runtime error: 500 (R-REACT-34). `try … catch (e)` lets the
  action continue. The refused write changes nothing.
- **Evidence:** inferred. **Confidence:** low. Probe P-10.
- **Cases:** `reacting.writes.side-effect-denied-writes-nothing`.

### R-REACT-41 — JavaScript actions cannot write
- **Behavior:** JavaScript `when`/`action` code has no `Pagelove` object. A
  reference to it is a `ReferenceError`, which is a runtime error. To write,
  JavaScript must queue an HttpRequest back to the host (R-REACT-49).
- **Evidence:** documented. **Source:** D-TRIG §Writing from triggers ("a
  Sessel-only capability … without a write provider"); D-PROC §Writing from
  processors. **Confidence:** high (no capability), medium (ReferenceError).
- **Cases:** `reacting.writes.js-action-has-no-write-provider`.

### R-REACT-42 — Transient elements
- **Behavior:** Triggers "can write to transient elements". The mechanism is
  not documented. pagelike applies a `Pagelove.PUT` whose target is a document
  containing a `p:transient` element to the session's transient content, using
  the transient-element rules (D-TE). This happens when the requesting session
  exists and the written item has the same identity selector. Otherwise the
  write is a normal document write (pagelike decision, low). Transient writes
  never trigger transition validation or handlers (they are not stored
  documents).
- **Evidence:** documented (capability only). **Source:** D-TRIG intro,
  §Authentication example; D-TE. **Confidence:** low. Probe P-12.

---

## 9. Processors: shaping the response

### R-REACT-43 — Pass-through by default
- **Behavior:** If no processor matches, or no matching processor changes the
  response, the core response is sent **unchanged**: status, headers (including
  `ETag`, `Content-Type`, `Content-Range`, `Cache-Control`) and body bytes.
- **Evidence:** documented. **Source:** D-PROC §Pass-through. **Confidence:** high.
- **Cases:** `reacting.processor.pass-through`.

### R-REACT-44 — Sessel status and body assignment
- **Behavior:** In a processor Sessel action, `Context.response.status = <n>`
  sets the status. Integers 100–599 are accepted, and a numeric string is
  converted. Anything else is a runtime error. `Context.response.body =
  <string>` replaces the body bytes (UTF-8, sent as given, **not**
  re-serialized) and recomputes `Content-Length`. When the body is replaced,
  pagelike removes `ETag` and `Content-Range` (pagelike decision, low). Other
  headers stay, including `Content-Type`. These are the only assignments that
  take effect.
- **Evidence:** documented. **Source:** D-PROC §Reading and writing the
  response, §Quick example (body), D-BLOG (status). BJ-CORS:21-25 ("the
  executor reads back only status and body"). **Confidence:** high
  (status, body), low (header clean-up).
- **Cases:** `reacting.processor.custom-404`,
  `reacting.processor.build-a-blog-status-rewrite`.

### R-REACT-45 — Header writes and JS mutation do not take effect
- **Behavior:**
  - Writes to response headers from Sessel have **no effect** in pagelike.
    Assigning the whole map (`Context.response.headers = {…}`) is accepted and
    ignored, with a logged warning. `Context.response.headers.set(…)` is an
    unknown function and index assignment (`headers["X"] = …`) is an invalid
    assignment target. Both are runtime errors (500).
  - Assignments to `ctx.response` (or `ctx.request`) in JavaScript are silently
    ignored. JS gates and actions use the same evaluation path, which does not
    read writes back.
  - To add a header, a processor must **throw** an `HTTPResponse` that carries
    the headers, the status and the body. JS is preferred when the body must
    stay byte-exact (R-REACT-32).
- **Evidence:** documented (JS), client-source (Sessel header writes, error
  messages). **Source:** D-PROC §Response-mutation asymmetry; BJ-CORS:19-25,
  37-39, `cbe1dcb:cors.html` comment (`unknown function: set`,
  `invalid assignment target`, header writes "dropped in silence even when it
  parses"). **Confidence:** high (JS), medium (Sessel).
- **Contradiction:** D-PROC §Pass-through says processors affect the response
  when an action writes `Context.response.headers`. The beta-js maintainer
  found header writes dropped on the live platform and reported it as a bug
  (§16 C9). Decision: reproduce the observed behavior now, and revisit after
  probe P-9.
- **Cases:** `reacting.processor.js-mutation-ignored`,
  `reacting.processor.js-throw-adds-headers`.

### R-REACT-46 — Processors see earlier processors' changes
- **Behavior:** Processors form a chain in the order of R-REACT-35. Each one
  sees `Context.response` as left by the previous one. Its `status` filter and
  `when` are evaluated against that current response.
- **Evidence:** inferred ("execute in order", chain vocabulary). **Source:**
  D-PROC §Request lifecycle, §Chain termination. **Confidence:** low.
- **Cases:** `reacting.processor.chain-sees-previous-change`.

### R-REACT-47 — Processor throws
- **Behavior:** Same as R-REACT-29 to R-REACT-32. The thrown response replaces
  the current one completely, including stored headers such as `Content-Type`.
  Common use: add headers by re-throwing `ctx.response.status` and
  `ctx.response.body` with extra `headers`, and set `Content-Type` explicitly
  because it is not carried over.
- **Evidence:** documented, client-source. **Source:** D-PROC §action, §Chain
  termination; BJ-CORS:50-70. **Confidence:** high.
- **Cases:** `reacting.processor.js-throw-adds-headers`,
  `reacting.processor.js-throw-preserves-bytes`.

---

## 10. Outbound HTTP requests (`HttpRequest`)

### R-REACT-48 — Item shape
- **Behavior:** An `action` (or `otherwise`) item of type
  `https://pagelove.org/HttpRequest` with properties `url` (required), `method`,
  `content-type`, `body`, `header` (0..n), `retry`. pagelike also accepts the
  itemtype spelling `https://pagelove.org/HTTPRequest` (pagelike decision).
  The Resource Binding page's `transformation` property (a JS-like object
  literal in `<code itemprop="transformation">`) is **not** supported. It is
  ignored, and its presence does not disable the item.
- **Evidence:** documented. **Source:** D-HTTP §Properties; D-RB §Trigger
  context (uses `HTTPRequest` and `transformation`). **Confidence:** high
  (shape), low (alias, `transformation`).
- **Contradiction:** itemtype spelling (§16 C10).

### R-REACT-49 — `url`
- **Behavior:** Required (R-REACT-4). The value is used **as given**. No
  normalization is applied, so `"https://hooks.example.com/" +
  Context.request.path` produces a double slash, which is sent as is. An
  absolute `http:` or `https:` URL is sent to that origin. A value starting
  with `/` is resolved against the **site's own public origin** (scheme and
  host of the current request). This is how a JavaScript action "writes back
  to the host". Such a request is an ordinary anonymous public-plane request,
  subject to the site's rules and triggers. pagelike delivers it in-process
  when the host is a site it serves. Any other scheme, or an unparseable URL,
  drops the action (pagelike decision).
- **Evidence:** documented (static, dynamic), inferred (relative resolution).
  **Source:** D-HTTP §url, §Static vs dynamic properties (JS example returns
  `"/api/" + ctx.request.path`); D-TRIG §Writing from triggers ("queue an
  outbound HTTP request back to the host instead"). **Confidence:** high
  (absolute), low (relative).
- **Cases:** `reacting.outbound.dynamic-url-sessel`,
  `reacting.outbound.dynamic-url-js`.

### R-REACT-50 — `method`
- **Behavior:** Default `POST`. Supported: `GET`, `POST`, `PUT`, `DELETE`,
  `PATCH`, compared case-insensitively and sent in upper case. Any other value
  drops the action (pagelike decision).
- **Evidence:** documented. **Source:** D-HTTP §method. **Confidence:** high
  (default, list), low (invalid values).
- **Cases:** `reacting.outbound.default-method-post`,
  `reacting.outbound.method-put`.

### R-REACT-51 — `content-type` and `body`
- **Behavior:** `Content-Type` is taken from `content-type`, default
  `text/html`, sent exactly as that string (no charset is added). `body` is
  sent as its UTF-8 bytes, exactly, with no HTML re-serialization. The dynamic
  forms return strings. An absent body sends an empty body
  (`Content-Length: 0` for POST, PUT and PATCH). `Content-Type` is sent on
  every request, including bodiless ones (inferred from "Content-Type header
  sent with the request. Defaults to text/html").
- **Evidence:** documented. **Source:** D-HTTP §content-type, §body; D-WH
  (JSON body built in Sessel). ATS:13-30 (JSON body is `Context.request.body`,
  content-type `application/json`). **Confidence:** high (default, value),
  low (bodiless requests).
- **Cases:** `reacting.outbound.docs-quick-example`,
  `reacting.outbound.webhook-recipe-json-body`,
  `reacting.outbound.ats-outbox-relay`.

### R-REACT-52 — `header`
- **Behavior:** Each `header` value adds one request header field:
  - a `https://pagelove.org/Pair` item with `key` and `value`. The value may be
    dynamic (Sessel or JS). An empty value (`<meta itemprop="value" content>`)
    sends the field with an empty value (inferred). A missing `key` drops the
    pair;
  - or a single line of text `Name: value`, split at the **first** colon. The
    name and value are trimmed of surrounding whitespace.
  Repeated names are all kept. Fields are sent in declaration order. pagelike
  MUST keep the relative order of same-name fields and SHOULD keep the order
  across names. Go's `http.Header` does not keep that order, so this needs an
  ordered header writer. The harness checks only same-name order.
  `Host`, `Content-Length`, `Transfer-Encoding` and `Connection` set by a
  `header` are ignored (pagelike decision).
- **Evidence:** documented. **Source:** D-HTTP §header ("Repeats are kept …
  sent in the order you write them"); D-WH §Authenticating the webhook; ATS:17-24.
  **Confidence:** high (forms, repeats), low (empty value, ignored names).
- **Cases:** `reacting.outbound.header-pairs-*`,
  `reacting.outbound.header-single-line`,
  `reacting.outbound.header-dynamic-value`.

### R-REACT-53 — `Content-Type` has one home
- **Behavior:** If a `header` sets `Content-Type` (name compared
  case-insensitively), its value **replaces** the `content-type` property, and
  the field is sent exactly once. When several `header` values set it, the
  last one wins (pagelike decision).
- **Evidence:** documented. **Source:** D-HTTP §header. **Confidence:** high /
  low (several).
- **Cases:** `reacting.outbound.content-type-header-replaces-property`.

### R-REACT-54 — Malformed headers are dropped
- **Behavior:** A header whose name is not a valid HTTP token (RFC 9110
  `tchar`s only, non-empty), or whose value contains a control character
  (CR, LF, NUL or other C0 controls except HTAB, or DEL), is **dropped**. This
  also applies when a dynamic value produces it. The rest of the request is
  sent normally.
- **Evidence:** documented. **Source:** D-HTTP §header ("Malformed headers
  are dropped"). **Confidence:** high (CR/LF, invalid names), medium (exact
  character sets).
- **Cases:** `reacting.outbound.malformed-headers-dropped`.

### R-REACT-55 — Dynamic properties
- **Behavior:** Dynamic `url`, `method`, `content-type`, `body` and Pair
  `value` items are evaluated **when the action runs** (R-REACT-18), in
  document order. They use the same context as the owning trigger or processor,
  so processors can read `Context.response`. Each property may use a different
  language. A `null` result counts as absent. An evaluation error is a runtime
  error of the owning item (R-REACT-34). A dynamic `retry` is not allowed: such
  a `retry` is treated as `0` (inferred).
- **Evidence:** documented (per-property language), inferred (timing, errors).
  **Source:** D-HTTP §Static vs dynamic properties, §Retry. **Confidence:**
  high / low.

### R-REACT-56 — Queueing and dispatch
- **Behavior:** Executing an HttpRequest action appends a fully evaluated
  request to the request's queue. The queue is dispatched only **after the
  response has been written to the client**. It is fire-and-forget: it never
  delays the response, and its outcome never changes it. Initial attempts
  start in queue order, with trigger-phase entries before processor-phase
  entries. Arrival order at the receiver is not guaranteed. The queue is
  dispatched whatever the response. This includes a trigger-thrown response, a
  core failure (for example 422), and an authorization denial when processors
  queue requests. A **trigger runtime error** also still dispatches what was
  queued before it (pagelike decision, consistent with committed side-effect
  writes). The webhook recipe relies on the processor `status` filter (for
  example `2xx`) to fire only on success.
- **Evidence:** documented (after response, fire-and-forget), inferred (the
  rest). **Source:** D-HTTP intro; D-WH §Execution timing; D-TRIG §action
  ("queued for dispatch after the response is sent"). **Confidence:** high /
  low.
- **Cases:** `reacting.outbound.failure-does-not-affect-client`,
  `reacting.outbound.queued-before-throw-still-sent`,
  `reacting.outbound.processor-status-filter-success-only`.

### R-REACT-57 — Retry
- **Behavior:** `retry` is a static non-negative integer, default `0`.
  Attempts = 1 + min(retry, 4). So `retry` 3 gives 4 attempts, and `retry` 9
  behaves exactly like 4, which gives 5 attempts. Negative, non-numeric or
  dynamic values count as 0 (inferred). The delay before attempt *k* (k ≥ 2) is
  min(2^(k−1) s, 30 s), which gives 2, 4, 8 and 16 seconds. After the last
  failed attempt the request is abandoned. Nothing is reported to the client
  (tracing only). pagelike SHOULD keep delays within ±20 % of the schedule.
- **Evidence:** documented. **Source:** D-HTTP §Retry (table, clamp, 30 s cap,
  "a plain number"), §Error handling; D-WH §Execution timing. **Confidence:**
  high.
- **Contradiction:** ATS:6-8 describes the outbox as "failures retried by
  Pagelove with exponential backoff" but sets no `retry`. The docs say the
  default is no retry (§16 C13). Decision: follow the docs.
- **Cases:** `reacting.outbound.retry-*` (slow).

### R-REACT-58 — What counts as failure
- **Behavior:** An attempt fails on a transport error (DNS, connect, TLS, reset,
  or timeout) or a final status outside 2xx. pagelike does **not** follow
  redirects, so a 3xx is a failure (pagelike decision; it also prevents
  redirect-based SSRF). The per-attempt timeout is 10 s for connect plus
  response headers, and 30 s overall (pagelike decision). The response body is
  read up to 64 KiB and discarded.
- **Evidence:** documented (transport and non-2xx), inferred (redirects,
  timeouts). **Source:** D-HTTP §Error handling. **Confidence:** high / low.
- **Cases:** `reacting.outbound.retry-default-zero-single-attempt`.

### R-REACT-59 — Durability (pagelike)
- **Behavior:** pagelike records queued generic requests in `site.db` table
  `outbox`. Entries from a write request are recorded in the same transaction
  as the write, or in their own transaction if the request wrote nothing. So
  pending retries survive a restart. On restart, an entry whose attempt was in
  flight is retried if attempts remain. That is at-least-once within the retry
  budget, which is stronger than the docs promise, and harmless. Transition
  handler deliveries are **at-most-once** and use a different rule
  (R-REACT-82).
- **Evidence:** inferred (plan §site.db `outbox`). **Confidence:** n/a (design).

### R-REACT-60 — Credentials in declarations
- **Behavior:** Header values, tokens included, are stored in plain text in the
  declaring document and are readable by anyone the authorization rules let
  read that document. pagelike adds no protection. Documentation and the
  console SHOULD warn about this. The ATS template ships the token field empty.
- **Evidence:** documented. **Source:** D-HTTP §header (blockquote); D-WH
  §Authenticating the webhook ("Where that token lives"); ATS:19.
  **Confidence:** high.

### R-REACT-61 — Destination policy (pagelike security decision)
- **Behavior:** pagelike refuses outbound requests to loopback, link-local
  (including `169.254.169.254`), RFC 1918, CGNAT and IPv6 ULA addresses unless
  the server is started with `--outbound-allow-private` or the site setting
  `outbound.allow_private: true`. The check applies to every resolved address
  (DNS rebinding guard). Requests to sites served by the same pagelike process
  are always allowed and delivered in-process. The harness enables private
  destinations for local targets so that `${SINK}` on 127.0.0.1 works. A
  refused destination is a failed attempt (retried per `retry`, then
  abandoned). PageLove's policy is unknown.
- **Evidence:** inferred. **Confidence:** n/a (security decision).

---

## 11. Transition constraints

### R-REACT-62 — Item shape and validity
- **Behavior:** Properties: `selector` (required; any CSS selector including
  PageLove extensions such as `:isa()`), `property` (required; the microdata
  property that holds the state), `from`, `to` (at least one of the two). A
  stored rule that lacks `selector` or `property`, or declares neither `from`
  nor `to`, is **ignored**. It never enforces anything and never blocks writes.
  When such a rule is **written through the serving path**, pagelike rejects
  the write with 422 (a platform-schema violation, `SchemaViolation`
  envelope, modeling R-MOD-70), matching "hosts running the platform
  schemas". Over WebDAV it is stored and ignored. Several `from` or `to` values
  on one rule permit every (from, to) combination (pagelike decision).
- **Evidence:** documented. **Source:** D-TC §Properties ("one that declares
  neither is ignored … on hosts running the platform schemas, writing such a
  rule is rejected with 422"), §Taking effect ("A stored rule missing its
  `selector` or `property` is ignored"). **Confidence:** high (ignored),
  medium (422 on the serving path), low (multi-valued).
- **Cases:** `reacting.tc.neither-from-nor-to-rejected-on-serving-path`,
  `reacting.tc.missing-property-ignored`.

### R-REACT-63 — Rule kinds and values
- **Behavior:**
  - `from` + `to`: a **step** `from → to`;
  - `to` only: an **entry** rule. The property, or the whole item, may appear
    with that value;
  - `from` only: an **exit** rule. The property, or the whole item, may
    disappear from that value.
  Values are compared as exact strings with the watched property's microdata
  value (no trimming, case-sensitive). The empty string `""`
  (`content=""`) is an ordinary state. It is distinct from absent, so
  `from=""` with `to="x"` permits `"" → "x"`, but it does not declare an
  entry.
- **Evidence:** documented. **Source:** D-TC §`from` and `to`. **Confidence:**
  high (kinds, empty), medium (no trimming).
- **Cases:** `reacting.tc.empty-string-is-a-state`.

### R-REACT-64 — Strictness and its scope
- **Behavior:** An item is **watched for property p** when at least one valid
  constraint with `property` p has a `selector` that selects the item's element.
  Selection is evaluated in the old document for the item's old state and in
  the new document for its new state. The item is watched if it is selected in
  either. For a watched item, **every** appearance, change and disappearance of
  p must be permitted by a rule among the constraints that watch it for p.
  Anything else is rejected with 422. Unwatched properties, the same property
  on items the selectors do not select, and other properties of watched items
  are unconstrained. A write that leaves p's value unchanged is not a
  transition and always passes.
- **Evidence:** documented. **Source:** D-TC §Strictness; D-SM §Declare the
  rules. DEMO3 listing.html:42-45 (without the entry rule every new listing
  was rejected, "verified"), app.js:105-109 (a same-value write is "a no-op,
  not a rejected transition (verified)"). **Confidence:** high.
- **Cases:** `reacting.tc.*-unconstrained`, `reacting.tc.unchanged-value-passes`.

### R-REACT-65 — Classification of a change
For each watched pair (old item o, new item n), either of which may be absent,
and each watched property p:

1. `vo` = list of microdata values of p on o (empty if o is absent). `vn`
   likewise for n.
2. If n exists and `len(vn) > 1`: violation **multi-valued** (R-REACT-66).
3. Old state `so` = `vo[0]` if `len(vo) == 1`, otherwise **absent**. An old
   multi-valued state is treated as absent (pagelike decision). New state `sn`
   likewise.
4. `so == sn` (both absent, or equal strings): no transition, so it passes.
5. `so` absent and `sn` present: an **entry**. It needs a rule with no `from`
   and `to == sn`.
6. `so` present and `sn` absent: an **exit**. It needs a rule with
   `from == so` and no `to`. Deleting the item, removing its property element,
   or deleting the whole document are all exits.
7. Both present and different: a **step**. It needs a rule with `from == so`
   and `to == sn`.

- **Evidence:** documented (entry, exit, step, unchanged), pagelike decision
  (old multi-valued). **Source:** D-TC §Strictness, §`from` and `to`; D-SM §The
  delete surprise. **Confidence:** high / low.

### R-REACT-66 — The watched property must be single-valued
- **Behavior:** A write that leaves a **constrained** item with two or more
  values for its watched property is rejected with 422. The violation names the
  item's itemtype, its key (if any) and the property. For a handler-only watch,
  the write commits and the handler does not fire for that item (R-REACT-78).
- **Evidence:** documented. **Source:** D-TC §The watched property must be
  single-valued; D-TH §When a handler fires. **Confidence:** high.
- **Cases:** `reacting.tc.multi-valued-rejected`,
  `reacting.th.multi-valued-commits-without-firing`.

### R-REACT-67 — Which writes are validated
| Write | Validated? |
|---|---|
| Public whole-document PUT (create or replace) | yes, whole-document pairing (R-REACT-68) |
| Public selector PUT, POST append/placement, selector DELETE | yes, selector-scoped identity (R-REACT-69) |
| Public whole-document DELETE | yes. Every watched item exits |
| Resource creation from a template (POST that writes a new document) | yes, as a whole-document create (inferred) |
| `Pagelove.PUT` / `Pagelove.DELETE` side-effect writes | yes (inferred from "the normal request pipeline") |
| CRDT change-set `PATCH` | yes, documented. pagelike currently answers 501 for PATCH (reading-writing), so there is nothing to validate |
| Element or document `MOVE` | yes. The moved element keeps its identity, so its values are unchanged and no transition occurs. Ancestor items at the source and destination are validated like selector DELETE and POST (pagelike decision) |
| Cascade rewrites and deletions (modeling referential actions) | yes, in the same validation (inferred) |
| Session-scoped transient element writes | no (not stored) |
| WebDAV authoring writes | **never** (R-REACT-75) |

- **Evidence:** documented (whole, selector, deletes, change-set merges, WebDAV),
  inferred (rest). **Source:** D-SM §When to use this approach ("whole-document
  and selector-scoped writes, deletes, and change-set merges alike"), §Take the
  legal step; D-TC §Strictness; D-TH §Delivery (CRDT). **Confidence:** high /
  low.

### R-REACT-68 — Item identity for whole-document writes
- **Behavior:** To compare stored document O with incoming document N, group
  watched items by exact itemtype (the first token of `itemtype`). Then,
  within each group:
  1. If the type has a primary key k (modeling R-MOD-33: honoured `@key`,
     inherited from a parent if needed), items that have a value for k pair
     **by equal key value**. A key value that appears twice on one side is an
     ambiguity (R-REACT-70). An old keyed item with no partner **exits**. A new
     keyed item with no partner **enters**. So a changed key, and an item
     gaining or losing its key, validates as one exit plus one entry, and each
     needs its own rule.
  2. Items without a key value (types with no key, or keyed-type items
     missing the value) pair only if **exactly one** such item exists on each
     side. One on one side and none on the other is an exit or an entry. Two or
     more on either side is an ambiguity.
- **Evidence:** documented. **Source:** D-TC §Item identity and `@key` ("The
  key may not change", "A lone keyless item still pairs", "Ambiguity is
  rejected"); D-SM §Seed an order, §The two-orders-in-one-document wedge.
  **Confidence:** high (rules), medium (grouping by first itemtype token,
  keyed-type items missing their value).
- **Cases:** `reacting.tc.key-pairing-*`, `reacting.tc.lone-keyless-item-pairs`,
  `reacting.tc.key-change-is-exit-plus-entry`.

### R-REACT-69 — Identity for selector-scoped writes
- **Behavior:** The request names the element it changes, so no guessing is
  needed. Every item that is an **ancestor** of the changed position pairs
  with itself. This covers demo-03's `<span itemprop="status">` replacement
  inside a Listing: the Listing's `status` goes from `open` to `claimed`.
  - Selector PUT replacing M with fragment F: if M and F's root element are
    both items of the same type, they pair. Items nested inside M and F use
    R-REACT-68 restricted to those two subtrees.
  - POST (append or placement) of F: items in F have no old counterpart and
    **enter**. Ancestors of the insertion point pair with themselves, so a
    property value added to an ancestor item is checked as an entry or step.
  - Selector DELETE of M: items in M's subtree **exit**. Ancestors pair with
    themselves.
- **Evidence:** documented (identity inherent), demo-source (ancestor pairing),
  inferred (details). **Source:** D-TC §Item identity ("Selector-scoped writes
  name the exact element they change, so identity is inherent there"); D-SM
  §Take the legal step; DEMO3 app.js:91-95 and listing.html:46-68.
  **Confidence:** high (principle), medium (ancestor pairing), low (subtree
  pairing).
- **Cases:** `reacting.tc.recipe-end-to-end`, `reacting.tc.demo3-claim-status-span`,
  `reacting.tc.post-append-is-entry`, `reacting.tc.selector-delete-is-exit`.

### R-REACT-70 — Ambiguity
- **Behavior:** An ambiguous group (R-REACT-68) whose items are watched by at
  least one **constraint** rejects the write with 422 when the write **touches**
  the constrained property for that group. pagelike defines "touches" as a
  difference in the item counts, or in the multiset of watched values of that
  group's keyless items, between O and N. A write that edits only
  unconstrained properties of such items passes. The violation message says
  the type needs a `@key` property. A group watched only by handlers is
  **skipped**: the write commits and no handler fires for those items.
- **Evidence:** documented, pagelike decision (definition of "touches").
  **Source:** D-TC §Item identity ("reject the write with 422 and a message
  saying the type needs a `@key` property"); D-SM §The two-orders-in-one-document
  wedge ("Every write to that document that touches a constrained property is
  rejected … Handler-only watches are not affected"). **Confidence:** high
  (reject, message), low ("touches").
- **Cases:** `reacting.tc.two-keyless-items-ambiguous`,
  `reacting.tc.ambiguous-group-untouched-passes`.

### R-REACT-71 — Pipeline position and atomicity
- **Behavior:** Transition validation is step 10 of the modeling write
  pipeline (modeling R-MOD-15): after schemas, uniqueness and shapes, and
  before referential actions and the commit. So a duplicate key fails first as
  a uniqueness 422. Constraints are read from the **committed** state at
  validation time, not from the write being validated (a write that adds rules
  and items together is checked against the old rules). All violations of the
  write are collected. If any exist, the response is 422 and **nothing** in any
  document changes.
- **Evidence:** documented (422, nothing changes), inferred (position, rule
  snapshot). **Source:** D-TC intro ("rejected with 422 … and nothing in the
  document changes"); D-SM §Watch the illegal step fail. **Confidence:** high /
  low.

### R-REACT-72 — The 422 body
- **Behavior:** `422 Unprocessable Entity`, `Content-Type: text/html;
  charset=utf-8`, a document whose `<body>` is a
  `https://pagelove.org/ConstraintViolation` item:
  - `name` = `Unprocessable Entity` (`<h1>`), `statusCode` = `422` (`<meta>`),
    `description` = `Transition constraints violated` (`<p>`);
  - one `violations` item per violation, of type `https://pagelove.org/Violation`,
    written as `<ul><li itemprop="violations" itemscope itemtype=…>`, with
    `<span>` properties:
    - `constraintSelector` = `[itemprop='<p>']` (a property selector, **not**
      the constraint's `selector`);
    - `failedConstraint` = `transition(<p>)`;
    - `message`. Keyed item: `Transition violation: '<itemtype>' item '<key>'
      property '<p>' may not change from '<from>' to '<to>' (constraint declared
      in '<path>')`. Keyless item: the same without ` item '<key>'`. `<path>`
      is the path of the first document, in R-REACT-35 order, that declares a
      constraint watching this item for p. Entries and exits use `''` for the
      missing side;
    - `itemtype`, `property`;
    - `key`: **omitted entirely** when the type declares no `@key` or the item
      has no key value. Absent and empty are different;
    - `from`: the old state, **present but empty** for an entry;
    - `to`: the new state, **present but empty** for an exit.
  - The multi-valued violation uses the same `failedConstraint`. Its message is
    `Transition violation: '<itemtype>' item '<key>' property '<p>' must be
    single-valued (found <n> values)`, it carries `from`, and omits `to`
    (pagelike wording).
  - The ambiguity violation: `message` = `Transition violation: type
    '<itemtype>' needs a @key property to pair items between writes (property
    '<p>')`, plus `itemtype` and `property` (pagelike wording, which contains
    the documented phrase "needs a `@key` property").
  - Title: `422 Unprocessable Entity - Transition Constraint Violation`
    (pagelike, following modeling R-MOD-71/72). Text is HTML-escaped.
  Clients must read it with a microdata parser. The message text is not a
  contract, and the documented example wraps it over lines.
- **Evidence:** documented (structure, fields, empty/absent rules),
  demo-source (`/transition/i` matched on the body, DEMO3 app.js:57-58),
  inferred (title, multi-valued/ambiguity wording). **Source:** D-TC §The 422
  body; D-SM §Watch the illegal step fail (keyless message);
  modeling R-MOD-72. **Confidence:** high (structure), low (non-documented
  wordings).
- **Cases:** `reacting.tc.recipe-end-to-end`, `reacting.tc.entry-without-rule-rejected`,
  `reacting.tc.delete-surprise-exit-needed`, `reacting.tc.key-pairing-rejects-with-key`.

### R-REACT-73 — Concurrent writes (412)
- **Behavior:** When two writes race on the same watched item, exactly one
  wins, and the other gets **412 Precondition Failed** (nothing applied) rather
  than being validated against stale state. After re-reading, a retry is
  checked against current state. A repeated step then fails 422 because the
  source state has moved on. On a site with transition rules, even an
  **unconditional** selector write that touches watched items may get 412
  when it overlaps a concurrent change.
  pagelike algorithm: at request arrival (R-REACT-8 step 3) record the version
  V0 of the target document. After taking the site write mutex, if the current
  version differs from V0 **and** the write's affected set (modeling R-MOD-14)
  contains an item watched by any constraint (evaluated on the current
  document), respond 412 with the standard Precondition Failed error document
  and write nothing. This check runs after `If-Match`/`If-Unmodified-Since`
  evaluation (reading-writing) and before validation. Writes that touch no
  watched item are unaffected. A request that arrives after another commit
  sees the new version at arrival and gets 422, not 412.
- **Evidence:** documented (outcomes), inferred (mechanism). **Source:** D-TC
  §Concurrent writes; D-SM §Watch the illegal step fail; SKILL:294-296,326
  ("On `412`, re-read the current state before retrying"). **Confidence:** high
  (outcomes), low (exact race window).
- **Contradiction (C15):** D-TC and D-SM say a retried step "then fails with
  422 because the source state has moved on". But when the retry writes the
  same target value that the winner already wrote (the docs' own example: both
  clients move pending → processing), the value is unchanged, and D-TC
  §Strictness says an unchanged value "is not a transition and always passes".
  DEMO3 (listing.html:71-78, app.js:105-109) confirms that a same-value write
  is "a no-op, not a rejected transition (verified)". Decision: the
  unchanged-value rule wins. A retry gets 422 only when its target differs from
  the current state.
- **Cases:** `reacting.tc.concurrent-same-step-one-wins`.

### R-REACT-74 — Taking effect
- **Behavior:** See R-REACT-5. Serving-path rule writes bind for the next
  write. On PageLove, WebDAV rule edits bind within 60 s. On pagelike they bind
  immediately.
- **Evidence:** documented. **Source:** D-TC §Taking effect. **Confidence:** high.

### R-REACT-75 — WebDAV bypasses constraints (the repair path)
- **Behavior:** Writes on the WebDAV authoring plane (PUT, DELETE, MOVE, COPY
  of documents) are never transition-validated and never fire handlers. This
  holds permanently, even when they create, change or delete watched items,
  and even for documents wedged by ambiguity or by a missing exit rule.
  WebDAV writes are still schema- and shape-validated (modeling C14).
- **Evidence:** documented. **Source:** D-TC §WebDAV bypasses constraints; D-SM
  §The repair path; D-TH §WebDAV edits never fire handlers; protocol R-PROTO-112.
  **Confidence:** high.
- **Cases:** `reacting.tc.webdav-bypass-repair`, `reacting.th.webdav-never-fires`.

---

## 12. Transition handlers

### R-REACT-76 — Item shape
- **Behavior:** Properties: `selector` (must name a type, R-REACT-77),
  `property` (the watched property), `becomes` (1..n; OR-ed), optional `when`
  (Sessel only), `action` (HttpRequest only). A handler missing `selector`,
  `property` or `becomes`, or having no `action`, never fires (inferred).
  Handlers are independent of constraints. They may watch a property no
  constraint watches, and then every change of it is permitted.
- **Evidence:** documented. **Source:** D-TH §Quick example, §When a handler
  fires ("repeated `becomes` values are OR'd"). **Confidence:** high / low
  (missing fields).

- **Live 2026-09-29:** a serving-path write refuses a TransitionHandler
  without exactly one `action` (Handler.action 1..1, R-REACT-4a).

### R-REACT-77 — The selector must name a type
- **Behavior:** Split the selector list on top-level commas. A branch
  **qualifies** only if it consists solely of type predicates:
  `[itemtype='T']` or `[itemtype="T"]` (the `=` operator only, any CSS quoting,
  optional ` i`/` s` flags not allowed), or `:isa('T')` (T or any
  schema-declared descendant). A compound made only of such predicates
  qualifies, and every predicate must accept the type (pagelike decision). A
  branch with anything else never fires: a tag, class, id, other attribute,
  another operator such as `*=`, a combinator, or another pseudo-class. The
  handler fires for an item if any qualifying branch accepts the item's type.
  Only the item's type is judged, never its position in the document. A handler
  with no qualifying branch never fires.
- **Evidence:** documented. **Source:** D-TH §The selector must name a type;
  D-SM §Notify a worker ("Class or structural selectors never fire a
  handler"). **Confidence:** high (listed forms), low (compounds, quoting).
- **Cases:** `reacting.th.class-selector-never-fires`,
  `reacting.th.selector-list-with-type-branch-fires`.

### R-REACT-78 — When a handler fires
- **Behavior:** After a serving-path write **commits**, compute the same item
  pairing as constraints (R-REACT-68/69) over the committed old and new
  states. For each qualifying handler h and each changed item whose type h
  accepts: let `so`, `sn` be the old and new states of h.property
  (R-REACT-65). Fire **once per item** when `sn` is present, `sn != so`, and
  `sn` is one of h's `becomes` values. So:
  - an item **created** in a watched state fires (an entry);
  - **exits never fire**;
  - an unchanged value fires nothing;
  - one write that changes several items fires once per item, each with its
    own Transition document;
  - an item with several values for the property does not fire, and the write
    still commits unless a constraint rejects it (R-REACT-66);
  - items in a handler-only ambiguous group are skipped (R-REACT-70).
  Firing never affects the write. The mutation is committed and the response is
  settled before handlers run, and nothing a handler does can fail or roll
  back the write.
- **Evidence:** documented. **Source:** D-TH §When a handler fires.
  **Confidence:** high.
- **Cases:** `reacting.th.*`.

### R-REACT-79 — The `when` gate
- **Behavior:** Optional, **Sessel only**, evaluated against the Transition
  document. `self` is the Transition item, so `self.path`, `self.selector`
  and `self.body` read its properties (cross-area Sessel element property
  access). `Context.request` and `Context.response` are not available. Absent:
  always deliver. Falsy: no delivery. Evaluation error: no delivery for that
  handler, and the committed write is unaffected. **Malformed** gate (not a
  Sessel item, including a JavaScript/Module item, or no `source`): the handler
  **never fires** (fails closed).
- **Evidence:** documented. **Source:** D-TH §The `when` gate. **Confidence:** high.
- **Cases:** `reacting.th.when-gate-*`, `reacting.th.js-gate-never-fires`.

### R-REACT-80 — Action restrictions
- **Behavior:** Each `action` must be an HttpRequest. A handler declaring a
  Sessel or JavaScript action **never fires** (pagelike: if any action is not
  an HttpRequest, the handler is disabled). `url`, `method` and every header
  value must be **plain text**. If any of them (including one header's value)
  is a Sessel or JS item, the handler does not fire at all and no request is
  sent. `retry`, `body` and `content-type` have **no effect**: the body is
  always the Transition document and `Content-Type` is always `text/html`. A
  `header` that sets `Content-Type` is also ignored (pagelike decision).
  `method` defaults to POST, as for other HttpRequests. Several HttpRequest
  actions each receive one delivery, in document order (inferred).
- **Evidence:** documented. **Source:** D-TH §The action, §Delivery ("A handler
  whose action uses an expression anywhere does not fire at all … such as an
  `Authorization` credential"). **Confidence:** high / low (several actions,
  Content-Type header).
- **Cases:** `reacting.th.sessel-action-never-fires`,
  `reacting.th.dynamic-header-disables-handler`,
  `reacting.th.body-and-content-type-ignored`.

### R-REACT-81 — The Transition document
- **Behavior:** The request body is this document, with this layout
  (whitespace is not a contract):

  ```html
  <!DOCTYPE html>
  <html>
    <head><title>Transition</title></head>
    <body itemscope itemtype="https://pagelove.org/Transition">
      <meta itemprop="path" content="/orders/order-1.html">
      <meta itemprop="selector" content="[itemtype=&quot;https://example.com/Order&quot;]:has([itemprop=&quot;orderNumber&quot;]:value-equals(&quot;10&quot;))">
      <div itemprop="body" itemscope itemtype="https://example.com/Order">
        <meta itemprop="orderNumber" content="10">
        <meta itemprop="status" content="processing">
      </div>
    </body>
  </html>
  ```

  - `path`: path of the modified document.
  - `selector`: addresses the changed item in that document. If the item's type
    has a `@key` and the item carries a value v for key property k:
    `[itemtype="T"]:has([itemprop="k"]:value-equals("v"))`. Otherwise
    `[itemtype="T"]` (the item was then necessarily alone of its type). Strings
    use double quotes with CSS string escaping (`\"`, `\\`). The attribute is
    HTML-escaped, so `"` becomes `&quot;`.
  - `body`: a deep copy of the changed element **as committed** by the write
    that fired. Its own `itemprop` attribute is set to `body`, replacing any
    original `itemprop` when the item was nested as another item's property.
    All other attributes and descendants are unchanged (attribute order is not
    a contract).
- **Evidence:** documented. **Source:** D-TH §What the action receives.
  **Confidence:** high (fields), medium (exact selector text).
- **Cases:** `reacting.th.recipe-notify-worker`, `reacting.th.nested-item-itemprop-replaced`,
  `reacting.th.keyless-selector-by-type`.

### R-REACT-82 — Delivery guarantees
- **Behavior:** Delivery runs in the background after the commit (and after the
  response, R-REACT-8 step 9). It is **at-most-once**: a single attempt, never
  retried, whatever `retry` says. A failed attempt, or a restart at the wrong
  moment, loses the notification. pagelike MAY persist pending deliveries, but
  it MUST mark a delivery as consumed **before** starting the attempt and MUST
  never re-send after a restart. The destination policy R-REACT-61 applies.
- **Evidence:** documented. **Source:** D-TH §Delivery; D-SM §Notify a worker.
  **Confidence:** high.
- **Cases:** `reacting.th.at-most-once-no-retry`,
  `reacting.th.failed-delivery-does-not-affect-write`.

### R-REACT-83 — Delivering to a PageLove host
- **Behavior:** A delivery to a PageLove (or pagelike) host is an ordinary
  request with no session. The target's rules apply, so it can be refused, for
  example with a 422 from a transition constraint, and the notification is
  then lost. It can also fire handlers in turn, **including the handler that
  sent it**, because the Transition document contains the watched item at the
  watched state. The chain stops by itself once the value is unchanged, but
  the second delivery overwrites the first at the target path. Authors add a
  `when` gate such as `self.path == "/orders.html"`. pagelike does nothing
  special beyond delivering in-process to its own sites (R-REACT-49).
- **Evidence:** documented. **Source:** D-TH §Delivery. **Confidence:** high.
- **Cases:** `reacting.th.delivery-to-own-host-is-ordinary-write`.

### R-REACT-84 — What never fires handlers
- **Behavior:** WebDAV authoring writes (permanently, by design), CRDT
  change-set PATCHes (in this version), exits, unchanged values, multi-valued
  items, handler-only ambiguous groups, handlers with malformed gates, non-type
  selectors, non-HttpRequest actions, or expression-valued url, method or
  headers. Side-effect writes (`Pagelove.PUT`/`DELETE`) **do** fire handlers,
  since they go through the serving path (inferred).
- **Evidence:** documented (all but side-effect writes). **Source:** D-TH
  §WebDAV edits never fire handlers, §Delivery ("CRDT change-set PATCHes do not
  fire handlers"). **Confidence:** high / low.

### R-REACT-85 — Duplicate deliveries
- **Behavior:** Two racing writes can each commit a matching change, so a
  worker can get the same logical notification twice. There is no
  deduplication. The docs say that with constraints the worker's second
  report-back fails with 422. That holds only when the report-back's target
  differs from the current state. A report-back that writes the value already
  stored is unchanged and passes (C15), so workers should compare before
  writing, or use conditional requests.
- **Evidence:** documented. **Source:** D-TH §Duplicate deliveries; D-SM
  §Notify a worker. **Confidence:** high.

---

## 13. Limits (pagelike)

### R-REACT-86 — Budgets, time and size limits
- **Behavior:** Trigger, processor and dynamic-property evaluation share the
  request's evaluation budget with composition (`TransactionBudget`, cross-area
  server-js and Sessel). Exhaustion is a runtime error (R-REACT-34, variant
  `timeout` or `out-of-memory`). pagelike limits:
  - at most 256 queued outbound requests per request (more is a runtime error);
  - outbound request bodies up to 1 MiB;
  - at most 64 side-effect writes per request.
  These limits are pagelike decisions (PageLove's are unknown).
- **Evidence:** documented (shared budget), inferred (numbers). **Source:**
  D-JSS §Resource limits. **Confidence:** medium / n/a.

---

## 14. Harness extension: outbound capture (`${SINK}`)

The harness README reserves `${SINK}` for outbound-HTTP cases but defines no
steps for it. The cases in this area use the following step kinds. Every such
case declares `requires: [outbound-http]`. The local target already advertises
that capability (`harness/target.go` `LocalTarget.Caps`), so until the runner
implements these steps and the `SINK` variable, those cases **fail loudly**
with "unsupported step keys". That is intentional: the failure is a to-do list
for the runner. The live target lacks the capability and skips them until a
public sink is configured. The proposed semantics:

- `${SINK}` expands to an absolute base URL unique to the case run (for example
  `http://127.0.0.1:43127/c-3f9a`). Cases append their own paths
  (`${SINK}/hook`). The sink records every request (method, path relative to
  the base, query, ordered header fields, body, arrival time).
- `sink_config: {path: /hook, responses: [500, 500, 200]}`: status codes
  returned to successive requests on that path. After the list is used up, the
  last entry repeats. The default is `[200]`. Optional `delay_ms`.
- `sink_expect`: waits up to `within_ms` for the **next unconsumed** request on
  `path` and consumes it. Assertions: `method`, `query`, `headers` (exact,
  names case-insensitive), `header_values` (all values of a name, in order),
  `header_matches` (regexp), `headers_absent`, `body`, `body_contains`,
  `body_not_contains`, `body_matches` (regexp), `body_json`, `microdata`
  (same forms as `expect`). `path` and every string assertion are
  `${…}`-expanded like `expect` strings (so `${P}` works in `path` and
  `body_contains`). `capture` works as usual.
- `sink_expect_none: {path, for_ms}`: no **further** request arrives on `path`
  within the window.
- `sink_expect_count: {path, count, within_ms, gaps_min_ms: [...], gaps_max_ms:
  [...]}`: at least `count` requests arrived in total (consumed ones
  included). Optional bounds on the time between consecutive arrivals check
  retry backoff. It consumes everything it counted.
- `${ORIGIN}` (already provided by the runner) is the public origin of the
  site under test. `reacting.th.delivery-to-own-host-is-ordinary-write` uses
  it to make a handler deliver to its own host.

Other harness facts the cases rely on: `site.files` are uploaded over WebDAV
(so live cases install reaction documents with a public `PUT` step instead,
R-REACT-5), and `expect.microdata` in list form matches items anywhere in the
body, nested `Violation` items included.

---

## 15. Cross-area dependencies

- **permissions-identity**: glob syntax and path forms (R-PERM-19/20/22) for
  `resource`; the request key element (R-PERM-24) for `selector`; authorization
  **before** triggers (C1; R-PERM-53 P-18); `Pagelove.PUT` authorization as the
  requesting principal; `request.auth` shape for `ctx.request.auth`
  (R-PERM-74); `Authorization` headers are not identity on the public plane
  (R-PERM-71), which is what makes SHOP's Basic-auth trigger possible.
- **modeling**: subtype discovery through `parent` (R-MOD-10); the write
  pipeline and the position of transitions in it (R-MOD-15 step 10); affected
  instances (R-MOD-14) for the 412 rule; primary keys (R-MOD-33) for pairing and
  Transition selectors; `@key` → `id` materialization for `Pagelove.PUT`
  (R-MOD-34); ConstraintViolation and BindingFailure envelopes (R-MOD-71..73);
  platform-schema validation of TransitionConstraint items (R-REACT-62);
  WebDAV still validates schemas (C14).
- **sessel**: `@schema` imports; `throw`/`try`/`catch`; `new T { … }`
  construction with repeated properties (`header:`); **property assignment**
  on `Context.response.status/body` (the grammar in D-SES does not show `=`
  assignment; the docs and D-BLOG rely on it); element property access
  (`self.path`); the site-wide default selector scope; truthiness; errors such
  as `unknown function` and `invalid assignment target`; the positional
  `HTTPResponse(status, message)` form (C12).
- **server-js**: module contract, sandbox, budgets, failure variants, value
  marshalling (truthiness of converted values, C11).
- **reading-writing**: status codes of successful writes (201/200/206/204), the
  Range grammar that defines "the request's selector", conditional requests
  (412 before transition validation), error documents (`internal/errdoc`),
  body size limits, PATCH (501 in pagelike).
- **sse**: side-effect writes emit events (R-SSE-23). Trigger-thrown and
  transition-rejected writes emit nothing (R-SSE-18). Transformed bodies appear
  in the request's own event.
- **protocol**: WebDAV bypass (R-PROTO-112/113); QUERY is safe (R-PROTO-40);
  the 60-second note for WebDAV rule edits.
- **composing-pages**: resource bindings on trigger elements (D-RB); transient
  elements (R-REACT-42); parameterized routes (concrete path matching); HEAD
  composition feeding processors.
- **store/plan**: side-effect writes commit outside the main transaction
  (R-REACT-36); the `outbox` table (R-REACT-59); reaction item cache and
  per-request snapshot (R-REACT-5); the version snapshot for 412 (R-REACT-73).

---

## 16. Contradictions and compatibility decisions

| # | Topic | Claims | Decision |
|---|---|---|---|
| C1 | Authorization vs triggers | Docs: triggers run "before the server processes a request" and do not place authorization. ATS relies on rules to keep anonymous users out of an outbound-mail trigger. | Authorization first. Denials skip triggers and core but still pass through processors (R-REACT-8). Probe P-1. |
| C2 | Header-name case in `Context.request.headers` | Docs index `"Authorization"`. SHOP:16-18: over HTTP/2 only lower-case lookup works, and the canonical form silently misses. POLLS uses lower case. | Lower-case keys, case-insensitive lookup (R-REACT-23). Canonical-case case marked `disputed`. Probe P-2. |
| C3 | Dispatch vs response order | Lifecycle list: dispatch (5) before response (6). Prose: dispatched after the response. | Send after the response (R-REACT-56). |
| C4 | Queue after trigger throw or error | Lifecycle: a throw skips steps 3–4 only. No statement on step 5. | Queued requests are still sent (R-REACT-9, R-REACT-56). Case marked low. Probe P-4. |
| C5 | `*` in `resource` | D-WH: `/notes/*` matches "any direct child". AuthorizationRule docs and D-PROC's bare `*`: crosses `/`. | One engine, `*` crosses `/` (R-REACT-12). Case `disputed`. Probe P-5. |
| C6 | Selector filter on POST append | D-WH: `selector [itemtype=Note]` with `POST` fires "only on creates … whose target element has the Note itemtype". D-TRIG: the element targeted by the request. | Key element = the list (R-REACT-14). The recipe as written would not fire for a plain list. Probe P-6. **Superseded (live 2026-09-29):** the selector filter is not evaluated, so the recipe fires. |
| C7 | Triggers reading stored state | DEMO-RM: "A Trigger sees only the incoming request". SHOP/POLLS triggers query stored documents. | Store queries work (R-REACT-24). |
| C8 | Sessel-thrown body | Docs: body is a string. BJ-CORS (live): Sessel re-serializes it as HTML. JS keeps bytes. | Reproduce the re-serialization for Sessel only (R-REACT-32). Probe P-8. |
| C9 | Processor header writes | D-PROC §Pass-through: writes to `Context.response.headers` affect the response. BJ-CORS: `.set` unknown, index assignment invalid, header writes dropped. | Header writes have no effect. Throw to add headers (R-REACT-45). Probe P-9. |
| C10 | HttpRequest itemtype spelling | Reacting pages: `HttpRequest`. Resource Binding page: `HTTPRequest` (plus an undocumented `transformation`). | Accept both spellings. Ignore `transformation` (R-REACT-48). Probe P-13. |
| C11 | JS `when` truthiness | Docs list `null, false, 0, ""` as falsy. JS `[]`/`{}` are truthy in JS, falsy in Sessel. | Convert, then apply Sessel truthiness (R-REACT-17). Probe P-14. |
| C12 | Sessel HTTPResponse constructor | D-JSS mentions `new HTTPResponse(status, message)` as Sessel's form. Every Sessel example uses `new HTTPResponse { … }`. | Brace form required. Positional form SHOULD also work (cross-area Sessel). |
| C13 | Default retry | Docs: `retry` defaults to 0. ATS:6-8: "failures retried by Pagelove with exponential backoff" with no `retry`. | Docs win: default 0 (R-REACT-57). The ATS text is outdated or aspirational. |
| C14 | Processor on missing/erroring response bodies | D-PROC shows processors on 404s. Nothing on denials. | Processors run on all core responses including denials (R-REACT-10). Probe P-1b. |
| C15 | Retried or duplicate steps | D-TC §Concurrent writes, D-SM and D-TH §Duplicate deliveries: a repeated step "fails with 422". D-TC §Strictness: an unchanged value "always passes". DEMO3: same-value writes are no-ops (verified). | Unchanged value passes. 422 only when the retry's target differs from the current state (R-REACT-73, R-REACT-85). |

Competing claims that pagelike decided against are kept as cases marked
`status: disputed`, so a live run shows which claim PageLove follows:

| Decision | Case that pagelike passes | Disputed case (other claim) |
|---|---|---|
| C1 | `reacting.lifecycle.authz-denial-before-triggers` | `reacting.lifecycle.triggers-before-authz` |
| C2 | `reacting.context.header-lookup-canonical-case` | `reacting.context.header-lookup-canonical-case-misses-http2` |
| C5 | `reacting.filters.resource-star-crosses-segments` | `reacting.filters.resource-star-direct-child-only` |
| C6 | `reacting.filters.selector-post-append-key-element-is-list` | `reacting.filters.selector-post-append-matches-fragment` |
| C8 | `reacting.processor.sessel-rethrow-escapes-non-html` | `reacting.processor.sessel-rethrow-keeps-bytes` |
| C13 | `reacting.outbound.retry-default-zero-single-attempt` | `reacting.outbound.retry-default-ats-claim` |

---

## 17. Open questions for live probing

Each probe is a minimal sequence against a disposable PageLove host. `P` is a
fresh prefix. Reaction documents are written with a public-plane `PUT` (allowed
by a rule `* → P/* : GET, HEAD, PUT, POST, DELETE`) unless stated otherwise.
`SINK` is a public request-capture URL.

- **P-1 Authorization vs triggers.** Rule `* PUT P/d/x.html Deny`. Trigger
  `resource P/d/*`, `method PUT`, Sessel action `throw new HTTPResponse
  {status: 409}`. Anonymous `PUT P/d/x.html` → 401 (authorization first) or
  409 (triggers first)? Repeat with an HttpRequest action to `SINK/p1`: does
  `SINK` receive anything?
  **P-1b** Rule `* GET P/d/secret.html Deny`, processor `status 401` that sets
  `Context.response.body = "processed"`. Anonymous GET → is the body
  replaced?
- **P-2 Header case.** Trigger with `when` `Context.request.headers["Authorization"]
  != null` and `otherwise` throwing 401. Send `PUT` with `Authorization: Basic
  dGVzdDp0ZXN0` over HTTP/1.1 and over HTTP/2. Repeat with
  `headers["authorization"]`. Record which lookups succeed on each protocol.
- **P-3 Header joining.** Trigger throws with `body: Context.request.headers["x-a"]`.
  Send two `X-A` field lines (`1`, `2`). Record `1, 2` vs `1` vs `2`.
  Repeat with `?a=1&a=2` and `Context.request.query["a"]`.
- **P-4 Queue after throw.** Trigger A (HttpRequest to `SINK/p4`) in `P/r/a.html`,
  trigger B throwing 409 in `P/r/b.html`. PUT → 409. Does `SINK/p4` get a
  request? Repeat with B raising a runtime error (`1 / "x"`), and with a core
  failure (a closed ShapeConstraint rejecting the body with 422).
- **P-5 Glob across segments.** Trigger `resource P/d/*` throwing 409. `PUT
  P/d/a/b.html` → 409 (crosses) or success (direct children only)?
- **P-6 Selector filter on POST.** Document `<ul id="l"></ul>`. Trigger
  `selector [itemtype='T']`, `method POST`, throwing 409. `POST Range:
  selector=#l` with body `<li itemscope itemtype="T">…</li>` → 409 or 206?
  Repeat with trigger selector `#l`.
- **P-7 Selector with no match.** Trigger `selector h1` throwing 409.
  `PUT Range: selector=#missing` → 409 (filter passes) or 416 (filter fails)?
- **P-8 Sessel body re-serialization.** Trigger throwing `new HTTPResponse
  {status: 409, body: "a && b => c"}` → body `a &amp;&amp; b =&gt; c` or raw?
  Same with `message`.
- **P-9 Processor header writes.** Processor Sessel action
  `Context.response.headers = {"x-probe": "1"}` → header present? Error?
  Repeat with `Context.response.headers["x-probe"] = "1"` (expected error).
- **P-10 Failing side-effect write.** Rule denies PUT on `P/locked/*`. Trigger
  action `Pagelove.PUT(new Thing {name: "x"}, "P/locked/a.html")`. Request →
  status? Is the main write applied? Wrap the call in `try { … } catch (e) {
  … }`: does the request succeed?
- **P-11 Nested triggers.** Trigger T1 on `PUT P/d/*` writes
  `Pagelove.PUT(…, "P/e/x.html")`. Trigger T2 on `PUT P/e/*` throws 409. Is
  the main PUT 500/409 (T2 ran on the nested write) or success (it did not)?
  Which actor performs the nested write (rule allowing only `alice` on `P/e/*`)?
- **P-12 Transient writes from triggers.** Document with `<ul id="cart"
  p:transient>`. A trigger performs `Pagelove.PUT(new ul …, "P/d/cart.html")`
  on a GET. Does the requesting session see the change? Do others?
- **P-13 Itemtype alias.** Trigger action with itemtype
  `https://pagelove.org/HTTPRequest` to `SINK/p13`. Is it sent?
- **P-14 JS truthiness.** JS `when` returning `[]`, `{}`, `undefined`, `NaN`,
  and a Promise. Record for each whether the action runs.
- **P-15 Thrown response defaults.** Trigger throwing `{status: 409}` (JS, no
  body/message). Record Content-Type, body, headers. Same for Sessel
  `new HTTPResponse {}`, which is expected to give 500.
- **P-16 Runtime error shape.** Sessel action `1 / "x"`. Record status,
  Content-Type, body itemtypes (`BindingFailure`? `Error`?).
- **P-17 Processor body assignment headers.** Processor sets
  `Context.response.body = "x"` on a GET that returns an `ETag`. Is `ETag`
  still present? Is `Content-Length` 1?
- **P-18 Processor chain.** Processor A (`/r/a.html`) sets status 404 on a 200.
  Processor B (`/r/b.html`, `status 404`) sets body `"B"`. Response body `B`?
- **P-19 Retry schedule.** HttpRequest `retry 3` to a sink returning 500.
  Record arrival times (expected gaps ≈2, 4, 8 s). `retry 9` gives 5 attempts?
  Redirect 302 → followed or failure?
- **P-20 Relative url.** JS url `"/p20-target.html"` with method PUT and a body.
  Does the host receive an anonymous PUT to itself?
- **P-21 Keyless ambiguity "touches".** Two keyless Orders (`pending`,
  `pending`), constraint entry `pending`. Whole-document PUT changing only a
  `total` → 200 or 422?
- **P-22 MOVE of a watched item.** Constraint on Order.status with no exit
  rule. `MOVE` the Order element to another document → 422 or success?
- **P-23 Multi-valued old state.** Via WebDAV store an Order with two `status`
  values. Public PUT setting a single `processing` → which rule is required
  (entry or none)?
- **P-24 412 window.** Two parallel selector PUTs `pending → processing` on the
  same order, repeated 20 times. Record the distribution of {412, 422}
  outcomes for the loser.
- **P-25 WebDAV propagation for triggers.** Upload a trigger (throws 409) over
  WebDAV. PUT through the public plane immediately, then after 10 s, 30 s and
  65 s. When does it start firing? Same for processors and handlers.
- **P-26 Handler for side-effect writes.** Trigger `Pagelove.PUT` creates an
  Order in state `processing`. Handler `becomes processing` → does `SINK/p26`
  receive a Transition?
- **P-27 Handler Content-Type header.** Handler action with a `header`
  `Content-Type: application/json`. Delivered Content-Type?
- **P-28 Handler `when` on body.** `when` `self.body.status == "processing"` (or
  the equivalent property access). Does Sessel element property access work on
  the Transition item?
- **P-29 Platform schema for constraints.** Public `PUT` of a TransitionConstraint
  with neither `from` nor `to` → 422 (body shape?). With `selector` missing →
  422 or accepted?
- **P-30 Trigger on SSE subscribe.** Trigger on `GET` throwing 409. `GET` with
  `Accept: text/event-stream` → 409 or a stream?

---

## 18. Case index

154 cases in `harness/cases/reacting/`. 6 are `status: disputed` (§16), 1 is
local-only (`live: false`), 4 need `slow`, 2 need `webdav` beyond setup, 1
needs `multi-actor`, and 48 need `outbound-http` (the sink steps of §14).

| File | Cases | Requirements |
|---|---|---|
| `discovery.yaml` | 6 | R-REACT-2, 4, 5 |
| `filters.yaml` | 12 | R-REACT-11..15 |
| `gates.yaml` | 10 | R-REACT-4, 17, 18, 23, 24, 29, 34 |
| `actions.yaml` | 11 | R-REACT-9, 18, 29..31, 34 |
| `order.yaml` | 3 | R-REACT-9, 33, 35 |
| `context.yaml` | 9 | R-REACT-22..25 |
| `writes.yaml` | 8 | R-REACT-36..41 |
| `lifecycle.yaml` | 3 | R-REACT-7..9 |
| `processor.yaml` | 18 | R-REACT-10, 13, 15, 17, 26, 32, 33, 35, 36, 43..47 |
| `outbound.yaml` | 18 | R-REACT-48..56 |
| `outbound-retry.yaml` | 6 | R-REACT-57, 58 |
| `transition-constraint.yaml` | 16 | R-REACT-5, 62..72 |
| `transition-identity.yaml` | 6 | R-REACT-68..70, 72 |
| `transition-concurrency-webdav.yaml` | 4 | R-REACT-5, 73..75 |
| `transition-handler.yaml` | 24 | R-REACT-36, 49, 66, 70, 75..85 |
