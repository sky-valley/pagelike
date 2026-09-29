# Notes for coding agents

pagelike is a Go server compatible with PageLove. Read README.md first, then
docs/architecture.md for the package map.

- **Build:** `go build ./cmd/pagelike`. **Test everything:** `go test ./...`
  (it includes every local harness case and the durability tests). **Browser
  suites:** `cd e2e && npm ci && npx playwright test`.
- **Behaviour changes** need a harness case in `harness/cases/<area>/` with
  `evidence` and `source`; see CONTRIBUTING.md. Don't change a case's
  expectation without citing new evidence.
- **Never run `--target live`** unless the user has set up a disposable
  PageLove host in `.secrets/pagelove.env` and asked for it. Never print that
  file.
- **All HTML goes through `internal/dom`** (`dom.Parse`, `dom.Render`), which
  implements PageLove's document model. Don't call `html.Parse` or
  `html.Render` directly.
- **Keep to the style:** simple internals, explicit errors (PageLove error
  documents via `internal/errdoc`), comments that cite requirements (`R-…`) or
  live observations (`LO-…`, "live 2026-09-29").
