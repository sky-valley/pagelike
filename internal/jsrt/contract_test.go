package jsrt

import (
	"strings"
	"testing"
)

// Module contract: docs/spec/javascript.md A2, A3, A6, A7.

func expectFailure(t *testing.T, req CallRequest, v Variant) *Failure {
	t.Helper()
	res, f := call(t, req)
	if f == nil {
		t.Fatalf("expected %s, got value %#v", v, res.Value)
	}
	if f.Variant != v {
		t.Fatalf("expected %s, got %s: %s\n%s", v, f.Variant, f.Message, f.Stack)
	}
	return f
}

func read(src string, arg Value) CallRequest {
	return CallRequest{Source: src, Slot: SlotRead, Args: []Value{arg}}
}

func TestDefaultExportShapes(t *testing.T) {
	for _, src := range []string{`export default 42;`, `export const x = 1;`, `export default {};`, ``, `// only a comment`} {
		f := expectFailure(t, read(src, nil), VariantShape)
		if f.Message == "" {
			t.Errorf("%q: empty shape message", src)
		}
	}
	// Arrow, function, async function and async arrow forms are accepted.
	for _, src := range []string{
		`export default () => 1;`,
		`export default function () { return 1; }`,
		`export default async function () { return 1; }`,
		`export default async () => 1;`,
		`const f = () => 1; export { f as default };`,
	} {
		if res := mustCall(t, read(src, nil)); res.Value != int64(1) {
			t.Errorf("%q: got %#v", src, res.Value)
		}
	}
	// A class passes the shape check but cannot be called without new.
	f := expectFailure(t, read(`export default class {}`, nil), VariantThrew)
	if !strings.HasPrefix(f.Message, "TypeError") {
		t.Errorf("class default: %s", f.Message)
	}
}

func TestParseFailure(t *testing.T) {
	f := expectFailure(t, read(`export default (`, nil), VariantParse)
	if !strings.HasPrefix(f.Message, "SyntaxError") || !strings.Contains(f.Message, "eval:") {
		t.Errorf("parse message %q", f.Message)
	}
	// A syntax error thrown at runtime is not a parse failure.
	f = expectFailure(t, read(`JSON.parse("{"); export default () => 1;`, nil), VariantThrew)
	if !strings.HasPrefix(f.Message, "SyntaxError") {
		t.Errorf("runtime SyntaxError: %s", f.Message)
	}
	// The legacy assert syntax does not parse in this engine.
	expectFailure(t, read(`import S from "https://x/S" assert { type: "https://pagelove.org/Schema" }; export default () => 1;`, nil), VariantParse)
}

func TestThrewMessageAndStack(t *testing.T) {
	f := expectFailure(t, read("export default function () {\n  return undefined.foo;\n}", nil), VariantThrew)
	if !strings.HasPrefix(f.Message, "TypeError: ") {
		t.Errorf("message %q", f.Message)
	}
	if !strings.Contains(f.Stack, "    at default (eval:2:") {
		t.Errorf("stack %q lacks the default frame in eval", f.Stack)
	}
	// Non-Error thrown values use String(value).
	f = expectFailure(t, read(`export default () => { throw "plain"; }`, nil), VariantThrew)
	if f.Message != "plain" || f.Stack != "" {
		t.Errorf("thrown string: %q %q", f.Message, f.Stack)
	}
	f = expectFailure(t, read(`export default () => { throw 42; }`, nil), VariantThrew)
	if f.Message != "42" {
		t.Errorf("thrown number: %q", f.Message)
	}
	// Top-level code runs on evaluation; its errors are threw.
	f = expectFailure(t, read(`throw new RangeError("top"); export default () => 1;`, nil), VariantThrew)
	if f.Message != "RangeError: top" {
		t.Errorf("top-level throw: %q", f.Message)
	}
	// Internal frames never show.
	f = expectFailure(t, read(`export default () => document.createElement("")`, nil), VariantThrew)
	if strings.Contains(f.Stack, "<eval>") || strings.Contains(f.Stack, "pl:") {
		t.Errorf("stack leaks internal frames: %q", f.Stack)
	}
}

func TestStrictModuleAndThis(t *testing.T) {
	res := mustCall(t, read(`const top = this; export default function () {
		const inner = function () { return this; };
		let r = "no-throw"; try { undeclaredX = 1; } catch (e) { r = e.name; }
		return [top === undefined, inner() === undefined, this === undefined, r].join("|");
	}`, nil))
	if res.Value != "true|true|true|ReferenceError" {
		t.Errorf("got %v", res.Value)
	}
	// default: this is the plain object of set fields, context is the argument.
	res = mustCall(t, CallRequest{Slot: SlotDefault, Source: `export default function (context) {
		return [typeof this, this.title, JSON.stringify(this.tags), context.document_html, arguments.length].join("|");
	}`, This: NewDict("title", "T", "tags", []Value{"a", "b"}), Args: []Value{NewDict("document_html", "<p>x</p>")}})
	if res.Value != `object|T|["a","b"]|<p>x</p>|1` {
		t.Errorf("default this/context: %v", res.Value)
	}
	// An arrow default does not see this.
	res = mustCall(t, CallRequest{Slot: SlotDefault, Source: `export default () => "this-" + typeof this;`, This: NewDict("a", 1), Args: []Value{NewDict()}})
	if res.Value != "this-undefined" {
		t.Errorf("arrow default: %v", res.Value)
	}
	// Schema-level @validate: this is a primitive string, the argument null.
	res = mustCall(t, CallRequest{Slot: SlotSchemaValidate, Source: `export default function (context) {
		return typeof this === "string" && context === null && arguments.length === 1 && this.includes('itemprop="n"');
	}`, This: `<div itemscope><meta itemprop="n" content="1"></div>`, Args: []Value{nil}})
	if res.Value != true {
		t.Errorf("schema validate: %v", res.Value)
	}
	// A method's this is its receiver; arguments are exactly the declared ones.
	res = mustCall(t, CallRequest{Slot: SlotMethod, Source: `export default function (a, b) { return this.name + ":" + a + ":" + b + ":" + arguments.length; }`,
		This: NewDict("name", "n"), Args: []Value{"x", nil}})
	if res.Value != "n:x:null:2" {
		t.Errorf("method: %v", res.Value)
	}
}

func TestWrongArgumentCountIsACallerError(t *testing.T) {
	_, f := call(t, CallRequest{Slot: SlotRead, Source: `export default () => 1;`})
	if f == nil || f.Variant != VariantInternal {
		t.Fatalf("got %v", f)
	}
}

func TestFreshContextPerEvaluation(t *testing.T) {
	src := `let n = 0; globalThis.g = (globalThis.g || 0) + 1; Array.prototype.poisoned = true;
		export default () => "N" + (++n) + ":" + g + ":" + ("poisoned" in []);`
	for i := 0; i < 3; i++ {
		if res := mustCall(t, read(src, nil)); res.Value != "N1:1:true" {
			t.Fatalf("evaluation %d saw state: %v", i, res.Value)
		}
	}
	// A module that poisons built-ins cannot break the next evaluation.
	mustCall(t, read(`JSON.stringify = () => "{}"; Object.keys = () => []; export default () => ({a: 1});`, nil))
	if res := mustCall(t, read(`export default () => JSON.stringify({a: 1});`, nil)); res.Value != `{"a":1}` {
		t.Errorf("poisoned built-ins leaked: %v", res.Value)
	}
}

func TestPoisonedBuiltinsDoNotBreakMarshalling(t *testing.T) {
	res := mustCall(t, read(`export default () => {
		JSON.stringify = () => "garbage"; Object.keys = () => []; Array.isArray = () => false;
		return {a: [1, 2], b: "x"};
	};`, nil))
	d, ok := res.Value.(*Dict)
	if !ok || d.Len() != 2 {
		t.Fatalf("got %#v", res.Value)
	}
}

func TestAsyncSettlement(t *testing.T) {
	res := mustCall(t, read(`export default async (v) => { await null; const x = await Promise.resolve(v); return x + "!"; };`, "ok"))
	if res.Value != "ok!" {
		t.Errorf("async: %v", res.Value)
	}
	// Top-level await.
	res = mustCall(t, read(`const base = await Promise.resolve(40); export default () => base + 2;`, nil))
	if res.Value != int64(42) {
		t.Errorf("TLA: %v", res.Value)
	}
	// A returned thenable is adopted.
	res = mustCall(t, read(`export default () => ({ then(r) { r("adopted"); } });`, nil))
	if res.Value != "adopted" {
		t.Errorf("thenable: %v", res.Value)
	}
	// A rejection is a throw of the reason.
	f := expectFailure(t, read(`export default async () => { throw new TypeError("rejected"); };`, nil), VariantThrew)
	if f.Message != "TypeError: rejected" {
		t.Errorf("rejection: %q", f.Message)
	}
	// A promise nothing can settle fails.
	f = expectFailure(t, read(`export default () => new Promise(() => {});`, nil), VariantThrew)
	if !strings.Contains(f.Message, "never settled") {
		t.Errorf("pending: %q", f.Message)
	}
	// Microtask chains run to completion.
	res = mustCall(t, read(`export default async () => { let n = 0; for (let i = 0; i < 1000; i++) await Promise.resolve().then(() => n++); return n; };`, nil))
	if res.Value != int64(1000) {
		t.Errorf("microtasks: %v", res.Value)
	}
}

func TestReturnValueMarshalling(t *testing.T) {
	res := mustCall(t, read(`const SHARED = {k: 1}; export default () => ({
		n: null, u: undefined, t: true, i: 42, f: 2.5, neg: -7, big: 2 ** 53 - 1, s: "x", l: [1, , "a"],
		m: new Map([[1, "one"], ["k", [2]]]), set: new Set(["a", "b"]), date: new Date(0), err: new Error("e"),
		nested: { deeper: { deepest: [true] } }, shared: [SHARED, SHARED], $type: "user-key"
	})`, nil))
	d := res.Value.(*Dict)
	want := map[string]string{
		"n": "<nil>", "u": "<nil>", "t": "true", "i": "42", "f": "2.5", "neg": "-7", "big": "9007199254740991", "s": "x",
		"l": "[1 <nil> a]", "set": "[a b]", "$type": "user-key",
	}
	for k, w := range want {
		v, ok := d.Get(k)
		if !ok {
			t.Errorf("missing %s", k)
			continue
		}
		if got := fmtValue(v); got != w {
			t.Errorf("%s: got %s want %s", k, got, w)
		}
	}
	if v, _ := d.Get("i"); v != int64(42) {
		t.Errorf("integers are int64, got %T", v)
	}
	if v, _ := d.Get("f"); v != 2.5 {
		t.Errorf("floats are float64, got %T", v)
	}
	m, _ := d.Get("m")
	if md, ok := m.(*Dict); !ok || strings.Join(md.Keys(), ",") != "1,k" {
		t.Errorf("Map: %#v", m)
	}
	for _, k := range []string{"date", "err"} {
		if v, _ := d.Get(k); v.(*Dict).Len() != 0 {
			t.Errorf("%s: expected an empty dictionary (own enumerable properties), got %s", k, fmtValue(v))
		}
	}
	if strings.Join(d.Keys(), ",") != "n,u,t,i,f,neg,big,s,l,m,set,date,err,nested,shared,$type" {
		t.Errorf("key order: %v", d.Keys())
	}
}

func fmtValue(v Value) string {
	b, err := MarshalJSON(v)
	if err != nil {
		return "ERR:" + err.Error()
	}
	s := string(b)
	switch x := v.(type) {
	case nil:
		return "<nil>"
	case string:
		return x
	case []Value:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = fmtValue(e)
		}
		return "[" + strings.Join(parts, " ") + "]"
	}
	return s
}

func TestReturnTypeFailures(t *testing.T) {
	for name, src := range map[string]string{
		"function":        `export default () => () => 1;`,
		"nested function": `export default () => ({a: [1, {f() {}}]});`,
		"symbol":          `export default () => Symbol("s");`,
		"cycle":           `export default () => { const o = {}; o.self = o; return o; };`,
		"array cycle":     `export default () => { const a = []; a.push([a]); return a; };`,
		"NaN":             `export default () => NaN;`,
		"Infinity":        `export default () => [1, -Infinity];`,
		"BigInt":          `export default () => 10n;`,
		"depth 65":        `export default () => { let v = []; for (let i = 0; i < 64; i++) v = [v]; return v; };`,
		"text node":       `export default () => new DOMParser().parseFromString("<p>t</p>", "text/html").querySelector("p").firstChild;`,
		"document":        `export default () => new DOMParser().parseFromString("<p>t</p>", "text/html");`,
		"fragment":        `export default () => new DOMParser().parseFromString("", "text/html").createDocumentFragment();`,
		"comment":         `export default () => new DOMParser().parseFromString("", "text/html").createComment("c");`,
		"class":           `export default () => class {};`,
	} {
		t.Run(name, func(t *testing.T) {
			f := expectFailure(t, read(src, nil), VariantReturnType)
			if f.Message == "" {
				t.Error("empty message")
			}
		})
	}
	// 64 levels are fine; shared, non-cyclic references are fine.
	mustCall(t, read(`export default () => { let v = []; for (let i = 0; i < 63; i++) v = [v]; return v; };`, nil))
	mustCall(t, read(`export default () => { const s = {x: 1}; return [s, {a: s}, [s]]; };`, nil))
	// The fix is named for nodes.
	f := expectFailure(t, read(`export default () => new DOMParser().parseFromString("<p>t</p>", "text/html");`, nil), VariantReturnType)
	if !strings.Contains(f.Message, "document.documentElement") {
		t.Errorf("document message: %s", f.Message)
	}
	f = expectFailure(t, read(`export default () => new DOMParser().parseFromString("<p>t</p>", "text/html").documentElement.firstChild;`, nil), VariantReturnType)
	if !strings.Contains(f.Message, "textContent") {
		t.Errorf("text message: %s", f.Message)
	}
}

func TestMarshalFailures(t *testing.T) {
	for name, v := range map[string]Value{
		"integer beyond 2^53": int64(1) << 60,
		"unsigned too big":    uint64(1) << 60,
		"NaN input":           nan(),
		"unknown Go type":     struct{ X int }{1},
		"channel":             make(chan int),
		"nested bad":          []Value{NewDict("x", struct{}{})},
	} {
		t.Run(name, func(t *testing.T) {
			_, f := call(t, read(`export default (v) => v;`, v))
			if f == nil || f.Variant != VariantMarshal {
				t.Fatalf("got %v", f)
			}
		})
	}
	// Deep inputs are refused before the call.
	var deep Value = "x"
	for i := 0; i < 70; i++ {
		deep = []Value{deep}
	}
	if _, f := call(t, read(`export default (v) => 1;`, deep)); f == nil || f.Variant != VariantMarshal {
		t.Fatalf("deep input: %v", f)
	}
}

func nan() float64 {
	z := 0.0
	return z / z
}

func TestInputMarshalling(t *testing.T) {
	d := NewDict("b", int64(2), "a", 1.5, "$type", "user", "list", []Value{true, nil, "s"}, "map", map[string]any{"z": 1, "y": 2})
	res := mustCall(t, read(`export default (v) => [Object.keys(v).join(","), typeof v.a, v.$type, JSON.stringify(v.list), Object.keys(v.map).join(",")].join("|");`, d))
	if res.Value != `b,a,$type,list,map|number|user|[true,null,"s"]|y,z` {
		t.Errorf("got %v", res.Value)
	}
	// undefined is a value too.
	res = mustCall(t, CallRequest{Slot: SlotMethod, Source: `export default function (a, b) { return [typeof a, typeof b, arguments.length, typeof this].join("|"); }`,
		Args: []Value{Undefined{}, []Value{Undefined{}}}})
	if res.Value != "undefined|object|2|undefined" {
		t.Errorf("undefined: %v", res.Value)
	}
	res = mustCall(t, CallRequest{Slot: SlotMethod, Source: `export default function () { return this === null; }`, This: Null})
	if res.Value != true {
		t.Errorf("null this: %v", res.Value)
	}
	// Temporal values (R-JS-42).
	res = mustCall(t, CallRequest{Slot: SlotMethod, Source: `export default function (i, d, t, dur) {
		return [i instanceof Date, i.toISOString(), d.toISOString(), t, dur, JSON.stringify(i instanceof Date ? {} : 0)].join("|"); }`,
		Args: []Value{Temporal{Kind: "Instant", ISO: "2024-05-06T07:08:09.5Z"}, Temporal{Kind: "PlainDate", ISO: "2024-05-06"},
			Temporal{Kind: "PlainTime", ISO: "07:08"}, Temporal{Kind: "Duration", ISO: "PT1H"}}})
	if res.Value != "true|2024-05-06T07:08:09.500Z|2024-05-06T00:00:00.000Z|07:08|PT1H|{}" {
		t.Errorf("temporal: %v", res.Value)
	}
	// A returned Date is an empty dictionary.
	res = mustCall(t, CallRequest{Slot: SlotMethod, Source: `export default (d) => d;`, Args: []Value{Temporal{Kind: "Instant", ISO: "2024-01-01T00:00:00Z"}}})
	if dd, ok := res.Value.(*Dict); !ok || dd.Len() != 0 {
		t.Errorf("date out: %#v", res.Value)
	}
}

func TestHeadersObject(t *testing.T) {
	h := NewHeaders(map[string][]string{"X-Case-Token": {"t1"}, "Accept": {"a", "b"}})
	ctx := NewDict("request", NewDict("method", "PUT", "headers", h))
	res := mustCall(t, CallRequest{Slot: SlotTriggerWhen, Source: `export default function (ctx) {
		const hs = ctx.request.headers;
		return [hs["X-Case-Token"], hs["x-case-token"], hs.Accept, hs["missing"] === undefined, Object.keys(hs).sort().join(","), "X-CASE-TOKEN" in hs].join("|");
	}`, Args: []Value{ctx}})
	if res.Value != "t1|t1|a, b|true|accept,x-case-token|true" {
		t.Errorf("got %v", res.Value)
	}
}
