package hosting

import (
	_ "embed"
	"net/http"

	"github.com/sky-valley/pagelike/internal/site"
)

//go:embed manage.html
var managePage []byte

func (h *Server) manage(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		fail(w, 405)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == "GET" {
		w.Write(managePage)
	}
}

func (h *Server) report(w http.ResponseWriter, r *http.Request, s *site.Site, sid string) {
	var input struct {
		Contribution string `json:"contribution"`
		Reason       string `json:"reason"`
	}
	if !decode(w, r, &input, 4096) {
		return
	}
	if input.Contribution != "" && !contributionID.MatchString(input.Contribution) || len(input.Reason) < 1 || len(input.Reason) > 1000 {
		fail(w, 400)
		return
	}
	var remote string
	if s.Store.DB().QueryRowContext(r.Context(), `SELECT remote_session FROM hosted_sessions WHERE id=?`, sid).Scan(&remote) != nil {
		fail(w, 503)
		return
	}
	status := h.identity(r.Context(), "report", map[string]string{"site": s.Name, "session": remote, "contribution": input.Contribution, "reason": input.Reason}, nil)
	if status != 200 {
		fail(w, status)
		return
	}
	w.WriteHeader(204)
}
