package hosting

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/sky-valley/pagelike/internal/features"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

func TestHostedParticipationLifecycle(t *testing.T) {
	var used, revoked, down atomic.Bool
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer issuer-secret" {
			t.Error("missing issuer credential")
			w.WriteHeader(401)
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["site"] != "example" {
			t.Error("missing site scope")
		}
		if down.Load() {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/redeem" {
			if body["ticket"] != "single-pass" || used.Swap(true) {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"subject": "participant-one", "session": "remote-session", "expiresAt": time.Now().Add(time.Hour).UnixMilli()})
			return
		}
		if body["session"] != "remote-session" || revoked.Load() {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(204)
	}))
	defer issuer.Close()
	reg, err := site.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	core := server.New(server.Config{Domain: "content.example", TrustProxy: true}, reg, nil, nil)
	h, err := New(Config{Domain: "content.example", AdminToken: "admin-secret", EdgeToken: "edge-secret", IdentityURL: issuer.URL, IdentityToken: "issuer-secret", FrameOrigins: []string{"https://player.example"}}, core)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, token, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://example.content.example"+path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Pagelike-Host", "example.content.example")
		r.Header.Set("Origin", "https://example.content.example")
		r.Header.Set("Content-Type", "text/html")
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	rules := `<div itemscope itemtype="https://pagelove.org/AuthorizationRule"><meta itemprop="actor" content="users"><meta itemprop="resource" content="/data/*"><meta itemprop="method" content="POST"><meta itemprop="action" content="Allow"></div>`
	files := map[string][]byte{"index.html": []byte("<h1>Hello</h1>"), "rules.html": []byte(rules), "data/list.html": []byte("<ul id=items></ul>")}
	install := func(generation int64, claimed, deleted bool) {
		b, _ := json.Marshal(Installation{Generation: generation, Version: "v1", Files: files, Participation: claimed, Deleted: deleted})
		w := call("PUT", "/v1/sites/example", "admin-secret", string(b), nil)
		if w.Code != 204 {
			t.Fatalf("install: %d %s", w.Code, w.Body.String())
		}
	}
	install(1, false, false)
	if w := call("GET", "/", "edge-secret", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Hello") {
		t.Fatalf("public read: %d %s", w.Code, w.Body.String())
	}
	if w := call("GET", "/", "wrong", "", nil); w.Code != 401 {
		t.Fatalf("unauthenticated transport: %d", w.Code)
	}
	if w := call("POST", "/-/session", "edge-secret", `{"ticket":"single-pass"}`, nil); w.Code != 403 {
		t.Fatalf("guest participation: %d", w.Code)
	}
	install(2, true, false)
	w := call("POST", "/-/session", "edge-secret", `{"ticket":"single-pass"}`, nil)
	if w.Code != 200 {
		t.Fatalf("exchange: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies: %d", len(cookies))
	}
	ck := cookies[0]
	if ck.Name != "__Host-session" || !ck.Secure || !ck.HttpOnly || !ck.Partitioned || ck.Domain != "" {
		t.Fatal("session cookie is not isolated")
	}
	if w := call("POST", "/-/session", "edge-secret", `{"ticket":"single-pass"}`, nil); w.Code != 401 {
		t.Fatalf("ticket replay: %d", w.Code)
	}
	r := httptest.NewRequest("POST", "https://example.content.example/data/list.html", bytes.NewBufferString(`<li id="vote">hello</li>`))
	r.Header.Set("Authorization", "Bearer edge-secret")
	r.Header.Set("X-Pagelike-Host", "example.content.example")
	r.Header.Set("Origin", "https://example.content.example")
	r.Header.Set("Content-Type", "text/html")
	r.Header.Set("Range", "selector=#items")
	r.AddCookie(ck)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code >= 300 {
		t.Fatalf("participation: %d %s", w.Code, w.Body.String())
	}
	s, _ := reg.Get(context.Background(), "example")
	doc, _ := s.Store.Get(context.Background(), "/data/list.html")
	if !bytes.Contains(doc.Body, []byte("hello")) {
		t.Fatal("participation was not stored")
	}
	if !bytes.Contains(doc.Body, []byte("data-contribution-id")) {
		t.Fatal("stored participation lacks server ownership marker")
	}
	for _, body := range []string{`<script>alert(1)</script>`, `<li onclick="alert(1)">x</li>`, `<li itemtype="https://pagelove.org/AuthorizationRule">x</li>`, `<li data-contribution-id="forged">x</li>`, `<li xmlns:pl="https://pagelove.org"><pl:include resource="/rules.html"></pl:include></li>`} {
		r := httptest.NewRequest("POST", "https://example.content.example/data/list.html", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer edge-secret")
		r.Header.Set("X-Pagelike-Host", "example.content.example")
		r.Header.Set("Origin", "https://example.content.example")
		r.Header.Set("Content-Type", "text/html")
		r.Header.Set("Range", "selector=#items")
		r.AddCookie(ck)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 422 {
			t.Fatalf("unsafe data accepted: %d %s", w.Code, w.Body.String())
		}
	}
	var contributions []contribution
	w = call("GET", "/-/contributions", "edge-secret", "", ck)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &contributions) != nil || len(contributions) != 1 {
		t.Fatalf("owned contributions: %d %s", w.Code, w.Body.String())
	}
	if w := call("DELETE", "/-/contributions/"+contributions[0].ID, "edge-secret", "", nil); w.Code != 401 {
		t.Fatalf("anonymous removal: %d", w.Code)
	}
	if w := call("DELETE", "/-/contributions/"+contributions[0].ID, "edge-secret", "", ck); w.Code != 204 {
		t.Fatalf("own removal: %d %s", w.Code, w.Body.String())
	}
	doc, _ = s.Store.Get(context.Background(), "/data/list.html")
	if bytes.Contains(doc.Body, []byte("hello")) {
		t.Fatal("removed contribution is still served")
	}
	if w := call("DELETE", "/-/contributions/"+contributions[0].ID, "edge-secret", "", ck); w.Code != 204 {
		t.Fatalf("removal retry: %d", w.Code)
	}
	revoked.Store(true)
	if w := call("PUT", "/data/new.html", "edge-secret", "<p>forbidden</p>", ck); w.Code != 401 {
		t.Fatalf("logout: %d", w.Code)
	}
	down.Store(true)
	if w := call("GET", "/", "edge-secret", "", ck); w.Code != 200 {
		t.Fatalf("auth outage broke reads: %d", w.Code)
	}
	if w := call("PUT", "/data/new.html", "edge-secret", "<p>forbidden</p>", ck); w.Code != 503 {
		t.Fatalf("outage write: %d", w.Code)
	}
	install(3, true, true)
	if w := call("GET", "/", "edge-secret", "", nil); w.Code != 410 {
		t.Fatalf("removed site: %d", w.Code)
	}
	install(2, true, false)
	if w := call("GET", "/", "edge-secret", "", nil); w.Code != 410 {
		t.Fatalf("stale install resurrected site: %d", w.Code)
	}
	if _, err := s.Store.Get(context.Background(), "/data/list.html"); err != nil {
		t.Fatal("removal erased retained participation")
	}
}

func TestHTTPSDevelopmentHostKeepsPortAndLocalParent(t *testing.T) {
	reg, err := site.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	core := server.New(server.Config{Domain: "localhost", TrustProxy: true}, reg, nil, nil)
	cfg := Config{Domain: "localhost", AdminToken: "admin", EdgeToken: "edge", IdentityToken: "identity", IdentityURL: "http://127.0.0.1:1", FrameOrigins: []string{"http://127.0.0.1:3001"}}
	h, err := New(cfg, core)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(Installation{Generation: 1, Version: "v1", Files: map[string][]byte{"index.html": []byte("<p>Local HTTPS</p>")}, Participation: true})
	r := httptest.NewRequest("PUT", "http://localhost/v1/sites/example", bytes.NewReader(b))
	r.Header.Set("Authorization", "Bearer admin")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("install: %d", w.Code)
	}
	for _, tc := range []struct {
		method, path, origin string
		status               int
	}{
		{"GET", "/", "", 200},
		{"POST", "/-/session", "https://example.localhost:8085", 400},
		{"POST", "/-/session", "https://example.localhost", 403},
	} {
		r = httptest.NewRequest(tc.method, "http://upstream"+tc.path, strings.NewReader("{}"))
		r.Header.Set("Authorization", "Bearer edge")
		r.Header.Set("X-Pagelike-Host", "example.localhost:8085")
		r.Header.Set("Origin", tc.origin)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s: got %d, want %d: %s", tc.method, tc.origin, w.Code, tc.status, w.Body.String())
		}
	}
	cfg.FrameOrigins = []string{"http://player.example"}
	if _, err := New(cfg, core); err == nil {
		t.Fatal("non-loopback HTTP parent accepted")
	}
	r = httptest.NewRequest("DELETE", "http://upstream/v1/sites/missing/contributions/01234567890123456789012345678901", nil)
	r.Header.Set("Authorization", "Bearer admin")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal("missing site storage acknowledged moderation")
	}
}
