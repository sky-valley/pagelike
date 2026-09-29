# pagelike — implementation plan

pagelike is a self-hostable, open-source runtime that reproduces the externally
observable behavior of PageLove: HTML documents as the data model, CSS selectors
as the addressing/query vocabulary, HTTP as the protocol. This file is the
architecture reference; `docs/compat/` holds the compatibility matrix and
report.

## Principles

- Reproduce behavior with the simplest internals that work: one process, one
  SQLite database per site, a per-site write mutex, reparse HTML when needed.
- Authoritative state lives in SQLite. HTML files are an explicit export/import
  format (`pagelike export` / `pagelike import`), never a second live copy.
- Every behavior is traceable to evidence (docs, client source, live
  observation) and pinned by a harness case.
- Unsupported operations fail clearly (501 with a pagelike Error item), never
  silently succeed.

## Process layout

```
pagelike serve --data ./data --listen 127.0.0.1:8787 --domain localhost
```

Routing is by `Host`:

| Host                           | Plane                                           |
|--------------------------------|-------------------------------------------------|
| `<site>.<domain>`              | public application plane (PageLove `dombase-http`) |
| `dav-<site>.<domain>`          | authoring plane: WebDAV + authoring QUERY (`dombase-webdav`) |
| `console.<domain>`             | pagelike control plane (sites, keys, users)     |
| extra aliases per site config  | public plane                                    |

`*.localhost` resolves to loopback in browsers and curl, so no DNS setup is
needed for development. Production uses a wildcard DNS record + TLS proxy.

## Data directory

```
data/
  control.db                 sites registry, authoring keys (hashed), audit
  sites/<site>/site.db       documents, events, sessions, users, transients
  sites/<site>/blobs/<sha>   opaque upload bodies (content addressed)
```

`site.db` tables (WAL, synchronous=FULL):

- `documents(path PK, content_type, body, blob_sha, version, etag, modified_ms)`
- `events(seq PK AUTOINCREMENT, ts_ms, path, event, payload, origin_session, origin_conn)`
- `users(sub PK, email, email_verified, name, password_hash, roles)`
- `sessions(id PK, sub, created_ms, expires_ms)`
- `transients(session_id, path, key, html)`
- `outbox(id PK, ...)` for outbound HTTP with retry state

## Write path (crash-safe commit)

1. Acquire the site write mutex (coarse, per site).
2. Load the affected documents at their current versions.
3. Apply the mutation to parsed DOMs in memory (x/net/html).
4. Run triggers → validation (schemas, shapes, transitions, uniqueness) →
   authorization re-check → processors.
5. One SQLite transaction writes every changed document (version+1, new
   ETag), every derived event, and every outbox entry. Commit.
6. Release mutex; publish committed events to the in-memory SSE broker.

Blob bodies are written to `blobs/<sha256>` (temp file, fsync, rename)
*before* the transaction that references them; unreferenced blobs are swept
at startup. Because document rows and their events commit in the same SQLite
transaction, state and replayable events always agree after a crash.

## Read path

Reads take no lock: SQLite WAL gives each request a consistent snapshot.
Composition (includes, bindings, templates, stamps, routes, transients)
runs per request against that snapshot. Host-wide "system items"
(AuthorizationRule, Group, Schema, ShapeConstraint, Trigger, Processor,
TransitionConstraint/Handler, …) are extracted into a per-site cache keyed
by the site's write generation.

## Packages

| package                | responsibility |
|------------------------|----------------|
| `cmd/pagelike`         | CLI: serve, site, user, key, export/import/fork, backup |
| `internal/store`       | SQLite persistence, blobs, events, generations |
| `internal/dom`         | parse/serialize, fragment parsing in context, stable selectors |
| `internal/selector`    | CSS selectors (fork of cascadia) + PageLove extensions |
| `internal/microdata`   | microdata extraction, values, JSON-LD |
| `internal/httpapi`     | public plane handlers: GET/HEAD/PUT/POST/DELETE/MOVE/OPTIONS/QUERY |
| `internal/sse`         | subscriptions, replay, resets, echo suppression, keepalive |
| `internal/authz`       | AuthorizationRule/Group evaluation, default-GET mode |
| `internal/identity`    | local accounts, sessions, OIDC RP, authoring keys |
| `internal/schema`      | Schema/Property/Types/Resolvers/Shape/Group constraints |
| `internal/compose`     | includes, bindings, stamps, methods, routes, pagination, transients, XML |
| `internal/liquid`      | Liquid templates (osteele/liquid + PageLove filters/drops) |
| `internal/sessel`      | Sessel lexer/parser/interpreter |
| `internal/jsrt`        | bounded server JavaScript runtime + DOM bindings |
| `internal/reactions`   | Trigger, Processor, HTTPRequest, TransitionConstraint/Handler |
| `internal/webdav`      | authoring plane (x/net/webdav FileSystem over store) |
| `internal/site`        | site lifecycle, config, fork/export/import |
| `harness`              | differential compatibility harness (Go runner + YAML cases) |

## Stages

1. **Vertical slice**: store, GET/HEAD/PUT/POST/DELETE with selectors,
   ETags/conditional requests, SSE with replay, local identity + rules,
   WebDAV PUT/GET/PROPFIND. Run beta-js primitives + polls app against it.
2. **Protocol breadth**: OPTIONS (flat/207), QUERY (css/sessel, both planes),
   MOVE, multipart + JSON-LD negotiation, uploads, directory index/redirect,
   resource creation, error documents, size caps, reserved namespace.
3. **Rules & identity**: full authorization model, groups, OIDC, keys.
4. **Modeling**: schemas, properties, types, defaults, uniqueness, keys,
   references/cascades, shapes, resolvers, methods, required combinations.
5. **Composition & languages**: Liquid, Sessel, server JS, includes,
   bindings, stamps, routes, pagination, transients, XML documents.
6. **Reactions**: triggers, processors, outbound HTTP, transitions.
7. **pagelike extensions** (namespaced `/-pagelike/`): fork,
   export/import, participation views, remix lineage.
8. **Evidence**: live differential runs, migration demo, report.

## Security boundaries

- Sites are isolated databases; bindings/queries can only see their own site.
- Authoring keys never reach browsers; the public plane never accepts them as
  end-user identity unless a site explicitly maps a key to a principal.
- Server JS runs in a bounded sandbox with no ambient host access.
- A development auth bypass exists only behind `--dev-insecure-auth`, refuses
  to start on non-loopback listeners, and logs a warning on every request.
