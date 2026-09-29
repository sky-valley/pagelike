package liquid

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Filter semantics from docs/spec/liquid.md §11–§18 beyond the docs'
// examples (which the corpus covers).

func TestStringFilters(t *testing.T) {
	item := itemsFromHTML("/i.html", `<p itemscope itemtype="t"> Hi <b>there</b> </p>`, "t")[0]
	runSpecCases(t, []specCase{
		{"coerce-nil", `[{{ nosuch | upcase }}]`, nil, "[]"},
		{"coerce-float", `{{ 2.0 | append: "!" }}`, nil, "2.0!"},
		{"coerce-array", `{{ a | upcase }}`, M{"a": []any{"x", "y"}}, "XY"},
		{"coerce-item", `[{{ i | downcase }}]`, M{"i": item}, "[hi there]"},
		{"capitalize-unicode", `{{ "éCOLE" | capitalize }}`, nil, "École"},
		{"strip-unicode", `[{{ s | strip }}]`, M{"s": "  x  "}, "[x]"},
		{"newline_to_br-crlf", `{{ s | newline_to_br }}`, M{"s": "a\r\nb"}, "a<br />\nb"},
		{"url_encode-star", `{{ "a*b~c é" | url_encode }}`, nil, "a*b~c+%C3%A9"},
		{"uri_escape", `{{ "a b%[x]" | uri_escape }}`, nil, "a%20b%25[x]"},
		{"url_decode-malformed", `{{ "100%+sure%zz" | url_decode }}`, nil, "100% sure%zz"},
		{"strip_html-style", `{{ s | strip_html }}`, M{"s": "<STYLE>p{}</STYLE><!-- c -->a &amp; <i>b</i>"}, "a &amp; b"},
		{"replace-default", `{{ "a-b" | replace: "-" }}`, nil, "ab"},
		{"replace-empty", `{{ "ab" | replace: "", "x" }}`, nil, "ab"},
		{"remove_last", `{{ "a-b-c" | remove_last: "-" }}`, nil, "a-bc"},
		{"sentence-one-two", `{{ a | array_to_sentence_string }}|{{ b | array_to_sentence_string }}`, M{"a": []any{"x"}, "b": []any{"x", "y"}}, "x|x and y"},
		{"slice-codepoints", `{{ "héllo" | slice: 1, 2 }}`, nil, "él"},
		{"slice-out-of-range", `[{{ "abc" | slice: 5 }}][{{ "abc" | slice: -5, 2 }}]`, nil, "[][]"},
		{"slice-clamped", `{{ "abc" | slice: 1, 99 }}`, nil, "bc"},
		{"slice-array", `{{ a | slice: -2, 5 | join: "," }}`, M{"a": []any{1, 2, 3}}, "2,3"},
		{"split-trailing-empties", `{{ "a,,b," | split: "," | size }}`, nil, "4"},
		{"split-empty-sep", `{{ "héj" | split: "" | join: "-" }}`, nil, "h-é-j"},
		{"truncate-default", `{{ s | truncate }}`, M{"s": strings.Repeat("x", 60)}, strings.Repeat("x", 47) + "..."},
		{"truncate-suffix-longer", `{{ "abcdef" | truncate: 2, "....." }}`, nil, "....."},
		{"truncatewords-zero", `{{ "a b c" | truncatewords: 0 }}`, nil, "a..."},
		{"truncatewords-short", `{{ "a  b" | truncatewords: 5 }}`, nil, "a  b"},
		{"size-number", `{{ 12345 | size }}`, nil, "0"},
		{"default-falsy", `{{ nil | default: 1 }}{{ false | default: 2 }}{{ "" | default: 3 }}{{ e | default: 4 }}{{ h | default: 5 }}`, M{"e": []any{}, "h": NewHash()}, "12345"},
		{"default-kept", `[{{ " " | default: 1 }}][{{ 0 | default: 1 }}]`, nil, "[ ][0]"},
		{"default-allow-false", `{{ false | default: 1, allow_false: true }}`, nil, "false"},
		{"slugify-unicode", `{{ "Crème Brûlée!!" | slugify }}`, nil, "crème-brûlée"},
		{"base64", `{{ "hé?" | base64_encode }}|{{ "aMOpPw==" | base64_decode }}|{{ "?>" | base64_url_safe_encode }}|{{ "Pz4" | base64_url_safe_decode }}`, nil, "aMOpPw==|hé?|Pz4=|?>"},
		{"xml_escape-again", `{{ "'" | xml_escape | escape }}`, nil, "&amp;#39;"}, // escape is not idempotent (live 2026-09-29)
	})
}

func TestNumberFilters(t *testing.T) {
	runSpecCases(t, []specCase{
		{"numeric-string-int", `{{ "41" | plus: 1 }}`, nil, "42"},
		{"numeric-string-float", `{{ "1.5" | plus: 1 }}`, nil, "2.5"},
		{"non-numeric-zero", `{{ "abc" | plus: 1 }}|{{ nil | times: 3 }}|{{ true | plus: 1 }}`, nil, "1|0|1"},
		{"modulo-sign", `{{ -7 | modulo: 3 }}|{{ 7 | modulo: -3 }}|{{ 7.5 | modulo: 2 }}`, nil, "2|-2|1.5"},
		{"divided-by-zero-float", `{{ 7 | divided_by: 0.0 }}`, nil, "7"},
		{"divided-by-float", `{{ 7.0 | divided_by: 2 }}`, nil, "3.5"},
		{"abs-types", `{{ -2.5 | abs }}|{{ "-3" | abs }}`, nil, "2.5|3"},
		{"ceil-floor-int", `{{ -3.2 | ceil }}|{{ -3.2 | floor }}|{{ "4.1" | ceil }}`, nil, "-3|-4|5"},
		{"round-half-away", `{{ 2.5 | round }}|{{ -2.5 | round }}`, nil, "3|-3"},
		{"round-negative-places", `{{ 1234.5 | round: -2 }}`, nil, "1200"},
		{"round-places-float", `{{ 2.0 | round: 2 }}|{{ 3 | round: 2 }}`, nil, "2.0|3"},
		{"at-least-types", `{{ 2 | at_least: 2.5 }}|{{ "9" | at_most: 3 }}`, nil, "2.5|3"},
		{"to_integer", `{{ -3.9 | to_integer }}|{{ "-3.9" | to_integer }}|{{ "x" | to_integer }}|{{ "-1e30" | to_integer }}`, nil, "-3|-3|0|-9223372036854775808"},
	})
	e := newTestEngine()
	out, err := e.Render(context.Background(), `{{ 9223372036854775807 | plus: 1 }}`, nil, nil)
	if err != nil || !strings.Contains(out, "integer overflow") {
		t.Errorf("overflow: %q %v", out, err)
	}
}

func TestArrayFilters(t *testing.T) {
	multi := itemsFromHTML("/m.html", `<div>
<p itemscope itemtype="t" id="a"><meta itemprop="tag" content="z"><meta itemprop="tag" content="a"><meta itemprop="n" content="1"></p>
<p itemscope itemtype="t" id="b"><meta itemprop="tag" content="m"><meta itemprop="n" content="2"></p>
</div>`, "t")
	runSpecCases(t, []specCase{
		{"first-last-coerce", `{{ 5 | first }}|{{ nil | first }}|{{ "" | last }}|{{ (3..7) | last }}`, nil, "5|||7"},
		{"reverse-new", `{{ a | reverse | join }}{{ a | join }}`, M{"a": []any{1, 2}}, "2 11 2"},
		{"sort-groups", `{{ a | sort | join: "," }}`, M{"a": []any{true, "b", 2, "10", "B", false, nil, 1.5}}, "1.5,2,10,B,b,false,true,"},
		{"sort-stable-missing", `{{ a | sort: "k" | map: "n" | join }}`, M{"a": []any{M{"n": 1}, M{"n": 2, "k": 1}, M{"n": 3}}}, "2 1 3"},
		{"sort-multi-first", `{{ ps | sort: "tag" | map: "n" | join }}`, M{"ps": multi}, "2 1"},
		{"sort_natural-missing", `{{ a | sort_natural: "k" | map: "n" | join }}`, M{"a": []any{M{"n": 1}, M{"n": 2, "k": "b"}, M{"n": 3, "k": "A"}}}, "3 2 1"},
		{"uniq-strict", `{{ a | uniq | size }}`, M{"a": []any{1, "1", 1, 1.0}}, "2"},
		{"uniq-field", `{{ a | uniq: "k" | map: "n" | join }}`, M{"a": []any{M{"n": 1, "k": "x"}, M{"n": 2, "k": "x"}, M{"n": 3}}}, "1 3"},
		{"compact-field", `{{ a | compact: "k" | map: "n" | join }}`, M{"a": []any{M{"n": 1, "k": "x"}, M{"n": 2}}}, "1"},
		{"map-non-object", `{{ a | map: "k" | size }}`, M{"a": []any{1, "x"}}, "2"},
		{"map-multi", `{{ ps | map: "tag" | join: "," }}`, M{"ps": multi}, "za,m"},
		// map takes arrays (and ranges) only: a single item, hash, scalar or
		// nil gives [] (live 2026-09-29, R-LIQ-145).
		{"map-single-item", `{% assign m = p | map: "n" %}{% if m == empty %}empty{% endif %}:{{ m.size }}:{{ m | json }}`, M{"p": multi[0]}, "empty:0:[]"},
		{"map-single-hash", `[{{ h | map: "name" }}]{{ h | map: "name" | size }}`, M{"h": M{"name": "x"}}, "[]0"},
		{"map-scalars", `{{ "abc" | map: "name" | size }}{{ 5 | map: "name" | size }}{{ nosuch | map: "name" | size }}`, nil, "000"},
		{"map-range", `{{ (1..3) | map: "x" | size }}`, nil, "3"},
		{"find-then-map", `[{{ ps | find: "n", 2 | map: "tag" }}]{% assign f = ps | find: "n", 2 %}{{ f.tag }}`, M{"ps": multi}, "[]m"},
		{"join-nil", `{{ a | join: "," }}`, M{"a": []any{"a", nil, "b"}}, "a,,b"},
		{"concat-scalar", `{{ a | concat: 3 | join }}`, M{"a": []any{1}}, "1 3"},
		{"where-number", `{{ ps | where: "n", 2 | size }}`, M{"ps": multi}, "1"},
		{"where-multi-scalar", `{{ ps | where: "tag", "m" | size }}{{ ps | where: "tag", "z" | size }}`, M{"ps": multi}, "10"},
		{"where-non-object", `{{ a | where: "k", nil | size }}`, M{"a": []any{nil, 1, "x"}}, "0"},
		{"find_index-none", `[{{ ps | find_index: "n", 9 }}]`, M{"ps": multi}, "[]"},
		{"has-false", `{{ ps | has: "n", 9 }}`, M{"ps": multi}, "false"},
		{"group_by-nil", `{% for g in a | group_by: "k" %}{{ g.name }}:{{ g.items | size }};{% endfor %}`, M{"a": []any{M{"k": "x"}, M{}, M{"k": "x"}}}, "x:2;:1;"},
		{"group_by-json", `{{ a | group_by: "k" | json }}`, M{"a": []any{M{"k": 1}}}, `[{"name":1,"items":[{"k":1}]}]`},
		{"sum-floats", `{{ a | sum }}|{{ b | sum }}|{{ c | sum }}`, M{"a": []any{1.5, 1.5}, "b": []any{0.1, 0.2}, "c": []any{}}, "3|0.30000000000000004|0"},
		{"push-nil", `{{ nosuch | push: 1 | size }}`, nil, "1"},
		{"push-scalar", `{{ "a" | push: "b" | join }}`, nil, "a b"},
		{"pop-empty", `{{ nosuch | pop | size }}`, nil, "0"},
	})
}

func TestDateFilters(t *testing.T) {
	runSpecCases(t, []specCase{
		{"offset-hhmm", `{{ "2026-04-12T13:45:00+0200" | date: "%H:%M" }}`, nil, "11:45"},
		{"negative-offset", `{{ "2026-04-12T23:45-05:00" | date: "%d %H:%M" }}`, nil, "13 04:45"},
		{"space-separator", `{{ "2026-04-12 13:45" | date: "%H:%M" }}`, nil, "13:45"},
		{"fraction-truncated", `{{ "2026-09-14T21:40:00.999Z" | date: "%S" }}`, nil, "00"},
		{"unix-negative", `{{ "-86400" | date }}`, nil, "1969-12-31"},
		{"unix-float", `{{ 1767225600.9 | unix_to_iso }}`, nil, "2026-01-01T00:00:00Z"},
		{"empty-input", `[{{ nil | date }}][{{ "" | date_to_string }}]`, nil, "[][]"},
		{"directives", `{{ "2026-04-12T15:04:05Z" | date: "%p %Z %z %y %-d %%%" }}`, nil, "PM UTC +0000 %y %-d %%"},
		{"date_add-negative-string", `{{ "2026-01-01" | date_add: "-60" }}`, nil, "2025-12-31T23:59:00Z"},
		{"unix_to_iso-string", `{{ "2026-01-01T02:00:00+02:00" | unix_to_iso }}`, nil, "2026-01-01T00:00:00Z"},
		{"extra-args-ignored", `{{ "2026-07-07" | date_to_string: "x", 1 }}`, nil, "07 Jul 2026"},
		{"now-shared", `{{ "now" | date: "%H:%M:%S" }}`, nil, "12:00:00"},
	})
	e := newTestEngine()
	for _, tpl := range []string{`{{ "2026-02-30" | date }}`, `{{ "2026-04-12T25:00" | date }}`, `{{ i | date }}`, `{{ "x" | date_add: "y" }}`} {
		out, err := e.Render(context.Background(), tpl, M{"i": people()[0]}, nil)
		if err != nil || !strings.Contains(out, "pagelove.org/Error") {
			t.Errorf("%s: want an inline error, got %q %v", tpl, out, err)
		}
	}
}

func TestSecurityAndRandomFilters(t *testing.T) {
	e := New(Options{Rand: bytes.NewReader(bytes.Repeat([]byte{7, 1, 200, 99, 3, 250, 42, 17}, 4096))})
	render := func(tpl string) string {
		out, err := e.Render(context.Background(), tpl, nil, nil)
		if err != nil {
			t.Fatalf("%s: %v", tpl, err)
		}
		return out
	}
	if out := render(`{{ "pw" | bcrypt: 4 }}`); !regexp.MustCompile(`^\$2b\$04\$.{53}$`).MatchString(out) {
		t.Errorf("bcrypt: %q", out)
	}
	for _, tpl := range []string{
		`{{ "pw" | bcrypt: 3 }}`, `{{ "pw" | bcrypt: 17 }}`, `{{ "pw" | bcrypt: "x" }}`,
		`{{ s | bcrypt: 4 }}`,
		`{{ "pw" | argon2: memory: 4 }}`, `{{ "pw" | argon2: time: 11 }}`, `{{ "pw" | argon2: length: 65 }}`,
		`{{ "pw" | argon2: memory: 262145 }}`, `{{ "pw" | argon2: format: "x" }}`,
		`{{ "pw" | argon2: format: "raw", salt: "short" }}`, `{{ "pw" | argon2: pepper: 1 }}`,
		`{{ 0 | random }}`, `{{ 4097 | random }}`, `{{ "x" | random }}`, `{{ 2 | random: upper: 2, lower: 1 }}`,
		`{{ 4 | random: upper: -1 }}`, `{{ 4 | random: upper: false, lower: false, digits: false }}`,
	} {
		out, err := e.Render(context.Background(), tpl, M{"s": strings.Repeat("x", 73)}, nil)
		if err != nil || !strings.Contains(out, "pagelove.org/Error") {
			t.Errorf("%s: want an inline error, got %q %v", tpl, out, err)
		}
	}
	for tpl, re := range map[string]string{
		`{{ 12 | random: symbols: 12 }}`:                      `^[!@#$%^&*()\-_=+\[\]{};:,.<>?]{12}$`,
		`{{ 10 | random: lower: true, digits: true }}`:        `^[a-z0-9]{10}$`,
		`{{ 10 | random: upper: false }}`:                     `^[a-z0-9]{10}$`,
		`{{ "8" | random: chars: "ab", chars_min: 8 }}`:       `^[ab]{8}$`,
		`{{ 6 | random: chars: "éé" }}`:                       `^é{6}$`,
		`{{ 20 | random: url_safe: true, upper: 5 }}`:         `^[A-Za-z0-9_-]{20}$`,
		`{{ 20 | diceware }}`:                                 `^[a-z]+(-[a-z]+){9}$`,
		`{{ nil | diceware }}`:                                `^[a-z]+-[a-z]+-[a-z]+$`,
		`{{ "pw" | argon2: memory: 64, time: 1, length: 4 }}`: `^\$argon2id\$v=19\$m=64,t=1,p=1\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{6}$`,
	} {
		if out := render(tpl); !regexp.MustCompile(re).MatchString(out) {
			t.Errorf("%s: %q does not match %s", tpl, out, re)
		}
	}
	if out := render(`{{ 30 | random: upper: 10, digits: 10 }}`); strings.Count(out, "") != 31 ||
		len(regexp.MustCompile(`[A-Z]`).FindAllString(out, -1)) < 10 || len(regexp.MustCompile(`[0-9]`).FindAllString(out, -1)) < 10 {
		t.Errorf("random minima: %q", out)
	}
	if n := len(defaultWordList); n < 1600 {
		t.Errorf("diceware list has %d words, need at least 1600", n)
	}
	seen := map[string]bool{}
	for _, w := range defaultWordList {
		if seen[w] || !regexp.MustCompile(`^[a-z]+$`).MatchString(w) {
			t.Errorf("bad or duplicate word %q", w)
		}
		seen[w] = true
	}
}

func TestJSON(t *testing.T) {
	item := itemsFromHTML("/p.html", `<div itemscope itemtype="t" id="p"><span itemprop="name">A</span><div itemprop="owner" itemscope><span itemprop="name">B</span></div><meta itemprop="tag" content="x"><meta itemprop="tag" content="y"></div>`, "t")[0]
	runSpecCases(t, []specCase{
		{"compact", `{{ h | json }}`, M{"h": NewHash().Set("z", 1.0).Set("a", []any{nil, true, "<&>"})}, `{"z":1.0,"a":[null,true,"<&>"]}`},
		{"indent", `{{ h | json: 2 }}`, M{"h": NewHash().Set("a", []any{1})}, "{\n  \"a\": [\n    1\n  ]\n}"},
		{"map-sorted", `{{ m | jsonify }}`, M{"m": M{"b": 1, "a": 2}}, `{"a":2,"b":1}`},
		{"item", `{{ i | json }}`, M{"i": item}, `{"@id":"/p.html#p","@type":"t","name":"A","owner":{"@id":"/p.html","name":"B"},"tag":["x","y"]}`},
		{"inspect", `{{ "a\"b" | inspect }}`, nil, `"a\"b"`},
		{"range", `{{ (1..3) | json }}`, nil, `[1,2,3]`},
		{"control-chars", `{{ s | json }}`, M{"s": "a\tb\u2028\x01"}, "\"a\\tb\\u2028\\u0001\""},
	})
	v, err := DecodeJSON([]byte(`{"b":60,"a":[1.5,2.0,null],"c":{"x":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	h := v.(*Hash)
	if got := strings.Join(h.Keys(), ","); got != "b,a,c" {
		t.Errorf("DecodeJSON order: %s", got)
	}
	if b, _ := h.Get("b"); b != 60 {
		t.Errorf("DecodeJSON integer: %#v", b)
	}
	out, _ := newTestEngine().Render(context.Background(), `{{ v | json }}|{{ v.b }}`, M{"v": v}, nil)
	if out != `{"b":60,"a":[1.5,2,null],"c":{"x":true}}|60` {
		t.Errorf("DecodeJSON round trip: %s", out)
	}
	if _, err := DecodeJSON([]byte(`{} {}`)); err == nil {
		t.Error("DecodeJSON accepted trailing data")
	}
}

func TestNowIsPerRequest(t *testing.T) {
	calls := 0
	clock := func() time.Time { calls++; return FixedNow.Add(time.Duration(calls) * time.Hour) }
	e := New(Options{Now: clock})
	b := NewBudget(Limits{})
	a1, _ := e.Render(context.Background(), `{{ "now" | date: "%H" }}`, nil, b)
	a2, _ := e.Render(context.Background(), `{{ "now" | date: "%H" }}`, nil, b)
	c, _ := e.Render(context.Background(), `{{ "now" | date: "%H" }}`, nil, nil)
	if a1 != a2 || a1 == c {
		t.Errorf("one instant per request: %s %s, another request %s", a1, a2, c)
	}
}
