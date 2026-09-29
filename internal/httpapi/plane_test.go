package httpapi_test

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sky-valley/pagelike/internal/control"
	"github.com/sky-valley/pagelike/internal/httpapi"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

// plane is an in-process pagelike serving site "t" at t.localhost.
type plane struct {
	t   *testing.T
	srv *httptest.Server
	key string
	s   *site.Site
}

func newPlane(t *testing.T, settings site.Settings) *plane {
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
	s, err := reg.Create(context.Background(), "t", settings)
	if err != nil {
		t.Fatal(err)
	}
	key, _, err := ctl.CreateKey(context.Background(), "test", []string{"t"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(server.Config{Domain: "localhost"}, reg, ctl, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
		reg.Close()
		ctl.Close()
	})
	return &plane{t: t, srv: srv, key: key, s: s}
}

func (p *plane) do(method, host, path string, body string, h map[string]string) *http.Response {
	p.t.Helper()
	req, _ := http.NewRequest(method, p.srv.URL+path, strings.NewReader(body))
	req.Host = host
	if body == "" {
		req.Body, req.ContentLength = nil, 0
	}
	if strings.HasPrefix(host, "dav-") {
		req.Header.Set("Authorization", "Bearer "+p.key)
	}
	for k, v := range h {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	return resp
}

func (p *plane) author(path, body string) {
	p.t.Helper()
	resp := p.do("PUT", "dav-t.localhost", path, body, map[string]string{"Content-Type": "text/html"})
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		p.t.Fatalf("setup PUT %s: %d", path, resp.StatusCode)
	}
}

func readAll(r *http.Response) string {
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	return string(b)
}

const rulesDoc = `<html><body><table><tbody>` +
	`<tr itemscope itemtype="https://pagelove.org/AuthorizationRule"><td><span itemprop="actor">*</span></td><td><span itemprop="resource">/*</span></td><td><span itemprop="method">GET</span></td><td itemprop="selector"></td><td itemprop="action">Allow</td></tr>` +
	`<tr itemscope itemtype="https://pagelove.org/AuthorizationRule"><td><span itemprop="actor">*</span></td><td><span itemprop="resource">/doc.html</span></td><td><span itemprop="method">POST</span></td><td itemprop="selector">ul</td><td itemprop="action">Allow</td></tr>` +
	`<tr itemscope itemtype="https://pagelove.org/AuthorizationRule"><td><span itemprop="actor">editors</span></td><td><span itemprop="resource">/doc.html</span></td><td><span itemprop="method">PUT</span></td><td itemprop="selector">h1</td><td itemprop="action">Allow</td></tr>` +
	`<tr itemscope itemtype="https://pagelove.org/AuthorizationRule"><td><span itemprop="actor">*</span></td><td><span itemprop="resource">/doc.html</span></td><td><span itemprop="method">*</span></td><td itemprop="selector">li</td><td itemprop="action">Allow</td></tr>` +
	`</tbody></table></body></html>`

// The 207 layout the official clients parse (R-PROTO-4/17): a document part,
// then one part per selector of this actor's selector-scoped rules.
func TestOptionsMultipart(t *testing.T) {
	p := newPlane(t, site.Settings{DefaultGet: "allow"})
	p.author("/_rules.html", rulesDoc)
	resp := p.do("OPTIONS", "t.localhost", "/doc.html", "", map[string]string{"Accept": "multipart/mixed", "Prefer": "return=representation"})
	body := readAll(resp)
	m := regexp.MustCompile(`^multipart/mixed; boundary=([0-9A-Za-z]+)$`).FindStringSubmatch(resp.Header.Get("Content-Type"))
	if resp.StatusCode != http.StatusMultiStatus || m == nil {
		t.Fatalf("%d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	b := m[1]
	// Each selector part lists what that selector's rules grant (live
	// 2026-09-28), in PageLove's framing.
	want := "--" + b + "\r\nAllow: GET, HEAD, OPTIONS\r\n\r\n" +
		"--" + b + "\r\nContent-Range: selector ul\r\nAllow: POST, OPTIONS\r\n\r\n" +
		"--" + b + "\r\nContent-Range: selector li\r\nAllow: GET, HEAD, PUT, DELETE, POST, OPTIONS\r\n\r\n" +
		"--" + b + "--\r\n"
	if body != want {
		t.Errorf("207 body:\n%q\nwant\n%q", body, want)
	}
	if resp.Header.Get("Vary") != "Authorization, Accept" || resp.Header.Get("Accept-Ranges") != "selector" || resp.Header.Get("Allow") != "" {
		t.Errorf("headers %v", resp.Header)
	}
	// The flat document answer includes selector-scoped grants (live).
	resp = p.do("OPTIONS", "t.localhost", "/doc.html", "", nil)
	readAll(resp)
	if got := resp.Header.Get("Allow"); got != "GET, HEAD, PUT, DELETE, POST, OPTIONS" {
		t.Errorf("flat document OPTIONS %q", got)
	}
	// Flat forms: document level and selector level, never a literal star.
	resp = p.do("OPTIONS", "t.localhost", "/doc.html", "", map[string]string{"Range": "selector=li"})
	readAll(resp)
	if got := resp.Header.Get("Allow"); resp.StatusCode != 200 || got != "GET, HEAD, PUT, DELETE, POST, OPTIONS" {
		t.Errorf("selector OPTIONS %d %q", resp.StatusCode, got)
	}
	resp = p.do("OPTIONS", "t.localhost", "/other.html", "", map[string]string{"Accept": "multipart/mixed"})
	readAll(resp)
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Content-Type") != "" {
		t.Errorf("204 fallback: %d %v", resp.StatusCode, resp.Header)
	}
}

// The body cap is enforced before anything else and closes the connection.
func TestBodyCapFirst(t *testing.T) {
	p := newPlane(t, site.Settings{DefaultGet: "allow", MaxBodyBytes: 8})
	resp := p.do("PUT", "t.localhost", "/.pagelove/x.html", "123456789", nil) // reserved and unauthorized too
	body := readAll(resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge || !resp.Close ||
		!strings.Contains(body, `itemtype="https://pagelove.org/Error"`) || !strings.Contains(body, `content="413"`) {
		t.Errorf("%d close=%v\n%s", resp.StatusCode, resp.Close, body)
	}
	resp = p.do("PUT", "t.localhost", "/.pagelove/x.html", "12345678", nil)
	readAll(resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("at the cap: %d, want the reserved-namespace 403", resp.StatusCode)
	}
}

// A signed-in subscriber whose session ends gets the matching reset and the
// stream closes (R-SSE-34); a Last-Event-ID ahead of every event replays
// nothing and resets nothing (R-SSE-31).
func TestStreamResets(t *testing.T) {
	httpapi.SessionCheckInterval = 50 * time.Millisecond
	p := newPlane(t, site.Settings{DefaultGet: "allow"})
	p.author("/doc.html", "<html><body><h1>x</h1></body></html>")
	p.author("/_rules.html", rulesDoc)
	ctx := context.Background()
	if err := p.s.Users.Upsert(ctx, identity.User{Sub: "alice"}, ""); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ action, reason string }{{"invalidate", "session-invalidated"}, {"expire", "session-expired"}} {
		sid, err := p.s.Users.Login(ctx, "", &identity.Identity{Sub: "alice"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		resp := p.do("GET", "t.localhost", "/doc.html", "", map[string]string{"Accept": "text/event-stream", "Cookie": identity.CookieSecure + "=" + sid})
		if resp.StatusCode != 200 {
			t.Fatalf("subscribe: %d %s", resp.StatusCode, readAll(resp))
		}
		rd := bufio.NewReader(resp.Body)
		head := ""
		for i := 0; i < 3; i++ {
			l, _ := rd.ReadString('\n')
			head += l
		}
		if head != ": connected\n\nevent: pagelove-connection\n" {
			t.Fatalf("stream head %q", head)
		}
		if c.action == "invalidate" {
			p.s.Users.EndSession(ctx, sid)
		} else {
			p.s.Store.DB().Exec(`UPDATE sessions SET expires_ms = 1 WHERE id = ?`, sid)
		}
		rest, _ := io.ReadAll(rd) // ends when the server closes the stream
		resp.Body.Close()
		if !strings.Contains(string(rest), "event: reset\ndata: <article itemscope itemtype=\"https://pagelove.org/StreamReset\">\ndata:   <span itemprop=\"reason\">"+c.reason+"</span>") {
			t.Errorf("%s: stream tail %q", c.action, rest)
		}
	}
	resp := p.do("GET", "t.localhost", "/doc.html", "", map[string]string{"Accept": "text/event-stream", "Last-Event-ID": "v1~0123456789ab." + strconv.FormatInt(time.Now().UnixMilli(), 10) + "-999999"})
	defer resp.Body.Close()
	rd := bufio.NewReader(resp.Body)
	for i := 0; i < 5; i++ { // ": connected", blank, the connection event
		rd.ReadString('\n')
	}
	done := make(chan string, 1)
	go func() { l, _ := rd.ReadString('\n'); done <- l }()
	select {
	case l := <-done:
		t.Errorf("a Last-Event-ID ahead of every event produced %q", l)
	case <-time.After(300 * time.Millisecond):
	}
}
