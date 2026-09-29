// Helpers for the release acceptance suite (docs/spec/apps.md §6–§8):
// locating the pinned upstream checkouts, installing apps, serving
// third-party modules from those checkouts, a network recorder, and small
// HTTP clients for both planes.
import { existsSync, readFileSync, readdirSync, statSync, mkdtempSync, cpSync, mkdirSync, writeFileSync } from "node:fs";
import { join, dirname, relative, sep } from "node:path";
import { tmpdir } from "node:os";
import { createHash } from "node:crypto";
import { execFileSync, spawnSync } from "node:child_process";
import httpServer from "node:http";
import { expect } from "@playwright/test";
import { repoRoot, origin, davOrigin, state, contentType, cli } from "./pagelike.mjs";

// ---------------------------------------------------------------------------
// Upstream material (git-ignored research/; MIT-licensed apps, docs snapshot)

/** The research/ directory: $PAGELIKE_RESEARCH, this checkout's research/,
 * or the nearest ancestor's (a worktree lives inside the main checkout). */
export function researchDir() {
  if (process.env.PAGELIKE_RESEARCH) return process.env.PAGELIKE_RESEARCH;
  let dir = repoRoot;
  for (;;) {
    if (existsSync(join(dir, "research", "upstream"))) return join(dir, "research");
    const up = dirname(dir);
    if (up === dir) return null;
    dir = up;
  }
}

export const hasResearch = () => researchDir() !== null;
export const upstream = (...p) => join(researchDir(), "upstream", ...p);

/** A page of the docs snapshot (research/docs/2026-09-28/md). */
export function docPage(name) {
  return readFileSync(join(researchDir(), "docs", "2026-09-28", "md", `docs.pagelove.com_${name}.md`), "utf8");
}

/** Fenced code blocks of a Markdown page, in order: [{lang, code}]. */
export function codeBlocks(md) {
  const out = [];
  const re = /^``` ?([\w-]*)\n([\s\S]*?)\n```$/gm;
  for (let m; (m = re.exec(md)); ) out.push({ lang: m[1], code: m[2] });
  return out;
}

// ---------------------------------------------------------------------------
// Installation

export function walk(dir) {
  const out = [];
  for (const name of readdirSync(dir).sort()) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...walk(p));
    else out.push(p);
  }
  return out;
}

/** A WebDAV client for one site (authoring plane, bearer key). */
export function dav(site, key) {
  const req = (method, path, { headers = {}, body } = {}) =>
    fetch(davOrigin(site) + encodeURI(path), { method, headers: { Authorization: `Bearer ${key}`, ...headers }, body });
  return {
    req,
    get: (path, headers) => req("GET", path, { headers }),
    text: async (path) => {
      const r = await req("GET", path);
      if (!r.ok) throw new Error(`dav GET ${path}: ${r.status}`);
      return r.text();
    },
    put: (path, body, headers = {}) =>
      req("PUT", path, { body, headers: { "Content-Type": contentType(path), ...headers } }),
    delete: (path, headers) => req("DELETE", path, { headers }),
    async putOk(path, body, headers = {}) {
      const r = await this.put(path, body, headers);
      if (!r.ok) throw new Error(`dav PUT ${path}: ${r.status} ${await r.text()}`);
      return r;
    },
  };
}

/** Upload the files of localDir (all, or the relative paths given) to the
 * site root over WebDAV, byte-for-byte; transform(rel, bytes) may edit. */
export async function install(site, key, localDir, { files, transform, skip } = {}) {
  const d = dav(site, key);
  const list = files ? files.map((f) => join(localDir, f)) : walk(localDir);
  const installed = [];
  for (const file of list) {
    const rel = "/" + relative(localDir, file).split(sep).join("/");
    if (skip && skip(rel)) continue;
    let body = readFileSync(file);
    if (transform) body = transform(rel, body) ?? body;
    await d.putOk(rel, body);
    installed.push(rel);
  }
  return installed;
}

export function siteExists(site) {
  return cli("site", "list").split("\n").includes(site);
}

/** Create the site unless it exists; return a fresh authoring key for it. */
export function ensureSite(site, { defaultGet = "allow" } = {}) {
  if (!siteExists(site)) cli("site", "create", site, "--default-get", defaultGet);
  return cli("key", "create", "--site", site);
}

/** A scratch copy of the pinned beta-js checkout as a fresh git repository
 * (its sync script lists files with `git ls-files`). */
export function betaJsWorkingCopy() {
  const dir = mkdtempSync(join(tmpdir(), "pagelike-betajs-"));
  cpSync(upstream("beta-js"), dir, { recursive: true, filter: (src) => !/\/(\.git|node_modules)(\/|$)/.test(src) });
  execFileSync("git", ["init", "-q"], { cwd: dir });
  execFileSync("git", ["add", "-A"], { cwd: dir });
  return dir;
}

/**
 * node_modules for an upstream project, installed once from its lock file
 * (or an explicit package list) into e2e/.cache and reused. Returns the
 * node_modules path, or null when the registry cannot be reached.
 */
export function npmDeps(name, { lockDir, packages }) {
  const cacheRoot = join(repoRoot, "e2e", ".cache");
  let manifest;
  if (lockDir) manifest = readFileSync(join(lockDir, "package-lock.json"));
  else manifest = Buffer.from(JSON.stringify(packages));
  const hash = createHash("sha256").update(manifest).digest("hex").slice(0, 12);
  const dir = join(cacheRoot, `${name}-${hash}`);
  const nm = join(dir, "node_modules");
  if (existsSync(join(dir, ".complete"))) return nm;
  mkdirSync(dir, { recursive: true });
  let r;
  if (lockDir) {
    cpSync(join(lockDir, "package.json"), join(dir, "package.json"));
    cpSync(join(lockDir, "package-lock.json"), join(dir, "package-lock.json"));
    r = spawnSync("npm", ["ci", "--no-audit", "--no-fund", "--ignore-scripts", "--prefer-offline"], { cwd: dir, encoding: "utf8", timeout: 300_000 });
  } else {
    writeFileSync(join(dir, "package.json"), JSON.stringify({ name: `pagelike-cache-${name}`, private: true }));
    r = spawnSync("npm", ["install", "--no-audit", "--no-fund", "--ignore-scripts", "--prefer-offline", "--legacy-peer-deps", ...packages],
      { cwd: dir, encoding: "utf8", timeout: 300_000 });
  }
  if (r.status !== 0) return null;
  writeFileSync(join(dir, ".complete"), "");
  return nm;
}

/** Run a beta-js .github/scripts/<script> against a pagelike site. */
export function runBetaJsScript(dir, script, site, key, args = [], env = {}) {
  const r = spawnSync("bash", [join(".github", "scripts", script), ...args], {
    cwd: dir, encoding: "utf8", timeout: 120_000,
    env: { ...process.env, PAGELOVE_WEBDAV_URL: `${davOrigin(site)}/`, PAGELOVE_API_KEY: key, ...env },
  });
  return { status: r.status, stdout: r.stdout, stderr: r.stderr, out: r.stdout + r.stderr };
}

/** Run pagelove-shop's ops/deploy-pagelove.sh as its GitHub workflow does
 * (site/ as the project dir, manifests from ops/) against a pagelike site. */
export function deployShop(site, key, backupDir, { storedFormSeed = true } = {}) {
  const shop = upstream("pagelove-shop");
  const r = spawnSync("sh", [join(shop, "ops", "deploy-pagelove.sh"), storedFormSeed ? shopSiteWithStoredFormSeed() : join(shop, "site"), backupDir], {
    encoding: "utf8", timeout: 120_000,
    env: {
      ...process.env, PAGELOVE_WEBDAV_URL: `${davOrigin(site)}/`, PAGELOVE_API_KEY: key,
      PAGELOVE_DEPLOY_MANIFEST: join(shop, "ops", "pagelove-files.txt"),
      PAGELOVE_DIRECTORIES_MANIFEST: join(shop, "ops", "pagelove-directories.txt"),
      PAGELOVE_SEED_MANIFEST: join(shop, "ops", "pagelove-seed-files.txt"),
    },
  });
  return { status: r.status, out: r.stdout + r.stderr };
}

/**
 * A working copy of pagelove-shop's site/ whose seed document is written in
 * the form PageLove stores HTML in (LO-15: an empty attribute value is
 * bare). The script cmp-checks the seed it just PUT, and on PageLove, as on
 * pagelike, `content=""` reads back as `content`, so the unmodified seed
 * fails that check (ACC-00 asserts this). Recorded in
 * docs/compat/app-changes.md.
 */
export function shopSiteWithStoredFormSeed() {
  const dir = mkdtempSync(join(tmpdir(), "shop-site-"));
  cpSync(upstream("pagelove-shop", "site"), dir, { recursive: true });
  const seed = join(dir, "data", "settings", "shop.html");
  writeFileSync(seed, readFileSync(seed, "utf8").replace(/(\s[\w:-]+)=""/g, "$1"));
  return dir;
}

/** Run ops/deploy-admin-password.sh (writes /private/admin.html). */
export function deployShopAdminPassword(site, key, password, backupDir) {
  const shop = upstream("pagelove-shop");
  const r = spawnSync("sh", [join(shop, "ops", "deploy-admin-password.sh"), backupDir], {
    encoding: "utf8", timeout: 60_000,
    env: { ...process.env, PAGELOVE_WEBDAV_URL: `${davOrigin(site)}/`, PAGELOVE_API_KEY: key, PAGELOVE_ADMIN_PASSWORD: password },
  });
  return { status: r.status, out: r.stdout + r.stderr };
}

/** The `betajs` site (§6.3): beta-js synced with its own sync-webdav.sh --all. */
export function ensureBetaJsSite(site = "betajs") {
  if (siteExists(site)) return;
  const key = ensureSite(site);
  const r = runBetaJsScript(betaJsWorkingCopy(), "sync-webdav.sh", site, key, ["--all"]);
  if (r.status !== 0) throw new Error(`sync-webdav.sh --all: ${r.out}`);
}

// ---------------------------------------------------------------------------
// Public-plane HTTP from the test process (a fresh anonymous client unless a
// cookie is passed).

export function http(site, { cookie, headers: base = {} } = {}) {
  return async (method, path, { headers = {}, body, redirect = "manual" } = {}) => {
    const h = { ...base, ...headers };
    if (cookie) h.Cookie = cookie;
    const res = await fetch(origin(site) + path, { method, headers: h, body, redirect });
    const text = method === "HEAD" ? "" : await res.text();
    return { status: res.status, headers: res.headers, text, res };
  };
}

/** Sign in with a local account and return the session cookie header. */
export async function sessionCookie(site, user, password) {
  const res = await fetch(origin(site) + "/-pagelike/login", {
    method: "POST",
    body: new URLSearchParams({ username: user, password }),
    redirect: "manual",
  });
  const set = res.headers.getSetCookie();
  if (!set.length) throw new Error(`login ${user}: ${res.status}`);
  return set.map((c) => c.split(";")[0]).join("; ");
}

// ---------------------------------------------------------------------------
// Third-party modules (§6.2)

const DOM_SUBSCRIBER_CDN = "https://cdn.pagelove.net/js/dom-subscriber/cde4007/index.mjs";

/** dom-subscriber at cde4007. The pinned checkout is 6b77709, whose
 * index.mjs beta-js vendors verbatim and documents as identical to cde4007
 * (beta-js pagelove/dom-subscriber.mjs header). Set PAGELIKE_E2E_GIT_SHOW=1
 * to read the blob with `git show cde4007:index.mjs` instead (§6.2). */
export function domSubscriberSource() {
  const repo = upstream("dom-subscriber");
  if (process.env.PAGELIKE_E2E_GIT_SHOW === "1") {
    return execFileSync("git", ["show", "cde4007:index.mjs"], { cwd: repo });
  }
  return readFileSync(join(repo, "index.mjs"));
}

const JS_HEADERS = { "Content-Type": "text/javascript", "Access-Control-Allow-Origin": "*" };

/**
 * Route third-party URLs for a browser context:
 *  - https://pagelove.github.io/beta-js/** from the pinned beta-js checkout,
 *    or (betajsSite) fetched from that pagelike site and passed through
 *    with the headers pagelike sent (ACC-BJ-3);
 *  - dom-subscriber cde4007 from the pinned checkout;
 *  - Google Fonts aborted; jszip from the network when reachable (ATS CV zip).
 */
export async function routeThirdParty(context, { betajsSite } = {}) {
  await context.route("https://pagelove.github.io/beta-js/**", async (route) => {
    const rel = new URL(route.request().url()).pathname.replace(/^\/beta-js\//, "");
    if (betajsSite) {
      const response = await route.fetch({ url: `${origin(betajsSite)}/${rel}` });
      return route.fulfill({ response });
    }
    const file = upstream("beta-js", ...rel.split("/"));
    if (!existsSync(file)) return route.fulfill({ status: 404, body: "not in pinned beta-js" });
    return route.fulfill({ status: 200, headers: JS_HEADERS, body: readFileSync(file) });
  });
  await context.route(DOM_SUBSCRIBER_CDN, (route) =>
    route.fulfill({ status: 200, headers: JS_HEADERS, body: domSubscriberSource() }));
  await context.route(/^https:\/\/fonts\.(googleapis|gstatic)\.com\//, (route) => route.abort());
}

/** A new browser context with third-party routing and an attached recorder. */
export async function newSession(browser, opts = {}) {
  const { betajsSite, ...ctxOpts } = opts;
  const context = await browser.newContext(ctxOpts);
  await routeThirdParty(context, { betajsSite });
  const net = new NetLog();
  net.attach(context);
  const page = await context.newPage();
  const errors = watchConsole(page);
  return { context, page, net, errors };
}

// ---------------------------------------------------------------------------
// Network recorder (§6.1)

const REQ_HEADERS = ["range", "destination", "destination-range", "pagelove-connection", "if-match", "if-none-match", "content-type", "accept", "last-event-id"];
const RES_HEADERS = ["content-type", "content-range", "etag", "location", "cache-control", "www-authenticate", "access-control-allow-origin"];

export class NetLog {
  constructor() {
    this.entries = [];
    this.byReq = new Map();
  }
  attach(context) {
    context.on("request", (req) => {
      const u = new URL(req.url());
      const h = req.headers();
      const e = {
        method: req.method(), url: req.url(), host: u.host, path: u.pathname, query: u.search,
        req: Object.fromEntries(REQ_HEADERS.filter((k) => h[k] !== undefined).map((k) => [k, h[k]])),
        authorization: h.authorization !== undefined, body: req.postData(), status: null, res: null, t: Date.now(),
      };
      this.byReq.set(req, e);
      this.entries.push(e);
    });
    context.on("response", async (res) => {
      const e = this.byReq.get(res.request());
      if (!e) return;
      e.status = res.status();
      const h = res.headers();
      e.res = Object.fromEntries(RES_HEADERS.filter((k) => h[k] !== undefined).map((k) => [k, h[k]]));
      e.response = res;
    });
    context.on("requestfailed", (req) => {
      const e = this.byReq.get(req);
      if (e) e.failed = req.failure()?.errorText || "failed";
    });
  }
  /** Entries matching a filter {method, path (string|RegExp), host, range, since}. */
  find(f = {}) {
    return this.entries.filter((e) =>
      (!f.method || e.method === f.method) &&
      (!f.host || e.host.startsWith(f.host)) &&
      (!f.path || (f.path instanceof RegExp ? f.path.test(e.path) : e.path === f.path)) &&
      (f.range === undefined || (f.range instanceof RegExp ? f.range.test(e.req.range ?? "") : e.req.range === f.range)) &&
      (!f.since || e.t >= f.since) &&
      (!f.pred || f.pred(e)));
  }
  /** Writes (PUT/POST/DELETE/MOVE/PATCH) to the app's own origin. */
  writes(f = {}) {
    return this.find(f).filter((e) => ["PUT", "POST", "DELETE", "MOVE", "PATCH"].includes(e.method) && e.url.startsWith("http://"));
  }
  mark() {
    return Date.now();
  }
  /** Wait until an entry matching f has a status; returns it. */
  async waitFor(f, { timeout = 10_000 } = {}) {
    const end = Date.now() + timeout;
    for (;;) {
      const hit = this.find(f).find((e) => e.status !== null || e.failed);
      if (hit) return hit;
      if (Date.now() > end) throw new Error(`no request matching ${JSON.stringify(f, (k, v) => (v instanceof RegExp ? String(v) : v))}`);
      await new Promise((r) => setTimeout(r, 25));
    }
  }
  /** Wait until every matching request so far has completed. */
  async settle(f = {}, { timeout = 10_000 } = {}) {
    const end = Date.now() + timeout;
    while (this.find(f).some((e) => e.status === null && !e.failed && !isStream(e))) {
      if (Date.now() > end) break;
      await new Promise((r) => setTimeout(r, 25));
    }
  }
}

const isStream = (e) => (e.req.accept || "").includes("text/event-stream");

export const ok2xx = (s) => s >= 200 && s < 300;

export function watchConsole(page) {
  const errors = [];
  page.on("pageerror", (e) => errors.push(`pageerror: ${e.name}: ${e.message}`));
  page.on("console", (m) => {
    if (m.type() === "error") errors.push(`console: ${m.text()}`);
  });
  return errors;
}

/** Uncaught exceptions and console errors (CSP violations included), minus
 * the browser's "Failed to load resource" lines for non-2xx answers the apps
 * expect and handle (e.g. the 416 of an upsert) and aborted font loads;
 * statuses are asserted from the network recorder instead (§6.1). */
export function realErrors(errors) {
  return errors.filter((e) => !/fonts\.(googleapis|gstatic)\.com|net::ERR_FAILED|ERR_ABORTED|Failed to load resource: the server responded with a status of/.test(e));
}

/** Parse a multipart/mixed body (OPTIONS 207) into [{headers, body}]. */
export function multipart(body, contentTypeHeader) {
  const m = /boundary="?([^";]+)"?/.exec(contentTypeHeader || "");
  if (!m) return [];
  const parts = body.split(`--${m[1]}`).slice(1);
  const out = [];
  for (const p of parts) {
    if (p.startsWith("--")) break;
    const [head, ...rest] = p.replace(/^\r?\n/, "").split(/\r?\n\r?\n/);
    const headers = {};
    for (const line of head.split(/\r?\n/)) {
      const i = line.indexOf(":");
      if (i > 0) headers[line.slice(0, i).trim().toLowerCase()] = line.slice(i + 1).trim();
    }
    out.push({ headers, body: rest.join("\n\n") });
  }
  return out;
}

/** A local HTTP sink for outbound requests sent by reactions (the server
 * runs with --outbound-allow 127.0.0.0/8). respond(path, [500, 500, 200])
 * scripts the statuses returned to successive requests (the last repeats). */
export async function startSink() {
  const hits = [];
  const scripts = new Map();
  const server = httpServer.createServer((req, res) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => {
      const path = new URL(req.url, "http://sink").pathname;
      hits.push({ method: req.method, path, headers: req.headers, body: Buffer.concat(chunks).toString("utf8"), at: Date.now() });
      const script = scripts.get(path) || [200];
      const status = script.length > 1 ? script.shift() : script[0];
      res.writeHead(status, { "Content-Type": "text/plain" }).end("ok");
    });
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const base = `http://127.0.0.1:${server.address().port}`;
  return {
    url: (path) => base + path,
    hits: (path) => hits.filter((h) => !path || h.path === path),
    respond: (path, statuses) => scripts.set(path, [...statuses]),
    close: () => new Promise((r) => server.close(r)),
  };
}

/** Accept any 2xx status. */
export function expect2xx(status, what = "") {
  expect(status, what).toBeGreaterThanOrEqual(200);
  expect(status, what).toBeLessThan(300);
}

export { origin, davOrigin, state, repoRoot };
