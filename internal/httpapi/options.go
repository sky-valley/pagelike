package httpapi

import (
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

// Accept-Query values per plane (R-PROTO-42).
const publicAcceptQuery = "text/css-selector, text/sessel"

// htmlTyped decides from the path alone whether OPTIONS should advertise
// selector ranges (R-PROTO-25): directories, .html/.htm and extension-less
// paths.
func htmlTyped(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case "", ".html", ".htm", ".xhtml":
		return true
	}
	return false
}

// Method sets reported by OPTIONS (docs/spec/protocol.md R-PROTO-12/13), in
// the canonical Allow order. MOVE and PATCH apply to whole documents only;
// QUERY is reported only when a rule names it explicitly (a wildcard grant
// never contributes it). OPTIONS itself is always appended.
var (
	docMethods  = []string{"GET", "HEAD", "PUT", "DELETE", "POST", "MOVE", "PATCH", "QUERY"}
	elemMethods = []string{"GET", "HEAD", "PUT", "DELETE", "POST", "QUERY"}
)

// options answers capability discovery purely from authorization rules; it
// never looks at whether the document or element exists and is never
// refused (PageLove docs, OPTIONS method; R-PROTO-10..22). Multipart mode
// (Accept listing multipart/mixed) answers 207 when a selector-scoped rule
// for this actor covers the path and 204 otherwise; without it the answer
// is a flat 200 for the document or for the Range selector.
func (p *Public) options(w http.ResponseWriter, r *http.Request, rc *reqCtx, docPath string) {
	snap, err := rc.site.Index(r.Context())
	if err != nil {
		p.fail(w, r, rc, err)
		return
	}
	pol := snap.Policy
	req := func(m string) authz.Request { return p.authzReq(r, rc, m, docPath) }
	docAllow := allowSet(pol, req, "", docMethods)
	h := w.Header()
	h.Set("Vary", "Authorization, Accept")
	// The documented advertisements (compat decisions C-4, C-5): live
	// PageLove sends neither, pagelike keeps the superset.
	h.Set("Accept-Query", publicAcceptQuery)
	if htmlTyped(docPath) {
		h.Set("Accept-Ranges", "selector")
	}
	if engine.AcceptQuality(r.Header.Get("Accept"), "multipart/mixed", true) > 0 {
		sels := actorSelectors(pol, req)
		if len(sels) == 0 {
			h.Set("Allow", docAllow)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// The first part is the document-level set; each selector part lists
		// what that selector's own rules grant (live 2026-09-28).
		parts := []engine.Part{{Header: [][2]string{{"Allow", docAllow}}}}
		scoped := selectorPolicy(pol)
		for _, sel := range sels {
			parts = append(parts, engine.Part{Header: [][2]string{
				{"Content-Range", "selector " + sel},
				{"Allow", allowSet(scoped, req, sel, elemMethods)},
			}})
		}
		body, ctype := engine.MultipartBody(parts)
		h.Set("Content-Type", ctype)
		h.Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusMultiStatus)
		if r.Method != http.MethodHead {
			w.Write(body)
		}
		return
	}
	if rng := engine.ParseRange(r.Header.Get("Range")); rng.HasSelector() {
		h.Set("Allow", allowSet(pol, req, rng.Selector, elemMethods))
	} else {
		// The flat document answer includes what selector-scoped rules grant
		// somewhere in the document (live 2026-09-28, superseding R-PROTO-12).
		h.Set("Allow", unionAllow(pol, req, docMethods))
	}
	h.Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

// selectorPolicy is pol restricted to its selector-scoped rules.
func selectorPolicy(pol *authz.Policy) *authz.Policy {
	var rules []authz.Rule
	for _, r := range pol.Rules {
		if strings.TrimSpace(r.Selector) != "" {
			rules = append(rules, r)
		}
	}
	return &authz.Policy{Rules: rules, Groups: pol.Groups, SelectorOptions: pol.SelectorOptions}
}

// unionAllow renders the methods the actor may perform on the document or
// on the elements some selector-scoped rule names.
func unionAllow(pol *authz.Policy, req func(string) authz.Request, methods []string) string {
	sels := pol.SelectorRules(req("GET"))
	var out []string
	for _, m := range methods {
		ok := allowed(pol, req, "", m)
		for _, sel := range sels {
			if ok || !isElemMethod(m) {
				break
			}
			ok = allowed(pol, req, sel, m)
		}
		if ok {
			out = append(out, m)
		}
	}
	return strings.Join(append(out, "OPTIONS"), ", ")
}

// allowSet renders the methods the actor may perform at sel ("" = document
// level) as an Allow value.
func allowSet(pol *authz.Policy, req func(string) authz.Request, sel string, methods []string) string {
	var out []string
	for _, m := range methods {
		if allowed(pol, req, sel, m) {
			out = append(out, m)
		}
	}
	return strings.Join(append(out, "OPTIONS"), ", ")
}

func isElemMethod(m string) bool {
	for _, x := range elemMethods {
		if x == m {
			return true
		}
	}
	return false
}

// allowed reports whether the actor may perform m at sel ("" = document
// level). QUERY counts only when a rule names it explicitly.
func allowed(pol *authz.Policy, req func(string) authz.Request, sel, m string) bool {
	d := pol.DecideText(req(m), sel)
	if !d.Allowed {
		return false
	}
	return m != "QUERY" || (d.Rule != nil && namesMethod(d.Rule.Methods, "QUERY"))
}

func namesMethod(ms []string, m string) bool {
	for _, x := range ms {
		if strings.EqualFold(strings.TrimSpace(x), m) {
			return true
		}
	}
	return false
}

// actorSelectors returns, in rule order, the distinct selectors of the
// selector-scoped rules (after MOVE-rule normalization) that cover the path
// and match this actor; each becomes a part of the 207 (R-PROTO-16/17).
// A rule is tested alone so that other rules cannot mask it.
func actorSelectors(pol *authz.Policy, req func(string) authz.Request) []string {
	var out []string
	for _, sel := range pol.SelectorRules(req("GET")) {
		if actorHasRule(pol, req, sel) {
			out = append(out, sel)
		}
	}
	return out
}

func actorHasRule(pol *authz.Policy, req func(string) authz.Request, sel string) bool {
	for _, rule := range pol.Rules {
		if strings.TrimSpace(rule.Selector) == "" || namesMethod(rule.Methods, "MOVE") {
			continue
		}
		one := &authz.Policy{Rules: []authz.Rule{rule}, Groups: pol.Groups}
		for _, m := range rule.Methods {
			if m = strings.ToUpper(strings.TrimSpace(m)); m == "*" {
				m = "GET"
			}
			if one.DecideText(req(m), sel).Matched {
				return true
			}
		}
	}
	return false
}

// query implements QUERY on the public edge (R-PROTO-40..75): the request
// Content-Type selects css-selector mode (like a GET with a selector range
// of the composed page, or every match as multipart when asked for) or
// Sessel mode (evaluated by a registered handler).
func (p *Public) query(w http.ResponseWriter, r *http.Request, rc *reqCtx, clean string) {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if strings.ToLower(ct) == "text/css-selector" {
		p.queryCSS(w, r, rc, clean)
		return
	}
	// Anything else is the Sessel path, authorized as QUERY, and a body
	// that is not text/sessel is then 400 (live 2026-09-28, superseding
	// the documented 415 with Accept-Query, R-PROTO-43).
	p.querySessel(w, r, rc, clean, strings.ToLower(ct) == "text/sessel")
}

func (p *Public) queryCSS(w http.ResponseWriter, r *http.Request, rc *reqCtx, clean string) {
	ctx := r.Context()
	body, err := p.readBody(w, r, rc)
	if err != nil {
		p.fail(w, r, rc, err)
		return
	}
	snap, err := rc.site.Index(ctx)
	if err != nil {
		p.fail(w, r, rc, err)
		return
	}
	// Only the single, individually addressed document (a directory URL
	// means its index), authorized exactly like a GET of it (R-PROTO-60/62).
	docPath := engine.DocPath(clean)
	rng := engine.Range{Unit: "selector", Selector: strings.TrimSpace(string(body)), Raw: "selector=" + strings.TrimSpace(string(body))}
	op := p.readOp(r, rc, http.MethodGet, docPath, rng)
	if !engine.CanRead(snap, op) {
		p.fail(w, r, rc, engine.Denied(rc.principal, docPath))
		return
	}
	if _, err := rng.CheckSelector(); err != nil {
		p.fail(w, r, rc, engine.InvalidReadSelector(err))
		return
	}
	doc, err := rc.site.Store.Get(ctx, docPath)
	if err != nil {
		if err == store.ErrNotFound {
			err = engine.MissingDocument(docPath)
		}
		p.fail(w, r, rc, err)
		return
	}
	if doc.IsBlob() || !site.IsMarkup(doc.ContentType) {
		p.fail(w, r, rc, engine.SelectorOnNonHTML(doc))
		return
	}
	// Exactly the GET-with-Range answer, one match or (when multipart is
	// accepted) every match (live-observed, R-PROTO-60..64).
	res, err := p.Engine.ReadMarkup(ctx, rc.site, snap, doc, op, p.Compose)
	if err != nil {
		p.fail(w, r, rc, err)
		return
	}
	p.respond(w, r, rc, res, engine.Range{})
}

// querySessel checks what every Sessel QUERY needs before evaluation and
// hands the request to the registered text/sessel handler (R-PROTO-74/75,
// as reconciled with live PageLove 2026-09-28): it is authorized as method
// QUERY on the request path, never by read access or the default-GET mode;
// then a body that is not text/sessel is 400, an empty program 422, and a
// missing target 404 (a directory URL means its index document, whose
// absence is 404 too). Without a Sessel engine the answer is 501.
func (p *Public) querySessel(w http.ResponseWriter, r *http.Request, rc *reqCtx, clean string, sessel bool) {
	ctx := r.Context()
	body, err := p.readBody(w, r, rc)
	if err != nil {
		p.fail(w, r, rc, err)
		return
	}
	snap, err := rc.site.Index(ctx)
	if err != nil {
		p.fail(w, r, rc, err)
		return
	}
	docPath := engine.DocPath(clean)
	if !snap.Policy.Decide(p.authzReq(r, rc, "QUERY", docPath), nil).Allowed {
		p.fail(w, r, rc, engine.Denied(rc.principal, docPath))
		return
	}
	if !sessel {
		p.fail(w, r, rc, errdoc.Read(http.StatusBadRequest, "UnsupportedQueryType", "QUERY method requires Content-Type: text/sessel"))
		return
	}
	if strings.TrimSpace(string(body)) == "" {
		p.fail(w, r, rc, errdoc.Read(http.StatusUnprocessableEntity, "EmptyQuery", "Invalid path: QUERY method requires a body containing the sessel expression"))
		return
	}
	if _, err := rc.site.Store.Get(ctx, docPath); err != nil {
		if err == store.ErrNotFound {
			err = engine.MissingDocument(docPath)
		}
		p.fail(w, r, rc, err)
		return
	}
	h := p.Queries["text/sessel"]
	if h == nil {
		p.fail(w, r, rc, errdoc.New(http.StatusNotImplemented, "NotImplemented", "Sessel queries are not available on this server"))
		return
	}
	op := p.readOp(r, rc, "QUERY", docPath, engine.ParseRange(r.Header.Get("Range")))
	op.Target = clean // a directory target leaves self unbound (R-PROTO-71)
	if err := h(ctx, rc.site, snap, op, body, w); err != nil {
		p.fail(w, r, rc, err)
	}
}
