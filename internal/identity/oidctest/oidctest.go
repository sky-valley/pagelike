// Package oidctest is an in-process OpenID Connect provider for tests and
// the local compatibility harness: discovery, an authorization endpoint that
// signs in a configured user without interaction, a token endpoint (client
// secret, PKCE S256, single-use codes), JWKS and userinfo. ID tokens are
// RS256 JWTs.
//
// It is NOT an identity provider: it authenticates nobody.
package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Provider is a running fake provider.
type Provider struct {
	URL          string // issuer
	ClientID     string
	ClientSecret string

	mu sync.Mutex
	// Claims are asserted for whoever signs in next (sub is required).
	claims map[string]any
	// UserinfoClaims are served by the userinfo endpoint in addition to
	// the ID-token claims (sub is added automatically).
	userinfo map[string]any
	// TamperNonce makes the next ID token carry a wrong nonce.
	tamperNonce bool
	codes       map[string]*grant
	tokens      map[string]map[string]any
	key         *rsa.PrivateKey
	srv         *httptest.Server
	// authorizations records every authorization request's query.
	authorizations []url.Values
}

type grant struct {
	clientID, redirectURI, nonce, challenge string
	claims                                  map[string]any
	expires                                 time.Time
}

// New starts a provider on a loopback listener.
func New() *Provider {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	p := &Provider{ClientID: "pagelike-test", ClientSecret: "test-secret-" + token()[:12], key: key,
		codes: map[string]*grant{}, tokens: map[string]map[string]any{},
		claims: map[string]any{"sub": "oidc-user", "email": "user@example.com", "email_verified": true, "name": "Test User"}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)
	mux.HandleFunc("GET /jwks", p.jwks)
	mux.HandleFunc("GET /userinfo", p.userinfoHandler)
	p.srv = httptest.NewServer(mux)
	p.URL = p.srv.URL
	return p
}

// Close stops the provider.
func (p *Provider) Close() { p.srv.Close() }

// SetUser sets the claims asserted for the next sign-in.
func (p *Provider) SetUser(claims map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.claims = claims
}

// SetUserinfo sets extra claims served only by the userinfo endpoint.
func (p *Provider) SetUserinfo(claims map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.userinfo = claims
}

// Authorizations returns the query of every authorization request so far.
func (p *Provider) Authorizations() []url.Values {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]url.Values(nil), p.authorizations...)
}

// TamperNextNonce makes the next ID token carry a wrong nonce.
func (p *Provider) TamperNextNonce() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tamperNonce = true
}

func (p *Provider) discovery(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                p.URL,
		"authorization_endpoint":                p.URL + "/authorize",
		"token_endpoint":                        p.URL + "/token",
		"jwks_uri":                              p.URL + "/jwks",
		"userinfo_endpoint":                     p.URL + "/userinfo",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
		"scopes_supported":                      []string{"openid", "email", "profile"},
	})
}

// authorize signs in the configured user and redirects back with a code.
func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p.mu.Lock()
	p.authorizations = append(p.authorizations, q)
	claims := copyMap(p.claims)
	p.mu.Unlock()
	switch {
	case q.Get("client_id") != p.ClientID:
		http.Error(w, "unknown client", http.StatusBadRequest)
		return
	case q.Get("response_type") != "code":
		http.Error(w, "unsupported response_type", http.StatusBadRequest)
		return
	case q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "":
		http.Error(w, "PKCE S256 required", http.StatusBadRequest)
		return
	case !strings.Contains(" "+q.Get("scope")+" ", " openid "):
		http.Error(w, "openid scope required", http.StatusBadRequest)
		return
	}
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || redirect.Scheme == "" {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	code := token()
	p.mu.Lock()
	p.codes[code] = &grant{clientID: p.ClientID, redirectURI: q.Get("redirect_uri"), nonce: q.Get("nonce"),
		challenge: q.Get("code_challenge"), claims: claims, expires: time.Now().Add(time.Minute)}
	p.mu.Unlock()
	back := redirect.Query()
	back.Set("code", code)
	back.Set("state", q.Get("state"))
	redirect.RawQuery = back.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, "invalid_request")
		return
	}
	id, secret, ok := r.BasicAuth()
	if ok {
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id, secret = r.PostFormValue("client_id"), r.PostFormValue("client_secret")
	}
	if id != p.ClientID || subtle.ConstantTimeCompare([]byte(secret), []byte(p.ClientSecret)) != 1 {
		w.Header().Set("WWW-Authenticate", `Basic realm="token"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	if r.PostFormValue("grant_type") != "authorization_code" {
		oauthError(w, "unsupported_grant_type")
		return
	}
	p.mu.Lock()
	g := p.codes[r.PostFormValue("code")]
	delete(p.codes, r.PostFormValue("code")) // single use
	tamper := p.tamperNonce
	p.tamperNonce = false
	p.mu.Unlock()
	if g == nil || time.Now().After(g.expires) || g.redirectURI != r.PostFormValue("redirect_uri") {
		oauthError(w, "invalid_grant")
		return
	}
	sum := sha256.Sum256([]byte(r.PostFormValue("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		oauthError(w, "invalid_grant")
		return
	}
	now := time.Now()
	claims := copyMap(g.claims)
	claims["iss"], claims["aud"], claims["iat"], claims["exp"] = p.URL, g.clientID, now.Unix(), now.Add(5*time.Minute).Unix()
	claims["nonce"] = g.nonce
	if tamper {
		claims["nonce"] = "tampered"
	}
	access := token()
	p.mu.Lock()
	p.tokens[access] = copyMap(g.claims)
	p.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 300, "id_token": p.sign(claims)})
}

func (p *Provider) userinfoHandler(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	p.mu.Lock()
	claims, ok := p.tokens[tok]
	extra := copyMap(p.userinfo)
	p.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	out := map[string]any{"sub": claims["sub"]}
	for k, v := range extra {
		out[k] = v
	}
	writeJSON(w, http.StatusOK, out)
}

func (p *Provider) jwks(w http.ResponseWriter, r *http.Request) {
	pub := p.key.PublicKey
	writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "k1",
		"n": b64(pub.N.Bytes()), "e": b64(big.NewInt(int64(pub.E)).Bytes()),
	}}})
}

// sign produces an RS256 JWT.
func (p *Provider) sign(claims map[string]any) string {
	hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "k1"})
	body, _ := json.Marshal(claims)
	input := b64(hdr) + "." + b64(body)
	sum := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, sum[:])
	if err != nil {
		panic(err)
	}
	return input + "." + b64(sig)
}

func oauthError(w http.ResponseWriter, code string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func token() string {
	b := make([]byte, 24)
	rand.Read(b)
	return b64(b)
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// String describes the provider (for test logs).
func (p *Provider) String() string {
	return fmt.Sprintf("oidctest provider at %s (client %s)", p.URL, p.ClientID)
}
