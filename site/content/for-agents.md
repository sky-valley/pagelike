---
title: For agents
description: How agents should consume the pagelike docs site.
---

# For agents

This site is built to be readable by an agent that has never heard of
pagelike and has no browser. The short version:

1. Start at [`/llms.txt`](/llms.txt) for a one-page manifest.
2. Use [`/llms-full.txt`](/llms-full.txt) (or [`/index.json`](/index.json))
   for a complete, structured catalog.
3. Read [`/spec/`](/spec/) to verify behaviour against the documented
   requirements; each requirement has its own URL.
4. Walk [`/build/`](/build/) for tutorials that take you from "fresh
   install" to "page that uses selectors, microdata permissions,
   schemas, Liquid, SSE".

## Where things live

| Resource | URL |
|---|---|
| Concise manifest | `/llms.txt` |
| Full docs dump with TOC | `/llms-full.txt` |
| Structured page catalog (JSON) | `/index.json` |
| Crawl sitemap | `/sitemap.xml` |
| Crawl allow rules | `/robots.txt` |

`/llms.txt` is the same shape as `llms.txt` at `github.com/sky-valley/pagelike`,
so an agent that has read the README can find the site the same way
regardless of whether its first contact is GitHub or GitHub Pages.

## Per-page affordances

Every page on this site carries the same content in every shape an
agent can fetch:

| Shape | URL |
|---|---|
| HTML | `<page>/index.html` |
| Markdown source | `<page>/index.md`, also served when the request carries `Accept: text/markdown` |
| Structured metadata | JSON-LD `Schema.org` payload in `<script type="application/ld+json">` on the HTML page |
| Per-page JSON catalog | `<page>/index.json` |

Every page also emits:

- `<link rel="canonical">` — the absolute, public URL.
- `<link rel="alternate" type="text/markdown">` — the markdown mirror.
- `<link rel="alternate" type="application/json">` — the JSON mirror.
- Open Graph (`og:title`, `og:description`, `og:type=article`,
  `og:site_name=pagelike`) and Twitter card tags.
- `Schema.org` JSON-LD: `SoftwareSourceCode` for the home,
  `TechArticle` for spec pages, `TechArticle` for prose pages.

## Specification has stable URLs

The behavioural spec for pagelike lives at `/spec/`. Each requirement
is its own page at `/spec/<area>/<R-ID>/`, and the test harness at
`harness/cases/<area>/<name>.yaml` covers it. Defects surfaced here
are tracked in the issue tracker with the `compatibility` label.

Per-area indexes:

- [Application compatibility](/spec/apps/)
- [Composition](/spec/composing/)
- [Server JavaScript](/spec/javascript/)
- [Liquid templates](/spec/liquid/)
- [Modelling (schemas)](/spec/modeling/)
- [Permissions and identity](/spec/permissions-identity/)
- [Wire protocol](/spec/protocol/)
- [Reactions](/spec/reacting/)
- [Reading and writing](/spec/reading-writing/)
- [Sessel query language](/spec/sessel/)
- [Server-sent events](/spec/sse/)

## How to learn enough to run / build

[`/build/`](/build/) is a tutorial series. Each tutorial is grounded
in a runnable [`/examples/<name>/`](/examples/) app. You can install it
locally with `examples/install.sh <name> <site>`, set the OIDC/cookies
you need, and read the source HTML.

## Cross-cuts

- The repo's [`AGENTS.md`](/agents/) describes the rules coding agents
  that work on the project itself must follow.
- [`/decisions/`](/decisions/) are the architectural decision records.
  Most cited: 0003 (HTML and selectors), 0004 (document model), 0005
  (divergence policy), 0006 (security divergences).
- [`/compat/report/`](/compat/report/) lists exactly which behaviours
  match PageLove, which differ on purpose, and which have no
  explanation yet.

## When something is wrong

If a page is broken, open an issue at
`github.com/sky-valley/pagelike/issues` with the URL and the fetcher's
user agent. If the issue is a security vulnerability, follow
[`/security/`](/security/).
