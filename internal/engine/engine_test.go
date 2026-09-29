package engine_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/site"
)

// oddDoc is formatted the way people write HTML by hand, which a
// re-serializing store would rewrite: single-quoted and unquoted attributes,
// bare boolean attributes, <br/>, character references, a comment with
// &amp;, a formatting element whose attributes x/net/html sorts, and
// irregular whitespace.
const oddDoc = "<!DOCTYPE html>\n<html lang=en>\n<head>\n  <meta charset='utf-8'>\n  <title>Odd &amp; formatted</title>\n</head>\n<body>\n" +
	"  <!-- a comment with &amp; and <b>markup</b> -->\n" +
	"  <ul id='list' class=items>\n" +
	"    <li id=a data-x='it&#39;s'>A<br/>line</li>\n" +
	"    <li id=b hidden>B</li>\n" +
	"  </ul>\n" +
	"  <p>Tom &amp; Jerry&nbsp;&copy; <a itemprop=\"url\" href=\"/x\">x</a></p>\n" +
	"  <input type=checkbox checked disabled>\n" +
	"\t<table><tbody id=rows><tr><td>1</td></tr></tbody></table>\n\n" +
	"</body>\n</html>\n"

type fixture struct {
	t   *testing.T
	s   *site.Site
	e   *engine.Engine
	ctx context.Context
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	reg, err := site.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.Close)
	ctx := context.Background()
	s, err := reg.Create(ctx, "test", site.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, s: s, e: &engine.Engine{}, ctx: ctx}
}

// write runs an authoring-plane write (no authorization rules involved).
func (f *fixture) write(op *engine.Op) *engine.Result {
	f.t.Helper()
	op.Plane = engine.Authoring
	res, err := f.e.Write(f.ctx, f.s, op)
	if err != nil {
		f.t.Fatalf("%s %s %s: %v", op.Method, op.Path, op.Range.Raw, err)
	}
	return res
}

func (f *fixture) put(path, ct, body string) {
	f.t.Helper()
	f.write(&engine.Op{Method: "PUT", Path: path, ContentType: ct, Body: []byte(body)})
}

func (f *fixture) sel(method, path, rng, body string) *engine.Result {
	f.t.Helper()
	return f.write(&engine.Op{Method: method, Path: path, Range: engine.ParseRange(rng), Body: []byte(body)})
}

func (f *fixture) stored(path string) string {
	f.t.Helper()
	d, err := f.s.Store.Get(f.ctx, path)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(d.Body)
}

// get performs a whole-document or selector GET through the engine.
func (f *fixture) get(path, rng string) *engine.ReadResult {
	f.t.Helper()
	snap, err := f.s.Index(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	d, err := f.s.Store.Get(f.ctx, path)
	if err != nil {
		f.t.Fatal(err)
	}
	res, err := f.e.ReadMarkup(f.ctx, f.s, snap, d, &engine.ReadOp{Plane: engine.Authoring, Method: "GET", Path: path, Range: engine.ParseRange(rng)}, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	return res
}

// replaceOnce replaces exactly one occurrence of old, failing otherwise.
func replaceOnce(t *testing.T, s, old, new string) string {
	t.Helper()
	if n := strings.Count(s, old); n != 1 {
		t.Fatalf("%q occurs %d times", old, n)
	}
	return strings.Replace(s, old, new, 1)
}

// HTML is stored in PageLove's serialized form (live 2026-09-29, LO-15):
// a whole-document PUT stores and serves the normalized document, and each
// selector write changes the element it addresses, with everything else,
// whitespace included, kept as stored.
func TestHTMLIsStoredSerialized(t *testing.T) {
	f := newFixture(t)
	const p = "/odd.html"
	f.put(p, "text/html", oddDoc)
	doc, _ := dom.Parse([]byte(oddDoc))
	want := string(dom.Render(doc))
	if got := f.stored(p); got != want {
		t.Fatalf("PUT stores the serialized form:\n got %q\nwant %q", got, want)
	}
	if got := f.get(p, ""); string(got.Body) != want {
		t.Fatalf("whole-document GET serves the stored bytes:\n%q", got.Body)
	}
	for _, form := range []string{`<html lang="en">`, `<meta charset="utf-8">`, "<!-- a comment with &amp; and <b>markup</b> -->",
		"Jerry\u00a0© <a itemprop=\"url\" href=\"/x\">", `<input type="checkbox" checked disabled>`, "\t<table>", "\n\n</body>\n</html>\n"} {
		if !strings.Contains(want, form) {
			t.Errorf("serialized form lacks %q", form)
		}
	}

	f.sel("PUT", p, "selector=#b", `<li id='b' class=new>B2</li>`)
	want = replaceOnce(t, want, `<li id="b" hidden>B</li>`, `<li id="b" class="new">B2</li>`)
	if got := f.stored(p); got != want {
		t.Fatalf("PUT:\n got %q\nwant %q", got, want)
	}

	// POST (append) inserts before the end tag, the body's surrounding
	// whitespace included (the docs' getting-started read-back shows the
	// request's trailing newline in the stored list).
	f.sel("POST", p, "selector=#list", "<li id=c>C<br/></li>\n")
	want = replaceOnce(t, want, "B2</li>\n  </ul>", "B2</li>\n  <li id=\"c\">C<br></li>\n</ul>")
	if got := f.stored(p); got != want {
		t.Fatalf("POST append:\n got %q\nwant %q", got, want)
	}

	f.sel("POST", p, "selector=#a; placement=before", `<li id=z>Z</li>`)
	want = replaceOnce(t, want, `<li id="a" `, `<li id="z">Z</li><li id="a" `)
	f.sel("POST", p, "selector=#rows; placement=prepend", `<tr><td>0</td></tr>`)
	want = replaceOnce(t, want, `<tbody id="rows">`, `<tbody id="rows"><tr><td>0</td></tr>`)
	if got := f.stored(p); got != want {
		t.Fatalf("POST before/prepend:\n got %q\nwant %q", got, want)
	}

	// DELETE removes the element and nothing else (the whitespace around it
	// stays, as in the docs' "Verify the removal" example).
	f.sel("DELETE", p, "selector=#a", "")
	want = replaceOnce(t, want, `<li id="a" data-x="it's">A<br>line</li>`, "")
	if got := f.stored(p); got != want {
		t.Fatalf("DELETE:\n got %q\nwant %q", got, want)
	}

	f.write(&engine.Op{Method: "MOVE", Path: p, Range: engine.ParseRange("selector=#c"),
		Destination: p, DestinationRange: engine.ParseRange("selector=#z; placement=before")})
	want = replaceOnce(t, want, `<li id="c">C<br></li>`, "")
	want = replaceOnce(t, want, `<li id="z">`, `<li id="c">C<br></li><li id="z">`)
	if got := f.stored(p); got != want {
		t.Fatalf("MOVE:\n got %q\nwant %q", got, want)
	}
	if got := f.get(p, ""); string(got.Body) != want {
		t.Errorf("whole-document GET after writes:\n got %q\nwant %q", got.Body, want)
	}
	if got := f.get(p, "selector=#c"); string(got.Body) != `<li id="c">C<br></li>` || got.Status != http.StatusPartialContent {
		t.Errorf("selector GET: %d %q", got.Status, got.Body)
	}
	if got := f.get(p, "selector=input"); string(got.Body) != `<input type="checkbox" checked disabled>` {
		t.Errorf("void element GET: %q", got.Body)
	}
}

// An unclosed <li> in a request body is closed where the body ends.
func TestUnclosedBodyIsClosed(t *testing.T) {
	f := newFixture(t)
	const p = "/list.html"
	src := "<ul id='l'>\n  <li>a</li>\n</ul>\n"
	f.put(p, "text/html", src)
	f.sel("POST", p, "selector=#l; placement=prepend", "<li class=new>x")
	want := "<ul id=\"l\"><li class=\"new\">x</li>\n  <li>a</li>\n</ul>\n"
	if got := f.stored(p); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// PageLove's model does not foster-parent: content inside <table> stays
// where it was written, and a top-level element of a document with several
// is replaced in place.
func TestNoFosterParenting(t *testing.T) {
	f := newFixture(t)
	const p = "/foster.html"
	f.put(p, "text/html", "<table><tr><td>a</td></tr><div id=d>x</div></table><p id=q>q</p>")
	f.sel("PUT", p, "selector=#q", "<p id=q>Q</p>")
	got := f.stored(p)
	want := `<table><tr><td>a</td></tr><div id="d">x</div></table><p id="q">Q</p>`
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestPreformattedLeadingNewlineSurvives(t *testing.T) {
	f := newFixture(t)
	const p = "/pre.html"
	f.put(p, "text/html", "<div id=d><pre>\n\nkeep</pre></div>")
	for i := 0; i < 3; i++ {
		f.sel("POST", p, "selector=#d", "<pre>\n\nnew</pre>")
	}
	if got := f.stored(p); got != "<div id=\"d\"><pre>\n\nkeep</pre><pre>\n\nnew</pre><pre>\n\nnew</pre><pre>\n\nnew</pre></div>" {
		t.Errorf("got %q", got)
	}
}

// Selector writes never address elements inside <template> contents.
func TestTemplateContentsAreNotTargets(t *testing.T) {
	f := newFixture(t)
	const p = "/tpl.html"
	f.put(p, "text/html", "<template><li>t</li></template><ul><li>real</li></ul>")
	f.sel("PUT", p, "selector=li", "<li>changed</li>")
	if got := f.stored(p); got != "<template><li>t</li></template><ul><li>changed</li></ul>" {
		t.Errorf("got %q", got)
	}
}

// Prefixed elements stay where the author put them, and a write next to a
// self-closed one splices without swallowing its siblings.
func TestPrefixedElementsInStoredDocuments(t *testing.T) {
	f := newFixture(t)
	const p = "/inc.html"
	src := "<body><p:include selector=\"#nav\" resource=\"/x\" />\n  <main id=m><p>Page</p></main>\n<table id=t><p:include resource='/r' selector='#rows'></p:include></table></body>"
	f.put(p, "text/html", src)
	f.sel("POST", p, "selector=#m", "<p>More</p>")
	if got := f.stored(p); got != "<body><p:include selector=\"#nav\" resource=\"/x\"></p:include>\n  <main id=\"m\"><p>Page</p><p>More</p></main>\n<table id=\"t\"><p:include resource=\"/r\" selector=\"#rows\"></p:include></table></body>" {
		t.Errorf("got %q", got)
	}
	if n := f.get(p, `selector=#t > p\:include`); n.Status != http.StatusPartialContent {
		t.Errorf("include inside <table> not kept in place: %d", n.Status)
	}
}

const atomDoc = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xmlns:p="https://pagelove.org/1.0">
  <title type='text'>Feed &amp; more</title>
  <entry><id>urn:1</id><content type="html"><![CDATA[<b>bold</b>]]></content></entry>
  <Entry><id>urn:X</id></Entry>
  <p:stamp site></p:stamp>
</feed>
`

func TestXMLDocuments(t *testing.T) {
	f := newFixture(t)
	const p = "/feed.xml"
	f.put(p, "application/atom+xml", atomDoc)
	if got := f.get(p, ""); string(got.Body) != atomDoc {
		t.Fatalf("whole-document GET: %q", got.Body)
	}
	// Selectors are case-sensitive on XML names.
	if got := f.get(p, "selector=Entry > id"); string(got.Body) != "<id>urn:X</id>" {
		t.Errorf("selector GET: %q", got.Body)
	}
	if got := f.get(p, "selector=entry"); string(got.Body) != `<entry><id>urn:1</id><content type="html"><![CDATA[<b>bold</b>]]></content></entry>` {
		t.Errorf("XML fragment GET: %q", got.Body)
	}
	// POST is parsed as XML in the feed's namespace and spliced in.
	res := f.sel("POST", p, "selector=feed", "<entry><id>urn:2</id><empty></empty></entry>")
	if string(res.Body) != "<entry><id>urn:2</id><empty/></entry>" {
		t.Errorf("POST response: %q", res.Body)
	}
	want := replaceOnce(t, atomDoc, "</p:stamp>\n</feed>", "</p:stamp>\n<entry><id>urn:2</id><empty></empty></entry></feed>")
	if got := f.stored(p); got != want {
		t.Fatalf("POST:\n got %q\nwant %q", got, want)
	}
	doc, err := dom.ParseXML([]byte(want))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range selector.MustCompile("feed > entry").MatchAll(doc) {
		if e.Namespace != "http://www.w3.org/2005/Atom" {
			t.Errorf("entry namespace %q", e.Namespace)
		}
	}
	// Appending into an empty-element tag cannot be spliced inside it; the
	// element is rewritten and everything else is kept.
	f.sel("POST", p, "selector=empty", "<x>1</x>")
	want = replaceOnce(t, want, "<empty></empty>", "<empty><x>1</x></empty>")
	if got := f.stored(p); got != want {
		t.Fatalf("POST into empty element:\n got %q\nwant %q", got, want)
	}
	f.put("/self.xml", "application/xml", `<r><a/><b>t</b></r>`)
	f.sel("POST", "/self.xml", "selector=a", "<c/>")
	if got := f.stored("/self.xml"); got != `<r><a><c/></a><b>t</b></r>` {
		t.Errorf("POST into self-closed element: %q", got)
	}
	f.sel("DELETE", p, "selector=Entry", "")
	if got := f.stored(p); got != replaceOnce(t, want, "<Entry><id>urn:X</id></Entry>", "") {
		t.Errorf("DELETE: %q", got)
	}
}

// Replacing <body> keeps the document's one head.
func TestReplaceBodyKeepsOneHead(t *testing.T) {
	f := newFixture(t)
	const p = "/b.html"
	src := "<!DOCTYPE html>\n<html><head><title>t</title></head>\n<body class=a><p>old</p></body></html>"
	f.put(p, "text/html", src)
	f.sel("PUT", p, "selector=body", "<body class=b><p>new</p></body>")
	if got, want := f.stored(p), strings.Replace(src, "<body class=a><p>old</p></body>", "<body class=\"b\"><p>new</p></body>", 1); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	f.sel("PUT", p, "selector=body", "<p>bare</p>")
	doc, err := dom.Parse([]byte(f.stored(p)))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(selector.MustCompile("head").MatchAll(doc)); n != 1 {
		t.Errorf("%d heads in %q", n, f.stored(p))
	}
	// No body is synthesized around the replacement (LO-15).
	if got := f.get(p, "selector=html > p"); string(got.Body) != "<p>bare</p>" {
		t.Errorf("replacement %q", got.Body)
	}
}
