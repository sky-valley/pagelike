package site

import (
	"context"
	"testing"

	"github.com/sky-valley/pagelike/internal/store"
)

func TestInstallPublishedPreservesParticipationAndExactVersions(t *testing.T) {
	ctx := context.Background()
	reg, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	s, err := reg.Create(ctx, "example", Settings{})
	if err != nil {
		t.Fatal(err)
	}
	first := map[string][]byte{"index.html": []byte("<h1>First</h1>"), "rules.html": []byte("<p>old rules</p>"), "data/votes.html": []byte("<ul></ul>")}
	if err := s.InstallPublished(ctx, "v1", 1, first); err != nil {
		t.Fatal(err)
	}
	_, err = s.Store.Update(ctx, func(tx *store.Tx) error {
		_, e := tx.Put(&store.Document{Path: "/data/votes.html", ContentType: "text/html", Body: []byte("<ul><li>real vote</li></ul>")})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	second := map[string][]byte{"index.html": []byte("<h1>Second</h1>"), "rules.html": []byte("<p>new rules</p>"), "data/votes.html": []byte("<ul><li>different seed</li></ul>")}
	if err := s.InstallPublished(ctx, "v2", 2, second); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"v1", "v2"} {
		d, err := s.Store.Get(store.WithPublishedVersion(ctx, version), "/data/votes.html")
		if err != nil || string(d.Body) != "<ul><li>real vote</li></ul>" {
			t.Fatalf("live data under %s: %v %v", version, d, err)
		}
	}
	d, err := s.Store.Get(store.WithPublishedVersion(ctx, "v1"), "/index.html")
	if err != nil || string(d.Body) != "<h1>First</h1>" {
		t.Fatalf("exact version: %v %v", d, err)
	}
	if err := s.InstallPublished(ctx, "v1", 1, first); err != nil {
		t.Fatal(err)
	}
	d, err = s.Store.Get(ctx, "/index.html")
	if err != nil || string(d.Body) != "<h1>Second</h1>" {
		t.Fatalf("stale install changed current: %v %v", d, err)
	}
	second["index.html"] = []byte("tampered")
	if err := s.InstallPublished(ctx, "v2", 2, second); err == nil {
		t.Fatal("accepted changed immutable version")
	}
}

func TestInstallPublishedNeverReseedsDeletedData(t *testing.T) {
	ctx := context.Background()
	reg, err := NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	s, err := reg.Create(ctx, "example", Settings{})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"index.html": []byte("<h1>Hello</h1>"), "data/a.html": []byte("<p>seed</p>")}
	if err := s.InstallPublished(ctx, "one", 1, files); err != nil {
		t.Fatal(err)
	}
	_, err = s.Store.Update(ctx, func(tx *store.Tx) error { return tx.Delete("/data/a.html") })
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InstallPublished(ctx, "two", 2, files); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.Get(ctx, "/data/a.html"); err != store.ErrNotFound {
		t.Fatalf("deleted seed resurrected: %v", err)
	}
}
