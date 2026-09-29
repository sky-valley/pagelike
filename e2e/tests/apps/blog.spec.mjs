// A2 — Tutorial "Build a blog" (Field Notes) (docs/spec/apps.md §7.3):
// ACC-BL-1..7. Every file is added in tutorial order, exactly as printed in
// the docs snapshot (learn/build-a-blog). The `users` part signs in with a
// local account alice@example.com (verified), standing in for an OIDC login.
import { test, expect } from "@playwright/test";
import { createSite, addUser } from "../../lib/pagelike.mjs";
import { hasResearch, docPage, codeBlocks, dav, http, newSession, expect2xx, origin, realErrors, sessionCookie } from "../../lib/apps.mjs";

test.skip(!hasResearch(), "research/ (docs snapshot) not available");
test.describe.configure({ mode: "serial" });

const SITE = "blog";
let key;
let B; // the tutorial's code blocks

/** The printed documents, keyed by the file the tutorial names. */
function blogFiles() {
  const b = codeBlocks(docPage("learn_build-a-blog")).map((x) => x.code + "\n");
  const pick = (needle) => b.find((c) => c.includes(needle));
  const files = {
    helloWorld: pick("<title>data: hello-world</title>"),
    index: pick("<title>Field Notes</title>\n</head>") || b[1],
    route: pick('<body e:post="'),
    processors: pick("<title>processors</title>"),
    archive: pick("<title>Archive — Field Notes</title>"),
    feed: pick("<?xml version"),
    form: pick('<form class="comment-form"'),
    script: pick('document.getElementById("comment-form")'),
    rules: pick("<title>authorization rules</title>"),
    constraints: pick("<title>constraints</title>"),
    lock: pick("The public reads composed pages"),
    attack: pick("gotcha"),
  };
  for (const [k, v] of Object.entries(files)) if (!v) throw new Error(`blog tutorial block ${k} not found`);
  return files;
}

/** "Copy hello-world.html to second-thoughts.html and change its data". */
function post(src, { slug, title, excerpt, date, display, month, status = "Published" }) {
  return src.replaceAll("hello-world", slug).replace("Hello, world", title)
    .replace(/(itemprop="excerpt" content=")[^"]*/, `$1${excerpt}`)
    .replace(/(itemprop="displayDate" content=")[^"]*/, `$1${display}`)
    .replace(/(itemprop="monthLabel" content=")[^"]*/, `$1${month}`)
    .replace(/(itemprop="status" content=")[^"]*/, `$1${status}`)
    .replace(/datetime="2026-08-03">3 August 2026/, `datetime="${date}">${display}`);
}

/** The route with the comment form (inside <main>, between .post-missing and
 * the back link) and the script (just before </body>), as the tutorial says. */
function routeWithComments(route, f) {
  return route.replace(/(\n\s*<a class="back")/, "\n" + f.form.replace(/^/gm, "    ").trimEnd() + "$1")
    .replace("</body>", f.script.trimEnd() + "\n</body>");
}

/** C-16: the printed route's own <style> names the Post itemtype, so the
 * processor's substring test never fires. The release-blocking variant uses
 * the rule §7.3 prescribes instead. */
const variantStyle = (route) => route.replace('main:has([itemtype="https://blog.example/Post"]) .post-missing', "main:has(article[itemscope]) .post-missing");

test.beforeAll(async () => {
  key = createSite(SITE, { defaultGet: "allow" });
  addUser(SITE, "alice", { email: "alice@example.com", verified: true, name: "Alice", password: "alice-local-test-pw" });
  B = blogFiles();
});

const put = (path, body) => dav(SITE, key).putOk(path, body);
const get = (path, opts) => http(SITE)("GET", path, opts);

test("ACC-BL-1 home composes posts", { tag: "@tierB" }, async ({ browser }) => {
  await put("/data/posts/hello-world.html", B.helloWorld);
  await put("/index.html", B.index);
  const first = await get("/index.html");
  expect(first.status).toBe(200);
  expect(first.text).toContain("The first post on Field Notes");
  await put("/data/posts/second-thoughts.html", post(B.helloWorld, { slug: "second-thoughts", title: "Second thoughts",
    excerpt: "A second look at the first week.", date: "2026-08-05", display: "5 August 2026", month: "August 2026" }));
  const ctx = await browser.newContext({ javaScriptEnabled: false });
  const page = await ctx.newPage();
  const r = await page.goto(origin(SITE) + "/index.html");
  expect(r.status()).toBe(200);
  await expect(page.locator("section.excerpts h2 a")).toHaveText(["Second thoughts", "Hello, world"]);
  const src = await r.text();
  expect(src).toContain("A second look at the first week.");
  expect(src).toContain("The first post on Field Notes");
  for (const s of ["{%", "r:posts", "p:template", "xmlns:"]) expect(src, s).not.toContain(s);
  await ctx.close();
});

test("ACC-BL-2 one route for every post, and a real 404", { tag: "@tierB" }, async () => {
  await put("/posts/:slug.html", B.route);
  const hello = await get("/posts/hello-world.html");
  expect(hello.status).toBe(200);
  expect(hello.text).toMatch(/<article itemscope(="")? itemtype="https:\/\/blog.example\/Post" id="post-hello-world">/);
  expect(hello.text).not.toContain("p:stamp");
  const missing = await get("/posts/nonsense.html");
  expect(missing.status).toBe(200);
  expect(missing.text).toContain("<h1>Not found</h1>");

  await put("/processors.html", B.processors);
  // As printed (C-16, P-APPS-22): the route's <style> names the Post type, so
  // under documented String.contains semantics the status stays 200. Recorded,
  // not release-blocking.
  const printed = await http(SITE)("HEAD", "/posts/nonsense.html");
  test.info().annotations.push({ type: "C-16 printed template", description: `HEAD /posts/nonsense.html -> ${printed.status}` });
  expect(printed.status).toBe(200);

  // Release-blocking variant: the stylesheet does not name the type.
  await put("/posts/:slug.html", variantStyle(B.route));
  expect((await http(SITE)("HEAD", "/posts/nonsense.html")).status).toBe(404);
  expect((await http(SITE)("HEAD", "/posts/hello-world.html")).status).toBe(200);
  const g = await get("/posts/nonsense.html");
  expect(g.status).toBe(404);
  expect(g.text).toContain("<h1>Not found</h1>");
  expect((await get("/posts/hello-world.html")).status).toBe(200);
});

test("ACC-BL-3 archive and feed", { tag: "@tierB" }, async () => {
  await put("/archive.html", B.archive);
  await put("/feed.xml", B.feed);
  const archive = await get("/archive.html");
  expect(archive.status).toBe(200);
  expect(archive.text.match(/<h2>August 2026<\/h2>/g)).toHaveLength(1);
  const rows = [...archive.text.matchAll(/<div class="archive-row"><a href="\/posts\/([a-z-]+)\.html">/g)].map((m) => m[1]);
  expect(rows).toEqual(["second-thoughts", "hello-world"]);
  expect(archive.text.indexOf("<h2>August 2026</h2>")).toBeLessThan(archive.text.indexOf('class="archive-row"'));

  const feed = await get("/feed.xml");
  expect(feed.status).toBe(200);
  expect(feed.headers.get("content-type")).toMatch(/xml/);
  expect(feed.text.startsWith("<?xml")).toBe(true);
  const entries = [...feed.text.matchAll(/<entry><title>([^<]*)<\/title>/g)].map((m) => m[1]);
  expect(entries).toEqual(["Second thoughts", "Hello, world"]);
  expect(feed.text).not.toMatch(/\s(p|r):(template|posts)=/);

  await put("/data/posts/september.html", post(B.helloWorld, { slug: "september", title: "September already",
    excerpt: "Autumn notes.", date: "2026-09-02", display: "2 September 2026", month: "September 2026" }));
  const archive2 = (await get("/archive.html")).text;
  expect(archive2).toContain("<h2>September 2026</h2>");
  expect(archive2.indexOf("<h2>September 2026</h2>")).toBeLessThan(archive2.indexOf("<h2>August 2026</h2>"));
  expect(((await get("/feed.xml")).text.match(/<entry>/g) || []).length).toBe(3);
});

test("ACC-BL-4 comment from the page", { tag: "@tierB" }, async ({ browser }) => {
  const route = routeWithComments(variantStyle(B.route), B);
  await put("/posts/:slug.html", route);
  await put("/rules.html", B.rules);
  const A = await newSession(browser);
  await A.page.goto(origin(SITE) + "/posts/hello-world.html");
  await A.page.fill('#comment-form input[name="author"]', "Ada");
  await A.page.fill('#comment-form textarea[name="body"]', "Nice post");
  const since = A.net.mark();
  await A.page.click('#comment-form button[type="submit"]');
  const post_ = await A.net.waitFor({ method: "POST", path: "/posts/hello-world.html", since });
  expect(post_.req.range).toBe("selector=#comments-hello-world");
  expect2xx(post_.status, "comment POST");
  await expect(A.page.locator("#comments-hello-world li.comment")).toHaveCount(1);
  const Bs = await newSession(browser);
  await Bs.page.goto(origin(SITE) + "/posts/hello-world.html");
  await expect(Bs.page.locator("#comments-hello-world li.comment", { hasText: "Nice post" })).toHaveCount(1);
  const data = await dav(SITE, key).text("/data/posts/hello-world.html");
  expect(data).toMatch(/<ul itemprop="comments" id="comments-hello-world" class="comment-list"><li class="comment" itemscope(="")? itemtype="https:\/\/blog.example\/Comment">[\s\S]*Nice post[\s\S]*<\/li><\/ul>/);
  expect(await dav(SITE, key).text("/posts/:slug.html")).toBe(route);
  expect(realErrors(A.errors)).toEqual([]);
  await A.context.close();
  await Bs.context.close();
});

test("ACC-BL-5 shape makes public comments safe", { tag: "@tierB" }, async ({ browser }) => {
  // The tutorial's attack, as its curl sends it.
  const attack = () => http(SITE)("POST", "/posts/hello-world.html", {
    headers: { Range: "selector=#comments-hello-world", "Content-Type": "text/html" },
    body: '<li class="comment"><script>alert("gotcha")</script></li>' });
  expect2xx((await attack()).status, "attack before the constraint");
  expect(await dav(SITE, key).text("/data/posts/hello-world.html")).toContain('<script>alert("gotcha")</script>');
  await put("/constraints.html", B.constraints);
  // "Delete the script comment the first attempt left" — over dav.
  const cleaned = (await dav(SITE, key).text("/data/posts/hello-world.html")).replace('<li class="comment"><script>alert("gotcha")</script></li>', "");
  await put("/data/posts/hello-world.html", cleaned);
  const again = await attack();
  expect(again.status).toBe(422);
  expect(await dav(SITE, key).text("/data/posts/hello-world.html")).toBe(cleaned);
  // An honest comment from the page still sails through.
  const A = await newSession(browser);
  await A.page.goto(origin(SITE) + "/posts/hello-world.html");
  await A.page.fill('#comment-form input[name="author"]', "Grace");
  await A.page.fill('#comment-form textarea[name="body"]', "First paragraph.\n\nSecond paragraph.");
  const since = A.net.mark();
  await A.page.click('#comment-form button[type="submit"]');
  expect2xx((await A.net.waitFor({ method: "POST", path: "/posts/hello-world.html", since })).status, "honest comment");
  await expect(A.page.locator("#comments-hello-world li.comment", { hasText: "Second paragraph." })).toHaveCount(1);
  await A.context.close();
});

test("ACC-BL-6 data folder locked, pages still compose", { tag: "@tierB" }, async () => {
  await put("/rules.html", B.rules.replace("</body>", B.lock + "</body>"));
  const anon = await get("/data/posts/hello-world.html");
  expect(anon.status).toBe(401);
  const home = await get("/index.html");
  expect(home.status).toBe(200);
  expect(home.text).toContain("Second thoughts");
  const alice = await sessionCookie(SITE, "alice", "alice-local-test-pw");
  expect((await http(SITE, { cookie: alice })("GET", "/data/posts/hello-world.html")).status).toBe(200);
});

test("ACC-BL-7 drafts stay hidden", { tag: "@tierB" }, async () => {
  await put("/data/posts/secret-plans.html", post(B.helloWorld, { slug: "secret-plans", title: "Secret plans",
    excerpt: "Not yet.", date: "2026-09-20", display: "20 September 2026", month: "September 2026", status: "Draft" }));
  for (const p of ["/index.html", "/archive.html", "/feed.xml"]) expect((await get(p)).text, p).not.toContain("Secret plans");
  expect((await http(SITE)("HEAD", "/posts/secret-plans.html")).status).toBe(404);
});
