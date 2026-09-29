package liquid

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// The corpus of decision 0002 (190 cases), ported from the spike
// (the Liquid spike, docs/decisions/0002). Every case passes: the spike's four
// known gaps (numeric-string comparison in where_exp and find_exp, the
// order of reversed and limit, tablerow markup) are closed here, as
// docs/spec/liquid.md R-LIQ-76, R-LIQ-72 and R-LIQ-70 require. Where the
// spec differs from the spike's expectation, the case follows the spec and
// says so.

// Example is one template from the PageLove docs or an upstream app, with
// equivalent data and the expected output.
type Example struct {
	ID     string
	Source string // doc page or app file
	// Tier: "std" = standard Shopify tag/filter; "ext" = PageLove/Jekyll/LiquidJS
	// extension; "app" = whole template from docs/apps; "rt" = runtime behaviour.
	Tier string
	// Expect: "documented" (output printed in the docs), "derived" (worked out
	// from the doc's prose/semantics), "reconstructed" (doc shows output but
	// the template source was lost in the scrape; template rebuilt).
	Expect string
	Tpl    string
	Data   func() map[string]any
	Want   string
	Norm   bool                              // compare with whitespace collapsed
	Check  func(out string, err error) error // replaces Want when set
}

var ws = regexp.MustCompile(`\s+`)

// Normalize collapses whitespace runs, for HTML comparisons.
func Normalize(s string) string { return strings.TrimSpace(ws.ReplaceAllString(s, " ")) }

// Verify checks an output against the example's expectation.
func (e Example) Verify(out string, err error) error {
	if e.Check != nil {
		return e.Check(out, err)
	}
	if err != nil {
		return err
	}
	got, want := out, e.Want
	if e.Norm {
		got, want = Normalize(got), Normalize(want)
	}
	if got != want {
		return fmt.Errorf("got %q want %q", clip(got), clip(want))
	}
	return nil
}

func clip(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

func matches(re string) func(string, error) error {
	r := regexp.MustCompile(re)
	return func(out string, err error) error {
		if err != nil {
			return err
		}
		if !r.MatchString(out) {
			return fmt.Errorf("output %q does not match %s", clip(out), re)
		}
		return nil
	}
}

func wantErr(sub string) func(string, error) error {
	return func(out string, err error) error {
		if err == nil {
			return fmt.Errorf("expected an error containing %q, got output %q", sub, clip(out))
		}
		if !strings.Contains(err.Error(), sub) {
			return fmt.Errorf("error %q does not mention %q", firstLine(err.Error()), sub)
		}
		return nil
	}
}

func containsAll(subs ...string) func(string, error) error {
	return func(out string, err error) error {
		if err != nil {
			return err
		}
		for _, s := range subs {
			if !strings.Contains(out, s) {
				return fmt.Errorf("output lacks %q: %q", s, clip(out))
			}
		}
		return nil
	}
}

func str(v ...any) []any { return v }

const errMarker = `itemtype="https://pagelove.org/Error"`

// Corpus returns every example.
func Corpus() []Example {
	var c []Example
	add := func(e ...Example) { c = append(c, e...) }
	const (
		S  = "languages/liquid/filters/string"
		N  = "languages/liquid/filters/number"
		A  = "languages/liquid/filters/array"
		D  = "languages/liquid/filters/date"
		SE = "languages/liquid/filters/security"
		R  = "languages/liquid/filters/random"
		DA = "languages/liquid/filters/data"
		X  = "languages/liquid/filters/expressions"
		F  = "languages/liquid/filters"
		T  = "languages/liquid/templating"
	)
	tags := func() map[string]any { return M{"tags": str("a", "b", "c")} }

	// ---------------- string ----------------
	add(
		Example{ID: "downcase", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "Hello, World" | downcase }}`, Want: "hello, world"},
		Example{ID: "upcase", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "Hello, World" | upcase }}`, Want: "HELLO, WORLD"},
		Example{ID: "capitalize", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "hello WORLD" | capitalize }}`, Want: "Hello world"},
		Example{ID: "strip", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "  hi  " | strip }}`, Want: "hi"},
		Example{ID: "lstrip", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "  hi  " | lstrip }}`, Want: "hi  "},
		Example{ID: "rstrip", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "  hi  " | rstrip }}`, Want: "  hi"},
		Example{ID: "strip_newlines", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "a\nb\nc" | strip_newlines }}`, Want: "abc"},
		Example{ID: "strip_newlines/crlf", Source: S, Tier: "std", Expect: "derived", Tpl: `{{ s | strip_newlines }}`, Data: func() M { return M{"s": "a\r\nb"} }, Want: "ab"},
		Example{ID: "newline_to_br", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "a\nb" | newline_to_br }}`, Want: "a<br />\nb"},
		Example{ID: "normalize_whitespace", Source: S, Tier: "ext", Expect: "documented", Tpl: `{{ "a   b\n c" | normalize_whitespace }}`, Want: "a b c"},
		Example{ID: "escape", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "<a href='x'>" | escape }}`, Want: "&lt;a href=&#39;x&#39;&gt;"},
		Example{ID: "escape/dquote", Source: S, Tier: "std", Expect: "derived", Tpl: `{{ s | escape }}`, Data: func() M { return M{"s": `say "hi"`} }, Want: "say &quot;hi&quot;"},
		Example{ID: "escape_once", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "1 &lt; 2 &amp; 3" | escape_once }}`, Want: "1 &lt; 2 &amp; 3"},
		Example{ID: "escape_once/named-entity", Source: S, Tier: "std", Expect: "derived", Tpl: `{{ "&copy; & <" | escape_once }}`, Want: "&copy; &amp; &lt;"},
		Example{ID: "url_encode", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "a b&c" | url_encode }}`, Want: "a+b%26c"},
		Example{ID: "url_decode", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "a+b%26c" | url_decode }}`, Want: "a b&c"},
		Example{ID: "cgi_escape", Source: S, Tier: "ext", Expect: "documented", Tpl: `{{ "a b & c" | cgi_escape }}`, Want: "a+b+%26+c"},
		Example{ID: "uri_escape", Source: S, Tier: "ext", Expect: "documented", Tpl: `{{ "http://x/a b?q=1&r=2" | uri_escape }}`, Want: "http://x/a%20b?q=1&r=2"},
		Example{ID: "xml_escape", Source: S, Tier: "ext", Expect: "documented", Tpl: `{{ "<a>'x'</a>" | xml_escape }}`, Want: "&lt;a&gt;&#39;x&#39;&lt;/a&gt;"},
		Example{ID: "strip_html", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "<b>hi</b><script>x()</script>" | strip_html }}`, Want: "hi"},
		Example{ID: "replace", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "a-b-c" | replace: "-", "+" }}`, Want: "a+b+c"},
		Example{ID: "replace_first", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "a-b-c" | replace_first: "-", "+" }}`, Want: "a+b-c"},
		Example{ID: "replace_last", Source: S, Tier: "ext", Expect: "documented", Tpl: `{{ "a-b-c" | replace_last: "-", "+" }}`, Want: "a-b+c"},
		Example{ID: "remove", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "a-b-c" | remove: "-" }}`, Want: "abc"},
		Example{ID: "remove_first", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "a-b-c" | remove_first: "-" }}`, Want: "ab-c"},
		Example{ID: "remove_last", Source: S, Tier: "ext", Expect: "documented", Tpl: `{{ "a-b-c" | remove_last: "-" }}`, Want: "a-bc"},
		Example{ID: "append", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "/page" | append: ".html" }}`, Want: "/page.html"},
		Example{ID: "prepend", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "world" | prepend: "hello " }}`, Want: "hello world"},
		Example{ID: "array_to_sentence_string", Source: S, Tier: "ext", Expect: "documented", Tpl: `{{ tags | array_to_sentence_string }}`, Data: tags, Want: "a, b, and c"},
		Example{ID: "array_to_sentence_string/or", Source: S, Tier: "ext", Expect: "documented", Tpl: `{{ tags | array_to_sentence_string: "or" }}`, Data: tags, Want: "a, b, or c"},
		Example{ID: "slice", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "hello" | slice: 1, 3 }}`, Want: "ell"},
		Example{ID: "slice/negative", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "hello" | slice: -1 }}`, Want: "o"},
		Example{ID: "split", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "a,b,c" | split: "," | join: " · " }}`, Want: "a · b · c"},
		Example{ID: "truncate", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "The quick brown fox" | truncate: 9 }}`, Want: "The qu..."},
		Example{ID: "truncatewords", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "The quick brown fox" | truncatewords: 2 }}`, Want: "The quick..."},
		Example{ID: "size", Source: S, Tier: "std", Expect: "documented", Tpl: `{{ "hello" | size }}`, Want: "5"},
		Example{ID: "size/object", Source: S, Tier: "std", Expect: "derived", Tpl: `{{ o | size }}`, Data: func() M { return M{"o": M{"a": 1, "b": 2}} }, Want: "2"},
		Example{ID: "number_of_words", Source: S, Tier: "ext", Expect: "documented", Tpl: `{{ "the quick brown fox" | number_of_words }}`, Want: "4"},
		Example{ID: "default", Source: S, Tier: "std", Expect: "derived", Tpl: `{{ user.nickname | default: "Anonymous" }}`, Data: func() M { return M{"user": people()[1]} }, Want: "Anonymous"},
		Example{ID: "slugify", Source: S, Tier: "ext", Expect: "documented", Tpl: `{{ "Hello, World!" | slugify }}`, Want: "hello-world"},
	)

	// ---------------- number ----------------
	add(
		Example{ID: "plus", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ 10 | plus: 5 }}`, Want: "15"},
		Example{ID: "minus", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ 10 | minus: 3 }}`, Want: "7"},
		Example{ID: "times", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ 6 | times: 7 }}`, Want: "42"},
		Example{ID: "divided_by/int", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ 7 | divided_by: 2 }}`, Want: "3"},
		Example{ID: "divided_by/float", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ 7 | divided_by: 2.0 }}`, Want: "3.5"},
		Example{ID: "divided_by/zero", Source: N, Tier: "std", Expect: "derived", Tpl: `{{ 7 | divided_by: 0 }}`, Want: "7"},
		Example{ID: "divided_by/floor", Source: N, Tier: "std", Expect: "derived", Tpl: `{{ -7 | divided_by: 2 }}`, Want: "-4"},
		Example{ID: "modulo", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ 13 | modulo: 5 }}`, Want: "3"},
		Example{ID: "modulo/zero", Source: N, Tier: "std", Expect: "derived", Tpl: `{{ 13 | modulo: 0 }}`, Want: "13"},
		Example{ID: "abs", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ -8 | abs }}`, Want: "8"},
		Example{ID: "ceil", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ 3.2 | ceil }}`, Want: "4"},
		Example{ID: "floor", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ 3.8 | floor }}`, Want: "3"},
		Example{ID: "round", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ 3.14159 | round }}`, Want: "3"},
		Example{ID: "round/2", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ 3.14159 | round: 2 }}`, Want: "3.14"},
		Example{ID: "round/int-type", Source: N, Tier: "std", Expect: "derived", Tpl: `{{ 7.6 | round | divided_by: 3 }}`, Want: "2"},
		Example{ID: "at_least/raise", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ 3 | at_least: 5 }}`, Want: "5"},
		Example{ID: "at_least/keep", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ 8 | at_least: 5 }}`, Want: "8"},
		Example{ID: "at_most/lower", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ 8 | at_most: 5 }}`, Want: "5"},
		Example{ID: "at_most/keep", Source: N, Tier: "std", Expect: "documented", Tpl: `{{ 3 | at_most: 5 }}`, Want: "3"},
		Example{ID: "to_integer", Source: N, Tier: "ext", Expect: "documented", Tpl: `{{ "3.9" | to_integer }}`, Want: "3"},
		Example{ID: "to_integer/plus", Source: N, Tier: "ext", Expect: "documented", Tpl: `{{ "42" | to_integer | plus: 8 }}`, Want: "50"},
		Example{ID: "to_integer/saturate", Source: N, Tier: "ext", Expect: "derived", Tpl: `{{ "1e30" | to_integer }}|{{ true | to_integer }}|{{ nil | to_integer }}`, Want: "9223372036854775807|1|0"},
		Example{ID: "plus/microdata-string", Source: N, Tier: "std", Expect: "derived", Tpl: `{{ p.price | plus: 1 }}`, Data: func() M { return M{"p": products()[2]} }, Want: "23"},
		Example{ID: "plus/float-result", Source: N, Tier: "std", Expect: "derived", Tpl: `{{ 10 | plus: 5.0 }}`, Want: "15.0"},
	)

	// ---------------- array ----------------
	items := func() M { return M{"items": str("x", "y", "z")} }
	add(
		Example{ID: "first", Source: A, Tier: "std", Expect: "derived", Tpl: `{{ items | first }}`, Data: items, Want: "x"},
		Example{ID: "first/string", Source: A, Tier: "std", Expect: "derived", Tpl: `{{ "hello" | first }}`, Want: "h"},
		Example{ID: "last", Source: A, Tier: "std", Expect: "derived", Tpl: `{{ items | last }}`, Data: items, Want: "z"},
		Example{ID: "reverse", Source: A, Tier: "std", Expect: "documented", Tpl: `{{ "a,b,c" | split: "," | reverse | join: "," }}`, Want: "c,b,a"},
		Example{ID: "sort", Source: A, Tier: "std", Expect: "documented", Tpl: `{{ "banana,apple,cherry" | split: "," | sort | join: ", " }}`, Want: "apple, banana, cherry"},
		Example{ID: "sort/by-field", Source: A, Tier: "std", Expect: "derived", Tpl: `{% assign newest = posts | sort: "date" %}{{ newest | map: "title" | join: "," }}`, Data: func() M { return M{"posts": yearPosts()} }, Want: "Two,Three,One"},
		Example{ID: "sort/missing-last", Source: A, Tier: "std", Expect: "derived", Tpl: `{{ people | sort: "nickname" | map: "name" | join: "," }}`, Data: func() M { return M{"people": people()} }, Want: "Dee,Cy,Ada,Bob"},
		Example{ID: "sort/mixed-kinds", Source: A, Tier: "std", Expect: "derived", Tpl: `{{ mixed | sort | join: "," }}`, Data: func() M { return M{"mixed": str(2, "apple", 1, "Banana")} }, Want: "1,2,Banana,apple"},
		Example{ID: "sort/numeric-strings", Source: A, Tier: "std", Expect: "derived", Tpl: `{{ "10,9" | split: "," | sort | join: "," }}`, Want: "9,10"},
		Example{ID: "sort_natural", Source: A, Tier: "std", Expect: "documented", Tpl: `{{ "b,A,c" | split: "," | sort_natural | join: "" }}`, Want: "Abc"},
		Example{ID: "sort_natural/text-order", Source: A, Tier: "std", Expect: "derived", Tpl: `{{ "9,10" | split: "," | sort_natural | join: "," }}`, Want: "10,9"},
		Example{ID: "sort_natural/by-field", Source: A, Tier: "std", Expect: "derived", Tpl: `{{ people | sort_natural: "nickname" | map: "name" | join: "," }}`, Data: func() M { return M{"people": people()} }, Want: "Dee,Cy,Ada,Bob"},
		Example{ID: "uniq", Source: A, Tier: "std", Expect: "documented", Tpl: `{{ "a,b,a,c" | split: "," | uniq | join: "," }}`, Want: "a,b,c"},
		Example{ID: "compact", Source: A, Tier: "std", Expect: "derived", Tpl: `{{ list | compact | join: ", " }}`, Data: func() M { return M{"list": str("a", nil, "b")} }, Want: "a, b"},
		Example{ID: "map", Source: A, Tier: "std", Expect: "derived", Tpl: `{{ people | map: "name" | join: ", " }}`, Data: func() M { return M{"people": people()} }, Want: "Ada, Bob, Cy, Dee"},
		Example{ID: "join", Source: A, Tier: "std", Expect: "derived", Tpl: `{{ tags | join: ", " }}`, Data: tags, Want: "a, b, c"},
		Example{ID: "join/scalar", Source: "pagelove-shop admin/products.html", Tier: "std", Expect: "derived", Tpl: `{{ p.variant | join: "," }}`, Data: func() M { return M{"p": shopProducts()[0]} }, Want: "One size"},
		Example{ID: "concat", Source: A, Tier: "std", Expect: "derived", Tpl: `{% assign all = drafts | concat: published %}{{ all | join: "," }}`, Data: func() M { return M{"drafts": str("d1"), "published": str("p1", "p2")} }, Want: "d1,p1,p2"},
		Example{ID: "where", Source: A, Tier: "std", Expect: "derived", Tpl: `{{ people | where: "role", "admin" | map: "name" | join: ", " }}`, Data: func() M { return M{"people": people()} }, Want: "Ada, Cy"},
		Example{ID: "where/truthy", Source: A, Tier: "std", Expect: "derived", Tpl: `{{ people | where: "nickname" | map: "name" | join: ", " }}`, Data: func() M { return M{"people": people()} }, Want: "Ada, Cy, Dee"},
		Example{ID: "reject", Source: A, Tier: "ext", Expect: "derived", Tpl: `{% assign active = users | reject: "suspended", true %}{{ active | map: "name" | join: ", " }}`, Data: func() M { return M{"users": mdUsers()} }, Want: "Ann, Cat"},
		// The docs pipe find's single result into map; live PageLove maps
		// arrays only, so that renders nothing (R-LIQ-145, live 2026-09-29).
		Example{ID: "find", Source: A, Tier: "ext", Expect: "derived", Tpl: `{{ products | find: "sku", "A-1" | map: "title" }}`, Data: func() M { return M{"products": products()} }, Want: ""},
		Example{ID: "find_index", Source: A, Tier: "ext", Expect: "documented", Tpl: `{{ products | find_index: "sku", "A-1" }}`, Data: func() M { return M{"products": products()} }, Want: "0"},
		Example{ID: "has", Source: A, Tier: "ext", Expect: "derived", Tpl: `{% if products | has: "onsale", true %}Sale on now!{% endif %}`, Data: func() M { return M{"products": products()} }, Want: "Sale on now!"},
		Example{ID: "group_by", Source: A, Tier: "ext", Expect: "derived", Tpl: `{% assign by_year = posts | group_by: "year" %}
{% for group in by_year %}
  <h2>{{ group.name }}</h2>
  <ul>{% for post in group.items %}<li>{{ post.title }}</li>{% endfor %}</ul>
{% endfor %}`, Data: func() M { return M{"posts": yearPosts()} }, Norm: true,
			Want: `<h2>2026</h2> <ul><li>One</li><li>Three</li></ul> <h2>2025</h2> <ul><li>Two</li></ul>`},
		Example{ID: "sum", Source: A, Tier: "ext", Expect: "derived", Tpl: `{{ prices | sum }}`, Data: func() M { return M{"prices": str(1, 2, 3)} }, Want: "6"},
		Example{ID: "sum/field", Source: A, Tier: "ext", Expect: "derived", Tpl: `{{ line_items | sum: "price" }}`, Data: func() M { return M{"line_items": lineItems()} }, Want: "23.5"},
		Example{ID: "sum/whole-is-int", Source: A, Tier: "ext", Expect: "derived", Tpl: `{{ ps | sum }}`, Data: func() M { return M{"ps": str("2", "3", "x")} }, Want: "5"},
		Example{ID: "push/loop", Source: A, Tier: "ext", Expect: "derived", Tpl: `{% for term in terms %}{% assign wanted = wanted | push: term %}{% endfor %}{{ wanted | join: ", " }}`, Data: func() M { return M{"terms": str("x", "y")} }, Want: "x, y"},
		Example{ID: "split/empty-is-one", Source: A, Tier: "std", Expect: "derived", Tpl: `{% assign a = "" | split: "," %}{{ a.size }}`, Want: "1"},
		Example{ID: "unshift", Source: A, Tier: "ext", Expect: "derived", Tpl: `{% assign crumbs = crumbs | unshift: "Home" %}{{ crumbs | join: " > " }}`, Data: func() M { return M{"crumbs": str("Blog", "Post")} }, Want: "Home > Blog > Post"},
		Example{ID: "pop", Source: A, Tier: "ext", Expect: "derived", Tpl: `{% assign rest = items | pop %}{{ rest | join: "," }}|{{ items | join: "," }}`, Data: items, Want: "x,y|x,y,z"},
		Example{ID: "shift", Source: A, Tier: "ext", Expect: "derived", Tpl: `{% assign tail = items | shift %}{{ tail | join: "," }}`, Data: items, Want: "y,z"},
	)

	// ---------------- date ----------------
	add(
		Example{ID: "date/B-Y", Source: D, Tier: "std", Expect: "documented", Tpl: `{{ "2026-04-12" | date: "%B %Y" }}`, Want: "April 2026"},
		Example{ID: "date/H-M", Source: D, Tier: "std", Expect: "documented", Tpl: `{{ "2026-04-12T13:45:00Z" | date: "%H:%M" }}`, Want: "13:45"},
		Example{ID: "date/now", Source: D, Tier: "std", Expect: "documented", Tpl: `{{ "now" | date: "%Y" }}`, Want: "2026"},
		Example{ID: "date/default-format", Source: D, Tier: "std", Expect: "derived", Tpl: `{{ "2026-04-12T13:45:00Z" | date }}`, Want: "2026-04-12"},
		Example{ID: "date/no-offset-is-utc", Source: D, Tier: "std", Expect: "derived", Tpl: `{{ "2026-04-12T13:45:00" | date: "%H:%M %Z %z" }}`, Want: "13:45 UTC +0000"},
		Example{ID: "date/offset-normalised", Source: D, Tier: "std", Expect: "derived", Tpl: `{{ "2026-04-12T13:45:00+02:00" | date: "%H:%M" }}`, Want: "11:45"},
		Example{ID: "date/date-only-midnight-utc", Source: D, Tier: "std", Expect: "derived", Tpl: `{{ "2026-04-12" | date: "%H:%M %z %j %A %a %b %p" }}`, Want: "00:00 +0000 102 Sunday Sun Apr AM"},
		Example{ID: "date/unix-string", Source: D, Tier: "std", Expect: "derived", Tpl: `{{ "1767225600" | date: "%Y-%m-%d" }}`, Want: "2026-01-01"},
		Example{ID: "date/unknown-directive", Source: D, Tier: "std", Expect: "derived", Tpl: `{{ "2026-04-02" | date: "%e/%m %%" }}`, Want: "%e/04 %"},
		Example{ID: "date/today", Source: D, Tier: "std", Expect: "derived", Tpl: `{{ "today" | date: "%Y-%m-%d %H:%M" }}`, Want: "2026-09-28 00:00"},
		Example{ID: "date_add", Source: D, Tier: "ext", Expect: "documented", Tpl: `{{ "2026-01-01T00:00:00Z" | date_add: 3600 }}`, Want: "2026-01-01T01:00:00Z"},
		Example{ID: "date_add/now", Source: D, Tier: "ext", Expect: "derived", Tpl: `{{ "now" | date_add: 172800 }}`, Want: "2026-09-30T12:00:00Z"},
		Example{ID: "unix_to_iso", Source: D, Tier: "ext", Expect: "documented", Tpl: `{{ 1767225600 | unix_to_iso }}`, Want: "2026-01-01T00:00:00Z"},
		Example{ID: "date_to_string", Source: D, Tier: "ext", Expect: "documented", Tpl: `{{ "2026-07-07T13:07:59Z" | date_to_string }}`, Want: "07 Jul 2026"},
		Example{ID: "date_to_long_string", Source: D, Tier: "ext", Expect: "documented", Tpl: `{{ "2026-07-07T13:07:59Z" | date_to_long_string }}`, Want: "07 July 2026"},
		Example{ID: "date_to_rfc822", Source: D, Tier: "ext", Expect: "documented", Tpl: `{{ "2026-07-07T13:07:59Z" | date_to_rfc822 }}`, Want: "Tue, 07 Jul 2026 13:07:59 +0000"},
		Example{ID: "date_to_xmlschema", Source: D, Tier: "ext", Expect: "documented", Tpl: `{{ "2026-07-07T13:07:59Z" | date_to_xmlschema }}`, Want: "2026-07-07T13:07:59+00:00"},
	)

	// ---------------- security / random ----------------
	add(
		Example{ID: "sha256", Source: SE, Tier: "ext", Expect: "documented", Tpl: `{{ "hello" | sha256 }}`, Want: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"},
		Example{ID: "bcrypt", Source: SE, Tier: "ext", Expect: "derived", Tpl: `{{ "password" | bcrypt }}`, Check: bcryptCheck(12)},
		Example{ID: "bcrypt/cost", Source: SE, Tier: "ext", Expect: "derived", Tpl: `{{ "password" | bcrypt: 10 }}`, Check: bcryptCheck(10)},
		Example{ID: "argon2", Source: SE, Tier: "ext", Expect: "derived", Tpl: `{{ "password" | argon2 }}`, Check: argonPHC(19456, 2)},
		Example{ID: "argon2/kwargs", Source: SE, Tier: "ext", Expect: "derived", Tpl: `{{ "password" | argon2: memory: 65536, time: 3 }}`, Check: argonPHC(65536, 3)},
		Example{ID: "argon2/raw", Source: SE, Tier: "ext", Expect: "derived", Tpl: `{{ "password" | argon2: format: "raw", salt: "per-user-unique-salt" }}`,
			Want: hex.EncodeToString(argon2.IDKey([]byte("password"), []byte("per-user-unique-salt"), 2, 19456, 1, 32))},
		Example{ID: "argon2/raw-needs-salt", Source: SE, Tier: "ext", Expect: "derived", Tpl: `<p>{{ "password" | argon2: format: "raw" }}</p>`, Check: containsAll(errMarker)},
		Example{ID: "random", Source: R, Tier: "ext", Expect: "derived", Tpl: `{{ 32 | random }}`, Check: matches(`^[A-Za-z0-9]{32}$`)},
		Example{ID: "random/mins", Source: R, Tier: "ext", Expect: "derived", Tpl: `{{ 24 | random: upper: 3, lower: 3, digits: 2 }}`, Check: randomMins},
		Example{ID: "random/classes", Source: R, Tier: "ext", Expect: "derived", Tpl: `{{ 16 | random: lower: true, digits: true }}`, Check: matches(`^[a-z0-9]{16}$`)},
		Example{ID: "random/url_safe", Source: R, Tier: "ext", Expect: "derived", Tpl: `{{ 40 | random: url_safe: true }}`, Check: matches(`^[A-Za-z0-9_-]{40}$`)},
		Example{ID: "diceware", Source: R, Tier: "ext", Expect: "derived", Tpl: `{{ 3 | diceware }}`, Check: matches(`^[a-z]+-[a-z]+-[a-z]+$`)},
	)

	// ---------------- data ----------------
	req := func() M { return M{"request": request(false)} }
	add(
		Example{ID: "json", Source: DA, Tier: "ext", Expect: "derived", Tpl: `{{ request | json }}`, Data: req, Check: jsonCheck(false)},
		Example{ID: "json/indent", Source: DA, Tier: "ext", Expect: "derived", Tpl: `{{ request | json: 2 }}`, Data: req, Check: jsonCheck(true)},
		Example{ID: "jsonify", Source: DA, Tier: "ext", Expect: "derived", Tpl: `{{ request | jsonify }}`, Data: req, Check: jsonCheck(false)},
		Example{ID: "inspect", Source: DA, Tier: "ext", Expect: "derived", Tpl: `{{ some_value | inspect }}`, Data: func() M { return M{"some_value": M{"a": str(1, "<b>")}} }, Want: `{"a":[1,"<b>"]}`},
		Example{ID: "json/item", Source: DA, Tier: "ext", Expect: "derived", Tpl: `{{ p | json }}`, Data: func() M { return M{"p": products()[0]} },
			Want: `{"@id":"/products.html#a1","@type":"https://example.com/Product","sku":"A-1","title":"Mug","price":"12.50","onsale":"false"}`},
	)

	// ---------------- expression variants ----------------
	typed := func() M { return M{"users": typedUsers()} }
	add(
		Example{ID: "where_exp", Source: X, Tier: "ext", Expect: "derived", Tpl: `{{ users | where_exp: "u", "u.age >= 18" | map: "name" | join: ", " }}`, Data: typed, Want: "Ann, Cat"},
		Example{ID: "where_exp/microdata-strings", Source: X, Tier: "ext", Expect: "derived", Tpl: `{{ users | where_exp: "u", "u.age >= 18" | map: "name" | join: ", " }}`, Data: func() M { return M{"users": mdUsers()} }, Want: "Ann, Cat"},
		Example{ID: "reject_exp", Source: X, Tier: "ext", Expect: "derived", Tpl: `{% assign upcoming = events | reject_exp: "e", "e.cancelled" %}{{ upcoming | map: "name" | join: "," }}`, Data: func() M { return M{"events": events()} }, Want: "Launch"},
		// As for find: map over the single result renders nothing
		// (R-LIQ-145, live 2026-09-29). The microdata example reads the
		// result's property instead, so the numeric-string comparison of
		// its predicate stays covered.
		Example{ID: "find_exp", Source: X, Tier: "ext", Expect: "derived", Tpl: `{{ products | find_exp: "p", "p.price < 10" | map: "title" }}`, Data: func() M {
			return M{"products": []any{M{"title": "Mug", "price": 12.5}, M{"title": "Sticker", "price": 3}}}
		}, Want: ""},
		Example{ID: "find_exp/microdata-strings", Source: X, Tier: "ext", Expect: "derived", Tpl: `{% assign p = products | find_exp: "p", "p.price < 10" %}{{ p.title }}`, Data: func() M { return M{"products": products()} }, Want: "Sticker"},
		Example{ID: "find_index_exp", Source: X, Tier: "ext", Expect: "derived", Tpl: `{{ steps | find_index_exp: "s", "s.done == false" }}`, Data: func() M {
			return M{"steps": []any{M{"done": true}, M{"done": false}}}
		}, Want: "1"},
		Example{ID: "has_exp", Source: X, Tier: "ext", Expect: "derived", Tpl: `{% if events | has_exp: "e", "e.starts > now" %}Upcoming events{% endif %}`, Data: func() M {
			return M{"events": events(), "now": "2026-09-28T12:00:00Z"} // `now` is not a documented variable; bound here
		}, Want: "Upcoming events"},
		Example{ID: "group_by_exp", Source: X, Tier: "ext", Expect: "derived", Tpl: `{% assign by_year = orders | group_by_exp: "o", "o.date | date: '%Y'" %}
{% for group in by_year %}
  <h3>{{ group.name }}</h3>
  <ul>{% for order in group.items %}<li>{{ order.ref }}</li>{% endfor %}</ul>
{% endfor %}`, Data: func() M { return M{"orders": orders()} }, Norm: true, Want: `<h3>2025</h3> <ul><li>A1</li><li>A3</li></ul> <h3>2026</h3> <ul><li>A2</li></ul>`},
		Example{ID: "exp/nested", Source: X, Tier: "ext", Expect: "derived", Tpl: `{{ groups | where_exp: "g", "g.items | has_exp: 'i', 'i.active'" | map: "name" | join: "," }}`, Data: func() M {
			return M{"groups": []any{
				M{"name": "g1", "items": []any{M{"active": false}, M{"active": true}}},
				M{"name": "g2", "items": []any{M{"active": false}}},
			}}
		}, Want: "g1"},
		Example{ID: "exp/self-reference-output", Source: X, Tier: "ext", Expect: "derived", Tpl: `{% assign pred = "users | where_exp: 'x', pred" %}<p>{{ users | where_exp: "u", pred }}</p>`, Data: typed, Check: containsAll("<p><span itemscope "+errMarker, "32")},
		Example{ID: "exp/self-reference-assign", Source: X, Tier: "ext", Expect: "derived", Tpl: `{% assign pred = "users | where_exp: 'x', pred" %}{% assign bad = users | where_exp: "u", pred %}never`, Data: typed, Check: wantErr("32")},
	)

	// ---------------- filter errors ----------------
	article := func() M { return M{"article": M{"date": "not a date", "title": "Still here"}} }
	add(
		Example{ID: "error/degrades-locally", Source: F, Tier: "rt", Expect: "derived", Tpl: `<p>Published {{ article.date | date: "%B %d, %Y" }}</p>
<p>{{ article.title }}</p>`, Data: article, Check: containsAll("<p>Published <span itemscope "+errMarker, "<p>Still here</p>")},
		Example{ID: "error/attribute-empty", Source: F, Tier: "rt", Expect: "derived", Tpl: `<a href="{{ article.date | date: "%Y" }}">{{ article.title }}</a>`, Data: article, Want: `<a href="">Still here</a>`},
		Example{ID: "error/control-flow-fails", Source: F, Tier: "rt", Expect: "derived", Tpl: `{% if article.date | date: "%Y" %}x{% endif %}`, Data: article, Check: wantErr("date")},
		Example{ID: "error/predicate-syntax", Source: X, Tier: "rt", Expect: "derived", Tpl: `<p>{{ users | where_exp: "u", "u.name | upcase == 'A'" }}</p><p>after</p>`, Data: typed, Check: containsAll(errMarker, "<p>after</p>")},
		Example{ID: "error/undefined-filter", Source: F, Tier: "rt", Expect: "derived", Tpl: `<p>{{ "x" | no_such_filter }}</p><p>after</p>`, Check: containsAll(errMarker, "<p>after</p>")},
	)

	// ---------------- standard tags ----------------
	add(
		Example{ID: "tag/capture", Source: "shopify", Tier: "std", Expect: "derived", Tpl: `{% capture x %}a{{ 1 | plus: 1 }}{% endcapture %}{{ x }}`, Want: "a2"},
		Example{ID: "tag/case", Source: "shopify", Tier: "std", Expect: "derived", Tpl: `{% case "b" %}{% when "a" %}A{% when "b", "c" %}B{% else %}Z{% endcase %}`, Want: "B"},
		Example{ID: "tag/for-break-continue", Source: "shopify", Tier: "std", Expect: "derived", Tpl: `{% for i in (1..6) %}{% if i == 3 %}{% continue %}{% endif %}{% if i == 5 %}{% break %}{% endif %}{{ forloop.index }}{% endfor %}`, Want: "124"},
		Example{ID: "tag/for-else", Source: "shopify", Tier: "std", Expect: "derived", Tpl: `{% for x in none %}a{% else %}none{% endfor %}`, Want: "none"},
		Example{ID: "tag/for-limit", Source: "build-a-blog", Tier: "std", Expect: "derived", Tpl: `{% for i in (1..9) limit: 4 %}{{ i }}{% endfor %}`, Want: "1234"},
		Example{ID: "tag/for-reversed-limit", Source: "shopify", Tier: "std", Expect: "derived", Tpl: `{% for i in (1..5) reversed limit:2 %}{{ i }}{% endfor %}`, Want: "21"},
		Example{ID: "tag/cycle", Source: "shopify", Tier: "std", Expect: "derived", Tpl: `{% for i in (1..3) %}{% cycle "a", "b" %}{% endfor %}`, Want: "aba"},
		Example{ID: "tag/unless", Source: "shopify", Tier: "std", Expect: "derived", Tpl: `{% unless false %}u{% endunless %}`, Want: "u"},
		Example{ID: "tag/raw", Source: "pagelove-cursor SKILL.md", Tier: "std", Expect: "derived", Tpl: "{% raw %}<script>const t = `${a}`; {{ x }}</script>{% endraw %}", Want: "<script>const t = `${a}`; {{ x }}</script>"},
		Example{ID: "tag/comment", Source: "shopify", Tier: "std", Expect: "derived", Tpl: `a{% comment %}{{ x }}{% endcomment %}b`, Want: "ab"},
		Example{ID: "tag/tablerow", Source: "shopify", Tier: "std", Expect: "derived", Tpl: `{% tablerow i in (1..2) %}{{ i }}{% endtablerow %}`, Want: "<tr class=\"row1\">\n<td class=\"col1\">1</td><td class=\"col2\">2</td></tr>\n"},
		Example{ID: "tag/and-or-contains", Source: "shopify", Tier: "std", Expect: "derived", Tpl: `{% if "abc" contains "b" and 1 < 2 or false %}y{% endif %}`, Want: "y"},
		Example{ID: "tag/blank-empty", Source: "pagelove-shop index.html", Tier: "std", Expect: "derived", Tpl: `{% if s != blank %}A{% endif %}{% if s == empty %}B{% endif %}{% if list == empty %}C{% endif %}{% if ws == blank %}D{% endif %}`, Data: func() M { return M{"s": "", "list": []any{}, "ws": "  "} }, Want: "BCD"},
		Example{ID: "tag/whitespace-control", Source: T, Tier: "std", Expect: "derived", Tpl: "<ul>\n  {%- for i in (1..2) -%}\n  <li>{{- i -}}</li>\n  {%- endfor -%}\n</ul>", Want: "<ul><li>1</li><li>2</li></ul>"},
		Example{ID: "tag/echo-increment", Source: "liquidjs", Tier: "ext", Expect: "derived", Tpl: `{% echo "e" | upcase %}{% increment c %}{% increment c %}`, Want: "E01"},
		Example{ID: "string-escapes", Source: S, Tier: "std", Expect: "derived", Tpl: `{{ "a\"b" }}|{{ 'a\nb' }}`, Want: `a"b|a\nb`},
	)

	// ---------------- templating pages ----------------
	add(
		Example{ID: "templating/users-listing", Source: T, Tier: "app", Expect: "derived",
			Tpl:  "\n    {% assign users = users | sort: 'fullname' %}\n    {%- for user in users -%}\n    <li>\n        <a href=\"{{ user['@id'] }}\">{{ user.fullname }}</a> ({{ user.email }})\n    </li>\n    {%- endfor -%}\n",
			Data: func() M { return M{"users": team()} },
			Want: "\n    <li>\n        <a href=\"/team.html#adam\">Adam Ant</a> (adam@example.com)\n    </li><li>\n        <a href=\"/team.html#zoe\">Zoe Zed</a> (zoe@example.com)\n    </li>"},
		Example{ID: "templating/request-debug", Source: T, Tier: "app", Expect: "derived", Tpl: "\n<pre>{{ request | json: 2 }}</pre>\n", Data: req, Check: func(out string, err error) error {
			if err != nil {
				return err
			}
			body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(out), "<pre>"), "</pre>")
			return jsonCheck(true)(body, nil)
		}},
		Example{ID: "templating/people-listing", Source: T, Tier: "app", Expect: "reconstructed",
			Tpl:  "\n    {% for person in people %}\n    <li>{{ person.name }}</li>\n    {% endfor %}\n  ",
			Data: func() M { return M{"people": anna()} },
			Want: "\n    \n    <li>Anna</li>\n    \n    <li>Ben</li>\n    \n  "},
		Example{ID: "request-document/request-object", Source: "reference/reading-and-writing/Request-Document", Tier: "app", Expect: "reconstructed",
			Tpl: "\n    <p>Method: {{ request.method }}</p>\n    <p>Path: {{ request.path }}</p>\n  ", Data: req,
			Want: "\n    <p>Method: GET</p>\n    <p>Path: /sspi-reqobj-page.html</p>\n  "},
		Example{ID: "resource-binding/contacts", Source: "reference/composing-pages/Resource-Binding", Tier: "app", Expect: "derived",
			Tpl: "<ul>\n        {% for contact in contacts %}\n        <li>{{ contact.name }}</li>\n        {% endfor %}\n    </ul>", Data: func() M { return M{"contacts": anna()} }, Norm: true,
			Want: "<ul> <li>Anna</li> <li>Ben</li> </ul>"},
		Example{ID: "expression-binding/summary", Source: "reference/composing-pages/Expression-Binding", Tier: "app", Expect: "reconstructed",
			Tpl: "\n    <p>Count: {{ count }}</p>\n    <p>Sum: {{ total }}</p>\n  ", Data: func() M { return M{"count": int64(2), "total": 100.0} },
			Want: "\n    <p>Count: 2</p>\n    <p>Sum: 100.0</p>\n  "},
		Example{ID: "expression-binding/totalling", Source: "reference/composing-pages/Expression-Binding", Tier: "app", Expect: "derived",
			Tpl: "<p>{{ count }} products totalling ${{ total }}</p>", Data: func() M { return M{"count": int64(2), "total": 100.0} },
			Want: "<p>2 products totalling $100.0</p>"},
		Example{ID: "expression-binding/scope", Source: "reference/composing-pages/Expression-Binding", Tier: "app", Expect: "derived",
			Tpl: "<p>{{ localcount }} of {{ sitecount }} items</p>", Data: func() M { return M{"localcount": int64(2), "sitecount": int64(5)} },
			Want: "<p>2 of 5 items</p>"},
		Example{ID: "js-binding/total-doubled", Source: "reference/composing-pages/JavaScript-Expression-Binding", Tier: "app", Expect: "documented",
			Tpl: "\n  <li>Total: {{ total }}</li>\n  <li>Doubled: {{ doubled }}</li>\n", Data: func() M { return M{"total": int64(60), "doubled": int64(120)} },
			Want: "\n  <li>Total: 60</li>\n  <li>Doubled: 120</li>\n"},
		Example{ID: "query/composed", Source: "reference/protocol/QUERY", Tier: "app", Expect: "derived", Tpl: `{{ 2 | plus: 3 }}`, Want: "5"},
		Example{ID: "routes/params", Source: "reference/composing-pages/Parameterized-Routes", Tier: "app", Expect: "derived",
			Tpl: `<h1>{{ request.params.slug }}</h1>`, Data: func() M {
				return M{"request": request(false, func(d *RequestData) { d.Params = []Param{{"slug", "hello-world"}} })}
			},
			Want: `<h1>hello-world</h1>`},
	)

	// ---------------- build-a-blog ----------------
	posts := func() M { return M{"posts": blogPosts()} }
	add(
		Example{ID: "blog/index", Source: "learn/build-a-blog", Tier: "app", Expect: "derived", Norm: true, Data: posts,
			Tpl: `
    {% assign recent = posts | where: "status", "Published" | sort: "publishedAt" | reverse %}
    {% for post in recent limit: 4 %}
    <article class="excerpt">
      <h2><a href="/posts/{{ post.slug }}.html">{{ post.title }}</a></h2>
      <p class="byline"><time datetime="{{ post.publishedAt }}">{{ post.displayDate }}</time> · {{ post.author }}</p>
      <p class="lede">{{ post.excerpt }}</p>
    </article>
    {% endfor %}
  `,
			Want: `<article class="excerpt"> <h2><a href="/posts/second-thoughts.html">Second thoughts</a></h2> <p class="byline"><time datetime="2026-08-05">5 August 2026</time> · Sam</p> <p class="lede">Why the blog has no build step & why that's fine.</p> </article> <article class="excerpt"> <h2><a href="/posts/hello-world.html">Hello, world</a></h2> <p class="byline"><time datetime="2026-08-03">3 August 2026</time> · Sam</p> <p class="lede">The first post on Field Notes, and a look at how this blog works.</p> </article>`},
		Example{ID: "blog/archive", Source: "learn/build-a-blog", Tier: "app", Expect: "derived", Norm: true, Data: posts,
			Tpl: `
    {% assign all = posts | where: "status", "Published" | sort: "publishedAt" | reverse %}
    {% if all.size == 0 %}<p>No posts yet.</p>{% endif %}
    {% assign last_m = "" %}
    {% for post in all %}
      {% if post.monthLabel != last_m %}<h2>{{ post.monthLabel }}</h2>{% assign last_m = post.monthLabel %}{% endif %}
      <div class="archive-row"><a href="/posts/{{ post.slug }}.html">{{ post.title }}</a> <time datetime="{{ post.publishedAt }}">{{ post.displayDate }}</time></div>
    {% endfor %}
  `,
			Want: `<h2>August 2026</h2> <div class="archive-row"><a href="/posts/second-thoughts.html">Second thoughts</a> <time datetime="2026-08-05">5 August 2026</time></div> <div class="archive-row"><a href="/posts/hello-world.html">Hello, world</a> <time datetime="2026-08-03">3 August 2026</time></div>`},
		Example{ID: "blog/atom-feed", Source: "learn/build-a-blog", Tier: "app", Expect: "derived", Data: posts,
			Tpl:  `<title>Field Notes</title><link href="https://field-notes.onpagelove.com/"/><link rel="self" href="https://field-notes.onpagelove.com/feed.xml"/><id>https://field-notes.onpagelove.com/</id>{% assign pub = posts | where: "status", "Published" | sort: "publishedAt" | reverse %}{% if pub.size > 0 %}<updated>{{ pub.first.publishedAt }}T00:00:00Z</updated>{% endif %}{% for post in pub %}<entry><title>{{ post.title | escape }}</title><link href="https://field-notes.onpagelove.com/posts/{{ post.slug }}.html"/><id>https://field-notes.onpagelove.com/posts/{{ post.slug }}.html</id><updated>{{ post.publishedAt }}T00:00:00Z</updated><published>{{ post.publishedAt }}T00:00:00Z</published><summary>{{ post.excerpt | escape }}</summary><author><name>{{ post.author | escape }}</name></author></entry>{% endfor %}`,
			Want: `<title>Field Notes</title><link href="https://field-notes.onpagelove.com/"/><link rel="self" href="https://field-notes.onpagelove.com/feed.xml"/><id>https://field-notes.onpagelove.com/</id><updated>2026-08-05T00:00:00Z</updated><entry><title>Second thoughts</title><link href="https://field-notes.onpagelove.com/posts/second-thoughts.html"/><id>https://field-notes.onpagelove.com/posts/second-thoughts.html</id><updated>2026-08-05T00:00:00Z</updated><published>2026-08-05T00:00:00Z</published><summary>Why the blog has no build step &amp; why that&#39;s fine.</summary><author><name>Sam</name></author></entry><entry><title>Hello, world</title><link href="https://field-notes.onpagelove.com/posts/hello-world.html"/><id>https://field-notes.onpagelove.com/posts/hello-world.html</id><updated>2026-08-03T00:00:00Z</updated><published>2026-08-03T00:00:00Z</published><summary>The first post on Field Notes, and a look at how this blog works.</summary><author><name>Sam</name></author></entry>`},
	)

	// ---------------- upstream apps ----------------
	add(
		Example{ID: "polls/index-listing", Source: "pagelove-polls site/index.html", Tier: "app", Expect: "derived", Norm: true,
			Data: func() M { return M{"polls": pollItems()} },
			Tpl: `
          {% assign listed = polls | where: "listed", "true" | sort: "created" | reverse %}
          {% if listed.size == 0 %}
          <p class="empty">No polls yet. Create the first one on the left.</p>
          {% endif %}
          {% for poll in listed limit: 8 %}
          {% assign url = poll['@id'] | split: "#" | first %}
          <a class="poll-card" href="{{ url }}">
            <h3>{{ poll.title | escape }}</h3>
            <p>by {{ poll.organizer | escape }} · {{ poll.optionCount }} dates · <span class="rc" data-src="{{ url }}">…</span> responses</p>
            <time>{{ poll.createdLabel | escape }}</time>
          </a>
          {% endfor %}
        `,
			Want: `<a class="poll-card" href="/polls/kfd47o4zqd.html"> <h3>Team lunch at the new place</h3> <p>by Benji · 3 dates · <span class="rc" data-src="/polls/kfd47o4zqd.html">…</span> responses</p> <time>14 Sept 2026</time> </a>`},
		Example{ID: "polls/new-poll-template", Source: "pagelove-polls site/templates/new-poll.html", Tier: "app", Expect: "derived",
			Tpl: htmlInner(fixture("pagelove-polls/new-poll.html")), Data: func() M {
				form := url.Values{
					"title": {"Team lunch at the new place"}, "organizer": {"Benji"},
					"description": {"Somewhere with outdoor seating. Bring appetite & opinions."},
					"options":     {"2026-09-18T12:30|Fri|18 Sep|12:30;;2026-09-19|Sat|19 Sep|;;2026-09-22T13:00|Mon|22 Sep|13:00"},
					"listed":      {"on"}, "created": {"2026-09-14T21:40:00.000Z"}, "createdLabel": {"14 Sept 2026"},
				}
				return M{"request": request(false, func(d *RequestData) {
					d.Method, d.ContentType, d.Body = "POST", "application/x-www-form-urlencoded", []byte(form.Encode())
				})}
			}, Check: newPollCheck},
		Example{ID: "shop/index", Source: "pagelove-shop site/index.html", Tier: "app", Expect: "derived", Norm: true,
			Data: func() M { return M{"products": shopProducts()} },
			Tpl: `
      {% assign items = products | where: "available", "yes" | sort: "name" %}
      {% for p in items %}
      <a class="card" href="/products/{{ p.slug }}.html">
        {% assign shot = p.image | join: "," | split: "," | first %}
        {% if shot != blank %}<span class="thumb has-img"><img src="{{ shot }}" alt="{{ p.name }}" loading="lazy"></span>
        {% else %}<span class="thumb">{{ p.emoji }}</span>{% endif %}
        <span class="cat">{{ p.category }}</span>
        <h2>{{ p.name }}</h2>
        <span class="price">{{ p.priceDisplay }}</span>
      </a>
      {% endfor %}
    `,
			Want: shopIndexWant()},
		Example{ID: "shop/admin-products", Source: "pagelove-shop site/admin/products.html", Tier: "app", Expect: "derived",
			Data: func() M { return M{"products": shopProducts()} },
			Tpl: `
      {% assign all = products | sort: "name" %}
      {% for p in all %}
      <li class="admin-row" data-slug="{{ p.slug }}" data-name="{{ p.name }}" data-sku="{{ p.sku }}" data-price="{{ p.price }}" data-category="{{ p.category }}" data-emoji="{{ p.emoji }}" data-available="{{ p.available }}" data-description="{{ p.description }}" data-variants="{{ p.variant | join: ',' }}" data-images="{{ p.image | join: ',' }}"></li>
      {% endfor %}
    `,
			Check: containsAll(`data-slug="cap" data-name="Pagelove Cap" data-sku="PL-CAP" data-price="2200"`, `data-variants="One size" data-images=""`, `data-variants="S,M,L,XL,XXL"`)},
		Example{ID: "shop/partials-authed", Source: "pagelove-shop site/partials.html", Tier: "app", Expect: "derived",
			Tpl:  "{% if request.headers.authorization %}<a href=\"/admin/index.html\">Orders</a>\n      <a href=\"/admin/products.html\">Products</a>\n      <a href=\"/admin/settings.html\">Settings</a>{% endif %}",
			Data: func() M { return M{"request": request(true)} },
			Want: "<a href=\"/admin/index.html\">Orders</a>\n      <a href=\"/admin/products.html\">Products</a>\n      <a href=\"/admin/settings.html\">Settings</a>"},
		Example{ID: "shop/partials-anonymous", Source: "pagelove-shop site/partials.html", Tier: "app", Expect: "derived",
			Tpl: "{% if request.headers.authorization %}<a href=\"/admin/index.html\">Orders</a>{% endif %}", Data: req, Want: ""},
		Example{ID: "kanban/whoami", Source: "pagelove-kanban site/app.js", Tier: "app", Expect: "derived",
			Tpl:  `{{ request.auth.username }}|{{ request.auth.claims.name }}|{{ request.auth.claims.email }}|{{ request.auth.claims.picture }}|{% for r in request.auth.role %}{{ r }} {% endfor %}`,
			Data: func() M { return M{"request": request(true)} },
			// The spike had no `role`; the spec exposes it as an alias of
			// `roles` (R-LIQ-61, C-6), which is what the kanban app reads.
			Want: "sub-ada|Ada Lovelace|ada@example.com|https://img.example/ada.png|admin users "},
		Example{ID: "skill/auth-aware-ui", Source: "pagelove-cursor SKILL.md", Tier: "app", Expect: "derived", Norm: true,
			Tpl:  "{% if request.auth.username %}\n  <span>{{ request.auth.claims.name }}</span> |\n  <a href=\"/auth/logout\">Logout</a>\n  {% else %}\n  <a href=\"/auth/login\">Login</a>\n  {% endif %}",
			Data: func() M { return M{"request": request(true)} },
			Want: `<span>Ada Lovelace</span> | <a href="/auth/logout">Logout</a>`},
		Example{ID: "skill/skip-template-items", Source: "pagelove-cursor SKILL.md", Tier: "app", Expect: "derived",
			Tpl: `{% for item in items %}{% unless item['@id'] contains 'request.body' %}[{{ item.name }}]{% endunless %}{% endfor %}`,
			Data: func() M {
				return M{"items": []any{M{"@id": "/r/1.html#r1", "name": "one"}, M{"@id": "/templates/new.html#{{ request.body.slug }}", "name": "{{ request.body.name }}"}}}
			},
			Want: "[one]"},
	)

	// ---------------- runtime: budget, isolation ----------------
	add(
		Example{ID: "budget/empty-loop", Source: T + "#limits", Tier: "rt", Expect: "derived", Tpl: `{% for i in (1..50000000) %}{% endfor %}done`, Check: wantErr("budget")},
		Example{ID: "budget/output", Source: T + "#limits", Tier: "rt", Expect: "derived", Tpl: `{% for i in (1..300000) %}{{ "0123456789012345678901234567890123456789012345678901234567890123456789" }}{% endfor %}`, Check: wantErr("budget")},
		Example{ID: "budget/filter-walk", Source: A, Tier: "rt", Expect: "derived", Tpl: `{% assign r = (1..3000000) | sort | size %}{{ r }}`, Check: wantErr("budget")},
		Example{ID: "isolation/include", Source: T + "#template-scope", Tier: "rt", Expect: "derived", Tpl: `{% include "go.mod" %}`, Check: func(out string, err error) error {
			if err == nil && strings.Contains(out, "module") {
				return errors.New("template read a file from the server's working directory")
			}
			if err == nil {
				return errors.New("include should fail")
			}
			return nil
		}},
	)
	return c
}

func shopIndexWant() string {
	card := func(slug, emoji, cat, name, price string) string {
		return `<a class="card" href="/products/` + slug + `.html"> <span class="thumb">` + emoji + `</span> <span class="cat">` + cat + `</span> <h2>` + name + `</h2> <span class="price">` + price + `</span> </a>`
	}
	return strings.Join([]string{
		card("cap", "🧢", "headwear", "Pagelove Cap", "£22.00"),
		card("hoodie", "🧥", "clothing", "Pagelove Hoodie", "£48.00"),
		card("mug", "☕", "homeware", "Pagelove Mug", "£14.00"),
		card("tee", "👕", "clothing", "Pagelove T-shirt", "£28.00"),
		card("tote", "👜", "bags", "Pagelove Tote", "£16.00"),
		card("stickers", "✨", "stationery", "Sticker Pack", "£6.00"),
	}, " ")
}

// newPollCheck compares fragments of the rendered poll with the poll page the
// real PageLove server generated from the same template (kfd47o4zqd.html).
func newPollCheck(out string, err error) error {
	if err != nil {
		return err
	}
	stored := fixture("pagelove-polls/kfd47o4zqd.html")
	for _, frag := range []string{`(?s)<title>.*?</title>`, `(?s)<header class="poll-head">.*?</header>`, `(?s)<thead>.*?</thead>`, `<article id="poll"[^>]*>`} {
		re := regexp.MustCompile(frag)
		want, got := re.FindString(stored), re.FindString(out)
		if Normalize(want) != Normalize(got) {
			return fmt.Errorf("fragment %s differs:\n got  %q\n want %q", frag, clip(Normalize(got)), clip(Normalize(want)))
		}
	}
	if !regexp.MustCompile(`<base href="/polls/[a-z0-9]{10}\.html">`).MatchString(out) {
		return errors.New("no <base href> with a 10-char lower/digit id")
	}
	return nil
}

// htmlInner returns the content of the <html> element (the template is on <html>).
func htmlInner(doc string) string {
	i := strings.Index(doc, "<html")
	j := strings.Index(doc[i:], ">")
	k := strings.LastIndex(doc, "</html>")
	return doc[i+j+1 : k]
}

func bcryptCheck(cost int) func(string, error) error {
	return func(out string, err error) error {
		if err != nil {
			return err
		}
		if e := bcrypt.CompareHashAndPassword([]byte(out), []byte("password")); e != nil {
			return fmt.Errorf("not a bcrypt hash of 'password': %q", out)
		}
		if c, _ := bcrypt.Cost([]byte(out)); c != cost {
			return fmt.Errorf("cost %d, want %d", c, cost)
		}
		return nil
	}
}

func argonPHC(m, t int) func(string, error) error {
	re := regexp.MustCompile(fmt.Sprintf(`^\$argon2id\$v=19\$m=%d,t=%d,p=1\$([A-Za-z0-9+/]+)\$([A-Za-z0-9+/]+)$`, m, t))
	return func(out string, err error) error {
		if err != nil {
			return err
		}
		if !re.MatchString(out) {
			return fmt.Errorf("not a PHC argon2id string with m=%d,t=%d: %q", m, t, out)
		}
		return nil
	}
}

func randomMins(out string, err error) error {
	if err != nil {
		return err
	}
	count := func(set string) int {
		n := 0
		for _, r := range out {
			if strings.ContainsRune(set, r) {
				n++
			}
		}
		return n
	}
	if len(out) != 24 || count(upperChars) < 3 || count(lowerChars) < 3 || count(digitChars) < 2 || count(upperChars+lowerChars+digitChars) != 24 {
		return fmt.Errorf("random: minimums not met in %q", out)
	}
	return nil
}

func jsonCheck(pretty bool) func(string, error) error {
	return func(out string, err error) error {
		if err != nil {
			return err
		}
		var v map[string]any
		if e := json.Unmarshal([]byte(out), &v); e != nil {
			return fmt.Errorf("not JSON: %v: %q", e, clip(out))
		}
		if v["method"] != "GET" || v["path"] != "/sspi-reqobj-page.html" {
			return fmt.Errorf("unexpected JSON: %q", clip(out))
		}
		if pretty != strings.Contains(out, "\n  \"") {
			return fmt.Errorf("indentation mismatch (pretty=%v): %q", pretty, clip(out))
		}
		return nil
	}
}
