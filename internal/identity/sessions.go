package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Sessions (spec R-PERM-64, R-PERM-65, R-PERM-68, R-PERM-69).
//
// Every visitor has a session, identified by an HttpOnly cookie. Anonymous
// sessions are not stored: their id is a random token that keys transient
// content and SSE echo suppression. Signing in creates a stored session
// under a fresh id (the "s-" prefix marks signed-in ids), moving the
// visitor's transient content to it; the old id stops working. Signing out
// deletes the stored session and its transient content and issues a fresh
// anonymous id. A signed-in id that is expired or no longer stored makes the
// request anonymous, and the caller issues a new anonymous id.

// SessionTTL is the default lifetime of a signed-in session.
const SessionTTL = 30 * 24 * time.Hour

// AnonymousCookieTTL is the cookie lifetime of anonymous sessions.
const AnonymousCookieTTL = 365 * 24 * time.Hour

// signedInPrefix marks ids of stored (signed-in) sessions.
const signedInPrefix = "s-"

// NewSessionID returns a random anonymous session identifier.
func NewSessionID() string { return randHex(18) }

func newSignedInID() string { return signedInPrefix + randHex(32) }

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ValidSessionID reports whether id is shaped like an id pagelike issues.
func ValidSessionID(id string) bool {
	if len(id) < 16 || len(id) > 128 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// SessionState classifies a presented session id.
type SessionState int

const (
	// SessionMissing: no id, or one pagelike never issues. Issue a new one.
	SessionMissing SessionState = iota
	// SessionAnonymous: a valid anonymous id.
	SessionAnonymous
	// SessionActive: a stored, unexpired signed-in session.
	SessionActive
	// SessionExpired: a stored session past its expiry. Issue a new id.
	SessionExpired
	// SessionInvalidated: a signed-in id that is no longer stored (signed
	// out, account deleted, revoked). Issue a new id.
	SessionInvalidated
)

// NeedsNewID reports whether the caller must issue a fresh anonymous id.
func (s SessionState) NeedsNewID() bool {
	return s == SessionMissing || s == SessionExpired || s == SessionInvalidated
}

func (s SessionState) String() string {
	return [...]string{"missing", "anonymous", "active", "expired", "invalidated"}[s]
}

// Identity is an authenticated end user as an identity source (a local
// account or an OpenID provider) asserted it at sign-in.
type Identity struct {
	Sub           string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
	Roles         []string
	Claims        map[string]any
	Issuer        string // OpenID issuer; "" for local accounts
	IdPSub        string // the provider's subject when Sub was mapped through a Link
}

// storedSession is the JSON kept in sessions.claims. Local sessions store
// only the version: their attributes are read from the account on every
// request, so account changes (roles, verification) apply at once.
type storedSession struct {
	V             int            `json:"v"`
	Issuer        string         `json:"iss,omitempty"`
	IdPSub        string         `json:"idp_sub,omitempty"`
	Email         string         `json:"email,omitempty"`
	EmailVerified bool           `json:"email_verified,omitempty"`
	Name          string         `json:"name,omitempty"`
	Picture       string         `json:"picture,omitempty"`
	Roles         []string       `json:"roles,omitempty"`
	Claims        map[string]any `json:"claims,omitempty"`
}

// Login binds id to a new signed-in session that replaces the visitor's
// current session prev (which may be ""): transient content moves to the new
// session id and prev stops working. It returns the new session id.
func (u *Users) Login(ctx context.Context, prev string, id *Identity, ttl time.Duration) (string, error) {
	if err := u.schema(); err != nil {
		return "", err
	}
	if ttl <= 0 {
		ttl = SessionTTL
	}
	st := storedSession{V: 1}
	if id.Issuer != "" {
		st = storedSession{V: 1, Issuer: id.Issuer, IdPSub: id.IdPSub, Email: id.Email, EmailVerified: id.EmailVerified,
			Name: id.Name, Picture: id.Picture, Roles: id.Roles, Claims: id.Claims}
	}
	blob, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	sid := newSignedInID()
	now := time.Now()
	tx, err := u.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(id, sub, claims, created_ms, expires_ms) VALUES (?,?,?,?,?)`,
		sid, id.Sub, string(blob), now.UnixMilli(), now.Add(ttl).UnixMilli()); err != nil {
		return "", err
	}
	if prev != "" && prev != sid {
		if _, err := tx.ExecContext(ctx, `UPDATE transients SET session_id = ? WHERE session_id = ?`, sid, prev); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, prev); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	u.Prune(ctx)
	return sid, nil
}

// Logout ends a session: the stored session and its transient content are
// deleted.
func (u *Users) Logout(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	tx, err := u.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM transients WHERE session_id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// EndSession deletes a stored session (kept for callers that only revoke).
func (u *Users) EndSession(ctx context.Context, id string) error {
	_, err := u.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// sessionGrace keeps expired rows for a while so live streams can still
// report "session-expired" rather than "session-invalidated".
const sessionGrace = 24 * time.Hour

// Prune deletes long-expired sessions and stale login states.
func (u *Users) Prune(ctx context.Context) {
	now := time.Now().UnixMilli()
	u.DB.ExecContext(ctx, `DELETE FROM sessions WHERE expires_ms < ?`, now-sessionGrace.Milliseconds())
	if u.schema() == nil {
		u.DB.ExecContext(ctx, `DELETE FROM oidc_states WHERE expires_ms < ?`, now)
	}
}

// Resolve maps a session id to a principal; anything but an active signed-in
// session yields an anonymous principal bound to that id.
func (u *Users) Resolve(ctx context.Context, sessionID string) *Principal {
	p, _ := u.ResolveSession(ctx, sessionID)
	return p
}

// Status classifies a session id without building a principal (for live
// streams that watch their subscriber's session).
func (u *Users) Status(ctx context.Context, sessionID string) SessionState {
	_, st := u.ResolveSession(ctx, sessionID)
	return st
}

// ResolveSession maps a session id to a principal and classifies it.
//
// Local sessions take their attributes from the account. OpenID sessions
// use the claims asserted at sign-in, plus the roles of a local account with
// the same sub, if one exists (so administrators can grant roles to provider
// identities).
func (u *Users) ResolveSession(ctx context.Context, sessionID string) (*Principal, SessionState) {
	p := Anonymous(sessionID)
	if !ValidSessionID(sessionID) {
		return p, SessionMissing
	}
	var sub, blob string
	var exp int64
	err := u.DB.QueryRowContext(ctx, `SELECT sub, claims, expires_ms FROM sessions WHERE id = ?`, sessionID).Scan(&sub, &blob, &exp)
	switch {
	case err != nil && strings.HasPrefix(sessionID, signedInPrefix):
		return p, SessionInvalidated
	case err != nil || sub == "":
		return p, SessionAnonymous
	case exp < time.Now().UnixMilli():
		return p, SessionExpired
	}
	var st storedSession
	if blob != "" {
		json.Unmarshal([]byte(blob), &st)
	}
	usr, uerr := u.Get(ctx, sub)
	p.Authenticated, p.Sub, p.Username = true, sub, sub
	if st.Issuer == "" {
		if uerr != nil {
			return Anonymous(sessionID), SessionInvalidated // account deleted
		}
		p.Email, p.EmailVerified, p.Name, p.Roles = usr.Email, usr.EmailVerified, usr.Name, usr.Roles
		p.Claims = usr.claims()
		return p, SessionActive
	}
	p.Issuer, p.Email, p.EmailVerified, p.Name, p.Picture = st.Issuer, st.Email, st.EmailVerified, st.Name, st.Picture
	p.Roles = append([]string(nil), st.Roles...)
	p.Claims = st.Claims
	if uerr == nil {
		for _, r := range usr.Roles {
			if !p.HasRole(r) {
				p.Roles = append(p.Roles, r)
			}
		}
	}
	return p, SessionActive
}

// LocalPrincipal builds the principal of a local account without a stored
// session (the loopback-only development impersonation header).
func (u *Users) LocalPrincipal(ctx context.Context, sub, session string) (*Principal, error) {
	usr, err := u.Get(ctx, sub)
	if err != nil {
		return nil, err
	}
	return &Principal{Authenticated: true, Sub: usr.Sub, Username: usr.Sub, Email: usr.Email, EmailVerified: usr.EmailVerified,
		Name: usr.Name, Roles: usr.Roles, Claims: usr.claims(), Session: session}, nil
}

// LocalIdentity is the sign-in identity of a local account.
func (usr *User) LocalIdentity() *Identity {
	return &Identity{Sub: usr.Sub, Email: usr.Email, EmailVerified: usr.EmailVerified, Name: usr.Name, Roles: usr.Roles, Claims: usr.claims()}
}

// claims presents a local account as the claims an identity provider would
// assert for it (request.auth.claims.*).
func (usr *User) claims() map[string]any {
	c := map[string]any{"sub": usr.Sub, "email_verified": usr.EmailVerified}
	if usr.Email != "" {
		c["email"] = usr.Email
	}
	if usr.Name != "" {
		c["name"] = usr.Name
	}
	if len(usr.Roles) > 0 {
		rs := make([]any, len(usr.Roles))
		for i, r := range usr.Roles {
			rs[i] = r
		}
		c["roles"] = rs
	}
	return c
}

// Session cookie names. On secure origins the __Host- prefix pins the
// cookie to the exact host (no Domain, Path=/, Secure).
const (
	CookieSecure   = "__Host-session"
	CookieInsecure = "pagelike_session"
)

// CookiePolicy says how a site writes its session cookie.
type CookiePolicy struct {
	// Name overrides the cookie name (default CookieSecure on secure
	// origins, CookieInsecure otherwise).
	Name string
	// Secure marks the cookie Secure (TLS origins and localhost).
	Secure bool
	// Partitioned selects SameSite=None; Partitioned (for sites embedded in
	// other sites' frames) instead of the default SameSite=Lax.
	Partitioned bool
}

func (c CookiePolicy) name() string {
	switch {
	case c.Name != "":
		return c.Name
	case c.Secure:
		return CookieSecure
	}
	return CookieInsecure
}

// SessionCookie reads the session id from a request, trying the policy's
// cookie name and then the default names.
func (c CookiePolicy) SessionCookie(r *http.Request) string {
	for _, name := range []string{c.Name, CookieSecure, CookieInsecure} {
		if name == "" {
			continue
		}
		if ck, err := r.Cookie(name); err == nil && ck.Value != "" && len(ck.Value) <= 128 {
			return ck.Value
		}
	}
	return ""
}

// Write sets the session cookie (HttpOnly, Path=/, no Domain), replacing a
// session cookie already set on this response (a visitor's first request
// that also signs in or out).
func (c CookiePolicy) Write(w http.ResponseWriter, id string, maxAge time.Duration) {
	h := w.Header()
	if prev := h["Set-Cookie"]; len(prev) > 0 {
		kept := make([]string, 0, len(prev))
		for _, v := range prev {
			if !strings.HasPrefix(v, c.name()+"=") {
				kept = append(kept, v)
			}
		}
		h["Set-Cookie"] = kept
	}
	ck := &http.Cookie{Name: c.name(), Value: id, Path: "/", HttpOnly: true, MaxAge: int(maxAge.Seconds()), SameSite: http.SameSiteLaxMode}
	if c.Secure {
		ck.Secure = true
		if c.Partitioned {
			ck.SameSite, ck.Partitioned = http.SameSiteNoneMode, true
		}
	}
	http.SetCookie(w, ck)
}

// SessionCookie reads the session id from a request using the default
// cookie names.
func SessionCookie(r *http.Request) string { return CookiePolicy{}.SessionCookie(r) }

// IsTLS reports whether the client reached us over HTTPS (directly or via a
// trusted proxy that sets X-Forwarded-Proto).
func IsTLS(r *http.Request, trustProxy bool) bool {
	if r.TLS != nil {
		return true
	}
	return trustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
