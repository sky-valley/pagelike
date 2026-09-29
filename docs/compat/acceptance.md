# Release acceptance suite — results

The scenarios of `docs/spec/apps.md` §7 (and the upstream-test runs of §8),
implemented as Playwright tests under `e2e/tests/apps/` (one file per app),
run against a locally built pagelike with system Chrome (`channel: chrome`).
Changes made to apps are recorded in `docs/compat/app-changes.md`.

- Run: `cd e2e && npx playwright test tests/apps` (the whole e2e suite:
  `npx playwright test`). Needs `research/` (the pinned upstream checkouts and
  the docs snapshot; found in this checkout or an ancestor directory, or at
  `$PAGELIKE_RESEARCH`). ACC-BJ-1, ACC-UP-1 and the §8.1 run install npm
  packages once into `e2e/.cache` (skipped when the registry is unreachable).
- Status column: `node e2e/tools/acceptance-status.mjs <playwright json report>`
  prints it from a run (see the tool's header).
- Tiers as in apps.md §6.1. ACC-BJ-3 is tier A there (§7.9 labels it B).
- Results below: run of 2026-09-29 on the reference laptop (Apple M1 Pro),
  then rerun after the LO-15
  document model and the 2026-09-29 reconciliations were merged (82 passed,
  3 skipped: the opt-in live migration demo). Rows changed by that rerun say
  so.

## Summary

| Tier | Scenarios | Pass | Fail | Not run |
|---|---|---|---|---|
| A — release-blocking core | 34 | 34 | 0 | 0 |
| B — release-blocking full | 36 (+ the §8.1 upstream run) | 36 (+ §8.1: 3/3); ACC-AT-4b with a recorded deviation | 0 | 0 |
| C — extended | 7 | 4 | 0 | 3 (ACC-SH-7, ACC-UP-4, ACC-UP-5) |

Counting: tier A = ACC-00, FA-1..4, PO-1..7, KB-1..12, DM-1..6, BJ-1,
BJ-3..5; tier B = BL-1..7, AT-1..7 (AT-4b is part of AT-4), AT-9, SH-1..6,
SH-8..10, BJ-2, RC-1..3, RC-5..9 (RC-7 and RC-8 are BJ-5 and BJ-4, run
once), UP-1..3; tier C = KB-13, KB-14, AT-8, SH-7, UP-4, UP-5 and RC-4
(its own row says tier C).

## Scenarios

| Scenario | Tier | Status | Notes |
|---|---|---|---|
| ACC-00 | A | pass | Shop `deploy-pagelove.sh`: unmodified, it stops at "Seed read-back mismatch" because `content=""` is stored as `content`, as on PageLove (LO-15). With the seed in stored form (app-changes.md A7) it runs twice: every `VERIFY … -> exact match`; the second run's MKCOLs answer 405, which the script accepts; the seed is left unchanged and a direct `If-None-Match: *` PUT → 412. beta-js `sync-webdav.sh --all` twice and `DRY_RUN=1` |
| ACC-FA-1 | A | pass | OPTIONS `/` → 207 with `selector #todo-list li` (PUT, DELETE) and `selector #todo-list` (POST); one `PUT /` `selector=#todo-list > li:nth-child(1)` |
| ACC-FA-2 | A | pass | Invoker Commands button; POST appends the response; the new item's PUT is `li:nth-child(4)` |
| ACC-FA-3 | A | pass | |
| ACC-FA-4 | A | pass | Flat 204 OPTIONS; `li.PUT is not a function` (the expected TypeError); no write sent; direct PUT → 401 error document |
| ACC-BL-1 | B | pass | JavaScript disabled; no Liquid, `r:`/`p:` or `xmlns:` in the source |
| ACC-BL-2 | B | pass | Printed template (C-16): `HEAD /posts/nonsense.html` → 200, recorded (substring semantics, not release-blocking). Variant stylesheet: HEAD → 404, hello-world → 200, GET → 404 with "Not found" |
| ACC-BL-3 | B | pass | One `August 2026` heading; feed XML type, `<?xml`, entries newest first; September post adds a heading and a third entry |
| ACC-BL-4 | B | pass | Write-through to `data/posts/hello-world.html`; route template byte-identical afterwards |
| ACC-BL-5 | B | pass | Attack 2xx before the constraint, 422 after; honest multi-paragraph comment 2xx |
| ACC-BL-6 | B | pass | Anonymous data read 401, home still composes, `alice` (users) 200 |
| ACC-BL-7 | B | pass | Draft absent from home, archive and feed; HEAD → 404 |
| ACC-PO-1 | A | pass | 301 `Location: /polls/<10>.html`; stored form has no `<base>`/`p:template`, keeps `xmlns:p` |
| ACC-PO-2 | A | pass | |
| ACC-PO-3 | A | pass | `Pagelove-Connection` equals B's stream token; one Grace row in B; A updated live with totals and "Best" |
| ACC-PO-4 | A | pass | PUT answers the row (206); live replace and removal in A; localStorage cleaned |
| ACC-PO-5 | A | pass | Second tab of the same session gets the row; the writer does not see it twice |
| ACC-PO-6 | A | pass | (a) 422 (b) 422 (c) 403 (d) 403 (e) 409 (f) 401 (g) 416 (h) 2xx; poll unchanged |
| ACC-PO-7 | A | pass | The drop is simulated by the test: the page's first stream is answered by the test with an early event id (`v1~…` shape) and ends; the reconnect is held while a response is posted and the site's events are pruned (`pagelike events prune --older-than 0s`), then goes to pagelike with that `Last-Event-ID` (set explicitly: Chromium does not surface the id of an intercepted stream) → `reset` → reload shows the current rows |
| ACC-KB-1 | A | pass | Open-mode site has no identity provider and no accounts: `/auth/login` → 404 NoIdentityProvider, the documented no-provider answer (R-PERM-67). ACC-KB-14 covers the sign-in form |
| ACC-KB-2 | A | pass | `#whoami-server` `\|\|\|\|`; no `pagelove:template`/`xmlns:pagelove` in the served board; member upsert PUT → 416 → POST append |
| ACC-KB-3 | A | pass | |
| ACC-KB-4 | A | pass | No echo checked on the SSE payloads themselves (each side receives the other's writes, never its own). §7.5's "no `?sync` after its own write alone" is not an app property: `PL.onWrite` schedules a self-served re-read after every write (`app.js:289-296`) |
| ACC-KB-5 | A | pass | Real HTML5 drags; exactly one MOVE and one verification GET (206) per drop (W-1) |
| ACC-KB-6 | A | pass | 24/24 MOVEs 2xx, 24 verification reads (none retried), every card once |
| ACC-KB-7 | A | pass | Card and list archive/restore |
| ACC-KB-8 | A | pass | 3 container POSTs, 5 adds, no 416 retries (W-2), 3 activity POSTs |
| ACC-KB-9 | A | pass | 50 KB PNG pasted, 8 MB file attached; bytes and types identical |
| ACC-KB-10 | A | pass | The `pagehide` keepalive DELETE is not reported to the automation layer; verified by the stored document losing `#V-u-bob` and A dropping B's face |
| ACC-KB-11 | A | pass | |
| ACC-KB-12 | A | pass | The app's own `checkVersion()` invoked instead of waiting 60 s |
| ACC-KB-13 | C | pass | ~1.1 MB board (400 cards, 60 activity entries). On the LO-15 document model (built from tokens): whole-document GET p95 23.7 ms (≤ 50), selector write p95 69.6 ms, MOVE p95 44.4 ms (both ≤ 150) in the kanban suite. Server side: `BenchmarkComposeKanbanBoard` 12.7 ms, `BenchmarkSelectorPutKanbanBoard` 41 ms (were ~46 and ~90 ms on the HTML5 tree). The test still reports the numbers and declares `test.fail` if a budget is missed |
| ACC-KB-14 | C | pass | Local accounts. `#whoami-server` is `alice\|Alice\|alice@example.com\|\|alice@example.com users `: the role list follows R-PERM-74 ([verified email] + groups + `users`), not §7.5's empty list. Rules needed an extra asset read rule (app-changes.md) |
| ACC-AT-1 | B | pass | |
| ACC-AT-2 | B | pass | `<table id="all-roles-data" hidden>` directly followed by `<tbody id="roles-body">`; broken `xmlns:example co=…` attribute handled |
| ACC-AT-3 | B | pass | 1 MB PDF stored byte-for-byte as `application/pdf`; anonymous CV read 401 |
| ACC-AT-4 | B | pass | Anonymous 401 with login link; admin1 200 and CV 200; bob 403 |
| ACC-AT-4b | B | pass (deviation) | Per-email rules removed: admin3 still 200 through the `admins` Group. Group removed too: **403**, because pagelike ignores `GroupMembership` items by design (permissions R-PERM-7/R-PERM-61, contradiction C7, fail closed) whereas §7.6 expects 200. The ATS itself does not depend on it |
| ACC-AT-5 | B | pass | Stage PUT → 206 with the new row; activity and interview POSTs answer the row; DELETE 2xx; no stray "null" |
| ACC-AT-6 | B | pass | (a) 422 (b) 401 (c) 401 (d) 401; document unchanged |
| ACC-AT-7 | B | pass | |
| ACC-AT-8 | C | pass | Outbox trigger relays to a local sink: one POST, JSON type, `Accept`, empty `X-Postmark-Server-Token`, body byte-identical |
| ACC-AT-9 | B | pass | The admins rule's selector list is reported as one part `selector tbody, tr[itemtype]` (R-PERM-63 reports a rule's selector as written); it covers both. Anonymous: only `tbody#candidates-body` with POST |
| ACC-SH-1 | B | pass | Six products sorted, emoji fallbacks, stamped tee with five variants, unknown slug 200; no 401 challenge on shopper pages |
| ACC-SH-2 | B | pass | Needed a runtime fix: transient write responses now carry `Cache-Control: private` |
| ACC-SH-3 | B | pass | |
| ACC-SH-4 | B | pass | 401 + `WWW-Authenticate: Basic realm="Pagelove Shop admin"`; answered challenge → 200 with admin links from Liquid; wrong password 401; HEAD 401 |
| ACC-SH-5 | B | pass | With the admin browser sending the Basic header on every request (app-changes.md): Chromium only pre-emptively sends it under `/admin/`, so the shop's product and image writes fail in Chromium with its own trigger's 401 — an app/browser issue, not pagelike |
| ACC-SH-6 | B | pass | |
| ACC-SH-7 | C | not run | Needs the TLS front end, the celld worker and a Stripe stub (§6.3) |
| ACC-SH-8 | B | pass | Uses the worker's own `orderDocument()` output |
| ACC-SH-9 | B | pass | Paid → Fulfilled via the admin UI; `shipped` → 422; public order page stamps it, data file 401 |
| ACC-SH-10 | B | pass | |
| ACC-DM-1 | A | pass | |
| ACC-DM-2 | A | pass | |
| ACC-DM-3 | A | pass | After a reload the app's own guard sends nothing; the server's unique key refuses a stale page's second reaction (422) |
| ACC-DM-4 | A | pass | |
| ACC-DM-5 | A | pass | |
| ACC-DM-6 | A | pass | |
| ACC-BJ-1 | A | pass | beta-js `npm test`: 12 tests, 0 failures |
| ACC-BJ-2 | B | pass | `verify-deploy.sh`: "Verified 6 module(s)"; served bytes equal stored; without `cors.html` the script fails on the missing header |
| ACC-BJ-3 | A | pass | FA-1..3 again with beta-js fetched from the `betajs` site: every module 200, `text/javascript`, `Access-Control-Allow-Origin: *` from `cors.html` |
| ACC-BJ-4 (= RC-8) | A | pass | As printed (a `<table>`), the server event reaches `sse.mjs` and `PLMutationApplied` fires with the write's selector, but beta-js@c204746 parses events with `DOMParser`, which drops a `<td>` outside a table, so the cell becomes bare text (client limitation; the server sends the body verbatim, R-SSE-8). With the records as `<div>`/`<span>`: re-render with the new value, `preventDefault()` keeps the old DOM, the writer's own write fires nothing |
| ACC-BJ-5 (= RC-7) | A | pass | Space-form `Content-Range: selector h1`; GET's ETag sent as `If-Match` → 206; stale tag → 412. The printed example binds `new PLDocument('/about.html')`, which beta-js does not treat as the current page (it compares with the absolute URL), so the live `<h1>` is wired through `new PLDocument()` |
| ACC-RC-1 | B | pass | |
| ACC-RC-2 | B | pass | Reference-page vocabulary (app-changes.md) |
| ACC-RC-3 | B | pass | Unverified e-mail never matches; membership swap flips the next requests |
| ACC-RC-4 | C | pass | Dispatched after the response; retry 3 → 4 attempts with growing gaps |
| ACC-RC-5 | B | pass | |
| ACC-RC-6 | B | pass | Composite uniqueness works (W-19) |
| ACC-RC-9 | B | pass | Wedge shown with a whole-document write (a selector write names its element, R-REACT-69) |
| ACC-UP-1 | B | pass | beta-js's jsdom + `sse.mjs` set-up fed by a real pagelike stream; POST, PUT, DELETE, MOVE applied; DOM equals the stored element |
| ACC-UP-2 | B | pass | Service Worker script served as JavaScript; every assertion of `test.html` passes |
| ACC-UP-3 | B | pass | Worker `node --test` passes once the stale import path is satisfied (app-changes.md); `orderDocument()` output accepted over dav |
| §8.1 demo-apps suite | B | pass | `tests/demo-pages.spec.js` unchanged against pagelike: 3/3 (axe WCAG A/AA, no console errors, < 3 s, < 1.5 MB, no-JS render, `javascript:` guard) |
| ACC-UP-4 | C | not run | `check-live.py` needs HTTPS on port 443 |
| ACC-UP-5 | C | not run | Needs a GitHub environment (`act`) |

## Workarounds exercised

| ID | Exercised by | Result on pagelike |
|---|---|---|
| W-1 | KB-5, KB-6, KB-7 | Every MOVE lands; one verification read each, never a retry |
| W-2 | KB-8 | No 416 on posts into just-written elements |
| W-3 | KB-4 | Mutation events reach the other session only |
| W-4 | KB-12 | `version.txt` read fresh (`no-store` + buster); no nag. pagelike serves static assets `public, max-age=300` as documented (R-RW, harness `rw.uploads.*`) |
| W-5 | KB-13 | Selector writes on a 1.1 MB board ≈ 0.1–0.2 s at p95 (the app reports ~1 s on PageLove); the R-APPS-17 budgets are borderline (above) |
| W-6, W-7, W-8, W-18, W-25 | — | Not exercised (no behaviour to check) |
| W-9 | AT-4, AT-6 (d) | Selector GET of the private admin document → 401 |
| W-10 | AT-4b | `admins` Group resolves; `GroupMembership` deliberately ignored |
| W-11 | AT-3, AT-6 | Selector rules enforced on writes; 401 carries a login link; no truncation; PDF type kept |
| W-12 | AT-8 | Trigger + HttpRequest relays server-side |
| W-13 | SH-4, SH-5 | Lower-case header lookup finds the credential; the gate holds |
| W-14 | SH-8 | Closed shape with the root permitted accepts real orders, refuses `<script>` |
| W-15 | SH-1 | `!= blank` emoji fallback |
| W-16 | SH-1, SH-4 | No shopper page is challenged; admin links from Liquid |
| W-17 | SH-2, SH-3 | Read-modify-write PUT of `#basket` |
| W-19 | RC-6 | Composite uniqueness enforced |
| W-20 | DM-2 | Client-side waitlist promotion PUT |
| W-21 | BL-*, PO-1 | Liquid composition |
| W-22, W-23 | DM-4 | Same-value claim is not a transition; the Claim unique key refuses the second claimant; entry rule present |
| W-24 | DM-5 | `*` Deny + `users` Allow at different tiers |
| W-26 | BJ-2, BJ-3 | JavaScript processor: headers honoured, body byte-exact |
| W-27 | PO-3, PO-5, BJ-4, UP-1 | No echo to the originating session/connection |
| W-28 | FA-1, BJ-5 | Space-form `Content-Range` |
| W-29 | ACC-00 | MKCOL on an existing collection → 409 + DirectoryAlreadyExists (superseded live: 405, which the script also accepts) |
| W-30 | BJ-2 | Bytes, type and CORS verified on the public host |
| W-31 | PO-3, PO-4 | Literal `<div itemprop="body">` body sliced by the app |

## Runtime fixes made for this suite

| Fix | Pinned by |
|---|---|
| Writes into a transient element (`p:transient`) answer with `Cache-Control: private` (their body is the requester's session copy; R-COMP-114, ACC-SH-2) — `internal/compose/write.go` | harness case `comp.tr.write-response-private` |

Test infrastructure (not runtime): the e2e state file is per checkout so
concurrent runs in other worktrees cannot redirect a run to their server,
and teardown waits for the server to exit.

## Known gaps and deviations (not failures of the apps)

- **ACC-KB-13 budgets** (tier C): met since the LO-15 document model (see
  the table). Every request still parses the whole stored document; a
  parsed-document cache per version would add headroom for larger boards.
- **ACC-AT-4b GroupMembership-only**: pagelike follows the permissions area's
  fail-closed decision (C7); apps.md C-10 wants the legacy table honoured.
  One of the two specs needs reconciling.
- **ACC-KB-14 role list** and **ACC-AT-9 part shape** follow the owning areas
  (R-PERM-74, R-PERM-63) where §7 is written differently.
- Client/app issues found (not pagelike): beta-js drops `<td>`/`<tr>` bodies
  in `sse.mjs` (ACC-BJ-4); the primitives recipe's relative `PLDocument` URL
  wires nothing (ACC-BJ-5); the widget recipe's two module scripts cannot
  share functions (ACC-BJ-4); the shop worker test imports a moved path
  (ACC-UP-3); the shop's admin writes outside `/admin/` do not carry the Basic
  credential in Chromium (ACC-SH-5).
