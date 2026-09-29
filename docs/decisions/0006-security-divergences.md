# 0006: Security divergences from PageLove

- **Status:** accepted
- **Date:** 2026-09-29
- **Policy:** [0005](0005-divergence-policy.md), class
  keep-documented-security. Each item has a `.live` sibling or a probe that
  asserts PageLove's behaviour.

pagelike follows PageLove's behaviour except where doing so would weaken
one of these properties. The list is what was decided and why.

| Property | PageLove (observed) | pagelike | Case |
|---|---|---|---|
| A selector-scoped Deny overrides a resource Allow at the same tier | the denied element is served or written | denied | `rw.authz.denied-fragment-read`, `authz.multimatch.*`, `authz.selector.selector-deny-overrides-resource-allow-same-tier` |
| OPTIONS reflects what is enforced | advertises methods a Deny removed | consistent with enforcement | `protocol.options.selector-deny-removes-method` |
| A rule is what it says | a padded `deny` is ignored; table rows merge (one row's grant leaks onto another's selector) | a padded deny is honoured; rows stay separate | `authz.discovery.*` |
| Absence isn't revealed to non-readers | MOVE answers 416 to a non-reader | fails closed | `protocol.move-authz.fail-closed-when-unreadable` |
| Personalized pages aren't cached publicly | `Cache-Control: public, max-age=5` on a page that reads the signed-in user | `private` | `rw.reqdoc.auth-read-is-private` |
| Errors don't leak other documents | uniqueness errors name the other document's path | withheld | `modeling.uniqueness.duplicate-across-documents` |
| Declared constraints hold on every write path | WebDAV PUTs skip schema and shape checks | validated (the shop's closed Order shape keeps scripts out of orders written by its worker) | `modeling.write-paths.webdav-put-validated-by-schema`, `modeling.shapes.webdav-write-checked`, `apps.shop.order-shape-dav` |
| Gates fail closed | a runtime error in a trigger's `when` is false, so the gate fails open | the request fails (500) | probe in `reacting.context.probe-0929.trigger-bindings` |
| Stored markup can't turn into live script | `<` and `>` stay literal in attribute values while noscript content is markup, a mutation-XSS path | `<` and `>` escaped in attribute values (byte-level only) | `TestNoscriptAttributeCannotBreakOut` |

Adopting PageLove's behaviour was judged **not** to weaken a boundary in
these cases (details in the decision files):
- anonymous `auth` is an object with no claims;
- QUERY sees only its target document, which is stricter;
- reaction items don't bind `r:` resources.

Boundaries that don't depend on PageLove at all:
- authoring keys never reach browsers and are never an end-user identity;
- sites are isolated (`internal/server/isolation_test.go`);
- the server-JavaScript sandbox has budgets and OS limits;
- the development auth bypass is opt-in and loopback-only.
