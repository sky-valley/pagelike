package reactions_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sky-valley/pagelike/internal/reactions"
)

// Behaviour adopted from live PageLove on 2026-09-29
// (docs/compat/decisions-2026-09-29/reacting.md).

const (
	msgNoAction   = "[https://pagelove.org/Handler].action: cardinality 1..1 violated: expected exactly 1 value, found 0"
	msgTwoActions = "[https://pagelove.org/Handler].action: cardinality 1..1 violated: expected exactly 1 value, found 2"
	msgTwoWhens   = "[https://pagelove.org/Handler].when: cardinality 0..1 violated: expected at most 1 value, found 2"
)

func msgMethod(v string) string {
	return `[https://pagelove.org/RequestHandler].method: Value "` + v + `" is not a valid https://pagelove.org/HTTPMethod (expected one of: "GET", "PUT", "POST", "DELETE", "PATCH", "MOVE", "*")`
}

func transitionHandler(inner string) string {
	return `<div itemscope itemtype="https://pagelove.org/TransitionHandler"><meta itemprop="selector" content="[itemtype='https://t.test/Order']"><meta itemprop="property" content="status"><meta itemprop="becomes" content="processing">` + inner + `</div>`
}

func httpAction(url string) string {
	return `<div itemprop="action" itemscope itemtype="https://pagelove.org/HttpRequest"><meta itemprop="url" content="` + url + `"></div>`
}

// TestPlatformSchemaWholeDocument: a serving-path PUT storing a handler
// its platform schema refuses is 422 with PageLove's messages (R-REACT-4a).
func TestPlatformSchemaWholeDocument(t *testing.T) {
	in := newInstance(t)
	one := sesselItem("action", "1")
	for _, c := range []struct {
		name, item string
		want       []string
	}{
		{"no action", trigger("/d/*", "PUT", sesselItem("when", "true")+sesselItem("otherwise", throw409)), []string{msgNoAction}},
		{"two actions", trigger("/d/*", "PUT", one+one), []string{msgTwoActions}},
		{"two whens", trigger("/d/*", "PUT", sesselItem("when", "true")+sesselItem("when", "true")+one), []string{msgTwoWhens}},
		{"lower-case method", trigger("/d/*", "put", one), []string{msgMethod("put")}},
		{"HEAD", trigger("/d/*", "HEAD", one), []string{msgMethod("HEAD")}},
		{"every problem", trigger("/d/*", "options", ""), []string{msgNoAction, msgMethod("options")}},
		{"processor", processor("/d/*", "get", "", ""), []string{msgNoAction, msgMethod("get")}},
		{"transition handler", transitionHandler(httpAction("http://127.0.0.1:1/a") + httpAction("http://127.0.0.1:1/b")), []string{msgTwoActions}},
	} {
		st, _, body := in.pub("PUT", "/r/bad.html", nil, "<!DOCTYPE html><html><body>"+c.item+"</body></html>")
		if st != 422 || !strings.Contains(body, "https://pagelove.org/SchemaViolation") {
			t.Fatalf("%s: got %d %s", c.name, st, body)
		}
		for _, m := range c.want {
			if !strings.Contains(body, m) {
				t.Fatalf("%s: body lacks %q: %s", c.name, m, body)
			}
		}
		if st, _, _ := in.pub("GET", "/r/bad.html", nil, ""); st != 404 {
			t.Fatalf("%s: the refused document was stored (%d)", c.name, st)
		}
	}
	// Accepted shapes: one action with several otherwise values, every
	// enumerated method, and a gate-less trigger.
	in.install("/r/ok.html", trigger("/d/*", "PUT", sesselItem("when", "true")+one+sesselItem("otherwise", "2")+sesselItem("otherwise", "3"))+
		trigger("/d/*", "*", one)+trigger("/d/*", "PATCH", one)+trigger("/d/*", "MOVE", one))
	// WebDAV writes are not checked; the stored item stays inert or runs.
	in.dav("PUT", "/r/dav.html", "<!DOCTYPE html><html><body>"+trigger("/nowhere/*", "put", "")+"</body></html>")
}

// TestPlatformSchemaSelectorWrites: selector writes check the handlers
// they insert and the handlers enclosing the change, and nothing else.
func TestPlatformSchemaSelectorWrites(t *testing.T) {
	in := newInstance(t)
	in.pub("PUT", "/r/k.html", nil, `<!DOCTYPE html><html><body><div id="box"></div></body></html>`)
	st, _, body := in.pub("POST", "/r/k.html", map[string]string{"Range": "selector=#box"}, trigger("/d/*", "PUT", ""))
	if st != 422 || !strings.Contains(body, msgNoAction) {
		t.Fatalf("inserted trigger: %d %s", st, body)
	}
	in.install("/r/l.html", `<div hidden itemscope itemtype="https://pagelove.org/Trigger"><meta itemprop="resource" content="/nowhere/*"><meta id="m" itemprop="method" content="PUT"><div id="act" itemprop="action" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source" type="text/sessel">1</script></div></div><p id="note">old</p>`)
	st, _, body = in.pub("PUT", "/r/l.html", map[string]string{"Range": "selector=#m"}, `<meta id="m" itemprop="method" content="put">`)
	if st != 422 || !strings.Contains(body, msgMethod("put")) {
		t.Fatalf("enclosing method: %d %s", st, body)
	}
	st, _, body = in.pub("DELETE", "/r/l.html", map[string]string{"Range": "selector=#act"}, "")
	if st != 422 || !strings.Contains(body, msgNoAction) {
		t.Fatalf("enclosing action removed: %d %s", st, body)
	}
	if st, _, _ := in.pub("PUT", "/r/l.html", map[string]string{"Range": "selector=#note"}, `<p id="note">new</p>`); st != 206 {
		t.Fatalf("unrelated selector write: %d", st)
	}
	// A handler stored invalid over WebDAV does not block writes elsewhere
	// in its document, but a whole-document PUT that keeps it is refused.
	in.dav("PUT", "/w/stored.html", `<!DOCTYPE html><html><body>`+trigger("/nowhere/*", "PUT", "")+`<p id="note">old</p></body></html>`)
	if st, _, _ := in.pub("PUT", "/w/stored.html", map[string]string{"Range": "selector=#note"}, `<p id="note">new</p>`); st != 206 {
		t.Fatalf("selector write beside a stored invalid trigger: %d", st)
	}
	if st, _, _ := in.pub("PUT", "/w/stored.html", nil, `<!DOCTYPE html><html><body>`+trigger("/nowhere/*", "PUT", "")+`</body></html>`); st != 422 {
		t.Fatalf("whole PUT keeping the invalid trigger: %d", st)
	}
}

// TestStoredMalformedHandlersStillRun: items written over WebDAV keep
// their documented runtime behaviour (R-REACT-4, 13, 17, 18).
func TestStoredMalformedHandlersStillRun(t *testing.T) {
	in := newInstance(t)
	first := `@schema HTTPResponse url("https://pagelove.org/HTTPResponse");
throw new HTTPResponse { status: 409, message: "first" }`
	second := `@schema HTTPResponse url("https://pagelove.org/HTTPResponse");
throw new HTTPResponse { status: 423, message: "second" }`
	in.dav("PUT", "/r/t.html", "<!DOCTYPE html><html><body>"+
		trigger("/a/*", "put", sesselItem("action", first)+sesselItem("action", second))+
		trigger("/b/*", "PUT", "")+
		"</body></html>")
	if st, _, body := in.pub("PUT", "/a/x.html", nil, "<p>x</p>"); st != 409 || !strings.Contains(body, "first") {
		t.Fatalf("stored two-action lower-case trigger: %d %s", st, body)
	}
	if st, _, _ := in.pub("PUT", "/b/x.html", nil, "<p>x</p>"); st != 201 {
		t.Fatalf("stored action-less trigger is skipped: %d", st)
	}
}

// TestSelectorFilterNotEvaluated: a selector filter never excludes a
// request (R-REACT-14, live 2026-09-29).
func TestSelectorFilterNotEvaluated(t *testing.T) {
	in := newInstance(t)
	withSel := func(item string) string {
		return strings.Replace(item, `<meta itemprop="resource"`, `<meta itemprop="selector" content="#never-present"><meta itemprop="resource"`, 1)
	}
	in.install("/r/t.html", withSel(trigger("/d/*", "PUT", sesselItem("action", throw409)))+
		withSel(trigger("/d/*", "POST", sesselItem("action", throw409)))+
		withSel(trigger("/d/*", "DELETE", sesselItem("action", throw409)))+
		withSel(trigger("/g/*", "GET", sesselItem("action", throw409)))+
		withSel(processor("/p/*", "GET", "", sesselItem("action", `@schema Context url("https://pagelove.org/Context");
Context.response.status = 418`))))
	doc := `<!DOCTYPE html><html><body><h1 id="h">T</h1><ul id="l"><li id="i1">a</li></ul></body></html>`
	in.dav("PUT", "/d/doc.html", doc)
	in.dav("PUT", "/g/doc.html", doc)
	in.dav("PUT", "/p/doc.html", doc)
	for _, c := range []struct{ method, path, rng, body string }{
		{"PUT", "/d/doc.html", "selector=#h", `<h1 id="h">U</h1>`},
		{"PUT", "/d/doc.html", "selector=#missing", `<p id="missing">x</p>`},
		{"POST", "/d/doc.html", "selector=#l", `<li>b</li>`},
		{"DELETE", "/d/doc.html", "selector=#i1", ""},
		{"GET", "/g/doc.html", "selector=#h", ""},
	} {
		if st, _, _ := in.pub(c.method, c.path, map[string]string{"Range": c.rng}, c.body); st != 409 {
			t.Fatalf("%s %s %s: %d, want the trigger's 409", c.method, c.path, c.rng, st)
		}
	}
	if st, _, _ := in.pub("GET", "/p/doc.html", map[string]string{"Range": "selector=#h"}, ""); st != 418 {
		t.Fatalf("processor: %d, want 418", st)
	}
}

// TestResourceBindingsNotBoundInReactions: r: bindings on or above a
// trigger or processor leave the name undefined; e: bindings still bind.
func TestResourceBindingsNotBoundInReactions(t *testing.T) {
	in := newInstance(t)
	in.dav("PUT", "/private/s.html", `<!DOCTYPE html><html><body><div itemscope itemtype="https://t.test/Secret"><meta itemprop="token" content="s3cret"></div></body></html>`)
	read := func(name string) string {
		return `@schema HTTPResponse url("https://pagelove.org/HTTPResponse");
throw new HTTPResponse { status: 409, body: "n=" + ` + name + `.count().String() }`
	}
	head := `<!DOCTYPE html><html xmlns:r="https://pagelove.org/Binding/CSS" xmlns:e="https://pagelove.org/Binding/Sessel" r:top="[itemtype='https://t.test/Secret']"><body>`
	in.pub("PUT", "/r/t.html", nil, head+
		strings.Replace(trigger("/a/*", "PUT", sesselItem("action", read("secret"))), `<div hidden `, `<div hidden r:secret="[itemtype='https://t.test/Secret']" `, 1)+
		trigger("/b/*", "PUT", sesselItem("action", read("top")))+
		strings.Replace(trigger("/c/*", "PUT", sesselItem("action", read("lst"))), `<div hidden `, `<div hidden e:lst="[1, 2]" `, 1)+
		"</body></html>")
	for _, c := range []struct{ path, name string }{{"/a/x.html", "secret"}, {"/b/x.html", "top"}} {
		st, _, body := in.pub("PUT", c.path, nil, "<p>x</p>")
		if st != 500 || !strings.Contains(body, "variable: "+c.name) || strings.Contains(body, "s3cret") {
			t.Fatalf("%s: %d %s", c.path, st, body)
		}
	}
	if st, _, body := in.pub("PUT", "/c/x.html", nil, "<p>x</p>"); st != 409 || body != "n=2" {
		t.Fatalf("e: binding: %d %q", st, body)
	}
}

// TestJavaScriptAnonymousAuth: ctx.request.auth of an anonymous request
// has empty claims and roles (R-REACT-25, live 2026-09-29).
func TestJavaScriptAnonymousAuth(t *testing.T) {
	var got any
	reactions.SetJSRunner(fakeJS{
		"echo-auth": func(arg map[string]any) (any, error) {
			got = arg["request"].(map[string]any)["auth"]
			return nil, nil
		},
	})
	t.Cleanup(func() { reactions.SetJSRunner(nil) })
	in := newInstance(t)
	in.install("/r/t.html", trigger("/d/*", "PUT", jsItem("action", "echo-auth")))
	if st, _, _ := in.pub("PUT", "/d/x.html", nil, "<p>x</p>"); st != 201 {
		t.Fatalf("PUT: %d", st)
	}
	want := map[string]any{"claims": map[string]any{}, "roles": []any{}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("anonymous auth = %#v, want %#v", got, want)
	}
}
