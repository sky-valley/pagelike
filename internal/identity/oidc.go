package identity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OpenID Connect relying party (spec R-PERM-66..70): authorization-code
// flow with PKCE (S256), state and nonce bound to the visitor's session,
// ID-token validation (signature via the provider's JWKS, iss, aud, exp,
// nonce), optional userinfo merge, and claims mapped to a Principal.

// RPConfig is one site's relying-party configuration, mirroring PageLove's
// per-host OpenID fields.
type RPConfig struct {
	// Issuer is the provider's issuer URL or its openid-configuration URL
	// (".../.well-known/openid-configuration").
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string // default: openid email profile
	RolesClaim   string   // claim holding roles (dotted path); default "roles"
	RedirectURL  string   // absolute URL of the site's callback path
}

// DefaultScopes are requested when a site configures none.
var DefaultScopes = []string{oidc.ScopeOpenID, "email", "profile"}

// LoginStateTTL bounds the time between starting and finishing a login.
const LoginStateTTL = 10 * time.Minute

// ConfigError reports a relying-party misconfiguration or an unreachable
// provider; the login endpoint answers 503. Msg is safe to show visitors;
// Err holds diagnostic detail for the operator's log.
type ConfigError struct {
	Msg string
	Err error
}

func (e *ConfigError) Error() string {
	if e.Err != nil {
		return "identity provider unavailable: " + e.Msg + ": " + e.Err.Error()
	}
	return "identity provider unavailable: " + e.Msg
}

func (e *ConfigError) Unwrap() error { return e.Err }

// CallbackError reports a login that cannot complete (bad or replayed state,
// provider error, invalid token); the callback answers 400 and the session
// is left unchanged. Msg is safe to show visitors; Err is for the log.
type CallbackError struct {
	Msg string
	Err error
}

func (e *CallbackError) Error() string {
	if e.Err != nil {
		return "sign-in failed: " + e.Msg + ": " + e.Err.Error()
	}
	return "sign-in failed: " + e.Msg
}

func (e *CallbackError) Unwrap() error { return e.Err }

// RP is a relying party serving any number of sites. It caches provider
// discovery documents (and their key sets) per issuer.
type RP struct {
	// HTTPClient talks to providers (default: 10 s timeout).
	HTTPClient *http.Client
	// DiscoveryTTL is how long a discovery document is reused (default 1 h).
	DiscoveryTTL time.Duration

	mu        sync.Mutex
	providers map[string]cachedProvider
}

type cachedProvider struct {
	p       *oidc.Provider
	fetched time.Time
}

var defaultHTTP = &http.Client{Timeout: 10 * time.Second}

func (rp *RP) client() *http.Client {
	if rp.HTTPClient != nil {
		return rp.HTTPClient
	}
	return defaultHTTP
}

// issuerURL derives the issuer from a configured issuer or discovery URL.
func issuerURL(s string) string {
	return strings.TrimSuffix(strings.TrimSpace(s), "/.well-known/openid-configuration")
}

func (rp *RP) provider(ctx context.Context, issuer string) (*oidc.Provider, error) {
	ttl := rp.DiscoveryTTL
	if ttl == 0 {
		ttl = time.Hour
	}
	rp.mu.Lock()
	c, ok := rp.providers[issuer]
	rp.mu.Unlock()
	if ok && time.Since(c.fetched) < ttl {
		return c.p, nil
	}
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, rp.client()), issuer)
	if err != nil {
		return nil, &ConfigError{Msg: "the provider's openid-configuration could not be loaded", Err: err}
	}
	rp.mu.Lock()
	if rp.providers == nil {
		rp.providers = map[string]cachedProvider{}
	}
	rp.providers[issuer] = cachedProvider{p: p, fetched: time.Now()}
	rp.mu.Unlock()
	return p, nil
}

func (cfg *RPConfig) check() error {
	switch {
	case issuerURL(cfg.Issuer) == "":
		return &ConfigError{Msg: "no openid-configuration URL is configured"}
	case cfg.ClientID == "":
		return &ConfigError{Msg: "no client id is configured"}
	case cfg.RedirectURL == "":
		return &ConfigError{Msg: "no callback URL"}
	}
	return nil
}

func (cfg *RPConfig) oauth(p *oidc.Provider) *oauth2.Config {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}
	hasOpenID := false
	for _, s := range scopes {
		hasOpenID = hasOpenID || s == oidc.ScopeOpenID
	}
	if !hasOpenID {
		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}
	return &oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, Endpoint: p.Endpoint(),
		RedirectURL: cfg.RedirectURL, Scopes: scopes}
}

// Begin starts a login for the visitor's session and returns the provider
// authorization URL to redirect to. redirect is the same-site path to
// return to afterwards.
func (rp *RP) Begin(ctx context.Context, u *Users, cfg RPConfig, session, redirect string) (string, error) {
	if err := cfg.check(); err != nil {
		return "", err
	}
	if err := u.schema(); err != nil {
		return "", err
	}
	p, err := rp.provider(ctx, issuerURL(cfg.Issuer))
	if err != nil {
		return "", err
	}
	state, nonce, verifier := randToken(), randToken(), oauth2.GenerateVerifier()
	now := time.Now()
	u.DB.ExecContext(ctx, `DELETE FROM oidc_states WHERE expires_ms < ?`, now.UnixMilli()) // abandoned logins
	if _, err := u.DB.ExecContext(ctx, `INSERT INTO oidc_states(state, session_id, nonce, verifier, redirect, expires_ms) VALUES (?,?,?,?,?,?)`,
		state, session, nonce, verifier, redirect, now.Add(LoginStateTTL).UnixMilli()); err != nil {
		return "", err
	}
	return cfg.oauth(p).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

// Complete finishes a login from the callback query. It returns the
// authenticated identity and the path to return to. The login state is
// single-use and must belong to the same session that started the login.
func (rp *RP) Complete(ctx context.Context, u *Users, cfg RPConfig, session string, q url.Values) (*Identity, string, error) {
	if err := cfg.check(); err != nil {
		return nil, "", err
	}
	if e := q.Get("error"); e != "" {
		msg := "the identity provider returned " + e
		if d := q.Get("error_description"); d != "" {
			msg += ": " + d
		}
		// The state is spent either way.
		u.takeState(ctx, q.Get("state"))
		return nil, "", &CallbackError{Msg: msg}
	}
	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		return nil, "", &CallbackError{Msg: "the callback carries no code or state"}
	}
	ls, err := u.takeState(ctx, state)
	if err != nil {
		return nil, "", err
	}
	if ls.session != session {
		return nil, "", &CallbackError{Msg: "the login was started in another session"}
	}
	p, err := rp.provider(ctx, issuerURL(cfg.Issuer))
	if err != nil {
		return nil, "", err
	}
	hctx := oidc.ClientContext(ctx, rp.client())
	tok, err := cfg.oauth(p).Exchange(hctx, code, oauth2.VerifierOption(ls.verifier))
	if err != nil {
		return nil, "", &CallbackError{Msg: "the provider did not accept the authorization code", Err: err}
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return nil, "", &CallbackError{Msg: "the token response has no id_token"}
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: cfg.ClientID}).Verify(hctx, raw)
	if err != nil {
		return nil, "", &CallbackError{Msg: "the provider's ID token is not valid", Err: err}
	}
	if idt.Nonce != ls.nonce {
		return nil, "", &CallbackError{Msg: "ID token nonce mismatch"}
	}
	claims := map[string]any{}
	if err := idt.Claims(&claims); err != nil {
		return nil, "", &CallbackError{Msg: "unreadable ID token claims"}
	}
	if p.UserInfoEndpoint() != "" {
		if ui, err := p.UserInfo(hctx, oauth2.StaticTokenSource(tok)); err == nil && ui.Subject == idt.Subject {
			extra := map[string]any{}
			if ui.Claims(&extra) == nil {
				for k, v := range extra {
					if _, ok := claims[k]; !ok {
						claims[k] = v
					}
				}
			}
		}
	}
	for _, k := range []string{"nonce", "at_hash", "c_hash"} {
		delete(claims, k) // protocol artifacts, not identity
	}
	id, err := IdentityFromClaims(claims, cfg.RolesClaim)
	if err != nil {
		return nil, "", err
	}
	id.Issuer = idt.Issuer
	if sub, ok := u.linkedSub(ctx, idt.Issuer, id.Sub); ok {
		id.IdPSub, id.Sub = id.Sub, sub
	}
	return id, ls.redirect, nil
}

type loginState struct {
	session, nonce, verifier, redirect string
}

// takeState consumes a login state.
func (u *Users) takeState(ctx context.Context, state string) (*loginState, error) {
	if state == "" {
		return nil, &CallbackError{Msg: "unknown login state"}
	}
	if err := u.schema(); err != nil {
		return nil, err
	}
	var ls loginState
	var exp int64
	err := u.DB.QueryRowContext(ctx, `DELETE FROM oidc_states WHERE state = ? RETURNING session_id, nonce, verifier, redirect, expires_ms`, state).
		Scan(&ls.session, &ls.nonce, &ls.verifier, &ls.redirect, &exp)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, &CallbackError{Msg: "unknown or already used login state"}
	case err != nil:
		return nil, err
	case exp < time.Now().UnixMilli():
		return nil, &CallbackError{Msg: "the login took too long; start again"}
	}
	return &ls, nil
}

// IdentityFromClaims maps identity claims to an Identity (R-PERM-70): sub,
// email, email_verified (boolean true or the string "true"), name, picture,
// and roles from rolesClaim (default "roles"; a dotted path reaches nested
// claims such as Keycloak's "realm_access.roles"; an array of strings, or
// one string split on whitespace). Nothing is synthesized for missing
// claims.
func IdentityFromClaims(c map[string]any, rolesClaim string) (*Identity, error) {
	sub, _ := c["sub"].(string)
	if sub == "" {
		return nil, &CallbackError{Msg: "the identity has no sub claim"}
	}
	id := &Identity{Sub: sub, Claims: c}
	id.Email, _ = c["email"].(string)
	switch v := c["email_verified"].(type) {
	case bool:
		id.EmailVerified = v
	case string:
		id.EmailVerified = v == "true"
	}
	id.Name, _ = c["name"].(string)
	id.Picture, _ = c["picture"].(string)
	if rolesClaim == "" {
		rolesClaim = "roles"
	}
	var cur any = c
	for _, seg := range strings.Split(rolesClaim, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			cur = nil
			break
		}
		cur = m[seg]
	}
	switch v := cur.(type) {
	case []any:
		for _, r := range v {
			if s, ok := r.(string); ok && s != "" {
				id.Roles = append(id.Roles, s)
			}
		}
	case []string:
		id.Roles = append(id.Roles, v...)
	case string:
		id.Roles = strings.Fields(v)
	}
	return id, nil
}

func randToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("identity: no randomness: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
