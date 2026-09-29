package compose

import (
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/liquid"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

// Request is the request a document is composed for: the source of the
// `request` object (R-COMP-24) and of the Request Document (R-COMP-120).
type Request struct {
	Method   string
	Path     string // decoded request path without the query (the concrete URL on routes)
	RawQuery string
	Query    url.Values
	Header   http.Header
	Host     string
	// Principal is the requester (nil or unauthenticated: anonymous). Its
	// Session keys transient content.
	Principal *identity.Principal
	// Params are the route captures in route-template order.
	Params      []Param
	ContentType string
	Body        []byte
}

// Param is one route capture.
type Param struct{ Name, Value string }

func (r *Request) paramMap() map[string]string {
	m := make(map[string]string, len(r.Params))
	for _, p := range r.Params {
		m[p.Name] = p.Value
	}
	return m
}

func (r *Request) authenticated() bool { return r.Principal != nil && r.Principal.Authenticated }

// claims returns every identity claim, with sub/email/name/picture filled
// in from the principal (R-COMP-24).
func (r *Request) claims() map[string]any {
	p := r.Principal
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
	if p.Picture != "" {
		set("picture", p.Picture)
	}
	return claims
}

// roles is the principal's role list as rules see it: verified email,
// group names, OIDC roles and "users" (R-PERM-74).
func (r *Request) roles(snap *site.Snapshot) []string {
	if !r.authenticated() {
		return nil
	}
	if snap != nil && snap.Policy != nil {
		return snap.Policy.RoleList(r.Principal)
	}
	return r.Principal.Roles
}

// liquidRequest builds Liquid's `request` (one per composition, so a read
// of an identity member anywhere marks the response private).
func (r *Request) liquidRequest(snap *site.Snapshot) *liquid.Request {
	params := make([]liquid.Param, len(r.Params))
	for i, p := range r.Params {
		params[i] = liquid.Param{Name: p.Name, Value: p.Value}
	}
	var auth *liquid.Auth
	if r.authenticated() {
		auth = &liquid.Auth{Username: r.Principal.Sub, Claims: r.claims(), Roles: r.roles(snap)}
	}
	return liquid.NewRequest(liquid.RequestData{Method: r.Method, Path: r.Path, RawQuery: r.RawQuery, Header: r.Header,
		Host: r.Host, Params: params, ContentType: r.ContentType, Body: r.Body, Auth: auth})
}

// sesselRequest builds Sessel's `request` (R-SESSEL-291); a form body is a
// dictionary of its fields (R-COMP-24), and the role list is available
// under both roles and role (C-7).
func (r *Request) sesselRequest(snap *site.Snapshot) *sessel.Dict {
	var auth *sessel.Dict
	if r.authenticated() {
		roles := r.roles(snap)
		auth = sessel.NewAuth(r.Principal.Sub, r.claims(), roles)
		rl := make(sessel.List, len(roles))
		for i, x := range roles {
			rl[i] = x
		}
		auth.Set("role", rl)
	} else {
		auth = sessel.NewAuth("", nil, nil)
		auth.Set("role", sessel.List{})
	}
	req := sessel.NewRequest(r.Method, r.Path, r.Header, r.Query, r.paramMap(), r.Body, auth)
	if fields, ok := formFields(r.ContentType, r.Body); ok {
		d := sessel.NewDict()
		for _, kv := range fields {
			d.Set(kv[0], kv[1]) // repeated fields: the last value wins (Q-6)
		}
		req.Set("body", d)
	}
	return req
}

// formFields decodes an application/x-www-form-urlencoded body.
func formFields(ct string, body []byte) ([][2]string, bool) {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil || mt != "application/x-www-form-urlencoded" {
		return nil, false
	}
	var out [][2]string
	for _, part := range strings.Split(string(body), "&") {
		if part == "" {
			continue
		}
		k, v, _ := strings.Cut(part, "=")
		kk, err1 := url.QueryUnescape(k)
		vv, err2 := url.QueryUnescape(v)
		if err1 != nil {
			kk = k
		}
		if err2 != nil {
			vv = v
		}
		out = append(out, [2]string{kk, vv})
	}
	return out, true
}

// identityRE finds reads of per-requester request members in Sessel or
// method source: request.auth / request.headers, Context.request…, or the
// bracket forms. Naming request alone does not taint (R-COMP-43).
var identityRE = regexp.MustCompile(`\b(?:auth|headers)\b`)

// readsIdentity reports whether Sessel source may read request.auth or
// request.headers (the response is then Cache-Control: private,
// R-COMP-150). The check is static and conservative: it looks for the
// member names next to a use of request or Context.
func readsIdentity(src string) bool {
	if !strings.Contains(src, "request") && !strings.Contains(src, "Context") {
		return false
	}
	return identityRE.MatchString(src)
}

// ReadsIdentity is readsIdentity for method dispatchers installed with
// SetMethodDispatcher: a Sessel implementation that may read a per-requester
// request member makes the page private.
func ReadsIdentity(src string) bool { return readsIdentity(src) }
