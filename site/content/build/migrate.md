---
title: Migrate from PageLove
description: Move a PageLove host into pagelike byte-for-byte.
---

# Tutorial — Migrate from PageLove

`pagelike migrate` copies a PageLove host into pagelike byte for
byte. The destination is a pagelike directory tree (a backup-style
archive) that can be imported with `pagelike import --create` to
land on a fresh site, or restored directly into the data directory.

This is documented as a `compatibility` boundary: the migration
copies exactly what is on PageLove, including its storage model;
re-implementing the runtime in pagelike at the same fidelity is
not a goal of just a migration.

## Set up

You need:

- an `authoring` key from the PageLove host (the `Bearer` token for
  WebDAV writes),
- a local pagelike install, and
- a destination directory with enough space for the host's
  documents.

## Pull from PageLove

```sh
pagelike migrate --from-dav "https://dav-<host>.pagelove.com" \
                 --key-file ~/.pagelike/page-love-host.key \
                 --out ./host-export
```

The command reads every document over the authoring plane (WebDAV)
and writes them under `./host-export/site/`, preserving relative
paths including the literal `:` that appears in route files such as
`products/:slug.html`.

## Import into a pagelike site

```sh
pagelike site create migrated --data ./data
pagelike import   --site migrated --in ./host-export --create
```

`--create` is harmless if the site already exists, but pairing it
with `--site` lets you name the destination explicitly.

## What gets migrated

- Every document on the authoring plane, byte for byte. Stored
  form is preserved; `Render(Parse(Render(x))) == Render(x)`
  means the source documents round-trip cleanly in pagelike.
- Templates, schema declarations, `AuthorizationRule` items.
- Authored baseline (the snapshot the auth plane compares against
  for participation views).

## What doesn't

- Identity bindings. PageLove users on the source host do not
  transfer. Per-site OIDC (`pagelike identity set`) keeps its
  identity mapping.
- Live SSE subscriptions. They terminate at the cut-over; new
  clients reconnect against the pagelike URL.
- Participation views. The participation records carry over but
  refer to the source's per-participant identifiers; the
  `/-pagelike/participations` shape is pagelike-specific and is
  regenerated when the host comes up.

## Verify after migration

Run the release-acceptance tier-1 against the migrated host. The
browser suites at `e2e/tests/apps/*.spec.mjs` exercise unmodified
PageLove apps on pagelike; if a migration is functioning, those
tests pass against the new host.

## What to read next

- [`/compat/migration/`](/compat/migration/) — full rules for the
  move (runbook format).
- [`/compat/app-changes/`](/compat/app-changes/) — known changes
  apps carry to work on past PageLove quirks.
- [`/compat/report/`](/compat/report/) — the runtime-compatibility
  report (the bits of PageLove pagelike doesn't replicate, with
  reasons).
- [`/commands/`](/commands/) — `migrate`, `import`, `export`,
  `backup`, `fork`.
