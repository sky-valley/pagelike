// Shared poll: one vote per signed-in person, enforced by the server
// (/rules.html). Tallies update live for everyone.
const page = "/index.html";
const meEl = document.querySelector("[data-me]");
const me = meEl ? meEl.dataset.me : null;
const votesEl = () => document.querySelector("#votes");
const options = [...document.querySelectorAll("#options li")];
let connection = null;

const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
const headers = (extra) => (connection ? { ...extra, "Pagelove-Connection": connection } : extra);
const voteId = (who) => "vote-" + who.replace(/[^A-Za-z0-9_-]/g, "_");

function render() {
  const counts = new Map(options.map((o) => [o.dataset.choice, 0]));
  let mine = null;
  for (const tr of votesEl().querySelectorAll("tr[data-choice]")) {
    counts.set(tr.dataset.choice, (counts.get(tr.dataset.choice) || 0) + 1);
    if (tr.dataset.voter === me) mine = tr.dataset.choice;
  }
  const total = [...counts.values()].reduce((a, b) => a + b, 0);
  for (const o of options) {
    const n = counts.get(o.dataset.choice) || 0;
    o.style.setProperty("--share", total ? n / total : 0);
    o.dataset.count = n;
    o.classList.toggle("mine", mine === o.dataset.choice);
  }
  document.querySelector(".total").textContent = `${total} vote${total === 1 ? "" : "s"}`;
}

async function vote(choice) {
  const id = voteId(me);
  const row = `<tr id="${id}" itemscope itemtype="https://example.org/Vote" data-voter="${esc(me)}" data-choice="${esc(choice)}"><td itemprop="choice">${esc(choice)}</td></tr>`;
  const existing = document.getElementById(id);
  const res = existing
    ? await fetch(page, { method: "PUT", headers: headers({ Range: `selector=#${id}`, "Content-Type": "text/html" }), body: row })
    : await fetch(page, { method: "POST", headers: headers({ Range: "selector=#votes", "Content-Type": "text/html" }), body: row });
  if (!res.ok) { alert(`Vote refused (${res.status})`); return; }
  const t = document.createElement("template");
  t.innerHTML = `<table><tbody>${(await res.text()).trim()}</tbody></table>`;
  const tr = t.content.querySelector("tr");
  existing ? existing.replaceWith(tr) : votesEl().append(tr);
  render();
}

// Seed the rows (the table body is plain data in the document).
fetch(page, { headers: { Range: "selector=#votes" } }).then((r) => r.ok && r.text()).then((html) => {
  if (!html) return;
  const t = document.createElement("template");
  t.innerHTML = `<table>${html}</table>`;
  votesEl().replaceWith(t.content.querySelector("tbody"));
  render();
});

const es = new EventSource(page);
es.addEventListener("pagelove-connection", (e) => (connection = e.data));
es.addEventListener("mutation", () => {
  // Re-read the votes on any change (rows are tiny).
  fetch(page, { headers: { Range: "selector=#votes" } }).then((r) => r.ok && r.text()).then((html) => {
    const t = document.createElement("template");
    t.innerHTML = `<table>${html}</table>`;
    document.querySelector("#votes").replaceWith(t.content.querySelector("tbody"));
    render();
  });
});

for (const o of options) {
  if (me) {
    o.tabIndex = 0;
    o.addEventListener("click", () => vote(o.dataset.choice));
  }
}
render();
