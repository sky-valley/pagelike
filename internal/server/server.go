// Package server routes requests by Host to a site's public plane, its
// authoring plane, or the instance control plane.
package server

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sky-valley/pagelike/internal/control"
	"github.com/sky-valley/pagelike/internal/dav"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/httpapi"
	"github.com/sky-valley/pagelike/internal/site"
)

// Config configures a server.
type Config struct {
	Domain     string // base domain; sites are served at <site>.<Domain>
	TrustProxy bool
	DevAuth    bool
}

// Server is the pagelike HTTP server.
type Server struct {
	Cfg     Config
	Sites   *site.Registry
	Control *control.DB
	Engine  *engine.Engine
	Public  *httpapi.Public
	Dav     *dav.Handler
	Log     *slog.Logger
	stopJan chan struct{}
}

// New wires a server.
func New(cfg Config, sites *site.Registry, ctl *control.DB, log *slog.Logger) *Server {
	eng := &engine.Engine{Domain: cfg.Domain}
	s := &Server{
		Cfg: cfg, Sites: sites, Control: ctl, Engine: eng, Log: log,
		Public: &httpapi.Public{Engine: eng, Log: log, TrustProxy: cfg.TrustProxy, DevAuth: cfg.DevAuth, Queries: map[string]httpapi.QueryHandler{}},
		Dav:    &dav.Handler{Engine: eng, Control: ctl, Log: log},
	}
	for _, f := range extensions {
		f(s)
	}
	return s
}

// extensions let feature packages wire themselves into new servers
// (composition, schemas, reactions, Sessel queries…).
var extensions []func(*Server)

// Extend registers a function applied to every new server.
func Extend(f func(*Server)) { extensions = append(extensions, f) }

// ServeHTTP routes by host.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(r.Host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(host, ".")
	dom := strings.ToLower(s.Cfg.Domain)
	ctx := r.Context()
	if strings.HasSuffix(host, "."+dom) {
		label := strings.TrimSuffix(host, "."+dom)
		if !strings.Contains(label, ".") {
			if strings.HasPrefix(label, "dav-") {
				st, err := s.Sites.Get(ctx, strings.TrimPrefix(label, "dav-"))
				if err != nil {
					s.noSite(w, r, host)
					return
				}
				s.Dav.Serve(w, r, st)
				return
			}
			if label == "console" {
				s.console(w, r)
				return
			}
			st, err := s.Sites.Get(ctx, label)
			if err != nil {
				s.noSite(w, r, host)
				return
			}
			s.Public.Serve(w, r, st)
			return
		}
	}
	if host == dom {
		s.landing(w, r)
		return
	}
	if st, err := s.Sites.ByAlias(ctx, host); err == nil {
		s.Public.Serve(w, r, st)
		return
	}
	s.noSite(w, r, host)
}

func (s *Server) noSite(w http.ResponseWriter, r *http.Request, host string) {
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, "<!DOCTYPE html><html><body itemscope itemtype=\"http://pagelove.org/Error\"><h1 itemprop=\"name\">Not Found</h1><meta itemprop=\"statusCode\" content=\"404\"><p itemprop=\"description\">No site is served at %s</p></body></html>", html.EscapeString(host))
}

func (s *Server) landing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>pagelike</title></head><body><h1>pagelike</h1><p>Sites are served at <code>&lt;site&gt;.%s</code>; their authoring (WebDAV) mounts at <code>dav-&lt;site&gt;.%s</code>.</p></body></html>`, html.EscapeString(s.Cfg.Domain), html.EscapeString(s.Cfg.Domain))
}

// StartJanitor prunes expired stream events periodically.
func (s *Server) StartJanitor(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				names, _ := s.Sites.List()
				for _, n := range names {
					if st, err := s.Sites.Get(ctx, n); err == nil {
						st.Store.PruneEvents(ctx, time.Now().Add(-httpapi.Retention))
					}
				}
			}
		}
	}()
}

// ErrNotLoopback is returned when dev auth is requested on a public listener.
var ErrNotLoopback = errors.New("--dev-insecure-auth requires a loopback listen address")

// CheckListen enforces that development auth only runs on loopback.
func CheckListen(addr string, devAuth bool) error {
	if !devAuth {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return ErrNotLoopback
	}
	return nil
}
