// Package site manages the lifecycle of sites: opening their stores,
// serializing their writes, holding their live-stream brokers, settings and
// caches of host-wide derived data.
package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/sse"
	"github.com/sky-valley/pagelike/internal/store"
)

// ErrNoSite reports an unknown site.
var ErrNoSite = errors.New("site: no such site")

var nameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ValidName reports whether s is a valid site name (a DNS label).
func ValidName(s string) bool { return nameRE.MatchString(s) && !strings.HasPrefix(s, "dav-") }

// Settings are per-site configuration, stored in the site database.
type Settings struct {
	// Hosted separates authored runtime configuration from participant data.
	Hosted bool `json:"hosted,omitempty"`
	// DefaultGet is the host default-GET mode: "allow" grants GET/HEAD
	// requests that no rule matches; "deny" refuses them.
	DefaultGet string `json:"default_get"`
	// Aliases are extra public hostnames served by this site.
	Aliases []string `json:"aliases,omitempty"`
	// MaxBodyBytes caps write request bodies (default 1 GiB, as PageLove).
	MaxBodyBytes int64 `json:"max_body_bytes,omitempty"`
	// Cookies selects the session cookie policy: "lax" (default; SameSite=Lax
	// as PageLove, spec R-PERM-64) or "partitioned" (SameSite=None; Secure;
	// Partitioned, for sites embedded in other sites' frames).
	Cookies string `json:"cookies,omitempty"`
	// EmbedOrigins lists origins trusted to frame this site's sign-in page
	// (pagelike extension for experiences played inside iframes). Empty
	// means sign-in pages refuse to be framed (frame-ancestors 'none').
	EmbedOrigins []string `json:"embed_origins,omitempty"`
	// SessionCookie overrides the session cookie name (default
	// __Host-session on secure origins, pagelike_session otherwise).
	SessionCookie string `json:"session_cookie,omitempty"`
	// SessionLifetime is the lifetime of signed-in sessions, as a Go
	// duration (default 720h).
	SessionLifetime string `json:"session_lifetime,omitempty"`
	// SSEBufferEvents overrides the per-stream event queue length
	// (pagelike-only; used by tests of slow-consumer handling).
	SSEBufferEvents int `json:"sse_buffer_events,omitempty"`
	// Identity paths (PageLove per-host OIDC settings; defaults /auth/*).
	LoginPath    string `json:"login_path,omitempty"`
	LogoutPath   string `json:"logout_path,omitempty"`
	CallbackPath string `json:"callback_path,omitempty"`
	// OIDC configures an external identity provider (optional). The client
	// secret lives only in the site database and is never exported.
	OIDC *OIDCConfig `json:"oidc,omitempty"`
	// Lineage records pagelike fork provenance (pagelike extension).
	Lineage *Lineage `json:"lineage,omitempty"`
	// Outbound is the destination policy of outbound HTTP requests sent by
	// reactions (docs/spec/reacting.md R-REACT-61). Nil denies private and
	// loopback destinations.
	Outbound *OutboundSettings `json:"outbound,omitempty"`
}

// OutboundSettings relaxes the outbound destination policy for one site.
type OutboundSettings struct {
	// AllowPrivate permits loopback, link-local, RFC 1918, CGNAT and ULA
	// destinations.
	AllowPrivate bool `json:"allow_private,omitempty"`
	// Allow lists destinations permitted although private: host names,
	// IP addresses, host:port or ip:port pairs, and CIDR prefixes.
	Allow []string `json:"allow,omitempty"`
}

// OIDCConfig mirrors PageLove's per-host OpenID configuration fields (the
// login, logout and callback paths are the Settings fields above).
type OIDCConfig struct {
	// Issuer is the provider's openid-configuration URL or its issuer URL.
	Issuer       string `json:"openid_configuration"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	// Scopes is a space-separated list (default "openid email profile").
	Scopes string `json:"scopes,omitempty"`
	// RolesClaim names the claim carrying roles (default "roles"; a dotted
	// path reaches nested claims). pagelike extension.
	RolesClaim string `json:"roles_claim,omitempty"`
}

// Lineage links a forked site to the site and version it came from.
type Lineage struct {
	ForkedFrom   string `json:"forked_from"`
	ForkedAt     string `json:"forked_at"`
	SourceGen    int64  `json:"source_generation"`
	SourceDigest string `json:"source_digest"`
	Note         string `json:"note,omitempty"`
}

// Defaults fills unset settings.
func (s *Settings) Defaults() {
	if s.DefaultGet == "" {
		s.DefaultGet = "allow"
	}
	if s.MaxBodyBytes == 0 {
		s.MaxBodyBytes = 1 << 30
	}
	if s.Cookies == "" {
		s.Cookies = "lax"
	}
	if s.LoginPath == "" {
		s.LoginPath = "/auth/login"
	}
	if s.LogoutPath == "" {
		s.LogoutPath = "/auth/logout"
	}
	if s.CallbackPath == "" {
		s.CallbackPath = "/auth/callback"
	}
}

// Site is an open site.
type Site struct {
	Name   string
	Store  *store.Site
	Broker *sse.Broker
	Users  *identity.Users

	// WriteMu serializes every mutation of the site.
	WriteMu sync.Mutex

	settingsMu   sync.RWMutex
	settings     Settings
	settingsRaw  string    // stored JSON the in-memory settings came from
	settingsRev  int64     // bumped whenever the settings change
	settingsSeen time.Time // last time the stored copy was checked

	index *Index
}

// Settings returns a copy of the site's settings.
//
// Settings changed by another process (the pagelike CLI against a running
// server) are picked up within a second.
func (s *Site) Settings() Settings {
	s.refreshSettings()
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	c := s.settings
	return c
}

// SettingsRevision identifies the current settings (caches key on it).
func (s *Site) SettingsRevision() int64 {
	s.refreshSettings()
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.settingsRev
}

func (s *Site) refreshSettings() {
	s.settingsMu.RLock()
	fresh := time.Since(s.settingsSeen) < time.Second
	s.settingsMu.RUnlock()
	if fresh {
		return
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if time.Since(s.settingsSeen) < time.Second {
		return
	}
	s.settingsSeen = time.Now()
	var v string
	if err := s.Store.DB().QueryRow(`SELECT value FROM meta WHERE key='settings'`).Scan(&v); err != nil || v == s.settingsRaw {
		return
	}
	var next Settings
	if json.Unmarshal([]byte(v), &next) != nil {
		return
	}
	next.Defaults()
	s.settings, s.settingsRaw = next, v
	s.settingsRev++
}

// UpdateSettings applies fn to the settings and persists them.
func (s *Site) UpdateSettings(ctx context.Context, fn func(*Settings)) error {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	next := s.settings
	fn(&next)
	next.Defaults()
	b, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if _, err := s.Store.DB().ExecContext(ctx, `INSERT INTO meta(key, value) VALUES ('settings', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(b)); err != nil {
		return err
	}
	s.settings, s.settingsRaw = next, string(b)
	s.settingsRev++
	s.settingsSeen = time.Now()
	return nil
}

func (s *Site) loadSettings() error {
	var v string
	err := s.Store.DB().QueryRow(`SELECT value FROM meta WHERE key='settings'`).Scan(&v)
	if err == nil {
		if err := json.Unmarshal([]byte(v), &s.settings); err != nil {
			return fmt.Errorf("site %s: bad settings: %w", s.Name, err)
		}
		s.settingsRaw = v
	}
	s.settings.Defaults()
	s.settingsSeen = time.Now()
	return nil
}

// Index returns the site's host-wide index of system items, rebuilt when
// the site's write generation changes.
func (s *Site) Index(ctx context.Context) (*Snapshot, error) { return s.index.Get(ctx) }

// Registry opens and tracks sites under a data directory.
type Registry struct {
	dir   string
	mu    sync.Mutex
	sites map[string]*Site
}

// NewRegistry returns a registry rooted at dataDir.
func NewRegistry(dataDir string) (*Registry, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "sites"), 0o750); err != nil {
		return nil, err
	}
	return &Registry{dir: dataDir, sites: map[string]*Site{}}, nil
}

// Dir returns the data directory.
func (r *Registry) Dir() string { return r.dir }

func (r *Registry) siteDir(name string) string { return filepath.Join(r.dir, "sites", name) }

// Exists reports whether a site exists on disk.
func (r *Registry) Exists(name string) bool {
	_, err := os.Stat(filepath.Join(r.siteDir(name), "site.db"))
	return err == nil
}

// Create creates a new, empty site.
func (r *Registry) Create(ctx context.Context, name string, settings Settings) (*Site, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("invalid site name %q (use a DNS label, not starting with dav-)", name)
	}
	r.mu.Lock()
	if r.Exists(name) {
		r.mu.Unlock()
		return nil, fmt.Errorf("site %q already exists", name)
	}
	r.mu.Unlock()
	s, err := r.Open(ctx, name, true)
	if err != nil {
		return nil, err
	}
	if err := s.UpdateSettings(ctx, func(cur *Settings) { *cur = settings }); err != nil {
		return nil, err
	}
	return s, nil
}

// Get returns an existing site, opening it if needed.
func (r *Registry) Get(ctx context.Context, name string) (*Site, error) {
	return r.Open(ctx, name, false)
}

// Open opens a site; create allows creating a missing one.
func (r *Registry) Open(ctx context.Context, name string, create bool) (*Site, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sites[name]; ok {
		return s, nil
	}
	if !ValidName(name) {
		return nil, ErrNoSite
	}
	if !create && !r.Exists(name) {
		return nil, ErrNoSite
	}
	st, err := store.Open(r.siteDir(name))
	if err != nil {
		return nil, err
	}
	s := &Site{Name: name, Store: st, Broker: sse.NewBroker(), Users: &identity.Users{DB: st.DB()}}
	s.index = newIndex(s)
	if err := s.loadSettings(); err != nil {
		st.Close()
		return nil, err
	}
	r.sites[name] = s
	return s, nil
}

// List returns the names of all sites on disk.
func (r *Registry) List() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(r.dir, "sites"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && r.Exists(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// Delete closes and removes a site from disk.
func (r *Registry) Delete(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sites[name]; ok {
		s.Broker.CloseAll()
		s.Store.Close()
		delete(r.sites, name)
	}
	if !ValidName(name) {
		return ErrNoSite
	}
	return os.RemoveAll(r.siteDir(name))
}

// ByAlias finds the site that lists host among its aliases.
func (r *Registry) ByAlias(ctx context.Context, host string) (*Site, error) {
	names, err := r.List()
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		s, err := r.Get(ctx, n)
		if err != nil {
			continue
		}
		for _, a := range s.Settings().Aliases {
			if strings.EqualFold(a, host) {
				return s, nil
			}
		}
	}
	return nil, ErrNoSite
}

// Close closes every open site.
func (r *Registry) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for n, s := range r.sites {
		s.Broker.CloseAll()
		s.Store.Close()
		delete(r.sites, n)
	}
}
