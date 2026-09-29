package compose

import (
	"context"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/store"
)

// Transient elements (docs/spec/composing.md §14). An element marked
// p:transient in the requested document is served as the requesting
// session's copy when there is one, else as the document default; its
// marker is stripped; the response is private. Writes to a transient
// element or its descendants change only the session copy (write.go) and
// emit no events. Session copies are stored in the site's transients
// table, keyed by (session, request path, element key); they are
// client-written, so they are served inert: no directive in them runs
// (R-COMP-116).

// TransientTTL is how long a session copy lives after its last write
// (R-COMP-112).
const TransientTTL = 30 * 24 * time.Hour

// transRec is a transient element of the composed text: text[start:end].
type transRec struct {
	start, end int
	key        string
}

// transientKey identifies a transient element of a stored document: its
// id (which a PUT may not change), else its position.
func transientKey(n *html.Node, r *region) string {
	if id := strings.TrimSpace(attr(n, "id")); id != "" {
		return "#" + id
	}
	idx := indexPath(r.root, n)
	parts := make([]string, len(idx))
	for i, x := range idx {
		parts[i] = strconv.Itoa(x)
	}
	return "@" + strings.Join(parts, ".")
}

// transientElement composes a transient element of the requested
// document: the session copy verbatim, or the default composed as usual.
func (c *composer) transientElement(n *html.Node, sp dom.Span, r *region, own *scope, edit *tagEdit, rbind, others []attrDirective, tpl, pag *attrDirective) error {
	key := transientKey(n, r)
	start := c.out.Len()
	if copy, ok := c.sessionCopy(key); ok {
		c.out.WriteString(copy)
	} else {
		replaced, err := c.dispatchAttrs(n, r, own, rbind, others)
		if err != nil {
			return err
		}
		if !replaced {
			if err := c.emitKept(n, sp, r, own, edit, tpl, pag); err != nil {
				return err
			}
		}
	}
	c.trans = append(c.trans, transRec{start: start, end: c.out.Len(), key: key})
	return nil
}

// sessionCopy returns this session's copy of a transient element.
func (c *composer) sessionCopy(key string) (string, bool) {
	if c.sessions == nil {
		c.sessions = loadSessionCopies(c.ctx, c.site.Store, c.sessionID(), c.req.Path)
	}
	v, ok := c.sessions[key]
	return v, ok
}

func (c *composer) sessionID() string {
	if c.req.Principal == nil {
		return ""
	}
	return c.req.Principal.Session
}

// loadSessionCopies reads a session's live copies for one document.
func loadSessionCopies(ctx context.Context, st *store.Site, session, path string) map[string]string {
	out := map[string]string{}
	if session == "" {
		return out
	}
	rows, err := st.DB().QueryContext(ctx, `SELECT key, html FROM transients WHERE session_id = ? AND path = ?`, session, path)
	if err != nil {
		return out
	}
	for rows.Next() {
		var k, h string
		if rows.Scan(&k, &h) == nil {
			out[k] = h
		}
	}
	rows.Close()
	if len(out) == 0 {
		return out
	}
	// Expired copies (last write older than TransientTTL) are ignored.
	cutoff := time.Now().Add(-TransientTTL).UnixMilli()
	if mrows, err := st.DB().QueryContext(ctx, `SELECT key FROM transient_writes WHERE session_id = ? AND path = ? AND written_ms < ?`, session, path, cutoff); err == nil {
		for mrows.Next() {
			var k string
			if mrows.Scan(&k) == nil {
				delete(out, k)
			}
		}
		mrows.Close()
	}
	return out
}

// transientWritesDDL is the table recording when each session copy was
// last written (the transients table has no timestamp).
const transientWritesDDL = `CREATE TABLE IF NOT EXISTS transient_writes (
	session_id TEXT NOT NULL,
	path       TEXT NOT NULL,
	key        TEXT NOT NULL,
	written_ms INTEGER NOT NULL,
	PRIMARY KEY (session_id, path, key)
)`

// saveSessionCopy stores a session copy inside the write transaction.
func saveSessionCopy(tx *store.Tx, session, path, key, markup string) error {
	if _, err := tx.Exec(`INSERT INTO transients(session_id, path, key, html) VALUES (?,?,?,?)
		ON CONFLICT(session_id, path, key) DO UPDATE SET html = excluded.html`, session, path, key, markup); err != nil {
		return err
	}
	if _, err := tx.Exec(transientWritesDDL); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO transient_writes(session_id, path, key, written_ms) VALUES (?,?,?,?)
		ON CONFLICT(session_id, path, key) DO UPDATE SET written_ms = excluded.written_ms`, session, path, key, time.Now().UnixMilli())
	return err
}

// dropSessionCopy removes a session copy (DELETE of the transient element:
// the default is served again).
func dropSessionCopy(tx *store.Tx, session, path, key string) error {
	_, err := tx.Exec(`DELETE FROM transients WHERE session_id = ? AND path = ? AND key = ?`, session, path, key)
	return err
}

// stripTransientMarker removes marker attributes (prefix:transient) from
// the root of a session copy: the copy is served verbatim.
func stripTransientMarker(n *html.Node) {
	out := n.Attr[:0]
	for _, a := range n.Attr {
		if _, local, ok := splitName(a.Key); ok && local == "transient" && a.Namespace == "" {
			continue
		}
		out = append(out, a)
	}
	n.Attr = out
}
