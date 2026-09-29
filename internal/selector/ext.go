// PageLove selector extensions (docs: reference/composing-pages/
// Selector-Extensions), implemented through Options.Pseudo so that the
// cascadia core needs no extension-specific code.

package selector

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/text/unicode/norm"
)

// ExtOptions carries request/site context the extensions need.
type ExtOptions struct {
	// IsA reports whether itemtype is target or a schema descendant of it.
	// nil means "no inheritance map loaded" and :isa() matches nothing.
	IsA func(itemtype, target string) bool
}

// PageLoveOptions returns parse options with every PageLove pseudo-class
// registered and cascadia-only syntax rejected.
func PageLoveOptions(ext ExtOptions) *Options {
	o := &Options{Strict: true, Pseudo: map[string]PseudoClassFunc{}}
	text := func(name string, value, exact bool) {
		o.Pseudo[name] = func(c *PseudoContext) (Sel, error) {
			s, fold, err := parseTextArgs(c)
			if err != nil {
				return nil, err
			}
			return textMatch{name: name, value: value, exact: exact, fold: fold, want: s}, nil
		}
	}
	text("contains", false, false)
	text("equals", false, true)
	text("value-contains", true, false)
	text("value-equals", true, true)
	num := func(name string, value, less bool) {
		o.Pseudo[name] = func(c *PseudoContext) (Sel, error) {
			args, err := splitArgs(c)
			if err != nil {
				return nil, err
			}
			if len(args) != 1 {
				return nil, fmt.Errorf(":%s() takes one number", name)
			}
			f, ok := parseNumber(args[0].s)
			if !ok {
				return nil, fmt.Errorf(":%s(): %q is not a number", name, args[0].s)
			}
			return numMatch{name: name, value: value, less: less, n: f}, nil
		}
	}
	num("less-than", false, true)
	num("greater-than", false, false)
	num("value-less-than", true, true)
	num("value-greater-than", true, false)
	o.Pseudo["only"] = func(c *PseudoContext) (Sel, error) {
		if !c.HasArgs {
			return nil, errors.New(":only() requires arguments")
		}
		rel, err := c.ParseRelativeSelectorList(c.Args)
		if err != nil {
			return nil, err
		}
		return onlyMatch{rel: rel, src: c.Args}, nil
	}
	o.Pseudo["isa"] = func(c *PseudoContext) (Sel, error) {
		args, err := splitArgs(c)
		if err != nil {
			return nil, err
		}
		if len(args) != 1 {
			return nil, errors.New(":isa() takes one itemtype URL")
		}
		return isaMatch{target: args[0].s, isa: ext.IsA}, nil
	}
	return o
}

// ------------------------------------------------------------ argument parsing

type arg struct {
	s      string
	quoted bool
}

// splitArgs splits "'a, b', i" into ["a, b"(quoted), "i"]. CSS escapes in
// quoted strings are decoded ('caf\E9 ' -> café).
func splitArgs(c *PseudoContext) ([]arg, error) {
	if !c.HasArgs {
		return nil, fmt.Errorf(":%s requires arguments", c.Name)
	}
	p := newParser(c.Args, c.opts)
	var out []arg
	for {
		p.skipWhitespace()
		if p.i >= len(p.s) {
			return nil, fmt.Errorf(":%s(): empty argument", c.Name)
		}
		switch p.s[p.i] {
		case '\'', '"':
			v, err := p.parseString()
			if err != nil {
				return nil, err
			}
			out = append(out, arg{s: v, quoted: true})
		default:
			start := p.i
			for p.i < len(p.s) && p.s[p.i] != ',' {
				p.i++
			}
			out = append(out, arg{s: strings.TrimSpace(p.s[start:p.i])})
		}
		p.skipWhitespace()
		if p.i >= len(p.s) {
			return out, nil
		}
		if p.s[p.i] != ',' {
			return nil, fmt.Errorf(":%s(): expected ',' at %q", c.Name, p.s[p.i:])
		}
		p.i++
	}
}

func parseTextArgs(c *PseudoContext) (want string, fold bool, err error) {
	args, err := splitArgs(c)
	if err != nil {
		return "", false, err
	}
	switch {
	case len(args) == 1:
	case len(args) == 2 && !args[1].quoted && strings.EqualFold(args[1].s, "i"):
		fold = true
	default:
		return "", false, fmt.Errorf(":%s(): expected ('text') or ('text', i)", c.Name)
	}
	return normalizeText(args[0].s), fold, nil
}

var numberRE = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`)

func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if !numberRE.MatchString(s) {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

// ------------------------------------------------------------ text & values

// TextContent is the normalized text content used by :contains/:equals:
// NFC, trimmed, internal whitespace runs collapsed to one space.
func TextContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
		case html.ElementNode, html.DocumentNode:
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
	}
	walk(n)
	return normalizeText(b.String())
}

func normalizeText(s string) string {
	s = norm.NFC.String(s)
	return strings.Join(strings.FieldsFunc(s, isCSSSpace), " ")
}

func isCSSSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
}

func getAttr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Namespace == "" && a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

// MicrodataValue implements the WHATWG property-value rules listed in the
// Selector-Extensions docs (missing designated attribute -> "", except
// <time>, which falls back to text content).
func MicrodataValue(n *html.Node) string {
	attr := ""
	switch valueElementName(n) {
	case "meta":
		attr = "content"
	case "audio", "embed", "iframe", "img", "source", "track", "video":
		attr = "src"
	case "a", "area", "link":
		attr = "href"
	case "object":
		attr = "data"
	case "data", "meter":
		attr = "value"
	case "time":
		if v, ok := getAttr(n, "datetime"); ok {
			return normalizeText(v)
		}
		return TextContent(n)
	}
	if attr != "" {
		v, _ := getAttr(n, attr)
		return normalizeText(v)
	}
	return TextContent(n)
}

// valueElementName is the name the microdata value rules switch on: the tag
// name for HTML elements, the local name for XML documents (PageLove runs
// microdata on XML too, e.g. an AuthorizationRule inside an Atom feed), and
// "" for SVG/MathML inside HTML.
func valueElementName(n *html.Node) string {
	switch n.Namespace {
	case "":
		return n.Data
	case "svg", "math":
		return ""
	}
	if i := strings.LastIndexByte(n.Data, ':'); i >= 0 {
		return n.Data[i+1:]
	}
	return n.Data
}

// asciiFold lower-cases A-Z only (the CSS attribute-selector `i` semantics).
func asciiFold(s string) string { return toLowerASCII(s) }

type textMatch struct {
	name               string
	value, exact, fold bool
	want               string
}

func (t textMatch) Match(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	var got string
	if t.value {
		got = MicrodataValue(n)
	} else {
		got = TextContent(n)
	}
	want := t.want
	if t.fold {
		got, want = asciiFold(got), asciiFold(want)
	}
	if t.exact {
		return got == want
	}
	return strings.Contains(got, want)
}
func (textMatch) Specificity() Specificity { return Specificity{0, 1, 0} }
func (textMatch) PseudoElement() string    { return "" }
func (t textMatch) String() string {
	s := fmt.Sprintf(":%s(%s", t.name, strconv.Quote(t.want))
	if t.fold {
		s += ", i"
	}
	return s + ")"
}

type numMatch struct {
	name        string
	value, less bool
	n           float64
}

func (m numMatch) Match(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	var s string
	if m.value {
		s = MicrodataValue(n)
	} else {
		s = TextContent(n)
	}
	f, ok := parseNumber(s)
	if !ok {
		return false
	}
	if m.less {
		return f < m.n
	}
	return f > m.n
}
func (numMatch) Specificity() Specificity { return Specificity{0, 1, 0} }
func (numMatch) PseudoElement() string    { return "" }
func (m numMatch) String() string {
	return fmt.Sprintf(":%s('%s')", m.name, strconv.FormatFloat(m.n, 'g', -1, 64))
}

type onlyMatch struct {
	rel RelativeList
	src string
}

// Match: every descendant element matches at least one relative selector
// anchored at n; vacuously true without descendants.
func (m onlyMatch) Match(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	ok := true
	var walk func(*html.Node)
	walk = func(p *html.Node) {
		for c := p.FirstChild; c != nil && ok; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			if !m.rel.MatchesFrom(n, c) {
				ok = false
				return
			}
			walk(c)
		}
	}
	walk(n)
	return ok
}
func (onlyMatch) Specificity() Specificity { return Specificity{0, 1, 0} }
func (onlyMatch) PseudoElement() string    { return "" }
func (m onlyMatch) String() string         { return ":only(" + m.src + ")" }

type isaMatch struct {
	target string
	isa    func(itemtype, target string) bool
}

func (m isaMatch) Match(n *html.Node) bool {
	if n.Type != html.ElementNode || m.isa == nil {
		return false
	}
	t, ok := getAttr(n, "itemtype")
	if !ok {
		return false
	}
	for _, typ := range strings.Fields(t) {
		if m.isa(typ, m.target) {
			return true
		}
	}
	return false
}
func (isaMatch) Specificity() Specificity { return Specificity{0, 1, 0} }
func (isaMatch) PseudoElement() string    { return "" }
func (m isaMatch) String() string         { return fmt.Sprintf(":isa(%s)", strconv.Quote(m.target)) }
