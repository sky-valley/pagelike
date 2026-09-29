// Helpers for driving a local pagelike instance from Playwright tests.
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, readdirSync, readFileSync, statSync, writeFileSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, relative, sep, extname } from "node:path";
import net from "node:net";
import { createHash } from "node:crypto";

export const repoRoot = new URL("../..", import.meta.url).pathname.replace(/\/$/, "");
// One state file per checkout, so e2e runs in different worktrees (or the
// main checkout) never talk to each other's server.
const stateFile = join(tmpdir(), `pagelike-e2e-state-${createHash("sha256").update(repoRoot).digest("hex").slice(0, 12)}.json`);

export function state() {
  return JSON.parse(readFileSync(stateFile, "utf8"));
}

function freePort() {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.listen(0, "127.0.0.1", () => {
      const { port } = s.address();
      s.close(() => resolve(port));
    });
    s.on("error", reject);
  });
}

export async function startServer() {
  const bin = join(repoRoot, "bin", "pagelike");
  execFileSync("go", ["build", "-o", bin, "./cmd/pagelike"], { cwd: repoRoot, stdio: "inherit" });
  const data = mkdtempSync(join(tmpdir(), "pagelike-e2e-"));
  const port = await freePort();
  // Loopback destinations are allowed for reactions so the acceptance
  // suites can capture outbound HTTP with a local sink (apps.md §6.3 ACC-AT-8).
  const proc = spawn(bin, ["serve", "--data", data, "--listen", `127.0.0.1:${port}`, "--outbound-allow", "127.0.0.0/8"],
    { stdio: ["ignore", process.env.PAGELIKE_E2E_LOG ? "inherit" : "ignore", "inherit"], detached: true });
  // wait for the port
  for (let i = 0; i < 100; i++) {
    try {
      const r = await fetch(`http://127.0.0.1:${port}/`);
      if (r.status) break;
    } catch {}
    await new Promise((r) => setTimeout(r, 100));
  }
  const st = { bin, data, port, pid: proc.pid };
  writeFileSync(stateFile, JSON.stringify(st));
  proc.unref();
  return st;
}

export async function stopServer() {
  if (!existsSync(stateFile)) return;
  const st = state();
  try { process.kill(st.pid, "SIGTERM"); } catch { return; }
  // Graceful shutdown waits up to 5 s for open streams; never leave the
  // server behind (it would hold the runner's stderr open).
  for (let i = 0; i < 70; i++) {
    await new Promise((r) => setTimeout(r, 100));
    try { process.kill(st.pid, 0); } catch { return; }
  }
  try { process.kill(st.pid, "SIGKILL"); } catch {}
}

export function cli(...args) {
  const st = state();
  return execFileSync(st.bin, [...args, "--data", st.data], { encoding: "utf8" }).trim();
}

export function origin(site) {
  return `http://${site}.localhost:${state().port}`;
}

export function davOrigin(site) {
  return `http://dav-${site}.localhost:${state().port}`;
}

/** Create a site and an authoring key scoped to it. Returns the key. */
export function createSite(site, { defaultGet = "allow" } = {}) {
  cli("site", "create", site, "--default-get", defaultGet);
  return execFileSync(state().bin, ["key", "create", "--site", site, "--data", state().data], { encoding: "utf8" }).trim();
}

export function addUser(site, user, { email = "", verified = true, name = "", roles = "", password }) {
  const args = ["user", "add", "--site", site, user, "--password", password];
  if (email) args.push("--email", email);
  if (verified) args.push("--verified");
  if (name) args.push("--name", name);
  if (roles) args.push("--roles", roles);
  cli(...args);
}

const types = { ".html": "text/html", ".css": "text/css", ".js": "text/javascript", ".mjs": "text/javascript", ".cjs": "text/javascript",
  ".json": "application/json", ".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
  ".gif": "image/gif", ".webp": "image/webp", ".ico": "image/x-icon", ".txt": "text/plain", ".xml": "application/xml",
  ".woff2": "font/woff2", ".woff": "font/woff", ".pdf": "application/pdf", ".md": "text/markdown" };

/** The Content-Type the suites upload a file with (by extension). */
export function contentType(path) {
  return types[extname(path).toLowerCase()] || "application/octet-stream";
}

function walk(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...walk(p));
    else out.push(p);
  }
  return out;
}

/** Upload every file under localDir to the site over WebDAV. */
export async function deploy(site, key, localDir, { transform } = {}) {
  for (const file of walk(localDir)) {
    const rel = "/" + relative(localDir, file).split(sep).join("/");
    let body = readFileSync(file);
    if (transform) body = transform(rel, body) ?? body;
    const res = await fetch(davOrigin(site) + encodeURI(rel), {
      method: "PUT",
      headers: { Authorization: `Bearer ${key}`, "Content-Type": types[extname(file)] || "application/octet-stream" },
      body,
    });
    if (!res.ok) throw new Error(`deploy ${rel}: ${res.status} ${await res.text()}`);
  }
}

/** Sign a browser context in through the site's login form. */
export async function signIn(page, site, user, password) {
  await page.goto(origin(site) + "/auth/login");
  await page.fill('input[name="username"]', user);
  await page.fill('input[name="password"]', password);
  await Promise.all([page.waitForNavigation(), page.click('button[type="submit"]')]);
}

/** Install an example from examples/<name>/site into a new site with
 * embeddable (partitioned) cookies; returns the authoring key. */
export async function installExample(example, site) {
  const key = createSite(site);
  cli("identity", "set", "--site", site, "--cookies", "partitioned");
  await deploy(site, key, join(repoRoot, "examples", example, "site"));
  return key;
}

/** Sign in through the site's login flow (local accounts). */
export async function login(page, site, user, password) {
  await page.goto(origin(site) + "/auth/login?redirect=/");
  await page.fill('input[name="username"]', user);
  await page.fill('input[name="password"]', password);
  await Promise.all([page.waitForURL(origin(site) + "/"), page.click('button[type="submit"]')]);
}

/** A tiny valid PNG (1x1 pixel) for upload tests. */
export const PNG = Buffer.from(
  "89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c4890000000d49444154789c6360f8cfc0f01f0005000201e2b1e3b30000000049454e44ae426082",
  "hex",
);
