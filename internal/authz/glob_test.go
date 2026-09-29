package authz

import "testing"

func TestGlobMatch(t *testing.T) {
	for _, tc := range []struct {
		pat, path string
		want      bool
	}{
		{"/x/*", "/x/", true},
		{"/x/*", "/x/a", true},
		{"/x/*", "/x/a/b/c.html", true},
		{"/x/*", "/x", false},
		{"/*", "/anything/at/all.html", true},
		{"/x/**", "/x/a/b", true},
		{"/f?.html", "/f1.html", true},
		{"/f?.html", "/f10.html", false},
		{"/f?.html", "/fé.html", true}, // one character, not one byte
		{"/[ab].html", "/a.html", true},
		{"/[ab].html", "/c.html", false},
		{"/[!ab].html", "/c.html", true},
		{"/[^ab].html", "/a.html", false},
		{"/[a-c]x", "/bx", true},
		{"/[]]x", "/]x", true},
		{"/{one,two}.html", "/one.html", true},
		{"/{one,two}.html", "/two.html", true},
		{"/{one,two}.html", "/three.html", false},
		{"/{a,b}/{c,d}", "/b/c", true},
		{"/{a,*}.html", "/zzz.html", true},
		{`/a\*b`, "/a*b", true},
		{`/a\*b`, "/axb", false},
		{"/CaSe", "/case", false},
		// Invalid patterns are literal text.
		{"/[ab.html", "/[ab.html", true},
		{"/[ab.html", "/a", false},
		{"/{a,b.html", "/{a,b.html", true},
		{"/{a,{b,c}}", "/{a,{b,c}}", true},
		{"/{a,{b,c}}", "/b", false},
		// Liquid braces are literal.
		{"/u/{{ user }}/*", "/u/{{ user }}/*", true},
		{"/u/{{ user }}/*", "/u/x/y", false},
		// Many stars stay linear.
		{"/*a*a*a*a*a*a*a*a*b", "/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
	} {
		if got := GlobMatch(tc.pat, tc.path); got != tc.want {
			t.Errorf("GlobMatch(%q, %q) = %v, want %v", tc.pat, tc.path, got, tc.want)
		}
	}
}

func TestGlobQuote(t *testing.T) {
	for _, s := range []string{"*", "a?b", "[x]", "{a,b}", `back\slash`, "plain/slash"} {
		if !GlobMatch(GlobQuote(s), s) {
			t.Errorf("quoted %q does not match itself", s)
		}
	}
	if GlobMatch(GlobQuote("*"), "anything") {
		t.Error("quoted * matched a non-star path")
	}
}
