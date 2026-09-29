package microdata

import (
	"encoding/json"
	"strings"

	"golang.org/x/net/html"
)

// SplitType splits a URL itemtype into (@context, @type): the context is the
// URL up to and including its last '/', the type is the remainder. Non-URL
// types have no context.
func SplitType(t string) (ctx, typ string) {
	if strings.Contains(t, "://") {
		if i := strings.LastIndexByte(t, '/'); i >= 0 && i < len(t)-1 {
			return t[:i+1], t[i+1:]
		}
	}
	return "", t
}

// JSONLD renders the top-level items found under the given roots as JSON-LD
// (PageLove docs, Content Negotiation → JSON-LD serialization): a single
// item is a flat object; several items are wrapped in @graph with @context
// hoisted when every item shares one vocabulary.
func JSONLD(roots []*html.Node) ([]byte, error) {
	var items []*Item
	for _, r := range roots {
		if IsItem(r) {
			items = append(items, Parse(r))
			continue
		}
		items = append(items, TopLevel(r)...)
	}
	objs := make([]map[string]any, 0, len(items))
	ctxs := map[string]bool{}
	for _, it := range items {
		o := itemObject(it)
		objs = append(objs, o)
		c, _ := o["@context"].(string)
		ctxs[c] = true
	}
	var out any
	switch {
	case len(objs) == 1:
		out = orderedObject(objs[0])
	default:
		graph := make([]any, 0, len(objs))
		shared := ""
		if len(ctxs) == 1 {
			for c := range ctxs {
				shared = c
			}
		}
		for _, o := range objs {
			if shared != "" {
				delete(o, "@context")
			}
			graph = append(graph, orderedObject(o))
		}
		top := ordered{}
		if shared != "" {
			top = append(top, kv{"@context", shared})
		}
		top = append(top, kv{"@graph", graph})
		out = top
	}
	return json.MarshalIndent(out, "", "  ")
}

func itemObject(it *Item) map[string]any {
	o := map[string]any{}
	if t := it.Type(); t != "" {
		c, typ := SplitType(t)
		if c != "" {
			o["@context"] = c
		}
		o["@type"] = typ
	}
	if it.ID != "" {
		o["@id"] = it.ID
	}
	order := []string{}
	vals := map[string][]any{}
	for _, p := range it.Props {
		var v any = p.Value
		if p.Item != nil {
			sub := itemObject(p.Item)
			if sc, ok := sub["@context"]; ok && sc == o["@context"] {
				delete(sub, "@context")
			}
			v = orderedObject(sub)
		}
		if _, ok := vals[p.Name]; !ok {
			order = append(order, p.Name)
		}
		vals[p.Name] = append(vals[p.Name], v)
	}
	for _, k := range order {
		if len(vals[k]) == 1 {
			o[k] = vals[k][0]
		} else {
			o[k] = vals[k]
		}
	}
	o["\x00order"] = order
	return o
}

type kv struct {
	K string
	V any
}
type ordered []kv

func (o ordered) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, e := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(e.K)
		v, err := json.Marshal(e.V)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// orderedObject emits @context, @type, @id, then properties in document order.
func orderedObject(o map[string]any) ordered {
	var out ordered
	for _, k := range []string{"@context", "@type", "@id"} {
		if v, ok := o[k]; ok {
			out = append(out, kv{k, v})
		}
	}
	order, _ := o["\x00order"].([]string)
	for _, k := range order {
		out = append(out, kv{k, o[k]})
	}
	return out
}
