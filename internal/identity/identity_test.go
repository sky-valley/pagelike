package identity

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sky-valley/pagelike/internal/store"
)

func newUsers(t *testing.T) *Users {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &Users{DB: st.DB()}
}

func TestIdentityFromClaims(t *testing.T) {
	for _, tc := range []struct {
		name   string
		claims map[string]any
		claim  string
		want   Identity
	}{
		{"bool verified", map[string]any{"sub": "s1", "email": "a@x", "email_verified": true, "roles": []any{"a", "b", 3}},
			"", Identity{Sub: "s1", Email: "a@x", EmailVerified: true, Roles: []string{"a", "b"}}},
		{"string verified", map[string]any{"sub": "s1", "email_verified": "true"}, "", Identity{Sub: "s1", EmailVerified: true}},
		{"other strings are not verified", map[string]any{"sub": "s1", "email_verified": "yes"}, "", Identity{Sub: "s1"}},
		{"space-separated roles", map[string]any{"sub": "s1", "roles": "admins  staff"}, "", Identity{Sub: "s1", Roles: []string{"admins", "staff"}}},
		{"nested roles claim", map[string]any{"sub": "s1", "realm_access": map[string]any{"roles": []any{"ops"}}}, "realm_access.roles",
			Identity{Sub: "s1", Roles: []string{"ops"}}},
		{"nothing synthesized", map[string]any{"sub": "123456"}, "", Identity{Sub: "123456"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := IdentityFromClaims(tc.claims, tc.claim)
			if err != nil {
				t.Fatal(err)
			}
			id.Claims = nil
			if !reflect.DeepEqual(*id, tc.want) {
				t.Errorf("got %+v, want %+v", *id, tc.want)
			}
		})
	}
	if _, err := IdentityFromClaims(map[string]any{"email": "a@x"}, ""); err == nil {
		t.Error("a missing sub must be refused")
	}
}

func TestSessionsLifecycle(t *testing.T) {
	ctx := context.Background()
	u := newUsers(t)
	if err := u.Upsert(ctx, User{Sub: "bob", Email: "bob@x", EmailVerified: true, Roles: []string{"staff"}}, "pw"); err != nil {
		t.Fatal(err)
	}
	anon := NewSessionID()
	if _, st := u.ResolveSession(ctx, anon); st != SessionAnonymous {
		t.Fatalf("fresh id state = %v", st)
	}
	for _, bad := range []string{"", "short", strings.Repeat("x", 200), "has space in it!!"} {
		if _, st := u.ResolveSession(ctx, bad); st != SessionMissing {
			t.Errorf("%q state = %v", bad, st)
		}
	}
	u.DB.Exec(`INSERT INTO transients(session_id, path, key, html) VALUES (?, '/p', 'k', '<b>x</b>')`, anon)

	usr, err := u.Authenticate(ctx, "bob@x", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.Authenticate(ctx, "bob", "nope"); err == nil {
		t.Error("wrong password accepted")
	}
	if _, err := u.Authenticate(ctx, "nobody", "pw"); err == nil {
		t.Error("unknown user accepted")
	}
	sid, err := u.Login(ctx, anon, usr.LocalIdentity(), 0)
	if err != nil {
		t.Fatal(err)
	}
	p, st := u.ResolveSession(ctx, sid)
	if st != SessionActive || p.Sub != "bob" || !p.EmailVerified || !p.HasRole("staff") || p.Claims["email"] != "bob@x" {
		t.Fatalf("resolved %v %+v", st, p)
	}
	var n int
	u.DB.QueryRow(`SELECT count(*) FROM transients WHERE session_id = ?`, sid).Scan(&n)
	if n != 1 {
		t.Errorf("transients moved: %d", n)
	}
	// Account changes apply to live local sessions.
	u.Upsert(ctx, User{Sub: "bob", Email: "bob@x", EmailVerified: true, Roles: []string{"admins"}}, "")
	if p, _ := u.ResolveSession(ctx, sid); !p.HasRole("admins") || p.HasRole("staff") {
		t.Errorf("roles after update: %v", p.Roles)
	}
	if u.Status(ctx, sid) != SessionActive {
		t.Error("status")
	}
	u.DB.Exec(`UPDATE sessions SET expires_ms = 1 WHERE id = ?`, sid)
	if p, st := u.ResolveSession(ctx, sid); st != SessionExpired || p.Authenticated {
		t.Errorf("expired: %v %v", st, p.Authenticated)
	}
	sid2, _ := u.Login(ctx, "", usr.LocalIdentity(), 0)
	if err := u.Logout(ctx, sid2); err != nil {
		t.Fatal(err)
	}
	if _, st := u.ResolveSession(ctx, sid2); st != SessionInvalidated || !st.NeedsNewID() {
		t.Errorf("after logout: %v", st)
	}
	// Deleting the account ends its sessions.
	sid3, _ := u.Login(ctx, "", usr.LocalIdentity(), 0)
	u.Delete(ctx, "bob")
	if _, st := u.ResolveSession(ctx, sid3); st != SessionInvalidated {
		t.Errorf("after delete: %v", st)
	}
}

func TestOIDCSessionAndLinks(t *testing.T) {
	ctx := context.Background()
	u := newUsers(t)
	// A local account without password grants roles to the provider identity.
	u.Upsert(ctx, User{Sub: "sub_1", Roles: []string{"editors"}}, "")
	if u.HasPasswordAccounts(ctx) {
		t.Error("an account without password is not a password account")
	}
	id := &Identity{Sub: "sub_1", Email: "a@x", EmailVerified: true, Name: "A", Roles: []string{"staff"},
		Claims: map[string]any{"sub": "sub_1", "tenant": "t1"}, Issuer: "https://idp.example"}
	sid, err := u.Login(ctx, "", id, 0)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := u.ResolveSession(ctx, sid)
	if p.Issuer != "https://idp.example" || p.Email != "a@x" || !reflect.DeepEqual(p.Roles, []string{"staff", "editors"}) || p.Claims["tenant"] != "t1" {
		t.Errorf("principal %+v", p)
	}
	if err := u.SetLink(ctx, Link{Issuer: "https://idp.example/.well-known/openid-configuration", IdPSub: "999", Sub: "sub_old"}); err != nil {
		t.Fatal(err)
	}
	if sub, ok := u.linkedSub(ctx, "https://idp.example/", "999"); !ok || sub != "sub_old" {
		t.Errorf("link lookup = %q %v", sub, ok)
	}
	if ls, _ := u.Links(ctx); len(ls) != 1 || ls[0].Issuer != "https://idp.example" {
		t.Errorf("links = %+v", ls)
	}
	u.DeleteLink(ctx, "https://idp.example", "999")
	if _, ok := u.linkedSub(ctx, "https://idp.example", "999"); ok {
		t.Error("link not deleted")
	}
}

func TestAuthSectionAndVars(t *testing.T) {
	p := &Principal{Authenticated: true, Sub: "sub-alice", Email: "alice@example.com", EmailVerified: true,
		Claims: map[string]any{"email": "alice@example.com", "email_verified": true, "n": 3.0, "list": []any{"x"}, "bad key": "v", "q": `a"b`}}
	got := AuthSection(p, []string{"alice@example.com", "users"})
	for _, want := range []string{
		`<section itemprop="auth" itemscope itemtype="https://pagelove.org/Authorization">`,
		`itemtype="https://pagelove.org/Claims"`,
		`<meta itemprop="email" content="alice@example.com">`,
		`<meta itemprop="email_verified" content="true">`,
		`<meta itemprop="n" content="3">`,
		`<meta itemprop="q" content="a&#34;b">`,
		`<meta itemprop="username" content="sub-alice">`,
		`<meta itemprop="role" content="users">`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("auth section lacks %s:\n%s", want, got)
		}
	}
	if strings.Contains(got, "list") || strings.Contains(got, "bad key") {
		t.Errorf("non-scalar or unnamed claims rendered:\n%s", got)
	}
	if got := AuthSection(Anonymous("s"), nil); got != `<section itemprop="auth" itemscope itemtype="https://pagelove.org/Authorization"></section>` {
		t.Errorf("anonymous section = %s", got)
	}
	v := AuthVars(p, []string{"users"})
	if v["username"] != "sub-alice" || !reflect.DeepEqual(v["role"], v["roles"]) {
		t.Errorf("vars = %v", v)
	}
	if len(AuthVars(nil, nil)) != 0 {
		t.Error("anonymous vars must be empty")
	}
}

func TestThrottle(t *testing.T) {
	th := NewThrottle(2, time.Hour)
	th.Fail("a")
	if !th.Allow("a") {
		t.Fatal("one failure must not block")
	}
	th.Fail("a")
	if th.Allow("a") || !th.Allow("b") {
		t.Fatal("the limit applies per key")
	}
	th.Reset("a")
	if !th.Allow("a") {
		t.Fatal("reset")
	}
	short := NewThrottle(1, time.Millisecond)
	short.Fail("k")
	time.Sleep(3 * time.Millisecond)
	if !short.Allow("k") {
		t.Fatal("the window expires")
	}
}

func TestCheckPasswordRejectsHostileParameters(t *testing.T) {
	// A stored hash must not be able to demand unbounded memory.
	if CheckPassword("$argon2id$v=19$m=4194304,t=2,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "x") {
		t.Error("accepted")
	}
	h := HashPassword("secret")
	if !CheckPassword(h, "secret") || CheckPassword(h, "Secret") {
		t.Error("round trip")
	}
}
