package liquid

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

// Behaviours from docs/spec/liquid.md that the corpus does not pin, one
// row per case.

type specCase struct {
	name string
	tpl  string
	vars M
	want string
}

func runSpecCases(t *testing.T, cases []specCase) {
	t.Helper()
	e := newTestEngine()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := e.Render(context.Background(), c.tpl, c.vars, nil)
			if err != nil {
				t.Fatalf("render %q: %v", c.tpl, err)
			}
			if out != c.want {
				t.Errorf("render %q\n got %q\nwant %q", c.tpl, out, c.want)
			}
		})
	}
}

func TestSpecComparisons(t *testing.T) {
	item := itemsFromHTML("/u.html", `<div itemscope itemtype="t" id="u"><meta itemprop="age" content="34"><meta itemprop="ok" content="true"><meta itemprop="score" content="7.5"></div>`, "t")[0]
	runSpecCases(t, []specCase{
		// R-LIQ-76: numeric strings against numbers, on either side.
		{"string-ge-number", `{% if u.age >= 18 %}Y{% else %}N{% endif %}`, M{"u": item}, "Y"},
		{"number-le-string", `{% if 18 <= u.age %}Y{% else %}N{% endif %}`, M{"u": item}, "Y"},
		{"number-gt-string", `{% if 40 > u.age %}Y{% else %}N{% endif %}`, M{"u": item}, "Y"},
		{"string-eq-number", `{% if u.age == 34 %}E{% endif %}{% if 34 == u.age %}F{% endif %}`, M{"u": item}, "EF"},
		{"float-string", `{% if u.score < 10 %}Y{% endif %}{% if u.score == 7.5 %}Z{% endif %}`, M{"u": item}, "YZ"},
		{"bool-string", `{% if u.ok == true %}T{% endif %}{% if false != u.ok %}F{% endif %}`, M{"u": item}, "TF"},
		{"no-exponent", `{% if "1e3" == 1000 %}Y{% else %}N{% endif %}`, nil, "N"},
		{"no-spaces", `{% if " 34" == 34 %}Y{% else %}N{% endif %}`, nil, "N"},
		{"strings-stay-strings", `{% if "10" < "9" %}Y{% endif %}`, nil, "Y"},
		{"incomparable", `{% if "a" < 1 %}Y{% else %}N{% endif %}{% if nil < 1 %}Y{% else %}N{% endif %}`, nil, "NN"},
		{"ne-alias", `{% if 1 <> 2 %}Y{% endif %}`, nil, "Y"},
		{"int-float", `{% if 1 == 1.0 %}Y{% endif %}`, nil, "Y"},
		{"nil-nil", `{% if nosuch == nil %}Y{% endif %}`, nil, "Y"},
		// R-LIQ-74: and/or right to left.
		{"or-and", `{% if true or false and false %}Y{% else %}N{% endif %}`, nil, "Y"},
		{"and-or", `{% if false and false or true %}Y{% else %}N{% endif %}`, nil, "N"},
		{"parens", `{% if (false and false) or true %}Y{% else %}N{% endif %}`, nil, "Y"},
		{"contains-string", `{% if "hello" contains "ell" %}Y{% endif %}`, nil, "Y"},
		{"contains-array", `{% assign a = "x,y" | split: "," %}{% if a contains "y" %}Y{% endif %}`, nil, "Y"},
		{"contains-item", `{% if u contains "age" %}Y{% else %}N{% endif %}`, M{"u": item}, "N"},
		{"contains-hash", `{% if h contains "a" %}Y{% else %}N{% endif %}{% if m contains "a" %}Y{% else %}N{% endif %}`, M{"h": NewHash().Set("a", 1), "m": M{"a": 1}}, "NN"},
		{"contains-number", `{% if "a1" contains 1 %}Y{% endif %}{% if a contains 1 %}Z{% endif %}{% if a contains "1" %}W{% endif %}`, M{"a": []any{int64(1)}}, "YZ"},
		{"contains-range", `{% if (1..5) contains 3 %}Y{% endif %}{% if (1..5) contains 6 %}N{% endif %}`, nil, "Y"},
		{"contains-nil", `{% if s contains nil %}Y{% else %}N{% endif %}{% if nosuch contains "a" %}Y{% else %}N{% endif %}`, M{"s": "abc"}, "NN"},
		{"contains-safe", `{% assign a = "<" | escape | split: "," %}{% if a contains "&lt;" %}Y{% endif %}`, nil, "Y"},
		{"compare-in-output", `{{ 1 < 2 }}|{{ "34" == 34 }}`, nil, "true|true"},
		{"compare-in-assign", `{% assign b = u.age > 30 %}{{ b }}`, M{"u": item}, "true"},
		{"case-numeric-string", `{% case u.age %}{% when 34 %}thirty-four{% endcase %}`, M{"u": item}, "thirty-four"},
		// R-LIQ-58: blank and empty on either side.
		{"blank-left", `{% if blank == s %}Y{% endif %}`, M{"s": "  "}, "Y"},
		{"blank-nil", `{% if nosuch == blank %}Y{% endif %}{% if nosuch == empty %}Z{% endif %}`, nil, "Y"},
		{"empty-hash", `{% if h == empty %}Y{% endif %}`, M{"h": NewHash()}, "Y"},
		{"blank-false", `{% if f == blank %}Y{% endif %}`, M{"f": false}, "Y"},
		{"not-blank", `{% if s != blank %}Y{% endif %}`, M{"s": "x"}, "Y"},
		{"blank-renders-empty", `[{{ blank }}]`, nil, "[]"},
	})
}

func TestSpecLoops(t *testing.T) {
	h := NewHash().Set("b", 1).Set("a", 2)
	item := itemsFromHTML("/u.html", `<div itemscope itemtype="t"><span itemprop="n">x</span></div>`, "t")[0]
	runSpecCases(t, []specCase{
		// R-LIQ-72: offset and limit first, then reversed.
		{"reversed-limit", `{% for i in (1..5) reversed limit:2 %}{{ i }}{% endfor %}`, nil, "21"},
		{"offset-limit-reversed", `{% for i in (1..9) offset: 2 limit: 3 reversed %}{{ i }}{% endfor %}`, nil, "543"},
		{"limit-variable", `{% for i in (1..9) limit: n %}{{ i }}{% endfor %}`, M{"n": "2"}, "12"},
		{"offset-past-end", `{% for i in (1..3) offset: 5 %}{{ i }}{% else %}none{% endfor %}`, nil, "none"},
		{"negative-limit", `{% for i in (1..3) limit: -1 %}{{ i }}{% else %}none{% endfor %}`, nil, "none"},
		// R-LIQ-71: loops over non-arrays.
		{"nil-else", `{% for x in nosuch %}a{% else %}E{% endfor %}`, nil, "E"},
		{"string-once", `{% for x in s %}[{{ x }}]{% endfor %}`, M{"s": "abc"}, "[abc]"},
		{"blank-string", `{% for x in s %}[{{ x }}]{% else %}E{% endfor %}`, M{"s": " "}, "E"},
		{"hash-pairs", `{% for p in h %}{{ p[0] }}={{ p[1] }};{% endfor %}`, M{"h": h}, "b=1;a=2;"},
		{"item-once", `{% for x in u %}{{ x.n }}{% endfor %}`, M{"u": item}, "x"},
		{"number-else", `{% for x in 5 %}a{% else %}E{% endfor %}`, nil, "E"},
		{"range-bounds-strings", `{% for i in (a..b) %}{{ i }}{% endfor %}`, M{"a": "2", "b": int64(4)}, "234"},
		{"empty-range", `{% for i in (3..1) %}{{ i }}{% else %}E{% endfor %}`, nil, "E"},
		{"forloop", `{% for i in (1..3) %}{{ forloop.index }}{{ forloop.index0 }}{{ forloop.rindex }}{{ forloop.rindex0 }}{% if forloop.first %}F{% endif %}{% if forloop.last %}L{% endif %}{{ forloop.length }} {% endfor %}`, nil,
			"1032F3 21213 3210L3 "},
		{"parentloop", `{% for i in (1..2) %}{% for j in (1..2) %}{{ forloop.parentloop.index }}{{ j }} {% endfor %}{% endfor %}`, nil, "11 12 21 22 "},
		{"loop-var-restored", `{% assign x = "o" %}{% for x in (1..2) %}{% endfor %}{{ x }}`, nil, "o"},
		{"break-in-if", `{% for i in (1..5) %}{% if i > 2 %}{% break %}{% endif %}{{ i }}{% endfor %}`, nil, "12"},
		{"nested-break", `{% for i in (1..2) %}{% for j in (1..3) %}{% if j == 2 %}{% break %}{% endif %}{{ i }}{{ j }} {% endfor %}{% endfor %}`, nil, "11 21 "},
		{"cycle-groups", `{% for i in (1..3) %}{% cycle "g": "a", "b" %}{% cycle "x", "y" %}{% endfor %}`, nil, "axbyax"},
		{"cycle-outside-loop", `{% cycle "a", "b" %}{% cycle "a", "b" %}`, nil, "ab"},
		// Shopify tablerow markup (R-LIQ-70).
		{"tablerow-cols", `{% tablerow i in (1..3) cols: 2 %}{{ i }}{% endtablerow %}`, nil,
			"<tr class=\"row1\">\n<td class=\"col1\">1</td><td class=\"col2\">2</td></tr>\n<tr class=\"row2\"><td class=\"col1\">3</td></tr>\n"},
		{"tablerowloop", `{% tablerow i in (1..2) %}{{ tablerowloop.col }}{{ tablerowloop.col_last }}{% endtablerow %}`, nil,
			"<tr class=\"row1\">\n<td class=\"col1\">1false</td><td class=\"col2\">2true</td></tr>\n"},
		{"tablerow-empty", `{% tablerow i in e %}{{ i }}{% endtablerow %}`, M{"e": []any{}}, "<tr class=\"row1\">\n</tr>\n"},
		{"tablerow-nil", `{% tablerow i in nosuch %}{{ i }}{% endtablerow %}`, nil, ""},
	})
}

func TestSpecTags(t *testing.T) {
	runSpecCases(t, []specCase{
		{"case-all-matches", `{% case 1 %}{% when 1 %}a{% when 2, 1 %}b{% else %}c{% endcase %}`, nil, "ab"},
		{"case-else", `{% case 3 %}{% when 1 %}a{% else %}c{% endcase %}`, nil, "c"},
		{"case-or", `{% case "y" %}{% when "x" or "y" %}hit{% endcase %}`, nil, "hit"},
		{"unless-elsif", `{% unless true %}a{% elsif true %}b{% else %}c{% endunless %}`, nil, "b"},
		{"if-filter", `{% if xs | size %}Y{% endif %}`, M{"xs": []any{}}, "Y"},
		{"capture-markers", `{% capture c %}<p>{{ "x" | date: "%Y" }}</p>{% endcapture %}{% if c contains "pagelove.org/Error" %}marker{% endif %}`, nil, "marker"},
		{"increment-decrement", `{% increment n %}{% increment n %}{% decrement n %}{{ n }}`, nil, "0111"},
		{"counter-vs-assign", `{% assign n = 10 %}{% increment n %}{{ n }}`, nil, "010"},
		{"liquid-tag", "{% liquid\n  assign x = 2\n  # a comment\n  if x > 1\n    echo x | plus: 1\n  endif\n%}!", nil, "3!"},
		{"inline-comment", `a{% # note %}b{%- # trims -%} c`, nil, "abc"},
		{"echo", `{% echo "x" | upcase %}`, nil, "X"},
		{"raw", `{% raw %}{{ x }}{% if %}{% endraw %}`, nil, "{{ x }}{% if %}"},
		{"multiline-output", "{{\n  \"a\"\n  | upcase\n}}", nil, "A"},
		{"empty-output", `[{{ }}]`, nil, "[]"},
		{"dollar-brace", `${{ 5 }}`, nil, "$5"},
		{"percent-in-output", `{{ "50%}" }}`, nil, "50%}"},
		{"null-is-nil", `{% if null == nil %}Y{% endif %}`, nil, "Y"},
		{"negative-index", `{% assign a = "x,y,z" | split: "," %}{{ a[-1] }}{{ a[i] }}`, M{"i": int64(1)}, "zy"},
		{"nested-int64-index", `{{ a[h.i] }}{{ a[h.i | plus: 1] }}{{ h["i"] }}`, M{"a": []any{"x", "y", "z"}, "h": NewHash().Set("i", int64(1))}, "yz1"},
		{"size-code-points", `{{ s.size }}|{{ s | size }}`, M{"s": "héllo"}, "5|5"},
		{"size-hash", `{{ h.size }}|{{ g.size }}`, M{"h": NewHash().Set("a", 1), "g": NewHash().Set("size", "big")}, "1|big"},
		{"size-range", `{{ (1..4).size }}`, nil, "4"},
		{"data-not-reevaluated", `{{ x }}`, M{"x": "{{ 6 | times: 7 }}"}, "{{ 6 | times: 7 }}"},
	})
}

func TestSpecRendering(t *testing.T) {
	runSpecCases(t, []specCase{
		// R-LIQ-56.
		{"floats", `{{ f }}|{{ g }}|{{ 7 | divided_by: 2.0 }}|{{ 1.5 | times: 2 }}`, M{"f": 100.0, "g": 3.25}, "100.0|3.25|3.5|3.0"},
		{"non-finite", `{{ a }}|{{ b }}|{{ c }}`, M{"a": math.NaN(), "b": math.Inf(1), "c": math.Inf(-1)}, "NaN|Infinity|-Infinity"},
		{"big-float", `{{ f }}`, M{"f": 1e20}, "100000000000000000000"},
		{"bool", `{{ true }}{{ false }}`, nil, "truefalse"},
		{"array-concat", `{{ a }}`, M{"a": []any{"x", 1, nil, 2.0}}, "x12.0"},
		{"hash-json", `{{ h }}`, M{"h": NewHash().Set("b", 1).Set("a", "<x>")}, `{"b":1,"a":"<x>"}`},
		{"range", `{{ (1..3) }}`, nil, "1..3"},
		{"int64", `{{ n | plus: 1 }}`, M{"n": int64(41)}, "42"},
		// R-LIQ-91: escape marks safe and is idempotent; other filters drop the mark.
		{"escape-twice", `{{ "<" | escape | escape }}`, nil, "&amp;lt;"}, // live 2026-09-29
		{"escape-once-twice", `{{ "<&amp;" | escape_once | escape }}`, nil, "&amp;lt;&amp;amp;"},
		{"mark-dropped", `{{ "<" | escape | append: "" | escape }}`, nil, "&amp;lt;"},
		{"safe-compare", `{% assign e = "a&b" | escape %}{% if e == "a&amp;b" %}Y{% endif %}{% if e contains "&amp;" %}Z{% endif %}{{ e.size }}`, nil, "YZ7"},
		// R-LIQ-90: no autoescape.
		{"no-autoescape", `{{ x }}`, M{"x": "<b>&</b>"}, "<b>&</b>"},
	})
}

func TestSpecErrors(t *testing.T) {
	e := newTestEngine()
	render := func(tpl string, vars M) (string, error) {
		return e.Render(context.Background(), tpl, vars, nil)
	}
	marker := `itemtype="https://pagelove.org/Error"`
	for _, c := range []struct {
		name, tpl, want string
	}{
		{"markup", `<p>{{ "x" | date: "%Y" }}</p>`, `<p><span itemscope ` + marker},
		{"names-filter", `{{ "x" | date: "%Y" }}`, `date: invalid date`},
		{"echo", `<p>{% echo "x" | date %}</p>`, marker},
		{"undefined-filter", `{{ 1 | nosuch }}`, `undefined filter`},
		{"missing-arg", `{{ "a" | replace }}`, `replace: missing argument`},
		{"bad-arg", `{{ "a" | truncate: "x" }}`, `truncate: argument must be a number`},
		{"negative-truncate", `{{ "a" | truncate: -1 }}`, `truncate: length must not be negative`},
		{"unknown-keyword", `{{ 8 | random: bogus: 1 }}`, `random: unknown keyword argument`},
		{"predicate", `{{ xs | where_exp: "x", "x ==" }}`, `invalid expression`},
		{"message-escaped", `{{ "<b>" | date }}`, `&quot;&lt;b&gt;&quot;`},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := render(c.tpl, M{"xs": []any{1}})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("%q: got %q, want it to contain %q", c.tpl, out, c.want)
			}
		})
	}
	for _, c := range []struct{ name, tpl string }{
		{"script", `<script>{{ "x" | date: "%Y" }}</script>`},
		{"attribute", `<a href="{{ "x" | date }}">`},
		{"unquoted-attribute", `<a href={{ "x" | date }}>`},
		{"between-attributes", `<a {{ "x" | date }}>`},
		{"comment", `<!-- {{ "x" | date }} -->`},
		{"title", `<title>{{ "x" | date }}</title>`},
		{"textarea-uppercase", `<TEXTAREA>{{ "x" | date }}</TEXTAREA>`},
	} {
		t.Run("empty/"+c.name, func(t *testing.T) {
			out, err := render(c.tpl, nil)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "Error") {
				t.Errorf("%q rendered a marker: %q", c.tpl, out)
			}
		})
	}
	// After a raw-text element closes, markup context is back.
	out, _ := render(`<script>x</script><p>{{ "x" | date }}</p>`, nil)
	if !strings.Contains(out, marker) {
		t.Errorf("no marker after </script>: %q", out)
	}

	// Composition errors (R-LIQ-202, R-LIQ-204, R-LIQ-205).
	for _, c := range []struct {
		name, tpl, msg string
		line           int
	}{
		{"if", "a\n{% if x | date %}{% endif %}", "date", 2},
		{"for", "{% for i in x | date %}{% endfor %}", "date", 1},
		{"assign", "\n\n{% assign y = x | date %}", "date", 3},
		{"case", "{% case x | date %}{% endcase %}", "date", 1},
		{"unterminated-output", "ok\n{{ x", "unterminated output", 2},
		{"unterminated-tag", "{% if", "unterminated tag", 1},
		{"unterminated-block", "{% if true %}", `unterminated "if" block`, 1},
		{"unterminated-raw", "\n{% raw %}x", `unterminated "raw" block`, 2},
		{"unknown-tag", "{% frobnicate %}", `undefined tag "frobnicate"`, 1},
		{"include", `{% include "go.mod" %}`, "no I/O", 1},
		{"render", `{% render "x" %}`, "no I/O", 1},
		{"syntax-in-output", "\n{{ x | }}", "syntax error", 2},
		{"break-outside", "{% break %}", "break outside a loop", 1},
		{"else-not-last", "{% if a %}{% else %}{% elsif b %}{% endif %}", "else must be the last", 1},
		{"line-after-multiline-output", "{{\nx\n}}\n{% if %}", "", 4},
	} {
		t.Run("fails/"+c.name, func(t *testing.T) {
			_, err := render(c.tpl, M{"x": "not a date"})
			var le *Error
			if !errors.As(err, &le) {
				t.Fatalf("%q: want *Error, got %v", c.tpl, err)
			}
			if le.Kind != KindTemplate || le.Status() != 500 {
				t.Errorf("%q: kind %s status %d", c.tpl, le.Kind, le.Status())
			}
			if !strings.Contains(le.Message, c.msg) {
				t.Errorf("%q: message %q lacks %q", c.tpl, le.Message, c.msg)
			}
			if le.Line != c.line {
				t.Errorf("%q: line %d, want %d (%s)", c.tpl, le.Line, c.line, le.Message)
			}
			if strings.Contains(le.Message, "pl_") || strings.Contains(le.Message, "goroutine") {
				t.Errorf("%q: message leaks internals: %q", c.tpl, le.Message)
			}
		})
	}
}

// TestAutoEscape: with Options.AutoEscape (how composition renders, as
// PageLove does, live 2026-09-29) outputs are HTML-escaped unless safe.
func TestAutoEscape(t *testing.T) {
	e := New(Options{AutoEscape: true})
	for _, c := range []struct{ src, want string }{
		{`{{ q }}`, "&lt;b&gt;x&lt;/b&gt;"},
		{`{{ q | escape }}`, "&lt;b&gt;x&lt;/b&gt;"},
		{`{{ "<" | escape | escape }}`, "&amp;lt;"},
		{`{{ "a\nb" | newline_to_br }}`, "a&lt;br /&gt;\nb"},
		{`<a title="{{ t }}">`, `<a title="&quot;q&#39;">`},
		{`{% echo q %}`, "&lt;b&gt;x&lt;/b&gt;"},
	} {
		got, err := e.Render(context.Background(), c.src, map[string]any{"q": "<b>x</b>", "t": `"q'`}, nil)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q (%v), want %q", c.src, got, err, c.want)
		}
	}
	got, _ := e.RenderFor(context.Background(), Host{Tag: "script"}, `var s = "{{ q }}";`, map[string]any{"q": "<b>"}, nil)
	if got != `var s = "<b>";` {
		t.Errorf("raw-text host: %q", got)
	}
}
