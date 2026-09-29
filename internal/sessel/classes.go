package sessel

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/sky-valley/pagelike/internal/dom"
)

// Class is a schema class (R-SESSEL-280..288): a type URL with its declared
// properties, methods and parent. The schema package implements it over the
// site's Schema items (with defaults, validation and resolvers);
// BasicClass is the generic implementation that ConstructInstance and
// SearchInstances make easy to reuse.
type Class interface {
	// URL is the type URL (the itemtype of instances).
	URL() string
	// Name is a display name (the Schema's name, else the URL's last segment).
	Name() string
	// Parent is the parent type URL; "" means the implicit
	// https://pagelove.org/Instance.
	Parent() string
	// Property returns this class's own declaration of a property, or nil.
	Property(name string) *Property
	// Methods returns this class's own methods (static and instance).
	Methods() []*Method
	// Construct materialises a new instance from construction items
	// (R-SESSEL-285): named property values and positional children,
	// schema defaults, validation.
	Construct(c *Call, items []Item) (*Element, error)
	// Search returns instances across the site (R-SESSEL-283). criteria is
	// an OR of AND-filters (property name → text); nil or empty means all.
	Search(c *Call, criteria []map[string]string, isa bool) (List, error)
}

// Property is a declared property.
type Property struct {
	Name        string
	Type        string
	Cardinality string // "", "0..1", "1..1", "0..n", "1..n"
	// Default is a static default (HasDefault) or DefaultSessel a dynamic
	// one (evaluated with self unbound).
	Default       string
	HasDefault    bool
	DefaultSessel string
	// Computed is an @computed Sessel expression evaluated with self = the
	// instance.
	Computed string
	// Key marks the @key property (auto-default [a-z][a-z0-9]{7}).
	Key bool
	// Read, when set, runs the @read pipeline over the stored values.
	Read func(c *Call, inst *Element, values List) (List, error)
}

// Method is a declared method. Source is a Sessel implementation; Native
// implements other languages (server JavaScript).
type Method struct {
	Name   string
	Params []string
	Static bool
	Source string
	Native func(c *Call, self Value, args []Value) (Value, error)
}

// BasicClass is a Class built from plain data.
type BasicClass struct {
	TypeURL   string
	TypeName  string
	ParentURL string
	Props     []*Property
	Meths     []*Method
}

func (b *BasicClass) URL() string { return b.TypeURL }

func (b *BasicClass) Name() string {
	if b.TypeName != "" {
		return b.TypeName
	}
	u := strings.TrimRight(b.TypeURL, "/")
	if i := strings.LastIndexAny(u, "/#"); i >= 0 {
		return u[i+1:]
	}
	return u
}

func (b *BasicClass) Parent() string { return b.ParentURL }

func (b *BasicClass) Property(name string) *Property {
	for _, p := range b.Props {
		if p.Name == name {
			return p
		}
	}
	return nil
}

func (b *BasicClass) Methods() []*Method { return b.Meths }

func (b *BasicClass) Construct(c *Call, items []Item) (*Element, error) {
	return ConstructInstance(c, b, items)
}

func (b *BasicClass) Search(c *Call, criteria []map[string]string, isa bool) (List, error) {
	return SearchInstances(c, b, criteria, isa)
}

// Built-in classes.
var (
	httpResponseClass = &BasicClass{TypeURL: URLHTTPResponse, TypeName: "HTTPResponse", Props: []*Property{
		{Name: "status", Cardinality: "0..1"}, {Name: "message", Cardinality: "0..1"}, {Name: "body", Cardinality: "0..1"},
		{Name: "header", Cardinality: "0..n"}}}
	pairClass       = &BasicClass{TypeURL: URLPair, TypeName: "Pair", Props: []*Property{{Name: "key", Cardinality: "0..1"}, {Name: "value", Cardinality: "0..1"}}}
	platformClass   = &BasicClass{TypeURL: URLPlatform, TypeName: "Pagelove"}
	reflectionClass = &BasicClass{TypeURL: URLSessel, TypeName: "Sessel"}
)

// chain returns cls and its ancestors (most-derived first).
func (c *Call) chain(cls Class) []Class {
	out := []Class{cls}
	seen := map[string]bool{cls.URL(): true}
	for p := cls.Parent(); p != "" && !seen[p]; {
		seen[p] = true
		pc, err := c.ev.lookupClass(p)
		if err != nil || pc == nil {
			break
		}
		out = append(out, pc)
		p = pc.Parent()
	}
	return out
}

// PropertyOf finds the most-derived declaration of name in cls's chain.
func (c *Call) PropertyOf(cls Class, name string) *Property {
	for _, k := range c.chain(cls) {
		if p := k.Property(name); p != nil {
			return p
		}
	}
	return nil
}

// ConstructInstance is the generic instance construction of R-SESSEL-285:
// a <div itemscope itemtype=URL>, named items as property values, positional
// items as children, then defaults for absent properties (including the
// @key auto-default). Validation is left to richer Class implementations.
func ConstructInstance(c *Call, cls Class, items []Item) (*Element, error) {
	n := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div,
		Attr: []html.Attribute{{Key: "itemscope"}, {Key: "itemtype", Val: cls.URL()}}}
	el := &Element{Node: n, Mutable: true, Class: cls}
	for _, it := range items {
		if it.Name == "" {
			nodes, err := c.ev.toNodes(it.Value, true)
			if err != nil {
				return nil, err
			}
			if err := insertNodes(n, nil, nodes); err != nil {
				return nil, err
			}
			continue
		}
		if err := addPropertyValue(c, n, it.Name, it.Value); err != nil {
			return nil, err
		}
	}
	done := map[string]bool{}
	for _, k := range c.chain(cls) {
		var props []*Property
		if b, ok := k.(interface{ AllProperties() []*Property }); ok {
			props = b.AllProperties()
		} else if b, ok := k.(*BasicClass); ok {
			props = b.Props
		}
		for _, p := range props {
			if done[p.Name] {
				continue
			}
			done[p.Name] = true
			if len(propertyNodes(n, p.Name)) > 0 {
				continue
			}
			switch {
			case p.HasDefault:
				if err := addPropertyValue(c, n, p.Name, p.Default); err != nil {
					return nil, err
				}
			case p.DefaultSessel != "":
				v, err := c.Eval(p.DefaultSessel, nil, false, nil)
				if err != nil {
					return nil, err
				}
				if v != nil {
					if err := addPropertyValue(c, n, p.Name, v); err != nil {
						return nil, err
					}
				}
			case p.Key:
				if err := addPropertyValue(c, n, p.Name, randomKey()); err != nil {
					return nil, err
				}
			}
		}
	}
	return el, nil
}

func randomKey() string {
	const lower = "abcdefghijklmnopqrstuvwxyz"
	const alnum = lower + "0123456789"
	b := make([]byte, 8)
	b[0] = lower[randInt(len(lower))]
	for i := 1; i < 8; i++ {
		b[i] = alnum[randInt(len(alnum))]
	}
	return string(b)
}

func randInt(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic(err)
	}
	return int(v.Int64())
}

// addPropertyValue appends property values for name (R-SESSEL-285):
// scalars as <meta itemprop content>, elements with itemprop added, lists
// one value per item, null nothing.
func addPropertyValue(c *Call, n *html.Node, name string, v Value) error {
	switch x := v.(type) {
	case nil:
		return nil
	case List:
		for _, it := range x {
			if err := addPropertyValue(c, n, name, it); err != nil {
				return err
			}
		}
		return nil
	case *Element:
		node := x.Node
		if !x.Mutable {
			node = dom.Clone(node)
		}
		ip := strings.Fields(attrOr(node, "itemprop", ""))
		has := false
		for _, t := range ip {
			has = has || t == name
		}
		if !has {
			setAttr(node, "itemprop", strings.TrimSpace(strings.Join(append(ip, name), " ")))
		}
		return insertNodes(n, nil, []*html.Node{node})
	case *Dict, *Lambda, Class, *TypeNS:
		return typeErr("%s cannot be the value of property %q", TypeName(v), name)
	}
	meta := &html.Node{Type: html.ElementNode, Data: "meta", DataAtom: atom.Meta,
		Attr: []html.Attribute{{Key: "itemprop", Val: name}, {Key: "content", Val: TextOf(v)}}}
	n.AppendChild(meta)
	return nil
}

// propertyNodes returns the property elements carrying name in the item
// rooted at n.
func propertyNodes(n *html.Node, name string) []*html.Node {
	var out []*html.Node
	for _, p := range itemProps(n) {
		if p.name == name {
			out = append(out, p.node)
		}
	}
	return out
}

// SearchInstances is the generic Class.search (R-SESSEL-283): the
// criteria become `:has([itemprop="k"]:value-equals("v"))` filters on the
// class's items across the whole site, in path then tree order. With isa
// the type test is :isa(URL) and each Instance keeps its actual class.
func SearchInstances(c *Call, cls Class, criteria []map[string]string, isa bool) (List, error) {
	base := "[itemscope][itemtype=" + cssString(cls.URL()) + "]"
	if isa {
		base = "[itemscope]:isa(" + cssString(cls.URL()) + ")"
	}
	var alts []string
	for _, crit := range criteria {
		keys := make([]string, 0, len(crit))
		for k := range crit {
			keys = append(keys, k)
		}
		sortStrings(keys)
		var b strings.Builder
		b.WriteString(base)
		for _, k := range keys {
			fmt.Fprintf(&b, ":has([itemprop=%s]:value-equals(%s))", cssString(k), cssString(crit[k]))
		}
		alts = append(alts, b.String())
	}
	if len(alts) == 0 {
		alts = []string{base}
	}
	sel, err := c.ev.compileCSS(strings.Join(alts, ", "), isa)
	if err != nil {
		return nil, err
	}
	roots, err := c.ev.siteRoots()
	if err != nil {
		return nil, err
	}
	found, err := c.ev.matchRoots(sel, roots)
	if err != nil {
		return nil, err
	}
	for _, v := range found {
		el := v.(*Element)
		el.Class = cls
		if isa {
			if t := strings.TrimSpace(attrOr(el.Node, "itemtype", "")); t != "" && t != cls.URL() {
				if actual, err := c.ev.lookupClass(t); err == nil && actual != nil {
					el.Class = actual
				}
			}
		}
	}
	return found, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// instanceProperty implements inst.prop (R-SESSEL-286).
func (ev *evaluator) instanceProperty(el *Element, name string) (Value, error) {
	c := &Call{Ctx: ev.r.ctx, ev: ev}
	p := c.PropertyOf(el.Class, name)
	if p != nil && p.Computed != "" {
		return c.Eval(p.Computed, el, true, nil)
	}
	var vals List
	for _, pr := range itemProps(el.Node) {
		if pr.name == name {
			vals = append(vals, propValue(el, pr.node))
		}
	}
	if p == nil {
		switch len(vals) {
		case 0:
			return nil, nil
		case 1:
			return vals[0], nil
		}
		return vals, nil
	}
	if p.Read != nil {
		var err error
		if vals, err = p.Read(c, el, vals); err != nil {
			return nil, err
		}
	}
	if p.Cardinality == "0..n" || p.Cardinality == "1..n" {
		if vals == nil {
			vals = List{}
		}
		return vals, nil
	}
	if len(vals) == 0 {
		return nil, nil
	}
	return vals[0], nil
}

// setInstanceProperty implements inst.prop = v (R-SESSEL-287). A queried
// instance is copied on first write (the snapshot is never mutated).
func (ev *evaluator) setInstanceProperty(el *Element, name string, v Value) error {
	c := &Call{Ctx: ev.r.ctx, ev: ev}
	if p := c.PropertyOf(el.Class, name); p != nil && p.Computed != "" {
		return typeErr("property %q is computed and cannot be written", name)
	}
	if !el.Mutable {
		el.Node = dom.Clone(el.Node)
		el.Mutable = true
	}
	old := propertyNodes(el.Node, name)
	holder := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	if err := addPropertyValue(c, holder, name, v); err != nil {
		return err
	}
	var nodes []*html.Node
	for ch := holder.FirstChild; ch != nil; ch = ch.NextSibling {
		nodes = append(nodes, ch)
	}
	for _, x := range nodes {
		holder.RemoveChild(x)
	}
	if len(old) == 0 {
		return insertNodes(el.Node, nil, nodes)
	}
	first := old[0]
	if err := insertNodes(first.Parent, first, nodes); err != nil {
		return err
	}
	for _, o := range old {
		if o.Parent != nil {
			o.Parent.RemoveChild(o)
		}
	}
	return nil
}

// findMethod finds a method by name through the chain (most-derived wins),
// preferring the overload whose parameter count matches argc.
func (c *Call) findMethod(cls Class, name string, static bool, argc int) *Method {
	for _, k := range c.chain(cls) {
		var cands []*Method
		for _, m := range k.Methods() {
			if m.Name == name && m.Static == static {
				cands = append(cands, m)
			}
		}
		if len(cands) == 0 {
			continue
		}
		for _, m := range cands {
			if len(m.Params) == argc {
				return m
			}
		}
		return cands[0]
	}
	return nil
}

// invokeMethod runs a method with self and positional arguments bound to
// its parameters (R-SESSEL-288).
func (ev *evaluator) invokeMethod(m *Method, self Value, args []Value, extra map[string]Value) (Value, error) {
	c := &Call{Ctx: ev.r.ctx, ev: ev}
	if m.Native != nil {
		if err := ev.r.enter(); err != nil {
			return nil, err
		}
		defer ev.r.leave()
		return m.Native(c, self, args)
	}
	locals := map[string]Value{}
	for i, p := range m.Params {
		var v Value
		if i < len(args) {
			v = args[i]
		}
		locals[p] = v
	}
	for k, v := range extra {
		locals[k] = v
	}
	return c.Eval(m.Source, self, true, locals)
}

// instanceMethod dispatches a schema method on an Instance (handled false
// when the schema declares neither the method nor doesNotUnderstand).
func (ev *evaluator) instanceMethod(el *Element, name string, args []Value) (Value, bool, error) {
	c := &Call{Ctx: ev.r.ctx, ev: ev}
	if m := c.findMethod(el.Class, name, false, len(args)); m != nil {
		v, err := ev.invokeMethod(m, el, args, nil)
		return v, true, err
	}
	return nil, false, nil
}

func (ev *evaluator) doesNotUnderstand(el *Element, name string, args []Value) (Value, bool, error) {
	c := &Call{Ctx: ev.r.ctx, ev: ev}
	m := c.findMethod(el.Class, "doesNotUnderstand", false, 0)
	if m == nil {
		return nil, false, nil
	}
	v, err := ev.invokeMethod(m, el, nil, map[string]Value{"messageName": name, "parameters": List(append([]Value{}, args...))})
	return v, true, err
}

// classMethod dispatches a call on a Class value: the platform interface,
// reflection, search/construct and static schema methods.
func (ev *evaluator) classMethod(cls Class, name string, args []Value) (Value, bool, error) {
	switch cls.URL() {
	case URLPlatform:
		return ev.platformMethod(name, args)
	case URLSessel:
		return ev.reflectionMethod(name, args)
	}
	c := &Call{Ctx: ev.r.ctx, ev: ev}
	if m := c.findMethod(cls, name, true, len(args)); m != nil {
		v, err := ev.invokeMethod(m, cls, args, nil)
		return v, true, err
	}
	switch name {
	case "search":
		v, err := ev.search(cls, args)
		return v, true, err
	case "construct":
		if len(args) != 1 {
			return nil, true, typeErr("construct() takes a Dictionary")
		}
		d, ok := args[0].(*Dict)
		if !ok {
			return nil, true, typeErr("construct() takes a Dictionary, not %s", TypeName(args[0]))
		}
		items := make([]Item, 0, d.Len())
		for _, k := range d.Keys() {
			items = append(items, Item{Name: k, Value: d.Lookup(k)})
		}
		v, err := ev.construct(cls, items)
		return v, true, err
	case "url", "URL":
		return cls.URL(), true, nil
	case "name":
		return cls.Name(), true, nil
	}
	return nil, false, nil
}

// search parses Class.search(criteria?, options?) (R-SESSEL-283).
func (ev *evaluator) search(cls Class, args []Value) (Value, error) {
	if len(args) > 2 {
		return nil, typeErr("search() takes criteria and options")
	}
	var criteria []map[string]string
	conv := func(d *Dict) (map[string]string, error) {
		m := map[string]string{}
		for _, k := range d.Keys() {
			switch v := d.Lookup(k).(type) {
			case string, int64, float64, bool:
				m[k] = TextOf(v)
			default:
				return nil, typeErr("search(): criterion %q must be a String, Number or Boolean, not %s", k, TypeName(v))
			}
		}
		return m, nil
	}
	if len(args) >= 1 {
		switch x := args[0].(type) {
		case nil:
		case *Dict:
			m, err := conv(x)
			if err != nil {
				return nil, err
			}
			criteria = append(criteria, m)
		case List:
			for _, it := range x {
				d, ok := it.(*Dict)
				if !ok {
					return nil, typeErr("search(): OR criteria must be Dictionaries, not %s", TypeName(it))
				}
				m, err := conv(d)
				if err != nil {
					return nil, err
				}
				criteria = append(criteria, m)
			}
		default:
			return nil, typeErr("search(): criteria must be a Dictionary or a List, not %s", TypeName(args[0]))
		}
	}
	isa := false
	if len(args) == 2 && args[1] != nil {
		opts, ok := args[1].(*Dict)
		if !ok {
			return nil, typeErr("search(): options must be a Dictionary")
		}
		for _, k := range opts.Keys() {
			if k != "isa" {
				return nil, typeErr("search(): unknown option %q", k)
			}
			b, ok := opts.Lookup(k).(bool)
			if !ok {
				return nil, typeErr("search(): option \"isa\" must be a Boolean")
			}
			isa = b
		}
	}
	return cls.Search(&Call{Ctx: ev.r.ctx, ev: ev}, criteria, isa)
}

// classMember implements Class.name without parentheses: a static method
// reference.
func (ev *evaluator) classMember(cls Class, name string) (Value, error) {
	c := &Call{Ctx: ev.r.ctx, ev: ev}
	if m := c.findMethod(cls, name, true, -1); m != nil {
		return NativeFunc(name, len(m.Params), func(cc *Call, args []Value) (Value, error) {
			return cc.ev.invokeMethod(m, cls, args, nil)
		}), nil
	}
	switch name {
	case "url", "URL":
		return cls.URL(), nil
	case "name":
		return cls.Name(), nil
	case "parent":
		if p := cls.Parent(); p != "" {
			return p, nil
		}
		return nil, nil
	}
	switch cls.URL() {
	case URLPlatform, URLSessel:
		return NativeFunc(name, -1, func(cc *Call, args []Value) (Value, error) {
			v, ok, err := cc.ev.classMethod(cls, name, args)
			if !ok && err == nil {
				return nil, typeErr("no method %q on %s", name, cls.Name())
			}
			return v, err
		}), nil
	}
	return nil, typeErr("class %s has no static member %q", cls.Name(), name)
}

// platformMethod implements Pagelove.GET/PUT/DELETE (R-SESSEL-295).
func (ev *evaluator) platformMethod(name string, args []Value) (Value, bool, error) {
	h := ev.r.host
	switch name {
	case "GET":
		if ev.env.DocumentOnly {
			// A QUERY program has no Pagelove.GET on live PageLove
			// (2026-09-29): it cannot read documents beyond its target.
			return nil, true, typeErr("unknown function: GET")
		}
		if len(args) != 1 {
			return nil, true, typeErr("Pagelove.GET() takes a path")
		}
		p, ok := args[0].(string)
		if !ok {
			return nil, true, typeErr("Pagelove.GET() takes a String path, not %s", TypeName(args[0]))
		}
		if h == nil {
			return nil, true, nil
		}
		v, err := h.Resource(ev.r.ctx, ev.resolvePath(p))
		if err != nil {
			return nil, true, runtimeErr("Pagelove.GET(%s): %v", p, err)
		}
		return v, true, nil
	case "PUT":
		if len(args) != 2 {
			return nil, true, typeErr("Pagelove.PUT() takes an item and a path")
		}
		item, ok := args[0].(*Element)
		if !ok {
			return nil, true, typeErr("Pagelove.PUT() takes an Element or Instance, not %s", TypeName(args[0]))
		}
		p, ok := args[1].(string)
		if !ok {
			return nil, true, typeErr("Pagelove.PUT() takes a String path, not %s", TypeName(args[1]))
		}
		w := writerOf(h)
		if w == nil {
			return nil, true, &Error{Type: RuntimeErrorType, Message: "Pagelove.PUT is not available here (no write provider)", Reason: ReasonNoWriter}
		}
		if err := w.Put(ev.r.ctx, item, ev.resolvePath(p)); err != nil {
			if e, ok := err.(*Error); ok {
				return nil, true, e
			}
			return nil, true, runtimeErr("Pagelove.PUT(%s): %v", p, err)
		}
		return nil, true, nil
	case "DELETE":
		if len(args) != 1 {
			return nil, true, typeErr("Pagelove.DELETE() takes a path")
		}
		w := writerOf(h)
		if w == nil {
			return nil, true, &Error{Type: RuntimeErrorType, Message: "Pagelove.DELETE is not available here (no write provider)", Reason: ReasonNoWriter}
		}
		target := args[0]
		if p, ok := target.(string); ok {
			target = ev.resolvePath(p)
		}
		if err := w.Delete(ev.r.ctx, target); err != nil {
			if e, ok := err.(*Error); ok {
				return nil, true, e
			}
			return nil, true, runtimeErr("Pagelove.DELETE: %v", err)
		}
		return nil, true, nil
	}
	return nil, false, nil
}

func writerOf(h Host) Writer {
	if h == nil {
		return nil
	}
	return h.Writer()
}

// reflectionMethod implements Sessel.stored/properties/schemaOf
// (R-SESSEL-289).
func (ev *evaluator) reflectionMethod(name string, args []Value) (Value, bool, error) {
	el := func() (*Element, error) {
		if len(args) == 0 {
			return nil, typeErr("Sessel.%s() needs an element", name)
		}
		if args[0] == nil {
			return nil, nil
		}
		e, ok := args[0].(*Element)
		if !ok {
			return nil, typeErr("Sessel.%s() needs an Element, not %s", name, TypeName(args[0]))
		}
		return e, nil
	}
	switch name {
	case "stored":
		e, err := el()
		if err != nil || e == nil {
			return nil, true, err
		}
		if len(args) != 2 {
			return nil, true, typeErr("Sessel.stored() takes an element and a property name")
		}
		pn, ok := args[1].(string)
		if !ok {
			return nil, true, typeErr("Sessel.stored(): property names are Strings")
		}
		for _, p := range itemProps(e.Node) {
			if p.name == pn {
				return propValue(e, p.node), true, nil
			}
		}
		return nil, true, nil
	case "properties":
		e, err := el()
		if err != nil || e == nil {
			return nil, true, err
		}
		out := List{}
		seen := map[string]bool{}
		for _, p := range itemProps(e.Node) {
			if !seen[p.name] {
				seen[p.name] = true
				out = append(out, p.name)
			}
		}
		return out, true, nil
	case "schemaOf":
		e, err := el()
		if err != nil || e == nil {
			return nil, true, err
		}
		if t, ok := getAttr(e.Node, "itemtype"); ok {
			return strings.TrimSpace(t), true, nil
		}
		return nil, true, nil
	}
	return nil, false, nil
}
