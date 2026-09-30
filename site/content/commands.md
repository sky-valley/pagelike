---
title: CLI reference
description: A complete reference for the pagelike command-line interface.
---

# pagelike — CLI reference

`pagelike` is one binary. It runs a server (`serve`), administers an
instance (`site`, `key`, `user`, `identity`, `events`), and moves data
(`export`, `import`, `fork`, `backup`, `migrate`).

This page is also available as a structured JSON dump at
[`/commands.json`](/commands.json) for agents that prefer data over
prose. Coverage is kept in sync by the build step (`cmd/site`).

## `pagelike serve`

```sh
pagelike serve [--data DIR] [--listen ADDR] [--domain DOMAIN]
               [--trust-proxy] [--dev-insecure-auth]
               [--outbound-allow-private] [--outbound-allow LIST]
               [--js-workers N]
```

| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--data` | path | `./data` | data directory (`$PAGELIKE_DATA` overrides) |
| `--listen` | addr | `127.0.0.1:8787` | listen address |
| `--domain` | string | `localhost` | base domain (`<site>.<domain>`) |
| `--trust-proxy` | bool | false | honour `X-Forwarded-Proto` from a TLS-terminating proxy |
| `--dev-insecure-auth` | bool | false | DEVELOPMENT ONLY: accept `X-Pagelike-Dev-User` impersonation from loopback clients |
| `--outbound-allow-private` | bool | false | allow reactions to send HTTP to private/loopback destinations |
| `--outbound-allow` | csv | "" | comma-separated private destinations reactions may reach (host, ip, host:port, CIDR) |
| `--js-workers` | int | 0 (one per CPU) | server JavaScript worker processes |

`--dev-insecure-auth` refuses to start on a non-loopback address.
See `/hosting/` and `/docs/development.md` for production steps.

## `pagelike site`

```sh
pagelike site create NAME [--default-get allow|deny]
pagelike site list
pagelike site delete NAME
```

| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--data` | path | `./data` | data directory |
| `--default-get` | enum | `allow` | unauthenticated `GET` policy: `allow` or `deny` |

## `pagelike key`

```sh
pagelike key create [--site NAME|*] [--label TEXT] [--ttl 720h]
pagelike key list
pagelike key revoke ID
```

| Flag | Type | Default | Meaning |
|---|---|---|---|
| `--site` | csv | `*` | site the key may author (`*` = instance admin) |
| `--label` | string | "" | label shown in `key list` |
| `--ttl` | duration | 0 (no expiry) | lifetime of the secret, printed once at create-time |

The secret is printed once at create-time. Hashes are stored in
`data/control.db`; the secret is never recoverable.

## `pagelike user`

```sh
pagelike user add    --site NAME [--name TEXT] [--email EMAIL] [--verified]
                     [--password-stdin]
pagelike user list  --site NAME
pagelike user set   --site NAME USER [--verified BOOL] [--password-stdin]
pagelike user rm    --site NAME USER
```

| Flag | Type | Meaning |
|---|---|---|
| `--name` | string | display name (defaults to the username) |
| `--email` | email | local-account email |
| `--verified` | bool | mark the email as verified (skip the verification flow) |
| `--password-stdin` | bool | read the password from stdin |

## `pagelike identity`

Configures per-site OIDC. See [`/identity/`](/identity/) for the
concepts.

```sh
pagelike identity set  --site NAME [--issuer URL] [--client-id ID]
                        [--client-secret-stdin] [--cookies default|partitioned]
                        [--embed-origins ORIGINS] [--default COOL|LOOSE]
pagelike identity show --site NAME
pagelike identity clear --site NAME
```

## `pagelike events`

```sh
pagelike events prune --site NAME [--older-than 10m]
```

Drops stream events older than the retention window (default 10 min).
Clients reconnecting from before that point receive a reset event
instead of the dropped history.

## Data portability

```sh
pagelike export  --site NAME --out DIR
pagelike import  --site NAME --in DIR [--create]
                  [--authored-from-live]
pagelike fork    --from SRC --to DST [--note TEXT]
pagelike backup  --out DIR
pagelike migrate --from-dav <PageLove WebDAV URL> --key-file FILE
                  --out DIR [--import --site NAME]
```

`pagelike backup` writes an online-consistent copy of every site
database (`VACUUM INTO`) plus its blobs and the control database.
Restore by stopping pagelike and copying the backup directory over
the data directory. `pagelike migrate` moves a host from PageLove to
pagelike byte-for-byte; see [`/compat/migration/`](/compat/migration/)
for the rules.

## `pagelike version`

Prints the build version: `v1.2.3` for a release build, the module
version for `go install`, or `dev` for an untagged build.

## `pagelike jsrt-worker`

Internal: a server-JavaScript worker on stdin/stdout. Spawned by
`pagelike serve` on first use; do not invoke directly.
