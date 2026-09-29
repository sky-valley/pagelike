package authz

import (
	"strings"

	"github.com/sky-valley/pagelike/internal/identity"
)

// Actor matching (spec R-PERM-9..18, R-PERM-58..62).

// Tiers of actor specificity; higher wins, 0 means "does not match".
const (
	tierNone     = 0
	tierWildcard = 1 // *
	tierMember   = 2 // users/authenticated, verified email, group, OIDC role, role:X
	tierUser     = 3 // exact user name (sub) and the deprecated :username forms
)

// Built-in groups every authenticated principal belongs to.
const (
	BuiltinUsers         = "users"
	BuiltinAuthenticated = "authenticated"
)

// subject is the per-decision view of the requester: its user name and its
// membership tokens, recomputed for every request (R-PERM-62).
type subject struct {
	authed bool
	sub    string
	email  string          // verified email, or ""
	tokens map[string]bool // verified email, group names, OIDC roles (no built-ins)
	groups []string        // names of Group items that include the principal, policy order
	roles  []string        // OIDC (or local account) roles, claim order
}

func (p *Policy) subjectOf(pr *identity.Principal) *subject {
	s := &subject{tokens: map[string]bool{}}
	if pr == nil || !pr.Authenticated {
		return s
	}
	s.authed, s.sub = true, pr.Sub
	if pr.EmailVerified && pr.Email != "" {
		s.email = pr.Email
		s.tokens[pr.Email] = true
		seen := map[string]bool{}
		for i := range p.Groups {
			g := &p.Groups[i]
			if g.Name == "" || seen[g.Name] || !g.includes(pr.Email) {
				continue
			}
			seen[g.Name] = true
			s.groups = append(s.groups, g.Name)
			s.tokens[g.Name] = true
		}
	}
	for _, r := range pr.Roles {
		if r != "" {
			s.roles = append(s.roles, r)
			s.tokens[r] = true
		}
	}
	return s
}

// includes applies the group's membership test to a verified email: the
// subtype's includes() method when one is bound, otherwise exact equality
// with a member value (R-PERM-58, R-PERM-60). A panicking includes() is
// treated as "not a member" (fail closed).
func (g *Group) includes(email string) (ok bool) {
	if g.Includes != nil {
		defer func() {
			if recover() != nil {
				ok = false
			}
		}()
		return g.Includes(email)
	}
	for _, m := range g.Members {
		if m == email {
			return true
		}
	}
	return false
}

// roleList is the principal's role list as composition sees it
// (request.auth.role/roles, the request document's role metas): the
// verified email, group names (Group items, then OIDC roles, without
// duplicates) and "users" (R-PERM-74).
func (s *subject) roleList() []string {
	if !s.authed {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(v string) {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	add(s.email)
	for _, g := range s.groups {
		add(g)
	}
	for _, r := range s.roles {
		add(r)
	}
	add(BuiltinUsers)
	return out
}

func (s *subject) hasRole(name string) bool {
	for _, r := range s.roleList() {
		if r == name {
			return true
		}
	}
	return false
}

// isDeprecatedUsername reports whether an actor value is one of the
// deprecated "the requester's own user name" forms (R-PERM-17).
func isDeprecatedUsername(v string) bool {
	switch v {
	case ":username", "${username}", "${request.auth.username}":
		return true
	}
	return false
}

// actorTier returns the highest tier at which any of the rule's actor
// values matches the requester (R-PERM-11). Values containing lookups are
// substituted first and then compared as literal tokens only: text coming
// from the request never acts as "*", a built-in group or a role: form
// (R-PERM-34).
func actorTier(values []string, req *Request, s *subject) int {
	best := tierNone
	for _, v := range values {
		t := tierNone
		switch {
		case v == "*":
			t = tierWildcard
		case !s.authed:
			// Every other form needs a signed-in principal.
		case isDeprecatedUsername(v):
			t = tierUser
		case isTemplated(v):
			lit := trimASCII(substitute(v, req, identityQuote))
			switch {
			case lit == "":
			case lit == s.sub:
				t = tierUser
			case s.tokens[lit]:
				t = tierMember
			}
		case v == s.sub:
			t = tierUser
		case v == BuiltinUsers || v == BuiltinAuthenticated:
			t = tierMember
		case strings.HasPrefix(v, "role:"):
			if s.hasRole(strings.TrimPrefix(v, "role:")) {
				t = tierMember
			}
		case s.tokens[v]:
			t = tierMember
		}
		if t > best {
			best = t
		}
	}
	return best
}
