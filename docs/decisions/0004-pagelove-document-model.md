# 0004: PageLove's document model — trees from tokens, one stored form

- **Status:** accepted
- **Date:** 2026-09-29
- **Supersedes:** the tree-construction and source-preservation parts of
  [0003](0003-html-and-selectors.md). The selector engine, `htmlser` and
  XML handling from 0003 remain.
- **Evidence:** LO-15 in `docs/compat/live-observations.md`. The probes are
  in `harness/cases/protocol/serialize-probe.yaml`, with observations in
  `harness/observations/live-2026-09-29-serialize/`.
- **Affects:** `internal/dom` (`BuildTree`), `internal/htmlser`
  (`Options.PageLove`), `internal/engine` and `internal/schema` (what gets
  stored), `internal/compose` and `internal/liquid` (template source and
  output), `internal/selector` (`:root`).

## Context

0003 assumed that PageLove parses with an HTML5 tree builder (html5ever)
and serves the bytes it stored. Two live findings contradicted that:
- **Writes re-serialize.** A WebDAV PUT of irregular markup is stored and
  echoed normalized (LO-9).
- **No HTML5 tree construction.** Targeted probes showed:
  - no implied `html`/`head`/`body`/`tbody`;
  - `<p>a<div>` nests;
  - no foster parenting;
  - `:root` is the first top-level element;
  - `table > tr` matches `<table><tr>`.

Which selectors match hand-written markup is protocol behaviour, so this
decides compatibility for real apps.

## Decision

1. **Build trees straight from tokens** (`dom.BuildTree`, over x/net/html's
   tokenizer).
   - An end tag closes the nearest open element with its name; a stray end
     tag is dropped.
   - `<x/>` is empty for every element.
   - Void elements never have content.
   - script, style and xmp are raw text; textarea and title are escapable
     text; noscript holds markup.
   - Only complete character references are decoded, and unquoted
     attribute values are kept as written.
   - `<?…>` and `</ …>` are text.
2. **Store every HTML write serialized in one form**, the same form served
   in responses.
   - Bare empty attributes.
   - Text escapes `& < >`, and U+00A0 is written literally.
   - Attribute values escape `& "`, plus `<` and `>` (see 0006).
   - The form is a fixed point of parse and serialize.
3. **Liquid reads the stored content** and follows PageLove:
   - an output or tag containing markup is unterminated (500);
   - references inside `{{ }}` and `{% %}` are decoded;
   - outputs are auto-escaped unless safe;
   - `escape` is not idempotent;
   - the output is re-serialized.
4. **A prefixed element with no namespace binding is an ordinary element.**

## Consequences

- The 24 live stored forms replay byte for byte, apart from the attribute
  escaping in 0006 (`TestStoredFormMatchesLive`).
- Parsing became 2 to 4 times faster, so a 1.1 MB board reads in about
  13 ms.
- Source splicing (`dom.Splice`, `schema/splice.go`) is no longer on any
  HTML write path; removing it is a cleanup issue.
- Documents written by earlier pagelike versions keep their bytes until
  their next write.
- Not probed, kept from the HTML5 tokenizer: CRLF handling, raw text in
  iframe/noembed/noframes/plaintext, and `<script/>`.
