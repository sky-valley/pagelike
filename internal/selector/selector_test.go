package selector_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/selector"
)

// One document that every feature case runs against. Element indices are
// document (pre-)order over all elements: html=0, head=1, title=2, body=3...
const featureDoc = `<!DOCTYPE html>
<html lang="en" xmlns:p="https://pagelove.org/1.0">
<head><title>T</title></head>
<body>
<h1 id="title">Hello World</h1>
<h2>hello world</h2>
<ul id="list" class="Items">
  <li id="a" itemprop="item">A</li>
  <li id="b" class="x" itemprop="item"><span>B</span></li>
  <li id="c" data-v="Foo-bar"> </li>
  <li id="d"><!--c--></li>
  <p id="stray">p</p>
</ul>
<div id="cfg" itemscope itemtype="https://pagelove.org/HostConfig">
  <meta itemprop="hostname" content="localhost">
  <a itemprop="url" href="/about">About Us</a>
  <data itemprop="score" value="42">Forty-two</data>
  <span itemprop="price"> 9.99 </span>
  <time itemprop="when">2026-09-28</time>
</div>
<main pagelove:template="text/liquid" p:template="x"><p:stamp greeting></p:stamp></main>
<svg viewBox="0 0 1 1"><linearGradient id="g"></linearGradient><foreignObject><p>in svg</p></foreignObject></svg>
<table><tbody id="rows"><tr><th itemprop="name">n</th><td>1</td></tr></tbody></table>
<div id="outer"><section><div id="inner"><p class="deep">x</p></div></section></div>
</body>
</html>`

// featureMatrix is the fork's result for each selector (sorted element
// indices, or ERR). Every row equals Servo's selectors crate (scraper 0.27,
// recorded with the oracle in the html-selectors spike (docs/decisions/0003)), except the
// deliberate divergences marked: scraper leaves :nth-child(… of S) disabled
// while browsers support it, and :contains is a PageLove extension.
var featureMatrix = []struct{ sel, want string }{
	{"h1", "4"},
	{"#list > li", "7,8,10,11"},
	{"ul li span", "9"},
	{"li + li", "8,10,11"},
	{"li ~ p", "12"},
	{"LI", "7,8,10,11"},
	{"UL#list", "6"},
	{"*", "0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33"},
	{".Items", "6"},
	{".items", ""},
	{"[id]", "4,6,7,8,10,11,12,13,22,26,30,32"},
	{"[data-v='Foo-bar']", "10"},
	{"[data-v|=Foo]", "10"},
	{"[data-v^=Foo]", "10"},
	{"[data-v$=bar]", "10"},
	{"[data-v*=o-b]", "10"},
	{"[data-v*=O-B i]", "10"},
	{"[data-v=foo-bar i]", "10"},
	{"[data-v='Foo-bar' s]", "10"},
	{"[itemprop~=item]", "7,8"},
	{"li:first-child", "7"},
	{"li:last-child", ""},
	{"li:only-child", ""},
	{"li:nth-child(2)", "8"},
	{"li:nth-child(2n+1)", "7,10"},
	{"li:nth-child(odd)", "7,10"},
	{"li:nth-last-child(1)", ""},
	{"li:nth-of-type(2)", "8"},
	{"li:first-of-type", "7"},
	{"li:last-of-type", "11"},
	{"p:only-of-type", "12,24,33"},
	{"span:only-of-type", "9,17"},
	{":root", "0"},
	{"li:empty", "11"},
	{":not(li)", "0,1,2,3,4,5,6,9,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33"},
	{"li:not(.x, #a)", "10,11"},
	{"li:not(:first-child)", "8,10,11"},
	{"ul > :not(li)", "12"},
	{":is(h1, h2)", "4,5"},
	{":where(h1, h2)", "4,5"},
	{"li:is(#a, #b)", "7,8"},
	{"ul:has(> p)", "6"},
	{"ul:has(span)", "6"},
	{"li:has(+ li)", "7,8,10"},
	{"li:has(ul span)", ""},
	{"section:has(#outer p)", ""},
	{"li:has(~ p)", "7,8,10,11"},
	{"div:has(> section > div > p)", "30"},
	{"div:has(> div)", ""},
	{"#cfg:has(> meta, > nav)", "13"},
	{"li:nth-child(1 of .x)", "8"},         // deliberate: Servo rejects it
	{"li:nth-child(2 of [itemprop])", "8"}, // deliberate: Servo rejects it
	{":scope", "0"},
	{":scope > body", "3"},
	{"[itemtype*=HostConfig]:has([itemprop=hostname])", "13"},
	{"linearGradient", "22"},
	{"lineargradient", ""},
	{"svg [viewBox]", ""},
	{"[viewBox]", "21"},
	{"[viewbox]", ""},
	{"foreignObject p", "24"},
	{"svg p", "24"},
	{"[p\\:template]", "19"},
	{"[pagelove\\:template]", "19"},
	{"p\\:stamp", "20"},
	{"main > p\\:stamp", "20"},
	{"[p|template]", "ERR"},
	{"#list > li:nth-child(2) > span:nth-child(1)", "9"},
	{"UL:nth-child(3)", "6"},
	{"#\\31 23", ""},
	{"[itemprop=\"item\"]", "7,8"},
	{"li[itemprop=\"item\"]", "7,8"},
	{"#list > [itemprop=\"item\"]", "7,8"},
	{"html > body > ul:nth-child(3) > li:nth-child(2)", "8"},
	{"li:contains(b)", ""}, // deliberate: Servo rejects it
	{"li:containsOwn(A)", "ERR"},
	{"li:matches(^A$)", "ERR"},
	{"ul:haschild(li)", "ERR"},
	{":input", "ERR"},
	{"[data-v!=x]", "ERR"},
	{"li:nth-child(", "ERR"},
	{"[", "ERR"},
	{"h1 >", "ERR"},
	{"", "ERR"},
	{"p::before", "ERR"},
}

func elementOrder(doc *html.Node) map[*html.Node]int {
	m := map[*html.Node]int{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			m[n] = len(m)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return m
}

func outcome(doc *html.Node, sel string, order map[*html.Node]int) string {
	s, err := selector.Compile(sel)
	if err != nil {
		return "ERR"
	}
	var idx []int
	for _, n := range s.MatchAll(doc) {
		idx = append(idx, order[n])
	}
	sort.Ints(idx)
	parts := make([]string, len(idx))
	for i, v := range idx {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ",")
}

func TestFeatureMatrix(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(featureDoc))
	if err != nil {
		t.Fatal(err)
	}
	order := elementOrder(doc)
	for _, c := range featureMatrix {
		if got := outcome(doc, c.sel, order); got != c.want {
			t.Errorf("%q: got %s want %s", c.sel, got, c.want)
		}
	}
}

// TestTemplateContents: browsers never match into <template> contents from
// the document, and neither does the fork (x/net/html keeps them as
// ordinary children, so stock cascadia did).
func TestTemplateContents(t *testing.T) {
	doc, _ := html.Parse(strings.NewReader(`<body><template id="tpl"><li class="tpl">t</li></template><ul><li>real</li></ul></body>`))
	for sel, want := range map[string]int{".tpl": 0, "template li": 0, "template > li": 0, "li": 1, "template:has(li)": 0, "#tpl": 1} {
		s := selector.MustCompile(sel)
		if got := len(s.MatchAll(doc)); got != want {
			t.Errorf("%s: MatchAll %d want %d", sel, got, want)
		}
		if first := s.MatchFirst(doc); (first != nil) != (want > 0) {
			t.Errorf("%s: MatchFirst %v", sel, first)
		}
	}
}

func TestCompileIsStrict(t *testing.T) {
	for _, sel := range []string{"p::before", "p:before", "li:containsOwn(A)", "li:matches(^A$)", "ul:haschild(li)", ":input", "[a!=b]", "[a#=(x)]", "", "   ", "[p|template]"} {
		if _, err := selector.Compile(sel); err == nil {
			t.Errorf("%q: accepted", sel)
		}
	}
	s, err := selector.Compile("  #a > li  ")
	if err != nil || s.String() != "#a > li" {
		t.Errorf("trimmed source: %v %v", s, err)
	}
}

func TestPageLoveExtensions(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(featureDoc + `<p id="t1"> Title </p><p id="t2">Title Bar</p><p id="caf">café</p>`))
	if err != nil {
		t.Fatal(err)
	}
	ext := selector.ExtOptions{
		IsA: func(typ, target string) bool {
			return typ == target || (target == "https://pagelove.org/Config" && strings.HasSuffix(typ, "HostConfig"))
		},
	}
	ids := func(sel string) string {
		s, err := selector.CompileWith(sel, ext)
		if err != nil {
			return "ERR: " + err.Error()
		}
		var out []string
		for _, n := range s.MatchAll(doc) {
			id := ""
			for _, a := range n.Attr {
				if a.Key == "id" {
					id = a.Val
				}
			}
			if id == "" {
				id = n.Data
			}
			out = append(out, id)
		}
		return strings.Join(out, ",")
	}
	cases := []struct{ sel, want string }{
		{"h1:contains('Hello')", "title"},
		{"h1:contains('hello')", ""},
		{":contains('hello', i)", "html,body,title,h2"},
		{"h2:contains(\"hello\")", "h2"},
		{"p:equals('Title')", "t1"},
		{"p:contains('Title')", "t1,t2"},
		{"p:equals('title', i)", "t1"},
		{"[itemprop=hostname]:value-equals('localhost')", "meta"},
		{"[itemprop=url]:value-equals('/about')", "a"},
		{"[itemprop=url]:equals('About Us')", "a"},
		{"[itemprop=score]:value-equals('42')", "data"},
		{"[itemprop=score]:value-contains('4')", "data"},
		{"[itemprop=when]:value-equals('2026-09-28')", "time"},
		{"[itemtype*=HostConfig]:has([itemprop=hostname]:value-equals('localhost'), [itemprop=alias]:value-equals('127.0.0.1'))", "cfg"},
		{"[itemprop=price]:less-than('10')", "span"},
		{"[itemprop=price]:greater-than('9.99')", ""},
		{"[itemprop=score]:value-greater-than(41)", "data"},
		{"[itemprop=score]:greater-than('41')", ""}, // text "Forty-two" is not numeric
		{"ul:only(> li)", ""},                       // #b has a <span>, and <p> is a child
		{"ul:only(> li, > li span, > p)", "list"},
		{"tbody:only(> tr, > tr > th, > tr > td)", "rows"},
		{"li:only(span)", "a,b,c,d"},
		{":isa('https://pagelove.org/Config')", "cfg"},
		{"p:contains('caf\\E9 ')", "caf"},
		{"li:containsOwn(A)", "ERR"},
		{"li:contains()", "ERR"},
		{"li:contains('a', x)", "ERR"},
		{"li:less-than('abc')", "ERR"},
	}
	for _, c := range cases {
		got := ids(c.sel)
		if strings.HasPrefix(got, "ERR") && c.want == "ERR" {
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q want %q", c.sel, got, c.want)
		}
	}
	// Without an inheritance map :isa() matches nothing.
	if n := len(selector.MustCompile(":isa('https://pagelove.org/Config')").MatchAll(doc)); n != 0 {
		t.Errorf(":isa without schemas matched %d", n)
	}
}

func TestSelectorFunctions(t *testing.T) {
	ev := fakeEval{}
	cases := []struct{ in, want string }{
		{"div:nth-child(count(h1))", "div:nth-child(3)"},
		{"[data-name=text-of(#title)]", `[data-name="Hello \"World\""]`},
		{"[data-name=value-of([itemprop='name'])]", `[data-name="Ada"]`},
		{"[data-link=attr-of('href', #home)]", `[data-link="/"]`},
		{"[x='count(h1)']", "[x='count(h1)']"},                 // inside a string: untouched
		{":contains('text-of(x)')", ":contains('text-of(x)')"}, // likewise
		{"li:nth-child(count(li:nth-child(count(h1))))", "li:nth-child(1)"},
	}
	for _, c := range cases {
		got, err := selector.ExpandFunctions(c.in, ev)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %s want %s", c.in, got, c.want)
		}
	}
}

type fakeEval struct{}

func (fakeEval) Count(sel string) (int, error) {
	if sel == "h1" {
		return 3, nil
	}
	return 1, nil
}
func (fakeEval) TextOf(string) (string, error)           { return `Hello "World"`, nil }
func (fakeEval) ValueOf(string) (string, error)          { return "Ada", nil }
func (fakeEval) AttrOf(attr, sel string) (string, error) { return "/", nil }

func TestPath(t *testing.T) {
	doc, err := dom.Parse([]byte(`<!DOCTYPE html><html><body><main id="m"><p:stamp site></p:stamp><div id="123"><span>x</span></div><div id="dup"></div><div id="dup"><i>y</i></div></main><ul><li>a</li><li>b</li></ul></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	find := func(sel string) *html.Node {
		n := selector.MustCompile(sel).MatchFirst(doc)
		if n == nil {
			t.Fatalf("%s: no match", sel)
		}
		return n
	}
	cases := []struct{ sel, path, root string }{
		{`main > p\:stamp`, `#m > p\:stamp:nth-child(1)`, `html > body > main:nth-child(1) > p\:stamp:nth-child(1)`},
		{"span", `#\31 23 > span:nth-child(1)`, `html > body > main:nth-child(1) > div:nth-child(2) > span:nth-child(1)`},
		{"i", `#m > div:nth-child(4) > i:nth-child(1)`, `html > body > main:nth-child(1) > div:nth-child(4) > i:nth-child(1)`},
		{"li + li", `html > body > ul:nth-child(2) > li:nth-child(2)`, `html > body > ul:nth-child(2) > li:nth-child(2)`},
		{"body", `html > body`, `html > body`},
	}
	for _, c := range cases {
		n := find(c.sel)
		if got := selector.Path(doc, n); got != c.path {
			t.Errorf("%s: Path %q want %q", c.sel, got, c.path)
		}
		if got := selector.RootPath(n); got != c.root {
			t.Errorf("%s: RootPath %q want %q", c.sel, got, c.root)
		}
		for _, p := range []string{c.path, c.root} {
			if back := selector.MustCompile(p).MatchAll(doc); len(back) != 1 || back[0] != n {
				t.Errorf("%s: %q does not round-trip", c.sel, p)
			}
		}
	}
	for in, want := range map[string]string{"123": `\31 23`, "-1a": `-\31 a`, "-": `\-`, "a b": `a\ b`, "p:x": `p\:x`, "é": "é", "a\x00": "a\uFFFD"} {
		if got := selector.EscapeIdent(in); got != want {
			t.Errorf("EscapeIdent(%q) = %q want %q", in, got, want)
		}
	}
}

// TestCorpusCanonicalRoundTrip: for every element of every upstream
// document (research/upstream, git-ignored; skipped when absent) both
// canonical forms select exactly that element.
func TestCorpusCanonicalRoundTrip(t *testing.T) {
	root := filepath.Join("..", "..", "research", "upstream")
	if _, err := os.Stat(root); err != nil {
		t.Skip("research/upstream not present")
	}
	total := 0
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && (info.Name() == ".git" || info.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if !strings.HasSuffix(p, ".html") {
			return nil
		}
		b, _ := os.ReadFile(p)
		doc, _ := dom.Parse(b)
		for _, el := range selector.MustCompile("*").MatchAll(doc) {
			for _, s := range []string{selector.Path(doc, el), selector.RootPath(el)} {
				total++
				c, err := selector.Compile(s)
				if err != nil {
					t.Errorf("%s: %q: %v", p, s, err)
					continue
				}
				if m := c.MatchAll(doc); len(m) != 1 || m[0] != el {
					t.Errorf("%s: %q selects %d elements", p, s, len(m))
				}
			}
		}
		return nil
	})
	t.Logf("%d canonical selectors round-tripped", total)
}
