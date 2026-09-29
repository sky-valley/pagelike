package sessel

import (
	"fmt"
	"strconv"
	"strings"
)

// tokKind enumerates lexical tokens (R-SESSEL-10..17).
type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tInt
	tFloat
	tString
	tSelector // ${ … }
	tAt       // @schema / @namespace (text holds the name)
	tPlus
	tMinus
	tStar
	tSlash
	tBang
	tEq     // ==
	tNe     // !=
	tLt     // <
	tGt     // >
	tLe     // <=
	tGe     // >=
	tCmp    // <=>
	tAnd    // &&
	tOr     // ||
	tCoal   // ??
	tQuest  // ?
	tColon  // :
	tQDot   // ?.
	tDot    // .
	tComma  // ,
	tSemi   // ;
	tLParen // (
	tRParen // )
	tLBrack // [
	tRBrack // ]
	tLBrace // {
	tRBrace // }
	tArrow  // =>
	tEllip  // ...
	tAssign // =
	tPipe   // |
	tDotSel // .${ … }
	tQDotSel
)

var tokNames = map[tokKind]string{
	tEOF: "end of input", tIdent: "identifier", tInt: "integer", tFloat: "number", tString: "string",
	tSelector: "selector literal", tAt: "declaration", tPlus: "'+'", tMinus: "'-'", tStar: "'*'", tSlash: "'/'",
	tBang: "'!'", tEq: "'=='", tNe: "'!='", tLt: "'<'", tGt: "'>'", tLe: "'<='", tGe: "'>='", tCmp: "'<=>'",
	tAnd: "'&&'", tOr: "'||'", tCoal: "'??'", tQuest: "'?'", tColon: "':'", tQDot: "'?.'", tDot: "'.'",
	tComma: "','", tSemi: "';'", tLParen: "'('", tRParen: "')'", tLBrack: "'['", tRBrack: "']'",
	tLBrace: "'{'", tRBrace: "'}'", tArrow: "'=>'", tEllip: "'...'", tAssign: "'='", tPipe: "'|'",
	tDotSel: "'.${'", tQDotSel: "'?.${'",
}

func (k tokKind) String() string {
	if s, ok := tokNames[k]; ok {
		return s
	}
	return fmt.Sprintf("token(%d)", int(k))
}

// token is one lexical token. pos/end are byte offsets in the program.
type token struct {
	kind tokKind
	pos  int
	end  int
	text string // identifier, selector source, declaration name
	soff int    // program offset of a selector token's (trimmed) source
	ival int64
	fval float64
	str  []strPart // string literal pieces
}

// strPart is a literal run or an interpolated expression source (#{ … }).
type strPart struct {
	lit    string
	src    string
	off    int // program offset of src
	isExpr bool
}

// reserved keywords (R-SESSEL-12).
var reserved = map[string]bool{
	"let": true, "if": true, "else": true, "try": true, "catch": true, "return": true, "throw": true,
	"new": true, "from": true, "true": true, "false": true, "null": true, "isa": true,
}

// lexer scans tokens on demand so the parser can switch to the member-name,
// construction-name and raw-selector rules where the grammar requires them.
type lexer struct {
	src  string // the whole program
	pos  int
	stop int // scanning limit (sub-parsers scan a slice of the program)
}

func newLexer(src string, start, stop int) *lexer { return &lexer{src: src, pos: start, stop: stop} }

func (l *lexer) errAt(pos int, format string, args ...any) *ParseError {
	return newParseError(l.src, pos, fmt.Sprintf(format, args...))
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isIdentChar(c byte) bool { return isIdentStart(c) || isDigit(c) }

// skipSpace skips whitespace and comments (// line, /* block */).
func (l *lexer) skipSpace() {
	for l.pos < l.stop {
		c := l.src[l.pos]
		switch {
		case isSpace(c):
			l.pos++
		case c == '/' && l.pos+1 < l.stop && l.src[l.pos+1] == '/':
			for l.pos < l.stop && l.src[l.pos] != '\n' {
				l.pos++
			}
		case c == '/' && l.pos+1 < l.stop && l.src[l.pos+1] == '*':
			end := strings.Index(l.src[l.pos+2:l.stop], "*/")
			if end < 0 {
				panic(l.errAt(l.pos, "unterminated block comment"))
			}
			l.pos += end + 4
		default:
			return
		}
	}
}

func (l *lexer) peekByte(off int) byte {
	if l.pos+off < l.stop {
		return l.src[l.pos+off]
	}
	return 0
}

// next scans the next token (panics with *ParseError on a lexical error).
func (l *lexer) next() token {
	l.skipSpace()
	start := l.pos
	if l.pos >= l.stop {
		return token{kind: tEOF, pos: start, end: start}
	}
	c := l.src[l.pos]
	tk := func(k tokKind, n int) token {
		l.pos += n
		return token{kind: k, pos: start, end: l.pos}
	}
	switch {
	case isIdentStart(c):
		for l.pos < l.stop && isIdentChar(l.src[l.pos]) {
			l.pos++
		}
		return token{kind: tIdent, pos: start, end: l.pos, text: l.src[start:l.pos]}
	case isDigit(c):
		return l.number()
	case c == '"' || c == '\'':
		return l.stringLit()
	case c == '$' && l.peekByte(1) == '{':
		src, soff, end := l.selectorLit(l.pos + 2)
		l.pos = end
		return token{kind: tSelector, pos: start, end: end, text: src, soff: soff}
	case c == '@':
		l.pos++
		s := l.pos
		for l.pos < l.stop && isIdentChar(l.src[l.pos]) {
			l.pos++
		}
		if s == l.pos {
			panic(l.errAt(start, "expected a declaration name after '@'"))
		}
		return token{kind: tAt, pos: start, end: l.pos, text: l.src[s:l.pos]}
	}
	switch c {
	case '+':
		return tk(tPlus, 1)
	case '-':
		return tk(tMinus, 1)
	case '*':
		return tk(tStar, 1)
	case '/':
		return tk(tSlash, 1)
	case '!':
		if l.peekByte(1) == '=' {
			return tk(tNe, 2)
		}
		return tk(tBang, 1)
	case '=':
		switch l.peekByte(1) {
		case '=':
			return tk(tEq, 2)
		case '>':
			return tk(tArrow, 2)
		}
		return tk(tAssign, 1)
	case '<':
		if l.peekByte(1) == '=' {
			if l.peekByte(2) == '>' {
				return tk(tCmp, 3)
			}
			return tk(tLe, 2)
		}
		return tk(tLt, 1)
	case '>':
		if l.peekByte(1) == '=' {
			return tk(tGe, 2)
		}
		return tk(tGt, 1)
	case '&':
		if l.peekByte(1) == '&' {
			return tk(tAnd, 2)
		}
	case '|':
		if l.peekByte(1) == '|' {
			return tk(tOr, 2)
		}
		return tk(tPipe, 1)
	case '?':
		switch l.peekByte(1) {
		case '?':
			return tk(tCoal, 2)
		case '.':
			if l.peekByte(2) == '$' && l.peekByte(3) == '{' {
				src, soff, end := l.selectorLit(l.pos + 4)
				l.pos = end
				return token{kind: tQDotSel, pos: start, end: end, text: src, soff: soff}
			}
			return tk(tQDot, 2)
		}
		return tk(tQuest, 1)
	case ':':
		return tk(tColon, 1)
	case '.':
		if l.peekByte(1) == '.' && l.peekByte(2) == '.' {
			return tk(tEllip, 3)
		}
		if l.peekByte(1) == '$' && l.peekByte(2) == '{' {
			src, soff, end := l.selectorLit(l.pos + 3)
			l.pos = end
			return token{kind: tDotSel, pos: start, end: end, text: src, soff: soff}
		}
		return tk(tDot, 1)
	case ',':
		return tk(tComma, 1)
	case ';':
		return tk(tSemi, 1)
	case '(':
		return tk(tLParen, 1)
	case ')':
		return tk(tRParen, 1)
	case '[':
		return tk(tLBrack, 1)
	case ']':
		return tk(tRBrack, 1)
	case '{':
		return tk(tLBrace, 1)
	case '}':
		return tk(tRBrace, 1)
	}
	panic(l.errAt(start, "unexpected character %q", string(rune(c))))
}

// number scans INTEGER or FLOAT (R-SESSEL-13): a '.' belongs to the number
// only when a digit follows it.
func (l *lexer) number() token {
	start := l.pos
	for l.pos < l.stop && isDigit(l.src[l.pos]) {
		l.pos++
	}
	isFloat := false
	if l.pos+1 < l.stop && l.src[l.pos] == '.' && isDigit(l.src[l.pos+1]) {
		isFloat = true
		l.pos++
		for l.pos < l.stop && isDigit(l.src[l.pos]) {
			l.pos++
		}
	}
	text := l.src[start:l.pos]
	if isFloat {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			panic(l.errAt(start, "invalid number %s", text))
		}
		return token{kind: tFloat, pos: start, end: l.pos, fval: f, text: text}
	}
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		panic(l.errAt(start, "integer literal %s is out of range", text))
	}
	return token{kind: tInt, pos: start, end: l.pos, ival: n, text: text}
}

// stringLit scans a quoted string with escapes and #{ } interpolation
// (R-SESSEL-14).
func (l *lexer) stringLit() token {
	start := l.pos
	q := l.src[l.pos]
	l.pos++
	var parts []strPart
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			parts = append(parts, strPart{lit: b.String()})
			b.Reset()
		}
	}
	for {
		if l.pos >= l.stop {
			panic(l.errAt(start, "unterminated string literal"))
		}
		c := l.src[l.pos]
		switch {
		case c == q:
			l.pos++
			flush()
			return token{kind: tString, pos: start, end: l.pos, str: parts}
		case c == '\\':
			if l.pos+1 >= l.stop {
				panic(l.errAt(start, "unterminated string literal"))
			}
			e := l.src[l.pos+1]
			switch e {
			case '"', '\'', '\\', '#':
				b.WriteByte(e)
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			default:
				b.WriteByte('\\')
				b.WriteByte(e)
			}
			l.pos += 2
		case c == '#' && l.peekByte(1) == '{':
			flush()
			exprStart := l.pos + 2
			end := scanBalanced(l.src, exprStart, l.stop, '}')
			if end < 0 {
				panic(l.errAt(l.pos, "unterminated #{ interpolation"))
			}
			parts = append(parts, strPart{src: l.src[exprStart:end], off: exprStart, isExpr: true})
			l.pos = end + 1
		default:
			b.WriteByte(c)
			l.pos++
		}
	}
}

// selectorLit scans the body of ${ … } starting just after "${" and returns
// the trimmed selector source, its offset, and the offset after the closing
// brace.
func (l *lexer) selectorLit(from int) (string, int, int) {
	end := scanBalanced(l.src, from, l.stop, '}')
	if end < 0 {
		panic(l.errAt(from-2, "unterminated selector literal"))
	}
	s, e := from, end
	for s < e && isSpace(l.src[s]) {
		s++
	}
	for e > s && isSpace(l.src[e-1]) {
		e--
	}
	return l.src[s:e], s, end + 1
}

// scanBalanced returns the index of the byte close that ends the region
// starting at i: (), [] and {} nest, quoted strings are skipped whole (with
// backslash escapes), and nested ${ … } nests through its brace. -1 when
// unterminated.
func scanBalanced(s string, i, stop int, close byte) int {
	var stack []byte
	for i < stop {
		c := s[i]
		if len(stack) == 0 && c == close {
			return i
		}
		switch c {
		case '"', '\'':
			j := i + 1
			for j < stop && s[j] != c {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			if j >= stop {
				return -1
			}
			i = j + 1
			continue
		case '(':
			stack = append(stack, ')')
		case '[':
			stack = append(stack, ']')
		case '{':
			stack = append(stack, '}')
		case ')', ']', '}':
			if len(stack) == 0 || stack[len(stack)-1] != c {
				return -1
			}
			stack = stack[:len(stack)-1]
		}
		i++
	}
	return -1
}

// member scans a member name after '.' or '?.' (R-SESSEL-11): an identifier
// that may contain interior hyphens followed by a letter or underscore.
func (l *lexer) member() (string, bool) {
	l.skipSpace()
	start := l.pos
	if l.pos >= l.stop || !isIdentStart(l.src[l.pos]) {
		return "", false
	}
	for l.pos < l.stop {
		c := l.src[l.pos]
		if isIdentChar(c) {
			l.pos++
			continue
		}
		if c == '-' && l.pos+1 < l.stop && isIdentStart(l.src[l.pos+1]) {
			l.pos++
			continue
		}
		break
	}
	return l.src[start:l.pos], true
}

// cssName scans a construction name (R-SESSEL-16): letters, digits, '-',
// '_' (the caller checks the first character).
func (l *lexer) cssName() string {
	start := l.pos
	if l.pos < l.stop && l.src[l.pos] == '-' {
		l.pos++
	}
	for l.pos < l.stop {
		c := l.src[l.pos]
		if isIdentChar(c) || c == '-' || c >= 0x80 {
			l.pos++
			continue
		}
		break
	}
	return l.src[start:l.pos]
}

// attrName scans a construction attribute name (letters, digits, _ . : -).
func (l *lexer) attrName() string {
	start := l.pos
	for l.pos < l.stop {
		c := l.src[l.pos]
		if isIdentChar(c) || c == '-' || c == '.' || c == ':' || c >= 0x80 {
			l.pos++
			continue
		}
		break
	}
	return l.src[start:l.pos]
}
