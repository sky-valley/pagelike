package compose

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
)

// templateCreate implements templated resource creation (docs/spec/
// composing.md §17, R-COMP-140..144): a POST without Range to a stored
// document composes it with request.method = POST and request.body, finds
// the first <base href> of the output, stores the output — directive
// attributes consumed, xmlns declarations kept, <base> removed — at the
// resolved path through the ordinary whole-document PUT pipeline, and
// answers 301 with Location (also observed live, LO-3).
func (e *Engine) templateCreate(ctx context.Context, eng *engine.Engine, w *engine.WriteCtx) (*engine.Result, error) {
	op := w.Op
	if op.Plane == engine.Public {
		// Authorize POST on the template before any templating (step 1).
		areq := authz.Request{Principal: op.Principal, Method: http.MethodPost, HTTPMethod: http.MethodPost, Path: op.Path, Header: op.Header, Query: op.Query}
		if !w.Snap.Policy.Decide(areq, nil).Allowed {
			return nil, w.Refuse()
		}
	}
	req := &Request{Method: http.MethodPost, Path: op.Path, Query: op.Query, RawQuery: op.Query.Encode(), Header: op.Header,
		Host: op.Host, Principal: op.Principal, ContentType: op.ContentType, Body: op.Body}
	res, err := e.compose(ctx, w.Site, w.Snap, w.Doc, nil, req, options{create: true})
	if err != nil {
		return nil, err
	}
	root, spans, err := parseWithSpans(res.text, res.xml)
	if err != nil {
		return nil, compositionError("the rendered template does not parse: %v", err)
	}
	base, href := firstBase(root)
	if base == nil {
		return nil, errdoc.New(http.StatusUnprocessableEntity, KindNoBase,
			"Template must include a <base href> element specifying the target resource path")
	}
	target, err := targetPath(op.Path, href, op.Host)
	if err != nil {
		return nil, err
	}
	stored := res.text
	if sp, ok := spans.By[base]; ok {
		stored = res.text[:sp.Start] + res.text[sp.End:] // surrounding whitespace stays (R-COMP-143)
	}
	put := &engine.Op{Plane: op.Plane, Method: http.MethodPut, Path: target, Body: []byte(stored),
		ContentType: w.Doc.ContentType, Principal: op.Principal, Conn: op.Conn, Host: op.Host,
		Header: withoutRange(op.Header), Query: op.Query}
	if _, err := eng.Apply(ctx, w.Site, w.Snap, w.Tx, put); err != nil {
		return nil, err
	}
	out := &engine.Result{Status: http.StatusMovedPermanently, Header: http.Header{}}
	out.Header.Set("Location", (&url.URL{Path: target}).EscapedPath())
	return out, nil
}

// firstBase finds the first <base> with a non-empty href (step 4).
func firstBase(root *html.Node) (*html.Node, string) {
	var found *html.Node
	var href string
	dom.Walk(root, func(n *html.Node) bool {
		if found != nil {
			return false
		}
		if n.Type == html.ElementNode && (n.DataAtom == atom.Base || (n.DataAtom == 0 && n.Data == "base")) {
			if h, ok := dom.Attr(n, "href"); ok && strings.TrimSpace(h) != "" {
				found, href = n, strings.TrimSpace(h)
			}
		}
		return true
	})
	return found, href
}

// targetPath resolves href against the template URL (step 5); a
// cross-origin href is 422 (pagelike decision).
func targetPath(templatePath, href, host string) (string, error) {
	u, err := url.Parse(href)
	if err != nil {
		return "", errdoc.New(http.StatusUnprocessableEntity, KindNoBase, "cannot parse <base href=%q>: %v", href, err)
	}
	if u.Host != "" && !strings.EqualFold(hostName(u.Host), hostName(host)) {
		return "", errdoc.New(http.StatusUnprocessableEntity, KindNoBase, "<base href=%q> names another origin", href)
	}
	ref := (&url.URL{Path: templatePath}).ResolveReference(&url.URL{Path: u.Path, RawPath: u.RawPath})
	p, err := engine.NormalizePath(ref.Path)
	if err != nil {
		return "", err
	}
	if p == "/" || strings.HasSuffix(p, "/") {
		return "", errdoc.New(http.StatusUnprocessableEntity, KindNoBase, "<base href=%q> names a directory, not a document", href)
	}
	return p, nil
}

func hostName(h string) string {
	if i := strings.LastIndexByte(h, ':'); i >= 0 && !strings.Contains(h[i:], "]") {
		return h[:i]
	}
	return h
}

// withoutRange is the header of the internal PUT: triggers see no Range
// (R-COMP-141 step 7).
func withoutRange(h http.Header) http.Header {
	c := h.Clone()
	if c == nil {
		c = http.Header{}
	}
	c.Del("Range")
	c.Del("If-Match")
	c.Del("If-None-Match")
	c.Set("Content-Type", "text/html")
	return c
}
