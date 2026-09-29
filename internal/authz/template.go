package authz

import (
	"fmt"
	"strconv"
	"strings"
)

// Templated rule values (PageLove docs, AuthorizationRule §Templated
// values; spec R-PERM-30..36).
//
// The actor, resource and selector fields may contain ${path} lookups that
// are filled in per request before matching. ":username" anywhere in those
// fields is the legacy shorthand for ${request.auth.username}. Liquid
// ({{ … }}) is not evaluated: it is literal text.

// isTemplated reports whether a field value contains per-request lookups.
func isTemplated(s string) bool {
	return strings.Contains(s, "${") || hasUsernameToken(s)
}

// hasUsernameToken reports whether s contains the legacy ":username" token
// (not followed by a name character, so ":usernames" is literal text).
func hasUsernameToken(s string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], ":username")
		if j < 0 {
			return false
		}
		end := i + j + len(":username")
		if end == len(s) || !isNameByte(s[end]) {
			return true
		}
		i = end
	}
}

func isNameByte(c byte) bool {
	return c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// expandUsername rewrites the legacy ":username" token to its ${…} form.
func expandUsername(s string) string {
	if !hasUsernameToken(s) {
		return s
	}
	var b strings.Builder
	for {
		j := strings.Index(s, ":username")
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := j + len(":username")
		if end < len(s) && isNameByte(s[end]) {
			b.WriteString(s[:end])
			s = s[end:]
			continue
		}
		b.WriteString(s[:j])
		b.WriteString("${request.auth.username}")
		s = s[end:]
	}
}

// substitute fills every ${path} lookup in s, passing each rendered value
// through quote (identity for actor and selector fields, glob escaping for
// resource patterns). An unterminated "${" and a lookup whose path is not a
// dotted name are left as literal text.
func substitute(s string, req *Request, quote func(string) string) string {
	s = expandUsername(s)
	if !strings.Contains(s, "${") {
		return s
	}
	var b strings.Builder
	for {
		i := strings.Index(s, "${")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		j := strings.IndexByte(s[i+2:], '}')
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		path := s[i+2 : i+2+j]
		b.WriteString(s[:i])
		if validLookupPath(path) {
			b.WriteString(quote(Lookup(path, req)))
		} else {
			b.WriteString(s[i : i+2+j+1])
		}
		s = s[i+2+j+1:]
	}
}

func identityQuote(s string) string { return s }

// validLookupPath reports whether p is a dotted lookup path: segments of
// letters, digits, '_' and '-'.
func validLookupPath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	for _, seg := range strings.Split(p, ".") {
		if seg == "" {
			return false
		}
		for i := 0; i < len(seg); i++ {
			if !isNameByte(seg[i]) {
				return false
			}
		}
	}
	return true
}

// Lookup resolves a ${path} expression against the request. Only strings,
// numbers and booleans render; a path that reaches a list or an object, or
// nothing at all, renders as the empty string (R-PERM-32). request.auth.*
// is empty for anonymous requests.
//
// Supported (R-PERM-31, plus the Sessel-doc spellings of §18 C10):
//
//	request.method, request.path, request.query_string, request.query.<k>,
//	request.headers.<name>, request.auth.{sub,username,email,name},
//	request.auth.claims.<c>, username, auth.claims.<c>, method, path, query.<k>
func Lookup(expr string, req *Request) string {
	parts := strings.Split(strings.TrimSpace(expr), ".")
	// Bare spellings: aliases of the request.* forms.
	switch parts[0] {
	case "username":
		if len(parts) != 1 {
			return ""
		}
		parts = []string{"request", "auth", "username"}
	case "auth", "method", "path", "query", "query_string", "headers":
		parts = append([]string{"request"}, parts...)
	}
	if len(parts) < 2 || parts[0] != "request" {
		return ""
	}
	switch parts[1] {
	case "method":
		if len(parts) == 2 {
			return strings.ToUpper(req.httpMethod())
		}
	case "path":
		if len(parts) == 2 {
			return req.Path
		}
	case "query_string":
		if len(parts) == 2 {
			if req.RawQuery != "" {
				return req.RawQuery
			}
			if len(req.Query) > 0 {
				return req.Query.Encode()
			}
		}
	case "query":
		if len(parts) == 3 && req.Query != nil {
			if vs := req.Query[parts[2]]; len(vs) > 0 {
				return vs[0]
			}
		}
	case "headers":
		if len(parts) == 3 {
			var vals []string
			for k, v := range req.Header {
				if strings.EqualFold(k, parts[2]) {
					vals = append(vals, v...)
				}
			}
			return strings.Join(vals, ", ")
		}
	case "auth":
		pr := req.Principal
		if pr == nil || !pr.Authenticated || len(parts) < 3 {
			return ""
		}
		switch parts[2] {
		case "sub", "username":
			if len(parts) == 3 {
				return pr.Sub
			}
		case "email":
			if len(parts) == 3 {
				return pr.Email
			}
		case "name":
			if len(parts) == 3 {
				return pr.Name
			}
		case "claims":
			if len(parts) >= 4 {
				return scalar(claimPath(pr.Claims, parts[3:]))
			}
		}
	}
	return ""
}

// claimPath walks nested claim objects.
func claimPath(m map[string]any, path []string) any {
	var cur any = m
	for _, seg := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = obj[seg]
	}
	return cur
}

// scalar renders strings, numbers and booleans; anything else is "".
func scalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int, int64, int32, uint, uint64:
		return fmt.Sprint(x)
	}
	return ""
}
