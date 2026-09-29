# Contributing to pagelike

Thanks for helping. pagelike aims to behave like PageLove, so most changes
start with evidence about what PageLove does.

## Ground rules

- **Evidence first.** A behaviour change comes with a harness case
  (`harness/cases/<area>/*.yaml`). The case carries its `evidence` level
  (documented, client-source, demo-source, live-observed, inferred) and a
  `source` citing the docs page, the client source line, or a live
  observation.
- **Divergences are explicit.** If pagelike should deliberately differ from
  PageLove (security, standards), keep the pagelike case, and add a sibling
  `<id>.live` with `status: live-divergence` that asserts PageLove's
  behaviour. Record the decision in `docs/compat/`.
- **Live testing is polite.** Run `--target live` only against a PageLove
  host you created for testing (`PAGELOVE_DISPOSABLE=yes`). The harness
  limits itself to 3 requests/s; don't raise it.
- **Keep it simple.** One process, SQLite, coarse per-site locks. Match the
  surrounding code's style and comment density.

## Before you open a pull request

```bash
gofmt -l cmd internal harness test tools   # must print nothing
go vet ./...
go test ./...                              # includes every local harness case and the durability tests
cd e2e && npm ci && npx playwright test    # if you touched anything a browser sees
```

## Layout

- `cmd/pagelike`: the CLI.
- `internal/*`: the runtime (see docs/architecture.md for the package map).
- `harness/`: the differential compatibility harness and its cases.
- `docs/spec/`: requirements by area. `docs/compat/`: the compatibility
  report, matrix, decisions and live observations.
- `e2e/`: browser suites. `examples/`: example experiences. `deploy/`:
  systemd and Caddy examples.

By contributing you agree that your contributions are licensed under the
Apache License 2.0.
