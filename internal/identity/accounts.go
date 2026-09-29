package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

// Users manages a site's end-user identity state in its site.db: local
// accounts (users), sessions, pending OIDC logins and identity links. The
// users, sessions and transients tables are created by the store schema;
// the identity-only tables are created on first use.
type Users struct {
	DB *sql.DB

	schemaOnce sync.Once
	schemaErr  error
}

const identitySchema = `
CREATE TABLE IF NOT EXISTS oidc_states (
	state      TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	nonce      TEXT NOT NULL,
	verifier   TEXT NOT NULL,
	redirect   TEXT NOT NULL,
	expires_ms INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS identity_links (
	issuer     TEXT NOT NULL,
	idp_sub    TEXT NOT NULL,
	sub        TEXT NOT NULL,
	created_ms INTEGER NOT NULL,
	PRIMARY KEY (issuer, idp_sub)
);
CREATE INDEX IF NOT EXISTS oidc_states_expires ON oidc_states(expires_ms);
CREATE INDEX IF NOT EXISTS sessions_sub ON sessions(sub);
`

func (u *Users) schema() error {
	u.schemaOnce.Do(func() {
		_, u.schemaErr = u.DB.Exec(identitySchema)
	})
	return u.schemaErr
}

// User is a local account. An account without a password cannot sign in
// with one; it can still carry roles for an OIDC principal with the same
// sub (see Resolve).
type User struct {
	Sub           string
	Email         string
	EmailVerified bool
	Name          string
	Roles         []string
	HasPassword   bool
}

// Password hashing is memory-hard (64 MiB per hash); bound concurrency so a
// burst of sign-in attempts cannot exhaust memory.
var hashSlots = make(chan struct{}, 4)

func argon(pw string, salt []byte, t, m uint32, p uint8, n uint32) []byte {
	hashSlots <- struct{}{}
	defer func() { <-hashSlots }()
	return argon2.IDKey([]byte(pw), salt, t, m, p, n)
}

// HashPassword returns an argon2id PHC-style hash.
func HashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	sum := argon(pw, salt, 2, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=2,p=2$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(sum))
}

// CheckPassword verifies pw against an argon2id hash from HashPassword.
func CheckPassword(hash, pw string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m > 1<<20 || t > 16 || p == 0 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon(pw, salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is checked for unknown users so response time does not reveal
// which accounts exist.
var dummyHash = sync.OnceValue(func() string { return HashPassword("pagelike-dummy-password") })

// Upsert creates or updates a local account. An empty password leaves the
// stored hash unchanged (or unset for new accounts, disabling password
// sign-in).
func (u *Users) Upsert(ctx context.Context, usr User, password string) error {
	if strings.TrimSpace(usr.Sub) == "" {
		return errors.New("identity: a user needs a sub")
	}
	hash := ""
	if password != "" {
		hash = HashPassword(password)
	}
	_, err := u.DB.ExecContext(ctx, `INSERT INTO users(sub, email, email_verified, name, roles, password_hash, created_ms)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(sub) DO UPDATE SET email=excluded.email, email_verified=excluded.email_verified,
		name=excluded.name, roles=excluded.roles,
		password_hash=CASE WHEN excluded.password_hash='' THEN users.password_hash ELSE excluded.password_hash END`,
		usr.Sub, usr.Email, boolInt(usr.EmailVerified), usr.Name, strings.Join(usr.Roles, ","), hash, time.Now().UnixMilli())
	return err
}

// ClearPassword disables password sign-in for an account.
func (u *Users) ClearPassword(ctx context.Context, sub string) error {
	_, err := u.DB.ExecContext(ctx, `UPDATE users SET password_hash = '' WHERE sub = ?`, sub)
	return err
}

// Delete removes an account and signs it out everywhere.
func (u *Users) Delete(ctx context.Context, sub string) error {
	if _, err := u.DB.ExecContext(ctx, `DELETE FROM sessions WHERE sub = ?`, sub); err != nil {
		return err
	}
	_, err := u.DB.ExecContext(ctx, `DELETE FROM users WHERE sub = ?`, sub)
	return err
}

// List returns all accounts.
func (u *Users) List(ctx context.Context) ([]User, error) {
	rows, err := u.DB.QueryContext(ctx, `SELECT sub, email, email_verified, name, roles, password_hash != '' FROM users ORDER BY sub`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		x, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

// Get returns one account (sql.ErrNoRows when absent).
func (u *Users) Get(ctx context.Context, sub string) (*User, error) {
	return scanUser(u.DB.QueryRowContext(ctx, `SELECT sub, email, email_verified, name, roles, password_hash != '' FROM users WHERE sub = ?`, sub))
}

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var x User
	var ev, hp int
	var roles string
	if err := row.Scan(&x.Sub, &x.Email, &ev, &x.Name, &roles, &hp); err != nil {
		return nil, err
	}
	x.EmailVerified, x.HasPassword = ev == 1, hp == 1
	x.Roles = SplitRoles(roles)
	return &x, nil
}

// HasPasswordAccounts reports whether any account can sign in with a
// password (the site then offers the local sign-in form).
func (u *Users) HasPasswordAccounts(ctx context.Context) bool {
	var one int
	err := u.DB.QueryRowContext(ctx, `SELECT 1 FROM users WHERE password_hash != '' LIMIT 1`).Scan(&one)
	return err == nil
}

// ErrInvalidCredentials is returned for any failed password sign-in.
var ErrInvalidCredentials = errors.New("invalid credentials")

// Authenticate checks a login name (sub, or email) and password. A sub
// match is preferred over an email match.
func (u *Users) Authenticate(ctx context.Context, login, password string) (*User, error) {
	var sub, hash string
	err := u.DB.QueryRowContext(ctx, `SELECT sub, password_hash FROM users
		WHERE sub = ? OR (email != '' AND email = ?) ORDER BY (sub = ?) DESC LIMIT 1`, login, login, login).Scan(&sub, &hash)
	if err != nil || hash == "" {
		CheckPassword(dummyHash(), password)
		return nil, ErrInvalidCredentials
	}
	if !CheckPassword(hash, password) {
		return nil, ErrInvalidCredentials
	}
	return u.Get(ctx, sub)
}

// Link is a mapping from an external identity (issuer + provider subject)
// to a local user name, used when a site changes identity provider but its
// rules and data name the old subjects (docs/identity.md).
type Link struct {
	Issuer, IdPSub, Sub string
}

// SetLink maps (issuer, idpSub) to sub for future sign-ins.
func (u *Users) SetLink(ctx context.Context, l Link) error {
	if err := u.schema(); err != nil {
		return err
	}
	if l.Issuer == "" || l.IdPSub == "" || l.Sub == "" {
		return errors.New("identity: a link needs an issuer, a provider subject and a local sub")
	}
	_, err := u.DB.ExecContext(ctx, `INSERT INTO identity_links(issuer, idp_sub, sub, created_ms) VALUES (?,?,?,?)
		ON CONFLICT(issuer, idp_sub) DO UPDATE SET sub=excluded.sub`, NormalizeIssuer(l.Issuer), l.IdPSub, l.Sub, time.Now().UnixMilli())
	return err
}

// DeleteLink removes a mapping.
func (u *Users) DeleteLink(ctx context.Context, issuer, idpSub string) error {
	if err := u.schema(); err != nil {
		return err
	}
	_, err := u.DB.ExecContext(ctx, `DELETE FROM identity_links WHERE issuer = ? AND idp_sub = ?`, NormalizeIssuer(issuer), idpSub)
	return err
}

// Links lists every mapping.
func (u *Users) Links(ctx context.Context) ([]Link, error) {
	if err := u.schema(); err != nil {
		return nil, err
	}
	rows, err := u.DB.QueryContext(ctx, `SELECT issuer, idp_sub, sub FROM identity_links ORDER BY issuer, idp_sub`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Link
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.Issuer, &l.IdPSub, &l.Sub); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// linkedSub returns the local sub an external identity is mapped to.
func (u *Users) linkedSub(ctx context.Context, issuer, idpSub string) (string, bool) {
	if u.schema() != nil {
		return "", false
	}
	var sub string
	err := u.DB.QueryRowContext(ctx, `SELECT sub FROM identity_links WHERE issuer = ? AND idp_sub = ?`, NormalizeIssuer(issuer), idpSub).Scan(&sub)
	return sub, err == nil && sub != ""
}

// NormalizeIssuer strips a trailing slash and a discovery-document suffix so
// "https://idp/" and "https://idp/.well-known/openid-configuration" name the
// same issuer.
func NormalizeIssuer(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "/.well-known/openid-configuration")
	return strings.TrimSuffix(s, "/")
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
