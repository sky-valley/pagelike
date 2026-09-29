// A4 — pagelove-polls @ c9270e5 (docs/spec/apps.md §7.4): ACC-PO-1..7.
// Installed unmodified: every file under site/ at the root of site `polls`
// (default-GET allow, §6.3).
import { test, expect } from "@playwright/test";
import { createSite, cli } from "../../lib/pagelike.mjs";
import { hasResearch, upstream, install, dav, http, newSession, expect2xx, origin, realErrors } from "../../lib/apps.mjs";

test.skip(!hasResearch(), "research/ (upstream apps) not available");
test.describe.configure({ mode: "serial" });

const SITE = "polls";
let key;
let poll; // path of the poll created by ACC-PO-1

test.beforeAll(async () => {
  key = createSite(SITE, { defaultGet: "allow" });
  await install(SITE, key, upstream("pagelove-polls", "site"));
});

/** Record the SSE connection tokens the page receives (test instrumentation
 * around the native EventSource; the app's own code is untouched). */
async function pollSession(browser) {
  const s = await newSession(browser);
  await s.context.addInitScript(() => {
    const Native = window.EventSource;
    window.__plTokens = [];
    window.EventSource = class extends Native {
      constructor(...args) {
        super(...args);
        this.addEventListener("pagelove-connection", (e) => window.__plTokens.push(e.data));
      }
    };
  });
  return s;
}

async function createPoll(page, { title, organizer, listed = true }) {
  await page.goto(origin(SITE) + "/");
  await page.fill("#title", title);
  await page.fill("#organizer", organizer);
  if (!listed) await page.uncheck('input[name="listed"]');
  await Promise.all([page.waitForURL(/\/polls\/[a-z0-9]+\.html$/), page.click("#submit-btn")]);
  return new URL(page.url()).pathname;
}

/** A poll to work on when ACC-PO-1 did not run (e.g. with --grep): the same
 * form submission, sent from the test. */
async function ensurePoll() {
  if (poll) return poll;
  const form = new URLSearchParams({ title: "Team lunch", organizer: "Ada", description: "", listed: "on",
    options: "2026-10-01|Thu|1 Oct|;;2026-10-02|Fri|2 Oct|;;2026-10-03|Sat|3 Oct|", created: new Date().toISOString(), createdLabel: "today" });
  const r = await http(SITE)("POST", "/templates/new-poll.html", { headers: { "Content-Type": "application/x-www-form-urlencoded" }, body: form.toString() });
  expect(r.status).toBe(301);
  return (poll = r.headers.get("location"));
}

async function openPoll(page, path) {
  await page.goto(origin(SITE) + path);
  await expect.poll(() => page.evaluate(() => window.__plTokens?.length ?? 0)).toBeGreaterThan(0);
}

/** Enter a response: name, then click the n-th vote buttons (no→yes). */
async function vote(page, name, yes = [0]) {
  await page.fill("#entry-name", name);
  for (const i of yes) await page.locator("#entry .vote").nth(i).click();
  await page.click("#save");
}

test("ACC-PO-1 create a poll", { tag: "@tierA" }, async ({ browser }) => {
  const A = await pollSession(browser);
  const since = A.net.mark();
  poll = await createPoll(A.page, { title: "Team lunch", organizer: "Ada" });
  const post = A.net.find({ method: "POST", path: "/templates/new-poll.html", since })[0];
  expect(post.req["content-type"]).toBe("application/x-www-form-urlencoded");
  expect(post.status).toBe(301);
  expect(post.res.location).toMatch(/^\/polls\/[a-z0-9]{10}\.html$/);
  expect(poll).toBe(post.res.location);
  const landed = A.net.find({ method: "GET", path: poll, since })[0];
  expect(landed.status).toBe(200);
  await expect(A.page.locator("h1")).toHaveText("Team lunch");
  await expect(A.page.locator('[itemprop="organizer"]')).toHaveText("Ada");
  await expect(A.page.locator("#grid thead th[data-option]")).toHaveCount(3);
  await expect(A.page.locator("#responses > tr")).toHaveCount(0);
  const src = (await http(SITE)("GET", poll)).text;
  expect(src).not.toMatch(/\{%|\{\{/);

  // Back on the home page the new poll is the first card, counted by a selector GET.
  const since2 = A.net.mark();
  await A.page.goto(origin(SITE) + "/");
  const first = A.page.locator("#recent a.poll-card").first();
  await expect(first).toHaveAttribute("href", poll);
  await expect(first.locator("h3")).toHaveText("Team lunch");
  await expect(first).toContainText("0 responses");
  const count = A.net.find({ method: "GET", path: poll, range: "selector=#responses", since: since2 })[0];
  expect(count.status).toBe(206);

  // Stored form (decision C-12): no <base>, no p:template.
  const stored = await dav(SITE, key).text(poll);
  expect(stored).not.toMatch(/<base\b/);
  expect(stored).not.toContain("p:template");
  expect(stored).toContain('xmlns:p="https://pagelove.org/1.0"');
  expect(realErrors(A.errors)).toEqual([]);
  await A.context.close();
});

test("ACC-PO-2 unlisted poll", { tag: "@tierA" }, async ({ browser }) => {
  await ensurePoll();
  const A = await pollSession(browser);
  const path = await createPoll(A.page, { title: "Secret santa", organizer: "Bo", listed: false });
  await expect(A.page.locator("h1")).toHaveText("Secret santa");
  await A.page.goto(origin(SITE) + "/");
  await expect(A.page.locator("#recent a.poll-card").first()).toHaveAttribute("href", poll);
  await expect(A.page.locator(`#recent a.poll-card[href="${path}"]`)).toHaveCount(0);
  const r = await http(SITE)("GET", path);
  expect(r.status).toBe(200);
  await A.context.close();
});

test("ACC-PO-3 votes arrive live", { tag: "@tierA" }, async ({ browser }) => {
  await ensurePoll();
  const A = await pollSession(browser);
  const B = await pollSession(browser);
  await openPoll(A.page, poll);
  await openPoll(B.page, poll);
  const since = B.net.mark();
  await vote(B.page, "Grace", [0, 2]);
  const post = await B.net.waitFor({ method: "POST", path: poll, since });
  expect(post.req.range).toBe("selector=#responses");
  const tokens = await B.page.evaluate(() => window.__plTokens);
  expect(post.req["pagelove-connection"]).toBe(tokens.at(-1));
  expect2xx(post.status, "POST");
  await expect(B.page.locator("#status")).toContainText("Saved");
  await expect(B.page.locator("#responses > tr", { hasText: "Grace" })).toHaveCount(1);
  await B.page.waitForTimeout(500); // an echo would arrive now
  await expect(B.page.locator("#responses > tr", { hasText: "Grace" })).toHaveCount(1);

  const row = A.page.locator("#responses > tr", { hasText: "Grace" });
  await expect(row).toHaveCount(1, { timeout: 2000 });
  await expect(A.page.locator('#grid tfoot td[data-option="o1"] .count')).toHaveText("1");
  await expect(A.page.locator('#grid tfoot td[data-option="o2"] .count')).toHaveText("0");
  await expect(A.page.locator('#grid tfoot td[data-option="o3"] .count')).toHaveText("1");
  await expect(A.page.locator('#grid thead th[data-option="o1"] .best-tag')).toHaveText("Best");
  await expect(A.page.locator('#grid thead th[data-option="o2"] .best-tag')).toHaveCount(0);
  expect(realErrors(A.errors)).toEqual([]);
  expect(realErrors(B.errors)).toEqual([]);
  // Keep both open for ACC-PO-4.
  test.info().annotations.push({ type: "row", description: await B.page.locator("#responses > tr", { hasText: "Grace" }).getAttribute("id") });
  sessions = { A, B };
});

let sessions;

test("ACC-PO-4 edit and withdraw", { tag: "@tierA" }, async () => {
  const { A, B } = sessions;
  const rowB = B.page.locator("#responses > tr", { hasText: "Grace" });
  const id = await rowB.getAttribute("id");
  expect(id).toMatch(/^r-[a-z0-9]{1,24}$/);
  const since = B.net.mark();
  await rowB.locator("button.edit").click();
  await B.page.locator("#entry .vote").nth(1).click(); // o2: no → yes
  await B.page.click("#save");
  const put = await B.net.waitFor({ method: "PUT", path: poll, since });
  expect(put.req.range).toBe(`selector=#${id}`);
  expect2xx(put.status, "PUT");
  const body = await put.response.text();
  expect(body.trim()).toMatch(new RegExp(`^<tr id="${id}"`));
  await expect(B.page.locator("#status")).toContainText("Updated");
  await expect(A.page.locator(`#${id} td[data-option="o2"]`)).toHaveAttribute("data-vote", "yes", { timeout: 2000 });

  const since2 = B.net.mark();
  await B.page.locator(`#${id} button.rm`).click();
  const del = await B.net.waitFor({ method: "DELETE", path: poll, since: since2 });
  expect(del.req.range).toBe(`selector=#${id}`);
  expect2xx(del.status, "DELETE");
  await expect(A.page.locator(`#${id}`)).toHaveCount(0, { timeout: 2000 });
  await expect(B.page.locator(`#${id}`)).toHaveCount(0);
  const own = await B.page.evaluate((p) => JSON.parse(localStorage.getItem("polls-by-pagelove:own:" + p) || "[]"), poll);
  expect(own).not.toContain(id);
  expect(realErrors(A.errors)).toEqual([]);
  expect(realErrors(B.errors)).toEqual([]);
  await A.context.close();
  await B.context.close();
});

test("ACC-PO-5 same session, second tab", { tag: "@tierA" }, async ({ browser }) => {
  await ensurePoll();
  const A = await pollSession(browser);
  const A2 = await A.context.newPage(); // same session, different connection
  await openPoll(A.page, poll);
  await openPoll(A2, poll);
  await vote(A.page, "Linus", [1]);
  await expect(A.page.locator("#status")).toContainText("Saved");
  await expect(A2.locator("#responses > tr", { hasText: "Linus" })).toHaveCount(1, { timeout: 2000 });
  await A.page.waitForTimeout(500);
  await expect(A.page.locator("#responses > tr", { hasText: "Linus" })).toHaveCount(1);
  await A.context.close();
});

test("ACC-PO-6 guards", { tag: "@tierA" }, async () => {
  await ensurePoll();
  const req = http(SITE);
  const row = (id, th = '<th scope="row" itemprop="name">Eve</th>', extra = "") =>
    `<tr id="${id}" itemscope itemtype="https://pagelove.org/PollResponse">${th}<td itemprop="vote" data-option="o1" data-vote="yes">yes${extra}</td></tr>`;
  const html = { "Content-Type": "text/html" };
  // A row that exists and is then withdrawn, for (g).
  expect2xx((await req("POST", poll, { headers: { ...html, Range: "selector=#responses" }, body: row("r-gone1") })).status);
  expect2xx((await req("DELETE", poll, { headers: { Range: "selector=#r-gone1" } })).status);
  const before = await dav(SITE, key).text(poll);

  const a = await req("POST", poll, { headers: { ...html, Range: "selector=#responses" }, body: row("r-img1", undefined, '<img src="x">') });
  expect(a.status, "(a) extra <img>").toBe(422);
  const b = await req("POST", poll, { headers: { ...html, Range: "selector=#responses" },
    body: row("r-click1", '<th scope="row" itemprop="name" onclick="alert(1)">Eve</th>') });
  expect(b.status, "(b) onclick on <th>").toBe(422);
  const c = await req("POST", poll, { headers: { ...html, Range: "selector=tbody#responses" }, body: row("r-sel1") });
  expect(c.status, "(c) rule matches, trigger regex does not").toBe(403);
  const d = await req("PUT", poll, { headers: { ...html, Range: "selector=#poll" }, body: '<article id="poll">pwned</article>' });
  expect(d.status, "(d) PUT #poll").toBe(403);
  const e = await req("PUT", poll, { headers: html, body: "<!DOCTYPE html><html><body>pwned</body></html>" });
  expect(e.status, "(e) whole-document PUT").toBe(409);
  const f = await req("DELETE", poll);
  expect(f.status, "(f) whole-document DELETE").toBe(401);
  const g = await req("DELETE", poll, { headers: { Range: "selector=#r-gone1" } });
  expect(g.status, "(g) DELETE of a deleted row").toBe(416);
  expect(await dav(SITE, key).text(poll)).toBe(before);

  const h = await req("PUT", "/polls/zzzz.html", { headers: html,
    body: '<!DOCTYPE html><html><body><article id="poll" itemscope itemtype="https://pagelove.org/Poll"><h1 itemprop="title">New</h1></article></body></html>' });
  expect2xx(h.status, "(h) whole-document PUT of a new poll");
});

test("ACC-PO-7 stream reset reloads", { tag: "@tierA" }, async ({ browser }) => {
  await ensurePoll();
  const A = await pollSession(browser);
  // Simulate a dropped stream: the page's first subscription is answered by
  // the test with an early event id (PageLove's shape) and then ends, so
  // EventSource reconnects to pagelike with that Last-Event-ID. The
  // reconnect is held until the events after that id have been pruned, so
  // pagelike answers it with a reset.
  const stale = "v1~000000000000.0-1";
  let streams = 0;
  const reconnects = [];
  let release;
  const held = new Promise((r) => { release = r; });
  await A.context.route(origin(SITE) + poll, async (route) => {
    const r = route.request();
    const h = await r.allHeaders();
    if (r.method() !== "GET" || !(h.accept || "").includes("text/event-stream")) return route.continue();
    if (streams++ === 0) {
      return route.fulfill({ status: 200, headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" },
        body: `retry: 1500\nid: ${stale}\nevent: keepalive\ndata: x\n\n` });
    }
    if (streams === 2) {
      await held;
      // Chromium does not surface the Last-Event-ID of a fulfilled stream to
      // request interception, so the reconnect carries it explicitly.
      reconnects.push(stale);
      return route.continue({ headers: { ...h, "last-event-id": stale } });
    }
    return route.continue();
  });
  await A.page.goto(origin(SITE) + poll);
  // Meanwhile someone else responds.
  const req = http(SITE);
  expect2xx((await req("POST", poll, { headers: { "Content-Type": "text/html", Range: "selector=#responses" },
    body: '<tr id="r-late1" itemscope itemtype="https://pagelove.org/PollResponse"><th scope="row" itemprop="name">Late</th><td itemprop="vote" data-option="o1" data-vote="yes">yes</td><td itemprop="vote" data-option="o2" data-vote="no">no</td><td itemprop="vote" data-option="o3" data-vote="no">no</td></tr>' })).status);
  await expect(A.page.locator("#responses > tr", { hasText: "Late" })).toHaveCount(0);
  cli("events", "prune", "--site", SITE, "--older-than", "0s");
  const reload = A.page.waitForEvent("load", { timeout: 10_000 });
  release();
  await expect.poll(() => reconnects.length, { timeout: 10_000 }).toBeGreaterThan(0);
  expect(reconnects[0]).toBe(stale);
  await reload; // the reset event made the page reload
  await expect(A.page.locator("#responses > tr", { hasText: "Late" })).toHaveCount(1);
  await A.context.close();
});
