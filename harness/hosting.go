package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sky-valley/pagelike/internal/hosting"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

var hostedCleanup sync.Map

// This target uses the real hosting HTTP boundary, including pass redemption,
// cookies and storage. Only the independent identity authority is a fixture.
func setupHosted(ctx context.Context, c *Case) (_ *Env, err error) {
	if c.Live || !c.Root || len(c.Site.Files) != 0 || len(c.Site.Rules) != 0 {
		return nil, errors.New("hosted cases require local root and explicit publications")
	}
	dir, err := os.MkdirTemp("", "pagelike-host-case-*")
	if err != nil {
		return nil, err
	}
	var reg *site.Registry
	var public *httptest.Server
	var issuer *httptest.Server
	cleanup := func() {
		if public != nil {
			public.CloseClientConnections()
			public.Close()
		}
		if issuer != nil {
			issuer.Close()
		}
		if reg != nil {
			reg.Close()
		}
		os.RemoveAll(dir)
	}
	defer func() {
		if err != nil {
			cleanup()
		}
	}()
	var mu sync.Mutex
	used := map[string]bool{}
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer identity" {
			w.WriteHeader(401)
			return
		}
		var in map[string]string
		if json.NewDecoder(r.Body).Decode(&in) != nil || in["site"] != "t" {
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/redeem" {
			actor, ok := c.Actors[in["ticket"]]
			if !ok || used[in["ticket"]] {
				w.WriteHeader(401)
				return
			}
			used[in["ticket"]] = true
			json.NewEncoder(w).Encode(map[string]any{"subject": in["ticket"], "session": in["ticket"], "expiresAt": time.Now().Add(time.Hour).UnixMilli(), "moderator": len(actor.Roles) > 0 && actor.Roles[0] == "moderator"})
			return
		}
		if !used[in["session"]] {
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(204)
	}))
	swap := &swapHandler{}
	var handler *hosting.Server
	open := func() error {
		var e error
		reg, e = site.NewRegistry(dir)
		if e != nil {
			return e
		}
		core := server.New(server.Config{Domain: "content.example", TrustProxy: true}, reg, nil, nil)
		handler, e = hosting.New(hosting.Config{Domain: "content.example", AdminToken: "admin", EdgeToken: "edge", IdentityToken: "identity", IdentityURL: issuer.URL, FrameOrigins: []string{"https://player.example"}}, core)
		if e != nil {
			return e
		}
		swap.mu.Lock()
		swap.h = handler
		swap.mu.Unlock()
		return nil
	}
	if err = open(); err != nil {
		return nil, err
	}
	public = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Bearer edge")
		}
		r.Header.Set("X-Pagelike-Host", "t.content.example")
		swap.ServeHTTP(w, r)
	}))
	env := &Env{PublicURL: public.URL, PublicHost: "t.content.example", Origin: "https://t.content.example", HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: noRedirect}, Vars: map[string]string{}, Actors: map[string]*Client{}, Caps: map[string]bool{"restart": true, "multi-actor": true}}
	for name := range c.Actors {
		env.Actors[name] = newClient(name)
	}
	env.Publish = func(ctx context.Context, b *PublishedBundle, files map[string][]byte) error {
		body, _ := json.Marshal(hosting.Installation{Generation: b.Generation, Version: b.Version, Files: files, Participation: b.Participation, Deleted: b.Deleted})
		r := httptest.NewRequest("PUT", "/v1/sites/t", bytes.NewReader(body)).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer admin")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 204 {
			return fmt.Errorf("install status %d: %s", w.Code, strings.TrimSpace(w.Body.String()))
		}
		return nil
	}
	env.Restart = func() error { reg.Close(); return open() }
	hostedCleanup.Store(env, cleanup)
	return env, nil
}
