# 0003: HTML parsing, serialization, XML and CSS selectors

- **Status:** proposed
- **Date:** 2026-09-28
- **Spike:** `research/spikes/html-selectors/` (exploratory; not part of the public repository — its results are recorded here).
- **Evidence snapshot:** docs 2026-09-28 (`research/docs/2026-09-28/md`), upstream apps listed in `research/COMMITS.txt`
- **Affects:** `internal/dom`, `internal/selector`, `internal/engine` (store/read), `internal/dav` (QUERY), `internal/microdata`, the planned `internal/compose` XML path, and the harness comparators

## Decision

1. **Keep `golang.org/x/net/html` as the HTML tokenizer and tree builder, as a dependency and not a fork.** It agrees with html5ever's tree builder on every case tested. Wrap it with two small fidelity fixups that re-tokenize the source (both in the spike, `dom/comments.go` and `dom/attrorder.go`):
   - it HTML-unescapes comment text;
   - since v0.55.0 it sorts the attributes of formatting elements (`<a>`, `<b>`, `<em>`, …) in place.
2. **Do not serialize with `html.Render`.** Use a WHATWG serializer compatible with html5ever (`htmlser`). On all 58 upstream HTML files and 28 edge-case documents, `htmlser` output is byte-identical to html5ever 0.39. `html.Render` output is identical on 0 of the 58.
3. **Fork cascadia v1.3.5 into `internal/selector`**, keeping the BSD-2-Clause notice (`LICENSE.cascadia` plus a `NOTICE` entry, with the upstream copyright header kept on each derived file).
   - Custom pseudo-classes cannot be added without a fork: the parser rejects unknown names in a closed `switch`, and every compound or combinator type is unexported.
   - Stock cascadia also gets standard CSS wrong in ways PageLove apps hit today: `:has(> x)`, `:scope`, `:is`/`:where`, unanchored `:has()`, SVG case, `:empty`, and a case-insensitive `:contains`.
   - The fork prototype adds about 900 lines to cascadia's roughly 2,100. It agrees with Servo's `selectors` crate on every standard selector tested, and it implements all PageLove extensions.
4. **Parse XML-family documents into the same `*html.Node` trees** with `encoding/xml` `RawToken`, a namespace stack, and a small XML serializer (`xmldom`). This keeps one selector engine and one composition pipeline for HTML and XML. Reject etree (it has a separate tree type and no CSS selectors) and `encoding/xml`'s `Encoder` (it mangles namespaces).
5. **Canonical selectors.** Use `html > body > tag:nth-child(n) > …`, or an id-anchored form `#unique-id > …`, with CSS-escaped identifiers. Both round-trip for every element of every upstream document through the fork, stock cascadia and Servo.
6. **Strongly consider source-preserving writes, and pin the behaviour with a live observation first.** Every whole-document response in the docs looks like PageLove serves stored bytes rather than a re-serialized DOM. The spike shows that splicing edits into the stored source works on x/net/html: 56 of 58 documents correlate cleanly, and 9,638 of 9,656 edits produce the same DOM as a re-serialization.
7. **Parse prefixed elements (`<p:include>`, `<t:hello/>`) as elements the author placed** by rewriting them to `<template data-pagelike-el=…>` before parsing (`dom/prefixed.go`). This needs no fork. The docs and the ATS app depend on this, and a spec parser does neither half of it.

## Method

- **Oracle.** `oracle/` is a Rust binary built against html5ever 0.39 with markup5ever_rcdom, xml5ever 0.39, and scraper 0.27 (Servo `selectors`). PageLove is a Rust server. Its exact crates are not public: the docs and the `pagelove/prompts` material say only "Rust" and "DOMFs". html5ever and Servo selectors are therefore the most likely stack, and the oracle stands in for it. Where the oracle and PageLove's published examples disagree, this document says so.
- **Corpus.** Every `*.html`, `*.js`, `*.mjs`, `*.svg` and `*.xml` file under `research/upstream`, and every per-page docs markdown file.
- **Tests.** `go test ./... -count=1` in the spike runs everything. The numbers below come from those tests.

## 1. Parsing documents and fragments in context

`html.ParseFragment(r, context)` implements the spec's fragment algorithm faithfully. The context must be the live element from the tree, so that its namespace, the table-section modes, `<select>`, `<template>` and any `<form>` ancestor all apply. `DataAtom` must be consistent with `Data`, which is always true for parsed nodes.

The fragment cases below all matched html5ever's `parse_fragment` (`dom/dom_test.go`):

| Context ← markup | Result | Note |
|---|---|---|
| `tbody` ← `<tr><td>a</td></tr>` | `<tr>…</tr>` | pagelove-polls and pagelove-ats POST rows into `tbody#…` and parse the response inside `<table><tbody>`. |
| `table` ← `<tr>…` | `<tbody><tr>…</tr></tbody>` | `tbody` is synthesized. |
| `tbody` ← `<td>a</td>` | `<tr><td>a</td></tr>` | |
| `body` or `div` ← `<tr><td>a</td></tr>` | text `a` | A parser that always uses a body context would break the ATS and polls apps. |
| `ul` ← `<li>x</li>` | `<li>x</li>` | |
| `select` ← `<option>`; `select` ← `<div>…<option>` | kept | Both parsers keep the `<div>`, per the new customizable-select rules. |
| `template` ← `<tr>…` | `<tr>…</tr>` | |
| `svg` ← `<circle/><foreignObject><p>` | SVG `circle`, HTML `p` inside | Self-closing is honoured in foreign content. In a `body` context the same markup gives an HTML `<circle>`. |
| `body` ← `just text` | one text node | |
| `body` ← `<li>a</li><li>b</li>` | 2 nodes | PageLove's behaviour for multi-node bodies is unknown; see Open questions. |
| `body` ← `\n  <!-- c --> <p>x</p>\n` | 5 nodes (whitespace, comment, whitespace, p, whitespace) | `internal/dom.ParseFragment` already trims the body. |
| `html` ← `<body class=x>…` | `<head></head><body class="x">…` | A PUT that replaces `<body>` must take the body and drop the implied head. |
| `body` ← `<body class=x><p>b</p>` | `<p>b</p>` | The body tag is dropped. |
| `textarea` / `script` ← markup | one text node | RCDATA and raw text. |
| `tbody` ← `{% for %}<tr>…</tr>{% endfor %}` | 3 nodes | See the Liquid note below. |

Placement contexts (`dom.ContextFor`):

- `append` and `prepend` use the target as the context.
- `replace` (PUT), `before` and `after` use the target's parent.
- `before` or `after` on the root element returns `ErrNoSiblingSlot`, which matches the documented 400.
- Replacing the root element needs a whole-document parse.

Whole documents follow the spec:

- Whitespace between `<html>` and `<head>` is dropped.
- Whitespace after `</body>` and `</html>` is moved inside `<body>`.
- `html`, `head` and `body` are always materialized. `dom.ParseDocument` records which of them were implied (tokenizer pre-scan), and `htmlser` can omit them again (`OmitImplied`, stable under re-parse).

### Two places where PageLove is evidently not a plain spec parser

1. **Foster parenting.** pagelove-ats writes `<table><p:include resource="/admin/index.html" selector="#roles-body"></p:include></table>` in `site/roles.html` and `site/roles/:role_name/index.html`. It then reads the result with `document.querySelectorAll('#all-roles-data tr[itemtype]')`.
   - html5ever and x/net/html both move `<p:include>` in front of the `<table>`. The included `<tbody>` would then land outside the table, and a browser would discard its row tags.
   - For the app to work, PageLove must leave the include where it was written.
2. **Self-closing prefixed elements.** The docs Includes example writes `<p:include … />` followed by `<main>…</main>`, and the response keeps `<main>` after the inlined header.
   - A spec parser ignores `/>` on non-void HTML elements, so it makes `<main>` a child of `<p:include>`. The include replacement would then delete `<main>`.

`dom.ParseDocumentPrefixedAsElements` handles both cases without forking x/net/html. It rewrites prefixed start and end tags (found by the tokenizer, so text inside `<script>` is untouched) into `<template data-pagelike-el="p:include" …>`, closing them immediately when self-closed. It then renames the elements back after parsing. `<template>` is legal in every insertion mode. Tested in `TestPrefixedElements`.

- **Side effect:** a prefixed element in `<head>` stays in `<head>`, where a spec parser would open `<body>` for it. This is probably also PageLove's behaviour, but it needs a live observation.

**Liquid inside tables.** Liquid source inside tables is foster-parented as well (pagelove-polls `templates/new-poll.html` has `{% for %}` between `<th>` cells). A `p:template` on the root must therefore render against the raw source text before HTML parsing, never against a serialized DOM. This belongs to the Liquid spike and is recorded here because it constrains `internal/dom`.

## 2. Serialization fidelity

`htmlser` versus html5ever 0.39 versus `html.Render` (x/net/html v0.59.0), from `htmlser/serialize_test.go`:

| Aspect | html5ever 0.39 (and browsers) | `html.Render` | `htmlser` |
|---|---|---|---|
| DOCTYPE | `<!DOCTYPE html>`; public and system ids dropped | keeps `PUBLIC "…" "…"` | same as html5ever |
| Void elements | `<br>`, `<meta …>` | `<br/>`, `<meta …/>` | `<br>` |
| Boolean attributes | `disabled=""`, `itemscope=""` | same | same |
| Attribute escaping | `&amp;`, `&quot;`, `&nbsp;`, **`&lt;` and `&gt;`** (WHATWG 2025 change) | `&amp;`, `&#34;`, `&#39;`, `&lt;`, `&gt;`; U+00A0 written literally | same as html5ever (`LegacyAttrEscaping` switches `<` and `>` off, for older html5ever) |
| Text escaping | `&amp;`, `&lt;`, `&gt;`, `&nbsp;`; `'` and `"` literal | adds `&#39;` and `&#34;`, which puts `where: &#34;listed&#34;` into Liquid source | same as html5ever |
| Entities in source | decoded; re-escaped only as above (`&copy;` becomes `©`, `&#39;` becomes `'`) | same | same |
| Comments | verbatim | the parser unescapes the text and `Render` re-escapes `&`, so `<!-- &amp; -->` becomes `<!-- & -->` becomes `<!-- &amp; -->` | verbatim, after `dom.FixComments` |
| Attribute order | source order | **sorted on formatting elements** (x/net/html ≥ v0.55.0; v0.54.0 is fine) | source order, after `dom.FixAttrOrder` |
| `<script>`, `<style>`, `<xmp>`, `<iframe>`, `<noembed>`, `<noframes>`, `<plaintext>` | raw | raw | raw |
| `<noscript>` | raw when scripting is enabled (html5ever's default) | always raw | raw unless `ScriptingDisabled` |
| `<pre>`, `<textarea>`, `<listing>` with a leading LF | no extra LF, so **each parse and serialize cycle loses one newline** | adds an LF, so the round trip is stable | html5ever behaviour by default; `PreserveLeadingNewline` for storage |
| `<template>` | contents serialized | contents serialized | contents serialized |
| SVG and MathML | adjusted case (`viewBox`, `linearGradient`, `foreignObject`); `xlink:href` and `xmlns:xlink` prefixes; `<path/>` becomes `<path></path>` | same | same |
| Names on HTML elements | lower-cased; the prefix stays part of the name: `p:template`, `r:posts`, `xmlns:r`, `pagelove:template` verbatim; `E:Mixed` becomes `e:mixed`; `r:postsByAuthor` becomes `r:postsbyauthor` | same | same |

Round trip, parse then render then parse then render: `htmlser` reaches a fixed point after one pass on all 58 upstream files and all edge cases. The one exception is the `<pre>` leading-newline case, where html5ever is not a fixed point either. With `PreserveLeadingNewline` it is a fixed point everywhere.

Corpus (`corpus/TestCorpusSerialization`):

| Check | Result |
|---|---|
| `htmlser` byte-identical to html5ever | 58/58 |
| `html.Render` byte-identical to html5ever | 0/58 |
| `htmlser` fixed point | 58/58 |
| Storage variant (`PreserveLeadingNewline`) fixed point | 58/58 |
| Byte-identical to the source file (even with `OmitImplied`) | 0/58 |

### What PageLove actually emits looks source-preserving

Every whole-document HTTP response in the docs shows these traits (for example DELETE-method "Verify the removal", Includes, Pagination, Expression-Binding, Transient-Elements, the Liquid templating example, QUERY, and ShapeConstraint):

- no implied `<head></head>`;
- bare boolean attributes (`<div itemscope itemtype=…>`);
- whitespace left between `<html>` and `<head>`, and after `</body>`;
- whitespace left behind by deleted elements (`\n  \n`).

html5ever serialization shows none of these. The pagelove-polls sample poll (`site/polls/kfd47o4zqd.html`) looks the same: bare `itemscope` and `crossorigin`, POSTed `<tr>` rows stored exactly as `poll.js` builds them. Its provenance is unclear, though, because it predates the current template.

Taken together, this is medium-confidence evidence that PageLove keeps the stored source and splices edits into it. Stored-source splicing would also explain why no `<head>` appears.

The spike shows this is feasible on x/net/html (`dom/spans.go`, `corpus/TestSpliceFeasibility`):

- Re-running the tokenizer and matching explicit start tags to elements in document order gives byte spans for the start tag, the content and the end tag.
- **Correlation is clean in 56 of 58 documents.** The two that are not are exactly the ATS foster-parenting cases.
- **9,638 of 9,656 edits give the same DOM as the re-serialization path.** The edits were delete, append a comment, and replace an element with its own serialization, on every element. The 18 mismatches are Liquid text foster-parented in a template, and one `<pre>` leading-newline case.
- The design that follows:
  - Serve stored bytes when nothing changed.
  - For a write, splice the new text at the span, re-parse, and check that the DOM equals the DOM-mutation result.
  - Fall back to `htmlser` (with `PreserveLeadingNewline`) when correlation is not clean or the check fails.
- **It also sidesteps every x/net/html fidelity problem above for untouched bytes.** Today `engine.store` runs `html.Render` over the whole document on every selector write, so a single write rewrites every `'`, `<br>` and `<a>` attribute order in the page.

## 3. XML documents

Requirements, from the docs (reference/composing-pages/XML-Documents):

- names are case-sensitive; there are no void or raw-text elements; processing instructions are retained;
- empty elements self-close (`<entry/>`); all `xmlns:` declarations are preserved;
- the same composition directives and microdata `@read` resolvers apply;
- an XML document with no PageLove namespaces and no microdata is served unchanged;
- the docs' own example `<p:stamp site>` is not well-formed, so the parser must be lenient.

| Option | Verdict |
|---|---|
| **`encoding/xml` RawToken into `*html.Node` + custom serializer (`xmldom`)** | **Recommended.** Qualified names are kept as written in `Data` and in attribute `Key`. `Namespace` holds the resolved URI, or the sentinel `urn:x-pagelike:xml:no-namespace`, so it is never `""` and selectors compare names case-sensitively. PIs and DOCTYPE become `RawNode`s with their exact source bytes. CDATA is detected with `InputOffset` and preserved. Parsing is lenient: valueless attributes become `""`, and HTML entities and mismatched end tags are accepted. Output is well-formed and a fixed point. RSS and SVG round-trip byte-identically. The Atom example differs only as the docs require (`<meta …/>` and `<p:stamp site=""/>` self-close). 4 of 7 upstream SVG/XML files are byte-identical; the other 3 differ only by `<path …></path>` becoming `<path …/>`. The fork's selectors work unchanged: `entry` is not `Entry`, and `p\:stamp`, `[e\:site]` and `meta:value-equals('*')` all match. |
| etree (beevik v1.8.1) | Good fidelity: case, PIs, CDATA (`PreserveCData`) and self-closing all survive. However, it rewrites `'` to `&apos;` in text and attributes and turns a valueless `site` into `site="site"`. Most importantly it has its own tree type with an XPath-like query language, so pagelike would need a second selector engine and a second composition pipeline. |
| `encoding/xml` Token plus `Encoder` | Unusable. It emits `xmlns:_xmlns="xmlns"`, `_xmlns:e=…` and `Sessel:site=…`, drops the `p:` prefix, and adds `xmlns` to every element. |
| xml5ever 0.39 serializer (oracle) | Not what PageLove emits. It drops root `xmlns` declarations, adds `xmlns:p=""`, never self-closes, and loses CDATA. PageLove must therefore use its own serializer, which supports the xmldom approach. |
| The current HTML parser (the vertical slice parses XML with `dom.Parse`) | Wrong. `<?xml …?>` becomes the comment `<!--?xml …?-->`, `Entry` becomes `entry`, RSS `<link>` is treated as void so its text escapes, and `<title>` becomes RCDATA. |

Microdata on XML has to switch on local element names, not `DataAtom`: XML nodes have `DataAtom == 0`. The fork's `MicrodataValue` does this; `internal/microdata` must do the same.

## 4. Selectors

**Stock cascadia v1.3.5 supports:**

- type, `*`, `#id`, `.class`;
- attributes with `=`, `~=`, `|=`, `^=`, `$=`, `*=` and the `i` flag, plus the non-standard `!=` and `#=` (regex);
- the descendant, `>`, `+` and `~` combinators, and selector lists;
- `:not(list)`, `:has(list)` (descendants only, **not anchored**);
- `:nth-child`, `:nth-last-child`, `:nth-of-type`, `:nth-last-of-type` (An+B, odd and even), `:first-`, `:last-` and `:only-child`/`-of-type`;
- `:empty` (whitespace counts as empty), `:root`, `:link`, `:lang`, `:enabled`, `:disabled`, `:checked`; `:hover`, `:focus` and similar never match;
- the non-standard `:contains` and `:containsOwn` (case-insensitive and lower-cased), `:matches` and `:matchesOwn` (regex), `:haschild`, `:input`;
- pseudo-elements only through `ParseWithPseudoElement`.

**It does not support** `:is`, `:where`, `:scope`, relative `:has(> x)`, `:has(+ x)` or `:has(~ x)`, `:nth-child(An+B of S)`, the `s` attribute flag, or namespace syntax `[ns|attr]`.

Feature matrix (`selector/TestFeatureMatrix`): 86 selectors run against one document through stock cascadia, the fork and Servo.

- **Stock cascadia disagrees with Servo on 24.**
  - Parse errors: `:is`, `:where`, `:scope`, `ul:has(> p)`, `li:has(+ li)`, `li:has(~ p)`, `div:has(> section > div > p)`, `#cfg:has(> meta, > nav)`, `[a='b' s]`.
  - Wrong matches: `linearGradient`, `[viewBox]` and `foreignObject p` match nothing, because the selector is lower-cased but SVG names are camelCase. `li:empty` matches a whitespace-only `<li>`. `li:has(ul span)` and `section:has(#outer p)` match, because `:has()` is not anchored to its subject.
  - Accepted where Servo rejects: `:containsOwn`, `:matches`, `:haschild`, `:input`, `[a!=b]`.
- **The fork disagrees with Servo on 3, all deliberate:**
  - `:nth-child(… of S)` twice: scraper leaves `of S` disabled, and browsers support it;
  - PageLove's `:contains`, which Servo does not have.

**Selectors actually used** (`corpus/TestCorpusSelectors`): there are 421 unique strings. They come from:

- `Range` headers in the docs and apps;
- AuthorizationRule `selector` values and ShapeConstraint `constraint` values;
- `r:` bindings, `p:include selector=`, and Sessel `${…}` literals;
- QUERY `text/css-selector` bodies;
- selector strings the JS clients send (`PL.post(path, '#boards', …)`, `'#' + id + ' [data-f="card-title"]'`) and what the client generators produce (`#\31 23`, `#p > DIV:nth-child(1) > SPAN:nth-child(1)`, `tag[itemprop="x"]`);
- `querySelector` strings in the apps.

Results:

- **The fork parses every real selector.** The 22 strings it rejects are docs placeholders (`:contains()`, `:less-than(n)`, `…`) or Sessel and JS expressions that the extractor caught.
- **Stock cascadia rejects 36 strings that the fork accepts**, among them:
  - the ShapeConstraint `:has(> th[itemprop="name"])` (pagelove-polls);
  - the Sessel literals `[itemtype=…]:has([itemprop='slug']:value-equals(…))` (pagelove-shop routes);
  - every `:isa`, `:value-equals` and `:only` example;
  - the beta-js `:scope > …` queries.
- The 131 selectors bound to a document (rule selectors and inline `querySelector` strings) were evaluated against their own documents: **0 differ from Servo.**
- `[p\:template]`, `[pagelove\:template]` and `p\:stamp` work everywhere, because the prefix is part of the local name. `[p|template]` is rejected by all three engines. The docs promise it through a `Namespace` request header, so the fork should gain `ns|name` parsing that resolves prefixes against in-scope `xmlns:` declarations. This is not prototyped.
- **Selector strings inside Sessel literals are not plain CSS.** They contain Sessel expressions in argument and value positions: `:value-equals(request.params.slug)`, `:greater-than(threshold)`, `[data-cat=category.first().text()]`. The Sessel layer must evaluate these, as it does selector functions, before calling the selector parser.

**`<template>` contents:**

- x/net/html keeps template contents as ordinary children, so cascadia matches `template li` and `.tpl`.
- Browsers never match into template contents.
- scraper traverses them but gives them a DocumentFragment parent.
- The fork's `Select` and `SelectFirst` follow browsers, and `:has()` does not descend into templates.

## 5. PageLove extensions and cascadia internals

- **Why a fork is required.** `parsePseudoclassSelector` is a closed `switch` whose `default` branch returns `unknown pseudoclass`. Every `Sel` implementation (`combinedSelector`, `compoundSelector`, `attrSelector` and the rest) is unexported, and `Matcher.Match(n)` takes no context.
  - Doing it without a fork would mean writing a whole parser and combinator engine next to cascadia, which amounts to a fork anyway.
  - Rewriting strings (for example `:equals('x')` into `:matches(^x$)`) cannot express microdata values, numeric comparison, `:only` or `:isa`.
- **The fork prototype** (`selector/`, BSD notice kept):
  - `Options.Pseudo` registry: registered names take precedence over built-ins. `PseudoContext` hands the extension the raw argument text, and can parse a selector list or relative selector list.
  - `:is()`, `:where()` and `:scope`. Scope is bound per query without shared mutable state, so compiled selectors stay safe for concurrent use.
  - `:has()` with relative selectors (`>`, `+`, `~`), anchored to the subject.
  - `:nth-child(An+B of S)`.
  - Case sensitivity: type and attribute names are case-insensitive for HTML elements and case-sensitive for foreign and XML elements.
  - Browser semantics for `:empty`.
  - Strict mode, which rejects cascadia-only syntax.
- **PageLove extensions implemented** (`selector/ext.go`, tested in `TestPageLoveExtensions` against every example in the Selector-Extensions docs):
  - text: `:contains`, `:equals`, `:value-contains`, `:value-equals`, with an optional `, i` flag (ASCII fold); text is normalized with NFC, trimmed, and whitespace runs collapsed;
  - numeric: `:less-than`, `:greater-than`, `:value-less-than`, `:value-greater-than` (strict decimal; text that is not a number never matches);
  - `:only(relative-list)`;
  - `:isa(url)`, taking the inheritance map as an injected function (matches nothing without schemas).
  - CSS escapes work inside arguments, for example `p:contains('caf\E9 ')`.
  - The task asked for two extensions as a prototype; all ten are in, because the hook made each one short.
- **`count()`, `text-of()`, `value-of()` and `attr-of()` are not pseudo-classes.** `selector.ExpandFunctions` is a pre-pass that respects strings and nesting and replaces each call with its value (an integer or a CSS string) through a `FuncEvaluator` that works across the site, before the selector is parsed.

## 6. Canonical document-rooted selectors

`canon.Path` produces `html > body > ul:nth-child(3) > li:nth-child(2)`: `head` and `body` appear without an index, and the root is `html` or `:root`. `canon.Anchored` starts from the nearest ancestor-or-self whose id is unique in the document, for example `#list > li:nth-child(2)`. Identifiers are escaped as `CSS.escape` would, so `p\:include:nth-child(2)` and `#\31 23` both work.

Results over 3,772 elements (7,544 selectors) in all upstream documents:

- **0 failures** with the fork (first match is the element, exactly one match), with stock cascadia, or with Servo.
- Average length: 74 characters for the path form, 49 for the anchored form; maximum 214.

Recommended use:

- **QUERY parts** (`Content-Range: selector <css>`): use the anchored form. It survives more edits and is what the beta-js generator also prefers.
- **Unique ids:** check them on every generation, since documents do contain duplicates.
- **Emit the RFC form `selector <css>`** (unit, space, value). beta-js and pagelove-primitives parse both that and `selector=`.
- **GET, PUT, POST and DELETE** echo the request selector in `Content-Range`. The docs show `Range: selector=main` answered with `content-range: selector main`.
- **SSE `selector`:** the docs example shows `main > h1`. pagelove-polls and beta-js apply it with `document.querySelector` on the client DOM, and beta-js's echo suppression compares it to the request selector string. Only standard CSS can go into it, never PageLove extensions.
- **Compute canonical selectors on the DOM the client sees**, which is the composed page.

The vertical slice's current `selector.Path` fails on prefixed tag names: it produces `p:stamp:nth-child(1)` without escaping. Use `canon.Ident`.

## Recommendation: depend or fork

| Library | Decision |
|---|---|
| `golang.org/x/net/html` | **Depend.** Pin the version, keep `FixComments` and `FixAttrOrder` with regression tests that fail once upstream fixes the attribute-sort regression (introduced in v0.55.0), and report both issues upstream. Use `ParseDocumentPrefixedAsElements` for stored PageLove documents. Use `ParseFragment` with the real context element for every write. |
| `github.com/andybalholm/cascadia` | **Fork into `internal/selector`.** Copy v1.3.5, keep `LICENSE.cascadia` and each file's copyright header, and change the cascadia line in the repo `NOTICE` (it already lists cascadia as a dependency) to say it is a modified copy in `internal/selector`. BSD-2-Clause is compatible with the repository's Apache-2.0 licence. Start from the spike's `selector/` directory. Remove the cascadia-only syntax, or keep it behind `Strict: false` for tests only. |
| Serializer | **Own** (`htmlser`, about 220 lines). Default to html5ever-compatible output for responses and fragments. Use `PreserveLeadingNewline` for anything that is stored and parsed again. |
| XML | **Own** (`xmldom`, on `encoding/xml` RawToken). Serve documents that composition does not change as stored bytes. |
| etree, xml5ever | Do not use. |

## Serialization normalizations the harness should apply

The harness compares pagelike with live PageLove. Until live observations settle the source-preservation question, normalize both sides as follows.

1. **Whole documents:** compare as DOMs (`body_html`). Parse both sides with the HTML algorithm and compare the canonical `htmlser` form. This absorbs all of:
   - attribute quoting style, bare versus `=""` boolean attributes, and `<br>` versus `<br/>`;
   - character references (`&#39;`/`'`, `&#34;`/`&quot;`/`"`, `&nbsp;`/U+00A0, `&copy;`/`©`);
   - `<` and `>` in attribute values;
   - upper versus lower case in tag and attribute names;
   - DOCTYPE public and system ids;
   - implied `<html>`, `<head>` and `<body>`;
   - whitespace before `<head>` and after `</body>` and `</html>`.
2. **Leading LF:** strip one leading LF from the content of `pre`, `textarea` and `listing` before comparing, because of the html5ever round-trip loss.
3. **Attribute order stays significant.** html5ever keeps source order, and so do the PageLove docs examples. Provide an opt-in `attr_order: ignore` only for cases that exercise x/net/html builds without the fixup.
4. **Comments:** compare verbatim; do not unescape.
5. **Fragments (206 bodies, POST echoes):** trim surrounding whitespace, then compare as DOMs **parsed in the target's context**. Parse `<tr>` bodies inside `<table><tbody>`, as pagelove-ats's `parseRowHtml` and pagelove-polls' `<template>` parse do.
6. **`Content-Range`:** normalize `selector=X` and `selector X` to `selector X`. Compare QUERY part selectors by what they select (resolve both on the recorded document), not as text, unless a case pins the format.
7. **SSE `mutation` payloads:**
   - compare the microdata fields;
   - compare `body` as a DOM parsed in the context of the element that `selector` matched;
   - compare `selector` by what it resolves to, unless the case pins the literal (beta-js echo suppression relies on the literal).
8. **XML:** compare after `xmldom` parse and render on both sides. This covers self-closing empties, entity expansion, and CDATA versus escaped text with the same value. Keep namespace declarations and their order significant, and keep PIs verbatim.
9. **`Accept-Ranges`:** accept `selector, bytes` or `bytes`; the Azure Front Door rewrite is documented.
10. The existing normalizations stay: hosts, dates, ETag literals, multipart boundaries, SSE ids.

## Integration notes for the current vertical slice

These were found by reading the code and were not changed here.

- **`internal/dom.Render`, `OuterHTML`, `InnerHTML`** use `html.Render`. Switch them to `htmlser`. Because `engine.ElementETag` hashes `OuterHTML`, element ETags will change once; that is fine before release.
- **`internal/dom.Parse`** passes `ParseOptionEnableScripting(false)`, but html5ever parses with scripting enabled, so `<noscript>` contents are raw text there. Choose `true`, and keep the serializer's setting the same as the parser's.
- **`internal/engine.store`** re-renders the whole document on every selector write. Adopt splicing with verification, or at least `htmlser` with `PreserveLeadingNewline`.
- **`internal/selector.Compile`** uses `ParseGroupWithPseudoElements`, so `p::before` is accepted and matches `<p>`. Servo rejects it, and PageLove presumably does too. Use the plain group parse with `Strict`.
- **`MatchAll` and `MatchFirst`** walk into `<template>` contents.
- **`selector.Path`** needs identifier escaping (see section 6).
- **XML documents** currently go through the HTML parser (see section 3). Route every `…+xml`, `application/xml` and `text/xml` document through `xmldom`.
- **`internal/microdata`** must key on local names so that it works on XML trees.
- **`internal/dom.ParseFragment`** falls back to a `body` context when it is given `nil`. That fallback turns POSTed `<tr>` rows into bare text. Make `nil` an error in write paths.

## Open questions: live harness cases to record

1. **Whether whole documents are served as stored bytes or re-serialized.**
   - Case: PUT `<!DOCTYPE html>\n<html>\n<body><div hidden itemscope data-x='a&#39;b'><br/></div></body>\n</html>\n`, then GET.
   - Also POST a row with bare `itemscope` into `tbody`, GET the document, and compare bytes.
   - This decides whether splicing (decision 6) becomes a requirement.
2. **html5ever version behaviour:** whether `<` and `>` in attribute values are escaped in a 206 fragment.
3. **Prefixed elements:** `<table><p:include …></p:include></table>` and `<p:include … />` followed by siblings. Expected: kept in place, and siblings kept.
4. **Request bodies:** a POST body with multiple top-level nodes, or only whitespace or text. What does the response echo, and what reaches the SSE `body`? Is the stored text the body verbatim or its serialization?
5. **Selector values:** does the SSE `selector` echo the request `Range` selector, or is it canonical? What exactly does a QUERY part's `Content-Range` look like (path or id-anchored; escaping)?
6. **Template contents:** do selectors match inside `<template>`?
7. **Case and namespaces:** `:empty` on whitespace-only elements; `linearGradient` and `[viewBox]` in inline SVG; the `Namespace` header together with `[p|template]`.
8. **Rejected syntax:** the status code for cascadia-only syntax (`:containsOwn`, `[a!=b]`) and for pseudo-elements. Expected 400 or 416 or 422; the docs name 422 for QUERY.
9. **Leading LF:** whether `<pre>` or `<textarea>` content that starts with an LF loses it after a write.
10. **XML head:** is an XML-family document with a leading `<?xml?>` and CDATA returned byte-identical when nothing is composed?

## Risks

- The oracle is a proxy. PageLove may run an older html5ever (which does not escape `<` and `>` in attributes) or a custom DOM; the evidence in section 2 already suggests a source-preserving one. Every compatibility claim above that rests on the oracle and not on the docs needs a live case.
- Splicing adds a correlation step and a verification parse to every write, roughly 2× parse cost per write. At pagelike's scale (one site, one writer mutex) this is negligible. The correlation algorithm is a prototype: it treats reordered text (foster-parented Liquid) as clean and relies on the verification step to catch it.
- Owning a selector engine is a maintenance cost: about 3,500 lines, somewhat less once the cascadia-only syntax is removed. It is offset by removing cascadia-only syntax and by the test corpus in the spike, which covers the feature matrix, all 421 selectors found in real use, and the canonical round trips.
- `FixAttrOrder` picks the first source order it sees when the same attribute set is written in two orders on formatting elements. That is harmless, but it is a heuristic until upstream is fixed.
