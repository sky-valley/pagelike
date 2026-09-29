// Subject-compound attribute coverage, for ShapeConstraint permits
// (docs/spec/modeling.md R-MOD-66). Added for internal/schema.

package selector

import (
	"strings"

	"golang.org/x/net/html"
)

// CoveredAttributes reports whether n matches the selector list and, if so,
// which attribute names the matching list members name in their subject
// compound selector: attribute selectors ([a], [a=v], [a^=v], …) name a,
// class selectors name "class" and id selectors name "id". Tag selectors,
// the universal selector and pseudo-classes (including everything inside
// :has(), :not(), :is() and :where()) name nothing, and compounds left of a
// combinator do not count (`ul > li[class]` names only class). Names are
// returned as the selector wrote them for foreign elements and lower-cased
// for HTML elements, and a qualified attribute name (xml:lang) is distinct
// from its local name (lang).
func (s *Selector) CoveredAttributes(n *html.Node) (matched bool, names []string) {
	if s == nil || n == nil || n.Type != html.ElementNode {
		return false, nil
	}
	for _, member := range s.group {
		if !member.Match(n) {
			continue
		}
		matched = true
		names = append(names, subjectAttributes(member, n.Namespace != "")...)
	}
	return matched, names
}

// subjectAttributes lists the attribute names the subject compound of sel
// references.
func subjectAttributes(sel Sel, foreign bool) []string {
	switch s := sel.(type) {
	case combinedSelector:
		if s.second != nil {
			return subjectAttributes(s.second, foreign)
		}
		return subjectAttributes(s.first, foreign)
	case compoundSelector:
		var out []string
		for _, part := range s.selectors {
			out = append(out, simpleAttribute(part, foreign)...)
		}
		return out
	}
	return simpleAttribute(sel, foreign)
}

func simpleAttribute(sel Sel, foreign bool) []string {
	switch s := sel.(type) {
	case classSelector:
		return []string{"class"}
	case idSelector:
		return []string{"id"}
	case attrSelector:
		if foreign && s.origKey != "" {
			return []string{s.origKey}
		}
		return []string{strings.ToLower(s.key)}
	}
	return nil
}
