---
name: reconcile-live
description: Run a live PageLove sample, triage every divergence, and bring pagelike and its cases back in line under the divergence policy — optionally fanned out to one agent per area. Use for periodic compatibility sweeps or after PageLove changes. Requires the user's go-ahead and a disposable host.
---

# Reconcile with live PageLove

Only with the user's go-ahead and a disposable host (see the root AGENTS.md).
The rules are in docs/decisions/0005 and 0006.

1. **Sample.** Pick one case per feature (at most ~25 per area). The list of
   2026-09-29 is `harness/live-sample-2026-09-29.txt`. Split it into batches
   under `LIVE_MAX_CASES` and run each:
   `scripts/live-run.sh sample --ids @<batch-file>`.
2. **List the divergences:**
   `scripts/divergences.py harness/observations/live-<date>-sample > /tmp/div.json`.
   This groups the failing cases by area, at most 10 per group.
3. **Triage.** For each case, read the observation JSON and the case. Decide
   whether it's a harness artifact or a real difference. Use
   `probe-pagelove` when the cause is unclear.
4. **Resolve** each divergence with exactly one class:
   - **adopt-live:** the case follows live, cites the observation, and
     pagelike changes (`implement-behaviour`);
   - **keep-documented-security** or **keep-standard:** the case stays, and a
     `<id>.live` sibling (`status: live-divergence`) asserts PageLove;
   - **harness artifact:** fix the case.
5. **For many divergences, fan out.** Start the saved workflow with the
   groups: `Workflow({name: "reconcile-live", args: {repoRoot, date,
   sampleDir, groups}})`. `repoRoot` is this checkout's absolute path;
   `groups` comes from step 2. Each agent works in its own worktree and
   writes `docs/compat/decisions-<date>/<area>.md`. Then:
   - merge the branches, least overlapping first, and run
     `scripts/check.sh` after each;
   - re-check every adopt-live against 0005 and 0006, and reclassify where
     needed (record it in an "Integration" section).
6. **Confirm.** Every case whose expectation changed without a live run
   goes into one final run:
   `scripts/live-run.sh final --ids <changed ids and their siblings>`.
   Expected: adopted cases pass, kept cases fail, `.live` siblings pass,
   disputed cases XFAIL. Anything else is a new finding.
7. **Publish the evidence:**
   - `scripts/matrix.sh` (the Total row's live "differ" count should be 0);
   - update the numbers in `docs/compat/report.md` and README.md;
   - add a CHANGELOG entry;
   - commit the observation directories with the decisions.
