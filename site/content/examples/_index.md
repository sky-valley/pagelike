---
title: Examples index
description: Runnable pagelike example apps that ship with the repo.
---

# Examples

The `examples/` directory ships four runnable apps. Each one uses
only PageLove-documented runtime features (no pagelike-only
extensions needed at the protocol level; the participation views and
remix are pagelike extensions) and is verified on pagelike by the
browser suites at `e2e/tests/examples.spec.mjs`. They have **not**
been run on PageLove — the participants sign in, and a PageLove host
needs its own OIDC provider for that, which the disposable test
host does not have.

| Example | What it shows |
|---|---|
| [`sky/`](/examples/sky/) | media submissions (image uploads into a per-user folder), server-enforced ownership (templated selectors + Sessel trigger + ClosedShape), live updates, shareable participation views, remix |
| [`poll/`](/examples/poll/) | one vote per signed-in person, change/withdraw own vote only, server-rendered tallies (binding + Liquid), live tallies |
| [`board/`](/examples/board/) | composition (include + binding + Liquid lanes), a `TransitionConstraint` state machine (illegal moves → 422), schema validation, live refresh of composed lanes |
| [`feed/`](/examples/feed/) | experiences embedded cross-origin in iframes |

## Install

Run `go build -o bin/pagelike ./cmd/pagelike`, start the server, then:

```sh
examples/install.sh sky sky
# open http://sky.localhost:8787/
```

`install.sh` creates the site if needed, sets partitioned cookies
(for iframe use), mints a one-hour site-scoped authoring key, and
uploads every file under `examples/<name>/site/`.

## Remix

```sh
pagelike fork --from sky --to dog --note "show me your dog"
```

Forking carries the authored baseline only — schema and `AuthorizationRule`
items — and never copies participants, identities, or keys. Lineage is
recorded and surfaceable as a pagelike extension at
`/-pagelike/experience`.
