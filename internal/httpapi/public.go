// Package httpapi serves a site's public application plane: the HTTP
// interface compatible clients and PageLove applications use.
package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/budget"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

// Router resolves a request path that has no stored document to a
// parameterized route template (supplied by the composition layer).
type Router func(snap *site.Snapshot, reqPath string) (storedPath string, params map[string]string, ok bool)

// QueryHandler evaluates a QUERY body of a given content type (e.g. Sessel).
// For text/sessel the public plane has already authorized the request as
// method QUERY on its path (docs/spec/protocol.md R-PROTO-74), rejected an
// empty body (400) and a missing target (404); op.Path is the request path
// (a directory path keeps its trailing slash, leaving self unbound) and
// op.Method is "QUERY".
type QueryHandler func(ctx context.Context, s *site.Site, snap *site.Snapshot, op *engine.ReadOp, body []byte, w http.ResponseWriter) error

// Public serves the public plane of every site.
type Public struct {
	// ResolvePrincipal and CheckSession let a managed host bind its identity
	// authority without exposing authoring credentials to public requests.
	ResolvePrincipal func(http.ResponseWriter, *http.Request, *site.Site) *identity.Principal
	CheckSession     func(context.Context, *site.Site, string) bool
	Engine           *engine.Engine
	Compose          engine.Composer
	Route            Router
	Queries          map[string]QueryHandler // extra QUERY content types
	Log              *slog.Logger
	// TrustProxy honours X-Forwarded-Proto for cookie security decisions.
	TrustProxy bool
	// DevAuth enables the X-Pagelike-Dev-User impersonation header. It is
	// only honoured for loopback clients and is logged on every use.
	DevAuth bool
	// Intercept wraps core processing (reactions; intercept.go).
	Intercept Interceptor
}

// reqCtx carries per-request state.
type reqCtx struct {
	site      *site.Site
	principal *identity.Principal
	host      string
}

// Methods pagelike implements on the public plane (sent in Allow on 405).
const publicMethods = "GET, HEAD, PUT, DELETE, POST, MOVE, OPTIONS, QUERY"

// Serve handles a request for site s.
func (p *Public) Serve(w http.ResponseWriter, r *http.Request, s *site.Site) {
	w = engine.ConventionalHeaders(w)
	// One budget per request, shared by every runtime that serves it.
	b := budget.New()
	r = r.WithContext(budget.With(r.Context(), b))
	w = &budgetWriter{ResponseWriter: w, b: b}
	rc := &reqCtx{site: s, host: hostOnly(r.Host)}
	rc.principal = p.principal(w, r, s)
	defer func() {
		if v := recover(); v != nil {
			if v == http.ErrAbortHandler {
				panic(v)
			}
			p.logger().Error("panic", "err", v, "path", r.URL.Path)
			p.fail(w, r, rc, errdoc.New(http.StatusInternalServerError, "InternalError", "internal error"))
		}
	}()
	if p.authEndpoint(w, r, rc) { // login, callback, logout (auth.go)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/-pagelike/") {
		p.pagelikeAPI(w, r, rc)
		return
	}
	clean, err := engine.NormalizePath(r.URL.Path)
	if err != nil {
		p.fail(w, r, rc, err)
		return
	}
	if p.Intercept != nil {
		p.Intercept.Intercept(w, r, p.call(r, rc, clean))
		return
	}
	p.serveMethod(w, r, rc, clean)
}

// serveMethod is core processing: it dispatches a request on its method.
func (p *Public) serveMethod(w http.ResponseWriter, r *http.Request, rc *reqCtx, clean string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		// Only an explicit text/event-stream range subscribes, and only on
		// GET: wildcards would make ordinary fetches hang (R-SSE-1).
		if r.Method == http.MethodGet && engine.AcceptQuality(r.Header.Get("Accept"), "text/event-stream", true) > 0 {
			p.subscribe(w, r, rc, clean)
			return
		}
		p.read(w, r, rc, clean)
	case http.MethodPut, http.MethodPost, http.MethodDelete, "MOVE":
		p.write(w, r, rc, clean)
	case http.MethodOptions:
		p.options(w, r, rc, engine.DocPath(clean))
	case "QUERY":
		p.query(w, r, rc, clean)
	case http.MethodPatch:
		if _, err := p.readBody(w, r, rc); err != nil {
			p.fail(w, r, rc, err)
			return
		}
		p.fail(w, r, rc, errdoc.New(http.StatusNotImplemented, "NotImplemented",
			"PATCH is advertised by PageLove's OPTIONS but its semantics are undocumented; pagelike does not guess them"))
	default:
		w.Header().Set("Allow", publicMethods)
		p.fail(w, r, rc, errdoc.New(http.StatusMethodNotAllowed, "MethodNotAllowed", "method %s is not supported", r.Method))
	}
}

func (p *Public) logger() *slog.Logger {
	if p.Log != nil {
		return p.Log
	}
	return slog.Default()
}

func (p *Public) authzReq(r *http.Request, rc *reqCtx, method, docPath string) authz.Request {
	return authz.Request{Principal: rc.principal, Method: method, Path: docPath, Header: r.Header, Query: r.URL.Query(), RawQuery: r.URL.RawQuery}
}

func (p *Public) readOp(r *http.Request, rc *reqCtx, method, docPath string, rng engine.Range) *engine.ReadOp {
	return &engine.ReadOp{Plane: engine.Public, Method: method, Path: docPath, Range: rng, Accept: r.Header.Get("Accept"),
		Principal: rc.principal, Header: r.Header, Query: r.URL.Query(), RawQuery: r.URL.RawQuery, Host: rc.host}
}

// read serves GET/HEAD for documents, directories, blobs and routes. The
// query string never takes part in lookup (R-RW-2).
func (p *Public) read(w http.ResponseWriter, r *http.Request, rc *reqCtx, clean string) {
	ctx := r.Context()
	s := rc.site
	rng := engine.ParseRange(r.Header.Get("Range"))
	snap, err := s.Index(ctx)
	if err != nil {
		p.fail(w, r, rc, err)
		return
	}
	if rng.HasSelector() {
		rng = engine.ExpandRange(ctx, s, snap, rng) // selector functions (compose)
		if _, err := rng.CheckSelector(); err != nil {
			if !rng.ExpansionFailed() {
				err = engine.InvalidReadSelector(err) // unparsable: 416, as observed live
			}
			p.fail(w, r, rc, err) // a selector function that cannot be evaluated stays 422
			return
		}
	}
	docPath := engine.DocPath(clean)
	doc, err := s.Store.Get(ctx, docPath)
	op := p.readOp(r, rc, r.Method, docPath, rng)
	if errors.Is(err, store.ErrNotFound) {
		if p.redirectDirectory(w, r, rc, snap, clean) {
			return
		}
		if p.Route != nil {
			if sp, ps, ok := p.Route(snap, docPath); ok {
				if d, e := s.Store.Get(ctx, sp); e == nil {
					doc, err, op.Params, op.RoutePath = d, nil, ps, sp
				}
			}
		}
	}
	// Refuse before revealing whether anything exists (R-RW-126).
	if !engine.CanRead(snap, op) {
		p.fail(w, r, rc, engine.Denied(rc.principal, docPath))
		return
	}
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			p.fail(w, r, rc, engine.MissingDocument(docPath))
			return
		}
		p.fail(w, r, rc, err)
		return
	}
	if doc.IsBlob() || !site.IsMarkup(doc.ContentType) {
		p.serveBlob(w, r, rc, snap, doc, op)
		return
	}
	readable := engine.AuthorizeRead(snap, op, nil)
	res, err := p.Engine.ReadMarkup(ctx, s, snap, doc, op, p.Compose)
	if err != nil {
		var e *errdoc.Error
		if !readable && errors.As(err, &e) && e.Status == http.StatusRequestedRangeNotSatisfiable {
			err = engine.Denied(rc.principal, docPath) // no match: absence is not revealed
		}
		p.fail(w, r, rc, err)
		return
	}
	p.respond(w, r, rc, res, rng)
}

// redirectDirectory answers 301 for a slash-less directory whose index the
// requester may read (R-RW-4); it reports whether it did. A final segment
// with a dot is always a file request. The redirect has no body and is
// privately cacheable for an hour (live 2026-09-28).
func (p *Public) redirectDirectory(w http.ResponseWriter, r *http.Request, rc *reqCtx, snap *site.Snapshot, clean string) bool {
	if strings.HasSuffix(clean, "/") || strings.Contains(path.Base(clean), ".") {
		return false
	}
	idx := clean + "/index.html"
	if _, err := rc.site.Store.Get(r.Context(), idx); err != nil {
		return false
	}
	if !engine.CanGrant(snap.Policy, p.authzReq(r, rc, "GET", idx)) {
		return false
	}
	loc := (&url.URL{Path: clean + "/"}).EscapedPath()
	if r.URL.RawQuery != "" {
		loc += "?" + r.URL.RawQuery
	}
	w.Header().Set("Location", loc)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusMovedPermanently)
	return true
}

// respond writes a document read, evaluating conditional requests
// (RFC 9110 §13.2.2 order: If-Match, If-Unmodified-Since, If-None-Match,
// If-Modified-Since) against the representation's validators and serving a
// single byte range of a whole representation (R-RW-13, R-PROTO-36).
func (p *Public) respond(w http.ResponseWriter, r *http.Request, rc *reqCtx, res *engine.ReadResult, rng engine.Range) {
	etag := res.Header.Get("ETag")
	lastMod, _ := http.ParseTime(res.Header.Get("Last-Modified"))
	if res.Status == http.StatusOK || res.Status == http.StatusPartialContent {
		switch evalPreconditions(r, etag, lastMod) {
		case http.StatusPreconditionFailed:
			err := errdoc.Precondition(errdoc.PreconditionETag)
			err.WithHeader("ETag", etag)
			err.WithHeader("Vary", res.Header.Get("Vary"))
			p.fail(w, r, rc, err)
			return
		case http.StatusNotModified:
			copyHeader(w.Header(), res.Header)
			notModified(w)
			return
		}
	}
	body := res.Body
	status := res.Status
	if status == http.StatusOK && rng.Unit == "bytes" && ifRange(r, etag, lastMod) {
		start, end, ok, unsatisfiable := byteRange(rng.Raw, int64(len(body)))
		switch {
		case unsatisfiable:
			err := errdoc.New(http.StatusRequestedRangeNotSatisfiable, "RangeNotSatisfiable", "byte range %s cannot be satisfied", rng.Raw)
			err.WithHeader("Content-Range", fmt.Sprintf("bytes */%d", len(body)))
			err.WithHeader("Vary", res.Header.Get("Vary"))
			err.WithHeader("Accept-Ranges", res.Header.Get("Accept-Ranges"))
			p.fail(w, r, rc, err)
			return
		case ok:
			res.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
			body, status = body[start:end+1], http.StatusPartialContent
		}
	}
	copyHeader(w.Header(), res.Header)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		w.Write(body)
	}
}

// evalPreconditions returns 412, 304 or 0 (proceed) for a read. If-Match is
// ignored on reads, as PageLove ignores it (live 2026-09-28: a stale
// If-Match on a fragment GET is served 206; beta-js sends it on GETs).
func evalPreconditions(r *http.Request, etag string, lastMod time.Time) int {
	if r.Header.Get("If-Match") != "" {
		// ignored
	} else if ius := r.Header.Get("If-Unmodified-Since"); ius != "" && !lastMod.IsZero() {
		if t, err := http.ParseTime(ius); err == nil && lastMod.Truncate(time.Second).After(t) {
			return http.StatusPreconditionFailed
		}
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		if engine.IfNoneMatch(inm, true, etag) {
			return http.StatusNotModified
		}
	} else if ims := r.Header.Get("If-Modified-Since"); ims != "" && !lastMod.IsZero() {
		if t, err := http.ParseTime(ims); err == nil && !lastMod.Truncate(time.Second).After(t) {
			return http.StatusNotModified
		}
	}
	return 0
}

// notModified writes a 304 carrying the validators and caching headers the
// 200/206 would have had, and no representation headers (R-RW-88).
func notModified(w http.ResponseWriter) {
	// As PageLove answers (live 2026-09-28): ETag, Last-Modified when the
	// representation has one, and Vary on Host and Range only.
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Content-Encoding", "Accept-Ranges", "Cache-Control"} {
		w.Header().Del(k)
	}
	w.Header().Set("Vary", engine.WriteVary)
	w.WriteHeader(http.StatusNotModified)
}

// ifRange reports whether a Range may be honoured given If-Range.
func ifRange(r *http.Request, etag string, lastMod time.Time) bool {
	ir := strings.TrimSpace(r.Header.Get("If-Range"))
	if ir == "" {
		return true
	}
	if strings.HasPrefix(ir, `"`) {
		return ir == etag
	}
	t, err := http.ParseTime(ir)
	return err == nil && !lastMod.IsZero() && lastMod.Truncate(time.Second).Equal(t)
}

// byteRange interprets a single "bytes=" range over size bytes. ok is false
// for syntax pagelike ignores (invalid or multiple ranges: the full
// representation is served).
func byteRange(h string, size int64) (start, end int64, ok, unsatisfiable bool) {
	unit, spec, _ := strings.Cut(h, "=")
	if !strings.EqualFold(strings.TrimSpace(unit), "bytes") || strings.Contains(spec, ",") {
		return 0, 0, false, false
	}
	a, b, found := strings.Cut(strings.TrimSpace(spec), "-")
	if !found {
		return 0, 0, false, false
	}
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" {
		n, err := strconv.ParseInt(b, 10, 64)
		if err != nil || n < 0 {
			return 0, 0, false, false
		}
		if n == 0 || size == 0 {
			return 0, 0, false, true
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true, false
	}
	first, err := strconv.ParseInt(a, 10, 64)
	if err != nil || first < 0 {
		return 0, 0, false, false
	}
	last := size - 1
	if b != "" {
		if last, err = strconv.ParseInt(b, 10, 64); err != nil || last < first {
			return 0, 0, false, false
		}
		if last >= size {
			last = size - 1
		}
	}
	if first >= size {
		return 0, 0, false, true
	}
	return first, last, true, false
}

// staticAsset reports whether a content type is a cacheable static asset:
// CSS, JavaScript, images and fonts (R-RW-101).
func staticAsset(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	switch ct {
	case "text/css", "text/javascript", "application/javascript", "application/x-javascript", "application/vnd.ms-fontobject":
		return true
	}
	return strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "font/") || strings.HasPrefix(ct, "application/font-woff")
}

// serveBlob serves an opaque resource byte-for-byte with its stored type,
// content-hash ETag and Last-Modified, ignoring Accept (R-RW-110..113).
// Conditional requests are evaluated here (with the same tag rules as
// documents); byte ranges are left to http.ServeContent.
func (p *Public) serveBlob(w http.ResponseWriter, r *http.Request, rc *reqCtx, snap *site.Snapshot, doc *store.Document, op *engine.ReadOp) {
	if op.Range.HasSelector() {
		p.fail(w, r, rc, engine.SelectorOnNonHTML(doc))
		return
	}
	if !engine.AuthorizeRead(snap, op, nil) {
		p.fail(w, r, rc, engine.Denied(rc.principal, op.Path))
		return
	}
	mod := time.UnixMilli(doc.ModifiedMS).UTC()
	h := w.Header()
	h.Set("Vary", engine.WriteVary) // live: blobs vary on Host, Range
	h.Set("Content-Type", doc.ContentType)
	h.Set("ETag", doc.ETag)
	h.Set("Last-Modified", mod.Format(http.TimeFormat))
	if staticAsset(doc.ContentType) {
		h.Set("Cache-Control", "public, max-age=300")
	}
	switch evalPreconditions(r, doc.ETag, mod) {
	case http.StatusPreconditionFailed:
		for _, k := range []string{"Content-Type", "Last-Modified", "Cache-Control"} {
			h.Del(k)
		}
		p.fail(w, r, rc, errdoc.Precondition(errdoc.PreconditionETag).WithHeader("ETag", doc.ETag))
		return
	case http.StatusNotModified:
		notModified(w)
		return
	}
	size := doc.Size
	if !doc.IsBlob() {
		size = int64(len(doc.Body))
	}
	if op.Range.Unit == "bytes" && ifRange(r, doc.ETag, mod) {
		if _, _, _, unsatisfiable := byteRange(op.Range.Raw, size); unsatisfiable {
			// Answered here so the 416 is an error document too.
			for _, k := range []string{"Content-Type", "Last-Modified", "Cache-Control"} {
				h.Del(k)
			}
			p.fail(w, r, rc, errdoc.New(http.StatusRequestedRangeNotSatisfiable, "RangeNotSatisfiable", "byte range %s cannot be satisfied", op.Range.Raw).
				WithHeader("Content-Range", fmt.Sprintf("bytes */%d", size)).WithHeader("Accept-Ranges", "bytes"))
			return
		}
	}
	// Conditions are settled; let ServeContent do ranges only.
	r2 := r.Clone(r.Context())
	for _, k := range []string{"If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since"} {
		r2.Header.Del(k)
	}
	if doc.IsBlob() {
		f, err := rc.site.Store.OpenBlob(doc.BlobSHA)
		if err != nil {
			p.fail(w, r, rc, err)
			return
		}
		defer f.Close()
		http.ServeContent(w, r2, "", mod, f)
		return
	}
	http.ServeContent(w, r2, "", mod, bytes.NewReader(doc.Body))
}

// readBody reads a request body under the site's cap (R-RW-120..122): a
// declared length over the cap is refused before reading anything, a
// streamed body as soon as it exceeds it; both close the connection.
func (p *Public) readBody(w http.ResponseWriter, r *http.Request, rc *reqCtx) ([]byte, error) {
	return engine.ReadBody(w, r, rc.site.Settings().MaxBodyBytes)
}

// write handles PUT, POST, DELETE and MOVE.
func (p *Public) write(w http.ResponseWriter, r *http.Request, rc *reqCtx, clean string) {
	body, err := p.readBody(w, r, rc)
	if err != nil {
		p.fail(w, r, rc, err)
		return
	}
	rng := engine.ParseRange(r.Header.Get("Range"))
	if rng.HasSelector() {
		if snap, err := rc.site.Index(r.Context()); err == nil {
			rng = engine.ExpandRange(r.Context(), rc.site, snap, rng) // selector functions (compose)
		}
	}
	// Selector writes address the document a GET would serve (a directory
	// URL means its index); whole-resource writes address the literal path.
	target := clean
	if rng.Present() {
		target = engine.DocPath(clean)
	}
	op := &engine.Op{
		Plane: engine.Public, Method: r.Method, Path: target, Range: rng,
		Body: body, ContentType: r.Header.Get("Content-Type"), IfMatch: r.Header.Get("If-Match"), IfNoneMatch: r.Header.Get("If-None-Match"),
		Principal: rc.principal, Conn: r.Header.Get("Pagelove-Connection"), Host: rc.host, Header: r.Header, Query: r.URL.Query(),
	}
	if r.Method == "MOVE" {
		op.DestinationRange = engine.ParseRange(r.Header.Get("Destination-Range"))
		op.Overwrite = r.Header.Get("Overwrite")
		if op.Destination, err = destinationPath(r); err != nil {
			p.fail(w, r, rc, err)
			return
		}
	}
	res, err := p.Engine.Write(r.Context(), rc.site, op)
	if err != nil {
		p.fail(w, r, rc, err)
		return
	}
	w.Header().Set("Vary", engine.WriteVary) // live: every write answer
	copyHeader(w.Header(), res.Header)
	if len(res.Body) > 0 {
		w.Header().Set("Content-Length", strconv.Itoa(len(res.Body)))
	}
	w.WriteHeader(res.Status)
	w.Write(res.Body)
}

// destinationPath reduces a MOVE Destination (a path, as the kanban app
// sends, or an absolute URL, as beta-js sends) to a percent-decoded path;
// query and fragment are ignored. A URL naming another host is 502
// (RFC 4918 §9.9.4).
func destinationPath(r *http.Request) (string, error) {
	v := strings.TrimSpace(r.Header.Get("Destination"))
	if v == "" {
		return "", nil
	}
	u, err := url.Parse(v)
	if err != nil {
		return "", errdoc.New(http.StatusBadRequest, "BadDestination", "cannot parse Destination %q", v)
	}
	if u.Host != "" && !strings.EqualFold(hostOnly(u.Host), hostOnly(r.Host)) {
		return "", errdoc.New(http.StatusBadGateway, "ForeignDestination", "Destination %q names another host", v)
	}
	if u.Path == "" {
		return "/", nil
	}
	return u.Path, nil
}

func copyHeader(dst, src http.Header) {
	for k, v := range src {
		dst[k] = v
	}
}

// fail renders an error as a PageLove-style error document.
func (p *Public) fail(w http.ResponseWriter, r *http.Request, rc *reqCtx, err error) {
	var e *errdoc.Error
	if !errors.As(err, &e) {
		if errors.Is(err, context.Canceled) {
			return
		}
		p.logger().Error("request failed", "err", err, "method", r.Method, "path", r.URL.Path)
		e = errdoc.New(http.StatusInternalServerError, "InternalError", "internal error")
	}
	if e.Resource == "" {
		e.Resource = r.URL.Path
	}
	h := w.Header()
	h.Del("ETag")
	copyHeader(h, e.Headers)
	login := ""
	if rc != nil {
		login = rc.site.Settings().LoginPath
	}
	body, ctype := e.RenderPublic(login), e.MediaType()
	if doc, ok := denialDocument(r, rc, e); ok { // authorization refusal (auth.go)
		// As PageLove: no Vary and no Cache-Control (401/403 are not
		// cacheable by default anyway).
		body, ctype = doc, "text/html; charset=utf-8"
	} else if !e.NoVary && h.Get("Vary") == "" {
		// Every other public-plane error varies like a write (live).
		h.Set("Vary", engine.WriteVary)
	}
	h.Set("Content-Type", ctype)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(e.Status)
	if r.Method != http.MethodHead {
		io.WriteString(w, body)
	}
}

func hostOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}

func isLocalhostName(h string) bool {
	h = strings.ToLower(h)
	return h == "localhost" || strings.HasSuffix(h, ".localhost") || h == "127.0.0.1" || h == "::1"
}

func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func htmlAttr(s string) string {
	return strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;").Replace(s)
}
