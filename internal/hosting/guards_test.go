package hosting

import (
	"context"
	"testing"

	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

func hostedEngine(t *testing.T) (*Server, *site.Site) {
	t.Helper()
	reg, err := site.NewRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.Close)
	s, err := reg.Create(context.Background(), "example", site.Settings{Hosted: true})
	if err != nil {
		t.Fatal(err)
	}
	core := server.New(server.Config{Domain: "content.example"}, reg, nil, nil)
	h, err := New(Config{Domain: "content.example", AdminToken: "admin", EdgeToken: "edge", IdentityToken: "identity", IdentityURL: "http://127.0.0.1:1", FrameOrigins: []string{"https://player.example"}}, core)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InstallPublished(context.Background(), "v1", 1, map[string][]byte{"index.html": []byte("<p>Original</p>")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.DB().Exec(contributionSchema); err != nil {
		t.Fatal(err)
	}
	return h, s
}

func TestHostingSecondaryWritesCannotChangeAuthoredContentOrExecuteData(t *testing.T) {
	for _, mutation := range []string{"authored-put", "authored-delete", "executable-data"} {
		t.Run(mutation, func(t *testing.T) {
			h, s := hostedEngine(t)
			h.core.Engine.Hooks.AddAfterWrite(engine.PhaseDefault, func(_ context.Context, w *engine.WriteCtx, _ *engine.Result) error {
				if mutation == "authored-delete" {
					return w.SideEffectDelete("/index.html")
				}
				path, body := "/index.html", "<p>Changed</p>"
				if mutation == "executable-data" {
					path, body = "/data/derived.html", "<script>bad()</script>"
				}
				_, err := w.SideEffectPut(path, "text/html", []byte(body))
				return err
			})
			_, err := h.core.Engine.Write(context.Background(), s, &engine.Op{Plane: engine.Authoring, Method: "PUT", Path: "/data/new.html", ContentType: "text/html", Body: []byte("<p>Contribution</p>"), Principal: &identity.Principal{Authenticated: true, Sub: "one"}})
			if err == nil {
				t.Fatal("secondary write escaped hosting boundary")
			}
			if _, err := s.Store.Get(context.Background(), "/data/new.html"); err != store.ErrNotFound {
				t.Fatalf("failed transaction committed participation: %v", err)
			}
			d, err := s.Store.Get(context.Background(), "/index.html")
			if err != nil || string(d.Body) != "<p>Original</p>" {
				t.Fatal("authored document changed")
			}
		})
	}
}

func TestOldUploadRemovalCannotRemoveReplacement(t *testing.T) {
	h, s := hostedEngine(t)
	for _, who := range []string{"one", "two"} {
		_, err := h.core.Engine.Write(context.Background(), s, &engine.Op{Plane: engine.Authoring, Method: "PUT", Path: "/data/value.txt", ContentType: "text/plain", Body: []byte(who), Principal: &identity.Principal{Authenticated: true, Sub: who}})
		if err != nil {
			t.Fatal(err)
		}
	}
	var id string
	if err := s.Store.DB().QueryRow(`SELECT id FROM hosted_contributions WHERE owner='one'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := h.removeContribution(context.Background(), s, id, "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.Get(context.Background(), "/data/value.txt"); err != nil {
		t.Fatal("old moderation removed newer resource")
	}
}
