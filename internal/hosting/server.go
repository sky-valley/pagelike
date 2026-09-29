// Package hosting exposes a managed document runtime behind an authenticated
// edge. It has no public authoring plane or local-account login surface.
package hosting

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

type Config struct {
	Domain        string
	AdminToken    string
	EdgeToken     string
	IdentityURL   string
	IdentityToken string
	FrameOrigins  []string
}

// Installation is one complete, ordered control-plane projection. Files are
// authored bytes (JSON base64). ExpiresAt is Unix milliseconds; zero never expires.
type Installation struct {
	Temporary     bool              `json:"temporary"`
	Generation    int64             `json:"generation"`
	Version       string            `json:"version"`
	Files         map[string][]byte `json:"files,omitempty"`
	Participation bool              `json:"participation"`
	ExpiresAt     int64             `json:"expiresAt"`
	Deleted       bool              `json:"deleted"`
}

type Server struct {
	cfg       Config
	core      *server.Server
	client    *http.Client
	installMu sync.Mutex
	requests  chan struct{}
	writes    chan struct{}
	streams   chan struct{}
}
type principalKey struct{}

func New(cfg Config, core *server.Server) (*Server, error) {
	u, err := url.Parse(cfg.IdentityURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) || cfg.Domain == "" || strings.ContainsAny(cfg.Domain, "/: ") || cfg.AdminToken == "" || cfg.EdgeToken == "" || cfg.IdentityToken == "" || cfg.AdminToken == cfg.EdgeToken || len(cfg.FrameOrigins) == 0 {
		return nil, errors.New("invalid hosting configuration")
	}
	for _, o := range cfg.FrameOrigins {
		v, e := url.Parse(o)
		if e != nil || (v.Scheme != "https" && !(v.Scheme == "http" && (v.Hostname() == "localhost" || v.Hostname() == "127.0.0.1"))) || v.Host == "" || v.User != nil || v.Path != "" || v.RawQuery != "" || v.Fragment != "" {
			return nil, errors.New("invalid frame origin")
		}
	}
	if cfg.IdentityToken == cfg.AdminToken || cfg.IdentityToken == cfg.EdgeToken {
		return nil, errors.New("hosting credentials must be distinct")
	}
	h := &Server{cfg: cfg, core: core, requests: make(chan struct{}, 32), writes: make(chan struct{}, 4), streams: make(chan struct{}, 64), client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	core.Engine.Hooks.BeforeWrite = append(core.Engine.Hooks.BeforeWrite, protectWrite)
	core.Engine.Hooks.BeforeWrite = append(core.Engine.Hooks.BeforeWrite, recordContribution)
	core.Engine.Hooks.GuardStore = guardStore
	core.Public.ResolvePrincipal = func(_ http.ResponseWriter, r *http.Request, _ *site.Site) *identity.Principal {
		p, _ := r.Context().Value(principalKey{}).(*identity.Principal)
		if p == nil {
			return identity.Anonymous(identity.NewSessionID())
		}
		return p
	}
	core.Public.CheckSession = func(ctx context.Context, s *site.Site, sid string) bool {
		current, err := state(ctx, s)
		if err != nil || current.Deleted || current.ExpiresAt > 0 && current.ExpiresAt <= time.Now().UnixMilli() {
			return false
		}
		if current.Temporary {
			return true
		}
		_, status := h.resolve(ctx, s, sid)
		return status == http.StatusOK
	}
	return h, nil
}

func token(r *http.Request, want string) bool {
	return subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+want)) == 1
}
func fail(w http.ResponseWriter, status int) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, http.StatusText(status), status)
}
func decode(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(&struct{}{}) != io.EOF {
		fail(w, 400)
		return false
	}
	return true
}
func state(ctx context.Context, s *site.Site) (Installation, error) {
	var text string
	var x Installation
	if err := s.Store.DB().QueryRowContext(ctx, `SELECT value FROM meta WHERE key='hosting'`).Scan(&text); err != nil {
		return x, err
	}
	err := json.Unmarshal([]byte(text), &x)
	return x, err
}

func (h *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/v1/sites/") {
		if !token(r, h.cfg.AdminToken) {
			fail(w, 401)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/sites/"), "/")
		if len(parts) > 1 {
			h.adminContributions(w, r, parts)
			return
		}
		h.install(w, r)
		return
	}
	if !token(r, h.cfg.EdgeToken) {
		fail(w, 401)
		return
	}
	slots := h.requests
	if r.Method == "GET" && strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		slots = h.streams
	} else if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && r.Method != "QUERY" {
		slots = h.writes
	}
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	default:
		w.Header().Set("Retry-After", "1")
		fail(w, 503)
		return
	}
	// The authenticated edge supplies the public host; the transport Host names
	// this private upstream and is never the tenant selector.
	r = r.Clone(r.Context())
	r.Host = r.Header.Get("X-Pagelike-Host")
	public, err := url.Parse("https://" + r.Host)
	if err != nil || public.Host != r.Host || public.User != nil || public.Path != "" || public.RawQuery != "" || public.Fragment != "" {
		fail(w, 404)
		return
	}
	name, ok := strings.CutSuffix(public.Hostname(), "."+h.cfg.Domain)
	if !ok || !site.ValidName(name) || strings.Contains(name, ".") || name == "console" {
		fail(w, 404)
		return
	}
	s, err := h.core.Sites.Get(r.Context(), name)
	if err != nil {
		fail(w, 404)
		return
	}
	current, err := state(r.Context(), s)
	if err != nil {
		fail(w, 503)
		return
	}
	if current.Deleted || current.ExpiresAt > 0 && current.ExpiresAt <= time.Now().UnixMilli() {
		fail(w, 410)
		return
	}
	if current.ExpiresAt > 0 {
		ctx, cancel := context.WithDeadline(r.Context(), time.UnixMilli(current.ExpiresAt))
		defer cancel()
		r = r.WithContext(ctx)
	}
	w.Header().Set("Content-Security-Policy", "frame-ancestors "+strings.Join(h.cfg.FrameOrigins, " "))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/-/client.js" {
		h.clientJS(w, r)
		return
	}
	if r.URL.Path == "/-/manage" {
		h.manage(w, r)
		return
	}
	writing := r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" && r.Method != "QUERY"
	if writing && r.Header.Get("Origin") != "https://"+r.Host {
		fail(w, 403)
		return
	}
	if r.URL.Path == "/-/session" {
		if r.Method != "POST" {
			fail(w, 405)
			return
		}
		if !current.Participation {
			fail(w, 403)
			return
		}
		h.exchange(w, r, s)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/auth/") || strings.HasPrefix(r.URL.Path, "/-pagelove/") || strings.HasPrefix(r.URL.Path, "/-pagelike/") {
		fail(w, 404)
		return
	}
	sid := ""
	if c, e := r.Cookie(identity.CookieSecure); e == nil {
		sid = c.Value
	}
	pr, status := h.resolve(r.Context(), s, sid)
	if current.Temporary {
		// A temporary site's unguessable URL is its test capability. Its subject
		// never identifies a real account and its data never leaves this site.
		pr = &identity.Principal{Authenticated: true, Sub: "preview", Username: "preview", Issuer: "preview"}
		status = 200
	}
	if r.URL.Path == "/-/contributions" || strings.HasPrefix(r.URL.Path, "/-/contributions/") {
		if status != 200 {
			fail(w, status)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/-/contributions")
		id = strings.TrimPrefix(id, "/")
		h.contributions(w, r, s, pr.Sub, id, pr.HasRole("moderator") && r.URL.Query().Get("all") == "1")
		return
	}
	if r.URL.Path == "/-/reports" {
		if r.Method != "POST" {
			fail(w, 405)
			return
		}
		if status != 200 {
			fail(w, status)
			return
		}
		if current.Temporary {
			fail(w, 403)
			return
		}
		h.report(w, r, s, sid)
		return
	}
	if writing && (!current.Participation || !store.LivePath(r.URL.Path)) {
		fail(w, 403)
		return
	}
	if writing && status != 200 {
		fail(w, status)
		return
	}
	if status != 200 {
		pr = identity.Anonymous(identity.NewSessionID())
	}
	if r.URL.Path == "/-/me" {
		if r.Method != "GET" {
			fail(w, 405)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"authenticated": pr.Authenticated, "participant": pr.Sub, "moderator": pr.HasRole("moderator")})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/-/") {
		fail(w, 404)
		return
	}
	// Neither transport credentials nor incoming forwarding headers are app data.
	r = r.Clone(context.WithValue(r.Context(), principalKey{}, pr))
	r.Header = r.Header.Clone()
	for _, header := range []string{"Authorization", "Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Pagelike-Dev-User", "X-Pagelike-Host"} {
		r.Header.Del(header)
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	if strings.HasPrefix(r.URL.Path, "/v/") {
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/v/"), "/", 2)
		if len(parts) != 2 || !site.ValidName(parts[0]) {
			fail(w, 404)
			return
		}
		r.URL.Path = "/" + parts[1]
		if strings.HasPrefix(r.URL.Path, "/auth/") || strings.HasPrefix(r.URL.Path, "/-pagelike/") || strings.HasPrefix(r.URL.Path, "/-pagelove/") || strings.HasPrefix(r.URL.Path, "/-/") {
			fail(w, 404)
			return
		}
		r.URL.RawPath = ""
		r = r.WithContext(store.WithPublishedVersion(r.Context(), parts[0]))
	}
	h.core.Public.Serve(w, r, s)
}

func (h *Server) install(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/v1/sites/")
	if r.Method != "PUT" || !site.ValidName(name) {
		fail(w, 404)
		return
	}
	var input Installation
	if !decode(w, r, &input, 30<<20) {
		return
	}
	if input.Generation < 1 || input.ExpiresAt < 0 || input.Temporary && (input.ExpiresAt <= time.Now().UnixMilli() || input.ExpiresAt > time.Now().Add(time.Hour).UnixMilli()+10000) {
		fail(w, 400)
		return
	}
	h.installMu.Lock()
	defer h.installMu.Unlock()
	s, err := h.core.Sites.Open(r.Context(), name, true)
	if err != nil {
		fail(w, 503)
		return
	}
	prev, err := state(r.Context(), s)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		fail(w, 503)
		return
	}
	if prev.Deleted {
		w.WriteHeader(204)
		return
	}
	if prev.Generation == input.Generation && (prev.Version != input.Version || prev.Participation != input.Participation || prev.ExpiresAt != input.ExpiresAt || prev.Temporary != input.Temporary || prev.Deleted != input.Deleted) {
		fail(w, 409)
		return
	}
	if prev.Generation > input.Generation {
		if !input.Deleted {
			if err := s.InstallPublished(r.Context(), input.Version, input.Generation, input.Files); err != nil {
				fail(w, 409)
				return
			}
		}
		w.WriteHeader(204)
		return
	}
	if !input.Deleted {
		for path, body := range input.Files {
			if strings.HasPrefix(path, "data/") {
				if _, err := contributionBody("/"+path, engine.ResolveContentType(path, ""), body); err != nil {
					fail(w, 422)
					return
				}
			}
		}
		if err := s.InstallPublished(r.Context(), input.Version, input.Generation, input.Files); err != nil {
			fail(w, 409)
			return
		}
	}
	input.Files = nil
	b, _ := json.Marshal(input)
	if _, err := s.Store.DB().ExecContext(r.Context(), `INSERT INTO meta(key,value) VALUES ('hosting',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(b)); err != nil {
		fail(w, 503)
		return
	}
	if err := s.UpdateSettings(r.Context(), func(cfg *site.Settings) {
		cfg.Hosted = true
		cfg.Cookies = "partitioned"
		cfg.MaxBodyBytes = 10 << 20
		cfg.EmbedOrigins = h.cfg.FrameOrigins
	}); err != nil {
		fail(w, 503)
		return
	}
	if _, err := s.Store.DB().ExecContext(r.Context(), `CREATE TABLE IF NOT EXISTS hosted_sessions(id TEXT PRIMARY KEY,remote_session TEXT NOT NULL)`); err != nil {
		fail(w, 503)
		return
	}
	if _, err := s.Store.DB().ExecContext(r.Context(), contributionSchema); err != nil {
		fail(w, 503)
		return
	}
	if input.Deleted || !input.Participation {
		s.Broker.CloseAll()
	}
	w.WriteHeader(204)
}

type assertion struct {
	Subject   string `json:"subject"`
	Session   string `json:"session"`
	ExpiresAt int64  `json:"expiresAt"`
	Moderator bool   `json:"moderator"`
}

func (h *Server) identity(ctx context.Context, action string, body any, out any) int {
	data, err := json.Marshal(body)
	if err != nil {
		return 503
	}
	r, err := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(h.cfg.IdentityURL, "/")+"/"+action, bytes.NewReader(data))
	if err != nil {
		return 503
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+h.cfg.IdentityToken)
	resp, err := h.client.Do(r)
	if err != nil {
		return 503
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return resp.StatusCode
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 503
	}
	if out != nil {
		d := json.NewDecoder(io.LimitReader(resp.Body, 4097))
		d.DisallowUnknownFields()
		if d.Decode(out) != nil || d.Decode(&struct{}{}) != io.EOF {
			return 503
		}
	}
	return 200
}

func (h *Server) exchange(w http.ResponseWriter, r *http.Request, s *site.Site) {
	var input struct {
		Ticket string `json:"ticket"`
	}
	if !decode(w, r, &input, 4096) {
		return
	}
	if len(input.Ticket) == 0 || len(input.Ticket) > 256 {
		fail(w, 400)
		return
	}
	var grant assertion
	status := h.identity(r.Context(), "redeem", map[string]string{"site": s.Name, "ticket": input.Ticket}, &grant)
	if status != 200 {
		fail(w, status)
		return
	}
	ttl := time.Until(time.UnixMilli(grant.ExpiresAt))
	if grant.Subject == "" || len(grant.Subject) > 128 || len(grant.Session) < 1 || len(grant.Session) > 256 || ttl <= 0 || ttl > 31*24*time.Hour {
		fail(w, 503)
		return
	}
	roles := []string{}
	if grant.Moderator {
		roles = append(roles, "moderator")
	}
	sid, err := s.Users.Login(r.Context(), "", &identity.Identity{Sub: grant.Subject, Issuer: "hosted", Roles: roles}, ttl)
	if err != nil {
		fail(w, 503)
		return
	}
	if _, err := s.Store.DB().ExecContext(r.Context(), `INSERT INTO hosted_sessions(id,remote_session) VALUES (?,?)`, sid, grant.Session); err != nil {
		s.Users.EndSession(r.Context(), sid)
		fail(w, 503)
		return
	}
	identity.CookiePolicy{Secure: true, Partitioned: true}.Write(w, sid, ttl)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"participant": grant.Subject})
}

func (h *Server) resolve(ctx context.Context, s *site.Site, sid string) (*identity.Principal, int) {
	if !identity.ValidSessionID(sid) {
		return nil, 401
	}
	pr, status := s.Users.ResolveSession(ctx, sid)
	if status != identity.SessionActive {
		return nil, 401
	}
	var remote string
	if err := s.Store.DB().QueryRowContext(ctx, `SELECT remote_session FROM hosted_sessions WHERE id=?`, sid).Scan(&remote); err != nil {
		return nil, 503
	}
	statusCode := h.identity(ctx, "validate", map[string]string{"site": s.Name, "session": remote}, nil)
	if statusCode != 200 {
		return nil, statusCode
	}
	return pr, 200
}
