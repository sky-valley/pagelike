package liquid

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/osteele/liquid/values"
)

// Array filters (R-LIQ-140 … R-LIQ-156). The input is coerced to an array
// (R-LIQ-101) and a new array is returned; nothing is changed in place.

func arrayFilters() map[string]filterSpec {
	return map[string]filterSpec{
		"first": {fn: func(c *fcall) (any, error) {
			if s, ok := asString(c.in); ok {
				if r, n := utf8.DecodeRuneInString(s); n > 0 {
					return string(r), nil
				}
				return nil, nil
			}
			if r, ok := c.in.(values.Range); ok {
				if r.Len() <= 0 {
					return nil, nil
				}
				return r.Index(0), nil
			}
			items := toArray(c.in)
			if len(items) == 0 {
				return nil, nil
			}
			return items[0], nil
		}},
		"last": {fn: func(c *fcall) (any, error) {
			if s, ok := asString(c.in); ok {
				if r, n := utf8.DecodeLastRuneInString(s); n > 0 {
					return string(r), nil
				}
				return nil, nil
			}
			if r, ok := c.in.(values.Range); ok {
				if r.Len() <= 0 {
					return nil, nil
				}
				return r.Index(r.Len() - 1), nil
			}
			items, err := c.list()
			if err != nil || len(items) == 0 {
				return nil, err
			}
			return items[len(items)-1], nil
		}},
		"reverse": listFilter(0, 0, func(c *fcall, items []any) (any, error) {
			out := make([]any, len(items))
			for i, e := range items {
				out[len(items)-1-i] = e
			}
			return out, nil
		}),
		"sort":         sortSpec(false),
		"sort_natural": sortSpec(true),
		"uniq": listFilter(0, 1, func(c *fcall, items []any) (any, error) {
			key := c.str(0, "")
			seen := map[string]bool{}
			out := []any{}
			for _, e := range items {
				v := e
				if key != "" {
					v, _ = prop(e, key)
				}
				if k := identityKey(v); !seen[k] {
					seen[k] = true
					out = append(out, e)
				}
			}
			return out, nil
		}),
		"compact": listFilter(0, 1, func(c *fcall, items []any) (any, error) {
			key := c.str(0, "")
			out := []any{}
			for _, e := range items {
				v := e
				if key != "" {
					v, _ = prop(e, key)
				}
				if v != nil {
					out = append(out, e)
				}
			}
			return out, nil
		}),
		"map": listFilter(1, 1, func(c *fcall, items []any) (any, error) {
			if !isList(c.in) {
				// Live PageLove maps arrays only: a single item or hash
				// (find's result), a scalar or nil gives [] rather than a
				// one-element array (R-LIQ-145, live 2026-09-29).
				return []any{}, nil
			}
			key := toString(c.args[0])
			out := make([]any, len(items))
			for i, e := range items {
				out[i], _ = prop(e, key)
			}
			return out, nil
		}),
		"join": listFilter(0, 1, func(c *fcall, items []any) (any, error) {
			sep := " "
			if c.has(0) && c.args[0] != nil {
				sep = toString(c.args[0])
			}
			s := make([]string, len(items))
			for i, e := range items {
				s[i] = toString(e)
			}
			return strings.Join(s, sep), nil
		}),
		"concat": listFilter(1, 1, func(c *fcall, items []any) (any, error) {
			more, err := c.listOf(c.args[0])
			if err != nil {
				return nil, err
			}
			if err := c.st.charge(int64(len(more))); err != nil {
				return nil, err
			}
			return append(append(make([]any, 0, len(items)+len(more)), items...), more...), nil
		}),
		"where": matchSpec(func(items []any, i int, m bool, out []any) ([]any, any, bool) {
			return keep(out, items[i], m), nil, false
		}),
		"reject": matchSpec(func(items []any, i int, m bool, out []any) ([]any, any, bool) {
			return keep(out, items[i], !m), nil, false
		}),
		"find":       matchSpec(func(items []any, i int, m bool, _ []any) ([]any, any, bool) { return nil, items[i], m }),
		"find_index": matchSpec(func(_ []any, i int, m bool, _ []any) ([]any, any, bool) { return nil, i, m }),
		"has":        matchSpec(func(_ []any, _ int, m bool, _ []any) ([]any, any, bool) { return nil, true, m }),
		"group_by": listFilter(1, 1, func(c *fcall, items []any) (any, error) {
			key := toString(c.args[0])
			return groupBy(items, func(e any) (any, error) { v, _ := fieldValue(e, key); return v, nil })
		}),
		"sum": listFilter(0, 1, func(c *fcall, items []any) (any, error) {
			key := c.str(0, "")
			var itotal int64
			var ftotal float64
			allInt, overflow := true, false
			for _, e := range items {
				if key != "" {
					e, _ = fieldValue(e, key)
				}
				i, f, isInt, ok := number(e)
				if !ok {
					continue // non-numeric counts as 0
				}
				if isInt && !overflow {
					s := itotal + i
					if (s > itotal) != (i > 0) {
						overflow = true
					}
					itotal = s
				}
				allInt = allInt && isInt
				ftotal += f
			}
			switch {
			case allInt && !overflow:
				return mkInt(itotal), nil
			case ftotal == math.Trunc(ftotal) && math.Abs(ftotal) < 1<<53:
				return mkInt(int64(ftotal)), nil // an integer when whole
			}
			return ftotal, nil
		}),
		"push": listFilter(1, 1, func(c *fcall, items []any) (any, error) {
			return append(append(make([]any, 0, len(items)+1), items...), c.args[0]), nil
		}),
		"unshift": listFilter(1, 1, func(c *fcall, items []any) (any, error) {
			return append([]any{c.args[0]}, items...), nil
		}),
		"pop": listFilter(0, 0, func(c *fcall, items []any) (any, error) {
			if len(items) == 0 {
				return []any{}, nil
			}
			return append([]any{}, items[:len(items)-1]...), nil
		}),
		"shift": listFilter(0, 0, func(c *fcall, items []any) (any, error) {
			if len(items) == 0 {
				return []any{}, nil
			}
			return append([]any{}, items[1:]...), nil
		}),
		"json":    jsonSpec(),
		"jsonify": jsonSpec(),
		"inspect": {fn: func(c *fcall) (any, error) { return encodeJSON(c.in, 0) }},
	}
}

// listFilter is a filter over the input coerced to an array. It walks the
// list, so it is charged per element.
func listFilter(minArgs, maxArgs int, fn func(c *fcall, items []any) (any, error)) filterSpec {
	return filterSpec{min: minArgs, max: maxArgs, walks: true, fn: func(c *fcall) (any, error) {
		items, err := c.list()
		if err != nil {
			return nil, err
		}
		return fn(c, items)
	}}
}

func sortSpec(natural bool) filterSpec {
	return listFilter(0, 1, func(c *fcall, items []any) (any, error) {
		if err := c.st.charge(sortCost(len(items))); err != nil {
			return nil, err
		}
		return sortValues(items, c.str(0, ""), natural), nil
	})
}

func keep(out []any, e any, ok bool) []any {
	if ok {
		out = append(out, e)
	}
	return out
}

// matchSpec builds where, reject, find, find_index and has (R-LIQ-146,
// R-LIQ-147): `field` alone matches a truthy field; `field, value` matches
// a field equal to value with R-LIQ-76 coercion ("true" equals true, "10"
// equals 10). Elements that are not objects never match. step returns the
// list so far, or a result and whether to stop.
func matchSpec(step func(items []any, i int, match bool, out []any) ([]any, any, bool)) filterSpec {
	return listFilter(1, 2, func(c *fcall, items []any) (any, error) {
		key := toString(c.args[0])
		match := func(e any) bool {
			if !isObject(e) {
				return false
			}
			v, _ := prop(e, key)
			if !c.has(1) {
				return truthy(v)
			}
			return looseEqual(v, c.args[1])
		}
		out := []any{}
		for i := range items {
			var res any
			var stop bool
			out, res, stop = step(items, i, match(items[i]), out)
			if stop {
				return res, nil
			}
		}
		switch c.name {
		case "where", "reject":
			return out, nil
		case "has":
			return false, nil
		}
		return nil, nil
	})
}

func jsonSpec() filterSpec {
	return filterSpec{max: 1, fn: func(c *fcall) (any, error) {
		indent, err := c.integer(0, 0)
		if err != nil {
			return nil, err
		}
		return encodeJSON(c.in, int(min(max(indent, 0), 10)))
	}}
}
