package sessel

import (
	"fmt"
	"regexp"
	"strings"
)

// parser is a recursive-descent parser for the reconstructed grammar of
// R-SESSEL-30. It drives the on-demand lexer directly where the grammar
// switches lexical rules (member names, construction names, selectors).
type parser struct {
	lx    *lexer
	tok   token
	ns    map[string]string // @namespace prefixes declared by the program
	depth int               // expression nesting (bounded by maxNesting)
}

// maxNesting bounds expression nesting so hostile programs cannot exhaust
// the stack during parsing or evaluation.
const maxNesting = 400

type parserState struct {
	pos int
	tok token
}

func (p *parser) save() parserState     { return parserState{p.lx.pos, p.tok} }
func (p *parser) restore(s parserState) { p.lx.pos, p.tok = s.pos, s.tok }

func (p *parser) advance() { p.tok = p.lx.next() }

func (p *parser) fail(pos int, format string, args ...any) {
	panic(newParseError(p.lx.src, pos, fmt.Sprintf(format, args...)))
}

func (p *parser) unexpected() {
	switch p.tok.kind {
	case tIdent:
		p.fail(p.tok.pos, "unexpected %q", p.tok.text)
	case tAt:
		p.fail(p.tok.pos, "declarations (@%s) must appear before any statement", p.tok.text)
	}
	p.fail(p.tok.pos, "unexpected %s", p.tok.kind)
}

func (p *parser) expect(k tokKind) token {
	if p.tok.kind != k {
		if p.tok.kind == tEOF {
			p.fail(p.tok.pos, "expected %s, found end of input", k)
		}
		p.fail(p.tok.pos, "expected %s, found %s", k, describe(p.tok))
	}
	t := p.tok
	p.advance()
	return t
}

func describe(t token) string {
	if t.kind == tIdent {
		return fmt.Sprintf("%q", t.text)
	}
	return t.kind.String()
}

func (p *parser) kw(s string) bool { return p.tok.kind == tIdent && p.tok.text == s }

// peek returns the token after the current one (not valid right after a
// '.' / '?.' token, whose member name uses a different lexical rule).
func (p *parser) peek() (t token) {
	save := p.lx.pos
	defer func() {
		p.lx.pos = save
		if r := recover(); r != nil {
			if _, ok := r.(*ParseError); !ok {
				panic(r)
			}
			t = token{kind: tEOF}
		}
	}()
	return p.lx.next()
}

// parseProgram compiles src. Parse errors are returned as *ParseError.
func parseProgram(src string) (prog *program, err error) {
	defer func() {
		if r := recover(); r != nil {
			pe, ok := r.(*ParseError)
			if !ok {
				panic(r)
			}
			prog, err = nil, pe
		}
	}()
	p := &parser{lx: newLexer(src, 0, len(src)), ns: map[string]string{}}
	p.advance()
	prog = &program{src: src}
	for p.tok.kind == tAt {
		prog.decls = append(prog.decls, p.parseDecl())
	}
	prog.body = p.parseFrame()
	if p.tok.kind != tEOF {
		p.unexpected()
	}
	return prog, nil
}

// parseDecl parses @schema/@namespace NAME url("…") [;] (R-SESSEL-39).
func (p *parser) parseDecl() decl {
	at := p.tok
	if at.text != "schema" && at.text != "namespace" {
		p.fail(at.pos, "unknown declaration @%s", at.text)
	}
	p.advance()
	if p.tok.kind != tIdent || reserved[p.tok.text] {
		p.fail(p.tok.pos, "expected a name after @%s", at.text)
	}
	name := p.tok.text
	p.advance()
	if !p.kw("url") {
		p.fail(p.tok.pos, "expected url(\"…\") in @%s", at.text)
	}
	p.advance()
	p.expect(tLParen)
	if p.tok.kind != tString {
		p.fail(p.tok.pos, "expected a string URL")
	}
	url := literalString(p.tok)
	p.advance()
	p.expect(tRParen)
	if p.tok.kind == tSemi {
		p.advance()
	}
	d := decl{kind: at.text, name: name, url: url}
	if d.kind == "namespace" {
		p.ns[name] = url
	}
	return d
}

func literalString(t token) string {
	var b strings.Builder
	for _, part := range t.str {
		if part.isExpr {
			b.WriteString("#{" + part.src + "}")
		} else {
			b.WriteString(part.lit)
		}
	}
	return b.String()
}

func (p *parser) canStartStatement() bool {
	switch p.tok.kind {
	case tInt, tFloat, tString, tSelector, tLParen, tLBrack, tLBrace, tBang, tMinus:
		return true
	case tIdent:
		switch p.tok.text {
		case "else", "catch", "isa":
			return false
		}
		return true
	}
	return false
}

func isCloser(k tokKind) bool {
	return k == tRParen || k == tRBrack || k == tRBrace || k == tComma || k == tEOF
}

// parseFrame parses statements up to '}' or end of input (not consumed).
func (p *parser) parseFrame() *blockNode {
	b := &blockNode{base: base{p.tok.pos}}
	for {
		for p.tok.kind == tSemi {
			p.advance()
		}
		if p.tok.kind == tRBrace || p.tok.kind == tEOF {
			return b
		}
		if !p.canStartStatement() {
			p.unexpected()
		}
		b.stmts = append(b.stmts, p.parseStatement())
		if p.tok.kind == tSemi || p.tok.kind == tRBrace || p.tok.kind == tEOF {
			continue
		}
		if !p.canStartStatement() {
			p.unexpected()
		}
	}
}

func (p *parser) parseBlock() *blockNode {
	p.expect(tLBrace)
	b := p.parseFrame()
	p.expect(tRBrace)
	return b
}

// parseStatement parses let_stmt | assign_stmt | expr.
func (p *parser) parseStatement() node {
	if p.kw("let") {
		pos := p.tok.pos
		p.advance()
		pat := p.parsePattern()
		p.expect(tAssign)
		return &letNode{base: base{pos}, pat: pat, val: p.parseExpr()}
	}
	e := p.parseExpr()
	if p.tok.kind == tAssign {
		pos := p.tok.pos
		p.checkTarget(e)
		p.advance()
		return &assignNode{base: base{pos}, target: e, val: p.parseExpr()}
	}
	return e
}

// checkTarget enforces R-SESSEL-35.
func (p *parser) checkTarget(e node) {
	switch t := e.(type) {
	case *identNode:
		p.fail(t.pos, "cannot assign to %s; rebind it with let", t.name)
	case *indexNode:
		if t.opt {
			break
		}
		if _, ok := t.x.(*identNode); ok {
			return
		}
		p.fail(t.pos, "index assignment is not a valid assignment target")
	case *memberNode:
		for x := node(t); ; {
			switch m := x.(type) {
			case *memberNode:
				if m.opt {
					p.fail(m.pos, "invalid assignment target")
				}
				x = m.x
				continue
			case *identNode:
				return
			case *indexNode:
				p.fail(m.pos, "index assignment is not a valid assignment target")
			}
			break
		}
	}
	p.fail(e.at(), "invalid assignment target")
}

func (p *parser) ident() string {
	if p.tok.kind != tIdent || reserved[p.tok.text] {
		p.fail(p.tok.pos, "expected a name, found %s", describe(p.tok))
	}
	s := p.tok.text
	p.advance()
	return s
}

// parsePattern parses IDENT | { field, … } | [ item, … ] (R-SESSEL-69).
func (p *parser) parsePattern() pattern {
	switch p.tok.kind {
	case tLBrace:
		p.advance()
		pt := pattern{isDict: true}
		for p.tok.kind != tRBrace {
			k := p.ident()
			f := patField{key: k, name: k}
			if p.tok.kind == tColon {
				p.advance()
				f.name = p.ident()
			}
			pt.fields = append(pt.fields, f)
			if p.tok.kind != tComma {
				break
			}
			p.advance()
		}
		p.expect(tRBrace)
		return pt
	case tLBrack:
		p.advance()
		pt := pattern{isList: true}
		for p.tok.kind != tRBrack {
			if p.tok.kind == tEllip {
				pos := p.tok.pos
				p.advance()
				if pt.rest != "" {
					p.fail(pos, "only one rest element is allowed")
				}
				pt.rest = p.ident()
			} else {
				if pt.rest != "" {
					p.fail(p.tok.pos, "the rest element must be last")
				}
				pt.items = append(pt.items, p.ident())
			}
			if p.tok.kind != tComma {
				break
			}
			p.advance()
		}
		p.expect(tRBrack)
		return pt
	}
	return pattern{name: p.ident()}
}

// parseExpr parses expr = let_expr | lambda | return | throw | ternary.
func (p *parser) parseExpr() node {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxNesting {
		p.fail(p.tok.pos, "expression nests too deeply")
	}
	switch {
	case p.kw("let"):
		return p.parseLetExpr()
	case p.kw("return"):
		pos := p.tok.pos
		p.advance()
		if isCloser(p.tok.kind) || p.tok.kind == tSemi {
			return &returnNode{base: base{pos}}
		}
		return &returnNode{base: base{pos}, x: p.parseExpr()}
	case p.kw("throw"):
		pos := p.tok.pos
		p.advance()
		return &throwNode{base: base{pos}, x: p.parseExpr()}
	}
	if p.lambdaAhead() {
		return p.parseLambda()
	}
	return p.parseTernary()
}

// parseLetExpr parses `let p = e; rest…` in expression position
// (R-SESSEL-32): the rest runs to the enclosing ), ], }, ',' or the end.
func (p *parser) parseLetExpr() node {
	b := &blockNode{base: base{p.tok.pos}}
	b.stmts = append(b.stmts, p.parseStatement())
	for {
		for p.tok.kind == tSemi {
			p.advance()
		}
		if isCloser(p.tok.kind) {
			return b
		}
		if !p.canStartStatement() {
			p.unexpected()
		}
		b.stmts = append(b.stmts, p.parseStatement())
	}
}

// lambdaAhead reports whether a lambda starts here (R-SESSEL-34).
func (p *parser) lambdaAhead() (ok bool) {
	switch p.tok.kind {
	case tIdent:
		if reserved[p.tok.text] {
			return false
		}
		return p.peek().kind == tArrow
	case tLParen:
		st := p.save()
		defer func() {
			p.restore(st)
			if r := recover(); r != nil {
				if _, isPE := r.(*ParseError); !isPE {
					panic(r)
				}
				ok = false
			}
		}()
		depth := 0
		for {
			switch p.tok.kind {
			case tLParen:
				depth++
			case tRParen:
				depth--
				if depth == 0 {
					p.advance()
					return p.tok.kind == tArrow
				}
			case tEOF:
				return false
			}
			p.advance()
		}
	}
	return false
}

func (p *parser) parseLambda() node {
	l := &lambdaNode{base: base{p.tok.pos}}
	if p.tok.kind == tIdent {
		l.params = []string{p.ident()}
	} else {
		p.expect(tLParen)
		for p.tok.kind != tRParen {
			if p.tok.kind == tEllip {
				pos := p.tok.pos
				p.advance()
				if l.rest != "" {
					p.fail(pos, "only one rest parameter is allowed")
				}
				l.rest = p.ident()
			} else {
				if l.rest != "" {
					p.fail(p.tok.pos, "the rest parameter must be last")
				}
				l.params = append(l.params, p.ident())
			}
			if p.tok.kind != tComma {
				break
			}
			p.advance()
		}
		p.expect(tRParen)
	}
	p.expect(tArrow)
	if p.tok.kind == tLBrace && !p.dictAhead() {
		l.body = p.parseBlock()
		return l
	}
	l.body = p.parseExpr()
	return l
}

// dictAhead: at '{', a dictionary literal starts when IDENT ':' or
// STRING ':' follows (R-SESSEL-33).
func (p *parser) dictAhead() bool {
	st := p.save()
	defer p.restore(st)
	p.advance()
	if p.tok.kind == tString || (p.tok.kind == tIdent && !reserved[p.tok.text]) {
		return p.peek().kind == tColon
	}
	return false
}

func (p *parser) parseTernary() node {
	c := p.parseCoalesce()
	if p.tok.kind != tQuest {
		return c
	}
	pos := p.tok.pos
	p.advance()
	a := p.parseBranch()
	p.expect(tColon)
	b := p.parseBranch()
	return &ternaryNode{base: base{pos}, c: c, a: a, b: b}
}

func (p *parser) parseBranch() node {
	if p.kw("throw") || p.kw("return") || p.lambdaAhead() {
		return p.parseExpr()
	}
	return p.parseTernary()
}

func (p *parser) parseCoalesce() node {
	l := p.parseOr()
	for p.tok.kind == tCoal {
		pos := p.tok.pos
		p.advance()
		l = &binaryNode{base: base{pos}, op: tCoal, l: l, r: p.parseOr()}
	}
	return l
}

func (p *parser) parseOr() node {
	l := p.parseAnd()
	for p.tok.kind == tOr {
		pos := p.tok.pos
		p.advance()
		l = &binaryNode{base: base{pos}, op: tOr, l: l, r: p.parseAnd()}
	}
	return l
}

func (p *parser) parseAnd() node {
	l := p.parseCompare()
	for p.tok.kind == tAnd {
		pos := p.tok.pos
		p.advance()
		l = &binaryNode{base: base{pos}, op: tAnd, l: l, r: p.parseCompare()}
	}
	return l
}

func (p *parser) parseCompare() node {
	l := p.parseAdditive()
	for {
		switch p.tok.kind {
		case tEq, tNe, tLt, tGt, tLe, tGe, tCmp:
			op, pos := p.tok.kind, p.tok.pos
			p.advance()
			l = &binaryNode{base: base{pos}, op: op, l: l, r: p.parseAdditive()}
		case tIdent:
			if p.tok.text != "isa" {
				return l
			}
			pos := p.tok.pos
			p.advance()
			l = &isaNode{base: base{pos}, x: l, path: p.parseTypeName()}
		default:
			return l
		}
	}
}

func (p *parser) parseTypeName() []string {
	if p.tok.kind != tIdent {
		p.fail(p.tok.pos, "expected a type name after isa")
	}
	path := []string{p.tok.text}
	p.advance()
	for p.tok.kind == tDot {
		name, ok := p.lx.member()
		if !ok {
			p.fail(p.tok.pos, "expected a type name after '.'")
		}
		path = append(path, name)
		p.advance()
	}
	return path
}

func (p *parser) parseAdditive() node {
	l := p.parseMult()
	for p.tok.kind == tPlus || p.tok.kind == tMinus {
		op, pos := p.tok.kind, p.tok.pos
		p.advance()
		l = &binaryNode{base: base{pos}, op: op, l: l, r: p.parseMult()}
	}
	return l
}

func (p *parser) parseMult() node {
	l := p.parseUnary()
	for p.tok.kind == tStar || p.tok.kind == tSlash {
		op, pos := p.tok.kind, p.tok.pos
		p.advance()
		l = &binaryNode{base: base{pos}, op: op, l: l, r: p.parseUnary()}
	}
	return l
}

func (p *parser) parseUnary() node {
	if p.tok.kind == tBang || p.tok.kind == tMinus {
		op, pos := p.tok.kind, p.tok.pos
		p.advance()
		p.depth++
		defer func() { p.depth-- }()
		if p.depth > maxNesting {
			p.fail(pos, "expression nests too deeply")
		}
		return &unaryNode{base: base{pos}, op: op, x: p.parseUnary()}
	}
	return p.parsePostfix(p.parsePrimary())
}

func (p *parser) parseArgs() []node {
	p.expect(tLParen)
	var args []node
	for p.tok.kind != tRParen {
		args = append(args, p.parseExpr())
		if p.tok.kind != tComma {
			break
		}
		p.advance()
	}
	p.expect(tRParen)
	return args
}

func (p *parser) parsePostfix(x node) node {
	for {
		switch p.tok.kind {
		case tDot:
			pos := p.tok.pos
			name, ok := p.lx.member()
			if !ok {
				p.fail(pos, "expected a member name after '.'")
			}
			p.advance()
			if p.tok.kind == tLParen {
				x = &methodNode{base: base{pos}, x: x, name: name, args: p.parseArgs()}
			} else {
				x = &memberNode{base: base{pos}, x: x, name: name}
			}
		case tQDot:
			pos := p.tok.pos
			p.lx.skipSpace()
			if p.lx.peekByte(0) == '[' {
				p.advance() // '['
				p.advance()
				idx := p.parseExpr()
				p.expect(tRBrack)
				x = &indexNode{base: base{pos}, x: x, idx: idx, opt: true}
				continue
			}
			name, ok := p.lx.member()
			if !ok {
				p.fail(pos, "expected a member name after '?.'")
			}
			p.advance()
			if p.tok.kind == tLParen {
				x = &methodNode{base: base{pos}, x: x, name: name, args: p.parseArgs(), opt: true}
			} else {
				x = &memberNode{base: base{pos}, x: x, name: name, opt: true}
			}
		case tQDotSel:
			p.fail(p.tok.pos, "optional chaining does not apply to sub-select; use ??")
		case tDotSel:
			t := p.tok
			x = &subSelectNode{base: base{t.pos}, x: x, sel: p.selTemplate(t)}
			p.advance()
		case tLBrack:
			pos := p.tok.pos
			p.advance()
			idx := p.parseExpr()
			p.expect(tRBrack)
			x = &indexNode{base: base{pos}, x: x, idx: idx}
		default:
			return x
		}
	}
}

func (p *parser) parsePrimary() node {
	t := p.tok
	pos := t.pos
	switch t.kind {
	case tInt:
		p.advance()
		return &litNode{base: base{pos}, val: t.ival}
	case tFloat:
		p.advance()
		return &litNode{base: base{pos}, val: t.fval}
	case tString:
		p.advance()
		return p.stringNode(t)
	case tSelector:
		p.advance()
		n := &selectorNode{base: base{pos}, sel: p.selTemplate(t)}
		if p.kw("from") {
			p.advance()
			n.hasFrom = true
			n.sources = p.parseSources(false)
		}
		return n
	case tLBrace:
		return p.parseDict()
	case tLBrack:
		p.advance()
		l := &listNode{base: base{pos}}
		for p.tok.kind != tRBrack {
			l.items = append(l.items, p.parseExpr())
			if p.tok.kind != tComma {
				break
			}
			p.advance()
		}
		p.expect(tRBrack)
		return l
	case tLParen:
		if p.lambdaAhead() {
			return p.parseLambda()
		}
		p.advance()
		e := p.parseExpr()
		p.expect(tRParen)
		return e
	case tIdent:
		switch t.text {
		case "true":
			p.advance()
			return &litNode{base: base{pos}, val: true}
		case "false":
			p.advance()
			return &litNode{base: base{pos}, val: false}
		case "null":
			p.advance()
			return &litNode{base: base{pos}, val: nil}
		case "new":
			return p.parseNew()
		case "if":
			return p.parseIf()
		case "try":
			return p.parseTry()
		case "from":
			return p.parseFromBlock()
		}
		if reserved[t.text] {
			p.unexpected()
		}
		p.advance()
		if p.tok.kind == tLParen {
			return &callNode{base: base{pos}, name: t.text, args: p.parseArgs()}
		}
		return &identNode{base: base{pos}, name: t.text}
	}
	p.unexpected()
	return nil
}

func (p *parser) stringNode(t token) node {
	n := &strNode{base: base{t.pos}}
	for _, part := range t.str {
		if !part.isExpr {
			n.parts = append(n.parts, strNodePart{lit: part.lit})
			continue
		}
		n.parts = append(n.parts, strNodePart{expr: p.subExpr(part.off, part.off+len(part.src), "empty #{} interpolation")})
	}
	return n
}

// subExpr parses the program slice [start, stop) as one expression.
func (p *parser) subExpr(start, stop int, emptyMsg string) node {
	sp := &parser{lx: newLexer(p.lx.src, start, stop), ns: p.ns, depth: p.depth + 1}
	sp.advance()
	if sp.tok.kind == tEOF {
		p.fail(start, "%s", emptyMsg)
	}
	e := sp.parseExpr()
	if sp.tok.kind != tEOF {
		sp.unexpected()
	}
	return e
}

func (p *parser) parseDict() node {
	d := &dictNode{base: base{p.tok.pos}}
	p.expect(tLBrace)
	for p.tok.kind != tRBrace {
		var key string
		switch {
		case p.tok.kind == tString:
			key = literalString(p.tok)
		case p.tok.kind == tIdent && !reserved[p.tok.text]:
			key = p.tok.text
		case p.tok.kind == tIdent:
			p.fail(p.tok.pos, "%q is a reserved word; quote it to use it as a key", p.tok.text)
		default:
			p.fail(p.tok.pos, "expected a dictionary key, found %s", describe(p.tok))
		}
		p.advance()
		p.expect(tColon)
		d.keys = append(d.keys, key)
		d.vals = append(d.vals, p.parseExpr())
		if p.tok.kind != tComma {
			break
		}
		p.advance()
	}
	p.expect(tRBrace)
	return d
}

// parseSources parses from-sources (R-SESSEL-36). In a from_list a ','
// continues the list only before self, document, prior or a string; in a
// from-block every ',' does.
func (p *parser) parseSources(block bool) []node {
	srcs := []node{p.parseSource()}
	for p.tok.kind == tComma {
		if !block {
			nt := p.peek()
			if !(nt.kind == tString || (nt.kind == tIdent && (nt.text == "self" || nt.text == "document" || nt.text == "prior"))) {
				break
			}
		}
		p.advance()
		srcs = append(srcs, p.parseSource())
	}
	return srcs
}

func (p *parser) parseSource() node {
	if p.kw("prior") {
		n := &priorNode{base: base{p.tok.pos}}
		p.advance()
		return p.parsePostfix(n)
	}
	return p.parsePostfix(p.parsePrimary())
}

func (p *parser) parseFromBlock() node {
	n := &fromBlockNode{base: base{p.tok.pos}}
	p.advance()
	n.sources = p.parseSources(true)
	n.body = p.parseBlock()
	return n
}

func (p *parser) parseIf() node {
	n := &ifNode{base: base{p.tok.pos}}
	p.advance()
	for {
		p.expect(tLParen)
		n.conds = append(n.conds, p.parseExpr())
		p.expect(tRParen)
		n.blocks = append(n.blocks, p.parseBlock())
		if !p.kw("else") {
			return n
		}
		p.advance()
		if p.kw("if") {
			p.advance()
			continue
		}
		n.els = p.parseBlock()
		return n
	}
}

func (p *parser) parseTry() node {
	n := &tryNode{base: base{p.tok.pos}}
	p.advance()
	n.body = p.parseBlock()
	if !p.kw("catch") {
		p.fail(p.tok.pos, "expected catch after try block")
	}
	p.advance()
	p.expect(tLParen)
	n.name = p.ident()
	p.expect(tRParen)
	n.catch = p.parseBlock()
	return n
}

// parseNew parses construct (R-SESSEL-30, 37, 260..265) with the CSS
// name rules of R-SESSEL-16.
func (p *parser) parseNew() node {
	n := &newNode{base: base{p.tok.pos}}
	lx := p.lx
	lx.skipSpace()
	if lx.pos >= lx.stop || !(isIdentStart(lx.src[lx.pos]) || lx.src[lx.pos] >= 0x80) {
		p.fail(lx.pos, "expected an element or class name after new")
	}
	n.name = lx.cssName()
	if lx.peekByte(0) == '|' && lx.peekByte(1) != '|' {
		lx.pos++
		n.prefix = n.name
		n.name = lx.cssName()
		if n.name == "" {
			p.fail(lx.pos, "expected a name after %s|", n.prefix)
		}
	}
mods:
	for {
		switch lx.peekByte(0) {
		case '.':
			save := lx.pos
			c := lx.peekByte(1)
			if !(isIdentStart(c) || c == '-' || c >= 0x80) {
				break mods
			}
			lx.pos++
			cls := lx.cssName()
			j := lx.pos
			for j < lx.stop && isSpace(lx.src[j]) {
				j++
			}
			if j < lx.stop && lx.src[j] == '(' { // a method call on the new element
				lx.pos = save
				break mods
			}
			n.mods = append(n.mods, modifier{kind: '.', name: cls})
		case '#':
			lx.pos++
			id := lx.cssName()
			if id == "" {
				p.fail(lx.pos, "expected an id after '#'")
			}
			n.mods = append(n.mods, modifier{kind: '#', name: id})
		case '[':
			lx.pos++
			lx.skipSpace()
			m := modifier{kind: '['}
			an := lx.attrName()
			if lx.peekByte(0) == '|' && lx.peekByte(1) != '=' {
				lx.pos++
				m.prefix = an
				an = lx.attrName()
			}
			if an == "" {
				p.fail(lx.pos, "expected an attribute name")
			}
			m.name = an
			lx.skipSpace()
			if lx.peekByte(0) == '=' {
				lx.pos++
				lx.skipSpace()
				m.hasVal = true
				if c := lx.peekByte(0); c == '"' || c == '\'' {
					s, end, ok := rawQuoted(lx.src, lx.pos, lx.stop)
					if !ok {
						p.fail(lx.pos, "unterminated attribute value")
					}
					m.lit = s
					lx.pos = end
					lx.skipSpace()
					if lx.peekByte(0) != ']' {
						p.fail(lx.pos, "expected ']'")
					}
					lx.pos++
				} else {
					end := scanBalanced(lx.src, lx.pos, lx.stop, ']')
					if end < 0 {
						p.fail(lx.pos, "unterminated attribute modifier")
					}
					m.hole = p.makeHole(lx.pos, end, false)
					lx.pos = end + 1
				}
			} else {
				if lx.peekByte(0) != ']' {
					p.fail(lx.pos, "expected ']' or '=' in attribute modifier")
				}
				lx.pos++
			}
			n.mods = append(n.mods, m)
		default:
			break mods
		}
	}
	p.advance()
	if p.tok.kind != tLBrace {
		return n
	}
	n.hasBody = true
	p.advance()
	for p.tok.kind != tRBrace {
		var it citem
		if (p.tok.kind == tString || (p.tok.kind == tIdent && !reserved[p.tok.text])) && p.peek().kind == tColon {
			if p.tok.kind == tString {
				it.name = literalString(p.tok)
			} else {
				it.name = p.tok.text
			}
			p.advance()
			p.advance()
		}
		it.expr = p.parseExpr()
		n.items = append(n.items, it)
		if p.tok.kind != tComma {
			break
		}
		p.advance()
	}
	p.expect(tRBrace)
	return n
}

// rawQuoted reads a quoted CSS-style literal at s[i] (no interpolation).
func rawQuoted(s string, i, stop int) (string, int, bool) {
	q := s[i]
	var b strings.Builder
	for j := i + 1; j < stop; j++ {
		c := s[j]
		switch {
		case c == q:
			return b.String(), j + 1, true
		case c == '\\' && j+1 < stop:
			j++
			b.WriteByte(s[j])
		default:
			b.WriteByte(c)
		}
	}
	return "", stop, false
}

var (
	identRE     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	cssNumberRE = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?([A-Za-z]+|%)?$`)
	flagRE      = regexp.MustCompile(`^(?s)(.*\S)\s+([iIsS])$`)
)

// makeHole classifies the unquoted text src[start:end] of a selector or
// construction value (R-SESSEL-202 and its fallback).
func (p *parser) makeHole(start, end int, attrFlag bool) *selHole {
	raw := p.lx.src[start:end]
	h := &selHole{}
	// trim, tracking the offset of the trimmed text
	for start < end && isSpace(p.lx.src[start]) {
		start++
	}
	for end > start && isSpace(p.lx.src[end-1]) {
		end--
	}
	raw = p.lx.src[start:end]
	if attrFlag {
		if m := flagRE.FindStringSubmatch(raw); m != nil && !cssNumberRE.MatchString(raw) {
			h.flag = " " + m[2]
			end = start + len(m[1])
			raw = m[1]
		}
	}
	h.raw = raw
	switch {
	case identRE.MatchString(raw):
		h.ident = raw
		h.expr = &identNode{base: base{start}, name: raw}
		if reserved[raw] && raw != "true" && raw != "false" && raw != "null" {
			h.expr = nil
		} else if raw == "true" || raw == "false" || raw == "null" {
			h.ident = ""
			h.expr = &litNode{base: base{start}, val: map[string]Value{"true": true, "false": false, "null": nil}[raw]}
		}
	case cssNumberRE.MatchString(raw):
		h.number = true
	case raw == "":
	default:
		h.expr = p.tryExpr(start, end)
	}
	return h
}

// tryExpr parses src[start:end] as an expression, returning nil when it is
// not one (the raw-CSS fallback).
func (p *parser) tryExpr(start, end int) (n node) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(*ParseError); !ok {
				panic(r)
			}
			n = nil
		}
	}()
	return p.subExpr(start, end, "empty expression")
}
