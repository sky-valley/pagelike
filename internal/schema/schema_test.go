package schema_test

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/query"
	"github.com/sky-valley/pagelike/internal/schema"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

type fixture struct {
	t   *testing.T
	ctx context.Context
	s   *site.Site
	e   *engine.Engine
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
	e := &engine.Engine{}
	schema.Install(e)
	f := &fixture{t: t, ctx: ctx, s: s, e: e}
	// Writes run on the public plane (a WebDAV PUT is stored unvalidated,
	// live 2026-09-29) as fixtureWriter, whom the fixture's rules allow
	// everything; the rules document itself is stored over WebDAV.
	var allow strings.Builder
	allow.WriteString("<!DOCTYPE html><html><body>")
	for _, r := range []string{"/*", "/*/*", "/*/*/*"} {
		allow.WriteString(`<div itemscope itemtype="https://pagelove.org/AuthorizationRule"><meta itemprop="actor" content="` + fixtureWriter.Sub +
			`"><meta itemprop="resource" content="` + r + `"><meta itemprop="method" content="*"><meta itemprop="action" content="Allow"></div>`)
	}
	allow.WriteString("</body></html>")
	if _, err := e.Write(ctx, s, &engine.Op{Plane: engine.Authoring, Method: "PUT", Path: "/_fixture-rules.html", Body: []byte(allow.String())}); err != nil {
		t.Fatal(err)
	}
	return f
}

// fixtureWriter is the principal of fixture writes.
var fixtureWriter = &identity.Principal{Authenticated: true, Sub: "fixture-writer", Username: "fixture-writer"}

// write runs a public-plane write as fixtureWriter (schemas and shapes
// apply; the fixture's rules allow everything) and returns the result or
// the refusal.
func (f *fixture) write(method, path, rng, body string) (*engine.Result, *errdoc.Error) {
	f.t.Helper()
	op := &engine.Op{Plane: engine.Public, Method: method, Path: path, Range: engine.ParseRange(rng), Body: []byte(body), Principal: fixtureWriter}
	res, err := f.e.Write(f.ctx, f.s, op)
	if err == nil {
		return res, nil
	}
	var e *errdoc.Error
	if !errors.As(err, &e) {
		f.t.Fatalf("%s %s: %v", method, path, err)
	}
	return nil, e
}

func (f *fixture) ok(method, path, rng, body string) *engine.Result {
	f.t.Helper()
	res, e := f.write(method, path, rng, body)
	if e != nil {
		f.t.Fatalf("%s %s %s: %d %s\n%s", method, path, rng, e.Status, e.Message, e.Document)
	}
	return res
}

func (f *fixture) refused(status int, method, path, rng, body string, contains ...string) *errdoc.Error {
	f.t.Helper()
	_, e := f.write(method, path, rng, body)
	if e == nil {
		f.t.Fatalf("%s %s %s: accepted, want %d", method, path, rng, status)
	}
	if e.Status != status {
		f.t.Fatalf("%s %s %s: status %d, want %d\n%s", method, path, rng, e.Status, status, e.Document)
	}
	for _, c := range contains {
		if !strings.Contains(e.Document, c) {
			f.t.Errorf("%s %s: body lacks %q:\n%s", method, path, c, e.Document)
		}
	}
	return e
}

func (f *fixture) stored(path string) string {
	f.t.Helper()
	d, err := f.s.Store.Get(f.ctx, path)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ""
		}
		f.t.Fatal(err)
	}
	return string(d.Body)
}

func (f *fixture) snap() *site.Snapshot {
	f.t.Helper()
	snap, err := f.s.Index(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	return snap
}

func page(body string) string { return "<!DOCTYPE html>\n<html><body>\n" + body + "\n</body></html>\n" }

func schemaDoc(typ string, props ...string) string {
	return page(`<div hidden itemscope itemtype="https://pagelove.org/Schema"><meta itemprop="type" content="` + typ + `">` +
		strings.Join(props, "") + `</div>`)
}

func prop(name, typ, card string, extra ...string) string {
	s := `<div itemprop="property" itemscope itemtype="https://pagelove.org/Property"><meta itemprop="name" content="` + name + `">`
	if typ != "" {
		s += `<meta itemprop="type" content="` + typ + `">`
	}
	if card != "" {
		s += `<meta itemprop="cardinality" content="` + card + `">`
	}
	return s + strings.Join(extra, "") + `</div>`
}

const (
	tTag  = "https://t.test/Tag"
	tText = "https://schema.host/Text"
	tInt  = "https://schema.host/Integer"
)

func TestCardinalityTypeAndEnvelope(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc(tTag, prop("label", tText, "1..1"), prop("n", tInt, "0..1")))
	e := f.refused(422, "PUT", "/a.html", "", page(`<div itemscope itemtype="`+tTag+`"><meta itemprop="n" content="x"></div>`),
		"https://pagelove.org/SchemaViolation", `<span itemprop="check">cardinality</span>`, `<span itemprop="check">type</span>`,
		`[https://t.test/Tag].label: cardinality 1..1 violated: expected exactly 1 value, found 0`, `<title>422 Unprocessable Entity - Schema Violation</title>`)
	if e.Kind != schema.KindSchemaViolation {
		t.Errorf("kind %q", e.Kind)
	}
	if f.stored("/a.html") != "" {
		t.Fatal("refused document was stored")
	}
	f.ok("PUT", "/a.html", "", page(`<div itemscope itemtype="`+tTag+`"><meta itemprop="label" content="x"><meta itemprop="n" content="-3"></div>`))
	// A nested item's own properties do not count for the outer item.
	f.refused(422, "PUT", "/b.html", "", page(`<div itemscope itemtype="`+tTag+`"><div itemscope><meta itemprop="label" content="in"></div></div>`), "cardinality")
	// Items without a schema are stored unchecked.
	f.ok("PUT", "/c.html", "", page(`<div itemscope itemtype="https://t.test/Other"><meta itemprop="label"></div>`))
}

// Live 2026-09-29: a selector write that breaks a cardinality answers
// PageLove's Cardinality problems item (text/html, no charset); a whole
// PUT keeps the SchemaViolation document.
func TestSelectorWriteCardinalityProblem(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc(tTag, prop("label", tText, "1..1")))
	f.ok("PUT", "/t.html", "", page(`<div id="t1" itemscope itemtype="`+tTag+`"><meta itemprop="label" content="a"></div>`))
	e := f.refused(422, "DELETE", "/t.html", `selector=#t1 > meta[itemprop="label"]`, "",
		`<div itemscope itemtype="https://dombase.pagelove.team/ns/error/Cardinality"><ul itemprop="problems"><li itemprop="problem"><span itemprop="message">https://t.test/Tag /label: cardinality 1..1 violated: expected exactly 1 value, found 0 (cardinality)</span></li></ul></div>`)
	if e.MediaType() != "text/html" || e.Kind != schema.ProblemCardinality {
		t.Errorf("media type %q, kind %q", e.MediaType(), e.Kind)
	}
	f.refused(422, "POST", "/t.html", "selector=#t1", `<meta itemprop="label" content="b">`, "https://t.test/Tag /label: cardinality 1..1 violated: expected exactly 1 value, found 2 (cardinality)")
	f.refused(422, "PUT", "/t.html", `selector=#t1 > meta[itemprop="label"]`, `<meta itemprop="note" content="x">`, "https://dombase.pagelove.team/ns/error/Cardinality")
	f.refused(422, "PUT", "/t2.html", "", page(`<div itemscope itemtype="`+tTag+`"><meta itemprop="label" content="a"><meta itemprop="label" content="b"></div>`),
		"https://pagelove.org/SchemaViolation", "[https://t.test/Tag].label: cardinality 1..1 violated: expected exactly 1 value, found 2")
}

// Live 2026-09-29: only outermost items are validated. An item inside
// another item's markup is not, whether it is a property value, a plain
// nested item, or inside an item without a schema; a schema-typed
// property is not type-checked.
func TestOnlyOutermostItemsAreValidated(t *testing.T) {
	f := newFixture(t)
	const addr, person = "https://t.test/Address", "https://t.test/Person"
	f.ok("PUT", "/schema.html", "", schemaDoc(addr, prop("street", tText, "1..1"), prop("city", tText, "1..1"))+
		schemaDoc(person, prop("name", tText, "1..1"), prop("address", addr, "0..1")))
	a := func(attrs string) string {
		return `<div ` + attrs + ` itemscope itemtype="` + addr + `"><span itemprop="street">1 Main St</span></div>`
	}
	f.refused(422, "PUT", "/top.html", "", page(a("")), "[https://t.test/Address].city")
	f.ok("PUT", "/value.html", "", page(`<div itemscope itemtype="`+person+`"><span itemprop="name">A</span>`+a(`itemprop="address"`)+`</div>`))
	f.ok("PUT", "/plain.html", "", page(`<div itemscope itemtype="`+person+`"><span itemprop="name">A</span>`+a("")+`</div>`))
	f.ok("PUT", "/wrapped.html", "", page(`<div itemscope itemtype="https://t.test/Wrapper">`+a("")+`</div>`))
	f.ok("PUT", "/text.html", "", page(`<div itemscope itemtype="`+person+`"><span itemprop="name">A</span><span itemprop="address">1 Main St</span></div>`))
	f.ok("PUT", "/wrong.html", "", page(`<div itemscope itemtype="`+person+`"><span itemprop="name">A</span><div itemprop="address" itemscope itemtype="`+person+`"></div></div>`))
	e := f.refused(422, "PUT", "/both.html", "", page(`<div itemscope itemtype="`+person+`">`+a(`itemprop="address"`)+`</div>`), "[https://t.test/Person].name")
	if strings.Contains(e.Document, "city") {
		t.Errorf("the nested Address was validated:\n%s", e.Document)
	}
}

// Live 2026-09-29: a value written to a @computed property is stored and
// validated like any other value.
func TestComputedPropertyValueIsValidatedNotRefused(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc(tTag, prop("n", tInt, "", `<div itemprop="@computed" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source">1 + 1</script></div>`)))
	f.ok("PUT", "/a.html", "", page(`<div itemscope itemtype="`+tTag+`"><meta itemprop="n" content="42"></div>`))
	if !strings.Contains(f.stored("/a.html"), `content="42"`) {
		t.Errorf("computed value not stored:\n%s", f.stored("/a.html"))
	}
	f.refused(422, "PUT", "/b.html", "", page(`<div itemscope itemtype="`+tTag+`"><meta itemprop="n" content="Countess"></div>`),
		"https://pagelove.org/SchemaViolation", `[https://t.test/Tag].n: Value &quot;Countess&quot;`)
}

// Live 2026-09-29: a WebDAV PUT is stored without schema or shape checks.
// WebDAV PUTs are validated like public writes (R-MOD-13, R-PROTO-112).
// Live PageLove stores them unchecked; pagelike keeps the documented check
// (keep-documented-security, decisions-2026-09-29/serialization.md).
func TestWebDAVPutIsValidated(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc(tTag, prop("label", tText, "1..1"))+page(`<div hidden itemscope itemtype="https://pagelove.org/ShapeConstraint">
  <meta itemprop="resource" content="/*"><code itemprop="selector">[itemtype*=Widget]</code><code itemprop="constraint">:has([itemprop="name"])</code></div>`))
	for path, body := range map[string]string{
		"/t.html": page(`<div itemscope itemtype="` + tTag + `"></div>`),
		"/w.html": page(`<div itemscope itemtype="https://t.test/Widget"></div>`),
	} {
		f.refused(422, "PUT", path, "", body)
		if _, err := f.e.Write(f.ctx, f.s, &engine.Op{Plane: engine.Authoring, Method: "PUT", Path: path, Body: []byte(body)}); err == nil {
			t.Errorf("WebDAV PUT %s was not validated", path)
		}
	}
}

func TestSchemaRegistrationTiming(t *testing.T) {
	f := newFixture(t)
	// The write carrying a schema is not validated against it (R-MOD-4).
	f.ok("PUT", "/both.html", "", schemaDoc(tTag, prop("label", tText, "1..1"))+page(`<div itemscope itemtype="`+tTag+`"></div>`))
	f.refused(422, "PUT", "/next.html", "", page(`<div itemscope itemtype="`+tTag+`"></div>`), "cardinality")
	f.ok("DELETE", "/both.html", "", "")
	f.ok("PUT", "/next.html", "", page(`<div itemscope itemtype="`+tTag+`"></div>`))
}

func TestDefaultsAreAddedToStoredDocument(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc(tTag,
		prop("title", tText, "1..1"),
		prop("status", tText, "0..1", `<meta itemprop="default" content="draft">`),
		prop("code", tText, "1..1", `<div itemprop="default" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source">"c-" + String.random(4)</script></div>`)))
	body := "<!DOCTYPE html>\n<html><body>\n<p>&copy; 2026 &#39;x&#39;<br/></p>\n<div itemscope itemtype=\"" + tTag + "\">\n  <meta itemprop=\"title\" content=\"Hello\">\n</div>\n</body></html>\n"
	res := f.ok("PUT", "/p.html", "", body)
	got := f.stored("/p.html")
	if !strings.Contains(got, "<p>© 2026 'x'<br></p>\n<div") {
		t.Errorf("untouched content changed:\n%s", got)
	}
	if !strings.Contains(got, `<meta itemprop="status" content="draft">`) || !regexp.MustCompile(`<meta itemprop="code" content="c-[A-Za-z0-9]{4}">`).MatchString(got) {
		t.Errorf("defaults missing:\n%s", got)
	}
	if string(res.Body) != got {
		t.Errorf("response body is not the stored document")
	}
	// A present but empty value is not defaulted.
	f.ok("PUT", "/q.html", "", page(`<div itemscope itemtype="`+tTag+`"><meta itemprop="title" content="t"><meta itemprop="status" content=""><meta itemprop="code" content="k"></div>`))
	if strings.Contains(f.stored("/q.html"), "draft") {
		t.Error("empty value was defaulted")
	}
	// Selector POST: the appended instance gets its defaults.
	f.ok("PUT", "/list.html", "", page(`<p>&copy;<br/></p><ul id="l"></ul>`))
	f.ok("POST", "/list.html", "selector=#l", `<li itemscope itemtype="`+tTag+`"><meta itemprop="title" content="new"></li>`)
	if got := f.stored("/list.html"); !strings.Contains(got, `content="draft"`) || !strings.Contains(got, `itemprop="code"`) || !strings.Contains(got, `<p>©<br></p>`) {
		t.Errorf("selector POST lost defaults or untouched bytes:\n%s", got)
	}
}

func TestKeyAutoDefault(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc("https://t.test/Host",
		prop("hid", tText, "1..1", `<meta itemprop="unique" content="true"><meta itemprop="@key" content="true">`),
		prop("hostname", tText, "0..1")))
	f.ok("PUT", "/h.html", "", page(`<div itemscope itemtype="https://t.test/Host"><meta itemprop="hostname" content="x"></div>`))
	if !regexp.MustCompile(`<meta itemprop="hid" content="[a-z][a-z0-9]{7}">`).MatchString(f.stored("/h.html")) {
		t.Errorf("no key default:\n%s", f.stored("/h.html"))
	}
	root := f.snap().Docs["/h.html"].Root
	var inst = findItem(root)
	if id, ok := schema.KeyID(f.snap(), inst); !ok || len(id) != 8 {
		t.Errorf("KeyID = %q %v", id, ok)
	}
}

func TestWriteResolversChildFirst(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", page(
		`<div hidden itemscope itemtype="https://pagelove.org/Schema"><meta itemprop="type" content="https://t.test/A">`+
			prop("code", tText, "1..1", `<script type="text/sessel" itemprop="@write">self.map((el) => el.set_text(el.text() + "a"))</script>`,
				`<script type="text/sessel" itemprop="@read">self.map((el) => el.set_text(el.text() + "1"))</script>`)+`</div>`+
			`<div hidden itemscope itemtype="https://pagelove.org/Schema"><meta itemprop="type" content="https://t.test/B"><meta itemprop="parent" content="https://t.test/A">`+
			prop("code", tText, "1..1", `<script type="text/sessel" itemprop="@write">self.map((el) => el.set_text(el.text() + "b"))</script>`,
				`<script type="text/sessel" itemprop="@read">self.map((el) => el.set_text(el.text() + "2"))</script>`)+`</div>`))
	f.ok("PUT", "/e.html", "", page(`<p>&amp; kept</p><div itemscope itemtype="https://t.test/B"><span itemprop="code">x</span></div>`))
	got := f.stored("/e.html")
	if !strings.Contains(got, `<span itemprop="code">xba</span>`) || !strings.Contains(got, "<p>&amp; kept</p>") {
		t.Fatalf("stored:\n%s", got)
	}
	// @read: root → leaf, representation only.
	doc, _ := f.s.Store.Get(f.ctx, "/e.html")
	op := &engine.ReadOp{Plane: engine.Public, Method: "GET", Path: "/e.html", Principal: identity.Anonymous("x")}
	res, err := f.e.ReadMarkup(f.ctx, f.s, f.snap(), doc, op, schema.ComposeWithRead(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Body), ">xba12<") {
		t.Errorf("read:\n%s", res.Body)
	}
	if f.stored("/e.html") != got {
		t.Error("read changed storage")
	}
	// A document without resolvers is served as stored.
	f.ok("PUT", "/plain.html", "", "<p>&copy;</p>")
	pd, _ := f.s.Store.Get(f.ctx, "/plain.html")
	res, err = f.e.ReadMarkup(f.ctx, f.s, f.snap(), pd, &engine.ReadOp{Plane: engine.Public, Method: "GET", Path: "/plain.html", Principal: identity.Anonymous("x")}, schema.ComposeWithRead(nil))
	if err != nil || string(res.Body) != "<p>©</p>" {
		t.Errorf("plain read: %v %q", err, res.Body)
	}
}

func TestResolverFailures(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc(tTag,
		prop("a", tText, "0..1", `<script type="text/sessel" itemprop="@write">"not an element"</script>`),
		prop("b", tText, "0..n", `<script type="text/sessel" itemprop="@write">[]</script>`),
		prop("c", tText, "0..1", `<script type="text/sessel" itemprop="@write">self.map((el) =></script>`)))
	f.refused(500, "PUT", "/x.html", "", page(`<div itemscope itemtype="`+tTag+`"><meta itemprop="a" content="v"></div>`), "https://pagelove.org/BindingFailure", "return-type")
	f.ok("PUT", "/y.html", "", page(`<div itemscope itemtype="`+tTag+`"><meta itemprop="b" content="secret"><meta itemprop="c" content="Kept"></div>`))
	if got := f.stored("/y.html"); strings.Contains(got, "secret") || !strings.Contains(got, "Kept") {
		t.Errorf("stored:\n%s", got)
	}
}

func TestValidatorsOrderAndResponses(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc(tTag,
		prop("email", tText, "1..1", `<div itemprop="@validate" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source">self.all((el) => el.text().contains("@"))</script></div>`),
		prop("code", tText, "0..1", `<script type="text/sessel" itemprop="@validate">@schema HTTPResponse url("https://pagelove.org/HTTPResponse"); throw new HTTPResponse { status: 409, message: "taken" }</script>`),
		prop("p", tText, "0..1", `<meta itemprop="group" content="g">`),
		prop("q", tText, "0..1", `<meta itemprop="group" content="g">`),
		`<div itemprop="constraint" itemscope><meta itemprop="group" content="g"><meta itemprop="cardinality" content="1..1"></div>`,
		`<script type="text/sessel" itemprop="@validate">self.microdata()["email"] != "no@x"</script>`))
	f.refused(422, "PUT", "/a.html", "", page(`<div itemscope itemtype="`+tTag+`"><span itemprop="email">bad</span><meta itemprop="p" content="1"></div>`), "@validate")
	// Schema-level @validate runs before groups (C1).
	e := f.refused(422, "PUT", "/b.html", "", page(`<div itemscope itemtype="`+tTag+`"><span itemprop="email">no@x</span></div>`), "@validate")
	if strings.Contains(e.Document, "group") {
		t.Errorf("group reported before schema @validate:\n%s", e.Document)
	}
	f.refused(422, "PUT", "/c.html", "", page(`<div itemscope itemtype="`+tTag+`"><span itemprop="email">a@x</span></div>`), "group &#x27;g&#x27; constraint violated")
	e = f.refused(409, "PUT", "/d.html", "", page(`<div itemscope itemtype="`+tTag+`"><span itemprop="email">a@x</span><meta itemprop="p" content="1"><meta itemprop="code" content="z"></div>`))
	if !strings.Contains(e.Document, "taken") {
		t.Errorf("thrown response body: %q", e.Document)
	}
	f.ok("PUT", "/e.html", "", page(`<div itemscope itemtype="`+tTag+`"><span itemprop="email">a@x</span><meta itemprop="q" content="1"></div>`))
}

func TestCompileFailureAndLoadErrors(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc("https://t.test/V", prop("x", tText, "0..1"), `<script type="text/sessel" itemprop="@validate">(((</script>`)+
		schemaDoc("https://t.test/C1", `<meta itemprop="parent" content="https://t.test/C2">`)+
		schemaDoc("https://t.test/C2", `<meta itemprop="parent" content="https://t.test/C1">`)+
		schemaDoc("https://t.test/R", prop("org", tText, "1..1", `<meta itemprop="references" content="https://t.test/Nope">`)))
	f.refused(422, "PUT", "/v.html", "", page(`<div itemscope itemtype="https://t.test/V"></div>`), "@validate")
	f.refused(422, "PUT", "/c.html", "", page(`<div itemscope itemtype="https://t.test/C1"></div>`), `<span itemprop="check">schema</span>`, "cyclic")
	f.refused(422, "PUT", "/r.html", "", page(`<div itemscope itemtype="https://t.test/R"><meta itemprop="org" content="a"></div>`), `<span itemprop="check">schema</span>`)
}

func TestUniqueness(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc("https://t.test/M",
		prop("user", tText, "1..1", `<meta itemprop="unique" content="pair">`),
		prop("org", tText, "0..1", `<meta itemprop="unique" content="pair">`),
		prop("slug", tText, "0..1", `<meta itemprop="unique" content="true">`)))
	m := func(u, o, s string) string {
		x := `<div itemscope itemtype="https://t.test/M"><meta itemprop="user" content="` + u + `">`
		if o != "" {
			x += `<meta itemprop="org" content="` + o + `">`
		}
		if s != "" {
			x += `<meta itemprop="slug" content="` + s + `">`
		}
		return x + `</div>`
	}
	f.ok("PUT", "/1.html", "", page(m("alice", "acme", "s1")))
	f.ok("PUT", "/2.html", "", page(m("alice", "globex", "")))
	// PageLove's ConstraintViolation problems item (live 2026-09-29):
	// composite members in alphabetical order, no path of the other
	// document (withheld: keep-documented-security).
	e := f.refused(422, "PUT", "/3.html", "", page(m("alice", "acme", "")),
		`<div itemscope itemtype="https://dombase.pagelove.team/ns/error/ConstraintViolation"><ul itemprop="problems"><li itemprop="problem"><span itemprop="message">Constraint violation: 1 violation(s):`+
			"\n  - [uniqueness] [itemprop='org'], [itemprop='user'] (unique-group(pair)): Composite uniqueness violation: value combination already exists for properties 'org, user'</span></li></ul></div>")
	if e.MediaType() != "text/html" || e.Kind != schema.ProblemConstraint {
		t.Errorf("media type %q, kind %q", e.MediaType(), e.Kind)
	}
	f.ok("PUT", "/4.html", "", page(m("bob", "", "")+m("bob", "", "")))
	// Two items of the written document: the first claimer is named.
	f.refused(422, "PUT", "/5.html", "", page(m("c", "", "s2")+m("d", "", "s2")),
		"[uniqueness] [itemprop='slug'] (unique(slug)): Uniqueness violation: another item in '/5.html' already claims this value for property 'slug' (first claimed by item '0.2.0.1')")
	e = f.refused(422, "PUT", "/6.html", "", page(m("e", "", "s1")), "Uniqueness violation: value already exists for property 'slug'")
	if strings.Contains(e.Document, "/1.html") {
		t.Errorf("the other document's path is disclosed:\n%s", e.Document)
	}
	// Only the first violation is reported.
	if e = f.refused(422, "PUT", "/7.html", "", page(m("alice", "acme", "s1"))); !strings.Contains(e.Document, "1 violation(s)") || strings.Count(e.Document, "[uniqueness]") != 1 {
		t.Errorf("more than the first violation:\n%s", e.Document)
	}
	f.ok("PUT", "/1.html", "", page(m("alice", "acme", "s1")+"<p>edited</p>")) // own value
	f.ok("DELETE", "/1.html", "", "")
	f.ok("PUT", "/6.html", "", page(m("e", "", "s1")))
}

func TestReferencesAndCascades(t *testing.T) {
	f := newFixture(t)
	org := schemaDoc("https://t.test/Org", prop("id", tText, "0..1", `<meta itemprop="unique" content="true">`))
	sub := schemaDoc("https://t.test/Partner", `<meta itemprop="parent" content="https://t.test/Org">`)
	member := schemaDoc("https://t.test/Member", prop("org", tText, "1..1", `<meta itemprop="references" content="https://t.test/Org#id">`, `<meta itemprop="cascade" content="true">`))
	tagged := schemaDoc("https://t.test/Tagged", prop("orgs", tText, "0..n", `<meta itemprop="references" content="https://t.test/Org#id">`, `<meta itemprop="cascade" content="true">`))
	f.ok("PUT", "/schema.html", "", org+sub+member+tagged)
	f.refused(422, "PUT", "/m.html", "", page(`<div itemscope itemtype="https://t.test/Member"><meta itemprop="org" content="acme"></div>`),
		"https://dombase.pagelove.team/ns/error/ConstraintViolation", "[reference] [itemprop='org'] (references(org)): Referenced value does not exist for property 'org'")
	f.ok("PUT", "/o.html", "", page(`<div id="o" itemscope itemtype="https://t.test/Partner"><meta itemprop="id" content="acme"></div>`))
	f.ok("PUT", "/other.html", "", page(`<div itemscope itemtype="https://t.test/Org"><meta itemprop="id" content="other"></div>`))
	f.ok("PUT", "/m.html", "", page(`<div itemscope itemtype="https://t.test/Member"><meta itemprop="org" content="acme"></div>`))
	f.ok("PUT", "/t.html", "", page(`<p>&copy;</p><div itemscope itemtype="https://t.test/Tagged"><meta itemprop="orgs" content="acme"><meta itemprop="orgs" content="other"></div>`))
	// A change rewrites referrers.
	res := f.ok("PUT", "/o.html", `selector=#o > meta[itemprop="id"]`, `<meta itemprop="id" content="acme2">`)
	_ = res
	if !strings.Contains(f.stored("/m.html"), `content="acme2"`) || !strings.Contains(f.stored("/t.html"), `content="acme2"`) {
		t.Fatalf("rewrite:\n%s\n%s", f.stored("/m.html"), f.stored("/t.html"))
	}
	if !strings.Contains(f.stored("/t.html"), "<p>©</p>") {
		t.Errorf("cascade changed untouched content:\n%s", f.stored("/t.html"))
	}
	// Disappearance: 1..1 referrer document deleted, 0..n value removed;
	// both emit events in the same transaction. The delete is made on the
	// public plane: PageLove announces application-plane writes only
	// (authoring-plane writes and their cascades emit no events, live
	// 2026-09-28), so the rule below grants it.
	f.ok("PUT", "/rules.html", "", `<!DOCTYPE html><html><body><div itemscope itemtype="https://pagelove.org/AuthorizationRule"><meta itemprop="actor" content="*"><meta itemprop="resource" content="/o.html"><meta itemprop="method" content="DELETE"><meta itemprop="action" content="Allow"></div></body></html>`)
	res, err := f.e.Write(f.ctx, f.s, &engine.Op{Plane: engine.Public, Method: "DELETE", Path: "/o.html", Principal: identity.Anonymous("cascade-test")})
	if err != nil {
		t.Fatalf("public DELETE: %v", err)
	}
	paths := map[string]bool{}
	for _, ev := range res.Events {
		paths[ev.Path] = true
	}
	if !paths["/o.html"] || !paths["/m.html"] || !paths["/t.html"] {
		t.Errorf("events: %v", paths)
	}
	if f.stored("/m.html") != "" {
		t.Error("1..1 referrer not deleted")
	}
	if got := f.stored("/t.html"); strings.Contains(got, "acme2") || !strings.Contains(got, `content="other"`) {
		t.Errorf("0..n referrer:\n%s", got)
	}
}

func TestRestrict(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc("https://t.test/Org", prop("id", tText, "1..1", `<meta itemprop="unique" content="true">`))+
		schemaDoc("https://t.test/Badge", prop("org", tText, "1..1", `<meta itemprop="references" content="https://t.test/Org#id">`, `<meta itemprop="cascade" content="restrict">`)))
	f.ok("PUT", "/o.html", "", page(`<div itemscope itemtype="https://t.test/Org"><meta itemprop="id" content="acme"></div>`))
	f.ok("PUT", "/b.html", "", page(`<div itemscope itemtype="https://t.test/Badge"><meta itemprop="org" content="acme"></div>`))
	e := f.refused(409, "DELETE", "/o.html", "", "", "https://dombase.pagelove.team/ns/error/CascadeBlocked",
		"Operation refused by cascade constraint: Constraint violation: 1 violation(s):\n  - [cascade-restrict] [itemprop='id'] (restrict(https://t.test/Org#id)): Cannot delete: 1 document(s) reference this value via 'org'")
	if e.Kind != schema.ProblemCascade {
		t.Errorf("kind %q", e.Kind)
	}
	if f.stored("/o.html") == "" {
		t.Error("restricted delete happened")
	}
}

func TestShapes(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/shapes.html", "", page(`
<div hidden itemscope itemtype="https://pagelove.org/ShapeConstraint">
  <meta itemprop="resource" content="/nav/*"><code itemprop="selector">nav</code>
  <code itemprop="constraint">:has(ul)</code><code itemprop="constraint">:has(ul li a)</code>
</div>
<div hidden itemscope itemtype="https://pagelove.org/ShapeConstraint">
  <meta itemprop="resource" content="/notes/*"><code itemprop="selector">article[itemtype*=Note]</code>
  <code itemprop="permit">[id]</code><code itemprop="permit">[itemprop="title"]</code>
</div>
<div hidden itemscope itemtype="https://pagelove.org/ShapeConstraint">
  <meta itemprop="resource" content="/notes/*"><code itemprop="selector">article[itemtype*=Note] &gt; [itemprop='title']</code>
  <code itemprop="permit">em</code>
</div>`))
	// PageLove's ShapeConstraint problems item (live 2026-09-29).
	f.refused(422, "PUT", "/nav/a.html", "", page(`<nav><p>x</p></nav>`),
		`<div itemscope itemtype="https://dombase.pagelove.team/ns/error/ShapeConstraint"><ul itemprop="problems"><li itemprop="problem"><span itemprop="message">Shape constraints violated</span></li>`+
			`<li itemprop="problem"><span itemprop="message">https://pagelove.org/ShapeConstraint nav: Element matching 'nav' does not satisfy constraint ':has(ul)'</span></li>`+
			`<li itemprop="problem"><span itemprop="message">https://pagelove.org/ShapeConstraint nav: Element matching 'nav' does not satisfy constraint ':has(ul li a)'</span></li></ul></div>`)
	f.ok("PUT", "/nav/b.html", "", page(`<nav><ul><li id="l1"><a href="/">h</a></li><li id="l2"><a href="/x">x</a></li></ul></nav>`))
	f.ok("DELETE", "/nav/b.html", "selector=#l2", "")
	e := f.refused(409, "DELETE", "/nav/b.html", "selector=#l1", "", "https://dombase.pagelove.team/ns/error/CascadeBlocked",
		"Operation refused by cascade constraint: Constraint violation: 1 violation(s):\n  - [https://pagelove.org/ShapeConstraint] nav (:has(ul li a)): Element matching 'nav' does not satisfy constraint ':has(ul li a)'")
	if e.Kind != schema.ProblemCascade {
		t.Errorf("kind %q", e.Kind)
	}
	f.ok("PUT", "/other/b.html", "", page(`<nav><p>not governed here</p></nav>`))
	// Closed shapes with composed ownership.
	f.ok("PUT", "/notes/a.html", "", page(`<article itemtype="https://s.test/Note" id="n"><h2 itemprop="title"><em>Hi</em></h2></article>`))
	f.refused(422, "PUT", "/notes/b.html", "", page(`<article itemtype="https://s.test/Note" id="n"><h2 itemprop="title"><span>Hi</span></h2></article>`),
		"Element &lt;span&gt; inside 'article[itemtype*=Note] &gt; [itemprop='title']' is not permitted by any permit")
	f.refused(422, "PUT", "/notes/c.html", "", page(`<article itemtype="https://s.test/Note" id="n" class="x"><h2 itemprop="title">Hi</h2></article>`),
		"Attribute &lt;class&gt; on &lt;article&gt; matched by 'article[itemtype*=Note]' is not permitted by the selector or any matching permit")
	f.refused(422, "PUT", "/notes/d.html", "", page(`<article itemtype="https://s.test/Note"><h2 itemprop="title" onclick="x()">Hi</h2></article>`), "onclick")
}

func TestMoveValidatesSourceAndDestination(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc("https://t.test/N", prop("v", tText, "1..1")))
	f.ok("PUT", "/m.html", "", page(`<div id="a" itemscope itemtype="https://t.test/N"><meta itemprop="v" content="a"></div><div id="b" itemscope itemtype="https://t.test/N"><meta itemprop="v" content="b"></div><div id="c"></div>`))
	op := &engine.Op{Plane: engine.Authoring, Method: "MOVE", Path: "/m.html", Range: engine.ParseRange(`selector=#a > meta`),
		Destination: "/m.html", DestinationRange: engine.ParseRange("selector=#b; placement=append")}
	_, err := f.e.Write(f.ctx, f.s, op)
	var e *errdoc.Error
	if !errors.As(err, &e) || e.Status != 422 || !strings.Contains(e.Document, "cardinality") {
		t.Fatalf("move: %v", err)
	}
	// Moving the whole first item before the empty div keeps both valid.
	op = &engine.Op{Plane: engine.Authoring, Method: "MOVE", Path: "/m.html", Range: engine.ParseRange(`selector=#a`),
		Destination: "/m.html", DestinationRange: engine.ParseRange("selector=#c; placement=append")}
	if _, err := f.e.Write(f.ctx, f.s, op); err != nil {
		t.Fatal(err)
	}
}

func TestSesselClasses(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc("https://t.test/Person",
		prop("first", tText, "1..1"), prop("last", tText, "1..1"),
		prop("full", "", "", `<div itemprop="@computed" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source">self.first + " " + self.last</script></div>`),
		prop("tags", tText, "0..n", `<script type="text/sessel" itemprop="@read">self.map((el) => el.set_attr("content", el.value().upper()))</script>`),
		prop("hid", tText, "1..1", `<meta itemprop="unique" content="true"><meta itemprop="@key" content="true">`),
		`<div itemprop="property" itemscope itemtype="https://pagelove.org/Method"><meta itemprop="name" content="greet">
		   <div itemprop="parameter" itemscope itemtype="https://pagelove.org/Parameter"><meta itemprop="name" content="greeting"></div>
		   <div itemprop="implementation" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source">greeting + ", " + self.first</script></div></div>`))
	f.ok("PUT", "/a.html", "", page(`<div itemscope itemtype="https://t.test/Person"><meta itemprop="first" content="Ada"><meta itemprop="last" content="Lovelace"><meta itemprop="tags" content="x"></div>`))
	snap := f.snap()
	host := query.NewHost(f.s, snap)
	eval := func(src string) sessel.Value {
		t.Helper()
		v, err := sessel.Eval(f.ctx, `@schema Person url("https://t.test/Person"); `+src, &sessel.Env{Host: host})
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		return v
	}
	if v := eval(`Person.search().first().full`); v != "Ada Lovelace" {
		t.Errorf("computed: %#v", v)
	}
	if v := eval(`Person.search().first().tags`); !sessel.Equal(v, sessel.List{"X"}) {
		t.Errorf("@read list: %#v", v)
	}
	if v := eval(`Person.search().first().greet("Hi")`); v != "Hi, Ada" {
		t.Errorf("method: %#v", v)
	}
	if v := eval(`(new Person { first: "G", last: "H" }).hid`); !regexp.MustCompile(`^[a-z][a-z0-9]{7}$`).MatchString(sessel.TextOf(v)) {
		t.Errorf("construct key: %#v", v)
	}
	if _, err := sessel.Eval(f.ctx, `@schema Person url("https://t.test/Person"); new Person { first: "G" }`, &sessel.Env{Host: host}); err == nil {
		t.Error("construction without a required property succeeded")
	}
	if _, err := sessel.Eval(f.ctx, `@schema Person url("https://t.test/Person"); new Person { first: "G", last: "H", full: "x" }`, &sessel.Env{Host: host}); err == nil {
		t.Error("construction with a computed value succeeded")
	}
}

func TestGroupSubtypeIncludes(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/system/schemas/team.html", "", schemaDoc("https://t.test/Team", `<meta itemprop="parent" content="https://pagelove.org/Group">`,
		prop("domain", tText, "1..1"),
		`<div itemprop="property" itemscope itemtype="https://pagelove.org/Method"><meta itemprop="name" content="includes">
		   <div itemprop="parameter" itemscope itemtype="https://pagelove.org/Parameter"><meta itemprop="name" content="email"></div>
		   <div itemprop="implementation" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source">email.endsWith(self.domain)</script></div></div>`))
	f.ok("PUT", "/groups.html", "", page(`<div itemscope itemtype="https://t.test/Team"><span itemprop="name">staff</span><meta itemprop="domain" content="@example.com"></div>`))
	f.ok("PUT", "/rules.html", "", page(`<div itemscope itemtype="https://pagelove.org/AuthorizationRule"><meta itemprop="actor" content="staff"><meta itemprop="resource" content="/doc.html"><meta itemprop="method" content="PUT"><meta itemprop="action" content="Allow"></div>`))
	pol := f.snap().Policy
	allowed := func(email string, verified bool) bool {
		p := &identity.Principal{Authenticated: true, Sub: email, Username: email, Email: email, EmailVerified: verified}
		return pol.Decide(authz.Request{Principal: p, Method: "PUT", Path: "/doc.html"}, nil).Allowed
	}
	if !allowed("a@example.com", true) || allowed("a@example.com", false) || allowed("z@other.org", true) {
		t.Error("includes() membership")
	}
	if pol.SelectorOptions == nil || !pol.SelectorOptions.IsA("https://t.test/Team", "https://pagelove.org/Group") {
		t.Error(":isa() map not installed")
	}
}

func TestRuleSubtypesAndActorRead(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc("https://pagelove.org/AuthorizationRule",
		prop("actor", "", "", `<script type="text/sessel" itemprop="@read">self.map((el) => el.value() == "group:ed" ? [new meta[itemprop="actor"][content="alice"] {}, new meta[itemprop="actor"][content="bob"] {}] : [el]).flatten()</script>`))+
		schemaDoc("https://t.test/PathRule", `<meta itemprop="parent" content="https://pagelove.org/AuthorizationRule">`))
	rule := func(typ, actor, res string) string {
		return `<div itemscope itemtype="` + typ + `"><meta itemprop="actor" content="` + actor + `"><meta itemprop="resource" content="` + res + `"><meta itemprop="method" content="PUT"><meta itemprop="action" content="Allow"></div>`
	}
	f.ok("PUT", "/rules.html", "", page(rule("https://pagelove.org/AuthorizationRule", "group:ed", "/a.html")+rule("https://t.test/PathRule", "carol", "/b.html")))
	pol := f.snap().Policy
	allowed := func(user, path string) bool {
		p := &identity.Principal{Authenticated: true, Sub: user, Username: user}
		return pol.Decide(authz.Request{Principal: p, Method: "PUT", Path: path}, nil).Allowed
	}
	if !allowed("alice", "/a.html") || !allowed("bob", "/a.html") || allowed("dave", "/a.html") {
		t.Error("@read on actor did not expand the placeholder")
	}
	if !allowed("carol", "/b.html") || allowed("alice", "/b.html") {
		t.Error("rule subtype not honoured")
	}
}

func TestDispatch(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc("https://t.test/Demo",
		`<div itemprop="property" itemscope itemtype="https://pagelove.org/Method"><meta itemprop="name" content="foo">
		   <div itemprop="implementation" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source">"zero"</script></div></div>`,
		`<div itemprop="property" itemscope itemtype="https://pagelove.org/Method"><meta itemprop="name" content="foo">
		   <div itemprop="parameter" itemscope itemtype="https://pagelove.org/Parameter"><meta itemprop="name" content="a"></div>
		   <div itemprop="implementation" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source">"one:" + a</script></div></div>`,
		`<div itemprop="property" itemscope itemtype="https://pagelove.org/Method"><meta itemprop="name" content="doesNotUnderstand">
		   <div itemprop="implementation" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source">messageName + "/" + parameters.count().String()</script></div></div>`,
		`<div itemprop="property" itemscope itemtype="https://pagelove.org/Method"><meta itemprop="name" content="pair"><meta itemprop="returns" content="https://pagelove.org/Element">
		   <div itemprop="parameter" itemscope itemtype="https://pagelove.org/Parameter"><meta itemprop="name" content="a"></div>
		   <div itemprop="parameter" itemscope itemtype="https://pagelove.org/Parameter"><meta itemprop="name" content="b"></div>
		   <div itemprop="implementation" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source">"pair:" + a + ":" + (b == null).String()</script></div></div>`))
	snap := f.snap()
	host := snap.Docs["/schema.html"].Root
	run := func(name string, attrs map[string]string) sessel.Value {
		t.Helper()
		el := findItem(host)
		n := &struct{}{}
		_ = n
		call := &schema.MethodCall{Site: f.s, Snap: snap, Type: "https://t.test/Demo", Name: name, Host: withAttrs(el, attrs), Path: "/schema.html"}
		v, err := schema.Dispatch(f.ctx, call)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return v
	}
	if v := run("foo", nil); v != "zero" {
		t.Errorf("no args: %#v", v)
	}
	if v := run("foo", map[string]string{"a": "1"}); v != "one:1" {
		t.Errorf("one arg: %#v", v)
	}
	if v := run("bar", map[string]string{"x": "1", "y": "2"}); !strings.HasPrefix(sessel.TextOf(v), "bar/") {
		t.Errorf("dnu: %#v", v)
	}
	// No overload fully supplied: the declared one runs, absent parameters
	// null (R-COMP-61; docs/compat/decisions.md D-3).
	if v := run("pair", map[string]string{"a": "1"}); v != "pair:1:true" {
		t.Errorf("partial arguments: %#v", v)
	}
	res, err := schema.DispatchMethod(f.ctx, &schema.MethodCall{Site: f.s, Snap: snap, Type: "https://t.test/Demo", Name: "pair",
		Host: withAttrs(findItem(host), map[string]string{"a": "1", "b": "2"}), Path: "/schema.html"})
	if err != nil || res.Value != "pair:1:false" || res.Method == nil || res.Method.Returns != "https://pagelove.org/Element" {
		t.Errorf("DispatchMethod: %+v %v", res, err)
	}
}

func TestJavaScriptSeam(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/schema.html", "", schemaDoc(tTag,
		prop("email", tText, "1..1",
			`<div itemprop="@validate" itemscope itemtype="https://pagelove.org/JavaScript/Module"><script itemprop="source">export default (v) => v.includes("@")</script></div>`,
			`<div itemprop="@write" itemscope itemtype="https://pagelove.org/JavaScript/Module"><script itemprop="source">export default (v) => v.toLowerCase()</script></div>`)))
	// No runtime installed: fail clearly.
	f.refused(501, "PUT", "/a.html", "", page(`<div itemscope itemtype="`+tTag+`"><meta itemprop="email" content="A@B"></div>`), "unavailable", "https://pagelove.org/JavaScript/Module")
	schema.SetJSRunner(fakeJS{})
	t.Cleanup(func() { schema.SetJSRunner(nil) })
	f.ok("PUT", "/a.html", "", page(`<div itemscope itemtype="`+tTag+`"><meta itemprop="email" content="A@B"></div>`))
	if !strings.Contains(f.stored("/a.html"), `content="a@b"`) {
		t.Errorf("js @write:\n%s", f.stored("/a.html"))
	}
	f.refused(422, "PUT", "/b.html", "", page(`<div itemscope itemtype="`+tTag+`"><meta itemprop="email" content="nope"></div>`), "@validate")
	e := f.refused(418, "PUT", "/c.html", "", page(`<div itemscope itemtype="`+tTag+`"><meta itemprop="email" content="teapot"></div>`))
	if e.Document != "short and stout" {
		t.Errorf("thrown response body %q", e.Document)
	}
}

// fakeJS stands in for internal/jsrt: it understands the two modules of
// TestJavaScriptSeam.
type fakeJS struct{}

func (fakeJS) Run(ctx context.Context, c *schema.JSCall) (*schema.JSResult, error) {
	v, _ := c.Args[0].(string)
	switch c.Slot {
	case schema.JSSlotWrite:
		if v == "TEAPOT" || v == "teapot" {
			return &schema.JSResult{Value: "teapot"}, nil
		}
		return &schema.JSResult{Value: strings.ToLower(v)}, nil
	case schema.JSSlotValidate:
		if v == "teapot" {
			return nil, &schema.JSThrownResponse{Status: 418, Message: "short and stout"}
		}
		return &schema.JSResult{Value: strings.Contains(v, "@")}, nil
	}
	return nil, &schema.BindingFailure{Variant: "threw", Message: "unexpected slot " + c.Slot}
}

func TestPrimitiveGrammars(t *testing.T) {
	for _, c := range []struct {
		typ  string
		ok   []string
		fail []string
	}{
		{tInt, []string{"0", "+5", "007", "-42", "9223372036854775807"}, []string{"", " 5", "3.14", "1e3", "9223372036854775808"}},
		{"https://schema.host/Number", []string{"42", ".5", "5.", "+3", "1e3", "inf", "NaN", "-Infinity"}, []string{"", "0x10", "1_000", " 1", "abc"}},
		{"https://schema.host/Boolean", []string{"true", "false"}, []string{"True", "1", ""}},
		{"https://schema.host/URL", []string{"https://example.com", "mailto:a@b.com"}, []string{"", "not-a-url", "/x.webp", "//example.com", "https://exa mple.com"}},
		{"https://schema.host/DateTime", []string{"2024-01-15T10:30:00Z", "2024-01-15T10:30:00.123+01:00"}, []string{"2024-01-15", "2024-01-15T10:30:00", "2024-01-15 10:30:00Z", "2024-01-15t10:30:00z"}},
		{"https://schema.host/Date", []string{"2024-01-15", "2024-02-29"}, []string{"2023-02-29", "2024-13-01", "2024-1-5", "2024-01-15T10:30:00Z"}},
		{"https://schema.host/Cardinal", []string{"0..1", "1..1", "0..n", "1..n"}, []string{"2..3", "1..*", ""}},
	} {
		for _, v := range c.ok {
			if !schema.CheckPrimitive(c.typ, v) {
				t.Errorf("%s: %q rejected", c.typ, v)
			}
		}
		for _, v := range c.fail {
			if schema.CheckPrimitive(c.typ, v) {
				t.Errorf("%s: %q accepted", c.typ, v)
			}
		}
	}
}

func TestWholeDocumentMoveChecksDestinationShapes(t *testing.T) {
	f := newFixture(t)
	f.ok("PUT", "/shapes.html", "", page(`<div hidden itemscope itemtype="https://pagelove.org/ShapeConstraint"><meta itemprop="resource" content="/strict/*"><code itemprop="constraint">:has(main)</code></div>`))
	f.ok("PUT", "/loose/a.html", "", page(`<p>no main</p>`))
	op := &engine.Op{Plane: engine.Authoring, Method: "MOVE", Path: "/loose/a.html", Destination: "/strict/a.html"}
	_, err := f.e.Write(f.ctx, f.s, op)
	var e *errdoc.Error
	if !errors.As(err, &e) || e.Status != http.StatusUnprocessableEntity {
		t.Fatalf("move into a shaped resource: %v", err)
	}
}

// findItem returns the first itemscope element under root.
func findItem(root *html.Node) *html.Node {
	var found *html.Node
	dom.Walk(root, func(n *html.Node) bool {
		if found == nil && n.Type == html.ElementNode && dom.HasAttr(n, "itemscope") {
			found = n
		}
		return found == nil
	})
	return found
}

// withAttrs builds a method element <t:m> with the given attributes in a
// document of its own.
func withAttrs(_ *html.Node, attrs map[string]string) *html.Node {
	doc := &html.Node{Type: html.DocumentNode}
	el := &html.Node{Type: html.ElementNode, Data: "t:m"}
	for k, v := range attrs {
		el.Attr = append(el.Attr, html.Attribute{Key: k, Val: v})
	}
	doc.AppendChild(el)
	return el
}
