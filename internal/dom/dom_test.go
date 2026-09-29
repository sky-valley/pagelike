package dom

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/sky-valley/pagelike/internal/htmlser"
)

// fragCases: fragment parsing in the context of each element. In PageLove's
// model (LO-15) markup parses the same under every parent; only text-only
// parents (textarea, script) differ. Markup is not trimmed here.
var fragCases = []struct{ ctx, frag, want, note string }{
	{"tbody", "<tr><td>a</td></tr>", "<tr><td>a</td></tr>", "POST a row into <tbody> (pagelove-polls, pagelove-ats)"},
	{"table", "<tr><td>a</td></tr>", "<tr><td>a</td></tr>", "row into <table>: no tbody (live: table > tr matches)"},
	{"tr", "<td>a</td>", "<td>a</td>", ""},
	{"tbody", "<td>a</td>", "<td>a</td>", "cell into tbody: no tr is synthesized"},
	{"body", "<tr><td>a</td></tr>", "<tr><td>a</td></tr>", "row in body context: kept"},
	{"div", "<tr><td>a</td></tr>", "<tr><td>a</td></tr>", ""},
	{"ul", "<li>x</li>", "<li>x</li>", ""},
	{"select", "<option>o</option>", "<option>o</option>", ""},
	{"select", "<div>d</div><option>o</option>", "<div>d</div><option>o</option>", "customizable-select parsing change"},
	{"td", "<b>cell</b>", "<b>cell</b>", ""},
	{"template", "<tr><td>t</td></tr>", "<tr><td>t</td></tr>", ""},
	{"body", "just text", "just text", "text-only fragment"},
	{"body", "<li>a</li><li>b</li>", "<li>a</li><li>b</li>", "multiple top-level nodes"},
	{"body", "\n  <!-- c --> <p>x</p>\n", "\n  <!-- c --> <p>x</p>\n", "leading whitespace and comments"},
	{"html", "<body class=x><p>b</p></body>", "<body class=\"x\"><p>b</p></body>", "PUT replacing <body>: no head is synthesized"},
	{"body", "<body class=x><p>b</p></body>", "<body class=\"x\"><p>b</p></body>", "a <body> tag is an element like any other"},
	{"head", "<title>t</title><meta charset=utf-8>", "<title>t</title><meta charset=\"utf-8\">", ""},
	{"body", "<script>if (a < b && c) {}</script>", "<script>if (a < b && c) {}</script>", ""},
	{"body", "<circle r='1'/>", "<circle r=\"1\"></circle>", "same markup in HTML context: not SVG"},
	{"body", "<p:stamp site></p:stamp><div p:template='text/liquid' r:posts='li' e:myVar='1'></div>", "<p:stamp site></p:stamp><div p:template=\"text/liquid\" r:posts=\"li\" e:myvar=\"1\"></div>", ""},
	{"tbody", "{% for x in y %}<tr><td>{{x}}</td></tr>{% endfor %}", "{% for x in y %}<tr><td>{{x}}</td></tr>{% endfor %}", "Liquid text in table context is foster-parented"},
	{"table", "<p:include resource='/a' selector='#b'></p:include>", "<p:include resource=\"/a\" selector=\"#b\"></p:include>", "include inside <table> (pagelove-ats roles.html)"},
	{"p", "<p>nested</p>", "<p>nested</p>", ""},
	{"textarea", "<b>x</b> &amp;", "&lt;b&gt;x&lt;/b&gt; &amp;", "RCDATA context"},
	{"script", "a < b </script> c", "a &lt; b &lt;/script&gt; c", "raw text context"},
	{"body", "", "", "empty body"},
	{"ul", "<li><!-- a &amp; b --><a title=t href=/x>l</a></li>", "<li><!-- a &amp; b --><a title=\"t\" href=\"/x\">l</a></li>", "comment and attribute-order fixups"},
}

// ctxNode builds a detached ancestor chain ending in tag and returns tag.
func ctxNode(tag string) *html.Node {
	chains := map[string][]string{
		"tbody":    {"html", "body", "table", "tbody"},
		"tr":       {"html", "body", "table", "tbody", "tr"},
		"td":       {"html", "body", "table", "tbody", "tr", "td"},
		"table":    {"html", "body", "table"},
		"head":     {"html", "head"},
		"html":     {"html"},
		"template": {"html", "body", "template"},
	}
	chain, ok := chains[tag]
	if !ok {
		chain = []string{"html", "body", tag}
	}
	var parent, n *html.Node
	for _, t := range chain {
		n = &html.Node{Type: html.ElementNode, Data: t, DataAtom: atom.Lookup([]byte(t))}
		if parent != nil {
			parent.AppendChild(n)
		}
		parent = n
	}
	return n
}

func serializeAll(ns []*html.Node) string {
	var b strings.Builder
	for _, n := range ns {
		b.WriteString(htmlser.String(n, htmlser.Options{PageLove: true}))
	}
	return b.String()
}

func TestFragments(t *testing.T) {
	for _, c := range fragCases {
		ns, err := parseHTMLFragment(c.frag, ctxNode(c.ctx))
		if err != nil {
			t.Errorf("%s %q: %v", c.ctx, c.frag, err)
			continue
		}
		if got := serializeAll(ns); got != c.want {
			t.Errorf("ctx=%s %q (%s)\n  want %q\n  got  %q", c.ctx, c.frag, c.note, c.want, got)
		}
	}
}

func TestParseFragmentNeedsContext(t *testing.T) {
	if _, err := ParseFragment("<tr><td>a</td></tr>", nil); !errors.Is(err, ErrNoContext) {
		t.Errorf("nil context: got %v, want ErrNoContext", err)
	}
	if _, err := ParseFragment("<p>x</p>", &html.Node{Type: html.TextNode, Data: "x"}); !errors.Is(err, ErrNoContext) {
		t.Errorf("text context: got %v, want ErrNoContext", err)
	}
	ns, err := ParseFragment("\n  <li>x</li>\n", ctxNode("ul"))
	if err != nil || len(ns) != 1 || ns[0].Data != "li" {
		t.Errorf("surrounding whitespace must be trimmed: %v %v", ns, err)
	}
	// ParseBodyFragment is the explicit, read-only parse.
	ns, err = ParseBodyFragment("<tr><td>a</td></tr>")
	if err != nil || serializeAll(ns) != "<tr><td>a</td></tr>" {
		t.Errorf("body fragment: %q %v", serializeAll(ns), err)
	}
}

func TestPlacementContexts(t *testing.T) {
	doc, err := Parse([]byte(`<!DOCTYPE html><html><body><table><tbody id="responses"><tr id="r1"><td>a</td></tr></tbody></table><ul id="l"><li id="a">A</li></ul></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	byID := func(id string) *html.Node {
		var found *html.Node
		Walk(doc, func(n *html.Node) bool {
			if v, ok := Attr(n, "id"); ok && v == id && n.Type == html.ElementNode {
				found = n
			}
			return found == nil
		})
		return found
	}
	type step struct {
		id    string
		p     Placement
		body  string
		check string // id of the element whose outer HTML is compared
		want  string
	}
	for _, c := range []step{
		{"responses", PlaceAppend, `<tr id="r2"><td>b</td></tr>`, "responses", `<tbody id="responses"><tr id="r1"><td>a</td></tr><tr id="r2"><td>b</td></tr></tbody>`},
		{"r1", PlaceReplace, `<tr id="r1"><td>A!</td></tr>`, "responses", `<tbody id="responses"><tr id="r1"><td>A!</td></tr><tr id="r2"><td>b</td></tr></tbody>`},
		{"r1", PlaceBefore, `<tr id="r0"><td>z</td></tr>`, "responses", `<tbody id="responses"><tr id="r0"><td>z</td></tr><tr id="r1"><td>A!</td></tr><tr id="r2"><td>b</td></tr></tbody>`},
		{"a", PlaceAfter, `<li id="b">B</li>`, "l", `<ul id="l"><li id="a">A</li><li id="b">B</li></ul>`},
		{"l", PlacePrepend, `<li id="z">Z</li>`, "l", `<ul id="l"><li id="z">Z</li><li id="a">A</li><li id="b">B</li></ul>`},
	} {
		target := byID(c.id)
		ctx := target.Parent
		if c.p == PlaceAppend || c.p == PlacePrepend {
			ctx = target
		}
		ns, err := ParseFragment(c.body, ctx)
		if err != nil {
			t.Fatal(err)
		}
		switch c.p {
		case PlaceAppend:
			Append(target, ns)
		case PlacePrepend:
			Prepend(target, ns)
		case PlaceBefore:
			InsertBefore(target, ns)
		case PlaceAfter:
			InsertAfter(target, ns)
		case PlaceReplace:
			Replace(target, ns)
		}
		if got := OuterHTML(byID(c.check)); got != c.want {
			t.Errorf("%s %s:\n got %s\nwant %s", c.id, c.p, got, c.want)
		}
	}
}

func TestPrefixedElements(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"include in table (pagelove-ats roles.html)",
			`<table><p:include resource="/admin/index.html" selector="#roles-body"></p:include></table>`,
			`<table><p:include resource="/admin/index.html" selector="#roles-body"></p:include></table>`},
		{"self-closing include (docs Includes example)",
			`<body><p:include selector="#nav" resource="/x" />
  <main><p>Page</p></main></body>`,
			`<body><p:include selector="#nav" resource="/x"></p:include>
  <main><p>Page</p></main></body>`},
		{"stamp in paragraph", `<p>Hi <p:stamp name></p:stamp>!</p>`, `<p>Hi <p:stamp name></p:stamp>!</p>`},
		{"prefixed tag inside script is text", `<script>x = "<p:include/>"</script>`, `<script>x = "<p:include/>"</script>`},
	}
	for _, c := range cases {
		doc, err := Parse([]byte(c.src))
		if err != nil {
			t.Fatal(err)
		}
		if got := string(Render(doc)); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
	// Fragments parse prefixed elements the same way, including inside a
	// prefixed context (template contents).
	tbody := ctxNode("tbody")
	ns, err := ParseFragment(`<p:include resource="/a" selector="#b"/><tr><td>x</td></tr>`, tbody)
	if err != nil {
		t.Fatal(err)
	}
	if got := serializeAll(ns); got != `<p:include resource="/a" selector="#b"></p:include><tr><td>x</td></tr>` {
		t.Errorf("prefixed fragment in tbody: %q", got)
	}
	inc := ns[0]
	ns, err = ParseFragment(`<tr><td>y</td></tr>`, inc)
	if err != nil || serializeAll(ns) != `<tr><td>y</td></tr>` {
		t.Errorf("fragment in a prefixed context: %q %v", serializeAll(ns), err)
	}
}

// The fixups exist because of x/net/html behaviours that differ from
// html5ever. These tests fail once upstream changes, so the fixups can be
// reviewed (and reported upstream) rather than silently kept.
func TestFixupsStillNeeded(t *testing.T) {
	const src = `<p><!-- a &amp; b --><a itemprop="url" href="/x">x</a></p>`
	plain, err := html.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	var comment, a *html.Node
	Walk(plain, func(n *html.Node) bool {
		switch {
		case n.Type == html.CommentNode:
			comment = n
		case n.Type == html.ElementNode && n.Data == "a":
			a = n
		}
		return true
	})
	if comment.Data != " a & b " {
		t.Errorf("x/net/html no longer unescapes comment text (%q): FixComments may be unnecessary", comment.Data)
	}
	if a.Attr[0].Key != "href" {
		t.Errorf("x/net/html no longer sorts formatting-element attributes (%v): FixAttrOrder may be unnecessary", a.Attr)
	}
	fixed, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := OuterHTML(DocumentElement(fixed)); got != `<p><!-- a &amp; b --><a itemprop="url" href="/x">x</a></p>` {
		t.Errorf("fixups not applied: %s", got)
	}
}

func TestRenderDispatch(t *testing.T) {
	h, _ := Parse([]byte("<!DOCTYPE html><p>a<br/>b</p><pre>\n\nx</pre>"))
	if got := string(Render(h)); got != "<!DOCTYPE html><p>a<br>b</p><pre>\n\nx</pre>" {
		t.Errorf("HTML render: %q", got)
	}
	if got := string(RenderStorage(h, ImpliedFor(h, "<!DOCTYPE html><p>a<br/>b</p>"))); got != "<!DOCTYPE html><p>a<br>b</p><pre>\n\nx</pre>" {
		t.Errorf("HTML storage render: %q", got)
	}
	x, err := ParseXML([]byte(`<?xml version="1.0"?><feed xmlns="urn:a"><e><![CDATA[<b>]]></e><empty></empty></feed>`))
	if err != nil {
		t.Fatal(err)
	}
	c := Clone(x)
	if got := string(Render(c)); got != `<?xml version="1.0"?><feed xmlns="urn:a"><e><![CDATA[<b>]]></e><empty/></feed>` {
		t.Errorf("XML render of a clone: %q", got)
	}
	feed := DocumentElement(c)
	if got := OuterHTML(feed.FirstChild); got != `<e><![CDATA[<b>]]></e>` {
		t.Errorf("XML outer: %q", got)
	}
	ns, err := ParseFragment(`<n>1</n>`, feed)
	if err != nil || len(ns) != 1 || ns[0].Namespace != "urn:a" {
		t.Fatalf("XML fragment: %v %v", ns, err)
	}
}

func TestSpansAndSplice(t *testing.T) {
	src := "<!DOCTYPE html>\n<html>\n<body>\n  <ul id=l class='x'>\n    <li>a<br/>\n    <li hidden>b</li>\n  </ul>\n  <p:include selector=\"#n\" resource='/x' />\n  <main>m</main>\n</body>\n</html>\n"
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	sp := ComputeSpans(doc, src)
	if !sp.Clean {
		t.Fatal("expected a clean correlation")
	}
	var ul, li1, br, inc, main *html.Node
	Walk(doc, func(n *html.Node) bool {
		if n.Type == html.ElementNode {
			switch {
			case n.Data == "ul":
				ul = n
			case n.Data == "li" && li1 == nil:
				li1 = n
			case n.Data == "br":
				br = n
			case n.Data == "p:include":
				inc = n
			case n.Data == "main":
				main = n
			}
		}
		return true
	})
	outer := func(n *html.Node) string { s := sp.By[n]; return src[s.Start:s.End] }
	if got := outer(ul); got != "<ul id=l class='x'>\n    <li>a<br/>\n    <li hidden>b</li>\n  </ul>" {
		t.Errorf("ul span %q", got)
	}
	// PageLove's model: the second li nests in the first, which the
	// </ul> closes (LO-15).
	if got := outer(li1); got != "<li>a<br/>\n    <li hidden>b</li>\n  " {
		t.Errorf("implicitly closed li span %q", got)
	}
	if s := sp.By[br]; !s.NoContent || outer(br) != "<br/>" {
		t.Errorf("br span %+v", s)
	}
	if s := sp.By[inc]; !s.NoContent || outer(inc) != `<p:include selector="#n" resource='/x' />` {
		t.Errorf("self-closed prefixed span %+v %q", s, outer(inc))
	}
	out, err := Splice([]byte(src), sp,
		Edit{Node: main, Placement: PlaceReplace, Text: "<main>M</main>"},
		Edit{Node: ul, Placement: PlaceAppend, Text: "<li>c</li>\n  "},
		Edit{Node: ul, Placement: PlaceBefore, Text: "<!-- x -->"})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(strings.Replace(src, "<main>m</main>", "<main>M</main>", 1), "<ul id=l class='x'>\n    <li>a<br/>\n    <li hidden>b</li>\n  </ul>", "<!-- x --><ul id=l class='x'>\n    <li>a<br/>\n    <li hidden>b</li>\n  <li>c</li>\n  </ul>", 1)
	if string(out) != want {
		t.Errorf("splice:\n got %q\nwant %q", out, want)
	}
	if _, err := Splice([]byte(src), sp, Edit{Node: br, Placement: PlaceAppend, Text: "x"}); !errors.Is(err, ErrNoSpan) {
		t.Errorf("append into a void element: %v", err)
	}
	if _, err := Splice([]byte(src), sp, Edit{Node: DocumentElement(doc).FirstChild, Placement: PlaceReplace}); err != nil && !errors.Is(err, ErrNoSpan) {
		t.Errorf("implied head: %v", err)
	}
	// Moving an element next to itself: remove, then insert at its old end.
	out, err = Splice([]byte(src), sp, Edit{Node: main, Placement: PlaceReplace}, Edit{Node: main, Placement: PlaceAfter, Text: "<main>m</main>"})
	if err != nil || string(out) != src {
		t.Errorf("remove+reinsert: %v\n%q", err, out)
	}
	// A tree changed after parsing no longer correlates cleanly.
	fsrc := "<table><tr><td>a</td></tr><div>x</div></table>"
	fdoc, _ := Parse([]byte(fsrc))
	if !ComputeSpans(fdoc, fsrc).Clean {
		t.Error("an unchanged tree must correlate cleanly")
	}
	DocumentElement(fdoc).AppendChild(&html.Node{Type: html.ElementNode, Data: "p"})
	if ComputeSpans(fdoc, fsrc).Clean {
		t.Error("a changed tree must not correlate cleanly")
	}
}

// upstreamHTML lists the upstream app documents (research/upstream is
// git-ignored; tests that need it skip when it is absent).
func upstreamHTML(t *testing.T) []string {
	root := filepath.Join("..", "..", "research", "upstream")
	if _, err := os.Stat(root); err != nil {
		t.Skip("research/upstream not present")
	}
	var out []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "vendor", "target":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".html") {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// TestCorpusRoundTrip: every upstream document reaches a serialization
// fixed point in one pass with the storage variant.
func TestCorpusRoundTrip(t *testing.T) {
	for _, f := range upstreamHTML(t) {
		b, _ := os.ReadFile(f)
		doc, err := Parse(b)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		st := RenderStorage(doc, nil)
		d2, _ := Parse(st)
		if again := RenderStorage(d2, nil); string(again) != string(st) {
			t.Errorf("%s: storage serialization is not a fixed point", f)
		}
	}
}

// TestCorpusSplice samples elements of every upstream document and checks
// that deleting one, appending a comment to it, and replacing it with its
// own serialization by splicing give the same DOM as mutating the tree
// (decision 0003 §6). The sample keeps the test fast.
func TestCorpusSplice(t *testing.T) {
	ser := func(n *html.Node) string { return htmlser.String(n, htmlser.Options{PreserveLeadingNewline: true}) }
	var docs, clean, edits, equal int
	for _, f := range upstreamHTML(t) {
		b, _ := os.ReadFile(f)
		src := string(b)
		doc, _ := Parse(b)
		sp := ComputeSpans(doc, src)
		docs++
		if !sp.Clean {
			continue
		}
		clean++
		var els []*html.Node
		Walk(doc, func(n *html.Node) bool {
			if n.Type == html.ElementNode {
				els = append(els, n)
			}
			return true
		})
		step := max(1, len(els)/12)
		for i := 0; i < len(els); i += step {
			el := els[i]
			if _, ok := sp.By[el]; !ok || el.Parent == nil || el.Parent.Type != html.ElementNode {
				continue
			}
			switch el.Data {
			case "html", "head", "body", "title", "textarea", "script", "style":
				continue
			}
			for _, op := range []struct {
				p    Placement
				text string
				mut  func(*html.Node)
			}{
				{PlaceReplace, "", func(n *html.Node) { n.Parent.RemoveChild(n) }},
				{PlaceAppend, "<!--pl-->", func(n *html.Node) { n.AppendChild(&html.Node{Type: html.CommentNode, Data: "pl"}) }},
				{PlaceReplace, ser(el), func(*html.Node) {}},
			} {
				spliced, err := Splice(b, sp, Edit{Node: el, Placement: op.p, Text: op.text})
				if err != nil {
					continue // void element: no content to append to
				}
				d2, _ := Parse(b)
				var els2 []*html.Node
				Walk(d2, func(n *html.Node) bool {
					if n.Type == html.ElementNode {
						els2 = append(els2, n)
					}
					return true
				})
				op.mut(els2[i])
				d3, _ := Parse(spliced)
				edits++
				if ser(d3) == ser(d2) {
					equal++
				}
			}
		}
	}
	t.Logf("docs %d, clean %d; sampled edits %d, equal DOM %d", docs, clean, edits, equal)
	if clean < docs*9/10 {
		t.Errorf("only %d/%d upstream documents correlate cleanly", clean, docs)
	}
	if equal < edits*98/100 {
		t.Errorf("only %d/%d spliced edits give the mutated DOM", equal, edits)
	}
}
