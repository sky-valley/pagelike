// Copyright (c) 2011 Andy Balholm. All rights reserved. Use of this source
// code is governed by the BSD-style licence in LICENSE.cascadia.
// Modified for pagelike (see NOTICE).

package selector

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// Matcher is the interface for basic selector functionality.
// Match returns whether a selector matches n.
type Matcher interface {
	Match(n *html.Node) bool
}

// Sel is the interface for all the functionality provided by selectors.
type Sel interface {
	Matcher
	Specificity() Specificity

	// Returns a CSS input compiling to this selector.
	String() string

	// Returns a pseudo-element, or an empty string.
	PseudoElement() string
}

// Parse parses a selector. Use `ParseWithPseudoElement`
// if you need support for pseudo-elements.
func Parse(sel string) (Sel, error) {
	p := newParser(sel, nil)
	compiled, err := p.parseSelector()
	if err != nil {
		return nil, err
	}

	if p.i < len(sel) {
		return nil, fmt.Errorf("parsing %q: %d bytes left over", sel, len(sel)-p.i)
	}

	return compiled, nil
}

// ParseWithPseudoElement parses a single selector,
// with support for pseudo-element.
func ParseWithPseudoElement(sel string) (Sel, error) {
	p := newParser(sel, nil)
	p.acceptPseudoElements = true
	compiled, err := p.parseSelector()
	if err != nil {
		return nil, err
	}

	if p.i < len(sel) {
		return nil, fmt.Errorf("parsing %q: %d bytes left over", sel, len(sel)-p.i)
	}

	return compiled, nil
}

// ParseGroup parses a selector, or a group of selectors separated by commas.
// Use `ParseGroupWithPseudoElements`
// if you need support for pseudo-elements.
func ParseGroup(sel string) (SelectorGroup, error) {
	return ParseGroupWithOptions(sel, nil)
}

// ParseGroupWithOptions parses a selector list with pagelike options
// (registered pseudo-classes, strict mode).
func ParseGroupWithOptions(sel string, opts *Options) (SelectorGroup, error) {
	p := newParser(sel, opts)
	compiled, err := p.parseSelectorGroup()
	if err != nil {
		return nil, err
	}

	if p.i < len(sel) {
		return nil, fmt.Errorf("parsing %q: %d bytes left over", sel, len(sel)-p.i)
	}

	return compiled, nil
}

// ParseGroupWithPseudoElements parses a selector, or a group of selectors separated by commas.
// It supports pseudo-elements.
func ParseGroupWithPseudoElements(sel string) (SelectorGroup, error) {
	p := newParser(sel, nil)
	p.acceptPseudoElements = true
	compiled, err := p.parseSelectorGroup()
	if err != nil {
		return nil, err
	}

	if p.i < len(sel) {
		return nil, fmt.Errorf("parsing %q: %d bytes left over", sel, len(sel)-p.i)
	}

	return compiled, nil
}

func queryInto(n *html.Node, m Matcher, storage []*html.Node) []*html.Node {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if m.Match(child) {
			storage = append(storage, child)
		}
		storage = queryInto(child, m, storage)
	}

	return storage
}

// QueryAll returns a slice of all the nodes that match m, from the descendants
// of n.
func QueryAll(n *html.Node, m Matcher) []*html.Node {
	return queryInto(n, m, nil)
}

// Query returns the first node that matches m, from the descendants of n.
// If none matches, it returns nil.
func Query(n *html.Node, m Matcher) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if m.Match(c) {
			return c
		}
		if matched := Query(c, m); matched != nil {
			return matched
		}
	}

	return nil
}

// Filter returns the nodes that match m.
func Filter(nodes []*html.Node, m Matcher) (result []*html.Node) {
	for _, n := range nodes {
		if m.Match(n) {
			result = append(result, n)
		}
	}
	return result
}

type tagSelector struct {
	tag  string // ASCII-lowercased, for HTML elements
	orig string // as written, for foreign (SVG/MathML) and XML elements
}

// Matches elements with a given tag name. pagelike: HTML-namespace elements
// compare case-insensitively (the parser already lowercased them); foreign and
// XML elements (Namespace != "") compare case-sensitively, as browsers do.
func (t tagSelector) Match(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	if n.Namespace == "" {
		return n.Data == t.tag
	}
	if t.orig == "" {
		return n.Data == t.tag
	}
	return n.Data == t.orig
}

func (c tagSelector) Specificity() Specificity {
	return Specificity{0, 0, 1}
}

func (c tagSelector) PseudoElement() string {
	return ""
}

type classSelector struct {
	class string
}

// Matches elements by class attribute.
func (t classSelector) Match(n *html.Node) bool {
	return matchAttribute(n, "class", func(s string) bool {
		return matchInclude(t.class, s, false)
	})
}

func (c classSelector) Specificity() Specificity {
	return Specificity{0, 1, 0}
}

func (c classSelector) PseudoElement() string {
	return ""
}

type idSelector struct {
	id string
}

// Matches elements by id attribute.
func (t idSelector) Match(n *html.Node) bool {
	return matchAttribute(n, "id", func(s string) bool {
		return s == t.id
	})
}

func (c idSelector) Specificity() Specificity {
	return Specificity{1, 0, 0}
}

func (c idSelector) PseudoElement() string {
	return ""
}

type attrSelector struct {
	key, val, operation string
	origKey             string // pagelike: as written, for foreign/XML elements
	regexp              *regexp.Regexp
	insensitive         bool
}

// Matches elements by attribute value.
func (t attrSelector) Match(n *html.Node) bool {
	// pagelike: foreign and XML elements match attribute names case-sensitively.
	if n.Type == html.ElementNode && n.Namespace != "" && t.origKey != "" && t.origKey != t.key {
		t.key = t.origKey
	}
	switch t.operation {
	case "":
		return matchAttribute(n, t.key, func(string) bool { return true })
	case "=":
		return matchAttribute(n, t.key, func(s string) bool { return matchInsensitiveValue(s, t.val, t.insensitive) })
	case "!=":
		return attributeNotEqualMatch(t.key, t.val, n, t.insensitive)
	case "~=":
		// matches elements where the attribute named key is a whitespace-separated list that includes val.
		return matchAttribute(n, t.key, func(s string) bool { return matchInclude(t.val, s, t.insensitive) })
	case "|=":
		return attributeDashMatch(t.key, t.val, n, t.insensitive)
	case "^=":
		return attributePrefixMatch(t.key, t.val, n, t.insensitive)
	case "$=":
		return attributeSuffixMatch(t.key, t.val, n, t.insensitive)
	case "*=":
		return attributeSubstringMatch(t.key, t.val, n, t.insensitive)
	case "#=":
		return attributeRegexMatch(t.key, t.regexp, n)
	default:
		panic(fmt.Sprintf("unsuported operation : %s", t.operation))
	}
}

// matches elements where we ignore (or not) the case of the attribute value
// the user attribute is the value set by the user to match elements
// the real attribute is the attribute value found in the code parsed
func matchInsensitiveValue(userAttr string, realAttr string, ignoreCase bool) bool {
	if ignoreCase {
		return strings.EqualFold(userAttr, realAttr)
	}
	return userAttr == realAttr

}

// matches elements where the attribute named key satisifes the function f.
func matchAttribute(n *html.Node, key string, f func(string) bool) bool {
	if n.Type != html.ElementNode {
		return false
	}
	for _, a := range n.Attr {
		if attrName(a) == key && f(a.Val) {
			return true
		}
	}
	return false
}

// attrName returns the qualified name used for selector matching:
// x/net/html splits adjusted foreign attributes (xlink:href, xml:lang,
// xmlns:xlink) into Namespace + Key; everything else is in Key.
func attrName(a html.Attribute) string {
	if a.Namespace != "" {
		return a.Namespace + ":" + a.Key
	}
	return a.Key
}

// attributeNotEqualMatch matches elements where
// the attribute named key does not have the value val.
func attributeNotEqualMatch(key, val string, n *html.Node, ignoreCase bool) bool {
	if n.Type != html.ElementNode {
		return false
	}
	for _, a := range n.Attr {
		if attrName(a) == key && matchInsensitiveValue(a.Val, val, ignoreCase) {
			return false
		}
	}
	return true
}

// returns true if s is a whitespace-separated list that includes val.
func matchInclude(val string, s string, ignoreCase bool) bool {
	for s != "" {
		i := strings.IndexAny(s, " \t\r\n\f")
		if i == -1 {
			return matchInsensitiveValue(s, val, ignoreCase)
		}
		if matchInsensitiveValue(s[:i], val, ignoreCase) {
			return true
		}
		s = s[i+1:]
	}
	return false
}

// matches elements where the attribute named key equals val or starts with val plus a hyphen.
func attributeDashMatch(key, val string, n *html.Node, ignoreCase bool) bool {
	return matchAttribute(n, key,
		func(s string) bool {
			if matchInsensitiveValue(s, val, ignoreCase) {
				return true
			}
			if len(s) <= len(val) {
				return false
			}
			if matchInsensitiveValue(s[:len(val)], val, ignoreCase) && s[len(val)] == '-' {
				return true
			}
			return false
		})
}

// attributePrefixMatch returns a Selector that matches elements where
// the attribute named key starts with val.
func attributePrefixMatch(key, val string, n *html.Node, ignoreCase bool) bool {
	return matchAttribute(n, key,
		func(s string) bool {
			if strings.TrimSpace(s) == "" {
				return false
			}
			if ignoreCase {
				return strings.HasPrefix(strings.ToLower(s), strings.ToLower(val))
			}
			return strings.HasPrefix(s, val)
		})
}

// attributeSuffixMatch matches elements where
// the attribute named key ends with val.
func attributeSuffixMatch(key, val string, n *html.Node, ignoreCase bool) bool {
	return matchAttribute(n, key,
		func(s string) bool {
			if strings.TrimSpace(s) == "" {
				return false
			}
			if ignoreCase {
				return strings.HasSuffix(strings.ToLower(s), strings.ToLower(val))
			}
			return strings.HasSuffix(s, val)
		})
}

// attributeSubstringMatch matches nodes where
// the attribute named key contains val.
func attributeSubstringMatch(key, val string, n *html.Node, ignoreCase bool) bool {
	return matchAttribute(n, key,
		func(s string) bool {
			if strings.TrimSpace(s) == "" {
				return false
			}
			if ignoreCase {
				return strings.Contains(strings.ToLower(s), strings.ToLower(val))
			}
			return strings.Contains(s, val)
		})
}

// attributeRegexMatch  matches nodes where
// the attribute named key matches the regular expression rx
func attributeRegexMatch(key string, rx *regexp.Regexp, n *html.Node) bool {
	return matchAttribute(n, key,
		func(s string) bool {
			return rx.MatchString(s)
		})
}

func (c attrSelector) Specificity() Specificity {
	return Specificity{0, 1, 0}
}

func (c attrSelector) PseudoElement() string {
	return ""
}

// see pseudo_classes.go for pseudo classes selectors

// on a static context, some selectors can't match anything
type neverMatchSelector struct {
	value string
}

func (s neverMatchSelector) Match(n *html.Node) bool {
	return false
}

func (s neverMatchSelector) Specificity() Specificity {
	return Specificity{0, 0, 0}
}

func (c neverMatchSelector) PseudoElement() string {
	return ""
}

type compoundSelector struct {
	selectors     []Sel
	pseudoElement string
}

// Matches elements if each sub-selectors matches.
func (t compoundSelector) Match(n *html.Node) bool {
	if len(t.selectors) == 0 {
		return n.Type == html.ElementNode
	}

	for _, sel := range t.selectors {
		if !sel.Match(n) {
			return false
		}
	}
	return true
}

func (s compoundSelector) Specificity() Specificity {
	var out Specificity
	for _, sel := range s.selectors {
		out = out.Add(sel.Specificity())
	}
	if s.pseudoElement != "" {
		// https://drafts.csswg.org/selectors-3/#specificity
		out = out.Add(Specificity{0, 0, 1})
	}
	return out
}

func (c compoundSelector) PseudoElement() string {
	return c.pseudoElement
}

type combinedSelector struct {
	first      Sel
	combinator byte
	second     Sel
}

func (t combinedSelector) Match(n *html.Node) bool {
	if t.first == nil {
		return false // maybe we should panic
	}
	switch t.combinator {
	case 0:
		return t.first.Match(n)
	case ' ':
		return descendantMatch(t.first, t.second, n)
	case '>':
		return childMatch(t.first, t.second, n)
	case '+':
		return siblingMatch(t.first, t.second, true, n)
	case '~':
		return siblingMatch(t.first, t.second, false, n)
	default:
		panic("unknown combinator")
	}
}

// matches an element if it matches d and has an ancestor that matches a.
func descendantMatch(a, d Matcher, n *html.Node) bool {
	if !d.Match(n) {
		return false
	}

	for p := n.Parent; p != nil; p = p.Parent {
		if a.Match(p) {
			return true
		}
	}

	return false
}

// matches an element if it matches d and its parent matches a.
func childMatch(a, d Matcher, n *html.Node) bool {
	return d.Match(n) && n.Parent != nil && a.Match(n.Parent)
}

// matches an element if it matches s2 and is preceded by an element that matches s1.
// If adjacent is true, the sibling must be immediately before the element.
func siblingMatch(s1, s2 Matcher, adjacent bool, n *html.Node) bool {
	if !s2.Match(n) {
		return false
	}

	if adjacent {
		for n = n.PrevSibling; n != nil; n = n.PrevSibling {
			if n.Type == html.TextNode || n.Type == html.CommentNode {
				continue
			}
			return s1.Match(n)
		}
		return false
	}

	// Walk backwards looking for element that matches s1
	for c := n.PrevSibling; c != nil; c = c.PrevSibling {
		if s1.Match(c) {
			return true
		}
	}

	return false
}

func (s combinedSelector) Specificity() Specificity {
	spec := s.first.Specificity()
	if s.second != nil {
		spec = spec.Add(s.second.Specificity())
	}
	return spec
}

// on combinedSelector, a pseudo-element only makes sens on the last
// selector, although others increase specificity.
func (c combinedSelector) PseudoElement() string {
	if c.second == nil {
		return ""
	}
	return c.second.PseudoElement()
}

// A SelectorGroup is a list of selectors, which matches if any of the
// individual selectors matches.
type SelectorGroup []Sel

// Match returns true if the node matches one of the single selectors.
func (s SelectorGroup) Match(n *html.Node) bool {
	for _, sel := range s {
		if sel.Match(n) {
			return true
		}
	}
	return false
}
