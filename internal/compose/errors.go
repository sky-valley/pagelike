package compose

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/liquid"
	"github.com/sky-valley/pagelike/internal/sessel"
)

// Error kinds of composition failures (docs/spec/composing.md R-COMP-160/161).
// A composition error fails the whole request: no partial page is served.
const (
	KindComposition    = "CompositionError"
	KindIncludeMissing = "IncludeNotFound"
	KindIncludeAmbig   = "IncludeAmbiguous"
	KindBudget         = "BudgetExceeded"
	KindTemplateEngine = "UnsupportedTemplateEngine"
	KindPagination     = "InvalidPagination"
	KindNoBase         = "MissingBase"
	KindJSUnavailable  = "JavaScriptUnavailable"
	KindNoMethod       = "NoMethod"
)

// MaxDispatches is the composition dispatch budget of one request
// (R-COMP-27): every include, stamp, method element and directive
// attribute costs one unit; include and stamp cycles end here.
const MaxDispatches = 500

func compositionError(format string, args ...any) *errdoc.Error {
	return errdoc.New(http.StatusInternalServerError, KindComposition, format, args...)
}

func budgetError() *errdoc.Error {
	return errdoc.New(http.StatusServiceUnavailable, KindBudget, "composition budget exceeded")
}

// noMethod is the documented element-form failure (R-COMP-67).
func noMethod(name string) *errdoc.Error {
	return errdoc.New(http.StatusInternalServerError, KindNoMethod, "no method found for %s and no doesNotUnderstand defined", name)
}

// sesselError maps a failed Sessel evaluation during composition (R-COMP-38,
// R-SESSEL-344): budget exhaustion is 503, a thrown HTTPResponse answers
// with its own status, anything else fails composition with 500.
func sesselError(what string, err error) error {
	var ed *errdoc.Error
	if errors.As(err, &ed) {
		return ed
	}
	if r, ok := sessel.ResponseOf(err); ok && r.Status >= 400 && r.Status <= 599 {
		msg := r.Message
		if msg == "" {
			msg = http.StatusText(r.Status)
		}
		return errdoc.New(r.Status, errdoc.DefaultKind(r.Status), "%s", msg)
	}
	if e, ok := sessel.AsError(err); ok && (e.Reason == sessel.ReasonBudget || e.Reason == sessel.ReasonTimeout) {
		return errdoc.New(http.StatusServiceUnavailable, KindBudget, "%s: %s", what, e.Message)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return compositionError("%s: %v", what, err)
}

// SesselError maps a failed Sessel evaluation of a composition directive
// (a method implementation run by an installed MethodDispatcher) the way
// composition maps its own: error documents kept, a thrown HTTPResponse
// answered with its status, budget exhaustion 503, anything else 500
// naming what failed.
func SesselError(what string, err error) error { return sesselError(what, err) }

// liquidError maps a failed render (R-LIQ-206, R-LIQ-212).
func liquidError(err error) error {
	var le *liquid.Error
	if errors.As(err, &le) {
		return le.ErrDoc()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errdoc.New(http.StatusInternalServerError, liquid.KindTemplate, "%v", err)
}

// wrapError keeps error documents and turns anything else into a
// composition error naming the directive.
func wrapError(what string, err error) error {
	var ed *errdoc.Error
	if errors.As(err, &ed) {
		return ed
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return compositionError("%s: %v", what, err)
}

func errorf(status int, kind, format string, args ...any) *errdoc.Error {
	return errdoc.New(status, kind, "%s", fmt.Sprintf(format, args...))
}
