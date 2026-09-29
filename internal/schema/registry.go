package schema

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

// Registry is the host-wide schema registry at one write generation
// (R-MOD-2): every Schema, Enum and ShapeConstraint declared in any stored
// HTML document, with parent chains resolved and inherited members merged.
// It is immutable once built and shared by every request of the generation
// (site.Snapshot.Ext). It implements sessel.ClassRegistry.
type Registry struct {
	Generation int64

	schemas map[string]*Schema // by governed type URL (first declaration wins)
	order   []*Schema          // declaration order: path, then document order
	enums   map[string]*Enum
	shapes  []*Shape
	classes map[string]*Class

	// refUsers maps a referenced "type#prop" to the effective referencing
	// properties that point at it (for referential actions).
	refUsers map[string][]*refUse
	// hasRead is set when some schema has an @read resolver or computed
	// property (fast path for ApplyRead).
	hasRead bool
}

// Schema is one Schema declaration (R-MOD-8).
type Schema struct {
	URL         string
	Name        string
	Description string
	ParentURL   string
	Path        string // declaring document
	Node        *html.Node

	Props       []*PropDecl   // own properties (first declaration of a name wins)
	Methods     []*MethodDecl // own methods (overloads kept)
	Constraints []*GroupDecl  // own group constraints (first per group name)
	Validate    *Slot         // schema-level @validate

	// Resolved at build time.
	chain   []*Schema // leaf first, then ancestors that are registered
	loadErr string    // cycle in the parent chain
	eff     *effective
}

// PropDecl is one Property declaration (R-MOD-17).
type PropDecl struct {
	Name         string
	Type         string
	Cardinality  string // normalized: "" when omitted or unrecognized (0..n)
	Description  string
	Default      *Slot // static literal or code
	Unique       bool
	UniqueGroups []string
	References   string
	Cascade      string // "true", "restrict" or "" (none)
	Groups       []string
	Validate     *Slot
	Write        *Slot
	Read         *Slot // non-computed @read (Sessel/Lambda, JavaScript)
	Computed     *Slot // @computed, or the legacy bare-Sessel @read (R-MOD-44)
	Key          bool
	Owner        *Schema
}

// MethodDecl is one Method declaration (R-MOD-58).
type MethodDecl struct {
	Name           string
	Params         []string
	ParamTypes     []string
	Returns        string
	Static         bool
	Implementation *Slot
	Owner          *Schema
}

// GroupDecl is a group constraint (R-MOD-54).
type GroupDecl struct {
	Group       string
	Cardinality string
	Members     []string // recipe form: property values on the constraint
	Owner       *Schema
}

// Enum is an Enum type declaration (R-MOD-23).
type Enum struct {
	URL    string
	Values []string
	Path   string
}

// Slot languages.
const (
	LangStatic  = "static"
	LangSessel  = "sessel"
	LangJS      = "javascript"
	LangUnknown = "unknown"
)

// Slot is a binding slot value (R-MOD-7): a static literal, a Sessel
// expression, a JavaScript module or an item of an unknown language.
type Slot struct {
	Lang     string
	Source   string // expression, module source or static value
	ItemType string // the wrapper itemtype ("" for a bare element)
	Lambda   bool   // https://pagelove.org/Sessel/Lambda wrapper

	once     sync.Once
	compiled *sessel.Program
	parseErr error
}

// LanguageURL is the BindingFailure language of the slot.
func (s *Slot) LanguageURL() string {
	switch s.Lang {
	case LangJS:
		return URLJavaScript
	case LangUnknown:
		return s.ItemType
	}
	return URLSessel
}

// effective is a schema's view with inheritance applied (R-MOD-11).
type effective struct {
	props      map[string]*EffectiveProp
	propOrder  []string
	groups     []*effGroup
	validators []*Slot // schema-level, ancestor-first
	key        *EffectiveProp
	loadErrs   []Violation
}

// EffectiveProp is a property of a schema after inheritance.
type EffectiveProp struct {
	Name         string
	Decl         *PropDecl // most-derived declaration: scalar fields
	Unique       bool      // union across the chain
	UniqueGroups []string  // union across the chain
	Groups       []string  // union across the chain
	WriteChain   []*Slot   // leaf → root
	ReadChain    []*Slot   // root → leaf
	RefType      string    // valid reference target, when declared and valid
	RefProp      string
	AutoKey      bool // the honoured @key with no explicit default
}

// Cardinality is the declared cardinality of the most-derived declaration.
func (p *EffectiveProp) Cardinality() string { return p.Decl.Cardinality }

type effGroup struct {
	decl    *GroupDecl
	members []string
}

type refUse struct {
	leaf *Schema
	prop *EffectiveProp
}

const extKey = "schema.registry"

// For returns the registry of a snapshot, building it once per generation.
func For(snap *site.Snapshot) *Registry {
	if snap == nil {
		return emptyRegistry
	}
	return snap.Ext(extKey, func(s *site.Snapshot) any { return build(s) }).(*Registry)
}

var emptyRegistry = &Registry{schemas: map[string]*Schema{}, enums: map[string]*Enum{}, classes: map[string]*Class{}, refUsers: map[string][]*refUse{}}

// declarations returns the items of exact type t on the host, outside
// <template> contents, in path then document order.
func declarations(snap *site.Snapshot, t string) []site.TypedItem {
	var out []site.TypedItem
	for _, ti := range snap.ItemsOfType(t) {
		if itemType(ti.Item.Node) != t || inTemplate(ti.Item.Node) {
			continue
		}
		pd := snap.Docs[ti.Path]
		if pd == nil || site.IsXML(pd.Type) {
			continue
		}
		out = append(out, ti)
	}
	return out
}

func build(snap *site.Snapshot) *Registry {
	r := &Registry{Generation: snap.Generation, schemas: map[string]*Schema{}, enums: map[string]*Enum{},
		classes: map[string]*Class{}, refUsers: map[string][]*refUse{}}
	for _, ti := range declarations(snap, URLSchema) {
		s := parseSchema(ti.Path, ti.Item)
		if s == nil {
			continue // no type: silently skipped (R-MOD-8)
		}
		if prev, dup := r.schemas[s.URL]; dup {
			slog.Warn("schema: duplicate declaration ignored", "type", s.URL, "path", s.Path, "kept", prev.Path)
			continue
		}
		r.schemas[s.URL] = s
		r.order = append(r.order, s)
	}
	for _, ti := range declarations(snap, URLEnum) {
		e := parseEnum(ti.Path, ti.Item)
		if e == nil {
			continue
		}
		if _, dup := r.enums[e.URL]; !dup {
			r.enums[e.URL] = e
		}
	}
	for _, s := range r.order {
		r.resolveChain(s)
	}
	for _, s := range r.order {
		if s.loadErr == "" {
			s.eff = r.effectiveOf(s)
		}
	}
	for _, s := range r.order {
		if s.eff != nil {
			r.checkReferences(s)
		}
	}
	for _, s := range r.order {
		r.classes[s.URL] = newClass(r, s)
	}
	for _, ti := range declarations(snap, URLShape) {
		if sh := parseShape(r, ti.Path, ti.Item); sh != nil {
			r.shapes = append(r.shapes, sh)
		}
	}
	return r
}

// Schema returns the registered schema governing type URL t, or nil.
func (r *Registry) Schema(t string) *Schema { return r.schemas[t] }

// Schemas returns every registered schema in declaration order.
func (r *Registry) Schemas() []*Schema { return r.order }

// Enum returns the enum declared for URL u, or nil.
func (r *Registry) Enum(u string) *Enum { return r.enums[u] }

// Class implements sessel.ClassRegistry. It returns a nil interface (not a
// typed nil) when no schema declares url.
func (r *Registry) Class(url string) sessel.Class {
	if c, ok := r.classes[url]; ok {
		return c
	}
	return nil
}

// IsA reports whether type t is target or a declared descendant of it
// (R-MOD-12). Every type isa https://pagelove.org/Instance. Unknown parents
// end the chain; cycles are cut.
func (r *Registry) IsA(t, target string) bool {
	if target == URLInstance {
		return true
	}
	seen := map[string]bool{}
	for t != "" && !seen[t] {
		if t == target {
			return true
		}
		seen[t] = true
		s := r.schemas[t]
		if s == nil {
			return false
		}
		t = s.ParentURL
	}
	return false
}

// Property returns the effective property name of type t, or nil.
func (r *Registry) Property(t, name string) *EffectiveProp {
	s := r.schemas[t]
	if s == nil || s.eff == nil {
		return nil
	}
	return s.eff.props[name]
}

// PropertyNames returns the effective property names of type t (own and
// inherited, in declaration order), or nil for an unregistered type.
func (r *Registry) PropertyNames(t string) []string {
	s := r.schemas[t]
	if s == nil || s.eff == nil {
		return nil
	}
	return append([]string(nil), s.eff.propOrder...)
}

// KeyProperty returns the honoured @key property of type t (R-MOD-33).
func (r *Registry) KeyProperty(t string) *EffectiveProp {
	s := r.schemas[t]
	if s == nil || s.eff == nil {
		return nil
	}
	return s.eff.key
}

// Chain returns t's schema followed by its registered ancestors.
func (r *Registry) Chain(t string) []*Schema {
	if s := r.schemas[t]; s != nil {
		return s.chain
	}
	return nil
}

// ---------------------------------------------------------------- parsing

// field returns the first value of prop name, trimmed (R-MOD-3); nested
// items yield "".
func field(it *microdata.Item, name string) string {
	for _, p := range it.Props {
		if p.Name == name {
			if p.Item != nil {
				return ""
			}
			return dom.TrimHTMLSpace(p.Value)
		}
	}
	return ""
}

// fields returns every non-empty trimmed value of prop name.
func fields(it *microdata.Item, name string) []string {
	var out []string
	for _, p := range it.Props {
		if p.Name == name && p.Item == nil {
			if v := dom.TrimHTMLSpace(p.Value); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

func firstProp(it *microdata.Item, name string) (microdata.Prop, bool) {
	for _, p := range it.Props {
		if p.Name == name {
			return p, true
		}
	}
	return microdata.Prop{}, false
}

// slotOf reads a binding slot (R-MOD-7). static allows a plain literal
// (the default slot); other slots treat a plain element as a Sessel
// expression (a bare <script> is Sessel whatever its type attribute).
func slotOf(it *microdata.Item, name string, static bool) *Slot {
	p, ok := firstProp(it, name)
	if !ok {
		return nil
	}
	if p.Item != nil {
		t := itemType(p.Node)
		src := ""
		if sp, ok := firstProp(p.Item, "source"); ok && sp.Item == nil {
			src = strings.TrimSpace(sp.Value) // script text, raw (R-SESSEL-2)
		}
		switch t {
		case URLSessel:
			return &Slot{Lang: LangSessel, Source: src, ItemType: t}
		case URLSesselLambda:
			return &Slot{Lang: LangSessel, Source: src, ItemType: t, Lambda: true}
		case URLJavaScript:
			return &Slot{Lang: LangJS, Source: src, ItemType: t}
		}
		return &Slot{Lang: LangUnknown, Source: src, ItemType: t}
	}
	if p.Node.Data == "script" {
		return &Slot{Lang: LangSessel, Source: strings.TrimSpace(dom.TextContent(p.Node))}
	}
	if static {
		return &Slot{Lang: LangStatic, Source: p.Value}
	}
	return &Slot{Lang: LangSessel, Source: strings.TrimSpace(p.Value)}
}

func parseSchema(path string, it *microdata.Item) *Schema {
	s := &Schema{URL: field(it, "type"), Name: field(it, "name"), Description: field(it, "description"),
		ParentURL: field(it, "parent"), Path: path, Node: it.Node}
	if s.URL == "" {
		return nil
	}
	seen := map[string]bool{}
	groupSeen := map[string]bool{}
	for _, p := range it.Props {
		switch p.Name {
		case "property":
			if p.Item == nil {
				continue
			}
			if itemType(p.Node) == URLMethod {
				if m := parseMethod(s, p.Item); m != nil {
					s.Methods = append(s.Methods, m)
				}
				continue
			}
			d := parseProp(s, p.Item)
			if d == nil || seen[d.Name] {
				continue
			}
			seen[d.Name] = true
			s.Props = append(s.Props, d)
		case "constraint":
			if p.Item == nil {
				continue
			}
			g := &GroupDecl{Group: field(p.Item, "group"), Cardinality: field(p.Item, "cardinality"), Members: fields(p.Item, "property"), Owner: s}
			if g.Group == "" || groupSeen[g.Group] {
				continue // no group: silently skipped (R-MOD-54)
			}
			if !validCardinality(g.Cardinality) {
				g.Cardinality = ""
			}
			groupSeen[g.Group] = true
			s.Constraints = append(s.Constraints, g)
		}
	}
	s.Validate = slotOf(it, "@validate", false)
	return s
}

func parseProp(owner *Schema, it *microdata.Item) *PropDecl {
	d := &PropDecl{Name: field(it, "name"), Type: field(it, "type"), Cardinality: field(it, "cardinality"),
		Description: field(it, "description"), References: field(it, "references"), Owner: owner}
	if d.Name == "" {
		return nil // silently skipped (R-MOD-17)
	}
	if !validCardinality(d.Cardinality) {
		d.Cardinality = "" // unrecognized: treated as omitted (R-MOD-18)
	}
	for _, u := range fields(it, "unique") {
		if u == "true" {
			d.Unique = true
		} else if u != "false" {
			d.UniqueGroups = append(d.UniqueGroups, u)
		}
	}
	switch c := field(it, "cascade"); c {
	case "true", "restrict":
		d.Cascade = c
	}
	d.Groups = fields(it, "group")
	d.Default = slotOf(it, "default", true)
	d.Validate = slotOf(it, "@validate", false)
	d.Write = slotOf(it, "@write", false)
	if r := slotOf(it, "@read", false); r != nil {
		if r.ItemType == URLSessel {
			d.Computed = r // legacy form (R-MOD-44, C13)
		} else {
			d.Read = r
		}
	}
	if c := slotOf(it, "@computed", false); c != nil {
		d.Computed = c // @computed wins over @read
	}
	d.Key = field(it, "@key") == "true"
	return d
}

func parseMethod(owner *Schema, it *microdata.Item) *MethodDecl {
	m := &MethodDecl{Name: field(it, "name"), Returns: field(it, "returns"), Static: field(it, "static") == "true", Owner: owner}
	for _, p := range it.Props {
		if p.Name == "parameter" && p.Item != nil {
			if n := field(p.Item, "name"); n != "" {
				m.Params = append(m.Params, n)
				m.ParamTypes = append(m.ParamTypes, field(p.Item, "type"))
			}
		}
	}
	m.Implementation = slotOf(it, "implementation", false)
	if m.Name == "" || m.Implementation == nil {
		return nil // ignored (R-MOD-58, pagelike decision)
	}
	return m
}

func parseEnum(path string, it *microdata.Item) *Enum {
	u := field(it, "type")
	if u == "" {
		u = field(it, "name")
	}
	if u == "" {
		return nil
	}
	e := &Enum{URL: u, Path: path}
	for _, p := range it.Props {
		if p.Name == "value" && p.Item == nil {
			e.Values = append(e.Values, dom.TrimHTMLSpace(p.Value))
		}
	}
	return e
}

// ---------------------------------------------------------------- inheritance

// resolveChain walks the parent chain (R-MOD-10): unknown parents stop it
// silently, a cycle is a load error for every schema reaching it.
func (r *Registry) resolveChain(s *Schema) {
	s.chain = []*Schema{s}
	seen := map[*Schema]bool{s: true}
	for cur := s; cur.ParentURL != ""; {
		p := r.schemas[cur.ParentURL]
		if p == nil {
			return
		}
		if seen[p] {
			s.loadErr = fmt.Sprintf("the parent chain of %s is cyclic (reaches %s again)", s.URL, p.URL)
			return
		}
		seen[p] = true
		s.chain = append(s.chain, p)
		cur = p
	}
}

// effectiveOf merges the chain's members (R-MOD-11).
func (r *Registry) effectiveOf(s *Schema) *effective {
	e := &effective{props: map[string]*EffectiveProp{}}
	// Walk root → leaf so that each level overrides the previous one.
	for i := len(s.chain) - 1; i >= 0; i-- {
		lvl := s.chain[i]
		for _, d := range lvl.Props {
			ep := e.props[d.Name]
			if ep == nil {
				ep = &EffectiveProp{Name: d.Name}
				e.props[d.Name] = ep
				e.propOrder = append(e.propOrder, d.Name)
			}
			ep.Decl = d // most-derived supplies the scalar fields
			ep.Unique = ep.Unique || d.Unique
			ep.UniqueGroups = union(ep.UniqueGroups, d.UniqueGroups)
			ep.Groups = union(ep.Groups, d.Groups)
			if d.Write != nil {
				ep.WriteChain = append([]*Slot{d.Write}, ep.WriteChain...) // leaf → root
			}
			if d.Read != nil {
				ep.ReadChain = append(ep.ReadChain, d.Read) // root → leaf
			}
		}
		if lvl.Validate != nil {
			e.validators = append(e.validators, lvl.Validate)
		}
	}
	for _, ep := range e.props {
		if ep.Decl.Computed != nil || len(ep.ReadChain) > 0 {
			r.hasRead = true
		}
	}
	// Group constraints: most-derived per group name; members are the union
	// of Property.group across the chain plus the recipe form (R-MOD-55/56).
	byGroup := map[string]*GroupDecl{}
	var groupOrder []string
	for i := len(s.chain) - 1; i >= 0; i-- {
		for _, g := range s.chain[i].Constraints {
			if _, ok := byGroup[g.Group]; !ok {
				groupOrder = append(groupOrder, g.Group)
			}
			byGroup[g.Group] = g
		}
	}
	for _, name := range groupOrder {
		g := byGroup[name]
		eg := &effGroup{decl: g}
		for _, pn := range e.propOrder {
			for _, gn := range e.props[pn].Groups {
				if gn == name {
					eg.members = append(eg.members, pn)
					break
				}
			}
		}
		for i := len(s.chain) - 1; i >= 0; i-- {
			for _, cg := range s.chain[i].Constraints {
				if cg.Group == name {
					eg.members = union(eg.members, cg.Members)
				}
			}
		}
		e.groups = append(e.groups, eg)
	}
	// @key: own properties in document order first, then the parent's
	// (R-MOD-33); honoured only with individual unique=true.
	for _, lvl := range s.chain {
		for _, d := range lvl.Props {
			if !d.Key {
				continue
			}
			ep := e.props[d.Name]
			if ep != nil && ep.Unique && ep.Decl.Computed == nil {
				e.key = ep
				break
			}
		}
		if e.key != nil {
			break
		}
	}
	if e.key != nil && e.key.Decl.Default == nil {
		e.key.AutoKey = true
	}
	// Compile failures of @validate reject every write (R-MOD-48).
	for _, pn := range e.propOrder {
		ep := e.props[pn]
		if v := ep.Decl.Validate; v != nil && v.Lang == LangSessel {
			if err := v.compile(); err != nil {
				e.loadErrs = append(e.loadErrs, Violation{Check: CheckValidate, ItemType: s.URL, Property: pn,
					Message: fmt.Sprintf("[%s].%s: @validate does not compile: %v", s.URL, pn, err)})
			}
		}
	}
	for _, v := range e.validators {
		if v.Lang == LangSessel {
			if err := v.compile(); err != nil {
				e.loadErrs = append(e.loadErrs, Violation{Check: CheckValidate, ItemType: s.URL,
					Message: fmt.Sprintf("[%s]: @validate does not compile: %v", s.URL, err)})
			}
		}
	}
	return e
}

// checkReferences validates `references` declarations (R-MOD-37): the
// target property must be individually unique on the target type.
func (r *Registry) checkReferences(s *Schema) {
	for _, pn := range s.eff.propOrder {
		ep := s.eff.props[pn]
		ref := ep.Decl.References
		if ref == "" {
			continue
		}
		i := strings.LastIndexByte(ref, '#')
		var t, p string
		if i >= 0 {
			t, p = strings.TrimSpace(ref[:i]), strings.TrimSpace(ref[i+1:])
		}
		msg := ""
		switch {
		case t == "" || p == "":
			msg = fmt.Sprintf("[%s].%s: references %q is not of the form {itemtype}#{itemprop}", s.URL, pn, ref)
		default:
			tp := r.Property(t, p)
			if tp == nil || !tp.Unique {
				msg = fmt.Sprintf("[%s].%s: references %s, which is not declared unique=true on %s", s.URL, pn, ref, t)
			}
		}
		if msg != "" {
			s.eff.loadErrs = append(s.eff.loadErrs, Violation{Check: CheckSchema, ItemType: s.URL, Property: pn, Message: msg})
			continue
		}
		ep.RefType, ep.RefProp = t, p
		key := t + "#" + p
		r.refUsers[key] = append(r.refUsers[key], &refUse{leaf: s, prop: ep})
	}
}

func union(a, b []string) []string {
	for _, x := range b {
		found := false
		for _, y := range a {
			if x == y {
				found = true
				break
			}
		}
		if !found {
			a = append(a, x)
		}
	}
	return a
}

// compile parses a Sessel slot once; safe for concurrent use.
func (s *Slot) compile() error {
	s.once.Do(func() { s.compiled, s.parseErr = sessel.Compile(s.Source) })
	return s.parseErr
}
