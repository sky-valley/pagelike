package schema

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

// Referential actions (R-MOD-39..43). When a write removes or changes a
// value that other instances reference, the referencing property's cascade
// decides: restrict refuses the write with 409 (and upgrades every
// referencing property of the same target), true deletes or rewrites the
// referrers, and none leaves them dangling. Cascades apply recursively and
// are planned during validation; AfterWrite stores them in the write's
// transaction, each with its mutation event.

type cascadePlan struct {
	puts    []plannedPut
	deletes []string
	at      time.Time
}

type plannedPut struct {
	path, ct string
	body     []byte
}

// plans holds the cascades of writes between Validate and AfterWrite. A
// write refused after Validate (by a later hook) never reaches AfterWrite;
// its plan is dropped by the sweep in storePlan.
var plans sync.Map // *engine.WriteCtx → *cascadePlan

const planTTL = 5 * time.Minute

func storePlan(w *engine.WriteCtx, plan *cascadePlan) {
	now := time.Now()
	plans.Range(func(k, v any) bool {
		if now.Sub(v.(*cascadePlan).at) > planTTL {
			plans.Delete(k)
		}
		return true
	})
	plan.at = now
	plans.Store(w, plan)
}

// AfterWrite is the engine AfterWrite hook: it applies the cascades that
// Validate planned for this write.
func AfterWrite(ctx context.Context, w *engine.WriteCtx, res *engine.Result) error {
	v, ok := plans.LoadAndDelete(w)
	if !ok {
		return nil
	}
	plan := v.(*cascadePlan)
	for _, pp := range plan.puts {
		if _, err := w.SideEffectPut(pp.path, pp.ct, pp.body); err != nil {
			return err
		}
	}
	for _, d := range plan.deletes {
		if err := w.SideEffectDelete(d); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	return nil
}

// cdoc is a document touched by the cascade.
type cdoc struct {
	path    string
	src     *srcDoc
	deleted bool
	current bool // the document of the write itself (its tree is D′)
}

type cascade struct {
	p        *pass
	view     *hostView
	docs     map[string]*cdoc
	restrict []UniqueViolation
	queue    []change
}

// change is a document whose referenced values may have disappeared or
// changed: before and after trees (after nil when deleted).
type change struct {
	path          string
	before, after *html.Node
}

// referential runs stage 11 for the current write.
func (p *pass) referential() error {
	if len(p.reg.refUsers) == 0 {
		return nil
	}
	c := &cascade{p: p, view: newHostView(p.w.Snap, p.reg), docs: map[string]*cdoc{}}
	var after *html.Node
	if p.kind != kindDocDelete {
		after = p.after
		c.docs[p.op.Path] = &cdoc{path: p.op.Path, src: p.doc, current: true}
	} else {
		c.docs[p.op.Path] = &cdoc{path: p.op.Path, deleted: true, current: true}
	}
	c.view.over[p.op.Path] = after
	c.queue = append(c.queue, change{path: p.op.Path, before: p.w.Before, after: after})
	for rounds := 0; len(c.queue) > 0 && rounds < 1000; rounds++ {
		ch := c.queue[0]
		c.queue = c.queue[1:]
		if err := c.process(ch); err != nil {
			return err
		}
	}
	if len(c.restrict) > 0 {
		return restrictError(c.restrict)
	}
	plan := &cascadePlan{}
	paths := make([]string, 0, len(c.docs))
	for path := range c.docs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		d := c.docs[path]
		switch {
		case d.current && d.deleted && p.kind == kindDocDelete:
			// the write itself deletes it
		case d.deleted:
			plan.deletes = append(plan.deletes, path)
		case d.current:
			if p.doc != nil {
				p.docHTML = ""
			}
		case d.src.changed:
			plan.puts = append(plan.puts, plannedPut{path: path, ct: d.src.ct, body: d.src.bytes()})
		}
	}
	if len(plan.puts)+len(plan.deletes) > 0 {
		storePlan(p.w, plan)
	}
	return nil
}

// valueSets returns, per instance type and referenced property, the values
// held in the tree.
func (c *cascade) valueSets(root *html.Node, props map[string]bool) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	if root == nil {
		return out
	}
	for _, n := range instancesUnder(root) {
		t := itemType(n)
		if c.p.reg.schemas[t] == nil {
			continue
		}
		for pn := range props {
			k := t + "#" + pn
			for _, v := range valueStrings(propElements(n, pn)) {
				if out[k] == nil {
					out[k] = map[string]bool{}
				}
				out[k][v] = true
			}
		}
	}
	return out
}

// referencedProps lists the property names that some reference targets.
func (c *cascade) referencedProps() map[string]bool {
	out := map[string]bool{}
	for _, users := range c.p.reg.refUsers {
		if len(users) > 0 {
			out[users[0].prop.RefProp] = true
		}
	}
	return out
}

type lostValue struct {
	typ, prop, value string
	changedTo        *string
}

// diff finds the referenced values a change removed or changed: values the
// document no longer holds, and, for instances paired across the change
// (same position, or same @key), a single value replaced by another.
func (c *cascade) diff(ch change) []lostValue {
	props := c.referencedProps()
	before, after := c.valueSets(ch.before, props), c.valueSets(ch.after, props)
	lost := map[string]map[string]bool{}
	for k, vals := range before {
		for v := range vals {
			if !after[k][v] {
				if lost[k] == nil {
					lost[k] = map[string]bool{}
				}
				lost[k][v] = true
			}
		}
	}
	changed := map[string]map[string]string{}
	if ch.before != nil && ch.after != nil {
		for _, pair := range c.pairs(ch.before, ch.after) {
			t := itemType(pair[0])
			for pn := range props {
				k := t + "#" + pn
				bv, av := valueStrings(propElements(pair[0], pn)), valueStrings(propElements(pair[1], pn))
				gone, added := minus(bv, av), minus(av, bv)
				if len(gone) == 1 && len(added) == 1 && lost[k][gone[0]] {
					if changed[k] == nil {
						changed[k] = map[string]string{}
					}
					changed[k][gone[0]] = added[0]
				}
			}
		}
	}
	var out []lostValue
	keys := make([]string, 0, len(lost))
	for k := range lost {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t, pn := splitKey(k)
		vals := make([]string, 0, len(lost[k]))
		for v := range lost[k] {
			vals = append(vals, v)
		}
		sort.Strings(vals)
		for _, v := range vals {
			lv := lostValue{typ: t, prop: pn, value: v}
			if to, ok := changed[k][v]; ok {
				lv.changedTo = &to
			}
			out = append(out, lv)
		}
	}
	return out
}

func splitKey(k string) (string, string) {
	for i := len(k) - 1; i >= 0; i-- {
		if k[i] == '#' {
			return k[:i], k[i+1:]
		}
	}
	return k, ""
}

func minus(a, b []string) []string {
	var out []string
	for _, x := range dedupStrings(a) {
		found := false
		for _, y := range b {
			if x == y {
				found = true
				break
			}
		}
		if !found {
			out = append(out, x)
		}
	}
	return out
}

// pairs matches governed instances of the same type across a change: by
// position first, then by @key value, then as the lone instance of their
// type (R-MOD-42).
func (c *cascade) pairs(before, after *html.Node) [][2]*html.Node {
	bi, ai := instancesUnder(before), instancesUnder(after)
	used := map[*html.Node]bool{}
	var out [][2]*html.Node
	byPath := map[string]*html.Node{}
	for _, a := range ai {
		byPath[fmt.Sprint(nodePath(a))] = a
	}
	var rest []*html.Node
	for _, b := range bi {
		t := itemType(b)
		if c.p.reg.schemas[t] == nil {
			continue
		}
		if a := byPath[fmt.Sprint(nodePath(b))]; a != nil && !used[a] && itemType(a) == t {
			used[a] = true
			out = append(out, [2]*html.Node{b, a})
			continue
		}
		rest = append(rest, b)
	}
	for _, b := range rest {
		t := itemType(b)
		key := c.p.reg.KeyProperty(t)
		var cands []*html.Node
		for _, a := range ai {
			if !used[a] && itemType(a) == t {
				cands = append(cands, a)
			}
		}
		var match *html.Node
		if key != nil {
			kb := valueStrings(propElements(b, key.Name))
			for _, a := range cands {
				if ka := valueStrings(propElements(a, key.Name)); len(kb) == 1 && len(ka) == 1 && kb[0] == ka[0] {
					match = a
					break
				}
			}
		}
		if match == nil && len(cands) == 1 {
			nb := 0
			for _, x := range bi {
				if itemType(x) == t {
					nb++
				}
			}
			if nb == 1 {
				match = cands[0]
			}
		}
		if match != nil {
			used[match] = true
			out = append(out, [2]*html.Node{b, match})
		}
	}
	return out
}

// process applies the referential actions for one change.
func (c *cascade) process(ch change) error {
	reg := c.p.reg
	keys := make([]string, 0, len(reg.refUsers))
	for k := range reg.refUsers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, lv := range c.diff(ch) {
		for _, key := range keys {
			users := reg.refUsers[key]
			if len(users) == 0 {
				continue
			}
			target, prop := users[0].prop.RefType, users[0].prop.RefProp
			if prop != lv.prop || !reg.IsA(lv.typ, target) {
				continue
			}
			if c.view.valuesOf(target, prop)[lv.value] {
				continue // still held by another instance of the target type
			}
			restrict := false
			for _, u := range users {
				if u.prop.Decl.Cascade == "restrict" {
					restrict = true
				}
			}
			for _, u := range users {
				refs := c.referrers(u, lv.value)
				if len(refs) == 0 {
					continue
				}
				mode := u.prop.Decl.Cascade
				if restrict {
					mode = "restrict"
				}
				switch mode {
				case "restrict":
					// PageLove's wording (live 2026-09-29): the referenced
					// property, restrict(<target>#<prop>), and the number of
					// referring documents.
					docs := map[string]bool{}
					for _, r := range refs {
						docs[r.path] = true
					}
					c.restrict = append(c.restrict, UniqueViolation{Kind: LineRestrict,
						Selector: fmt.Sprintf("[itemprop='%s']", prop), Constraint: "restrict(" + target + "#" + prop + ")",
						ItemType: u.leaf.URL, Property: u.prop.Name, Value: lv.value,
						Message: fmt.Sprintf("Cannot delete: %d document(s) reference this value via '%s'", len(docs), u.prop.Name)})
				case "true":
					if err := c.apply(u, lv, refs); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// referrers returns the instances of the referencing type that hold value
// in the referencing property.
func (c *cascade) referrers(u *refUse, value string) []instRef {
	var out []instRef
	for _, in := range c.view.ofType(u.leaf.URL) {
		for _, v := range valueStrings(propElements(in.node, u.prop.Name)) {
			if v == value {
				out = append(out, in)
				break
			}
		}
	}
	return out
}

// open returns the cascade's working copy of a document, loading it from
// the transaction (so earlier writes in the transaction are seen).
func (c *cascade) open(path string) (*cdoc, error) {
	if d, ok := c.docs[path]; ok {
		return d, nil
	}
	sd, err := c.p.w.Tx.Get(path)
	if err != nil {
		return nil, err
	}
	root, err := site.ParseMarkup(sd.ContentType, sd.Body)
	if err != nil {
		return nil, err
	}
	d := &cdoc{path: path, src: newSrcDoc(path, sd.ContentType, sd.Body, root)}
	c.docs[path] = d
	c.view.over[path] = root
	return d, nil
}

// locate finds, in the working copy, the node at the same position as n in
// the snapshot's parse of the same stored bytes.
func locate(d *cdoc, n *html.Node) *html.Node {
	if attached(d.src.root, n) {
		return n
	}
	return nodeAt(d.src.root, nodePath(n))
}

// apply performs cascade=true on the referrers (R-MOD-41/42).
func (c *cascade) apply(u *refUse, lv lostValue, refs []instRef) error {
	byDoc := map[string][]*html.Node{}
	var order []string
	for _, r := range refs {
		if _, ok := byDoc[r.path]; !ok {
			order = append(order, r.path)
		}
		byDoc[r.path] = append(byDoc[r.path], r.node)
	}
	for _, path := range order {
		d, err := c.open(path)
		if err != nil {
			return err
		}
		if d.deleted {
			continue
		}
		before := dom.Clone(d.src.root)
		deleteDoc := false
		for _, n := range byDoc[path] {
			inst := locate(d, n)
			if inst == nil {
				continue
			}
			var matching []*html.Node
			all := propElements(inst, u.prop.Name)
			for _, e := range all {
				if v := valueOf(e); !v.null && !v.item && v.s == lv.value {
					matching = append(matching, e)
				}
			}
			if lv.changedTo != nil {
				for _, e := range matching {
					cl := dom.Clone(e)
					setValue(cl, *lv.changedTo)
					d.src.replace(e, []*html.Node{cl})
				}
				continue
			}
			switch u.prop.Decl.Cardinality {
			case "1..1":
				deleteDoc = true
			case "1..n":
				if len(matching) >= len(all) {
					deleteDoc = true
					break
				}
				fallthrough
			default:
				for _, e := range matching {
					d.src.remove(e)
				}
			}
		}
		if deleteDoc {
			d.deleted = true
			c.view.over[path] = nil
			c.queue = append(c.queue, change{path: path, before: before, after: nil})
			continue
		}
		if d.current {
			c.p.docHTML = ""
		}
		c.queue = append(c.queue, change{path: path, before: before, after: d.src.root})
	}
	return nil
}
