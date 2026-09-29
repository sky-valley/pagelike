package compose

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/sessel"
)

// Namespace URIs composition recognises (docs/spec/composing.md R-COMP-10).
// Matching is exact string equality.
const (
	NSPagelove   = "https://pagelove.org/1.0"
	NSBindingCSS = "https://pagelove.org/Binding/CSS"
	NSSessel     = "https://pagelove.org/Binding/Sessel"
	NSJavaScript = "https://pagelove.org/Binding/JavaScript"
	// NSLegacyResource is accepted as an alias of NSBindingCSS (compat
	// decision C-16: an April 2026 agent skill documents it).
	NSLegacyResource = "https://pagelove.org/1.0/Resource"
)

// isPageloveNS reports whether uri is one of the built-in Pagelove
// namespaces (the 1.0 namespace and the three binding namespaces).
func isPageloveNS(uri string) bool {
	switch uri {
	case NSPagelove, NSBindingCSS, NSSessel, NSJavaScript, NSLegacyResource:
		return true
	}
	return false
}

// splitName splits a qualified name at its first colon.
func splitName(name string) (prefix, local string, ok bool) {
	i := strings.IndexByte(name, ':')
	if i <= 0 || i == len(name)-1 {
		return "", "", false
	}
	return name[:i], name[i+1:], true
}

// neverDispatched reports prefixes that are never directives (R-COMP-13).
func neverDispatched(prefix string) bool {
	switch prefix {
	case "xml", "xlink", "xmlns":
		return true
	}
	return false
}

// scope is one level of the composition Context (R-COMP-20): the names
// bound by bindings and method Context writes at one element, and the
// namespace declarations made there. Lookups go to the nearest ancestor.
type scope struct {
	parent *scope
	names  []string
	vals   map[string]sessel.Value
	ns     map[string]string // prefix → URI declared at this level
}

func (s *scope) child() *scope { return &scope{parent: s} }

// set binds name at this level (a later binding of the same name on the
// same element replaces the earlier one).
func (s *scope) set(name string, v sessel.Value) {
	if s.vals == nil {
		s.vals = map[string]sessel.Value{}
	}
	if _, ok := s.vals[name]; !ok {
		s.names = append(s.names, name)
	}
	s.vals[name] = v
}

// lookup finds name at the nearest level that binds it.
func (s *scope) lookup(name string) (sessel.Value, bool) {
	for x := s; x != nil; x = x.parent {
		if v, ok := x.vals[name]; ok {
			return v, true
		}
	}
	return nil, false
}

// visible returns every visible name (nearest wins), outermost first.
func (s *scope) visible() ([]string, map[string]sessel.Value) {
	var chain []*scope
	for x := s; x != nil; x = x.parent {
		chain = append(chain, x)
	}
	m := map[string]sessel.Value{}
	var order []string
	for i := len(chain) - 1; i >= 0; i-- {
		for _, n := range chain[i].names {
			if _, seen := m[n]; !seen {
				order = append(order, n)
			}
			m[n] = chain[i].vals[n]
		}
	}
	return order, m
}

// declare records the xmlns:* declarations of element n at this level.
func (s *scope) declare(n *html.Node) {
	for _, a := range n.Attr {
		if a.Namespace != "" {
			continue
		}
		p, ok := strings.CutPrefix(a.Key, "xmlns:")
		if a.Key == "xmlns" {
			p, ok = "", true // the default namespace (XML fragments need it)
		}
		if !ok || (p == "" && a.Key != "xmlns") {
			continue
		}
		if s.ns == nil {
			s.ns = map[string]string{}
		}
		if _, dup := s.ns[p]; !dup {
			s.ns[p] = a.Val
		}
	}
}

// inScope returns every namespace declaration visible at s (nearest wins),
// keyed by prefix ("" is the default namespace).
func (s *scope) inScope() map[string]string {
	out := map[string]string{}
	for x := s; x != nil; x = x.parent {
		for p, u := range x.ns {
			if _, seen := out[p]; !seen {
				out[p] = u
			}
		}
	}
	return out
}

// resolve returns the URI prefix is bound to. An undeclared "pagelove"
// prefix is the 1.0 namespace (compat decision R-COMP-12: the docs'
// examples use pagelove:template without a visible declaration).
func (s *scope) resolve(prefix string) (string, bool) {
	if prefix == "" {
		return "", false
	}
	for x := s; x != nil; x = x.parent {
		if u, ok := x.ns[prefix]; ok {
			return u, true
		}
	}
	if prefix == "pagelove" {
		return NSPagelove, true
	}
	return "", false
}

// withDeclarationsOf returns a scope under s carrying the namespace
// declarations in scope at n in its own document (its ancestors' and its
// own): content spliced from another document resolves prefixes with the
// origin's declarations first, then the including page's (R-COMP-84).
func (s *scope) withDeclarationsOf(n *html.Node) *scope {
	var chain []*html.Node
	for x := n.Parent; x != nil; x = x.Parent {
		if x.Type == html.ElementNode {
			chain = append(chain, x)
		}
	}
	out := s.child()
	for i := len(chain) - 1; i >= 0; i-- {
		lvl := out.child()
		lvl.declare(chain[i])
		out = lvl
	}
	return out
}
