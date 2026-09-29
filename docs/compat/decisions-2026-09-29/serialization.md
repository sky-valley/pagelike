# Decisions: HTML model, storage form and Liquid source (2026-09-29)

This follow-up to LO-9 ("stored HTML is re-serialized", deferred on
2026-09-28) became live observation LO-15. Every decision here is
**adopt-live**.

**Evidence.** Observations are in
`harness/observations/live-2026-09-29-serialize/`, from host
`live-test-host`. The live requests were about 80 in total, all at 3 req/s:
- six probe cases (`harness/cases/protocol/serialize-probe.yaml`, prefix
  `protocol.serialize.probe-0929.`);
- one run of 12 existing cases.

| Case(s) | PageLove | pagelike before | Now |
|---|---|---|---|
| `protocol.serialize.probe-0929.dom-shape`, `.edge-rules` | The tree is built from tokens: no implied html/head/body/tbody, no implied end tags, no foster parenting; `:root` is the first top-level element. | HTML5 tree construction (x/net/html) with fidelity fixups. | `dom.BuildTree`: a naive tree builder over the x/net/html tokenizer. Unit test `TestStoredFormMatchesLive` replays the 24 live stored forms. |
| `.dav-put-forms`, `.public-writes`, `protocol.webdav.get-is-byte-exact` | Every HTML write stores and echoes the serialized form; reads serve it. | Kept the uploaded bytes and spliced edits into them (LO-1 reading). | Stores `dom.Render(tree)` (`htmlser.Options.PageLove`) on every HTML write. XML is unchanged. The `.live` sibling is folded into the case. |
| `.liqsrc-in-tags`, `.liqsrc-outside-tags`, `liquid.output.entities-not-decoded` | References inside Liquid delimiters are decoded; markup outside is left as stored. | The raw source reached Liquid (R-LIQ-13). | `liquid.SourceFromHTML`. |
| `liquid.filters-string.escaping-family` (+ new `.markup-in-output-500`) | Markup inside `{{ }}` leaves it unterminated: 500. | Evaluated the output. | An output or tag containing markup is a template error. The docs' filter examples are rewritten with escaped literals. |
| `liquid.escape.autoescape` / `.no-autoescape` / `.escape-idempotent`, `liquid.filters-string.newlines` | Outputs are auto-escaped; `escape` marks its result safe but escapes again. | No autoescape; `escape` was idempotent (spec C-4). | `liquid.Options.AutoEscape` (on in composition). `escape` always escapes. `.no-autoescape` becomes the disputed claim. |
| `liquid.errors.attribute-context-renders-empty`, `liquid.apps.shop-admin-data-attributes`, `modeling.defaults.present-empty-not-defaulted` | Empty attributes are bare in stored and composed output. | `href=""`. | Same serializer everywhere. |
| `comp.ns.unbound-element-500` | An unbound prefixed element is an ordinary element (200). | 500 NoMethod (R-COMP-13 row 1, documented). | An unbound prefix is inert. The case keeps its id. |

**One deliberate difference: `<` and `>` in attribute values.** PageLove
writes them literally (`title="<&amp;>"`). pagelike escapes them
(`title="&lt;&amp;&gt;"`), as the WHATWG serializer does since 2025. The reason
is PageLove's model: it reads `<noscript>` content as markup, but a browser
reads it as raw text. So a participant who can store
`<noscript><img alt="</noscript><img src=x onerror=…>">` would get live
markup when the page is viewed (mutation XSS). Escaping closes that. The only
cost is those bytes in attribute values: values read through the DOM, selectors
or microdata are unchanged. Class: keep-standard, for security.
`TestStoredFormMatchesLive` applies exactly this substitution to the two live
forms it affects; `TestNoscriptAttributeCannotBreakOut` pins it.

**Security review.**
- Autoescape makes template output safer than before.
- In the serialized form, attribute values escape `"`, `<` and `>` (see
  above), so a value can break out neither of its attribute nor of a
  raw-text element in the browser.
- The naive tree does not change authorization. Rules match on the same
  tree that selectors and writes use.

**Consequences and limits.**
- Spans are exact, because every element comes from a start tag.
  Composition still reads template sources through spans. The splicing
  helpers (`dom.Splice`, `schema/splice.go`) are no longer on any HTML
  write path, and the HTML5-model helpers (`FixComments`, `FixAttrOrder`,
  `rewritePrefixed`) only back the html5ever-compatibility tests of
  `htmlser`. Both are candidates for removal.
- Documents stored by earlier pagelike versions keep their bytes until
  their next write, which stores them serialized.
- Not probed, so kept from the HTML5 tokenizer:
  - CRLF handling;
  - iframe, noembed, noframes and plaintext as raw text;
  - `<script/>`;
  - Liquid outputs inside raw-text elements inside a template, which are
    escaped like any other output. Only a raw-text *host* is exempt.

## Integration (merging the five area reconciliations of 2026-09-29)

The five area reconciliations were done in parallel on the HTML5 tree model. They
were merged onto LO-15. After the merge, two of their adopt-live decisions
were reclassified under the decision policy. Both reversals were checked
against the recorded live observations; no new live runs were needed.

| Case(s) | Area decision | Integrated decision | Why |
|---|---|---|---|
| `modeling.write-paths.webdav-put-validated-by-schema`, `modeling.shapes.webdav-write-checked`, `apps.shop.order-shape-dav` | adopt-live: a WebDAV PUT is stored without schema or shape checks | **keep-documented-security**: WebDAV PUTs stay validated; the former case bodies are now `.live` siblings | R-PROTO-112 and R-MOD-65 document the check. The shop's closed Order shape is what keeps a `<script>` out of orders its checkout worker writes over WebDAV (ACC-SH-8). Following live would remove a documented guard against stored script injection. |
| `comp.pag.links-and-slice`, `comp.pag.preserve-other-params`, `comp.pag.multiple-with-ids` | adopt-live: links carry only the paginator's page parameter | **keep-standard** for the other parameters: the link form stays live (path-absolute, `paginate:<id>:page`, order first/last/prev/next, `title`), but the request's other parameters are kept; the adopted case bodies are `.live` siblings, and the four `comp.pag.probe-0929.*` probes are `live-divergence` | Dropping `q=smith` and the page length makes "next" show a different, unfiltered list: plainly incorrect navigation that no PageLove app can depend on. The docs' recipe keeps them (ACC-RC-5). |

| `liquid.filters-string.escaping-family` | (rewritten at LO-15 with escaped literals) | **keep-standard** for one detail: the final live run shows `{{ "&copy; …" \| escape_once }}` rendering `Â©`, UTF-8 read as Latin-1; the xml_escape and strip_html parts match. Sibling `.live` | Double-encoded characters are plainly incorrect output. |

**Final confirmation run** (`harness/observations/live-2026-09-29-final/`, 21
cases): the Liquid cases updated at LO-15 now match; the six kept cases fail
live as designed while their `.live` siblings pass; the disputed
`liquid.escape.no-autoescape` is XFAIL; and `escaping-family` exposed the
`escape_once` encoding bug above.

Other integration consequences:
- Item index paths in uniqueness messages now count every child node, which
  matches live exactly (`0.2.0.1`) now that the tree is built from tokens.
- The 65 s modeling probe `modeling.probe-0929.write-envelopes` is tagged
  `slow`.
- The javascript area's DOMParser rules (no implied elements, top-level
  `outerHTML`, several document children) agree with LO-15.
- `liquid.escape.escape-filter.live` became the normal case
  `liquid.escape.escape-filter-inline-500`.
