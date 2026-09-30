---
title: Run a server
description: Stand up pagelike on localhost, create a site, mint an authoring key, and upload your first document.
---

# Run a server

This tutorial stands up a server on `127.0.0.1:8787`, creates a site
called `demo`, mints an authoring key, and uploads the simplest
possible document. After it, you should have `http://demo.localhost:8787/`
serving on the public plane and `http://dav-demo.localhost:8787/`
on the authoring plane.

## 1. Install the binary

Either build from source:

```sh
go install github.com/sky-valley/pagelike/cmd/pagelike@latest
```

Or download a Release from github.com/sky-valley/pagelike/releases
(Linux, macOS, Windows).

## 2. Create a data directory and start the server

```sh
mkdir -p ./data
pagelike serve --data ./data
```

The default domain is `localhost`, so `*.localhost` resolves through
the wildcard DNS that points to loopback. You don't need to edit
`/etc/hosts`.

## 3. Create a site

In another terminal:

```sh
pagelike site create demo --data ./data --default-get allow
```

`--default-get allow` means anyone can `GET` documents on the public
plane without an account. Use `deny` for sites where reads are gated
behind identity; you'll then write
`AuthorizationRule` items to open them up.

## 4. Mint an authoring key

```sh
KEY=$(pagelike key create --site demo --label "tutorial" --data ./data 2>/dev/null)
```

The key is printed once and is not recoverable. Keep `$KEY` for
this terminal session; in production, store it in your secret manager.

## 5. Upload your first document

`http://dav-demo.localhost:8787/` is the authoring plane. Documents
are uploaded with `PUT` and a `Bearer` token:

```sh
curl -X PUT http://dav-demo.localhost:8787/index.html \
  -H "Authorization: Bearer $KEY" \
  --data-binary '<h1 id="hello">Hello, pagelike</h1>'
```

## 6. Read it back

Public plane, full document:

```sh
curl http://demo.localhost:8787/index.html
```

Public plane, selector-scoped:

```sh
curl http://demo.localhost:8787/index.html -H 'Range: selector=#hello'
```

Authoring plane, with the key (writes allowed):

```sh
curl http://dav-demo.localhost:8787/index.html -H "Authorization: Bearer $KEY"
```

## 7. Watch it change

Subscribe to the SSE stream and keep the connection open. Updates
echo out as `id: N\ndata: …\n\n`:

```sh
curl -N http://demo.localhost:8787/index.html \
     -H 'Accept: text/event-stream'
```

In another terminal, write to a selector:

```sh
curl -X PUT http://dav-demo.localhost:8787/index.html \
  -H "Authorization: Bearer $KEY" \
  -H 'Range: selector=#hello' \
  --data-binary '<h1 id="hello">Edited at '"$(date)"'</h1>'
```

The SSE stream carries the change event when the write is
acknowledged.

## What to read next

- [`/build/guestbook/`](/build/guestbook/) — a small app that uses
  selector ranges to append to a list.
- [`/build/permissions/`](/build/permissions/) — declare who may do
  what with `AuthorizationRule`.
- [`/commands/`](/commands/) — full CLI surface.
- [`/spec/reading-writing/`](/spec/reading-writing/) — the wire-level
  specification for the operations you used above.
