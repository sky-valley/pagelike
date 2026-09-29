# Protocol — OPTIONS, Accept-Ranges, QUERY, MOVE, WebDAV (behavioral spec)

Area: `protocol`. Packages: `internal/httpapi` (OPTIONS, QUERY, MOVE on the
public plane), `internal/webdav` (authoring plane incl. authoring QUERY), with
calls into `internal/authz`, `internal/selector`, `internal/sessel`,
`internal/compose`. Cases: `harness/cases/protocol/`.

This document specifies what a pagelike server must do so that clients written
for PageLove's wire protocol work unchanged: capability discovery (`OPTIONS`),
range-unit advertisement (`Accept-Ranges`, the `selector`/`bytes`/`entries`
units), the `QUERY` method in both of its modes on both planes, element and
whole-document `MOVE`, and the WebDAV authoring plane (revalidation, ETags,
error documents, status codes). It also fixes the shared wire formats
(`multipart/mixed`, `Content-Range` spelling) that these features and the
official clients depend on.

It is written from third-party evidence (docs snapshot 2026-09-28, pinned
upstream code, one set of live read-only probes). Anything not stated by a
source is marked `inferred` and says why. Keywords MUST / SHOULD / MAY are
normative for pagelike.

## 0. Sources, abbreviations, conventions

### 0.1 Docs (Markdown snapshot `research/docs/2026-09-28/md/`)

| Tag | Page | Notes |
|---|---|---|
| D-OPT | `reference/protocol/OPTIONS-method` | identical to the OPTIONS section of `all/reference/protocol` |
| D-DAV | `reference/protocol/WebDAV` | identical to the WebDAV section of `all/reference/protocol` |
| D-AR | `reference/protocol/Accept-Ranges` | identical to the Accept-Ranges section of `all/reference/protocol` |
| D-Q | `reference/protocol/QUERY` | identical to the QUERY section of `all/reference/protocol`; **not listed** in the group index (`reference/protocol` and the combined page's own "Pages in this group" list only OPTIONS, WebDAV, Accept-Ranges) |
| D-MOVE | `reference/reading-and-writing/MOVE-method` | identical to the MOVE section of `all/reference/reading-and-writing` (verified by diff) |
| D-RW | `all/reference/reading-and-writing` | GET/PUT/POST/DELETE sections, group index |
| D-UP | `reference/reading-and-writing/Uploading-Files` | blobs, selector ops on blobs → 422 |
| D-SSE | `reference/reading-and-writing/Server-Sent-Events` | MOVE `placement` row |
| D-RULE | `reference/permissions/AuthorizationRule` | wildcard expansion, multi-match authorization, 401 bodies |
| D-PAG | `reference/composing-pages/Pagination`, `recipes/paginating-a-list` | only other `Content-Range` examples; no `entries` unit |
| D-ROUTE | `all/reference/composing-pages` §Parameterized routes · Resolution | whole-document writes incl. MOVE are literal-only |
| D-TC | `all/reference/reacting-to-changes` §WebDAV bypasses constraints, §WebDAV edits never fire handlers | 60-second rule propagation |
| D-SM | `recipes/declaring-a-state-machine` §The repair path: WebDAV | |
| D-SES | `all/languages/sessel` | default scope, `self`, `Selector.execute()` |
| D-PRIM | `languages/javascript/client/primitives`, `recipes/reading-from-the-primitives-layer` | client OPTIONS description |

Combined vs individual pages: all protocol pages and the MOVE page are
content-identical to their combined counterparts (checked with `diff`). Two
index-level differences exist: QUERY is orphaned from the protocol group index
(above), and the reading-and-writing group index describes MOVE as relocating a
fragment "within or across documents", which the MOVE page itself contradicts
(C-12).

The `entries` range unit appears in only two pages: D-AR and D-Q. Both point to
"Reading and writing" for "the general `entries=` mechanism", but no page under
reading-and-writing (combined or individual) mentions `entries` at all. The
Pagination pages use query parameters (`paginate:page`), not a range unit (C-22).

### 0.2 Code (`research/upstream/`, commits from `research/COMMITS.txt`)

| Tag | File | Short commit |
|---|---|---|
| BJ-PRIM | `beta-js/pagelove/primitives.mjs` (official client) | c204746 |
| BJ-MAIN | `beta-js/pagelove.mjs` | c204746 |
| BJ-COMP | `beta-js/pagelove/component.mjs` (MOVE requests) | c204746 |
| BJ-SSE | `beta-js/pagelove/sse.mjs` | c204746 |
| BJ-SYNC | `beta-js/.github/scripts/sync-webdav.sh` (official WebDAV deploy tooling) | c204746 |
| BJ-VER | `beta-js/.github/scripts/verify-deploy.sh` | c204746 |
| BJ-CORS | `beta-js/cors.html` | c204746 |
| PP | `pagelove-primitives/index.mjs`, `pagelove-primitives/test-sw.js` | e73986d |
| DCORE / DFORMS | `dom-core/index.mjs`, `dom-forms/index.mjs` (older libraries) | 26d124f / ab7ddef |
| KANBAN | `pagelove-kanban/site/app.js` | 85109ab |
| POLLS | `pagelove-polls/site/admin/auth.html` | c9270e5 |
| ATS | `pagelove-ats/site/admin/auth.html` | 8f200fc |
| SHOP | `pagelove-shop/ops/deploy-pagelove.sh` | d887054 |
| DEMO | `demo-apps/README.md` | c4dd883 |
| PDEV | `pagelove-dev/skills/pagelove-dev/SKILL.md`, `.../references/cross-platform-command-recipes.md` | b489923 |
| CURSOR | `pagelove-cursor/plugins/pagelove/skills/pagelove-dev/SKILL.md` | b97c3ef |
| PROMPTS | `pagelove/prompts/src/partials/response_codes.liquid` | deb2857 |

Evidence found in agent skills, READMEs and deploy scripts is third-party
description of PageLove behavior (data), classed `demo-source` when it comes
from an app/demo repository and `client-source` when it is code in the
official client repository.

### 0.3 Live observations

`LIVE:<case>` = `read-probes.json` (earlier research notes, 2026-09-28,
anonymous requests to `https://docs.pagelove.com/` through Azure Front Door,
Python urllib, **no `Accept` header**). Recorded results used here:

| Case | Request | Result |
|---|---|---|
| `LIVE:capabilities` | `OPTIONS /` | `200`, `Allow: HEAD, GET, OPTIONS`, `Vary: Authorization, Accept`, empty chunked body, no `Accept-Ranges`, no `Accept-Query`, no `Content-Type` |
| `LIVE:element-capabilities` | `OPTIONS /` + `Range: selector=h1` + `Accept: multipart/mixed` | `204`, `Allow: GET, HEAD, OPTIONS`, `Vary: Authorization, Accept`, no body |
| `LIVE:query-css` | `QUERY /` + `Content-Type: text/css-selector`, body `h1` | `206`, `Content-Type: text/html`, `content-range: selector h1`, `etag` identical to the GET-fragment ETag, `vary: Host, Range, Accept`, body = the single `<h1>` element (not multipart) |
| `LIVE:query-sessel` | `QUERY /` + `Content-Type: text/sessel`, body `${h1}.count()` | `401`, `Content-Type: text/html; charset=utf-8`, body `itemtype="https://pagelove.org/1.0/Error"` with `resource` = `/index.html` |
| `LIVE:fragment` | `GET /` + `Range: selector=h1` | `206`, `content-range: selector h1` (space form), no `Accept-Ranges` |
| `LIVE:missing-fragment` | `GET /` + non-matching selector | `416`, body `itemtype="http://pagelove.org/Error"` (`name`, `statusCode`, `description`) |

### 0.4 Planes and vocabulary

- **edge** — the public application plane (`<site>.<domain>` in pagelike;
  PageLove's `dombase-http`, "the edge proxy"). End-user identities and
  AuthorizationRules apply.
- **dav** — the authoring plane (`dav-<site>.<domain>` in pagelike; PageLove's
  `dombase-webdav`). Authenticated by an authoring key; AuthorizationRules do
  not apply.
- *document-level rule* — an AuthorizationRule with an empty/whitespace
  `selector`; *selector-scoped rule* — one with a non-empty `selector`.
- *effective method set* — the methods an actor may perform at a path
  (document level) or at a selector (element level), per §2.

## 1. Shared wire conventions

### R-PROTO-1 — Header names, casing and list syntax
Header field names are case-insensitive. PageLove itself mixes casings on the
wire (`etag`, `content-range`, `vary` lowercase next to `Allow`, `Vary` in
title case; D-Q examples show lowercase `etag:`/`content-type:`). pagelike
SHOULD emit conventional casing (`ETag`, `Content-Range`, `Allow`,
`Accept-Ranges`, `Accept-Query`, `Content-Location`, `Last-Modified`, `Vary`)
and MUST NOT rely on any casing in requests. List-valued headers (`Allow`,
`Vary`, `Accept-Ranges`, `Accept-Query`) use `", "` (comma + one space) between
members.
- **Evidence:** live-observed (mixed casing), documented (examples) · **Confidence:** high
- **Source:** LIVE:fragment, LIVE:capabilities; D-Q examples; D-OPT examples.
- **Edge cases:** Go's `http.Header.Set` canonicalizes `ETag` to `Etag`; that is acceptable (clients read headers case-insensitively), but assigning the map key directly keeps `ETag`. Header casing *inside multipart part headers* is not normalized by HTTP stacks; see R-PROTO-4.

### R-PROTO-2 — `Range` / `Destination-Range` request syntax used by this area
- `Range: selector=<css>` — the unit token `selector`, `=`, then a CSS
  selector running to the end of the field value. Leading/trailing optional
  whitespace is trimmed; everything else (spaces, `=`, quotes, brackets,
  `>`, `,`) belongs to the selector.
- `Destination-Range: selector=<css>; placement=<p>` (MOVE only). The
  placement parameter is found by matching the field value against
  `^\s*selector\s*=\s*(.*?)\s*;\s*placement\s*=\s*([A-Za-z]+)\s*$` — i.e. the
  *last* `; placement=` wins, so a `;` inside the selector (e.g. inside a
  quoted attribute value) is preserved.
- `Range: entries=<start>-<end>` (QUERY Sessel mode; §8).
- Any other unit (`sessel=`, `rows=`, …) is unrecognised (R-PROTO-5).
- Header values are decoded as UTF-8 (cross-area: reading-writing / selector
  extensions; D-ROUTE neighbouring §Selector extensions says non-ASCII selectors
  in `Range` work as written).
- **Evidence:** documented (forms) · client-source (BJ-PRIM:263, BJ-COMP:777-779, KANBAN:9-12 build exactly these strings) · inferred (parsing algorithm) · **Confidence:** high (forms) / medium (parser)
- **Edge cases:** unit token matching SHOULD be case-insensitive (inferred). `selector=` with an empty selector is an invalid selector (422 for MOVE; see R-PROTO-99). A `Range` carrying `; placement=` on a MOVE is not part of the MOVE contract; pagelike SHOULD reject it as an invalid source selector only if the remainder fails to parse as CSS (inferred, low).

### R-PROTO-3 — `Content-Range` spelling emitted by pagelike
pagelike MUST emit `Content-Range` in the RFC 9110 shape *unit SP value*:
`Content-Range: selector <css>` and `Content-Range: entries <a>-<b>/<total>`.
It MUST NOT emit the legacy `selector=<css>` spelling.
- **Evidence:** live-observed (`content-range: selector h1` on GET and QUERY) · client-source (BJ-PRIM:193-202 comment: the server "now emits" the space form; PP test-sw.js:33-40 "The live server emits Content-Range in the HTTP `<unit> <value>` form") · documented (D-Q "Content-Range: selector <path>", "Content-Range: entries <start>-<end>/<total>") · **Confidence:** high
- **Contradiction:** D-OPT's 207 example and D-RW/D-PAG examples show `Content-Range: selector=h1` (C-2, C-25).
- **Edge cases:** clients must parse both. BJ-PRIM strips `^selector\s*=?\s*`; PP strips `^\s*selector\s*[=\s]\s*` case-insensitively; the older DCORE:64 and DFORMS:404 match `/selector=(.+)$/` only and would break on the space form (accepted: those libraries predate the change).

### R-PROTO-4 — `multipart/mixed` wire format (OPTIONS 207, QUERY)
Every multipart response pagelike emits MUST be RFC 2046 `multipart/mixed` with
these additional constraints imposed by the official parsers:
1. `Content-Type: multipart/mixed; boundary=<B>` where `boundary` is the
   **last** parameter and `<B>` is **unquoted**, 1–70 characters from
   `[0-9A-Za-z]`. (BJ-PRIM:169 and PP extract the boundary with
   `/boundary=(.+)$/`, so a quoted boundary or a trailing parameter breaks them.)
2. No preamble and no epilogue beyond an optional final CRLF.
3. Each part: `--<B>` CRLF, then header lines `Name: value` CRLF with
   **exactly one space after the colon** (BJ-PRIM:124 splits header lines on
   `": "`), then an empty line (CRLF), then the part body, then the next
   delimiter (CRLF `--<B>`), and finally `--<B>--`.
4. All line breaks are CRLF (BJ-PRIM splits on `"\r\n\r\n"` and `"\r\n"`).
5. Part bodies MAY be empty (OPTIONS parts are header-only).
6. A multipart body with zero parts is serialized as the close delimiter only:
   `--<B>--` CRLF (inferred; RFC 2046 has no zero-part form; all parsers above
   discard sections that trim to empty or `--`).
- Go's `mime/multipart.Writer` with `SetBoundary` (alphanumeric boundary)
  satisfies 1–5.
- **Evidence:** client-source · documented (D-OPT example layout) · inferred (zero-part form) · **Confidence:** high (1–5) / low (6)
- **Source:** BJ-PRIM:113-178; PP index.mjs:59-116, test-sw.js:30-55; DCORE multipart.mjs; DFORMS:170-187.
- **Edge cases:** a part-header value containing `": "` (e.g. a selector `[title="a: b"]`) is truncated by BJ-PRIM's parser at that point — pagelike cannot fix this; do not rewrite the selector. Part header names are lowercased by BJ-PRIM itself, so casing is free, but SHOULD be conventional.

> **Superseded by live observation (2026-09-28), items 1–2.** PageLove's
> framing, which pagelike reproduces:
> - The boundary is `boundary` followed by 32 alphanumerics, unquoted, and is
>   the last parameter.
> - There is no preamble.
> - Bodies with part content end at the close delimiter `--<B>--`, with **no**
>   trailing CRLF.
> - Header-only multiparts (OPTIONS 207) end `--<B>--` CRLF.
>
> See [LO-8](../compat/live-observations.md).

### R-PROTO-5 — Unrecognised range units are ignored
A `GET`/`HEAD` whose `Range` names a unit the server does not implement MUST be
answered as if no `Range` header were present: the whole representation,
`200`, no `Content-Range`. This explicitly covers the removed `sessel=` unit
("sending it now does nothing at all").
- **Evidence:** documented · **Confidence:** high
- **Source:** D-AR §Standard response (last paragraph).
- **Edge cases:** applies whatever the unit's value (`Range: sessel=${h1}.count()`, `Range: rows=0-1`). For writes (`PUT`/`POST`/`DELETE`) with an unknown unit the behavior is not documented (cross-area: reading-writing); for `MOVE` see R-PROTO-92. For `QUERY` css mode a `Range` header is not part of the contract (ignored, inferred).

### R-PROTO-6 — `Vary`
- OPTIONS responses (200/204/207): `Vary: Authorization, Accept` (exactly these
  two tokens). Documented and live-observed.
- PROPFIND responses: MUST include `Depth` in `Vary` (documented; D-DAV).
- Edge QUERY css responses in GET-equivalent mode (R-PROTO-63): the same
  `Vary` a GET fragment gets (live: `Host, Range, Accept`; owned by
  reading-writing).
- **Evidence:** documented · live-observed · **Confidence:** high
- **Source:** D-OPT §When there are no selector-scoped rules; LIVE:capabilities, LIVE:element-capabilities; D-DAV §Directory listings.
- **Edge cases:** pagelike identifies sessions by cookie; PageLove's OPTIONS still says only `Authorization, Accept`. Keep the documented value (a shared cache in front of pagelike must not cache OPTIONS; pagelike SHOULD also send `Cache-Control: private` on OPTIONS — inferred, not asserted by cases).

> **Refined by live observation (2026-09-28).**
> - PROPFIND carries `Vary: Depth` only for **collections**; file answers have
>   none.
> - Authoring-plane QUERY answers carry no `Vary`.
> - Edge css QUERY answers carry the GET sets:
>   - `Host, Range, Accept` for the fragment;
>   - `Host, Range, Accept, Paginate` for the multipart branch;
>   - `Host, Range` for errors.
> - Sessel JSON results carry `Vary: Host, Range`.
>
> OPTIONS is unchanged. See [LO-6](../compat/live-observations.md).

## 2. OPTIONS — capability discovery (edge plane)

### R-PROTO-10 — Purpose and invariants
`OPTIONS <path>` answers "which methods could this actor perform here?" purely
from AuthorizationRules (plus the host's default-GET mode) matched against the
request path, the actor and — when present — the requested selector string.
It MUST NOT modify any state, MUST NOT load the document, and MUST NOT evaluate
the selector against content.
- Consequences (MUST): OPTIONS on a path with no document answers normally
  (never `404`); OPTIONS with `Range: selector=<css>` whose selector would match
  nothing answers normally (never `416`).
- **Evidence:** documented · **Confidence:** high
- **Source:** D-OPT §When to reach for it, §OPTIONS never checks whether the target exists.
- **Edge cases:**
  - Syntactically invalid selector in `Range` (`h1[`): not documented. Because the selector is never evaluated, pagelike SHOULD still answer normally (no `422`) (inferred, low; P-5).
  - Paths under the reserved `/.pagelove/` namespace: answer from rules like any other path (inferred, low).
  - Parameterized-route URLs: rules are matched against the concrete request path (inferred from D-ROUTE, which authorizes selector writes on the route page URL).
  - The document ETag is unchanged by OPTIONS (case `protocol.options.does-not-modify`).

### R-PROTO-11 — Response form selection
1. If the request's `Accept` header explicitly lists the media type
   `multipart/mixed` with q > 0 → **multipart mode** (R-PROTO-16..19).
   `*/*`, `multipart/*` or an absent `Accept` do not select it (inferred:
   "that header selects the multipart form").
2. Otherwise, if a `Range: selector=<css>` header is present → **flat
   selector-scoped** response (R-PROTO-15).
3. Otherwise → **flat document-level** response (R-PROTO-14).
- **Evidence:** documented (1 selects multipart; 2 and 3 exist) · inferred (precedence and Accept parsing) · **Confidence:** high / medium
- **Source:** D-OPT §Standard response, §Selector-scoped OPTIONS, §Multipart OPTIONS.

### R-PROTO-12 — Effective method sets
Let *U_doc* = `GET, HEAD, PUT, DELETE, POST, MOVE, PATCH` and
*U_sel* = `GET, HEAD, PUT, DELETE, POST` (MOVE and PATCH apply only to whole
documents).
- **Rule normalization first:** MOVE-with-selector handling of R-PROTO-97
  applies (an `Allow` rule listing `MOVE` with a selector is discarded
  entirely; a `Deny` rule listing `MOVE` with a selector is kept with its
  selector removed, for all of its methods).
- **Document-level set** for actor A at path p: for each method m in
  *U_doc* ∪ {explicitly named supported methods, see below}, run the
  permissions area's conflict resolution over document-level rules whose
  actor matches A, whose resource matches p, and whose method list contains m
  or `*`. m is allowed if that yields Allow, or if **no** rule matched and
  m ∈ {GET, HEAD} and the host default-GET mode is `allow`. The default-GET
  mode never grants any other method (in particular never MOVE).
- **Selector-level set** for selector string s: as above, but over
  document-level rules ∪ selector-scoped rules whose `selector`, after
  trimming and collapsing internal whitespace, equals s; methods drawn from
  *U_sel* (plus explicitly named supported methods other than MOVE/PATCH).
- A method named explicitly in a rule (`QUERY`, …) is reported only if
  pagelike implements it for that plane (`QUERY` on the edge). The wildcard
  `*` expands to *U_doc* / *U_sel* only — it never contributes `QUERY` and is
  never reported literally.
- `OPTIONS` is always appended to every reported set, including an otherwise
  empty one.
- **Evidence:** documented (wildcard expansion; OPTIONS always listed in all examples; never `*`) · live-observed (default-GET host, no rules → `HEAD, GET, OPTIONS`) · inferred (selector-string matching, union with document-level rules, explicit QUERY) · **Confidence:** high (expansion) / medium (default-GET) / low (selector-string equality)
- **Source:** D-OPT §When to reach for it; D-RULE §`method`; D-MOVE §A MOVE rule must not carry a selector; LIVE:capabilities.
- **Edge cases:**
  - D-OPT's 207 example shows the selector part `GET, POST, PUT, OPTIONS` while the document part is `GET, OPTIONS`: selector-scoped `Allow` rules add methods on top of document-level grants, and document-level grants carry into selector parts.
  - A selector-scoped `Deny` for m at s removes m from s's set when it is at the top actor-specificity tier (deny wins at equal tier; cross-area permissions).
  - Templated rule fields (`${request.auth.username}`) are substituted per request before matching (cross-area permissions).
  - Whether rules for *other* actors influence the set: they do not (only actor-matching rules participate).
  - LIVE:capabilities shows `HEAD` granted along with `GET` by the default-GET mode. Whether an explicit `GET` rule also implies `HEAD` is not documented (apps list `HEAD` explicitly: POLLS:31); pagelike SHOULD treat an explicit GET grant as also granting HEAD (inferred, low; cases never assert HEAD's absence or presence under an explicit GET rule).

> **Superseded by live observation (2026-09-28).**
> - The **flat document-level** answer (R-PROTO-14) is the union of the
>   document-level set and every method that an actor-matching
>   selector-scoped rule grants on the path (element methods only).
> - In the **207**, the first part lists the document-level set. Each
>   selector part lists only what that selector's selector-scoped rules grant
>   (plus `OPTIONS`). Document-level grants no longer carry into selector
>   parts: live answered `Allow: POST, OPTIONS` for `ul` under a document
>   `GET` rule.
> - Live PageLove ignores Deny rules when advertising, including a same-tier
>   selector-scoped Deny. pagelike keeps removing denied methods so that
>   `Allow` matches what it enforces (keep-documented-security;
>   `protocol.options.selector-deny-removes-method.live`).
>
> Case: `protocol.options.selector-scoped-flat`. See
> [decisions](../compat/decisions.md).

### R-PROTO-13 — `Allow` formatting and order
`Allow` lists method tokens in upper case separated by `", "`. pagelike MUST
emit the canonical order `GET, HEAD, PUT, DELETE, POST, MOVE, PATCH, QUERY,
OPTIONS` (restricted to the allowed subset, `OPTIONS` always last). Clients and
harness cases MUST treat the order as insignificant.
- **Evidence:** documented (order of the wildcard list; OPTIONS last in every example) · live-observed (PageLove itself is inconsistent: `HEAD, GET, OPTIONS` on the 200 and `GET, HEAD, OPTIONS` on the 204) · **Confidence:** medium
- **Contradiction:** C-3.

### R-PROTO-14 — Flat document-level response
- Status `200`. Headers: `Allow: <document-level set>`,
  `Vary: Authorization, Accept`, `Accept-Ranges: selector` when the path is
  HTML-typed (R-PROTO-25), `Accept-Query: text/css-selector, text/sessel`
  (R-PROTO-42). No body (`Content-Length: 0`), no `Content-Type`, no `ETag`.
- **Evidence:** live-observed (status, Allow, Vary, empty body) · documented (Accept-Ranges, Accept-Query) · **Confidence:** high (status/Allow/Vary), medium (the two documented headers — absent in LIVE:capabilities, C-4, C-5)
- **Source:** D-OPT §Standard response; LIVE:capabilities.

### R-PROTO-15 — Flat selector-scoped response (`Range: selector=<css>`)
- Status `200`. Same headers as R-PROTO-14 but `Allow` is the selector-level
  set for the requested selector string (R-PROTO-12). The literal `*` MUST
  NOT appear; a wildcard grant is expanded.
- **Evidence:** documented (status 200, Accept-Ranges) · **Confidence:** high (status) / medium (Allow computation)
- **Source:** D-OPT §Selector-scoped OPTIONS.
- **Contradiction:** the documented example response is `Allow: *, OPTIONS`, which contradicts the rule that `Allow` never contains a literal `*` (same page, §When to reach for it; D-RULE §method). Decision: expand (C-1). The disputed example is kept as a `status: disputed` case.
- **Edge cases:** selector matching nothing or document absent → still 200 (R-PROTO-10). A selector string that equals no rule's selector → the set degenerates to the document-level set restricted to *U_sel* (inferred).

### R-PROTO-16 — Multipart mode: when it is a 207
In multipart mode the response is `207 Multi-Status` with a multipart body iff
at least one **actor-matching** selector-scoped rule (Allow or Deny, after
R-PROTO-97 normalization) has a resource pattern matching the request path.
Otherwise it is the `204` fallback (R-PROTO-18).
- **Evidence:** documented ("when the target path has at least one selector-scoped authorization rule") · inferred (restriction to actor-matching rules) · **Confidence:** high (criterion) / low (actor restriction; P-4)
- **Source:** D-OPT §Multipart OPTIONS.
- **Edge cases:** the document need not exist (207 is still produced — R-PROTO-10). A path whose only selector-scoped rules are `Allow MOVE` rules (discarded) has no selector-scoped rules → 204.

### R-PROTO-17 — 207 body
- `Content-Type: multipart/mixed; boundary=<B>` per R-PROTO-4, plus
  `Vary: Authorization, Accept`, `Accept-Ranges: selector` (HTML-typed path),
  `Accept-Query`.
- **Part 1** (document level): single header `Allow: <document-level set>`;
  no `Content-Range`; empty body.
- **One further part per distinct selector string** among the qualifying
  selector-scoped rules, in first-appearance order of the rules (rule document
  order; inferred): headers `Content-Range: selector <css>` (R-PROTO-3; the
  rule's selector text after `${…}` substitution, verbatim otherwise) then
  `Allow: <selector-level set>`; empty body.
- **Evidence:** documented (layout, header pair per part, document part first) · client-source (space-form Content-Range; clients ignore parts without Content-Range: BJ-PRIM:193) · inferred (dedupe, order) · **Confidence:** high (layout) / medium (spelling) / low (order)
- **Source:** D-OPT §Multipart OPTIONS example; BJ-PRIM:180-221; PP test-sw.js:30-55.
- **Superseded by live observation (2026-09-28), in part:** the 207 carries no
  top-level `Allow` header (the 204 fallback does), and selector parts list
  selector-scoped grants only (see R-PROTO-12).
- **Edge cases:**
  - The part selector is what BJ-PRIM hands to `DOMSubscriber.subscribe(document, selector)` in the browser, i.e. it must be a selector the browser's `querySelectorAll` accepts. Rule selectors using PageLove-only pseudo-classes (`:value-equals()`, `:isa()`…) will throw client-side; pagelike still emits them verbatim (inferred).
  - Selectors containing `": "` are truncated by BJ-PRIM (R-PROTO-4).
  - An element-level part whose set is only `OPTIONS` is still emitted (inferred).

### R-PROTO-18 — Multipart mode: 204 fallback
When multipart mode is requested but R-PROTO-16's criterion fails (only
document-level rules, or no matching rules at all), the response is
`204 No Content` with exactly the flat document-level headers of R-PROTO-14
(`Allow`, `Vary: Authorization, Accept`, `Accept-Ranges: selector`,
`Accept-Query`) and no body, no `Content-Type`.
- **Evidence:** documented · live-observed · **Confidence:** high
- **Source:** D-OPT §When there are no selector-scoped rules; LIVE:element-capabilities.
- **Edge cases:** clients must read the status before parsing (documented). The official client does not: see R-PROTO-21.

### R-PROTO-19 — Multipart mode with a `Range` header
A `Range: selector=` header does not change multipart mode: with no qualifying
selector-scoped rules the answer is the 204 fallback (live-observed); with
qualifying rules pagelike emits the full 207 enumeration, ignoring `Range`
(inferred, low; P-4).
- **Source:** LIVE:element-capabilities.

### R-PROTO-20 — `Prefer: return=representation`
Every official client sends `Prefer: return=representation` with
`Accept: multipart/mixed` on OPTIONS. pagelike MUST accept the header and
(pending P-3) MUST NOT change the response because of it: status and body are
decided by R-PROTO-16/18 alone. pagelike SHOULD NOT send `Preference-Applied`.
- **Evidence:** client-source (header sent) · inferred (ignored; the docs never mention it and describe 204 as the answer to clients that "always send" the multipart Accept) · **Confidence:** medium
- **Source:** BJ-PRIM:184-187; PP index.mjs:119-126; DCORE:47-52; DFORMS:388-394; CURSOR:451-452.

### R-PROTO-21 — Client consumption contract (what beta-js needs)
`beta-js/pagelove.mjs` issues `OPTIONS <page URL>` with `Prefer:
return=representation` and `Accept: multipart/mixed` on module load
(`export const ready = _doc.OPTIONS()`, BJ-MAIN:43) and awaits it in
`start()` (BJ-MAIN:98). For each part with a `content-range` header it strips
the unit, then for every element matching that selector dispatches a
`PLCapability` event whose `allow` is the part's `Allow` split on `,` and
trimmed; methods GET/PUT/POST/DELETE (upper-cased) become callable on the
element. Therefore pagelike MUST: use the 207 layout of R-PROTO-17, spell method
tokens in upper case, and never put `*` in `Allow`.
- **Known upstream incompatibility (not to be "fixed" server-side):** on any
  2xx OPTIONS response whose `Content-Type` lacks `boundary=` (i.e. the
  documented 204 fallback) BJ-PRIM's `MultipartMessage` constructor warns and
  returns an instance without `.message`; reading `.parts` then rejects with a
  `TypeError`, so `ready` rejects and `Pagelove.start()` throws (verified by
  running the class under Node). On a non-2xx OPTIONS it throws explicitly.
  PP (pagelove-primitives) throws `"HTTP Message is not multi-part"` on the
  204. Pages that rely on beta-js therefore need at least one selector-scoped
  rule for the viewing actor. pagelike follows the documented 204 (C-17).
- **Evidence:** client-source · **Confidence:** high
- **Source:** BJ-PRIM:133-221, BJ-MAIN:43,98,1123; PP index.mjs:78-81.

### R-PROTO-22 — OPTIONS is never refused for lack of an OPTIONS grant
pagelike MUST answer OPTIONS (200/204/207 per above) for any actor, including
anonymous on a deny-by-default host, without requiring a rule that grants the
`OPTIONS` method; the worst case is `Allow: OPTIONS`. Rules naming `OPTIONS`
are accepted and have no further effect (inferred).
- **Evidence:** inferred (OPTIONS "answers purely from authorization rules", documented examples never show 401/403) · demo-source counter-hint (POLLS:31 and ATS:117,124,140 grant `OPTIONS` explicitly, which would be pointless if OPTIONS were always answered) · **Confidence:** low (P-6)

### R-PROTO-23 — CORS preflight (cross-area)
An `OPTIONS` request carrying `Origin` and `Access-Control-Request-Method` is a
CORS preflight. PageLove "does emit CORS headers, but only for this host's own
canonical name and declared aliases" (BJ-CORS:14-18, a comment in the official
repository). pagelike MUST answer preflights for configured aliases with the
appropriate `Access-Control-Allow-*` headers *in addition to* the capability
headers above; for other origins no `Access-Control-Allow-Origin` is sent.
Exact preflight headers are owned by the site/config area (flagged cross-area).
- **Evidence:** client-source (comment) · **Confidence:** low

### R-PROTO-24 — OPTIONS on the dav plane
Not documented. pagelike SHOULD answer with WebDAV class headers (`DAV: 1` or
`DAV: 1, 2` if LOCK is implemented), an `Allow` listing the implemented WebDAV
methods plus `QUERY`, and `Accept-Query: text/css-selector` (inferred from D-Q
§QUERY: "Servers advertise … on OPTIONS responses via the Accept-Query header").
- **Evidence:** inferred · **Confidence:** low (P-23)

### R-PROTO-25 — "HTML-typed path" (for `Accept-Ranges` on OPTIONS)
Because OPTIONS never loads the document, "when the resource is HTML" is
decided from the path: a path ending in `/`, `.html` or `.htm`, or with no
extension in its last segment, is HTML-typed (inferred from D-UP rule 1: `.html`
/ `.htm` are always `text/html`). XML extensions (`.xml`, `.xhtml`) SHOULD also
get `Accept-Ranges: selector` (inferred). Other extensions: omit the header.
- **Evidence:** inferred · **Confidence:** low

## 3. Accept-Ranges and range units

### R-PROTO-30 — `Accept-Ranges: selector, bytes` on HTML responses
Every response carrying (part of) an HTML document — `GET`/`HEAD` with status
`200`, `206`, `304` — MUST include `Accept-Ranges: selector, bytes` whether or
not the request used `Range`. pagelike SHOULD also include it on a `416` for an
existing HTML document. XML documents (selector-addressable) SHOULD get the same
value (inferred).
- **Evidence:** documented · **Confidence:** high (value) / medium (status coverage)
- **Source:** D-AR §Standard response.
- **Contradiction:** LIVE:fragment and LIVE:missing-fragment carry no `Accept-Ranges` at all, although D-AR predicts the CDN would leave `bytes` (C-5). Harness cases never assert `Accept-Ranges` against live.

### R-PROTO-31 — OPTIONS advertises `Accept-Ranges: selector` only
- **Evidence:** documented · **Confidence:** high · **Source:** D-AR §Standard response; D-OPT §Standard response. Applies to 200/204/207 (R-PROTO-14..18).

### R-PROTO-32 — `entries` is supported but not advertised
`entries` MUST NOT appear in `Accept-Ranges` (documented "not advertised").
Clients hard-code it.
- **Evidence:** documented · **Confidence:** high · **Source:** D-AR §Standard response.

### R-PROTO-33 — No expression range unit
There is no range unit that evaluates an expression; `sessel=` was removed and
is handled by R-PROTO-5. Expression evaluation is only via `QUERY` with
`Content-Type: text/sessel` (§7).
- **Evidence:** documented · **Confidence:** high · **Source:** D-AR §Standard response.

### R-PROTO-34 — Blobs
Non-HTML/XML blobs are not selector-addressable (D-UP: selector reads/writes
fail with `422`). pagelike MUST NOT advertise `selector` on blob responses; it
SHOULD advertise `bytes` only if it implements byte ranges for blobs, else omit
the header (inferred, P-28).

### R-PROTO-35 — CDN caveat (informative)
PageLove's CDN rewrites `Accept-Ranges` (documented: to `bytes`; observed:
removed). Selector ranges still work end-to-end. pagelike has no such rewrite;
a pagelike deployment behind a CDN may experience the same.
- **Source:** D-AR §Known issue.

### R-PROTO-36 — `bytes` ranges on HTML
`bytes` is advertised for HTML but its semantics on composed HTML are not
documented (range over the composed representation? the stored bytes?).
pagelike SHOULD implement standard RFC 9110 byte ranges over the exact
representation that the same request without `Range` would return (inferred,
low; P-27). Cross-area: reading-writing.

## 4. QUERY — common rules

### R-PROTO-40 — Method semantics
`QUERY` (draft-ietf-httpbis-safe-method-w-body) carries the query in the
request body. It is safe and idempotent: pagelike MUST NOT mutate documents,
emit SSE events, or run write-side triggers/processors for it (inferred from
"safe"). Read-side composition, resolvers and processors that apply to GET MAY
run in edge mode (cross-area: composing, reacting-to-changes).
- **Evidence:** documented (safe, idempotent, cacheable) · inferred (no side effects list) · **Confidence:** high / medium
- **Source:** D-Q intro.

### R-PROTO-41 — Mode selection by request `Content-Type`
The media-type essence of `Content-Type` (case-insensitive, parameters such as
`charset` ignored) selects the mode: `text/css-selector` → CSS-selector mode;
`text/sessel` → Sessel mode. The body is decoded as UTF-8 and trimmed of
leading/trailing whitespace before use (inferred).
- **Evidence:** documented (two types, different behavior) · inferred (parsing) · **Confidence:** high / medium
- **Source:** D-Q intro.

### R-PROTO-42 — `Accept-Query`
pagelike MUST send `Accept-Query` listing the query media types the target
accepts, comma-separated, unquoted:
- edge plane: `Accept-Query: text/css-selector, text/sessel`
- dav plane: `Accept-Query: text/css-selector`
on (a) every OPTIONS response of that plane and (b) every `415` response to a
QUERY.
- **Evidence:** documented · **Confidence:** medium (header value on the edge is inferred from the plane matrix; D-Q's table shows `Accept-Query: text/css-selector` for the css-mode 415)
- **Source:** D-Q intro, §CSS-selector mode · Errors.
- **Contradiction:** LIVE:capabilities has no `Accept-Query` (C-4).

> **Superseded by live observation (2026-09-28), part (b) on the edge.**
> Edge QUERY never answers `415`, so no `Accept-Query` accompanies its errors
> (R-PROTO-43). Part (a) is kept as a documented superset (C-4, reviewed in
> [decisions](../compat/decisions.md)). The dav plane's `415` still carries
> `Accept-Query: text/css-selector`.

### R-PROTO-43 — Unsupported or missing query type → 415
A QUERY whose `Content-Type` is missing, or not one of the plane's accepted
query types, MUST be answered `415 Unsupported Media Type` with `Accept-Query`
(R-PROTO-42) and an error document (R-PROTO-130). On the dav plane
`text/sessel` is unsupported → 415.
- **Evidence:** documented (intro sentence; css table) · **Confidence:** medium
- **Contradiction:** D-Q's Sessel error table maps "missing/wrong Content-Type" to `400` (C-9). Decision: 415. The losing claim is a `status: disputed` case.

> **Superseded by live observation (2026-09-28) on the edge (C-9
> reversed).** An edge QUERY whose `Content-Type` is not `text/css-selector`
> takes the Sessel path:
> 1. It is authorized as method `QUERY` (`401`/`403` without a grant).
> 2. It is then answered `400`, read vocabulary, "QUERY method requires
>    Content-Type: text/sessel", with no `Accept-Query`.
>
> The disputed `protocol.query-sessel.400-wrong-type-documented` passed live
> and is now the winning claim. The dav plane keeps `415`. Cases:
> `protocol.query-edge.415-unsupported-type`,
> `protocol.query-sessel.415-unsupported-type`.

### R-PROTO-44 — Plane support matrix
| Plane | `text/css-selector` | `text/sessel` |
|---|---|---|
| dav | host (`/`), prefix (`…/`), single document; raw stored markup; always multipart | not served (415) |
| edge | single, individually addressed document; composed page; authorized like GET | served (target document or directory) |
- **Evidence:** documented · **Confidence:** high (css), medium (sessel on dav = 415 is inferred from "Served by the edge proxy")
- **Source:** D-Q intro and §CSS-selector mode.

## 5. QUERY, CSS-selector mode, dav plane

### R-PROTO-50 — Scope from the target URI
| Target URI | Scope |
|---|---|
| `/` | every document on the host |
| a path ending in `/` | every document whose path starts with that prefix (recursive) |
| any other path | that single document |
Documents considered are HTML (and XML, inferred) documents; blobs are skipped
(inferred: not selector-addressable, D-UP). Documents under `/.pagelove/`
SHOULD be skipped (inferred).
- **Evidence:** documented · **Confidence:** high (table) / medium ("recursive": "every document under that prefix")
- **Source:** D-Q §On the WebDAV authoring server.

### R-PROTO-51 — Authentication and authorization
Authenticated like every dav request (R-PROTO-111: authoring key; the docs
example uses `Authorization: Basic …`). AuthorizationRules do not filter the
result (the authoring plane bypasses rules, R-PROTO-111).
- **Evidence:** documented (Basic in example) · demo-source (rules bypassed on WebDAV) · **Confidence:** medium

### R-PROTO-52 — Evaluation
The selector is evaluated against the **raw stored markup** of each in-scope
document (no templates, includes, bindings, stamps — those are edge-only), with
pagelike's full selector dialect (cross-area: selector extensions). All matches
are returned (not just the first), in document order within a document;
documents are ordered by ascending byte-wise path (inferred, P-26).
- **Evidence:** documented (raw vs composed contrast, "every matching element") · inferred (ordering) · **Confidence:** high / low
- **Source:** D-Q intro and §On the edge proxy ("not the raw stored markup").

### R-PROTO-53 — Success response
`200 OK`, `Content-Type: multipart/mixed; boundary=<B>` (R-PROTO-4), a
top-level `ETag` (R-PROTO-56). The multipart shape is used at every scope,
including a single document with a single match.
- **Evidence:** documented (text) · **Confidence:** high (multipart) / medium (200: text says a no-match query "still returns 200 OK"; the success example shows `201` with a `text/html` single-document body — C-7)
- **Source:** D-Q §On the WebDAV authoring server.

### R-PROTO-54 — Part format
One part per match. Part headers, in this order:
- `Content-Type: text/html` (inferred; not listed by the docs)
- `Content-Location: <document path>` — absolute path of the source document
  (path only, no scheme/host; inferred)
- `Content-Range: selector <doc-rooted selector>` — a selector rooted at the
  document that addresses exactly this match, suitable for a follow-up
  `Range: selector=<it>` `PUT`/`DELETE` (first-match semantics must hit this
  element). It is **not** the request selector.
- `ETag: <fragment ETag>` — the element-level ETag this element would have in a
  GET fragment response (cross-area: reading-writing ETags).
- `Last-Modified: <IMF-fixdate>` of the fragment (pagelike MAY use the
  document's modification time; inferred).
Body: the element's outer HTML serialized from the stored DOM.
- **Doc-rooted selector recommendation** (inferred): `#<id>` when the element
  has an `id` that is unique in the document; otherwise a child-index chain
  from the root, `:root > body:nth-child(2) > main:nth-child(1) > h1:nth-child(1)`
  (`:nth-child` over element children at every step).
- **Evidence:** documented (four header semantics) · inferred (Content-Type, order, selector form) · **Confidence:** high (presence) / low (exact spellings; P-26)
- **Source:** D-Q "Each part carries".

> **Superseded by live observation (2026-09-28): exact spellings (P-26
> settled).** Headers in this order:
> - `Content-Type`
> - `Content-Location: <document path>`
> - `Content-Range: selector html:nth-child(1) > body:nth-child(1) > …`:
>   every step is `name:nth-child(i)` from `html` down, and an implied empty
>   `<head>` that the source never wrote does not count as a sibling;
> - `ETag: <fragment tag>` (R-RW-97 formula)
> - `Last-Modified: <the document's>`
> - `Content-Length`
>
> The top-level answer carries `ETag` (one quoted hash), `Accept-Ranges: bytes`
> and `Last-Modified`, and no `Vary`.

### R-PROTO-55 — No matches
A query that matches nothing (in an existing scope) returns `200 OK` with a
multipart body containing zero parts (R-PROTO-4 item 6) and a top-level ETag.
- **Evidence:** documented · **Confidence:** high (status) / low (exact bytes)

### R-PROTO-56 — Top-level ETag and conditional re-query
The top-level `ETag` is a function of the selector text and the ordered list of
matched fragments' ETags: it changes whenever the selector text, the match set
or any matched fragment changes, and only then. Recommended:
`"q-" + hex(sha256(selectorText + "\n" + join(fragmentETags, "\n")))`
(strong). A QUERY with `If-None-Match` listing that ETag (or `*` when at least
one part would be returned — inferred) MUST be answered `304 Not Modified`
with the `ETag` header and no body.
- Selector text is the trimmed body; `h1` and `body h1` are different texts and
  therefore different ETags even if they match the same element (documented).
- A change to an unmatched part of a document does not change the ETag if
  fragment ETags are content-derived (inferred, medium — follows from the
  definition).
- **Evidence:** documented · **Confidence:** high
- **Source:** D-Q §Conditional re-query.
- **Contradiction:** the example answer to the conditional request is `HTTP/1.1 200` with a different ETag, while the text promises 304 (C-7). Decision: 304 when unchanged.

> **Superseded by live observation (2026-09-28), second bullet.** Fragment
> tags end in the document version (R-RW-97), so **any** write to a document
> changes its parts' tags and the answer's tag, even outside the matched
> fragments. The 304 on an unchanged answer holds. Case:
> `protocol.query-dav.etag-ignores-unmatched-change`.

### R-PROTO-57 — Errors
| Condition | Status |
|---|---|
| no dav credentials / invalid key | `401` (R-PROTO-111) |
| `Content-Type` missing or not `text/css-selector` | `415` + `Accept-Query: text/css-selector` |
| empty (whitespace-only) body | `422 Unprocessable Content` |
| selector fails to parse | `422` |
| `Accept` present and does not admit `multipart/mixed` (e.g. `Accept: text/html`) | `406 Not Acceptable` |
| single-document target does not exist; prefix target with no documents | `404 Not Found` |
Evaluation order (inferred): 401 → 415 → 406 → 422 → 404.
- **Evidence:** documented (table) · inferred (prefix-without-documents → 404, order) · **Confidence:** high / low
- **Source:** D-Q §CSS-selector mode · Errors.
- **Edge cases:** `Accept: */*`, `multipart/*`, absent → acceptable. Target is a blob → `422` (inferred from D-UP).

> **Refined by live observation (2026-09-28): error bodies.** The statuses
> hold. The bodies are PageLove's short authoring articles:
> - QUERY refusals and a missing key: `https://pagelove.org/Error/Internal`,
>   with `status` meta and `message` p.
> - Missing paths: `Error/NotFound`, "Not found: <p>" or "Collection not found:
>   <p>".
>
> See R-PROTO-120 and [LO-12](../compat/live-observations.md).

## 6. QUERY, CSS-selector mode, edge plane

### R-PROTO-60 — Target
Only a single, individually addressed document. A path ending in `/` is
resolved exactly as a GET would resolve it (its `index.html`, live-observed:
`QUERY /` answered from `/index.html`); host-wide and subtree scopes are not
available on the edge. A directory path without an index document → `404`
(inferred, same as GET).
- **Evidence:** documented · live-observed · **Confidence:** high / medium
- **Source:** D-Q §On the edge proxy; LIVE:query-css.

### R-PROTO-61 — Composed page
The selector is evaluated against the **composed** page (after templates,
includes, expression bindings, stamps, pagination), the same DOM a GET would
serialize for this request.
- **Evidence:** documented (text) · **Confidence:** medium
- **Contradiction:** the docs example's response body shows the uncomposed Liquid source `{{ 2 | plus: 3 }}` inside a full HTML document (C-8). Decision: composed.

### R-PROTO-62 — Authorization
Authorized exactly as a `GET` of that path by the same actor, including the
default-GET mode (anonymous css QUERY succeeded on a default-GET host with no
rules). Denied → `401` (anonymous) / `403` (authenticated) with the permissions
area's error document.
- **Evidence:** documented · live-observed · **Confidence:** high
- **Source:** D-Q §On the edge proxy; LIVE:query-css.
- **Edge cases:** multi-match authorization follows the GET rules of D-RULE: a GET-equivalent single-match answer authorizes only the first match; a multipart all-matches answer requires every match to be permitted (cross-area permissions).

### R-PROTO-63 — Response shape (compatibility decision)
- **Default (no `Accept`, `*/*`, `text/html`, or anything not explicitly
  preferring `multipart/mixed`)**: behave exactly like `GET <path>` with
  `Range: selector=<body>`: `206 Partial Content`, `Content-Type: text/html`,
  `Content-Range: selector <request selector>`, the first match's outer HTML,
  the same `ETag` and `Vary` the GET fragment response would carry; no match →
  `416` like GET (inferred from the equivalence; P-8).
- **`Accept` explicitly listing `multipart/mixed` (q > 0, preferred over
  text/html)**: every match as multipart per §5's part format, `200`, top-level
  ETag and `If-None-Match` → `304` as in R-PROTO-56; no match → `200` with an
  empty multipart (documented for QUERY).
- **Evidence:** live-observed (default) · documented (multipart) · inferred (Accept-driven reconciliation) · **Confidence:** high (default) / low (multipart branch)
- **Source:** LIVE:query-css; D-Q §CSS-selector mode ("Even a single-document query returns multipart/mixed").
- **Contradiction:** C-6 (docs: always multipart; live: single fragment 206).

> **Confirmed and refined by live observation (2026-09-28).** C-6 is
> confirmed: the disputed `protocol.query-edge.documented-multipart` is XFAIL.
> - The multipart branch uses the GET all-matches format (R-RW-31 as
>   reconciled): parts named by the request selector, `Vary: Host, Range,
>   Accept, Paginate`.
> - A missing document is `404` "Document not found".
> - A non-HTML target is `422` (read vocabulary).

### R-PROTO-64 — Errors
Same table as R-PROTO-57, plus `401`/`403` from R-PROTO-62; `415` carries
`Accept-Query: text/css-selector, text/sessel`. An invalid selector → `422`
(documented for css mode; P-10 because GET-equivalence might instead yield
GET's invalid-selector status).
- **Evidence:** documented · **Confidence:** medium

> **Superseded by live observation (2026-09-28) (P-10 settled).**
> - An invalid selector is `416` "HTML parsing error: Invalid CSS selector:
>   …", like a GET.
> - A non-css content type is `400` on the Sessel path (R-PROTO-43), not
>   `415`.
>
> Cases: `protocol.query-edge.422-invalid-selector`,
> `protocol.query-edge.415-unsupported-type`.

## 7. QUERY, Sessel mode (edge plane)

### R-PROTO-70 — Request
`QUERY <path>` with `Content-Type: text/sessel`; the body is one Sessel program
(cross-area: sessel language). Served by the edge only (R-PROTO-44).
- **Source:** D-Q §Sessel mode.

### R-PROTO-71 — Bindings and scope
- Target is a document → `self` is that document's root element and `prior` is
  `null` (inferred: no write in progress).
- Target is a directory (path ends in `/`) → `self` is unbound; evaluating an
  expression that references `self` (e.g. `from self`, bare `self`) fails with
  `416`. Expressions that do not reference `self` evaluate normally.
- Bare selector literals without `from` query the **entire site** per the Sessel
  language (D-SES "By default, a selector queries across the entire site"); the
  QUERY target does not narrow them. `from self` or `Selector.execute()`
  (equivalent to `from self`) scopes to the target.
- `request` context (cross-area sessel) is the QUERY request.
- **Evidence:** documented · **Confidence:** high (self) / medium (bare-selector scope — the D-Q examples use bare `${h1}` apparently expecting document scope, C-10)
- **Source:** D-Q §Sessel mode; D-SES §`self` and `prior`, §Scopes (lines 126-170 of the combined page), §Selector type.
- **Edge cases:** a directory path whose `index.html` exists: the docs say "querying a directory leaves `self` unbound"; LIVE:query-sessel reported `resource /index.html` for `QUERY /`, suggesting index resolution happens first. Decision: follow the docs text (unbound) pending P-22.

> **Superseded by live observation (2026-09-29): document scope (C-10 settled).**
> A QUERY program sees only its target document. Bare selectors search the
> target; path, glob, element and document sources match nothing; elements
> carry no path or document; `Pagelove.GET` is "unknown function: GET". This
> keeps QUERY, which is authorized on the request path alone, from reading
> other documents (decisions-2026-09-29/sessel.md).
>
> **Superseded by live observation (2026-09-28) (P-22 settled).**
> - A directory URL means its `index.html`, as for GET. `self` is that
>   document, and a missing index is `404` "Document not found:
>   <dir>/index.html" before evaluation.
> - `new Selector {…}.execute()` is **not** available ("unknown function:
>   execute"). Use `${sel} from self` to scope to the target (C-10).
>
> Case: `protocol.query-sessel.directory-self-unbound`.

### R-PROTO-72 — Result media type
| Result | Status | `Content-Type` | Body |
|---|---|---|---|
| a single element | `206` (see C-7) | `text/html` | the element's outer HTML |
| anything else (string, number, boolean, list, dictionary, null, …) | `200` | `application/sessel+json` | R-PROTO-73 |
With `Range: entries=` on a list result: `206` with `Content-Range` (§8).
- **Evidence:** documented (type mapping) · documented-but-contradictory (status) · **Confidence:** high (types) / low (status; P-21)
- **Source:** D-Q §Sessel mode.
- **Contradiction:** both D-Q examples pair requests with mismatched responses (`${h1}.first()` → `206` + `<main class="calc">5</main>`; `${li}.count()` → `200` + `text/html` `<h1>James</h1>`). Harness cases accept `[200, 206]` for element results.

### R-PROTO-73 — `application/sessel+json`
JSON (RFC 8259) of the result value, with one extension: every element value
nested anywhere in the result is encoded as a tagged object
`{"$type": "element", "$html": "<outer HTML>", "$source": "<document path>"}`;
`$source` is omitted for elements constructed with `new` (not selected from
storage). A list of elements is a JSON array of tagged objects; a dictionary is
a JSON object; `null` → `null`; booleans/numbers/strings as JSON.
- Recommended key order: `$type`, `$html`, `$source` (docs order; JSON
  consumers must not depend on it).
- Serialization of Sessel values with no JSON analogue (schema Instances,
  temporal values, Selector values, Blobs) is not documented (P-21). pagelike
  SHOULD encode Instances as their element form (tagged object), temporal values
  as ISO 8601 strings, Selector values as their selector string (inferred, low).
- **Evidence:** documented · **Confidence:** high (element tagging) / low (other types)
- **Source:** D-Q §Sessel mode.

> **Superseded by live observation (2026-09-29).** Selected elements are
> encoded `{"$type": "element", "$html": "<outer HTML>"}`, with **no**
> `$source`. Case: `protocol.query-sessel.element-list-tagged`.

### R-PROTO-74 — Authorization (compatibility decision)
A Sessel QUERY can read any document on the site through bare selectors, so it
MUST NOT be granted by the default-GET mode or by read permission on the target
alone. pagelike authorizes it as method `QUERY` against the request path:
allowed only when an AuthorizationRule grants `QUERY` (or `*`) to the actor for
that path; otherwise `401` (anonymous) / `403` (authenticated).
- **Evidence:** live-observed (anonymous → 401 on a default-GET-allow host where GET and css QUERY of the same path succeed) · inferred (the rule-based grant) · **Confidence:** medium (denial) / low (grant mechanism; P-12)
- **Source:** LIVE:query-sessel.
- **Edge cases:** the css mode on the same path stays GET-authorized (R-PROTO-62). Whether evaluation additionally checks per-document read permission for cross-document selectors is owned by the sessel/permissions areas (D-SES: bare selectors read the whole site; PDEV SKILL.md:258: resource bindings query the whole site graph without per-request authorization; pagelike SHOULD apply the same trusted-author model only to *stored* expressions, not to request-supplied ones — flagged).

### R-PROTO-75 — Errors
| Condition | Status |
|---|---|
| actor not granted QUERY | `401` / `403` (R-PROTO-74) |
| `Content-Type` missing/other | `415` + `Accept-Query` (R-PROTO-43; docs table says 400, C-9) |
| empty (whitespace-only) body | `400 Bad Request` |
| expression fails to parse | `400` |
| expression fails static evaluation (unknown identifier, type error detectable before running) | `400` |
| target document not found | `404` |
| `self` referenced on a directory target | `416 Range Not Satisfiable` |
| `Range: entries=` on a non-list result | `416` |
| expression raised a runtime error (`throw`, a throwing conversion such as `.Integer()` on bad input, …) | `500` |
| evaluation budget exhausted | `503` (cross-area: budgets) |
- **Evidence:** documented (table) · inferred (400-vs-500 split, 503) · **Confidence:** high (rows as documented) / low (split)
- **Source:** D-Q §Sessel mode · Errors.
- **Edge cases:** "fails to parse or evaluate" (400) vs "raised a runtime error" (500) overlap; decision: anything detected before evaluation or a Sessel `TypeError` raised by the language core is 400; an explicit `throw` or an error from a user-defined function/method is 500 (low; P-21). Directory targets: a prefix with no documents → `404` (inferred).

> **Superseded by live observation (2026-09-28).** All bodies are read
> vocabulary (`http://pagelove.org/Error`).
>
> | Condition | Answer |
> |---|---|
> | `Content-Type` missing or other | `400` "QUERY method requires Content-Type: text/sessel" (C-9 reversed) |
> | Empty body | `422` "Invalid path: QUERY method requires a body containing the sessel expression" |
> | Parse error | `400` "Sessel compilation error: …" |
> | `throw` and other runtime errors | `400` "Sessel evaluation error: … (at bytes a..b)", not `500` (P-21 settled for throw) |
> | Directory target | Its index (R-PROTO-71): `404` when missing, never `416` |
>
> Cases: `protocol.query-sessel.400-body-errors`, `.500-runtime-error`,
> `.415-unsupported-type`, `.directory-self-unbound`.

## 8. The `entries` range unit

### R-PROTO-80 — Request syntax
`Range: entries=<start>-<end>` with `<start>`, `<end>` non-negative decimal
integers, `start <= end`. Open-ended (`entries=5-`) and suffix (`entries=-5`)
forms are not documented; pagelike SHOULD accept `entries=<start>-` (to the end)
and reject suffix forms as unsatisfiable (inferred, low).
- **Evidence:** documented (bounded form) · **Confidence:** high (bounded) / low (others)
- **Source:** D-Q §Paginating a list result; D-AR.

### R-PROTO-81 — Response `Content-Range`
`Content-Range: entries <start>-<end>/<total>` (space after the unit), where
`<start>-<end>` is the slice actually returned and `<total>` the length of the
full result.
- **Evidence:** documented · **Confidence:** high (format)

> **Superseded by live observation (2026-09-29).** PageLove ends the value
> with a semicolon: `Content-Range: entries 0-1/5;`. Zero-based inclusive
> indexes are confirmed (R-PROTO-82, P-19). Cases:
> `protocol.query-sessel.entries-pagination`, `.entries-format`.

### R-PROTO-82 — Semantics (inferred)
Indices are **zero-based and inclusive** (HTTP byte-range analogy): on a list of
length N, `entries=a-b` returns items `a..min(b, N-1)` and reports
`entries a-min(b,N-1)/N`, status `206`. `a >= N` (including N = 0) →
`416 Range Not Satisfiable` with `Content-Range: entries */N`. `a > b` → the
`Range` is invalid and is ignored (RFC 9110 treatment of invalid ranges) →
full result `200`.
- **Evidence:** inferred · **Confidence:** low (P-19)

### R-PROTO-83 — Applicability
- QUERY Sessel mode with a list result (documented). The body is the sliced
  JSON array (R-PROTO-73).
- A non-list result with `entries=` → `416` (documented).
- GET all-matches reads (multipart / JSON-LD answers to a selector) are the
  other "list" the docs allude to ("works on `Range` requests"; D-RULE
  "a paginated read asks for a range of a selector's matches"), but no request
  syntax combining a selector and an entries range is documented anywhere
  (C-22). pagelike MUST NOT invent one before P-20 settles it; until then a GET
  with `Range: entries=…` is an unrecognised-unit request (R-PROTO-5) unless
  the reading-writing area defines it.
- **Evidence:** documented · **Confidence:** high (QUERY) / low (GET)

### R-PROTO-84 — Authorization over the whole match set (cross-area)
For all-matches reads, authorization is decided over the selector's whole match
set, so every page of a paginated read is refused if any match is denied
(D-RULE). For Sessel QUERY the grant is R-PROTO-74.

### R-PROTO-85 — Not advertised (R-PROTO-32).

## 9. MOVE (edge plane)

### R-PROTO-90 — Purpose and success
`MOVE` relocates an element within one document (element move) or relocates a
whole document to another path (whole-document move) atomically: readers never
observe the element missing or duplicated. Success is `204 No Content` with an
empty body.
- **Evidence:** documented · **Confidence:** high
- **Source:** D-MOVE intro, §When to reach for it.
- **Edge cases:** response headers beyond the status are not documented. pagelike SHOULD send the document's new `ETag` (element move: the request document; whole-document move: the destination) and nothing else of note (inferred, low; P-17). beta-js and KANBAN treat any 2xx as success (BJ-COMP:792, KANBAN:16).

### R-PROTO-91 — Request headers
| Header | Element move | Whole-document move |
|---|---|---|
| `Range: selector=<source>` | required | absent |
| `Destination: <path-or-URL>` | required; must identify the request path | required; may name another path |
| `Destination-Range: selector=<anchor>; placement=<p>` | required | absent |
| `If-Match` | optional (R-PROTO-100) | optional |
`Destination` MAY be an absolute path (`/board.html`, as KANBAN sends) or an
absolute URL (`https://host/board.html?x=1`, as beta-js sends
`location.href` minus the fragment). pagelike MUST reduce an absolute URL to its
path (percent-decoded, query and fragment ignored) and compare paths; an
absolute URL whose authority is not the request's host → `502 Bad Gateway`
(RFC 4918 §9.9.4; inferred, low).
- **Evidence:** documented (table) · client-source (absolute URL: BJ-COMP:771-779; path: KANBAN:10-12) · **Confidence:** high (table) / medium (URL handling)
- **Source:** D-MOVE §Headers.

> **Kept despite live observation (2026-09-28).** PageLove answers a
> same-document `Destination` that carries a query string with `501
> MoveCrossResource`. That breaks beta-js on any page URL with a query, so
> pagelike keeps ignoring the query (keep-standard);
> `protocol.move.destination-absolute-url.live` measures the divergence.

### R-PROTO-92 — Classification
- `Range` and `Destination-Range` both absent → whole-document move (R-PROTO-98).
- Both present → element move.
- Exactly one present → `422` (incomplete element move).
- `Destination-Range` without a parseable `placement` → `422`.
- Whole-document move without `Destination` → `422`; element move without
  `Destination` → `422` (inferred; the header is "required").
- `Range` using a unit other than `selector` on a MOVE → `422` (inferred).
- **Evidence:** documented · **Confidence:** high
- **Source:** D-MOVE §Headers, §Error cases.

### R-PROTO-93 — Placement
`placement=` is one of `append`, `prepend`, `before`, `after`, matched
case-insensitively; any other value → `422` (inferred). Insertion site relative
to the destination anchor: `append` = last child, `prepend` = first child,
`before` = previous sibling, `after` = next sibling.
- **Evidence:** documented · **Confidence:** high
- **Source:** D-MOVE §Placement.
- **Edge cases:** `before`/`after` on a parentless anchor (the root element): POST answers `400` for the same situation (D-RW §POST Placement); MOVE's table says "illegal move → 422". Decision: `422` for MOVE (low; P-18).

### R-PROTO-94 — Element move semantics
1. Resolve the source: the **first** element in document order matching the
   `Range` selector (writes act on the first match; D-RULE).
2. Resolve the anchor: the first element matching the `Destination-Range`
   selector (inferred, same rule).
3. Illegal → `422`: anchor is the source or inside the source's subtree for
   `append`/`prepend`; anchor inside the source's subtree for `before`/`after`;
   source is the root element; anchor parentless for `before`/`after`.
   Anchor **is** the source with `before`/`after` → no-op, `204` (inferred,
   low).
4. Detach the source node (with its subtree, attributes, `id`) and insert that
   same node at the placement. Whitespace text nodes adjacent to the source's old
   position stay where they were; no whitespace is added at the new position
   (inferred; cases compare whitespace-insensitively).
5. Schema cardinality is checked at the source's old parent (after removal) and
   at the new parent (after insertion); a violation at either → `422` and
   nothing is written (cross-area: modeling).
6. Commit the document once (one version bump, one ETag change).
- **Evidence:** documented (atomicity, cardinality, illegal-descendant) · inferred (first-match anchor, whitespace, no-op) · **Confidence:** high / low
- **Source:** D-MOVE §When to reach for it, §Error cases.

### R-PROTO-95 — No cross-document element moves
An element move whose `Destination` path differs from the request path MUST be
rejected with `501 Not Implemented` carrying an error document of kind
`MoveCrossResource`, before anything is written. Only whole-document moves may
change paths.
- **Evidence:** documented · **Confidence:** high
- **Source:** D-MOVE §Error cases (paragraph after the table).
- **Contradiction:** the reading-and-writing index says MOVE relocates "within or across documents" (C-12).

### R-PROTO-96 — Authorization: default-deny and the three checks
MOVE is always default-deny: the host default-GET mode never grants it. An
element move requires **all** of:
| # | Method | Target evaluated | Granted by |
|---|---|---|---|
| 1 | `MOVE` | the request path, document level (no selector) | a document-level MOVE rule |
| 2 | `DELETE` | the source element | a DELETE rule whose selector (if any) matches the source, or a document-level DELETE rule |
| 3 | `POST` | the element whose child list changes: the anchor for `append`/`prepend`, the anchor's parent for `before`/`after` | a POST rule matching that element (or document-level POST) |
All three are evaluated against the request path. Each check uses the
permissions area's conflict resolution. Failure of any check → `401` if the
request is unauthenticated, `403` if authenticated.
- **Fail closed:** if a selector in check 2 or 3 matches no element, or the
  document backing the check cannot be read, the check fails. How this
  combines with the 416/404 absence statuses: see R-PROTO-99.
- **Evidence:** documented · **Confidence:** high
- **Source:** D-MOVE §Authorization, §The three checks, §Denials.
- **Edge cases:** "Granting MOVE alone is not sufficient"; a single POST rule on a container (`.lane`) authorizes append/prepend into it and before/after any of its children; narrowing the DELETE rule (`#lane-todo > .card`) restricts sources; narrowing the POST rule (`#lane-done`) restricts destinations (all documented, cases in `move-authz.yaml`).

### R-PROTO-97 — MOVE rules must not carry a selector
Rule normalization (applies to every evaluation, including OPTIONS):
- An `Allow` rule whose method list contains `MOVE` and whose `selector` is
  non-empty after trimming is **discarded entirely** (all of its methods, not
  just MOVE).
- A `Deny` rule whose method list contains `MOVE` and whose selector is
  non-empty is **kept with the selector treated as absent** — for every method
  it lists (a `Deny` on `MOVE, DELETE` with `.card` refuses both for the whole
  document).
- An empty or whitespace-only selector is fine.
- **Evidence:** documented · **Confidence:** high (MOVE-only rules) / medium (the "all methods" reading for Allow — the docs say the rule "is discarded entirely")
- **Source:** D-MOVE §A MOVE rule must not carry a selector; D-RULE §`selector`.

### R-PROTO-98 — Whole-document MOVE
- Authorized by a MOVE rule on **both** the request path and the `Destination`
  path; a Deny matching either refuses the request. Checks 2 and 3 do not apply
  (no DELETE/POST rules needed).
- Source document missing → `404` (inferred).
- Destination path without a document → the document is created there, the
  source path no longer exists, `204`.
- Destination path with an existing document: not documented. pagelike follows
  RFC 4918 `Overwrite`: absent or `T` → replace, `204`; `F` → `412`
  (inferred, low; P-16).
- Destination equal to the source path → `403` (RFC 4918 §9.9.4; inferred, low).
- Paths are literal: parameterized routes are not resolved for whole-document
  writes, including MOVE (D-ROUTE).
- Reserved namespace: request path or Destination under `/.pagelove/` → `403`
  (R-PROTO-104).
- Stored bytes, content type and blob-ness move unchanged; the destination gets
  a new version/ETag (inferred).
- **Evidence:** documented (authorization, creation) · inferred (rest) · **Confidence:** high / low
- **Source:** D-MOVE §Whole-document MOVE, §Error cases; D-ROUTE §Resolution.

> **Kept despite live observation (2026-09-28).** PageLove answers a missing
> source with `500` and an `Internal` problems item. pagelike keeps `404`
> with the write path's `NotFound` problems item (keep-standard);
> `protocol.move.whole-document-missing-source.live` measures the
> divergence. A denial names the request path. The destination's event
> carries the whole document as its body (R-SSE-17 as reconciled).

### R-PROTO-99 — Error table and evaluation order
| Condition | Status |
|---|---|
| request path or Destination under `/.pagelove/` | `403` |
| only one of `Range`/`Destination-Range`; missing/invalid `placement`; missing `Destination` | `422` |
| element move whose `Destination` names another document | `501` + `MoveCrossResource` |
| invalid `Range` or `Destination-Range` selector syntax | `422` |
| request document missing (element move) / source missing (whole-document) | `404` |
| any authorization check denies | `401` unauthenticated / `403` authenticated |
| `If-Match` does not match (R-PROTO-100) | `412` |
| source selector matches nothing | `416` |
| destination selector matches nothing | `404` |
| illegal move (into own subtree, etc.) | `422` |
| schema cardinality violated at source or destination | `422` |
| selector ranges on a blob (non-HTML/XML) document | `422` (D-UP) |
Recommended evaluation order (inferred; P-15): the table order, with the
"absence vs denial" rule of D-RW §PUT/§DELETE applied to the two no-match rows:
a non-matching source/anchor is reported as `416`/`404` only when the actor may
`GET` the document; otherwise the fail-closed denial (`401`/`403`) is reported,
so absence is never revealed to an actor who cannot read the page.
- **Evidence:** documented (statuses) · inferred (order, absence rule) · **Confidence:** high / low
- **Source:** D-MOVE §Error cases, §Denials; D-RW §PUT "Absence and denial are distinct".

> **Superseded by live observation (2026-09-28), in part (P-15 settled).**
>
> | Condition | Answer |
> |---|---|
> | Missing headers | `422`, `MoveMissingHeaders` problems item |
> | Cross-document `Destination` | `501`, `MoveCrossResource` problems item |
> | Invalid source selector | Matches nothing: `416` selector-no-match |
> | Invalid destination selector | `422` `InvalidPath`, "Invalid destination selector: invalid selector '<sel>': <err>" |
> | Absent source or anchor | `416`/`404` only when the actor may read the page **and** holds DELETE (source) or POST (anchor) at document level; otherwise refused `401`/`403`, because the element check cannot run on an absent element (`authz.move.source-absent-readable-416`) |
> | Absent source, actor cannot read the page | PageLove: `416`. pagelike keeps the denial (keep-documented-security; `protocol.move-authz.fail-closed-when-unreadable.live`) |
>
> A `204` carries `ETag`, `Last-Modified` and `Vary: Host, Range`.

### R-PROTO-100 — Conditional MOVE (`If-Match`)
If `If-Match` is present and the document changed since that ETag, the move is
rejected with `412 Precondition Failed` (re-read and retry) instead of being
applied to the new state; `If-Match: *` passes whenever the document exists.
Compatibility decision: an `If-Match` entity-tag matches if it equals **either**
the current document ETag (documented) **or** the current element ETag of the
source element (what beta-js sends: it copies the ETag obtained from
`HEAD` + `Range: selector=#id`, BJ-PRIM:474-501, into `If-Match` on MOVE,
BJ-COMP:781-783). pagelike SHOULD include the current document `ETag` on the
412 response (as PUT/DELETE do, D-RW).
- **Evidence:** documented (412) · client-source (element ETag) · **Confidence:** high (412 on stale doc ETag) / low (element-ETag acceptance; P-13)
- **Source:** D-MOVE §Concurrency; BJ-COMP:764-803.
- **Contradiction:** KANBAN:29-40 reports that on the build it was tested against "conditional writes reject every request here — even If-Match: *" (C-13). Decision: docs.

> **Superseded by live observation (2026-09-28) (C-14, P-13 settled).** For
> an element move, only the **source element's** current tag (or `*`)
> satisfies `If-Match`. The document's tag is `412`, and the 412 carries the
> source's tag. Case: `protocol.move.if-match-current`.

### R-PROTO-101 — Concurrency
Concurrent MOVEs against the same document are serialized (pagelike's per-site
write mutex) and each is applied to the latest committed version, re-checking
source/anchor existence and cardinality; both succeed if both remain valid.
A move whose source vanished meanwhile fails with that condition's own status
(`416`), never silently.
- **Evidence:** documented · **Confidence:** high
- **Source:** D-MOVE §Concurrency.
- **Contradiction:** KANBAN:29-40 measured "Two concurrent MOVEs … lose one of them roughly three times in four … the server answers 204 but the element never leaves its old parent" on an older build (C-13). Decision: docs (both land).

### R-PROTO-102 — Schema cardinality (cross-area: modeling)
See R-PROTO-94 step 5.

### R-PROTO-103 — Mutation event (cross-area: SSE)
A successful element move emits exactly one `mutation` event on the document's
stream with `method` = `MOVE`, `selector` = the `Range` selector verbatim,
`destination` = the `Destination-Range` selector verbatim (without the unit and
the placement parameter), `placement` lowercased. beta-js's SSE client applies
it with `document.querySelector(destination)` and the placement
(BJ-SSE:113-135, 220-239). Owned by the SSE spec (R-SSE requirements on MOVE,
`docs/spec/sse.md`); whole-document MOVE events are an open question (P-16).
- **Evidence:** client-source · documented (the `placement` row names MOVE) · **Confidence:** medium

> **Refined by live observation (2026-09-28).** The event's `etag` is the
> document's new tag, unquoted, and `body` is the moved element as served
> (R-SSE-13/14 as reconciled). A whole-document MOVE announces DELETE on the
> source and PUT on the destination, with the whole document as the body.

### R-PROTO-104 — Reserved namespace
Any MOVE targeting `/.pagelove/…` (request path or Destination) → `403`.
- **Evidence:** documented (D-RW §PUT "Reserved namespace" lists MOVE) · **Confidence:** high (request path) / medium (Destination)

### R-PROTO-105 — Never `405` for MOVE on a document
beta-js treats `405` as "server does not support MOVE" and permanently falls
back to POST+DELETE / PUT for the rest of the page's life (BJ-COMP:757-800).
pagelike MUST NOT answer `405` to a MOVE on the edge plane; unsupported MOVE
shapes use the statuses above (and pagelike's general policy of `501` for
unimplemented operations).
- **Evidence:** client-source · **Confidence:** high

### R-PROTO-106 — Echo suppression token (cross-area: SSE)
MOVE is a mutating request: a `Pagelove-Connection` header on it SHOULD narrow
echo suppression exactly as for PUT/POST/DELETE (D-SSE lists PUT/POST/DELETE/
PATCH only; inferred for MOVE).

## 10. WebDAV authoring plane

### R-PROTO-110 — Endpoint
Each site has its own WebDAV mount representing the **live** site (not a
staging copy): a write over WebDAV is immediately what the edge serves. pagelike
serves it at `dav-<site>.<domain>`; PageLove's is advertised by the console as
`webdav-url` and conventionally is the public host prefixed with `dav-`
(`https://dav-xxxx.onpagelove.com/`).
- **Evidence:** documented (per-site mount, live) · client-source (BJ-SYNC:9-10, BJ-VER:30-35 derive the public host by removing `dav-`) · demo-source (PDEV says never derive; use the advertised URL) · **Confidence:** high
- **Source:** D-DAV §When to reach for it, §Live site.

### R-PROTO-111 — Authentication; rules bypassed
- Every dav request MUST carry the site's authoring key:
  `Authorization: Bearer <key>` (what all official tooling sends) or
  `Authorization: Basic <base64(user:key)>` (the form in D-Q's examples; the
  user part is ignored — inferred). Missing/invalid → `401` with
  `WWW-Authenticate: Bearer` and `WWW-Authenticate: Basic realm="…"` (inferred)
  and an error document; PageLove's message reads "Bearer token rejected"
  (PDEV:180).
- AuthorizationRules do not apply to dav requests ("WebDAV bypasses rules,
  letting you write the very rules that govern subsequent access").
- **Evidence:** client-source (BJ-SYNC:20) · demo-source (DEMO:146; SHOP:51; PDEV:29,180) · documented (Basic in D-Q) · **Confidence:** high (Bearer, bypass) / medium (Basic)

> **Refined by live observation (2026-09-29).** The 401 carries
> `WWW-Authenticate: Basic realm="WebDAV"` (no Bearer challenge),
> `DAV: 1, 2` and `MS-Author-Via: DAV`, with a short
> `https://pagelove.org/Error/Internal` article: `status` 401, message
> "Authentication required" for a missing key. pagelike uses "Bearer token
> rejected" (PDEV:180) for a wrong key, which was not probed.
> The first live run's 207/200 answers were a runner artifact: the key was
> sent on anonymous requests. Case: `protocol.webdav.unauthenticated-401`.

### R-PROTO-112 — Writes and the shared pipeline
- A dav write of an HTML/XML document goes through the shared write pipeline
  (schema/shape/uniqueness validation → `422`, with the pipeline's own error
  document embedded as `detail`, R-PROTO-120) **except** that it is never
  transition-validated and never fires TransitionHandlers (D-TC, D-SM "the
  repair path").
  live 2026-09-29 (modeling, adopt-live): a dav `PUT` is stored with no schema
  or shape check (docs/compat/decisions-2026-09-29/modeling.md, R-MOD-13).
- Rules documents edited over WebDAV take effect "within 60 seconds" on
  PageLove; pagelike applies them on commit (a permitted special case of
  "within 60 s"). Live harness runs that write rules over WebDAV must allow for
  the delay.
- Concurrency: the author coordinates; a write that conflicts with a concurrent
  change → `409`; `If-Match` / `If-None-Match` preconditions → `412`.
- **Evidence:** documented · **Confidence:** high
- **Source:** D-DAV §Live site, §When something goes wrong; D-TC; D-SM.

### R-PROTO-113 — GET returns the stored bytes

> **Superseded live 2026-09-29 (LO-15, docs/compat/decisions-2026-09-29/serialization.md):**
> every HTML write, a dav PUT included, stores the document serialized in
> PageLove's form, and the PUT echoes it; a dav GET returns those stored bytes.
> A read-back is byte-identical only for sources already in that form.
A dav `GET` returns the stored representation byte-for-byte (for HTML: the
markup exactly as last written over WebDAV, or as last serialized after an edge
mutation) with the stored `Content-Type` and the **content ETag**. It is never
composed and response processors never run on it ("WebDAV is the storage API
and does not apply them"). A read-back after a dav PUT is byte-identical to the
uploaded file.
- **Evidence:** client-source (BJ-VER:10-11) · demo-source (SHOP:165,249 `cmp -s` read-back) · **Confidence:** high
- **Edge cases:** HTML bytes are not re-serialized on a dav PUT (pagelike's store must keep the original bytes until an edge mutation rewrites the document). Cross-area: store/reading-writing.

> **Kept despite live observation (2026-09-28), deferred.** PageLove stores
> WebDAV-uploaded HTML re-serialized and serves it back that way:
> - `<!DOCTYPE html>`, lower-cased and closed tags, quoted attributes, bare
>   empty attributes;
> - the PUT echoes the stored form.
>
> pagelike keeps the bytes (keep-standard, deferred: it needs a serializer
> with PageLove's output, and a decision for Liquid template sources).
> `protocol.webdav.get-is-byte-exact.live` measures the divergence. See
> [LO-9](../compat/live-observations.md).

### R-PROTO-114 — PUT
- New path → `201 Created`; existing path → `200 OK` (`204` also acceptable to
  clients). Missing parent collections are created implicitly ("no directory
  creation is needed").
- `If-None-Match: *` on an existing path → `412` (create-only); `If-Match:
  <content ETag>` (the PROPFIND `getetag`) that no longer matches → `412`.
- Content-type resolution follows D-UP (`.html`/`.htm` always `text/html`,
  else request `Content-Type`, else extension, else
  `application/octet-stream`; cross-area).
- Paths the server will not accept → `400`; request exhausted its allowance →
  `507`; storage briefly unavailable → `503`.
- **Evidence:** demo-source (DEMO:143; PDEV recipes:302-316; SHOP:148-175; BJ-SYNC:129-137) · documented (status meanings) · **Confidence:** high (201/200/412) / medium (parents)

> **Refined by live observation (2026-09-28).** A successful PUT echoes the
> stored body with its `Content-Type` and `ETag`:
> - no `Last-Modified`;
> - no `Vary`;
> - `Accept-Ranges: bytes` on the 200 replace.

### R-PROTO-115 — MKCOL
- Creates a collection → `201`.
- Existing collection → `409 Conflict` with an error document whose type is
  `https://dombase.pagelove.team/ns/error/DirectoryAlreadyExists` (not RFC
  4918's `405`).
- Missing parent: not documented (BJ-SYNC:106-107 notes 409 "also means parent
  missing"); pagelike SHOULD create parents implicitly as PUT does and answer
  `201` (inferred, low; P-24).
- **Evidence:** demo-source (SHOP:95-112) · client-source (BJ-SYNC:98-113) · **Confidence:** high (409 + kind)

> **Superseded by live observation (2026-09-28/29) (C-19 reversed, P-24
> settled).**
>
> | Situation | Answer |
> |---|---|
> | Create | `201`, `ETag` of 64 zeros |
> | Existing collection | **`405`** with `Allow`, plus an `https://pagelove.org/Error` article (kind `Internal`) whose detail names `DirectoryAlreadyExists` |
> | Missing parent | `409`, `ParentDirectoryMissing` (no implicit creation) |
>
> Case: `protocol.webdav.mkcol`. The deploy scripts accept 405 and 409.

### R-PROTO-116 — DELETE
Existing file/collection → `2xx` (`204` recommended); absent → `404`.
Deleting a collection deletes its subtree (RFC 4918; inferred).
- **Evidence:** client-source (BJ-SYNC:170-174) · **Confidence:** high (404)

> **Superseded by live observation (2026-09-28).** A DELETE of an absent
> path is `204`, with the ETag of empty content, as on the public plane.
> PROPFIND and GET of an absent path stay `404`, as a short `Error/NotFound`
> article: "Not found: <p>" or "Collection not found: <p>". Case:
> `protocol.webdav.missing-is-404`.

### R-PROTO-117 — PROPFIND basics
- `207 Multi-Status`, `Content-Type: application/xml; charset=utf-8`, a
  `DAV:multistatus` body (namespace prefix `D:` in PageLove's wording) with one
  `D:response` for the target and, at `Depth: 1`, one per direct child. Each
  entry carries at least `D:href`, `D:getetag`, `D:getcontenttype`,
  `D:getlastmodified`, `D:resourcetype` (`D:collection` for collections),
  `D:getcontentlength` for files (standard RFC 4918 live properties; inferred
  beyond `getetag`, type and last-modified, which the docs name).
- `Depth` absent → **1** (not RFC 4918's infinity). `Depth: 0` → target only.
  `Depth: infinity` → not documented; pagelike answers `403` with the
  `DAV:propfind-finite-depth` precondition element (RFC 4918 §9.1) (inferred,
  low; P-23).
- Empty request body = `allprop`.
- Target missing → `404`.
- **Evidence:** documented (depth default, getetag, properties named) · client-source/demo-source (207 on success, 404 on missing: PDEV recipes:264-285) · **Confidence:** high (status, default depth) / medium (property list)
- **Source:** D-DAV §Directory listings and revalidation.
- **Implementation note:** `golang.org/x/net/webdav` defaults a missing `Depth` to infinity and emits no ETag/`Vary`/304 handling for PROPFIND; pagelike must wrap it.

### R-PROTO-118 — PROPFIND revalidation for collections
- Every PROPFIND response for a collection carries `ETag: <tag>` where the tag
  identifies **the answer** (collection + depth): the `Depth: 0` and `Depth: 1`
  answers have different tags.
- The same tag appears as the `D:getetag` of the collection's own entry in that
  response ("the two always agree").
- `Vary: Depth` on every PROPFIND response.
- `If-None-Match: <tag>` matching the current tag for **the same depth** →
  `304 Not Modified`, empty body (with `ETag` and `Vary`). A tag from the other
  depth never produces a 304 ("a listing is never answered with 'nothing
  changed' on the strength of a stat").
- The tag changes whenever a child is added, removed, or edited. Changes outside
  the collection MUST NOT change it (inferred from "if nothing in the directory
  has changed"); changes deeper in the subtree SHOULD change it (conservative,
  inferred).
- Recommended construction (inferred): depth-0 tag =
  `"d0-" + hash(path, subtreeGeneration)`; depth-1 tag =
  `"d1-" + hash(path, sorted (childName, isCollection, childContentETag or child depth-0 tag, contentType, lastModified))`.
- **Evidence:** documented · **Confidence:** high (behaviors) / low (construction)
- **Source:** D-DAV §Directory listings and revalidation.

### R-PROTO-119 — PROPFIND revalidation for files; two different tags
- A file PROPFIND carries `ETag: <properties tag>` identifying the answer (the
  file's properties, including content type and last-modified).
  `If-None-Match` with it → `304` while nothing about the file changed.
- The file's `D:getetag` in the body is the **content ETag** — the one to send
  as `If-Match` on a PUT (save only if nobody else has) and the one a dav GET
  returns in its `ETag` header.
- The two are different values; the properties tag MUST NOT satisfy `If-Match`
  on a write (inferred from "two different tags for two different jobs").
- In a collection's `Depth: 1` listing, each child file's `D:getetag` is its
  content ETag (inferred).
- **Evidence:** documented · demo-source (PDEV:164-167, recipes:269-316 read the body getetag and send it as `If-Match`) · **Confidence:** high (semantics) / medium (dav GET ETag = getetag) / low (properties tag rejected by If-Match)
- **Source:** D-DAV §Directory listings and revalidation (last two paragraphs).

> **Refined by live observation (2026-09-28), R-PROTO-117..119.**
> - Collection answers are tagged `"sha256:<64 hex>"` and carry `Vary: Depth`.
> - File answers are tagged `"propfind:sha256:<64 hex>"`, with no `Vary`.
> - The body follows PageLove's layout (`D:` prefix; the root entry has no
>   displayname or getlastmodified).
>
> The behaviours are unchanged, and the webdav cases passed live. See
> [LO-12](../compat/live-observations.md).

### R-PROTO-120 — Error documents (dav plane)
Every dav error response (4xx/5xx except 304) carries `Content-Type:
text/html; charset=utf-8` and an HTML document containing:
```html
<article itemscope itemtype="https://pagelove.org/Error">
  <meta itemprop="status" content="422">
  <meta itemprop="kind" content="UntranslatableWrite">
  <div itemprop="type" itemscope
       itemtype="https://dombase.pagelove.team/ns/error/UntranslatableWrite"></div>
  <p itemprop="message">…human-readable…</p>
  <!-- when the shared pipeline refused the write: -->
  <div itemprop="detail">…the pipeline's own error document, unchanged…</div>
</article>
```
- `kind` names the failure; the nested `type` item repeats it as
  `https://dombase.pagelove.team/ns/error/<Kind>`. Kind names are shared with
  the HTTP (edge) interface; the dav plane never invents its own.
- Known kinds: `UntranslatableWrite` (422, documented example),
  `DirectoryAlreadyExists` (409 on MKCOL, demo-source), `MoveCrossResource`
  (501 on edge MOVE, documented). Others are not public; pagelike SHOULD reuse
  its edge kind names.
- **Evidence:** documented · demo-source · **Confidence:** high (shape) / medium (detail element form)
- **Source:** D-DAV §When something goes wrong; SHOP:98-101.

> **Superseded by live observation (2026-09-28), in part.** PageLove uses
> three authoring-plane shapes, all `text/html; charset=utf-8`:
> 1. a short `<article itemscope itemtype="https://pagelove.org/Error/NotFound">`
>    with `status` meta and `message` p, for missing paths;
> 2. the same short shape typed `Error/Internal`, for refused QUERYs and
>    missing or rejected keys;
> 3. the documented `https://pagelove.org/Error` article, of kind `Conflict`
>    (MKCOL 409) or `Internal` (405, 412, 422…), whose `detail` holds the
>    public plane's own document: a problems item, a precondition message or
>    a constraint violation.
>
> `DirectoryAlreadyExists` appears only inside the 405's detail.

### R-PROTO-121 — Status meanings (dav plane)
| Status | Meaning (client action) |
|---|---|
| `404` | the addressed file is not there ("this file is gone") |
| `500` | server failure, **including failure to load the site's schema** — never reported as 404 ("try again later") |
| `409` | write conflicts with a concurrent change; MKCOL on existing collection |
| `400` | path the server will not accept |
| `401` | missing/invalid authoring key |
| `412` | `If-Match` / `If-None-Match` precondition failed |
| `422` | content does not satisfy the site's schema or constraints |
| `503` | storage layer briefly unavailable; retry |
| `507` | request exhausted its allowance |
- **Evidence:** documented · demo-source (PDEV:179-192) · **Confidence:** high
- **Source:** D-DAV §When something goes wrong.

### R-PROTO-122 — WebDAV MOVE/COPY (authoring plane)
Not documented by PageLove. pagelike SHOULD implement RFC 4918 MOVE/COPY of
files and collections on the dav plane (`Destination` absolute URI on the same
dav host, `Overwrite`, `201` new / `204` replaced) without AuthorizationRules
and without transition validation (inferred). This is unrelated to the edge
MOVE of §9.

### R-PROTO-123 — LOCK
Not documented. Desktop clients (macOS Finder) need class-2 locking to mount
read-write; pagelike MAY offer advisory in-memory locks (x/net/webdav
`NewMemLS`) that do not block edge writes ("The author is responsible for
coordinating concurrent changes") (inferred).

### R-PROTO-124 — Events from dav writes
Whether dav writes produce SSE `mutation`/`reset` events is not documented
(cross-area: SSE; P-25). Transition handlers never fire (documented).

> **Settled by live observation (2026-09-28) (P-25).** A dav write emits
> **no** event to public-plane subscribers (R-SSE-22 as reconciled). Case:
> `sse.scope.webdav-write-emits`.

## 11. Error documents across planes (summary; cross-area owner: errors/permissions)

### R-PROTO-130 — Error body vocabularies in use
PageLove uses several Microdata vocabularies for error bodies; pagelike SHOULD
reproduce the one used for each condition and harness cases assert only status
codes and documented kind names:
| Where | itemtype | properties | Evidence |
|---|---|---|---|
| dav errors; 413; recommended for MOVE/QUERY errors | `https://pagelove.org/Error` | `status`, `kind`, `type`, `message`, `detail` (413: `status`, `message`) | documented (D-DAV, D-RW §Request body size limit) |
| 401 on the edge (incl. QUERY sessel) | `https://pagelove.org/1.0/Error` on `<body>` | `message`, `resource`; link to `/-pagelove/oidc/login` | documented (D-RULE §Testable examples) · live-observed (LIVE:query-sessel) |
| 416 on the edge (GET) | `http://pagelove.org/Error` (http!) on `<body>` | `name`, `statusCode`, `description` | live-observed (LIVE:missing-fragment) |
| 422 constraint violations | `https://pagelove.org/ConstraintViolation` | `name`, `statusCode`, `description`, `violations` | documented (modeling area) |
- **Confidence:** medium. Contradiction C-18.

### R-PROTO-131 — Protocol-area error kinds pagelike MUST emit
`MoveCrossResource` (501, edge MOVE), `DirectoryAlreadyExists` (409, dav
MKCOL). For other protocol errors pagelike chooses stable kind names (e.g.
`UnsupportedQueryType` 415, `InvalidSelector` 422, `IncompleteMove` 422,
`IllegalMove` 422, `NotAcceptable` 406) — these are pagelike-only names and
MUST NOT be asserted by cases against live.

### R-PROTO-132 — Content-Type of error bodies
`text/html; charset=utf-8` (live 401) — pagelike uses it for all HTML error
documents.

## 12. Status code index (this area)

| Status | Where |
|---|---|
| 200 | flat OPTIONS; dav css QUERY (incl. no matches); Sessel QUERY non-element result; edge css multipart branch; dav PUT replace |
| 201 | dav PUT create; dav MKCOL |
| 204 | multipart OPTIONS fallback; MOVE success; dav DELETE |
| 206 | edge css QUERY default branch; Sessel element result (C-7); `entries` slice |
| 207 | multipart OPTIONS with selector-scoped rules; PROPFIND |
| 304 | QUERY `If-None-Match` (dav and multipart edge); PROPFIND `If-None-Match` same depth |
| 400 | Sessel parse/eval/empty body; dav unacceptable path |
| 401 / 403 | edge denials (anonymous / authenticated): OPTIONS never; QUERY; MOVE; dav 401 bad key |
| 403 | reserved namespace MOVE; whole-document MOVE onto itself (inferred); PROPFIND Depth infinity (inferred) |
| 404 | MOVE destination anchor absent; MOVE doc missing; QUERY target missing; dav missing file |
| 406 | css QUERY with `Accept` excluding multipart/mixed (dav, and edge multipart branch) |
| 409 | dav conflicts; MKCOL existing |
| 412 | MOVE `If-Match`; dav PUT preconditions; whole-document MOVE `Overwrite: F` (inferred) |
| 415 | QUERY missing/unsupported Content-Type (+ `Accept-Query`) |
| 416 | MOVE source absent; Sessel `self` on directory; `entries` on non-list / unsatisfiable; edge css default branch no match |
| 422 | incomplete/invalid MOVE, illegal move, cardinality; css QUERY empty/invalid selector; selector ops on blobs; dav schema/constraint violation |
| 500 | Sessel runtime error; dav schema-load failure |
| 501 | cross-document element MOVE (`MoveCrossResource`) |
| 502 | MOVE `Destination` on another host (inferred) |
| 503 | dav storage unavailable; evaluation budget |
| 507 | dav allowance exhausted |

## 13. Cross-area dependencies

- **permissions**: actor matching, glob `resource` matching, conflict
  resolution (specificity tiers, deny-wins), default-GET mode, templated fields,
  multi-match authorization, 401/403 selection and bodies — used by OPTIONS
  (R-PROTO-12), edge QUERY (R-PROTO-62/74), MOVE (R-PROTO-96/97/99). The
  MOVE-selector normalization (R-PROTO-97) must be implemented once in
  `internal/authz` and applied everywhere.
- **reading-writing**: `Range: selector=` parsing and first-match rule,
  `Content-Range` spelling (R-PROTO-3 must agree), element/document ETags
  (QUERY parts, MOVE `If-Match`, HEAD+Range ETag), directory index resolution
  and 301 (edge QUERY target), absence-vs-denial rule, reserved namespace,
  selector ops on blobs → 422, body size caps, `bytes` ranges.
- **selector**: CSS dialect and PageLove extensions for QUERY/MOVE; stable
  "document-rooted" selector generation for QUERY parts.
- **composing**: edge css QUERY runs on the composed page; parameterized routes
  are literal for whole-document MOVE.
- **sessel**: language, default scope, `self`/`prior`, value model →
  `application/sessel+json`, error classes (400 vs 500), budgets.
- **sse**: MOVE mutation events (`destination`, `placement`), echo
  suppression token on MOVE, events for whole-document MOVE and dav writes.
- **modeling**: cardinality checks for MOVE; schema validation on dav writes
  (`422` with `detail`); `500` when the schema cannot be loaded.
- **reacting-to-changes**: dav writes bypass transition constraints and
  handlers; rules/constraints propagation delay.
- **identity**: authoring keys (Bearer/Basic) on the dav plane; the edge never
  accepts keys as end-user identity.
- **site/config**: CORS for aliases on OPTIONS preflight; host naming
  (`dav-` prefix).
- **store**: byte-exact storage of dav-uploaded HTML; per-site write mutex
  (MOVE concurrency); subtree generations for PROPFIND tags.

## 14. Contradictions and compatibility decisions

| # | Topic | Claims | Decision |
|---|---|---|---|
| C-1 | literal `*` in `Allow` | D-OPT example `Allow: *, OPTIONS` vs D-OPT/D-RULE "the `Allow` header never contains a literal `*`" | Expand wildcard (R-PROTO-12); example kept as disputed case |
| C-2 | OPTIONS part `Content-Range` | D-OPT example `selector=h1` vs BJ-PRIM/PP comments (server "now emits" / "live server emits" `selector <css>`) and live GET/QUERY `selector h1` | Emit space form (R-PROTO-3); disputed case for `=` |
| C-3 | `Allow` order | live 200 `HEAD, GET, OPTIONS` vs live 204 `GET, HEAD, OPTIONS` vs docs order | Canonical order; order-insensitive assertions |
| C-4 | `Accept-Query` on OPTIONS | D-Q says advertised on OPTIONS vs LIVE:capabilities has none | Emit it (superset); cases local-only |
| C-5 | `Accept-Ranges` | D-AR `selector, bytes` on every HTML response, `selector` on OPTIONS, CDN leaves `bytes` vs live responses carry none | Emit documented values; never assert live |
| C-6 | edge css QUERY shape | D-Q "returns every matching element as multipart/mixed … even a single-document query" vs LIVE:query-css `206` single fragment, `Content-Range: selector h1` | Accept-driven (R-PROTO-63): GET-equivalent by default, multipart when explicitly accepted |
| C-7 | QUERY status codes | dav example `201` + `text/html` doc vs text "200 OK"/multipart; conditional example `200` vs text `304`; Sessel element example `206`, value example `200` with `text/html` and mismatched bodies | dav 200 multipart; 304 on match; Sessel value 200, element 206 (cases accept 200/206) |
| C-8 | edge QUERY composition | text "queries the composed page" vs example body showing raw `{{ 2 \| plus: 3 }}` | Composed |
| C-9 | wrong QUERY Content-Type | intro: 415 + `Accept-Query` vs Sessel table: 400 | 415 for missing/unsupported types; 400 only for Sessel body problems |
| C-10 | Sessel default scope in QUERY | D-SES: bare selectors query the whole site vs D-Q examples use bare `${h1}` on a document as if document-scoped | Keep language semantics; cases use `from self`/`Selector.execute()`; P-21 |
| C-11 | Sessel QUERY authorization | D-Q implies an ordinary read vs LIVE:query-sessel `401` for anonymous where GET/css QUERY succeed | Authorize as method QUERY; never via default-GET (R-PROTO-74) |
| C-12 | cross-document MOVE | reading-and-writing index "within or across documents" vs D-MOVE `501 MoveCrossResource` | 501 |
| C-13 | MOVE reliability | D-MOVE: concurrent moves both land; If-Match works vs KANBAN:29-40 (older build): ~3/4 concurrent moves lost with 204, every conditional write rejected even `If-Match: *` | Docs |
| C-14 | MOVE `If-Match` tag | D-MOVE: document ETag vs beta-js sends the source element's ETag from HEAD+Range | Accept either (R-PROTO-100); P-13 |
| C-15 | MOVE `Destination` form | D-MOVE `<path>` vs beta-js absolute URL (with query) | Accept both; compare paths |
| C-16 | MOVE SSE events | D-SSE method list PUT/POST/DELETE vs its own `placement` row and BJ-SSE MOVE handling with `destination` | Emit MOVE events (owned by SSE spec) |
| C-17 | 204 OPTIONS fallback vs official client | D-OPT/LIVE 204 vs BJ-PRIM crash (`TypeError`, `ready` rejects) and PP throw on non-multipart 2xx | Follow docs/live; document client bug; P-3 checks whether `Prefer` changes the server answer |
| C-18 | error vocabularies | `https://pagelove.org/Error` (dav, 413) vs `https://pagelove.org/1.0/Error` (401) vs `http://pagelove.org/Error` (live 416) vs `ConstraintViolation` (422) | Reproduce per condition; assert only status + documented kinds |
| C-19 | MKCOL on existing collection | RFC 4918 `405` vs PageLove `409` + `DirectoryAlreadyExists` (SHOP, BJ-SYNC) | 409 |
| C-20 | dav PUT with missing parent | RFC 4918 `409` vs DEMO "no directory creation is needed" | Create parents |
| C-21 | PROPFIND default depth | RFC 4918 infinity (and x/net/webdav) vs D-DAV default 1 | 1 |
| C-22 | `entries` general mechanism | D-Q/D-AR point to Reading and writing vs no reading-and-writing page mentions `entries` | Implement for Sessel QUERY only; GET syntax pending P-20 |
| C-23 | dav credentials | D-Q examples `Authorization: Basic` vs all tooling `Authorization: Bearer <key>` | Accept both |
| C-24 | invalid selector status | D-MOVE/D-Q 422 vs CURSOR:481-489 table "416 Invalid CSS selector" (older agent skill) | 422 for MOVE/QUERY |
| C-25 | `Content-Range` in GET/Pagination docs | D-RW/D-PAG `selector=h1`, `selector=ul#contacts` vs live `selector h1` | Space form (cross-area with reading-writing) |

## 15. Open questions for live probing

Read-only probes can run against `https://docs.pagelove.com/` (anonymous);
others need the disposable test host (`PAGELOVE_HOST`, `PAGELOVE_DAV_URL`, key)
with files and rules under a probe prefix `/_pl/probe-<id>/`. Each line is the
minimal sequence; record full status + headers + body.

- **P-1 wildcard in Allow.** Host: rule `* /_pl/p1/* * Allow`. `OPTIONS /_pl/p1/a.html`; `OPTIONS /_pl/p1/a.html` + `Range: selector=h1`; with rule selector `h1`: same two. Settles C-1 and the exact expansion lists (does `QUERY` appear?).
- **P-2 Allow order stability.** `OPTIONS https://docs.pagelove.com/` ×3; ×3 with `Accept: multipart/mixed`. Compare token order (C-3).
- **P-3 Prefer.** `OPTIONS https://docs.pagelove.com/` with `Accept: multipart/mixed` and `Prefer: return=representation` (no Range), then without `Prefer`. 204 both → R-PROTO-20 holds; 207 with Prefer → beta-js crash is avoided upstream and pagelike must mirror it.
- **P-4 207 details.** Host: doc `/_pl/p4/d.html` with `<ul><li>`; rules `* GET Allow`, `* POST Allow selector ul`, `editors PUT Allow selector li`. `OPTIONS` + `Accept: multipart/mixed` as anonymous, then as an editor, then with `Range: selector=li`. Records part order, `Content-Range` spelling, whether other actors' selector rules create parts, whether Range changes the answer, part body/CRLF layout.
- **P-5 flat selector semantics.** Same host: `OPTIONS` + `Range: selector=ul`, `Range: selector=body > ul`, `Range: selector=h1[` (invalid). Does string-inequivalent selector get the POST? Does invalid syntax error?
- **P-6 OPTIONS on deny host.** Deny-mode host, no rules: anonymous `OPTIONS /_pl/p6/x.html` (200 `Allow: OPTIONS`? 401?). Then add rule `* OPTIONS Deny` and repeat.
- **P-7 Accept-Query / Accept-Ranges exposure.** `OPTIONS` on the dav URL (bypasses the public CDN?) and on the edge; `GET` on a blob and on an XML document (R-PROTO-25/34).
- **P-8 edge css QUERY branches.** `QUERY https://docs.pagelove.com/` + `Content-Type: text/css-selector`: (a) body `p` (many matches), no Accept; (b) body `h1` + `Accept: multipart/mixed`; (c) body `#pagelove-probe-none`; (d) body `h1` + `Accept: text/html`; (e) repeat (b) with the returned ETag in `If-None-Match`.
- **P-9 composition.** Host doc with `<main class="calc" pagelove:template="text/liquid">{{ 2 | plus: 3 }}</main>`; edge `QUERY` `.calc` → `5` or raw (C-8); dav `QUERY` `.calc` → raw.
- **P-10 invalid selector on edge QUERY.** `QUERY https://docs.pagelove.com/` body `h1[` → 422 / 400 / 416?
- **P-11 415.** `QUERY https://docs.pagelove.com/` with no Content-Type; with `text/plain`; with `text/sessel` + empty body. Record `Accept-Query` value (C-9).
- **P-12 Sessel authorization.** Host rules `* GET Allow` only → anonymous `QUERY /_pl/p12/d.html` `text/sessel` body `1 + 1` (401?); add `* QUERY Allow` → 200 `application/sessel+json` `2`?
- **P-13 MOVE If-Match.** Host board doc + MOVE/DELETE/POST rules. `HEAD` + `Range: selector=#card-3` → E_el; `GET` → E_doc; `MOVE` with `If-Match: E_el`; reset; `MOVE` with `If-Match: E_doc`; `MOVE` with `If-Match: *`.
- **P-14 MOVE Destination URL.** `Destination: https://<host>/_pl/p14/board.html?x=1` and `https://other.example/_pl/p14/board.html`.
- **P-15 MOVE error precedence.** (a) cross-doc Destination + non-matching source; (b) no rules + non-matching source; (c) only `Range` + no rules; (d) readable doc, DELETE rule not covering a non-matching source; (e) deny-GET doc + non-matching source.
- **P-16 whole-document MOVE edge cases.** Onto an existing path; with `Overwrite: F`; Destination = source; SSE events on source and destination streams; `If-Match` on whole-document MOVE.
- **P-17 MOVE response headers.** Record all headers of a 204 MOVE (ETag? Content-Range?).
- **P-18 MOVE structural edges.** `Destination-Range: selector=html; placement=before`; anchor = source with `before`, with `append`; source = `html`; placement `sideways`.
- **P-19 entries semantics.** Host doc with five `<li>`; `QUERY` `text/sessel` body `(new Selector { "li" }).execute()` with `Range: entries=0-1`, `entries=1-1`, `entries=3-10`, `entries=5-6`, `entries=2-`, `entries=-2`, `entries=3-1`; also on `(new Selector { "li" }).execute().count()`. Records base, inclusivity, clamping, status, 416 form.
- **P-20 entries on GET.** `GET /_pl/p19/d.html` + `Accept: multipart/mixed` with `Range: selector=li` (baseline), then `Range: entries=0-1`, `Range: selector=li; entries=0-1`, `Range: selector=li, entries=0-1`; same with `Accept: application/ld+json`.
- **P-21 Sessel result encoding/status.** Element result (`(new Selector { "h1" }).execute().first()`): status and `Content-Range`? Bare `${h1}.first()` on a document when another document also has an `h1` (scope, C-10). Encodings of an Instance (`X.search()`), a date, a Selector, a dictionary containing an element; `"abc".Integer()` (400 vs 500); `throw "x"`.
- **P-22 Sessel directory targets.** `QUERY /_pl/p22/` (with and without `index.html`) body `self` and body `1`; a prefix with no documents.
- **P-23 PROPFIND.** `PROPFIND /_pl/p23/` `Depth: infinity`; file PROPFIND header ETag vs body `getetag` vs dav GET ETag; If-Match on dav PUT with the header tag; unrelated change outside the collection then `If-None-Match`; dav `OPTIONS /` headers (`DAV`, `Allow`, `Accept-Query`).
- **P-24 dav writes.** `MKCOL /_pl/p24/a/b/` with missing parent; `PUT /.pagelove/x.html` over dav; `PUT /_pl/p24/../x.html`; error kinds/bodies for 404, 400.
- **P-25 dav writes and SSE.** Subscribe to `/_pl/p25/d.html` on the edge (rule `* GET Allow`), `PUT` it over dav → mutation? reset? nothing?
- **P-26 dav QUERY details.** `QUERY /_pl/p26/` (two docs, three matches, one blob, one XML): part header casing and order, `Content-Location` form, `Content-Range` selector form (round-trip it with a GET `Range`), document order, whether blobs/XML appear, bytes of a zero-match response.
- **P-27 bytes ranges.** `GET /_pl/p27/d.html` + `Range: bytes=0-9`.
- **P-28 blob Accept-Ranges.** `GET /_pl/p27/a.json` headers.

## 16. Harness cases

Files under `harness/cases/protocol/`:

| File | Covers |
|---|---|
| `options.yaml` | R-PROTO-10..22, 31; C-1, C-2, C-3, C-4 |
| `accept-ranges.yaml` | R-PROTO-5, 30..33 |
| `query-dav.yaml` | R-PROTO-43, 50..57 |
| `query-edge.yaml` | R-PROTO-60..64; C-6, C-8 |
| `query-sessel.yaml` | R-PROTO-70..75, 80..83; C-7, C-9, C-11 |
| `move.yaml` | R-PROTO-90..95, 98..101, 103..105 |
| `move-authz.yaml` | R-PROTO-96..99 |
| `webdav.yaml` | R-PROTO-111..121 |

Case index (ids without the `protocol.` prefix; 131 cases, 4 marked
`status: disputed` as the losing side of C-1, C-2, C-6, C-9):

- `options.yaml`: `options.flat-document-level`, `options.no-existence-check`, `options.selector-no-match`, `options.selector-scoped-flat`, `options.wildcard-document-expansion`, `options.wildcard-selector-expansion`, `options.wildcard-star-example` (disputed), `options.multipart-207`, `options.multipart-content-range-space-form`, `options.multipart-content-range-equals-form` (disputed), `options.multipart-204-document-rules-only`, `options.multipart-204-no-rules`, `options.default-get-host-no-rules`, `options.deny-host-no-rules`, `options.multipart-with-range-no-selector-rules`, `options.prefer-return-representation-ignored`, `options.accept-ranges`, `options.accept-query`, `options.does-not-modify`, `options.default-get-never-grants-move`, `options.move-allow-rule-with-selector-discarded`, `options.selector-deny-removes-method`, `options.actor-specific-selector-rules`
- `accept-ranges.yaml`: `accept-ranges.html-full-document`, `accept-ranges.html-fragment`, `accept-ranges.sessel-unit-ignored`, `accept-ranges.unknown-unit-ignored`
- `query-dav.yaml`: `query-dav.single-document-multipart`, `query-dav.part-headers`, `query-dav.multiple-matches`, `query-dav.prefix-scope`, `query-dav.host-scope`, `query-dav.no-match`, `query-dav.conditional-304`, `query-dav.etag-changes-with-fragment`, `query-dav.etag-depends-on-selector-text`, `query-dav.etag-ignores-unmatched-change`, `query-dav.raw-markup`, `query-dav.415-missing-content-type`, `query-dav.415-sessel-not-served`, `query-dav.422-empty-or-invalid`, `query-dav.406-accept`, `query-dav.404-missing-target`
- `query-edge.yaml`: `query-edge.single-fragment`, `query-edge.same-etag-as-get-fragment`, `query-edge.documented-multipart` (disputed), `query-edge.multipart-when-accepted`, `query-edge.composed-page`, `query-edge.authorized-like-get`, `query-edge.default-get-mode`, `query-edge.directory-index`, `query-edge.missing-document`, `query-edge.415-unsupported-type`, `query-edge.422-invalid-selector`, `query-edge.no-match`
- `query-sessel.yaml`: `query-sessel.number-result`, `query-sessel.element-result`, `query-sessel.element-list-tagged`, `query-sessel.constructed-element-no-source`, `query-sessel.scalar-results`, `query-sessel.directory-self-unbound`, `query-sessel.entries-pagination`, `query-sessel.entries-format`, `query-sessel.entries-non-list-416`, `query-sessel.400-body-errors`, `query-sessel.500-runtime-error`, `query-sessel.404-missing-document`, `query-sessel.415-unsupported-type`, `query-sessel.400-wrong-type-documented` (disputed), `query-sessel.not-granted-by-get`, `query-sessel.default-get-host-401`
- `move.yaml`: `move.append-example`, `move.before-example`, `move.prepend-and-after`, `move.placement-case-insensitive`, `move.incomplete-requests-422`, `move.unknown-placement-422`, `move.cross-document-501`, `move.source-absent-416`, `move.destination-absent-404`, `move.invalid-selectors-422`, `move.into-own-descendant-422`, `move.first-source-match`, `move.first-anchor-match`, `move.whole-document`, `move.whole-document-missing-source`, `move.element-move-missing-document`, `move.if-match-stale-412`, `move.if-match-current`, `move.if-match-element-etag`, `move.destination-absolute-url`, `move.concurrent-moves-both-land`, `move.mutation-event`, `move.blob-with-ranges-422`, `move.never-405`, `move.reserved-namespace-403`
- `move-authz.yaml`: `move-authz.default-deny-even-with-default-get`, `move-authz.move-rule-alone-insufficient`, `move-authz.each-check-required`, `move-authz.allow-move-rule-with-selector-discarded`, `move-authz.whitespace-selector-is-fine`, `move-authz.deny-move-rule-with-selector-widened`, `move-authz.deny-with-selector-widens-co-listed-methods`, `move-authz.post-rule-on-container-covers-all-placements`, `move-authz.append-keys-on-anchor`, `move-authz.narrow-delete-rule`, `move-authz.narrow-post-rule`, `move-authz.fail-closed-when-unreadable`, `move-authz.authenticated-denial-403`, `move-authz.editors-example`, `move-authz.whole-document-needs-destination-grant`, `move-authz.whole-document-deny-at-destination`, `move-authz.whole-document-needs-no-delete-post`
- `webdav.yaml`: `webdav.propfind-collection`, `webdav.propfind-304`, `webdav.propfind-default-depth-is-1`, `webdav.depth-tags-differ`, `webdav.tag-changes-on-child-add-edit-remove`, `webdav.tag-ignores-outside-changes`, `webdav.file-propfind-304`, `webdav.file-header-tag-differs-from-content-tag`, `webdav.put-if-match-content-tag`, `webdav.put-if-match-properties-tag-rejected`, `webdav.put-create-and-replace`, `webdav.put-if-none-match-star`, `webdav.get-is-byte-exact`, `webdav.put-creates-parents`, `webdav.mkcol`, `webdav.missing-is-404`, `webdav.bypasses-authorization-rules`, `webdav.unauthenticated-401`

Notes for the runner (requirements on `harness/`, not on the server):
- Sessel selector literals `${…}` collide with the runner's `${name}`
  substitution. Cases therefore use the documented equivalent
  `(new Selector { "li" }).execute()` (= `${li} from self`) and never write
  `${` in Sessel bodies. A literal-`$` escape (proposal: `$${` → `${`) would let
  future cases quote docs examples verbatim.
- `move.yaml` uses `${ORIGIN}` (scheme://host[:port] of the plane under test)
  for the absolute-URL `Destination` case; the runner must provide it.
- `Allow` assertions use `header_matches` regexes that are order-insensitive;
  where both presence and absence matter the same request is sent twice with
  different regexes.
- Live runs: rules written over WebDAV may take up to 60 s to bind on PageLove
  (R-PROTO-112); cases that depend on rules are still `live: true` and rely on
  the runner writing rules through a path that binds immediately or waiting.
  Cases whose outcome depends on the host default-GET mode set
  `site.settings.default_get` and are `live: false`.

## 17. Live reconciliation 2026-09-28

A live PageLove run on 2026-09-28 (host `live-test-host`,
`harness/observations/live-2026-09-28/`), confirmed on 2026-09-29
(`harness/observations/live-2026-09-29-confirm/`), failed 27 protocol cases.
[docs/compat/decisions.md](../compat/decisions.md) has one row per case, giving
the observed behaviour, the documented claim, the decision and the rationale.
Details mined from passing cases are in
[docs/compat/live-observations.md](../compat/live-observations.md) (LO-4
onward).

**Superseded by live observation.** Each requirement below is marked in place
with the new behaviour:

| Requirement | New behaviour |
|---|---|
| R-PROTO-4 | Boundary shape; no trailing CRLF after the close delimiter. |
| R-PROTO-12/14/17 | The flat answer is the union of document-level and selector-scoped grants. The 207's selector parts list selector-scoped grants only. |
| R-PROTO-42(b)/43 | Edge QUERY with a non-css type takes the Sessel path: `400`, no `415` (C-9 reversed). |
| R-PROTO-54 | Part headers and the `nth-child` Content-Range. |
| R-PROTO-56 | Any write changes the answer's tag. |
| R-PROTO-64 | An invalid selector is 416 (P-10). |
| R-PROTO-71 | A directory means its index (P-22), and `Selector.execute()` does not exist. |
| R-PROTO-73 | No `$source`. |
| R-PROTO-75 | 400 and 422 errors; `throw` is 400. |
| R-PROTO-81 | The value ends with `;`. |
| R-PROTO-99 | The MOVE error table (P-15). |
| R-PROTO-100 | The source element's tag only (C-14). |
| R-PROTO-115 | MKCOL: 405 on an existing collection, 409 for a missing parent (C-19 reversed). |
| R-PROTO-116 | A DELETE of an absent path is 204. |
| R-PROTO-120 | Short authoring articles. |

Refined in place: R-PROTO-6, 57, 63, 103, 111, 114, 117..119 and 124.

**Kept despite live divergence.** Each is marked in place, and a `.live`
sibling measures PageLove:

| Requirement | Kept behaviour | Class |
|---|---|---|
| R-PROTO-91 | Query strings in `Destination` are ignored | keep-standard |
| R-PROTO-98 | Missing source: 404, not 500 | keep-standard |
| R-PROTO-99 | Unreadable page: denial, not 416 | keep-documented-security |
| R-PROTO-12 | OPTIONS removes denied methods | keep-documented-security |
| R-PROTO-113 | Byte-exact dav storage | adopt-live 2026-09-29 (LO-15): stored serialized |

**Documented supersets kept:** C-4 and C-5 (`Accept-Query` and
`Accept-Ranges` on OPTIONS and HTML responses). PageLove sends neither on
OPTIONS or HTML reads, and sends `Accept-Ranges: bytes` on blobs, XML,
JSON-LD, all-matches and dav answers. pagelike keeps the documented values
and sends `bytes` where PageLove serves like a blob.

**Confirmed:** C-1, C-2 and C-6. Their disputed cases are XFAIL live.

**Settled probes:**
- P-10: 416.
- P-13: element tag.
- P-15: table above.
- P-19: zero-based inclusive, trailing `;`.
- P-21: `throw` is 400; `$source` absent.
- P-22: the index.
- P-24: 409 `ParentDirectoryMissing`.
- P-25: no events.
- P-26: part format.

**Harness note:** the runner now supports `$${…}` for literal Sessel `${…}`
(§16's proposal). The Sessel cases use `${sel} from self`, because PageLove has
no `Selector.execute()`.
