# Live reconciliation decisions: composing-liquid (2026-09-29)


> **Update (LO-15, same day).** The keep-standard part of
> `liquid.escape.escape-filter` is superseded: pagelike now builds documents
> from tokens and stores them serialized, as PageLove does, so markup inside a
> Liquid string literal is an element and the page fails with 500 on both.
> The sibling `liquid.escape.escape-filter.live` became the normal case
> `liquid.escape.escape-filter-inline-500`. See `serialization.md`.

This file records how pagelike handled the six cases of the composing-liquid
area that diverged in the sampled live PageLove run of 2026-09-29 (one case
per feature). It follows the decision policy in
[`docs/compat/decisions.md`](../decisions.md) (adopt-live by default;
keep-documented-security and keep-standard with a `.live` sibling; harness
artifacts fixed in the case or harness).

**Evidence.**
- Sampled run: `harness/observations/live-2026-09-29/` (a case id maps to its
  file by turning dots into dashes and cutting the name to 40 characters).
- Reconciliation runs:
  `harness/observations/live-2026-09-29-reconcile-composing-liquid/`.
  - `run1-probes/`: run 1, seven probe cases with loose expectations.
  - `run2/`: run 2, the six fixed cases, both `.live` siblings and the seven
    probes (15 passed). `liquid.escape.escape-filter` still used an
    intermediate input then (strings taken from microdata).
  - The folder itself: run 3, the final run of the same 15 cases (15 passed).
    `RUN-SUMMARY.txt` describes all three.
- Budget: 3 live runs, 37 case runs (7 + 15 + 15), about 265 requests in all
  (recorded steps plus setup uploads and teardown).

## Summary

| Case | Class | pagelike change | `.live` sibling |
|---|---|---|---|
| `comp.pag.links-and-slice` | adopt-live | Links carry the path and the paginator's own page parameter only, order first/last/prev/next, `title` in the `Link` header | — |
| `comp.pag.first-page-no-prev` | adopt-live | Same link form | — |
| `comp.pag.multiple-without-ids-422` | adopt-live | Several paginators without ids are allowed (200) | — |
| `liquid.compose.range-over-rendered-output` | harness-artifact (the incidental range-literal 500 is keep-standard) | None | `liquid.compose.range-over-rendered-output.live` |
| `liquid.escape.escape-filter` | adopt-live after fixing the input (the incidental raw-markup 500 is keep-standard) | Character references inside `{{ }}` / `{% %}` are decoded | `liquid.escape.escape-filter.live` |
| `liquid.filters-exp.where-reject-find` | adopt-live | `map` over anything but an array gives `[]` | — |

| Failures | adopt-live | keep-documented-security | keep-standard | harness artifact |
|---|---|---|---|---|
| 6 | 5 | 0 | 0 (2 incidental, with siblings) | 1 |

Each case is counted once, under its main class. The two keep-standard
decisions concern inputs the cases used incidentally; they are measured by
the two `.live` siblings. One harness fix was needed along the way
([Harness change](#harness-change)).

## Cases

### comp.pag.links-and-slice

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/comp-pag-links-and-slice.json`; probes
  `run1-probes/comp-pag-probe-0929-single-with-id.json`,
  `run1-probes/comp-pag-probe-0929-single-without-id.json`,
  `run2/comp-pag-probe-0929-single-with-id.json`; final run
  `comp-pag-links-and-slice.json`.
- **PageLove:** for `?paginate:page=2&paginate:length=3` on a 7-item list with
  `id="contacts"`, it serves Dave, Eve, Frank and appends to `<head>`
  `<link rel="first" href="/…/contacts.html?paginate:contacts:page=1" title="contacts">`,
  then `last` (page 3), `prev` (1) and `next` (3). Each `href` is the request
  path plus `paginate:<id>:page=N` only: the length, other parameters
  (`q=smith`, `x=1`) and other paginators' state are dropped. Without an id
  the key is `paginate:page` and there is no `title`. The `Link` header has
  the same URLs, in the same order, as `<…>; rel="first"; title="contacts"`.
  A paginator reads `paginate:<id>:page` / `:length` when present and
  otherwise `paginate:page` / `paginate:length` (the prefixed form wins when
  both are sent).
- **pagelike now:** the same links, order, `title` attribute and `title`
  header parameter, and the same parameter reading (`internal/compose/paginate.go`,
  `paginateParam`).
- **Rationale:** the documented example (query-only `href` keeping
  `&amp;paginate:length=3`, order first/prev/next/last) is not what PageLove
  serves. The live form is a format choice that clients do not parse, so the
  default applies. Dropping the length and other parameters is a PageLove bug
  (following `next` from a page of length 3 leads to a page of the default
  length), but it only affects navigation links, not data, and an author who
  needs state-preserving links can build them from `request.query`; under the
  policy that is adopt-live. This is the one judgment call in this area that
  the integrator may want to revisit (keep-standard would keep the documented
  carry-forward and add a sibling).

### comp.pag.first-page-no-prev

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/comp-pag-first-page-no-prev.json`; final
  run `comp-pag-first-page-no-prev.json`.
- **PageLove:** page 1 has `first`, `last` and `next`
  (`/…/contacts.html?paginate:contacts:page=2`, `title="contacts"`) and no
  `prev`.
- **pagelike now:** the same.
- **Rationale:** the absence of `prev` on page 1 was already right; only the
  link form differed, and it follows the decision above.

### comp.pag.multiple-without-ids-422

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/comp-pag-multiple-without-ids-422.json`;
  probes `run1-probes/comp-pag-probe-0929-without-ids.json` and
  `run2/comp-pag-probe-0929-without-ids.json`; final run
  `comp-pag-multiple-without-ids-422.json`.
- **PageLove:** 200. Both id-less lists are paginated, both driven by
  `paginate:page` / `paginate:length`, and each emits its own links
  (`?paginate:page=N`, no `title`). The case's document has no `<head>` in its
  source, and PageLove then adds no `<link>` elements (the `Link` header
  still carries all six values). On a page that mixes a paginator with an id
  and one without, `paginate:page=2` moves both and `paginate:a:page=2` moves
  only the one with the id.
- **pagelike now:** no 422 for several paginators; the same parameter
  sharing; `<link>` elements only when `<head>` is written in the source.
  The 422 for a `p:paginate` value that is not a positive integer is
  unchanged.
- **Rationale:** the documented 422 is stricter than PageLove. Accepting the
  page is lenient and loses no data, so the default applies.

### liquid.compose.range-over-rendered-output

- **Class:** harness-artifact. The incidental range-literal behaviour is
  keep-standard, measured by `liquid.compose.range-over-rendered-output.live`.
- **Observations:** `live-2026-09-29/liquid-compose-range-over-rendered-outpu.json`;
  probe `run1-probes/liquid-compose-probe-0929-range-literal.json` (and
  `run2/`); final run `liquid-compose-range-over-rendered-outpu.json` and
  `liquid-compose-range-over-rendered-live.json`.
- **PageLove:** every range literal fails the template: `(1..3)`,
  `(1 .. 3)`, `(1..b)` and `{{ (1..3) | join: "," }}` give 500
  "Liquid parse error at None (node None): expected RParen, got Dot";
  `(a..b)` and `(a..3)` give "expected field name after '.', got Dot". The
  whole page fails, and so do selector reads of it. With a split list
  (`"1,2,3" | split: ","`) the case's selector reads return the generated
  `<li class="n1">` and `<li class="n3">` (206).
- **pagelike now:** unchanged. Ranges work (R-LIQ-70, R-LIQ-74). The case now
  loops over a split list, and passes on both targets.
- **Rationale:** the case tests composition order (a selector read sees
  template output); the range literal was an incidental input that PageLove
  rejects for an unrelated reason, so the case was fixed. The rejection
  itself is a 500 on valid, standard Liquid that the spec lists as
  supported; adopting it would make pagelike fail valid templates, so it is
  keep-standard with a sibling. Other live cases that use ranges
  incidentally are listed under [Open items](#open-items).

### liquid.escape.escape-filter

- **Class:** adopt-live, after fixing the case's input. The incidental
  raw-markup behaviour is keep-standard, measured by
  `liquid.escape.escape-filter.live`.
- **Observations:** `live-2026-09-29/liquid-escape-escape-filter.json`; probe
  `run1-probes/liquid-escape-probe-0929-source-text.json` and
  `run2/liquid-escape-probe-0929-source-text.json`; final run
  `liquid-escape-escape-filter.json` and `liquid-escape-escape-filter-live.json`.
- **PageLove:** the docs example written inline,
  `<p p:template="text/liquid">{{ "<a href='x'>" | escape }}…</p>`, fails the
  page with 500 "unterminated `{{` interpolation marker": PageLove parses
  `<a href='x'>` as an element before Liquid runs, so the Liquid source is
  broken. The probes show how PageLove builds the source: inside `{{ }}` and
  `{% %}`, character references are decoded (`{{ "&amp;" | size }}` is `1`,
  `{{ "&lt;a href='x'&gt;" | escape }}` escapes `<a href='x'>`,
  `title="{{ 'a&amp;b' | size }}"` is `3`, `{{ "a & b" | escape }}` gives
  `a &amp; b` although PageLove stores it as `{{ "a &amp; b" | escape }}`,
  and `{% if 2 > 1 %}` works), while literal text keeps its references
  (`x &lt;i&gt;y&lt;/i&gt;` stays text). The rendered output is re-serialized
  (`&#39;` is served as `'`, `&quot;` as `"`).
- **pagelike now:** the preprocessor decodes character references inside
  Liquid outputs and tag arguments of a named HTML host where the HTML parser
  would decode them: in text, `title`/`textarea` and attribute values; not in
  `script`/`style`, comments, raw blocks, XML documents, or `Render` with
  `Host{}` (`internal/liquid/preprocess.go`, `htmlctx.go` `decodesRefs`). The
  host's raw source is still what is rendered (R-LIQ-10), so raw markup inside
  a Liquid string still renders. Output bytes are still emitted as rendered.
- **Rationale:** the case wrote markup inside HTML text; any HTML parser
  reads that as an element, and PageLove does. The case is about the escape
  filter, so its input now writes the docs example as HTML requires
  (`{{ "&lt;a href='x'&gt;" | escape }}`) and compares the body as HTML. That
  form relies on the decoding, which pagelike did not do (R-LIQ-13 said
  references reach Liquid undecoded): a real difference, adopted by default.
  It also matters beyond this case, because PageLove stores re-serialized
  HTML: a `&` in Liquid code comes back from PageLove (for example through
  the migration tool) as `&amp;`, as the probe's stored copy shows, and an
  HTML serializer writes `<` and `>` in text as `&lt;` and `&gt;` too.
  PageLove's failure on raw markup inside a string literal is a 500 on a
  template pagelike's raw-source model renders correctly (R-LIQ-10, C-3), so
  that part is keep-standard. The output re-serialization is the same
  difference as LO-9 (HTML re-serialized on write) and stays deferred with
  it; the case compares as HTML so it does not depend on it.

### liquid.filters-exp.where-reject-find

- **Class:** adopt-live.
- **Observations:** `live-2026-09-29/liquid-filters-exp-where-reject-find.json`;
  probes `run1-probes/liquid-filters-exp-probe-0929-map-single.json` and
  `run2/liquid-filters-exp-probe-0929-map-single.json`; final run
  `liquid-filters-exp-where-reject-find.json`.
- **PageLove:** `find_exp` returns the item (`found.name` is `Retro`), but
  `map` over a single value does not wrap it in an array: for an item, a
  hash, a string, a number or nil, `map: "name"` gives an empty array
  (`== empty`, size 0, `json: 0` prints `[]`). So the docs pattern
  `find_exp: … | map: "name"` (and `find: … | map: "title"`) renders
  nothing.
- **pagelike now:** `map` returns `[]` for any input that is not an array or
  a range (`internal/liquid/filters_array.go`). The case reads `found.name`
  for `find` and asserts the empty `find | map` separately (`find_map`).
- **Rationale:** R-LIQ-101's coercion of a single value to `[value]` is
  Shopify's behaviour, not PageLove's. Rendering nothing for a single-value
  `map` is lenient and misreports no data; a template that works on PageLove
  never relies on the wrapping, so the default applies. Only `map` was
  changed; the other single-value differences the probe saw are open items.

## Live-divergence siblings

They have `status: live-divergence`: skipped locally, run only with
`--target live`, where they assert what PageLove does. Both passed in the
final run.

| Sibling | Asserts (PageLove) | Kept pagelike behaviour | Class |
|---|---|---|---|
| `liquid.compose.range-over-rendered-output.live` | `{% for i in (1..3) %}` fails the page: 500 "expected RParen, got Dot" | Standard ranges (R-LIQ-70, R-LIQ-74) | keep-standard |
| `liquid.escape.escape-filter.live` | `{{ "<a href='x'>" \| escape }}` written inline fails the page: 500 "unterminated `{{` interpolation marker" | Raw host source (R-LIQ-10) | keep-standard |

## Probes

All are `evidence: live-observed` and ran in all three runs (loose
expectations in run 1, then asserting what PageLove served).

| Probe | Status | Question answered |
|---|---|---|
| `comp.pag.probe-0929.single-with-id` | — | Prefixed vs unprefixed page and length, which wins, what the links carry (`q`, length) |
| `comp.pag.probe-0929.single-without-id` | — | Link form without an id (no `title`, `paginate:page`) |
| `comp.pag.probe-0929.two-with-ids` | — | Independent state, `paginate:page` fallback for both, per-paginator links |
| `comp.pag.probe-0929.without-ids` | — | 200 for id-less paginators, shared parameters, a mixed page |
| `liquid.compose.probe-0929.range-literal-forms` | live-divergence | Every spelling of a range literal fails; a split list works |
| `liquid.escape.probe-0929.source-text` | live-divergence | Where references are decoded, output re-serialization, stored form |
| `liquid.filters-exp.probe-0929.map-single-value` | — | `find_exp`'s result; `map` over an item, hash, string, number, nil |

## Other cases changed to follow these decisions

These cases were not in the sampled run and were not run live themselves;
their new expectations follow the probes, which serve the same constructs.

| Case | Change | Follows |
|---|---|---|
| `comp.pag.last-page-no-next` | `prev` href is `${P}/contacts.html?paginate:contacts:page=2` | links-and-slice |
| `comp.pag.preserve-other-params` | Links do not carry `q=smith` (title changed; id kept) | links-and-slice |
| `comp.pag.multiple-with-ids` | Each paginator's links carry only its own page | links-and-slice |
| `comp.pag.link-header` | `Link` value `</…?paginate:contacts:page=1>; rel="first"; title="contacts"` | links-and-slice |
| `liquid.output.entities-not-decoded` | `{{ "a &amp; b" \| size }}` is 5 (title changed; id kept) | escape-filter |
| `liquid.filters-string.escaping-family` | `escape_once` inputs spell references as `&amp;lt;` etc. so the filter still sees references | escape-filter |
| `liquid.filters-array.querying-objects` | `find` read as a property; `find \| map` asserted empty | where-reject-find |
| `liquid.filters-exp.numeric-predicates` | `find_exp` read as a property | where-reject-find |

Unit tests changed or added: `internal/compose` (`TestPagination`,
`TestPaginateParam` replacing `TestWithParam`), `internal/liquid`
(`TestPreprocess`, `TestCompileCache`, `TestArrayFilters`, and the corpus
examples `find`, `find_exp` and `find_exp/microdata-strings`), `harness`
(`TestObservationName`).

Spec notes ("live 2026-09-29: …"): `docs/spec/composing.md` R-COMP-131,
R-COMP-132, R-COMP-134 and the §21 status row; `docs/spec/liquid.md`
R-LIQ-10, R-LIQ-13, R-LIQ-74 and R-LIQ-145.

## Harness change

`SaveObservations` named a `.live` sibling's file by the same 40-character
cut as its case when the id is long, so in run 2 the sibling's observations
overwrote the case's. `harness.ObservationName` keeps the `-live` suffix
(`liquid-compose-range-over-rendered-live.json`); names that were not cut are
unchanged, and `LoadOutcomes` reads the case id from the file, so recorded
runs load as before. The run-2 file was renamed to match its content.

## Open items

Observed while probing, outside the six cases, and not reconciled here:

1. **Whitespace inside a paginated element.** PageLove removes all element
   children and appends the page's slice after the remaining text nodes
   (`<ul id="contacts">\n\n<li>Carol</li><li>Dave</li></ul>`); pagelike cuts
   the other children in place. No case asserts it. Moving kept children
   would also move write-through anchors, so it is not a local change.
2. **Cache-Control.** Paginated pages, and pages with a resource binding,
   are served `private` live; pagelike sends `public, max-age=5` (LO-7). Not
   asserted by any case.
3. **`json`.** Without an indent argument it is a filter error live
   ("json() indent argument must be an integer"), although R-LIQ-190 makes
   the argument optional; `json: 0` prints one element per line.
4. **Other array filters on a single item.** Live, `where` and `sort` return
   the item unchanged and `concat` onto an item returns the argument only;
   pagelike coerces to `[item]` (R-LIQ-101).
5. **Output and storage re-serialization (LO-9).** PageLove re-serializes
   rendered output (`&#39;` → `'`) and stores re-serialized sources, so
   `&#123;&#123; 1 | plus: 1 &#125;&#125;` in a template renders `2` live and
   stays literal text in pagelike.
6. **Live cases that use range literals incidentally** and so fail live on
   the kept range difference: `liquid.compose.pagination-after-templates`,
   `liquid.filters-random.random-minimum-counts`,
   `liquid.output.whitespace-preserved`, `liquid.output.whitespace-control`,
   `liquid.output.table-rows-in-place`, `liquid.tags.loops`,
   `liquid.tags.reversed-limit-order`. `liquid.compose.write-into-rendered-output`
   passed live only because its template failed to compose (the 416 names
   the Liquid parse error), so it does not test what it claims live.
7. **Live cases with raw markup inside Liquid strings**, which fail live on
   the kept raw-source difference: `liquid.filters-string.escaping-family`
   (`xml_escape`, `strip_html` inputs) and `liquid.filters-data.json-not-html-escaped`.
