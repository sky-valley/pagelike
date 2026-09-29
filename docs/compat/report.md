# pagelike compatibility report

Date: 2026-09-29 (pagelike v0.1.0). pagelike is an independent project and is
not affiliated with, or endorsed by, PageLove.

pagelike is a single Go process that serves PageLove applications. It keeps
each site in a SQLite database (WAL, `synchronous=FULL`) plus a blob folder. It
uses coarse per-site write locks, parses documents with its own tree builder
on top of x/net/html's tokenizer, selects with a cascadia fork carrying
PageLove's extensions, and runs server JavaScript in bounded QuickJS worker
processes. This report says what is compatible, how that was established, where
pagelike deliberately differs, and what is missing.

## At a glance

| What | Result |
|---|---|
| Compatibility harness, local | **1426 cases: 1358 passed, 0 failed, 68 skipped (skips: live-only siblings and probes, disputed claims)** (`go run ./harness/cmd/harness run --slow`) |
| Compatibility harness, live PageLove | **603 cases run against live PageLove: 578 match its current expectations, 25 differ as recorded (pagelike keeps its behaviour, or a disputed claim lost), 0 unexplained** |
| Go tests (unit, integration, durability, isolation) | all pass (`go test ./...`) |
| Browser suites (system Chrome) | 82 passed, 3 skipped (the opt-in live migration demo) |
| Real PageLove apps, unmodified, with the official client (release acceptance) | tier A 34/34, tier B 36/36, tier C 4 of 7 (3 not run: they need TLS, a payment stub or GitHub Actions) |
| Migration from PageLove | demonstrated end to end: live participation on PageLove, `pagelike migrate`, and the app keeps working on pagelike |
| Crash consistency | 0 acknowledged writes lost over 4 SIGKILL rounds under 12-way concurrent writes; state and replayable events agree exactly |
| Performance (M1 Pro, loopback) | GET 9,868 req/s (p50 2.9 ms), composed page 3,153 req/s, appends to one document 181/s (serialized per site), 866/s across 20 documents, SSE fan-out to 200 subscribers p50 13.8 ms with 100% delivery |

## Sources and dates

- **PageLove documentation:** a snapshot of docs.pagelove.com taken
  2026-09-28T20:53Z (200 pages). The manifest records the ETag, Last-Modified
  and SHA-256 of each page (`research/docs/2026-09-28/manifest.json`; the
  snapshot is git-ignored and re-fetchable with `tools/research/fetch_docs.py`).
- **Official code**, cloned 2026-09-28 (`research/COMMITS.txt`):
  - `beta-js` (the official client) c204746, 2026-09-14;
  - `pagelove-polls` c9270e5 and `pagelove-kanban` 85109ab, 2026-09-24;
  - `pagelove-ats` 8f200fc, 2026-09-28;
  - `pagelove-shop` d887054, 2026-09-04;
  - `demo-apps` c4dd883, 2026-08-26;
  - `pagelove-dev` b489923, `pagelove-primitives` e73986d and the `dom-*`
    packages.
- **Live PageLove:** the disposable host `live-test-host`, created for this
  work on 2026-09-28, was probed on 2026-09-28 and 2026-09-29:
  - about 1,880 recorded requests over 763 case executions, plus
    each case's setup uploads;
  - rate-limited to 3 requests/s by the harness, which refuses any host not
    marked disposable;
  - observations are kept in `harness/observations/live-*`.
- **Specification:** `docs/spec/*.md` covers 11 areas, derived from the
  sources above. Every requirement has an evidence level (documented,
  client-source, demo-source, live-observed, inferred), a confidence, and the
  contradictions between sources.

## How compatibility was established

1. **Differential harness.** About 1,400 YAML cases (`harness/cases/`) each
   carry an evidence level and a source. They run against pagelike locally, or
   against live PageLove with `--target live`, and every run can record its
   requests and responses (`--observations`). Live runs use a sample (one case
   per feature) plus targeted re-runs.
2. **Reconciliation policy** (`docs/compat/decisions.md`):
   - **adopt-live** is the default: the case follows PageLove and pagelike is
     changed.
   - **keep-documented-security:** the documented behaviour is kept when
     following PageLove would weaken a security property.
   - **keep-standard:** the standard behaviour is kept when PageLove has a
     clear bug that would produce incorrect results.
   - In both keep cases, a `.live` sibling case (`status: live-divergence`)
     asserts what PageLove does, so every difference stays measurable.
   - Harness artifacts are fixed in the harness.
3. **Minimization.** An unclear divergence got probe cases
   (`*.probe-0928.*`, `*.probe-0929.*`) that isolate one behaviour, run live
   within a fixed budget.
4. **Record.**
   - Decisions: `docs/compat/decisions.md` (2026-09-28, 69 divergences) and
     `docs/compat/decisions-2026-09-29/*.md` (40 divergences from the
     2026-09-29 sample, plus the storage-model finding and integration).
   - Observations: `docs/compat/live-observations.md` (LO-1 to LO-15).
   - Changes made to apps: `docs/compat/app-changes.md`.

## Coverage

The full matrix is [`matrix.md`](matrix.md) (`go run ./harness/cmd/harness
matrix …`, command at its end). By area:

| Area | Cases | Local pass / fail / skip | Live: run | match | kept | differ |
|---|---|---|---|---|---|---|
| reading-writing | 137 | 132 / 0 / 5 | 131 | 126 | 5 | 0 |
| protocol | 141 | 133 / 0 / 8 | 124 | 117 | 7 | 0 |
| sse | 62 | 62 / 0 / 0 | 44 | 44 | 0 | 0 |
| permissions-identity | 164 | 156 / 0 / 8 | 106 | 101 | 5 | 0 |
| modeling | 198 | 186 / 0 / 12 | 35 | 33 | 2 | 0 |
| composing | 165 | 157 / 0 / 8 | 35 | 32 | 3 | 0 |
| liquid | 144 | 138 / 0 / 6 | 40 | 38 | 2 | 0 |
| sessel | 64 | 62 / 0 / 2 | 27 | 27 | 0 | 0 |
| javascript | 152 | 146 / 0 / 6 | 30 | 30 | 0 | 0 |
| reacting | 164 | 153 / 0 / 11 | 29 | 29 | 0 | 0 |
| apps | 35 | 33 / 0 / 2 | 2 | 1 | 1 | 0 |
| **total** | **1426** | **1358 / 0 / 68** | **603** | **578** | **25** | **0** |

"Kept" is a case that fails live as expected: pagelike keeps its documented
or standard behaviour (its `.live` sibling passes live), or a disputed claim
lost. The areas first run on 2026-09-28 (reading-writing, protocol, sse,
permissions) were run almost entirely; the others were sampled one case per
feature, then re-run where they differed.

**Implemented areas:**
- **Reading and writing:** GET/HEAD, selector ranges, placements, PUT, POST,
  DELETE, conditional requests, microdata and JSON-LD, the Request Document.
- **Protocol:** MOVE, OPTIONS (flat, 207 and 204), QUERY in css and Sessel
  modes, the WebDAV authoring plane, ETags.
- **SSE:** connection tokens and echo suppression, replay and reset with
  PageLove's `v1~` event ids, keepalive, slow-consumer drops, includer
  audiences.
- **Permissions and identity:**
  - AuthorizationRule tiers, deny-wins, default-GET and templated values;
  - local accounts, per-site OIDC with PKCE, and sessions;
  - embedding with partitioned cookies.
- **Modeling:** schemas, properties, cardinality, types and enums, defaults,
  `@write` resolvers, validators, uniqueness, references and cascades, shape
  constraints, transitions.
- **Composing:** includes, stamps, resource and expression bindings, method
  elements, templated resources, pagination, transient elements.
- **Liquid:** the PageLove dialect, filters, autoescape, budgets.
- **Sessel:** the full language.
- **Server JavaScript:** a DOM subset, the method, validator and reaction
  slots, budgets.
- **Reacting:** triggers, processors, the outbox, outbound HTTP to
  allow-listed destinations.

**The document model (LO-15), found on 2026-09-29.** PageLove does not run
HTML5 tree construction.
- It builds each document straight from tokens: no implied
  html/head/body/tbody, no implied end tags, no foster parenting.
- It stores every HTML write serialized in one form, with bare empty
  attributes and a literal U+00A0.
- It decodes character references only inside Liquid tags, treats markup
  inside a Liquid output as an unterminated tag, and auto-escapes Liquid
  output.

pagelike now does the same. All 24 live stored forms are reproduced byte for
byte (`internal/dom/tree_test.go`). This changes which selectors match
hand-written markup (`table > tr` matches; `body` does not exist in a bare
fragment), so it matters for real apps. It also made large documents 2 to 4
times faster to read and write.

## Deliberate differences from PageLove

Each item has a `.live` sibling, a `live-divergence` probe, or both. All of
them passed against live PageLove when recorded.

**Kept for security** (keep-documented-security). Following PageLove would
weaken a documented protection:
- A selector-scoped `Deny` overrides a resource-level `Allow` at the same
  tier. PageLove ignores it, and serves or writes the denied element:
  - `rw.authz.denied-fragment-read`;
  - `authz.multimatch.*` (2 cases);
  - `authz.selector.selector-deny-overrides-resource-allow-same-tier`.
- OPTIONS does not advertise a method that a Deny removes
  (`protocol.options.selector-deny-removes-method`).
- `authz.discovery.whitespace-trimmed`: a padded `deny` is honoured.
- `authz.discovery.table-item-rows-are-separate-rules`: table rows stay
  separate rules; PageLove merges them.
- MOVE fails closed for an actor who cannot read the source
  (`protocol.move-authz.fail-closed-when-unreadable`).
- A page that reads the signed-in user is `Cache-Control: private`
  (`rw.reqdoc.auth-read-is-private`).
- A uniqueness refusal does not name the other document's path
  (`modeling.uniqueness.duplicate-across-documents`).
- WebDAV PUTs are checked against schemas and shapes:
  - `modeling.write-paths.webdav-put-validated-by-schema`;
  - `modeling.shapes.webdav-write-checked`;
  - `apps.shop.order-shape-dav`.

  PageLove stores them unchecked, which leaves the shop's closed Order shape
  unable to keep a `<script>` out of orders its worker writes.
- A runtime error in a trigger's `when` gate fails the request. PageLove
  treats it as false, so the gate fails open. This is recorded in
  `reacting.md`, measured by a probe step.

**Kept standard** (keep-standard). PageLove's behaviour is a bug that would
give incorrect results:
- Microdata `itemref` and multi-token `itemprop` (`rw.md.*`).
- MOVE with a `Destination` that has a query (PageLove answers 501), and a
  whole-document MOVE of a missing source (PageLove answers 500).
- Pagination links keep the request's other parameters, such as the search
  and the page length (`comp.pag.*`). The link form is otherwise PageLove's.
- Liquid range literals `(1..3)` (PageLove answers 500 "expected RParen").
- Sessel temporal accessors also work as properties; PageLove answers them
  only as methods (`sessel.query.values.temporal-format`).
- A Sessel trigger's thrown 303 plus Location is sent as a redirect
  (`reacting.actions.sessel-throw-303-location-pair`).
- Server-JS node identity (`===`) and `String(classList)`
  (`javascript.dom.*`).
- **Hardening:** stored and served HTML escapes `<` and `>` in attribute
  values. PageLove writes them literally, and because its model reads
  `<noscript>` content as markup while browsers read it as raw text, that
  allows a mutation-XSS. The difference is byte-level only; DOM values are
  unchanged.

## Known gaps and limits

- **Not probed, kept from the HTML5 tokenizer:**
  - CRLF handling in stored HTML;
  - raw-text status of iframe, noembed, noframes and plaintext;
  - `<script/>`.
- **Acceptance scenarios not run:**
  - ACC-SH-7: needs TLS, the payment worker and a Stripe stub;
  - ACC-UP-4: needs HTTPS on port 443;
  - ACC-UP-5: needs a GitHub Actions environment.
- **ACC-AT-4b:** the permissions spec ignores the legacy `GroupMembership`
  table (fail closed), and the apps spec expects it honoured. One of the two
  specs needs changing.
- **Not reconciled** (details in `decisions-2026-09-29/composing-liquid.md`):
  - `Cache-Control` on paginated and binding pages is private on PageLove;
    pagelike sends `public, max-age=5`;
  - `json` filter edge cases;
  - `where`/`sort` on a single item;
  - whitespace placement inside paginated elements.
- **Open question:** long JavaScript method results. A string of about 700
  characters failed to marshal on PageLove; it was not pursued.
- **Parsing cost:** every request parses the whole stored document. A 1.1 MB
  board reads in about 13 ms and takes a selector write in about 41 ms,
  server side. A parsed-tree cache would add headroom.
- **Writes are serialized per site** by design. See the performance table.

## Performance

Generated by `go run ./tools/loadtest -duration 5s -subscribers 200` on 2026-09-29 (commit 9da5362, PageLove's document model, LO-15). Single process, default settings, fresh data directory, SQLite WAL with synchronous=FULL (every acknowledged write is fsynced).

Machine: Apple M1 Pro, 10 CPUs, darwin/arm64, go1.26.4. Server and load generator on the same machine, loopback HTTP/1.1.

| Scenario | Concurrency | Requests/s | p50 | p95 | p99 | Errors |
|---|---|---|---|---|---|---|
| GET whole document (6 KB, stored bytes) | 32 | 9868 | 2.91 ms | 6.76 ms | 9.35 ms | 0 |
| GET selector fragment (`Range: selector=#i25`) | 32 | 6187 | 4.26 ms | 12.39 ms | 17.53 ms | 0 |
| GET composed page (binding over 50 items + Liquid loop) | 32 | 3153 | 8.80 ms | 22.29 ms | 30.75 ms | 0 |
| POST append, one document (serialized per site) | 16 | 181 | 89.83 ms | 153.07 ms | 154.59 ms | 0 |
| POST append, 20 documents in one site | 16 | 866 | 18.62 ms | 26.84 ms | 29.53 ms | 0 |

SSE fan-out: 200 subscribers on one document, 100 writes at 50/s: 20000 of 20000 deliveries (100.0%); delivery latency after the write request was sent p50 13.75 ms, p95 15.49 ms, p99 16.64 ms.

Large document (release acceptance ACC-KB-13, ~1.1 MB kanban board, 400 cards): whole-document GET p95 23.7 ms, selector write p95 69.6 ms, MOVE p95 44.4 ms, measured through the browser suite; server side `BenchmarkComposeKanbanBoard` 12.7 ms and `BenchmarkSelectorPutKanbanBoard` 41 ms per operation (`go test ./internal/compose -bench Kanban`).

Notes:

- Writes to one site are serialized by design (coarse per-site lock + one SQLite transaction per write). The single-document append rate falls as the document grows, because every write parses the stored document and stores its serialization; the document in that scenario grew from 50 to ~900 elements during the run. Writes spread over documents run at ~1.2 ms each.
- Earlier run (commit ee104d9, HTML5 tree model with source splicing): GET whole document 6075/s, composed page 2740/s, one-document appends 154/s, 20-document appends 834/s, SSE p50 15.6 ms; the kanban board read ~46 ms and wrote ~90 ms server side. Building the tree straight from tokens (LO-15) is what made reads and large-document writes faster.
- For comparison, the official kanban app records selector writes of about 1 s on a large board on PageLove itself (research/upstream/pagelove-kanban/site/app.js, workaround W-5 in docs/spec/apps.md).
- Durability tests (test/durability) SIGKILL the server during 12-way concurrent appends: 0 acknowledged writes lost over 4 crash rounds, and state and replayable events agree exactly.


## Security boundaries

Each boundary is enforced on the server and tested:
- **Participants.** Ownership rules, triggers and closed shapes are enforced
  on every write. The example experiences test a participant editing or
  deleting another's contribution (refused). Participation views are served only
  while the viewer may read the element.
- **Sites.** `internal/server/isolation_test.go` checks:
  - authoring keys are scoped to their site;
  - sessions do not cross sites;
  - bindings and Sessel queries see only their own site;
  - an authoring key is never an end-user identity.

  QUERY programs see only their target document (live 2026-09-29).
- **Forks.** A remix copies authored state only: no participants, identities,
  keys or sessions. It records its lineage, and cannot reach the source's
  state (`internal/server/portable_test.go`, the example remix test).
- **Credentials.**
  - Authoring keys never reach browsers: the WebDAV plane is a separate host,
    and keys are stored as hashes.
  - Sessions are HttpOnly `__Host-` cookies.
  - `serve --dev-insecure-auth` is opt-in, refuses non-loopback addresses and
    logs every use.
- **Outbound HTTP** from reactions cannot reach private or loopback
  addresses unless the operator allows them (`--outbound-allow LIST`,
  `--outbound-allow-private`). The tests' webhook sinks are local and
  allowed explicitly.

## Persistence and synchronization

The durability tests (`test/durability`) run the real binary:
- **Crash consistency:** SIGKILL during 12-way concurrent appends, 4 rounds.
  No acknowledged write is lost, nothing is duplicated, and the document and
  its replayable events agree exactly.
- **Conditional writes:** 16 writers each perform 25 contended
  read-modify-write increments guarded by `If-Match`. The final value equals
  the number of successful writes, and stale tags get 412.
- **Reconnect after restart:** a client holding an event id that reconnects
  after a crash gets exactly the missed mutations, in order. An expired id
  gets a `reset` (ACC-PO-7 in the browser).

## Migration

`pagelike migrate --from-dav <url> --key-file … --out DIR --import --site
NAME` copies a PageLove host's stored files byte for byte, then imports them.
The demonstration (`docs/compat/migration.md`, `e2e/tests/migration.spec.mjs`,
opt-in with `PAGELIKE_LIVE_E2E=1`) has three phases:
1. The unmodified pagelove-polls app is used live on PageLove by two browser
   sessions.
2. The host is migrated to pagelike.
3. The same app on pagelike keeps its polls, responses, rules and live
   updates.

Identity mapping (`docs/compat/migration.md`):
- End-user identities are not copied. A pagelike site uses local accounts or
  its own OIDC provider.
- Rules that name users by their PageLove subject keep working once the new
  provider's subject is linked to the old one: `pagelike user link --site S
  --issuer <new> --idp-sub <new sub> <old sub>`.
- Emails and roles need no mapping when the new provider asserts the same
  verified values.
- The polls app's responses carry no identity, so it needs no mapping.

## Example experiences

These are in `examples/` and verified in `e2e/tests/examples.spec.mjs` with
several browser sessions:
- **"Show me your sky":** photo submissions into per-user folders, ownership
  enforced on the server, and live updates to other sessions. It plays inside
  a cross-origin iframe, sign-in included, using partitioned cookies and
  `--embed-origins`.
- **Remix:** "show me your dog" has independent state, no participants, and a
  recorded lineage.
- **Shared poll:** one vote per person, live tallies, no tampering.
- **Team board:** composed lanes, a TransitionConstraint state machine
  (illegal moves get 422), live refresh across sessions.
- **Participation views:** `/-pagelike/p/<id>` shows one contribution with
  attribution and a link to the experience and version it was made in.

The examples use only PageLove-documented runtime features, plus pagelike's
own participation views and remix. They have not been run on PageLove: their
participants must sign in, and the disposable host has no identity provider.

## Running the live harness responsibly

The live target (`harness run --target live`) sends real requests to a
PageLove host. It refuses to run unless `.secrets/pagelove.env` names the
host and marks it disposable (`PAGELOVE_DISPOSABLE=yes`), and it is
rate-limited to 3 requests/s. Use a host you created for testing, never one
that holds real data. Every case writes only under its own `/_pl/<case>-<id>/`
folder. Delete the host when you are done.
