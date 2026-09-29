package sessel

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// callMethod dispatches recv.name(args) on the runtime type of recv
// (R-SESSEL-76). Arguments are already evaluated.
func (ev *evaluator) callMethod(recv Value, name string, args []Value) (Value, error) {
	var (
		v   Value
		ok  bool
		err error
	)
	switch r := recv.(type) {
	case nil:
		return nil, nil
	case int64, float64:
		v, ok, err = ev.numberMethod(recv, name, args)
	case string:
		v, ok, err = ev.stringMethod(r, name, args)
	case List:
		v, ok, err = ev.listMethod(r, name, args)
	case *Dict:
		v, ok, err = ev.dictMethod(r, name, args)
	case *Element:
		if r.Class != nil {
			if v, ok, err = ev.instanceMethod(r, name, args); ok || err != nil {
				return v, err
			}
		}
		v, ok, err = ev.elementMethod(r, name, args)
		if !ok && err == nil && r.Class != nil {
			v, ok, err = ev.doesNotUnderstand(r, name, args)
		}
	case *SelectorValue:
		v, ok, err = ev.selectorMethod(r, name, args)
	case *TypeNS:
		v, ok, err = ev.namespaceMethod(r, name, args)
	case Class:
		v, ok, err = ev.classMethod(r, name, args)
	case temporal:
		v, ok, err = ev.temporalMethod(r, name, args)
	case *Blob:
		switch name {
		case "metadata":
			if r.Meta == nil {
				return nil, nil
			}
			return r.Meta.Copy(), nil
		case "path":
			return r.Path, nil
		}
	}
	if ok || err != nil {
		return v, err
	}
	return ev.universalMethod(recv, name, args)
}

// universalMethod implements the conversions of R-SESSEL-94 and the
// first()/last() leniency of R-SESSEL-142.
func (ev *evaluator) universalMethod(recv Value, name string, args []Value) (Value, error) {
	switch name {
	case "String", "toString":
		if s, ok := recv.(string); ok {
			return s, nil
		}
		return TextOf(recv), nil
	case "Bool":
		return Truthy(recv), nil
	case "Number":
		switch x := recv.(type) {
		case int64, float64:
			return x, nil
		case string:
			return parseNumber(x), nil
		}
		return nil, nil
	case "Integer":
		switch x := recv.(type) {
		case int64:
			return x, nil
		case float64:
			return floatToInt(math.Trunc(x))
		case string:
			if !intRE.MatchString(x) {
				return nil, typeErr("%q is not an integer", x)
			}
			n, err := strconv.ParseInt(x, 10, 64)
			if err != nil {
				return nil, typeErr("%q is out of the Integer range", x)
			}
			return n, nil
		}
		return nil, typeErr("cannot convert %s to Integer", TypeName(recv))
	case "Float":
		switch x := recv.(type) {
		case int64:
			return float64(x), nil
		case float64:
			return x, nil
		case string:
			if !numRE.MatchString(x) {
				return nil, typeErr("%q is not a number", x)
			}
			f, err := strconv.ParseFloat(x, 64)
			if err != nil {
				return nil, typeErr("%q is not a number", x)
			}
			return f, nil
		}
		return nil, typeErr("cannot convert %s to Float", TypeName(recv))
	case "first", "last":
		if len(args) == 0 {
			return recv, nil
		}
	}
	return nil, typeErr("no method '%s' on %s", name, TypeName(recv))
}

var (
	intRE = regexp.MustCompile(`^-?[0-9]+$`)
	numRE = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)
)

// parseNumber is the smart parse of .Number(): Integer, Float or null.
func parseNumber(s string) Value {
	if !numRE.MatchString(s) {
		return nil
	}
	if intRE.MatchString(s) {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) {
		return nil
	}
	return f
}

func floatToInt(f float64) (Value, error) {
	if f >= 9.223372036854775807e18 || f < -9.223372036854775808e18 || math.IsNaN(f) {
		return nil, runtimeErr("integer overflow")
	}
	return int64(f), nil
}

func argCount(name string, args []Value, min, max int) error {
	if len(args) < min || len(args) > max {
		if min == max {
			return typeErr("%s() takes %d argument(s), got %d", name, min, len(args))
		}
		return typeErr("%s() takes %d to %d arguments, got %d", name, min, max, len(args))
	}
	return nil
}

func (ev *evaluator) numberMethod(recv Value, name string, args []Value) (Value, bool, error) {
	switch name {
	case "abs":
		switch x := recv.(type) {
		case int64:
			if x == math.MinInt64 {
				return nil, true, runtimeErr("integer overflow")
			}
			if x < 0 {
				return -x, true, nil
			}
			return x, true, nil
		case float64:
			return math.Abs(x), true, nil
		}
	case "floor", "ceil", "round":
		switch x := recv.(type) {
		case int64:
			return x, true, nil
		case float64:
			var f float64
			switch name {
			case "floor":
				f = math.Floor(x)
			case "ceil":
				f = math.Ceil(x)
			default:
				f = math.Round(x)
			}
			v, err := floatToInt(f)
			return v, true, err
		}
	}
	return nil, false, nil
}

var regexCache sync.Map

func compileRegex(p string) (*regexp.Regexp, error) {
	if r, ok := regexCache.Load(p); ok {
		return r.(*regexp.Regexp), nil
	}
	r, err := regexp.Compile(p)
	if err != nil {
		return nil, typeErr("invalid regular expression %q: %v", p, err)
	}
	regexCache.Store(p, r)
	return r, nil
}

func strArg(method string, args []Value, i int) (string, error) {
	if i >= len(args) {
		return "", typeErr("%s() is missing argument %d", method, i+1)
	}
	s, ok := args[i].(string)
	if !ok {
		return "", typeErr("%s() expects a String, not %s", method, TypeName(args[i]))
	}
	return s, nil
}

func intArg(method string, args []Value, i int) (int64, error) {
	if i >= len(args) {
		return 0, typeErr("%s() is missing argument %d", method, i+1)
	}
	n, ok := args[i].(int64)
	if !ok {
		return 0, typeErr("%s() expects an Integer, not %s", method, TypeName(args[i]))
	}
	return n, nil
}

// sliceBounds resolves [start, end) with negative indices and clamping
// (R-SESSEL-116/143).
func sliceBounds(method string, args []Value, n int) (int, int, error) {
	if err := argCount(method, args, 1, 2); err != nil {
		return 0, 0, err
	}
	norm := func(i int64) int {
		if i < 0 {
			i += int64(n)
		}
		if i < 0 {
			return 0
		}
		if i > int64(n) {
			return n
		}
		return int(i)
	}
	s, err := intArg(method, args, 0)
	if err != nil {
		return 0, 0, err
	}
	start, end := norm(s), n
	if len(args) == 2 && args[1] != nil {
		e, err := intArg(method, args, 1)
		if err != nil {
			return 0, 0, err
		}
		end = norm(e)
	}
	if start >= end {
		return 0, 0, nil
	}
	return start, end, nil
}

func (ev *evaluator) stringMethod(s string, name string, args []Value) (Value, bool, error) {
	switch name {
	case "contains", "startsWith", "endsWith":
		if err := argCount(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		a, err := strArg(name, args, 0)
		if err != nil {
			return nil, true, err
		}
		switch name {
		case "contains":
			return strings.Contains(s, a), true, nil
		case "startsWith":
			return strings.HasPrefix(s, a), true, nil
		}
		return strings.HasSuffix(s, a), true, nil
	case "matches":
		if err := argCount(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		p, err := strArg(name, args, 0)
		if err != nil {
			return nil, true, err
		}
		re, err := compileRegex(p)
		if err != nil {
			return nil, true, err
		}
		return re.MatchString(s), true, nil
	case "count":
		return int64(len([]rune(s))), true, nil
	case "isEmpty":
		return s == "", true, nil
	case "trim":
		return strings.TrimSpace(s), true, nil
	case "lower", "lowercase":
		return strings.ToLower(s), true, nil
	case "upper", "uppercase":
		return strings.ToUpper(s), true, nil
	case "replace":
		if err := argCount(name, args, 2, 2); err != nil {
			return nil, true, err
		}
		p, err := strArg(name, args, 0)
		if err != nil {
			return nil, true, err
		}
		r, err := strArg(name, args, 1)
		if err != nil {
			return nil, true, err
		}
		if p == "" {
			return s, true, nil
		}
		out := strings.ReplaceAll(s, p, r)
		return out, true, ev.r.alloc(len(out))
	case "split":
		if err := argCount(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		d, err := strArg(name, args, 0)
		if err != nil {
			return nil, true, err
		}
		var parts []string
		if d == "" {
			for _, r := range s {
				parts = append(parts, string(r))
			}
		} else {
			parts = strings.Split(s, d)
		}
		out := make(List, len(parts))
		for i, p := range parts {
			out[i] = p
		}
		return out, true, ev.r.alloc(16*len(out) + len(s))
	case "slice":
		rs := []rune(s)
		a, b, err := sliceBounds(name, args, len(rs))
		if err != nil {
			return nil, true, err
		}
		return string(rs[a:b]), true, nil
	case "sha256", "hmac_sha256", "bcrypt", "argon2":
		v, err := hashMethod(ev.r, s, name, args)
		return v, true, err
	case "slugify":
		return slugify(s), true, nil
	case "parseDateTime":
		return parseDateTime(s), true, nil
	}
	return nil, false, nil
}

func (ev *evaluator) lambdaArg(method string, args []Value, i int) (*Lambda, error) {
	if i >= len(args) {
		return nil, typeErr("%s() needs a lambda", method)
	}
	l, ok := args[i].(*Lambda)
	if !ok {
		return nil, typeErr("%s() needs a lambda, not %s", method, TypeName(args[i]))
	}
	return l, nil
}

// numericOf is the value extraction of .sum()/.min()/.max() (R-SESSEL-144).
func numericOf(v Value) (Value, bool) {
	switch x := v.(type) {
	case int64, float64:
		return x, true
	case string:
		n := parseNumber(x)
		return n, n != nil
	case *Element:
		if s, ok := elementValue(x.Node).(string); ok {
			n := parseNumber(s)
			return n, n != nil
		}
	}
	return nil, false
}

func (ev *evaluator) listMethod(l List, name string, args []Value) (Value, bool, error) {
	call := func(f *Lambda, el Value, i int) (Value, error) {
		return ev.callLambda(f, []Value{el, int64(i), l})
	}
	switch name {
	case "count":
		return int64(len(l)), true, nil
	case "isEmpty":
		return len(l) == 0, true, nil
	case "first":
		if len(l) == 0 {
			return nil, true, nil
		}
		return l[0], true, nil
	case "last":
		if len(l) == 0 {
			return nil, true, nil
		}
		return l[len(l)-1], true, nil
	case "at":
		if err := argCount(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		i, err := intArg(name, args, 0)
		if err != nil {
			return nil, true, err
		}
		return listAt(l, i), true, nil
	case "slice":
		a, b, err := sliceBounds(name, args, len(l))
		if err != nil {
			return nil, true, err
		}
		return append(List{}, l[a:b]...), true, nil
	case "sum":
		var isum int64
		var fsum float64
		isFloat := false
		for _, it := range l {
			n, ok := numericOf(it)
			if !ok {
				continue
			}
			switch x := n.(type) {
			case int64:
				if isFloat {
					fsum += float64(x)
					continue
				}
				s := isum + x
				if (s > isum) != (x > 0) {
					return nil, true, runtimeErr("integer overflow")
				}
				isum = s
			case float64:
				if !isFloat {
					isFloat, fsum = true, float64(isum)
				}
				fsum += x
			}
		}
		if isFloat {
			v, err := checkFloat(fsum)
			return v, true, err
		}
		return isum, true, nil
	case "min", "max":
		var best Value
		for _, it := range l {
			n, ok := numericOf(it)
			if !ok {
				continue
			}
			if best == nil {
				best = n
				continue
			}
			c, _ := compareValues(n, best)
			if (name == "min" && c < 0) || (name == "max" && c > 0) {
				best = n
			}
		}
		return best, true, nil
	case "filter", "reject", "find", "map", "each", "takeWhile", "dropWhile", "all", "any":
		f, err := ev.lambdaArg(name, args, 0)
		if err != nil {
			return nil, true, err
		}
		switch name {
		case "map":
			out := make(List, 0, len(l))
			for i, it := range l {
				v, err := call(f, it, i)
				if err != nil {
					return nil, true, err
				}
				out = append(out, v)
			}
			return out, true, ev.r.alloc(16 * len(out))
		case "each":
			for i, it := range l {
				if _, err := call(f, it, i); err != nil {
					return nil, true, err
				}
			}
			return l, true, nil
		case "find":
			for i, it := range l {
				v, err := call(f, it, i)
				if err != nil {
					return nil, true, err
				}
				if Truthy(v) {
					return it, true, nil
				}
			}
			return nil, true, nil
		case "all", "any":
			for i, it := range l {
				v, err := call(f, it, i)
				if err != nil {
					return nil, true, err
				}
				if name == "all" && !Truthy(v) {
					return false, true, nil
				}
				if name == "any" && Truthy(v) {
					return true, true, nil
				}
			}
			return name == "all", true, nil
		case "takeWhile", "dropWhile":
			for i, it := range l {
				v, err := call(f, it, i)
				if err != nil {
					return nil, true, err
				}
				if !Truthy(v) {
					if name == "takeWhile" {
						return append(List{}, l[:i]...), true, nil
					}
					return append(List{}, l[i:]...), true, nil
				}
			}
			if name == "takeWhile" {
				return append(List{}, l...), true, nil
			}
			return List{}, true, nil
		}
		out := List{}
		for i, it := range l {
			v, err := call(f, it, i)
			if err != nil {
				return nil, true, err
			}
			if Truthy(v) == (name == "filter") {
				out = append(out, it)
			}
		}
		return out, true, nil
	case "unique":
		seen := map[string]bool{}
		out := List{}
		for _, it := range l {
			k := TextOf(it)
			if !seen[k] {
				seen[k] = true
				out = append(out, it)
			}
		}
		return out, true, nil
	case "subset", "disjoint":
		if err := argCount(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		other, ok := args[0].(List)
		if !ok {
			return nil, true, typeErr("%s() needs a List, not %s", name, TypeName(args[0]))
		}
		set := map[string]bool{}
		for _, it := range other {
			set[TextOf(it)] = true
		}
		for _, it := range l {
			in := set[TextOf(it)]
			if name == "subset" && !in {
				return false, true, nil
			}
			if name == "disjoint" && in {
				return false, true, nil
			}
		}
		return true, true, nil
	case "contains":
		if err := argCount(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		want := TextOf(args[0])
		for _, it := range l {
			if TextOf(it) == want {
				return true, true, nil
			}
		}
		return false, true, nil
	case "sort":
		v, err := ev.sortList(l, args)
		return v, true, err
	case "reverse":
		out := make(List, len(l))
		for i, it := range l {
			out[len(l)-1-i] = it
		}
		return out, true, nil
	case "flatten":
		var out List
		var walk func(List)
		walk = func(x List) {
			for _, it := range x {
				if sub, ok := it.(List); ok {
					walk(sub)
				} else {
					out = append(out, it)
				}
			}
		}
		walk(l)
		if out == nil {
			out = List{}
		}
		return out, true, nil
	case "join":
		if err := argCount(name, args, 0, 1); err != nil {
			return nil, true, err
		}
		sep := ","
		if len(args) == 1 {
			s, err := strArg(name, args, 0)
			if err != nil {
				return nil, true, err
			}
			sep = s
		}
		parts := make([]string, len(l))
		for i, it := range l {
			parts[i] = TextOf(it)
		}
		out := strings.Join(parts, sep)
		return out, true, ev.r.alloc(len(out))
	case "reduce", "reduceRight":
		v, err := ev.reduce(l, name == "reduceRight", args)
		return v, true, err
	case "microdata":
		out := make(List, len(l))
		for i, it := range l {
			if el, ok := it.(*Element); ok {
				out[i] = ev.microdata(el)
			}
		}
		return out, true, nil
	}
	return nil, false, nil
}

func (ev *evaluator) reduce(l List, right bool, args []Value) (Value, error) {
	var cb *Lambda
	var acc Value
	seeded := false
	switch len(args) {
	case 1:
		f, ok := args[0].(*Lambda)
		if !ok {
			return nil, typeErr("reduce() needs a lambda")
		}
		cb = f
	case 2:
		if f, ok := args[1].(*Lambda); ok {
			cb, acc, seeded = f, args[0], true
		} else if f, ok := args[0].(*Lambda); ok {
			cb, acc, seeded = f, args[1], true
		} else {
			return nil, typeErr("reduce() needs a lambda")
		}
	default:
		return nil, typeErr("reduce() takes an initial value and a lambda")
	}
	items := l
	if right {
		items = make(List, len(l))
		for i, it := range l {
			items[len(l)-1-i] = it
		}
	}
	start := 0
	if !seeded {
		if len(items) == 0 {
			return nil, runtimeErr("reduce() of an empty list with no initial value")
		}
		acc, start = items[0], 1
	}
	for i := start; i < len(items); i++ {
		idx := i
		if right {
			idx = len(items) - 1 - i
		}
		v, err := ev.callLambda(cb, []Value{acc, items[i], int64(idx), l})
		if err != nil {
			return nil, err
		}
		acc = v
	}
	return acc, nil
}

// sortList implements .sort() (R-SESSEL-154): stable; key mode for a
// one-parameter lambda, comparator mode for two.
func (ev *evaluator) sortList(l List, args []Value) (Value, error) {
	out := append(List{}, l...)
	var firstErr error
	cmpKeys := func(a, b Value) int {
		if a == nil || b == nil {
			switch {
			case a == nil && b == nil:
				return 0
			case a == nil:
				return -1
			}
			return 1
		}
		c, ok := compareValues(a, b)
		if !ok && firstErr == nil {
			firstErr = typeErr("cannot sort %s and %s", TypeName(a), TypeName(b))
		}
		return c
	}
	if len(args) == 0 {
		sort.SliceStable(out, func(i, j int) bool { return cmpKeys(out[i], out[j]) < 0 })
		return out, firstErr
	}
	f, err := ev.lambdaArg("sort", args, 0)
	if err != nil {
		return nil, err
	}
	if f.Arity() >= 2 {
		sort.SliceStable(out, func(i, j int) bool {
			if firstErr != nil {
				return false
			}
			v, err := ev.callLambda(f, []Value{out[i], out[j]})
			if err != nil {
				firstErr = err
				return false
			}
			switch x := v.(type) {
			case nil:
				return false
			case int64:
				return x < 0
			case float64:
				return x < 0
			}
			firstErr = typeErr("sort comparator must return a Number, not %s", TypeName(v))
			return false
		})
		return out, firstErr
	}
	keys := make([]Value, len(out))
	for i, it := range out {
		k, err := ev.callLambda(f, []Value{it, int64(i), l})
		if err != nil {
			return nil, err
		}
		keys[i] = k
	}
	idx := make([]int, len(out))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(i, j int) bool { return cmpKeys(keys[idx[i]], keys[idx[j]]) < 0 })
	sorted := make(List, len(out))
	for i, k := range idx {
		sorted[i] = out[k]
	}
	return sorted, firstErr
}

func (ev *evaluator) dictMethod(d *Dict, name string, args []Value) (Value, bool, error) {
	switch name {
	case "keys":
		out := List{}
		for _, k := range d.Keys() {
			out = append(out, k)
		}
		return out, true, nil
	case "values":
		out := List{}
		for _, k := range d.keys {
			out = append(out, d.vals[k])
		}
		return out, true, nil
	case "entries":
		out := List{}
		for _, k := range d.keys {
			e := NewDict()
			e.Set("key", k)
			e.Set("value", d.vals[k])
			out = append(out, e)
		}
		return out, true, nil
	case "contains":
		if err := argCount(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		k, err := strArg(name, args, 0)
		if err != nil {
			return nil, true, err
		}
		_, ok := d.Get(k)
		return ok, true, nil
	case "count":
		return int64(d.Len()), true, nil
	case "isEmpty":
		return d.Len() == 0, true, nil
	case "delete":
		if err := argCount(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		k, err := strArg(name, args, 0)
		if err != nil {
			return nil, true, err
		}
		c := d.Copy()
		c.Delete(k)
		return c, true, nil
	case "merge":
		if err := argCount(name, args, 1, 1); err != nil {
			return nil, true, err
		}
		o, ok := args[0].(*Dict)
		if !ok {
			return nil, true, typeErr("merge() needs a Dictionary, not %s", TypeName(args[0]))
		}
		c := d.Copy()
		for _, k := range o.keys {
			c.Set(k, o.vals[k])
		}
		return c, true, nil
	case "get":
		if len(args) != 2 {
			return nil, true, typeErr("get() takes a key and a default")
		}
		k, err := strArg(name, args, 0)
		if err != nil {
			return nil, true, err
		}
		if v, ok := d.Get(k); ok {
			return v, true, nil
		}
		return args[1], true, nil
	}
	if f, ok := d.Lookup(name).(*Lambda); ok {
		v, err := ev.callLambda(f, args)
		return v, true, err
	}
	return nil, false, nil
}

func (ev *evaluator) selectorMethod(s *SelectorValue, name string, args []Value) (Value, bool, error) {
	switch name {
	case "execute":
		sel, err := ev.compileCSS(s.Source, false)
		if err != nil {
			return nil, true, err
		}
		rs := &rootSet{}
		if len(args) == 0 {
			if !ev.env.HasSelf {
				return nil, true, &Error{Type: RuntimeErrorType, Message: "execute(): self is unbound in this context", Reason: ReasonSelfUnbound}
			}
			if ev.env.DocumentOnly {
				ev.addSelfRoot(rs)
			} else if err := ev.addRoots(rs, ev.env.Self); err != nil {
				return nil, true, err
			}
		} else {
			for _, a := range args {
				if err := ev.addRoots(rs, a); err != nil {
					return nil, true, err
				}
			}
		}
		v, err := ev.matchRoots(sel, rs)
		return v, true, err
	case "toString", "String":
		return s.Source, true, nil
	}
	return nil, false, nil
}

// namespaceMember implements Namespace.name without parentheses.
func (ev *evaluator) namespaceMember(ns *TypeNS, name string) (Value, error) {
	if t, ok := typeNamespaces[ns.Name+"."+name]; ok {
		return t, nil
	}
	return NativeFunc(ns.Name+"."+name, -1, func(c *Call, args []Value) (Value, error) {
		v, ok, err := c.ev.namespaceMethod(ns, name, args)
		if !ok && err == nil {
			return nil, typeErr("%s has no function %q", ns.Name, name)
		}
		return v, err
	}), nil
}

func (ev *evaluator) namespaceMethod(ns *TypeNS, name string, args []Value) (Value, bool, error) {
	switch ns.Name {
	case "Integer", "Float", "Number":
		if name == "random" {
			v, err := randomNumber(ns.Name, args)
			return v, true, err
		}
	case "String":
		if name == "random" {
			v, err := randomString(args)
			if err == nil {
				err = ev.r.alloc(len(v.(string)))
			}
			return v, true, err
		}
	case "Element":
		if name == "fromString" {
			s, err := strArg("Element.fromString", args, 0)
			if err != nil {
				return nil, true, err
			}
			v, err := fromString(s)
			return v, true, err
		}
	case "Document":
		if name == "parse" {
			s, err := strArg("Document.parse", args, 0)
			if err != nil {
				return nil, true, err
			}
			v, err := parseDocument(s)
			return v, true, err
		}
	}
	if strings.HasPrefix(ns.Name, "Temporal") {
		return ev.temporalStatic(ns.Name, name, args)
	}
	return nil, false, nil
}
