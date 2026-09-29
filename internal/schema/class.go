package schema

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/sessel"
)

// Class is a registered schema as a Sessel class (R-SESSEL-280..288):
// construction with defaults and validation, typed search, property reads
// through computed properties and the @read pipeline, and methods (Sessel
// bodies; JavaScript bodies through the JSRunner).
type Class struct {
	reg     *Registry
	s       *Schema
	props   []*sessel.Property
	methods []*sessel.Method
}

var _ sessel.Class = (*Class)(nil)

func newClass(r *Registry, s *Schema) *Class {
	c := &Class{reg: r, s: s}
	for _, d := range s.Props {
		sp := &sessel.Property{Name: d.Name, Type: d.Type, Cardinality: d.Cardinality}
		if d.Default != nil {
			switch d.Default.Lang {
			case LangStatic:
				sp.Default, sp.HasDefault = d.Default.Source, true
			case LangSessel:
				sp.DefaultSessel = d.Default.Source
			}
		}
		if d.Computed != nil && d.Computed.Lang == LangSessel {
			sp.Computed = d.Computed.Source
		}
		if s.eff != nil && s.eff.key != nil && s.eff.key.Name == d.Name {
			sp.Key = true
		}
		sp.Read = c.readFunc(d.Name)
		c.props = append(c.props, sp)
	}
	for _, m := range s.Methods {
		c.methods = append(c.methods, c.method(m))
	}
	return c
}

// Schema returns the class's declaration.
func (c *Class) Schema() *Schema { return c.s }

func (c *Class) URL() string { return c.s.URL }

func (c *Class) Name() string {
	if c.s.Name != "" {
		return c.s.Name
	}
	u := strings.TrimRight(c.s.URL, "/")
	if i := strings.LastIndexAny(u, "/#:"); i >= 0 {
		return u[i+1:]
	}
	return u
}

func (c *Class) Parent() string { return c.s.ParentURL }

func (c *Class) Property(name string) *sessel.Property {
	for _, p := range c.props {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// AllProperties lists the own declarations (used by generic helpers).
func (c *Class) AllProperties() []*sessel.Property { return c.props }

func (c *Class) Methods() []*sessel.Method { return c.methods }

func (c *Class) Search(call *sessel.Call, criteria []map[string]string, isa bool) (sessel.List, error) {
	return sessel.SearchInstances(call, c, criteria, isa)
}

// Construct implements `new Type { … }` (R-SESSEL-285, R-MOD-35): the
// generic construction, then the schema's defaults (including the @key
// auto default), then the structural checks; a violation is an error.
func (c *Class) Construct(call *sessel.Call, items []sessel.Item) (*sessel.Element, error) {
	el, err := sessel.ConstructInstance(call, &sessel.BasicClass{TypeURL: c.s.URL}, items)
	if err != nil {
		return nil, err
	}
	el.Class = c
	s := c.s
	if s.loadErr != "" {
		return nil, fmt.Errorf("[%s]: %s", s.URL, s.loadErr)
	}
	for _, pn := range s.eff.propOrder {
		ep := s.eff.props[pn]
		if ep.Decl.Computed != nil {
			if len(propElements(el.Node, pn)) > 0 {
				return nil, fmt.Errorf("[%s].%s: the property is computed and cannot be written", s.URL, pn)
			}
			continue
		}
		if len(propElements(el.Node, pn)) > 0 {
			continue
		}
		nodes, err := c.defaultNodes(call, el.Node, ep)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			setItemprop(n, pn)
			el.Node.AppendChild(n)
		}
	}
	p := &pass{reg: c.reg}
	var msgs []string
	for _, pn := range s.eff.propOrder {
		ep := s.eff.props[pn]
		if ep.Decl.Computed != nil {
			continue
		}
		elems := propElements(el.Node, pn)
		if !cardinalityOK(ep.Decl.Cardinality, len(elems)) {
			msgs = append(msgs, fmt.Sprintf("[%s].%s: %s", s.URL, pn, cardinalityDetail(ep.Decl.Cardinality, len(elems))))
		}
		for _, v := range p.typeCheck(s, ep, elems) {
			msgs = append(msgs, v.Message)
		}
	}
	if len(msgs) > 0 {
		return nil, errors.New(strings.Join(msgs, "; "))
	}
	return el, nil
}

// defaultNodes computes a property's default during construction.
func (c *Class) defaultNodes(call *sessel.Call, inst *html.Node, ep *EffectiveProp) ([]*html.Node, error) {
	d := ep.Decl.Default
	switch {
	case d == nil:
		if ep.AutoKey {
			return []*html.Node{newMeta(ep.Name, randomKey())}, nil
		}
		return nil, nil
	case d.Lang == LangStatic:
		return []*html.Node{newMeta(ep.Name, d.Source)}, nil
	case d.Lang == LangSessel:
		v, err := call.Eval(d.Source, nil, false, nil)
		if err != nil {
			return nil, err
		}
		return valueNodes(ep.Name, v)
	case d.Lang == LangJS:
		res, err := runJS(call.Ctx, &JSCall{Slot: JSSlotDefault, Source: d.Source, This: c.reg.instanceView(c.s.URL, inst, ""), HasThis: true,
			Args: []any{map[string]any{"document_html": dom.OuterHTML(inst)}}, Document: dom.OuterHTML(inst), DocumentWritable: true, Classes: c.reg,
			Host: call.Host(), Budget: callBudget(call)})
		if err != nil {
			return nil, err
		}
		return valueNodes(ep.Name, fromJS(res.Value))
	}
	return nil, unknownLanguage(d)
}

// readFunc returns the typed-read hook of a property (R-SESSEL-286): a
// JavaScript @computed, else the @read pipeline (root → leaf) of the
// instance's own class over copies of the stored value elements.
func (c *Class) readFunc(name string) func(call *sessel.Call, inst *sessel.Element, vals sessel.List) (sessel.List, error) {
	return func(call *sessel.Call, inst *sessel.Element, vals sessel.List) (sessel.List, error) {
		leaf := c.s
		if inst.Class != nil {
			if s := c.reg.schemas[inst.Class.URL()]; s != nil && s.eff != nil {
				leaf = s
			}
		}
		if leaf.eff == nil {
			return vals, nil
		}
		ep := leaf.eff.props[name]
		if ep == nil {
			return vals, nil
		}
		if comp := ep.Decl.Computed; comp != nil {
			switch comp.Lang {
			case LangSessel:
				v, err := call.Eval(comp.Source, inst, true, nil)
				if err != nil {
					return nil, err
				}
				return sessel.List{v}, nil
			case LangJS:
				res, err := runJS(call.Ctx, &JSCall{Slot: JSSlotComputed, Source: comp.Source, This: c.reg.instanceView(leaf.URL, inst.Node, name), HasThis: true,
					Args: []any{map[string]any{"document_html": dom.OuterHTML(inst.Node)}}, Classes: c.reg, Host: call.Host(), Budget: callBudget(call)})
				if err != nil {
					return nil, err
				}
				return sessel.List{fromJS(res.Value)}, nil
			}
			return nil, unknownLanguage(comp)
		}
		if len(ep.ReadChain) == 0 {
			return vals, nil
		}
		elems := propElements(inst.Node, name)
		if len(elems) == 0 {
			return vals, nil
		}
		ev := &evaluator{ctx: call.Ctx, host: call.Host(), reg: c.reg, budget: call.Env().Budget}
		out, err := ev.readChain(ep, elems, sesselEnv{})
		if err != nil {
			return nil, err
		}
		res := make(sessel.List, 0, len(out))
		for _, n := range out {
			if isItem(n) {
				res = append(res, sessel.NewElement(n))
				continue
			}
			if v := valueOf(n); v.null {
				res = append(res, nil)
			} else {
				res = append(res, v.s)
			}
		}
		return res, nil
	}
}

// method converts a declaration into a Sessel method: a Sessel body runs in
// the calling evaluation; other languages go through Native.
func (c *Class) method(m *MethodDecl) *sessel.Method {
	sm := &sessel.Method{Name: m.Name, Params: m.Params, Static: m.Static}
	impl := m.Implementation
	switch impl.Lang {
	case LangSessel:
		sm.Source = impl.Source
	case LangJS:
		reg := c.reg
		sm.Native = func(call *sessel.Call, self sessel.Value, args []sessel.Value) (sessel.Value, error) {
			goArgs := make([]any, len(m.Params))
			for i := range goArgs {
				if i < len(args) {
					goArgs[i] = sessel.ToGo(args[i])
				}
			}
			res, err := runJS(call.Ctx, &JSCall{Slot: JSSlotMethod, Source: impl.Source, This: sessel.ToGo(self), HasThis: true,
				Args: goArgs, Context: call.Context(), Classes: reg, Receiver: self, Host: call.Host(), Budget: callBudget(call)})
			if err != nil {
				return nil, err
			}
			return fromJS(res.Value), nil
		}
	default:
		sm.Native = func(*sessel.Call, sessel.Value, []sessel.Value) (sessel.Value, error) {
			return nil, unknownLanguage(impl)
		}
	}
	return sm
}

// callBudget is the Sessel budget of the evaluation a hook runs in.
func callBudget(call *sessel.Call) *sessel.Budget {
	if env := call.Env(); env != nil {
		return env.Budget
	}
	return nil
}

// randomKey is the implicit @key default (R-MOD-35): ^[a-z][a-z0-9]{7}$.
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

// readChain runs a property's @read chain (root → leaf) over copies of its
// value elements and returns the resulting elements (R-MOD-50/51). Stages
// that do not compile are skipped (R-MOD-48).
func (ev *evaluator) readChain(ep *EffectiveProp, elems []*html.Node, env sesselEnv) ([]*html.Node, error) {
	cur := make([]*html.Node, len(elems))
	for i, e := range elems {
		cur[i] = dom.Clone(e)
	}
	for _, slot := range ep.ReadChain {
		if len(cur) == 0 {
			break
		}
		switch slot.Lang {
		case LangSessel:
			if slot.compile() != nil {
				continue
			}
			self := make(sessel.List, len(cur))
			for i, n := range cur {
				self[i] = &sessel.Element{Node: n, Mutable: true}
			}
			env.self, env.hasSelf = self, true
			v, err := ev.runResolver(slot, env)
			if err != nil {
				return nil, err
			}
			nodes, err := resultElements(v)
			if err != nil {
				return nil, err
			}
			for _, n := range nodes {
				setItemprop(n, ep.Name)
			}
			cur = nodes
		case LangJS:
			res, err := ev.runJSSlot(slot, &JSCall{Slot: JSSlotRead, Args: []any{pipelineValue(ep.Decl.Cardinality, cur)}})
			if err != nil {
				return nil, err
			}
			in, rest := cur, []*html.Node(nil)
			if !isMulti(ep.Decl.Cardinality) {
				in, rest = cur[:1], cur[1:] // only the first value flowed through
			}
			next, err := jsResultNodes(ep.Name, in, res.Value)
			if err != nil {
				return nil, err
			}
			cur = append(next, rest...)
		default:
			return nil, unknownLanguage(slot)
		}
	}
	return cur, nil
}

// jsResultNodes applies a JavaScript pipeline result to detached copies of
// the current elements (the read-path counterpart of pass.writeBack).
func jsResultNodes(pn string, cur []*html.Node, v any) ([]*html.Node, error) {
	var vals []any
	switch x := v.(type) {
	case nil:
	case []any:
		vals = x
	default:
		vals = []any{x}
	}
	var out []*html.Node
	for i, it := range vals {
		if m, ok := it.(map[string]any); ok && m["$type"] == "element" {
			h, _ := m["$html"].(string)
			if n := parseElement(h); n != nil {
				setItemprop(n, pn)
				out = append(out, n)
				continue
			}
		}
		if _, ok := it.(map[string]any); ok {
			return nil, &BindingFailure{Language: URLJavaScript, Variant: "return-type", Message: "an object cannot be a property value"}
		}
		src := cur[len(cur)-1]
		if i < len(cur) {
			src = cur[i]
		}
		n := dom.Clone(src)
		setValue(n, scalarText(it))
		out = append(out, n)
	}
	return out, nil
}
