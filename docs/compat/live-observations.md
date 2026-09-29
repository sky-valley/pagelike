# Live observations of PageLove

Read-only and (when credentials exist) write probes against real PageLove.
Each entry: date, target, request, what was seen, what it settles.
Hosts probed read-only are PageLove's own public demos; no writes were made
to hosts we do not own.

## LO-1 (2026-09-28) Whole-document GET serves stored source bytes

- Target: `https://barn-hair-4926.onpagelove.com/` (PageLove's shop demo,
  deployed from github.com/pagelove/pagelove-shop @ d887054).
- Requests: anonymous `GET` of `/data/products/cap.html`, `/constraints.html`,
  `/partials.html`, `/css/shop.css`; compared with `site/<path>` in the repo.
- Seen: `shop.css` byte-identical. The three HTML documents are byte-identical
  to the repository source **except** that PageLove inserts a fixed-position
  "Powered by Pagelove" `<div>` immediately before `</body>` (free plan
  branding). No implied `<head>`, no attribute re-quoting, no whitespace
  changes, no `<br>` normalization.
- Settles: decision 0003 §6 (source preservation) — PageLove keeps and serves
  the stored bytes for documents without composition changes. pagelike must
  store raw uploaded bytes and splice element edits into them rather than
  re-serializing whole documents.
- pagelike decision: do **not** inject branding (it is a plan feature, not a
  runtime contract). Harness normalization strips the PageLove footer
  (`div` with `Powered by … Pagelove` immediately before `</body>`).
- Headers seen on the 200 (volatile ones omitted): see below.

```
HTTP/2 200 
content-type: text/html
etag: "6117ee90571d015fd63c481ba4acd9fe693d7f032dc89ca497d620523e199a77"
last-modified: Fri, 04 Sep 2026 14:49:29 GMT
vary: Host, Range, Accept
x-budget-consumed-ops: 1
x-budget-consumed-memory: 761
x-budget-consumed-time: 85
x-storage-consumed: 543271
x-cache: CONFIG_NOCACHE

```

## LO-2 (2026-09-28) Entity tags

- Target: same host as LO-1.
- Blob `/css/shop.css`: `ETag: "be8430dd…292f"` = SHA-256 of the stored
  bytes (matches `shasum -a 256` of the repo file). Confirms the documented
  "content-hash strong ETag" for blobs; pagelike uses the same scheme, so
  blob ETags are byte-for-byte equal to PageLove's.
- HTML `/data/products/cap.html`: `ETag: "6117ee90…9a77"` is **not** the
  SHA-256 of the stored bytes (`3450ddbe…`) nor of the served bytes with the
  footer (`df68512d…`). HTML document tags are opaque, internally derived
  values; only their change/equality behaviour can be compared.
- `x-budget-consumed-memory: 761` equals the stored document size (761
  bytes), consistent with the footer being added after storage/budgeting.

## LO-3 (2026-09-28) Disposable host, WebDAV and rule propagation

- A disposable host (`live-test-host` in the observations) was created with an account API key via
  `POST /console/templates/new-host.html` (form field `org`) → `301` with
  `Location: /organizations/<org>/<hid>.html` (templated resource creation
  answers 301 + Location, per Resource Creation step 6). The redirect target
  is `403` to the key; host records are read from `/console/index.html`
  (`urn:Host` items). New host: plan `free`, `default-get-authz-mode: allow`.
- `PROPFIND /` Depth 1 on an empty host → `207`,
  `Content-Type: application/xml; charset=utf-8`, `Vary: Depth`,
  `ETag: "sha256:<64 hex>"`; the collection's `<D:getetag>` equals the
  header tag; the root entry has no displayname/getlastmodified.
- `MKCOL` → `201`; WebDAV `PUT` of a new HTML file → `201`.
- Anonymous selector `POST` with no rules → `401`. After uploading an
  AuthorizationRule document over WebDAV, the very next request (t+0 s) is
  allowed (`206`): **authorization rules take effect immediately**; the
  documented 60 s propagation applies to transition constraints, not rules.

## LO-4 (2026-09-28) Entity-tag formulas

- Target: disposable host `live-test-host`. Full run in
  `harness/observations/live-2026-09-28/`, confirmation runs in
  `live-2026-09-29-confirm/`.
- **Element (fragment) tags.** A fragment's tag is
  `"<sha256 of the served fragment>-<the same hash>-<document version>"`.
  - The version is 1 after upload and grows by one with every write.
  - Any write to a document therefore changes every element tag in it.
- **Tags on write responses:**
  - A selector POST answers the inserted child's tag
    (`rw-post-etag-is-anchor-tag.json`: `sha256("<li>two</li>")` twice,
    version 2).
  - A selector PUT answers the replacement's tag, or the document's tag when
    the replacement no longer matches the request selector.
  - A selector DELETE answers the document's new tag.
  - A whole-document DELETE answers
    `"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"`
    (SHA-256 of nothing), even when nothing was there.
- **Whole-document tags.**
  - Static documents: opaque (LO-2).
  - Composed pages: weak, `W/"<sha256 of the served bytes>"`.
  - JSON-LD of a whole document: strong, the SHA-256 of the JSON body (13 of
    13 observations).
  - JSON-LD all-matches answers: weak, `W/"<sha256 of the JSON>"`.
- **Authoring-plane tags:**
  - PROPFIND: collections `"sha256:<hex>"`; file answers
    `"propfind:sha256:<hex>"`, with the content tag as `D:getetag`.
  - MKCOL 201: `"0000…0000"` (64 zeros).
  - QUERY answers: one quoted hash.
- **SSE:** the `etag` of a mutation event is the document's new tag, without
  quotes.
- **pagelike:** adopted every formula above byte for byte where the input is
  known (`engine.FragmentETag`, `EmptyETag`, `ComposedETag`,
  `WeakETagOf`). Static whole-document tags stay pagelike's content hash.

## LO-5 (2026-09-28) Error vocabularies by pipeline stage

The error body depends on where the request failed, not on its status:

| Stage | Body | Headers |
|---|---|---|
| Read path (GET/HEAD/QUERY 400, 404, 416, 422) | `<title>NNN Reason - Error</title>` and `<body itemscope itemtype="http://pagelove.org/Error">` with `name`, `statusCode`, `description`. 422 is "Unprocessable Entity". `"` is escaped as `&quot;` in the description. | `Content-Type: text/html`, `Vary: Host, Range` |
| Write path (PUT/POST/DELETE/MOVE 404, 422, 500, 501) | A bare `<div itemscope itemtype="https://dombase.pagelove.team/ns/error/<Kind>"><ul itemprop="problems"><li itemprop="problem"><span itemprop="message">…</span></li></ul></div>`. Kinds: `NotFound` ("document not found at request path: …"), `InvalidPath`, `MoveMissingHeaders`, `MoveCrossResource`, `Internal`. The parentless-anchor 422 is typed `https://pagelove.org/Error`. | `text/html`, `Vary: Host, Range` |
| Write 416 | `http://pagelove.org/Error` holding `statusCode` and a nested `selector-no-match` problems item. | `text/html`, `Vary: Host, Range` |
| Authorization-layer 416 (method granted only by selector-scoped rules) | The read-path page. | `text/html; charset=utf-8`, `Content-Range: selector */`, no `Vary` |
| 412 | `<div itemscope itemtype="http://pagelove.org/Error"><span itemprop="message">Precondition Failed: ETag does not match</span></div>`. Other messages: "Document does not exist", "ETag matches If-None-Match", "resource already exists". | The relevant `ETag`, `Vary: Host, Range` |
| 401/403 | The documented `https://pagelove.org/1.0/Error` page (no login link on this host). | `text/html; charset=utf-8` only: no `Vary`, no `Cache-Control`, no `ETag` (119 of 119 denials) |
| WebDAV | See LO-12. | |

**pagelike:** reproduces each shape (`errdoc.Read`, `Problems`, `NoMatch`,
`Precondition`, `RenderShort`, `RenderWrapped`). Harness cases assert only
the status and documented properties.

## LO-6 (2026-09-28) Header sets

Aggregated over every response of the run:

| Response | `Vary` | Other headers |
|---|---|---|
| HTML reads (200/206, HEAD too) | `Host, Range, Accept` | `Last-Modified` on 200s of stored documents only (not on 206, not on composed pages); no `Accept-Ranges` |
| All-matches reads (multipart 200, JSON-LD 206) | `Host, Range, Accept, Paginate` | |
| Whole-document JSON-LD | `Host, Range, Accept` | `Accept-Ranges: bytes`, `Content-Type: application/ld+json; charset=utf-8` |
| Blobs and XML documents | `Host, Range` | `Accept-Ranges: bytes`, the stored type verbatim (no charset added) |
| 304 | `Host, Range` | `ETag` and `Last-Modified`; no `Cache-Control`, no `Accept-Ranges` |
| Public-plane writes (PUT 200/201/206, POST 206, DELETE 204, MOVE 204) | `Host, Range` | `ETag` and `Last-Modified`; `Content-Range: selector <request selector>` on 206 only |
| OPTIONS 200/204/207 | `Authorization, Accept` | `Allow` on 200/204 only; no `Accept-Ranges`, no `Accept-Query` |
| SSE | none | `Content-Type: text/event-stream`, `Cache-Control: no-cache` |
| WebDAV | `Depth` on collection PROPFIND only | PUT: `Content-Type`, `ETag`, `Accept-Ranges: bytes` on 200. QUERY: `ETag`, `Last-Modified`, `Accept-Ranges: bytes` |

**pagelike:** adopted every `Vary` set (`engine.ReadVary`, `AllMatchVary`,
`WriteVary`), the 304 header set, the SSE head, and the WebDAV
PUT/QUERY/PROPFIND headers. Kept as documented supersets: `Accept-Ranges:
selector, bytes` on HTML and `Accept-Query` on OPTIONS (compat C-4/C-5,
reviewed in `docs/compat/decisions.md`).

## LO-7 (2026-09-28) Caching directives

- Static assets (CSS, JS, images, fonts) are `public, max-age=300` (7 of 7).
- Composed pages (Liquid, bindings) are `public, max-age=5`. That includes a
  page reading `request.auth`, which pagelike keeps `private` (security).
- Static HTML documents, fragments, writes and errors carry no
  `Cache-Control`.
- The slash-less directory redirect: `301`, `Location` path-absolute with the
  query kept, no body, `Cache-Control: private, max-age=3600`.
- **pagelike:** adopted all of these except the `request.auth` page.

## LO-8 (2026-09-28) Multipart and JSON-LD framing

- **Boundary:** `boundary` plus 32 alphanumerics, unquoted, last parameter.
- **No trailing CRLF** after the close delimiter on parts with bodies.
  OPTIONS header-only multiparts end in CRLF.
- **All-matches GET parts** (status 200), in order:
  - `Content-Disposition: attachment; filename="<document path>"`
  - `Content-Type`
  - `Content-Range: selector <request selector>`
  - `ETag`
  - `Content-Length`
- **Authoring QUERY parts**, in order:
  - `Content-Type`
  - `Content-Location`
  - `Content-Range: selector html:nth-child(1) > body:nth-child(1) > …` (an
    implied empty `<head>` is not counted)
  - `ETag`
  - `Last-Modified`
  - `Content-Length`
- **OPTIONS 207:** a first part with only `Allow` (document-level), then per
  selector `Content-Range: selector <css>` and `Allow` listing only that
  selector's selector-scoped grants (live: `Allow: POST, OPTIONS` for `ul`
  under a document-level GET rule).
- **JSON-LD:** keys of every object are sorted (so `@context` and `@type`
  come first), and there is no HTML escaping of `<`, `>` or `&`.
- **pagelike:** adopted all of it (`engine.MatchPart`, `QueryParts`,
  `NthChildPath`, `MultipartBody`, `NewBoundary`, `jsonLD`).

## LO-9 (2026-09-28) Stored HTML is re-serialized on write (deferred)

- A WebDAV PUT of deliberately irregular HTML reads back normalized:
  - `<!DOCTYPE html>`;
  - lower-cased, closed elements;
  - quoted attribute values;
  - bare empty attributes.
- The PUT response echoes that normalized form. Whether public-plane
  whole-document PUTs normalize the same way was not probed separately.
- This contradicts LO-1 only for sources that were not already in this form.
  The shop demo's sources were.
- **pagelike:** keeps the stored bytes (keep-standard, deferred). Matching
  needs a serializer option with PageLove's attribute output, and a decision
  for Liquid sources, whose markup would be rewritten.
  `protocol.webdav.get-is-byte-exact.live` measures it.

## LO-10 (2026-09-28/29) Sessel QUERY details

- `new Selector { "li" }.execute()` fails: "Sessel evaluation error: unknown
  function: execute". The docs present it as equivalent to `${li} from self`,
  which works.
- A directory URL is its `index.html`. A missing index is `404` "Document
  not found: <dir>/index.html".
- Errors (read vocabulary):
  - wrong or missing type: `400` "QUERY method requires Content-Type:
    text/sessel";
  - empty body: `422` "Invalid path: QUERY method requires a body containing
    the sessel expression";
  - parse error: `400` "Sessel compilation error: …";
  - `throw`: `400` "Sessel evaluation error: thrown value (at bytes a..b)".
- Results:
  - JSON results are `application/sessel+json` with `Vary: Host, Range`.
  - Selected elements are `{"$type":"element","$html":…}`, with no
    `$source`.
  - `Range: entries=a-b` answers `Content-Range: entries a-b/N;`, with a
    trailing semicolon.
- **pagelike:** the transport-level parts are adopted (`querySessel` errors,
  directory index). The evaluator is not implemented yet (cases need
  `sessel`).

## LO-11 (2026-09-28/29) SSE wire details

- **Stream head:** `Content-Type: text/event-stream`, `Cache-Control:
  no-cache`, no `Vary`.
- **Stream body:** it opens with `: connected` and a blank line, then
  `event: pagelove-connection` whose data is a UUID v4. Keepalive comments
  are `: ping`.
- **Event ids:** `v1~<first 12 hex of sha256(path)>.<unix ms>-<n>`.
- **Replay:**
  - An unparseable `Last-Event-ID` is ignored.
  - A well-formed old id replays what is retained.
  - `reset` is sent only once events after the id were pruned.
- **Suppression:** `Pagelove-Connection: <token>` suppresses exactly that
  stream, whatever session sends it.
- **Which writes emit events:**
  - Subscribing to a missing document opens a 200 stream.
  - WebDAV writes emit nothing.
  - A trigger's side-effect write emits nothing.
  - A write to an included partial is also delivered to the including
    page's stream.
  - A write through a composed page is announced on that page.
- **Event payloads:** a whole-document PUT event carries the whole document
  as `body`, and the event `etag` is the document tag, unquoted.
- **Trigger binding:** a Trigger document took effect only after about 60 s.
  Writes a few seconds after upload did not fire it, and a write 61 s after
  upload did. That matches the documented 60 s propagation for constraints,
  while rules bind immediately (LO-3).
- **pagelike:** adopted everything except includes and write-through
  (composition not implemented). Triggers bind on commit.

## LO-12 (2026-09-28/29) WebDAV plane

- **401:**
  - Anonymous requests get `WWW-Authenticate: Basic realm="WebDAV"`,
    `DAV: 1, 2`, `MS-Author-Via: DAV`, and
    `<article itemscope itemtype="https://pagelove.org/Error/Internal"><meta itemprop="status" content="401"><p itemprop="message">Authentication required</p></article>`.
  - For a rejected key, pagelike uses the message "Bearer token rejected",
    as the pagelove-dev skill quotes it. That case was not probed live.
- **Missing paths** get `Error/NotFound` short articles: "Not found: <p>" or
  "Collection not found: <p>".
- **Errors from the write pipeline** are wrapped in the documented
  `https://pagelove.org/Error` article (kind `Conflict` or `Internal`), with
  the public-plane document in `detail`.
- **MKCOL:**
  - create: `201` with a 64-zero ETag;
  - missing parent: `409` `ParentDirectoryMissing` (the parent is not
    created implicitly);
  - existing collection: `405`, with
    `Allow: OPTIONS, GET, HEAD, PUT, POST, DELETE, COPY, MOVE, LOCK, UNLOCK, PROPFIND, QUERY`
    and `DirectoryAlreadyExists` in the detail. The 405 was confirmed by a
    probe on 2026-09-29.
- **PUT** echoes the stored body (re-serialized, LO-9), with `Content-Type`
  and `ETag`, no `Last-Modified`, and `Accept-Ranges: bytes` on 200.
- **DELETE** of a missing path is `204`, with the empty-content tag.
- **PROPFIND:**
  - `Vary: Depth` on collections only.
  - Tags as in LO-4.
  - `D:` prefix; the root entry has no displayname or getlastmodified.
- **pagelike:** adopted all of it except LO-9.

## LO-13 (2026-09-28/29) Authorization quirks

The following hold on PageLove:
- **Resource matching:**
  - `/x/*` also matches `/x`, but not `/xy.html`.
  - Rules are matched against the canonical document path, so an `Allow` for
    `dir/` does not cover a request to `dir/`.
- **Rule extraction:**
  - Rules inside XML documents are ignored.
  - Rule fields are trimmed except `action`, so a padded `allow` or `deny` is
    ignored.
  - A `<table itemscope>` rule merges its rows. The first row's selector gets
    every row's methods, which is a privilege escalation (probe: DELETE on
    row 1's `h1` answered 204).
- **Deny rules:**
  - A selector-scoped Deny does not override a resource-level Allow at the
    same tier, for reads (single and all-matches) and writes. The reverse (a
    resource-level Deny over a selector Allow) holds.
  - OPTIONS ignores Deny rules: `* * deny` on the path still answers
    `Allow: GET, HEAD, OPTIONS` on a default-GET-allow host
    (`authz-method-options-not-gated.json`).
- **MOVE:** an absent MOVE source is `416` even to an actor who cannot read
  the document. With DELETE/POST granted only by selector-scoped rules, it
  is `401` even when readable.
- **Identity:**
  - The authoring API key on the public plane is anonymous (`401`).
  - `/auth/login` on a host without an identity provider is `404`.
- **pagelike:** adopted the first two resource-matching points for Allow
  rules, ignores rules in XML documents, ignores a padded `allow`, and
  adopted the selector-only MOVE refusal. It keeps the security-relevant
  behaviours: a padded deny is honoured, rows are split, a selector Deny
  wins, OPTIONS reflects denies, and absence is not revealed to non-readers.
  Each has a `.live` sibling (see `docs/compat/decisions.md`), except the
  OPTIONS-under-deny answer, which no case asserts.

## LO-14 (2026-09-28) Harness artifacts in the first live run

The first run's harness produced these failures, none of them PageLove
behaviour:
- It sent `as: author` requests to the WebDAV host even with
  `plane: public`, and sent the authoring key on anonymous WebDAV requests.
  That made "the API key is an identity" and "anonymous WebDAV is allowed"
  look like PageLove behaviour.
- It did not strip the plan footer before microdata checks on 401 bodies.
- It counted disputed cases (expected to fail live) and root cases as
  failures.
- It gave triggers too little time to bind.

All are fixed in `harness/` (see `docs/compat/decisions.md`, "Harness
artifacts").

## LO-15 (2026-09-29) Documents are built from tokens and stored serialized

Probes: `harness/cases/protocol/serialize-probe.yaml`, observations in
`harness/observations/live-2026-09-29-serialize/`. This resolves LO-9 and
revises LO-1 and decision 0003.

- **No HTML5 tree construction.** PageLove builds the tree straight from
  tokens:
  - no implied html, head, body or tbody: in a stored `<p>hi</p>`,
    `body` does not match and `:root` is the `<p>`; `table > tr` matches
    `<table><tr>`;
  - no implied end tags: `<p>a<div>b` nests the div, `<li>a<li>b` nests
    the second li, `<option>` nests;
  - no foster parenting: text and elements inside `<table>` stay in place;
  - an end tag closes the nearest open element of its name and everything
    opened after it (`<b><i>x</b>y</i>` gives `<b><i>x</i></b>y`), and an
    end tag with no open element of its name is dropped;
  - `<x/>` is an empty element for every name, and elements still open at
    the end are closed there;
  - `:root` matches only the first top-level element.
- **Token rules.**
  - script, style and xmp are raw text; textarea and title are escapable
    text; noscript holds markup.
  - Only complete references (`&amp;`, `&copy;`, `&#60;`) are decoded, in
    text and quoted attribute values. `&copy` without `;` stays as written.
    Unquoted attribute values are not decoded at all.
  - `<?…>` and `</ …>` are text; `<!…>` and `<![CDATA[…]]>` are comments.
  - Doctype ids are dropped (`<!DOCTYPE html>`).
  - Names are lower-cased, with no SVG case adjustment (`foreignobject`,
    `viewbox`). The first of duplicate attributes wins.
- **Stored and served serialized.** Every HTML write, whether a WebDAV or
  public PUT or a selector write, stores the document serialized in one
  form, which is also the form of every response:
  - empty attributes are bare (`<input disabled>`, `itemscope`);
  - attribute values escape only `&` and `"`;
  - text escapes only `& < >`, and U+00A0 is written literally;
  - void elements have no end tag, and every other element is closed.

  Whitespace, including whitespace outside `<html>`, is kept. The form is
  a fixed point, which is why sources that were already in it (the demo
  apps, LO-1) read back byte for byte.
- **Liquid.**
  - A template's source is its stored content. An output or tag that
    contains markup is unterminated, a 500: in `{{ "<a>x</a>" }}` the
    tokenizer made `<a>` an element.
  - Complete references inside `{{ }}` and `{% %}` are decoded before
    evaluation, so `{% if 2 &gt; 1 %}` works and `{{ "a&amp;b" | size }}`
    is 3. Text outside the delimiters stays as stored.
  - Outputs are **auto-escaped** unless safe. A query value `<b>x</b>` and
    `newline_to_br`'s `<br />` come out as text. `escape` marks its
    result safe, but it is not idempotent: `"<" | escape | escape` renders
    `&amp;lt;`.
  - The output is parsed and serialized in the form above.
- **Unbound prefixes.** A prefixed element whose prefix no `xmlns`
  declaration binds is an ordinary element (200, kept in place), not the
  documented 500.
- **Server JS.** `new DOMParser().parseFromString("<p>x</p>")` has no body
  (`.body` is null), matching the tree model. The 500s of
  `javascript.dom.domparser-ignores-type` and `javascript.dom.tree-operations`
  in the 2026-09-29 sample come from this.
- **pagelike:** adopted all of it:
  - `dom.BuildTree` is a tree builder over the x/net/html tokenizer, with
    exact spans;
  - `htmlser.Options.PageLove` is the serialization, except that it also
    escapes `<` and `>` in attribute values, against a noscript
    mutation-XSS;
  - all HTML is stored serialized;
  - `liquid.SourceFromHTML` and `liquid.Options.AutoEscape` implement the
    Liquid rules;
  - an unbound prefix is inert.

  See `docs/compat/decisions-2026-09-29/serialization.md`.
