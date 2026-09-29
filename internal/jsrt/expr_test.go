package jsrt

import (
	"context"
	"strings"
	"testing"
	"time"
)

// j: expression bindings (R-JS-30..36), Context (R-JS-17/18) and the
// request object (R-JS-34).

func eval(t *testing.T, req ExprRequest) (*Result, *Failure) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return rt(t).EvalExpression(ctx, req)
}

func mustEval(t *testing.T, req ExprRequest) *Result {
	t.Helper()
	res, f := eval(t, req)
	if f != nil {
		t.Fatalf("unexpected failure %s: %s\n%s", f.Variant, f.Message, f.Stack)
	}
	return res
}

func TestExpressionScope(t *testing.T) {
	scope := NewDict("total", 60, "class", "kw", "first-name", "Ada", "myval", "lc", "items", []Value{1, 2, 3})
	res := mustEval(t, ExprRequest{Scope: scope, Expression: `[total * 2, Context["class"], Context["first-name"], myval, items.length, typeof Context.request, this === undefined].join("|")`})
	if res.Value != "120|kw|Ada|lc|3|undefined|true" {
		t.Errorf("got %v", res.Value)
	}
	// Reserved words and non-identifiers are not bare variables.
	if _, f := eval(t, ExprRequest{Scope: scope, Expression: `first`}); f == nil || f.Variant != VariantThrew || !strings.HasPrefix(f.Message, "ReferenceError") {
		t.Errorf("unknown identifier: %v", f)
	}
	// Numbers stay numbers.
	res = mustEval(t, ExprRequest{Expression: `[10, 20, 30].reduce((a, b) => a + b, 0)`})
	if res.Value != int64(60) {
		t.Errorf("reduce: %#v", res.Value)
	}
}

func TestExpressionAwaitAndAdoption(t *testing.T) {
	if res := mustEval(t, ExprRequest{Expression: `await Promise.resolve(41).then((x) => x + 1)`}); res.Value != int64(42) {
		t.Errorf("await: %v", res.Value)
	}
	if res := mustEval(t, ExprRequest{Expression: `Promise.resolve("adopted" + "-value")`}); res.Value != "adopted-value" {
		t.Errorf("adoption: %v", res.Value)
	}
	res := mustEval(t, ExprRequest{Expression: `'SHA:' + Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode('abc')))).map((b) => b.toString(16).padStart(2, '0')).join('')`})
	if res.Value != "SHA:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Errorf("digest: %v", res.Value)
	}
}

func TestExpressionFailures(t *testing.T) {
	for name, c := range map[string]struct {
		expr string
		v    Variant
	}{
		"parse":              {`1 +`, VariantParse},
		"statements":         {`1; 2`, VariantParse},
		"wrapper injection":  {`1); globalThis.x = (2`, VariantParse},
		"bracket injection":  {`1)]; (0, [2`, VariantParse},
		"throw":              {`(() => { throw new Error("boom"); })()`, VariantThrew},
		"reject":             {`await Promise.reject(new Error("no"))`, VariantThrew},
		"unknown identifier": {`notDefinedAnywhere + 1`, VariantThrew},
		"function result":    {`() => 1`, VariantReturnType},
		"dynamic import":     {`import("x")`, VariantImportNotAllowed},
		"timeout":            {`(() => { for (;;) {} })()`, VariantTimeout},
	} {
		t.Run(name, func(t *testing.T) {
			_, f := eval(t, ExprRequest{Expression: c.expr, Budget: Budget{Time: 200 * time.Millisecond}})
			if f == nil || f.Variant != c.v {
				t.Fatalf("got %v", f)
			}
		})
	}
	// A comment in the expression cannot swallow the wrapper.
	if res := mustEval(t, ExprRequest{Expression: `1 + 1 // trailing comment`}); res.Value != int64(2) {
		t.Errorf("comment: %v", res.Value)
	}
}

func TestExpressionFreshContext(t *testing.T) {
	mustEval(t, ExprRequest{Expression: `(globalThis.k = 5, 1)`})
	if res := mustEval(t, ExprRequest{Expression: `typeof globalThis.k`}); res.Value != "undefined" {
		t.Errorf("state leaked: %v", res.Value)
	}
}

func TestRequestTaint(t *testing.T) {
	req := &Request{Method: "GET", Path: "/p/j.html", Query: NewDict("q", "hello"),
		Headers: NewHeaders(map[string][]string{"X-Probe": {"1"}}), Auth: NewDict("username", "ada", "claims", NewDict("email", "a@x"))}
	for expr, want := range map[string]bool{
		`[request.method, request.path, request.query.q].join("|")`: false,
		`typeof request`:                        false,
		`request.path.length`:                   false,
		`typeof request.headers`:                true,
		`request.headers["x-probe"]`:            true,
		`request.auth.username`:                 true,
		`"auth" in request`:                     true,
		`Object.keys(request).length`:           true,
		`JSON.stringify(Context.request.query)`: false,
		`JSON.stringify(Context.request)`:       true,
	} {
		res := mustEval(t, ExprRequest{Expression: expr, Request: req})
		if res.Tainted != want {
			t.Errorf("%s: tainted=%v, want %v (value %v)", expr, res.Tainted, want, res.Value)
		}
	}
	res := mustEval(t, ExprRequest{Expression: `[request.method, request.path, request.query.q, request.headers["X-PROBE"], request.auth.claims.email].join("|")`, Request: req})
	if res.Value != "GET|/p/j.html|hello|1|a@x" {
		t.Errorf("members: %v", res.Value)
	}
	// Anonymous: auth is undefined, and reading it still taints.
	res = mustEval(t, ExprRequest{Expression: `typeof request.auth`, Request: &Request{Method: "GET", Path: "/"}})
	if res.Value != "undefined" || !res.Tainted {
		t.Errorf("anonymous auth: %v tainted=%v", res.Value, res.Tainted)
	}
}

func TestContextWriteBack(t *testing.T) {
	host := &StubHost{}
	res := mustCall(t, CallRequest{Slot: SlotDispatch, Host: host, Args: []Value{"hi"}, Context: NewDict("existing", 1),
		Source: `export default function (value) {
			Context.greeting = "CTX-" + value;
			Context.list = [1, { a: true }];
			Context.el = new DOMParser().parseFromString("<b>x</b>", "text/html").querySelector("b");
			delete Context.existing;
			let bad = "ok"; try { Context.fn = () => 1; } catch (e) { bad = e.name; }
			return bad + "|" + Context.greeting + "|" + ("existing" in Context);
		}`})
	if res.Value != "TypeError|CTX-hi|false" {
		t.Errorf("value %v", res.Value)
	}
	if len(host.Writes) != 4 || host.Writes[0].Name != "greeting" || host.Writes[0].Value != "CTX-hi" || !host.Writes[3].Deleted {
		t.Fatalf("host writes %+v", host.Writes)
	}
	if e, ok := host.Writes[2].Value.(*Element); !ok || e.HTML != "<b>x</b>" {
		t.Errorf("element write %#v", host.Writes[2].Value)
	}
	if len(res.ContextWrites) != 4 || res.ContextWrites[1].Name != "list" {
		t.Errorf("result writes %+v", res.ContextWrites)
	}
	// Without a host, the writes are still reported.
	res = mustCall(t, CallRequest{Slot: SlotMethod, Args: []Value{}, Source: `export default () => { Context.a = 1; return 0; };`})
	if len(res.ContextWrites) != 1 || res.ContextWrites[0].Value != int64(1) {
		t.Errorf("no host: %+v", res.ContextWrites)
	}
	// Slots without Context do not have it.
	if res := mustCall(t, read(`export default () => typeof Context;`, nil)); res.Value != "undefined" {
		t.Errorf("Context in @read: %v", res.Value)
	}
	// Context.request in methods.
	res = mustCall(t, CallRequest{Slot: SlotDispatch, Args: []Value{}, Context: NewDict("request", &Request{Method: "GET", Path: "/x"}),
		Source: `export default () => Context.request.path;`})
	if res.Value != "/x" || res.Tainted {
		t.Errorf("Context.request: %v %v", res.Value, res.Tainted)
	}
}

func TestCryptoAndText(t *testing.T) {
	res := mustCall(t, read(`export default async () => {
		const enc = new TextEncoder().encode("héllo €");
		const dec = new TextDecoder().decode(enc);
		const r = crypto.getRandomValues(new Uint8Array(16));
		const u = crypto.randomUUID();
		const sha1 = new Uint8Array(await crypto.subtle.digest({ name: "SHA-1" }, new TextEncoder().encode("abc")));
		let bad = "ok"; try { await crypto.subtle.digest("MD5", new Uint8Array(1)); } catch (e) { bad = e.name; }
		let big = "ok"; try { crypto.getRandomValues(new Uint8Array(70000)); } catch (e) { big = e.name; }
		return [enc.length, dec, r.length, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(u), sha1[0].toString(16), bad, big,
			typeof crypto.subtle.encrypt, new TextDecoder().decode(new Uint8Array([0xff]))].join("|");
	};`, nil))
	if res.Value != "10|héllo €|16|true|a9|NotSupportedError|QuotaExceededError|undefined|�" {
		t.Errorf("got %q", res.Value)
	}
}
