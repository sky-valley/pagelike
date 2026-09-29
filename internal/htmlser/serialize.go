// Package htmlser serializes golang.org/x/net/html trees with the WHATWG
// "serializing HTML fragments" algorithm, matching html5ever's serializer
// (and browsers' outerHTML) instead of html.Render's Go-specific choices.
//
// Differences from html.Render that this package removes:
//
//   - void elements end in ">" not "/>";
//   - text escapes only & < > and U+00A0 (&nbsp;); ' and " stay literal;
//   - attribute values escape & " U+00A0 (and, per the 2025 spec change that
//     html5ever >= 0.3x follows, < and >) using &quot; not &#34;;
//   - comments are written verbatim (Render escapes '&' inside them);
//   - the DOCTYPE is written as "<!DOCTYPE name>" (public/system ids dropped);
//   - no extra newline is inserted after <pre>/<textarea>/<listing>;
//   - noscript is raw text unless Options.ScriptingDisabled.
package htmlser

import (
	"bufio"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// Options tweak the serializer. The zero value matches html5ever 0.39.
type Options struct {
	// LegacyAttrEscaping leaves < and > unescaped in attribute values (the
	// pre-2025 WHATWG algorithm, html5ever <= 0.2x, older browsers).
	LegacyAttrEscaping bool
	// ScriptingDisabled serializes <noscript> children as escaped text
	// (html5ever and x/net/html both parse with scripting enabled by default).
	ScriptingDisabled bool
	// PreserveLeadingNewline emits an extra LF after <pre>, <textarea> and
	// <listing> when their content starts with LF (the pre-2016 spec step
	// that html.Render still performs). html5ever and browsers omit it, which
	// makes their output lose one leading newline per parse/serialize cycle;
	// use this for *storage* so repeated writes are lossless.
	PreserveLeadingNewline bool
	// OmitImplied suppresses the start and end tags of html/head/body
	// elements that an HTML5 tree builder created implicitly. An implied
	// element is omitted only while it still has no attributes; an implied,
	// empty <head> disappears entirely. (PageLove's model implies none.)
	OmitImplied map[*html.Node]bool
	// PageLove writes PageLove's form (live observation LO-15): empty
	// attributes bare ("<input disabled>"), text escaping only & < >
	// (U+00A0 is written literally), and <noscript> content as markup.
	// Attribute values escape & " < >: PageLove leaves < and > literal
	// there, which lets `<noscript><img alt="</noscript><img onerror=…>">`
	// turn into live markup when a browser (which reads noscript as raw
	// text) parses the stored page; escaping them, as the WHATWG
	// serializer does since 2025, closes that (a deliberate difference,
	// decisions-2026-09-29/serialization.md). The other options do not
	// apply.
	PageLove bool
}

// Render writes n (outerHTML for an element; the children for a document).
func Render(w io.Writer, n *html.Node, o Options) error {
	bw, ok := w.(*bufio.Writer)
	if !ok {
		bw = bufio.NewWriter(w)
	}
	s := serializer{w: bw, o: o}
	s.node(n)
	if s.err != nil {
		return s.err
	}
	return bw.Flush()
}

// String is a convenience wrapper around Render.
func String(n *html.Node, o Options) string {
	var b strings.Builder
	_ = Render(&b, n, o)
	return b.String()
}

// InnerHTML serializes the children of n.
func InnerHTML(n *html.Node, o Options) string {
	var b strings.Builder
	bw := bufio.NewWriter(&b)
	s := serializer{w: bw, o: o}
	s.children(n)
	bw.Flush()
	return b.String()
}

type serializer struct {
	w   *bufio.Writer
	o   Options
	err error
}

func (s *serializer) str(v string) {
	if s.err == nil {
		_, s.err = s.w.WriteString(v)
	}
}

var voidElements = map[string]bool{
	"area": true, "base": true, "basefont": true, "bgsound": true, "br": true,
	"col": true, "embed": true, "frame": true, "hr": true, "img": true,
	"input": true, "keygen": true, "link": true, "meta": true, "param": true,
	"source": true, "track": true, "wbr": true,
}

func (s *serializer) node(n *html.Node) {
	switch n.Type {
	case html.DocumentNode:
		s.children(n)
	case html.DoctypeNode:
		s.str("<!DOCTYPE " + n.Data + ">")
	case html.CommentNode:
		s.str("<!--" + n.Data + "-->")
	case html.TextNode:
		if s.o.PageLove {
			if rawTextParent(n.Parent, false) {
				s.str(n.Data)
			} else {
				s.str(escapePageLove(n.Data, false))
			}
			return
		}
		if rawTextParent(n.Parent, !s.o.ScriptingDisabled) {
			s.str(n.Data)
		} else {
			s.str(escapeText(n.Data))
		}
	case html.RawNode:
		s.str(n.Data)
	case html.ElementNode:
		s.element(n)
	}
}

func (s *serializer) children(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		s.node(c)
	}
}

func (s *serializer) element(n *html.Node) {
	if s.o.PageLove {
		s.str("<" + n.Data)
		for _, a := range n.Attr {
			if a.Val == "" {
				s.str(" " + attrName(a))
			} else {
				s.str(" " + attrName(a) + `="` + escapePageLove(a.Val, true) + `"`)
			}
		}
		s.str(">")
		if voidElements[n.Data] {
			return
		}
		s.children(n)
		s.str("</" + n.Data + ">")
		return
	}
	if s.o.OmitImplied[n] && len(n.Attr) == 0 {
		if n.Data == "head" && n.FirstChild == nil {
			return
		}
		s.children(n)
		return
	}
	s.str("<" + n.Data)
	for _, a := range n.Attr {
		s.str(" " + attrName(a) + `="` + escapeAttr(a.Val, !s.o.LegacyAttrEscaping) + `"`)
	}
	s.str(">")
	if n.Namespace == "" && voidElements[n.Data] {
		return
	}
	if s.o.PreserveLeadingNewline && n.Namespace == "" && (n.Data == "pre" || n.Data == "textarea" || n.Data == "listing") {
		if c := n.FirstChild; c != nil && c.Type == html.TextNode && strings.HasPrefix(c.Data, "\n") {
			s.str("\n")
		}
	}
	s.children(n)
	s.str("</" + n.Data + ">")
}

// attrName follows the spec's "serialized name" rules. x/net/html stores the
// adjusted foreign attributes as Namespace "xml"/"xmlns"/"xlink" + local Key.
func attrName(a html.Attribute) string {
	switch a.Namespace {
	case "":
		return a.Key
	case "xmlns":
		if a.Key == "xmlns" {
			return "xmlns"
		}
		return "xmlns:" + a.Key
	default: // xml, xlink
		return a.Namespace + ":" + a.Key
	}
}

func rawTextParent(p *html.Node, scripting bool) bool {
	if p == nil || p.Type != html.ElementNode || p.Namespace != "" {
		return false
	}
	switch p.Data {
	case "style", "script", "xmp", "iframe", "noembed", "noframes", "plaintext":
		return true
	case "noscript":
		return scripting
	}
	return false
}

func escapeText(v string) string {
	if !strings.ContainsAny(v, "&<> ") {
		return v
	}
	var b strings.Builder
	for _, r := range v {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case ' ':
			b.WriteString("&nbsp;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// escapePageLove escapes text (& < >) or an attribute value (& " < >).
func escapePageLove(v string, attr bool) string {
	special := "&<>"
	if attr {
		special = "&\"<>"
	}
	if !strings.ContainsAny(v, special) {
		return v
	}
	var b strings.Builder
	for _, r := range v {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '"' && attr:
			b.WriteString("&quot;")
		case r == '<':
			b.WriteString("&lt;")
		case r == '>':
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func escapeAttr(v string, ltgt bool) string {
	if !strings.ContainsAny(v, "&\"<> ") {
		return v
	}
	var b strings.Builder
	for _, r := range v {
		switch {
		case r == '&':
			b.WriteString("&amp;")
		case r == '"':
			b.WriteString("&quot;")
		case r == ' ':
			b.WriteString("&nbsp;")
		case r == '<' && ltgt:
			b.WriteString("&lt;")
		case r == '>' && ltgt:
			b.WriteString("&gt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
