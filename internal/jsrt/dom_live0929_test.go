package jsrt

import "testing"

// Behaviour adopted from live PageLove on 2026-09-29
// (docs/compat/decisions-2026-09-29/javascript.md).

func TestDOMParserLeavesOutImpliedElements(t *testing.T) {
	res := mustCall(t, read(`export default () => {
		const P = new DOMParser();
		const K = (n) => (n === null ? "-" : n.nodeName);
		const G = (m) => { const d = P.parseFromString(m, "text/html"); return new XMLSerializer().serializeToString(d) + "/" + K(d.documentElement) + "/" + K(d.head) + "/" + K(d.body) + "/" + d.childNodes.length; };
		const d = P.parseFromString('<ul id="l"></ul><p>x</p>', "application/xml");
		const l = d.querySelector("#l");
		return [G('<ul id="l"></ul><p>x</p>'), G("<!DOCTYPE html><html><head></head><body><p>x</p></body></html>"), G("<title>t</title><p>x</p>"),
			G("<body><p>x</p></body>"), G("<html><p>x</p></html>"), G(""), G("t <b>b</b>"), l.parentNode.nodeType + "," + l.parentElement + "," + (l.parentNode === d)].join(" ## ");
	};`, nil))
	want := `<ul id="l"></ul><p>x</p>/UL/-/-/2 ## <!DOCTYPE html><html><head></head><body><p>x</p></body></html>/HTML/HEAD/BODY/2 ## <title>t</title><p>x</p>/TITLE/-/-/2 ## ` +
		`<body><p>x</p></body>/BODY/-/BODY/1 ## <html><p>x</p></html>/HTML/-/-/1 ## /-/-/-/0 ## t <b>b</b>/B/-/-/2 ## 9,null,true`
	if res.Value != want {
		t.Errorf("got  %v\nwant %v", res.Value, want)
	}
}

func TestDocumentChildrenAreUnrestricted(t *testing.T) {
	res := mustCall(t, read(`export default () => {
		const P = new DOMParser();
		const X = (n) => new XMLSerializer().serializeToString(n);
		const mk = () => P.parseFromString('<p id="a">a</p><p id="b">b</p>', "text/html");
		const r = [];
		let d = mk(); d.appendChild(d.createElement("i")); d.appendChild(d.createTextNode("t")); r.push(X(d));
		d = mk(); d.querySelector("#a").before("s"); d.querySelector("#b").after(d.createElement("u")); r.push(X(d));
		d = mk(); d.querySelector("#b").insertAdjacentHTML("afterend", "<i>i</i>"); d.querySelector("#a").insertAdjacentHTML("beforebegin", "<u>u</u>"); r.push(X(d));
		d = mk(); d.querySelector("#a").outerHTML = "<b>1</b><b>2</b>"; r.push(X(d) + "/" + d.documentElement.tagName);
		return r.join(" ## ");
	};`, nil))
	want := `<p id="a">a</p><p id="b">b</p><i></i>t ## s<p id="a">a</p><p id="b">b</p><u></u> ## <u>u</u><p id="a">a</p><p id="b">b</p><i>i</i> ## <b>1</b><b>2</b><p id="b">b</p>/B`
	if res.Value != want {
		t.Errorf("got  %v\nwant %v", res.Value, want)
	}
	// The read-only ambient document still refuses the same operations.
	res = mustCall(t, CallRequest{Slot: SlotValidate, Args: []Value{"v"}, Document: &Document{HTML: `<p id="p1">keep</p>`},
		Source: `export default () => { try { document.querySelector("#p1").outerHTML = "<i></i>"; return "ok"; } catch (e) { return e.name; } }`})
	if res.Value != "NoModificationAllowedError" {
		t.Errorf("read-only outerHTML: %v", res.Value)
	}
}

func TestClassListKeepsDuplicates(t *testing.T) {
	res := mustCall(t, read(`export default () => {
		const d = new DOMParser().parseFromString('<p id="a" class="a b a"></p><p id="b" class="a b a"></p><p id="c" class="a b a"></p><p id="d" class="a b d"></p><p id="e" class="q"></p>', "text/html");
		const g = (id) => d.getElementById(id);
		const a = g("a").classList, out = [a.length, [...a].join(","), a.item(2), a.item(3), a.item(-1)];
		a.add("c", "a"); out.push(g("a").className);
		g("b").classList.remove("a"); out.push(g("b").className);
		out.push(g("c").classList.replace("a", "b"), g("c").className);
		const dl = g("d").classList; out.push(dl.replace("b", "b"), g("d").className, dl.replace("a", "d"), g("d").className);
		const e = g("e").classList; e.add(""); e.add("x y"); out.push(JSON.stringify(g("e").getAttribute("class")), e.length);
		e.remove("q", "x", "y"); out.push(g("e").hasAttribute("class"));
		let te = "ok"; try { a.item("0"); } catch (x) { te = x.name; }
		out.push(te, e.toggle("z", 1), String(a));
		return out.map(String).join("|");
	};`, nil))
	want := `3|a,b,a|a|undefined|undefined|a b a c|b|true|b a|true|a b d|true|d b|"q x y"|3|false|TypeError|true|a b a c`
	if res.Value != want {
		t.Errorf("got  %v\nwant %v", res.Value, want)
	}
}

func TestNamespacePipeSelectors(t *testing.T) {
	res := mustCall(t, read(`export default () => {
		const d = new DOMParser().parseFromString('<div id="r"><p>x</p><div><p>y</p></div><svg><g></g></svg></div>', "text/html");
		d.querySelector("#r").appendChild(d.createElementNS("http://www.w3.org/2000/svg", "circle"));
		const n = (s) => { try { return d.querySelectorAll(s).length; } catch (e) { return e.name; } };
		return [n("*|p"), n("*|*"), n("*"), n("|p"), n("|*"), n("|circle"), n("*|circle"), n("[*|id]"), n("[|id]"), n("svg|g"), n("p[id|=x]"),
			d.querySelector("p").matches("*|p"), d.querySelector("p").closest("*|div").id, n(".a\\|b"), n("[title='*|x']")].join("|");
	};`, nil))
	if res.Value != "2|7|7|2|6|0|1|1|1|SyntaxError|0|true|r|0|0" {
		t.Errorf("got %v", res.Value)
	}
}

func TestRewriteNamespacePipes(t *testing.T) {
	for in, want := range map[string]string{
		"*|p":           "p",
		"*|*":           "*",
		"|p":            "p:" + noNamespacePseudo,
		"div > |p.x":    "div > p:" + noNamespacePseudo + ".x",
		"[*|id]":        "[id]",
		"[|id]":         "[id]",
		"[lang|=en]":    "[lang|=en]",
		"svg|g":         "svg|g",
		`[title="*|x"]`: `[title="*|x"]`,
		`.a\|b`:         `.a\|b`,
		"a, |b":         "a, b:" + noNamespacePseudo,
	} {
		if got := rewriteNamespacePipes(in); got != want {
			t.Errorf("rewriteNamespacePipes(%q) = %q, want %q", in, got, want)
		}
	}
}
