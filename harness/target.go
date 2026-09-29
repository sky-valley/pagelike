package harness

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sky-valley/pagelike/internal/control"
	_ "github.com/sky-valley/pagelike/internal/features"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/identity/oidctest"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

// Env is a prepared environment for one case run.
type Env struct {
	Prefix     string
	PublicURL  string // scheme://host[:port] clients connect to
	PublicHost string // Host header value for the public plane
	DavURL     string
	DavHost    string
	AuthorAuth string // Authorization header for the authoring plane
	Vars       map[string]string
	Actors     map[string]*Client
	HTTP       *http.Client
	Restart    func() error
	Limiter    *Limiter
	Origin     string // scheme://host of the public plane (for absolute URLs)
	// SessionControl expires or invalidates an actor's session (local only).
	SessionControl func(c *Client, action string) error
	Caps           map[string]bool
	// StallStream, when set, makes the server's writes to the client
	// connection with the given local address block (or flow again), as
	// for a reader whose socket no longer drains (see stall.go).
	StallStream func(localAddr string, paused bool)
	// Sink captures outbound requests (${SINK}; local targets only).
	Sink *Sink
	// PruneEvents drops every stored stream event of the case's site
	// (local only), standing in for the retention window passing.
	PruneEvents func() error
}

// Target prepares environments.
type Target interface {
	Name() string
	Caps() map[string]bool
	Setup(ctx context.Context, c *Case) (*Env, error)
	Teardown(ctx context.Context, env *Env)
}

// Client is an actor's HTTP identity: a cookie store plus fixed headers.
type Client struct {
	Name    string
	mu      sync.Mutex
	cookies map[string]string
	Header  http.Header
}

func newClient(name string) *Client {
	return &Client{Name: name, cookies: map[string]string{}, Header: http.Header{}}
}

func (c *Client) apply(req *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]string, 0, len(c.cookies))
	for k := range c.cookies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		req.AddCookie(&http.Cookie{Name: k, Value: c.cookies[k]})
	}
	for k, v := range c.Header {
		req.Header[k] = v
	}
}

func (c *Client) absorb(resp *http.Response) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ck := range resp.Cookies() {
		if ck.MaxAge < 0 || (ck.Value == "" && ck.MaxAge == 0 && !ck.Expires.IsZero() && ck.Expires.Before(time.Now())) {
			delete(c.cookies, ck.Name)
			continue
		}
		c.cookies[ck.Name] = ck.Value
	}
}

// Limiter bounds request rate (live targets).
type Limiter struct {
	mu   sync.Mutex
	next time.Time
	gap  time.Duration
}

// Wait blocks until the next request may be sent.
func (l *Limiter) Wait() {
	if l == nil {
		return
	}
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	wait := l.next.Sub(now)
	l.next = l.next.Add(l.gap)
	l.mu.Unlock()
	time.Sleep(wait)
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------- local

// LocalTarget runs an in-process pagelike per case.
type LocalTarget struct {
	Quiet bool
}

func (t *LocalTarget) Name() string { return "local" }

// Caps lists what the local target provides.
func (t *LocalTarget) Caps() map[string]bool {
	return map[string]bool{"multi-actor": true, "webdav": true, "outbound-http": true, "restart": true,
		"server-js": true, "sessel": true, "liquid": true, "slow": true, "sse-control": true, "session-control": true}
}

type localState struct {
	dir    string
	reg    *site.Registry
	ctl    *control.DB
	srv    *httptest.Server
	hand   *swapHandler
	stalls *stalls
	idp    *oidctest.Provider // site.settings.oidc: stub
	sink   *Sink              // ${SINK} capture endpoint
}

type swapHandler struct {
	mu sync.RWMutex
	h  http.Handler
}

func (s *swapHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	h := s.h
	s.mu.RUnlock()
	h.ServeHTTP(w, r)
}

// Setup creates a fresh site "t" with the case's files, rules and actors.
func (t *LocalTarget) Setup(ctx context.Context, c *Case) (*Env, error) {
	dir, err := os.MkdirTemp("", "pagelike-case-*")
	if err != nil {
		return nil, err
	}
	st := &localState{dir: dir, hand: &swapHandler{}, stalls: newStalls()}
	if err := st.open(t.Quiet); err != nil {
		return nil, err
	}
	settings := site.Settings{DefaultGet: "allow"}
	if v, ok := c.Site.Settings["default_get"].(string); ok {
		settings.DefaultGet = v
	}
	// The body cap is applied after the case's files are uploaded: setup
	// is not part of what the case tests, and a small cap would refuse it.
	bodyCap, hasCap := c.Site.Settings["max_request_body_bytes"].(int)
	if v, ok := c.Site.Settings["sse_buffer_events"].(int); ok {
		settings.SSEBufferEvents = v
	}
	// Outbound requests may reach the case's capture sink on loopback and
	// nothing else private (docs/spec/reacting.md R-REACT-61, §14).
	st.sink = NewSink()
	settings.Outbound = &site.OutboundSettings{Allow: []string{st.sink.HostPort()}}
	if v, _ := c.Site.Settings["oidc"].(string); v == "stub" {
		// An in-process OpenID provider stands in for the host's IdP.
		st.idp = oidctest.New()
		settings.OIDC = &site.OIDCConfig{Issuer: st.idp.URL, ClientID: st.idp.ClientID, ClientSecret: st.idp.ClientSecret}
	}
	if _, err := st.reg.Create(ctx, "t", settings); err != nil {
		return nil, err
	}
	secret, _, err := st.ctl.CreateKey(ctx, "harness", []string{"t"}, time.Hour)
	if err != nil {
		return nil, err
	}
	st.srv = httptest.NewUnstartedServer(st.hand)
	st.srv.Listener = stallListener{Listener: st.srv.Listener, stalls: st.stalls}
	st.srv.Start()
	env := &Env{
		Prefix: "/_pl/" + sanitize(c.ID) + "-" + randHex(3), PublicURL: st.srv.URL, PublicHost: "t.localhost",
		DavURL: st.srv.URL, DavHost: "dav-t.localhost", AuthorAuth: "Bearer " + secret,
		Vars: map[string]string{}, Actors: map[string]*Client{}, HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: noRedirect},
		Caps: t.Caps(),
	}
	if c.Root {
		env.Prefix = ""
	}
	env.StallStream = st.stalls.set
	env.Sink = st.sink
	env.Vars["SINK"] = st.sink.URL()
	env.Restart = func() error { return st.restart(t.Quiet) }
	env.PruneEvents = func() error {
		sp, err := st.reg.Get(context.Background(), "t")
		if err != nil {
			return err
		}
		_, err = sp.Store.PruneEvents(context.Background(), time.Now().Add(time.Hour))
		return err
	}
	env.Origin = "http://t.localhost"
	env.SessionControl = func(cl *Client, action string) error {
		sp, err := st.reg.Get(context.Background(), "t")
		if err != nil {
			return err
		}
		cl.mu.Lock()
		sid := cl.cookies[identity.CookieSecure]
		if sid == "" {
			sid = cl.cookies[identity.CookieInsecure]
		}
		cl.mu.Unlock()
		switch action {
		case "expire":
			_, err = sp.Store.DB().Exec(`UPDATE sessions SET expires_ms = 1 WHERE id = ?`, sid)
		case "invalidate":
			_, err = sp.Store.DB().Exec(`DELETE FROM sessions WHERE id = ?`, sid)
		default:
			err = fmt.Errorf("unknown action %q", action)
		}
		return err
	}
	env.Vars["__local_dir"] = dir
	env.Vars["__cleanup"] = "local"
	localStates.Store(env, st)
	// Actors: local accounts with generated passwords, signed in for real.
	sitep, _ := st.reg.Get(ctx, "t")
	for name, a := range c.Actors {
		sub := a.Sub
		if sub == "" {
			sub = name
		}
		pw := randHex(12)
		if err := sitep.Users.Upsert(ctx, identity.User{Sub: sub, Email: a.Email, EmailVerified: a.EmailVerified, Name: a.Name, Roles: a.Roles}, pw); err != nil {
			return nil, err
		}
		cl := newClient(name)
		if err := login(env, cl, sub, pw); err != nil {
			return nil, fmt.Errorf("login %s: %w", name, err)
		}
		env.Actors[name] = cl
	}
	if err := uploadSetup(ctx, env, c); err != nil {
		return nil, err
	}
	if hasCap {
		if err := sitep.UpdateSettings(ctx, func(s *site.Settings) { s.MaxBodyBytes = int64(bodyCap) }); err != nil {
			return nil, err
		}
	}
	return env, nil
}

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func (st *localState) open(quiet bool) error {
	reg, err := site.NewRegistry(st.dir)
	if err != nil {
		return err
	}
	ctl, err := control.Open(st.dir)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if !quiet {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	}
	srv := server.New(server.Config{Domain: "localhost"}, reg, ctl, log)
	st.reg, st.ctl = reg, ctl
	st.hand.mu.Lock()
	st.hand.h = srv
	st.hand.mu.Unlock()
	return nil
}

// restart closes every open database and reopens from disk, simulating a
// process restart (in-memory state such as SSE subscribers is lost).
func (st *localState) restart(quiet bool) error {
	st.reg.Close()
	st.ctl.Close()
	return st.open(quiet)
}

var localStates sync.Map // *Env → *localState

// Teardown removes the case's data.
func (t *LocalTarget) Teardown(ctx context.Context, env *Env) {
	if v, ok := localStates.LoadAndDelete(env); ok {
		st := v.(*localState)
		if st.srv != nil {
			st.srv.CloseClientConnections()
			st.srv.Close()
		}
		if st.idp != nil {
			st.idp.Close()
		}
		if st.sink != nil {
			st.sink.Close()
		}
		st.reg.Close()
		st.ctl.Close()
		os.RemoveAll(st.dir)
	}
}

func login(env *Env, cl *Client, user, pw string) error {
	form := url.Values{"username": {user}, "password": {pw}}
	req, _ := http.NewRequest(http.MethodPost, env.PublicURL+"/auth/login", strings.NewReader(form.Encode()))
	req.Host = env.PublicHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cl.apply(req)
	resp, err := env.HTTP.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	cl.absorb(resp)
	if resp.StatusCode != http.StatusSeeOther {
		return fmt.Errorf("login returned %d", resp.StatusCode)
	}
	return nil
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if len(out) > 40 {
		out = out[:40]
	}
	return strings.Trim(out, "-")
}

// uploadSetup writes the case's files and rules over the authoring plane.
func uploadSetup(ctx context.Context, env *Env, c *Case) error {
	files := append([]File(nil), c.Site.Files...)
	if len(c.Site.Rules) > 0 {
		files = append(files, File{Path: "${P}/_rules.html", Body: rulesDoc(c.Site.Rules)})
	}
	made := map[string]bool{}
	for _, f := range files {
		p := Expand(f.Path, env)
		body := Expand(f.Body, env)
		if f.BodyFile != "" {
			b, err := os.ReadFile(path.Join(path.Dir(c.File), f.BodyFile))
			if err != nil {
				return err
			}
			body = string(b)
		}
		// Create parent collections (needed by strict WebDAV servers).
		dir := path.Dir(p)
		var chain []string
		for d := dir; d != "/" && d != "." && !made[d]; d = path.Dir(d) {
			chain = append([]string{d}, chain...)
		}
		for _, d := range chain {
			status, _, err := davDo(env, "MKCOL", d+"/", nil, "")
			if err != nil {
				return err
			}
			// An existing collection is 405 on PageLove, as in RFC 4918
			// (live 2026-09-29); older builds answered 409.
			if status >= 400 && status != http.StatusMethodNotAllowed && status != http.StatusConflict {
				return fmt.Errorf("MKCOL %s: %d", d, status)
			}
			made[d] = true
		}
		status, respBody, err := davDo(env, http.MethodPut, p, []byte(body), f.ContentType)
		if err != nil {
			return err
		}
		if status >= 300 {
			return fmt.Errorf("setup PUT %s: %d %s", p, status, truncate(string(respBody), 300))
		}
	}
	return nil
}

func rulesDoc(rules []RuleSpec) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html><head><title>rules</title></head><body><table><tbody>\n")
	for _, r := range rules {
		b.WriteString(`<tr itemscope itemtype="https://pagelove.org/AuthorizationRule">`)
		b.WriteString("<td>")
		for _, a := range r.Actor {
			b.WriteString(`<span itemprop="actor">` + htmlEsc(a) + `</span>`)
		}
		b.WriteString("</td><td>")
		for _, x := range r.Resource {
			b.WriteString(`<span itemprop="resource">` + htmlEsc(x) + `</span>`)
		}
		b.WriteString("</td><td>")
		for _, m := range r.Method {
			b.WriteString(`<span itemprop="method">` + htmlEsc(m) + `</span>`)
		}
		b.WriteString(`</td><td itemprop="selector">` + htmlEsc(r.Selector) + `</td>`)
		b.WriteString(`<td itemprop="action">` + htmlEsc(r.Action) + "</td></tr>\n")
	}
	b.WriteString("</tbody></table></body></html>\n")
	return b.String()
}

func htmlEsc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

func davDo(env *Env, method, p string, body []byte, ct string) (int, []byte, error) {
	env.Limiter.Wait()
	req, err := http.NewRequest(method, strings.TrimSuffix(env.DavURL, "/")+escapePath(p), bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Host = env.DavHost
	req.Header.Set("Authorization", env.AuthorAuth)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	resp, err := env.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}

func escapePath(p string) string {
	u := url.URL{Path: p}
	return u.EscapedPath()
}

// ---------------------------------------------------------------- live

// LiveTarget runs cases against a disposable PageLove host.
type LiveTarget struct {
	PublicHost string // e.g. pagelike-harness-1234.onpagelove.com
	DavURL     string // e.g. https://dav-pagelike-harness-1234.onpagelove.com/
	APIKey     string
	Actors     map[string]string // actor name → Cookie header value
	RPS        float64
}

func (t *LiveTarget) Name() string { return "live" }

// Caps lists what the live target provides.
func (t *LiveTarget) Caps() map[string]bool {
	c := map[string]bool{"webdav": true, "server-js": true, "sessel": true, "liquid": true}
	if len(t.Actors) > 0 {
		c["multi-actor"] = true
	}
	return c
}

// Setup uploads the case's files under a unique prefix.
func (t *LiveTarget) Setup(ctx context.Context, c *Case) (*Env, error) {
	if c.Root && (len(c.Site.Files) > 0 || len(c.Site.Rules) > 0) {
		// Only read-only root probes run live (opted in with --root): a
		// root case with setup would write host-wide paths.
		return nil, fmt.Errorf("root cases with setup files are not run live")
	}
	gap := time.Duration(float64(time.Second) / max(t.RPS, 0.5))
	host := t.PublicHost
	dav := strings.TrimSuffix(t.DavURL, "/")
	du, err := url.Parse(dav)
	if err != nil {
		return nil, err
	}
	env := &Env{
		Prefix: "/_pl/" + sanitize(c.ID) + "-" + randHex(3), PublicURL: "https://" + host, PublicHost: host,
		DavURL: dav, DavHost: du.Host, AuthorAuth: "Bearer " + t.APIKey,
		Vars: map[string]string{}, Actors: map[string]*Client{}, HTTP: &http.Client{Timeout: 60 * time.Second, CheckRedirect: noRedirect,
			Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 15 * time.Second}).DialContext, ForceAttemptHTTP2: true}},
		Limiter: &Limiter{gap: gap}, Caps: t.Caps(), Origin: "https://" + host,
	}
	if c.Root {
		env.Prefix = ""
	}
	for name := range c.Actors {
		ck, ok := t.Actors[name]
		if !ok {
			return nil, fmt.Errorf("actor %q has no live session configured", name)
		}
		cl := newClient(name)
		cl.Header.Set("Cookie", ck)
		env.Actors[name] = cl
	}
	if err := uploadSetup(ctx, env, c); err != nil {
		t.Teardown(ctx, env)
		return nil, err
	}
	return env, nil
}

// Teardown deletes the case's prefix from the host.
func (t *LiveTarget) Teardown(ctx context.Context, env *Env) {
	if env == nil || !strings.HasPrefix(env.Prefix, "/_pl/") {
		return
	}
	davDo(env, http.MethodDelete, env.Prefix+"/", nil, "")
}
