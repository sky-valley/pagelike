// Package identity resolves who is making a request: end-user principals
// (local accounts or an OpenID Connect provider per site), their sessions,
// and the request-document view of identity.
//
// Authoring credentials (API keys) and end-user identity are deliberately
// separate: keys authenticate the authoring and control planes only and are
// never accepted as an end-user identity on the public plane (R-PERM-71).
package identity

import (
	"strings"
)

// Principal is the end-user identity attached to a public-plane request
// (spec R-PERM-9). Sub is the user name rules match at the top specificity
// tier; Email counts for email and Group membership only when
// EmailVerified.
type Principal struct {
	Authenticated bool
	Sub           string // OIDC subject (possibly mapped through a Link) / local user name
	Username      string // always Sub (request.auth.username)
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
	Roles         []string       // OIDC roles claim, plus local account roles
	Claims        map[string]any // every identity claim as asserted (request.auth.claims.*)
	Issuer        string         // OpenID issuer; "" for local accounts
	Session       string         // session id (also set for anonymous visitors)
}

// Anonymous returns an unauthenticated principal bound to a session.
func Anonymous(session string) *Principal { return &Principal{Session: session} }

// DisplayName returns a human label.
func (p *Principal) DisplayName() string {
	switch {
	case p == nil || !p.Authenticated:
		return "anonymous"
	case p.Name != "":
		return p.Name
	case p.Email != "":
		return p.Email
	}
	return p.Sub
}

// HasRole reports whether the principal carries role r.
func (p *Principal) HasRole(r string) bool {
	for _, x := range p.Roles {
		if x == r {
			return true
		}
	}
	return false
}

// SplitRoles parses a stored comma/space separated role list.
func SplitRoles(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}
