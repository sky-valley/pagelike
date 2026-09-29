package schema

import (
	"github.com/sky-valley/pagelike/internal/authz"
	"github.com/sky-valley/pagelike/internal/engine"
	"github.com/sky-valley/pagelike/internal/query"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/sessel"
	"github.com/sky-valley/pagelike/internal/site"
)

func init() {
	// Host-wide Sessel classes (Type.search(), new Type {}, methods).
	query.SetClassSource(func(snap *site.Snapshot) sessel.ClassRegistry { return For(snap) })
	// Group subtypes, rule subtypes, @read on AuthorizationRule.actor, :isa().
	authz.RegisterHook("schema", policyHook)
	// :isa() in Range selectors (R-COMP-177).
	engine.SelectorIsA = rangeIsA
	server.Extend(Register)
}

// rangeIsA is the :isa() relation of Range selectors over snap's registry
// (R-COMP-177), as composition's selectors see it: an element matches when
// its itemtype is a declared schema that is the target or descends from it.
// With no schema declared nothing matches, not even equal types.
func rangeIsA(snap *site.Snapshot) func(itemtype, target string) bool {
	reg := For(snap)
	if len(reg.Schemas()) == 0 {
		return nil
	}
	return func(t, target string) bool { return reg.Schema(t) != nil && reg.IsA(t, target) }
}

// Register wires the schema system into a server: validation and cascades
// on the engine, and @read resolvers on public-plane representations.
func Register(s *server.Server) {
	Install(s.Engine)
	if s.Public != nil {
		s.Public.Compose = composeWithRead(s.Public.Compose)
	}
}

// Install adds the schema hooks to an engine in engine.PhaseSchema:
// Validate (schemas, shapes, uniqueness, references, restrict) and
// AfterWrite (cascades). The phase puts them before TransitionConstraints
// (engine.PhaseTransition; R-MOD-15 steps 1-9 before 10, R-REACT-71) and
// before other after-write hooks (cascades are part of the write that
// processors observe), whatever the package initialization order.
func Install(e *engine.Engine) {
	e.Hooks.AddValidate(engine.PhaseSchema, Validate)
	e.Hooks.AddAfterWrite(engine.PhaseSchema, AfterWrite)
}
