package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// A site database made before outbox.okey existed opens, gains the column
// and keeps its rows; opening it again changes nothing.
func TestOpenAddsOutboxOrderingKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "blobs"), 0o750); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "site.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE outbox (
		id INTEGER PRIMARY KEY AUTOINCREMENT, created_ms INTEGER NOT NULL, kind TEXT NOT NULL, request TEXT NOT NULL,
		attempts INTEGER NOT NULL DEFAULT 0, max_attempts INTEGER NOT NULL DEFAULT 1, next_ms INTEGER NOT NULL,
		state TEXT NOT NULL DEFAULT 'pending', last_error TEXT NOT NULL DEFAULT '');
		INSERT INTO outbox(created_ms, kind, request, next_ms) VALUES (1, 'http', '{}', 1);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for i := 0; i < 2; i++ {
		s, err := Open(dir)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		var key string
		var n int
		if err := s.DB().QueryRow(`SELECT COUNT(*), MAX(okey) FROM outbox`).Scan(&n, &key); err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if n != 1 || key != "" {
			t.Fatalf("open %d: %d rows, key %q; want the old row with an empty key", i, n, key)
		}
		// The statement the previous release runs still works, so a
		// rollback is safe.
		if _, err := s.DB().Exec(`INSERT INTO outbox(created_ms, kind, request, attempts, max_attempts, next_ms, state) VALUES (1,'http','{}',0,1,1,'done')`); err != nil {
			t.Fatalf("old insert: %v", err)
		}
		s.DB().Exec(`DELETE FROM outbox WHERE state = 'done'`)
		s.Close()
	}
}
