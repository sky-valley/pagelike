# Live reconciliation decisions: javascript (2026-09-29)

This file records how pagelike handled the 8 `javascript` cases that diverged
in the live PageLove run of 2026-09-29 (one sampled case per feature). It
follows the decision policy of [`../decisions.md`](../decisions.md):
`adopt-live` is the default, `keep-documented-security` and `keep-standard`
keep pagelike's behaviour and add a `live-divergence` case that asserts what
PageLove does, and `harness artifact` fixes the case or the harness.

**Evidence.**
- The sampled run: `harness/observations/live-2026-09-29/` (`RUN-SUMMARY.txt`
  plus one JSON file per case). A case id maps to its file by turning dots into
  dashes and cutting the name to 40 characters.
- This reconciliation: `harness/observations/live-2026-09-29-reconcile-javascript/`.
  - The top-level JSON files are the final run (run 4, 13 cases, all passed).
  - `exploratory/` holds the observations of runs 1 to 3 that the final run
    does not repeat, including probes that were later merged or dropped.
  - `RUN-SUMMARY.txt` lists all four runs.
- Probes: `harness/cases/javascript/dom-probes-0929.yaml`.
- Budget: 4 live runs and 40 case executions (limits: 4 runs, 15 cases per run).

## Summary

| Case | Class | PageLove | pagelike now |
|---|---|---|---|
| `javascript.client.head-etag-then-conditional-writes` | adopt-live (adopted on 2026-09-28, confirmed) | GET with a stale `If-Match`: 206 | Same |
| `javascript.query.javascript-body-415` | adopt-live (adopted on 2026-09-28, confirmed) | 400 "QUERY method requires Content-Type: text/sessel", no `Accept-Query` | Same |
| `javascript.dom.classlist` | adopt-live | Duplicate tokens are kept; `item()` out of range is `undefined` | Same |
| `javascript.dom.domparser-ignores-type` | adopt-live (tree building: keep-standard) | Implied `html`/`head`/`body` are left out | Same; the WHATWG tree builder is kept |
| `javascript.dom.namespace-pipe-selectors` | adopt-live | `*\|p` is `p` in any namespace; `\|p` is `p` without a namespace | Same |
| `javascript.dom.closest-and-matches` | harness artifact (node identity: keep-standard) | `closest` is inclusive, but `b.closest('button') === b` is false | Inclusive; `===` is true |
| `javascript.dom.tree-operations` | harness artifact (node identity: keep-standard) | 500: the case read `parentElement` of a top-level parsed node | Passes live after the rework |
| `javascript.dom.inner-outer-adjacent-html` | harness artifact | 500: the case read `d.body` | Passes live after the rework |

| Class | Cases |
|---|---|
| adopt-live | 5 (2 of them confirm the reconciliation of 2026-09-28) |
| keep-documented-security | 0 |
| keep-standard | 0 listed cases; 1 new pair (`javascript.dom.node-identity`), plus the tree-builder and stringifier parts below |
| harness artifact | 3 |

- **Final live run (run 4):** 13 cases, all passed:
  - the 8 listed cases;
  - the sibling `javascript.dom.node-identity.live`;
  - four probes: `domparser-shape`, `document-children`, `classlist-duplicates`
    and `kept-divergences`, the last with `status: live-divergence`.
- **The new case `javascript.dom.node-identity`** asserts standard node
  identity. It is `live: true`, as other kept main cases are, so it fails
  against live by design. It was not part of the final run, because its sibling
  measures the difference.

## Cases

### javascript.client.head-etag-then-conditional-writes

- **Class:** adopt-live. It was adopted on 2026-09-28 as a consequential update
  of `rw.cond.get-if-match-stale` (`../decisions.md`), and is confirmed live here.
- **Observations:**
  - `live-2026-09-29/javascript-client-head-etag-then-conditi.json` (run against
    the case as it stood before the merge of 2026-09-28);
  - `live-2026-09-29-reconcile-javascript/javascript-client-head-etag-then-conditi.json`
    (pass).
- **PageLove:**
  - HEAD with `Range: selector=#list` answers 206 with an element tag.
  - A PUT with that tag answers 206 with a new tag.
  - A GET with the stale `If-Match` answers **206** (the header is ignored).
  - A PUT with the stale tag answers 412 ("Precondition Failed: ETag does not
    match").
- **pagelike now:** the same. `If-Match` is ignored on GET (R-RW-140 as
  reconciled), and is enforced on writes.
- **Rationale:** the sampled run predates the merge of the 2026-09-28
  reconciliation, and its only failure was the superseded expectation of 412 on
  the GET. beta-js sends `If-Match` on element GETs, so ignoring it matches
  PageLove and harms no data, while writes stay protected. No code changed in
  this reconciliation. `docs/spec/javascript.md` R-JS-105 now carries the
  supersession note that the earlier reconciliation listed as outstanding.

### javascript.query.javascript-body-415

- **Class:** adopt-live. It was adopted on 2026-09-28 (C-9 reversed, see
  `protocol.query-sessel.415-unsupported-type`), and is confirmed live here.
- **Observations:**
  - `live-2026-09-29/javascript-query-javascript-body-415.json` (the old case,
    which had no QUERY grant: 401);
  - `live-2026-09-29-reconcile-javascript/javascript-query-javascript-body-415.json`
    (pass).
- **PageLove:**
  - A QUERY with `Content-Type: text/javascript` is authorized as QUERY. With no
    grant, the answer is 401.
  - With a grant, the answer is **400**, using the read-path error page, with the
    message "QUERY method requires Content-Type: text/sessel", and there is no
    `Accept-Query`.
- **pagelike now:** the same, through the Sessel path of `internal/query`.
- **Rationale:** the sampled run used the pre-merge case. That case had neither
  the QUERY grant (hence the 401) nor the reconciled expectation (415, with
  `Accept-Query`). The current case carries both fixes and passes live. A
  JavaScript body is not a query language PageLove supports, and 400 tells the
  client so. R-JS-4 now carries the supersession note.

### javascript.dom.classlist

- **Class:** adopt-live.
- **Observations:**
  - `live-2026-09-29/javascript-dom-classlist.json`;
  - `live-2026-09-29-reconcile-javascript/javascript-dom-classlist.json` and
    `javascript-dom-probe-0929-classlist-dupl.json`;
  - `exploratory/javascript-dom-probe-0929-classlist-dupl.json`.
- **PageLove:** the token list is the `class` attribute split on ASCII
  whitespace, **with duplicates kept**.
  - On `class="a b a"`:
    - `length` is 3;
    - `add('c', 'a')` gives `a b a c`;
    - `toggle('b')` and `remove('a')` drop every copy;
    - `replace('a', 'x')` changes only the first copy (`x a c z`);
    - `replace('a', 'b')` gives `b a`: the new token goes to the first copy of
      either token, the other copies of the new token are dropped, and the other
      copies of the old token stay.
  - `item()` out of range is `undefined`. `item('0')` throws `TypeError`.
  - Tokens are not validated: `add('')` and `add('a b')` throw nothing, and the
    latter writes `a a b`.
  - Emptying the list removes the attribute, and `value = ''` sets `class=""`.
  - Kept differences:
    - `String(classList)` is `[object Object]`;
    - `el.classList === el.classList` is false (see node identity below).
- **pagelike now:** `internal/jsrt/prelude_dom.js` `DOMTokenList` implements
  exactly the list semantics above. That covers duplicates kept, `remove` and
  `toggle` of every copy, the `replace` rule, `undefined` out of range, a
  `TypeError` for an index that is not a number, and no token validation.
  pagelike keeps two things:
  - the standard stringifier, which returns the class string;
  - one `classList` object per element.

  Unit test: `TestClassListKeepsDuplicates`.
- **Rationale:** the default applies. The differences show only when an author
  writes duplicate class tokens, or passes unusual arguments. pagelike then
  writes the same attribute bytes as PageLove, so documents stay
  byte-compatible, and the set of classes a selector sees is the same. The
  token validation that pagelike used to do was an inference ("like browsers"),
  and live evidence wins over inference. `String(classList)` returning
  `[object Object]` is kept standard, because adopting it would write plainly
  wrong data: an `el.className = String(other.classList)` would store
  `[object Object]`. The kept stringifier is measured by
  `javascript.dom.probe-0929.kept-divergences`.

### javascript.dom.domparser-ignores-type

- **Class:** adopt-live for the document shape; keep-standard for tree
  construction.
- **Observations:**
  - `live-2026-09-29/javascript-dom-domparser-ignores-type.json`: 500, "cannot
    read property 'firstElementChild' of null";
  - `exploratory/javascript-dom-probe-0929-domparser-shap.json`,
    `-domparser-xml.json`, `-domparser-tree.json` (a 500, see Open questions)
    and `-domparser-shap-run3.json`;
  - `live-2026-09-29-reconcile-javascript/javascript-dom-domparser-ignores-type.json`,
    `javascript-dom-probe-0929-domparser-shap.json` and
    `javascript-dom-probe-0929-kept-divergenc.json`.
- **PageLove:** `parseFromString` ignores the type. `application/xml` and
  `image/svg+xml` still give HTML elements with upper-case `tagName`. The
  document holds only what the markup wrote:
  - `'<ul>…</ul><p>x</p>'` has two children (`UL`, `P`). The `documentElement`
    is the first element, `head` and `body` are `null`, and a top-level element
    has the document as `parentNode` and `null` as `parentElement`.
  - Explicit `<html>`, `<head>` and `<body>` tags are kept.
  - `''` gives an empty document.
  - Its parser is not the WHATWG tree builder:
    - `<x/>` closes every element (`<span/>`, `<bar/>`, `<my-el/>`);
    - `<p>a<p>b` nests the second `p` in the first;
    - `<table><tr>` gets no `tbody`.
- **pagelike now:** `domState.parseDocument` (`internal/jsrt/dom.go`) parses
  with the same parser as stored documents. It then replaces every `html`,
  `head` or `body` element that has no start tag in the source with its
  children (`unwrapImplied`, using `dom.ImpliedFor`). A document accepts any
  children (several elements and text). The `outerHTML` setter and
  `insertAdjacentHTML('beforebegin'|'afterend')` work on a document's child,
  parsing in a `body` context, as the `document-children` probe shows PageLove
  does. The case now checks `P|null|null|1|true|<p>x</p>|SVG|<svg><g></g></svg>`.
  Unit tests: `TestDOMParserLeavesOutImpliedElements` and
  `TestDocumentChildrenAreUnrestricted`.
- **Rationale:**
  - **The shape is adopted.** The documented claim ("html, head and body exist
    even for application/xml") is wrong on PageLove, and this is also how
    PageLove stores documents (implied elements omitted, LO-1). Code written
    for PageLove reads a parsed fragment's nodes as the document's children,
    and code that expects `d.body` crashes there, so matching PageLove is the
    compatible choice.
  - **The tree construction is kept.** The self-closing, `p` nesting and
    `tbody` differences are kept standard: pagelike's single HTML parser (used
    for stored documents and responses) is the WHATWG one. Adopting PageLove's
    tree building would give server code trees that differ from what every
    browser builds from the same markup, so isomorphic code would break. It
    would also need a second parser. Well-formed markup (explicit end tags, a
    `tbody`, no `<x/>` on non-void elements) gives the same tree in both.
    Measured by `javascript.dom.probe-0929.kept-divergences`.

### javascript.dom.namespace-pipe-selectors

- **Class:** adopt-live.
- **Observations:**
  - `live-2026-09-29/javascript-dom-namespace-pipe-selectors.json` (`*|p`
    matched 1);
  - `exploratory/javascript-dom-probe-0929-namespace-pipe.json`;
  - `live-2026-09-29-reconcile-javascript/javascript-dom-namespace-pipe-selectors.json`.
- **PageLove:**
  - `svg|g` throws `SyntaxError`.
  - `*|p` matches like `p`, `*|*` like `*`, and `[*|id]` like `[id]`.
  - `|p` matches `p` elements without a namespace. That covers every
    HTML-parsed element, including `svg > g` (`|g` is 1), but not an element
    that `createElementNS` made with a namespace: `|circle` is 0 and `|*` is 6
    of 7.
  - `matches('*|p')` and `closest('*|div')` work.
- **pagelike now:** `rewriteNamespacePipes` (`internal/jsrt/dom.go`) rewrites
  the pipe forms, outside strings, before compiling:
  - `*|x` becomes `x`;
  - `[|a` becomes `[a`;
  - `|x` becomes `x` with a private pseudo-class that matches elements for
    which `namespaceURI` is null.

  Selectors are compiled with the new `selector.CompileOptions` (PageLove's
  options plus that pseudo-class). A named prefix is left alone, so it is still
  a `SyntaxError`, and so is `|=` (dash-match). The case now checks
  `SyntaxError|2|2|7|7|2|1|0|6|1|true|r`. Unit tests:
  `TestNamespacePipeSelectors`, `TestRewriteNamespacePipes` and
  `internal/selector` `TestCompileOptionsPrivatePseudo`.
- **Rationale:** the documented divergence ("`*|tag` parses but matches
  nothing") is not what PageLove does, and PageLove's answer is the CSS
  standard. The change is local to the JavaScript DOM: HTTP `Range` selectors
  were not probed, and are unchanged.

### javascript.dom.closest-and-matches

- **Class:** harness artifact. The real difference it exposed, node identity,
  is keep-standard.
- **Observations:**
  - `live-2026-09-29/javascript-dom-closest-and-matches.json`;
  - `exploratory/javascript-dom-probe-0929-closest-identi.json`;
  - `live-2026-09-29-reconcile-javascript/javascript-dom-closest-and-matches.json`
    and `javascript-dom-node-identity-live.json`.
- **PageLove:**
  - `closest` is inclusive: `b.closest('button').id` is `b`, and
    `closest('#b')` returns the button.
  - Every node access returns a **new object**, so the following are all false:
    - `b.closest('button') === b`;
    - `d.querySelector('#b') === b`;
    - `d.getElementById('b') === b`;
    - `b.parentNode.firstChild === b`;
    - `b.ownerDocument === d`;
    - `el.classList === el.classList`.

    Also, `new Set([b, d.querySelector('#b')])` has size 2. A method returns
    the object it was given (`appendChild(n) === n`).
- **pagelike now:** closest is inclusive, and node identity is kept: one node is
  one object, because the wrapper cache is keyed by handle. The case reads
  inclusiveness as `b.closest('button').id`, and passes live. The identity
  difference has its own pair:
  - `javascript.dom.node-identity` (pagelike, standard);
  - `javascript.dom.node-identity.live` (`status: live-divergence`, passed live).
- **Rationale:**
  - **The case was an artifact.** It meant to test that `closest` is inclusive,
    and it checked that through object identity. PageLove's `closest` is
    inclusive, so the case is fixed to test only that.
  - **Node identity is kept standard.** A node being one object is a DOM
    standard that DOM code relies on everywhere: `===` against a stored
    reference, event-delegation style `closest(...) === el`, and nodes as `Set`
    or `WeakMap` keys. Adopting fresh objects would make those comparisons
    answer false for the same node (plainly incorrect data). It would also
    break code written for browsers, which is the point of an isomorphic server
    DOM.

### javascript.dom.tree-operations

- **Class:** harness artifact. The residual identity difference is
  keep-standard, as for `closest-and-matches`.
- **Observations:**
  - `live-2026-09-29/javascript-dom-tree-operations.json`: 500, "cannot read
    property 'tagName' of null";
  - `exploratory/javascript-dom-tree-operations.json` and
    `exploratory/javascript-dom-probe-0929-tree-operation.json`: after the first
    rework, everything matched except `l.ownerDocument === d` (false);
  - `live-2026-09-29-reconcile-javascript/javascript-dom-tree-operations.json`
    (pass).
- **PageLove:**
  - The operations (`insertBefore`, `removeChild`, `replaceChild`, fragments,
    `cloneNode`, `contains`, `textContent`, and the element traversal) give the
    documented results.
  - The original case failed only because `l.parentElement` of a top-level
    parsed element is `null` (DOMParser shape, above).
  - The reworked case then differed only on `l.ownerDocument === d` (node
    identity).
- **pagelike now:** unchanged behaviour. The case now wraps its markup in
  `div#root` (expecting `DIV`), and reads `l.ownerDocument.nodeType` (9)
  instead of the identity comparison. It passes locally and live.
- **Rationale:** the case tests tree mutation. Its DOMParser dependence and its
  identity comparison were incidental, and each is covered by its own case
  (`domparser-ignores-type`, `node-identity`).

### javascript.dom.inner-outer-adjacent-html

- **Class:** harness artifact.
- **Observations:**
  - `live-2026-09-29/javascript-dom-inner-outer-adjacent-html.json`: 500,
    "cannot read property 'children' of null";
  - `exploratory/javascript-dom-inner-outer-adjacent-html.json` and
    `exploratory/javascript-dom-probe-0929-inner-outer-st.json`;
  - `live-2026-09-29-reconcile-javascript/javascript-dom-inner-outer-adjacent-html.json`
    and `javascript-dom-probe-0929-document-child.json`.
- **PageLove:** once the markup has its own root, every expectation holds:
  - `innerHTML` get and set, `outerHTML`;
  - the four `insertAdjacentHTML` positions, case-insensitive;
  - `SyntaxError` for a bad position;
  - silent no-ops beside a parentless element;
  - `NoModificationAllowedError` for `outerHTML` on a parentless element.

  The probe also showed that `insertAdjacentHTML('beforebegin'|'afterend')` and
  the `outerHTML` setter work on a top-level node of a parsed document.
- **pagelike now:** the same. Operations on a document's child are allowed (see
  DOMParser above; they were `NoModificationAllowedError` before).
- **Rationale:** the case read `d.body.children` of a parsed fragment, which
  depends on the DOMParser shape, not on the content APIs it tests. It now reads
  `#root`'s children.

## Other findings

| Finding | Class | Where |
|---|---|---|
| A document accepts several elements and text children; `appendChild`, `insertBefore`, `before`, `after`, `replaceWith`, `remove`, `outerHTML` and `insertAdjacentHTML` work on its top-level nodes | adopt-live | probe `javascript.dom.probe-0929.document-children`; `checkInsert` no longer restricts document children, and doctypes stay document-only |
| `DOMTokenList.item('0')` throws `TypeError`; `add('')` and `add('a b')` throw nothing | adopt-live | probe `javascript.dom.probe-0929.classlist-duplicates` |
| `String(classList)` is `[object Object]` | keep-standard | probe `javascript.dom.probe-0929.kept-divergences` |
| DOMParser tree building: `<x/>` closes any element, `p` in `p`, no `tbody` | keep-standard here; **adopted at integration** with LO-15 (pagelike now builds every document from tokens, `serialization.md`) | probe `javascript.dom.probe-0929.kept-divergences` |
| Node identity (a new object per access) | keep-standard | `javascript.dom.node-identity`, `.live` |

## Open questions

- **Long method results.** A method returning a string of about 700
  characters, built as `'R:' + parts.join(...)`, failed on PageLove with 500
  "Composition failed: internal error: JS method 'probe' failed: return-type
  error in javascript/module binding: JavaScript value type cannot be
  marshalled back into dombase Value". This is
  `exploratory/javascript-dom-probe-0929-domparser-tree.json`, run 2. Results
  of up to 469 characters worked.
  - A likely cause is a concatenated (rope) string at or above 512 characters,
    which PageLove's marshaller does not flatten.
  - It was not probed further: it is not one of the listed cases, and it would
    be a keep-standard crash on valid input.
  - The probes now build their result with `Array.prototype.join`, and stay
    under 500 characters.
  - A dedicated probe should compare `'x'.repeat(300) + 'y'.repeat(300)` with
    `['x'.repeat(300), 'y'.repeat(300)].join('')`.
- **HTTP selectors with namespace pipes.** Only the JavaScript DOM was
  reconciled. Whether a `Range: selector=*|main` read works on PageLove is for
  the reading-writing area.
- **PageLove's parser outside DOMParser.** Stored documents and HTTP reads may
  use the same non-WHATWG tree builder (for example, `<my-el/>` in a stored
  page). That belongs to the composing and reading-writing areas.

## Files changed

- `internal/jsrt/dom.go`:
  - DOMParser leaves out implied elements (`unwrapImplied`);
  - a document accepts any children;
  - `outerHTML` and `insertAdjacentHTML` work beside a document's child;
  - namespace-pipe selectors (`rewriteNamespacePipes`, the private
    `-pagelike-no-namespace` pseudo-class).
- `internal/jsrt/prelude_dom.js`: `DOMTokenList` list semantics.
- `internal/selector/compile.go`: adds `CompileOptions`, a three-line exported
  wrapper; no behaviour change.
- Tests:
  - new `internal/jsrt/dom_live0929_test.go` and
    `internal/selector/compileoptions_test.go`;
  - existing tests that read `d.body` of a parsed fragment now parse an
    explicit `<body>` or read the document's children (`dom_test.go`,
    `contract_test.go`, `limits_test.go`, `internal/jsglue/jsglue_test.go`);
  - `conformance_test.go` skips `live-divergence` cases and also replays
    `dom-probes-0929.yaml`.
- Cases:
  - `harness/cases/javascript/dom.yaml`: five listed cases updated, plus
    `javascript.dom.node-identity` and its `.live` sibling;
  - new `harness/cases/javascript/dom-probes-0929.yaml` (4 probes).
- `docs/spec/javascript.md`: "live 2026-09-29" notes in R-JS-4, R-JS-64,
  R-JS-66, R-JS-69, R-JS-70, R-JS-73, R-JS-74 and R-JS-105.
