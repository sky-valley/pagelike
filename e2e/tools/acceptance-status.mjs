#!/usr/bin/env node
// Print the status column of docs/compat/acceptance.md from a Playwright JSON
// report of the acceptance suites:
//
//   cd e2e && PLAYWRIGHT_JSON_OUTPUT_NAME=/tmp/acc.json npx playwright test tests/apps --reporter=json
//   node tools/acceptance-status.mjs /tmp/acc.json
//
// One row per scenario (the test title's leading ACC-… id), with its tier tag
// and pass / fail / skip / expected-fail (a known gap declared with
// test.fail at run time), plus per-tier counts.
import { readFileSync } from "node:fs";

const report = JSON.parse(readFileSync(process.argv[2], "utf8"));
const rows = [];
function walk(suite) {
  for (const spec of suite.specs || []) {
    const m = /^(ACC-[A-Z0-9]+-?[0-9a-z]*(\/[A-Z]+-\d+)?|§8\.\d)/.exec(spec.title);
    if (!m) continue;
    const tier = (spec.tags || []).map((t) => t.replace(/^@?tier/, "")).find((t) => /^[ABC]$/.test(t)) || "?";
    for (const t of spec.tests || []) {
      const last = t.results?.at(-1);
      let status = "skip";
      if (t.status === "expected") status = t.expectedStatus === "failed" ? "expected-fail" : "pass";
      else if (t.status === "unexpected") status = "fail";
      else if (t.status === "flaky") status = "pass (flaky)";
      rows.push({ id: m[1], title: spec.title, tier, status, ms: last?.duration ?? 0 });
    }
  }
  for (const s of suite.suites || []) walk(s);
}
for (const s of report.suites) walk(s);

console.log("| Scenario | Tier | Status | Test |");
console.log("|---|---|---|---|");
for (const r of rows) console.log(`| ${r.id} | ${r.tier} | ${r.status} | ${r.title.replace(/\|/g, "\\|")} |`);
const tiers = {};
for (const r of rows) {
  tiers[r.tier] ??= {};
  tiers[r.tier][r.status] = (tiers[r.tier][r.status] || 0) + 1;
}
console.log("\n" + Object.entries(tiers).map(([t, c]) => `Tier ${t}: ${Object.entries(c).map(([s, n]) => `${n} ${s}`).join(", ")}`).join("\n"));
