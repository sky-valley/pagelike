package hosting

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

// errorRecords keeps the messages of Error records: what reaches Sentry.
type errorRecords struct {
	mu   sync.Mutex
	msgs []string
}

func (e *errorRecords) Enabled(context.Context, slog.Level) bool { return true }
func (e *errorRecords) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		e.mu.Lock()
		e.msgs = append(e.msgs, r.Message)
		e.mu.Unlock()
	}
	return nil
}
func (e *errorRecords) WithAttrs([]slog.Attr) slog.Handler { return e }
func (e *errorRecords) WithGroup(string) slog.Handler      { return e }
func (e *errorRecords) got() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return strings.Join(e.msgs, "; ")
}

func faultHost(t *testing.T, identity http.HandlerFunc) (*Server, *errorRecords, func(method, path, token, body string) int) {
	t.Helper()
	issuer := httptest.NewServer(identity)
	t.Cleanup(issuer.Close)
	reg, err := site.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close() })
	records := &errorRecords{}
	core := server.New(server.Config{Domain: "content.example", TrustProxy: true}, reg, nil, slog.New(records))
	h, err := New(Config{Domain: "content.example", AdminToken: "admin-secret", EdgeToken: "edge-secret", IdentityURL: issuer.URL, IdentityToken: "issuer-secret", FrameOrigins: []string{"https://app.example"}}, core)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, token, body string) int {
		r := httptest.NewRequest(method, "https://example.content.example"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Pagelike-Host", "example.content.example")
		r.Header.Set("Origin", "https://example.content.example")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	b, _ := json.Marshal(Installation{Generation: 1, Version: "v1", Files: map[string][]byte{"index.html": []byte("<h1>Hi</h1>")}, Participation: true})
	if code := call("PUT", "/v1/sites/example", "admin-secret", string(b)); code != 204 {
		t.Fatalf("install: %d", code)
	}
	return h, records, call
}

// Control answering out of contract is our fault to fix; Control being down
// or refusing a ticket is not pagelike's (Control reports its own outages).
func TestIdentityFaultsAreReportedOnlyWhenControlBreaksContract(t *testing.T) {
	cases := []struct {
		name     string
		answer   http.HandlerFunc
		reported string
	}{
		{"malformed answer", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"subject":`)) }, "hosting: identity answer malformed"},
		{"invalid grant", func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`{"subject":"","session":"s","expiresAt":1}`))
		}, "hosting: identity grant invalid"},
		{"refused ticket", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) }, ""},
		{"control erroring", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(502) }, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, records, call := faultHost(t, c.answer)
			if code := call("POST", "/-/session", "edge-secret", `{"ticket":"t"}`); code < 400 {
				t.Fatalf("exchange: %d", code)
			}
			if got := records.got(); got != c.reported {
				t.Fatalf("error records = %q, want %q", got, c.reported)
			}
		})
	}
}

// Every 503 the runtime gives for its own storage is reported.
func TestStorageFaultsAreReported(t *testing.T) {
	h, records, call := faultHost(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	s, err := h.core.Sites.Get(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.DB().Exec(`DROP TABLE meta`); err != nil {
		t.Fatal(err)
	}
	if code := call("GET", "/", "edge-secret", ""); code != 503 {
		t.Fatalf("read without installation state: %d", code)
	}
	if got := records.got(); got != "hosting: reading installation state" {
		t.Fatalf("error records = %q", got)
	}
}

// Visitors' and the edge's mistakes are never reported.
func TestClientErrorsAreNotReported(t *testing.T) {
	_, records, call := faultHost(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(401) })
	for _, c := range []struct{ method, path, token string }{
		{"GET", "/", "wrong"}, {"GET", "/missing.html", "edge-secret"}, {"POST", "/-/session", "edge-secret"},
		{"PUT", "/v1/sites/example", "admin-secret"}, {"GET", "/-/nothing", "edge-secret"},
	} {
		call(c.method, c.path, c.token, "{}")
	}
	if got := records.got(); got != "" {
		t.Fatalf("error records = %q, want none", got)
	}
}

// A site that exists without an installation (one opened and never
// installed) is not a hosted site: pruning skips it and reads answer 404.
// Before, every minute's prune failed on it, and its reads answered 503.
func TestSiteWithoutInstallationIsNotAFault(t *testing.T) {
	h, records, call := faultHost(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	s, err := h.core.Sites.Open(context.Background(), "uninstalled", true)
	if err != nil {
		t.Fatal(err)
	}
	_ = s
	if err := h.PruneTemporary(context.Background(), time.Now()); err != nil {
		t.Fatalf("prune: %v", err)
	}
	r := httptest.NewRequest("GET", "https://uninstalled.content.example/", nil)
	r.Header.Set("Authorization", "Bearer edge-secret")
	r.Header.Set("X-Pagelike-Host", "uninstalled.content.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("read of an uninstalled site: %d", w.Code)
	}
	if code := call("GET", "/", "edge-secret", ""); code != 200 {
		t.Fatalf("installed site: %d", code)
	}
	if got := records.got(); got != "" {
		t.Fatalf("error records = %q, want none", got)
	}
}
