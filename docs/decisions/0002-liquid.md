# 0002: Liquid engine for PageLove-compatible templates

- Status: proposed (spike complete, 2026-09-28)
- Spike: `research/spikes/liquid/` (exploratory; not part of the public repository — its results are recorded here).
- Scope: `pagelove:template="text/liquid"` rendering (stage 5, `internal/liquid`)

## Decision

Build `internal/liquid` on **github.com/osteele/liquid v1.9.2 (MIT)**. Use its
public lower-level packages (`render`, `expressions`, `filters`, `tags`,
`values`, `parser`) rather than the `liquid.Engine` facade. Add a pagelike
layer of about 2,400 lines, about 950 of which are filter code:

1. **PageLove filters.** Add 35 missing filters and override 30 stock filters
   whose behaviour differs from PageLove's.
2. **`*Item` values.** Bound elements are exposed as microdata items that
   implement `values.Value`.
3. **Source preprocessing.** This layer does four things:
   - error degradation for each output expression
   - keyword-only filter arguments
   - `blank`/`empty` literals
   - rendering `{% else %}` when a loop collection is nil
4. **Per-render budget.** It covers work, time, output bytes, assigned memory
   and the 32-level limit on `*_exp` nesting.

Do **not** write our own engine. Do **not** adopt Notifuse/liquidgo or
go-liquid/liquid.

After live differential runs have confirmed PageLove's exact semantics, carry a
**thin patch fork** through a `replace` directive to `sky-valley/liquid`. The
fork is only for the things the public API cannot express:

- numeric-string comparison
- keyword-only filter arguments in the grammar
- `blank`/`empty` literals
- the order in which loop modifiers apply
- three panic and error-message fixes

Offer each patch upstream. The preprocessing workarounds cover every PageLove
example in the corpus until then.

In short: **the library plus custom filters now, backed by a thin fork later.
No rewrite.**

## Evidence

The corpus is `pl/corpus.go`. It has 190 cases:

- **Filter examples:** every example on the Liquid docs pages. The `/all/`
  page is a superset of the individual pages, which I diffed against it. Each
  example has equivalent data.
- **Pages from the docs:** the Templating, Resource-Binding,
  Expression-Binding, JS-Binding, Request-Document, QUERY and
  Parameterized-Routes pages, plus build-a-blog's index, archive and Atom feed.
- **Every `text/liquid` template in `research/upstream`:**
  - polls listing
  - polls `new-poll.html`, checked against the poll page that real PageLove
    generated
  - shop index
  - shop admin products page
  - shop auth partial
  - kanban `whoami`
  - pagelove-dev SKILL.md snippets
- **Runtime probes:** error degradation, budget and isolation.

Bound elements are built from real HTML using `x/net/html` and the microdata
value algorithm. The shop and polls tests use the upstream data files as they
are.

Each case's expected output is labelled with where it came from:

- **documented:** the docs print the output (68 cases).
- **reconstructed:** the docs print the output but the scrape lost the
  template source (3 cases).
- **derived:** worked out from the docs' prose (119 cases).

The pagelike engine is osteele plus the `pl` layer. Each stock engine was run
with the same data as plain maps.

| engine | std/97 | ext/61 | app/23 | rt/9 | all/190 | documented/68 |
|---|---|---|---|---|---|---|
| **pagelike** (osteele + `pl`) | 95 | 59 | 23 | 9 | **186** | **68** |
| osteele v1.9.2 stock | 70 | 6 | 18 | 1 | 95 | 48 |
| Notifuse/liquidgo (Jan 2026 snapshot) | 77 | 11 | 18 | 1 | 107 | 47 |
| go-liquid/liquid v0.1.0 | 81 | 1 | 18 | 2 | 102 | 47 |

The four pagelike misses are all library semantics. Each one is recorded as a
known gap in the corpus:

- `where_exp` and `find_exp` over microdata strings: `"34" >= 18` is false.
- `for … reversed limit:2` gives `54`, where Shopify gives `21`.
- `tablerow` omits Shopify's newlines.

In `BenchmarkPageLikeBlog500` (500 bound posts, then `where`, `sort`,
`reverse`, and 50 rendered), pagelike takes **0.47 ms/op**. Stock osteele on
plain maps takes 0.73 ms/op. Preprocessing plus compiling one template takes
about 76 µs, and it is cached for each pooled instance. Concurrent renders pass
under `-race`.

## Library assessment (osteele/liquid v1.9.2)

**Maintenance.** The library is active: releases v1.8.0 (2026-02), and
v1.9.0, v1.9.1 and v1.9.2 (2026-08). There is one principal maintainer, with
outside contributors. Its runtime dependencies are `osteele/tuesday` (MIT) and
`gopkg.in/yaml.v2` (Apache-2.0). `golang.org/x/tools` is needed only at build
time, through `tool`. About 10k lines. The grammar is goyacc
(`expressions/expressions.y`).

**License.** MIT (© 2017 Oliver Steele). It is compatible with pagelike's
Apache-2.0 licence. The alternatives are liquidgo (MIT) and go-liquid
(BSD-3-Clause).

### Standard tags and filters that work

**Tags:**
- `assign`, `capture`
- `if`/`elsif`/`else`, `unless`, `case`/`when`/`else`
- `for`:
  - `else`
  - `limit:`, including `limit: 4` with a space, which go-liquid rejects
  - `offset:`
  - `reversed`
  - `break`/`continue`
  - `forloop.*`
  - ranges
- `cycle`, `tablerow`, `raw`, `comment`
- `include`/`render`, which must be disabled (see below)

**Expressions:**
- `==`, `!=`, `<`, `>`, `<=`, `>=`
- `contains`, `and`/`or`
- `a.b`, `a['@id']`, `a[0]`
- `.size`/`.first`/`.last` on arrays; `.size` on maps and strings
- filters inside `if` conditions, as in `{% if xs | has: "k", true %}`
- `\n` `\t` `\"` `\\` escapes inside double-quoted literals (since 1.9.0);
  single quotes are literal

**Whitespace control.** `{%-`, `-%}`, `{{-` and `-}}` all work. The Templating
page's `{%- for -%}` listing matches exactly.

**Filters that match PageLove unchanged:**
- downcase, upcase, capitalize
- strip, lstrip, rstrip
- url_encode, url_decode
- replace, replace_first, replace_last
- remove, remove_first, remove_last
- append, prepend
- slice, truncate, truncatewords
- default, ceil, floor, at_least, at_most
- uniq, where (one- and two-argument forms)
- the plus/minus/times family on integers

`sum`, `where`, `at_least`, `at_most`, `replace_last` and `remove_last` were
added in 1.9.0.

**Tags added in the prototype.** `echo`, `increment` and `decrement` are added
through `AddTag`. `liquid`, `ifchanged` and `{% # %}` are absent, and neither
PageLove's docs nor its apps use them.

### Registering custom filters and tags

- **Filters.** Register with `cfg.AddFilter(name, fn)`, or
  `Engine.RegisterFilter`.
  - `fn` is any Go function with one or more inputs and one or two outputs. A
    second output must be an `error`.
  - Arguments are converted by `values.Convert`.
  - An optional argument can be declared as a `func(T) T` parameter, which
    acts as a default.
  - Keyword arguments arrive as a trailing `map[string]any`.
  - A parameter of type `expressions.Closure` makes the library parse the
    string argument as an expression bound to the current scope. That is how
    the `*_exp` filters are built (`pred.Bind(name, item).Evaluate()`).
  - Registering a name again overrides the stock filter.
  - `filters.AddStandardFilters` takes any `FilterDictionary`. The prototype
    passes a *metering* dictionary that wraps every stock filter with
    `reflect.MakeFunc` so that it is charged to the budget.
- **Tags.** `render.Config.AddTag(name, TagCompiler)` gives a compile-time
  hook: the tag arguments are parsed once.
  - `FindTagDefinition` lets us wrap an existing tag. The prototype wraps
    `assign` to charge the memory an assignment holds.
  - `AddBlock(name).Clause(...).Compiler(...)` defines block tags.
  - The `Engine.RegisterTag`/`RegisterBlock` facade only gets a render-time
    `render.Context`.
- **Per-render state.** Filters have no access to their context. The
  prototype pools *instances*, each holding a config, its own mutable state and
  a template cache. A render checks out an instance for exclusive use, so
  filter closures can keep depth, budget and clock without globals.

### Drops and property access for element lists

- **`Drop`.** The `Drop` interface is `ToLiquid() any`, which returns a proxy.
  The library does not guarantee how many times it is called, so every lookup
  is eager.
- **`values.Value`.** This interface is exported: `Interface`, `Int`, `Equal`,
  `Less`, `Contains`, `IndexValue`, `PropertyValue`, `Test`. `ValueOf` passes
  implementations through untouched, so `pl.Item` implements it directly. This
  gives:
  - lazy microdata extraction, cached with `sync.Once`
  - `item.prop`
  - `item['@id']`, which is `<path>#<id>` as the polls app requires
  - `item.size`
  - identity that survives filters
  - an explicit `String()` and `MarshalJSON`, in document order

  A repeated itemprop is `[]any`, a single one is a string, and a nested
  itemscope is a nested `*Item`. The shop's `p.variant | join` needs both
  shapes to work.
- **Stock filter pitfalls:**
  - `sort: "key"` only sorts maps (`reflect.Map`) and silently does nothing on
    structs or custom Values.
  - `sort_natural: "key"` **panics** when a map lacks the key.
  - The `size` filter returns 0 for maps and objects.

  All three are overridden in `pl`.
- **Comparison dispatch.** Comparisons dispatch on the **left** operand only.
  `a.Less(b)` gives no hook when the literal is on the left, and `x > 18` is
  compiled as `18.Less(x)`. A custom Value therefore cannot add numeric-string
  coercion (see Gap G1).

### Errors and strictness

- **Undefined names.** Undefined variables and properties render as empty.
  `StrictVariables()` makes them errors, including in `{% if nope %}`.
  PageLove is lax, as the docs' `request.auth.*` examples show.
- **Undefined filters.** An undefined filter is an error. `LaxFilters()`
  passes the value through instead.
- **Any error aborts the whole render.** The first error wins. There is no
  lax or warn mode and no error hook for each output expression.
- **Panics escape `Render`.** Two ways I found:
  - A syntax error inside a `where_exp` predicate string.
  - `sort_natural` by a missing key.

  The caller must `recover`.
- **Exponential error messages.** `expressions.FilterError.Error()` quotes the
  inner error with `%q`. Nested expression filters, as in the docs'
  self-referential predicate, build a message of about 2^32 bytes: the process
  hung until I flattened errors at each nesting level.
- **Default template store reads the working directory.** `{% include
  "go.mod" %}` on a stock engine read the server's working directory. It must
  be replaced with a store that denies every read, as the prototype's
  `denyStore` does.

### Date formatting differences

| | osteele `date` | PageLove `date` |
|---|---|---|
| default format | `%a, %b %d, %y` → `Sun, Apr 12, 26` | `%Y-%m-%d` |
| `2026-04-12` | midnight in **server-local TZ** (the test machine printed `+0100`) | midnight UTC |
| `2026-04-12T13:45:00` (no offset) | **error** | UTC |
| `…+02:00` | keeps the offset (13:45) | normalises to UTC (11:45) |
| `"today"` | error | supported |
| directives | tuesday: every Ruby directive (`%e`, `%-d`, `%I`, `%s`, …) | only `%Y %m %d %H %M %S %B %b %A %a %j %p %Z %z %%`; unknown directives pass through (`%e` stays `%e`) |
| Jekyll `date_to_*`, `date_add`, `unix_to_iso` | absent | present |
| unparseable input | render error | inline error marker |

The prototype replaces `date` completely. The replacement has its own strftime
subset and parses everything as UTC.

### Blocking issues (cannot be fixed by registering filters or tags)

| # | Issue | Evidence | Prototype workaround | Durable fix |
|---|---|---|---|---|
| G1 | Microdata values are strings, and the library never compares numeric strings with numbers (`"34" >= 18` is false). PageLove's docs (`where_exp: "u", "u.age >= 18"`; "use `sort` for numeric order") imply it does. | corpus `*_exp/microdata-strings` | none. `pl`'s own filters (`sort`, `where`, `has`, …) coerce, but expressions evaluated by the library cannot. | fork: a PageLove comparison mode in `values.Less`/`Equal`, or typed microdata values from schemas. **Confirm live first.** |
| G2 | Keyword-only filter arguments are a syntax error: `{{ 24 \| random: upper: 3 }}`, `argon2: memory: …`. The grammar requires a positional argument first. The polls app's `new-poll.html` fails on line 4. | corpus `random/*`, `argon2/*`, `polls/new-poll-template` | rewrite `\| f: k: v` to `\| f: nil, k: v` | fork: one grammar production (`filter_params: KEYWORD expr`) plus goyacc; upstreamable |
| G3 | No `blank`/`empty` literals. They parse as undefined variables, so `shot != blank` is always true, which is the bug the shop index comment warns about. | corpus `tag/blank-empty` | bind sentinel Values and rewrite `x != blank` to `blank != x` | fork (grammar and `values`); upstreamable |
| G4 | No per-expression error degradation. PageLove renders an inline `https://pagelove.org/Error` element for a failing `{{ }}`, or nothing in attribute context. Control flow and `assign` fail the render. | corpus `error/*` | rewrite `{{ x }}` to `{% pl_out x %}`, or to `{% pl_out_attr x %}` inside tags, attributes and script/style/title/textarea. A small HTML tokenizer state machine picks which. | keep this in pagelike. Only we know the HTML context. A fork hook on `ObjectNode` errors would remove the rewrite. |
| G5 | No execution budget. Loops, filter walks and `assign` have no hooks; `FRender` only checks on write. | corpus `budget/*`: a stock empty loop over 50M items runs to completion | inject `{% pl_tick %}` into every `for`/`tablerow`; metered filter wrapper that pre-charges the list length; wrapped `assign`; bounded writer; time checked at each charge | fork hook for loop iterations, optional; `capture` still needs a wrapper |
| G6 | `reversed` is applied before `offset`/`limit`, in any position. Shopify and LiquidJS differ, and upstream documents the difference. | `tag/for-reversed-limit` | none | fork, if live PageLove confirms the Shopify or LiquidJS order |
| G7 | `{% for x in nil %}…{% else %}` renders nothing. | `tag/for-else` | rewrite the collection to `coll \| pl_list` | fork, one line |
| G8 | Panics escape (G-errors above), and FilterError messages blow up. | probes | `recover` in `Render` and `pl_out`; flatten `*_exp` errors | fork plus upstream |

Other expression-syntax notes:
- Multi-line `{{ }}` works when the newlines only surround the expression.
- `tablerow` markup lacks Shopify's newlines. That is minor, and PageLove's
  output is unverified.

## Gap list: PageLove filters missing from osteele (35)

Doc semantics are summarised. Every one is implemented in `pl/` and covered by
the corpus.

**String:**
- `normalize_whitespace`: collapse whitespace runs to one space, then trim.
- `cgi_escape`: form encoding, where a space becomes `+`.
- `uri_escape`: encodeURI; keeps `;/?:@&=+$,` and turns a space into `%20`.
- `xml_escape`: escape `& < > " '`, with `'` as `&#39;`.
- `array_to_sentence_string [conn="and"]`: Oxford comma.
- `number_of_words`: whitespace-separated count.
- `slugify`: lowercase, runs of non-alphanumerics become `-`, trim `-`.

**Number:**
- `to_integer`: floats truncate toward zero; numeric strings parse;
  `true`/`false` become 1/0; anything else becomes 0; saturates at int64;
  NaN becomes 0.

**Array:**
- `reject`, `find`, `find_index`, `has`: `field[, value]` matching. Without a
  value, match truthiness. Non-objects never match.
- `group_by`: `[{name, items}]` in the order keys are first seen.
- `push`, `unshift`, `pop`, `shift`: always return a new array; nil becomes
  `[]`; a scalar is promoted to a list.

**Date** (every input is normalised to UTC):
- `date_add secs`: returns ISO `…Z`.
- `unix_to_iso`.
- `date_to_string`: `%d %b %Y`.
- `date_to_long_string`: `%d %B %Y`.
- `date_to_rfc822`: `%a, %d %b %Y %H:%M:%S +0000`.
- `date_to_xmlschema`: `…+00:00`.

**Security:**
- `sha256`: hex.
- `bcrypt [cost=12]`.
- `argon2`:
  - Argon2id, PHC output by default.
  - Keyword arguments `format` (`phc`|`raw`), `salt` (required for raw,
    8 bytes or more), `memory=19456` (8 or more), `time=2` (1 or more),
    `length=32` (4–64).

**Random** (from the OS RNG):
- `random`:
  - The input is the length.
  - Keyword arguments `upper`, `lower`, `digits`, `symbols` and `alphanumeric`
    each take a bool or a minimum count. Also `url_safe`, `chars` and
    `chars_min`.
- `diceware [n=3, 1–10]`: hyphen-joined words from a list of more than 1,600.

**Data:**
- `jsonify`: alias of `json`, including the indent argument.

**Expression variants:** `where_exp`, `reject_exp`, `find_exp`,
`find_index_exp`, `has_exp` and `group_by_exp`.
- The signature is `coll | f: "var", "<liquid expression>"`.
- Nested use is allowed.
- Nesting deeper than 32 levels is an error: an inline marker in `{{ }}`, a
  failed render in `assign`.

**Stock filters overridden for PageLove semantics (30):**
- `json`: indent argument; no `<` HTML escaping; items keep document
  order.
- `inspect`.
- `date`: see the date table above.
- `newline_to_br`: keeps the `\n`.
- `strip_newlines`: also removes `\r`.
- `strip_html`: drops `<script>`/`<style>` contents.
- `escape`: `"` becomes `&quot;`, not Go's `&#34;`.
- `escape_once`: stock turns `&copy;` into `©`.
- `split`: `"" | split: ","` is `[""]`; stock trims trailing empty strings.
- `size`: counts object keys.
- `first`/`last`: return the first or last character of a string.
- `join`, `map`, `concat`, `compact`, `reverse`: coerce a scalar or nil to a
  list. Stock errors: the shop admin page fails with `can't convert
  string(One size)`.
- `sort`/`sort_natural`:
  - missing values go last
  - different kinds are grouped
  - numeric strings sort numerically in `sort` and as text in `sort_natural`
  - they work on any Value
  - no panic
- `where`: the string `"true"` matches `true`, because microdata is strings.
  This is an assumption.
- `sum`: numeric strings count; the result is an integer when whole.
- `plus`, `minus`, `times`: integer when both operands are integers, numeric
  strings included.
- `divided_by`: floor division; dividing by zero returns the input.
- `modulo`: modulo zero returns the input; stock gives NaN.
- `abs` and `round`: `round` with 0 places returns an integer.
- Output rendering: integral floats render Ruby-style (`100.0`), which is what
  the Expression-Binding doc shows.

## Alternatives considered

- **Fork osteele outright now.** This would be premature: G1, G6 and the exact
  error markup need live PageLove captures first. The public packages already
  cover everything else. We keep a patch fork in reserve and hold it to under
  about 300 lines of diff.
- **Write our own engine.** It would give full control: JS-like coercion, and
  budget and HTML-context hooks in the core. But it means 3–4k lines plus the
  long tail of Shopify semantics that osteele already matches:
  - whitespace trimming
  - `forloop`, `cycle`, `tablerow` and `case`
  - Liquid truthiness
  - ranges
  - the closure machinery

  The PageLove dialect differences sit in about 8 places (G1–G8). They do not
  justify a rewrite.
- **Notifuse/liquidgo** (MIT, a port of Ruby Liquid 5.10). It has good ideas:
  `ResourceLimits` (render/assign scores, output length) and an
  `ExceptionRenderer` for inline errors. It also has the full Ruby tag set.
  Against it:
  - It is a pseudo-version snapshot with no tagged releases. The last commit
    was 2026-01.
  - Its lax mode silently drops unknown filters and filter errors.
  - Hashes render in Ruby `inspect` form (`{"a"=>1}`), and `json` gives the
    same.
  - Filters are struct methods, the Ruby strainer design. There is no closure
    parameter for `*_exp`.
  - Stock score: 107/190.
- **go-liquid/liquid** (BSD-3). It is v0.1.0, released once in 2026-08. It
  aims to match the Ruby gem byte for byte and has lax, warn and strict modes.
  Against it:
  - no custom tags, no keyword arguments, no closure parameters
  - it rejects `limit: 4` with a space, which breaks build-a-blog and the
    polls listing
  - it mis-parses `item['@id' ]` in an `unless`
  - Stock score: 102/190.

## Integration notes for `internal/liquid` and `internal/compose`

1. **Take template source from the raw bytes, not a re-serialised DOM.** An
   HTML parse would foster-parent `{% %}` text out of `<table>/<tr>`. The
   pagelove-dev SKILL.md calls this a known failure. The polls
   `new-poll.html` (2026-09) puts `{% for %}` inside `<tr>`, and the stored
   generated poll shows it works on PageLove today. Re-serialising would also
   change entities and quotes. `internal/dom` must therefore keep source
   offsets of the `pagelove:template` element's inner content, for example
   with `html.Tokenizer.Raw()`. The rendered output is then parsed as a
   fragment in the element's context.
2. **Output replaces the element's children.** The element itself stays. The
   `p:template` attribute and the `r:`/`e:`/`j:` attributes are stripped. The
   generated poll keeps `<html lang="en" xmlns:p=…>` but loses `p:template`.
   The XML-Documents page says XML keeps every `xmlns:` declaration.
3. **Bindings.**
   - A resource binding becomes `[]any` of `*pl.Item`, evaluated lazily.
   - Expression bindings:
     - Integral JavaScript numbers become `int64`: the docs show `60`,
       not `60.0`.
     - Sessel floats stay `float64`: the docs show `Sum: 100.0`.
   - `request` has `method`, `path`, `query`, `headers` (lowercase),
     `params`, `body` (form fields) and `auth{username, claims, roles}`.
     The kanban app reads `request.auth.role` in the singular, which is
     undocumented.
   - Make `request` a Value that records a read of `auth.*`, so compose can
     mark the response `Cache-Control: private`.
4. **Budget.** Replace the prototype's per-render counters with the request
   budget from the plan: work, time and memory share one allowance. Budget
   exhaustion fails the request (a 503 elsewhere in the docs), never a partial
   page. Cap `bcrypt` cost and `argon2` memory and time so one filter call
   cannot eat the budget. The prototype caps bcrypt at 16.
5. **Security.**
   - Use a template store that denies every read.
   - Expose no Go methods or callables through bindings.
   - Resource bindings read the whole site; that is PageLove's documented
     trust model.
   - AuthorizationRule fields are **not** Liquid (the docs say `{{` is literal
     there). Never route them through this engine.
6. **One `Renderer` per process.** It pools instances; compile caches live in
   each instance and should become a bounded LRU. Pass the document's content
   type to `Preprocess` so that XML treats `<title>` as a normal element, not
   HTML raw text.
7. **Resource creation (`POST` to a template).** The template sits on
   `<html>` and must render a `<base href>`. `random: lower: true,
   digits: true` is how the polls app mints ids, so G2's rewrite is required.

## Open questions to pin with live differential cases (harness)

The prototype makes a documented guess for each of these.

1. **The inline error element:** its exact markup, and whether a `{{ }}`
   *syntax* error degrades locally or fails the page.
2. **Autoescaping.** Does PageLove auto-escape `{{ }}` output? The escape doc
   says its result is "marked safe, so it is not escaped again".
3. **G1.** Do `"34" >= 18` and `sort` over `"9","10"` coerce numbers? Are
   microdata values ever typed by a Schema?
4. **Loop modifiers.** What is the order of `reversed` versus `limit`/`offset`,
   and what markup does `tablerow` produce?
5. **Rendering.** How are floats rendered (`15.0` vs `15`)? What does
   `{{ item }}` render? Is `@id` a path or an absolute URL? What key order does
   JSON use?
6. **`split`.** Does it drop trailing empty strings (`"a,b,," | split: ","`)?
7. **String literals.** Do single-quoted literals process escapes?
8. **Dates and bindings.** Is `today` midnight? Is there a `now` variable (the
   `has_exp` doc example uses one)? Is `request.auth.role` an alias of
   `roles`?
9. **Random data.** What is `random`'s symbol set, and what is `diceware`'s
   word list? We need our own licensed list; the prototype has a placeholder.
10. **Item shape.** Are single-valued itemprops scalars and repeated ones
    arrays? What does a binding of non-itemscope elements expose?

## Running the spike

The spike is not part of the public repository. Its corpus lives on as
`internal/liquid/corpus_test.go` (`go test ./internal/liquid`).

