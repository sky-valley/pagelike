// Package xmldom parses XML-family documents (RSS, Atom, SVG, sitemaps, any
// application/xml, text/xml or +xml type other than XHTML) into
// golang.org/x/net/html node trees, so the same selector engine, microdata
// and composition code run on HTML and XML, and serializes those trees back
// as well-formed XML (decision 0003 §3).
//
// Mapping onto html.Node:
//
//	element     Type=ElementNode, Data=qualified name as written ("atom:link",
//	            "Entry"), Namespace=resolved namespace URI or NoNamespace
//	            (never "", so selectors compare names case-sensitively and no
//	            code mistakes an XML element for an HTML one), DataAtom=0
//	attribute   Key=qualified name as written ("xmlns:e", "e:site"), Namespace=""
//	text        TextNode; text from a CDATA section has Namespace=CDATA so it
//	            is written back as CDATA (and survives dom.Clone)
//	comment     CommentNode (data verbatim)
//	<?pi ...?>  RawNode holding the exact source bytes (the XML declaration too)
//	<!DOCTYPE>  RawNode holding the exact source bytes
package xmldom

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// NoNamespace marks XML elements in no namespace. It is non-empty so that
// code which treats Namespace=="" as "HTML element" never mistakes XML.
const NoNamespace = "urn:x-pagelike:xml:no-namespace"

// CDATA is the Namespace of text nodes parsed from a CDATA section.
const CDATA = "urn:x-pagelike:xml:cdata"

// Options for Parse.
type Options struct {
	// Strict requires well-formed XML. The default is lenient, like
	// PageLove's documented example (`<p:stamp site>` has an attribute with
	// no value): valueless and unquoted attributes, HTML named entities and
	// mismatched end tags are tolerated.
	Strict bool
}

// Span locates an element in the source it was parsed from, with the same
// layout as dom.Span: src[Start:InnerStart] is the start tag,
// src[InnerStart:InnerEnd] the content, src[InnerEnd:End] the end tag.
type Span struct {
	Start, InnerStart, InnerEnd, End int
	// SelfClosing marks an empty-element tag (<entry/>), which has no
	// content region to insert into.
	SelfClosing bool
}

// Spans maps each element to its source span. Clean is false when the
// lenient parser had to repair the input (mismatched or missing end tags),
// in which case the spans must not be used for editing.
type Spans struct {
	By    map[*html.Node]Span
	Clean bool
}

// Parse parses src into a DocumentNode.
func Parse(src []byte, o Options) (*html.Node, error) {
	doc, _, err := parse(src, o, false)
	return doc, err
}

// ParseWithSpans parses src and records every element's source span.
func ParseWithSpans(src []byte, o Options) (*html.Node, Spans, error) {
	return parse(src, o, true)
}

func parse(src []byte, o Options, withSpans bool) (*html.Node, Spans, error) {
	d := xml.NewDecoder(bytes.NewReader(src))
	d.Strict = o.Strict
	if !o.Strict {
		d.Entity = xml.HTMLEntity
		d.AutoClose = nil
	}
	sp := Spans{Clean: true}
	if withSpans {
		sp.By = map[*html.Node]Span{}
	}
	root := &html.Node{Type: html.DocumentNode}
	type frame struct {
		n  *html.Node
		ns map[string]string // prefix -> URI declared on this element
		sp Span
	}
	stack := []frame{{n: root, ns: map[string]string{
		"xml":   "http://www.w3.org/XML/1998/namespace",
		"xmlns": "http://www.w3.org/2000/xmlns/",
	}}}
	resolve := func(prefix string) string {
		for i := len(stack) - 1; i >= 0; i-- {
			if u, ok := stack[i].ns[prefix]; ok {
				if u == "" {
					return NoNamespace
				}
				return u
			}
		}
		return NoNamespace
	}
	// closeTo pops frames above i, recording spans; only the frame at i+1
	// (the one the end tag named) gets an explicit end tag.
	closeTo := func(i, at, end int) {
		for len(stack) > i+1 {
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if withSpans {
				s := f.sp
				if len(stack) == i+1 {
					s.InnerEnd, s.End = at, end
				} else {
					s.InnerEnd, s.End = at, at
					sp.Clean = false
				}
				sp.By[f.n] = s
			}
		}
	}
	for {
		start := int(d.InputOffset())
		tok, err := d.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, Spans{}, err
		}
		end := int(d.InputOffset())
		raw := src[start:end]
		top := stack[len(stack)-1].n
		switch t := tok.(type) {
		case xml.StartElement:
			n := &html.Node{Type: html.ElementNode, Data: qname(t.Name)}
			decl := map[string]string{}
			for _, a := range t.Attr {
				switch {
				case a.Name.Space == "" && a.Name.Local == "xmlns":
					decl[""] = a.Value
				case a.Name.Space == "xmlns":
					decl[a.Name.Local] = a.Value
				}
				n.Attr = append(n.Attr, html.Attribute{Key: qname(a.Name), Val: a.Value})
			}
			if !o.Strict {
				fixValueless(n, raw)
			}
			f := frame{n: n, ns: decl, sp: Span{Start: start, InnerStart: end}}
			if bytes.HasSuffix(raw, []byte("/>")) {
				// encoding/xml reports <x/> as a start and an end token; the
				// end consumes no input.
				f.sp.SelfClosing = true
			}
			stack = append(stack, f)
			n.Namespace = resolve(t.Name.Space)
			top.AppendChild(n)
		case xml.EndElement:
			name := qname(t.Name)
			matched := false
			for i := len(stack) - 1; i > 0; i-- {
				if stack[i].n.Data == name {
					closeTo(i-1, start, end)
					matched = true
					break
				}
			}
			if !matched {
				sp.Clean = false // stray end tag: dropped from the tree
			}
		case xml.CharData:
			n := &html.Node{Type: html.TextNode, Data: string(t)}
			if bytes.HasPrefix(raw, []byte("<![CDATA[")) {
				n.Namespace = CDATA
			} else if c := top.LastChild; c != nil && c.Type == html.TextNode && c.Namespace != CDATA {
				c.Data += n.Data // merge adjacent text (entity boundaries)
				continue
			}
			top.AppendChild(n)
		case xml.Comment:
			top.AppendChild(&html.Node{Type: html.CommentNode, Data: string(t)})
		case xml.ProcInst, xml.Directive:
			top.AppendChild(&html.Node{Type: html.RawNode, Data: string(raw)})
		}
	}
	if len(stack) > 1 {
		if o.Strict {
			return nil, Spans{}, errors.New("xmldom: unclosed elements at EOF")
		}
		closeTo(0, len(src), len(src))
		sp.Clean = false
	}
	return root, sp, nil
}

func qname(n xml.Name) string {
	if n.Space != "" {
		return n.Space + ":" + n.Local
	}
	return n.Local
}

// fixValueless: encoding/xml (non-strict) gives a valueless attribute the
// attribute's own name as value (`site` -> site="site"). HTML and xml5ever
// give "". Detect that from the raw start tag and restore "".
func fixValueless(n *html.Node, raw []byte) {
	s := string(raw)
	for i, a := range n.Attr {
		if a.Val != a.Key {
			continue
		}
		idx := strings.Index(s, " "+a.Key)
		if idx < 0 {
			continue
		}
		rest := strings.TrimLeft(s[idx+1+len(a.Key):], " \t\r\n")
		if !strings.HasPrefix(rest, "=") {
			n.Attr[i].Val = ""
		}
	}
}

// fragmentRoot is the synthetic element ParseFragment wraps markup in.
const fragmentRoot = "pagelike-fragment"

// ParseFragment parses markup as XML content inserted as children of
// context: prefixes resolve against the namespace declarations in scope at
// context (including its default namespace), so a POSTed <entry> lands in
// the Atom namespace of its feed.
func ParseFragment(markup string, context *html.Node) ([]*html.Node, error) {
	if context == nil || context.Type != html.ElementNode {
		return nil, errors.New("xmldom: fragment parsing needs a context element")
	}
	var b strings.Builder
	b.WriteString("<" + fragmentRoot)
	for prefix, uri := range inScope(context) {
		name := "xmlns"
		if prefix != "" {
			name += ":" + prefix
		}
		b.WriteString(" " + name + `="` + escAttr(uri) + `"`)
	}
	b.WriteString(">" + markup + "</" + fragmentRoot + ">")
	doc, err := Parse([]byte(b.String()), Options{})
	if err != nil {
		return nil, err
	}
	wrap := doc.FirstChild
	if wrap == nil || wrap.Type != html.ElementNode || wrap.Data != fragmentRoot {
		return nil, errors.New("xmldom: fragment did not parse as element content")
	}
	var out []*html.Node
	for c := wrap.FirstChild; c != nil; {
		next := c.NextSibling
		wrap.RemoveChild(c)
		out = append(out, c)
		c = next
	}
	return out, nil
}

// inScope collects the namespace declarations visible at n (innermost wins).
func inScope(n *html.Node) map[string]string {
	out := map[string]string{}
	for e := n; e != nil; e = e.Parent {
		if e.Type != html.ElementNode {
			continue
		}
		for _, a := range e.Attr {
			prefix, ok := "", false
			switch {
			case a.Key == "xmlns":
				ok = true
			case strings.HasPrefix(a.Key, "xmlns:"):
				prefix, ok = a.Key[len("xmlns:"):], true
			}
			if _, seen := out[prefix]; ok && !seen {
				out[prefix] = a.Val
			}
		}
	}
	return out
}

// IsXML reports whether n belongs to a tree built by this package: an
// element whose namespace is neither HTML ("") nor the HTML parser's
// foreign-content markers ("svg", "math"), or a document, text or comment
// whose nearest element (or document element) is one.
func IsXML(n *html.Node) bool {
	for x := n; x != nil; x = x.Parent {
		switch x.Type {
		case html.ElementNode:
			return x.Namespace != "" && x.Namespace != "svg" && x.Namespace != "math"
		case html.DocumentNode:
			for c := x.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode {
					return IsXML(c)
				}
			}
			return false
		}
	}
	return false
}

// Render writes n as well-formed XML: a document writes its children, an
// element its outer XML. Empty elements self-close, names keep their case
// and prefixes, PIs and the doctype are written verbatim.
func Render(w io.Writer, n *html.Node) error {
	bw := bufio.NewWriter(w)
	writeNode(bw, n)
	return bw.Flush()
}

// String renders n (see Render).
func String(n *html.Node) string {
	var b strings.Builder
	_ = Render(&b, n)
	return b.String()
}

// InnerXML renders the children of n.
func InnerXML(n *html.Node) string {
	var b strings.Builder
	bw := bufio.NewWriter(&b)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		writeNode(bw, c)
	}
	bw.Flush()
	return b.String()
}

func writeNode(bw *bufio.Writer, n *html.Node) {
	switch n.Type {
	case html.DocumentNode:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			writeNode(bw, c)
		}
	case html.RawNode:
		bw.WriteString(n.Data)
	case html.DoctypeNode: // only in trees built by the HTML parser
		bw.WriteString("<!DOCTYPE " + n.Data + ">")
	case html.CommentNode:
		bw.WriteString("<!--" + n.Data + "-->")
	case html.TextNode:
		if n.Namespace == CDATA {
			bw.WriteString("<![CDATA[" + strings.ReplaceAll(n.Data, "]]>", "]]]]><![CDATA[>") + "]]>")
		} else {
			bw.WriteString(escText(n.Data))
		}
	case html.ElementNode:
		bw.WriteString("<" + n.Data)
		for _, a := range n.Attr {
			name := a.Key
			if a.Namespace != "" { // nodes built by the HTML parser
				name = a.Namespace + ":" + a.Key
			}
			bw.WriteString(" " + name + `="` + escAttr(a.Val) + `"`)
		}
		if n.FirstChild == nil {
			bw.WriteString("/>")
			return
		}
		bw.WriteString(">")
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			writeNode(bw, c)
		}
		bw.WriteString("</" + n.Data + ">")
	}
}

var textEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#xD;")
var attrEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", `"`, "&quot;", "\t", "&#x9;", "\n", "&#xA;", "\r", "&#xD;")

func escText(s string) string { return textEsc.Replace(s) }
func escAttr(s string) string { return attrEsc.Replace(s) }
