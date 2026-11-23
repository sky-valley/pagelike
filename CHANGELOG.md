# Changelog

## Unreleased

Server-sent events (docs snapshot 2026-11-23):

- Every mutation event names who made the change (`actor`: kind and
  sign-in id, never an email or a name).
- A page's stream carries the events of the documents its composition
  reads, through nested includes, stamps and bindings, and replays them
  after `Last-Event-ID`. A source's events need a rule granting its read;
  default GET is not enough.
- A write through a composed page is announced as the origin document's
  event.
- Writes made by triggers, processors and methods are streamed, naming the
  requester.

Liquid (docs snapshot 2026-11-23):

- `request.body` is parsed by Content-Type: form fields (last value wins),
  parsed JSON, or the raw text; an empty body is `{}`.
- New `request.rawBody`, `request.host` and `request.queryString`;
  `request.authentication` and `request.authn` alias `request.auth`.

MOVE:

- An element can be moved to another page of the site in one step. The
  source stream gets a DELETE and the destination stream a POST, with a
  shared `correlation`.
- An element MOVE answers with the moved element's ETag.

Authorization hardening PageLove documented after the port (docs snapshot
2026-11-23):

- A value substituted into a rule's selector is literal text and can no longer
  add selectors or break out of a quoted string.
- A Deny rule missing its actor, resource or method refuses broadly instead of
  being dropped.
- An element Deny narrows a page-wide Allow: selector writes to the protected
  element, an element around it or inside it are refused, and whole-page PUT,
  MOVE and POST must leave protected elements exactly as they were.
- A failed group lookup (`includes` erroring or over budget) refuses the
  request with 502 instead of treating the person as in no group.

Reading and writing:

- Request paths must be canonical (no `//`, `.` segments, backslashes or
  needless percent-escapes); others are 400 InvalidPath before any rule runs.
- A Range header naming more than one `selector=` is 400.
- POST placement is parsed strictly; anything but `; placement=<keyword>` is
  ignored and the body appended.
- A POST whose body is only whitespace is 422.
- A POST with no selector to a page that is not a template saves the body as
  the page instead of failing.
- A GET a trigger or processor ran on is `Cache-Control: private, no-store`
  unless a thrown HTTPResponse opts back in.
- Byte ranges: several ranges come back as `multipart/byteranges`, HEAD
  ignores `Range`, a weak `If-Range` never matches, unsatisfiable ranges get
  an error item, and pages and files advertise `Accept-Ranges: selector,
  bytes`.

QUERY (breaking):

- The public site runs no client-supplied query code. A `text/sessel`,
  `text/javascript`, `multipart/query`, other or untyped QUERY is 405 with
  `Accept-Query: text/css-selector`; a selector QUERY still works. Sessel
  QUERY moved to the authoring plane, which needs the authoring key.
- A wildcard method grant no longer lists PATCH in `Allow`.

## v0.3.0 — 2026-11-20

- Build with Go 1.27.4 and current dependencies. Go 1.26.0 left 38
  standard-library vulnerabilities reachable; govulncheck now finds none.
- Fix server JavaScript with modernc.org/quickjs v0.26 and later, which moved
  the VM's context pointer.
- Optional Sentry error reporting for `serve` and `host` via `SENTRY_DSN`
  (see docs/hosting.md). Managed hosting now logs its own storage and identity
  contract failures, which it previously answered with a bare 503.
- Managed hosting skips a site that was opened but never installed: temporary
  site pruning no longer fails on it every minute, and its reads answer 404,
  not 503. Pruning failures now name the site in the log.

## v0.2.1 — 2026-09-30

- Fix Linux worker memory accounting: a fresh JavaScript worker no longer
  inherits the host's old peak for its own memory limit. The worker's actual
  memory ceiling remains enforced. A subprocess regression test reproduces the
  failure before the fix.
- Includes the managed-identity changes below. The v0.2.0 tag's release job was
  blocked by this existing Linux issue; v0.2.1 is the published release.

## v0.2.0 — 2026-09-30

- Closing a site registry now prevents a late background scan from reopening
  its databases during shutdown or test cleanup.
- Managed hosting separates immutable authored versions from live participation,
  with site-scoped tickets, partitioned private sessions, current authorization,
  contribution management, ordered publication and bounded runtime resources.
- `pagelike.identity()` quietly restores an existing participant through an
  approved parent, including on another device. First-time visitors stay
  anonymous. Restoration adds no contribution and remains usable on identity
  outages; `participate()` remains the deliberate join path.
- Browser coverage includes real cross-device cookie/session behavior, forged
  parent replies, concurrent calls, logout and missing parent responses.
- Published documentation now includes managed-host identity and its parent
  protocol, plus native specification and harness coverage. This is a pagelike
  extension, not a new PageLove compatibility claim.

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
