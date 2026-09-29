# Live reconciliation decisions: reacting (2026-09-29)

This file records how pagelike handled the eight reacting cases that diverged
in the live PageLove run of 2026-09-29 (a sample of one case per feature). It
follows the decision policy of [decisions.md](../decisions.md): `adopt-live`
is the default, `keep-documented-security` and `keep-standard` keep pagelike
and add a `.live` sibling, and `harness-artifact` fixes the case or harness.

**Evidence.**
- First run: `harness/observations/live-2026-09-29/` (one JSON file per case;
  the file name is the case id with dots turned into dashes, cut to 40
  characters).
- Reconciliation runs: `harness/observations/live-2026-09-29-reconcile-reacting/`.
  The JSON files at the top are the final run; `run1/`, `run2/` and `run3/`
  keep the earlier runs, and `RUN-SUMMARY.txt` lists every outcome.
- Probes written for this reconciliation (`harness/cases/reacting/probe-0929.yaml`,
  `evidence: live-observed`):
  - `reacting.discovery.probe-0929.handler-shape`: which Trigger, Processor and
    TransitionHandler shapes a serving-path write accepts;
  - `reacting.filters.probe-0929.selector-scope`: which requests a
    selector-filtered trigger or processor fires on;
  - `reacting.actions.probe-0929.thrown-redirect` (`live-divergence`): what
    becomes of a thrown 3xx for each method;
  - `reacting.context.probe-0929.trigger-bindings` (`live-divergence`): which
    names trigger and processor code can see, and what a failing gate does.

Live budget: 4 runs, 29 cases sent (9, 4, 4 and 12; the final run also listed
one local-only case, which the runner skipped).

## Summary

| Case id | Class | pagelike change |
|---|---|---|
| `reacting.actions.actions-run-in-document-order` | adopt-live | 422 for a handler without exactly one action |
| `reacting.actions.sessel-throw-303-location-pair` | keep-standard | none; sibling `.live` |
| `reacting.context.header-lookup-lowercase` | harness-artifact | none (case gets the no-op action) |
| `reacting.context.js-auth-anonymous-null` | adopt-live | anonymous `auth` is `{claims: {}, roles: []}` |
| `reacting.context.resource-binding-on-trigger` | adopt-live (after a harness fix) | `r:` bindings are not bound in reaction code |
| `reacting.discovery.no-action-skipped` | adopt-live | 422 (same check as the first row) |
| `reacting.filters.method-case-insensitive` | adopt-live | 422 for a method outside the HTTPMethod enumeration |
| `reacting.filters.selector-semantic-match` | adopt-live | the `selector` filter is not evaluated |

| Class | Cases |
|---|---|
| adopt-live | 6 |
| keep-documented-security | 0 (one related finding, below) |
| keep-standard | 1 |
| harness-artifact | 1 |

All eight cases, the `.live` sibling and the four probes passed in the final
live run, except `reacting.actions.sessel-throw-303-location-pair`, which is
now `live: false` (it asserts the kept pagelike behaviour; its sibling runs
live instead).

## The platform schema of reaction items (three cases)

Three failures had one cause. A serving-path write that stores a Trigger is
validated against PageLove's platform schemas and refused with 422 before
anything runs:

```
<div itemscope itemtype="https://pagelove.org/SchemaViolation"><ul itemprop="problems"><li itemprop="problem"><span itemprop="message">[https://pagelove.org/Handler].action: cardinality 1..1 violated: expected exactly 1 value, found 0</span></li></ul></div>
```

The probe `reacting.discovery.probe-0929.handler-shape` mapped the schema:

- `Handler` (Trigger, Processor and TransitionHandler): `action` exactly one,
  `when` at most one, `otherwise` any number.
- `RequestHandler` (Trigger and Processor): each `method` must be one of `GET`,
  `PUT`, `POST`, `DELETE`, `PATCH`, `MOVE`, `*`, compared case-sensitively.
  `HEAD`, `Put`, `put`, `get` and `options` were all refused, each value with
  its own problem, after the `action` problem.
- A whole-document PUT checks every item. A selector POST that inserts a
  trigger is checked. A selector PUT or DELETE that breaks the enclosing
  trigger (an invalid `method`, the only `action` removed) is refused too, but
  in another envelope (`https://dombase.pagelove.team/ns/error/Cardinality`,
  message `https://pagelove.org/Handler /action: … (cardinality)`). A selector
  write elsewhere in a document whose trigger was stored invalid over WebDAV
  succeeds (206). WebDAV writes are never checked.

pagelike implements this in `internal/reactions/platform.go`, which also holds
the existing TransitionConstraint check (R-REACT-62). The messages are
PageLove's, and every refusal uses pagelike's SchemaViolation envelope. The
runtime rules for items stored over WebDAV (skip an item without actions, run
several actions in order, match methods case-insensitively, AND several gates)
are unchanged, and local-only `-stored` cases keep them tested.

### `reacting.actions.actions-run-in-document-order`

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/reacting-actions-actions-run-in-document.json`;
  final `live-2026-09-29-reconcile-reacting/reacting-actions-actions-run-in-document.json`;
  probe `reacting-discovery-probe-0929-handler-sh.json`.
- **PageLove:** the PUT installing a trigger with two actions is 422
  "[https://pagelove.org/Handler].action: cardinality 1..1 violated: expected
  exactly 1 value, found 2". Nothing is installed, and the later PUT is 201.
- **pagelike now:** the same 422 and message. A two-action trigger stored over
  WebDAV still runs its actions in document order
  (`reacting.actions.actions-run-in-document-order-stored`, local only).
- **Rationale:** The docs describe several actions per trigger, but PageLove's
  own schema allows one, so no PageLove app can have installed a second action
  through the serving path. Refusing the write is a stricter check that fails
  closed and tells the author at once. Several effects still fit in one
  Sessel action or in several triggers. Nothing an app relies on is lost, so
  the default applies.

### `reacting.discovery.no-action-skipped`

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/reacting-discovery-no-action-skipped.json`;
  final `live-2026-09-29-reconcile-reacting/reacting-discovery-no-action-skipped.json`.
- **PageLove:** an action-less trigger is refused with 422 ("… found 0") and
  not stored (GET 404). The following write proceeds.
- **pagelike now:** the same. A stored action-less trigger is still skipped
  (`reacting.discovery.no-action-skipped-stored`, local only).
- **Rationale:** The documented "a trigger with neither is skipped" describes
  an item that does nothing. Refusing to store it is harmless and fails
  closed. The same check also refuses a trigger with only a `when` and an
  `otherwise`, a shape the docs and demos use. The docs' own `otherwise` example
  adds the no-op action `1`, which is the accepted shape. The cases that used
  gate-only triggers now carry it: `header-lookup-lowercase`,
  `header-lookup-canonical-case`, `header-lookup-canonical-case-misses-http2`,
  `resource-binding-on-trigger` (first rewrite) and
  `gates.shop-basic-auth-gate`.

### `reacting.filters.method-case-insensitive`

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/reacting-filters-method-case-insensitive.json`;
  final `live-2026-09-29-reconcile-reacting/reacting-filters-method-case-insensitive.json`.
- **PageLove:** `method="put"` is 422 `[https://pagelove.org/RequestHandler].method:
  Value "put" is not a valid https://pagelove.org/HTTPMethod (expected one of:
  "GET", "PUT", "POST", "DELETE", "PATCH", "MOVE", "*")`.
- **pagelike now:** the same, for Triggers and Processors. A stored trigger
  with `put` still matches PUT (`reacting.filters.method-case-insensitive-stored`,
  local only).
- **Rationale:** Case-insensitive matching is only reachable for items stored
  over WebDAV. Refusing a lower-case or `HEAD` value at install time loses
  nothing, because `GET` already covers HEAD and `*` covers every method. It
  also matches the error an author sees on PageLove.

## `reacting.actions.sessel-throw-303-location-pair`

- **Class:** keep-standard.
- **Observations:** `live-2026-09-29/reacting-actions-sessel-throw-303-locati.json`;
  probe `live-2026-09-29-reconcile-reacting/reacting-actions-probe-0929-thrown-redir.json`
  (first form in `run1/`); the sibling's final result is in
  `live-2026-09-29-reconcile-reacting/reacting-actions-sessel-throw-303-locati.json`.
- **PageLove:** for a POST **without `Range`**, a trigger's thrown 3xx is not
  sent. PageLove instead reads the `Location` as a template, as in templated
  creation (composing R-COMP-140):
  - a missing page (`/other-page.html`) gives 404 "Document not found:
    /other-page.html";
  - an existing page without `<base href>` gives 422 "Template must include a
    &lt;base href&gt; element specifying the target resource path" (for 303
    and 302 alike);
  - an absolute external URL gave a Cloudflare 403 page;
  - a thrown 201 with a `Location` keeps its status and body but loses the
    `Location`.

  GET, PUT, DELETE and selector POSTs send the thrown 303 with its
  `Location`, as documented.
- **pagelike now:** unchanged: every method sends the thrown response,
  including the 303 and its `Location` (R-REACT-31).
- **Rationale:** The documented example is a redirect after a form POST, which
  browsers follow as Post/Redirect/Get (RFC 9110 §15.4.4). On PageLove, the
  form POST instead reads the target as a template. That gives 404/422 for
  ordinary pages, and for a real template it would store a new document the
  trigger never asked for. Adopting it would make a redirect write data, and
  the behaviour cannot be built on. The kept case is now `live: false`, and
  `reacting.actions.sessel-throw-303-location-pair.live` asserts PageLove's 404
  with no `Location`.

## `reacting.context.header-lookup-lowercase`

- **Class:** harness-artifact.
- **Observations:** `live-2026-09-29/reacting-context-header-lookup-lowercase.json`;
  final `live-2026-09-29-reconcile-reacting/reacting-context-header-lookup-lowercase.json`.
- **PageLove:** the gate-only trigger (`when` + `otherwise`) was refused with
  422 "… action … found 0", so the header lookup was never exercised. With the
  documented no-op action added, the lower-case lookup works: 401 without the
  header, 201 with it.
- **pagelike now:** unchanged (lower-case keys, case-insensitive lookup,
  R-REACT-23). It passes on both targets.
- **Rationale:** The refusal concerned the item's shape (the platform schema
  above), not the header lookup, so the case was fixed rather than adopting a
  rejection of the gate.

## `reacting.context.js-auth-anonymous-null`

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/reacting-context-js-auth-anonymous-null.json`;
  final `live-2026-09-29-reconcile-reacting/reacting-context-js-auth-anonymous-null.json`.
- **PageLove:** `JSON.stringify(ctx.request.auth ?? null)` for an anonymous
  request is `{"claims":{},"roles":[]}`.
- **pagelike now:** the same object (`internal/reactions/eval.go`, `jsArg`).
  Before, it was absent (`undefined`). Sessel already exposed an object with
  empty claims and roles.
- **Rationale:** The anonymous object holds no claim and no role, so every
  check that can authorize someone (a claim, an email, a role) still refuses
  an anonymous request. PageLove apps read `ctx.request.auth.claims.x`
  without a null check, which pagelike turned into a TypeError and a 500. The
  only code that behaves differently tests the truthiness of `auth` itself,
  which is already wrong on PageLove, so no PageLove app can rely on it. This
  is not a platform security boundary, so the default applies.
  `javascript.md` R-JS-21 still says "absent", and `internal/jsglue`'s unit
  test was updated.

## `reacting.context.resource-binding-on-trigger`

- **Class:** adopt-live, after a harness fix.
- **Observations:** `live-2026-09-29/reacting-context-resource-binding-on-tri.json`
  (422, no action); `run1/reacting-context-resource-binding-on-tri.json` (with
  the no-op action, the right token was still refused); probe
  `run2/` and final `reacting-context-probe-0929-trigger-bind.json`; final
  `reacting-context-resource-binding-on-tri.json`.
- **PageLove:** an `r:` resource binding on the trigger element, on
  `<body>`, on `<html>`, or on a Processor is not bound. Reading it in an
  action is 500 with a `TriggerError` item, "undefined variable: secret". In
  a `when` gate the same error is swallowed as falsy, so `otherwise` ran for
  the right token too. A bare Sessel selector
  (`${[itemtype='…'] [itemprop="token"]}`) reads the same private document.
- **pagelike now:** reaction items bind only `e:`/`j:` expression bindings.
  An `r:` name is undefined ("unresolved variable: secret", 500). The case
  now reads the binding in an action and expects 500, and it passes on both
  targets.
- **Rationale:** PageLove does not implement the documented binding placement
  for triggers, so no PageLove app uses it, and removing it does not reduce
  security (a binding reads the whole host without authorization). Authors keep
  the documented alternative, a bare Sessel selector, which the shop demo
  uses. `e:`/`j:` bindings were not probed and are kept.

## `reacting.filters.selector-semantic-match`

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/reacting-filters-selector-semantic-match.json`;
  probe `run1/` and final `reacting-filters-probe-0929-selector-sco.json`;
  final `reacting-filters-selector-semantic-match.json`.
- **PageLove:** a trigger filtered on `article` fired for a selector PUT of
  `#h` (a heading) in every case the probe tried:
  - in a document with no article at all;
  - with the article under another parent;
  - for a missing element (instead of core's 416);
  - for a whole-document PUT.

  Triggers filtered on `#never-present` fired for selector POST, GET and
  DELETE, and a processor with that filter rewrote the status of a selector
  GET. The `selector` filter is not evaluated.
- **pagelike now:** the filter is parsed and ignored (`internal/reactions`: the
  key-element matching is gone), for triggers and processors.
- **Rationale:** No PageLove app can depend on the filter excluding a request,
  and apps tested on PageLove do depend on it firing. The webhook recipe's
  Note-filtered POST trigger fires on PageLove only because the filter is
  ignored, and it would never fire under pagelike's key-element rule (C6).
  Over-firing never grants anything: triggers and processors can refuse,
  rewrite or queue, but not authorize. So the default applies.
- **Consequential updates:**
  - `selector-no-match-does-not-fire` now fires (409);
  - `selector-post-append-key-element-is-list` now fires;
  - `selector-post-append-matches-fragment` is no longer `disputed`, because it
    would XPASS live.

## Related finding kept: a failing `when` is falsy on PageLove

The binding probe showed that PageLove swallows a runtime error in a `when`
gate (`no_such_name == 1`) as falsy, and runs `otherwise` (409 "when-false").
An error in an action is surfaced (500 `TriggerError`). The docs say runtime
errors are "surfaced, not swallowed, regardless of which language raised
them" (R-REACT-34). pagelike keeps 500 for a failing gate
(keep-documented-security). A gate of the form "when this request is bad, the
action refuses it" would otherwise fail open whenever the check errors. No
listed case covers this; the probe's `live-divergence` step measures it. The
unlisted `reacting.gates.when-runtime-error-fails-request` would fail live for
this reason.

## Other consequential updates (local harness)

- `reacting.gates.several-when-all-must-pass`: two `when` values are now
  refused (422, `Handler.when` 0..1, probe step). The AND rule moved to
  `reacting.gates.several-when-all-must-pass-stored` (local only).
- `reacting.outbound.method-put`: its two HttpRequest actions are split into
  two triggers.
- `reacting.th.sessel-action-never-fires`: the two-action handler is stored
  over WebDAV (site files), since the serving path refuses it. The case needs
  `outbound-http`, so it is local only anyway.
- `internal/reactions/reactions_test.go`: the gate test carries the no-op
  action. New unit tests are in `internal/reactions/live0929_test.go`.

## Harness notes

- A YAML step name containing ` #` is cut at the `#` (a comment). The probe's
  step names are quoted.
- Inside a Sessel `${…}` selector, an unquoted attribute value
  (`[itemprop=token]`) is an expression, so PageLove answered "undefined
  variable: token". Quote such values (`[itemprop="token"]`). The CSS `r:`
  attribute values are plain CSS and unaffected.

## Unresolved

- `e:`/`j:` expression bindings on reaction items were not probed.
- `Handler` also covers TransitionHandler. Its other properties (`selector`,
  `property`, `becomes`) and HttpRequest action properties (for example a
  lower-case outbound `method`) were not probed.
- pagelike reports enclosing-item violations of selector writes in the
  SchemaViolation envelope, where PageLove uses
  `https://dombase.pagelove.team/ns/error/Cardinality`. The modeling area owns
  that envelope question.
- `javascript.md` R-JS-21 ("auth absent for anonymous") needs the same note as
  R-REACT-25. It is outside this area.
