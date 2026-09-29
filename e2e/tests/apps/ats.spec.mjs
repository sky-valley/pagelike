// A6 — pagelove-ats "Hiring by Pagelove" @ 8f200fc (docs/spec/apps.md §7.6):
// ACC-AT-1..7, ACC-AT-9 (tier B) and ACC-AT-8 (tier C, outbox relay).
//
// Installed unmodified: every file under site/, default-GET deny (the
// README's lock-down). Staff identities are pagelike LOCAL ACCOUNTS with
// verified emails standing in for the OIDC provider (R-APPS-15):
// admin1@example.com, admin3@example.com (staff), bob@example.com (not staff).
// Test-only edits (recorded in docs/compat/app-changes.md): ACC-AT-4b removes
// rules/groups from admin/auth.html over dav; ACC-AT-8 points the outbox
// trigger's url at a local sink.
import { test, expect } from "@playwright/test";
import { createSite, addUser } from "../../lib/pagelike.mjs";
import { hasResearch, upstream, install, dav, http, newSession, expect2xx, origin, realErrors, multipart, sessionCookie } from "../../lib/apps.mjs";
import http_ from "node:http";

test.skip(!hasResearch(), "research/ (upstream apps) not available");
test.describe.configure({ mode: "serial" });

const SITE = "ats";
const PW = { admin1: "admin1-local-test-pw", admin3: "admin3-local-test-pw", bob: "bob-local-test-pw" };
let key;
let applicant; // candidate id created in ACC-AT-3
let cvPath;

test.beforeAll(async () => {
  key = createSite(SITE, { defaultGet: "deny" });
  await install(SITE, key, upstream("pagelove-ats", "site"));
  addUser(SITE, "admin1", { email: "admin1@example.com", verified: true, name: "Admin One", password: PW.admin1 });
  addUser(SITE, "admin3", { email: "admin3@example.com", verified: true, name: "Admin Three", password: PW.admin3 });
  addUser(SITE, "bob", { email: "bob@example.com", verified: true, name: "Bob", password: PW.bob });
});

async function signIn(page, user, to = "/admin/") {
  await page.goto(origin(SITE) + "/auth/login?redirect=" + encodeURIComponent(to));
  await page.fill('input[name="username"]', user);
  await page.fill('input[name="password"]', PW[user]);
  await Promise.all([page.waitForURL(origin(SITE) + to), page.click('button[type="submit"]')]);
}

test("ACC-AT-1 careers page", { tag: "@tierB" }, async ({ browser }) => {
  const A = await newSession(browser);
  await A.page.goto(origin(SITE) + "/");
  const roles = await A.net.waitFor({ method: "GET", path: "/roles.html", range: "selector=#roles-body" });
  expect(roles.query).toMatch(/^\?t=\d+$/);
  expect(roles.status).toBe(206);
  const body = await roles.response.text();
  expect(body.trim()).toMatch(/^<tbody id="roles-body">/);
  expect((body.match(/itemtype="https:\/\/pagelove.org\/Role"/g) || []).length).toBe(3);
  await expect(A.page.locator('a[href="/roles/founding-engineer/"]').first()).toBeVisible();
  await expect(A.page.locator('a[href="/roles/community-manager/"]').first()).toBeVisible();
  await expect(A.page.locator('a[href="/roles/product-designer/"]')).toHaveCount(0);
  expect(realErrors(A.errors)).toEqual([]);
  await A.context.close();
});

test("ACC-AT-2 role pages from one route", { tag: "@tierB" }, async ({ browser }) => {
  const A = await newSession(browser);
  const r = await A.page.goto(origin(SITE) + "/roles/founding-engineer/");
  expect(r.status()).toBe(200);
  await expect(A.page.locator("#role-view h1")).toHaveText("Founding Engineer");
  const html = (await http(SITE)("GET", "/roles/founding-engineer/")).text;
  expect(html).toMatch(/<table id="all-roles-data" hidden(="")?>\s*<tbody id="roles-body">/);
  expect(html).not.toContain("p:include");
  expect(html).not.toMatch(/xmlns:(p|example)/);
  await A.page.goto(origin(SITE) + "/roles/product-designer/"); // On Hold: the script sends the visitor home
  await A.page.waitForURL(origin(SITE) + "/");
  await A.page.goto(origin(SITE) + "/roles/nope/");
  await expect(A.page.locator("#role-view")).toContainText("Role not found");
  expect(realErrors(A.errors)).toEqual([]);
  await A.context.close();
});

test("ACC-AT-3 apply anonymously", { tag: "@tierB" }, async ({ browser }) => {
  const A = await newSession(browser);
  await A.page.goto(origin(SITE) + "/apply.html?role=R-001");
  await expect(A.page.locator('#rolePills .pill.active')).toHaveCount(1);
  await A.page.fill('#applyForm input[name="name"]', "Grace Hopper");
  await A.page.fill('#applyForm input[name="email"]', "grace@example.com");
  const pdf = Buffer.concat([Buffer.from("%PDF-1.7\n"), Buffer.alloc(1024 * 1024 - 9, 0x20)]);
  await A.page.setInputFiles('#cvDrop input[type="file"]', { name: "grace-cv.pdf", mimeType: "application/pdf", buffer: pdf });
  await A.page.fill('#applyForm textarea[name="intro"]', "Compilers, mostly.");
  const since = A.net.mark();
  await A.page.click("#submitBtn");
  await expect(A.page.locator("#successView")).toBeVisible();
  const put = A.net.find({ method: "PUT", path: /^\/uploads\/C-[a-z0-9]+-cv\.pdf$/, since })[0];
  expect(put.req["content-type"]).toBe("application/pdf");
  expect2xx(put.status, "CV upload");
  const post = A.net.find({ method: "POST", path: "/admin/index.html", since })[0];
  expect(post.req.range).toBe("selector=tbody#candidates-body");
  expect2xx(post.status, "candidate row");
  const ack = await A.page.locator("#ackId").textContent();
  applicant = ack.replace("Reference: ", "").trim();
  cvPath = put.path;
  expect(cvPath).toBe(`/uploads/${applicant}-cv.pdf`);
  expect((await http(SITE)("GET", cvPath)).status, "anonymous CV read").toBe(401);
  const stored = await dav(SITE, key).get(cvPath);
  expect(Buffer.from(await stored.arrayBuffer()).equals(pdf)).toBe(true);
  expect(realErrors(A.errors)).toEqual([]);
  await A.context.close();
});

test("ACC-AT-4 admin is private, staff get in", { tag: "@tierB" }, async ({ browser }) => {
  for (const p of ["/admin/", "/admin/index.html"]) {
    const r = await http(SITE)("GET", p);
    expect(r.status, p).toBe(401);
    expect(r.text).toContain('<a href="/auth/login">Log in</a>');
  }
  const B = await newSession(browser);
  await signIn(B.page, "admin1");
  expect(B.net.find({ method: "GET", path: "/admin/" }).at(-1).status).toBe(200);
  await expect(B.page.locator(`#candidates-body > tr#${applicant}`)).toHaveCount(1);
  const cv = await B.page.request.get(origin(SITE) + cvPath);
  expect(cv.status()).toBe(200);
  expect(cv.headers()["content-type"]).toBe("application/pdf");
  expect(realErrors(B.errors)).toEqual([]);
  await B.context.close();

  const C = await newSession(browser);
  await C.page.goto(origin(SITE) + "/auth/login?redirect=/admin/");
  await C.page.fill('input[name="username"]', "bob");
  await C.page.fill('input[name="password"]', PW.bob);
  await C.page.click('button[type="submit"]');
  await C.page.waitForURL(origin(SITE) + "/admin/");
  expect(C.net.find({ method: "GET", path: "/admin/" }).at(-1).status).toBe(403);
  await C.context.close();
});

/** Remove every <tr> AuthorizationRule whose actor is one of the listed
 * e-mail addresses (the per-email fallbacks). */
function withoutEmailRules(doc) {
  return doc.replace(/\s*<tr itemscope itemtype="https:\/\/pagelove.org\/AuthorizationRule">\s*<td itemprop="actor">admin\d@example\.com<\/td>[\s\S]*?<\/tr>/g, "");
}

test("ACC-AT-4b the admins Group and GroupMembership both resolve", { tag: "@tierB" }, async () => {
  const d = dav(SITE, key);
  const original = await d.text("/admin/auth.html");
  try {
    const noEmail = withoutEmailRules(original);
    expect(noEmail).not.toMatch(/AuthorizationRule">\s*<td itemprop="actor">admin\d@example\.com/);
    expect(original.length - noEmail.length).toBeGreaterThan(2000); // 10 rows removed
    await d.putOk("/admin/auth.html", noEmail);
    const admin3 = await sessionCookie(SITE, "admin3", PW.admin3);
    expect((await http(SITE, { cookie: admin3 })("GET", "/admin/")).status, "via the admins Group").toBe(200);
    const noGroup = noEmail.replace(/<div itemscope itemtype="https:\/\/pagelove.org\/Group">[\s\S]*?<\/div>/, "");
    expect(noGroup).not.toContain('itemtype="https://pagelove.org/Group"');
    await d.putOk("/admin/auth.html", noGroup);
    // §7.6 expects the legacy GroupMembership table alone to still grant (C-10).
    // The owning area decided the opposite: GroupMembership items are ignored,
    // failing closed (permissions R-PERM-7, R-PERM-61, contradiction C7; case
    // authz.group.groupmembership-legacy-ignored). The ATS itself never needs
    // it — its Group resolves (above). Recorded as a deviation, not a failure.
    expect((await http(SITE, { cookie: admin3 })("GET", "/admin/")).status, "GroupMembership only: ignored (permissions C7)").toBe(403);
    const bob = await sessionCookie(SITE, "bob", PW.bob);
    expect((await http(SITE, { cookie: bob })("GET", "/admin/")).status).toBe(403);
  } finally {
    await d.putOk("/admin/auth.html", original);
  }
});

test("ACC-AT-5 staff edits", { tag: "@tierB" }, async ({ browser }) => {
  const B = await newSession(browser);
  B.page.on("dialog", (d) => d.accept());
  await signIn(B.page, "admin1", "/admin/index.html");
  const row = B.page.locator(`#candidates-body > tr#${applicant}`);
  await row.locator("td").first().click(); // expand the candidate
  let since = B.net.mark();
  await B.page.locator(`.editable[data-edit="stage"][data-id="${applicant}"]`).click();
  await B.page.locator(`.editable[data-edit="stage"][data-id="${applicant}"] .pill`, { hasText: "Interview 1" }).click();
  const put = await B.net.waitFor({ method: "PUT", path: "/admin/index.html", since });
  expect(put.req.range).toBe(`selector=#${applicant}`);
  expect(put.status).toBe(206);
  expect((await put.response.text()).trim()).toMatch(new RegExp(`^<tr id="${applicant}"`));
  await expect(B.page.locator(`#candidates-body > tr#${applicant} [itemprop="stage"]`)).toHaveText("Interview 1");
  const stageLog = await B.net.waitFor({ method: "POST", path: "/admin/index.html", range: "selector=#activity-body", since });
  expect2xx(stageLog.status, "stage-change activity");
  expect((await stageLog.response.text()).trim()).toMatch(/^<tr id="L-\d+"/);

  // Add an activity note.
  since = B.net.mark();
  await B.page.locator(`[data-add-activity="${applicant}"]`).click();
  await B.page.fill(`.activity-list[data-candidate-id="${applicant}"] form.activity-form input[name="summary"]`, "Strong compiler background");
  await B.page.locator(`.activity-list[data-candidate-id="${applicant}"] form.activity-form`).evaluate((f) => f.requestSubmit());
  const note = await B.net.waitFor({ method: "POST", path: "/admin/index.html", range: "selector=#activity-body", since });
  expect2xx(note.status, "activity note");

  // Add an interview, then delete it.
  await B.page.click('nav.tabs a[href="#interviews"]');
  since = B.net.mark();
  await B.page.click("#addInterviewBtn");
  const form = B.page.locator("form.interview-form");
  await form.locator(`.pill-group[data-name="candidateId"] .pill[data-value="${applicant}"]`).click();
  await form.locator('button[type="submit"]').click();
  const iv = await B.net.waitFor({ method: "POST", path: "/admin/index.html", range: "selector=#interviews-body", since });
  expect2xx(iv.status, "interview");
  const ivHtml = (await iv.response.text()).trim();
  expect(ivHtml).toMatch(/^<tr id="I-\d+"/);
  const ivId = ivHtml.match(/^<tr id="(I-\d+)"/)[1];
  await expect(B.page.locator(`#interviews-body > tr#${ivId}`)).toHaveCount(1);
  since = B.net.mark();
  await B.page.locator(`#interviews-body > tr#${ivId} td`).first().click();
  await B.page.locator(`[data-delete-interview="${ivId}"]`).click();
  await B.page.locator(`[data-confirm-interview="${ivId}"]`).click();
  const del = await B.net.waitFor({ method: "DELETE", path: "/admin/index.html", since });
  expect(del.req.range).toBe(`selector=#${ivId}`);
  expect2xx(del.status, "delete interview");
  await expect(B.page.locator(`#interviews-body > tr#${ivId}`)).toHaveCount(0);
  expect(await B.page.locator("#candidates-body").textContent()).not.toMatch(/\bnull\b/);
  expect(realErrors(B.errors)).toEqual([]);
  await B.context.close();
});

test("ACC-AT-6 public window stays narrow", { tag: "@tierB" }, async () => {
  const req = http(SITE);
  const html = { "Content-Type": "text/html" };
  const before = await dav(SITE, key).text("/admin/index.html");
  const a = await req("POST", "/admin/index.html", { headers: { ...html, Range: "selector=tbody#candidates-body" },
    body: '<tr id="C-evil01" itemscope itemtype="https://pagelove.org/Candidate"><td><img src=x onerror=alert(1)></td><td itemprop="id">C-evil01</td></tr>' });
  expect(a.status, "(a) <img onerror> in a cell").toBe(422);
  const b = await req("POST", "/admin/index.html", { headers: { ...html, Range: "selector=#roles-body" }, body: '<tr id="R-999"><td>x</td></tr>' });
  expect(b.status, "(b) POST #roles-body").toBe(401);
  const c = await req("PUT", "/admin/index.html", { headers: { ...html, Range: "selector=#C-a1001x" }, body: '<tr id="C-a1001x"><td>x</td></tr>' });
  expect(c.status, "(c) PUT a candidate").toBe(401);
  const d = await req("GET", "/admin/index.html", { headers: { Range: "selector=#roles-body" } });
  expect(d.status, "(d) selector GET of the private document").toBe(401);
  expect(await dav(SITE, key).text("/admin/index.html")).toBe(before);
});

test("ACC-AT-7 new role gets a public page", { tag: "@tierB" }, async ({ browser }) => {
  const B = await newSession(browser);
  B.page.on("dialog", (d) => d.accept());
  await signIn(B.page, "admin1", "/admin/index.html");
  await B.page.click('nav.tabs a[href="#roles"]');
  const since = B.net.mark();
  await B.page.click("#addRoleBtn");
  const form = B.page.locator("form.role-form");
  await form.locator('input[name="title"]').fill("Staff Engineer");
  await form.locator('input[name="hiringManager"]').fill("Admin One");
  await form.locator('input[name="location"]').fill("Remote");
  await form.locator('button[type="submit"]').click();
  const post = await B.net.waitFor({ method: "POST", path: "/admin/index.html", range: "selector=#roles-body", since });
  expect2xx(post.status, "role");
  await B.context.close();

  const A = await newSession(browser);
  const r = await A.page.goto(origin(SITE) + "/roles/staff-engineer/");
  expect(r.status()).toBe(200);
  await expect(A.page.locator("#role-view h1")).toHaveText("Staff Engineer");
  await A.page.goto(origin(SITE) + "/");
  await expect(A.page.locator('a[href="/roles/staff-engineer/"]').first()).toBeVisible();
  await A.context.close();
});

test("ACC-AT-9 rule tables parse (OPTIONS)", { tag: "@tierB" }, async () => {
  const opts = async (cookie) => {
    const r = await http(SITE, { cookie })("OPTIONS", "/admin/index.html", { headers: { Accept: "multipart/mixed", Prefer: "return=representation" } });
    return { status: r.status, parts: r.status === 207 ? multipart(r.text, r.headers.get("content-type")) : [], allow: r.headers.get("allow") };
  };
  // Staff through the admins Group (admin3 has per-email rules only on
  // /admin/index.html for admin1..3, so use the group-only view: admin3's
  // email rules grant everything whole-resource too; both are listed).
  const staff = await opts(await sessionCookie(SITE, "admin1", PW.admin1));
  expect(staff.status).toBe(207);
  test.info().annotations.push({ type: "staff parts", description: JSON.stringify(staff.parts.map((p) => p.headers)) });
  // A rule's selector is reported as written (R-PERM-63), so the admins rule
  // `tbody, tr[itemtype]` is one part whose selector list covers both; a
  // client subscribing with querySelectorAll (beta-js) wires both.
  const covering = (sel) => staff.parts.filter((p) => (p.headers["content-range"] || "").replace(/^selector /, "").split(",").map((s) => s.trim()).includes(sel));
  for (const sel of ["tbody", "tr[itemtype]"]) {
    const parts = covering(sel);
    expect(parts.length, sel).toBeGreaterThan(0);
    for (const m of ["POST", "PUT", "DELETE"]) expect(parts.some((p) => new RegExp(`\\b${m}\\b`).test(p.headers.allow || "")), `${sel} ${m}`).toBe(true);
  }
  const anon = await opts(undefined);
  expect(anon.status).toBe(207);
  const writeParts = anon.parts.filter((p) => /\b(POST|PUT|DELETE)\b/.test(p.headers.allow || ""));
  expect(writeParts.map((p) => [p.headers["content-range"], p.headers.allow])).toEqual([["selector tbody#candidates-body", expect.stringMatching(/\bPOST\b/)]]);
  expect(writeParts[0].headers.allow).not.toMatch(/\b(PUT|DELETE)\b/);
});

test("ACC-AT-8 outbox relays email", { tag: "@tierC" }, async ({ browser }) => {
  // A local sink stands in for api.postmarkapp.com (test-only url edit).
  const got = [];
  const sink = http_.createServer((req, res) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => { got.push({ method: req.method, url: req.url, headers: req.headers, body: Buffer.concat(chunks).toString("utf8"), at: Date.now() }); res.writeHead(200, { "Content-Type": "application/json" }).end('{"ErrorCode":0}'); });
  });
  await new Promise((r) => sink.listen(0, "127.0.0.1", r));
  const sinkUrl = `http://127.0.0.1:${sink.address().port}/email`;
  const d = dav(SITE, key);
  const original = await d.text("/outbox/index.html");
  try {
    await d.putOk("/outbox/index.html", original.replace('content="https://api.postmarkapp.com/email"', `content="${sinkUrl}"`));
    const B = await newSession(browser);
    B.page.on("dialog", (dlg) => dlg.accept());
    await signIn(B.page, "admin1", "/admin/index.html");
    const cid = applicant ?? "C-a1001x"; // the ACC-AT-3 applicant, or a seeded candidate when run alone
    await B.page.locator(`#candidates-body > tr#${cid} td`).first().click();
    await B.page.locator(`[data-send-email="${cid}"]`).click();
    const composer = B.page.locator(`.email-composer-slot[data-candidate-id="${cid}"]`);
    await composer.locator('input[name="subject"]').fill("Next steps");
    await composer.locator('textarea[name="body"]').fill("Hello Grace, let's talk.");
    const since = B.net.mark();
    const t0 = Date.now();
    await composer.locator(".send-via-ats-btn").click();
    const put = await B.net.waitFor({ method: "PUT", path: /^\/outbox\/E-[a-z0-9]{8}\.json$/, since });
    expect(put.req["content-type"]).toBe("application/json");
    expect2xx(put.status, "outbox PUT");
    const answeredAt = Date.now();
    await expect.poll(() => got.length, { timeout: 10_000 }).toBe(1);
    const [hit] = got;
    expect(hit.method).toBe("POST");
    expect(hit.headers["content-type"]).toMatch(/^application\/json/);
    expect(hit.headers.accept).toBe("application/json");
    expect(hit.headers["x-postmark-server-token"]).toBe("");
    const uploaded = JSON.parse(put.body);
    expect(hit.body).toBe(put.body);
    expect(uploaded.To).toMatch(/@/);
    if (applicant) expect(uploaded.To).toBe("grace@example.com");
    expect(answeredAt - t0).toBeLessThan(10_000);
    await expect(composer.locator(".email-status")).toContainText("Sent", { timeout: 5000 });
    await B.context.close();
  } finally {
    await d.putOk("/outbox/index.html", original);
    sink.close();
  }
});
