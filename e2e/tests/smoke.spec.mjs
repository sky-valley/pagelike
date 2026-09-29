import { test, expect } from "@playwright/test";
import { createSite, deploy, origin } from "../lib/pagelike.mjs";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

// Two browser contexts (two independent sessions) see each other's writes live.
test("two sessions see live updates over SSE", async ({ browser }) => {
  const key = createSite("smoke");
  const dir = mkdtempSync(join(tmpdir(), "smoke-"));
  writeFileSync(join(dir, "rules.html"), `<!DOCTYPE html><html><body><table><tr itemscope itemtype="https://pagelove.org/AuthorizationRule">
    <td itemprop="actor">*</td><td itemprop="resource">/*</td><td><span itemprop="method">GET</span><span itemprop="method">POST</span></td>
    <td itemprop="action">Allow</td></tr></table></body></html>`);
  writeFileSync(join(dir, "index.html"), `<!DOCTYPE html><html><head><title>smoke</title></head><body><ul id="list"></ul>
<script type="module">
  const es = new EventSource(location.pathname);
  es.addEventListener("mutation", (e) => {
    const doc = new DOMParser().parseFromString(e.data, "text/html");
    const body = doc.querySelector('[itemprop="body"]').innerHTML;
    document.querySelector("#list").insertAdjacentHTML("beforeend", body);
  });
  window.add = (text) => fetch(location.pathname, { method: "POST", headers: { Range: "selector=#list" }, body: "<li>" + text + "</li>" });
</script></body></html>`);
  await deploy("smoke", key, dir);
  const a = await (await browser.newContext()).newPage();
  const b = await (await browser.newContext()).newPage();
  await a.goto(origin("smoke") + "/index.html");
  await b.goto(origin("smoke") + "/index.html");
  await a.waitForTimeout(300);
  await a.evaluate(() => window.add("from-a"));
  await expect(b.locator("#list li")).toHaveText(["from-a"]);
});
