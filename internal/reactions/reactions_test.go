package reactions_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sky-valley/pagelike/internal/control"
	_ "github.com/sky-valley/pagelike/internal/query"
	_ "github.com/sky-valley/pagelike/internal/reactions"
	"github.com/sky-valley/pagelike/internal/server"
	"github.com/sky-valley/pagelike/internal/site"
)

// instance is a pagelike server with one site "t" whose outbound policy
// allows the test's capture sink.
type instance struct {
	t    *testing.T
	reg  *site.Registry
	srv  *httptest.Server
	key  string
	sink *sink
}

type captured struct {
	method, path string
	header       http.Header
	body         string
	at           time.Time
}

type sink struct {
	srv    *httptest.Server
	mu     sync.Mutex
	reqs   []captured
	status map[string][]int
}

func newSink(t *testing.T) *sink {
	s := &sink{status: map[string][]int{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		n := 0
		for _, c := range s.reqs {
			if c.path == r.URL.Path {
				n++
			}
		}
		s.reqs = append(s.reqs, captured{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: string(b), at: time.Now()})
		code := 200
		if st := s.status[r.URL.Path]; len(st) > 0 {
			code = st[min(n, len(st)-1)]
		}
		s.mu.Unlock()
		w.WriteHeader(code)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// wait returns the requests on path once at least n arrived.
func (s *sink) wait(t *testing.T, path string, n int, d time.Duration) []captured {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		s.mu.Lock()
		var out []captured
		for _, c := range s.reqs {
			if c.path == path {
				out = append(out, c)
			}
		}
		s.mu.Unlock()
		if len(out) >= n || time.Now().After(deadline) {
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func newInstance(t *testing.T) *instance {
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
	sk := newSink(t)
	ctx := context.Background()
	if _, err := reg.Create(ctx, "t", site.Settings{DefaultGet: "allow",
		Outbound: &site.OutboundSettings{Allow: []string{strings.TrimPrefix(sk.srv.URL, "http://")}}}); err != nil {
		t.Fatal(err)
	}
	key, _, err := ctl.CreateKey(ctx, "test", []string{"*"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(server.Config{Domain: "localhost"}, reg, ctl, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(func() { srv.Close(); reg.Close(); ctl.Close() })
	in := &instance{t: t, reg: reg, srv: srv, key: key, sink: sk}
	in.dav("PUT", "/_rules.html", openRules)
	return in
}

const openRules = `<!DOCTYPE html><html><body><table><tr itemscope itemtype="https://pagelove.org/AuthorizationRule">
<td itemprop="actor">*</td><td itemprop="resource">/*</td><td><span itemprop="method">GET</span><span itemprop="method">HEAD</span><span itemprop="method">POST</span>
<span itemprop="method">PUT</span><span itemprop="method">DELETE</span></td><td itemprop="action">Allow</td></tr></table></body></html>`

func (in *instance) do(method, host, path string, hdr map[string]string, body string) (int, http.Header, string) {
	in.t.Helper()
	req, _ := http.NewRequest(method, in.srv.URL+path, strings.NewReader(body))
	req.Host = host
	if body == "" {
		req.Body = nil
	}
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

func (in *instance) dav(method, path, body string) {
	in.t.Helper()
	if st, _, b := in.do(method, "dav-t.localhost", path, map[string]string{"Content-Type": "text/html"}, body); st >= 300 {
		in.t.Fatalf("dav %s %s: %d %s", method, path, st, b)
	}
}

func (in *instance) pub(method, path string, hdr map[string]string, body string) (int, http.Header, string) {
	in.t.Helper()
	if hdr == nil {
		hdr = map[string]string{}
	}
	if _, ok := hdr["Content-Type"]; !ok && body != "" {
		hdr["Content-Type"] = "text/html"
	}
	return in.do(method, "t.localhost", path, hdr, body)
}

// install stores a reaction document through the public plane.
func (in *instance) install(path, body string) {
	in.t.Helper()
	if st, _, b := in.pub("PUT", path, nil, "<!DOCTYPE html><html><body>"+body+"</body></html>"); st >= 300 {
		in.t.Fatalf("install %s: %d %s", path, st, b)
	}
}

func sesselItem(prop, src string) string {
	return `<div itemprop="` + prop + `" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source" type="text/sessel">` + src + `</script></div>`
}

func trigger(resource, method, inner string) string {
	m := ""
	if method != "" {
		m = `<meta itemprop="method" content="` + method + `">`
	}
	return `<div hidden itemscope itemtype="https://pagelove.org/Trigger"><meta itemprop="resource" content="` + resource + `">` + m + inner + `</div>`
}

func processor(resource, method, status, inner string) string {
	extra := ""
	if method != "" {
		extra += `<meta itemprop="method" content="` + method + `">`
	}
	if status != "" {
		extra += `<meta itemprop="status" content="` + status + `">`
	}
	return `<div hidden itemscope itemtype="https://pagelove.org/Processor"><meta itemprop="resource" content="` + resource + `">` + extra + inner + `</div>`
}

const throw409 = `@schema HTTPResponse url("https://pagelove.org/HTTPResponse");
throw new HTTPResponse { status: 409, message: "refused" }`

func TestTriggerThrowStopsCore(t *testing.T) {
	in := newInstance(t)
	in.install("/r/t.html", trigger("/d/*", "PUT", sesselItem("action", throw409)))
	st, h, body := in.pub("PUT", "/d/x.html", nil, "<p>x</p>")
	if st != 409 || !strings.Contains(body, "refused") {
		t.Fatalf("got %d %q", st, body)
	}
	if ct := h.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type %q", ct)
	}
	if st, _, _ := in.pub("GET", "/d/x.html", nil, ""); st != 404 {
		t.Fatalf("document was written: %d", st)
	}
	// GET is not filtered by a PUT trigger.
	if st, _, _ := in.pub("GET", "/r/t.html", nil, ""); st != 200 {
		t.Fatalf("GET: %d", st)
	}
}

func TestTriggerRunsBeforeCoreErrors(t *testing.T) {
	in := newInstance(t)
	in.install("/r/t.html", trigger("/d/*", "PUT", sesselItem("action", throw409)))
	st, _, _ := in.pub("PUT", "/d/missing.html", map[string]string{"Range": "selector=h1"}, "<h1>x</h1>")
	if st != 409 {
		t.Fatalf("got %d, want the trigger's 409 before core's 404", st)
	}
}

func TestAuthorizationBeforeTriggers(t *testing.T) {
	in := newInstance(t)
	in.dav("PUT", "/deny.html", `<!DOCTYPE html><html><body><div itemscope itemtype="https://pagelove.org/AuthorizationRule">
<span itemprop="actor">*</span><span itemprop="resource">/d/x.html</span><span itemprop="method">PUT</span><span itemprop="action">Deny</span></div></body></html>`)
	in.install("/r/t.html", trigger("/d/*", "PUT", sesselItem("action", throw409)))
	if st, _, _ := in.pub("PUT", "/d/x.html", nil, "<p>x</p>"); st != 401 {
		t.Fatalf("got %d, want 401 (authorization first, decision C1)", st)
	}
}

func TestGateOtherwiseAndHeaderLookup(t *testing.T) {
	in := newInstance(t)
	gate := `@schema Context url("https://pagelove.org/Context");
(Context.request.headers["Authorization"] ?? "") == "Basic dGVzdDp0ZXN0"`
	deny := `@schema HTTPResponse url("https://pagelove.org/HTTPResponse");
@schema Pair url("https://pagelove.org/Pair");
throw new HTTPResponse { status: 401, header: new Pair { key: "WWW-Authenticate", value: "Basic realm=\"t\"" }, body: "nope" }`
	in.install("/r/t.html", trigger("/admin/*", "GET", sesselItem("when", gate)+sesselItem("action", "1")+sesselItem("otherwise", deny)))
	in.dav("PUT", "/admin/index.html", "<!DOCTYPE html><html><body><h1>Dash</h1></body></html>")
	st, h, _ := in.pub("GET", "/admin/index.html", nil, "")
	if st != 401 || h.Get("WWW-Authenticate") != `Basic realm="t"` {
		t.Fatalf("got %d %v", st, h)
	}
	if h.Get("ETag") != "" {
		t.Fatalf("thrown response carried the stored ETag")
	}
	if st, _, b := in.pub("GET", "/admin/index.html", map[string]string{"authorization": "Basic dGVzdDp0ZXN0"}, ""); st != 200 || !strings.Contains(b, "Dash") {
		t.Fatalf("with credential: %d %s", st, b)
	}
	// HEAD is matched by a GET trigger.
	if st, _, _ := in.pub("HEAD", "/admin/index.html", nil, ""); st != 401 {
		t.Fatalf("HEAD: %d", st)
	}
}

func TestSesselThrownBodyIsReserialized(t *testing.T) {
	in := newInstance(t)
	in.install("/r/t.html", trigger("/d/*", "PUT", sesselItem("action", `@schema HTTPResponse url("https://pagelove.org/HTTPResponse");
throw new HTTPResponse { status: 409, body: "a && b => c" }`)))
	_, _, body := in.pub("PUT", "/d/x.html", nil, "<p>x</p>")
	if body != "a &amp;&amp; b =&gt; c" {
		t.Fatalf("body %q", body)
	}
}

func TestRuntimeErrorIs500WithBindingFailure(t *testing.T) {
	in := newInstance(t)
	in.install("/r/t.html", trigger("/d/*", "PUT", sesselItem("when", `"abc".Integer() > 0`)+sesselItem("action", "1")))
	st, _, body := in.pub("PUT", "/d/x.html", nil, "<p>x</p>")
	if st != 500 || !strings.Contains(body, "https://pagelove.org/BindingFailure") {
		t.Fatalf("got %d %s", st, body)
	}
}

func TestJavaScriptWithoutRuntimeFailsClearly(t *testing.T) {
	in := newInstance(t)
	js := `<div itemprop="action" itemscope itemtype="https://pagelove.org/JavaScript/Module"><script type="module" itemprop="source">export default function(ctx) { return 1; }</script></div>`
	in.install("/r/t.html", trigger("/d/*", "PUT", js))
	st, _, body := in.pub("PUT", "/d/x.html", nil, "<p>x</p>")
	if st != 501 || !strings.Contains(body, "unavailable") || !strings.Contains(body, "https://pagelove.org/JavaScript/Module") {
		t.Fatalf("got %d %s", st, body)
	}
}

func TestSideEffectWriteSurvivesLaterThrow(t *testing.T) {
	in := newInstance(t)
	in.install("/r/a.html", trigger("/d/*", "PUT", sesselItem("action", `@schema Pagelove url("https://pagelove.org/1.0");
@schema Thing url("https://schema.org/Thing");
Pagelove.PUT(new Thing { name: "survivor" }, "/side/record.html")`)))
	in.install("/r/b.html", trigger("/d/*", "PUT", sesselItem("action", throw409)))
	if st, _, _ := in.pub("PUT", "/d/x.html", nil, "<p>x</p>"); st != 409 {
		t.Fatalf("got %d", st)
	}
	st, _, body := in.pub("GET", "/side/record.html", nil, "")
	if st != 200 || !strings.Contains(body, "survivor") || !strings.Contains(body, "https://schema.org/Thing") {
		t.Fatalf("side effect: %d %s", st, body)
	}
}

func TestBodyTransformation(t *testing.T) {
	in := newInstance(t)
	in.install("/r/t.html", trigger("/d/*", "PUT", sesselItem("action", `@schema Context url("https://pagelove.org/Context");
@schema Pagelove url("https://pagelove.org/1.0");
@schema Article url("https://schema.org/Article");
Pagelove.PUT(new Article { name: "transformed" }, Context.request.path)`)))
	if st, _, b := in.pub("PUT", "/d/post.html", nil, "<p>original</p>"); st >= 300 {
		t.Fatalf("put: %d %s", st, b)
	}
	_, _, body := in.pub("GET", "/d/post.html", nil, "")
	if !strings.Contains(body, "transformed") || strings.Contains(body, "original") {
		t.Fatalf("stored %s", body)
	}
}

func TestProcessorRewritesStatusAndBody(t *testing.T) {
	in := newInstance(t)
	in.dav("PUT", "/posts/empty.html", "<!DOCTYPE html><html><body><h1>Not found</h1></body></html>")
	in.install("/r/p.html", processor("/posts/*", "GET", "200",
		sesselItem("when", `@schema Context url("https://pagelove.org/Context");
Context.response.body.contains("blog.example/Post") == false`)+
			sesselItem("action", `@schema Context url("https://pagelove.org/Context");
Context.response.status = 404`)))
	st, h, body := in.pub("GET", "/posts/empty.html", nil, "")
	if st != 404 || !strings.Contains(body, "Not found") || h.Get("ETag") == "" {
		t.Fatalf("got %d %v %s", st, h, body)
	}
	if st, _, _ := in.pub("HEAD", "/posts/empty.html", nil, ""); st != 404 {
		t.Fatalf("HEAD: %d (processors must see the GET body)", st)
	}
	in.install("/r/q.html", processor("/missing/*", "GET", "404", sesselItem("action", `@schema Context url("https://pagelove.org/Context");
Context.response.body = "custom"`)))
	st, h, body = in.pub("GET", "/missing/x.html", nil, "")
	if st != 404 || body != "custom" || h.Get("Content-Length") != "6" {
		t.Fatalf("custom 404: %d %v %q", st, h, body)
	}
}

// A processor that passes the response through serves the stored bytes
// (which are the document's serialized form, LO-15).
func TestPassThroughKeepsStoredBytes(t *testing.T) {
	in := newInstance(t)
	src := "<!DOCTYPE html>\n<html><body>\n  <p   class=x>kept   as is</p>\n</body></html>\n"
	in.dav("PUT", "/d/doc.html", src)
	in.install("/r/p.html", processor("/d/*", "", "", sesselItem("action", `@schema Context url("https://pagelove.org/Context");
Context.response.status == 200`)))
	st, h, body := in.pub("GET", "/d/doc.html", nil, "")
	if want := "<!DOCTYPE html>\n<html><body>\n  <p class=\"x\">kept   as is</p>\n</body></html>\n"; st != 200 || body != want || h.Get("ETag") == "" {
		t.Fatalf("got %d %q %v", st, body, h)
	}
}

func TestOutboundWebhookAfterResponse(t *testing.T) {
	in := newInstance(t)
	in.install("/r/t.html", trigger("/blog/*", "PUT", `<div itemprop="action" itemscope itemtype="https://pagelove.org/HttpRequest">
<meta itemprop="url" content="`+in.sink.srv.URL+`/notify"><meta itemprop="body" content="updated">
<div itemprop="header" itemscope itemtype="https://pagelove.org/Pair"><meta itemprop="key" content="X-Multi"><meta itemprop="value" content="one"></div>
<div itemprop="header" itemscope itemtype="https://pagelove.org/Pair"><meta itemprop="key" content="X-Multi"><meta itemprop="value" content="two"></div>
<div itemprop="header" itemscope itemtype="https://pagelove.org/Pair"><meta itemprop="key" content="Bad Name"><meta itemprop="value" content="x"></div>
</div>`))
	if st, _, _ := in.pub("PUT", "/blog/p.html", nil, "<p>x</p>"); st >= 300 {
		t.Fatalf("put: %d", st)
	}
	got := in.sink.wait(t, "/notify", 1, 5*time.Second)
	if len(got) != 1 {
		t.Fatalf("sink got %d requests", len(got))
	}
	r := got[0]
	if r.method != "POST" || r.body != "updated" || r.header.Get("Content-Type") != "text/html" {
		t.Fatalf("request %+v", r)
	}
	if v := r.header.Values("X-Multi"); len(v) != 2 || v[0] != "one" || v[1] != "two" {
		t.Fatalf("X-Multi %v", v)
	}
	if r.header.Get("Bad Name") != "" {
		t.Fatalf("malformed header sent")
	}
}

func TestOutboundPrivateDestinationRefused(t *testing.T) {
	in := newInstance(t)
	// Another loopback listener that the site does not allow.
	other := newSink(t)
	in.install("/r/t.html", trigger("/d/*", "PUT", `<div itemprop="action" itemscope itemtype="https://pagelove.org/HttpRequest"><meta itemprop="url" content="`+other.srv.URL+`/x"></div>`))
	if st, _, _ := in.pub("PUT", "/d/x.html", nil, "<p>x</p>"); st >= 300 {
		t.Fatalf("put: %d", st)
	}
	if got := other.wait(t, "/x", 1, 1500*time.Millisecond); len(got) != 0 {
		t.Fatalf("a private destination outside the allowlist was contacted")
	}
}

const orderRules = `
<div itemscope itemtype="https://pagelove.org/TransitionConstraint"><meta itemprop="selector" content="[itemtype='https://t.test/Order']"><meta itemprop="property" content="status"><meta itemprop="to" content="pending"></div>
<div itemscope itemtype="https://pagelove.org/TransitionConstraint"><meta itemprop="selector" content="[itemtype='https://t.test/Order']"><meta itemprop="property" content="status"><meta itemprop="from" content="pending"><meta itemprop="to" content="processing"></div>`

func order(id, status string) string {
	return `<div id="` + id + `" itemscope itemtype="https://t.test/Order"><meta itemprop="status" content="` + status + `"></div>`
}

func TestTransitionConstraints(t *testing.T) {
	in := newInstance(t)
	in.install("/transitions/rules.html", orderRules)
	if st, _, b := in.pub("PUT", "/orders/o.html", nil, "<!DOCTYPE html><html><body>"+order("o1", "pending")+"</body></html>"); st >= 300 {
		t.Fatalf("seed: %d %s", st, b)
	}
	if st, _, _ := in.pub("PUT", "/orders/o.html", map[string]string{"Range": "selector=#o1"}, order("o1", "processing")); st != 206 {
		t.Fatalf("legal step: %d", st)
	}
	st, _, body := in.pub("PUT", "/orders/o.html", map[string]string{"Range": "selector=#o1"}, order("o1", "success"))
	if st != 422 || !strings.Contains(body, "https://pagelove.org/ConstraintViolation") || !strings.Contains(body, "transition(status)") ||
		!strings.Contains(body, "/transitions/rules.html") {
		t.Fatalf("illegal step: %d %s", st, body)
	}
	if strings.Contains(body, `itemprop="key"`) {
		t.Fatalf("keyless violation carries a key")
	}
	// Deleting needs an exit rule.
	if st, _, _ := in.pub("DELETE", "/orders/o.html", nil, ""); st != 422 {
		t.Fatalf("delete without exit rule: %d", st)
	}
	// WebDAV bypasses constraints (R-REACT-75).
	in.dav("DELETE", "/orders/o.html", "")
	// Entry without a rule.
	if st, _, _ := in.pub("PUT", "/orders/n.html", nil, "<!DOCTYPE html><html><body>"+order("n1", "shipped")+"</body></html>"); st != 422 {
		t.Fatalf("undeclared entry: %d", st)
	}
	// A malformed rule written through the serving path is refused.
	st, _, body = in.pub("PUT", "/transitions/bad.html", nil, `<!DOCTYPE html><html><body><div itemscope itemtype="https://pagelove.org/TransitionConstraint"><meta itemprop="selector" content="x"><meta itemprop="property" content="status"></div></body></html>`)
	if st != 422 || !strings.Contains(body, "https://pagelove.org/SchemaViolation") {
		t.Fatalf("malformed rule: %d %s", st, body)
	}
}

func TestTransitionKeyPairing(t *testing.T) {
	in := newInstance(t)
	in.install("/schema.html", `<div hidden itemscope itemtype="https://pagelove.org/Schema"><meta itemprop="type" content="https://t.test/Order">
<div itemprop="property" itemscope itemtype="https://pagelove.org/Property"><meta itemprop="name" content="orderNumber"><meta itemprop="unique" content="true"><meta itemprop="@key" content="true"></div></div>`)
	in.install("/transitions/rules.html", orderRules)
	keyed := func(n, s string) string {
		return `<div itemscope itemtype="https://t.test/Order"><meta itemprop="orderNumber" content="` + n + `"><meta itemprop="status" content="` + s + `"></div>`
	}
	doc := func(items ...string) string {
		return "<!DOCTYPE html><html><body>" + strings.Join(items, "") + "</body></html>"
	}
	if st, _, b := in.pub("PUT", "/orders/b.html", nil, doc(keyed("A", "pending"), keyed("B", "pending"))); st >= 300 {
		t.Fatalf("seed: %d %s", st, b)
	}
	if st, _, b := in.pub("PUT", "/orders/b.html", nil, doc(keyed("B", "pending"), keyed("A", "processing"))); st >= 300 {
		t.Fatalf("reordered legal step: %d %s", st, b)
	}
	st, _, body := in.pub("PUT", "/orders/b.html", nil, doc(keyed("A", "processing"), keyed("B", "shipped")))
	if st != 422 || !strings.Contains(body, `<span itemprop="key">B</span>`) {
		t.Fatalf("keyed violation: %d %s", st, body)
	}
}

func TestTransitionHandlerDelivery(t *testing.T) {
	in := newInstance(t)
	in.install("/r/h.html", `<div itemscope itemtype="https://pagelove.org/TransitionHandler">
<meta itemprop="selector" content="[itemtype='https://t.test/Order']"><meta itemprop="property" content="status"><meta itemprop="becomes" content="processing">
<div itemprop="action" itemscope itemtype="https://pagelove.org/HttpRequest"><meta itemprop="url" content="`+in.sink.srv.URL+`/payments"><meta itemprop="retry" content="3"></div></div>`)
	in.sink.mu.Lock()
	in.sink.status["/payments"] = []int{500}
	in.sink.mu.Unlock()
	if st, _, b := in.pub("PUT", "/orders/o.html", nil, "<!DOCTYPE html><html><body>"+order("o1", "pending")+"</body></html>"); st >= 300 {
		t.Fatalf("seed: %d %s", st, b)
	}
	if st, _, _ := in.pub("PUT", "/orders/o.html", map[string]string{"Range": "selector=#o1"}, order("o1", "processing")); st != 206 {
		t.Fatalf("step: %d", st)
	}
	got := in.sink.wait(t, "/payments", 1, 5*time.Second)
	if len(got) != 1 {
		t.Fatalf("deliveries: %d", len(got))
	}
	b := got[0].body
	for _, want := range []string{"https://pagelove.org/Transition", `content="/orders/o.html"`, `itemprop="body"`, `content="[itemtype=&quot;https://t.test/Order&quot;]"`, `content="processing"`} {
		if !strings.Contains(b, want) {
			t.Fatalf("transition document lacks %q:\n%s", want, b)
		}
	}
	// At most once: a failed delivery is not retried whatever retry says.
	if got := in.sink.wait(t, "/payments", 2, 3*time.Second); len(got) != 1 {
		t.Fatalf("delivery retried: %d", len(got))
	}
	// WebDAV edits never fire handlers.
	in.dav("PUT", "/orders/w.html", "<!DOCTYPE html><html><body>"+order("w1", "processing")+"</body></html>")
	if got := in.sink.wait(t, "/payments", 2, 1500*time.Millisecond); len(got) != 1 {
		t.Fatalf("WebDAV write fired a handler")
	}
}

func TestProcessorHeaderWrites(t *testing.T) {
	in := newInstance(t)
	in.install("/r/a.html", processor("/d/a*", "GET", "", sesselItem("action", `@schema Context url("https://pagelove.org/Context");
Context.response.headers = {"x-probe": "1"}`)))
	in.install("/r/b.html", processor("/d/b*", "GET", "", sesselItem("action", `@schema Context url("https://pagelove.org/Context");
Context.response.headers["x-probe"] = "1"`)))
	in.dav("PUT", "/d/a.html", "<!DOCTYPE html><html><body><h1>A</h1></body></html>")
	in.dav("PUT", "/d/b.html", "<!DOCTYPE html><html><body><h1>B</h1></body></html>")
	st, h, _ := in.pub("GET", "/d/a.html", nil, "")
	if st != 200 || h.Get("X-Probe") != "" {
		t.Fatalf("whole-map assignment: %d %v (want ignored)", st, h)
	}
	if st, _, _ := in.pub("GET", "/d/b.html", nil, ""); st != 500 {
		t.Fatalf("index assignment: %d, want a runtime error", st)
	}
}
