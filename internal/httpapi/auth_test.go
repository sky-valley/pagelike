package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sky-valley/pagelike/internal/control"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/identity/oidctest"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

// fixture is a pagelike server with one site "t" served at t.localhost.
type fixture struct {
	t    *testing.T
	ts   *httptest.Server
	site *site.Site
}

func newFixture(t *testing.T, set site.Settings) *fixture {
	t.Helper()
	dir := t.TempDir()
	reg, err := site.NewRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctl, err := control.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := server.New(server.Config{Domain: "localhost"}, reg, ctl, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s, err := reg.Create(context.Background(), "t", set)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv)
	t.Cleanup(func() { ts.Close(); reg.Close(); ctl.Close() })
	return &fixture{t: t, ts: ts, site: s}
}

// browser keeps the session cookie of one visitor.
type browser struct {
	f      *fixture
	cookie string // current session id
}

type resp struct {
	status int
	header http.Header
	body   string
}

func (b *browser) do(method, path string, form url.Values, hdr ...string) resp {
	b.f.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, b.f.ts.URL+path, body)
	req.Host = "t.localhost"
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	if b.cookie != "" {
		req.AddCookie(&http.Cookie{Name: identity.CookieSecure, Value: b.cookie})
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := client.Do(req)
	if err != nil {
		b.f.t.Fatal(err)
	}
	defer r.Body.Close()
	data, _ := io.ReadAll(r.Body)
	for _, c := range r.Cookies() {
		if c.Name == identity.CookieSecure {
			if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
				b.f.t.Errorf("session cookie attributes: %+v", c)
			}
			b.cookie = c.Value
		}
	}
	return resp{r.StatusCode, r.Header, string(data)}
}

func (b *browser) whoami() map[string]any {
	b.f.t.Helper()
	r := b.do("GET", "/-pagelike/whoami", nil)
	var out map[string]any
	if err := json.Unmarshal([]byte(r.body), &out); err != nil {
		b.f.t.Fatalf("whoami: %v (%s)", err, r.body)
	}
	return out
}

// followIdP performs the provider leg of a login: the authorization
// request (auto-approved by the fake provider) and returns the callback
// path with its query.
func followIdP(t *testing.T, loc string) string {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := client.Get(loc)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusFound {
		t.Fatalf("provider authorize: %d", r.StatusCode)
	}
	cb, err := url.Parse(r.Header.Get("Location"))
	if err != nil || cb.Host != "t.localhost" {
		t.Fatalf("provider redirected to %q", r.Header.Get("Location"))
	}
	return cb.RequestURI()
}

func TestOIDCLogin(t *testing.T) {
	idp := oidctest.New()
	defer idp.Close()
	idp.SetUser(map[string]any{"sub": "sub_X3jX", "email": "alice@example.com", "email_verified": "true",
		"name": "Alice", "picture": "https://example.com/a.png", "groups": []any{"editors", "staff"}})
	idp.SetUserinfo(map[string]any{"locale": "en"})
	f := newFixture(t, site.Settings{OIDC: &site.OIDCConfig{Issuer: idp.URL + "/.well-known/openid-configuration",
		ClientID: idp.ClientID, ClientSecret: idp.ClientSecret, RolesClaim: "groups"}})
	ctx := context.Background()

	b := &browser{f: f}
	r := b.do("GET", "/auth/login?redirect-post=/doc.html", nil)
	if r.status != http.StatusFound {
		t.Fatalf("login: %d %s", r.status, r.body)
	}
	anonID := b.cookie
	if anonID == "" {
		t.Fatal("no anonymous session issued")
	}
	loc, _ := url.Parse(r.header.Get("Location"))
	q := loc.Query()
	for k, want := range map[string]string{"response_type": "code", "client_id": idp.ClientID,
		"redirect_uri": "http://t.localhost/auth/callback", "code_challenge_method": "S256"} {
		if q.Get(k) != want {
			t.Errorf("authorization %s = %q, want %q", k, q.Get(k), want)
		}
	}
	if q.Get("state") == "" || q.Get("nonce") == "" || q.Get("code_challenge") == "" || !strings.Contains(q.Get("scope"), "openid") {
		t.Errorf("authorization request lacks state/nonce/PKCE/openid: %v", q)
	}
	// Transient content follows the visitor into the signed-in session.
	if _, err := f.site.Store.DB().Exec(`INSERT INTO transients(session_id, path, key, html) VALUES (?,?,?,?)`, anonID, "/doc.html", "k", "<p>x</p>"); err != nil {
		t.Fatal(err)
	}

	cb := followIdP(t, loc.String())
	r = b.do("GET", cb, nil)
	if r.status != http.StatusFound || r.header.Get("Location") != "/doc.html" {
		t.Fatalf("callback: %d %q %s", r.status, r.header.Get("Location"), r.body)
	}
	if b.cookie == anonID || !strings.HasPrefix(b.cookie, "s-") {
		t.Fatalf("session id not rotated on login: %q", b.cookie)
	}
	var moved int
	f.site.Store.DB().QueryRow(`SELECT count(*) FROM transients WHERE session_id = ?`, b.cookie).Scan(&moved)
	if moved != 1 {
		t.Errorf("transient rows moved to the new session: %d", moved)
	}
	who := b.whoami()
	if who["authenticated"] != true || who["sub"] != "sub_X3jX" || who["email"] != "alice@example.com" || who["email_verified"] != true ||
		who["name"] != "Alice" || who["picture"] != "https://example.com/a.png" || who["issuer"] != idp.URL {
		t.Errorf("principal = %v", who)
	}
	if roles, _ := who["role_list"].([]any); len(roles) != 4 || roles[0] != "alice@example.com" || roles[3] != "users" {
		t.Errorf("role list = %v", who["role_list"])
	}
	pr, _ := f.site.Users.ResolveSession(ctx, b.cookie)
	if pr.Claims["locale"] != "en" || pr.Claims["nonce"] != nil {
		t.Errorf("claims = %v (userinfo merged, protocol claims dropped)", pr.Claims)
	}
	if _, st := f.site.Users.ResolveSession(ctx, anonID); st != identity.SessionAnonymous {
		t.Errorf("old session id state = %v", st)
	}

	// The login state is single use.
	replay := &browser{f: f, cookie: b.cookie}
	if r := replay.do("GET", cb, nil); r.status != http.StatusBadRequest {
		t.Errorf("replayed callback: %d", r.status)
	}

	// Logout ends the session everywhere; the old cookie is anonymous and
	// gets a fresh id.
	signedIn := b.cookie
	if r := b.do("GET", "/auth/logout?redirect-post=/bye", nil); r.status != http.StatusFound || r.header.Get("Location") != "/bye" {
		t.Fatalf("logout: %d %q", r.status, r.header.Get("Location"))
	}
	if b.cookie == signedIn || b.whoami()["authenticated"] != false {
		t.Error("logout must end the session")
	}
	stale := &browser{f: f, cookie: signedIn}
	if stale.whoami()["authenticated"] != false || stale.cookie == signedIn {
		t.Error("an invalidated session must be anonymous with a new id")
	}
}

func TestOIDCLoginFailures(t *testing.T) {
	idp := oidctest.New()
	defer idp.Close()
	f := newFixture(t, site.Settings{OIDC: &site.OIDCConfig{Issuer: idp.URL, ClientID: idp.ClientID, ClientSecret: idp.ClientSecret}})
	start := func(b *browser) string {
		r := b.do("GET", "/auth/login", nil)
		if r.status != http.StatusFound {
			t.Fatalf("login: %d %s", r.status, r.body)
		}
		return followIdP(t, r.header.Get("Location"))
	}

	// A callback from another session is refused (login CSRF).
	cb := start(&browser{f: f})
	other := &browser{f: f}
	if r := other.do("GET", cb, nil); r.status != http.StatusBadRequest || other.whoami()["authenticated"] != false {
		t.Errorf("cross-session callback: %d", r.status)
	}

	// A token whose nonce does not match is refused.
	b := &browser{f: f}
	cb = start(b)
	idp.TamperNextNonce()
	if r := b.do("GET", cb, nil); r.status != http.StatusBadRequest || !strings.Contains(r.body, "nonce") {
		t.Errorf("tampered nonce: %d %s", r.status, r.body)
	}

	// Provider errors are reported, not trusted.
	b = &browser{f: f}
	b.do("GET", "/auth/login", nil)
	if r := b.do("GET", "/auth/callback?error=access_denied&state=x", nil); r.status != http.StatusBadRequest {
		t.Errorf("provider error: %d", r.status)
	}

	// An identity link maps the provider subject to a local user name.
	if err := f.site.Users.SetLink(context.Background(), identity.Link{Issuer: idp.URL + "/", IdPSub: "oidc-user", Sub: "sub_old"}); err != nil {
		t.Fatal(err)
	}
	b = &browser{f: f}
	b.do("GET", start(b), nil)
	if who := b.whoami(); who["sub"] != "sub_old" {
		t.Errorf("linked sub = %v", who["sub"])
	}

	// An unreachable provider is a 503 with an Error item.
	down := newFixture(t, site.Settings{OIDC: &site.OIDCConfig{Issuer: "http://127.0.0.1:1", ClientID: "x"}})
	if r := (&browser{f: down}).do("GET", "/auth/login", nil); r.status != http.StatusServiceUnavailable || !strings.Contains(r.body, "Error") {
		t.Errorf("unreachable provider: %d", r.status)
	}
	// A missing client id is a configuration error.
	noClient := newFixture(t, site.Settings{OIDC: &site.OIDCConfig{Issuer: idp.URL}})
	if r := (&browser{f: noClient}).do("GET", "/-pagelove/oidc/login", nil); r.status != http.StatusServiceUnavailable {
		t.Errorf("missing client id: %d", r.status)
	}
}

func TestPasswordLoginAndDenials(t *testing.T) {
	f := newFixture(t, site.Settings{})
	ctx := context.Background()
	b := &browser{f: f}
	// Without any identity provider the login path is 404 and refusals
	// carry no login link.
	if r := b.do("GET", "/auth/login", nil); r.status != http.StatusNotFound {
		t.Errorf("login without provider: %d", r.status)
	}
	r := b.do("PUT", "/doc.html", url.Values{})
	if r.status != http.StatusUnauthorized || strings.Contains(r.body, "Log in") ||
		r.header.Get("Content-Type") != "text/html; charset=utf-8" || r.header.Get("WWW-Authenticate") != "" {
		t.Errorf("401 without provider: %d %q %s", r.status, r.header.Get("Content-Type"), r.body)
	}

	if err := f.site.Users.Upsert(ctx, identity.User{Sub: "bob", Email: "bob@example.com", EmailVerified: true, Roles: []string{"staff"}}, "correct horse"); err != nil {
		t.Fatal(err)
	}
	if r := b.do("GET", "/auth/login?redirect-post=/x", nil); r.status != http.StatusSeeOther || !strings.HasPrefix(r.header.Get("Location"), "/-pagelike/login") {
		t.Errorf("login with local accounts: %d %q", r.status, r.header.Get("Location"))
	}
	if r := b.do("GET", "/-pagelike/login", nil); r.status != http.StatusOK || !strings.Contains(r.body, `name="password"`) {
		t.Errorf("local form: %d", r.status)
	}
	if r := b.do("PUT", "/doc.html", url.Values{}); !strings.Contains(r.body, `<p><a href="/auth/login">Log in</a></p>`) {
		t.Errorf("401 with a provider must link to the login path: %s", r.body)
	}
	if r := b.do("POST", "/auth/login", url.Values{"username": {"bob"}, "password": {"wrong"}}); r.status != http.StatusUnauthorized {
		t.Errorf("wrong password: %d", r.status)
	}
	if r := b.do("POST", "/auth/login", url.Values{"username": {"bob"}, "password": {"correct horse"}}, "Origin", "https://evil.example"); r.status != http.StatusForbidden {
		t.Errorf("cross-origin login post: %d", r.status)
	}
	anon := b.cookie
	r = b.do("POST", "/auth/login", url.Values{"username": {"bob@example.com"}, "password": {"correct horse"}, "redirect-post": {"//evil.example/"}})
	if r.status != http.StatusSeeOther || r.header.Get("Location") != "/" || b.cookie == anon {
		t.Fatalf("password login: %d %q", r.status, r.header.Get("Location"))
	}
	if who := b.whoami(); who["sub"] != "bob" || who["email_verified"] != true {
		t.Errorf("principal = %v", who)
	}
	// A first request that signs in sets exactly one session cookie.
	fresh := &browser{f: f}
	r = fresh.do("POST", "/auth/login", url.Values{"username": {"bob"}, "password": {"correct horse"}})
	if n := len(r.header.Values("Set-Cookie")); r.status != http.StatusSeeOther || n != 1 || !strings.HasPrefix(fresh.cookie, "s-") {
		t.Errorf("first-contact login: %d with %d Set-Cookie", r.status, n)
	}
	r = b.do("PUT", "/doc.html", url.Values{})
	if r.status != http.StatusForbidden || !strings.Contains(r.body, `<p><a href="/auth/logout">Log out</a></p>`) {
		t.Errorf("403: %d %s", r.status, r.body)
	}
	// Session expiry makes the request anonymous with a fresh session id.
	f.site.Store.DB().Exec(`UPDATE sessions SET expires_ms = 1 WHERE id = ?`, b.cookie)
	expired := b.cookie
	if who := b.whoami(); who["authenticated"] != false || b.cookie == expired {
		t.Errorf("expired session: %v (cookie reissued: %v)", who, b.cookie != expired)
	}
}

func TestDevAuthBypassIsLoopbackOnly(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8787", ":8787", "192.168.1.5:80", "[::]:80"} {
		if server.CheckListen(addr, true) == nil {
			t.Errorf("--dev-insecure-auth accepted on %s", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:8787", "[::1]:8787", "localhost:8787"} {
		if err := server.CheckListen(addr, true); err != nil {
			t.Errorf("loopback %s refused: %v", addr, err)
		}
	}
	dir := t.TempDir()
	reg, _ := site.NewRegistry(dir)
	ctl, _ := control.Open(dir)
	defer reg.Close()
	defer ctl.Close()
	s, _ := reg.Create(context.Background(), "t", site.Settings{})
	s.Users.Upsert(context.Background(), identity.User{Sub: "bob"}, "")
	for _, dev := range []bool{false, true} {
		ts := httptest.NewServer(server.New(server.Config{Domain: "localhost", DevAuth: dev}, reg, ctl, slog.New(slog.NewTextHandler(io.Discard, nil))))
		b := &browser{f: &fixture{t: t, ts: ts, site: s}}
		if got := decode(t, b.do("GET", "/-pagelike/whoami", nil, "X-Pagelike-Dev-User", "bob").body)["authenticated"]; got != dev {
			t.Errorf("dev=%v: impersonation authenticated=%v", dev, got)
		}
		if got := decode(t, b.do("GET", "/-pagelike/whoami", nil, "X-Pagelike-Dev-User", "bob", "X-Forwarded-For", "203.0.113.9").body)["authenticated"]; got != false {
			t.Errorf("dev=%v: proxied impersonation accepted", dev)
		}
		ts.Close()
	}
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("%v: %s", err, s)
	}
	return out
}
