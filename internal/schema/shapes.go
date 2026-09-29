package schema

import (
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/selector"
)

// Shape is a ShapeConstraint declaration (R-MOD-61).
type Shape struct {
	Path        string
	Resources   []string
	globs       []*authz.Glob
	Selectors   []shapeSel // scope selectors (OR); none = the root element
	Constraints []shapeSel
	Permits     []shapeSel // any permit closes the shape
}

type shapeSel struct {
	text string
	sel  *selector.Selector
}

// Closed reports whether the shape has permits (R-MOD-65).
func (sh *Shape) Closed() bool { return len(sh.Permits) > 0 }

// Shapes returns every loaded shape.
func (r *Registry) Shapes() []*Shape { return r.shapes }

func parseShape(r *Registry, path string, it *microdata.Item) *Shape {
	sh := &Shape{Path: path, Resources: fields(it, "resource")}
	for _, g := range sh.Resources {
		sh.globs = append(sh.globs, authz.CompileGlob(g))
	}
	ext := selector.ExtOptions{IsA: r.IsA}
	compile := func(name string) ([]shapeSel, bool) {
		var out []shapeSel
		for _, t := range fields(it, name) {
			s, err := selector.CompileWith(t, ext)
			if err != nil {
				return nil, false
			}
			out = append(out, shapeSel{text: t, sel: s})
		}
		return out, true
	}
	var ok bool
	if sh.Selectors, ok = compile("selector"); !ok {
		return nil // a selector that does not parse disables the shape
	}
	if sh.Constraints, ok = compile("constraint"); !ok {
		return nil
	}
	if sh.Permits, ok = compile("permit"); !ok {
		return nil
	}
	if len(sh.Constraints) == 0 && len(sh.Permits) == 0 {
		return nil // neither: ignored (R-MOD-61, C6)
	}
	return sh
}

// appliesTo reports whether the shape governs document path p (R-MOD-62).
func (sh *Shape) appliesTo(p string) bool {
	if len(sh.globs) == 0 {
		return true
	}
	for _, g := range sh.globs {
		if g.Match(p) {
			return true
		}
	}
	return false
}

// scope returns the scope elements of the shape in doc (document order)
// with, for each, the selector text that matched it (":root" without
// selectors).
func (sh *Shape) scope(doc *html.Node) ([]*html.Node, map[*html.Node]string) {
	label := map[*html.Node]string{}
	if len(sh.Selectors) == 0 {
		root := dom.DocumentElement(doc)
		if root == nil {
			return nil, label
		}
		label[root] = ":root"
		return []*html.Node{root}, label
	}
	for _, s := range sh.Selectors {
		for _, n := range s.sel.MatchAll(doc) {
			if _, ok := label[n]; !ok {
				label[n] = s.text
			}
		}
	}
	var out []*html.Node
	dom.Walk(doc, func(n *html.Node) bool {
		if _, ok := label[n]; ok {
			out = append(out, n)
		}
		return true
	})
	return out, label
}

// checkShapes evaluates the applicable shapes on doc, checking the scope
// elements for which affected returns true (R-MOD-63..67).
func (r *Registry) checkShapes(path string, doc *html.Node, affected func(*html.Node) bool) []ShapeViolation {
	var applicable []*Shape
	for _, sh := range r.shapes {
		if sh.appliesTo(path) {
			applicable = append(applicable, sh)
		}
	}
	if len(applicable) == 0 {
		return nil
	}
	type scoped struct {
		els   []*html.Node
		label map[*html.Node]string
	}
	scopes := make([]scoped, len(applicable))
	closedRoot := map[*html.Node]bool{} // ownership over the whole D′ (R-MOD-67)
	for i, sh := range applicable {
		els, label := sh.scope(doc)
		scopes[i] = scoped{els, label}
		if sh.Closed() {
			for _, e := range els {
				closedRoot[e] = true
			}
		}
	}
	var vs []ShapeViolation
	for i, sh := range applicable {
		for _, e := range scopes[i].els {
			if !affected(e) {
				continue
			}
			label := scopes[i].label[e]
			for _, c := range sh.Constraints {
				if !c.sel.Matches(e) {
					vs = append(vs, ShapeViolation{Selector: label, Constraint: c.text,
						Message: fmt.Sprintf("Element matching '%s' does not satisfy constraint '%s'", label, c.text)})
				}
			}
			if sh.Closed() {
				vs = append(vs, sh.checkClosed(e, label, closedRoot)...)
			}
		}
	}
	return vs
}

// checkClosed checks a closed shape's scope root r (R-MOD-65/66): every
// descendant element must match a permit, every attribute must be covered
// by a matching permit, r's attributes by its matching permits or the
// shape's selectors; subtrees of nested closed scopes belong to them.
func (sh *Shape) checkClosed(r *html.Node, label string, closedRoot map[*html.Node]bool) []ShapeViolation {
	var vs []ShapeViolation
	// Messages as PageLove words them (live 2026-09-29).
	attrFail := func(n *html.Node, name string) {
		msg := fmt.Sprintf("Attribute <%s> on <%s> inside '%s' is not permitted by any matching permit", name, n.Data, label)
		if n == r {
			msg = fmt.Sprintf("Attribute <%s> on <%s> matched by '%s' is not permitted by the selector or any matching permit", name, n.Data, label)
		}
		vs = append(vs, ShapeViolation{Selector: label, Constraint: "permit", Message: msg})
	}
	check := func(n *html.Node, pool []shapeSel) {
		covered := map[string]bool{}
		matched := false
		for _, ps := range pool {
			ok, names := ps.sel.CoveredAttributes(n)
			if !ok {
				continue
			}
			matched = true
			for _, a := range names {
				covered[a] = true
			}
		}
		if !matched && n != r {
			vs = append(vs, ShapeViolation{Selector: label, Constraint: "permit",
				Message: fmt.Sprintf("Element <%s> inside '%s' is not permitted by any permit", n.Data, label)})
			return
		}
		for _, a := range n.Attr {
			name := a.Key
			if a.Namespace != "" {
				name = a.Namespace + ":" + a.Key
			}
			key := name
			if n.Namespace == "" {
				key = strings.ToLower(name)
			}
			if !covered[key] {
				attrFail(n, name)
			}
		}
	}
	check(r, append(append([]shapeSel{}, sh.Permits...), sh.Selectors...))
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue // text, comments and the like are always allowed
			}
			check(c, sh.Permits)
			if closedRoot[c] {
				continue // a nested closed scope owns its subtree
			}
			walk(c)
		}
	}
	walk(r)
	return vs
}

// shapes runs stage 9 on the final D′: 422, or 409 for a DELETE (R-MOD-68).
func (p *pass) shapes() error {
	affected := func(n *html.Node) bool { return p.elems == nil || p.elems[n] }
	vs := p.reg.checkShapes(p.op.Path, p.after, affected)
	if len(vs) == 0 {
		return nil
	}
	status := http.StatusUnprocessableEntity
	if p.op.Method == "DELETE" {
		status = http.StatusConflict
	}
	return shapeError(status, vs)
}

// moveShapes re-checks, for a whole-document MOVE, the shapes whose
// resource matches the destination (the content is unchanged, so no
// schema checks run; R-MOD-13).
func (p *pass) moveShapes() error {
	if p.w.Before == nil || len(p.reg.shapes) == 0 {
		return nil
	}
	dest, err := engine.NormalizePath(p.op.Destination)
	if err != nil || p.op.Destination == "" {
		return nil
	}
	vs := p.reg.checkShapes(dest, p.w.Before, func(*html.Node) bool { return true })
	if len(vs) == 0 {
		return nil
	}
	return shapeError(http.StatusUnprocessableEntity, vs)
}
