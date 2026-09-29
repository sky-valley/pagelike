package liquid

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// Number filters (R-LIQ-130 … R-LIQ-137). Inputs and arguments coerce as
// R-LIQ-101 says: numeric strings parse (integer syntax gives an integer),
// anything else is 0.

func numberFilters() map[string]filterSpec {
	binary := func(op byte) filterSpec {
		return filterSpec{min: 1, max: 1, fn: func(c *fcall) (any, error) { return arith(c.in, c.args[0], op) }}
	}
	return map[string]filterSpec{
		"plus":       binary('+'),
		"minus":      binary('-'),
		"times":      binary('*'),
		"divided_by": binary('/'),
		"modulo":     binary('%'),
		"abs": {fn: func(c *fcall) (any, error) {
			i, f, isInt, ok := number(c.in)
			switch {
			case !ok:
				return 0, nil
			case isInt && i == math.MinInt64:
				return nil, errors.New("integer overflow")
			case isInt:
				return mkInt(absInt(i)), nil
			}
			return math.Abs(f), nil
		}},
		"ceil":  {fn: func(c *fcall) (any, error) { return roundTo(c.in, math.Ceil), nil }},
		"floor": {fn: func(c *fcall) (any, error) { return roundTo(c.in, math.Floor), nil }},
		"round": {max: 1, fn: func(c *fcall) (any, error) {
			places, err := c.integer(0, 0)
			if err != nil {
				return nil, err
			}
			i, f, isInt, ok := number(c.in)
			switch {
			case !ok:
				return 0, nil
			case isInt && places >= 0:
				return mkInt(i), nil
			case places <= 0:
				// Half away from zero; negative places round to tens,
				// hundreds, … and still give an integer.
				p := math.Pow10(int(-places))
				return mkInt(satTrunc(math.Round(f/p) * p)), nil
			}
			p := math.Pow10(int(min(places, 15)))
			return math.Round(f*p) / p, nil
		}},
		"at_least": {min: 1, max: 1, fn: func(c *fcall) (any, error) {
			if compareNum(c.in, c.args[0]) < 0 {
				return normNum(c.args[0]), nil
			}
			return normNum(c.in), nil
		}},
		"at_most": {min: 1, max: 1, fn: func(c *fcall) (any, error) {
			if compareNum(c.in, c.args[0]) > 0 {
				return normNum(c.args[0]), nil
			}
			return normNum(c.in), nil
		}},
		"to_integer": {fn: func(c *fcall) (any, error) { return mkInt(toInteger(c.in)), nil }},
	}
}

// roundTo is ceil and floor: an integer result.
func roundTo(v any, fn func(float64) float64) any {
	i, f, isInt, ok := number(v)
	switch {
	case !ok:
		return 0
	case isInt:
		return mkInt(i)
	}
	return mkInt(satTrunc(fn(f)))
}

func compareNum(a, b any) int {
	ai, af, aInt, _ := number(a)
	bi, bf, bInt, _ := number(b)
	return compareNumbers(ai, af, aInt, bi, bf, bInt)
}

// normNum returns a number filter operand as a number (0 when it is not).
func normNum(v any) any {
	i, f, isInt, ok := number(v)
	switch {
	case !ok:
		return 0
	case isInt:
		return mkInt(i)
	}
	return f
}

// toInteger is to_integer (R-LIQ-137): floats truncate toward zero, numeric
// strings parse, booleans are 1 and 0, anything else is 0, and values
// beyond the int64 range saturate.
func toInteger(v any) int64 {
	switch x := v.(type) {
	case bool:
		if x {
			return 1
		}
		return 0
	case string, SafeString:
		s := strings.TrimSpace(toString(x))
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		} else if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			if strings.HasPrefix(s, "-") {
				return math.MinInt64
			}
			return math.MaxInt64
		}
		if s == "" || !looksNumeric(s) {
			return 0
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return 0
		}
		return satTrunc(f)
	}
	i, f, isInt, ok := goNumber(v)
	switch {
	case !ok:
		return 0
	case isInt:
		return i
	}
	return satTrunc(f)
}
