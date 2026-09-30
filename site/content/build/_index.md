---
title: Build for pagelike
description: Tutorials for running a pagelike server and building pages that use its most distinctive features.
---

# Build for pagelike

A series of small tutorials. Each one starts from a fresh
`pagelike serve`, ends with a page on the public plane that uses one
of pagelike's distinctive features, and is grounded in a runnable
example under [`/examples/<name>/`](/examples/).

| Tutorial | What it teaches | Runnable example |
|---|---|---|
| [Run a server](/build/run-server/) | `pagelike serve`, `site create`, `key create`, WebDAV authoring | n/a |
| [Guestbook](/build/guestbook/) | range reads, range writes, the simplest microdata app | [`/examples/sky/`](/examples/sky/) |
| [Permissions](/build/permissions/) | `AuthorizationRule`, deny-wins, selector-scoped rules | [`/examples/sky/`](/examples/sky/) |
| [Schemas](/build/schema/) | modelled data: properties, types, constraints, transitions | [`/examples/board/`](/examples/board/) |
| [Liquid templates](/build/liquid/) | bindings, includes, stamps, server-rendered HTML | [`/examples/board/`](/examples/board/) |
| [Live updates](/build/sse/) | SSE subscribe, replay with `Last-Event-ID`, reset semantics | [`/examples/poll/`](/examples/poll/) |
| [Remember participants](/build/managed-identity/) | managed hosting, quiet identity restoration across devices | browser hosting tests |
| [Migrate from PageLove](/build/migrate/) | moving a PageLove host to pagelike byte-for-byte | n/a |

Each tutorial starts with the assumption that you can install the
binary. If you can't, start at [`/architecture/`](/architecture/) for
the high-level model and then `/hosting/` for installation.

## What makes pagelike different

- **Range reads / range writes.** A `Range: selector=<css>` header on
  `GET` returns a single element of the stored document. On `POST`, the
  request body replaces or extends that element.
- **Documents are the database.** No SQL, no `INSERT` statements. Books
  of `AuthorizationRule` items, schema declarations, and Liquid
  templates are inside the documents themselves.
- **One trip, many reads.** Clients subscribe with `Accept:
  text/event-stream`; every acknowledged write pushes an event.
- **Microdata-only metadata.** Permissions, schemas and properties are
  declared as `itemscope itemtype="https://pagelove.org/AuthorizationRule"`
  items inside the documents.
- **The model is documented to the byte.** [`/spec/`](/spec/) lists the
  behaviour one `R-XXX-N` ID at a time, with a test under
  `harness/cases/<area>/` and evidence in `docs/compat/live-observations.md`.
