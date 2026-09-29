# Development guide

How pagelike is built and verified, including lessons that cost time to
learn.

## Test tiers

| Tier | Command | Runs | Time |
|---|---|---|---|
| Unit and integration | `go test ./internal/...` | package tests | ~1 min |
| Compatibility harness | `go test ./harness` or `go run ./harness/cmd/harness run` | every local case, in-process | ~1 min |
| Slow cases | `go run ./harness/cmd/harness run --slow` | plus SSE retention windows and long waits | ~12 min |
| Durability | `go test ./test/durability` | the real binary, SIGKILL during concurrent writes, restart and replay | ~15 s |
| Browser | `cd e2e && npx playwright test` | example experiences, the release acceptance suite (with `research/upstream`) | ~2 min |
| Everything | `scripts/check.sh` | all of the above except slow and live | ~5 min |
| Live | `scripts/live-run.sh <label> --ids …` | selected cases against a disposable PageLove host | depends on the run |

CI (`.github/workflows/ci.yml`) runs `go test ./...`, the public-content
scan and the browser suites on every push and pull request.

## Lessons

- **Timing assertions flake under load.** When a test proves a limit by
  its outcome (for example the error variant), log how long it took rather
  than failing on a wall-clock bound. The out-of-memory test failed once
  under the parallel `go test ./...` for this reason.
- **The server-JavaScript pool is finite.** More concurrent JavaScript
  requests than `--js-workers` queue up, and past the request's time
  budget they get `503 BudgetExceeded`. That's correct behaviour, but it
  looks like a regression in an oversubscribed test run (the CLI harness
  at `--parallel 8` on a 4-core runner). Keep test concurrency at or below
  the CPU count.
- **The in-process harness tests the code it was built from.** Rebuild or
  `go run` after changing runtime code.
- **Observation file names collide at 40 characters.** Choose ids that
  differ early.
- **Untagged sleeps slow everything.** A case that waits more than a few
  seconds is `requires: [slow]`.
- **`git add -A` picks up local build output.** `.gitignore` covers
  `/pagelike`, `/bin/`, `/dist/` and `research/`; run
  `scripts/scan-public.sh` before any push.

## Working with parallel agents

Large reconciliations were done by one agent per area, each in its own git
worktree (`.claude/workflows/reconcile-live.js`). What made merging them
work:
- **Separate files for decisions.** Each agent writes
  `docs/compat/decisions-<date>/<area>.md` instead of editing a shared
  file; the integrator writes the cross-cutting summary.
- **A bounded live budget per agent:** runs, cases per run, and only its
  own ids.
- **Merge the least-overlapping branches first**, then run the whole suite
  after each merge. Expect duplicated fixes, such as two agents adding the
  same helper test or two decoding implementations; keep one.
- **Re-check each adopt-live decision against 0005 and 0006 when
  merging.** An area agent optimises for its own area.
- **Point agent worktrees at the secrets with a `.secrets` symlink.** It's
  ignored as `/.secrets`, which covers a symlink as well as a directory.
  Never copy the file.

## Releasing

See `.claude/skills/release/SKILL.md`. In short: update CHANGELOG.md, run
`scripts/check.sh` and `scripts/scan-public.sh`, and push a `vX.Y.Z` tag.
The release workflow builds `scripts/release.sh` archives and publishes
them.
