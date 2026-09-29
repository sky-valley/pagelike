package compose

import (
	"errors"
	"net/http"
	"path"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/sessel"
)

// elementDirective dispatches a prefixed element (R-COMP-25 step 1): the
// built-in include and stamp, or a schema method. The element is replaced
// by the result, which is composed in turn.
func (c *composer) elementDirective(n *html.Node, r *region, own *scope, prefix, local string) error {
	name := prefix + ":" + local
	uri, bound := own.resolve(prefix)
	if !bound {
		return noMethod(name) // R-COMP-13 row 1
	}
	switch uri {
	case NSPagelove:
		switch local {
		case "include":
			return c.include(n, r, own)
		case "stamp":
			return c.stamp(n, r, own)
		}
		return errdoc.New(http.StatusInternalServerError, KindNoMethod, "unknown method '%s' on Pagelove namespace", local)
	case NSBindingCSS, NSLegacyResource, NSSessel, NSJavaScript:
		// Bindings exist only in attribute form ([JB] §Only the attribute form).
		return noMethod(name)
	}
	if !c.eng.methods().HasSchema(c.snap, uri) {
		return noMethod(name)
	}
	cx := c.contextDict(own)
	res, err := c.eng.methods().Dispatch(c.ctx, &MethodCall{Snap: c.snap, TypeURL: uri, Name: local, Attrs: n.Attr,
		Self: c.dispatchedElement(n, r), DocPath: c.docPath, Context: cx, Request: c.sesselRequest(),
		Host: c.sesselHost(), Budget: c.budget.Sessel(), JS: c.eng.js()})
	if errors.Is(err, ErrNoSchema) || errors.Is(err, ErrNoMethod) {
		return noMethod(name)
	}
	if err != nil {
		return wrapError(name, err)
	}
	if res.Private {
		c.private = true
	}
	// A method's Context mutations are visible to the content replacing
	// the element (R-COMP-21).
	sub := own.child()
	c.applyContext(sub, own, cx, res.Context)
	return c.emitValue(res.Value, contextOf(n, r), r, sub)
}

// methodAttribute dispatches a schema method written as an attribute
// (R-COMP-63): the value is the first declared parameter. A method that
// declares returns https://pagelove.org/Element and yields a non-null
// result replaces the whole host; otherwise the attribute is removed and
// any Context mutation is visible to the host's subtree.
func (c *composer) methodAttribute(n *html.Node, r *region, own *scope, d attrDirective) (bool, error) {
	cx := c.contextDict(own)
	res, err := c.eng.methods().Dispatch(c.ctx, &MethodCall{Snap: c.snap, TypeURL: d.uri, Name: d.local, Attribute: true,
		Value: d.val, Self: c.dispatchedElement(n, r), DocPath: c.docPath, Context: cx, Request: c.sesselRequest(),
		Host: c.sesselHost(), Budget: c.budget.Sessel(), JS: c.eng.js()})
	if errors.Is(err, ErrNoSchema) || errors.Is(err, ErrNoMethod) {
		return false, nil // attribute form never errors: stripped silently (R-COMP-67)
	}
	if err != nil {
		return false, wrapError(d.key, err)
	}
	if res.Private {
		c.private = true
	}
	if res.ReturnsElement && res.Value != nil {
		sub := own.child()
		c.applyContext(sub, own, cx, res.Context)
		return true, c.emitValue(res.Value, contextOf(n, r), r, sub)
	}
	c.applyContext(own, own, cx, res.Context)
	return false, nil
}

// dispatchedElement is `self` for a method: the dispatched element.
func (c *composer) dispatchedElement(n *html.Node, r *region) *sessel.Element {
	d := c.sesselHost().Document(r.path)
	if d == nil || r.snapRoot == nil || d.Root != r.snapRoot {
		d = c.selfElement().Doc
	}
	return sessel.Queried(n, d)
}

// contextDict is the Context object at sc: every visible name and request.
func (c *composer) contextDict(sc *scope) *sessel.Dict {
	names, vals := sc.visible()
	cx := sessel.NewContext(c.sesselRequest())
	for _, n := range names {
		cx.Set(n, vals[n])
	}
	return cx
}

// applyContext copies Context entries an implementation added or changed
// (from the Context dict cx, or reported by a JavaScript runtime) into to.
func (c *composer) applyContext(to, before *scope, cx *sessel.Dict, extra map[string]sessel.Value) {
	for _, k := range cx.Keys() {
		if k == "request" {
			continue
		}
		v := cx.Lookup(k)
		if old, ok := before.lookup(k); ok && sessel.Equal(old, v) {
			continue
		}
		to.set(k, v)
	}
	for k, v := range extra {
		if k != "request" {
			to.set(k, v)
		}
	}
}

// include resolves <p:include selector resource> (R-COMP-80..86): the
// selector is evaluated in every candidate document's stored markup
// (documents matching the resource globs, or the whole site graph with the
// Request Document); exactly one match replaces the element and is
// composed in place, 0 matches fail the request 404, several 500.
func (c *composer) include(n *html.Node, r *region, own *scope) error {
	src, ok := html5Attr(n, "selector")
	if !ok || strings.TrimSpace(src) == "" {
		return compositionError("<%s> needs a selector attribute", n.Data)
	}
	sel, err := c.compileSelector(src)
	if err != nil {
		return compositionError("<%s selector=%q>: %v", n.Data, src, err)
	}
	var globs []string
	if res, ok := html5Attr(n, "resource"); ok {
		for _, g := range strings.Fields(res) {
			if !strings.HasPrefix(g, "/") {
				g = path.Join(path.Dir(c.docPath), g)
			}
			globs = append(globs, g)
		}
	}
	type hit struct {
		path string
		node *html.Node
		req  bool
	}
	var hits []hit
	for _, p := range c.snap.Paths {
		if len(globs) > 0 && !matchesAny(globs, p) {
			continue
		}
		pd := c.snap.Docs[p]
		if pd == nil {
			continue
		}
		for _, m := range sel.MatchAll(pd.Root) {
			hits = append(hits, hit{path: p, node: m})
		}
		if len(hits) > 1 {
			break
		}
	}
	if len(globs) == 0 && len(hits) < 2 {
		if rd := c.requestRegion(); rd != nil {
			for _, m := range sel.MatchAll(rd.root) {
				hits = append(hits, hit{node: m, req: true})
			}
		}
	}
	switch {
	case len(hits) == 0:
		return errdoc.New(http.StatusNotFound, KindIncludeMissing, "<%s selector=%q> matches nothing", n.Data, src)
	case len(hits) > 1:
		return errdoc.New(http.StatusInternalServerError, KindIncludeAmbig, "<%s selector=%q> matches more than one element", n.Data, src)
	}
	h := hits[0]
	var reg *region
	if h.req {
		reg = c.reqDoc
		c.private = true // the Request Document is per requester (R-COMP-121)
	} else if reg, err = docRegion(c.ctx, c.site, c.snap, h.path); err != nil {
		return err
	}
	if reg == nil {
		return errdoc.New(http.StatusNotFound, KindIncludeMissing, "<%s selector=%q> matches nothing", n.Data, src)
	}
	node := reg.node(h.node)
	sp, ok := reg.spans.By[node]
	if node == nil || !ok {
		return compositionError("<%s selector=%q>: the matched element has no source", n.Data, src)
	}
	// Directives inside run in the including page's Context, prefixes
	// resolving with the origin's declarations first (R-COMP-84).
	return c.element(node, sp, reg, own.withDeclarationsOf(node))
}

// stamp emits the Context values named by <p:stamp name…> (R-COMP-70..73):
// none → removed, one → that value, several → a list, in attribute order.
func (c *composer) stamp(n *html.Node, r *region, own *scope) error {
	var vals sessel.List
	for _, a := range n.Attr {
		if a.Namespace != "" || a.Key == "xmlns" || strings.HasPrefix(a.Key, "xmlns:") {
			continue
		}
		if v, ok := own.lookup(a.Key); ok && v != nil {
			vals = append(vals, v)
		}
	}
	switch len(vals) {
	case 0:
		return nil
	case 1:
		return c.emitValue(vals[0], contextOf(n, r), r, own)
	}
	return c.emitValue(vals, contextOf(n, r), r, own)
}

func matchesAny(globs []string, p string) bool {
	for _, g := range globs {
		if authz.GlobMatch(g, p) {
			return true
		}
	}
	return false
}

// html5Attr reads an attribute of a directive element.
func html5Attr(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}
