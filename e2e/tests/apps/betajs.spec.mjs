// A9/A10 — beta-js @ c204746 and pagelove-primitives @ e73986d
// (docs/spec/apps.md §7.9, §8.2, §8.3): ACC-BJ-1, ACC-BJ-2, ACC-BJ-4,
// ACC-BJ-5 (= ACC-RC-7), ACC-RC-8 (= ACC-BJ-4), ACC-UP-1, ACC-UP-2.
// ACC-BJ-3 lives with the first app (first-app.spec.mjs).
import { test, expect } from "@playwright/test";
import { createSite } from "../../lib/pagelike.mjs";
import {
  hasResearch, upstream, dav, http, newSession, expect2xx, origin, davOrigin, multipart, codeBlocks, docPage,
  betaJsWorkingCopy, runBetaJsScript, ensureSite, npmDeps, install, realErrors, repoRoot,
} from "../../lib/apps.mjs";
import { cpSync, symlinkSync } from "node:fs";
import { join } from "node:path";
import { spawnSync } from "node:child_process";

test.skip(!hasResearch(), "research/ (upstream apps and docs snapshot) not available");
test.describe.configure({ mode: "serial" });

/** A scratch beta-js copy with its locked devDependencies (jsdom). */
function betaJsWithDeps() {
  const nm = npmDeps("beta-js", { lockDir: upstream("beta-js") });
  if (!nm) return null;
  const dir = betaJsWorkingCopy();
  symlinkSync(nm, join(dir, "node_modules"));
  return dir;
}

test("ACC-BJ-1 beta-js unit tests", { tag: "@tierA" }, async () => {
  test.setTimeout(300_000);
  const dir = betaJsWithDeps();
  test.skip(!dir, "npm registry unreachable: cannot install beta-js devDependencies");
  const r = spawnSync("npm", ["test"], { cwd: dir, encoding: "utf8", timeout: 120_000 });
  const out = r.stdout.replace(/\x1b\[[0-9;]*m/g, ""); // the runner colours its report under Playwright
  const count = (what) => Number((out.match(new RegExp(`\\b${what} (\\d+)\\s*$`, "m")) || [])[1]);
  test.info().annotations.push({ type: "npm test", description: `tests ${count("tests")}, pass ${count("pass")}, fail ${count("fail")}` });
  expect(r.status, r.stdout + r.stderr).toBe(0);
  expect(count("tests")).toBeGreaterThan(0);
  expect(count("fail")).toBe(0);
});

test("ACC-BJ-2 beta-js self-hosted on pagelike", { tag: "@tierB" }, async () => {
  test.setTimeout(180_000);
  const dir = betaJsWorkingCopy();
  const key = ensureSite("betajs-verify");
  const sync = runBetaJsScript(dir, "sync-webdav.sh", "betajs-verify", key, ["--all"]);
  expect(sync.status, sync.out).toBe(0);
  const env = { PAGELOVE_PUBLIC_URL: `${origin("betajs-verify")}/` };
  const ok = runBetaJsScript(dir, "verify-deploy.sh", "betajs-verify", key, [], env);
  expect(ok.status, ok.out).toBe(0);
  expect(ok.out).toMatch(/Verified \d+ module\(s\): served, readable cross-origin, and parsing\./);
  const modules = (ok.out.match(/^ {2}OK {5}/gm) || []).length;
  expect(modules).toBeGreaterThanOrEqual(6);
  // Byte-for-byte: the processor passes the stored module through untouched.
  const served = Buffer.from(await (await fetch(origin("betajs-verify") + "/pagelove/primitives.mjs")).arrayBuffer());
  expect(served.equals(Buffer.from(await (await fetch(davOrigin("betajs-verify") + "/pagelove/primitives.mjs", { headers: { Authorization: `Bearer ${key}` } })).arrayBuffer()))).toBe(true);

  // Without cors.html pagelike adds no blanket CORS, so the check fails.
  const key2 = ensureSite("betajs-nocors");
  expect(runBetaJsScript(dir, "sync-webdav.sh", "betajs-nocors", key2, ["--all"]).status).toBe(0);
  expect((await dav("betajs-nocors", key2).delete("/cors.html")).status).toBeLessThan(300);
  const bad = runBetaJsScript(dir, "verify-deploy.sh", "betajs-nocors", key2, [], { PAGELOVE_PUBLIC_URL: `${origin("betajs-nocors")}/` });
  expect(bad.status).toBe(1);
  expect(bad.out).toContain("no Access-Control-Allow-Origin; a cross-origin import will be blocked");
});

/** The widget recipe page (D-R-WID) assembled from its code blocks. The
 * read/render snippet and the re-render listener are printed as two module
 * scripts, but modules do not share scope (the listener would throw
 * `renderChart is not defined`), so their bodies go into one module. */
function widgetPage(path, { cells = false } = {}) {
  const b = codeBlocks(docPage("recipes_embedding-a-third-party-widget")).map((x) => x.code);
  let [data, libs, read, rerender] = [b[0], b[1], b[2], b[3]];
  // The non-table variant: the same records as <div>/<span> instead of
  // <tr>/<td> (see the ACC-BJ-4 notes on beta-js's table-cell parsing).
  if (cells) data = data.replace(/<table id="sales-data">\s*<tbody>/, '<div id="sales-data">').replace(/<\/tbody>\s*<\/table>/, "</div>")
    .replace(/<tr /g, "<div ").replace(/<\/tr>/g, "</div>").replace(/<td /g, "<span ").replace(/<\/td>/g, "</span>");
  const inner = (s) => s.replace(/^\s*<script type="module">\n?/, "").replace(/<\/script>\s*$/, "");
  return `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Sales</title>
${libs}
</head><body>
${data}
<script type="module">
${inner(read)}
${inner(rerender)}
</script>
<!-- An SSE subscription is never granted by the default-GET mode (D-AR
     §Conflict resolution, R-SSE-3), so the live page needs a read rule. -->
<div hidden itemscope itemtype="https://pagelove.org/AuthorizationRule">
  <meta itemprop="actor" content="*">
  <meta itemprop="resource" content="${path}">
  <meta itemprop="method" content="GET">
  <meta itemprop="action" content="allow">
</div>
<div hidden itemscope itemtype="https://pagelove.org/AuthorizationRule">
  <meta itemprop="actor" content="*">
  <meta itemprop="resource" content="${path}">
  <meta itemprop="method" content="PUT">
  <meta itemprop="selector" content='[itemtype="https://example.com/SalesRecord"] [itemprop="revenue"]'>
  <meta itemprop="action" content="allow">
</div>
</body></html>
`;
}

async function widgetSession(browser, S, path) {
  const A = await newSession(browser);
  // The recipe's placeholder charting library: a stub that records renders.
  await A.context.route("https://cdn.example.com/chart-library.min.js", (route) => route.fulfill({
    status: 200, headers: { "Content-Type": "text/javascript" },
    body: "window.__renders=[];window.ChartLibrary={render(canvas,cfg){window.__renders.push(cfg.datasets[0].data.slice())}};",
  }));
  await A.context.addInitScript(() => {
    window.__events = [];
    for (const t of ["PLMutation", "PLMutationApplied"]) document.addEventListener(t, (e) => window.__events.push({ type: t, selector: e.detail.selector }));
  });
  const stream = A.page.waitForResponse((r) => (r.request().headers().accept || "").includes("text/event-stream"));
  await A.page.goto(origin(S) + path);
  expect((await stream).status()).toBe(200);
  await expect.poll(() => A.page.evaluate(() => window.__renders.length)).toBe(1);
  expect(await A.page.evaluate(() => window.__renders[0])).toEqual([42000, 51000, 47000]);
  await A.page.waitForTimeout(300);
  return A;
}

const applied = (page) => page.evaluate(() => window.__events.filter((e) => e.type === "PLMutationApplied").map((e) => e.selector));

test("ACC-BJ-4 widget recipe with sse.mjs (= ACC-RC-8)", { tag: "@tierA" }, async ({ browser }) => {
  const S = "betajs-widget";
  const key = createSite(S);
  await dav(S, key).putOk("/widget.html", widgetPage("/widget.html"));
  await dav(S, key).putOk("/widget-div.html", widgetPage("/widget-div.html", { cells: true }));
  const sel = '[itemtype="https://example.com/SalesRecord"]:nth-child(2) [itemprop="revenue"]';
  const B = http(S); // another session

  // 1. The recipe as printed (a <table>): the server event reaches sse.mjs,
  // which dispatches PLMutation/PLMutationApplied with the write's selector.
  // beta-js@c204746 parses the event with DOMParser, which drops a <td>
  // outside a table (the reason pagelove-polls slices the payload, W-31), so
  // the cell arrives as bare text; that is a client limitation, recorded in
  // docs/compat/acceptance.md, not asserted either way here.
  const T = await widgetSession(browser, S, "/widget.html");
  expect2xx((await B("PUT", "/widget.html", { headers: { "Content-Type": "text/html", Range: `selector=${sel}` }, body: '<td itemprop="revenue">60000</td>' })).status);
  await expect.poll(() => applied(T.page), { timeout: 2000 }).toEqual([sel]);
  expect(realErrors(T.errors)).toEqual([]);
  await T.context.close();

  // 2. The same recipe with the records in <div>/<span>: every expectation.
  const A = await widgetSession(browser, S, "/widget-div.html");
  expect2xx((await B("PUT", "/widget-div.html", { headers: { "Content-Type": "text/html", Range: `selector=${sel}` }, body: '<span itemprop="revenue">60000</span>' })).status);
  await expect.poll(() => applied(A.page), { timeout: 2000 }).toEqual([sel]);
  await expect.poll(() => A.page.evaluate(() => window.__renders.at(-1))).toEqual([42000, 60000, 47000]);

  // A PLMutation listener that cancels keeps the old DOM.
  await A.page.evaluate(() => document.addEventListener("PLMutation", (e) => e.preventDefault()));
  expect2xx((await B("PUT", "/widget-div.html", { headers: { "Content-Type": "text/html", Range: `selector=${sel}` }, body: '<span itemprop="revenue">70000</span>' })).status);
  await expect.poll(() => A.page.evaluate(() => window.__events.filter((e) => e.type === "PLMutation").length), { timeout: 2000 }).toBe(2);
  await A.page.waitForTimeout(200);
  expect(await A.page.locator(sel).textContent()).toBe("60000");
  expect(await applied(A.page)).toHaveLength(1);

  // A write from A itself fires nothing in A (no echo to the originating session).
  const before = await A.page.evaluate(() => window.__events.length);
  const own = await A.page.evaluate(async (sel) => (await fetch("/widget-div.html", { method: "PUT", headers: { "Content-Type": "text/html", Range: `selector=${sel}` }, body: '<span itemprop="revenue">80000</span>' })).status, sel);
  expect2xx(own);
  await A.page.waitForTimeout(1000);
  expect(await A.page.evaluate(() => window.__events.length)).toBe(before);
  expect(realErrors(A.errors)).toEqual([]);
  await A.context.close();
});

/** The primitives recipe page (D-R-PRIM "complete example") at /about.html. */
function aboutPage() {
  const b = codeBlocks(docPage("recipes_reading-from-the-primitives-layer")).map((x) => x.code);
  const example = b.find((c) => c.includes("new PLDocument('/about.html')"));
  return `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>About</title></head><body>
<h1>About us</h1>
<p>We make things.</p>
${example}
<div hidden itemscope itemtype="https://pagelove.org/AuthorizationRule">
  <meta itemprop="actor" content="*"><meta itemprop="resource" content="/about.html">
  <meta itemprop="method" content="GET"><meta itemprop="selector" content="h1"><meta itemprop="action" content="allow">
</div>
<div hidden itemscope itemtype="https://pagelove.org/AuthorizationRule">
  <meta itemprop="actor" content="*"><meta itemprop="resource" content="/about.html">
  <meta itemprop="method" content="PUT"><meta itemprop="selector" content="h1"><meta itemprop="action" content="allow">
</div>
</body></html>
`;
}

test("ACC-BJ-5 primitives recipe (= ACC-RC-7)", { tag: "@tierA" }, async ({ browser }) => {
  const S = "betajs-prim";
  const key = createSite(S);
  await dav(S, key).putOk("/about.html", aboutPage());
  const A = await newSession(browser);
  const options = A.page.waitForResponse((r) => r.request().method() === "OPTIONS");
  await A.page.goto(origin(S) + "/about.html");
  const opt = await options;
  expect(new URL(opt.url()).pathname).toBe("/about.html");
  expect(opt.status()).toBe(207);
  const h1 = multipart(await opt.text(), opt.headers()["content-type"]).find((p) => p.headers["content-range"] === "selector h1");
  expect(h1, "a part with the space-form Content-Range (W-28)").toBeTruthy();
  expect(h1.headers.allow).toMatch(/\bGET\b/);
  expect(h1.headers.allow).toMatch(/\bPUT\b/);

  // The printed example binds a PLDocument for the *relative* URL, which
  // beta-js@c204746 does not treat as the current page, so it wires the
  // fetched copy rather than the live <h1> and logs nothing. The page's own
  // document (new PLDocument()) is wired, as pagelove.mjs does it.
  const MOD = "https://pagelove.github.io/beta-js/pagelove/primitives.mjs";
  const got = await A.page.evaluate(async (MOD) => {
    const { PLDocument } = await import(MOD);
    await new PLDocument().OPTIONS();
    const heading = document.querySelector("h1");
    for (let i = 0; i < 100 && typeof heading.GET !== "function"; i++) await new Promise((r) => setTimeout(r, 20));
    const fresh = await heading.GET();
    return { text: fresh.textContent, etag: heading.etag };
  }, MOD);
  expect(got.text).toBe("About us");
  expect(got.etag).toMatch(/^"/);
  // A HEAD of the same element (beta-js's lazy ETag fetch) agrees with the GET.
  const head = await http(S)("HEAD", "/about.html", { headers: { Range: "selector=h1" } });
  expect(head.headers.get("etag")).toBe(got.etag);

  const since = A.net.mark();
  const put = await A.page.evaluate(async (MOD) => {
    const { PLElement } = await import(MOD);
    const heading = document.querySelector("h1");
    const stale = heading.etag;
    heading.textContent = "About pagelike";
    const first = await new PLElement(location.href, heading).PUT();
    heading.etag = stale;
    heading.textContent = "About again";
    const second = await new PLElement(location.href, heading).PUT();
    return { first: first.status, second: second.status, stale };
  }, MOD);
  const puts = A.net.find({ method: "PUT", since });
  expect(puts).toHaveLength(2);
  expect(puts[0].req["if-match"]).toBe(got.etag);
  expect(puts[0].req.range).toMatch(/^selector=.*\bh1(:nth-child\(1\))?$/); // beta-js generates a positional selector
  expect(put.first).toBe(206);
  expect(puts[1].req["if-match"]).toBe(put.stale);
  expect(put.second).toBe(412);
  expect((await http(S)("GET", "/about.html", { headers: { Range: "selector=h1" } })).text).toBe("<h1>About pagelike</h1>");
  await A.context.close();
});

test("ACC-UP-1 beta-js SSE contract with server-produced payloads", { tag: "@tierB" }, async () => {
  test.setTimeout(300_000);
  const dir = betaJsWithDeps();
  test.skip(!dir, "npm registry unreachable: cannot install beta-js devDependencies");
  const S = "betajs-sse";
  const key = createSite(S);
  await dav(S, key).putOk("/page.html", `<!DOCTYPE html><html><body><ul id="list"></ul>
<div hidden itemscope itemtype="https://pagelove.org/AuthorizationRule"><meta itemprop="actor" content="*"><meta itemprop="resource" content="/page.html">
<meta itemprop="method" content="GET"><meta itemprop="method" content="PUT"><meta itemprop="method" content="POST"><meta itemprop="method" content="DELETE"><meta itemprop="method" content="MOVE">
<meta itemprop="action" content="allow"></div></body></html>`);
  cpSync(join(repoRoot, "e2e", "upstream-adapters", "beta-js", "pagelike-sse.test.mjs"), join(dir, "test", "pagelike-sse.test.mjs"));
  const r = spawnSync("node", ["--import", "./test/helpers/register.mjs", "--test", "test/pagelike-sse.test.mjs"],
    { cwd: dir, encoding: "utf8", timeout: 60_000, env: { ...process.env, PAGELIKE_DOC: origin(S) + "/page.html" } });
  expect(r.status, r.stdout + r.stderr).toBe(0);
  expect(r.stdout.replace(/\x1b\[[0-9;]*m/g, "")).toMatch(/\bpass 1\s*$/m);
});

test("ACC-UP-2 pagelove-primitives test page served by pagelike", { tag: "@tierB" }, async ({ browser }) => {
  const S = "primitives-test";
  const key = createSite(S);
  await install(S, key, upstream("pagelove-primitives"), { files: ["test.html", "test-sw.js", "index.mjs"] });
  const js = await http(S)("GET", "/test-sw.js");
  expect(js.headers.get("content-type")).toMatch(/^text\/javascript/);
  const A = await newSession(browser);
  await A.page.goto(origin(S) + "/test.html");
  await expect(A.page.locator("#test-results .summary")).toBeVisible({ timeout: 15_000 });
  const summary = await A.page.locator("#test-results .summary").textContent();
  const fails = await A.page.locator("#test-results .fail").allTextContents();
  test.info().annotations.push({ type: "summary", description: summary });
  expect(fails.filter((f) => !f.includes("passed,")), summary).toEqual([]);
  expect(summary).toMatch(/^\d+ passed, 0 failed/);
  await A.context.close();
});
