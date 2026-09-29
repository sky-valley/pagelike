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
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

type testInstance struct {
	t   *testing.T
	dir string
	reg *site.Registry
	ctl *control.DB
	srv *httptest.Server
	key string
}

func newInstance(t *testing.T, sites ...string) *testInstance {
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
	ctx := context.Background()
	for _, s := range sites {
		if _, err := reg.Create(ctx, s, site.Settings{}); err != nil {
			t.Fatal(err)
		}
	}
	key, _, err := ctl.CreateKey(ctx, "test", []string{"*"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(server.Config{Domain: "localhost"}, reg, ctl, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(func() { srv.Close(); reg.Close(); ctl.Close() })
	return &testInstance{t: t, dir: dir, reg: reg, ctl: ctl, srv: srv, key: key}
}

func (in *testInstance) do(method, host, path string, hdr map[string]string, body string) (int, http.Header, string) {
	in.t.Helper()
	req, _ := http.NewRequest(method, in.srv.URL+path, strings.NewReader(body))
	req.Host = host
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if strings.HasPrefix(host, "dav-") {
		req.Header.Set("Authorization", "Bearer "+in.key)
	}
	resp, err := in.srv.Client().Do(req)
	if err != nil {
		in.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(b)
}

const openRules = `<!DOCTYPE html><html><body><table><tr itemscope itemtype="https://pagelove.org/AuthorizationRule">
<td itemprop="actor">*</td><td itemprop="resource">/*</td><td><span itemprop="method">GET</span><span itemprop="method">POST</span>
<span itemprop="method">PUT</span></td><td itemprop="action">Allow</td></tr></table></body></html>`

func TestForkCopiesAuthoredStateOnly(t *testing.T) {
	in := newInstance(t, "sky")
	page := "<!DOCTYPE html><html><head><title>Show me your sky</title></head><body><h1>Show me your sky</h1><ul id=\"skies\"></ul></body></html>"
	if st, _, b := in.do("PUT", "dav-sky.localhost", "/rules.html", nil, openRules); st != 201 {
		t.Fatalf("rules: %d %s", st, b)
	}
	if st, _, b := in.do("PUT", "dav-sky.localhost", "/index.html", nil, page); st != 201 {
		t.Fatalf("page: %d %s", st, b)
	}
	// A participant contributes through the public plane.
	if st, _, b := in.do("POST", "sky.localhost", "/index.html", map[string]string{"Range": "selector=#skies"}, `<li id="p1">alice's sunset</li>`); st != 206 {
		t.Fatalf("participation: %d %s", st, b)
	}
	// A participant-created document.
	if st, _, b := in.do("PUT", "sky.localhost", "/uploads/p2.html", nil, "<p>bob's private note</p>"); st != 201 {
		t.Fatalf("participant doc: %d %s", st, b)
	}
	if _, err := site.Fork(context.Background(), in.reg, "sky", "dog", "show me your dog"); err != nil {
		t.Fatal(err)
	}
	st, _, body := in.do("GET", "dog.localhost", "/index.html", nil, "")
	if st != 200 || !strings.Contains(body, "Show me your sky") {
		t.Fatalf("fork page: %d %s", st, body)
	}
	if strings.Contains(body, "alice") {
		t.Fatalf("fork leaked participant contribution: %s", body)
	}
	if st, _, _ := in.do("GET", "dog.localhost", "/uploads/p2.html", nil, ""); st != 404 {
		t.Fatalf("fork leaked participant document: status %d", st)
	}
	// The original keeps its state.
	if _, _, body := in.do("GET", "sky.localhost", "/index.html", nil, ""); !strings.Contains(body, "alice") {
		t.Fatalf("source lost participant state: %s", body)
	}
	// Writes to the fork do not reach the source.
	in.do("POST", "dog.localhost", "/index.html", map[string]string{"Range": "selector=#skies"}, `<li id="d1">rex</li>`)
	if _, _, body := in.do("GET", "sky.localhost", "/index.html", nil, ""); strings.Contains(body, "rex") {
		t.Fatal("fork write leaked into source")
	}
	dog, _ := in.reg.Get(context.Background(), "dog")
	if l := dog.Settings().Lineage; l == nil || l.ForkedFrom != "sky" || l.SourceDigest == "" {
		t.Fatalf("lineage not recorded: %+v", l)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	in := newInstance(t, "src", "dst")
	in.do("PUT", "dav-src.localhost", "/rules.html", nil, openRules)
	in.do("PUT", "dav-src.localhost", "/a.html", nil, "<!DOCTYPE html><html><body><p id=x>one</p></body></html>")
	in.do("PUT", "dav-src.localhost", "/img/logo.png", map[string]string{"Content-Type": "image/png"}, "\x89PNG\r\n\x1a\nfake")
	in.do("PUT", "src.localhost", "/a.html", map[string]string{"Range": "selector=#x"}, "<p id=x>two</p>")
	ctx := context.Background()
	src, _ := in.reg.Get(ctx, "src")
	out := t.TempDir()
	if _, err := site.Export(ctx, src, out, "live"); err != nil {
		t.Fatal(err)
	}
	dst, _ := in.reg.Get(ctx, "dst")
	if _, err := site.Import(ctx, dst, out, site.ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, body := in.do("GET", "dst.localhost", "/a.html", nil, ""); !strings.Contains(body, "two") {
		t.Fatalf("live state not imported: %s", body)
	}
	if st, h, body := in.do("GET", "dst.localhost", "/img/logo.png", nil, ""); st != 200 || h.Get("Content-Type") != "image/png" || body != "\x89PNG\r\n\x1a\nfake" {
		t.Fatalf("blob not byte-exact: %d %q %q", st, h.Get("Content-Type"), body)
	}
	a1, _ := src.Store.AuthoredDigest(ctx)
	a2, _ := dst.Store.AuthoredDigest(ctx)
	if a1 != a2 {
		t.Fatalf("authored baseline differs after import: %s vs %s", a1, a2)
	}
}
