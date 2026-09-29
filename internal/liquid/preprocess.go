package liquid

import (
	"encoding/hex"
	"fmt"
	"html"
	"regexp"
	"strings"
)

// Template preprocessing. The library compiles what this produces; the
// rewrites carry PageLove semantics the library cannot express (decision
// 0002, "Source preprocessing"):
//
//   - `{{ x }}` becomes `{% pl_out x %}` where markup may appear and
//     `{% pl_out_attr x %}` anywhere else (inside a tag, a comment or a
//     raw-text element), so each output expression degrades on its own:
//     an inline Error item, or nothing (R-LIQ-200, R-LIQ-201). `{% echo %}`
//     is treated the same way.
//   - Expressions in outputs and tags go through the expression rewriter
//     (expr.go).
//   - `{% liquid %}` is expanded into one tag per line and `{% # … %}` is
//     dropped (R-LIQ-70).
//   - An unterminated `{{` or `{%`, or an unterminated raw or comment block,
//     is a syntax error instead of literal text (R-LIQ-78).
//
// Trim markers are carried over, the content of raw and comment blocks is
// left untouched, and every rewritten token keeps its newline count, so
// line numbers in errors match the author's source.

// templateToken finds outputs, inline comments and tags. The tag
// alternative is the library's own pattern, so both scanners agree on
// where tags are; outputs may span lines, which the library's pattern does
// not allow.
var templateToken = regexp.MustCompile(`(?s:\{\{(-?)(.*?)(-?)\}\})` +
	`|\{%(-?)\s*#(?:[^%]|%[^}])*?(-?)%\}` +
	`|\{%(-?)\s*(\w+)(?:\s+((?:[^%]|%[^}])+?))?\s*(-?)%\}`)

type preprocessor struct {
	src      string
	out      strings.Builder
	ctx      *markupContext
	verbatim string // "raw" or "comment" inside such a block
	openLine int    // line where the verbatim block opened
	refs     bool   // decode character references inside Liquid markup

	lineOff, lineNo int // line cursor: src[:lineOff] ends on line lineNo
}

// preprocess rewrites a template for compilation.
func preprocess(src string, host Host) (string, error) {
	p := &preprocessor{src: src, ctx: newMarkupContext(host), refs: host.Tag != "" && !host.XML}
	last := 0
	for _, m := range templateToken.FindAllStringSubmatchIndex(src, -1) {
		if err := p.text(last, m[0]); err != nil {
			return "", err
		}
		tok := src[m[0]:m[1]]
		group := func(i int) string {
			if m[2*i] < 0 {
				return ""
			}
			return src[m[2*i]:m[2*i+1]]
		}
		switch {
		case m[2] >= 0: // {{ output }}
			if p.verbatim != "" {
				p.out.WriteString(tok)
				break
			}
			p.output(tok, group(1), strings.TrimSpace(group(2)), group(3))
		case m[8] >= 0: // {% # comment %}
			if p.verbatim != "" {
				p.out.WriteString(tok)
				break
			}
			p.empty(tok, group(4), group(5))
		default: // {% tag args %}
			// The optional argument group can swallow the right trim
			// marker (`{% endfor -%}`); read it from the source, as the
			// library does.
			args, r := group(8), ""
			if len(tok) >= 5 && tok[len(tok)-3] == '-' {
				r, args = "-", strings.TrimSuffix(strings.TrimSpace(args), "-")
			}
			if err := p.tag(tok, group(6), group(7), strings.TrimSpace(args), r, p.line(m[0])); err != nil {
				return "", err
			}
		}
		last = m[1]
	}
	if err := p.text(last, len(src)); err != nil {
		return "", err
	}
	if p.verbatim != "" {
		return "", &Error{Kind: KindTemplate, Line: p.openLine, Message: fmt.Sprintf("unterminated %q block", p.verbatim)}
	}
	return p.out.String(), nil
}

// line returns the 1-based line of a source offset. Offsets are asked for
// in increasing order, so a cursor keeps this linear.
func (p *preprocessor) line(offset int) int {
	if offset < p.lineOff {
		p.lineOff, p.lineNo = 0, 1
	}
	if p.lineNo == 0 {
		p.lineNo = 1
	}
	p.lineNo += strings.Count(p.src[p.lineOff:offset], "\n")
	p.lineOff = offset
	return p.lineNo
}

// text copies literal text, feeding it to the markup context. Outside raw
// and comment blocks a leftover delimiter means an unterminated output or
// tag.
func (p *preprocessor) text(from, to int) error {
	s := p.src[from:to]
	if p.verbatim == "" {
		for _, d := range []string{"{{", "{%"} {
			if i := strings.Index(s, d); i >= 0 {
				what := "output"
				if d == "{%" {
					what = "tag"
				}
				return &Error{Kind: KindTemplate, Line: p.line(from + i), Message: fmt.Sprintf("unterminated %s %q", what, firstLine(clipString(s[i:], 40)))}
			}
		}
	}
	if p.verbatim != "comment" {
		p.ctx.feed(s)
	}
	p.out.WriteString(s)
	return nil
}

// emit writes a tag, padded with the newlines the original token had.
func (p *preprocessor) emit(orig, l, body, r string) {
	pad := strings.Count(orig, "\n") - strings.Count(body, "\n")
	p.out.WriteString("{%" + l + " " + body + strings.Repeat("\n", max(pad, 0)) + " " + r + "%}")
}

// empty replaces a token that renders nothing, keeping its trim markers and
// newlines.
func (p *preprocessor) empty(orig, l, r string) {
	if l == "" && r == "" && !strings.Contains(orig, "\n") {
		return
	}
	p.emit(orig, l, "comment", "")
	p.out.WriteString("{% endcomment " + r + "%}")
}

// decode decodes HTML character references in an output expression or a
// tag's arguments where the document's HTML parser would decode them, as
// live PageLove does for a template in an HTML document: `{{ "a &amp; b" }}`
// sees `a & b` (PageLove stores a `&` in Liquid code that way), and
// `{% if n &gt; 0 %}` compares. Literal text outside Liquid markup is left
// as written (R-LIQ-13, live 2026-09-29).
func (p *preprocessor) decode(s string) string {
	if p.refs && strings.IndexByte(s, '&') >= 0 && p.ctx.decodesRefs() {
		return html.UnescapeString(s)
	}
	return s
}

// output converts an output expression (or echo) to pl_out/pl_out_attr.
func (p *preprocessor) output(orig, l, expr, r string) {
	expr = p.decode(expr)
	if expr == "" {
		p.empty(orig, l, r)
		return
	}
	name := "pl_out"
	if !p.ctx.markupAllowed() {
		name = "pl_out_attr"
	}
	p.emit(orig, l, name+" "+encodeArgs(rewriteCond(expr)), r)
}

// encodeArgs protects an expression that contains the tag terminator.
func encodeArgs(s string) string {
	if strings.Contains(s, "%}") {
		return "#" + hex.EncodeToString([]byte(s))
	}
	return s
}

// decodeArgs undoes encodeArgs.
func decodeArgs(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		if b, err := hex.DecodeString(s[1:]); err == nil {
			return string(b)
		}
	}
	return s
}

func (p *preprocessor) tag(orig, l, name, args, r string, line int) error {
	if p.verbatim != "" {
		if name == "end"+p.verbatim {
			p.verbatim = ""
		}
		p.out.WriteString(orig)
		return nil
	}
	switch name {
	case "raw", "comment":
		p.verbatim, p.openLine = name, line
		p.out.WriteString(orig)
		return nil
	case "echo":
		p.output(orig, l, args, r)
		return nil
	case "liquid":
		return p.liquidTag(orig, l, args, r, line)
	}
	body := name
	if args != "" {
		body += " " + rewriteTagArgs(name, p.decode(args))
	}
	p.emit(orig, l, body, r)
	return nil
}

// rewriteTagArgs applies the expression rewriter to a tag's arguments.
func rewriteTagArgs(name, args string) string {
	switch name {
	case "if", "elsif", "unless", "case":
		return rewriteCond(args)
	case "when":
		return rewriteWhen(args)
	case "assign":
		return rewriteAssign(args)
	case "for", "tablerow":
		return rewriteLoop(args)
	}
	return args
}

// liquidTag expands `{% liquid %}`: each non-empty line that is not a `#`
// comment is a tag without delimiters.
func (p *preprocessor) liquidTag(orig, l, args, r string, line int) error {
	var lines []string
	for _, ln := range strings.Split(args, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" && !strings.HasPrefix(ln, "#") {
			lines = append(lines, ln)
		}
	}
	for i, ln := range lines {
		name, rest, _ := strings.Cut(ln, " ")
		if strings.IndexFunc(name, func(c rune) bool {
			return !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9')
		}) >= 0 {
			return &Error{Kind: KindTemplate, Line: line, Message: fmt.Sprintf("invalid line in liquid tag: %q", clipString(ln, 60))}
		}
		tl := ""
		if i == 0 {
			tl, l = l, ""
		}
		synthetic := "{%" + tl + " " + ln + " %}"
		if err := p.tag(synthetic, tl, name, strings.TrimSpace(rest), "", line); err != nil {
			return err
		}
	}
	// The right trim marker and the tag's newlines go on a trailing empty
	// construct.
	if p.verbatim == "" {
		p.empty(orig, l, r)
	}
	return nil
}

func clipString(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
