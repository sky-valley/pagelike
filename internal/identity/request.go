package identity

import (
	"html"
	"sort"
	"strconv"
	"strings"
)

// Identity in composition (spec R-PERM-74, R-PERM-75). The composition
// layer builds the transient request document and the `request` variable of
// Liquid, Sessel and server JavaScript from these helpers; roles is the
// principal's role list from authz.Policy.RoleList (verified email, group
// names, OIDC roles, "users").

// AuthVars returns the value of request.auth for templates and scripts:
// username (= sub), sub, email, email_verified, name, picture, claims, and
// the role list under both "role" and "roles" (§18 C9). Anonymous requests
// get an empty map, so every lookup is empty rather than an error.
func AuthVars(p *Principal, roles []string) map[string]any {
	if p == nil || !p.Authenticated {
		return map[string]any{}
	}
	rl := make([]any, len(roles))
	for i, r := range roles {
		rl[i] = r
	}
	claims := make(map[string]any, len(p.Claims))
	for k, v := range p.Claims {
		claims[k] = v
	}
	return map[string]any{
		"username": p.Sub, "sub": p.Sub, "email": p.Email, "email_verified": p.EmailVerified,
		"name": p.Name, "picture": p.Picture, "claims": claims, "role": rl, "roles": rl,
	}
}

// AuthSection renders the request document's auth section:
//
//	<section itemprop="auth" itemscope itemtype="https://pagelove.org/Authorization">
//	  <section itemprop="claims" itemscope itemtype="https://pagelove.org/Claims">
//	    <meta itemprop="{claim}" content="{value}">   one per scalar claim, sorted
//	  </section>
//	  <meta itemprop="username" content="{sub}">
//	  <meta itemprop="role" content="{role}">         one per role-list entry
//	</section>
//
// For anonymous requests the section is present and empty.
func AuthSection(p *Principal, roles []string) string {
	var b strings.Builder
	b.WriteString(`<section itemprop="auth" itemscope itemtype="https://pagelove.org/Authorization">`)
	if p != nil && p.Authenticated {
		b.WriteString("\n  <section itemprop=\"claims\" itemscope itemtype=\"https://pagelove.org/Claims\">\n")
		keys := make([]string, 0, len(p.Claims))
		for k := range p.Claims {
			if k != "" && !strings.ContainsAny(k, " \t\n\r\f") {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			if v, ok := scalarClaim(p.Claims[k]); ok {
				b.WriteString(`    <meta itemprop="` + html.EscapeString(k) + `" content="` + html.EscapeString(v) + "\">\n")
			}
		}
		b.WriteString("  </section>\n")
		b.WriteString(`  <meta itemprop="username" content="` + html.EscapeString(p.Sub) + "\">\n")
		for _, r := range roles {
			b.WriteString(`  <meta itemprop="role" content="` + html.EscapeString(r) + "\">\n")
		}
	}
	b.WriteString("</section>")
	return b.String()
}

func scalarClaim(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case int:
		return strconv.Itoa(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	}
	return "", false
}
