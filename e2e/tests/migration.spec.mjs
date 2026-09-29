// Migration demonstration: an app with live participation runs on a
// disposable PageLove host, is copied to pagelike with `pagelike migrate`,
// and keeps working there — state, rules and live updates.
//
// It talks to live PageLove (and creates one poll there per run), so it
// runs only when asked: PAGELIKE_LIVE_E2E=1. It also requires
// .secrets/pagelove.env (PAGELOVE_API_KEY, PAGELOVE_HOST,
// PAGELOVE_DAV_URL, PAGELOVE_DISPOSABLE=yes) and the unmodified
// pagelove-polls app installed at that host's root. Skipped otherwise.
import { test, expect } from "@playwright/test";
import { existsSync, readFileSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import { state, origin, repoRoot } from "../lib/pagelike.mjs";

const secrets = join(repoRoot, ".secrets", "pagelove.env");
const env = existsSync(secrets)
  ? Object.fromEntries(readFileSync(secrets, "utf8").split("\n").filter((l) => l.includes("=")).map((l) => l.split(/=(.*)/s).slice(0, 2)))
  : {};
const LIVE = process.env.PAGELIKE_LIVE_E2E === "1" && env.PAGELOVE_HOST && env.PAGELOVE_DISPOSABLE === "yes" ? `https://${env.PAGELOVE_HOST}` : null;

test.describe.configure({ mode: "serial" });
test.skip(!LIVE, "live migration demo: set PAGELIKE_LIVE_E2E=1 with a disposable PageLove host configured");

let pollPath;
const stamp = Date.now().toString(36);

async function vote(page, name) {
  await page.fill("#entry-name", name);
  await page.locator("#entry .vote").first().click(); // toggle the first option to "yes"
  await page.click("#save");
  await expect(page.locator("#status, [role=status]").first()).toContainText("Saved", { timeout: 10000 });
}

test("phase 1 — live participation on PageLove", async ({ browser }) => {
  // Create a poll exactly as the app's form does (a form POST to the template).
  const form = new URLSearchParams({
    title: `Migration demo ${stamp}`, organizer: "Ada", description: "pagelike migration demo",
    options: "2026-10-01T12:00|Thu|1 Oct|12:00;;2026-10-02T12:00|Fri|2 Oct|12:00", listed: "on",
    created: new Date().toISOString(), createdLabel: "today",
  });
  const res = await fetch(`${LIVE}/templates/new-poll.html`, { method: "POST", body: form, redirect: "manual" });
  expect([301, 302, 303]).toContain(res.status);
  pollPath = new URL(res.headers.get("location"), LIVE).pathname;
  expect(pollPath).toMatch(/^\/polls\/[a-z0-9]+\.html$/);

  const a = await (await browser.newContext()).newPage();
  const b = await (await browser.newContext()).newPage();
  await a.goto(LIVE + pollPath);
  await b.goto(LIVE + pollPath);
  await expect(a.locator("h1")).toContainText(`Migration demo ${stamp}`);
  await b.waitForTimeout(1500); // let both live streams connect
  await vote(a, "Ada (on PageLove)");
  // The other session sees it live, over PageLove's own SSE.
  await expect(b.locator("#responses tr", { hasText: "Ada (on PageLove)" })).toBeVisible({ timeout: 10000 });
  await vote(b, "Bo (on PageLove)");
  await expect(a.locator("#responses tr", { hasText: "Bo (on PageLove)" })).toBeVisible({ timeout: 10000 });
});

test("phase 2 — migrate the host to pagelike", async () => {
  const out = mkdtempSync(join(tmpdir(), "pagelike-migration-"));
  const st = state();
  const log = execFileSync(st.bin, ["migrate", "--from-dav", env.PAGELOVE_DAV_URL, "--key-file", secrets, "--out", out,
    "--exclude", "/_pl/", "--import", "--site", "polls", "--data", st.data], { encoding: "utf8" });
  expect(log).toContain("imported into site polls");
  writeFileSync(join(repoRoot, "e2e", "test-results", "migration-log.txt"), log);
  // The poll document arrived byte-for-byte as PageLove stores it.
  const liveBytes = await (await fetch(env.PAGELOVE_DAV_URL.replace(/\/$/, "") + pollPath, { headers: { Authorization: `Bearer ${env.PAGELOVE_API_KEY}` } })).text();
  const copied = readFileSync(join(out, "files", pollPath.slice(1)), "utf8");
  expect(copied).toBe(liveBytes);
});

test("phase 3 — the app keeps working on pagelike", async ({ browser }) => {
  const c = await (await browser.newContext()).newPage();
  const d = await (await browser.newContext()).newPage();
  await c.goto(origin("polls") + pollPath);
  await d.goto(origin("polls") + pollPath);
  // State survived: both PageLove participants are there.
  await expect(c.locator("#responses tr", { hasText: "Ada (on PageLove)" })).toBeVisible();
  await expect(c.locator("#responses tr", { hasText: "Bo (on PageLove)" })).toBeVisible();
  await d.waitForTimeout(500);
  // New participation, live across sessions, on pagelike.
  await vote(c, "Cy (on pagelike)");
  await expect(d.locator("#responses tr", { hasText: "Cy (on pagelike)" })).toBeVisible({ timeout: 5000 });
  // The app's own rules still hold: an existing poll cannot be overwritten…
  const overwrite = await c.evaluate(async (p) => (await fetch(p, { method: "PUT", headers: { "Content-Type": "text/html" }, body: "<p>gone</p>" })).status, pollPath);
  expect(overwrite).toBe(409);
  // …a row may contain only a name and vote cells (closed shape)…
  const script = await c.evaluate(async (p) => (await fetch(p, { method: "POST", headers: { Range: "selector=#responses" },
    body: '<tr id="r-evil" itemscope itemtype="https://pagelove.org/PollResponse"><th scope="row" itemprop="name">x<script>alert(1)</script></th></tr>' })).status, pollPath);
  expect(script).toBe(422);
  // …and only the responses body can be written.
  const title = await c.evaluate(async (p) => (await fetch(p, { method: "PUT", headers: { Range: "selector=h1" }, body: "<h1>hijacked</h1>" })).status, pollPath);
  expect([401, 403]).toContain(title);
  // New polls can be created on pagelike through the same template.
  const res = await fetch(origin("polls") + "/templates/new-poll.html", { method: "POST", redirect: "manual",
    body: new URLSearchParams({ title: `Created on pagelike ${stamp}`, organizer: "Cy", options: "2026-10-03T12:00|Sat|3 Oct|12:00", listed: "on" }) });
  expect([301, 302, 303]).toContain(res.status);
  const created = await fetch(origin("polls") + new URL(res.headers.get("location"), origin("polls")).pathname);
  expect(await created.text()).toContain(`Created on pagelike ${stamp}`);
});
