// Example experiences: participation, live collaboration across sessions,
// enforced ownership, iframes, remix with independent state, and shareable
// participation views tied to the originating experience and version.
import { test, expect } from "@playwright/test";
import { installExample, addUser, login, origin, davOrigin, cli, PNG, deploy, createSite } from "../lib/pagelike.mjs";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const PW = { alice: "alice-local-test-pw", bob: "bob-local-test-pw" };

test.describe.configure({ mode: "serial" });

let skyKey;
test.beforeAll(async () => {
  skyKey = await installExample("sky", "sky");
  addUser("sky", "alice", { name: "Alice", password: PW.alice });
  addUser("sky", "bob", { name: "Bob", password: PW.bob });
});

async function shareSky(page, caption) {
  await page.setInputFiles("#photo", { name: "sky.png", mimeType: "image/png", buffer: PNG });
  await page.fill("#caption", caption);
  await page.click('#share button[type="submit"]');
  await expect(page.locator("#status")).toHaveText("Shared. Thank you!");
}

test("sky: a participant shares a photo and others see it live; ownership is enforced", async ({ browser }) => {
  const alice = await (await browser.newContext()).newPage();
  const bob = await (await browser.newContext()).newPage();
  const anon = await (await browser.newContext()).newPage();
  await login(alice, "sky", "alice", PW.alice);
  await login(bob, "sky", "bob", PW.bob);
  await anon.goto(origin("sky") + "/");
  await expect(alice.locator("[data-me]")).toHaveText("Alice");
  await expect(anon.locator("#share")).toBeHidden();
  await bob.waitForTimeout(300); // let the streams open

  await shareSky(alice, "Orange dusk over the harbour");
  // Bob (another session) and an anonymous viewer see it live, without reloading.
  const entryForBob = bob.locator("#skies > li", { hasText: "Orange dusk" });
  await expect(entryForBob).toBeVisible({ timeout: 3000 });
  await expect(anon.locator("#skies > li", { hasText: "Orange dusk" })).toBeVisible({ timeout: 3000 });
  await expect(entryForBob.locator("img")).toHaveJSProperty("complete", true);

  // Bob gets no Remove button for Alice's sky, and the server refuses if he tries anyway.
  await expect(entryForBob.getByRole("button", { name: "Remove" })).toHaveCount(0);
  const id = await entryForBob.getAttribute("id");
  const status = await bob.evaluate(async (id) => (await fetch("/index.html", { method: "DELETE", headers: { Range: `selector=#${id}` } })).status, id);
  expect(status).toBe(403);
  // Bob cannot post a sky claiming to be Alice.
  const forged = await bob.evaluate(async () => (await fetch("/index.html", { method: "POST", headers: { Range: "selector=#skies" },
    body: '<li id="sky-forged" itemscope itemtype="https://example.org/Sky" data-owner="alice"><img itemprop="image" src="/uploads/alice/x.png" alt="x"><p itemprop="caption">forged</p></li>' })).status);
  expect(forged).toBe(403);
  // Nor plant HTML on the origin through the upload folder.
  const planted = await bob.evaluate(async () => (await fetch("/uploads/bob/evil.html", { method: "PUT", headers: { "Content-Type": "image/png" }, body: "<script>alert(1)</script>" })).status);
  expect(planted).toBe(415);

  // Alice's entry carries a share link to a participation view tied to this experience.
  const share = alice.locator("#skies > li", { hasText: "Orange dusk" }).getByRole("link", { name: "Share" });
  await expect(share).toBeVisible();
  const href = await share.getAttribute("href");
  const view = await (await browser.newContext()).newPage();
  await view.goto(origin("sky") + href);
  await expect(view.locator("body")).toContainText("Alice contributed to");
  await expect(view.locator("body")).toContainText("Show me your sky");
  await expect(view.locator("article img")).toHaveAttribute("src", /\/uploads\/alice\//);
});

test("sky: remix gets independent state, no participants, recorded lineage", async ({ browser }) => {
  cli("fork", "--from", "sky", "--to", "dog", "--note", "show me your dog");
  cli("identity", "set", "--site", "dog", "--cookies", "partitioned");
  const dogKey = cli("key", "create", "--site", "dog");
  // The remix author changes the invitation over the authoring plane.
  const res = await fetch(davOrigin("dog") + "/index.html", { method: "PUT",
    headers: { Authorization: `Bearer ${dogKey}`, Range: "selector=#invitation" }, body: '<h1 id="invitation">Show me your dog</h1>' });
  expect(res.status).toBe(206);
  addUser("dog", "carol", { name: "Carol", password: "carol-local-test-pw" });

  const page = await (await browser.newContext()).newPage();
  await page.goto(origin("dog") + "/");
  await expect(page.locator("#invitation")).toHaveText("Show me your dog");
  await expect(page.locator("#skies > li")).toHaveCount(0); // no participants copied
  // Accounts are not copied either: Alice has no account on the remix.
  const noAlice = await fetch(origin("dog") + "/-pagelike/login", { method: "POST", body: new URLSearchParams({ username: "alice", password: PW.alice }), redirect: "manual" });
  expect(noAlice.status).not.toBe(303);

  await login(page, "dog", "carol", "carol-local-test-pw");
  await shareSky(page, "Rex at the beach");
  // The original is untouched by the remix's participation.
  const orig = await (await browser.newContext()).newPage();
  await orig.goto(origin("sky") + "/");
  await expect(orig.locator("#skies")).not.toContainText("Rex at the beach");
  await expect(orig.locator("#invitation")).toHaveText("Show me your sky");
  // Lineage is visible on the remix's experience record and participation views.
  const exp = await (await fetch(origin("dog") + "/-pagelike/experience")).json();
  expect(exp.remix_of.site).toBe("sky");
  expect(exp.version).not.toBe(exp.remix_of.version);
  const parts = await (await fetch(origin("dog") + "/-pagelike/participations")).json();
  const entry = parts.participations.find((p) => p.element_id);
  const view = await (await browser.newContext()).newPage();
  await view.goto(origin("dog") + entry.share_url);
  await expect(view.locator(".lineage")).toContainText("A remix of sky");
});

test("sky: plays inside a cross-origin iframe, with live updates and in-frame sign-in", async ({ browser }) => {
  const feedKey = createSite("feed");
  await deploy("feed", feedKey, join(new URL("../../examples/feed/site", import.meta.url).pathname));
  const port = new URL(origin("feed")).port;
  // Sign-in pages refuse to be framed unless the site trusts the embedder.
  const before = await fetch(origin("sky") + "/-pagelike/login");
  expect(before.headers.get("content-security-policy")).toContain("frame-ancestors 'none'");
  cli("identity", "set", "--site", "sky", "--embed-origins", origin("feed").replace(/\/$/, ""));
  await new Promise((r) => setTimeout(r, 1200)); // the running server re-reads settings within 1 s
  const viewer = await (await browser.newContext()).newPage();
  await viewer.goto(origin("feed") + `/?port=${port}`);
  const frame = viewer.frameLocator("#sky");
  await expect(frame.locator("#invitation")).toHaveText("Show me your sky");
  await expect(frame.locator("#skies > li").first()).toBeVisible();

  // Sign in inside the frame (partitioned cookies keep the session in the embed).
  const inFrame = viewer.frame({ url: /sky\.localhost/ });
  await inFrame.goto(origin("sky") + "/auth/login?redirect=/");
  await inFrame.fill('input[name="username"]', "bob");
  await inFrame.fill('input[name="password"]', PW.bob);
  await Promise.all([inFrame.waitForURL(origin("sky") + "/"), inFrame.click('button[type="submit"]')]);
  await expect(frame.locator("[data-me]")).toHaveText("Bob");

  // A participant elsewhere posts; the embedded experience updates live.
  const alice = await (await browser.newContext()).newPage();
  await login(alice, "sky", "alice", PW.alice);
  await shareSky(alice, "Stars from the balcony");
  await expect(frame.locator("#skies > li", { hasText: "Stars from the balcony" })).toBeVisible({ timeout: 3000 });
  // And the embedded, signed-in participant can post from inside the frame.
  await inFrame.setInputFiles("#photo", { name: "sky.png", mimeType: "image/png", buffer: PNG });
  await inFrame.fill("#caption", "Posted from inside the feed");
  await inFrame.click('#share button[type="submit"]');
  await expect(alice.locator("#skies > li", { hasText: "Posted from inside the feed" })).toBeVisible({ timeout: 3000 });
});

test("poll: one vote per person, live tallies, no tampering", async ({ browser }) => {
  await installExample("poll", "poll");
  addUser("poll", "alice", { name: "Alice", password: PW.alice });
  addUser("poll", "bob", { name: "Bob", password: PW.bob });
  const alice = await (await browser.newContext()).newPage();
  const bob = await (await browser.newContext()).newPage();
  await login(alice, "poll", "alice", PW.alice);
  await login(bob, "poll", "bob", PW.bob);
  await bob.waitForTimeout(300);
  await alice.click('#options li[data-choice="garden"]');
  await expect(bob.locator('#options li[data-choice="garden"]')).toHaveAttribute("data-count", "1", { timeout: 3000 });
  await bob.click('#options li[data-choice="recipes"]');
  await expect(alice.locator(".total")).toHaveText("2 votes", { timeout: 3000 });
  // Changing a vote moves it rather than adding one.
  await alice.click('#options li[data-choice="recipes"]');
  await expect(bob.locator('#options li[data-choice="recipes"]')).toHaveAttribute("data-count", "2", { timeout: 3000 });
  await expect(bob.locator(".total")).toHaveText("2 votes");
  // A second vote and a vote cast as someone else are refused by the server.
  const second = await alice.evaluate(async () => (await fetch("/index.html", { method: "POST", headers: { Range: "selector=#votes" },
    body: '<tr id="vote-alice-2" itemscope itemtype="https://example.org/Vote" data-voter="alice" data-choice="soundwalk"><td itemprop="choice">soundwalk</td></tr>' })).status);
  expect(second).toBe(409);
  const tamper = await alice.evaluate(async () => (await fetch("/index.html", { method: "PUT", headers: { Range: "selector=#vote-bob" },
    body: '<tr id="vote-bob" itemscope itemtype="https://example.org/Vote" data-voter="bob" data-choice="garden"><td itemprop="choice">garden</td></tr>' })).status);
  expect(tamper).toBe(403);
  // The server-composed tally agrees.
  const html = await (await fetch(origin("poll") + "/")).text();
  expect(html).toContain('<p class="total">2 votes</p>');
});

test("board: composed lanes, state machine enforced, live refresh across sessions", async ({ browser }) => {
  await installExample("board", "board");
  addUser("board", "alice", { name: "Alice", password: PW.alice });
  addUser("board", "bob", { name: "Bob", password: PW.bob });
  const alice = await (await browser.newContext()).newPage();
  const bob = await (await browser.newContext()).newPage();
  await login(alice, "board", "alice", PW.alice);
  await login(bob, "board", "bob", PW.bob);
  await expect(alice.locator("#board-header h1")).toHaveText("Team board"); // included partial
  await bob.waitForTimeout(300);
  await alice.fill("#title", "Write the launch note");
  await alice.click('#new-card button[type="submit"]');
  const inTodo = bob.locator('[data-lane="todo"] .card', { hasText: "Write the launch note" });
  await expect(inTodo).toBeVisible({ timeout: 3000 });
  // An illegal jump is refused by the server's transition constraints.
  await alice.locator('[data-lane="todo"] .card', { hasText: "Write the launch note" }).getByRole("button", { name: "Skip to Done" }).click();
  await expect(alice.locator("#msg")).toContainText("isn't allowed");
  // Legal moves go through and show up for Bob live.
  await alice.locator('[data-lane="todo"] .card', { hasText: "Write the launch note" }).getByRole("button", { name: "Doing →" }).click();
  await expect(bob.locator('[data-lane="doing"] .card', { hasText: "Write the launch note" })).toBeVisible({ timeout: 3000 });
  await bob.locator('[data-lane="doing"] .card', { hasText: "Write the launch note" }).getByRole("button", { name: "Done →" }).click();
  await expect(alice.locator('[data-lane="done"] .card', { hasText: "Write the launch note" })).toBeVisible({ timeout: 3000 });
});
