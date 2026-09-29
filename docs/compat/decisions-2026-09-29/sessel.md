# Live reconciliation decisions: sessel (2026-09-29)

This file records how pagelike handled the nine `sessel` cases that diverged in
the sampled live PageLove run of 2026-09-29 (one case per feature). It follows
the decision policy table in [`../decisions.md`](../decisions.md)
(`adopt-live`, `keep-documented-security`, `keep-standard`, harness artifact).

**Evidence.**
- Failing run: `harness/observations/live-2026-09-29/` (`RUN-SUMMARY.txt`, one
  JSON file per case; a case id maps to its file by turning dots into dashes
  and cutting the name to 40 characters).
- This reconciliation: `harness/observations/live-2026-09-29-reconcile-sessel/`.
  Three live runs, 19 cases in all, through `./bin/harness run --target live`
  only:
  1. probes, 3 cases (exploration);
  2. probes extended, 3 cases (follow-up questions);
  3. final run, 13 cases: the nine cases below, the `.live` sibling and the
     three probes. **13 passed, 0 failed.**

  The harness writes one file per case, so the run-3 files replaced the run-1
  and run-2 probe files. Every probe step of run 3 repeats a run-1/2 question
  with its answer as the expectation, so the run-3 files hold the same answers.
  One run-1 answer (Temporal accessor properties are `null`) is no longer a
  probe step; it is kept in `sessel-query-values-temporal-format-live.json`
  and in the original `live-2026-09-29/sessel-query-values-temporal-format.json`.
- Probe cases: `harness/cases/sessel/probes-0929.yaml`
  (`sessel.query.probe-0929.from-and-path`, `.values`,
  `.mutation-and-selector`; `evidence: live-observed`). Their files are
  `sessel-query-probe-0929-from-and-path.json`,
  `sessel-query-probe-0929-values.json` and
  `sessel-query-probe-0929-mutation-and-sel.json`. Step numbers below refer to
  those files.

## Summary

| Case id | Class | Observation files | One line |
|---|---|---|---|
| `sessel.query.new.setters-and-mutation` | adopt-live | `live-2026-09-29/sessel-query-new-setters-and-mutation.json`; probe mutation steps 1-7 | Sub-select results are stored (immutable) elements even under a constructed element. |
| `sessel.query.sel.element-getters` | adopt-live | `live-2026-09-29/sessel-query-sel-element-getters.json`; probe from-and-path step 5; probe values steps 20-21 | `text()` is never null; a QUERY reaches no other document. |
| `sessel.query.sel.from-path` | adopt-live | `live-2026-09-29/sessel-query-sel-from-path.json`; probe from-and-path steps 3, 7 | A QUERY program sees only its target document. |
| `sessel.query.sel.from-self-list` | adopt-live | `live-2026-09-29/sessel-query-sel-from-self-list.json`; probe from-and-path steps 1, 4 | Elements of a QUERY have no provenance (`path()` null). |
| `sessel.query.sel.selector-type` | adopt-live | `live-2026-09-29/sessel-query-sel-selector-type.json`; probe mutation steps 8-10 | `new Selector {…}` needs `@schema Selector`, else it is a `<selector>` element. |
| `sessel.query.sel.sub-select-provenance` | adopt-live | `live-2026-09-29/sessel-query-sel-sub-select-provenance.json`; probe from-and-path step 6 | An element from-source matches nothing in a QUERY; no provenance. |
| `sessel.query.values.integer-and-float-division` | adopt-live | `live-2026-09-29/sessel-query-values-integer-and-float-di.json`; probe values steps 1-4 | A Float's text form has no `.0`; JSON keeps it. |
| `sessel.query.values.temporal-format` | keep-standard (partly adopt-live) | `live-2026-09-29/sessel-query-values-temporal-format.json`; probe values steps 16-18; `sessel-query-values-temporal-format-live.json` | Accessors answer only as methods live; pagelike adds the method form and keeps the documented properties. |
| `sessel.query.values.try-catch-message` | adopt-live | `live-2026-09-29/sessel-query-values-try-catch-message.json`; probe values steps 5-15 | The arithmetic TypeError message names the operand. |

| Class | Cases |
|---|---|
| adopt-live | 8 |
| keep-documented-security | 0 |
| keep-standard | 1 (`temporal-format`, with its method form adopted) |
| harness artifact | 0 |

No failure was a harness artifact: every case built valid input, and each
difference reproduced in a minimised probe.

## Cases

### `sessel.query.new.setters-and-mutation` — adopt-live

- **Observations:** `live-2026-09-29/sessel-query-new-setters-and-mutation.json`
  (step 2: 400); probe `sessel-query-probe-0929-mutation-and-sel.json`
  steps 1-7; the final pass is
  `live-2026-09-29-reconcile-sessel/sessel-query-new-setters-and-mutation.json`.
- **PageLove:** `new li {…}.insertBefore(el.${ li }.last())` is a TypeError,
  "type error: insertBefore() reference must be a mutable child element, not a
  stored element". A sub-select on a constructed element yields *stored*
  (immutable) elements: the text setter fails ("type error: text(value) setter
  requires a constructed element, got element"), `replaceWith()` fails ("type
  error: replaceWith() cannot be used on stored (immutable) elements"), and
  `remove()` does nothing and returns null. The same holds for elements
  queried from the document (step 7). `el.children()` handles are live and
  mutable: `insertBefore(el.children().last())` gives
  `<li>a</li><li>b</li><li>c</li>`.
- **pagelike before:** a sub-select kept the receiver's mutability, so the
  docs' example worked. A queried element's `remove()` was a TypeError.
- **pagelike now:** sub-select results are always stored elements
  (`subSelect`, `internal/sessel/selectors.go`). The three messages are
  PageLove's, and `remove()` on a stored element is a no-op. The case's
  step 2 uses `children()`. A new step 3 asserts the refusal of a
  sub-selected reference.
- **Rationale:** the docs' element reference (R-SESSEL-211/240) passes
  `el.${ li }.first()` to `insertBefore`, which PageLove refuses. A program
  written for PageLove therefore already mutates constructed trees through
  `children()`, and pagelike's broader acceptance only hid the difference. The
  change produces no incorrect data: refused mutations fail loudly, and the
  silent `remove()` no-op matches PageLove exactly. Resolver working copies
  (`self` in `@write`/`@read`) are still mutable. Only elements reached
  *through* a sub-select changed, and no case or example mutates those.

### `sessel.query.sel.from-path` — adopt-live (settles protocol C-10)

- **Observations:** `live-2026-09-29/sessel-query-sel-from-path.json`
  (`null`, `null`, `0`); probe from-and-path steps 3, 4, 7, 8, 11, 12.
- **PageLove:** in a QUERY, every `from` source other than `self` matches
  nothing. That covers literal, extensionless, relative and glob paths, the
  target's own path, `document`, elements and Lists (step 3: eight variants,
  all 0; step 8). It makes no difference whether the other document was written
  through WebDAV or through a public-plane PUT made in the same case (step 2).
  A bare selector searches only the target document (step 4), and a
  block-level `from "<path>" { … }` sees nothing (step 8).
  `Pagelove.GET` does not exist ("unknown function: GET", step 10). In
  bindings and triggers the store is reachable
  (`comp.eb.count-sum` and `reacting.context.polls-existing-document-guard`
  pass live), so the restriction belongs to QUERY.
- **pagelike before:** a QUERY program read the whole site: bare selectors,
  paths, globs and `Pagelove.GET` (R-SESSEL-209: "the same reach"). This
  ignored the reader's authorization, so a QUERY grant on one page exposed
  every page, including pages whose GET was denied.
- **pagelike now:** `sessel.Env.DocumentOnly`, set by the QUERY handler
  (`internal/query/handler.go`), confines the program to its target:
  - bare selectors search only the target;
  - only `from self` (and `Selector.execute()` with no argument) reaches it;
  - other sources are still evaluated, for their errors, but match nothing;
  - elements have no provenance;
  - `Pagelove.GET` is the TypeError "unknown function: GET".

  Bindings, triggers, validators and methods are unchanged. The case now
  expects `null`, `null`, `0`.
- **Rationale:** this is the default adopt-live, and it also strengthens a
  security boundary. QUERY is authorized on the request path alone
  (R-PROTO-74), and PageLove honours that by letting the program read nothing
  else. Keeping pagelike's site-wide reach would keep a cross-document read
  path that bypasses GET denials. It also settles protocol contradiction C-10
  (and the scope part of P-21) in favour of the QUERY examples' document
  scope.

### `sessel.query.sel.from-self-list` — adopt-live

- **Observations:** `live-2026-09-29/sessel-query-sel-from-self-list.json`
  (`… == "${P}/doc.html"` is false); probe from-and-path steps 1, 4, 7 and 8
  (`path()` is `null` for `from self` elements, for bare-selector elements, on
  a document written through the public plane, and for `self`).
- **PageLove:** `.path()` (and `.document()`) are null for every element of a
  QUERY program.
- **pagelike before:** it returned the document path.
- **pagelike now:** `path()` and `document()` are null, and `.microdata()`
  has no `@id`, under `DocumentOnly`. The case returns `path()` directly and
  expects `null`.
- **Rationale:** this follows from the same QUERY confinement as `from-path`,
  and it is harmless: a QUERY has only one document, so provenance carries no
  information there. The documented provenance (`"/team/benji.html"`) holds in
  bindings and triggers. The polls guard compares `p.path()` with the request
  path in a trigger, and that comparison passes live.

### `sessel.query.sel.sub-select-provenance` — adopt-live

- **Observations:** `live-2026-09-29/sessel-query-sel-sub-select-provenance.json`
  (`[["a","b"],0,false]`); probe from-and-path step 6 (`${li} from s` → 0,
  `${li} from [s]` → 0, `s.${ li }` → 2, and its `path()` → null).
- **PageLove:** sub-select works in memory. An element used as a `from`
  source matches nothing in a QUERY, and results have no path.
- **pagelike before:** an element source was equivalent to a sub-select
  (R-SESSEL-205), and results kept provenance.
- **pagelike now:** under `DocumentOnly`, element and List sources match
  nothing and `path()` is null. The case expects `[[a, b], 0, null]`.
  Outside QUERY, an element source still searches its descendants. That was
  not observed live and stays inferred.
- **Rationale:** same confinement as above. Sub-select, the documented way to
  search inside an element, is unaffected.

### `sessel.query.sel.element-getters` — adopt-live

- **Observations:** `live-2026-09-29/sessel-query-sel-element-getters.json`
  (step 1: `document()` of an element from another path is null; step 2:
  `<meta>` `.text()` is `""`); probe from-and-path step 5; probe values
  steps 20-21.
- **PageLove:**
  - Step 1 is the QUERY confinement: the other document is unreachable, so
    the chain ends in null.
  - `.text()` is never null. It is `""` for an element with no text (meta,
    empty span, a whitespace-only `<p>`, `new p {}`).
  - A stored element's text is whitespace-collapsed and trimmed
    (`"  a\n  b "` → `"a b"`, `" x <b> y </b> "` → `"x y"`).
  - A constructed element's text is raw (`" x "`).
  - `.value()` of a stored text-content element is collapsed the same way,
    and null when that is empty. Attribute values are verbatim.
- **pagelike before:** `text()` returned null for empty text and never
  normalised whitespace (R-SESSEL-221, "inferred", SP-33 open).
- **pagelike now:** `text()` and `value()` follow PageLove for stored elements
  in every context (`internal/sessel/elements.go`). `TextOf` of an element,
  microdata values and member access are unchanged. The case expects
  `["#main > h1:nth-child(1)", null]` and `["/x", "2026-01-02", "c", "", "$29.99"]`.
- **Rationale:** these are default adoptions. SP-33 was an open probe, and the
  docs already told authors to trim `.text()` themselves, so no program relies
  on the untrimmed form. The normalisation was observed in a QUERY. Applying
  it to stored elements everywhere is the simplest consistent reading, since
  "stored element" is a property of the value, not of the host. A full local
  run showed no other case depends on the old form.

### `sessel.query.sel.selector-type` — adopt-live

- **Observations:** `live-2026-09-29/sessel-query-sel-selector-type.json`
  (400 "unknown function: execute"); probe mutation steps 8-10.
- **PageLove:** `new Selector { "h1" }` without an import is an ordinary
  element, `<selector>h1</selector>`, so `.execute()` is unknown. With
  `@schema Selector url("https://pagelove.org/Selector")` (the form the
  Method-Elements docs use), the Selector type works: `execute()` is
  `from self` (1 match), `toString()` is the CSS, and `execute(path)` matches
  nothing in a QUERY.
- **pagelike before:** `Selector` was a built-in name, so the bare form
  constructed a Selector.
- **pagelike now:** `new Selector {…}` constructs the type only when the
  program declares (`@schema`) or binds the name
  (`classNamed`, `internal/sessel/construct.go`). `isa Selector` and the
  JSON encoding are unchanged. The case imports the schema and expects
  `[0, Welcome, h1]`. Two new steps assert the `<selector>` element and the
  400 for `.execute()` without the import. This also explains LO-10.
- **Rationale:** this is a default adoption that produces no incorrect data.
  The bare form fails loudly on both runtimes, and the documented import form
  behaves identically.

### `sessel.query.values.integer-and-float-division` — adopt-live

- **Observations:** `live-2026-09-29/sessel-query-values-integer-and-float-di.json`
  (`42.Float().String()` is `"42"`); probe values steps 1-4.
- **PageLove:** the text form of a Float (`.String()`, interpolation,
  `join`, construction text) is the shortest round-trip decimal: no `.0` and
  never an exponent (`"42"`, `"2.5"`, `"0.30000000000000004"`,
  `"10000000000000000"`, `"0.00000015"`, `"-0"`). The JSON encoding keeps
  `.0` on integral values (`42.0`, `100.0`, `1000000000000000.0`) and uses
  exponents outside `1e-5 <= |x| < 1e16`, written `1e+16`, `1.5e-7`, `1e-6`.
  A Liquid template still renders a Float sum as `100.0`
  (`live-2026-09-29/comp-eb-count-sum.json`).
- **pagelike before:** the text form also kept `.0` (R-SESSEL-92, SP-7
  open), and JSON wrote `1e16`.
- **pagelike now:** `TextFloat` gives the text form
  (`internal/sessel/value.go`). `FormatFloat` (JSON) writes `e+`. Liquid
  receives typed Floats, as before. The case expects
  `[2,2.5,"2","42",42.0]` byte for byte.
- **Rationale:** this is a default adoption. The number is the same, only its
  text differs, and PageLove is the runtime that programs are written against.
  The expression tests that used `.String()` to observe the Integer/Float
  kind now use `isa`.

### `sessel.query.values.try-catch-message` — adopt-live

- **Observations:** `live-2026-09-29/sessel-query-values-try-catch-message.json`;
  probe values steps 5-15.
- **PageLove:** an arithmetic TypeError's message is
  `type error: cannot use '<v>' in arithmetic`. It quotes the first
  non-numeric operand by its text:
  - `"a" + 1` and `1 + "a"` → `'a'`;
  - `true + 1` → `'true'`;
  - `[1] + 1` → `'1'`;
  - `[1, "b"] + 1` → `'1, b'`;
  - `{a: 1} + 1` → `'{"a":1}'`.

  Numeric Strings are not coerced (`"5" + 1` → `'5'`), and the same message
  applies to `*`. The category is `TypeError`. Division by zero is
  `RuntimeError` "division by zero", which was already pagelike's.
- **pagelike before:** "Cannot add String and Integer" (the docs' text,
  R-SESSEL-80/343).
- **pagelike now:** PageLove's message, for all four operators (`-` and `/`
  are inferred) whenever neither operand is null (`arith`,
  `internal/sessel/eval.go`). Null operands keep the old message, because
  null arithmetic was not probed.
- **Rationale:** this is a default adoption, since message text is data a
  program can show. The status and category are unchanged.

### `sessel.query.values.temporal-format` — keep-standard (method form adopted)

- **Observations:** `live-2026-09-29/sessel-query-values-temporal-format.json`
  (`d.dayOfWeek` is null); probe values steps 16-18 (`d.year()` …
  `d.inLeapYear()`, `dt.hour()` …, `p.months()` all answer);
  `live-2026-09-29-reconcile-sessel/sessel-query-values-temporal-format-live.json`
  (`[d.year, d.month, d.dayOfWeek, dt.hour]` are all null).
- **PageLove:** Temporal accessors answer only as zero-argument methods. The
  documented property form (`date.year`, `date.dayOfWeek`) is null.
  `PlainDateTime` has no `dayOfWeek()` ("runtime error: unsupported temporal
  method: plaindatetime.dayOfWeek()"), which is a PageLove gap.
- **pagelike before:** it had only the property form.
- **pagelike now:** accessors also answer as zero-argument methods on every
  Temporal type (`temporalMethod`, `internal/sessel/temporal_ops.go`).
  Property access still returns the value. The case uses `d.dayOfWeek()` and
  passes on both runtimes. The sibling `sessel.query.values.temporal-format.live`
  (`status: live-divergence`) asserts PageLove's nulls.
- **Rationale:** adopting the method form costs nothing. Adopting a null
  `date.year` would produce plainly incorrect data: a date's year is not
  null, and a program comparing it would silently take the wrong branch. The
  documented properties therefore stay (keep-standard). A program written for
  PageLove uses the method form, which pagelike now answers.

## Other cases updated from the same evidence

These `sessel` cases were not in the failing sample, and the task did not
allow running them live. They asserted the site-wide QUERY reach that the
probes refuted, so they were updated to the adopted behaviour
(`evidence: live-observed`, citing the probe steps that evaluate the same
expressions on live).

| Case id | Was | Now | Probe steps |
|---|---|---|---|
| `sessel.query.sel.self-and-document` | `(${h1} from document).count()` = 1 | 0 | from-and-path 8, 11 |
| `sessel.query.sel.glob-aggregate` | sum over `products/*` = 125 | 0 | 3, 12 |
| `sessel.query.sel.glob-order` | `[Cap, Nike]` | `[]` | 3, 11 |
| `sessel.query.sel.multi-source-and-block` | 3, 2, `[Nike, Welcome]`, `[Welcome, Nike]` | 0, 0, `[null, Welcome]`, `[Welcome]` | 3, 8, 11 |
| `sessel.query.sel.site-wide-default` | 3 documents | the target only: `[[Welcome], 0]` | 4, 11 |
| `sessel.query.sel.microdata` | `@id` = `<path>#widget` | `null` | 9 |

In-process expression tests (`harness/cases/sessel/expressions.yaml`), run by
`internal/sessel` with no QUERY confinement, were updated for the adopted
language changes:
- the Float text form, checked with `isa` for the numeric kind;
- the arithmetic messages;
- `text()` never null, and collapsed on stored elements;
- the `@schema Selector` import;
- `children()` in the mutation examples, plus new tests for the stored-element
  refusals.

Two Go tests changed:
- `internal/sessel/api_test.go` `TestEncodeJSON` imports `Selector`;
- `internal/server/isolation_test.go` queries with a bare selector instead of
  a bare `new Selector` (it still proves cross-site isolation).

New unit tests are in `internal/sessel/live0929_test.go`.

## Live-divergence sibling

| Sibling | Asserts (PageLove) | Kept pagelike case | Class |
|---|---|---|---|
| `sessel.query.values.temporal-format.live` | `date.year`, `date.month`, `date.dayOfWeek` and `datetime.hour` are null; `date.dayOfWeek()` is 1 | `sessel.query.values.temporal-format` (method form, passes on both) and the property tests in `expressions.yaml` | keep-standard |

It passed live in run 3.

## Probes

| Probe | What it isolates | Final run |
|---|---|---|
| `sessel.query.probe-0929.from-and-path` | QUERY confinement: path, glob, relative and element sources; bare selectors; provenance; `document`; block and multi-source `from`; microdata `@id`; `Pagelove.GET`; the updated cases' expressions | pass (12 steps) |
| `sessel.query.probe-0929.values` | Float text and JSON forms, arithmetic messages, Temporal accessor methods, empty and whitespace `text()`/`value()` | pass (21 steps) |
| `sessel.query.probe-0929.mutation-and-selector` | `children()` versus sub-select handles, stored-element errors, `remove()` no-op, `Selector` with and without `@schema` | pass (10 steps) |

## Open points for the integrator

- **Protocol spec.** `docs/spec/protocol.md` C-10 / P-21 (QUERY scope) are
  settled by `from-path`: QUERY is document-scoped. The protocol spec belongs
  to another area and was not edited. R-SESSEL-204/205/209 in
  `docs/spec/sessel.md` carry the note.
- **Shared code.** `internal/query/handler.go` (the QUERY handler) now sets
  `DocumentOnly`. No protocol, javascript or other case changed outcome in the
  full local run (1327 passed, 0 failed).
- **Inferred extensions:**
  - the arithmetic message for `-` and `/`;
  - `text()` normalisation outside QUERY;
  - element `from` sources outside QUERY, which still search descendants.

  All three are consistent with the observations but were not probed
  directly.
- **TypeError messages.** PageLove prefixes its TypeError messages with
  `type error: `. pagelike uses the prefix only in the messages observed here
  (arithmetic, `insertBefore`, the text setter, `replaceWith`). Other
  messages keep pagelike's wording.
