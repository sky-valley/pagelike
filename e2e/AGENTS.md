# e2e/AGENTS.md

Browser suites (Playwright, system Chrome: `channel: chrome`, or
`PW_CHANNEL`). `global-setup.mjs` builds pagelike, starts it on a free port
with a fresh data directory, and writes a state file for each checkout.

| Suite | What | Needs |
|---|---|---|
| `tests/smoke.spec.mjs` | live updates across two sessions | nothing |
| `tests/examples.spec.mjs` | the example experiences: ownership, uploads, iframe sign-in with partitioned cookies, remix, poll, board | nothing |
| `tests/apps/*.spec.mjs` | release acceptance: real PageLove apps and the official client, unmodified (docs/spec/apps.md §7) | `research/upstream/` (`tools/research/clone_upstream.sh`); skipped without it |
| `tests/migration.spec.mjs` | a live PageLove app migrated into pagelike | `PAGELIKE_LIVE_E2E=1` plus a disposable host in `.secrets/pagelove.env`; **creates a poll on PageLove each run** |

## Rules

- Apps run **unmodified**. Every change made to an app to run it
  (configuration, fixtures, assembly, scenario steps) is recorded in
  `docs/compat/app-changes.md` with the reason. Never rewrite app behaviour
  to work around a missing pagelike feature; fix pagelike.
- Results live in `docs/compat/acceptance.md`.
  `node tools/acceptance-status.mjs <json report>` rebuilds its status column.
- Timing budgets (ACC-KB-13) report their numbers and declare `test.fail`
  when a budget is missed, instead of flaking.
- Use a `cli(...)` from `lib/pagelike.mjs` for server-side setup, such as
  `events prune` to force a stream reset.
