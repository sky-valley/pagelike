# harness/AGENTS.md

The differential compatibility harness: YAML cases run in-process against
pagelike (`--target local`), or against a PageLove host (`--target live`). The
case format is in `harness/README.md`; this file holds the rules and the
traps.

## Writing cases

- **Id:** `<area-prefix>.<topic>.<name>`, globally unique.
- **Evidence and source are mandatory:**
  - `documented`: docs page and section, with the snapshot date;
  - `client-source` or `demo-source`: repo@commit:line;
  - `live-observed`: the observation file path;
  - `inferred`: say from what.
- **Paths** go under `${P}/…`, so live runs stay inside the case's own
  `/_pl/<case>-<id>/` folder. Don't write host-wide paths unless the case is
  `root: true` (and read-only).
- **`live: false`** for cases that can't run on PageLove: they need
  `sse-control`, `restart`, `prune_events`, or pagelike-only endpoints.
- **`requires: [slow]`** for anything that sleeps more than a few seconds
  (retention windows, 60 s propagation checks). Untagged sleeps slow every
  `go test` run and every CI run.

## Divergences (docs/decisions/0005)

- **Keeping pagelike's behaviour:** leave the case, and add `<id>.live` with
  `status: live-divergence` asserting what PageLove does. Siblings run only
  with `--target live`.
- **A losing side of a documented contradiction:** `status: disputed`. It is
  reported as XFAIL live, and an XPASS is reported as a failure, so the
  decision gets revisited.
- **Probes** (`*.probe-MMDD.*`) isolate one question for a live run. Once
  their answer is adopted they are ordinary cases. If pagelike deliberately
  differs, give them `status: live-divergence`.

## Traps we hit

- **Observation file names** are the id with dots turned into dashes, cut
  to 40 characters. Two long ids with the same 40-character prefix overwrite
  each other's observations. Keep new ids short or distinct early; `.live`
  siblings are handled (`ObservationName`).
- **`go test ./harness`** runs every case in parallel at GOMAXPROCS. The
  CLI's `--parallel` above the CPU count starves the server-JavaScript
  worker pool and produces `503 BudgetExceeded` that isn't a real
  regression.
- **The local target runs in-process.** Rebuild `bin/harness` (or use
  `go run`) after changing runtime code, or you're testing old code.
- **The PageLove free plan** adds a "Powered by" footer before `</body>`.
  `harness.StripFooter` removes it before body checks.

## Live runs

Only through `scripts/live-run.sh`, only on request, and only against a
disposable host (see the root AGENTS.md). Observations go to
`harness/observations/live-<date>[-label]/` and are committed. They're the
evidence decision records cite. `scripts/matrix.sh` reads all of them, later
directories overriding earlier ones.
