package liquid

import (
	"strings"
	"testing"

	"github.com/osteele/liquid/expressions"
)

func TestPreprocess(t *testing.T) {
	for _, c := range []struct {
		in, want string
		host     Host
	}{
		{in: `<p>{{ x }}</p>`, want: `<p>{% pl_out x %}</p>`},
		{in: `<a href="{{ x }}">{{- y -}}</a>`, want: `<a href="{% pl_out_attr x %}">{%- pl_out y -%}</a>`},
		{in: `<script>{{ x }}</script><b>{{ y }}</b>`, want: `<script>{% pl_out_attr x %}</script><b>{% pl_out y %}</b>`},
		{in: `{{ x }}`, host: Host{Tag: "TITLE"}, want: `{% pl_out_attr x %}`},
		{in: `<title>{{ x }}</title>`, host: Host{XML: true}, want: `<title>{% pl_out x %}</title>`},
		{in: `<![CDATA[{{ x }}]]>{{ y }}`, host: Host{XML: true}, want: `<![CDATA[{% pl_out_attr x %}]]>{% pl_out y %}`},
		{in: `<?pi {{ x }}?>{{ y }}`, host: Host{XML: true}, want: `<?pi {% pl_out_attr x %}?>{% pl_out y %}`},
		{in: `{{ 3 | random: upper: 3, lower: 3 }}`, want: `{% pl_out 3 | random: nil, upper: 3, lower: 3 %}`},
		{in: `{{ "a | b: c: d" | f: 1, k: 2 }}`, want: `{% pl_out "a | b: c: d" | f: 1, k: 2 %}`},
		{in: `{% if x != blank %}`, want: `{% if (x | pl_ne: blank) %}`},
		{in: `{% if "x != blank" == y %}`, want: `{% if ("x != blank" | pl_eq: y) %}`},
		{in: `{%- for p in ps limit: 4 -%}`, want: `{%- for p in ps limit: 4 -%}`},
		{in: `{% for i in (1..n.size) %}`, want: `{% for i in (1..((n | size) | pl_int)) %}`},
		{in: `{% raw %}{{ x }}{% endraw %}`, want: `{% raw %}{{ x }}{% endraw %}`},
		{in: `{% comment %}{{ x | }}<p>{% endcomment %}<a href="{{ y }}">`, want: `{% comment %}{{ x | }}<p>{% endcomment %}<a href="{% pl_out_attr y %}">`},
		{in: "{{ a\n}}\nb", want: "{% pl_out a\n %}\nb"},
		{in: `{% echo x %}`, want: `{% pl_out x %}`},
		{in: `{% # hi %}`, want: ``},
		{in: "{%- # hi\n -%}", want: "{%- comment\n %}{% endcomment -%}"},
		{in: "{% liquid\nassign a = 1\necho a\n%}", want: "{% assign a = 1 %}{% pl_out a %}{% comment\n\n\n %}{% endcomment %}"},
		{in: `{% endfor -%}`, want: `{% endfor -%}`},
		{in: `{{ "%}" }}`, want: `{% pl_out #22257d22 %}`},
		// Character references inside Liquid markup are decoded for a
		// named HTML host where its parser decodes them (R-LIQ-13, live
		// 2026-09-29): text, RCDATA, attribute values; not raw text,
		// comments, raw blocks, XML, or Host{}.
		{in: `x &lt;i&gt; {{ "&amp;" | size }}{% if n &gt; 0 %}`, host: Host{Tag: "p"}, want: `x &lt;i&gt; {% pl_out "&" | size %}{% if (n | pl_gt: 0) %}`},
		{in: `<a title="{{ 'a&amp;b' }}">`, host: Host{Tag: "p"}, want: `<a title="{% pl_out_attr 'a&b' %}">`},
		{in: `{{ "&lt;" }}`, host: Host{Tag: "textarea"}, want: `{% pl_out_attr "<" %}`},
		{in: `<script>{{ "&lt;" }}</script><!-- {{ "&lt;" }} -->{% raw %}{{ "&lt;" }}{% endraw %}`, host: Host{Tag: "p"}, want: `<script>{% pl_out_attr "&lt;" %}</script><!-- {% pl_out_attr "&lt;" %} -->{% raw %}{{ "&lt;" }}{% endraw %}`},
		{in: `{{ "&amp;" }}`, host: Host{Tag: "p", XML: true}, want: `{% pl_out "&amp;" %}`},
		{in: `{{ "&amp;" }}`, want: `{% pl_out "&amp;" %}`},
	} {
		got, err := preprocess(c.in, c.host)
		if err != nil || got != c.want {
			t.Errorf("preprocess(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestRewriteCond(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`a == b`, `(a | pl_eq: b)`},
		{`a.b[0] <> "x"`, `(a.b[0] | pl_ne: "x")`},
		{`u.age >= 18`, `(u.age | pl_ge: 18)`},
		{`18 < u.age and x`, `(18 | pl_lt: u.age) and x`},
		{`a or b and c`, `a or (b and (c))`},
		{`a and b and c`, `a and b and c`},
		{`a or b and c or d`, `a or (b and (c or (d)))`},
		{`xs contains "a"`, `(xs | pl_contains: "a")`},
		{`a contains b and c`, `(a | pl_contains: b) and c`},
		{`xs | has: "k", true`, `xs | has: "k", true`},
		{`xs | where_exp: "x", "x.a > 1"`, `xs | where_exp: "x", ("x.a > 1" | pl_pred)`},
		{`xs | where_exp: "x", pred | size`, `xs | where_exp: "x", (pred | pl_pred) | size`},
		{`n | random: upper: 3`, `n | random: nil, upper: 3`},
		{`x | default: y, allow_false: true`, `x | default: y, allow_false: true`},
		{`s.size`, `(s | size)`},
		{`s.size.x`, `(s | size).x`},
		{`h["size"]`, `h["size"]`},
		{`(a > 1)`, `((a | pl_gt: 1))`},
		{`(1..5)`, `(1..5)`},
		{`(-2..n)`, `(-2..(n | pl_int))`},
		{`x | f: (a == b)`, `x | f: ((a | pl_eq: b))`},
		{`a[b == c]`, `a[((b | pl_eq: c) | pl_index)]`},
		{`a[0]["k"][i]`, `a[0]["k"][(i | pl_index)]`},
		{`"it's" == 'a "b"'`, `("it's" | pl_eq: 'a "b"')`},
		{`"a\"b" == x`, `("a\"b" | pl_eq: x)`},
		{`x-1 == y`, `(x-1 | pl_eq: y)`},
		{`a | upcase == "A"`, `a | upcase == "A"`}, // not valid Liquid: left alone
		{`x ==`, `x ==`},
		{`"unterminated`, `"unterminated`},
		{``, ``},
		{`café == 1`, `(café | pl_eq: 1)`},
		{`empty? == true`, `(empty? | pl_eq: true)`},
	} {
		if got := rewriteCond(c.in); got != c.want {
			t.Errorf("rewriteCond(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRewriteStatements(t *testing.T) {
	for _, c := range []struct {
		fn       func(string) string
		in, want string
	}{
		{rewriteAssign, `x = a == 1`, `x = (a | pl_eq: 1)`},
		{rewriteAssign, `x = xs | where_exp: "i", "i.n > 2"`, `x = xs | where_exp: "i", ("i.n > 2" | pl_pred)`},
		{rewriteAssign, `x.y = 1`, `x.y = 1`},
		{rewriteLoop, `i in (1..n) reversed limit: m.size`, `i in (1..(n | pl_int)) reversed limit: (m | size)`},
		{rewriteLoop, `p in ps | sort: "a" offset: 1`, `p in ps | sort: "a" offset: 1`},
		{rewriteWhen, `a.size, "b" or c`, `(a | size), "b" or c`},
	} {
		if got := c.fn(c.in); got != c.want {
			t.Errorf("rewrite(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Rewriting never turns an expression the library accepts into one it
// rejects.
func TestRewritePreservesValidity(t *testing.T) {
	exprs := []string{
		`a`, `a.b.c`, `a["b"][0]`, `a == b`, `a != b and c > d or e <= f`, `(a..b)`, `(1..3) | join`,
		`x | f`, `x | f: 1`, `x | f: 1, k: 2`, `a contains b`,
		`(a == b) or (c and d)`, `x | where_exp: "i", "i.a == 1 and i.b"`, `"a" == 'b'`, `nil == blank`,
		`x | f: (a | g: 1)`, `a[b.size]`, `true`, `-1 < 1`, `1.5 >= 1`, `x | where_exp: "i", "i | has_exp: 'j', 'j > 1'"`,
	}
	for _, src := range exprs {
		if _, err := expressions.Parse(src); err != nil {
			t.Fatalf("test expression %q does not parse: %v", src, err)
		}
		out := rewriteCond(src)
		if _, err := expressions.Parse(out); err != nil {
			t.Errorf("rewriteCond(%q) = %q does not parse: %v", src, out, err)
		}
	}
}

func TestLineNumbersSurvivePreprocessing(t *testing.T) {
	src := "a\n{{ x\n| upcase }}\n{% if a ==\n b %}\n{% liquid\n assign c = 1\n echo c\n%}\n{% endif %}\nend"
	out, err := preprocess(src, Host{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Count(out, "\n"), strings.Count(src, "\n"); got != want {
		t.Errorf("preprocessing changed the line count: %d -> %d\n%s", want, got, out)
	}
	if !strings.HasSuffix(out, "\nend") {
		t.Errorf("text after the tags moved: %q", out)
	}
}
