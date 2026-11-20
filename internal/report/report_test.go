package report

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

type storeFault struct{ msg string }

func (e *storeFault) Error() string { return e.msg }

func newTestReporter(t *testing.T) (*Reporter, *sentry.MockTransport) {
	t.Helper()
	transport := &sentry.MockTransport{}
	r, err := New(Options{DSN: "https://public@sentry.example/1", Environment: "test", Release: "v-test", Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("a DSN must turn reporting on")
	}
	return r, transport
}

func TestNoDSNTurnsReportingOff(t *testing.T) {
	r, err := New(Options{})
	if err != nil || r != nil {
		t.Fatalf("New without DSN = %v, %v; want nil, nil", r, err)
	}
	// A nil reporter is safe everywhere it is wired.
	var buf strings.Builder
	log := slog.New(r.Handler(slog.NewTextHandler(&buf, nil)))
	log.Error("request failed", "err", errors.New("x"))
	if !strings.Contains(buf.String(), "request failed") {
		t.Fatal("a nil reporter must still pass records to the log")
	}
	r.Failure("host", errors.New("x"))
	r.Flush()
	h := r.Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

func TestErrorRecordsBecomeEventsWithFixedFieldsOnly(t *testing.T) {
	r, transport := newTestReporter(t)
	var buf strings.Builder
	log := slog.New(r.Handler(slog.NewTextHandler(&buf, nil)))
	log.Error("request failed", "err", &storeFault{msg: "secret value 42"}, "method", "POST", "path", "/data/private-note.html", "site", "abc")
	log.Warn("schema: duplicate declaration ignored", "path", "/x")
	log.Info("pagelike listening")
	if !strings.Contains(buf.String(), "secret value 42") {
		t.Fatal("the local log must keep the full record")
	}
	events := transport.Events()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (only Error records reach Sentry)", len(events))
	}
	e := events[0]
	if e.Level != sentry.LevelError || e.Environment != "test" || e.Release != "v-test" {
		t.Fatalf("event level/env/release = %s/%s/%s", e.Level, e.Environment, e.Release)
	}
	if len(e.Exception) != 1 || e.Exception[0].Value != "request failed" || e.Exception[0].Type != "*report.storeFault" {
		t.Fatalf("exception = %+v; want the record's fixed message typed by its error", e.Exception)
	}
	if e.Exception[0].Stacktrace == nil || len(e.Exception[0].Stacktrace.Frames) == 0 {
		t.Fatal("an error event needs the stack it was logged from")
	}
	if e.Tags["method"] != "POST" || e.Tags["error_type"] != "*report.storeFault" {
		t.Fatalf("tags = %v", e.Tags)
	}
	if strings.Join(e.Fingerprint, " ") != "request failed *report.storeFault" {
		t.Fatalf("fingerprint = %v", e.Fingerprint)
	}
	dump := strings.Join([]string{e.Message, e.Exception[0].Value, e.Transaction}, " ")
	for k, v := range e.Tags {
		dump += " " + k + "=" + v
	}
	for _, leak := range []string{"secret value 42", "private-note", "abc"} {
		if strings.Contains(dump, leak) {
			t.Fatalf("event carries %q, which is request data: %s", leak, dump)
		}
	}
}

func TestRepeatedFaultIsThrottled(t *testing.T) {
	r, transport := newTestReporter(t)
	now := time.Unix(1000, 0)
	r.now = func() time.Time { return now }
	log := slog.New(r.Handler(slog.NewTextHandler(io.Discard, nil)))
	for range 50 {
		log.Error("request failed", "err", &storeFault{})
	}
	log.Error("reactions: queueing outbound requests", "err", &storeFault{})
	if n := len(transport.Events()); n != 2 {
		t.Fatalf("got %d events in one burst, want one per fingerprint", n)
	}
	now = now.Add(throttle + time.Second)
	log.Error("request failed", "err", &storeFault{})
	if n := len(transport.Events()); n != 3 {
		t.Fatalf("got %d events, want the fault reported again after the window", n)
	}
}

func TestCanceledRequestsAreNotFaults(t *testing.T) {
	r, transport := newTestReporter(t)
	log := slog.New(r.Handler(slog.NewTextHandler(io.Discard, nil)))
	log.Error("request failed", "err", context.Canceled)
	log.Error("request failed", "err", errors.Join(context.DeadlineExceeded))
	if n := len(transport.Events()); n != 0 {
		t.Fatalf("got %d events for requests their clients ended", n)
	}
}

func TestRecoverReportsHandlerPanicsWithTheirStack(t *testing.T) {
	r, transport := newTestReporter(t)
	h := r.Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		var m map[string]int
		m["boom"]++
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/data/x", nil))
	if w.Code != 500 {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	events := transport.Events()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	e := events[0]
	if e.Exception[0].Type != "panic" || !strings.HasPrefix(e.Tags["panic_type"], "runtime.") || e.Tags["method"] != "GET" {
		t.Fatalf("exception/tags = %+v %v", e.Exception, e.Tags)
	}
	found := false
	for _, f := range e.Exception[0].Stacktrace.Frames {
		if strings.Contains(f.Function, "TestRecoverReportsHandlerPanicsWithTheirStack") {
			found = true
		}
	}
	if !found {
		t.Fatal("the panic's stack must include the panicking handler")
	}
}

func TestRecoverLetsAbortedHandlersAbort(t *testing.T) {
	r, transport := newTestReporter(t)
	h := r.Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler re-raised", v)
		}
		if n := len(transport.Events()); n != 0 {
			t.Fatalf("an aborted handler is not a fault; got %d events", n)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

func TestFailureReportsTheProcessExit(t *testing.T) {
	r, transport := newTestReporter(t)
	r.Failure("host", &storeFault{msg: "listen tcp 127.0.0.1:8788: address already in use"})
	events := transport.Events()
	if len(events) != 1 || events[0].Tags["command"] != "host" || events[0].Exception[0].Value != "pagelike host exited" {
		t.Fatalf("events = %+v", events)
	}
	if strings.Contains(events[0].Exception[0].Value, "8788") {
		t.Fatal("the exit's error text stays in the local log")
	}
}
