package liquid

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/osteele/liquid/values"
)

// Request is the `request` object (R-LIQ-60 … R-LIQ-65, R-COMP-24). It is a
// read-only hash with the members method, path, query, headers, params,
// body and auth, and it records whether a template read anything that makes
// the response private: a member of `request.auth` or `request.headers`, or
// the whole object serialized or iterated (R-LIQ-64). Composition shares one
// Request between every template of a request and checks Private afterwards
// to decide on `Cache-Control: private`.
type Request struct {
	h       *Hash
	private atomic.Bool
}

// Param is one parameterized-route capture.
type Param struct {
	Name, Value string
}

// Auth is the authenticated identity; nil means anonymous.
type Auth struct {
	// Username is the OIDC subject (`sub`).
	Username string
	// Claims holds every claim of the identity (email, name, picture, …).
	Claims map[string]any
	// Roles is the role list, exposed as both `roles` and `role` (C-6):
	// verified email, group names, OIDC roles and `users` (R-PERM-74).
	Roles []string
}

// RequestData describes the request a template renders for.
type RequestData struct {
	// Method is the request method; it is upper-cased. A resource-creation
	// POST renders with POST.
	Method string
	// Path is the percent-decoded request path without the query (on a
	// parameterized route, the concrete requested path).
	Path string
	// RawQuery is the query string as received, without '?'.
	RawQuery string
	// Header holds the request headers. Names are lower-cased and repeated
	// headers joined with ", ".
	Header http.Header
	// Host becomes the `host` header when Header has none (net/http moves
	// it out of the header map).
	Host string
	// Params are the route captures, decoded, in route-template order.
	Params []Param
	// ContentType and Body are the request body. A form body
	// (application/x-www-form-urlencoded, or the text fields of
	// multipart/form-data) becomes a hash of fields; any other body is the
	// raw string, "" when there is none.
	ContentType string
	Body        []byte
	// Auth is the authenticated identity, nil for anonymous requests.
	Auth *Auth
}

// NewRequest builds the `request` value.
func NewRequest(d RequestData) *Request {
	h := NewHash()
	h.Set("method", strings.ToUpper(d.Method))
	h.Set("path", d.Path)
	h.Set("query", parseQuery(d.RawQuery))
	h.Set("headers", headerHash(d.Header, d.Host))
	params := NewHash()
	for _, p := range d.Params {
		params.Set(p.Name, p.Value)
	}
	h.Set("params", params)
	h.Set("body", bodyValue(d.ContentType, d.Body))
	h.Set("auth", authHash(d.Auth))
	return &Request{h: h}
}

// RequestFromHTTP builds the `request` value from an HTTP request. The body
// must be read by the caller (it may already have been consumed); params are
// the route captures and auth the identity (nil for anonymous).
func RequestFromHTTP(r *http.Request, body []byte, params []Param, auth *Auth) *Request {
	return NewRequest(RequestData{
		Method:      r.Method,
		Path:        r.URL.Path, // already percent-decoded
		RawQuery:    r.URL.RawQuery,
		Header:      r.Header,
		Host:        r.Host,
		Params:      params,
		ContentType: r.Header.Get("Content-Type"),
		Body:        body,
		Auth:        auth,
	})
}

// Private reports whether a template read `request.auth`, `request.headers`
// or the whole request (R-LIQ-64).
func (r *Request) Private() bool { return r.private.Load() }

// get reads a member, recording private reads.
func (r *Request) get(key string) (any, bool) {
	if key == "auth" || key == "headers" {
		r.private.Store(true)
	}
	return r.h.Get(key)
}

// whole returns the underlying hash for serialization and iteration, which
// read every member.
func (r *Request) whole() *Hash {
	r.private.Store(true)
	return r.h
}

// Interface implements values.Value.
func (r *Request) Interface() any { return r }

// Int implements values.Value.
func (r *Request) Int() int { panic(values.TypeError("can't convert request to int")) }

// Equal implements values.Value.
func (r *Request) Equal(o values.Value) bool { return o.Interface() == r }

// Less implements values.Value.
func (r *Request) Less(values.Value) bool { return false }

// Contains implements values.Value.
func (r *Request) Contains(values.Value) bool { return false }

// IndexValue implements values.Value: `request['path']`.
func (r *Request) IndexValue(k values.Value) values.Value { return r.PropertyValue(k) }

// PropertyValue implements values.Value: `request.path`.
func (r *Request) PropertyValue(k values.Value) values.Value {
	key, ok := k.Interface().(string)
	if !ok {
		return undefinedValue
	}
	if v, found := r.get(key); found {
		return values.ValueOf(v)
	}
	if key == "size" {
		return values.ValueOf(r.h.Len())
	}
	return undefinedValue
}

// Test implements values.Value.
func (r *Request) Test() bool { return true }

// parseQuery decodes a query or form body into a hash in first-seen order;
// a repeated name becomes an array of strings (R-LIQ-60).
func parseQuery(raw string) *Hash {
	h := NewHash()
	for raw != "" {
		var part string
		part, raw, _ = strings.Cut(raw, "&")
		if part == "" {
			continue
		}
		name, value, _ := strings.Cut(part, "=")
		addField(h, unescapeForm(name), unescapeForm(value))
	}
	return h
}

func addField(h *Hash, name, value string) {
	switch prev, ok := h.Get(name); {
	case !ok:
		h.Set(name, value)
	default:
		if arr, isArr := prev.([]any); isArr {
			h.Set(name, append(arr, value))
		} else {
			h.Set(name, []any{prev, value})
		}
	}
}

// unescapeForm decodes form encoding; a malformed escape is kept as written.
func unescapeForm(s string) string {
	if u, err := url.QueryUnescape(s); err == nil {
		return u
	}
	return urlDecode(s)
}

func headerHash(hdr http.Header, host string) *Hash {
	names := make([]string, 0, len(hdr)+1)
	lower := map[string][]string{}
	for k, vs := range hdr {
		lk := strings.ToLower(k)
		if _, seen := lower[lk]; !seen {
			names = append(names, lk)
		}
		lower[lk] = append(lower[lk], vs...)
	}
	if _, ok := lower["host"]; !ok && host != "" {
		names = append(names, "host")
		lower["host"] = []string{host}
	}
	sort.Strings(names)
	h := NewHash()
	for _, k := range names {
		h.Set(k, strings.Join(lower[k], ", "))
	}
	return h
}

func bodyValue(contentType string, body []byte) any {
	mt, params, _ := mime.ParseMediaType(contentType)
	switch mt {
	case "application/x-www-form-urlencoded":
		return parseQuery(string(body))
	case "multipart/form-data":
		if h, ok := multipartFields(body, params["boundary"]); ok {
			return h
		}
	}
	return string(body)
}

// multipartFields returns the text fields of a multipart/form-data body;
// file parts are skipped.
func multipartFields(body []byte, boundary string) (*Hash, bool) {
	if boundary == "" {
		return nil, false
	}
	h := NewHash()
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			return h, true
		}
		if err != nil {
			return nil, false
		}
		if p.FileName() != "" || p.FormName() == "" {
			continue
		}
		v, err := io.ReadAll(p)
		if err != nil {
			return nil, false
		}
		addField(h, p.FormName(), string(v))
	}
}

// authHash is `request.auth` (R-LIQ-61): anonymous requests get a nil
// username, empty claims and empty role lists, so every member is empty and
// reading one is never an error.
func authHash(a *Auth) *Hash {
	h := NewHash()
	roles := []any{}
	if a == nil {
		h.Set("username", nil)
		h.Set("claims", NewHash())
	} else {
		h.Set("username", a.Username)
		h.Set("claims", HashOf(a.Claims))
		for _, r := range a.Roles {
			roles = append(roles, r)
		}
	}
	h.Set("roles", roles)
	h.Set("role", roles)
	return h
}
