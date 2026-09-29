// Package engine implements PageLove document semantics independent of the
// transport: selector-addressed reads and writes, conditional requests,
// authorization decisions, validation hooks and event generation. The public
// HTTP plane, the WebDAV authoring plane and server-side actions (triggers)
// all mutate documents through it.
//
// Every write follows one pipeline order (docs/spec/reading-writing.md
// R-RW-90): request validation (malformed ranges, placements) → reserved
// namespace → authorization of the method on the path → resource existence
// → resource kind (selector on a blob) → range resolution (416, or the
// refusals that keep absence hidden from those who may not read) → element
// authorization → preconditions (412) → mutation, triggers and validation
// (422) → commit. The request-body size cap is enforced by the transports
// before any of this. Preconditions are evaluated under the site write
// mutex, so a write that loses a race is 412 rather than applied.
package engine

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/sse"
	"github.com/sky-valley/pagelike/internal/store"
)

// Plane distinguishes the public application plane from authoring.
type Plane int

const (
	Public Plane = iota
	Authoring
)

// Op is a write request.
type Op struct {
	// Quiet suppresses mutation events: side-effect writes of triggers and
	// processors are not streamed by PageLove (live 2026-09-29, sse
	// decisions); the write itself still commits and validates normally.
	Quiet       bool
	Plane       Plane
	Method      string // PUT, POST, DELETE, MOVE, PATCH
	Path        string // normalized absolute path
	Range       Range
	Body        []byte
	ContentType string
	IfMatch     string
	IfNoneMatch string
	Principal   *identity.Principal
	Conn        string // Pagelove-Connection token of the writer, if any
	Host        string
	Header      http.Header
	Query       url.Values

	// MOVE
	Destination      string // destination path (already reduced from a URL)
	DestinationRange Range
	Overwrite        string // whole-document MOVE: "F" refuses to replace
}

// Result is a write outcome.
type Result struct {
	Status int
	Header http.Header
	Body   []byte
	Events []store.Event
}

// Hooks are extension points filled in by higher-level packages. Each list
// runs in registration order; the first error aborts the write.
//
// Registration order is the pipeline order the specs require: reactions
// register Triggers in BeforeWrite, modeling registers schema/shape
// validation and reactions register TransitionConstraints in Validate,
// reactions register Processors and the outbox in AfterWrite.
type Hooks struct {
	// BeforeWrite runs before the mutation is applied (triggers). It may
	// rewrite op.Body or refuse with an *errdoc.Error.
	BeforeWrite []func(ctx context.Context, w *WriteCtx) error
	// Validate runs after the mutation is applied to the in-memory DOMs and
	// before anything is stored (schemas, shapes, transitions). Feature
	// packages add to it with AddValidate, whose phases fix the order
	// whatever the order packages register in.
	Validate []func(ctx context.Context, w *WriteCtx) error
	// AfterWrite runs after storage, inside the transaction (processors,
	// outbox entries). It may adjust the result. See AddAfterWrite.
	AfterWrite []func(ctx context.Context, w *WriteCtx, res *Result) error

	// Phases of the Validate and AfterWrite entries (parallel to the
	// lists; entries appended directly are PhaseDefault).
	validatePhases []Phase
	afterPhases    []Phase
}

// Phase orders hooks within the Validate and AfterWrite lists: a lower
// phase runs first, hooks of one phase in the order they were added. The
// write pipeline's order (docs/spec/modeling.md R-MOD-15) is thereby
// explicit instead of depending on Go package initialization order.
type Phase int

const (
	// PhaseSchema: schemas, properties, types, shapes, uniqueness,
	// references and restrict (R-MOD-15 steps 1-9); cascades after the
	// write (they are part of the write processors observe).
	PhaseSchema Phase = 100
	// PhaseTransition: TransitionConstraint validation (R-MOD-15 step 10,
	// R-REACT-71): after every schema check, so a duplicate key fails first
	// as a uniqueness 422.
	PhaseTransition Phase = 200
	// PhaseDefault: hooks added without a phase (appended to the lists).
	PhaseDefault Phase = 1000
)

// AddValidate adds a Validate hook in phase p.
func (h *Hooks) AddValidate(p Phase, f func(ctx context.Context, w *WriteCtx) error) {
	h.Validate, h.validatePhases = insertPhased(h.Validate, h.validatePhases, p, f)
}

// AddAfterWrite adds an AfterWrite hook in phase p.
func (h *Hooks) AddAfterWrite(p Phase, f func(ctx context.Context, w *WriteCtx, res *Result) error) {
	h.AfterWrite, h.afterPhases = insertPhased(h.AfterWrite, h.afterPhases, p, f)
}

// ValidatePhases reports the phase of each Validate hook, in run order.
func (h *Hooks) ValidatePhases() []Phase {
	return padPhases(h.validatePhases, len(h.Validate))
}

// AfterWritePhases reports the phase of each AfterWrite hook, in run order.
func (h *Hooks) AfterWritePhases() []Phase {
	return padPhases(h.afterPhases, len(h.AfterWrite))
}

func padPhases(ps []Phase, n int) []Phase {
	out := append([]Phase(nil), ps...)
	for len(out) < n {
		out = append(out, PhaseDefault)
	}
	return out[:n]
}

// insertPhased inserts f after every hook of a phase <= p.
func insertPhased[F any](list []F, phases []Phase, p Phase, f F) ([]F, []Phase) {
	phases = padPhases(phases, len(list))
	i := len(list)
	for j, q := range phases {
		if q > p {
			i = j
			break
		}
	}
	return slices.Insert(list, i, f), slices.Insert(phases, i, p)
}

func (e *Engine) runBefore(ctx context.Context, w *WriteCtx) error {
	for _, f := range e.Hooks.BeforeWrite {
		if err := f(ctx, w); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) validate(ctx context.Context, w *WriteCtx) error {
	for _, f := range e.Hooks.Validate {
		if err := f(ctx, w); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) after(ctx context.Context, w *WriteCtx, res *Result) (*Result, error) {
	for _, f := range e.Hooks.AfterWrite {
		if err := f(ctx, w, res); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// WriteCtx is the state of one write as it moves through the pipeline.
type WriteCtx struct {
	Site     *site.Site
	Snap     *site.Snapshot
	Tx       *store.Tx
	Op       *Op
	Doc      *store.Document // current stored document (nil if absent)
	Before   *html.Node      // parsed document before the write (nil if absent/blob)
	After    *html.Node      // parsed document after the write (nil for deletes/blobs)
	Target   *html.Node      // targeted element in After (or Before for DELETE)
	Inserted []*html.Node    // nodes inserted by POST/PUT
	Engine   *Engine
	// Extra documents changed by side-effect writes in the same transaction.
	Extra []*store.Document
	// Via is the composed page a write-through was addressed to (the
	// request path) when this write lands in an origin document
	// (ApplyRouted); "" otherwise. ShapeConstraints apply when their glob
	// matches either path (docs/spec/composing.md R-COMP-93).
	Via string
	// ViaSelector is the selector the client addressed on the Via page; a
	// write-through's event reports it (live 2026-09-28).
	ViaSelector string

	spans      *dom.Spans // source spans of Before in Doc.Body (lazily computed for HTML)
	sideEffect bool       // a write made on behalf of another (Apply)
	cascade    bool       // a schema cascade: announced like the request itself
	// Composition (compose_hooks.go): a write routed from a composed page
	// was authorized against that page (preauth); locate pins the target
	// resolved in the composed view.
	preauth bool
	locate  func(before *html.Node) *html.Node
}

// Engine executes operations against sites.
type Engine struct {
	Hooks Hooks
	// Domain is the base domain sites are served under; a site's canonical
	// host (reported in mutation events) is <site>.<Domain>.
	Domain string
}

// ReservedPrefix is the platform namespace that is never writable.
const ReservedPrefix = "/.pagelove/"

// NormalizePath cleans a request path, preserving a trailing slash, and
// rejects traversal.
func NormalizePath(p string) (string, error) {
	if p == "" || p[0] != '/' {
		p = "/" + p
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", errdoc.New(http.StatusBadRequest, "BadPath", "path traversal is not allowed")
		}
	}
	trailing := strings.HasSuffix(p, "/")
	c := path.Clean(p)
	if trailing && c != "/" {
		c += "/"
	}
	return c, nil
}

// DocPath maps a directory path to its index document.
func DocPath(p string) string {
	if strings.HasSuffix(p, "/") {
		return p + "index.html"
	}
	return p
}

// extTypes is pagelike's extension table for content-type inference
// (docs/spec/reading-writing.md R-RW-60); it is consulted before the
// platform's MIME table so results do not depend on the host OS.
var extTypes = map[string]string{
	".css": "text/css", ".js": "text/javascript", ".mjs": "text/javascript", ".json": "application/json",
	".xml": "application/xml", ".rss": "application/rss+xml", ".atom": "application/atom+xml",
	".svg": "image/svg+xml", ".xhtml": "application/xhtml+xml", ".png": "image/png", ".jpg": "image/jpeg",
	".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif",
	".ico": "image/x-icon", ".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf",
	".pdf": "application/pdf", ".txt": "text/plain", ".csv": "text/csv", ".md": "text/markdown",
	".zip": "application/zip", ".wasm": "application/wasm", ".mp3": "audio/mpeg", ".mp4": "video/mp4",
	".webm": "video/webm",
}

// ResolveContentType applies PageLove's upload rule order: .html/.htm is
// always text/html; else the explicit header, verbatim; else the
// extension; else application/octet-stream.
func ResolveContentType(p, header string) string {
	ext := strings.ToLower(path.Ext(p))
	if ext == ".html" || ext == ".htm" {
		return "text/html"
	}
	if h := strings.TrimSpace(header); h != "" {
		return h
	}
	if t, ok := extTypes[ext]; ok {
		return t
	}
	if ext != "" {
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
	}
	return "application/octet-stream"
}

// Denied returns 401 for anonymous principals and 403 otherwise.
func Denied(p *identity.Principal, resource string) *errdoc.Error {
	return errdoc.Denied(p != nil && p.Authenticated, resource)
}

// CanGrant reports whether any rule, at any granularity, can grant the
// request's method on its path to its principal: the document-level
// decision, or the decision for some selector named by a selector-scoped
// rule. A request that fails this is refused before existence is revealed
// (docs/spec/reading-writing.md R-RW-126).
func CanGrant(pol *authz.Policy, req authz.Request) bool {
	if pol.Decide(req, nil).Allowed {
		return true
	}
	for _, sel := range pol.SelectorRules(req) {
		if pol.DecideText(req, sel).Allowed {
			return true
		}
	}
	return false
}

func (w *WriteCtx) authzReq(method string) authz.Request {
	return authz.Request{Principal: w.Op.Principal, Method: method, HTTPMethod: w.Op.Method, Path: w.Op.Path, Header: w.Op.Header, Query: w.Op.Query}
}

// authorize checks method on target (nil: document level).
func (e *Engine) authorize(w *WriteCtx, method string, target *html.Node) error {
	if w.Op.Plane == Authoring || w.preauth {
		return nil
	}
	if !w.Snap.Policy.Decide(w.authzReq(method), target).Allowed {
		return Denied(w.Op.Principal, w.Op.Path)
	}
	return nil
}

// authorizeAny refuses a write whose method no rule could grant on the path.
func (e *Engine) authorizeAny(w *WriteCtx, method string) error {
	if w.Op.Plane == Authoring || w.preauth || CanGrant(w.Snap.Policy, w.authzReq(method)) {
		return nil
	}
	return Denied(w.Op.Principal, w.Op.Path)
}

// canRead reports whether the principal may read the whole document.
func (e *Engine) canRead(w *WriteCtx) bool {
	return w.Op.Plane == Authoring || w.Snap.Policy.Decide(w.authzReq("GET"), nil).Allowed
}

// Write executes a mutating operation. It serializes with the site's write
// mutex and commits documents and events in one transaction.
func (e *Engine) Write(ctx context.Context, s *site.Site, op *Op) (*Result, error) {
	if op.Plane == Public && strings.HasPrefix(op.Path, ReservedPrefix) {
		return nil, reserved()
	}
	if err := checkRequest(op); err != nil {
		return nil, err
	}
	s.WriteMu.Lock()
	defer s.WriteMu.Unlock()
	snap, err := s.Index(ctx)
	if err != nil {
		return nil, err
	}
	var res *Result
	events, err := s.Store.Update(ctx, func(tx *store.Tx) error {
		w := &WriteCtx{Site: s, Snap: snap, Tx: tx, Op: op, Engine: e}
		r, err := e.apply(ctx, w)
		if err != nil {
			return err
		}
		res = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	res.Events = events
	publish(s, snap, events)
	return res, nil
}

// Apply runs an operation inside an existing transaction (used for
// side-effect writes from triggers and processors). Events are recorded in
// the transaction and published by the outer Write; they are attributed to
// no session, so the writer whose request caused them receives them too.
func (e *Engine) Apply(ctx context.Context, s *site.Site, snap *site.Snapshot, tx *store.Tx, op *Op) (*Result, error) {
	if op.Plane == Public && strings.HasPrefix(op.Path, ReservedPrefix) {
		return nil, reserved()
	}
	if err := checkRequest(op); err != nil {
		return nil, err
	}
	return e.apply(ctx, &WriteCtx{Site: s, Snap: snap, Tx: tx, Op: op, Engine: e, sideEffect: true})
}

func reserved() error {
	return errdoc.New(http.StatusForbidden, "ReservedNamespace", "paths under /.pagelove/ are reserved and not writable")
}

// checkRequest rejects malformed requests before anything is looked up.
func checkRequest(op *Op) error {
	switch op.Method {
	case "MOVE":
		return nil // validated by move, which has its own error table
	case "PUT", "POST", "DELETE":
	default:
		return errdoc.New(http.StatusNotImplemented, "NotImplemented", "method %s is not supported", op.Method)
	}
	switch {
	case op.Range.HasSelector():
		if _, err := op.Range.CheckSelector(); err != nil {
			return err
		}
		// An unknown POST placement is an append (live 2026-09-28,
		// superseding R-RW-71); postSelector maps it.
		if op.Method != "DELETE" && dom.TrimHTMLSpace(string(op.Body)) == "" {
			// Compat decision (spec R-RW-67, Q-3): nothing to write.
			return errdoc.New(http.StatusBadRequest, "EmptyBody", "a selector %s needs markup in the request body", op.Method)
		}
	case op.Range.Present():
		// Only selector ranges address part of a document on writes; any
		// other unit would silently turn into a whole-resource write.
		return errdoc.New(http.StatusNotImplemented, "RangeUnitNotSupported", "writes accept only the selector range unit, not %q", op.Range.Raw)
	case strings.HasSuffix(op.Path, "/"):
		if op.Method == "POST" {
			// Docs disagree on POST to a directory (spec §23 C-9).
			return errdoc.New(http.StatusNotImplemented, "NotImplemented", "POST to a directory is not implemented")
		}
		// Whole-resource writes address the literal path (R-RW-63).
		return errdoc.New(http.StatusBadRequest, "BadPath", "a whole-resource %s needs a document path, not a directory", op.Method)
	}
	return nil
}

func (e *Engine) apply(ctx context.Context, w *WriteCtx) (*Result, error) {
	op := w.Op
	op.Range = op.Range.BindSnapshot(w.Snap)
	op.DestinationRange = op.DestinationRange.BindSnapshot(w.Snap)
	doc, err := w.Tx.Get(op.Path)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	w.Doc = doc
	if doc != nil && !doc.IsBlob() && site.IsMarkup(doc.ContentType) {
		if site.IsXML(doc.ContentType) {
			// The XML parser records source spans as it goes.
			if root, sp, err := dom.ParseXMLWithSpans(doc.Body); err == nil {
				w.Before, w.spans = root, &sp
			}
		} else {
			w.Before, _ = dom.Parse(doc.Body)
		}
	}
	if res, done, err := e.composeWrite(ctx, w); done {
		return res, err
	}
	switch op.Method {
	case "PUT":
		if op.Range.HasSelector() {
			return e.putSelector(ctx, w)
		}
		return e.putDocument(ctx, w)
	case "POST":
		if op.Range.HasSelector() {
			return e.postSelector(ctx, w)
		}
		return e.postCreate(ctx, w)
	case "DELETE":
		if op.Range.HasSelector() {
			return e.deleteSelector(ctx, w)
		}
		return e.deleteDocument(ctx, w)
	case "MOVE":
		return e.move(ctx, w)
	}
	return nil, errdoc.New(http.StatusNotImplemented, "NotImplemented", "method %s is not supported", op.Method)
}

// checkPreconditions evaluates If-Match / If-None-Match for a write against
// the one tag the write is conditioned on: the document's stored tag for a
// whole-document write, the addressed element's tag for a selector write
// (the target of PUT and DELETE, the anchor of POST, the source of MOVE).
// PageLove compares nothing else: a document tag on a selector write is
// stale (live 2026-09-28, superseding R-RW-89). The 412 carries that tag
// (R-RW-91) and PageLove's wording.
func (e *Engine) checkPreconditions(w *WriteCtx, current string) error {
	op := w.Op
	exists := w.Doc != nil
	fail := func(msg string) error {
		err := errdoc.Precondition(msg)
		if exists && current != "" {
			err.WithHeader("ETag", current)
		}
		return err
	}
	if op.IfMatch != "" && !IfMatch(op.IfMatch, exists, current) {
		if !exists {
			return fail(errdoc.PreconditionMissing)
		}
		return fail(errdoc.PreconditionETag)
	}
	if op.IfNoneMatch != "" && IfNoneMatch(op.IfNoneMatch, exists, current) {
		if strings.TrimSpace(op.IfNoneMatch) == "*" {
			return fail(errdoc.PreconditionExists)
		}
		return fail(errdoc.PreconditionNoneMatch)
	}
	return nil
}

// version is the stored version of the document being written (0 when
// absent); element tags before the write carry it.
func (w *WriteCtx) version() int64 {
	if w.Doc == nil {
		return 0
	}
	return w.Doc.Version
}

// writeHeaders sets the headers every successful write response carries
// (live 2026-09-28): the resulting stored version's modification time.
func writeHeaders(h http.Header, d *store.Document) {
	if d != nil {
		h.Set("Last-Modified", time.UnixMilli(d.ModifiedMS).UTC().Format(http.TimeFormat))
	}
}

// eventTag is the etag property of a mutation event: the document's new
// stored tag, unquoted (live 2026-09-28: selector PUT, POST, DELETE and
// whole-document PUT events all carry it; superseding R-SSE-13).
func eventTag(d *store.Document) string {
	if d == nil {
		return ""
	}
	return strings.Trim(d.ETag, `"`)
}

// event records a mutation event in the transaction. PageLove streams only
// the requests clients make on the application plane: authoring-plane
// writes (live 2026-09-28) and side-effect writes of triggers and
// processors (live 2026-09-29) emit none, superseding R-SSE-22/23.
func (e *Engine) event(w *WriteCtx, path string, m sse.Mutation, targets ...*html.Node) error {
	op := w.Op
	if op.Plane == Authoring || op.Quiet || (w.sideEffect && !w.cascade) {
		return nil
	}
	if w.Via != "" {
		// A write through an included or stamped element is announced on
		// the page it was addressed to, not on the origin document (live
		// 2026-09-28, decisions.md sse.scope.write-through-event-on-origin).
		path = w.Via
		if w.ViaSelector != "" && m.Selector != "" {
			m.Selector = w.ViaSelector
		}
	}
	m.Path = path
	m.Host = e.canonicalHost(w)
	session, conn := "", ""
	if op.Principal != nil {
		session = op.Principal.Session
	}
	conn = strings.TrimSpace(op.Conn)
	return w.Tx.AddEvent(store.Event{Path: path, Name: "mutation", Data: m.Render(), OriginSession: session, OriginConn: conn, Targets: targets})
}

// canonicalHost is the site's primary host name, reported identically to
// every subscriber whatever alias or plane the write came through
// (docs/spec/sse.md R-SSE-12).
func (e *Engine) canonicalHost(w *WriteCtx) string {
	if e.Domain != "" {
		return strings.ToLower(w.Site.Name + "." + strings.TrimPrefix(e.Domain, "."))
	}
	h := strings.ToLower(w.Op.Host)
	if i := strings.LastIndexByte(h, ':'); i >= 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	return strings.TrimPrefix(h, "dav-")
}

// put stores a document and, for authoring-plane writes, its authored
// baseline (the state a fork copies).
func (e *Engine) put(w *WriteCtx, nd *store.Document) (*store.Document, error) {
	stored, err := w.Tx.Put(nd)
	if err != nil {
		return nil, err
	}
	if w.Op.Plane == Authoring {
		if err := w.Tx.PutAuthored(stored); err != nil {
			return nil, err
		}
	}
	return stored, nil
}

// markupType is the Content-Type of fragments cut from a document: exactly
// text/html for HTML (as observed live), the stored type for XML.
func markupType(ct string) string {
	if site.IsXML(ct) {
		return ct
	}
	return "text/html"
}

// sameNode finds, in clone, the node corresponding to n in orig (by path).
func sameNode(orig, clone, n *html.Node) *html.Node {
	var idx []int
	for x := n; x != nil && x != orig; x = x.Parent {
		i := 0
		for c := x.Parent.FirstChild; c != x; c = c.NextSibling {
			i++
		}
		idx = append(idx, i)
	}
	cur := clone
	for k := len(idx) - 1; k >= 0; k-- {
		c := cur.FirstChild
		for i := 0; i < idx[k]; i++ {
			c = c.NextSibling
		}
		cur = c
	}
	return cur
}

func firstElement(nodes []*html.Node) *html.Node {
	for _, n := range nodes {
		if n.Type == html.ElementNode {
			return n
		}
	}
	return nil
}

func renderNodes(nodes []*html.Node) string {
	var b strings.Builder
	for _, n := range nodes {
		b.WriteString(dom.OuterHTML(n))
	}
	return b.String()
}
