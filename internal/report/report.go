// Package report sends pagelike's own faults to Sentry, when SENTRY_DSN is
// set. It reports what the operator must fix, never what a site's author or a
// visitor caused: Error-level log records, handler panics and a command that
// exits with an error. Author errors (composition, templates, schemas, server
// JavaScript and its budgets) are error documents, not log errors, and stay
// out.
//
// An event carries fixed words only: the log message (a constant at every
// call site), the Go type of the error or panic, and the request method. Paths,
// hosts, site names, error text, headers and bodies stay in the local log,
// since they can carry visitors' data.
package report

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
)

// throttle is the shortest gap between two events with one fingerprint, so a
// fault on every request becomes one issue that keeps alerting, not a flood.
const throttle = time.Minute

type Options struct {
	DSN         string
	Environment string
	Release     string
	Transport   sentry.Transport // tests only
}

// Reporter is nil when reporting is off; every method is safe on nil.
type Reporter struct {
	client *sentry.Client
	now    func() time.Time
	mu     sync.Mutex
	last   map[string]time.Time
}

// FromEnv reads SENTRY_DSN and SENTRY_ENVIRONMENT. Without a DSN it returns
// nil: reporting is off.
func FromEnv(release string) (*Reporter, error) {
	return New(Options{DSN: os.Getenv("SENTRY_DSN"), Environment: os.Getenv("SENTRY_ENVIRONMENT"), Release: release})
}

func New(o Options) (*Reporter, error) {
	if o.DSN == "" {
		return nil, nil
	}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:         o.DSN,
		Environment: o.Environment,
		Release:     o.Release,
		Transport:   o.Transport,
		DataCollection: &sentry.DataCollection{
			UserInfo: sentry.Set(false),
			Cookies:  &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
			HTTPHeaders: &sentry.HeaderCollectionConfig{
				Request:  &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
				Response: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
			},
			HTTPBodies:  []sentry.BodyType{},
			QueryParams: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("sentry: %w", err)
	}
	return &Reporter{client: client, now: time.Now, last: map[string]time.Time{}}, nil
}

// Flush waits briefly for queued events; call it before the process exits.
func (r *Reporter) Flush() {
	if r == nil {
		return
	}
	r.client.Flush(2 * time.Second)
}

// Failure reports a command that exited with err. Only the command's name and
// the error's type leave the host.
func (r *Reporter) Failure(command string, err error) {
	if r == nil || err == nil || ended(err) {
		return
	}
	typ := errorType(err)
	r.send("pagelike "+command+" exited", typ, map[string]string{"command": command, "error_type": typ},
		[]string{"exit", command, typ}, sentry.NewStacktrace())
}

// Recover reports a panic that escapes next, with its stack, and answers 500.
// http.ErrAbortHandler is net/http's way to abort a response, not a fault.
func (r *Reporter) Recover(next http.Handler) http.Handler {
	if r == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			r.Panic(req.Method, v)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, req)
	})
}

// Panic reports a recovered panic from inside the deferred function that
// recovered it, so the stack still holds the panicking frames.
func (r *Reporter) Panic(method string, v any) {
	if r == nil {
		return
	}
	typ := fmt.Sprintf("%T", v)
	r.send("panic", "panic", map[string]string{"panic_type": typ, "method": method},
		[]string{"panic", typ}, sentry.NewStacktrace())
}

// Handler wraps next so that its Error records also become events. Every
// record still reaches next unchanged.
func (r *Reporter) Handler(next slog.Handler) slog.Handler {
	if r == nil {
		return next
	}
	return &handler{next: next, r: r}
}

type handler struct {
	next  slog.Handler
	r     *Reporter
	attrs []slog.Attr
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= slog.LevelError || h.next.Enabled(ctx, l)
}

func (h *handler) Handle(ctx context.Context, rec slog.Record) error {
	if rec.Level >= slog.LevelError {
		h.report(rec)
	}
	if !h.next.Enabled(ctx, rec.Level) {
		return nil
	}
	return h.next.Handle(ctx, rec)
}

func (h *handler) WithAttrs(as []slog.Attr) slog.Handler {
	return &handler{next: h.next.WithAttrs(as), r: h.r, attrs: append(append([]slog.Attr{}, h.attrs...), as...)}
}

// Groups only nest the local log's keys; events read top-level keys.
func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{next: h.next.WithGroup(name), r: h.r, attrs: h.attrs}
}

func (h *handler) report(rec slog.Record) {
	var cause any
	method := ""
	visit := func(a slog.Attr) bool {
		switch a.Key {
		case "err", "error":
			cause = a.Value.Any()
		case "method":
			method = a.Value.String()
		}
		return true
	}
	for _, a := range h.attrs {
		visit(a)
	}
	rec.Attrs(visit)
	if err, ok := cause.(error); ok && ended(err) {
		return
	}
	typ := "none"
	if cause != nil {
		typ = fmt.Sprintf("%T", cause)
	}
	tags := map[string]string{"error_type": typ}
	if method != "" && len(method) <= 16 {
		tags["method"] = method
	}
	h.r.send(rec.Message, typ, tags, []string{rec.Message, typ}, sentry.NewStacktrace())
}

func (r *Reporter) send(value, typ string, tags map[string]string, fingerprint []string, stack *sentry.Stacktrace) {
	key := fmt.Sprint(fingerprint)
	r.mu.Lock()
	now := r.now()
	if last, ok := r.last[key]; ok && now.Sub(last) < throttle {
		r.mu.Unlock()
		return
	}
	r.last[key] = now
	r.mu.Unlock()
	event := sentry.NewEvent()
	event.Level = sentry.LevelError
	event.Exception = []sentry.Exception{{Type: typ, Value: value, Stacktrace: stack}}
	event.Fingerprint = fingerprint
	scope := sentry.NewScope()
	scope.SetTags(tags)
	r.client.CaptureEvent(event, nil, scope)
}

// ended is a request or process its caller ended, not a fault.
func ended(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func errorType(err error) string { return fmt.Sprintf("%T", err) }
