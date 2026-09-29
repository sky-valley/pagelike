package dom_test

// Code generated from harness/observations/live-2026-09-29-serialize (the
// echo of each WebDAV PUT on live PageLove); do not edit by hand.

var liveStoredForms = []struct{ name, src, stored string }{
	{"fragment-only", "  \n<p>hi</p>\n", "  \n<p>hi</p>\n"},
	{"doctype-lower-no-html", "<!doctype html><title>t</title><p>a", "<!DOCTYPE html><title>t</title><p>a</p>"},
	{"full-doc-whitespace", "<!DOCTYPE html>\n<html lang=en>\n<head>\n<meta charset=utf-8>\n</head>\n<body>\n<p>x</p>\n</body>\n</html>\n", "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n</head>\n<body>\n<p>x</p>\n</body>\n</html>\n"},
	{"explicit-empty-head", "<html><head></head><body><p>x</p></body></html>", "<html><head></head><body><p>x</p></body></html>"},
	{"attributes", "<div><input disabled><input disabled=\"\"><input value=\"a&quot;b\" title='<&>' data-q=\"it's\"><span class=c id=d CLASS=e DATA-X=1 onClick=\"f()\">s</span><a href=\"?a=1&b=2\">l</a></div>", "<div><input disabled><input disabled><input value=\"a&quot;b\" title=\"<&amp;>\" data-q=\"it's\"><span class=\"c\" id=\"d\" data-x=\"1\" onclick=\"f()\">s</span><a href=\"?a=1&amp;b=2\">l</a></div>"},
	{"text-escaping", "<p>a &amp; b &lt; c > d&nbsp;e \" ' &copy; é</p><p>{% if a > b and c < d %}x{% endif %}</p>", "<p>a &amp; b &lt; c &gt; d e \" ' © é</p><p>{% if a &gt; b and c &lt; d %}x{% endif %}</p>"},
	{"tables", "<table><tr><td>x</td></tr></table><table>{% for r in rows %}<tr><td>{{r}}</td></tr>{% endfor %}</table>", "<table><tr><td>x</td></tr></table><table>{% for r in rows %}<tr><td>{{r}}</td></tr>{% endfor %}</table>"},
	{"leading-newlines", "<pre>\n\nx</pre><textarea>\nq</textarea><listing>\nl</listing>", "<pre>\n\nx</pre><textarea>\nq</textarea><listing>\nl</listing>"},
	{"void-and-foreign", "<br/><img src=a /><hr></hr><svg viewbox=\"0 0 1 1\"><circle r=\"1\"/><foreignObject><p>f</p></foreignObject></svg><math><mi>x</mi></math>", "<br><img src=\"a\"><hr><svg viewbox=\"0 0 1 1\"><circle r=\"1\"></circle><foreignobject><p>f</p></foreignobject></svg><math><mi>x</mi></math>"},
	{"raw-text", "<script>if (a<b && c>d) { x = \"</p>\" }</script><style>a>b{c:d}</style><template><p>t</template><noscript><p>n</p></noscript><!--c--><?pi x?>", "<script>if (a<b && c>d) { x = \"</p>\" }</script><style>a>b{c:d}</style><template><p>t</p></template><noscript><p>n</p></noscript><!--c-->&lt;?pi x?&gt;"},
	{"pagelove-attributes", "<!DOCTYPE html>\n<html xmlns:p=\"https://pagelove.org/1.0\" xmlns:r=\"https://pagelove.org/Binding/CSS\"><body><ul r:items=\"[itemtype='https://ex/T']\" p:template=\"text/liquid\">{% for i in items %}<li>{{ i.n }}</li>{% endfor %}</ul><div itemscope itemtype=\"https://ex/T\"><span itemprop=n>v</span><meta itemprop=m content=1></div></body></html>", "<!DOCTYPE html>\n<html xmlns:p=\"https://pagelove.org/1.0\" xmlns:r=\"https://pagelove.org/Binding/CSS\"><body><ul r:items=\"[itemtype='https://ex/T']\" p:template=\"text/liquid\">{% for i in items %}<li>{{ i.n }}</li>{% endfor %}</ul><div itemscope itemtype=\"https://ex/T\"><span itemprop=\"n\">v</span><meta itemprop=\"m\" content=\"1\"></div></body></html>"},
	{"after-body-content", "<html><body><p>a</p></body></html>\n<!-- tail -->\n", "<html><body><p>a</p></body></html>\n<!-- tail -->\n"},
	{"put frag", "  \n<p>hi</p>\n", "  \n<p>hi</p>\n"},
	{"put tbl", "<table><tr><td>x</td></tr></table>", "<table><tr><td>x</td></tr></table>"},
	{"put pdiv", "<p>a<div>b</div>", "<p>a<div>b</div></p>"},
	{"put lis", "<ul><li>a<li>b</ul>", "<ul><li>a<li>b</li></li></ul>"},
	{"put misnest", "<b><i>x</b>y</i><p>z</p></div><select><option>a<option>b</select>", "<b><i>x</i></b>y<p>z</p><select><option>a<option>b</option></option></select>"},
	{"put selfclose", "<div/>x<p:include src=\"a.html\"/>y<span/>z", "<div></div>x<p:include src=\"a.html\"></p:include>y<span></span>z"},
	{"put stray-end", "<p>a</p></p></br><div>b</DIV>", "<p>a</p><div>b</div>"},
	{"put attr-escapes", "<p title=\"a&nbsp;b<c>d&amp;e\" data-u=&amp;x data-c='&copy'>t &copy &notit; a < b</p>", "<p title=\"a b<c>d&amp;e\" data-u=\"&amp;amp;x\" data-c=\"&amp;copy\">t &amp;copy &amp;notit; a &lt; b</p>"},
	{"put noscript", "<noscript><p>n</p></noscript><textarea><b>x</b> &amp;</textarea><title>a &amp; <b></title><xmp><i>r</i></xmp>", "<noscript><p>n</p></noscript><textarea>&lt;b&gt;x&lt;/b&gt; &amp;</textarea><title>a &amp; &lt;b&gt;</title><xmp><i>r</i></xmp>"},
	{"put doctype-ids", "<!DOCTYPE html PUBLIC \"-//W3C//DTD XHTML 1.0 Strict//EN\" \"http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd\"><p>x</p>", "<!DOCTYPE html><p>x</p>"},
	{"put cdata-bogus", "<svg><![CDATA[a<b]]></svg><!foo><! -- x --></ x>", "<svg><!--[CDATA[a<b]]--></svg><!--foo--><!-- -- x ---->&lt;/ x&gt;"},
	{"put two-roots", "<p>a</p><p>b</p>", "<p>a</p><p>b</p>"},
}
