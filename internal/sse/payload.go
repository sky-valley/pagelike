package sse

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"

	"github.com/sky-valley/pagelike/internal/store"
)

// Mutation describes one document change as streamed to subscribers.
type Mutation struct {
	Method      string // PUT, POST, DELETE, MOVE
	Selector    string // selector the write addressed ("" for whole-document writes)
	ETag        string // the document's new stored tag, unquoted as PageLove shows it ("" omits it)
	Path        string
	Host        string
	Body        string // mutated HTML fragment as stored; the whole document for a whole-document PUT; empty for DELETE
	Placement   string // POST and MOVE only
	Destination string // MOVE only: destination anchor selector
}

// Render produces the Mutation microdata article used as SSE data
// (docs/spec/sse.md R-SSE-8). Property order is method, selector, etag,
// path, host, body, placement, destination. The body opening tag is
// literally <div itemprop="body"> and it is the last <div> in the article,
// so clients that slice the payload between that tag and the last </div>
// (pagelove-polls) get the raw fragment; later properties are spans.
func (m Mutation) Render() string {
	var b strings.Builder
	b.WriteString(`<article itemscope itemtype="https://pagelove.org/Mutation">` + "\n")
	span := func(prop, v string) {
		b.WriteString(`  <span itemprop="` + prop + `">` + escText(v) + "</span>\n")
	}
	span("method", m.Method)
	span("selector", m.Selector)
	if m.ETag != "" {
		span("etag", m.ETag)
	}
	span("path", m.Path)
	span("host", m.Host)
	b.WriteString(`  <div itemprop="body">` + m.Body + "</div>\n")
	if m.Placement != "" {
		span("placement", m.Placement)
	}
	if m.Destination != "" {
		span("destination", m.Destination)
	}
	b.WriteString(`</article>`)
	return b.String()
}

// WholeDocument reports whether a rendered mutation payload describes a
// whole-document write (its selector is empty, R-SSE-17).
func WholeDocument(data string) bool {
	return strings.Contains(data, `<span itemprop="selector"></span>`)
}

// escText escapes text content as HTML serializers do for property values
// read with textContent: & and < only, so a selector like "main > h1" or a
// quoted ETag reads back verbatim in the raw payload too.
func escText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;").Replace(s)
}

// Reset reasons (docs/spec/sse.md R-SSE-33); no others are ever sent.
const (
	ResetEventsExpired      = "events-expired"
	ResetSessionExpired     = "session-expired"
	ResetSessionInvalidated = "session-invalidated"
)

// Reset renders a StreamReset payload.
func Reset(reason string) string {
	return `<article itemscope itemtype="https://pagelove.org/StreamReset">` + "\n" +
		`  <span itemprop="reason">` + escText(reason) + `</span>` + "\n" +
		`</article>`
}

// WriteEvent writes one SSE event (fields id, event, then one data line per
// line of data; CR LF and lone CR become LF first) and flushes it.
func WriteEvent(w io.Writer, id, name, data string) error {
	var b strings.Builder
	if id != "" {
		b.WriteString("id: " + id + "\n")
	}
	if name != "" {
		b.WriteString("event: " + name + "\n")
	}
	data = strings.ReplaceAll(strings.ReplaceAll(data, "\r\n", "\n"), "\r", "\n")
	for _, line := range strings.Split(data, "\n") {
		b.WriteString("data: " + line + "\n")
	}
	b.WriteString("\n")
	_, err := io.WriteString(w, b.String())
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return err
}

// Keepalive is the comment written every 20 seconds (R-SSE-35).
const Keepalive = ": ping\n\n"

// WriteStored writes a committed event.
func WriteStored(w io.Writer, e store.Event) error { return WriteEvent(w, EventID(e), e.Name, e.Data) }

// Connected is the comment PageLove opens every stream with (live
// 2026-09-28), before the pagelove-connection event.
const Connected = ": connected\n\n"

// Event ids have PageLove's shape (live 2026-09-28, superseding the
// documented "<ms>-<n>" example of R-SSE-29):
// "v1~<first 12 hex digits of sha256(document path)>.<ms>-<n>", where
// "<ms>-<n>" is the stored event's position.
const idPrefix = "v1~"

// EventID renders a stored event's id.
func EventID(e store.Event) string {
	sum := sha256.Sum256([]byte(e.Path))
	return idPrefix + hex.EncodeToString(sum[:])[:12] + "." + e.ID()
}

// ParseEventID parses a Last-Event-ID of EventID's shape into the stored
// position it names. ok is false for anything else, including the bare
// "<ms>-<n>" form: PageLove ignores such ids (the stream is then live
// only, with no reset).
func ParseEventID(id string) (ms, seq int64, ok bool) {
	id = strings.TrimSpace(id)
	if !strings.HasPrefix(id, idPrefix) {
		return 0, 0, false
	}
	key, pos, found := strings.Cut(id[len(idPrefix):], ".")
	if !found || len(key) != 12 {
		return 0, 0, false
	}
	if _, err := hex.DecodeString(key); err != nil {
		return 0, 0, false
	}
	return store.ParseEventID(pos)
}
