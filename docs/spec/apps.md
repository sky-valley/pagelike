# Apps — acceptance inventory and release acceptance suite

Area: `apps`. Cases: `harness/cases/apps/` (HTTP-level slices of the app flows).
Browser suite: `e2e/tests/apps/*.spec.mjs` (one file per app; helpers in
`e2e/lib/apps.mjs` and `e2e/lib/pagelike.mjs`). Results:
`docs/compat/acceptance.md`; changes made to apps: `docs/compat/app-changes.md`.

This document inventories every official PageLove application, tutorial and
client library we have source for, lists every server behaviour each one
depends on (with file:line references), records the workarounds they carry for
past platform bugs, and defines the finite **release acceptance suite**: the
end-to-end scenarios that prove pagelike runs each app unmodified.

It does not restate protocol details owned by other areas. Where an app relies
on a behaviour already specified elsewhere, it cites that requirement
(`R-RW-*` reading-writing, `R-PROTO-*` protocol, `R-SSE-*` sse, `R-PERM-*`
permissions-identity, `R-MOD-*` modeling). Behaviours that only the apps pin
down, or that no area owns yet, are specified here as `R-APPS-*`.

Keywords MUST / SHOULD / MAY are normative for pagelike.

## 0. Sources

Docs (Markdown snapshot `research/docs/2026-09-28/md/`). The learn and recipe
pages have no counterpart in the combined `all_*` pages, so there is no
combined-vs-individual difference to report for them. For the reference pages
cited here (Resource-Creation, Includes, Parameterized-Routes, Stamp,
Transient-Elements, Trigger, Processor) the combined
`all_reference_composing-pages` / `all_reference_reacting-to-changes` sections
were compared and are identical apart from the wrapping header `<div>`.

| Tag | Page |
|---|---|
| D-L1 | `learn/your-first-app` (shopping list) |
| D-L2 | `learn/build-a-blog` (Field Notes) |
| D-R-TX | `recipes/transforming-data` |
| D-R-LINK | `recipes/linking-related-data` |
| D-R-GRP | `recipes/group-based-permissions` |
| D-R-HOOK | `recipes/sending-a-webhook` |
| D-R-PAGE | `recipes/paginating-a-list` |
| D-R-CU | `recipes/composite-uniqueness` |
| D-R-PRIM | `recipes/reading-from-the-primitives-layer` |
| D-R-WID | `recipes/embedding-a-third-party-widget` |
| D-R-SM | `recipes/declaring-a-state-machine` |
| D-RC | `reference/composing-pages/Resource-Creation` |
| D-INC | `reference/composing-pages/Includes` |
| D-ROUTE | `reference/composing-pages/Parameterized-Routes` |
| D-STAMP | `reference/composing-pages/Stamp` |
| D-TR | `reference/composing-pages/Transient-Elements` |
| D-TRIG, D-PROC | `reference/reacting-to-changes/Trigger`, `Processor` |
| D-LIQ | `languages/liquid` (templating, filters incl. `random`) |
| D-SX | `languages/sessel/reference/syntax` (§request.auth in composition) |
| D-SXE | `languages/sessel/reference/element` (`@id` = source path + `#` + id) |

Code (`research/upstream/`, commits from `research/COMMITS.txt`):

| Tag | Repository @ commit | What it is |
|---|---|---|
| POLLS | `pagelove-polls@c9270e5` | Template: scheduling polls |
| KANBAN | `pagelove-kanban@85109ab` | Template: Trello-like boards |
| ATS | `pagelove-ats@8f200fc` | Template: applicant tracking ("Hiring by Pagelove") |
| SHOP | `pagelove-shop@d887054` | Template: storefront + celld/Stripe worker + ops scripts |
| DEMO | `demo-apps@c4dd883` | Five demos, gallery, Playwright tests, deploy scripts |
| BJ | `beta-js@c204746` | Official browser client (+ node tests, WebDAV deploy scripts) |
| PP | `pagelove-primitives@e73986d` | Predecessor of `beta-js/pagelove/primitives.mjs` |
| DCORE, DFORMS, DPRIM, DSUB, SELREQ | `dom-core@26d124f`, `dom-forms@ab7ddef`, `dom-primitives@1c395f8`, `dom-subscriber@6b77709`, `selector-request@f770fb3` | Legacy client libraries (CDN `cdn.pagelove.net/js/<module>/<sha>/`) |
| PLMETA | `pagelove@deb2857` | Meta repo: submodules `js/*` + agent prompt files |

Earlier notes (hypotheses, cited only where they add an observation):
private research notes of 2026-09-28 (not published) —
`understanding.md` (N-UND), the captured blog post
`blog.pagelove.com_posts_building-our-own-hiring-platform.html.txt` (N-BLOG,
2026-08-12) and the captured production ATS role page
`ats.pagelove.com_roles_founding-software-engineer.html` (N-ATSLIVE, fetched
2026-09-28 17:39, a *live observation* of PageLove serving the ATS route page).

Out of scope (not in the task list or empty): `pagelove-cursor`,
`pagelove-marketplace`, `pagelove-dev` (agent skill; mined by other areas),
`crm-template`, `freelancer-invoicing-template`, `pantry-template` (empty
clones).

## 1. Inventory at a glance

| # | App | Kind | Server features exercised (short) | Identity model | Min. pagelike stage |
|---|---|---|---|---|---|
| A1 | Your first app | tutorial | GET, selector PUT/POST/DELETE, OPTIONS 207 (beta-js), in-page rules | none (`*`) | 1–2 |
| A2 | Build a blog | tutorial | CSS + Sessel bindings, Liquid, stamp, parameterized route, write-through POST, Processor (status rewrite), closed ShapeConstraint, `users` actor, XML composition | anonymous + `users` | 5–6 |
| A3 | Recipes ×9 | docs | unique/composite unique, references/cascade, groups, Processor+HttpRequest, pagination, primitives, sse.mjs events, transitions + handlers + dav repair | mixed | 3–6 |
| A4 | Polls | template | templated creation (POST→Liquid→`<base>`→301), bindings+Liquid, selector POST/PUT/DELETE, rule tables, closed shapes, Sessel triggers, SSE with connection token | client-only (localStorage) | 5 |
| A5 | Kanban | template | whole PUT create, selector PUT/POST(placements)/DELETE, 416-then-POST upsert, MOVE with `Destination-Range`, SSE as refetch signal, uploads, Liquid `request.auth.*` with `pagelove:` prefix, `/auth/login` | OIDC optional, else localStorage profile; **no rules shipped** | 3 (+5 for whoami) |
| A6 | ATS | template | rule tables incl. `Group`/`GroupMembership`, per-email rules, selector writes with response-echo, public write window on a private doc, closed shapes w/o `resource`, `<p:include>` inside `<table>`, directory-param route, blob uploads, Trigger + HttpRequest outbox | OIDC `admins` group (verified email); anonymous applicants | 3–6 |
| A7 | Shop | template | bindings+Liquid, Sessel bindings+stamp on routes, transient basket, Sessel triggers on GET and writes with Basic-auth gate, deny-overrides, enum schemas, closed shape, dav writes with preconditions by an external worker | none (Basic credential checked by trigger) | 5–6 |
| A8 | demo-apps (5 demos + gallery + tests) | demos | selector POST/PUT/DELETE, closed shapes (incl. composed), unique keys, `@key`, TransitionConstraint, Deny/Allow tiers with `users`, directory index, static assets | seeded picker (localStorage); `users` for one feed | 3–4 |
| A9 | beta-js | client library | OPTIONS 207, selector writes with `If-Match`, lazy HEAD ETags, SSE; self-hosting needs dav sync + JS Processor for CORS | n/a | 1–2 (+6 for self-host) |
| A10 | pagelove-primitives | legacy client | OPTIONS 207 (space-form `Content-Range`), selector GET/PUT/POST/DELETE | n/a | 2 |
| A11 | dom-core, dom-forms, dom-primitives, dom-subscriber, selector-request | legacy clients | OPTIONS (`=`-form `Content-Range` parsing), `Accept-Ranges` sniffing, PATCH (JSON), WebSocket (rustybeam) | n/a | smoke only |
| A12 | pagelove (meta) | prompts | none (documentation for agents) | n/a | — |

Stages refer to `docs/design.md` §Stages.

## 2. Per-app inventory

Format per app: purpose; files; server features (with refs); client library
usage and external URLs; identity assumptions; workarounds for past platform
bugs (register IDs `W-n`, §4); what must change to run on another origin.

### A1 — Tutorial: Your first Pagelove application (D-L1)

**Purpose.** A shopping list in one `index.html`: tick items (PUT), add
(POST), delete (DELETE), each gated by an in-page `AuthorizationRule`.

**Files.** One document, `/index.html` (the tutorial evolves it through six
versions; the final one is D-L1 lines 238–320, the DOMSubscriber variant lines
329–359).

**Server features.**
- Whole-document GET of `/index.html` (or `/`) → 200 (R-RW-20, R-RW-3).
- `OPTIONS` with `Prefer: return=representation`, `Accept: multipart/mixed`
  issued by `pagelove.mjs` at import (BJ `pagelove.mjs:43`,
  `pagelove/primitives.mjs:180-218`): the 207 parts' `Content-Range`
  selectors (`#todo-list li` with `PUT, DELETE`; `#todo-list` with `POST`)
  are what attach `li.PUT()`, `li.DELETE()`, `ul.POST()` to the DOM. Without
  a matching part the tutorial's `li.PUT()` is `undefined` and the handler
  throws (R-PROTO-16..21).
- Selector PUT with the beta-js generated selector: an `<li>` without id gets
  `#todo-list > li:nth-child(N)` (BJ `primitives.mjs:1-60`); body is the
  element's `outerHTML` after `toggleAttribute("checked")` (D-L1:67-79).
  Rule matching is on the key element (R-PERM-25), so `#todo-list li` must
  cover the `:nth-child` address.
- Selector POST to `#todo-list` with a string body (D-L1:153-164, 211-220);
  the client appends the response body (R-RW-72).
- Selector DELETE of the `li` (D-L1:200-206).
- Rules live in the same document, actor `*`, resource `/index.html`, method
  `PUT`+`DELETE` (two `method` metas, D-L1:225-234) and `POST` (D-L1:168-176),
  action lowercase `allow` (R-PERM-1..4, R-PERM-23).
- A page opened at `/` sends every request to `/` (BJ `primitives.mjs:423-427`
  uses `location.href` minus the fragment) while the rules name
  `/index.html` → R-APPS-6, R-PERM-19.
- Writes are refused without the rule (D-L1:81) → anonymous `401` (R-PERM-54).

**Client library / external URLs.**
`https://pagelove.github.io/beta-js/pagelove.mjs` (module, D-L1:62) and, in
the last step, `https://cdn.pagelove.net/js/dom-subscriber/cde4007/index.mjs`
(D-L1:331). Both are cross-origin module imports served by third parties.
The button uses the Invoker Commands API (`command`/`commandfor`), which needs
a browser that implements it.

**Identity.** None; every rule uses `*`.

**Workarounds.** None.

**Other origin.** No hostnames. The rule `resource` values assume the file is
at the site root; storing it elsewhere requires editing them. Offline runs
need the two external modules mapped to local copies (§6.2).

### A2 — Tutorial: Build a blog (D-L2)

**Purpose.** "Field Notes": posts are data files; home, archive, Atom feed and
per-post pages compose themselves; public comments pinned to one element and
sanitised by a closed shape; data folder hidden from direct reads.

**Files.** `data/posts/hello-world.html`, `data/posts/second-thoughts.html`
(D-L2:13-35, 76), `index.html` (45-68), `posts/:slug.html` (84-108, form
216-223, script 227-271), `processors.html` (122-146), `archive.html`
(162-185), `feed.xml` (193-196), `rules.html` (280-294 + 342-356),
`constraints.html` (314-332).

**Server features.**
- Resource binding `r:posts="[itemtype='https://blog.example/Post']"` over the
  whole site + `p:template="text/liquid"` on a `<section>`; Liquid filters
  `where`, `sort`, `reverse`, `limit`, loop, `size`, `assign`, comparisons of
  bound properties (`status`, `publishedAt` from `<time datetime>`, `slug`,
  `title`, `excerpt`, `displayDate`, `monthLabel`, `author`) (D-L2:56-65,
  174-182). Requires both `xmlns:p` and `xmlns:r` on `<html>` (D-L2:70).
  Composed output contains no Liquid, no `r:`/`p:` attributes and no `xmlns:*`
  (D-INC example; R-APPS-9).
- Parameterized route `posts/:slug.html` with `request.params.slug`
  (D-L2:80-82; D-ROUTE).
- Sessel expression binding on `<body>`:
  `e:post="${[itemtype='…Post']:has([itemprop='slug']:value-equals(request.params.slug)):has([itemprop='status']:value-equals('Published'))}.first()"`
  and `<p:stamp post>`; when nothing matches the stamp is removed
  (D-L2:95-112; D-STAMP §How the result is emitted).
- Processor on `resource /posts/*`, `method GET`, `status 200`, Sessel `when`
  `Context.response.body.contains("blog.example/Post") == false`, Sessel action
  `Context.response.status = 404` (D-L2:122-146). The tutorial checks it with
  `curl -sI` — a **HEAD** — and expects `HTTP/2 404` for an unknown slug and
  `200` for a real one (D-L2:152-156) → R-APPS-19.5. As printed, the route's
  own `<style>` names the Post itemtype, which defeats the substring test
  (C-16).
- XML document composition: `feed.xml` with `<?xml …?>` prolog, `p:template`
  and `r:posts` on the Atom `<feed>` root, Liquid `escape`, `first`
  (D-L2:193-196); fetched with `curl -s` (D-L2:200-202).
- Comment POST to the **route URL** with `Range: selector=#comments-<slug>`,
  `Content-Type: text/html`, body = serialized `<li class="comment" itemscope
  itemtype="https://blog.example/Comment">…`; on `res.ok` the client appends
  the same element (D-L2:260-268). The write routes through the stamp to
  `data/posts/<slug>.html` (D-L2:275; D-STAMP §write-through; D-ROUTE
  §Resolution — selector writes to route URLs).
- Rule: actor `*`, resource `/posts/*`, selector `[itemprop='comments']`,
  method `POST` (D-L2:285-291). Authorization is evaluated against the route
  page, not the data file (D-STAMP).
- Closed ShapeConstraint with two `resource` values (`/data/posts/*`,
  `/posts/*`) and six permits; an attack body `<li class="comment"><script>…`
  is 2xx before the constraint exists and `422` after (D-L2:304-336). The
  pre-existing script comment must be deleted before honest comments succeed
  again (R-MOD-65 rule 5).
- Lock rules: `* GET /data/* deny` + `users GET /data/* allow`; anonymous
  direct read refused, home page still lists posts because composition runs
  with elevated rights (D-L2:342-358; R-PERM-77).

**Client library.** None (inline module script with `fetch`).

**Identity.** Public anonymous; "signed-in users (you, in your editor)" for
the `users` rule (D-L2:358). Note the tutorial conflates the WebDAV editor
(authoring key) with a signed-in public-plane user; on pagelike the `users`
grant needs a real public-plane session (R-PERM-15), the dav plane bypasses
rules anyway (R-PROTO-111).

**Workarounds.** None.

**Other origin.** `feed.xml` hard-codes `https://field-notes.onpagelove.com/`
in `<link>`, `<id>` and entry URLs (D-L2:195); a different origin must edit
it (cosmetic for browsers, significant for feed readers). All other paths are
root-relative.

### A3 — Recipes (nine pages)

Recipes are feature demonstrations rather than apps. Each is mapped to the
owning area; §7.10 turns each into one acceptance scenario.

| Recipe | Server features | Owning area / notes |
|---|---|---|
| D-R-TX transforming data | Property `@write` Sessel resolver (lowercase slug), `@read` resolver (format date), `self.map`, `new meta[…]{}` construction, `parseDateTime().format("d MMMM yyyy")` | modeling R-MOD-49..51, sessel |
| D-R-LINK linking related data | `unique` true / group name, `references`, `onDelete` restrict (409) / cascade, `GroupConstraint` cardinality `1..n` | modeling R-MOD-30..43, R-MOD-54..57 |
| D-R-GRP group-based permissions | `https://pagelove.org/Group` name + `member` emails, verified-email gate, rule `actor editors`, membership edits take effect next request | permissions R-PERM-58..62 |
| D-R-HOOK sending a webhook | Processor `resource`/`method`/`selector`, action `HttpRequest` with `url`, `method`, `content-type`, Sessel `body`, `header` Pair, `retry` (0 → one attempt; n → n+1 attempts, exponential backoff), dispatched after the response | reactions (outbound-http) |
| D-R-PAGE paginating | `p:paginate="10"`, `?paginate:page=`, `?paginate:length=`, clamp to last page, `Link` headers + `<link>` elements, per-id paginators `paginate:<id>:page`, Range fragment after composition | composition |
| D-R-CU composite uniqueness | `unique` group name across two properties; fourth write 422; atomic | modeling R-MOD-31 |
| D-R-PRIM primitives layer | `PLDocument(url).OPTIONS()` (207), `new PLElement(url, el).GET()` (`Range: selector=#note-42` → fragment), `PUT()` with `If-Match` when an ETag is known | protocol R-PROTO-21, reading-writing |
| D-R-WID third-party widget | `pagelove/sse.mjs` loaded alone; `PLMutation` (cancelable, before DOM change) and `PLMutationApplied` (after) on `document` with `detail.selector` | sse R-SSE-* (client contract) |
| D-R-SM state machine | TransitionConstraint entry/step/exit rules, 422 `ConstraintViolation` body, 412 for a lost race, `@key` pairing, TransitionHandler `becomes` + HttpRequest (at-most-once), DELETE exit violation, WebDAV repair path (never transition-validated, never fires handlers), rules edited over dav take effect within 60 s | reactions, modeling, protocol R-PROTO-112 |

External URLs in recipes: `https://pagelove.github.io/beta-js/pagelove/primitives.mjs`,
`…/pagelove/sse.mjs`, `https://cdn.example.com/chart-library.min.js`
(placeholder), `https://example.com/webhooks/note-created`,
`https://worker.example.com/payments`, `https://myapp.example.com` (curl
examples).

### A4 — pagelove-polls (POLLS)

**Purpose.** Doodle-style scheduling polls: create a poll from a form, share
the link, everyone adds a row of yes / if-need-be / no votes, rows update live.

**Files (`site/`).** `index.html` (home: create form + recent polls),
`templates/new-poll.html` (Liquid template that becomes a poll page),
`polls/kfd47o4zqd.html` (sample poll with three responses), `admin/auth.html`
(rules, shapes, triggers), `assets/create.js`, `assets/poll.js`,
`assets/polls.css`. Repo root: `pagelove.html` (manifest, not installed),
`LICENSE`.

**Server features.**
- *Templated creation* (D-RC; R-APPS-13): the home form is `<form
  method="post" action="/templates/new-poll.html">` (`index.html:40`);
  `create.js` fills hidden fields and calls the native
  `HTMLFormElement.prototype.submit` so the browser POSTs
  `application/x-www-form-urlencoded` and **follows the redirect**
  (`create.js:1-2, 84-87` "the server answers 301"). The template has
  `p:template="text/liquid"` on `<html>` (`new-poll.html:2`), draws a 10-char
  id with `{% assign id = 10 | random: lower: true, digits: true %}`
  (`:6`), reads form fields as `request.body.title|organizer|description|options|listed|created|createdLabel`
  (`:7, 17-21, 42`), splits options with `split: ";;"` and `split: "|"`
  (`:20, 61`), and names its storage location with
  `<base href="/polls/{{ id }}.html">` (`:10`). The template writes its own
  itemtype as `itemtype="{{ 'https://pagelove.org/Poll' }}"` (`:34`), so the
  stored template is not itself a Poll item for the home page's site-wide
  binding, which reads stored markup including templates (R-COMP-31).
- Resource binding + Liquid on the home page:
  `r:polls="[itemtype='https://pagelove.org/Poll']"`,
  `where: "listed", "true" | sort: "created" | reverse`, `limit: 8`, and
  `poll['@id'] | split: "#" | first` as the poll URL (`index.html:84-97`) →
  R-APPS-10 (`@id` is `<path>#<id>`, path-relative, D-SXE).
- Selector GET per poll card: `fetch(url, {headers: {Range:
  'selector=#responses'}})` then counts `<tr>` in `<table>${html}</table>`
  (`create.js:90-103`) → 206 with the `<tbody>` (R-RW-25).
- Voting (`poll.js:77-124, 146-159`): POST `Range: selector=#responses`
  body one `<tr id="r-xxxxxxxx" itemscope itemtype="https://pagelove.org/PollResponse"><th scope="row" itemprop="name">…</th><td itemprop="vote" data-option="o1" data-vote="yes">yes</td>…</tr>`;
  PUT `Range: selector=#r-xxxx`; DELETE `Range: selector=#r-xxxx`. Every
  write carries `Pagelove-Connection: <token>` once the stream has sent one
  (`:80`). The client parses the PUT/POST **response body** as the new row and
  falls back to its own markup if empty (`:104, 113`).
- Status mapping shown to the user: `422` shape rejection, `401`/`403` not
  allowed, `416` row gone (`poll.js:87-92`).
- SSE: `new EventSource(location.pathname)`; `pagelove-connection` stores the
  token; `mutation` payloads are parsed by slicing the literal
  `<div itemprop="body">` … last `</div>` because a `DOMParser` would drop the
  `<tr>` (`poll.js:204-240`); `placement` values `prepend`/`before`/`after`/
  default append; `reset` → `location.reload()` (`:241`). All of this is
  specified in R-SSE-6, R-SSE-8, R-SSE-14, R-SSE-38.
- Rules as **table rows** (`admin/auth.html:27-70`): `<tr itemscope
  itemtype=AuthorizationRule>` with `<td itemprop="actor">`, multiple
  `<li itemprop="resource">`/`<li itemprop="method">`, an **empty**
  `<td itemprop="selector"></td>` meaning whole resource, and a selector with
  an escaped `&gt;` (`tbody#responses &gt; tr[itemtype="…PollResponse"]`)
  (R-PERM-2, R-PERM-2a, R-PERM-4). Grants: public GET/HEAD/OPTIONS on
  `/index.html`, `/polls/*`, `/assets/*`; POST on the template; document-level
  PUT on `/polls/*` (needed by templated creation); POST on `tbody#responses`;
  PUT/DELETE on response rows.
- Two closed ShapeConstraints (`:78-94`): on `tbody#responses` (permits for
  `tr[id][itemscope][itemtype=…]`, `th[scope="row"][itemprop="name"]`,
  `td[itemprop="vote"][data-option][data-vote]`) and on the row itself with a
  `constraint` `:has(> th[itemprop="name"])` (R-MOD-61..68).
- Two Sessel Triggers (`:99-150`) on `/polls/*`: (1) PUT/POST with a Range
  header: POST must have `Range` exactly `selector=#responses`, PUT exactly
  `selector=#r-[a-z0-9]{1,24}`, else `throw new HTTPResponse {status: 403,
  message: …}`; reads the header as `Context.request.headers["range"]`
  (lowercase) and uses `.matches(regex)`. (2) whole-document PUT (no Range):
  `(${[itemtype="https://pagelove.org/Poll"]} from "/polls/*").any(p =>
  p.path() == path)` → `409` "This poll already exists and cannot be
  replaced." The triggers exist because the document-level PUT grant needed by
  templated creation would otherwise let anyone replace any element or the
  whole poll (`:96-97`).

**Client library.** None; plain `fetch` + `EventSource`. External: Google
Fonts CSS (`index.html:8-10`, `new-poll.html:11-13`), links to
`https://pagelove.com/` and `https://docs.pagelove.com/`.

**Identity.** Client-side only. "Rows you save are remembered in this
browser" (`new-poll.html:106`; `poll.js:19-23` localStorage key
`polls-by-pagelove:own:<path>`). The server permits anyone to PUT/DELETE any
response row; ownership is cosmetic.

**Workarounds.** W-31 (SSE body sliced as a string). Not a platform bug but a
format dependency.

**Other origin.** None needed: every URL is root-relative
(`location.origin + POLL` for the share link, `poll.js:29`). The template must
be installed at the host root because `/polls/*`, `/templates/new-poll.html`
and `/assets/*` are hard-coded in rules, JS and Liquid. The shipped sample
poll (`polls/kfd47o4zqd.html`) has no `<base>` and no `p:template` on
`<html>` but keeps `xmlns:p` and the blank lines left by `{% assign %}` tags,
i.e. it looks like a stored templated-creation result (R-APPS-13).

### A5 — pagelove-kanban (KANBAN)

**Purpose.** Trello-like boards: home page of board tiles; one HTML document
per board holding lists, cards, labels, members, epics, goals, activity,
presence and archive; everything written as selector-addressed fragments.

**Files (`site/`).** `index.html` (home; empty `#boards`), `app.js` (3584
lines, classic script), `style.css`, `version.txt` (`1789423173`),
`img/*.svg`, `fonts/montserrat.woff2`. No rules document, no `admin/slack.html`
(referenced, not shipped).

**Server features.** The fragment client is `PL.req` (`app.js:6-25`):
- `Range: selector=<sel>` plus `; placement=<p>` **only for POST**
  (`:9`); `Content-Type: text/html` when there is a body (`:14`); any non-2xx
  is an error whose message ends in `→ <status>` (`:16`).
- Board creation: whole-document `PUT /boards/b-xxxxxxx.html` of a full
  document (`:554-556`, template `:642-692`), then `POST /index.html` with
  `Range: selector=#boards; placement=prepend` (`:557`), then navigate.
  Home-page writes always target the literal `/index.html` (`:542`).
- Upsert (`:51-61`): `PUT <sel>`; if it fails with `416` ("PUT only replaces
  what already exists", R-RW-66) → `POST <container>; placement=append`.
  Used for every card field (`[data-f="…"]` metas), labels, members, presence.
- Selector PUT on nested selectors such as `#C-x [data-f="card-title"]`,
  `[data-f="board-name"]`, `body > [data-f="digest-sent"]` (`:1869, 2411,
  1342`), POST to `body` (`:505, 885, 933, 2368`), POST into a card
  (`#C-x; placement=append`, `:1526`), DELETE of any `#id` (`:957, 2197,
  2346`), DELETE with `keepalive: true` on `pagehide` (`:514-516`).
- MOVE (`:10-12, 23`): `MOVE <board path>` with `Range: selector=#<id>`,
  `Destination: <same path>` (a path, not a URL), `Destination-Range:
  selector=<dest>; placement=append|before`. Used for drag and drop
  (`:3471-3486`), archive/restore of cards, lists, boards and goals
  (`:581, 592, 603, 1193, 1418, 3293, 3304, 3352`). Each MOVE is followed by a
  verification `GET <path>?v=<ms>` with `Range: selector=<container>`
  (`moveVerified`, `:41-49`) — W-1. Whole-document MOVE is never used.
- `postEventually` retries a POST up to nine times while it answers `416`
  (`:1554-1569`) — W-2.
- Live sync (`:242-405`): `new EventSource(path)`; every `mutation` or `reset`
  only schedules a debounced full `GET <path>?sync=<ms>` with
  `cache: 'no-store'`; a 30 s fallback poll (`:254, 403`). Relies on "the
  platform streams a mutation event to every subscriber EXCEPT the
  originator" (`:245-253`) → R-SSE-37.
- Uploads: `PUT /uploads/avatars/<sub>-<name>` (`:202-204, 2582-2584`) and
  `PUT /uploads/<card>-<ts36>-<name>` with the file's own `Content-Type`
  ("Verified up to 8 MB on this host", `:1539-1551`) → R-APPS-19.
- Liquid in a stored board document with a **non-`p` prefix**:
  `<html … xmlns:pagelove="https://pagelove.org/1.0">` and `<div
  id="whoami-server" hidden pagelove:template="text/liquid">{{
  request.auth.username }}|{{ request.auth.claims.name }}|{{
  request.auth.claims.email }}|{{ request.auth.claims.picture }}|{% for r in
  request.auth.role %}{{ r }} {% endfor %}</div>` (`:643, 653`), read back by
  splitting on `|` (`:136-146`) → R-APPS-9, R-APPS-10, R-PERM-74 (`role` vs
  `roles`, C-3).
- Login links `/auth/login` and `/auth/logout` (`:173, 186, 2613-2617`) →
  R-PERM-67, R-PERM-69.
- `GET /version.txt?v=<ms>` with `cache: 'no-store'` every 60 s (`:264-278`);
  `GET /admin/slack.html?v=<ms>` read as text and regex-scanned for
  `data-f="slack-webhook" content="…"` (`:1347-1353`).
- Writes to another document from a board: the background swatch also writes
  the home tile `PUT /index.html` `#b-x [data-f="bg"]` (`:2509`).
- Concurrency assumptions stated in comments: concurrent POST appends both land
  (`:911-914, 1500-1501`, R-RW-76); last-write-wins per field (`:28-40,
  728-731`).

**Client library.** None (classic script, no modules). External: none except
the Slack webhook URL configured in `/admin/slack.html` (browser POSTs
`text/plain` JSON directly to Slack, `:1356-1359`).

**Identity.** "No identity is invented for you" (`:125-126`): a signed-in
session's claims win (`serverIdentity`, `:136-146`); without one, a profile
(name + random `u-xxxxxxx` sub) saved in `localStorage['board:me']` is used;
with neither the page navigates to `/auth/login` (`:151-175`). Membership is
"opening a board" (`:2359-2361`). Comment ownership is "honest but cosmetic:
with the board still open to `*`, nothing stops a crafted request editing
someone else's comment" (`:2241-2245`). Elsewhere the code says "The server
denies anonymous reads anyway" (`:171-172`) and that `/admin/slack.html` is
"readable only by signed-in members" (`:1215-1217`) — so the deployed host had
rules that are **not in the repository**. pagelike acceptance must supply them
(§6.3).

**Workarounds.** W-1..W-8 (§4).

**Other origin.** No hostnames in code. Needs: a rules document (none
shipped); optionally an identity provider at `/auth/login` (Zitadel/WorkOS are
mentioned in comments, `:120-123, 2477`); optionally `/admin/slack.html`.

### A6 — pagelove-ats, "Hiring by Pagelove" (ATS)

**Purpose.** Applicant tracking: public careers page, per-role pages, public
application form with CV upload; private admin (roles, candidates, kanban,
interviews, activity, email templates, email sending).

**Files (`site/`).** `index.html` (careers), `apply.html`, `roles.html`
(public composed roles feed), `roles/:role_name/index.html` (route),
`admin/index.html` (3826 lines: data + UI + script), `admin/auth.html`
(groups, rules, shapes), `outbox/index.html` (Trigger → Postmark),
`README.html`. Rules also mention `/role.html`, `/role/*`, `/whoami.html`,
which are not shipped (`admin/auth.html:196-211, 273-322`).

**Server features.**
- Rule and group documents as tables (`admin/auth.html:56-97, 100-336`):
  `Group` `admins` with **duplicated** `member` metas (`:56-64`), a parallel
  legacy `GroupMembership` table (`:66-97`, R-PERM-61), rules keyed on
  `admins`, per-email fallback rules (`admin1@example.com` …), selector list
  `tbody, tr[itemtype]` for admin writes (`:132`), GET on both
  `/admin/index.html` and `/admin/` (`:114-127`, R-PERM-19), public
  `PUT /uploads/*` with admin-only reads (`:213-248`), admin-only
  `/outbox/*` (`:249-264`), and the **public write window**: actor `*`,
  `POST` on `/admin/index.html` selector `tbody#candidates-body`, independent
  of GET (`:324-333`).
- ShapeConstraints **without `resource`** (host-wide) whose `selector` and
  `constraint`/`permit` values are `<code>` text (`:340-409`); the candidates
  shape is closed with 17 element+attribute permits (`:351-380`); several
  permits match one element and their attribute coverage is united (e.g.
  `<a href class>` covered by `a[class]` + `a[href]`) (R-MOD-66).
- Admin writes (`admin/index.html:1855-1899`): raw `fetch` to the constant
  `ADMIN_DOC = '/admin/index.html'` "so writes hit the right file whether the
  page is loaded at /admin/ or /admin/index.html" (`:1855-1857`); PUT/POST
  parse the **response body** as the new `<tr>` and swap it in (`:1872-1874,
  1886-1888`); an empty body would insert the text `null` → R-RW-64, R-RW-72.
- Public reads: `/roles.html` includes `#roles-body` from the private admin
  page inside a `<table>` (`roles.html:13-15`); the careers page and the apply
  form read it with `Range: selector=#roles-body` (`index.html:412-418`,
  `apply.html:448`). The route `roles/:role_name/index.html` includes the same
  fragment into `<table id="all-roles-data" hidden>` (`:75`) and its script
  reads `#all-roles-data tr[itemtype]` (`:128-129`) → R-APPS-7, R-APPS-8.
  Production serves exactly that shape (N-ATSLIVE: `<table
  id="all-roles-data" hidden>\n<tbody id="roles-body">…</tbody>\n</table>` on
  `/roles/founding-software-engineer/`).
- Application (`apply.html:620-631, 680-683`): anonymous `PUT
  /uploads/<C-id>-cv.<pdf|doc|docx>` (≤ 5 MB client limit, `:520-521`), then
  `POST /admin/index.html` `Range: selector=tbody#candidates-body` with the row
  (`:495-515`).
- Outbox (`outbox/index.html:10-32`): a **Trigger** on `PUT
  /outbox/E-*.json` whose action is an `HttpRequest` to Postmark, headers as
  `Pair`s (one with an empty `content` attribute), body Sessel
  `Context.request.body`. Admin UI PUTs JSON blobs there
  (`admin/index.html:1690-1700`).
- Admin CV zip: `fetch(cvUrl, {credentials: 'same-origin'})` (`:2861, 2868`)
  and a dynamically loaded `jszip` from cdnjs (`:2820`).

**Client library.** None. External: Google Fonts, cdnjs jszip,
`https://api.postmarkapp.com/email` (legacy client path, token blank),
Gmail compose links.

**Identity.** Public anonymous applicants; staff via OIDC (`/auth/login`
links, `index.html:403`, route `:85`) matched to `admins` by verified email.
README: the public demo leaves `/admin/` readable ("open to anyone who has the
URL") because the host default-GET mode is `allow`; locking down means
"enabling default-get-authz-mode: deny" (`README.html` §banner, "Make it
yours").

**Workarounds.** W-9, W-10, W-11, W-12 (§4).

**Other origin.** `admin/index.html:2785-2786` hard-codes
`https://huge-stomp-7042.onpagelove.com/roles/<slug>/` as the displayed and
copied public URL — must be edited (functional links use `/roles/…`). The
route file's `<html>` carries a broken attribute `xmlns:example co="…"` from a
search-and-replace (`roles/:role_name/index.html:2`) that must not break
composition (R-APPS-9). Postmark token in `outbox/index.html:19`; admin emails
in `admin/auth.html`.

### A7 — pagelove-shop (SHOP)

**Purpose.** Storefront: catalogue, session baskets, Stripe Checkout through a
trusted checkout worker, order confirmation, Basic-auth admin for orders,
products, images and settings.

**Files.** `site/`: `index.html`, `products/:slug.html`, `basket.html`,
`checkout.html`, `orders/:id.html`, `admin/index.html`,
`admin/orders/:id.html`, `admin/products.html`, `admin/settings.html`,
`partials.html`, `rules.html`, `admin-auth.html`, `constraints.html`,
`schemas.html`, `data/products/{cap,hoodie,mug,stickers,tee,tote}.html`,
`data/settings/shop.html` (seed), `private/admin.example.html`,
`css/shop.css`, `js/shop.mjs`. `worker/` (celld/Cloudflare-style module:
`/checkout`, `/webhook`, `/health`; node tests). `ops/` (WebDAV deploy,
admin-password generator, celld/MinIO install, Stripe webhook setup).
`.github/workflows/deploy-pagelove.yml`.

**Server features.**
- `<p:include selector="#chrome" resource="/partials.html">` on every page;
  the included header contains a Liquid `<span p:template="text/liquid">{% if
  request.headers.authorization %}…admin links…{% endif %}</span>`
  (`partials.html:20-22`) → R-APPS-10, R-APPS-12.
- Catalogue binding + Liquid: `r:products=…`, `where: "available", "yes" |
  sort: "name"`, multi-valued `p.image | join: "," | split: "," | first`,
  `!= blank` (`index.html:17-31`); admin lists with `sort: "placedAt" |
  reverse`, `p.variant | join: ','` (`admin/index.html:17-22`,
  `admin/products.html:17-21`).
- Sessel bindings on routes + stamp: `products/:slug.html:13-16`,
  `orders/:id.html:14-17`, `admin/orders/:id.html:10-13`; settings stamped
  into `checkout.html:10-13` and `admin/settings.html:10-13` from a document
  the public cannot read (elevated composition, R-PERM-77).
- Transient basket `<ul id="basket" p:transient></ul>` (`basket.html:27`):
  read `GET /basket.html` `Range: selector=#basket` (session copy), write by
  read-modify-write `PUT … Range: selector=#basket` with body `<ul
  id="basket">…</ul>`, clear by `DELETE … Range: selector=#basket`
  (`shop.mjs:57-96`) (D-TR). The code avoids POST into a transient element
  (`basket.html:16-19`, `shop.mjs:3-10`; C-4).
- Rules (`rules.html`): public GET `/*`; PUT/DELETE `#basket`; PUT allowed on
  `/data/orders/*`, `/data/settings/*`, `/data/products/*`, `/images/*` for
  `*` (the trigger is the real gate); GET **deny** on `/data/orders/*`,
  `/data/settings/*`; method `*` deny on `/private/*` ("Deny wins over the
  broad public GET allow", `:52-58`) (R-PERM-40).
- Triggers (`admin-auth.html:19-59`): (1) `resource /admin/*`, **`method GET`**
  (so also HEAD, D-TRIG), `when` `(Context.request.headers["authorization"] ??
  "") == ${[itemtype="https://shop.example/AdminCredential"]
  [itemprop="authorization"]}.first().value()`, `otherwise` throws
  `HTTPResponse {status: 401, header: new Pair {key: "WWW-Authenticate",
  value: "Basic realm=\"Pagelove Shop admin\""}, body: "Authorization
  required"}`. (2) same `when` on PUT/DELETE of data/images paths, `otherwise`
  401 "Admin credential required to change shop data". Header names are looked
  up in lowercase "because HTTP/2 sends header names lowercased"
  (`:16-18`) → R-APPS-12, C-2.
- Schemas with enums as `https://schema.host/Enum` items (`schemas.html:8-30`),
  properties declared on `<div itemprop="property">`, `unique` slug/sku/orderId,
  a property with no `type` (`Order.lines`, `:252-256`), `image` typed Text
  because origin-relative paths fail `schema.host/URL` (`:92-93`).
- Closed ShapeConstraint on orders whose root must itself be permitted
  (`constraints.html:9-19`) (R-MOD-65 rule 3).
- Admin writes (`shop.mjs`): selector PUT `[itemprop="paymentStatus"]` on
  `/data/orders/<id>.html` (write without read permission, `:404-409`);
  whole-document PUT of a product with `Content-Type: text/html;
  charset=utf-8` (`:610-612`); image blob `PUT /images/<stem>.<ext>` with the
  file's type (`:489-502`); whole DELETE of a product treating `416` like
  success (`:637-639`); selector PUT of `[itemprop="checkoutEndpoint"]` on
  `/data/settings/shop.html` (`:695-698`). Every admin request carries the
  browser-cached `Authorization: Basic …` header.
- Checkout worker (`worker/src/index.js`): public `GET
  <SHOP_ORIGIN>/data/products/<slug>.html` with `Accept: text/html`,
  `redirect: 'manual'` (`:175-190`); orders over **WebDAV** with `Authorization:
  Bearer <key>` (`:239-249`): create `PUT` with `If-None-Match: *`, treat
  `409`/`412` as "exists" (`:251-268`); update by dav `GET` (reads `ETag`) then
  `PUT` with `If-Match`, retrying once on `409`/`412` (`:425-452`). The order
  document is built by `orderDocument()` (`:219-237`) and must pass the closed
  shape and the Order/OrderLine schemas.
- Ops (`ops/deploy-pagelove.sh`): `MKCOL` each ancestor accepting `2xx`, `405`,
  or `409` whose body contains
  `https://dombase.pagelove.team/ns/error/DirectoryAlreadyExists`
  (`:94-112`); seed files created with `If-None-Match: *` (`412` = keep)
  (`:143-176`); every deployed file read back over dav and compared
  byte-for-byte (`:233-253`) → R-PROTO-113..115.

**Client library.** Own module `js/shop.mjs`. External: the celld origin (from
settings), Stripe-hosted checkout and dashboard links.

**Identity.** "This host has no OIDC, so every actor is `*`" (`rules.html:5-7`).
Shoppers: anonymous with the platform session cookie (transient basket).
Admin: HTTP Basic credential compared by triggers; `private/admin.html` is
generated by `ops/deploy-admin-password.sh` from `owner:<password>`.

**Workarounds.** W-13..W-18 (§4).

**Other origin.** `worker/wrangler.jsonc` `SHOP_ORIGIN`
(`https://barn-hair-4926.onpagelove.com`), `SHOP_WEBDAV_URL`,
`STRIPE_API_BASE`; `.dev.vars.example` `SHOP_WEBDAV_URL=https://dav-barn-hair-4926.onpagelove.com/`.
The worker **refuses non-HTTPS** origins and backends except
`http://*.int.exe.xyz` (`index.js:36-41, 58-71`) and requires the browser's
`Origin` to equal `SHOP_ORIGIN` exactly (`:43-56`). The storefront's checkout
origin is data (Admin → Settings). README demo link. `private/admin.html` must
be generated for the new host, and `private/admin.example.html` must **not**
be installed next to it (§6.3, R-APPS-1).

### A8 — demo-apps (DEMO)

**Purpose.** Five self-contained demos plus a gallery; each "leans on a
different Pagelove mechanic" (`README.md:24-34`).

**Files.** `index.html` (gallery), `gallery.css`, `favicon.svg`,
`robots.txt`, `sitemap.xml`, `assets/{motion-core.js,ui-enhance.js,vendor/anime.umd.min.js,fonts/*,og-demo-apps.png,…}`,
`demo-0N-*/{index.html,app.js,styles.css,motion.js,schema/*.html}`,
`demo-04-accountability/feed.html`, `tests/demo-pages.spec.js`,
`playwright.config.mjs`, `scripts/{prepare-deploy.py,check-repo.py,test-deploy.py,check-live.py,serve-test.cjs,og-card.html}`,
`docs/LIVE-TESTING.md`, `.github/workflows/ci.yml`.

**Server features (all writes are `fetch` to the demo's own `index.html` /
`feed.html` with `Range: selector=…`; `Content-Type: text/html` only when there
is a body).**

| Demo | Writes | Rules | Validation |
|---|---|---|---|
| 01 event (`demo-01-event/index.html:101-176`, `app.js:44-162`) | POST `#pl-rsvps`; DELETE `#rsvp-<who>`; PUT `#rsvp-<who> meta[itemprop='rsvpStatus']` (waitlist promotion) | `*` GET page; POST `#pl-rsvps`; DELETE `[id^='rsvp-']`; PUT `[id^='rsvp-'] meta[itemprop='rsvpStatus']` | closed shapes on `#pl-rsvps` and `#pl-rsvps > li`; RSVP schema `rsvpKey` unique (`schema/event.html:41-55`); 422 body must match `/uniqueness/i` (`app.js:54`) |
| 02 show & tell (`index.html:56, 177, 190-300`, `app.js:48-190`) | POST `#pl-grid` (submission containing an empty `ul.comments`), POST `#pl-reactions`, POST `#comments-<id>` | `*` GET; POST `#pl-grid`, `.comments`, `#pl-reactions` | closed shapes on `#pl-grid`, `#pl-grid > li`, `.comments`, `.comments > li`, `#pl-reactions`, `#pl-reactions > li`; the `.comments` shape lies inside the `#pl-grid` shape (composed shapes, R-MOD-67); `reactionKey` unique |
| 03 resource exchange (`index.html:60, 135, 144-225`, `app.js:47-185`, `schema/listing.html`) | POST `#pl-listings`; PUT `#status-<listing>` (a `<span itemprop="status">`); POST `#pl-claims`; DELETE `#claim-<listing>` | `*` GET; POST `#pl-listings`, `#pl-claims`; PUT `.status-tag`; DELETE `.claim` | TransitionConstraints `→open`, `open→claimed`, `claimed→completed`, `claimed→open` (`listing.html:42-68`); `listingId` unique + `@key` via `<meta itemprop="@key" content="true">` (`:8`); Claim `listingId` unique is the real double-claim guard (`:71-79`) |
| 04 accountability (`feed.html:24-97`, `app.js:50-101`) | POST `/demo-04-accountability/feed.html` `#pl-checkins` | index: `*` GET only; feed: `*` GET **Deny** + `users` GET **Allow** + `*` POST `#pl-checkins` | closed shapes; `checkInKey` unique; anonymous feed GET must be **401** (`app.js:86-101`, `check-live.py:59-61`) |
| 05 professional (`index.html:88, 139, 173-275`, `app.js:67-217`) | POST `#pl-topics`, `.replies` (`#replies-<id>`), `#pl-resources` | `*` GET; three POST rules | three pairs of closed shapes; client blocks `javascript:` URLs before any request (`tests/demo-pages.spec.js:94-113`) |

Also: directory URLs `/demo-0N-*/` must serve `index.html` with 200
(`scripts/check-live.py:12-21`); assets are GET-able without rules, i.e. the
host default-GET mode is `allow` (R-PERM-44); vocabulary itemtypes are
`https://__PAGELOVE_HOST__/vocab/<Type>` rewritten per host by
`prepare-deploy.py` ("itemtype URLs register globally", `README.md:101`).

**Client library.** None (`app.js` IIFEs, vendored anime.js). Pages carry a
strict CSP meta (`default-src 'none'; script-src 'self'; connect-src 'self';
…`, e.g. `demo-01-event/index.html:6`) → R-APPS-3.

**Identity.** "The identity picker labels seeded demo users; it is not
authentication" (`README.md:203`); `users` (any signed-in account) for the
feed read (`README.md:45-66`).

**Workarounds.** W-19..W-25 (§4).

**Other origin.** Run `python scripts/prepare-deploy.py --host <hostname>`
(hostname only; the validator rejects ports and schemes,
`prepare-deploy.py:17-22`); it rewrites `__PAGELOVE_HOST__` in canonical/OG
URLs, sitemap, robots and itemtypes. `check-live.py` hard-codes `https://`
(`:50`).

### A9 — beta-js (BJ)

**Purpose.** The official browser client: `pagelove.mjs` (renderer, schema,
templates, bindings), `pagelove/primitives.mjs` (`PLDocument`, `PLElement`),
`pagelove/sse.mjs` (live DOM sync), `pagelove/component.mjs`,
`pagelove/debug.mjs`, vendored `pagelove/dom-subscriber.mjs` (identical to
dom-subscriber `cde4007`, header comment). Documented URL root
`https://pagelove.github.io/beta-js/` (docs cite `pagelove.mjs`,
`pagelove/primitives.mjs`, `pagelove/sse.mjs`, `pagelove/component.mjs`,
`pagelove/debug.mjs`).

**Server contract.** Owned by other areas: OPTIONS 207 parts and `Allow`
(R-PROTO-16..21), selector writes and their responses (R-RW-64, R-RW-72,
R-RW-73), ETag/`If-Match` (R-RW-85..91), MOVE (R-PROTO-90..106), SSE event
format and echo suppression (R-SSE-43 checklist).

**Tests.** `test/sse-echo.test.mjs`, `test/primitives-completion-order.test.mjs`
run under `node --test` with jsdom, a `FakeEventSource` and a scripted `fetch`
(`test/helpers/*.mjs`); no server is involved (§8.2).

**Self-hosting on a Pagelove host.** `.github/workflows/deploy.yml` syncs the
repo to a host over WebDAV (`.github/scripts/sync-webdav.sh`: `Bearer` key,
`MKCOL` treating `409` as "exists" after a `PROPFIND Depth: 0` → `207` check,
create-only `PUT` with `If-None-Match: *` for host-owned prefixes, `DELETE`
accepting `404`) and verifies the **public** host
(`verify-deploy.sh`: derive the public URL by stripping `dav-` from the
WebDAV URL; for each `.mjs` require `200`, `Access-Control-Allow-Origin` of `*`
or the probe origin, `Content-Type` `text/javascript*`/`application/javascript*`,
and bytes that `node --check` parses). CORS comes from `cors.html`, a
Processor on `/pagelove.mjs` and `/pagelove/*`, `method GET`, `status 2xx`,
whose **JavaScript module** action throws `{schema_url: HTTPResponse, status,
body: ctx.response.body, headers: {"Access-Control-Allow-Origin": "*",
"Content-Type": "text/javascript"}}` (`cors.html:50-70`) → R-APPS-22, W-26.

### A10 — pagelove-primitives (PP)

`index.mjs` is the ancestor of `beta-js/pagelove/primitives.mjs`: OPTIONS
(`Prefer: return=representation`, `Accept: multipart/mixed`), multipart parse
requiring `boundary=` as the **last** Content-Type parameter
(`index.mjs:106-116`), `Content-Range` parsed as `selector` + space-or-`=`
(`:131-137`, commit `bfd25de` "the space form the server actually sends"),
`PLElement.GET/PUT/POST/DELETE` with `Range: selector=<generated>`; **PUT
sends no Content-Type** (a string body becomes `text/plain;charset=UTF-8`,
`:190-199, 237-241`) — pagelike must parse selector-PUT bodies as HTML
regardless (R-RW-67). Imports
`https://cdn.pagelove.net/js/dom-subscriber/cde4007/index.mjs` (`:1`). Tests:
`test.html` + `test-sw.js` mock `fetch` in a Service Worker; the mock asserts
`PUT` returns `200` (`test.html:179`), so the suite cannot be pointed at a real
server (§8.3).

### A11 — Legacy libraries

| Library | Server-facing behaviour | pagelike stance |
|---|---|---|
| dom-core (`index.mjs`, `methods.mjs`, `multipart.mjs`) | Unfinished code (undefined `allowed`, `HTTPCan`, `processOptionsPart`); imports from site root `/dom-core/*.mjs`; OPTIONS 207 parsed with `selector=(.+)$` | Not an acceptance target (cannot run) |
| dom-forms | OPTIONS 207 parsed with `content-range … match(/selector=(.+)$/)` (`index.mjs:387-418`) → **incompatible with the space form** pagelike emits (R-PROTO-3); element `PUT` falls back to `GET` on failure; imports `https://unpkg.com/invokers-polyfill@latest/invoker.min.js` and CDN-relative `../../dom-subscriber/0.1.1/` | Documented incompatibility; no acceptance scenario |
| dom-primitives (`index.mjs`, `das-ws.mjs`) | Detects a "DOM-aware server" by `OPTIONS` → `Accept-Ranges` containing `selector` (`index.mjs:431-445`, R-PROTO-31); `window.server.can()` via `OPTIONS` + `Range` → `Allow` (`:500-556`); `HEAD` with `Range`; `PATCH` with JSON; WebSocket stream of `http://rustybeam.net/StreamItem` items (`das-ws.mjs`) | Smoke only: `Accept-Ranges` sniff and `Allow`; PATCH → 501 (R-SSE-25 decision); no WebSocket |
| dom-subscriber | Pure client (MutationObserver) | No server contract |
| selector-request | Pure client (`#(selector=…)` URL syntax parser) | No server contract |

### A12 — pagelove (meta repo)

Submodules `js/{dom-core,selector-request,dom-forms,dom-primitives,dom-subscriber,pagelove-primitives}`
and generated agent prompts (`prompts/*.md`, generated from
`prompts/src/*.liquid`). The prompts describe an older "Rust HTTP server"
surface (GET/PUT/POST/DELETE with Range, 206/416 codes) that other areas
already cover. No acceptance scenario.

## 3. Requirements derived from the apps (R-APPS-*)

Each requirement: behaviour; **Evidence** · **Confidence**; **Source**; edge
cases; statuses; cross-area flags. Requirements that merely restate another
area are not repeated; §3.9 lists the cross-references the acceptance suite
depends on.

### 3.1 Installing and hosting apps

#### R-APPS-1 — The installable unit is `site/`
A template repository (polls, kanban, ats, shop) is installed by copying the
contents of its `site/` directory to the host root, byte-for-byte, preserving
relative paths (including literal `:` in route file names such as
`products/:slug.html`, `roles/:role_name/index.html`). The repository-root
`pagelove.html` manifest (`<div itemscope itemtype="urn:console:Template">`
with `name` and `description` metas) describes the template and is never
installed. pagelike MUST provide an install path with this effect (WebDAV
upload of each file, or an equivalent `pagelike import --site <s> <dir>`), and
SHOULD read `urn:console:Template` `name`/`description` for display.
- **Evidence:** demo-source · **Confidence:** high
- **Source:** POLLS/KANBAN/ATS/SHOP `pagelove.html:7-20` (comment "only the contents of site/ are copied, and they land at the host root"); SHOP `README.md:375-383`; N-UND:105 (console shows an ATS/Kanban/Polls install ribbon on an empty host).
- **Edge cases:** the four manifests all keep the title "Pagelove Shop — template manifest" (copy-paste) — use the `name` meta, not `<title>`. SHOP ships `site/private/admin.example.html`; a blind copy installs a placeholder `AdminCredential` next to the real one (see R-APPS-26 for why that locks the admin out). The shop's own deploy list (`ops/pagelove-files.txt`) omits it; acceptance installs the shop with that list plus the seed file (§6.3).

#### R-APPS-2 — One app per site, served at the root
All apps use root-relative absolute paths (`/assets/…`, `/polls/*`,
`/boards/…`, `/demo-01-event/…`, `/admin/index.html`) in markup, JavaScript,
rules and Liquid. pagelike MUST serve each installed app at the root of its own
site (Host-routed, `docs/design.md` §Process layout); no path-prefix mounting is
required or supported for acceptance.
- **Evidence:** demo-source · **Confidence:** high
- **Source:** DEMO `README.md:87-89` ("absolute path … load-bearing"); KANBAN `app.js:542, 554`; POLLS `admin/auth.html:27-70`; ATS `admin/index.html:1855-1857`.

#### R-APPS-3 — Served HTML contains only stored or composed content
pagelike MUST NOT inject scripts, styles, markup, comments or `<meta>` into
served HTML (no dev banners, no live-reload script, no analytics). Composition
output (R-APPS-9) is the only transformation. Rationale: demo pages declare a
strict CSP (`script-src 'self'`, `style-src 'self'`, `connect-src 'self'`);
any injected inline script or style produces a CSP console error, and the
upstream Playwright suite fails a page on any console error.
- **Evidence:** demo-source · **Confidence:** high
- **Source:** DEMO `demo-01-event/index.html:6` (and the other demo pages); `tests/demo-pages.spec.js:41-68`.

#### R-APPS-4 — Content types for app assets
Static files stored by the apps MUST be served with these types (stored type
from the upload, else by extension; R-RW-60): `.js`/`.mjs` `text/javascript`
(module scripts and Service Workers are refused by browsers otherwise;
beta-js `verify-deploy.sh` requires `text/javascript*` or
`application/javascript*`), `.css` `text/css`, `.svg` `image/svg+xml`,
`.png` `image/png`, `.woff2` `font/woff2`, `.woff` `font/woff`, `.txt`
`text/plain`, `.xml` an XML type, `.json` `application/json`, `.pdf`
`application/pdf`. A `charset` parameter is allowed.
- **Evidence:** client-source + demo-source · **Confidence:** high (JS, CSS), medium (others)
- **Source:** BJ `.github/scripts/verify-deploy.sh:84-91`, `sync-webdav.sh` `content_type_for`; DEMO `scripts/serve-test.cjs:10-20` (the types the tests were written against); PP `test-sw.js` (Service Worker registration); N-BLOG ("PDFs coming back with the wrong content type" was a platform bug).
- **Cross-area:** reading-writing R-RW-60, protocol R-PROTO-114.

#### R-APPS-5 — Query strings never change which document is addressed
For whole and selector GET/HEAD, writes, OPTIONS and SSE subscriptions, the
query string is ignored when locating the stored document or route template
(apps append cache busters `?v=`, `?t=`, `?sync=`, `?cb=` and state such as
`?card=`, `?checkout=success`, `?role=`, `?via=`). The only query parameters
with server meaning are pagination's `paginate:*` (D-R-PAGE) and whatever a
template reads through `request.query`.
- **Evidence:** demo-source · **Confidence:** high
- **Source:** KANBAN `app.js:44, 267, 311, 1349, 1754`; ATS `index.html:413`, `roles/:role_name/index.html` apply link; SHOP `shop.mjs:237, 335-339`; POLLS `index.html:11` (`polls.css?v=20260924b`); DEMO `demo-01-event/index.html:18-23`.
- **Cross-area:** reading-writing R-RW-2; sse R-SSE-2.

#### R-APPS-6 — A directory URL is its `index.html` for every method
When a page is loaded at a directory URL (`/`, `/admin/`, `/demo-01-event/`),
beta-js sends OPTIONS, HEAD, selector PUT/POST/DELETE, MOVE (`Destination`)
and the EventSource subscription to that directory URL. pagelike MUST treat a
path ending in `/` exactly like `<path>index.html` for all methods (not only
GET/HEAD, R-RW-3): same document, same events stream, same authorization path
matching (R-PERM-19), same ETags.
- **Evidence:** client-source (URL choice) · inferred (write semantics) · **Confidence:** medium
- **Source:** BJ `pagelove/primitives.mjs:423-427` (`PLDocument` URL = `location.href` without fragment); D-L1 (rules name `/index.html`); KANBAN `app.js:542` and ATS `admin/index.html:1855-1857` hard-code the `index.html` path "so writes hit the right file whether the page is loaded at /admin/ or /admin/index.html" — which suggests PageLove did not (or did not reliably) map directory URLs for writes. Open question P-APPS-4.
- **Edge cases:** a write to `/x/` whose `index.html` does not exist → `404`. The SSE stream for `/x/` and `/x/index.html` MUST be the same stream (a write through either URL is delivered to subscribers of both).

#### R-APPS-7 — Parameterized directory routes
A route template stored at `/a/:p/index.html` MUST answer `GET /a/<v>/`
(directory-index resolution applies first, then route matching on
`/a/<v>/index.html`, D-ROUTE §Resolution) and `GET /a/<v>/index.html`, with
`request.params.p = <v>` (percent-decoded). `GET /a/<v>` (no slash, no dot in
the last segment) SHOULD answer `301` to `/a/<v>/` when the route resolves
(R-RW-4 extended to routes; inferred). Authorization for the route page uses
the concrete URL (`/a/<v>/` and its `index.html` form, R-PERM-19).
- **Evidence:** demo-source · live-observed (N-ATSLIVE serves `/roles/founding-software-engineer/` from the template) · **Confidence:** high (trailing-slash form), low (redirect)
- **Source:** ATS `roles/:role_name/index.html`; `admin/auth.html:265-272` (`/roles/*` GET); `admin/index.html:3419` ("served by the parameterised route /roles/:role_name/").
- **Edge cases:** a selector GET on a route URL returns `404` (D-ROUTE §Error cases) — the ATS careers pages therefore read the fragment from the literal `/roles.html` instead. Unknown slug: the route still answers `200` (the page's own script renders "Role not found").
- **Cross-area:** composing R-COMP-103 (same decision; its Q-11 is the slash-less question), R-COMP-107.

### 3.2 Composition details the apps depend on

#### R-APPS-8 — Pagelove elements keep their position in restricted content models
`<p:include>` and `<p:stamp>` placed where the HTML parsing algorithm would
foster-parent or drop an unknown element — notably as a child of `<table>`,
`<tbody>`, `<tr>`, `<select>` — MUST be resolved **at their source position**:
the materialised fragment (e.g. a `<tbody>`) becomes a child of the `<table>`
the include was written in. pagelike's composition MUST therefore not run the
stored markup through an HTML5 tree builder that relocates these elements
before resolution (resolve Pagelove elements on the token stream, or parse
with foster-parenting disabled for Pagelove-namespace elements, then serialize).
The same applies to a selector GET of the composed page: `table >
tbody#roles-body` MUST match.
- **Evidence:** demo-source · live-observed · **Confidence:** medium-high
- **Source:** ATS `roles.html:13-15` (`<table><p:include resource="/admin/index.html" selector="#roles-body"></p:include></table>`), `roles/:role_name/index.html:75, 128-129` (script reads `#all-roles-data tr[itemtype]`); N-ATSLIVE (served `<table id="all-roles-data" hidden>` followed by `<tbody id="roles-body">`).
- **Edge cases:** include with 0 matches → `404`, more than one → `500` (D-INC §Cardinality). Include whose `resource` the requester cannot read still resolves (elevated composition, R-PERM-77).
- **Cross-area:** composing R-COMP-5 (source-preserving parse, same decision), R-COMP-80..86; store (parser fidelity).

#### R-APPS-9 — Namespaces are recognised by URI; composed output strips them
Processing attributes and elements are recognised by the namespace URI bound
to their prefix on the element or an ancestor, never by a fixed prefix:
`https://pagelove.org/1.0` (`p:`, `pagelove:` — `template`, `include`,
`stamp`, `transient`, `paginate`), `https://pagelove.org/Binding/CSS` (`r:`),
`https://pagelove.org/Binding/Sessel` (`e:`). Composed HTML responses omit
every `xmlns:*` declaration and every recognised processing attribute (D-INC
example; D-LIQ "All SSPI namespaces and binding attributes have been
stripped"; D-TR example drops `p:transient`); composed XML responses keep
their `xmlns:*` declarations but drop the directive attributes (R-COMP-14).
Stored documents (dav GET, R-PROTO-113) keep everything.
Malformed attributes produced by editing mistakes MUST NOT break composition:
`<html lang="en" xmlns:example co="https://pagelove.org/1.0"
xmlns:p="https://pagelove.org/1.0">` parses as attributes `xmlns:example`
(empty), `co`, `xmlns:p`; the `p` binding still works; `xmlns:example` is
stripped like any `xmlns:*`; `co` is left as an ordinary attribute.
- **Evidence:** documented (prefix freedom, stripping) · demo-source (KANBAN `pagelove:` prefix; ATS malformed attribute) · **Confidence:** high (URI recognition), low (malformed-attribute handling)
- **Source:** D-INC §Namespace declaration ("the prefix can be any valid XML prefix"); KANBAN `app.js:643, 653`; ATS `roles/:role_name/index.html:2`; D-L2:70 ("need the two xmlns declarations").
- **Edge cases:** a document with `p:template` but **no** `xmlns:p` is not processed (D-L2:70) and is served verbatim, braces included.
- **Cross-area:** composing R-COMP-10..14 (owner; same decisions).

#### R-APPS-10 — Liquid data contract used by the apps
Owned by the Liquid/composition area; listed because the apps fail visibly if
any item differs:
1. Binding items expose each Microdata property by name with its value
   (meta `content`, `time` `datetime`, else text); repeated properties are
   arrays (`p.image | join: ","`, `p.variant | join: ','`, SHOP
   `index.html:24`, `admin/products.html:20`); a missing property is empty so
   `!= blank` works (SHOP `index.html:17-19` notes an empty string is truthy).
2. `item['@id']` is the origin-relative source path plus `#` plus the element
   id (`/polls/kfd47o4zqd.html#poll`); POLLS derives the poll URL with
   `split: "#" | first` and then `fetch`es it with a `Range` header — an
   absolute `@id` naming another origin would turn that into a failing
   cross-origin request (POLLS `index.html:90-93`, `create.js:92-103`; D-SXE).
3. `request.body` during templated creation is the **decoded form map**
   (`request.body.title`, unchecked checkbox → absent, checked → `"on"`)
   (POLLS `new-poll.html:7, 17-21, 42`). Not documented (D-RC only says the
   body is "available"); demo-source, confidence medium.
4. `request.auth.username` = OIDC `sub`; `request.auth.claims.name|email|picture`;
   `request.auth.role` (KANBAN) and `request.auth.roles` (docs) both iterate the
   role list; all empty for an anonymous request (R-PERM-74, C-3).
5. `request.headers.<lowercase-name>` (SHOP `partials.html:20`).
6. Filters/tags used: `where`, `sort`, `reverse`, `limit:`, `first`, `split`,
   `join`, `default`, `strip`, `escape`, `size`, `random: lower: true, digits:
   true`, `assign`, `for`/`forloop.index`, `if`/`!=`/`==`/`>`.
- **Evidence:** documented + demo-source · **Confidence:** high (1, 4–6), medium (2, 3)
- **Cross-area:** composing R-COMP-24 (`request`), R-COMP-31 (binding results), R-COMP-52 (values in Liquid), R-COMP-142 (form bodies); permissions R-PERM-74.

#### R-APPS-11 — Sessel constructs used by the apps (cross-area inventory)
Selector literals `${…}` with `:has(…)`, `:value-equals(request.params.x)`,
`.first()`, `.first().value()`, `from "<glob>"` scoping, `.any(p => p.path()
== path)`; `Context.request.headers["<name>"]`, `Context.request.method`,
`Context.request.path`, `Context.request.body` (raw string), `.matches(regex)`,
`??`, `if`, `let`; `throw new HTTPResponse {status, message}` and `{status,
header: new Pair {key, value}, body}`; `Context.response.body.contains(…)`,
`Context.response.status = 404`; `@schema X url("…")` headers.
- **Evidence:** documented + demo-source · **Confidence:** high
- **Source:** D-L2:95, 122-146; POLLS `admin/auth.html:104-148`; SHOP `admin-auth.html:22-57`, `products/:slug.html:13`; ATS `outbox/index.html:25-30`.
- **Cross-area:** sessel, reactions.

#### R-APPS-12 — Request header lookup is case-insensitive
`Context.request.headers[<name>]` (Sessel, triggers and processors),
`ctx.request.headers[<name>]` (JavaScript actions) and Liquid
`request.headers.<name>` MUST find a header regardless of the case used in the
lookup key: keys are exposed lowercase, and a lookup with any casing
(`"Authorization"`, `"authorization"`, `"range"`) returns the value.
Enumeration (e.g. `{{ request | json }}`) shows lowercase names.
- **Evidence:** documented (capitalised lookups in D-TRIG examples) · demo-source (lowercase lookups in POLLS and SHOP; SHOP says capitalised lookups returned nothing on PageLove) · **Confidence:** medium
- **Source:** D-TRIG §when, §Chain termination (`headers["Authorization"]`); POLLS `admin/auth.html:107, 114`; SHOP `admin-auth.html:16-18, 25, 50`.
- **Why:** both spellings exist in official material; case-insensitivity makes both work and cannot turn a correct app into an incorrect one. Recorded as C-2; P-APPS-2 establishes PageLove's actual behaviour.
- **Cross-area:** reacting R-REACT-23 and sessel R-SESSEL-293 take the same decision; Liquid `request.headers` is composing R-COMP-24.

### 3.3 Templated creation (polls)

#### R-APPS-13 — Templated creation as the polls template uses it
1. `POST /templates/new-poll.html` with `Content-Type:
   application/x-www-form-urlencoded` from a native form submission (browser
   `Accept: text/html,…`). Authorization: POST on the template (D-RC step 2).
2. The template's root element carries `p:template="text/liquid"`; the whole
   document is rendered with `request.body` = decoded form fields
   (R-APPS-10.3).
3. The rendered document's `<base href>` names the storage path
   (`/polls/<10 chars>.html`); a missing/empty `href` → `422` (D-RC §Error
   cases). Authorization: PUT at that path (D-RC step 5); whole-document PUT
   Triggers for that path are expected to run (P-APPS-19).
4. Stored form: the rendered document with the `<base>` element removed and
   the `p:template` attribute removed; everything else verbatim, including
   `xmlns:p` and whitespace left by Liquid tags (decision; matches the shipped
   sample poll).
5. Response `301 Moved Permanently`, `Location: <base href as written>`
   (path-absolute), empty or short body; the browser follows with a GET
   (`create.js:84-87`). The mutation event, if any, is per R-SSE-24.
6. A path collision (random id already used) is decided by the whole-document
   PUT path: the polls trigger answers `409` (POLLS `admin/auth.html:130-150`).
- **Evidence:** documented (1–3, 5) · demo-source (2 request.body, 4, 6) · **Confidence:** high (status/Location), low (stored form)
- **Source:** D-RC; POLLS `index.html:40`, `templates/new-poll.html:2-10`, `polls/kfd47o4zqd.html:1-16`, `assets/create.js:1-2, 56-88`.
- **Statuses:** `301` success; `401`/`403` POST or PUT not authorized (no resource written); `422` no `<base href>`; `400` body cut short; `409`/`403` from triggers.
- **Cross-area:** composing R-COMP-140..144 (owner; identical decisions on request body, stored form and empty 301 body).

### 3.4 Identity as the apps see it

#### R-APPS-14 — Browser Basic credentials are application data
A request carrying `Authorization: Basic …` on the public plane is processed as
the request's normal principal (anonymous unless it also carries a session);
the header MUST NOT be rejected, consumed or treated as an identity by pagelike
(R-PERM-71), and it MUST be visible to triggers, processors and Liquid
(R-APPS-12). A trigger's thrown `HTTPResponse` with a `header` Pair
`WWW-Authenticate: Basic realm="…"` MUST be sent verbatim with the thrown
status so browsers show their native credential prompt; browsers then attach
the credential to later same-origin requests, which SHOP relies on for every
admin write.
- **Evidence:** demo-source · documented (HTTPResponse `header`) · **Confidence:** high
- **Source:** SHOP `admin-auth.html:5-35`, `partials.html:9-19`, `shop.mjs:351-352, 404-409`; D-TRIG §HTTPResponse headers.
- **Edge cases:** `GET` triggers also fire for `HEAD` (D-TRIG "`GET` implies `HEAD`"). The `/private/*` deny rule answers `401` for an anonymous request even when a correct Basic header is present (the header is not identity).

#### R-APPS-15 — Identity fixtures for acceptance
Acceptance needs, per site: local accounts with verified emails
(`admin1@example.com` for ATS `admins`; `alice@example.com`,
`bob@example.com` generic), OIDC-style claims `name`, `email`, `picture`,
`email_verified` exposed to composition (R-PERM-70, R-PERM-74), and the login
path `/auth/login` serving pagelike's local sign-in form (R-PERM-67,
R-PERM-73; `e2e/lib/pagelike.mjs` `signIn` fills `username`/`password`). The
`users` actor matches any of them; `*` also matches anonymous.
- **Evidence:** demo-source (paths, claims) · pagelike decision (local accounts) · **Confidence:** high

### 3.5 Consistency, concurrency and performance

#### R-APPS-25 — Read-your-writes, immediately
After a write returns 2xx, every later request on any connection of any
client MUST observe it: a selector GET of the container, a subsequent POST
into an element that was itself just inserted, a MOVE verification read. There
is no replication lag in pagelike (single process, per-site mutex, WAL
snapshots; `docs/design.md`). This makes the KANBAN retry loops (W-1, W-2)
succeed on their first iteration.
- **Evidence:** inferred (architecture) · demo-source (the workarounds exist because PageLove once lagged) · **Confidence:** high
- **Source:** KANBAN `app.js:29-49, 1554-1569`; docs/design.md §Read path.

#### R-APPS-16 — Concurrent writes to one document all land
Two concurrent element MOVEs of different elements in one document, two
concurrent POST appends into one element, and a MOVE racing a field PUT MUST
all take effect (serialized by the site mutex), each answering 2xx. A MOVE
that answers 2xx MUST have moved the element (never "204 but the element
never leaves its old parent").
- **Evidence:** documented (appends; MOVE atomicity) · demo-source (KANBAN reports the opposite on an older build) · **Confidence:** high
- **Source:** R-RW-76; R-PROTO-101; KANBAN `app.js:29-40, 911-914, 1500-1501`.

#### R-APPS-17 — Performance envelope for app-sized documents (pagelike target)
Not PageLove behaviour; a release target so the apps feel as intended. On a
reference laptop, for a 1 MB board document (KANBAN keeps ≤ 60 activity
entries per board "because every write reprocesses the whole document",
`app.js:945-948`, and reports "Selector writes cost ~1s on a big document on
this platform", `:763-765`): selector write p95 ≤ 150 ms, whole-document GET
p95 ≤ 50 ms, SSE fan-out to 10 subscribers ≤ 100 ms after commit. The demo
pages must load in < 3 s with < 1.5 MB transferred (upstream test budget,
`tests/demo-pages.spec.js:69-73`).
- **Evidence:** inferred (targets) · demo-source (budget) · **Confidence:** medium

### 3.6 Files and uploads

#### R-APPS-18 — Blob uploads the apps perform
Whole-resource PUT of arbitrary bytes with the file's own `Content-Type`
(`image/*`, `application/pdf`, Word types, `application/octet-stream`,
`application/json`) to paths the app chooses (`/uploads/<card>-<ts36>-<name>`,
`/uploads/avatars/<sub>-…`, `/uploads/<C-id>-cv.pdf`, `/images/<stem>.<ext>`,
`/outbox/E-<id>.json`) MUST be stored byte-for-byte and served back with that
type (R-RW-60, R-PROTO-34). Sizes up to at least 8 MB MUST be accepted
(KANBAN "Verified up to 8 MB on this host"). A PUT that is authorized while
GET on the same path is not (ATS `/uploads/*` for anonymous applicants) MUST
succeed; a later anonymous GET is refused. Truncated uploads MUST fail rather
than store partial bytes (N-BLOG "A tool that silently truncated files").
- **Evidence:** demo-source · documented (D-RC 400 on cut-short bodies) · **Confidence:** high
- **Source:** KANBAN `app.js:202-204, 1539-1551, 2582-2584`; ATS `apply.html:620-631`, `admin/auth.html:213-248`; SHOP `shop.mjs:489-502`; ATS `admin/index.html:1690-1700`.

### 3.7 Reactions the apps depend on

#### R-APPS-19 — Trigger and Processor features in use
1. Triggers filtered by `resource` glob(s) and `method`(s), including **GET**
   (with HEAD implied) — SHOP admin gate.
2. `when` false → the `otherwise` actions run (SHOP); `when` false and no
   `otherwise` → trigger skipped (POLLS).
3. `throw new HTTPResponse` ends the request with that status, `message` or
   `body` as body, and any `header` Pairs as response headers (POLLS 403/409,
   SHOP 401 + `WWW-Authenticate`).
4. A Trigger whose action is an `HttpRequest` (ATS outbox) queues an outbound
   request after the write, with header Pairs (an empty `content` yields an
   empty header value) and a Sessel `body` of `Context.request.body` (raw JSON
   string); failures never affect the client response (D-R-HOOK).
5. Processors filtered by `status` (`200`, class `2xx`), with a Sessel `when`
   reading `Context.response.body` and a Sessel action assigning
   `Context.response.status` (blog). The same processor MUST apply to `HEAD`
   (the tutorial verifies it with `curl -sI`; R-RW-24 "HEAD returns exactly the
   status and headers GET would").
6. A Processor whose action is a JavaScript module that throws an
   `HTTPResponse`-shaped object with `status`, `body` (the original body
   string, passed through **byte-for-byte**) and `headers` (e.g.
   `Access-Control-Allow-Origin`, `Content-Type`) replaces the response with
   those headers (beta-js `cors.html`).
7. Neither triggers nor processors run on the authoring (dav) plane (BJ
   `verify-deploy.sh:8-11`; D-R-SM "WebDAV … never fire handlers").
- **Evidence:** documented + demo-source + client-source · **Confidence:** high (1–5, 7), medium (6 body pass-through)
- **Source:** D-TRIG; D-PROC; D-L2:122-156; SHOP `admin-auth.html`; POLLS `admin/auth.html:96-150`; ATS `outbox/index.html:10-32`; BJ `cors.html:7-70`.
- **Cross-area:** reacting R-REACT-6 (where reactions run), R-REACT-13 (GET implies HEAD), R-REACT-18 (`otherwise`), R-REACT-31 (thrown response on the wire), R-REACT-43..45 (processor status/body, dropped header writes), R-REACT-48..57 (HttpRequest, queueing, retry); server-js.

### 3.8 Errors, caching, CORS, tooling

#### R-APPS-20 — Statuses the apps branch on
The apps make decisions on exact status codes, so these MUST be exact:
`416` for a selector PUT/DELETE/POST whose target selector matches nothing
(KANBAN upsert and `postEventually`, POLLS "row no longer exists", SHOP treats
`416` on a product DELETE as success); `422` for shape, schema, uniqueness and
transition failures (POLLS, DEMO, blog); `401` for anonymous denials and
trigger-thrown 401 (DEMO feed probe, SHOP `res.status === 401` messages);
`403` for an authenticated denial or a trigger 403; `409` from triggers
(POLLS); `412` for failed preconditions (worker, D-R-SM). Any 2xx is success
for every app (no app distinguishes 200/201/204/206). The body of a uniqueness
`422` MUST contain the word "uniqueness" in any case (DEMO `app.js` match
`/uniqueness/i` to show a friendly message; R-MOD-72).
- **Evidence:** demo-source + documented · **Confidence:** high
- **Source:** KANBAN `app.js:16, 58, 1565`; POLLS `poll.js:87-92`; SHOP `shop.mjs:615-616, 639, 708-710`; DEMO `demo-01-event/app.js:50-57`, `demo-04-accountability/app.js:64-74, 95-101`; worker `index.js:258, 448`.

#### R-APPS-21 — Caching of app documents and assets
HTML responses (composed or not) MUST carry validators (`ETag`) and MUST NOT
be cacheable in a way that serves a stale document after a write (use
`Cache-Control: no-cache` or equivalent revalidation). Blob assets SHOULD be
served with an `ETag` and a short or revalidating cache policy; pagelike MUST
NOT mark app assets `immutable`. Responses whose content depends on identity,
headers or transient state are `Cache-Control: private` (D-TR, D-SX). Apps
defend with `cache: 'no-store'` and query busters anyway (R-APPS-5).
- **Evidence:** demo-source (KANBAN: PageLove "serves assets with a fixed 5-minute cache and no override", `app.js:257-262`) · documented (private rules) · **Confidence:** medium
- **Compatibility decision:** do not reproduce a fixed 5-minute asset cache; prefer revalidation. Apps that assume staleness (KANBAN version bar) still behave correctly.

#### R-APPS-22 — CORS for cross-origin module imports
By default pagelike MUST answer cross-origin requests the way PageLove's edge
does: CORS headers only for the site's own names and declared aliases, never a
blanket `Access-Control-Allow-Origin: *` (BJ `cors.html:15-17`). A site that
wants its files importable from other origins adds a Processor (R-APPS-19.6);
the resulting response MUST carry `Access-Control-Allow-Origin: *` and
`Content-Type: text/javascript` and the unmodified module bytes, so that
`verify-deploy.sh` passes (200, allow-origin `*`, JavaScript type, `node
--check` parses).
- **Evidence:** client-source · **Confidence:** medium
- **Source:** BJ `cors.html`, `.github/scripts/verify-deploy.sh:43-97`.
- **Cross-area:** protocol R-PROTO-23 (preflight); reacting R-REACT-31, R-REACT-32, R-REACT-45.

#### R-APPS-23 — WebDAV tooling contract
The official deploy scripts MUST work unmodified against pagelike's authoring
plane at `http(s)://dav-<site>.<domain>/`: `Authorization: Bearer <key>`;
`MKCOL` of an existing collection → `409` with an error document containing
`https://dombase.pagelove.team/ns/error/DirectoryAlreadyExists` (or `405`);
`PROPFIND` `Depth: 0` of an existing collection → `207`; `PUT` → 2xx (201
create, 200 replace), `If-None-Match: *` on an existing path → `412`; `GET`
returns the uploaded bytes exactly (`cmp -s`); `DELETE` of a missing path →
`404`. The public host is derived by removing the `dav-` prefix from the dav
host name (BJ `verify-deploy.sh:30-40`), which pagelike's host layout already
satisfies.
- **Evidence:** demo-source + client-source · **Confidence:** high
- **Source:** SHOP `ops/deploy-pagelove.sh:49-253`, `ops/deploy-admin-password.sh`; BJ `.github/scripts/sync-webdav.sh:84-175`, `verify-deploy.sh`; DEMO `README.md:138-146`.
- **Cross-area:** protocol R-PROTO-110..124.

#### R-APPS-24 — Checkout worker contract (shop)
(a) A public anonymous `GET /data/products/<slug>.html` with `Accept:
text/html` returns the stored product document `200` (rules allow `GET /*`;
redirects are treated as failures, so no redirect may occur). (b) Over the dav
plane: `PUT /data/orders/<id>.html` with `If-None-Match: *` creates (2xx) or
answers `412`/`409` if present; the created document must pass the Order
closed shape and the Order/OrderLine schemas (dav writes are validated,
R-PROTO-112). (c) dav `GET` returns an `ETag` that, sent as `If-Match` on a
dav `PUT`, succeeds when unchanged and yields `412` (or `409`) when the order
changed in between (R-PROTO-119). (d) The order is then visible through the
public route `/orders/<id>.html` while `/data/orders/<id>.html` stays
unreadable to the public.
- **Evidence:** demo-source · **Confidence:** high
- **Source:** SHOP `worker/src/index.js:175-190, 219-268, 425-452`; `constraints.html`; `schemas.html:123-257`; `rules.html:26-38`; `orders/:id.html`.

#### R-APPS-26 — Deterministic order of site-wide query results
Where an app takes `.first()` of a site-wide selector query (SHOP compares the
Basic header with `${[itemtype=".../AdminCredential"] [itemprop="authorization"]}.first().value()`;
the blog and shop routes take `.first()` of the matching item), the result
order MUST be deterministic: documents in ascending byte order of their path,
then document order within each document (pagelike decision; not documented).
Consequence to document for operators: with both `/private/admin.example.html`
and `/private/admin.html` installed, the placeholder sorts first and the real
password can never match — install the shop per R-APPS-1.
- **Evidence:** inferred · **Confidence:** low (PageLove's order unknown, P-APPS-11)
- **Cross-area:** composing R-COMP-31 and sessel (store queries) take the same order.

#### R-APPS-27 — Transient basket behaviour the shop needs
For `<ul id="basket" p:transient>` on `/basket.html`: a selector GET of
`#basket` returns the requesting session's copy (default markup for a new
session); a selector PUT with a body that still matches `#basket` stores the
copy for that session only (`206`/2xx) and never changes the stored document
or other sessions; a body that no longer matches → `422`; a selector DELETE
reverts the session to the default; every response built from the document is
`Cache-Control: private`; bindings never see session copies. Anonymous
visitors get a session cookie on first contact (R-PERM-64). POST into a
transient element also writes to the session (documented; the shop avoids
it, C-4).
- **Evidence:** documented · demo-source · **Confidence:** high
- **Source:** D-TR; SHOP `basket.html:10-27`, `shop.mjs:3-10, 55-96`.
- **Cross-area:** composing R-COMP-110..117 (owner).

### 3.9 Cross-area requirements exercised by the acceptance suite

| Behaviour | Owner requirement(s) | Apps |
|---|---|---|
| Selector GET 206 / 416 / 404 | R-RW-25, R-RW-28 | all |
| Selector PUT echoes the stored replacement; no upsert | R-RW-64, R-RW-66 | ATS, POLLS, KANBAN |
| Selector POST placements and response body; `ETag` = anchor | R-RW-70..76 | all |
| Selector DELETE 204; 416 on retry | R-RW-80, R-RW-82 | POLLS, KANBAN, DEMO |
| Conditional writes (`If-Match`, `If-None-Match: *`) | R-RW-85..91 | beta-js, worker, scripts |
| OPTIONS 207 parts, `Allow`, space-form `Content-Range` | R-PROTO-3, R-PROTO-16..21 | beta-js, PP, A1 |
| MOVE with `Destination` path and `Destination-Range` placement; three grants | R-PROTO-90..101, R-PERM-47 | KANBAN |
| SSE: connection token, article format, literal body tag, no echo, narrowing by `Pagelove-Connection`, reset | R-SSE-6, R-SSE-8, R-SSE-14, R-SSE-37, R-SSE-38, R-SSE-43 | POLLS, KANBAN, beta-js |
| Rules in tables, empty selector, multi-values, deny-wins, `users`, groups, default-GET | R-PERM-1..4, 2a, 15, 40, 44, 58..62 | all |
| Directory paths in rules | R-PERM-19 | ATS, DEMO, A1 |
| Composition not filtered by rules; write-through authorised on the composed page | R-PERM-28, R-PERM-77 | blog, ATS, SHOP |
| Closed shapes, composed shapes, attribute coverage union | R-MOD-61..68 | blog, POLLS, ATS, DEMO, SHOP |
| Uniqueness, `@key`, transitions | R-MOD-30..36, R-MOD-72 (+ reactions) | DEMO, SHOP |
| Error document shapes | R-PERM-55/56, R-MOD-70..72, R-PROTO-130..132 | all |
| Composition: source-preserving parse, prefixes, stripping, bindings, Liquid values, stamps, includes, write routing, transients, routes, templated creation, XML | R-COMP-5, R-COMP-10..14, R-COMP-24, R-COMP-30..38, R-COMP-50..54, R-COMP-70..76, R-COMP-80..95, R-COMP-100..117, R-COMP-123..128, R-COMP-140..144 | blog, POLLS, ATS, SHOP, KANBAN |
| Reactions: filters, `otherwise`, thrown responses, processors, HttpRequest | R-REACT-6..57 | blog, POLLS, SHOP, ATS, beta-js |
| Sessel: selector literals, `.first()`, `from`, header lookup | sessel.md (R-SESSEL-*, incl. R-SESSEL-293) | blog, POLLS, SHOP |

## 4. Register of workarounds for past platform bugs

"Claim" is what the source says about PageLove at the time; "pagelike" is the
compatibility decision; "Check" is the acceptance scenario that proves the
workaround is harmless (the app still works) on pagelike.

| ID | Where | Claim (summarised) | pagelike | Check |
|---|---|---|---|---|
| W-1 | KANBAN `app.js:29-49` (comment "MOVE, confirmed.") | Two concurrent MOVEs on one document lost one "roughly three times in four": 204 returned but the element stayed; `If-Match` could not guard because "conditional writes reject every request here — even If-Match: *". Workaround: after each MOVE, GET the destination (`?v=` buster) and repeat up to 5 times; measured loss 9/12 → 0/12. | Every 2xx MOVE moves (R-APPS-16); `If-Match` works (R-RW-86); decision already recorded as protocol C-13. | ACC-KB-5, ACC-KB-6: each drop produces exactly one MOVE and one verification GET. |
| W-2 | KANBAN `app.js:1554-1569` | A POST into an element that was itself just written could 416 "for a moment" (read-path staleness, "same family as the MOVE loss"); retry nine times over ~8 s. | Read-your-writes (R-APPS-25). | ACC-KB-8: one POST per add. |
| W-3 | KANBAN `app.js:242-253, 383-404` | An early build polled every 3 s "in the belief the stream was dead; the truth was originator suppression plus a test that never properly isolated the principals". Now SSE triggers a refetch; 30 s fallback poll. | No echo to the originating session (R-SSE-37). | ACC-KB-4. |
| W-4 | KANBAN `app.js:257-278` | Assets served "with a fixed 5-minute cache and no override"; app polls `/version.txt`. | Revalidating caches (R-APPS-21). | ACC-KB-12. |
| W-5 | KANBAN `app.js:763-765, 943-959` | Selector writes ~1 s on a big document; every write reprocesses the whole document; activity log trimmed to 60. | Performance target R-APPS-17. | ACC-KB-13 (timing). |
| W-6 | KANBAN `app.js:1156-1160, 1204-1218` | "There is no scheduler on this platform": weekly goal sweep and daily Slack digest run in the first browser that opens the board. | Out of scope (no scheduler in PageLove either). | — |
| W-7 | KANBAN `app.js:2016-2019` | "There is no CRDT and no PATCH on this platform." | PATCH → 501 (sse C8). | — |
| W-8 | KANBAN `app.js:1887-1890` | Attributes cannot be written independently; a label rename rewrites the whole `<meta>`. | Same (element granularity). | — |
| W-9 | ATS `admin/auth.html:38-47` | "Any public selector-scoped GET allow rule" opened "every selector of that resource for anonymous reads (explicit deny rules do not override it)", so roles moved to a separate page included from the private one. | Selector rules apply only to their selector; deny wins at the top tier (R-PERM-25, R-PERM-40). | ACC-AT-4, case `apps.ats.roles-include-in-table`. |
| W-10 | ATS `admin/auth.html:66-68, 106-113` | The `admins` Group "isn't being resolved on this host (whoami shows roles=users only)"; rules duplicated per email; a parallel `GroupMembership` table added. | Both Group and GroupMembership resolve (R-PERM-58, R-PERM-61). | ACC-AT-4 (both paths). |
| W-11 | N-BLOG (2026-08-12) | Platform issues found while building the ATS: selector rules not enforced on writes; missing client id broke login redirect; 403 without login guidance; silent file truncation; PDFs with wrong content type (sent as form upload instead of PUT); a module import killing a script silently. | Enforced (R-PERM-51); login link in 401 (R-PERM-55); no truncation (R-APPS-18); types (R-APPS-4). | ACC-AT-3, ACC-AT-6. |
| W-12 | ATS `apply.html` comments (~440-444), `outbox/index.html` | Client-side Postmark token is a leak vector; confirmation email moved server-side (Trigger + HttpRequest). | Outbound HTTP from triggers (R-APPS-19.4). | ACC-AT-8. |
| W-13 | SHOP `admin-auth.html:16-18` | `headers["Authorization"]` "is always empty" (HTTP/2 lowercases names); the gate would let everyone through. | Case-insensitive lookup (R-APPS-12). | ACC-SH-4, case `apps.shop.admin-basic-gate`. |
| W-14 | SHOP `constraints.html:12-14` | Closed-shape root attributes are checked; not naming the root refused "every real order … the first time". | Same (R-MOD-65 rule 3). | ACC-SH-7/8. |
| W-15 | SHOP `index.html:17-19` | A missing image yields an empty string, truthy in Liquid → use `!= blank`. | Same Liquid semantics. | ACC-SH-1. |
| W-16 | SHOP `partials.html:9-19` | Never probe `/admin/` from the browser: the 401 + `WWW-Authenticate` prompts every shopper; admin links decided in Liquid from `request.headers.authorization`. | R-APPS-14. | ACC-SH-4. |
| W-17 | SHOP `basket.html:16-19`, `shop.mjs:3-10` | POST into a transient element "is NOT documented", so the shop uses read-modify-write PUT. | POST supported per D-TR (C-4); PUT path unaffected. | ACC-SH-2/3. |
| W-18 | SHOP `schemas.html:92-93` | Origin-relative image paths fail `schema.host/URL`, so `image` is Text. | Same type semantics (modeling). | — |
| W-19 | DEMO `demo-04-accountability/schema/checkin.html:6-8`, `demo-02…/schema/reaction.html:6-7` | Composite unique groups "verified broken on this host"; single synthetic key used. | Composite uniqueness per docs (modeling C15). | ACC-RC-6 (§7.10). |
| W-20 | DEMO `demo-01-event/app.js:138-140`, `schema/event.html:35-40`, `demo-03…/schema/listing.html:30-41` | "Processor proved unreliable" for waitlist promotion; a Trigger "only sees the incoming request, never stored state". | Processors per docs; triggers can query via Sessel `${…}` (POLLS uses it) — the claim is about their design, not a bug. | ACC-DM-2. |
| W-21 | DEMO `demo-05-professional/app.js:103-105` | "Liquid composition … proved unreliable on this platform"; replaced by client JS. | Liquid per docs. | ACC-BL-*, ACC-PO-1. |
| W-22 | DEMO `demo-03…/schema/listing.html:71-79`, `app.js:105-109` | A transition to the same value is a no-op ("verified"), so two claimants both "succeed"; a unique Claim record is the real guard. | Same-value writes are not transitions (modeling/reactions). | ACC-DM-4. |
| W-23 | DEMO `listing.html:42-45` | Without an entry rule (`to` without `from`) new items are rejected. | Documented strictness (D-R-SM). | ACC-DM-4. |
| W-24 | DEMO `demo-04…/feed.html:13-24` | Deny `*` + Allow at a different tier works; "the same-tier pattern proven broken". | Tier model (R-PERM-38..40). | ACC-DM-5. |
| W-25 | DEMO `demo-04…/index.html` comment before line 122 | Rule scoped to the exact page, "not resource:\"/*\"" (see BUILD-REFERENCE.md, not in repo). | No special behaviour. | — |
| W-26 | BJ `cors.html:7-49` | Processor header writes are dropped ("the processor executor reads back only status and body"); `Context.response.headers.set` is not Sessel; a Sessel rethrow HTML-escapes the body (`=>` → `=&gt;`); JS actions cannot mutate `ctx.response`; the edge adds CORS only for the host's own names. | JS-thrown `headers` honoured, body byte-exact (R-APPS-19.6, R-APPS-22, R-REACT-31); processor header writes dropped and Sessel-rethrown bodies re-serialized as HTML exactly as described, so `cors.html` keeps needing its JavaScript action (R-REACT-32, R-REACT-45; P-APPS-15). | ACC-BJ-2. |
| W-27 | BJ `test/sse-echo.test.mjs:1-14` | The server "deliberately does NOT stream a mutation back to the connection that caused it"; client keeps pending echoes with a TTL. | R-SSE-37/38. | ACC-PO-3, ACC-BJ-4. |
| W-28 | PP `index.mjs:131-137` (commit `bfd25de`) | OPTIONS parts' `Content-Range` is `selector <css>` (space), not `selector=<css>`; the old parser wired nothing up. | Emit space form (R-PROTO-3). Legacy dom-forms/dom-core still parse `=` (A11). | ACC-BJ-5. |
| W-29 | BJ `sync-webdav.sh:104-113`; SHOP `deploy-pagelove.sh:94-112` | MKCOL on an existing collection → 409 (not RFC 405); also 409 for a missing parent, so scripts disambiguate with PROPFIND. | R-PROTO-115 (409 + DirectoryAlreadyExists). | ACC-00. |
| W-30 | BJ `verify-deploy.sh:1-11` | "A module that uploaded badly still answers 200"; verify bytes, type and CORS on the public host; processors do not run on WebDAV. | R-APPS-4, R-APPS-19.7. | ACC-BJ-2. |
| W-31 | POLLS `poll.js:207-217` | SSE body extracted by string slicing because `DOMParser` drops `<tr>` outside a table. | Literal body tag (R-SSE-8). | ACC-PO-3. |

## 5. Contradictions and compatibility decisions

| ID | Topic | Claims | Decision |
|---|---|---|---|
| C-1 | Status of a successful selector POST | Docs and R-RW-72: `206 Partial Content`. beta-js test helper: "A 201 response carrying `html`, as the server answers a POST" (`test/helpers/primitives-env.mjs:47-52`). | `206` (documented). No app distinguishes 2xx codes (R-APPS-20); app cases accept any 2xx. |
| C-2 | Case of header names in `Context.request.headers` | D-TRIG examples look up `"Authorization"`; SHOP says that lookup is always empty and uses lowercase; POLLS uses lowercase `"range"`. | Case-insensitive lookup, lowercase keys (R-APPS-12). Probe P-APPS-2. |
| C-3 | Role list accessor | Docs/D-SX: `request.auth.roles`; KANBAN: `request.auth.role`. | Expose both (permissions C9, R-PERM-74). |
| C-4 | POST into a transient element | D-TR §Mutating: "A PUT or POST targeting a transient element … writes to the session". SHOP: POST-append "is NOT documented (the identity rule, the error table and the summary all name PUT only)". | Follow D-TR (POST writes the session copy). The shop never POSTs, so either reading keeps it working. Probe P-APPS-6. |
| C-5 | Composite uniqueness | D-R-CU/D-R-LINK: works, atomically. DEMO: "verified broken on this host". | Docs (modeling C15). |
| C-6 | Processor and Liquid reliability | Docs: supported. DEMO: "proved unreliable" (W-20, W-21). | Docs. |
| C-7 | Concurrent MOVE and `If-Match` | Docs: atomic, preconditions honoured. KANBAN: 3/4 concurrent MOVEs lost; every conditional write rejected (W-1). | Docs (protocol C-13). |
| C-8 | PATCH / CRDT | Docs mention CRDT change-set PATCH. KANBAN: "no CRDT and no PATCH on this platform". | PATCH → 501 (sse C8). |
| C-9 | Selector-scoped GET allow leaking other selectors | ATS W-9 vs docs' per-selector evaluation and deny-wins. | Docs. Probe P-APPS-18. |
| C-10 | Group resolution | ATS W-10 vs D-R-GRP/R-PERM-58. | Docs; also honour legacy `GroupMembership` (R-PERM-61). |
| C-11 | Directory URL for writes | beta-js writes to `location.href` (directory URL when loaded at `/x/`); KANBAN and ATS hard-code `index.html` paths "so writes hit the right file". Docs cover GET/HEAD only. | Map all methods (R-APPS-6). Probe P-APPS-4. |
| C-12 | Stored form after templated creation | D-RC: silent. Shipped sample poll: no `<base>`, no `p:template`, keeps `xmlns:p`. | Strip both (R-APPS-13.4). Probe P-APPS-1. |
| C-13 | Where the shop's orders come from | SHOP `constraints.html:5-8`: "Orders are written by the shopper's browser". README and worker: orders are written by celld over WebDAV; `rules.html:26-30` agrees with the worker. | Internal inconsistency of the app; pagelike validates dav writes (R-PROTO-112), so the constraint protects either path. |
| C-14 | ATS demo openness | ATS README: "The admin at /admin/ is open to anyone who has the URL". `admin/auth.html`: admin writes only for `admins` or listed emails. | Both true under default-GET `allow`: anyone reads, only staff write. Acceptance runs ATS in the locked-down mode the README recommends (§6.3). |
| C-15 | Blog `users` vs WebDAV editor | D-L2:358: "Signed-in users (you, in your editor) can still read the files". The editor is the dav plane, which bypasses rules; `users` is a public-plane session. | Both work on pagelike (dav bypass R-PROTO-111; `users` R-PERM-15). |
| C-16 | Blog 404 processor vs its own stylesheet | D-L2:134, 153-156: the Processor's `when` is `Context.response.body.contains("blog.example/Post") == false` and `curl -sI /posts/nonsense.html` shows `404`. But the route template printed at D-L2:91-93 keeps `<style>main:has([itemtype="https://blog.example/Post"]) .post-missing {…}</style>` in the served page, so under the documented substring semantics of `String.contains` the `when` is always false and the status stays `200`. | pagelike implements `contains` as documented (substring on the composed body, R-REACT-26). ACC-BL-2 and case `apps.blog.route-stamp-and-404` use a stylesheet that does not name the type; the verbatim template is case `apps.blog.route-404-verbatim-template` (`status: disputed`). Probe P-APPS-22. |

## 6. Acceptance environment and per-app setup

### 6.1 Environment, sessions and tiers

- **Server.** pagelike built from the release commit and started as in
  `e2e/global-setup.mjs`: `pagelike serve --data <fresh dir> --listen
  127.0.0.1:<port>` with the default `--domain localhost`; public origin
  `http://<site>.localhost:<port>`, authoring origin
  `http://dav-<site>.localhost:<port>`. One site per app: `first-app`,
  `first-app-norules`, `blog`, `polls`, `kanban`, `ats`, `shop`, `demos`,
  `betajs`, `recipes` (`pagelike site create <site> --default-get
  allow|deny`, authoring key from `pagelike key create --site <site>`).
- **Browser sessions.** Playwright Chromium ≥ 135 (Invoker Commands API for
  A1). Context **A** and context **B** are separate browser profiles, i.e.
  separate cookie jars and therefore separate PageLove sessions. **A′** is a
  second page in context A: same session, different connection.
- **Network recorder.** Every scenario records, per request: method, path and
  query, request headers `Range`, `Destination`, `Destination-Range`,
  `Pagelove-Connection`, `If-Match`, `If-None-Match`, `Content-Type`,
  `Authorization` (presence only); per response: status, `Content-Type`,
  `Content-Range`, `ETag`, `Location`, `Cache-Control`, `WWW-Authenticate`.
  "Exactly one request" assertions refer to this log.
- **Time limits.** A remote change must be visible in the other session within
  2 s (KANBAN: 3 s, because it debounces 250 ms and holds a 1.2 s quiet window
  after its own writes, `app.js:285-296, 386-393`).
- **Console.** Unless a scenario says otherwise, the page must log no uncaught
  exception and no CSP violation.
- **Tiers.** *A — release-blocking core* (stages 1–5): ACC-00, ACC-FA-*,
  ACC-PO-*, ACC-KB-1..12, ACC-DM-1..6, ACC-BJ-1, ACC-BJ-3..5. *B —
  release-blocking full*: ACC-BL-*, ACC-AT-1..7, ACC-AT-9, ACC-SH-1..6,
  ACC-SH-8..10, ACC-BJ-2, ACC-RC-*, ACC-UP-1..3. *C — extended* (external
  dependencies or TLS): ACC-KB-13, ACC-KB-14, ACC-AT-8, ACC-SH-7, ACC-UP-4,
  ACC-UP-5.

### 6.2 Third-party modules

Apps and tutorials import modules from third-party origins. For reproducible
runs, the acceptance harness serves them from the pinned checkouts through
Playwright request routing (`context.route`), with `Content-Type:
text/javascript` and `Access-Control-Allow-Origin: *`:

| URL pattern | Served from |
|---|---|
| `https://pagelove.github.io/beta-js/**` | `research/upstream/beta-js` at `c204746` (same relative path) |
| `https://cdn.pagelove.net/js/dom-subscriber/cde4007/index.mjs` | `git -C research/upstream/dom-subscriber show cde4007:index.mjs` |
| `https://cdnjs.cloudflare.com/ajax/libs/jszip/3.10.1/jszip.min.js` | network, or a vendored copy (ATS CV zip only) |
| `https://fonts.googleapis.com/**`, `https://fonts.gstatic.com/**` | aborted (fonts fall back; no assertion depends on them) |

ACC-BJ-3 repeats A1 with the beta-js route pointed at pagelike's own `betajs`
site instead.

### 6.3 Installation and configuration per app

All uploads go through the authoring plane (`e2e/lib/pagelike.mjs` `deploy`,
or the app's own scripts where named).

| App | Install | Configuration |
|---|---|---|
| A1 first app | `first-app`: the final document of D-L1 (lines 238–320, script replaced by the DOMSubscriber version 329–359) at `/index.html`. `first-app-norules`: the same document without the two rule `<div>`s. | default-GET `allow` |
| A2 blog | `blog`: files are added in tutorial order by the scenarios (ACC-BL-*), each exactly as printed in D-L2. | default-GET `allow`; account `alice@example.com` (verified) |
| A4 polls | `polls`: every file under `site/` | default-GET `allow` (the rules grant every public read the app needs; `/templates/new-poll.html` GET is left to the default) |
| A5 kanban | `kanban`: every file under `site/` plus the rules document below at `/rules.html` | *open mode*: default-GET `allow`, rules actor `*`. *members mode* (ACC-KB-14): default-GET `deny`, actor `users`, accounts alice/bob with `name` claims. |
| A6 ATS | `ats`: every file under `site/` | default-GET `deny` (the README's lock-down). Accounts `admin1@example.com` (verified), `admin3@example.com` (verified, used to exercise the Group path only if its per-email rules are removed in ACC-AT-4b), `bob@example.com` (verified, not staff). For ACC-AT-8 set `outbox/index.html` `url` meta to the harness sink (test-only edit) and start pagelike with `--outbound-allow-private` so a loopback sink is reachable (R-REACT-61). Optional: replace `https://huge-stomp-7042.onpagelove.com` in `admin/index.html:2785-2786` with the site origin. |
| A7 shop | `shop`: `PAGELOVE_WEBDAV_URL=http://dav-shop.localhost:<port>/ PAGELOVE_API_KEY=<key> ./ops/deploy-pagelove.sh "$PWD/site" <backup dir>` (installs exactly `ops/pagelove-files.txt`, creates the collections of `pagelove-directories.txt`, seeds `data/settings/shop.html`); then `PAGELOVE_ADMIN_PASSWORD=test ./ops/deploy-admin-password.sh <backup dir>` (writes `/private/admin.html` with `Basic base64(owner:test)`). `private/admin.example.html` is **not** installed (R-APPS-26). | default-GET `allow`. For ACC-SH-7 only: TLS front end (`deploy/Caddyfile.example`) serving `https://shop.pagelike.test` and `https://dav-shop.pagelike.test` from a locally trusted CA (`NODE_EXTRA_CA_CERTS` for the worker, browser trust for Chromium); worker `.dev.vars`: `SHOP_ORIGIN=https://shop.pagelike.test`, `SHOP_WEBDAV_URL=https://dav-shop.pagelike.test/`, `PAGELOVE_API_KEY=<key>`, `STRIPE_API_BASE=https://stripe-stub.pagelike.test/`, `STRIPE_WEBHOOK_SECRET=whsec_test`, `STRIPE_SECRET_KEY=sk_test_x`, `STRIPE_MODE=test`; a Stripe stub answering `GET v1/balance` (`livemode:false`) and `POST v1/checkout/sessions`. |
| A8 demos | `demos`: `python scripts/prepare-deploy.py --host demos.localhost --output <tmp>` then upload `<tmp>/**` | default-GET `allow`; account `alice@example.com` for the feed |
| A9 beta-js | `betajs`: `PAGELOVE_WEBDAV_URL=http://dav-betajs.localhost:<port>/ PAGELOVE_API_KEY=<key> .github/scripts/sync-webdav.sh --all` | default-GET `allow` |
| A3 recipes | `recipes`: one document per recipe, as printed in the recipe page, written through the public plane when the recipe says `PUT` and over dav otherwise | as each recipe states |

Kanban acceptance rules (`/rules.html`, open mode; members mode replaces `*`
by `users`). Element MOVE needs MOVE on the document plus DELETE on the source
and POST on the destination (R-PERM-47); the empty `selector` makes each grant
whole-resource (R-PERM-4, R-PERM-27):

```html
<!DOCTYPE html>
<html><head><title>board rules</title></head><body><table>
  <tr itemscope itemtype="https://pagelove.org/AuthorizationRule">
    <td itemprop="actor">*</td>
    <td><ul><li itemprop="resource">/index.html</li><li itemprop="resource">/boards/*</li></ul></td>
    <td><ul><li itemprop="method">GET</li><li itemprop="method">HEAD</li><li itemprop="method">OPTIONS</li>
      <li itemprop="method">PUT</li><li itemprop="method">POST</li><li itemprop="method">DELETE</li><li itemprop="method">MOVE</li></ul></td>
    <td itemprop="selector"></td><td itemprop="action">allow</td>
  </tr>
  <tr itemscope itemtype="https://pagelove.org/AuthorizationRule">
    <td itemprop="actor">*</td>
    <td><ul><li itemprop="resource">/uploads/*</li></ul></td>
    <td><ul><li itemprop="method">GET</li><li itemprop="method">PUT</li></ul></td>
    <td itemprop="selector"></td><td itemprop="action">allow</td>
  </tr>
</table></body></html>
```

### 6.4 What changes when an app moves to another origin

| App | Hard-coded origin-specific values | Other prerequisites |
|---|---|---|
| A1 | none | beta-js and dom-subscriber reachable (third-party CDNs) |
| A2 | `feed.xml` `https://field-notes.onpagelove.com/…` (links and ids) | identity provider for the `users` part |
| A4 | none | installed at the host root |
| A5 | none | a rules document (none shipped); optional `/auth/login` provider; optional `/admin/slack.html` |
| A6 | `admin/index.html:2785-2786` (`huge-stomp-7042.onpagelove.com`); Postmark token; admin emails | OIDC provider with verified emails; default-GET `deny` to make admin private |
| A7 | `worker/wrangler.jsonc` `SHOP_ORIGIN`/`SHOP_WEBDAV_URL`/`STRIPE_API_BASE`; `.dev.vars`; README demo link; Stripe webhook URL (`ops/configure-stripe-webhook.sh`) | HTTPS for the worker's origins; `private/admin.html` generated per host; celld origin saved in Admin → Settings |
| A8 | none in source (`__PAGELOVE_HOST__` token), rewritten by `prepare-deploy.py --host` | the host name must be port-less (regex), so canonical/OG URLs omit the pagelike port; harmless for function |
| A9 | GitHub Pages URL in docs and tutorials | for self-hosting: `cors.html` processor (server JS) |

## 7. Release acceptance suite

Each scenario lists: tier, sessions, steps, expected observations,
requirements. "2xx" means any success status (R-APPS-20). Paths are on the
app's own site.

### 7.1 Installation

**ACC-00 — Official deploy tooling works against pagelike** (A)
1. Run the shop deploy (§6.3) twice in a row.
2. Run beta-js `sync-webdav.sh --all` twice, then `DRY_RUN=1` once.

Expected: both scripts exit 0 both times; the shop run prints `VERIFY
<path> -> exact match` for every file of `ops/pagelove-files.txt`; the second
shop run prints `MKCOL <dir> -> already exists` (409 +
`DirectoryAlreadyExists`) and leaves `data/settings/shop.html` unchanged (seed
PUT answered `412`); the beta-js second run completes without MKCOL failures.
Refs: R-APPS-1, R-APPS-23, R-PROTO-113..115.

### 7.2 A1 — Your first app

**ACC-FA-1 — Tick persists** (A; A, B)
1. A opens `/` (not `/index.html`). Wait for `OPTIONS /` → `207`.
2. A ticks "Get Milk".
3. A reloads; B opens `/index.html`.

Expected: the OPTIONS body has a part with `Content-Range: selector #todo-list
li` and `Allow` containing `PUT` and `DELETE`, and a part for `#todo-list`
with `POST`. Step 2 sends exactly one `PUT /` with `Range: selector=#todo-list
> li:nth-child(1)` → 2xx. After step 3 the first checkbox is checked and
struck through in both sessions. Refs: R-APPS-6, R-PROTO-16..21, R-PERM-19,
R-PERM-25.

**ACC-FA-2 — Add an item** (A; A)
1. Type "Bread", click **Add**. 2. Tick the new item. 3. Reload.

Expected: one `POST /` with `Range: selector=#todo-list` → 2xx, the new `<li>`
(with its delete button) appended from the response body; step 2 sends a `PUT`
for `#todo-list > li:nth-child(4)` (DOMSubscriber wiring) → 2xx; after reload
"Bread" is present and checked. Refs: R-RW-70..72.

**ACC-FA-3 — Delete an item** (A; A)
Click the bin on "Buy Eggs"; reload. Expected: one `DELETE /` with the item's
selector → 2xx; after reload the item is gone. Refs: R-RW-80.

**ACC-FA-4 — No rule, no write** (A; A on `first-app-norules`)
Open `/`; tick the first box. Expected: the OPTIONS response lists no part for
`#todo-list li` with `PUT` (flat 204 or a 207 without write methods), so
`li.PUT` is undefined and the page throws `TypeError` (the one scenario where a
console error is expected); no write request is sent. A direct `PUT /` with the
same `Range` from the test → `401` with the error document of R-PERM-55.
Refs: R-PERM-44, R-PERM-54, R-PERM-63.

### 7.3 A2 — Build a blog

**ACC-BL-1 — Home composes posts** (B; curl + A)
Add `data/posts/hello-world.html` and `index.html`; GET `/index.html`; add
`data/posts/second-thoughts.html` (dates 2026-08-05); GET again with
JavaScript disabled in A.
Expected: `200`; the section lists "Second thoughts" before "Hello, world";
the source contains the excerpts, no `{%`, no `r:posts`, no `p:template`, no
`xmlns:`. Refs: R-APPS-9, R-APPS-10, R-PERM-77.

**ACC-BL-2 — One route for every post, and a real 404** (B)
Add `posts/:slug.html`. GET `/posts/hello-world.html` → `200` with the
stamped `<article>`; GET `/posts/nonsense.html` → `200` showing "Not found".
Add `processors.html`. `curl -sI /posts/nonsense.html` → `404`; `curl -sI
/posts/hello-world.html` → `200`; GET `/posts/nonsense.html` → `404` whose
body still shows "Not found". The 404 half requires the route's `<style>` rule
not to mention the Post itemtype (C-16): run it once with the template as
printed (result recorded, not release-blocking until P-APPS-22 is settled) and
once with `main:has(article[itemscope]) .post-missing` (release-blocking).
Refs: R-APPS-19.5, R-RW-24, R-REACT-26, D-ROUTE, D-STAMP.

**ACC-BL-3 — Archive and feed** (B)
Add `archive.html` and `feed.xml`. GET `/archive.html` → one `<h2>August
2026</h2>` followed by both rows. `curl -s /feed.xml` → `200`, an XML
`Content-Type`, starts with `<?xml`, two `<entry>` elements newest first, no
`p:`/`r:` attributes. Add a post dated 2026-09-02: a `September 2026` heading
appears and the feed has three entries. Refs: R-APPS-9, R-APPS-10.

**ACC-BL-4 — Comment from the page** (B; A, B)
Add the comment form and script to `posts/:slug.html`, and `rules.html`. A
opens `/posts/hello-world.html`, posts "Nice post".
Expected: `POST /posts/hello-world.html` `Range:
selector=#comments-hello-world` → 2xx; the comment appears in A; B reloads and
sees it; a dav GET of `/data/posts/hello-world.html` contains the comment
`<li>` inside `#comments-hello-world` and the route template is unchanged.
Refs: D-STAMP write-through, D-ROUTE, R-PERM-28.

**ACC-BL-5 — Shape makes public comments safe** (B; curl)
Run the tutorial's attack `curl` → 2xx (no constraint yet). Add
`constraints.html`. Delete the script comment over dav. Run the attack again
→ `422`, data file unchanged. Post an honest comment from the page → 2xx.
Refs: R-MOD-65, R-MOD-68, R-MOD-71.

**ACC-BL-6 — Data folder locked, pages still compose** (B; A anonymous, B
signed in as alice)
Add the two lock rules. A: `GET /data/posts/hello-world.html` → `401`; `GET
/index.html` → `200` listing the posts. B: the same data URL → `200`.
Refs: R-PERM-15, R-PERM-40, R-PERM-54, R-PERM-77.

**ACC-BL-7 — Drafts stay hidden** (B)
Add a post with `status` Draft. It is absent from home, archive and feed;
`curl -sI /posts/<draft-slug>.html` → `404`.

### 7.4 A4 — Polls

**ACC-PO-1 — Create a poll** (A; A)
On `/`, fill title "Team lunch", organizer "Ada", three dates, keep "List this
poll" ticked, submit.
Expected: `POST /templates/new-poll.html` (`application/x-www-form-urlencoded`)
→ `301`, `Location` matching `^/polls/[a-z0-9]{10}\.html$`; the browser lands
there (`200`): `<h1>` "Team lunch", organizer "Ada", three option columns,
empty `#responses`, no Liquid in the source. Back on `/`, the poll is the first
card; its count comes from `GET /polls/<id>.html` `Range:
selector=#responses` → `206` and reads "0 responses". A dav GET of the new
poll has no `<base>` element and no `p:template` attribute (decision C-12).
Refs: R-APPS-10, R-APPS-13.

**ACC-PO-2 — Unlisted poll** (A; A)
Create a poll with the box unticked → not on `/`, but reachable by its URL.

**ACC-PO-3 — Votes arrive live** (A; A, B)
A and B open the same poll. B enters "Grace", sets two votes, saves.
Expected: B sends `POST /polls/<id>.html` with `Range: selector=#responses`
and `Pagelove-Connection: <token from B's stream>` → 2xx; B's grid shows one
Grace row (not two); within 2 s A shows the row and the totals/"Best" markers
update, without reload. Refs: R-SSE-6, R-SSE-8, R-SSE-14, R-SSE-37, R-SSE-38.

**ACC-PO-4 — Edit and withdraw** (A; A, B)
B clicks Edit on its row, changes a vote, updates; then Remove.
Expected: `PUT` `Range: selector=#r-xxxxxxxx` → 2xx whose body is the row;
A's row is replaced within 2 s; `DELETE` same selector → 2xx; A's row
disappears within 2 s; B's `localStorage` no longer lists the row.

**ACC-PO-5 — Same session, second tab** (A; A, A′)
A and A′ open the poll; A saves a row.
Expected: A′ shows the row within 2 s (the write named A's stream in
`Pagelove-Connection`, so only that stream is suppressed); A does not show it
twice. Refs: R-SSE-38.

**ACC-PO-6 — Guards** (A; HTTP from the test)
Anonymous requests to an existing poll:
(a) POST `Range: selector=#responses`, row with an extra `<img src=x>` → `422`;
(b) POST row with `onclick` on the `<th>` → `422`;
(c) POST `Range: selector=tbody#responses` (rule matches, trigger regex does
not) → `403`;
(d) PUT `Range: selector=#poll` → `403`;
(e) whole-document PUT of the poll → `409`;
(f) whole-document DELETE → `401`;
(g) DELETE of an already-deleted row → `416`;
(h) whole-document PUT to a new `/polls/zzzz.html` → 2xx (trigger lets new
polls through).
The poll document is unchanged by (a)–(g). Refs: R-APPS-12, R-APPS-19,
R-MOD-65, R-PERM-54, R-RW-82.

**ACC-PO-7 — Stream reset reloads** (A; A)
With the page open, make the server drop A's stream and reconnect it with a
`Last-Event-ID` older than retention (pagelike test hook, or wait 10 min in the
`slow` variant). Expected: a `reset` event; the page reloads and shows current
rows. Refs: R-SSE-31..33.

### 7.5 A5 — Kanban (open mode unless stated)

Pre-step for every scenario: seed each context's `localStorage['board:me']`
with `{"sub":"u-alice","name":"Alice"}` / `{"sub":"u-bob","name":"Bob"}`
(the app's documented no-OIDC path).

**ACC-KB-1 — Home and profile gate** (A; fresh context C without a profile)
C opens `/` → board list; C opens a board → navigates to `/auth/login` (a
pagelike login page when an identity provider is configured; otherwise the
documented no-provider answer of R-PERM-67). Refs: R-APPS-15, R-PERM-67.

**ACC-KB-2 — Create a board** (A; A)
Create "Launch" with background "forest".
Expected: `PUT /boards/b-xxxxxxx.html` → 2xx; `POST /index.html` with `Range:
selector=#boards; placement=prepend` → 2xx; navigation to the board;
`GET /boards/b-….html?sync=…` → `200` whose `#whoami-server` text is `||||`
(anonymous) and whose source has no `pagelove:template`/`xmlns:pagelove`;
on first load the app posts `#labels`/`#members`/`#epics`/`#activity`
containers or member records with upserts (PUT → `416` → POST append).
Refs: R-APPS-9, R-APPS-10, R-RW-66.

**ACC-KB-3 — Lists and cards** (A; A)
Add lists "To do", "Done"; add three cards; rename a card; set a due date;
tick complete.
Expected: POSTs to `#lists` and `#L-…-cards` with `; placement=append` → 2xx;
PUT `#C-… [data-f="card-title"]` → 2xx; due date: PUT `#C-… [data-f="card-due"]`
→ 2xx (the meta exists in new cards); reload shows everything.

**ACC-KB-4 — Live sync** (A; A, B)
Both open the board; A adds a card; B renames another.
Expected: each change appears in the other context within 3 s; each context
performs a `GET …?sync=` after a remote mutation and none after its own write
alone (no echo, W-3). Refs: R-SSE-37.

**ACC-KB-5 — Drag and drop is one MOVE** (A; A, B)
A drags card 1 from "To do" to the end of "Done", then drags card 2 before
card 1.
Expected per drop: exactly one `MOVE /boards/b-….html` with `Range:
selector=#C-…`, `Destination: /boards/b-….html`, `Destination-Range:
selector=#L-…-cards; placement=append` (then `selector=#C-1; placement=before`)
→ 2xx, followed by exactly one verification `GET …?v=…` `Range:
selector=#L-…-cards` → `206` containing the card id (no retry MOVE). B sees the
new order within 3 s; a reload shows it. Refs: R-APPS-16, R-APPS-25,
R-PROTO-90..96, R-PERM-47.

**ACC-KB-6 — Concurrent moves both land** (A; scripted)
From two contexts, issue 12 pairs of simultaneous MOVEs of different cards
into the same list using the app's `moveVerified`.
Expected: 24/24 MOVEs 2xx, every verification succeeds on the first read,
final document contains every card exactly once. Refs: R-APPS-16 (W-1).

**ACC-KB-7 — Archive and restore** (A; A)
Archive a card (writes `card-home`, `card-archived-at`, then MOVE to
`#archive`), restore it from the menu (MOVE back to its home list), archive and
restore a list. Expected: all 2xx, positions correct after reload.

**ACC-KB-8 — Checklists, comments, attachments** (A; A)
Open a card; add a checklist and two items; post a comment; attach a link.
Expected: containers created by `POST #C-…; placement=append` → 2xx; each add
is exactly one `POST` → 2xx (no 416 retries, W-2); the activity log gets one
`POST #activity` per action.

**ACC-KB-9 — Uploads** (A; A)
Paste a 50 KB PNG into a description, attach an 8 MB file.
Expected: `PUT /uploads/C-…-<ts36>-<name>` with the file's type → 2xx each;
`GET` of each URL returns identical bytes and type; the description stores
`![name](/uploads/…)`. Refs: R-APPS-18.

**ACC-KB-10 — Presence** (A; A, B)
Expected: within 12 s each context shows the other's avatar (upsert of
`#V-<sub>` into `#presence` every 10 s); closing B sends `DELETE` `Range:
selector=#V-u-bob` with keepalive → 2xx; A drops B's face within 30 s.

**ACC-KB-11 — Cross-document write** (A; A)
Change the board background. Expected: `PUT /boards/…` `Range: selector=body
[data-f="board-bg"]` (upsert) and `PUT /index.html` `Range: selector=#b-…
[data-f="bg"]` → 2xx; the home tile shows the new colour.

**ACC-KB-12 — No update nag** (A; A)
`GET /version.txt?v=…` → `200`, `text/plain`, body `1789423173`; no "new
version" bar appears. Refs: R-APPS-4, R-APPS-21.

**ACC-KB-13 — Big board stays fast** (C)
Import a board with 400 cards and 60 activity entries (~1 MB); measure 50
card-title edits and 50 drops. Expected: R-APPS-17 targets met.

**ACC-KB-14 — Members mode** (C; A signed in as alice, B anonymous)
Expected: A's `#whoami-server` reads `<sub>|Alice|alice@example.com||` and the
board works without a local profile; B's `GET /boards/…` → `401` and the page
sends B to `/auth/login`. Refs: R-APPS-10.4, R-PERM-44, R-PERM-74.

### 7.6 A6 — ATS (default-GET deny)

**ACC-AT-1 — Careers page** (B; anonymous A)
Open `/`. Expected: `GET /roles.html?t=…` `Range: selector=#roles-body` →
`206` (a `<tbody>` with the three seeded roles); two "Open" role cards link to
`/roles/founding-engineer/` and `/roles/community-manager/`.
Refs: R-APPS-5, R-APPS-8, R-PERM-77.

**ACC-AT-2 — Role pages from one route** (B; anonymous A)
Open `/roles/founding-engineer/` → `200`, title "Founding Engineer", JD
rendered; `/roles/product-designer/` (status "On Hold") → the script redirects
to `/`; `/roles/nope/` → "Role not found". The served HTML has `<table
id="all-roles-data" hidden>` with the `<tbody id="roles-body">` **inside** it
and no `p:include`. Refs: R-APPS-7, R-APPS-8, R-APPS-9.

**ACC-AT-3 — Apply anonymously** (B; anonymous A)
On `/apply.html?role=R-001` attach a 1 MB PDF and submit.
Expected: `PUT /uploads/C-xxxxxx-cv.pdf` (`application/pdf`) → 2xx; `POST
/admin/index.html` `Range: selector=tbody#candidates-body` → 2xx; success view
shows the reference; an anonymous GET of the CV URL → `401`.
Refs: R-APPS-18, R-MOD-66 (union coverage of `a[class]` + `a[href]`).

**ACC-AT-4 — Admin is private, staff get in** (B; A anonymous, B admin1, C bob)
A: `GET /admin/` and `/admin/index.html` → `401` with a login link. B signs in
at `/auth/login` → `/admin/` `200` showing the new candidate; the CV link
downloads (`200`, `application/pdf`). C → `403`.
**ACC-AT-4b** (same with the per-email rules deleted over dav): B still gets
`200` through the `admins` Group; with the Group deleted too and only the
`GroupMembership` table left, B still gets `200`. Refs: R-PERM-19, R-PERM-54,
R-PERM-58, R-PERM-61 (W-9, W-10).

**ACC-AT-5 — Staff edits** (B; B)
Change the candidate's stage, add an interview, add an activity note, delete
the interview.
Expected: `PUT /admin/index.html` `Range: selector=#C-…` → `206` whose body is
the new `<tr>` (the UI swaps it in; no stray text "null"); POSTs to
`#interviews-body`/`#activity-body` → 2xx with the row as body; `DELETE` →
2xx. Refs: R-RW-64, R-RW-72.

**ACC-AT-6 — Public window stays narrow** (B; HTTP, anonymous)
(a) POST to `tbody#candidates-body` with `<img src=x onerror=alert(1)>` in a
cell → `422`; (b) POST `Range: selector=#roles-body` → `401`; (c) PUT
`Range: selector=#C-a1001x` → `401`; (d) GET `Range: selector=#roles-body` on
`/admin/index.html` → `401`. Refs: R-MOD-65, R-PERM-42, R-PERM-51 (W-11).

**ACC-AT-7 — New role gets a public page** (B; B)
Create a role "Staff Engineer" (slug `staff-engineer`, Open).
Expected: POST `#roles-body` → 2xx; anonymous `/roles/staff-engineer/` →
`200` rendering it; `/` lists it.

**ACC-AT-8 — Outbox relays email** (C; B + sink)
Send an email to the candidate from the admin UI.
Expected: `PUT /outbox/E-xxxxxxxx.json` (`application/json`) → 2xx; within
10 s the sink receives one `POST` with `Content-Type: application/json`,
`Accept: application/json`, an empty `X-Postmark-Server-Token`, and a body
byte-identical to the uploaded JSON; the admin's response did not wait for it.
Refs: R-APPS-19.4.

**ACC-AT-9 — Rule tables parse** (B; OPTIONS)
As B, `OPTIONS /admin/index.html` with `Accept: multipart/mixed` lists
`tbody` and `tr[itemtype]` parts with `POST, PUT, DELETE`; as anonymous, only
`tbody#candidates-body` with `POST`. Refs: R-PERM-2a, R-PERM-63, R-PROTO-17.

### 7.7 A7 — Shop

**ACC-SH-1 — Catalogue and product pages** (B; A)
`/` lists the six products sorted by name, cards linking to
`/products/<slug>.html`, emoji fallbacks (no images yet); `/products/tee.html`
→ `200` with the stamped Product (five variants) and the buy panel;
`/products/nope.html` → `200` with no product. No page triggers a credential
prompt. Refs: R-APPS-10.1, R-APPS-26, R-PERM-77 (W-15, W-16).

**ACC-SH-2 — Baskets are per session** (B; A, B)
A adds a tee (M ×2) and a mug; B adds a cap.
Expected: each add is `GET /basket.html` `Range: selector=#basket` then `PUT`
with the same range → 2xx; A's basket page shows two lines, B's one; both
responses `Cache-Control: private`; a dav GET of `/basket.html` still has the
empty default `<ul id="basket" p:transient></ul>`; the home page (bindings)
is unaffected. Refs: R-APPS-27.

**ACC-SH-3 — Basket identity and reset** (B; HTTP as A's session)
PUT `<ul id="other"></ul>` → `422`; `DELETE` `Range: selector=#basket` → 2xx
and the next GET returns the default. Refs: R-APPS-27.

**ACC-SH-4 — Basic-auth admin gate** (B; A)
A opens `/admin/index.html`.
Expected: `401` with `WWW-Authenticate: Basic realm="Pagelove Shop admin"` →
native prompt; with `owner`/`test` → `200`; the chrome now shows Orders /
Products / Settings (Liquid on `request.headers.authorization`); with a wrong
password → `401`; `HEAD /admin/index.html` without credentials → `401`.
Refs: R-APPS-12, R-APPS-14, R-APPS-19 (W-13).

**ACC-SH-5 — Product admin** (B; A authenticated)
Create a product with an uploaded image, edit it, delete it.
Expected: `PUT /images/<stem>.webp` (image type) → 2xx; `PUT
/data/products/<slug>.html` `Content-Type: text/html; charset=utf-8` → 2xx; the
product appears on `/` (sorted) if available; `DELETE` → 2xx. The same PUT
without `Authorization` → `401` "Admin credential required…". A PUT whose
product duplicates an existing `sku` → `422` (uniqueness). Refs: R-MOD-30.

**ACC-SH-6 — Settings stamped, source private** (B; A authenticated, B
anonymous)
Save `https://checkout.example` in Admin → Settings.
Expected: `PUT /data/settings/shop.html` `Range:
selector=[itemprop="checkoutEndpoint"]` → 2xx; B: `GET /checkout.html` → `200`
containing the stamped `ShopSettings` with that value; `GET
/data/settings/shop.html` → `401`. Refs: R-PERM-40, R-PERM-77.

**ACC-SH-7 — Checkout end to end** (C; A with worker and Stripe stub)
Set the celld origin; A checks out.
Expected: worker `GET /data/products/<slug>.html` → `200` (public); dav `PUT
/data/orders/<id>.html` `If-None-Match: *` → 2xx; a replay of the same
checkout request → worker sees `412` and reuses the order; browser is sent to
the stub URL; `/orders/<id>.html` → `200` showing "Confirming payment"; a
signed `checkout.session.completed` webhook → worker dav GET (`ETag`) + `PUT
If-Match` → 2xx; the order page shows "Paid"; `GET /data/orders/<id>.html` →
`401`. Refs: R-APPS-24.

**ACC-SH-8 — Order shape holds on dav** (B; dav)
dav PUT of an order document with an extra `<script>` inside the Order → `422`
(pipeline error embedded); without it → 2xx. Refs: R-PROTO-112, R-MOD-65.

**ACC-SH-9 — Admin order status** (B; A authenticated)
Mark an order paid then fulfilled. Expected: `PUT /data/orders/<id>.html`
`Range: selector=[itemprop="paymentStatus"]` → 2xx each; `/admin/orders/<id>.html`
reflects it; `paymentStatus` `shipped` (not in the enum) → `422`.
Refs: R-MOD-23.

**ACC-SH-10 — Private stays private** (B; HTTP)
`GET /private/admin.html` without and with the correct Basic header → `401`
both times; `PUT` → `401`. Refs: R-APPS-14, R-PERM-40.

### 7.8 A8 — demo-apps

**ACC-DM-1 — Live smoke** (A; HTTP)
The eight `PUBLIC_PATHS` of `scripts/check-live.py` (`/`, the five
`/demo-0N-…/`, `/favicon.svg`, `/assets/og-demo-apps.png`) → `200`; `/`
contains no `__PAGELOVE_HOST__`; anonymous `GET
/demo-04-accountability/feed.html` → `401`. Refs: R-RW-3, R-PERM-44.

**ACC-DM-2 — Event** (A; A)
As Elena: RSVP → `POST` `#pl-rsvps` 2xx; RSVP again from a second context
(same attendee) → `422`, toast "You've already RSVPed…"; cancel → `DELETE
#rsvp-elena` 2xx; seed a waitlisted RSVP, cancel a "going" one → `PUT
#rsvp-<x> meta[itemprop='rsvpStatus']` 2xx. Refs: R-APPS-20, R-MOD-30 (W-20).

**ACC-DM-3 — Show and tell** (A; A)
Publish a project (`POST #pl-grid` 2xx); react (`POST #pl-reactions` 2xx);
react again after reload (same voter) → `422`; comment on the new project
(`POST #comments-<id>` 2xx — requires composed closed shapes). Refs: R-MOD-67.

**ACC-DM-4 — Resource exchange** (A; A as alice, B as ben)
A posts a listing (2xx); A claims it (`POST #pl-claims` 2xx, `PUT
#status-<id>` 2xx); B claims it → `422` "uniqueness"; A marks completed (2xx);
a `PUT #status-<id>` back to `claimed` → `422`. Seeded "drill" (claimed):
release → `DELETE #claim-listing-drill` 2xx + `PUT` `open` 2xx.
Refs: R-MOD-33, transitions (W-22, W-23).

**ACC-DM-5 — Accountability** (A; A anonymous, B signed in)
A checks in (`POST /demo-04-accountability/feed.html` `#pl-checkins` 2xx);
again the same day → `422`; "peek" button → toast "Denied as expected: 401";
B opens the feed → `200` including A's check-in. Refs: R-PERM-15, R-PERM-40
(W-24).

**ACC-DM-6 — Professional** (A; A)
Topic, reply, `https://` resource → three POSTs 2xx; a `javascript:` URL is
rejected before any request (0 writes). Refs: tests/demo-pages.spec.js:94-113.

### 7.9 A9–A10 — Client libraries

**ACC-BJ-1 — beta-js unit tests** (A)
`cd research/upstream/beta-js && npm ci && npm test` passes (sanity: server-free
contract tests, §8.2).

**ACC-BJ-2 — beta-js self-hosted on pagelike** (B)
After `sync-webdav.sh --all` (with `cors.html`), run
`PAGELOVE_WEBDAV_URL=http://dav-betajs.localhost:<port>/
PAGELOVE_PUBLIC_URL=http://betajs.localhost:<port>/
.github/scripts/verify-deploy.sh`. Expected: "Verified N module(s)" with every
`.mjs` `200`, `Access-Control-Allow-Origin: *`, `text/javascript`, bytes that
parse. Without `cors.html`: the same script fails on the missing header
(pagelike adds no blanket CORS). Refs: R-APPS-4, R-APPS-19.6, R-APPS-22.

**ACC-BJ-3 — Cross-origin import from a pagelike host** (B; A)
Repeat ACC-FA-1..3 with `https://pagelove.github.io/beta-js/**` routed to
`http://betajs.localhost:<port>/**`. Expected: identical results.

**ACC-BJ-4 — Widget recipe with `sse.mjs`** (A; A, B)
Page from D-R-WID (sales table + listener) with a rule allowing PUT on
`[itemtype="https://example.com/SalesRecord"] [itemprop="revenue"]`. B changes
February's revenue with a selector PUT. Expected: within 2 s A fires
`PLMutationApplied` with `detail.selector` of that cell and re-reads 51000 →
new value; a `PLMutation` listener calling `preventDefault()` keeps the old
DOM. A write from A itself fires nothing in A. Refs: R-SSE-43.

**ACC-BJ-5 — Primitives recipe** (A; A)
D-R-PRIM "complete example" on `/about.html` with a GET rule and a PUT rule
on `h1`. Expected: `OPTIONS /about.html` → `207` whose `h1` part uses
`Content-Range: selector h1` (space form, W-28); `heading.GET()` returns the
fragment; `new PLElement(url, h1).PUT()` after a lazy `HEAD` sends `If-Match`
with the element's ETag → `206`; a second PUT with a stale tag → `412`.
Refs: R-PROTO-3, R-PROTO-17, R-RW-86.

### 7.10 A3 — Recipes (one scenario each)

| ID | Recipe | Steps and expected observations |
|---|---|---|
| ACC-RC-1 | D-R-TX | Schema with `@write` lowercase slug and `@read` date format; `PUT` a Project with `My-Project` → stored `my-project` (dav GET); a stored `2026-04-08T09:15:00Z` reads back as `8 April 2026` with the `datetime` kept. |
| ACC-RC-2 | D-R-LINK | Second Project with `slug` launch → `422`; a `project` reference to nothing → `422` naming the reference; deleting a referenced Project with `onDelete` restrict → `409`; `GroupConstraint` contact `1..n`: an item with neither email nor phone → `422`. |
| ACC-RC-3 | D-R-GRP | Group `editors` with alice (verified) and a rule for `/blog/*`: alice POST → 2xx, bob → `403`; edit the Group to swap alice for bob (no rule edit) → the next requests flip. |
| ACC-RC-4 | D-R-HOOK | Processor on `POST /notes/*` Note items, HttpRequest to the sink: a note POST returns before the sink receives `{"path": "/notes/…"}`; with `retry` 3 and a failing sink, 4 attempts with growing gaps; the client response is unaffected. (Tier C: outbound-http.) |
| ACC-RC-5 | D-R-PAGE | 23 contacts, `p:paginate="10"`: page 2 has 10 items and `Link` rel first/prev/next/last; `?paginate:page=9` clamps to 3; `?q=smith&paginate:page=2` keeps `q=smith` in links; `Range: selector=ul#contacts` on page 2 → `206` with only page 2; two paginators navigate independently. |
| ACC-RC-6 | D-R-CU | The four Membership writes: 2xx, 2xx, 2xx, `422` (W-19 contradiction resolved per docs). |
| ACC-RC-7 | D-R-PRIM | = ACC-BJ-5. |
| ACC-RC-8 | D-R-WID | = ACC-BJ-4. |
| ACC-RC-9 | D-R-SM | Rules document with the four constraints: create at pending 2xx; pending→processing 2xx and an SSE `mutation` observed; pending→success `422` with the documented ConstraintViolation fields; two racing legal steps with `If-Match` → one 2xx, one `412`, retry `422`; handler `becomes processing` POSTs a Transition document to the sink once; delete without the exit rule → `422`, with it → 2xx; two unkeyed Orders in one document wedge until repaired over dav; dav edit fires no handler. |

## 8. Pointing the upstream tests at pagelike

### 8.1 demo-apps Playwright suite (`tests/demo-pages.spec.js`)

The suite starts its own static server over `.deploy-ci`
(`startStaticServer(".deploy-ci", 0)`) and builds `baseURL =
http://127.0.0.1:<port>`; it then checks each route for HTTP success, a
visible `<h1>`, zero axe WCAG A/AA violations, zero console errors, load time
< 3 s and transfer < 1.5 MB, the same routes with JavaScript disabled, and the
`javascript:` URL guard (`:38-113`). To run it **unmodified** against
pagelike:

1. Deploy the demos to site `demos` (§6.3).
2. Run Playwright with `node --import <shim-register>.mjs`, where the shim is a
   resolve hook (the same technique beta-js uses in
   `test/helpers/register.mjs`/`loader.mjs`) that redirects
   `scripts/serve-test.cjs` to a module whose `startStaticServer()` returns an
   `http.Server` listening on `127.0.0.1:0` that **reverse-proxies** every
   request to `http://demos.localhost:<pagelike port>` with `Host:
   demos.localhost:<port>` (pagelike routes by Host, so a bare `127.0.0.1`
   request would not reach the site). `server.address()`, `close()` and
   `closeAllConnections()` keep their meaning.
3. Expected: all three tests pass. Failures map to R-APPS-3 (console/CSP),
   R-APPS-4 (asset types), R-RW-3 (directory index), R-APPS-17 (budget).

`bun run prepare:test` uses `--host ci-demo.example`; use `--host
demos.localhost` instead so vocabulary itemtypes are stable per site (any
host-less name works; the tests do not depend on it).

### 8.2 beta-js node tests (`test/*.test.mjs`)

They run entirely in jsdom with `FakeEventSource` and a scripted `fetch`; no
request leaves the process, so they cannot be pointed at a server. They are
still valuable as **contracts** pagelike's output must satisfy:
`mutationPayload()` (`test/helpers/dom.mjs:83-94`) is the event format the
client is tested against, and `createdResponse()` models a POST answer (201,
`ETag`, HTML body; C-1). Proposed pagelike-side adapter (ACC-UP-1): a test
that opens a real SSE stream to pagelike with the `eventsource` package, feeds
each received `data` into `pagelove/sse.mjs` under the same jsdom setup
(replacing `FakeEventSource.emit` input), and asserts the DOM outcome of POST,
PUT, DELETE and MOVE writes made through pagelike — i.e. the existing test
bodies with server-produced payloads.

### 8.3 pagelove-primitives `test.html`

The page registers `test-sw.js`, a Service Worker that answers every
same-origin OPTIONS and `Range` request with canned responses (including
`200` for PUT and a mix of `selector #x` and `selector=#x` parts). It cannot
be pointed at a real server without changing the expectations. ACC-UP-2:
serve `test.html`, `test-sw.js` and `index.mjs` from a pagelike site and open
`test.html` — this checks that pagelike serves the Service Worker script with
a JavaScript type so registration succeeds (R-APPS-4); all assertions should
pass because the SW answers them. Live behaviour of the same library against
pagelike is covered by ACC-BJ-5 (beta-js primitives are its successor).

### 8.4 Shop worker tests (`worker/test/worker.test.js`)

`npm run check` in `worker/` tests pricing, order documents, Stripe form data
and webhook signatures without any network. ACC-UP-3: run them unchanged
(sanity), then **validate the generated order document against pagelike**: PUT
`orderDocument()` output for a sample cart over dav to a site with the shop's
`schemas.html` and `constraints.html` → 2xx (R-APPS-24).

### 8.5 Scripts

| Script | Against pagelike | Scenario |
|---|---|---|
| demo-apps `scripts/check-live.py --host <h>` | Hard-codes `https://` and a port-less host (`:25-29, 50`). Run pagelike behind TLS on 443 (`deploy/Caddyfile.example`) as `demos.pagelike.test` with the local CA in `SSL_CERT_FILE`. | ACC-UP-4 (C): exit 0, "Live checks passed for 8 public resources; the members-only feed returned 401 anonymously." |
| demo-apps `scripts/check-repo.py`, `test-deploy.py` | Repository-only; no server. | — |
| demo-apps `docs/LIVE-TESTING.md` §3 | The five manual write checks. | = ACC-DM-2..6 |
| beta-js `sync-webdav.sh`, `verify-deploy.sh` | Accept `http://` URLs with ports. | ACC-00, ACC-BJ-2 |
| shop `ops/deploy-pagelove.sh`, `deploy-admin-password.sh` | Accept `http://` WebDAV URLs. | ACC-00, §6.3 |
| shop `ops/configure-stripe-webhook.sh`, `install-celld.sh` | exe.dev/Stripe specific; not run. | — |
| shop GitHub workflow | Needs a GitHub environment; equivalent to running the two ops scripts. | ACC-UP-5 (C): run the workflow with `act` pointing `PAGELOVE_WEBDAV_URL` at pagelike. |

## 9. Harness cases in `harness/cases/apps/`

HTTP-level slices of the flows above, runnable without a browser
(`HARNESS_FILTER=cases/apps go test ./harness -run TestLocalCases`). On the
2026-09-28 build of pagelike the 34 cases load; the stage-1 flows (first app
CRUD and rule refusal, kanban fragment client, MOVE and concurrent MOVE,
directory index, member reads) pass — as does the disputed verbatim blog
case, but only because routes are not resolved yet — and the rest fail on
features not built yet (shapes, uniqueness, transitions, Liquid, Sessel bindings and triggers,
transients, templated creation, directory-URL POST). Types and
schemas that are host-wide on PageLove are made unique per run
(`https://pagelike.test${P}/…`), and rule/processor/trigger resources are
rewritten under `${P}`; otherwise the markup is the app's own.

| File | Cases | Requirements |
|---|---|---|
| `first-app.yaml` | `apps.first-app.shopping-list-crud`, `apps.first-app.write-needs-rule`, `apps.first-app.directory-url-writes` | A1, R-APPS-6 |
| `blog.yaml` | `apps.blog.home-composes-posts`, `apps.blog.route-stamp-and-404`, `apps.blog.route-404-verbatim-template` (disputed, C-16), `apps.blog.comment-write-through-and-shape`, `apps.blog.data-folder-locked`, `apps.blog.data-folder-member`, `apps.blog.feed-xml` | A2, R-APPS-9/10/19 |
| `polls.yaml` | `apps.polls.vote-edit-withdraw`, `apps.polls.trigger-guards`, `apps.polls.templated-create`, `apps.polls.templated-create-stored-form` | A4, R-APPS-12/13 |
| `kanban.yaml` | `apps.kanban.fragment-client-upsert`, `apps.kanban.move-verified`, `apps.kanban.concurrent-moves-land`, `apps.kanban.home-prepend`, `apps.kanban.whoami-template-any-prefix`, `apps.kanban.whoami-signed-in` | A5, R-APPS-9/10/16/25 |
| `ats.yaml` | `apps.ats.public-apply-window`, `apps.ats.roles-include-in-table`, `apps.ats.route-malformed-xmlns`, `apps.ats.staff-row-echo` | A6, R-APPS-7/8/9 |
| `shop.yaml` | `apps.shop.transient-basket`, `apps.shop.admin-basic-gate`, `apps.shop.order-confirmation-elevated`, `apps.shop.order-shape-dav` | A7, R-APPS-12/14/24/27 |
| `demos.yaml` | `apps.demos.event-rsvp-unique`, `apps.demos.show-and-tell-composed-shapes`, `apps.demos.exchange-claim-and-transitions`, `apps.demos.accountability-feed-gate`, `apps.demos.accountability-feed-member`, `apps.demos.directory-index` | A8 |

## 10. Open questions for live probing

Minimal sequences for a disposable PageLove host, `P` = a fresh prefix, anon =
no cookies, author = dav plane.

- **P-APPS-1 (C-12) stored form and redirect of templated creation.** author
  PUT `P/t.html` = `<!DOCTYPE html><html xmlns:p="https://pagelove.org/1.0"
  p:template="text/liquid"><head><base href="P/out/{{ request.body.slug
  }}.html"><title>{{ request.body.title }}</title></head><body><h1>{{
  request.body.title }}</h1></body></html>` and a rule `* POST P/t.html`, `*
  PUT P/out/*`. anon `POST P/t.html` form `slug=a&title=Hi` → record status,
  `Location` (path vs absolute), body. author GET `P/out/a.html` → is `<base>`
  present? is `p:template` present? `xmlns:p`? Repeat the POST → status (409?
  overwrite?).
- **P-APPS-2 (C-2) header lookup case.** Trigger on `P/h.html` method GET,
  `when` `Context.request.headers["X-Probe"] != null`, action throws 418; a
  second trigger identical with `"x-probe"` and 419. anon GET with `X-Probe: 1`
  → 418, 419 or 200. (Same question as reacting P-2 and sessel SP-36.)
- **P-APPS-3 (R-APPS-8) include in a table.** author PUT `P/src.html`
  (`<table><tbody id="rb"><tr><td>x</td></tr></tbody></table>`) and
  `P/inc.html` (`<table id="t"><p:include resource="P/src.html"
  selector="#rb"></p:include></table>`, `xmlns:p`). anon GET `P/inc.html` →
  is `#rb` inside `#t`? anon GET `Range: selector=#t > tbody#rb` → 206 or 416?
- **P-APPS-4 (C-11) writes via a directory URL.** author PUT
  `P/d/index.html` with `<ul id="l"></ul>` and a rule `* POST P/d/index.html
  selector #l`. anon `POST P/d/` `Range: selector=#l` → status; author GET
  `P/d/index.html` → appended? Same for `OPTIONS P/d/` (207 parts?) and an SSE
  subscription on `P/d/` receiving a write made to `P/d/index.html`.
- **P-APPS-5 (R-APPS-7) directory-param route.** author PUT
  `P/r/:s/index.html` (Sessel binding stamping `request.params.s`). anon GET
  `P/r/x/` → 200? `P/r/x/index.html` → 200? `P/r/x` → 301 to `P/r/x/` or 404?
- **P-APPS-6 (C-4) POST into a transient element.** author PUT `P/b.html` with
  `<ul id="c" p:transient></ul>` and rules for POST/PUT `#c`. anon (cookie jar
  1) POST `Range: selector=#c` `<li>a</li>` → status; jar 1 GET `#c` → has a?
  jar 2 GET `#c` → default?
- **P-APPS-7 (R-APPS-19.5) processor on HEAD.** The blog processor under `P`;
  anon `HEAD P/posts/nonsense.html` → 404?
- **P-APPS-8 (R-APPS-10.2) `@id` in Liquid.** author PUT `P/a.html` with
  `<div id="x" itemscope itemtype="https://pagelike.test/P/T">` and `P/l.html`
  with `r:t="[itemtype='https://pagelike.test/P/T']"` and `{{ t.first['@id']
  }}`. anon GET `P/l.html` → exact value (`/…/a.html#x`? absolute URL?). Also
  an item without `id`.
- **P-APPS-9 (C-1) selector POST status.** anon POST into an allowed selector
  → 201 or 206? headers `ETag`, `Content-Location`, `Content-Range`.
- **P-APPS-10 (W-1) If-Match on a selector PUT.** GET `Range: selector=#a` →
  capture ETag; PUT with that `If-Match` → 2xx? PUT with `If-Match: *` → 2xx?
  stale tag → 412?
- **P-APPS-11 (R-APPS-26) order of site-wide results.** author PUT
  `P/z/1.html` and `P/a/2.html`, each with one item of type
  `https://pagelike.test/P/O` carrying a different `v`; page with Sessel
  binding `${[itemtype='…O'] [itemprop='v']}.first().value()` stamped; anon GET
  → which value? Rename `P/a/2.html` to `P/y/2.html` over dav → changes?
- **P-APPS-12 (R-APPS-9) malformed xmlns attribute.** author PUT a page whose
  `<html>` is `<html lang="en" xmlns:example co="https://pagelove.org/1.0"
  xmlns:p="https://pagelove.org/1.0">` with a `<p:include>`; anon GET →
  200? which attributes remain on `<html>`?
- **P-APPS-13 (C-3)** covered by permissions P-probes (`request.auth.role` vs
  `roles`).
- **P-APPS-14 (R-APPS-21) cache headers.** anon GET a stored `.js` blob and an
  `.html` page → `Cache-Control`, `ETag`, `Last-Modified`; wait 1 min after a
  dav overwrite and GET again (served fresh?).
- **P-APPS-15 (W-26) Sessel rethrow of a non-HTML body.** Processor on GET
  `P/m.mjs` (`status 2xx`) with a Sessel action throwing `HTTPResponse
  {status: 200, body: Context.response.body}`; blob content `a => a && b` →
  served bytes escaped (`=&gt;`) or intact? Then the JS-module variant with
  `headers` → ACAO present, body intact? (Same as reacting P-8.)
- **P-APPS-16 (R-APPS-14) trigger-thrown header.** SHOP's GET trigger under
  `P`; anon GET → `401` and exact `WWW-Authenticate`; with the right Basic
  header → 200; `HEAD` without → 401?
- **P-APPS-17 (W-10)** needs a signed-in actor with a verified email: Group
  only (no per-email rule) grants GET? `GroupMembership` only?
- **P-APPS-18 (C-9) selector allow leaking.** default-GET deny host; rule `*
  GET P/s.html selector #pub`; anon GET `Range: selector=#priv` → 401 (docs)
  or 206 (ATS claim)? Add `* GET P/s.html selector #priv deny` → result?
- **P-APPS-19 (R-APPS-13.3) triggers during templated creation.** P-APPS-1
  setup plus the polls whole-document PUT trigger on `P/out/*`; POST the form
  twice with the same slug → second answer 409 (trigger ran) or 301?
- **P-APPS-20 (R-APPS-18) upload size.** anon PUT 8 MB and 32 MB blobs to an
  allowed path → statuses; GET back length.
- **P-APPS-22 (C-16) the blog's 404 as printed.** Install D-L2's
  `data/posts/hello-world.html`, `posts/:slug.html` (verbatim, including the
  `<style>` rule naming `https://blog.example/Post`) and `processors.html`
  under `P`. anon `HEAD P/posts/nonsense.html` → 404 (the tutorial's claim) or
  200 (substring semantics)? Repeat with the style rule removed → 404 expected.
- **P-APPS-21 write without read.** Rules `* PUT P/w.html` and `* GET P/w.html
  deny`; anon selector PUT → status and response body (does the 206 echo the
  element to a principal that cannot read it?).
