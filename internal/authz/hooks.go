package authz

import "sync"

// Hook adjusts a site's policy while it is assembled for a new write
// generation, before any request can see it, so it may modify p freely.
// host is the *site.Snapshot being built (typed any so authz does not depend
// on the site package); a hook may read its documents and items but must not
// touch its Policy field.
//
// This is the extension point for the schema package (architecture.md,
// authz.Group.Includes):
//   - items whose schema type descends from https://pagelove.org/Group are
//     groups: append GroupFromItem(...) with Includes bound to the most
//     derived schema's includes() method, if any (R-PERM-60);
//   - items of schema subtypes of AuthorizationRule are rules: append
//     RulesFromItem(...) (R-PERM-7);
//   - an @read resolver on AuthorizationRule.actor rewrites Rule.Actors
//     (R-PERM-8);
//   - SelectorOptions carries the inheritance map :isa() consults in rule
//     selectors.
type Hook func(host any, p *Policy)

var hooks struct {
	mu    sync.RWMutex
	names []string
	fns   map[string]Hook
}

// RegisterHook installs (or replaces) the named hook for every policy built
// afterwards. Hooks run in registration order.
func RegisterHook(name string, h Hook) {
	hooks.mu.Lock()
	defer hooks.mu.Unlock()
	if hooks.fns == nil {
		hooks.fns = map[string]Hook{}
	}
	if _, ok := hooks.fns[name]; !ok {
		hooks.names = append(hooks.names, name)
	}
	hooks.fns[name] = h
}

// RunHooks applies the registered hooks to a freshly assembled policy. The
// site index calls it once per generation, after extracting every rule and
// group.
func (p *Policy) RunHooks(host any) {
	hooks.mu.RLock()
	fns := make([]Hook, 0, len(hooks.names))
	for _, n := range hooks.names {
		fns = append(fns, hooks.fns[n])
	}
	hooks.mu.RUnlock()
	for _, f := range fns {
		f(host, p)
	}
	if len(fns) > 0 {
		for i := range p.Rules {
			p.Rules[i].compile() // rules may have been added or edited
		}
	}
}
