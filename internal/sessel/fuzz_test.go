package sessel_test

import (
	"context"
	"os"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/sky-valley/pagelike/internal/sessel"
)

// FuzzEval checks that no program crashes the parser or the evaluator:
// every input either parses or yields a *ParseError, and evaluation returns
// a value or an error. Seeds come from expressions.yaml when present.
func FuzzEval(f *testing.F) {
	for _, s := range []string{`1 + 2 * 3`, `new div.a#b[c=d] { text: "x", new p {} }`, `${li} from self, "/x/*"`,
		`let [a, ...b] = [1, 2]; b.map((x, i) => { x: x, i: i })`, `"#{ ${h1}.first()?.text() ?? "n" }"`,
		`try { throw { message: "m", type: "T" } } catch (e) { e.type }`, `Temporal.Duration.from("P1Y").total("days", Temporal.PlainDate.from("2026-01-01"))`} {
		f.Add(s)
	}
	if b, err := os.ReadFile(expressionsFile); err == nil {
		var items []map[string]any
		if yaml.Unmarshal(b, &items) == nil {
			for _, it := range items {
				if s, ok := it["expr"].(string); ok {
					f.Add(s)
				}
			}
		}
	}
	h := sessel.NewMemHost()
	d, _ := h.AddHTML("/doc.html", `<!DOCTYPE html><html><body><h1 id="t">A</h1><ul><li>1</li><li>2</li></ul><template><p itemprop="x">t</p></template></body></html>`)
	h.AddHTML("/x/a.html", `<html><body><div itemscope itemtype="https://e.com/T"><meta itemprop="k" content="v"></div></body></html>`)
	f.Fuzz(func(t *testing.T, src string) {
		p, err := sessel.Compile(src)
		if err != nil {
			if !sessel.IsParseError(err) {
				t.Fatalf("compile error is not a ParseError: %T %v", err, err)
			}
			return
		}
		b := sessel.NewBudget()
		b.MaxOps = 20000
		b.Deadline = time.Now().Add(200 * time.Millisecond)
		v, err := p.Eval(context.Background(), &sessel.Env{Host: h, Self: d.Element(), HasSelf: true, Budget: b})
		if err != nil {
			if e, ok := sessel.AsError(err); ok && len(e.Message) > 16 && e.Message[:16] == "internal error: " {
				t.Fatalf("interpreter panic: %v\nsource: %q", e.Message, src)
			}
			return
		}
		_, _ = sessel.EncodeJSON(v)
	})
}
