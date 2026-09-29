package liquid

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/osteele/liquid/expressions"
	"github.com/osteele/liquid/parser"

	"github.com/sky-valley/pagelike/internal/errdoc"
)

// Error kinds of a failed render.
const (
	// KindTemplate is a composition error: a template syntax error, or an
	// error in a condition, loop collection or assign (R-LIQ-202 … R-LIQ-206).
	// Composition answers 500.
	KindTemplate = "TemplateError"
	// KindBudget is budget exhaustion (R-LIQ-212). Composition answers 503.
	KindBudget = "BudgetExceeded"
)

// Error is a render that failed as a whole. Errors in a single output
// expression never produce one: they degrade inline (R-LIQ-200).
type Error struct {
	// Kind is KindTemplate or KindBudget.
	Kind string
	// Message describes the failure. It never contains file paths, stack
	// traces or other documents' content (R-LIQ-209).
	Message string
	// Line is the 1-based line in the template source, 0 when unknown.
	Line int

	err error
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("liquid: line %d: %s", e.Line, e.Message)
	}
	return "liquid: " + e.Message
}

// Unwrap returns the underlying error; errors.Is(err, ErrBudget) holds for
// budget failures.
func (e *Error) Unwrap() error { return e.err }

// Status is the HTTP status composition answers with: 503 for budget
// exhaustion, 500 otherwise (R-LIQ-206, R-LIQ-212).
func (e *Error) Status() int {
	if e.Kind == KindBudget {
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

// ErrDoc is the error document composition serves for the failure.
func (e *Error) ErrDoc() *errdoc.Error {
	msg := e.Message
	if e.Line > 0 {
		msg = fmt.Sprintf("line %d: %s", e.Line, msg)
	}
	return errdoc.New(e.Status(), e.Kind, "%s", msg)
}

// ErrExpDepth matches (with errors.Is) the error raised when expression
// filters nest deeper than the limit, 32 by default (R-LIQ-163).
var ErrExpDepth = errors.New("expression filter nesting too deep")

type depthError struct{ limit int }

func (e depthError) Error() string {
	return fmt.Sprintf("expression filter nesting exceeds %d levels", e.limit)
}
func (e depthError) Is(target error) bool { return target == ErrExpDepth }

// maxMarkerMessage bounds the message inside an inline Error item.
const maxMarkerMessage = 300

// errorMarker is what a failed output expression renders in markup context
// (R-LIQ-201): an inline https://pagelove.org/Error item. In XML documents
// the meta element self-closes so the output stays well-formed.
func errorMarker(msg string, xml bool) string {
	if utf8.RuneCountInString(msg) > maxMarkerMessage {
		r := []rune(msg)
		msg = string(r[:maxMarkerMessage])
	}
	meta := `<meta itemprop="kind" content="TemplateError">`
	if xml {
		meta = `<meta itemprop="kind" content="TemplateError"/>`
	}
	return `<span itemscope itemtype="https://pagelove.org/Error">` + meta +
		`<span itemprop="message">` + htmlEscaper.Replace(msg) + `</span></span>`
}

// internalFilters maps the filters the preprocessor inserts to what the
// author wrote, for messages.
var internalFilters = map[string]string{
	"pl_pred":     "expression filter predicate",
	"pl_int":      "range bound",
	"pl_index":    "index",
	"pl_contains": "contains",
	"pl_eq":       "==",
	"pl_ne":       "!=",
	"pl_lt":       "<",
	"pl_gt":       ">",
	"pl_le":       "<=",
	"pl_ge":       ">=",
}

// errMessage turns an evaluation error into one bounded line that names the
// failing filter. Nested filter errors are flattened to the innermost cause:
// the library quotes each level's message inside the next, which grows
// exponentially through nested expression filters (R-LIQ-165).
func errMessage(err error) string {
	name := ""
	for {
		var fe expressions.FilterError
		if errors.As(err, &fe) {
			if n, internal := internalFilters[fe.FilterName]; !internal || name == "" {
				if internal {
					name = n
				} else {
					name = fe.FilterName
				}
			}
			err = fe.Err
			continue
		}
		var ue expressions.UndefinedFilter
		if errors.As(err, &ue) {
			return fmt.Sprintf("undefined filter %q", string(ue))
		}
		break
	}
	msg := cleanMessage(err)
	if name != "" && !strings.HasPrefix(msg, name+":") {
		msg = name + ": " + msg
	}
	return msg
}

var liquidErrorPrefix = regexp.MustCompile(`^Liquid error(?: \(line \d+\))?: `)

// cleanMessage removes the library's location wrapping and any trailing
// stack trace from an error message and bounds its length.
func cleanMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	var pe parser.Error
	if errors.As(err, &pe) && pe.Cause() != nil {
		msg = pe.Cause().Error()
	}
	msg = liquidErrorPrefix.ReplaceAllString(msg, "")
	msg = firstLine(msg)
	if len(msg) > 1000 {
		msg = msg[:1000] + "…"
	}
	return msg
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// isFatal reports whether an evaluation error must fail the whole render
// even inside `{{ }}`: budget exhaustion and a cancelled request.
func isFatal(err error) bool {
	return errors.Is(err, ErrBudget) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
