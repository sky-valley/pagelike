package compose_test

import (
	"strings"
	"testing"

	"github.com/sky-valley/pagelike/internal/jsglue"
)

// Composition splices its results into the stored bytes, which are in
// PageLove's serialized form (LO-15): everything no directive touches is
// served as stored, comments included.
func TestStoredFormServed(t *testing.T) {
	p := newPlane(t)
	p.allow("GET")
	page := "<!DOCTYPE html>\n<html lang=en xmlns:p=\"https://pagelove.org/1.0\">\n<head><title>A &amp; B</title></head>\n<body>\n" +
		"<!-- kept &amp; verbatim -->\n<p class='q' data-x=&copy;>caf&eacute;<br/>line</p>\n" +
		"<ul id=\"l\" p:template=\"text/liquid\">{% for i in (1..2) %}<li title='{{ i }}'>&#39;{{ i }}&#39;</li>{% endfor %}</ul>\n" +
		"</body>\n</html>\n"
	p.author("/page.html", page)
	r := p.do("GET", "/page.html", "", nil)
	// An unquoted value is kept as written (&copy; is not decoded there),
	// and &eacute; and &#39; are decoded in text (LO-15).
	want := "<!DOCTYPE html>\n<html lang=\"en\">\n<head><title>A &amp; B</title></head>\n<body>\n" +
		"<!-- kept &amp; verbatim -->\n<p class=\"q\" data-x=\"&amp;copy;\">café<br>line</p>\n" +
		"<ul id=\"l\"><li title=\"1\">'1'</li><li title=\"2\">'2'</li></ul>\n" +
		"</body>\n</html>\n"
	if r.status != 200 || r.body != want {
		t.Fatalf("%d\n%s\nwant\n%s", r.status, r.body, want)
	}
	if cc := r.header.Get("Cache-Control"); cc != "public, max-age=5" {
		t.Errorf("shareable composed page Cache-Control %q", cc)
	}
	// Fragments of a shareable page carry no Cache-Control (R-RW-102).
	f := p.do("GET", "/page.html", "", map[string]string{"Range": "selector=#l li:nth-child(2)"})
	// A fragment read serves the element's bytes as composed.
	f.must(t, 206)
	if f.body != `<li title="2">'2'</li>` {
		t.Errorf("fragment %q", f.body)
	}
	if cc := f.header.Get("Cache-Control"); cc != "" {
		t.Errorf("fragment Cache-Control %q", cc)
	}
	// Reads do not change the stored document.
	if s := p.stored("/page.html"); !strings.Contains(s, `p:template="text/liquid">{% for i in (1..2) %}<li title="{{ i }}">'{{ i }}'</li>{% endfor %}</ul>`) {
		t.Errorf("stored changed:\n%s", s)
	}
}

// A document with nothing to compose is served as stored, with its stored
// ETag and no caching directive.
func TestPlainDocumentUnchanged(t *testing.T) {
	p := newPlane(t)
	p.allow("GET")
	page := "<!DOCTYPE html>\n<html><body><div t:foo=\"bar\" data-u='x'>{{ not liquid }}</div></body></html>\n"
	p.author("/plain.html", page)
	r := p.do("GET", "/plain.html", "", nil)
	r.must(t, 200)
	if r.body != p.stored("/plain.html") || r.header.Get("Cache-Control") != "" {
		t.Errorf("%q %q", r.body, r.header.Get("Cache-Control"))
	}
}

func TestBindingsIncludesStamps(t *testing.T) {
	p := newPlane(t)
	p.allow("GET")
	p.author("/data/a.html", `<!DOCTYPE html><html><body><article id="rec-a" class="rec" itemscope itemtype="urn:t:Rec"><h1 itemprop="title">Alpha &amp; co</h1><meta itemprop="n" content="2"></article></body></html>`)
	p.author("/data/b.html", `<!DOCTYPE html><html><body><article id="rec-b" itemscope itemtype="urn:t:Rec"><h1 itemprop="title">Beta</h1><meta itemprop="n" content="3"></article></body></html>`)
	p.author("/partials/nav.html", `<!DOCTYPE html><html><body><nav id="nav">Nav <b>bold</b></nav></body></html>`)
	p.author("/page.html", `<!DOCTYPE html>
<html xmlns:p="https://pagelove.org/1.0" xmlns:r="https://pagelove.org/Binding/CSS" xmlns:e="https://pagelove.org/Binding/Sessel">
<body e:first="${[itemtype='urn:t:Rec']}.first()" e:total="${[itemtype='urn:t:Rec'] [itemprop=n]}.sum()">
<p:include selector="#nav" resource="/partials/*"/>
<main><p:stamp first></p:stamp></main>
<ol id="list" r:recs="[itemtype='urn:t:Rec']" p:template="text/liquid">{% for r in recs %}<li>{{ r.title }}|{{ r['@id'] }}</li>{% endfor %}</ol>
<p id="total"><p:stamp total></p:stamp></p>
<p id="none"><p:stamp missing></p:stamp>x</p>
</body>
</html>
`)
	r := p.do("GET", "/page.html", "", nil)
	r.must(t, 200,
		`<nav id="nav">Nav <b>bold</b></nav>`,
		`<main><article id="rec-a" class="rec" itemscope itemtype="urn:t:Rec"><h1 itemprop="title">Alpha &amp; co</h1>`,
		`<li>Alpha &amp; co|/data/a.html#rec-a</li><li>Beta|/data/b.html#rec-b</li>`,
		`<p id="total">5</p>`,
		`<p id="none">x</p>`)
	r.mustNot(t, "p:include", "p:stamp", "xmlns:", "r:recs", "e:first", "p:template")
}

func TestCompositionErrors(t *testing.T) {
	p := newPlane(t)
	p.allow("GET")
	p.author("/part.html", `<!DOCTYPE html><html><body><i class="d">1</i><i class="d">2</i></body></html>`)
	cases := []struct {
		body   string
		status int
	}{
		{`<html xmlns:p="https://pagelove.org/1.0"><body><p:include selector="#nope" resource="/part.html"></p:include></body></html>`, 404},
		{`<html xmlns:p="https://pagelove.org/1.0"><body><p:include selector=".d" resource="/part.html"></p:include></body></html>`, 500},
		{`<html xmlns:p="https://pagelove.org/1.0"><body><p:include resource="/part.html"></p:include></body></html>`, 500},
		// An unbound prefix is an ordinary element (live 2026-09-29).
		{`<html><body><t:foo></t:foo></body></html>`, 200},
		{`<html xmlns:p="https://pagelove.org/1.0"><body><p:nope></p:nope></body></html>`, 500},
		{`<html xmlns:e="https://pagelove.org/Binding/Sessel"><body e:x="nosuch + 1"></body></html>`, 500},
		{`<html xmlns:e="https://pagelove.org/Binding/Sessel"><body e:x="((("></body></html>`, 500},
		{`<html xmlns:r="https://pagelove.org/Binding/CSS"><body r:x="[[["></body></html>`, 500},
		{`<html xmlns:p="https://pagelove.org/1.0"><body><p p:template="text/mustache">x</p></body></html>`, 500},
		{`<html xmlns:p="https://pagelove.org/1.0"><body><p p:template="text/liquid">{% if %}</p></body></html>`, 500},
		{`<html xmlns:p="https://pagelove.org/1.0"><body><ul p:paginate="zero"><li>a</li></ul></body></html>`, 422},
		// A j: expression that throws fails composition (R-JS-36).
		{`<html xmlns:j="https://pagelove.org/Binding/JavaScript"><body j:x="(() => { throw new Error('x'); })()"></body></html>`, 500},
		// An include of the page itself recurses until the dispatch budget.
		{`<html xmlns:p="https://pagelove.org/1.0"><body><div id="loop"><p:include selector="#loop" resource="/e.html"></p:include></div></body></html>`, 503},
	}
	for i, c := range cases {
		p.author("/e.html", c.body)
		r := p.do("GET", "/e.html", "", nil)
		if r.status != c.status {
			t.Errorf("case %d: status %d, want %d\n%s", i, r.status, c.status, r.body)
		}
		// A failed composition also fails fragment reads.
		if f := p.do("GET", "/e.html", "", map[string]string{"Range": "selector=body"}); f.status != c.status && !(c.status == 200 && f.status == 206) {
			t.Errorf("case %d (fragment): status %d, want %d", i, f.status, c.status)
		}
	}
}

func TestWriteThrough(t *testing.T) {
	p := newPlane(t)
	p.allow("GET", "PUT", "POST", "DELETE")
	origin := `<!DOCTYPE html>
<html><body>
<header id="nav">Nav v1</header>
<article id="rec-1" itemscope itemtype="urn:t:Rec"><h1 itemprop="title">One</h1><ul id="notes"></ul></article>
<p id="keep">keep</p>
</body></html>
`
	p.author("/data/o.html", origin)
	page := `<!DOCTYPE html>
<html xmlns:p="https://pagelove.org/1.0" xmlns:e="https://pagelove.org/Binding/Sessel">
<body e:rec="${[itemtype='urn:t:Rec']}.first()">
<p:include resource="/data/o.html" selector="header#nav"></p:include>
<main><p:stamp rec></p:stamp><ul id="native"></ul></main>
<ol id="gen" p:template="text/liquid"><li id="g1">{{ 1 }}</li></ol>
</body>
</html>
`
	p.author("/page.html", page)
	h := map[string]string{"Content-Type": "text/html"}
	with := func(rng string) map[string]string {
		m := map[string]string{"Range": "selector=" + rng}
		for k, v := range h {
			m[k] = v
		}
		return m
	}
	p.do("PUT", "/page.html", `<header id="nav">Nav v2</header>`, with("header#nav")).must(t, 206, "Nav v2")
	p.do("PUT", "/page.html", `<h1 itemprop="title">Two</h1>`, with("#rec-1 h1")).must(t, 206)
	p.do("POST", "/page.html", `<li>note</li>`, with("#notes")).must(t, 206)
	p.do("POST", "/page.html", `<li>native</li>`, with("#native")).must(t, 206)
	p.do("PUT", "/page.html", `<li id="g1">x</li>`, with("#g1")).must(t, 416)
	p.do("PUT", "/page.html", `<li>x</li>`, with("#absent")).must(t, 416)

	o := p.stored("/data/o.html")
	for _, want := range []string{`<header id="nav">Nav v2</header>`, `<h1 itemprop="title">Two</h1>`, `<ul id="notes"><li>note</li></ul>`, `<p id="keep">keep</p>`} {
		if !strings.Contains(o, want) {
			t.Errorf("origin lacks %q:\n%s", want, o)
		}
	}
	s := p.stored("/page.html")
	if !strings.Contains(s, `<ul id="native"><li>native</li></ul>`) || strings.Contains(s, "Nav v2") || !strings.Contains(s, "<p:stamp rec>") {
		t.Errorf("page:\n%s", s)
	}
	p.do("DELETE", "/page.html", "", map[string]string{"Range": "selector=#rec-1"}).must(t, 204)
	if o := p.stored("/data/o.html"); strings.Contains(o, "rec-1") || !strings.Contains(o, "keep") {
		t.Errorf("origin after delete:\n%s", o)
	}
	// A failed include blocks writes to the page (R-COMP-94).
	p.author("/broken.html", `<html xmlns:p="https://pagelove.org/1.0"><body><p:include selector="#missing"></p:include><ul id="l"></ul></body></html>`)
	p.do("POST", "/broken.html", `<li>x</li>`, with("#l")).must(t, 404)
}

func TestRoutes(t *testing.T) {
	p := newPlane(t)
	p.allow("GET", "PUT", "POST")
	p.author("/data/hello.html", `<!DOCTYPE html><html><body><article id="post-hello" itemscope itemtype="urn:t:Post"><meta itemprop="slug" content="hello"><ul id="comments"></ul></article></body></html>`)
	p.author("/posts/:slug.html", `<!DOCTYPE html>
<html xmlns:p="https://pagelove.org/1.0" xmlns:e="https://pagelove.org/Binding/Sessel">
<body e:post="${[itemtype='urn:t:Post']:has([itemprop=slug]:value-equals(request.params.slug))}.first()">
<main><p:stamp post></p:stamp><div id="missing">Not found</div></main>
<p id="slug" p:template="text/liquid">{{ request.params.slug }}|{{ request.path }}</p>
</body>
</html>
`)
	p.author("/m/:a/q/v.html", `<html><body>MORE</body></html>`)
	p.author("/m/:a/:b/v.html", `<html><body>LESS</body></html>`)

	r := p.do("GET", "/posts/hello.html", "", nil)
	r.must(t, 200, `id="post-hello"`, `<p id="slug">hello|/posts/hello.html</p>`)
	if got := r.header.Get("Route-Parameters"); got != "slug=hello" {
		t.Errorf("route-parameters %q", got)
	}
	p.do("GET", "/posts/other.html", "", nil).must(t, 200, `<div id="missing">Not found</div>`)
	p.do("GET", "/posts/hello.html", "", map[string]string{"Range": "selector=main"}).must(t, 404)
	p.do("GET", "/m/1/q/v.html", "", nil).must(t, 200, "MORE")
	p.do("GET", "/m/1/z/v.html", "", nil).must(t, 200, "LESS")

	h := map[string]string{"Content-Type": "text/html", "Range": "selector=#comments"}
	p.do("POST", "/posts/hello.html", `<li>Nice</li>`, h).must(t, 206)
	if !strings.Contains(p.stored("/data/hello.html"), "<li>Nice</li>") {
		t.Error("the comment did not land in the data file")
	}
	h["Range"] = "selector=#missing"
	p.do("POST", "/posts/hello.html", `<li>no</li>`, h).must(t, 416)
	if strings.Contains(p.stored("/posts/:slug.html"), "<li>") {
		t.Error("the template was written through a concrete URL")
	}
}

func TestPagination(t *testing.T) {
	p := newPlane(t)
	p.allow("GET")
	p.author("/c.html", "<!DOCTYPE html>\n<html>\n<head><title>C</title></head>\n<body>\n<ul id=\"contacts\" xmlns:p=\"https://pagelove.org/1.0\" p:paginate=\"10\">\n<li>A</li>\n<li>B</li>\n<li>C</li>\n<li>D</li>\n<li>E</li>\n</ul>\n</body>\n</html>\n")
	// Links name the request path and this paginator's (id-prefixed) page,
	// in the order first, last, prev, next (live 2026-09-29), and keep the
	// request's other parameters (kept standard: live drops them).
	r := p.do("GET", "/c.html?q=x&paginate:page=2&paginate:length=2", "", nil)
	r.must(t, 200,
		"<ul id=\"contacts\">\n\n\n<li>C</li>\n<li>D</li>\n\n</ul>",
		`<title>C</title><link rel="first" href="/c.html?q=x&amp;paginate:length=2&amp;paginate:contacts:page=1" title="contacts"><link rel="last" href="/c.html?q=x&amp;paginate:length=2&amp;paginate:contacts:page=3" title="contacts"><link rel="prev" href="/c.html?q=x&amp;paginate:length=2&amp;paginate:contacts:page=1" title="contacts"><link rel="next" href="/c.html?q=x&amp;paginate:length=2&amp;paginate:contacts:page=3" title="contacts"></head>`)
	links := r.header.Values("Link")
	if len(links) != 4 || links[0] != `</c.html?q=x&paginate:length=2&paginate:contacts:page=1>; rel="first"; title="contacts"` ||
		links[3] != `</c.html?q=x&paginate:length=2&paginate:contacts:page=3>; rel="next"; title="contacts"` {
		t.Errorf("Link %q", links)
	}
	// The id-prefixed parameters win over the unprefixed ones.
	p.do("GET", "/c.html?paginate:page=1&paginate:contacts:page=3&paginate:contacts:length=2", "", nil).must(t, 200, "<li>E</li>")
	p.do("GET", "/c.html?paginate:page=9&paginate:length=2", "", nil).must(t, 200, "<li>E</li>")
	f := p.do("GET", "/c.html?paginate:length=2", "", map[string]string{"Range": "selector=#contacts"})
	f.must(t, 206, "<li>A</li>")
	f.mustNot(t, "<li>C</li>")

	// Several paginators without ids are allowed and share the unprefixed
	// parameters; without a <head> in the source only Link headers carry
	// the links.
	p.author("/two.html", `<html xmlns:p="https://pagelove.org/1.0"><body><ul p:paginate="1"><li>a</li><li>b</li></ul><ul p:paginate="1"><li>c</li><li>d</li></ul></body></html>`)
	two := p.do("GET", "/two.html?paginate:page=2", "", nil)
	two.must(t, 200, "<ul><li>b</li></ul><ul><li>d</li></ul>")
	two.mustNot(t, "<link")
	if got := two.header.Values("Link"); len(got) != 6 || got[0] != `</two.html?paginate:page=1>; rel="first"` || got[2] != `</two.html?paginate:page=1>; rel="prev"` || got[3] != got[0] {
		t.Errorf("Link %q", got)
	}
}

func TestTemplatedCreation(t *testing.T) {
	p := newPlane(t)
	p.allow("GET", "POST", "PUT")
	p.author("/t/new.html", `<!DOCTYPE html>
<html lang="en" xmlns:p="https://pagelove.org/1.0" p:template="text/liquid">
<head>
<title>{{ request.body.title }}</title>
<base href="../entries/{{ request.body.slug }}.html">
</head>
<body><h1>{{ request.body.title | escape }}</h1><p>{{ request.method }}</p></body>
</html>
`)
	form := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	r := p.do("POST", "/t/new.html", "slug=hi&title=Caf%C3%A9+%26+co", form)
	r.must(t, 301)
	if loc := r.header.Get("Location"); loc != "/entries/hi.html" {
		t.Errorf("Location %q", loc)
	}
	want := "<!DOCTYPE html>\n<html lang=\"en\" xmlns:p=\"https://pagelove.org/1.0\">\n<head>\n<title>Café &amp; co</title>\n\n</head>\n<body><h1>Café &amp; co</h1><p>POST</p></body>\n</html>\n"
	if got := p.stored("/entries/hi.html"); got != want {
		t.Errorf("stored:\n%q\nwant\n%q", got, want)
	}
	p.author("/t/nobase.html", `<html xmlns:p="https://pagelove.org/1.0" p:template="text/liquid"><head><base href="{{ request.body.none }}"></head><body></body></html>`)
	p.do("POST", "/t/nobase.html", "a=b", form).must(t, 422)
}

func TestTemplatedCreationNeedsPutAtTarget(t *testing.T) {
	p := newPlane(t)
	p.allow("GET", "POST")
	p.author("/t/new.html", `<html xmlns:p="https://pagelove.org/1.0" p:template="text/liquid"><head><base href="/x/{{ request.body.s }}.html"></head><body></body></html>`)
	p.do("POST", "/t/new.html", "s=a", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}).must(t, 401)
	if r := p.do("GET", "/x/a.html", "", map[string]string{"plane": "dav"}); r.status != 404 {
		t.Errorf("stored anyway: %d", r.status)
	}
}

func TestXMLComposition(t *testing.T) {
	p := newPlane(t)
	p.allow("GET")
	p.author("/feed.xml", `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:e="https://pagelove.org/Binding/Sessel" xmlns:p="https://pagelove.org/1.0" e:site="'Blog &amp; co'">
  <title><p:stamp site></p:stamp></title>
  <atom:link href="/feed.xml"></atom:link>
  <Entry></Entry>
</feed>
`, "application/atom+xml")
	r := p.do("GET", "/feed.xml", "", nil)
	r.must(t, 200, "<?xml", "<title>Blog &amp; co</title>", `xmlns:p="https://pagelove.org/1.0"`, "<Entry/>", `<atom:link href="/feed.xml"/>`)
	r.mustNot(t, "p:stamp", "e:site")
	if ct := r.header.Get("Content-Type"); ct != "application/atom+xml" {
		t.Errorf("Content-Type %q", ct)
	}
}

func TestMethods(t *testing.T) {
	p := newPlane(t)
	p.allow("GET", "PUT")
	schema := `<div hidden itemscope itemtype="https://pagelove.org/Schema"><meta itemprop="type" content="urn:t:M">
<div itemprop="property" itemscope itemtype="https://pagelove.org/Method"><meta itemprop="name" content="hello"><meta itemprop="returns" content="https://pagelove.org/Element">
<div itemprop="parameter" itemscope itemtype="https://pagelove.org/Parameter"><meta itemprop="name" content="who"></div>
<div itemprop="implementation" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source" type="text/sessel">new p { "hello " + who }</script></div></div>
<div itemprop="property" itemscope itemtype="https://pagelove.org/Method"><meta itemprop="name" content="doesNotUnderstand">
<div itemprop="implementation" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source" type="text/sessel">Context[messageName] = parameters[0] + "!"; null</script></div></div>
<div itemprop="property" itemscope itemtype="https://pagelove.org/Method"><meta itemprop="name" content="js"><meta itemprop="returns" content="https://pagelove.org/Element">
<div itemprop="implementation" itemscope itemtype="https://pagelove.org/JavaScript/Module"><script itemprop="source" type="module">export default () => null</script></div></div>
</div>`
	p.author("/m.html", `<!DOCTYPE html><html xmlns:t="urn:t:M" xmlns:p="https://pagelove.org/1.0"><body>
<main><t:hello who="Ada" other="x"></t:hello><div id="h" p:template="text/liquid" t:greeting="hi">[{{ greeting }}]</div></main>
`+schema+`</body></html>`)
	r := p.do("GET", "/m.html", "", map[string]string{"Range": "selector=main"})
	r.must(t, 206, "<p>hello Ada</p>", `<div id="h">[hi!]</div>`)
	r.mustNot(t, "t:hello", "t:greeting", "other")
	// A JavaScript method returning null removes its element (R-JS-60).
	p.author("/js.html", `<!DOCTYPE html><html xmlns:t="urn:t:M"><body><main><t:js></t:js></main>`+schema+`</body></html>`)
	p.do("GET", "/js.html", "", map[string]string{"Range": "selector=main"}).must(t, 206, "<main></main>")
}

// Without a JavaScript runtime every JavaScript slot of composition fails
// clearly with 501, never silently.
func TestNoJavaScriptRuntime(t *testing.T) {
	jsglue.Uninstall()
	t.Cleanup(jsglue.Install)
	p := newPlane(t)
	p.allow("GET")
	p.author("/j.html", `<html xmlns:j="https://pagelove.org/Binding/JavaScript"><body j:x="1 + 1"></body></html>`)
	p.do("GET", "/j.html", "", nil).must(t, 501, "JavaScriptUnavailable")
	p.author("/m.html", `<!DOCTYPE html><html xmlns:t="urn:t:JS"><body><main><t:js></t:js></main>
<div hidden itemscope itemtype="https://pagelove.org/Schema"><meta itemprop="type" content="urn:t:JS">
<div itemprop="property" itemscope itemtype="https://pagelove.org/Method"><meta itemprop="name" content="js">
<div itemprop="implementation" itemscope itemtype="https://pagelove.org/JavaScript/Module"><script itemprop="source" type="module">export default () => null</script></div></div>
</div></body></html>`)
	p.do("GET", "/m.html", "", nil).must(t, 501, "JavaScriptUnavailable")
}

// A created document keeps its read-time directives (pagination, transient
// markers): they describe the stored page, not the creation request.
func TestTemplatedCreationKeepsReadTimeDirectives(t *testing.T) {
	p := newPlane(t)
	p.allow("GET", "POST", "PUT")
	p.author("/t/list.html", `<html xmlns:p="https://pagelove.org/1.0" p:template="text/liquid"><head><base href="/l/{{ request.body.s }}.html"></head><body><ul id="u" p:paginate="1"><li>a</li><li>b</li></ul><ul id="c" p:transient></ul></body></html>`)
	p.do("POST", "/t/list.html", "s=x", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}).must(t, 301)
	got := p.stored("/l/x.html")
	want := `<html xmlns:p="https://pagelove.org/1.0"><head></head><body><ul id="u" p:paginate="1"><li>a</li><li>b</li></ul><ul id="c" p:transient></ul></body></html>`
	if got != want {
		t.Errorf("stored\n%s\nwant\n%s", got, want)
	}
	p.do("GET", "/l/x.html", "", nil).must(t, 200, `<ul id="u"><li>a</li></ul>`)
}

func TestRequestDocumentAndPrivacy(t *testing.T) {
	p := newPlane(t)
	p.allow("GET")
	p.author("/r.html", `<html xmlns:p="https://pagelove.org/1.0"><body><div id="m"><p:include selector="[itemtype='https://pagelove.org/Request'] > meta[itemprop=method]"></p:include></div></body></html>`)
	r := p.do("GET", "/r.html?x=1", "", nil)
	r.must(t, 200, `<meta itemprop="method" content="GET">`)
	if r.header.Get("Cache-Control") != "private" {
		t.Errorf("Cache-Control %q", r.header.Get("Cache-Control"))
	}
	p.author("/s.html", `<html xmlns:e="https://pagelove.org/Binding/Sessel" xmlns:p="https://pagelove.org/1.0"><body e:who="request.auth.claims.email" e:path="request.path"><p id="a"><p:stamp who></p:stamp>|<p:stamp path></p:stamp></p></body></html>`)
	r = p.do("GET", "/s.html", "", nil)
	r.must(t, 200, `<p id="a">|/s.html</p>`)
	if r.header.Get("Cache-Control") != "private" {
		t.Errorf("reading request.auth: Cache-Control %q", r.header.Get("Cache-Control"))
	}
}

func TestSelectorFunctionsInRange(t *testing.T) {
	p := newPlane(t)
	p.allow("GET")
	p.author("/fn.html", `<html><body><ol id="l"><li>1</li><li>2</li><li>3</li></ol><i data-k>x</i><i data-k>y</i><b data-t>two</b><p data-v="two" id="t">T</p></body></html>`)
	r := p.do("GET", "/fn.html", "", map[string]string{"Range": "selector=#l li:nth-child(count([data-k]))"})
	r.must(t, 206, "<li>2</li>")
	if cr := r.header.Get("Content-Range"); cr != "selector #l li:nth-child(count([data-k]))" {
		t.Errorf("Content-Range %q", cr)
	}
	p.do("GET", "/fn.html", "", map[string]string{"Range": "selector=[data-v=text-of([data-t])]"}).must(t, 206, `id="t"`)
	p.do("GET", "/fn.html", "", map[string]string{"Range": "selector=[data-v=text-of([data-k])]"}).must(t, 422)
}
