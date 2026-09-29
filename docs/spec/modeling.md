# Modeling: schemas and validation (behavioral spec)

Status: draft. Snapshot of sources: 2026-09-28. Area code: `MOD`. Harness cases:
`harness/cases/modeling/*.yaml`.

This spec covers the PageLove schema system as pagelike must reproduce it:
where declarations live and how they are discovered, `Schema` and `Property`
items, inheritance, primitive and enum types, nested types, defaults,
uniqueness, primary keys, references and cascades, computed properties,
validators, `@read`/`@write` resolvers, `GroupConstraint` ("required
combinations"), `Method` declarations, `ShapeConstraint` (open and closed), which
writes are validated and in what order, and the 422/409 error documents.

It does not cover the Sessel or JavaScript runtimes themselves (only the
contract each binding slot has with the schema system), method *invocation*
during composition, or TransitionConstraint (reactions area). Those
dependencies are listed in §18.

## 0. Conventions

Each requirement has an id `R-MOD-<n>` and these fields:

- **Evidence**: `documented` (public docs), `client-source` (beta-js),
  `demo-source` (official apps and templates), `inferred` (reasoned; the reason
  is given). "pagelike decision" marks a choice made where PageLove behavior is
  unknown. These choices are meant to be revisited after live probing (§20).
- **Source**: doc page and section, or `repo/path@commit:line`. Doc pages are
  cited as `docs <path> §<section>`. Every doc citation refers to the
  2026-09-28 snapshot in `research/docs/2026-09-28/md/`.
- **Confidence**: high / medium / low.

Commits used: beta-js `c204746`, demo-apps `c4dd883`, pagelove-shop `d887054`,
pagelove-ats `8f200fc`, pagelove-polls `c9270e5`, pagelove-dev `b489923`,
pagelove-cursor `b97c3ef`.

**Combined vs individual pages.** The combined page
`docs.pagelove.com_all_reference_modeling-data.md` is identical, section by
section, to the eight individual `reference_modeling-data*` pages. The
`JavaScript in schemas`, `Schema definitions in HTML` and `Schema instances in
HTML` sections of `all_languages_javascript.md` are also identical to their
individual pages. No combined-vs-individual differences exist in this area.
One rendering defect exists in both copies: in the Property page §"Reading a
multi-valued property", the Liquid example is an empty code block (the docs
site's own Liquid processing appears to have consumed it). The surrounding
prose is intact.

Terminology used below:

- **Declaration**: a microdata item whose itemtype is one of the system
  vocabulary URLs in R-MOD-1.
- **Instance**: an element that has `itemscope` and an `itemtype` naming a
  registered schema (R-MOD-9).
- **Item scope** of an element: its microdata properties, meaning the
  `[itemprop]` descendants whose nearest `[itemscope]` ancestor is that
  element. A nested `[itemscope]` that carries `itemprop` is itself a property
  of the outer item. Its own descendants are not properties of the outer item.
- **Value** of a property element: its microdata value (R-MOD-19).
- **Affected instances / affected elements** of a write: defined in R-MOD-14
  and R-MOD-63.

---

## 1. Declarations and discovery

### R-MOD-1 — System vocabulary URLs

The following itemtype URLs have meaning to the schema system. They are compared
as exact, case-sensitive strings after trimming ASCII whitespace.

| URL | Role |
|---|---|
| `https://pagelove.org/Schema` | Schema declaration |
| `https://pagelove.org/Property` | Property declaration (under `itemprop="property"`) |
| `https://pagelove.org/Method` | Method declaration (under `itemprop="property"`) |
| `https://pagelove.org/Parameter` | Method parameter (under `itemprop="parameter"`) |
| `https://schema.host/GroupConstraint` | Group constraint (the itemtype is not checked; see R-MOD-54) |
| `https://schema.host/Enum` | Enum type declaration |
| `https://pagelove.org/ShapeConstraint` | Shape constraint |
| `https://pagelove.org/Sessel` | Sessel binding item (`source` holds the expression) |
| `https://pagelove.org/Sessel/Lambda` | Sessel binding item (a non-computed `@read` transformer; see R-MOD-44) |
| `https://pagelove.org/JavaScript/Module` | JavaScript binding item (`source` holds an ES module) |
| `https://pagelove.org/Instance` | Implicit root parent of user schemas |
| `https://pagelove.org/SchemaViolation` | 422 envelope for schema failures |
| `https://pagelove.org/ConstraintViolation`, `https://pagelove.org/Violation` | 422/409 envelope for shape, uniqueness and transition failures |
| `https://pagelove.org/BindingFailure` | Binding-evaluation failure item |
| `https://pagelove.org/HTTPResponse` | Thrown response object (validators, `@write`, triggers) |
| `https://schema.host/{Text,String,URL,Number,Integer,FloatingPoint,Boolean,DateTime,Date,Cardinal}` | Primitive types |

- Evidence: documented. Source: docs reference/modeling-data/Schema §Shape; Property §Shape, §Defaults;
  Methods §Declaration; GroupConstraint §Syntax; Types §Primitive types, §Enum types; ShapeConstraint §Shape;
  languages/javascript/server/javascript-in-schemas §Shape, §Errors. Confidence: high.
- Edge cases: `http://` variants of these URLs are different strings and are not recognized. The live
  416 probe used `http://pagelove.org/Error`, which only shows that envelope URLs are not uniform across
  error kinds. The schema system itself recognizes only the `https://` forms above.

### R-MOD-2 — Where declarations live

Any declaration (Schema, Enum, ShapeConstraint) may appear in **any HTML document on the host**, at
any depth, visible or `hidden`, and a document may mix declarations with ordinary content and data.
Declarations are **host-wide**. A schema declared in `/a/defs.html` governs instances in every
document of the host. There is no required location. Observed conventions include `/system/schemas/`
(mentioned once), per-feature `schema/*.html` directories, and a single `/schemas.html` or
`/constraints.html` at the site root.

Declarations inside `<template>` contents are not discovered, because microdata extraction does not
enter template contents (inferred, low). Declarations inside non-HTML resources (JSON, blobs) are not
discovered.

- Evidence: documented + demo-source. Sources: docs reference/permissions/Group ("Store the schema under
  `/system/schemas/`"); docs reference/modeling-data/Types §Enum types ("in any schema document");
  docs recipes/declaring-a-state-machine ("Store them in any document on the host");
  demo-apps/README.md@c4dd883:101 ("itemtype URLs register globally");
  pagelove-shop/README.md@d887054:42 ("Pagelove discovers these definitions from the deployed HTML");
  pagelove-ats/site/admin/auth.html@8f200fc:340-401 (global shapes declared in an admin page).
  Confidence: high (anywhere), low (`<template>` exclusion).
- pagelike: keep a per-site registry built from every stored `text/html` document. It is rebuilt
  incrementally on every committed write and cached by site write generation (plan.md §Read path).

### R-MOD-3 — Reading declaration fields

A declaration's fields are its microdata properties, read with the value rules of R-MOD-19. Authors
use `<meta itemprop=… content=…>`, `<code itemprop=…>text</code>`, `<span>`, `<td>`, `<strong>` and
similar interchangeably. For a field documented as `0..1`/`1..1`, the first value in document order is
used. Unknown fields are ignored. Field values are trimmed of leading and trailing ASCII whitespace
before use (inferred: `<code>` and `<td>` values in real apps carry incidental whitespace, e.g.
pagelove-ats `<code itemprop="selector">`).

- Evidence: documented (examples use meta and code interchangeably) + demo-source
  (pagelove-ats/site/admin/auth.html@8f200fc:342; pagelove-cursor SKILL.md@b97c3ef:141-150 uses
  `<span itemprop="selector">`). Confidence: high (forms), medium (trimming).

### R-MOD-4 — Registration timing

1. A document written through the **public plane** (PUT/POST/DELETE/MOVE, including selector-scoped
   writes and resource creation) updates the registry when the write commits. The **next** request sees
   the new declarations. "The schema is registered with the host's cache when the document containing it
   is PUT/POST'd."
2. The write that carries a declaration is **not** validated against that declaration. It is validated
   against the registry as it stood before the write. ("A constraint binds as soon as the document holding
   it is written ... the very next write validates against the new rules.")
3. Declarations written over **WebDAV** take effect in PageLove "within 60 seconds". This is documented
   for TransitionConstraint rules, and inferred for schemas and shapes, which live in the same host cache.
   Method Elements docs also say a schema not yet in the cache makes dispatch fail and advise to "PUT the
   document containing the schema once to register it".
4. **pagelike decision**: registration is immediate for both planes. The registry is part of the same
   committed generation as the write, which satisfies "within 60 seconds".
5. Harness consequence: modeling cases register their declarations in their **first step**, with an
   anonymous public-plane `PUT` of `${P}/schema.html` (or `shapes.html`). That makes them take effect at
   once on PageLove too. Only `modeling.discovery.webdav-authored-schema-within-60s` relies on WebDAV
   setup, and it waits 61 s (`requires: [slow]`). Seed documents that must bypass a declaration are placed
   in `site.files`, which is written before the declaration exists.

- Evidence: documented (1, 3 for transitions), inferred (2, 3 for schemas).
  Sources: docs reference/modeling-data/Methods §Declaration; docs reference/composing-pages/Method-Elements
  §Defining the method, §Error cases; docs reference/reacting-to-changes/TransitionConstraint §Taking effect;
  docs recipes/declaring-a-state-machine ("rules documents edited over WebDAV take effect within 60 seconds").
  Confidence: high (1), medium (2), medium (3).
- Edge cases: a write that both declares a schema and contains instances of it is accepted or rejected
  against the old registry, and the new schema applies from the next write (case
  `modeling.discovery.same-write-not-self-validated`).

### R-MOD-5 — Updating and removing declarations

Replacing or editing the declaring document replaces its declarations. Deleting the document (or the
declaration element) unregisters them. Existing stored instances are **never re-validated** when a schema
changes. They are checked again only when a later write affects them (R-MOD-14). ("No database
migrations. The document schema evolves with the document.")

- Evidence: inferred from R-MOD-4 and the blog. Source: blog "The shape of Pagelove" §(no migrations);
  docs reference/modeling-data/Methods §Declaration. Confidence: medium.

### R-MOD-6 — Duplicate and overriding declarations

- A host-local schema or enum with the same URL as a **platform (system) schema/enum** overrides it
  (documented: "Enums declared in your host's own schema documents override a platform enum with the same
  URL, the same way host-local schemas override system schemas").
- Two host-local declarations of the same schema `type` URL: PageLove behavior is unknown. **pagelike
  decision**: the declaration found first when documents are ordered by path (byte-wise ascending) and
  then by document order wins. The others are ignored, with a warning in the server log. (beta-js lets the
  last one on a page win, but that covers one page only: pagelove.mjs@c204746:266-304.)
- Two enums with the same URL: the same rule applies.
- Evidence: documented (first bullet); inferred (rest). Confidence: high / low.

### R-MOD-7 — Binding slots and language selection

Slots that hold code are `default`, `@read`, `@write`, `@validate` (property and schema level),
`@computed`, and a Method's `implementation`. A slot value takes one of these forms:

| Form | Language |
|---|---|
| `<script type="text/sessel" itemprop="SLOT">expr</script>` (a plain property, not an item) | Sessel; the value is the element text |
| `<div itemprop="SLOT" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source">expr</script></div>` | Sessel |
| same with `itemtype="https://pagelove.org/Sessel/Lambda"` | Sessel (see R-MOD-44 for `@read`) |
| same with `itemtype="https://pagelove.org/JavaScript/Module"` | JavaScript ES module; default export must be a function |
| a typed item with any other itemtype | `unknown-language` binding failure when evaluated |
| `<meta itemprop="default" content="…">` (default slot only) | static literal (R-MOD-27) |

The language is chosen **by the wrapper itemtype, never by `<script type>`**.

- Evidence: documented. Source: docs reference/modeling-data/Schema §Schema-level @validate; Property
  §Defaults, §Computed properties; Resolvers §Shape; javascript-in-schemas §Shape, §Errors.
  Confidence: high (forms), medium (bare `<script>` defaults to Sessel even if `type` is not
  `text/sessel`: inferred).

---

## 2. Schema items and inheritance

### R-MOD-8 — Schema item

An item with itemtype `https://pagelove.org/Schema` and these fields:

| Field | Card. | Meaning |
|---|---|---|
| `type` | 1..1 | Governed itemtype URL. **A schema with no (or empty) `type` is silently skipped.** |
| `name` | 0..1 | Human name, used in messages and tooling |
| `description` | 0..1 | Text |
| `parent` | 0..1 | Governed URL of the parent schema |
| `property` | 0..n | Nested Property or Method items (R-MOD-17, R-MOD-58) |
| `constraint` | 0..n | Nested group constraints (R-MOD-54) |
| `@validate` | 0..1 | Schema-level validator (R-MOD-46) |

Property items may be wrapped in non-item containers such as `<ul>`, and that has no effect.

- Evidence: documented. Source: docs reference/modeling-data/Schema §Shape, §Fields, §Error cases;
  Methods §Declaration (property items inside `<ul>`). Confidence: high.

### R-MOD-9 — Governed-type matching

An element is an **instance of schema S** when it has `itemscope` and its `itemtype` value, trimmed,
equals S's `type` exactly (case-sensitive). Instances may be top-level items or nested items. Items
whose itemtype names no registered schema are stored **unchanged, without any checks**. The same holds
for undeclared properties on governed items (R-MOD-17).

- Multiple whitespace-separated itemtype tokens: PageLove behavior is unknown. **pagelike decision**:
  compare the whole trimmed attribute value, so a multi-token itemtype matches no schema. This mirrors
  exact-match usage in beta-js (`pagelove.mjs@c204746:256`) and in shape examples. Probe P-MOD-5.
- Evidence: documented (no-schema pass-through). Source: docs reference/modeling-data/Schema §When to
  reach for it. Confidence: high / low (multi-token).

### R-MOD-10 — Parent chain

1. `parent` names another schema's governed URL. The chain is walked from root to leaf.
2. When `parent` is omitted, the schema's parent is implicitly `https://pagelove.org/Instance`, a system
   schema that declares no user-visible properties (inferred).
3. An **unknown parent URL is not an error**: the chain silently stops at the last resolvable ancestor.
4. A **cycle** is an error. Every write that affects an instance of any schema in the cycle fails with
   **422** (SchemaViolation, `check` = `schema`, pagelike decision for the check name).
5. Inheritance also defines the subtype relation used by references (R-MOD-38), nested types (R-MOD-24),
   Sessel `search(…, {"isa": true})` and the `:isa()` selector (cross-area).

- Evidence: documented. Source: docs reference/modeling-data/Schema §Inheritance, §Error cases.
  Confidence: high.
- Contradiction: beta-js throws on an unknown parent (`Schema "<child>" declares unknown parent
  "<parent>"`) and on cycles (pagelove.mjs@c204746:316,322). That is client-side behavior of the served
  library and does not change server semantics (§19 C12).

### R-MOD-11 — How inherited members combine

Let the chain be `[root, …, leaf]`.

| Member | Rule | Evidence |
|---|---|---|
| Properties | Union by `name`. For a name declared at several levels, the **most-derived declaration** supplies the scalar fields (`type`, `cardinality`, `default`, `references`, `cascade`, `@key`, `@validate`, `@computed`, `description`). A field the child omits is **not** inherited from the parent (whole-declaration override). | documented ("Child declarations override parent declarations by `name`"); whole-override is inferred, low. Probe P-MOD-39 |
| `unique` | Union across the chain: if any level marks the name `unique` (or gives it a composite group), it applies to the leaf. | documented |
| `group` membership | Union across the chain for each property name. | documented (GroupConstraint §Inheritance) |
| Group constraints | Keyed by group name. The most-derived constraint for a group wins, and parent constraints apply unless the child declares the same group. | documented |
| `@write` resolvers | Every level that declares one for the name runs, **child-first** (leaf → root). | documented |
| `@read` resolvers | Every level runs, **ancestor-first** (root → leaf). | documented |
| Schema-level `@validate` | Every level's schema-level validator runs, **ancestor-first**, and all must return truthy. | documented (Schema §Inheritance) |
| Property-level `@validate` | **Most-derived only**. There is no chaining. | documented (Property §Validators, JS §Chaining order) |
| `default` | Most-derived default for the name wins. | documented |
| `@key` | The schema's own properties first (document order, first wins), then the parent's, and so on (R-MOD-33). | documented |
| Methods | By name, most-derived wins (a subtype group can override `includes()`). | documented (permissions/Group §Custom membership) |

- Contradiction: Schema §Inheritance says "`@validate` expressions: every validator in the chain runs"
  while Property/JS say "most-derived wins, no chaining". Resolution (§19 C3): the first applies to
  **schema-level** validators and the second to **property-level** ones.

### R-MOD-12 — Subtype relation

`A isa B` holds when A = B or B appears in A's parent chain. Only declared ancestry counts. Two unrelated
types with the same property values are unrelated.

- Evidence: documented. Source: docs reference/modeling-data/Property §References and cascade ("Subtypes
  count"); Sessel §Typed search. Confidence: high.

---

## 3. When validation runs

### R-MOD-13 — Which operations are validated

Schema validation, uniqueness, references and shapes apply to every **mutating** operation:

| Operation | Validated |
|---|---|
| Public `PUT` (whole document, including create; selector replace) | yes |
| Public `POST` (selector append; resource creation from templates) | yes |
| Public `DELETE` with selector | yes (the remaining document) |
| Public whole-document `DELETE` | no schema/shape check. Only restrict/cascade (R-MOD-39..43) |
| Public `MOVE` (element) | yes, at source and destination (cardinality explicitly documented) |
| Public whole-document `MOVE` | content unchanged, so no schema checks. pagelike re-checks shapes whose `resource` matches the destination path. Referenced values do not disappear, so no referential action applies (inferred, low) |
| WebDAV `PUT` (authoring plane) | **yes**: schemas and shapes (not TransitionConstraints) |
| `Pagelove.PUT`/`Pagelove.DELETE` from triggers/processors | yes ("goes through the normal request pipeline, including authorization checks and schema validation") |
| Sessel `new Type { … }` / JS method returning a typed instance | defaults and validation at construction (Sessel/JS areas) |
| `GET`, `HEAD`, `OPTIONS`, `QUERY` | no validation. `@read` resolvers apply to representations (R-MOD-50) |

- Evidence: documented + demo-source. Sources: docs reference/modeling-data/ShapeConstraint §How shape
  constraints run ("POST, PUT, DELETE"); docs reference/reading-and-writing (PUT/POST/DELETE/MOVE error
  tables list 422 "Schema validation fails"; MOVE §When to reach for it); docs reference/protocol/WebDAV
  §When something goes wrong ("422 when the content does not satisfy the site's schema or constraints";
  refusals from "the shared pipeline" are wrapped under `detail`); pagelove-shop/site/constraints.html@d887054:5-14
  (orders written over WebDAV by `worker/src/index.js:250-256` were refused 422 by a closed shape);
  docs reference/reacting-to-changes/Trigger §Side-effect writes; javascript-in-schemas §Method bodies.
  Confidence: high (public plane), medium (WebDAV), low (whole-document MOVE).
- Contradiction: pagelove-dev SKILL.md@b489923:213-215 says "WebDAV success does not test application
  AuthorizationRule, ShapeConstraint, Trigger, or Processor behavior", and TransitionConstraint says
  "WebDAV authoring writes are never transition-validated". Decision (§19 C14): WebDAV writes bypass
  AuthorizationRules (the author key), Triggers, Processors and TransitionConstraints, but not schemas or
  shapes.
- **live 2026-09-29 (superseded, adopt-live):** a WebDAV `PUT` is stored with no schema or shape check,
  even 65 s after the declarations were stored; pagelike skips the whole pipeline for it (defaults,
  `@write`, uniqueness and cascades included, inferred). WebDAV DELETE/MOVE were not probed and are
  unchanged (docs/compat/decisions-2026-09-29/modeling.md).

### R-MOD-14 — Affected instances

Instance-level processing (R-MOD-15 steps 1–8) runs only on the **affected instances** of a write, computed on the
proposed post-write DOM D′:

| Write | Affected instances |
|---|---|
| Whole-document write (public PUT without Range, WebDAV PUT, `Pagelove.PUT`, resource creation) | every instance in D′ |
| Selector `PUT` replacing the first match M with fragment F | every instance inside F (inclusive), plus every instance that is an ancestor of F's position |
| Selector `POST` appending F into M | every instance inside F, plus M if M is an instance, plus instance ancestors of M |
| Selector `DELETE` of M | instance ancestors of M's former position (evaluated after removal) |
| Element `MOVE` | instance ancestors of the source position (after removal), instance ancestors of the destination, and every instance inside the moved subtree |

Instances outside the affected set are not re-checked, even if they are already invalid.
Nested instances are validated independently against their own schema, whether or not they are
property values of the outer item. For example, OrderLine `<li>` items in a plain `<ul itemprop="lines">`
are validated by the OrderLine schema.

- Evidence: inferred from the MOVE doc ("checked at both the source ancestor (after removal) and the
  destination ancestor"), the cascade section ("Removing the only value of a `1..1` property ... is
  rejected with 422") and demo-source pagelove-shop/site/schemas.html@d887054:249-256 ("each nested
  OrderLine is validated independently by the OrderLine schema above"). Also docs
  reference/permissions/AuthorizationRule ("A write changes only the first match").
  Confidence: medium (overall), low (pre-existing invalid siblings are not re-checked: probe P-MOD-14).
- **live 2026-09-29 (superseded, adopt-live):** only outermost items are instances. An item inside
  another item's markup is not validated, whether it is a property value, a plain nested item, or inside
  an item without a schema (so the shop's OrderLine items are not checked).

### R-MOD-15 — Write pipeline order (normative for pagelike)

For one request, after routing, authorization and Triggers (other areas), and after the mutation has
been applied in memory to produce D′:

1. **Computed-property guard**: an affected instance that carries a value element for a computed
   property fails with 422, `check` `computed` (R-MOD-44). (live 2026-09-29: removed; the value is
   checked in stage 4 like any other.)
2. **Defaults**: for each affected instance and each declared property with no value element, inject the
   effective default (static, dynamic, or the `@key` auto default) (R-MOD-27..29, R-MOD-35).
3. **`@write` chains** (child-first per property) on properties with at least one value (R-MOD-49).
4. **Structural checks**: cardinality, type and enum on every affected instance, including recursion
   into nested typed values. **All** violations from this stage, across all affected instances, are
   collected into one 422.
5. **Property-level `@validate`** (most-derived) for properties with at least one value. Failures are
   collected into one 422.
6. **Schema-level `@validate`** (every level, ancestor-first). 422.
7. **Group constraints**. 422.
8. **Host-wide checks** against the committed state plus this write: reference integrity (422),
   individual and composite uniqueness (422 ConstraintViolation).
9. **ShapeConstraints** on the final D′ (422, or 409 for DELETE).
10. TransitionConstraints (reactions area).
11. **Referential actions** for referenced values that disappear or change: `restrict` gives 409; cascades
    add deletions and rewrites to the same transaction and are applied recursively (R-MOD-39..43).
12. Commit everything atomically (R-MOD-16).

A stage runs only if every earlier stage passed. The first failing stage determines the response.

- Evidence: steps 3→4 documented ("`@write` runs before validation. The data that validation sees is the
  post-transform result"). Steps 4→5 documented (Types §How type validation runs), 5→6 documented
  (Schema §Schema-level @validate), 6→7 documented (Schema; GroupConstraint §Error cases). "Uniqueness is
  enforced atomically with the write" is documented. The positions of 1, 2, 8, 9 and 11 are
  inferred (pagelike decision) and cross-stage barriers are inferred.
  Confidence: high (documented pairs), low (the rest).
- Contradictions: see §19 C1 (group vs schema-level `@validate`) and C2 (property `@validate` vs
  required-property check). The order of defaults vs `@write` is undocumented (open question P-MOD-16).

### R-MOD-16 — Atomicity

A rejected write changes nothing: no document is modified, no cascade is applied, no SSE event is
emitted, and the stored ETag is unchanged. "No partial modification is applied on failure." An accepted
write commits the document change, all cascade effects, and their events in one transaction.

- Evidence: documented (ShapeConstraint §Error cases; composite-uniqueness recipe "There is no window in
  which a duplicate can slip through"; build-a-blog "the write never happens"). Confidence: high.

---

## 4. Property

### R-MOD-17 — Property item

Items under `itemprop="property"` of a Schema. An item whose itemtype is `https://pagelove.org/Method`
is a Method (R-MOD-58). **Any other itemtype is treated as a Property** (pagelike decision, low). This
mirrors the "itemtype is not checked" rule for group constraints. beta-js instead accepts only
`https://pagelove.org/Property` and its declared subtypes (pagelove.mjs@c204746:262-277).

| Field | Card. | Meaning |
|---|---|---|
| `name` | 1..1 | `itemprop` name on data items. **No or empty name: the property is silently skipped.** |
| `type` | 0..1 | Primitive, enum, or schema URL. Omitted: no type validation |
| `cardinality` | 0..1 | `0..1`, `1..1`, `0..n`, `1..n`. Default `0..n` |
| `description` | 0..1 | Text |
| `default` | 0..1 | Static text or dynamic binding (R-MOD-27..28) |
| `unique` | 0..n | `"true"`, or a composite group name (R-MOD-30..31) |
| `references` | 0..1 | `{itemtype}#{itemprop}` (R-MOD-37) |
| `cascade` | 0..1 | `"true"`, `"restrict"`, `"false"`/omitted (R-MOD-39) |
| `group` | 0..n | Group-constraint membership (R-MOD-55) |
| `@validate`, `@write`, `@read`, `@computed` | 0..1 each | Bindings |
| `@key` | 0..1 | `"true"`/`"false"` (R-MOD-33) |

- An `itemprop` on a governed item that no Property declares is **ignored by validation**.
- Property-name matching: a property element is any element in the item scope whose `itemprop`
  attribute, split on ASCII whitespace, contains the name. This follows microdata token semantics. The docs
  write `[itemprop="<name>"]` loosely. pagelike decision, low. Probe P-MOD-6.
- Duplicate declarations of one name in one schema: the first in document order wins (pagelike decision,
  mirroring the documented `@key` rule "first wins on duplicates").
- Evidence: documented. Source: docs reference/modeling-data/Property §Fields, §When to reach for it,
  §Error cases. Confidence: high.

### R-MOD-18 — Cardinality

| Value | Allowed count of value elements |
|---|---|
| `0..n` (default when omitted) | any |
| `0..1` | 0 or 1 |
| `1..1` | exactly 1 |
| `1..n` | ≥ 1 |

The count is taken over the item scope and **excludes anything inside nested `[itemscope]`
boundaries** (a nested item that is itself the property value counts as one). A present element with an
empty value **counts** as present: absent and empty are different.
Violation: 422 SchemaViolation, `check` `cardinality`.

- An unrecognized cardinality string (for example `2..5` or `1..*`) is treated as omitted (`0..n`). This
  is a pagelike decision. `Cardinal` is documented as "used internally by schema definitions", so PageLove
  may reject such a schema document instead. Probe P-MOD-38.
- Evidence: documented. Source: docs reference/modeling-data/Property §Cardinality values, §Error cases;
  Types §Primitive types (Cardinal). Confidence: high.

### R-MOD-19 — Values used for validation

The value of a property element follows the WHATWG microdata rules as PageLove documents them for
`:value-equals`:

| Element | Value |
|---|---|
| `meta` | `content` attribute |
| `audio`, `embed`, `iframe`, `img`, `source`, `track`, `video` | `src` attribute (raw, not resolved) |
| `a`, `area`, `link` | `href` attribute (raw, not resolved) |
| `object` | `data` attribute |
| `data`, `meter` | `value` attribute |
| `time` | `datetime` attribute if present, else text content |
| element with `itemscope` | an item (not a string) |
| anything else | descendant text content |

Values are used **verbatim**: attribute values and text content are not trimmed. An element that should
carry a value attribute but lacks it yields `""` (for example an `<a>` with no `href`). The exception is
`<time>`, which falls back to its text. A text-content element whose text is empty yields **null**, and
"empty text is treated as absent" for uniqueness and reference comparisons. The Sessel Element reference
says `.value()` "matches the value used by uniqueness and reference constraints". pagelike uses the same
extraction for type checks (inferred). For cardinality, an element with empty text still counts as
present (pagelike decision, low. Probe P-MOD-7).

- Evidence: documented + demo-source. Source: docs reference/composing-pages/Selector-Extensions §Text
  content vs microdata values; docs languages/sessel/reference/element §`.value()` (verbatim, empty text
  is null, same value as the uniqueness and reference constraints);
  demo-apps/demo-02-show-and-tell/index.html@c4dd883:64
  (`<time itemprop="createdAt" datetime="2026-08-18T10:00:00Z">Aug 18</time>` under a `DateTime` 1..1
  schema, working live); pagelove-shop/site/schemas.html@d887054:92-94 (origin-relative paths fail `URL`).
  Confidence: high (table, verbatim), medium (type checks use the same extraction).
- Cross-area note: the modeling docs' resolver examples mutate stored elements with `el.set_text(…)` and
  `el.set_attr(…)`, while the Sessel Element reference documents setters as `.text(expr)`, `.value(expr)`
  and `.attr(name, expr)` "on constructed elements only". The Sessel area must accept the
  `set_text`/`set_attr` forms inside `@write`/`@read` pipelines, because they are the documented
  resolver examples.
- Contradiction: Types §How type validation runs says "for elements with a `content` attribute ... the
  `content` value is checked; for other elements, the text content is checked". Taken literally, that
  rejects the demo's `<time>` values. Decision (§19 C9): use microdata values.

### R-MOD-20 — Reading multi-valued properties (cross-area)

When a property is read from a **typed item** (Sessel instance access, JS instance, Liquid/Resource-binding
typed rendering), a property whose schema **spells out** `0..n` or `1..n` reads as a **list**: absent
gives `[]`, one value gives a one-element list. A property with `0..1`, `1..1` or **no** cardinality
reads as a plain value, even though omitted means `0..n` for validation. Elements returned by plain
selector queries carry no schema and are not coerced. This rule is implemented by the composition,
Liquid and Sessel areas using this area's registry.

- Evidence: documented. Source: docs reference/modeling-data/Property §Reading a multi-valued property.
  Confidence: high.

---

## 5. Types

### R-MOD-21 — Primitive types (exact grammars)

Each value element's value (R-MOD-19) is checked. Violations: 422, `check` `type`.

| URL | Rule (pagelike grammar) |
|---|---|
| `https://schema.host/Text`, `…/String` | any string, including `""` and markup-like text |
| `https://schema.host/URL` | Non-empty, begins with an RFC 3986 scheme `ALPHA *( ALPHA / DIGIT / "+" / "-" / "." )` followed by `:`, and the whole string parses as an absolute URL. Accepts `https://example.com`, `mailto:a@b.com`. Rejects `not-a-url`, `""`, `/images/x.webp`, `//example.com`. |
| `https://schema.host/Number`, `…/FloatingPoint` | f64 syntax: `[+-]?(digits[.digits?]|.digits)([eE][+-]?digits)?`, or case-insensitive `inf`, `infinity`, `nan` with an optional sign. No whitespace, no hex, no `_`. |
| `https://schema.host/Integer` | `[+-]?[0-9]+` within the signed 64-bit range. `3.14`, `abc`, `""` and overflow are rejected. |
| `https://schema.host/Boolean` | exactly `true` or `false` (lowercase) |
| `https://schema.host/DateTime` | RFC 3339 `date-time`: `YYYY-MM-DDTHH:MM:SS[.frac](Z|±HH:MM)`. Date-only is rejected. |
| `https://schema.host/Date` | exactly `YYYY-MM-DD` (4-2-2 digits), calendar-valid (`2023-02-29` rejected, `2024-02-29` accepted). Datetimes are rejected. |
| `https://schema.host/Cardinal` | exactly one of `0..1`, `1..1`, `0..n`, `1..n` |

A nested item (an element with `itemscope`) used as the value of a primitive-typed property is a type
violation (pagelike decision, low. Probe P-MOD-13).

- Evidence: documented (rules and examples); the exact grammars are inferred from the "f64"/"i64"/RFC
  wording, which suggests a Rust implementation. Source: docs reference/modeling-data/Types §Primitive
  types, §Validation examples. Confidence: high for the documented examples, low for edge grammar (probes
  P-MOD-8..12).
- Implementation note: Go's `strconv.ParseFloat` accepts hex floats and `Inf` spellings, and
  `time.Parse(time.RFC3339)` differs from Rust `chrono` on lowercase `t`/`z`. Pre-validate with explicit
  grammars.

### R-MOD-22 — Absent vs empty vs whitespace

- Absent: only cardinality applies.
- Present and empty (`content=""` or empty text): counts toward cardinality and is type-checked. A
  null value (empty text content, R-MOD-19) is type-checked as `""`. `""` passes Text and fails every
  other primitive type and every enum that does not list `""`.
- Whitespace is never trimmed, so `" 42"` fails Integer (R-MOD-19).
- Evidence: documented (Types §Validation examples list `""` as invalid for URL/Number/Integer/Boolean).
  Confidence: high.

### R-MOD-23 — Enum types

Declared anywhere on the host as an item with itemtype `https://schema.host/Enum`:

```html
<div hidden itemscope itemtype="https://schema.host/Enum">
  <meta itemprop="name" content="https://example.com/Status">   <!-- or itemprop="type" -->
  <meta itemprop="value" content="active">
  <meta itemprop="value" content="retired">
</div>
```

- The identifying URL comes from `name` or `type` (both are accepted; if both are present, `type` wins,
  as a pagelike decision).
- A property whose `type` is the enum URL accepts only values that exactly equal (case-sensitive) one
  listed `value`. An enum with no values rejects everything.
- Violation: 422, `check` `enum`. The message names the offending value and the permitted list, in this
  documented form (shown wrapped):
  `[https://example.com/Gadget].status: Value "sideways" is not a valid https://example.com/Status (expected one of: "active", "retired")`.
- Host enums override platform enums of the same URL (R-MOD-6).
- Evidence: documented + demo-source. Source: docs reference/modeling-data/Types §Enum types, §Error
  cases; pagelove-shop/site/schemas.html@d887054:8-30. Confidence: high.

### R-MOD-24 — Nested schema types

When `type` is a registered schema URL T, each value element must be a nested item (`itemscope`) whose
itemtype isa T (pagelike accepts declared subtypes, inferred low). The nested item is validated
recursively against its own schema, including cardinality, type, `@validate` and groups. For a nested
schema-level `@validate`, `self` is the nested item. A plain (non-item) value, or an item of an
unrelated itemtype, is a type violation (`check` `type`).

- Evidence: documented. Source: docs reference/modeling-data/Types §Nested schema types, §Error cases;
  Property §Type. Confidence: high (subtype acceptance: low).
- **live 2026-09-29 (superseded, adopt-live):** PageLove checks neither the value of a schema-typed
  property (plain text and items of other types are accepted) nor the nested item's own properties;
  pagelike does the same (docs/compat/decisions-2026-09-29/modeling.md).

### R-MOD-25 — Unknown and omitted types

A `type` URL that is not a primitive, not a declared enum and not a registered schema is **silently
ignored**: no validation, any value accepted. This allows forward references. An omitted `type` also
means no type validation. As a consequence, the client-side URLs `https://pagelove.org/Text`,
`…/DateTime` and so on (used by pagelove.mjs) are unknown to the server and are not validated.

- Evidence: documented. Source: docs reference/modeling-data/Types §Unknown types; client-side usage
  in docs languages/javascript/client/schema-definitions-in-html §Examples. Confidence: high.

### R-MOD-26 — One report for shape errors

Cardinality, type and enum violations from all affected instances are reported together in one 422 "so
the reader can fix all shape errors in one pass".

- Evidence: documented. Source: docs reference/modeling-data/Types §How type validation runs.
  Confidence: high.

---

## 6. Defaults

### R-MOD-27 — Static default

`<meta itemprop="default" content="draft">`. When the property has **no value element** in an affected
instance, the server injects `<meta itemprop="<name>" content="<default>">` as the **last child** of
the instance element (the placement is a pagelike decision). A present-but-empty value is not absent, so
no default is injected. Injection happens before validation, so a default can satisfy `1..1`. The most
derived default wins. The injected element is stored and returned by later GETs.

- Evidence: documented (form, "injected ... when the property is absent on a write", child overrides
  parent). Source: docs reference/modeling-data/Property §Defaults. Confidence: high (form), medium
  (placement, empty-not-absent).

### R-MOD-28 — Dynamic default

- Sessel: `<div itemprop="default" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source" type="text/sessel">String.random(8)</script></div>`.
  It is a bare expression and `self` is unavailable.
- JavaScript: the default export is a function. `this` is a read-only plain-object view of the in-progress
  instance, holding the schema-declared fields already set; mutations to it are discarded. The first
  argument `context` currently has `document_html` (the serialized document being written) and may gain
  keys. A mutable `document` global views the in-progress element, and its changes are kept. A default may
  not read another property whose default is being computed in the same creation (it reads `undefined`).
- Result conversion: a scalar is stringified and injected as a `meta` (R-MOD-27). An element or instance
  is injected as the property element, with `itemprop=<name>` added (inferred).
- `Math.random()`/`Date.now()` are permitted, and the server's value is what gets stored.
- Evidence: documented. Source: docs reference/modeling-data/Property §Defaults; javascript-in-schemas
  §`this` and `context` for `default`, §Examples. Confidence: high (contract), low (element results).

### R-MOD-29 — Default errors

A dynamic default that fails at write time is a "write error on the affected item". **pagelike
decision**: 422 SchemaViolation, `check` `default`, carrying a nested `BindingFailure` (R-MOD-73).

- Evidence: documented (error), inferred (status). Source: docs reference/modeling-data/Property §Error
  cases. Confidence: low. Probe P-MOD-26.

---

## 7. Uniqueness

### R-MOD-30 — Individual uniqueness

`<meta itemprop="unique" content="true">`: no two instances **of the same governed type** on the host
may share a value for the property. The comparison is across all documents and within the same document.

- Scope is per exact itemtype. Uniqueness is "scoped per host and per type, so two different types can
  share a value without conflict". An inherited `unique` applies to the leaf type. pagelike compares
  instances whose itemtype equals the leaf type exactly, so subtype instances are not compared with
  supertype instances (inferred, low. Probe P-MOD-19).
- Values are microdata values (R-MOD-19) compared as exact, verbatim strings after `@write`. For
  multi-valued properties, any shared value between two different instances is a conflict. Duplicate
  values inside one instance are not a uniqueness conflict (pagelike decision). A text-content element
  with empty text has a null value and never conflicts ("empty text is treated as absent", Sessel Element
  reference). A `content=""` attribute value is the string `""` and is compared like any other value
  (pagelike decision, low).
- Only values held by **affected instances** are checked, against all other instances host-wide
  (committed state plus the rest of this write). Re-writing an instance with its own unchanged value is not
  a conflict. Deleting an instance frees its values for reuse.
- Enforcement is atomic with the commit: concurrent duplicates cannot both succeed.
- Violation: **422**, ConstraintViolation envelope (R-MOD-72). The body contains the word "uniqueness"
  (demo apps match `/uniqueness/i` on the 422 text).
- Evidence: documented + demo-source. Sources: docs reference/modeling-data/Property §Uniqueness;
  docs languages/sessel/reference/element §`.value()` (the value used by uniqueness and reference constraints);
  recipes/linking-related-data §Unique constraints, §When to use; recipes/composite-uniqueness;
  demo-apps/demo-01-event/schema/event.html@c4dd883:35-45 and app.js:54 (duplicate RSVP in the same
  list, 422 matching /uniqueness/i); demo-03 app.js@c4dd883:105-137 (claim, release by selector DELETE,
  re-claim); docs TransitionConstraint §The 422 body ("the same shape as uniqueness violations").
  Confidence: high (core), medium (envelope), low (subtype scope, empties).

### R-MOD-31 — Composite uniqueness

A `unique` value other than `"true"` is a **group name**. The properties of a schema sharing the group
name form a composite key: the **tuple** of their values must be unique among instances of the type,
while individual values may repeat. A property may carry both `true` and a group, and several groups.

- pagelike decisions: groups are per schema (after inheritance). An instance missing a member value
  does not participate in that group's check (SQL NULL semantics). For multi-valued members, each
  combination is compared (low confidence).
- Worked example (documented): (alice, acme) accepted, (alice, globex) accepted, (bob, acme) accepted,
  a second (alice, acme) rejected with 422.
- Evidence: documented. Source: docs reference/modeling-data/Property §Uniqueness;
  recipes/composite-uniqueness §A worked example. Confidence: high (documented example), low (absent
  members).
- Contradiction: demo-apps schemas avoid composite groups because they were "verified broken on this
  host" (demo-04-accountability/schema/checkin.html@c4dd883:6-8; demo-02 reaction.html:6-7), while the
  docs and pagelove-dev SKILL.md@b489923:289-291 describe them as supported. Decision (§19 C15):
  implement per docs. The live case `modeling.uniqueness.composite-recipe` settles it.

### R-MOD-32 — Uniqueness during cascades and selector writes

Uniqueness is checked for instances inside a selector `POST`/`PUT` fragment exactly as for whole-document
writes. A `POST` that appends a duplicate into a list fails with 422 and appends nothing.

- Evidence: demo-source (demo-01/02/03/04 all append via selector POST). Confidence: high.

---

## 8. Primary key (`@key`)

### R-MOD-33 — When `@key` is honoured

| Condition | Behaviour |
|---|---|
| `@key: "true"` and `unique` contains `"true"` on the same property | honoured |
| `@key: "true"` without individual `unique: "true"` (including composite-only) | ignored (the schema loads fine) |
| several honoured `@key` on one schema | document order: the first wins |
| no own `@key`, parent has one | the parent's applies |
| none in the chain | no key |

- Evidence: documented. Source: docs reference/modeling-data/Property §Primary key §Requirements,
  §Inheritance, §Error cases. Confidence: high.

### R-MOD-34 — `id` emission

When an instance is **materialised via `Pagelove.PUT`** (Sessel), the key value is written as `id` on the
instance's root element, overwriting any existing `id`. If the value is empty, contains whitespace, or
the property is computed, no `id` is emitted and the PUT still proceeds. Plain HTTP PUT/POST bodies are
stored as sent, and the server does not add `id`.

- Example (documented): `new Host { hid: "abc123", hostname: "abc123.example.com" }` then
  `Pagelove.PUT(h, "/hosts/abc123.html")` stores `<div id="abc123" itemscope itemtype="https://example.com/Host">…`.
- Evidence: documented. Source: docs reference/modeling-data/Property §Primary key §Value rules.
  Confidence: high (Pagelove.PUT), medium (HTTP writes not affected).

### R-MOD-35 — Auto-generated key default

An honoured `@key` property with no explicit `default` gets an implicit default equivalent to the Sessel
expression `String.random(1, { lower: true }) + String.random(7, { lower: true, digits: true })`. That
is 8 characters matching `^[a-z][a-z0-9]{7}$`. Like any default (R-MOD-27), it is injected when the
property is absent, including on HTTP writes (inferred) and at `new Type {}` construction (documented).
An explicit `default`, or an explicit value, overrides it. Computed properties never get it.

- Evidence: documented. Source: docs reference/modeling-data/Property §Auto-generated default.
  Confidence: high (construction), medium (HTTP writes).

### R-MOD-36 — Key and identity (cross-area)

TransitionConstraint pairs old and new items by the `@key` value in whole-document writes, and returns
422 when two keyless items of one type are ambiguous. This area provides the key resolution.

- Evidence: documented. Source: docs reference/reacting-to-changes/TransitionConstraint §Item identity and
  `@key`. Confidence: high.

---

## 9. References and cascades

### R-MOD-37 — `references` declaration

Format: `{itemtype}#{itemprop}`, split at the **last** `#` (pagelike decision). The target property must
be declared individually `unique: "true"` on the target type, either in the target schema's own chain or
inherited.

- A malformed value (no `#`, empty type or empty property), or a target that is not `unique: true`, is a
  **schema load error**: every write that affects an instance of the *referencing* type is rejected. The
  **pagelike decision** is 422 SchemaViolation, `check` `schema`. The status is undocumented; the WebDAV
  page uses 500 for "could not load the site's schema" (§19 C16).
- `cascade` without `references` is ignored.
- Evidence: documented. Source: docs reference/modeling-data/Property §Fields, §Error cases.
  Confidence: high (load error), low (status).

### R-MOD-38 — Reference integrity on write

Every value of a referencing property on an affected instance must equal the target property's value on
**some instance whose type isa the target type** (declared descendants count, unrelated types do not),
in the committed state plus this write. Otherwise: 422 SchemaViolation, `check` `references`, with a
message naming the unresolved value (pagelike decision for the check name. The recipe says "The error
describes which reference was unresolved").

- Evidence: documented. Source: docs reference/modeling-data/Property §References and cascade;
  recipes/linking-related-data §Referential integrity. Confidence: high (rule), low (body).

### R-MOD-39 — `cascade` values and upgrade

| `cascade` | Effect when a referenced value disappears/changes |
|---|---|
| `"true"` | cascade (R-MOD-41, R-MOD-42) |
| `"restrict"` | block: **409 Conflict** |
| omitted / `"false"` | nothing. Referrers keep a dangling value, which is only checked again when a referrer is next written |

If several referencing properties (across types) point at the same target property and **any** uses
`restrict`, all of them behave as `restrict` for that target.

- Evidence: documented. Source: docs reference/modeling-data/Property §References and cascade.
  Confidence: high.

### R-MOD-40 — What triggers referential actions

Only **schema-valid** operations: `DELETE` of the document holding the referenced value, or a write that
removes or changes the value while leaving its own document schema-valid. Removing the only value of a
`1..1` referenced property is rejected with **422 before** any cascade is considered. `0..1`/`0..n`
referenced values may be removed by selector DELETE, and that triggers the action.

- Evidence: documented. Source: docs reference/modeling-data/Property §What triggers a cascade.
  Confidence: high.

### R-MOD-41 — Cascade disposition of referrers (on disappearance)

| Referencing property's cardinality | Action |
|---|---|
| `1..1` | the **referencing document** is deleted |
| `0..1` / `0..n` | the referencing property element is removed, and the document survives |
| `1..n` | the matching value element is removed. If it was the last, the document is deleted |

Cascades apply recursively: a deleted referrer can itself be a referenced target. pagelike guards cycles
by visiting each document once. Every cascade effect commits in the same transaction and emits the usual
events.

- Evidence: documented (table). Source: docs reference/modeling-data/Property §What the cascade does to
  referrers. Confidence: high (table), low (a whole document is deleted even when it holds other items:
  probe P-MOD-22), low (recursion).

### R-MOD-42 — Cascade on value change

When a referenced value *changes* (an instance keeps existing but its target property value goes from
v₁ to v₂, and no other instance of the target type still holds v₁), `cascade: "true"` rewrites every
referrer's matching value from v₁ to v₂. For selector writes, "the same instance" means the replaced
element's position. For whole-document writes, instances are paired by `@key`, or as the lone instance
of the type (inferred, reusing the TransitionConstraint pairing rules).

- Evidence: documented (rewrite), inferred (pairing). Confidence: medium / low.

### R-MOD-43 — Restrict response

A write or DELETE blocked by `restrict` returns **409 Conflict** with an error document (pagelike: a
ConstraintViolation-style body whose description is `Referential constraints violated`), and nothing
changes. The 409 is documented for DELETE, and extended to value-changing PUT/POST/MOVE by pagelike.

- Evidence: documented (DELETE), inferred (others). Confidence: high / low.

---

## 10. Computed properties

### R-MOD-44 — `@computed`

- A property with an `@computed` slot (Sessel or JS) has **no stored value**. Its value is computed on
  every *typed* read. In Sessel `self` is the whole instance (`self.first-name + " " + self.last-name`).
  In JS `this` is the instance, which requires the `function` form.
- Writing a value to it is **rejected** ("the write is rejected with an error rather than silently
  accepted"). **pagelike decision**: 422 SchemaViolation, `check` `computed`, when an affected instance
  carries a value element for the property.
  **live 2026-09-29 (superseded, adopt-live):** PageLove stores the written value and validates it like
  any other (a declared `type` applies); the JSON-LD of a stored instance carries no computed value.
  pagelike dropped the guard (write-time only; Sessel/JS construction still refuses a computed value).
- `@computed` wins over `@read` on the same property.
- **Legacy form**: an `@read` slot whose item has the *bare* itemtype `https://pagelove.org/Sessel` (not
  `Sessel/Lambda`) is a computed property, not a read transformer.
- `@key` combined with a computed property does nothing and adds no auto default.
- Computed values are not injected into raw HTML GET responses. They are visible through typed access
  (Sessel/JS instances, composition). This is a pagelike decision, low. Probe P-MOD-24.
- Evidence: documented. Source: docs reference/modeling-data/Property §Computed properties, §Legacy
  syntax; javascript-in-schemas §Computed properties. Confidence: high (rules), low (status, HTML
  visibility).
- Contradiction: Resolvers §Identity resolver shows a bare-`Sessel` `@read` returning `self` as an
  "identity resolver". By the legacy rule that is a computed property. Decision (§19 C13): the legacy rule
  wins.

---

## 11. Validators

### R-MOD-45 — Property-level `@validate`

- Runs on affected instances for properties with **at least one value** (pagelike decision), after
  defaults, `@write` and the structural checks. Only the most-derived declaration runs.
- Sessel: `self` is the **list of the property's value elements** (e.g.
  `self.all((el) => el.text().matches("^[^@]+@[^@]+\\.[^@]+$"))`).
- JS: the first positional argument is the property value. That is a string for single-valued reads and
  an array for explicit `0..n`/`1..n` (pagelike decision, following R-MOD-20).
- The result must be truthy. Falsy, a thrown error, or a non-`true` Sessel result gives 422
  SchemaViolation, `check` `@validate`. Throwing an HTTPResponse-shaped object (R-MOD-47) chooses the
  status and message instead.
- Evidence: documented. Source: docs reference/modeling-data/Property §Validators, §Error cases;
  javascript-in-schemas §Slot semantics. Confidence: high (contract), low (absent-property skip).

### R-MOD-46 — Schema-level `@validate`

- Sessel: `self` is the instance element, and `self.microdata()` gives all properties. JS: `this` is the
  **serialized instance HTML string**, and the positional `context` is `null`, so the `function` form is
  required.
- Every level of the chain runs, ancestor-first, and all must be truthy. Failure gives 422 `check`
  `@validate`.
- It runs after cardinality, type and property-level `@validate` have passed, and before group constraints.
- Evidence: documented. Source: docs reference/modeling-data/Schema §Schema-level @validate,
  §Inheritance. Confidence: high (contract), medium (order; see §19 C1).

### R-MOD-47 — Refusing with a chosen response

In a JS property `@validate` or `@write`, throwing a **plain object** with
`schema_url` (or `itemtype`) = `https://pagelove.org/HTTPResponse`, plus `status` and `message` (and
optionally `body` and `headers`), makes the response that status. The body is `body`, or `message` when
`body` is absent (the Trigger semantics, reactions area). `new HTTPResponse(…)` in JS raises a
`ReferenceError`. That is an ordinary failure, so the client gets the **standard** rejection. Any other
thrown value is an ordinary failure too. A Sessel `throw new HTTPResponse { status: …, message: … }`
behaves the same way (inferred from Trigger).

- Evidence: documented. Source: docs javascript-in-schemas §Explaining why a value was rejected;
  reference/reacting-to-changes/Trigger (HTTPResponse fields). Confidence: high (JS), medium (Sessel).

### R-MOD-48 — Compile failures

- A `@validate` (either level) that fails to compile gives 422 on **every** write affecting instances of
  the governed type.
- A `@read`/`@write` that fails to compile: that step is **skipped**, and the rest of the chain runs.
- Evidence: documented. Source: docs reference/modeling-data/Schema §Error cases; Property §Error
  cases; Resolvers §Error cases. Confidence: high.

---

## 12. Resolvers (`@write`, `@read`)

### R-MOD-49 — `@write`

Runs on the write path before validation, and "what it returns is what ends up on the article". Sessel:
`self` is the list of the property's value elements in the item scope (excluding nested items). The
result must be an element or a list of elements, where a single element is auto-wrapped. The returned
elements **replace** the originals in place (the first returned element takes the position of the first
original). Extra returned elements are inserted after it, and an **empty list removes the property**.
JS: the value flows in as the first argument and the return is the new value (pagelike: written back to
`content` for `meta`, `datetime` for `time`, text content otherwise). Chain: child → root.

- Documented examples: email trim+lowercase (`" Alice@Example.COM "` → `"alice@example.com"`); slug via
  `new meta[itemprop="slug"][content=normalized] {}` (`My-Project` → `my-project`); JS postcode
  (uppercase, or throw a 422 HTTPResponse).
- Evidence: documented. Source: docs reference/modeling-data/Resolvers; recipes/transforming-data;
  javascript-in-schemas. Confidence: high (semantics), low (exact splice placement).
- **live 2026-09-29:** the documented email example fails on PageLove (500 `dombase…/Internal`
  "resolver pipeline error: sessel binding threw: unknown function: set_text"); so do `.lowercase()` and
  the `.text(x)` setter on a stored element ("requires a constructed element"). What runs is a new
  element: `self.map((el) => new span[itemprop="email"] { el.text().trim().lower() })`. pagelike still
  accepts the documented forms (deferred to the Sessel area, R-SESSEL-113/241/242).

### R-MOD-50 — `@read`

Runs after fetch and before the response. It changes only the representation, and the stored value is
unchanged. It applies to public-plane representations of governed instances: `GET`/`HEAD` of whole
documents and selector ranges, content negotiation (JSON-LD is extracted from the rendered page), the
edge `QUERY` over composed pages, and typed reads in composition. It does **not** apply to the WebDAV
authoring plane (raw bytes) or to `Sessel.stored()`. Chain: root → leaf.

Documented example: stored `<time itemprop="createdAt" datetime="2026-04-08T09:15:00Z">` is read as
`<time itemprop="createdAt" datetime="2026-04-08T09:15:00Z">8 April 2026</time>`. `@read` resolvers
also apply when the platform itself reads rules (a resolver on `AuthorizationRule.actor` expands a
placeholder actor). That is a cross-area dependency with permissions.

- Evidence: documented (GET), inferred (WebDAV raw, SSE payloads raw). Source: docs
  reference/modeling-data/Resolvers §When each hook runs; recipes/transforming-data; recipes/group-based-permissions
  §Membership beyond verified email. Confidence: high / low. Probe P-MOD-25.

### R-MOD-51 — Pipeline value and chaining

Each stage's output is the next stage's input. Levels without a resolver are skipped. Sessel and JS
stages may alternate. The marshalling between them is the pagelike decision from R-MOD-49: elements
become values (R-MOD-19) for JS, and JS values are written back into the elements for the next Sessel
stage. Example chain `[Entity, Person, Employee]`: `@write` runs Employee → Person → Entity, and `@read`
runs Entity → Person → Employee.

- Evidence: documented. Source: docs reference/modeling-data/Resolvers §Inheritance ordering,
  §Mixed-language chaining. Confidence: high.

### R-MOD-52 — Resolver errors

| Condition | Result |
|---|---|
| compile failure | the step is skipped |
| returns something other than element(s) (Sessel) | the request fails |
| throws at runtime | the request fails |
| returns an empty list | the property is removed from the output or storage |

Write-path failures happen before storage. **pagelike decision**: status **500** with a SchemaViolation
envelope that carries a `BindingFailure`. The JS page says a `@write` failing other than by a thrown
HTTPResponse "is an internal error". A read-path failure makes the GET fail with 500.

- Evidence: documented (table), inferred (500). Source: docs reference/modeling-data/Resolvers §Error
  cases; javascript-in-schemas §Explaining why a value was rejected. Confidence: high / medium.

### R-MOD-53 — Cross-document reads in resolvers

Resolvers can run selector queries against other documents of the host (for example a `${…}` selector
literal in Sessel). They see the committed state. Whether an in-flight write is visible is unspecified,
and pagelike shows committed state only.

- Evidence: documented (capability). Source: docs reference/modeling-data/Resolvers §Cross-document
  queries. Confidence: high / low.

---

## 13. Required combinations (`GroupConstraint`)

### R-MOD-54 — Recognition

Any item under a Schema's `itemprop="constraint"` whose `group` value is **non-empty** is a group
constraint. **Its itemtype is not checked**. Docs use `https://schema.host/GroupConstraint`, while the
Schema page and a recipe use `https://pagelove.org/GroupConstraint`. A constraint with no or empty
`group` is silently skipped. `cardinality` is `0..1`/`1..1`/`0..n`/`1..n` and defaults to `0..n` (no
constraint).

- Evidence: documented. Source: docs reference/modeling-data/GroupConstraint §Properties, §Error cases.
  Confidence: high.

### R-MOD-55 — Membership and counting

A group's members are every property, across the whole inheritance chain, that lists the group name in
its `group` field (a property may be in several groups). **pagelike extension**: the constraint item's own
`property` values (the recipe form) are also members. A member is **present** when it has at least one
value element in the item scope. The count of present members must satisfy the cardinality.

- Evidence: documented (Property.group). Source: docs reference/modeling-data/GroupConstraint §How groups
  are assembled; recipes/linking-related-data §Group constraints (recipe form). Confidence: high /
  low (extension).

### R-MOD-56 — Inheritance

Membership is additive across the chain. A child constraint with the same group name overrides the
parent's cardinality for the child type. Parent constraints otherwise apply.

- Evidence: documented. Source: GroupConstraint §Inheritance behaviour. Confidence: high.

### R-MOD-57 — Violation

422 SchemaViolation, `check` `group`, message
`group '<name>' constraint violated: <cardinality error>`. It runs after schema-level `@validate`
(R-MOD-15).

- Evidence: documented. Source: GroupConstraint §Error cases. Confidence: high.

---

## 14. Methods (declaration only)

### R-MOD-58 — Method item

`<li itemprop="property" itemscope itemtype="https://pagelove.org/Method">` inside a Schema:

| Field | Req. | Meaning |
|---|---|---|
| `name` | yes | method name (`foo` in `<t:foo>`, `obj.foo()`) |
| `implementation` | yes | Sessel or JS item holding `source` |
| `returns` | no | type URL hint. `https://pagelove.org/Element` (or a subtype) marks an HTML fragment result |
| `parameter` | no | ordered `https://pagelove.org/Parameter` items with `name` and `type` |
| `static` | no | `"true"` installs it on the class, with the class as `self`/`this` |

A Method is not a data property: it has no cardinality or type checks, and it does not appear in
instances. A Method without `name` or `implementation` is ignored (pagelike decision).

- Evidence: documented. Source: docs reference/modeling-data/Methods §Declaration, §Fields,
  §Implementation languages. Confidence: high.

### R-MOD-59 — Overloads, fallback, inheritance

Several methods may share a name with different parameter sets. The caller gets the overload whose
declared parameters are all supplied, with the most specific (largest) full match winning. A method
named `doesNotUnderstand` catches unknown names and receives `messageName` and `parameters`.
Methods are inherited and overridden by name.

- Evidence: documented. Source: Methods §Overloading, §doesNotUnderstand; permissions/Group §Custom
  membership. Confidence: high.

### R-MOD-60 — Registration and client inertness

Methods register with the schema (R-MOD-4). Invocation belongs to the composition, Sessel and JS areas.
pagelove.mjs ignores Methods, `@computed` and schema-level `@validate`.

- Evidence: documented. Source: Methods §Declaration; schema-definitions-in-html §Not supported
  client-side. Confidence: high.

---

## 15. ShapeConstraint

### R-MOD-61 — Declaration

Item `https://pagelove.org/ShapeConstraint` with fields:

| Field | Card. | Meaning |
|---|---|---|
| `resource` | 0..n | path globs. Omitted means global (all resources) |
| `selector` | 0..n | scope selectors (OR). Omitted means the document root (`:root`) |
| `constraint` | 0..n | selectors each scope element must satisfy |
| `permit` | 0..n | presence of any permit makes the shape **closed** |

A shape with **neither** a constraint nor a permit is ignored. A permit-only shape is valid: it requires
nothing and closes the scope. A shape containing a selector that fails to parse is ignored as a whole and
never blocks writes (pagelike decision, low).

- Evidence: documented. Source: docs reference/modeling-data/ShapeConstraint §Fields, §Error cases.
  Confidence: high.
- Contradiction: the error table says "Constraint with no `constraint` selectors: Silently skipped", while
  §Fields says a permit-only shape is valid. Resolution (§19 C6): skip only when there are neither.

### R-MOD-62 — Applicability

A shape applies to a write on document path p when it has no `resource`, or any `resource` glob matches p
(glob syntax per the permissions area, e.g. `/notes/*`). When a write is routed to an origin document
(a stamped or included element), the shape applies if a glob matches **either** the request path or the
origin document path (inferred from build-a-blog, which lists both `/data/posts/*` and `/posts/*`).

- Evidence: documented + inferred. Source: ShapeConstraint §How shape constraints run; learn/build-a-blog
  §Making comments safe to accept. Confidence: high / low.

### R-MOD-63 — Scope elements and which are checked

Scope elements are the elements of the proposed DOM D′ matching any `selector`, or the root element when
there is no selector. A scope element is **checked** when it is affected by the write:

| Write | Affected elements |
|---|---|
| whole-document write (PUT/WebDAV PUT/create) | all elements of D′ |
| selector PUT | elements of the replacement fragment, and all ancestors of its position |
| selector POST | elements of the appended fragment, the target, and its ancestors |
| selector DELETE | ancestors of the removed element, after removal |
| element MOVE | the union for the removal and the insertion |

- Evidence: documented + demo-source. Source: ShapeConstraint §How shape constraints run (steps 1–4,
  "evaluated against the proposed modified DOM"; for DELETE "the ancestor element with the targeted element
  removed"); docs User example (whole-document PUT, selector `[itemtype*=User]`); pagelove-cursor
  SKILL.md@b97c3ef:141-150,473-479 (selector `#my-list li`, POST to `#my-list` rejected with 422);
  pagelove-ats/site/admin/auth.html@8f200fc:340-348 (row shape on a POST to the table body);
  pagelove-polls/site/admin/auth.html@c9270e5:76 (shapes "fire on the granular write paths").
  Confidence: medium.
- Contradiction: step 2 of the docs algorithm says "the request target is checked against the
  `selector`". Taken literally, the cursor and ats examples would never fire. Decision (§19 C8): use the
  affected-set rule, which reproduces every documented and demo example.

### R-MOD-64 — Constraint evaluation

For each checked scope element E and each `constraint` C, **E must match C** as a selector with E as the
subject (so `:has(…)` is relative to E, `:not(:empty)` means E is non-empty, and `[required]` means E has
the attribute). All constraints are evaluated and every failure is reported. The shape has no
short-circuit.

- Evidence: documented. Source: ShapeConstraint §Examples (the nav example reports both failing
  constraints), §CSS selector patterns; the message wording "Element matching 'X' does not satisfy
  constraint 'C'". Confidence: medium. The §CSS patterns row `[itemprop="name"]` ("An element with the
  `name` property exists") suggests subtree semantics. Probe P-MOD-28.

### R-MOD-65 — Closed shapes: what is checked

For a closed shape and each checked scope element R (the scope root):

1. Every **descendant element** of R must match at least one `permit`.
2. Every **attribute** of every descendant must be *covered* by at least one permit that matches that
   element (R-MOD-66).
3. **R itself** is exempt from rule 1. Its attributes are checked against a pool made of the matching
   permits **plus the shape's `selector`s**.
4. Text, comments, CDATA and processing instructions are always allowed.
5. The whole subtree of R is checked, **including content that existed before this write**. A
   pre-existing uncovered element blocks later writes into the scope until it is removed (build-a-blog:
   "delete the script comment the first attempt left").

Violations: 422 for PUT/POST/MOVE, and 409 for DELETE (R-MOD-68).

- Evidence: documented + demo-source. Source: ShapeConstraint §Closed shapes, §What gets checked,
  §Worked example; learn/build-a-blog §Making comments safe to accept;
  pagelove-shop/site/constraints.html@d887054:12-15 (root attributes must be permitted).
  Confidence: high.

### R-MOD-66 — Attribute coverage by a permit

A permit covers attribute *a* on element X only if the permit matches X and its selector **references
*a* by name in the subject compound selector**, outside functional pseudo-classes:

| Permit | Covers |
|---|---|
| `[itemprop="title"]`, `[lang]`, `[x^=…]`, any attribute selector | that attribute |
| `.important` | `class` |
| `#main` | `id` |
| `article` (type only) | no attributes |
| `:has(> span)`, `:not(script)`, `:is(…)`, `:where(…)` | nothing from inside the parentheses |
| `ul > li[class]` | `class` (only the subject compound `li[class]` counts) |
| selector list `a[href], b[title]` | the union over the list members that match X |

Attribute names compare case-insensitively for HTML elements. Namespaced attributes are distinct:
`[lang]` does not cover `xml:lang`. There is no wildcard attribute allow. Pseudo-elements never match.

- Evidence: documented (table, limitations); inferred (subject-compound rule, selector lists,
  `:is`/`:where`). Source: ShapeConstraint §What each permit pattern covers, §Known limitations.
  Confidence: high (table), medium (rest).

### R-MOD-67 — Composed closed shapes

When a closed shape B's scope element lies **strictly inside** closed shape A's scope (both applicable to
the resource), B owns its subtree: A's check skips everything below B's root, and B's permits judge it.
B's root itself is still checked by A (it must be covered by A's permits, including its attributes).
Ownership is one-way. Ownership is computed over the whole D′, independent of which elements are
affected. Open shapes never take ownership.

Documented example: outer permits `[id]`, `[itemprop="title"]`, `[itemprop="content"]`, and inner
(`… > [itemprop='title']`) permits `em`. Then `<h2 itemprop="title"><em>Hi</em></h2>` is accepted and
`<span>` is rejected.

- Evidence: documented. Source: ShapeConstraint §Composed shapes. Confidence: high.

### R-MOD-68 — Status and body

- POST/PUT (and MOVE, pagelike) violating any shape: **422 Unprocessable Content/Entity**.
- DELETE whose result violates a shape: **409 Conflict**.
- Body: ConstraintViolation document (R-MOD-71). Every violation across shapes and elements is listed.
- Evidence: documented. Source: ShapeConstraint §Error cases, §Examples. Confidence: high (MOVE: low).

### R-MOD-69 — Known limitations (by design)

No wildcard attribute permit. Exclusion-only permits cover nothing. Namespaces are distinct.
Pseudo-elements are ineffective. Shapes express structure only, not value rules.

- Evidence: documented. Source: ShapeConstraint §Known limitations. Confidence: high.

---

## 16. Error documents

All error bodies are `text/html` (pagelike sends `Content-Type: text/html; charset=utf-8`) with microdata,
modelled on the live 416 error document (`<title>416 Range Not Satisfiable - Error</title>`,
`<body itemscope itemtype=…>`, `h1 itemprop="name"`, `meta itemprop="statusCode"`,
`p itemprop="description"`). Error responses carry no `ETag` (inferred from the live 416 probe).

### R-MOD-70 — SchemaViolation (pagelike shape; only the itemtype and `check` values are documented)

```html
<!DOCTYPE html>
<html>
  <head>
    <title>422 Unprocessable Entity - Schema Violation</title>
  </head>
  <body itemscope itemtype="https://pagelove.org/SchemaViolation">
    <h1 itemprop="name">Unprocessable Entity</h1>
    <meta itemprop="statusCode" content="422">
    <p itemprop="description">Schema validation failed</p>
    <ul>
      <li itemprop="violations" itemscope itemtype="https://pagelove.org/Violation">
        <span itemprop="check">cardinality</span>
        <span itemprop="itemtype">https://example.com/Tag</span>
        <span itemprop="property">label</span>
        <span itemprop="message">[https://example.com/Tag].label: expected exactly one value (1..1), found 0</span>
      </li>
    </ul>
  </body>
</html>
```

`check` values: `cardinality`, `type`, `enum`, `@validate`, `group` (documented), and the pagelike
additions `computed`, `default`, `references`, `schema`. The message prefix is `[<itemtype>].<property>: `
(documented for enum). For schema-level checks it is `[<itemtype>]: `. A `value` property is included for
type and enum failures. A binding failure adds `<div itemprop="failure" itemscope itemtype="https://pagelove.org/BindingFailure">…</div>`
inside the violation.

- Evidence: documented (itemtype, check values, enum and group messages); inferred (markup).
  Source: docs reference/modeling-data/Schema §Error cases; Property §Error cases; Types §Error cases;
  GroupConstraint §Error cases. Confidence: medium (itemtype and checks), low (markup).
- **live 2026-09-29:** cardinality messages now use PageLove's words (`[T].label: cardinality 1..1
  violated: expected exactly 1 value, found 0`; groups `[T]: group 'g' constraint violated: cardinality
  …`). A **selector** write (PUT/POST/DELETE, element MOVE) that breaks a cardinality answers PageLove's
  bare problems item `https://dombase.pagelove.team/ns/error/Cardinality` (`T /label: cardinality 1..1
  violated: … (cardinality)`, `text/html`), adopted. PageLove's whole-write SchemaViolation is also a bare
  problems item (`<div itemscope itemtype="https://pagelove.org/SchemaViolation"><ul itemprop="problems">…`),
  and the `check` computed is gone (R-MOD-44); pagelike keeps the page above for whole writes (deferred:
  the BindingFailure nesting of R-MOD-73 was not observed).

### R-MOD-71 — ConstraintViolation for shapes (documented exactly)

```html
<!DOCTYPE html>
<html>
  <head>
    <title>422 Unprocessable Entity - Shape Constraint Violation</title>
  </head>
  <body itemscope itemtype="https://pagelove.org/ConstraintViolation">
    <h1 itemprop="name">Unprocessable Entity</h1>
    <meta itemprop="statusCode" content="422">
    <p itemprop="description">Shape constraints violated</p>
    <ul itemprop="violations">
      <li itemscope itemtype="https://pagelove.org/Violation">
        <span itemprop="constraintSelector">[itemtype*=User]</span>
        <span itemprop="failedConstraint">:has([itemprop=&quot;email&quot;])</span>
        <span itemprop="message">Element matching &#x27;[itemtype*=User]&#x27; does not satisfy constraint &#x27;:has([itemprop=&quot;email&quot;])&#x27;</span>
      </li>
    </ul>
  </body>
</html>
```

- `constraintSelector` is the shape `selector` as written (`:root` when there is none, as a pagelike
  decision). `failedConstraint` is the constraint text. The message is
  `Element matching '<selector>' does not satisfy constraint '<constraint>'`. Text is HTML-escaped with `'`
  as `&#x27;` and `"` as `&quot;`.
- Closed-shape failures (pagelike, undocumented): `failedConstraint` is `permit`, and the message is
  `Element <tag> is not permitted by shape '<selector>'` or `Attribute '<name>' on <tag> is not permitted
  by shape '<selector>'`.
- DELETE (409): title `409 Conflict - Shape Constraint Violation`, h1 `Conflict`, statusCode `409`
  (inferred).
- Evidence: documented. Source: ShapeConstraint §Examples. Confidence: high (422 shape form), low
  (closed and 409 variants).
- **live 2026-09-29 (superseded, adopt-live):** PageLove answers 422 with the bare problems item
  `https://dombase.pagelove.team/ns/error/ShapeConstraint` (`text/html`): a first problem "Shape
  constraints violated", then one problem per violation, `https://pagelove.org/ShapeConstraint <selector>:
  <message>`, escaping only `& < >`. Closed-shape messages: `Element <img> inside '<sel>' is not permitted
  by any permit`, `Attribute <onclick> on <h2> inside '<sel>' is not permitted by any matching permit`,
  `Attribute <class> on <article> matched by '<sel>' is not permitted by the selector or any matching
  permit`. A DELETE (409) answers `…/CascadeBlocked`: `Operation refused by cascade constraint: Constraint
  violation: 1 violation(s):` then `  - [https://pagelove.org/ShapeConstraint] <sel> (<constraint>):
  <message>`. pagelike renders exactly these.

### R-MOD-72 — ConstraintViolation for uniqueness (and restrict)

This uses the TransitionConstraint markup (`<ul><li itemprop="violations" itemscope itemtype=…Violation>`),
which is documented as "the same shape as uniqueness violations". pagelike fills in:
description `Uniqueness constraints violated`, title `422 Unprocessable Entity - Uniqueness Constraint
Violation`, `constraintSelector` `[itemprop='<prop>']`, `failedConstraint` `unique(<prop>)` (individual)
or `unique(<group>)` (composite), and a message such as
`Uniqueness violation: 'https://example.com/Tag' property 'label' value 'x' already exists`. It also
includes `itemtype`, `property`, and `value`. The other instance's location is not disclosed. For
restrict, the status is 409, the description is `Referential constraints violated`, and failedConstraint is
`restrict(<prop>)`.

- Evidence: documented (the shape is shared) + demo-source (the text matches `/uniqueness/i`); inferred
  (field values). Source: TransitionConstraint §The 422 body; demo-apps app.js files (e.g.
  demo-01-event/app.js@c4dd883:54). Confidence: medium / low.
- **live 2026-09-29 (superseded, adopt-live except the path):** PageLove answers the bare problems item
  `https://dombase.pagelove.team/ns/error/ConstraintViolation` whose one message is `Constraint violation:
  1 violation(s):` followed by `  - [uniqueness] [itemprop='slug'] (unique(slug)): Uniqueness violation:
  value already exists for property 'slug' at '<path>'`; composite: `[itemprop='a'], [itemprop='b']
  (unique-group(g)): Composite uniqueness violation: value combination already exists for properties 'a,
  b'` (members sorted); inside one document: `another item in '<path>' already claims this value for
  property 'slug' (first claimed by item '0.2.0.1')`. Only the first uniqueness violation is reported.
  Unresolved references use the same item (`[reference] [itemprop='org'] (references(org)): Referenced
  value does not exist for property 'org'`), and restrict answers 409 `…/CascadeBlocked` (`Operation
  refused by cascade constraint: Constraint violation: …  - [cascade-restrict] [itemprop='id']
  (restrict(<T>#id)): Cannot delete: 1 document(s) reference this value via 'org'`). pagelike renders
  these, but still withholds `at '<path>'` for a value held in another document
  (keep-documented-security: the writer may not be allowed to read that document); its item paths in
  the in-document message follow its own parser and may differ from PageLove's.

### R-MOD-73 — BindingFailure

```html
<div itemscope itemtype="https://pagelove.org/BindingFailure">
  <meta itemprop="language" content="https://pagelove.org/JavaScript/Module">
  <meta itemprop="variant" content="threw">
  <p itemprop="message">TypeError: …</p>
  <pre itemprop="stack">    at default (eval:2:17)</pre>
</div>
```

Variants: `parse`, `shape`, `threw`, `timeout`, `out-of-memory`, `marshal`, `return-type`,
`import-not-allowed`, `unknown-schema`, `unknown-language`. `stack` appears only for `threw` when it is
available. Sessel failures use the same structure with a different `language`. It is nested inside the
SchemaViolation envelope.

- Evidence: documented. Source: javascript-in-schemas §Errors. Confidence: high (item), low (nesting
  property name).

### R-MOD-74 — Status summary

| Condition | Status | Body |
|---|---|---|
| cardinality / type / enum / property @validate / schema @validate / group | 422 | SchemaViolation |
| cyclic parent chain; @validate compile failure; reference load error | 422 (pagelike for reference load) | SchemaViolation `check` `schema`/`@validate` |
| write to a computed property | 422 (pagelike) | SchemaViolation `computed` |
| dynamic default failure | 422 (pagelike) | SchemaViolation `default` + BindingFailure |
| unresolved reference | 422 | SchemaViolation `references` (pagelike) |
| uniqueness (individual/composite) | 422 | ConstraintViolation |
| shape violated by POST/PUT/MOVE | 422 | ConstraintViolation |
| shape violated by DELETE | 409 | ConstraintViolation |
| restrict-blocked delete/change | 409 | ConstraintViolation-style |
| `@write` runtime failure / bad return (not HTTPResponse) | 500 (pagelike) | SchemaViolation + BindingFailure |
| `@read` failure | 500 (pagelike) | error document |
| thrown HTTPResponse in `@validate`/`@write` | its `status` (default 500) | its `body`/`message` |

live 2026-09-29: a value written to a computed property is validated, not refused (R-MOD-44); unresolved
references, uniqueness, shapes, restrict and selector-write cardinality answer PageLove's problems items
(`…/ConstraintViolation`, `…/ShapeConstraint`, `…/CascadeBlocked`, `…/Cardinality`; R-MOD-70..72). A Sessel
`@write` runtime error was observed as 500 `dombase…/Internal` "resolver pipeline error: sessel binding
threw: …"; pagelike keeps its SchemaViolation + BindingFailure there (not adopted, noted in
docs/compat/decisions-2026-09-29/modeling.md).

### R-MOD-75 — WebDAV plane errors

On the authoring plane, a refused write returns the same status, with an
`<article itemscope itemtype="https://pagelove.org/Error">` carrying `status`, `kind`, a nested `type`
item `https://dombase.pagelove.team/ns/error/<Kind>`, a `message`, and the pipeline's own error document
unchanged under a `detail` property.

- pagelike decision for `kind` names (undocumented): `SchemaViolation` (422 schema failures),
  `ConstraintViolation` (422 shape and uniqueness), `ReferentialConflict` (409 restrict and 409 shape on
  DELETE), and `BindingFailure` (500 resolver failures). The public-plane bodies are the documents in
  R-MOD-70..73. The live public-plane error probes (416, 401) carried no `kind`, so pagelike does not add
  one there. (live 2026-09-29: a WebDAV PUT is no longer validated, R-MOD-13; the problems-item refusals
  carry their item kind, and the wrapper's `kind` is `Internal`, or `Conflict` for a 409.)
- Evidence: documented. Source: docs reference/protocol/WebDAV §When something goes wrong;
  research read-probes.json (416 and 401 bodies). Confidence: high (wrapping), low (kind names).

---

## 17. Client-side schema view (informative, beta-js)

### R-MOD-76 — pagelove.mjs discovery differences

pagelike serves beta-js unchanged. Differences from server semantics that pages may rely on:

- Discovery covers every `[itemtype="https://pagelove.org/Schema"]` on the page. `type` and `parent` are
  read from direct children only.
- Property items must be `https://pagelove.org/Property` or a declared subtype, and others are ignored with
  a warning. `name`, `type` and `cardinality` are read from any descendant.
- Default cardinality is `0..1` (the server uses `0..n`). The default type is `""`.
- An unknown parent or a cycle throws.
- `create()` builds `<article itemscope itemtype id="item-<base36 time><rand4>">`. It uses `time[datetime]`
  only for `https://pagelove.org/DateTime`/`Date`, and treats other `https://pagelove.org/…` types (except
  Text/Integer/Number/Boolean) as nested schema values. `0..n` properties with no value are omitted.
- `default` (static meta or JS module) and a JS `@read` are loaded client-side through Blob URLs.
- Evidence: client-source. Source: beta-js/pagelove.mjs@c204746:227-389. Confidence: high.

---

## 18. Cross-area dependencies

- **reading-writing**: status codes for successful writes (201 create, 206 selector, 204 DELETE/MOVE),
  first-match semantics, conditional requests (412 vs 422 precedence: probe P-MOD-41), the element-identity
  rule for selector PUT, MOVE mechanics, and the base error document (`internal/errdoc`).
- **selectors**: full CSS plus the PageLove extensions (`:has`, `:not`, `:value-equals`, `:isa`) for shape
  scopes, constraints and permits. Permits need an AST (R-MOD-66). `:isa()` consumes this area's subtype
  map.
- **microdata**: item scoping, token-split `itemprop`, the value table (R-MOD-19), and nested items.
- **permissions**: `resource` glob matching (shapes), authorization before validation, `@read` on
  AuthorizationRule fields, and the system `Group` schema and its subtypes.
- **sessel**: evaluation of every Sessel slot, `String.random`, `self` binding conventions,
  `new Type {}` (defaults and validation at construction), `Type.search()`, `Pagelove.PUT` (runs this
  pipeline and emits `@key` ids), and `throw new HTTPResponse`.
- **server-js**: module evaluation, marshalling, budgets, BindingFailure variants, the `pagelove:schema`
  import (resolves classes from this registry), and the `unknown-schema` variant.
- **composition/liquid**: method elements (dispatch through R-MOD-58), typed instances and the list-read
  rule (R-MOD-20), `@read` on composed output, write routing through stamps and includes (R-MOD-62).
- **reactions**: Triggers run before validation, Processors after, TransitionConstraints (they share the
  ConstraintViolation envelope and `@key` pairing, and bypass WebDAV), and the HTTPResponse object.
- **webdav**: shared pipeline, Error wrapping with `detail`, and immediate registration.
- **sse**: no events for rejected writes. Cascade deletions and rewrites emit events for every affected
  document.
- **store**: host-wide indexes for uniqueness and references, maintained transactionally, and a registry
  cached per write generation.
- **harness**: `${P}` substitution must apply inside file bodies and request bodies. Cases embed `${P}`
  in type URLs, shape resources and paths.

---

## 19. Contradictions and compatibility decisions

| # | Topic | Claims | Decision |
|---|---|---|---|
| C1 | Order of schema-level `@validate` vs group constraints | Schema and GroupConstraint pages: schema `@validate` then groups. JS page slot table: schema `@validate` "after ... group constraints have all passed". | Schema `@validate` first (two pages against one). Cases: `modeling.validators.order-schema-validate-before-group` (primary) and `…-group-before-schema-validate` (disputed). |
| C2 | Property `@validate` vs cardinality | Property and JS pages: "after defaults ... but before the required-property check". Types and Schema pages: cardinality/type run before property `@validate`. | Cardinality/type first. |
| C3 | `@validate` inheritance | Schema: every validator in the chain runs. Property and JS: most-derived only. | Schema-level runs the whole chain. Property-level runs the most-derived only. |
| C4 | GroupConstraint itemtype and membership | Reference: `https://schema.host/GroupConstraint`, membership via `Property.group`. Schema page and linking recipe: `https://pagelove.org/GroupConstraint`, and the recipe lists members as `property` values on the constraint. | Itemtype ignored. Membership is the union of both forms. Recipe form case is marked `disputed`. |
| C5 | References | Reference: `{itemtype}#{itemprop}`, and a target not unique gives a load error. The recipe uses `references` = a bare type URL with a `URL`-typed value, field `onDelete`, and "Restrict is the default for required references". | Follow the reference page. `onDelete` is ignored and omitted cascade means none. Recipe cases are `disputed`. |
| C6 | Shapes without constraints | Error table: skipped. Fields: permit-only is valid. | Skip only when there are neither constraints nor permits. |
| C7 | Shape without selector | Fields: "applies to the document root". Global example: "all resources and all elements". | Root. The all-elements case is `disputed`. |
| C8 | Which elements a shape checks | Algorithm step 2: "the request target is checked against the selector". Examples: whole-document PUTs and descendant selectors fire. | Affected-set rule (R-MOD-63). |
| C9 | Value used for type checks | Types page: content attribute or text. Selector-Extensions and a live demo: microdata values (`time[datetime]`, `href`, …). | Microdata values. The literal reading is kept as a `disputed` case. |
| C10 | 422 reason phrase | "Unprocessable Entity" vs "Unprocessable Content". | Not significant. Bodies use "Unprocessable Entity" as in the documented shape example. |
| C11 | Violation list markup | Shape example: `<ul itemprop="violations"><li itemscope …>`. Transition example: `<ul><li itemprop="violations" itemscope …>`. | Reproduce each documented form for its own kind. Undocumented kinds use the transition form (valid microdata). |
| C12 | Client vs server schema semantics | beta-js: cardinality default `0..1`, unknown parent throws, `https://pagelove.org/<Type>` type URLs. Server: `0..n`, silent stop, `https://schema.host/<Type>`. | Each side keeps its own. The server treats `https://pagelove.org/Text` etc. as unknown types. |
| C13 | Bare-Sessel `@read` | Resolvers: "identity resolver". Property: legacy computed property. | Legacy computed rule. |
| C14 | WebDAV and constraints | WebDAV page and shop demo: schemas and constraints enforced (422). pagelove-dev skill: WebDAV success "does not test ... ShapeConstraint". TransitionConstraint: WebDAV bypasses transitions. | Enforce schemas and shapes on WebDAV. Bypass transitions, rules, triggers and processors. |
| C15 | Composite uniqueness works | Docs and skill: supported. demo-apps: "verified broken on this host". | Implement per docs. The live case settles it. |
| C16 | Status for schema-load failures | Schema/Property: 422 ("every write is rejected"). WebDAV page: 500 when the site's schema cannot be loaded. | 422 for declared-but-invalid schemas. 500 only for infrastructure failures. |
| C17 | `@write` failure status | Resolvers: "request fails". JS: "internal error". | 500. |
| C18 | Registration of WebDAV-authored schemas | TransitionConstraint: WebDAV edits apply within 60 s. Method Elements: "PUT the document ... once to register it". | pagelike: immediate on both planes. |
| C19 | `@key` auto default outside construction | Documented "at instance construction". Defaults apply "on a write". | Also inject on HTTP writes when the key is absent. |

---

## 20. Open questions for live probing

All probes run on a disposable host. `T` is `https://pagelike.test/<prefix>/X`, a type URL unique to
the run, so they cannot collide with real data. `S` is a schema document written over WebDAV and
followed by a 61 s wait (or written by a public PUT). Paths are under the run prefix. Every probe needs
rules allowing anonymous GET/PUT/POST/DELETE/MOVE on the prefix.

- **P-MOD-1 (WebDAV propagation)**: dav `PUT S` (T with `label` 1..1). Then public `PUT /x-N.html` with an
  item lacking `label` at t = 0, 5, 15, 30, 45, 60, 75 s. Record the first 422.
- **P-MOD-2 (public registration)**: public `PUT S`, then immediately public `PUT` an invalid item. Expect
  422 if the registration is immediate.
- **P-MOD-3 (self-validation)**: public `PUT /both.html` containing S and an invalid T item, then
  `PUT /next.html` with an invalid item. Record whether each is 201 or 422.
- **P-MOD-4 (duplicate schema declarations)**: `/a.html` declares T `label` 1..1 and `/b.html` declares T
  `label` 0..1 Integer. `PUT` items: (no label), (`label`=`x`). Then swap the paths.
- **P-MOD-5 (multi-token itemtype)**: an item with `itemtype="T https://example.com/Other"` lacking
  `label`. Record 201 or 422.
- **P-MOD-6 (itemprop tokens)**: T `label` 1..1. Item `<span itemprop="label title">x</span>`. 201 means
  tokens count.
- **P-MOD-7 (whitespace and empty text)**: Integer property with `<span> 42 </span>`, and
  `<meta content=" 42 ">` (the expected result under verbatim values is 422 for both). Also a `1..1`
  property given as `<span itemprop="p"></span>` (empty text): does it count as present?
- **P-MOD-8 (Number grammar)**: values `1e3`, `.5`, `5.`, `+3`, `inf`, `NaN`, `0x10`, `1_000`, `" 1"`.
- **P-MOD-9 (Integer grammar)**: `+5`, `007`, `-0`, `9223372036854775807`, `9223372036854775808`.
- **P-MOD-10 (DateTime grammar)**: `2024-01-15t10:30:00z`, `2024-01-15 10:30:00Z`,
  `2024-01-15T10:30:00.123+01:00`, `2024-01-15T10:30:00`, `2016-12-31T23:59:60Z`.
- **P-MOD-11 (Date grammar)**: `2024-1-5`, `0000-01-01`, `2024-13-01`, `+2024-01-01`.
- **P-MOD-12 (URL grammar)**: `javascript:alert(1)`, `a:b`, ` https://x.com`, `http://`, `//x.com`,
  `https://exa mple.com`.
- **P-MOD-13 (item as primitive value)**: Text property whose value is `<div itemprop="p" itemscope>…</div>`.
- **P-MOD-14 (affected set)**: dav-write a document with two T items where item #2 is invalid (written
  before S exists), then write S and wait. Public `PUT Range: selector=#i1 …` with a valid replacement of
  item #1. 206 means unaffected instances are not rechecked.
- **P-MOD-15 (stage barriers)**: two items, one with a cardinality failure and one with a failing
  property `@validate`. Inspect which violations are reported.
- **P-MOD-16 (default vs @write)**: a property with static default `ABC` and an `@write` that lowercases.
  PUT without the property and inspect the stored value (`ABC` or `abc`).
- **P-MOD-17 (@validate on absent)**: an optional property with JS `@validate` `(v) => typeof v === 'string'`.
  PUT without it.
- **P-MOD-18 (JS pipeline shape)**: a `0..n` property with JS `@write` `(v) => JSON.stringify(v)`, sent
  with 1 and with 2 values. Inspect storage.
- **P-MOD-19 (uniqueness details)**: two items with `content=""` for a unique property. The same value
  twice in one item. A parent type with unique `id` and a child type item sharing the value with a parent
  item.
- **P-MOD-20 (composite)**: the documented four-step membership recipe, plus a membership lacking `org-id`
  twice.
- **P-MOD-21 (references)**: a target unique only via its parent, `references` with two `#`, and a
  referencing item and its target created in one whole-document PUT.
- **P-MOD-22 (cascade scope)**: one document holding two referencing items (1..1, `cascade: true`) plus
  other content. DELETE the target document. Is the whole document deleted or just the item?
- **P-MOD-23 (restrict on change)**: a selector PUT changing a referenced value under `restrict`.
  409 or 422?
- **P-MOD-24 (computed visibility)**: GET an instance with a computed `display-name`. Does HTML include
  it? Also a Sessel QUERY `T.search().first().display-name`.
- **P-MOD-25 (@read scope)**: `@read` uppercase. Compare public GET, `Accept: application/ld+json`, dav
  GET, edge QUERY, and SSE mutation payloads.
- **P-MOD-26 (error statuses)**: JS `@write` throwing `Error`, JS default throwing, a `references` value
  without `#`, and a `@validate` syntax error. Record status and body itemtype for each.
- **P-MOD-27 (shape scope)**: shape `selector` `#list > li` with a constraint `:has([itemprop=name])`.
  (a) POST to `#list` an `<li>` without name. (b) PUT `Range: selector=#list` with a bad li.
  (c) PUT `Range: selector=#list > li:first-child` with a valid li while a sibling li is bad.
- **P-MOD-28 (constraint semantics)**: shape selector `article`, constraint `[itemprop="name"]`, with a
  PUT of `<article><span itemprop="name">x</span></article>`. 201 means subtree semantics.
- **P-MOD-29 (no selector)**: shape with only `constraint` `:not([itemscope]:not([itemtype]))`, and a
  PUT of a document containing `<div itemscope>`.
- **P-MOD-30 (shapes vs defaults)**: a closed shape permitting only `[itemprop="title"]` and a schema
  default for `status`. PUT an item with only a title.
- **P-MOD-31 (shape on MOVE)**: a closed shape on `#a` that forbids `.x`. MOVE `.x` from `#b` into `#a`.
- **P-MOD-32 (routed writes)**: a shape with only the origin path in `resource`, then POST through a
  stamped page. Repeat with only the request path.
- **P-MOD-33 / P-MOD-34 / P-MOD-35 (bodies)**: capture the full 422 bodies for a closed-shape failure,
  a cardinality failure, and a duplicate unique value.
- **P-MOD-36 (recipe group form)**: a constraint listing `property` members with no `Property.group`.
  PUT items with none, one, or two of the members present.
- **P-MOD-37 (C1 order)**: schema-level `@validate` false plus a violated group constraint. Which
  `check` is reported?
- **P-MOD-38 (Cardinal on declarations)**: PUT a schema document with `cardinality` `2..5`. Does it get
  422? And how do instances behave?
- **P-MOD-39 (override granularity)**: parent `label` Integer 1..1. The child redeclares `label` with only
  a `description`. Is a child item without a label rejected?
- **P-MOD-40 (WebDAV enforcement)**: dav `PUT` an invalid T item (after S propagated), and dav `PUT` into
  a closed-shape resource. Record the status and the `kind`.
- **P-MOD-41 (precedence)**: `PUT` with a stale `If-Match` and invalid content. 412 or 422?

---

## 21. Harness cases (index)

The cases live in `harness/cases/modeling/`. They are generated for consistency, and they are the
normative artifacts. Case ids are `modeling.<topic>.<name>`. The `notes` field of every case names the
requirements it pins (`spec: R-MOD-…`).

| File | Cases | Covers |
|---|---|---|
| `discovery.yaml` | 9 | R-MOD-2, 4, 5, 8, 9 (host-wide discovery, registration timing, 60 s WebDAV bound, `<template>`, no-type schemas) |
| `inheritance.yaml` | 10 | R-MOD-10, 11 (parent props, override, unknown parent, cycles, unique/validator/default merge rules) |
| `cardinality.yaml` | 15 | R-MOD-13, 14, 17, 18, 26 (four cardinalities, nested scope, selector PUT/POST/DELETE, MOVE) |
| `types.yaml` | 22 | R-MOD-19, 21, 22, 25 (every primitive with documented valid/invalid values, value extraction, grammar edges) |
| `enums.yaml` | 4 | R-MOD-23 |
| `nested.yaml` | 5 | R-MOD-14, 24 |
| `defaults.yaml` | 10 | R-MOD-27..29 |
| `keys.yaml` | 4 | R-MOD-33..35 |
| `uniqueness.yaml` | 13 | R-MOD-30..32 (including the documented composite recipe; one `.live` sibling, 2026-09-29) |
| `references.yaml` | 16 | R-MOD-37..43 |
| `computed.yaml` | 4 | R-MOD-44 |
| `validators.yaml` | 11 | R-MOD-45..48, stage order (R-MOD-15) |
| `resolvers.yaml` | 15 | R-MOD-49..52 |
| `groups.yaml` | 9 | R-MOD-54..57 |
| `methods.yaml` | 3 | R-MOD-58..60 |
| `shapes.yaml` | 16 | R-MOD-61..64, 68, 71 |
| `shapes-closed.yaml` | 13 | R-MOD-65..67, 69 |
| `write-paths.yaml` | 6 | R-MOD-13, 14, 16 |
| `errors.yaml` | 6 | R-MOD-70..73 |
| `probes-0929.yaml` | 5 | live probes of 2026-09-29 (R-MOD-13, 14, 24, 44, 49, 70..72; docs/compat/decisions-2026-09-29/modeling.md) |

Cases marked `status: disputed` record the losing side of a §19 contradiction:
`types.time-text-content-literal` (C9), `references.ondelete-recipe-restrict` (C5),
`validators.order-group-before-schema-validate` (C1), `resolvers.identity-bare-sessel-read` (C13), and
`shapes.global-example-all-elements` (C7). Every type URL embeds `${P}`
(`https://pagelike.test${P}/Name`), and every shape declares a `resource` under `${P}`. Live runs therefore
cannot affect other data on the host. The harness must apply `${P}` substitution inside file and request
bodies.
