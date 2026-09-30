package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// frontMatter is the minimal YAML preamble parsed at the top of every
// markdown source. We keep the schema flat on purpose: title and a short
// description suffice for a docs site, and the rest comes from the page
// itself.
type frontMatter struct {
	Title       string
	Description string
}

// parseFrontMatter reads up to "---\n...\n---\n" or returns zero. The body
// after the fence is returned alongside. This is a hand-rolled subset
// because the only keys we accept are Title and Description, both scalars.
func parseFrontMatter(src []byte) (frontMatter, []byte) {
	var fm frontMatter
	if !bytes.HasPrefix(src, []byte("---\n")) {
		return fm, src
	}
	bodyStart := 4
	sc := bufio.NewScanner(bytes.NewReader(src[bodyStart:]))
	for sc.Scan() {
		line := sc.Text()
		advance := len(line) + 1
		if line == "---" {
			rest := append([]byte(nil), src[bodyStart+advance:]...)
			return fm, rest
		}
		bodyStart += advance
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "title":
			fm.Title = strings.TrimSpace(v)
		case "description":
			fm.Description = strings.TrimSpace(v)
		}
	}
	// No closing fence; treat the whole input as body for safety.
	return frontMatter{}, src
}

// siteMeta is the JSON-LD payload rendered into every page's <head>.
// SoftwareSourceCode is the most-specific Schema.org type that fits a
// server runtime; individual pages use TechArticle or Article for prose.
type siteMeta struct {
	Context              string       `json:"@context"`
	Type                 string       `json:"@type"`
	Name                 string       `json:"name"`
	Description          string       `json:"description,omitempty"`
	URL                  string       `json:"url,omitempty"`
	License              string       `json:"license,omitempty"`
	ProgrammingLang      string       `json:"programmingLanguage,omitempty"`
	RuntimePlatform      string       `json:"runtimePlatform,omitempty"`
	Author               *personOrOrg `json:"author,omitempty"`
	CodeRepository       string       `json:"codeRepository,omitempty"`
	DateModified         string       `json:"dateModified,omitempty"`
	ArticleSection       string       `json:"articleSection,omitempty"`
	Keywords             []string     `json:"keywords,omitempty"`
	InLanguage           string       `json:"inLanguage,omitempty"`
	ArticleBody          string       `json:"articleBody,omitempty"`
	Dependencies         []string     `json:"dependencies,omitempty"`
	OperatingSystem      []string     `json:"operatingSystem,omitempty"`
	SoftwareRequirements string       `json:"softwareRequirements,omitempty"`
	ThumbnailUrl         string       `json:"thumbnailUrl,omitempty"`
}

type personOrOrg struct {
	Type string `json:"@type"`
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

func (m siteMeta) JSON() []byte {
	b, _ := json.MarshalIndent(m, "", "  ")
	return b
}

// htime renders t in RFC 3339, the form isitagentready-style checkers tend
// to recognise (sitemap lastmod, dateModified, etc.).
func htime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
