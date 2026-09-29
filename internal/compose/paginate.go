package compose

import (
	"fmt"
	"html"
	"net/url"
	"sort"
	"strconv"
	"strings"

	nethtml "golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
)

// Pagination (docs/spec/composing.md §16) runs after everything else, on
// the fully composed markup: children outside the requested page are cut
// from the text, navigation links are appended to <head>, and Link headers
// are returned (R-COMP-130..134).

// pager is one p:paginate element found by the walk: off is the offset of
// its start tag in the composed text.
type pager struct {
	off    int
	length int
	id     string
}

// edit replaces text[start:end] with text (an insertion when start == end).
type edit struct {
	start, end int
	text       string
}

// finish runs the post-walk passes and assembles the result.
func (c *composer) finish(res *result) (*result, error) {
	text := c.out.String()
	anchors, trans := c.anchors, c.trans
	if len(c.pagers) > 0 {
		var edits []edit
		var links []string
		var err error
		edits, links, err = c.paginate(text)
		if err != nil {
			return nil, err
		}
		text = applyEdits(text, edits)
		anchors = shiftAnchors(anchors, edits)
		trans = shiftTrans(trans, edits)
		for _, l := range links {
			res.header.Add("Link", l)
		}
	}
	res.changed = text != res.text // res.text is the stored markup until now
	res.text = text
	res.private = c.private
	res.anchors, res.trans = anchors, trans
	return res, nil
}

// paginate computes the edits and Link header values of every paginator.
//
// Live 2026-09-29 (docs/compat/decisions-2026-09-29/composing-liquid.md):
// a paginator reads paginate:<id>:page and paginate:<id>:length when it has
// an id and the request carries them, and otherwise the unprefixed
// paginate:page and paginate:length, which every paginator shares; several
// paginators are allowed with or without ids. Each link is the request path
// with this paginator's page parameter, in the order first, last, prev,
// next. With an id, the <link> element has title="<id>" and the Link header
// value a title parameter.
//
// PageLove drops every other query parameter from the links (the search
// "q=smith", the page length), so following "next" loses the filter. That is
// kept standard (decisions-2026-09-29/serialization.md, "Integration"): the
// links keep the request's other parameters, as the docs' recipe shows, and
// the .live siblings of comp.pag.links-and-slice and
// comp.pag.preserve-other-params measure the difference.
func (c *composer) paginate(text string) ([]edit, []string, error) {
	root, spans, err := parseWithSpans(text, c.xml)
	if err != nil {
		return nil, nil, compositionError("composed document does not parse: %v", err)
	}
	byStart := map[int]*nethtml.Node{}
	var head *nethtml.Node
	for n, sp := range spans.By {
		byStart[sp.Start] = n
	}
	if !c.xml {
		head = dom.Head(root)
	}
	q := rawQueryPairs(c.req.RawQuery)
	path := (&url.URL{Path: c.req.Path}).EscapedPath()
	var edits []edit
	var linkTags strings.Builder
	var headers []string
	for _, p := range c.pagers {
		el := byStart[p.off]
		if el == nil {
			continue
		}
		var kids []*nethtml.Node
		for ch := el.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type == nethtml.ElementNode {
				kids = append(kids, ch)
			}
		}
		if len(kids) == 0 {
			continue
		}
		page, length := 1, p.length
		if v, ok := paginateParam(q, p.id, "page"); ok {
			page = positive(v, 1)
		}
		if v, ok := paginateParam(q, p.id, "length"); ok {
			length = positive(v, p.length)
		}
		pages := (len(kids) + length - 1) / length
		if page > pages {
			page = pages
		}
		first, last := (page-1)*length, page*length
		for i, k := range kids {
			if i >= first && i < last {
				continue
			}
			if sp, ok := spans.By[k]; ok {
				edits = append(edits, edit{start: sp.Start, end: sp.End})
			}
		}
		if pages <= 1 {
			continue
		}
		pageKey := "paginate:page"
		if p.id != "" {
			pageKey = "paginate:" + p.id + ":page"
		}
		rel := []struct {
			name string
			page int
			ok   bool
		}{{"first", 1, true}, {"last", pages, true}, {"prev", page - 1, page > 1}, {"next", page + 1, page < pages}}
		for _, l := range rel {
			if !l.ok {
				continue
			}
			target := path + "?" + keptQuery(q, pageKey) + pageKey + "=" + strconv.Itoa(l.page)
			value := fmt.Sprintf("<%s>; rel=%q", target, l.name)
			linkTags.WriteString(`<link rel="` + l.name + `" href="` + html.EscapeString(target) + `"`)
			if p.id != "" {
				linkTags.WriteString(` title="` + html.EscapeString(p.id) + `"`)
				value += fmt.Sprintf("; title=%q", p.id)
			}
			linkTags.WriteString(">")
			headers = append(headers, value)
		}
	}
	if linkTags.Len() > 0 && head != nil {
		// Only a <head> written in the source receives the links: live
		// PageLove adds none to a document whose <head> is implied.
		if sp, ok := spans.By[head]; ok && !sp.NoContent {
			edits = append(edits, edit{start: sp.InnerEnd, end: sp.InnerEnd, text: linkTags.String()})
		}
	}
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	return edits, headers, nil
}

// keptQuery is the request's query without the paginator's page parameter
// (the unprefixed paginate:page too, which the link's own parameter
// replaces), re-encoded, ending in "&" when not empty.
func keptQuery(q [][2]string, pageKey string) string {
	var b strings.Builder
	for _, kv := range q {
		if kv[0] == pageKey || kv[0] == "paginate:page" {
			continue
		}
		b.WriteString(queryEscape(kv[0]) + "=" + queryEscape(kv[1]) + "&")
	}
	return b.String()
}

// queryEscape escapes a query component, keeping ':' readable as PageLove's
// paginate:… parameters are written.
func queryEscape(s string) string { return strings.ReplaceAll(url.QueryEscape(s), "%3A", ":") }

// paginateParam reads a paginator's page or length parameter: the
// id-prefixed form when the paginator has an id and the request carries it,
// otherwise the unprefixed form that every paginator shares.
func paginateParam(q [][2]string, id, name string) (string, bool) {
	if id != "" {
		if v, ok := queryValue(q, "paginate:"+id+":"+name); ok {
			return v, true
		}
	}
	return queryValue(q, "paginate:"+name)
}

func positive(v string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func queryValue(q [][2]string, key string) (string, bool) {
	for _, kv := range q {
		if kv[0] == key {
			return kv[1], true
		}
	}
	return "", false
}

// applyEdits splices non-overlapping edits (sorted by start) into text.
func applyEdits(text string, edits []edit) string {
	var b strings.Builder
	pos := 0
	for _, e := range edits {
		if e.start < pos {
			continue
		}
		b.WriteString(text[pos:e.start])
		b.WriteString(e.text)
		pos = e.end
	}
	b.WriteString(text[pos:])
	return b.String()
}

// shift maps an offset of the text before edits (sorted, non-overlapping)
// to the text after them; ok is false when the offset was inside a removed
// or replaced range. An insertion at the offset lands before it.
func shift(off int, edits []edit) (int, bool) {
	delta := 0
	for _, e := range edits {
		switch {
		case e.start == e.end:
			if e.start <= off {
				delta += len(e.text)
			}
		case e.end <= off:
			delta += len(e.text) - (e.end - e.start)
		case e.start <= off:
			return 0, false
		}
	}
	return off + delta, true
}

func shiftAnchors(as []anchor, edits []edit) []anchor {
	if len(edits) == 0 {
		return as
	}
	out := as[:0:0]
	for _, a := range as {
		if off, ok := shift(a.off, edits); ok {
			a.off = off
			out = append(out, a)
		}
	}
	return out
}

func shiftTrans(ts []transRec, edits []edit) []transRec {
	if len(edits) == 0 {
		return ts
	}
	out := ts[:0:0]
	for _, t := range ts {
		s, ok1 := shift(t.start, edits)
		e, ok2 := shift(t.end, edits)
		if ok1 && ok2 {
			t.start, t.end = s, e
			out = append(out, t)
		}
	}
	return out
}

// parseWithSpans parses a composed document and records element spans.
func parseWithSpans(text string, xml bool) (*nethtml.Node, dom.Spans, error) {
	if xml {
		return dom.ParseXMLWithSpans([]byte(text))
	}
	root, err := dom.Parse([]byte(text))
	if err != nil {
		return nil, dom.Spans{}, err
	}
	return root, dom.ComputeSpans(root, text), nil
}
