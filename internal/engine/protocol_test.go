package engine

import (
	"strings"
	"testing"
)

func TestPreconditionTags(t *testing.T) {
	const cur = `"abc"`
	for _, c := range []struct {
		header        string
		exists        bool
		match, noneOK bool // IfMatch result; IfNoneMatch result
	}{
		{`"abc"`, true, true, true},
		{`abc`, true, true, true}, // unquoted tags compare as if quoted (R-RW-85)
		{`W/"abc"`, true, false, true},
		{`"x", "abc"`, true, true, true},
		{`"x,y", "abc"`, true, true, true}, // a comma inside a tag does not split it
		{`"x,abc"`, true, false, false},
		{`*`, true, true, true},
		{`*`, false, false, false},
		{`"abc"`, false, false, false},
		{`"nope"`, true, false, false},
	} {
		if got := IfMatch(c.header, c.exists, cur); got != c.match {
			t.Errorf("IfMatch(%s, exists=%v) = %v, want %v", c.header, c.exists, got, c.match)
		}
		if got := IfNoneMatch(c.header, c.exists, cur); got != c.noneOK {
			t.Errorf("IfNoneMatch(%s, exists=%v) = %v, want %v", c.header, c.exists, got, c.noneOK)
		}
	}
	// A weak current tag never satisfies If-Match (strong comparison).
	if IfMatch(`"abc"`, true, `W/"abc"`) {
		t.Error("If-Match matched a weak current tag")
	}
}

// Tag shapes as PageLove forms them (live 2026-09-28).
func TestTagShapes(t *testing.T) {
	composed := ComposedETag([]byte("<html>composed</html>"))
	if !strings.HasPrefix(composed, `W/"`) || len(composed) != 64+4 {
		t.Fatalf("composed tag %s is not a weak content hash", composed)
	}
	if IfMatch(composed, true, composed) {
		t.Error("a weak composed tag satisfied If-Match (strong comparison)")
	}
	// "<h>-<h>-<version>": sha256 of the fragment twice, then the version.
	const li = "<li>two</li>" // sha256 observed as the tag of this POST body
	want := `"6de25f78336cb76acda5abdabd9849c8b091de4f14d8075637c260310542c97b-6de25f78336cb76acda5abdabd9849c8b091de4f14d8075637c260310542c97b-2"`
	if got := FragmentETag(li, 2); got != want {
		t.Errorf("FragmentETag = %s, want %s", got, want)
	}
	if EmptyETag != `"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"` {
		t.Errorf("EmptyETag = %s", EmptyETag)
	}
	if got := NewBoundary(); len(got) != len("boundary")+32 || !strings.HasPrefix(got, "boundary") {
		t.Errorf("boundary %q", got)
	}
}

func TestParseRange(t *testing.T) {
	for _, c := range []struct {
		in, unit, sel, placement string
	}{
		{"selector=h1", "selector", "h1", ""},
		{"  selector=main > h1  ", "selector", "main > h1", ""},
		{"Selector=h1", "selector", "h1", ""},
		{"selector=ul; placement=prepend", "selector", "ul", "prepend"},
		{"selector=ul ;placement=AFTER", "selector", "ul", "after"},
		{"selector=ul; Placement = before ", "selector", "ul", "before"},
		{`selector=[title="a;b"]`, "selector", `[title="a;b"]`, ""},
		{`selector=[title="a; placement=x"] li; placement=append`, "selector", `[title="a; placement=x"] li`, "append"},
		{"selector=ul; placement=in-side", "selector", "ul; placement=in-side", ""},
		{"selector=", "selector", "", ""},
		{"sessel=${h1}.count()", "sessel", "", ""},
		{"bytes=0-3", "bytes", "", ""},
		{"garbage", "", "", ""},
		{"", "", "", ""},
	} {
		r := ParseRange(c.in)
		if r.Unit != c.unit || r.Selector != c.sel || r.Placement != c.placement {
			t.Errorf("ParseRange(%q) = unit %q selector %q placement %q; want %q %q %q", c.in, r.Unit, r.Selector, r.Placement, c.unit, c.sel, c.placement)
		}
	}
	if r := ParseRange("entries=3-10"); r.Start != 3 || r.End != 10 {
		t.Errorf("entries: %+v", r)
	}
	if r := ParseRange("entries=2-"); r.Start != 2 || r.End != -1 {
		t.Errorf("open entries: %+v", r)
	}
	if _, err := ParseRange("selector=").CheckSelector(); err == nil || !strings.HasPrefix(err.Error(), "400") {
		t.Errorf("empty selector: %v, want 400", err)
	}
	if _, err := ParseRange("selector=h1[").CheckSelector(); err == nil || !strings.HasPrefix(err.Error(), "422") {
		t.Errorf("unparsable selector: %v, want 422", err)
	}
}

func TestNegotiate(t *testing.T) {
	for _, c := range []struct {
		accept   string
		selector bool
		want     Representation
	}{
		{"", false, RepHTML},
		{"application/ld+json", false, RepJSONLD},
		{"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", false, RepHTML},
		{"*/*", false, RepHTML},
		{"application/json", false, RepHTML},
		{"image/png", false, RepHTML},
		{"text/html;q=0.5, application/ld+json;q=0.9", false, RepJSONLD},
		{"application/ld+json;q=0", false, RepHTML},
		{"application/*", false, RepJSONLD}, // HTML is not acceptable at all
		{"text/*, application/*", false, RepHTML},
		{"text/html, application/ld+json", false, RepHTML},
		{"multipart/mixed", false, RepHTML}, // multipart needs a selector range
		{"multipart/mixed", true, RepMultipart},
		{"multipart/*", true, RepHTML}, // only an explicit multipart/mixed
		{"multipart/mixed;q=0", true, RepHTML},
		{"text/html, multipart/mixed", true, RepHTML},
		{"multipart/mixed, text/html;q=0.5", true, RepMultipart},
		{"application/ld+json", true, RepJSONLD},
	} {
		if got := Negotiate(c.accept, c.selector); got != c.want {
			t.Errorf("Negotiate(%q, %v) = %v, want %v", c.accept, c.selector, got, c.want)
		}
	}
}

func TestAcceptQuality(t *testing.T) {
	for _, c := range []struct {
		accept, typ string
		explicit    bool
		want        float64
	}{
		{"", "text/html", false, 1},
		{"", "text/event-stream", true, 0},
		{"text/event-stream", "text/event-stream", true, 1},
		{"TEXT/Event-Stream; charset=utf-8", "text/event-stream", true, 1},
		{"text/event-stream;q=0", "text/event-stream", true, 0},
		{"*/*", "text/event-stream", true, 0},
		{"text/*;q=0.3, */*;q=0.9", "text/html", false, 0.3}, // most specific range wins
		{"text/html;q=0.2, text/html;q=0.7", "text/html", false, 0.7},
		{"text/html;q=bogus", "text/html", false, 0},
	} {
		if got := AcceptQuality(c.accept, c.typ, c.explicit); got != c.want {
			t.Errorf("AcceptQuality(%q, %q, %v) = %v, want %v", c.accept, c.typ, c.explicit, got, c.want)
		}
	}
}

func TestMultipartWireFormat(t *testing.T) {
	body, ctype := MultipartBody([]Part{
		{Header: [][2]string{{"Content-Type", "text/html"}, {"Content-Range", "selector #a"}}, Body: "<p id=\"a\">x</p>"},
		{Header: [][2]string{{"Allow", "GET, OPTIONS"}}},
	})
	b := strings.TrimPrefix(ctype, "multipart/mixed; boundary=")
	if b == ctype || strings.ContainsAny(b, `"; `) {
		t.Fatalf("boundary must be the last, unquoted parameter: %q", ctype)
	}
	// PageLove's framing (live 2026-09-28): a body-less part ends at its
	// header block, and a data answer ends at the close delimiter.
	want := "--" + b + "\r\nContent-Type: text/html\r\nContent-Range: selector #a\r\n\r\n<p id=\"a\">x</p>\r\n" +
		"--" + b + "\r\nAllow: GET, OPTIONS\r\n\r\n--" + b + "--"
	if string(body) != want {
		t.Errorf("multipart:\n got %q\nwant %q", body, want)
	}
	// An OPTIONS answer (header-only parts) ends with a line break.
	opts, ct := MultipartBody([]Part{{Header: [][2]string{{"Allow", "GET, OPTIONS"}}}})
	ob := strings.TrimPrefix(ct, "multipart/mixed; boundary=")
	if string(opts) != "--"+ob+"\r\nAllow: GET, OPTIONS\r\n\r\n--"+ob+"--\r\n" {
		t.Errorf("OPTIONS multipart: %q", opts)
	}
	if empty, ct := MultipartBody(nil); string(empty) != "--"+strings.TrimPrefix(ct, "multipart/mixed; boundary=")+"--" {
		t.Errorf("zero parts: %q", empty)
	}
}
