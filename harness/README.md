# Differential compatibility harness

A case describes initial site state, identities, an ordered list of steps, and
expected observations. The same case runs against a local pagelike instance
(always) and against real PageLove (optionally, when credentials exist).

```
go run ./harness/cmd/harness run    [--target local|live] [--filter SUBSTR] [--ids ID,ID|@file]
                                    [--slow] [--root] [--parallel N] [--observations DIR] [-v]
go run ./harness/cmd/harness record --target live [--ids …]      # run and store observations
go run ./harness/cmd/harness list   [--filter SUBSTR]              # case ids and titles
```

| Flag | Effect |
|---|---|
| `--filter` | Selects cases whose file path or id contains the substring. |
| `--ids` | Selects exact case ids, as a comma list or `@file` holding one. Unknown ids are an error. Use it for targeted live re-runs. |
| `--slow` | Includes cases that `require: [slow]` (retention windows, trigger binding). |
| `--root` | Opts in to live runs of read-only `root: true` probes. Root cases with setup files never run live. |
| `--observations DIR` | Writes one JSON file per case, with status, headers and body of every response and the first bytes of every stream. `record` defaults to `harness/observations/<target>`. |

Live runs require `.secrets/pagelove.env` (git-ignored, never committed or
printed). It must hold:
- `PAGELOVE_API_KEY`;
- `PAGELOVE_HOST`, the public hostname of a *disposable* test host;
- `PAGELOVE_DAV_URL`;
- `PAGELOVE_DISPOSABLE=yes`.

Live runs refuse to start without them, and rate-limit to 3 req/s. Prefer
`scripts/live-run.sh <label> --ids …`, which also caps a run at 40 cases and
records the observation directory in `harness/observations/ORDER` for
`scripts/matrix.sh`.

## Case files

YAML under `harness/cases/<area>/<topic>.yaml`. A file holds one case or a
list of cases (`cases: [...]`).

```yaml
id: rw.get.selector-first-match      # unique, dotted: <area>.<topic>.<name>
title: GET with a selector returns the first match as 206
area: reading-writing                # matrix section
feature: GET selector range          # matrix row
evidence: documented                 # documented | client-source | demo-source | live-observed | inferred
source: "docs reference/reading-and-writing/GET-method §Examples (snapshot 2026-09-28)"
confidence: high                     # high | medium | low
requires: []                         # capabilities; see below
live: true                           # safe and meaningful to run against PageLove
notes: optional free text, e.g. contradictions between sources
site:
  settings:
    default_get: allow               # allow | deny  (host default-GET mode)
  files:
    - path: ${P}/doc.html            # ${P} = per-case path prefix (see below)
      body: |
        <!DOCTYPE html>
        <html><body><h1>A</h1><h1>B</h1></body></html>
    - path: ${P}/data.json
      content_type: application/json
      body: '{"a":1}'
  rules:                             # shorthand; the runner writes ${P}/_rules.html
    - {actor: "*", resource: "${P}/*", method: [GET, PUT], action: Allow}
    - {actor: alice, resource: "${P}/doc.html", method: [DELETE], selector: "h1", action: Allow}
actors:                              # identities used by `as:`; `anonymous`, `visitor` and `author` are built in
  alice: {sub: alice, email: alice@example.com, email_verified: true, roles: [editors]}
steps:
  - name: read the first heading
    as: anonymous                    # anonymous | visitor | author (authoring key; WebDAV plane unless plane: public) | <actor>
    plane: public                    # public (default; dav for author) | dav
    request:
      method: GET
      path: ${P}/doc.html
      headers: {Range: "selector=h1"}
      body: ""                       # string; or body_file
    expect:
      status: 206                    # int or list of acceptable ints
      headers:                       # exact match after normalization (case-insensitive names)
        Content-Range: "selector h1"
      headers_present: [ETag]
      headers_absent: [Set-Cookie]
      header_matches: {Content-Type: "^text/html"}   # regexp
      body: "<h1>A</h1>"             # exact after trimming
      body_html: "<h1>A</h1>"        # DOM-equivalent (whitespace-insensitive between tags)
      body_contains: ["A"]
      body_not_contains: ["B"]
      body_json: {...}               # JSON-equivalent
      microdata: {...}               # optional: expected extracted items
    capture:                         # values reusable later as ${name}
      etag1: header.ETag
      bare: header.ETag|unquote      # filters: |unquote strips surrounding quotes,
      ver: 'header.ETag|regex:-(\d+)"$'   # |regex:RE keeps the first group (or the match)
      conn: sse.s1.connection        # from an sse stream
  - name: document state after write
    as: author
    plane: dav
    request: {method: GET, path: ${P}/doc.html}
    expect: {status: 200, body_contains: ["<h1>A</h1>"]}

  # SSE steps
  - sse_open: {name: s1, path: ${P}/doc.html, as: anonymous, last_event_id: "${evt}"}
  - sse_expect:
      name: s1
      event: mutation                # mutation | reset | pagelove-connection
      within_ms: 3000
      data_contains: ['itemprop="method">PUT']
      data_microdata: {method: PUT, selector: h1, placement: null}
      capture: {evt: id}
  - sse_expect_none: {name: s1, event: mutation, for_ms: 1500}
  - sse_close: {name: s1}
  - prune_events: true               # local only (requires: [sse-control]): drop retained
                                     # events as if the retention window had passed

  # concurrency: steps inside run simultaneously; expectations checked per step
  - parallel:
      - {as: anonymous, request: {...}, expect: {...}}
      - {as: anonymous, request: {...}, expect: {...}}
  - sleep_ms: 100
```

### Prefix and isolation

`${P}` expands to a unique prefix per case run, e.g. `/_pl/rw-get-3f9a`. Cases
must put every file and every rule `resource` under `${P}` unless they declare
`root: true` (runs exclusively; live runs of root cases are opt-in). Locally
each case runs in a fresh site in a temporary data directory.

### Capabilities (`requires:`)

- `multi-actor` — needs named end-user identities beyond anonymous/author.
  Live targets only provide this when `.secrets/pagelove.env` maps actors to
  session cookies (`PAGELOVE_ACTOR_<NAME>_COOKIE`).
- `outbound-http` — case makes the server call a URL; runner provides
  `${SINK}` (a local capture endpoint; live runs need a public sink).
  Locally `${SINK}` is `http://127.0.0.1:<port>/c-<id>`, unique per case run,
  and the case's site allows that one loopback destination (setting
  `outbound.allow`). Steps (docs/spec/reacting.md §14, `harness/sink.go`):
  `sink_config: {path, responses: [500, 200], delay_ms}` (statuses returned
  to successive requests on a path; the last repeats; default 200),
  `sink_expect: {path, within_ms, method, query, headers, header_values,
  header_matches, headers_absent, body, body_contains, body_not_contains,
  body_matches, body_json, microdata, capture}` (waits for and consumes the
  next request on the path), `sink_expect_none: {path, for_ms}` (no further
  request) and `sink_expect_count: {path, count, within_ms, gaps_min_ms,
  gaps_max_ms}` (at least `count` arrivals, consumed ones included, with
  optional bounds on the gaps between them).
- `server-js`, `sessel`, `liquid` — informational, used for the matrix.
- `webdav` — uses the authoring plane beyond simple setup.
- `slow` — takes more than a few seconds; runs only with `--slow`.
- `sse-control` — needs the `prune_events` step (event retention control);
  local target only.

### Normalization

Before comparison the runner normalizes: host names (`${HOST}`), `Date`,
`Last-Modified`, `x-azure-ref`, `x-budget-*`, `x-storage-consumed`,
`X-Cache`, `Set-Cookie` values, multipart boundaries and SSE ids. ETag
*literals* are not compared unless a case spells one out; ETag
*relationships* are (use captures and `${…}` substitution, or
`etag_equals`/`etag_differs` expectations).

PageLove's free plan inserts a "Powered by Pagelove" footer before
`</body>` (LO-1). `harness.StripFooter` removes exactly that footer from
every body before any body or microdata check. pagelike never emits it.

`$${…}` in a case sends a literal `${…}` (for example, Sessel selector
literals such as `$${li} from self`). Every other `${name}` is substituted from
the case variables and captures.

### Evidence levels

- `documented` — stated in public docs (cite page + section + snapshot date).
- `client-source` — required by beta-js or other official client code (cite file@commit).
- `demo-source` — relied on by official demo/template apps (cite file@commit).
- `live-observed` — recorded from real PageLove (observation file cited).
- `inferred` — reasoned, not stated; must say why.

### Statuses

### Built-in identities

- `anonymous` — no credentials and **no cookie jar**: every request (and every
  SSE stream) is a fresh visitor with a fresh session. Cases that open
  several anonymous SSE streams rely on this.
- `visitor` — anonymous too, but with a cookie jar that lives for the whole
  case run: it keeps the session cookie the server sets, so consecutive
  `visitor` steps share one anonymous session (transient elements,
  session-scoped state). Live runs need no secrets for it.
- `author` — the authoring key (the WebDAV plane by default; `plane: public`
  sends the key to the public plane).
- `<actor>` — a named end-user declared under `actors:` (signed in; keeps its
  own cookies; `multi-actor`).

Runner details: `as: author` (or a `plane: dav` step without `as`) sends the
authoring key; `as: anonymous` on the dav plane sends no credentials.

| `status:` | Local target | Live target |
|---|---|---|
| (none) | Runs. | Runs if `live: true`. |
| `disputed` | Skipped: pagelike implements the winning claim. | A failure is reported as `XFAIL`, which is expected. A pass is reported as a failure with an `XPASS` message: PageLove honours the losing claim, so the compatibility decision must be reviewed. |
| `live-divergence` | Skipped. | Runs. |

When sources disagree, keep one case per claim, mark the losing ones with
`status: disputed` and explain in `notes`. The compatibility decision is
recorded in the spec and in `docs/compat/decisions.md`.

A `live-divergence` case records a deliberate difference. It is a sibling
`<id>.live` of a case whose pagelike behaviour differs from live PageLove on
purpose (keep-documented-security or keep-standard in
`docs/compat/decisions.md`), and it asserts PageLove's behaviour. If one
starts failing live, PageLove changed, and the divergence can be revisited.

The summary line counts passed, failed, XFAIL and skipped cases.

Runner details:
- `as: author` (or a `plane: dav` step without `as`) sends the authoring key.
  `as: author` goes to the WebDAV host unless the step says `plane: public`,
  in which case the key is sent to the public plane.
- `as: anonymous` on the dav plane sends no credentials.

The live run of 2026-09-28 predates both rules. See `docs/compat/decisions.md`
for the harness artifacts that it produced.

When an `sse_expect` fails, the failure lists every event the stream saw.
`microdata` expectation values are `${…}`-expanded like other expectations.
`site.settings.max_request_body_bytes` is applied after the case's files are
uploaded, so setup is not refused by a small cap.
