package htmlser_test

import (
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/htmlser"
)

// goldenCases exercise every serialization rule decision 0003 §2 lists. The
// expected output is html5ever 0.39 + markup5ever_rcdom (scraper for
// <template>, which rcdom drops), recorded with the oracle in
// the html-selectors spike (docs/decisions/0003).
var goldenCases = []struct{ name, src, html5ever string }{
	{"doctype-and-ws",
		"<!DOCTYPE html>\n<html>\n<head><title>T</title></head>\n<body>\n<p>x</p>\n</body>\n</html>\n",
		"<!DOCTYPE html><html><head><title>T</title></head>\n<body>\n<p>x</p>\n\n\n</body></html>"},
	{"implied-head",
		"<!DOCTYPE html><html><body><h1>Title</h1></body></html>",
		"<!DOCTYPE html><html><head></head><body><h1>Title</h1></body></html>"},
	{"legacy-doctype",
		"<!doctype HTML PUBLIC \"-//W3C//DTD HTML 4.01//EN\" \"http://www.w3.org/TR/html4/strict.dtd\"><p>x",
		"<!DOCTYPE html><html><head></head><body><p>x</p></body></html>"},
	{"escaping",
		"<p title='a \"q\" & <b>   it&#39;s'>Tom &amp; Jerry's <b>&lt;tag&gt;</b> \"quoted\"  nbsp &copy; &nbsp;</p>",
		"<html><head></head><body><p title=\"a &quot;q&quot; &amp; &lt;b&gt;   it's\">Tom &amp; Jerry's <b>&lt;tag&gt;</b> \"quoted\"  nbsp © &nbsp;</p></body></html>"},
	{"void-and-boolean",
		"<input disabled hidden=\"\" checked=checked><br><img src=x alt=\"\"><meta charset=utf-8><hr/>",
		"<html><head></head><body><input disabled=\"\" hidden=\"\" checked=\"checked\"><br><img src=\"x\" alt=\"\"><meta charset=\"utf-8\"><hr></body></html>"},
	{"microdata",
		"<div itemscope itemtype='https://schema.org/Thing'><meta itemprop=name content=X><link itemprop=url href=\"/a?b=1&c=2\"></div>",
		"<html><head></head><body><div itemscope=\"\" itemtype=\"https://schema.org/Thing\"><meta itemprop=\"name\" content=\"X\"><link itemprop=\"url\" href=\"/a?b=1&amp;c=2\"></div></body></html>"},
	{"raw-text",
		"<script>if (a < b && c > d) { x = \"</p>\" }</script><style>a > b { content: \"&amp;\" }</style><xmp><b>&</b></xmp>",
		"<html><head><script>if (a < b && c > d) { x = \"</p>\" }</script><style>a > b { content: \"&amp;\" }</style></head><body><xmp><b>&</b></xmp></body></html>"},
	{"pre-newline",
		"<textarea>\nfirst</textarea><pre>\n\nsecond</pre><listing>\nL</listing><pre>x</pre>",
		"<html><head></head><body><textarea>first</textarea><pre>\nsecond</pre><listing>L</listing><pre>x</pre></body></html>"},
	{"comment",
		"<!-- a & b < c > d --><p><!--in-->x</p>",
		"<!-- a & b < c > d --><html><head></head><body><p><!--in-->x</p></body></html>"},
	{"svg",
		"<svg viewBox=\"0 0 10 10\" xmlns:xlink=\"http://www.w3.org/1999/xlink\"><use xlink:href=\"#a\"/><linearGradient gradientUnits=\"x\"/><path d=\"M0 0\"/><foreignObject><p>h</p></foreignObject></svg>",
		"<html><head></head><body><svg viewBox=\"0 0 10 10\" xmlns:xlink=\"http://www.w3.org/1999/xlink\"><use xlink:href=\"#a\"></use><linearGradient gradientUnits=\"x\"></linearGradient><path d=\"M0 0\"></path><foreignObject><p>h</p></foreignObject></svg></body></html>"},
	{"mathml",
		"<math><mi>x</mi><annotation-xml encoding=\"text/html\"><p>h</p></annotation-xml></math>",
		"<html><head></head><body><math><mi>x</mi><annotation-xml encoding=\"text/html\"><p>h</p></annotation-xml></math></body></html>"},
	{"pagelove-namespaces",
		"<html xmlns:p=\"https://pagelove.org/1.0\" xmlns:r=\"https://pagelove.org/Binding/CSS\"><body><div r:posts=\"li\" p:template=\"text/liquid\" pagelove:template=\"x\" E:Mixed=\"1\"><p:stamp greeting></p:stamp><p:include selector=\"#nav\"></p:include></div></body></html>",
		"<html xmlns:p=\"https://pagelove.org/1.0\" xmlns:r=\"https://pagelove.org/Binding/CSS\"><head></head><body><div r:posts=\"li\" p:template=\"text/liquid\" pagelove:template=\"x\" e:mixed=\"1\"><p:stamp greeting=\"\"></p:stamp><p:include selector=\"#nav\"></p:include></div></body></html>"},
	{"template",
		"<template><li>in template</li><tr><td>row</td></tr></template><table><template><tr><td>t</td></tr></template></table>",
		"<html><head><template><li>in template</li>row</template></head><body><table><template><tr><td>t</td></tr></template></table></body></html>"},
	{"noscript",
		"<noscript><p>ns & \"q\"</p></noscript>",
		"<html><head><noscript><p>ns & \"q\"</p></noscript></head><body></body></html>"},
	{"liquid-in-table",
		"<table><tbody>{% for o in opts %}<tr><td>{{ o }}</td></tr>{% endfor %}</tbody></table>",
		"<html><head></head><body>{% for o in opts %}{% endfor %}<table><tbody><tr><td>{{ o }}</td></tr></tbody></table></body></html>"},
	{"liquid-quotes",
		"<div p:template=\"text/liquid\">{% assign x = a | where: \"listed\", \"true\" %}{{ x }} it's</div>",
		"<html><head></head><body><div p:template=\"text/liquid\">{% assign x = a | where: \"listed\", \"true\" %}{{ x }} it's</div></body></html>"},
	{"text-only",
		"text only",
		"<html><head></head><body>text only</body></html>"},
	{"empty",
		"",
		"<html><head></head><body></body></html>"},
	{"trailing-comment",
		"<html><body></body></html><!-- after -->",
		"<html><head></head><body></body></html><!-- after -->"},
	{"crlf",
		"<p>line1\r\nline2\rline3</p>",
		"<html><head></head><body><p>line1\nline2\nline3</p></body></html>"},
	{"select-content",
		"<select><option>a</option><div>not allowed?</div><optgroup><option>b</option></optgroup></select>",
		"<html><head></head><body><select><option>a</option><div>not allowed?</div><optgroup><option>b</option></optgroup></select></body></html>"},
	{"entities-in-attr",
		"<a href=\"?paginate:page=1&amp;paginate:length=3\" title=\"&lt;x&gt; &quot;y&quot;\">x</a>",
		"<html><head></head><body><a href=\"?paginate:page=1&amp;paginate:length=3\" title=\"&lt;x&gt; &quot;y&quot;\">x</a></body></html>"},
	{"uppercase",
		"<DIV ID=Main CLASS=\"A b\"><SPAN>x</SPAN></DIV>",
		"<html><head></head><body><div id=\"Main\" class=\"A b\"><span>x</span></div></body></html>"},
	{"comment-entities",
		"<p><!-- &amp; &lt; --></p>",
		"<html><head></head><body><p><!-- &amp; &lt; --></p></body></html>"},
	{"formatting-attr-order",
		"<p><a itemprop=\"url\" href=\"/x\" class=\"c\">x</a><b title=\"t\" id=\"i\">y</b></p>",
		"<html><head></head><body><p><a itemprop=\"url\" href=\"/x\" class=\"c\">x</a><b title=\"t\" id=\"i\">y</b></p></body></html>"},
	{"nested-forms-and-p",
		"<p>a<div>b</div><form><form><input></form>",
		"<html><head></head><body><p>a</p><div>b</div><form><input></form></body></html>"},
}

// parseHTML5 parses with the HTML5 tree-construction algorithm (x/net/html
// plus the fidelity fixups), which the html5ever-compatible options are
// defined against. pagelike itself parses with PageLove's model
// (dom.BuildTree) and serializes with Options.PageLove.
func parseHTML5(src string) (*html.Node, error) {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return nil, err
	}
	dom.FixComments(doc, src)
	dom.FixAttrOrder(doc, src)
	return doc, nil
}

// TestMatchesHTML5ever: htmlser over an x/net/html tree (with the dom
// fixups) is byte-identical to html5ever on the same input.
func TestMatchesHTML5ever(t *testing.T) {
	for _, c := range goldenCases {
		doc, err := parseHTML5(c.src)
		if err != nil {
			t.Fatal(err)
		}
		if got := htmlser.String(doc, htmlser.Options{}); got != c.html5ever {
			t.Errorf("%s:\n src       %q\n htmlser   %q\n html5ever %q", c.name, c.src, got, c.html5ever)
		}
	}
}

// TestFixedPoint: parse -> serialize -> parse -> serialize is stable after
// the first pass, for the response and the storage variants.
func TestFixedPoint(t *testing.T) {
	variants := map[string]htmlser.Options{
		"default":                {},
		"PreserveLeadingNewline": {PreserveLeadingNewline: true},
	}
	for _, c := range goldenCases {
		for name, o := range variants {
			ser := func(src string) string {
				d, err := parseHTML5(src)
				if err != nil {
					t.Fatal(err)
				}
				return htmlser.String(d, o)
			}
			r1 := ser(c.src)
			r2 := ser(r1)
			r3 := ser(r2)
			if r2 != r3 {
				t.Errorf("%s/%s: not a fixed point after 2 passes:\n r2 %q\n r3 %q", c.name, name, r2, r3)
			}
			// Only the leading-LF rule may change output on the second pass,
			// and the storage variant exists to prevent exactly that.
			if name == "PreserveLeadingNewline" && r1 != r2 {
				t.Errorf("%s/%s: storage variant changed on 2nd pass:\n r1 %q\n r2 %q", c.name, name, r1, r2)
			}
		}
	}
}

func TestOptions(t *testing.T) {
	doc, err := parseHTML5("<p title='<x>'>a</p><noscript><b>&amp;</b></noscript><pre>\n\nx</pre>")
	if err != nil {
		t.Fatal(err)
	}
	body := dom.Body(doc)
	cases := []struct {
		name string
		o    htmlser.Options
		want string
	}{
		{"default", htmlser.Options{}, `<p title="&lt;x&gt;">a</p><noscript><b>&amp;</b></noscript><pre>` + "\nx</pre>"},
		{"legacy attr escaping", htmlser.Options{LegacyAttrEscaping: true}, `<p title="<x>">a</p><noscript><b>&amp;</b></noscript><pre>` + "\nx</pre>"},
		{"scripting disabled", htmlser.Options{ScriptingDisabled: true}, `<p title="&lt;x&gt;">a</p><noscript>&lt;b&gt;&amp;amp;&lt;/b&gt;</noscript><pre>` + "\nx</pre>"},
		{"storage", htmlser.Options{PreserveLeadingNewline: true}, `<p title="&lt;x&gt;">a</p><noscript><b>&amp;</b></noscript><pre>` + "\n\nx</pre>"},
	}
	for _, c := range cases {
		if got := htmlser.InnerHTML(body, c.o); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

// TestDiffersFromRender pins why html.Render is not used: it differs from
// html5ever on the golden cases that exercise void elements, quotes,
// comments, DOCTYPE ids and attribute order.
func TestDiffersFromRender(t *testing.T) {
	differ := 0
	for _, c := range goldenCases {
		d, err := html.Parse(strings.NewReader(c.src))
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		html.Render(&b, d)
		if b.String() != c.html5ever {
			differ++
		}
	}
	if differ == 0 {
		t.Error("html.Render now matches html5ever on every case; revisit decision 0003 §2")
	}
}
