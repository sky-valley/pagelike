package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

type publishedVersionKey struct{}

// WithPublishedVersion selects immutable authored files while keeping live
// documents shared. An empty version reads the currently installed authored set.
func WithPublishedVersion(ctx context.Context, version string) context.Context {
	return context.WithValue(ctx, publishedVersionKey{}, version)
}

func PublishedVersion(ctx context.Context) string {
	v, _ := ctx.Value(publishedVersionKey{}).(string)
	return v
}

// PublishedDocuments returns authored documents without archived live seeds.
func (s *Site) PublishedDocuments(ctx context.Context, version string) ([]*Document, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+docCols+` FROM published_documents WHERE revision=? ORDER BY path`, version)
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
		if !LivePath(d.Path) {
			out = append(out, d)
		}
	}
	return out, rows.Err()
}

// LivePath identifies the mutable namespaces of a published application.
func LivePath(path string) bool {
	return strings.HasPrefix(path, "/data/") || strings.HasPrefix(path, "/uploads/")
}

// PutPublished stores one immutable document as part of installing a bundle.
// The caller checks the bundle digest before invoking this within its transaction.
func (t *Tx) PutPublished(version string, d *Document) error {
	sum := sha256.Sum256(d.Body)
	etag, size := `"`+hex.EncodeToString(sum[:])+`"`, int64(len(d.Body))
	if d.BlobSHA != "" {
		etag, size = `"`+d.BlobSHA+`"`, d.Size
	}
	_, err := t.tx.Exec(`INSERT INTO published_documents(revision,`+docCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`, version, d.Path, d.ContentType, d.Body, d.BlobSHA, size, 1, etag, t.now, t.now)
	return err
}
