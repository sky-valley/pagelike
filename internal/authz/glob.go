package authz

import (
	"strings"
	"unicode/utf8"
)

// Resource globs (PageLove docs, AuthorizationRule §resource; spec
// R-PERM-20..22). A pattern is anchored at both ends of the path:
//
//	*, **     any run of characters, including '/' and the empty string
//	?         exactly one character
//	[abc]     one character from the class; [a-z] ranges; [!…] or [^…] negates
//	{a,b,c}   alternation (not nested)
//	\c        the literal character c
//
// Anything else is literal and case-sensitive. A pattern that does not
// compile (unbalanced '[' or '{', nested '{') and any pattern containing
// "{{" (legacy Liquid, R-PERM-36) is compared as literal text.

// maxAlternatives bounds the expansion of {…} groups; a pattern producing
// more alternatives is treated as invalid (compared literally).
const maxAlternatives = 256

// Glob is a compiled resource pattern.
type Glob struct {
	src     string
	literal bool        // compare src verbatim
	alts    [][]globTok // alternation-free token sequences, OR-ed
}

type globKind uint8

const (
	gLit globKind = iota
	gOne
	gAny
	gClass
)

type globTok struct {
	kind  globKind
	r     rune
	class *charClass
}

type charClass struct {
	neg    bool
	ranges [][2]rune
}

func (c *charClass) has(r rune) bool {
	in := false
	for _, rg := range c.ranges {
		if r >= rg[0] && r <= rg[1] {
			in = true
			break
		}
	}
	return in != c.neg
}

func (t globTok) matchOne(r rune) bool {
	switch t.kind {
	case gLit:
		return t.r == r
	case gOne:
		return true
	case gClass:
		return t.class.has(r)
	}
	return false
}

// CompileGlob compiles a resource pattern. It never fails: invalid patterns
// compile to literal comparisons.
func CompileGlob(pattern string) *Glob {
	g := &Glob{src: pattern}
	if strings.Contains(pattern, "{{") {
		g.literal = true
		return g
	}
	alts, ok := parseGlob(pattern)
	if !ok {
		g.literal = true
		return g
	}
	g.alts = alts
	return g
}

// Match reports whether the whole path matches.
func (g *Glob) Match(path string) bool {
	if g.literal {
		return g.src == path
	}
	s := []rune(path)
	for _, seq := range g.alts {
		if matchSeq(seq, s) {
			return true
		}
	}
	return false
}

// GlobMatch matches a resource pattern against a path.
func GlobMatch(pattern, path string) bool { return CompileGlob(pattern).Match(path) }

// GlobQuote escapes glob metacharacters so s matches literally (used for
// values substituted into resource patterns, R-PERM-33).
func GlobQuote(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '*', '?', '[', ']', '{', '}', '\\', ',':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// parseGlob tokenizes a pattern and expands its alternation groups into
// alternation-free sequences. ok is false for invalid patterns.
func parseGlob(p string) (alts [][]globTok, ok bool) {
	alts = [][]globTok{nil}
	for i := 0; i < len(p); {
		switch p[i] {
		case '{':
			end, parts, ok := splitAlternation(p, i)
			if !ok {
				return nil, false
			}
			var partToks [][]globTok
			for _, part := range parts {
				toks, ok := tokenize(part)
				if !ok {
					return nil, false
				}
				partToks = append(partToks, toks)
			}
			if len(alts)*len(partToks) > maxAlternatives {
				return nil, false
			}
			var next [][]globTok
			for _, a := range alts {
				for _, t := range partToks {
					seq := make([]globTok, 0, len(a)+len(t))
					seq = append(append(seq, a...), t...)
					next = append(next, seq)
				}
			}
			alts = next
			i = end
		default:
			tok, n, ok := nextToken(p[i:])
			if !ok {
				return nil, false
			}
			for k := range alts {
				alts[k] = append(alts[k], tok)
			}
			i += n
		}
	}
	return alts, true
}

// splitAlternation parses the {…} group starting at p[start] and returns
// the index after its closing brace and its comma-separated parts (raw
// text, escapes kept). Nested or unterminated groups are invalid.
func splitAlternation(p string, start int) (end int, parts []string, ok bool) {
	last := start + 1
	inClass := false
	for j := start + 1; j < len(p); j++ {
		switch c := p[j]; {
		case c == '\\':
			j++
		case inClass:
			if c == ']' {
				inClass = false
			}
		case c == '[':
			inClass = true
			// A ']' right after '[' or '[!' is a member, not the end.
			if j+1 < len(p) && (p[j+1] == '!' || p[j+1] == '^') {
				j++
			}
			if j+1 < len(p) && p[j+1] == ']' {
				j++
			}
		case c == '{':
			return 0, nil, false
		case c == ',':
			parts = append(parts, p[last:j])
			last = j + 1
		case c == '}':
			return j + 1, append(parts, p[last:j]), true
		}
	}
	return 0, nil, false
}

// tokenize converts alternation-free pattern text into tokens.
func tokenize(p string) ([]globTok, bool) {
	var out []globTok
	for i := 0; i < len(p); {
		tok, n, ok := nextToken(p[i:])
		if !ok {
			return nil, false
		}
		out = append(out, tok)
		i += n
	}
	return out, true
}

// nextToken reads one token (not an alternation group) from p.
func nextToken(p string) (globTok, int, bool) {
	r, w := utf8.DecodeRuneInString(p)
	switch r {
	case '*':
		n := 1
		for n < len(p) && p[n] == '*' {
			n++
		}
		return globTok{kind: gAny}, n, true
	case '?':
		return globTok{kind: gOne}, 1, true
	case '[':
		cls, n, ok := parseClass(p)
		if !ok {
			return globTok{}, 0, false
		}
		return globTok{kind: gClass, class: cls}, n, true
	case '{':
		return globTok{}, 0, false // nested alternation
	case '\\':
		if len(p) == 1 {
			return globTok{kind: gLit, r: '\\'}, 1, true
		}
		r2, w2 := utf8.DecodeRuneInString(p[1:])
		return globTok{kind: gLit, r: r2}, 1 + w2, true
	}
	return globTok{kind: gLit, r: r}, w, true
}

// parseClass parses a [...] class at the start of p.
func parseClass(p string) (*charClass, int, bool) {
	c := &charClass{}
	i := 1
	if i < len(p) && (p[i] == '!' || p[i] == '^') {
		c.neg = true
		i++
	}
	first := true
	for i < len(p) {
		if p[i] == ']' && !first {
			return c, i + 1, true
		}
		first = false
		lo, w := utf8.DecodeRuneInString(p[i:])
		if lo == '\\' && i+1 < len(p) {
			i++
			lo, w = utf8.DecodeRuneInString(p[i:])
		}
		i += w
		hi := lo
		if i+1 < len(p) && p[i] == '-' && p[i+1] != ']' {
			i++
			h, w2 := utf8.DecodeRuneInString(p[i:])
			if h == '\\' && i+1 < len(p) {
				i++
				h, w2 = utf8.DecodeRuneInString(p[i:])
			}
			hi = h
			i += w2
		}
		if hi < lo {
			lo, hi = hi, lo
		}
		c.ranges = append(c.ranges, [2]rune{lo, hi})
	}
	return nil, 0, false // unterminated
}

// matchSeq matches an alternation-free token sequence against the whole of
// s. Because '*' matches any run (including '/'), backtracking to the most
// recent star is sufficient, giving O(len(p)·len(s)) time.
func matchSeq(p []globTok, s []rune) bool {
	pi, si := 0, 0
	starP, starS := -1, 0
	for si < len(s) {
		if pi < len(p) && p[pi].kind == gAny {
			starP, starS = pi, si
			pi++
			continue
		}
		if pi < len(p) && p[pi].matchOne(s[si]) {
			pi++
			si++
			continue
		}
		if starP >= 0 {
			pi = starP + 1
			starS++
			si = starS
			continue
		}
		return false
	}
	for pi < len(p) && p[pi].kind == gAny {
		pi++
	}
	return pi == len(p)
}
