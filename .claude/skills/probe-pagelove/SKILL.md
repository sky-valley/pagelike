---
name: probe-pagelove
description: Find out exactly what live PageLove does for one behaviour, with minimal, polite requests — write probe cases, run them within a budget, record an LO observation. Use when a case's expectation is uncertain or a divergence's cause is unclear. Requires the user's go-ahead and a disposable host.
---

# Probe PageLove

Only with the user's go-ahead, and only when `.secrets/pagelove.env` names a
disposable host (`scripts/live-run.sh` checks it). Never read or print that
file.

1. **Frame the questions.** Write down the one to five questions the probe
   must answer. For example: is an implied `<tbody>` created? Does
   `table > tr` match?
2. **Write minimized probe cases**, in
   `harness/cases/<area>/<topic>-probes.yaml` (ids `<area>.probe-MMDD.<name>`,
   short and distinct in the first 40 characters):
   - one behaviour per step;
   - expectations loose (`status: [200, 201, …]`) when the point is to
     record the body;
   - several questions per case to save setup requests;
   - selector reads after a write if the question is about structure;
   - `evidence: live-observed`, `source: "probe YYYY-MM-DD"`.

   Run it locally first, so you know it works:
   `go run ./harness/cmd/harness run --ids <ids>`.
3. **Run it live, once:** `scripts/live-run.sh probe-<topic> --ids <ids>`.
   Read the bodies from `harness/observations/live-<date>-probe-<topic>/*.json`.
4. **Iterate only if an answer raises a sharper question.** Two or three
   runs at most, each a handful of cases.
5. **Record.**
   - Add an `LO-n` entry to `docs/compat/live-observations.md`: what was
     probed, what PageLove does (quote status and short bodies), and what
     pagelike does about it.
   - Then apply docs/decisions/0005 (`implement-behaviour` for adopt-live).
   - When the answers are adopted, the probe cases keep their expectations
     as ordinary cases. If pagelike deliberately differs, they become
     `status: live-divergence`.
