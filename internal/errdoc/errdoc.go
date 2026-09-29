// Package errdoc renders PageLove-style error documents: HTML pages carrying
// Error microdata so programs can read the failure.
//
// PageLove uses several vocabularies, chosen by the code path that refused
// the request rather than by status alone. pagelike reproduces the ones
// observed live (docs/compat/decisions.md, live run 2026-09-28; spec
// reading-writing R-RW-130, protocol R-PROTO-120/130):
//   - authorization denials (401, and 403 for signed-in principals):
//     itemtype https://pagelove.org/1.0/Error on <body> with message and
//     resource, plus a login link on 401 (documented, and observed);
//   - the read path (GET/HEAD/QUERY; ShapeRead): a "<code> <Reason> - Error"
//     page, itemtype http://pagelove.org/Error (note http) on <body> with
//     name, statusCode and description;
//   - the write path (ShapeProblems): a bare <div> item typed
//     https://dombase.pagelove.team/ns/error/<Kind> holding a problems list;
//   - a write whose selector matched nothing (ShapeNoMatch, 416): a
//     selector-no-match problems item nested in an http://pagelove.org/Error
//     page with statusCode;
//   - failed preconditions (ShapePrecondition, 412): an
//     http://pagelove.org/Error page with one message span;
//   - everything else (ShapeAuto: 400, 403 reserved namespace, 413, 501,
//     …): itemtype https://pagelove.org/Error on an <article> with status,
//     kind, a nested type item https://dombase.pagelove.team/ns/error/<kind>,
//     message and an optional detail (docs, WebDAV → When something goes
//     wrong).
//
// The observed vocabularies are served as text/html (no charset); denials
// and the generic article as text/html; charset=utf-8 (MediaType). The
// authoring plane's own documents are RenderShort and RenderWrapped.
package errdoc

import (
	"fmt"
	"html"
	"net/http"
	"strings"
)

// ContentType is the media type of every error document.
const ContentType = "text/html; charset=utf-8"

// Shape selects the vocabulary of a public-plane error document.
type Shape int

const (
	// ShapeAuto picks by status: denials use the Permissions document, 416
	// the read-path page, everything else the generic article.
	ShapeAuto Shape = iota
	// ShapeRead is the read path's "<code> <Reason> - Error" page.
	ShapeRead
	// ShapeProblems is the write path's bare problems-list item.
	ShapeProblems
	// ShapeNoMatch is the 416 of a write whose selector matched nothing.
	ShapeNoMatch
	// ShapePrecondition is the 412 of a failed If-Match / If-None-Match.
	ShapePrecondition
)

// Error is an HTTP error with a machine-readable kind.
type Error struct {
	Shape Shape
	// ItemType overrides the problems item's type (ShapeProblems); the
	// default is https://dombase.pagelove.team/ns/error/<Kind>.
	ItemType string
	// Charset serves the document as text/html; charset=utf-8 whatever its
	// shape (the authorization layer's 416, which also carries no Vary).
	Charset bool
	// NoVary suppresses the Vary header public-plane errors carry.
	NoVary bool

	Status   int
	Kind     string // e.g. "RangeNotSatisfiable", "UntranslatableWrite", "MoveCrossResource"
	Message  string
	Resource string
	Detail   string // optional nested error document (HTML) from the pipeline
	// Document, when set, is a complete error document rendered by the
	// package that refused the request (e.g. a ConstraintViolation from
	// validation). The public plane serves it unchanged; the authoring
	// plane embeds it as the detail of its own error article (R-PROTO-112).
	Document string
	Headers  http.Header
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%d %s: %s", e.Status, e.Kind, e.Message)
	}
	return fmt.Sprintf("%d %s", e.Status, e.Kind)
}

// New constructs an error.
func New(status int, kind, format string, args ...any) *Error {
	return &Error{Status: status, Kind: kind, Message: fmt.Sprintf(format, args...)}
}

// Read constructs a read-path error (ShapeRead).
func Read(status int, kind, format string, args ...any) *Error {
	e := New(status, kind, format, args...)
	e.Shape = ShapeRead
	return e
}

// Problems constructs a write-path error (ShapeProblems) whose item type is
// https://dombase.pagelove.team/ns/error/<kind>.
func Problems(status int, kind, format string, args ...any) *Error {
	e := New(status, kind, format, args...)
	e.Shape = ShapeProblems
	return e
}

// NoMatch is the 416 of a write whose selector matched nothing, worded as
// PageLove words it.
func NoMatch(selector string) *Error {
	e := New(http.StatusRequestedRangeNotSatisfiable, "selector-no-match", "selector %q: selector matched no elements at the request path or any origin", selector)
	e.Shape = ShapeNoMatch
	return e
}

// Precondition messages (live-observed wording).
const (
	PreconditionETag      = "ETag does not match"
	PreconditionMissing   = "Document does not exist"
	PreconditionNoneMatch = "ETag matches If-None-Match"
	PreconditionExists    = "resource already exists"
)

// Precondition constructs a 412 (ShapePrecondition) with one of the
// Precondition* messages.
func Precondition(msg string) *Error {
	e := New(http.StatusPreconditionFailed, "PreconditionFailed", "Precondition Failed: %s", msg)
	e.Shape = ShapePrecondition
	return e
}

// MediaType is the Content-Type of the rendered public-plane document.
func (e *Error) MediaType() string {
	if e.IsDenial() || e.Charset {
		return ContentType
	}
	if e.Shape == ShapeProblems {
		// A problems item is text/html whoever rendered it (a Document
		// with several problems: modeling, live 2026-09-29).
		return "text/html"
	}
	if e.Document != "" {
		return ContentType
	}
	switch e.Shape {
	case ShapeRead, ShapeProblems, ShapeNoMatch, ShapePrecondition:
		return "text/html"
	}
	if e.Status == http.StatusRequestedRangeNotSatisfiable {
		return "text/html"
	}
	return ContentType
}

// Denial kinds: the authorization refusals that use the Permissions
// vocabulary. Other 403s (the reserved namespace) use the generic shape.
const (
	KindUnauthorized = "Unauthorized"
	KindForbidden    = "Forbidden"
)

// Denied returns the authorization refusal for a request: 401 when the
// principal is not signed in, 403 when it is (docs/spec/reading-writing.md
// R-RW-125).
func Denied(authenticated bool, resource string) *Error {
	if !authenticated {
		return &Error{Status: http.StatusUnauthorized, Kind: KindUnauthorized, Resource: resource}
	}
	return &Error{Status: http.StatusForbidden, Kind: KindForbidden, Resource: resource}
}

// IsDenial reports whether e is an authorization refusal (401/403).
func (e *Error) IsDenial() bool {
	return e.Status == http.StatusUnauthorized || (e.Status == http.StatusForbidden && e.Kind == KindForbidden)
}

// WithHeader attaches a response header.
func (e *Error) WithHeader(k, v string) *Error {
	if e.Headers == nil {
		e.Headers = http.Header{}
	}
	e.Headers.Set(k, v)
	return e
}

// StatusText returns the RFC 9110 reason phrase (Go's net/http still uses
// the RFC 7231 names for 413, 416 and 422).
func StatusText(status int) string {
	switch status {
	case http.StatusRequestEntityTooLarge:
		return "Content Too Large"
	case http.StatusRequestedRangeNotSatisfiable:
		return "Range Not Satisfiable"
	case http.StatusUnprocessableEntity:
		return "Unprocessable Content"
	}
	return http.StatusText(status)
}

// DefaultKind maps a status to a default error kind name.
func DefaultKind(status int) string {
	return strings.ReplaceAll(StatusText(status), " ", "")
}

func (e *Error) kind() string {
	if e.Kind != "" {
		return e.Kind
	}
	return DefaultKind(e.Status)
}

// Render produces the public-plane error document without a login link.
func (e *Error) Render() string { return e.RenderPublic("") }

// RenderAuth renders the 401/403 document with a login link.
func (e *Error) RenderAuth(loginPath string) string { return e.renderAuth(loginPath, "") }

// RenderDenial renders the authorization refusal document exactly as
// documented (spec permissions-identity R-PERM-55/56): a login link on a
// 401 when loginPath is set, a logout link on a 403 when logoutPath is set
// (both only when the host has an identity provider).
func (e *Error) RenderDenial(loginPath, logoutPath string) string {
	return e.renderAuth(loginPath, logoutPath)
}

// RenderPublic produces the error document for the public plane, choosing
// the vocabulary PageLove uses for the status. loginPath is linked from 401
// documents ("" omits the link).
func (e *Error) RenderPublic(loginPath string) string {
	switch {
	case e.Document != "":
		return e.Document
	case e.IsDenial():
		return e.renderAuth(loginPath, "")
	}
	switch e.Shape {
	case ShapeRead:
		return e.renderRange()
	case ShapeProblems:
		return e.renderProblems()
	case ShapeNoMatch:
		return e.renderNoMatch()
	case ShapePrecondition:
		return e.renderPrecondition()
	}
	if e.Status == http.StatusRequestedRangeNotSatisfiable {
		return e.renderRange()
	}
	return e.RenderAuthoring()
}

// readReason is the reason phrase of the read-path page: PageLove prints
// "Range Not Satisfiable" but the RFC 7231 "Unprocessable Entity".
func readReason(status int) string {
	if status == http.StatusRequestedRangeNotSatisfiable {
		return "Range Not Satisfiable"
	}
	return http.StatusText(status)
}

// problemsItem renders a bare problems-list item. Its message is escaped
// as text, quotes left literal (live: `selector "#nope": …`).
func problemsItem(itemtype, msg string) string {
	return `<div itemscope itemtype="` + esc(itemtype) + `"><ul itemprop="problems"><li itemprop="problem"><span itemprop="message">` + textEsc.Replace(msg) + `</span></li></ul></div>`
}

// Text escapers matching PageLove's: problem messages escape only & < >;
// the read path's description also writes " as &quot; (live: a Sessel
// parse error shows unexpected token &quot;Eof&quot;).
var (
	textEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	readEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
)

// ProblemsItem renders a bare problems-list item typed
// https://dombase.pagelove.team/ns/error/<kind>.
func ProblemsItem(kind, msg string) string {
	return problemsItem("https://dombase.pagelove.team/ns/error/"+kind, msg)
}

// renderProblems renders the write path's error item (live 2026-09-28:
// NotFound, InvalidPath, MoveMissingHeaders, MoveCrossResource, …).
func (e *Error) renderProblems() string {
	if e.ItemType != "" {
		return problemsItem(e.ItemType, e.Message)
	}
	return ProblemsItem(e.kind(), e.Message)
}

// renderNoMatch renders the 416 of a write whose selector matched nothing.
func (e *Error) renderNoMatch() string {
	return fmt.Sprintf("<!DOCTYPE html>\n<html><body><div itemscope itemtype=\"http://pagelove.org/Error\"><span itemprop=\"statusCode\">%d</span><span itemprop=\"message\">%s</span></div></body></html>",
		e.Status, ProblemsItem("selector-no-match", e.Message))
}

// renderPrecondition renders a 412.
func (e *Error) renderPrecondition() string {
	return "<!DOCTYPE html>\n<html><body><div itemscope itemtype=\"http://pagelove.org/Error\"><span itemprop=\"message\">" + textEsc.Replace(e.Message) + "</span></div></body></html>"
}

// renderAuth renders the documented 401/403 document (docs, Permissions →
// AuthorizationRule → Testable examples).
func (e *Error) renderAuth(loginPath, logoutPath string) string {
	title := fmt.Sprintf("%d %s", e.Status, StatusText(e.Status))
	msg := e.Message
	if msg == "" {
		if e.Status == http.StatusUnauthorized {
			msg = "Authentication required to access this resource."
		} else {
			msg = "You do not have permission to access this resource."
		}
	}
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html>\n<head>\n    <meta charset=\"UTF-8\">\n")
	fmt.Fprintf(&b, "    <title>%s</title>\n</head>\n", esc(title))
	b.WriteString("<body itemscope itemtype=\"https://pagelove.org/1.0/Error\">\n")
	fmt.Fprintf(&b, "    <h1>%s</h1>\n", esc(title))
	fmt.Fprintf(&b, "    <p itemprop=\"message\">%s</p>\n", esc(msg))
	fmt.Fprintf(&b, "    <dl>\n        <dt>Resource</dt>\n        <dd itemprop=\"resource\">%s</dd>\n    </dl>\n", esc(e.Resource))
	if e.Status == http.StatusUnauthorized && loginPath != "" {
		fmt.Fprintf(&b, "    <p><a href=\"%s\">Log in</a></p>\n", esc(loginPath))
	}
	if e.Status == http.StatusForbidden && logoutPath != "" {
		fmt.Fprintf(&b, "    <p><a href=\"%s\">Log out</a></p>\n", esc(logoutPath))
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// renderRange renders the read path's page (observed live 2026-09-28 for
// 400, 404, 416 and 422).
func (e *Error) renderRange() string {
	text := readReason(e.Status)
	var b strings.Builder
	fmt.Fprintf(&b, "<!DOCTYPE html>\n<html>\n  <head>\n    <title>%d %s - Error</title>\n  </head>\n", e.Status, esc(text))
	b.WriteString("  <body itemscope itemtype=\"http://pagelove.org/Error\">\n")
	fmt.Fprintf(&b, "    <h1 itemprop=\"name\">%s</h1>\n", esc(text))
	fmt.Fprintf(&b, "    <meta itemprop=\"statusCode\" content=\"%d\">\n", e.Status)
	fmt.Fprintf(&b, "    <p itemprop=\"description\">%s</p>\n", readEsc.Replace(e.Message))
	b.WriteString("  </body>\n</html>\n")
	return b.String()
}

// RenderAuthoring renders the generic error article (docs, WebDAV → When
// something goes wrong): the shape of every authoring-plane error and of
// public-plane errors without a more specific vocabulary.
func (e *Error) RenderAuthoring() string {
	kind := e.kind()
	var b strings.Builder
	fmt.Fprintf(&b, "<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"UTF-8\">\n<title>%d %s</title>\n</head>\n<body>\n", e.Status, esc(StatusText(e.Status)))
	b.WriteString("<article itemscope itemtype=\"https://pagelove.org/Error\">\n")
	fmt.Fprintf(&b, "  <meta itemprop=\"status\" content=\"%d\">\n", e.Status)
	fmt.Fprintf(&b, "  <meta itemprop=\"kind\" content=\"%s\">\n", esc(kind))
	fmt.Fprintf(&b, "  <div itemprop=\"type\" itemscope itemtype=\"https://dombase.pagelove.team/ns/error/%s\"></div>\n", esc(kind))
	msg := e.Message
	if msg == "" {
		msg = StatusText(e.Status)
	}
	fmt.Fprintf(&b, "  <p itemprop=\"message\">%s</p>\n", esc(msg))
	if e.Resource != "" {
		fmt.Fprintf(&b, "  <meta itemprop=\"resource\" content=\"%s\">\n", esc(e.Resource))
	}
	detail := e.Detail
	if detail == "" {
		detail = e.Document
	}
	if detail != "" {
		fmt.Fprintf(&b, "  <div itemprop=\"detail\">%s</div>\n", detail)
	}
	b.WriteString("</article>\n</body>\n</html>\n")
	return b.String()
}

// Authoring-plane documents (live 2026-09-28): a short article for a missing
// path or a refused QUERY, and a wrapper article whose detail carries the
// document the write pipeline produced.

// RenderShort renders <article itemtype="https://pagelove.org/Error/<suffix>">
// with status and message (suffix "NotFound" or "Internal").
func RenderShort(suffix string, status int, msg string) string {
	return fmt.Sprintf(`<article itemscope itemtype="https://pagelove.org/Error/%s"><meta itemprop="status" content="%d"><p itemprop="message">%s</p></article>`, esc(suffix), status, textEsc.Replace(msg))
}

// RenderWrapped renders the generic authoring article around detail markup
// (a public-plane error document or a problems item).
func RenderWrapped(status int, kind, detail string) string {
	return fmt.Sprintf(`<article itemscope itemtype="https://pagelove.org/Error"><meta itemprop="status" content="%d"><meta itemprop="kind" content="%s"><div itemprop="type" itemscope itemtype="https://dombase.pagelove.team/ns/error/%s"></div><div itemprop="detail">%s</div></article>`, status, esc(kind), esc(kind), detail)
}

func esc(s string) string { return html.EscapeString(s) }
