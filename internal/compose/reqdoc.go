package compose

import (
	"html"
	"net/url"
	"sort"
	"strings"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/site"
)

// URLRequest is the root item type of the Request Document.
const URLRequest = "https://pagelove.org/Request"

// requestDocument renders the transient Request Document of a request
// (docs/spec/reading-writing.md R-RW-136, composing R-COMP-120): it is part
// of the site graph for r: bindings and global includes, has no HTTP
// address and is never stored. Anything that reads it makes the response
// private.
func requestDocument(req *Request, snap *site.Snapshot) string {
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\"><head></head>\n")
	b.WriteString(`<body itemscope itemtype="` + URLRequest + `">` + "\n")
	b.WriteString(`<meta itemprop="path" content="` + esc(req.Path) + "\">\n")
	b.WriteString(`<meta itemprop="method" content="` + esc(strings.ToUpper(req.Method)) + "\">\n")
	b.WriteString(`<meta itemprop="query" content="` + esc(req.RawQuery) + "\">\n")
	b.WriteString(`<meta itemprop="body" content="` + esc(string(req.Body)) + "\">\n")
	b.WriteString(`<section itemprop="query" itemscope itemtype="https://pagelove.org/Request/HTTP/Query">` + "\n")
	for _, kv := range rawQueryPairs(req.RawQuery) {
		if validPropName(kv[0]) {
			b.WriteString(`<meta itemprop="` + esc(kv[0]) + `" content="` + esc(kv[1]) + "\">\n")
		}
	}
	b.WriteString("</section>\n")
	b.WriteString(`<section itemprop="headers" itemscope itemtype="https://pagelove.org/Request/HTTP/Headers">` + "\n")
	names := make([]string, 0, len(req.Header))
	for k := range req.Header {
		names = append(names, k)
	}
	if req.Host != "" && req.Header.Get("Host") == "" {
		names = append(names, "Host")
	}
	sort.Strings(names)
	for _, k := range names {
		v := strings.Join(req.Header.Values(k), ", ")
		if strings.EqualFold(k, "Host") && v == "" {
			v = req.Host
		}
		b.WriteString(`<meta itemprop="` + esc(strings.ToLower(k)) + `" content="` + esc(v) + "\">\n")
	}
	b.WriteString("</section>\n")
	b.WriteString(identity.AuthSection(req.Principal, req.roles(snap)))
	b.WriteString("\n</body></html>\n")
	return b.String()
}

// rawQueryPairs decodes a query string preserving order and repeats.
func rawQueryPairs(raw string) [][2]string {
	var out [][2]string
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		k, v, _ := strings.Cut(part, "=")
		if kk, err := url.QueryUnescape(k); err == nil {
			k = kk
		}
		if vv, err := url.QueryUnescape(v); err == nil {
			v = vv
		}
		out = append(out, [2]string{k, v})
	}
	return out
}

func validPropName(s string) bool { return s != "" && !strings.ContainsAny(s, " \t\n\r\f") }

// requestRegion parses the Request Document into a (generated) region.
func requestRegion(req *Request, snap *site.Snapshot) *region {
	text := requestDocument(req, snap)
	root, err := dom.Parse([]byte(text))
	if err != nil {
		return nil
	}
	return &region{text: text, root: root, spans: dom.ComputeSpans(root, text), kind: provGenerated, reqDoc: true}
}
