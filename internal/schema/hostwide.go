package schema

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/site"
)

// hostView is the host as a write sees it for host-wide checks (R-MOD-30,
// R-MOD-38): the committed snapshot, with some documents superseded by
// proposed trees (the document being written, cascade targets) and some
// removed.
type hostView struct {
	snap *site.Snapshot
	reg  *Registry
	over map[string]*html.Node // path → proposed document (nil: deleted)
}

type instRef struct {
	path string
	node *html.Node
}

func newHostView(snap *site.Snapshot, reg *Registry) *hostView {
	return &hostView{snap: snap, reg: reg, over: map[string]*html.Node{}}
}

// ofType returns the instances whose itemtype is exactly t.
func (v *hostView) ofType(t string) []instRef {
	var out []instRef
	for _, ti := range v.snap.ItemsOfType(t) {
		if _, superseded := v.over[ti.Path]; superseded {
			continue
		}
		n := ti.Item.Node
		if itemType(n) != t || inTemplate(n) {
			continue
		}
		out = append(out, instRef{ti.Path, n})
	}
	paths := make([]string, 0, len(v.over))
	for p := range v.over {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		root := v.over[p]
		if root == nil {
			continue
		}
		for _, n := range instancesUnder(root) {
			if itemType(n) == t {
				out = append(out, instRef{p, n})
			}
		}
	}
	return out
}

// isaTypes returns t and every registered type that descends from it.
func (r *Registry) isaTypes(t string) []string {
	out := []string{t}
	for _, s := range r.order {
		if s.URL != t && r.IsA(s.URL, t) {
			out = append(out, s.URL)
		}
	}
	return out
}

// valuesOf returns the set of non-null values property prop holds on
// instances isa t in the view.
func (v *hostView) valuesOf(t, prop string) map[string]bool {
	set := map[string]bool{}
	for _, tt := range v.reg.isaTypes(t) {
		for _, in := range v.ofType(tt) {
			for _, s := range valueStrings(propElements(in.node, prop)) {
				set[s] = true
			}
		}
	}
	return set
}

// hostWide runs stage 8: reference integrity, then individual and
// composite uniqueness, each a 422 ConstraintViolation problems item (live
// 2026-09-29).
func (p *pass) hostWide() error {
	view := newHostView(p.w.Snap, p.reg)
	view.over[p.op.Path] = p.after
	if vs := p.references(view); len(vs) > 0 {
		return constraintError(vs)
	}
	if uv := p.uniqueness(view); len(uv) > 0 {
		// PageLove reports the first uniqueness violation only (live
		// 2026-09-29: two duplicated unique properties, "1 violation(s)").
		return constraintError(uv[:1])
	}
	return nil
}

func (p *pass) references(view *hostView) []UniqueViolation {
	var vs []UniqueViolation
	targets := map[string]map[string]bool{}
	p.eachProp(func(inst *html.Node, s *Schema, ep *EffectiveProp) error {
		if ep.RefType == "" {
			return nil
		}
		key := ep.RefType + "#" + ep.RefProp
		set, ok := targets[key]
		if !ok {
			set = view.valuesOf(ep.RefType, ep.RefProp)
			targets[key] = set
		}
		for _, v := range valueStrings(propElements(inst, ep.Name)) {
			if !set[v] {
				vs = append(vs, UniqueViolation{Kind: LineReference, Selector: fmt.Sprintf("[itemprop='%s']", ep.Name),
					Constraint: "references(" + ep.Name + ")", ItemType: s.URL, Property: ep.Name, Value: v,
					Message: fmt.Sprintf("Referenced value does not exist for property '%s'", ep.Name)})
			}
		}
		return nil
	})
	return vs
}

// uniqueIndex maps a type and property (or composite group) to the
// instances holding each value (or value tuple).
type uniqueIndex map[string]map[string][]instRef

func (ix uniqueIndex) holders(view *hostView, t, key string, values func(*html.Node) []string) map[string][]instRef {
	k := t + "\x00" + key
	if m, ok := ix[k]; ok {
		return m
	}
	m := map[string][]instRef{}
	for _, in := range view.ofType(t) {
		for _, v := range dedupStrings(values(in.node)) {
			m[v] = append(m[v], in)
		}
	}
	ix[k] = m
	return m
}

func dedupStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// tuples returns every combination of the members' values on inst, joined
// by NUL; nil when a member has no value (R-MOD-31: no participation).
func tuples(inst *html.Node, members []string) []string {
	combos := []string{""}
	for i, m := range members {
		vals := dedupStrings(valueStrings(propElements(inst, m)))
		if len(vals) == 0 {
			return nil
		}
		var next []string
		for _, c := range combos {
			for _, v := range vals {
				if i == 0 {
					next = append(next, v)
				} else {
					next = append(next, c+"\x00"+v)
				}
			}
		}
		combos = next
	}
	return combos
}

// uniqueness checks individual and composite uniqueness of the affected
// instances. Messages follow PageLove (live 2026-09-29), except that a
// value held in another document does not name that document: PageLove
// appends " at '<path>'", which would disclose a document the writer may
// not be allowed to read (R-MOD-72, keep-documented-security).
func (p *pass) uniqueness(view *hostView) []UniqueViolation {
	ix := uniqueIndex{}
	var out []UniqueViolation
	seen := map[string]bool{}
	add := func(v UniqueViolation) {
		k := v.ItemType + "\x00" + v.Constraint + "\x00" + v.Value
		if !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	for _, inst := range p.instances {
		s := p.schemaOf(inst)
		t := s.URL
		groups := map[string][]string{}
		var groupOrder []string
		for _, pn := range s.eff.propOrder {
			ep := s.eff.props[pn]
			for _, g := range ep.UniqueGroups {
				if _, ok := groups[g]; !ok {
					groupOrder = append(groupOrder, g)
				}
				groups[g] = append(groups[g], pn)
			}
			if !ep.Unique {
				continue
			}
			name := pn
			h := ix.holders(view, t, "p:"+name, func(n *html.Node) []string { return valueStrings(propElements(n, name)) })
			for _, v := range dedupStrings(valueStrings(propElements(inst, pn))) {
				elsewhere, first := p.conflict(h[v], inst)
				var msg string
				switch {
				case elsewhere:
					msg = fmt.Sprintf("Uniqueness violation: value already exists for property '%s'", pn)
				case first != nil:
					msg = fmt.Sprintf("Uniqueness violation: another item in '%s' already claims this value for property '%s' (first claimed by item '%s')",
						p.op.Path, pn, itemIndexPath(first))
				default:
					continue
				}
				add(UniqueViolation{Kind: LineUniqueness, Selector: fmt.Sprintf("[itemprop='%s']", pn), Constraint: "unique(" + pn + ")",
					ItemType: t, Property: pn, Value: v, Message: msg})
			}
		}
		for _, g := range groupOrder {
			members := groups[g]
			h := ix.holders(view, t, "g:"+g, func(n *html.Node) []string { return tuples(n, members) })
			// PageLove names the members in alphabetical order.
			sorted := append([]string(nil), members...)
			sort.Strings(sorted)
			sels := make([]string, len(sorted))
			for i, m := range sorted {
				sels[i] = fmt.Sprintf("[itemprop='%s']", m)
			}
			for _, tu := range tuples(inst, members) {
				elsewhere, first := p.conflict(h[tu], inst)
				var msg string
				switch {
				case elsewhere:
					msg = fmt.Sprintf("Composite uniqueness violation: value combination already exists for properties '%s'", strings.Join(sorted, ", "))
				case first != nil:
					msg = fmt.Sprintf("Composite uniqueness violation: another item in '%s' already claims this value combination for properties '%s' (first claimed by item '%s')",
						p.op.Path, strings.Join(sorted, ", "), itemIndexPath(first))
				default:
					continue
				}
				add(UniqueViolation{Kind: LineUniqueness, Selector: strings.Join(sels, ", "), Constraint: "unique-group(" + g + ")",
					ItemType: t, Property: strings.Join(members, ", "), Value: strings.Join(strings.Split(tu, "\x00"), ", "), Message: msg})
			}
		}
	}
	return out
}

// conflict reports whether an instance other than inst holds the value:
// elsewhere when one is in another document, else first is the first
// holder of the written document in document order (nil: no conflict).
func (p *pass) conflict(holders []instRef, inst *html.Node) (elsewhere bool, first *html.Node) {
	other := false
	for _, h := range holders {
		if h.node == inst {
			continue
		}
		other = true
		if h.path != p.op.Path {
			return true, nil
		}
	}
	if !other {
		return false, nil
	}
	for _, h := range holders { // document order within the written document
		if h.path == p.op.Path {
			return false, h.node
		}
	}
	return false, nil
}

// itemIndexPath names an item as PageLove's uniqueness messages do: "0"
// for the document, then the index of each ancestor-or-self among its
// parent's child nodes (doctype, text and comments included). With
// PageLove's document model (LO-15) the numbers are PageLove's: live
// 2026-09-29 answered '0.2.0.1' for the second child of <body> in
// "<!DOCTYPE html>\n<html><body>\n  <div …>".
func itemIndexPath(n *html.Node) string {
	var idx []string
	for x := n; x != nil && x.Parent != nil; x = x.Parent {
		i := 0
		for c := x.Parent.FirstChild; c != nil && c != x; c = c.NextSibling {
			i++
		}
		idx = append(idx, strconv.Itoa(i))
	}
	idx = append(idx, "0")
	for i, j := 0, len(idx)-1; i < j; i, j = i+1, j-1 {
		idx[i], idx[j] = idx[j], idx[i]
	}
	return strings.Join(idx, ".")
}
