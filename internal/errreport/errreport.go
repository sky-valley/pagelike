// Package errreport sends the server's own failures to Sentry, when an
// operator asks for it with SENTRY_DSN. It is off by default and costs
// nothing then.
//
// What is reported: every log record at Error level (a request that failed
// with an internal error, a recovered panic, a reaction that could not be
// recorded, an authoring request that failed), a panic that escapes a
// handler, and an error that ends the process. Warnings and the PageLove
// error documents a request answers with (4xx, expected 503s) are outcomes,
// not failures, and stay out.
//
// What an event carries: the log message, the error's type and text, the
// request method and path when the record has them, and the stack where it
// was logged. Never headers, cookies, query strings, bodies, the requester's
// address or identity: SendDefaultPII stays off and no HTTP integration
// is installed.
package errreport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

var enabled bool

// transport replaces Sentry delivery in tests.
var transport sentry.Transport

// Init enables reporting when SENTRY_DSN is set. The environment label comes
// from SENTRY_ENVIRONMENT. release names the running build; when empty, the
// binary's embedded VCS revision is used. A malformed DSN is an error so a
// misconfigured deploy fails loudly instead of running unmonitored.
func Init(release string) (bool, error) {
	dsn := strings.TrimSpace(os.Getenv("SENTRY_DSN"))
	if dsn == "" {
		return false, nil
	}
	if release == "" || release == "dev" {
		if r := revision(); r != "" {
			release = r
		}
	}
	err := sentry.Init(sentry.ClientOptions{
		Dsn:            dsn,
		Environment:    strings.TrimSpace(os.Getenv("SENTRY_ENVIRONMENT")),
		Release:        release,
		SendDefaultPII: false,
		// Logs carry the context; breadcrumbs from other goroutines would
		// only attach unrelated requests to an event.
		MaxBreadcrumbs: -1,
		Transport:      transport,
	})
	if err != nil {
		return false, fmt.Errorf("SENTRY_DSN: %w", err)
	}
	enabled = true
	return true, nil
}

// Flush waits up to two seconds for queued events. Call it before exiting.
func Flush() {
	if enabled {
		sentry.Flush(2 * time.Second)
	}
}

// CaptureFatal reports the error that is about to end the process.
func CaptureFatal(err error) {
	if !enabled || err == nil {
		return
	}
	report("process exited with an error", err, nil)
	Flush()
}

func revision() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return ""
}

// Handler wraps next so that records at Error level and above are also
// reported. Without Init it returns next unchanged.
func Handler(next slog.Handler) slog.Handler {
	if !enabled {
		return next
	}
	return &handler{next: next}
}

type handler struct {
	next  slog.Handler
	attrs []slog.Attr
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool { return h.next.Enabled(ctx, l) }

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		var cause any
		fields := map[string]string{}
		visit := func(a slog.Attr) bool {
			switch a.Key {
			case "err":
				cause = a.Value.Any()
			case "method", "path", "site":
				fields[a.Key] = a.Value.String()
			}
			return true
		}
		for _, a := range h.attrs {
			visit(a)
		}
		r.Attrs(visit)
		report(r.Message, cause, fields)
	}
	return h.next.Handle(ctx, r)
}

func (h *handler) WithAttrs(as []slog.Attr) slog.Handler {
	return &handler{next: h.next.WithAttrs(as), attrs: append(append([]slog.Attr(nil), h.attrs...), as...)}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{next: h.next.WithGroup(name), attrs: h.attrs}
}

// Recover reports a panic that escapes next, then re-panics so net/http
// keeps its usual behaviour (the connection is closed and the panic logged).
// http.ErrAbortHandler is the deliberate way to abort a response and is not
// reported.
func Recover(next http.Handler) http.Handler {
	if !enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v != http.ErrAbortHandler {
					report("panic", v, map[string]string{"method": r.Method, "path": r.URL.Path})
					sentry.Flush(time.Second)
				}
				panic(v)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// report sends one event. Events group by message and error type, so one
// failing code path is one issue however many requests hit it.
func report(message string, cause any, fields map[string]string) {
	class, text := describe(cause)
	ev := sentry.NewEvent()
	ev.Level = sentry.LevelError
	ev.Message = message
	ev.Exception = []sentry.Exception{{
		Type:       message,
		Value:      text,
		Stacktrace: sentry.NewStacktrace(),
	}}
	ev.Tags = map[string]string{"error_class": class}
	if m := fields["method"]; m != "" {
		ev.Tags["method"] = m
	}
	if len(fields) > 0 {
		c := sentry.Context{}
		for k, v := range fields {
			c[k] = v
		}
		ev.Contexts = map[string]sentry.Context{"pagelike": c}
	}
	ev.Fingerprint = []string{message, class}
	sentry.CaptureEvent(ev)
}

func describe(cause any) (class, text string) {
	switch c := cause.(type) {
	case nil:
		return "none", ""
	case error:
		// Name the first type that says something: "*fmt.wrapError" doesn't.
		inner := c
		for {
			switch fmt.Sprintf("%T", inner) {
			case "*fmt.wrapError", "*fmt.wrapErrors":
				if u := errors.Unwrap(inner); u != nil {
					inner = u
					continue
				}
			}
			break
		}
		return fmt.Sprintf("%T", inner), c.Error()
	default:
		return fmt.Sprintf("%T", c), fmt.Sprint(c)
	}
}
