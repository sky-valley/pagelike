---
title: Liquid templates
description: Render server-side, pagelike-style: bindings, includes, and stamps.
---

# Tutorial — Liquid templates

Pagelike renders Liquid templates server-side when an element
carries a `*:template` attribute whose value is `text/liquid` (or
another registered MIME). The full PageLove dialect is supported;
the engine is in `internal/liquid` and the spec is at
[`/spec/liquid/`](/spec/liquid/).

This is a quick orientation, not a substitute for the spec.

## The minimal template

Pick a host element, declare the `pagelove` (or `p:`) namespace,
and put the template source on a `pagelove:template` attribute:

```html
<ul xmlns:p="https://pagelove.org/1.0">
  <li><span p:template="text/liquid">{{ entry.signed_by }}</span>: {{ entry.message }}</li>
</ul>
```

When the host is rendered, the children of `<li>` in the rendered
output are the Liquid expansion. The original `<span>` and its
template attribute disappear.

## Bindings

`r:` binds a server-composed JSON-LD document. `e:` binds the
element value. `j:` binds a Sessel expression result.

```html
<ul xmlns:p="https://pagelove.org/1.0">
  <li p:bind-r="entries">
    {{ entry.signed_by }} — {{ entry.message }}
  </li>
</ul>
```

The page looks up `entries` in composition context (a sibling
`r:`/`e:`/`j:` definition), passes each item through the
`<li>` template, and emits one `<li>` per item.

## Includes and stamps

A `pagelove:include` (or `p:include`) builds a card from another
document:

```html
<article xmlns:p="https://pagelove.org/1.0"
         p:include="/cards/{{ slug }}/index.html">
</article>
```

A stamp (`p:stamp`) embeds the same shape at many places:

```html
<section xmlns:p="https://pagelove.org/1.0"
         p:stamp="parts/{{ slug }}.html">
  <p>fallback if the stamp is missing</p>
</section>
```

The runtime looks up the stamp under that path; on miss, it falls
back to the children of the host element. Both are documented at
[`/spec/composing/`](/spec/composing/).

## Auto-escaping

`{{ value }}` HTML-escapes by default. The result of `{{ "<b>" }}`
is `&lt;b&gt;`, not `<b>`. `{{{ value }}}` is the no-escape form
and should be reserved for trusted sources.

## What to read next

- [`/spec/liquid/`](/spec/liquid/) — full behavioural spec for the
  dialect.
- [`/spec/composing/`](/spec/composing/) — bindings, includes,
  stamps, routes, resource creation, transient elements.
- [`/examples/board/`](/examples/board/) — a Kanban-style board
  where the lanes are bound `r:` to per-column documents.
- [`/examples/poll/`](/examples/poll/) — a poll with Liquid
  composition and a server-side tally.
