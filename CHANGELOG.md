# Changelog

## v0.1.0 — 2026-09-29

First public release.

- **Runtime.** A single Go binary and one data directory. Each site is one
  SQLite database (WAL, `synchronous=FULL`) plus content-addressed blobs.
  Writes and their change events commit together.
- **PageLove-compatible protocol.**
  - Selector-addressed GET/HEAD/PUT/POST/DELETE/MOVE, placements, conditional
    requests with fragment ETags.
  - OPTIONS, `QUERY` in css and Sessel modes.
  - A WebDAV authoring plane.
  - Server-sent events with replay, reset and echo suppression.
- **Document model as PageLove has it.** Trees are built from tokens, and
  every HTML write is stored in one serialized form (LO-15).
- **Declarative features.**
  - Authorization rules, local accounts and OpenID Connect.
  - Schemas, validators, uniqueness, references, shape constraints,
    transitions.
  - Includes, bindings, method elements, pagination, transient elements.
  - Liquid (autoescaped), Sessel, and sandboxed server JavaScript
    (QuickJS worker processes).
  - Triggers, processors and outbound HTTP.
- **pagelike extensions** (`/-pagelike/`):
  - experience versions;
  - participation records and shareable participation views;
  - `fork` (remix);
  - `export`/`import`, `backup`;
  - `migrate` from a PageLove host;
  - `events prune`.
- **Compatibility evidence.**
  - A differential harness of 1,426 cases. Locally, 1,358 pass and 0 fail.
  - On live PageLove, 603 cases ran: 578 match, 25 differ on purpose, 0
    unexplained.
  - See docs/compat/report.md.
- **Known limits.**
  - Windows builds are experimental: server-JavaScript workers get the
    engine's limits and deadlines, but no OS-level memory caps.
  - Every request parses the whole document, and writes to a site are
    serialized.
