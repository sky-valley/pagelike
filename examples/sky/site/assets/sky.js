// "Show me your sky" — a participation experience in plain HTML + HTTP.
// Data lives in /index.html (#skies); photos live in /uploads/<user>/.
// The server enforces ownership (see /rules.html); this script only drives UI.

const page = location.pathname.endsWith("/") ? location.pathname + "index.html" : location.pathname;
const skies = document.querySelector("#skies");
const form = document.querySelector("#share");
const statusEl = document.querySelector("#status");
const meEl = document.querySelector("[data-me]");
const me = meEl ? meEl.dataset.me : null;
const myName = meEl ? meEl.textContent.trim() : null;
let connection = null; // Pagelove-Connection token of our live stream
let shares = new Map(); // element id -> share URL (pagelike participation views)

const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);

function writeHeaders(extra = {}) {
  const h = { ...extra };
  if (connection) h["Pagelove-Connection"] = connection;
  return h;
}

function decorate(li) {
  if (!li || li.dataset.decorated) return;
  li.dataset.decorated = "1";
  const bar = document.createElement("div");
  bar.className = "actions";
  if (me && li.dataset.owner === me) {
    const del = document.createElement("button");
    del.type = "button";
    del.textContent = "Remove";
    del.onclick = async () => {
      const res = await fetch(page, { method: "DELETE", headers: writeHeaders({ Range: `selector=#${CSS.escape(li.id)}` }) });
      if (res.ok) li.remove();
    };
    bar.append(del);
  }
  const share = shares.get(li.id);
  if (share) {
    const a = document.createElement("a");
    a.href = share;
    a.textContent = "Share";
    a.target = "_top";
    bar.append(a);
  }
  li.append(bar);
}

async function loadShares() {
  // pagelike-only: participation views. On other runtimes this 404s and is ignored.
  try {
    const res = await fetch(`/-pagelike/participations?path=${encodeURIComponent(page)}`, { headers: { Accept: "application/json" } });
    if (!res.ok) return;
    const data = await res.json();
    shares = new Map(data.participations.filter((p) => p.element_id).map((p) => [p.element_id, p.share_url]));
  } catch {}
}

function refreshDecorations() {
  for (const li of skies.querySelectorAll(":scope > li")) {
    delete li.dataset.decorated;
    li.querySelector(":scope > .actions")?.remove();
    decorate(li);
  }
}

function fragment(html) {
  const t = document.createElement("template");
  t.innerHTML = html.trim();
  return t.content.firstElementChild;
}

// Live updates from other participants (the server never echoes our own writes).
function listen() {
  const es = new EventSource(page);
  es.addEventListener("pagelove-connection", (e) => { connection = e.data; });
  es.addEventListener("mutation", (e) => {
    const data = e.data;
    const get = (p) => {
      const m = data.match(new RegExp(`<span itemprop="${p}">([\\s\\S]*?)</span>`));
      return m ? m[1].replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&amp;/g, "&") : "";
    };
    const method = get("method"), selector = get("selector");
    const open = data.indexOf('<div itemprop="body">');
    const body = open < 0 ? "" : data.slice(open + 21, data.lastIndexOf("</div>"));
    if (method === "POST" && selector === "#skies") {
      const li = fragment(body);
      if (li && !document.getElementById(li.id)) {
        skies.prepend(li);
        loadShares().then(refreshDecorations);
      }
    } else if (method === "DELETE") {
      document.querySelector(selector)?.remove();
    } else if (method === "PUT" && selector) {
      const old = document.querySelector(selector), li = fragment(body);
      if (old && li) { old.replaceWith(li); decorate(li); }
    } else if (method === "PUT" && !selector) {
      location.reload(); // the author changed the page itself
    }
  });
  es.addEventListener("reset", () => location.reload());
}

async function share(ev) {
  ev.preventDefault();
  const file = document.querySelector("#photo").files[0];
  const caption = document.querySelector("#caption").value.trim();
  if (!file || !caption) return;
  const ext = ({ "image/png": "png", "image/jpeg": "jpg", "image/webp": "webp", "image/gif": "gif", "image/avif": "avif" })[file.type];
  if (!ext) { statusEl.textContent = "Please choose a PNG, JPEG, WebP, GIF or AVIF photo."; return; }
  const id = crypto.randomUUID().slice(0, 12);
  const src = `/uploads/${encodeURIComponent(me)}/${id}.${ext}`;
  statusEl.textContent = "Uploading…";
  const up = await fetch(src, { method: "PUT", headers: writeHeaders({ "Content-Type": file.type }), body: file });
  if (!up.ok) { statusEl.textContent = `Upload refused (${up.status}).`; return; }
  const now = new Date().toISOString();
  const entry = `<li id="sky-${id}" itemscope itemtype="https://example.org/Sky" data-owner="${esc(me)}">` +
    `<img itemprop="image" src="${esc(src)}" alt="${esc(caption)}">` +
    `<p itemprop="caption">${esc(caption)}</p>` +
    `<meta itemprop="author" content="${esc(myName)}">` +
    `<time itemprop="posted" datetime="${now}">${new Date(now).toLocaleString()}</time></li>`;
  const res = await fetch(page, { method: "POST", headers: writeHeaders({ Range: "selector=#skies; placement=prepend", "Content-Type": "text/html" }), body: entry });
  if (!res.ok) { statusEl.textContent = `Not shared (${res.status}): ${(await res.text()).replace(/<[^>]+>/g, " ").trim().slice(0, 160)}`; return; }
  const li = fragment(await res.text());
  if (li && !document.getElementById(li.id)) skies.prepend(li);
  form.reset();
  statusEl.textContent = "Shared. Thank you!";
  await loadShares();
  refreshDecorations();
}

if (me) {
  form.hidden = false;
  form.addEventListener("submit", share);
}
listen();
loadShares().then(refreshDecorations);
