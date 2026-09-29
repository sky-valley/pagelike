package sessel

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var voidTags = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true, "input": true,
	"link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

// evalNew implements `new …` (R-SESSEL-260..265, 285).
func (ev *evaluator) evalNew(x *newNode, sc *scope) (Value, error) {
	if x.prefix == "" && len(x.mods) == 0 {
		if v, ok := ev.classNamed(x.name, sc); ok {
			items, err := ev.evalItems(x.items, sc)
			if err != nil {
				return nil, err
			}
			switch c := v.(type) {
			case *TypeNS: // Selector
				return newSelector(items)
			case Class:
				return ev.construct(c, items)
			}
		}
	}
	return ev.buildElement(x, sc)
}

// classNamed resolves a construction name to a Class or the Selector
// namespace (steps 1–4 of R-SESSEL-60, without raising).
func (ev *evaluator) classNamed(name string, sc *scope) (Value, bool) {
	if !ev.resolvable(name, sc) {
		return nil, false
	}
	v, err := ev.lookup(name, sc)
	if err != nil {
		return nil, false
	}
	switch c := v.(type) {
	case Class:
		return c, true
	case *TypeNS:
		// `new Selector {…}` builds a Selector only where the program
		// imports it (@schema Selector url("https://pagelove.org/Selector"))
		// or binds the name; otherwise it is an ordinary <selector> element,
		// as on live PageLove (2026-09-29).
		if c.Name == "Selector" {
			_, declared := ev.decls[name]
			_, bound := sc.lookup(name)
			if declared || bound {
				return c, true
			}
		}
	}
	return nil, false
}

// Item is one construction item: a named property value (Name != "") or a
// positional child.
type Item struct {
	Name  string
	Value Value
}

func (ev *evaluator) evalItems(items []citem, sc *scope) ([]Item, error) {
	out := make([]Item, 0, len(items))
	for _, it := range items {
		v, err := ev.eval(it.expr, sc)
		if err != nil {
			return nil, err
		}
		out = append(out, Item{Name: it.name, Value: v})
	}
	return out, nil
}

func newSelector(items []Item) (Value, error) {
	var src *string
	for _, it := range items {
		if it.Name != "" && it.Name != "selector" {
			return nil, typeErr("Selector takes a CSS string or selector:, not %q", it.Name)
		}
		s, ok := it.Value.(string)
		if !ok {
			if sv, ok := it.Value.(*SelectorValue); ok {
				s = sv.Source
			} else {
				return nil, typeErr("Selector takes a CSS String, not %s", TypeName(it.Value))
			}
		}
		s = strings.TrimSpace(s)
		src = &s
	}
	if src == nil {
		return nil, typeErr("Selector needs a CSS string")
	}
	return &SelectorValue{Source: *src}, nil
}

// construct builds an Instance through the class (defaults, validation).
func (ev *evaluator) construct(c Class, items []Item) (Value, error) {
	el, err := c.Construct(&Call{Ctx: ev.r.ctx, ev: ev}, items)
	if err != nil {
		if _, ok := err.(*Error); ok {
			return nil, err
		}
		return nil, typeErr("%v", err)
	}
	return el, ev.r.alloc(256)
}

// buildElement constructs a plain element.
func (ev *evaluator) buildElement(x *newNode, sc *scope) (Value, error) {
	n := &html.Node{Type: html.ElementNode}
	switch {
	case x.prefix != "":
		uri := ev.namespaceURI(x.prefix)
		switch uri {
		case nsSVG:
			n.Namespace, n.Data = "svg", x.name
		case nsMathML:
			n.Namespace, n.Data = "math", x.name
		default:
			n.Data = x.prefix + ":" + x.name
		}
	default:
		n.Data = strings.ToLower(x.name)
		n.DataAtom = atom.Lookup([]byte(n.Data))
	}
	classIdx := -1
	for _, m := range x.mods {
		switch m.kind {
		case '.':
			if classIdx < 0 {
				classIdx = len(n.Attr)
				n.Attr = append(n.Attr, html.Attribute{Key: "class", Val: m.name})
			} else {
				n.Attr[classIdx].Val += " " + m.name
			}
		case '#':
			setAttr(n, "id", m.name)
		case '[':
			name := m.name
			if m.prefix != "" {
				name = m.prefix + ":" + m.name
			}
			if !m.hasVal {
				setAttr(n, name, "")
				continue
			}
			if m.hole == nil {
				setAttr(n, name, m.lit)
				continue
			}
			v, raw, err := ev.holeValue(m.hole, sc)
			if err != nil {
				return nil, err
			}
			switch {
			case raw:
				setAttr(n, name, m.hole.raw)
			case v == nil || v == false:
			case v == true:
				setAttr(n, name, "")
			default:
				setAttr(n, name, TextOf(v))
			}
		}
	}
	el := &Element{Node: n, Mutable: true}
	if err := ev.r.alloc(128); err != nil {
		return nil, err
	}
	if voidTags[n.Data] && n.Namespace == "" {
		// body items of void elements are evaluated but ignored
		_, err := ev.evalItems(x.items, sc)
		return el, err
	}
	for _, it := range x.items {
		v, err := ev.eval(it.expr, sc)
		if err != nil {
			return nil, err
		}
		switch it.name {
		case "":
			if _, ok := v.(*Dict); ok {
				return nil, typeErr("a Dictionary cannot be a child element")
			}
			nodes, err := ev.toNodes(v, true)
			if err != nil {
				return nil, err
			}
			if err := insertNodes(n, nil, nodes); err != nil {
				return nil, err
			}
		case "text":
			if v != nil {
				n.AppendChild(&html.Node{Type: html.TextNode, Data: TextOf(v)})
			}
		default:
			return nil, typeErr("%q is not a property of a plain <%s> element", it.name, n.Data)
		}
	}
	return el, nil
}

// holeValue evaluates an unquoted value, reporting raw=true when the
// fallback of R-SESSEL-202 applies.
func (ev *evaluator) holeValue(h *selHole, sc *scope) (Value, bool, error) {
	if h.number || h.expr == nil || (h.ident != "" && !ev.resolvable(h.ident, sc)) {
		return nil, true, nil
	}
	v, err := ev.eval(h.expr, sc)
	return v, false, err
}

func (ev *evaluator) namespaceURI(prefix string) string {
	if d, ok := ev.decls[prefix]; ok && d.kind == "namespace" {
		return d.url
	}
	return ""
}
