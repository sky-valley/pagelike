package liquid

import (
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/osteele/liquid/parser"

	"github.com/sky-valley/pagelike/internal/dom"
)

func budgetError(t *testing.T, err error) *Error {
	t.Helper()
	var le *Error
	if !errors.As(err, &le) || le.Kind != KindBudget || le.Status() != 503 || !errors.Is(err, ErrBudget) {
		t.Fatalf("want a BudgetExceeded error, got %v", err)
	}
	return le
}

func TestBudget(t *testing.T) {
	e := New(Options{})
	ctx := context.Background()

	t.Run("work", func(t *testing.T) {
		_, err := e.Render(ctx, `{% for i in (1..1000) %}{% endfor %}`, nil, NewBudget(Limits{Work: 500}))
		budgetError(t, err)
	})
	t.Run("never-inline", func(t *testing.T) {
		// Exhaustion inside {{ }} fails the render; it is not degraded.
		_, err := e.Render(ctx, `<p>{{ (1..100000) | sort | size }}</p>`, nil, NewBudget(Limits{Work: 1000}))
		budgetError(t, err)
	})
	t.Run("output-memory", func(t *testing.T) {
		_, err := e.Render(ctx, `{% for i in (1..100) %}0123456789{% endfor %}`, nil, NewBudget(Limits{Memory: 500}))
		budgetError(t, err)
	})
	t.Run("capture-memory", func(t *testing.T) {
		_, err := e.Render(ctx, `{% capture c %}{% for i in (1..100) %}0123456789{% endfor %}{% endcapture %}`, nil, NewBudget(Limits{Memory: 500}))
		budgetError(t, err)
	})
	t.Run("assign-memory", func(t *testing.T) {
		_, err := e.Render(ctx, `{% assign s = "x" | append: big %}`, M{"big": strings.Repeat("y", 600)}, NewBudget(Limits{Memory: 500}))
		budgetError(t, err)
	})
	t.Run("push-is-linear", func(t *testing.T) {
		// Reassigning a growing list charges its growth only.
		b := NewBudget(Limits{})
		out, err := e.Render(ctx, `{% for i in (1..300) %}{% assign w = w | push: "abcdefgh" %}{% endfor %}{{ w.size }}`, nil, b)
		if err != nil || out != "300" {
			t.Fatalf("%q %v", out, err)
		}
		if m := b.Usage().Memory; m > 300*16+100 {
			t.Errorf("memory charged %d", m)
		}
	})
	t.Run("range-materialized", func(t *testing.T) {
		_, err := e.Render(ctx, `{{ (1..200000) | join }}`, nil, NewBudget(Limits{Memory: 1 << 20}))
		budgetError(t, err)
	})
	t.Run("lazy-range-loop", func(t *testing.T) {
		out, err := e.Render(ctx, `{% for i in (1..900000000) limit: 3 %}{{ i }}{% endfor %}`, nil, NewBudget(Limits{Work: 100}))
		if err != nil || out != "123" {
			t.Fatalf("%q %v", out, err)
		}
	})
	t.Run("time", func(t *testing.T) {
		_, err := e.Render(ctx, `{% for i in (1..100000000) %}{% endfor %}`, nil, NewBudget(Limits{Time: 20 * time.Millisecond}))
		le := budgetError(t, err)
		if !strings.Contains(le.Message, "time") {
			t.Errorf("message %q", le.Message)
		}
	})
	t.Run("shared-across-renders", func(t *testing.T) {
		b := NewBudget(Limits{Work: 50})
		for i := 0; ; i++ {
			_, err := e.Render(ctx, `{% for i in (1..10) %}{% endfor %}`, nil, b)
			if err != nil {
				budgetError(t, err)
				if i == 0 {
					t.Error("first render already exhausted the budget")
				}
				break
			}
		}
		// Once exhausted, every later render fails at once.
		_, err := e.Render(ctx, `x`, nil, b)
		budgetError(t, err)
		if u := b.Usage(); u.Work <= 50 {
			t.Errorf("usage %+v", u)
		}
	})
	t.Run("other-runtimes-charge", func(t *testing.T) {
		b := NewBudget(Limits{Work: 10})
		if err := b.Charge(9); err != nil {
			t.Fatal(err)
		}
		_, err := e.Render(ctx, `{{ 1 | plus: 1 }}{{ 2 }}`, nil, b)
		budgetError(t, err)
	})
	t.Run("cancelled", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := e.Render(cctx, `{% for i in (1..1000) %}{{ i }}{% endfor %}`, nil, nil)
		var le *Error
		if !errors.As(err, &le) || !errors.Is(err, context.Canceled) {
			t.Fatalf("want a cancelled render, got %v", err)
		}
	})
	t.Run("exp-depth-is-not-budget", func(t *testing.T) {
		out, err := e.Render(ctx, `{% assign p = "xs | where_exp: 'x', p" %}<p>{{ xs | where_exp: "u", p }}</p>`, M{"xs": []any{1}}, nil)
		if err != nil || !strings.Contains(out, "exceeds 32 levels") {
			t.Fatalf("%q %v", out, err)
		}
		_, err = e.Render(ctx, `{% assign p = "xs | where_exp: 'x', p" %}{% assign y = xs | where_exp: "u", p %}`, M{"xs": []any{1}}, nil)
		var le *Error
		if !errors.As(err, &le) || le.Kind != KindTemplate || !strings.Contains(le.Message, "32") {
			t.Fatalf("want a TemplateError naming the limit, got %v", err)
		}
	})
	t.Run("bounded-message", func(t *testing.T) {
		// Nested predicate failures must not grow the message per level.
		tpl := `{% assign p = "xs | where_exp: 'x', p | date: 'q'" %}<p>{{ xs | where_exp: "u", p }}</p>`
		out, err := e.Render(ctx, tpl, M{"xs": []any{1}}, nil)
		if err != nil || len(out) > 1000 {
			t.Fatalf("len %d, %v", len(out), err)
		}
	})
}

func TestRequest(t *testing.T) {
	e := newTestEngine()
	render := func(tpl string, r *Request) string {
		out, err := e.Render(context.Background(), tpl, M{"request": r}, nil)
		if err != nil {
			t.Fatalf("%s: %v", tpl, err)
		}
		return out
	}
	t.Run("anonymous", func(t *testing.T) {
		r := request(false)
		if got := render(`[{{ request.auth.username }}|{{ request.auth.claims.email }}|{{ request.auth.roles.size }}|{{ request.auth.role.size }}]{% if request.auth.username %}IN{% endif %}`, r); got != "[||0|0]" {
			t.Errorf("anonymous auth: %q", got)
		}
		if got := render(`{{ request.auth | json }}`, r); got != `{"username":null,"claims":{},"roles":[],"role":[]}` {
			t.Errorf("anonymous auth JSON: %s", got)
		}
	})
	t.Run("private", func(t *testing.T) {
		for tpl, private := range map[string]bool{
			`{{ request.path }}{{ request.query.foo }}{{ request.method }}{{ request.params.x }}`: false,
			`{{ request.auth.username }}`:                          true,
			`{{ request.headers['accept'] }}`:                      true,
			`{{ request["auth"] }}`:                                true,
			`{{ request | json }}`:                                 true,
			`{{ request }}`:                                        true,
			`{% for m in request %}{{ m[0] }}{% endfor %}`:         true,
			`{% assign r = request %}{{ r.size }}`:                 false,
			`{% if request.auth.claims.email == "x" %}{% endif %}`: true,
			`{{ request | map: "path" }}`:                          false,
		} {
			r := request(true)
			render(tpl, r)
			if r.Private() != private {
				t.Errorf("%s: private = %v, want %v", tpl, r.Private(), private)
			}
		}
	})
	t.Run("members", func(t *testing.T) {
		r := NewRequest(RequestData{
			Method: "post", Path: "/a b.html", RawQuery: "q=a+b&t=1&t=2&x=%zz&&e=",
			Header:      http.Header{"X-Probe": {"1", "2"}, "Accept-Language": {"en"}},
			Params:      []Param{{"slug", "hi there"}, {"id", "7"}},
			ContentType: "application/x-www-form-urlencoded; charset=utf-8", Body: []byte("title=A+%26+B&o=1&o=2"),
		})
		got := render(`{{ request.method }}|{{ request.path }}|{{ request.query.q }}|{{ request.query.t | join: "," }}|{{ request.query.x }}|[{{ request.query.e }}]|{{ request.headers.x-probe }}|{{ request.headers['accept-language'] }}|{{ request.params.slug }}|{{ request.params | json }}|{{ request.body.title }}|{{ request.body.o | join }}`, r)
		want := `POST|/a b.html|a b|1,2|%zz|[]|1, 2|en|hi there|{"slug":"hi there","id":"7"}|A & B|1 2`
		if got != want {
			t.Errorf("members:\n got %s\nwant %s", got, want)
		}
		if got := render(`[{{ request.body }}]|{{ request.query | json }}|{{ request.params | json }}`, NewRequest(RequestData{Method: "GET"})); got != "[]|{}|{}" {
			t.Errorf("empty request: %s", got)
		}
		if got := render(`{{ request.body }}`, NewRequest(RequestData{ContentType: "text/plain", Body: []byte("raw & text")})); got != "raw & text" {
			t.Errorf("raw body: %s", got)
		}
	})
	t.Run("from-http", func(t *testing.T) {
		var body strings.Builder
		mw := multipart.NewWriter(&body)
		mw.WriteField("name", "Ada")
		fw, _ := mw.CreateFormFile("file", "x.txt")
		fw.Write([]byte("ignored"))
		mw.Close()
		req := httptest.NewRequest("POST", "http://site.localhost/p%20q.html?x=1", strings.NewReader(body.String()))
		req.Header.Set("Content-Type", mw.FormDataContentType())
		r := RequestFromHTTP(req, []byte(body.String()), nil, &Auth{Username: "sub", Roles: []string{"users"}})
		got := render(`{{ request.path }}|{{ request.query.x }}|{{ request.headers.host }}|{{ request.body.name }}|[{{ request.body.file }}]|{{ request.auth.username }}|{{ request.auth.role | join }}`, r)
		if got != "/p q.html|1|site.localhost|Ada|[]|sub|users" {
			t.Errorf("from http: %s", got)
		}
	})
}

func TestItem(t *testing.T) {
	doc := `<!DOCTYPE html><html><body>
<div id="card" itemscope itemtype="https://ex/Card https://ex/Other" itemref="extra" data-x="1" class="c">
  <span itemprop="name"> Ada </span>
  <a itemprop="url" href="/ada">link</a>
  <img itemprop="photo" src="a.png">
  <time itemprop="born">1815</time>
  <data itemprop="n" value="3">three</data>
  <meta itemprop="size" content="XL">
  <div itemprop="pet" itemscope itemtype="https://ex/Pet" id="pet"><span itemprop="name">Rex</span></div>
  <span itemprop="tag">a</span><span itemprop="tag">b</span>
</div>
<p id="extra"><span itemprop="ref">from itemref</span></p>
<p id="plain">not <b>an</b> item</p>
</body></html>`
	root, err := dom.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	var card, plain *Item
	dom.Walk(root, func(n *dom.Node) bool {
		switch dom.AttrOr(n, "id", "") {
		case "card":
			card = NewItem("/people/ada.html", n)
		case "plain":
			plain = NewItem("/people/ada.html", n)
		}
		return true
	})
	runSpecCases(t, []specCase{
		{"values", `{{ c.name }}|{{ c.url }}|{{ c.photo }}|{{ c.born }}|{{ c.n }}|{{ c.ref }}`, M{"c": card}, " Ada |/ada|a.png|1815|3|from itemref"},
		{"reserved", `{{ c['@id'] }}|{{ c['@type'] }}|{{ c['@tag'] }}|{{ c['@attributes'].class }}|{{ c.pet['@id'] }}`, M{"c": card}, "/people/ada.html#card|https://ex/Card https://ex/Other|div|c|/people/ada.html#pet"},
		{"size-prop", `{{ c.size }}|{{ c | size }}|{{ c.pet.size }}`, M{"c": card}, "XL|XL|1"},
		{"multi", `{{ c.tag | join: "," }}|{{ c.tag.size }}|{{ c.name.size }}`, M{"c": card}, "a,b|2|5"},
		{"nested", `{{ c.pet.name }}|{{ c.pet }}`, M{"c": card}, "Rex|Rex"},
		{"missing", `{{ c.nope | default: "none" }}`, M{"c": card}, "none"},
		{"render", `[{{ p }}]`, M{"p": plain}, "[not an item]"},
		{"non-item", `[{{ p.name }}]{{ p['@id'] }}|{{ p.size }}|{{ p['@html'] }}`, M{"p": plain}, "[]/people/ada.html#plain|0|not <b>an</b> item"},
		{"identity", `{% assign a = cs | where: "name", " Ada " %}{% if a[0] == c %}same{% endif %}`, M{"c": card, "cs": []any{card, plain}}, "same"},
		{"truthy", `{% if p %}yes{% endif %}`, M{"p": plain}, "yes"},
	})
	out, _ := newTestEngine().Render(context.Background(), `{{ c | json }}`, M{"c": card}, nil)
	want := `{"@id":"/people/ada.html#card","@type":"https://ex/Card https://ex/Other","name":" Ada ","url":"/ada","photo":"a.png","born":"1815","n":"3","size":"XL","pet":{"@id":"/people/ada.html#pet","@type":"https://ex/Pet","name":"Rex"},"tag":["a","b"],"ref":"from itemref"}`
	if out != want {
		t.Errorf("json:\n got %s\nwant %s", out, want)
	}
	if got := strings.Join(card.Keys(), ","); got != "name,url,photo,born,n,size,pet,tag,ref" {
		t.Errorf("keys: %s", got)
	}
}

func TestXMLHost(t *testing.T) {
	root, err := dom.ParseXML([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"><entry itemscope itemtype="t" id="e1"><title itemprop="title">T</title></entry></feed>`))
	if err != nil {
		t.Fatal(err)
	}
	var feed, entry *dom.Node
	dom.Walk(root, func(n *dom.Node) bool {
		switch n.Data {
		case "feed":
			feed = n
		case "entry":
			entry = n
		}
		return true
	})
	host := HostOf(feed)
	if !host.XML || host.Tag != "feed" {
		t.Fatalf("HostOf: %+v", host)
	}
	e := newTestEngine()
	out, err := e.RenderFor(context.Background(), host, `<title>{{ x | date }}</title>{{ e.title }}|{{ e['@id'] }}`, M{"x": "bad", "e": NewItem("/feed.xml", entry)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `<meta itemprop="kind" content="TemplateError"/>`) || !strings.HasSuffix(out, "T|/feed.xml#e1") {
		t.Errorf("xml render: %s", out)
	}
	if h := HostOf(root.FirstChild); h.Tag != "feed" || !h.XML {
		t.Errorf("HostOf(feed) = %+v", h)
	}
}

func TestConcurrentRenders(t *testing.T) {
	e := New(Options{})
	posts := blogPosts()
	tpl := `{% assign recent = posts | where: "status", "Published" | sort: "publishedAt" | reverse %}{% for p in recent %}{{ p.title }};{% endfor %}{{ posts | where_exp: "p", "p.slug != 'x'" | size }}`
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for g := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 30 {
				// Vary the source so compiles and cache hits interleave.
				out, err := e.Render(context.Background(), tpl+fmt.Sprint(i%3), M{"posts": posts}, nil)
				if err != nil {
					errs <- err
					return
				}
				if want := "Second thoughts;Hello, world;3" + fmt.Sprint(i%3); out != want {
					errs <- fmt.Errorf("goroutine %d: got %q", g, out)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestCompileCache(t *testing.T) {
	e := New(Options{CacheSize: 2})
	for i := range 5 {
		if _, err := e.Render(context.Background(), fmt.Sprintf("{{ %d }}", i), nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if n := e.cache.order.Len(); n != 2 {
		t.Errorf("cache holds %d entries, want 2", n)
	}
	// Syntax errors are cached and returned again.
	_, err1 := e.Render(context.Background(), "{% if %}", nil, nil)
	_, err2 := e.Render(context.Background(), "{% if %}", nil, nil)
	if err1 == nil || err1 != err2 {
		t.Errorf("cached syntax error: %v / %v", err1, err2)
	}
	// The same source compiles differently for different hosts.
	a, _ := e.RenderFor(context.Background(), Host{}, `{{ x | date }}`, M{"x": "bad"}, nil)
	b, _ := e.RenderFor(context.Background(), Host{Tag: "title"}, `{{ x | date }}`, M{"x": "bad"}, nil)
	if a == b || b != "" {
		t.Errorf("host-specific compile: %q vs %q", a, b)
	}
	// A named HTML host decodes character references inside Liquid
	// markup; Host{} does not, and the two do not share a cache entry.
	c, _ := e.RenderFor(context.Background(), Host{}, `{{ "&amp;" | size }}`, nil, nil)
	d, _ := e.RenderFor(context.Background(), Host{Tag: "p"}, `{{ "&amp;" | size }}`, nil, nil)
	if c != "5" || d != "1" {
		t.Errorf("reference decoding by host: %q vs %q", c, d)
	}
	// Raw markup inside a Liquid string is part of the raw source and
	// renders (R-LIQ-10; live PageLove fails such a page, keep-standard),
	// and the references form gives the same result.
	for _, src := range []string{`{{ "<a href='x'>" | escape }}`, `{{ "&lt;a href='x'&gt;" | escape }}`} {
		if out, err := e.RenderFor(context.Background(), Host{Tag: "p"}, src, nil, nil); err != nil || out != "&lt;a href=&#39;x&#39;&gt;" {
			t.Errorf("%s = %q, %v", src, out, err)
		}
	}
}

func TestPanicsAreContained(t *testing.T) {
	e := newTestEngine()
	for _, tpl := range []string{
		`{{ xs | sort_natural: "missing" }}`,
		`{% assign y = xs | where_exp: "x", "x |" %}`,
		`{{ (1..x) }}`,
	} {
		_, err := e.Render(context.Background(), tpl, M{"xs": []any{M{"a": 1}, 2}, "x": "not a number"}, nil)
		if err != nil && strings.Contains(err.Error(), "goroutine") {
			t.Errorf("%s leaked a stack trace: %v", tpl, err)
		}
	}
}

func BenchmarkBlog500(b *testing.B) {
	var sb strings.Builder
	for i := range 500 {
		fmt.Fprintf(&sb, `<article itemscope itemtype="https://blog.example/Post" id="p%d"><meta itemprop="slug" content="p%d"><meta itemprop="status" content="Published"><meta itemprop="excerpt" content="Excerpt %d"><meta itemprop="displayDate" content="d"><h1 itemprop="title">Post %d</h1><time itemprop="publishedAt" datetime="2026-%02d-%02d">x</time><span itemprop="author">Sam</span></article>`, i, i, i, i, i%12+1, i%28+1)
	}
	posts := itemsFromHTML("/posts.html", sb.String(), "https://blog.example/Post")
	tpl := `{% assign recent = posts | where: "status", "Published" | sort: "publishedAt" | reverse %}
{% for post in recent limit: 50 %}<article><h2><a href="/posts/{{ post.slug }}.html">{{ post.title }}</a></h2><p>{{ post.displayDate }} · {{ post.author }}</p><p>{{ post.excerpt }}</p></article>{% endfor %}`
	e := New(Options{})
	vars := M{"posts": posts}
	for b.Loop() {
		if _, err := e.Render(context.Background(), tpl, vars, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompile measures preprocessing and compiling the polls template
// (what a compile-cache miss costs).
func BenchmarkCompile(b *testing.B) {
	inst := New(Options{}).newInstance()
	src := htmlInner(fixture("pagelove-polls/new-poll.html"))
	for b.Loop() {
		pre, err := preprocess(src, Host{})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := inst.cfg.Compile(pre, parser.SourceLoc{LineNo: 1}); err != nil {
			b.Fatal(err)
		}
	}
}
