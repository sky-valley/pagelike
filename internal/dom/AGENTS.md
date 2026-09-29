# internal/dom/AGENTS.md

This package implements **PageLove's document model** (docs/decisions/0004,
LO-15 in docs/compat/live-observations.md). Every HTML document the runtime
touches is parsed and serialized here. Getting this wrong changes which
selectors match, which is to say what the protocol does.

## The model

- **`BuildTree`:** a tree built straight from x/net/html's tokens.
  - There's **no HTML5 tree construction**: no implied `html`/`head`/`body`/
    `tbody`, no implied end tags, no foster parenting, no adoption agency.
  - An end tag closes the nearest open element with its name, and a stray end
    tag is dropped.
  - `<x/>` is empty for every element.
  - `:root` is the first top-level element.
  - Only complete character references are decoded. Unquoted attribute values
    are kept as written.
  - `<?…>` and `</ …>` are text.
- **Serialization** is one form for storage and responses
  (`htmlser.Options{PageLove: true}` via `dom.Render`/`OuterHTML`):
  - bare empty attributes;
  - text escapes `& < >`, and U+00A0 is written literally;
  - attribute values escape `& " < >`.

  Escaping `<` and `>` in attributes is **our one deliberate difference**:
  it closes a noscript mutation-XSS (docs/decisions/0006).
- **The stored form is a fixed point:** `Render(Parse(Render(x))) ==
  Render(x)`. `TestStoredFormMatchesLive` replays live PageLove's stored
  forms, byte for byte, apart from that one difference.
- **Spans are exact** (every element has a start tag). Composition reads
  Liquid template sources through them.

## Rules

- Runtime code never calls `html.Parse`, `html.ParseFragment` or
  `html.Render`. `FixComments`, `FixAttrOrder` and the `htmlser` default
  (html5ever-compatible) mode survive only for htmlser's own tests.
- XML-family documents go through `xmldom`; the helpers here dispatch on
  `xmldom.IsXML`.
- Any change to parsing or serialization needs:
  - a live-observed case or a replayed live form;
  - a run of the whole harness, because template sources, microdata,
    authorization rule discovery and SSE payloads all go through here;
  - the e2e suites.
