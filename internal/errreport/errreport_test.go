package errreport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

type recorder struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (r *recorder) Flush(time.Duration) bool              { return true }
func (r *recorder) FlushWithContext(context.Context) bool { return true }
func (r *recorder) Configure(sentry.ClientOptions)        {}
func (r *recorder) Close()                                {}
func (r *recorder) SendEvent(e *sentry.Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}
func (r *recorder) take() []*sentry.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.events
	r.events = nil
	return e
}

func start(t *testing.T, dsn string) *recorder {
	t.Helper()
	rec := &recorder{}
	transport = rec
	t.Setenv("SENTRY_DSN", dsn)
	t.Setenv("SENTRY_ENVIRONMENT", "test")
	t.Cleanup(func() { transport = nil; enabled = false; sentry.CurrentHub().BindClient(nil) })
	return rec
}

func TestOffWithoutDSN(t *testing.T) {
	start(t, "")
	on, err := Init("v1")
	if on || err != nil {
		t.Fatalf("Init = %v, %v; want off", on, err)
	}
	h := slog.NewTextHandler(io.Discard, nil)
	if Handler(h) != slog.Handler(h) {
		t.Fatal("Handler wrapped the logger with reporting off")
	}
}

func TestMalformedDSNFails(t *testing.T) {
	start(t, "not a dsn")
	if _, err := Init("v1"); err == nil {
		t.Fatal("a malformed SENTRY_DSN was accepted")
	}
}

func TestErrorRecordsAreReported(t *testing.T) {
	rec := start(t, "https://public@sentry.invalid/1")
	if on, err := Init("v1"); !on || err != nil {
		t.Fatalf("Init = %v, %v", on, err)
	}
	log := slog.New(Handler(slog.NewTextHandler(io.Discard, nil)))
	log.Info("listening")
	log.Warn("identity provider unavailable", "err", errors.New("dial"))
	cause := &fs.PathError{Op: "open", Path: "x.db", Err: fs.ErrNotExist}
	log.Error("request failed", "err", fmt.Errorf("write: %w", cause), "method", "PUT", "path", "/a.html", "token", "secret")
	sentry.Flush(time.Second)
	got := rec.take()
	if len(got) != 1 {
		t.Fatalf("got %d events, want only the Error record", len(got))
	}
	e := got[0]
	if e.Message != "request failed" || e.Release != "v1" || e.Environment != "test" {
		t.Errorf("message/release/env = %q %q %q", e.Message, e.Release, e.Environment)
	}
	if e.Tags["error_class"] != "*fs.PathError" || e.Tags["method"] != "PUT" {
		t.Errorf("tags = %v", e.Tags)
	}
	if len(e.Exception) != 1 || e.Exception[0].Value != "write: "+cause.Error() || e.Exception[0].Stacktrace == nil {
		t.Errorf("exception = %+v", e.Exception)
	}
	if c := e.Contexts["pagelike"]; c["path"] != "/a.html" || c["token"] != nil {
		t.Errorf("context = %v; want path and nothing unlisted", c)
	}
	if len(e.Fingerprint) != 2 || e.Fingerprint[1] != "*fs.PathError" {
		t.Errorf("fingerprint = %v", e.Fingerprint)
	}
}

func TestRecoverReportsAndRepanics(t *testing.T) {
	rec := start(t, "https://public@sentry.invalid/1")
	if _, err := Init("v1"); err != nil {
		t.Fatal(err)
	}
	h := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	func() {
		defer func() {
			if v := recover(); v != "boom" {
				t.Fatalf("recovered %v; want the panic re-raised", v)
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/p?q=secret", nil))
	}()
	abort := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	func() {
		defer func() { _ = recover() }()
		abort.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	sentry.Flush(time.Second)
	got := rec.take()
	if len(got) != 1 || got[0].Message != "panic" || got[0].Exception[0].Value != "boom" {
		t.Fatalf("events = %+v; want one panic event", got)
	}
	if p := got[0].Contexts["pagelike"]["path"]; p != "/p" {
		t.Errorf("path = %v; want no query string", p)
	}
}
