package reactions_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sky-valley/pagelike/internal/reactions"
)

// fakeJS stands in for internal/jsrt: modules are looked up by source text.
type fakeJS map[string]func(arg map[string]any) (any, error)

func (f fakeJS) CallDefault(_ context.Context, c *reactions.JSCall) (any, error) {
	if fn, ok := f[strings.TrimSpace(c.Source)]; ok {
		return fn(c.Arg)
	}
	return nil, &reactions.JSError{Variant: "threw", Message: "ReferenceError: unknown module"}
}

func jsItem(prop, src string) string {
	return `<div itemprop="` + prop + `" itemscope itemtype="https://pagelove.org/JavaScript/Module"><script type="module" itemprop="source">` + src + `</script></div>`
}

func TestJavaScriptThroughRunner(t *testing.T) {
	reactions.SetJSRunner(fakeJS{
		"gate-empty": func(map[string]any) (any, error) { return []any{}, nil },
		"gate-put": func(arg map[string]any) (any, error) {
			return arg["request"].(map[string]any)["method"] == "PUT", nil
		},
		"throw": func(map[string]any) (any, error) {
			return nil, &reactions.ThrownResponse{Status: 409, Body: "a && b", HasBody: true, Headers: [][2]string{{"X-Test", "yes"}, {"Content-Type", "text/plain"}}}
		},
		"mutate": func(arg map[string]any) (any, error) {
			arg["response"].(map[string]any)["status"] = 418 // never read back
			return nil, nil
		},
		"boom": func(map[string]any) (any, error) {
			return nil, &reactions.JSError{Variant: "threw", Message: "Error: boom", Stack: "at default (eval:1:1)"}
		},
	})
	t.Cleanup(func() { reactions.SetJSRunner(nil) })
	in := newInstance(t)
	in.install("/r/a.html", trigger("/a/*", "PUT", jsItem("when", "gate-empty")+sesselItem("action", throw409)))
	if st, _, _ := in.pub("PUT", "/a/x.html", nil, "<p>x</p>"); st >= 300 {
		t.Fatalf("an empty array gate is falsy (C11): %d", st)
	}
	in.install("/r/b.html", trigger("/b/*", "", jsItem("when", "gate-put")+jsItem("action", "throw")))
	st, h, body := in.pub("PUT", "/b/x.html", nil, "<p>x</p>")
	if st != 409 || body != "a && b" || h.Get("X-Test") != "yes" || h.Get("Content-Type") != "text/plain" {
		t.Fatalf("JS throw: %d %v %q", st, h, body)
	}
	if st, _, _ := in.pub("GET", "/b/x.html", nil, ""); st != 404 {
		t.Fatalf("gate should not pass GET: %d", st)
	}
	in.dav("PUT", "/c/doc.html", "<!DOCTYPE html><html><body><h1>C</h1></body></html>")
	in.install("/r/c.html", processor("/c/*", "GET", "", jsItem("action", "mutate")))
	if st, _, body := in.pub("GET", "/c/doc.html", nil, ""); st != 200 || !strings.Contains(body, "<h1>C</h1>") {
		t.Fatalf("JS mutation must be ignored: %d %s", st, body)
	}
	in.install("/r/d.html", trigger("/d/*", "PUT", jsItem("action", "boom")))
	st, _, body = in.pub("PUT", "/d/x.html", nil, "<p>x</p>")
	if st != 500 || !strings.Contains(body, "https://pagelove.org/BindingFailure") || !strings.Contains(body, "at default") {
		t.Fatalf("JS error: %d %s", st, body)
	}
}

// slotJS records the slot of every JavaScript call; a module whose source
// is "no" answers false.
type slotJS struct {
	mu    sync.Mutex
	slots []string
}

func (s *slotJS) CallDefault(_ context.Context, c *reactions.JSCall) (any, error) {
	src := strings.TrimSpace(c.Source)
	s.mu.Lock()
	s.slots = append(s.slots, src+"="+c.Slot)
	s.mu.Unlock()
	return src != "no", nil
}

// Each JavaScript call names its slot, which decides whether a thrown
// HTTPResponse is honoured (actions) or an ordinary failure (gates).
func TestJavaScriptSlots(t *testing.T) {
	js := &slotJS{}
	reactions.SetJSRunner(js)
	t.Cleanup(func() { reactions.SetJSRunner(nil) })
	in := newInstance(t)
	in.install("/r/s.html", trigger("/s/*", "PUT", jsItem("when", "no")+jsItem("action", "act")+jsItem("otherwise", "else")))
	in.dav("PUT", "/p/doc.html", "<!DOCTYPE html><html><body><h1>P</h1></body></html>")
	in.install("/r/p.html", processor("/p/*", "GET", "", jsItem("when", "yes")+jsItem("action", "pact")))
	in.pub("PUT", "/s/x.html", nil, "<p>x</p>")
	in.pub("GET", "/p/doc.html", nil, "")
	js.mu.Lock()
	got := strings.Join(js.slots, ",")
	js.mu.Unlock()
	want := "no=" + reactions.SlotTriggerWhen + ",else=" + reactions.SlotTriggerOtherwise + ",yes=" + reactions.SlotProcessorWhen + ",pact=" + reactions.SlotProcessorAction
	if got != want {
		t.Errorf("slots %s, want %s", got, want)
	}
}
