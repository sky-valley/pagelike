---
title: Guestbook
description: Build a multi-writer guestbook with range reads and range writes.
---

# Tutorial — Guestbook

A guestbook is the canonical first pagelike app. It exercises the
two operations that are most unlike anything a server framework
usually offers: **range reads** (`Range: selector=<css>` on `GET`)
and **range writes** (`Range: selector=<css>` on `POST`, with the
request body inserting at the matched element).

## Start from a clean site

```sh
pagelike serve --data ./data &
pagelike site create demo --data ./data --default-get allow
KEY=$(pagelike key create --site demo --label "guestbook" --data ./data 2>/dev/null)
```

## A document with three sections

`index.html`:

```html
<ul id="entries"></ul>
<form onsubmit="event.preventDefault();
  const li = document.createElement('li');
  li.textContent = this.msg.value;
  fetch('/index.html', {method: 'POST', headers: {'Range': 'selector=#entries'},
                        body: li.outerHTML})">
  <input name="msg"><button>Sign</button>
</form>
```

Upload:

```sh
curl -X PUT http://dav-demo.localhost:8787/index.html \
  -H "Authorization: Bearer $KEY" \
  --data-binary @index.html
```

## Read it back, in three ways

```sh
curl http://demo.localhost:8787/index.html                       # the whole document
curl http://demo.localhost:8787/index.html -H 'Range: selector=#entries'   # just the <ul>
curl http://demo.localhost:8787/index.html -H 'Range: html'                # the rendered HTML fragment for #entries
```

The third form is what an HTML-aware client uses to drop the
selector's contents into the page without re-parsing.

## Write — three useful shapes

Append (the default placement for `<li>` and most lists):

```sh
curl -X POST http://dav-demo.localhost:8787/index.html \
  -H "Authorization: Bearer $KEY" \
  -H 'Range: selector=#entries' \
  --data-binary '<li>Hello from curl</li>'
```

Replace the matched node:

```sh
curl -X PUT http://dav-demo.localhost:8787/index.html \
  -H "Authorization: Bearer $KEY" \
  -H 'Range: selector=#entries' \
  --data-binary '<ul id="entries"><li>Reset</li></ul>'
```

Prepend (using `before` placement):

```sh
curl -X POST http://dav-demo.localhost:8787/index.html \
  -H "Authorization: Bearer $KEY" \
  -H 'Range: selector=#entries; placement=before' \
  --data-binary '<li>Pinned note</li>'
```

The full placement vocabulary is documented at
[`/spec/reading-writing/R-RW-7/`](/spec/reading-writing/R-RW-7/).
Quick reference:

| Placement | Where the request body lands |
|---|---|
| `append` (default) | last child of each matched element |
| `append-child` | last child of each matched element (alias) |
| `before` | before each matched element |
| `after` | after each matched element |
| `prepend` | first child of each matched element |
| `replace` | replaces each matched element |
| `replace-children` | replaces the children of each matched element |

## What you have so far

Anyone can read the document; anyone can post signatures. There's
no spam filter, no auth, no rate limit. That's fine for a demo and
not fine for production. The next tutorial, [`/build/permissions/`](/build/permissions/),
shows how to declare who may do what with microdata.

## What to read next

- [`/build/permissions/`](/build/permissions/) — add `AuthorizationRule`.
- [`/build/schema/`](/build/schema/) — declare the shape of an `<li>`.
- [`/spec/reading-writing/R-RW-7/`](/spec/reading-writing/R-RW-7/) —
  placements, in full.
- [`/examples/sky/`](/examples/sky/) — a runnable guestbook-style
  example with per-user folders, ownership, and live share views.
