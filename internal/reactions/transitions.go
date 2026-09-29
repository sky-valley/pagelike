package reactions

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/microdata"
)

// ---------------------------------------------------------------- item identity

// itemPair is one item before and after a write; either side may be absent
// (an entry or an exit).
type itemPair struct {
	typ      string
	old, new *html.Node
}

// ambiguity is a group of items that cannot be paired (R-REACT-70).
type ambiguity struct {
	typ        string
	olds, news []*html.Node
}

type changeSet struct {
	pairs     []itemPair
	ambiguous []ambiguity
}

// changes pairs the items a write affects (R-REACT-67..69). ok is false
// for writes transitions do not examine.
func changes(w *engine.WriteCtx, types *typeInfo) (*changeSet, bool) {
	op := w.Op
	cs := &changeSet{}
	sel := op.Range.HasSelector()
	switch {
	case op.Method == http.MethodPut && !sel, op.Method == http.MethodDelete && !sel:
		if w.Before == nil && w.After == nil {
			return nil, false
		}
		cs.whole(docItems(w.Before), docItems(w.After), types)
		return cs, true
	case op.Method == "MOVE":
		// The moved element keeps its identity; pairing the whole document
		// finds every value that changed (pagelike simplification).
		if w.Before == nil || w.After == nil {
			return nil, false
		}
		cs.whole(docItems(w.Before), docItems(w.After), types)
		return cs, true
	case !sel || w.Before == nil || w.After == nil && op.Method != http.MethodDelete:
		return nil, false // resource creation and other writes: not examined here
	}
	s, err := op.Range.CheckSelector()
	if err != nil {
		return nil, false
	}
	target := s.MatchFirst(w.Before)
	if target == nil {
		return nil, false
	}
	switch op.Method {
	case http.MethodPut:
		if target.Parent == nil || target.Parent.Type != html.ElementNode {
			cs.whole(docItems(w.Before), docItems(w.After), types) // the root element: a whole document
			return cs, true
		}
		cs.ancestors(w, target.Parent)
		olds := subtreeItems(target)
		var news []*html.Node
		for _, n := range w.Inserted {
			news = append(news, subtreeItems(n)...)
		}
		// The request names the element it replaces: its root pairs with
		// the new root when both are items of one type (R-REACT-69).
		if f := firstElementOf(w.Inserted); f != nil && isItem(target) && isItem(f) && firstType(target) == firstType(f) && firstType(f) != "" {
			cs.pairs = append(cs.pairs, itemPair{typ: firstType(f), old: target, new: f})
			olds, news = without(olds, target), without(news, f)
		}
		cs.whole(olds, news, types)
	case http.MethodPost:
		scope := target
		if op.Range.Placement == "before" || op.Range.Placement == "after" {
			scope = target.Parent
		}
		if scope == nil || scope.Type != html.ElementNode {
			return nil, false
		}
		cs.ancestors(w, scope)
		for _, n := range w.Inserted {
			for _, it := range subtreeItems(n) {
				cs.pairs = append(cs.pairs, itemPair{typ: firstType(it), new: it})
			}
		}
	case http.MethodDelete:
		cs.ancestors(w, target.Parent)
		for _, it := range subtreeItems(target) {
			cs.pairs = append(cs.pairs, itemPair{typ: firstType(it), old: it})
		}
	default:
		return nil, false
	}
	return cs, true
}

// ancestors pairs the items enclosing a selector write's change with
// themselves. The change happens inside p, so p and its ancestors keep
// their positions in the new document.
func (cs *changeSet) ancestors(w *engine.WriteCtx, p *html.Node) {
	if p == nil || w.After == nil {
		return
	}
	q := nodeAtPath(w.After, pathOf(p))
	for p != nil && q != nil && p.Type == html.ElementNode && q.Type == html.ElementNode {
		if isItem(p) && isItem(q) && firstType(p) != "" {
			cs.pairs = append(cs.pairs, itemPair{typ: firstType(p), old: p, new: q})
		}
		p, q = p.Parent, q.Parent
	}
}

// whole pairs items the way whole-document writes do (R-REACT-68): by type
// (first itemtype token), then by honoured @key value; keyless items pair
// only one-to-one. Anything else is ambiguous.
func (cs *changeSet) whole(olds, news []*html.Node, types *typeInfo) {
	var order []string
	byType := map[string]*[2][]*html.Node{}
	add := func(side int, n *html.Node) {
		t := firstType(n)
		if t == "" {
			return
		}
		g := byType[t]
		if g == nil {
			g = &[2][]*html.Node{}
			byType[t] = g
			order = append(order, t)
		}
		g[side] = append(g[side], n)
	}
	for _, n := range olds {
		add(0, n)
	}
	for _, n := range news {
		add(1, n)
	}
	for _, t := range order {
		g := byType[t]
		k := types.keyOf(t)
		var keyOrder []string
		keyed := map[string]*[2][]*html.Node{}
		var loose [2][]*html.Node
		for side := 0; side < 2; side++ {
			for _, n := range g[side] {
				v := ""
				if k != "" {
					v = keyValue(n, k)
				}
				if v == "" {
					loose[side] = append(loose[side], n)
					continue
				}
				kg := keyed[v]
				if kg == nil {
					kg = &[2][]*html.Node{}
					keyed[v] = kg
					keyOrder = append(keyOrder, v)
				}
				kg[side] = append(kg[side], n)
			}
		}
		for _, v := range keyOrder {
			cs.match(t, keyed[v][0], keyed[v][1])
		}
		cs.match(t, loose[0], loose[1])
	}
}

// match pairs at most one old and one new item; more on either side is an
// ambiguity.
func (cs *changeSet) match(t string, olds, news []*html.Node) {
	switch {
	case len(olds) > 1 || len(news) > 1:
		cs.ambiguous = append(cs.ambiguous, ambiguity{typ: t, olds: olds, news: news})
	case len(olds) == 1 && len(news) == 1:
		cs.pairs = append(cs.pairs, itemPair{typ: t, old: olds[0], new: news[0]})
	case len(olds) == 1:
		cs.pairs = append(cs.pairs, itemPair{typ: t, old: olds[0]})
	case len(news) == 1:
		cs.pairs = append(cs.pairs, itemPair{typ: t, new: news[0]})
	}
}

// ---------------------------------------------------------------- validation

// violation is one refused change (R-REACT-72).
type violation struct {
	kind     string // "transition", "multi", "ambiguous"
	itemtype string
	key      string
	hasKey   bool
	property string
	from, to string
	hasTo    bool
	declared string // path of the first document declaring a watching constraint
	count    int    // multi-valued: number of values
}

// watchers returns the constraints watching a pair, by property.
func (ix *index) watchers(p itemPair) map[string][]*constraint {
	var out map[string][]*constraint
	for _, c := range ix.constraints {
		if (p.old != nil && c.sel.Matches(p.old)) || (p.new != nil && c.sel.Matches(p.new)) {
			if out == nil {
				out = map[string][]*constraint{}
			}
			out[c.property] = append(out[c.property], c)
		}
	}
	return out
}

// watchedGroup returns the properties constraints watch on an ambiguous
// group's items.
func (ix *index) watchedGroup(g ambiguity) map[string][]*constraint {
	out := map[string][]*constraint{}
	for _, c := range ix.constraints {
		hit := false
		for _, n := range g.olds {
			hit = hit || c.sel.Matches(n)
		}
		for _, n := range g.news {
			hit = hit || c.sel.Matches(n)
		}
		if hit {
			out[c.property] = append(out[c.property], c)
		}
	}
	return out
}

// validate checks every watched change against the constraints
// (R-REACT-64..70) and returns the violations.
func (ix *index) validate(cs *changeSet) []violation {
	var out []violation
	for _, p := range cs.pairs {
		byProp := ix.watchers(p)
		for _, prop := range sortedKeys(byProp) {
			ws := byProp[prop]
			vo, vn := stateOf(p.old, prop), stateOf(p.new, prop)
			key, hasKey := ix.itemKey(p)
			if p.new != nil && len(vn) > 1 {
				so, _ := single(vo)
				out = append(out, violation{kind: "multi", itemtype: p.typ, key: key, hasKey: hasKey, property: prop, from: so, declared: ws[0].path, count: len(vn)})
				continue
			}
			so, soOK := single(vo)
			sn, snOK := single(vn)
			if soOK == snOK && so == sn {
				continue // unchanged: not a transition
			}
			if permitted(ws, so, soOK, sn, snOK) {
				continue
			}
			out = append(out, violation{kind: "transition", itemtype: p.typ, key: key, hasKey: hasKey, property: prop, from: so, to: sn, hasTo: true, declared: ws[0].path})
		}
	}
	for _, g := range cs.ambiguous {
		byProp := ix.watchedGroup(g)
		for _, prop := range sortedKeys(byProp) {
			if touches(g, prop) {
				out = append(out, violation{kind: "ambiguous", itemtype: g.typ, property: prop, declared: byProp[prop][0].path})
			}
		}
	}
	return out
}

// watched reports whether any affected item is watched by a constraint.
func (ix *index) watched(cs *changeSet) bool {
	for _, p := range cs.pairs {
		if len(ix.watchers(p)) > 0 {
			return true
		}
	}
	for _, g := range cs.ambiguous {
		if len(ix.watchedGroup(g)) > 0 {
			return true
		}
	}
	return false
}

// permitted applies the rule kinds of R-REACT-63/65.
func permitted(ws []*constraint, so string, soOK bool, sn string, snOK bool) bool {
	for _, c := range ws {
		switch {
		case !soOK && snOK: // entry
			if !c.hasFrom && c.hasTo && containsStr(c.to, sn) {
				return true
			}
		case soOK && !snOK: // exit
			if c.hasFrom && !c.hasTo && containsStr(c.from, so) {
				return true
			}
		default: // step
			if c.hasFrom && c.hasTo && containsStr(c.from, so) && containsStr(c.to, sn) {
				return true
			}
		}
	}
	return false
}

// touches reports whether a write changes an ambiguous group's watched
// property: a different number of items, or a different multiset of their
// states (pagelike definition, R-REACT-70).
func touches(g ambiguity, prop string) bool {
	if len(g.olds) != len(g.news) {
		return true
	}
	states := func(ns []*html.Node) []string {
		var out []string
		for _, n := range ns {
			s, ok := single(stateOf(n, prop))
			if !ok {
				s = "\x00absent"
			}
			out = append(out, s)
		}
		sort.Strings(out)
		return out
	}
	a, b := states(g.olds), states(g.news)
	for i := range a {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}

// itemKey is the key of a pair's item (the old item's for exits).
func (ix *index) itemKey(p itemPair) (string, bool) {
	k := ix.types.keyOf(p.typ)
	if k == "" {
		return "", false
	}
	for _, n := range []*html.Node{p.old, p.new} {
		if n != nil {
			if v := keyValue(n, k); v != "" {
				return v, true
			}
		}
	}
	return "", false
}

// validate is the engine Validate hook: the platform-schema check of
// reaction items written through the serving path (platform.go), the race
// check (412) and transition validation (422). WebDAV writes are never
// transition-validated (R-REACT-75).
func (x *Reactions) validate(ctx context.Context, w *engine.WriteCtx) error {
	if w.Op.Plane == engine.Authoring || w.Snap == nil {
		return nil
	}
	ix := indexFor(w.Snap) // constraints as committed when validation runs (R-REACT-71)
	if err := checkPlatformItems(w, ix.types); err != nil {
		return err
	}
	if len(ix.constraints) == 0 {
		return nil
	}
	cs, ok := changes(w, ix.types)
	if !ok {
		return nil
	}
	if st := stateFrom(ctx); st.isMain(w) {
		st.mu.Lock()
		v0, tracked := st.arrival[w.Op.Path]
		st.mu.Unlock()
		cur := int64(0)
		if w.Doc != nil {
			cur = w.Doc.Version
		}
		if tracked && cur != v0 && ix.watched(cs) {
			// Another write changed the document after this request
			// arrived and this write touches watched items (R-REACT-73).
			e := errdoc.New(http.StatusPreconditionFailed, "PreconditionFailed", "the document changed while this request was in flight and the write touches items under transition constraints; re-read and retry")
			if w.Doc != nil {
				e.WithHeader("ETag", w.Doc.ETag)
			}
			return e
		}
	}
	if vs := ix.validate(cs); len(vs) > 0 {
		return &errdoc.Error{Status: http.StatusUnprocessableEntity, Kind: "ConstraintViolation",
			Message: "Transition constraints violated", Document: violationDocument(vs)}
	}
	return nil
}

// violationDocument renders the 422 ConstraintViolation body (R-REACT-72).
func violationDocument(vs []violation) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html>\n  <head>\n    <title>422 Unprocessable Entity - Transition Constraint Violation</title>\n  </head>\n")
	b.WriteString(`  <body itemscope itemtype="` + TypeConstraintViolation + `">` + "\n")
	b.WriteString("    <h1 itemprop=\"name\">Unprocessable Entity</h1>\n    <meta itemprop=\"statusCode\" content=\"422\">\n    <p itemprop=\"description\">Transition constraints violated</p>\n    <ul>\n")
	for _, v := range vs {
		item := ""
		if v.hasKey {
			item = fmt.Sprintf(" item '%s'", v.key)
		}
		var msg string
		switch v.kind {
		case "multi":
			msg = fmt.Sprintf("Transition violation: '%s'%s property '%s' must be single-valued (found %d values)", v.itemtype, item, v.property, v.count)
		case "ambiguous":
			msg = fmt.Sprintf("Transition violation: type '%s' needs a @key property to pair items between writes (property '%s')", v.itemtype, v.property)
		default:
			msg = fmt.Sprintf("Transition violation: '%s'%s property '%s' may not change from '%s' to '%s' (constraint declared in '%s')", v.itemtype, item, v.property, v.from, v.to, v.declared)
		}
		span := func(prop, val string) {
			b.WriteString(`        <span itemprop="` + prop + `">` + textEsc(val) + "</span>\n")
		}
		b.WriteString(`      <li itemprop="violations" itemscope itemtype="` + TypeViolation + `">` + "\n")
		span("constraintSelector", "[itemprop='"+v.property+"']")
		span("failedConstraint", "transition("+v.property+")")
		span("message", msg)
		span("itemtype", v.itemtype)
		span("property", v.property)
		if v.hasKey {
			span("key", v.key)
		}
		if v.kind != "ambiguous" {
			span("from", v.from)
			if v.kind == "transition" {
				span("to", v.to)
			}
		}
		b.WriteString("      </li>\n")
	}
	b.WriteString("    </ul>\n  </body>\n</html>\n")
	return b.String()
}

// ---------------------------------------------------------------- helpers

// docItems returns a document's items outside <template> contents.
func docItems(root *html.Node) []*html.Node {
	if root == nil {
		return nil
	}
	return subtreeItems(root)
}

func subtreeItems(n *html.Node) []*html.Node {
	var out []*html.Node
	walkItems(n, func(it *html.Node) { out = append(out, it) })
	return out
}

func isItem(n *html.Node) bool {
	return n != nil && n.Type == html.ElementNode && dom.HasAttr(n, "itemscope")
}

func firstType(n *html.Node) string {
	if f := strings.Fields(dom.AttrOr(n, "itemtype", "")); len(f) > 0 {
		return f[0]
	}
	return ""
}

func firstElementOf(nodes []*html.Node) *html.Node {
	for _, n := range nodes {
		if n.Type == html.ElementNode {
			return n
		}
	}
	return nil
}

func without(ns []*html.Node, x *html.Node) []*html.Node {
	out := ns[:0:0]
	for _, n := range ns {
		if n != x {
			out = append(out, n)
		}
	}
	return out
}

// stateOf lists an item's microdata values for a property (nil item: none).
func stateOf(n *html.Node, prop string) []string {
	if n == nil {
		return nil
	}
	var out []string
	for _, p := range microdata.Parse(n).Props {
		if p.Name != prop {
			continue
		}
		if p.Item != nil {
			out = append(out, dom.TextContent(p.Node))
		} else {
			out = append(out, p.Value)
		}
	}
	return out
}

// single is the state of a single-valued property; several values or none
// are absent (R-REACT-65).
func single(vs []string) (string, bool) {
	if len(vs) == 1 {
		return vs[0], true
	}
	return "", false
}

func keyValue(n *html.Node, k string) string {
	vs := stateOf(n, k)
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}

// pathOf is n's child-index path from the root.
func pathOf(n *html.Node) []int {
	var idx []int
	for x := n; x.Parent != nil; x = x.Parent {
		i := 0
		for c := x.Parent.FirstChild; c != x; c = c.NextSibling {
			i++
		}
		idx = append(idx, i)
	}
	for i, j := 0, len(idx)-1; i < j; i, j = i+1, j-1 {
		idx[i], idx[j] = idx[j], idx[i]
	}
	return idx
}

func nodeAtPath(root *html.Node, idx []int) *html.Node {
	cur := root
	for _, i := range idx {
		c := cur.FirstChild
		for ; c != nil && i > 0; i-- {
			c = c.NextSibling
		}
		if c == nil {
			return nil
		}
		cur = c
	}
	return cur
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
