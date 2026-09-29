package compose_test

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/sky-valley/pagelike/internal/compose"
	"github.com/sky-valley/pagelike/internal/jsglue"
	"github.com/sky-valley/pagelike/internal/sessel"
)

// fakeJS stands in for internal/jsrt: it records what composition hands a
// JavaScript runtime and answers canned values.
type fakeJS struct{ bindings []*compose.JSBinding }

func (f *fakeJS) Binding(ctx context.Context, b *compose.JSBinding) (*compose.JSResult, error) {
	f.bindings = append(f.bindings, b)
	switch b.Source {
	case "total * 2":
		return &compose.JSResult{Value: b.Values["total"].(int64) * 2}, nil
	case "request.auth.username":
		return &compose.JSResult{Value: nil, Private: true}, nil
	}
	return &compose.JSResult{Value: int64(60)}, nil
}

func (f *fakeJS) Method(ctx context.Context, m *compose.JSMethod) (*compose.JSResult, error) {
	p := &html.Node{Type: html.ElementNode, Data: "p", DataAtom: atom.P}
	p.AppendChild(&html.Node{Type: html.TextNode, Data: "from " + m.Name + " on " + strings.TrimSpace(m.Self.Node.Data)})
	return &compose.JSResult{Value: sessel.NewElement(p)}, nil
}

// The JavaScript runtime plugs in through SetJSRunner: j: bindings see the
// earlier bindings, their results feed later bindings and templates, and
// JavaScript methods return elements. The seam is exercised with
// composition's own method dispatcher (the integration's is uninstalled).
func TestJSRunnerWiring(t *testing.T) {
	jsglue.Uninstall()
	t.Cleanup(jsglue.Install)
	js := &fakeJS{}
	compose.SetJSRunner(js)
	p := newPlane(t)
	p.allow("GET")
	p.author("/j.html", `<html xmlns:j="https://pagelove.org/Binding/JavaScript" xmlns:p="https://pagelove.org/1.0"><body>
<ul j:total="[10, 20, 30].reduce((a, b) => a + b, 0)" j:doubled="total * 2" p:template="text/liquid"><li>{{ total }}</li><li>{{ doubled }}</li></ul>
<main j:who="request.auth.username"><t:m xmlns:t="urn:t:JS"></t:m></main>
<div hidden itemscope itemtype="https://pagelove.org/Schema"><meta itemprop="type" content="urn:t:JS">
<div itemprop="property" itemscope itemtype="https://pagelove.org/Method"><meta itemprop="name" content="m">
<div itemprop="implementation" itemscope itemtype="https://pagelove.org/JavaScript/Module"><script itemprop="source" type="module">export default () => null</script></div></div></div>
</body></html>`)
	r := p.do("GET", "/j.html", "", nil)
	r.must(t, 200, "<li>60</li><li>120</li>", "<main><p>from m on t:m</p></main>")
	if r.header.Get("Cache-Control") != "private" {
		t.Errorf("a runtime reporting an identity read makes the page private: %q", r.header.Get("Cache-Control"))
	}
	if len(js.bindings) != 3 || js.bindings[1].Names[0] != "total" || js.bindings[0].Request == nil {
		t.Errorf("bindings %+v", js.bindings)
	}
}
