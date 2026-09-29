# Liquid templating — behavioral specification

Area: `liquid` (requirement prefix `R-LIQ`). Harness cases:
`harness/cases/liquid/*.yaml` (ids `liquid.*`, all tagged `requires: [liquid]`).
Engine choice and spike: `docs/decisions/0002-liquid.md` (osteele/liquid plus a
pagelike layer). This document is the behavioral contract that engine must
meet. Where the spike's guesses and this spec differ, this spec wins, and §24
lists each difference.

This area covers server-side Liquid rendering of elements that carry a
`*:template="text/liquid"` attribute. It covers how a template is declared,
what text is rendered, where the output goes, which variables are in scope
(bindings, `request`, locals), the value model for bound elements, tags,
expressions, every documented filter, dates, escaping, error degradation and
budgets. Bindings (`r:`, `e:`, `j:`), includes, stamps, pagination, routes and
resource creation belong to area `composing`. They appear here only where the
template sees them, and each such dependency is flagged `X-…` (§23).

---

## 0. Conventions

**Normative words.** MUST / SHOULD / MAY follow RFC 2119. "pagelike MUST" marks a
compatibility decision where the sources are silent or disagree. The decision
is always stated together with the competing claims (§24).

**Evidence levels.**
- `documented`: public docs, snapshot 2026-09-28.
- `client-source`: official client or agent-skill code.
- `demo-source`: official apps.
- `live-observed`: seen on real PageLove.
- `inferred`: reasoned from the other sources; the reason is given.

**Source keys.** Docs pages are in `research/docs/2026-09-28/md/`. I checked
every line of each individual Liquid page against the combined page
`docs.pagelove.com_all_languages_liquid.md`, and all of them are present there
verbatim. The combined page adds one thing: the `people-listing` fixture as an
untitled first section (§24 C-14).

| Key | Source |
|---|---|
| [LIQ] | docs `languages/liquid` (overview) |
| [TPL] | docs `languages/liquid/templating` (§Enabling, §Data sources, §Template scope, §Examples, §Worked example, §Limits) |
| [FLT] | docs `languages/liquid/filters` (§Filters by category, §When a filter errors) |
| [F-STR] [F-NUM] [F-ARR] [F-DATE] [F-SEC] [F-RND] [F-DATA] [F-EXP] | docs `languages/liquid/filters/{string,number,array,date,security,random,data,expressions}` |
| [FIX] | docs `languages/liquid/fixtures/people-listing` (raw HTML shows an empty `<ul>`) |
| [RB] [EB] [JB] [ME] [RC] [PR] [PAG] [XML] [INC] [STAMP] [TR] [CP] | docs `reference/composing-pages/{Resource-Binding, Expression-Binding, JavaScript-Expression-Binding, Method-Elements, Resource-Creation, Parameterized-Routes, Pagination, XML-Documents, Includes, Stamp, Transient-Elements}` and the group overview |
| [RD] [CN] | docs `reference/reading-and-writing/{Request-Document, Content-Negotiation}` |
| [QRY] | docs `reference/protocol/QUERY` §On the edge proxy |
| [AZR] | docs `reference/permissions/AuthorizationRule` §Templated values |
| [PROP] | docs `reference/modeling-data/Property` §Reading a multi-valued property |
| [SSX] | docs `languages/sessel/reference/syntax` §Context variables, §Authenticated identity in composition |
| [JSS] | docs `languages/javascript/server/javascript-in-schemas` §Resource limits |
| [BLOG] | docs `learn/build-a-blog` (index, archive, feed, "Locking the data folder") |
| [PAGR] | docs `recipes/paginating-a-list` |
| [SHOP-IDX] [SHOP-PART] [SHOP-ADM] [SHOP-DATA] | `pagelove-shop@d887054`: `site/index.html:17-32`, `site/partials.html:9-22`, `site/admin/{index,products}.html:17-22`, `site/data/products/*.html`, `site/schemas.html:85-92` |
| [POLL-IDX] [POLL-TPL] [POLL-OUT] | `pagelove-polls@c9270e5`: `site/index.html:84-97`, `site/templates/new-poll.html:1-117`, `site/polls/kfd47o4zqd.html` (a stored poll page the template generated) |
| [KAN] | `pagelove-kanban@85109ab:site/app.js:653` (template), `:131-146` (client parse, "empty when anonymous") |
| [ATS] | `pagelove-ats@8f200fc:site/admin/index.html:1398-1414` (literal `{{first_name}}` in stored records) |
| [DEMO5] | `demo-apps@c4dd883:demo-05-professional/app.js:102-104` ("Liquid composition … proved unreliable") |
| [SKILL] | `pagelove-dev@b489923:skills/pagelove-dev/SKILL.md:272-275, 319` (Aug 2026) |
| [SKILL-OLD] | `pagelove-cursor@b97c3ef:plugins/pagelove/skills/pagelove-dev/SKILL.md:101-128, 167-188, 385-431, 506-510` (Apr 2026; older server) |
| [LIVE] | `read-probes.json` (budget response headers on docs.pagelove.com) |
| [D0002] [D0003] [LO-1] | `docs/decisions/0002-liquid.md`, `docs/decisions/0003-html-and-selectors.md`, `docs/compat/live-observations.md` LO-1 |
| [RW] [PERM] [PROTO] | sibling specs `docs/spec/{reading-writing,permissions-identity,protocol}.md` |

**Terms.**
- *Host*: the element that carries the template attribute.
- *Template source*: the text Liquid renders.
- *Rendered output*: the text Liquid produces.
- *Directive attribute*: a prefixed attribute whose prefix is bound to a
  PageLove namespace or to a method-bearing schema (area `composing`).
- *Context*: the composition variable map (area `composing`).
- *Item*: the Liquid value that wraps a bound element.
- *Markup context*: an output position where element markup may appear.
- *Composition error*: the whole request fails with no partial page.
- *Filter error*: one expression fails and degrades locally.

---

## 1. Declaring a template

**R-LIQ-1 Template attribute.** An element is a template host when it has an
attribute whose local name is `template` and whose prefix is bound, by an
`xmlns:<prefix>` declaration on the element itself or on an ancestor, to
exactly `https://pagelove.org/1.0`.
- The docs use `pagelove:template` ([TPL], [EB], [JB], [QRY]) and `p:template`
  ([RB], [RD], [BLOG]), and the apps use both ([KAN] `pagelove:`; [SHOP-IDX],
  [POLL-TPL] `p:`). Any prefix works.
- Evidence: documented ([TPL] §Enabling templating; [RB] §Namespace
  declaration, where "the prefix can be any valid XML prefix"), demo-source.
- Confidence: high for `p`/`pagelove`, medium for other prefixes.
- Edge cases:
  - The declaration may sit on the host itself, as in
    `<ul xmlns:p="…" p:template="…">`; [PAGR] does this for `p:paginate`.
  - HTML attribute names are ASCII case-insensitive, so `P:Template` works.
    XML documents compare case-sensitively ([XML] §The XML dialect).

**R-LIQ-2 Engine value.** The attribute value is a MIME type naming the engine.
- `text/liquid` selects Liquid. pagelike compares the value after trimming ASCII
  whitespace, ignoring ASCII case.
- For any other value, pagelike MUST fail composition (R-LIQ-206) with Error
  kind `UnsupportedTemplateEngine`.
- Evidence: documented for `text/liquid` ([TPL] engine table, whose only row is
  Liquid). The unknown-value behavior is inferred: the plan's principle that
  unsupported features fail clearly.
- Confidence: high (`text/liquid`), low (unknown values; P-LIQ-17).

**R-LIQ-3 An unbound prefix is inert.** A `*:template` whose prefix is not bound
to `https://pagelove.org/1.0` is not a directive. That covers a missing
declaration, a typo, or another URI such as `https://pagelove.org/`.
- Nothing is rendered: `{{ … }}` stays literal and the attribute is left in the
  output exactly as written.
- Evidence: documented. [ME] §Error cases: an attribute-form dispatch with an
  unbound prefix is "left in the output exactly as written". [RB]: server-side
  processing "must also be enabled" by the 1.0 declaration. [SKILL-OLD]:506
  lists "Missing SSPI namespace" as a mistake.
- Confidence: medium.

**R-LIQ-4 Where a host may appear.** A host may be any element, including:
- `<html>` ([POLL-TPL]:2)
- `<body>` ([RB] example)
- `<section>`, `<ul>`, `<div>`, `<span>`, `<main>`
- the root element of an XML document ([BLOG] `feed.xml`)

Templates also run in documents served through parameterized routes ([PR]), in
resource-creation templates ([RC]), and in fragments brought in by includes
and stamps, when the composition walk reaches them (inferred).
- Evidence: documented, demo-source.
- Confidence: high.

**R-LIQ-5 Only the host subtree is templated.** Text that looks like Liquid but
sits outside every host is ordinary text and is served unchanged. A document
with no host is served verbatim, `{{ … }}` included.
- Evidence: documented ([TPL] §Enabling templating: the rest of the document
  "is unchanged"). Also demo-source: [ATS] stores email templates containing
  `{{first_name}}` in ordinary table cells.
- Confidence: high.

**R-LIQ-6 XML documents.** A host in an XML-family document works the same way.
Source and output use the XML dialect (R-LIQ-35), and every `xmlns:`
declaration is kept (R-LIQ-33).
- Evidence: documented ([XML] §Composition; [BLOG] §The feed).
- Confidence: high.

**R-LIQ-7 Nested hosts.** A host inside another host's subtree is rendered as
part of the outer template. The outer template source contains the inner
host's markup and its Liquid. The inner `*:template` attribute is removed from
the output and is never dispatched a second time.
- Why: this stops double rendering, and it stops template injection when data
  rendered by the outer template contains Liquid delimiters (R-LIQ-92).
- Evidence: inferred.
- Confidence: low (P-LIQ-8).

**R-LIQ-8 Liquid appears nowhere else.** Liquid is evaluated only inside hosts.
It is never evaluated in:
- AuthorizationRule fields ([AZR]: `{{ … }}` "is ordinary text", cross-area
  [PERM] R-PERM-36)
- `e:`/`j:`/`r:` attribute values
- `<p:include>` attributes
- Trigger, Processor or Schema sources
- Evidence: documented ([AZR]).
- Confidence: high.

---

## 2. Template source

**R-LIQ-10 The source is the host's raw inner markup.** The template source is
the host's content exactly as written in the source text it came from:
- the stored document bytes;
- the origin document, for included or stamped content;
- the session content, for a transient element.

It is the character range from the end of the host's start tag to the start of
its end tag. It is **not** a re-serialization of a parsed DOM. That means no
entity decoding or re-encoding, no attribute re-quoting, no foster parenting
and no parser-implied elements. Consequences:
- Liquid tags between table parts work. [POLL-TPL]:56-84 puts `{% for %}`
  between `<th>`/`<td>` cells inside `<thead>`, `<tbody>` and `<tfoot>`, and
  [POLL-OUT]:56-93 shows the cells generated in place.
- `<` and `>` inside Liquid work: `{% if pub.size > 0 %}` in [BLOG] and
  `"p.price < 10"` in [F-EXP].
- `{{ … }}` inside attribute values, `<title>`, `<script>`, `<style>`,
  `<textarea>` and HTML comments is rendered ([POLL-TPL]:6-10 renders inside
  `<title>` and `<base href>`; [SKILL-OLD]:389-399 says scripts are processed).
- Evidence: demo-source, documented.
- Confidence: medium. Contradiction C-3.
- live 2026-09-29 (adopted with LO-15, docs/compat/decisions-2026-09-29/serialization.md,
  superseding the keep-standard of composing-liquid.md): markup inside a
  Liquid string literal (the docs' `{{ "<a href='x'>" | escape }}` written
  inline) is an element in the stored document, so the output does not close
  within its run of text and the page fails with 500 "unterminated `{{`".
  pagelike does the same (`liquid.SourceFromHTML`). Case:
  `liquid.escape.escape-filter-inline-500`.
- Edge cases:
  - A host produced by composition itself has no source text, for example a
    method-element result. pagelike then uses the html5ever-compatible
    serialization of its children. That is lossy: `<` in text becomes `&lt;`,
    and Liquid then sees the entity. This is a decision.
  - Implementation: `internal/dom` spans (`Span.InnerStart…InnerEnd`) give the
    region. When the spans for the document are not clean, pagelike recovers
    the region with the tokenizer's raw token text [D0002 §Integration 1].

**R-LIQ-11 The host's start tag is not rendered.** Liquid inside the host's own
attribute values is not evaluated. `<div class="{{ c }}" p:template="text/liquid">`
keeps `class="{{ c }}"`.
- Evidence: inferred. It follows from R-LIQ-30's children-only model, which
  [D0002 §Integration 2] also adopts.
- Confidence: low (P-LIQ-7).

**R-LIQ-12 Delimiters.**
- Output: `{{ expr }}`. Tags: `{% tag … %}`. Whitespace-trimming variants:
  `{{-`, `-}}`, `{%-`, `-%}`. A `-` removes all whitespace, newlines included,
  up to the neighboring non-whitespace character.
- Delimiters are recognized anywhere in the source text.
- A `$` immediately before `{{` is ordinary text: `${{ total }}` renders `$`
  followed by the value ([EB] examples).
- To emit literal delimiters, wrap them in `{% raw %}…{% endraw %}`
  ([SKILL-OLD]:389-399).
- Evidence: documented ([TPL] `{%- for -%}` example), client-source.
- Confidence: high.

**R-LIQ-13 Entities are not decoded before Liquid sees them.**
`{{ "a &amp; b" }}` outputs the eight characters `a &amp; b`.
- Inside a `"`-quoted attribute, Liquid string literals must use `'`, as every
  docs example does. `&quot;` is not a quote to Liquid.
- Evidence: inferred from R-LIQ-10.
- Confidence: medium.
- live 2026-09-29 (adopted, supersedes the example above;
  docs/compat/decisions-2026-09-29/composing-liquid.md): inside `{{ }}` and
  `{% %}` of an HTML host, character references are decoded where the HTML
  parser decodes them (text, `title`/`textarea`, attribute values; not
  `script`/`style`, comments, raw blocks or XML) before Liquid parses them:
  `{{ "a &amp; b" | size }}` is `5`, `{% if n &gt; 0 %}` compares, and
  `title="{{ 'a&amp;b' | size }}"` gives `3`. Literal text outside Liquid
  markup is still output as written.

**R-LIQ-14 Whitespace is preserved.** Text between delimiters is copied
byte-for-byte. A tag without trim markers leaves its line's indentation and
newline in place. The worked example ([TPL]) renders
`<ul>\n    \n    <li>Anna</li>\n    \n    <li>Ben</li>\n    \n  </ul>`, and
[POLL-OUT]:6-9 and :16-22 show the blank lines left by `assign`/`if` lines.
- Evidence: documented, demo-source.
- Confidence: high.

---

## 3. Pipeline position and dispatch order

**R-LIQ-20 When templates run.** Composition, and therefore templating, runs
per request for:
- whole-document GET/HEAD;
- selector-range reads (Range);
- edge `QUERY` in `text/css-selector` mode, which evaluates the composed page
  ([QRY]; cross-area [PROTO] R-PROTO-61);
- writes whose selector is resolved against the composed view ([RW] R-RW-16);
- resource-creation `POST`, where the template document is composed with the
  POST request ([RC]).

The authoring (DAV) plane serves and queries raw stored source, with no
templating ([PROTO] dav rows).
- Evidence: documented.
- Confidence: high (reads), medium (writes).

**R-LIQ-21 Order on the host element.** Every other directive attribute on the
same element is dispatched before `*:template`, wherever it appears in the
source:
1. resource bindings (`r:`) first;
2. then expression bindings (`e:`, `j:`) in declaration order;
3. then any other method attributes;
4. `*:template` last.

If an earlier method replaces the host element (an Element return), dispatch
stops and the template never runs.
- Evidence: documented ([ME] §Attribute form: any `*:template` attribute is
  "always dispatched last"; [EB] §The three bindings compared: "Resource
  bindings resolve first").
- Confidence: high.

**R-LIQ-22 Tree-walk timing.** A host renders when the pre-order composition
walk reaches it: after its ancestors' directives, and before any directive
inside its own subtree. When Liquid evaluates, nothing inside the host has run
yet:
- bindings on descendants
- `<p:include>`, `<p:stamp>` and method elements
- transient markers

Their raw markup is part of the template source. So a descendant's binding is
not visible to the template (R-LIQ-41).
- Evidence: inferred from [ME] (subtree scoping of dispatch results) and [EB]
  §Scope.
- Confidence: low (P-LIQ-9).

**R-LIQ-23 The walk continues into the output.** After the host's children are
replaced (R-LIQ-30), composition walks the new children. Any
`<p:include>`, `<p:stamp>`, method element, `r:`/`e:`/`j:` binding,
`p:transient` or `p:paginate` in the rendered output takes effect. Nested
`*:template` attributes do not (R-LIQ-7).
- Evidence: inferred ([ME] §How the result becomes HTML: "Composition recurses
  into the spliced fragment"), documented ([PAG]: pagination "operates on the
  fully-resolved DOM" after templates).
- Confidence: medium (pagination), low (the other directives; P-LIQ-10).

**R-LIQ-24 What runs after templates.** These run on the rendered page:
- pagination ([PAG] §When to reach for it; [PAGR] see-also);
- selector extraction for Range reads and edge QUERY ([PAG] §With Range
  selectors; [QRY]);
- JSON-LD content negotiation ([CN] see-also: "server-side rendering that runs
  before negotiation");
- `xmlns:` stripping for HTML (area `composing`).
- Evidence: documented.
- Confidence: high.

**R-LIQ-25 Bindings see stored documents, not composed ones.** A resource
binding reads the *stored* source of every document, never its rendered
output. A template document that contains microdata items is therefore matched
by bindings on other pages, and its property values are raw Liquid text.
- Apps work around this. [POLL-TPL]:34 computes the itemtype in Liquid
  (`itemtype="{{ 'https://pagelove.org/Poll' }}"`), so the stored template
  carries no `Poll` item. [SKILL-OLD]:422-431 filters on `@id` instead.
- Evidence: demo-source, client-source, documented ([TR]: "Bindings query the
  canonical document").
- Confidence: medium.

**R-LIQ-26 No side effects.** A template cannot mutate documents, create
resources, perform I/O or issue HTTP requests. There are no filesystem or
document reads through `include`/`render`/`layout` (R-LIQ-77).
- Evidence: documented ([TPL] §Template scope).
- Confidence: high.

---

## 4. Output placement

**R-LIQ-30 The host stays and only its children are replaced.** The output
consists of:
- the host element itself, kept;
- its `*:template` attribute and every dispatched directive attribute
  (`r:`, `e:`, `j:`, …) removed;
- its other attributes (`id`, `class`, `hidden`, `lang`, `data-*`, …) kept
  unchanged, in source order;
- its children replaced by the rendered output, parsed (R-LIQ-31).
- Evidence:
  - documented: [TPL] §Worked example shows the output `<ul>` without the
    binding attribute and says "All SSPI namespaces and binding attributes have
    been stripped from the output". [QRY]'s composed result is
    `<main class="calc">5</main>`.
  - demo-source: [KAN]:137 reads `#whoami-server` after rendering, so `id`
    survives. [POLL-OUT]:2 has `<html lang="en" xmlns:p=…>` with no
    `p:template`.
- Confidence: high. Contradiction C-1: the docs prose says the element is
  replaced.

**R-LIQ-31 Parsing the output.** The rendered string becomes the host's new
content, parsed in the host's context with the same structure-preserving rules
as stored documents: no foster parenting and no implied elements (area `dom`,
[RW] R-RW-21).
- pagelike SHOULD splice the rendered text into the document source in place of
  the host's content region, then re-parse. That gives exactly the stored-document
  parse. Malformed output is repaired inside the host: it can never close the
  host or change its siblings.
- Evidence: inferred; [D0003] notes that Liquid output inside tables must not
  be foster-parented.
- Confidence: medium.

**R-LIQ-32 Empty output.** If the output is only whitespace, or empty, the host
keeps that whitespace, or becomes empty. For example, zero items in the worked
example give `<ul>\n    \n  </ul>`.
- Evidence: documented, weakly ([FIX] raw HTML shows exactly this shape).
- Confidence: medium.

**R-LIQ-33 `xmlns:` declarations.**
- In HTML responses, PageLove `xmlns:` declarations are stripped at the end of
  composition (area `composing`; [INC] example).
- In XML responses, all `xmlns:` declarations stay ([XML] §Differences), while
  the directive attributes are still removed.
- A resource-creation POST stores the rendered document with its `xmlns:`
  declarations still present ([POLL-OUT]:2). That is cross-area, X-3.
- Evidence: documented, demo-source.
- Confidence: high (HTML/XML), medium (creation).

**R-LIQ-34 Byte fidelity outside the host.** Composition changes only the
host's start tag (directive attributes removed, nothing else re-quoted or
reordered) and its content. Every other byte of the served document equals the
stored source, subject to the other directives ([LO-1]; [RW] R-RW-21).
- Evidence: live-observed (LO-1, for documents without composition), inferred
  for templated documents.
- Confidence: medium.

**R-LIQ-35 XML well-formedness.** In an XML-family document the output is parsed
with the XML dialect. If the result is not well-formed, composition fails
(R-LIQ-206).
- Evidence: inferred.
- Confidence: low.

---

## 5. Variables in scope

**R-LIQ-40 Sources of names.** A name resolves in this order:
1. Template-local variables: `assign`, `capture`, `for` loop variables,
   `increment`/`decrement` counters, `cycle` state, and the `forloop`
   variable.
2. Context entries visible at the host:
   - bindings (`r:`, `e:`, `j:`) declared on the host or on any ancestor
     element, nearest declaration winning;
   - values that method elements wrote to `Context` when the host is inside
     their subtree.
3. `request` (R-LIQ-60). `request` is `Context.request`, so a binding named
   `request` shadows it.
- Evidence: documented ([TPL] §Data sources; [EB] §Scope: "nearest ancestor
  wins"; [ME]: Context mutations are visible to the dispatched element and its
  descendants; [JB]: `Context` holds bindings and `request`).
- Confidence: high (locals, host/ancestor bindings, `request`), medium
  (method-written Context).

**R-LIQ-41 What a template cannot see.**
- bindings on sibling elements ([EB] §Scope: "localcount is NOT" visible to
  the sibling);
- bindings on descendants of the host (R-LIQ-22);
- variables assigned in a different host;
- anything in another document.

Such names are undefined, so they are nil (R-LIQ-203).
- Evidence: documented (siblings), inferred (the rest).
- Confidence: high (siblings), low (descendants).

**R-LIQ-42 Binding names.** A variable name is the binding attribute's local
name, the part after the prefix.
- In HTML documents the parser ASCII-lowercases attribute names, so
  `r:postsByAuthor` binds `postsbyauthor` ([D0003] §names).
- XML documents preserve case.
- Hyphenated names such as `r:my-list` are valid Liquid identifiers.
- Evidence: inferred.
- Confidence: low (P-LIQ-11).

**R-LIQ-43 There is no `Context` variable.** Unlike a `j:` binding ([JB]),
Liquid has no variable named `Context` unless a binding carries that name.
- Evidence: inferred; the docs never show `Context` in Liquid.
- Confidence: low.

**R-LIQ-44 Locals shadow bindings.** `{% assign users = users | sort: 'fullname' %}`
reads the binding and then shadows it for the rest of the render ([TPL]
example).
- Evidence: documented.
- Confidence: high.

**R-LIQ-45 Each host renders in a fresh scope.** Assignments are local to one
render. A variable assigned in one host is undefined in another host on the
same page.
- Evidence: inferred ([TPL]: template execution is "scoped to the annotated
  element").
- Confidence: medium.

---

## 6. Value model

**R-LIQ-50 Types.** A Liquid value is one of:
- nil
- boolean
- integer (signed 64-bit)
- float (IEEE double)
- string (UTF-8)
- array
- hash (string keys, insertion-ordered)
- item (a bound element, R-LIQ-52)
- range

**R-LIQ-51 A resource binding's value.** The value is an array of items, one per
matched element, in site-graph order.
- The order is defined by area `composing` (X-1). pagelike's order is: document
  path ascending (byte order), then document order.
- No match gives an empty array, never nil ([RB] §Error cases: "Empty
  collection (not an error)").
- The binding is evaluated per request and never cached ([RB] §Semantics).
- An invalid selector fails the request during composition ([RB] §Error cases,
  owned by `composing`).
- Evidence: documented.
- Confidence: high (empty, per request), low (order).

**R-LIQ-52 Reading an item's properties.** For an item `it` and a name `k`,
`it.k` and `it['k']` read the elements whose `itemprop` token list contains
`k`. The search runs over the item's descendants and does not descend into a
nested `[itemscope]`. It also follows `itemref` (WHATWG; low).
- Each element contributes its microdata value from the [RW] R-RW-50 table:
  - `meta` → `content`
  - media elements → `src`
  - `a`/`area`/`link` → `href`, the raw attribute
  - `object` → `data`
  - `data`/`meter` → `value`
  - `time` → `datetime`, or else its text
  - an element with `itemscope` → a nested item
  - anything else → its descendant text content, untrimmed
- How many values there are decides the shape:
  - zero → nil
  - exactly one → that value, a string or an item
  - several → an array in document order

  This holds even when a Schema declares `0..n`: [PROP] says a property read
  "off an element returned by a selector query still gives you a plain value
  when there is exactly one". Values on this route are always strings; they
  are never typed as numbers or booleans.
- Evidence:
  - documented: [PROP]; [TPL] `user.fullname`, `user.email`; [BLOG] reads
    `meta`, `time` and `h1` properties.
  - demo-source: the shop reads `p.variant`, which is single for the cap
    ("One size") and repeated five times for the tee, then applies `join`
    ([SHOP-DATA], [SHOP-ADM]:20).
- Confidence: high (single vs several, meta/text), medium (other element
  kinds), low (`itemref`).
- Edge cases:
  - `{{ cap.variant.size }}` is `8`, the string length, while the tee's is `5`.
    That is the documented pitfall. The `join`/`split` idiom in [SHOP-IDX]:24
    coerces either shape.
  - A missing property is nil, so `default` applies ([F-STR] `default` example).

**R-LIQ-53 Reserved `@` keys on items.**
- `it['@id']`:
  - `<document path>#<element id>` when the element has an `id`;
  - otherwise just `<document path>`.

  The path is the stored document path: absolute, percent-decoded, with no
  scheme or host.
- `it['@type']`: the `itemtype` attribute verbatim, or nil when absent.
- pagelike MAY add more `@` conveniences (`@text`, `@html`, `@tag`,
  `@attributes`). These are pagelike-only and make no compatibility claim
  ([D0002]).
- Evidence:
  - documented: [TPL] uses `user['@id']` as an `href` and selects `[id]`.
  - demo-source: [POLL-IDX]:90 uses `poll['@id'] | split: "#" | first` as the
    poll's URL.
  - client-source: [SKILL-OLD]:424-431 filters with
    `item['@id'] contains 'request.body'`.
- Confidence: medium (`path#id`), low (absolute path versus full URL, the
  no-id form, `@type`; P-LIQ-12).

**R-LIQ-54 Elements that are not items.** A bound element without `itemscope`
(for example `r:prices="[itemprop=price]"`) is still an item value. It has no
properties, so every `k` lookup is nil. It keeps the reserved keys and its
rendering (R-LIQ-56).
- Evidence: inferred.
- Confidence: low (P-LIQ-12).

**R-LIQ-55 Special members.**
- `.size`, `.first`, `.last` work on arrays, and `.size` on strings (standard
  Liquid). Examples: [BLOG] `pub.first.publishedAt` and `all.size`;
  [POLL-IDX] `listed.size`; [POLL-TPL] `opts.size`.
- On a hash, `.size` is the value of a `size` key when one exists, otherwise
  the key count.
- On an item, `size` is the `size` itemprop when there is one, otherwise the
  number of distinct property names.
- `first` and `last` on an item are ordinary property lookups.
- Evidence: documented/demo-source (arrays), inferred (items).
- Confidence: high (arrays), low (items).

**R-LIQ-56 How `{{ v }}` renders.**

| Value | Output |
|---|---|
| nil | the empty string |
| boolean | `true` or `false` |
| integer | decimal |
| float | Ruby-style. An integral value within ±1e16 gets one decimal (`100.0`, `15.0`). Anything else is the shortest round-trip decimal without an exponent (`3.5`, `3.14`). NaN and infinities render `NaN`, `Infinity`, `-Infinity`. |
| array | the rendered elements concatenated with no separator |
| hash | compact JSON (R-LIQ-190) |
| item | the element's text content, trimmed |
| range | `a..b` |

- Evidence: documented for integral floats ([EB] example `Sum: 100.0`) and
  integers ([JB] `Total: 60`), inferred for the rest.
- Confidence: medium (numbers), low (array, hash, item; P-LIQ-13).

**R-LIQ-57 Truthiness.** Only nil and `false` are falsy. `""`, `0`, `[]` and
`{}` are truthy.
- Evidence: demo-source. [SHOP-IDX]:17-19 says an empty string "is TRUTHY in
  Liquid", which is why the shop tests `!= blank`. This is also standard
  Liquid.
- Confidence: high.

**R-LIQ-58 The `blank` and `empty` literals.**
- `x == empty` is true for `""`, `[]` and `{}`.
- `x == blank` is true for nil, `false`, `""`, whitespace-only strings, `[]`
  and `{}`.
- `!=` negates. The literals may appear on either side.
- Evidence: demo-source (`shot != blank`, [SHOP-IDX]:25).
- Confidence: medium (empty string, nil), low (whitespace-only).

**R-LIQ-59 Converting binding values.**
- Sessel (`e:`):
  - integer → integer; float → float
  - string, boolean
  - `null` → nil
  - list → array; dictionary → hash
  - element → item
  - temporal → ISO-8601 string (low)
- JavaScript (`j:`):
  - A number that is integral and within ±2^53 becomes an integer. The docs
    render `Total: 60` and `Doubled: 120`, not `60.0`. Any other number
    becomes a float.
  - string, boolean
  - `null`/`undefined` → nil
  - array → array; plain object → hash
  - DOM element → item
- Evidence: documented ([JB] §In action; [EB] §Count and sum).
- Confidence: medium.

---

## 7. The `request` object

**R-LIQ-60 Members.** `request` is defined in every template:

| Member | Value | Evidence / confidence |
|---|---|---|
| `method` | The upper-case method of the current request: `GET`, or `POST` while a resource-creation template renders. | documented [RD], [SSX]; high |
| `path` | The request path, percent-decoded, without the query. On a parameterized route this is the concrete requested path, not the template's path. | documented [RD]; high |
| `query` | A hash of parsed query parameters: names and values URL-decoded, `+` as space. A repeated name becomes an array of strings. `{}` when there is no query. | documented [RD] (`request.query.*`); high / low (repeats) |
| `headers` | A hash of request headers with lower-case names, including `authorization` and `cookie`. Repeated headers are joined with `, `. Hyphenated names need bracket access: `request.headers['accept-language']`. | documented [SSX] (`request.headers.*`), demo [SHOP-PART]:20; high / low (repeats) |
| `params` | A hash of parameterized-route captures, percent-decoded. `{}` on non-route pages. | documented [PR] §Reading captured parameters (the Liquid example was lost in the scrape, C-15); high |
| `body` | For an `application/x-www-form-urlencoded` body, a hash of decoded fields, with repeats as arrays. Otherwise the raw body as a string, `""` for requests without a body. | demo [POLL-TPL]:7-21, documented [RC] steps 1 and 3, [RD] §Fields (raw `body`), client [SKILL-OLD]:408-420; high (form) / low (other types) |
| `auth` | See R-LIQ-61. | |

- Evidence: as listed.
- Confidence: as listed.

**R-LIQ-61 `request.auth`.**
- For an authenticated request:
  - `username` is the OIDC `sub`;
  - `claims` is a hash of every claim (`email`, `name`, `picture`, `sub`, …);
  - `roles` and `role` are the same list, cross-area [PERM] R-PERM-74: the
    verified email, group names, OIDC roles and `users`.
- For an anonymous request:
  - `username` is nil;
  - `claims` is `{}`, so every claim is nil;
  - `roles` and `role` are `[]`.
  - `{% if request.auth.username %}` is therefore false, and the kanban
    whoami template renders `||||`.
- Nothing here ever raises an error.
- Evidence:
  - documented: [SSX] §Authenticated identity: "empty (falsy) — there is no
    error".
  - demo-source: [KAN]:131-146 "(empty when anonymous)", and [KAN]:653 uses
    `request.auth.role`.
  - client-source: [SKILL-OLD]:172-188 uses `request.auth.roles`.
- Confidence: high (`username`, `claims`), medium (the two list spellings).

**R-LIQ-62 Names that exist only in rules.** `auth`, `method`, `path` and
`query` are authorization-rule names ([SSX]). They are not bound in
templates, so under R-LIQ-203 they are nil.
- Evidence: documented that they are unbound; the outcome follows the pagelike
  decision.
- Confidence: medium (unbound), low (nil rather than an error; C-2,
  P-LIQ-3).

**R-LIQ-63 Serializing the whole object.** `{{ request | json: 2 }}` serializes
the object with the members above. JSON key order carries no meaning.
- Evidence: documented ([TPL] §Request object; [F-DATA] `json`).
- Confidence: medium.

**R-LIQ-64 Caching.** A composed response whose template read any member of
`request.auth` or `request.headers` is served `Cache-Control: private` ([RW]
R-RW-103). Serializing or iterating the whole `request` counts as reading them.
Reading only `method`, `path`, `query` or `params` keeps the page shareable.
- Evidence: documented ([SSX]: any `request.auth.*` makes the page private;
  [JB] §Caching: `request.auth`, `request.headers`; [PR]: `request.params` is
  shared).
- Confidence: high (auth), medium (headers, whole object).

**R-LIQ-65 `request` is read-only.** `{% assign request = … %}` only shadows the
name locally.
- Evidence: inferred.
- Confidence: medium.

---

## 8. Syntax: tags and expressions

**R-LIQ-70 Supported tags.** Standard Shopify Liquid semantics apply unless a
row says otherwise:

| Tag | Notes |
|---|---|
| `if` / `elsif` / `else` / `endif` | Conditions may carry a filter chain (R-LIQ-74). |
| `unless` / `else` / `endunless` | |
| `case` / `when` / `else` / `endcase` | `when a, b` and `when a or b`. |
| `for` / `else` / `endfor` | `limit:` and `offset:` (with or without a space after the colon, e.g. `limit: 8` in [POLL-IDX]), `reversed`, ranges `(a..b)` with variable bounds, `break`, `continue`. `forloop.{index, index0, rindex, rindex0, first, last, length, parentloop}`; [POLL-TPL] uses `forloop.index`. |
| `cycle` | Optional group name. |
| `tablerow` / `endtablerow` | `cols:`, `limit:`, `offset:`, the `tablerowloop` variable, Shopify markup. |
| `assign`, `capture` / `endcapture` | [TPL] names both. |
| `increment`, `decrement` | |
| `raw` / `endraw` | [SKILL-OLD]:389-399. |
| `comment` / `endcomment` | |
| `echo` | An output expression inside a tag. |
| `liquid`, `{% # … %}` | SHOULD be supported. No PageLove doc or app uses them. |

- Evidence: documented for `assign`, `capture`, `for`, `if`, whitespace control
  and `raw`; demo-source for `limit:`, `forloop`, `unless`; the rest is
  inferred from "standard Shopify Liquid" ([FLT]).
- Confidence: high for the documented and demo tags, medium for the others.

**R-LIQ-71 Looping over non-arrays.**
- `for` over nil runs zero iterations, and the `else` branch runs.
- Over a string, it runs one iteration with that string.
- Over a hash, it iterates `[key, value]` pairs.
- Over an item, it runs one iteration with the item.
- Evidence: inferred (Shopify).
- Confidence: low.

**R-LIQ-72 Order of loop modifiers.** `offset` and `limit` are applied first,
then `reversed`, following Shopify: `(1..5) reversed limit:2` yields `2` then
`1`.
- Evidence: inferred; [D0002] gap G6.
- Confidence: low (P-LIQ-14).

**R-LIQ-73 Filters in conditions.** The condition of `if`, `elsif`, `unless` and
`case` has the form `expression ( "|" filter[: args] )*`. The filters apply to
the value of the whole expression: `{% if products | has: "onsale", true %}`
([F-ARR] `has`) and `{% if events | has_exp: "e", "e.starts > now" %}` ([F-EXP]).
To compare a filtered value, assign it first.
- Evidence: documented.
- Confidence: medium (grammar details).

**R-LIQ-74 Expressions.**
- String literals:
  - `'…'`: the text is literal.
  - `"…"`: the escapes `\n`, `\t`, `\"` and `\\` are processed. The docs'
    `{{ "a\nb\nc" | strip_newlines }}` → `abc` depends on this (C-11).
- Other literals: integers, floats, `true`, `false`, `nil`/`null`, `empty`,
  `blank`, and ranges `(a..b)`. live 2026-09-29 (kept, keep-standard;
  docs/compat/decisions-2026-09-29/composing-liquid.md): live PageLove's parser
  rejects every range literal (`(1..3)`, `(1 .. 3)`, `(a..b)`, in `for` and in
  output) and fails the page with 500; pagelike keeps standard ranges.
  Measured by `liquid.compose.range-over-rendered-output.live`.
- Paths: `a.b`, `a['@id']`, `a[0]`, `a[-1]`, `a[var]`, and `a.size` /
  `a.first` / `a.last` (R-LIQ-55).
- Operators: `==`, `!=`, `<>`, `<`, `>`, `<=`, `>=`, `contains`, `and`, `or`.
  There are no parentheses, and `and`/`or` evaluate right to left (Shopify).
- `contains` tests a substring when the left side is a string, and membership
  when it is an array. It is false for other types.
- Evidence: documented/demo-source (paths, operators; [SKILL-OLD]:428
  `contains`), inferred (escapes).
- Confidence: high (operators, paths), low (escapes).

**R-LIQ-75 Keyword arguments.** A filter accepts `name: value` keyword
arguments, both after positional ones and with no positional argument at all:
`{{ 24 | random: upper: 3, lower: 3, digits: 2 }}` ([F-RND]) and
`argon2: memory: 65536, time: 3` ([F-SEC]). [POLL-TPL]:6 mints poll ids with
`10 | random: lower: true, digits: true`.
- Evidence: documented, demo-source.
- Confidence: high.

**R-LIQ-76 Comparing numeric strings.** Microdata values are strings (R-LIQ-52).
When one operand of `==`, `!=`, `<`, `>`, `<=` or `>=` is a number and the
other is a string whose whole text is a decimal number (optional sign, digits,
optional fraction), the two compare numerically. When one operand is a boolean
and the other is `"true"` or `"false"`, they compare as booleans.
- Otherwise Shopify rules apply: values of different types are unequal, and an
  ordering comparison between incomparable types is false.
- The same coercion applies to the field-equality filters (R-LIQ-146).
- Evidence: documented examples imply it. [F-EXP]'s `"u.age >= 18"` and
  `"p.price < 10"` are shown over bound items, whose values are strings, and
  [F-ARR] says to use `sort` "when you want numeric order". [D0002] gap G1.
- Confidence: low (P-LIQ-5).

**R-LIQ-77 No `include`, `render`, `layout` or `section`.** These tags are not
supported, because templates perform no I/O. Using one is a template syntax
error (R-LIQ-205). The tags MUST NOT read any file, document or network
resource.
- Evidence: documented ([TPL] §Template scope) for "no I/O". The error status
  is a decision. [D0002] found that the stock engine reads the server's
  working directory.
- Confidence: high (no reading), low (status).

**R-LIQ-78 Unknown tags.** An unknown tag, an unmatched `end…` tag, or an
unterminated `{{`/`{%` is a template syntax error (R-LIQ-205).
- Evidence: inferred.
- Confidence: low.

---

## 9. Escaping and safety

**R-LIQ-90 No automatic escaping.** *Superseded live 2026-09-29 (LO-15,
docs/compat/decisions-2026-09-29/serialization.md): PageLove HTML-escapes every
`{{ }}` output whose value is not marked safe (a query value `<b>x</b>` renders
as text; `newline_to_br`'s `<br />` is escaped). pagelike renders compositions
with `liquid.Options.AutoEscape`; outputs in a raw-text host are not escaped.
Case: `liquid.escape.autoescape`; the text below is the losing claim
(`liquid.escape.no-autoescape`, disputed).* `{{ … }}` inserts a string value verbatim, in
HTML and XML documents and in every context. Authors escape with the `escape`
filter.
- Evidence: demo-source and documented behavior of PageLove's own authors, who
  escape every untrusted value explicitly:
  - [POLL-TPL] escapes every `request.body` value;
  - [POLL-IDX]:92-94 escapes the titles;
  - [BLOG] escapes only in `feed.xml`, where a raw `&` would break the XML;
  - [D0002] assumes no autoescape.

  Against this, [F-STR] says `escape`'s result is "marked safe, so it is not
  escaped again" (R-LIQ-91).
- Confidence: low. Contradiction C-4, P-LIQ-1. The autoescape reading is kept
  as a `status: disputed` case.

**R-LIQ-91 Safe marking.** *Live 2026-09-29: the mark only exempts a value from
autoescaping; `escape` is not idempotent (`"<" | escape | escape` renders
`&amp;lt;`, `liquid.escape.escape-idempotent`).* `escape`, `escape_once` and `xml_escape` return a
string marked safe. `escape` and `xml_escape` applied to a safe string return
it unchanged, so `x | escape | escape` escapes only once. Any other filter that
builds a new string drops the mark. Output never re-escapes, whether marked or
not (R-LIQ-90).
- Evidence: documented ([F-STR] `escape`).
- Confidence: medium. The idempotent reading is the one consistent with
  R-LIQ-90 (P-LIQ-1).

**R-LIQ-92 Data is never re-evaluated.** Liquid delimiters that appear inside a
value are output as text and never evaluated. This covers binding data,
request fields and filter results. A Person named `{{ 6 | times: 7 }}` renders
exactly that, never `42`. A query parameter `q={{request.headers.cookie}}`
echoed with `{{ request.query.q }}` prints those characters.
- Evidence: inferred from Liquid semantics. [AZR] removed Liquid from rules for
  precisely this injection risk: a header value "could put `{{ … }}` in a
  header and cause a template to run".
- Confidence: high.

**R-LIQ-93 Markup injection (informative).** Output is not escaped (R-LIQ-90),
and composition continues into rendered output (R-LIQ-23). An untrusted value
emitted without `escape` can therefore inject elements, including PageLove
directive elements such as `<p:include selector=…>`. Those are then resolved
with the page's authority.
- Authors MUST `escape` untrusted values, as PageLove's own apps do.
- pagelike MAY offer a per-site, opt-in autoescape mode as a pagelike-only
  extension. It is off by default and not a compatibility behavior.
- Evidence: inferred.
- Confidence: medium.

**R-LIQ-94 Trust model.** A resource binding reads the whole site regardless of
the requester's permissions. A template exposes whatever it renders to anyone
allowed to GET the page. For example, a page anonymous users may read can list
data from `/data/*` that anonymous users are denied.
- Evidence: documented ([RB] §Security; [BLOG] §Locking the data folder).
- Confidence: high.

---

## 10. Filters: general rules

**R-LIQ-100 The filter set.** Every filter in §11–§18 MUST exist with the
semantics stated. Standard Shopify filters that PageLove's pages do not list
SHOULD exist with Shopify semantics: `base64_encode`, `base64_decode`,
`base64_url_safe_encode`, `base64_url_safe_decode`.
- Evidence: documented for the listed set ([FLT] §Filters by category). The
  base64 filters are inferred from "provides the standard Shopify Liquid
  filters".
- Confidence: high (listed), low (base64).

**R-LIQ-101 Coercing input.**
- String filters coerce their input to a string first ([F-STR] intro):
  - nil → `""`
  - numbers → as R-LIQ-56
  - arrays → concatenated
  - items → their text
- Number filters coerce their input and numeric arguments:
  - a numeric string parses: integer syntax → integer, otherwise float
  - nil, a non-numeric string, a boolean, an array or a hash → `0`
  - `to_integer` has its own rules (R-LIQ-137)
- Array filters coerce their input:
  - nil → `[]`
  - an array → itself
  - a range → its elements
  - anything else → `[value]`

  The exceptions are `first`, `last`, `size` and `slice`, which act on the
  characters of a string.
- Evidence: documented (string coercion; `push`'s coercion, [F-ARR]),
  demo-source (the shop applies `join` to a scalar and to nil).
- Confidence: medium.

**R-LIQ-102 Filter errors.** These are filter errors, handled by R-LIQ-200:
- an input or argument outside a filter's domain, as each filter defines it;
- a missing required argument;
- an unknown filter name;
- an expression-filter predicate that does not parse;
- expression nesting beyond 32 levels.
- Evidence: documented for the category ([FLT]); inferred for the individual
  triggers.
- Confidence: medium.

**R-LIQ-103 Characters.** Lengths, indices and slicing count Unicode code
points. Case mapping is Unicode simple case mapping.
- Evidence: inferred.
- Confidence: low.

---

## 11. String filters (R-LIQ-110 … R-LIQ-125)

All string filters coerce input to a string (R-LIQ-101). Results carry no safe
mark unless the filter says otherwise.
- Evidence: documented ([F-STR]) for the name, the summary semantics and each
  "Docs example" cell, which is the page's example with its printed result.
  Rows marked *inferred* fill in edge semantics.
- Confidence: high for the docs examples, medium for the other semantics
  unless a row says otherwise.

| ID | Filter | Semantics | Docs example → result | Edge / notes |
|---|---|---|---|---|
| R-LIQ-110 | `downcase`, `upcase` | Unicode lower-/upper-case | `"Hello, World"` → `hello, world` / `HELLO, WORLD` | |
| R-LIQ-110 | `capitalize` | First character upper-case, the rest lower-case | `"hello WORLD"` → `Hello world` | |
| R-LIQ-111 | `strip`, `lstrip`, `rstrip` | Trim Unicode whitespace at both ends / the start / the end | `"  hi  "` → `hi` / `hi  ` / `  hi` | |
| R-LIQ-111 | `strip_newlines` | Remove every `\r` and `\n` | `"a\nb\nc"` → `abc` | Needs escape processing in `"…"` (R-LIQ-74) |
| R-LIQ-111 | `newline_to_br` | Replace each `\r\n` or `\n` with `<br />\n` (the newline is kept) | `"a\nb"` → `a<br />\nb` | Not marked safe |
| R-LIQ-111 | `normalize_whitespace` (ext) | Collapse each whitespace run to one space, then trim | `"a   b\n c"` → `a b c` | |
| R-LIQ-112 | `escape` | `&`→`&amp;`, `<`→`&lt;`, `>`→`&gt;`, `"`→`&quot;`, `'`→`&#39;`; result marked safe; unchanged when the input is already safe (R-LIQ-91) | `"<a href='x'>"` → `&lt;a href=&#39;x&#39;&gt;` | `&quot;` rather than `&#34;` is inferred (low) |
| R-LIQ-112 | `escape_once` | As `escape`, but an `&` that starts a character reference (`&name;`, `&#123;`, `&#x1f;`) is left alone; marked safe | `"1 &lt; 2 &amp; 3"` → `1 &lt; 2 &amp; 3` | `&copy; &` → `&copy; &amp;` |
| R-LIQ-112 | `xml_escape` (ext) | As `escape` | `"<a>'x'</a>"` → `&lt;a&gt;&#39;x&#39;&lt;/a&gt;` | |
| R-LIQ-113 | `url_encode` | UTF-8 percent-encoding for forms: `A–Z a–z 0–9 - _ . ~` and `*` unchanged, space → `+`, everything else `%XX` in upper-case hex | `"a b&c"` → `a+b%26c` | |
| R-LIQ-113 | `cgi_escape` (ext) | Same as `url_encode` | `"a b & c"` → `a+b+%26+c` | |
| R-LIQ-113 | `uri_escape` (ext) | Like JavaScript `encodeURI`: keeps `A–Z a–z 0–9 ; , / ? : @ & = + $ - _ . ! ~ * ' ( ) #` and also `[` `]`; space → `%20`; `%` → `%25` | `"http://x/a b?q=1&r=2"` → `http://x/a%20b?q=1&r=2` | Not idempotent (low) |
| R-LIQ-113 | `url_decode` | `+` → space; `%XX` decoded as UTF-8; a malformed `%` sequence is left as is | `"a+b%26c"` → `a b&c` | |
| R-LIQ-114 | `strip_html` | Remove `<script>…</script>`, `<style>…</style>` and `<!--…-->` together with their contents, then every remaining `<…>` tag; entities left as they are | `"<b>hi</b><script>x()</script>"` → `hi` | |
| R-LIQ-115 | `replace: s, r` | Replace every literal occurrence of `s` with `r` (`r` defaults to `""`) | `"a-b-c" \| replace: "-", "+"` → `a+b+c` | An empty `s` returns the input unchanged (decision, low) |
| R-LIQ-115 | `replace_first`, `replace_last` (ext) | Replace the first / last occurrence | → `a+b-c` / `a-b+c` | |
| R-LIQ-115 | `remove: s`, `remove_first`, `remove_last` (ext) | Delete every / the first / the last occurrence | → `abc` / `ab-c` / `a-bc` | |
| R-LIQ-116 | `append: s`, `prepend: s` | Concatenate after / before | `"/page" \| append: ".html"` → `/page.html`; `"world" \| prepend: "hello "` → `hello world` | Arguments are coerced to strings |
| R-LIQ-117 | `array_to_sentence_string[: conn = "and"]` (ext) | Array input: `[]` → `""`, `[a]` → `a`, `[a,b]` → `a <conn> b`, three or more → `a, b, <conn> c` (Oxford comma) | `tags` = [a,b,c] → `a, b, and c`; `: "or"` → `a, b, or c` | Output for 1 and 2 items is inferred (Jekyll) |
| R-LIQ-118 | `slice: offset[, length = 1]` | Substring by code point, or sub-array. A negative offset counts from the end; out of range → `""` or `[]`; the length is clamped | `"hello" \| slice: 1, 3` → `ell`; `slice: -1` → `o` | |
| R-LIQ-119 | `split: sep` | Split on the literal `sep`. An empty `sep` splits into code points. Empty fields are kept, trailing ones included: `""` → `[""]`, `"a,,b,"` → `["a","","b",""]` | `"a,b,c" \| split: "," \| join: " · "` → `a · b · c` | `"" \| split: ","` gives `[""]`, not `[]`; the docs call this a footgun ([F-ARR] §push). Trailing empties are low (P-LIQ-15) |
| R-LIQ-120 | `truncate[: n = 50[, suffix = "..."]]` | When the code-point length is ≤ `n`, return the input. Otherwise the first `max(n − len(suffix), 0)` code points plus `suffix` | `"The quick brown fox" \| truncate: 9` → `The qu...` | `n` < 0 → filter error |
| R-LIQ-120 | `truncatewords[: n = 15[, suffix = "..."]]` | Words are maximal runs of non-whitespace. When there are ≤ `n` words, return the input unchanged. Otherwise the first `n` words joined with one space, plus `suffix` (`n` < 1 is treated as 1) | `truncatewords: 2` → `The quick...` | |
| R-LIQ-121 | `size` | String → code points; array → length; hash or item → R-LIQ-55; anything else → `0` | `"hello"` → `5` | A number gives `0` (Shopify gives 8) |
| R-LIQ-121 | `number_of_words` (ext) | Count of runs of non-whitespace, as an integer | `"the quick brown fox"` → `4` | |
| R-LIQ-122 | `default: fallback[, allow_false: false]` | Return `fallback` when the input is nil, `false`, `""`, `[]` or `{}`; with `allow_false: true`, `false` is kept | `user.nickname \| default: "Anonymous"` (nickname absent → `Anonymous`) | A whitespace-only string is **not** replaced (low) |
| R-LIQ-123 | `slugify` (ext) | Lower-case; each run of characters that are not Unicode letters or digits becomes one `-`; leading and trailing `-` trimmed | `"Hello, World!"` → `hello-world` | Treating non-ASCII letters as alphanumeric is low |

**R-LIQ-124 Escape sequences in the docs examples.** Two docs examples,
`strip_newlines` and `newline_to_br`, write `\n` inside a double-quoted
literal. pagelike processes those escapes (R-LIQ-74). Harness cases also test
the same filters on captured real newlines, so the filter semantics are checked
independently of literal-escape processing.
- Evidence: documented.
- Confidence: medium.

**R-LIQ-125 Errors.** The string filters never error on their input, because
coercion always succeeds. Wrong argument types (such as `truncate: "x"`) and
missing required arguments (`replace` without `s`) are filter errors.
- Evidence: inferred.
- Confidence: low.

---

## 12. Number filters (R-LIQ-130 … R-LIQ-137)

Inputs and arguments are coerced as in R-LIQ-101. "Integer when both are
integers" means both operands are integers after coercion, so `"41" | plus: 1`
is `42` ([D0002]).
- Evidence: documented ([F-NUM]), with each docs example quoted.
- Confidence: high for the examples, medium for the rest unless a row says
  otherwise.

| ID | Filter | Semantics | Docs example → result |
|---|---|---|---|
| R-LIQ-130 | `plus`, `minus`, `times` | Integer when both operands are integers, otherwise float. Integer overflow is a filter error (decision, low). | `10 \| plus: 5` → `15`; `10 \| minus: 3` → `7`; `6 \| times: 7` → `42`; `10 \| plus: 5.0` → `15.0` (inferred) |
| R-LIQ-131 | `divided_by` | Integer ÷ integer is **floor** division, so `-7 \| divided_by: 2` → `-4` (medium); a float operand gives a float; a zero divisor, `0` or `0.0`, returns the input unchanged | `7 \| divided_by: 2` → `3`; `7 \| divided_by: 2.0` → `3.5`; `7 \| divided_by: 0` → `7` |
| R-LIQ-132 | `modulo` | Floor modulo, whose result takes the divisor's sign: `-7 \| modulo: 3` → `2` (inferred, low); floats allowed; a zero divisor returns the input unchanged | `13 \| modulo: 5` → `3`; `13 \| modulo: 0` → `13` |
| R-LIQ-133 | `abs` | Absolute value, keeping the type | `-8 \| abs` → `8` |
| R-LIQ-134 | `ceil`, `floor` | Round up / down to an **integer** | `3.2 \| ceil` → `4`; `3.8 \| floor` → `3` |
| R-LIQ-135 | `round[: places = 0]` | 0 places → an integer, rounding half away from zero (`2.5` → `3`, inferred); more than 0 → a float with that many decimals; a negative number of places rounds to tens, hundreds, … as an integer (low) | `3.14159 \| round` → `3`; `round: 2` → `3.14` |
| R-LIQ-136 | `at_least: n`, `at_most: n` | `max(input, n)` / `min(input, n)` | `3 \| at_least: 5` → `5`; `8 \| at_least: 5` → `8`; `8 \| at_most: 5` → `5`; `3 \| at_most: 5` → `3` |
| R-LIQ-137 | `to_integer` (ext) | A float truncates toward zero; a numeric string parses (`"3.9"` → `3`, `"-3.9"` → `-3`, `"1e30"` → saturates); `true` → `1`, `false` → `0`; nil and non-numeric → `0`; beyond the int64 range, saturate to ±9223372036854775807 (minimum −9223372036854775808); NaN → `0`; an integer is returned unchanged | `"3.9" \| to_integer` → `3`; `"42" \| to_integer \| plus: 8` → `50` |

---

## 13. Array filters (R-LIQ-140 … R-LIQ-155)

Inputs are coerced to arrays as in R-LIQ-101. Every filter returns a **new**
array and never changes its input ([F-ARR] §Mutation). Filters that walk a
list are metered while they run (R-LIQ-211).
- Evidence: documented ([F-ARR]), with each docs example quoted.
- Confidence: high for the examples, medium for the rest unless a row says
  otherwise.

| ID | Filter | Semantics | Docs example → result |
|---|---|---|---|
| R-LIQ-140 | `first`, `last` | The first / last element of an array, or the first / last character of a string. nil when empty. | `items \| first` |
| R-LIQ-141 | `reverse` | The array reversed | `"a,b,c" \| split: "," \| reverse \| join: ","` → `c,b,a` |
| R-LIQ-142 | `sort[: field]` | See R-LIQ-156 | `"banana,apple,cherry" \| split: "," \| sort \| join: ", "` → `apple, banana, cherry` |
| R-LIQ-143 | `sort_natural[: field]` | Case-insensitive comparison of each value's string form. Numbers compare as text, so `"10"` comes before `"9"`. Missing values go last; the sort is stable. | `"b,A,c" \| split: "," \| sort_natural \| join: ""` → `Abc` |
| R-LIQ-144 | `uniq[: field]` | Remove duplicates, keeping the first occurrence. Equality is type-strict, so `1` and `"1"` are distinct. With a field, the property value decides. | `"a,b,a,c" \| split: "," \| uniq \| join: ","` → `a,b,c` |
| R-LIQ-144 | `compact[: field]` | Remove nil elements, or elements whose field is nil | `list \| compact \| join: ", "` |
| R-LIQ-145 | `map: field` | Replace each element with its `field` value, per R-LIQ-52 for items. A non-object element gives nil. Live 2026-09-29 (adopted): an input that is not an array or range (a single item or hash such as `find`'s result, a scalar, nil) gives `[]`, not R-LIQ-101's `[value]`, so `find: … \| map: "title"` renders nothing. | `people \| map: "name" \| join: ", "` |
| R-LIQ-145 | `join[: sep = " "]` | Join the string forms of the elements; nil → `""` | `tags \| join: ", "` |
| R-LIQ-145 | `concat: array` | Input followed by the argument. A non-array argument is coerced (R-LIQ-101). | `drafts \| concat: published` |
| R-LIQ-146 | `where: field[, value]` | Keep elements whose `field` equals `value`, compared with R-LIQ-76 coercion (so `"true"` matches `true` and `"10"` matches `10`). With no `value`, keep elements whose field is truthy (R-LIQ-57; an empty-string property is truthy). A non-object element never matches. A multi-valued property (an array) never equals a scalar. | `people \| where: "role", "admin" \| map: "name" \| join: ", "` |
| R-LIQ-147 | `reject` (ext) | The complement of `where`, with the same arguments | `users \| reject: "suspended", true` |
| R-LIQ-147 | `find` (ext) | The first element that `where` would keep, or nil | `products \| find: "sku", "A-1" \| map: "title"` |
| R-LIQ-147 | `find_index` (ext) | Its 0-based index, or nil | `products \| find_index: "sku", "A-1"` → `0` |
| R-LIQ-147 | `has` (ext) | `true` when some element matches, otherwise `false` | `{% if products \| has: "onsale", true %}` |
| R-LIQ-148 | `group_by: field` (ext) | An array of hashes `{"name": key, "items": [...]}` in the order keys are first seen. Elements with a missing field share the key nil. | `posts \| group_by: "year"`, then `group.name` / `group.items` |
| R-LIQ-149 | `sum[: field]` (ext) | Sum the numbers, or the `field` values. Numeric strings count as their value; anything non-numeric counts as `0`. The result is an integer when it is whole (`1.5 + 1.5` → `3`), otherwise a float. `[]` → `0`. | `prices \| sum`; `line_items \| sum: "price"` |
| R-LIQ-150 | `push: v` (ext) | Coerce the input (nil → `[]`, a scalar → `[scalar]`), then append `v` | the loop idiom `{% assign wanted = wanted \| push: term %}`, where `wanted` starts unassigned |
| R-LIQ-151 | `unshift: v` (ext) | The same coercion, then prepend `v` | `crumbs \| unshift: "Home"` |
| R-LIQ-152 | `pop`, `shift` (ext) | Without the last / first element; `[]` stays `[]` | `items \| pop`, `items \| shift` |
| R-LIQ-153 | `slice` | See R-LIQ-118; it also works on arrays | |
| R-LIQ-154 | `size`, `first`, `last` as properties | R-LIQ-55 | |
| R-LIQ-155 | Field lookups | For `field` arguments, an item uses R-LIQ-52; when the property is multi-valued, `sort`, `sort_natural`, `group_by` and `sum` use its **first** value (decision, low). A hash uses its key; anything else gives nil. | |

**R-LIQ-156 How `sort` orders values.** `sort` is a stable sort by the value, or
by `field`, using this total order:
1. **Missing values last.** An element whose value is nil, or whose field is
   absent, goes after everything else and keeps its relative order. An empty
   string `""` is a value, not a missing one, so it sorts with the strings.
2. **Kinds are grouped, never interleaved.** First numbers, then numeric
   strings, compared numerically with the numbers. Then the other strings, by
   Unicode code point, case-sensitive (`"B"` before `"a"`). Then booleans
   (`false` before `true`). Then everything else (items, hashes, arrays) in
   their original order.
- Evidence: documented. [F-ARR] §sort: items "that have nothing to sort on go
  to the end"; an empty nickname "sorts with the text"; mixed kinds come back
  "grouped by kind rather than interleaved"; and `sort_natural` sends readers
  to `sort` for numeric order.
- Confidence: high (missing last, empty string as a value), medium (grouped
  kinds, numeric strings), low (the order between groups; P-LIQ-6).

---

## 14. Expression-variant filters (R-LIQ-160 … R-LIQ-166)

**R-LIQ-160 Signature and evaluation.** Every expression filter is called as
`collection | f: "<var>", "<expr>"`.
1. The collection is coerced to an array.
2. For each element, a child scope is created that sees every outer variable
   and binds `<var>` to the element.
3. `<expr>` is a string. It is parsed on every evaluation as a Liquid
   expression (R-LIQ-74) followed by an optional filter chain (R-LIQ-73), for
   example `"o.date | date: '%Y'"`.
4. The predicate's truthiness follows R-LIQ-57.
- Evidence: documented ([F-EXP] intro and signature).
- Confidence: high.

**R-LIQ-161 The six filters.**
- `where_exp`: elements whose predicate is truthy (`users | where_exp: "u", "u.age >= 18"`).
- `reject_exp`: elements whose predicate is falsy.
- `find_exp`: the first truthy element, or nil.
- `find_index_exp`: its 0-based index, or nil.
- `has_exp`: a boolean.
- `group_by_exp`: groups `{name, items}` keyed by the expression's value, in
  the order keys are first seen. The docs example groups orders by
  `"o.date | date: '%Y'"`.
- Evidence: documented.
- Confidence: high (semantics), low where an example needs the numeric-string
  coercion of R-LIQ-76.

**R-LIQ-162 Nesting.** A predicate may itself call expression filters, as in
`"g.items | has_exp: 'i', 'i.active'"`. Such nesting ends normally.
- Evidence: documented.
- Confidence: high.

**R-LIQ-163 The 32-level limit.** Evaluating expression filters may nest at most
32 levels. Level 33 raises the filter error "expression filter nesting exceeds
32 levels" instead of evaluating. The canonical trigger is a self-referencing
predicate:
`{% assign pred = "users | where_exp: 'x', pred" %}{{ users | where_exp: "u", pred }}`.
The collection must be non-empty for any predicate to be evaluated.
- Evidence: documented ([F-EXP]; [TPL] §Limits: refused "after 32 levels").
- Confidence: high.

**R-LIQ-164 Where the error shows.** The limit and predicate errors surface
through R-LIQ-200/204:
- inside `{{ }}`: the page renders, with an inline Error item in place;
- inside `{% assign %}`: the render stops and "the page does not appear at
  all".
- Evidence: documented ([F-EXP]).
- Confidence: high.

**R-LIQ-165 Error messages stay bounded.** A nested failure MUST NOT make the
error message grow with each level. pagelike flattens it to one message
([D0002] found an exponential-size message in the stock engine).
- Evidence: inferred.
- Confidence: high.

**R-LIQ-166 `now` in the docs example.** The docs' `has_exp` example compares
`e.starts > now`. `now` is not a defined variable, so it is nil and the
comparison is false (C-18).
- Evidence: inferred.
- Confidence: medium.

---

## 15. Date and time filters (R-LIQ-170 … R-LIQ-178)

**R-LIQ-170 Input normalization.** Every date filter accepts these inputs and
normalizes them to UTC:
- An ISO-8601 date, `YYYY-MM-DD` → 00:00:00 UTC.
- An ISO-8601 datetime, `YYYY-MM-DDTHH:MM[:SS[.fraction]]`:
  - with `Z` or `±HH:MM`/`±HHMM`, converted to UTC, so
    `2026-04-12T13:45:00+02:00` is 11:45 UTC;
  - with no offset, taken as UTC;
  - a space instead of `T` is accepted (decision);
  - fractions such as `.000Z`, which appear in the polls data, are accepted and
    truncated to the second.
- A Unix timestamp in seconds: an integer, or a string that is an optional sign
  followed by digits. A float, or a float string, loses its fraction (low).
- `"now"`: the request instant. There is one instant per request, shared by
  every use (decision).
- `"today"`: 00:00:00 UTC of the current UTC date.
- nil or `""`: the filter returns `""` without an error (decision, low).
- Anything else, including an item or an array, is a filter error.
- Evidence: documented for the first four input kinds, `now`, `today`, and
  UTC normalization ([F-DATE] intro); inferred for the rest.
- Confidence: high (documented inputs), low (the others).

**R-LIQ-171 `date[: format = "%Y-%m-%d"]`.** Directives, all in UTC and all
English:

| Directive | Meaning |
|---|---|
| `%Y` | 4-digit year |
| `%m` | month, `01`–`12` |
| `%d` | day, `01`–`31` |
| `%H` | hour, `00`–`23` |
| `%M` | minute, `00`–`59` |
| `%S` | second, `00`–`59` |
| `%B` / `%b` | full / abbreviated month name |
| `%A` / `%a` | full / abbreviated weekday name |
| `%j` | day of the year, `001`–`366` |
| `%p` | `AM`/`PM` |
| `%Z` | `UTC` |
| `%z` | `+0000` |
| `%%` | `%` |

Any other directive (`%e`, `%-d`, `%I`, `%y`, `%s`, …) is copied unchanged,
`%` included. The default format differs from Shopify, where the format is
required.
- Docs examples: `"2026-04-12" | date: "%B %Y"` → `April 2026`;
  `"2026-04-12T13:45:00Z" | date: "%H:%M"` → `13:45`;
  `"now" | date: "%Y"` → the current year.
- Derived: `"2026-04-12" | date: "%H:%M %z %j %A %a %b %p"` →
  `00:00 +0000 102 Sunday Sun Apr AM`.
- Evidence: documented.
- Confidence: high.

**R-LIQ-172 `date_add: seconds`.** Adds `seconds` to the date and returns
`YYYY-MM-DDTHH:MM:SSZ`, with no fraction. The seconds may be an integer or a
numeric string, negative included; a non-numeric value is a filter error.
Docs example: `"2026-01-01T00:00:00Z" | date_add: 3600` → `2026-01-01T01:00:00Z`.
- Evidence: documented.
- Confidence: high.

**R-LIQ-173 `unix_to_iso`.** Same output format as `date_add`. Docs example:
`1767225600 | unix_to_iso` → `2026-01-01T00:00:00Z`. It accepts every input
kind in R-LIQ-170 (inferred).
- Evidence: documented.
- Confidence: high.

**R-LIQ-174 The Jekyll formats.** These four take no argument. Extra arguments
are ignored (decision).

| Filter | Format | Docs example (`"2026-07-07T13:07:59Z"`) |
|---|---|---|
| `date_to_string` | `%d %b %Y` | `07 Jul 2026` |
| `date_to_long_string` | `%d %B %Y` | `07 July 2026` |
| `date_to_rfc822` | `%a, %d %b %Y %H:%M:%S +0000` | `Tue, 07 Jul 2026 13:07:59 +0000` |
| `date_to_xmlschema` | `%Y-%m-%dT%H:%M:%S+00:00` | `2026-07-07T13:07:59+00:00` |

- Evidence: documented.
- Confidence: high.

**R-LIQ-175 No time zones.** The output is always UTC. There is no time-zone
argument, no site time zone, and no locale.
- Evidence: documented ([F-DATE] "normalises them to UTC").
- Confidence: high.

**R-LIQ-176 Unparseable input.** An unparseable input is a filter error (R-LIQ-200).
The docs use `date` given an unparseable string as their example of a filter
error.
- Evidence: documented ([FLT]).
- Confidence: high.

**R-LIQ-177 Microdata dates.** A date from microdata, such as
`<time itemprop="publishedAt" datetime="2026-08-03">`, reaches Liquid as the
string `2026-08-03` (R-LIQ-52) and parses as a date. [BLOG] prints it raw.
- Evidence: documented.
- Confidence: high.

**R-LIQ-178 `now` and caching.** A template that renders `"now"` or `"today"`
output still follows R-LIQ-64 for caching. PageLove documents no
`Cache-Control` for time-dependent output, and pagelike adds none (decision).
- Evidence: inferred.
- Confidence: low.

---

## 16. Hashing and security filters (R-LIQ-180 … R-LIQ-183)

**R-LIQ-180 `sha256`.** The lower-case hex SHA-256 of the input's UTF-8 bytes,
after string coercion. Docs example: `"hello" | sha256` →
`2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824`.
- Evidence: documented.
- Confidence: high.

**R-LIQ-181 `bcrypt[: cost = 12]`.**
- The output is a 60-character modular-crypt string
  `$2b$<cost as 2 digits>$<53 chars>` with a fresh random 16-byte salt.
- `cost` must be an integer from 4 to 31. Outside that range, or non-integer,
  it is a filter error; the docs give an out-of-range hashing parameter as an
  error example.
- pagelike MAY refuse costs above a configured ceiling (default 16) with a
  filter error, to protect the budget (decision).
- An input longer than 72 bytes is a filter error (decision, low).
- Docs examples: `"password" | bcrypt`, `"password" | bcrypt: 10`.
- Evidence: documented.
- Confidence: high (format, default), low (the `2b` prefix, the 72-byte rule).

**R-LIQ-182 `argon2`.** Argon2id with version `v=19` and parallelism 1
(inferred). Keyword arguments:
- `format`: `"phc"` (default) or `"raw"`. Any other value is a filter error.
- `salt`: required for `raw`, at least 8 bytes of UTF-8. It is ignored in PHC
  mode, which uses a random 16-byte salt.
- `memory`: KiB, default `19456`, minimum 8.
- `time`: default `2`, minimum 1.
- `length`: bytes, default `32`, range 4–64.

Outputs:
- PHC: `$argon2id$v=19$m=<memory>,t=<time>,p=1$<salt, base64 without padding>$<hash, base64 without padding>`.
- Raw: the lower-case hex of `length` bytes. It is deterministic for a given
  input, salt and parameters.

Errors: `raw` without a salt, a salt under 8 bytes, a parameter out of range,
an unknown keyword. pagelike MAY cap `memory` (default cap 262144 KiB) and
`time` (default cap 10) with a filter error (decision).
- Docs examples: `"password" | argon2`;
  `argon2: memory: 65536, time: 3`;
  `argon2: format: "raw", salt: "per-user-unique-salt"`.
- Evidence: documented ([F-SEC] parameter table).
- Confidence: high (parameters, defaults, errors), low (`p=1`, the salt
  length).

**R-LIQ-183 Security filters are not safe-marked.** No security filter's output
is marked safe. Their alphabets contain no HTML-special characters, so this
does not matter.
- Evidence: inferred.
- Confidence: medium.

---

## 17. Random generation filters (R-LIQ-185 … R-LIQ-187)

**R-LIQ-185 `random`.** The input is the length.
- The length must be an integer, or a numeric string, of at least 1. pagelike
  caps it at 4096. Anything else is a filter error.
- Characters come from the OS CSPRNG, uniformly from the alphabet, and the
  result is shuffled after the minima are placed.
- The alphabet:
  - with no class keyword, `A–Z a–z 0–9`;
  - otherwise the union of the classes enabled by the keywords below. So
    `lower: true, digits: true` gives only `[a-z0-9]`.
- A class keyword takes `true` (include it), `false` (exclude it) or an
  integer n (include it, with at least n characters from it):
  - `upper`: `A–Z`
  - `lower`: `a–z`
  - `digits`: `0–9`
  - `symbols`: `!@#$%^&*()-_=+[]{}` plus `;:,.<>?`. The last seven are a
    pagelike decision: the docs' list ends in "…".
  - `alphanumeric`: `A–Z a–z 0–9`
- `url_safe: true`: the alphabet is exactly `A–Z a–z 0–9 - _`.
- `chars: "<set>"`: a custom set of code points, deduplicated.
  `chars_min: n` sets the minimum drawn from it.
- If the minima sum to more than the length, or the alphabet is empty, that is
  a filter error.
- Docs examples: `32 | random` (32 alphanumerics); `24 | random: upper: 3, lower: 3, digits: 2`;
  `16 | random: lower: true, digits: true`.
- Evidence: documented ([F-RND] table).
- Confidence: high (length, default alphabet, class selection), low (the
  symbol set, the caps).

**R-LIQ-186 `diceware`.** The input is the word count, clamped to 1–10.
Default 3 for nil or non-numeric input. The words are drawn uniformly (CSPRNG)
from a list of at least 1,600 lower-case ASCII words with no `-` in them, and
joined with `-`, for example `fuzzy-blue-wombat`. pagelike MUST ship its own
openly licensed word list; PageLove's list is not available.
- Evidence: documented.
- Confidence: high (shape, clamp), low (the word list).

**R-LIQ-187 Random output does not repeat.** Random filters give different
output on each render. This does not make the page private. The docs call
templates "deterministic" anyway (C-5).
- Evidence: inferred.
- Confidence: medium.

---

## 18. Data and JSON filters (R-LIQ-190 … R-LIQ-192)

**R-LIQ-190 `json[: indent]`.** JSON text.
- Without an indent it is compact: no spaces, with `,` and `:` as separators.
- `indent: n` with n > 0 pretty-prints with n spaces per level, `": "` between
  key and value, and one member per line.
- Strings are UTF-8 and are **not** HTML-escaped (`<`, `>` and `&` stay
  literal).
- nil → `null`. Integral floats are written as `100.0` (low).
- Hashes keep insertion order.
- An item becomes an object: `@id`, `@type`, then its properties in
  first-occurrence document order, with values as in R-LIQ-52 (low).
- Docs examples: `{{ request | json }}` (compact); `{{ request | json: 2 }}`
  (pretty, 2-space).
- Evidence: documented.
- Confidence: high (compact vs indent), low (item shape, `<`; P-LIQ-13).

**R-LIQ-191 `jsonify`.** Identical to `json`, including the indent argument.
- Evidence: documented.
- Confidence: high.

**R-LIQ-192 `inspect`.** Identical to compact `json`.
- Evidence: documented.
- Confidence: high.

---

## 19. Errors

**R-LIQ-200 Output expressions fail locally.** When evaluating one `{{ … }}` or
`{% echo %}` fails, only that expression is affected. Such failures include a
filter error (R-LIQ-102), an unknown filter, a predicate syntax error, and the
depth limit. The rest of the template and the page compose normally, and the
response status is unaffected (200).
- Evidence: documented ([FLT] §When a filter errors: "a single bad value never
  blanks the whole page"; [F-EXP]).
- Confidence: high (behavior), low (unknown filters degrading rather than
  failing the page; P-LIQ-4).

**R-LIQ-201 What a failed expression renders.**
- **Markup context:** an inline Error item. pagelike's markup is
  `<span itemscope itemtype="https://pagelove.org/Error"><meta itemprop="kind" content="TemplateError"><span itemprop="message">MESSAGE</span></span>`.
  MESSAGE is HTML-escaped, at most 300 characters, and names the filter.
- **Every other context:** the empty string. That covers the inside of a tag
  (an attribute value, or between attributes), an HTML comment, the RAWTEXT
  and RCDATA elements (`script`, `style`, `title`, `textarea`, `xmp`,
  `iframe`, `noembed`, `noframes`), and in XML a tag, comment, CDATA section
  or processing instruction.
- The context is the HTML tokenizer state at the `{{` in the template source,
  or the XML equivalent in XML documents.
- Evidence: documented for the markup marker's itemtype and for the empty
  attribute value ([FLT]: "In an attribute value … the expression renders
  empty instead"). The exact markup and the raw-text rule are pagelike
  decisions ([D0002]).
- Confidence: high (itemtype, attribute rule), low (exact markup, other
  contexts; P-LIQ-2).
- Harness cases assert only
  `itemtype="https?://pagelove.org/(1\.0/)?Error"` (PageLove uses several
  Error vocabularies; [RW] R-RW-130).

**R-LIQ-202 Control-flow errors fail composition.** An error while evaluating
any of these is a composition error (R-LIQ-206), not degraded:
- the condition of `if`, `elsif`, `unless`, `case` or `when`, filter chains
  included;
- the collection or range of `for` or `tablerow`.

The docs' reason: "there is no safe partial meaning for a failed condition or
loop".
- Evidence: documented ([FLT]).
- Confidence: high (behavior), low (status).

**R-LIQ-203 Undefined names are nil.** An undefined variable, a missing hash
key, a missing item property, or a member of nil evaluates to nil, in every
context, and is never an error:
- `{{ nosuch }}` renders `""`;
- `{% if nosuch %}` is false;
- `{% assign w = w | push: x %}` builds a list from nothing.
- Evidence:
  - documented: [F-ARR] §push, where an unassigned variable "is `nil`"; [SSX],
    where anonymous `request.auth.*` is falsy "— there is no error".
  - demo-source: [KAN]'s anonymous render.
- Confidence: medium. Contradiction C-2: [SSX] says bare rule-only names raise
  "undefined variable". That claim is kept as a disputed case (P-LIQ-3).

**R-LIQ-204 Errors in `assign` and `capture`.**
- An error while evaluating an `assign` right-hand side is a composition error
  (R-LIQ-206); "the page does not appear at all".
- Inside a `capture` block, an output expression degrades as in R-LIQ-200,
  with the marker captured into the variable.
- Evidence: documented ([F-EXP]) for `assign`, inferred for `capture`.
- Confidence: high (`assign`), low (`capture`).

**R-LIQ-205 Template syntax errors fail composition.** These are composition
errors:
- an unterminated `{{` or `{%`;
- an unknown tag;
- an unbalanced block;
- `include`/`render`;
- a malformed expression in a tag or output.

A malformed predicate *string* passed to an expression filter is only a runtime
filter error (R-LIQ-200).
- Evidence: inferred.
- Confidence: low (P-LIQ-4).

**R-LIQ-206 The composition-error response.**
- Status `500 Internal Server Error`.
- `Content-Type: text/html; charset=utf-8`.
- The body is the [RW] R-RW-130 "all others" Error document: itemtype
  `https://pagelove.org/Error`, with `status` 500, `kind` `TemplateError`, and
  a `message` holding the Liquid error and, where known, the 1-based line in
  the template source.
- There is no partial page and no `ETag`.
- The same applies to Range reads and edge `QUERY` of the page.
- Evidence: inferred from [ME] §Error cases (an implementation error is "HTTP
  500 and the error message") and [EB]/[JB] ("Request fails with an error
  during composition").
- Confidence: medium (the request fails, no partial page), low (500; P-LIQ-4).

**R-LIQ-207 Budget exhaustion.** Covered by R-LIQ-212 (`503`).

**R-LIQ-208 Errors in resource creation.** When a resource-creation template
fails, the POST gets the R-LIQ-206 response and nothing is written. Missing
`<base href>` is `422` (area `composing`, [RC]).
- Evidence: inferred for the failure; documented for 422.
- Confidence: medium.

**R-LIQ-209 Error messages leak nothing.** Messages MUST NOT contain server file
paths, stack traces or other documents' content.
- Evidence: inferred.
- Confidence: high.

---

## 20. Budgets and limits

**R-LIQ-210 One shared budget.** Templates draw on the same per-request budget
as Sessel, JavaScript, bindings and method dispatch. The axes are work
(operations), wall time and memory. PageLove reads the budget from the host's
`https://pagelove.org/TransactionBudget` microdata.
- Evidence: documented ([TPL] §Limits: "every axis of it applies"; [JSS]
  §Resource limits).
- Confidence: high.

**R-LIQ-211 What is charged.**
- **Work:** at least one unit for each executed tag, output and filter call.
  Each loop iteration (`for`, `tablerow`) is charged. Filters that walk a list
  (`sort`, `sort_natural`, `uniq`, `where`, `find`, `reject`, `map`,
  `group_by`, the `_exp` variants, `sum`, `join`, `concat`, `compact`) charge
  per element while they run and check the time limit as they go.
- **Time:** a long render is interrupted.
- **Memory:** every byte written to the output, and every value stored by
  `assign`/`capture` (a string by its length, a list by the sum of its
  elements). A value taken from the site and placed into the page whole is not
  charged; the docs' example is content "shared rather than copied".
- Evidence: documented ([TPL] §Limits; [F-ARR] intro).
- Confidence: high (categories), low (units).

**R-LIQ-212 Exhaustion fails the request.** Exhausting any axis fails the whole
request with `503 Service Unavailable` and an Error document of kind
`BudgetExceeded`.
- It is never degraded inline, not even inside `{{ }}`, and never returns a
  partial page.
- Evidence: documented. [TPL]: "fails the request rather than returning a
  partial page". [F-ARR]: fails "with a time-limit error". [AZR]: templates in
  rule fields could once cause "the `503 Service Unavailable` a budget failure
  produces elsewhere". [ME]: composition budget → 503.
- Confidence: high (fails), medium (503).

**R-LIQ-213 pagelike's default limits.** These are per-site configurable
defaults and make no compatibility claim:

| Limit | Default |
|---|---|
| wall time for the whole composition | 2 s |
| work | 10,000,000 units per request |
| template output plus stored values | 16 MiB |
| expression-filter nesting | 32 (a documented compatibility value) |
| maximum `random` length | 4096 |
| maximum `bcrypt` cost | 16 |
| maximum `argon2` memory | 262144 KiB |

PageLove's actual numbers are unknown.
- Evidence: inferred.
- Confidence: low.

**R-LIQ-214 Budget headers.** pagelike SHOULD report consumption, templates
included, in `X-Budget-Consumed-Ops`, `X-Budget-Consumed-Memory` and
`X-Budget-Consumed-Time`, as PageLove does ([LIVE], [LO-1]). The values are not
comparable, and the harness normalizes them away.
- Evidence: live-observed (the header names).
- Confidence: medium.

**R-LIQ-215 Nesting limit is a filter error.** The 32-level nesting limit
(R-LIQ-163) raises a filter error, not a budget failure.
- Evidence: documented.
- Confidence: high.

**R-LIQ-216 The composition dispatch budget.** Each template dispatch counts as
one dispatch toward the composition's 500-dispatch budget. Beyond it:
`503 composition budget exceeded`.
- Evidence: documented for method dispatch ([ME]); inferred for templates.
- Confidence: low.

---

## 21. Caching, validators and writes

**R-LIQ-220 Cache-Control.** Covered by R-LIQ-64 (cross-area [RW] R-RW-102/103).

**R-LIQ-221 ETags of composed pages.** The validator of a templated page is
derived from the served bytes ([RW] R-RW-96), so pages with `now` or `random`
output get a fresh `ETag` per render. `If-None-Match` compares against a fresh
composition.
- Evidence: inferred.
- Confidence: medium.

**R-LIQ-222 Selector writes into template output.** Elements produced by
rendering have no writable origin.
- A `PUT`, `POST` or `DELETE` whose first selector match lies strictly inside a
  host's rendered content gets `416 Range Not Satisfiable`, and nothing is
  written.
- The host element itself maps to its stored element, so a write to the host
  edits the stored template source.
- Evidence: inferred by analogy with [PR] (a matched element "that is not
  stamped/included, so it has no writable origin" → 416).
- Confidence: low (P-LIQ-16).

**R-LIQ-223 No events for bound data.** Changes to data a template reads through
a binding produce no SSE events on the template page. Streams are per stored
document (area `sse`).
- Evidence: inferred.
- Confidence: medium.

**R-LIQ-224 HEAD.** HEAD composes exactly as GET does, templates included, and
sends no body ([RW] R-RW-24).
- Evidence: documented (HEAD semantics).
- Confidence: high.

**R-LIQ-225 JSON-LD.** A JSON-LD response for a templated page reflects the
rendered microdata (R-LIQ-24).
- Evidence: documented ([CN]).
- Confidence: medium.

---

## 22. Security summary

Templates run with the page's authority over the whole site (R-LIQ-94). They
are side-effect free (R-LIQ-26) and cannot read files (R-LIQ-77). Data is
never re-evaluated as Liquid (R-LIQ-92). Output is not auto-escaped (R-LIQ-90),
so authors must escape untrusted values (R-LIQ-93). Liquid is never applied to
rule fields (R-LIQ-8). Budgets bound every render (R-LIQ-210…216), and error
messages leak nothing (R-LIQ-209).

---

## 23. Cross-area dependencies

- **X-1 composing: bindings.** `r:`, `e:` and `j:` evaluation, the order in
  which resource bindings return elements (R-LIQ-51), and Context scoping
  (R-LIQ-40/41). Also the dispatch order that runs `*:template` last
  (R-LIQ-21), attribute stripping (R-LIQ-30) and the namespace URIs.
  - The older resource-binding namespace `https://pagelove.org/1.0/Resource`
    ([SKILL-OLD]:376-382) versus the current `https://pagelove.org/Binding/CSS`
    ([RB]) is a `composing` decision. Liquid is unaffected.
- **X-2 composing: walk.** Includes, stamps, method elements, transients and
  pagination inside or around hosts (R-LIQ-22/23/24); `xmlns:` stripping
  (R-LIQ-33).
- **X-3 composing: resource creation.** POST to a template: `request.method` is
  `POST`, and `request.body` holds the form fields (R-LIQ-60). The output must
  contain `<base href>`, the response is 301 with `Location`, and a missing
  `<base>` is 422. The created document is stored rendered, with directive
  attributes removed and `xmlns:` kept. [POLL-OUT] suggests the `<base>` element
  is removed from the stored copy (demo-source, low).
- **X-4 composing: parameterized routes.** `request.params` and `request.path`
  on route pages (R-LIQ-60).
- **X-5 dom.** Source spans for raw template text (R-LIQ-10), a
  structure-preserving parse of the output (R-LIQ-31), attribute-name case
  (R-LIQ-42), the XML dialect (R-LIQ-35), and byte fidelity (R-LIQ-34; [RW]
  R-RW-21, [LO-1]).
- **X-6 reading-writing.** The composed view for GET, Range and HEAD (R-LIQ-20);
  JSON-LD after templating (R-LIQ-225); `Cache-Control: private`
  (R-LIQ-64 ↔ R-RW-103); composed ETags (R-LIQ-221 ↔ R-RW-96); Error
  documents (R-LIQ-206 ↔ R-RW-130); the microdata value function (R-LIQ-52 ↔
  R-RW-50); the Request Document (R-LIQ-60 ↔ R-RW-135..137); writes into
  template output (R-LIQ-222 ↔ R-RW-16).
- **X-7 permissions-identity.** The shape of `request.auth`, with both `role`
  and `roles` ([PERM] R-PERM-74); no Liquid in rules ([PERM] R-PERM-36);
  bindings bypass authorization (R-LIQ-94).
- **X-8 modeling.** Typed instances and cardinality. Resource-binding elements
  currently read without schema typing ([PROP]); if a later release types
  them, R-LIQ-52 and R-LIQ-76 change. Whether `@read` resolvers apply to values
  read by a template is open (P-LIQ-12).
- **X-9 sessel / server-js.** Value conversion (R-LIQ-59) and the shared budget
  (R-LIQ-210).
- **X-10 protocol.** Edge `QUERY` sees the composed page; DAV reads and QUERY
  see the raw source ([PROTO] R-PROTO-61, dav rows).
- **X-11 budgets.** TransactionBudget, the 503 shape, and the
  `X-Budget-Consumed-*` headers (R-LIQ-210…216).

---

## 24. Contradictions and compatibility decisions

**C-1 Output placement.**
- Claims: the docs prose says the server "replaces the element with the
  output" and the engine processes the subtree, "replacing it" ([LIQ], [TPL]).
  Every concrete example keeps the host element and removes only the directive
  attributes: [TPL]'s worked example, [QRY]'s `<main class="calc">5</main>`,
  [KAN], [POLL-OUT].
- Decision: keep the host and replace its children (R-LIQ-30). Do not render
  the host's own attributes (R-LIQ-11, low).

**C-2 Undefined variables.**
- Claims:
  - [SSX] says referencing a bare rule-only name in a template "raises
    `undefined variable`".
  - [SKILL-OLD]:387 reports "Unknown variable" errors from an older engine.
  - [F-ARR] documents the unassigned-variable `push` idiom, where the variable
    is nil.
  - [SSX] and [KAN] show missing `request.auth.*` members as empty with no
    error.
- Decision: lax (R-LIQ-203). The `[SSX]` claim is kept as a
  `status: disputed` case, `liquid.request.bare-rule-names-error`.
  [PERM] R-PERM-74 repeats [SSX]; this spec reads that sentence as describing
  Sessel bindings. It is flagged to the permissions area.

**C-3 Liquid inside tables.**
- Claims: [SKILL-OLD]:385-387 (April 2026) says foster parenting moves
  `{% %}` out of `<table>`/`<tbody>` and breaks templates. [POLL-TPL] (September
  2026) relies on Liquid between table cells, and [POLL-OUT] shows correct
  output.
- Decision: render the raw source (R-LIQ-10). The skill describes an older
  release.

**C-4 Autoescape.**
- Claims: [F-STR] says `escape`'s result is "marked safe, so it is not escaped
  again", which suggests autoescape. PageLove's own templates escape untrusted
  values explicitly, and [BLOG] escapes only in XML.
- Decision: no autoescape. Safe marking makes `escape` idempotent (R-LIQ-90/91).
  The autoescape reading is kept as a disputed case (P-LIQ-1).

**C-5 Determinism.**
- Claims: [TPL] says execution is "side-effect free, and deterministic", yet
  `random`, `diceware`, `bcrypt`, `argon2` (PHC salt) and `"now"` are
  non-deterministic.
- Decision: read "deterministic" as "no side effects". `now` is fixed per
  request (R-LIQ-170).

**C-6 Role list spelling.**
- Claims: [SSX] and [SKILL-OLD] use `request.auth.roles`; [KAN] uses
  `request.auth.role`.
- Decision: expose both, the same list (R-LIQ-61, as [PERM] R-PERM-74 does).

**C-7 Liquid in authorization rules.**
- Claims: [SKILL-OLD]:101-128 documents Liquid in rule fields. [AZR] and
  [SKILL]:272 say it was removed.
- Decision: never evaluate it there (R-LIQ-8; [PERM] R-PERM-36).

**C-8 QUERY example.**
- Claims: [QRY]'s response blocks are misaligned. The composed-page request
  shows the stored markup under `HTTP/1.1 201`, and the composed result
  `<main class="calc">5</main>` appears under the next (Sessel) request.
- Decision: the edge QUERY sees the composed page, whose shape is
  `<main class="calc">5</main>` (R-LIQ-30; [PROTO] C-8).

**C-9 Resource-binding namespace.**
- Claims: `https://pagelove.org/1.0/Resource` ([SKILL-OLD]) versus
  `https://pagelove.org/Binding/CSS` ([RB]).
- Decision: deferred to `composing` (X-1).

**C-10 `has: "onsale", true` against string microdata.**
- Claims: the [F-ARR] example compares a field to the boolean `true`, but
  microdata values are strings (R-LIQ-52).
- Decision: `"true"` matches `true` in the field-equality filters (R-LIQ-146,
  low; [D0002] made the same assumption).

**C-11 Escapes in string literals.**
- Claims: Shopify processes no escapes. The docs examples
  `"a\nb\nc" | strip_newlines` → `abc` and `"a\nb" | newline_to_br` → `a<br />\nb`
  only work if `\n` is a newline.
- Decision: double-quoted literals process `\n \t \" \\` (R-LIQ-74, low;
  P-LIQ-15).

**C-12 Deviations from Shopify that PageLove documents.** These are not
contradictions between PageLove sources; pagelike follows PageLove:
- `divided_by`/`modulo` by zero return the input;
- `date` defaults its format and accepts `now`/`today`;
- `"" | split` gives `[""]`;
- `size` of a non-collection is `0`;
- `sort` groups mixed kinds.

**C-13 The float in `Sum: 100.0`.**
- Claims: [EB] prints `Sum: 100.0` but does not show the template, which the
  docs build lost (`{% example %}`).
- Decision: integral floats render with `.0` (R-LIQ-56, medium).

**C-14 The fixture page.**
- Claims: the combined page and [FIX] contain a fixture, `People` with an
  empty `<ul>`, rendered by the docs site's own build. It is not a PageLove
  observation.
- Decision: use it only as weak support for R-LIQ-32 and for the whitespace
  shape of the worked example.

**C-15 Content lost from the docs.**
- Claims: the docs build (Eleventy/LiquidJS) swallowed several Liquid snippets:
  - the Liquid example in [PR] §Reading captured parameters (rendered as an
    empty code span);
  - the example in [PROP] (an empty `liquid` block);
  - the `{% example "…" %}` source documents in [TPL], [EB], [JB], [RD] and
    [RC].
- Decision: reconstruct the templates from the printed outputs. The cases say
  "reconstructed" in `notes`.

**C-16 The `e:localcount` example.**
- Claims: [EB] §Scope's `e:localcount="${div.item} from self).count()"` has
  unbalanced parentheses, a doc bug.
- Decision: the harness case uses a valid expression.

**C-17 Reliability.**
- Claims: [DEMO5] says Liquid composition "proved unreliable" and switched to
  client JS, without detail.
- Decision: informative only.

**C-18 `now` in `has_exp`.**
- Claims: the [F-EXP] example uses `now` as a variable.
- Decision: there is no such variable; it is nil (R-LIQ-166).

**C-19 Divergences from the spike.** Where this spec differs from [D0002]'s
prototype:
- `{{ item }}` renders trimmed text in both (no difference).
- An unknown filter degrades inline in both.
- This spec adds safe-mark idempotence (R-LIQ-91).
- This spec treats template syntax errors as 500 (R-LIQ-205).
- This spec adds `role` alongside `roles` (R-LIQ-61).
- This spec makes anonymous `username` nil rather than `""`, because `""` is
  truthy and would break `{% if request.auth.username %}`.

---

## 25. Open questions for live probing

Setup for every probe: author `PUT` (DAV) under a disposable prefix `/p`, a rule
file granting `*` `GET` on `/p/*`, and anonymous `GET` on the public plane
unless stated otherwise. Each probe is a runnable harness case (§26).

| ID | Question | Minimal probe |
|---|---|---|
| P-LIQ-1 | Autoescape? Is `escape` idempotent? | `/p/e.html`: `<p id="a" p:template="text/liquid">{{ request.query.q }}</p><p id="b" p:template="text/liquid">{{ "<" \| escape \| escape }}</p>`; `GET /p/e.html?q=%3Cb%3Ex%3C%2Fb%3E`. Raw `<b>x</b>` means no autoescape. `#b` equal to `&lt;` means safe-marking. |
| P-LIQ-2 | Exact inline Error markup; behavior in `<title>`/`<script>` | `<p p:template="text/liquid">A{{ "zz" \| date: "%Y" }}B</p><title p:template="text/liquid">{{ "zz" \| date }}</title>`: capture the body; is `<title>` empty or does it contain the marker text? |
| P-LIQ-3 | Undefined names: nil or error? | `<p p:template="text/liquid">[{{ nosuch }}\|{{ method }}\|{{ auth.claims.email }}]</p>` → `[\|\|]` versus an Error item versus 500. Also `{% assign w = w \| push: 1 %}{{ w.size }}` → `1`. |
| P-LIQ-4 | Status for control-flow, assign, syntax and unknown-filter errors | Four pages: `{% if "zz" \| date: "%Y" %}`, `{% assign y = "zz" \| date: "%Y" %}`, `{% if %}` left unclosed, `{{ 1 \| nosuchfilter }}`; record status and body. |
| P-LIQ-5 | Numeric-string comparison | `{% assign a = "34" %}{% if a >= 18 %}Y{% else %}N{% endif %}{% if a == 34 %}E{% endif %}`; and `"10,9" \| split: "," \| sort \| join: ","`. |
| P-LIQ-6 | Order of `sort` groups | `nil \| push: 2 \| push: "b" \| push: 1 \| push: "a" \| sort \| join: ","` → `1,2,a,b` or `a,b,1,2`? |
| P-LIQ-7 | Host attributes rendered? | `<div id="h" data-v="{{ 1 \| plus: 1 }}" p:template="text/liquid">x</div>` → `data-v="2"` or the literal? |
| P-LIQ-8 | Nested hosts | `<div p:template="text/liquid">{% assign x = 2 %}<section p:template="text/liquid">[{{ x }}]</section></div>` → `[2]` (outer only) or `[]` (inner re-rendered)? |
| P-LIQ-9 | Descendant binding visible? | `<div p:template="text/liquid"><p e:x="1 + 1">[{{ x }}]</p></div>` with `xmlns:e` declared. |
| P-LIQ-10 | Directives in template output processed? | `<div p:template="text/liquid">{% if true %}<p:include selector="#nav" resource="/p/partials.html"></p:include>{% endif %}</div>` → is the header included? |
| P-LIQ-11 | Case of binding names | `<div r:myItems="…" p:template="text/liquid">[{{ myItems.size }}\|{{ myitems.size }}]</div>`. |
| P-LIQ-12 | Item shape: `@id` form, `@type`, non-item elements, `{{ item }}`, `@read` | Bind `[itemtype='…/T']` over a doc with `<div id="a" itemscope itemtype="…/T"><span itemprop="n">x</span></div>`; render `[{{ t['@id'] }}\|{{ t['@type'] }}\|{{ t }}\|{{ t \| json }}]`. |
| P-LIQ-13 | Rendering floats, arrays, hashes, items | `{{ 10 \| plus: 5.0 }}\|{{ "a,b" \| split: "," }}\|{{ 7 \| divided_by: 2.0 }}`. |
| P-LIQ-14 | Loop modifier order; `tablerow` markup | `{% for i in (1..5) reversed limit:2 %}{{ i }}{% endfor %}`; `{% tablerow i in (1..2) %}{{ i }}{% endtablerow %}`. |
| P-LIQ-15 | Literal escapes; trailing empties in `split` | `{{ "a\nb" \| size }}` (3 or 4?); `{{ "a,b,," \| split: "," \| size }}`. |
| P-LIQ-16 | Selector writes into template output | Page with `<ul id="l" p:template="text/liquid">{% for i in (1..2) %}<li id="i{{ i }}">{{ i }}</li>{% endfor %}</ul>` and a PUT rule on `/p/*`; `PUT Range: selector=#i1` → 416? `POST Range: selector=#l` → where does it land? |
| P-LIQ-17 | Unknown engine | `p:template="text/mustache"` → 500, verbatim, or stripped? |
| P-LIQ-18 | Budget status and error shape | `{% for i in (1..100000000) %}{% endfor %}` → 503? Body itemtype? (Opt-in: this spends production budget.) |
| P-LIQ-19 | Cache-Control for `request.headers` | `{{ request.headers['x-probe'] }}` → `private`? |
| P-LIQ-20 | Resource-creation output | POST to a template whose output has `<base href>`; GET the created document; is `<base>` present? `p:template`? `xmlns:p`? |

---

## 26. Case index (`harness/cases/liquid/`)

| File | Covers |
|---|---|
| `declaring.yaml` | R-LIQ-1…8: prefixes, namespace binding, inert unbound prefix, text outside hosts, unknown engine, `<html>` host, nested hosts |
| `output.yaml` | R-LIQ-10…14, 30…34: host kept, attributes stripped, exact whitespace, trim markers, empty output, tables, `<title>`/attribute rendering, host attributes not rendered, xmlns stripping |
| `bindings.yaml` | R-LIQ-40…59, 94: the worked people example, TeamMember sort and `@id`, `<body>` host, multi-valued route, missing properties, microdata value kinds, nested items, polls `@id` URL, `e:` count/sum, declaration order, scope, `j:`, stored templates seen raw, denied data rendered, empty bindings, descendant binding |
| `request.yaml` | R-LIQ-60…65: debug JSON, method/path/query, custom header private, shop authorization header, route params, anonymous auth, bare names (plus a disputed case), form POST resource creation |
| `tags.yaml` | R-LIQ-70…79: control flow, loops, `forloop`, `limit:`, filters in `if`, truthiness, `blank`/`empty`, `raw`, `include` refused, loop modifiers, numeric-string comparison |
| `escaping.yaml` | R-LIQ-90…93: no autoescape (plus a disputed case), `escape` idempotence, data not re-evaluated |
| `filters-string.yaml` | R-LIQ-110…125 |
| `filters-number.yaml` | R-LIQ-130…137 |
| `filters-array.yaml` | R-LIQ-140…156 |
| `filters-expressions.yaml` | R-LIQ-160…166 |
| `filters-date.yaml` | R-LIQ-170…178 |
| `filters-security.yaml` | R-LIQ-180…183 |
| `filters-random-data.yaml` | R-LIQ-185…192 |
| `errors.yaml` | R-LIQ-200…209 |
| `limits.yaml` | R-LIQ-210…216 (`live: false`, opt-in) |
| `composition.yaml` | R-LIQ-20…25, 221…225: JSON-LD, pagination, include in output, Range over output, Atom feed, DAV raw, writes into output |
| `apps.yaml` | whole templates from [BLOG], [SHOP-*], [POLL-*]; polls creation end to end |
