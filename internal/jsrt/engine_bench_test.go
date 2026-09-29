package jsrt

import (
	"testing"
	"time"

	"modernc.org/quickjs"
)

// BenchmarkEngineInProcess runs the worker's evaluation directly (no IPC),
// to see where in-worker time goes. Only the benchmark runs an engine in
// the test process; the runtime never does.
func BenchmarkEngineInProcess(b *testing.B) {
	e := newEngine(workerConfig{stackSlots: DefaultStackSlots, parallelPrepare: true})
	e.start()
	msg := &callMsg{Source: `export default (v) => v.trim().toLowerCase();`, Kind: "read", ArgCount: 1, DocMode: docNone,
		Args: `["  Hello "]`, TimeoutNS: int64(time.Second), Memory: DefaultMemory}
	for i := 0; i < b.N; i++ {
		res := e.evaluate(msg, nil)
		if res.Outcome != "ok" {
			b.Fatal(res.Outcome, res.Message)
		}
	}
}

func BenchmarkPrepareAndClose(b *testing.B) {
	e := newEngine(workerConfig{stackSlots: DefaultStackSlots})
	m := newMeter()
	var tNew, tPrelude, tClose time.Duration
	for i := 0; i < b.N; i++ {
		t0 := time.Now()
		vm, _ := quickjs.NewVM()
		t1 := time.Now()
		st := &evalState{e: e, vm: vm, dom: newDOMState(0, 0)}
		_ = vm.RegisterHostFunc("__pl_dom", st.domHost)
		_ = vm.RegisterHostFunc("__pl_sys", st.sysHost)
		bc, err := e.code(m, &e.bytecode, preludeSource)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := vm.EvalBytecode(bc); err != nil {
			b.Fatal(err)
		}
		t2 := time.Now()
		vm.Close()
		t3 := time.Now()
		tNew += t1.Sub(t0)
		tPrelude += t2.Sub(t1)
		tClose += t3.Sub(t2)
	}
	b.ReportMetric(float64(tNew.Microseconds())/float64(b.N), "µs-newvm")
	b.ReportMetric(float64(tPrelude.Microseconds())/float64(b.N), "µs-prelude")
	b.ReportMetric(float64(tClose.Microseconds())/float64(b.N), "µs-close")
}
