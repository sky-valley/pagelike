// Selector functions (count(), text-of(), value-of(), attr-of()) are not
// pseudo-classes: they sit in value positions and are replaced by their
// result *before* the selector is parsed, after a cross-document evaluation.

package selector

import (
	"fmt"
	"strconv"
	"strings"
)

// FuncEvaluator evaluates selector functions against the site graph.
type FuncEvaluator interface {
	Count(sel string) (int, error)
	// TextOf/ValueOf/AttrOf return "" for zero matches and an error for more
	// than one match.
	TextOf(sel string) (string, error)
	ValueOf(sel string) (string, error)
	AttrOf(attr, sel string) (string, error)
}

var selectorFuncs = []string{"count(", "text-of(", "value-of(", "attr-of("}

// ExpandFunctions rewrites every selector function in sel with its value:
// count() becomes an integer, the others a double-quoted CSS string.
// Functions nest (their arguments are expanded first).
func ExpandFunctions(sel string, ev FuncEvaluator) (string, error) {
	var b strings.Builder
	for i := 0; i < len(sel); {
		c := sel[i]
		if c == '\'' || c == '"' { // copy strings verbatim
			j := i + 1
			for j < len(sel) && sel[j] != c {
				if sel[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(sel) {
				return "", fmt.Errorf("unterminated string in %q", sel)
			}
			b.WriteString(sel[i : j+1])
			i = j + 1
			continue
		}
		if c == '\\' && i+1 < len(sel) {
			b.WriteString(sel[i : i+2])
			i += 2
			continue
		}
		fn := ""
		if i == 0 || !(nameChar(sel[i-1]) || sel[i-1] == ':' || sel[i-1] == '\\') {
			for _, f := range selectorFuncs {
				if strings.HasPrefix(sel[i:], f) {
					fn = f
					break
				}
			}
		}
		if fn == "" {
			b.WriteByte(c)
			i++
			continue
		}
		p := newParser(sel, nil)
		p.i = i + len(fn) - 1
		raw, err := p.consumeBalancedArgs()
		if err != nil {
			return "", err
		}
		inner, err := ExpandFunctions(raw, ev)
		if err != nil {
			return "", err
		}
		val, err := evalFunc(strings.TrimSuffix(fn, "("), strings.TrimSpace(inner), ev)
		if err != nil {
			return "", err
		}
		b.WriteString(val)
		i = p.i
	}
	return b.String(), nil
}

func evalFunc(name, args string, ev FuncEvaluator) (string, error) {
	switch name {
	case "count":
		n, err := ev.Count(args)
		return strconv.Itoa(n), err
	case "text-of":
		s, err := ev.TextOf(args)
		return cssString(s), err
	case "value-of":
		s, err := ev.ValueOf(args)
		return cssString(s), err
	case "attr-of":
		p := newParser(args, nil)
		p.skipWhitespace()
		if p.i >= len(p.s) || (p.s[p.i] != '\'' && p.s[p.i] != '"') {
			return "", fmt.Errorf("attr-of(): first argument must be a quoted attribute name")
		}
		attr, err := p.parseString()
		if err != nil {
			return "", err
		}
		p.skipWhitespace()
		if p.i >= len(p.s) || p.s[p.i] != ',' {
			return "", fmt.Errorf("attr-of(): expected ','")
		}
		s, err := ev.AttrOf(attr, strings.TrimSpace(p.s[p.i+1:]))
		return cssString(s), err
	}
	return "", fmt.Errorf("unknown selector function %s()", name)
}

// cssString quotes s as a CSS double-quoted string.
func cssString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n' || r == '\r' || r == '\f' || r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\%x ", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
