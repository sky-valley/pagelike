# End-user identity in pagelike

How visitors of a pagelike site get an identity, how that identity is matched
by `AuthorizationRule`s, and how to carry identities over when moving a site
from PageLove. Behavior is specified in `docs/spec/permissions-identity.md`
(§3, §15, §16); this page is the operator's view. Code: `internal/identity`,
`internal/httpapi/auth.go`, `internal/authz`.

## The principal

Every public-plane request is either anonymous or carries a signed-in
principal:

| Field | Meaning | Used by rules as |
|---|---|---|
| `sub` | the user name: the OpenID `sub` claim, or the local account name | exact actor (top specificity), `${request.auth.username}`, `:username` |
| `email` + `email_verified` | from the provider / the account | email actor and `Group` `member` matching, **only when verified** |
| `roles` | the provider's roles claim, plus the roles of a local account with the same `sub` | bare actor tokens and `role:X` |
| `name`, `picture`, all claims | informational | `${request.auth.name}`, `${request.auth.claims.X}` |

Display names, `preferred_username` and unverified emails never match a rule.
Matching is exact and case-sensitive. API keys and `Authorization` headers are
never an end-user identity on the public plane: such requests are anonymous
(they authenticate the WebDAV authoring plane and the console only).

## Identity sources

A site can offer an OpenID Connect provider, local password accounts, or both.
Without either, every visitor is anonymous, the login path answers `404`, and
refusal documents carry no login link.

### OpenID Connect (PageLove's per-host OIDC settings)

```
echo "$CLIENT_SECRET" | pagelike identity set --site blog \
    --issuer https://idp.example.com/.well-known/openid-configuration \
    --client-id pagelike-blog --client-secret-stdin \
    [--scopes "openid email profile"] [--roles-claim roles] \
    [--login-path /auth/login] [--logout-path /auth/logout] [--callback-path /auth/callback]
pagelike identity show --site blog
```

| PageLove console (per host) | pagelike setting (`site.db` settings JSON) | `pagelike identity set` flag |
|---|---|---|
| openid-configuration URL | `oidc.openid_configuration` | `--issuer` |
| client-id | `oidc.client_id` | `--client-id` |
| client-secret | `oidc.client_secret` (never exported) | `--client-secret-stdin` |
| login-path / logout-path / callback | `login_path`, `logout_path`, `callback_path` (also used by local accounts) | `--login-path`, `--logout-path`, `--callback-path` |
| (pagelike) scopes, roles claim | `oidc.scopes`, `oidc.roles_claim` | `--scopes`, `--roles-claim` |
| `default-get-authz-mode` | `default_get` | `pagelike site create --default-get` |

Register `https://<site host><callback path>` (default
`https://blog.example.org/auth/callback`) as the redirect URI at the provider.
`--issuer` accepts the issuer URL or its openid-configuration URL (PageLove's
console field). `--roles-claim` names the claim carrying roles; a dotted path
reaches nested claims (Keycloak: `realm_access.roles`); a single string value is
split on whitespace.

The flow is the authorization-code flow with PKCE (S256). `state` and `nonce`
are random, single-use, expire after 10 minutes and are bound to the visitor's
session, so a callback started elsewhere is refused. The ID token is verified
against the provider's JWKS (issuer, audience, expiry, nonce); userinfo claims
are merged when the provider has a userinfo endpoint. A misconfigured or
unreachable provider makes the login path answer `503` with an Error item; a
failed callback answers `400` and leaves the session unchanged.

Endpoints (not subject to rules; they take precedence over stored documents):

| Path | Method | Behavior |
|---|---|---|
| login path, and `/-pagelove/oidc/login` | GET | `302` to the provider (`?redirect-post=/path` chooses where to land); with only local accounts, `303` to `/-pagelike/login` |
| login path, `/-pagelike/login` | POST | local password sign-in (`username`, `password`, `redirect-post`) |
| callback path | GET | completes an OpenID login |
| logout path | GET, POST | ends the session, `302`/`303` to `redirect-post` or `/` |
| `/-pagelike/whoami` | GET | the current principal as JSON (pagelike extension) |

Redirect targets must be same-site absolute paths; anything else becomes `/`.

### Local accounts

```
pagelike user add --site blog alice --email alice@example.org --verified --roles editors --password-stdin
pagelike user passwd --site blog alice --password-stdin      # or --clear
pagelike user list --site blog
pagelike user delete --site blog alice                       # also signs alice out everywhere
```

An account without a password cannot sign in, but its roles are added to a
provider identity with the same `sub`: that is how an administrator grants
roles to OpenID users whose provider has no roles claim.

## Sessions

Every visitor gets a session cookie on first contact (`__Host-session` on
HTTPS and `*.localhost`, `pagelike_session` on plain HTTP; `HttpOnly`,
`Path=/`, no `Domain`, `SameSite=Lax`; `--cookies partitioned` switches to
`SameSite=None; Secure; Partitioned` for sites embedded in other sites' frames).
Anonymous sessions are not stored; they key transient content and SSE echo
suppression.

Signing in always issues a new session id (defeating session fixation) and
moves the visitor's transient content to it. Signing out deletes the session
and its transient content and issues a new anonymous id. Signed-in sessions
last `--session-lifetime` (default 720h); an expired, signed-out or deleted
session makes the request anonymous and the response carries a fresh anonymous
cookie.

`--dev-insecure-auth` (development only) lets loopback clients impersonate a
local account with `X-Pagelike-Dev-User`. `pagelike serve` refuses the flag on
non-loopback listen addresses, and the header is ignored on requests carrying
proxy forwarding headers (`Forwarded`, `X-Forwarded-For`, …) so a local
reverse proxy cannot expose it.

## Migrating identities from PageLove

Rules and data written for PageLove name users in three ways; each needs a
different treatment when the site moves to pagelike.

1. **Verified emails and group names** (`alice@example.org`, `editors`,
   `Group` documents, `users`): nothing to do. They match whatever identity
   source verifies the same email or asserts the same role.

2. **OpenID subjects** (`sub_X3jX…` in `actor` values, `${request.auth.username}`
   paths such as `/users/sub_X3jX…/`, `data-owner` attributes):
   - *Same provider*: configure pagelike with the provider PageLove used (same
     issuer, a client registered for the pagelike host). The provider asserts
     the same `sub`, so rules and data keep working unchanged.
   - *New provider*: the new provider asserts different subjects. Map each
     person's new subject to the old one instead of rewriting rules and data:

     ```
     pagelike user link --site blog --issuer https://new-idp.example.com \
         --idp-sub 7c1e0f… sub_X3jX…
     pagelike user links --site blog
     ```

     At sign-in, a linked identity's `sub` (and `request.auth.username`) is the
     old subject; its claims (`request.auth.claims.sub`) keep the provider's
     value. Links are keyed by issuer, so they cannot be claimed through
     another provider.
   - *Local accounts instead of a provider*: create the account under the old
     subject, so rules keep matching:

     ```
     pagelike user add --site blog sub_X3jX… --email alice@example.org --verified --password-stdin
     ```

3. **Provider roles** (`admins` asserted by PageLove's identity provider): keep
   a provider that asserts them (`--roles-claim` if the claim is not `roles`),
   or grant them locally with a password-less account per subject
   (`pagelike user add --site blog sub_X3jX… --roles admins`), or replace them
   with a `Group` document listing verified emails.

To find the subjects a site depends on, search its documents for
`itemprop="actor"` values that are neither `*`, `users`, `authenticated`,
emails, group names nor `role:` forms, and for subject-shaped path segments.
Exported sites (`pagelike export`) never contain accounts, sessions, links or
client secrets; recreate them on the target instance.
