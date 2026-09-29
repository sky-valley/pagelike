package jsrt

import (
	"strings"
	"testing"

	"github.com/sky-valley/pagelike/internal/dom"
)

// Server DOM behaviour beyond the replayed harness cases (A10).

func TestDefaultDocumentIsWritable(t *testing.T) {
	doc, _ := dom.Parse([]byte(`<!DOCTYPE html><html><body><article itemscope itemtype="https://x/Post"><meta itemprop="title" content="Hello"></article></body></html>`))
	inst := dom.FindElement(doc, "article")
	res := mustCall(t, CallRequest{Slot: SlotDefault, This: NewDict("title", "Hello"), Args: []Value{NewDict("document_html", "")},
		Document: &Document{Node: inst, Source: "/p.html"},
		Source: `export default function () {
			const t = document.querySelector('[itemprop="title"]');
			t.setAttribute("data-seen", "yes");
			document.documentElement.append(document.createElement("hr"));
			return [t.getAttribute("content").toLowerCase(), document.documentElement.tagName, document.body, document.head].join("|");
		}`})
	if res.Value != "hello|ARTICLE||" {
		t.Errorf("value %v", res.Value)
	}
	if !res.DocumentChanged || !strings.Contains(res.Document, `data-seen="yes"`) || !strings.HasPrefix(res.Document, "<article") || !strings.Contains(res.Document, "<hr></article>") {
		t.Errorf("document %q changed=%v", res.Document, res.DocumentChanged)
	}
	// The caller's tree is untouched.
	if strings.Contains(dom.OuterHTML(inst), "data-seen") {
		t.Error("the host tree was modified")
	}
	// Unchanged documents are not returned.
	res = mustCall(t, CallRequest{Slot: SlotDefault, This: NewDict(), Args: []Value{NewDict()}, Document: &Document{Node: inst}, Source: `export default () => document.querySelectorAll("*").length;`})
	if res.DocumentChanged || res.Document != "" || res.Value != int64(2) {
		t.Errorf("unchanged: %+v", res)
	}
}

func TestReadOnlyDocument(t *testing.T) {
	res := mustCall(t, CallRequest{Slot: SlotValidate, Args: []Value{"v"}, Document: &Document{HTML: `<!DOCTYPE html><html><body><p id="p1" class="c">keep</p></body></html>`},
		Source: `export default () => {
			const p = document.querySelector("#p1");
			const tries = [
				() => p.setAttribute("id", "x"), () => { p.textContent = "y"; }, () => p.classList.add("d"), () => p.remove(),
				() => { p.className; p.innerHTML = "<b>z</b>"; }, () => p.after("x"), () => document.body.appendChild(document.createElement("b")),
				() => { p.outerHTML = "<i></i>"; }, () => p.insertAdjacentHTML("beforeend", "<i></i>"), () => p.firstChild.data = "d",
				() => p.removeAttribute("id"), () => p.setAttributeNS(null, "a", "b"), () => document.createElement("i").appendChild(p),
			];
			const names = tries.map((f) => { try { f(); return "ok"; } catch (e) { return e.name + ":" + e.code; } });
			// Construction is always allowed and yields writable nodes.
			const li = document.createElement("li"); li.textContent = "fresh"; li.setAttribute("a", "1");
			const copy = p.cloneNode(true); copy.id; copy.setAttribute("id", "p2");
			const parsed = new DOMParser().parseFromString("<body><p>x</p></body>", "text/html"); parsed.body.append("y");
			return [...new Set(names)].join(",") + "|" + p.outerHTML + "|" + li.outerHTML + "|" + copy.outerHTML + "|" + parsed.body.innerHTML;
		}`})
	want := `NoModificationAllowedError:7|<p id="p1" class="c">keep</p>|<li a="1">fresh</li>|<p id="p2" class="c">keep</p>|<p>x</p>y`
	if res.Value != want {
		t.Errorf("got  %v\nwant %v", res.Value, want)
	}
}

func TestDispatchThisIsHostElement(t *testing.T) {
	doc, _ := dom.Parse([]byte(`<!DOCTYPE html><html xmlns:t="urn:t"><body><main><t:probe data-k="v"><p>a</p></t:probe></main><p>outside</p></body></html>`))
	host := dom.FindElement(doc, "t:probe")
	res := mustCall(t, CallRequest{Slot: SlotDispatch, This: &Element{Node: host}, Args: []Value{}, Document: &Document{Node: host, Source: "/d.html"}, Context: NewDict(),
		Source: `export default function () {
			return [this === document.documentElement, this.tagName, this.localName, this.prefix, this.namespaceURI, this.getAttribute("data-k"),
				document.querySelectorAll("p").length, this.parentNode === document, document.head, this instanceof HTMLElement].join("|");
		}`})
	if res.Value != "true|T:PROBE|probe|t|urn:t|v|1|true||true" {
		t.Errorf("got %v", res.Value)
	}
}

func TestElementValueRefsIntoWholeDocument(t *testing.T) {
	doc, _ := dom.Parse([]byte(`<!DOCTYPE html><ul><li id="a">1</li><li id="b">2</li></ul><p>x</p>`))
	var lis []Value
	dom.Walk(doc, func(n *dom.Node) bool {
		if n.Data == "li" {
			lis = append(lis, &Element{Node: n})
		}
		return true
	})
	res := mustCall(t, CallRequest{Slot: SlotMethod, This: lis[1], Args: []Value{lis}, Document: &Document{Node: doc, Source: "/l.html"},
		Source: `export default function (items) {
			return [this === document.querySelector("#b"), items[0] === document.getElementById("a"), items.length, this.previousElementSibling.id, document.querySelectorAll("[data-pagelike-jsref]").length].join("|");
		}`})
	if res.Value != "true|true|2|a|0" {
		t.Errorf("got %v", res.Value)
	}
}

func TestStandaloneElementValues(t *testing.T) {
	res := mustCall(t, CallRequest{Slot: SlotMethod, Args: []Value{
		&Element{HTML: `<li class="x"><b>1</b></li>`, Source: "/other.html"},
		&Element{HTML: `<tr><td>c</td></tr>`, Writable: true},
	}, Source: `export default function (ro, rw) {
		let e1 = "ok"; try { ro.setAttribute("a", "b"); } catch (e) { e1 = e.name; }
		rw.setAttribute("data-w", "1");
		return [e1, ro.querySelector("b").textContent, ro.querySelectorAll("*").length, rw.tagName, rw.firstElementChild.tagName, ro, rw];
	}`})
	l := res.Value.([]Value)
	if l[0] != "NoModificationAllowedError" || l[1] != "1" || l[2] != int64(2) || l[3] != "TR" || l[4] != "TD" {
		t.Errorf("got %v", l)
	}
	ro, rw := l[5].(*Element), l[6].(*Element)
	if ro.Source != "/other.html" || ro.HTML != `<li class="x"><b>1</b></li>` {
		t.Errorf("ro %+v", ro)
	}
	if rw.Source != "" || rw.HTML != `<tr data-w="1"><td>c</td></tr>` {
		t.Errorf("rw %+v", rw)
	}
}

func TestReturnedElementsCarrySource(t *testing.T) {
	res := mustCall(t, CallRequest{Slot: SlotRead, Args: []Value{nil}, Document: &Document{HTML: `<p id="s">stored</p>`, Source: "/s.html"},
		Source: `export default () => [document.querySelector("#s"), document.createElement("i"), document.querySelectorAll("p")];`})
	l := res.Value.([]Value)
	if e := l[0].(*Element); e.Source != "/s.html" || e.HTML != `<p id="s">stored</p>` {
		t.Errorf("stored %+v", e)
	}
	if e := l[1].(*Element); e.Source != "" || e.HTML != "<i></i>" {
		t.Errorf("constructed %+v", e)
	}
	if nl := l[2].([]Value); len(nl) != 1 {
		t.Errorf("NodeList %v", nl)
	}
	b, _ := MarshalJSON(res.Value)
	if !strings.Contains(string(b), `{"$type":"element","$html":"<p id=\"s\">stored</p>","$source":"/s.html"}`) {
		t.Errorf("sessel+json %s", b)
	}
}

func TestXMLDocument(t *testing.T) {
	rss := `<?xml version="1.0"?><rss xmlns:atom="http://www.w3.org/2005/Atom"><channel><title>Feed</title><atom:link href="/f"/><item><title>One</title></item><item><title>Two</title></item></channel></rss>`
	res := mustCall(t, CallRequest{Slot: SlotRead, Args: []Value{nil}, Document: &Document{HTML: rss, XML: true, Source: "/feed.xml"},
		Source: `export default () => {
			const titles = [...document.querySelectorAll("item > title")].map((t) => t.textContent);
			const link = document.getElementsByTagNameNS("http://www.w3.org/2005/Atom", "link")[0];
			return [titles.join(","), document.documentElement.tagName, link.tagName, link.localName, link.prefix, link.namespaceURI, link.getAttribute("href"), document.querySelector("channel > title").outerHTML, link instanceof HTMLElement, link instanceof Element].join("|");
		}`})
	if res.Value != "One,Two|rss|atom:link|link|atom|http://www.w3.org/2005/Atom|/f|<title>Feed</title>|false|true" {
		t.Errorf("got %v", res.Value)
	}
}

func TestCrossOriginReflection(t *testing.T) {
	// javascript.dom.crossorigin-nullable-enum with its </script> escaped.
	res := mustCall(t, read(`export default () => {
		const d = new DOMParser().parseFromString('<img id="a"><img id="b" crossorigin><img id="c" crossorigin="USE-CREDENTIALS"><img id="e" crossorigin="bogus"><script id="s" crossorigin="anonymous"><\/script>', 'text/html');
		const q = (s) => d.querySelector(s);
		const before = [q('#a').crossOrigin, q('#b').crossOrigin, q('#c').crossOrigin, q('#e').crossOrigin, q('#s').crossOrigin].map(String).join('|');
		q('#e').crossOrigin = null;
		return before + '|' + q('#e').hasAttribute('crossorigin');
	};`, nil))
	if res.Value != "null|anonymous|use-credentials|anonymous|anonymous|false" {
		t.Errorf("got %v", res.Value)
	}
}

func TestMarkupCannotBeInjectedThroughNames(t *testing.T) {
	res := mustCall(t, read(`export default () => {
		const d = new DOMParser().parseFromString("", "text/html");
		const r = (f) => { try { f(); return "ok"; } catch (e) { return e.name; } };
		const el = d.createElement("p");
		return [r(() => d.createElement("div onclick=x")), r(() => d.createElement("a>b")), r(() => el.setAttribute("a b", "1")),
			r(() => el.setAttribute('x"', "1")), r(() => d.createElementNS("urn:x", "a b")), r(() => el.setAttribute("ok", '"><script>'))
		].join("|") + "|" + el.outerHTML;
	};`, nil))
	if res.Value != `InvalidCharacterError|InvalidCharacterError|InvalidCharacterError|InvalidCharacterError|InvalidCharacterError|ok|<p ok="&quot;&gt;&lt;script&gt;"></p>` {
		t.Errorf("got %v", res.Value)
	}
}

func TestInnerHTMLKeepsWhitespace(t *testing.T) {
	res := mustCall(t, read(`export default () => {
		const d = new DOMParser().parseFromString("<table><tbody></tbody></table>", "text/html");
		const p = d.createElement("p");
		p.innerHTML = "  a <b>b</b> c\n";
		const tb = d.querySelector("tbody");
		tb.innerHTML = "<tr><td>1</td></tr>";
		return JSON.stringify(p.textContent) + "|" + p.childNodes.length + "|" + tb.outerHTML;
	};`, nil))
	if res.Value != `"  a b c\n"|3|<tbody><tr><td>1</td></tr></tbody>` {
		t.Errorf("got %v", res.Value)
	}
}

func TestNoDocumentWhereNoneIsGiven(t *testing.T) {
	res := mustCall(t, read(`export default () => typeof document;`, nil))
	if res.Value != "undefined" {
		t.Errorf("got %v", res.Value)
	}
	if _, f := call(t, CallRequest{Slot: SlotHTTPRequestProperty, Args: []Value{nil}, Source: `export default () => 1;`, Document: &Document{HTML: "<p>"}}); f == nil || f.Variant != VariantInternal {
		t.Errorf("an HttpRequest property takes no document: %v", f)
	}
}

func TestDOMClassesAreLazyButComplete(t *testing.T) {
	// Touching one interface builds the DOM; every documented global exists.
	res := mustCall(t, read(`export default () => {
		const names = ["Node", "Document", "DocumentFragment", "CharacterData", "Text", "Comment", "Element", "HTMLElement", "NodeList", "DOMTokenList",
			"DOMParser", "XMLSerializer", "DOMException", "HTMLMediaElement", "HTMLAnchorElement", "HTMLVideoElement", "HTMLTableCellElement", "HTMLDirectoryElement"];
		const missing = names.filter((n) => typeof globalThis[n] !== "function");
		const own = Object.getOwnPropertyNames(globalThis).filter((n) => /^HTML/.test(n)).length;
		return missing.join(",") + "|" + (own >= 70) + "|" + (Object.getPrototypeOf(HTMLVideoElement) === HTMLMediaElement) + "|" + HTMLAudioElement.name;
	};`, nil))
	if res.Value != "|true|true|HTMLAudioElement" {
		t.Errorf("got %v", res.Value)
	}
	// Illegal constructors.
	res = mustCall(t, read(`export default () => [Node, Element, HTMLAnchorElement, NodeList, Text].map((C) => { try { new C(); return "ok"; } catch (e) { return e.name; } }).join(",");`, nil))
	if res.Value != "TypeError,TypeError,TypeError,TypeError,TypeError" {
		t.Errorf("got %v", res.Value)
	}
}

func TestSelectorsUsePagelikeEngine(t *testing.T) {
	res := mustCall(t, read(`export default () => {
		const d = new DOMParser().parseFromString('<ul><li>apple</li><li>Banana</li><li data-n="5">x</li></ul>', "text/html");
		return [d.querySelectorAll("li:contains('an')").length, d.querySelector("li:greater-than(3)") === null, d.querySelectorAll(":scope li").length, d.querySelectorAll("li:nth-child(2 of li)").length].join("|");
	};`, nil))
	if res.Value != "1|true|3|1" {
		t.Errorf("got %v", res.Value)
	}
}
