package errdoc

import (
	"net/http"
	"strings"
	"testing"
)

// Each status uses the vocabulary PageLove documents or was observed to
// use (docs/spec/reading-writing.md R-RW-130).
func TestVocabularies(t *testing.T) {
	for _, c := range []struct {
		err       *Error
		login     string
		contains  []string
		forbidden []string
	}{
		{Denied(false, "/a.html"), "/auth/login", []string{
			`<body itemscope itemtype="https://pagelove.org/1.0/Error">`, "<title>401 Unauthorized</title>",
			`<p itemprop="message">Authentication required to access this resource.</p>`,
			`<dd itemprop="resource">/a.html</dd>`, `<a href="/auth/login">Log in</a>`}, nil},
		{Denied(true, "/a.html"), "/auth/login", []string{`itemtype="https://pagelove.org/1.0/Error"`, "403 Forbidden"}, []string{"Log in"}},
		{New(http.StatusRequestedRangeNotSatisfiable, "RangeNotSatisfiable", "HTML parsing error: No elements matched selector: #x"), "", []string{
			"<title>416 Range Not Satisfiable - Error</title>", `<body itemscope itemtype="http://pagelove.org/Error">`,
			`<h1 itemprop="name">Range Not Satisfiable</h1>`, `<meta itemprop="statusCode" content="416">`,
			`<p itemprop="description">HTML parsing error: No elements matched selector: #x</p>`}, nil},
		{New(http.StatusRequestEntityTooLarge, "ContentTooLarge", "too big"), "", []string{
			`<article itemscope itemtype="https://pagelove.org/Error">`, `<meta itemprop="status" content="413">`, `<p itemprop="message">too big</p>`}, nil},
		{New(http.StatusForbidden, "ReservedNamespace", "reserved"), "", []string{`itemtype="https://pagelove.org/Error"`,
			`<meta itemprop="kind" content="ReservedNamespace">`, `itemtype="https://dombase.pagelove.team/ns/error/ReservedNamespace"`}, []string{"1.0/Error"}},
		{New(http.StatusNotImplemented, "MoveCrossResource", "no"), "", []string{`content="MoveCrossResource"`, `content="501"`}, nil},
	} {
		got := c.err.RenderPublic(c.login)
		for _, s := range c.contains {
			if !strings.Contains(got, s) {
				t.Errorf("%d %s: missing %s in\n%s", c.err.Status, c.err.Kind, s, got)
			}
		}
		for _, s := range c.forbidden {
			if strings.Contains(got, s) {
				t.Errorf("%d %s: unexpected %s", c.err.Status, c.err.Kind, s)
			}
		}
	}
	// A document supplied by the refusing package is served unchanged on
	// the public plane and embedded as detail on the authoring plane.
	e := New(http.StatusUnprocessableEntity, "ConstraintViolation", "shape")
	e.Document = `<div itemscope itemtype="https://pagelove.org/ConstraintViolation"></div>`
	if e.RenderPublic("") != e.Document || !strings.Contains(e.RenderAuthoring(), `<div itemprop="detail"><div itemscope`) {
		t.Error("pipeline documents are not passed through")
	}
	if e.MediaType() != ContentType {
		t.Errorf("full pipeline document served as %q", e.MediaType())
	}
	// A problems item is text/html without a charset, even when the
	// refusing package rendered it (several problems in one item).
	e.Shape = ShapeProblems
	if e.MediaType() != "text/html" || e.RenderPublic("") != e.Document {
		t.Errorf("problems document served as %q", e.MediaType())
	}
}
