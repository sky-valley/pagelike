# Reading and writing — behavioral specification

Area: `reading-writing` (requirement prefix `R-RW`). Harness cases:
`harness/cases/reading-writing/*.yaml` (ids `rw.*`).

This document specifies the public application plane (`<site>.<domain>`,
PageLove's `dombase-http` edge) for: GET/HEAD, PUT, POST (incl. placement),
DELETE, selector ranges, content negotiation (HTML, JSON-LD, all-matches
multipart), microdata value extraction, uploads/blobs, directory indexes and
redirects, ETags/conditional requests/caching, the request-body size cap, the
reserved `/.pagelove/` namespace, the absence-vs-denial rules (416 vs 401/403),
error documents, and the Request Document. MOVE and Server-Sent Events live in
the same doc group upstream but are specified by their own areas; they appear
here only as cross-area dependencies.

---

## 0. Conventions

**Normative words.** MUST / SHOULD / MAY as in RFC 2119. "pagelike MUST" marks
a compatibility decision where sources are silent or disagree.

**Evidence levels.** `documented` (public docs), `client-source` (official
browser client beta-js), `demo-source` (official apps), `live-observed` (read
probes against docs.pagelove.com on 2026-09-28), `inferred` (reasoned; the
reason is given).

**Source keys** (snapshot 2026-09-28; the combined page
`docs.pagelove.com_all_reference_reading-and-writing.md` is byte-identical to
the individual pages except where noted in §23):

| Key | Source |
|---|---|
| [GET] [PUT] [POST] [DELETE] [MOVE] [CN] [UP] [RD] [SSE] | docs `reference/reading-and-writing/{GET-method,PUT-method,POST-method,DELETE-method,MOVE-method,Content-Negotiation,Uploading-Files,Request-Document,Server-Sent-Events}` |
| [AR] [OPT] [QRY] [WD] | docs `reference/protocol/{Accept-Ranges,OPTIONS-method,QUERY,WebDAV}` |
| [AZR] | docs `reference/permissions/AuthorizationRule` |
| [SX] [PAG] [RC] [XML] [INC] [PR] [TR] | docs `reference/composing-pages/{Selector-Extensions,Pagination,Resource-Creation,XML-Documents,Includes,Parameterized-Routes,Transient-Elements}` |
| [SC] | docs `reference/modeling-data/ShapeConstraint` |
| [IDX] | docs index page (`docs.pagelove.com_index.md`, "A minimal example") |
| [GL-range] [GL-sru] [GL-mv] [GL-res] [GL-method] | docs `reference/glossary/{range-header,selector-range-unit,microdata-value,resource,method}` |
| [PRIM-DOC] | docs `languages/javascript/client/primitives` |
| [BJS-P] | `beta-js/pagelove/primitives.mjs@c204746` |
| [BJS-M] [BJS-C] [BJS-S] | `beta-js/pagelove.mjs`, `pagelove/component.mjs`, `pagelove/sse.mjs` @c204746 |
| [PP] | `pagelove-primitives/index.mjs@e73986d` (older client) |
| [KAN] | `pagelove-kanban/site/app.js@85109ab` |
| [POLL] [POLLC] | `pagelove-polls/site/assets/{poll.js,create.js}@c9270e5` |
| [SHOP] | `pagelove-shop/site/js/shop.mjs@d887054` |
| [ATS] | `pagelove-ats/site/{apply.html,admin/index.html}@8f200fc` |
| [DEMO] | `demo-apps@c4dd883` (`scripts/check-live.py`, `demo-*/app.js`) |
| [PROMPT] | `pagelove/prompts/src/partials/*.liquid@deb2857` (Feb 2026, older server) |
| [LIVE] | private research notes of 2026-09-28, `read-probes.json` (not published) (cases `fragment`, `missing-fragment`, `query-css`, `query-sessel`) |

**Terms.** *Document*: an HTML resource (stored content type `text/html`).
*XML document*: stored content type `application/xml`, `text/xml` or `*/*+xml`.
*Blob*: any other stored resource. *Composed view*: the HTML produced by
running composition (templates, bindings, includes, stamps, pagination,
transients, `@read` resolvers) over a stored document for the current request
(area `composing`). *Target*: the element(s) a selector range addresses.
*Anchor*: the target of a POST placement.

---

## 1. Resources and path resolution

**R-RW-1 Resource kinds.** Every stored path is exactly one of: HTML document,
XML document, or blob, decided solely by its stored content type (R-RW-60),
never by the request's `Accept`. HTML/XML documents are parsed, composed and
selector-addressable; blobs are opaque bytes.
Evidence: documented. Source: [UP] §intro, §Content type; [XML] §XML-family
content types. Confidence: high.

**R-RW-2 Path lookup.** The lookup key is the percent-decoded request path.
The query string never participates in lookup (it is still visible to
composition as `request.query`). A path is case-sensitive.
Evidence: demo-source (clients cache-bust document reads and selector reads
with `?v=`/`?sync=`/`?t=` and expect the same document: [KAN]:44, [KAN]:311,
[ATS] index.html:413) + inferred (percent-decoding: [PR] decodes captured
segments; case sensitivity: standard). Confidence: high (query), medium
(decoding, case). Edge: `GET /doc.html?x=1` with `Range: selector=h1` → same
206 as without the query.

**R-RW-3 Directory index.** A GET/HEAD whose path ends in `/` is served from
`<path>index.html` (same status, headers and selector semantics as requesting
`<path>index.html` directly). If there is no `index.html` there (and no
parameterized route matches, see cross-area X-3) the result is `404`. There is
no public-plane directory listing.
Evidence: documented ([GET] §Directory requests) + demo-source ([DEMO]
check-live.py:12-20 expects `200` for `/demo-01-event/` etc.). Listing absence:
inferred. Confidence: high.

**R-RW-4 Slash-less directory redirect.** A GET/HEAD for a path that (a) does
not end in `/`, (b) has no stored resource at that exact path, (c) whose final
segment contains no `.`, and (d) for which `<path>/index.html` exists **and is
readable by the requester** (R-RW-125ff), is answered `301 Moved Permanently`
with `Location: <path>/` followed by `?<original query>` when the request had
a query string. The `Location` value is path-absolute as in the docs
(`/blog/`); pagelike MUST emit a path-absolute reference. Body: optional short
HTML; clients must not depend on it.
Evidence: documented ([GET] §Directory requests). Confidence: high (status,
Location, query preservation); medium (path-absolute form).
Edge cases:
- `/blog?x=1&y=2` → `Location: /blog/?x=1&y=2` (query verbatim, not re-encoded).
- Index exists but requester may not read it → `404` (no redirect; do not
  reveal existence). Requesting `/blog/` in that case returns the normal
  denial (`401`/`403`, R-RW-125).
- Final segment contains `.` (e.g. `/v1.2`, `/style.css`) → always a file
  request: `404` if nothing is stored there, even if `/v1.2/index.html`
  exists.
- No `index.html` → plain `404`.
- Only GET/HEAD redirect (inferred: the docs describe reads; a PUT to `/blog`
  addresses the literal path `/blog`).

> **Superseded by live observation (2026-09-28), in part.**
> - When the requester may not read the index, the answer is no longer `404`.
>   Rules for `<path>/*` also govern the slash-less `<path>`, so an unreadable
>   directory gets the ordinary denial: `401`/`403`, with `resource` set to the
>   slash-less path.
> - The `301` has no body and carries `Cache-Control: private, max-age=3600`.
>
> Cases: `rw.dir.unreadable-index-404`, `rw.dir.redirect-preserves-query`.
> See [decisions](../compat/decisions.md).

**R-RW-5 Implicit directories.** Directories are implicit: a whole-document
PUT to `/a/b/c.html` succeeds without creating `/a/` or `/a/b/` first.
Evidence: demo-source ([KAN]:1541-1553 PUTs `/uploads/<generated>`; [ATS]
apply.html:622-631 PUTs `/uploads/<id>-cv.<ext>`; [SHOP]:493-503 PUTs
`/images/<stem>.<ext>`). Confidence: high.

**R-RW-6 Reserved namespace.** Any write (`PUT`, `POST`, `DELETE`, `MOVE`,
and pagelike also `PATCH`) whose target path is under the prefix `/.pagelove/`
is refused with `403 Forbidden` for every actor, including anonymous (not
`401`), regardless of authorization rules. Only the root prefix is reserved
(`/x/.pagelove/y.html` is an ordinary path). The body is an error document
(R-RW-130).
Evidence: documented ([PUT] §Error cases and "Reserved namespace" note).
Confidence: high (403 on writes), medium (403 independent of identity/rules,
root-only prefix). Reads of `/.pagelove/…` are unspecified (open question
Q-17); pagelike SHOULD answer `404` for reads because nothing user-visible is
stored there.

**R-RW-7 Methods.** This area covers GET, HEAD, PUT, POST and DELETE on
the public plane. MOVE, OPTIONS and QUERY are specified by areas `move` and
`protocol`. The glossary lists PATCH ("apply a CRDT change-set") but no page
documents its wire format; pagelike answers PATCH with `501 Not Implemented`
and an Error item (kind `NotImplemented`), and any other unknown method with
`405 Method Not Allowed` plus an `Allow` header. beta-js treats `405` on MOVE
as "unsupported, fall back to POST+DELETE" ([BJS-C]:796-800), so `405` must
never be used for an authorization failure.
Evidence: documented ([GL-method]) + pagelike principle ("unsupported
operations fail clearly": docs/design.md) + client-source. Confidence: medium.

---

## 2. The `Range` header and selector range unit

**R-RW-10 Grammar.** A selector range is
`Range: selector=<css-selector>` optionally followed, for POST only, by
`;` OWS `placement=<placement>`. The server:
1. strips leading/trailing OWS from the field value;
2. requires the unit token `selector` followed immediately by `=`
   (the clients never send spaces around `=`: [BJS-P]:263, [KAN]:9);
3. for POST, if the remainder matches `^(.*?)\s*;\s*placement\s*=\s*([A-Za-z]+)\s*$`
   (case-insensitive key) the selector is group 1 and the placement group 2;
   otherwise the whole remainder is the selector (so `;` inside quoted CSS
   strings is preserved);
4. trims the selector. An empty selector is a malformed range (R-RW-17).
The selector is a CSS Selectors Level 4 selector list plus PageLove extensions
(area `selectors`).
Evidence: documented ([GET], [POST] §Placement "supply placement=<value> as a
Range: sub-field", example `Range: selector=ul; placement=prepend`); client
header shape client-source ([BJS-P]:263, [KAN]:9, [BJS-C]:777-779).
Confidence: high (basic form), medium (sub-field parsing details).
Edge: a `placement=` sub-field on GET/PUT/DELETE is ignored by pagelike
(inferred; [KAN]:9 only ever adds it to POST).

**R-RW-11 Header encoding.** Range header values are decoded as UTF-8, so
`Range: selector=p:contains('café')` works as written; CSS escapes
(`caf\E9 `) are equivalent.
Evidence: documented ([SX] §Non-ASCII text in selectors). Confidence: high.

**R-RW-12 Unknown units are ignored.** A `Range` header whose unit is neither
`selector`, `bytes` nor `entries` (e.g. the removed `sessel=`) is ignored: the
request is processed as if no `Range` were sent (full `200` for GET).
Evidence: documented ([AR] "an unrecognised range unit is ignored and you get
the whole resource back"). Confidence: high.

**R-RW-13 `bytes` unit.** `Accept-Ranges: selector, bytes` is advertised on
HTML resources ([AR]); byte ranges are therefore part of the surface.
pagelike MUST support a single `bytes=<first>-<last>` / `bytes=<first>-` /
`bytes=-<suffix>` range on blobs (206, `Content-Range: bytes <f>-<l>/<len>`,
416 with `Content-Range: bytes */<len>` when unsatisfiable, per RFC 9110
§14) and MAY ignore byte ranges on HTML/XML documents (serve 200).
Multi-range byte requests MAY be answered with the full 200.
Evidence: documented (advertisement only) + inferred (RFC 9110). Confidence:
low. (Header advertisement is area `protocol`; see §23 C-7 for the live
observation that no `Accept-Ranges` header was returned.)

**R-RW-14 `entries` unit.** `Range: entries=<start>-<end>` paginates list
results (answered with `Content-Range: entries <start>-<end>/<total>`); the
docs point to "Reading and writing" for the general mechanism but that page
does not describe it. Out of scope here except: pagelike MUST NOT treat
`entries=` on a plain GET of an HTML document as a selector range; until
specified by the `query`/`composing` areas it is ignored (full 200).
Evidence: documented ([AR], [QRY] §Paginating a list result) — mechanism
undocumented for GET. Confidence: low. Open question Q-15.

**R-RW-15 First match, document order.** When the selector matches several
elements, a single-target operation (GET/HEAD with a non-multipart,
non-JSON-LD `Accept`; PUT; POST; DELETE) uses the **first match in document
order** (pre-order traversal of the composed view), including for selector
lists (`h2, h1` returns an `<h1>` that precedes the `<h2>`). Elements in
`<head>` are addressable.
Evidence: documented ([GL-sru] "only the first match is used"; [AZR]
§Selectors that match several elements "acts on the first match in document
order only"); selector lists and head: inferred (CSS `querySelector`
semantics, which the client mirrors). Confidence: high.

**R-RW-16 Evaluation target.** Selectors in reads are evaluated against the
composed view of the requested document for the requesting actor (templates,
bindings, includes, pagination run first; the selector then extracts).
Selector writes are also resolved against the composed view; when the target
originates from an include/stamp the write is routed to the origin resource
(cross-area X-3). Evidence: documented ([PAG] §With Range selectors; [INC]
§Interaction with HTTP Document Mutation; [POST] §Concurrency note). Confidence:
high (reads), medium (writes).

**R-RW-17 Malformed ranges.** A `Range: selector=` with an empty selector, or
a selector that fails to parse, is a client error. pagelike MUST answer `422
Unprocessable Content` for an unparsable selector and `400 Bad Request` for a
syntactically broken `selector=` field (empty value). No write occurs.
Evidence: inferred — MOVE answers `422` for an invalid `Range` selector
([MOVE] §Error cases) and QUERY answers `422` for an unparsable selector
([QRY] §Errors); the older server documented `400` for "Invalid Range header
syntax" ([PROMPT] response_codes). Confidence: low. Case accepts
`[400, 416, 422]`. Open question Q-6.

> **Superseded by live observation (2026-09-28) for reads.** A GET or HEAD
> (and a css QUERY) whose selector is empty or does not parse answers `416`
> with the read error document "HTML parsing error: Invalid CSS selector: …"
> (`rw-get-malformed-selector.json`). Writes keep `422`/`400`. See
> [decisions](../compat/decisions.md) (C-10).

---

## 3. GET and HEAD — whole resource

**R-RW-20 Whole-document GET.** `GET <path>` without a selector range returns
`200 OK` with the complete composed representation. For HTML documents:
`Content-Type: text/html` (a `charset=utf-8` parameter is allowed; clients
match the prefix), body = serialized composed document. For XML documents the
stored XML content type is kept ([XML]). For blobs see §15.
Evidence: documented ([GET] §When to reach for it; [DELETE] §Verify the
removal example). Confidence: high.

**R-RW-21 Whole-document serialization.** The serialization reproduces the
stored markup's structure and inter-element whitespace; it MUST NOT insert
parser-implied `<head>`/`<body>`/`<tbody>` elements that were absent from the
stored source (the docs' DELETE verification example returns
`<html><body>…</body></html>` with no `<head>`), and removed elements leave
their neighbouring whitespace text nodes in place. HTML-namespace
declarations used only by PageLove directives (`xmlns:p`, `xmlns:e`, …) are
stripped from HTML responses (area `composing`).
Evidence: documented ([DELETE] §Verify the removal; [INC] basic include
example; [XML] §Differences from HTML composition). Confidence: medium (docs
examples appear to be generated by a test suite against the real server:
fixture names like `/delete-verify-test.html`).

**R-RW-22 ETag and validators on 200.** Every `200 OK` carries an `ETag`
(§13) and, where the server knows one, `Last-Modified` (the time of the last
committed change of the stored resource). pagelike MUST send `Last-Modified`
on whole-resource reads of stored documents and blobs and MAY omit it on
fragment responses (the live 206 had none).
Evidence: documented ([GET] §Caching) + live-observed (no `Last-Modified` on
206, [LIVE] `fragment`). Confidence: high (ETag), medium (Last-Modified
placement).

**R-RW-23 Missing resource.** A GET/HEAD for a path with no stored resource,
no directory index (R-RW-3/4) and no parameterized route is `404 Not Found`
with an HTML error document (R-RW-130). A selector range does not change this:
missing document → `404`, never `416`.
Evidence: documented ([GET] §Resource not found, §Error cases; [PR] §Error
cases). Confidence: high.

**R-RW-24 HEAD.** HEAD returns exactly the status and headers GET would
return for the same request (including `ETag`, `Content-Range`, `Vary`,
`Content-Type`; `Content-Length` MAY be the GET length or omitted) and no
body. beta-js obtains element ETags with
`HEAD` + `Range: selector=<generated selector>` ([BJS-P]:484-487) and reads
only the `ETag` response header.
Evidence: client-source ([BJS-P]:474-501, [PRIM-DOC] §ETag handling).
Confidence: high.

---

## 4. GET and HEAD — selector ranges

**R-RW-25 Fragment response.** `GET <path>` with `Range: selector=<css>` on an
HTML/XML document whose composed view has at least one match returns
`206 Partial Content` with:
- body: the outer serialization of the first match (R-RW-15), with no
  leading/trailing whitespace outside the element; it MUST parse (after
  trimming) as **exactly one node** because clients reject anything else
  ([BJS-P]:92-111 `htmlToNode`, used by `GET()` at :407-416);
- `Content-Type: text/html` (live: exactly `text/html`, no charset; XML
  documents keep their XML type);
- `Content-Range` identifying the element (R-RW-26);
- `ETag` for the fragment (R-RW-27);
- `Vary` including `Range` and `Accept` (R-RW-37).
Elements serialize with the HTML serialization algorithm; element-context
elements (e.g. `<tbody>`, `<tr>`) are returned bare — clients wrap them
themselves ([POLLC]:91-104 wraps a `<tbody>` in `<table>`).
Evidence: documented ([GET] §Retrieve a single element), client-source,
live-observed ([LIVE] `fragment`: 206, `Content-Type: text/html`, body is the
bare `<h1 …>…</h1>`). Confidence: high.

**R-RW-26 `Content-Range` wire format.** pagelike MUST emit
`Content-Range: selector <css>` — the unit, one SP, then the request's
selector text verbatim (trimmed; without any `; placement=` sub-field). This
is the RFC 9110 `Content-Range` shape and is what the live server sends.
Clients accept both `selector <css>` and `selector=<css>` ([BJS-P]:193-202,
[PP]:131-137, whose comment states the server sends the space form).
Evidence: live-observed ([LIVE] `fragment`: `content-range: selector h1`;
`query-css` identical) vs documented `Content-Range: selector=h1` ([GET]
example, [IDX], [PAG], [OPT] multipart parts). Confidence: medium. See §23
C-1. The value echoes the request selector, not a normalized path (live:
request `h1` → `selector h1`).

**R-RW-27 Fragment ETag.** A 206 fragment response carries an `ETag` that
identifies the current state of the matched element (§13, R-RW-97), and a
HEAD with the same `Range` returns the same tag. That tag is accepted in
`If-Match` on a subsequent selector write to the same element (R-RW-89) and in
`If-None-Match` on a subsequent fragment GET (R-RW-88).
Evidence: client-source ([BJS-P]:258-273 sends `If-Match: <element.etag>` on
PUT/DELETE; :484-490 fills `element.etag` from `HEAD`+`Range`), live-observed
(tag present on 206: `"<64hex>-<64hex>-5"`). Confidence: medium.

**R-RW-28 No match.** If the document exists, the requester may read it, and
the selector matches no element in the composed view, the answer is
`416 Range Not Satisfiable` with an HTML error document and no `ETag`
(live: `vary: Host, Range`, `Content-Type: text/html`). The 416 must not
carry a `Content-Range`.
Evidence: documented ([GET] §Selector matches nothing), live-observed
([LIVE] `missing-fragment`). Confidence: high. For who-may-learn-absence see
R-RW-127.

**R-RW-29 Selector on a blob.** A selector-range read (or write) on a blob is
`422 Unprocessable Content` ("selector operations require an HTML or XML
document").
Evidence: documented ([UP] §Serving). Confidence: high.

**R-RW-30 Selector on XML.** Selector ranges work on XML documents (the
XML dialect: case-sensitive names, no void elements); the fragment is
serialized as well-formed XML and served with the stored XML content type.
Evidence: documented ([UP] §Serving "selector operations require an HTML or
XML document"; [XML] §The XML dialect). Confidence: medium.

> **Superseded by live observation (2026-09-28).** On the public plane, XML
> documents are served like blobs.
> - A selector read or write is `422` "Invalid path: Selector operations
>   require HTML documents, but <path> has content type <type>". R-RW-29 uses
>   the same message.
> - Whole-document XML reads carry `Vary: Host, Range` and
>   `Accept-Ranges: bytes`.
> - The authoring-plane QUERY still reads XML.
>
> Case: `rw.up.xml-selector`. See [decisions](../compat/decisions.md).

---

## 5. All-matches reads

**R-RW-31 multipart/mixed.** A GET with a selector range and an `Accept`
whose preferred supported type is `multipart/mixed` (listed explicitly, not
via a wildcard) returns **every** match, in document order, as a
`multipart/mixed` body. pagelike wire format (compat decision, modeled on the
documented OPTIONS/QUERY multiparts and on what the clients' multipart parser
requires):

```
HTTP/1.1 206 Partial Content
Content-Type: multipart/mixed; boundary=<boundary>
Vary: Range, Accept
ETag: "<tag over selector text + every part's ETag>"

--<boundary>\r\n
Content-Type: text/html\r\n
Content-Range: selector <document-rooted selector addressing this match>\r\n
ETag: "<element tag>"\r\n
\r\n
<outerHTML of match 1>\r\n
--<boundary>\r\n
…
--<boundary>--\r\n
```

Part headers use CRLF line breaks and exactly `": "` between name and value
(the clients split on `\r\n\r\n` and `": "`: [BJS-P]:113-131, [PP]:60-76). The
`boundary` parameter is the last parameter of `Content-Type` (clients match
`/boundary=(.+)$/`). The per-part `Content-Range` is a document-rooted
selector that uniquely addresses that match (round-trippable into a
follow-up PUT/DELETE), as documented for QUERY parts ([QRY] §On the WebDAV
authoring server).
Status: documented only as "answered with every match"; pagelike uses `206`
(inferred: partial representation, as with RFC 9110 multipart/byteranges).
Zero matches → `416` as for single-target reads (inferred).
Evidence: documented ([AZR] §Selectors that match several elements), rest
inferred. Confidence: medium (existence), low (status and part headers).
Authorization: R-RW-128.

> **Superseded by live observation (2026-09-28): wire format.**
> - Status `200`, `Vary: Host, Range, Accept, Paginate`, no top-level `ETag`.
> - The boundary is `boundary` followed by 32 alphanumerics.
> - Each part carries, in this order: `Content-Disposition: attachment;
>   filename="<document path>"`, `Content-Type`,
>   `Content-Range: selector <request selector>` (the request's selector, not
>   a document-rooted one), `ETag: <fragment tag>` and `Content-Length`.
> - The close delimiter ends the body, with no trailing CRLF.
>
> Cases: `authz.multimatch.all-matches-read-denied-if-any-denied` (last step)
> and the multipart observations. See [LO-8](../compat/live-observations.md).

**R-RW-32 JSON-LD of all matches.** A GET with a selector range and
`Accept: application/ld+json` (R-RW-36) returns the JSON-LD of every match:
for each match in document order, if the matched element carries `itemscope`
it contributes that item, otherwise it contributes the top-level items
(R-RW-40) inside its subtree. Items are then serialized with R-RW-41..48 (one
item → flat object; several → `@graph`). Status `206` with
`Content-Range: selector <css>` and `Content-Type: application/ld+json;
charset=utf-8` (compat decision; the docs only say "answered with every match
… as a @graph array").
Evidence: documented ([AZR] §Selectors that match several elements; [CN]
"Content negotiation applies to both full-document and selector requests").
Confidence: medium (all matches, @graph), low (status, flat-vs-graph for a
single match). Open question Q-9.

---

## 6. Content negotiation

**R-RW-35 Supported representations.** For HTML documents two representations
exist: `text/html` (default; the composed HTML) and `application/ld+json`
(the composed page's microdata as JSON-LD). When `Accept` is absent, or
matches neither, HTML is returned with `200` — never `406`.
Evidence: documented ([CN] §Supported media types). Confidence: high.

**R-RW-36 Selection algorithm (compat decision).** Parse `Accept` per RFC 9110
§12.5.1. Let *qH* be the effective q of `text/html` and *qJ* of
`application/ld+json` (most specific matching range wins: exact type >
`type/*` > `*/*`; missing → 0). Serve JSON-LD iff *qJ* > 0 and *qJ* > *qH*;
otherwise HTML. Consequences: `Accept: application/ld+json` → JSON-LD;
browser default `text/html,…,*/*;q=0.8` → HTML; `*/*` → HTML;
`application/json` → HTML. `multipart/mixed` is considered only with a
selector range (R-RW-31).
Evidence: inferred (the docs describe only exact values). Confidence: low
for q-value edge cases; high for the four listed consequences. Open question
Q-8.

**R-RW-37 `Vary`.** Every response for an HTML document (whole, fragment,
JSON-LD, 304, and error responses produced after the resource was resolved)
carries `Vary` listing at least `Accept`; fragment and range-error responses
also list `Range`. pagelike emits `Vary: Range, Accept` (live additionally
lists `Host`, which pagelike MAY include).
Evidence: documented ([CN] §Vary header "All responses include Vary: Accept"),
live-observed (206: `vary: Host, Range, Accept`; 416: `vary: Host, Range` —
see §23 C-4). Confidence: medium.

> **Superseded by live observation (2026-09-28).** pagelike sends PageLove's
> sets:
>
> | Response | `Vary` |
> |---|---|
> | Negotiated reads (200/206 HTML, JSON-LD) | `Host, Range, Accept` |
> | All-matches reads | `Host, Range, Accept, Paginate` |
> | Writes, 304, blobs, XML, and read/write errors | `Host, Range` |
> | Denials (`401`/`403`) and the authorization layer's `416` | none |
>
> See [LO-6](../compat/live-observations.md) and
> [decisions](../compat/decisions.md) (C-4).

**R-RW-38 JSON-LD response.** `200 OK`,
`Content-Type: application/ld+json; charset=utf-8`, body = JSON-LD built from
the **rendered** (composed) page — templates and `@read` resolvers run first.
Carries its own `ETag` distinct from the HTML representation's (§13).
Evidence: documented ([CN] §JSON-LD serialization, §Example). Confidence:
high (status/type/source), medium (distinct ETag — RFC 9110 requirement).

**R-RW-39 Non-HTML resources ignore `Accept`.** Blobs are returned unchanged
with their stored type whatever `Accept` says; XML documents are always served
as XML (no JSON-LD).
Evidence: documented ([CN] §Non-HTML resources; [XML] §Differences from HTML
composition). Confidence: high.

> **Refined by live observation (2026-09-28).** The rule still holds, and
> XML is now also refused for selectors (R-RW-30). Blob responses carry
> `Vary: Host, Range`, not `Accept`, and a 304 drops `Cache-Control` and
> `Accept-Ranges` ([LO-6](../compat/live-observations.md)).

---

## 7. JSON-LD serialization

**R-RW-40 Top-level items.** A top-level item is an element with `itemscope`
and without `itemprop` (WHATWG microdata). Items reached as property values
of another item are nested, never top-level. `itemscope` elements with
`itemprop` whose ancestors have no item are ignored (inferred, WHATWG).
Evidence: documented ([CN] "multiple top-level itemscope elements") + WHATWG.
Confidence: high.

**R-RW-41 Context inference.** For an item whose (first) `itemtype` token is
an absolute URL, `@context` is the URL up to and including its last `/` and
`@type` is the remainder: `http://schema.org/Person` →
`"@context": "http://schema.org/"`, `"@type": "Person"`;
`https://pagelove.org/Request/HTTP/Query` → `https://pagelove.org/Request/HTTP/`
+ `Query`.
Evidence: documented ([CN] §Context inference). Confidence: high for the
documented example; low for URLs with `#` fragments or trailing `/` (Q-10).

**R-RW-42 Non-URL types.** A non-URL `itemtype` (e.g. `Widget`) yields
`"@type": "Widget"` and no `@context`. An item without `itemtype` yields no
`@type` and no `@context` (inferred).
Evidence: documented ([CN]). Confidence: high / low (no itemtype).

**R-RW-43 Single item.** Exactly one top-level item → a flat object
`{"@context": …, "@type": …, <properties>}` (no `@graph`).
Evidence: documented. Confidence: high. (§23 C-5: the Feb-2026 prompt showed
`@graph` for one item.)

**R-RW-44 Several items.** Two or more → `{"@graph": [ … ]}` in document
order. If every item has the same `@context`, it is hoisted:
`{"@context": C, "@graph": [{"@type": …}, …]}` and omitted from the members.
Otherwise there is no top-level `@context` and each member that has one
carries its own.
Evidence: documented ([CN] §Multiple items). Confidence: high (hoisting),
medium (absence of a top-level context in the mixed case).

**R-RW-45 Property values.** Each property name maps to its value (R-RW-50);
a property with a single value is a scalar (JSON string, or object for an
item); a property occurring more than once in the item is a JSON array in
document (crawl) order. Values are strings; pagelike does not coerce numbers
or booleans.
Evidence: documented (scalar `"name": "Alice"`), array/typing inferred.
Confidence: medium.

**R-RW-46 Nested items.** A property whose element has `itemscope` has an
object value with its own `@type` (per R-RW-41/42) and properties; it carries
`@context` only if its context differs from the enclosing item's.
Evidence: inferred (JSON-LD scoping; docs silent). Confidence: low.

**R-RW-47 `itemid` and `itemref`.** `itemid` maps to `@id` (inferred). The
property crawl of an item follows `itemref` (space-separated IDs; referenced
elements' subtrees contribute properties as if they were inside the item),
per WHATWG "the properties of an item". Cycles are broken by visiting each
element once.
Evidence: WHATWG microdata + inferred mapping. Confidence: medium (itemref),
low (@id).

> **Kept despite live observation (2026-09-28).** PageLove's JSON-LD ignores
> `itemref`. pagelike keeps the WHATWG crawl (keep-standard), and
> `rw.md.itemref.live` measures the divergence.

**R-RW-48 No items.** A page with no top-level items returns
`{"@graph": []}` (compat decision, `200`).
Evidence: inferred. Confidence: low. Q-11.

**R-RW-49 Property names.** An `itemprop` attribute is a set of
space-separated tokens; the element contributes its value to each named
property. Names are used verbatim (absolute-URL names are not shortened).
Multiple `itemtype` tokens: the first determines `@context`/`@type`
(inferred).
Evidence: WHATWG; inferred for PageLove. Confidence: medium / low.

> **Kept despite live observation (2026-09-28).** PageLove emits a single key
> `"name alternateName"`. pagelike keeps one property per token
> (keep-standard), and `rw.md.multiple-itemprop-tokens.live` measures the
> divergence.

JSON formatting (whitespace, key order) is not significant; pagelike emits
`@context`, `@type`, `@id` first, then properties in first-occurrence order.

> **Superseded by live observation (2026-09-28): key order.** PageLove
> serializes every JSON-LD object with its keys in sorted order (so `@…` keys
> come first), with no HTML escaping. pagelike does the same.
> - A whole-document JSON-LD answer gets a strong tag, the SHA-256 of the
>   JSON.
> - An all-matches JSON-LD answer gets a weak tag (`W/"…"`).
>
> See [LO-8](../compat/live-observations.md).

---

## 8. Microdata value extraction (normative for JSON-LD, shared with selectors)

PageLove states that microdata values follow the WHATWG HTML Microdata
specification ([GL-mv], [SX] §Text content vs microdata values). pagelike's
`internal/microdata` MUST implement exactly this table; the same function
backs `:value-*` selectors (area `selectors`) and schema property reads (area
`modeling`).

**R-RW-50 Property value of an element** (first rule that applies wins):

| # | Element | Value |
|---|---|---|
| 1 | has `itemscope` | the nested item (R-RW-46) |
| 2 | `meta` | `content` attribute; `""` if absent |
| 3 | `audio`, `embed`, `iframe`, `img`, `source`, `track`, `video` | `src` attribute; `""` if absent |
| 4 | `a`, `area`, `link` | `href` attribute; `""` if absent |
| 5 | `object` | `data` attribute; `""` if absent |
| 6 | `data`, `meter` | `value` attribute; `""` if absent |
| 7 | `time` | `datetime` attribute if present; otherwise the descendant text content |
| 8 | any other element | descendant text content (concatenation of all descendant text nodes, in tree order, markup removed) |

Evidence: documented ([SX] §Text content vs microdata values table and "When
a spec-listed element is missing its designated attribute, the microdata
value is an empty string — except `<time>`"). Confidence: high.

**R-RW-51 URL values.** WHATWG resolves rows 3–5 to absolute URLs against the
document base URL; PageLove's selector docs compare against the raw attribute
(`[itemprop=url]:value-equals('/about')` matches `href="/about"`). pagelike
MUST use the raw attribute value (no resolution) in JSON-LD, for consistency
with selector semantics. Evidence: documented (selector behaviour) +
compat decision. Confidence: low (JSON-LD). Q-12.

**R-RW-52 No normalization in values.** Values are not trimmed or collapsed
(WHATWG). The NFC + trim + whitespace-collapse normalization in [SX] §Text
normalization applies only when *matching* in selector pseudo-classes, not to
extracted JSON-LD values. Evidence: inferred (the normalization is described
as part of "text-based operations"). Confidence: low. Q-13.

**R-RW-53 Case of element names.** Element-name comparisons in the table are
ASCII case-insensitive for HTML documents and case-sensitive for XML
documents (XML dialect, [XML]). Confidence: medium.

---

## 9. PUT

**R-RW-60 Stored content type resolution** (applies to every whole-resource
write: PUT, WebDAV PUT, POST-created resources). In order:

| # | Rule |
|---|---|
| 1 | path ends in `.html` or `.htm` (ASCII case-insensitive, pagelike decision) → `text/html`, whatever `Content-Type` says |
| 2 | else the request's `Content-Type` value, stored verbatim (including parameters, even if generic or wrong, e.g. `application/x-www-form-urlencoded`) |
| 3 | else inferred from the extension (table below) |
| 4 | else `application/octet-stream` |

The stored type is served back verbatim as `Content-Type` and decides the
resource kind (R-RW-1).
Evidence: documented ([UP] §Content type), demo-source for byte uploads with
`file.type` ([KAN]:1544-1548, [SHOP]:493-503, [ATS] apply.html:625-628) and the
blog-reported bug of PDFs stored as form uploads. Confidence: high (order),
medium (parameter preservation, `.HTML` casing).

Extension table (pagelike; inferred except `.css`/`.png` which are
documented): `.css text/css`, `.js`/`.mjs text/javascript`, `.json
application/json`, `.xml application/xml`, `.rss application/rss+xml`, `.atom
application/atom+xml`, `.svg image/svg+xml`, `.xhtml application/xhtml+xml`,
`.png image/png`, `.jpg`/`.jpeg image/jpeg`, `.gif image/gif`, `.webp
image/webp`, `.avif image/avif`, `.ico image/x-icon`, `.woff font/woff`,
`.woff2 font/woff2`, `.ttf font/ttf`, `.otf font/otf`, `.pdf
application/pdf`, `.txt text/plain`, `.csv text/csv`, `.md text/markdown`,
`.zip application/zip`, `.wasm application/wasm`, `.mp3 audio/mpeg`, `.mp4
video/mp4`, `.webm video/webm`. Unknown → rule 4.

**R-RW-61 Whole-resource PUT: create.** `PUT <path>` without a selector range
to a path with no stored resource stores the body as a new resource and
returns `201 Created`. For HTML documents the body is the stored document (the
docs show the document echoed); for blobs pagelike returns an empty body.
Response headers: `ETag` of the new resource (R-RW-99). Authorization: `PUT`
on the path (area `permissions`); validation (schemas, shape constraints)
may reject with `422` (area `modeling`).
Evidence: documented ([SC] examples: `PUT /people/complete-user.html` →
`HTTP/1.1 201` + echoed document; [PROMPT] "201 Created: Successful PUT
creating new document"). Confidence: high (201), medium (echo).

**R-RW-62 Whole-resource PUT: replace.** A whole-resource PUT to an existing
path replaces it atomically. Status is not documented; pagelike returns
`200 OK` with the same body convention as R-RW-61 and the new `ETag`. Clients
only test `response.ok`.
Evidence: inferred (RFC 9110 §9.3.4; [KAN]:556, [SHOP]:609-613 check only
`ok`). Confidence: low. Cases accept `[200, 201, 204]`. Q-2.

**R-RW-63 Whole-document writes are literal.** A whole-resource PUT (and
DELETE) addresses the stored path verbatim — parameterized routes and
directory indexes are not resolved (`PUT /blog/` is not a write to
`/blog/index.html`; pagelike answers `400` for a whole-resource write to a path
ending in `/`).
Evidence: documented for routes ([PR] "Whole-document writes … are literal
only"); trailing-slash handling inferred. Confidence: high / low.

**R-RW-64 Selector PUT.** `PUT <path>` with `Range: selector=<css>` on an HTML
or XML document replaces the first match (R-RW-15) with the nodes parsed from
the body and returns `206 Partial Content` with:
- body: serialization of the replacement as stored (for the usual single
  element body, exactly that element — clients parse it with a
  single-node parser: [POLL]:99-106 `parseFragment(await res.text())`);
- `Content-Range: selector <request selector>` identifying the replaced
  element (documented in prose; the docs' examples omit the header);
- `ETag` of the new element (client-source: [BJS-P]:296-297 stores the
  response ETag on the element and reuses it as `If-Match` on the next write,
  so a missing ETag would make the next conditional write fail).
The write persists: a subsequent `GET` with the same selector returns the new
element.
Evidence: documented ([PUT] §Replace a single element, §Write persists),
client-source. Confidence: high (status/body/persist), medium
(Content-Range/ETag).

**R-RW-65 Fragment parsing context.** Bodies of selector writes are parsed
with the HTML fragment parsing algorithm in the context of the element that
will become the new nodes' parent: the target's parent for PUT and for POST
`before`/`after`; the anchor itself for POST `append`/`prepend`. This is what
lets `<tr>…</tr>` be POSTed into a `<tbody>` and a `<tr>` be PUT over a row.
For XML documents the body is parsed as an XML fragment with in-scope
namespaces of the context.
Evidence: demo-source ([POLL]:56-63, :99-121 — POST `<tr>` to `#responses`,
a `<tbody>`; PUT `<tr>` over `#r-xxxx`). Confidence: high.

**R-RW-66 No upsert.** A selector PUT whose selector matches nothing is
`416` (R-RW-127), not an insert; a selector PUT to a missing document is
`404`. Clients implement upsert as PUT → on 416 → POST append ([KAN]:50-60).
Evidence: documented ([PUT] §Error cases), demo-source. Confidence: high.

**R-RW-67 Body shapes.** Content-Type for selector writes: the body is always
parsed as markup of the document's dialect; `text/html`,
`text/html;charset=UTF-8` (beta-js, [BJS-P]:264),
`text/html; charset=utf-8` and a missing `Content-Type` are all accepted
(inferred). A body with several top-level nodes replaces the target with all
of them in order (inferred). An empty or whitespace-only body: pagelike
answers `400` (compat decision; Q-3). The replacement need not still match
the request selector (inferred; the "must still match" identity rule applies
only to transient elements, area `composing`, [TR]). Confidence: low.

**R-RW-68 Concurrency (whole-document PUT).** A conditional whole-document
PUT (`If-Match`) succeeds only if the precondition still holds **when the
write commits**; a concurrent committed write in between makes it `412`
(§12). Evidence: documented ([PUT] §Concurrency). Confidence: high.

**R-RW-69 Error table (PUT).** `404` document missing (selector PUT); `416`
no match; `401`/`403` denied (R-RW-125); `403` reserved namespace; `422`
schema/shape validation failed, selector-scoped write on a collaborative
(CRDT) document that cannot be reconciled, or selector on a blob; `413` body
over cap; `412` precondition failed. Evidence: documented ([PUT] §Error
cases, [UP]). Confidence: high.

---

## 10. POST

**R-RW-70 Selector POST inserts.** `POST <path>` with `Range: selector=<css>`
inserts the nodes parsed from the body (R-RW-65) relative to the first match
(the anchor) according to `placement` (default `append`):

| placement | insertion site |
|---|---|
| `append` (default) | after the anchor's last child |
| `prepend` | before the anchor's first child (before leading whitespace text) |
| `before` | immediately before the anchor, as a sibling |
| `after` | immediately after the anchor, as a sibling |

Existing children are preserved. All nodes of the body, including whitespace
text nodes, are inserted (the docs' read-back shows the request's trailing
newline in the stored list).
Evidence: documented ([POST] §Placement, §Verify the append; [IDX]).
Confidence: high.

**R-RW-71 Placement values.** Values are ASCII case-insensitive (as for MOVE,
[MOVE] §Placement). An unknown value (e.g. `placement=inside`) is `400 Bad
Request` (compat decision; Q-5).
Confidence: medium (case), low (400).

> **Superseded by live observation (2026-09-28).** An unknown placement is
> treated as `append` (`206`, appended). Case: `rw.post.unknown-placement`.
> See [decisions](../compat/decisions.md).

**R-RW-72 POST response.** `206 Partial Content`, body = serialization of the
inserted content with surrounding whitespace-only text nodes trimmed — for the
usual single-element body, exactly that element — and
`Content-Range: selector <request selector>` naming the anchor (the updated
element), not the inserted child. Clients parse the body as a single node and
append it locally ([BJS-P]:350-354; [POLL]:113-118; [PP]:229-233).
Evidence: documented ([POST] examples; [IDX] shows `Content-Range:
selector=#items` with body `<li>Second item</li>`), client-source.
Confidence: high (body), medium (Content-Range).

**R-RW-73 POST `ETag` / `Content-Location` rule.** beta-js interprets the
POST response per RFC 9110 §8.8.3: if there is no `Content-Location`, or it
equals the request URI (same path, no query, no fragment), the `ETag` is the
**anchor's** new tag; if `Content-Location` names anything else (different
path, or any query or fragment), the `ETag` is the **new child's** tag
([BJS-P]:355-396). pagelike MUST NOT send `Content-Location` on selector POST
responses and MUST send `ETag` = the anchor's element tag after the insertion
(R-RW-97), so that `If-Match` on the anchor's next write succeeds. (If an
`ETag` were for the child without a distinguishing `Content-Location`, the
client would pin a wrong tag on the parent — the failure mode the client code
comment describes: "stale If-Match on the first write against the child,
causing 412".)
Evidence: client-source. Confidence: medium (what the live server sends is
unknown: Q-4).

> **Superseded by live observation (2026-09-28).** A selector POST has no
> `Content-Location`, and its `ETag` is the **inserted child's** fragment tag,
> `"<sha256(child)>-<sha256(child)>-<document version>"`. `Content-Range`
> still names the request selector. Case: `rw.post.etag-is-anchor-tag`
> (Q-4 settled). See [decisions](../compat/decisions.md).

**R-RW-74 Parentless anchor.** `placement=before`/`after` whose anchor has no
element parent (the root `<html>`) is `400 Bad Request`; nothing is written.
Evidence: documented ([POST] §Placement). Confidence: high.

> **Superseded by live observation (2026-09-28).** The answer is `422`, with a
> problems item of type `https://pagelove.org/Error`: "placement=Before
> relative to the document root is not possible: the root has no siblings".
> Case: `rw.post.parentless-anchor-400`.

**R-RW-75 Absent anchor.** With `append`/`prepend` (or no placement) and no
match → `416` (subject to R-RW-127). With `before`/`after` and no match → the
request is **refused** as an authorization failure (`401` anonymous / `403`
authenticated), not `416`, because the insertion site (the anchor's parent)
cannot be authorized (fail-closed, as for MOVE check 3).
Evidence: documented ([POST] "Absence and denial are distinct" note).
Confidence: medium (status of the refusal).

> **Superseded by live observation (2026-09-28).** `before`/`after` with an
> absent anchor behaves like `append`: `416` when the actor may read the page,
> and a denial otherwise (R-RW-127).
> - When POST is granted only by selector-scoped rules, the `416` comes from
>   the authorization layer: read vocabulary with `charset=utf-8`,
>   `Content-Range: selector */` and no `Vary`.
>
> Cases: `rw.post.absent-anchor-before-refused`,
> `authz.absent.post-before-absent-anchor-refuses`.

**R-RW-76 Concurrency.** Appends are additive: concurrent selector POSTs to
the same anchor all succeed and all land (each is applied against the latest
committed version under the site write lock). A conditional POST
(`If-Match`) whose tag is no longer current is `412` and is not applied.
beta-js never sends `If-Match` on POST ([BJS-P]:265).
Evidence: documented ([POST] §Concurrency), client-source. Confidence: high.

**R-RW-77 Stamped/included anchors.** When the anchor was projected into the
composed page by `<p:stamp>`/`<p:include>` (or a parameterized route
template), the insertion is routed to the origin resource and retried against
the origin's latest version on conflict; authorization is evaluated against
the requested (composed) page. Cross-area X-3. Evidence: documented ([POST]
§Concurrency note; [INC]; [PR]). Confidence: high.

**R-RW-78 POST without a selector range.** A POST with no `Range` targets
resource creation: POST to a template resource (composed; output must contain
`<base href>`; `301` to the new resource; `422` without `<base href>`) — area
`composing` ([RC]). [UP] additionally says POST "to a directory" creates a
blob under a generated name "the same way" — underspecified (§23 C-9, Q-16).

**R-RW-79 POST errors.** `404` document missing; `416` no match (append);
`400` bad placement / parentless anchor; `401`/`403` denied; `403` reserved
namespace; `422` validation; `412` precondition; `413` over cap. Evidence:
documented ([POST] §Error cases). Confidence: high.

---

## 11. DELETE

**R-RW-80 Selector DELETE.** `DELETE <path>` with `Range: selector=<css>`
removes the first match and all its descendants and returns `204 No Content`
with `Content-Range: selector <request selector>` and no body. Sibling
whitespace text nodes stay. Clients remove the element locally on any 2xx
([BJS-P]:327-341) and treat `416` on a retried delete as "already gone"
([BJS-C]:929-943, [POLL]:90).
Evidence: documented ([DELETE] §Remove an element, §Verify the removal),
client-source. Confidence: high (204), medium (Content-Range).

> **Superseded by live observation (2026-09-28).** The `204` carries `ETag`
> (the document's new tag), `Last-Modified` and `Vary: Host, Range`, but **no**
> `Content-Range`. Case: `rw.delete.content-range`.

**R-RW-81 Whole-document DELETE.** `DELETE <path>` without a range removes the
resource (document or blob) and returns `204`; a later GET is `404`.
Evidence: documented ([DELETE] §Delete a whole document). Confidence: high.

**R-RW-82 DELETE errors.** `404` resource missing; `416` no match; `401`/`403`
denied; `403` reserved namespace; `422` validation (e.g. cardinality or state
machine exits) or non-reconcilable collaborative document; `412` stale
`If-Match`. Evidence: documented ([DELETE] §Error cases). Confidence: high.

> **Superseded by live observation (2026-09-28), R-RW-81/82.** A
> whole-document DELETE of a missing path succeeds: `204` with the ETag of
> empty content (`"e3b0c442…b855"`), `Last-Modified` and `Vary: Host, Range`.
> A selector DELETE there is still `404`, with a `NotFound` problems item.
> Case: `rw.delete.missing-document-404`.

**R-RW-83 Conditional DELETE.** Whole-document DELETE honours `If-Match`
(single, list, `*`) exactly as PUT; a failed precondition is `412` with the
current `ETag`, and nothing is removed. pagelike applies the same to selector
DELETE (R-RW-89). Evidence: documented ([DELETE] §Concurrency).
Confidence: high.

---

## 12. Conditional requests

**R-RW-85 Entity-tag syntax.** Tags are strong, double-quoted opaque strings
(`"…"`). `If-Match`/`If-None-Match` accept `*` or a comma-separated list of
tags with OWS. A listed tag written without quotes is compared as if quoted
(compat decision, because the SSE `etag` property is shown unquoted in the
docs and beta-js copies it into `If-Match`: [SSE] example, [BJS-S]:247-249).
Weak tags (`W/"x"`) never match in `If-Match` (strong comparison) and match
by opaque value in `If-None-Match` (weak comparison).
Evidence: documented forms ([PUT] §Concurrency), RFC 9110 §13.1.
Confidence: high (forms), low (unquoted tolerance).

**R-RW-86 `If-Match` on writes.** The write proceeds only if the target
exists and the header is `*`, or any listed tag matches (R-RW-89) the current
state; otherwise `412 Precondition Failed` and nothing is written.
`If-Match: *` on a whole-resource PUT to an absent path is `412` (the path is
not created).
Evidence: documented ([PUT] §Concurrency). Confidence: high.

**R-RW-87 `If-None-Match` on writes.** `If-None-Match: *` makes a PUT
create-only: `412` if anything is stored at the path, else create (`201`). A
listed tag that matches the current state → `412`; non-matching tags →
proceed.
Evidence: documented ([PUT] §Concurrency). Confidence: high.

**R-RW-88 `If-None-Match` on reads.** GET/HEAD with `If-None-Match` listing
the tag the response would carry (or `*` when the resource exists) →
`304 Not Modified`, no body, with the `ETag`, `Vary`, `Cache-Control` and
`Content-Location` headers the 200/206 would have had. Works for whole
documents, blobs, fragments (tag of the fragment) and JSON-LD (tag of that
representation).
Evidence: documented ([GET] §Caching; [UP] §Serving), fragment case inferred.
Confidence: high (whole/blob), medium (fragment).

> **Superseded by live observation (2026-09-28): 304 headers.** A `304`
> carries `ETag`, `Last-Modified` (when the 200 had one) and
> `Vary: Host, Range`. It drops `Cache-Control` and `Accept-Ranges`. See
> [LO-6](../compat/live-observations.md).

**R-RW-89 What a tag is compared with.**
- Whole-resource writes: the resource's stored-version tag, or the tag a
  whole-resource GET would currently return (they differ only for composed
  documents).
- Selector writes (PUT/POST/DELETE with `Range`): the stored-version tag of
  the document, the tag a whole-resource GET would currently return, **or**
  the current element tag of the target (the tag a GET/HEAD with the same
  `Range` would return now). The element tag is what
  beta-js sends ([BJS-P]:265-268); the document tag is what the docs describe
  for conditional appends ([POST] §Concurrency).
Evidence: documented + client-source; union is a compat decision.
Confidence: medium. Q-1 asks whether live element tags are element- or
document-scoped.

> **Superseded by live observation (2026-09-28) for selector writes.** Only
> the current tag of the addressed element satisfies `If-Match`: the target
> for PUT and DELETE, the anchor for POST, and the source for MOVE. The
> document's tag is `412`. The `412` carries that element's tag and the
> message "Precondition Failed: …". Element tags include the document version
> (R-RW-97), so any committed write to the document invalidates them. Whole-
> resource writes are unchanged. Cases:
> `rw.cond.post-if-match-document-tag`, `protocol.move.if-match-current`
> (Q-1 settled).

**R-RW-90 Ordering.** Evaluate in this order: body-size cap (R-RW-120) →
reserved namespace (R-RW-6) → authorization of the method on the path →
resource existence (404; skipped for a whole-resource PUT, which may create)
→ resource kind (blob + selector → 422) → range
resolution (416 / absence refusals, R-RW-127) → element authorization →
preconditions (412/304) → mutation and validation (422) → commit. Per RFC
9110 §13.2.1 preconditions are ignored when the request would otherwise fail
with a non-2xx/412 status. The precondition is re-checked atomically inside
the commit (under the per-site write mutex), so a write that loses a race is
`412` rather than silently applied.
Evidence: documented ("when the write commits": [PUT] §Concurrency), RFC 9110,
ordering inferred. Confidence: medium.

**R-RW-91 412 response.** `412 Precondition Failed`, HTML error document,
`ETag` = the tag a GET with the same `Range` (or without, for whole-resource
writes) would return now, so a client can retry without re-reading; beta-js
stores that `ETag` ([BJS-P]:296-297).
Evidence: documented ([PUT] §Concurrency "The 412 response carries the
document's current ETag"; [DELETE] §Concurrency). Confidence: high (present),
medium (fragment-scoped value for selector writes).

**R-RW-92 `If-Modified-Since` / `If-Unmodified-Since`.** pagelike honours them
per RFC 9110 only when the corresponding entity-tag header is absent and the
response carries `Last-Modified`. Evidence: inferred. Confidence: low.

---

## 13. Entity tags

**R-RW-95 General.** Every successful read representation carries a strong
`ETag`. Tags are opaque; clients never parse them. A tag changes whenever the
bytes of that representation change. HTML, JSON-LD and each fragment are
distinct representations with distinct tags.
Evidence: documented ([GET] §Caching), RFC 9110. Confidence: high.

**R-RW-96 Stored-version tag.** Each stored resource has a stored-version tag
that changes on every committed change of its bytes. pagelike:
`"<lowercase hex SHA-256 of the stored bytes>"`. The whole-resource GET tag
of a document whose composition has no request-dependent or cross-document
input (no directives) MUST equal its stored-version tag; otherwise pagelike
uses `"<hexV>-<hex SHA-256 of the served bytes>"` so the tag changes when an
included/bound resource changes. pagelike stores documents in their
re-serialized form so that served bytes of a directive-free document equal
the stored bytes.
Evidence: inferred. Confidence: low (format), medium (behaviour).

> **Superseded by live observation (2026-09-28) for composed pages.**
> PageLove's whole-document tags are opaque (LO-2), so pagelike keeps its
> stored-version tag for static documents. A **composed** page instead gets:
> - a weak `W/"<sha256 of the served bytes>"`;
> - `Cache-Control: public, max-age=5` (except R-RW-103 pages);
> - no `Last-Modified`.
>
> See [LO-4](../compat/live-observations.md).

**R-RW-97 Element tag.** The tag of a fragment identifies the current state
of that element in the composed view. Live shape:
`"<64 hex>-<64 hex>-<decimal>"` (both hashes equal for a static page; exact
semantics unknown). The client-source evidence ([BJS-P]:355-366: assigning the
parent's tag to a freshly POSTed child caused 412s) shows tags are
element-specific. pagelike: `"<hex SHA-256 of the element's served
outerHTML>"` — a content hash, so an unrelated change elsewhere in the
document does not invalidate it.
Evidence: live-observed shape, client-source, pagelike decision. Confidence:
low (semantics). Q-1.

> **Superseded by live observation (2026-09-28).** The element tag is
> `"<sha256 of the served fragment>-<the same hash>-<document version>"`,
> byte for byte as PageLove forms it. The document version starts at 1 on
> upload and increases with every write, so **any** write to the document
> changes every element tag. Cases: `rw.post.etag-is-anchor-tag`,
> `protocol.query-dav.etag-ignores-unmatched-change`. See
> [LO-4](../compat/live-observations.md).

**R-RW-98 Blob tag.** A blob's tag is a strong content hash: identical bytes
yield identical tags (also across paths); any change yields a new tag.
pagelike: `"<hex SHA-256 of the bytes>"`.
Evidence: documented ([UP] §Serving "content-hash strong ETag"). Confidence:
high (strong/content-derived), medium (equality across paths).

**R-RW-99 Write responses carry the resulting tag.** Successful writes return
`ETag`: whole-resource PUT → new stored-version tag; selector PUT → new
element's tag; selector POST → anchor's tag (R-RW-73); selector DELETE →
pagelike sends the document's stored-version tag; whole DELETE → none.
Evidence: client-source ([BJS-P]:296-297 and the POST logic); DELETE values
inferred. Confidence: medium.

> **Superseded by live observation (2026-09-28).**
>
> | Write | `ETag` |
> |---|---|
> | Selector PUT | The replacement's fragment tag. If the replacement no longer matches the request selector, the document's tag. |
> | Selector POST | The inserted child's fragment tag (R-RW-73). |
> | Selector DELETE | The document's new tag. |
> | Whole DELETE | The tag of empty content, `"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"`. |
>
> Every successful write also carries `Last-Modified` and
> `Vary: Host, Range`.

**R-RW-100 Tags in SSE.** The `etag` property of a mutation event (area
`sse`) MUST be the same string (including quotes) as the `ETag` header a
GET/HEAD of the mutated element would return, because beta-js stores it as
`element.etag` and reuses it in `If-Match` ([BJS-S]:247-249).
Evidence: client-source. Confidence: medium.

> **Superseded by live observation (2026-09-28).** The event `etag` is the
> **document's** new tag, without quotes (R-SSE-13 as reconciled).
> R-RW-85 accepts unquoted tags. Case:
> `sse.mutation.put-etag-matches-response`.

---

## 14. Caching headers

**R-RW-101 Static assets.** Blobs whose stored type is CSS
(`text/css`), JavaScript (`text/javascript`, `application/javascript`,
`application/x-javascript`), an image (`image/*`) or a font (`font/*`,
`application/font-woff*`, `application/vnd.ms-fontobject`) are served with
`Cache-Control: public, max-age=300`. Other blobs (JSON, PDF, text, archives)
get no `public` caching directive from this rule.
Evidence: documented ([GET] §Caching gives `public, max-age=300`; [UP]
§Serving says `public, max-age` without a number — §23 C-6). Confidence:
high (CSS/JS/images/fonts), medium (exact type list).

**R-RW-102 HTML pages.** Shareable composed HTML "uses a much shorter
shared-cache floor"; the live server sent no `Cache-Control` on a fragment
response. pagelike MUST NOT send `public, max-age=300` for HTML/XML and SHOULD
omit `Cache-Control` for shareable pages.
Evidence: documented + live-observed. Confidence: medium. Q-14.

> **Superseded by live observation (2026-09-28).**
> - Composed (templated or bound) pages are served
>   `Cache-Control: public, max-age=5`, with a weak ETag.
> - Static documents and fragments carry no `Cache-Control`.
> - Pages under R-RW-103 stay `private` (keep-documented-security;
>   `rw.reqdoc.auth-read-is-private`).
>
> Q-14 settled.

**R-RW-103 User-varying pages are private.** A response whose composition read
per-requester data — the Request Document (any fragment), `request.auth.*`,
`request.headers.*`, or a transient element — is served
`Cache-Control: private` (both whole-document and fragment responses).
Reading only `request.path`, `request.method`, `request.query` keeps it
shareable.
Evidence: documented ([RD] §Caching note; [TR]; composing-pages caching
notes). Confidence: high.

> **Kept despite live observation (2026-09-28).** Live PageLove serves a page
> that reads `request.auth` as `public, max-age=5`. pagelike keeps `private`
> (keep-documented-security). `rw.reqdoc.auth-read-is-private.live` measures
> the divergence.

**R-RW-104 SSE.** Event streams are never cached (`cache-control: no-cache`;
area `sse`).

---

## 15. Uploads and blobs

**R-RW-110 Byte fidelity.** A blob stores the request body byte-for-byte and a
GET returns exactly those bytes with the stored `Content-Type`, `ETag`
(R-RW-98), `Last-Modified`, and, for static types, `Cache-Control` (R-RW-101).
No charset is added or removed.
Evidence: documented ([UP] §Uploading example: PUT `application/json` →
GET `content-type: application/json` + identical body). Confidence: high.

**R-RW-111 Raw uploads only.** Uploads are a single PUT whose body is the file
itself; the server does not parse `multipart/form-data` for uploads (a
multipart body would be stored verbatim as a blob of type
`multipart/form-data; boundary=…`).
Evidence: demo-source ([SHOP]:489 "No multipart anywhere"; [KAN], [ATS]
uploads). Confidence: high.

**R-RW-112 Opaque.** Blobs are never composed (no directives, bindings,
templates, Liquid, includes are evaluated inside them) and are not
selector-addressable (R-RW-29).
Evidence: documented ([UP] §Serving). Confidence: high.

**R-RW-113 Accept is ignored.** R-RW-39. Confidence: high.

**R-RW-114 Size.** Blobs are subject to the body-size cap (§16) only; the
kanban app reports 8 MB uploads working ([KAN]:1540). Confidence: high.

**R-RW-115 POST to a directory.** Out of scope until Q-16 is settled
(cross-area `composing`).

---

## 16. Request-body size cap

**R-RW-120 Cap.** Every write request body (any method carrying a body) is
limited by an operator cap, default 1 GiB (1073741824 bytes), overridable per
site. A body exactly at the cap is accepted (inferred: "exceeds").
Evidence: documented ([PUT] §Request body size limit). Confidence: high.
pagelike config: per-site setting `max_request_body_bytes` (harness
`site.settings.max_request_body_bytes`).

**R-RW-121 Declared too large.** If `Content-Length` exceeds the cap the
server answers `413 Content Too Large` immediately without reading the body,
before authorization and before any other processing, and closes the
connection (`Connection: close` on HTTP/1.1). The body is HTML carrying an
`https://pagelove.org/Error` item with `status` (`413`) and `message`
properties. Nothing is written.
Evidence: documented. Confidence: high (413, microdata type/props), medium
(ordering before auth).

**R-RW-122 Streaming bodies.** A body without an accurate `Content-Length`
(chunked) is read until it exceeds the cap, then `413` as above; nothing is
written. A body shorter than its `Content-Length` (connection dropped) is
`400` and nothing is written ([RC] §Error cases for templated POST;
generalised by pagelike).
Evidence: documented / inferred. Confidence: high / medium.

---

## 17. Authorization interplay (absence vs denial)

Authorization rules themselves are area `permissions`. This section fixes how
authorization outcomes combine with 404/416 for the methods in this area.

**R-RW-125 401 vs 403.** A denial is `401 Unauthorized` when the request is
unauthenticated and `403 Forbidden` when authenticated. The `401` body is the
documented `https://pagelove.org/1.0/Error` document including a login link
(R-RW-130).
Evidence: documented ([MOVE] §Denials; [AZR] §Testable examples show 401 for
anonymous PUT/DELETE/GET), demo-source ([DEMO] check-live.py:59-61 expects
anonymous 401). Confidence: high.

**R-RW-126 Resource-level denial first.** If no rule (at any granularity) can
grant the method on the path to the actor — including a denied read of the
whole document — the request is refused with 401/403 before existence is
revealed: a missing document in an unreadable area is 401/403, not 404; a
selector write when the actor holds no grant for that method on the path is
401/403 whether or not the target exists.
Evidence: inferred from [PUT]/[POST]/[DELETE] "there is nothing there is
itself information about the page". Confidence: medium.

**R-RW-127 Selector absence vs denial.** For a selector operation on an
existing document where the actor passes R-RW-126:
1. If the actor may not read the document (or the region the selector
   addresses), any selector write whose target is absent **or** present but
   not writable is refused identically (401/403). Absence is never reported
   as 416 to someone who cannot read the page.
2. Else if the selector matches nothing → `416` (for GET/HEAD, PUT, DELETE,
   POST append/prepend). The 416 is re-checked against the live committed
   document (no stale cache may produce it). POST `before`/`after` with an
   absent anchor is refused (R-RW-75).
3. Else the first match is authorized (element-level rules); a matching Deny,
   or no Allow at the top specificity, → 401/403.
Evidence: documented ([PUT]/[DELETE]/[POST] "Absence and denial are
distinct" notes; [GET] §Error cases). Confidence: medium (exact ordering),
high (the invariants "absent+readable → 416", "exists+denied → 401/403",
"unreadable → never 416").

> **Superseded by live observation (2026-09-28), in step 2.**
> - POST `before`/`after` with an absent anchor is `416` like `append`
>   (R-RW-75 as reconciled).
> - When the method is granted to the actor only by selector-scoped rules,
>   the `416` comes from the authorization layer: read vocabulary with
>   `charset=utf-8`, `Content-Range: selector */`, no `Vary`.
> - When it is granted at document level, the answer is the write path's
>   selector-no-match `416`.
>
> The invariants hold, including "unreadable → never 416", which pagelike
> also keeps for MOVE (keep-documented-security,
> `protocol.move-authz.fail-closed-when-unreadable`).

**R-RW-128 Which elements are authorized.** Single-target reads and all
writes are authorized against the first match only; all-matches reads
(multipart/mixed, JSON-LD) against every match, and one denied match refuses
the whole request (decided over the full match set, independent of
pagination).
Evidence: documented ([AZR] §Selectors that match several elements).
Confidence: high.

> **Kept despite live observation (2026-09-28).** Live PageLove does not let a
> selector-scoped Deny override a resource-level Allow, so it serves denied
> elements, both as the first match and inside all-matches answers. pagelike
> keeps this rule (keep-documented-security). The `.live` siblings of
> `rw.authz.denied-fragment-read` and the two `authz.multimatch.*` cases
> measure the divergence.

**R-RW-129 Default-GET.** GET/HEAD with no matching rule is allowed or denied
according to the site's default-GET mode (area `permissions`); writes are
default-deny.

---

## 18. Error documents

**R-RW-130 Shape.** Every 4xx/5xx from this area carries an HTML error
document (`Content-Type: text/html; charset=utf-8`) whose root item describes
the failure. PageLove currently uses several vocabularies; pagelike MUST
reproduce the observed/documented shape for the statuses where one is known
and use the WebDAV/413 shape elsewhere:

| Status | itemtype | properties | Source |
|---|---|---|---|
| 401 (and 403) | `https://pagelove.org/1.0/Error` on `<body>` | `message` (`<p>`), `resource` (`<dd>`, request path); `<title>` and `<h1>` `401 Unauthorized`; a `<a href="/-pagelove/oidc/login">Log in</a>` link | documented [AZR], live-observed [LIVE] `query-sessel` (no login link there) |
| 416 | `http://pagelove.org/Error` (note `http`) on `<body>` | `name` (`<h1>` "Range Not Satisfiable"), `statusCode` (`<meta content="416">`), `description` (`<p>`, e.g. `HTML parsing error: No elements matched selector: <css>`); `<title>416 Range Not Satisfiable - Error</title>` | live-observed [LIVE] `missing-fragment` |
| 422 shape violation | `https://pagelove.org/ConstraintViolation` | `name`, `statusCode`, `description`, `violations` → `Violation{constraintSelector, failedConstraint, message}` | documented [SC] (area `modeling`) |
| 413 | `https://pagelove.org/Error` | `status`, `message` | documented [PUT] |
| all others (400, 403 reserved, 404, 412, 422 other, 501, 5xx) | `https://pagelove.org/Error` on an `<article>` or `<body>` | `status` (meta), `kind` (meta, e.g. `UntranslatableWrite`, `MoveCrossResource`), `type` (nested item `https://dombase.pagelove.team/ns/error/<kind>`), `message`, optional `detail` | documented [WD] §When something goes wrong (same names as the HTTP interface) |

Clients read only the status (and sometimes `text()` for display: [DEMO]
demo-01 app.js:44-57 greps the body for "uniqueness"). Harness cases assert
only status, content type and the documented microdata properties.
Evidence: as listed. Confidence: medium. §23 C-3.

> **Superseded by live observation (2026-09-28).** PageLove chooses the
> vocabulary by **pipeline stage**, not by status. pagelike reproduces each
> shape, with `Content-Type: text/html` (no charset) unless noted:
>
> | Stage | Shape |
> |---|---|
> | Read path: 400/404/416/422 on GET, HEAD and QUERY | A page `<title>NNN Reason - Error</title>` with `<body itemscope itemtype="http://pagelove.org/Error">` holding `name` (h1), `statusCode` (meta) and `description` (p). `Vary: Host, Range`. Messages such as "Document not found: <path>", "HTML parsing error: No elements matched selector: <css>", "Invalid path: …". |
> | Write path: 404/422/500/501 on PUT, POST, DELETE and MOVE | A bare `<div itemscope itemtype="https://dombase.pagelove.team/ns/error/<Kind>"><ul itemprop="problems"><li itemprop="problem"><span itemprop="message">…</span></li></ul></div>`. Kinds seen: `NotFound`, `InvalidPath`, `MoveMissingHeaders`, `MoveCrossResource`, `Internal`. The parentless-anchor 422 uses the itemtype `https://pagelove.org/Error`. `Vary: Host, Range`. |
> | Write 416 | `http://pagelove.org/Error` with `statusCode`, wrapping a `selector-no-match` problems item. |
> | Authorization layer 416 (grant only selector-scoped) | The read-path page with `charset=utf-8`, `Content-Range: selector */` and no `Vary`. |
> | 412 | `http://pagelove.org/Error` holding one `message` span, "Precondition Failed: ETag does not match" (also "Document does not exist", "ETag matches If-None-Match", "resource already exists"). It carries the relevant `ETag`. |
> | 401/403 | Unchanged (R-PERM-55), `charset=utf-8`, no `Cache-Control`, no `Vary`. |
>
> See [LO-5](../compat/live-observations.md).

---

## 19. Request Document

**R-RW-135 Existence.** For every request a transient HTML document with root
item `https://pagelove.org/Request` on `<body>` exists for the duration of
request processing. It is addressable only from composition (resource
bindings `r:`, includes) — e.g. `[itemtype*=Request] …` — and is **not**
addressable over HTTP; it is never persisted.
Evidence: documented ([RD]). Confidence: high.

**R-RW-136 Shape.**

```html
<!doctype html>
<html lang="en"><head></head>
<body itemscope itemtype="https://pagelove.org/Request">
  <meta itemprop="path" content="/index.html">          <!-- request path, no query -->
  <meta itemprop="method" content="GET">
  <meta itemprop="query" content="foo=bar">             <!-- raw query string, "" if none -->
  <meta itemprop="body" content="">                     <!-- raw request body -->
  <section itemprop="query" itemscope itemtype="https://pagelove.org/Request/HTTP/Query">
    <meta itemprop="foo" content="bar">                 <!-- one meta per parsed parameter -->
  </section>
  <section itemprop="headers" itemscope itemtype="https://pagelove.org/Request/HTTP/Headers">
    <meta itemprop="accept" content="…">                <!-- one meta per header, lower-case name -->
  </section>
  <section itemprop="auth" itemscope itemtype="https://pagelove.org/Authorization">
    <section itemprop="claims" itemscope itemtype="https://pagelove.org/Claims">
      <meta itemprop="email" content="…"> <meta itemprop="name" content="…">
      <meta itemprop="sub" content="…">   <meta itemprop="picture" content="…">
    </section>
    <meta itemprop="username" content="<sub>">
    <meta itemprop="role" content="<email>">            <!-- one meta per role/group -->
  </section>
</body></html>
```

Note the two `query` properties (raw string and parsed item). Repeated query
parameters produce repeated metas (inferred). Anonymous requests have an
empty or absent `auth` section (inferred).
Evidence: documented ([RD] §Shape, §Fields). Confidence: high (shape),
low (repeats/anonymous).

**R-RW-137 `request` variable.** The same data is available without a
selector as the `request` context variable in expression bindings and Liquid:
`request.path`, `request.method`, `request.query.<name>`,
`request.auth.claims.email`, … (and, in Liquid, `request.body.<field>` for
url-encoded form posts: [POLL] templates/new-poll.html). A Liquid page
rendering `Method: {{ request.method }}` / `Path: {{ request.path }}` returns
`Method: GET` / `Path: /<page>` on GET.
Evidence: documented ([RD] §Examples), demo-source. Confidence: high.
Details belong to areas `composing`/`liquid`/`sessel`.

**R-RW-138 Caching.** R-RW-103.

---

## 20. Client contract summary (what official clients send and need)

**R-RW-140 Requests sent by beta-js** ([BJS-P]@c204746):
- Every `PLElement` request: `Range: selector=<generated>` (no space) — :263.
  Generated selectors: `#<CSS.escape(id)>` if the element has an id; else a
  `>`-joined path anchored at the nearest ancestor with an id, using
  `[itemprop="…"]`, `tag[itemprop="…"]` or `tag:nth-child(n)` steps
  (:8-64). Servers must support `:nth-child`, attribute selectors with
  quoted values, `#id` with CSS escapes, and `>` combinators.
- Body writes: `Content-Type: text/html;charset=UTF-8` (:264); body is the
  element's `outerHTML` minus `data-pl-*` and `contenteditable` attributes
  (:73-90).
- `If-Match: <element.etag>` on GET/PUT/DELETE (not POST) when the element
  has a string tag (:265-268) — note: also on **GET** (a conditional GET with
  `If-Match`; pagelike MUST evaluate it per RFC 9110: 412 when it fails).
  **Superseded by live observation (2026-09-28):** PageLove ignores
  `If-Match` on GET and HEAD (206 despite a stale tag), and so does pagelike
  (`rw.cond.get-if-match-stale`; Q-23 settled).
- `HEAD` + `Range` to fetch element tags lazily (:484-487).
- OPTIONS with `Prefer: return=representation`, `Accept: multipart/mixed`
  (:180-189; area `protocol`).
- `pagelove.mjs` fallback PUT: `Range: selector=#<id>`,
  `Content-Type: text/html` ([BJS-M]:944-953).
- MOVE: `Range`, `Destination: <absolute URL>`, `Destination-Range:
  selector=#…; placement=…`, optional `If-Match` ([BJS-C]:767-803; area
  `move`).

**R-RW-141 Responses beta-js depends on.**
- Any response's `ETag` header (any status, including 412) replaces
  `element.etag` (:296-297).
- GET 2xx body = exactly one node after trim (:407-416).
- POST 2xx body = exactly one node (the inserted child) (:350-354), plus the
  ETag/Content-Location rule (R-RW-73).
- DELETE 2xx → local removal; 416 treated as already gone by component
  helpers ([BJS-C]:936).
- MOVE 405 → fall back to POST+DELETE (area `move`).

---

## 21. Status code summary

| Situation | GET/HEAD | PUT | POST | DELETE |
|---|---|---|---|---|
| whole resource ok | 200 | 201 create / 200 replace | (see R-RW-78) | 204 |
| selector ok | 206 | 206 | 206 | 204 |
| not modified | 304 | — | — | — |
| directory w/o slash, readable index | 301 | — | — | — |
| resource missing | 404 | 404 (selector) | 404 | 404 |
| selector matches nothing (readable) | 416 | 416 | 416 (append/prepend) | 416 |
| before/after anchor absent | — | — | 401/403 | — |
| parentless before/after, bad placement | — | — | 400 | — |
| malformed/unparsable selector | 400/422 | 400/422 | 400/422 | 400/422 |
| selector on blob | 422 | 422 | 422 | 422 |
| validation failed | — | 422 | 422 | 422 |
| precondition failed | 412 (If-Match) | 412 | 412 | 412 |
| denied | 401 anon / 403 auth | same | same | same |
| reserved `/.pagelove/` | (404) | 403 | 403 | 403 |
| body over cap | — | 413 | 413 | 413 |

---

## 22. Cross-area dependencies

- **X-1 selectors** (`internal/selector`): CSS L4 + PageLove extensions,
  first-match document order, UTF-8 header decoding; `:value-*` share
  §8's value function.
- **X-2 dom** (`internal/dom`): context-aware fragment parsing (R-RW-65);
  serialization that preserves source structure without implied elements
  (R-RW-21); document-rooted stable selectors for multipart parts (R-RW-31);
  XML dialect.
- **X-3 composing**: composition before selection (R-RW-16); write routing
  to origin for stamped/included elements and route templates (R-RW-77);
  transient-element PUT identity rule and `Cache-Control: private`
  (R-RW-103); `xmlns:` stripping; Resource Creation via POST + `<base>`
  (R-RW-78); parameterized routes (resolved for whole-document GET, not for
  whole-document writes; selector GET on a route path → 404 per [PR]).
- **X-4 permissions**: rule matching, default-GET mode, specificity; the
  absence-vs-denial ordering here (§17) must use the same evaluator.
- **X-5 modeling**: schema/shape/transition validation on writes (422, and
  412 races for transitions).
- **X-6 sse**: every committed write emits a `mutation` event carrying
  method, selector, element tag (R-RW-100), path, host, body, placement;
  writes read the `Pagelove-Connection` header for echo suppression.
- **X-7 protocol**: `Accept-Ranges` advertisement, OPTIONS, QUERY
  (multipart format shared with R-RW-31), WebDAV (authoring writes share
  R-RW-60 content-type resolution and store; WebDAV bypasses transition
  constraints).
- **X-8 move**: shares placement vocabulary, If-Match semantics, reserved
  namespace, 416/404 conventions.
- **X-9 store**: stored-version tags, content-addressed blobs, per-site write
  mutex that makes preconditions commit-atomic.
- **X-10 harness**: needs `site.settings.max_request_body_bytes`, must not
  add a default `Content-Type` to bodies, and must support the `microdata`
  expectation list format used by these cases (see implementation notes of
  the summary).

---

## 23. Contradictions and compatibility decisions

- **C-1 Content-Range spelling.** Docs: `Content-Range: selector=h1` ([GET],
  [IDX], [PAG], [OPT]); live and QUERY docs: `selector h1` / `selector <path>`;
  clients accept both and say the server now emits the space form
  ([BJS-P]:194-202, [PP]:131-137). **Decision:** emit `selector <css>`.
  Harness: `rw.get.content-range-space` (live-observed) and
  `rw.get.content-range-equals` (`status: disputed`).
- **C-2 Conditional writes in practice.** Docs promise If-Match semantics;
  the kanban app's comment says conditional writes rejected every request,
  even `If-Match: *`, on the build it tested ([KAN]:29-38). **Decision:**
  implement the documented semantics; treat the comment as historical.
- **C-3 Error vocabularies.** `https://pagelove.org/1.0/Error` (401),
  `http://pagelove.org/Error` with `statusCode` (416 live),
  `https://pagelove.org/Error` with `status`/`kind` (413, WebDAV),
  `https://pagelove.org/ConstraintViolation` (422). **Decision:** per-status
  replication (R-RW-130); clients rely on status codes only.
- **C-4 Vary.** Docs: `Vary: Accept` on all responses; live 416 omitted
  `Accept`. **Decision:** always include `Accept` (and `Range`).
- **C-5 JSON-LD single item.** Current docs: flat object for one item,
  `@context` `http://schema.org/` (trailing slash). Older server prompt
  ([PROMPT] http_api): `@graph` with one item and `@context`
  `http://schema.org` (no slash). **Decision:** current docs.
- **C-6 Static-asset max-age.** [GET]: `public, max-age=300`; [UP]:
  `public, max-age` (no value). **Decision:** 300.
- **C-7 Accept-Ranges.** Docs: `Accept-Ranges: selector, bytes` on every HTML
  response (AFD rewrites to `bytes`); the live 206/416 responses carried no
  `Accept-Ranges` at all. **Decision (area protocol):** emit
  `selector, bytes`.
- **C-8 Status of whole-document PUT.** 201 on create (documented); replace
  status undocumented. **Decision:** 200.
- **C-9 POST to a directory.** [UP] says POST to a directory creates a blob
  with a generated name "the same way it creates HTML resources — see
  Resource Creation", but [RC] describes POST to a *template document* with
  `<base href>`. **Decision:** not implemented until probed (pagelike: 501
  with an Error item for POST without `Range` to a path ending in `/`).
- **C-10 Invalid selector status.** MOVE/QUERY: 422; old server: 400;
  live 416 body wording ("HTML parsing error: No elements matched
  selector") suggests selector evaluation errors may surface as 416.
  **Decision:** 422 for unparsable selectors.
- **C-11 QUERY composed example.** [QRY] claims the edge QUERY sees the
  composed page but its example shows raw `{{ 2 | plus: 3 }}`; [PAG] says
  selector reads see the composed page. **Decision:** reads see the
  composed view (R-RW-16).
- **C-12 Combined vs individual pages.** Identical except
  `Uploading-Files`: the individual page has a stray line
  ``Given `/data.json` contains`` before the PUT example (no semantic
  difference).
- **C-13 Error reason phrase.** 422 is "Unprocessable Entity" in most pages,
  "Unprocessable Content" in [UP]/[QRY] (RFC 9110 name). Status code is what
  matters; pagelike uses RFC 9110 reason phrases.

---

## 24. Open questions for live probing

Each probe uses a disposable host `H` with anonymous access and rules under a
fresh prefix `/p`. `→` shows what to record.

- **Q-1 Element vs document tags.** PUT `/p/t.html` =
  `<html><body><h1>A</h1><p id=x>B</p></body></html>`. `HEAD /p/t.html`
  `Range: selector=h1` → e1; `HEAD … selector=#x` → e2 (are e1 and e2
  different? do they embed the whole-GET tag?). `PUT … Range: selector=#x`
  body `<p id=x>C</p>`. `HEAD … selector=h1` → e1' (== e1?). `PUT … selector=h1
  If-Match: e1` → 206 or 412? Also `GET /p/t.html` → whole tag; `PUT
  Range: selector=h1 If-Match: <whole tag>` → 206 or 412?
- **Q-2 Whole PUT replace status.** PUT a new doc (201?), PUT again → 200,
  201 or 204; is the body echoed? Is `ETag` returned on each?
- **Q-3 Selector PUT body edge cases.** PUT `Range: selector=h1` with (a)
  empty body, (b) `text only`, (c) `<h1>1</h1><h2>2</h2>` → status and
  stored result.
- **Q-4 POST response headers.** POST `Range: selector=ul` body
  `<li>n</li>` → is there `Content-Location`? `Content-Range` value? Which
  element's tag is `ETag` (compare with HEAD `selector=ul` and HEAD of the
  new li)? Body for a multi-node POST body?
- **Q-5 Placement parsing.** `placement=PREPEND`, `placement=inside`,
  `placement=` on PUT/DELETE, extra spaces `selector=ul ;placement=after` →
  statuses.
- **Q-6 Malformed ranges.** `Range: selector=`, `Range: selector=h1[`,
  `Range: Selector=h1`, `Range: selector = h1` on GET and PUT → statuses.
- **Q-7 Content-Range on writes.** PUT/DELETE selector responses: is
  `Content-Range` present and spelled with space or `=`?
- **Q-8 Accept q-values.** GET with `Accept: text/html;q=0.5,
  application/ld+json;q=0.9`; `Accept: */*`; `Accept: application/json`;
  `Accept: application/ld+json;q=0` → representation chosen.
- **Q-9 Selector + JSON-LD / multipart.** `GET Range: selector=li` with
  `Accept: application/ld+json` and with `Accept: multipart/mixed` on a page
  with 1 and with 3 matches → status (200/206), `Content-Range`, part
  headers, flat vs `@graph`; 0 matches → 416 or empty 200?
- **Q-10 Context split.** Items with `itemtype="http://ex.org/vocab#Thing"`,
  `itemtype="http://ex.org/"`, two itemtype tokens, no itemtype →
  `@context`/`@type`.
- **Q-11 No items.** JSON-LD for a page without microdata → body.
- **Q-12 URL values.** `<a itemprop=u href="/x">`, `<img itemprop=i
  src="rel.png">` in JSON-LD → raw or absolute?
- **Q-13 Text normalization.** `<span itemprop=n>  A \n  B </span>` →
  JSON-LD value; repeated `itemprop` → array?; nested item object shape;
  `itemid` → `@id`?
- **Q-14 Cache-Control on HTML.** Whole GET of a static HTML page and of a
  Liquid page reading `request.auth` → `Cache-Control` values;
  `Last-Modified` present on whole GET?
- **Q-15 entries on GET.** `GET` with `Range: entries=0-1` on an HTML page and
  on a page with a `p:paginate` list.
- **Q-16 POST to a directory.** `POST /p/uploads/` with
  `Content-Type: image/png` and bytes → status, `Location`, generated name.
- **Q-17 Reserved namespace reads.** `GET /.pagelove/` and
  `GET /.pagelove/anything.html` → status (read-only probe).
- **Q-18 Whole DELETE with If-Match: \*** on a missing path → 404 or 412;
  `If-None-Match` on DELETE.
- **Q-19 Absence vs denial without any method grant.** With GET allowed and
  no DELETE rule: `DELETE Range: selector=#absent` → 401 or 416?
- **Q-20 HEAD Range on missing selector** → 416 with no body; HEAD of a
  denied fragment → 401.
- **Q-21 Directory redirect details.** `GET /p/blog` (index present) →
  `Location` absolute or path-absolute; query encoding preserved;
  `HEAD /p/blog` → 301; `PUT /p/blog/` → status.
- **Q-22 Byte ranges.** `GET /p/a.txt` with `Range: bytes=0-3` → 206 with
  `Content-Range: bytes 0-3/<n>`? On an HTML page?
- **Q-23 Conditional GET with If-Match** (beta-js sends it on GET): stale tag
  → 412?

---

## 25. Live reconciliation 2026-09-28

A live PageLove run on 2026-09-28 (host `live-test-host`,
`harness/observations/live-2026-09-28/`), confirmed on 2026-09-29
(`harness/observations/live-2026-09-29-confirm/`), failed 16 reading-writing
cases. [docs/compat/decisions.md](../compat/decisions.md) has one row per case,
giving the observed behaviour, the documented claim, the decision and the
rationale. [docs/compat/live-observations.md](../compat/live-observations.md)
(LO-4 onward) records the details mined from the passing cases.

**Superseded by live observation.** Each requirement below is marked in place
with the new behaviour:

| Requirement | New behaviour |
|---|---|
| R-RW-4 | An unreadable directory is a denial (401/403 naming the slash-less path), not a 404. The 301 has no body and is `private, max-age=3600`. |
| R-RW-17 | An invalid read selector is 416 in the read vocabulary. |
| R-RW-30 | XML is not selector-addressable on the public plane (422). |
| R-RW-31 | New all-matches multipart wire format, status 200. |
| R-RW-37 | PageLove's `Vary` sets. |
| R-RW-49 | JSON-LD key order: sorted. |
| R-RW-71 | An unknown placement is append. |
| R-RW-73 | The POST tag is the child's tag. |
| R-RW-74 | Parentless anchor: 422. |
| R-RW-75 | Absent before/after anchor: 416. |
| R-RW-80 | No `Content-Range` on DELETE. |
| R-RW-81/82 | A whole DELETE of a missing path is 204. |
| R-RW-88 | 304 headers. |
| R-RW-89 | Selector writes compare with the element tag only. |
| R-RW-96 | Composed pages: weak tag, `public, max-age=5`. |
| R-RW-97 | Tag formula `"<h>-<h>-<version>"`. |
| R-RW-99 | Tags carried by write responses. |
| R-RW-100 | The SSE tag is the document tag, unquoted. |
| R-RW-102 | Composed pages cacheable for 5 s. |
| R-RW-127 | Step 2 for before/after, and the authorization-layer 416. |
| R-RW-130 | Error vocabularies by pipeline stage. |
| R-RW-140 | `If-Match` on GET is ignored. |

The status table in §21 reads accordingly:
- whole DELETE of a missing path: 204;
- before/after with an absent anchor: 416;
- parentless anchor: 422, and an unknown placement is appended;
- malformed selector on reads: 416;
- selector on XML: 422.

**Kept despite live divergence.** Each is marked in place, and a `.live`
sibling measures PageLove:
- R-RW-103: pages that read `request.auth` stay `private`
  (keep-documented-security).
- R-RW-128: a selector-scoped Deny overrides a resource-level Allow, and
  all-matches reads are all-or-nothing (keep-documented-security).
- R-RW-47 (`itemref`) and R-RW-49 (multi-token `itemprop`) (keep-standard).

**Kept as documented supersets:**
- C-4 as far as `Accept-Ranges` goes, and C-7: `Accept-Ranges: selector, bytes`
  on HTML responses, which PageLove omits.
- R-RW-21 byte preservation. PageLove re-serializes stored HTML on write
  (LO-9); pagelike keeps the stored bytes until a serializer with PageLove's
  output is available (deferred).

**Settled open questions:**

| Question | Answer |
|---|---|
| Q-1 | Element tags are element-specific and include the document version. |
| Q-4 | No `Content-Location`; the tag is the child's. |
| Q-5 | An unknown placement is append. |
| Q-6 | 416 on reads. |
| Q-14 | `public, max-age=5` on composed pages. |
| Q-21 | The Location is path-absolute and keeps the query. |
| Q-23 | `If-Match` on GET is ignored. |
