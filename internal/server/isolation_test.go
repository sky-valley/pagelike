package server_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sky-valley/pagelike/internal/control"
	_ "github.com/sky-valley/pagelike/internal/features"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

// TestSitesAreIsolated checks the boundaries between sites on one instance:
// authoring keys are scoped to their site, sessions do not cross sites, and
// composition and Sessel queries only ever see their own site's documents.
func TestSitesAreIsolated(t *testing.T) {
	dir := t.TempDir()
	reg, _ := site.NewRegistry(dir)
	ctl, _ := control.Open(dir)
	ctx := context.Background()
	for _, n := range []string{"alpha", "beta"} {
		if _, err := reg.Create(ctx, n, site.Settings{DefaultGet: "allow"}); err != nil {
			t.Fatal(err)
		}
	}
	alphaKey, _, _ := ctl.CreateKey(ctx, "a", []string{"alpha"}, 0)
	betaKey, _, _ := ctl.CreateKey(ctx, "b", []string{"beta"}, 0)
	srv := httptest.NewServer(server.New(server.Config{Domain: "localhost"}, reg, ctl, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer srv.Close()
	do := func(method, host, path string, hdr map[string]string, body string) (int, http.Header, string) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Host = host
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, string(b)
	}
	bearer := func(k string) map[string]string { return map[string]string{"Authorization": "Bearer " + k} }

	// 1. Authoring keys are scoped to their site.
	if st, _, _ := do("PUT", "dav-beta.localhost", "/x.html", bearer(alphaKey), "<p>intrusion</p>"); st != http.StatusUnauthorized {
		t.Fatalf("alpha's key authored beta: %d", st)
	}
	secret := `<!DOCTYPE html><html><body><p id="secret" itemscope itemtype="https://ex/Secret"><span itemprop="v">beta-private</span></p></body></html>`
	if st, _, b := do("PUT", "dav-beta.localhost", "/private.html", bearer(betaKey), secret); st != 201 {
		t.Fatalf("beta setup: %d %s", st, b)
	}
	rules := `<!DOCTYPE html><html><body><table><tr itemscope itemtype="https://pagelove.org/AuthorizationRule"><td itemprop="actor">*</td><td itemprop="resource">/private.html</td><td itemprop="method">GET</td><td itemprop="selector"></td><td itemprop="action">Deny</td></tr>
<tr itemscope itemtype="https://pagelove.org/AuthorizationRule"><td itemprop="actor">*</td><td itemprop="resource">/*</td><td><span itemprop="method">GET</span><span itemprop="method">QUERY</span></td><td itemprop="selector"></td><td itemprop="action">Allow</td></tr></table></body></html>`
	do("PUT", "dav-beta.localhost", "/rules.html", bearer(betaKey), rules)
	do("PUT", "dav-alpha.localhost", "/rules.html", bearer(alphaKey), strings.ReplaceAll(rules, "/private.html", "/nothing.html"))

	// 2. A binding on alpha sees only alpha's documents, never beta's.
	page := `<!DOCTYPE html><html xmlns:p="https://pagelove.org/1.0" xmlns:r="https://pagelove.org/Binding/CSS"><body><p id="out" r:s="[itemtype='https://ex/Secret']" p:template="text/liquid">found {{ s.size }}{% for x in s %} {{ x.v }}{% endfor %}</p></body></html>`
	do("PUT", "dav-alpha.localhost", "/probe.html", bearer(alphaKey), page)
	if st, _, b := do("GET", "alpha.localhost", "/probe.html", nil, ""); st != 200 || strings.Contains(b, "beta-private") || !strings.Contains(b, "found 0") {
		t.Fatalf("alpha binding saw another site's data: %d %s", st, b)
	}
	// 3. A Sessel query on alpha cannot reach beta either.
	if _, _, b := do("QUERY", "alpha.localhost", "/probe.html", map[string]string{"Content-Type": "text/sessel"}, `${[itemtype='https://ex/Secret']}.count()`); strings.Contains(b, "beta-private") || strings.TrimSpace(b) != "0" {
		t.Fatalf("alpha Sessel query result %q", b)
	}

	// 4. Sessions do not cross sites: a signed-in beta session is anonymous on alpha.
	b, _ := reg.Get(ctx, "beta")
	b.Users.Upsert(ctx, identity.User{Sub: "eve", Name: "Eve"}, "")
	sid, err := b.Users.Login(ctx, "", &identity.Identity{Sub: "eve"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	cookie := map[string]string{"Cookie": identity.CookieSecure + "=" + sid}
	who := func(host string) string {
		_, _, body := do("GET", host, "/-pagelike/whoami", cookie, "")
		return body
	}
	if !strings.Contains(who("beta.localhost"), `"sub":"eve"`) {
		t.Fatalf("session not valid on its own site: %s", who("beta.localhost"))
	}
	if strings.Contains(who("alpha.localhost"), "eve") {
		t.Fatalf("beta session authenticated on alpha: %s", who("alpha.localhost"))
	}

	// 5. Authoring keys are never accepted as end-user identity on the public plane.
	if st, _, _ := do("GET", "beta.localhost", "/private.html", bearer(betaKey), ""); st != http.StatusUnauthorized {
		t.Fatalf("authoring key granted public-plane read of a denied document: %d", st)
	}
}
