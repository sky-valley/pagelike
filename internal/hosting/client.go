package hosting

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"
)

//go:embed client.js
var clientScript string

func (h *Server) clientJS(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		fail(w, 405)
		return
	}
	parents, _ := json.Marshal(h.cfg.FrameOrigins)
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	if r.Method == "GET" {
		w.Write([]byte(strings.ReplaceAll(clientScript, "__FRAME_ORIGINS__", string(parents))))
	}
}
