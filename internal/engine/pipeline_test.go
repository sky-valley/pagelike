package engine_test

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/store"
)

// rules renders AuthorizationRule rows: actor, resource, methods, selector,
// action.
func rules(rows ...[5]string) string {
	var b strings.Builder
	b.WriteString("<html><body><table><tbody>")
	for _, r := range rows {
		b.WriteString(`<tr itemscope itemtype="https://pagelove.org/AuthorizationRule"><td><span itemprop="actor">` + r[0] +
			`</span></td><td><span itemprop="resource">` + r[1] + `</span></td><td>`)
		for _, m := range strings.Split(r[2], ",") {
			b.WriteString(`<span itemprop="method">` + m + `</span>`)
		}
		b.WriteString(`</td><td itemprop="selector">` + r[3] + `</td><td itemprop="action">` + r[4] + "</td></tr>")
	}
	b.WriteString("</tbody></table></body></html>")
	return b.String()
}

var anon = &identity.Principal{Session: "anon-session"}

// public runs a public-plane write as an anonymous visitor.
func (f *fixture) public(op *engine.Op) (*engine.Result, *errdoc.Error) {
	f.t.Helper()
	op.Plane = engine.Public
	if op.Principal == nil {
		op.Principal = anon
	}
	res, err := f.e.Write(f.ctx, f.s, op)
	if err == nil {
		return res, nil
	}
	var e *errdoc.Error
	if !errors.As(err, &e) {
		f.t.Fatalf("%s %s: %v", op.Method, op.Path, err)
	}
	return nil, e
}

func (f *fixture) status(op *engine.Op) int {
	f.t.Helper()
	res, e := f.public(op)
	if e != nil {
		return e.Status
	}
	return res.Status
}

func sel(method, path, rng, body string) *engine.Op {
	return &engine.Op{Method: method, Path: path, Range: engine.ParseRange(rng), Body: []byte(body)}
}

// The absence-versus-denial rules (docs/spec/reading-writing.md R-RW-126/127).
func TestAbsenceVersusDenial(t *testing.T) {
	f := newFixture(t)
	f.put("/doc.html", "text/html", `<html><body><p id="a">a</p><p id="b">b</p></body></html>`)
	f.put("/secret.html", "text/html", `<html><body><div id="slot">x</div><p id="other">o</p></body></html>`)
	f.put("/_rules.html", "text/html", rules(
		[5]string{"*", "/*", "GET", "", "Allow"},
		[5]string{"*", "/doc.html", "PUT", "#a", "Allow"},
		[5]string{"*", "/secret.html", "GET", "", "Deny"},
		[5]string{"*", "/secret.html", "PUT", "#slot", "Allow"},
	))
	for _, c := range []struct {
		name string
		op   *engine.Op
		want int
	}{
		{"granted target", sel("PUT", "/doc.html", "selector=#a", "<p id=a>A</p>"), http.StatusPartialContent},
		{"absent target on a readable page", sel("PUT", "/doc.html", "selector=#zz", "<p>z</p>"), http.StatusRequestedRangeNotSatisfiable},
		{"present target not granted", sel("PUT", "/doc.html", "selector=#b", "<p id=b>B</p>"), http.StatusUnauthorized},
		{"no rule grants DELETE, target present", sel("DELETE", "/doc.html", "selector=#a", ""), http.StatusUnauthorized},
		{"no rule grants DELETE, target absent", sel("DELETE", "/doc.html", "selector=#zz", ""), http.StatusUnauthorized},
		{"no rule grants PUT, document missing", sel("PUT", "/missing.html", "selector=#a", "<p>x</p>"), http.StatusUnauthorized},
		{"unreadable page, absent target", sel("PUT", "/secret.html", "selector=#zz", "<p>x</p>"), http.StatusUnauthorized},
		{"unreadable page, present target not granted", sel("PUT", "/secret.html", "selector=#other", "<p>x</p>"), http.StatusUnauthorized},
		{"unreadable page, granted slot", sel("PUT", "/secret.html", "selector=#slot", `<div id="slot">y</div>`), http.StatusPartialContent},
		{"reserved namespace beats everything", sel("PUT", "/.pagelove/x.html", "selector=#a", "<p>x</p>"), http.StatusForbidden},
		{"malformed selector before authorization", sel("DELETE", "/doc.html", "selector=p[", ""), http.StatusUnprocessableEntity},
		{"empty selector", sel("DELETE", "/doc.html", "selector=", ""), http.StatusBadRequest},
		// An unknown placement is an append, authorized like one (live).
		{"unknown placement", sel("POST", "/doc.html", "selector=body; placement=inside", "<p>x</p>"), http.StatusUnauthorized},
		{"empty body", sel("PUT", "/doc.html", "selector=#a", " \n"), http.StatusBadRequest},
		{"non-selector range on a write", sel("PUT", "/doc.html", "bytes=0-3", "abcd"), http.StatusNotImplemented},
		{"POST to a directory", &engine.Op{Method: "POST", Path: "/dir/", Body: []byte("x")}, http.StatusNotImplemented},
		{"whole PUT to a directory path", &engine.Op{Method: "PUT", Path: "/dir/", Body: []byte("x")}, http.StatusBadRequest},
	} {
		if got := f.status(c.op); got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}
}

func TestConditionalSelectorWrites(t *testing.T) {
	f := newFixture(t)
	f.put("/n.html", "text/html", `<html><body><article id="n">one</article><p id="o">o</p></body></html>`)
	f.put("/_rules.html", "text/html", rules([5]string{"*", "/*", "GET,PUT,POST,DELETE", "", "Allow"}))
	elem := f.get("/n.html", "selector=#n").Header.Get("ETag")
	doc := f.get("/n.html", "").Header.Get("ETag")

	// The element tag is accepted, quoted or not (R-RW-85).
	for i, quoted := range []bool{true, false} {
		op := sel("PUT", "/n.html", "selector=#n", `<article id="n">v`+string(rune('2'+i))+`</article>`)
		op.IfMatch = elem
		if !quoted {
			op.IfMatch = strings.Trim(elem, `"`)
		}
		tag := op.IfMatch
		res, e := f.public(op)
		if e != nil {
			t.Fatalf("If-Match %s: %v", tag, e)
		}
		elem = res.Header.Get("ETag")
		if got := f.get("/n.html", "selector=#n").Header.Get("ETag"); got != elem {
			t.Fatalf("PUT response tag %s != element tag %s", elem, got)
		}
	}
	// A selector write is conditioned on the element's tag only: the
	// document's tag is stale for it (live 2026-09-28).
	doc = f.get("/n.html", "").Header.Get("ETag")
	op := sel("POST", "/n.html", "selector=#n", "<b>x</b>")
	op.IfMatch = doc
	if _, e := f.public(op); e == nil || e.Status != http.StatusPreconditionFailed || e.Headers.Get("ETag") != elem {
		t.Fatalf("document tag on a selector POST: %v, want 412 carrying the anchor's tag", e)
	}
	op.IfMatch = elem
	if _, e := f.public(op); e != nil {
		t.Fatalf("anchor tag on a selector POST: %v", e)
	}
	// A stale tag is 412 carrying the tag to retry with: the element's.
	op = sel("PUT", "/n.html", "selector=#n", `<article id="n">lost</article>`)
	op.IfMatch = doc
	_, e := f.public(op)
	if e == nil || e.Status != http.StatusPreconditionFailed {
		t.Fatalf("stale If-Match: %v", e)
	}
	if got, want := e.Headers.Get("ETag"), f.get("/n.html", "selector=#n").Header.Get("ETag"); got != want {
		t.Errorf("412 ETag %s, want the element's %s", got, want)
	}
	// Preconditions never mask 416.
	op = sel("DELETE", "/n.html", "selector=#nope", "")
	op.IfMatch = `"bogus"`
	if got := f.status(op); got != http.StatusRequestedRangeNotSatisfiable {
		t.Errorf("stale If-Match on an absent target: %d, want 416", got)
	}
}

func TestWholeDocumentWritesAndEvents(t *testing.T) {
	f := newFixture(t)
	f.put("/_rules.html", "text/html", rules([5]string{"*", "/*", "*", "", "Allow"}))
	const doc = "<!DOCTYPE html>\n<html><body><h1>v1</h1></body></html>\n"
	res, _ := f.public(&engine.Op{Method: "PUT", Path: "/w.html", Body: []byte(doc)})
	if res.Status != http.StatusCreated || string(res.Body) != doc || res.Header.Get("ETag") == "" {
		t.Fatalf("create: %d %q %v", res.Status, res.Body, res.Header)
	}
	res, _ = f.public(&engine.Op{Method: "PUT", Path: "/w.html", Body: []byte(doc)})
	if res.Status != http.StatusOK || string(res.Body) != doc {
		t.Fatalf("replace: %d %q", res.Status, res.Body)
	}
	// Blobs are echoed too (live 2026-09-28).
	res, _ = f.public(&engine.Op{Method: "PUT", Path: "/w.json", ContentType: "application/json", Body: []byte(`{"a":1}`)})
	if res.Status != http.StatusCreated || string(res.Body) != `{"a":1}` || res.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("blob create: %d %q", res.Status, res.Body)
	}
	// A whole DELETE answers the tag of empty content, even for a path
	// that holds nothing (live 2026-09-28).
	for _, p := range []string{"/w.html", "/never-there.html"} {
		if res, _ = f.public(&engine.Op{Method: "DELETE", Path: p}); res.Status != http.StatusNoContent || res.Header.Get("ETag") != engine.EmptyETag {
			t.Fatalf("delete %s: %d %v", p, res.Status, res.Header)
		}
	}
	evs, err := f.s.Store.EventsAfter(f.ctx, "/w.html", 0)
	if err != nil || len(evs) != 3 {
		t.Fatalf("events: %v %d", err, len(evs))
	}
	// Whole-document events: empty selector, no placement; on PUT the new
	// document's tag (unquoted) and the whole document as body, on DELETE
	// neither (R-SSE-17, live 2026-09-28).
	put, del := evs[1].Data, evs[2].Data
	for _, want := range []string{`<span itemprop="method">PUT</span>`, `<span itemprop="selector"></span>`, `<div itemprop="body"><!DOCTYPE html>`} {
		if !strings.Contains(put, want) {
			t.Errorf("PUT event lacks %s:\n%s", want, put)
		}
	}
	if !regexp.MustCompile(`<span itemprop="etag">[0-9a-f]{64}</span>`).MatchString(put) {
		t.Errorf("PUT event etag is not the unquoted document tag:\n%s", put)
	}
	if strings.Contains(put, "placement") || strings.Contains(del, `itemprop="etag"`) || !strings.Contains(del, `<span itemprop="selector"></span>`) {
		t.Errorf("events:\n%s\n%s", put, del)
	}
	if evs[0].OriginSession != "anon-session" {
		t.Errorf("origin session %q", evs[0].OriginSession)
	}
}

func TestPostInsertsSurroundingWhitespace(t *testing.T) {
	f := newFixture(t)
	f.put("/_rules.html", "text/html", rules([5]string{"*", "/*", "*", "", "Allow"}))
	f.put("/g.html", "text/html", "<html><body>\n  <ul id=\"items\">\n    <li>First item</li>\n  </ul>\n</body></html>\n")
	res, e := f.public(sel("POST", "/g.html", "selector=#items", "<li>Second item</li>\n"))
	if e != nil {
		t.Fatal(e)
	}
	if string(res.Body) != "<li>Second item</li>" || res.Header.Get("Content-Range") != "selector #items" {
		t.Errorf("POST response %q %v", res.Body, res.Header)
	}
	// The POST tag is the inserted child's (live 2026-09-28), not the
	// anchor's; there is no Content-Location.
	if res.Header.Get("ETag") != f.get("/g.html", "selector=#items > li:nth-child(2)").Header.Get("ETag") || res.Header.Get("Content-Location") != "" {
		t.Errorf("the POST ETag %s must be the inserted child's, with no Content-Location", res.Header.Get("ETag"))
	}
	if res.Header.Get("ETag") == f.get("/g.html", "selector=#items").Header.Get("ETag") {
		t.Error("the POST ETag is the anchor's")
	}
	// The docs' getting-started read-back, byte for byte.
	want := "<ul id=\"items\">\n    <li>First item</li>\n  <li>Second item</li>\n</ul>"
	if got := string(f.get("/g.html", "selector=#items").Body); got != want {
		t.Errorf("read-back %q, want %q", got, want)
	}
	evs, _ := f.s.Store.EventsAfter(f.ctx, "/g.html", 0)
	last := evs[len(evs)-1].Data
	if !strings.Contains(last, `<div itemprop="body"><li>Second item</li></div>`) || !strings.Contains(last, `<span itemprop="placement">append</span>`) ||
		!strings.Contains(last, `itemprop="etag"`) {
		t.Errorf("POST event:\n%s", last)
	}
	// Two inserted elements: both in the event body.
	f.public(sel("POST", "/g.html", "selector=#items", "<li>a</li><li>b</li>"))
	evs, _ = f.s.Store.EventsAfter(f.ctx, "/g.html", 0)
	if last := evs[len(evs)-1].Data; !strings.Contains(last, "<li>a</li><li>b</li>") {
		t.Errorf("multi-node POST event:\n%s", last)
	}
}

func TestMoveOrderAndEvents(t *testing.T) {
	f := newFixture(t)
	const board = `<html><body><ul id="l"><li id="a">A</li><li id="b">B</li></ul><ul id="m"></ul></body></html>`
	f.put("/b.html", "text/html", board)
	f.put("/locked.html", "text/html", board)
	f.put("/_rules.html", "text/html", rules(
		[5]string{"*", "/b.html", "GET,MOVE,DELETE,POST", "", "Allow"},
		[5]string{"*", "/locked.html", "MOVE,DELETE,POST", "", "Allow"},
		[5]string{"*", "/locked.html", "GET", "", "Deny"},
		[5]string{"*", "/moved/*", "MOVE", "", "Allow"},
	))
	move := func(path, src, dst, dest string) *engine.Op {
		return &engine.Op{Method: "MOVE", Path: path, Range: engine.ParseRange(src), DestinationRange: engine.ParseRange(dst), Destination: dest}
	}
	for _, c := range []struct {
		name string
		op   *engine.Op
		want int
	}{
		{"cross-document before anything else", move("/nope.html", "selector=#a", "selector=#m; placement=append", "/other.html"), http.StatusNotImplemented},
		{"missing placement", move("/b.html", "selector=#a", "selector=#m", "/b.html"), http.StatusUnprocessableEntity},
		{"empty source selector", move("/b.html", "selector=", "selector=#m; placement=append", "/b.html"), http.StatusUnprocessableEntity},
		{"absent source, readable", move("/b.html", "selector=#zz", "selector=#m; placement=append", "/b.html"), http.StatusRequestedRangeNotSatisfiable},
		{"absent anchor, readable", move("/b.html", "selector=#a", "selector=#zz; placement=append", "/b.html"), http.StatusNotFound},
		{"absent source, unreadable: fail closed", move("/locked.html", "selector=#zz", "selector=#m; placement=append", "/locked.html"), http.StatusUnauthorized},
		{"into its own subtree", move("/b.html", "selector=#l", "selector=#a; placement=append", "/b.html"), http.StatusUnprocessableEntity},
		{"reserved destination", move("/b.html", "", "", "/.pagelove/x.html"), http.StatusForbidden},
		{"whole move onto itself", move("/b.html", "", "", "/b.html"), http.StatusForbidden},
		{"before itself is a no-op", move("/b.html", "selector=#a", "selector=#a; placement=before", "/b.html"), http.StatusNoContent},
		{"element move", move("/b.html", "selector=#a", "selector=#m; placement=APPEND", "/b.html"), http.StatusNoContent},
	} {
		if got := f.status(c.op); got != c.want {
			t.Errorf("%s: status %d, want %d", c.name, got, c.want)
		}
	}
	evs, _ := f.s.Store.EventsAfter(f.ctx, "/b.html", 0)
	if len(evs) != 1 { // the one element move (authoring writes emit none)
		t.Fatalf("%d events on /b.html", len(evs))
	}
	for _, want := range []string{`>MOVE<`, `<span itemprop="selector">#a</span>`, `<span itemprop="placement">append</span>`, `<span itemprop="destination">#m</span>`} {
		if !strings.Contains(evs[0].Data, want) {
			t.Errorf("MOVE event lacks %s:\n%s", want, evs[0].Data)
		}
	}
	// A whole-document move announces a deletion and a creation.
	if got := f.status(move("/b.html", "", "", "/elsewhere/b.html")); got != http.StatusUnauthorized {
		t.Fatalf("whole move without a grant at the destination: %d", got)
	}
	op := move("/b.html", "", "", "/moved/b.html")
	op.Overwrite = "F"
	if got := f.status(op); got != http.StatusNoContent {
		t.Fatalf("whole move: %d", got)
	}
	src, _ := f.s.Store.EventsAfter(f.ctx, "/b.html", 0)
	dst, _ := f.s.Store.EventsAfter(f.ctx, "/moved/b.html", 0)
	if len(src) != 2 || !strings.Contains(src[1].Data, ">DELETE<") || len(dst) != 1 || !strings.Contains(dst[0].Data, ">PUT<") {
		t.Errorf("whole-document move events: %d on the source, %d on the destination", len(src), len(dst))
	}
	if _, err := f.s.Store.Get(f.ctx, "/b.html"); !errors.Is(err, store.ErrNotFound) {
		t.Error("the source still exists")
	}
	f.put("/b.html", "text/html", board)
	op.Path = "/b.html"
	if got := f.status(op); got != http.StatusPreconditionFailed {
		t.Errorf("Overwrite: F onto an existing document: %d, want 412", got)
	}
}
