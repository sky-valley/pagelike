package sessel

import (
	"context"
	"net/http"
	"testing"
)

// triggerSrc is a typical trigger `when` gate (pagelove-polls / shop style):
// header lookups, a regex guard, a thrown HTTPResponse and a site query.
const triggerSrc = `@schema Context url("https://pagelove.org/Context");
@schema HTTPResponse url("https://pagelove.org/HTTPResponse");
let range = Context.request.headers["range"] ?? "";
if (Context.request.method == "PUT" && range.matches("^selector=#r-[a-z0-9]{1,24}$") == false) {
  throw new HTTPResponse { status: 403, message: "<p>Forbidden</p>" }
}
let token = (Context.request.headers["authorization"] ?? "").slice(7);
(${[itemprop="token"]} from "/admin/tokens.html").any(t => t.value() == token)`

func benchHost(b *testing.B) (*MemHost, *Dict) {
	h := NewMemHost()
	if _, err := h.AddHTML("/admin/tokens.html", `<!DOCTYPE html><html><body><ul>
		<li><meta itemprop="token" content="aaa"></li><li><meta itemprop="token" content="bbb"></li>
		<li><meta itemprop="token" content="secret"></li></ul></body></html>`); err != nil {
		b.Fatal(err)
	}
	req := NewRequest("PUT", "/polls/p1.html", http.Header{"Range": {"selector=#r-abc123"}, "Authorization": {"Bearer secret"}}, nil, nil, nil, nil)
	return h, req
}

// BenchmarkTriggerParse measures parsing alone (no cache).
func BenchmarkTriggerParse(b *testing.B) {
	for b.Loop() {
		if _, err := parseProgram(triggerSrc); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTriggerEval measures evaluation of a compiled program.
func BenchmarkTriggerEval(b *testing.B) {
	h, req := benchHost(b)
	p, err := Compile(triggerSrc)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		v, err := p.Eval(context.Background(), &Env{Host: h, Request: req})
		if err != nil || v != true {
			b.Fatalf("%v %v", v, err)
		}
	}
}

// BenchmarkTriggerParseEval measures the Eval(src) entry point: the program
// cache makes repeated sources skip parsing.
func BenchmarkTriggerParseEval(b *testing.B) {
	h, req := benchHost(b)
	b.ReportAllocs()
	for b.Loop() {
		v, err := Eval(context.Background(), triggerSrc, &Env{Host: h, Request: req})
		if err != nil || v != true {
			b.Fatalf("%v %v", v, err)
		}
	}
}
