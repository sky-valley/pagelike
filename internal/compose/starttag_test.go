package compose

import (
	"testing"
)

func TestRewriteStartTag(t *testing.T) {
	cases := []struct {
		tag  string
		drop []string
		set  [][2]string
		xml  bool
		want string
	}{
		{`<html lang="en" xmlns:p="https://pagelove.org/1.0" p:template="text/liquid">`, []string{"xmlns:p", "p:template"}, nil, false, `<html lang="en">`},
		// Everything else in the tag stays as written: quoting, order,
		// valueless attributes, references, odd whitespace.
		{"<div  class='a'\n\tr:x=\"[a='b']\" hidden data-q=&amp; e:y=1>", []string{"r:x", "e:y"}, nil, false, "<div  class='a' hidden data-q=&amp;>"},
		{`<P:X a="1" P:Template="text/liquid"/>`, []string{"p:template"}, nil, false, `<P:X a="1"/>`},
		// An unquoted value runs to the '>' (the tokenizer's rule).
		{`<i a=1 p:x=y/>`, []string{"p:x"}, nil, false, `<i a=1>`},
		{`<ul id="cart" p:transient>`, []string{"p:transient"}, nil, false, `<ul id="cart">`},
		// XML names are case-sensitive.
		{`<feed P:template="x" p:template="text/liquid">`, []string{"p:template"}, nil, true, `<feed P:template="x">`},
		{`<article class="rec">`, nil, [][2]string{{"id", "k1"}}, false, `<article class="rec" id="k1">`},
		{`<article id="old" class="rec">`, nil, [][2]string{{"id", "k&1"}}, false, `<article id="k&amp;1" class="rec">`},
		{`<br>`, nil, [][2]string{{"id", "b"}}, false, `<br id="b">`},
	}
	for _, c := range cases {
		e := &tagEdit{set: c.set}
		for _, d := range c.drop {
			e.dropAttr(d)
		}
		if got := rewriteStartTag(c.tag, e, c.xml); got != c.want {
			t.Errorf("rewrite %q:\n got %q\nwant %q", c.tag, got, c.want)
		}
	}
}

func TestScanStartTagValues(t *testing.T) {
	tag := `<a href="/x?a=1&amp;b=2" title='it''s' data-x=y disabled>`
	attrs := scanStartTag(tag)
	want := []struct{ name, val string }{{"href", "/x?a=1&amp;b=2"}, {"title", "it"}, {"'s'", ""}, {"data-x", "y"}, {"disabled", ""}}
	if len(attrs) != len(want) {
		t.Fatalf("got %d attrs: %+v", len(attrs), attrs)
	}
	for i, a := range attrs {
		val := ""
		if a.valStart >= 0 {
			val = tag[a.valStart:a.valEnd]
		}
		if a.name != want[i].name || val != want[i].val {
			t.Errorf("attr %d = %q=%q, want %q=%q", i, a.name, val, want[i].name, want[i].val)
		}
	}
}

func TestRouteMatching(t *testing.T) {
	rt, ok := parseRoute("/orgs/:org_id/teams/:team.html")
	if !ok || rt.literals != 2 {
		t.Fatalf("parse: %+v %v", rt, ok)
	}
	ps, ok := rt.match("/orgs/acme/teams/backend.html")
	if !ok || len(ps) != 2 || ps[0] != (Param{"org_id", "acme"}) || ps[1] != (Param{"team", "backend"}) {
		t.Errorf("match: %v %v", ps, ok)
	}
	for _, miss := range []string{"/orgs/acme/teams/.html", "/orgs/acme/teams/backend.txt", "/orgs/acme/teams", "/orgs/acme/x/backend.html"} {
		if _, ok := rt.match(miss); ok {
			t.Errorf("%s matched", miss)
		}
	}
	if _, ok := parseRoute("/plain/page.html"); ok {
		t.Error("a path without parameters is not a route")
	}
}

// TestPaginateParam: the id-prefixed parameter wins when the paginator has
// an id and the request carries it; otherwise the unprefixed one applies
// (live 2026-09-29, comp.pag.probe-0929.*).
func TestPaginateParam(t *testing.T) {
	cases := []struct {
		raw, id, name string
		want          string
		ok            bool
	}{
		{"paginate:page=2&paginate:length=3", "contacts", "page", "2", true},
		{"paginate:page=2&paginate:length=3", "contacts", "length", "3", true},
		{"paginate:page=2&paginate:contacts:page=3", "contacts", "page", "3", true},
		{"paginate:contacts:page=3&paginate:page=2", "", "page", "2", true},
		{"paginate:users:page=2", "posts", "page", "", false},
		{"paginate%3Aa%3Apage=4", "a", "page", "4", true},
		{"q=smith", "", "page", "", false},
	}
	for _, c := range cases {
		got, ok := paginateParam(rawQueryPairs(c.raw), c.id, c.name)
		if got != c.want || ok != c.ok {
			t.Errorf("paginateParam(%q, %q, %q) = %q, %v; want %q, %v", c.raw, c.id, c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestShiftOffsets(t *testing.T) {
	edits := []edit{{start: 5, end: 5, text: "LINK"}, {start: 10, end: 20, text: ""}, {start: 30, end: 32, text: "abcd"}}
	cases := []struct {
		off, want int
		ok        bool
	}{{0, 0, true}, {5, 9, true}, {9, 13, true}, {10, 0, false}, {19, 0, false}, {20, 14, true}, {31, 0, false}, {40, 36, true}}
	for _, c := range cases {
		got, ok := shift(c.off, edits)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("shift(%d) = %d,%v want %d,%v", c.off, got, ok, c.want, c.ok)
		}
	}
}
