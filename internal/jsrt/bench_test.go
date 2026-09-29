package jsrt

import (
	"context"
	"testing"
	"time"
)

// BenchmarkWarmCall measures a warm round trip through the pool: a trivial
// module on an already-started worker (fresh context per evaluation,
// prelude from cached bytecode, IPC both ways).
func BenchmarkWarmCall(b *testing.B) {
	r := New(Options{Size: 1})
	defer r.Close()
	req := CallRequest{Source: `export default (v) => v.trim().toLowerCase();`, Slot: SlotRead, Args: []Value{"  Hello "}}
	ctx := context.Background()
	if _, f := r.CallModule(ctx, req); f != nil {
		b.Fatal(f)
	}
	b.ResetTimer()
	var inWorker int64
	for i := 0; i < b.N; i++ {
		res, f := r.CallModule(ctx, req)
		if f != nil {
			b.Fatal(f)
		}
		inWorker += int64(res.Stats.Elapsed)
	}
	b.ReportMetric(float64(inWorker)/float64(b.N)/1000, "µs-in-worker/op")
}

// BenchmarkWarmCallDOM ships a small document and queries it.
func BenchmarkWarmCallDOM(b *testing.B) {
	r := New(Options{Size: 1})
	defer r.Close()
	req := CallRequest{
		Source:   `export default () => [...document.querySelectorAll("li")].map((li) => li.textContent).join(",") + "|" + document.querySelector("#x").textContent;`,
		Slot:     SlotRead,
		Args:     []Value{nil},
		Document: &Document{HTML: `<!doctype html><html><head><title>t</title></head><body><ul class="items"><li class="a">alpha</li><li class="b">beta</li><li class="c">gamma</li></ul><p id="x">hello <b>world</b></p></body></html>`},
	}
	ctx := context.Background()
	if _, f := r.CallModule(ctx, req); f != nil {
		b.Fatal(f)
	}
	b.ResetTimer()
	var inWorker int64
	for i := 0; i < b.N; i++ {
		res, f := r.CallModule(ctx, req)
		if f != nil {
			b.Fatal(f)
		}
		inWorker += int64(res.Stats.Elapsed)
	}
	b.ReportMetric(float64(inWorker)/float64(b.N)/1000, "µs-in-worker/op")
}

// BenchmarkWarmCallIdle spaces calls out so the worker has prepared its next
// context: the latency a request sees on a pool that is not saturated.
func BenchmarkWarmCallIdle(b *testing.B) {
	r := New(Options{Size: 1})
	defer r.Close()
	req := CallRequest{Source: `export default (v) => v.trim().toLowerCase();`, Slot: SlotRead, Args: []Value{"  Hello "}}
	ctx := context.Background()
	if _, f := r.CallModule(ctx, req); f != nil {
		b.Fatal(f)
	}
	var total, inWorker time.Duration
	for i := 0; i < b.N; i++ {
		time.Sleep(2 * time.Millisecond)
		t0 := time.Now()
		res, f := r.CallModule(ctx, req)
		if f != nil {
			b.Fatal(f)
		}
		total += time.Since(t0)
		inWorker += res.Stats.Elapsed
	}
	b.ReportMetric(float64(total.Microseconds())/float64(b.N), "µs-latency/op")
	b.ReportMetric(float64(inWorker.Microseconds())/float64(b.N), "µs-in-worker/op")
}

// BenchmarkWarmCallParallel and BenchmarkWarmCallIdleParallel repeat the
// two measurements with ParallelPrepare.
func BenchmarkWarmCallParallel(b *testing.B) {
	r := New(Options{Size: 1, ParallelPrepare: true})
	defer r.Close()
	req := CallRequest{Source: `export default (v) => v.trim().toLowerCase();`, Slot: SlotRead, Args: []Value{"  Hello "}}
	ctx := context.Background()
	if _, f := r.CallModule(ctx, req); f != nil {
		b.Fatal(f)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, f := r.CallModule(ctx, req); f != nil {
			b.Fatal(f)
		}
	}
}

func BenchmarkWarmCallIdleParallel(b *testing.B) {
	r := New(Options{Size: 1, ParallelPrepare: true})
	defer r.Close()
	req := CallRequest{Source: `export default (v) => v.trim().toLowerCase();`, Slot: SlotRead, Args: []Value{"  Hello "}}
	ctx := context.Background()
	if _, f := r.CallModule(ctx, req); f != nil {
		b.Fatal(f)
	}
	var total time.Duration
	for i := 0; i < b.N; i++ {
		time.Sleep(2 * time.Millisecond)
		t0 := time.Now()
		if _, f := r.CallModule(ctx, req); f != nil {
			b.Fatal(f)
		}
		total += time.Since(t0)
	}
	b.ReportMetric(float64(total.Microseconds())/float64(b.N), "µs-latency/op")
}

// BenchmarkWarmExpression is a j: binding.
func BenchmarkWarmExpression(b *testing.B) {
	r := New(Options{Size: 1})
	defer r.Close()
	req := ExprRequest{Expression: "a + 1", Scope: NewDict("a", 41)}
	ctx := context.Background()
	if _, f := r.EvalExpression(ctx, req); f != nil {
		b.Fatal(f)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, f := r.EvalExpression(ctx, req); f != nil {
			b.Fatal(f)
		}
	}
}
