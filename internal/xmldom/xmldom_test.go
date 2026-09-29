package xmldom_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/xmldom"
)

// The Atom example from docs reference/composing-pages/XML-Documents
// (note `<p:stamp site>`: an attribute with no value, i.e. not well-formed).
const atomDoc = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:e="https://pagelove.org/Binding/Sessel" xmlns:p="https://pagelove.org/1.0" e:site="'Pagelove Blog'">
  <title><p:stamp site></p:stamp></title>
  <entry><id>urn:1</id></entry>
  <Entry><id>urn:2</id></Entry>
  <div hidden="hidden" itemscope="itemscope" itemtype="https://pagelove.org/AuthorizationRule">
    <meta itemprop="actor" content="*"></meta>
    <meta itemprop="resource" content="/*"></meta>
  </div>
</feed>
`

const rssDoc = `<?xml version="1.0"?>
<!-- feed -->
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
<channel>
  <title>Tom &amp; Jerry's &lt;blog&gt;</title>
  <atom:link href="https://x.test/feed.xml?a=1&amp;b=2" rel="self" type="application/rss+xml"/>
  <item><description><![CDATA[<p>Hello <b>world</b></p>]]></description><guid isPermaLink="false">1</guid></item>
</channel>
</rss>
`

const svgDoc = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 10 10"><defs><linearGradient id="g"><stop offset="0"/></linearGradient></defs><use xlink:href="#g"/><foreignObject><div xmlns="http://www.w3.org/1999/xhtml">x</div></foreignObject></svg>`

const xhtmlDoc = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="en"><head><title>X</title></head><body><p>a&nbsp;b &#169; <br/></p></body></html>`

var xmlCases = map[string]string{"atom": atomDoc, "rss": rssDoc, "svg": svgDoc, "xhtml": xhtmlDoc}

func TestRoundTrip(t *testing.T) {
	identical := map[string]bool{"rss": true, "svg": true}
	for name, src := range xmlCases {
		d, err := xmldom.Parse([]byte(src), xmldom.Options{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		r1 := xmldom.String(d)
		d2, err := xmldom.Parse([]byte(r1), xmldom.Options{Strict: true})
		if err != nil {
			t.Errorf("%s: output is not well-formed XML: %v\n%s", name, err, r1)
			continue
		}
		if r2 := xmldom.String(d2); r1 != r2 {
			t.Errorf("%s: not a fixed point\n r1 %q\n r2 %q", name, r1, r2)
		}
		if identical[name] && r1 != src {
			t.Errorf("%s: output differs from source\n src %q\n out %q", name, src, r1)
		}
	}
	// The Atom example differs only as the docs require: empty elements
	// self-close and the valueless attribute gets a value.
	d, _ := xmldom.Parse([]byte(atomDoc), xmldom.Options{})
	out := xmldom.String(d)
	for _, want := range []string{`<p:stamp site=""/>`, `<meta itemprop="actor" content="*"/>`, `<?xml version="1.0" encoding="UTF-8"?>`, `e:site="'Pagelove Blog'"`} {
		if !strings.Contains(out, want) {
			t.Errorf("atom output lacks %q:\n%s", want, out)
		}
	}
}

func TestNamesAndNamespaces(t *testing.T) {
	d, err := xmldom.Parse([]byte(atomDoc), xmldom.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var feed, stamp *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "feed":
				feed = n
			case "p:stamp":
				stamp = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(d)
	if feed == nil || feed.Namespace != "http://www.w3.org/2005/Atom" || feed.DataAtom != 0 {
		t.Fatalf("feed: %+v", feed)
	}
	if stamp == nil || stamp.Namespace != "https://pagelove.org/1.0" || len(stamp.Attr) != 1 || stamp.Attr[0] != (html.Attribute{Key: "site", Val: ""}) {
		t.Fatalf("p:stamp: %+v", stamp)
	}
	if !xmldom.IsXML(d) || !xmldom.IsXML(feed) || !xmldom.IsXML(feed.FirstChild) {
		t.Error("IsXML false on an XML tree")
	}
	hdoc, _ := html.Parse(strings.NewReader(`<p>x<svg><circle/></svg></p>`))
	if xmldom.IsXML(hdoc) {
		t.Error("IsXML true on an HTML tree")
	}
	d2, _ := xmldom.Parse([]byte(`<r><x/></r>`), xmldom.Options{})
	if r := d2.FirstChild; r.Namespace != xmldom.NoNamespace {
		t.Errorf("no-namespace element: Namespace %q", r.Namespace)
	}
}

func TestXMLSelectors(t *testing.T) {
	d, err := xmldom.Parse([]byte(atomDoc), xmldom.Options{})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		sel  string
		want int
	}{
		{"entry", 1}, // case-sensitive: <Entry> is a different element
		{"Entry", 1},
		{"ENTRY", 0},
		{"feed > title", 1},
		{`p\:stamp`, 1}, // prefixed names are matched as written
		{`[e\:site]`, 1},
		{`[xmlns\:e]`, 1},
		{"[itemtype$=AuthorizationRule] > meta[itemprop=actor]", 1},
		{"meta:value-equals('*')", 1}, // microdata value from content= (local name)
		{":root", 1},
	}
	for _, c := range cases {
		s, err := selector.Compile(c.sel)
		if err != nil {
			t.Fatalf("%s: %v", c.sel, err)
		}
		if got := len(s.MatchAll(d)); got != c.want {
			t.Errorf("%s: got %d want %d", c.sel, got, c.want)
		}
	}
}

func TestSpans(t *testing.T) {
	src := `<?xml version="1.0"?>` + "\n" + `<feed xmlns="urn:a"><entry id="1"><t>x</t></entry><empty/><p:x xmlns:p="urn:p" a='1'>y</p:x></feed>`
	d, sp, err := xmldom.ParseWithSpans([]byte(src), xmldom.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !sp.Clean {
		t.Fatal("well-formed input must correlate cleanly")
	}
	check := func(sel, outer, inner string, self bool) {
		t.Helper()
		n := selector.MustCompile(sel).MatchFirst(d)
		s, ok := sp.By[n]
		if !ok {
			t.Fatalf("%s: no span", sel)
		}
		if got := src[s.Start:s.End]; got != outer {
			t.Errorf("%s: outer %q want %q", sel, got, outer)
		}
		if got := src[s.InnerStart:s.InnerEnd]; got != inner {
			t.Errorf("%s: inner %q want %q", sel, got, inner)
		}
		if s.SelfClosing != self {
			t.Errorf("%s: SelfClosing %v", sel, s.SelfClosing)
		}
	}
	check("entry", `<entry id="1"><t>x</t></entry>`, `<t>x</t>`, false)
	check("empty", `<empty/>`, ``, true)
	check(`p\:x`, `<p:x xmlns:p="urn:p" a='1'>y</p:x>`, `y`, false)

	_, sp2, err := xmldom.ParseWithSpans([]byte(`<a><b></a>`), xmldom.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sp2.Clean {
		t.Error("a repaired document must not correlate cleanly")
	}
}

func TestParseFragment(t *testing.T) {
	d, err := xmldom.Parse([]byte(atomDoc), xmldom.Options{})
	if err != nil {
		t.Fatal(err)
	}
	feed := selector.MustCompile("feed").MatchFirst(d)
	nodes, err := xmldom.ParseFragment(`<entry><id>urn:3</id><p:stamp site/></entry> <![CDATA[<raw>]]>`, feed)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("got %d nodes", len(nodes))
	}
	entry := nodes[0]
	if entry.Data != "entry" || entry.Namespace != "http://www.w3.org/2005/Atom" || entry.Parent != nil {
		t.Errorf("entry: Data %q Namespace %q", entry.Data, entry.Namespace)
	}
	if stamp := entry.LastChild; stamp.Data != "p:stamp" || stamp.Namespace != "https://pagelove.org/1.0" {
		t.Errorf("p:stamp: Data %q Namespace %q", stamp.Data, stamp.Namespace)
	}
	if nodes[2].Namespace != xmldom.CDATA {
		t.Error("CDATA section not marked")
	}
	feed.AppendChild(entry)
	if got := xmldom.String(entry); got != `<entry><id>urn:3</id><p:stamp site=""/></entry>` {
		t.Errorf("entry XML %q", got)
	}
	if _, err := xmldom.ParseFragment("<x/>", nil); err == nil {
		t.Error("nil context must be an error")
	}
}

// TestUpstreamXMLFiles round-trips every .xml/.svg file in research/upstream
// (git-ignored; skipped when absent).
func TestUpstreamXMLFiles(t *testing.T) {
	root := filepath.Join("..", "..", "research", "upstream")
	if _, err := os.Stat(root); err != nil {
		t.Skip("research/upstream not present")
	}
	var files []string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && (info.Name() == ".git" || info.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if ext := filepath.Ext(p); ext == ".xml" || ext == ".svg" {
			files = append(files, p)
		}
		return nil
	})
	identical := 0
	for _, f := range files {
		src, _ := os.ReadFile(f)
		d, err := xmldom.Parse(src, xmldom.Options{})
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		out := xmldom.String(d)
		if _, err := xmldom.Parse([]byte(out), xmldom.Options{Strict: true}); err != nil {
			t.Errorf("%s: output not well-formed: %v", f, err)
		}
		if out == string(src) {
			identical++
		}
	}
	t.Logf("%d/%d upstream XML/SVG files byte-identical after parse+render", identical, len(files))
}
