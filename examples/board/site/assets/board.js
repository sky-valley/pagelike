// Team board: lanes are composed on the server; moves are PUTs of a card
// with a new status, checked by the server's state machine (422 if illegal).
const meEl = document.querySelector("[data-me]");
const me = meEl ? meEl.dataset.me : null;
const next = { todo: ["doing"], doing: ["todo", "done"], done: [] };
let connection = null;
const msg = document.querySelector("#msg");
const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
const headers = (extra) => (connection ? { ...extra, "Pagelove-Connection": connection } : extra);

async function readCard(key) {
  const res = await fetch("/cards.html", { headers: { Range: `selector=#${CSS.escape(key)}` } });
  return res.ok ? res.text() : null;
}

async function move(key, to) {
  const html = await readCard(key);
  if (!html) return;
  const updated = html.replace(/(<meta itemprop="status" content=")[^"]*(")/, `$1${to}$2`);
  const res = await fetch("/cards.html", { method: "PUT", headers: headers({ Range: `selector=#${CSS.escape(key)}`, "Content-Type": "text/html" }), body: updated });
  if (res.status === 422) {
    msg.textContent = "That move isn't allowed by the board's state machine.";
    return;
  }
  if (!res.ok) { msg.textContent = `Move refused (${res.status}).`; return; }
  msg.textContent = "";
  await refresh();
}

function decorate() {
  for (const card of document.querySelectorAll(".card")) {
    if (!me || card.querySelector(".moves")) continue;
    const bar = document.createElement("div");
    bar.className = "moves";
    for (const to of next[card.dataset.status] || []) {
      const b = document.createElement("button");
      b.type = "button";
      b.textContent = to === "todo" ? "← To do" : to === "doing" ? "Doing →" : "Done →";
      b.onclick = () => move(card.dataset.key, to);
      bar.append(b);
    }
    // Deliberately offer an illegal jump so the server-side rule is visible.
    if (card.dataset.status === "todo") {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "illegal";
      b.textContent = "Skip to Done";
      b.onclick = () => move(card.dataset.key, "done");
      bar.append(b);
    }
    card.append(bar);
  }
}

// Re-read the composed lanes (a selector read of the composed page).
async function refresh() {
  const res = await fetch("/index.html", { headers: { Range: "selector=#lanes" } });
  if (!res.ok) return;
  const t = document.createElement("template");
  t.innerHTML = (await res.text()).trim();
  document.querySelector("#lanes").replaceWith(t.content.firstElementChild);
  decorate();
}

async function add(ev) {
  ev.preventDefault();
  const title = document.querySelector("#title").value.trim();
  if (!title) return;
  const key = "card-" + crypto.randomUUID().slice(0, 8);
  const card = `<article id="${key}" itemscope itemtype="https://example.org/Card" data-owner="${esc(me)}">` +
    `<meta itemprop="key" content="${key}"><h3 itemprop="title">${esc(title)}</h3>` +
    `<meta itemprop="status" content="todo"><meta itemprop="owner" content="${esc(me)}"></article>`;
  const res = await fetch("/cards.html", { method: "POST", headers: headers({ Range: "selector=#cards", "Content-Type": "text/html" }), body: card });
  msg.textContent = res.ok ? "" : `Not added (${res.status}).`;
  if (res.ok) { ev.target.reset(); await refresh(); }
}

const es = new EventSource("/cards.html");
es.addEventListener("pagelove-connection", (e) => (connection = e.data));
es.addEventListener("mutation", () => refresh());

if (me) {
  const form = document.querySelector("#new-card");
  form.hidden = false;
  form.addEventListener("submit", add);
}
decorate();
