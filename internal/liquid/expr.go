package liquid

import (
	"strings"
	"unicode/utf8"

	"github.com/osteele/liquid/expressions"
)

// Expression rewriting.
//
// The library's expression grammar is fixed (goyacc), so PageLove semantics
// it cannot express are obtained by rewriting expression source before the
// library parses it. The rewriter tokenizes like the library's scanner,
// parses the same grammar, and changes only:
//
//   - comparisons: `a OP b` becomes `(a | pl_OP: b)` for ==, !=, <>, <, >, <=
//     and >=, so numeric strings compare with numbers and `blank`/`empty`
//     work on either side (R-LIQ-58, R-LIQ-76; decision 0002 gaps G1, G3);
//   - `a contains b` becomes `(a | pl_contains: b)`: a substring test on
//     strings, membership in arrays and ranges, false otherwise (R-LIQ-74);
//   - `and`/`or` chains that mix the two are grouped right to left, as
//     Shopify evaluates them (R-LIQ-74);
//   - a filter whose first argument is a keyword argument gets a leading
//     `nil,` (`random: upper: 3`), which the grammar otherwise rejects
//     (R-LIQ-75, gap G2);
//   - the predicate argument of the *_exp filters is piped through
//     pl_pred, which applies this same rewriting to the predicate string
//     when the filter runs (R-LIQ-160);
//   - `x.size` becomes `(x | size)`, so strings count code points and
//     hashes, items and ranges follow R-LIQ-55;
//   - range bounds that are not integer literals are piped through pl_int,
//     so `(1..item.count)` accepts numeric strings.
//
// Anything the rewriter does not understand is left exactly as written, and
// a rewrite that the library would not parse falls back to the original, so
// rewriting never turns a valid expression into an invalid one.

type tokKind uint8

const (
	tEOF tokKind = iota
	tString
	tNumber
	tIdent
	tKeyword // `name:` (the text is the name)
	tProp    // `.name` (the text is the name)
	tDotDot
	tCmp
	tAnd
	tOr
	tContains
	tIn
	tLParen
	tRParen
	tLBrack
	tRBrack
	tPipe
	tComma
	tOther
)

type etok struct {
	kind       tokKind
	start, end int
	text       string
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= utf8.RuneSelf
}
func isIdentTail(c byte) bool { return isIdentStart(c) || isDigit(c) || c == '-' }

// scanIdent returns the end of the identifier starting at i.
func scanIdent(s string, i int) int {
	j := i + 1
	for j < len(s) && isIdentTail(s[j]) {
		j++
	}
	if j < len(s) && s[j] == '?' {
		j++
	}
	return j
}

// lexExpr tokenizes an expression the way the library's scanner does. ok is
// false for input it cannot tokenize (an unterminated string).
func lexExpr(src string) (toks []etok, ok bool) {
	i := 0
	add := func(k tokKind, start, end int, text string) {
		toks = append(toks, etok{k, start, end, text})
		i = end
	}
	for i < len(src) {
		c := src[i]
		switch {
		case isSpaceByte(c):
			i++
		case c == '"' || c == '\'':
			j := i + 1
			for j < len(src) && src[j] != c {
				if c == '"' && src[j] == '\\' && j+1 < len(src) {
					j++
				}
				j++
			}
			if j >= len(src) {
				return nil, false
			}
			add(tString, i, j+1, src[i:j+1])
		case isDigit(c) || (c == '-' && i+1 < len(src) && isDigit(src[i+1])):
			j := i + 1
			for j < len(src) && isDigit(src[j]) {
				j++
			}
			if j+1 < len(src) && src[j] == '.' && isDigit(src[j+1]) {
				j += 2
				for j < len(src) && isDigit(src[j]) {
					j++
				}
			}
			add(tNumber, i, j, src[i:j])
		case c == '.' && i+1 < len(src) && src[i+1] == '.':
			add(tDotDot, i, i+2, "..")
		case c == '.' && i+1 < len(src) && isIdentStart(src[i+1]):
			j := scanIdent(src, i+1)
			add(tProp, i, j, src[i+1:j])
		case isIdentStart(c):
			j := scanIdent(src, i)
			word := src[i:j]
			switch {
			case j < len(src) && src[j] == ':':
				add(tKeyword, i, j+1, word)
			case word == "and":
				add(tAnd, i, j, word)
			case word == "or":
				add(tOr, i, j, word)
			case word == "contains":
				add(tContains, i, j, word)
			case word == "in":
				add(tIn, i, j, word)
			default:
				add(tIdent, i, j, word)
			}
		case strings.HasPrefix(src[i:], "=="), strings.HasPrefix(src[i:], "!="),
			strings.HasPrefix(src[i:], "<>"), strings.HasPrefix(src[i:], "<="),
			strings.HasPrefix(src[i:], ">="):
			add(tCmp, i, i+2, src[i:i+2])
		case c == '<' || c == '>':
			add(tCmp, i, i+1, src[i:i+1])
		case c == '(':
			add(tLParen, i, i+1, "(")
		case c == ')':
			add(tRParen, i, i+1, ")")
		case c == '[':
			add(tLBrack, i, i+1, "[")
		case c == ']':
			add(tRBrack, i, i+1, "]")
		case c == '|':
			add(tPipe, i, i+1, "|")
		case c == ',':
			add(tComma, i, i+1, ",")
		default:
			_, size := utf8.DecodeRuneInString(src[i:])
			add(tOther, i, i+size, src[i:i+size])
		}
	}
	return toks, true
}

var cmpFilters = map[string]string{
	"==": "pl_eq", "!=": "pl_ne", "<>": "pl_ne",
	"<": "pl_lt", ">": "pl_gt", "<=": "pl_le", ">=": "pl_ge",
}

var expFilters = map[string]bool{
	"where_exp": true, "reject_exp": true, "find_exp": true,
	"find_index_exp": true, "has_exp": true, "group_by_exp": true,
}

// piece is a parsed span of the source and its rewritten text.
type piece struct {
	start, end int
	out        string
}

type rewriteFailure struct{}

type exprParser struct {
	src  string
	toks []etok
	pos  int
}

func (p *exprParser) peek() tokKind {
	if p.pos < len(p.toks) {
		return p.toks[p.pos].kind
	}
	return tEOF
}

func (p *exprParser) next() etok {
	if p.pos >= len(p.toks) {
		panic(rewriteFailure{})
	}
	t := p.toks[p.pos]
	p.pos++
	return t
}

func (p *exprParser) expect(k tokKind) etok {
	if p.peek() != k {
		panic(rewriteFailure{})
	}
	return p.next()
}

// splice returns src[start:end] with each child's span replaced by its
// rewritten text. Children are in order and may be zero-width insertions.
func (p *exprParser) splice(start, end int, kids []piece) string {
	var b strings.Builder
	at := start
	for _, k := range kids {
		b.WriteString(p.src[at:k.start])
		b.WriteString(k.out)
		at = k.end
	}
	b.WriteString(p.src[at:end])
	return b.String()
}

// cond := rel { (and | or) rel }
func (p *exprParser) cond() piece {
	rels := []piece{p.rel()}
	var ops []etok
	for p.peek() == tAnd || p.peek() == tOr {
		ops = append(ops, p.next())
		rels = append(rels, p.rel())
	}
	start, end := rels[0].start, rels[len(rels)-1].end
	mixed := false
	for _, op := range ops[min(1, len(ops)):] {
		mixed = mixed || op.kind != ops[0].kind
	}
	if !mixed {
		return piece{start, end, p.splice(start, end, rels)}
	}
	out := "(" + rels[len(rels)-1].out + ")"
	for i := len(ops) - 1; i >= 0; i-- {
		out = rels[i].out + " " + ops[i].text + " " + out
		if i > 0 {
			out = "(" + out + ")"
		}
	}
	return piece{start, end, out}
}

// rel := filtered [ CMP expr ]   (the left side must be a plain expr)
func (p *exprParser) rel() piece {
	f, plain := p.filtered()
	if p.peek() == tCmp && plain {
		op := p.next()
		r := p.expr()
		return piece{f.start, r.end, "(" + f.out + " | " + cmpFilters[op.text] + ": " + r.out + ")"}
	}
	return f
}

// filtered := expr [ contains expr ] { '|' name [ ':' params ] }
func (p *exprParser) filtered() (piece, bool) {
	e := p.expr()
	kids := []piece{e}
	end, plain := e.end, true
	if p.peek() == tContains {
		p.next()
		r := p.expr()
		kids = []piece{{e.start, r.end, "(" + e.out + " | pl_contains: " + r.out + ")"}}
		end, plain = r.end, false
	}
	for p.peek() == tPipe {
		p.next()
		plain = false
		name := p.next()
		end = name.end
		switch name.kind {
		case tIdent:
		case tKeyword:
			params, pend := p.params(name.text)
			kids = append(kids, params...)
			end = pend
		default:
			panic(rewriteFailure{})
		}
	}
	return piece{e.start, end, p.splice(e.start, end, kids)}, plain
}

// params := param { ',' param },  param := KEYWORD expr | expr
func (p *exprParser) params(filter string) ([]piece, int) {
	var kids []piece
	positional, end := 0, 0
	for first := true; ; first = false {
		if p.peek() == tKeyword {
			kw := p.next()
			if first {
				kids = append(kids, piece{kw.start, kw.start, "nil, "})
			}
			v := p.expr()
			kids = append(kids, v)
			end = v.end
		} else {
			v := p.expr()
			positional++
			if positional == 2 && expFilters[filter] {
				v.out = "(" + v.out + " | pl_pred)"
			}
			kids = append(kids, v)
			end = v.end
		}
		if p.peek() != tComma {
			return kids, end
		}
		p.next()
	}
}

// expr := primary { '.' name | '[' cond ']' }
func (p *exprParser) expr() piece {
	prim := p.primary()
	start, end, out := prim.start, prim.end, prim.out
	for {
		switch p.peek() {
		case tProp:
			t := p.next()
			if t.text == "size" {
				out = "(" + out + " | size)"
			} else {
				out += p.src[end:t.end]
			}
			end = t.end
		case tLBrack:
			first := p.pos + 1
			p.next()
			idx := p.cond()
			rb := p.expect(tRBrack)
			inner := idx.out
			if lit := p.toks[first]; p.pos-first != 2 || (lit.kind != tString && lit.kind != tNumber) {
				// The library indexes arrays with int only; pl_index
				// converts other integer kinds (int64 from bindings).
				inner = "(" + inner + " | pl_index)"
			}
			out += p.src[end:idx.start] + inner + p.src[idx.end:rb.end]
			end = rb.end
		default:
			return piece{start, end, out}
		}
	}
}

// primary := STRING | NUMBER | IDENT | '(' cond ')' | '(' expr '..' expr ')'
func (p *exprParser) primary() piece {
	switch p.peek() {
	case tString, tNumber, tIdent:
		t := p.next()
		return piece{t.start, t.end, t.text}
	case tLParen:
		lp := p.next()
		inner := p.cond()
		if p.peek() == tDotDot {
			p.next()
			hi := p.cond()
			rp := p.expect(tRParen)
			return piece{lp.start, rp.end, "(" + p.rangeBound(inner) + ".." + p.rangeBound(hi) + ")"}
		}
		rp := p.expect(tRParen)
		return piece{lp.start, rp.end, p.splice(lp.start, rp.end, []piece{inner})}
	}
	panic(rewriteFailure{})
}

// rangeBound pipes a range bound through pl_int unless it is an integer
// literal.
func (p *exprParser) rangeBound(b piece) string {
	lit := strings.TrimSpace(p.src[b.start:b.end])
	if lit != "" && b.out == lit && (isDigit(lit[0]) || lit[0] == '-') && !strings.Contains(lit, ".") {
		return lit
	}
	return "(" + b.out + " | pl_int)"
}

// rewrite runs fn over src's tokens and returns the rewritten text, or src
// itself when the rewriter cannot parse it, when it does not consume the
// whole input, or when validate rejects the result.
func rewrite(src string, fn func(p *exprParser) string, validate func(string) error) (out string) {
	toks, ok := lexExpr(src)
	if !ok || len(toks) == 0 {
		return src
	}
	defer func() {
		if r := recover(); r != nil {
			if _, failed := r.(rewriteFailure); !failed {
				panic(r)
			}
			out = src
		}
	}()
	p := &exprParser{src: src, toks: toks}
	out = fn(p)
	if p.pos != len(p.toks) {
		return src
	}
	if out != src && validate != nil && validate(out) != nil && validate(src) == nil {
		return src
	}
	return out
}

func parseExpression(s string) error { _, err := expressions.Parse(s); return err }

// rewriteCond rewrites a condition or output expression.
func rewriteCond(src string) string {
	return rewrite(src, func(p *exprParser) string {
		c := p.cond()
		return p.src[:c.start] + c.out + p.src[c.end:]
	}, parseExpression)
}

// rewriteAssign rewrites the right-hand side of `assign target = value`.
func rewriteAssign(src string) string {
	return rewrite(src, func(p *exprParser) string {
		for p.peek() != tEOF && !(p.peek() == tOther && p.toks[p.pos].text == "=") {
			p.next()
		}
		p.expect(tOther)
		c := p.cond()
		return p.src[:c.start] + c.out + p.src[c.end:]
	}, func(s string) error {
		_, err := expressions.ParseStatement(expressions.AssignStatementSelector, s)
		return err
	})
}

// rewriteLoop rewrites `var in collection modifiers` (for, tablerow).
func rewriteLoop(src string) string {
	return rewrite(src, func(p *exprParser) string {
		p.expect(tIdent)
		p.expect(tIn)
		coll, _ := p.filtered()
		kids := []piece{coll}
		for p.peek() != tEOF {
			switch p.peek() {
			case tIdent:
				p.next()
			case tKeyword:
				p.next()
				kids = append(kids, p.expr())
			case tComma:
				p.next()
			default:
				panic(rewriteFailure{})
			}
		}
		return p.splice(0, len(p.src), kids)
	}, func(s string) error {
		_, err := expressions.ParseStatement(expressions.LoopStatementSelector, s)
		return err
	})
}

// rewriteWhen rewrites the values of a `when` clause: expr { (',' | or) expr }.
func rewriteWhen(src string) string {
	return rewrite(src, func(p *exprParser) string {
		var kids []piece
		for {
			kids = append(kids, p.expr())
			if p.peek() != tComma && p.peek() != tOr {
				break
			}
			p.next()
		}
		return p.splice(0, len(p.src), kids)
	}, func(s string) error {
		_, err := expressions.ParseStatement(expressions.WhenStatementSelector, s)
		return err
	})
}
