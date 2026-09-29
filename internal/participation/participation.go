// Package participation adds pagelike-only capabilities on top of
// PageLove-compatible behaviour, without changing any PageLove request:
//
//   - participation records: every public-plane contribution that adds an
//     identified element (POST) or creates a document is recorded with its
//     contributor, time and the experience version it was made against;
//   - experience identity: the site's version is the digest of its authored
//     baseline (what the author wrote through the authoring plane), and a
//     forked site (remix) carries lineage to its source site and version;
//   - shareable participation views under /-pagelike/p/<id>, tied to the
//     originating experience and version, showing the contribution only to
//     viewers who may read it.
//
// All endpoints live under /-pagelike/, which PageLove does not use.
package participation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html/atom"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/httpapi"
	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

func init() {
	server.Extend(func(s *server.Server) {
		s.Engine.Hooks.AfterWrite = append(s.Engine.Hooks.AfterWrite, record)
	})
	httpapi.RegisterExtension(serve)
}

// ---------------------------------------------------------------- version

type versionCache struct {
	mu   sync.Mutex
	vals map[string]cachedVersion
}

type cachedVersion struct {
	key     string
	version string
}

var versions = &versionCache{vals: map[string]cachedVersion{}}

// Version returns the site's experience version (digest of its authored
// baseline), cached until the authored baseline changes.
func Version(ctx context.Context, s *site.Site) string {
	var n int64
	var last int64
	s.Store.DB().QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(updated_ms),0) FROM authored`).Scan(&n, &last)
	key := fmt.Sprintf("%d/%d", n, last)
	versions.mu.Lock()
	if c, ok := versions.vals[s.Name]; ok && c.key == key {
		versions.mu.Unlock()
		return c.version
	}
	versions.mu.Unlock()
	v, err := s.Store.AuthoredDigest(ctx)
	if err != nil {
		return ""
	}
	versions.mu.Lock()
	versions.vals[s.Name] = cachedVersion{key: key, version: v}
	versions.mu.Unlock()
	return v
}

// ---------------------------------------------------------------- recording

func newID() string {
	var b [9]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// record is an engine AfterWrite hook: it runs inside the write transaction,
// so a participation exists exactly when the contribution committed.
func record(ctx context.Context, w *engine.WriteCtx, res *engine.Result) error {
	op := w.Op
	if op.Plane != engine.Public || res == nil || res.Status >= 300 {
		return nil
	}
	p := store.Participation{ID: newID(), Path: op.Path, Method: op.Method, Version: Version(ctx, w.Site)}
	if pr := op.Principal; pr != nil {
		p.Session = pr.Session
		if pr.Authenticated {
			p.Sub = pr.Sub
			p.Display = pr.DisplayName()
		}
	}
	switch {
	case op.Method == http.MethodPost && op.Range.HasSelector():
		for _, n := range w.Inserted {
			if id, ok := dom.Attr(n, "id"); ok && id != "" {
				p.ElementID = id
				break
			}
		}
		if p.ElementID == "" {
			return nil // unidentified fragments cannot be addressed later
		}
	case (op.Method == http.MethodPut || op.Method == http.MethodPost) && !op.Range.HasSelector() && res.Status == http.StatusCreated:
		if loc := res.Header.Get("Location"); loc != "" {
			p.Path = loc
		}
	default:
		return nil
	}
	return w.Tx.AddParticipation(p)
}

// ---------------------------------------------------------------- endpoints

func serve(w http.ResponseWriter, r *http.Request, x *httpapi.ExtRequest) bool {
	rest := strings.TrimPrefix(r.URL.Path, "/-pagelike/")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	switch {
	case rest == "experience":
		experience(w, r, x)
	case rest == "participations":
		list(w, r, x)
	case strings.HasPrefix(rest, "p/"):
		view(w, r, x, strings.TrimPrefix(rest, "p/"))
	default:
		return false
	}
	return true
}

type participationJSON struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	ElementID   string `json:"element_id,omitempty"`
	Contributor string `json:"contributor"`
	Created     string `json:"created"`
	Version     string `json:"experience_version"`
	ShareURL    string `json:"share_url"`
}

func experience(w http.ResponseWriter, r *http.Request, x *httpapi.ExtRequest) {
	ctx := r.Context()
	set := x.Site.Settings()
	out := map[string]any{
		"site":    x.Site.Name,
		"version": Version(ctx, x.Site),
		"title":   title(ctx, x.Site),
	}
	if set.Lineage != nil {
		out["remix_of"] = map[string]any{"site": set.Lineage.ForkedFrom, "version": set.Lineage.SourceDigest, "forked_at": set.Lineage.ForkedAt, "note": set.Lineage.Note}
	}
	writeJSON(w, out)
}

// readable reports whether the viewer may read the element (or document).
func readable(ctx context.Context, x *httpapi.ExtRequest, p *store.Participation) (bool, string) {
	snap, err := x.Site.Index(ctx)
	if err != nil {
		return false, ""
	}
	op := &engine.ReadOp{Plane: engine.Public, Method: http.MethodGet, Path: p.Path, Principal: x.Principal, Header: http.Header{}}
	doc, err := x.Site.Store.Get(ctx, p.Path)
	if err != nil {
		return false, ""
	}
	if p.ElementID == "" {
		return engine.AuthorizeRead(snap, op, nil), ""
	}
	if doc.IsBlob() || !site.IsMarkup(doc.ContentType) {
		return false, ""
	}
	root, err := dom.Parse(doc.Body)
	if err != nil {
		return false, ""
	}
	sel, err := selector.Compile("#" + selector.EscapeIdent(p.ElementID))
	if err != nil {
		return false, ""
	}
	el := sel.MatchFirst(root)
	if el == nil || !engine.AuthorizeRead(snap, op, el) {
		return false, ""
	}
	return true, dom.OuterHTML(el)
}

func list(w http.ResponseWriter, r *http.Request, x *httpapi.ExtRequest) {
	ctx := r.Context()
	ps, err := x.Site.Store.Participations(ctx, r.URL.Query().Get("path"), 200)
	if err != nil {
		x.Public.Fail(w, r, x, err)
		return
	}
	out := []participationJSON{}
	for i := range ps {
		p := &ps[i]
		if ok, _ := readable(ctx, x, p); !ok {
			continue
		}
		out = append(out, toJSON(p))
	}
	writeJSON(w, map[string]any{"site": x.Site.Name, "version": Version(ctx, x.Site), "participations": out})
}

func toJSON(p *store.Participation) participationJSON {
	who := p.Display
	if who == "" {
		who = "anonymous"
	}
	return participationJSON{ID: p.ID, Path: p.Path, ElementID: p.ElementID, Contributor: who,
		Created: time.UnixMilli(p.CreatedMS).UTC().Format(time.RFC3339), Version: p.Version, ShareURL: "/-pagelike/p/" + p.ID}
}

func view(w http.ResponseWriter, r *http.Request, x *httpapi.ExtRequest, id string) {
	ctx := r.Context()
	p, err := x.Site.Store.Participation(ctx, id)
	if err != nil {
		x.Public.Fail(w, r, x, errdoc.New(http.StatusNotFound, "NotFound", "no such participation"))
		return
	}
	ok, frag := readable(ctx, x, p)
	if !ok {
		// Absent, withdrawn, or not readable by this viewer: never reveal it.
		x.Public.Fail(w, r, x, errdoc.New(http.StatusNotFound, "NotFound", "this contribution is not available"))
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSON(w, map[string]any{"participation": toJSON(p), "experience": map[string]any{"site": x.Site.Name, "title": title(ctx, x.Site), "current_version": Version(ctx, x.Site)}, "html": frag})
		return
	}
	j := toJSON(p)
	exp := title(ctx, x.Site)
	lineage := ""
	if l := x.Site.Settings().Lineage; l != nil {
		lineage = fmt.Sprintf(`<p class="lineage">A remix of <strong>%s</strong> (version %s).</p>`, html.EscapeString(l.ForkedFrom), html.EscapeString(l.SourceDigest))
	}
	body := frag
	if p.ElementID == "" {
		body = fmt.Sprintf(`<p><a href="%s">%s</a></p>`, html.EscapeString(p.Path), html.EscapeString(p.Path))
	}
	page := `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(j.Contributor+" in "+exp) + `</title>
<meta property="og:title" content="` + html.EscapeString(j.Contributor+" in "+exp) + `">
<style>
:root{color-scheme:light dark;--bg:#faf8f4;--fg:#1b1b1b;--muted:#6b6b6b;--card:#fff}
@media (prefers-color-scheme:dark){:root{--bg:#141414;--fg:#eee;--muted:#a0a0a0;--card:#1e1e1e}}
body{margin:0;background:var(--bg);color:var(--fg);font:16px/1.5 system-ui,sans-serif}
main{max-width:640px;margin:0 auto;padding:24px 16px}
.card{background:var(--card);border-radius:14px;padding:16px;box-shadow:0 1px 3px #0002;overflow:hidden}
.card img{max-width:100%;height:auto;border-radius:8px}
.meta{color:var(--muted);font-size:14px}
a{color:inherit}
</style></head><body><main>
<p class="meta"><strong>` + html.EscapeString(j.Contributor) + `</strong> contributed to
<a href="/">` + html.EscapeString(exp) + `</a> · ` + html.EscapeString(j.Created) + `</p>
<article class="card" data-participation="` + html.EscapeString(j.ID) + `" data-experience-version="` + html.EscapeString(j.Version) + `">` + body + `</article>
<p class="meta">Made against experience version <code>` + html.EscapeString(j.Version) + `</code>; current version <code>` + html.EscapeString(Version(ctx, x.Site)) + `</code>.</p>
` + lineage + `
<p><a href="/">Open the experience →</a></p>
</main></body></html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Write([]byte(page))
}

func title(ctx context.Context, s *site.Site) string {
	d, err := s.Store.Get(ctx, "/index.html")
	if err != nil || d.IsBlob() {
		return s.Name
	}
	root, err := dom.Parse(d.Body)
	if err != nil {
		return s.Name
	}
	var t string
	dom.Walk(root, func(n *dom.Node) bool {
		if t == "" && n.DataAtom == atom.Title {
			t = strings.TrimSpace(dom.TextContent(n))
		}
		return t == ""
	})
	if t == "" {
		return s.Name
	}
	return t
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}
