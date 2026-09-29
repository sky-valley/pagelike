// ACC-00 — Official deploy tooling works against pagelike (docs/spec/apps.md
// §7.1): pagelove-shop's ops/deploy-pagelove.sh and beta-js's
// .github/scripts/sync-webdav.sh, each run twice against the authoring plane.
import { test, expect } from "@playwright/test";
import { createSite } from "../../lib/pagelike.mjs";
import { hasResearch, upstream, dav, deployShop, betaJsWorkingCopy, runBetaJsScript } from "../../lib/apps.mjs";
import { mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

test.skip(!hasResearch(), "research/ (upstream apps) not available");

const listed = (file) => readFileSync(file, "utf8").split("\n").map((l) => l.replace(/#.*$/, "").trim()).filter(Boolean);

test("ACC-00 official deploy tooling works against pagelike", { tag: "@tierA" }, async () => {
  test.setTimeout(180_000);
  // pagelove-shop, unmodified: every file is written, but the seed's
  // read-back check fails because <meta … content=""> is stored as
  // <meta … content> — as on PageLove (LO-15). The script stops there.
  const raw = createSite("acc00-shop-raw");
  const failed = deployShop("acc00-shop-raw", raw, mkdtempSync(join(tmpdir(), "shop-backup-")), { storedFormSeed: false });
  expect(failed.status, failed.out).toBe(1);
  expect(failed.out).toContain("Seed read-back mismatch: data/settings/shop.html");
  expect(await dav("acc00-shop-raw", raw).text("/data/settings/shop.html")).toContain('itemprop="checkoutEndpoint" content>');

  // With the seed written in the stored form: deploy twice in a row.
  const key = createSite("acc00-shop");
  const first = deployShop("acc00-shop", key, mkdtempSync(join(tmpdir(), "shop-backup-")));
  expect(first.status, first.out).toBe(0);
  const files = listed(upstream("pagelove-shop", "ops", "pagelove-files.txt"));
  for (const f of files) expect(first.out, f).toContain(`VERIFY ${f} -> exact match`);
  expect(first.out).toContain("SEED data/settings/shop.html -> created and verified");
  // An admin edits the seeded settings; a redeploy must keep them.
  const edited = (await dav("acc00-shop", key).text("/data/settings/shop.html")).replace(/(itemprop="checkoutEndpoint" content)(="[^"]*")?/, '$1="https://kept.example"');
  await dav("acc00-shop", key).putOk("/data/settings/shop.html", edited);

  const second = deployShop("acc00-shop", key, mkdtempSync(join(tmpdir(), "shop-backup-")));
  expect(second.status, second.out).toBe(0);
  // MKCOL on an existing collection is 405 (live 2026-09-29, decisions.md
  // protocol.webdav.mkcol), which the script accepts without a message.
  expect(second.out).not.toMatch(/MKCOL .* failed/);
  for (const f of files) expect(second.out, f).toContain(`VERIFY ${f} -> exact match`);
  expect(second.out).toContain("SEED data/settings/shop.html -> already present, left unchanged");
  expect(await dav("acc00-shop", key).text("/data/settings/shop.html")).toBe(edited);
  // The create-only seed PUT itself answers 412 on an existing document.
  const seed = await dav("acc00-shop", key).put("/data/settings/shop.html", readFileSync(upstream("pagelove-shop", "site", "data", "settings", "shop.html")), { "If-None-Match": "*" });
  expect(seed.status).toBe(412);

  // beta-js: sync --all twice, then a dry run.
  const bkey = createSite("acc00-betajs");
  const dir = betaJsWorkingCopy();
  const s1 = runBetaJsScript(dir, "sync-webdav.sh", "acc00-betajs", bkey, ["--all"]);
  expect(s1.status, s1.out).toBe(0);
  expect(s1.out).toContain("Sync complete.");
  const s2 = runBetaJsScript(dir, "sync-webdav.sh", "acc00-betajs", bkey, ["--all"]);
  expect(s2.status, s2.out).toBe(0);
  expect(s2.out).not.toMatch(/MKCOL .* -> HTTP/);
  expect(s2.out).toContain("Sync complete.");
  const dry = runBetaJsScript(dir, "sync-webdav.sh", "acc00-betajs", bkey, ["--all"], { DRY_RUN: "1" });
  expect(dry.status, dry.out).toBe(0);
  expect(dry.out).toMatch(/^ {2}PUT {4}pagelove\.mjs {2}\[text\/javascript\]$/m);
  // Every uploaded module reads back byte-for-byte.
  for (const f of ["pagelove.mjs", "pagelove/primitives.mjs", "pagelove/sse.mjs", "cors.html"]) {
    const got = Buffer.from(await (await dav("acc00-betajs", bkey).get("/" + f)).arrayBuffer());
    expect(got.equals(readFileSync(upstream("beta-js", ...f.split("/")))), f).toBe(true);
  }
});
