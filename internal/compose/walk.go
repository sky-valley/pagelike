package compose

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/sky-valley/pagelike/internal/budget"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/liquid"
	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/query"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

// composer is the state of one composition. It walks regions in document
// (pre-)order and writes the composed markup to out: bytes of a region that
// no directive touches are copied from the region's source, directive
// elements are replaced by their results (which are walked in turn), and
// the start tags of kept elements lose only their directive attributes.
type composer struct {
	eng     *Engine
	ctx     context.Context
	site    *site.Site
	snap    *site.Snapshot
	req     *Request
	doc     *region // the requested document
	docPath string
	xml     bool
	create  bool // composing a template for storage (options.create)
	track   bool // record provenance anchors (selector writes)
	budget  *budget.Request

	out        strings.Builder
	anchors    []anchor
	pagers     []pager
	trans      []transRec
	private    bool
	dispatches int

	lreq     *liquid.Request
	sreq     *sessel.Dict
	host     *query.Host
	self     *sessel.Element
	reqDoc   *region
	reqSDoc  *sessel.Document
	sessions map[string]string // transient copies of this session for the document
	setID    string            // id for the next element's start tag (@key instances)
}

// dispatch charges one unit of the composition budget (R-COMP-27).
func (c *composer) dispatch() error {
	c.dispatches++
	if c.dispatches > MaxDispatches {
		return budgetError()
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (c *composer) run() error {
	sc := &scope{}
	pos, err := c.walk(c.doc.root, c.doc, sc, 0)
	if err != nil {
		return err
	}
	c.out.WriteString(c.doc.text[pos:])
	return nil
}

// walk composes the children of parent (a node of r's tree), copying r's
// source from offset pos, and returns the offset where it stopped.
func (c *composer) walk(parent *html.Node, r *region, sc *scope, pos int) (int, error) {
	for ch := parent.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type != html.ElementNode {
			continue
		}
		sp, ok := r.spans.By[ch]
		if !ok {
			// An element the parser implied (html, head, body, tbody,
			// colgroup): its children are in the source around it.
			var err error
			if pos, err = c.walk(ch, r, sc, pos); err != nil {
				return 0, err
			}
			continue
		}
		if sp.Start < pos {
			continue // defensive: walkable regions never overlap
		}
		c.out.WriteString(r.text[pos:sp.Start])
		if err := c.element(ch, sp, r, sc); err != nil {
			return 0, err
		}
		pos = sp.End
	}
	return pos, nil
}

// anchor records the provenance of an element about to be written (only
// when composing for a selector write, the one consumer).
func (c *composer) anchor(n *html.Node, r *region) {
	if !c.track || r.kind == provGenerated || r.path == "" {
		return
	}
	c.anchors = append(c.anchors, anchor{off: c.out.Len(), path: r.path, node: n, root: r.root})
}

// contextOf is the element fragment markup replacing n is parsed in.
func contextOf(n *html.Node, r *region) *html.Node {
	if n.Parent != nil && n.Parent.Type == html.ElementNode {
		return n.Parent
	}
	return r.context
}

// directiveElement reports whether n is written as a prefixed element that
// composition must dispatch (R-COMP-25 step 1, R-COMP-13).
func (c *composer) directiveElement(n *html.Node, r *region, sc *scope) (prefix, local string, ok bool) {
	if r.xml {
		prefix, local, ok = splitName(n.Data)
		if !ok || neverDispatched(prefix) {
			return "", "", false
		}
		// XML documents use namespaces for their own vocabularies (Atom,
		// RSS extensions): only Pagelove namespaces and schema types
		// dispatch there.
		uri, bound := sc.resolve(prefix)
		if !bound || !(isPageloveNS(uri) || c.eng.methods().HasSchema(c.snap, uri)) {
			return "", "", false
		}
		return prefix, local, true
	}
	if n.Namespace != "" || !dom.IsPrefixed(n) {
		return "", "", false
	}
	prefix, local, ok = splitName(n.Data)
	if !ok || neverDispatched(prefix) {
		return "", "", false
	}
	// An element whose prefix no xmlns declaration binds is an ordinary
	// element, as in the attribute form (live 2026-09-29, superseding the
	// documented 500 of R-COMP-13 row 1).
	if _, bound := sc.resolve(prefix); !bound {
		return "", "", false
	}
	return prefix, local, true
}

// element composes one element of region r whose source is sp.
func (c *composer) element(n *html.Node, sp dom.Span, r *region, parent *scope) error {
	if !r.xml && n.Namespace == "" && n.DataAtom == atom.Template {
		// <template> contents are inert (no selector reaches them); they
		// are served as written.
		c.setID = ""
		c.anchor(n, r)
		c.out.WriteString(r.text[sp.Start:sp.End])
		return nil
	}
	setID := c.setID
	c.setID = ""
	own := parent.child()
	own.declare(n)
	if prefix, local, ok := c.directiveElement(n, r, own); ok {
		if err := c.dispatch(); err != nil {
			return err
		}
		return c.elementDirective(n, r, own, prefix, local)
	}
	return c.plain(n, sp, r, own, setID)
}

// attrDirective is one recognised directive attribute.
type attrDirective struct {
	key, local, uri, val string
}

// plain composes an ordinary element: its directive attributes are
// dispatched (resource bindings, then the others in source order, then
// the template), its start tag written without them, then its content.
// setID, when not empty, becomes the element's id: a stamped instance of a
// schema with a @key property is addressable as #<key> in the page
// (R-COMP-76).
func (c *composer) plain(n *html.Node, sp dom.Span, r *region, own *scope, setID string) error {
	edit := &tagEdit{}
	if setID != "" {
		edit.set = append(edit.set, [2]string{"id", setID})
	}
	var rbind, others []attrDirective
	var tpl, pag *attrDirective
	transient := false
	for _, a := range n.Attr {
		if a.Namespace != "" || a.Key == "xmlns" {
			continue
		}
		if strings.HasPrefix(a.Key, "xmlns:") {
			if !r.xml && !c.create {
				edit.dropAttr(a.Key) // every xmlns:* is stripped from HTML (R-COMP-14)
			}
			continue
		}
		prefix, local, ok := splitName(a.Key)
		if !ok || neverDispatched(prefix) {
			continue
		}
		uri, bound := own.resolve(prefix)
		if !bound {
			continue // unbound prefix: left exactly as written (R-COMP-13)
		}
		d := attrDirective{key: a.Key, local: local, uri: uri, val: a.Val}
		if c.create && (local == "transient" || (uri == NSPagelove && local == "paginate")) {
			continue // read-time directives of the created document
		}
		edit.dropAttr(a.Key)
		if local == "transient" {
			transient = true // the transient marker is never dispatched (R-COMP-65)
			continue
		}
		switch uri {
		case NSPagelove:
			switch local {
			case "template":
				tpl = &d
			case "paginate":
				pag = &d
			}
			// other 1.0 attributes are stripped (R-COMP-13 row 3)
		case NSBindingCSS, NSLegacyResource:
			rbind = append(rbind, d)
		default:
			others = append(others, d)
		}
	}

	if transient {
		c.private = true // R-COMP-114
		if r == c.doc && r.kind != provGenerated {
			return c.transientElement(n, sp, r, own, edit, rbind, others, tpl, pag)
		}
	}
	replaced, err := c.dispatchAttrs(n, r, own, rbind, others)
	if err != nil || replaced {
		return err
	}
	return c.emitKept(n, sp, r, own, edit, tpl, pag)
}

// dispatchAttrs runs the binding and method attributes of n in order. It
// reports whether a method replaced the whole host (R-COMP-63).
func (c *composer) dispatchAttrs(n *html.Node, r *region, own *scope, rbind, others []attrDirective) (bool, error) {
	for _, d := range rbind {
		if err := c.dispatch(); err != nil {
			return false, err
		}
		v, err := c.resourceBinding(d.local, d.val)
		if err != nil {
			return false, err
		}
		own.set(d.local, v)
	}
	for _, d := range others {
		switch d.uri {
		case NSSessel:
			if err := c.dispatch(); err != nil {
				return false, err
			}
			v, err := c.sesselBinding(d.local, d.val, own)
			if err != nil {
				return false, err
			}
			own.set(d.local, v)
		case NSJavaScript:
			if err := c.dispatch(); err != nil {
				return false, err
			}
			v, err := c.jsBinding(n, d.local, d.val, own)
			if err != nil {
				return false, err
			}
			own.set(d.local, v)
		default:
			if !c.eng.methods().HasSchema(c.snap, d.uri) {
				continue // bound to an unknown URI: stripped silently (R-COMP-13 row 2)
			}
			if err := c.dispatch(); err != nil {
				return false, err
			}
			replaced, err := c.methodAttribute(n, r, own, d)
			if err != nil || replaced {
				return replaced, err
			}
		}
	}
	return false, nil
}

// emitKept writes an element composition keeps: its start tag (directive
// attributes removed), then its content — the template's rendered output
// or its composed children — and its end tag.
func (c *composer) emitKept(n *html.Node, sp dom.Span, r *region, own *scope, edit *tagEdit, tpl, pag *attrDirective) error {
	start := r.text[sp.Start:sp.InnerStart]
	if pag != nil {
		p, err := parsePaginate(pag.val)
		if err != nil {
			return err
		}
		p.off, p.id = c.out.Len(), attr(n, "id")
		c.pagers = append(c.pagers, p)
	}
	c.anchor(n, r)
	c.out.WriteString(rewriteStartTag(start, edit, r.xml))
	if sp.NoContent {
		return nil
	}
	if tpl != nil && !r.tplOut {
		if err := c.dispatch(); err != nil {
			return err
		}
		if err := c.template(n, sp, r, own, tpl.val); err != nil {
			return err
		}
	} else {
		pos, err := c.walk(n, r, own, sp.InnerStart)
		if err != nil {
			return err
		}
		if pos < sp.InnerEnd {
			c.out.WriteString(r.text[pos:sp.InnerEnd])
		}
	}
	c.out.WriteString(r.text[sp.InnerEnd:sp.End])
	return nil
}

// template renders a p:template host's raw inner markup with Liquid and
// composes the output in place of the host's children (R-COMP-50..53).
func (c *composer) template(n *html.Node, sp dom.Span, r *region, own *scope, engineName string) error {
	if !strings.EqualFold(strings.TrimSpace(engineName), "text/liquid") {
		return errdoc.New(http.StatusInternalServerError, KindTemplateEngine,
			"unsupported template engine %q (only text/liquid is supported)", engineName)
	}
	host := liquid.HostOf(n)
	src, err := liquid.SourceFromHTML(r.text[sp.InnerStart:sp.InnerEnd], host)
	if err != nil {
		return liquidError(err)
	}
	out, err := c.eng.Liquid.RenderFor(c.ctx, host, src, c.liquidVars(own), c.budget.Liquid())
	if err != nil {
		return liquidError(err)
	}
	if c.liquidRequest().Private() {
		c.private = true
	}
	if !r.xml && !liquid.RawTextHost(host.Tag) {
		// PageLove parses the output and serializes it in its form, so a
		// "&" or "&#39;" in it comes out as "&amp;" or "'" (live 2026-09-29).
		if d, err := dom.Parse([]byte(out)); err == nil {
			out = string(dom.Render(d))
		}
	}
	fr, err := fragmentRegion(out, n, r.xml, own.inScope())
	if err != nil {
		return compositionError("template output of <%s> does not parse: %v", n.Data, err)
	}
	fr.tplOut = true
	if err := ensureWalkable(fr); err != nil {
		return err
	}
	pos, err := c.walk(fr.root, fr, own, 0)
	if err != nil {
		return err
	}
	c.out.WriteString(fr.text[pos:])
	return nil
}

// emitValue writes a directive's result (R-COMP-62): elements and
// instances are spliced and composed, lists element by element, scalars as
// escaped text, null nothing.
func (c *composer) emitValue(v sessel.Value, ctxNode *html.Node, r *region, sc *scope) error {
	switch x := v.(type) {
	case nil:
		return nil
	case sessel.List:
		for _, it := range x {
			if err := c.emitValue(it, ctxNode, r, sc); err != nil {
				return err
			}
		}
		return nil
	case *sessel.Element:
		return c.emitElementValue(x, ctxNode, r, sc)
	}
	c.out.WriteString(escapeText(sessel.TextOf(v)))
	return nil
}

// emitElementValue splices an element value. A queried element keeps its
// origin: its stored markup is copied from its document and it stays
// projected (writable through the page, R-COMP-73). A constructed element
// is serialized and its markup composed as generated content.
func (c *composer) emitElementValue(el *sessel.Element, ctxNode *html.Node, r *region, sc *scope) error {
	id := instanceKey(el)
	if !el.Mutable && el.Doc != nil {
		if reg, err := c.regionOf(el.Doc); err != nil {
			return err
		} else if reg != nil {
			if node := reg.node(el.Node); node != nil {
				if sp, ok := reg.spans.By[node]; ok {
					if reg.reqDoc {
						c.private = true
					}
					if id != "" && attr(node, "id") != id {
						c.setID = id // applied to this element's start tag by plain
					}
					return c.element(node, sp, reg, sc.withDeclarationsOf(node))
				}
			}
		}
	}
	n := el.Node
	if id != "" {
		n = dom.Clone(n)
		dom.SetAttr(n, "id", id)
	}
	return c.emitMarkup(dom.OuterHTML(n), ctxNode, r, sc)
}

// emitMarkup parses markup as the content of ctxNode and composes it as
// generated content.
func (c *composer) emitMarkup(markup string, ctxNode *html.Node, r *region, sc *scope) error {
	fr, err := fragmentRegion(markup, ctxNode, r.xml, sc.inScope())
	if err != nil {
		return compositionError("result markup does not parse: %v", err)
	}
	if err := ensureWalkable(fr); err != nil {
		return err
	}
	pos, err := c.walk(fr.root, fr, sc, 0)
	if err != nil {
		return err
	}
	c.out.WriteString(fr.text[pos:])
	return nil
}

// instanceKey is the @key value of a schema instance, emitted as the
// spliced root's id (R-COMP-76), or "".
func instanceKey(el *sessel.Element) string {
	if el.Class == nil {
		return ""
	}
	var props []*sessel.Property
	if b, ok := el.Class.(interface{ AllProperties() []*sessel.Property }); ok {
		props = b.AllProperties()
	} else if b, ok := el.Class.(*sessel.BasicClass); ok {
		props = b.Props
	}
	for _, p := range props {
		if !p.Key {
			continue
		}
		if v := propText(el.Node, p.Name); v != "" {
			return v
		}
	}
	return ""
}

// propText returns the first microdata value of property name on the item
// rooted at n (not descending into nested items).
func propText(n *html.Node, name string) string {
	var found string
	var walk func(*html.Node) bool
	walk = func(x *html.Node) bool {
		for ch := x.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type != html.ElementNode {
				continue
			}
			for _, t := range strings.Fields(attr(ch, "itemprop")) {
				if t == name {
					found = strings.TrimSpace(microdata.Value(ch))
					return true
				}
			}
			if dom.HasAttr(ch, "itemscope") {
				continue
			}
			if walk(ch) {
				return true
			}
		}
		return false
	}
	walk(n)
	return found
}

// regionOf returns the region a queried element's document is in.
func (c *composer) regionOf(d *sessel.Document) (*region, error) {
	if c.reqDoc != nil && d.Root == c.reqDoc.root {
		return c.reqDoc, nil
	}
	if d.Path == "" {
		return nil, nil
	}
	if d.Path == c.docPath && c.doc.snapRoot == d.Root {
		return c.doc, nil
	}
	return docRegion(c.ctx, c.site, c.snap, d.Path)
}

// parsePaginate validates a p:paginate value (R-COMP-134).
func parsePaginate(v string) (pager, error) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return pager{}, errdoc.New(http.StatusUnprocessableEntity, KindPagination, "p:paginate must be a positive integer, not %q", v)
	}
	return pager{length: n}, nil
}
