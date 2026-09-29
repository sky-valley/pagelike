package jsrt

import (
	"bytes"
	"context"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// The sandbox has no ambient capabilities (R-JS-37..39, ADR 0001).

var absentGlobals = []string{"fetch", "process", "require", "setTimeout", "setInterval", "HTTPResponse", "Pagelove",
	"clearTimeout", "clearInterval", "setImmediate", "queueMicrotask", "XMLHttpRequest", "WebSocket", "EventSource",
	"module", "exports", "Buffer", "Deno", "Bun", "window", "self", "navigator", "location", "localStorage", "performance",
	"console", "structuredClone", "URL", "URLSearchParams", "atob", "btoa", "std", "os", "print", "scriptArgs", "gc",
	"Intl", "Temporal", "__pl_dom", "__pl_sys", "__pagelike_driver"}

var presentGlobals = map[string]string{
	"DOMException": "function", "DOMParser": "function", "XMLSerializer": "function", "Node": "function", "Element": "function",
	"HTMLElement": "function", "HTMLAnchorElement": "function", "Document": "function", "crypto": "object", "TextEncoder": "function",
	"TextDecoder": "function", "globalThis": "object", "Math": "object", "JSON": "object", "Map": "function", "Proxy": "function",
	"Reflect": "object", "BigInt": "function", "WeakRef": "function", "SharedArrayBuffer": "function", "InternalError": "function",
	"escape": "function", "eval": "function", "Atomics": "object",
}

func TestAbsentGlobals(t *testing.T) {
	list := `["` + strings.Join(absentGlobals, `","`) + `"]`
	src := `export default () => ` + list + `.filter((n) => { try { return typeof globalThis[n] !== "undefined" || eval("typeof " + n) !== "undefined"; } catch (e) { return false; } }).join(",");`
	if res := mustCall(t, read(src, nil)); res.Value != "" {
		t.Errorf("present in a module: %v", res.Value)
	}
	if res := mustCall(t, CallRequest{Slot: SlotDispatch, Source: src, Args: []Value{}, Document: &Document{HTML: "<p>"}, Context: NewDict()}); res.Value != "" {
		t.Errorf("present in a method: %v", res.Value)
	}
	if res := mustEval(t, ExprRequest{Expression: list + `.filter((n) => typeof globalThis[n] !== "undefined").join(",")`}); res.Value != "" {
		t.Errorf("present in j:: %v", res.Value)
	}
	for _, n := range []string{"fetch", "setTimeout", "process", "require"} {
		f := expectFailure(t, read(`export default () => `+n+`;`, nil), VariantThrew)
		if !strings.HasPrefix(f.Message, "ReferenceError") {
			t.Errorf("%s: %s", n, f.Message)
		}
	}
}

func TestPresentGlobals(t *testing.T) {
	var parts []string
	for n := range presentGlobals {
		parts = append(parts, `"`+n+`:" + typeof `+n)
	}
	res := mustCall(t, read(`export default () => [`+strings.Join(parts, ", ")+`].join("|");`, nil))
	for _, p := range strings.Split(res.Value.(string), "|") {
		n, typ, _ := strings.Cut(p, ":")
		if presentGlobals[n] != typ {
			t.Errorf("%s is %s, want %s", n, typ, presentGlobals[n])
		}
	}
	// Math.random and Date.now are not made deterministic.
	res = mustCall(t, read(`export default () => [typeof Math.random(), Math.random() < 1, Date.now() > 1700000000000, typeof new Date().toISOString()].join("|");`, nil))
	if res.Value != "number|true|true|string" {
		t.Errorf("nondeterminism: %v", res.Value)
	}
	// Blocking is impossible.
	f := expectFailure(t, read(`export default () => Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 1000);`, nil), VariantThrew)
	if !strings.Contains(f.Message, "block") {
		t.Errorf("Atomics.wait: %s", f.Message)
	}
}

func TestOnlyDocumentedGlobalsAreAdded(t *testing.T) {
	// Every own global property that is not an ECMAScript/QuickJS built-in
	// must be a documented platform global.
	res := mustCall(t, CallRequest{Slot: SlotDispatch, Args: []Value{}, Document: &Document{HTML: "<p>"}, Context: NewDict(),
		Source: `export default () => Object.getOwnPropertyNames(globalThis).join(",");`})
	builtins := map[string]bool{}
	for _, n := range strings.Split("Object,Function,Error,EvalError,RangeError,ReferenceError,SyntaxError,TypeError,URIError,InternalError,AggregateError,Iterator,Array,parseInt,parseFloat,isNaN,isFinite,queueMicrotask,decodeURI,decodeURIComponent,encodeURI,encodeURIComponent,escape,unescape,Infinity,NaN,undefined,Number,Boolean,String,Math,Reflect,Symbol,eval,globalThis,Date,RegExp,JSON,Proxy,Map,Set,WeakMap,WeakSet,WeakRef,FinalizationRegistry,ArrayBuffer,SharedArrayBuffer,Uint8ClampedArray,Int8Array,Uint8Array,Int16Array,Uint16Array,Int32Array,Uint32Array,BigInt64Array,BigUint64Array,Float16Array,Float32Array,Float64Array,DataView,Atomics,Promise,BigInt,DisposableStack,AsyncDisposableStack,SuppressedError,Iterator", ",") {
		builtins[n] = true
	}
	documented := map[string]bool{"DOMException": true, "DOMParser": true, "XMLSerializer": true, "Node": true, "Document": true,
		"DocumentFragment": true, "CharacterData": true, "Text": true, "Comment": true, "Element": true, "HTMLElement": true,
		"NodeList": true, "DOMTokenList": true, "crypto": true, "TextEncoder": true, "TextDecoder": true, "document": true, "Context": true}
	for _, d := range ifaceTable {
		documented[d.Name] = true
	}
	for _, n := range strings.Split(res.Value.(string), ",") {
		if !builtins[n] && !documented[n] {
			t.Errorf("undocumented global %q", n)
		}
	}
}

func TestWorkerLockdown(t *testing.T) {
	var buf safeBuffer
	r := New(Options{Size: 1, Logger: slog.New(slog.NewTextHandler(&buf, nil))})
	defer r.Close()
	if err := r.Warm(context.Background()); err != nil {
		t.Fatal(err)
	}
	log := buf.String()
	for _, want := range []string{"CORE=0:true", "FSIZE=0:true", "NOFILE=32:true", "CPU=600:true"} {
		if !strings.Contains(log, want) {
			t.Errorf("lockdown lacks %q: %s", want, log)
		}
	}
	if runtime.GOOS == "linux" && !strings.Contains(log, "no_new_privs:true") {
		t.Errorf("no_new_privs not set: %s", log)
	}
}

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestNoJavaScriptInTheHost checks the isolation design: evaluations report
// the PID of a worker process, never this one.
func TestNoJavaScriptInTheHost(t *testing.T) {
	res := mustCall(t, read(`export default () => 1;`, nil))
	if res.Stats.WorkerPID == 0 || res.Stats.WorkerPID == osGetpid() {
		t.Fatalf("evaluation ran in pid %d (host %d)", res.Stats.WorkerPID, osGetpid())
	}
	if res.Stats.WorkerRSS <= 0 || res.Stats.RoundTrip <= 0 || res.Stats.SourceHash == "" || res.Stats.Kind != "read" {
		t.Errorf("stats %+v", res.Stats)
	}
}

func TestPool(t *testing.T) {
	r := New(Options{Size: 3, MaxCalls: 5})
	defer r.Close()
	// Concurrency beyond the pool size queues.
	var wg sync.WaitGroup
	errs := make(chan string, 64)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, f := r.CallModule(context.Background(), CallRequest{Slot: SlotRead, Source: `export default (v) => v * 2;`, Args: []Value{i}})
			if f != nil || res.Value != int64(2*i) {
				errs <- f.Error()
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	// Workers are recycled after MaxCalls.
	pids := map[int]bool{}
	for i := 0; i < 12; i++ {
		pids[alive(t, r)] = true
	}
	if len(pids) < 3 {
		t.Errorf("expected recycling across 12 calls with MaxCalls=5, saw pids %v", pids)
	}
}

// TestNestedEvaluation: a host callback that evaluates JavaScript again
// (Sessel calling a JavaScript method) must not deadlock on a one-worker
// pool.
func TestNestedEvaluation(t *testing.T) {
	r := New(Options{Size: 1})
	defer r.Close()
	host := &StubHost{Methods: map[string]func(*MethodCall) (*MethodResult, error){}}
	host.Methods[noteURL+"#escalate"] = func(c *MethodCall) (*MethodResult, error) {
		return nil, nil
	}
	var inner *Result
	host.Methods[noteURL+"#escalate"] = func(c *MethodCall) (*MethodResult, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		res, f := r.CallModule(context.WithValue(ctx, inCallbackKey, true), CallRequest{Slot: SlotMethod, Source: `export default (n) => n * 10;`, Args: c.Args})
		if f != nil {
			return nil, f
		}
		inner = res
		return &MethodResult{Value: res.Value}, nil
	}
	res, f := r.CallModule(context.Background(), CallRequest{Slot: SlotMethod, Host: host, Schemas: noteSchemas(), Args: []Value{},
		Source: importNote + `export default () => new Note({}).escalate(4);`})
	if f != nil || res.Value != int64(40) || inner == nil || inner.Stats.WorkerPID == res.Stats.WorkerPID {
		t.Fatalf("nested: %v %v", f, res)
	}
}

func TestCancelAndClose(t *testing.T) {
	r := New(Options{Size: 1})
	pid := alive(t, r)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(100 * time.Millisecond); cancel() }()
	_, f := r.CallModule(ctx, CallRequest{Slot: SlotRead, Source: `export default () => { for (;;) {} };`, Args: []Value{nil}, Budget: Budget{Time: 10 * time.Second}})
	if f == nil || f.Variant != VariantInternal || !strings.Contains(f.Message, "canceled") {
		t.Fatalf("cancel: %v", f)
	}
	if alive(t, r) == pid {
		t.Error("the canceled evaluation's worker should have been replaced")
	}
	r.Close()
	if _, f := r.CallModule(context.Background(), CallRequest{Slot: SlotRead, Source: `export default () => 1;`, Args: []Value{nil}}); f == nil {
		t.Error("calls after Close must fail")
	}
}
