package main

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// specSource is the sliceable image of one docs/spec/<area>.md file.
// The Markdown body is rendered and indexed as a per-area page; one
// requirement page is emitted per R-XXX-N heading.
type specSource struct {
	Area      string
	Path      string
	Modified  time.Time
	IndexText string
	IndexHTML []byte
	Reqs      []reqSource
}

// reqSource is one requirement, lifted out of a spec source by the
// bold-prefixed `**R-… Title.**` convention.
type reqSource struct {
	ID          string
	Title       string
	Requirement string
	Body        []byte
	Markdown    []byte
	BodyHTML    []byte
}

// reqHeader matches requirement declarations in both shapes the
// upstream specs use: a bold-prefixed inline line
// `**R-LIQ-1 Title.**`, and a heading-prefixed line at any depth up to
// H5 `### R-MOD-1 — Title` (or ## / #### / #####), depending on the
// area. IDs are captured with optional suffixes (`R-MOD-9a`,
// `R-APPS-19.5`). The separator between ID and title accepts an em dash,
// hyphen, colon, or a plain space — javascript.md uses `R-JS-1:`,
// reacting.md uses `R-REACT-4a —`, others use `R-MOD-1 —`.
var reqBold = regexp.MustCompile(`(?m)^\*\*\s*R-([A-Z]+-[0-9]+[a-zA-Z0-9.]*)\s+(.+?)\.\*\*`)
var reqHeading = regexp.MustCompile(`(?m)^#{2,5}\s+R-([A-Z]+-[0-9]+[a-zA-Z0-9.]*)\s*[:\-—]?\s+(.+?)\s*$`)

// specMatch is a unified pre-parse shape for a requirement header found
// in either bold or heading form. It records the byte offsets into the
// source so parseSpec can slice cleanly irrespective of header style.
type specMatch struct {
	idStart, idEnd int
	titleStart     int
	titleEnd       int
	bodyStart      int // first byte after the matched header line
}

// findSpecMatches merges the two header regexes by source position and
// returns them in source order. End-of-line stripping happens here so
// the body slicing is unambiguous.
func findSpecMatches(data []byte) []specMatch {
	collect := func(re *regexp.Regexp) []specMatch {
		var ms []specMatch
		for _, loc := range re.FindAllSubmatchIndex(data, -1) {
			// loc[2:3] is the id slug; [4:5] is the title span.
			// The body starts at the newline that closes this header line.
			bodyStart := loc[1]
			for bodyStart < len(data) && data[bodyStart] != '\n' {
				bodyStart++
			}
			if bodyStart < len(data) {
				bodyStart++ // skip the newline
			}
			ms = append(ms, specMatch{
				idStart:    loc[2],
				idEnd:      loc[3],
				titleStart: loc[4],
				titleEnd:   loc[5],
				bodyStart:  bodyStart,
			})
		}
		return ms
	}
	all := append([]specMatch{}, collect(reqBold)...)
	all = append(all, collect(reqHeading)...)
	sort.Slice(all, func(i, j int) bool { return all[i].bodyStart < all[j].bodyStart })
	return all
}

// parseSpec splits a single docs/spec/<area>.md file into per-requirement
// bodies. The behaviour at the edges is deliberate: a spec with no R-
// headers falls back to a single area page holding the whole body, so we
// don't accidentally drop content.
func parseSpec(p string, data []byte) ([]reqSource, error) {
	matches := findSpecMatches(data)
	if len(matches) == 0 {
		area := strings.TrimSuffix(path.Base(p), ".md")
		return []reqSource{{
			ID:       "spec",
			Title:    titleForArea(area),
			Body:     data,
			Markdown: data,
		}}, nil
	}
	out := make([]reqSource, 0, len(matches))
	for i, m := range matches {
		id := "R-" + string(data[m.idStart:m.idEnd])
		title := strings.TrimSpace(string(data[m.titleStart:m.titleEnd]))
		bodyEnd := len(data)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1].bodyStart
			if bodyEnd > 0 && data[bodyEnd-1] == '\n' {
				bodyEnd-- // trim the structural newline
			}
		}
		body := append([]byte(nil), data[m.bodyStart:bodyEnd]...)
		out = append(out, reqSource{
			ID:          id,
			Title:       title,
			Requirement: title,
			Body:        body,
			Markdown:    body,
		})
	}
	return out, nil
}

// buildSpecIndex produces the markdown for a spec-area index page. It is
// a small table with one row per requirement; the URL of each row is
// implicit in the layout's link-rel-alternate pipeline.
func buildSpecIndex(area string, reqs []reqSource) string {
	var b strings.Builder
	title := titleForArea(area)
	if title == "" {
		title = area
	}
	fmt.Fprintf(&b, "# %s\n", title)
	fmt.Fprintf(&b, "\n%s specification area. Each requirement has its own page at `/spec/%s/<R-ID>/`.\n\n", title, area)
	fmt.Fprintf(&b, "There are %d requirements in this area; the harness is under `harness/cases/%s/*.yaml`.\n\n", len(reqs), area)
	b.WriteString("| ID | Title |\n|---|---|\n")
	for _, r := range reqs {
		fmt.Fprintf(&b, "| [%s](%s/) | %s |\n", r.ID, r.ID, r.Title)
	}
	b.WriteString("\nSee `docs/spec/README.md` or `/spec/` for a cross-area index.\n")
	return b.String()
}

// specJSONLD builds the per-requirement JSON-LD payload. Most of the
// page chrome is shared via pageLD; this only handles requirement
// hierarchy hints so an agent can resolve "what spec owns R-LIQ-90?".
func specJSONLD(area, reqID, title string) []byte {
	out := map[string]any{
		"@context":       "https://schema.org",
		"@type":          "TechArticle",
		"name":           fmt.Sprintf("%s — %s", reqID, title),
		"url":            fmt.Sprintf("/spec/%s/%s/", area, reqID),
		"isPartOf":       map[string]any{"@type": "TechArticle", "name": titleForArea(area), "url": fmt.Sprintf("/spec/%s/", area)},
		"articleSection": "Specification",
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return b
}
