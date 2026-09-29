package httpapi

import (
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sky-valley/pagelike/internal/errdoc"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/site"
)

// Identity on the public plane (docs/spec/permissions-identity.md §15):
// sessions for every visitor, the built-in login, callback and logout
// endpoints (per-site paths, default /auth/*), an OpenID Connect relying
// party per site, local password accounts, and the authorization refusal
// documents. Built-in endpoints are not subject to rules and take
// precedence over stored documents at their paths.

// OIDCLogin, when set, overrides the built-in login handling (it returns
// true when it answered the request).
var OIDCLogin func(p *Public, w http.ResponseWriter, r *http.Request, rc *reqCtx) bool

// relyingParty is shared by every site; it caches provider discovery
// documents and key sets per issuer.
var relyingParty = &identity.RP{}

// pageloveLoginAlias is the login link PageLove's docs show; it behaves as
// the site's login path (§18 C2).
const pageloveLoginAlias = "/-pagelove/oidc/login"

// localLoginPath serves pagelike's own password sign-in form.
const localLoginPath = "/-pagelike/login"

// authEndpoint serves the built-in identity endpoints. It reports whether
// the request was one of them.
func (p *Public) authEndpoint(w http.ResponseWriter, r *http.Request, rc *reqCtx) bool {
	set := rc.site.Settings()
	switch r.URL.Path {
	case set.LoginPath, pageloveLoginAlias:
		p.login(w, r, rc)
	case set.CallbackPath:
		p.callback(w, r, rc)
	case set.LogoutPath:
		p.logout(w, r, rc)
	default:
		return false
	}
	return true
}

// principal resolves the requester, issuing an anonymous session cookie on
// first contact and whenever the presented session is expired or no longer
// valid (R-PERM-64, R-PERM-65).
func (p *Public) principal(w http.ResponseWriter, r *http.Request, s *site.Site) *identity.Principal {
	if pr := p.devPrincipal(r, s); pr != nil {
		return pr
	}
	pol := p.cookiePolicy(r, s)
	pr, st := s.Users.ResolveSession(r.Context(), pol.SessionCookie(r))
	if st.NeedsNewID() {
		pr = identity.Anonymous(identity.NewSessionID())
		pol.Write(w, pr.Session, identity.AnonymousCookieTTL)
	}
	return pr
}

// devPrincipal implements the development impersonation header. It is only
// honoured when the server runs with --dev-insecure-auth (which refuses
// non-loopback listeners), for a loopback peer, and never for a request
// relayed by a proxy; every use is logged.
func (p *Public) devPrincipal(r *http.Request, s *site.Site) *identity.Principal {
	u := r.Header.Get("X-Pagelike-Dev-User")
	if !p.DevAuth || u == "" || !isLoopback(r) || relayed(r) {
		return nil
	}
	pr, err := s.Users.LocalPrincipal(r.Context(), u, "dev-"+u)
	if err != nil {
		return nil
	}
	p.logger().Warn("dev auth impersonation", "user", u, "site", s.Name)
	return pr
}

// relayed reports whether a request carries proxy forwarding headers.
func relayed(r *http.Request) bool {
	for _, h := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip"} {
		if r.Header.Get(h) != "" {
			return true
		}
	}
	return false
}

func (p *Public) cookiePolicy(r *http.Request, s *site.Site) identity.CookiePolicy {
	set := s.Settings()
	return identity.CookiePolicy{
		Name:        set.SessionCookie,
		Secure:      identity.IsTLS(r, p.TrustProxy) || isLocalhostName(hostOnly(r.Host)),
		Partitioned: set.Cookies == "partitioned",
	}
}

func sessionTTL(set site.Settings) time.Duration {
	if d, err := time.ParseDuration(set.SessionLifetime); err == nil && d > 0 {
		return d
	}
	return identity.SessionTTL
}

func hasOIDC(set site.Settings) bool {
	return set.OIDC != nil && strings.TrimSpace(set.OIDC.Issuer) != ""
}

// hasIdentityProvider reports whether visitors can sign in at all (an OpenID
// provider or local password accounts); refusal documents link to the login
// and logout paths only then (R-PERM-55, §18 C3).
func hasIdentityProvider(r *http.Request, s *site.Site) bool {
	return hasOIDC(s.Settings()) || s.Users.HasPasswordAccounts(r.Context())
}

// rpConfig builds the site's relying-party configuration for this request.
func (p *Public) rpConfig(r *http.Request, set site.Settings) identity.RPConfig {
	scheme := "http"
	if identity.IsTLS(r, p.TrustProxy) {
		scheme = "https"
	}
	return identity.RPConfig{
		Issuer: set.OIDC.Issuer, ClientID: set.OIDC.ClientID, ClientSecret: set.OIDC.ClientSecret,
		Scopes: strings.Fields(set.OIDC.Scopes), RolesClaim: set.OIDC.RolesClaim,
		RedirectURL: scheme + "://" + r.Host + set.CallbackPath,
	}
}

// safeRedirect keeps post-login redirects on the same site: an absolute
// path, never a scheme-relative or backslash URL.
func safeRedirect(v string) string {
	if v == "" || !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") || strings.ContainsAny(v, "\\\r\n\t") {
		return "/"
	}
	if u, err := url.Parse(v); err != nil || u.Host != "" || u.Scheme != "" {
		return "/"
	}
	return v
}

// redirectParam reads the post-login target: PageLove's redirect-post, plus
// common aliases.
func redirectParam(r *http.Request) string {
	q := r.URL.Query()
	for _, k := range []string{"redirect-post", "redirect", "return_to", "next"} {
		if v := q.Get(k); v != "" {
			return v
		}
		if r.Method == http.MethodPost {
			if v := r.PostFormValue(k); v != "" {
				return v
			}
		}
	}
	return ""
}

// login serves the login path (R-PERM-67). GET redirects to the site's
// OpenID provider, or to the local sign-in form when the site only has
// password accounts; without either it is 404. POST signs in a local
// account (username + password form fields).
func (p *Public) login(w http.ResponseWriter, r *http.Request, rc *reqCtx) {
	if OIDCLogin != nil && OIDCLogin(p, w, r, rc) {
		return
	}
	set := rc.site.Settings()
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	case http.MethodPost:
		p.passwordLogin(w, r, rc)
		return
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		p.fail(w, r, rc, errdoc.New(http.StatusMethodNotAllowed, "MethodNotAllowed", "the login endpoint accepts GET and POST"))
		return
	}
	target := safeRedirect(redirectParam(r))
	switch {
	case hasOIDC(set):
		// Each start stores a pending login; bound how fast one client can
		// create them.
		key := rc.site.Name + "|" + p.clientAddr(r)
		if !loginStartThrottle.Allow(key) {
			w.Header().Set("Retry-After", "600")
			p.fail(w, r, rc, errdoc.New(http.StatusTooManyRequests, "TooManyLogins", "too many sign-in attempts; try again later"))
			return
		}
		loginStartThrottle.Fail(key)
		loc, err := relyingParty.Begin(r.Context(), rc.site.Users, p.rpConfig(r, set), rc.principal.Session, target)
		if err != nil {
			p.identityFailure(w, r, rc, err)
			return
		}
		http.Redirect(w, r, loc, http.StatusFound)
	case rc.site.Users.HasPasswordAccounts(r.Context()):
		http.Redirect(w, r, localLoginPath+"?redirect-post="+url.QueryEscape(target), http.StatusSeeOther)
	default:
		p.fail(w, r, rc, errdoc.New(http.StatusNotFound, "NoIdentityProvider", "this site has no identity provider configured"))
	}
}

// callback completes an OpenID login (R-PERM-68).
func (p *Public) callback(w http.ResponseWriter, r *http.Request, rc *reqCtx) {
	set := rc.site.Settings()
	w.Header().Set("Cache-Control", "no-store")
	if !hasOIDC(set) {
		p.fail(w, r, rc, errdoc.New(http.StatusNotFound, "NoIdentityProvider", "this site has no identity provider configured"))
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		p.fail(w, r, rc, errdoc.New(http.StatusMethodNotAllowed, "MethodNotAllowed", "the callback endpoint accepts GET"))
		return
	}
	id, target, err := relyingParty.Complete(r.Context(), rc.site.Users, p.rpConfig(r, set), rc.principal.Session, r.URL.Query())
	if err != nil {
		p.identityFailure(w, r, rc, err)
		return
	}
	p.signIn(w, r, rc, id, target, http.StatusFound)
}

// passwordLogin signs in a local account from a form post.
func (p *Public) passwordLogin(w http.ResponseWriter, r *http.Request, rc *reqCtx) {
	if o := r.Header.Get("Origin"); o != "" && o != "null" {
		if u, err := url.Parse(o); err != nil || !strings.EqualFold(u.Host, r.Host) {
			p.fail(w, r, rc, errdoc.New(http.StatusForbidden, "CrossOriginLogin", "sign-in forms must be posted from this site"))
			return
		}
	}
	key := rc.site.Name + "|" + p.clientAddr(r)
	if !loginThrottle.Allow(key) {
		w.Header().Set("Retry-After", "600")
		p.loginForm(w, r, rc, "Too many failed sign-in attempts; try again later.", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		p.loginForm(w, r, rc, "Could not read the form.", http.StatusBadRequest)
		return
	}
	usr, err := rc.site.Users.Authenticate(r.Context(), strings.TrimSpace(r.PostFormValue("username")), r.PostFormValue("password"))
	if err != nil {
		loginThrottle.Fail(key)
		p.loginForm(w, r, rc, "Unknown user or wrong password.", http.StatusUnauthorized)
		return
	}
	loginThrottle.Reset(key)
	p.signIn(w, r, rc, usr.LocalIdentity(), safeRedirect(redirectParam(r)), http.StatusSeeOther)
}

// loginThrottle bounds failed password sign-ins per site and client address;
// loginStartThrottle bounds OpenID logins started (each stores a pending
// login state until it completes or expires).
var (
	loginThrottle      = identity.NewThrottle(20, 10*time.Minute)
	loginStartThrottle = identity.NewThrottle(120, 10*time.Minute)
)

// clientAddr is the client's IP address: the peer, or with --trust-proxy
// the address the proxy appended last to X-Forwarded-For.
func (p *Public) clientAddr(r *http.Request) string {
	if p.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	return hostOnly(r.RemoteAddr)
}

// signIn replaces the visitor's session with a signed-in one (a new id;
// transient content moves along) and redirects.
func (p *Public) signIn(w http.ResponseWriter, r *http.Request, rc *reqCtx, id *identity.Identity, target string, status int) {
	ttl := sessionTTL(rc.site.Settings())
	sid, err := rc.site.Users.Login(r.Context(), rc.principal.Session, id, ttl)
	if err != nil {
		p.fail(w, r, rc, err)
		return
	}
	p.cookiePolicy(r, rc.site).Write(w, sid, ttl)
	http.Redirect(w, r, safeRedirect(target), status)
}

// logout ends the session and starts a new anonymous one (R-PERM-69).
func (p *Public) logout(w http.ResponseWriter, r *http.Request, rc *reqCtx) {
	status := http.StatusFound
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	case http.MethodPost:
		status = http.StatusSeeOther
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		r.ParseForm()
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		p.fail(w, r, rc, errdoc.New(http.StatusMethodNotAllowed, "MethodNotAllowed", "the logout endpoint accepts GET and POST"))
		return
	}
	if err := rc.site.Users.Logout(r.Context(), rc.principal.Session); err != nil {
		p.fail(w, r, rc, err)
		return
	}
	p.cookiePolicy(r, rc.site).Write(w, identity.NewSessionID(), identity.AnonymousCookieTTL)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, safeRedirect(redirectParam(r)), status)
}

// identityFailure answers a failed login step: 503 for provider
// misconfiguration or outage, 400 for a login that cannot complete.
func (p *Public) identityFailure(w http.ResponseWriter, r *http.Request, rc *reqCtx, err error) {
	var ce *identity.ConfigError
	var cb *identity.CallbackError
	switch {
	case errors.As(err, &ce):
		p.logger().Warn("identity provider unavailable", "site", rc.site.Name, "err", ce)
		p.fail(w, r, rc, errdoc.New(http.StatusServiceUnavailable, "IdentityProviderUnavailable", "%s", ce.Msg))
	case errors.As(err, &cb):
		p.logger().Info("sign-in failed", "site", rc.site.Name, "err", cb)
		p.fail(w, r, rc, errdoc.New(http.StatusBadRequest, "SignInFailed", "%s", cb.Msg))
	default:
		p.fail(w, r, rc, err)
	}
}

// denialDocument renders an authorization refusal (engine.Denied and the
// SSE subscribe check) as the documented 1.0 Error document (R-PERM-55/56).
// ok is false for other errors.
func denialDocument(r *http.Request, rc *reqCtx, e *errdoc.Error) (body string, ok bool) {
	if rc == nil || (e.Kind != "Unauthorized" && e.Kind != "Forbidden") {
		return "", false
	}
	login, logout := "", ""
	if hasIdentityProvider(r, rc.site) {
		set := rc.site.Settings()
		login, logout = set.LoginPath, set.LogoutPath
	}
	return e.RenderDenial(login, logout), true
}

func (p *Public) loginForm(w http.ResponseWriter, r *http.Request, rc *reqCtx, msg string, status int) {
	red := html.EscapeString(safeRedirect(redirectParam(r)))
	errHTML := ""
	if msg != "" {
		errHTML = `<p class="err" role="alert">` + html.EscapeString(msg) + `</p>`
	}
	page := `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Sign in · ` + html.EscapeString(rc.site.Name) + `</title>
<style>body{font:16px system-ui,sans-serif;display:grid;place-items:center;min-height:100vh;margin:0;background:#f6f5f2;color:#1b1b1b}
form{background:#fff;padding:24px;border-radius:12px;box-shadow:0 1px 4px #0002;display:grid;gap:12px;width:min(320px,90vw)}
input,button{font:inherit;padding:8px 10px;border-radius:8px;border:1px solid #bbb}button{background:#1b1b1b;color:#fff;border:0;cursor:pointer}
.err{color:#b00020;margin:0}</style></head>
<body><form method="post" action="` + localLoginPath + `">
<h1 style="margin:0;font-size:20px">Sign in</h1>` + errHTML + `
<label>Username or email<br><input name="username" autocomplete="username" required></label>
<label>Password<br><input name="password" type="password" autocomplete="current-password" required></label>
<input type="hidden" name="redirect-post" value="` + red + `">
<button type="submit">Sign in</button></form></body></html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors "+frameAncestors(rc))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		w.Write([]byte(page))
	}
}

// pagelikeAPI serves pagelike-specific endpoints under /-pagelike/. They
// never shadow PageLove paths.
func (p *Public) pagelikeAPI(w http.ResponseWriter, r *http.Request, rc *reqCtx) {
	switch strings.TrimPrefix(r.URL.Path, "/-pagelike/") {
	case "whoami":
		pr := rc.principal
		out := map[string]any{"authenticated": pr.Authenticated, "site": rc.site.Name}
		if pr.Authenticated {
			out["sub"], out["email"], out["email_verified"], out["name"], out["roles"] = pr.Sub, pr.Email, pr.EmailVerified, pr.Name, pr.Roles
			if pr.Picture != "" {
				out["picture"] = pr.Picture
			}
			if pr.Issuer != "" {
				out["issuer"] = pr.Issuer
			}
			if snap, err := rc.site.Index(r.Context()); err == nil {
				out["role_list"] = snap.Policy.RoleList(pr)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(out)
	case "login":
		if !rc.site.Users.HasPasswordAccounts(r.Context()) {
			p.fail(w, r, rc, errdoc.New(http.StatusNotFound, "NoLocalAccounts", "this site has no password accounts"))
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			p.loginForm(w, r, rc, "", http.StatusOK)
		case http.MethodPost:
			p.passwordLogin(w, r, rc)
		default:
			w.Header().Set("Allow", "GET, HEAD, POST")
			p.fail(w, r, rc, errdoc.New(http.StatusMethodNotAllowed, "MethodNotAllowed", "use GET or POST"))
		}
	default:
		if PagelikeExtra != nil && PagelikeExtra(p, w, r, rc) {
			return
		}
		if p.runExtensions(w, r, rc) {
			return
		}
		http.NotFound(w, r)
	}
}

// PagelikeExtra lets other packages add /-pagelike/ endpoints.
var PagelikeExtra func(p *Public, w http.ResponseWriter, r *http.Request, rc *reqCtx) bool

// frameAncestors is the CSP frame-ancestors source list for sign-in pages:
// 'none' unless the site lists trusted embedding origins (clickjacking
// protection stays on by default).
func frameAncestors(rc *reqCtx) string {
	var out []string
	for _, o := range rc.site.Settings().EmbedOrigins {
		o = strings.TrimSpace(o)
		// Only scheme://host[:port] sources; anything else is ignored.
		if u, err := url.Parse(o); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.Path == "" && !strings.ContainsAny(o, " ;'\"") {
			out = append(out, u.Scheme+"://"+u.Host)
		}
	}
	if len(out) == 0 {
		return "'none'"
	}
	return "'self' " + strings.Join(out, " ")
}
