package dom

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// PageLove's document model (live observation LO-15, 2026-09-29). PageLove
// does not run the HTML5 tree-construction algorithm. It builds the tree
// straight from the tokens:
//
//   - no implied elements: a stored "<p>hi</p>" has no html, head or body,
//     and "<table><tr>" has no tbody (":root" is the first top-level
//     element; "table > tr" matches);
//   - no implied end tags and no reparenting: "<p>a<div>b" nests the div in
//     the p, "<li>a<li>b" nests the second li, text inside <table> stays
//     where it was written, misnested formatting is closed at the first
//     matching end tag;
//   - an end tag closes the nearest open element of that name and
//     everything opened after it; an end tag with no open element of that
//     name is dropped;
//   - "<x/>" is an empty element for every tag name, void elements never
//     have content, and elements still open at the end are closed there;
//   - script, style, xmp, iframe, noembed, noframes and plaintext hold raw
//     text; textarea and title hold escapable text; noscript holds markup;
//   - only complete references ("&amp;", "&#60;", "&copy;") are decoded, in
//     text and in quoted attribute values; unquoted attribute values are
//     kept as written; "<?…>" and "</ …>" are text; doctype ids are dropped;
//   - attribute names are lower-cased and the first of duplicates wins;
//     element names are lower-cased and there is no SVG/MathML case
//     adjustment (every element is in the HTML namespace).
//
// The tokenizer is x/net/html's; this file only replaces the tree builder
// and the few token rules above. Because every element comes from a start
// tag, spans are exact and recorded while building.

var voidElements = map[string]bool{
	"area": true, "base": true, "basefont": true, "bgsound": true, "br": true,
	"col": true, "embed": true, "frame": true, "hr": true, "img": true,
	"input": true, "keygen": true, "link": true, "meta": true, "param": true,
	"source": true, "track": true, "wbr": true,
}

// IsVoid reports whether an HTML element name is a void element.
func IsVoid(name string) bool { return voidElements[name] }

// rawTextElements hold unescaped text up to their end tag.
var rawTextElements = map[string]bool{
	"script": true, "style": true, "xmp": true, "iframe": true,
	"noembed": true, "noframes": true, "plaintext": true,
}

type openElement struct {
	n     *html.Node
	start int // offset of the start tag
	inner int // offset just after the start tag
}

type treeBuilder struct {
	src   string
	doc   *html.Node
	stack []openElement
	spans map[*html.Node]Span
}

func (b *treeBuilder) top() *html.Node {
	if len(b.stack) == 0 {
		return b.doc
	}
	return b.stack[len(b.stack)-1].n
}

// close pops the stack down to (and including) index i; the elements end
// at offset innerEnd with an end tag ending at end.
func (b *treeBuilder) close(i, innerEnd, end int, explicit bool) {
	for j := len(b.stack) - 1; j >= i; j-- {
		o := b.stack[j]
		sp := Span{Start: o.start, InnerStart: o.inner, InnerEnd: innerEnd, End: innerEnd}
		if j == i && explicit {
			sp.End, sp.ExplicitEnd = end, true
		}
		b.spans[o.n] = sp
	}
	b.stack = b.stack[:i]
}

func (b *treeBuilder) text(s string) {
	if s == "" {
		return
	}
	p := b.top()
	if last := p.LastChild; last != nil && last.Type == html.TextNode {
		last.Data += s
		return
	}
	p.AppendChild(&html.Node{Type: html.TextNode, Data: s})
}

// BuildTree parses src with PageLove's model and returns the document node
// and the exact source span of every element.
func BuildTree(src string) (*html.Node, Spans) {
	b := &treeBuilder{src: src, doc: &html.Node{Type: html.DocumentNode}, spans: map[*html.Node]Span{}}
	z := html.NewTokenizer(strings.NewReader(src))
	off := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		raw := string(z.Raw())
		start, end := off, off+len(raw)
		off = end
		switch tt {
		case html.TextToken:
			if p := b.top(); p.Type == html.ElementNode && rawTextElements[p.Data] {
				b.text(raw)
			} else {
				b.text(DecodeReferences(raw))
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			n := &html.Node{Type: html.ElementNode, Data: string(name), DataAtom: atom.Lookup(name), Attr: tagAttributes(raw, len(name))}
			b.top().AppendChild(n)
			if tt == html.SelfClosingTagToken || voidElements[n.Data] {
				b.spans[n] = Span{Start: start, InnerStart: end, InnerEnd: end, End: end, ExplicitEnd: true, NoContent: true}
				if n.Data == "noscript" || rawTextElements[n.Data] || n.Data == "textarea" || n.Data == "title" {
					z.NextIsNotRawText()
				}
				continue
			}
			if n.Data == "noscript" {
				z.NextIsNotRawText()
			}
			b.stack = append(b.stack, openElement{n: n, start: start, inner: end})
		case html.EndTagToken:
			name, _ := z.TagName()
			for i := len(b.stack) - 1; i >= 0; i-- {
				if b.stack[i].n.Data == string(name) {
					b.close(i, start, end, true)
					break
				}
			}
		case html.CommentToken:
			if strings.HasPrefix(raw, "<?") || strings.HasPrefix(raw, "</") {
				b.text(raw)
				continue
			}
			b.top().AppendChild(&html.Node{Type: html.CommentNode, Data: commentData(raw)})
		case html.DoctypeToken:
			name := "html"
			if f := strings.Fields(string(z.Text())); len(f) > 0 {
				name = strings.ToLower(f[0])
			}
			b.top().AppendChild(&html.Node{Type: html.DoctypeNode, Data: name})
		}
	}
	b.close(0, len(src), len(src), false)
	return b.doc, Spans{By: b.spans, Clean: true}
}

// commentData is a comment's text exactly as written between its
// delimiters (x/net/html would unescape references in it).
func commentData(raw string) string {
	if d, ok := strings.CutPrefix(raw, "<!--"); ok {
		for _, end := range []string{"-->", "--!>"} {
			if s, ok := strings.CutSuffix(d, end); ok {
				return s
			}
		}
		if d == ">" || d == "->" {
			return ""
		}
		return d // unterminated at the end of the source
	}
	d := strings.TrimPrefix(raw, "<!")
	return strings.TrimSuffix(d, ">")
}

// tagAttributes scans the attributes of a raw start tag (after "<" and the
// name) with the tokenizer's attribute rules, keeping unquoted values as
// written and decoding complete references in quoted ones.
func tagAttributes(raw string, nameLen int) []html.Attribute {
	s := raw[1+nameLen:]
	var attrs []html.Attribute
	seen := map[string]bool{}
	space := func(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r' }
	i := 0
	for i < len(s) {
		for i < len(s) && (space(s[i]) || s[i] == '/') {
			i++
		}
		if i >= len(s) || s[i] == '>' {
			break
		}
		j := i + 1 // a leading '=' belongs to the name
		for j < len(s) && !space(s[j]) && s[j] != '/' && s[j] != '>' && s[j] != '=' {
			j++
		}
		name := strings.ToLower(s[i:j])
		i = j
		for i < len(s) && space(s[i]) {
			i++
		}
		val := ""
		if i < len(s) && s[i] == '=' {
			i++
			for i < len(s) && space(s[i]) {
				i++
			}
			if i < len(s) && (s[i] == '"' || s[i] == '\'') {
				q := s[i]
				k := strings.IndexByte(s[i+1:], q)
				if k < 0 {
					val, i = DecodeReferences(s[i+1:]), len(s)
				} else {
					val, i = DecodeReferences(s[i+1:i+1+k]), i+2+k
				}
			} else {
				k := i
				for k < len(s) && !space(s[k]) && s[k] != '>' {
					k++
				}
				val, i = s[i:k], k
			}
		}
		if !seen[name] {
			seen[name] = true
			attrs = append(attrs, html.Attribute{Key: name, Val: val})
		}
	}
	return attrs
}

// DecodeReferences decodes complete character references ("&amp;",
// "&#60;", "&#x3c;", "&copy;"); anything else, including legacy references
// without a semicolon, stays as written (LO-15).
func DecodeReferences(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '&' {
			if j := strings.IndexByte(s[i+1:], ';'); j > 0 && j <= 32 {
				ref := s[i : i+j+2]
				if dec, ok := reference(ref); ok {
					b.WriteString(dec)
					i += len(ref)
					continue
				}
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func reference(ref string) (string, bool) {
	name := ref[1 : len(ref)-1]
	if name[0] == '#' {
		digits := name[1:]
		hex := len(digits) > 0 && (digits[0] == 'x' || digits[0] == 'X')
		if hex {
			digits = digits[1:]
		}
		if digits == "" {
			return "", false
		}
		for _, c := range digits {
			if !(c >= '0' && c <= '9' || hex && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')) {
				return "", false
			}
		}
		return html.UnescapeString(ref), true
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return "", false
		}
	}
	dec := html.UnescapeString(ref)
	if dec == ref || strings.HasSuffix(dec, ";") && name != "semi" {
		return "", false // unknown, or only a prefix of the name is a reference
	}
	return dec, true
}
