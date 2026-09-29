// Package dav serves a site's authoring plane: a WebDAV mount of the live
// site (PageLove's dombase-webdav) plus authoring-scope QUERY.
//
// Authoring writes bypass application authorization and transition
// constraints (documented WebDAV behaviour) but still pass schema and shape
// validation; like PageLove they emit no SSE events (live 2026-09-28).
// Behaviour follows docs/spec/protocol.md §10 (R-PROTO-110..124) as
// reconciled with live PageLove (docs/compat/decisions.md): Bearer or Basic
// authoring keys (401 with a Basic challenge otherwise), byte-exact reads,
// implicit parent collections for PUT, MKCOL on an existing collection is
// 405 and under a missing parent 409, PROPFIND defaults to Depth 1 and
// revalidates with per-answer ETags (Vary: Depth on collections, 304), and
// errors are PageLove's short Error/NotFound and Error/Internal articles or
// the https://pagelove.org/Error article wrapping the pipeline's document.
package dav

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sky-valley/pagelike/internal/control"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/selector"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

// Handler serves the authoring plane.
type Handler struct {
	Engine  *engine.Engine
	Control *control.DB
	Log     *slog.Logger
}

// davMethods are the methods the authoring plane implements.
const davMethods = "OPTIONS, GET, HEAD, PUT, DELETE, MKCOL, PROPFIND, PROPPATCH, MOVE, COPY, LOCK, UNLOCK, QUERY"

// acceptQuery is the only query type the authoring plane serves (R-PROTO-44).
const acceptQuery = "text/css-selector"

// Serve handles one authoring request for site s.
func (h *Handler) Serve(w http.ResponseWriter, r *http.Request, s *site.Site) {
	w = engine.ConventionalHeaders(w)
	if ok, presented := h.authenticate(r, s); !ok {
		// As PageLove answers (live 2026-09-29): a Basic challenge, the DAV
		// headers and a short Error/Internal article.
		w.Header().Set("WWW-Authenticate", `Basic realm="WebDAV"`)
		w.Header().Set("DAV", "1, 2")
		w.Header().Set("MS-Author-Via", "DAV")
		msg := "Authentication required"
		if presented {
			msg = "Bearer token rejected"
		}
		h.fail(w, r, queryError(http.StatusUnauthorized, "%s", msg))
		return
	}
	p, err := engine.NormalizePath(r.URL.Path)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	switch r.Method {
	case http.MethodOptions:
		w.Header().Set("DAV", "1, 2")
		w.Header().Set("Allow", davMethods)
		w.Header().Set("Accept-Query", acceptQuery)
		w.Header().Set("MS-Author-Via", "DAV")
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	case http.MethodGet, http.MethodHead:
		h.get(w, r, s, p)
	case http.MethodPut:
		h.put(w, r, s, p)
	case http.MethodDelete:
		h.delete(w, r, s, p)
	case "MKCOL":
		h.mkcol(w, r, s, p)
	case "PROPFIND":
		h.propfind(w, r, s, p)
	case "PROPPATCH":
		h.proppatch(w, r, p)
	case "MOVE", "COPY":
		h.moveCopy(w, r, s, p)
	case "LOCK":
		h.lock(w, r, p)
	case "UNLOCK":
		w.WriteHeader(http.StatusNoContent)
	case "QUERY":
		h.query(w, r, s, p)
	default:
		w.Header().Set("Allow", davMethods)
		h.fail(w, r, errdoc.New(http.StatusMethodNotAllowed, "MethodNotAllowed", "method %s is not supported on the authoring plane", r.Method))
	}
}

// authenticate checks the site's authoring key, sent as a Bearer token (all
// official tooling) or as the password of Basic credentials (the QUERY docs'
// examples; the user name is ignored). presented reports whether any
// credential was offered.
func (h *Handler) authenticate(r *http.Request, s *site.Site) (ok, presented bool) {
	key := control.PresentedKey(r.Header.Get("Authorization"))
	if key == "" {
		if user, pw, basic := r.BasicAuth(); basic {
			key = pw
			if key == "" {
				key = user
			}
		}
	}
	if key == "" {
		return false, r.Header.Get("Authorization") != ""
	}
	if h.Control == nil {
		return false, true
	}
	_, err := h.Control.Check(r.Context(), key, s.Name)
	return err == nil, true
}

func (h *Handler) author() *identity.Principal {
	return &identity.Principal{Authenticated: true, Sub: "author", Username: "author"}
}

// get returns stored bytes exactly, never composed (R-PROTO-113), with the
// content ETag that PROPFIND reports as getetag.
func (h *Handler) get(w http.ResponseWriter, r *http.Request, s *site.Site, p string) {
	if strings.HasSuffix(p, "/") {
		// A collection GET answers with its Depth 1 listing.
		h.propfindAnswer(w, r, s, p, "1")
		return
	}
	doc, err := s.Store.Get(r.Context(), p)
	if err != nil {
		h.fail(w, r, notFound(err, p))
		return
	}
	mod := time.UnixMilli(doc.ModifiedMS).UTC()
	w.Header().Set("ETag", doc.ETag)
	w.Header().Set("Content-Type", doc.ContentType)
	if doc.IsBlob() {
		f, err := s.Store.OpenBlob(doc.BlobSHA)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		defer f.Close()
		http.ServeContent(w, r, "", mod, f)
		return
	}
	http.ServeContent(w, r, "", mod, bytes.NewReader(doc.Body))
}

// put stores a file byte-for-byte: 201 when new, 200 when replaced;
// missing parent collections are implied (R-PROTO-114).
func (h *Handler) put(w http.ResponseWriter, r *http.Request, s *site.Site, p string) {
	body, err := engine.ReadBody(w, r, s.Settings().MaxBodyBytes)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	op := &engine.Op{Plane: engine.Authoring, Method: http.MethodPut, Path: p, Body: body, ContentType: r.Header.Get("Content-Type"),
		IfMatch: r.Header.Get("If-Match"), IfNoneMatch: r.Header.Get("If-None-Match"), Principal: h.author(), Host: r.Host, Header: r.Header,
		Range: engine.ParseRange(r.Header.Get("Range"))}
	res, err := h.Engine.Write(r.Context(), s, op)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	for k, v := range res.Header {
		w.Header()[k] = v
	}
	if !op.Range.HasSelector() {
		// A file upload echoes what was stored, with its type and tag and,
		// on a replace, Accept-Ranges (live 2026-09-28).
		w.Header().Del("Last-Modified")
		if res.Status == http.StatusOK {
			w.Header().Set("Accept-Ranges", "bytes")
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(res.Body)))
	w.WriteHeader(res.Status)
	w.Write(res.Body)
}

// delete removes a file, or a collection with everything under it.
func (h *Handler) delete(w http.ResponseWriter, r *http.Request, s *site.Site, p string) {
	ctx := r.Context()
	if strings.HasSuffix(p, "/") {
		if err := h.Engine.DeleteCollection(ctx, s, p, r.Host); err != nil {
			h.fail(w, r, notFound(err, p))
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	op := &engine.Op{Plane: engine.Authoring, Method: http.MethodDelete, Path: p, IfMatch: r.Header.Get("If-Match"),
		IfNoneMatch: r.Header.Get("If-None-Match"), Principal: h.author(), Host: r.Host, Header: r.Header,
		Range: engine.ParseRange(r.Header.Get("Range"))}
	res, err := h.Engine.Write(ctx, s, op)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	for k, v := range res.Header {
		w.Header()[k] = v
	}
	// A missing file is deleted too: 204 with the empty-content tag, and
	// nothing else (live 2026-09-28, superseding R-PROTO-116's 404).
	w.Header().Del("Last-Modified")
	w.WriteHeader(res.Status)
}

// conflict is the authoring plane's 409: a Conflict article whose detail
// names the problem.
func conflict(kind, msg string) *errdoc.Error {
	e := errdoc.New(http.StatusConflict, kind, "%s", msg)
	e.Document = errdoc.ProblemsItem(kind, msg)
	return e
}

// mkcolAllow is the Allow of PageLove's 405 for MKCOL on an existing
// collection (live 2026-09-29).
const mkcolAllow = "OPTIONS, GET, HEAD, PUT, POST, DELETE, COPY, MOVE, LOCK, UNLOCK, PROPFIND, QUERY"

// mkcol creates a collection (R-PROTO-115, as reconciled with live
// PageLove 2026-09-28/29, as RFC 4918 has it): its parent must exist (409
// ParentDirectoryMissing), and MKCOL on an existing collection is 405 with
// a DirectoryAlreadyExists detail. A new collection answers 201 with an
// all-zero tag.
func (h *Handler) mkcol(w http.ResponseWriter, r *http.Request, s *site.Site, p string) {
	ctx := r.Context()
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	exists, err := s.Store.DirExists(ctx, p)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if _, ferr := s.Store.Get(ctx, strings.TrimSuffix(p, "/")); exists || ferr == nil {
		e := errdoc.New(http.StatusMethodNotAllowed, "DirectoryAlreadyExists", "Directory already exists")
		e.Document = errdoc.ProblemsItem("DirectoryAlreadyExists", "Directory already exists")
		w.Header().Set("Allow", mkcolAllow)
		h.fail(w, r, e)
		return
	}
	if parent := path.Dir(strings.TrimSuffix(p, "/")); parent != "/" {
		if ok, err := s.Store.DirExists(ctx, parent+"/"); err != nil || !ok {
			h.fail(w, r, conflict("ParentDirectoryMissing", "Parent directory does not exist"))
			return
		}
	}
	s.WriteMu.Lock()
	defer s.WriteMu.Unlock()
	if _, err := s.Store.Update(ctx, func(tx *store.Tx) error { return tx.MkDir(p) }); err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("ETag", `"`+strings.Repeat("0", 64)+`"`)
	w.WriteHeader(http.StatusCreated)
}

// moveCopy implements WebDAV MOVE and COPY of files and collections
// (RFC 4918; R-PROTO-122): Destination on the same host, Overwrite honoured,
// 201 when the destination is new and 204 when it was replaced.
func (h *Handler) moveCopy(w http.ResponseWriter, r *http.Request, s *site.Site, p string) {
	dest := strings.TrimSpace(r.Header.Get("Destination"))
	u, err := url.Parse(dest)
	if dest == "" || err != nil {
		h.fail(w, r, errdoc.New(http.StatusBadRequest, "BadDestination", "a Destination header is required"))
		return
	}
	if u.Host != "" && !strings.EqualFold(hostOnly(u.Host), hostOnly(r.Host)) {
		h.fail(w, r, errdoc.New(http.StatusBadGateway, "ForeignDestination", "Destination %q names another host", dest))
		return
	}
	dp, err := engine.NormalizePath(u.Path)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	overwrite := !strings.EqualFold(strings.TrimSpace(r.Header.Get("Overwrite")), "F")
	existed, err := h.Engine.AuthoringCopy(r.Context(), s, p, dp, r.Method == "MOVE", overwrite, r.Host)
	if err != nil {
		h.fail(w, r, notFound(err, p))
		return
	}
	if existed {
		w.WriteHeader(http.StatusNoContent)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
}

func (h *Handler) lock(w http.ResponseWriter, r *http.Request, p string) {
	// Advisory locks are accepted but not enforced: the live site is shared
	// with application writers anyway (PageLove docs, WebDAV → Live site).
	token := fmt.Sprintf("opaquelocktoken:%x", sha256.Sum256([]byte(p+time.Now().String())))
	w.Header().Set("Lock-Token", "<"+token+">")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><D:prop xmlns:D="DAV:"><D:lockdiscovery><D:activelock><D:locktype><D:write/></D:locktype><D:lockscope><D:exclusive/></D:lockscope><D:depth>0</D:depth><D:timeout>Second-3600</D:timeout><D:locktoken><D:href>%s</D:href></D:locktoken><D:lockroot><D:href>%s</D:href></D:lockroot></D:activelock></D:lockdiscovery></D:prop>`, token, xmlEsc(hrefOf(p)))
}

func (h *Handler) proppatch(w http.ResponseWriter, r *http.Request, p string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?><D:multistatus xmlns:D="DAV:"><D:response><D:href>%s</D:href><D:propstat><D:prop/><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response></D:multistatus>`, xmlEsc(hrefOf(p)))
}

// propfind answers PROPFIND at Depth 0 or 1; a missing Depth means 1 and
// infinity is refused (R-PROTO-117).
func (h *Handler) propfind(w http.ResponseWriter, r *http.Request, s *site.Site, p string) {
	var depth string
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Depth"))) {
	case "", "1":
		depth = "1"
	case "0":
		depth = "0"
	case "infinity":
		e := errdoc.New(http.StatusForbidden, "PropfindFiniteDepth", "Depth: infinity is not supported; use Depth 0 or 1 (DAV:propfind-finite-depth)")
		e.Detail = `<D:error xmlns:D="DAV:"><D:propfind-finite-depth/></D:error>`
		h.fail(w, r, e)
		return
	default:
		h.fail(w, r, errdoc.New(http.StatusBadRequest, "BadDepth", "Depth must be 0, 1 or infinity"))
		return
	}
	h.propfindAnswer(w, r, s, p, depth)
}

// entry is one D:response of a listing.
type entry struct {
	href     string
	dir      bool
	doc      *store.Document
	modified int64
	etag     string // D:getetag: the content tag of a file, the listing tag of a collection
}

// propfindAnswer writes a listing. Its ETag identifies the answer (target
// and depth): for a collection it is also the collection's own getetag and
// changes when anything below it changes; for a file it is a properties tag,
// distinct from the content tag in getetag (R-PROTO-118/119). If-None-Match
// with the current tag for the same depth is 304.
func (h *Handler) propfindAnswer(w http.ResponseWriter, r *http.Request, s *site.Site, p, depth string) {
	ctx := r.Context()
	var entries []entry
	var respTag string
	if !strings.HasSuffix(p, "/") {
		d, err := s.Store.Get(ctx, p)
		if err != nil {
			// Maybe a collection addressed without its slash.
			if ok, _ := s.Store.DirExists(ctx, p+"/"); !ok {
				h.fail(w, r, notFound(err, p))
				return
			}
			p += "/"
		} else {
			respTag = quotedHash("propfind:sha256", fmt.Sprintf("%s|%s|%s|%d|%d", d.Path, d.ETag, d.ContentType, d.ModifiedMS, d.Size))
			entries = []entry{{href: p, doc: d, modified: d.ModifiedMS, etag: d.ETag}}
		}
	}
	if strings.HasSuffix(p, "/") {
		ok, err := s.Store.DirExists(ctx, p)
		if err != nil || !ok {
			h.fail(w, r, notFound(store.ErrNotFound, p))
			return
		}
		docs, err := s.Store.List(ctx, p, false)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		dirs, err := s.Store.Dirs(ctx, p)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		self := entry{href: p, dir: true, modified: newest(docs, p)}
		if depth == "0" {
			self.etag = subtreeTag(p, docs, dirs)
			entries = []entry{self}
		} else {
			children := childEntries(p, docs, dirs)
			hsh := sha256.New()
			fmt.Fprintf(hsh, "%s\n", p)
			for _, c := range children {
				ct := ""
				if c.doc != nil {
					ct = c.doc.ContentType
				}
				fmt.Fprintf(hsh, "%s|%v|%s|%s|%d\n", c.href, c.dir, c.etag, ct, c.modified)
			}
			self.etag = `"sha256:` + hex.EncodeToString(hsh.Sum(nil)) + `"`
			entries = append([]entry{self}, children...)
		}
		respTag = self.etag
	}
	w.Header().Set("ETag", respTag)
	if strings.HasSuffix(p, "/") {
		// Only a collection's listing depends on Depth (live 2026-09-28:
		// file answers and errors carry no Vary).
		w.Header().Set("Vary", "Depth")
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" && engine.IfNoneMatch(inm, true, respTag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	// PageLove's listing, byte for byte in layout (live 2026-09-28): a
	// collection shows its type and tag only; a file its type, media type,
	// length, modification time and content tag.
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="utf-8" ?>` + "\n" + `<D:multistatus xmlns:D="DAV:">` + "\n")
	for _, e := range entries {
		b.WriteString("  <D:response>\n    <D:href>" + xmlEsc(hrefOf(e.href)) + "</D:href>\n    <D:propstat>\n      <D:prop>\n")
		if e.dir {
			b.WriteString("      <D:resourcetype><D:collection/></D:resourcetype>\n")
		} else {
			b.WriteString("      <D:resourcetype/>\n")
			b.WriteString("      <D:getcontenttype>" + xmlEsc(e.doc.ContentType) + "</D:getcontenttype>\n")
			b.WriteString(fmt.Sprintf("      <D:getcontentlength>%d</D:getcontentlength>\n", e.doc.Size))
			b.WriteString("      <D:getlastmodified>" + time.UnixMilli(e.modified).UTC().Format(http.TimeFormat) + "</D:getlastmodified>\n")
		}
		b.WriteString("      <D:getetag>" + xmlEsc(e.etag) + "</D:getetag>\n")
		b.WriteString("      </D:prop>\n      <D:status>HTTP/1.1 200 OK</D:status>\n    </D:propstat>\n  </D:response>\n")
	}
	b.WriteString("</D:multistatus>\n")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(b.Len()))
	w.WriteHeader(http.StatusMultiStatus)
	if r.Method != http.MethodHead {
		w.Write(b.Bytes())
	}
}

// childEntries lists the direct children of collection p, by name: files
// with their content tags, sub-collections with their Depth 0 tags.
func childEntries(p string, docs []*store.Document, dirs []string) []entry {
	byName := map[string]*entry{}
	for _, d := range docs {
		rest := strings.TrimPrefix(d.Path, p)
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			name := p + rest[:i+1]
			if byName[name] == nil {
				byName[name] = &entry{href: name, dir: true}
			}
			continue
		}
		byName[d.Path] = &entry{href: d.Path, doc: d, modified: d.ModifiedMS, etag: d.ETag}
	}
	for _, dp := range dirs {
		rest := strings.TrimPrefix(dp, p)
		if i := strings.IndexByte(rest, '/'); rest != "" && i >= 0 {
			if name := p + rest[:i+1]; byName[name] == nil {
				byName[name] = &entry{href: name, dir: true}
			}
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]entry, 0, len(names))
	for _, n := range names {
		e := byName[n]
		if e.dir {
			e.modified = newest(docs, e.href)
			e.etag = subtreeTag(e.href, docs, dirs)
		}
		out = append(out, *e)
	}
	return out
}

// subtreeTag is a collection's Depth 0 tag: it changes whenever anything
// below the collection changes, and only then.
func subtreeTag(p string, docs []*store.Document, dirs []string) string {
	hsh := sha256.New()
	fmt.Fprintf(hsh, "%s\n", p)
	for _, d := range docs {
		if strings.HasPrefix(d.Path, p) {
			fmt.Fprintf(hsh, "%s|%s|%s|%d\n", d.Path, d.ETag, d.ContentType, d.ModifiedMS)
		}
	}
	for _, dp := range dirs {
		if strings.HasPrefix(dp, p) {
			fmt.Fprintf(hsh, "dir %s\n", dp)
		}
	}
	return `"sha256:` + hex.EncodeToString(hsh.Sum(nil)) + `"`
}

func newest(docs []*store.Document, prefix string) int64 {
	var n int64
	for _, d := range docs {
		if strings.HasPrefix(d.Path, prefix) && d.ModifiedMS > n {
			n = d.ModifiedMS
		}
	}
	return n
}

func quotedHash(kind, s string) string {
	sum := sha256.Sum256([]byte(s))
	return `"` + kind + ":" + hex.EncodeToString(sum[:]) + `"`
}

// query implements authoring-plane QUERY (text/css-selector) over the raw
// stored markup, with host, subtree or single-document scope selected by
// the target URI; the answer is always multipart (R-PROTO-50..57). Checks
// run in the order 415 → 406 → 422 → 404.
func (h *Handler) query(w http.ResponseWriter, r *http.Request, s *site.Site, p string) {
	ctx := r.Context()
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !strings.EqualFold(ct, acceptQuery) {
		w.Header().Set("Accept-Query", acceptQuery)
		h.fail(w, r, queryError(http.StatusUnsupportedMediaType, "QUERY requires Content-Type: %s", acceptQuery))
		return
	}
	if acc := r.Header.Get("Accept"); strings.TrimSpace(acc) != "" && engine.AcceptQuality(acc, "multipart/mixed", false) == 0 {
		h.fail(w, r, queryError(http.StatusNotAcceptable, "QUERY responds with multipart/mixed; Accept did not permit it"))
		return
	}
	body, err := engine.ReadBody(w, r, s.Settings().MaxBodyBytes)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		h.fail(w, r, queryError(http.StatusUnprocessableEntity, "QUERY body must contain a CSS selector"))
		return
	}
	sel, err := selector.Compile(text)
	if err != nil {
		h.fail(w, r, queryError(http.StatusUnprocessableEntity, "Invalid CSS selector: Invalid CSS selector: %v", err))
		return
	}
	var docs []*store.Document
	if strings.HasSuffix(p, "/") {
		all, err := s.Store.List(ctx, p, true)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		for _, d := range all {
			if !strings.HasPrefix(d.Path, engine.ReservedPrefix) && !d.IsBlob() && site.IsMarkup(d.ContentType) {
				docs = append(docs, d)
			}
		}
		if len(docs) == 0 && p != "/" {
			h.fail(w, r, errdoc.New(http.StatusNotFound, "NotFound", "%s holds no documents", p))
			return
		}
	} else {
		d, err := s.Store.Get(ctx, p)
		if err != nil {
			h.fail(w, r, notFound(err, p))
			return
		}
		if d.IsBlob() || !site.IsMarkup(d.ContentType) {
			h.fail(w, r, queryError(http.StatusUnprocessableEntity, "Selector operations require HTML documents, but %s has content type %s", d.Path, d.ContentType))
			return
		}
		docs = []*store.Document{d}
	}
	var parts []engine.Part
	var newest int64
	op := &engine.ReadOp{Plane: engine.Authoring, Method: http.MethodGet, Principal: h.author()}
	for _, d := range docs {
		op.Path = d.Path
		ps, err := h.Engine.QueryParts(ctx, s, nil, d, op, nil, sel)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		if len(ps) > 0 && d.ModifiedMS > newest {
			newest = d.ModifiedMS
		}
		parts = append(parts, ps...)
	}
	etag := engine.MultipartETag(text, parts)
	w.Header().Set("ETag", etag)
	w.Header().Set("Accept-Ranges", "bytes")
	if newest > 0 {
		w.Header().Set("Last-Modified", time.UnixMilli(newest).UTC().Format(http.TimeFormat))
	}
	if inm := strings.TrimSpace(r.Header.Get("If-None-Match")); inm != "" {
		if (inm == "*" && len(parts) > 0) || (inm != "*" && engine.IfNoneMatch(inm, true, etag)) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	out, ctype := engine.MultipartBody(parts)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	w.WriteHeader(http.StatusOK)
	w.Write(out)
}

// notFound words a missing path as PageLove's authoring plane does (live
// 2026-09-28).
func notFound(err error, p string) error {
	if errors.Is(err, store.ErrNotFound) {
		if strings.HasSuffix(p, "/") {
			return errdoc.New(http.StatusNotFound, "NotFound", "Collection not found: %s", p)
		}
		return errdoc.New(http.StatusNotFound, "NotFound", "Not found: %s", p)
	}
	return err
}

// queryKind marks a refusal rendered as a short Error/Internal article: a
// refused authoring QUERY, a missing or rejected authoring key.
const queryKind = "AuthoringQueryRefused"

func queryError(status int, format string, args ...any) *errdoc.Error {
	return errdoc.New(status, queryKind, format, args...)
}

// render picks the authoring plane's document for e (R-PROTO-120, as
// observed live 2026-09-28): a short Error/NotFound article for a missing
// path, a short Error/Internal article for a refused QUERY, and for what the
// write pipeline refused (preconditions, validation, conflicts) the generic
// article carrying the pipeline's own document as its detail.
func render(e *errdoc.Error) string {
	switch {
	case e.Status == http.StatusNotFound:
		return errdoc.RenderShort("NotFound", e.Status, e.Message)
	case e.Kind == queryKind:
		return errdoc.RenderShort("Internal", e.Status, e.Message)
	case e.Status == http.StatusConflict && e.Document != "":
		return errdoc.RenderWrapped(e.Status, "Conflict", e.Document)
	case e.Document != "" || e.Shape != errdoc.ShapeAuto:
		return errdoc.RenderWrapped(e.Status, "Internal", e.RenderPublic(""))
	}
	return e.RenderAuthoring()
}

// fail writes the authoring-plane error document (R-PROTO-120).
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var e *errdoc.Error
	if !errors.As(err, &e) {
		if errors.Is(err, context.Canceled) {
			return
		}
		if h.Log != nil {
			h.Log.Error("authoring request failed", "err", err, "method", r.Method, "path", r.URL.Path)
		}
		e = errdoc.New(http.StatusInternalServerError, "InternalError", "internal error")
	}
	w.Header().Del("ETag")
	for k, v := range e.Headers {
		w.Header()[k] = v
	}
	if e.Status == http.StatusPreconditionFailed {
		w.Header().Del("ETag") // PageLove's authoring 412 carries no tag
	}
	body := render(e)
	w.Header().Set("Content-Type", errdoc.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(e.Status)
	if r.Method != http.MethodHead {
		io.WriteString(w, body)
	}
}

// hrefOf percent-encodes a path for D:href.
func hrefOf(p string) string { return (&url.URL{Path: p}).EscapedPath() }

func hostOnly(h string) string {
	if i := strings.LastIndexByte(h, ':'); i >= 0 && !strings.Contains(h[i:], "]") {
		return h[:i]
	}
	return h
}

// xmlEsc escapes XML character data. Quotes stay literal so a getetag reads
// back as the exact header value (deploy scripts grep it into If-Match).
func xmlEsc(s string) string { return xmlText.Replace(s) }

var xmlText = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
