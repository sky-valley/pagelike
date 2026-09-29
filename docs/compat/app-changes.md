# Changes made to the official apps for the release acceptance suite

The release acceptance suite (`e2e/tests/apps/*.spec.mjs`, scenarios in
`docs/spec/apps.md` §7) runs the official PageLove apps against pagelike
**without changing what they do**. This file records every change made to an
app, a tutorial or a recipe to run it, and why. Anything not listed here is
installed byte-for-byte from the pinned checkouts in
`research/upstream/<repo>` (commits in `research/COMMITS.txt`) or copied
verbatim from the docs snapshot (`research/docs/2026-09-28/md`).

Nothing below rewrites app behaviour to bypass an unsupported runtime
feature. The changes are of four kinds only: **configuration** (endpoints,
the identity provider), **fixture data** the app expects the operator to
supply (rules documents, seed records), **assembly** of documents a tutorial
or recipe prints in pieces, and **scenario steps** that §7 itself prescribes
(for example "delete the per-email rules over dav").

## Identity used for every scenario that needs one

pagelike **local accounts** with verified e-mail addresses
(`pagelike user add --site S NAME --email E --verified --name N`), signed in
through `/auth/login` (which redirects to pagelike's `/-pagelike/login` form
when a site has no OIDC provider). The in-process fake OIDC provider
(`internal/identity/oidctest`) is not used by the browser suite.

| App | Accounts | Why |
|---|---|---|
| Kanban, members mode (ACC-KB-14) | `alice` (alice@example.com), `bob` (bob@example.com), with `name` claims | the `users` actor and `request.auth.claims.*` |
| ATS (ACC-AT-*) | `admin1` (admin1@example.com), `admin3` (admin3@example.com), `bob` (bob@example.com), all verified | the `admins` Group and per-email rules match verified e-mail (R-APPS-15) |
| demo-apps feed (ACC-DM-5) | `alice` (alice@example.com) | demo 04's `users` read rule |
| Blog (ACC-BL-6) | `alice` (alice@example.com) | the tutorial's `users` rule |
| Group recipe (ACC-RC-3) | `alice`, `bob`, `dave` (verified), `mallory` (carol@example.com, **unverified**) | Group membership keys on verified e-mail |

## Per app

### A1 — Tutorial "Your first Pagelove application" (`first-app.spec.mjs`)

| File | Change | Why |
|---|---|---|
| `/index.html` | Assembled from the snapshot: the final document (learn/your-first-app, code block after "Our full application should now look like this") with its inline `<script type="module">` replaced by the DOMSubscriber version | exactly the §6.3 install instruction |
| `/index.html` on site `first-app-norules` | The same document without its two rule `<div>`s | §6.3 (ACC-FA-4) |

Third-party modules are served from the pinned checkouts (§6.2):
`https://pagelove.github.io/beta-js/**` from `beta-js@c204746`;
`https://cdn.pagelove.net/js/dom-subscriber/cde4007/index.mjs` from
`dom-subscriber`'s pinned checkout (`6b77709`), whose `index.mjs` beta-js
vendors and documents as identical to `cde4007` (header of
`beta-js/pagelove/dom-subscriber.mjs`); `PAGELIKE_E2E_GIT_SHOW=1` makes the
suite read the blob with `git show cde4007:index.mjs` instead. Google Fonts
requests are aborted. For ACC-BJ-3 the beta-js URLs are fetched from the
pagelike site `betajs` (installed with beta-js's own sync script) and passed
through with pagelike's headers.

### A2 — Tutorial "Build a blog" (`blog.spec.mjs`)

All files are the printed code blocks, added in tutorial order.

| File | Change | Why |
|---|---|---|
| `data/posts/second-thoughts.html`, `september.html`, `secret-plans.html` | Copies of `hello-world.html` with slug, title, excerpt, dates, month and (drafts) status changed | the tutorial's own instruction ("Copy hello-world.html … and change its data"), §7.3 BL-1/3/7 |
| `posts/:slug.html` | Second variant with the `<style>` rule `main:has(article[itemscope]) .post-missing` instead of `main:has([itemtype="https://blog.example/Post"]) .post-missing` | §7.3 ACC-BL-2 and contradiction C-16: as printed, the page's own stylesheet contains the Post itemtype, so the processor's substring test can never fire. The printed template is run first and its result (200) recorded; the variant is the release-blocking run and is used from then on |
| `posts/:slug.html` | Comment form inserted inside `<main>` between `.post-missing` and the back link; script inserted before `</body>` | the tutorial's placement instructions |
| `rules.html` | The two lock rules appended inside `<body>` | tutorial §Locking the data folder |
| `data/posts/hello-world.html` | The `<script>` comment left by the first attack removed over WebDAV | tutorial §Making comments safe ("delete the script comment") |

### A3 — Recipes (`recipes.spec.mjs`, `betajs.spec.mjs`)

Recipes print fragments, not sites. Each recipe site gets a permissive rules
document (`* <method *> /* allow`), since recipes show no rules; the
fragments are wrapped in the documents they belong in.

| Recipe | Change | Why |
|---|---|---|
| Transforming data (ACC-RC-1) | The two printed `Property` blocks wrapped in one `Schema` for `https://example.com/Project` | assembly |
| Linking related data (ACC-RC-2) | `references` written `https://example.com/Project#slug` with `cascade` `restrict`, not the recipe's bare type URL and `onDelete` | the Property reference page is authoritative (modeling spec contradiction C5); the recipe's vocabulary is a disputed claim |
| Group-based permissions (ACC-RC-3) | Printed Group, rule and changed-Group blocks as documents; a target `/blog/index.html` with a `#posts` list | assembly |
| Sending a webhook (ACC-RC-4) | Webhook URL pointed at a local sink; writes append **into a Note** element; `<meta itemprop="retry" content="3">` added for the retry half | endpoint configuration; the selector filter matches the request's key element, which for a POST append is the anchor (reacting R-REACT-14, contradiction C6: the recipe "would not fire for a plain list"); the retry meta is the recipe's own suggestion |
| Paginating a list (ACC-RC-5) | The printed contact list filled to 23 contacts; the two-paginator block filled with items | the recipe elides the items (`<!-- ... more contacts ... -->`) |
| Composite uniqueness (ACC-RC-6) | Schema as printed | none |
| State machine (ACC-RC-9) | The printed rules document is installed first **without** its exit rule, which is added later (the recipe's "delete surprise"); handler URL pointed at a local sink | scenario steps; endpoint configuration |
| Primitives (ACC-BJ-5 = ACC-RC-7) | `/about.html` = an `<h1>` plus the printed "complete example" and GET/PUT rules scoped to `h1` | §7.9 |
| Third-party widget (ACC-BJ-4 = ACC-RC-8) | Printed blocks assembled into `/widget.html`; the read/render snippet and the re-render listener put in **one** module script; a `GET` rule added; the placeholder `https://cdn.example.com/chart-library.min.js` answered by a stub that records renders; a second page `/widget-div.html` with the same records as `<div>`/`<span>` | modules do not share scope (as two scripts the listener throws `renderChart is not defined`); an SSE subscription is never granted by the default-GET mode (R-SSE-3), so a live page needs a read rule; the chart library is a placeholder; beta-js's `sse.mjs` parses events with `DOMParser`, which drops a `<td>` outside a table (see acceptance.md ACC-BJ-4) |

### A4 — pagelove-polls (`polls.spec.mjs`)

No changes. Every file under `site/` installed byte-for-byte.

### A5 — pagelove-kanban (`kanban.spec.mjs`)

| File | Change | Why |
|---|---|---|
| `/rules.html` (new) | The §6.3 acceptance rules document (open mode, actor `*`) | the repository ships no rules; the deployed host had rules that are not in the repository (§2 A5) |
| `/rules.html` on site `kanban-members` | Actor `users` instead of `*`, **plus one read rule for the app's own assets** (`/app.js`, `/style.css`, `/version.txt`, `/img/*`, `/fonts/*`) | members mode runs with default-GET `deny` (§6.3), under which the app could not load its own script and stylesheet; §6.3 lists only the board rules |

### A6 — pagelove-ats (`ats.spec.mjs`)

| File | Change | Why |
|---|---|---|
| `admin/auth.html` | ACC-AT-4b only: the per-email `AuthorizationRule` rows removed over WebDAV, then the `Group` item removed as well; the original is restored afterwards | §7.6 ACC-AT-4b prescribes exactly this |
| `outbox/index.html` | ACC-AT-8 only: the `url` meta changed from `https://api.postmarkapp.com/email` to a local sink; restored afterwards | §6.3 ("test-only edit") |

Not applied: the optional replacement of `https://huge-stomp-7042.onpagelove.com`
in `admin/index.html` (only the displayed/copied public URL; no scenario
reads it).

### A7 — pagelove-shop (`shop.spec.mjs`, `deploy-tooling.spec.mjs`)

Installed with the shop's own `ops/deploy-pagelove.sh` (the GitHub workflow's
invocation: `site/` as the project directory, manifests from `ops/`) and
`ops/deploy-admin-password.sh` with the password `test`.
`private/admin.example.html` is not installed (§6.3, R-APPS-26). One site file
is written differently, in the seed only (below).

| Item | Change | Why |
|---|---|---|
| Seed `data/settings/shop.html` | Deployed from a working copy in which `content=""` is written `content` (`shopSiteWithStoredFormSeed` in `e2e/lib/apps.mjs`); ACC-00 first runs the unmodified site and asserts the failure | PageLove stores HTML in its serialized form, where an empty attribute value is bare (LO-15, live 2026-09-29), so the script's `cmp` read-back of the seed it just PUT fails on PageLove too ("Seed read-back mismatch"). Every other shop file is already in that form and verifies byte for byte |
| Worker tests (ACC-UP-3) | Run in a scratch copy of the repository in which `site/js` is **also** placed at the repository root; the tests are unchanged | `worker/test/worker.test.js` imports `../../js/shop.mjs`, a path from before the app moved under `site/`; as committed at `d887054` it fails with `ERR_MODULE_NOT_FOUND` regardless of the server |
| Admin browser sessions (ACC-SH-5, -6, -9) | The browser context sends `Authorization: Basic …` with every request of the origin | the shop assumes a browser attaches the cached credential to all same-origin requests (R-APPS-14); Chromium only does so below the challenged directory (`/admin/`), so the shop's own writes to `/images/*` and `/data/*` went out without it and its trigger answered 401 (observed). ACC-SH-4 answers the real challenge instead |

### A8 — demo-apps (`demos.spec.mjs`)

Prepared with the repository's own `python scripts/prepare-deploy.py --host
demos.localhost --output <tmp>` and uploaded unchanged. The upstream
Playwright suite (§8.1) runs `tests/demo-pages.spec.js` unchanged; its
`scripts/serve-test.cjs` is replaced by a stand-in
(`e2e/upstream-adapters/demo-apps/serve-test.cjs`) whose `startStaticServer()`
reverse-proxies to the pagelike site, and the configuration adds the system
Chrome channel and one worker.

### A9/A10 — beta-js, pagelove-primitives (`betajs.spec.mjs`, `deploy-tooling.spec.mjs`)

beta-js is synced unchanged with its own `.github/scripts/sync-webdav.sh`
and checked with `verify-deploy.sh`, both run from a scratch copy of the
pinned checkout initialised as a fresh git repository (the scripts list
files with `git ls-files`). ACC-BJ-2 deletes `cors.html` over WebDAV on a
second site to show the check failing without it (scenario step).
`pagelove-primitives`' `test.html`, `test-sw.js` and `index.mjs` are
installed unchanged (ACC-UP-2). ACC-UP-1 adds pagelike's adapter test
(`e2e/upstream-adapters/beta-js/pagelike-sse.test.mjs`) next to beta-js's
tests in the scratch copy; beta-js's own tests and helpers are unchanged.
