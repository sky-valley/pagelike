package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// writeRobots emits /robots.txt allowing crawl, with explicit rules for
// the named AI crawlers the agent-readiness scanners expect. Comments at
// the top reference the agent-aware entry points so a human reader sees
// them too; see https://isitagentready.com/ for the current category set
// the scanners check.
func writeRobots(outDir, baseURL string) error {
	trim := strings.TrimRight(baseURL, "/")
	// Per the emerging agent-readiness convention, give the named AI
	// bots explicit Allow groups. Sites that don't list these, and that
	// use the global "User-agent: * Disallow", get downgraded by the
	// scanners even if the global group would permit them.
	body := fmt.Sprintf(`# pagelike docs — agent-aware static site
# llms.txt:      %[1]s/llms.txt
# llms-full:     %[1]s/llms-full.txt
# machine index: %[1]s/index.json
# canonical page: %[1]s/for-agents/

User-agent: *
Allow: /
Sitemap: %[1]s/sitemap.xml

User-agent: GPTBot
Allow: /

User-agent: ChatGPT-User
Allow: /

User-agent: ClaudeBot
Allow: /

User-agent: Claude-Web
Allow: /

User-agent: anthropic-ai
Allow: /

User-agent: PerplexityBot
Allow: /

User-agent: Google-Extended
Allow: /

User-agent: Applebot-Extended
Allow: /

User-agent: CCBot
Allow: /
`, trim)
	return writeFile(path.Join(outDir, "robots.txt"), []byte(body))
}

// writeSitemap emits /sitemap.xml from the collected page list. lastmod
// is rounded to a day so a build run that runs twice in 24h stays cached.
func writeSitemap(outDir, baseURL string, pages []page) error {
	type smURL struct {
		XMLName xml.Name `xml:"url"`
		Loc     string   `xml:"loc"`
		LastMod string   `xml:"lastmod,omitempty"`
	}
	type smURLSet struct {
		XMLName xml.Name `xml:"urlset"`
		XMLNS   string   `xml:"xmlns,attr"`
		URLs    []smURL  `xml:"url"`
	}
	trim := strings.TrimRight(baseURL, "/")
	s := smURLSet{XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, p := range pages {
		lastmod := ""
		if !p.Modified.IsZero() {
			lastmod = p.Modified.UTC().Format("2006-01-02")
		}
		s.URLs = append(s.URLs, smURL{Loc: trim + p.URLPath, LastMod: lastmod})
	}
	body, err := xml.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path.Join(outDir, "sitemap.xml"), append([]byte(xml.Header), body...))
}

// writeLlmsTxt writes /llms.txt using a short, stable shape that
// matches the convention the upstream repo already publishes at root.
// The point of the doc is to be a one-page manifest for an agent that
// has never heard of pagelike.
func writeLlmsTxt(outDir, baseURL string, pages []page) error {
	trim := strings.TrimRight(baseURL, "/")
	var b strings.Builder
	b.WriteString("# pagelike\n\n")
	b.WriteString("> pagelike is an open-source (Apache-2.0), self-hostable Go server compatible with PageLove (https://pagelove.com). HTML documents are both the application and its database. CSS selectors address elements; HTTP reads and writes them. Server-sent events stream every change. Permissions, schemas, templates and reactions are declared as microdata inside the documents.\n\n")
	b.WriteString("Key facts:\n")
	b.WriteString("- Single static binary (`pagelike serve`); each site is a SQLite database plus blobs; sites are routed by host name (`<site>.<domain>`, authoring at `dav-<site>.<domain>`).\n")
	b.WriteString("- Compatibility is measured with a differential harness: 1,426 cases, all local cases pass; 603 were run against live PageLove, 578 match and 25 differ on purpose. See docs/compat/report.md.\n")
	b.WriteString("- Supports Liquid, Sessel and sandboxed server JavaScript (QuickJS), AuthorizationRule, schemas, transitions, triggers, WebDAV authoring, OpenID Connect and local accounts.\n\n")

	group := groupPages(pages)
	for name, list := range group {
		if len(list) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s\n", name)
		for _, p := range list {
			if p.Description == "" {
				fmt.Fprintf(&b, "- [%s](%s%s)\n", p.Title, trim, p.URLPath)
			} else {
				fmt.Fprintf(&b, "- [%s](%s%s): %s\n", p.Title, trim, p.URLPath, p.Description)
			}
		}
		b.WriteString("\n")
	}
	return writeFile(path.Join(outDir, "llms.txt"), []byte(b.String()))
}

// writeLlmsFullTxt writes /llms-full.txt with a TOC and then a section
// per page that contains the markdown source plus its metadata. The
// format is intentionally boring: a heading per page, a metadata block
// (URL, modified:), and the markdown body verbatim. An agent can chunk
// the file on '^## ' boundaries if it needs to.
func writeLlmsFullTxt(outDir, baseURL string, pages []page) error {
	trim := strings.TrimRight(baseURL, "/")
	var b strings.Builder
	b.WriteString("# pagelike — full docs dump\n\n")
	b.WriteString("> One-file dump of every published page. Use /llms.txt for the short version, /index.json for a structured catalog.\n\n")
	b.WriteString("## Table of contents\n\n")
	for _, p := range pages {
		fmt.Fprintf(&b, "- [%s](%s%s)\n", p.Title, trim, p.URLPath)
		if p.Description != "" {
			fmt.Fprintf(&b, "  — %s\n", p.Description)
		}
	}
	b.WriteString("\n---\n\n")
	for _, p := range pages {
		fmt.Fprintf(&b, "## %s\n\n", p.Title)
		fmt.Fprintf(&b, "URL: %s%s\n", trim, p.URLPath)
		if p.Description != "" {
			fmt.Fprintf(&b, "Description: %s\n", p.Description)
		}
		if !p.Modified.IsZero() {
			fmt.Fprintf(&b, "Modified: %s\n", p.Modified.UTC().Format(time.RFC3339))
		}
		b.WriteString("\n")
		_, body := parseFrontMatter(p.Markdown)
		b.Write(body)
		if !bytes_hasSuffix(body, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n---\n\n")
	}
	return writeFile(path.Join(outDir, "llms-full.txt"), []byte(b.String()))
}

func bytes_hasSuffix(b []byte, s string) bool {
	if len(b) < len(s) {
		return false
	}
	return string(b[len(b)-len(s):]) == s
}

// indexPage is the per-page shape of /index.json. The set is small and
// stable; agents consume it as a single GET, no parsing required beyond
// the standard library.
type indexPage struct {
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Type        string   `json:"type,omitempty"`
	Modified    string   `json:"modified,omitempty"`
	Markdown    string   `json:"markdown,omitempty"`
	Sections    []string `json:"sections,omitempty"`
}

type indexShape struct {
	Site        string      `json:"site"`
	LLMsTxt     string      `json:"llms_txt"`
	LLMsFullTxt string      `json:"llms_full_txt"`
	Sitemap     string      `json:"sitemap"`
	License     string      `json:"license"`
	Generated   string      `json:"generated"`
	PageCount   int         `json:"page_count"`
	Pages       []indexPage `json:"pages"`
}

// writeIndexJSON emits /index.json for the whole site and a per-page
// /index.json next to each HTML file. The latter is small enough to be
// cheap even for hundreds of pages.
func writeIndexJSON(outDir, baseURL string, pages []page) error {
	trim := strings.TrimRight(baseURL, "/")
	root := indexShape{
		Site:        trim,
		LLMsTxt:     trim + "/llms.txt",
		LLMsFullTxt: trim + "/llms-full.txt",
		Sitemap:     trim + "/sitemap.xml",
		License:     "Apache-2.0",
		Generated:   time.Now().UTC().Format(time.RFC3339),
		PageCount:   len(pages),
		Pages:       make([]indexPage, 0, len(pages)),
	}
	for _, p := range pages {
		root.Pages = append(root.Pages, indexPage{
			URL:         trim + p.URLPath,
			Title:       p.Title,
			Description: p.Description,
			Type:        p.Type,
			Modified:    htime(p.Modified),
		})
	}
	rootBody, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFile(path.Join(outDir, "index.json"), rootBody); err != nil {
		return err
	}
	// Per-page JSON mirrors under each section, produced only for pages
	// that have their own directory (i.e. not the bare root).
	for _, p := range pages {
		if p.URLPath == "/" {
			continue
		}
		single := indexShape{
			Site:        trim,
			LLMsTxt:     trim + "/llms.txt",
			LLMsFullTxt: trim + "/llms-full.txt",
			Sitemap:     trim + "/sitemap.xml",
			License:     "Apache-2.0",
			Generated:   time.Now().UTC().Format(time.RFC3339),
			PageCount:   1,
			Pages: []indexPage{{
				URL:         trim + p.URLPath,
				Title:       p.Title,
				Description: p.Description,
				Type:        p.Type,
				Modified:    htime(p.Modified),
			}},
		}
		// Construct the path directly; urlToOutFile always appends
		// index.html which would turn this into index.json/index.html.
		outFile := path.Join(outDir, strings.Trim(p.URLPath, "/"), "index.json")
		if err := os.MkdirAll(path.Dir(outFile), 0o755); err != nil {
			return err
		}
		body, err := json.MarshalIndent(single, "", "  ")
		if err != nil {
			return err
		}
		if err := writeFile(outFile, body); err != nil {
			return err
		}
	}
	return nil
}

// pageLD is the JSON-LD payload rendered into every page's <head>.
// Top-level pages (the home and the big section pages) emit
// SoftwareSourceCode; individual prose pages emit TechArticle.
func pageLD(v pageView, p page) template.JS {
	m := siteMeta{
		Context:         "https://schema.org",
		InLanguage:      "en",
		ProgrammingLang: "Go",
		RuntimePlatform: "Linux, macOS, Windows",
		License:         "https://www.apache.org/licenses/LICENSE-2.0",
		CodeRepository:  "https://github.com/sky-valley/pagelike",
		Author:          &personOrOrg{Type: "Organization", Name: "sky-valley", URL: "https://github.com/sky-valley"},
	}
	switch {
	case v.URL == strings.TrimRight(v.Site.URL, "/")+"/" || v.IsIndex:
		m.Type = "SoftwareSourceCode"
		m.Name = "pagelike"
		m.Description = "Pagelike is an open-source, self-hostable Go server compatible with PageLove. HTML documents are both the application and its database; CSS selectors address the data; HTTP and server-sent events read, write and stream it."
		m.URL = v.URL
	case p.Type == "requirement":
		m.Type = "TechArticle"
		m.Name = v.Title
		m.Description = v.Description
		m.URL = v.URL
		m.DateModified = v.Modified
		m.ArticleSection = "Specification"
	case p.Type == "spec":
		m.Type = "TechArticle"
		m.Name = v.Title
		m.Description = v.Description
		m.URL = v.URL
		m.DateModified = v.Modified
		m.ArticleSection = "Specification"
	default:
		m.Type = "TechArticle"
		m.Name = v.Title
		m.Description = v.Description
		m.URL = v.URL
		m.DateModified = v.Modified
	}
	body, _ := json.MarshalIndent(m, "", "  ")
	return template.JS(body)
}

// groupPages re-orders the page list by section for both /llms.txt and
// the per-area index. Top-level pages come first, then /spec/, then
// /compat/, then /docs/. The order inside each group is alphabetical.
func groupPages(pages []page) map[string][]page {
	groups := map[string][]page{
		"Docs":          nil,
		"Specification": nil,
		"Compatibility": nil,
		"Operations":    nil,
		"Project":       nil,
	}
	for _, p := range pages {
		switch {
		case p.URLPath == "/" || p.URLPath == "/agents/" ||
			p.URLPath == "/for-agents/" || p.URLPath == "/commands/":
			groups["Docs"] = append(groups["Docs"], p)
		case strings.HasPrefix(p.URLPath, "/spec/"):
			groups["Specification"] = append(groups["Specification"], p)
		case strings.HasPrefix(p.URLPath, "/compat/"):
			groups["Compatibility"] = append(groups["Compatibility"], p)
		case strings.HasPrefix(p.URLPath, "/hosting/") ||
			strings.HasPrefix(p.URLPath, "/identity/") ||
			strings.HasPrefix(p.URLPath, "/architecture/") ||
			strings.HasPrefix(p.URLPath, "/development/") ||
			strings.HasPrefix(p.URLPath, "/security/"):
			groups["Operations"] = append(groups["Operations"], p)
		default:
			groups["Project"] = append(groups["Project"], p)
		}
	}
	for k := range groups {
		sort.Slice(groups[k], func(i, j int) bool { return groups[k][i].URLPath < groups[k][j].URLPath })
	}
	return groups
}
