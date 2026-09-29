package schema

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/sessel"
)

// Validate is the engine Validate hook: the schema and shape pipeline of
// R-MOD-15 for every mutating operation on either plane (R-MOD-13).
//
// Live PageLove stores a WebDAV PUT without any schema or shape check
// (2026-09-29). pagelike keeps validating it (keep-documented-security,
// decisions-2026-09-29/serialization.md, "Integration"): R-PROTO-112 and
// R-MOD-65 document the check, and apps rely on it, such as the shop's
// closed Order shape, which keeps a <script> out of orders its worker
// writes over WebDAV.
//
//  0. schema load errors (cyclic parents, invalid references, @validate
//     compile failures) on affected instances → 422;
//  1. (no computed-property guard: computed properties are validated like
//     any other, live 2026-09-29);
//  2. defaults (static, Sessel, JavaScript, @key auto default);
//  3. @write chains, child-first;
//  4. cardinality, type and enum, all collected into one 422 (a
//     Cardinality problems item for selector writes);
//  5. property-level @validate (most-derived);
//  6. schema-level @validate (every level, ancestor-first);
//  7. group constraints;
//  8. reference integrity and uniqueness against the committed state plus
//     this write;
//  9. ShapeConstraints on the final document (422, 409 for DELETE);
//  11. referential actions: restrict → 409; cascades are planned here and
//     applied by AfterWrite in the same transaction.
//
// The registry is the one of the snapshot the write started from, so a
// write that carries a declaration is not validated against it (R-MOD-4).
// Defaults and @write results are stored: w.After is mutated in place and,
// for whole-document PUTs, w.Op.Body is rewritten by splicing the changes
// into the request bytes.
func Validate(ctx context.Context, w *engine.WriteCtx) error {
	reg := For(w.Snap)
	if len(reg.order) == 0 && len(reg.shapes) == 0 {
		return nil
	}
	kind := classify(w)
	if kind == kindNone {
		return nil
	}
	p := &pass{ctx: ctx, w: w, op: w.Op, reg: reg, kind: kind}
	p.ev = newEvaluator(ctx, w.Site, w.Snap, reg)
	p.ev.webdav = w.Op.Plane == engine.Authoring
	return p.run()
}

// pass is one write's validation state.
type pass struct {
	ctx  context.Context
	w    *engine.WriteCtx
	op   *engine.Op
	reg  *Registry
	ev   *evaluator
	kind writeKind

	ct        string
	after     *html.Node // D′ (document node)
	doc       *srcDoc
	rewrite   bool                // the stored body is op.Body
	elems     map[*html.Node]bool // affected elements; nil = all
	instances []*html.Node        // affected governed instances, document order
	afterDoc  *sessel.Document    // provenance of D′ for Sessel
	prior     *sessel.Element     // pre-write document root
	docHTML   string              // cached serialization of D′
}

func (p *pass) run() error {
	w := p.w
	switch p.kind {
	case kindDocDelete:
		if w.Before == nil {
			return nil
		}
		return p.referential()
	case kindDocMove:
		return p.moveShapes()
	}
	p.after = w.After
	p.ct = "text/html"
	if w.Doc != nil {
		p.ct = w.Doc.ContentType
	} else if p.op.Method == "PUT" {
		p.ct = engine.ResolveContentType(p.op.Path, p.op.ContentType)
	}
	p.afterDoc = &sessel.Document{Path: p.op.Path, Type: p.ct, Root: p.after}
	if w.Before != nil {
		bd := &sessel.Document{Path: p.op.Path, Type: p.ct, Root: w.Before}
		p.prior = docElement(bd)
	}
	if p.kind == kindWhole {
		if p.op.Method == "PUT" {
			p.rewrite = true
			p.doc = newSrcDoc(p.op.Path, p.ct, p.op.Body, p.after)
		} else {
			p.doc = newSrcDoc(p.op.Path, p.ct, nil, p.after)
		}
	} else {
		p.doc = newSrcDoc(p.op.Path, p.ct, nil, p.after)
		p.elems = affectedElements(w)
	}
	p.instances = p.reg.affectedInstances(p.after, p.elems)

	stages := []func() error{
		p.loadErrors, p.defaults, p.writes, p.structural,
		p.propertyValidators, p.schemaValidators, p.groups, p.hostWide, p.shapes, p.referential,
	}
	for _, st := range stages {
		if err := st(); err != nil {
			return err
		}
	}
	if p.rewrite && p.doc.changed {
		p.op.Body = p.doc.bytes()
	}
	return nil
}

func (p *pass) schemaOf(inst *html.Node) *Schema { return p.reg.schemas[itemType(inst)] }

// eachProp calls fn for every effective property of every affected
// instance whose schema loaded.
func (p *pass) eachProp(fn func(inst *html.Node, s *Schema, ep *EffectiveProp) error) error {
	for _, inst := range p.instances {
		s := p.schemaOf(inst)
		if s == nil || s.eff == nil {
			continue
		}
		for _, pn := range s.eff.propOrder {
			if err := fn(inst, s, s.eff.props[pn]); err != nil {
				return err
			}
		}
	}
	return nil
}

func dedupe(vs []Violation) []Violation {
	seen := map[string]bool{}
	var out []Violation
	for _, v := range vs {
		k := v.Check + "\x00" + v.Message
		if !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	return out
}

func unprocessable(vs []Violation) error {
	if len(vs) == 0 {
		return nil
	}
	return schemaError(http.StatusUnprocessableEntity, dedupe(vs))
}

// ---------------------------------------------------------------- stage 0

func (p *pass) loadErrors() error {
	var vs []Violation
	for _, inst := range p.instances {
		s := p.schemaOf(inst)
		if s.loadErr != "" {
			vs = append(vs, Violation{Check: CheckSchema, ItemType: s.URL, Message: fmt.Sprintf("[%s]: %s", s.URL, s.loadErr)})
			continue
		}
		vs = append(vs, s.eff.loadErrs...)
	}
	return unprocessable(vs)
}

// Stage 1, the computed-property guard, is gone: PageLove stores a value
// written to a @computed property and validates it like any other value
// (live 2026-09-29; R-MOD-44).

// ---------------------------------------------------------------- stage 2

func (p *pass) defaults() error {
	for i, inst := range p.instances {
		s := p.schemaOf(inst)
		if voidElements[inst.Data] && inst.Namespace == "" {
			continue // cannot hold property elements
		}
		var view map[string]any
		for _, pn := range s.eff.propOrder {
			ep := s.eff.props[pn]
			if ep.Decl.Computed != nil || len(propElements(inst, pn)) > 0 {
				continue
			}
			d := ep.Decl.Default
			if d == nil && !ep.AutoKey {
				continue
			}
			var nodes []*html.Node
			switch {
			case d == nil:
				nodes = []*html.Node{newMeta(pn, randomKey())}
			case d.Lang == LangStatic:
				nodes = []*html.Node{newMeta(pn, d.Source)}
			case d.Lang == LangSessel:
				v, err := p.ev.runSessel(d, sesselEnv{})
				if err != nil {
					return p.defaultFailure(s, pn, err)
				}
				if nodes, err = valueNodes(pn, v); err != nil {
					return p.defaultFailure(s, pn, err)
				}
			case d.Lang == LangJS:
				if view == nil {
					view = p.reg.instanceView(s.URL, inst, "")
				}
				orig := dom.OuterHTML(inst)
				res, err := p.ev.runJSSlot(d, &JSCall{Slot: JSSlotDefault, This: view, HasThis: true,
					Args: []any{map[string]any{"document_html": p.html()}}, Document: orig, DocumentWritable: true})
				if err != nil {
					return p.defaultFailure(s, pn, err)
				}
				if res.Document != "" && res.Document != orig {
					if n := p.replaceInstanceMarkup(inst, res.Document); n != nil {
						inst = n
						p.instances[i] = n
					}
				}
				if nodes, err = valueNodes(pn, fromJS(res.Value)); err != nil {
					return p.defaultFailure(s, pn, err)
				}
			default:
				return p.defaultFailure(s, pn, unknownLanguage(d))
			}
			for _, n := range nodes {
				setItemprop(n, pn)
				p.appendChild(inst, n)
			}
		}
	}
	return nil
}

func (p *pass) defaultFailure(s *Schema, pn string, err error) error {
	mk := func(f *BindingFailure) []Violation {
		return []Violation{{Check: CheckDefault, ItemType: s.URL, Property: pn, Failure: f,
			Message: fmt.Sprintf("[%s].%s: the default could not be computed: %s", s.URL, pn, f.Message)}}
	}
	if e, ok := p.ev.fatal(err, false, mk); ok {
		return e
	}
	if tr, ok := err.(*thrownResponse); ok {
		err = &BindingFailure{Language: URLSessel, Variant: "threw", Message: tr.Error()}
	}
	return schemaError(http.StatusUnprocessableEntity, mk(failureOf(err)))
}

var voidElements = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true}

// replaceInstanceMarkup applies a JavaScript default's writable `document`
// back onto the instance element (R-JS-11).
func (p *pass) replaceInstanceMarkup(inst *html.Node, markup string) *html.Node {
	if inst.Parent == nil {
		return nil
	}
	nodes, err := dom.ParseFragment(markup, inst.Parent)
	if err != nil {
		return nil
	}
	el := firstElement(nodes)
	if el == nil {
		return nil
	}
	p.replace(inst, []*html.Node{el})
	return el
}

func firstElement(nodes []*html.Node) *html.Node {
	for _, n := range nodes {
		if n.Type == html.ElementNode {
			return n
		}
	}
	return nil
}

// valueNodes converts a computed value into property elements (R-MOD-28):
// scalars as <meta content>, elements as themselves, lists item by item,
// null as nothing.
func valueNodes(name string, v sessel.Value) ([]*html.Node, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case sessel.List:
		var out []*html.Node
		for _, it := range x {
			ns, err := valueNodes(name, it)
			if err != nil {
				return nil, err
			}
			out = append(out, ns...)
		}
		return out, nil
	case *sessel.Element:
		n := x.Node
		if n.Parent != nil || !x.Mutable {
			n = dom.Clone(n)
		}
		return []*html.Node{n}, nil
	case *sessel.Dict:
		if t, _ := x.Lookup("$type").(string); t == "element" {
			if h, ok := x.Lookup("$html").(string); ok {
				if n := parseElement(h); n != nil {
					return []*html.Node{n}, nil
				}
			}
		}
		return nil, &BindingFailure{Variant: "return-type", Message: "a Dictionary cannot be a property value"}
	case bool, int64, float64, string:
		return []*html.Node{newMeta(name, sessel.TextOf(x))}, nil
	}
	if s := sessel.TextOf(v); s != "" {
		return []*html.Node{newMeta(name, s)}, nil
	}
	return nil, &BindingFailure{Variant: "return-type", Message: fmt.Sprintf("a %s cannot be a property value", sessel.TypeName(v))}
}

// parseElement parses markup as a body fragment and returns its first
// element.
func parseElement(markup string) *html.Node {
	nodes, err := dom.ParseBodyFragment(markup)
	if err != nil {
		return nil
	}
	if el := firstElement(nodes); el != nil {
		el.Parent, el.PrevSibling, el.NextSibling = nil, nil, nil
		return el
	}
	return nil
}

// ---------------------------------------------------------------- stage 3

func (p *pass) writes() error {
	// Inner instances first, so an outer resolver works on their results.
	for i := len(p.instances) - 1; i >= 0; i-- {
		inst := p.instances[i]
		if inst == nil || !attached(p.after, inst) {
			continue
		}
		s := p.schemaOf(inst)
		for _, pn := range s.eff.propOrder {
			ep := s.eff.props[pn]
			for _, slot := range ep.WriteChain {
				elems := propElements(inst, pn)
				if len(elems) == 0 {
					break
				}
				if err := p.writeStage(s, ep, inst, slot, elems); err != nil {
					return err
				}
			}
		}
	}
	live := p.instances[:0]
	for _, n := range p.instances {
		if n != nil && attached(p.after, n) {
			live = append(live, n)
		}
	}
	p.instances = live
	return nil
}

func (p *pass) writeFailure(s *Schema, pn string, err error) error {
	mk := func(f *BindingFailure) []Violation {
		return []Violation{{Check: CheckWrite, ItemType: s.URL, Property: pn, Failure: f,
			Message: fmt.Sprintf("[%s].%s: @write failed: %s", s.URL, pn, f.Message)}}
	}
	if e, ok := p.ev.fatal(err, true, mk); ok {
		return e
	}
	return schemaError(http.StatusInternalServerError, mk(failureOf(err)))
}

func (p *pass) writeStage(s *Schema, ep *EffectiveProp, inst *html.Node, slot *Slot, elems []*html.Node) error {
	pn := ep.Name
	switch slot.Lang {
	case LangSessel:
		if slot.compile() != nil {
			return nil // a resolver that does not compile is skipped (R-MOD-48)
		}
		clones, cloneOf := cloneAll(elems)
		self := make(sessel.List, len(clones))
		for i, c := range clones {
			self[i] = &sessel.Element{Node: c, Mutable: true}
		}
		v, err := p.ev.runResolver(slot, sesselEnv{self: self, hasSelf: true, prior: p.prior, doc: docElement(p.afterDoc)})
		if err != nil {
			return p.writeFailure(s, pn, err)
		}
		nodes, err := resultElements(v)
		if err != nil {
			return p.writeFailure(s, pn, err)
		}
		for _, n := range nodes {
			setItemprop(n, pn)
		}
		p.replaceValues(elems, nodes, cloneOf)
		return nil
	case LangJS:
		res, err := p.ev.runJSSlot(slot, &JSCall{Slot: JSSlotWrite, Args: []any{pipelineValue(ep.Decl.Cardinality, elems)}, Document: p.html()})
		if err != nil {
			return p.writeFailure(s, pn, err)
		}
		if !isMulti(ep.Decl.Cardinality) {
			elems = elems[:1] // only the first value flowed through (R-MOD-20)
		}
		if err := p.writeBack(pn, elems, res.Value); err != nil {
			return p.writeFailure(s, pn, err)
		}
		return nil
	}
	return p.writeFailure(s, pn, unknownLanguage(slot))
}

// cloneAll copies elements for a resolver's working list and maps every
// original node to its copy (to follow nested instances through the
// replacement).
func cloneAll(elems []*html.Node) ([]*html.Node, map[*html.Node]*html.Node) {
	m := map[*html.Node]*html.Node{}
	out := make([]*html.Node, len(elems))
	for i, e := range elems {
		c := dom.Clone(e)
		out[i] = c
		var pair func(a, b *html.Node)
		pair = func(a, b *html.Node) {
			m[a] = b
			for x, y := a.FirstChild, b.FirstChild; x != nil && y != nil; x, y = x.NextSibling, y.NextSibling {
				pair(x, y)
			}
		}
		pair(e, c)
	}
	return out, m
}

// resultElements checks a Sessel resolver result: an Element or a List of
// Elements (R-MOD-49); anything else fails the request (R-MOD-52).
func resultElements(v sessel.Value) ([]*html.Node, error) {
	var out []*html.Node
	add := func(x sessel.Value) error {
		el, ok := x.(*sessel.Element)
		if !ok {
			return &BindingFailure{Language: URLSessel, Variant: "return-type",
				Message: fmt.Sprintf("a resolver must return an Element or a List of Elements, not %s", sessel.TypeName(x))}
		}
		n := el.Node
		if n.Parent != nil || !el.Mutable {
			n = dom.Clone(n)
		}
		out = append(out, n)
		return nil
	}
	switch x := v.(type) {
	case sessel.List:
		for _, it := range x {
			if err := add(it); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	if err := add(v); err != nil {
		return nil, err
	}
	return out, nil
}

// replaceValues puts a resolver's result in place of the original value
// elements: the first result takes the first original's position, the rest
// follow it, the other originals are removed; an empty result removes the
// property (R-MOD-49).
func (p *pass) replaceValues(origs, nodes []*html.Node, cloneOf map[*html.Node]*html.Node) {
	if len(origs) == 0 {
		return
	}
	p.replace(origs[0], nodes)
	for _, o := range origs[1:] {
		p.replace(o, nil)
	}
	for i, inst := range p.instances {
		if inst == nil || attached(p.after, inst) {
			continue
		}
		if c, ok := cloneOf[inst]; ok && attached(p.after, c) {
			p.instances[i] = c
		} else {
			p.instances[i] = nil
		}
	}
}

// writeBack writes a JavaScript pipeline result into the value elements
// (R-MOD-49): strings into content/datetime/text, one per element for a
// list; an element result replaces; an empty list or null removes the
// property (pagelike decision).
func (p *pass) writeBack(pn string, elems []*html.Node, v any) error {
	var vals []any
	switch x := v.(type) {
	case nil:
		vals = nil
	case []any:
		vals = x
	default:
		vals = []any{x}
	}
	var nodes []*html.Node
	for i, it := range vals {
		if m, ok := it.(map[string]any); ok {
			if m["$type"] == "element" {
				h, _ := m["$html"].(string)
				if n := parseElement(h); n != nil {
					setItemprop(n, pn)
					nodes = append(nodes, n)
					continue
				}
			}
			return &BindingFailure{Language: URLJavaScript, Variant: "return-type", Message: "an object cannot be a property value"}
		}
		src := elems[len(elems)-1]
		if i < len(elems) {
			src = elems[i]
		}
		c := dom.Clone(src)
		if isItem(c) {
			return &BindingFailure{Language: URLJavaScript, Variant: "return-type", Message: "a nested item cannot take a scalar value"}
		}
		setValue(c, scalarText(it))
		nodes = append(nodes, c)
	}
	p.replaceValues(elems, nodes, nil)
	return nil
}

func scalarText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return fmt.Sprintf("%d", int64(x))
		}
		return sessel.FormatFloat(x)
	}
	return sessel.TextOf(sessel.FromGo(v))
}

// setValue writes v into the slot the element's microdata value comes from.
func setValue(n *html.Node, v string) {
	switch valueElementName(n) {
	case "meta":
		dom.SetAttr(n, "content", v)
	case "audio", "embed", "iframe", "img", "source", "track", "video":
		dom.SetAttr(n, "src", v)
	case "a", "area", "link":
		dom.SetAttr(n, "href", v)
	case "object":
		dom.SetAttr(n, "data", v)
	case "data", "meter":
		dom.SetAttr(n, "value", v)
	case "time":
		if dom.HasAttr(n, "datetime") {
			dom.SetAttr(n, "datetime", v)
			return
		}
		dom.SetTextContent(n, v)
	default:
		dom.SetTextContent(n, v)
	}
}

// replace and appendChild mutate D′ through the source tracker and keep
// the affected-element set in step.
func (p *pass) replace(orig *html.Node, nodes []*html.Node) {
	aff := p.elems != nil && p.elems[orig]
	p.doc.replace(orig, nodes)
	p.docHTML = ""
	if aff {
		for _, n := range nodes {
			p.markAffected(n)
		}
	}
}

func (p *pass) appendChild(parent, n *html.Node) {
	p.doc.appendChild(parent, n)
	p.docHTML = ""
	if p.elems != nil && p.elems[parent] {
		p.markAffected(n)
	}
}

func (p *pass) markAffected(n *html.Node) {
	dom.Walk(n, func(x *html.Node) bool {
		if x.Type == html.ElementNode {
			p.elems[x] = true
		}
		return true
	})
}

// html is the serialized document being written (JavaScript context).
func (p *pass) html() string {
	if p.docHTML == "" {
		p.docHTML = string(dom.Render(p.after))
	}
	return p.docHTML
}

// ---------------------------------------------------------------- stage 4

// structural checks cardinality, type and enum (live 2026-09-29: computed
// properties are checked like the others, and a property typed with a
// schema is not checked at all, neither its item type nor the nested
// item's own properties). A selector write whose first failure is a
// cardinality answers PageLove's Cardinality problems item
// ("<type> /<prop>: cardinality 1..1 violated: … (cardinality)"); other
// writes answer the SchemaViolation document.
func (p *pass) structural() error {
	var vs []Violation
	var cardProblems []string
	p.eachProp(func(inst *html.Node, s *Schema, ep *EffectiveProp) error {
		elems := propElements(inst, ep.Name)
		card := ep.Decl.Cardinality
		if !cardinalityOK(card, len(elems)) {
			detail := cardinalityDetail(card, len(elems))
			vs = append(vs, Violation{Check: CheckCardinality, ItemType: s.URL, Property: ep.Name,
				Message: fmt.Sprintf("[%s].%s: %s", s.URL, ep.Name, detail)})
			cardProblems = append(cardProblems, fmt.Sprintf("%s /%s: %s (cardinality)", s.URL, ep.Name, detail))
		}
		if p.reg.schemas[ep.Decl.Type] == nil {
			vs = append(vs, p.typeCheck(s, ep, elems)...)
		}
		return nil
	})
	if len(vs) > 0 && p.kind == kindSelector && vs[0].Check == CheckCardinality {
		return problemsError(http.StatusUnprocessableEntity, ProblemCardinality, cardProblems)
	}
	return unprocessable(vs)
}

// typeCheck validates each value against the property type (R-MOD-21..25).
func (p *pass) typeCheck(s *Schema, ep *EffectiveProp, elems []*html.Node) []Violation {
	t := ep.Decl.Type
	if t == "" {
		return nil
	}
	var vs []Violation
	pre := fmt.Sprintf("[%s].%s: ", s.URL, ep.Name)
	switch {
	case isPrimitive(t):
		for _, e := range elems {
			v := valueOf(e)
			if v.item {
				vs = append(vs, Violation{Check: CheckType, ItemType: s.URL, Property: ep.Name,
					Message: pre + "a nested item is not a valid " + t})
				continue
			}
			if !checkPrimitive(t, v.s) {
				vs = append(vs, Violation{Check: CheckType, ItemType: s.URL, Property: ep.Name, Value: strPtr(v.s),
					Message: fmt.Sprintf("%sValue %q is not a valid %s", pre, v.s, t)})
			}
		}
	case p.reg.enums[t] != nil:
		en := p.reg.enums[t]
		for _, e := range elems {
			v := valueOf(e)
			ok := false
			if !v.item {
				for _, a := range en.Values {
					if a == v.s {
						ok = true
						break
					}
				}
			}
			if !ok {
				shown := v.s
				if v.item {
					shown = "[item]"
				}
				vs = append(vs, Violation{Check: CheckEnum, ItemType: s.URL, Property: ep.Name, Value: strPtr(shown),
					Message: fmt.Sprintf("%sValue %q is not a valid %s (expected one of: %s)", pre, shown, t, quoteList(en.Values))})
			}
		}
	case p.reg.schemas[t] != nil:
		for _, e := range elems {
			if !isItem(e) {
				vs = append(vs, Violation{Check: CheckType, ItemType: s.URL, Property: ep.Name, Value: strPtr(valueOf(e).s),
					Message: fmt.Sprintf("%sexpected an item of type %s, found a plain value", pre, t)})
				continue
			}
			if it := itemType(e); !p.reg.IsA(it, t) || it == "" {
				vs = append(vs, Violation{Check: CheckType, ItemType: s.URL, Property: ep.Name,
					Message: fmt.Sprintf("%sexpected an item of type %s, found an item of type %q", pre, t, it)})
			}
		}
	}
	return vs
}

// ---------------------------------------------------------------- stage 5

func (p *pass) validateFailure(s *Schema, pn string, err error, responses bool) (*errdoc.Error, Violation) {
	mk := func(f *BindingFailure) []Violation {
		return []Violation{p.validateViolation(s, pn, f)}
	}
	if e, ok := p.ev.fatal(err, responses, mk); ok {
		return e, Violation{}
	}
	if tr, ok := err.(*thrownResponse); ok {
		err = &BindingFailure{Language: URLSessel, Variant: "threw", Message: tr.Error()}
	}
	return nil, p.validateViolation(s, pn, failureOf(err))
}

func (p *pass) validateViolation(s *Schema, pn string, f *BindingFailure) Violation {
	v := Violation{Check: CheckValidate, ItemType: s.URL, Property: pn, Failure: f}
	if pn == "" {
		v.Message = fmt.Sprintf("[%s]: the instance was rejected by @validate", s.URL)
	} else {
		v.Message = fmt.Sprintf("[%s].%s: the value was rejected by @validate", s.URL, pn)
	}
	return v
}

func (p *pass) propertyValidators() error {
	var vs []Violation
	err := p.eachProp(func(inst *html.Node, s *Schema, ep *EffectiveProp) error {
		slot := ep.Decl.Validate
		if slot == nil || ep.Decl.Computed != nil {
			return nil
		}
		elems := propElements(inst, ep.Name)
		if len(elems) == 0 {
			return nil // runs only for properties with a value (R-MOD-45)
		}
		ok, err := p.runValidator(slot, JSSlotValidate, elems, ep)
		if err != nil {
			e, v := p.validateFailure(s, ep.Name, err, true)
			if e != nil {
				return e
			}
			vs = append(vs, v)
			return nil
		}
		if !ok {
			vs = append(vs, p.validateViolation(s, ep.Name, nil))
		}
		return nil
	})
	if err != nil {
		return err
	}
	return unprocessable(vs)
}

// runValidator evaluates a property-level validator over the value
// elements: Sessel self is their List, JavaScript gets the value.
func (p *pass) runValidator(slot *Slot, kind string, elems []*html.Node, ep *EffectiveProp) (bool, error) {
	switch slot.Lang {
	case LangSessel:
		self := make(sessel.List, len(elems))
		for i, e := range elems {
			self[i] = queried(e, p.afterDoc)
		}
		v, err := p.ev.runSessel(slot, sesselEnv{self: self, hasSelf: true, prior: p.prior, doc: docElement(p.afterDoc)})
		if err != nil {
			return false, err
		}
		return sessel.Truthy(v), nil
	case LangJS:
		res, err := p.ev.runJSSlot(slot, &JSCall{Slot: kind, Args: []any{pipelineValue(ep.Decl.Cardinality, elems)}, Document: p.html()})
		if err != nil {
			return false, err
		}
		return jsTruthy(res.Value), nil
	}
	return false, unknownLanguage(slot)
}

// jsTruthy is JavaScript truthiness over the JSON-like value model.
func jsTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0 && !math.IsNaN(x)
	case int64:
		return x != 0
	case int:
		return x != 0
	}
	return true
}

// ---------------------------------------------------------------- stage 6

func (p *pass) schemaValidators() error {
	var vs []Violation
	for _, inst := range p.instances {
		s := p.schemaOf(inst)
		for _, slot := range s.eff.validators {
			ok, err := p.runSchemaValidator(slot, inst)
			if err != nil {
				e, v := p.validateFailure(s, "", err, false)
				if e != nil {
					return e
				}
				vs = append(vs, v)
				break
			}
			if !ok {
				vs = append(vs, p.validateViolation(s, "", nil))
				break
			}
		}
	}
	return unprocessable(vs)
}

func (p *pass) runSchemaValidator(slot *Slot, inst *html.Node) (bool, error) {
	switch slot.Lang {
	case LangSessel:
		v, err := p.ev.runSessel(slot, sesselEnv{self: queried(inst, p.afterDoc), hasSelf: true, prior: p.prior, doc: docElement(p.afterDoc)})
		if err != nil {
			return false, err
		}
		return sessel.Truthy(v), nil
	case LangJS:
		res, err := p.ev.runJSSlot(slot, &JSCall{Slot: JSSlotSchemaValidate, This: dom.OuterHTML(inst), HasThis: true, Args: []any{nil}, Document: p.html()})
		if err != nil {
			return false, err
		}
		return jsTruthy(res.Value), nil
	}
	return false, unknownLanguage(slot)
}

// ---------------------------------------------------------------- stage 7

func (p *pass) groups() error {
	var vs []Violation
	for _, inst := range p.instances {
		s := p.schemaOf(inst)
		for _, g := range s.eff.groups {
			present := 0
			for _, m := range g.members {
				if len(propElements(inst, m)) > 0 {
					present++
				}
			}
			card := g.decl.Cardinality
			if cardinalityOK(card, present) {
				continue
			}
			vs = append(vs, Violation{Check: CheckGroup, ItemType: s.URL, Property: strings.Join(g.members, " "),
				Message: fmt.Sprintf("[%s]: group '%s' constraint violated: %s", s.URL, g.decl.Group, cardinalityDetail(card, present))})
		}
	}
	return unprocessable(vs)
}
