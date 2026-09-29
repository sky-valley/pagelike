# pagelike architecture: packages, ownership and integration contracts

This complements `docs/design.md`. It fixes the seams between packages so
feature work can proceed in parallel without editing each other's code.

## Dependency direction (no cycles)

```
cmd/pagelike, harness
   └─ internal/features (links every feature package)
        └─ internal/jsglue (integration: jsrt ⇄ schema, compose, reactions)
   └─ internal/server  (wires everything; the only place features are registered)
        ├─ internal/httpapi (public plane)   ├─ internal/dav (authoring plane)
        ├─ internal/compose ─┐               ├─ internal/reactions ─┐
        ├─ internal/schema ──┼── use ───────►│ internal/sessel      │
        │                    │               │ internal/jsrt        │
        │                    │               │ internal/liquid      │
        ├─ internal/engine ◄─┘ (hooks)       └──────────────────────┘
        ├─ internal/site, internal/store, internal/sse, internal/authz, internal/identity
        └─ internal/dom, internal/htmlser, internal/xmldom, internal/selector, internal/microdata, internal/errdoc
```

Rules:

- **Language runtimes** (`sessel`, `jsrt`, `liquid`) import only the leaf
  packages (`dom`, `htmlser`, `xmldom`, `selector`, `microdata`, `errdoc`)
  and the standard library. They never import `site`, `store`, `engine`,
  `schema`, `compose` or `reactions`. Everything they need from the host is an
  interface they declare (`sessel.Host`, `jsrt.Host`, Liquid data/drops).
- **Feature packages** (`schema`, `compose`, `reactions`) import the language
  runtimes and `engine`/`site`, and implement the runtimes' host interfaces.
- **`internal/server`** is the composition root: `server.Extend` registers
  feature packages (each package exposes `func Register(s *server.Server)` or
  is wired directly in `server.New`). Feature packages must not mutate
  another package's state except through these hooks.
- **`internal/jsglue`** is the one package that imports several feature
  packages: it owns the process-wide `jsrt.Runtime` (lazy; pool size from
  `pagelike serve --js-workers`), implements `jsrt.Host` over the schema
  registry, and installs the feature packages' JavaScript seams, the schema
  registry's method dispatcher for composition and `schema.SetKeyID` for
  `Pagelove.PUT` (see its package documentation). Linking it
  (`internal/features`) is what turns server JavaScript on.

## Extension points (stable)

| Hook | Owner | Consumer(s) |
|---|---|---|
| `engine.Hooks.BeforeWrite` (list) | engine | (free; triggers run in `httpapi.Public.Intercept`, before core, so they also cover reads, missing documents and throws that must pre-empt core errors) |
| `engine.Hooks.Validate` (list; `AddValidate(phase, f)`) | engine | schema (`engine.PhaseSchema`: Schema/Property/Types/Shape/GroupConstraint/uniqueness/references), reactions (`engine.PhaseTransition`: TransitionConstraint). Phases fix R-MOD-15's order whatever the registration order (R-REACT-71) |
| `engine.Hooks.AfterWrite` (list; `AddAfterWrite(phase, f)`) | engine | schema (`engine.PhaseSchema`: cascades), reactions (HTTPRequest outbox rows queued by triggers, TransitionHandler deliveries; processors run in `httpapi.Public.Intercept`), participation records |
| `httpapi.Public.Intercept` (`httpapi.Interceptor`) | httpapi | reactions (trigger phase before core processing, processors over its buffered response, dispatch after the response; R-REACT-8) |
| `reactions.SetJSRunner` (`reactions.JSRunner`) | reactions | jsglue → jsrt (JavaScript gates, actions and dynamic values, per `reactions.JSCall.Slot`; nil → 501 BindingFailure) |
| `reactions.SetKeyIDFunc` | reactions | jsglue → `schema.SetKeyID` (the id `Pagelove.PUT` writes on a keyed instance, R-MOD-34) |
| `schema.SetJSRunner` (`schema.JSRunner`) | schema | jsglue → jsrt (defaults, resolvers, validators, computed properties, methods, Group `includes()`; nil → 501 BindingFailure) |
| `engine.SelectorIsA` (`Range.BindSnapshot`) | engine | schema (`:isa()` in `Range` selectors over the registry, R-COMP-177) |
| `engine.Composer` (`httpapi.Public.Compose`) | engine/httpapi | compose |
| `httpapi.Router` (`Public.Route`) | httpapi | compose (parameterized routes) |
| `engine.TemplateCreate` | engine | compose (templated resource creation) |
| `engine.ComposeWrite`, `engine.ApplyRouted`, `WriteCtx.Locate` | engine (`compose_hooks.go`) | compose (selector writes resolved on the composed view: write-through to origins, transient session copies, 416 for generated content) |
| `engine.SelectorFunctions` (`engine.ExpandRange`) | engine | compose (`count()`/`text-of()`/`value-of()`/`attr-of()` in `Range` selectors) |
| `compose.SetJSRunner` (`compose.JSRunner`) | compose | jsglue → jsrt (`j:` bindings, JavaScript methods; without it they answer 501 `JavaScriptUnavailable`) |
| `compose.SetMethodDispatcher` (`compose.MethodDispatcher`) | compose | jsglue → `schema.DispatchMethod` (method elements/attributes over the schema registry; the fallback `compose.SchemaMethods` reads Schema items) |
| `httpapi.Public.Queries["text/sessel"]` | httpapi | `internal/query` (Sessel QUERY; `engine.ReadOp.Target` keeps the directory slash) |
| `query.SetClassSource` | query | schema (supplies `sessel.Class` registries; default is `sessel.MicrodataClasses`) |
| `sessel.Host`, `sessel.Class`, `sessel.Writer` | sessel | query (`query.Host`, read-only over a snapshot), schema (Classes), reactions/compose (hosts with `WriteProvider`, Context, bindings) |
| `site.Snapshot.Ext(key, build)` | site | every feature package caches its per-generation index here (schema registry, rule/trigger lists, route table) |
| `httpapi.OIDCLogin`, `httpapi.PagelikeExtra` | httpapi | identity (OIDC), pagelike extensions |
| `authz.Group.Includes` | authz | schema (Group subtypes with `includes()`) |

`engine.WriteCtx` carries the snapshot, the transaction, the op, the
before/after DOMs, the target, and `Extra` documents changed by side-effect
writes. Writes that must commit or fail with the outer write (cascades,
templated creation) go through `engine.Apply` inside the same transaction and
are published with it. Trigger and processor side-effect writes
(`Pagelove.PUT`/`DELETE`) instead use `engine.Write` in their own
transaction: they must survive a later throw (docs/spec/reacting.md
R-REACT-36), so the trigger phase runs before the site write mutex is taken.

## Value model across languages

Sessel owns the canonical value model (`sessel.Value`): null, bool, number,
string, list, dictionary (ordered), element (queried: node + document path +
document root; constructed: detached node), selector, class, instance,
lambda, temporal values, document, blob. `jsrt` marshals to/from JSON-like
data plus tagged elements (`{"$type":"element","$html":…,"$source":…}`, the
same shape as `application/sessel+json`). Liquid receives elements as
microdata item drops (see `docs/decisions/0002-liquid.md`).

## Isolation

- Server JavaScript runs only in `pagelike jsrt-worker` child processes
  (decision 0001): engine limits + OS limits + parent watchdog. The DOM
  crosses as serialized HTML; coarse callbacks (schema lookup, Sessel method
  calls, Context write-back) go over the same pipe.
- Sessel and Liquid run in-process under per-request budgets (ops, time,
  memory) shared across one request.
- Each site is a separate database; every graph query is scoped to one site.
  Resource bindings see the whole site (PageLove's trusted-author model) but
  never another site.
