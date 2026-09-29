// Package authz evaluates PageLove AuthorizationRule and Group items
// (docs/spec/permissions-identity.md; PageLove docs, Permissions, snapshot
// 2026-09-28).
//
// Model:
//   - every AuthorizationRule item stored anywhere on the host is a rule
//     (meta, table-row and table forms; multi-valued fields; malformed rules
//     ignored; MOVE rules with a selector normalized at extraction);
//   - a rule is a candidate for a request when its method, resource glob,
//     actor and (optional) selector all match; the selector is evaluated
//     against the element the request targets (the key element);
//   - among candidates only the most specific actor tier counts (exact user
//     name and the deprecated :username forms > users/authenticated,
//     verified email, group, OIDC role, role:X > *), and there a Deny wins;
//   - no candidate denies, except a plain GET/HEAD when the host's
//     default-GET mode is "allow" (never an SSE subscription, a write, MOVE
//     or a Sessel QUERY).
//
// Actor, resource and selector values may contain ${…} lookups filled in
// per request (template.go); Liquid {{ … }} is literal text.
//
// Directory URLs: a document path "/d/index.html" is also addressed as
// "/d/". Rules are matched against the canonical document path; a rule
// written for the directory spelling grants nothing there (live
// 2026-09-28, superseding R-PERM-19), but such a Deny still refuses it, so
// a refusal cannot be bypassed by switching spellings.
package authz

import (
	"net/url"
	"strings"
	"sync"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/selector"
)

// Itemtypes.
const (
	RuleType  = "https://pagelove.org/AuthorizationRule"
	GroupType = "https://pagelove.org/Group"
)

// Effect is a rule's action.
type Effect int

const (
	Allow Effect = iota + 1
	Deny
)

// Rule is one extracted AuthorizationRule. Field values are trimmed and
// non-empty; methods are upper-cased.
type Rule struct {
	Actors    []string
	Resources []string
	Methods   []string
	Selector  string // "" = the whole resource; several values joined with ", "
	Effect    Effect
	Source    string   // path of the document declaring the rule
	Warnings  []string // extraction notes for authors (e.g. literal Liquid)

	globs []*Glob // compiled Resources; nil entries for templated patterns
}

// compile precompiles the rule's untemplated resource patterns.
func (r *Rule) compile() {
	r.globs = make([]*Glob, len(r.Resources))
	for i, pat := range r.Resources {
		if !isTemplated(pat) {
			r.globs[i] = CompileGlob(pat)
		}
	}
}

// Group is a group definition: a platform Group item or an item of a schema
// subtype of Group.
type Group struct {
	Name    string
	Members []string // exact verified-email matches
	Source  string
	// Includes, when set, decides membership instead of Members: the schema
	// package binds a Group subtype's includes() method here. It is called
	// only with a verified email (R-PERM-59) and must return false on error
	// or exhausted budget (fail closed).
	Includes func(email string) bool
}

// Policy is a host's rule set at one write generation. It is immutable once
// published; build it with ExtractRules, then RunHooks.
type Policy struct {
	Rules      []Rule
	Groups     []Group
	DefaultGet bool // host default-GET mode grants unmatched GET/HEAD
	// SelectorOptions, when set by a hook, is the selector extension context
	// for rule selectors (the schema inheritance map :isa() consults).
	SelectorOptions *selector.ExtOptions

	selCache sync.Map // untemplated selector source → compiledSelector
}

// Request describes one authorization question.
type Request struct {
	Principal *identity.Principal
	// Method is the method being authorized (GET for reads and css QUERY,
	// DELETE/POST/MOVE for the three MOVE checks, …).
	Method string
	// Path is the canonical document path (a directory URL "/d/" is
	// "/d/index.html").
	Path   string
	Header map[string][]string
	Query  url.Values
	// RawQuery is the request's raw query string (${request.query_string});
	// when empty the encoded Query is used.
	RawQuery string
	// HTTPMethod is the request's own method when it differs from Method
	// (${request.method}); when empty Method is used.
	HTTPMethod string
	// Subscribe marks an SSE subscription (never granted by default-GET).
	Subscribe bool
}

func (r *Request) httpMethod() string {
	if r.HTTPMethod != "" {
		return r.HTTPMethod
	}
	return r.Method
}

// Decision is the outcome of an authorization check.
type Decision struct {
	Allowed bool
	Matched bool  // at least one rule matched (false: the default applied)
	Rule    *Rule // the deciding rule, when Matched
}

// Decide answers the request for the whole resource (target == nil) or for
// one key element (target != nil; an element of the document the request
// selector was resolved in). Resource-level rules are candidates in both
// cases; selector-scoped rules only when target matches their selector.
func (p *Policy) Decide(req Request, target *html.Node) Decision {
	return p.decide(&req, p.subjectOf(req.Principal), target)
}

// DecideAll answers an all-matches read (multipart/mixed, JSON-LD): every
// element is decided on its own and all must be allowed (R-PERM-41). The
// first refusal is returned. With no targets it is the resource-level
// decision (never a vacuous grant).
func (p *Policy) DecideAll(req Request, targets []*html.Node) Decision {
	s := p.subjectOf(req.Principal)
	if len(targets) == 0 {
		return p.decide(&req, s, nil)
	}
	var last Decision
	for _, t := range targets {
		d := p.decide(&req, s, t)
		if !d.Allowed {
			return d
		}
		last = d
	}
	return last
}

func (p *Policy) decide(req *Request, s *subject, target *html.Node) Decision {
	return p.resolve(req, s, func(r *Rule) bool {
		switch sc, _, sel := p.scopeOf(r, req); sc {
		case scopeResource:
			return true
		case scopeSelector:
			return target != nil && sel.Matches(target)
		}
		return false
	})
}

// DecideText answers the request as OPTIONS does, without looking at
// document content: resource-level rules apply, and selector-scoped rules
// apply when their (substituted, whitespace-normalized) selector text equals
// selText. selText "" considers resource-level rules only (R-PERM-63).
func (p *Policy) DecideText(req Request, selText string) Decision {
	want := normalizeSelectorText(selText)
	return p.resolve(&req, p.subjectOf(req.Principal), func(r *Rule) bool {
		switch sc, text, _ := p.scopeOf(r, &req); sc {
		case scopeResource:
			return true
		case scopeSelector:
			return want != "" && normalizeSelectorText(text) == want
		}
		return false
	})
}

// resolve runs the documented decision procedure (R-PERM-38) over the rules
// whose selector condition holds.
func (p *Policy) resolve(req *Request, s *subject, selectorOK func(*Rule) bool) Decision {
	method := strings.ToUpper(req.Method)
	best := tierNone
	var allow, deny *Rule
	for i := range p.Rules {
		r := &p.Rules[i]
		if !methodMatches(r.Methods, method) {
			continue
		}
		tier := actorTier(r.Actors, req, s)
		if tier == tierNone || tier < best {
			continue
		}
		if !p.resourceMatches(r, req) || !selectorOK(r) {
			continue
		}
		if tier > best {
			best, allow, deny = tier, nil, nil
		}
		if r.Effect == Deny {
			if deny == nil {
				deny = r
			}
		} else if allow == nil {
			allow = r
		}
	}
	switch {
	case deny != nil:
		return Decision{Allowed: false, Matched: true, Rule: deny}
	case allow != nil:
		return Decision{Allowed: true, Matched: true, Rule: allow}
	}
	return Decision{Allowed: p.defaultGrants(req, method)}
}

// defaultGrants is the fallback when no rule matched (R-PERM-44, 45).
func (p *Policy) defaultGrants(req *Request, method string) bool {
	return p.DefaultGet && !req.Subscribe && (method == "GET" || method == "HEAD")
}

// SelectorRules returns the distinct selector texts of the selector-scoped
// rules whose actor matches the requester and whose resource covers the
// path, in rule order (the parts of a multipart OPTIONS answer, R-PERM-63).
func (p *Policy) SelectorRules(req Request) []string {
	s := p.subjectOf(req.Principal)
	seen := map[string]bool{}
	var out []string
	for i := range p.Rules {
		r := &p.Rules[i]
		if r.Selector == "" || actorTier(r.Actors, &req, s) == tierNone || !p.resourceMatches(r, &req) {
			continue
		}
		sc, text, _ := p.scopeOf(r, &req)
		key := normalizeSelectorText(text)
		if sc != scopeSelector || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, text)
	}
	return out
}

// HasSelectorRules reports whether any selector-scoped rule whose actor
// matches the requester covers the request path, for any method. When none
// does, element-level decisions equal the resource-level one.
func (p *Policy) HasSelectorRules(req Request) bool {
	s := p.subjectOf(req.Principal)
	for i := range p.Rules {
		r := &p.Rules[i]
		if r.Selector == "" || actorTier(r.Actors, &req, s) == tierNone || !p.resourceMatches(r, &req) {
			continue
		}
		if sc, _, _ := p.scopeOf(r, &req); sc == scopeSelector {
			return true
		}
	}
	return false
}

// RoleList returns the principal's role list as composition exposes it
// (request.auth.role / request.auth.roles and the request document's role
// metas): verified email, group names, OIDC roles, then "users". It is
// empty for anonymous requests (R-PERM-74).
func (p *Policy) RoleList(pr *identity.Principal) []string {
	return p.subjectOf(pr).roleList()
}

// methodMatches implements R-PERM-23: "*" matches every method and a HEAD
// request is also covered by GET rules.
func methodMatches(ms []string, m string) bool {
	for _, x := range ms {
		if x == "*" || x == m || (m == "HEAD" && x == "GET") {
			return true
		}
	}
	return false
}

// resourceMatches reports whether one of the rule's patterns matches the
// document path. Values substituted into a pattern are escaped so they
// match literally (R-PERM-33). As on live PageLove (2026-09-28):
//   - a pattern ending in "/*" also covers the bare directory name ("/x/*"
//     governs "/x"), so a slash-less path is decided by the rules of the
//     directory it names;
//   - a rule written for the directory URL ("/d/") does not grant its index
//     document "/d/index.html"; pagelike still lets such a Deny refuse it
//     (fail closed; decisions.md authz.resource.directory-form-rule).
func (p *Policy) resourceMatches(r *Rule, req *Request) bool {
	dir := ""
	if strings.HasSuffix(req.Path, "/index.html") && r.Effect == Deny {
		dir = strings.TrimSuffix(req.Path, "index.html")
	}
	for i, pat := range r.Resources {
		var g *Glob
		if i < len(r.globs) && r.globs[i] != nil {
			g = r.globs[i]
		} else if strings.Contains(pat, "{{") {
			g = CompileGlob(pat) // literal comparison, lookups not rendered
		} else {
			g = CompileGlob(substitute(pat, req, GlobQuote))
		}
		if g.Match(req.Path) || (dir != "" && g.Match(dir)) {
			return true
		}
		if !strings.HasSuffix(req.Path, "/") && (strings.HasSuffix(pat, "/*") || strings.HasSuffix(pat, "/**")) && g.Match(req.Path+"/") {
			return true
		}
	}
	return false
}

// compiledSelector caches a static rule selector's compilation.
type compiledSelector struct {
	sel *selector.Selector
	err error
}

// scope says what a rule governs for one request.
type scope int

const (
	scopeResource scope = iota // the whole resource and every element in it
	scopeSelector              // elements matching the rule's selector
	scopeNone                  // nothing: an Allow whose selector is unusable
)

// scopeOf resolves the rule's selector for this request (substituting
// lookups; static selectors are compiled once per policy). A Deny whose
// selector substitutes to nothing or does not parse is widened to the whole
// resource, and such an Allow grants nothing: a refusal that cannot be
// scoped is widened, never dropped (R-PERM-29).
func (p *Policy) scopeOf(r *Rule, req *Request) (sc scope, text string, sel *selector.Selector) {
	if r.Selector == "" {
		return scopeResource, "", nil
	}
	text = r.Selector
	var err error
	if isTemplated(text) {
		text = trimASCII(substitute(text, req, identityQuote))
		if text != "" {
			sel, err = p.compileSelector(text)
		}
	} else {
		v, ok := p.selCache.Load(text)
		if !ok {
			s, e := p.compileSelector(text)
			v, _ = p.selCache.LoadOrStore(text, compiledSelector{sel: s, err: e})
		}
		c := v.(compiledSelector)
		sel, err = c.sel, c.err
	}
	switch {
	case text != "" && err == nil:
		return scopeSelector, text, sel
	case r.Effect == Deny:
		return scopeResource, "", nil
	}
	return scopeNone, "", nil
}

func (p *Policy) compileSelector(src string) (*selector.Selector, error) {
	if p.SelectorOptions != nil {
		return selector.CompileWith(src, *p.SelectorOptions)
	}
	return selector.Compile(src)
}

// normalizeSelectorText trims and collapses internal whitespace, the
// equality OPTIONS uses between a requested selector and a rule's.
func normalizeSelectorText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
