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

// ofType returns the instances that carry type t, alone or among others.
func (v *hostView) ofType(t string) []instRef {
	var out []instRef
	for _, ti := range v.snap.ItemsOfType(t) {
		if _, superseded := v.over[ti.Path]; superseded {
			continue
		}
		n := ti.Item.Node
		if !hasType(n, t) || inTemplate(n) {
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
			if hasType(n, t) {
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

// anyIsA reports whether one of the element's types is t or descends
// from it.
func (r *Registry) anyIsA(n *html.Node, t string) bool {
	for _, x := range itemTypes(n) {
		if r.IsA(x, t) {
			return true
		}
	}
	return false
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

// uniqueIndex maps a scope (the declaring type and a property or group)
// to the instances holding each value.
type uniqueIndex map[string]map[string][]instRef

// holders indexes the values every instance of owner's type, or of a type
// beneath it, holds (docs snapshot 2026-11-23: uniqueness covers the
// declaring type and every type beneath it).
func (ix uniqueIndex) holders(view *hostView, owner, key string, values func(t string, n *html.Node) []string) map[string][]instRef {
	k := owner + "\x00" + key
	if m, ok := ix[k]; ok {
		return m
	}
	m := map[string][]instRef{}
	counted := map[*html.Node]bool{} // an item with several types counts once
	for _, t := range view.reg.isaTypes(owner) {
		for _, in := range view.ofType(t) {
			if counted[in.node] {
				continue
			}
			counted[in.node] = true
			for _, v := range dedupStrings(values(t, in.node)) {
				m[v] = append(m[v], in)
			}
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

// repeated returns the values that occur more than once in vals.
func repeated(vals []string) []string {
	n := map[string]int{}
	var out []string
	for _, v := range vals {
		if n[v]++; n[v] == 2 {
			out = append(out, v)
		}
	}
	return out
}

// groupValues are the values an instance of type t holds in unique group
// g: those of every member property, each value on its own.
func (r *Registry) groupValues(t, g string, n *html.Node) []string {
	s := r.Schema(t)
	if s == nil {
		return nil
	}
	var out []string
	for _, pn := range s.eff.propOrder {
		if containsString(s.eff.props[pn].UniqueGroups, g) {
			out = append(out, valueStrings(propElements(n, pn))...)
		}
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// uniqueness checks individual and group uniqueness of the affected
// instances (R-MOD-30, R-MOD-31; docs snapshot 2026-11-23):
//   - a unique property's value may be held by one item of the declaring
//     type or a type beneath it, and only once on that item;
//   - the member properties of a group share one set of values, with the
//     same two rules, owned by the highest type declaring a member.
//
// A value held on another page is reported as "at 'another page'": the
// error never names a page other than the one asked for.
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
		for _, s := range p.reg.governing(inst) {
			t := s.URL
			var groupOrder []string
			for _, pn := range s.eff.propOrder {
				ep := s.eff.props[pn]
				groupOrder = union(groupOrder, ep.UniqueGroups)
				if !ep.Unique {
					continue
				}
				name := pn
				sel, constraint := fmt.Sprintf("[itemprop='%s']", pn), "unique("+pn+")"
				own := valueStrings(propElements(inst, pn))
				for _, v := range repeated(own) {
					add(UniqueViolation{Kind: LineUniqueness, Selector: sel, Constraint: constraint, ItemType: t, Property: pn, Value: v,
						Message: fmt.Sprintf("Uniqueness violation: item '%s' in '%s' holds this value more than once for property '%s'", itemIndexPath(inst), p.op.Path, pn)})
				}
				h := ix.holders(view, ep.UniqueOwner, "p:"+name, func(_ string, n *html.Node) []string { return valueStrings(propElements(n, name)) })
				for _, v := range dedupStrings(own) {
					elsewhere, first := p.conflict(h[v], inst)
					var msg string
					switch {
					case elsewhere:
						msg = fmt.Sprintf("Uniqueness violation: value already exists for property '%s' at 'another page'", pn)
					case first != nil:
						msg = fmt.Sprintf("Uniqueness violation: another item in '%s' already claims this value for property '%s' (first claimed by item '%s')",
							p.op.Path, pn, itemIndexPath(first))
					default:
						continue
					}
					add(UniqueViolation{Kind: LineUniqueness, Selector: sel, Constraint: constraint, ItemType: t, Property: pn, Value: v, Message: msg})
				}
			}
			for _, g := range groupOrder {
				group := g
				var sels []string
				for _, pn := range s.eff.propOrder {
					if containsString(s.eff.props[pn].UniqueGroups, g) {
						sels = append(sels, fmt.Sprintf("[itemprop='%s']", pn))
					}
				}
				sort.Strings(sels)
				sel, constraint := strings.Join(sels, ", "), "unique-group("+g+")"
				own := view.reg.groupValues(t, g, inst)
				for _, v := range repeated(own) {
					add(UniqueViolation{Kind: LineUniqueness, Selector: sel, Constraint: constraint, ItemType: t, Property: g, Value: v,
						Message: fmt.Sprintf("Uniqueness violation: item '%s' in '%s' holds this value more than once in group '%s'", itemIndexPath(inst), p.op.Path, g)})
				}
				h := ix.holders(view, s.eff.groupOwner[g], "g:"+g, func(ht string, n *html.Node) []string { return view.reg.groupValues(ht, group, n) })
				for _, v := range dedupStrings(own) {
					elsewhere, first := p.conflict(h[v], inst)
					var msg string
					switch {
					case elsewhere:
						msg = fmt.Sprintf("Uniqueness violation: value already exists in group '%s' at 'another page'", g)
					case first != nil:
						msg = fmt.Sprintf("Uniqueness violation: another item in '%s' already claims this value in group '%s' (first claimed by item '%s')",
							p.op.Path, g, itemIndexPath(first))
					default:
						continue
					}
					add(UniqueViolation{Kind: LineUniqueness, Selector: sel, Constraint: constraint, ItemType: t, Property: g, Value: v, Message: msg})
				}
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
