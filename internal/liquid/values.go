package liquid

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/osteele/liquid/values"
)

// The Liquid value model (spec R-LIQ-50). Templates see:
//
//	nil, bool, int (int64 and the other Go integer kinds are accepted),
//	float64, string, SafeString, []any, *Hash (map[string]any is accepted),
//	*Item, *Request and values.Range (from `(a..b)` literals).
//
// Everything in this file is shared by the filters, the tags and the
// output writer, so that coercion is the same wherever a value is used.

// undefinedValue is the library's (unexported) "undefined" Value, which
// renders as nil and makes values.IsUndefined true.
var undefinedValue = values.ValueOf(map[string]any{}).PropertyValue(values.ValueOf("x"))

// SafeString is a string that escape, escape_once or xml_escape produced
// (R-LIQ-91). Output never escapes anything, safe or not (R-LIQ-90); the mark
// only makes `x | escape | escape` escape once. Filters that build a new
// string return a plain string, which drops the mark.
//
// SafeString implements values.Value so the library's property, contains and
// comparison paths treat it as the string it is.
type SafeString string

// String returns the string.
func (s SafeString) String() string { return string(s) }

// Interface implements values.Value.
func (s SafeString) Interface() any { return s }

// Int implements values.Value.
func (s SafeString) Int() int { panic(values.TypeError("can't convert string to int")) }

// Equal implements values.Value.
func (s SafeString) Equal(o values.Value) bool { return looseEqual(string(s), o.Interface()) }

// Less implements values.Value.
func (s SafeString) Less(o values.Value) bool {
	c, ok := looseCompare(string(s), o.Interface())
	return ok && c < 0
}

// Contains implements values.Value (substring test).
func (s SafeString) Contains(o values.Value) bool {
	return strings.Contains(string(s), toString(o.Interface()))
}

// IndexValue implements values.Value.
func (s SafeString) IndexValue(values.Value) values.Value { return undefinedValue }

// PropertyValue implements values.Value: only `size` (code points).
func (s SafeString) PropertyValue(k values.Value) values.Value {
	if k.Interface() == "size" {
		return values.ValueOf(utf8.RuneCountInString(string(s)))
	}
	return undefinedValue
}

// Test implements values.Value: every string is truthy.
func (s SafeString) Test() bool { return true }

// emptyLiteral stands in for Liquid's `blank` and `empty` literals, which the
// library's grammar lacks. The engine binds `blank` and `empty` to these
// values; comparisons are rewritten to the pl_eq/pl_ne filters, which ask
// the literal whether the other operand matches it, on either side
// (R-LIQ-58).
type emptyLiteral struct{ blank bool }

func (e emptyLiteral) Interface() any                          { return e }
func (e emptyLiteral) String() string                          { return "" }
func (e emptyLiteral) Int() int                                { panic(values.TypeError("blank is not a number")) }
func (e emptyLiteral) Less(values.Value) bool                  { return false }
func (e emptyLiteral) Contains(values.Value) bool              { return false }
func (e emptyLiteral) IndexValue(values.Value) values.Value    { return undefinedValue }
func (e emptyLiteral) PropertyValue(values.Value) values.Value { return undefinedValue }
func (e emptyLiteral) Test() bool                              { return true }
func (e emptyLiteral) Equal(o values.Value) bool               { return e.matches(o.Interface()) }

// matches reports whether v equals the literal: `empty` is "", [] and {};
// `blank` adds nil, false and whitespace-only strings.
func (e emptyLiteral) matches(v any) bool {
	v = unwrapSafe(v)
	if o, ok := v.(emptyLiteral); ok {
		return o.blank == e.blank || e.blank
	}
	if e.blank {
		switch x := v.(type) {
		case nil:
			return true
		case bool:
			return !x
		case string:
			return strings.TrimSpace(x) == ""
		}
	}
	switch x := v.(type) {
	case string:
		return x == ""
	case *Hash:
		return x.Len() == 0
	case map[string]any:
		return len(x) == 0
	case values.Range:
		return x.Len() <= 0
	}
	if isList(v) {
		return listLen(v) == 0
	}
	return false
}

// unwrapSafe turns a SafeString back into a plain string.
func unwrapSafe(v any) any {
	if s, ok := v.(SafeString); ok {
		return string(s)
	}
	return v
}

// asString reports whether v is a (plain or safe) string.
func asString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case SafeString:
		return string(x), true
	}
	return "", false
}

// toString is the string coercion every string filter applies to its input
// (R-LIQ-101) and the way `{{ v }}` renders (R-LIQ-56): nil is "", numbers
// render Ruby-style, arrays concatenate their elements, items render their
// trimmed text, hashes render as compact JSON and ranges as `a..b`.
func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case SafeString:
		return string(x)
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return formatFloat(x)
	case float32:
		return formatFloat(float64(x))
	case []byte:
		return string(x)
	case *Item:
		return x.String()
	case *Hash, *Request, map[string]any:
		s, _ := encodeJSON(x, 0)
		return s
	case values.Range:
		return strconv.Itoa(x.Index(0).(int)) + ".." + strconv.Itoa(x.Index(x.Len()-1).(int))
	case emptyLiteral:
		return ""
	case time.Time:
		return x.UTC().Format("2006-01-02 15:04:05 -0700")
	case []any:
		var b strings.Builder
		for _, e := range x {
			b.WriteString(toString(e))
		}
		return b.String()
	}
	if i, _, isInt, ok := goNumber(v); ok && isInt {
		return strconv.FormatInt(i, 10)
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		var b strings.Builder
		for i := range rv.Len() {
			b.WriteString(toString(rv.Index(i).Interface()))
		}
		return b.String()
	case reflect.Map:
		s, _ := encodeJSON(v, 0)
		return s
	}
	if s, ok := v.(fmt.Stringer); ok {
		return s.String()
	}
	return fmt.Sprint(v)
}

// formatFloat renders floats Ruby-style (R-LIQ-56): an integral value within
// ±1e16 keeps one decimal (`100.0`, the Expression-Binding docs' `Sum:
// 100.0`); anything else is the shortest round-trip decimal without an
// exponent.
func formatFloat(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == math.Trunc(f) && math.Abs(f) < 1e16:
		return strconv.FormatFloat(f, 'f', 1, 64)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// mkInt returns an integer result as the library's native int (literals,
// forloop.index and array indexes are all int), falling back to int64 only
// where int is narrower.
func mkInt(i int64) any {
	if i >= math.MinInt && i <= math.MaxInt {
		return int(i)
	}
	return i
}

// goNumber reports whether v is a Go number (not a numeric string).
func goNumber(v any) (i int64, f float64, isInt bool, ok bool) {
	switch x := v.(type) {
	case int:
		return int64(x), float64(x), true, true
	case int64:
		return x, float64(x), true, true
	case float64:
		return 0, x, false, true
	case int32:
		return int64(x), float64(x), true, true
	case int16:
		return int64(x), float64(x), true, true
	case int8:
		return int64(x), float64(x), true, true
	case uint8:
		return int64(x), float64(x), true, true
	case uint16:
		return int64(x), float64(x), true, true
	case uint32:
		return int64(x), float64(x), true, true
	case uint:
		if uint64(x) > math.MaxInt64 {
			return 0, float64(x), false, true
		}
		return int64(x), float64(x), true, true
	case uint64:
		if x > math.MaxInt64 {
			return 0, float64(x), false, true
		}
		return int64(x), float64(x), true, true
	case float32:
		return 0, float64(x), false, true
	}
	return 0, 0, false, false
}

// number is the number filters' input coercion (R-LIQ-101): Go numbers, and
// numeric strings (integer syntax gives an integer, anything else a float).
// ok is false for everything else, which the filters treat as 0.
func number(v any) (i int64, f float64, isInt bool, ok bool) {
	if i, f, isInt, ok = goNumber(v); ok {
		return
	}
	s, isStr := asString(v)
	if !isStr {
		return 0, 0, false, false
	}
	s = strings.TrimSpace(s)
	if s == "" || !looksNumeric(s) {
		return 0, 0, false, false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, float64(n), true, true
	}
	if x, err := strconv.ParseFloat(s, 64); err == nil && !math.IsNaN(x) && !math.IsInf(x, 0) {
		return 0, x, false, true
	}
	return 0, 0, false, false
}

// looksNumeric is a cheap pre-check that avoids strconv's error allocations
// for strings that are obviously not numbers.
func looksNumeric(s string) bool {
	c := s[0]
	if c == '-' || c == '+' || c == '.' {
		if len(s) == 1 {
			return false
		}
		c = s[1]
	}
	if c < '0' || c > '9' {
		return c == '.'
	}
	for i := 1; i < len(s); i++ {
		switch d := s[i]; {
		case d >= '0' && d <= '9', d == '.', d == 'e', d == 'E':
		case (d == '+' || d == '-') && (s[i-1] == 'e' || s[i-1] == 'E'):
		default:
			return false
		}
	}
	return true
}

// toArray is the array filters' input coercion (R-LIQ-101): nil is [], a
// list is itself, a range is its elements, anything else is [value].
// Callers that may receive a range should use (*fcall).list, which charges
// the budget before materializing it.
func toArray(v any) []any {
	switch x := v.(type) {
	case nil:
		return []any{}
	case []any:
		return x
	case values.Range:
		if x.Len() <= 0 {
			return []any{}
		}
		return x.AsArray()
	case string, SafeString, []byte:
		return []any{v}
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = rv.Index(i).Interface()
		}
		return out
	}
	return []any{v}
}

// isList reports whether v is an array or a range.
func isList(v any) bool {
	switch v.(type) {
	case nil, string, SafeString, []byte:
		return false
	case []any, values.Range:
		return true
	}
	k := reflect.ValueOf(v).Kind()
	return k == reflect.Slice || k == reflect.Array
}

// listLen is the length of a list (0 for anything else), without
// materializing ranges.
func listLen(v any) int {
	switch x := v.(type) {
	case []any:
		return len(x)
	case values.Range:
		return max(x.Len(), 0)
	}
	if isList(v) {
		return reflect.ValueOf(v).Len()
	}
	return 0
}

// truthy is Liquid truthiness (R-LIQ-57): only nil and false are falsy.
func truthy(v any) bool { return v != nil && v != false }

// isObject reports whether v has named properties: items, hashes and the
// request. Field filters never match anything else (R-LIQ-146).
func isObject(v any) bool {
	switch v.(type) {
	case *Item, *Hash, *Request, map[string]any:
		return true
	case nil, string, SafeString, bool:
		return false
	}
	return reflect.ValueOf(v).Kind() == reflect.Map
}

// prop reads property key the way `v.key` does, for the field filters.
func prop(v any, key string) (any, bool) {
	switch x := v.(type) {
	case *Item:
		return x.get(key)
	case *Hash:
		return x.Get(key)
	case *Request:
		return x.get(key)
	case map[string]any:
		e, ok := x[key]
		return e, ok
	}
	if !isObject(v) {
		return nil, false
	}
	pv := values.ValueOf(v).PropertyValue(values.ValueOf(key))
	if values.IsUndefined(pv) {
		return nil, false
	}
	return pv.Interface(), true
}

// fieldValue is prop for sort, sort_natural, group_by and sum: a
// multi-valued item property counts as its first value (R-LIQ-155).
func fieldValue(v any, key string) (any, bool) {
	x, ok := prop(v, key)
	if _, isItem := v.(*Item); isItem {
		if a, isArr := x.([]any); isArr && len(a) > 0 {
			return a[0], ok
		}
	}
	return x, ok
}

// sizeOf is `size` for filters and the `.size` property (R-LIQ-55, R-LIQ-121):
// strings count code points, lists their elements, hashes and items their
// `size` member if they have one and otherwise their keys; anything else is 0.
func sizeOf(v any) any {
	switch x := v.(type) {
	case nil:
		return 0
	case string:
		return utf8.RuneCountInString(x)
	case SafeString:
		return utf8.RuneCountInString(string(x))
	case *Item:
		if s, ok := x.get("size"); ok {
			return s
		}
		return 0
	case *Hash:
		if s, ok := x.Get("size"); ok {
			return s
		}
		return x.Len()
	case *Request:
		return x.h.Len()
	case map[string]any:
		if s, ok := x["size"]; ok {
			return s
		}
		return len(x)
	}
	if isList(v) {
		return listLen(v)
	}
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Map {
		return rv.Len()
	}
	return 0
}
