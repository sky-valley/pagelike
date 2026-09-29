package jsglue

import (
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/jsrt"
	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/schema"
	"github.com/sky-valley/pagelike/internal/sessel"
)

// Value conversions between the jsrt model, Sessel values and the schema
// package's JSON-like model (docs/spec/javascript.md A6).

// errNoForm reports a value without a JavaScript form (variant marshal).
type errNoForm struct{ typ string }

func (e *errNoForm) Error() string { return "a " + e.typ + " has no JavaScript form" }

// fromSessel converts a Sessel value for JavaScript (R-JS-40). Elements of
// registered schemas become instances of their imported classes when reg is
// set; other elements are DOM element handles (writable when constructed in
// this request).
func fromSessel(v sessel.Value, reg *schema.Registry) (jsrt.Value, error) {
	switch x := v.(type) {
	case nil, bool, string, int64, float64:
		return x, nil
	case sessel.List:
		out := make([]jsrt.Value, len(x))
		for i, it := range x {
			jv, err := fromSessel(it, reg)
			if err != nil {
				return nil, err
			}
			out[i] = jv
		}
		return out, nil
	case *sessel.Dict:
		if x == nil {
			return nil, nil
		}
		d := jsrt.NewDict()
		for _, k := range x.Keys() {
			jv, err := fromSessel(x.Lookup(k), reg)
			if err != nil {
				return nil, err
			}
			d.Set(k, jv)
		}
		return d, nil
	case *sessel.Element:
		if x == nil || x.Node == nil {
			return nil, nil
		}
		n := x.Node
		if n.Type == xhtml.DocumentNode {
			if n = dom.DocumentElement(n); n == nil {
				return nil, &errNoForm{"Document"}
			}
		}
		if n.Type != xhtml.ElementNode {
			return nil, &errNoForm{"non-element node"}
		}
		path := ""
		if x.Doc != nil {
			path = x.Doc.Path
		}
		if t := instanceType(reg, x); t != "" {
			return instanceOf(reg, t, n, path), nil
		}
		return &jsrt.Element{Node: n, Source: path, Writable: x.Mutable}, nil
	case sessel.Class:
		return &jsrt.Class{Type: x.URL()}, nil
	case *sessel.SelectorValue:
		return x.Source, nil
	}
	if t := sessel.TypeName(v); strings.HasPrefix(t, "Temporal.") {
		return jsrt.Temporal{Kind: strings.TrimPrefix(t, "Temporal."), ISO: sessel.TextOf(v)}, nil
	}
	return nil, &errNoForm{sessel.TypeName(v)}
}

// fromSesselLenient is fromSessel with values that have no JavaScript form
// read as undefined: for ambient data (Context entries, scope names) that
// the code may never touch.
func fromSesselLenient(v sessel.Value, reg *schema.Registry) jsrt.Value {
	jv, err := fromSessel(v, reg)
	if err != nil {
		return jsrt.Undefined{}
	}
	return jv
}

// instanceType returns the registered schema type of a Sessel element, or
// "": a typed instance (Class set) or an item whose itemtype is registered.
func instanceType(reg *schema.Registry, el *sessel.Element) string {
	if reg == nil {
		return ""
	}
	if el.Class != nil {
		if t := el.Class.URL(); reg.Schema(t) != nil {
			return t
		}
		return ""
	}
	return ""
}

// itemTypeOf returns the first itemtype of an item element, or "".
func itemTypeOf(n *xhtml.Node) string {
	if n == nil || n.Type != xhtml.ElementNode || !dom.HasAttr(n, "itemscope") {
		return ""
	}
	f := strings.Fields(dom.AttrOr(n, "itemtype", ""))
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// instanceOf builds the JavaScript instance of a stored item of registered
// type t: its declared properties that are set, as a list for an explicit
// 0..n/1..n cardinality and the first value otherwise (R-JS-11 list-read
// rule); nested items of registered types are instances, other nested
// items elements.
func instanceOf(reg *schema.Registry, t string, n *xhtml.Node, path string) *jsrt.Instance {
	it := microdata.Parse(n)
	props := jsrt.NewDict()
	for _, name := range reg.PropertyNames(t) {
		var vals []jsrt.Value
		for _, p := range it.Props {
			if p.Name != name {
				continue
			}
			if p.Item != nil {
				if nt := itemTypeOf(p.Node); nt != "" && reg.Schema(nt) != nil {
					vals = append(vals, instanceOf(reg, nt, p.Node, path))
				} else {
					vals = append(vals, &jsrt.Element{Node: p.Node, Source: path})
				}
				continue
			}
			vals = append(vals, p.Value)
		}
		if len(vals) == 0 {
			continue
		}
		if ep := reg.Property(t, name); ep != nil && isMulti(ep.Cardinality()) {
			props.Set(name, vals)
		} else {
			props.Set(name, vals[0])
		}
	}
	return &jsrt.Instance{Type: t, Props: props}
}

func isMulti(card string) bool { return card == "0..n" || card == "1..n" }

// fromGo converts the schema package's JSON-like model (and plain Go data)
// for JavaScript: tagged elements ({"$type":"element","$html":…}) become
// element values, maps dictionaries with sorted keys.
func fromGo(v any) jsrt.Value {
	switch x := v.(type) {
	case []any:
		out := make([]jsrt.Value, len(x))
		for i, it := range x {
			out[i] = fromGo(it)
		}
		return out
	case map[string]any:
		if x["$type"] == "element" {
			h, _ := x["$html"].(string)
			src, _ := x["$source"].(string)
			return &jsrt.Element{HTML: h, Source: src}
		}
		d := jsrt.NewDict()
		for _, k := range sortedKeys(x) {
			d.Set(k, fromGo(x[k]))
		}
		return d
	case int:
		return int64(x)
	case sessel.List, *sessel.Dict, *sessel.Element, *sessel.SelectorValue:
		return fromSesselLenient(x, nil)
	}
	return v
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// toGo converts a JavaScript result to the JSON-like model (R-JS-41):
// dictionaries become maps, elements and instances tagged elements (an
// instance as its microdata item).
func toGo(v jsrt.Value) any {
	switch x := v.(type) {
	case []jsrt.Value:
		out := make([]any, len(x))
		for i, it := range x {
			out[i] = toGo(it)
		}
		return out
	case *jsrt.Dict:
		if x == nil {
			return nil
		}
		m := make(map[string]any, x.Len())
		for _, k := range x.Keys() {
			it, _ := x.Get(k)
			m[k] = toGo(it)
		}
		return m
	case *jsrt.Element:
		if x == nil {
			return nil
		}
		m := map[string]any{"$type": "element", "$html": x.HTML}
		if x.Source != "" {
			m["$source"] = x.Source
		}
		return m
	case *jsrt.Instance:
		if x == nil {
			return nil
		}
		return map[string]any{"$type": "element", "$html": instanceMarkup(x, "")}
	}
	return v
}

// toSessel converts a JavaScript value to a Sessel value: elements and
// instances become constructed elements (instances of registered schemas
// typed with their class when reg is set).
func toSessel(v jsrt.Value, reg *schema.Registry) sessel.Value {
	switch x := v.(type) {
	case nil, bool, string, int64, float64:
		return x
	case []jsrt.Value:
		out := make(sessel.List, len(x))
		for i, it := range x {
			out[i] = toSessel(it, reg)
		}
		return out
	case *jsrt.Dict:
		if x == nil {
			return nil
		}
		d := sessel.NewDict()
		for _, k := range x.Keys() {
			it, _ := x.Get(k)
			d.Set(k, toSessel(it, reg))
		}
		return d
	case *jsrt.Element:
		if x == nil {
			return nil
		}
		if n := parseElement(x.HTML); n != nil {
			return sessel.NewElement(n)
		}
		return nil
	case *jsrt.Instance:
		if x == nil {
			return nil
		}
		n := parseElement(instanceMarkup(x, ""))
		if n == nil {
			return nil
		}
		el := sessel.NewElement(n)
		if reg != nil {
			if c := reg.Class(x.Type); c != nil {
				el.Class = c
			}
		}
		return el
	case *jsrt.Class:
		if reg != nil && x != nil {
			if c := reg.Class(x.Type); c != nil {
				return c
			}
		}
		return nil
	}
	return sessel.FromGo(v)
}

// instanceMarkup serializes an instance as its microdata item (R-JS-60,
// R-JS-87): declared properties in order, scalars as <meta content>,
// nested instances and elements as nested items/elements carrying the
// itemprop. itemprop names the item's own property when it is nested.
func instanceMarkup(inst *jsrt.Instance, itemprop string) string {
	var b strings.Builder
	b.WriteString("<div")
	if itemprop != "" {
		b.WriteString(` itemprop="` + html.EscapeString(itemprop) + `"`)
	}
	b.WriteString(` itemscope itemtype="` + html.EscapeString(inst.Type) + `">`)
	if inst.Props != nil {
		for _, k := range inst.Props.Keys() {
			v, _ := inst.Props.Get(k)
			writeProp(&b, k, v)
		}
	}
	b.WriteString("</div>")
	return b.String()
}

func writeProp(b *strings.Builder, name string, v jsrt.Value) {
	switch x := v.(type) {
	case nil:
	case []jsrt.Value:
		for _, it := range x {
			writeProp(b, name, it)
		}
	case *jsrt.Instance:
		if x != nil {
			b.WriteString(instanceMarkup(x, name))
		}
	case *jsrt.Element:
		if x == nil {
			return
		}
		if n := parseElement(x.HTML); n != nil {
			dom.SetAttr(n, "itemprop", name)
			b.WriteString(dom.OuterHTML(n))
		}
	case *jsrt.Dict:
		// A dictionary has no microdata form; it is left out.
	default:
		fmt.Fprintf(b, `<meta itemprop="%s" content="%s">`, html.EscapeString(name), html.EscapeString(sessel.TextOf(toSessel(v, nil))))
	}
}

var leadingTagRE = regexp.MustCompile(`^\s*<([A-Za-z][A-Za-z0-9:-]*)`)

// parseElement parses element markup (a value's outer HTML) into a detached
// element. Table parts and other context-sensitive elements parse as in a
// <template>; html, head and body roots as a document.
func parseElement(markup string) *xhtml.Node {
	m := leadingTagRE.FindStringSubmatch(markup)
	if m == nil {
		return nil
	}
	switch strings.ToLower(m[1]) {
	case "html", "head", "body":
		doc, err := dom.Parse([]byte(markup))
		if err != nil {
			return nil
		}
		root := dom.DocumentElement(doc)
		if root == nil {
			return nil
		}
		target := root
		switch strings.ToLower(m[1]) {
		case "head":
			target = dom.Head(doc)
		case "body":
			target = dom.Body(doc)
		}
		if target == nil {
			return nil
		}
		dom.Detach(target)
		return target
	}
	ctx := &xhtml.Node{Type: xhtml.ElementNode, Data: "template", DataAtom: atom.Template}
	nodes, err := dom.ParseFragment(markup, ctx)
	if err != nil {
		return nil
	}
	for _, n := range nodes {
		if n.Type == xhtml.ElementNode {
			if n.Parent != nil {
				n.Parent.RemoveChild(n)
			}
			n.PrevSibling, n.NextSibling = nil, nil
			return n
		}
	}
	return nil
}
