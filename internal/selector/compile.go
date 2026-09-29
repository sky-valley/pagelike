// Package selector compiles and evaluates CSS selectors against x/net/html
// trees (HTML documents and XML documents parsed by xmldom). It is the
// single place the runtime resolves selectors.
//
// The engine is a fork of github.com/andybalholm/cascadia v1.3.5 (see
// NOTICE and LICENSE.cascadia; decision 0003 §4–5) that adds :is(),
// :where(), :scope, relative and anchored :has(), :nth-child(An+B of S),
// HTML-versus-foreign case sensitivity, browser :empty, and a pseudo-class
// registry used for PageLove's selector extensions (ext.go): :contains,
// :equals, :value-contains, :value-equals, :less-than, :greater-than,
// :value-less-than, :value-greater-than, :only and :isa. Selector
// functions (count(), text-of(), value-of(), attr-of()) are expanded before
// parsing by ExpandFunctions.
//
// Compile is strict: cascadia-only syntax (:containsOwn, :matches,
// :haschild, :input, [a!=b], [a#=re]) and pseudo-elements are rejected, as
// in browsers and Servo's selectors. Matching never descends into
// <template> contents.
package selector

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// Selector is a compiled selector list.
type Selector struct {
	src   string
	group SelectorGroup
}

// defaultOptions are the PageLove parse options without an inheritance map
// (:isa matches nothing). Read-only once built, so safe to share.
var defaultOptions = PageLoveOptions(ExtOptions{})

// Compile parses a selector list with PageLove's extensions.
func Compile(src string) (*Selector, error) {
	return compile(src, defaultOptions)
}

// CompileWith parses a selector list with request- or site-specific
// extension context (the schema inheritance map :isa() consults).
func CompileWith(src string, ext ExtOptions) (*Selector, error) {
	return compile(src, PageLoveOptions(ext))
}

// CompileOptions parses a selector list with caller-built options (for
// example PageLoveOptions plus a private pseudo-class).
func CompileOptions(src string, opts *Options) (*Selector, error) {
	return compile(src, opts)
}

func compile(src string, opts *Options) (*Selector, error) {
	s := strings.TrimSpace(src)
	if s == "" {
		return nil, fmt.Errorf("empty selector")
	}
	g, err := ParseGroupWithOptions(s, opts)
	if err != nil {
		return nil, fmt.Errorf("invalid selector %q: %w", s, err)
	}
	return &Selector{src: s, group: g}, nil
}

// MustCompile panics on error; for static selectors.
func MustCompile(src string) *Selector {
	s, err := Compile(src)
	if err != nil {
		panic(err)
	}
	return s
}

// String returns the source text.
func (s *Selector) String() string { return s.src }

// Group returns the compiled selector list.
func (s *Selector) Group() SelectorGroup { return s.group }

// MatchAll returns root (when it is an element) and its descendants that
// match, in document order, without descending into <template> contents.
func (s *Selector) MatchAll(root *html.Node) []*html.Node { return Select(root, s.group) }

// MatchFirst returns the first match in document order (the Range:
// selector= rule), or nil.
func (s *Selector) MatchFirst(root *html.Node) *html.Node { return SelectFirst(root, s.group) }

// Matches reports whether n matches the selector.
func (s *Selector) Matches(n *html.Node) bool {
	return n != nil && n.Type == html.ElementNode && s.group.Match(n)
}
