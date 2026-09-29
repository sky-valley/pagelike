# pagelike

**An open-source, self-hostable runtime compatible with [PageLove](https://pagelove.com).**
In this model the HTML document is both the application and its database. CSS
selectors address the data inside it, and plain HTTP reads it, writes it and
streams its changes. pagelike ships as one Go binary with one data directory
and no external services.

[![ci](https://github.com/sky-valley/pagelike/actions/workflows/ci.yml/badge.svg)](https://github.com/sky-valley/pagelike/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/sky-valley/pagelike)](https://github.com/sky-valley/pagelike/releases)
[![license](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/sky-valley/pagelike.svg)](https://pkg.go.dev/github.com/sky-valley/pagelike)

```bash
curl http://demo.localhost:8787/index.html -H 'Range: selector=#items'          # read one element
curl -X POST http://demo.localhost:8787/index.html -H 'Range: selector=#items' \
     --data-binary '<li>new item</li>'                                           # append to it
curl -N http://demo.localhost:8787/index.html -H 'Accept: text/event-stream'     # watch it change live
```

## Credit where it's due: PageLove

The ideas here are PageLove's. PageLove designed this way of building
software: documents as data, selectors as addresses, HTTP as the whole API,
and permissions, schemas and reactions declared inside the documents
themselves. They built it, documented it thoroughly, and published a client
and example applications under the MIT licence. If you want the hosted,
supported product, use [PageLove](https://pagelove.com).

pagelike is an **independent reimplementation** for people who want to
self-host, study or extend the model.
- **Not affiliated.** It is not affiliated with, sponsored by or endorsed by
  PageLove. "PageLove" is used only to describe compatibility.
- **Clean-room.** It contains **no PageLove server code**. It was written from
  PageLove's [public documentation](https://docs.pagelove.com), its public
  client and apps ([github.com/pagelove](https://github.com/pagelove)), and
  black-box testing of the hosted service at modest request rates, on test
  hosts created for the purpose.
- **Differences are documented and measured.** Where pagelike deliberately
  behaves differently (mostly security fixes), a test says so. See
  [Compatibility](#compatibility).

If you work on PageLove and something here should change, including the name,
please [open an issue](https://github.com/sky-valley/pagelike/issues).

## How it works

A pagelike app is a set of HTML files. Here is a guestbook:

```html
<!-- index.html -->
<ul id="entries"></ul>
<form onsubmit="event.preventDefault();
  const li = document.createElement('li'); li.textContent = this.msg.value;
  fetch('/index.html', {method: 'POST', headers: {Range: 'selector=#entries'},
                        body: li.outerHTML})">
  <input name="msg"><button>Sign</button>
</form>
```

```html
<!-- rules.html: who may do what, declared as data -->
<table><tr itemscope itemtype="https://pagelove.org/AuthorizationRule">
  <td itemprop="actor">*</td><td itemprop="resource">/index.html</td>
  <td itemprop="method">POST</td><td itemprop="selector">#entries</td>
  <td itemprop="action">Allow</td></tr></table>
```

The `POST` appends the `<li>` to the stored document, and every client
subscribed to the page's event stream receives the change. There is no server code,
database or API layer to write. Beyond that, the model covers:
- schemas and validation;
- composition: includes, bindings and Liquid templates;
- reactions: triggers, processors and webhooks;
- a small query language (Sessel);
- sandboxed server-side JavaScript.

All of it is declared in the documents.

## Install

Download a binary from [Releases](https://github.com/sky-valley/pagelike/releases)
(Linux and macOS, amd64 and arm64; Windows amd64, experimental):

```bash
tar -xzf pagelike_*_linux_amd64.tar.gz && sudo mv pagelike_*/pagelike /usr/local/bin/
pagelike version
```

Or build it with Go 1.26+. There are no C dependencies, because SQLite and
QuickJS are pure Go.

```bash
go install github.com/sky-valley/pagelike/cmd/pagelike@latest
```

## Quick start

```bash
pagelike site create demo                 # a site: its own data, rules and users
pagelike key create --site demo           # an authoring key, printed once
pagelike serve                            # http://127.0.0.1:8787
```

Sites are routed by host name, and `*.localhost` resolves to loopback, so no
DNS setup is needed locally:

| URL | Plane |
|---|---|
| `http://demo.localhost:8787/` | public plane: what browsers and PageLove clients use |
| `http://dav-demo.localhost:8787/` | authoring plane: WebDAV mount plus authoring `QUERY` (needs the authoring key) |
| `http://console.localhost:8787/api/sites` | control API (needs an instance key, scope `*`) |

Upload an app over WebDAV with the key as a bearer token, or as the Basic-auth
password that Finder and most WebDAV clients use:

```bash
curl -X PUT http://dav-demo.localhost:8787/index.html \
  -H "Authorization: Bearer $KEY" --data-binary @index.html
```

Writes are denied unless the site's documents contain `AuthorizationRule`
items that allow them, as on PageLove. For complete apps with ownership,
uploads, live updates, iframe embedding and remixing, see
[`examples/`](examples/).

## What's implemented

| Area | Highlights |
|---|---|
| Reading and writing | selector ranges, placements (`append`, `before`, …), PUT, POST, DELETE, MOVE, conditional requests with fragment ETags, microdata and JSON-LD, the Request Document |
| Protocol | OPTIONS (flat, 207 and 204), `QUERY` in css and Sessel modes, WebDAV authoring plane, uploads, directory index, error documents |
| Live updates | server-sent events with connection tokens (no echo to the writer), replay with `Last-Event-ID`, reset, keepalive, slow-consumer protection |
| Permissions and identity | AuthorizationRule tiers, deny-wins, templated values (`${request.auth.username}`), groups; local accounts; OpenID Connect with PKCE; partitioned cookies for iframes |
| Modeling | schemas, properties, cardinality, types, enums, defaults, `@write` resolvers, validators, uniqueness, references and cascades, shape constraints, transitions |
| Composition | includes, stamps, resource and expression bindings, method elements, templated resource creation, pagination, transient elements, XML documents |
| Languages | Liquid (PageLove dialect, autoescaped), Sessel (full interpreter), server JavaScript (QuickJS in bounded worker processes) |
| Reactions | triggers, processors, outbox, outbound HTTP to allowed destinations |

pagelike builds and stores documents the way PageLove does: straight from
tokens, with no HTML5 tree construction, serialized in one canonical form. So
selectors match what they match on PageLove; for example `table > tr` matches
`<table><tr>`. Details: [LO-15](docs/compat/live-observations.md).

## Compatibility

Compatibility is measured, not claimed. A differential harness of 1,426 YAML
cases runs against pagelike and, at modest rates, against live PageLove.

| | Result (2026-09-29) |
|---|---|
| Local | 1,358 pass, 0 fail, 68 skipped (live-only or disputed cases) |
| Live PageLove | 603 cases: 578 match, 25 differ on purpose, 0 unexplained |
| Real PageLove apps and the official client, unmodified | release acceptance tier A 34/34, tier B 36/36 |
| Crash safety | SIGKILL under concurrent writes: no acknowledged write lost, and state and events agree |

**Deliberate differences.** In each case the reason is recorded and a test
asserts PageLove's actual behaviour.
- **Security.** pagelike keeps the documented protection where PageLove
  doesn't enforce it:
  - selector-scoped Deny rules override resource-level Allows;
  - pages that read the signed-in user aren't publicly cached;
  - WebDAV uploads are schema-validated;
  - trigger gates fail closed;
  - `<`/`>` are escaped in stored attribute values (a noscript mutation-XSS).
- **Standards.** pagelike keeps standard behaviour where PageLove's differs:
  - microdata `itemref`;
  - Liquid range literals;
  - pagination links keep the query;
  - no `escape_once` double-encoding.

Full details:
- [docs/compat/report.md](docs/compat/report.md): the compatibility report;
- [docs/compat/matrix.md](docs/compat/matrix.md): per-feature results;
- [docs/compat/decisions.md](docs/compat/decisions.md) and
  [decisions-2026-09-29/](docs/compat/decisions-2026-09-29/): every divergence
  and how it was resolved;
- [docs/compat/live-observations.md](docs/compat/live-observations.md): what
  PageLove was observed to do.

## Self-hosting

1. Install the binary and choose a durable data directory, e.g. `/var/lib/pagelike`.
2. Point a wildcard DNS record (`*.example.org`) at the host and terminate TLS
   in a reverse proxy that keeps `Host` and doesn't buffer event streams
   ([`deploy/Caddyfile.example`](deploy/Caddyfile.example)).
3. Run it under a supervisor:
   `pagelike serve --data /var/lib/pagelike --listen 127.0.0.1:8787 --domain example.org --trust-proxy`.
   A hardened systemd unit is in [`deploy/pagelike.service`](deploy/pagelike.service).

Server JavaScript runs in worker processes of the same binary (`--js-workers N`,
default one per CPU). On Linux they run with memory limits and
`no_new_privs`.

**Storage.** Each site is one SQLite database (WAL, `synchronous=FULL`) plus
content-addressed blobs. Every acknowledged write is on disk, committed together
with its change event, before the response is sent.

```
data/
  control.db              authoring keys (hashes only)
  sites/<site>/site.db    documents, authored baseline, events, users, sessions
  sites/<site>/blobs/     uploaded files by SHA-256
```

**Operations.**

```bash
pagelike backup --out /backups/pagelike-$(date +%F)            # consistent online snapshot
pagelike export --site demo --out ./demo-export                # a site as plain files
pagelike import --site demo2 --in ./demo-export --create
pagelike events prune --site demo --older-than 10m             # drop old stream events
```

To restore a backup, stop pagelike and copy the backup directory over the data
directory.

## Identity

- **Local accounts:** `pagelike user add --site demo alice --email alice@example.com --verified --password-stdin`.
  Users sign in at `/auth/login` and get an HttpOnly `__Host-` session cookie.
- **OpenID Connect per site** (with PKCE): `pagelike identity set --site demo --issuer … --client-id …`.
- **Embedding in iframes on other sites:** `pagelike identity set --site demo --cookies partitioned --embed-origins https://host.example`.
- **Authoring keys** never act as an end-user identity and are never sent to browsers.
- **Local testing only:** `serve --dev-insecure-auth` accepts an
  `X-Pagelike-Dev-User` header. It refuses to start on a non-loopback address.

See [docs/identity.md](docs/identity.md).

## pagelike extensions

These are additions PageLove doesn't have. They never change a PageLove
request; they live under `/-pagelike/` and in the CLI.

- **Experience version:** `GET /-pagelike/experience` returns a digest of the
  site's authored content and, for a remix, where it came from.
- **Participation records:** every contribution that adds an identified
  element is recorded with its contributor and the version it was made
  against. `GET /-pagelike/participations` lists them, and `/-pagelike/p/<id>`
  is a shareable view of one contribution, shown only to viewers allowed to
  read it.
- **Remix:** `pagelike fork --from sky --to dog` copies what the author wrote,
  not what participants contributed, and records the lineage.
- **Migration:** `pagelike migrate --from-dav <PageLove WebDAV URL> --key-file … --out DIR --import --site NAME`
  copies a PageLove host into pagelike byte for byte. See
  [docs/compat/migration.md](docs/compat/migration.md).

## Development

```bash
go test ./...                                   # unit, integration, durability and every local harness case
go run ./harness/cmd/harness run                # the compatibility harness (add --slow for retention cases)
cd e2e && npm ci && npx playwright test         # browser suites (system Chrome)
tools/research/clone_upstream.sh                # optional: PageLove's apps, for the release acceptance suite
```

`go run ./harness/cmd/harness run --target live` runs cases against a PageLove
host that **you** create for testing. It refuses hosts not marked disposable
and limits itself to 3 requests/s. See [harness/README.md](harness/README.md).

**Where to read next:**
- Design: [docs/design.md](docs/design.md) and [docs/architecture.md](docs/architecture.md).
- Specification by area, with an evidence level for every requirement: [docs/spec/](docs/spec/).
- Decisions: [docs/decisions/](docs/decisions/).

Contributions are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md). To report
a vulnerability, see [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE). Third-party notices are in [NOTICE](NOTICE).
