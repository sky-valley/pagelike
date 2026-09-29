package liquid

import (
	"strings"
	"sync"

	"github.com/osteele/liquid/values"
	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/microdata"
)

// Item is a bound element as Liquid sees it: a microdata item (R-LIQ-52 …
// R-LIQ-56, decision 0002 "*Item values"). It implements values.Value
// directly rather than the library's Drop interface, so that property
// lookups are lazy, an item keeps its identity through filters, and
// `{{ item }}` and `| json` render something deliberate.
//
// Keys:
//
//	item.<name>        the microdata value(s) of itemprop <name>: nil when
//	                   absent, the value when there is one, an array in
//	                   document order when there are several. Values are
//	                   strings (package microdata's value rules, itemref
//	                   included); a nested itemscope is a nested *Item.
//	item['@id']        "<document path>#<id>", or the path when the element
//	                   has no id
//	item['@type']      the itemtype attribute verbatim (nil when absent)
//	item.size          the `size` itemprop if there is one, else the number
//	                   of distinct property names
//	item['@text']      text content, untrimmed        (pagelike-only)
//	item['@html']      inner markup                   (pagelike-only)
//	item['@tag']       element name                   (pagelike-only)
//	item['@attributes'] attributes as a hash          (pagelike-only)
//
// An element without itemscope is still an Item: it has no properties but
// keeps the reserved keys and its rendering (R-LIQ-54).
//
// `{{ item }}` and string coercion give the element's trimmed text; JSON
// gives {"@id", "@type", properties in first-occurrence order}.
//
// An Item reads its node lazily and caches what it read: the tree must not
// change while templates that can see it are rendering.
type Item struct {
	// Path is the stored document path (absolute, percent-decoded).
	Path string
	// Node is the bound element.
	Node *html.Node

	once  sync.Once
	md    *microdata.Item
	names []string       // property names, first-seen document order
	props map[string]any // name -> value, []any when repeated
}

// NewItem wraps a bound element found in the document stored at path.
func NewItem(path string, n *html.Node) *Item { return &Item{Path: path, Node: n} }

func (it *Item) load() {
	it.once.Do(func() {
		it.props = map[string]any{}
		if it.md == nil {
			if !microdata.IsItem(it.Node) {
				return
			}
			it.md = microdata.Parse(it.Node)
		}
		for _, p := range it.md.Props {
			var v any = p.Value
			if p.Item != nil {
				v = &Item{Path: it.Path, Node: p.Node, md: p.Item}
			}
			switch prev, seen := it.props[p.Name]; {
			case !seen:
				it.names = append(it.names, p.Name)
				it.props[p.Name] = v
			default:
				if arr, isArr := prev.([]any); isArr {
					it.props[p.Name] = append(arr, v)
				} else {
					it.props[p.Name] = []any{prev, v}
				}
			}
		}
	})
}

// get looks a key up: the reserved @ keys first, then the properties, then
// `size`.
func (it *Item) get(key string) (any, bool) {
	switch key {
	case "@id":
		if id, ok := dom.Attr(it.Node, "id"); ok && id != "" {
			return it.Path + "#" + id, true
		}
		return it.Path, true
	case "@type":
		return dom.Attr(it.Node, "itemtype")
	case "@text":
		return dom.TextContent(it.Node), true
	case "@html":
		return dom.InnerHTML(it.Node), true
	case "@tag":
		return it.Node.Data, true
	case "@attributes":
		h := NewHash()
		for _, a := range it.Node.Attr {
			name := a.Key
			if a.Namespace != "" && !strings.Contains(a.Namespace, "/") {
				name = a.Namespace + ":" + a.Key
			}
			h.Set(name, a.Val)
		}
		return h, true
	}
	it.load()
	if v, ok := it.props[key]; ok {
		return v, true
	}
	if key == "size" {
		return len(it.names), true
	}
	return nil, false
}

// Keys returns the property names in first-seen document order.
func (it *Item) Keys() []string {
	it.load()
	return append([]string(nil), it.names...)
}

// String is how an item renders and coerces to a string: its trimmed text
// content (R-LIQ-56).
func (it *Item) String() string { return strings.TrimSpace(dom.TextContent(it.Node)) }

// Interface implements values.Value.
func (it *Item) Interface() any { return it }

// Int implements values.Value.
func (it *Item) Int() int { panic(values.TypeError("can't convert item to int")) }

// Equal implements values.Value: two items are equal when they wrap the
// same element.
func (it *Item) Equal(o values.Value) bool {
	other, ok := o.Interface().(*Item)
	return ok && other.Node == it.Node
}

// Less implements values.Value: items are not ordered.
func (it *Item) Less(values.Value) bool { return false }

// Contains implements values.Value: false, as for hashes (R-LIQ-74).
func (it *Item) Contains(values.Value) bool { return false }

// IndexValue implements values.Value: `item['name']`, `item['@id']`.
func (it *Item) IndexValue(k values.Value) values.Value { return it.PropertyValue(k) }

// PropertyValue implements values.Value: `item.name`.
func (it *Item) PropertyValue(k values.Value) values.Value {
	key, ok := k.Interface().(string)
	if !ok {
		return undefinedValue
	}
	v, found := it.get(key)
	if !found {
		return undefinedValue
	}
	return values.ValueOf(v)
}

// Test implements values.Value: every item is truthy.
func (it *Item) Test() bool { return true }
