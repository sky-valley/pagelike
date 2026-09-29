package jsrt

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Value is a dombase value crossing the Go/JavaScript boundary. The dynamic
// types are:
//
//	nil            null
//	Undefined      undefined (inputs only: `this` of most slots)
//	bool
//	int64          integers within ±(2^53−1) (inputs also accept the other
//	               Go integer types and json.Number)
//	float64        finite numbers
//	string
//	[]Value        lists (inputs also accept []any and []string)
//	*Dict          dictionaries, insertion ordered (inputs also accept
//	               map[string]any, marshalled in sorted key order)
//	*Element       elements, tagged {"$type":"element","$html":…,"$source":…}
//	*Instance      instances of a registered schema
//	*Class         a registered schema class (inputs only)
//	Temporal       temporal values (inputs only, R-JS-42)
//	*Headers       case-insensitive header objects (inputs only, R-JS-24)
//	*Request       the j: `request` object (inputs only, R-JS-34)
//
// Results only ever contain nil, bool, int64, float64, string, []Value,
// *Dict, *Element and *Instance.
type Value = any

// Undefined is the JavaScript undefined value. CallRequest.This defaults to
// it; pass nil for null.
type Undefined struct{}

// Dict is an insertion-ordered string-keyed dictionary.
type Dict struct {
	keys []string
	m    map[string]Value
}

// NewDict returns a dictionary holding kv pairs (key, value, key, value, …).
func NewDict(kv ...any) *Dict {
	d := &Dict{m: map[string]Value{}}
	for i := 0; i+1 < len(kv); i += 2 {
		d.Set(fmt.Sprint(kv[i]), kv[i+1])
	}
	return d
}

// Set adds or replaces key; a new key goes last.
func (d *Dict) Set(key string, v Value) {
	if d.m == nil {
		d.m = map[string]Value{}
	}
	if _, ok := d.m[key]; !ok {
		d.keys = append(d.keys, key)
	}
	d.m[key] = v
}

// Get returns the value under key.
func (d *Dict) Get(key string) (Value, bool) {
	if d == nil {
		return nil, false
	}
	v, ok := d.m[key]
	return v, ok
}

// Delete removes key.
func (d *Dict) Delete(key string) {
	if d == nil {
		return
	}
	if _, ok := d.m[key]; !ok {
		return
	}
	delete(d.m, key)
	for i, k := range d.keys {
		if k == key {
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

// Len is the number of entries.
func (d *Dict) Len() int {
	if d == nil {
		return 0
	}
	return len(d.keys)
}

// MarshalJSON encodes the dictionary as a JSON object in insertion order
// (elements and instances in their tagged form).
func (d *Dict) MarshalJSON() ([]byte, error) { return MarshalJSON(d) }

// Element is an element value. As an input it is either a node of the
// request's Document (Node set, and inside Document.Node's tree), or
// self-contained markup (HTML, or Node serialized) parsed in the worker as a
// detached element. Elements from stored trees are read-only in JavaScript;
// set Writable for elements constructed during this request.
//
// As a result, HTML is the element's outer HTML and Source the document path
// it belongs to ("" for elements the binding constructed, as in
// application/sessel+json).
type Element struct {
	HTML     string
	Source   string
	Node     *html.Node
	Writable bool
}

// Instance is an instance of a registered schema: Props holds the declared
// properties that are set (a List for multi-valued properties), in
// declaration order. For Map schemas Entries holds the map entries.
type Instance struct {
	Type    string
	Props   *Dict
	Entries *Dict
}

// Class is a registered schema class (a Sessel Class value).
type Class struct {
	Type string
}

// Temporal is a Sessel temporal value. Kind is one of Instant,
// ZonedDateTime, PlainDateTime, PlainDate (these become a JavaScript Date,
// UTC) or PlainTime, PlainYearMonth, PlainMonthDay, Duration (these become
// their ISO string), R-JS-42.
type Temporal struct {
	Kind string
	ISO  string
}

// Headers is a header object: JavaScript sees own enumerable lower-cased
// keys and looks names up case-insensitively (R-JS-24).
type Headers struct {
	names []string
	vals  map[string]string
}

// NewHeaders builds a header object from an http.Header-like map, joining
// repeated values with ", ".
func NewHeaders(h map[string][]string) *Headers {
	out := &Headers{vals: map[string]string{}}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out.Add(k, strings.Join(h[k], ", "))
	}
	return out
}

// Add appends a header value (joined with ", " when the name repeats).
func (h *Headers) Add(name, value string) {
	if h.vals == nil {
		h.vals = map[string]string{}
	}
	k := strings.ToLower(name)
	if old, ok := h.vals[k]; ok {
		h.vals[k] = old + ", " + value
		return
	}
	h.names = append(h.names, k)
	h.vals[k] = value
}

// Request is the `request` object of j: bindings (R-JS-34). Reading Headers
// or Auth (or enumerating them) taints the evaluation (Result.Tainted).
// A nil Auth means an anonymous request: request.auth is undefined.
type Request struct {
	Method  string
	Path    string
	Query   *Dict
	Params  *Dict
	Body    Value
	Headers *Headers
	Auth    Value
	// Extra holds further shared (non-tainting) members.
	Extra *Dict
}

// ---------------------------------------------------------------- wire encoding

// maxSafeInt is 2^53−1, the largest integer a JavaScript number holds exactly.
const maxSafeInt = 1<<53 - 1

// maxDepth bounds the nesting of marshalled values (R-JS-41).
const maxDepth = 64

// wireElem is one element of a value, shipped out of band so the JSON of the
// value can pass through the worker untouched.
type wireElem struct {
	HTML     string `json:"html,omitempty"`
	Source   string `json:"source,omitempty"`
	Ref      int    `json:"ref,omitempty"` // 1 = the document root; >1 = marker ref into the document
	Writable bool   `json:"w,omitempty"`
	Context  string `json:"ctx,omitempty"` // tag of the parent element, for fragment parsing
	XML      bool   `json:"xml,omitempty"`
}

// valueEncoder marshals Go values to the wire JSON: plain JSON plus tagged
// objects ({"$type": …}). Elements go to elems and appear as {"$type":
// "element","$i":n}.
type valueEncoder struct {
	buf     bytes.Buffer
	elems   []wireElem
	schemas map[string]bool
	refs    func(*html.Node) (int, bool) // resolves nodes of the request document
}

type marshalError struct{ msg string }

func (e *marshalError) Error() string { return e.msg }

func (e *valueEncoder) fail(format string, a ...any) error {
	return &marshalError{fmt.Sprintf(format, a...)}
}

func (e *valueEncoder) encode(v Value, depth int) error {
	if depth > maxDepth {
		return e.fail("value nests deeper than %d levels", maxDepth)
	}
	b := &e.buf
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case Undefined, *Undefined:
		b.WriteString(`{"$type":"undefined"}`)
	case nullValue:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int:
		return e.int(int64(x))
	case int8:
		return e.int(int64(x))
	case int16:
		return e.int(int64(x))
	case int32:
		return e.int(int64(x))
	case int64:
		return e.int(x)
	case uint:
		return e.uint(uint64(x))
	case uint8:
		return e.uint(uint64(x))
	case uint16:
		return e.uint(uint64(x))
	case uint32:
		return e.uint(uint64(x))
	case uint64:
		return e.uint(x)
	case float32:
		return e.float(float64(x))
	case float64:
		return e.float(x)
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return e.int(i)
		}
		f, err := x.Float64()
		if err != nil {
			return e.fail("invalid number %q", string(x))
		}
		return e.float(f)
	case string:
		writeJSONString(b, x)
	case []Value:
		return e.list(len(x), func(i int) Value { return x[i] }, depth)
	case []string:
		return e.list(len(x), func(i int) Value { return x[i] }, depth)
	case *Dict:
		if x == nil {
			b.WriteString("null")
			return nil
		}
		return e.dict(x.keys, func(k string) Value { return x.m[k] }, depth)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return e.dict(keys, func(k string) Value { return x[k] }, depth)
	case *Element:
		if x == nil {
			b.WriteString("null")
			return nil
		}
		return e.element(x)
	case Element:
		return e.element(&x)
	case *Instance:
		if x == nil {
			b.WriteString("null")
			return nil
		}
		return e.instance(x, depth)
	case Instance:
		return e.instance(&x, depth)
	case *Class:
		if x == nil {
			b.WriteString("null")
			return nil
		}
		return e.class(x.Type)
	case Class:
		return e.class(x.Type)
	case Temporal:
		return e.temporal(x)
	case *Temporal:
		if x == nil {
			b.WriteString("null")
			return nil
		}
		return e.temporal(*x)
	case *Headers:
		if x == nil {
			b.WriteString("null")
			return nil
		}
		b.WriteString(`{"$type":"headers","$entries":[`)
		for i, k := range x.names {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteByte('[')
			writeJSONString(b, k)
			b.WriteByte(',')
			writeJSONString(b, x.vals[k])
			b.WriteByte(']')
		}
		b.WriteString("]}")
	case *Request:
		if x == nil {
			b.WriteString("null")
			return nil
		}
		return e.request(x, depth)
	default:
		if l, ok := v.([]any); ok {
			return e.list(len(l), func(i int) Value { return l[i] }, depth)
		}
		return e.fail("a %T has no JavaScript form", v)
	}
	return nil
}

func (e *valueEncoder) int(i int64) error {
	if i > maxSafeInt || i < -maxSafeInt {
		return e.fail("integer %d is outside ±(2^53−1)", i)
	}
	e.buf.WriteString(strconv.FormatInt(i, 10))
	return nil
}

func (e *valueEncoder) uint(u uint64) error {
	if u > maxSafeInt {
		return e.fail("integer %d is outside ±(2^53−1)", u)
	}
	e.buf.WriteString(strconv.FormatUint(u, 10))
	return nil
}

func (e *valueEncoder) float(f float64) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return e.fail("number %v has no JSON form", f)
	}
	e.buf.WriteString(strconv.FormatFloat(f, 'g', -1, 64))
	return nil
}

func (e *valueEncoder) list(n int, at func(int) Value, depth int) error {
	e.buf.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			e.buf.WriteByte(',')
		}
		if err := e.encode(at(i), depth+1); err != nil {
			return err
		}
	}
	e.buf.WriteByte(']')
	return nil
}

func (e *valueEncoder) dict(keys []string, at func(string) Value, depth int) error {
	tagged := false
	for _, k := range keys {
		if k == "$type" {
			tagged = true
		}
	}
	b := &e.buf
	if tagged {
		// A user dictionary with a "$type" key would read as a tagged value.
		b.WriteString(`{"$type":"dict","$entries":[`)
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteByte('[')
			writeJSONString(b, k)
			b.WriteByte(',')
			if err := e.encode(at(k), depth+1); err != nil {
				return err
			}
			b.WriteByte(']')
		}
		b.WriteString("]}")
		return nil
	}
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		writeJSONString(b, k)
		b.WriteByte(':')
		if err := e.encode(at(k), depth+1); err != nil {
			return err
		}
	}
	b.WriteByte('}')
	return nil
}

func (e *valueEncoder) element(x *Element) error {
	we := wireElem{Source: x.Source, Writable: x.Writable}
	if x.Node != nil {
		if e.refs != nil {
			if ref, ok := e.refs(x.Node); ok {
				we.Ref = ref
			}
		}
		if we.Ref == 0 {
			if x.Node.Type != html.ElementNode {
				return e.fail("an Element value must hold an element node")
			}
			we.HTML = outerHTML(x.Node)
			if p := x.Node.Parent; p != nil && p.Type == html.ElementNode {
				we.Context = p.Data
			}
			we.XML = isXMLNode(x.Node)
		}
	} else {
		we.HTML = x.HTML
	}
	e.elems = append(e.elems, we)
	fmt.Fprintf(&e.buf, `{"$type":"element","$i":%d}`, len(e.elems)-1)
	return nil
}

func (e *valueEncoder) instance(x *Instance, depth int) error {
	if x.Type == "" {
		return e.fail("an Instance needs its schema type URL")
	}
	if e.schemas != nil {
		e.schemas[x.Type] = true
	}
	b := &e.buf
	b.WriteString(`{"$type":"instance","$itemtype":`)
	writeJSONString(b, x.Type)
	b.WriteString(`,"$props":`)
	props := x.Props
	if props == nil {
		props = NewDict()
	}
	if err := e.dict(props.keys, func(k string) Value { return props.m[k] }, depth+1); err != nil {
		return err
	}
	if x.Entries != nil {
		b.WriteString(`,"$entries":`)
		if err := e.dict(x.Entries.keys, func(k string) Value { return x.Entries.m[k] }, depth+1); err != nil {
			return err
		}
	}
	b.WriteByte('}')
	return nil
}

func (e *valueEncoder) class(t string) error {
	if t == "" {
		return e.fail("a Class needs its schema type URL")
	}
	if e.schemas != nil {
		e.schemas[t] = true
	}
	e.buf.WriteString(`{"$type":"class","$itemtype":`)
	writeJSONString(&e.buf, t)
	e.buf.WriteByte('}')
	return nil
}

// temporal maps Temporal values per R-JS-42: date-like kinds become a Date
// (epoch milliseconds), the others their ISO string.
func (e *valueEncoder) temporal(t Temporal) error {
	switch t.Kind {
	case "Instant", "ZonedDateTime", "PlainDateTime", "PlainDate":
		ms, err := temporalMillis(t)
		if err != nil {
			return e.fail("temporal %s %q: %v", t.Kind, t.ISO, err)
		}
		fmt.Fprintf(&e.buf, `{"$type":"date","$ms":%d}`, ms)
	case "PlainTime", "PlainYearMonth", "PlainMonthDay", "Duration":
		writeJSONString(&e.buf, t.ISO)
	default:
		return e.fail("unknown temporal kind %q", t.Kind)
	}
	return nil
}

func temporalMillis(t Temporal) (int64, error) {
	s := t.ISO
	if i := strings.IndexByte(s, '['); i >= 0 { // ZonedDateTime annotation
		s = s[:i]
	}
	layouts := []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02"}
	for _, l := range layouts {
		if tm, err := time.Parse(l, s); err == nil {
			return tm.UnixMilli(), nil
		}
	}
	return 0, errors.New("not an ISO 8601 date-time")
}

func (e *valueEncoder) request(r *Request, depth int) error {
	b := &e.buf
	b.WriteString(`{"$type":"request","shared":{`)
	n := 0
	field := func(k string, v Value) error {
		if n > 0 {
			b.WriteByte(',')
		}
		n++
		writeJSONString(b, k)
		b.WriteByte(':')
		return e.encode(v, depth+2)
	}
	if err := field("method", r.Method); err != nil {
		return err
	}
	if err := field("path", r.Path); err != nil {
		return err
	}
	q := r.Query
	if q == nil {
		q = NewDict()
	}
	if err := field("query", q); err != nil {
		return err
	}
	p := r.Params
	if p == nil {
		p = NewDict()
	}
	if err := field("params", p); err != nil {
		return err
	}
	if r.Body != nil {
		if err := field("body", r.Body); err != nil {
			return err
		}
	}
	if r.Extra != nil {
		for _, k := range r.Extra.keys {
			if err := field(k, r.Extra.m[k]); err != nil {
				return err
			}
		}
	}
	b.WriteString(`},"private":{`)
	n = 0
	h := r.Headers
	if h == nil {
		h = &Headers{}
	}
	if err := field("headers", h); err != nil {
		return err
	}
	if r.Auth != nil {
		if err := field("auth", r.Auth); err != nil {
			return err
		}
	}
	b.WriteString("}}")
	return nil
}

// writeJSONString writes s as a JSON string without the HTML escaping of
// encoding/json (markup stays readable in application/sessel+json).
func writeJSONString(b *bytes.Buffer, s string) {
	enc := json.NewEncoder(b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	b.Truncate(b.Len() - 1) // Encode appends a newline
}

// MarshalJSON encodes a Value as application/sessel+json-compatible JSON:
// elements and instances become {"$type":"element","$html":…,"$source":…}
// (the source omitted for constructed elements); dictionaries keep their
// order.
func MarshalJSON(v Value) ([]byte, error) {
	var b bytes.Buffer
	if err := marshalPlain(&b, v, 0); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func marshalPlain(b *bytes.Buffer, v Value, depth int) error {
	if depth > 4*maxDepth {
		return errors.New("value nests too deeply")
	}
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case []Value:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := marshalPlain(b, e, depth+1); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case *Dict:
		b.WriteByte('{')
		for i, k := range x.keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, k)
			b.WriteByte(':')
			if err := marshalPlain(b, x.m[k], depth+1); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case *Element:
		b.WriteString(`{"$type":"element","$html":`)
		writeJSONString(b, x.HTML)
		if x.Source != "" {
			b.WriteString(`,"$source":`)
			writeJSONString(b, x.Source)
		}
		b.WriteByte('}')
	case *Instance:
		b.WriteString(`{"$type":"instance","$itemtype":`)
		writeJSONString(b, x.Type)
		if x.Props != nil {
			b.WriteString(`,"$props":`)
			if err := marshalPlain(b, x.Props, depth+1); err != nil {
				return err
			}
		}
		if x.Entries != nil {
			b.WriteString(`,"$entries":`)
			if err := marshalPlain(b, x.Entries, depth+1); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fmt.Errorf("number %v has no JSON form", x)
		}
		b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case string:
		writeJSONString(b, x)
	default:
		j, err := json.Marshal(x)
		if err != nil {
			return err
		}
		b.Write(j)
	}
	return nil
}

// ---------------------------------------------------------------- wire decoding

// resultElem is an element of a result value, serialized by the worker.
type resultElem struct {
	HTML   string `json:"html"`
	Source string `json:"source,omitempty"`
}

// decodeValue decodes a result value's wire JSON, preserving object key
// order and substituting elements from elems.
func decodeValue(data []byte, elems []resultElem) (Value, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeNext(dec, elems, 0)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func decodeNext(dec *json.Decoder, elems []resultElem, depth int) (Value, error) {
	if depth > 4*maxDepth {
		return nil, errors.New("result nests too deeply")
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case nil, bool, string:
		return t, nil
	case json.Number:
		return decodeNumber(t)
	case json.Delim:
		switch t {
		case '[':
			out := []Value{}
			for dec.More() {
				v, err := decodeNext(dec, elems, depth+1)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return out, nil
		case '{':
			d := NewDict()
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k, _ := kt.(string)
				v, err := decodeNext(dec, elems, depth+1)
				if err != nil {
					return nil, err
				}
				d.Set(k, v)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return untag(d, elems)
		}
	}
	return nil, fmt.Errorf("unexpected token %v", tok)
}

func decodeNumber(n json.Number) (Value, error) {
	if i, err := n.Int64(); err == nil && i <= maxSafeInt && i >= -maxSafeInt {
		return i, nil
	}
	f, err := n.Float64()
	if err != nil {
		return nil, err
	}
	if f == math.Trunc(f) && math.Abs(f) <= maxSafeInt {
		return int64(f), nil
	}
	return f, nil
}

// untag turns tagged objects of the result encoding back into values.
func untag(d *Dict, elems []resultElem) (Value, error) {
	tv, ok := d.Get("$type")
	if !ok {
		return d, nil
	}
	tag, _ := tv.(string)
	switch tag {
	case "element":
		iv, _ := d.Get("$i")
		i, ok := iv.(int64)
		if !ok || i < 0 || int(i) >= len(elems) {
			return nil, errors.New("bad element reference in result")
		}
		return &Element{HTML: elems[i].HTML, Source: elems[i].Source}, nil
	case "instance":
		it, _ := d.Get("$itemtype")
		inst := &Instance{Type: fmt.Sprint(it)}
		if p, ok := d.Get("$props"); ok {
			if pd, ok := p.(*Dict); ok {
				inst.Props = pd
			}
		}
		if inst.Props == nil {
			inst.Props = NewDict()
		}
		if en, ok := d.Get("$entries"); ok {
			if ed, ok := en.(*Dict); ok {
				inst.Entries = ed
			}
		}
		return inst, nil
	case "dict":
		out := NewDict()
		ev, _ := d.Get("$entries")
		list, _ := ev.([]Value)
		for _, e := range list {
			pair, _ := e.([]Value)
			if len(pair) != 2 {
				return nil, errors.New("bad dict entry in result")
			}
			k, _ := pair[0].(string)
			out.Set(k, pair[1])
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown tagged value %q in result", tag)
}
