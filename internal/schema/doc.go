// Package schema implements PageLove's modeling system
// (docs/spec/modeling.md, area MOD): Schema, Property, Method, Enum,
// GroupConstraint and ShapeConstraint declarations, their discovery and
// inheritance, the write-time validation pipeline, references with
// referential actions, resolvers, computed properties, and the error
// documents of refused writes.
//
// # Registry
//
// For(snap) returns the host-wide Registry of a site snapshot, built once
// per write generation and cached with site.Snapshot.Ext (R-MOD-2). Every
// stored HTML document is scanned for declarations (exact itemtype, outside
// <template>); the first declaration of a type URL in path order wins
// (R-MOD-6). Parent chains are resolved (unknown parents end the chain,
// cycles are load errors; R-MOD-10) and each schema gets its effective
// members (R-MOD-11): properties overridden by name (whole declaration),
// unique and group membership united, @write chains leaf → root, @read
// chains root → leaf, schema-level validators ancestor-first, group
// constraints most-derived per group, the honoured @key. The Registry is a
// sessel.ClassRegistry (installed with query.SetClassSource), so
// Type.search(), new Type {…}, instance property reads (computed properties
// and @read pipelines) and methods work in every Sessel evaluation of the
// host. Registry.IsA is the subtype relation :isa() and references use.
//
// # Writes
//
// Validate (engine Hooks.Validate) runs on every mutating operation of
// both planes (R-MOD-13) except a WebDAV PUT, which PageLove stores
// unvalidated (live 2026-09-29), in the order of R-MOD-15, against the
// registry of the snapshot the write started from (R-MOD-4); see its
// documentation. Affected instances and elements follow R-MOD-14/R-MOD-63,
// with PageLove's rule that only outermost items are instances. Defaults and
// @write results are stored: for whole-document PUTs the changes are
// spliced into the request bytes (decision 0003 §6) and written back to
// w.Op.Body; selector writes leave their storage to the engine's splicer.
// Cascades are planned during validation (restrict answers 409 before
// anything is stored) and applied by AfterWrite (engine Hooks.AfterWrite)
// inside the same transaction through engine.WriteCtx.SideEffectPut/Delete,
// each with its mutation event. Install puts both hooks first in their
// lists.
//
// Refusals are *errdoc.Error values whose Document is the body: the
// SchemaViolation page (R-MOD-70) with BindingFailure items (R-MOD-73) for
// whole writes, and PageLove's bare problems items (live 2026-09-29) for
// the rest: ShapeConstraint and CascadeBlocked for shapes (R-MOD-71),
// ConstraintViolation for uniqueness and references and CascadeBlocked for
// restrict (R-MOD-72), Cardinality for selector writes. A thrown
// HTTPResponse in a property @validate or @write chooses the response
// (R-MOD-47).
//
// # Reads
//
// ApplyRead(ctx, site, snap, path, root) runs the @read resolvers of the
// governed instances of a tree the caller owns (R-MOD-50). Register wraps
// httpapi.Public.Compose so public-plane representations (GET/HEAD whole and
// by selector, JSON-LD, edge QUERY) get them; a Composer that calls
// ApplyRead on its final tree itself (with the ctx it was given) disables
// the wrapper's pass for that request. Documents without resolvers are
// untouched, so they are still served as their stored bytes.
//
// # Methods
//
// Registry.FindMethod and Dispatch serve method elements (composition):
// overload selection by supplied attributes, doesNotUnderstand fallback,
// Sessel parameters as named locals, JavaScript parameters positionally
// (R-MOD-59, R-SESSEL-296, R-JS-18/19). KeyID/SetKeyID give the id that
// Pagelove.PUT writes for a keyed instance (R-MOD-34).
//
// # Authorization
//
// An authz hook (authz.RegisterHook) turns items of schema subtypes of
// Group into groups whose includes() method decides membership (R-PERM-60),
// items of subtypes of AuthorizationRule into rules (R-PERM-7), applies
// @read resolvers on AuthorizationRule.actor (R-PERM-8), and gives rule
// selectors the :isa() inheritance map.
//
// # JavaScript
//
// JavaScript/Module slots run through a JSRunner installed with
// SetJSRunner (internal/jsglue, the internal/jsrt integration). JSCall
// carries what the runner needs beyond the JSON-like values: the Sessel
// receiver of a direct method call (instance or class), the dispatching
// element of a composition dispatch, and the Sessel host and budget for
// Sessel methods JavaScript calls through imported classes. Without a
// runner, every JavaScript slot fails clearly with HTTP 501 and a
// BindingFailure of variant "unavailable".
//
// # Range selectors
//
// The package installs engine.SelectorIsA, so :isa() in Range selectors
// matches declared schemas and their descendants (R-COMP-177).
package schema
