package jsglue_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sky-valley/pagelike/internal/control"
	"github.com/sky-valley/pagelike/internal/engine"
	_ "github.com/sky-valley/pagelike/internal/features"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

// plane is an in-process pagelike with every feature linked, serving site
// "t" at t.localhost (public) and dav-t.localhost (authoring).
type plane struct {
	t   *testing.T
	srv *httptest.Server
	s   *server.Server
	key string
}

func newPlane(t *testing.T) *plane {
	t.Helper()
	dir := t.TempDir()
	reg, err := site.NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctl, err := control.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Create(context.Background(), "t", site.Settings{DefaultGet: "allow"}); err != nil {
		t.Fatal(err)
	}
	key, _, err := ctl.CreateKey(context.Background(), "test", []string{"t"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	s := server.New(server.Config{Domain: "localhost"}, reg, ctl, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(s)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
		reg.Close()
		ctl.Close()
	})
	p := &plane{t: t, srv: srv, s: s, key: key}
	p.author("/_rules.html", `<!DOCTYPE html><html><body><div itemscope itemtype="https://pagelove.org/AuthorizationRule"><meta itemprop="actor" content="*"><meta itemprop="resource" content="/*"><meta itemprop="method" content="GET"><meta itemprop="method" content="PUT"><meta itemprop="action" content="allow"></div></body></html>`)
	return p
}

type reply struct {
	status int
	header http.Header
	body   string
}

func (p *plane) do(method, path, body string, h map[string]string, dav bool) reply {
	p.t.Helper()
	req, _ := http.NewRequest(method, p.srv.URL+path, strings.NewReader(body))
	req.Host = "t.localhost"
	if dav {
		req.Host = "dav-t.localhost"
		req.Header.Set("Authorization", "Bearer "+p.key)
	}
	if body == "" {
		req.Body, req.ContentLength = nil, 0
	}
	for k, v := range h {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return reply{status: resp.StatusCode, header: resp.Header, body: string(b)}
}

func (p *plane) author(path, body string) {
	p.t.Helper()
	if r := p.do("PUT", path, body, map[string]string{"Content-Type": "text/html"}, true); r.status >= 300 {
		p.t.Fatalf("setup PUT %s: %d %s", path, r.status, r.body)
	}
}

func (p *plane) put(path, body string, h map[string]string) reply {
	p.t.Helper()
	if h == nil {
		h = map[string]string{}
	}
	h["Content-Type"] = "text/html"
	return p.do("PUT", path, body, h, false)
}

func (r reply) must(t *testing.T, status int, contains ...string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("status %d, want %d; body:\n%s", r.status, status, r.body)
	}
	for _, c := range contains {
		if !strings.Contains(r.body, c) {
			t.Errorf("body does not contain %q:\n%s", c, r.body)
		}
	}
}

const js = "https://pagelove.org/JavaScript/Module"

func module(slot, src string) string {
	return `<div itemprop="` + slot + `" itemscope itemtype="` + js + `"><script itemprop="source" type="module">` + src + `</script></div>`
}

// Schema slots run in the shared runtime: a default that imports a schema
// and calls its Sessel method through the imported class (the host's
// CallMethod), @write, and a thrown HTTPResponse from @validate.
func TestSchemaSlots(t *testing.T) {
	p := newPlane(t)
	p.put("/schema.html", `<!DOCTYPE html><html><body>
<div hidden itemscope itemtype="https://pagelove.org/Schema"><meta itemprop="type" content="urn:t:Greeter">
<div itemprop="property" itemscope itemtype="https://pagelove.org/Method"><meta itemprop="name" content="greet">
<div itemprop="parameter" itemscope itemtype="https://pagelove.org/Parameter"><meta itemprop="name" content="who"></div>
<div itemprop="implementation" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source" type="text/sessel">"hi " + who</script></div></div>
</div>
<div hidden itemscope itemtype="https://pagelove.org/Schema"><meta itemprop="type" content="urn:t:Note">
<div itemprop="property" itemscope itemtype="https://pagelove.org/Property"><meta itemprop="name" content="greeting">
`+module("default", `import G from "urn:t:Greeter" with { type: "https://pagelove.org/Schema" }; export default () => new G({}).greet("Ada");`)+`</div>
<div itemprop="property" itemscope itemtype="https://pagelove.org/Property"><meta itemprop="name" content="email"><meta itemprop="cardinality" content="1..1">
`+module("@write", `export default (v) => v.toLowerCase();`)+`
`+module("@validate", `export default (v) => { if (v.startsWith("teapot")) throw { schema_url: "https://pagelove.org/HTTPResponse", status: 418, message: "short and stout", headers: { "X-Why": "tea" } }; return v.includes("@"); };`)+`</div>
</div></body></html>`, nil).must(t, 201)

	p.put("/a.html", `<!DOCTYPE html><html><body><div itemscope itemtype="urn:t:Note"><meta itemprop="email" content="A@B.C"></div></body></html>`, nil).must(t, 201)
	stored := p.do("GET", "/a.html", "", nil, true).body
	for _, want := range []string{`content="a@b.c"`, `itemprop="greeting" content="hi Ada"`} {
		if !strings.Contains(stored, want) {
			t.Errorf("stored document lacks %s:\n%s", want, stored)
		}
	}
	r := p.put("/b.html", `<!DOCTYPE html><html><body><div itemscope itemtype="urn:t:Note"><meta itemprop="email" content="teapot@x"></div></body></html>`, nil)
	r.must(t, 418, "short and stout")
	if r.header.Get("X-Why") != "tea" {
		t.Errorf("thrown headers: %v", r.header)
	}
	p.put("/c.html", `<!DOCTYPE html><html><body><div itemscope itemtype="urn:t:Note"><meta itemprop="email" content="nope"></div></body></html>`, nil).must(t, 422, "@validate")
}

// Composition: j: bindings (request members, taint), JavaScript method
// elements dispatched through the schema registry (this is the host
// element, Context writes reach the subtree).
func TestComposition(t *testing.T) {
	p := newPlane(t)
	p.author("/page.html", `<!DOCTYPE html><html xmlns:j="https://pagelove.org/Binding/JavaScript" xmlns:p="https://pagelove.org/1.0" xmlns:t="urn:t:Card"><body>
<main j:q="request.query.k + '!'"><p id="q"><p:stamp q></p:stamp></p><t:card label="L"><p:stamp mark></p:stamp></t:card></main>
<div hidden itemscope itemtype="https://pagelove.org/Schema"><meta itemprop="type" content="urn:t:Card">
<div itemprop="property" itemscope itemtype="https://pagelove.org/Method"><meta itemprop="name" content="card">
<div itemprop="parameter" itemscope itemtype="https://pagelove.org/Parameter"><meta itemprop="name" content="label"></div>
`+module("implementation", `export default function (label) { Context.mark = "M"; const p = new DOMParser().parseFromString("<b>" + label + ":" + this.tagName.toLowerCase() + ":" + (this === document.documentElement) + "</b>", "text/html"); return p.querySelector("b"); }`)+`</div>
</div></body></html>`)
	r := p.do("GET", "/page.html?k=v", "", map[string]string{"Range": "selector=main"}, false)
	r.must(t, 206, `<p id="q">v!</p>`, `<b>L:t:card:true</b>`)
	if cc := r.header.Get("Cache-Control"); strings.Contains(cc, "private") {
		t.Errorf("reading request.query must not make the page private: %q", cc)
	}
	p.author("/who.html", `<!DOCTYPE html><html xmlns:j="https://pagelove.org/Binding/JavaScript" xmlns:p="https://pagelove.org/1.0"><body j:h="typeof request.headers"><p:stamp h></p:stamp></body></html>`)
	r = p.do("GET", "/who.html", "", nil, false)
	r.must(t, 200, "object")
	if cc := r.header.Get("Cache-Control"); !strings.Contains(cc, "private") {
		t.Errorf("reading request.headers makes the page private: %q", cc)
	}
}

// Reactions: header objects look names up case-insensitively, a thrown
// HTTPResponse ends an action with its response, and one thrown in a gate
// is an ordinary failure (R-JS-53).
func TestReactions(t *testing.T) {
	p := newPlane(t)
	p.put("/r/triggers.html", `<!DOCTYPE html><html><body>
<div hidden itemscope itemtype="https://pagelove.org/Trigger"><meta itemprop="resource" content="/d/*"><meta itemprop="method" content="PUT">
`+module("action", `export default (ctx) => { throw { schema_url: "https://pagelove.org/HTTPResponse", status: 409, body: "probe=" + ctx.request.headers["X-Probe"] + ",auth=" + ("auth" in ctx.request) }; };`)+`</div>
<div hidden itemscope itemtype="https://pagelove.org/Trigger"><meta itemprop="resource" content="/g/*"><meta itemprop="method" content="PUT">
`+module("when", `export default () => { throw { schema_url: "https://pagelove.org/HTTPResponse", status: 409 }; };`)+`
`+module("action", `export default () => {};`)+`</div>
</body></html>`, nil).must(t, 201)
	// An anonymous request has auth {claims: {}, roles: []} (reacting
	// R-REACT-25, live 2026-09-29).
	p.put("/d/x.html", "<p>x</p>", map[string]string{"X-Probe": "yes"}).must(t, 409, "probe=yes,auth=true")
	p.put("/g/x.html", "<p>x</p>", nil).must(t, 500, "BindingFailure")
}

// The engine's Validate hooks run in phase order: every schema check before
// TransitionConstraints (R-REACT-71), whatever order the feature packages
// registered in.
func TestValidatePhases(t *testing.T) {
	p := newPlane(t)
	ph := p.s.Engine.Hooks.ValidatePhases()
	iSchema, iTransition := -1, -1
	for i, x := range ph {
		if i > 0 && x < ph[i-1] {
			t.Errorf("validate phases out of order: %v", ph)
		}
		if x == engine.PhaseSchema && iSchema < 0 {
			iSchema = i
		}
		if x == engine.PhaseTransition && iTransition < 0 {
			iTransition = i
		}
	}
	if iSchema < 0 || iTransition < 0 || iSchema > iTransition {
		t.Errorf("schema (%d) must validate before transitions (%d): %v", iSchema, iTransition, ph)
	}
}
