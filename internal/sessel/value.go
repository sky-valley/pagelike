package sessel

import (
	"math"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// Value is a Sessel runtime value (R-SESSEL-90). Its dynamic type is one of:
//
//	nil            Null
//	bool           Boolean
//	int64          Integer
//	float64        Float
//	string         String
//	List           List (immutable)
//	*Dict          Dictionary (ordered; type namespace Map)
//	*Element       Element — queried (with provenance) or constructed; a
//	               Document is the root element of a queried document and an
//	               Instance is an Element with a Class
//	*SelectorValue Selector
//	*Lambda        Lambda
//	Class          Class (schema class)
//	*TypeNS        a type namespace (Number, String, Temporal.PlainDate, …)
//	*Blob          a non-HTML stored resource (Pagelove.GET)
//	PlainDate, PlainTime, PlainDateTime, Instant, ZonedDateTime, Duration,
//	PlainYearMonth, PlainMonthDay — the Temporal types
type Value = any

// List is an immutable ordered sequence.
type List []Value

// Dict is an insertion-ordered dictionary with String keys. A folding Dict
// (request headers) stores lower-case keys and looks keys up ASCII
// case-insensitively (R-SESSEL-293).
type Dict struct {
	keys []string
	vals map[string]Value
	fold bool
}

// NewDict returns an empty dictionary.
func NewDict() *Dict { return &Dict{vals: map[string]Value{}} }

// NewFoldDict returns an empty dictionary with case-insensitive keys.
func NewFoldDict() *Dict { return &Dict{vals: map[string]Value{}, fold: true} }

func (d *Dict) key(k string) string {
	if d.fold {
		return strings.ToLower(k)
	}
	return k
}

// Get returns the value for k.
func (d *Dict) Get(k string) (Value, bool) {
	if d == nil {
		return nil, false
	}
	v, ok := d.vals[d.key(k)]
	return v, ok
}

// Lookup returns the value for k or nil.
func (d *Dict) Lookup(k string) Value { v, _ := d.Get(k); return v }

// Set adds or replaces k (a replaced key keeps its position).
func (d *Dict) Set(k string, v Value) {
	k = d.key(k)
	if _, ok := d.vals[k]; !ok {
		d.keys = append(d.keys, k)
	}
	d.vals[k] = v
}

// Delete removes k.
func (d *Dict) Delete(k string) {
	k = d.key(k)
	if _, ok := d.vals[k]; !ok {
		return
	}
	delete(d.vals, k)
	for i, x := range d.keys {
		if x == k {
			d.keys = append(d.keys[:i:i], d.keys[i+1:]...)
			break
		}
	}
}

// Keys returns the keys in insertion order.
func (d *Dict) Keys() []string {
	if d == nil {
		return nil
	}
	return append([]string(nil), d.keys...)
}

// Len returns the number of entries.
func (d *Dict) Len() int {
	if d == nil {
		return 0
	}
	return len(d.keys)
}

// Copy returns a shallow copy.
func (d *Dict) Copy() *Dict {
	c := &Dict{keys: append([]string(nil), d.keys...), vals: make(map[string]Value, len(d.vals)), fold: d.fold}
	for k, v := range d.vals {
		c.vals[k] = v
	}
	return c
}

// Document is a stored markup document as seen by Sessel: the provenance of
// queried elements. Root is the parsed document node; it must never be
// mutated (snapshots are shared).
type Document struct {
	Path string
	Type string // content type ("" means text/html)
	Root *html.Node
	Meta *Dict // Pagelove.GET metadata, when known
}

// Element returns the document's root element as a queried element.
func (d *Document) Element() *Element {
	r := documentElement(d.Root)
	if r == nil {
		return nil
	}
	return &Element{Node: r, Doc: d}
}

// Element is an element value. Queried elements (Doc != nil or Mutable
// false) are immutable snapshots; constructed elements are mutable
// (R-SESSEL-220). An Element with a Class is an Instance.
type Element struct {
	Node    *html.Node
	Doc     *Document
	Class   Class
	Mutable bool
}

// NewElement wraps a detached node as a constructed (mutable) element.
func NewElement(n *html.Node) *Element { return &Element{Node: n, Mutable: true} }

// Queried wraps a node of a stored document (immutable, with provenance).
func Queried(n *html.Node, doc *Document) *Element { return &Element{Node: n, Doc: doc} }

func (e *Element) derive(n *html.Node) *Element {
	return &Element{Node: n, Doc: e.Doc, Mutable: e.Mutable}
}

// isDocRoot reports whether e is the root element of its source document.
func (e *Element) isDocRoot() bool {
	return e.Doc != nil && e.Doc.Root != nil && documentElement(e.Doc.Root) == e.Node
}

// Blob is a non-HTML resource returned by Pagelove.GET (R-SESSEL-246).
type Blob struct {
	Path string
	Meta *Dict
}

// SelectorValue is an inert Selector (R-SESSEL-210).
type SelectorValue struct{ Source string }

// Lambda is a first-class function: a Sessel lambda closure or a native
// function (static method references, host callbacks).
type Lambda struct {
	params []string
	rest   string
	body   node
	scope  *scope
	env    *evaluator // evaluator that created it (for cross-evaluation calls)
	native func(c *Call, args []Value) (Value, error)
	arity  int
	name   string
}

// Arity is the number of declared non-rest parameters.
func (l *Lambda) Arity() int {
	if l.native != nil {
		return l.arity
	}
	return len(l.params)
}

// NativeFunc wraps a Go function as a Sessel lambda.
func NativeFunc(name string, arity int, f func(c *Call, args []Value) (Value, error)) *Lambda {
	return &Lambda{native: f, arity: arity, name: name}
}

// TypeNS is a type namespace value (R-SESSEL-95): Number, Integer, Float,
// String, Bool, Null, Element, List, Map, Document, Temporal, Selector,
// Class, Instance and the Temporal sub-namespaces.
type TypeNS struct{ Name string }

var typeNamespaces = map[string]*TypeNS{}

func init() {
	for _, n := range []string{"Number", "Integer", "Float", "String", "Bool", "Null", "Element", "List", "Map",
		"Document", "Temporal", "Selector", "Class", "Instance",
		"Temporal.PlainDate", "Temporal.PlainTime", "Temporal.PlainDateTime", "Temporal.Instant",
		"Temporal.ZonedDateTime", "Temporal.Duration", "Temporal.PlainYearMonth", "Temporal.PlainMonthDay",
		"Temporal.Now"} {
		typeNamespaces[n] = &TypeNS{Name: n}
	}
}

// TypeName returns the user-facing type name of v (used in messages such as
// "Cannot add String and Integer", R-SESSEL-80).
func TypeName(v Value) string {
	switch x := v.(type) {
	case nil:
		return "Null"
	case bool:
		return "Boolean"
	case int64:
		return "Integer"
	case float64:
		return "Float"
	case string:
		return "String"
	case List:
		return "List"
	case *Dict:
		return "Map"
	case *Element:
		if x.Class != nil {
			return "Instance"
		}
		return "Element"
	case *SelectorValue:
		return "Selector"
	case *Lambda:
		return "Lambda"
	case Class:
		return "Class"
	case *TypeNS:
		return "Type"
	case *Blob:
		return "Blob"
	case temporal:
		return "Temporal." + x.temporalType()
	}
	return "Value"
}

// Truthy implements R-SESSEL-93.
func Truthy(v Value) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case int64:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x != ""
	case List:
		return len(x) > 0
	case *Dict:
		return x.Len() > 0
	}
	return true
}

// FormatFloat renders a Float in application/sessel+json (R-SESSEL-97):
// integral values below 1e16 keep a ".0"; otherwise the shortest round-trip
// form, with exponent notation outside 1e-5 <= |x| < 1e16 ("1e+16",
// "1.5e-7", as live 2026-09-29). The text representation is TextFloat.
func FormatFloat(f float64) string {
	if f == 0 {
		if math.Signbit(f) {
			return "-0.0"
		}
		return "0.0"
	}
	a := math.Abs(f)
	if f == math.Trunc(f) && a < 1e16 {
		return strconv.FormatFloat(f, 'f', -1, 64) + ".0"
	}
	if a >= 1e-5 && a < 1e16 {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	s := strconv.FormatFloat(f, 'e', -1, 64) // 1e+16, 1.5e-07
	mant, exp, _ := strings.Cut(s, "e")
	sign := "+"
	if exp[0] == '-' {
		sign = "-"
	}
	exp = strings.TrimLeft(exp[1:], "0")
	if exp == "" {
		exp = "0"
	}
	return mant + "e" + sign + exp
}

// TextFloat is the text representation of a Float (R-SESSEL-92, live
// 2026-09-29): the shortest round-trip digits in plain decimal notation,
// with no ".0" on integral values and never an exponent ("42", "2.5",
// "10000000000000000", "0.00000015", "-0").
func TextFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// TextOf is the text representation of v (R-SESSEL-89) as used by
// interpolation, join, construction and comparisons by text; null → "".
func TextOf(v Value) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return TextFloat(x)
	case *Element:
		if s, ok := elementValue(x.Node).(string); ok {
			return s
		}
		return ""
	case *SelectorValue:
		return x.Source
	case temporal:
		return x.String()
	case Class:
		return x.URL()
	case *TypeNS:
		return x.Name
	case *Lambda:
		return "<lambda>"
	case *Blob:
		return x.Path
	case List, *Dict:
		b, err := EncodeJSON(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
	return ""
}

// Equal implements == (R-SESSEL-83).
func Equal(a, b Value) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case int64:
		switch y := b.(type) {
		case int64:
			return x == y
		case float64:
			return float64(x) == y
		}
		return false
	case float64:
		switch y := b.(type) {
		case int64:
			return x == float64(y)
		case float64:
			return x == y
		}
		return false
	case string:
		y, ok := b.(string)
		return ok && x == y
	case List:
		y, ok := b.(List)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !Equal(x[i], y[i]) {
				return false
			}
		}
		return true
	case *Dict:
		y, ok := b.(*Dict)
		if !ok || x.Len() != y.Len() {
			return false
		}
		for _, k := range x.keys {
			yv, ok := y.Get(k)
			if !ok || !Equal(x.vals[k], yv) {
				return false
			}
		}
		return true
	case *Element:
		y, ok := b.(*Element)
		return ok && x.Node == y.Node
	case *SelectorValue:
		y, ok := b.(*SelectorValue)
		return ok && x.Source == y.Source
	case temporal:
		y, ok := b.(temporal)
		return ok && temporalEquals(x, y)
	case Class:
		y, ok := b.(Class)
		return ok && x.URL() == y.URL()
	case *TypeNS:
		y, ok := b.(*TypeNS)
		return ok && x.Name == y.Name
	}
	return a == b
}

// compareValues orders two values (R-SESSEL-84): numbers numerically,
// strings by code point, same-type Temporal values by compare. ok is false
// for incomparable types.
func compareValues(a, b Value) (int, bool) {
	switch x := a.(type) {
	case int64:
		switch y := b.(type) {
		case int64:
			return cmp3(x < y, x > y), true
		case float64:
			return cmpFloat(float64(x), y), true
		}
	case float64:
		switch y := b.(type) {
		case int64:
			return cmpFloat(x, float64(y)), true
		case float64:
			return cmpFloat(x, y), true
		}
	case string:
		if y, ok := b.(string); ok {
			return strings.Compare(x, y), true
		}
	case temporal:
		if y, ok := b.(temporal); ok && x.temporalType() == y.temporalType() {
			if c, ok := temporalCompare(x, y); ok {
				return c, true
			}
		}
	}
	return 0, false
}

func cmp3(lt, gt bool) int {
	switch {
	case lt:
		return -1
	case gt:
		return 1
	}
	return 0
}

func cmpFloat(a, b float64) int { return cmp3(a < b, a > b) }
