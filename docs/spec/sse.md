# SSE — live mutation streams (behavioral spec)

Area: `sse`. Package: `internal/sse` (see `docs/design.md`). Cases: `harness/cases/sse/`.

This document specifies what a pagelike server must do so that clients written
for PageLove's Server-Sent Events (SSE) stream work unchanged. It is written
from third-party evidence (docs snapshot 2026-09-28 and pinned upstream code);
everything not stated by a source is marked `inferred` and says why.

Keywords MUST / SHOULD / MAY are normative for pagelike.

## 0. Sources and abbreviations

Docs (Markdown snapshot `research/docs/2026-09-28/md/`):

| Tag | Page | Notes |
|---|---|---|
| D-SSE | `reference/reading-and-writing/Server-Sent-Events` | Identical (byte-for-byte except a trailing header div) to the SSE section of `all/reference/reading-and-writing` (lines 875–1066). |
| D-JS | `languages/javascript/client/server-sent-events` | Identical to the SSE section of `all/languages/javascript` (lines 1671–1811). |
| D-AR | `reference/permissions/AuthorizationRule` | §Conflict resolution repeats the SSE default-GET exception. |
| D-GET, D-PUT, D-POST, D-DEL, D-MOVE | `reference/reading-and-writing/*-method` | |
| D-INC, D-STAMP, D-ROUTE | `reference/composing-pages/Includes`, `Stamp`, `Parameterized-Routes` | write-through to origin |
| D-TR | `reference/composing-pages/Transient-Elements` | session cookie `pagelove_session` |
| D-TRIG, D-PROC | `reference/reacting-to-changes/Trigger`, `Processor` | side-effect writes |
| D-TH, D-TC | `reference/reacting-to-changes/TransitionHandler`, `TransitionConstraint` | WebDAV and CRDT notes |
| D-DAV | `reference/protocol/WebDAV` | |
| D-GLOSS | `reference/glossary/method` | PATCH = CRDT change-set |
| D-SM | `recipes/declaring-a-state-machine` | "published on the SSE mutation stream" |
| D-CN | `reference/reading-and-writing/Content-Negotiation` | `Vary: Accept` |

Code (`research/upstream/`, commits from `research/COMMITS.txt`):

| Tag | File |
|---|---|
| BJ-SSE | `beta-js/pagelove/sse.mjs@c204746` (official client) |
| BJ-PRIM | `beta-js/pagelove/primitives.mjs@c204746` |
| BJ-TEST | `beta-js/test/sse-echo.test.mjs@c204746`, `beta-js/test/helpers/dom.mjs@c204746` |
| POLLS | `pagelove-polls/site/assets/poll.js@c9270e5` (connection-token flow) |
| KANBAN | `pagelove-kanban/site/app.js@85109ab` |

`demo-apps@c4dd883` (demo-01..05) contains no `EventSource` usage; the
`pagelove`, `pagelove-primitives`, `dom-*`, `pagelove-ats`, `pagelove-shop`
repositories contain no SSE code (searched 2026-09-28). The earlier research
notes (`understanding.md`, `read-probes.json`) contain no SSE observations; the
live probes cover only GET/OPTIONS/QUERY.

## 1. Model in one paragraph

A client subscribes to one document path with `GET` + `Accept: text/event-stream`.
The server authorizes it like a read but never via the default-GET mode, answers
`200` with an open `text/event-stream`, sends a `pagelove-connection` event whose
data is a fresh opaque token, then streams one `mutation` event per committed
write to that exact path (never to pages that merely include it). Each mutation
event has a server-assigned id (`<ms>-<seq>`), and its data is an HTML
`<article>` carrying Microdata properties `method`, `selector`, `etag`, `path`,
`host`, `body`, plus `placement` (POST, MOVE) and `destination` (MOVE). Events are
retained 10 minutes; a reconnect with `Last-Event-ID` replays newer events or gets
a `reset` (`events-expired`). A `: ping` comment is sent every 20 s. A slow
reader whose bounded buffer fills is disconnected. A write is never echoed to the
connection that made it: by default all streams of the writer's session are
skipped; a `Pagelove-Connection: <token>` header on the write narrows that to the
one matching stream.

## 2. Requirements

Format of each requirement: behavior, then **Evidence** · **Confidence**,
**Source**, **Edge cases**, **Status/shape** where relevant.

### A. Recognizing and admitting a subscription

#### R-SSE-1 — Subscribe request
A request is a *subscribe request* iff its method is `GET` and its `Accept`
header contains the media range `text/event-stream` (type/subtype compared
case-insensitively, parameters ignored, `q` > 0). Everything else about the URL
is the same resource a `GET` would address.
- **Evidence:** documented (method + Accept) · inferred (Accept parsing rules) · **Confidence:** high (GET+Accept), medium (parsing details)
- **Source:** D-SSE §Subscribing; D-AR §Conflict resolution ("a `GET` with `Accept: text/event-stream`").
- **Edge cases:**
  - Browsers' `EventSource` always sends exactly `Accept: text/event-stream`; supporting that exact value is the compatibility floor.
  - `Accept: */*`, `text/*` or absent MUST NOT start a stream (wildcards do not count) — otherwise ordinary fetches would hang. Inferred.
  - `text/event-stream;q=0` is not a subscribe. Inferred.
  - `HEAD` with `Accept: text/event-stream` is an ordinary HEAD, never a stream. Inferred (HEAD has no body).
  - The doc's live example writes `SUBSCRIBE /path` — that is notation for GET+Accept, not an HTTP method; pagelike MUST NOT treat a literal `SUBSCRIBE` method as a subscription.
  - A `Range: selector=…` header on a subscribe request is ignored; the stream always covers the whole document (inferred from "A subscription covers the path you subscribed to"; low confidence, see Q24).

#### R-SSE-2 — Subscription path
The subscription key is the stored document path the equivalent `GET` would
serve: query string and fragment are discarded; `/dir/` resolves to
`/dir/index.html`; a slash-less directory path gets the same `301` a GET gets.
- **Evidence:** documented (query `?conn=` ignored) · inferred (general query stripping, directory mapping) · **Confidence:** high for `?conn=`, medium for other query strings, low for directories
- **Source:** D-SSE §Echo suppression (`?conn=` ignored); BJ-SSE:36 (default subscription URL is `location.href` minus `#`, i.e. keeps the query string, so the server must ignore it for events to arrive); POLLS:7,205 and KANBAN:397 subscribe to `location.pathname`; D-GET §Directory requests.
- **Edge cases:** `/doc.html?x=1` and `/doc.html` are the same subscription. A subscriber to `/dir/` receives events for writes to `/dir/index.html` (and vice versa) and the event's `path` is `/dir/index.html` (Q14).

#### R-SSE-3 — Authorization
A subscribe request is authorized exactly like a whole-document `GET` of the
resolved path by the same principal (method token `GET`, resource-level
evaluation, actor specificity, deny-wins), with one difference: when **no rule
matches**, the request is denied even if the host's default-GET mode would grant
an ordinary GET. An explicit `Allow` rule for `GET` (e.g. `* /polls/* GET Allow`)
is what makes a path subscribable.
- **Evidence:** documented · **Confidence:** high
- **Source:** D-SSE §Subscribing; D-AR §Conflict resolution (last two sentences); POLLS app rules (`pagelove-polls/site/admin/auth.html@c9270e5:28-34` grant `*` GET on `/polls/*`, which is what makes its live stream work).
- **Status/shape:** denial is answered before any stream starts, with the same response the permissions area uses for a denied GET: `401 Unauthorized` for an anonymous principal, `403 Forbidden` for an authenticated one, `Content-Type: text/html…`, body an HTML error document (shape owned by the permissions area; D-AR §Testable examples shows `itemtype="https://pagelove.org/1.0/Error"`). No `text/event-stream` content type on denials.
- **Edge cases:**
  - Default-GET `allow` + no rule: plain GET → 200, subscribe → 401/403.
  - Explicit `Deny` for GET → subscribe denied (same as GET).
  - D-GET's error table says "Authorization denied → 403" without distinguishing; D-AR and D-MOVE show 401 for unauthenticated. Decision: 401 anonymous / 403 authenticated (medium).
  - Selector-scoped GET rules: the subscribe has no selector, so it is decided like a whole-document GET (cross-area: permissions).
  - Rules changing after the stream opened: see R-SSE-28 (re-check per event).

#### R-SSE-4 — Subscribing to a path with no document
After authorization succeeds, if no document exists at the resolved path the
server SHOULD answer `404 Not Found` (same error document as GET) and not open a
stream.
- **Evidence:** inferred (mirrors GET; an `EventSource` that gets a non-200 stops reconnecting, which is the right outcome for a deleted page) · **Confidence:** low
- **Source:** D-GET §Error cases (by analogy). Nothing in D-SSE addresses it (Q13).
- **Edge cases:** authorization is decided first, so an unauthorized subscriber never learns whether the document exists (consistent with "absence and denial are distinct" in D-PUT/D-DEL). An already-open stream is *not* closed when its document is deleted (R-SSE-17).

> **Superseded by live observation (2026-09-28) (Q13 settled).** An
> authorized subscribe to a path with no document opens a normal `200`
> stream, which then carries the document's creation when it happens.
> Authorization is still decided first. Case:
> `sse.subscribe.missing-document-404`.

### B. The stream

#### R-SSE-5 — Response head and framing
A successful subscription is answered `200 OK` with:
- `Content-Type: text/event-stream` (no other media type; pagelike SHOULD send it without parameters, exactly as documented);
- `Cache-Control: no-cache`;
- no `Content-Length`, `ETag`, `Last-Modified`, `Content-Range`;
- `Vary: Accept` SHOULD be present (D-CN says every response carries it; inferred for streams).

The body is UTF-8, lines end in LF (`\n`), each field is written `name: value`
(one space after the colon), and each event ends with one blank line. The server
MUST flush after every event and comment. The response is never stored by any
shared cache (D-GET §Caching: "SSE streams are never cached").
- **Evidence:** documented (status, two headers, never cached) · inferred (framing details from the WHATWG SSE format the docs link to) · **Confidence:** high / medium
- **Source:** D-SSE §Subscribing (example response lists `content-type: text/event-stream` and `cache-control: no-cache`); D-GET §Caching.
- **Edge cases:** HTTP/2 lowercases header names (as in the doc sample); HTTP/1.1 uses chunked transfer. pagelike behind a proxy SHOULD also send `X-Accel-Buffering: no` (harmless extra header). A `Set-Cookie` establishing a session MAY appear (cross-area identity; Q28).

> **Superseded by live observation (2026-09-28).**
> - The head is exactly `Content-Type: text/event-stream` and
>   `Cache-Control: no-cache`, with **no** `Vary`.
> - The body opens with the comment `: connected` and a blank line, before
>   the `pagelove-connection` event.

#### R-SSE-6 — First event: `pagelove-connection`
The first event on every stream (fresh or reconnecting, before any replayed
event, reset, or mutation) is:

```
event: pagelove-connection
data: 6fQ2mZ0bXr8Kx1Tq9sVw3A

```

Its data is an opaque token identifying this one stream. pagelike rules:
- MUST be sent immediately after the response head (it also flushes the head);
- MUST NOT carry an `id:` field (an id would overwrite the browser's last-event-id and corrupt replay) and SHOULD NOT carry `retry:`;
- token: at least 128 bits of randomness, encoded with characters valid in an HTTP header value and in SSE data (pagelike: base64url, no padding, no whitespace);
- a new token for every stream, including each automatic reconnect; tokens are never reused.
- **Evidence:** documented (first event, name, opaque token, server-assigned) · client-source (POLLS:206 reads `e.data` verbatim and echoes it) · inferred (no id, uniqueness per reconnect, format) · **Confidence:** high / medium
- **Source:** D-SSE §Echo suppression (the event "arrives before any mutation event"; the server "always assigns the token"); POLLS:26,206,80.
- **Edge cases:** a `?conn=` query value never becomes the token (R-SSE-39). Two concurrent streams of the same session get different tokens.

> **Superseded by live observation (2026-09-28), token format.**
> - The token is a lowercase UUID version 4 (e.g.
>   `061c847e-c5e9-424f-b0d0-ab8f709a4307`), not base64url.
> - The event follows the `: connected` comment (R-SSE-5), so it is the first
>   **event** but not the first bytes.
>
> Every other rule holds.

#### R-SSE-7 — Mutation event framing
```
id: 1790000000123-0
event: mutation
data: <article itemscope itemtype="https://pagelove.org/Mutation">
data:   <span itemprop="method">PUT</span>
data:   <span itemprop="selector">main > h1</span>
data:   <span itemprop="etag">"9c1e…"</span>
data:   <span itemprop="path">/pages/index.html</span>
data:   <span itemprop="host">example.pagelike.test</span>
data:   <div itemprop="body"><h1>Hello
data: again</h1></div>
data: </article>

```
Field order `id`, `event`, then `data` lines. The article text is split on line
breaks and each line becomes one `data:` line (so a body that contains newlines
spans several `data:` lines; the client re-joins them with `\n`). CR LF and lone
CR inside the payload are normalized to LF before splitting.
- **Evidence:** documented (id/event/data layout, article, two-space indentation of property lines) · inferred (splitting rule — required by the SSE format for any multi-line payload) · **Confidence:** high
- **Source:** D-SSE §Mutation events.
- **Edge cases:** an empty payload line is sent as `data: ` (or `data:`), never omitted, so the joined data keeps the same line structure.

#### R-SSE-8 — The Mutation article
The data of a `mutation` event is exactly one element
`<article itemscope itemtype="https://pagelove.org/Mutation">` whose Microdata
properties are, in this order:

| # | element | itemprop | presence |
|---|---|---|---|
| 1 | `<span>` | `method` | always |
| 2 | `<span>` | `selector` | always (may be empty, R-SSE-17) |
| 3 | `<span>` | `etag` | when the server has an element ETag for the result (R-SSE-13) |
| 4 | `<span>` | `path` | always |
| 5 | `<span>` | `host` | always |
| 6 | `<div>` | `body` | always (empty for DELETE) |
| 7 | `<span>` | `placement` | POST and MOVE only |
| 8 | `<span>` | `destination` | MOVE only |

Serialization constraints (these are what the known clients rely on):
- The body opening tag MUST be literally `<div itemprop="body">` (no other attributes, double quotes) — POLLS:213 locates the body by that exact string.
- No `</div>` may appear after the body's closing tag — POLLS:215 takes the body as everything up to the *last* `</div>` in the payload. Hence every property after `body` MUST be a `<span>` (or other non-div element).
- The body content is raw, unescaped HTML (the fragment itself), not text.
- Text properties are HTML text: escape `&` and `<`; `>` SHOULD be left literal (the documented example prints `main > h1`). Clients read them with `textContent` (BJ-SSE:119-123; POLLS:212).
- Clients locate properties with `querySelector('[itemprop="…"]')`, so extra properties MAY be added later without breaking them, but pagelike MUST NOT add a second element with any of these itemprop names.
- **Evidence:** documented (itemtype, method/selector/etag/path/host/body spans+div, placement row) · client-source (destination; body-by-innerHTML) · demo-source (literal body tag, body-last) · **Confidence:** high for property set; medium for order after `body`
- **Source:** D-SSE §Mutation events, §Mutation event fields; BJ-SSE:113-135; POLLS:207-218; BJ-TEST `helpers/dom.mjs:83-94` (placement span after body, no etag).
- **Edge cases:** microdata parsers see `body`'s value as its text content; clients that need HTML use `innerHTML`/string slicing. A body containing `<tr>`/`<td>` must be emitted unchanged even though an HTML parser would drop it outside a table (POLLS:207-209 explains the slicing workaround).

#### R-SSE-9 — `method`
Uppercase HTTP method of the write that caused the event: `PUT`, `POST`,
`DELETE`, or `MOVE`. (`PATCH` is reserved, R-SSE-25.)
- **Evidence:** documented (PUT/POST/DELETE) · client-source (MOVE, BJ-SSE:220-239) · **Confidence:** high / medium
- **Source:** D-SSE §Mutation event fields (lists three methods but the `placement` row mentions MOVE; see Contradiction C1).
- **Edge cases:** one write request → exactly one mutation event (no DELETE+POST pair for a MOVE).

#### R-SSE-10 — `selector`
For element-scoped writes, the selector the writer sent in `Range: selector=…`,
verbatim after (a) removing the `selector=` unit prefix, (b) removing any
`; placement=…` parameter, (c) trimming surrounding whitespace. It is *not*
rewritten to a canonical path. For MOVE it is the source (`Range`) selector.
For whole-document writes it is empty (R-SSE-17).
- **Evidence:** client-source · **Confidence:** medium
- **Source:** BJ-SSE:166-172 matches incoming events to local writes by comparing `selector` strings with the request's Range value (BJ-PRIM:286-287 derives it with `range.replace("selector=", "")`) and then resolves it with `document.querySelector`; POLLS:219-236 uses the event selector as the container for POST and the target for PUT/DELETE; BJ-TEST uses identical selectors for write and event. D-SSE only says "CSS selector identifying the mutated element".
- **Edge cases:**
  - Multi-match selectors (e.g. `li`): writes act on the first match (D-AR §Selectors that match several elements), and subscribers resolve the same selector to their first match, so the verbatim selector stays correct.
  - POST `placement=before|after`: `selector` is the anchor, not the new node (D-SSE §Applying a POST event).
  - Write-through (includes/stamps): see R-SSE-19 — the request selector may not match in the origin document (Q12).

#### R-SSE-11 — `path`
Absolute path (leading `/`, no scheme/host/query) of the stored document the
change landed in. For write-through writes this is the origin document, not
the composed page that was addressed.
- **Evidence:** documented ("Document path") · inferred (origin for write-through, from D-SSE §Composed resources + D-INC) · **Confidence:** high / medium
- **Source:** D-SSE §Mutation event fields; POLLS:221 drops events whose `path` differs from the page's `location.pathname`; D-JS §Examples (filter by `event.detail.path`).
- **Edge cases:** directory index documents report `/dir/index.html` (Q14). Percent-encoding: pagelike reports the decoded stored path (Q14).

> **Superseded by live observation (2026-09-28) for write-through.** A write
> addressed to a composed page reports the **written page's** path, not the
> origin's (R-SSE-19 as reconciled). Case:
> `sse.scope.write-through-event-on-origin`.

#### R-SSE-12 — `host`
The virtual host of the site the document belongs to. pagelike: the site's
primary host name, lowercase, without port, identical for every subscriber
regardless of which alias they connected through.
- **Evidence:** documented ("Virtual host") · inferred (canonical, no port) · **Confidence:** medium
- **Source:** D-SSE §Mutation events (example subscribes on `example.pagelove.org` and the event says `example.com`, suggesting the site's own host name rather than the request's; Contradiction C7).

#### R-SSE-13 — `etag`
Element-level ETag of the content after the write, written as the exact string
the write's own `ETag` response header carried for that element (including the
double quotes, e.g. `"9c1e…"`), so that a client can send it back verbatim as
`If-Match`. Present for PUT (replacement element), POST (the inserted element
when it is a single element; otherwise omitted), MOVE (the moved element).
Omitted for DELETE and whole-document deletes.
- **Evidence:** documented (property exists, "Element-level ETag of the mutated content") · client-source (BJ-SSE:247-249 stores `mutation.etag` on the resulting element; BJ-PRIM:266-268 later sends `element.etag` verbatim as `If-Match`) · inferred (quoting, equality with the response header, per-method presence) · **Confidence:** high (present on PUT) / low (rest)
- **Source:** D-SSE §Mutation event fields; D-JS (`etag` "when supplied"); BJ-TEST payloads omit it.
- **Edge cases:** the doc example prints `a1b2c3...` unquoted — an illustration (Contradiction C6). Cases never compare ETag literals; only the relationship "event etag == write response ETag" is probed (low confidence, Q4).

> **Superseded by live observation (2026-09-28) (Q4 settled).** `etag` is
> the **document's** new tag after the write, without quotes. It is present
> on PUT, POST, MOVE and selector DELETE events, and absent on a
> whole-document DELETE. For a selector PUT it therefore differs from the
> write response's element tag. It equals the tag that a selector DELETE
> answers, and the tag that a whole PUT answers, once unquoted. The doc
> example's unquoted form (C6) was right. Case:
> `sse.mutation.put-etag-matches-response`.

#### R-SSE-14 — `body`
- PUT (element): the replacement node(s) as stored — the same markup the `206` response body carries.
- POST: the inserted node(s) as stored — the same markup the `206` response body carries (D-POST examples: body `<li>New item</li>` → response `<li>New item</li>`).
- DELETE: empty (`<div itemprop="body"></div>`).
- MOVE: the moved element's markup (outerHTML) as stored after the move (recommendation; BJ-SSE ignores it).
- Whole-document writes: empty (R-SSE-17).

Body markup is the stored (uncomposed) form: server-side composition (includes,
bindings, templates, transients) is not applied.
- **Evidence:** documented ("The mutated HTML fragment (empty for DELETE)") · demo-source (POLLS:104,113 treats the write response and POLLS:228 the event body as the same fragment) · inferred (MOVE body, stored vs composed) · **Confidence:** high (PUT/POST/DELETE) / low (MOVE, composition)
- **Source:** D-SSE §Mutation event fields; D-PUT/D-POST §Examples.
- **Edge cases:** server-added content (schema defaults, generated keys) is included because the stored form is sent. Multi-node POST bodies are sent whole (BJ-SSE appends the whole fragment; BJ-SSE uses only the first element for PUT).

#### R-SSE-15 — `placement`
Present on every POST event and every MOVE event, lowercase, one of `append`,
`prepend`, `before`, `after`: the insertion site actually used relative to the
element `selector` (POST) or `destination` (MOVE) matched. A POST without a
`placement=` parameter reports `append` explicitly. Absent for PUT, DELETE and
whole-document events.
- **Evidence:** documented · **Confidence:** high (POST), medium (MOVE)
- **Source:** D-SSE §Mutation event fields, §Applying a POST event; POLLS:217,231-235 (defaults to append when missing); BJ-SSE:223 (lowercases, defaults to append).
- **Edge cases:** the request may spell placement in any case (D-MOVE: case-insensitive); the event always reports lowercase (inferred).

#### R-SSE-16 — `destination`
MOVE only: the `Destination-Range` selector verbatim (unit prefix and
`; placement=` parameter removed, trimmed).
- **Evidence:** client-source · **Confidence:** medium
- **Source:** BJ-SSE:132,221 (`document.querySelector(mutation.destination)`). Not in D-SSE (Contradiction C2).

#### R-SSE-17 — Whole-document writes
A whole-document write (PUT without `Range`, creating or replacing; DELETE
without `Range`) emits one `mutation` event to subscribers of that path with
`method` `PUT`/`DELETE`, `selector` present and **empty**, `body` empty, no
`placement`, and `etag` = the new document ETag for PUT (omitted for DELETE).
Subscribers treat an empty selector as "refetch the document". The open stream
stays open after a whole-document DELETE; a later re-creation produces a PUT
event on the same stream.
- **Evidence:** inferred · **Confidence:** low
- **Why:** no source documents it. An empty selector is the only choice harmless to all known clients: BJ-SSE `querySelector('')` throws inside the listener (logged, no DOM change); POLLS:222 skips a falsy selector; KANBAN:398 refetches on any mutation. A selector like `html` with the document as body would make BJ-SSE/POLLS replace the page root with a fragment. Emitting nothing would leave KANBAN-style clients stale. (Q7)

> **Superseded by live observation (2026-09-28), body and etag (Q7).**
> - A whole-document PUT event carries the **whole stored document** as its
>   `body`, with `etag` set to the new document tag, unquoted.
> - The selector stays empty.
> - A whole DELETE has an empty body.
>
> Observation: `sse-mutation-whole-document-put.json`.

#### R-SSE-18 — Which requests emit events
Exactly one mutation event per successful, committed mutating request on the
public plane: PUT, POST, DELETE, MOVE (element-scoped or whole-document). No
event for: any request that fails (`4xx`/`5xx`, including `401/403/404/409/412/413/416/422`),
reads (GET/HEAD/OPTIONS/QUERY), or writes rolled back by a trigger/validation.
A successful write whose stored result is byte-identical to before still emits
(pagelike keeps it simple; Q23).
- **Evidence:** documented ("When a subscribed document is modified…"; D-SM: a committed write "like any other mutation, is published on the SSE mutation stream") · inferred (failures, one-per-request, no-op) · **Confidence:** high / low (no-op)

#### R-SSE-19 — Write-through writes (includes, stamps, parameterized routes)
When a write addressed to a composed page is routed to an included/stamped
element's origin resource, the event is published on the **origin** path only;
subscribers of the composed page receive nothing. `path` is the origin path.
`selector`: pagelike reports the request selector if it matches the written
element in the origin document, otherwise a generated selector for that element
(`#id` when it has an id, else a `>`-joined `:nth-child()` path from `html`).
- **Evidence:** documented (routing to origin: D-INC §Interaction with HTTP Document Mutation, D-STAMP, D-ROUTE; event scope: D-SSE §Composed resources) · inferred (selector rule) · **Confidence:** medium / low (selector)

> **Superseded by live observation (2026-09-28) (Q12).** The write lands on
> the origin, but the event is published on the **written (composed)
> page's** stream only, with `path` set to that page and the request
> selector verbatim. The origin's subscribers receive nothing. pagelike has
> no includes yet, so `sse.scope.write-through-event-on-origin` holds
> PageLove's expectations and fails locally until composition exists.

#### R-SSE-20 — Subscription scope
A subscription covers its one path. Mutating a resource that a page includes
publishes to the included resource's subscribers, never to subscribers of
including pages. Clients follow composed pages by opening one stream per path.
- **Evidence:** documented · **Confidence:** high
- **Source:** D-SSE §Composed resources.

> **Superseded by live observation (2026-09-28).** A write to an included
> resource is delivered to its own subscribers **and** to the subscribers of
> every page that includes it, as the same event (same id and `path`, which
> names the included resource). Case: `sse.scope.include-not-propagated`
> (pending composition in pagelike).

#### R-SSE-21 — Transient elements
Writes that target a `p:transient` element (or its descendants) change only the
writer's session copy, not the canonical document, and emit **no** mutation
event to any subscriber.
- **Evidence:** inferred · **Confidence:** medium
- **Why:** D-TR: transient changes "do not affect other sessions or the canonical document"; broadcasting them would leak one session's content to others, and same-session tabs would be suppressed anyway (R-SSE-37).

#### R-SSE-22 — WebDAV (authoring plane) writes
A WebDAV write that creates, replaces, deletes or renames an HTML/XML document
emits whole-document events per R-SSE-17 (rename/MOVE: DELETE event on the old
path, PUT event on the new path). WebDAV writes carry no session, so they are
never echo-suppressed.
- **Evidence:** inferred · **Confidence:** low
- **Why:** D-DAV says the mount "represents the live site"; D-TH/D-TC explicitly exclude WebDAV from transition handlers and constraints but say nothing about SSE, so live pages should see author edits. Needs a probe (Q9).

> **Superseded by live observation (2026-09-28) (Q9 settled).** WebDAV writes
> emit **no** events: nothing arrived within 8 s. pagelike's `engine.event`
> skips authoring-plane writes. Case: `sse.scope.webdav-write-emits`.

#### R-SSE-23 — Server-originated writes (triggers, processors, outbound HTTP)
- Side-effect writes (`Pagelove.PUT(item, "/other.html")`, `Pagelove.DELETE`) go through the normal pipeline (D-TRIG §Side-effect writes, D-PROC) and emit events like the equivalent whole-document write. They are not attributed to the triggering request's session/connection, so the originator also receives them.
- A body transformation (`Pagelove.PUT(item, Context.request.path)`) is not a separate write: the request's own event carries the transformed content and is suppressed like any own write.
- An outbound HTTP request (HttpRequest action, transition handler delivery) that writes back to a PageLove host is an ordinary write with whatever session it carries (normally none).
- **Evidence:** inferred · **Confidence:** low (Q11)

> **Superseded by live observation (2026-09-29) (Q11 settled), first
> bullet.** A trigger's side-effect write (`Pagelove.PUT(item,
> "/other.html")`) is committed but emits **no** event on the side
> document's stream. pagelike marks such writes (`sideEffect`) and emits
> nothing for them. Case: `sse.scope.trigger-side-effect-emits`, which waits
> 61 s for the trigger to bind.
> - The second bullet (a body transformation is part of the request's own
>   event) still holds.
> - An outbound request that writes back arrives as an ordinary
>   application-plane write, so the third bullet holds too.

#### R-SSE-24 — Templated resource creation
A `POST` to a template that creates a new document (D-RC) emits a whole-document
PUT event on the new document's path and nothing on the template's path.
- **Evidence:** inferred · **Confidence:** low

#### R-SSE-25 — PATCH, CRDT and `crdt-delta`
PageLove's docs describe a `PATCH` that applies a CRDT change-set to a whole
document, and mention a `crdt-delta` SSE event "paired" with a mutation event.
No payload, id, ordering or trigger condition for `crdt-delta` is documented
anywhere, and the kanban template (2026-09-24) states there is no CRDT and no
PATCH on the platform. pagelike decision:
- `PATCH` → `501 Not Implemented` with a pagelike Error item (plan principle); therefore no `crdt-delta` events are ever emitted.
- The event names `crdt-delta` and the method token `PATCH` are reserved; pagelike MUST NOT use them for anything else.
- If implemented later: one `crdt-delta` event immediately after its paired `mutation` event, sharing the same suppression decision (R-SSE-40), and replayable like mutations.
- **Evidence:** documented (existence of PATCH/CRDT/crdt-delta in D-GLOSS, D-SSE §Echo suppression, D-TH "CRDT change-set PATCHes do not fire handlers", D-PUT/D-DEL "collaborative document" 422 rows, D-AR OPTIONS Allow lists PATCH) · demo-source (KANBAN:2016-2019 denies it exists) · **Confidence:** low (Contradiction C8, Q26)

### C. Delivery

#### R-SSE-26 — Fan-out
Every open, authorized stream subscribed to the event's path receives the
event, except streams excluded by echo suppression (R-SSE-37/38) or by read
authorization (R-SSE-28). Streams on other paths receive nothing.
- **Evidence:** documented ("every subscriber receives the mutation") · **Confidence:** high
- **Source:** D-SSE intro, §Composed resources.

#### R-SSE-27 — Ordering and consistency
- Events of one path are delivered in commit order; within a stream ids are strictly increasing.
- An event is published only after its write committed; a client that receives an event and then GETs the document sees that write (or a later state).
- Relative order between the writer's HTTP response and other subscribers' events is unspecified.
- Delivery should be prompt (docs: "in real time"); harness cases allow 3 s.
- **Evidence:** documented ("in real time") · inferred (commit order, read-after-event; matches `docs/design.md` write path step 6) · **Confidence:** high

#### R-SSE-28 — Read authorization of delivered events
Before delivering an event to a stream, pagelike re-evaluates the subscriber's
read access using the current rules: if the principal can no longer subscribe to
the path, the server closes the stream; if a GET of the event's target element
(PUT/POST/MOVE: the resulting element(s); DELETE: the removed element as it was)
would be denied to that principal by a selector-scoped rule, the event is not
delivered to that stream.
- **Evidence:** inferred (security: without it an element hidden by a selector-level `Deny` leaks through the stream) · **Confidence:** low (Q22)

### D. Ids, retention, replay, resets

#### R-SSE-29 — Event ids
Every `mutation` event has an `id:` of the form `<ms>-<n>` (decimal Unix
milliseconds of the commit, `-`, a decimal counter), matching `^[0-9]+-[0-9]+$`.
Ids are strictly increasing in (ms, n) numeric order within a site, so the same
event has the same id on every stream and on replay. Clients treat ids as opaque.
pagelike MAY use the site-wide event sequence number as `n`.
- **Evidence:** documented (example `1709942400000-0`; "server-assigned event identifier used for reconnection") · inferred (format generalization, ordering) · **Confidence:** medium
- **Edge cases:** `pagelove-connection` and `reset` events carry no id (R-SSE-6, R-SSE-33). The harness normalizes id literals; cases compare ids only through captures.

> **Superseded by live observation (2026-09-28).** Ids are
> `v1~<first 12 hex of sha256(path)>.<ms>-<n>`, e.g.
> `v1~ad7e4f97b66d.1790634867705-0`. The documented `<ms>-<n>` is the tail of
> the id. pagelike uses its site-wide event sequence as `n`, and ordering
> within a path is unchanged. Case: `sse.mutation.id-format`.

#### R-SSE-30 — Retention
Mutation events are retained for 10 minutes after commit (pagelike: exactly 10
minutes by default, configurable; persisted with the documents so a restart
does not lose them). Older events are purged.
- **Evidence:** documented · **Confidence:** high
- **Source:** D-SSE §Reconnection.

#### R-SSE-31 — Replay with `Last-Event-ID`
If a subscribe request carries a non-empty `Last-Event-ID` header that is valid
and not expired, the server, after `pagelove-connection`, sends every retained
event of that path whose id is strictly greater, in order and with their
original ids, then continues with live events with no gap and no duplicate at
the replay/live boundary.
- **Evidence:** documented · **Confidence:** high (replay), medium (ordering after pagelove-connection)
- **Source:** D-SSE §Reconnection, §If your client stops reading ("receives everything it missed within the retention window"); D-SSE §Echo suppression ("The first event on each stream is `pagelove-connection`").
- **Edge cases:**
  - Id equal to the newest event → nothing replayed.
  - Id newer than any event (clock skew, another site) → treated as a position; nothing replayed (low; Q15).
  - Echo suppression applies to replayed events with the same rule as live ones, using the write's recorded origin session/token against the new stream (R-SSE-40).
  - Empty `Last-Event-ID` header = absent. No query-parameter alternative.

#### R-SSE-32 — Expired or unusable `Last-Event-ID`
If `Last-Event-ID` is present but its millisecond part is older than the
retention window (now − 10 min), or it cannot be parsed as `<ms>-<n>`, the
server sends (after `pagelove-connection`) one `reset` event with reason
`events-expired`, replays nothing, and keeps the stream open for live events.
- **Evidence:** documented (expired → reset `events-expired`) · inferred (timestamp test, malformed ids, stream continues) · **Confidence:** high / medium / low
- **Why the stream continues:** KANBAN:399 handles `reset` by refetching and keeps using the same `EventSource`; closing would make the browser reconnect with the same expired id forever.

> **Superseded by live observation (2026-09-28/29) (Q15 settled).**
> - An **unparseable** `Last-Event-ID` is ignored: no reset, live events
>   only.
> - A well-formed id is replayed from, however old it is (for example an id
>   whose time part is 1970), as long as no event after it has been pruned.
> - A `reset` with `events-expired` is sent only when events after the id
>   are gone. pagelike prunes to the retention window, then checks whether
>   the oldest retained sequence number is past the id's.
> - The stream continues after a reset.
>
> Cases: `sse.replay.malformed-id-resets`, `sse.replay.ancient-id-resets`,
> `sse.replay.expired-after-retention` (slow),
> `sse.replay.stream-continues-after-reset` (local, `prune_events`).

#### R-SSE-33 — Reset event wire format
```
event: reset
data: <article itemscope itemtype="https://pagelove.org/StreamReset">
data:   <span itemprop="reason">events-expired</span>
data: </article>

```
No `id:` line. `reason` is one of `events-expired`, `session-expired`,
`session-invalidated`; pagelike MUST NOT invent other reasons (clients map an
absent reason to `unknown`, BJ-SSE:143-148, and all known clients react to any
reset by refetching or reloading: BJ-SSE:58-73, POLLS:241, KANBAN:399).
- **Evidence:** documented · **Confidence:** high
- **Source:** D-SSE §Reset events.

#### R-SSE-34 — Session resets
- When the subscriber's session reaches its expiry time while the stream is open, the server sends `reset` with `session-expired` and then closes the stream.
- When the subscriber's session is deleted (logout), found corrupt, or otherwise unusable, the server sends `reset` with `session-invalidated` and then closes the stream.
- A subscribe request whose cookie names an expired/unknown session is handled by the identity area first (new anonymous session or denial); it does not produce a session reset.
- **Evidence:** documented (reasons and meanings) · inferred (timing, close afterwards) · **Confidence:** high / low (Q20)

### E. Keepalive and flow control

#### R-SSE-35 — Keepalive
Every 20 seconds of stream lifetime the server writes the comment line
`: ping` followed by a blank line (`": ping\n\n"`), whether or not events were
sent in between. Comments never carry ids or dispatch events.
- **Evidence:** documented · **Confidence:** high (interval, text), medium (exact bytes, fixed ticker)
- **Source:** D-SSE §Keepalives.

#### R-SSE-36 — Bounded send buffer
Each stream has a bounded queue (pagelike default: 256 events or 4 MiB,
whichever fills first). Publishing never blocks on a slow stream: when a
stream's queue is full, the server closes that stream (no reset event, no error
body — the connection just ends). The client reconnects with `Last-Event-ID`
and receives what it missed from retention.
- **Evidence:** documented (behavior) · inferred (sizes, no reset before close) · **Confidence:** high / low
- **Source:** D-SSE §If your client stops reading.

### F. Echo suppression

#### R-SSE-37 — Default: by session
A mutation is never delivered to a stream whose session is the writer's session
when the write carries no `Pagelove-Connection` header. All streams of that
session (every tab sharing the cookie) are skipped. Other sessions receive it.
- **Evidence:** documented · demo-source (KANBAN:243-253,290-294 relies on "every subscriber EXCEPT the originator") · client-source (BJ-TEST:9-13) · **Confidence:** high
- **Source:** D-SSE §Echo suppression; D-JS §DOM mutations.
- **Edge cases:** the session is the one named by the `pagelove_session` cookie (D-TR §Sessions and access control; identity area). A request with no session cookie gets a fresh session and so matches no existing stream — which is why two cookie-less `curl` clients see each other's writes. Writes with no session at all (WebDAV, server-originated) are never suppressed (R-SSE-41).

#### R-SSE-38 — `Pagelove-Connection` narrows suppression
If a mutating request (PUT, POST, DELETE, MOVE, and PATCH if ever supported)
carries `Pagelove-Connection: <token>`, the event is withheld only from the
stream whose token equals the header value **and** whose session is the
writer's session; every other stream, including other tabs of the same session,
receives it. pagelike compares the trimmed header value byte-for-byte; an
unknown or stale token therefore suppresses nothing.
- **Evidence:** documented (narrowing, header name, fallback when absent) · demo-source (POLLS:77-86 sends the header on every write once known) · inferred (session AND token; unknown token; MOVE) · **Confidence:** high / low
- **Source:** D-SSE §Echo suppression; D-JS §DOM mutations (stock `PageloveSSE` does not do this).
- **Edge cases:**
  - Header absent (including writes sent before the `pagelove-connection` event arrived) → R-SSE-37.
  - Header on GET/HEAD/OPTIONS/QUERY → ignored.
  - D-SSE lists PUT/POST/DELETE/PATCH; MOVE is also honoured (Contradiction C9).
  - Alternative reading "token match alone, ignoring session" differs only when a client sends another session's token (Q18).

> **Superseded by live observation (2026-09-28) (Q18 settled).** The token
> **alone** decides. A write carrying `Pagelove-Connection: <T>` is withheld
> from the stream whose token is T, whatever session sent it, and delivered
> to every other stream. Tokens are unguessable UUIDs given only to the
> stream's holder. Case: `sse.echo.cross-session-token-not-honoured`.

#### R-SSE-39 — Legacy channels removed
- A `conn` query parameter on the subscribe URL is ignored; the token is always server-assigned.
- The `X-Pagelove-Connection` request header is not read; a write carrying only it is treated as having no token (session suppression).
- **Evidence:** documented · **Confidence:** high
- **Source:** D-SSE §Echo suppression (last paragraph).

#### R-SSE-40 — Scope of suppression
Suppression applies to every event a write produces for a stream — the
`mutation` event and any paired `crdt-delta` — and to replayed events: pagelike
records each event's origin session id and origin token and evaluates
R-SSE-37/38 against the receiving stream at delivery or replay time.
- **Evidence:** documented (covers mutation + crdt-delta) · inferred (replay) · **Confidence:** high / low
- **Edge cases:** a tab that wrote with token T1, lost its stream, and reconnects (new token T2) with an older `Last-Event-ID` receives its own write on replay (T1 ≠ T2). Clients must tolerate echoes; beta-js keeps a local echo queue for this reason (BJ-SSE:80-104,166-170; BJ-TEST).

#### R-SSE-41 — Writes without a session
Writes that have no end-user session (authoring plane, trigger/processor
side-effect writes, handler deliveries without cookies) are delivered to every
subscriber.
- **Evidence:** inferred · **Confidence:** medium

> **Superseded by live observation (2026-09-28/29), in part.**
> Authoring-plane writes and trigger or processor side-effect writes emit no
> events at all (R-SSE-22/23 as reconciled). Application-plane writes
> without a session are still delivered to every subscriber.

### G. Lifecycle and client compatibility

#### R-SSE-42 — Stream termination
The server removes a subscription as soon as the client disconnects (write
error or context cancellation) and closes every stream on shutdown. It never
closes a healthy stream on its own except for R-SSE-28 (access lost),
R-SSE-34 (session), R-SSE-36 (slow reader). An `EventSource` reconnects
automatically after any close (R-SSE-31 then applies).
- **Evidence:** inferred · **Confidence:** medium

#### R-SSE-43 — Compatibility checklist (what shipped clients rely on)
A pagelike stream is compatible with the pinned clients iff all of these hold:
1. `event: mutation` / `event: reset` / `event: pagelove-connection` names exactly (BJ-SSE:48,58; POLLS:206,219,241; KANBAN:398-399).
2. Mutation data parses as HTML containing `[itemtype="https://pagelove.org/Mutation"]` (BJ-SSE:116; else the event is dropped with a warning).
3. `method` uppercase; `selector` resolvable with `document.querySelector` in the subscriber's DOM; the verbatim request selector (BJ-SSE:166-172).
4. Literal `<div itemprop="body">` and body as the last `</div>` (POLLS:213-216).
5. `placement` on POST (POLLS:217 defaults to append, BJ-SSE ignores it for POST).
6. `destination` + `placement` on MOVE (BJ-SSE:220-239).
7. `path` equals the subscriber's `location.pathname` for pages without query strings (POLLS:221).
8. `pagelove-connection` data usable verbatim as a header value (POLLS:80,206).
9. No echo to the originating session/stream (BJ-TEST, KANBAN:243-253).
- **Evidence:** client-source / demo-source · **Confidence:** high

## 3. Cross-area dependencies

- **permissions / authz** — R-SSE-3 needs "whole-document GET decision without default-GET fallback"; R-SSE-28 needs per-element read decisions for a principal at delivery time; denial response shape (401/403 error document) is owned there.
- **identity** — session id from `pagelove_session`, anonymous session minting, session expiry/invalidation notifications (R-SSE-34, R-SSE-37).
- **reading-writing** — the write pipeline must hand the broker, per committed write: method, normalized request selector, placement, destination selector, stored path, stored body markup, resulting element ETag, origin session id, origin `Pagelove-Connection` token. Whole-document writes, MOVE (element and whole-document), status codes of failures (no event).
- **compose** — write-through routing to origin (R-SSE-19); includes do not propagate (R-SSE-20); transient writes produce no event (R-SSE-21).
- **reactions** — trigger/processor side-effect writes (R-SSE-23); transition-handler deliveries.
- **webdav** — authoring writes publish whole-document events (R-SSE-22).
- **store** — `events(seq, ts_ms, path, event, payload, origin_session, origin_conn)` from `docs/design.md` must also let replay filter by path and time; retention purge job (R-SSE-30).
- **harness** — cases use the SSE step vocabulary plus the extensions in §6.

## 4. Contradictions between sources and decisions

| # | Topic | Claims | Decision |
|---|---|---|---|
| C1 | MOVE events | D-SSE field table: `method` is PUT, POST or DELETE; same table: `placement` is for "POST and MOVE only"; BJ-SSE applies `MOVE` events. | Emit `method=MOVE` events with `placement` and `destination` (R-SSE-9/15/16). |
| C2 | `destination` | Absent from D-SSE; parsed and required by BJ-SSE:132,221. | Emit it on MOVE. |
| C3 | POST placement in the client | D-SSE: clients must insert relative to the anchor using `placement`; D-JS and BJ-SSE:193-200 always append. | Server always emits `placement`; the stock client's gap is not a server concern. |
| C4 | `PageloveSSE.parse` shape | D-JS: returns `{method, selector, path, host, body, etag}`; BJ-SSE:125-134 also returns `destination`, `placement`. | Code wins; property set per R-SSE-8. |
| C5 | Property order | D-SSE example: method, selector, etag, path, host, body; BJ-TEST helper: method, selector, path, host, body, placement (no etag). | Order in R-SSE-8; only "body is the last div" is load-bearing. |
| C6 | ETag spelling | D-SSE example `a1b2c3...` (unquoted); BJ-SSE/BJ-PRIM reuse it verbatim as `If-Match`, which needs quotes. | Emit the exact quoted header value (R-SSE-13); probe Q4. |
| C7 | `host` | Example subscribes to `example.pagelove.org`, event says `example.com`. | Site's canonical host, not the request Host (R-SSE-12); probe Q6. |
| C8 | CRDT / PATCH | Docs: PATCH applies CRDT change-sets, `crdt-delta` events exist, OPTIONS may list PATCH; KANBAN:2016-2019 (2026-09-24): "no CRDT and no PATCH on this platform". | pagelike: PATCH → 501, no `crdt-delta` (R-SSE-25). |
| C9 | Token on MOVE | D-SSE lists PUT/POST/DELETE/PATCH as the requests that should carry `Pagelove-Connection`. MOVE is a mutation too. | Honour the token on MOVE (R-SSE-38). |
| C10 | Denial status | D-GET error table: 403; D-AR/D-MOVE: 401 unauthenticated, 403 authenticated. | 401 anonymous / 403 authenticated (R-SSE-3). |
| C11 | "SUBSCRIBE" | D-SSE live example uses a `SUBSCRIBE` pseudo-request. | Notation only; subscribe = GET + Accept (R-SSE-1). |

The combined pages (`all/…`) and the individual pages are textually identical
for both SSE pages; no combined-vs-individual divergence exists in this area.

## 5. Implementation notes for pagelike (non-normative)

- Broker: in-memory map `path → set<stream>`; per stream a bounded channel; publish = non-blocking send, on full → cancel stream (R-SSE-36).
- After the write transaction commits, for each inserted `events` row publish `{seq, ts_ms, path, payload, origin_session, origin_conn}`; `payload` is the pre-rendered article text, so replay and live send identical bytes.
- Subscribe handler order: resolve path → authorize (no default-GET) → existence → write head → `pagelove-connection` → register with broker (buffering live events) → if `Last-Event-ID`: expired/malformed → `reset`, else query `events WHERE path=? AND seq > ?` and send → drain buffered live events skipping seq ≤ last replayed → live loop with 20 s ticker.
- Id: `fmt.Sprintf("%d-%d", ts_ms, seq)`; parse both numbers; expiry uses `ts_ms`.
- Retention purge: delete `events` rows with `ts_ms < now-10min` periodically; replay decisions use the id's `ts_ms`, not row presence.
- Session expiry timers per stream (R-SSE-34); identity area notifies the broker on logout.
- Per-event authorization re-check (R-SSE-28) uses the cached rule generation; cheap when no selector-scoped read rules exist for the path.

## 6. Harness conventions used by the `sse` cases

These cases use the step vocabulary in `harness/README.md` plus the following
extensions, which the runner must implement:

- `sse_open` MAY carry `headers:` (extra request headers) and `expect:` (`status`, `headers`, `header_matches`, `headers_absent`) checked against the stream's response head.
- `sse_expect` semantics: waits up to `within_ms` for the next event of type `event`, skipping events of other types; with `next: true` the very next event (comments excluded) must match. Extra matchers: `data_not_contains`, `data_matches` (regexp over the joined data), `id_matches` (regexp over the raw id, before normalization), `id_absent: true`. `capture` accepts `id` and `data`.
- `data_microdata` extracts the first item of the payload (Mutation or StreamReset); property values are text content; `null` means "property absent", `""` means "present and empty".
- `sse_expect_comment: {name, text, within_ms}` — waits for an SSE comment line whose text (after `:` and one optional space) equals `text`.
- `sse_expect_closed: {name, within_ms}` — the server ended the stream.
- `sse_pause: {name}` / `sse_resume: {name}` — stop/restart reading the stream socket (`requires: [sse-control]`).
- `session_control: {actor, action: expire | invalidate}` — local-only identity hook (`requires: [session-control]`).
- `repeat: {count, step}` — runs one request step `count` times sequentially.
- `site.settings.sse_buffer_events` — pagelike-only knob that sets the per-stream queue length for a local run (ignored by live runs; cases using it are `live: false`).
- `${P}` (and other `${…}` captures) are substituted inside file bodies and header values as well as paths. Equality of two event ids is asserted with `id_matches: '^${captured}$'` (ids contain only digits and `-`).
- Identity model assumed by the cases: `anonymous` requests and streams carry **no cookies** (no cookie jar), so each anonymous request is its own fresh session; each named actor has one fixed session, shared by all its requests and streams in a case (two `sse_open` as the same actor = two tabs of one session).
- Captures from one step are available to later steps as `${name}`.

## 7. Open questions for live probing

All probes assume a disposable host, paths under a unique prefix `$P`, a rules
document granting `*` GET/PUT/POST/DELETE/MOVE on `$P/*`, and `curl -N` for
streams (`curl -N -H 'Accept: text/event-stream' https://$HOST$P/doc.html`).
Anonymous = no cookie jar unless stated.

- **Q1 Response head.** `curl -si -N -H 'Accept: text/event-stream' $P/doc.html` for 2 s → record every header (Content-Type parameters, Cache-Control, Vary, Set-Cookie, Transfer-Encoding, X-Accel-Buffering, Access-Control-*).
- **Q2 Connection event bytes.** Same request, dump raw bytes (`curl -N … | od -c | head`) → does `pagelove-connection` carry `id:`/`retry:`? token length/charset? Open twice → tokens differ? Reconnect with `Last-Event-ID` → new token?
- **Q3 Selector string.** Stream open; `PUT $P/doc.html` with `Range: selector=  main  >  h1 ` (extra spaces), then `Range: selector=li` (multi-match), then `POST` with `Range: selector=ul; placement=prepend` → record the `selector` text of each event (verbatim? trimmed? whitespace-collapsed? canonicalized?).
- **Q4 ETag.** `PUT` with `Range: selector=h1` → compare the response `ETag` header with the event's `etag` text (quotes? equal?). Repeat for POST (single `<li>`, and two `<li>` siblings), DELETE, MOVE. Then use the event etag as `If-Match` on `PUT Range: selector=h1` → 206 or 412?
- **Q5 Body.** POST a `<li>` that contains an `e:` binding or `<p:include>` → is the event body stored or composed markup? MOVE → body empty or moved element?
- **Q6 Serialization.** Capture a raw POST and MOVE event → property order, `placement`/`destination` element type, escaping of `>` and `&` in `selector` (use `Range: selector=a[title="x&y"] > b`), `host` value vs request Host.
- **Q7 Whole-document writes.** Stream on `$P/doc.html`; `PUT $P/doc.html` (no Range, full HTML) → event? method/selector/body/etag? Then `DELETE $P/doc.html` (no Range) → event? stream still open? Then PUT it again → event on the same stream?
- **Q8 Whole-document MOVE.** Streams on `$P/a.html` and `$P/b.html`; `MOVE $P/a.html` with `Destination: $P/b.html` → which events on which stream?
- **Q9 WebDAV.** Stream (anonymous, public plane) on `$P/doc.html`; author `PUT` of the same file over the DAV mount → any event within 5 s? Shape?
- **Q10 Transient.** Document with `<ul id="cart" p:transient>`; stream A (anonymous); anonymous `POST Range: selector=#cart` → any event on A?
- **Q11 Triggers.** Trigger (Sessel `Pagelove.PUT(new Thing{…}, "$P/side.html")`) on `PUT $P/doc.html`; streams on both paths; anonymous PUT → events on `side.html`? on `doc.html`?
- **Q12 Write-through selector.** `$P/page.html` includes `header#nav` from `$P/partial.html` (placed at a different depth); streams on both; `PUT $P/page.html Range: selector=body > div > header#nav` → event on which path, with which selector?
- **Q13 Missing document.** Stream on `$P/missing.html` (rule allows GET) → status? Then create it → events? Stream open on a doc that is then deleted whole → closed or open?
- **Q14 Paths.** Stream on `$P/dir/` and `$P/dir/index.html`; write to `$P/dir/index.html` → which streams, and what `path`? Stream on `$P/doc.html?x=1` → receives writes? Path with `%20` → encoded or decoded in `path`?
- **Q15 Last-Event-ID edge cases.** Reconnect with `Last-Event-ID: garbage`, `1000-0`, `99999999999999-0`, `` (empty) → reset? replay? nothing? After a reset, does a later write still arrive on the same stream? Does the reset carry `id:`?
- **Q16 Retention boundary.** Capture an id, wait 9 min, reconnect with it (expect replay of later writes); capture another, wait 11 min (expect `events-expired`).
- **Q17 Id scope.** Streams on `$P/a.html` and `$P/b.html`; alternate writes → are ids monotonic across paths (site-wide) or per path? Two writes in the same millisecond → `-0`, `-1`?
- **Q18 Echo with tokens.** (multi-actor for full coverage) Stream A anonymous; anonymous writer (no cookie) sends `Pagelove-Connection: <A's token>` → does A receive it (session-AND model) or not (token-only model)? Same session: token of a closed stream or `bogus` → suppressed or delivered? Token on MOVE honoured? Replay after reconnect includes own token-tagged write?
- **Q19 Keepalive.** Open a stream for 65 s with no writes → timestamps of `: ping` lines (first at 20 s? fixed ticker or idle timer?). Exact bytes (`: ping\n\n`?).
- **Q20 Session resets.** (multi-actor) Log an actor out in another request while its stream is open → `session-invalidated`? stream closed? Session expiry observed when?
- **Q21 Slow consumer.** Open a stream with a reader that stops reading; issue ~2000 POSTs of 8 KiB → when is the stream closed? Any final event? Reconnect replays everything?
- **Q22 Event filtering.** (multi-actor) Rule denying anonymous GET of `.secret`; an editor PUTs `.secret`; does an anonymous subscriber receive the event (with the secret body)?
- **Q23 No-op writes.** PUT the identical fragment twice → two events or one?
- **Q24 Request variants.** `Accept: text/event-stream;q=0.5, text/html`; `Accept: */*`; `HEAD` with `Accept: text/event-stream`; subscribe with `Range: selector=h1` → stream? filtered to that element?
- **Q25 Rule change mid-stream.** (author) Remove the GET rule while a stream is open, then write → still delivered? stream closed?
- **Q26 PATCH/CRDT.** `OPTIONS $P/doc.html` → is PATCH in `Allow`? `PATCH $P/doc.html` with an empty body → status and error kind? Any `crdt-delta` event ever observed on a stream?
- **Q27 Multi-node writes.** POST `<li>a</li><li>b</li>` → one event with both nodes, or two?
- **Q28 Session cookie on subscribe.** Does the subscribe response set `pagelove_session`? Does a cookie-less writer get one (and thus a fresh session per request)?

## 8. Case index (requirement → harness case)

| Requirement | Cases (`harness/cases/sse/…`) |
|---|---|
| R-SSE-1 | subscribe: `sse.subscribe.response-head`, `sse.subscribe.wildcard-accept-is-not-a-subscribe` |
| R-SSE-2 | subscribe: `sse.subscribe.query-string-ignored`, `sse.subscribe.conn-query-ignored`, `sse.subscribe.directory-index` |
| R-SSE-3 | subscribe: `sse.subscribe.default-get-does-not-grant`, `sse.subscribe.default-get-deny-no-rule`, `sse.subscribe.explicit-deny-rule`, `sse.subscribe.authenticated-denied-403`, `sse.subscribe.group-rule-grants` |
| R-SSE-4 | subscribe: `sse.subscribe.missing-document-404` |
| R-SSE-5 | subscribe: `sse.subscribe.response-head` |
| R-SSE-6 | subscribe: `sse.subscribe.first-event-is-connection`, `sse.subscribe.connection-event-precedes-mutation`, `sse.subscribe.connection-event-has-no-id`, `sse.subscribe.tokens-distinct-per-stream` |
| R-SSE-7 | mutation: `sse.mutation.multiline-body` |
| R-SSE-8 | mutation: `sse.mutation.put-properties`, `sse.mutation.table-row-body` |
| R-SSE-9 | mutation: `sse.mutation.put-properties`, `sse.mutation.post-default-placement`, `sse.mutation.delete`, `sse.mutation.move-append` |
| R-SSE-10 | mutation: `sse.mutation.selector-verbatim`, `sse.mutation.multi-match-selector`, `sse.mutation.post-placements` |
| R-SSE-11 | mutation: `sse.mutation.put-properties`; scope: `sse.scope.write-through-event-on-origin` |
| R-SSE-12 | mutation: `sse.mutation.put-properties` (presence only) |
| R-SSE-13 | mutation: `sse.mutation.put-properties` (presence), `sse.mutation.put-etag-matches-response` |
| R-SSE-14 | mutation: `sse.mutation.post-default-placement`, `sse.mutation.delete`, `sse.mutation.table-row-body` |
| R-SSE-15 | mutation: `sse.mutation.post-default-placement`, `sse.mutation.post-placements`, `sse.mutation.move-append`, `sse.mutation.move-before` |
| R-SSE-16 | mutation: `sse.mutation.move-append`, `sse.mutation.move-before` |
| R-SSE-17 | mutation: `sse.mutation.whole-document-put`, `sse.mutation.whole-document-delete` |
| R-SSE-18 | delivery: `sse.delivery.failed-writes-emit-nothing` |
| R-SSE-19, 20 | scope: `sse.scope.include-not-propagated`, `sse.scope.write-through-event-on-origin`; delivery: `sse.delivery.other-path-not-delivered` |
| R-SSE-21 | scope: `sse.scope.transient-write-no-event` |
| R-SSE-22 | scope: `sse.scope.webdav-write-emits` |
| R-SSE-23 | scope: `sse.scope.trigger-side-effect-emits` |
| R-SSE-26 | delivery: `sse.delivery.fanout`, `sse.delivery.other-path-not-delivered` |
| R-SSE-27 | delivery: `sse.delivery.commit-order`, `sse.delivery.read-after-event` |
| R-SSE-28 | flow: `sse.flow.denied-element-not-streamed` |
| R-SSE-29 | mutation: `sse.mutation.id-format`; delivery: `sse.delivery.fanout` |
| R-SSE-30 | replay: `sse.replay.within-retention`, `sse.replay.expired-after-retention` |
| R-SSE-31 | replay: `sse.replay.last-event-id`, `sse.replay.current-id-replays-nothing`; flow: `sse.flow.slow-consumer-disconnected-then-replayed` |
| R-SSE-32, 33 | replay: `sse.replay.ancient-id-resets`, `sse.replay.stream-continues-after-reset`, `sse.replay.malformed-id-resets`, `sse.replay.expired-after-retention` |
| R-SSE-34 | flow: `sse.flow.session-invalidated-reset`, `sse.flow.session-expired-reset` |
| R-SSE-35 | keepalive: `sse.keepalive.ping`, `sse.keepalive.ping-repeats` |
| R-SSE-36 | flow: `sse.flow.slow-consumer-disconnected-then-replayed` |
| R-SSE-37 | echo: `sse.echo.same-session-suppressed`, `sse.echo.all-tabs-of-session-suppressed`, `sse.echo.anonymous-writer-not-suppressed` |
| R-SSE-38 | echo: `sse.echo.token-narrows-to-one-tab`, `sse.echo.token-on-post-and-delete`, `sse.echo.token-on-move`, `sse.echo.unknown-token-suppresses-nothing`, `sse.echo.cross-session-token-not-honoured` |
| R-SSE-39 | echo: `sse.echo.legacy-header-not-read`; subscribe: `sse.subscribe.conn-query-ignored` |
| R-SSE-25, 40, 41, 42, 43 | spec-only (no observable case without PATCH support, replay-echo probes, or client harnesses) |

## 9. Live reconciliation 2026-09-28

A live PageLove run on 2026-09-28 (host `live-test-host`,
`harness/observations/live-2026-09-28/`), confirmed on 2026-09-29
(`harness/observations/live-2026-09-29-confirm/`), failed 11 sse cases. All 11
were resolved as adopt-live. [docs/compat/decisions.md](../compat/decisions.md)
has one row per case, giving the observed behaviour, the documented claim, the
decision and the rationale. Details mined from passing cases are in
[docs/compat/live-observations.md](../compat/live-observations.md) (LO-11).

**Superseded by live observation.** Each requirement below is marked in place
with the new behaviour:

| Requirement | New behaviour |
|---|---|
| R-SSE-4 | Missing document: the stream opens (Q13). |
| R-SSE-5 | No `Vary`; the body starts with `: connected`. |
| R-SSE-6 | The token is a UUID v4. |
| R-SSE-11 / R-SSE-19 | A write-through is announced on the written page with that page's path (Q12). |
| R-SSE-13 | The event etag is the document's new tag, unquoted (Q4; resolves C6 in favour of the unquoted example). |
| R-SSE-17 | A whole PUT carries the whole document (Q7). |
| R-SSE-20 | Includes propagate to including pages. |
| R-SSE-22 | WebDAV writes emit nothing (Q9). |
| R-SSE-23 | Side-effect writes emit nothing (Q11). |
| R-SSE-29 | Ids are `v1~<hex>.<ms>-<n>`. |
| R-SSE-32 | Unparseable ids are ignored; a reset happens only after pruning (Q15). |
| R-SSE-38 | The token alone suppresses (Q18). |
| R-SSE-41 | Authoring-plane and side-effect writes emit nothing. |

**Resolution of C6:** the event carries the document tag unquoted. beta-js
copies it into `If-Match`, where pagelike (and PageLove) treat an unquoted
value as the quoted tag (R-RW-85). The document tag no longer matches an
element's `If-Match` after a selector write (R-RW-89 as reconciled), so a
client that reuses it gets `412` and must re-read. That is PageLove's
behaviour.

**Pending in pagelike:** R-SSE-19/20 need includes (composition), and R-SSE-21
needs transient elements. Their cases hold PageLove's expectations.
`sse.scope.transient-write-no-event` passed live.

**Harness:** `sse.replay.stream-continues-after-reset` now prunes events
locally (`prune_events`, capability `sse-control`), because PageLove resets
only after pruning, which takes the 10-minute retention window
(`sse.replay.expired-after-retention`, slow). Both retention cases pass
locally.
