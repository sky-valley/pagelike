package jsrt

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Budgets, limits and hostile scripts (R-JS-55..57, ADR 0001): every one
// ends in a clean failure; the worker survives or is killed and respawned;
// the host process is never at risk (these tests run in it).

func hostile(t *testing.T, r *Runtime, src string, b Budget) (*Failure, time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t0 := time.Now()
	res, f := r.CallModule(ctx, CallRequest{Slot: SlotRead, Source: src, Args: []Value{nil}, Budget: b})
	if f == nil {
		t.Fatalf("expected a failure, got %#v", res.Value)
	}
	return f, time.Since(t0)
}

// alive checks the runtime still evaluates, returning the worker PID.
func alive(t *testing.T, r *Runtime) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, f := r.CallModule(ctx, CallRequest{Slot: SlotRead, Source: `export default (v) => v + 1;`, Args: []Value{41}})
	if f != nil || res.Value != int64(42) {
		t.Fatalf("runtime no longer evaluates: %v %v", f, res)
	}
	return res.Stats.WorkerPID
}

func TestTimeouts(t *testing.T) {
	r := New(Options{Size: 1})
	defer r.Close()
	pid := alive(t, r)
	for name, src := range map[string]string{
		"infinite loop":         `export default () => { for (;;) {} };`,
		"catch loop":            `export default () => { for (;;) { try { for (;;) {} } catch (e) {} } };`,
		"backtracking regex":    `export default () => /^(a+)+(?=c)/.test("a".repeat(40) + "b");`,
		"RE2-style regex":       `export default () => /^(a+)+$/.test("a".repeat(40) + "b");`,
		"async loop":            `export default async () => { for (;;) { await null; } };`,
		"top-level loop":        `for (;;) {} export default () => 1;`,
		"finally loop":          `export default () => { try { return 1; } finally { for (;;) {} } };`,
		"sort comparator loop":  `export default () => [3, 1, 2].sort(() => { for (;;) {} });`,
		"getter loop in result": `export default () => ({ get x() { for (;;) {} } });`,
	} {
		t.Run(name, func(t *testing.T) {
			f, el := hostile(t, r, src, Budget{Time: 100 * time.Millisecond})
			if f.Variant != VariantTimeout {
				t.Fatalf("got %s: %s", f.Variant, f.Message)
			}
			if el > 2*time.Second {
				t.Errorf("took %v", el)
			}
			if p := alive(t, r); p != pid {
				t.Errorf("the engine interrupt should have handled it, but the worker changed (%d -> %d): %s", pid, p, f.Message)
			}
		})
	}
}

func TestContextDeadlineBoundsBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	t0 := time.Now()
	_, f := rt(t).CallModule(ctx, CallRequest{Slot: SlotRead, Source: `export default () => { for (;;) {} };`, Args: []Value{nil}, Budget: Budget{Time: 10 * time.Second}})
	if f == nil || f.Variant != VariantTimeout || time.Since(t0) > 2*time.Second {
		t.Fatalf("got %v after %v", f, time.Since(t0))
	}
}

func TestOutOfMemory(t *testing.T) {
	if raceEnabled {
		t.Skip("time-bounded memory bombs are too slow under the race detector")
	}
	r := New(Options{Size: 1})
	defer r.Close()
	for name, src := range map[string]string{
		"array bomb":      `export default () => { const a = []; for (;;) a.push(new Array(100000).fill(7)); };`,
		"map bomb":        `export default () => { const m = new Map(); for (let i = 0; ; i++) m.set(i, { i }); };`,
		"caught map bomb": `export default () => { const m = new Map(); try { for (let i = 0; ; i++) m.set(i, { i }); } catch (e) { throw e; } };`,
		"typed array":     `export default () => new Uint8Array(1 << 30).fill(1).length;`,
		"object graph":    `export default () => { let o = {}; for (;;) o = { o, pad: "x".repeat(100) + Math.random() }; };`,
		"global bomb":     `export default () => { globalThis.keep = []; for (;;) keep.push("x".repeat(1000) + Math.random()); };`,
		"DOM node bomb":   `export default () => { const d = new DOMParser().parseFromString("<body></body>", "text/html"); for (;;) d.body.appendChild(d.createElement("div")); };`,
		"DOM text bomb":   `export default () => { const d = new DOMParser().parseFromString("<body></body>", "text/html"); const s = "x".repeat(100000); for (;;) d.body.append(s); };`,
	} {
		t.Run(name, func(t *testing.T) {
			f, el := hostile(t, r, src, Budget{Time: 5 * time.Second, Memory: 16 << 20})
			if f.Variant != VariantOutOfMemory {
				t.Fatalf("got %s: %s", f.Variant, f.Message)
			}
			// The variant proves the memory limit fired, not the deadline;
			// how long that takes depends on machine load, so it is logged.
			if el > 3*time.Second {
				t.Logf("took %v", el)
			}
			alive(t, r)
		})
	}
	// A string that outgrows the engine's length limit is a thrown error.
	f, _ := hostile(t, r, `export default () => { let s = "x"; for (;;) s = s + s; };`, Budget{Memory: 16 << 20})
	if f.Variant != VariantOutOfMemory && f.Variant != VariantThrew {
		t.Fatalf("string doubling: %s %s", f.Variant, f.Message)
	}
	// A user's own `throw null` is not mistaken for memory exhaustion.
	f, _ = hostile(t, r, `export default () => { throw null; };`, Budget{})
	if f.Variant != VariantThrew || f.Message != "null" {
		t.Fatalf("throw null: %s %s", f.Variant, f.Message)
	}
	alive(t, r)
}

func TestStackOverflowIsThrew(t *testing.T) {
	r := New(Options{Size: 1})
	defer r.Close()
	pid := alive(t, r)
	for name, src := range map[string]string{
		"recursion":       `const f = (n) => f(n + 1) + 1; export default () => f(0);`,
		"method":          `class A { m(n) { return this.m(n + 1) + 1; } } export default () => new A().m(0);`,
		"getter":          `const o = { get x() { return this.x + 1; } }; export default () => o.x;`,
		"toString":        `const o = { toString() { return "" + o; } }; export default () => "" + o;`,
		"proxy chain":     `export default () => { let p = {}; for (let i = 0; i < 1e5; i++) p = new Proxy(p, {}); return p.x; };`,
		"generator":       `function* g(n) { yield* g(n + 1); } export default () => [...g(0)].length;`,
		"sort recursion":  `const a = [1, 2, 3]; const f = () => a.sort(() => f()); export default () => f();`,
		"JSON.parse":      `export default () => JSON.parse("[".repeat(1e6) + "]".repeat(1e6)).length;`,
		"RegExp nesting":  `export default () => new RegExp("(?:".repeat(1e5) + "a" + ")".repeat(1e5)).test("a");`,
		"parser nesting":  `export default () => eval("[".repeat(1e5) + "]".repeat(1e5)).length;`,
		"caught overflow": `const f = (n) => f(n + 1); export default () => { try { f(0); } catch (e) { throw e; } };`,
	} {
		t.Run(name, func(t *testing.T) {
			// Enough memory that the payloads reach their recursive phase.
			f, _ := hostile(t, r, src, Budget{Time: 5 * time.Second, Memory: 128 << 20})
			if f.Variant != VariantThrew || !strings.Contains(f.Message, "stack overflow") {
				t.Fatalf("got %s: %s", f.Variant, f.Message)
			}
			if p := alive(t, r); p != pid {
				t.Errorf("the worker was replaced (%d -> %d)", pid, p)
			}
		})
	}
}

func TestNativeNestingBombs(t *testing.T) {
	if raceEnabled {
		t.Skip("time-bounded memory bombs are too slow under the race detector")
	}
	r := New(Options{Size: 1})
	defer r.Close()
	for name, src := range map[string]string{
		"Array.join":     `export default () => { let a = []; for (let i = 0; i < 1e6; i++) a = [a]; return String(a).length; };`,
		"JSON.stringify": `export default () => { let a = []; for (let i = 0; i < 1e6; i++) a = [a]; return JSON.stringify(a).length; };`,
		"flat":           `export default () => { let a = []; for (let i = 0; i < 1e6; i++) a = [a]; return a.flat(Infinity).length; };`,
	} {
		t.Run(name, func(t *testing.T) {
			// A large memory budget lets the payload reach its recursive phase.
			f, _ := hostile(t, r, src, Budget{Time: 10 * time.Second, Memory: 256 << 20})
			if !(f.Variant == VariantThrew && strings.Contains(f.Message, "stack overflow") || f.Variant == VariantOutOfMemory) {
				t.Fatalf("got %s: %s", f.Variant, f.Message)
			}
			alive(t, r)
		})
		t.Run(name+" small budget", func(t *testing.T) {
			f, _ := hostile(t, r, src, Budget{Time: 10 * time.Second})
			if f.Variant != VariantOutOfMemory && !strings.Contains(f.Message, "stack overflow") {
				t.Fatalf("got %s: %s", f.Variant, f.Message)
			}
			alive(t, r)
		})
	}
}

// TestRecursionDepth measures the frame limit: at least 500 frames must
// succeed (R-JS-56).
func TestRecursionDepth(t *testing.T) {
	req := read(`const f = (n) => (n === 0 ? 0 : 1 + f(n - 1));
		export default () => { let d = 0; try { for (d = 100; d < 100000; d += 10) f(d); } catch (e) { return d - 10; } return -1; };`, nil)
	req.Budget.Time = 30 * time.Second
	res := mustCall(t, req)
	depth := res.Value.(int64)
	t.Logf("maximum JavaScript recursion depth: %d frames", depth)
	if depth < 500 {
		t.Fatalf("depth %d < 500", depth)
	}
	if res := mustCall(t, read(`const f = (n) => (n === 0 ? 0 : 1 + f(n - 1)); export default () => f(500) === 500;`, nil)); res.Value != true {
		t.Fatal("500 frames failed")
	}
}

func TestMicrotaskFlood(t *testing.T) {
	r := New(Options{Size: 1})
	defer r.Close()
	f, el := hostile(t, r, `export default () => { const f = () => Promise.resolve().then(f); f(); f(); return new Promise(() => {}); };`, Budget{Time: 200 * time.Millisecond})
	// The flood either runs out of time, or out of memory (after which the
	// queue drains and the binding's own promise can never settle).
	if f.Variant != VariantTimeout && f.Variant != VariantOutOfMemory && !strings.Contains(f.Message, "never settled") {
		t.Fatalf("got %s: %s", f.Variant, f.Message)
	}
	if el > 3*time.Second {
		t.Errorf("took %v", el)
	}
	alive(t, r)
}

func TestDOMOperationBudget(t *testing.T) {
	f, _ := hostile(t, rt(t), `export default () => { const d = new DOMParser().parseFromString("<p>x</p>", "text/html"); for (let i = 0; ; i++) { try { d.querySelector("p"); } catch (e) {} } };`,
		Budget{Time: 5 * time.Second, DOMOps: 1000})
	if f.Variant != VariantTimeout || !strings.Contains(f.Message, "DOM operation") {
		t.Fatalf("got %s: %s", f.Variant, f.Message)
	}
	res := mustCall(t, CallRequest{Slot: SlotRead, Args: []Value{nil}, Budget: Budget{DOMOps: 1000},
		Source: `export default () => { const d = new DOMParser().parseFromString("<p>x</p>", "text/html"); for (let i = 0; i < 10; i++) d.querySelector("p"); return 1; };`})
	if res.Stats.DOMOps < 11 {
		t.Errorf("DOM ops not counted: %+v", res.Stats)
	}
}

// TestParentWatchdogKills covers a stall the engine interrupt cannot reach:
// one long native DOM operation. The parent SIGKILLs the worker at budget +
// grace and the pool respawns it.
func TestParentWatchdogKills(t *testing.T) {
	r := New(Options{Size: 1, Grace: 100 * time.Millisecond})
	defer r.Close()
	pid := alive(t, r)
	f, el := hostile(t, r, `export default () => {
		const d = new DOMParser().parseFromString("<p></p>".repeat(3000), "text/html");
		return d.querySelectorAll("p:has(~ p ~ p ~ p ~ p ~ p ~ p)").length;
	};`, Budget{Time: 200 * time.Millisecond})
	if f.Variant != VariantTimeout {
		t.Fatalf("got %s: %s", f.Variant, f.Message)
	}
	if el > 3*time.Second {
		t.Errorf("took %v", el)
	}
	if p := alive(t, r); p == pid {
		t.Errorf("expected a new worker after the kill")
	}
}

// TestRSSCeiling: a worker that outgrows its RSS ceiling is killed (by
// itself or by the parent) and reported as out-of-memory.
func TestRSSCeiling(t *testing.T) {
	if raceEnabled {
		t.Skip("time-bounded memory bombs are too slow under the race detector")
	}
	r := New(Options{Size: 1, RSSLimit: 160 << 20})
	defer r.Close()
	pid := alive(t, r)
	f, _ := hostile(t, r, `export default () => { const a = []; for (;;) a.push(new Array(100000).fill(Math.random())); };`,
		Budget{Time: 20 * time.Second, Memory: 2 << 30})
	if f.Variant != VariantOutOfMemory {
		t.Fatalf("got %s: %s", f.Variant, f.Message)
	}
	if p := alive(t, r); p == pid {
		t.Errorf("expected a new worker")
	}
}

func TestHTTPResponse(t *testing.T) {
	throwWith := func(slot Slot, obj string) (*Failure, bool) {
		_, f := call(t, CallRequest{Slot: slot, Source: `export default () => { throw ` + obj + `; };`, Args: []Value{nil}})
		if f == nil {
			t.Fatalf("no failure for %s", obj)
		}
		return f, f.Response != nil
	}
	f, ok := throwWith(SlotWrite, `{ schema_url: "https://pagelove.org/HTTPResponse", status: 422, message: "bad postcode" }`)
	if !ok || f.Response.Status != 422 || f.Response.Message != "bad postcode" || f.Response.Body != "bad postcode" || f.Variant != VariantThrew {
		t.Fatalf("got %+v %+v", f, f.Response)
	}
	f, ok = throwWith(SlotTriggerAction, `{ itemtype: "https://pagelove.org/HTTPResponse", message: "no-status" }`)
	if !ok || f.Response.Status != 500 {
		t.Fatalf("default status: %+v", f.Response)
	}
	f, ok = throwWith(SlotProcessorAction, `{ schema_url: "https://pagelove.org/HTTPResponse", status: 409, body: "body-wins", message: "m",
		headers: { "X-From": "yes", "Content-Type": "text/javascript", "bad name": "x", "X-Ctl": "a\nb" } }`)
	if !ok || f.Response.Body != "body-wins" || len(f.Response.Headers) != 2 || f.Response.Headers[0] != [2]string{"X-From", "yes"} {
		t.Fatalf("body/headers: %+v", f.Response)
	}
	// Not honoured outside @write, @validate and actions.
	for _, slot := range []Slot{SlotRead, SlotComputed, SlotSchemaValidate, SlotTriggerWhen} {
		if f, ok := throwWith(slot, `{ schema_url: "https://pagelove.org/HTTPResponse", status: 409, message: "m" }`); ok || f.Variant != VariantThrew {
			t.Errorf("%s honoured HTTPResponse", slot)
		}
	}
	// Ordinary failures: no marker, bad status, Errors, primitives, new HTTPResponse.
	for _, obj := range []string{`{ status: 409, message: "nope" }`, `{ schema_url: "https://pagelove.org/HTTPResponse", status: 700 }`,
		`{ schema_url: "https://pagelove.org/HTTPResponse", status: "409" }`, `new Error("e")`, `"https://pagelove.org/HTTPResponse"`,
		`Object.create({ schema_url: "https://pagelove.org/HTTPResponse" })`} {
		if _, ok := throwWith(SlotValidate, obj); ok {
			t.Errorf("%s was honoured", obj)
		}
	}
	f = expectFailure(t, CallRequest{Slot: SlotValidate, Source: `export default () => { throw new HTTPResponse(409); };`, Args: []Value{nil}}, VariantThrew)
	if !strings.HasPrefix(f.Message, "ReferenceError") {
		t.Errorf("new HTTPResponse: %s", f.Message)
	}
	// Rejections count too.
	_, f = call(t, CallRequest{Slot: SlotValidate, Args: []Value{nil}, Source: `export default async () => { await null; throw { schema_url: "https://pagelove.org/HTTPResponse", status: 418 }; };`})
	if f == nil || f.Response == nil || f.Response.Status != 418 {
		t.Errorf("rejected HTTPResponse: %+v", f)
	}
}

func TestFailureItem(t *testing.T) {
	f := &Failure{Variant: VariantThrew, Message: "TypeError: <x>", Stack: "    at default (eval:2:17)"}
	h := f.HTML()
	for _, want := range []string{`itemtype="https://pagelove.org/BindingFailure"`, `content="threw"`, `TypeError: &lt;x&gt;`, `<pre itemprop="stack">    at default (eval:2:17)</pre>`, `itemprop="failure"`} {
		if !strings.Contains(h, want) {
			t.Errorf("item lacks %q: %s", want, h)
		}
	}
	if !(&Failure{Variant: VariantTimeout}).Budget() || (&Failure{Variant: VariantThrew}).Budget() {
		t.Error("Budget()")
	}
}
