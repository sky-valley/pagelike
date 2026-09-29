package sessel

import (
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/selector"
)

// documentElement returns the root element under a document node (or the
// node itself when it is an element).
func documentElement(root *html.Node) *html.Node {
	if root == nil {
		return nil
	}
	if root.Type == html.ElementNode {
		return root
	}
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			return c
		}
	}
	return nil
}

// serialized attribute name (namespace:key for adjusted foreign attributes).
func attrKey(a html.Attribute) string {
	if a.Namespace != "" {
		return a.Namespace + ":" + a.Key
	}
	return a.Key
}

func getAttr(n *html.Node, name string) (string, bool) {
	if n == nil || n.Type != html.ElementNode {
		return "", false
	}
	for _, a := range n.Attr {
		if strings.EqualFold(attrKey(a), name) {
			return a.Val, true
		}
	}
	return "", false
}

func attrOr(n *html.Node, name, def string) string {
	if v, ok := getAttr(n, name); ok {
		return v
	}
	return def
}

func setAttr(n *html.Node, name, val string) {
	for i, a := range n.Attr {
		if strings.EqualFold(attrKey(a), name) {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: name, Val: val})
}

func removeAttr(n *html.Node, name string) {
	out := n.Attr[:0]
	for _, a := range n.Attr {
		if !strings.EqualFold(attrKey(a), name) {
			out = append(out, a)
		}
	}
	n.Attr = out
}

func hasAttr(n *html.Node, name string) bool { _, ok := getAttr(n, name); return ok }

// localName is the name the value rules switch on (HTML elements only).
func valueTag(n *html.Node) string {
	if n.Namespace != "" {
		return ""
	}
	return n.Data
}

// valueAttr is the attribute holding an element's value (R-SESSEL-222),
// "" for text-content elements.
func valueAttr(n *html.Node) string {
	switch valueTag(n) {
	case "a", "area", "link":
		return "href"
	case "audio", "embed", "iframe", "img", "source", "track", "video":
		return "src"
	case "object":
		return "data"
	case "meta":
		return "content"
	case "data", "meter":
		return "value"
	case "time":
		return "datetime"
	}
	return ""
}

// elementText is the concatenated descendant text, without <template>
// contents; null when empty (R-SESSEL-221).
func elementText(n *html.Node) Value {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(p *html.Node) {
		if selector.IsTemplate(p) {
			return
		}
		for c := p.FirstChild; c != nil; c = c.NextSibling {
			switch c.Type {
			case html.TextNode:
				b.WriteString(c.Data)
			case html.ElementNode:
				walk(c)
			}
		}
	}
	walk(n)
	if b.Len() == 0 {
		return nil
	}
	return b.String()
}

// elementValue implements .value() (R-SESSEL-222).
func elementValue(n *html.Node) Value {
	a := valueAttr(n)
	switch {
	case a == "datetime":
		if v, ok := getAttr(n, a); ok {
			return v
		}
		return elementText(n)
	case a != "":
		return attrOr(n, a, "")
	}
	return elementText(n)
}

func elementValueString(n *html.Node) string {
	s, _ := elementValue(n).(string)
	return s
}

type propRef struct {
	name string
	node *html.Node
}

// itemProps lists the property elements of the item rooted at n (the
// microdata crawl: descendants outside nested items, plus itemref).
func itemProps(n *html.Node) []propRef {
	var out []propRef
	for _, p := range microdata.Parse(n).Props {
		out = append(out, propRef{name: p.Name, node: p.Node})
	}
	return out
}

// propValue is a property element's value: a nested item as an Element,
// otherwise its microdata value.
func propValue(owner *Element, n *html.Node) Value {
	if hasAttr(n, "itemscope") {
		return owner.derive(n)
	}
	return elementValue(n)
}

// elementProperty implements member access on a plain element
// (R-SESSEL-232): none → null, one → its value, several → a List.
func (ev *evaluator) elementProperty(el *Element, name string) Value {
	var vals List
	for _, p := range itemProps(el.Node) {
		if p.name == name {
			vals = append(vals, propValue(el, p.node))
		}
	}
	switch len(vals) {
	case 0:
		return nil
	case 1:
		return vals[0]
	}
	return vals
}

// microdata implements .microdata(); a DocumentOnly program sees no
// provenance, so there is no @id.
func (ev *evaluator) microdata(el *Element) Value {
	if ev.env.DocumentOnly && el.Doc != nil {
		el = &Element{Node: el.Node, Class: el.Class, Mutable: el.Mutable}
	}
	return microdataDict(el)
}

// collapseSpace joins the whitespace-separated fields of s with single
// spaces (the text of stored elements, live 2026-09-29).
func collapseSpace(s string) string { return strings.Join(strings.Fields(s), " ") }

// microdataDict implements .microdata() (R-SESSEL-233).
func microdataDict(el *Element) Value {
	n := el.Node
	if !hasAttr(n, "itemscope") {
		return nil
	}
	d := NewDict()
	if t := strings.Fields(attrOr(n, "itemtype", "")); len(t) > 0 {
		_, short := microdata.SplitType(t[0])
		d.Set("@type", short)
	}
	var order []string
	vals := map[string]List{}
	for _, p := range itemProps(n) {
		if _, ok := vals[p.name]; !ok {
			order = append(order, p.name)
		}
		var v Value
		if hasAttr(p.node, "itemscope") {
			v = microdataDict(el.derive(p.node))
		} else {
			v = elementValue(p.node)
		}
		vals[p.name] = append(vals[p.name], v)
	}
	for _, k := range order {
		if l := vals[k]; len(l) == 1 {
			d.Set(k, l[0])
		} else {
			d.Set(k, l)
		}
	}
	if el.Doc != nil {
		if id := attrOr(n, "id", ""); id != "" {
			d.Set("@id", el.Doc.Path+"#"+id)
		}
	}
	return d
}

func parentElement(n *html.Node) *html.Node {
	if n.Parent != nil && n.Parent.Type == html.ElementNode {
		return n.Parent
	}
	return nil
}

// selectorOf implements .selector() (R-SESSEL-225).
func selectorOf(n *html.Node) string {
	if id := attrOr(n, "id", ""); id != "" {
		return "#" + selector.EscapeIdent(id)
	}
	tag := selector.EscapeIdent(n.Data)
	p := parentElement(n)
	if p == nil {
		return tag
	}
	return selectorOf(p) + " > " + tag + ":nth-child(" + strconv.Itoa(elementIndex(n)) + ")"
}

func elementIndex(n *html.Node) int {
	i := 0
	for c := n.Parent.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			i++
		}
		if c == n {
			return i
		}
	}
	return 0
}

func outerHTML(n *html.Node) string { return dom.OuterHTML(n) }

func innerHTML(n *html.Node) string { return dom.InnerHTML(n) }

func (ev *evaluator) mustMutable(el *Element, method string) error {
	if !el.Mutable {
		return typeErr("%s() cannot modify a queried element (clone() it first)", method)
	}
	return nil
}

// toNodes converts a value to nodes to insert (R-SESSEL-235): a queried
// element is deep-copied, a constructed one is moved.
func (ev *evaluator) toNodes(v Value, top bool) ([]*html.Node, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case *Element:
		if !x.Mutable {
			return []*html.Node{dom.Clone(x.Node)}, ev.r.alloc(256)
		}
		return []*html.Node{x.Node}, nil
	case string:
		if x == "" {
			return nil, nil
		}
		return []*html.Node{{Type: html.TextNode, Data: x}}, ev.r.alloc(len(x))
	case bool, int64, float64, temporal, *SelectorValue:
		return []*html.Node{{Type: html.TextNode, Data: TextOf(x)}}, nil
	case List:
		if !top {
			return nil, typeErr("nested lists cannot be inserted as children")
		}
		var out []*html.Node
		for _, it := range x {
			ns, err := ev.toNodes(it, false)
			if err != nil {
				return nil, err
			}
			out = append(out, ns...)
		}
		return out, nil
	}
	return nil, typeErr("cannot insert %s as a child", TypeName(v))
}

func insertNodes(parent, before *html.Node, nodes []*html.Node) error {
	for _, c := range nodes {
		if dom.IsAncestor(c, parent) {
			return typeErr("cannot insert an element into itself")
		}
	}
	for _, c := range nodes {
		if c.Parent != nil {
			c.Parent.RemoveChild(c)
		}
		parent.InsertBefore(c, before)
	}
	return nil
}

// setValue implements the .value(x) setter (R-SESSEL-234).
func setValue(n *html.Node, v Value) {
	if a := valueAttr(n); a != "" {
		if v == nil {
			removeAttr(n, a)
			return
		}
		setAttr(n, a, TextOf(v))
		return
	}
	dom.SetTextContent(n, TextOf(v))
}

// elementMethod dispatches Element methods (§12). handled is false for an
// unknown name.
func (ev *evaluator) elementMethod(el *Element, name string, args []Value) (Value, bool, error) {
	n := el.Node
	argc := len(args)
	switch name {
	case "text", "set_text":
		if name == "text" && argc == 0 {
			// Never null; a stored element's text is whitespace-collapsed
			// and trimmed (live 2026-09-29).
			t, _ := elementText(n).(string)
			if !el.Mutable {
				t = collapseSpace(t)
			}
			return t, true, nil
		}
		if argc != 1 {
			return nil, true, typeErr("%s() takes one argument", name)
		}
		if !el.Mutable && name == "text" {
			return nil, true, typeErr("type error: text(value) setter requires a constructed element, got element")
		}
		if err := ev.mustMutable(el, name); err != nil {
			return nil, true, err
		}
		dom.SetTextContent(n, TextOf(args[0]))
		return el, true, nil
	case "value":
		if argc == 0 {
			if !el.Mutable && valueAttr(n) == "" {
				// A stored text-content element: collapsed and trimmed,
				// null when empty (live 2026-09-29).
				if t := collapseSpace(elementValueString(n)); t != "" {
					return t, true, nil
				}
				return nil, true, nil
			}
			return elementValue(n), true, nil
		}
		if err := ev.mustMutable(el, name); err != nil {
			return nil, true, err
		}
		setValue(n, args[0])
		return el, true, nil
	case "attr", "set_attr":
		if argc == 0 || argc > 2 {
			return nil, true, typeErr("%s() takes a name and optionally a value", name)
		}
		an, ok := args[0].(string)
		if !ok {
			return nil, true, typeErr("attribute names are Strings, not %s", TypeName(args[0]))
		}
		if name == "attr" && argc == 1 {
			if v, ok := getAttr(n, an); ok {
				return v, true, nil
			}
			return nil, true, nil
		}
		if argc != 2 {
			return nil, true, typeErr("set_attr() takes a name and a value")
		}
		if err := ev.mustMutable(el, name); err != nil {
			return nil, true, err
		}
		if args[1] == nil {
			removeAttr(n, an)
		} else {
			setAttr(n, an, TextOf(args[1]))
		}
		return el, true, nil
	case "path":
		if el.Doc == nil || ev.env.DocumentOnly {
			return nil, true, nil
		}
		return el.Doc.Path, true, nil
	case "document":
		if el.Doc == nil || ev.env.DocumentOnly {
			return nil, true, nil
		}
		return el.Doc.Element(), true, nil
	case "metadata":
		if el.Doc != nil && el.Doc.Meta != nil && el.isDocRoot() {
			return el.Doc.Meta.Copy(), true, nil
		}
		return nil, true, nil
	case "selector":
		return selectorOf(n), true, nil
	case "clone":
		c := dom.Clone(n)
		return &Element{Node: c, Mutable: true, Class: el.Class}, true, ev.r.alloc(256)
	case "children":
		out := List{}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				out = append(out, el.derive(c))
			}
		}
		return out, true, nil
	case "parent":
		p := parentElement(n)
		if p == nil {
			return nil, true, nil
		}
		return el.derive(p), true, nil
	case "content":
		if !selector.IsTemplate(n) {
			return nil, true, nil
		}
		holder := &html.Node{Type: html.DocumentNode}
		out := List{}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			cc := dom.Clone(c)
			holder.AppendChild(cc)
			if cc.Type == html.ElementNode {
				out = append(out, &Element{Node: cc, Mutable: true})
			}
		}
		return out, true, ev.r.alloc(256)
	case "getHTML", "innerhtml", "innerHTML":
		return innerHTML(n), true, nil
	case "outerHTML", "outerhtml":
		return outerHTML(n), true, nil
	case "microdata":
		return ev.microdata(el), true, nil
	case "setHTML":
		if argc != 1 {
			return nil, true, typeErr("setHTML() takes one argument")
		}
		if err := ev.mustMutable(el, name); err != nil {
			return nil, true, err
		}
		nodes, err := dom.ParseFragment(TextOf(args[0]), n)
		if err != nil {
			return nil, true, typeErr("setHTML(): %v", err)
		}
		dom.RemoveChildren(n)
		for _, c := range nodes {
			n.AppendChild(c)
		}
		return el, true, nil
	case "empty":
		if err := ev.mustMutable(el, name); err != nil {
			return nil, true, err
		}
		dom.RemoveChildren(n)
		return el, true, nil
	case "remove":
		if !el.Mutable {
			return nil, true, nil // a no-op on a stored element (live 2026-09-29)
		}
		if n.Parent != nil {
			n.Parent.RemoveChild(n)
		}
		return nil, true, nil
	case "append", "prepend":
		if argc != 1 {
			return nil, true, typeErr("%s() takes one argument", name)
		}
		if err := ev.mustMutable(el, name); err != nil {
			return nil, true, err
		}
		nodes, err := ev.toNodes(args[0], true)
		if err != nil {
			return nil, true, err
		}
		before := (*html.Node)(nil)
		if name == "prepend" {
			before = n.FirstChild
		}
		if err := insertNodes(n, before, nodes); err != nil {
			return nil, true, err
		}
		return el, true, nil
	case "replaceWith":
		if argc != 1 {
			return nil, true, typeErr("replaceWith() takes one argument")
		}
		if !el.Mutable {
			return nil, true, typeErr("type error: replaceWith() cannot be used on stored (immutable) elements")
		}
		if n.Parent == nil {
			return nil, true, typeErr("replaceWith(): the element has no parent")
		}
		nodes, err := ev.toNodes(args[0], true)
		if err != nil {
			return nil, true, err
		}
		parent := n.Parent
		if err := insertNodes(parent, n, nodes); err != nil {
			return nil, true, err
		}
		parent.RemoveChild(n)
		if r, ok := args[0].(*Element); ok && !r.Mutable && len(nodes) == 1 {
			return &Element{Node: nodes[0], Mutable: true}, true, nil
		}
		return args[0], true, nil
	case "insertBefore":
		if argc != 1 {
			return nil, true, typeErr("insertBefore() takes one argument")
		}
		ref, ok := args[0].(*Element)
		if !ok {
			return nil, true, typeErr("insertBefore() needs an Element, not %s", TypeName(args[0]))
		}
		if err := ev.mustMutable(el, name); err != nil {
			return nil, true, err
		}
		if !ref.Mutable {
			return nil, true, typeErr("type error: insertBefore() reference must be a mutable child element, not a stored element")
		}
		if ref.Node.Parent == nil {
			return nil, true, typeErr("insertBefore(): the reference element has no parent")
		}
		if err := insertNodes(ref.Node.Parent, ref.Node, []*html.Node{n}); err != nil {
			return nil, true, err
		}
		return el, true, nil
	}
	return nil, false, nil
}

// fromString implements Element.fromString (R-SESSEL-243).
func fromString(s string) (Value, error) {
	nodes, err := dom.ParseBodyFragment(s)
	if err != nil {
		return nil, typeErr("Element.fromString(): %v", err)
	}
	for _, c := range nodes {
		if c.Type == html.ElementNode {
			if c.Parent != nil {
				c.Parent.RemoveChild(c)
			}
			return &Element{Node: c, Mutable: true}, nil
		}
	}
	return nil, typeErr("Element.fromString(): the input contains no element")
}

// parseDocument implements Document.parse (R-SESSEL-244).
func parseDocument(s string) (Value, error) {
	t := strings.ToLower(strings.TrimLeft(s, " \t\r\n\f"))
	if strings.HasPrefix(t, "<!doctype") || strings.HasPrefix(t, "<html") {
		doc, err := dom.Parse([]byte(s))
		if err != nil {
			return nil, typeErr("Document.parse(): %v", err)
		}
		root := documentElement(doc)
		if root == nil {
			return nil, typeErr("Document.parse(): no root element")
		}
		return &Element{Node: root, Mutable: true}, nil
	}
	nodes, err := dom.ParseBodyFragment(s)
	if err != nil {
		return nil, typeErr("Document.parse(): %v", err)
	}
	var elems []*html.Node
	for _, c := range nodes {
		if c.Type == html.ElementNode {
			elems = append(elems, c)
		}
	}
	if len(elems) == 1 && len(nodes) == 1 {
		return &Element{Node: elems[0], Mutable: true}, nil
	}
	body := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	for _, c := range nodes {
		if c.Parent != nil {
			c.Parent.RemoveChild(c)
		}
		body.AppendChild(c)
	}
	return &Element{Node: body, Mutable: true}, nil
}
