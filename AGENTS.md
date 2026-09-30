# AGENTS.md

pagelike is an open-source Go server compatible with PageLove. HTML documents
are the app and its database, CSS selectors address the data, and HTTP plus
server-sent events read, write and stream it. Read README.md for the product
and docs/architecture.md for the package map.

## Non-negotiables

1. **Evidence before behaviour.** Every behaviour change comes with a harness
   case (`harness/cases/<area>/*.yaml`) that has an `evidence` level and a
   `source`. Never change a case's expectation without new evidence.
   Differences from PageLove follow the policy in
   [docs/decisions/0005](docs/decisions/0005-divergence-policy.md).
2. **All HTML goes through `internal/dom`**, which implements PageLove's
   document model: trees built from tokens and one stored form
   ([0004](docs/decisions/0004-pagelove-document-model.md)). Never call
   `html.Parse`, `html.ParseFragment` or `html.Render` in runtime code.
3. **Security boundaries don't move to match PageLove**
   ([0006](docs/decisions/0006-security-divergences.md)):
   - authoring keys never reach browsers and are never an end-user identity;
   - sites never see each other;
   - `--dev-insecure-auth` stays opt-in and loopback-only.
4. **Live PageLove is someone else's service.**
   - Run `--target live` only through `scripts/live-run.sh`, only when the
     user asks, and only against a disposable host they created.
   - The harness caps it at 3 requests/s.
   - Never read or print `.secrets/`.
5. **This repository is public.** No personal paths, account names, host
   names, keys, or references to private products. `scripts/scan-public.sh`
   must pass.

## Commands

```bash
scripts/check.sh            # definition of done: gofmt, vet, go test ./... (every local harness case), e2e
scripts/check.sh --quick    # skip the browser suites
go run ./harness/cmd/harness run --filter <area>     # iterate on one area
go run ./harness/cmd/harness run --slow --ids <id>   # slow cases (SSE retention, keepalives)
scripts/build-docs.sh       # build the docs site to site/public and run scan-public
go run ./cmd/site build     # rebuild site/public (alternative invocation)
go run ./cmd/site serve     # local preview on 127.0.0.1:9000
```

## Where knowledge lives

| Need | Go to |
|---|---|
| Published docs site | `https://sky-valley.github.io/pagelike/` (built by `cmd/site`, deployed by `.github/workflows/docs.yml`) |
| Agent entry point | `llms.txt` at the repo root and `/llms.txt` on the docs site; full dump at `/llms-full.txt`; structured catalog at `/index.json` |
| Build / serve the site | `cmd/site/` (render.go, build.go, agent.go, spec.go); template + CSS embedded into the binary |
| Site content additions | `site/content/` (for-agents.md, commands.md, build/, examples/) |
| Subsystem rules | `internal/dom/AGENTS.md`, `harness/AGENTS.md`, `e2e/AGENTS.md` |
| Repeatable procedures | `.claude/skills/`: `implement-behaviour`, `probe-pagelove`, `reconcile-live`, `acceptance-run`, `release` |
| Deterministic steps | `scripts/`: `check`, `live-run`, `divergences`, `matrix`, `perf`, `scan-public`, `release`, `build-docs` |
| Multi-agent runs | `.claude/workflows/reconcile-live.js` (started by the `reconcile-live` skill) |
| Why things are the way they are | `docs/decisions/` (ADRs), `docs/compat/decisions*.md`, `docs/compat/live-observations.md` (LO-n) |
| What PageLove specifies | `docs/spec/<area>.md` (requirements `R-…` with evidence levels) |
| How we work | `docs/development.md` (test tiers, flakiness, CI, merging parallel agent work) |
| Backlog | GitHub issues (labels: `compatibility`, `scaling`, `security`, `cleanup`) |

## Style

- Simple internals: one process, SQLite, coarse per-site locks.
- Errors are PageLove error documents (`internal/errdoc`).
- Comments cite requirements (`R-LIQ-90`) or observations (`LO-15`,
  "live 2026-09-29").
- Match the surrounding code's density and naming.
- Commit messages explain why. End them with any attribution lines the
  harness asks for.
