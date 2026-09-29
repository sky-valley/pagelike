// Package control holds instance-wide state: authoring API keys. Keys are
// high-entropy random tokens stored only as SHA-256 hashes; the plaintext is
// shown once at creation.
package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DB is the control database.
type DB struct{ db *sql.DB }

// Key describes an authoring key (never the secret itself).
type Key struct {
	ID        string
	Prefix    string
	Label     string
	Sites     []string // "*" means every site
	CreatedMS int64
	ExpiresMS int64 // 0 = never
}

// Open opens data/control.db.
func Open(dataDir string) (*DB, error) {
	dsn := "file:" + filepath.Join(dataDir, "control.db") + "?_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=busy_timeout(10000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS keys (
		id TEXT PRIMARY KEY, prefix TEXT NOT NULL, hash TEXT NOT NULL UNIQUE, label TEXT NOT NULL DEFAULT '',
		sites TEXT NOT NULL, created_ms INTEGER NOT NULL, expires_ms INTEGER NOT NULL DEFAULT 0)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db: db}, nil
}

// Close closes the database.
func (c *DB) Close() error { return c.db.Close() }

func hashKey(k string) string {
	s := sha256.Sum256([]byte(k))
	return hex.EncodeToString(s[:])
}

// CreateKey mints a key for the given sites ("*" for all) and returns the
// plaintext once.
func (c *DB) CreateKey(ctx context.Context, label string, sites []string, ttl time.Duration) (string, *Key, error) {
	var b [20]byte
	rand.Read(b[:])
	secret := "pk_" + hex.EncodeToString(b[:])
	var idb [6]byte
	rand.Read(idb[:])
	k := &Key{ID: hex.EncodeToString(idb[:]), Prefix: secret[:11], Label: label, Sites: sites, CreatedMS: time.Now().UnixMilli()}
	if ttl > 0 {
		k.ExpiresMS = time.Now().Add(ttl).UnixMilli()
	}
	_, err := c.db.ExecContext(ctx, `INSERT INTO keys(id, prefix, hash, label, sites, created_ms, expires_ms) VALUES (?,?,?,?,?,?,?)`,
		k.ID, k.Prefix, hashKey(secret), label, strings.Join(sites, ","), k.CreatedMS, k.ExpiresMS)
	if err != nil {
		return "", nil, err
	}
	return secret, k, nil
}

// ErrBadKey reports an unknown, expired or out-of-scope key.
var ErrBadKey = errors.New("invalid authoring key")

// Check validates a presented key for a site.
func (c *DB) Check(ctx context.Context, secret, site string) (*Key, error) {
	if !strings.HasPrefix(secret, "pk_") || len(secret) > 128 {
		return nil, ErrBadKey
	}
	h := hashKey(secret)
	var k Key
	var sites, stored string
	err := c.db.QueryRowContext(ctx, `SELECT id, prefix, hash, label, sites, created_ms, expires_ms FROM keys WHERE hash = ?`, h).
		Scan(&k.ID, &k.Prefix, &stored, &k.Label, &sites, &k.CreatedMS, &k.ExpiresMS)
	if err != nil || subtle.ConstantTimeCompare([]byte(stored), []byte(h)) != 1 {
		return nil, ErrBadKey
	}
	if k.ExpiresMS != 0 && k.ExpiresMS < time.Now().UnixMilli() {
		return nil, ErrBadKey
	}
	k.Sites = strings.Split(sites, ",")
	for _, s := range k.Sites {
		if s == "*" || s == site {
			return &k, nil
		}
	}
	if site == "" {
		return &k, nil
	}
	return nil, ErrBadKey
}

// List returns all keys (metadata only).
func (c *DB) List(ctx context.Context) ([]Key, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT id, prefix, label, sites, created_ms, expires_ms FROM keys ORDER BY created_ms`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Key
	for rows.Next() {
		var k Key
		var sites string
		if err := rows.Scan(&k.ID, &k.Prefix, &k.Label, &sites, &k.CreatedMS, &k.ExpiresMS); err != nil {
			return nil, err
		}
		k.Sites = strings.Split(sites, ",")
		out = append(out, k)
	}
	return out, rows.Err()
}

// Revoke deletes a key by id or prefix.
func (c *DB) Revoke(ctx context.Context, idOrPrefix string) (bool, error) {
	res, err := c.db.ExecContext(ctx, `DELETE FROM keys WHERE id = ? OR prefix = ?`, idOrPrefix, idOrPrefix)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Backup writes a consistent copy of the control database.
func (c *DB) Backup(ctx context.Context, dst string) error {
	_, err := c.db.ExecContext(ctx, `VACUUM INTO ?`, dst)
	return err
}

// PresentedKey extracts a key from Authorization: Bearer or Basic (password).
func PresentedKey(authz string) string {
	authz = strings.TrimSpace(authz)
	if len(authz) > 7 && strings.EqualFold(authz[:7], "bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	return ""
}
