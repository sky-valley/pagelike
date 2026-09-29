package reactions

import (
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

// Item types (docs/spec/reacting.md R-REACT-1).
const (
	TypeTrigger              = "https://pagelove.org/Trigger"
	TypeProcessor            = "https://pagelove.org/Processor"
	TypeTransitionConstraint = "https://pagelove.org/TransitionConstraint"
	TypeTransitionHandler    = "https://pagelove.org/TransitionHandler"
	TypeHttpRequest          = "https://pagelove.org/HttpRequest"
	TypeHTTPRequestAlias     = "https://pagelove.org/HTTPRequest" // R-REACT-48, decision C10
	TypeSessel               = sessel.URLSessel
	TypeJavaScript           = "https://pagelove.org/JavaScript/Module"
	TypePair                 = sessel.URLPair
	TypeTransition           = "https://pagelove.org/Transition"
	TypeConstraintViolation  = "https://pagelove.org/ConstraintViolation"
	TypeViolation            = "https://pagelove.org/Violation"
	TypeSchemaViolation      = "https://pagelove.org/SchemaViolation"
	TypeBindingFailure       = "https://pagelove.org/BindingFailure"
)

// Languages of dynamic values.
const (
	langStatic = ""
	langSessel = "sessel"
	langJS     = "js"
	langOther  = "other" // an item of another type: malformed where code is required
)

// value is a property value: static text or dynamic code (R-REACT-3).
type value struct {
	text   string // static value (untrimmed)
	lang   string
	source string // code (Sessel program or JS module)
}

func (v *value) dynamic() bool { return v.lang != langStatic }

// action is one action/otherwise item (R-REACT-18).
type action struct {
	lang string // langSessel, langJS, or "http"
	code string
	http *httpAction
}

// header is one declared request header (R-REACT-52).
type header struct {
	key   string // "" drops the pair
	value *value
}

// httpAction is an HttpRequest action item (R-REACT-48).
type httpAction struct {
	url, method, contentType, body *value // nil: absent
	headers                        []header
	retry                          int  // clamped to 0..4 (R-REACT-57)
	dynamic                        bool // url, method or a header value is code (disables handlers, R-REACT-80)
}

// binding is an expression binding in scope of a reaction item
// (R-REACT-24; composing R-COMP-35/40).
type binding struct {
	name string
	kind string // "sessel", "js"
	src  string
}

// rule is a Trigger or Processor (R-REACT-11..18).
type rule struct {
	processor bool
	path      string // declaring document
	order     int    // position among the document's reaction items
	resources []*authz.Glob
	methods   []string // upper case; "*" matches all
	statuses  []string // processors: "404", "4xx"
	statusRaw int
	when      []*value
	actions   []*action
	otherwise []*action
	bindings  []binding
}

// constraint is a TransitionConstraint (R-REACT-62..63).
type constraint struct {
	path     string
	sel      *selector.Selector
	property string
	from, to []string
	hasFrom  bool
	hasTo    bool
}

// handler is a TransitionHandler (R-REACT-76..80).
type handler struct {
	path     string
	branches []*selector.Selector // qualifying type branches (R-REACT-77)
	property string
	becomes  []string
	when     *value
	actions  []*httpAction
}

// index is the per-snapshot set of reaction items (R-REACT-2), cached in
// site.Snapshot.Ext.
type index struct {
	triggers    []*rule
	processors  []*rule
	constraints []*constraint
	handlers    []*handler
	types       *typeInfo
	// malformed counts items that were skipped (diagnostics).
	malformed int
}

func (ix *index) empty() bool {
	return len(ix.triggers) == 0 && len(ix.processors) == 0 && len(ix.constraints) == 0 && len(ix.handlers) == 0
}

const extKey = "reactions.index"

// indexFor returns the reaction items of a snapshot (cached per generation).
func indexFor(snap *site.Snapshot) *index {
	snap = snap.Configuration()
	return snap.Ext(extKey, func(s *site.Snapshot) any { return buildIndex(s) }).(*index)
}

func buildIndex(snap *site.Snapshot) *index {
	ix := &index{types: buildTypes(snap)}
	ext := selector.ExtOptions{IsA: ix.types.isa}
	for _, p := range snap.Paths { // sorted by path: byte order (R-REACT-35)
		pd := snap.Docs[p]
		if pd == nil || pd.Root == nil {
			continue
		}
		order := 0
		walkItems(pd.Root, func(n *html.Node) {
			kinds := ix.types.reactionKinds(n)
			if len(kinds) == 0 {
				return
			}
			order++
			it := microdata.Parse(n)
			for _, k := range kinds {
				switch k {
				case TypeTrigger, TypeProcessor:
					if r := parseRule(p, order, n, it, k == TypeProcessor); r != nil {
						if r.processor {
							ix.processors = append(ix.processors, r)
						} else {
							ix.triggers = append(ix.triggers, r)
						}
					} else {
						ix.malformed++
					}
				case TypeTransitionConstraint:
					if c := parseConstraint(p, it, ext); c != nil {
						ix.constraints = append(ix.constraints, c)
					} else {
						ix.malformed++
					}
				case TypeTransitionHandler:
					if h := parseHandler(p, it, ext); h != nil {
						ix.handlers = append(ix.handlers, h)
					} else {
						ix.malformed++
					}
				}
			}
		})
	}
	return ix
}

// walkItems visits itemscope elements in document order, never descending
// into <template> contents (R-REACT-2).
func walkItems(root *html.Node, f func(*html.Node)) {
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if dom.HasAttr(n, "itemscope") {
				f(n)
			}
			if selector.IsTemplate(n) {
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}

// ---------------------------------------------------------------- types

// typeInfo is the part of the schema registry reactions need: the parent
// chain (subtype discovery, :isa) and honoured primary keys (R-MOD-10,
// R-MOD-33), read from https://pagelove.org/Schema items.
type typeInfo struct {
	parent map[string]string
	key    map[string]string // the type's own honoured @key property
}

func buildTypes(snap *site.Snapshot) *typeInfo {
	t := &typeInfo{parent: map[string]string{}, key: map[string]string{}}
	seen := map[string]bool{}
	for _, p := range snap.Paths {
		pd := snap.Docs[p]
		if pd == nil || pd.Root == nil {
			continue
		}
		walkItems(pd.Root, func(n *html.Node) {
			if !hasToken(dom.AttrOr(n, "itemtype", ""), sessel.URLSchema) {
				return
			}
			it := microdata.Parse(n)
			typ := strings.TrimSpace(propText(it, "type"))
			if typ == "" || seen[typ] {
				return // the first declaration of a type wins
			}
			seen[typ] = true
			if par := strings.TrimSpace(propText(it, "parent")); par != "" {
				t.parent[typ] = par
			}
			for _, pp := range it.Props {
				if pp.Name != "property" || pp.Item == nil || pp.Item.HasType(sessel.URLMethod) {
					continue
				}
				pi := pp.Item
				if !strings.EqualFold(strings.TrimSpace(propText(pi, "@key")), "true") {
					continue
				}
				unique := false
				for _, u := range pi.All("unique") {
					if strings.EqualFold(strings.TrimSpace(u), "true") {
						unique = true
					}
				}
				if name := strings.TrimSpace(propText(pi, "name")); unique && name != "" {
					t.key[typ] = name // honoured only with unique: true; first wins
					break
				}
			}
		})
	}
	return t
}

// chain returns typ and its declared ancestors (cycle-safe).
func (t *typeInfo) chain(typ string) []string {
	out := []string{typ}
	seen := map[string]bool{typ: true}
	for cur := typ; ; {
		p, ok := t.parent[cur]
		if !ok || seen[p] {
			return out
		}
		seen[p] = true
		out = append(out, p)
		cur = p
	}
}

// isa reports whether typ is target or a declared descendant of it.
func (t *typeInfo) isa(typ, target string) bool {
	if t == nil {
		return typ == target
	}
	for _, x := range t.chain(typ) {
		if x == target {
			return true
		}
	}
	return false
}

// keyOf is the honoured primary key property of typ, inherited from the
// nearest ancestor that declares one ("" when none).
func (t *typeInfo) keyOf(typ string) string {
	if t == nil {
		return ""
	}
	for _, x := range t.chain(typ) {
		if k := t.key[x]; k != "" {
			return k
		}
	}
	return ""
}

var reactionTypes = []string{TypeTrigger, TypeProcessor, TypeTransitionConstraint, TypeTransitionHandler}

// reactionKinds returns the reaction base types an item element is (itself
// or through a schema-declared subtype), each at most once.
func (t *typeInfo) reactionKinds(n *html.Node) []string {
	var out []string
	for _, tok := range strings.Fields(dom.AttrOr(n, "itemtype", "")) {
		for _, rt := range reactionTypes {
			if t.isa(tok, rt) && !containsStr(out, rt) {
				out = append(out, rt)
			}
		}
	}
	return out
}

// ---------------------------------------------------------------- parsing

// propText is the static value of an item's first property name.
func propText(it *microdata.Item, name string) string {
	for _, p := range it.Props {
		if p.Name == name {
			if p.Item != nil {
				return ""
			}
			return p.Value
		}
	}
	return ""
}

// readValue reads a property value (R-REACT-3): an element that is itself a
// Sessel or JavaScript/Module item is code (language by itemtype), another
// item is langOther, anything else static text.
func readValue(p microdata.Prop) *value {
	if p.Item == nil {
		return &value{text: p.Value}
	}
	switch {
	case p.Item.HasType(TypeSessel):
		return &value{lang: langSessel, source: codeSource(p.Item)}
	case p.Item.HasType(TypeJavaScript):
		return &value{lang: langJS, source: codeSource(p.Item)}
	}
	return &value{lang: langOther}
}

// codeSource is the text of a code item's source property ("" when absent).
func codeSource(it *microdata.Item) string {
	for _, p := range it.Props {
		if p.Name == "source" && p.Item == nil {
			return dom.TextContent(p.Node)
		}
	}
	return ""
}

// filterValues returns the trimmed, non-empty static values of a filter
// property.
func filterValues(it *microdata.Item, name string) []string {
	var out []string
	for _, p := range it.Props {
		if p.Name != name || p.Item != nil {
			continue
		}
		if v := strings.TrimSpace(p.Value); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// stateValues returns the untrimmed static values of a state property
// (from, to, becomes); the empty string is a value (R-REACT-63).
func stateValues(it *microdata.Item, name string) ([]string, bool) {
	var out []string
	found := false
	for _, p := range it.Props {
		if p.Name != name || p.Item != nil {
			continue
		}
		found = true
		out = append(out, p.Value)
	}
	return out, found
}

func parseRule(path string, order int, n *html.Node, it *microdata.Item, processor bool) *rule {
	r := &rule{processor: processor, path: path, order: order}
	for _, g := range filterValues(it, "resource") {
		r.resources = append(r.resources, authz.CompileGlob(g))
	}
	for _, m := range filterValues(it, "method") {
		r.methods = append(r.methods, strings.ToUpper(m))
	}
	// The selector filter is not evaluated (R-REACT-14, live 2026-09-29).
	if processor {
		for _, s := range filterValues(it, "status") {
			r.statusRaw++
			if validStatusPattern(s) {
				r.statuses = append(r.statuses, strings.ToLower(s))
			}
		}
	}
	for _, p := range it.Props {
		switch p.Name {
		case "when":
			v := readValue(p)
			if v.lang != langSessel && v.lang != langJS || strings.TrimSpace(v.source) == "" {
				return nil // a malformed gate skips the item (R-REACT-4)
			}
			r.when = append(r.when, v)
		case "action", "otherwise":
			a := parseAction(p)
			if a == nil {
				continue // an unknown action item is dropped (R-REACT-4)
			}
			if p.Name == "action" {
				r.actions = append(r.actions, a)
			} else {
				r.otherwise = append(r.otherwise, a)
			}
		}
	}
	if len(r.actions) == 0 && len(r.otherwise) == 0 {
		return nil // R-REACT-4, R-REACT-18
	}
	r.bindings = bindingsFor(n)
	return r
}

func parseAction(p microdata.Prop) *action {
	if p.Item == nil {
		return nil
	}
	switch {
	case p.Item.HasType(TypeSessel):
		return &action{lang: langSessel, code: codeSource(p.Item)}
	case p.Item.HasType(TypeJavaScript):
		return &action{lang: langJS, code: codeSource(p.Item)}
	case p.Item.HasType(TypeHttpRequest), p.Item.HasType(TypeHTTPRequestAlias):
		return &action{lang: "http", http: parseHTTP(p.Item)}
	}
	return nil
}

func parseHTTP(it *microdata.Item) *httpAction {
	h := &httpAction{}
	for _, p := range it.Props {
		switch p.Name {
		case "url", "method", "content-type", "body":
			if h.slot(p.Name) != nil {
				continue // the first value of each property counts
			}
			v := readValue(p)
			switch p.Name {
			case "url":
				h.url = v
			case "method":
				h.method = v
			case "content-type":
				h.contentType = v
			case "body":
				h.body = v
			}
			if v.dynamic() && p.Name != "body" && p.Name != "content-type" {
				h.dynamic = true
			}
		case "header":
			if p.Item != nil {
				// A key/value pair (https://pagelove.org/Pair); a missing
				// key drops it, an absent value sends an empty field.
				hd := header{key: strings.TrimSpace(propText(p.Item, "key")), value: &value{}}
				for _, pp := range p.Item.Props {
					if pp.Name == "value" {
						hd.value = readValue(pp)
						break
					}
				}
				if hd.value.dynamic() {
					h.dynamic = true
				}
				h.headers = append(h.headers, hd)
				continue
			}
			// Single-line form "Name: value", split at the first colon.
			name, val, ok := strings.Cut(p.Value, ":")
			if !ok {
				h.headers = append(h.headers, header{})
				continue
			}
			h.headers = append(h.headers, header{key: strings.TrimSpace(name), value: &value{text: strings.TrimSpace(val)}})
		case "retry":
			if p.Item != nil {
				h.retry = 0 // a dynamic retry counts as 0 (R-REACT-55)
				continue
			}
			n, err := strconv.Atoi(strings.TrimSpace(p.Value))
			if err != nil || n < 0 {
				n = 0
			}
			h.retry = min(n, 4)
		}
	}
	return h
}

func (h *httpAction) slot(name string) *value {
	switch name {
	case "url":
		return h.url
	case "method":
		return h.method
	case "content-type":
		return h.contentType
	case "body":
		return h.body
	}
	return nil
}

func parseConstraint(path string, it *microdata.Item, ext selector.ExtOptions) *constraint {
	c := &constraint{path: path}
	sels := filterValues(it, "selector")
	props := filterValues(it, "property")
	if len(sels) == 0 || len(props) == 0 {
		return nil // R-REACT-62: ignored
	}
	c.property = props[0]
	c.from, c.hasFrom = stateValues(it, "from")
	c.to, c.hasTo = stateValues(it, "to")
	if !c.hasFrom && !c.hasTo {
		return nil
	}
	sel, err := selector.CompileWith(sels[0], ext)
	if err != nil {
		return nil // a selector that does not parse never matches (R-REACT-4)
	}
	c.sel = sel
	return c
}

func parseHandler(path string, it *microdata.Item, ext selector.ExtOptions) *handler {
	h := &handler{path: path}
	sels := filterValues(it, "selector")
	props := filterValues(it, "property")
	becomes, ok := stateValues(it, "becomes")
	if len(sels) == 0 || len(props) == 0 || !ok {
		return nil // R-REACT-76: never fires
	}
	h.property, h.becomes = props[0], becomes
	for _, branch := range splitSelectorList(sels[0]) {
		if !typeBranch(branch) {
			continue
		}
		if s, err := selector.CompileWith(branch, ext); err == nil {
			h.branches = append(h.branches, s)
		}
	}
	if len(h.branches) == 0 {
		return nil // R-REACT-77: no qualifying branch
	}
	for _, p := range it.Props {
		switch p.Name {
		case "when":
			v := readValue(p)
			if v.lang != langSessel || strings.TrimSpace(v.source) == "" {
				return nil // a malformed gate fails closed (R-REACT-79)
			}
			h.when = v
		case "action":
			if p.Item == nil || !(p.Item.HasType(TypeHttpRequest) || p.Item.HasType(TypeHTTPRequestAlias)) {
				return nil // any non-HttpRequest action disables the handler (R-REACT-80)
			}
			a := parseHTTP(p.Item)
			if a.dynamic {
				return nil // expressions in url, method or headers (R-REACT-80)
			}
			h.actions = append(h.actions, a)
		}
	}
	if len(h.actions) == 0 {
		return nil
	}
	return h
}

// splitSelectorList splits a selector list on top-level commas.
func splitSelectorList(s string) []string {
	var out []string
	depth, start := 0, 0
	var quote rune
	for i, r := range s {
		switch {
		case quote != 0:
			if r == '\\' {
				continue
			}
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '(' || r == '[':
			depth++
		case r == ')' || r == ']':
			depth--
		case r == ',' && depth == 0:
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// typeBranch reports whether a selector branch consists solely of type
// predicates: [itemtype='T'] (the = operator, quoted, no flags) and
// :isa('T'), possibly compounded (R-REACT-77).
func typeBranch(b string) bool {
	if b == "" {
		return false
	}
	for b != "" {
		switch {
		case strings.HasPrefix(b, "["):
			end := closing(b, ']')
			if end < 0 {
				return false
			}
			inner := strings.TrimSpace(b[1:end])
			name, val, ok := strings.Cut(inner, "=")
			if !ok || strings.TrimSpace(name) != "itemtype" {
				return false
			}
			if !quoted(strings.TrimSpace(val)) {
				return false // other operators leave a trailing character on name; flags break quoting
			}
			b = b[end+1:]
		case strings.HasPrefix(strings.ToLower(b), ":isa("):
			end := closing(b, ')')
			if end < 0 {
				return false
			}
			b = b[end+1:]
		default:
			return false
		}
	}
	return true
}

// closing returns the index of the delimiter closing the construct that
// starts at b[0], skipping quoted strings.
func closing(b string, delim byte) int {
	var quote byte
	for i := 1; i < len(b); i++ {
		c := b[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == delim:
			return i
		}
	}
	return -1
}

func quoted(s string) bool {
	if len(s) < 2 {
		return false
	}
	q := s[0]
	if q != '\'' && q != '"' || s[len(s)-1] != q {
		return false
	}
	for i := 1; i < len(s)-1; i++ {
		switch s[i] {
		case '\\':
			i++
		case q:
			return false // the string ends before the last character
		}
	}
	return true
}

// bindingsFor collects the expression binding attributes (e:, j:) on n and
// its ancestors, the outermost first, so nearer declarations override
// (R-REACT-24). Resource bindings (r:) are not bound in trigger or
// processor code: PageLove leaves their names undefined (live 2026-09-29).
func bindingsFor(n *html.Node) []binding {
	var chain []*html.Node
	for e := n; e != nil && e.Type == html.ElementNode; e = e.Parent {
		chain = append(chain, e)
	}
	var out []binding
	for i := len(chain) - 1; i >= 0; i-- {
		e := chain[i]
		for _, a := range e.Attr {
			prefix, local, ok := strings.Cut(a.Key, ":")
			if !ok || prefix == "xmlns" || local == "" {
				continue
			}
			kind := ""
			switch namespaceURI(e, prefix) {
			case "https://pagelove.org/Binding/Sessel":
				kind = "sessel"
			case "https://pagelove.org/Binding/JavaScript":
				kind = "js"
			default:
				continue
			}
			out = append(out, binding{name: local, kind: kind, src: a.Val})
		}
	}
	return out
}

// namespaceURI resolves a prefix through the xmlns declarations in scope.
func namespaceURI(n *html.Node, prefix string) string {
	for e := n; e != nil && e.Type == html.ElementNode; e = e.Parent {
		if v, ok := dom.Attr(e, "xmlns:"+prefix); ok {
			return v
		}
	}
	return ""
}

func hasToken(list, tok string) bool {
	for _, t := range strings.Fields(list) {
		if t == tok {
			return true
		}
	}
	return false
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func validStatusPattern(s string) bool {
	if len(s) != 3 {
		return false
	}
	if s[0] < '1' || s[0] > '5' {
		return false
	}
	if (s[1] == 'x' || s[1] == 'X') && (s[2] == 'x' || s[2] == 'X') {
		return true
	}
	return s[1] >= '0' && s[1] <= '9' && s[2] >= '0' && s[2] <= '9'
}
