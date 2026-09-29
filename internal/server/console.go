package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sky-valley/pagelike/internal/control"
	"github.com/sky-valley/pagelike/internal/site"
)

// console serves a small JSON control API at console.<domain>, authenticated
// with an authoring key scoped to "*" (instance administrator).
//
//	GET    /api/sites              list sites
//	POST   /api/sites {name, default_get}
//	DELETE /api/sites/<name>
func (s *Server) console(w http.ResponseWriter, r *http.Request) {
	key := control.PresentedKey(r.Header.Get("Authorization"))
	k, err := s.Control.Check(r.Context(), key, "")
	if err != nil || !hasAll(k.Sites) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="pagelike console"`)
		http.Error(w, "an instance authoring key (scope *) is required", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	p := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case p == "/api/sites" && r.Method == http.MethodGet:
		names, err := s.Sites.List()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		out := []map[string]any{}
		for _, n := range names {
			st, err := s.Sites.Get(ctx, n)
			if err != nil {
				continue
			}
			set := st.Settings()
			out = append(out, map[string]any{"name": n, "hostname": n + "." + s.Cfg.Domain, "webdav-url": "http://dav-" + n + "." + s.Cfg.Domain + "/", "default_get": set.DefaultGet, "lineage": set.Lineage})
		}
		writeJSON(w, http.StatusOK, out)
	case p == "/api/sites" && r.Method == http.MethodPost:
		var req struct {
			Name       string `json:"name"`
			DefaultGet string `json:"default_get"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if _, err := s.Sites.Create(ctx, req.Name, site.Settings{DefaultGet: req.DefaultGet}); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"name": req.Name, "created": time.Now().UTC()})
	case strings.HasPrefix(p, "/api/sites/") && r.Method == http.MethodDelete:
		name := strings.TrimPrefix(p, "/api/sites/")
		if !s.Sites.Exists(name) {
			http.NotFound(w, r)
			return
		}
		if err := s.Sites.Delete(name); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func hasAll(sites []string) bool {
	for _, s := range sites {
		if s == "*" {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
