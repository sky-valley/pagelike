package liquid

import (
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/osteele/liquid/values"
)

// Comparisons (R-LIQ-74, R-LIQ-76).
//
// The library compiles `a < b` into a.Less(b) (and `a > b` into b.Less(a)),
// so a literal on the left decides, and it never compares a numeric string
// with a number: `"34" >= 18` is false (decision 0002, gap G1). Microdata
// values are always strings, so the preprocessor rewrites every comparison
// into one of the pl_eq … pl_ge filters, which call the functions below.

// decimalNumber parses the strict decimal form R-LIQ-76 coerces: an
// optional sign, digits, and an optional fraction. No spaces, no exponent.
func decimalNumber(s string) (i int64, f float64, isInt bool, ok bool) {
	body := s
	if body != "" && (body[0] == '+' || body[0] == '-') {
		body = body[1:]
	}
	intPart, frac, hasDot := strings.Cut(body, ".")
	if intPart == "" || (hasDot && frac == "") || !allDigits(intPart) || (hasDot && !allDigits(frac)) {
		return 0, 0, false, false
	}
	if !hasDot {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n, float64(n), true, true
		}
	}
	x, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, 0, false, false
	}
	return 0, x, false, true
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// numericOperand returns a comparison operand as a number: a Go number, or,
// when other is a Go number, a string in strict decimal form.
func numericOperand(v, other any) (i int64, f float64, isInt bool, ok bool) {
	if i, f, isInt, ok = goNumber(v); ok {
		return
	}
	if _, _, _, otherNum := goNumber(other); !otherNum {
		return 0, 0, false, false
	}
	if s, isStr := asString(v); isStr {
		return decimalNumber(s)
	}
	return 0, 0, false, false
}

// compareNumbers orders two numbers exactly when both are integers.
func compareNumbers(ai int64, af float64, aInt bool, bi int64, bf float64, bInt bool) int {
	if aInt && bInt {
		switch {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		}
		return 0
	}
	switch {
	case af < bf:
		return -1
	case af > bf:
		return 1
	}
	return 0
}

// looseCompare orders a and b for <, >, <= and >=. ok is false when the two
// are not comparable (different kinds, nil, items, …): every ordering
// comparison between them is then false.
func looseCompare(a, b any) (c int, ok bool) {
	a, b = unwrapSafe(a), unwrapSafe(b)
	ai, af, aInt, aNum := numericOperand(a, b)
	bi, bf, bInt, bNum := numericOperand(b, a)
	switch {
	case aNum && bNum:
		if math.IsNaN(af) || math.IsNaN(bf) {
			return 0, false
		}
		return compareNumbers(ai, af, aInt, bi, bf, bInt), true
	case aNum || bNum:
		return 0, false
	}
	as, aStr := a.(string)
	bs, bStr := b.(string)
	if aStr && bStr {
		return strings.Compare(as, bs), true
	}
	return 0, false
}

// looseEqual is `==` (R-LIQ-76): blank/empty on either side, numbers
// against numbers or strict decimal strings, booleans against "true" and
// "false", and otherwise strict equality.
func looseEqual(a, b any) bool {
	a, b = unwrapSafe(a), unwrapSafe(b)
	if e, ok := a.(emptyLiteral); ok {
		return e.matches(b)
	}
	if e, ok := b.(emptyLiteral); ok {
		return e.matches(a)
	}
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ai, af, aInt, aNum := numericOperand(a, b)
	bi, bf, bInt, bNum := numericOperand(b, a)
	switch {
	case aNum && bNum:
		return compareNumbers(ai, af, aInt, bi, bf, bInt) == 0 && !math.IsNaN(af)
	case aNum || bNum:
		return false
	}
	if ab, ok := a.(bool); ok {
		if s, isStr := b.(string); isStr && (s == "true" || s == "false") {
			return ab == (s == "true")
		}
	}
	if bb, ok := b.(bool); ok {
		if s, isStr := a.(string); isStr && (s == "true" || s == "false") {
			return bb == (s == "true")
		}
	}
	return strictEqual(a, b)
}

// strictEqual is type-strict equality (uniq, and the elements of arrays and
// hashes): 1 and "1" differ, 1 and 1.0 are equal, items are equal when they
// wrap the same element, hashes and arrays compare member by member.
func strictEqual(a, b any) bool {
	a, b = unwrapSafe(a), unwrapSafe(b)
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if ai, af, aInt, aNum := goNumber(a); aNum {
		bi, bf, bInt, bNum := goNumber(b)
		return bNum && compareNumbers(ai, af, aInt, bi, bf, bInt) == 0 && !math.IsNaN(af)
	}
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case *Item:
		y, ok := b.(*Item)
		return ok && x.Node == y.Node
	case *Request:
		return a == b
	case emptyLiteral:
		y, ok := b.(emptyLiteral)
		return ok && x == y
	case *Hash, map[string]any:
		return hashEqual(a, b)
	}
	if isList(a) {
		if !isList(b) {
			return false
		}
		xa, xb := toArray(a), toArray(b)
		if len(xa) != len(xb) {
			return false
		}
		for i := range xa {
			if !strictEqual(xa[i], xb[i]) {
				return false
			}
		}
		return true
	}
	if ra, rb := reflect.ValueOf(a), reflect.ValueOf(b); ra.Type() == rb.Type() && ra.Type().Comparable() {
		return a == b
	}
	return values.Equal(a, b)
}

// hashEqual compares two hashes (ordered or not) key by key.
func hashEqual(a, b any) bool {
	ka, ga, okA := hashView(a)
	kb, gb, okB := hashView(b)
	if !okA || !okB || len(ka) != len(kb) {
		return false
	}
	for _, k := range ka {
		va, _ := ga(k)
		vb, ok := gb(k)
		if !ok || !strictEqual(va, vb) {
			return false
		}
	}
	return true
}

// hashView returns the keys of a hash and a getter; ok is false when v is
// not a hash.
func hashView(v any) (keys []string, get func(string) (any, bool), ok bool) {
	switch x := v.(type) {
	case *Hash:
		return x.Keys(), x.Get, true
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		return keys, func(k string) (any, bool) { e, ok := x[k]; return e, ok }, true
	}
	return nil, nil, false
}
