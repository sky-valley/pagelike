---
name: acceptance-run
description: Run the release acceptance suite — real PageLove apps and the official client, unmodified, against pagelike in Chrome — and update docs/compat/acceptance.md. Use before a release or after changes to parsing, composition, authorization, SSE or the example apps.
---

# Run the release acceptance suite

1. **Fetch the apps:** `tools/research/clone_upstream.sh`. It checks out the
   public PageLove repositories at the commits in `research/COMMITS.txt`.
   To test newer upstream commits, update COMMITS.txt deliberately and say
   so in the results.
2. **Run**
   `cd e2e && npm ci && PLAYWRIGHT_JSON_OUTPUT_NAME=/tmp/acc.json npx playwright test tests/apps --reporter=json`.
   - The live migration demo stays skipped unless the user asks
     (`PAGELIKE_LIVE_E2E=1`), because it writes to PageLove.
3. **Update the results:**
   `node tools/acceptance-status.mjs /tmp/acc.json` (from `e2e/`) prints the status
   column for `docs/compat/acceptance.md`. Update the summary table and any
   rows whose notes changed.
4. **For each failure, decide:** is it pagelike, the app, or the scenario?
   - Fix pagelike (`implement-behaviour`).
   - Never change an app to get around a missing feature.
   - Configuration, fixtures, assembly and scenario steps may change. Every
     such change goes in `docs/compat/app-changes.md` with its reason.
5. **Performance scenarios** (ACC-KB-13) report their numbers and declare
   `test.fail` over budget. Record the numbers in the row.
