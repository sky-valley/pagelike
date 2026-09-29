package jsrt

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/xmldom"
)

// domState is the worker-side DOM of one evaluation: every node JavaScript
// can reach has an integer handle, and the prelude's node classes are thin
// wrappers that call op with it. Nodes are x/net/html nodes, parsed and
// serialized with internal/dom and matched with internal/selector, exactly
// as on the host.
//
// Documents are DocumentNodes registered in docs. A DocumentFragment is a
// DocumentNode registered in frags. Read-only trees (the ambient document of
// a read-only slot, read-only element values) are recorded by their root in
// roRoots; since nothing can be inserted into or removed from them, a node
// is read-only exactly when its root is.
type domState struct {
	nodes   []*html.Node
	ids     map[*html.Node]int
	docs    map[*html.Node]*docInfo
	frags   map[*html.Node]bool
	roRoots map[*html.Node]bool
	owner   map[*html.Node]*html.Node // detached subtree root / created node → document
	nsOf    map[*html.Node]string     // createElementNS in an HTML document
	sels    map[string]*selector.Selector
	selOpts *selector.Options // PageLove options plus the "|x" pseudo-class (compile)

	ambient      *html.Node
	ambientDirty bool
	inheritedNS  map[string]string // xmlns declarations above an element-rooted view

	errName, errMsg string
	ops             int
	maxOps          int
	opsExceeded     bool
	onOpsExceeded   func()
	alloc, maxAlloc int64
	oom             bool
}

type docInfo struct {
	xml    bool
	source string
}

func newDOMState(maxAlloc int64, maxOps int) *domState {
	return &domState{
		ids:      map[*html.Node]int{},
		docs:     map[*html.Node]*docInfo{},
		frags:    map[*html.Node]bool{},
		roRoots:  map[*html.Node]bool{},
		owner:    map[*html.Node]*html.Node{},
		nsOf:     map[*html.Node]string{},
		sels:     map[string]*selector.Selector{},
		maxAlloc: maxAlloc,
		maxOps:   maxOps,
	}
}

// Node kinds reported to the prelude (negative: not an element).
const (
	kindText     = -3
	kindComment  = -8
	kindDocument = -9
	kindDoctype  = -10
	kindFragment = -11
	kindPI       = -7
)

// errSentinel is what op returns after recording an error; the prelude
// then asks for the error with opErr and throws it.
const errSentinel = "\x00"

type domErr struct{ name, msg string }

func (e *domErr) Error() string { return e.name + ": " + e.msg }

func domError(name, msg string) error { return &domErr{name, msg} }

var errReadOnly = domError("NoModificationAllowedError", "document is read-only in this binding context")

func (d *domState) handle(n *html.Node) int {
	if n == nil {
		return -1
	}
	if h, ok := d.ids[n]; ok {
		return h
	}
	d.ids[n] = len(d.nodes)
	d.nodes = append(d.nodes, n)
	return len(d.nodes) - 1
}

func (d *domState) node(v any) (*html.Node, error) {
	h, ok := toInt(v)
	if !ok || h < 0 || h >= len(d.nodes) {
		return nil, domError("TypeError", "argument is not a Node")
	}
	return d.nodes[h], nil
}

func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		if x == float64(int(x)) {
			return int(x), true
		}
	}
	return 0, false
}

func toStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

// charge accounts DOM memory against the evaluation's memory budget.
func (d *domState) charge(n int64) error {
	d.alloc += n
	if d.maxAlloc > 0 && d.alloc > d.maxAlloc {
		d.oom = true
		return domError("InternalError", "out of memory")
	}
	return nil
}

// ------------------------------------------------------------ tree helpers

func rootOf(n *html.Node) *html.Node {
	for n.Parent != nil {
		n = n.Parent
	}
	return n
}

func (d *domState) isDoc(n *html.Node) bool {
	_, ok := d.docs[n]
	return ok
}

func (d *domState) isFrag(n *html.Node) bool { return d.frags[n] }

// ownerDoc is the document a node belongs to (the document itself for a
// document).
func (d *domState) ownerDoc(n *html.Node) *html.Node {
	r := rootOf(n)
	if d.isDoc(r) {
		return r
	}
	if o := d.owner[r]; o != nil {
		return o
	}
	if o := d.owner[n]; o != nil {
		return o
	}
	return d.ambient
}

func (d *domState) readOnly(n *html.Node) bool { return len(d.roRoots) > 0 && d.roRoots[rootOf(n)] }

func (d *domState) xmlDoc(n *html.Node) bool {
	if di := d.docs[d.ownerDoc(n)]; di != nil {
		return di.xml
	}
	return false
}

// mutated records a change to a (writable) ambient tree.
func (d *domState) mutated(n *html.Node) {
	if d.ambient != nil && rootOf(n) == d.ambient {
		d.ambientDirty = true
	}
}

// detach removes n from its parent, remembering its document.
func (d *domState) detach(n *html.Node) {
	if n.Parent == nil {
		return
	}
	d.owner[n] = d.ownerDoc(n)
	d.mutated(n.Parent)
	n.Parent.RemoveChild(n)
}

// htmlish reports an element in the HTML namespace of an HTML document:
// HTML-parsed (including foreign content, which pagelike stores without a
// namespace, R-JS-64) or made by createElement.
func (d *domState) htmlish(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	if _, ok := d.nsOf[n]; ok {
		return false
	}
	return n.Namespace == "" || n.Namespace == "svg" || n.Namespace == "math"
}

func asciiUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func (d *domState) tagName(n *html.Node) string {
	if d.htmlish(n) {
		return asciiUpper(n.Data)
	}
	return n.Data
}

func localName(n *html.Node) string {
	if _, after, ok := strings.Cut(n.Data, ":"); ok {
		return after
	}
	return n.Data
}

func prefixOf(n *html.Node) (string, bool) {
	before, _, ok := strings.Cut(n.Data, ":")
	return before, ok
}

// nsURI is namespaceURI (R-JS-68).
func (d *domState) nsURI(n *html.Node) (string, bool) {
	if ns, ok := d.nsOf[n]; ok {
		return ns, ns != ""
	}
	if n.Namespace != "" && n.Namespace != "svg" && n.Namespace != "math" {
		if n.Namespace == xmldom.NoNamespace {
			return "", false
		}
		return n.Namespace, true
	}
	if p, ok := prefixOf(n); ok {
		for e := n; e != nil; e = e.Parent {
			if e.Type != html.ElementNode {
				continue
			}
			if v, ok := attrGet(e, "xmlns:"+p, true); ok {
				return v, true
			}
		}
		if rootOf(n) == d.ambient {
			if v, ok := d.inheritedNS[p]; ok {
				return v, true
			}
		}
	}
	return "", false
}

// ifaceIndex picks an element's class (R-JS-64): per-tag interface for HTML
// elements when one exists, HTMLElement otherwise, Element for namespaced
// ones.
func (d *domState) ifaceIndex(n *html.Node) int {
	if !d.htmlish(n) {
		return 0
	}
	if i, ok := ifaceByTag[n.Data]; ok {
		return i
	}
	return 1
}

func (d *domState) kind(n *html.Node) int {
	switch n.Type {
	case html.ElementNode:
		return d.ifaceIndex(n)
	case html.TextNode:
		return kindText
	case html.CommentNode:
		return kindComment
	case html.DoctypeNode:
		return kindDoctype
	case html.DocumentNode:
		if d.isFrag(n) {
			return kindFragment
		}
		return kindDocument
	case html.RawNode:
		if strings.HasPrefix(n.Data, "<!") {
			return kindDoctype
		}
		return kindPI
	}
	return kindText
}

func (d *domState) nodeType(n *html.Node) int {
	switch k := d.kind(n); {
	case k >= 0:
		return 1
	default:
		return -k
	}
}

func (d *domState) nodeName(n *html.Node) string {
	switch n.Type {
	case html.ElementNode:
		return d.tagName(n)
	case html.TextNode:
		return "#text"
	case html.CommentNode:
		return "#comment"
	case html.DoctypeNode:
		return n.Data
	case html.DocumentNode:
		if d.isFrag(n) {
			return "#document-fragment"
		}
		return "#document"
	case html.RawNode:
		s := strings.TrimPrefix(strings.TrimPrefix(n.Data, "<?"), "<!")
		if strings.HasPrefix(strings.ToUpper(s), "DOCTYPE") {
			f := strings.Fields(s[len("DOCTYPE"):])
			if len(f) > 0 {
				return strings.TrimSuffix(f[0], ">")
			}
			return ""
		}
		f := strings.Fields(s)
		if len(f) > 0 {
			return strings.TrimSuffix(strings.TrimSuffix(f[0], ">"), "?")
		}
	}
	return ""
}

// attrName is an attribute's qualified name.
func attrName(a html.Attribute) string {
	switch a.Namespace {
	case "":
		return a.Key
	case "xmlns":
		if a.Key == "xmlns" {
			return "xmlns"
		}
		return "xmlns:" + a.Key
	}
	return a.Namespace + ":" + a.Key
}

func attrIndex(n *html.Node, name string, fold bool) int {
	for i, a := range n.Attr {
		q := attrName(a)
		if q == name || fold && strings.EqualFold(q, name) {
			return i
		}
	}
	return -1
}

func attrGet(n *html.Node, name string, exact bool) (string, bool) {
	if i := attrIndex(n, name, !exact); i >= 0 {
		return n.Attr[i].Val, true
	}
	return "", false
}

// validName rejects names that would change the markup when serialized.
func validName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r <= ' ', r == '"', r == '\'', r == '>', r == '/', r == '=', r == '<', r == 0x7f:
			return false
		}
	}
	return true
}

// validateQName applies createElementNS/setAttributeNS's NamespaceError
// rules (R-JS-65): empty, more than one ':', a leading or trailing ':', or a
// prefix without a namespace. The reserved xml/xmlns checks are not applied.
func validateQName(ns string, nsNull bool, q string) error {
	if q == "" || strings.Count(q, ":") > 1 || strings.HasPrefix(q, ":") || strings.HasSuffix(q, ":") {
		return domError("NamespaceError", fmt.Sprintf("'%s' is not a valid qualified name", q))
	}
	if !validName(q) {
		return domError("InvalidCharacterError", fmt.Sprintf("'%s' contains invalid characters", q))
	}
	if strings.Contains(q, ":") && (nsNull || ns == "") {
		return domError("NamespaceError", "a prefixed name needs a namespace")
	}
	return nil
}

// ------------------------------------------------------------ parsing

// parseFragment parses markup as children of ctx without trimming
// whitespace (dom.ParseFragment trims; innerHTML must not).
func parseFragment(markup string, ctx *html.Node) ([]*html.Node, error) {
	trimmed := dom.TrimHTMLSpace(markup)
	lead := markup[:strings.Index(markup, trimmed)]
	if trimmed == "" {
		lead = markup
	}
	trail := markup[len(lead)+len(trimmed):]
	var nodes []*html.Node
	if trimmed != "" {
		var err error
		nodes, err = dom.ParseFragment(trimmed, ctx)
		if err != nil {
			return nil, err
		}
	}
	if lead != "" {
		if len(nodes) > 0 && nodes[0].Type == html.TextNode {
			nodes[0].Data = lead + nodes[0].Data
		} else {
			nodes = append([]*html.Node{{Type: html.TextNode, Data: lead}}, nodes...)
		}
	}
	if trail != "" {
		if k := len(nodes); k > 0 && nodes[k-1].Type == html.TextNode {
			nodes[k-1].Data += trail
		} else {
			nodes = append(nodes, &html.Node{Type: html.TextNode, Data: trail})
		}
	}
	return nodes, nil
}

func contextElement(tag string, xml bool) *html.Node {
	if tag == "" {
		if xml {
			tag = "root"
		} else {
			tag = "body"
		}
	}
	n := &html.Node{Type: html.ElementNode, Data: tag, DataAtom: atom.Lookup([]byte(tag))}
	if xml {
		n.Namespace = xmldom.NoNamespace
		n.DataAtom = 0
	}
	return n
}

func weight(n *html.Node) int64 {
	var w int64
	dom.Walk(n, func(x *html.Node) bool {
		w += 96 + int64(len(x.Data))
		for _, a := range x.Attr {
			w += 32 + int64(len(a.Key)+len(a.Val))
		}
		return true
	})
	return w
}

// loadDocument parses the ambient document.
func (d *domState) loadDocument(wd *wireDoc, readOnly bool) (*html.Node, map[int]*html.Node, error) {
	var doc *html.Node
	var err error
	if wd.Root {
		ctx := contextElement(wd.Context, wd.XML)
		var nodes []*html.Node
		nodes, err = dom.ParseFragment(wd.HTML, ctx)
		if err != nil {
			return nil, nil, err
		}
		doc = &html.Node{Type: html.DocumentNode}
		for _, n := range dom.Elements(nodes) {
			doc.AppendChild(n)
			break
		}
		if doc.FirstChild == nil {
			return nil, nil, errors.New("the document element did not parse")
		}
	} else if wd.XML {
		doc, err = dom.ParseXML([]byte(wd.HTML))
	} else {
		doc, err = dom.Parse([]byte(wd.HTML))
	}
	if err != nil {
		return nil, nil, err
	}
	refs := map[int]*html.Node{}
	if wd.Root {
		refs[1] = doc.FirstChild
	}
	if wd.Marked {
		dom.Walk(doc, func(n *html.Node) bool {
			if n.Type == html.ElementNode {
				if i := attrIndex(n, refAttr, false); i >= 0 {
					var id int
					fmt.Sscan(n.Attr[i].Val, &id)
					refs[id] = n
					n.Attr = append(n.Attr[:i:i], n.Attr[i+1:]...)
				}
			}
			return true
		})
	}
	d.docs[doc] = &docInfo{xml: wd.XML || xmldom.IsXML(doc), source: wd.Source}
	d.ambient = doc
	d.inheritedNS = wd.NS
	if readOnly {
		d.roRoots[doc] = true
	}
	return doc, refs, nil
}

// contextFor picks a parse context for element markup that came without
// one: table parts need their table context to survive parsing.
func contextFor(markup string) string {
	z := html.NewTokenizer(strings.NewReader(markup))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return ""
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "tr":
				return "tbody"
			case "td", "th":
				return "tr"
			case "tbody", "thead", "tfoot", "caption", "colgroup":
				return "table"
			case "col":
				return "colgroup"
			case "option", "optgroup":
				return "select"
			case "html":
				return ""
			case "head", "body":
				return "html"
			}
			return ""
		}
	}
}

// loadElement parses a self-contained element value into a container
// document of its own.
func (d *domState) loadElement(we wireElem) (*html.Node, error) {
	if we.Context == "" && !we.XML {
		we.Context = contextFor(we.HTML)
	}
	ctx := contextElement(we.Context, we.XML)
	nodes, err := dom.ParseFragment(we.HTML, ctx)
	if err != nil {
		return nil, err
	}
	els := dom.Elements(nodes)
	if len(els) == 0 {
		return nil, errors.New("element value did not parse as an element")
	}
	doc := &html.Node{Type: html.DocumentNode}
	doc.AppendChild(els[0])
	d.docs[doc] = &docInfo{xml: we.XML, source: we.Source}
	if !we.Writable {
		d.roRoots[doc] = true
	}
	return els[0], nil
}

// sourceOf is the $source of an element returned by the binding.
func (d *domState) sourceOf(n *html.Node) string {
	if di := d.docs[rootOf(n)]; di != nil {
		return di.source
	}
	return ""
}

// ------------------------------------------------------------ selectors

// compile parses a DOM selector argument (R-JS-73). Namespace-pipe
// selectors (live 2026-09-29): "*|x" is x in any namespace, "|x" is x
// without a namespace (every HTML-parsed element; not one made by
// createElementNS with a namespace), and a named prefix ("svg|g") is a
// SyntaxError, since no prefix can be declared.
func (d *domState) compile(sel string) (*selector.Selector, error) {
	if s, ok := d.sels[sel]; ok {
		return s, nil
	}
	src := rewriteNamespacePipes(sel)
	if d.selOpts == nil {
		d.selOpts = selector.PageLoveOptions(selector.ExtOptions{})
		d.selOpts.Pseudo[noNamespacePseudo] = func(*selector.PseudoContext) (selector.Sel, error) {
			return noNamespace{d}, nil
		}
	}
	s, err := selector.CompileOptions(src, d.selOpts)
	if err != nil {
		return nil, domError("SyntaxError", fmt.Sprintf("'%s' is not a valid selector", sel))
	}
	d.sels[sel] = s
	return s, nil
}

// noNamespacePseudo is the private pseudo-class "|x" compiles to.
const noNamespacePseudo = "-pagelike-no-namespace"

// noNamespace matches elements without a namespace (the "|" prefix).
type noNamespace struct{ d *domState }

func (m noNamespace) Match(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	_, ns := m.d.nsURI(n)
	return !ns
}
func (noNamespace) Specificity() selector.Specificity { return selector.Specificity{} }
func (noNamespace) PseudoElement() string             { return "" }
func (noNamespace) String() string                    { return ":" + noNamespacePseudo }

// rewriteNamespacePipes rewrites the namespace-pipe forms outside strings:
// "*|x" → "x" (type or attribute position), "|x" → "x:-pagelike-no-namespace"
// in type position, "[|a" → "[a". A named prefix ("svg|g") is left alone, so
// the selector parser rejects it; "|=" (dash-match) is untouched.
func rewriteNamespacePipes(s string) string {
	if !strings.Contains(s, "|") {
		return s
	}
	var b strings.Builder
	var quote byte
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' && i+1 < len(s) {
				b.WriteByte(c)
				i++
				c = s[i]
			} else if c == quote {
				quote = 0
			}
		case c == '\\' && i+1 < len(s):
			b.WriteByte(c)
			i++
			c = s[i]
		case c == '"' || c == '\'':
			quote = c
		case c == '[':
			depth++
		case c == ']':
			depth--
		case c == '*' && i+1 < len(s) && s[i+1] == '|' && (i+2 >= len(s) || s[i+2] != '='):
			i++ // "*|" → ""
			continue
		case c == '|' && (i+1 >= len(s) || s[i+1] != '=') && (i == 0 || !isNameByte(s[i-1]) && s[i-1] != '*' && s[i-1] != '|'):
			// "|x": x without a namespace.
			j := i + 1
			for j < len(s) && (isNameByte(s[j]) || s[j] == '*' || s[j] == '\\') {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j, len(s))
			b.WriteString(s[i+1 : j])
			if depth == 0 {
				b.WriteString(":" + noNamespacePseudo)
			}
			i = j - 1
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isNameByte(c byte) bool {
	return c == '-' || c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// queryRoot is the scope of querySelector(All) on n: the whole document n
// belongs to (R-JS-71), or the subtree of its topmost ancestor when
// detached.
func queryRoot(n *html.Node) *html.Node { return rootOf(n) }

// ------------------------------------------------------------ mutation

// checkInsert validates inserting node into parent (R-JS-61, R-JS-66,
// R-JS-77).
func (d *domState) checkInsert(parent, node *html.Node) error {
	if d.readOnly(parent) {
		return errReadOnly
	}
	switch parent.Type {
	case html.ElementNode, html.DocumentNode:
	default:
		return domError("HierarchyRequestError", "this node type does not support children")
	}
	if node.Type == html.DocumentNode && !d.isFrag(node) {
		return domError("HierarchyRequestError", "a document cannot be inserted")
	}
	if dom.IsAncestor(node, parent) {
		return domError("HierarchyRequestError", "the new child is an ancestor of the parent")
	}
	if d.readOnly(node) {
		return errReadOnly
	}
	if (node.Type == html.DoctypeNode) && !d.isDoc(parent) {
		return domError("HierarchyRequestError", "a doctype can only be a child of a document")
	}
	// A document may hold text and several elements (live 2026-09-29:
	// PageLove accepts appendChild of a second element or of text on a
	// parsed document, which is how parseFromString leaves fragments).
	return nil
}

// prepare turns a node about to be inserted under parent into the list of
// nodes actually inserted: a fragment's children, and a deep copy of a node
// from another document (R-JS-66).
func (d *domState) prepare(parent, node *html.Node) ([]*html.Node, error) {
	target := d.ownerDoc(parent)
	foreign := d.ownerDoc(node) != target
	var list []*html.Node
	if d.isFrag(node) {
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			list = append(list, c)
		}
		if foreign {
			for i, c := range list {
				list[i] = d.clone(c, true, target)
			}
		}
		return list, nil
	}
	if foreign {
		cp := d.clone(node, true, target)
		if err := d.charge(weight(cp)); err != nil {
			return nil, err
		}
		return []*html.Node{cp}, nil
	}
	return []*html.Node{node}, nil
}

// insert places nodes under parent before ref (nil = append).
func (d *domState) insert(parent *html.Node, nodes []*html.Node, ref *html.Node) {
	for _, n := range nodes {
		if n == ref {
			continue
		}
		if n.Parent != nil {
			d.mutated(n.Parent)
			n.Parent.RemoveChild(n)
		}
		parent.InsertBefore(n, ref)
		delete(d.owner, n)
	}
	d.mutated(parent)
}

func (d *domState) clone(n *html.Node, deep bool, owner *html.Node) *html.Node {
	c := &html.Node{Type: n.Type, DataAtom: n.DataAtom, Data: n.Data, Namespace: n.Namespace}
	c.Attr = append([]html.Attribute(nil), n.Attr...)
	if ns, ok := d.nsOf[n]; ok {
		d.nsOf[c] = ns
	}
	if d.isFrag(n) {
		d.frags[c] = true
	}
	if di, ok := d.docs[n]; ok {
		d.docs[c] = &docInfo{xml: di.xml}
	}
	if deep {
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			cc := d.clone(ch, true, owner)
			c.AppendChild(cc)
		}
	}
	if owner != nil {
		d.owner[c] = owner
	}
	return c
}

func (d *domState) setText(n *html.Node, s string) error {
	switch n.Type {
	case html.TextNode, html.CommentNode:
		if d.readOnly(n) {
			return errReadOnly
		}
		if err := d.charge(int64(len(s))); err != nil {
			return err
		}
		n.Data = s
		d.mutated(n)
	case html.ElementNode, html.DocumentNode:
		if d.isDoc(n) {
			return nil
		}
		if d.readOnly(n) {
			return errReadOnly
		}
		if err := d.charge(int64(96 + len(s))); err != nil {
			return err
		}
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			d.detach(c)
			c = next
		}
		if s != "" {
			n.AppendChild(&html.Node{Type: html.TextNode, Data: s})
		}
		d.mutated(n)
	}
	return nil
}

func (d *domState) parseInto(markup string, ctx *html.Node) ([]*html.Node, error) {
	if err := d.charge(int64(3 * len(markup))); err != nil {
		return nil, err
	}
	nodes, err := parseFragment(markup, ctx)
	if err != nil {
		return nil, domError("SyntaxError", err.Error())
	}
	return nodes, nil
}

// ------------------------------------------------------------ the host op

// DOM operation codes shared with the prelude.
const (
	opErr = iota
	opKind
	opParent
	opFirst
	opLast
	opPrev
	opNext
	opChildNodes
	opChildren
	opNodeName
	opText
	opSetText
	opData
	opSetData
	opLength
	opOwnerDoc
	opAppend
	opInsertBefore
	opRemoveChild
	opReplaceChild
	opClone
	opContains
	opTag
	opLocalName
	opPrefix
	opNSURI
	opGetAttr
	opHasAttr
	opSetAttr
	opRemoveAttr
	opGetAttrNS
	opSetAttrNS
	opRemoveAttrNS
	opInnerHTML
	opSetInnerHTML
	opOuterHTML
	opSetOuterHTML
	opInsertAdjacent
	opQS
	opQSA
	opByID
	opByTagNS
	opClosest
	opMatches
	opCreateElement
	opCreateElementNS
	opCreateText
	opCreateComment
	opCreateFragment
	opDocElement
	opHead
	opBody
	opParse
	opSerialize
	opInsertNodes
	opRemove
	opFirstEl
	opLastEl
	opNextEl
	opPrevEl
	opElCount
	opNodeType
	opHasChildren
	opParentEl
)

// op is the __pl_dom host function: op(code, handle, args…).
func (d *domState) op(args []any) (any, error) {
	d.ops++
	if d.maxOps > 0 && d.ops > d.maxOps {
		d.opsExceeded = true
		if d.onOpsExceeded != nil {
			d.onOpsExceeded()
		}
		d.errName, d.errMsg = "InternalError", "DOM operation budget exhausted"
		return errSentinel, nil
	}
	if len(args) == 0 {
		return nil, errors.New("missing op")
	}
	code, _ := toInt(args[0])
	if code == opErr {
		return []any{d.errName, d.errMsg}, nil
	}
	r, err := d.do(code, args[1:])
	if err != nil {
		var de *domErr
		if errors.As(err, &de) {
			d.errName, d.errMsg = de.name, de.msg
		} else {
			d.errName, d.errMsg = "TypeError", err.Error()
		}
		return errSentinel, nil
	}
	return r, nil
}

func arg(a []any, i int) any {
	if i < len(a) {
		return a[i]
	}
	return nil
}

func (d *domState) nodes1(a []any) (*html.Node, error) { return d.node(arg(a, 0)) }

func handles(d *domState, ns []*html.Node) []any {
	out := make([]any, len(ns))
	for i, n := range ns {
		out[i] = d.handle(n)
	}
	return out
}

func (d *domState) do(code int, a []any) (any, error) {
	if code == opParse {
		return d.parseDocument(toStr(arg(a, 0)))
	}
	n, err := d.nodes1(a)
	if err != nil {
		return nil, err
	}
	switch code {
	case opKind:
		return d.kind(n), nil
	case opNodeType:
		return d.nodeType(n), nil
	case opParent:
		return d.handle(n.Parent), nil
	case opParentEl:
		if n.Parent != nil && n.Parent.Type == html.ElementNode {
			return d.handle(n.Parent), nil
		}
		return -1, nil
	case opFirst:
		return d.handle(n.FirstChild), nil
	case opLast:
		return d.handle(n.LastChild), nil
	case opPrev:
		return d.handle(n.PrevSibling), nil
	case opNext:
		return d.handle(n.NextSibling), nil
	case opHasChildren:
		return n.FirstChild != nil, nil
	case opChildNodes:
		var out []*html.Node
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			out = append(out, c)
		}
		return handles(d, out), nil
	case opChildren:
		return handles(d, dom.ElementChildren(n)), nil
	case opFirstEl:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				return d.handle(c), nil
			}
		}
		return -1, nil
	case opLastEl:
		for c := n.LastChild; c != nil; c = c.PrevSibling {
			if c.Type == html.ElementNode {
				return d.handle(c), nil
			}
		}
		return -1, nil
	case opNextEl:
		for c := n.NextSibling; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				return d.handle(c), nil
			}
		}
		return -1, nil
	case opPrevEl:
		for c := n.PrevSibling; c != nil; c = c.PrevSibling {
			if c.Type == html.ElementNode {
				return d.handle(c), nil
			}
		}
		return -1, nil
	case opElCount:
		return len(dom.ElementChildren(n)), nil
	case opNodeName:
		return d.nodeName(n), nil
	case opText:
		return dom.TextContent(n), nil
	case opSetText:
		return nil, d.setText(n, toStr(arg(a, 1)))
	case opData:
		return n.Data, nil
	case opSetData:
		return nil, d.setText(n, toStr(arg(a, 1)))
	case opLength:
		return utf8.RuneCountInString(n.Data), nil
	case opOwnerDoc:
		if d.isDoc(n) {
			return -1, nil
		}
		return d.handle(d.ownerDoc(n)), nil
	case opAppend, opInsertBefore:
		c, err := d.node(arg(a, 1))
		if err != nil {
			return nil, err
		}
		var ref *html.Node
		if code == opInsertBefore {
			if r := arg(a, 2); r != nil {
				if ref, err = d.node(r); err != nil {
					return nil, err
				}
			}
		}
		if err := d.checkInsert(n, c); err != nil {
			return nil, err
		}
		if ref != nil && ref.Parent != n {
			return nil, domError("NotFoundError", "the reference node is not a child of this node")
		}
		list, err := d.prepare(n, c)
		if err != nil {
			return nil, err
		}
		d.insert(n, list, ref)
		return d.handle(c), nil
	case opRemoveChild:
		c, err := d.node(arg(a, 1))
		if err != nil {
			return nil, err
		}
		if d.readOnly(n) {
			return nil, errReadOnly
		}
		if c.Parent != n {
			return nil, domError("NotFoundError", "the node to be removed is not a child of this node")
		}
		d.detach(c)
		return d.handle(c), nil
	case opReplaceChild:
		nw, err := d.node(arg(a, 1))
		if err != nil {
			return nil, err
		}
		old, err := d.node(arg(a, 2))
		if err != nil {
			return nil, err
		}
		if d.readOnly(n) {
			return nil, errReadOnly
		}
		if old.Parent != n {
			return nil, domError("NotFoundError", "the node to be replaced is not a child of this node")
		}
		if nw != old {
			if err := d.checkInsert(n, nw); err != nil {
				return nil, err
			}
			list, err := d.prepare(n, nw)
			if err != nil {
				return nil, err
			}
			ref := old.NextSibling
			for ref != nil && contains(list, ref) {
				ref = ref.NextSibling
			}
			d.detach(old)
			d.insert(n, list, ref)
		}
		return d.handle(old), nil
	case opClone:
		deep, _ := arg(a, 1).(bool)
		c := d.clone(n, deep, d.ownerDoc(n))
		if d.isDoc(n) {
			delete(d.owner, c)
		}
		if err := d.charge(weight(c)); err != nil {
			return nil, err
		}
		return d.handle(c), nil
	case opContains:
		o := arg(a, 1)
		if o == nil {
			return false, nil
		}
		other, err := d.node(o)
		if err != nil {
			return nil, err
		}
		return dom.IsAncestor(n, other), nil
	case opTag:
		return d.tagName(n), nil
	case opLocalName:
		return localName(n), nil
	case opPrefix:
		if p, ok := prefixOf(n); ok {
			return p, nil
		}
		return nil, nil
	case opNSURI:
		if ns, ok := d.nsURI(n); ok {
			return ns, nil
		}
		return nil, nil
	case opGetAttr:
		if v, ok := attrGet(n, toStr(arg(a, 1)), !d.htmlish(n)); ok {
			return v, nil
		}
		return nil, nil
	case opHasAttr:
		_, ok := attrGet(n, toStr(arg(a, 1)), !d.htmlish(n))
		return ok, nil
	case opSetAttr:
		return nil, d.setAttr(n, toStr(arg(a, 1)), toStr(arg(a, 2)), d.htmlish(n))
	case opRemoveAttr:
		if d.readOnly(n) {
			return nil, errReadOnly
		}
		if i := attrIndex(n, toStr(arg(a, 1)), d.htmlish(n)); i >= 0 {
			n.Attr = append(n.Attr[:i:i], n.Attr[i+1:]...)
			d.mutated(n)
		}
		return nil, nil
	case opGetAttrNS:
		q, ok := d.nsQName(n, arg(a, 1), toStr(arg(a, 2)))
		if !ok {
			return nil, nil
		}
		if v, ok := attrGet(n, q, true); ok {
			return v, nil
		}
		return nil, nil
	case opSetAttrNS:
		ns, nsNull := nsArg(arg(a, 1))
		q := toStr(arg(a, 2))
		if err := validateQName(ns, nsNull, q); err != nil {
			return nil, err
		}
		return nil, d.setAttr(n, q, toStr(arg(a, 3)), false)
	case opRemoveAttrNS:
		if d.readOnly(n) {
			return nil, errReadOnly
		}
		q, ok := d.nsQName(n, arg(a, 1), toStr(arg(a, 2)))
		if ok {
			if i := attrIndex(n, q, false); i >= 0 {
				n.Attr = append(n.Attr[:i:i], n.Attr[i+1:]...)
				d.mutated(n)
			}
		}
		return nil, nil
	case opInnerHTML:
		return dom.InnerHTML(n), nil
	case opSetInnerHTML:
		if d.readOnly(n) {
			return nil, errReadOnly
		}
		nodes, err := d.parseInto(toStr(arg(a, 1)), n)
		if err != nil {
			return nil, err
		}
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			d.detach(c)
			c = next
		}
		d.insert(n, nodes, nil)
		return nil, nil
	case opOuterHTML:
		return outerHTML(n), nil
	case opSetOuterHTML:
		p := n.Parent
		if p == nil {
			return nil, domError("NoModificationAllowedError", "the element has no parent element")
		}
		if d.readOnly(n) || d.readOnly(p) {
			return nil, errReadOnly
		}
		ctx := p
		if p.Type == html.DocumentNode {
			// A fragment's or a document's child (live 2026-09-29:
			// PageLove replaces a top-level element of a parsed document).
			ctx = contextElement("", d.xmlDoc(p))
		}
		nodes, err := d.parseInto(toStr(arg(a, 1)), ctx)
		if err != nil {
			return nil, err
		}
		ref := n.NextSibling
		d.detach(n)
		d.insert(p, nodes, ref)
		return nil, nil
	case opInsertAdjacent:
		return nil, d.insertAdjacent(n, toStr(arg(a, 1)), toStr(arg(a, 2)))
	case opQS, opQSA:
		s, err := d.compile(toStr(arg(a, 1)))
		if err != nil {
			return nil, err
		}
		root := queryRoot(n)
		if code == opQS {
			return d.handle(s.MatchFirst(root)), nil
		}
		return handles(d, s.MatchAll(root)), nil
	case opByID:
		id := toStr(arg(a, 1))
		var found *html.Node
		for c := n.FirstChild; c != nil && found == nil; c = c.NextSibling {
			dom.Walk(c, func(x *html.Node) bool {
				if found != nil {
					return false
				}
				if x.Type == html.ElementNode {
					if v, ok := attrGet(x, "id", true); ok && v == id {
						found = x
						return false
					}
				}
				return true
			})
		}
		return d.handle(found), nil
	case opByTagNS:
		ns, nsNull := nsArg(arg(a, 1))
		local := toStr(arg(a, 2))
		var out []*html.Node
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			dom.Walk(c, func(x *html.Node) bool {
				if x.Type != html.ElementNode {
					return true
				}
				if local != "*" && localName(x) != local {
					return true
				}
				if ns != "*" {
					uri, has := d.nsURI(x)
					if nsNull || ns == "" {
						if has {
							return true
						}
					} else if !has || uri != ns {
						return true
					}
				}
				out = append(out, x)
				return true
			})
		}
		return handles(d, out), nil
	case opClosest:
		s, err := d.compile(toStr(arg(a, 1)))
		if err != nil {
			return nil, err
		}
		for e := n; e != nil; e = e.Parent {
			if e.Type == html.ElementNode && s.Matches(e) {
				return d.handle(e), nil
			}
		}
		return -1, nil
	case opMatches:
		s, err := d.compile(toStr(arg(a, 1)))
		if err != nil {
			return nil, err
		}
		return s.Matches(n), nil
	case opCreateElement:
		tag := toStr(arg(a, 1))
		if !validName(tag) {
			return nil, domError("InvalidCharacterError", fmt.Sprintf("'%s' is not a valid element name", tag))
		}
		e := &html.Node{Type: html.ElementNode}
		if d.docs[n] != nil && d.docs[n].xml {
			e.Data, e.Namespace = tag, xmldom.NoNamespace
		} else {
			e.Data = asciiLower(tag)
			e.DataAtom = atom.Lookup([]byte(e.Data))
		}
		return d.created(n, e)
	case opCreateElementNS:
		ns, nsNull := nsArg(arg(a, 1))
		q := toStr(arg(a, 2))
		if err := validateQName(ns, nsNull, q); err != nil {
			return nil, err
		}
		e := &html.Node{Type: html.ElementNode, Data: q}
		if d.docs[n] != nil && d.docs[n].xml {
			e.Namespace = ns
			if ns == "" {
				e.Namespace = xmldom.NoNamespace
			}
		} else {
			d.nsOf[e] = ns
		}
		return d.created(n, e)
	case opCreateText:
		return d.created(n, &html.Node{Type: html.TextNode, Data: toStr(arg(a, 1))})
	case opCreateComment:
		return d.created(n, &html.Node{Type: html.CommentNode, Data: toStr(arg(a, 1))})
	case opCreateFragment:
		f := &html.Node{Type: html.DocumentNode}
		d.frags[f] = true
		return d.created(n, f)
	case opDocElement:
		return d.handle(dom.DocumentElement(n)), nil
	case opHead:
		return d.handle(firstTag(n, "head")), nil
	case opBody:
		return d.handle(firstTag(n, "body")), nil
	case opSerialize:
		return d.serialize(n), nil
	case opInsertNodes:
		mode := toStr(arg(a, 1))
		var list []*html.Node
		for _, x := range a[2:] {
			c, err := d.node(x)
			if err != nil {
				return nil, err
			}
			list = append(list, c)
		}
		return nil, d.insertNodes(n, mode, list)
	case opRemove:
		if n.Parent == nil {
			return nil, nil
		}
		if d.readOnly(n) {
			return nil, errReadOnly
		}
		d.detach(n)
		return nil, nil
	}
	return nil, fmt.Errorf("unknown DOM op %d", code)
}

func contains(list []*html.Node, n *html.Node) bool {
	for _, x := range list {
		if x == n {
			return true
		}
	}
	return false
}

func firstTag(doc *html.Node, tag string) *html.Node {
	var found *html.Node
	dom.Walk(doc, func(x *html.Node) bool {
		if found != nil {
			return false
		}
		if x.Type == html.ElementNode && x.Namespace == "" && x.Data == tag {
			found = x
			return false
		}
		return true
	})
	return found
}

// nsArg reads a namespace argument; JavaScript null (or undefined) is the
// null namespace.
func nsArg(v any) (ns string, isNull bool) {
	if v == nil {
		return "", true
	}
	return toStr(v), false
}

// nsQName resolves (ns, local) to the qualified attribute name through an
// in-scope xmlns:<p> declaration (R-JS-68).
func (d *domState) nsQName(n *html.Node, nsv any, local string) (string, bool) {
	ns, nsNull := nsArg(nsv)
	if nsNull || ns == "" {
		return local, true
	}
	for e := n; e != nil; e = e.Parent {
		if e.Type != html.ElementNode {
			continue
		}
		for _, at := range e.Attr {
			q := attrName(at)
			if strings.HasPrefix(q, "xmlns:") && at.Val == ns {
				return q[len("xmlns:"):] + ":" + local, true
			}
		}
	}
	if rootOf(n) == d.ambient {
		for p, uri := range d.inheritedNS {
			if uri == ns {
				return p + ":" + local, true
			}
		}
	}
	return "", false
}

func (d *domState) setAttr(n *html.Node, name, val string, fold bool) error {
	if n.Type != html.ElementNode {
		return domError("TypeError", "not an element")
	}
	if !validName(name) {
		return domError("InvalidCharacterError", fmt.Sprintf("'%s' is not a valid attribute name", name))
	}
	if d.readOnly(n) {
		return errReadOnly
	}
	if err := d.charge(int64(32 + len(name) + len(val))); err != nil {
		return err
	}
	if fold {
		name = asciiLower(name)
	}
	if i := attrIndex(n, name, fold); i >= 0 {
		n.Attr[i].Val = val
	} else {
		n.Attr = append(n.Attr, html.Attribute{Key: name, Val: val})
	}
	d.mutated(n)
	return nil
}

func (d *domState) created(docNode *html.Node, n *html.Node) (any, error) {
	if !d.isDoc(docNode) {
		return nil, domError("TypeError", "not a document")
	}
	if err := d.charge(96 + int64(len(n.Data))); err != nil {
		return nil, err
	}
	d.owner[n] = docNode
	return d.handle(n), nil
}

func (d *domState) parseDocument(markup string) (any, error) {
	if err := d.charge(int64(3 * len(markup))); err != nil {
		return nil, err
	}
	doc, implied, err := dom.ParseDocumentPrefixedAsElements(markup)
	if err != nil {
		return nil, domError("SyntaxError", err.Error())
	}
	unwrapImplied(doc, implied)
	d.docs[doc] = &docInfo{}
	return d.handle(doc), nil
}

// unwrapImplied replaces the html, head and body elements that the tree
// builder created without a start tag in the source by their children, so a
// parsed document holds what the markup wrote (R-JS-74; live 2026-09-29:
// PageLove's parseFromString("<ul>…</ul><p>…</p>") has the ul and the p as
// the document's children, and no head or body).
func unwrapImplied(doc *html.Node, implied dom.Implied) {
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; {
			next := c.NextSibling
			walk(c)
			if implied[c] {
				for g := c.FirstChild; g != nil; {
					gn := g.NextSibling
					c.RemoveChild(g)
					n.InsertBefore(g, c)
					g = gn
				}
				n.RemoveChild(c)
			}
			c = next
		}
	}
	walk(doc)
}

func (d *domState) serialize(n *html.Node) string {
	switch {
	case n.Type == html.DocumentNode && d.isFrag(n):
		var b strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			b.WriteString(outerHTML(c))
		}
		return b.String()
	case n.Type == html.DocumentNode:
		return renderDocument(n)
	}
	return outerHTML(n)
}

func (d *domState) insertAdjacent(n *html.Node, pos, markup string) error {
	switch asciiLower(pos) {
	case "beforebegin", "afterend":
		p := n.Parent
		if p == nil {
			return nil
		}
		if d.readOnly(p) {
			return errReadOnly
		}
		ctx := p
		if p.Type == html.DocumentNode {
			// A fragment's or a document's child (live 2026-09-29: PageLove
			// inserts beside a top-level element of a parsed document).
			ctx = contextElement("", d.xmlDoc(p))
		}
		nodes, err := d.parseInto(markup, ctx)
		if err != nil {
			return err
		}
		ref := n
		if asciiLower(pos) == "afterend" {
			ref = n.NextSibling
		}
		d.insert(p, nodes, ref)
	case "afterbegin", "beforeend":
		if d.readOnly(n) {
			return errReadOnly
		}
		nodes, err := d.parseInto(markup, n)
		if err != nil {
			return err
		}
		var ref *html.Node
		if asciiLower(pos) == "afterbegin" {
			ref = n.FirstChild
		}
		d.insert(n, nodes, ref)
	default:
		return domError("SyntaxError", fmt.Sprintf("'%s' is not a valid position", pos))
	}
	return nil
}

// insertNodes implements append, prepend, before, after and replaceWith
// (R-JS-71). All checks run before anything moves.
func (d *domState) insertNodes(n *html.Node, mode string, list []*html.Node) error {
	parent := n
	if mode == "before" || mode == "after" || mode == "replaceWith" {
		parent = n.Parent
		if parent == nil {
			return nil
		}
	}
	for _, c := range list {
		if err := d.checkInsert(parent, c); err != nil {
			return err
		}
	}
	if mode == "replaceWith" && d.readOnly(n) {
		return errReadOnly
	}
	var nodes []*html.Node
	for _, c := range list {
		p, err := d.prepare(parent, c)
		if err != nil {
			return err
		}
		nodes = append(nodes, p...)
	}
	var ref *html.Node
	switch mode {
	case "append":
	case "prepend":
		ref = parent.FirstChild
		for ref != nil && contains(nodes, ref) {
			ref = ref.NextSibling
		}
	case "before":
		prev := n.PrevSibling
		for prev != nil && contains(nodes, prev) {
			prev = prev.PrevSibling
		}
		if prev == nil {
			ref = parent.FirstChild
		} else {
			ref = prev.NextSibling
		}
		for ref != nil && contains(nodes, ref) {
			ref = ref.NextSibling
		}
	case "after", "replaceWith":
		ref = n.NextSibling
		for ref != nil && contains(nodes, ref) {
			ref = ref.NextSibling
		}
	default:
		return fmt.Errorf("bad insertion mode %q", mode)
	}
	if mode == "replaceWith" && !contains(nodes, n) {
		d.detach(n)
	}
	d.insert(parent, nodes, ref)
	return nil
}

// ------------------------------------------------------------ serialization helpers

func outerHTML(n *html.Node) string { return dom.OuterHTML(n) }

func renderDocument(n *html.Node) string { return string(dom.Render(n)) }

func isXMLNode(n *html.Node) bool { return xmldom.IsXML(n) }
