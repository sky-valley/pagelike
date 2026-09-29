// A3 — Recipes (docs/spec/apps.md §7.10): ACC-RC-1..3, ACC-RC-5, ACC-RC-6,
// ACC-RC-9 (tier B) and ACC-RC-4 (tier C: outbound HTTP). ACC-RC-7 and
// ACC-RC-8 are ACC-BJ-5 and ACC-BJ-4 (betajs.spec.mjs).
//
// Each recipe's markup is taken from the docs snapshot where the recipe prints
// it, wrapped into the documents it needs (a Schema item, a rules document).
// Recipes show no authorization rules; each site gets a permissive rule so
// the recipe's own writes are allowed (fixture, recorded in app-changes.md).
import { test, expect } from "@playwright/test";
import { createSite, addUser } from "../../lib/pagelike.mjs";
import { hasResearch, docPage, codeBlocks, dav, http, expect2xx, origin, sessionCookie, startSink } from "../../lib/apps.mjs";

test.skip(!hasResearch(), "research/ (docs snapshot) not available");

const blocks = (page) => codeBlocks(docPage(`recipes_${page}`)).map((b) => b.code);
const OPEN_RULES = (paths = ["/*"]) => `<!DOCTYPE html><html><body>${paths.map((p) => `
<div hidden itemscope itemtype="https://pagelove.org/AuthorizationRule"><meta itemprop="actor" content="*">
<meta itemprop="resource" content="${p}"><meta itemprop="method" content="*"><meta itemprop="action" content="allow"></div>`).join("")}
</body></html>`;

async function recipeSite(name, rulePaths) {
  const key = createSite(name, { defaultGet: "allow" });
  await dav(name, key).putOk("/rules.html", OPEN_RULES(rulePaths));
  return { key, d: dav(name, key), req: http(name) };
}
const html = { "Content-Type": "text/html" };
const doc = (body) => `<!DOCTYPE html>\n<html><body>\n${body}\n</body></html>\n`;

test("ACC-RC-1 transforming data on write and read", { tag: "@tierB" }, async () => {
  const [slugProp, dateProp] = blocks("transforming-data");
  const { d, req } = await recipeSite("recipe-tx");
  await d.putOk("/schemas.html", doc(`<div hidden itemscope itemtype="https://pagelove.org/Schema">
  <meta itemprop="type" content="https://example.com/Project">
  <ul>
${slugProp}
${dateProp}
  </ul>
</div>`));
  const put = await req("PUT", "/projects/p1.html", { headers: html, body: doc(`<div itemscope itemtype="https://example.com/Project">
  <meta itemprop="slug" content="My-Project">
  <time itemprop="createdAt" datetime="2026-04-08T09:15:00Z"></time>
</div>`) });
  expect2xx(put.status, "PUT a Project");
  const stored = await d.text("/projects/p1.html");
  expect(stored).toContain('<meta itemprop="slug" content="my-project">');
  expect(stored).not.toContain("My-Project");
  const read = await req("GET", "/projects/p1.html");
  expect(read.text).toContain('<time itemprop="createdAt" datetime="2026-04-08T09:15:00Z">8 April 2026</time>');
  expect(read.text).toContain('content="my-project"');
});

test("ACC-RC-2 linking related data", { tag: "@tierB" }, async () => {
  const { d, req } = await recipeSite("recipe-link");
  const [uniqueProp, , , , groupConstraint] = blocks("linking-related-data");
  // The recipe prints `references` as a bare type URL with an `onDelete`
  // field; the Property reference page (which wins, modeling C5) writes
  // `{itemtype}#{itemprop}` and `cascade`. The scenario uses the latter.
  await d.putOk("/schemas.html", doc(`<div hidden itemscope itemtype="https://pagelove.org/Schema">
  <meta itemprop="type" content="https://example.com/Project">
  <ul>
${uniqueProp}
  </ul>
</div>
<div hidden itemscope itemtype="https://pagelove.org/Schema">
  <meta itemprop="type" content="https://example.com/Task">
  <ul>
    <li itemprop="property" itemscope itemtype="https://pagelove.org/Property">
      <meta itemprop="name" content="project">
      <meta itemprop="type" content="https://schema.host/Text">
      <meta itemprop="cardinality" content="1..1">
      <meta itemprop="references" content="https://example.com/Project#slug">
      <meta itemprop="cascade" content="restrict">
    </li>
  </ul>
</div>
<div hidden itemscope itemtype="https://pagelove.org/Schema">
  <meta itemprop="type" content="https://example.com/Person">
  <ul>
    <li itemprop="property" itemscope itemtype="https://pagelove.org/Property"><meta itemprop="name" content="email"><meta itemprop="type" content="https://schema.host/Text"><meta itemprop="cardinality" content="0..1"></li>
    <li itemprop="property" itemscope itemtype="https://pagelove.org/Property"><meta itemprop="name" content="phone"><meta itemprop="type" content="https://schema.host/Text"><meta itemprop="cardinality" content="0..1"></li>
  </ul>
${groupConstraint}
</div>`));
  const project = (slug) => doc(`<div itemscope itemtype="https://example.com/Project"><meta itemprop="slug" content="${slug}"></div>`);
  expect2xx((await req("PUT", "/projects/launch.html", { headers: html, body: project("launch") })).status);
  const dup = await req("PUT", "/projects/launch-2.html", { headers: html, body: project("launch") });
  expect(dup.status, "a second Project with slug launch").toBe(422);
  const dangling = await req("PUT", "/tasks/t0.html", { headers: html, body: doc('<div itemscope itemtype="https://example.com/Task"><meta itemprop="project" content="nowhere"></div>') });
  expect(dangling.status, "a reference to nothing").toBe(422);
  expect(dangling.text).toContain("project");
  expect2xx((await req("PUT", "/tasks/t1.html", { headers: html, body: doc('<div itemscope itemtype="https://example.com/Task"><meta itemprop="project" content="launch"></div>') })).status);
  const del = await req("DELETE", "/projects/launch.html");
  expect(del.status, "deleting a referenced Project under restrict").toBe(409);
  expect((await d.get("/projects/launch.html")).status).toBe(200);
  const nobody = await req("PUT", "/people/p0.html", { headers: html, body: doc('<div itemscope itemtype="https://example.com/Person"><span>no contact</span></div>') });
  expect(nobody.status, "GroupConstraint contact 1..n with neither email nor phone").toBe(422);
  expect2xx((await req("PUT", "/people/p1.html", { headers: html, body: doc('<div itemscope itemtype="https://example.com/Person"><meta itemprop="phone" content="555-0100"></div>') })).status);
});

test("ACC-RC-3 group-based permissions", { tag: "@tierB" }, async () => {
  const S = "recipe-groups";
  const key = createSite(S, { defaultGet: "allow" });
  const d = dav(S, key);
  const [group, rule, changed] = blocks("group-based-permissions");
  await d.putOk("/groups.html", doc(group));
  await d.putOk("/rules.html", doc(rule));
  await d.putOk("/blog/index.html", doc('<ul id="posts"></ul>'));
  addUser(S, "alice", { email: "alice@example.com", verified: true, password: "alice-local-test-pw" });
  addUser(S, "bob", { email: "bob@example.com", verified: true, password: "bob-local-test-pw" });
  addUser(S, "dave", { email: "dave@example.com", verified: true, password: "dave-local-test-pw" });
  addUser(S, "mallory", { email: "carol@example.com", verified: false, password: "mallory-local-test-pw" });
  const as = async (u) => http(S, { cookie: await sessionCookie(S, u, `${u}-local-test-pw`) });
  const post = async (u, text) => (await (await as(u))("POST", "/blog/index.html", { headers: { ...html, Range: "selector=#posts" }, body: `<li>${text}</li>` })).status;
  expect2xx(await post("alice", "from alice"), "alice is an editor");
  expect2xx(await post("bob", "from bob"), "bob is an editor");
  expect(await post("dave", "from dave"), "dave is not (yet) an editor").toBe(403);
  expect(await post("mallory", "unverified carol"), "an unverified email never matches").toBe(403);
  // Change membership without touching rules: alice and dave (the recipe's second block).
  await d.putOk("/groups.html", doc(changed));
  expect(await post("bob", "bob again"), "bob left the group").toBe(403);
  expect2xx(await post("dave", "dave now"), "dave joined the group");
  expect2xx(await post("alice", "alice still"), "alice stays");
});

test("ACC-RC-4 sending a webhook", { tag: "@tierC" }, async () => {
  test.setTimeout(90_000);
  const sink = await startSink();
  try {
    const [processor] = blocks("sending-a-webhook");
    const { d, req } = await recipeSite("recipe-hook");
    await d.putOk("/processors.html", doc(processor.replace("https://example.com/webhooks/note-created", sink.url("/note-created"))));
    // The selector filter matches the request's key element, which for a POST
    // append is the anchor (reacting R-REACT-14, contradiction C6: the recipe
    // "would not fire for a plain list"). So the writes append into a Note.
    await d.putOk("/notes/inbox.html", doc('<article id="note-1" itemscope itemtype="https://example.com/Note"><p itemprop="text">first</p></article>\n<ul id="other"></ul>'));
    sink.respond("/note-created", [200]);
    const t0 = Date.now();
    const r = await req("POST", "/notes/inbox.html", { headers: { ...html, Range: "selector=#note-1" }, body: "<p>a follow-up line</p>" });
    const answered = Date.now();
    expect2xx(r.status, "note POST");
    await expect.poll(() => sink.hits("/note-created").length, { timeout: 5000 }).toBe(1);
    const [hit] = sink.hits("/note-created");
    expect(hit.at).toBeGreaterThanOrEqual(answered - 5); // dispatched after the response
    expect(JSON.parse(hit.body)).toEqual({ path: "/notes/inbox.html" });
    expect(hit.headers["content-type"]).toMatch(/^application\/json/);
    expect(answered - t0).toBeLessThan(2000);
    // PageLove never uses a processor's selector filter to exclude a request
    // (live 2026-09-29, decisions-2026-09-29/reacting.md,
    // reacting.filters.selector-semantic-match): a POST to a plain list fires
    // the processor too.
    expect2xx((await req("POST", "/notes/inbox.html", { headers: { ...html, Range: "selector=#other" }, body: "<li>plain</li>" })).status);
    await expect.poll(() => sink.hits("/note-created").length, { timeout: 5000 }).toBe(2);

    // retry 3 against a failing sink: 4 attempts, growing gaps, client unaffected.
    await d.putOk("/processors.html", doc(processor.replace("https://example.com/webhooks/note-created", sink.url("/failing"))
      .replace('<meta itemprop="content-type" content="application/json">', '<meta itemprop="content-type" content="application/json">\n    <meta itemprop="retry" content="3">')));
    sink.respond("/failing", [500]);
    const r2 = await req("POST", "/notes/inbox.html", { headers: { ...html, Range: "selector=#note-1" }, body: "<p>again</p>" });
    expect2xx(r2.status, "client response unaffected by a failing webhook");
    await expect.poll(() => sink.hits("/failing").length, { timeout: 60_000, intervals: [500] }).toBe(4);
    const at = sink.hits("/failing").map((h) => h.at);
    const gaps = at.slice(1).map((t, i) => t - at[i]);
    test.info().annotations.push({ type: "retry gaps (ms)", description: gaps.join(", ") });
    for (let i = 1; i < gaps.length; i++) expect(gaps[i]).toBeGreaterThan(gaps[i - 1]);
    await new Promise((r) => setTimeout(r, 2000));
    expect(sink.hits("/failing")).toHaveLength(4);
  } finally {
    await sink.close();
  }
});

test("ACC-RC-5 paginating a list", { tag: "@tierB" }, async () => {
  const [contactsMarkup, twoPaginators] = blocks("paginating-a-list");
  const { d, req } = await recipeSite("recipe-page");
  const names = Array.from({ length: 23 }, (_, i) => `Person ${String(i + 1).padStart(2, "0")}`);
  const items = names.map((n) => `  <li itemscope itemtype="https://example.com/Contact">\n    <span itemprop="name">${n}</span>\n    <span itemprop="email">p${n.slice(-2)}@example.com</span>\n  </li>`).join("\n");
  // The recipe's list, with its "... more contacts ..." filled in to 23.
  const list = contactsMarkup.replace(/<ul id="contacts"([^>]*)>[\s\S]*<\/ul>/, `<ul id="contacts"$1>\n${items}\n</ul>`);
  await d.putOk("/contacts.html", `<!DOCTYPE html>\n<html><head><title>Contacts</title></head><body>\n${list}\n</body></html>\n`);
  const count = (t) => (t.match(/<li itemscope/g) || []).length;
  const links = (r) => r.headers.get("link") || "";

  const p2 = await req("GET", "/contacts.html?paginate:page=2");
  expect(p2.status).toBe(200);
  expect(count(p2.text)).toBe(10);
  expect(p2.text).toContain("Person 11");
  expect(p2.text).not.toContain("Person 10<");
  for (const rel of ["first", "prev", "next", "last"]) expect(links(p2), rel).toMatch(new RegExp(`rel="${rel}"`));
  // Links carry the paginator's own page parameter (live 2026-09-29,
  // decisions-2026-09-29/composing-liquid.md, comp.pag.links-and-slice).
  expect(links(p2)).toContain("paginate:contacts:page=3>");
  expect(p2.text).toMatch(/<link[^>]+rel="next"/);
  const clamp = await req("GET", "/contacts.html?paginate:page=9");
  expect(count(clamp.text)).toBe(3);
  expect(clamp.text).toContain("Person 23");
  expect(links(clamp)).not.toMatch(/rel="next"/);
  const q = await req("GET", "/contacts.html?q=smith&paginate:page=2");
  expect(links(q)).toMatch(/q=smith/);
  const frag = await req("GET", "/contacts.html?paginate:page=2", { headers: { Range: "selector=ul#contacts" } });
  expect(frag.status).toBe(206);
  expect(count(frag.text)).toBe(10);
  expect(frag.text.trim()).toMatch(/^<ul id="contacts"/);

  // Two paginators on one page, navigated independently.
  const li = (p, i) => `<li>${p} ${i}</li>`;
  const two = twoPaginators.replace("<!-- user items -->", Array.from({ length: 12 }, (_, i) => li("user", i + 1)).join(""))
    .replace("<!-- post items -->", Array.from({ length: 25 }, (_, i) => li("post", i + 1)).join(""));
  await d.putOk("/dashboard.html", `<!DOCTYPE html>\n<html><head><title>Dashboard</title></head><body>\n${two}\n</body></html>\n`);
  const dash = await req("GET", "/dashboard.html?paginate:users:page=2&paginate:posts:page=3");
  expect(dash.status).toBe(200);
  expect(dash.text).toContain("<li>user 6</li>");
  expect(dash.text).not.toContain("<li>user 5</li>");
  expect(dash.text).toContain("<li>post 21</li>");
  expect(dash.text).not.toContain("<li>post 20</li>");
  expect(links(dash)).toMatch(/paginate:users:page=\d[^>]*paginate:posts:page=3|paginate:posts:page=3[^>]*paginate:users:page=\d/);
});

test("ACC-RC-6 composite uniqueness", { tag: "@tierB" }, async () => {
  const schema = blocks("composite-uniqueness").find((c) => c.includes("https://pagelove.org/Schema"));
  const { d, req } = await recipeSite("recipe-cu");
  await d.putOk("/schemas.html", doc(schema));
  const m = (user, org) => doc(`<div itemscope itemtype="https://example.com/Membership"><meta itemprop="user-id" content="${user}"><meta itemprop="org-id" content="${org}"></div>`);
  expect2xx((await req("PUT", "/m/1.html", { headers: html, body: m("alice", "acme") })).status, "alice/acme");
  expect2xx((await req("PUT", "/m/2.html", { headers: html, body: m("alice", "globex") })).status, "alice/globex");
  expect2xx((await req("PUT", "/m/3.html", { headers: html, body: m("bob", "acme") })).status, "bob/acme");
  const fourth = await req("PUT", "/m/4.html", { headers: html, body: m("alice", "acme") });
  expect(fourth.status, "alice/acme again").toBe(422);
  expect(fourth.text).toMatch(/uniqueness/i);
  expect((await d.get("/m/4.html")).status).toBe(404);
});

test("ACC-RC-9 declaring a state machine", { tag: "@tierB" }, async () => {
  test.setTimeout(90_000);
  const sink = await startSink();
  try {
    const b = blocks("declaring-a-state-machine");
    const rulesDoc = b.find((c) => c.includes("<!-- /transitions/rules.html -->"));
    const handler = b.find((c) => c.includes("TransitionHandler"));
    const exitRule = b.filter((c) => c.includes('<meta itemprop="from" content="success">') && !c.includes("pending")).pop();
    const withoutExit = rulesDoc.replace(/\s*<!-- success orders may be deleted[\s\S]*?<\/div>/, "");
    const { d, req } = await recipeSite("recipe-sm");
    // Start without the exit rule (the "delete surprise"), with the handler pointed at the sink.
    const handlerDoc = doc(handler.replace("https://worker.example.com/payments", sink.url("/payments")));
    await d.putOk("/transitions/rules.html", withoutExit);
    await d.putOk("/transitions/handler.html", handlerDoc);
    const order = (status) => `<div id="order1" itemscope itemtype="https://example.com/Order">\n  <meta itemprop="status" content="${status}">\n  <span itemprop="total">42.00</span>\n</div>`;

    expect2xx((await req("PUT", "/orders/order-1.html", { headers: html, body: doc(order("pending")) })).status, "create at pending");
    // Watch the stream while taking the legal step.
    const ctl = new AbortController();
    const stream = await fetch(origin("recipe-sm") + "/orders/order-1.html", { headers: { Accept: "text/event-stream" }, signal: ctl.signal });
    expect(stream.status).toBe(200);
    const reader = stream.body.pipeThrough(new TextDecoderStream()).getReader();
    let seen = "";
    const pump = (async () => { try { for (;;) { const { value, done } = await reader.read(); if (done) return; seen += value; } } catch {} })();
    await expect.poll(() => seen.includes("pagelove-connection")).toBe(true);
    const head = await req("GET", "/orders/order-1.html", { headers: { Range: "selector=#order1" } });
    const etag = head.headers.get("etag");
    const step = await req("PUT", "/orders/order-1.html", { headers: { ...html, Range: "selector=#order1", "If-Match": etag }, body: order("processing") });
    expect2xx(step.status, "pending -> processing");
    await expect.poll(() => /event: mutation/.test(seen), { timeout: 3000 }).toBe(true);
    ctl.abort();
    await pump;
    // The handler POSTs a Transition document to the worker, once.
    await expect.poll(() => sink.hits("/payments").length, { timeout: 5000 }).toBe(1);
    const tr = sink.hits("/payments")[0];
    expect(tr.method).toBe("POST");
    expect(tr.body).toContain("/orders/order-1.html");
    expect(tr.body).toMatch(/processing/);

    // A second, racing attempt at the same legal step with the old tag: 412; the retry: 422.
    const race = await req("PUT", "/orders/order-1.html", { headers: { ...html, Range: "selector=#order1", "If-Match": etag }, body: order("processing") });
    expect(race.status, "the loser of the race").toBe(412);
    const skip = await req("PUT", "/orders/order-1.html", { headers: { ...html, Range: "selector=#order1" }, body: order("pending") });
    expect(skip.status).toBe(422);

    // An illegal step: 422 with the documented ConstraintViolation fields.
    expect2xx((await req("PUT", "/orders/order-2.html", { headers: html, body: doc(order("pending")) })).status);
    const bad = await req("PUT", "/orders/order-2.html", { headers: { ...html, Range: "selector=#order1" }, body: order("success") });
    expect(bad.status).toBe(422);
    for (const s of ['itemtype="https://pagelove.org/ConstraintViolation"', 'itemtype="https://pagelove.org/Violation"', "transition(status)", '<span itemprop="from">pending</span>', '<span itemprop="to">success</span>', "/transitions/rules.html"]) {
      expect(bad.text, s).toContain(s);
    }
    await new Promise((r) => setTimeout(r, 500));
    expect(sink.hits("/payments"), "handler fired once only").toHaveLength(1);

    // The worker reports back: processing -> success. Then the delete surprise.
    expect2xx((await req("PUT", "/orders/order-1.html", { headers: { ...html, Range: "selector=#order1" }, body: order("success") })).status, "processing -> success");
    expect((await req("DELETE", "/orders/order-1.html")).status, "delete without an exit rule").toBe(422);
    await d.putOk("/transitions/exit.html", doc(exitRule));
    expect2xx((await req("DELETE", "/orders/order-1.html")).status, "delete with the exit rule");

    // Two unkeyed Orders in one document wedge until repaired over dav. A
    // whole-document write must pair items and cannot (R-REACT-70); a
    // selector write names its element, so identity is inherent (R-REACT-69).
    const item = (id, status) => order(status).replace('id="order1" ', `id="${id}" `);
    await d.putOk("/orders/pair.html", doc(item("a", "pending") + "\n" + item("b", "pending")));
    const wedged = await req("PUT", "/orders/pair.html", { headers: html, body: doc(item("a", "processing") + "\n" + item("b", "pending")) });
    expect(wedged.status, "ambiguous unkeyed pair").toBe(422);
    expect(wedged.text).toContain("@key");
    const repaired = doc(item("a", "processing"));
    const before = sink.hits("/payments").length;
    expect2xx((await d.put("/orders/pair.html", repaired)).status, "dav repair (never transition-validated)");
    await new Promise((r) => setTimeout(r, 1000));
    expect(sink.hits("/payments").length, "a dav edit fires no handler").toBe(before);
    expect2xx((await req("PUT", "/orders/pair.html", { headers: { ...html, Range: "selector=#a" }, body: order("success").replace('id="order1" ', 'id="a" ') })).status, "unwedged");
  } finally {
    await sink.close();
  }
});
