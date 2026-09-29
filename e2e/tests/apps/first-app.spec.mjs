// A1 — Tutorial "Your first Pagelove application" (docs/spec/apps.md §7.2):
// ACC-FA-1..4, and ACC-BJ-3 (the same flows with beta-js imported from a
// pagelike host instead of GitHub Pages).
//
// The document is taken verbatim from the docs snapshot: the final
// application (learn/your-first-app, lines 238-320) with its inline script
// replaced by the DOMSubscriber version (lines 329-359), as §6.3 prescribes.
import { test, expect } from "@playwright/test";
import { createSite } from "../../lib/pagelike.mjs";
import {
  hasResearch, dav, http, newSession, multipart, expect2xx, origin, realErrors, ensureBetaJsSite,
} from "../../lib/apps.mjs";
import { firstAppDocument, withoutRules } from "../../lib/fixtures.mjs";

test.skip(!hasResearch(), "research/ (upstream apps and docs snapshot) not available");
test.describe.configure({ mode: "serial" });

const struck = (li) => li.evaluate((el) => getComputedStyle(el).textDecorationLine);

/** Runs FA-1..3 on `site`; betajs() names a pagelike host serving beta-js. */
function firstAppFlows(site, { betajs = () => undefined, idPrefix = "ACC-FA", tag = "@tierA" } = {}) {
  test(`${idPrefix}-1 tick persists`, { tag }, async ({ browser }) => {
    const A = await newSession(browser, { betajsSite: betajs() });
    const B = await newSession(browser, { betajsSite: betajs() });
    const options = A.page.waitForResponse((r) => r.request().method() === "OPTIONS");
    await A.page.goto(origin(site) + "/");
    const opt = await options;
    expect(new URL(opt.url()).pathname).toBe("/");
    expect(opt.status()).toBe(207);
    const parts = multipart(await opt.text(), opt.headers()["content-type"]);
    const li = parts.find((p) => p.headers["content-range"] === "selector #todo-list li");
    const ul = parts.find((p) => p.headers["content-range"] === "selector #todo-list");
    expect(li, "a part for #todo-list li").toBeTruthy();
    expect(li.headers.allow).toMatch(/\bPUT\b/);
    expect(li.headers.allow).toMatch(/\bDELETE\b/);
    expect(ul, "a part for #todo-list").toBeTruthy();
    expect(ul.headers.allow).toMatch(/\bPOST\b/);

    const since = A.net.mark();
    await A.page.locator("#todo-list li", { hasText: "Get Milk" }).locator("input").check();
    const put = await A.net.waitFor({ method: "PUT", since });
    await A.net.settle({ since });
    const writes = A.net.writes({ since });
    expect(writes).toHaveLength(1);
    expect(put.path).toBe("/");
    expect(put.req.range).toBe("selector=#todo-list > li:nth-child(1)");
    expect2xx(put.status, "PUT");

    await A.page.reload();
    await B.page.goto(origin(site) + "/index.html");
    for (const p of [A.page, B.page]) {
      const first = p.locator("#todo-list li").first();
      await expect(first.locator("input")).toBeChecked();
      expect(await struck(first)).toContain("line-through");
    }
    expect(realErrors(A.errors)).toEqual([]);
    if (betajs()) {
      // The modules came from the pagelike host, with its cors.html headers.
      const mods = A.net.find({ host: "pagelove.github.io" });
      expect(mods.length).toBeGreaterThanOrEqual(5);
      for (const m of mods) {
        expect(m.status, m.url).toBe(200);
        expect(m.res["access-control-allow-origin"], m.url).toBe("*");
        expect(m.res["content-type"], m.url).toMatch(/^text\/javascript/);
      }
    }
    await A.context.close();
    await B.context.close();
  });

  test(`${idPrefix}-2 add an item`, { tag }, async ({ browser }) => {
    const A = await newSession(browser, { betajsSite: betajs() });
    await A.page.goto(origin(site) + "/");
    await A.net.waitFor({ method: "OPTIONS" });
    const since = A.net.mark();
    await A.page.fill("#new-item", "Bread");
    await A.page.click('button[commandfor="todo-list"]');
    const post = await A.net.waitFor({ method: "POST", since });
    expect(post.path).toBe("/");
    expect(post.req.range).toBe("selector=#todo-list");
    expect2xx(post.status, "POST");
    const bread = A.page.locator("#todo-list li", { hasText: "Bread" });
    await expect(bread).toHaveCount(1);
    await expect(bread.locator("button.delete")).toHaveCount(1);
    expect(A.net.writes({ since })).toHaveLength(1);

    const since2 = A.net.mark();
    await bread.locator("input").check();
    const put = await A.net.waitFor({ method: "PUT", since: since2 });
    expect(put.req.range).toBe("selector=#todo-list > li:nth-child(4)");
    expect2xx(put.status, "PUT");
    await A.page.reload();
    await expect(A.page.locator("#todo-list li", { hasText: "Bread" }).locator("input")).toBeChecked();
    expect(realErrors(A.errors)).toEqual([]);
    await A.context.close();
  });

  test(`${idPrefix}-3 delete an item`, { tag }, async ({ browser }) => {
    const A = await newSession(browser, { betajsSite: betajs() });
    await A.page.goto(origin(site) + "/");
    await A.net.waitFor({ method: "OPTIONS" });
    const since = A.net.mark();
    await A.page.locator("#todo-list li", { hasText: "Buy Eggs" }).locator("button.delete").click();
    const del = await A.net.waitFor({ method: "DELETE", since });
    expect(del.path).toBe("/");
    expect(del.req.range).toBe("selector=#todo-list > li:nth-child(2)");
    expect2xx(del.status, "DELETE");
    await A.net.settle({ since });
    expect(A.net.writes({ since })).toHaveLength(1);
    await A.page.reload();
    await expect(A.page.locator("#todo-list li", { hasText: "Buy Eggs" })).toHaveCount(0);
    await expect(A.page.locator("#todo-list li", { hasText: "Make pancakes" })).toHaveCount(1);
    expect(realErrors(A.errors)).toEqual([]);
    await A.context.close();
  });
}

test.describe("A1 first app", () => {
  test.beforeAll(async () => {
    const doc = firstAppDocument();
    const key = createSite("first-app");
    await dav("first-app", key).putOk("/index.html", doc);
    const key2 = createSite("first-app-norules");
    await dav("first-app-norules", key2).putOk("/index.html", withoutRules(doc));
  });

  firstAppFlows("first-app");

  test("ACC-FA-4 no rule, no write", { tag: "@tierA" }, async ({ browser }) => {
    const A = await newSession(browser);
    const options = A.page.waitForResponse((r) => r.request().method() === "OPTIONS");
    await A.page.goto(origin("first-app-norules") + "/");
    const opt = await options;
    const text = opt.status() === 207 ? await opt.text() : "";
    const parts = multipart(text, opt.headers()["content-type"]);
    expect(parts.filter((p) => /\b(PUT|POST|DELETE)\b/.test(p.headers.allow || ""))).toEqual([]);
    const since = A.net.mark();
    await A.page.locator("#todo-list li").first().locator("input").check();
    await expect.poll(() => A.errors.some((e) => /TypeError/.test(e)), { timeout: 5000 }).toBe(true);
    await A.page.waitForTimeout(300);
    expect(A.net.writes({ since })).toHaveLength(0);
    // The same write sent directly is refused with the 401 error document.
    const r = await http("first-app-norules")("PUT", "/", {
      headers: { Range: "selector=#todo-list > li:nth-child(1)", "Content-Type": "text/html" },
      body: '<li><input type="checkbox" checked="">Get Milk</li>',
    });
    expect(r.status).toBe(401);
    expect(r.headers.get("content-type")).toMatch(/^text\/html/);
    expect(r.headers.get("www-authenticate")).toBeNull();
    expect(r.text).toContain('itemtype="https://pagelove.org/1.0/Error"');
    expect(r.text).toContain('<dd itemprop="resource">/index.html</dd>');
    await A.context.close();
  });
});

// ACC-BJ-3 — beta-js imported cross-origin from a pagelike host: the
// `betajs` site, installed with beta-js's own sync script (§6.3), serves the
// modules with CORS from its cors.html processor. Tier A per §6.1.
test.describe("ACC-BJ-3 beta-js from a pagelike host", () => {
  test.beforeAll(async () => {
    const key = createSite("first-app-bj3");
    await dav("first-app-bj3", key).putOk("/index.html", firstAppDocument());
    ensureBetaJsSite("betajs");
  });
  firstAppFlows("first-app-bj3", { betajs: () => "betajs", idPrefix: "ACC-BJ-3/FA", tag: "@tierA" });
});
