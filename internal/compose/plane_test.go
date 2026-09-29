package compose_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sky-valley/pagelike/internal/control"
	_ "github.com/sky-valley/pagelike/internal/features"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

// plane is an in-process pagelike serving site "t" at t.localhost with
// every feature linked, and one anonymous client that keeps its cookies.
type plane struct {
	t       testing.TB
	srv     *httptest.Server
	key     string
	s       *site.Site
	cookies map[string]string
}

func newPlane(t testing.TB) *plane {
	t.Helper()
	dir := t.TempDir()
	reg, err := site.NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctl, err := control.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := reg.Create(context.Background(), "t", site.Settings{DefaultGet: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := ctl.CreateKey(context.Background(), "test", []string{"t"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(server.Config{Domain: "localhost"}, reg, ctl, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
		reg.Close()
		ctl.Close()
	})
	return &plane{t: t, srv: srv, key: key, s: s, cookies: map[string]string{}}
}

type reply struct {
	status int
	header http.Header
	body   string
}

func (p *plane) do(method, path, body string, h map[string]string) reply {
	p.t.Helper()
	host := "t.localhost"
	if dav, ok := h["plane"]; ok && dav == "dav" {
		host = "dav-t.localhost"
	}
	req, _ := http.NewRequest(method, p.srv.URL+path, strings.NewReader(body))
	req.Host = host
	if body == "" {
		req.Body, req.ContentLength = nil, 0
	}
	if host == "dav-t.localhost" {
		req.Header.Set("Authorization", "Bearer "+p.key)
	} else {
		for k, v := range p.cookies {
			req.AddCookie(&http.Cookie{Name: k, Value: v})
		}
	}
	for k, v := range h {
		if k != "plane" {
			req.Header.Set(k, v)
		}
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer resp.Body.Close()
	if host == "t.localhost" {
		for _, c := range resp.Cookies() {
			p.cookies[c.Name] = c.Value
		}
	}
	b, _ := io.ReadAll(resp.Body)
	return reply{status: resp.StatusCode, header: resp.Header, body: string(b)}
}

// author stores a document over the authoring plane.
func (p *plane) author(path, body string, ct ...string) {
	p.t.Helper()
	h := map[string]string{"plane": "dav", "Content-Type": "text/html"}
	if len(ct) > 0 {
		h["Content-Type"] = ct[0]
	}
	if r := p.do("PUT", path, body, h); r.status >= 300 {
		p.t.Fatalf("setup PUT %s: %d %s", path, r.status, r.body)
	}
}

// stored reads a document's stored bytes over the authoring plane.
func (p *plane) stored(path string) string {
	p.t.Helper()
	return p.do("GET", path, "", map[string]string{"plane": "dav"}).body
}

// allow stores a rules document granting methods on /* to everyone.
func (p *plane) allow(methods ...string) {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html><html><body>")
	for _, m := range methods {
		b.WriteString(`<div itemscope itemtype="https://pagelove.org/AuthorizationRule"><meta itemprop="actor" content="*"><meta itemprop="resource" content="/*"><meta itemprop="method" content="` + m + `"><meta itemprop="action" content="allow"></div>`)
	}
	b.WriteString("</body></html>")
	p.author("/_rules.html", b.String())
}

func (r reply) must(t *testing.T, status int, contains ...string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("status %d, want %d; body:\n%s", r.status, status, r.body)
	}
	for _, c := range contains {
		if !strings.Contains(r.body, c) {
			t.Errorf("body does not contain %q:\n%s", c, r.body)
		}
	}
}

func (r reply) mustNot(t *testing.T, absent ...string) {
	t.Helper()
	for _, c := range absent {
		if strings.Contains(r.body, c) {
			t.Errorf("body contains %q:\n%s", c, r.body)
		}
	}
}
