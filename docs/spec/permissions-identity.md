# Permissions and identity (behavioral spec)

Area: `permissions-identity`. Packages: `internal/authz`, `internal/identity`
(see `docs/design.md`). Cases: `harness/cases/permissions-identity/`.

This document specifies how a pagelike server decides whether a request may
proceed (PageLove `AuthorizationRule` and `Group` items, the host default-GET
mode, the special cases for SSE, MOVE, QUERY and OPTIONS), what it answers when
it refuses (401 vs 403 and the error document), and how requests acquire an
identity (sessions, OIDC login, API keys, the WebDAV authoring plane). It is
written from third-party evidence (docs snapshot 2026-09-28, pinned upstream
code, a handful of recorded live read probes). Anything no source states is
marked `inferred` and says why.

Keywords MUST / SHOULD / MAY are normative for pagelike. "PageLove" means the
observed/documented upstream system; "pagelike" means this implementation.

## 0. Sources and abbreviations

Docs (Markdown snapshot `research/docs/2026-09-28/md/`). The combined page
`all_reference_permissions.md` is byte-identical, section for section, to the
individual pages `reference_permissions_AuthorizationRule.md` and
`reference_permissions_Group.md` (verified by diff); no auth-relevant sentence
in any `all_*` page is missing from an individual page.

| Tag | Page | Used for |
|---|---|---|
| D-AR | `reference/permissions/AuthorizationRule` | fields, templating, matching model, conflict resolution, examples |
| D-GRP | `reference/permissions/Group` | Group documents, subtypes, roles, built-in groups |
| D-RCP | `recipes/group-based-permissions` | Group recipe, membership change, `@read` resolver expansion |
| D-ACT | `reference/glossary/actor` | actor definition |
| D-GET, D-PUT, D-POST, D-DEL | `reference/reading-and-writing/*-method` | status tables, "absence and denial are distinct" |
| D-MOVE | `reference/reading-and-writing/MOVE-method` | §Authorization, §Denials, §Error cases |
| D-SSE | `reference/reading-and-writing/Server-Sent-Events` | §Subscribing (no default-GET) |
| D-REQ | `reference/reading-and-writing/Request-Document` | `auth` section shape |
| D-CN | `reference/reading-and-writing/Content-Negotiation` | JSON-LD all-matches reads |
| D-OPT | `reference/protocol/OPTIONS-method` | capability reporting |
| D-QRY | `reference/protocol/QUERY` | QUERY authorization |
| D-DAV | `reference/protocol/WebDAV` | authoring plane |
| D-SES | `languages/sessel/reference/syntax` §Context variables, §Authenticated identity in composition | `request.auth.*`, rule-only names |
| D-TR | `reference/composing-pages/Transient-Elements` §Sessions and access control | `pagelove_session` cookie |
| D-INC, D-STAMP, D-ROUTE, D-RB | `reference/composing-pages/Includes`, `Stamp`, `Parameterized-Routes`, `Resource-Binding` §Security | write-through authorization, unfiltered bindings |
| D-RC | `reference/composing-pages/Resource-Creation` | two-check templated creation |
| D-XML | `reference/composing-pages/XML-Documents` §Example | rule inside an XML document |
| D-SX | `reference/composing-pages/Selector-Extensions` §:isa() | rule subtypes mentioned |
| D-TRIG | `reference/reacting-to-changes/Trigger` | `ctx.request.auth`, trigger-thrown 401/403 |
| D-SM | `recipes/declaring-a-state-machine` (line 232) | WebDAV-edited rules bind within 60 s |
| D-L1 | `learn/your-first-app` | selector-scoped PUT/POST/DELETE rules on `/index.html` |
| D-L2 | `learn/build-a-blog` §Now the permission, §Locking the data folder | `/posts/*`, `/data/*` deny + `users` allow |
| D-ONB | `onboarding/*` | console `redirect_uri=…/auth/callback` |

Code (`research/upstream/`, commits from `research/COMMITS.txt`):

| Tag | File@commit |
|---|---|
| BJ-PRIM | `beta-js/pagelove/primitives.mjs@c204746` (OPTIONS capability discovery, document URL) |
| BJ-SSE | `beta-js/pagelove/sse.mjs@c204746:41` (`withCredentials: true`) |
| SKILL | `pagelove-dev/skills/pagelove-dev/SKILL.md@b489923` (official agent skill; `urn:Host`, `default-get-authz-mode`) |
| SKILL-R | `pagelove-dev/skills/pagelove-dev/references/cross-platform-command-recipes.md@b489923` |
| CURSOR | `pagelove-cursor/plugins/pagelove/skills/pagelove-dev/SKILL.md@b97c3ef` (older skill: Liquid in rules, `GroupMembership`) |
| POLLS | `pagelove-polls/site/admin/auth.html@c9270e5` |
| ATS | `pagelove-ats/site/admin/auth.html@8f200fc`, `site/README.html@8f200fc` |
| SHOP | `pagelove-shop/site/rules.html@d887054`, `site/admin-auth.html@d887054` |
| KANBAN | `pagelove-kanban/site/app.js@85109ab` |
| DEMO | `demo-apps@c4dd883` (`README.md`, `demo-0N-*/index.html`, `demo-04-accountability/feed.html`, `scripts/check-live.py`) |

Earlier notes (hypotheses, not facts): `understanding.md`; `read-probes.json`
(6 anonymous read probes against `https://docs.pagelove.com/` on 2026-09-28,
tag **LP**), `console.mjs` (console landing page markup, tag **LP-CON**), and
the ATS founder blog post text (tag **BLOG-ATS**, "Authorisation rules with
selectors not being enforced on writes", "A missing client ID breaking the login
redirect", "A 403 that told you nothing about how to log in").

Orchestrator brief (tag **BRIEF**): mentions a `__Host-session` cookie set on
anonymous GET, per-host login/logout/callback paths with defaults such as
`/auth/login`, and console OIDC fields (openid-configuration URL, client-id,
client-secret, login-path, logout-path, callback). None of these is confirmed by
a source available to this spec; they are treated as unverified observations.

## 1. Model in one page

A **rule** is any Microdata item of type `https://pagelove.org/AuthorizationRule`
found in any stored document on the host. It has one or more `actor`,
`resource` (path glob) and `method` values, an optional `selector`, and an
`action` (`Allow`/`Deny`, case-insensitive).

A request has an **actor**: either anonymous or a signed-in **principal** (OIDC
`sub`, email + `email_verified`, name, roles, raw claims). The principal's
**membership tokens** are its verified email, the names of `Group` items that
include it, its OIDC roles, and the built-ins `users`/`authenticated`.

For each authorization question (method, path, optional **key element**) the
server collects the **candidate rules**: rules whose method matches, whose
resource glob matches the path, whose actor matches the principal, and whose
selector is absent or matches the key element. Each candidate has a **tier**:
2 = exact user name (`sub`, also the deprecated `:username`), 1 = membership
token (email, group, role, `users`), 0 = `*`. Only the highest-tier candidates
count; among them any `Deny` wins, otherwise `Allow`. No candidates means deny,
except a plain `GET`/`HEAD` (not an SSE subscribe) when the host's default-GET
mode is `allow`.

Which element is the key element depends on the operation (first match for
writes and single reads; every match for multipart/JSON-LD reads, each decided
separately and all required; the anchor's parent for `POST placement=before|after`).
`MOVE` is three checks (MOVE on the document, DELETE on the source, POST on the
destination). A refusal is `401` for anonymous requests and `403` for
authenticated ones, with an HTML body of type `https://pagelove.org/1.0/Error`.

## 2. Rule discovery and extraction

### R-PERM-1 — Rules are Microdata items found anywhere on the host
- **Behavior:** Every element in any stored HTML document of the site that has
  `itemscope` and whose `itemtype` token list contains exactly
  `https://pagelove.org/AuthorizationRule` is one rule. Rules are host-wide: the
  document a rule lives in does not limit what it governs (its `resource`
  values do). A rule may sit in the document it governs, in a dedicated rules
  page (conventions seen: `/admin/auth.html`, `/rules.html`), or in an XML
  document (the XML example stores a rule inside an Atom feed). The `hidden`
  attribute, the element name (`div`, `tr`, `table`, `li`…), and whether the
  item is nested inside another item are irrelevant. Rules are extracted from
  **stored** markup: composition output (includes, templates, bindings),
  per-session transient content, and `<template>` contents do not contribute.
- **Evidence:** documented (location), demo-source (forms), inferred (stored-only,
  nesting). **Confidence:** high (anywhere on host), medium (stored-only, nested).
- **Source:** D-L1 ("put anywhere in the document, or even in another file");
  D-AR §Fields (table example); D-XML §Example; CURSOR:53 ("discovered
  wherever they exist across the site"); POLLS, ATS, SHOP, DEMO (all forms).
- **Edge cases:** documents at parameterized-route template paths
  (`/users/:id/x.html`) and under `/.pagelove/` are still scanned (inferred). A
  document that fails to parse contributes nothing. Non-HTML/XML blobs are not
  scanned. Other itemtypes that merely resemble a rule
  (`https://pagelove.org/AuthorisationRule`, `http://…`) are not rules.
- **Cross-area:** `internal/store` generation-keyed system-item cache (plan §Read path).
- **Cases:** `authz.discovery.rule-in-same-document`,
  `authz.discovery.rule-in-other-document`, `authz.discovery.rule-in-xml-document`,
  `authz.discovery.itemtype-must-match`.

> **Superseded by live observation (2026-09-28): XML documents.** A rule
> stored in an XML document is **not** discovered: an XML-hosted `Allow`
> granted nothing (401). PageLove treats XML like a blob, with no selectors
> and no rules (R-RW-30 as reconciled). pagelike's `ExtractRules` skips XML.
> This fails closed. Case: `authz.discovery.rule-in-xml-document`.

### R-PERM-2 — Field values follow Microdata value extraction, trimmed
- **Behavior:** Properties are collected per the HTML Microdata algorithm
  (properties of an item are its descendant `itemprop` elements not separated by
  another `itemscope`, plus `itemref`). Value: `<meta>` → `content`; `<a>`,
  `<link>`, `<area>` → `href`; `<data>`, `<meter>` → `value`; `<time>` →
  `datetime` or text; otherwise `textContent`. pagelike MUST trim ASCII
  whitespace from every rule field value. Each `itemprop` occurrence is one
  value; values are **not** split on commas or whitespace (`content="GET, PUT"`
  is a single, non-matching method token). The table form puts multi-valued
  fields inside a non-item cell, e.g. `<td><ul><li itemprop="method">POST</li>
  <li itemprop="method">PUT</li></ul></td>`; the `<meta>` form repeats the meta.
- **Evidence:** documented (forms), demo-source (multi-line cells, no comma
  splitting), inferred (trim). **Confidence:** high (forms), medium (trim,
  no-split).
- **Source:** D-AR §Example (table with nested `li`), D-RCP (meta form), POLLS:28-70
  (multiple `resource` in `li`), CURSOR:83-99 ("separate elements, NOT
  comma-separated"), CURSOR:107-126 (multi-line `td` values).
- **Cases:** `authz.discovery.table-form-nested-lists`,
  `authz.discovery.whitespace-trimmed`, `authz.discovery.comma-separated-methods-not-split`.

> **Superseded by live observation (2026-09-28/29) for the `action` field.**
> PageLove trims `actor`, `resource`, `method` and `selector`, but **not**
> `action`, so a padded action is unrecognized:
> - A padded `allow` grants nothing, and pagelike adopts that (it fails
>   closed).
> - A padded `deny` refuses nothing on PageLove. pagelike keeps honouring it
>   (keep-documented-security), because ignoring a refusal the author wrote
>   fails open. `authz.discovery.whitespace-trimmed.live` measures it.

### R-PERM-2a — A rule item on a `<table>` with several rows is one rule per row
- **Behavior:** The two docs examples put `itemscope itemtype=".../AuthorizationRule"`
  on the `<table>` element and write one rule per `<tr>` (rows carry no
  `itemscope`). Plain Microdata would merge every row into one item with many
  values, which makes those examples meaningless. pagelike MUST therefore split:
  when a rule item's element is a `<table>` (or `<thead>`/`<tbody>`/`<tfoot>`),
  every descendant `<tr>` that contains at least one of the item's properties
  becomes a separate rule built from the properties inside that row only;
  properties of the item outside any `<tr>` are ignored. Rows with no property
  (header rows) produce nothing. When the item is a `<tr>` (the form every demo
  app uses) or any other element, normal Microdata applies.
- **Evidence:** documented (examples), inferred (splitting algorithm).
  **Confidence:** medium.
- **Source:** D-AR §Example: blog admin interface; D-MOVE §Example: allowing
  editors to reorder cards; contrast POLLS/ATS/CURSOR (`<tr itemscope>` per rule).
- **Cases:** `authz.discovery.table-item-rows-are-separate-rules`.

> **Kept despite live observation (2026-09-28/29) (P-28 settled).** PageLove
> merges the rows into one rule, with the first row's selector and every
> row's methods. Row 2's DELETE was granted on row 1's `h1` (a probe got 204
> and the h1 was removed) and refused on the items row 2 names. That grants
> a method on elements the author never named, so pagelike keeps splitting
> rows (keep-documented-security).
> `authz.discovery.table-item-rows-are-separate-rules.live` measures the
> divergence.

### R-PERM-3 — Required fields; malformed rules are ignored
- **Behavior:** A rule needs ≥1 non-empty `actor`, ≥1 non-empty `resource`, ≥1
  non-empty `method`, and exactly one `action` whose trimmed value equals
  `allow` or `deny` case-insensitively (`Allow`, `allow`, `Deny`, `deny` are all
  in real use). A rule missing any of these, or with an unrecognized action, is
  ignored (neither grants nor denies). Empty individual values are dropped
  before the check. If several `action` values are present, pagelike SHOULD use
  `deny` if any is `deny` (fail closed; inferred).
- **Evidence:** documented ("At least one resource is required"), demo-source
  (case), inferred (ignore rule). **Confidence:** high (case), medium (ignore).
- **Source:** D-AR §resource; DOCS examples use `Allow`/`Deny`, POLLS/ATS/SHOP/L1/L2
  use lowercase; D-TRIG §Error handling (malformed trigger microdata is skipped,
  by analogy).
- **Cases:** `authz.discovery.action-case-insensitive`, `authz.discovery.malformed-rule-ignored`.

> **Refined by live observation (2026-09-28).** Case-insensitivity is
> confirmed (with document-level rules). The action is **not** trimmed (see
> R-PERM-2): a padded `allow` is unrecognized, and pagelike still honours a
> padded `deny`.

### R-PERM-4 — Empty selector means "whole resource"
- **Behavior:** A `selector` whose trimmed value is empty (including an empty
  `<td itemprop="selector"></td>`) is treated as absent: the rule is
  resource-level. If a rule has several non-empty `selector` values, pagelike
  SHOULD treat them as one selector list (joined with `, `) (inferred; no source
  shows it).
- **Evidence:** documented. **Confidence:** high.
- **Source:** D-AR §selector ("An empty or whitespace-only selector cell is fine");
  D-MOVE §A MOVE rule must not carry a selector; POLLS/ATS rows with empty cells.
- **Cases:** `authz.discovery.empty-selector-cell-means-document`.

### R-PERM-5 — MOVE rules with a selector are normalized at extraction
- **Behavior:** If any `method` value of a rule equals `MOVE` (case-insensitive)
  and the rule has a non-empty selector: an `Allow` rule is discarded **in full**
  (it grants nothing for any of its methods); a `Deny` rule is kept with its
  selector removed, for **all** its methods (it becomes resource-level). A
  method value of `*` does not count as "includes MOVE" for this purpose (a
  `*`+selector rule is valid; see R-PERM-23, R-PERM-47).
- **Evidence:** documented. **Confidence:** high (MOVE token), medium (`*` exemption,
  inferred from D-OPT reporting selector-scoped `*` grants without MOVE).
- **Source:** D-AR §selector (last paragraph); D-MOVE §A MOVE rule must not carry a selector.
- **Cases:** `authz.move.allow-with-selector-discarded`,
  `authz.move.allow-with-selector-discarded-in-full`, `authz.move.deny-with-selector-widened`,
  `authz.move.deny-with-selector-widens-other-methods`.

### R-PERM-6 — Rule and group changes take effect on the next request
- **Behavior:** A write through the public plane that adds, edits or removes a
  rule or `Group` item MUST affect the very next request. PageLove documents
  that rule documents edited over WebDAV bind "within 60 seconds"; pagelike MUST
  bind them immediately (its cache is keyed by the site write generation), which
  is a compatible tightening.
- **Evidence:** documented. **Confidence:** high (groups), medium (rules; the 60 s
  sentence is in the state-machine recipe).
- **Source:** D-RCP §Change membership ("picks up the change on the next request");
  D-SM line 232; D-TC ("binds as soon as the document holding it is written
  through the serving path").
- **Harness note:** live runs must allow up to 60 s after WebDAV setup before the
  first step (runner policy), or rewrite rule documents through the public plane.
- **Cases:** `authz.discovery.rule-change-effective-next-request`,
  `authz.group.membership-change-next-request`.

### R-PERM-7 — Vocabulary that is not (or no longer) a rule source
- **Behavior:** pagelike MUST NOT treat these as grants: `https://pagelove.org/GroupMembership`
  items (older vocabulary, see R-PERM-61 and §18 C7); Liquid `{{ … }}` in rule
  fields (literal text, R-PERM-36). `https://pagelove.org/PathAuthorizationRule`
  and `https://pagelove.org/TypeAuthorizationRule` appear only as example
  itemtypes in the `:isa()` docs; their semantics are undocumented. pagelike
  SHOULD treat an item whose type is a schema subtype of AuthorizationRule (via
  `parent`, per `:isa()`) as a rule with the same fields, and MUST ignore the two
  named types unless a schema declares them (inferred).
- **Evidence:** documented (Liquid removal), inferred (others). **Confidence:** low
  for subtypes.
- **Source:** D-AR §Templated values; D-SX §:isa(); CURSOR:78, ATS:66-96.

### R-PERM-8 — `@read` resolvers on `AuthorizationRule.actor` (cross-area)
- **Behavior:** If the site defines a schema for `https://pagelove.org/AuthorizationRule`
  with an `@read` resolver on `actor`, rule extraction MUST run the property read
  pipeline and use the resolved value(s); a resolver may expand one placeholder
  (e.g. `group:editors`) into several actor values. Without such a resolver a
  value like `group:editors` is an ordinary (group-name) token.
- **Evidence:** documented (recipe mention only). **Confidence:** low (mechanics
  undocumented).
- **Source:** D-GRP §Membership beyond verified email; D-RCP §Membership beyond verified email.
- **Cross-area:** modeling-data (Resolvers, schema cache).

## 3. Principals and actor matching

### R-PERM-9 — Principal model
- **Behavior:** A request is either **anonymous** (no signed-in principal; this
  includes requests that carry an anonymous session cookie, an API key, or any
  `Authorization` header on the public plane, see R-PERM-71) or
  **authenticated**, with a principal:
  `sub` (string, = "user name"/`username`), `email` (optional), `email_verified`
  (bool; true only if the claim is boolean `true` or the string `"true"`),
  `name`, `picture` (optional), `roles` (list of strings, may be empty), and
  `claims` (all identity claims as received). The `sub` is opaque (e.g.
  `sub_X3jX…`, or a numeric Zitadel id).
- **Evidence:** documented. **Confidence:** high.
- **Source:** D-AR §actor; D-SES §Authenticated identity in composition
  (`request.auth.username` = OIDC `sub`); D-REQ §Shape; KANBAN:155-158.

### R-PERM-10 — Membership tokens
- **Behavior:** An authenticated principal's membership tokens are the union of:
  its email **iff** `email_verified`; the `name` of every Group item that
  includes it (R-PERM-58..60); every entry of its OIDC `roles`; and the built-ins
  `users` and `authenticated`. Anonymous requests have none. Tokens are compared
  as exact, case-sensitive strings (inferred; "matched exactly" is documented for
  Group members).
- **Evidence:** documented. **Confidence:** high (sources), low (case-sensitivity).
- **Source:** D-AR §Groups and membership; D-GRP §Other sources of membership.

### R-PERM-11 — Actor token forms and specificity tiers
Each (post-substitution, trimmed) `actor` value is matched against the request.
The rule's tier is the highest tier among its matching values.

| Actor value | Matches | Tier |
|---|---|---|
| `*` | every request, anonymous included | 0 |
| `users`, `authenticated` | any authenticated principal | 1 |
| an email, e.g. `alice@example.com` | principal whose `email` equals it **and** `email_verified` | 1 |
| a group name, e.g. `editors` | principal holding that membership token (Group doc or OIDC role) | 1 |
| `role:<name>` | principal whose role list (R-PERM-74) contains `<name>` | 1 |
| a user name, e.g. `sub_abc` | principal whose `sub` equals it | 2 |
| `:username`, `${username}`, `${request.auth.username}` (deprecated as actor) | any authenticated principal (it substitutes the principal's own `sub`) | 2 |
| empty (e.g. substitution yielded `""`) | nothing | — |

- A value that matches in two ways takes the higher tier (e.g. a `sub` that
  happens to equal a group name matches at tier 2).
- **Evidence:** documented (all forms and the three-tier ordering); inferred
  (`:username` sharing tier 2 with exact user names, `role:` against the full role
  list). **Confidence:** high / medium / low respectively.
- **Source:** D-AR §actor, §`:username` as an actor is deprecated, §Conflict
  resolution; D-GRP §How a group actor is matched; D-RCP.
- **Cases:** `authz.actor.*`, `authz.conflict.*`.

### R-PERM-12 — `*` matches anonymous and signed-in requests
- **Evidence:** documented. **Confidence:** high. **Source:** D-AR §actor.

### R-PERM-13 — User name is the OIDC `sub`, never the display name
- **Behavior:** Only `sub` matches a tier-2 actor. `name`, `email` or
  `preferred_username` never match as a user name.
- **Evidence:** documented. **Confidence:** high. **Source:** D-AR §actor; D-SES.
- **Cases:** `authz.actor.user-name-is-sub`.

### R-PERM-14 — Email actors require a verified email
- **Behavior:** An email actor matches only when the principal's `email` equals
  it and `email_verified` is true; an unverified or absent email never matches.
  Tier 1, not tier 2.
- **Evidence:** documented. **Confidence:** high. **Source:** D-AR §actor.
- **Cases:** `authz.actor.verified-email`, `authz.conflict.email-same-tier-as-group`,
  `authz.conflict.user-name-beats-email`.

### R-PERM-15 — `users` / `authenticated`
- **Behavior:** Built-in groups, no document needed, match every authenticated
  principal regardless of roles or Group documents; never anonymous.
- **Evidence:** documented. **Confidence:** high. **Source:** D-AR §actor; D-GRP;
  D-L2 §Locking the data folder; DEMO README.md:45-66.
- **Cases:** `authz.actor.users-builtin`, `authz.actor.authenticated-alias`,
  `authz.conflict.users-allow-star-deny-anonymous`.

### R-PERM-16 — `role:` form
- **Behavior:** `role:X` matches a principal whose role list contains `X`
  (tier 1). A bare `X` also matches an OIDC role (D-GRP), so `role:` is an
  explicit spelling of the same membership test.
- **Evidence:** documented (existence only). **Confidence:** medium.
- **Source:** D-AR §Templated values ("Match roles with the `role:` actor form instead").
- **Cases:** `authz.actor.role-prefix-form`.

### R-PERM-17 — Deprecated `:username` actor
- **Behavior:** As an actor, `:username` (and `${username}`,
  `${request.auth.username}`) matches every authenticated principal at tier 2
  (above groups). Anonymous → empty → no match. Replacing it by `users` lowers the
  rule to tier 1, so an equal-tier deny then wins.
- **Evidence:** documented. **Confidence:** high (behavior), low (exactly tier 2
  rather than a separate tier between 1 and 2; see §20 P-10).
- **Source:** D-AR §`:username` as an actor is deprecated; SKILL:265-266.
- **Cases:** `authz.actor.username-token-deprecated`,
  `authz.actor.username-token-tier-above-users`, `authz.actor.username-token-vs-user-deny`.

### R-PERM-18 — Several actor values are OR-ed
- **Evidence:** documented. **Confidence:** high. **Source:** D-AR §actor.
- **Cases:** `authz.actor.multiple-values-or`.

## 4. Resource matching

### R-PERM-19 — Which path is matched
- **Behavior:** The glob is matched against the request path (percent-decoded,
  without query string or fragment). pagelike MUST treat a pattern as matching if
  it matches **either** (a) that request path or (b) the canonical document path,
  where a path ending in `/` canonicalizes to `<path>index.html`. For a
  parameterized-route request the path is the concrete URL (not the template).
  For include/stamp write-through it is the composed page's path, never the
  origin's (R-PERM-28).
- **Evidence:** documented (routes, includes), client-source + documented tutorial
  (index form), demo-source (directory form), live-observed (error `resource` is
  `/index.html` for a request to `/`). **Confidence:** medium.
- **Source:** D-L1 (rule on `/index.html`, page served at `/`) + BJ-PRIM:423-425
  (writes go to `location.href`); ATS:106-127 (rules duplicated for
  `/admin/index.html` and `/admin/`); LP `query-sessel` (resource `/index.html`);
  D-ROUTE §Resolution; D-INC §Interaction with HTTP Document Mutation.
- **Edge cases:** `/admin` (no slash) is a different path; directory redirect
  handling is in R-PERM-52. Query strings never participate.
- **Cases:** `authz.resource.directory-index-normalization`,
  `authz.resource.directory-form-rule`, `authz.resource.query-string-ignored`.

> **Superseded by live observation (2026-09-28) (P-1 settled, C15).**
> PageLove matches rules against the **canonical document path** only. A
> request to `dir/` is decided against `dir/index.html`, so an `Allow`
> written for `dir/` grants nothing there, which is why the ATS app
> duplicates its rules.
> - pagelike adopts this for `Allow` rules.
> - A `Deny` written in the directory form still refuses `dir/index.html`,
>   because pagelike fails closed.
> - Denials name the canonical path.
>
> Case: `authz.resource.directory-form-rule`.

### R-PERM-20 — Glob syntax
- **Behavior:** `resource` values are anchored globs over the whole path:

  | Syntax | Meaning |
  |---|---|
  | `*` | any sequence of characters, **including `/` and the empty string** |
  | `**` | same as `*` |
  | `?` | exactly one character |
  | `[abc]`, `[a-z]` | one character from the class; `[!…]` or `[^…]` negates |
  | `{a,b,c}` | alternation (not nested) |
  | `\c` | literal `c` |
  | anything else | literal, case-sensitive |

  Consequences: `/x/*` matches `/x/`, `/x/a`, `/x/a/b/c.html`, but not `/x`;
  `/*` matches every path; `/admin/*` matches `/admin/` (and its index).
- **Evidence:** documented (`*` crosses segments: `/*` "everyone can read
  everything", `/data/*` refusing `/data/posts/hello-world.html`, `/admin/*`
  matching a request to `/admin/`; `*`, `?`, `[`, `{` named as pattern
  characters), inferred (exact class/alternation semantics follow the common Rust
  `globset` defaults, whose metacharacters are exactly this set).
  **Confidence:** high for `*`; medium for `?`, `[…]`, `{…}`; low for `?`
  matching `/`.
- **Source:** D-AR §Example: blog admin interface, §A substituted value in a
  `resource` is a literal; D-L2 §Locking the data folder; D-ROUTE §Example (`/*`).
- **Cases:** `authz.resource.*`.

> **Superseded by live observation (2026-09-28): glob anchoring (P-2, in
> part).** A pattern ending in `/*` also matches the bare directory name:
> `/x/*` matches `/x` (a PUT to `/x` under a `/x/*` Allow got 201). It still
> does not match `/xy.html`. So a slash-less directory under a denied `dir/*`
> is a denial (R-PERM-52 as reconciled). Case:
> `authz.resource.trailing-slash-star-not-bare-name`.

### R-PERM-21 — Several resource values are OR-ed
- **Evidence:** demo-source + inferred from "one of the rule's resource patterns".
  **Confidence:** high. **Source:** D-AR §Matching model (2); POLLS:30; SHOP:65-66.

### R-PERM-22 — Invalid patterns
- **Behavior:** A pattern that does not compile (unbalanced `[` or `{`, nested
  `{`) is compared as literal text. A pattern containing `{{` is always compared
  as literal text (R-PERM-36). Either way it effectively matches only the
  identical path.
- **Evidence:** documented for `{{` ("compared as literal text"), inferred for the
  rest. **Confidence:** medium / low.

## 5. Method matching

### R-PERM-23 — Method tokens
- **Behavior:** Rule method values are compared case-insensitively after
  uppercasing (sources only use uppercase). `*` matches every method, including
  `MOVE`, `PATCH`, `QUERY` and extension methods. A request with method `HEAD`
  matches rules listing `HEAD` **or** `GET`; a rule listing only `HEAD` does not
  grant `GET` (inferred from HTTP semantics and "`GET`/`HEAD`" being treated as one
  class by the default-GET mode). `OPTIONS` in a rule's method list has no effect
  (R-PERM-63).
- **Evidence:** documented (`*`), inferred (HEAD, case). **Confidence:** high / medium.
- **Source:** D-AR §method; POLLS:31 and ATS list `HEAD`/`OPTIONS` explicitly
  (belt and braces).
- **Cases:** `authz.method.wildcard-grants-all`, `authz.method.head-follows-get`.

### R-PERM-24 — The authorization question asked per request type

| Request | Checked as method | Key element(s) | Default-GET fallback |
|---|---|---|---|
| `GET`/`HEAD`, no `Range` | GET | none (resource-level) | yes |
| `GET`/`HEAD` + `Range: selector=S`, `Accept` other than multipart/ld+json | GET | first match of S | yes |
| `GET` + `Range: selector=S` + `Accept: multipart/mixed` or `application/ld+json` | GET | every match of S | yes, per element |
| `GET` + `Accept: text/event-stream` (SSE subscribe) | GET | none | **no** |
| `QUERY` + `Content-Type: text/css-selector` (public plane) | GET | as a GET of that path | yes |
| `QUERY` + `Content-Type: text/sessel` (public plane) | QUERY | none | **no** |
| `OPTIONS` | not gated | — | — |
| `PUT` no `Range` (create/replace/upload) | PUT | none | no |
| `PUT` + `Range` | PUT | first match (the replaced element) | no |
| `POST` + `Range`, placement append/prepend (default append) | POST | first match (the anchor) | no |
| `POST` + `Range`, placement before/after | POST | **parent** of the first match | no |
| `POST` no `Range` to a template (templated creation) | POST on the template path, then PUT on the `<base href>` path | none | no |
| `DELETE` no `Range` | DELETE | none | no |
| `DELETE` + `Range` | DELETE | first match (the removed element) | no |
| `PATCH` (CRDT change-set, whole document) | PATCH | none | no |
| `MOVE` | see R-PERM-47/48 | | no |

- **Evidence:** documented except: HEAD (inferred), QUERY sessel (live-observed:
  anonymous 401 on a host that serves the same path publicly), PATCH (inferred
  from D-OPT listing PATCH among whole-document methods).
  **Confidence:** high for documented rows; medium for QUERY sessel; low for PATCH.
- **Source:** D-AR §Selectors that match several elements, §Conflict resolution;
  D-SSE §Subscribing; D-QRY §On the edge proxy; LP `query-sessel`; D-POST §Placement;
  D-MOVE §The three checks (POST keying); D-RC §Mode comparison.
- **Cross-area:** reading-writing (Range grammar, placement, first-match), protocol
  (QUERY, OPTIONS), sse.

## 6. Selector-scoped rules

### R-PERM-25 — A selector rule applies when the key element matches it
- **Behavior:** A rule with selector `S` is a candidate for key element `E` iff
  `E` is one of the elements returned by evaluating `S` against the **same
  document** in which the request selector was resolved (i.e. `E ∈
  querySelectorAll(S)` over the whole document, so `S` may use ancestors, e.g.
  `#todo-list li`). An element merely *inside* an element matching `S` does not
  match. Selector lists (`li, input`; `tbody, tr[itemtype]`) and PageLove selector
  extensions are allowed.
- **Evidence:** documented ("the rule applies only to elements satisfying the
  selector"; "selector-level rules are first narrowed to those whose selector
  matches the targeted element"), demo-source (rules list `li, input` so that the
  checkbox inside an `li` is writable; a rule targets `[id^='rsvp-']
  meta[itemprop='rsvpStatus']` exactly), client-source (BJ-PRIM:198-221 attaches
  capabilities to elements matching the reported rule selector).
  **Confidence:** medium-high.
- **Source:** D-AR §selector, §Conflict resolution; CURSOR:317-330, 359; DEMO
  demo-01-event/index.html:170-175; BJ-PRIM:180-221.
- **Cases:** `authz.selector.target-must-match-rule-selector`,
  `authz.selector.rule-selector-evaluated-in-document-context`,
  `authz.discovery.table-form-nested-lists` (selector list).

### R-PERM-26 — Whole-document requests never match selector rules
- **Behavior:** A request without a `Range` selector has no key element, so only
  resource-level rules can be candidates. A selector-scoped `PUT` grant does not
  permit a whole-document `PUT`; a selector-scoped `GET` deny does not affect a
  whole-document `GET` (see §20 P-6).
- **Evidence:** documented (matching model condition 4), demo-source (polls needed
  a document-level `PUT` for templated creation). **Confidence:** high (writes),
  medium (whole-document reads).
- **Source:** D-AR §Matching model; POLLS:97.
- **Cases:** `authz.selector.selector-rule-does-not-cover-whole-document`.

### R-PERM-27 — Resource-level rules also govern selector requests
- **Behavior:** A rule without selector is a candidate for every key element in
  a matching resource. So `* PUT Allow /polls/*` lets anyone replace any element
  of a poll, and a resource-level `Deny` refuses element-level requests at its
  tier (a widened `Deny` "refuses both for the whole document").
- **Evidence:** demo-source + documented. **Confidence:** high.
- **Source:** POLLS:97; D-MOVE §A MOVE rule must not carry a selector.
- **Cases:** `authz.selector.resource-level-rule-covers-selector-writes`,
  `authz.selector.resource-deny-overrides-selector-allow-same-tier`,
  `authz.selector.selector-deny-overrides-resource-allow-same-tier`.

> **Kept despite live observation (2026-09-28).** On PageLove a
> selector-scoped `Deny` does **not** override a resource-level `Allow` at the
> same tier. The write went through, and denied elements are served. So
> "denying a fragment is enough" protects nothing there. pagelike keeps the
> single candidate set of R-PERM-38 (keep-documented-security). The `.live`
> siblings of `authz.selector.selector-deny-overrides-resource-allow-same-tier`,
> `authz.multimatch.*` and `rw.authz.denied-fragment-read` measure it. The
> reverse direction (a resource-level Deny over a selector Allow) holds on
> PageLove.

### R-PERM-28 — Composed pages, write-through, routes, transient elements
- **Behavior:**
  - Reads and `QUERY` (css) resolve selectors against the **composed** page; rule
    selectors are evaluated against that same composed DOM.
  - A write to an element projected by `<p:include>`/`<p:stamp>` is authorized
    against the composed page path and the element as it appears there; the
    origin resource needs no rule. The origin's own rules are not consulted.
  - A selector write to a parameterized-route URL is authorized against the
    concrete URL.
  - Transient elements (`p:transient`) use ordinary rules; the key element is the
    element the request selector matched (inferred: not the transient ancestor).
- **Evidence:** documented. **Confidence:** high (includes, routes), medium (transient key).
- **Source:** D-INC §Interaction with HTTP Document Mutation; D-STAMP; D-ROUTE
  §Resolution; D-TR §Sessions and access control; SHOP rules.html:15-24.
- **Cross-area:** composing-pages.
- **Cases:** `authz.selector.include-write-authorized-on-composed-page`,
  `authz.selector.transient-element-rule`.

### R-PERM-29 — Unparseable rule selectors
- **Behavior:** An `Allow` rule whose selector fails to parse is ignored. A
  `Deny` rule whose selector fails to parse is kept as resource-level (widened),
  following the documented principle that a refusal that cannot be honoured
  exactly is widened, never dropped.
- **Evidence:** inferred (principle documented for MOVE). **Confidence:** low.
- **Source:** D-MOVE §A MOVE rule must not carry a selector.

## 7. Templated values

### R-PERM-30 — `${…}` lookups in `actor`, `resource`, `selector`
- **Behavior:** In these three fields (not `method`, not `action`), every
  occurrence of `${path}` where `path` is a dot-separated lookup (segments of
  letters, digits, `_`, `-`) is replaced per request. `:username` anywhere in these
  fields is shorthand for `${request.auth.username}`. An unterminated `${` is
  literal. Substitution happens before matching.
- **Evidence:** documented. **Confidence:** high (syntax), low (segment charset,
  `:username` outside `actor`).
- **Source:** D-AR §Templated values.

### R-PERM-31 — Lookup context
- **Behavior:** Roots and paths pagelike MUST support:

  | Lookup | Value |
  |---|---|
  | `request.method` | request method, uppercase |
  | `request.path` | request path (decoded, no query) |
  | `request.headers.<name>` | header value, name matched case-insensitively (write it lowercase); repeated headers joined with `, ` |
  | `request.query_string` | raw query string without `?` (only when present) |
  | `request.query.<k>` | first value of query parameter `k` |
  | `request.auth.sub`, `request.auth.username` | principal `sub` (authenticated only) |
  | `request.auth.email`, `request.auth.name` | principal email / name (authenticated only) |
  | `username` | alias of `request.auth.username` |

  pagelike SHOULD additionally accept the Sessel-doc spellings `auth.claims.<c>`,
  `request.auth.claims.<c>`, `method`, `path`, `query.<k>` (see §18 C10).
- **Evidence:** documented. **Confidence:** high (table), low (aliases).
- **Source:** D-AR §Templated values (lookup table); D-SES §Context variables.

### R-PERM-32 — Rendering rules
- **Behavior:** Only strings, numbers and booleans render (booleans as `true`/
  `false`); a path that resolves to a list or object (e.g. `${request.auth.roles}`,
  `${request.headers}`) renders as the empty string; a path that resolves to
  nothing renders as the empty string. Anonymous requests have no `request.auth`,
  so every `request.auth.*` lookup renders empty.
- **Evidence:** documented. **Confidence:** high.
- **Source:** D-AR §Templated values.
- **Cases:** `authz.template.missing-lookup-empty-string`,
  `authz.template.object-lookup-empty`, `authz.template.anonymous-auth-lookup-empty`,
  `authz.template.roles-list-lookup-empty`.

### R-PERM-33 — Substituted values are literal inside `resource`
- **Behavior:** Glob metacharacters (`*`, `?`, `[`, `]`, `{`, `}`, `\`) coming from
  a substituted value MUST be escaped before the pattern is compiled; the
  author's own metacharacters keep their meaning (`/users/${request.auth.username}/*`
  keeps its trailing `*`). A `/` in a substituted value is **not** neutralized: it
  spans segments (documented open issue; keep current behavior).
- **Evidence:** documented. **Confidence:** high (escaping), medium (slash; may change).
- **Source:** D-AR §A substituted value in a `resource` is a literal.
- **Cases:** `authz.template.header-value-is-literal-in-resource`,
  `authz.template.substituted-slash-spans-segments`, `authz.template.username-literal-glob`.

### R-PERM-34 — Substituted values inside `actor`
- **Behavior:** The substituted text becomes the actor value and is matched per
  R-PERM-11 **as a literal token**: a substituted `*`, `users`, or `role:x` coming
  from request data MUST NOT be interpreted as the wildcard or a special form
  (inferred safety rule; PageLove behavior unknown, §20 P-13). Exception: the whole
  value being exactly `:username`/`${username}`/`${request.auth.username}` is the
  deprecated form of R-PERM-17.
- **Evidence:** inferred. **Confidence:** low.

### R-PERM-35 — Substituted values inside `selector`
- **Behavior:** Plain textual substitution into the selector source, then parse
  (the docs say the value is copied in). Authors quote attribute values
  themselves (`[data-owner="${request.auth.email}"]`). pagelike MUST NOT add
  escaping (compatibility), and SHOULD log a warning when a substituted value
  contains `"`, `'`, `]` or `,` (inferred; §20 P-14).
- **Evidence:** documented (substitution), inferred (no escaping). **Confidence:** medium / low.
- **Cases:** `authz.template.request-method-in-selector`,
  `authz.template.header-lookup-in-selector`, `authz.template.auth-email-in-selector`.

### R-PERM-36 — Liquid is not evaluated; `{{ … }}` is literal text
- **Behavior:** Rule fields are never rendered as Liquid. A field containing `{{`
  is compared literally, so a legacy `allow` stops granting and a legacy `deny`
  stops denying. No warning is required (PageLove gives none); pagelike SHOULD
  log one at extraction time.
- **Evidence:** documented. **Confidence:** high.
- **Source:** D-AR §Templated values; SKILL:272, 319; contrast CURSOR:101-126 (old behavior).
- **Cases:** `authz.template.liquid-braces-literal-allow-stops-granting`,
  `authz.template.liquid-braces-literal-deny-stops-denying`.

### R-PERM-37 — Rule evaluation is free of request budget
- **Behavior:** Evaluating rules and `${…}` lookups MUST NOT consume the request's
  Sessel/JS budget and can never cause a `503`. (Evaluating a Group subtype's
  `includes()` is Sessel/JS and MAY consume budget; unspecified upstream.)
- **Evidence:** documented. **Confidence:** high. **Source:** D-AR §Templated values (last paragraph).

## 8. Decision procedure

### R-PERM-38 — Candidate set and decision (normative pseudo-code)

```
decide(method, path, E):              # E = key element or none
  C = [ r in rules
        | methodMatches(r, method)
        and resourceMatches(r, path)          # R-PERM-19/20, after substitution
        and tier(r, principal) != none        # R-PERM-11, after substitution
        and (r.selector is empty or (E != none and E ∈ select(r.selector, doc(E)))) ]
  if C is empty: return NOMATCH
  t   = max(tier(r) for r in C)
  top = [ r in C | tier(r) == t ]
  return DENY if any(r.action == deny for r in top) else ALLOW

finalize(result, method, isSSE):
  if result == NOMATCH:
     return ALLOW if method in {GET, HEAD} and not isSSE and site.default_get == allow
     return DENY
  return result
```

- **Evidence:** documented (matching model, the two conflict-resolution steps,
  default deny, default-GET exception). **Confidence:** high for the shape; medium
  for merging resource-level and selector-level candidates into one set (the doc
  says "the same resolution applies at both granularities").
- **Source:** D-AR §Matching model, §Conflict resolution.

### R-PERM-39 — Order independence
- **Behavior:** The result MUST NOT depend on the order of rules within a
  document, across documents, or across sources.
- **Evidence:** documented. **Confidence:** high.
- **Cases:** `authz.conflict.order-independent`.

### R-PERM-40 — Deny wins only at the top tier
- **Behavior:** A tier-2 `Allow alice` beats tier-0 `Deny *`; tier-1 `Allow
  editors` beats `Deny *`; `Allow editors` + `Deny suspended` for a member of both
  is denied; a tier-2 allow beats a tier-1 deny.
- **Evidence:** documented. **Confidence:** high.
- **Source:** D-AR §Conflict resolution; D-GRP.
- **Cases:** `authz.conflict.*`.

### R-PERM-41 — Reads that return every match are all-or-nothing
- **Behavior:** For an all-matches read (R-PERM-24), `decide` runs once per
  matched element (each with its own selector-level candidates) and then
  `finalize` per element. The request is allowed only if every element is
  allowed; one denied element (or one uncovered element while default-GET is
  `deny`) refuses the whole request with 401/403 — never a partial or filtered
  body. A single-result read (any other `Accept`) is decided only on the first
  match.
- **Evidence:** documented. **Confidence:** high.
- **Source:** D-AR §Selectors that match several elements; D-CN.
- **Cases:** `authz.multimatch.*`.

> **Kept despite live observation (2026-09-28).** Live PageLove serves every
> match, and a denied first match, because its selector-scoped Deny does not
> override the resource-level Allow (see R-PERM-27). pagelike keeps the
> documented all-or-nothing rule (keep-documented-security). The two
> `authz.multimatch.*.live` siblings measure it.

### R-PERM-42 — Writes are decided on the first match only
- **Evidence:** documented. **Confidence:** high. **Source:** D-AR §Selectors that match several elements.
- **Cases:** `authz.selector.write-first-match-only`.

### R-PERM-43 — Paginated all-matches reads are decided over the whole match set
- **Behavior:** When a read asks for a page (range) of a selector's matches, the
  all-matches decision is made over **every** element the selector matches, not
  the page; a denied element anywhere refuses every page of that selector.
- **Evidence:** documented. **Confidence:** high (rule), low (request syntax; owned
  by reading-writing, see §20 P-7).
- **Source:** D-AR §Selectors that match several elements (last paragraph).

## 9. Default-GET mode and the special methods

### R-PERM-44 — Host default-GET mode
- **Behavior:** Each site has a setting `default-get-authz-mode ∈ {allow, deny}`
  (pagelike config key `default_get`). It only resolves NOMATCH results for
  `GET`/`HEAD` (whole document, single selector read, and per element in
  all-matches reads) and for css-selector `QUERY` (authorized as a GET). `allow`
  = unmatched reads succeed; `deny` = unmatched reads are refused. It never
  overrides a matching `Deny` or `Allow`. New pagelike sites default to `allow`
  (the sample host block shows `allow`; ATS instructs switching to `deny` to lock
  down; DEMO's live check expects rule-less assets to return 200).
- **Evidence:** documented + client-source. **Confidence:** high (semantics),
  medium (default value).
- **Source:** D-AR §Conflict resolution; SKILL:93-114, 149-151, 239; SKILL-R:560-590;
  ATS:31-36, README.html ("enabling default-get-authz-mode: deny"); DEMO
  scripts/check-live.py:12-21.
- **Cases:** `authz.default-get.*`.

### R-PERM-45 — SSE subscriptions are never default-granted
- **Behavior:** `GET` with `Accept: text/event-stream` is authorized like a GET
  but NOMATCH always denies, whatever the mode. pagelike treats a request as a
  subscribe when the `Accept` header lists `text/event-stream` (cross-area: sse).
  Authorization happens at subscribe time; denial is an ordinary 401/403 error
  response, not a stream.
- **Evidence:** documented. **Confidence:** high.
- **Source:** D-AR §Conflict resolution; D-SSE §Subscribing.
- **Cases:** `authz.default-get.sse-never-default-granted`, `authz.default-get.sse-granted-by-rule`.

### R-PERM-46 — Writes, MOVE, PATCH and Sessel QUERY are never default-granted
- **Evidence:** documented (writes, MOVE), live-observed (Sessel QUERY).
  **Confidence:** high / medium.
- **Source:** D-AR §Testable examples (Default deny); D-MOVE §Authorization; LP.
- **Cases:** `authz.method.default-deny-put`, `authz.move.no-rule-denied`,
  `authz.method.query-sessel-not-default-granted`.

## 10. MOVE

### R-PERM-47 — Element MOVE needs three independent grants
- **Behavior:** For `MOVE` with `Range: selector=SRC` and `Destination-Range:
  selector=DST; placement=P` (Destination must equal the request path), all of:
  1. `finalize(decide(MOVE, path, none))` = ALLOW (resource-level only; after
     R-PERM-5 normalization);
  2. `finalize(decide(DELETE, path, first match of SRC))` = ALLOW;
  3. `finalize(decide(POST, path, K))` = ALLOW, where `K` = first match of DST for
     `append`/`prepend`, or its parent for `before`/`after`.
  Any failure refuses the request (401/403). Default-GET never applies. A single
  container-level `POST .lane` rule authorizes every placement into or within a
  lane.
- **Evidence:** documented. **Confidence:** high.
- **Source:** D-MOVE §The three checks, §Example: allowing editors to reorder cards.
- **Cases:** `authz.move.*`.

### R-PERM-48 — Whole-document MOVE needs MOVE on source and destination
- **Behavior:** Without `Range` and `Destination-Range`, require
  `finalize(decide(MOVE, source, none))` and `finalize(decide(MOVE, Destination,
  none))` both ALLOW; a `Deny` matching either path refuses. Creating the
  destination (no document there) is still only a MOVE check (no PUT check;
  inferred).
- **Evidence:** documented. **Confidence:** high.
- **Source:** D-MOVE §Whole-document MOVE.
- **Cases:** `authz.move.whole-document-needs-both-paths`, `authz.move.whole-document-deny-at-destination`.

### R-PERM-49 — MOVE absence vs denial ordering
- **Behavior:** Structural validation first (422 for incomplete headers, 501 for
  cross-document element moves — both independent of identity; inferred order).
  Then check 1. Then, if SRC matches nothing: `416` if the actor may GET the
  document (resource-level read decision), else 401/403; if DST matches nothing:
  `404` under the same condition, else 401/403. Then checks 2 and 3.
- **Evidence:** documented (statuses), inferred (ordering reconciling "fails
  closed" with the 416/404 table; §18 C11). **Confidence:** medium.
- **Source:** D-MOVE §Denials, §Error cases.
- **Cases:** `authz.move.source-absent-readable-416`, `authz.move.source-absent-unreadable-denied`,
  `authz.move.dest-absent-readable-404`.

> **Superseded by live observation (2026-09-28), in part.** An absent source
> (or anchor) is reported as `416` (or `404`) only when two conditions hold:
> - the actor may read the document, **and**
> - DELETE (or POST) is granted to it at document level.
>
> When those methods are granted only by selector-scoped rules, the element
> check cannot run on an absent element, so the request is refused
> (`401`/`403`), even on a readable page. PageLove answers `416` to an actor
> who cannot read the page. pagelike keeps the denial there
> (keep-documented-security,
> `protocol.move-authz.fail-closed-when-unreadable.live`). Case:
> `authz.move.source-absent-readable-416`.

## 11. Absence vs denial and ordering

### R-PERM-50 — Reads
- **Behavior:** For `GET`/`HEAD` (and css `QUERY`):
  1. Let `R0 = finalize(decide(GET, path, none))` (resource-level read).
  2. Document missing (and no parameterized route resolves): `R0` DENY → 401/403,
     else `404`.
  3. Selector given but matches nothing: `R0` DENY → 401/403, else `416`.
  4. Otherwise decide on the key element(s) per R-PERM-24/41.
  A reader therefore never learns that a document or element exists or not
  unless it could read it.
- **Evidence:** documented (distinct 404/416 vs 401/403; the "Path restrictions"
  example answers 401 for an unreadable path), inferred (the exact ordering).
  **Confidence:** medium.
- **Source:** D-GET §Error cases; D-AR §Testable examples (Path restrictions).
- **Cases:** `authz.absent.get-nonexistent-doc-denied-401`,
  `authz.absent.get-nonexistent-doc-allowed-404`,
  `authz.absent.get-selector-nomatch-readable-416`,
  `authz.absent.get-selector-nomatch-unreadable-401`.

### R-PERM-51 — Selector writes (PUT, POST, DELETE)
- **Behavior:**
  1. Document missing: `R0` DENY → 401/403, else `404`.
  2. Selector matches nothing:
     - `POST` with `placement=before|after` → refuse (401/403) regardless of read
       access (documented current behavior).
     - otherwise `R0` DENY → 401/403, else `416`.
  3. Target exists: `finalize(decide(method, path, key element))`; DENY (including
     NOMATCH) → 401/403. Read permission is **not** required when the write is
     allowed (public write windows on unreadable pages work).
  Whole-document writes skip steps 1–2 (a missing document is created by `PUT`,
  and `DELETE` of a missing document is `404` only when `R0` is ALLOW, else
  401/403; inferred).
- **Evidence:** documented + demo-source. **Confidence:** high (1–3 statuses),
  medium (whole-document delete ordering).
- **Source:** D-PUT/D-POST/D-DEL "Absence and denial are distinct"; ATS:324-333;
  DEMO README.md:45-66 and feed.html:79-96.
- **Cases:** `authz.absent.*`.

> **Superseded by live observation (2026-09-28), in part.**
> - Step 2, first bullet: POST `before`/`after` with an absent anchor is
>   treated like any other absent target: `416` when readable, a denial
>   otherwise.
> - When the method is granted only by selector-scoped rules, that `416` is
>   the authorization layer's (read vocabulary with `charset=utf-8`,
>   `Content-Range: selector */`, no `Vary`).
> - A whole-document DELETE of a missing path is `204` when DELETE is granted
>   (R-RW-81/82 as reconciled).
>
> Case: `authz.absent.post-before-absent-anchor-refuses`.

### R-PERM-52 — Directory redirects respect read permission
- **Behavior:** A slash-less request for a directory (`/blog`) is redirected
  (`301`, `Location: /blog/` plus the original query) only if the actor may read
  `/blog/index.html`; otherwise it is a plain `404` (not 401). A path whose last
  segment contains `.` is never redirected.
- **Evidence:** documented. **Confidence:** high (statement), medium (404 rather
  than 401 when the index exists but is unreadable).
- **Source:** D-GET §Directory requests.
- **Cross-area:** reading-writing.
- **Cases:** `authz.resource.slashless-directory-unreadable-404`,
  `authz.resource.slashless-directory-readable-redirect`.

> **Superseded by live observation (2026-09-28) (P-22 settled).** Because
> `dir/*` governs `dir` (R-PERM-20 as reconciled), a slash-less request under
> a denied directory is the ordinary denial: `401`/`403`, with `resource` set
> to the slash-less path, not `404`. The readable redirect is `301` with no
> body and `Cache-Control: private, max-age=3600`.

### R-PERM-53 — Position in the pipeline
- **Behavior:** pagelike order for public-plane requests: (1) framing/size checks
  (`413` before reading the body); (2) routing to built-in endpoints (login,
  logout, callback) which are not rule-gated; (3) reserved namespace: writes
  under `/.pagelove/` → `403` regardless of rules (cross-area); (4)
  authorization per §8–§11; (5) triggers, validation (schemas, shapes,
  transitions → `422`), write, processors; (6) re-check authorization inside the
  write transaction against the committed document version (plan §Write path).
  Trigger-thrown `HTTPResponse` statuses (e.g. `401` with `WWW-Authenticate`,
  `403`) are passed through unchanged and are not this area's error documents.
  Relative order of triggers and authorization is unspecified upstream (§20 P-18).
- **Evidence:** documented (413, reserved namespace, triggers exist), inferred
  (order). **Confidence:** low for the order.
- **Source:** D-PUT §Request body size limit, §Reserved namespace; D-TRIG; SHOP admin-auth.html:5-35.

## 12. Denial responses

### R-PERM-54 — 401 for anonymous, 403 for authenticated
- **Behavior:** Every authorization refusal from this area (including NOMATCH
  default-deny, MOVE checks, SSE, write windows) is `401 Unauthorized` when the
  request has no authenticated principal and `403 Forbidden` when it has one.
  The per-method tables that list only `403` are describing the authenticated
  case.
- **Evidence:** documented. **Confidence:** high.
- **Source:** D-MOVE §Denials; D-AR §Testable examples (401 for anonymous
  requests); D-PUT/D-POST/D-DEL notes ("gets `401`/`403`"); DEMO check-live.py:58-61.
- **Cases:** `authz.actor.401-vs-403`, `authz.error.403-for-authenticated`.

### R-PERM-55 — The 401 document
- **Behavior:** Status `401`, `Content-Type: text/html; charset=utf-8`, no
  `WWW-Authenticate` header, body exactly this template (whitespace as shown;
  `{resource}` is the canonical document path of the request, e.g. `/index.html`
  for `/`, HTML-escaped; the login paragraph is present only when the host has an
  identity provider configured, `{login}` = the host's login path):

  ```
  <!DOCTYPE html>
  <html>
  <head>
      <meta charset="UTF-8">
      <title>401 Unauthorized</title>
  </head>
  <body itemscope itemtype="https://pagelove.org/1.0/Error">
      <h1>401 Unauthorized</h1>
      <p itemprop="message">Authentication required to access this resource.</p>
      <dl>
          <dt>Resource</dt>
          <dd itemprop="resource">{resource}</dd>
      </dl>
      <p><a href="{login}">Log in</a></p>
  </body>
  </html>
  ```
  Microdata: one item `https://pagelove.org/1.0/Error` with `message` and
  `resource`. `HEAD` gets the same status and headers with no body.
- **Evidence:** documented (body with login link), live-observed (same body
  without the link, `text/html; charset=utf-8`, no `WWW-Authenticate`, resource
  canonicalized). **Confidence:** high (shape), medium (link condition, headers).
- **Source:** D-AR §Testable examples; LP `query-sessel`; BLOG-ATS ("a 403 that told
  you nothing about how to log in").
- **Cases:** `authz.error.401-document-shape`, `authz.error.401-resource-is-canonical-path`,
  `authz.error.401-login-link-when-oidc`.

### R-PERM-56 — The 403 document
- **Behavior:** Not documented upstream. pagelike MUST use the same shape with
  `<title>403 Forbidden</title>`, `<h1>403 Forbidden</h1>`, a `message` property
  (recommended text: `You do not have permission to access this resource.`) and
  the `resource` property; it SHOULD include `<p><a href="{logout}">Log out</a></p>`
  only when an identity provider is configured. Cases assert only status,
  itemtype and `resource`.
- **Evidence:** inferred. **Confidence:** low.

### R-PERM-57 — Denials are not cacheable and not streams
- **Behavior:** Denials carry no `ETag`; pagelike SHOULD send `Cache-Control:
  no-store`. An SSE denial is an ordinary HTML error response.
- **Evidence:** inferred (LP shows no ETag/Cache-Control on the 401). **Confidence:** low.
- **Cases:** `authz.error.sse-denied-is-plain-error`, `authz.error.head-denied-no-body`.

> **Superseded by live observation (2026-09-28).** Denials carry only
> `Content-Type: text/html; charset=utf-8`: no `ETag`, no `Cache-Control`
> (pagelike dropped its `no-store`) and no `Vary`. All 119 denial responses
> in the run had that shape. A 401 is not cacheable by default (RFC 9111
> §3), so nothing is lost.

## 13. Groups

### R-PERM-58 — Group documents
- **Behavior:** Any item of type `https://pagelove.org/Group` stored anywhere on
  the host (conventionally `/groups.html` or the rules page) defines a group:
  `name` (1..1; first non-empty value used; an item with a blank name is ignored)
  and `member` (0..n email addresses; duplicates harmless). Several Group items
  with the same name contribute the union of their memberships (inferred). The
  base membership test `includes(email)` is exact string equality against the
  principal's **verified** email.
- **Evidence:** documented. **Confidence:** high (fields), medium (union, first name).
- **Source:** D-GRP §The `Group` document; D-RCP; ATS:56-64 (duplicate members).
- **Cases:** `authz.group.document-grants-members`, `authz.group.stored-anywhere`,
  `authz.group.multiple-documents-same-name-union`.

### R-PERM-59 — Verified email gate
- **Behavior:** Group membership (base or subtype) is only evaluated for
  principals with `email_verified = true`; `includes()` is never called for an
  unverified or absent email.
- **Evidence:** documented. **Confidence:** high. **Source:** D-GRP §Verified email only.
- **Cases:** `authz.group.unverified-email-not-member`.

### R-PERM-60 — Group subtypes and `includes()`
- **Behavior:** An item whose type is a schema whose `parent` chain reaches
  `https://pagelove.org/Group` is a group. If the most-derived schema defines a
  `Method` named `includes` (Sessel or JS, one `email` parameter, returns
  Boolean), membership = that method called with `self` = the group item and
  `email` = the verified email; otherwise the base exact-member test applies.
  Errors or budget exhaustion inside `includes()` → not a member (fail closed;
  inferred). The docs place the schema under `/system/schemas/`.
- **Evidence:** documented. **Confidence:** medium (execution details).
- **Source:** D-GRP §Custom membership.
- **Cross-area:** modeling-data (Schema `parent`, Methods), languages (Sessel/JS).
- **Cases:** `authz.group.subtype-includes-override`,
  `authz.group.subtype-without-override-inherits-member-match`.

### R-PERM-61 — OIDC roles claim; legacy GroupMembership
- **Behavior:** Every string in the principal's `roles` claim (asserted by the
  identity provider, fixed for the session) is a membership token usable as a
  bare actor. pagelike reads claim `roles` by default (array of strings; a single
  string is split on whitespace), configurable per site for providers that use
  another claim (pagelike extension). `https://pagelove.org/GroupMembership`
  items are ignored (§18 C7).
- **Evidence:** documented (roles), inferred (claim parsing, GroupMembership).
  **Confidence:** high / low.
- **Source:** D-GRP §Other sources of membership; KANBAN:141-145.
- **Cases:** `authz.group.oidc-roles-claim`, `authz.group.groupmembership-legacy-ignored`,
  `authz.group.groupmembership-legacy-honored` (disputed).

### R-PERM-62 — Membership is recomputed per request
- **Evidence:** documented. **Confidence:** high. **Source:** D-RCP §Change membership.

## 14. Capability reporting (interface to OPTIONS)

### R-PERM-63 — OPTIONS is answered from rules for the requesting actor
- **Behavior:** `OPTIONS` is never refused by this area. `internal/authz` MUST
  expose, for (actor, path), the document-level allowed method set and, for
  every selector-scoped rule whose actor matches, that rule's (substituted)
  selector with the method set allowed for elements matching it. Allowed sets
  are computed with the same `decide`/`finalize` logic (default-GET included for
  GET/HEAD: LP shows `Allow: HEAD, GET, OPTIONS` for an anonymous OPTIONS on
  the publicly readable docs host). A `*` grant expands to `GET, HEAD, PUT, DELETE, POST, MOVE, PATCH`
  (document) or `GET, HEAD, PUT, DELETE, POST` (selector). OPTIONS never checks
  document or element existence. Wire format, 204 vs 207 and headers belong to
  the protocol area.
- **Evidence:** documented + client-source + live-observed. **Confidence:** high.
- **Source:** D-OPT; D-AR §method; BJ-PRIM:180-221 (tutorial capability flow
  depends on the per-selector parts); LP `capabilities`, `element-capabilities`.
- **Cases:** `authz.method.options-not-gated`, `authz.method.options-reports-selector-grants`.

> **Refined by live observation (2026-09-28).**
> - The flat document-level answer is the union with selector-scoped grants
>   (R-PROTO-12 as reconciled).
> - PageLove ignores Deny rules when advertising.
> - pagelike keeps `Allow` consistent with what it enforces, so denied
>   methods are removed (keep-documented-security,
>   `protocol.options.selector-deny-removes-method.live`).

## 15. Identity and sessions

### R-PERM-64 — Every visitor has a session
- **Behavior:** The first response to a request without a valid session cookie
  MUST set a session cookie (anonymous visitors included). The docs name it
  `pagelove_session`; BRIEF reports `__Host-session`. pagelike: cookie name
  configurable, default `__Host-session`, attributes `Path=/; Secure; HttpOnly;
  SameSite=Lax` (no `Domain`). Requests presenting a valid cookie get no new
  cookie. The session carries: optional principal, transient element content
  (30-day TTL per element, independent of session lifetime), OIDC login state,
  and the echo-suppression key for SSE. A mutation that reaches the server with
  no session (only possible for non-browser clients that drop cookies) and needs
  one (transient writes) is `409`.
- **Evidence:** documented (every visitor, name), BRIEF (name). **Confidence:**
  high (behavior), low (name; not app-visible, never asserted by cases).
- **Source:** D-TR §Sessions and access control, §Error responses; D-SSE
  §Echo suppression; BJ-SSE:41.
- **Cases:** `authz.identity.anonymous-session-cookie`.

### R-PERM-65 — Session expiry and invalidation
- **Behavior:** Sessions have a configurable lifetime. An expired or invalid
  session cookie makes the request anonymous (and a new anonymous session is
  issued). Open SSE streams whose session expires or is invalidated receive a
  `reset` event with reason `session-expired` / `session-invalidated`
  (cross-area: sse).
- **Evidence:** documented (SSE reasons), inferred (rest). **Confidence:** medium.
- **Source:** D-SSE §Reset events.

### R-PERM-66 — Per-host identity provider configuration
- **Behavior:** A site MAY have one OIDC relying-party configuration with fields
  (console names per BRIEF; pagelike keys in parentheses): openid-configuration
  URL (`oidc.discovery_url`), client id (`oidc.client_id`), client secret
  (`oidc.client_secret`), login path (`oidc.login_path`, default `/auth/login`),
  logout path (`oidc.logout_path`, default `/auth/logout`), callback path
  (`oidc.callback_path`, default `/auth/callback`), plus pagelike extensions
  `oidc.roles_claim` (default `roles`) and `oidc.scopes` (default `openid email
  profile`). The host's `default-get-authz-mode` sits alongside (`urn:Host`
  field in the console). A site without a configured provider has no login link
  in error documents and every public-plane request is anonymous (SHOP: "This host
  has no OIDC, so every actor is `*`").
- **Evidence:** BRIEF (fields), demo-source (paths `/auth/login`, `/auth/logout`),
  documented (`/auth/callback` as the console's redirect URI), client-source
  (`default-get-authz-mode`). **Confidence:** low (field list), medium (default paths).
- **Source:** KANBAN:173, 186, 2614-2617; ATS index.html:403; CURSOR:167-188;
  D-ONB (`redirect_uri=https://config.onpagelove.com/auth/callback`); LP-CON
  (`/auth/login?redirect-post=/console/`); SHOP rules.html:5-7; SKILL:93-114;
  BLOG-ATS (missing client id broke login).

### R-PERM-67 — Login endpoint
- **Behavior:** `GET {login_path}[?redirect-post=<path>]`: if a provider is
  configured → `302` to the provider's `authorization_endpoint` with
  `response_type=code`, `client_id`, `redirect_uri=<scheme>://<host>{callback_path}`,
  `scope`, `state` and `nonce` bound to the session, and PKCE (`S256`). The
  `redirect-post` target (a same-origin absolute path; anything else is replaced
  by `/`) is stored in the session. Without a provider → `404` (pagelike; PageLove
  unknown). Misconfiguration (e.g. missing client id, unreachable discovery) →
  `503` with a pagelike Error item. `/-pagelove/oidc/login` MUST behave as an alias
  of the login path (the docs' error example links there). Built-in endpoints are
  not subject to rules and take precedence over stored documents at those paths.
- **Evidence:** inferred (standard OIDC code flow); LP-CON for `redirect-post`.
  **Confidence:** low (parameters), medium (`redirect-post`).
- **Cases:** `authz.identity.login-path-redirects`, `authz.identity.login-path-without-provider`.

> **Confirmed by live observation (2026-09-29).** On a host without an
> identity provider, `GET /auth/login` is `404`, as in pagelike. This is a
> read-only root probe, run with `--root`.

### R-PERM-68 — Callback endpoint
- **Behavior:** `GET {callback_path}?code&state`: verify `state` against the
  session, exchange the code (client secret), validate the ID token (signature
  via JWKS, `iss`, `aud`, `exp`, `nonce`), optionally merge userinfo claims,
  bind the principal to the session **rotating the session id**, then `302` to
  the stored `redirect-post` or `/`. Invalid/expired state or token → `400` with
  an Error item and no session change.
- **Evidence:** inferred. **Confidence:** low.

### R-PERM-69 — Logout endpoint
- **Behavior:** `GET` or `POST {logout_path}[?redirect-post=<path>]` removes the
  principal from the session (new anonymous session id), then `302` to the
  target or `/`. RP-initiated logout at the provider's `end_session_endpoint` is
  optional.
- **Evidence:** demo-source (link exists), inferred (semantics). **Confidence:** low.
- **Source:** KANBAN:2614-2617; CURSOR:175.

### R-PERM-70 — Claims to principal mapping
- **Behavior:** `sub` → `sub`/`username`; `email`, `email_verified`, `name`,
  `picture` copied; roles from R-PERM-61; all claims retained for
  `request.auth.claims.*`. Providers that omit `name`/`email` (e.g. Zitadel
  without token settings) leave them empty; nothing is synthesized.
- **Evidence:** documented + demo-source. **Confidence:** medium.
- **Source:** D-SES; D-REQ; KANBAN:155-158.

### R-PERM-71 — API keys and `Authorization` headers are not end-user identity
- **Behavior:** Console API keys (`pk_…`, `Authorization: Bearer`) authenticate
  the console and the WebDAV authoring plane only. On the public plane an
  `Authorization` header (Bearer or Basic) does not create a principal: the
  request stays anonymous (so a refusal is `401`), and the header remains visible
  to triggers (`Context.request.headers["authorization"]`, lowercase name).
  pagelike MAY later add explicit per-site key→principal mappings (plan §Security
  boundaries); none exist by default.
- **Evidence:** client-source (skill), demo-source (Basic-auth shop gate).
  **Confidence:** medium.
- **Source:** SKILL:329; SKILL-R:538-540; SHOP admin-auth.html:5-35.
- **Cases:** `authz.identity.api-key-not-end-user-identity`,
  `authz.identity.basic-authorization-header-not-identity`.

> **Confirmed by live observation (2026-09-29).** The authoring key on the
> public plane is refused like an anonymous request (`401`). The first run's
> 200 came from the runner sending the request to the WebDAV host (a harness
> artifact, fixed).

### R-PERM-72 — The authoring plane bypasses rules
- **Behavior:** Requests on the WebDAV authoring plane authenticated with an
  authoring key are not evaluated against `AuthorizationRule`s (they are how rule
  documents get written in the first place). Unauthenticated authoring-plane
  requests are `401` (authoring-plane error format; cross-area protocol).
- **Evidence:** demo-source. **Confidence:** medium (the WebDAV page's see-also
  says rules "apply to WebDAV operations"; §18 C13).
- **Source:** DEMO README.md:145-146; D-DAV; D-SM (WebDAV bypasses transition constraints).
- **Cases:** `authz.identity.webdav-bypasses-rules`.

### R-PERM-73 — pagelike-only identity sources (not PageLove behavior)
- **Behavior:** pagelike MAY offer local accounts (`users` table) at the same
  login path when no OIDC provider is configured, producing the same principal
  shape (`email_verified` only after verification). Behind
  `--dev-insecure-auth` (loopback only, warning per request), the harness MAY mint
  sessions for named actors (`sub`, `email`, `email_verified`, `roles`, `name`)
  through a dev-only endpoint under `/-pagelike/dev/` or directly in `site.db`;
  that is how `requires: [multi-actor]` cases run locally.
- **Evidence:** inferred (plan). **Confidence:** n/a (design decision).

## 16. Identity in composition and the request document

### R-PERM-74 — `request.auth` in composition
- **Behavior:** Expression bindings, Liquid templates and schema code can read
  `request.auth.username` (= `sub`), `request.auth.claims.<claim>` (any claim),
  and the **role list** as both `request.auth.role` and `request.auth.roles`. The
  role list = [verified email (if any)] + group names (Group documents, then OIDC
  roles, deduplicated) + `users`. For anonymous requests all of these are empty
  (falsy), never an error. The bare names `auth.claims.*`, `method`, `path`,
  `query.*` are rule-only and raise `undefined variable` in composition.
  Triggers see the equivalent under `ctx.request.auth` (JS) /
  `Context.request.auth` (Sessel).
- **Evidence:** documented + demo-source. **Confidence:** high (username, claims),
  medium (role list contents and both spellings), low (ordering).
- **Source:** D-SES §Authenticated identity in composition; D-REQ §Shape (role list
  includes the email, `admins`, `staff`, `users`); KANBAN:653 (`request.auth.role`);
  CURSOR:183-188 (`request.auth.roles`); D-TRIG (ctx.request.auth).
- **Cross-area:** languages (Liquid, Sessel, JS), composing-pages.
- **Cases:** `authz.identity.request-auth-in-liquid`, `authz.identity.request-auth-roles-alias`,
  `authz.identity.anonymous-auth-empty-in-composition`.

### R-PERM-75 — Request document `auth` section
- **Behavior:** The transient request document (`https://pagelove.org/Request`,
  not addressable over HTTP, reachable by includes and `r:` bindings during
  composition) contains, for an authenticated request,
  `<section itemprop="auth" itemscope itemtype="https://pagelove.org/Authorization">`
  holding a `claims` item (`https://pagelove.org/Claims`, one
  `<meta itemprop="{claim}" content="{value}">` per scalar claim), one
  `<meta itemprop="username">`, and one `<meta itemprop="role">` per role-list
  entry (R-PERM-74 order). For anonymous requests the `auth` section is present
  but empty (inferred).
- **Evidence:** documented. **Confidence:** high (shape), low (anonymous form).
- **Source:** D-REQ §Shape, §Fields; D-RB (note on binding the request document).
- **Cases:** `authz.identity.request-document-auth-include`.

### R-PERM-76 — Identity makes responses private
- **Behavior:** Any composed response that read `request.auth.*` or a
  request-document fragment MUST carry `Cache-Control: private` (cross-area:
  composing-pages / caching).
- **Evidence:** documented. **Confidence:** high. **Source:** D-SES; D-REQ; D-RB.

> **Kept despite live observation (2026-09-28).** PageLove serves such pages
> `Cache-Control: public, max-age=5` (keep-documented-security;
> `rw.reqdoc.auth-read-is-private.live`).

### R-PERM-77 — Composition is not filtered by rules
- **Behavior:** Bindings and includes read the whole site graph with server
  authority, regardless of whether the requester could fetch the source
  directly; only the request to the composed page itself is authorized. (This is
  the documented trust boundary: authoring a composed page = read access to the
  host.)
- **Evidence:** documented. **Confidence:** high.
- **Source:** D-RB §Security; D-L2 §Locking the data folder.
- **Cases:** `authz.identity.composition-reads-despite-rules`.

## 17. Cross-area dependencies (explicit)

| Area | What this area needs from it / gives to it |
|---|---|
| reading-writing | Range grammar, first-match resolution, all-matches reads (multipart, JSON-LD), POST placement, 206/204 codes, 404/416 semantics, directory index + 301, reserved `/.pagelove/` 403, templated creation, If-Match (authorization re-checked at commit). Paginated all-matches request syntax (R-PERM-43). |
| protocol | OPTIONS Allow/207 construction from R-PERM-63; QUERY css vs sessel authorization (R-PERM-24); WebDAV plane bypass and its error format (R-PERM-72). |
| sse | Subscribe authorization without default-GET (R-PERM-45); session expiry resets; echo suppression keyed on the session (R-PERM-64). |
| composing-pages | Composed DOM used for rule selectors; include/stamp/route write-through authorized on the composed page (R-PERM-28); unfiltered bindings (R-PERM-77); transient content per session; request document (R-PERM-75); `Cache-Control: private`. |
| modeling-data | Group subtypes (`parent`, Methods, `:isa()`), `@read` resolvers on `AuthorizationRule.actor` (R-PERM-8), schema cache. |
| reactions | Triggers may throw 401/403 themselves; `Pagelove.PUT` inside triggers goes through authorization (as which actor is unspecified, §20 P-18); ordering of triggers vs rule checks. |
| languages | Sessel/JS for `includes()`; Liquid/Sessel `request.auth.*`. |
| control plane | `default_get`, OIDC settings, API keys, `urn:Host` fields. |

## 18. Contradictions between sources and compatibility decisions

| # | Topic | Claims | Decision |
|---|---|---|---|
| C1 | Session cookie name | D-TR: `pagelove_session`; BRIEF: `__Host-session`. | Not app-visible (HttpOnly, never read by clients). Default `__Host-session`, configurable; cases never assert the name. |
| C2 | Login link target | D-AR examples: `/-pagelove/oidc/login`; apps (KANBAN, ATS, CURSOR) and console: `/auth/login`. | Link to the host's configured login path (default `/auth/login`); serve `/-pagelove/oidc/login` as an alias. |
| C3 | Login link presence | D-AR: present; LP (docs host): absent. | Present only when the host has an identity provider (the docs host presumably has none). |
| C4 | 401 vs 403 | Method tables: "Authorization denied → 403"; D-MOVE, notes, examples: 401 for unauthenticated. | 401 anonymous, 403 authenticated (R-PERM-54). |
| C5 | Selector-scoped GET allow | ATS:39-46: any public selector-scoped GET allow opens every selector and explicit denies do not override it; D-AR: per-element resolution. | Follow D-AR (the ATS note matches a platform bug the ATS blog lists as found and reported). Case keeps the docs claim. |
| C6 | Group resolution | ATS:110-112: `admins` group "isn't being resolved on this host (whoami shows roles=users only)"; D-GRP: Group documents grant. | Follow D-GRP (historical note). |
| C7 | `GroupMembership` | CURSOR:78, ATS:66-96 (actor + group rows); current docs: only `Group`. | Ignore `GroupMembership` (fail closed); disputed case records the other claim. |
| C8 | Liquid in rule fields | CURSOR:101-126: fields are Liquid templates; D-AR/SKILL: removed, literal. | Literal (R-PERM-36). |
| C9 | Role list accessor | D-SES/CURSOR: `request.auth.roles`; KANBAN: `request.auth.role` (matches D-REQ itemprop `role`). | Expose both (R-PERM-74). |
| C10 | Lookup vocabulary in rules | D-AR: `${request.*}` with `auth.sub/username/email/name`; D-SES: bare `auth.claims.*`, `method`, `path`, `query.*` are "authorization rules only". | Accept both spellings plus `request.auth.claims.*` (superset; R-PERM-31). |
| C11 | MOVE with a missing source/destination | D-MOVE §Denials: authorization "fails closed" (denied) if a selector matches nothing; D-MOVE §Error cases: 416 / 404. | 416/404 only when the actor may read the document, else 401/403 (R-PERM-49), consistent with the "absence and denial are distinct" rule for other writes. |
| C12 | Literal `*` in Allow | D-OPT selector example shows `Allow: *, OPTIONS`; D-OPT/D-AR text: never a literal `*`. | Concrete methods (BJ-PRIM would silently ignore `*`). Owned by protocol. |
| C13 | WebDAV and rules | DEMO README: WebDAV bypasses rules; D-DAV see-also: rules "apply to WebDAV operations". | Bypass for authoring-key requests (R-PERM-72). |
| C14 | Error vocabularies | Auth errors: `https://pagelove.org/1.0/Error` (`message`, `resource`); 413/WebDAV: `https://pagelove.org/Error` (`status`, `kind`, `message`); LP 416: `http://pagelove.org/Error` (`name`, `statusCode`, `description`). | Per-status fidelity: this area always emits the 1.0 shape. |
| C15 | Directory paths in rules | D-L1 relies on `/index.html` rules covering `/`; ATS duplicates rules for `/admin/` and `/admin/index.html`. | Match either form (R-PERM-19). |
| C16 | `:username` tier | D-AR: "above a group name" (exact tier unstated). | Same tier as exact user names (R-PERM-17); probe P-10. |
| C17 | Default of default-GET mode | SKILL sample host: `allow`; no doc states a default. | `allow` for new sites. |
| C18 | Email in role list | D-REQ example lists the email as a `role`; D-AR treats emails as a separate actor form. | Role list includes the verified email (R-PERM-74); matching is unaffected. |
| C19 | Table-form rules | D-AR and D-MOVE examples: `itemscope` on `<table>`, one rule per `<tr>`; HTML Microdata would merge rows into one item; every demo app uses `<tr itemscope>`. | Split table items per row (R-PERM-2a); `<tr itemscope>` works unchanged. |

## 19. Harness conventions used by this area's cases

- `rules:` shorthand is avoided; rule documents are written as files so the exact
  Microdata form (meta, table, nested `li`) is under test.
- Rule fields contain PageLove lookups such as `${request.method}`. The runner
  MUST substitute only `${P}`, `${HOST}`, `${SINK}` and captured names and leave
  every other `${…}` untouched.
- `microdata:` expectations use `{itemtype: <url>, properties: {<name>: <value>}}`
  and assert that an item of that type with those property values exists.
- Extra capabilities: `host-config` (case sets `site.settings`, e.g.
  `default_get`, that a live target cannot change; live runs only on a host that
  already matches); `oidc-config` (site needs an identity provider; locally the
  runner configures a stub OpenID provider via `site.settings.oidc: stub`);
  `sessel`, `liquid` (informational).
- Actors used across files (each case declares the ones it uses): `alice`
  (`sub-alice`, verified `alice@example.com`), `bob` (`sub-bob`, verified),
  `carol` (`sub-carol`, verified), `mallory` (`sub-mallory`, **unverified**
  `alice@example.com`), `dave`/`erin` (verified, role `staff`), `ops` (role
  `admins`, unverified email), `nomail` (`sub-nomail`, no email), `star` (`sub`
  = `a*`), `zed` (verified, other domain). Locally they are minted through the
  dev-only mechanism of R-PERM-73.
- Every rule `resource` and file path is under `${P}`. Two read-only probes of
  host-wide login paths declare `root: true`.
- `authz.group.groupmembership-legacy-honored` carries `status: disputed` (the
  losing claim of C7).
- `as: author` with `plane: public` (one case) means: send the author's API key
  on the public plane.
- Cases that write rule documents over WebDAV need the live runner's settle
  delay (R-PERM-6).
- Case files: `discovery`, `resource-glob`, `methods`, `selector-scope`,
  `multi-match`, `conflict`, `actors`, `groups`, `templated`, `default-get`,
  `move`, `absence-denial`, `errors`, `identity` (159 cases, 102 marked `live`).

## 20. Open questions for live probing

Each probe is a minimal sequence on a disposable host; `P` is a fresh prefix.
"anon" = no cookies; "user" = a signed-in browser session exported as a cookie.

- **P-1 Index normalization.** Rules: `* PUT Allow P/app/index.html selector h1`.
  anon `PUT P/app/` `Range: selector=h1` → 206 (normalized) or 401 (literal only)?
  Repeat with the rule on `P/app/` and request `P/app/index.html`.
- **P-2 Glob details.** Rules `* PUT Allow` on `P/f?.html`, `P/[ab].html`,
  `P/{one,two}.html`, `P/x/*`. anon whole-document PUTs to `P/f1.html`,
  `P/f10.html`, `P/f/.html`, `P/a.html`, `P/c.html`, `P/one.html`, `P/x`,
  `P/x/`, `P/x/a/b.html` → which are 2xx vs 401.
- **P-3 HEAD vs GET rules.** Default-GET allow host. Rule `* GET Deny P/d.html`.
  anon `HEAD P/d.html` → 401 (GET rules cover HEAD) or 200 (falls to default).
  Then rule `* HEAD Deny` only, anon `GET` → 200?
- **P-4 OPTIONS gating.** Rule `* * Deny P/*`. anon `OPTIONS P/d.html` → status and `Allow`.
- **P-5 Sessel QUERY.** Rule `* QUERY Allow P/d.html`; anon `QUERY` `text/sessel` `1 + 1` → 200? Without the rule → 401 (as LP)?
- **P-6 Whole-document GET vs selector deny.** Rules `* GET Allow P/d.html`,
  `* GET Deny P/d.html selector .secret`. anon `GET P/d.html` → 200 with the
  secret, 200 without it, or 401?
- **P-7 Paginated all-matches syntax.** Find the documented request form for "a
  range of a selector's matches" (e.g. `Range: selector=.item` + `Accept:
  multipart/mixed` + an `entries=` range) and confirm a page without the denied
  element is still 401.
- **P-8 Absent write target, unreadable page.** Rules `* GET Deny P/d.html`,
  `* PUT Allow P/d.html selector li`. anon `PUT` `Range: selector=#missing` → 401
  (documented) — confirm; then with `* GET Allow` → 416.
- **P-9 Resource-level deny vs selector allow at equal tier.** Rules `* PUT Deny P/d.html`, `* PUT Allow P/d.html selector .open`. anon PUT `.open` → 401 (single candidate set) or 206 (two gates)?
- **P-10 `:username` tier.** user alice. Rules `:username PUT Deny`, `<alice-sub> PUT Allow` on `P/d.html`. alice PUT → 403 (same tier) or 206 (lower tier).
- **P-11 `role:` form and role list.** user with IdP role `staff`: rule `role:staff PUT Allow` → 206? A Liquid page `{% for r in request.auth.role %}` vs `request.auth.roles` → which renders; does the list contain the email and `authenticated`?
- **P-12 Email case.** user email `alice@example.com` (verified), rule actor `Alice@Example.com` → 403 or 206; same for a Group `member`.
- **P-13 Substituted wildcard in actor.** Rule actor `${request.headers.x-actor}`, `PUT Allow`. anon PUT with `X-Actor: *` → 401 (literal) or 206 (wildcard).
- **P-14 Selector substitution escaping.** Rule selector `[data-o="${request.headers.x-o}"]`. anon PUT to `[data-o=a]` element with `X-O: a"], [data-o="b` → does it grant the `b` element?
- **P-15 Session cookie.** anon `GET P/d.html` → `Set-Cookie` name and attributes; second request with the cookie → no new cookie?
- **P-16 Login endpoints.** anon `GET /auth/login?redirect-post=/` and `GET /-pagelove/oidc/login` on a host with and without an identity provider → status, `Location` parameters (scope, PKCE, state); `GET /auth/logout`.
- **P-17 403 body.** user bob denied by a rule → full 403 body (title, message text, login/logout link?).
- **P-18 Trigger vs rule order.** Rule `* PUT Deny P/d.html` and a Trigger on `P/d.html` PUT that throws 418. anon PUT → 401 or 418? Also: which actor is used for `Pagelove.PUT` inside a trigger.
- **P-19 GroupMembership.** user alice (verified), only a `GroupMembership` row `alice@example.com → admins`, rule `admins PUT Allow` → 403 (ignored) or 206.
- **P-20 Rule itemtype scheme.** Rule with `itemtype="http://pagelove.org/AuthorizationRule"` → honored?
- **P-21 Comma in method.** Rule `method="GET, PUT"` (one element) → PUT allowed?
- **P-22 Directory redirect with unreadable index.** Rules `* GET Deny P/blog/*`. anon `GET P/blog` → 404 (documented) vs 401.
- **P-23 Error `resource` for directories.** anon denied `GET P/dir/` → `resource` = `P/dir/index.html`?
- **P-24 Rules in XML documents / route templates.** A rule inside `P/feed.xml` and inside `P/:id/x.html` → honored?
- **P-25 WebDAV and rules.** Author key `GET` over WebDAV of a document whose rules deny everyone → 200?
- **P-26 SSE per-event filtering.** Subscriber allowed at document level, rule `* GET Deny selector .secret`; a write to `.secret` → is the mutation event delivered?
- **P-27 Rule propagation.** Rewrite a rule document over WebDAV and poll: how long until it binds (≤60 s?); rewrite via public PUT: immediate?
- **P-28 Table-form rules.** One `<table itemscope itemtype=".../AuthorizationRule">`
  with row 1 `* P/d.html PUT h1 Allow` and row 2 `* P/d.html DELETE .item Allow`.
  anon `PUT` `Range: selector=.item` → 401 (rows split) or 206 (rows merged)?
- **P-29 Resource-level vs selector gates for reads.** Rules `* GET Deny P/d.html`,
  `* GET Allow P/d.html selector #pub`; anon `GET` `Range: selector=#pub` → 401
  (single candidate set, R-PERM-38) or 206.

## 21. Live reconciliation 2026-09-28

A live PageLove run on 2026-09-28 (host `live-test-host`,
`harness/observations/live-2026-09-28/`), confirmed on 2026-09-29
(`harness/observations/live-2026-09-29-confirm/`), failed 15
permissions-identity cases. [docs/compat/decisions.md](../compat/decisions.md)
has one row per case, giving the observed behaviour, the documented claim, the
decision and the rationale. Four of the 15 were harness artifacts:
- the plan footer inside 401 bodies;
- a root probe;
- the runner sending the author's key to the WebDAV host;
- a case confounded by the selector-Deny divergence.

**Superseded by live observation.** Each requirement below is marked in place
with the new behaviour:

| Requirement | New behaviour |
|---|---|
| R-PERM-1 | No rules in XML documents. |
| R-PERM-2 / R-PERM-3 | `action` is not trimmed: a padded `allow` grants nothing. |
| R-PERM-19 | Rules are matched against the canonical path only for Allow (P-1, C15). |
| R-PERM-20 | `dir/*` also matches `dir`. |
| R-PERM-49 | An absent source or anchor with selector-only DELETE/POST grants is refused. |
| R-PERM-51 | before/after absent anchor: 416; the authorization layer's 416 shape. |
| R-PERM-52 | A slash-less unreadable directory is a denial (P-22). |
| R-PERM-57 | Denials carry no `Cache-Control`. |

Refined or confirmed in place: R-PERM-63, 67 and 71.

**Kept despite live divergence** (keep-documented-security). Each is marked in
place, and a `.live` sibling measures PageLove:

| Requirement | Kept behaviour |
|---|---|
| R-PERM-2 | A padded `deny` still refuses. |
| R-PERM-2a | Table rows stay separate rules. PageLove merges them, which escalates privileges (P-28). |
| R-PERM-27 / R-PERM-38 | A selector-scoped Deny beats a resource-level Allow at the same tier. PageLove does not enforce it (P-9's mirror). |
| R-PERM-41 | All-matches reads are all-or-nothing, and a denied first match is refused. |
| R-PERM-49 | Absence is not revealed to non-readers on MOVE. |
| R-PERM-63 | `Allow` reflects denies. |
| R-PERM-76 | Pages that read `request.auth` stay private. |

Of these, R-PERM-27/38, R-PERM-41 and R-PERM-63 all trace to one PageLove
behaviour: a selector-scoped Deny does not override a resource-level Allow,
and Deny rules are ignored when advertising. If PageLove fixes it, the `.live`
siblings will start failing, which is the signal to drop them.

**Settled probes:**

| Probe | Answer |
|---|---|
| P-1 | Canonical path only. |
| P-2 (in part) | `/x/*` matches `/x`. |
| P-4 | OPTIONS is answered under `* * Deny`, and live even lists the default-GET methods. |
| P-16 (no provider) | 404. |
| P-22 | 401. |
| P-24 (XML) | Not honoured. |
| P-28 | Rows merged on PageLove; kept split in pagelike. |
