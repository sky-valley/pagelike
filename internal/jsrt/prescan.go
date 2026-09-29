package jsrt

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The engine's module loader receives only the specifier, not the import
// attributes (ADR 0001 §Schema imports), so the worker checks the forms of
// a module's imports with a lexical pre-scan: it tokenizes the source
// (comments, strings, template literals and regular expressions are
// skipped) and reads every import declaration, re-export and import() call.
// The normalizer is the enforcement point: it only ever admits the schema
// URLs this scan approved, so a scan that misreads odd source can only
// reject, never widen what a module may load.

type importDecl struct {
	Spec        string
	DefaultOnly bool // `import X from "…"`
	Attrs       map[string]string
	HasAttrs    bool
	Assert      bool // legacy `assert { … }`
}

type prescanResult struct {
	Imports []importDecl
	// Problem is the first import that is not allowed, with a reason.
	Problem       string
	ProblemSpec   string
	DynamicImport bool
}

// schemaImports returns the specifiers of the valid schema imports.
func (p *prescanResult) schemaImports() []string {
	var out []string
	for _, im := range p.Imports {
		if isSchemaImport(im) {
			out = append(out, im.Spec)
		}
	}
	return out
}

func isSchemaImport(im importDecl) bool {
	if !im.DefaultOnly || im.Assert || !im.HasAttrs || len(im.Attrs) != 1 || im.Attrs["type"] != SchemaImportType {
		return false
	}
	s := strings.ToLower(im.Spec)
	return !strings.HasPrefix(s, "data:") && !strings.HasPrefix(s, "blob:")
}

func prescan(src string) *prescanResult {
	res := &prescanResult{}
	toks := tokenize(src)
	fail := func(spec, why string) {
		if res.Problem == "" {
			res.Problem, res.ProblemSpec = why, spec
		}
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.kind != tkIdent {
			continue
		}
		prevDot := i > 0 && toks[i-1].kind == tkPunct && toks[i-1].text == "."
		if prevDot {
			continue
		}
		switch t.text {
		case "import":
			if i+1 < len(toks) && toks[i+1].text == "(" {
				res.DynamicImport = true
				fail("import()", "dynamic import() is not allowed")
				continue
			}
			if i+1 < len(toks) && toks[i+1].text == "." { // import.meta
				continue
			}
			im, next := parseImport(toks, i+1)
			i = next - 1
			res.Imports = append(res.Imports, im)
			if !isSchemaImport(im) {
				switch {
				case im.Assert:
					fail(im.Spec, "the legacy assert syntax is not supported")
				case im.HasAttrs && im.Attrs["type"] == SchemaImportType && !im.DefaultOnly:
					fail(im.Spec, "only the default binding of a schema import is defined")
				case im.HasAttrs && im.Attrs["type"] == SchemaImportType:
					fail(im.Spec, "not a schema URL")
				default:
					fail(im.Spec, "only schema imports (with { type: \""+SchemaImportType+"\" }) are allowed")
				}
			}
		case "export":
			// export * from "x"; export * as n from "x"; export { a } from "x"
			j := i + 1
			switch {
			case j < len(toks) && toks[j].text == "*":
				j++
				if j+1 < len(toks) && toks[j].text == "as" {
					j += 2
				}
			case j < len(toks) && toks[j].text == "{":
				for j < len(toks) && toks[j].text != "}" {
					j++
				}
				j++
			default:
				continue
			}
			if j+1 < len(toks) && toks[j].text == "from" && toks[j+1].kind == tkString {
				fail(toks[j+1].str, "re-exports (export … from) are not allowed")
			}
		}
	}
	return res
}

// parseImport reads an import declaration starting after `import`.
func parseImport(toks []token, i int) (importDecl, int) {
	var im importDecl
	defaultBinding, other := false, false
	if i < len(toks) && toks[i].kind == tkString { // import "x"
		im.Spec = toks[i].str
		i++
	} else {
		for i < len(toks) {
			t := toks[i]
			if t.kind == tkIdent && t.text == "from" && i+1 < len(toks) && toks[i+1].kind == tkString {
				im.Spec = toks[i+1].str
				i += 2
				break
			}
			switch {
			case t.text == "{" || t.text == "*":
				other = true
				if t.text == "{" {
					for i < len(toks) && toks[i].text != "}" {
						i++
					}
				}
			case t.kind == tkIdent && !other && !defaultBinding:
				defaultBinding = true
			case t.text == ",":
			case t.kind == tkIdent && (t.text == "as"):
			case t.kind == tkIdent:
			default:
				return im, i
			}
			i++
		}
	}
	im.DefaultOnly = defaultBinding && !other
	if i < len(toks) && toks[i].kind == tkIdent && (toks[i].text == "with" || toks[i].text == "assert") && i+1 < len(toks) && toks[i+1].text == "{" {
		im.Assert = toks[i].text == "assert"
		im.HasAttrs = true
		im.Attrs = map[string]string{}
		i += 2
		for i < len(toks) && toks[i].text != "}" {
			k := toks[i]
			if (k.kind == tkIdent || k.kind == tkString) && i+2 < len(toks) && toks[i+1].text == ":" && toks[i+2].kind == tkString {
				key := k.text
				if k.kind == tkString {
					key = k.str
				}
				im.Attrs[key] = toks[i+2].str
				i += 3
				continue
			}
			i++
		}
		if i < len(toks) {
			i++
		}
	}
	return im, i
}

type tokKind int

const (
	tkIdent tokKind = iota
	tkString
	tkPunct
	tkOther
)

type token struct {
	kind tokKind
	text string
	str  string // decoded string literal value
}

// tokenize is a JavaScript tokenizer precise enough to find import and
// export declarations: it skips comments, strings, template literals (with
// nested substitutions) and regular expression literals, telling a regular
// expression from a division by the previous significant token.
func tokenize(src string) []token {
	var toks []token
	var tmplDepth []int // brace depth at which each open ${ … } ends
	braces := 0
	regexAllowed := true
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
			continue
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return toks
			}
			i += end + 4
			continue
		case c == '"' || c == '\'':
			s, n := readString(src[i:], c)
			toks = append(toks, token{kind: tkString, text: src[i : i+n], str: s})
			i += n
			regexAllowed = false
			continue
		case c == '`':
			n := skipTemplate(src[i+1:])
			if n < 0 { // "${": the substitution continues as code
				j := i + 1
				j += templateUntilSubst(src[j:])
				tmplDepth = append(tmplDepth, braces)
				braces++
				i = j
				toks = append(toks, token{kind: tkOther, text: "`"})
				regexAllowed = true
				continue
			}
			toks = append(toks, token{kind: tkOther, text: "`"})
			i += 1 + n
			regexAllowed = false
			continue
		case c == '}' && len(tmplDepth) > 0 && braces-1 == tmplDepth[len(tmplDepth)-1]:
			// end of a template substitution: continue the template
			braces--
			tmplDepth = tmplDepth[:len(tmplDepth)-1]
			rest := src[i+1:]
			n := skipTemplate(rest)
			if n < 0 {
				j := templateUntilSubst(rest)
				tmplDepth = append(tmplDepth, braces)
				braces++
				i += 1 + j
				regexAllowed = true
				continue
			}
			i += 1 + n
			regexAllowed = false
			continue
		case c == '/' && regexAllowed:
			n := skipRegex(src[i:])
			toks = append(toks, token{kind: tkOther, text: "/re/"})
			i += n
			regexAllowed = false
			continue
		case isIdentStart(src, i):
			j := i
			for j < len(src) {
				r, size := utf8.DecodeRuneInString(src[j:])
				if r == '$' || r == '_' || r == '\\' || unicode.IsLetter(r) || unicode.IsDigit(r) || r == 0x200c || r == 0x200d {
					j += size
					continue
				}
				break
			}
			word := src[i:j]
			toks = append(toks, token{kind: tkIdent, text: word})
			i = j
			regexAllowed = keywordBeforeExpr[word]
			continue
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			j := i + 1
			for j < len(src) && (isAlnum(src[j]) || src[j] == '.' || src[j] == '_') {
				j++
			}
			toks = append(toks, token{kind: tkOther, text: src[i:j]})
			i = j
			regexAllowed = false
			continue
		}
		// punctuation
		switch c {
		case '{':
			braces++
		case '}':
			braces--
		}
		toks = append(toks, token{kind: tkPunct, text: string(c)})
		i++
		regexAllowed = !(c == ')' || c == ']')
	}
	return toks
}

var keywordBeforeExpr = map[string]bool{
	"return": true, "typeof": true, "instanceof": true, "in": true, "of": true, "new": true, "delete": true,
	"void": true, "throw": true, "case": true, "do": true, "else": true, "yield": true, "await": true,
}

func isAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isIdentStart(src string, i int) bool {
	r, _ := utf8.DecodeRuneInString(src[i:])
	return r == '$' || r == '_' || r == '\\' || r == '#' || unicode.IsLetter(r)
}

func readString(s string, q byte) (string, int) {
	var b strings.Builder
	i := 1
	for i < len(s) {
		c := s[i]
		if c == q {
			return b.String(), i + 1
		}
		if c == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '\n':
			default:
				b.WriteByte(s[i+1])
			}
			i += 2
			continue
		}
		if c == '\n' {
			return b.String(), i
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), i
}

// skipTemplate returns the length up to and including the closing backtick,
// or -1 when a ${ substitution comes first.
func skipTemplate(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '`':
			return i + 1
		case '$':
			if i+1 < len(s) && s[i+1] == '{' {
				return -1
			}
		}
	}
	return len(s)
}

// templateUntilSubst returns the length up to and including the next "${".
func templateUntilSubst(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '$':
			if i+1 < len(s) && s[i+1] == '{' {
				return i + 2
			}
		}
	}
	return len(s)
}

func skipRegex(s string) int {
	inClass := false
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '\n':
			return i
		case '/':
			if !inClass {
				j := i + 1
				for j < len(s) && isAlnum(s[j]) {
					j++
				}
				return j
			}
		}
	}
	return len(s)
}
