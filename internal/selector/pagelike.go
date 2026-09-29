// pagelike additions to the cascadia fork: parse options, pluggable
// pseudo-classes, :is()/:where(), :scope, anchored relative :has(),
// :nth-child(An+B of S) and scoped queries.

package selector

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// Options configures parsing.
type Options struct {
	// Pseudo registers pseudo-classes by lower-case name. Registered names
	// take precedence over cascadia's built-ins (so :contains can be given
	// PageLove semantics).
	Pseudo map[string]PseudoClassFunc
	// Strict rejects cascadia-only syntax (:matches, :matchesOwn,
	// :containsOwn, :haschild, :input, [a!=b], [a#=re]) and cascadia's own
	// :contains when no PageLove :contains is registered.
	Strict bool
	// CascadiaEmpty restores cascadia's :empty (whitespace-only text counts as
	// empty). The default is browser / Servo semantics.
	CascadiaEmpty bool
}

// PseudoClassFunc builds a matcher for a registered pseudo-class.
type PseudoClassFunc func(ctx *PseudoContext) (Sel, error)

// PseudoContext is handed to a PseudoClassFunc.
type PseudoContext struct {
	Name    string
	HasArgs bool   // false for a non-functional use (":foo")
	Args    string // raw text between the parentheses
	opts    *Options
}

// ParseSelectorList parses s as a selector list with the same options.
func (c *PseudoContext) ParseSelectorList(s string) (SelectorGroup, error) {
	return ParseGroupWithOptions(s, c.opts)
}

// RelativeList is a parsed <relative-selector-list>.
type RelativeList []relativeSelector

// ParseRelativeSelectorList parses s as a relative selector list
// ("> li, > li span", "li").
func (c *PseudoContext) ParseRelativeSelectorList(s string) (RelativeList, error) {
	p := newParser(s, c.opts)
	rel, err := p.parseRelativeSelectorList()
	if err != nil {
		return nil, err
	}
	p.skipWhitespace()
	if p.i < len(p.s) {
		return nil, fmt.Errorf("parsing %q: %d bytes left over", s, len(s)-p.i)
	}
	return rel, nil
}

// MatchesFrom reports whether candidate matches any item of the list when
// the list is anchored at subject.
func (l RelativeList) MatchesFrom(subject, candidate *html.Node) bool {
	for _, r := range l {
		if bindAnchor(r.sel, subject).Match(candidate) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- :is/:where

type isPseudoClassSelector struct {
	where bool
	match SelectorGroup
}

func (s isPseudoClassSelector) Match(n *html.Node) bool {
	return n.Type == html.ElementNode && s.match.Match(n)
}

func (s isPseudoClassSelector) Specificity() Specificity {
	if s.where {
		return Specificity{}
	}
	var max Specificity
	for _, sel := range s.match {
		if sp := sel.Specificity(); max.Less(sp) {
			max = sp
		}
	}
	return max
}

func (s isPseudoClassSelector) PseudoElement() string { return "" }

func (s isPseudoClassSelector) String() string {
	name := "is"
	if s.where {
		name = "where"
	}
	return fmt.Sprintf(":%s(%s)", name, s.match.String())
}

// ---------------------------------------------------------------- :scope

// scopeSelector is ":scope". Unbound (a plain query) it matches the root
// element, like document.querySelector(':scope'); QueryScoped binds it.
type scopeSelector struct{}

func (scopeSelector) Match(n *html.Node) bool {
	return n.Type == html.ElementNode && (n.Parent == nil || n.Parent.Type == html.DocumentNode)
}
func (scopeSelector) Specificity() Specificity { return Specificity{0, 1, 0} }
func (scopeSelector) PseudoElement() string    { return "" }
func (scopeSelector) String() string           { return ":scope" }

// identitySel matches exactly one node (a bound :scope or :has() anchor).
type identitySel struct{ node *html.Node }

func (s identitySel) Match(n *html.Node) bool { return n == s.node }
func (identitySel) Specificity() Specificity  { return Specificity{0, 1, 0} }
func (identitySel) PseudoElement() string     { return "" }
func (identitySel) String() string            { return ":scope" }

// ---------------------------------------------------------------- :has()

// scopeAnchor is the implicit leftmost compound of a relative selector.
type scopeAnchor struct{}

func (scopeAnchor) Match(*html.Node) bool    { return false }
func (scopeAnchor) Specificity() Specificity { return Specificity{} }
func (scopeAnchor) PseudoElement() string    { return "" }
func (scopeAnchor) String() string           { return "" }

type relativeSelector struct {
	combinator byte // ' ', '>', '+', '~'
	sel        Sel  // chain whose leftmost element is a scopeAnchor
}

// anchorLeftmost rewrites "A b B" into "<anchor> comb A b B".
func anchorLeftmost(sel Sel, comb byte) Sel {
	if c, ok := sel.(combinedSelector); ok {
		return combinedSelector{first: anchorLeftmost(c.first, comb), combinator: c.combinator, second: c.second}
	}
	return combinedSelector{first: scopeAnchor{}, combinator: comb, second: sel}
}

// bindAnchor replaces the scopeAnchor on the left spine with subject.
// It allocates O(chain length) per call and never mutates shared state, so
// compiled selectors stay safe for concurrent use.
func bindAnchor(sel Sel, subject *html.Node) Sel {
	switch c := sel.(type) {
	case combinedSelector:
		return combinedSelector{first: bindAnchor(c.first, subject), combinator: c.combinator, second: c.second}
	case scopeAnchor:
		return identitySel{subject}
	}
	return sel
}

type hasPseudoClassSelector struct {
	rel []relativeSelector
}

func (s hasPseudoClassSelector) Match(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	for _, r := range s.rel {
		bound := bindAnchor(r.sel, n)
		switch r.combinator {
		case ' ', '>':
			if anyDescendant(n, bound) {
				return true
			}
		case '+', '~':
			for sib := n.NextSibling; sib != nil; sib = sib.NextSibling {
				if sib.Type != html.ElementNode {
					continue
				}
				if bound.Match(sib) || anyDescendant(sib, bound) {
					return true
				}
				if r.combinator == '+' && !chainGoesDeeper(r.sel) {
					break
				}
			}
		}
	}
	return false
}

func chainGoesDeeper(sel Sel) bool {
	c, ok := sel.(combinedSelector)
	if !ok {
		return false
	}
	return c.combinator == ' ' || c.combinator == '>' || chainGoesDeeper(c.first)
}

func anyDescendant(n *html.Node, m Matcher) bool {
	if isTemplate(n) {
		return false
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		if m.Match(c) || anyDescendant(c, m) {
			return true
		}
	}
	return false
}

func (s hasPseudoClassSelector) Specificity() Specificity {
	var max Specificity
	for _, r := range s.rel {
		if sp := r.sel.Specificity(); max.Less(sp) {
			max = sp
		}
	}
	return max
}

func (hasPseudoClassSelector) PseudoElement() string { return "" }

func (s hasPseudoClassSelector) String() string {
	parts := make([]string, len(s.rel))
	for i, r := range s.rel {
		parts[i] = strings.TrimSpace(relString(r))
	}
	return ":has(" + strings.Join(parts, ", ") + ")"
}

func relString(r relativeSelector) string {
	str := r.sel.String()
	str = strings.TrimPrefix(str, " ")
	if r.combinator == ' ' {
		return strings.TrimSpace(str)
	}
	return str
}

// ---------------------------------------------------------------- :nth-child(An+B of S)

type nthOfPseudoClassSelector struct {
	nthPseudoClassSelector
	of SelectorGroup
}

func (s nthOfPseudoClassSelector) Match(n *html.Node) bool {
	if n.Type != html.ElementNode || n.Parent == nil || !s.of.Match(n) {
		return false
	}
	i, count := -1, 0
	for c := n.Parent.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || !s.of.Match(c) {
			continue
		}
		count++
		if c == n {
			i = count
		}
	}
	if s.last {
		i = count - i + 1
	}
	i -= s.b
	if s.a == 0 {
		return i == 0
	}
	return i%s.a == 0 && i/s.a >= 0
}

func (s nthOfPseudoClassSelector) Specificity() Specificity {
	sp := Specificity{0, 1, 0}
	var max Specificity
	for _, sel := range s.of {
		if x := sel.Specificity(); max.Less(x) {
			max = x
		}
	}
	return sp.Add(max)
}

func (s nthOfPseudoClassSelector) String() string {
	name := "nth-child"
	if s.last {
		name = "nth-last-child"
	}
	return fmt.Sprintf(":%s(%dn%+d of %s)", name, s.a, s.b, s.of.String())
}

// ---------------------------------------------------------------- scoped queries

// bindScope returns sel with every unbound :scope replaced by root.
func bindScope(sel Sel, root *html.Node) Sel {
	switch c := sel.(type) {
	case scopeSelector:
		return identitySel{root}
	case combinedSelector:
		out := combinedSelector{first: bindScope(c.first, root), combinator: c.combinator}
		if c.second != nil {
			out.second = bindScope(c.second, root)
		}
		return out
	case compoundSelector:
		out := compoundSelector{pseudoElement: c.pseudoElement, selectors: make([]Sel, len(c.selectors))}
		for i, s := range c.selectors {
			out.selectors[i] = bindScope(s, root)
		}
		return out
	case relativePseudoClassSelector:
		return relativePseudoClassSelector{name: c.name, match: bindScopeGroup(c.match, root)}
	case isPseudoClassSelector:
		return isPseudoClassSelector{where: c.where, match: bindScopeGroup(c.match, root)}
	}
	return sel
}

func bindScopeGroup(g SelectorGroup, root *html.Node) SelectorGroup {
	out := make(SelectorGroup, len(g))
	for i, s := range g {
		out[i] = bindScope(s, root)
	}
	return out
}

// QueryAllScoped is element.querySelectorAll(sel): matches among the
// descendants of root, with :scope bound to root.
func QueryAllScoped(root *html.Node, g SelectorGroup) []*html.Node {
	return QueryAll(root, bindScopeGroup(g, root))
}

// BindScope returns g with every unbound :scope replaced by root, for
// callers that walk the tree themselves (Sessel's sub-select, which also
// keeps out of <template> contents).
func BindScope(g SelectorGroup, root *html.Node) SelectorGroup { return bindScopeGroup(g, root) }

// IsTemplate reports an HTML <template> element, whose contents selectors
// never enter.
func IsTemplate(n *html.Node) bool { return isTemplate(n) }

// isTemplate reports an HTML <template> element (whose contents browsers
// and Servo's selectors keep in a separate DocumentFragment).
func isTemplate(n *html.Node) bool {
	return n.Type == html.ElementNode && n.Namespace == "" && n.Data == "template"
}

// Select returns all matches among n's descendants (and n itself when it is
// an element), in document order, with :scope bound to the document element
// and without descending into <template> contents (browser parity).
func Select(n *html.Node, g SelectorGroup) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.ElementNode && g.Match(c) {
			out = append(out, c)
		}
		if isTemplate(c) {
			return
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			walk(k)
		}
	}
	walk(n)
	return out
}

// SelectFirst is the Range: selector= rule: first match in document order.
func SelectFirst(n *html.Node, g SelectorGroup) *html.Node {
	var found *html.Node
	var walk func(*html.Node) bool
	walk = func(c *html.Node) bool {
		if c.Type == html.ElementNode && g.Match(c) {
			found = c
			return true
		}
		if isTemplate(c) {
			return false
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			if walk(k) {
				return true
			}
		}
		return false
	}
	walk(n)
	return found
}

// QueryFirst returns the first match in document order among n and its
// descendants (the Range: selector= "first match" rule).
func QueryFirst(n *html.Node, m Matcher) *html.Node {
	if n.Type == html.ElementNode && m.Match(n) {
		return n
	}
	return Query(n, m)
}
