package site

import (
	"context"
	"errors"
	"testing"
)

func TestClosedRegistryCannotReopenSites(t *testing.T) {
	r, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Create(context.Background(), "example", Settings{}); err != nil {
		t.Fatal(err)
	}
	r.Close()
	// A late background scan must not reopen SQLite after shutdown/cleanup.
	if _, err := r.Get(context.Background(), "example"); !errors.Is(err, ErrNoSite) {
		t.Fatalf("closed registry reopened a site: %v", err)
	}
}
