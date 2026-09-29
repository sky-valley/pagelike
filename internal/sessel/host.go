package sessel

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Host is everything an evaluation needs from its environment. It is
// implemented over a site snapshot by the glue packages (internal/query for
// QUERY; compose, schema and reactions add their own capabilities), and by
// MemHost for tests. Sessel itself never imports site, store or engine.
type Host interface {
	// Documents returns the stored markup documents named by a from-source
	// string: an exact path or a glob (R-SESSEL-205/206). The empty pattern
	// means every document of the site (the default selector scope,
	// R-SESSEL-204). Results are in ascending path order; missing documents
	// and non-markup resources are omitted.
	Documents(ctx context.Context, pattern string) ([]*Document, error)
	// Resource is Pagelove.GET(path): the root element of a markup document
	// (its Document carrying Meta), a *Blob for another resource, or nil
	// when nothing is stored at path (R-SESSEL-245/246).
	Resource(ctx context.Context, path string) (Value, error)
	// Class resolves a type URL to its schema class. It returns (nil, nil)
	// when no schema declares url; Sessel then uses a BasicClass.
	Class(ctx context.Context, url string) (Class, error)
	// Writer is the write provider of the Pagelove platform interface
	// (R-SESSEL-295). Nil refuses Pagelove.PUT/DELETE with a RuntimeError.
	Writer() Writer
	// Now is the clock read by Temporal.Now.
	Now() time.Time
}

// Writer performs the platform interface's writes through the host's
// write pipeline.
type Writer interface {
	// Put stores item as the document at path (or, inside a trigger, replaces
	// the in-flight request body when path is the request path).
	Put(ctx context.Context, item *Element, path string) error
	// Delete deletes the document at a path (String) or an element.
	Delete(ctx context.Context, target Value) error
}

// Env is the evaluation context of one program run (R-SESSEL-1): the
// context bindings, the shared per-request Context object and the budget.
type Env struct {
	Host Host
	// Self is the value of `self`; it is bound only when HasSelf is set
	// (e.g. a QUERY on a directory leaves it unbound).
	Self    Value
	HasSelf bool
	// Prior is the pre-mutation document root in write contexts (nil → null).
	Prior *Element
	// Document overrides `document` (default: self when it is a document
	// root, else the root of self's document).
	Document *Element
	// Context is the shared per-request Context object (R-SESSEL-290); a
	// fresh one is created when nil. Its "request" entry defaults to Request.
	Context *Dict
	// Request is bound as `request` (composition, QUERY). Build it with
	// NewRequest.
	Request *Dict
	// Vars are further host context names: earlier binding names,
	// messageName, parameters, method parameters, authorization-rule names.
	Vars map[string]Value
	// Budget is shared by every evaluation of one request (NewBudget).
	Budget *Budget
	// Mutable marks the element(s) in Self as mutable working copies
	// (resolver pipelines, R-SESSEL-242). The host must pass detached
	// copies (e.g. dom.Clone of the stored nodes): stored snapshots are
	// shared and must never be mutated.
	Mutable bool
	// Location is the "system timezone" of Temporal.Now (default UTC).
	Location *time.Location
	// DocumentOnly confines the program to the Self document, as a
	// request-supplied QUERY program is on live PageLove (2026-09-29):
	// bare selectors search only that document, `from self` is the only
	// from-source that reaches it (paths, globs, elements, `document` and
	// Selector.execute(path) match nothing), elements carry no provenance
	// (path(), document() and the microdata @id are null) and Pagelove.GET
	// is not available.
	DocumentOnly bool
}

// Budget is the per-request evaluation budget shared by every Sessel (and
// server JavaScript) evaluation of one request (R-SESSEL-356/357). It is not
// safe for concurrent use.
type Budget struct {
	MaxOps   int64
	MaxMem   int64
	MaxDepth int
	Deadline time.Time

	Ops, Mem  int64
	exhausted *Error
}

// Default limits (R-SESSEL-357).
const (
	DefaultMaxOps     = 2_000_000
	DefaultMaxMem     = 64 << 20
	DefaultMaxDepth   = 256
	DefaultTimeout    = 2 * time.Second
	MaxProgramBytes   = 256 << 10
	MaxSelectorResult = 100_000
)

// NewBudget returns a budget with the default limits, its clock starting now.
func NewBudget() *Budget {
	return &Budget{MaxOps: DefaultMaxOps, MaxMem: DefaultMaxMem, MaxDepth: DefaultMaxDepth, Deadline: time.Now().Add(DefaultTimeout)}
}

// Exhausted reports the error that exhausted the budget, if any.
func (b *Budget) Exhausted() *Error { return b.exhausted }

// NewRequest builds the request object of R-SESSEL-291: method, path,
// headers (lower-case keys, case-insensitive lookup), query (first values),
// params, body and rawBody, and auth ({claims, username, roles}; claims and
// username are null for anonymous requests).
func NewRequest(method, path string, header http.Header, query url.Values, params map[string]string, body []byte, auth *Dict) *Dict {
	r := NewDict()
	r.Set("method", strings.ToUpper(method))
	r.Set("path", path)
	h := NewFoldDict()
	names := make([]string, 0, len(header))
	for k := range header {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		sep := ", "
		if strings.EqualFold(k, "Cookie") {
			sep = "; "
		}
		h.Set(k, strings.Join(header[k], sep))
	}
	r.Set("headers", h)
	q := NewDict()
	qk := make([]string, 0, len(query))
	for k := range query {
		qk = append(qk, k)
	}
	sort.Strings(qk)
	for _, k := range qk {
		if vs := query[k]; len(vs) > 0 {
			q.Set(k, vs[0])
		}
	}
	r.Set("query", q)
	p := NewDict()
	pk := make([]string, 0, len(params))
	for k := range params {
		pk = append(pk, k)
	}
	sort.Strings(pk)
	for _, k := range pk {
		p.Set(k, params[k])
	}
	r.Set("params", p)
	r.Set("body", string(body))
	r.Set("rawBody", string(body))
	if auth == nil {
		auth = NewAuth("", nil, nil)
	}
	r.Set("auth", auth)
	return r
}

// NewAuth builds request.auth: {claims, username, roles}. An empty username
// means anonymous (claims.* and username read as null).
func NewAuth(username string, claims map[string]any, roles []string) *Dict {
	a := NewDict()
	c := NewDict()
	keys := make([]string, 0, len(claims))
	for k := range claims {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c.Set(k, FromGo(claims[k]))
	}
	a.Set("claims", c)
	if username == "" {
		a.Set("username", nil)
	} else {
		a.Set("username", username)
	}
	rl := make(List, 0, len(roles))
	for _, r := range roles {
		rl = append(rl, r)
	}
	a.Set("roles", rl)
	return a
}

// NewContext builds a Context object whose request is req (may be nil).
func NewContext(req *Dict) *Dict {
	c := NewDict()
	if req != nil {
		c.Set("request", req)
	}
	return c
}

// SetResponse installs Context.response (processors): status, body and
// headers (lower-case keys).
func SetResponse(ctx *Dict, status int, body string, header http.Header) {
	r := NewDict()
	r.Set("status", int64(status))
	r.Set("body", body)
	h := NewFoldDict()
	names := make([]string, 0, len(header))
	for k := range header {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		h.Set(k, strings.Join(header[k], ", "))
	}
	r.Set("headers", h)
	ctx.Set("response", r)
}

// ResponseFields reads Context.response back (status, body; ok false when
// absent or malformed).
func ResponseFields(ctx *Dict) (status int, body string, ok bool) {
	r, _ := ctx.Lookup("response").(*Dict)
	if r == nil {
		return 0, "", false
	}
	switch s := r.Lookup("status").(type) {
	case int64:
		status = int(s)
	case float64:
		status = int(s)
	default:
		return 0, "", false
	}
	b, _ := r.Lookup("body").(string)
	return status, b, true
}
