package liquid

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"sort"
	"strings"

	"github.com/osteele/liquid/values"
)

// Filters.
//
// pagelike registers the whole PageLove filter set itself (R-LIQ-100: the
// documented filters plus Shopify's base64 filters) rather than starting
// from the library's standard filters: string, number and array coercion
// must follow R-LIQ-101 for every filter, and most stock filters differ
// from PageLove somewhere (decision 0002 lists 30 overrides). Every filter
// is charged to the budget: one unit per call, plus one per element for the
// filters that walk a list (R-LIQ-211).

// fcall is one filter invocation.
type fcall struct {
	st   *renderState
	name string
	in   any
	args []any          // positional arguments
	kw   map[string]any // keyword arguments
}

type filterSpec struct {
	min, max int      // positional arity; max < 0 means unlimited
	kwargs   []string // accepted keyword argument names
	walks    bool     // charges one unit per element of a list input
	fn       func(c *fcall) (any, error)
}

// filterError is a filter error with a message meant for authors.
func filterError(format string, a ...any) error { return fmt.Errorf(format, a...) }

func (inst *instance) addFilter(name string, spec filterSpec) {
	inst.cfg.AddFilter(name, func(in any, args ...any) (any, error) {
		c := &fcall{st: inst.st, name: name, in: in}
		if len(spec.kwargs) > 0 && len(args) > 0 {
			if m, ok := args[len(args)-1].(map[string]any); ok {
				c.kw, args = m, args[:len(args)-1]
				// The preprocessor writes `f: k: v` as `f: nil, k: v`.
				if spec.max == 0 && len(args) == 1 && args[0] == nil {
					args = nil
				}
			}
		}
		c.args = args
		for k := range c.kw {
			if !contains(spec.kwargs, k) {
				return nil, filterError("unknown keyword argument %q", k)
			}
		}
		switch {
		case len(args) < spec.min:
			return nil, filterError("missing argument (%d given, %d required)", len(args), spec.min)
		case spec.max >= 0 && len(args) > spec.max:
			return nil, filterError("too many arguments (%d given, at most %d)", len(args), spec.max)
		}
		cost := int64(1)
		if spec.walks {
			cost += int64(listLen(in))
		}
		if err := c.st.charge(cost); err != nil {
			return nil, err
		}
		return spec.fn(c)
	})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// has reports whether positional argument i was given.
func (c *fcall) has(i int) bool { return i < len(c.args) }

// str returns argument i coerced to a string, or def when absent.
func (c *fcall) str(i int, def string) string {
	if !c.has(i) {
		return def
	}
	return toString(c.args[i])
}

// integer returns argument i as an integer, or def when absent. A value
// that is not a number is a filter error (R-LIQ-125).
func (c *fcall) integer(i int, def int64) (int64, error) {
	if !c.has(i) || c.args[i] == nil {
		return def, nil
	}
	return c.intValue(c.args[i], "argument")
}

func (c *fcall) intValue(v any, what string) (int64, error) {
	n, f, isInt, ok := number(v)
	switch {
	case !ok:
		return 0, filterError("%s must be a number, got %q", what, clipString(toString(v), 40))
	case isInt:
		return n, nil
	case math.IsNaN(f):
		return 0, filterError("%s must be a number", what)
	}
	return satTrunc(f), nil
}

// list coerces the input to an array (R-LIQ-101), charging a range's
// materialization to the memory axis first.
func (c *fcall) list() ([]any, error) { return c.listOf(c.in) }

func (c *fcall) listOf(v any) ([]any, error) {
	if r, ok := v.(values.Range); ok && r.Len() > 0 {
		if err := c.st.chargeMemory(16 * int64(r.Len())); err != nil {
			return nil, err
		}
	}
	return toArray(v), nil
}

// addFilters registers every filter on an instance.
func (inst *instance) addFilters() {
	for name, spec := range stringFilters() {
		inst.addFilter(name, spec)
	}
	for name, spec := range numberFilters() {
		inst.addFilter(name, spec)
	}
	for name, spec := range arrayFilters() {
		inst.addFilter(name, spec)
	}
	for name, spec := range dateFilters() {
		inst.addFilter(name, spec)
	}
	for name, spec := range cryptoFilters() {
		inst.addFilter(name, spec)
	}
	for name, spec := range internalFilterSpecs(inst) {
		inst.addFilter(name, spec)
	}
	inst.addExpFilters()
}

// internalFilterSpecs are the filters the preprocessor inserts.
func internalFilterSpecs(inst *instance) map[string]filterSpec {
	cmp := func(test func(a, b any) bool) filterSpec {
		return filterSpec{min: 1, max: 1, fn: func(c *fcall) (any, error) { return test(c.in, c.args[0]), nil }}
	}
	return map[string]filterSpec{
		"pl_eq": cmp(looseEqual),
		"pl_ne": cmp(func(a, b any) bool { return !looseEqual(a, b) }),
		"pl_lt": cmp(func(a, b any) bool { c, ok := looseCompare(a, b); return ok && c < 0 }),
		"pl_gt": cmp(func(a, b any) bool { c, ok := looseCompare(a, b); return ok && c > 0 }),
		"pl_le": cmp(func(a, b any) bool { c, ok := looseCompare(a, b); return ok && c <= 0 }),
		"pl_ge": cmp(func(a, b any) bool { c, ok := looseCompare(a, b); return ok && c >= 0 }),
		// pl_contains is `a contains b` (R-LIQ-74).
		"pl_contains": {min: 1, max: 1, walks: true, fn: func(c *fcall) (any, error) {
			return containsValue(c.in, c.args[0]), nil
		}},
		// pl_int is a range bound: an integer, numeric strings included.
		"pl_int": {fn: func(c *fcall) (any, error) {
			if c.in == nil {
				return 0, nil
			}
			n, err := c.intValue(c.in, "range bound")
			if err != nil {
				return nil, err
			}
			return int(max(min(n, math.MaxInt32), math.MinInt32)), nil
		}},
		// pl_index is a computed index: integers of any kind become int,
		// the only kind the library indexes arrays with.
		"pl_index": {fn: func(c *fcall) (any, error) {
			if i, _, isInt, ok := goNumber(c.in); ok && isInt {
				return mkInt(i), nil
			}
			return unwrapSafe(c.in), nil
		}},
		// pl_pred rewrites an expression filter's predicate string.
		"pl_pred": {fn: func(c *fcall) (any, error) { return inst.eng.predicate(toString(c.in)) }},
	}
}

// containsValue is `contains`: a substring test when the left side is a
// string (the right side coerced to a string), strict membership in an
// array or range, and false for anything else — hashes and items included.
func containsValue(in, v any) bool {
	if s, ok := asString(in); ok {
		return v != nil && strings.Contains(s, toString(v))
	}
	if r, ok := in.(values.Range); ok {
		i, _, isInt, ok := goNumber(v)
		return ok && isInt && i >= int64(r.Index(0).(int)) && i < int64(r.Index(0).(int)+r.Len())
	}
	if !isList(in) {
		return false
	}
	for _, e := range toArray(in) {
		if strictEqual(e, v) {
			return true
		}
	}
	return false
}

// satTrunc truncates toward zero, saturating at the int64 range; NaN is 0.
func satTrunc(f float64) int64 {
	switch {
	case math.IsNaN(f):
		return 0
	case f >= math.MaxInt64:
		return math.MaxInt64
	case f <= math.MinInt64:
		return math.MinInt64
	}
	return int64(f)
}

// arith implements plus, minus, times, divided_by and modulo (R-LIQ-130 …
// R-LIQ-132): integer when both operands are integers after coercion
// (numeric strings included), float otherwise; integer overflow is an
// error; integer division floors; modulo takes the divisor's sign; a zero
// divisor returns the input unchanged.
func arith(a, b any, op byte) (any, error) {
	ai, af, aInt, aok := number(a)
	bi, bf, bInt, bok := number(b)
	if !aok {
		ai, af, aInt = 0, 0, true
	}
	if !bok {
		bi, bf, bInt = 0, 0, true
	}
	if (op == '/' || op == '%') && ((bInt && bi == 0) || (!bInt && bf == 0)) {
		return a, nil
	}
	if aInt && bInt {
		overflow := errors.New("integer overflow")
		switch op {
		case '+':
			s := ai + bi
			if (s > ai) != (bi > 0) {
				return nil, overflow
			}
			return mkInt(s), nil
		case '-':
			d := ai - bi
			if (d < ai) != (bi > 0) {
				return nil, overflow
			}
			return mkInt(d), nil
		case '*':
			if ai == 0 || bi == 0 {
				return 0, nil
			}
			hi, lo := bits.Mul64(uint64(absInt(ai)), uint64(absInt(bi)))
			neg := (ai < 0) != (bi < 0)
			if hi != 0 || lo > math.MaxInt64+boolToUint(neg) || absInt(ai) < 0 || absInt(bi) < 0 {
				return nil, overflow
			}
			if neg {
				return mkInt(-int64(lo)), nil
			}
			return mkInt(int64(lo)), nil
		case '/':
			if ai == math.MinInt64 && bi == -1 {
				return nil, overflow
			}
			q := ai / bi
			if ai%bi != 0 && (ai < 0) != (bi < 0) {
				q--
			}
			return mkInt(q), nil
		case '%':
			if bi == -1 {
				return 0, nil
			}
			m := ai % bi
			if m != 0 && (m < 0) != (bi < 0) {
				m += bi
			}
			return mkInt(m), nil
		}
	}
	switch op {
	case '+':
		return af + bf, nil
	case '-':
		return af - bf, nil
	case '*':
		return af * bf, nil
	case '/':
		return af / bf, nil
	}
	return af - bf*math.Floor(af/bf), nil
}

func absInt(i int64) int64 {
	if i < 0 {
		return -i
	}
	return i
}

func boolToUint(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// sortValues implements sort (R-LIQ-156) and sort_natural (R-LIQ-143):
// stable; values that are missing (nil, or no such field) go last; sort
// groups kinds — numbers and numeric strings (numerically), then other
// strings by code point, then booleans, then everything else in its
// original order; sort_natural compares case-folded string forms.
func sortValues(in []any, key string, natural bool) []any {
	type row struct {
		v    any
		has  bool
		rank int
		f    float64
		s    string
		b    bool
	}
	rows := make([]row, len(in))
	for i, v := range in {
		k, has := v, v != nil
		if key != "" {
			k, has = fieldValue(v, key)
			has = has && k != nil
		}
		r := row{v: v, has: has}
		if has {
			k = unwrapSafe(k)
			switch {
			case natural:
				r.s = strings.ToLower(toString(k))
			default:
				if _, f, _, ok := number(k); ok {
					r.rank, r.f = 0, f
				} else if s, ok := k.(string); ok {
					r.rank, r.s = 1, s
				} else if b, ok := k.(bool); ok {
					r.rank, r.b = 2, b
				} else {
					r.rank = 3
				}
			}
		}
		rows[i] = r
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := &rows[i], &rows[j]
		switch {
		case a.has != b.has:
			return a.has
		case !a.has:
			return false
		case natural:
			return a.s < b.s
		case a.rank != b.rank:
			return a.rank < b.rank
		}
		switch a.rank {
		case 0:
			return a.f < b.f
		case 1:
			return a.s < b.s
		case 2:
			return !a.b && b.b
		}
		return false
	})
	out := make([]any, len(rows))
	for i, r := range rows {
		out[i] = r.v
	}
	return out
}

// sortCost is the extra work of sorting n elements.
func sortCost(n int) int64 {
	if n < 2 {
		return 0
	}
	return int64(n) * int64(bits.Len(uint(n)))
}

// groupBy groups items by key in first-seen order (R-LIQ-148, R-LIQ-161).
// Keys are compared type-strictly; a missing field is the key nil.
func groupBy(items []any, key func(any) (any, error)) (any, error) {
	type group struct {
		name  any
		items []any
	}
	var order []*group
	index := map[string]*group{}
	for _, e := range items {
		k, err := key(e)
		if err != nil {
			return nil, err
		}
		id := identityKey(k)
		g, ok := index[id]
		if !ok {
			g = &group{name: k}
			index[id] = g
			order = append(order, g)
		}
		g.items = append(g.items, e)
	}
	out := make([]any, len(order))
	for i, g := range order {
		out[i] = NewHash().Set("name", g.name).Set("items", g.items)
	}
	return out, nil
}

// identityKey is a type-strict key for uniq and group_by: 1 and "1" differ,
// items are keyed by element.
func identityKey(v any) string {
	v = unwrapSafe(v)
	switch x := v.(type) {
	case nil:
		return "nil"
	case string:
		return "s:" + x
	case bool:
		return fmt.Sprint("b:", x)
	case *Item:
		return fmt.Sprintf("i:%p", x.Node)
	case *Request:
		return fmt.Sprintf("r:%p", x)
	}
	if i, f, isInt, ok := goNumber(v); ok {
		if isInt {
			return fmt.Sprint("n:", i)
		}
		return fmt.Sprint("n:", f)
	}
	s, err := encodeJSON(v, 0)
	if err != nil {
		return fmt.Sprintf("x:%p", &v)
	}
	return fmt.Sprintf("%T:%s", v, s)
}
