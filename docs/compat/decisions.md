# Live reconciliation decisions (2026-09-28)

This file records how pagelike handled each failure of the live PageLove run of
2026-09-28 (host `live-test-host`, runner at commit 4ad1a33), across the
reading-writing, protocol, sse and permissions-identity areas: 69 failing
cases in all.

**Evidence.**
- First run: `harness/observations/live-2026-09-28/` (`RUN-SUMMARY.txt` plus one
  JSON file per case). A case id maps to its file by turning dots into dashes
  and cutting the name to 40 characters. For example,
  `authz.absent.post-before-absent-anchor-refuses` →
  `authz-absent-post-before-absent-anchor-r.json`.
- Confirmation runs of 2026-09-29: `harness/observations/live-2026-09-29-confirm/`,
  using the same naming. The `-live` suffix marks a `.live` sibling.
- Changed cases cite their own observation file in `source:`.

**Decision policy** (applies to real behaviour differences only):

| Class | Meaning |
|---|---|
| `adopt-live` | The case expectations now follow PageLove (`evidence: live-observed`, citing the observation), and pagelike is changed to match. This is the default, and it also covers PageLove bugs that do not produce incorrect data. |
| `keep-documented-security` | Following PageLove would weaken a security property. pagelike keeps its documented behaviour. A sibling case `<id>.live` (`status: live-divergence`, run only with `--target live`) asserts what PageLove does. |
| `keep-standard` | PageLove has a clear bug whose adoption would produce plainly incorrect data, or would break a standard every client relies on. pagelike keeps the standard behaviour, and a `.live` sibling measures the difference. |
| `harness artifact` | The failure came from the harness or the case, not from a behaviour difference. It is fixed in `harness/` or the case, and pagelike's behaviour is unchanged unless noted. |

## Summary

| Area | Failures | adopt-live | keep-documented-security | keep-standard | harness artifact |
|---|---|---|---|---|---|
| reading-writing | 16 | 10 | 2 | 2 | 2 |
| protocol | 27 | 18 | 2 | 3 | 4 |
| sse | 11 | 11 | 0 | 0 | 0 |
| permissions-identity | 15 | 6 | 5 | 0 | 4 |
| **total** | **69** | **45** | **9** | **5** | **10** |

- Each case is counted once, under its main class; mixed cases are marked in
  their row.
- One case that passed live also changed a decision:
  `protocol.query-sessel.400-wrong-type-documented` was marked disputed, passed
  live (an XPASS), and so reversed compatibility decision C-9 (adopt-live, listed
  under protocol).
- 14 `.live` siblings measure the deliberate divergences
  ([list](#live-divergence-siblings)).

## Harness artifacts (fixed in the harness)

1. **PageLove footer.** The free plan inserts a "Powered by Pagelove" `<div>`
   before `</body>` (LO-1). On 401 documents the footer landed inside the
   `https://pagelove.org/1.0/Error` item, so its properties did not match.
   `harness.StripFooter` now removes that exact footer before any body check.
   Cases: `rw.authz.401-error-document`, `authz.error.401-document-shape`.
2. **Disputed cases counted as failures.** A `status: disputed` case records the
   losing side of a documented contradiction, and is expected to fail live. The
   runner now reports it as `XFAIL`. When one passes (`XPASS`), the runner
   reports a failure so that the decision is reviewed.
   Cases: `rw.get.content-range-equals`,
   `protocol.options.wildcard-star-example`,
   `protocol.options.multipart-content-range-equals-form`,
   `protocol.query-edge.documented-multipart`.
3. **Root cases.** The runner reported root cases as live failures ("root cases
   are not run live"). Read-only root probes now run live when opted in with
   `--root`, and are skipped otherwise. Case:
   `authz.identity.login-path-without-provider`. Live answered 404, which is
   pagelike's answer (R-PERM-67).
4. **Plane routing.** The runner at 4ad1a33 sent every `as: author` request to
   the WebDAV host, even with `plane: public`. It also sent the authoring key
   on every `plane: dav` request, including `as: anonymous` ones. Now:
   - `as: author` goes to the public plane when the step says so;
   - anonymous dav requests carry no credentials.

   Cases: `authz.identity.api-key-not-end-user-identity` and
   `protocol.webdav.unauthenticated-401`. Both now pass live, and the two
   `.live` siblings written after the first run were removed.
5. **Case confounded by another divergence.**
   `authz.discovery.action-case-insensitive` used a selector-scoped Deny
   against a resource-level Allow, which PageLove does not enforce (see
   `authz.selector.selector-deny-overrides-resource-allow-same-tier`). The case
   now tests action spellings with document-level rules only.
6. **Sessel expressions.** The Sessel cases avoided `${…}`, which the runner
   substitutes, by using the documented equivalent
   `(new Selector { "li" }).execute()`. PageLove rejects that form with
   "unknown function: execute". The runner now has an escape (`$${…}` sends a
   literal `${…}`), and the cases use `${li} from self`. The docs' claimed
   equivalence is recorded as a live difference (LO-10). These cases are
   counted as adopt-live below, because their other expectations changed too.
7. **Other setup and timing issues:**
   - `protocol.webdav.mkcol` did not create its own prefix collection.
   - `sse.replay.ancient-id-resets` sent a bare `1000-0` id, which PageLove
     cannot parse.
   - `sse.scope.trigger-side-effect-emits` wrote before the trigger was bound.
     The case now waits 61 s and is marked slow.

   Each was fixed in the case, and then reconciled as a real difference
   (rows below).
8. **Harness additions made during reconciliation:**
   - `--ids` (a comma list or `@file`) for targeted live re-runs;
   - capture filters `|unquote` and `|regex:`;
   - a list of every event seen on the stream when an SSE expectation fails;
   - the `prune_events` step behind the `sse-control` capability (local only);
   - the `live-divergence` status.

## reading-writing

| Case id | Observed (live) | Documented claim | Decision | Rationale |
|---|---|---|---|---|
| `rw.authz.denied-fragment-read` | 206 serves the element that a selector-scoped `Deny` names, because a resource-level `Allow` covers the read. The all-matches multipart includes it. | The first match is authorized. All-matches reads are refused if any match is denied (R-RW-128, R-PERM-41). | keep-documented-security | "Denying a fragment is enough" is the only way to hide an element. Serving it leaks exactly what the author denied. Sibling: `.live`. |
| `rw.authz.401-error-document` | Same 1.0/Error body, plus the plan footer. | R-RW-130 401 shape. | harness artifact | Footer stripping (artifact 1). |
| `rw.cond.post-if-match-document-tag` | A selector POST's `If-Match` with the document tag is 412, and the 412 carries the anchor's tag. | The document tag or the element tag satisfies `If-Match` (R-RW-89 union). | adopt-live | Only the addressed element's tag counts (Q-1 settled). Element tags carry the document version, so any write invalidates them. |
| `rw.cond.get-if-match-stale` | A GET with a stale `If-Match` is 206. | RFC 9110 would answer 412 (R-RW-140). | adopt-live | beta-js sends `If-Match` on GET. Ignoring it matches PageLove and harms no data. |
| `rw.delete.content-range` | Selector DELETE: 204 with `ETag`, `Last-Modified`, `Vary: Host, Range`. No `Content-Range`. | 204 with `Content-Range: selector <css>` (R-RW-80). | adopt-live | No client reads it. |
| `rw.delete.missing-document-404` | Whole DELETE of a missing path: 204 with the ETag of empty content (`"e3b0c442…b855"`). Selector DELETE there: 404 NotFound problems item. | 404 for both (R-RW-81/82). | adopt-live | Idempotent DELETE is a defensible reading (RFC 9110 §9.3.5). No data is misreported. |
| `rw.dir.unreadable-index-404` | A slash-less directory under a denied `dir/*` is 401, with `resource` set to the slash-less path. No redirect. | 404 without a redirect (R-RW-4). | adopt-live | `dir/*` governs `dir` itself (R-PERM-20 as reconciled), so the request is a denial. It reveals nothing beyond the ordinary denial of `dir/`. |
| `rw.get.content-range-equals` | `Content-Range: selector h1`. | Docs examples: `selector=h1` (disputed, losing side of C-1). | harness artifact | Now XFAIL (artifact 2). C-1 is confirmed. |
| `rw.md.itemref` | JSON-LD ignores `itemref`. | WHATWG property crawl follows `itemref` (R-RW-47). | keep-standard | Dropping referenced properties publishes items that misstate the page. The fix would sit in `internal/microdata`, which is shared by selectors and modeling. Sibling: `.live`. |
| `rw.md.multiple-itemprop-tokens` | One key, `"name alternateName"`. | Each token is its own property (R-RW-49). | keep-standard | No vocabulary has that property name, so the output is incorrect data. Sibling: `.live`. |
| `rw.post.unknown-placement` | `placement=inside`: 206, appended. | 400 (R-RW-71, compat decision). | adopt-live | Lenient and harmless: append is the default placement. |
| `rw.post.parentless-anchor-400` | `before`/`after` on `<html>`: 422 problems item (`https://pagelove.org/Error`, message "placement=Before relative to the document root is not possible: the root has no siblings"). | 400 (R-RW-74). | adopt-live | Status and vocabulary only. |
| `rw.post.absent-anchor-before-refused` | `before`/`after` with an absent anchor on a readable page: 416 selector-no-match. | Refused as a denial (R-RW-75). | adopt-live | Absence is still hidden from anyone who cannot read the page (R-RW-127). |
| `rw.post.etag-is-anchor-tag` | The POST ETag is the inserted child's tag (`"<sha256(child)>-<same>-<version>"`). No `Content-Location`. | The anchor's tag (R-RW-73, from beta-js's RFC 9110 reading). | adopt-live | Q-4 settled. beta-js pins the child's tag on the parent, which is its own problem. The tag formula is adopted byte for byte (LO-4). |
| `rw.reqdoc.auth-read-is-private` | A page that reads `request.auth` is served `Cache-Control: public, max-age=5`, with no session `Vary`. | `private` (R-RW-103, R-PERM-76). | keep-documented-security | A shared cache could hand one visitor's personalized page to another. Other composed pages adopt `public, max-age=5` (LO-7). Sibling: `.live`. |
| `rw.up.xml-selector` | A selector on an XML document: 422 "Invalid path: Selector operations require HTML documents, but … has content type application/atom+xml". | XML is selector-addressable (R-RW-30). | adopt-live | PageLove serves XML like a blob on the public plane. Whole-document XML reads are unchanged. The authoring-plane QUERY still reads XML. |

## protocol

| Case id | Observed (live) | Documented claim | Decision | Rationale |
|---|---|---|---|---|
| `protocol.move-authz.fail-closed-when-unreadable` | 416 for an absent source on a document the actor cannot read. | Fails closed, 401/403 (R-PROTO-99, R-PERM-49). | keep-documented-security | A 416 is an oracle for selector existence on unreadable pages. Sibling: `.live`. |
| `protocol.move.invalid-selectors-422` | An unparsable source selector matches nothing (416 selector-no-match). An unparsable destination is 422 `InvalidPath` "Invalid destination selector: invalid selector '<sel>': <err>". | 422 for both (R-PROTO-99, C-24). | adopt-live | Status and vocabulary. |
| `protocol.move.whole-document-missing-source` | 500, with an Internal problems item saying the document was not found. | 404 (R-PROTO-98). | keep-standard | A server error for a client's missing resource is a PageLove bug that no client can build on. pagelike answers 404 with the write path's NotFound item. Sibling: `.live`. |
| `protocol.move.if-match-current` | The document tag is 412 (carrying the source element's tag). The source element's tag passes, and `*` passes. | Document tag or element tag (R-PROTO-100, C-14). | adopt-live | Same rule as selector writes (Q-1/P-13 settled). |
| `protocol.move.destination-absolute-url` | A `Destination` with a query string is 501 `MoveCrossResource`. | The query is ignored when comparing paths (R-PROTO-91, C-15). | keep-standard | beta-js sends `location.href`. Refusing any page URL with a query breaks the official client. Lookup ignores the query everywhere else (R-RW-2). Sibling: `.live`. |
| `protocol.options.selector-scoped-flat` | The flat document-level `Allow` includes methods granted only by selector-scoped rules. | Document-level grants only (R-PROTO-12/14). | adopt-live | The flat answer is the union. The multipart form keeps the split. |
| `protocol.options.wildcard-star-example` | `Allow` is expanded. | Docs example `Allow: *, OPTIONS` (disputed, C-1). | harness artifact | XFAIL. C-1 is confirmed. |
| `protocol.options.multipart-content-range-equals-form` | Parts use `Content-Range: selector h1`. | Docs example `selector=h1` (disputed, C-2). | harness artifact | XFAIL. C-2 is confirmed. |
| `protocol.options.selector-deny-removes-method` | PUT is advertised at a selector that a same-tier Deny names. The multipart answer is 204. | The Deny removes the method (R-PROTO-12). | keep-documented-security | pagelike enforces the deny (see the permissions rows), so it advertises what it enforces. Sibling: `.live`. |
| `protocol.query-dav.etag-ignores-unmatched-change` | A write outside the matched fragment changes the answer's tag. The part tags end in the document version (`-1` → `-2`). | Unchanged (R-PROTO-56, which held only for content-derived fragment tags). | adopt-live | Follows from the adopted tag formula (LO-4). |
| `protocol.query-edge.documented-multipart` | Without `Accept`: 206 `text/html`. | Always multipart (disputed, C-6). | harness artifact | XFAIL. C-6 is confirmed. |
| `protocol.query-edge.415-unsupported-type` | `text/plain` without a QUERY grant: 401. With a grant: 400 "QUERY method requires Content-Type: text/sessel". No `Accept-Query`. | 415 with `Accept-Query` (R-PROTO-42/43). | adopt-live | Anything that is not a css query goes down the Sessel path, which is authorized as QUERY. |
| `protocol.query-edge.422-invalid-selector` | 416 "HTML parsing error: Invalid CSS selector: …" (read vocabulary). | 422 (R-PROTO-64, C-10). | adopt-live | Same answer as a GET with that selector. The parser's message text differs (Rust selectors vs pagelike's own parser). |
| `protocol.query-sessel.number-result` | 400 "unknown function: execute" for the case's `Selector.execute()`. The confirmation run with `${li} from self` passed. | The docs say `Selector.execute()` equals `from self` (C-10). | adopt-live | Cases use the literal form (artifact 6). pagelike's Sessel is not implemented yet, so the case needs `sessel`. |
| `protocol.query-sessel.element-result` | As above. The confirmation run passed. | Same. | adopt-live | Same. |
| `protocol.query-sessel.element-list-tagged` | Selected elements are `{"$type":"element","$html":…}` with no `$source`. | `$source` names the document (R-PROTO-73). | adopt-live | Omitting a field is harmless. |
| `protocol.query-sessel.directory-self-unbound` | A directory URL means its `index.html`: 404 "Document not found: …/sub/index.html" before evaluation. | `self` is unbound on directories and the answer is 416 (R-PROTO-71/75). | adopt-live | P-22 settled. pagelike's `querySessel` resolves the index the same way. |
| `protocol.query-sessel.entries-pagination` | `Content-Range: entries 0-1/5;` (with a trailing `;`). | `entries 0-1/5` (R-PROTO-81). | adopt-live | Zero-based inclusive indexes are confirmed (P-19). |
| `protocol.query-sessel.entries-format` | `entries 1-2/5;`. | No `;`. | adopt-live | Same as above. |
| `protocol.query-sessel.entries-non-list-416` | Same parse failure as `number-result`. The confirmation run passed (416). | 416 (R-PROTO-83). | adopt-live | The expectation holds. Only the expression changed. |
| `protocol.query-sessel.400-body-errors` | Empty body: 422 "Invalid path: QUERY method requires a body containing the sessel expression". Parse error: 400 "Sessel compilation error: …". | 400 for both (R-PROTO-75). | adopt-live | pagelike's `querySessel` answers the 422 read error. |
| `protocol.query-sessel.500-runtime-error` | `throw`: 400 "Sessel evaluation error: thrown value (at bytes 6..12)". | 500 (R-PROTO-75). | adopt-live | P-21 settled for `throw`. |
| `protocol.query-sessel.415-unsupported-type` | `application/json`: 400 "QUERY method requires Content-Type: text/sessel". | 415 (C-9). | adopt-live | C-9 reversed. |
| `protocol.query-sessel.400-wrong-type-documented` | Passed live (XPASS; it is not one of the 69). | The Sessel error table: 400. | adopt-live | It is now the winning claim, so the `disputed` status was removed. |
| `protocol.webdav.get-is-byte-exact` | A WebDAV PUT of HTML is stored re-serialized: lower-cased tags, quoted attributes, closed elements, `<!DOCTYPE html>`, bare empty attributes. The PUT echoes the stored form. | The stored bytes are returned byte for byte (R-PROTO-113). | keep-standard (deferred); **adopted 2026-09-29 (LO-15)** | Deferred on 2026-09-28: it needed a serializer with PageLove's bare-attribute output and a decision for Liquid sources. Resolved the next day: see `decisions-2026-09-29/serialization.md`. The sibling was folded into the case. |
| `protocol.webdav.mkcol` | A missing parent is 409 `ParentDirectoryMissing` (the case did not create its prefix: artifact 7). A probe on 2026-09-29 showed that an existing collection is **405**. | 409 `DirectoryAlreadyExists` (R-PROTO-115, C-19). | adopt-live | RFC 4918 answer. pagelike: 405 with `Allow` and the `DirectoryAlreadyExists` detail; 409 `ParentDirectoryMissing`; 201 with an ETag of 64 zeros. The deploy scripts accept either. |
| `protocol.webdav.missing-is-404` | DELETE of a missing path is 204 (ETag of empty content). PROPFIND and GET are 404 `Error/NotFound` short articles. | DELETE 404 (R-PROTO-116). | adopt-live | Same as the public-plane whole DELETE. |
| `protocol.webdav.unauthenticated-401` | First run: 207/200, only because the runner sent the key (artifact 4). Confirmation run: 401 with `WWW-Authenticate: Basic realm="WebDAV"`, `DAV: 1, 2`, `MS-Author-Via: DAV`, and a short `Error/Internal` article "Authentication required". | 401 (R-PROTO-111). | harness artifact | The status held. pagelike adopted the headers and body (LO-12). |

## sse

| Case id | Observed (live) | Documented claim | Decision | Rationale |
|---|---|---|---|---|
| `sse.echo.cross-session-token-not-honoured` | A writer that is not in s1's session, presenting s1's token, suppressed s1's copy. | Session AND token (R-SSE-38). | adopt-live | Q18: the token alone names the stream. Tokens are unguessable UUIDs given only to the stream's holder, so a page can hand its token to a helper writer. |
| `sse.mutation.id-format` | Ids look like `v1~<12 hex of sha256(path)>.<ms>-<seq>`. | `<ms>-<n>` (R-SSE-29). | adopt-live | The documented form is the tail of the id. Clients treat ids as opaque. |
| `sse.mutation.put-etag-matches-response` | The event `etag` is the document's new tag, unquoted. It equals the tag that a DELETE or whole PUT answers, not the element tag of a selector write. | The element tag, quoted (R-SSE-13, R-RW-100). | adopt-live | Q4 settled. beta-js copies it into `If-Match`, and pagelike accepts unquoted tags (R-RW-85). |
| `sse.replay.ancient-id-resets` | With a real id whose time part is 1000 ms, every retained event after it is replayed. No reset. (The first run's bare `1000-0` was an artifact.) | Reset for ids older than the retention window (R-SSE-32). | adopt-live | A reset happens only when events after the id have been pruned. pagelike checks the event sequence after pruning (`eventsLost`). |
| `sse.replay.stream-continues-after-reset` | No reset could be produced live without waiting out retention. | The stream stays open after a reset (R-SSE-32). | adopt-live | The reset condition follows PageLove. The case now prunes events itself (`prune_events`, `requires: [sse-control]`), so it runs locally only. `sse.replay.expired-after-retention` (slow) covers live. |
| `sse.replay.malformed-id-resets` | An unparseable `Last-Event-ID` is ignored: only the connection event, then live events. | Reset `events-expired` (R-SSE-32). | adopt-live | Harmless: the client stays live. |
| `sse.scope.include-not-propagated` | A write to an included partial also reached the including page's stream (same event, same id and path). | Not propagated (R-SSE-20). | adopt-live | Expectations adopted. pagelike has no includes yet, so this case still fails locally (composition not implemented). |
| `sse.scope.write-through-event-on-origin` | A write through the composed page is announced on the page's stream with `path` set to the page. Nothing is announced on the partial's stream. | On the origin only, with the origin path (R-SSE-11/19). | adopt-live | Same as above: expectations adopted, and the case still fails locally until composition exists. |
| `sse.scope.webdav-write-emits` | No event within 8 s after a WebDAV PUT. | Whole-document events (R-SSE-22). | adopt-live | Q9 settled. `engine.event` emits nothing for authoring-plane writes. |
| `sse.scope.trigger-side-effect-emits` | Once the trigger was bound (61 s after setup), its `Pagelove.PUT` wrote `side.html`, but the side stream got no event. | Side-effect writes emit (R-SSE-23). | adopt-live | Q11 settled. Side-effect writes emit nothing (`w.sideEffect`). Re-confirmed live after the change (see [Live confirmation](#live-confirmation)). |
| `sse.subscribe.missing-document-404` | 200 `text/event-stream`: `: connected`, the connection event, then keepalives. A later creation is announced. | 404 (R-SSE-4). | adopt-live | Q13 settled. Authorization is still decided first. |

## permissions-identity

| Case id | Observed (live) | Documented claim | Decision | Rationale |
|---|---|---|---|---|
| `authz.absent.post-before-absent-anchor-refuses` | 416 for `before` and `after`. The POST grant was selector-scoped, so this is the authorization layer's 416 (`Content-Range: selector */`, read vocabulary with charset, no `Vary`). | Refused (R-PERM-51). | adopt-live | Absence stays hidden from non-readers. pagelike reproduces both 416 shapes (`authzNoMatch`). |
| `authz.discovery.action-case-insensitive` | The deny that failed was selector-scoped. | Case-insensitive actions (R-PERM-3). | harness artifact | Artifact 5. It passes live now. |
| `authz.discovery.whitespace-trimmed` | Padded actor, resource, method and selector are trimmed. A padded action is not recognized: a padded `allow` grants nothing and a padded `deny` refuses nothing. | Every field is trimmed (R-PERM-2). | keep-documented-security (partly adopt-live) | pagelike adopts the ignored padded `allow` (it fails closed) but keeps honouring a padded `deny`, because ignoring a refusal the author wrote fails open. Sibling: `.live`. |
| `authz.discovery.rule-in-xml-document` | An `Allow` stored in an XML document grants nothing (401). | Rules may sit in XML documents (R-PERM-1). | adopt-live | PageLove treats XML like a blob (no selectors, no rules). `ExtractRules` skips XML. Ignoring an extra source of grants fails closed. |
| `authz.discovery.table-item-rows-are-separate-rules` | Rows are merged into one rule, with the first row's selector and every row's methods. Row 2's DELETE was granted on row 1's `h1` and denied on the items it names. | One rule per row (R-PERM-2a). | keep-documented-security | The merge grants a method on elements the author never named (privilege escalation, confirmed by a probe: DELETE h1 → 204). Sibling: `.live`. |
| `authz.error.401-document-shape` | Same body, plus the plan footer. | R-PERM-55. | harness artifact | Artifact 1. |
| `authz.identity.login-path-without-provider` | Not run (a root case). With `--root`: 404. | 404 (R-PERM-67, pagelike). | harness artifact | Artifact 3. pagelike's answer is confirmed. |
| `authz.identity.api-key-not-end-user-identity` | First run: 200, because the key went to the WebDAV host. Confirmation run: 401 on the public plane. | 401 (R-PERM-71). | harness artifact | Artifact 4. |
| `authz.move.source-absent-readable-416` | 401 for a missing source or anchor when DELETE/POST are granted only by selector-scoped rules, even on a readable page. | 416/404 when readable (R-PERM-49). | adopt-live | The element check cannot run on an absent element, so the request is refused unless the method is granted at document level. This fails closed, and with document-level grants 416/404 still hold. |
| `authz.multimatch.all-matches-read-denied-if-any-denied` | Every match is served, the denied one included. | All-or-nothing (R-PERM-41). | keep-documented-security | Same leak as `rw.authz.denied-fragment-read`. The multipart part headers are adopted (LO-8). Sibling: `.live`. |
| `authz.multimatch.single-read-first-match-denied` | The denied first match is served. | Refused (R-PERM-41). | keep-documented-security | Same as above. Sibling: `.live`. |
| `authz.resource.trailing-slash-star-not-bare-name` | `/x/*` also matches `/x` (PUT `/x` 201). `/xy.html` is refused. | `/x/*` does not match `/x` (R-PERM-20). | adopt-live | The bare directory name belongs with the directory, and the anchoring still rejects `/xy.html`. |
| `authz.resource.directory-form-rule` | An `Allow` written for `dir/` grants nothing on `dir/` requests (the rule is matched against `dir/index.html`). | The request path or the canonical path (R-PERM-19). | adopt-live | This is why the ATS app duplicates its rules. A `Deny` written for `dir/` still refuses `dir/index.html` in pagelike (fail closed). |
| `authz.resource.slashless-directory-unreadable-404` | 401, with `resource` set to the slash-less path. | 404 (R-PERM-52). | adopt-live | Follows from `/x/*` matching `/x`. |
| `authz.selector.selector-deny-overrides-resource-allow-same-tier` | The write went through: a selector-scoped Deny does not override a resource-level Allow. | "Denying a fragment is enough" (R-PERM-40). | keep-documented-security | Otherwise a fragment deny protects nothing. Sibling: `.live`. |

## Live-divergence siblings

These cases have `status: live-divergence`. They are skipped locally and run
only with `--target live`, where they assert what PageLove does.

| Sibling | Asserts (PageLove) | Kept pagelike case | Class |
|---|---|---|---|
| `rw.authz.denied-fragment-read.live` | A denied element is served (single and multipart) | `rw.authz.denied-fragment-read` | keep-documented-security |
| `rw.md.itemref.live` | JSON-LD without the `itemref` properties | `rw.md.itemref` | keep-standard |
| `rw.md.multiple-itemprop-tokens.live` | One `"name alternateName"` key | `rw.md.multiple-itemprop-tokens` | keep-standard |
| `rw.reqdoc.auth-read-is-private.live` | `Cache-Control: public, max-age=5` on an auth-reading page | `rw.reqdoc.auth-read-is-private` | keep-documented-security |
| `protocol.move-authz.fail-closed-when-unreadable.live` | 416 to a non-reader | `protocol.move-authz.fail-closed-when-unreadable` | keep-documented-security |
| `protocol.move.whole-document-missing-source.live` | 500 Internal | `protocol.move.whole-document-missing-source` | keep-standard |
| `protocol.move.destination-absolute-url.live` | 501 for a `Destination` with a query | `protocol.move.destination-absolute-url` | keep-standard |
| `protocol.options.selector-deny-removes-method.live` | PUT advertised despite the Deny | `protocol.options.selector-deny-removes-method` | keep-documented-security |
| ~~`protocol.webdav.get-is-byte-exact.live`~~ | A re-serialized echo and read-back | `protocol.webdav.get-is-byte-exact` | folded into the case on 2026-09-29 (LO-15) |
| `authz.discovery.whitespace-trimmed.live` | A padded `deny` is ignored | `authz.discovery.whitespace-trimmed` | keep-documented-security |
| `authz.discovery.table-item-rows-are-separate-rules.live` | Merged rows; DELETE granted on row 1's selector | `authz.discovery.table-item-rows-are-separate-rules` | keep-documented-security |
| `authz.multimatch.all-matches-read-denied-if-any-denied.live` | Every match served | `authz.multimatch.all-matches-read-denied-if-any-denied` | keep-documented-security |
| `authz.multimatch.single-read-first-match-denied.live` | The denied first match served | `authz.multimatch.single-read-first-match-denied` | keep-documented-security |
| `authz.selector.selector-deny-overrides-resource-allow-same-tier.live` | The write goes through | `authz.selector.selector-deny-overrides-resource-allow-same-tier` | keep-documented-security |

All 14 passed against live on 2026-09-29. Four of the nine
keep-documented-security cases share one root cause: PageLove does not let a
selector-scoped `Deny` override a resource-level `Allow`. That covers
`rw.authz.denied-fragment-read`, both multimatch cases and
`authz.selector.selector-deny-overrides-resource-allow-same-tier`, and it
explains the OPTIONS row too.

## Consequential updates outside the four areas

A full local run of every area, compared with the base commit 17a5b99, found
two javascript cases that encoded superseded behaviour. They were updated to
the reconciled behaviour. No other area changed outcome.

| Case id | Was | Now | Follows |
|---|---|---|---|
| `javascript.client.head-etag-then-conditional-writes` | GET with a stale `If-Match`: 412 | 206 (ignored) | `rw.cond.get-if-match-stale` (adopt-live) |
| `javascript.query.javascript-body-415` | 415 with `Accept-Query` | With a QUERY grant: 400 "QUERY method requires Content-Type: text/sessel", with no `Accept-Query` | `protocol.query-sessel.415-unsupported-type` (adopt-live, C-9 reversed) |

`docs/spec/javascript.md` (R-JS-4, R-JS-102) still describes the old answers.
That file is outside this reconciliation's scope and is listed under
[Unresolved and deferred](#unresolved-and-deferred).

## Compatibility decisions revisited

| Decision | Before | After |
|---|---|---|
| reading-writing C-4, protocol C-5: `Accept-Ranges` / `Accept-Query` | Documented superset | **Kept.** Live sends none on HTML reads, `bytes` on JSON-LD, all-matches, XML and blobs, and neither on OPTIONS. pagelike keeps the documented `selector, bytes` / `selector` and `Accept-Query` because clients never read them and they are the documented contract. It sends `bytes` where PageLove serves like a blob. |
| reading-writing C-4: `Vary` | Always `Range, Accept` | **Superseded.** Uses PageLove's sets (LO-6). |
| reading-writing C-10, protocol C-24: invalid selector | 422 | **Superseded for reads and css QUERY:** 416 "HTML parsing error". An invalid MOVE destination selector stays 422; an invalid source matches nothing (416). |
| protocol C-9: wrong QUERY type | 415 | **Reversed:** 400 (Sessel path). |
| protocol C-14: MOVE `If-Match` | Document or element tag | **Superseded:** the source element's tag only. |
| protocol C-19: MKCOL on an existing collection | 409 | **Superseded:** 405, with the `DirectoryAlreadyExists` detail kept. |
| reading-writing C-1, protocol C-1/C-2/C-6 | Disputed | **Confirmed** (XFAIL live). |
| permissions C15: directory paths in rules | Match either form | **Superseded for Allow** (canonical path only). A directory-form Deny still applies. |
| permissions C19: table-form rules | Split per row | **Kept** (security). |

## Live confirmation

Only changed cases were re-run, and the whole reconciliation used about 570
live requests (the budget was about 600):
- probes: about 30;
- confirmation run 1: about 460;
- confirmation run 2: about 65;
- trigger re-run: about 15.

1. **Confirmation run 1** (2026-09-29, 65 cases: every changed case and every
   `.live` sibling): 56 passed, 9 failed. The 9 were:
   - the two `.live` siblings for the api-key and anonymous-WebDAV cases, which
     had been built on the routing artifact (4) and were removed;
   - `protocol.query-sessel.element-list-tagged` (`$source`, adopted);
   - `protocol.query-sessel.entries-pagination` and `entries-format` (the
     trailing `;`, adopted);
   - `protocol.webdav.mkcol` (405, adopted);
   - `sse.replay.ancient-id-resets` (replay, adopted);
   - `sse.replay.stream-continues-after-reset` (now local-only);
   - `sse.scope.trigger-side-effect-emits` (trigger binding delay).
2. **Confirmation run 2** (`--slow --root`, 9 cases): 8 passed. The trigger case
   failed, as expected before the change: no event on the side stream.
3. **Trigger re-run** after adopting "side effects emit nothing" (`--slow`,
   1 case): passed. The side document was written, and its stream got no
   event (`harness/observations/live-2026-09-29-confirm/sse-scope-trigger-side-effect-emits.json`).

After these runs every changed case passes live, or is XFAIL for disputed
cases, or is local-only (`sse.replay.stream-continues-after-reset`).

## Unresolved and deferred

- ~~**HTML re-serialization on write**~~ (`protocol.webdav.get-is-byte-exact`).
  Resolved on 2026-09-29: LO-15, `decisions-2026-09-29/serialization.md`.
- **Microdata `itemref` and multi-token `itemprop`.** Kept standard. Any change
  would belong in `internal/microdata`, which this reconciliation may not edit.
- **Composition-dependent cases.** `sse.scope.include-not-propagated` and
  `sse.scope.write-through-event-on-origin` hold PageLove's expectations and
  keep failing locally until includes exist, as do the Sessel cases (`sessel`
  not implemented).
- **OPTIONS ignoring denies.** Live advertises methods that a same-tier Deny
  removes (`authz.method.options-not-gated` observation). pagelike keeps
  `Allow` consistent with what it enforces.
- **Transient-element writes.** `sse.scope.transient-write-no-event` passed live
  (no event), but pagelike has no transient-element store yet.
- **Retention cases.** `sse.replay.within-retention` and
  `sse.replay.expired-after-retention` take 10+ minutes. Both pass locally
  (`--slow`) and were not re-run live.
- **Stale javascript spec text.** `docs/spec/javascript.md` R-JS-4 (415 for a
  JavaScript QUERY body) and R-JS-102 (412 for GET with a stale `If-Match`)
  need the same supersession notes as their reading-writing and protocol
  counterparts. That file was not in scope, but its cases were updated.


## Integration decisions (2026-09-29)


Cross-area decisions taken when harness cases (or the specs behind them)
disagree. Each entry: the claims, the evidence for each, the decision, and
the cases that pin it. The losing claim keeps a case marked
`status: disputed` (run live only) so a live run can settle it; the
per-area specs under `docs/spec/` keep their own contradiction tables.

### D-1 (2026-09-29) Selector reads of parameterized-route URLs

- Claims:
  - A `GET` with `Range: selector=…` to a concrete route URL (no literal
    document at the path) answers **404**: routes resolve only for
    whole-document reads. Case `comp.route.selector-get-404`.
  - Such a read resolves the route and answers **206** with the composed
    element. Implied by `sessel.bind.route-params` and
    `liquid.request.route-params`, which read `main` of a route page by
    selector to check `request.params`.
- Evidence:
  - 404: documented, the Parameterized-Routes error table (spec composing
    R-COMP-107, R-COMP-101: resolution runs "only for a whole-document GET",
    and for selector writes).
  - 206: inferred only. The Sessel and Liquid cases cite the
    Parameterized-Routes example and "Reading captured parameters" sections,
    which document `request.params`, not selector reads; the Range header
    was a convenience of the case author. The composing spec notes that
    selector writes do resolve routes and that beta-js reads elements of the
    current page by selector (contradiction C-12, probe Q-13), which makes
    the 206 reading plausible but not documented.
- Decision: 404 (the documented table). The Sessel and Liquid cases keep
  their documented claims (params exposure, percent-decoding) and now read
  the whole document; the 206 claim is kept as the disputed case
  `comp.route.selector-get-resolves` for a live run to settle Q-13.
- Also fixed in `liquid.request.route-params`: the percent-decoding step
  wrote `%20` in the case path, which the runner escapes again (`%2520`);
  it now writes a space, as `comp.route.percent-decoded` does.

### D-2 (2026-09-29) Shared anonymous sessions in cases

- Claims: the transient-element cases (composing R-COMP-110..117) assumed an
  anonymous client keeps its session cookie across steps; SSE cases rely on
  `anonymous` carrying no cookies (each stream is a fresh visitor).
- Decision: both hold, as two identities. `anonymous` stays cookie-less;
  the new built-in `visitor` is anonymous with a cookie jar for the whole
  case run (harness/README.md, Built-in identities). The transient cases
  whose steps must share one session run as `visitor`.

### D-3 (2026-09-29) Overloads with absent parameters (method elements)

- Claims: modeling R-MOD-59 ("the caller gets the overload whose declared
  parameters are all supplied") versus composing R-COMP-61 / Method-Elements
  "an absent attribute is null" (case
  `javascript.methods.absent-parameter-is-null`, probe P-JS-11).
- Decision: a fully supplied overload wins (largest first); when none is
  fully supplied, the first declared overload of the most-derived declaring
  schema runs with the missing parameters null. doesNotUnderstand only
  catches names no schema of the chain declares. This is what composition
  did before it dispatched through the schema registry
  (`schema.Registry.FindMethod`), so no case changes outcome.
