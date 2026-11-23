package authz

import (
	"strings"

	"golang.org/x/net/html"
)

// An element Deny narrows a page-wide Allow (docs AuthorizationRule §An
// element deny narrows a page-wide allow and §Whole-page writes, snapshot
// 2026-11-23; spec R-PERM-122..124).
//
// A rule with no selector allows a write to every element of the page. A
// selector-scoped Deny that applies to the requester takes elements back:
// against the element rules the page-wide allow counts as the least
// specific rule, whatever its actor. An element is protected when the
// element rules alone (those whose selector matches it) resolve to Deny.

// ElementDenyApplies reports whether a selector-scoped Deny applies to the
// request: its method, actor and resource match. When none does, writes are
// decided by the ordinary rules alone and nothing more is checked.
func (p *Policy) ElementDenyApplies(req Request) bool {
	s := p.subjectOf(req.Principal)
	method := strings.ToUpper(req.Method)
	for i := range p.Rules {
		r := &p.Rules[i]
		if r.Effect != Deny || r.Selector == "" || !methodMatches(r.Methods, method) {
			continue
		}
		if actorTier(r.Actors, &req, s) == tierNone || !p.resourceMatches(r, &req) {
			continue
		}
		if sc, _, _ := p.scopeOf(r, &req); sc == scopeSelector {
			return true
		}
	}
	return false
}

// elementDenied reports whether the element rules alone resolve to Deny for
// n: a more specific element Allow beats a less specific element Deny, a
// tie goes to deny, and no element rule means no opinion.
func (p *Policy) elementDenied(req *Request, s *subject, n *html.Node) bool {
	d := p.resolve(req, s, func(r *Rule) bool {
		sc, _, sel := p.scopeOf(r, req)
		return sc == scopeSelector && sel.Matches(n)
	})
	return d.Matched && !d.Allowed
}

// NarrowedWrite reports whether an element Deny refuses a selector write to
// target that the ordinary rules allow: a deny covering the element
// written, an element it sits inside, or (unless the write only adds
// children, as POST does to its container) an element inside it.
func (p *Policy) NarrowedWrite(req Request, target *html.Node, descendants bool) bool {
	if target == nil || !p.ElementDenyApplies(req) {
		return false
	}
	s := p.subjectOf(req.Principal)
	for n := target; n != nil; n = n.Parent {
		if n.Type == html.ElementNode && p.elementDenied(&req, s, n) {
			return true
		}
	}
	if !descendants {
		return false
	}
	var found bool
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil && !found; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if p.elementDenied(&req, s, c) {
				found = true
				return
			}
			walk(c)
		}
	}
	walk(target)
	return found
}

// Protected returns the outermost protected elements of root in document
// order. An element inside a protected one is part of it, whatever its own
// rules say; an element a more specific Allow frees is skipped and its
// descendants are examined. root may be nil (a page that does not exist,
// or a body stored as a file): it holds none.
func (p *Policy) Protected(req Request, root *html.Node) []*html.Node {
	if root == nil {
		return nil
	}
	s := p.subjectOf(req.Principal)
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && p.elementDenied(&req, s, c) {
				out = append(out, c)
				continue
			}
			walk(c)
		}
	}
	walk(root)
	return out
}
