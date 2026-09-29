// A8 — demo-apps @ c4dd883 (docs/spec/apps.md §7.8): ACC-DM-1..6.
// Installed as §6.3 prescribes: `python scripts/prepare-deploy.py --host
// demos.localhost --output <tmp>`, then every prepared file uploaded over
// WebDAV. The members-only feed reader signs in with a local account
// (alice@example.com, verified), the `users` actor of demo 04.
import { test, expect } from "@playwright/test";
import { createSite, addUser } from "../../lib/pagelike.mjs";
import { hasResearch, upstream, install, dav, http, newSession, expect2xx, origin, realErrors, npmDeps, repoRoot } from "../../lib/apps.mjs";
import { mkdtempSync, readFileSync, mkdirSync, cpSync, symlinkSync, writeFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync, spawnSync } from "node:child_process";

test.skip(!hasResearch(), "research/ (upstream apps) not available");
test.describe.configure({ mode: "serial" });

const SITE = "demos";
let key;

/** Prepare the deploy bundle with the app's own script (§6.3). */
export function prepareDemos(host) {
  const out = mkdtempSync(join(tmpdir(), "pagelike-demos-"));
  execFileSync("python3", ["scripts/prepare-deploy.py", "--host", host, "--output", out], { cwd: upstream("demo-apps"), encoding: "utf8" });
  return out;
}

test.beforeAll(async () => {
  key = createSite(SITE, { defaultGet: "allow" });
  await install(SITE, key, prepareDemos("demos.localhost"));
  addUser(SITE, "alice", { email: "alice@example.com", verified: true, name: "Alice", password: "alice-local-test-pw" });
});

async function demoSession(browser, who) {
  const s = await newSession(browser, { reducedMotion: "reduce" });
  if (who) await s.context.addInitScript((w) => localStorage.setItem("pl-identity", w), who);
  return s;
}

const toast = (page) => page.locator("#pl-toasts");

test("ACC-DM-1 live smoke", { tag: "@tierA" }, async () => {
  // The PUBLIC_PATHS of scripts/check-live.py.
  const paths = ["/", "/demo-01-event/", "/demo-02-show-and-tell/", "/demo-03-resource-exchange/", "/demo-04-accountability/",
    "/demo-05-professional/", "/favicon.svg", "/assets/og-demo-apps.png"];
  const get = http(SITE);
  for (const p of paths) {
    const r = await get("GET", p);
    expect(r.status, p).toBe(200);
    if (p === "/") expect(r.text).not.toContain("__PAGELOVE_HOST__");
  }
  expect((await get("GET", "/demo-04-accountability/feed.html")).status).toBe(401);
});

test("ACC-DM-2 event", { tag: "@tierA" }, async ({ browser }) => {
  const PAGE = "/demo-01-event/index.html";
  const A = await demoSession(browser, "elena");
  const B = await demoSession(browser, "elena"); // second context, same attendee, loaded before A's RSVP
  await A.page.goto(origin(SITE) + "/demo-01-event/");
  await B.page.goto(origin(SITE) + "/demo-01-event/");
  let since = A.net.mark();
  await A.page.click("#pl-rsvp");
  const post = await A.net.waitFor({ method: "POST", path: PAGE, since });
  expect(post.req.range).toBe("selector=#pl-rsvps");
  expect2xx(post.status, "RSVP");
  await expect(A.page.locator("#attendee-elena")).toHaveText("Elena");

  const sinceB = B.net.mark();
  await B.page.click("#pl-rsvp");
  const dup = await B.net.waitFor({ method: "POST", path: PAGE, since: sinceB });
  expect(dup.status).toBe(422);
  expect(await dup.response.text()).toMatch(/uniqueness/i);
  await expect(toast(B.page)).toContainText("You've already RSVPed to this event.");

  since = A.net.mark();
  await A.page.click("#pl-cancel-rsvp");
  const del = await A.net.waitFor({ method: "DELETE", path: PAGE, since });
  expect(del.req.range).toBe("selector=#rsvp-elena");
  expect2xx(del.status, "cancel");

  // Seed a waitlisted RSVP (Farid), then cancel a "going" one (Alice): the
  // app promotes the oldest waitlisted RSVP with a selector PUT.
  const seed = await http(SITE)("POST", PAGE, { headers: { "Content-Type": "text/html", Range: "selector=#pl-rsvps" },
    body: `<li id="rsvp-farid" itemscope itemtype="https://demos.localhost/vocab/RSVP">
  <meta itemprop="rsvpKey" content="event-fall-meetup:farid">
  <meta itemprop="rsvpEventId" content="event-fall-meetup">
  <meta itemprop="attendee" content="farid">
  <meta itemprop="rsvpStatus" content="waitlist">
</li>` });
  expect2xx(seed.status, "seed waitlist");
  const C = await demoSession(browser, "alice");
  await C.page.goto(origin(SITE) + "/demo-01-event/");
  await expect(C.page.locator("#attendee-farid")).toHaveText("Farid (waitlist)");
  since = C.net.mark();
  await C.page.click("#pl-cancel-rsvp");
  const put = await C.net.waitFor({ method: "PUT", path: PAGE, since });
  expect(put.req.range).toBe("selector=#rsvp-farid meta[itemprop='rsvpStatus']");
  expect2xx(put.status, "promotion");
  await expect(toast(C.page)).toContainText("Promoted Farid from the waitlist.");
  const stored = await dav(SITE, key).text(PAGE);
  expect(stored).toMatch(/id="rsvp-farid"[\s\S]*?rsvpStatus" content="going"/);
  for (const s of [A, B, C]) expect(realErrors(s.errors)).toEqual([]);
  for (const s of [A, B, C]) await s.context.close();
});

test("ACC-DM-3 show and tell", { tag: "@tierA" }, async ({ browser }) => {
  const PAGE = "/demo-02-show-and-tell/index.html";
  const A = await demoSession(browser, "carla");
  await A.page.goto(origin(SITE) + "/demo-02-show-and-tell/");
  let since = A.net.mark();
  await A.page.fill("#ns-title", "Walnut cutting board");
  await A.page.fill("#ns-desc", "End grain, finished with oil.");
  await A.page.fill("#ns-tags", "woodworking, kitchen");
  await A.page.click('#pl-new-submission button[type="submit"]');
  const post = await A.net.waitFor({ method: "POST", path: PAGE, since });
  expect(post.req.range).toBe("selector=#pl-grid");
  expect2xx(post.status, "publish");
  const card = A.page.locator("#pl-grid > li.card", { hasText: "Walnut cutting board" });
  await expect(card).toHaveCount(1);
  const id = await card.getAttribute("id");

  const B = await demoSession(browser, "carla"); // same voter, page loaded before the reaction
  await B.page.goto(origin(SITE) + "/demo-02-show-and-tell/");
  since = A.net.mark();
  await card.locator(`[data-react-for="${id}"]`).click();
  const react = await A.net.waitFor({ method: "POST", path: PAGE, since });
  expect(react.req.range).toBe("selector=#pl-reactions");
  expect2xx(react.status, "react");

  // After a reload the client sees its own reaction and sends nothing...
  await A.page.reload();
  since = A.net.mark();
  await A.page.locator(`[data-react-for="${id}"]`).click();
  await expect(toast(A.page)).toContainText("You've already reacted to this.");
  expect(A.net.writes({ since })).toHaveLength(0);
  // ...and a stale page reacting again is refused by the server's unique key.
  const sinceB = B.net.mark();
  await B.page.locator(`[data-react-for="${id}"]`).click();
  const dup = await B.net.waitFor({ method: "POST", path: PAGE, since: sinceB });
  expect(dup.status).toBe(422);
  await expect(toast(B.page)).toContainText("You've already reacted to this.");

  // Comment on the new project: the .comments shape lies inside #pl-grid's.
  since = A.net.mark();
  await A.page.locator(`[data-comment-toggle="comments-${id}"]`).click();
  await A.page.fill("#pl-comment-text", "Beautiful grain!");
  await A.page.click("#pl-comment-submit");
  const comment = await A.net.waitFor({ method: "POST", path: PAGE, since });
  expect(comment.req.range).toBe(`selector=#comments-${id}`);
  expect2xx(comment.status, "comment");
  await expect(toast(A.page)).toContainText("Comment posted.");
  for (const s of [A, B]) expect(realErrors(s.errors)).toEqual([]);
  for (const s of [A, B]) await s.context.close();
});

test("ACC-DM-4 resource exchange", { tag: "@tierA" }, async ({ browser }) => {
  const PAGE = "/demo-03-resource-exchange/index.html";
  const A = await demoSession(browser, "alice");
  await A.page.goto(origin(SITE) + "/demo-03-resource-exchange/");
  let since = A.net.mark();
  await A.page.selectOption("#nl-type", "have");
  await A.page.fill("#nl-title", "Tile saw");
  await A.page.fill("#nl-desc", "Wet saw, free to borrow for a weekend.");
  await A.page.click('#pl-new-listing button[type="submit"]');
  const post = await A.net.waitFor({ method: "POST", path: PAGE, since });
  expect(post.req.range).toBe("selector=#pl-listings");
  expect2xx(post.status, "listing");
  const listing = A.page.locator("#pl-listings > li.listing", { hasText: "Tile saw" });
  await expect(listing).toHaveCount(1);
  const id = await listing.getAttribute("id");

  const B = await demoSession(browser, "ben"); // sees the listing still open
  await B.page.goto(origin(SITE) + "/demo-03-resource-exchange/");
  await expect(B.page.locator(`#status-${id}`)).toHaveText("open");

  since = A.net.mark();
  await listing.getByRole("button", { name: "Claim" }).click();
  await expect(toast(A.page)).toContainText("Claimed.");
  const claim = A.net.find({ method: "POST", path: PAGE, since })[0];
  expect(claim.req.range).toBe("selector=#pl-claims");
  expect2xx(claim.status, "claim");
  const status = A.net.find({ method: "PUT", path: PAGE, since })[0];
  expect(status.req.range).toBe(`selector=#status-${id}`);
  expect2xx(status.status, "open→claimed");

  const sinceB = B.net.mark();
  await B.page.locator(`#${id}`).getByRole("button", { name: "Claim" }).click();
  const late = await B.net.waitFor({ method: "POST", path: PAGE, since: sinceB });
  expect(late.status).toBe(422);
  expect(await late.response.text()).toMatch(/uniqueness/i);
  await expect(toast(B.page)).toContainText("Someone already claimed this");

  since = A.net.mark();
  await listing.getByRole("button", { name: "Mark completed" }).click();
  await expect(toast(A.page)).toContainText("Marked completed.");
  expect2xx(A.net.find({ method: "PUT", path: PAGE, since })[0].status, "claimed→completed");
  const back = await http(SITE)("PUT", PAGE, { headers: { "Content-Type": "text/html", Range: `selector=#status-${id}` },
    body: `<span class="status-tag" id="status-${id}" data-status="claimed" itemprop="status">claimed</span>` });
  expect(back.status, "completed→claimed is not a declared transition").toBe(422);

  // The seeded "drill" listing (claimed): release.
  since = A.net.mark();
  await A.page.locator("#listing-drill").getByRole("button", { name: "Release claim" }).click();
  await expect(toast(A.page)).toContainText("Claim released.");
  const del = A.net.find({ method: "DELETE", path: PAGE, since })[0];
  expect(del.req.range).toBe("selector=#claim-listing-drill");
  expect2xx(del.status, "release");
  const reopen = A.net.find({ method: "PUT", path: PAGE, since })[0];
  expect(reopen.req.range).toBe("selector=#status-listing-drill");
  expect2xx(reopen.status, "claimed→open");
  for (const s of [A, B]) expect(realErrors(s.errors)).toEqual([]);
  for (const s of [A, B]) await s.context.close();
});

test("ACC-DM-5 accountability", { tag: "@tierA" }, async ({ browser }) => {
  const FEED = "/demo-04-accountability/feed.html";
  const A = await demoSession(browser, "dmitri");
  await A.page.goto(origin(SITE) + "/demo-04-accountability/");
  let since = A.net.mark();
  await A.page.fill("#ci-note", "Morning practice done");
  await A.page.click('#pl-checkin-form button[type="submit"]');
  const post = await A.net.waitFor({ method: "POST", path: FEED, since });
  expect(post.req.range).toBe("selector=#pl-checkins");
  expect2xx(post.status, "check-in");
  await expect(toast(A.page)).toContainText("Checked in.");
  since = A.net.mark();
  await A.page.click('#pl-checkin-form button[type="submit"]');
  const again = await A.net.waitFor({ method: "POST", path: FEED, since });
  expect(again.status).toBe(422);
  await expect(toast(A.page)).toContainText("You've already checked in today as Dmitri.");
  await A.page.click("#pl-peek");
  await expect(toast(A.page)).toContainText("Denied as expected: 401");

  const B = await newSession(browser);
  await B.page.goto(origin(SITE) + "/auth/login?redirect=" + encodeURIComponent(FEED));
  await B.page.fill('input[name="username"]', "alice");
  await B.page.fill('input[name="password"]', "alice-local-test-pw");
  await Promise.all([B.page.waitForURL(origin(SITE) + FEED), B.page.click('button[type="submit"]')]);
  const feed = B.net.find({ method: "GET", path: FEED }).at(-1);
  expect(feed.status).toBe(200);
  const date = new Date().toISOString().slice(0, 10);
  await expect(B.page.locator(`#checkin-dmitri-${date}`)).toHaveCount(1);
  await A.context.close();
  await B.context.close();
});

test("ACC-DM-6 professional", { tag: "@tierA" }, async ({ browser }) => {
  const PAGE = "/demo-05-professional/index.html";
  const A = await demoSession(browser, "farid");
  await A.page.goto(origin(SITE) + "/demo-05-professional/");
  const since = A.net.mark();
  await A.page.click("#tab-discussions");
  await A.page.fill("#nt-title", "Async standups");
  await A.page.fill("#nt-body", "Anyone moved fully async?");
  await A.page.click('#pl-new-topic button[type="submit"]');
  await expect(toast(A.page)).toContainText("Topic posted.");
  const topic = A.page.locator("#pl-topics > li.topic", { hasText: "Async standups" });
  const tid = await topic.getAttribute("id");
  await topic.locator("[data-reply-toggle]").click();
  await A.page.fill("#pl-reply-text", "We did, it works.");
  await A.page.click("#pl-reply-submit");
  await expect(toast(A.page)).toContainText("Reply posted.");
  await A.page.click("#tab-resources");
  await A.page.fill("#nr-title", "Remote handbook");
  await A.page.fill("#nr-url", "https://example.com/handbook");
  await A.page.fill("#nr-desc", "Good defaults for distributed teams.");
  await A.page.click('#pl-new-resource button[type="submit"]');
  await expect(toast(A.page)).toContainText("Resource shared.");
  const posts = A.net.find({ method: "POST", path: PAGE, since });
  expect(posts.map((p) => p.req.range)).toEqual(["selector=#pl-topics", `selector=#replies-${tid}`, "selector=#pl-resources"]);
  posts.forEach((p) => expect2xx(p.status, p.req.range));

  // As tests/demo-pages.spec.js does it, on a fresh page.
  await A.page.reload();
  await A.page.click("#tab-resources");
  const since2 = A.net.mark();
  await A.page.fill("#nr-title", "Unsafe example");
  await A.page.fill("#nr-url", "javascript:alert(document.domain)");
  await A.page.fill("#nr-desc", "This must never be stored as a clickable link.");
  await A.page.locator("#pl-new-resource").evaluate((f) => f.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })));
  await expect(toast(A.page)).toContainText("Only https:// and http:// links can be shared.");
  expect(A.net.writes({ since: since2 })).toHaveLength(0);
  expect(realErrors(A.errors)).toEqual([]);
  await A.context.close();
});

// §8.1 — the upstream Playwright suite (tests/demo-pages.spec.js, unchanged)
// pointed at pagelike: scripts/serve-test.cjs is replaced by a module whose
// startStaticServer() reverse-proxies to a pagelike site (the resolve-hook
// technique of §8.1, done by placing the stand-in where the test imports it).
test("§8.1 demo-apps' own Playwright suite against pagelike (ACC-UP)", { tag: "@tierB" }, async () => {
  test.setTimeout(300_000);
  const axe = npmDeps("demo-apps-axe", { packages: ["@axe-core/playwright@4.13.0"] });
  test.skip(!axe, "npm registry unreachable: cannot install @axe-core/playwright");
  const S = "demos-up";
  const k = createSite(S, { defaultGet: "allow" });
  await install(S, k, prepareDemos("demos.localhost"));

  const dir = mkdtempSync(join(tmpdir(), "demo-apps-suite-"));
  mkdirSync(join(dir, "tests"));
  mkdirSync(join(dir, "scripts"));
  mkdirSync(join(dir, "node_modules", "@axe-core"), { recursive: true });
  cpSync(upstream("demo-apps", "tests", "demo-pages.spec.js"), join(dir, "tests", "demo-pages.spec.js"));
  cpSync(join(repoRoot, "e2e", "upstream-adapters", "demo-apps", "serve-test.cjs"), join(dir, "scripts", "serve-test.cjs"));
  cpSync(join(repoRoot, "e2e", "upstream-adapters", "demo-apps", "playwright.config.mjs"), join(dir, "playwright.config.mjs"));
  writeFileSync(join(dir, "package.json"), JSON.stringify({ name: "demo-apps-suite", private: true, type: "module" }));
  const e2eModules = join(repoRoot, "e2e", "node_modules");
  for (const m of ["@playwright/test", "playwright", "playwright-core"]) {
    const from = m.startsWith("@") ? join(e2eModules, ...m.split("/")) : join(e2eModules, m);
    const to = m.startsWith("@") ? join(dir, "node_modules", ...m.split("/")) : join(dir, "node_modules", m);
    if (m.startsWith("@")) mkdirSync(join(dir, "node_modules", m.split("/")[0]), { recursive: true });
    symlinkSync(from, to);
  }
  symlinkSync(join(axe, "@axe-core", "playwright"), join(dir, "node_modules", "@axe-core", "playwright"));
  if (!existsSync(join(dir, "node_modules", "axe-core"))) symlinkSync(join(axe, "axe-core"), join(dir, "node_modules", "axe-core"));

  const env = Object.fromEntries(Object.entries(process.env).filter(([k]) => !/^(TEST_|PW_TEST|PLAYWRIGHT_)/.test(k)));
  const r = spawnSync("node", [join(e2eModules, "@playwright", "test", "cli.js"), "test", "-c", "playwright.config.mjs"], {
    cwd: dir, encoding: "utf8", timeout: 240_000, env: { ...env, PAGELIKE_DEMOS_ORIGIN: origin(S), FORCE_COLOR: "0" },
  });
  const out = (r.stdout + r.stderr).replace(/\x1b\[[0-9;]*m/g, "");
  let summary = "";
  try {
    const res = JSON.parse(readFileSync(join(dir, "results.json"), "utf8"));
    summary = JSON.stringify(res.stats);
  } catch {}
  test.info().annotations.push({ type: "upstream suite", description: summary || out.slice(-400) });
  expect(r.status, out).toBe(0);
  expect(out).toMatch(/3 passed/);
});
