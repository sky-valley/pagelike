package site

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"path"
	"strings"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/store"
)

// InstallPublished atomically installs an authored bundle and seeds each live
// path once. Replays cannot roll back the current bundle or resurrect a seed.
// Published revisions are immutable; current rules always govern shared state.
func (s *Site) InstallPublished(ctx context.Context, version string, generation int64, files map[string][]byte) error {
	if !ValidName(version) || generation < 1 || len(files) == 0 || len(files) > 256 || len(files["index.html"]) == 0 {
		return errors.New("invalid published bundle")
	}
	var total int
	for p, b := range files {
		total += len(b)
		if total > 20<<20 || p != path.Clean(p) || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00\r\n") || strings.HasPrefix(p, "uploads/") || strings.HasPrefix(p, "-/") || strings.HasPrefix(p, "v/") {
			return errors.New("invalid published file")
		}
		for _, part := range strings.Split(p, "/") {
			if part == "" || strings.HasPrefix(part, ".") {
				return errors.New("invalid published path")
			}
		}
	}
	encoded, err := json.Marshal(files)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(encoded)
	digest := hex.EncodeToString(hash[:])
	s.WriteMu.Lock()
	defer s.WriteMu.Unlock()
	// Blob writes happen under the site write lock and before their referencing
	// transaction. Store opening sweeps interrupted, unreferenced blobs.
	docs := make([]*store.Document, 0, len(files))
	for p, b := range files {
		ct := mime.TypeByExtension(path.Ext(p))
		if ct == "" {
			ct = "application/octet-stream"
		}
		d := &store.Document{Path: "/" + p, ContentType: ct}
		if IsMarkup(ct) {
			root, e := ParseMarkup(ct, b)
			if e != nil {
				return e
			}
			d.Body = dom.Render(root)
		} else {
			d.BlobSHA, d.Size, err = s.Store.WriteBlob(bytes.NewReader(b))
			if err != nil {
				return err
			}
		}
		docs = append(docs, d)
	}
	_, err = s.Store.Update(ctx, func(tx *store.Tx) error {
		var previous string
		e := tx.QueryRow(`SELECT digest FROM published_bundles WHERE id = ?`, version).Scan(&previous)
		switch {
		case e == nil && previous != digest:
			return errors.New("published version is immutable")
		case e != nil && !errors.Is(e, sql.ErrNoRows):
			return e
		}
		if errors.Is(e, sql.ErrNoRows) {
			if _, e = tx.Exec(`INSERT INTO published_bundles(id,digest) VALUES (?,?)`, version, digest); e != nil {
				return e
			}
			for _, d := range docs {
				if e := tx.PutPublished(version, d); e != nil {
					return e
				}
			}
		}
		var installed int64
		e = tx.QueryRow(`SELECT CAST(value AS INTEGER) FROM meta WHERE key='published_generation'`).Scan(&installed)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if generation <= installed {
			return nil
		}
		existing, e := tx.List("/", false)
		if e != nil {
			return e
		}
		for _, d := range existing {
			if !store.LivePath(d.Path) {
				if e := tx.Delete(d.Path); e != nil {
					return e
				}
			}
		}
		if e := tx.DeleteAuthored("/"); e != nil {
			return e
		}
		for _, d := range docs {
			seed := store.LivePath(d.Path)
			if seed {
				r, e := tx.Exec(`INSERT OR IGNORE INTO published_seeds(path) VALUES (?)`, d.Path)
				if e != nil {
					return e
				}
				n, e := r.RowsAffected()
				if e != nil {
					return e
				}
				seed = n == 1
			}
			if !store.LivePath(d.Path) || seed {
				if _, e := tx.Put(d); e != nil {
					return e
				}
			}
			if e := tx.PutAuthored(d); e != nil {
				return e
			}
		}
		for key, value := range map[string]string{"published_generation": fmt.Sprint(generation), "published_current": version} {
			if _, e := tx.Exec(`INSERT INTO meta(key,value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); e != nil {
				return e
			}
		}
		return nil
	})
	return err
}
