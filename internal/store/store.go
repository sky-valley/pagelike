// Package store persists one site's authoritative state in SQLite.
//
// Every document row, the events derived from a mutation, and any outbox
// entries commit in a single SQLite transaction, so replayable events always
// agree with document state after a crash. Opaque upload bodies live in a
// content-addressed blob directory next to the database; a blob file is made
// durable before the transaction that references it commits, and
// unreferenced blobs are swept when the site is opened.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
	_ "modernc.org/sqlite"
)

// ErrNotFound reports that a document does not exist.
var ErrNotFound = errors.New("store: not found")

// Document is one stored resource. HTML and XML documents keep their markup
// in Body; opaque blobs keep their bytes in the blob directory (BlobSHA).
type Document struct {
	Path        string
	ContentType string
	Body        []byte
	BlobSHA     string
	Size        int64
	Version     int64
	ETag        string // quoted strong ETag of the stored representation
	ModifiedMS  int64
	CreatedMS   int64
}

// IsBlob reports whether the document is an opaque blob.
func (d *Document) IsBlob() bool { return d.BlobSHA != "" }

// Event is a committed, replayable change notification.
type Event struct {
	Seq           int64
	TimeMS        int64
	Path          string
	Name          string // "mutation" (future: other SSE event names)
	Data          string // SSE data payload (HTML microdata)
	OriginSession string
	OriginConn    string
	// Also lists further document paths whose live subscribers receive the
	// event (pages that include Path); not persisted.
	Also []string

	// Targets are the elements the change produced (for a removal: the
	// element removed, in the tree it was removed from), attached by the
	// writer so each stream can check read access to them at delivery
	// (docs/spec/sse.md R-SSE-28). They live in memory only: replayed
	// events have none.
	Targets []*html.Node
}

// ID renders the SSE event id, "<ms>-<seq>" (shape follows PageLove's ids):
// the commit time and the site-wide sequence number, so the same event has
// the same id on every stream and on replay.
func (e Event) ID() string { return fmt.Sprintf("%d-%d", e.TimeMS, e.Seq) }

// ParseEventID parses an SSE event id "<ms>-<seq>" (both decimal).
func ParseEventID(id string) (ms, seq int64, ok bool) {
	a, b, found := strings.Cut(strings.TrimSpace(id), "-")
	if !found {
		return 0, 0, false
	}
	ms, err1 := strconv.ParseInt(a, 10, 64)
	seq, err2 := strconv.ParseInt(b, 10, 64)
	if err1 != nil || err2 != nil || ms < 0 || seq < 0 || a[0] == '+' || b[0] == '+' {
		return 0, 0, false
	}
	return ms, seq, true
}

// Site is an open site database.
type Site struct {
	dir string
	db  *sql.DB
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS documents (
	path         TEXT PRIMARY KEY,
	content_type TEXT NOT NULL,
	body         BLOB,
	blob_sha     TEXT NOT NULL DEFAULT '',
	size         INTEGER NOT NULL,
	version      INTEGER NOT NULL,
	etag         TEXT NOT NULL,
	created_ms   INTEGER NOT NULL,
	modified_ms  INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
	seq            INTEGER PRIMARY KEY AUTOINCREMENT,
	ts_ms          INTEGER NOT NULL,
	path           TEXT NOT NULL,
	name           TEXT NOT NULL,
	data           TEXT NOT NULL,
	origin_session TEXT NOT NULL DEFAULT '',
	origin_conn    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS events_path_seq ON events(path, seq);
CREATE TABLE IF NOT EXISTS users (
	sub            TEXT PRIMARY KEY,
	email          TEXT NOT NULL DEFAULT '',
	email_verified INTEGER NOT NULL DEFAULT 0,
	name           TEXT NOT NULL DEFAULT '',
	roles          TEXT NOT NULL DEFAULT '',
	password_hash  TEXT NOT NULL DEFAULT '',
	created_ms     INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	sub        TEXT NOT NULL DEFAULT '',
	claims     TEXT NOT NULL DEFAULT '',
	created_ms INTEGER NOT NULL,
	expires_ms INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS transients (
	session_id TEXT NOT NULL,
	path       TEXT NOT NULL,
	key        TEXT NOT NULL,
	html       TEXT NOT NULL,
	PRIMARY KEY (session_id, path, key)
);
CREATE TABLE IF NOT EXISTS outbox (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	created_ms  INTEGER NOT NULL,
	kind        TEXT NOT NULL,
	request     TEXT NOT NULL,
	attempts    INTEGER NOT NULL DEFAULT 0,
	max_attempts INTEGER NOT NULL DEFAULT 1,
	next_ms     INTEGER NOT NULL,
	state       TEXT NOT NULL DEFAULT 'pending',
	last_error  TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS authored (
	path         TEXT PRIMARY KEY,
	content_type TEXT NOT NULL,
	body         BLOB,
	blob_sha     TEXT NOT NULL DEFAULT '',
	size         INTEGER NOT NULL,
	updated_ms   INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS participations (
	id          TEXT PRIMARY KEY,
	path        TEXT NOT NULL,
	element_id  TEXT NOT NULL DEFAULT '',
	selector    TEXT NOT NULL DEFAULT '',
	method      TEXT NOT NULL,
	sub         TEXT NOT NULL DEFAULT '',
	display     TEXT NOT NULL DEFAULT '',
	session     TEXT NOT NULL DEFAULT '',
	version     TEXT NOT NULL DEFAULT '',
	created_ms  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS participations_path ON participations(path, created_ms);
CREATE TABLE IF NOT EXISTS dirs (
	path       TEXT PRIMARY KEY,
	created_ms INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS published_bundles (
 id TEXT PRIMARY KEY,
 digest TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS published_documents (
 revision TEXT NOT NULL,
 path TEXT NOT NULL,
 content_type TEXT NOT NULL,
 body BLOB,
 blob_sha TEXT NOT NULL DEFAULT '',
 size INTEGER NOT NULL,
 version INTEGER NOT NULL,
 etag TEXT NOT NULL,
 created_ms INTEGER NOT NULL,
 modified_ms INTEGER NOT NULL,
 PRIMARY KEY (revision,path)
);
CREATE TABLE IF NOT EXISTS published_seeds (path TEXT PRIMARY KEY);
INSERT OR IGNORE INTO meta(key, value) VALUES ('generation', '0');
`

// Open opens (creating if needed) the site database in dir.
func Open(dir string) (*Site, error) {
	if err := os.MkdirAll(filepath.Join(dir, "blobs"), 0o750); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.Join(dir, "site.db") +
		"?_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=busy_timeout(10000)&_pragma=foreign_keys(ON)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: init schema: %w", err)
	}
	s := &Site{dir: dir, db: db}
	if err := s.sweepBlobs(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Site) Close() error { return s.db.Close() }

// Dir returns the site directory.
func (s *Site) Dir() string { return s.dir }

// DB exposes the underlying database for packages that own their own tables
// (identity, transients, outbox). They must not touch documents or events.
func (s *Site) DB() *sql.DB { return s.db }

const docCols = `path, content_type, body, blob_sha, size, version, etag, created_ms, modified_ms`

func scanDoc(row interface{ Scan(...any) error }) (*Document, error) {
	d := &Document{}
	var body []byte
	if err := row.Scan(&d.Path, &d.ContentType, &body, &d.BlobSHA, &d.Size, &d.Version, &d.ETag, &d.CreatedMS, &d.ModifiedMS); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	d.Body = body
	return d, nil
}

// Get returns the document at path.
func (s *Site) Get(ctx context.Context, path string) (*Document, error) {
	if version, _ := ctx.Value(publishedVersionKey{}).(string); version != "" && !LivePath(path) {
		return scanDoc(s.db.QueryRowContext(ctx, `SELECT `+docCols+` FROM published_documents WHERE revision = ? AND path = ?`, version, path))
	}
	return scanDoc(s.db.QueryRowContext(ctx, `SELECT `+docCols+` FROM documents WHERE path = ?`, path))
}

// List returns documents whose path starts with prefix, ordered by path.
// Bodies are included only when withBody is set.
func (s *Site) List(ctx context.Context, prefix string, withBody bool) ([]*Document, error) {
	cols := docCols
	if !withBody {
		cols = `path, content_type, NULL, blob_sha, size, version, etag, created_ms, modified_ms`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols+` FROM documents WHERE substr(path, 1, ?) = ? ORDER BY path`, len(prefix), prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Document
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Generation returns a counter bumped by every committed write transaction.
// Caches of host-wide derived data key on it.
func (s *Site) Generation(ctx context.Context) (int64, error) {
	var v string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='generation'`).Scan(&v); err != nil {
		return 0, err
	}
	var g int64
	fmt.Sscanf(v, "%d", &g)
	return g, nil
}

// OpenBlob opens a blob body for reading.
func (s *Site) OpenBlob(sha string) (*os.File, error) {
	if len(sha) != 64 || strings.ContainsAny(sha, "./\\") {
		return nil, fmt.Errorf("store: bad blob id")
	}
	return os.Open(filepath.Join(s.dir, "blobs", sha))
}

// WriteBlob durably stores r in the blob directory and returns its sha256.
// It must be called before the transaction that references the blob.
func (s *Site) WriteBlob(r io.Reader) (sha string, size int64, err error) {
	tmp, err := os.CreateTemp(filepath.Join(s.dir, "blobs"), ".upload-*")
	if err != nil {
		return "", 0, err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(tmp, h), r)
	if err != nil {
		return "", 0, err
	}
	if err = tmp.Sync(); err != nil {
		return "", 0, err
	}
	if err = tmp.Close(); err != nil {
		return "", 0, err
	}
	sha = hex.EncodeToString(h.Sum(nil))
	final := filepath.Join(s.dir, "blobs", sha)
	if err = os.Rename(tmp.Name(), final); err != nil {
		return "", 0, err
	}
	if d, e := os.Open(filepath.Join(s.dir, "blobs")); e == nil {
		d.Sync()
		d.Close()
	}
	return sha, size, nil
}

// sweepBlobs removes blob files that no committed document references
// (left behind by an interrupted upload or a replaced/deleted blob).
func (s *Site) sweepBlobs() error {
	dir := filepath.Join(s.dir, "blobs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	refs := map[string]bool{}
	rows, err := s.db.Query(`SELECT blob_sha FROM documents WHERE blob_sha != '' UNION SELECT blob_sha FROM authored WHERE blob_sha != '' UNION SELECT blob_sha FROM published_documents WHERE blob_sha != ''`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var sha string
		rows.Scan(&sha)
		refs[sha] = true
	}
	rows.Close()
	for _, e := range entries {
		if !refs[e.Name()] {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}

// Tx is a write transaction. Obtain one with Site.Update.
type Tx struct {
	tx      *sql.Tx
	now     int64
	events  []Event
	touched map[string]bool
}

// Now is the transaction's commit timestamp in milliseconds.
func (t *Tx) Now() int64 { return t.now }

// Get reads a document inside the transaction (sees the transaction's writes).
func (t *Tx) Get(path string) (*Document, error) {
	return scanDoc(t.tx.QueryRow(`SELECT `+docCols+` FROM documents WHERE path = ?`, path))
}

// List reads documents under prefix inside the transaction.
func (t *Tx) List(prefix string, withBody bool) ([]*Document, error) {
	cols := docCols
	if !withBody {
		cols = `path, content_type, NULL, blob_sha, size, version, etag, created_ms, modified_ms`
	}
	rows, err := t.tx.Query(`SELECT `+cols+` FROM documents WHERE substr(path, 1, ?) = ? ORDER BY path`, len(prefix), prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Document
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Put stores a document, assigning version, ETag and timestamps.
// For markup documents Body is the serialized markup; for blobs set BlobSHA
// and Size (the blob must already be durable via WriteBlob).
func (t *Tx) Put(d *Document) (*Document, error) {
	prev, err := t.Get(d.Path)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	out := *d
	out.ModifiedMS = t.now
	out.Version = 1
	out.CreatedMS = t.now
	if prev != nil {
		out.Version = prev.Version + 1
		out.CreatedMS = prev.CreatedMS
	}
	if out.BlobSHA != "" {
		out.Body = nil
		out.ETag = `"` + out.BlobSHA + `"`
	} else {
		out.Size = int64(len(out.Body))
		sum := sha256.Sum256(out.Body)
		out.ETag = `"` + hex.EncodeToString(sum[:]) + `"`
	}
	_, err = t.tx.Exec(`INSERT INTO documents(`+docCols+`) VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(path) DO UPDATE SET content_type=excluded.content_type, body=excluded.body,
		blob_sha=excluded.blob_sha, size=excluded.size, version=excluded.version, etag=excluded.etag,
		modified_ms=excluded.modified_ms`,
		out.Path, out.ContentType, out.Body, out.BlobSHA, out.Size, out.Version, out.ETag, out.CreatedMS, out.ModifiedMS)
	if err != nil {
		return nil, err
	}
	t.touched[out.Path] = true
	return &out, nil
}

// Delete removes a document. Deleting a missing document returns ErrNotFound.
func (t *Tx) Delete(path string) error {
	res, err := t.tx.Exec(`DELETE FROM documents WHERE path = ?`, path)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	t.touched[path] = true
	return nil
}

// AddEvent records an event; it is committed with the transaction and
// returned (with its sequence number) from Update.
func (t *Tx) AddEvent(e Event) error {
	e.TimeMS = t.now
	res, err := t.tx.Exec(`INSERT INTO events(ts_ms, path, name, data, origin_session, origin_conn) VALUES (?,?,?,?,?,?)`,
		e.TimeMS, e.Path, e.Name, e.Data, e.OriginSession, e.OriginConn)
	if err != nil {
		return err
	}
	e.Seq, _ = res.LastInsertId()
	t.events = append(t.events, e)
	return nil
}

// Exec runs an arbitrary statement inside the transaction (for packages that
// own auxiliary tables such as outbox and transients).
func (t *Tx) Exec(query string, args ...any) (sql.Result, error) { return t.tx.Exec(query, args...) }

// QueryRow runs a single-row query inside the transaction.
func (t *Tx) QueryRow(query string, args ...any) *sql.Row { return t.tx.QueryRow(query, args...) }

// Update runs fn in a write transaction and commits it atomically. It returns
// the events committed by the transaction. Callers serialize site writes with
// their own mutex; SQLite's IMMEDIATE transaction is the backstop.
func (s *Site) Update(ctx context.Context, fn func(*Tx) error) ([]Event, error) {
	sqltx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	t := &Tx{tx: sqltx, now: time.Now().UnixMilli(), touched: map[string]bool{}}
	if err := fn(t); err != nil {
		sqltx.Rollback()
		return nil, err
	}
	if len(t.touched) > 0 {
		if _, err := sqltx.Exec(`UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key='generation'`); err != nil {
			sqltx.Rollback()
			return nil, err
		}
	}
	if err := sqltx.Commit(); err != nil {
		return nil, err
	}
	return t.events, nil
}

// EventsAfter returns retained events for path with seq > after, in order.
func (s *Site) EventsAfter(ctx context.Context, path string, after int64) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT seq, ts_ms, path, name, data, origin_session, origin_conn
		FROM events WHERE path = ? AND seq > ? ORDER BY seq`, path, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Seq, &e.TimeMS, &e.Path, &e.Name, &e.Data, &e.OriginSession, &e.OriginConn); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EventBounds returns the lowest retained and highest issued sequence numbers.
func (s *Site) EventBounds(ctx context.Context) (minSeq, maxSeq int64, err error) {
	var mn, mx sql.NullInt64
	if err = s.db.QueryRowContext(ctx, `SELECT MIN(seq), MAX(seq) FROM events`).Scan(&mn, &mx); err != nil {
		return 0, 0, err
	}
	// sqlite_sequence remembers the highest seq ever issued even after pruning.
	var issued sql.NullInt64
	s.db.QueryRowContext(ctx, `SELECT seq FROM sqlite_sequence WHERE name='events'`).Scan(&issued)
	return mn.Int64, max(mx.Int64, issued.Int64), nil
}

// PruneEvents deletes events older than the retention window.
func (s *Site) PruneEvents(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE ts_ms < ?`, olderThan.UnixMilli())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// PutAuthored records the authoring-plane baseline for a path (pagelike
// extension used by fork/remix: a fork copies authored state only).
func (t *Tx) PutAuthored(d *Document) error {
	_, err := t.tx.Exec(`INSERT INTO authored(path, content_type, body, blob_sha, size, updated_ms) VALUES (?,?,?,?,?,?)
		ON CONFLICT(path) DO UPDATE SET content_type=excluded.content_type, body=excluded.body, blob_sha=excluded.blob_sha,
		size=excluded.size, updated_ms=excluded.updated_ms`, d.Path, d.ContentType, d.Body, d.BlobSHA, d.Size, t.now)
	return err
}

// DeleteAuthored removes a path's authored baseline (prefix match when the
// path ends in '/').
func (t *Tx) DeleteAuthored(path string) error {
	var err error
	if strings.HasSuffix(path, "/") {
		_, err = t.tx.Exec(`DELETE FROM authored WHERE substr(path,1,?) = ?`, len(path), path)
	} else {
		_, err = t.tx.Exec(`DELETE FROM authored WHERE path = ?`, path)
	}
	return err
}

// Authored lists the authored baseline (bodies included).
func (s *Site) Authored(ctx context.Context) ([]*Document, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path, content_type, body, blob_sha, size, updated_ms FROM authored ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Document
	for rows.Next() {
		d := &Document{}
		if err := rows.Scan(&d.Path, &d.ContentType, &d.Body, &d.BlobSHA, &d.Size, &d.ModifiedMS); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AuthoredDigest hashes the authored baseline: the "experience version".
func (s *Site) AuthoredDigest(ctx context.Context) (string, error) {
	docs, err := s.Authored(ctx)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	for _, d := range docs {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00", d.Path, d.ContentType, d.BlobSHA)
		h.Write(d.Body)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// Participation is a recorded public-plane contribution (pagelike extension).
type Participation struct {
	ID, Path, ElementID, Selector, Method, Sub, Display, Session, Version string
	CreatedMS                                                             int64
}

// AddParticipation records a contribution inside the write transaction.
func (t *Tx) AddParticipation(p Participation) error {
	_, err := t.tx.Exec(`INSERT INTO participations(id, path, element_id, selector, method, sub, display, session, version, created_ms)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, p.ID, p.Path, p.ElementID, p.Selector, p.Method, p.Sub, p.Display, p.Session, p.Version, t.now)
	return err
}

// Participations lists contributions, newest first (path "" = all).
func (s *Site) Participations(ctx context.Context, path string, limit int) ([]Participation, error) {
	q := `SELECT id, path, element_id, selector, method, sub, display, session, version, created_ms FROM participations`
	var args []any
	if path != "" {
		q += ` WHERE path = ?`
		args = append(args, path)
	}
	q += ` ORDER BY created_ms DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Participation
	for rows.Next() {
		var p Participation
		if err := rows.Scan(&p.ID, &p.Path, &p.ElementID, &p.Selector, &p.Method, &p.Sub, &p.Display, &p.Session, &p.Version, &p.CreatedMS); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Participation returns one contribution by id.
func (s *Site) Participation(ctx context.Context, id string) (*Participation, error) {
	var p Participation
	err := s.db.QueryRowContext(ctx, `SELECT id, path, element_id, selector, method, sub, display, session, version, created_ms FROM participations WHERE id = ?`, id).
		Scan(&p.ID, &p.Path, &p.ElementID, &p.Selector, &p.Method, &p.Sub, &p.Display, &p.Session, &p.Version, &p.CreatedMS)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// MkDir records an explicit (possibly empty) directory. Directories are
// otherwise implicit in document paths.
func (t *Tx) MkDir(path string) error {
	_, err := t.tx.Exec(`INSERT OR IGNORE INTO dirs(path, created_ms) VALUES (?, ?)`, path, t.now)
	t.touched[path] = true
	return err
}

// RemoveDirs deletes explicit directory markers under prefix (inclusive).
func (t *Tx) RemoveDirs(prefix string) error {
	_, err := t.tx.Exec(`DELETE FROM dirs WHERE substr(path, 1, ?) = ?`, len(prefix), prefix)
	t.touched[prefix] = true
	return err
}

// DirExists reports whether dir (ending in '/') exists explicitly or
// implicitly (some document lives under it).
func (s *Site) DirExists(ctx context.Context, dir string) (bool, error) {
	if dir == "/" {
		return true, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM dirs WHERE substr(path,1,?) = ?) + (SELECT COUNT(*) FROM documents WHERE substr(path,1,?) = ?)`,
		len(dir), dir, len(dir), dir).Scan(&n)
	return n > 0, err
}

// Dirs lists explicit directory markers under prefix.
func (s *Site) Dirs(ctx context.Context, prefix string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM dirs WHERE substr(path,1,?) = ? ORDER BY path`, len(prefix), prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		rows.Scan(&p)
		out = append(out, p)
	}
	return out, rows.Err()
}

// Checkpoint flushes the WAL into the main database file (used before backup).
func (s *Site) Checkpoint(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

// Backup writes a consistent copy of the database to dst using VACUUM INTO.
func (s *Site) Backup(ctx context.Context, dst string) error {
	_, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dst)
	return err
}
