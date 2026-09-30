package main

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path"
	"strings"
	"time"
)

// pageView is the struct the layout template receives for every page. The
// Body is already-rendered HTML from renderMarkdown and is marked safe so
// the html/template engine does not escape it.
type pageView struct {
	Title         string
	Description   string
	URL           string
	MarkdownURL   string
	JSONURL       string
	Modified      string
	Body          template.HTML
	JSONLD        template.JS
	OpenGraphTags template.HTML
	CanonicalLink template.HTML
	Sections      []sectionView
	Breadcrumb    []navItem
	Site          navContext
	IsIndex       bool
}

type sectionView struct {
	ID    string
	Title string
	Level int
}

type navItem struct {
	Title string
	URL   string
}

type navContext struct {
	Name string
	URL  string
}

// openGraph builds the og:* and twitter:* meta tags from the page view and
// base URL. Returned as raw HTML so the layout template can drop it in
// directly.
func openGraph(v pageView) template.HTML {
	parts := []string{
		fmt.Sprintf(`<meta property="og:title" content=%q>`, v.Title),
		fmt.Sprintf(`<meta property="og:description" content=%q>`, v.Description),
		fmt.Sprintf(`<meta property="og:url" content=%q>`, v.URL),
		`<meta property="og:type" content="article">`,
		`<meta property="og:site_name" content="pagelike">`,
		fmt.Sprintf(`<meta name="twitter:card" content="summary">`),
		fmt.Sprintf(`<meta name="twitter:title" content=%q>`, v.Title),
		fmt.Sprintf(`<meta name="twitter:description" content=%q>`, v.Description),
	}
	return template.HTML(strings.Join(parts, "\n  "))
}

// canonicalLinks emits <link rel="canonical">, plus <link rel="alternate">
// for the markdown mirror and the per-page JSON shape. These three hints
// are what an agent-aware checkist looks for first; see also the
// isitagentready-style audit checklist in cmd/site/agent.go.
func canonicalLinks(v pageView) template.HTML {
	parts := []string{
		fmt.Sprintf(`<link rel="canonical" href=%q>`, v.URL),
		fmt.Sprintf(`<link rel="alternate" type="text/markdown" href=%q>`, v.MarkdownURL),
		fmt.Sprintf(`<link rel="alternate" type="application/json" href=%q>`, v.JSONURL),
	}
	return template.HTML(strings.Join(parts, "\n  "))
}

// toURL joins the base URL with the canonical URL path. The base URL has
// no trailing slash and the URL path has a leading slash, so a plain
// concatenation is sufficient.
func trimSlash(s string) string { return strings.TrimRight(s, "/") }

// buildBreadcrumb produces the parent/child chain from the URL path.
func buildBreadcrumb(baseURL, urlPath string) []navItem {
	clean := path.Clean("/" + urlPath)
	if clean == "/" {
		return nil
	}
	segments := strings.Split(strings.Trim(clean, "/"), "/")
	var out []navItem
	for i, seg := range segments {
		title := segmentTitle(seg)
		if title == "" {
			continue
		}
		parent := strings.Join(segments[:i+1], "/")
		out = append(out, navItem{Title: title, URL: trimSlash(baseURL) + "/" + parent})
	}
	return out
}

func segmentTitle(seg string) string {
	seg = strings.TrimSuffix(seg, ".md")
	switch seg {
	case "":
		return ""
	case "index":
		return "Home"
	}
	titled := strings.ReplaceAll(seg, "-", " ")
	titled = strings.ReplaceAll(titled, "_", " ")
	return titleCase(titled)
}

func titleCase(s string) string {
	parts := strings.Fields(s)
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// extractSectionHeadings is a post-render pass that finds <h2 id="...">s
// and surfaces them for the in-page TOC. Goldmark's WithAutoHeadingID
// already produced the IDs we use, so this just walks them out.
func extractSectionHeadings(html []byte) []sectionView {
	var out []sectionView
	cur := 0
	for {
		idx := bytes.Index(html[cur:], []byte("<h"))
		if idx < 0 {
			break
		}
		cur += idx
		tagEnd := bytes.IndexByte(html[cur:], '>')
		if tagEnd < 0 {
			break
		}
		openTag := html[cur : cur+tagEnd+1]
		if !bytes.HasPrefix(openTag, []byte("<h2 ")) {
			cur += tagEnd + 1
			continue
		}
		// capture id="..."
		openQuoteIdx := bytes.Index(openTag, []byte("id=\""))
		if openQuoteIdx < 0 {
			cur += tagEnd + 1
			continue
		}
		from := openQuoteIdx + 4
		closeQuoteIdx := bytes.IndexByte(openTag[from:], '"')
		if closeQuoteIdx < 0 {
			cur += tagEnd + 1
			continue
		}
		id := string(openTag[from : from+closeQuoteIdx])
		// walk past the open tag, capture text up to </h2>
		afterOpen := cur + tagEnd + 1
		closeIdx := bytes.Index(html[afterOpen:], []byte("</h2>"))
		if closeIdx < 0 {
			cur += tagEnd + 1
			continue
		}
		inner := html[afterOpen : afterOpen+closeIdx]
		title := stripTags(inner)
		out = append(out, sectionView{ID: id, Title: title, Level: 2})
		cur = afterOpen + closeIdx + len("</h2>")
	}
	return out
}

// stripTags removes tags from a byte slice using a tiny state machine.
// It is intentionally not safe for arbitrary input; it exists solely to
// turn the inner HTML of an <h2> into a heading title for the TOC.
func stripTags(s []byte) string {
	var b bytes.Buffer
	depth := 0
	for _, c := range s {
		switch c {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteByte(c)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// renderLayout writes the full HTML for a single page using the embedded
// layout template in cmd/site/templates/page.html. The data argument is
// the per-page view and the base URL/globals are filled in by the caller.
func renderLayout(view pageView) ([]byte, error) {
	t, err := template.New("layout").Funcs(template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
	}).ParseFS(templateFS(), "*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "page.html", view); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// lastmodTime peels the most recent mtime out of the inputs that produced
// the page so that sitemaps and JSON-LD can report an honest dateModified.
func lastmodTime(paths ...string) time.Time {
	var latest time.Time
	for _, p := range paths {
		if p == "" {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if latest.Before(fi.ModTime()) {
			latest = fi.ModTime()
		}
	}
	return latest
}
