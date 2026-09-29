package reactions

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/sky-valley/pagelike/internal/control"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

// TestRaceOnWatchedItemIs412 checks R-REACT-73: a write whose document
// changed after the request arrived, and which touches an item under a
// transition constraint, is refused with 412 and changes nothing.
func TestRaceOnWatchedItemIs412(t *testing.T) {
	dir := t.TempDir()
	reg, err := site.NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctl, err := control.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reg.Close(); ctl.Close() })
	ctx := context.Background()
	s, err := reg.Create(ctx, "t", site.Settings{DefaultGet: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(server.Config{Domain: "localhost"}, reg, ctl, slog.New(slog.NewTextHandler(io.Discard, nil)))
	x, ok := srv.Public.Intercept.(*Reactions)
	if !ok {
		t.Fatal("reactions are not installed")
	}
	put := func(path, body string) {
		t.Helper()
		if _, err := srv.Engine.Write(ctx, s, &engine.Op{Plane: engine.Authoring, Method: "PUT", Path: path, Body: []byte(body)}); err != nil {
			t.Fatal(err)
		}
	}
	put("/rules.html", `<!DOCTYPE html><html><body><div itemscope itemtype="https://pagelove.org/AuthorizationRule"><span itemprop="actor">*</span><span itemprop="resource">/*</span><span itemprop="method">PUT</span><span itemprop="action">Allow</span></div>
<div itemscope itemtype="https://pagelove.org/TransitionConstraint"><meta itemprop="selector" content="[itemtype='https://t.test/Order']"><meta itemprop="property" content="status"><meta itemprop="from" content="pending"><meta itemprop="to" content="processing"></div></body></html>`)
	put("/o.html", `<!DOCTYPE html><html><body><div id="o1" itemscope itemtype="https://t.test/Order"><meta itemprop="status" content="pending"></div><p id="free">x</p></body></html>`)
	doc, err := s.Store.Get(ctx, "/o.html")
	if err != nil {
		t.Fatal(err)
	}
	write := func(sel, body string) error {
		st := &reqState{x: x, site: s, method: "PUT", target: "/o.html", phase: phaseCore,
			arrival: map[string]int64{"/o.html": doc.Version - 1}} // it arrived before the last commit
		op := &engine.Op{Plane: engine.Public, Method: "PUT", Path: "/o.html", Range: engine.ParseRange("selector=" + sel),
			Body: []byte(body), Principal: identity.Anonymous("s")}
		_, err := srv.Engine.Write(withState(ctx, st), s, op)
		return err
	}
	var e *errdoc.Error
	if err := write("#o1", `<div id="o1" itemscope itemtype="https://t.test/Order"><meta itemprop="status" content="processing"></div>`); !errors.As(err, &e) || e.Status != 412 {
		t.Fatalf("watched item: %v, want 412", err)
	}
	if err := write("#free", `<p id="free">y</p>`); err != nil {
		t.Fatalf("unwatched element: %v, want success", err)
	}
}
