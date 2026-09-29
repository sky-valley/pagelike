package engine_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/site"
)

// Validate and AfterWrite hooks run in phase order whatever order they were
// added in: schema checks before TransitionConstraints (R-MOD-15, R-REACT-71),
// hooks appended without a phase last.
func TestHookPhases(t *testing.T) {
	f := newFixture(t)
	var ran []string
	hook := func(name string) func(context.Context, *engine.WriteCtx) error {
		return func(context.Context, *engine.WriteCtx) error { ran = append(ran, name); return nil }
	}
	after := func(name string) func(context.Context, *engine.WriteCtx, *engine.Result) error {
		return func(context.Context, *engine.WriteCtx, *engine.Result) error { ran = append(ran, name); return nil }
	}
	h := &f.e.Hooks
	h.Validate = append(h.Validate, hook("legacy"))
	h.AddValidate(engine.PhaseTransition, hook("transition"))
	h.AddValidate(engine.PhaseSchema, hook("schema"))
	h.AddValidate(engine.PhaseTransition, hook("transition2"))
	h.AddValidate(engine.PhaseSchema, hook("schema2"))
	h.AfterWrite = append(h.AfterWrite, after("after-legacy"))
	h.AddAfterWrite(engine.PhaseSchema, after("cascade"))

	want := []engine.Phase{engine.PhaseSchema, engine.PhaseSchema, engine.PhaseTransition, engine.PhaseTransition, engine.PhaseDefault}
	if got := h.ValidatePhases(); !reflect.DeepEqual(got, want) {
		t.Errorf("validate phases %v, want %v", got, want)
	}
	f.put("/a.html", "text/html", "<!DOCTYPE html><html><body><p>x</p></body></html>")
	if got := strings.Join(ran, ","); got != "schema,schema2,transition,transition2,legacy,cascade,after-legacy" {
		t.Errorf("hooks ran %s", got)
	}

	// The first refusal wins: a schema refusal pre-empts the transition check.
	ran = nil
	f.e.Hooks = engine.Hooks{}
	f.e.Hooks.AddValidate(engine.PhaseTransition, func(context.Context, *engine.WriteCtx) error {
		ran = append(ran, "transition")
		return errdoc.New(422, "Transition", "transition")
	})
	f.e.Hooks.AddValidate(engine.PhaseSchema, func(context.Context, *engine.WriteCtx) error {
		ran = append(ran, "schema")
		return errdoc.New(422, "Unique", "unique")
	})
	_, err := f.e.Write(f.ctx, f.s, &engine.Op{Plane: engine.Authoring, Method: "PUT", Path: "/b.html", ContentType: "text/html", Body: []byte("<p>y</p>")})
	var ed *errdoc.Error
	if !errors.As(err, &ed) || ed.Kind != "Unique" || strings.Join(ran, ",") != "schema" {
		t.Errorf("err %v, ran %v", err, ran)
	}
}

// Range selectors resolve :isa() against the snapshot's schema hierarchy
// once bound to it (SelectorIsA, R-COMP-177); unbound, :isa() matches
// nothing.
func TestRangeIsA(t *testing.T) {
	f := newFixture(t)
	f.put("/a.html", "text/html", `<!DOCTYPE html><html><body><div id="c" itemscope itemtype="urn:t:Child"></div></body></html>`)
	old := engine.SelectorIsA
	t.Cleanup(func() { engine.SelectorIsA = old })
	engine.SelectorIsA = func(*site.Snapshot) func(string, string) bool {
		return func(t, target string) bool { return t == target || t == "urn:t:Child" && target == "urn:t:Base" }
	}
	snap, err := f.s.Index(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	rng := engine.ParseRange("selector=div:isa('urn:t:Base')")
	sel, err := rng.BindSnapshot(snap).CheckSelector()
	if err != nil {
		t.Fatal(err)
	}
	if m := sel.MatchFirst(snap.Docs["/a.html"].Root); m == nil {
		t.Error("bound :isa() does not match a descendant type")
	}
	sel, err = rng.CheckSelector()
	if err != nil {
		t.Fatal(err)
	}
	if m := sel.MatchFirst(snap.Docs["/a.html"].Root); m != nil {
		t.Error("unbound :isa() matched")
	}
	if res := f.get("/a.html", "selector=div:isa('urn:t:Base')"); res.Status != 206 {
		t.Errorf("engine read with :isa(): status %d", res.Status)
	}
}
