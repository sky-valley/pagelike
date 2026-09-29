package sessel

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/selector"
)

// ClassRegistry resolves type URLs to classes (nil when undeclared).
type ClassRegistry interface {
	Class(url string) Class
}

// ClassSet is a minimal read-only class registry built from the
// https://pagelove.org/Schema items of a set of documents: type, name,
// parent, properties (type, cardinality, static and Sessel defaults,
// @computed, @key) and Sessel methods. It is the stand-in until the schema
// package (validation, resolvers, JavaScript methods) supplies richer
// Classes; hosts may use either.
type ClassSet struct {
	byURL map[string]*BasicClass
}

// Class implements ClassRegistry.
func (s *ClassSet) Class(url string) Class {
	if s == nil {
		return nil
	}
	if c, ok := s.byURL[url]; ok {
		return c
	}
	return nil
}

// Len reports the number of classes.
func (s *ClassSet) Len() int { return len(s.byURL) }

// MicrodataClasses reads Schema declarations from docs (in the given order;
// the first declaration of a type wins).
func MicrodataClasses(docs []*Document) *ClassSet {
	s := &ClassSet{byURL: map[string]*BasicClass{}}
	for _, d := range docs {
		if d == nil || d.Root == nil {
			continue
		}
		for _, n := range itemNodes(d.Root, URLSchema) {
			c := schemaClass(n)
			if c == nil {
				continue
			}
			if _, dup := s.byURL[c.TypeURL]; !dup {
				s.byURL[c.TypeURL] = c
			}
		}
	}
	return s
}

// itemNodes finds itemscope elements of type t outside <template>.
func itemNodes(root *html.Node, t string) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if hasAttr(n, "itemscope") {
				for _, x := range strings.Fields(attrOr(n, "itemtype", "")) {
					if x == t {
						out = append(out, n)
						break
					}
				}
			}
			if selector.IsTemplate(n) {
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

func first(it *microdata.Item, name string) (microdata.Prop, bool) {
	for _, p := range it.Props {
		if p.Name == name {
			return p, true
		}
	}
	return microdata.Prop{}, false
}

func fieldText(it *microdata.Item, name string) string {
	p, ok := first(it, name)
	if !ok {
		return ""
	}
	if p.Item != nil {
		return ""
	}
	return strings.TrimSpace(elementValueString(p.Node))
}

// slotSource reads a Sessel binding slot (R-MOD-7): a Sessel item's source,
// or a bare <script> property's text. ok is false for static values and
// other languages.
func slotSource(p microdata.Prop) (string, bool) {
	if p.Item != nil {
		if p.Item.HasType(URLSessel) || p.Item.HasType(URLLambda) {
			return strings.TrimSpace(fieldText(p.Item, "source")), true
		}
		return "", false
	}
	if p.Node.Data == "script" {
		return strings.TrimSpace(elementValueString(p.Node)), true
	}
	return "", false
}

func schemaClass(n *html.Node) *BasicClass {
	it := microdata.Parse(n)
	t := fieldText(it, "type")
	if t == "" {
		return nil
	}
	c := &BasicClass{TypeURL: t, TypeName: fieldText(it, "name"), ParentURL: fieldText(it, "parent")}
	seen := map[string]bool{}
	for _, p := range it.Props {
		if p.Name != "property" || p.Item == nil {
			continue
		}
		pi := p.Item
		if pi.HasType(URLMethod) {
			if m := methodOf(pi); m != nil {
				c.Meths = append(c.Meths, m)
			}
			continue
		}
		name := fieldText(pi, "name")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		prop := &Property{Name: name, Type: fieldText(pi, "type"), Cardinality: fieldText(pi, "cardinality")}
		switch prop.Cardinality {
		case "0..1", "1..1", "0..n", "1..n":
		default:
			prop.Cardinality = ""
		}
		if dp, ok := first(pi, "default"); ok {
			if src, isCode := slotSource(dp); isCode {
				prop.DefaultSessel = src
			} else if dp.Item == nil {
				prop.Default, prop.HasDefault = elementValueString(dp.Node), true
			}
		}
		if cp, ok := first(pi, "@computed"); ok {
			if src, isCode := slotSource(cp); isCode {
				prop.Computed = src
			}
		}
		prop.Key = strings.EqualFold(fieldText(pi, "@key"), "true")
		c.Props = append(c.Props, prop)
	}
	return c
}

func methodOf(it *microdata.Item) *Method {
	name := fieldText(it, "name")
	if name == "" {
		return nil
	}
	m := &Method{Name: name, Static: strings.EqualFold(fieldText(it, "static"), "true")}
	for _, p := range it.Props {
		if p.Name == "parameter" && p.Item != nil {
			if pn := fieldText(p.Item, "name"); pn != "" {
				m.Params = append(m.Params, pn)
			}
		}
	}
	ip, ok := first(it, "implementation")
	if !ok {
		return nil
	}
	src, isSessel := slotSource(ip)
	if !isSessel {
		m.Native = func(c *Call, self Value, args []Value) (Value, error) {
			return nil, runtimeErr("method %s is not implemented in Sessel and no runtime for its language is available", name)
		}
		return m
	}
	m.Source = src
	return m
}
