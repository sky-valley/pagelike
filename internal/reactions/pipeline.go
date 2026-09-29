package reactions

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/httpapi"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

// Intercept implements httpapi.Interceptor: the request pipeline of
// docs/spec/reacting.md R-REACT-8.
func (x *Reactions) Intercept(w http.ResponseWriter, r *http.Request, c *httpapi.Call) {
	ctx := r.Context()
	s := c.Site
	snap, err := s.Index(ctx)
	if err != nil {
		c.Next(w, r)
		return
	}
	ix := indexFor(snap) // the reaction item set is fixed at arrival (R-REACT-5)
	if ix.empty() {
		c.Next(w, r)
		return
	}
	method := r.Method
	write := isWrite(method)
	paths := candidatePaths(c.Path)
	trig := filterRules(ix.triggers, method, paths)
	var proc []*rule
	if !c.Subscribe { // a stream cannot be buffered (R-REACT-6)
		proc = filterRules(ix.processors, method, paths)
	}
	transitions := write && (len(ix.constraints) > 0 || len(ix.handlers) > 0)
	if len(trig) == 0 && len(proc) == 0 && !transitions {
		c.Next(w, r)
		return
	}

	rng := engine.ParseRange(r.Header.Get("Range"))
	st := &reqState{x: x, site: s, call: c, r: r, method: method, path: c.Path, principal: c.Principal,
		docPath: engine.DocPath(c.Path), target: c.Path, arrival: map[string]int64{}}
	if rng.Present() {
		st.target = st.docPath // selector writes address the served document
	}
	st.origin = c.Scheme + "://" + r.Host
	if pd := snap.Docs[st.docPath]; pd != nil {
		st.prior = &sessel.Document{Path: pd.Path, Type: pd.Type, Root: pd.Root}
	}
	if write && len(ix.constraints) > 0 {
		st.arrival[st.target] = versionIn(snap, st.target)
	}
	st.hasSelector = rng.HasSelector()
	ctx = withState(ctx, st)
	r = r.WithContext(ctx)
	st.r = r

	bodyRead := false
	if (len(trig) > 0 || len(proc) > 0) && hasBody(r) {
		body, err := c.ReadBody(w, r)
		if err != nil { // 413 framing rejections come before reactions (R-REACT-10)
			c.Fail(w, r, err)
			return
		}
		st.body, bodyRead = body, true
	}
	st.req = sessel.NewRequest(method, c.Path, contextHeaders(r), r.URL.Query(), nil, st.body, authOf(c.Principal))
	st.cctx = sessel.NewContext(st.req)

	// Trigger phase: only for requests authorization lets through; a
	// denial skips triggers and core produces it (decision C1).
	if len(trig) > 0 && x.authorized(snap, st, rng) {
		st.phase = phaseTriggers
		if o := x.runRules(ctx, st, trig, nil); o.done() {
			x.respondOutcome(w, r, st, o)
			x.finish(ctx, st, 0)
			return
		}
	}

	core := r
	if bodyRead {
		core = withBody(r, st.body)
	}
	st.mu.Lock()
	st.phase = phaseCore
	st.mu.Unlock()
	if len(proc) == 0 {
		sw := &statusWriter{ResponseWriter: w}
		c.Next(sw, core)
		x.finish(ctx, st, sw.status)
		return
	}
	// Processors see the full representation, for HEAD too (R-REACT-26).
	coreReq := core
	if method == http.MethodHead {
		coreReq = core.Clone(ctx)
		coreReq.Method = http.MethodGet
	}
	buf := newBuffer()
	c.Next(buf, coreReq)
	x.settleCore(st, buf.code())
	st.mu.Lock()
	st.phase = phaseProcessors
	st.mu.Unlock()
	resp := &response{status: buf.code(), header: buf.header, body: buf.body.Bytes()}
	o := x.runRules(ctx, st, proc, resp)
	if o.done() {
		x.respondOutcome(w, r, st, o)
	} else {
		resp.write(w, r)
	}
	x.finish(ctx, st, -1)
}

// settleCore records whether the main write committed: its tentative
// outbox rows (queued requests, handler deliveries) stand only then.
func (x *Reactions) settleCore(st *reqState, status int) {
	committed := status > 0 && status < 400
	st.settle(committed)
	if !committed {
		st.mu.Lock()
		st.persisted = 0
		st.mu.Unlock()
	}
}

// finish dispatches what the request queued, after its response has been
// written (R-REACT-56): queued requests not already recorded by the main
// write's transaction are stored now, held rows are released, and the
// delivery worker is woken. coreStatus is the core response status when
// settleCore has not run yet (0: core did not run; -1: already settled).
func (x *Reactions) finish(ctx context.Context, st *reqState, coreStatus int) {
	if coreStatus >= 0 {
		x.settleCore(st, coreStatus)
	}
	st.mu.Lock()
	st.phase = phaseDone
	rest := append([]*outRequest(nil), st.queue[min(st.persisted, len(st.queue)):]...)
	held := append([]int64(nil), st.held...)
	st.mu.Unlock()
	ctx = context.WithoutCancel(ctx)
	if len(rest) > 0 {
		if err := x.out.insert(ctx, st.site, rest); err != nil {
			x.log.Error("reactions: queueing outbound requests", "err", err, "site", st.site.Name)
		}
	}
	if len(held) > 0 {
		if err := x.out.release(ctx, st.site, held); err != nil {
			x.log.Error("reactions: releasing outbound requests", "err", err, "site", st.site.Name)
		}
	}
	if len(rest) > 0 || len(held) > 0 {
		x.out.wake(st.site)
	}
}

// authorized is the authorization pre-check of R-REACT-8 step 4: could any
// rule grant the request's method on its path. Denied requests skip the
// trigger phase; core then produces the refusal.
func (x *Reactions) authorized(snap *site.Snapshot, st *reqState, rng engine.Range) bool {
	r := st.r
	req := authz.Request{Principal: st.principal, Method: st.method, HTTPMethod: st.method, Path: st.docPath,
		Header: r.Header, Query: r.URL.Query(), RawQuery: r.URL.RawQuery}
	switch st.method {
	case http.MethodGet, http.MethodHead:
		req.Subscribe = st.call.Subscribe
		return engine.CanGrant(snap.Policy, req)
	case http.MethodPut, http.MethodPost, http.MethodDelete, "MOVE":
		req.Path = st.target
		return engine.CanGrant(snap.Policy, req)
	case "QUERY":
		if engine.CanGrant(snap.Policy, req) {
			return true
		}
		req.Method = http.MethodGet
		return engine.CanGrant(snap.Policy, req)
	}
	return true // OPTIONS, PATCH and other methods carry no authorization of their own
}

// ---------------------------------------------------------------- matching

func isWrite(m string) bool {
	switch m {
	case http.MethodPut, http.MethodPost, http.MethodDelete, "MOVE", http.MethodPatch:
		return true
	}
	return false
}

// candidatePaths are the spellings a resource glob is matched against: the
// request path and, for a directory, its index document (and back).
func candidatePaths(p string) []string {
	out := []string{p}
	switch {
	case strings.HasSuffix(p, "/"):
		out = append(out, p+"index.html")
	case strings.HasSuffix(p, "/index.html"):
		out = append(out, strings.TrimSuffix(p, "index.html"))
	}
	return out
}

// filterRules keeps the rules whose resource and method filters match
// (R-REACT-11..13); the selector and status filters need more context.
func filterRules(rules []*rule, method string, paths []string) []*rule {
	var out []*rule
	for _, ru := range rules {
		if ru.matchResource(paths) && ru.matchMethod(method) {
			out = append(out, ru)
		}
	}
	return out
}

func (ru *rule) matchResource(paths []string) bool {
	if len(ru.resources) == 0 {
		return true
	}
	for _, g := range ru.resources {
		for _, p := range paths {
			if g.Match(p) {
				return true
			}
		}
	}
	return false
}

// matchMethod: case-insensitive, GET also matches HEAD, "*" matches all.
func (ru *rule) matchMethod(m string) bool {
	if len(ru.methods) == 0 {
		return true
	}
	um := strings.ToUpper(m)
	for _, x := range ru.methods {
		if x == "*" || x == um || (x == http.MethodGet && um == http.MethodHead) {
			return true
		}
	}
	return false
}

// matchStatus implements R-REACT-15 (processors only).
func (ru *rule) matchStatus(status int) bool {
	if !ru.processor || ru.statusRaw == 0 {
		return true
	}
	code := strconv.Itoa(status)
	for _, p := range ru.statuses {
		if p == code || (strings.HasSuffix(p, "xx") && len(code) == 3 && code[0] == p[0]) {
			return true
		}
	}
	return false
}

func versionIn(snap *site.Snapshot, p string) int64 {
	if pd := snap.Docs[p]; pd != nil {
		return pd.Version
	}
	return 0
}

// ---------------------------------------------------------------- request data

func hasBody(r *http.Request) bool {
	return r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0
}

func withBody(r *http.Request, body []byte) *http.Request {
	r2 := r.Clone(r.Context())
	r2.Body = io.NopCloser(bytes.NewReader(body))
	r2.ContentLength = int64(len(body))
	r2.Header.Del("Content-Length")
	return r2
}

// contextHeaders are the request headers as reactions see them
// (R-REACT-23), including Host, which net/http keeps apart.
func contextHeaders(r *http.Request) http.Header {
	h := r.Header.Clone()
	if h == nil {
		h = http.Header{}
	}
	if r.Host != "" && h.Get("Host") == "" {
		h.Set("Host", r.Host)
	}
	return h
}

// authOf builds request.auth (R-PERM-74) for Sessel.
func authOf(p *identity.Principal) *sessel.Dict {
	if p == nil || !p.Authenticated {
		return sessel.NewAuth("", nil, nil)
	}
	return sessel.NewAuth(username(p), claimsOf(p), p.Roles)
}

func username(p *identity.Principal) string {
	if p.Username != "" {
		return p.Username
	}
	return p.Sub
}

func claimsOf(p *identity.Principal) map[string]any {
	claims := map[string]any{}
	for k, v := range p.Claims {
		claims[k] = v
	}
	set := func(k string, v any) {
		if _, ok := claims[k]; !ok {
			claims[k] = v
		}
	}
	set("sub", p.Sub)
	if p.Email != "" {
		set("email", p.Email)
		set("email_verified", p.EmailVerified)
	}
	if p.Name != "" {
		set("name", p.Name)
	}
	return claims
}

// ---------------------------------------------------------------- response capture

// response is a buffered core (or processed) response.
type response struct {
	status  int
	header  http.Header
	body    []byte
	changed bool // the body was replaced by a processor
}

// write sends the response: headers as captured (a replaced body drops the
// validators that described the old one, R-REACT-44), no body for HEAD.
func (resp *response) write(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	for k, v := range resp.header {
		h[k] = v
	}
	if resp.changed {
		h.Del("ETag")
		h.Del("Content-Range")
		h.Set("Content-Length", strconv.Itoa(len(resp.body)))
	}
	w.WriteHeader(resp.status)
	if r.Method != http.MethodHead && bodyAllowed(resp.status) {
		w.Write(resp.body)
	}
}

func bodyAllowed(status int) bool {
	return status >= 200 && status != http.StatusNoContent && status != http.StatusNotModified
}

// buffer is a ResponseWriter that captures core processing's response.
type buffer struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func newBuffer() *buffer { return &buffer{header: http.Header{}} }

func (b *buffer) Header() http.Header { return b.header }

func (b *buffer) WriteHeader(code int) {
	if b.status == 0 && code >= 200 {
		b.status = code
	}
}

func (b *buffer) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

// Flush is a no-op (buffered).
func (b *buffer) Flush() {}

func (b *buffer) code() int {
	if b.status == 0 {
		return http.StatusOK
	}
	return b.status
}

// statusWriter passes a response through, remembering its status.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 && code >= 200 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(p []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(p)
}

// Flush supports streaming responses (SSE subscriptions).
func (s *statusWriter) Flush() {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }
