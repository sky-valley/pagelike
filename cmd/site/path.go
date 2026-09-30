package main

import (
	"path"
	"strings"
)

// repoPathToURL maps a repo-relative markdown path to a URL path under the
// site's base URL. README, AGENTS.md and a handful of top-level files get
// friendly URLs; everything else mirrors the directory shape.
func repoPathToURL(repoPath string) (url string, title string) {
	repoPath = strings.TrimPrefix(repoPath, "./")
	switch repoPath {
	case "README.md":
		return "/", "pagelike"
	case "AGENTS.md":
		return "/agents/", "pagelike — for coding agents"
	case "CONTRIBUTING.md":
		return "/contributing/", "Contributing"
	case "SECURITY.md":
		return "/security/", "Security policy"
	case "LICENSE":
		return "/license/", "Apache License 2.0"
	}
	if strings.HasPrefix(repoPath, "docs/spec/") {
		// specSourceTasks handles the slicing; the parent area index lives
		// at /spec/<area>/ for the spec preprocessor.
		rest := strings.TrimPrefix(repoPath, "docs/spec/")
		area := strings.TrimSuffix(rest, ".md")
		return "/spec/" + area + "/", titleForArea(area)
	}
	if strings.HasPrefix(repoPath, "docs/decisions/") {
		rest := strings.TrimPrefix(repoPath, "docs/decisions/")
		rest = strings.TrimSuffix(rest, ".md")
		return "/decisions/" + rest + "/", ""
	}
	if strings.HasPrefix(repoPath, "docs/compat/") {
		rest := strings.TrimPrefix(repoPath, "docs/compat/")
		rest = strings.TrimSuffix(rest, ".md")
		return "/compat/" + rest + "/", ""
	}
	if strings.HasPrefix(repoPath, "docs/") {
		rest := strings.TrimPrefix(repoPath, "docs/")
		rest = strings.TrimSuffix(rest, ".md")
		return "/" + rest + "/", ""
	}
	if strings.HasPrefix(repoPath, "site/content/") {
		rest := strings.TrimPrefix(repoPath, "site/content/")
		rest = strings.TrimSuffix(rest, ".md")
		switch rest {
		case "index", "_index":
			return "/", ""
		}
		// `<dir>/_index.md` becomes `<dir>/` so each section has a
		// conventional landing page.
		if strings.HasSuffix(rest, "/_index") {
			return "/" + strings.TrimSuffix(rest, "/_index") + "/", ""
		}
		return "/" + rest + "/", ""
	}
	return "/" + strings.TrimSuffix(repoPath, ".md") + "/", ""
}

// titleForArea returns a human title for a spec area slug.
func titleForArea(area string) string {
	switch area {
	case "apps":
		return "Application compatibility"
	case "composing":
		return "Composition"
	case "javascript":
		return "Server JavaScript"
	case "liquid":
		return "Liquid templates"
	case "modeling":
		return "Modelling (schemas)"
	case "permissions-identity":
		return "Permissions and identity"
	case "protocol":
		return "Wire protocol"
	case "reacting":
		return "Reactions"
	case "reading-writing":
		return "Reading and writing"
	case "sessel":
		return "Sessel query language"
	case "sse":
		return "Server-sent events"
	}
	return ""
}

// urlToOutFile maps a URL path to the output file path inside outDir.
// `/` becomes outDir/index.html so the bare host lands on the right page.
func urlToOutFile(outDir, urlPath string) string {
	clean := path.Clean("/" + urlPath)
	if clean == "/" {
		return path.Join(outDir, "index.html")
	}
	return path.Join(outDir, clean, "index.html")
}

// joinURL joins a base URL prefix with a sub-path, handling the trailing
// slash on the prefix and producing an absolute URL.
func joinURL(base, sub string) string {
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	clean := strings.TrimPrefix(sub, "/")
	return base + clean
}
