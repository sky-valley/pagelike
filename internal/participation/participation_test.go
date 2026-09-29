package participation_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sky-valley/pagelike/internal/control"
	"github.com/sky-valley/pagelike/internal/identity"
	_ "github.com/sky-valley/pagelike/internal/participation"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

const rules = `<!DOCTYPE html><html><body><table>
<tr itemscope itemtype="https://pagelove.org/AuthorizationRule"><td itemprop="actor">*</td><td itemprop="resource">/*</td>
<td><span itemprop="method">GET</span></td><td itemprop="selector"></td><td itemprop="action">Allow</td></tr>
<tr itemscope itemtype="https://pagelove.org/AuthorizationRule"><td itemprop="actor">users</td><td itemprop="resource">/index.html</td>
<td><span itemprop="method">POST</span></td><td itemprop="selector">#skies</td><td itemprop="action">Allow</td></tr>
<tr itemscope itemtype="https://pagelove.org/AuthorizationRule"><td itemprop="actor">*</td><td itemprop="resource">/index.html</td>
<td><span itemprop="method">GET</span></td><td itemprop="selector">.private</td><td itemprop="action">Deny</td></tr>
</table></body></html>`

func TestParticipationRecordedAndShared(t *testing.T) {
	dir := t.TempDir()
	reg, _ := site.NewRegistry(dir)
	ctl, _ := control.Open(dir)
	ctx := context.Background()
	s, err := reg.Create(ctx, "sky", site.Settings{DefaultGet: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	s.Users.Upsert(ctx, identity.User{Sub: "alice", Name: "Alice"}, "")
	key, _, _ := ctl.CreateKey(ctx, "t", []string{"sky"}, 0)
	srv := httptest.NewServer(server.New(server.Config{Domain: "localhost", DevAuth: true}, reg, ctl, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer srv.Close()
	do := func(method, host, path, body string, hdr map[string]string) (int, string) {
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
		return resp.StatusCode, string(b)
	}
	auth := map[string]string{"Authorization": "Bearer " + key}
	do("PUT", "dav-sky.localhost", "/rules.html", rules, auth)
	do("PUT", "dav-sky.localhost", "/index.html", `<!DOCTYPE html><html><head><title>Show me your sky</title></head><body><ul id="skies"></ul></body></html>`, auth)

	alice := map[string]string{"X-Pagelike-Dev-User": "alice", "Range": "selector=#skies"}
	if st, b := do("POST", "sky.localhost", "/index.html", `<li id="s1">sunset over the bay</li>`, alice); st != 206 {
		t.Fatalf("contribute: %d %s", st, b)
	}
	if st, b := do("POST", "sky.localhost", "/index.html", `<li id="s2" class="private">my secret sky</li>`, alice); st != 206 {
		t.Fatalf("contribute private: %d %s", st, b)
	}
	// Anonymous contributions are refused by the rules and never recorded.
	if st, _ := do("POST", "sky.localhost", "/index.html", `<li id="s3">anon</li>`, map[string]string{"Range": "selector=#skies"}); st != 401 {
		t.Fatalf("anonymous POST: %d", st)
	}

	st, body := do("GET", "sky.localhost", "/-pagelike/participations", "", nil)
	if st != 200 {
		t.Fatalf("list: %d %s", st, body)
	}
	var out struct {
		Version        string
		Participations []struct {
			ID, ElementID, Contributor, ShareURL string `json:",omitempty"`
			ElementIDJ                           string `json:"element_id"`
			ShareURLJ                            string `json:"share_url"`
			VersionJ                             string `json:"experience_version"`
		}
	}
	json.Unmarshal([]byte(body), &out)
	if len(out.Participations) != 1 || out.Participations[0].ElementIDJ != "s1" || out.Participations[0].Contributor != "Alice" {
		t.Fatalf("expected only the public contribution by Alice, got %s", body)
	}
	if out.Participations[0].VersionJ == "" || out.Participations[0].VersionJ != out.Version {
		t.Fatalf("participation not tied to the experience version: %s", body)
	}
	st, page := do("GET", "sky.localhost", out.Participations[0].ShareURLJ, "", nil)
	if st != 200 || !strings.Contains(page, "sunset over the bay") || !strings.Contains(page, "Alice") || !strings.Contains(page, "Show me your sky") {
		t.Fatalf("share view: %d %s", st, page)
	}
	if strings.Contains(body, "secret") {
		t.Fatal("private contribution leaked in the list")
	}
}
