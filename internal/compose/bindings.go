package compose

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/liquid"
	"github.com/sky-valley/pagelike/internal/query"
	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/sessel"
)

// Bindings (docs/spec/composing.md §6–§8). Values live in the Context as
// Sessel values (the canonical value model); Liquid sees them converted
// (R-COMP-52).

// resourceBinding evaluates an r: selector against every stored document
// of the site, in ascending path order, and then the Request Document; the
// value is the list of all matches (R-COMP-31). Authorization never
// filters what a binding reads (R-COMP-32).
func (c *composer) resourceBinding(name, src string) (sessel.Value, error) {
	sel, err := c.compileSelector(src)
	if err != nil {
		return nil, compositionError("r:%s: %v", name, err)
	}
	h := c.sesselHost()
	out := sessel.List{}
	for _, p := range c.snap.Paths {
		pd := c.snap.Docs[p]
		if pd == nil {
			continue
		}
		matches := sel.MatchAll(pd.Root)
		if len(matches) == 0 {
			continue
		}
		d := h.Document(p)
		if d == nil {
			d = &sessel.Document{Path: p, Type: pd.Type, Root: pd.Root}
		}
		for _, m := range matches {
			out = append(out, sessel.Queried(m, d))
		}
	}
	rd := c.requestRegion()
	if rd != nil {
		for _, m := range sel.MatchAll(rd.root) {
			c.private = true // any fragment of the Request Document (R-COMP-121)
			out = append(out, sessel.Queried(m, c.reqSDoc))
		}
	}
	return out, nil
}

// compileSelector compiles a composition selector: PageLove extensions,
// selector functions expanded over the site graph (R-COMP-178), :isa()
// over the schema hierarchy.
func (c *composer) compileSelector(src string) (*selector.Selector, error) {
	if hasSelectorFunction(src) {
		var err error
		if src, err = selector.ExpandFunctions(src, &siteFuncs{snap: c.snap}); err != nil {
			return nil, err
		}
	}
	return selector.CompileWith(src, selector.ExtOptions{IsA: c.isA})
}

// isA reports whether itemtype t is target or a schema descendant of it
// (R-COMP-177); with no schema declaring t nothing matches.
func (c *composer) isA(t, target string) bool {
	h := c.sesselHost()
	seen := map[string]bool{}
	for cur := t; cur != "" && !seen[cur]; {
		seen[cur] = true
		cls, err := h.Class(c.ctx, cur)
		if err != nil || cls == nil {
			return false
		}
		if cur == target {
			return true
		}
		cur = cls.Parent()
	}
	return false
}

// sesselBinding evaluates an e: expression (R-COMP-35..38): self is the
// root element of the requested document (R-SESSEL-1), earlier bindings
// of the element and its ancestors are bare names and Context entries,
// request is the request object, ${…} queries the whole site.
func (c *composer) sesselBinding(name, src string, sc *scope) (sessel.Value, error) {
	names, vals := sc.visible()
	cx := sessel.NewContext(c.sesselRequest())
	for _, n := range names {
		cx.Set(n, vals[n])
	}
	self := c.selfElement()
	env := &sessel.Env{Host: c.sesselHost(), Context: cx, Request: c.sesselRequest(), Vars: vals, Budget: c.budget.Sessel()}
	if self != nil {
		env.Self, env.HasSelf, env.Document = self, true, self
	}
	v, err := sessel.Eval(c.ctx, src, env)
	if readsIdentity(src) {
		c.private = true
	}
	if err != nil {
		return nil, sesselError("e:"+name, err)
	}
	return v, nil
}

// jsBinding evaluates a j: expression through the JavaScript runtime.
func (c *composer) jsBinding(n *html.Node, name, src string, sc *scope) (sessel.Value, error) {
	js := c.eng.js()
	if js == nil {
		return nil, errNoJS("the j:" + name + " binding")
	}
	names, vals := sc.visible()
	res, err := js.Binding(c.ctx, &JSBinding{Name: name, Source: src, DocPath: c.docPath, Host: n, Names: names, Values: vals, Request: c.sesselRequest()})
	if err != nil {
		return nil, wrapError("j:"+name, err)
	}
	if res.Private {
		c.private = true
	}
	return res.Value, nil
}

// sesselHost is the read-only Sessel host over the snapshot: whole-site
// selector scope in path order, schema classes (query.SetClassSource).
func (c *composer) sesselHost() *query.Host {
	if c.host == nil {
		c.host = query.NewHost(c.site, c.snap)
	}
	return c.host
}

// selfElement is the requested document's root as a queried element.
func (c *composer) selfElement() *sessel.Element {
	if c.self != nil {
		return c.self
	}
	d := c.sesselHost().Document(c.docPath)
	if d == nil || d.Root != c.doc.snapRoot {
		d = &sessel.Document{Path: c.docPath, Root: c.doc.root}
		if c.doc.xml {
			d.Type = "application/xml"
		}
	}
	c.self = d.Element()
	return c.self
}

func (c *composer) liquidRequest() *liquid.Request {
	if c.lreq == nil {
		c.lreq = c.req.liquidRequest(c.snap)
	}
	return c.lreq
}

func (c *composer) sesselRequest() *sessel.Dict {
	if c.sreq == nil {
		c.sreq = c.req.sesselRequest(c.snap)
	}
	return c.sreq
}

// requestRegion builds the Request Document once per composition.
func (c *composer) requestRegion() *region {
	if c.reqDoc == nil {
		c.reqDoc = requestRegion(c.req, c.snap)
		if c.reqDoc != nil {
			c.reqSDoc = &sessel.Document{Path: "", Type: "text/html", Root: c.reqDoc.root}
		}
	}
	return c.reqDoc
}

// liquidVars are the names a template sees: request, then the Context
// names visible at the host (a binding named request shadows it).
func (c *composer) liquidVars(sc *scope) map[string]any {
	names, vals := sc.visible()
	m := make(map[string]any, len(names)+1)
	m["request"] = c.liquidRequest()
	for _, n := range names {
		m[n] = toLiquid(vals[n])
	}
	return m
}

// toLiquid converts a Sessel value for templates (R-LIQ-59): lists become
// arrays, dictionaries ordered hashes, elements microdata items.
func toLiquid(v sessel.Value) any {
	switch x := v.(type) {
	case nil, bool, float64, string:
		return x
	case int64:
		return int(x)
	case sessel.List:
		out := make([]any, len(x))
		for i, it := range x {
			out[i] = toLiquid(it)
		}
		return out
	case *sessel.Dict:
		h := liquid.NewHash()
		for _, k := range x.Keys() {
			h.Set(k, toLiquid(x.Lookup(k)))
		}
		return h
	case *sessel.Element:
		path := ""
		if x.Doc != nil {
			path = x.Doc.Path
		}
		return liquid.NewItem(path, x.Node)
	}
	return sessel.TextOf(v)
}

// hasSelectorFunction reports whether a selector uses count(), text-of(),
// value-of() or attr-of().
func hasSelectorFunction(s string) bool {
	return strings.Contains(s, "count(") || strings.Contains(s, "text-of(") ||
		strings.Contains(s, "value-of(") || strings.Contains(s, "attr-of(")
}
