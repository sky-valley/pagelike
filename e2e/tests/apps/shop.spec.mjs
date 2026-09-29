// A7 — pagelove-shop @ d887054 (docs/spec/apps.md §7.7): ACC-SH-1..6,
// ACC-SH-8..10 (tier B) and ACC-UP-3 (worker tests + order document).
// ACC-SH-7 (checkout end to end through the celld worker and a Stripe stub
// behind TLS) is tier C and not run here (docs/compat/acceptance.md).
//
// Installed as §6.3 prescribes: ops/deploy-pagelove.sh with the repository's
// manifests (exactly ops/pagelove-files.txt, the collections of
// pagelove-directories.txt and the seed), then ops/deploy-admin-password.sh
// with the password "test". private/admin.example.html is not installed.
import { test, expect } from "@playwright/test";
import { createSite } from "../../lib/pagelike.mjs";
import {
  hasResearch, upstream, dav, http, newSession, expect2xx, origin, realErrors, deployShop, deployShopAdminPassword,
} from "../../lib/apps.mjs";
import { mkdtempSync, cpSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { spawnSync } from "node:child_process";

test.skip(!hasResearch(), "research/ (upstream apps) not available");
test.describe.configure({ mode: "serial" });

const SITE = "shop";
const BASIC = "Basic " + Buffer.from("owner:test").toString("base64");
const ADMIN = { username: "owner", password: "test" };
let key;

test.beforeAll(async () => {
  key = createSite(SITE, { defaultGet: "allow" });
  const d = deployShop(SITE, key, mkdtempSync(join(tmpdir(), "shop-backup-")));
  if (d.status !== 0) throw new Error(d.out);
  const a = deployShopAdminPassword(SITE, key, "test", mkdtempSync(join(tmpdir(), "shop-admin-")));
  if (a.status !== 0) throw new Error(a.out);
});

/** A browser session. `admin` sends the Basic credential with every request
 * of the origin, which is what the shop assumes a browser does once the
 * shopkeeper has answered the prompt (R-APPS-14: "browsers then attach the
 * credential to later same-origin requests"). Chromium only attaches it
 * pre-emptively below the challenged directory (/admin/), so the shop's
 * writes to /images/* and /data/* would go out without it (observed: 401
 * from the shop's own trigger); see docs/compat/acceptance.md, ACC-SH-5.
 * `credentials` answers the real challenge instead (ACC-SH-4). */
async function shopSession(browser, { admin = false, credentials } = {}) {
  const opts = {};
  if (admin) opts.extraHTTPHeaders = { Authorization: BASIC };
  if (credentials) opts.httpCredentials = credentials;
  return newSession(browser, opts);
}

const prompts = (net) => net.find({ pred: (e) => e.status === 401 && e.res?.["www-authenticate"] });

/** The worker's own order document for a sample cart (ACC-UP-3, SH-8/9). */
async function sampleOrder(id, extra = "") {
  const { orderDocument } = await import(pathToFileURL(upstream("pagelove-shop", "worker", "src", "index.js")).href);
  const cart = { currency: "GBP", subtotal: 2800, shipping: 495, amountTotal: 3295,
    lines: [{ sku: "PL-TEE", slug: "tee", name: "Pagelove T-shirt", variant: "M", unitPrice: 2800, quantity: 1, lineTotal: 2800 }] };
  const input = { requestId: id, email: "ada@example.com", customerName: "Ada Lovelace", line1: "1 Analytical St", city: "London", postcode: "N1 1AA", country: "United Kingdom" };
  let html = orderDocument(input, cart, "cs_test_" + id, "2026-09-29T10:00:00.000Z");
  if (extra) html = html.replace("<ul itemprop=\"lines\">", extra + "<ul itemprop=\"lines\">");
  return html;
}

test("ACC-SH-1 catalogue and product pages", { tag: "@tierB" }, async ({ browser }) => {
  const A = await shopSession(browser);
  await A.page.goto(origin(SITE) + "/");
  const names = await A.page.locator(".grid a.card h2").allTextContents();
  expect(names).toHaveLength(6);
  expect(names).toEqual([...names].sort());
  const hrefs = await A.page.locator(".grid a.card").evaluateAll((as) => as.map((a) => a.getAttribute("href")));
  for (const h of hrefs) expect(h).toMatch(/^\/products\/[a-z-]+\.html$/);
  await expect(A.page.locator(".grid .thumb img")).toHaveCount(0); // emoji fallbacks (W-15)
  await expect(A.page.locator(".grid .thumb").first()).not.toHaveText("");
  await expect(A.page.locator("#chrome nav")).not.toContainText("Orders");

  await A.page.goto(origin(SITE) + "/products/tee.html");
  expect(A.net.find({ path: "/products/tee.html" })[0].status).toBe(200);
  await expect(A.page.locator("[data-product-page] h1")).toHaveText("Pagelove T-shirt");
  await expect(A.page.locator('select[aria-label="Variant"] option')).toHaveCount(5);
  await expect(A.page.getByRole("button", { name: "Add to basket" })).toBeVisible();
  const src = (await http(SITE)("GET", "/products/tee.html")).text;
  expect(src).toContain('itemtype="https://shop.example/Product"');
  expect(src).not.toContain("p:stamp");

  await A.page.goto(origin(SITE) + "/products/nope.html");
  expect(A.net.find({ path: "/products/nope.html" })[0].status).toBe(200);
  await expect(A.page.locator("[data-product-page] h1")).toHaveText("Not found");
  expect(prompts(A.net), "no page triggers a credential prompt (W-16)").toEqual([]);
  expect(realErrors(A.errors)).toEqual([]);
  await A.context.close();
});

async function addToBasket(page, slug, { variant, qty = 1 } = {}) {
  await page.goto(origin(SITE) + `/products/${slug}.html`);
  if (variant) await page.selectOption('select[aria-label="Variant"]', variant);
  await page.fill('input[aria-label="Quantity"]', String(qty));
  await page.getByRole("button", { name: "Add to basket" }).click();
  await expect(page.locator("[data-product-page] .note").first()).toContainText("Added.");
}

let shopperA;

test("ACC-SH-2 baskets are per session", { tag: "@tierB" }, async ({ browser }) => {
  const A = await shopSession(browser);
  const B = await shopSession(browser);
  const since = A.net.mark();
  await addToBasket(A.page, "tee", { variant: "M", qty: 2 });
  await addToBasket(A.page, "mug");
  await addToBasket(B.page, "cap");
  const gets = A.net.find({ method: "GET", path: "/basket.html", range: "selector=#basket", since });
  const puts = A.net.find({ method: "PUT", path: "/basket.html", range: "selector=#basket", since });
  // Each add is a read-modify-write: a GET of #basket, then one PUT (pages
  // also GET #basket on load to paint the count).
  expect(puts).toHaveLength(2);
  for (const p of puts) expect(gets.some((g) => g.t <= p.t), "a GET precedes each PUT").toBe(true);
  for (const e of [...gets, ...puts]) {
    expect2xx(e.status, `${e.method} #basket`);
    expect(e.res["cache-control"]).toMatch(/\bprivate\b/);
  }
  await A.page.goto(origin(SITE) + "/basket.html");
  await expect(A.page.locator("[data-basket-view] .line")).toHaveCount(2);
  await expect(A.page.locator("[data-basket-view] .line").first()).toContainText("M");
  await B.page.goto(origin(SITE) + "/basket.html");
  await expect(B.page.locator("[data-basket-view] .line")).toHaveCount(1);
  await expect(B.page.locator("[data-basket-view] .line")).toContainText("Cap");
  // The stored document keeps its default; bindings see no session copies.
  expect(await dav(SITE, key).text("/basket.html")).toContain('<ul id="basket" p:transient></ul>');
  const home = (await http(SITE)("GET", "/")).text;
  expect(home).not.toContain("data-qty");
  expect(realErrors(A.errors)).toEqual([]);
  shopperA = A;
  await B.context.close();
});

test("ACC-SH-3 basket identity and reset", { tag: "@tierB" }, async () => {
  const A = shopperA;
  const req = A.page.request; // A's session cookie
  const bad = await req.put(origin(SITE) + "/basket.html", { headers: { Range: "selector=#basket", "Content-Type": "text/html" }, data: '<ul id="other"></ul>' });
  expect(bad.status()).toBe(422);
  const still = await req.get(origin(SITE) + "/basket.html", { headers: { Range: "selector=#basket" } });
  expect(await still.text()).toContain('data-sku="PL-TEE"');
  const del = await req.delete(origin(SITE) + "/basket.html", { headers: { Range: "selector=#basket" } });
  expect2xx(del.status(), "DELETE #basket");
  const after = await req.get(origin(SITE) + "/basket.html", { headers: { Range: "selector=#basket" } });
  expect(after.status()).toBe(206);
  expect((await after.text()).trim()).toBe('<ul id="basket"></ul>');
  await A.context.close();
});

test("ACC-SH-4 Basic-auth admin gate", { tag: "@tierB" }, async ({ browser }) => {
  // Without credentials the browser receives the challenge (a desktop browser
  // shows its native prompt; headless Chromium cancels the navigation).
  const A = await shopSession(browser);
  await A.page.goto(origin(SITE) + "/admin/index.html").catch((e) => expect(e.message).toContain("ERR_INVALID_AUTH_CREDENTIALS"));
  const challenge = await A.net.waitFor({ path: "/admin/index.html" });
  expect(challenge.status).toBe(401);
  expect(challenge.res["www-authenticate"]).toBe('Basic realm="Pagelove Shop admin"');
  await A.context.close();

  const ok = await shopSession(browser, { credentials: ADMIN }); // answers the native prompt
  const r2 = await ok.page.goto(origin(SITE) + "/admin/index.html");
  expect(r2.status()).toBe(200);
  await expect(ok.page.locator("#chrome nav")).toContainText("Orders");
  await expect(ok.page.locator("#chrome nav")).toContainText("Products");
  await expect(ok.page.locator("#chrome nav")).toContainText("Settings");
  await ok.context.close();

  const wrong = await shopSession(browser, { credentials: { username: "owner", password: "nope" } });
  const r3 = await wrong.page.goto(origin(SITE) + "/admin/index.html");
  expect(r3.status()).toBe(401);
  await wrong.context.close();

  expect((await http(SITE)("HEAD", "/admin/index.html")).status).toBe(401);
  expect((await http(SITE)("GET", "/admin/index.html", { headers: { Authorization: BASIC } })).status).toBe(200);
});

test("ACC-SH-5 product admin", { tag: "@tierB" }, async ({ browser }) => {
  const A = await shopSession(browser, { admin: true });
  A.page.on("dialog", (d) => d.accept());
  await A.page.goto(origin(SITE) + "/admin/products.html");
  let since = A.net.mark();
  await A.page.click("[data-new-product]");
  // The editor renders each field as <div><label>…</label><input|textarea></div>.
  const field = (label) => A.page.locator(`.editor-scrim label:text-is("${label}") + :is(input, textarea)`);
  await field("Name").fill("Pagelove Poster");
  await field("SKU").fill("PL-POSTER");
  await field("Price in pence").fill("1500");
  await field("Category").fill("print");
  await field("Variants (comma separated)").fill("A2");
  await field("Description").fill("A selector, framed.");
  await A.page.locator('.editor-scrim input[type="file"]').setInputFiles({ name: "poster.webp", mimeType: "image/webp", buffer: Buffer.from("RIFF\x10\x00\x00\x00WEBPVP8 poster") });
  const img = await A.net.waitFor({ method: "PUT", path: /^\/images\/pagelove-poster-[a-z0-9]+\.webp$/, since });
  expect(img.req["content-type"]).toBe("image/webp");
  expect2xx(img.status, "image upload");
  await expect(A.page.locator(".editor-scrim .note", { hasText: "Uploaded." })).toHaveCount(1);
  await Promise.all([A.page.waitForEvent("load"), A.page.getByRole("button", { name: "Create product" }).click()]);
  const put = A.net.find({ method: "PUT", path: "/data/products/pagelove-poster.html", since })[0];
  expect(put.req["content-type"]).toBe("text/html; charset=utf-8");
  expect2xx(put.status, "product PUT");

  const home = await (await shopSession(browser)).page;
  await home.goto(origin(SITE) + "/");
  const names = await home.locator(".grid a.card h2").allTextContents();
  expect(names).toContain("Pagelove Poster");
  expect(names).toEqual([...names].sort());
  await expect(home.locator('.grid a.card[href="/products/pagelove-poster.html"] img')).toHaveAttribute("src", img.path);

  // Edit it, then delete it.
  since = A.net.mark();
  await A.page.locator(".line-admin", { hasText: "Pagelove Poster" }).getByRole("button", { name: "Edit" }).click();
  await field("Price in pence").fill("1800");
  await Promise.all([A.page.waitForEvent("load"), A.page.getByRole("button", { name: "Save changes" }).click()]);
  expect2xx(A.net.find({ method: "PUT", path: "/data/products/pagelove-poster.html", since })[0].status, "edit");
  expect(await dav(SITE, key).text("/data/products/pagelove-poster.html")).toContain('itemprop="price" content="1800"');
  since = A.net.mark();
  await Promise.all([A.page.waitForEvent("load"), A.page.locator(".line-admin", { hasText: "Pagelove Poster" }).getByRole("button", { name: "Delete" }).click()]);
  expect2xx(A.net.find({ method: "DELETE", path: "/data/products/pagelove-poster.html", since })[0].status, "delete");
  expect((await dav(SITE, key).get("/data/products/pagelove-poster.html")).status).toBe(404);

  // Without the credential the trigger refuses; a duplicate sku breaks uniqueness.
  const doc = (await dav(SITE, key).text("/data/products/mug.html")).replace(/content="mug"/g, 'content="mug2"').replace(/id="product-mug"/, 'id="product-mug2"');
  const anon = await http(SITE)("PUT", "/data/products/mug2.html", { headers: { "Content-Type": "text/html; charset=utf-8" }, body: doc });
  expect(anon.status).toBe(401);
  expect(anon.text).toContain("Admin credential required to change shop data");
  const dup = await http(SITE)("PUT", "/data/products/mug2.html", { headers: { "Content-Type": "text/html; charset=utf-8", Authorization: BASIC }, body: doc });
  expect(dup.status, "a second product with sku PL-MUG").toBe(422);
  expect(dup.text).toMatch(/uniqueness/i);
  expect(realErrors(A.errors)).toEqual([]);
  await A.context.close();
});

test("ACC-SH-6 settings stamped, source private", { tag: "@tierB" }, async ({ browser }) => {
  const A = await shopSession(browser, { admin: true });
  await A.page.goto(origin(SITE) + "/admin/settings.html");
  const since = A.net.mark();
  await A.page.fill('[data-settings-form] input[name="checkoutEndpoint"]', "https://checkout.example");
  await A.page.click('[data-settings-form] button[type="submit"]');
  await expect(A.page.locator("[data-settings-note]")).toContainText("Settings saved.");
  const put = A.net.find({ method: "PUT", path: "/data/settings/shop.html", since })[0];
  expect(put.req.range).toBe('selector=[itemprop="checkoutEndpoint"]');
  expect2xx(put.status, "settings PUT");
  const checkout = await http(SITE)("GET", "/checkout.html");
  expect(checkout.status).toBe(200);
  expect(checkout.text).toContain('itemtype="https://shop.example/ShopSettings"');
  expect(checkout.text).toContain('<meta itemprop="checkoutEndpoint" content="https://checkout.example">');
  expect((await http(SITE)("GET", "/data/settings/shop.html")).status).toBe(401);
  await A.context.close();
});

test("ACC-SH-8 order shape holds on dav", { tag: "@tierB" }, async () => {
  const bad = await dav(SITE, key).put("/data/orders/ord-bad-0001.html", await sampleOrder("ord-bad-0001", "<script>alert(1)</script>"));
  expect(bad.status).toBe(422);
  expect(await bad.text()).toMatch(/Shape|Constraint/);
  expect((await dav(SITE, key).get("/data/orders/ord-bad-0001.html")).status).toBe(404);
  const good = await dav(SITE, key).put("/data/orders/ord-good-0001.html", await sampleOrder("ord-good-0001"), { "If-None-Match": "*" });
  expect2xx(good.status, "order via dav");
  expect((await dav(SITE, key).put("/data/orders/ord-good-0001.html", await sampleOrder("ord-good-0001"), { "If-None-Match": "*" })).status).toBe(412);
});

test("ACC-SH-9 admin order status", { tag: "@tierB" }, async ({ browser }) => {
  const id = "ord-status-0001";
  expect2xx((await dav(SITE, key).put(`/data/orders/${id}.html`, await sampleOrder(id))).status);
  const A = await shopSession(browser, { admin: true });
  await A.page.goto(origin(SITE) + `/admin/orders/${id}.html`);
  await expect(A.page.locator("[data-admin-order] .admin-head .badge")).toHaveText("Awaiting payment");
  for (const [button, badge] of [["Mark as paid", "Paid"], ["Mark as fulfilled", "Fulfilled"]]) {
    const since = A.net.mark();
    await Promise.all([A.page.waitForEvent("load"), A.page.getByRole("button", { name: button }).click()]);
    const put = A.net.find({ method: "PUT", path: `/data/orders/${id}.html`, since })[0];
    expect(put.req.range).toBe('selector=[itemprop="paymentStatus"]');
    expect2xx(put.status, button);
    await expect(A.page.locator("[data-admin-order] .admin-head .badge")).toHaveText(badge);
  }
  const shipped = await http(SITE)("PUT", `/data/orders/${id}.html`, { headers: { Authorization: BASIC, Range: 'selector=[itemprop="paymentStatus"]', "Content-Type": "text/html" },
    body: '<meta itemprop="paymentStatus" content="shipped">' });
  expect(shipped.status, "a value outside the PaymentStatus enum").toBe(422);
  // The public order page stamps the order although /data/orders/* is unreadable.
  const conf = await http(SITE)("GET", `/orders/${id}.html`);
  expect(conf.status).toBe(200);
  expect(conf.text).toContain(`content="${id}"`);
  expect((await http(SITE)("GET", `/data/orders/${id}.html`)).status).toBe(401);
  expect(realErrors(A.errors)).toEqual([]);
  await A.context.close();
});

test("ACC-SH-10 private stays private", { tag: "@tierB" }, async () => {
  const req = http(SITE);
  expect((await req("GET", "/private/admin.html")).status).toBe(401);
  expect((await req("GET", "/private/admin.html", { headers: { Authorization: BASIC } })).status).toBe(401);
  expect((await req("PUT", "/private/admin.html", { headers: { Authorization: BASIC, "Content-Type": "text/html" }, body: "<p>x</p>" })).status).toBe(401);
  expect((await dav(SITE, key).get("/private/admin.example.html")).status, "the placeholder is not installed").toBe(404);
});

test("ACC-UP-3 shop worker tests and its order document on pagelike", { tag: "@tierB" }, async () => {
  // A scratch copy of the repository (the worker tests import ../../site/js).
  const repo = mkdtempSync(join(tmpdir(), "shop-repo-"));
  cpSync(upstream("pagelove-shop"), repo, { recursive: true, filter: (s) => !/\/(\.git|node_modules)(\/|$)/.test(s) });
  // worker/test/worker.test.js imports ../../js/shop.mjs, the path from before
  // the app moved under site/ (it fails as committed at d887054). The scratch
  // copy also places site/js at the repository root; the test is unchanged.
  cpSync(join(repo, "site", "js"), join(repo, "js"), { recursive: true });
  const dir = join(repo, "worker");
  const r = spawnSync("node", ["--test"], { cwd: dir, encoding: "utf8", timeout: 120_000 });
  const out = (r.stdout + r.stderr).replace(/\x1b\[[0-9;]*m/g, "");
  const count = (w) => Number((out.match(new RegExp(`\\b${w} (\\d+)\\s*$`, "m")) || [])[1]);
  test.info().annotations.push({ type: "node --test", description: `tests ${count("tests")}, pass ${count("pass")}, fail ${count("fail")}` });
  expect(r.status, out).toBe(0);
  expect(count("fail")).toBe(0);
  // The worker's orderDocument() output passes the shop's schemas and closed shape over dav.
  const id = "ord-worker-0001";
  expect2xx((await dav(SITE, key).put(`/data/orders/${id}.html`, await sampleOrder(id), { "If-None-Match": "*", "Content-Type": "text/html; charset=utf-8" })).status);
});
