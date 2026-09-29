package liquid

import (
	"embed"
	"net/http"
	"strings"
	"time"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/microdata"
)

// Test fixtures for the corpus (ported from the Liquid spike, docs/decisions/0002).
//
// testdata/ holds files from PageLove's MIT-licensed apps, copied unchanged
// with their licences: pagelove-polls@c9270e5 (site/polls/kfd47o4zqd.html,
// site/templates/new-poll.html) and pagelove-shop@d887054
// (site/data/products/*.html).

//go:embed testdata
var testdata embed.FS

func fixture(name string) string {
	b, err := testdata.ReadFile("testdata/" + name)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// FixedNow is the corpus clock, so "now" is reproducible.
var FixedNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

// M is a variable map.
type M = map[string]any

// itemsFromHTML parses a document and returns every item whose itemtype is
// itemtype, as a resource binding `[itemtype='…']` would find them.
func itemsFromHTML(path, doc, itemtype string) []any {
	root, err := dom.Parse([]byte(doc))
	if err != nil {
		panic(err)
	}
	var out []any
	for _, it := range microdata.OfType(root, itemtype) {
		out = append(out, NewItem(path, it.Node))
	}
	return out
}

const peopleHTML = `<!DOCTYPE html><html><body><ul>
<li itemscope itemtype="https://example.com/Person" id="ada"><span itemprop="name">Ada</span><meta itemprop="role" content="admin"><meta itemprop="nickname" content="Countess"></li>
<li itemscope itemtype="https://example.com/Person" id="bob"><span itemprop="name">Bob</span><meta itemprop="role" content="editor"></li>
<li itemscope itemtype="https://example.com/Person" id="cy"><span itemprop="name">Cy</span><meta itemprop="role" content="admin"><meta itemprop="nickname" content="Ace"></li>
<li itemscope itemtype="https://example.com/Person" id="dee"><span itemprop="name">Dee</span><meta itemprop="role" content="editor"><meta itemprop="nickname" content=""></li>
</ul></body></html>`

func people() []any { return itemsFromHTML("/people.html", peopleHTML, "https://example.com/Person") }

// anna is the Templating page's worked example: two documents, one Person each.
func anna() []any {
	a := itemsFromHTML("/sspi-tpl-anna.html", `<div itemscope itemtype="http://schema.org/Person" id="anna"><span itemprop="name">Anna</span></div>`, "http://schema.org/Person")
	b := itemsFromHTML("/sspi-tpl-ben.html", `<div itemscope itemtype="http://schema.org/Person" id="ben"><span itemprop="name">Ben</span></div>`, "http://schema.org/Person")
	return append(a, b...)
}

const teamHTML = `<!DOCTYPE html><html><body>
<section itemscope itemtype="http://example.com/TeamMember" id="zoe"><h2 itemprop="fullname">Zoe Zed</h2><span itemprop="email">zoe@example.com</span></section>
<section itemscope itemtype="http://example.com/TeamMember" id="adam"><h2 itemprop="fullname">Adam Ant</h2><span itemprop="email">adam@example.com</span></section>
</body></html>`

func team() []any { return itemsFromHTML("/team.html", teamHTML, "http://example.com/TeamMember") }

const productsHTML = `<!DOCTYPE html><html><body>
<div itemscope itemtype="https://example.com/Product" id="a1"><meta itemprop="sku" content="A-1"><h2 itemprop="title">Mug</h2><data itemprop="price" value="12.50">£12.50</data><meta itemprop="onsale" content="false"></div>
<div itemscope itemtype="https://example.com/Product" id="b2"><meta itemprop="sku" content="B-2"><h2 itemprop="title">Sticker</h2><data itemprop="price" value="3">£3</data><meta itemprop="onsale" content="true"></div>
<div itemscope itemtype="https://example.com/Product" id="c3"><meta itemprop="sku" content="C-3"><h2 itemprop="title">Cap</h2><data itemprop="price" value="22">£22</data><meta itemprop="onsale" content="false"></div>
</body></html>`

func products() []any {
	return itemsFromHTML("/products.html", productsHTML, "https://example.com/Product")
}

const usersHTML = `<!DOCTYPE html><html><body>
<div itemscope itemtype="https://example.com/User" id="ann"><span itemprop="name">Ann</span><meta itemprop="age" content="34"></div>
<div itemscope itemtype="https://example.com/User" id="ben"><span itemprop="name">Ben</span><meta itemprop="age" content="17"><meta itemprop="suspended" content="true"></div>
<div itemscope itemtype="https://example.com/User" id="cat"><span itemprop="name">Cat</span><meta itemprop="age" content="18"></div>
</body></html>`

func mdUsers() []any { return itemsFromHTML("/users.html", usersHTML, "https://example.com/User") }

// typedUsers is the same data as it arrives from a Sessel or JavaScript
// expression binding: real numbers and booleans, not microdata strings.
func typedUsers() []any {
	return []any{
		M{"name": "Ann", "age": 34},
		M{"name": "Ben", "age": 17, "suspended": true},
		M{"name": "Cat", "age": 18},
	}
}

const lineItemsHTML = `<!DOCTYPE html><html><body>
<table><tr itemscope itemtype="https://example.com/LineItem" id="l1"><td itemprop="price">12.50</td></tr>
<tr itemscope itemtype="https://example.com/LineItem" id="l2"><td itemprop="price">3</td></tr>
<tr itemscope itemtype="https://example.com/LineItem" id="l3"><td itemprop="price">8</td></tr></table>
</body></html>`

func lineItems() []any {
	return itemsFromHTML("/cart.html", lineItemsHTML, "https://example.com/LineItem")
}

const postsYearHTML = `<!DOCTYPE html><html><body>
<article itemscope itemtype="https://example.com/Post" id="p1"><h1 itemprop="title">One</h1><meta itemprop="year" content="2026"><time itemprop="date" datetime="2026-03-01">1 Mar</time></article>
<article itemscope itemtype="https://example.com/Post" id="p2"><h1 itemprop="title">Two</h1><meta itemprop="year" content="2025"><time itemprop="date" datetime="2025-11-20">20 Nov</time></article>
<article itemscope itemtype="https://example.com/Post" id="p3"><h1 itemprop="title">Three</h1><meta itemprop="year" content="2026"><time itemprop="date" datetime="2026-01-15">15 Jan</time></article>
</body></html>`

func yearPosts() []any {
	return itemsFromHTML("/posts.html", postsYearHTML, "https://example.com/Post")
}

const ordersHTML = `<!DOCTYPE html><html><body>
<div itemscope itemtype="https://example.com/Order" id="o1"><meta itemprop="ref" content="A1"><time itemprop="date" datetime="2025-12-30T10:00:00Z">30 Dec</time></div>
<div itemscope itemtype="https://example.com/Order" id="o2"><meta itemprop="ref" content="A2"><time itemprop="date" datetime="2026-01-05">5 Jan</time></div>
<div itemscope itemtype="https://example.com/Order" id="o3"><meta itemprop="ref" content="A3"><time itemprop="date" datetime="2025-06-01">1 Jun</time></div>
</body></html>`

func orders() []any { return itemsFromHTML("/orders.html", ordersHTML, "https://example.com/Order") }

const eventsHTML = `<!DOCTYPE html><html><body>
<div itemscope itemtype="https://example.com/Event" id="e1"><span itemprop="name">Launch</span><time itemprop="starts" datetime="2026-10-02T18:00:00Z">2 Oct</time></div>
<div itemscope itemtype="https://example.com/Event" id="e2"><span itemprop="name">Retro</span><time itemprop="starts" datetime="2026-09-01T18:00:00Z">1 Sep</time><meta itemprop="cancelled" content="yes"></div>
</body></html>`

func events() []any { return itemsFromHTML("/events.html", eventsHTML, "https://example.com/Event") }

// The build-a-blog tutorial's first post.
const helloWorldPost = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><title>data: hello-world</title></head>
<body>
  <article itemscope itemtype="https://blog.example/Post" id="post-hello-world">
    <meta itemprop="slug" content="hello-world">
    <meta itemprop="status" content="Published">
    <meta itemprop="excerpt" content="The first post on Field Notes, and a look at how this blog works.">
    <meta itemprop="displayDate" content="3 August 2026">
    <meta itemprop="monthLabel" content="August 2026">
    <h1 itemprop="title">Hello, world</h1>
    <p class="byline"><time itemprop="publishedAt" datetime="2026-08-03">3 August 2026</time> · <span itemprop="author">Sam</span></p>
    <div itemprop="body">
      <p>Welcome to Field Notes. This post is a single HTML file. The rest of the
      blog is about to build itself around it.</p>
    </div>
    <h2 class="comments-title">Comments</h2>
    <ul itemprop="comments" id="comments-hello-world" class="comment-list"></ul>
  </article>
</body>
</html>`

func blogPosts() []any {
	second := replaceAll(helloWorldPost,
		"hello-world", "second-thoughts",
		"Hello, world", "Second thoughts",
		"The first post on Field Notes, and a look at how this blog works.", "Why the blog has no build step & why that's fine.",
		"2026-08-03", "2026-08-05",
		"3 August 2026", "5 August 2026")
	draft := replaceAll(helloWorldPost,
		"hello-world", "draft-idea",
		"Published", "Draft",
		"Hello, world", "Not ready",
		"2026-08-03", "2026-08-09")
	var out []any
	out = append(out, itemsFromHTML("/data/posts/hello-world.html", helloWorldPost, "https://blog.example/Post")...)
	out = append(out, itemsFromHTML("/data/posts/second-thoughts.html", second, "https://blog.example/Post")...)
	out = append(out, itemsFromHTML("/data/posts/draft-idea.html", draft, "https://blog.example/Post")...)
	return out
}

func replaceAll(s string, pairs ...string) string {
	for i := 0; i+1 < len(pairs); i += 2 {
		s = strings.ReplaceAll(s, pairs[i], pairs[i+1])
	}
	return s
}

func pollItems() []any {
	var out []any
	out = append(out, itemsFromHTML("/polls/kfd47o4zqd.html", fixture("pagelove-polls/kfd47o4zqd.html"), "https://pagelove.org/Poll")...)
	// The template is on the same host; its itemtype is written as
	// {{ 'https://pagelove.org/Poll' }} precisely so the binding skips it.
	out = append(out, itemsFromHTML("/templates/new-poll.html", fixture("pagelove-polls/new-poll.html"), "https://pagelove.org/Poll")...)
	return out
}

func shopProducts() []any {
	var out []any
	for _, f := range []string{"cap", "hoodie", "mug", "stickers", "tee", "tote"} {
		out = append(out, itemsFromHTML("/data/products/"+f+".html", fixture("pagelove-shop/"+f+".html"), "https://shop.example/Product")...)
	}
	return out
}

// request is the documented `request` object, built with NewRequest.
func request(authed bool, change ...func(*RequestData)) *Request {
	d := RequestData{
		Method:   "GET",
		Path:     "/sspi-reqobj-page.html",
		RawQuery: "foo=bar",
		Header:   http.Header{"Accept": {"text/html"}},
		Host:     "field-notes.localhost",
	}
	if authed {
		d.Header.Set("Authorization", "Basic YWRtaW46eA==")
		d.Auth = &Auth{
			Username: "sub-ada",
			Claims:   map[string]any{"name": "Ada Lovelace", "email": "ada@example.com", "picture": "https://img.example/ada.png"},
			Roles:    []string{"admin", "users"},
		}
	}
	for _, fn := range change {
		fn(&d)
	}
	return NewRequest(d)
}
