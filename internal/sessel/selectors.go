package sessel

import (
	"fmt"
	"path"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/selector"
)

// Extended pseudo-classes whose unquoted argument is a Sessel expression
// (R-SESSEL-202).
var extPseudo = map[string]bool{
	"contains": true, "equals": true, "value-contains": true, "value-equals": true,
	"greater-than": true, "less-than": true, "value-greater-than": true, "value-less-than": true,
}

const (
	nsSVG    = "http://www.w3.org/2000/svg"
	nsMathML = "http://www.w3.org/1998/Math/MathML"
)

// selTemplate builds the selector template for a selector token: literal
// CSS with holes for unquoted attribute values and extended pseudo-class
// arguments.
func (p *parser) selTemplate(t token) *selTemplate {
	src, off := t.text, t.soff
	tpl := &selTemplate{src: src}
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			tpl.parts = append(tpl.parts, selPart{lit: lit.String()})
			lit.Reset()
		}
	}
	addHole := func(h *selHole) {
		flush()
		tpl.parts = append(tpl.parts, selPart{hole: h})
		tpl.holes++
	}
	n := len(src)
	prevName := func(i int) bool {
		if i == 0 {
			return false
		}
		c := src[i-1]
		return isIdentChar(c) || c == '-' || c == '.' || c == '#' || c == ':' || c == '\\' || c == '|' || c >= 0x80
	}
	for i := 0; i < n; {
		c := src[i]
		switch {
		case c == '"' || c == '\'':
			j := skipQuoted(src, i)
			lit.WriteString(src[i:j])
			i = j
		case c == '\\' && i+1 < n:
			lit.WriteString(src[i : i+2])
			i += 2
		case c == '[':
			i = p.scanAttrSel(tpl, src, off, i, &lit, addHole)
		case c == ':':
			j := i + 1
			for j < n && (isIdentChar(src[j]) || src[j] == '-') {
				j++
			}
			name := strings.ToLower(src[i+1 : j])
			if name == "isa" {
				tpl.isa = true
			}
			if extPseudo[name] && j < n && src[j] == '(' {
				argStart := j + 1
				end := scanBalanced(src, argStart, n, ')')
				if end < 0 {
					lit.WriteString(src[i:])
					i = n
					break
				}
				lit.WriteString(src[i:argStart])
				arg := src[argStart:end]
				body, flag := splitCaseFlag(arg)
				bt := strings.TrimSpace(body)
				if bt == "" || bt[0] == '"' || bt[0] == '\'' {
					lit.WriteString(arg)
				} else {
					addHole(p.makeHole(off+argStart, off+argStart+len(body), false))
					lit.WriteString(flag)
				}
				lit.WriteByte(')')
				i = end + 1
				break
			}
			lit.WriteString(src[i:j])
			i = j
		case (isIdentStart(c) || c == '*') && !prevName(i):
			j := i + 1
			for j < n && (isIdentChar(src[j]) || src[j] == '-') {
				j++
			}
			if j+1 < n && src[j] == '|' && src[j+1] != '=' && src[j+1] != '|' {
				prefix := src[i:j]
				k := j + 1
				for k < n && (isIdentChar(src[k]) || src[k] == '-' || src[k] == '*') {
					k++
				}
				local := src[j+1 : k]
				lit.WriteString(p.nsName(tpl, prefix, local, false))
				i = k
				break
			}
			lit.WriteString(src[i:j])
			i = j
		default:
			lit.WriteByte(c)
			i++
		}
	}
	flush()
	for _, f := range []string{"count(", "text-of(", "value-of(", "attr-of("} {
		if strings.Contains(src, f) {
			tpl.funcs = true
		}
	}
	return tpl
}

// nsName rewrites prefix|name (R-SESSEL-203): SVG/MathML elements are
// matched by local name, other namespaces by their serialized prefix:name.
func (p *parser) nsName(tpl *selTemplate, prefix, local string, attr bool) string {
	if prefix == "*" || prefix == "" {
		return local
	}
	uri, ok := p.ns[prefix]
	if !ok {
		tpl.nsErr = fmt.Sprintf("undeclared namespace prefix %q in selector", prefix)
		return local
	}
	if !attr && (uri == nsSVG || uri == nsMathML) {
		return local
	}
	return prefix + `\:` + local
}

// scanAttrSel handles '[' at src[i] and returns the index to continue at.
func (p *parser) scanAttrSel(tpl *selTemplate, src string, off, i int, lit *strings.Builder, addHole func(*selHole)) int {
	n := len(src)
	j := i + 1
	for j < n && isSpace(src[j]) {
		j++
	}
	ns := j
	for j < n && (isIdentChar(src[j]) || src[j] == '-' || src[j] == '\\' || src[j] >= 0x80) {
		if src[j] == '\\' {
			j++
		}
		j++
	}
	head := src[i:j]
	if j+1 < n && src[j] == '|' && src[j+1] != '=' {
		prefix := src[ns:j]
		k := j + 1
		for k < n && (isIdentChar(src[k]) || src[k] == '-') {
			k++
		}
		head = src[i:ns] + p.nsName(tpl, prefix, src[j+1:k], true)
		j = k
	}
	afterName := j
	for j < n && isSpace(src[j]) {
		j++
	}
	if j >= n {
		lit.WriteString(head + src[afterName:])
		return n
	}
	opEnd := -1
	switch src[j] {
	case ']':
		lit.WriteString(head + src[afterName:j+1])
		return j + 1
	case '=':
		opEnd = j + 1
	case '~', '|', '^', '$', '*':
		if j+1 < n && src[j+1] == '=' {
			opEnd = j + 2
		}
	}
	if opEnd < 0 {
		lit.WriteString(head)
		return afterName
	}
	k := opEnd
	for k < n && isSpace(src[k]) {
		k++
	}
	lit.WriteString(head)
	lit.WriteString(src[afterName:k])
	if k < n && (src[k] == '"' || src[k] == '\'') {
		return k
	}
	end := scanBalanced(src, k, n, ']')
	if end < 0 {
		lit.WriteString(src[k:])
		return n
	}
	if strings.TrimSpace(src[k:end]) == "" {
		lit.WriteString(src[k : end+1])
		return end + 1
	}
	h := p.makeHole(off+k, off+end, true)
	addHole(h)
	lit.WriteString("]")
	return end + 1
}

func skipQuoted(s string, i int) int {
	q := s[i]
	j := i + 1
	for j < len(s) && s[j] != q {
		if s[j] == '\\' {
			j++
		}
		j++
	}
	if j >= len(s) {
		return len(s)
	}
	return j + 1
}

// splitCaseFlag splits a trailing top-level ", i" case flag off a
// pseudo-class argument.
func splitCaseFlag(arg string) (body, flag string) {
	depth := 0
	last := -1
	for i := 0; i < len(arg); i++ {
		switch c := arg[i]; c {
		case '"', '\'':
			i = skipQuoted(arg, i) - 1
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				last = i
			}
		}
	}
	if last >= 0 {
		if f := strings.TrimSpace(arg[last+1:]); f == "i" || f == "I" {
			return arg[:last], arg[last:]
		}
	}
	return arg, ""
}

// cssString quotes s as a CSS string.
func cssString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\a `)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\%x `, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// renderSelector evaluates a template's holes and compiles the result.
func (ev *evaluator) renderSelector(tpl *selTemplate, sc *scope) (*selector.Selector, error) {
	if tpl.nsErr != "" {
		return nil, typeErr("%s", tpl.nsErr)
	}
	if tpl.holes == 0 && !tpl.isa && !tpl.funcs {
		tpl.once.Do(func() {
			var b strings.Builder
			for _, part := range tpl.parts {
				b.WriteString(part.lit)
			}
			tpl.compiled, tpl.cerr = compileCSSCached(b.String())
		})
		return tpl.compiled, tpl.cerr
	}
	var b strings.Builder
	for _, part := range tpl.parts {
		if part.hole == nil {
			b.WriteString(part.lit)
			continue
		}
		h := part.hole
		switch {
		case h.number || h.expr == nil || (h.ident != "" && !ev.resolvable(h.ident, sc)):
			b.WriteString(h.raw)
		default:
			v, err := ev.eval(h.expr, sc)
			if err != nil {
				return nil, err
			}
			b.WriteString(cssString(TextOf(v)))
		}
		b.WriteString(h.flag)
	}
	css := b.String()
	if tpl.funcs {
		exp, err := selector.ExpandFunctions(css, &funcEval{ev: ev})
		if err != nil {
			if e, ok := err.(*Error); ok {
				return nil, e
			}
			return nil, typeErr("invalid selector %q: %v", css, err)
		}
		css = exp
	}
	return ev.compileCSS(css, tpl.isa)
}

// compileCSS compiles a CSS selector list (relative selectors are anchored
// at :scope); :isa() selectors are compiled with this run's inheritance.
func (ev *evaluator) compileCSS(css string, isa bool) (*selector.Selector, error) {
	if isa || strings.Contains(css, ":isa(") {
		s, err := selector.CompileWith(scopeRelative(css), selector.ExtOptions{IsA: ev.isA})
		if err != nil {
			return nil, typeErr("invalid selector %q: %v", css, err)
		}
		return s, nil
	}
	return compileCSSCached(css)
}

type cssEntry struct {
	sel *selector.Selector
	err error
}

var (
	cssCache sync.Map
	cssCount atomic.Int64
)

func compileCSSCached(css string) (*selector.Selector, error) {
	if e, ok := cssCache.Load(css); ok {
		ce := e.(*cssEntry)
		return ce.sel, ce.err
	}
	s, err := selector.Compile(scopeRelative(css))
	ce := &cssEntry{sel: s}
	if err != nil {
		ce.err = typeErr("invalid selector %q: %v", css, err)
	}
	if cssCount.Add(1) > 16384 {
		cssCache.Range(func(k, _ any) bool { cssCache.Delete(k); return true })
		cssCount.Store(0)
	}
	cssCache.Store(css, ce)
	return ce.sel, ce.err
}

// scopeRelative prefixes selectors that start with a combinator with
// :scope, so `> li` in a sub-select is relative to the receiver.
func scopeRelative(css string) string {
	parts := splitTopLevel(css, ',')
	changed := false
	for i, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" && (t[0] == '>' || t[0] == '+' || t[0] == '~') {
			parts[i] = ":scope " + t
			changed = true
		}
	}
	if !changed {
		return css
	}
	return strings.Join(parts, ", ")
}

func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'':
			i = skipQuoted(s, i) - 1
		case '\\':
			i++
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		default:
			if c == sep && depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// rootSet is a from-scope: the roots a selector searches.
type rootSet struct{ roots []selRoot }

type selRoot struct {
	node    *html.Node // a document node (whole document) or an element (descendants)
	doc     *Document
	elem    bool
	mutable bool
}

func (rs *rootSet) addElement(el *Element) {
	if el.isDocRoot() {
		rs.roots = append(rs.roots, selRoot{node: el.Doc.Root, doc: el.Doc, mutable: el.Mutable})
		return
	}
	rs.roots = append(rs.roots, selRoot{node: el.Node, doc: el.Doc, elem: true, mutable: el.Mutable})
}

func (ev *evaluator) siteRoots() (*rootSet, error) {
	rs := &rootSet{}
	if ev.env.DocumentOnly {
		ev.addSelfRoot(rs)
		return rs, nil
	}
	if ev.r.host == nil {
		return rs, nil
	}
	docs, err := ev.r.host.Documents(ev.r.ctx, "")
	if err != nil {
		return nil, runtimeErr("unreadable stored document: %v", err)
	}
	for _, d := range docs {
		rs.roots = append(rs.roots, selRoot{node: d.Root, doc: d})
	}
	return rs, nil
}

// evalSources evaluates from-sources into roots (R-SESSEL-205).
func (ev *evaluator) evalSources(srcs []node, sc *scope) (*rootSet, error) {
	rs := &rootSet{}
	for _, s := range srcs {
		if ev.env.DocumentOnly {
			// Only `from self` reaches the document; any other source is
			// evaluated (for its errors) and matches nothing.
			if id, ok := s.(*identNode); ok && id.name == "self" {
				if _, shadowed := sc.lookup("self"); !shadowed {
					if !ev.env.HasSelf {
						return nil, &Error{Type: RuntimeErrorType, Message: "self is unbound in this context", Reason: ReasonSelfUnbound}
					}
					ev.addSelfRoot(rs)
					continue
				}
			}
			if _, ok := s.(*priorNode); !ok {
				if _, err := ev.eval(s, sc); err != nil {
					return nil, err
				}
			}
			continue
		}
		var v Value
		if _, ok := s.(*priorNode); ok {
			if ev.env.Prior != nil {
				v = ev.env.Prior
			}
		} else {
			var err error
			if v, err = ev.eval(s, sc); err != nil {
				return nil, err
			}
		}
		if err := ev.addRoots(rs, v); err != nil {
			return nil, err
		}
	}
	return rs, nil
}

// addSelfRoot adds the whole Self document (DocumentOnly programs).
func (ev *evaluator) addSelfRoot(rs *rootSet) {
	if el, ok := ev.env.Self.(*Element); ok && ev.env.HasSelf {
		rs.addElement(el)
	}
}

func (ev *evaluator) addRoots(rs *rootSet, v Value) error {
	if ev.env.DocumentOnly {
		switch v.(type) {
		case nil, string, *Element, List:
			return nil // DocumentOnly: only `from self` reaches the document
		}
	}
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		p := ev.resolvePath(x)
		if ev.r.host == nil || p == "" {
			return nil
		}
		docs, err := ev.r.host.Documents(ev.r.ctx, p)
		if err != nil {
			return runtimeErr("unreadable stored document: %v", err)
		}
		for _, d := range docs {
			rs.roots = append(rs.roots, selRoot{node: d.Root, doc: d})
		}
		return nil
	case *Element:
		rs.addElement(x)
		return nil
	case List:
		for _, it := range x {
			if err := ev.addRoots(rs, it); err != nil {
				return err
			}
		}
		return nil
	}
	return typeErr("cannot use %s as a from source", TypeName(v))
}

// resolvePath resolves a relative path against the current document's
// directory.
func (ev *evaluator) resolvePath(p string) string {
	if p == "" || strings.HasPrefix(p, "/") {
		return p
	}
	dir := "/"
	if el, ok := ev.env.Self.(*Element); ok && ev.env.HasSelf && el.Doc != nil {
		dir = path.Dir(el.Doc.Path)
	}
	joined := path.Join(dir, p)
	if strings.HasSuffix(p, "/") && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	return joined
}

// evalSelector evaluates a selector literal (R-SESSEL-200..207).
func (ev *evaluator) evalSelector(x *selectorNode, sc *scope) (Value, error) {
	sel, err := ev.renderSelector(x.sel, sc)
	if err != nil {
		return nil, err
	}
	var roots *rootSet
	switch {
	case x.hasFrom:
		roots, err = ev.evalSources(x.sources, sc)
	default:
		if roots = sc.fromRoots(); roots == nil {
			roots, err = ev.siteRoots()
		}
	}
	if err != nil {
		return nil, err
	}
	return ev.matchRoots(sel, roots)
}

// matchRoots runs sel over roots, concatenating matches in root order and
// dropping elements already reached through an earlier root.
func (ev *evaluator) matchRoots(sel *selector.Selector, rs *rootSet) (List, error) {
	out := List{}
	var seen map[*html.Node]bool
	if len(rs.roots) > 1 {
		seen = map[*html.Node]bool{}
	}
	for _, r := range rs.roots {
		g := sel.Group()
		visited := 0
		var matches []*html.Node
		if r.elem {
			g = selector.BindScope(g, r.node)
			for c := r.node.FirstChild; c != nil; c = c.NextSibling {
				matches = collectMatches(c, g, matches, &visited)
			}
		} else {
			matches = collectMatches(r.node, g, matches, &visited)
		}
		if err := ev.r.ops(visited); err != nil {
			return nil, err
		}
		for _, m := range matches {
			if seen != nil {
				if seen[m] {
					continue
				}
				seen[m] = true
			}
			out = append(out, &Element{Node: m, Doc: r.doc, Mutable: r.mutable})
		}
		if len(out) > MaxSelectorResult {
			return nil, runtimeErr("selector result exceeds %d elements", MaxSelectorResult)
		}
	}
	return out, ev.r.alloc(24 * len(out))
}

// collectMatches walks n and its descendants in tree order without
// entering <template> contents.
func collectMatches(n *html.Node, g selector.SelectorGroup, out []*html.Node, visited *int) []*html.Node {
	if n.Type == html.ElementNode {
		*visited++
		if g.Match(n) {
			out = append(out, n)
		}
		if selector.IsTemplate(n) {
			return out
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		out = collectMatches(c, g, out, visited)
	}
	return out
}

// subSelect implements recv.${ css } (R-SESSEL-161, 211).
func (ev *evaluator) subSelect(v Value, tpl *selTemplate, sc *scope) (Value, error) {
	sel, err := ev.renderSelector(tpl, sc)
	if err != nil {
		return nil, err
	}
	// Results are stored (immutable) elements even under a constructed
	// element (live 2026-09-29): mutate a constructed tree through
	// children() instead.
	rs := &rootSet{}
	switch x := v.(type) {
	case *Element:
		rs.roots = append(rs.roots, selRoot{node: x.Node, doc: x.Doc, elem: true})
	case List:
		for _, it := range x {
			if el, ok := it.(*Element); ok {
				rs.roots = append(rs.roots, selRoot{node: el.Node, doc: el.Doc, elem: true})
			}
		}
	default:
		return nil, typeErr("cannot sub-select on %s", TypeName(v))
	}
	return ev.matchRoots(sel, rs)
}

// funcEval evaluates selector functions (count(), text-of(), …) site-wide.
type funcEval struct{ ev *evaluator }

func (f *funcEval) all(sel string) (List, error) {
	s, err := f.ev.compileCSS(sel, false)
	if err != nil {
		return nil, err
	}
	roots, err := f.ev.siteRoots()
	if err != nil {
		return nil, err
	}
	return f.ev.matchRoots(s, roots)
}

func (f *funcEval) one(sel string) (*Element, error) {
	l, err := f.all(sel)
	if err != nil {
		return nil, err
	}
	switch len(l) {
	case 0:
		return nil, nil
	case 1:
		return l[0].(*Element), nil
	}
	return nil, typeErr("selector function argument %q matches %d elements", sel, len(l))
}

func (f *funcEval) Count(sel string) (int, error) {
	l, err := f.all(sel)
	return len(l), err
}

func (f *funcEval) TextOf(sel string) (string, error) {
	e, err := f.one(sel)
	if e == nil || err != nil {
		return "", err
	}
	return selector.TextContent(e.Node), nil
}

func (f *funcEval) ValueOf(sel string) (string, error) {
	e, err := f.one(sel)
	if e == nil || err != nil {
		return "", err
	}
	return selector.MicrodataValue(e.Node), nil
}

func (f *funcEval) AttrOf(attr, sel string) (string, error) {
	e, err := f.one(sel)
	if e == nil || err != nil {
		return "", err
	}
	return attrOr(e.Node, attr, ""), nil
}
