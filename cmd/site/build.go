package main

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// sitePlan describes the build. The defaults match the repo layout so the
// binary works without flags when run from the repo root.
type sitePlan struct {
	srcRoot string
	outDir  string
	baseURL string
	now     time.Time
}

func cmdBuild(args []string) error {
	pl := sitePlan{
		srcRoot: ".",
		outDir:  "site/public",
		baseURL: "https://sky-valley.github.io/pagelike",
		now:     time.Now().UTC(),
	}
	fs := flagSet("build")
	fs.StringVar(&pl.srcRoot, "src", pl.srcRoot, "repo root (used as the source tree)")
	fs.StringVar(&pl.outDir, "out", pl.outDir, "output directory")
	fs.StringVar(&pl.baseURL, "base-url", pl.baseURL, "canonical URL prefix (used in <head> and JSON-LD)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if abs, err := filepath.Abs(pl.srcRoot); err == nil {
		pl.srcRoot = abs
	}
	b := &builder{plan: pl}
	return b.run()
}

// builder turns the repo's markdown into a static HTML site. It collects
// every page source from a fixed inventory plus an auto-walk of
// site/content/, renders each, then writes the agent-aware primitives.
type builder struct {
	plan  sitePlan
	pages []page
	specs []specSource
}

// page ties together everything we know about a single output file. A
// page can produce an HTML file, a Markdown mirror, and contributes to
// the site-wide index.
type page struct {
	URLPath     string
	Title       string
	Description string
	BodyHTML    []byte
	Markdown    []byte
	Modified    time.Time
	Type        string // index | page | spec | requirement
}

func (b *builder) run() error {
	if err := os.RemoveAll(b.plan.outDir); err != nil {
		return err
	}
	if err := os.MkdirAll(b.plan.outDir, 0o755); err != nil {
		return err
	}
	if err := b.collect(); err != nil {
		return err
	}
	if err := b.render(); err != nil {
		return err
	}
	if err := b.write(); err != nil {
		return err
	}
	if err := b.writeStatic(); err != nil {
		return err
	}
	if err := b.writeAgentPrimitives(); err != nil {
		return err
	}
	return nil
}

// collect walks three trees and dedupes by repo path so a file with a
// known URL mapping is never silently double-collected. The trees are
// the top-level repo (README, AGENTS, CONTRIBUTING, SECURITY, LICENSE),
// docs/ (architecture, design, spec/* via the spec preprocessor,
// decisions/*, compat/* minus live-observations), and site/content/
// (build tutorials, for-agents, examples, etc.).
func (b *builder) collect() error {
	handled := map[string]bool{}

	// Top-level markdown files at the repo root.
	for _, src := range defaultInventory(b.plan.srcRoot) {
		if src.Path == "" || handled[src.Path] {
			continue
		}
		handled[src.Path] = true
		if err := b.consumeSource(src); err != nil {
			return fmt.Errorf("%s: %w", src.Path, err)
		}
	}

	// Everything else under docs/, plus site/content/ for additions.
	for _, root := range []string{
		filepath.Join(b.plan.srcRoot, "docs"),
		filepath.Join(b.plan.srcRoot, "site", "content"),
	} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				return nil
			}
			name := strings.ToLower(d.Name())
			if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".json") {
				return nil
			}
			if d.Name() == "live-observations.md" {
				// Per AGENTS.md, tightly-controlled observations do not
				// belong on a discoverable public mirror.
				return nil
			}
			rel, err := filepath.Rel(b.plan.srcRoot, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if handled[rel] {
				return nil
			}
			// Skip a site/content/index.md that would collide with the
			// README-derived /.
			urlPath, _ := repoPathToURL(rel)
			if urlPath == "/" {
				return nil
			}
			handled[rel] = true
			if strings.HasSuffix(name, ".json") {
				return b.consumeStatic(rel)
			}
			return b.consumeSource(pageSource{Path: rel, FromContent: true, URLPath: urlPath})
		})
		if err != nil {
			return err
		}
	}
	return nil
}

type pageSource struct {
	Path        string
	FromContent bool
	URLPath     string
}

// consumeSource reads the file at the given repo-relative path and
// appends a page record to the builder. Spec-area files get sliced into
// one page per requirement via the spec preprocessor.
func (b *builder) consumeSource(src pageSource) error {
	abs := filepath.Join(b.plan.srcRoot, src.Path)
	data, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	if strings.HasPrefix(src.Path, "docs/spec/") && strings.HasSuffix(src.Path, ".md") {
		return b.consumeSpec(abs, data)
	}
	url := src.URLPath
	if url == "" {
		url, _ = repoPathToURL(src.Path)
	}
	title, desc := deriveTitleAndDescription(url, string(data))
	p := page{
		URLPath:     url,
		Title:       title,
		Description: desc,
		Markdown:    data,
		Modified:    lastmodTime(abs),
	}
	b.pages = append(b.pages, p)
	return nil
}

// consumeSpec splits a docs/spec/<area>.md file into one page per
// requirement and one per-area index page.
func (b *builder) consumeSpec(path string, data []byte) error {
	reqs, err := parseSpec(path, data)
	if err != nil {
		return err
	}
	area := pathArea(path)
	b.specs = append(b.specs, specSource{
		Area:      area,
		Path:      path,
		Modified:  lastmodTime(path),
		IndexText: buildSpecIndex(area, reqs),
		Reqs:      reqs,
	})
	return nil
}

// consumeStatic copies a JSON (or other non-markdown) asset from
// site/content/ or docs/ straight through to the output. Used for
// /commands.json and any future fixture.
func (b *builder) consumeStatic(rel string) error {
	abs := filepath.Join(b.plan.srcRoot, rel)
	data, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	urlPath, _ := repoPathToURL(rel)
	out := path.Join(b.plan.outDir, strings.TrimPrefix(urlPath, "/"))
	if err := os.MkdirAll(path.Dir(out), 0o755); err != nil {
		return err
	}
	return writeFile(out, data)
}

// render passes every page through the layout template. Markdown bodies
// go through Goldmark and are wrapped in the page chrome; mirrored .md
// files are written byte-for-byte.
func (b *builder) render() error {
	for i := range b.pages {
		if err := b.renderOne(&b.pages[i]); err != nil {
			return err
		}
	}
	for i := range b.specs {
		for j := range b.specs[i].Reqs {
			req := &b.specs[i].Reqs[j]
			body, err := renderMarkdown(req.Body)
			if err != nil {
				return err
			}
			req.BodyHTML = body
		}
		idxBody, err := renderMarkdown([]byte(b.specs[i].IndexText))
		if err != nil {
			return err
		}
		b.specs[i].IndexHTML = idxBody
	}
	return nil
}

func (b *builder) renderOne(p *page) error {
	if p.URLPath == "/" {
		// The home gets a stripped body for the description.
		p.Description = firstParagraph(p.Markdown)
	}
	body, err := renderMarkdown(p.Markdown)
	if err != nil {
		return err
	}
	p.BodyHTML = body
	return nil
}

// write flushes every HTML and .md mirror under b.plan.outDir.
func (b *builder) write() error {
	for _, p := range b.pages {
		if err := b.writePage(p); err != nil {
			return err
		}
	}
	for _, s := range b.specs {
		if err := b.writeSpecArea(s); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) writePage(p page) error {
	view := b.pageView(p)
	html, err := renderLayout(view)
	if err != nil {
		return err
	}
	outFile := urlToOutFile(b.plan.outDir, p.URLPath)
	if err := os.MkdirAll(filepath.Dir(outFile), 0o755); err != nil {
		return err
	}
	if err := writeFile(outFile, html); err != nil {
		return err
	}
	mdPath := path.Join(path.Dir(outFile), "index.md")
	return writeFile(mdPath, prependFrontMatter(p, p.Markdown))
}

func (b *builder) writeSpecArea(s specSource) error {
	areaURL := "/spec/" + s.Area + "/"
	p := page{
		URLPath:     areaURL,
		Title:       titleForArea(s.Area),
		Description: "Requirements for " + titleForArea(s.Area),
		Markdown:    []byte(s.IndexText),
		Modified:    s.Modified,
	}
	view := b.pageView(p)
	html, err := renderLayout(view)
	if err != nil {
		return err
	}
	outFile := urlToOutFile(b.plan.outDir, areaURL)
	if err := os.MkdirAll(filepath.Dir(outFile), 0o755); err != nil {
		return err
	}
	if err := writeFile(outFile, html); err != nil {
		return err
	}
	mdPath := path.Join(path.Dir(outFile), "index.md")
	if err := writeFile(mdPath, prependFrontMatter(p, []byte(s.IndexText))); err != nil {
		return err
	}
	for _, req := range s.Reqs {
		reqURL := areaURL + req.ID + "/"
		rp := page{
			URLPath:     reqURL,
			Title:       req.Title,
			Description: req.Requirement,
			Markdown:    []byte(req.Markdown),
			BodyHTML:    req.BodyHTML,
			Modified:    s.Modified,
			Type:        "requirement",
		}
		view := b.pageView(rp)
		html, err := renderLayout(view)
		if err != nil {
			return err
		}
		reqPath := urlToOutFile(b.plan.outDir, reqURL)
		if err := os.MkdirAll(filepath.Dir(reqPath), 0o755); err != nil {
			return err
		}
		if err := writeFile(reqPath, html); err != nil {
			return err
		}
		mdPath := path.Join(path.Dir(reqPath), "index.md")
		if err := writeFile(mdPath, prependFrontMatter(rp, rp.Markdown)); err != nil {
			return err
		}
	}
	return nil
}

// pageView materialises the template data for a single page.
func (b *builder) pageView(p page) pageView {
	fm, _ := parseFrontMatter(p.Markdown)
	title := p.Title
	if title == "" {
		title = fm.Title
	}
	desc := p.Description
	if desc == "" {
		desc = fm.Description
	}
	if desc == "" {
		desc = firstParagraph(p.Markdown)
	}
	canonical := trimSlash(b.plan.baseURL) + p.URLPath
	view := pageView{
		Title:       title,
		Description: desc,
		URL:         canonical,
		MarkdownURL: trimSlash(b.plan.baseURL) + strings.TrimSuffix(p.URLPath, "/") + "/index.md",
		JSONURL:     trimSlash(b.plan.baseURL) + strings.TrimSuffix(p.URLPath, "/") + "/index.json",
		Modified:    htime(p.Modified),
		Body:        template.HTML(p.BodyHTML),
		Breadcrumb:  buildBreadcrumb(b.plan.baseURL, p.URLPath),
		Site:        navContext{Name: "pagelike", URL: trimSlash(b.plan.baseURL) + "/"},
		IsIndex:     p.URLPath == "/",
	}
	view.Sections = extractSectionHeadings([]byte(view.Body))
	view.OpenGraphTags = openGraph(view)
	view.CanonicalLink = canonicalLinks(view)
	view.JSONLD = pageLD(view, p)
	return view
}

func (b *builder) writeStatic() error {
	return fs.WalkDir(staticSub(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(staticSub(), p)
		if err != nil {
			return err
		}
		out := filepath.Join(b.plan.outDir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return writeFile(out, data)
	})
}

func (b *builder) writeAgentPrimitives() error {
	if err := writeRobots(b.plan.outDir, b.plan.baseURL); err != nil {
		return err
	}
	if err := writeSitemap(b.plan.outDir, b.plan.baseURL, b.collectedPages()); err != nil {
		return err
	}
	if err := writeLlmsTxt(b.plan.outDir, b.plan.baseURL, b.collectedPages()); err != nil {
		return err
	}
	if err := writeLlmsFullTxt(b.plan.outDir, b.plan.baseURL, b.collectedPages()); err != nil {
		return err
	}
	return writeIndexJSON(b.plan.outDir, b.plan.baseURL, b.collectedPages())
}

// collectedPages returns a stable, sorted copy of every page including
// spec-area index pages and individual requirements, used by the
// sitemap/llms-sitemap emitters.
func (b *builder) collectedPages() []page {
	var all []page
	all = append(all, b.pages...)
	for _, s := range b.specs {
		all = append(all, page{
			URLPath:     "/spec/" + s.Area + "/",
			Title:       titleForArea(s.Area),
			Description: "Requirements for " + titleForArea(s.Area),
			Modified:    s.Modified,
			Type:        "spec",
		})
		for _, r := range s.Reqs {
			all = append(all, page{
				URLPath:     "/spec/" + s.Area + "/" + r.ID + "/",
				Title:       r.Title,
				Description: r.Requirement,
				Modified:    s.Modified,
				Type:        "requirement",
			})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].URLPath < all[j].URLPath })
	return all
}

// defaultInventory returns the fixed list of top-tier sources we always
// include. site/content/ is auto-walked separately.
func defaultInventory(srcRoot string) []pageSource {
	return []pageSource{
		{Path: "README.md", URLPath: "/"},
		{Path: "AGENTS.md", URLPath: "/agents/"},
		{Path: "CONTRIBUTING.md", URLPath: "/contributing/"},
		{Path: "SECURITY.md", URLPath: "/security/"},
		{Path: "LICENSE", URLPath: "/license/"},
		{Path: "docs/architecture.md", URLPath: "/architecture/"},
		{Path: "docs/design.md", URLPath: "/design/"},
		{Path: "docs/development.md", URLPath: "/development/"},
		{Path: "docs/hosting.md", URLPath: "/hosting/"},
		{Path: "docs/identity.md", URLPath: "/identity/"},
		{Path: "docs/compat/report.md", URLPath: "/compat/report/"},
		{Path: "docs/compat/matrix.md", URLPath: "/compat/matrix/"},
		{Path: "docs/compat/decisions.md", URLPath: "/compat/decisions/"},
		{Path: "docs/compat/performance.md", URLPath: "/compat/performance/"},
		{Path: "docs/compat/acceptance.md", URLPath: "/compat/acceptance/"},
		{Path: "docs/compat/app-changes.md", URLPath: "/compat/app-changes/"},
		{Path: "docs/compat/migration.md", URLPath: "/compat/migration/"},
		// live-observations.md is intentionally skipped; per AGENTS.md,
		// those observations are tightly controlled and shouldn't be
		// amplified through a public mirror.
	}
}

// pathArea peels the area slug out of docs/spec/<area>.md for the spec
// preprocessor.
func pathArea(p string) string {
	base := filepath.Base(p)
	base = strings.TrimSuffix(base, ".md")
	return base
}

// deriveTitleAndDescription picks a page title from the URL when no
// front-matter is available, then derives a description from the first
// non-empty paragraph of the body.
func deriveTitleAndDescription(urlPath, body string) (string, string) {
	var title string
	if urlPath == "/" {
		title = "pagelike"
	} else {
		title = segmentTitle(path.Base(path.Clean(urlPath)))
	}
	return title, firstParagraph([]byte(body))
}

// firstParagraph strips YAML front matter and returns the first
// non-blank line preceded by the next blank line; that's the simplest
// proxy for a page description without parsing markdown tables of
// contents out of the body.  The returned string has markdown inline
// syntax (`**…**`, `*…*`, `[text](url)`, “ ` “) collapsed so it reads
// naturally in an HTML meta tag.
func firstParagraph(src []byte) string {
	_, body := parseFrontMatter(src)
	lines := strings.Split(string(body), "\n")
	var para []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(para) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			continue
		}
		para = append(para, trimmed)
	}
	if len(para) == 0 {
		return ""
	}
	out := strings.Join(para, " ")
	out = stripMarkdownInline(out)
	if len(out) > 200 {
		out = out[:200]
		if i := strings.LastIndex(out, " "); i > 0 {
			out = out[:i]
		}
		out += "…"
	}
	return out
}

// stripMarkdownInline turns a markdown inline span into a plain string:
// drops emphasis delimiters, takes the text of a link, removes inline
// code. It only handles the simple cases the docs surface; full markdown
// is rendered by Goldmark into the body HTML.
var (
	mdBold = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdEmph = regexp.MustCompile(`\*([^*]+)\*`)
	mdCode = regexp.MustCompile("`([^`]+)`")
	mdLink = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
)

func stripMarkdownInline(s string) string {
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdBold.ReplaceAllString(s, "$1")
	s = mdEmph.ReplaceAllString(s, "$1")
	s = mdCode.ReplaceAllString(s, "$1")
	return strings.TrimSpace(s)
}

// writeFile writes data to out, creating directories as needed.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

// prependFrontMatter ensures the published .md carries title and
// description so an agent that fetches the markdown mirror gets the
// same metadata as the HTML.
func prependFrontMatter(p page, body []byte) []byte {
	var b bytes.Buffer
	hadFront := bytes.HasPrefix(body, []byte("---\n"))
	if !hadFront {
		b.WriteString("---\n")
		if p.Title != "" {
			fmt.Fprintf(&b, "title: %s\n", p.Title)
		}
		if p.Description != "" {
			fmt.Fprintf(&b, "description: %s\n", p.Description)
		}
		b.WriteString("---\n\n")
	}
	b.Write(body)
	return b.Bytes()
}

// templateHTML marks raw HTML as trusted so the layout's html/template
// pass does not escape it. Used for the .Body field.  Kept as an alias so
// the build step can read it tersely.
func templateHTML(b []byte) template.HTML { return template.HTML(b) }

// templateJS marks the JSON-LD payload as JavaScript-safe for the layout.
// The layout wraps it in <script type="application/ld+json">.
func templateJS(b []byte) template.JS { return template.JS(b) }
