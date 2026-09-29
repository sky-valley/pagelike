# Live reconciliation decisions: modeling (2026-09-29)

This file records how pagelike handled the nine modeling cases that diverged in
the live PageLove run of 2026-09-29 (one sampled case per feature). It follows
the policy table of `docs/compat/decisions.md`: `adopt-live` is the default,
`keep-documented-security` and `keep-standard` keep pagelike and add a
`.live` sibling, and a `harness artifact` is fixed in the case or harness.

**Evidence.**
- First run: `harness/observations/live-2026-09-29/` (`RUN-SUMMARY.txt` plus
  one JSON file per case; the file name is the case id with dots turned into
  dashes, cut to 40 characters; a `.live` sibling now keeps its `-live`
  suffix, see Harness artifacts and harness notes).
- Probes and confirmation: `harness/observations/live-2026-09-29-reconcile-modeling/`,
  same naming. Probe cases are in `harness/cases/modeling/probes-0929.yaml`.
- Live runs made for this area: 4, with 37 cases in all (run 1: 5 probes;
  run 2: 2 probes; runs 3 and 4, the confirmations: the 9 listed cases, 1
  `.live` sibling and 5 probes, 15/15 passed each time). Run 4 repeats run 3
  after a harness fix (see Harness artifacts and harness notes); its files
  replace those of the earlier runs for the cases it ran, and each
  re-observed the same answers.

## Summary

| Case id | Class | PageLove | pagelike now |
|---|---|---|---|
| `modeling.computed.write-rejected` | adopt-live | Stores a value written to a `@computed` property, and type-checks it like any other value | Same (the computed guard is gone) |
| `modeling.nested.recursive-validation` | adopt-live | Validates only outermost items; nested items and schema-typed values are not checked | Same |
| `modeling.resolvers.sessel-write-normalise-email` | harness artifact | The documented `el.set_text(…)` form fails (500); a constructed element with `.lower()` works | Case uses the working form; pagelike runs both (Sessel differences deferred) |
| `modeling.shapes-closed.note-worked-example` | adopt-live | 422 `dombase…/ShapeConstraint` problems item, PageLove's closed-shape messages | Same, byte for byte |
| `modeling.shapes.required-properties-user` | adopt-live | 422 `dombase…/ShapeConstraint` problems item | Same, byte for byte |
| `modeling.uniqueness.composite-recipe` | adopt-live | 422 `dombase…/ConstraintViolation` item, `unique-group(g)`, members sorted | Same, byte for byte |
| `modeling.uniqueness.duplicate-across-documents` | keep-documented-security (partly adopt-live) | 422 `dombase…/ConstraintViolation` item naming the other document (`at '<path>'`) | Same item, without the path; `.live` sibling |
| `modeling.write-paths.failed-write-changes-nothing` | adopt-live | A selector write that breaks a cardinality: 422 `dombase…/Cardinality` item | Same, byte for byte |
| `modeling.write-paths.webdav-put-validated-by-schema` | adopt-live | A WebDAV PUT is stored with no schema or shape check, also 65 s after the declarations | Same |

| Class | Cases |
|---|---|
| adopt-live | 7 |
| keep-documented-security | 1 (partly adopt-live) |
| keep-standard | 0 |
| harness artifact | 1 |
| **total** | **9** |

New cases:
- `.live` sibling: `modeling.uniqueness.duplicate-across-documents.live`.
- Probes (all `evidence: live-observed`): `modeling.probe-0929.resolver-forms`
  (`status: live-divergence`, see Unresolved), `modeling.probe-0929.nested-scope`,
  `modeling.probe-0929.computed-value`, `modeling.probe-0929.write-envelopes`,
  `modeling.probe-0929.constraint-envelopes`. A sixth probe,
  `modeling.probe-0929.webdav-after-propagation`, ran once (run 2) and was then
  merged into `write-envelopes` to keep each confirmation run within 15 cases.
  Its observation file is kept.

## Cases

### modeling.computed.write-rejected

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/modeling-computed-write-rejected.json`;
  `live-2026-09-29-reconcile-modeling/modeling-probe-0929-computed-value.json`,
  `modeling-computed-write-rejected.json`.
- **PageLove:** a PUT carrying `<meta itemprop="display-name" content="Countess">`
  for a `@computed` property answers 201 and stores it. The probe shows the
  value is validated like any other: with `type` Integer, "Countess" is refused
  (422 SchemaViolation "Value \"Countess\" is not a valid integer") and "42" is
  stored. The JSON-LD of a stored Person carries no computed value.
- **pagelike before:** 422 SchemaViolation `check` `computed` (a pagelike
  decision for the documented "rejected with an error").
- **pagelike now:** the computed guard (R-MOD-15 stage 1) is removed and the
  structural stage checks computed properties like the others
  (`internal/schema/pipeline.go`). Sessel/JS construction (`new Type {…}`)
  still refuses a computed value; that path was not probed.
- **Rationale:** the docs say such a write is rejected, PageLove accepts it.
  Accepting it produces no incorrect data: the stored markup is what the client
  sent, it is still type-checked, and typed reads compute the property anyway.
  The rejection status was never documented (it was a pagelike choice), so
  default policy applies. The legacy bare-Sessel `@read` case was updated the
  same way.

### modeling.nested.recursive-validation

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/modeling-nested-recursive-validation.json`;
  `live-2026-09-29-reconcile-modeling/modeling-probe-0929-nested-scope.json`
  (runs 2 and 3), `modeling-nested-recursive-validation.json`.
- **PageLove:** a Person whose `address` is an Address item missing its 1..1
  `city` is stored (201). The probe separates the questions: a top-level
  Address missing `city` is refused (the schema is live); an Address inside the
  Person's markup without `itemprop`, an Address inside an item whose type has
  no schema, an `address` value that is plain text, and an `address` value
  typed as Person are all stored. A Person missing `name` and holding an
  incomplete Address is refused for `name` only.
- **pagelike before:** nested items were validated recursively (R-MOD-24) and
  independently (R-MOD-14), and schema-typed values were type-checked.
- **pagelike now:** only outermost items (no item ancestor) are instances
  (`Registry.affectedInstances`), and schema-typed properties are not
  type-checked in the write pipeline.
- **Rationale:** validation PageLove does not do is a behaviour clients cannot
  rely on, and skipping it stores what the client sent, so it produces no
  incorrect data (policy: adopt-live covers PageLove bugs that do not produce
  incorrect data). The shop demo's comment that nested OrderLines are
  "validated independently" is not true on PageLove. Four more nested cases
  were updated (see Consequential updates).

### modeling.resolvers.sessel-write-normalise-email

- **Class:** harness artifact (the Sessel differences it revealed are
  unresolved and deferred, see below).
- **Observations:** `live-2026-09-29/modeling-resolvers-sessel-write-normalis.json`;
  `live-2026-09-29-reconcile-modeling/modeling-probe-0929-resolver-forms.json`,
  `modeling-resolvers-sessel-write-normalis.json`.
- **PageLove:** the documented example
  `self.map((el) => el.set_text(el.text().trim().lowercase()))` fails with 500
  `https://dombase.pagelove.team/ns/error/Internal` "resolver pipeline error:
  sessel binding threw: unknown function: set_text (at bytes 0..4)". The probe
  tried four forms: `.lowercase()` is unknown (A, B); the setter `.text(x)`
  refuses a stored element, "type error: text(value) setter requires a
  constructed element, got element" (D); a constructed element
  `new span[itemprop="email"] { el.text().trim().lower() }` works and stores
  `alice@example.com` (C).
- **pagelike before and now:** all four forms work (R-SESSEL-113 aliases,
  R-SESSEL-241 `set_text`, R-SESSEL-242 mutable pipeline elements).
- **Rationale:** the case built its input from a docs example that PageLove
  cannot run, so it tested the Sessel dialect rather than the `@write` pipeline.
  The case now uses form C, the shape of the documented slug recipe, and passes
  on both targets: the pipeline semantics (the result replaces the value before
  storage) agree. Whether pagelike should also refuse `set_text`,
  `lowercase()` and setters on stored elements is a Sessel-area decision (the
  evaluator is shared); it is recorded under Unresolved and measured by the
  live-only probe `modeling.probe-0929.resolver-forms`.

### modeling.shapes-closed.note-worked-example

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/modeling-shapes-closed-note-worked-examp.json`;
  `live-2026-09-29-reconcile-modeling/modeling-shapes-closed-note-worked-examp.json`.
- **PageLove:** the statuses are as documented (201, then three 422s, and the
  refused note is not stored). The body is not the documented
  ConstraintViolation page but the write path's bare problems item
  `<div itemscope itemtype="https://dombase.pagelove.team/ns/error/ShapeConstraint"><ul itemprop="problems">`,
  with a first problem "Shape constraints violated" and then one problem per
  violation, `https://pagelove.org/ShapeConstraint <selector>: <message>`.
  Closed-shape messages: "Element &lt;img&gt; inside '…' is not permitted by
  any permit", "Attribute &lt;onclick&gt; on &lt;h2&gt; inside '…' is not
  permitted by any matching permit", "Attribute &lt;class&gt; on
  &lt;article&gt; matched by '…' is not permitted by the selector or any
  matching permit". `Content-Type: text/html`.
- **pagelike before:** the documented page (`https://pagelove.org/ConstraintViolation`,
  `constraintSelector`, `failedConstraint`) with its own closed-shape wording.
- **pagelike now:** PageLove's item and messages, byte for byte
  (`schema.shapeError`, `Shape.checkClosed`), served as `text/html`.
- **Rationale:** status and vocabulary only; the same kind of adoption as the
  write-path vocabularies of LO-5. No client can depend on the documented page,
  since PageLove never serves it.

### modeling.shapes.required-properties-user

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/modeling-shapes-required-properties-user.json`;
  `live-2026-09-29-reconcile-modeling/modeling-shapes-required-properties-user.json`,
  `modeling-probe-0929-constraint-envelopes.json` (two violations; a shape broken
  by a selector DELETE).
- **PageLove:** as above: 422 ShapeConstraint problems item with the documented
  message "Element matching '[itemtype*=User]' does not satisfy constraint
  ':has([itemprop="email"])'" (quotes literal: only `& < >` are escaped). Two
  failing constraints give two problems, in constraint order. A selector DELETE
  that breaks the shape answers 409
  `https://dombase.pagelove.team/ns/error/CascadeBlocked` with one problem:
  "Operation refused by cascade constraint: Constraint violation: 1
  violation(s):" and a line "  - [https://pagelove.org/ShapeConstraint]
  [itemtype*=User] (:has([itemprop="email"])): Element matching …".
- **pagelike before:** the documented page (409 variant inferred).
- **pagelike now:** both items byte for byte.
- **Rationale:** as for the closed-shape case. The documented `constraintSelector`
  and `failedConstraint` properties are dropped because PageLove does not send
  them; the same information is in the message.

### modeling.uniqueness.composite-recipe

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/modeling-uniqueness-composite-recipe.json`;
  `live-2026-09-29-reconcile-modeling/modeling-uniqueness-composite-recipe.json`.
- **PageLove:** the behaviour is as documented (composite groups work, which
  settles contradiction C15). The body is the bare problems item
  `https://dombase.pagelove.team/ns/error/ConstraintViolation` with one message:
  "Constraint violation: 1 violation(s):\n  - [uniqueness] [itemprop='org-id'],
  [itemprop='user-id'] (unique-group(membership-pair)): Composite uniqueness
  violation: value combination already exists for properties 'org-id, user-id'".
  Members are in alphabetical order, not declaration order.
- **pagelike before:** a ConstraintViolation page in the TransitionConstraint
  markup, `unique(<group>)`, declaration order.
- **pagelike now:** PageLove's item byte for byte (`hostwide.go`). As PageLove
  does (probe `constraint-envelopes`: two duplicated unique properties give
  "1 violation(s)"), only the first uniqueness violation is reported.
- **Rationale:** vocabulary only. Demo apps match `/uniqueness/i` on the body,
  which still holds.

### modeling.uniqueness.duplicate-across-documents

- **Class:** keep-documented-security (partly adopt-live).
- **Observations:** `live-2026-09-29/modeling-uniqueness-duplicate-across-doc.json`;
  `live-2026-09-29-reconcile-modeling/modeling-uniqueness-duplicate-across-doc.json`,
  `modeling-uniqueness-duplicate-acros-live.json` (the sibling),
  `modeling-probe-0929-constraint-envelopes.json`.
- **PageLove:** 422 ConstraintViolation problems item, "  - [uniqueness]
  [itemprop='slug'] (unique(slug)): Uniqueness violation: value already exists
  for property 'slug' at '/_pl/…/p1.html'". Two items of the written document
  sharing a value: "another item in '<path>' already claims this value for
  property 'slug' (first claimed by item '0.2.0.1')".
- **pagelike now:** the same item and wording, but for a value held in another
  document the message ends at "for property 'slug'". The in-document message
  is adopted (it names only the document being written); its item path follows
  pagelike's parser (an implied empty `<head>` is skipped, but the whitespace
  node PageLove keeps before `<html>` does not exist), so the numbers can
  differ.
- **Rationale:** naming the path of the document that already holds the value
  tells a writer where that value lives, including in documents they may not
  be allowed to read. R-MOD-72 decided that "the other instance's location is
  not disclosed", and following PageLove would weaken that authorization
  property; the refusal itself (inherent to uniqueness) is kept. Everything
  else in the body is adopted. The sibling
  `modeling.uniqueness.duplicate-across-documents.live` asserts the path in
  PageLove's answer.

### modeling.write-paths.failed-write-changes-nothing

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/modeling-write-paths-failed-write-change.json`;
  `live-2026-09-29-reconcile-modeling/modeling-write-paths-failed-write-change.json`,
  `modeling-probe-0929-write-envelopes.json`.
- **PageLove:** atomicity holds (the document and its ETag are unchanged). The
  refusal of the selector DELETE is not a SchemaViolation but
  `<div itemscope itemtype="https://dombase.pagelove.team/ns/error/Cardinality">`
  with "https://pagelike.test/…/Tag /label: cardinality 1..1 violated: expected
  exactly 1 value, found 0 (cardinality)". The probe shows the same item for a
  selector PUT and a selector POST ("found 2"), while a whole PUT answers the
  SchemaViolation item "[…/Tag].label: cardinality 1..1 violated: expected
  exactly 1 value, found 2".
- **pagelike now:** a selector write (PUT, POST, DELETE, element MOVE) whose
  first structural failure is a cardinality answers the Cardinality item byte
  for byte; cardinality and group messages use PageLove's words everywhere
  ("cardinality 1..1 violated: expected exactly 1 value, found 0"). Whole
  writes keep pagelike's SchemaViolation page (see Unresolved).
- **Rationale:** vocabulary only. Element MOVE and the 0..1 / 1..n phrases
  ("at most 1 value", "at least 1 value") are inferred from the observed 1..1
  form.

### modeling.write-paths.webdav-put-validated-by-schema

> **Reclassified at integration: keep-documented-security.** pagelike keeps
> validating WebDAV PUTs; this section's live findings are asserted by the
> `.live` siblings. See `serialization.md`, "Integration".

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/modeling-write-paths-webdav-put-validate.json`;
  `live-2026-09-29-reconcile-modeling/modeling-probe-0929-webdav-after-propaga.json`
  (run 2), `modeling-probe-0929-write-envelopes.json` (runs 3 and 4),
  `modeling-write-paths-webdav-put-validate.json`.
- **PageLove:** a WebDAV PUT of a Tag missing its 1..1 `label` is stored (201)
  and reads back as sent. A WebDAV PUT that breaks a ShapeConstraint is stored
  too, while the same documents are refused on the public plane. Waiting 65 s
  after the declarations were stored changes nothing, so the documented 60 s
  propagation of constraints does not explain it.
- **pagelike before:** WebDAV PUTs went through the whole pipeline (R-MOD-13,
  R-PROTO-112: "422 when the content does not satisfy the site's schema or
  constraints").
- **pagelike now:** `schema.Validate` returns at once for an authoring-plane
  PUT, so neither schemas nor shapes apply, nor (inferred) defaults, `@write`,
  uniqueness and cascades. WebDAV DELETE and MOVE were not probed and are
  unchanged.
- **Rationale:** the authoring plane is the site author's own credential (it
  already bypasses rules, triggers and transitions), so skipping validation
  there weakens no authorization boundary; it matches the pagelove-dev skill
  ("WebDAV success does not test … ShapeConstraint"). Consequence worth knowing:
  the shop demo's closed Order shape does not protect orders its worker writes
  over WebDAV, on PageLove or now on pagelike.

## Harness artifacts and harness notes

- The resolver case's input was an undocumented-dialect example (above).
- **Observation file names of `.live` siblings.** An observation file is the
  case id with dots turned into dashes, cut to 40 characters, so a long
  `<id>.live` lost its suffix and overwrote the file of `<id>` (run 3:
  `modeling-uniqueness-duplicate-across-doc.json` held the sibling's answer).
  `harness.ObservationName` now keeps `-live` for a `.live` sibling (the base
  cut to 35 characters: `modeling-uniqueness-duplicate-acros-live.json`);
  every other name is unchanged. Run 4 was made to record both files.
- The `write-envelopes` probe sleeps 65 s (`sleep_ms`) without being tagged
  `slow`: live runs here may not use `--slow`, and the wait is what rules out
  propagation as the explanation. It adds about 65 s to a full local run (it
  runs in parallel with others).
- Unit tests in `internal/schema` now write on the public plane under an
  allow-all rule for a dedicated principal, since authoring-plane PUTs are no
  longer validated.

## Consequential updates

Local cases that encoded the superseded behaviour were updated to it (none was
run live, as they are not in this area's sample):

| Case(s) | Now |
|---|---|
| `modeling.cardinality.selector-delete-only-required-value`, `selector-post-exceeds-max`, `selector-put-replacement-validated`, `move-checks-source-and-destination`; `modeling.references.removing-required-referenced-value-is-422` | Cardinality problems item |
| `modeling.computed.legacy-bare-sessel-read-is-computed` | A written value is accepted |
| `modeling.errors.shape-violation-document` | Exact ShapeConstraint item |
| `modeling.inheritance.unique-inherited`; every refusal in `uniqueness.yaml` | ConstraintViolation item, `[uniqueness]` |
| `modeling.nested.wrong-itemtype-is-type-mismatch`, `plain-value-is-type-mismatch`, `typed-items-validated-independently`, `schema-validate-self-is-nested-item` | Accepted (the ids predate the answer) |
| every 422 in `shapes.yaml` and `shapes-closed.yaml`; `modeling.shapes.delete-leaving-violation-is-409` | ShapeConstraint item; CascadeBlocked for the 409 |
| `modeling.shapes.webdav-write-checked` | The WebDAV PUT is stored (the id predates the answer) |
| `apps.shop.order-shape-dav` (apps area) | The WebDAV order carrying a script is stored |
| `reacting.tc.uniqueness-before-transition` (reacting area) | Asserts `[uniqueness]` / "Uniqueness violation" instead of the old description |

Unresolved references (422, `[reference] … (references(org)): Referenced value
does not exist for property 'org'`) and restrict (409 CascadeBlocked,
`[cascade-restrict] [itemprop='id'] (restrict(<T>#id)): Cannot delete: 1
document(s) reference this value via 'org'`) were switched to PageLove's items
too, from the passing cases `modeling.references.unresolved-rejected` and
`modeling.references.restrict-blocks-delete` of the same live run: they share
the "Constraint violation" renderer.

## Unresolved and deferred

- **Sessel dialect in resolvers (Sessel area).** PageLove has no `set_text`
  and no `.lowercase()`, and its `.text(x)` setter refuses a stored element;
  pagelike provides all three (R-SESSEL-113/241/242) because the modeling docs
  use them. Deciding needs the shared evaluator (`internal/sessel`), so it is
  left to the Sessel area. `modeling.probe-0929.resolver-forms`
  (`status: live-divergence`) measures it. `resolvers.yaml` still has cases
  using `set_text` (`write-runs-before-validation`, `chain-order`,
  `mixed-language-chain`) and `.lowercase()` (`recipe-slug-lowercase`); they
  would fail live for the same reason and were not run.
- **Whole-write SchemaViolation body.** PageLove answers a bare problems item
  (`<div itemscope itemtype="https://pagelove.org/SchemaViolation"><ul itemprop="problems">…`,
  one problem per violation, type failures worded "is not a valid integer").
  pagelike keeps its SchemaViolation page with `check` spans: cases assert only
  the item type, and how PageLove nests BindingFailure items (R-MOD-73) was not
  observed.
- **`@write` runtime failures.** Observed as 500 `dombase…/Internal` "resolver
  pipeline error: sessel binding threw: …"; pagelike keeps 500 SchemaViolation
  plus BindingFailure (R-MOD-52).
- **Not probed:** WebDAV DELETE/MOVE validation; type or enum failures of
  selector writes (they keep the SchemaViolation page); closed shapes under a
  409; composite duplicates inside one document; typed reads of computed
  properties (Sessel QUERY needs an identity, not live).

## Files outside the modeling area

- `internal/errdoc/errdoc.go`: `MediaType` serves a `ShapeProblems` error as
  `text/html` even when the refusing package supplies the `Document` (a
  problems item with several problems). No existing error had both.
- `harness/runner.go`: `ObservationName` (see Harness artifacts and harness
  notes).
- `docs/spec/protocol.md` R-PROTO-112: a one-line note that a dav `PUT` is no
  longer validated.
- `harness/cases/apps/shop.yaml` and `harness/cases/reacting/transition-constraint.yaml`:
  one step each (Consequential updates).
