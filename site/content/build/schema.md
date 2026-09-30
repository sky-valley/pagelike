---
title: Schemas
description: Declare the shape of an li element so writes are validated server-side.
---

# Tutorial — Schemas

Schemas in pagelike are documents about elements. They declare the
properties an item carries, the data types, the cardinality, and the
shape constraints a write must satisfy to be accepted. They're
documents too — they live in `schemas.html`, and they're written by
the same tools and same `AuthorizationRule` rules as the data they
constrain.

For the full model, see [`/spec/modeling/`](/spec/modeling/).

## A schema for guestbook entries

Schemas are registered items; each SchemaItem carries its own URL
and the URL of its schema document.

```html
<table>
  <tr itemscope itemtype="https://pagelove.org/SchemaItem">
    <td itemprop="name">GuestbookEntry</td>
    <td itemprop="schema">/schemas.html</td>
  </tr>
</table>
```

Then declare the property. Each property carries a `name`, a
`type`, and cardinality:

```html
<table>
  <tr itemscope itemtype="https://pagelove.org/Property">
    <td itemprop="name">signed_by</td>
    <td itemprop="type">https://pagelove.org/types/Text</td>
    <td itemprop="cardinality">required single</td>
  </tr>
  <tr itemscope itemtype="https://pagelove.org/Property">
    <td itemprop="name">message</td>
    <td itemprop="type">https://pagelove.org/types/Text</td>
    <td itemprop="cardinality">required single</td>
    <td itemprop="maxLength">500</td>
  </tr>
  <tr itemscope itemtype="https://pagelove.org/Property">
    <td itemprop="name">added_at</td>
    <td itemprop="type">https://pagelove.org/types/DateTime</td>
    <td itemprop="cardinality">computed single</td>
    <td itemprop="default">${request.now}</td>
  </tr>
</table>
```

`schemas.html` declared once. New `<li>` write attempts that lack a
`signed_by` slot or whose `message` is longer than 500 characters
are rejected with 422 and a PageLove error document.

## Transitions

States and transitions live in the same `schemas.html`. A board's
column rules are a `TransitionConstraint`:

```html
<table>
  <tr itemscope itemtype="https://pagelove.org/TransitionConstraint">
    <td itemprop="itemtype">BoardCard</td>
    <td itemprop="from">todo</td>
    <td itemprop="to">doing</td>
    <td itemprop="allow">${request.auth.username}</td>
  </tr>
</table>
```

Cards moving `todo` → `doing` succeed; every other transition is
rejected. The example in
[`/examples/board/`](/examples/board/) uses this for a Kanban-style
board and exposes a 422 error document on illegal moves.

## Closed shape constraints

A `ClosedShape` says "elements matching this selector may not have
children outside the named properties". This is what stops an
attacker from injecting `<script>` into a `<li>` on a public site.

```html
<table>
  <tr itemscope itemtype="https://pagelove.org/ClosedShape">
    <td itemprop="selector">[itemtype$=GuestbookEntry]</td>
    <td itemprop="allow">signed_by message added_at</td>
    <td itemprop="itemtype">GuestbookEntry</td>
  </tr>
</table>
```

When this rule is present, writes that introduce children outside
the named set are rejected, even if the schema check would have
passed. The Sky example leans on this for its media submission
shape.

## What to read next

- [`/spec/modeling/`](/spec/modeling/) — properties, types,
  cardinality, transitions, closed shapes, references and cascades.
- [`/examples/board/`](/examples/board/) — a runnable Kanban-style
  board with `TransitionConstraint`.
- [`/examples/sky/`](/examples/sky/) — media submissions gated by
  `ClosedShape`.
- [`/security/`](/security/) — the differences pagelike makes on
  top of PageLove.
