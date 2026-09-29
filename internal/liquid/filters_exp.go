package liquid

import (
	"errors"

	"github.com/osteele/liquid/expressions"
)

// Expression-variant filters (R-LIQ-160 … R-LIQ-166): `coll | f: "var",
// "<expression>"`. The library parses the predicate string into a closure
// when a filter parameter has the Closure type; the preprocessor pipes the
// string through pl_pred first, so the predicate gets the same rewriting as
// template expressions (numeric-string comparison, blank/empty, …).
//
// Nesting deeper than Options.MaxExpDepth (32) is a filter error. Errors
// from nested levels are flattened to one message at every level, because
// the library quotes each level's message inside the next one.

func (inst *instance) addExpFilters() {
	add := func(name string, body func(items []any, test func(any) (any, error)) (any, error)) {
		inst.cfg.AddFilter(name, func(in any, varName any, pred expressions.Closure) (any, error) {
			st := inst.st
			v, ok := asString(varName)
			if !ok || !isIdentifier(v) {
				return nil, filterError("the first argument must be a variable name")
			}
			if pred == nil {
				return nil, filterError("missing expression")
			}
			if st.depth >= st.opts.MaxExpDepth {
				st.depthExceeded = true
				return nil, depthError{st.opts.MaxExpDepth}
			}
			if err := st.charge(1 + int64(listLen(in))); err != nil {
				return nil, err
			}
			c := &fcall{st: st, name: name, in: in}
			items, err := c.list()
			if err != nil {
				return nil, err
			}
			st.depth++
			defer func() {
				if st.depth--; st.depth == 0 {
					st.depthExceeded = false
				}
			}()
			return body(items, func(e any) (any, error) {
				if err := st.charge(1); err != nil {
					return nil, err
				}
				r, err := evalClosure(pred.Bind(v, e))
				if err != nil {
					if fatal := st.fatal(err); fatal != nil {
						return nil, fatal
					}
					if st.depthExceeded {
						return nil, depthError{st.opts.MaxExpDepth}
					}
					return nil, errors.New(errMessage(err))
				}
				return r, nil
			})
		})
	}
	add("where_exp", func(items []any, test func(any) (any, error)) (any, error) {
		out := []any{}
		for _, e := range items {
			v, err := test(e)
			if err != nil {
				return nil, err
			}
			if truthy(v) {
				out = append(out, e)
			}
		}
		return out, nil
	})
	add("reject_exp", func(items []any, test func(any) (any, error)) (any, error) {
		out := []any{}
		for _, e := range items {
			v, err := test(e)
			if err != nil {
				return nil, err
			}
			if !truthy(v) {
				out = append(out, e)
			}
		}
		return out, nil
	})
	add("find_exp", func(items []any, test func(any) (any, error)) (any, error) {
		for _, e := range items {
			v, err := test(e)
			if err != nil || truthy(v) {
				return e, err
			}
		}
		return nil, nil
	})
	add("find_index_exp", func(items []any, test func(any) (any, error)) (any, error) {
		for i, e := range items {
			v, err := test(e)
			if err != nil {
				return nil, err
			}
			if truthy(v) {
				return i, nil
			}
		}
		return nil, nil
	})
	add("has_exp", func(items []any, test func(any) (any, error)) (any, error) {
		for _, e := range items {
			v, err := test(e)
			if err != nil {
				return nil, err
			}
			if truthy(v) {
				return true, nil
			}
		}
		return false, nil
	})
	add("group_by_exp", func(items []any, test func(any) (any, error)) (any, error) {
		return groupBy(items, test)
	})
}

// evalClosure evaluates a predicate, turning escaping panics into errors.
func evalClosure(c expressions.Closure) (v any, err error) {
	defer func() {
		if p := recover(); p != nil {
			v, err = nil, panicError(p)
		}
	}()
	return c.Evaluate()
}
