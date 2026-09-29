#!/usr/bin/env python3
"""Snapshot public PageLove documentation with provenance.

Crawls docs.pagelove.com (and a few named blog pages), stores raw HTML plus a
text rendering under research/docs/<date>/, and writes manifest.json recording
URL, fetch time, status, ETag, Last-Modified and sha256 for every page.

The snapshot directory is git-ignored: the pages are third-party content. The
manifest is what the compatibility report cites.
"""
import concurrent.futures
import datetime
import hashlib
import json
import re
import sys
import urllib.parse
import urllib.request
from html.parser import HTMLParser
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2] / "research" / "docs"
SEEDS = [
    "https://docs.pagelove.com/",
    "https://docs.pagelove.com/all/",
    "https://docs.pagelove.com/learn/",
    "https://docs.pagelove.com/recipes/",
    "https://docs.pagelove.com/reference/",
    "https://docs.pagelove.com/beta/",
    "https://blog.pagelove.com/",
    "https://blog.pagelove.com/posts/the-shape-of-pagelove.html",
]
ALLOWED_HOSTS = {"docs.pagelove.com", "blog.pagelove.com"}


class Text(HTMLParser):
    BLOCK = {"p", "div", "h1", "h2", "h3", "h4", "h5", "li", "pre", "br", "section", "tr", "dt", "dd", "table"}

    def __init__(self):
        super().__init__()
        self.out, self.links, self.skip = [], [], 0

    def handle_starttag(self, tag, attrs):
        d = dict(attrs)
        if tag in ("script", "style"):
            self.skip += 1
        if tag in self.BLOCK:
            self.out.append("\n")
        if tag == "a" and d.get("href"):
            self.links.append(d["href"])

    def handle_endtag(self, tag):
        if tag in ("script", "style"):
            self.skip -= 1

    def handle_data(self, data):
        if not self.skip:
            self.out.append(data)


def name_for(url):
    p = urllib.parse.urlparse(url)
    path = p.path.strip("/").replace("/", "_") or "index"
    return f"{p.netloc}_{path}"


def fetch(url):
    req = urllib.request.Request(url, headers={"User-Agent": "pagelike-research/0.1 (+compat research)"})
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            body = r.read()
            status = r.status
            headers = dict(r.headers)
    except Exception as e:  # noqa: BLE001
        return {"url": url, "error": str(e)}
    text = body.decode("utf-8", "replace")
    p = Text()
    p.feed(text)
    return {
        "url": url,
        "status": status,
        "etag": headers.get("etag") or headers.get("ETag"),
        "last_modified": headers.get("last-modified") or headers.get("Last-Modified"),
        "sha256": hashlib.sha256(body).hexdigest(),
        "bytes": len(body),
        "html": text,
        "text": re.sub(r"\n[ \t\n]+", "\n\n", "".join(p.out)),
        "links": [urllib.parse.urljoin(url, l).split("#")[0] for l in p.links],
    }


def main():
    day = sys.argv[1] if len(sys.argv) > 1 else datetime.date.today().isoformat()
    out = ROOT / day
    out.mkdir(parents=True, exist_ok=True)
    seen, frontier, manifest = set(), list(SEEDS), []
    fetched_at = datetime.datetime.now(datetime.timezone.utc).isoformat()
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        while frontier:
            batch = [u for u in dict.fromkeys(frontier) if u not in seen]
            frontier = []
            seen.update(batch)
            for res in pool.map(fetch, batch):
                if "error" in res:
                    manifest.append(res)
                    continue
                n = name_for(res["url"])
                (out / f"{n}.html").write_text(res["html"])
                (out / f"{n}.txt").write_text(res["text"])
                manifest.append({k: v for k, v in res.items() if k not in ("html", "text", "links")} | {"file": n, "chars": len(res["text"])})
                for link in res["links"]:
                    host = urllib.parse.urlparse(link).netloc
                    if host == "docs.pagelove.com" and link not in seen and not re.search(r"\.(css|js|svg|png|ico|xml|woff2?)$", link):
                        frontier.append(link)
    manifest.sort(key=lambda m: m["url"])
    (out / "manifest.json").write_text(json.dumps({"fetched_at": fetched_at, "pages": manifest}, indent=2))
    for m in manifest:
        print(m.get("status", "ERR"), m.get("chars", ""), m["url"], m.get("error", ""))


if __name__ == "__main__":
    main()
