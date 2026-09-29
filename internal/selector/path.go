package selector

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Canonical document-rooted selectors (decision 0003 §6): the value pagelike
// puts in a QUERY or multipart part's "Content-Range: selector <css>". Two
// forms exist:
//
//	RootPath: html > body > ul:nth-child(2) > li:nth-child(3)
//	Path:     #list > li:nth-child(3)   (nearest ancestor-or-self whose id is
//	                                     unique in the document, else RootPath)
//
// Both use only CSS Level 3 syntax that browsers' querySelector, Servo's
// selectors crate and stock cascadia accept, with identifiers escaped as
// CSS.escape() does (p\:include, #\31 23), so a client can send the
// selector back in a follow-up request or apply it to its own DOM.
//
// Elements inside <template> contents are never matched (see Select), so
// no selector is produced for them.

// Path returns the id-anchored canonical selector for n within doc (doc may
// be nil, in which case n's root is used to check id uniqueness).
func Path(doc, n *html.Node) string {
	if doc == nil {
		doc = rootOf(n)
	}
	ids := idCounts(doc)
	var steps []string
	for e := n; e != nil && e.Type == html.ElementNode; e = parentElement(e) {
		if id, ok := getAttr(e, "id"); ok && id != "" && ids[id] == 1 {
			steps = append(steps, "#"+EscapeIdent(id))
			reverse(steps)
			return strings.Join(steps, " > ")
		}
		steps = append(steps, step(e))
	}
	reverse(steps)
	return strings.Join(steps, " > ")
}

// RootPath returns the positional selector for n from the root element.
func RootPath(n *html.Node) string {
	var steps []string
	for e := n; e != nil && e.Type == html.ElementNode; e = parentElement(e) {
		steps = append(steps, step(e))
	}
	reverse(steps)
	return strings.Join(steps, " > ")
}

func step(e *html.Node) string {
	p := parentElement(e)
	if p == nil {
		if e.Namespace == "" && e.DataAtom == atom.Html {
			return "html"
		}
		return ":root"
	}
	// head and body are unique children of the root html element.
	if e.Namespace == "" && p.DataAtom == atom.Html && p.Namespace == "" && (e.DataAtom == atom.Head || e.DataAtom == atom.Body) {
		return e.Data
	}
	return fmt.Sprintf("%s:nth-child(%d)", EscapeIdent(e.Data), childIndex(e))
}

func parentElement(e *html.Node) *html.Node {
	p := e.Parent
	if p == nil || p.Type != html.ElementNode {
		return nil
	}
	return p
}

func childIndex(e *html.Node) int {
	i := 0
	for c := e.Parent.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			i++
		}
		if c == e {
			return i
		}
	}
	return i
}

func rootOf(n *html.Node) *html.Node {
	for n.Parent != nil {
		n = n.Parent
	}
	return n
}

// idCounts counts id values among the elements selectors can match (not
// inside <template> contents).
func idCounts(r *html.Node) map[string]int {
	m := map[string]int{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if id, ok := getAttr(n, "id"); ok {
				m[id]++
			}
			if isTemplate(n) {
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(r)
	return m
}

func reverse(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// EscapeIdent escapes s as a CSS identifier (CSSOM CSS.escape()).
func EscapeIdent(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i, r := range rs {
		switch {
		case r == 0:
			b.WriteRune('�')
		case (r >= 1 && r <= 0x1f) || r == 0x7f:
			fmt.Fprintf(&b, "\\%x ", r)
		case i == 0 && r >= '0' && r <= '9':
			fmt.Fprintf(&b, "\\%x ", r)
		case i == 1 && r >= '0' && r <= '9' && rs[0] == '-':
			fmt.Fprintf(&b, "\\%x ", r)
		case i == 0 && r == '-' && len(rs) == 1:
			b.WriteString(`\-`)
		case r >= 0x80 || r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			b.WriteRune(r)
		default:
			b.WriteByte('\\')
			b.WriteRune(r)
		}
	}
	return b.String()
}
