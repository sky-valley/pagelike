package schema

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/sky-valley/pagelike/internal/errdoc"
)

// Violation check names (R-MOD-70): documented ones and pagelike additions.
const (
	CheckCardinality = "cardinality"
	CheckType        = "type"
	CheckEnum        = "enum"
	CheckValidate    = "@validate"
	CheckGroup       = "group"
	CheckComputed    = "computed"
	CheckDefault     = "default"
	CheckReferences  = "references"
	CheckSchema      = "schema"
	CheckWrite       = "@write"
	CheckRead        = "@read"
)

// Violation is one entry of a SchemaViolation document.
type Violation struct {
	Check    string
	ItemType string
	Property string
	Value    *string
	Message  string
	Failure  *BindingFailure
}

// BindingFailure describes a failed binding evaluation (R-MOD-73).
type BindingFailure struct {
	Language string // https://pagelove.org/Sessel, …/JavaScript/Module, or the unknown itemtype
	Variant  string // parse, shape, threw, timeout, out-of-memory, marshal, return-type, import-not-allowed, unknown-schema, unknown-language, unavailable
	Message  string
	Stack    string
}

func (f *BindingFailure) Error() string { return f.Variant + ": " + f.Message }

// ShapeViolation is one violated shape constraint or permit (R-MOD-71).
type ShapeViolation struct {
	Selector   string
	Constraint string
	Message    string
}

// UniqueViolation is one line of a "Constraint violation" problem: a
// uniqueness, reference or restrict failure (R-MOD-72), rendered as
// "[<Kind>] <Selector> (<Constraint>): <Message>".
type UniqueViolation struct {
	Kind       string // uniqueness, reference, cascade-restrict
	Selector   string // [itemprop='p']
	Constraint string // unique(p), unique-group(g), references(p), restrict(T#p)
	ItemType   string
	Property   string
	Value      string
	Message    string
}

// Kinds of "Constraint violation" lines (live 2026-09-29).
const (
	LineUniqueness = "uniqueness"
	LineReference  = "reference"
	LineRestrict   = "cascade-restrict"
)

// esc escapes text as the documented error bodies do: & < > and quotes,
// with ' as &#x27; and " as &quot; (R-MOD-71).
func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#x27;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// reason returns the reason phrase used in error bodies ("Unprocessable
// Entity", as in the documented examples, C10).
func reason(status int) string {
	if status == http.StatusUnprocessableEntity {
		return "Unprocessable Entity"
	}
	return http.StatusText(status)
}

func head(b *strings.Builder, status int, title, itemtype, description string) {
	fmt.Fprintf(b, "<!DOCTYPE html>\n<html>\n  <head>\n    <title>%d %s - %s</title>\n  </head>\n", status, esc(reason(status)), esc(title))
	fmt.Fprintf(b, "  <body itemscope itemtype=\"%s\">\n", itemtype)
	fmt.Fprintf(b, "    <h1 itemprop=\"name\">%s</h1>\n", esc(reason(status)))
	fmt.Fprintf(b, "    <meta itemprop=\"statusCode\" content=\"%d\">\n", status)
	fmt.Fprintf(b, "    <p itemprop=\"description\">%s</p>\n", esc(description))
}

func tail(b *strings.Builder) { b.WriteString("  </body>\n</html>\n") }

// renderFailure renders a BindingFailure item (R-MOD-73) at the given
// indentation.
func renderFailure(b *strings.Builder, f *BindingFailure, indent string) {
	fmt.Fprintf(b, "%s<div itemprop=\"failure\" itemscope itemtype=\"%s\">\n", indent, URLBindingFailure)
	fmt.Fprintf(b, "%s  <meta itemprop=\"language\" content=\"%s\">\n", indent, esc(f.Language))
	fmt.Fprintf(b, "%s  <meta itemprop=\"variant\" content=\"%s\">\n", indent, esc(f.Variant))
	fmt.Fprintf(b, "%s  <p itemprop=\"message\">%s</p>\n", indent, esc(f.Message))
	if f.Stack != "" && f.Variant == "threw" {
		fmt.Fprintf(b, "%s  <pre itemprop=\"stack\">%s</pre>\n", indent, esc(f.Stack))
	}
	fmt.Fprintf(b, "%s</div>\n", indent)
}

// SchemaViolationDocument renders the SchemaViolation envelope (R-MOD-70).
func SchemaViolationDocument(status int, vs []Violation) string {
	var b strings.Builder
	desc := "Schema validation failed"
	if status >= 500 {
		desc = "Schema binding failed"
	}
	head(&b, status, "Schema Violation", URLSchemaViolation, desc)
	b.WriteString("    <ul>\n")
	for _, v := range vs {
		fmt.Fprintf(&b, "      <li itemprop=\"violations\" itemscope itemtype=\"%s\">\n", URLViolation)
		fmt.Fprintf(&b, "        <span itemprop=\"check\">%s</span>\n", esc(v.Check))
		fmt.Fprintf(&b, "        <span itemprop=\"itemtype\">%s</span>\n", esc(v.ItemType))
		if v.Property != "" {
			fmt.Fprintf(&b, "        <span itemprop=\"property\">%s</span>\n", esc(v.Property))
		}
		if v.Value != nil {
			fmt.Fprintf(&b, "        <span itemprop=\"value\">%s</span>\n", esc(*v.Value))
		}
		fmt.Fprintf(&b, "        <span itemprop=\"message\">%s</span>\n", esc(v.Message))
		if v.Failure != nil {
			renderFailure(&b, v.Failure, "        ")
		}
		b.WriteString("      </li>\n")
	}
	b.WriteString("    </ul>\n")
	tail(&b)
	return b.String()
}

// Write-path problems items (live 2026-09-29, docs/compat/
// decisions-2026-09-29/modeling.md): shape, uniqueness, reference and
// restrict refusals, and the cardinality failures of selector writes, are
// answered with PageLove's bare problems item typed
// https://dombase.pagelove.team/ns/error/<Kind>, not with the documented
// full pages (R-MOD-71/72).
const (
	ProblemShape       = "ShapeConstraint"
	ProblemConstraint  = "ConstraintViolation"
	ProblemCascade     = "CascadeBlocked"
	ProblemCardinality = "Cardinality"

	problemsBase = "https://dombase.pagelove.team/ns/error/"
	// cascadePrefix opens the message of a 409 CascadeBlocked problem.
	cascadePrefix = "Operation refused by cascade constraint: "
)

// problemEsc escapes a problem message as PageLove does: & < > only.
var problemEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// ProblemsDocument renders a problems item of the given kind with one
// problem per message.
func ProblemsDocument(kind string, msgs []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<div itemscope itemtype="%s%s"><ul itemprop="problems">`, problemsBase, kind)
	for _, m := range msgs {
		fmt.Fprintf(&b, `<li itemprop="problem"><span itemprop="message">%s</span></li>`, problemEsc.Replace(m))
	}
	b.WriteString(`</ul></div>`)
	return b.String()
}

// constraintList renders the message of a ConstraintViolation or
// CascadeBlocked problem, one line per violation:
//
//	Constraint violation: 1 violation(s):
//	  - [uniqueness] [itemprop='slug'] (unique(slug)): Uniqueness violation: …
func constraintList(vs []UniqueViolation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Constraint violation: %d violation(s):", len(vs))
	for _, v := range vs {
		fmt.Fprintf(&b, "\n  - [%s] %s (%s): %s", v.Kind, v.Selector, v.Constraint, v.Message)
	}
	return b.String()
}

func problemsError(status int, kind string, msgs []string) *errdoc.Error {
	return &errdoc.Error{Shape: errdoc.ShapeProblems, Status: status, Kind: kind, Message: msgs[0], Document: ProblemsDocument(kind, msgs)}
}

// ReadFailureDocument renders a failed @read/@computed evaluation (500 on
// the read path, R-MOD-52): the Error vocabulary with the BindingFailure.
func ReadFailureDocument(status int, message string, f *BindingFailure) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!DOCTYPE html>\n<html>\n  <head>\n    <title>%d %s - Error</title>\n  </head>\n", status, esc(reason(status)))
	b.WriteString("  <body>\n")
	b.WriteString("    <article itemscope itemtype=\"https://pagelove.org/Error\">\n")
	fmt.Fprintf(&b, "      <meta itemprop=\"status\" content=\"%d\">\n", status)
	fmt.Fprintf(&b, "      <p itemprop=\"message\">%s</p>\n", esc(message))
	if f != nil {
		renderFailure(&b, f, "      ")
	}
	b.WriteString("    </article>\n")
	tail(&b)
	return b.String()
}

// Error kinds of SchemaViolation refusals (R-MOD-75, pagelike decision).
// Problems-item refusals carry their item kind (Problem*).
const (
	KindSchemaViolation = "SchemaViolation"
	KindBindingFailure  = "BindingFailure"
)

func schemaError(status int, vs []Violation) *errdoc.Error {
	kind := KindSchemaViolation
	if status >= 500 {
		kind = KindBindingFailure
	}
	msg := "Schema validation failed"
	if len(vs) > 0 {
		msg = vs[0].Message
	}
	return &errdoc.Error{Status: status, Kind: kind, Message: msg, Document: SchemaViolationDocument(status, vs)}
}

// shapeError answers failed shapes (R-MOD-68/71, live 2026-09-29): a 422
// ShapeConstraint problems item ("Shape constraints violated", then one
// problem per violation), or for a DELETE a 409 CascadeBlocked item.
func shapeError(status int, vs []ShapeViolation) *errdoc.Error {
	if status == http.StatusConflict {
		lines := make([]UniqueViolation, len(vs))
		for i, v := range vs {
			lines[i] = UniqueViolation{Kind: URLShape, Selector: v.Selector, Constraint: v.Constraint, Message: v.Message}
		}
		return problemsError(status, ProblemCascade, []string{cascadePrefix + constraintList(lines)})
	}
	msgs := []string{"Shape constraints violated"}
	for _, v := range vs {
		msgs = append(msgs, URLShape+" "+v.Selector+": "+v.Message)
	}
	return problemsError(status, ProblemShape, msgs)
}

// constraintError answers uniqueness and reference failures: a 422
// ConstraintViolation problems item.
func constraintError(vs []UniqueViolation) *errdoc.Error {
	return problemsError(http.StatusUnprocessableEntity, ProblemConstraint, []string{constraintList(vs)})
}

// restrictError answers a write refused by a restrict reference: a 409
// CascadeBlocked problems item.
func restrictError(vs []UniqueViolation) *errdoc.Error {
	return problemsError(http.StatusConflict, ProblemCascade, []string{cascadePrefix + constraintList(vs)})
}

// responseError turns a thrown HTTPResponse into the write's response
// (R-MOD-47): its status (default 500) and its body, or message.
func responseError(status int, body string, headers [][2]string) *errdoc.Error {
	if status < 100 || status > 599 {
		status = http.StatusInternalServerError
	}
	// An empty body falls back to the generic error article.
	e := &errdoc.Error{Status: status, Kind: "HTTPResponse", Message: body, Document: body}
	for _, h := range headers {
		if validHeader(h[0], h[1]) {
			e.WithHeader(h[0], h[1])
		}
	}
	return e
}

func validHeader(k, v string) bool {
	if k == "" {
		return false
	}
	for _, r := range k {
		if r <= ' ' || r >= 0x7f || strings.ContainsRune("()<>@,;:\\\"/[]?={}", r) {
			return false
		}
	}
	for _, r := range v {
		if (r < ' ' && r != '\t') || r == 0x7f {
			return false
		}
	}
	return true
}

func strPtr(s string) *string { return &s }
