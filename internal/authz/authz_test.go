package authz

import (
	"fmt"
	stdhtml "html"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/identity"
	"github.com/sky-valley/pagelike/internal/microdata"
	"github.com/sky-valley/pagelike/internal/selector"
)

// rule renders one meta-form rule; empty selector omitted.
func rule(actor, resource, method, sel, action string) string {
	s := `<div itemscope itemtype="https://pagelove.org/AuthorizationRule">`
	for _, a := range strings.Split(actor, "|") {
		s += fmt.Sprintf(`<meta itemprop="actor" content="%s">`, stdhtml.EscapeString(a))
	}
	s += fmt.Sprintf(`<meta itemprop="resource" content="%s">`, stdhtml.EscapeString(resource))
	for _, m := range strings.Split(method, "|") {
		s += fmt.Sprintf(`<meta itemprop="method" content="%s">`, stdhtml.EscapeString(m))
	}
	if sel != "" {
		s += fmt.Sprintf(`<meta itemprop="selector" content="%s">`, stdhtml.EscapeString(sel))
	}
	return s + fmt.Sprintf(`<meta itemprop="action" content="%s"></div>`, stdhtml.EscapeString(action))
}

func policyOf(t *testing.T, defaultGet bool, body ...string) *Policy {
	t.Helper()
	root, err := dom.Parse([]byte("<!DOCTYPE html><html><body>" + strings.Join(body, "\n") + "</body></html>"))
	if err != nil {
		t.Fatal(err)
	}
	rules, groups := ExtractRules("/rules.html", root)
	p := &Policy{Rules: rules, Groups: groups, DefaultGet: defaultGet}
	p.RunHooks(nil)
	return p
}

var (
	anon  = &identity.Principal{Session: "s"}
	alice = &identity.Principal{Authenticated: true, Sub: "sub-alice", Email: "alice@example.com", EmailVerified: true}
	bob   = &identity.Principal{Authenticated: true, Sub: "sub-bob", Email: "bob@example.com", EmailVerified: true}
	// mallory claims alice's address without verifying it.
	mallory = &identity.Principal{Authenticated: true, Sub: "sub-mallory", Email: "alice@example.com"}
	ops     = &identity.Principal{Authenticated: true, Sub: "sub-ops", Email: "ops@example.com", Roles: []string{"admins", "staff"}}
)

func allowed(p *Policy, pr *identity.Principal, method, path string) bool {
	return p.Decide(Request{Principal: pr, Method: method, Path: path}, nil).Allowed
}

func TestTiersAndConflicts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules []string
		pr    *identity.Principal
		want  bool
	}{
		{"no rule denies writes", nil, alice, false},
		{"star allow", []string{rule("*", "/d.html", "PUT", "", "Allow")}, anon, true},
		{"user beats star deny", []string{rule("*", "/d.html", "PUT", "", "Deny"), rule("sub-alice", "/d.html", "PUT", "", "Allow")}, alice, true},
		{"users beats star deny", []string{rule("*", "/d.html", "PUT", "", "Deny"), rule("users", "/d.html", "PUT", "", "Allow")}, alice, true},
		{"users does not match anonymous", []string{rule("*", "/d.html", "PUT", "", "Deny"), rule("users", "/d.html", "PUT", "", "Allow")}, anon, false},
		{"authenticated alias", []string{rule("authenticated", "/d.html", "PUT", "", "Allow")}, bob, true},
		{"deny wins at equal tier", []string{rule("users", "/d.html", "PUT", "", "Allow"), rule("alice@example.com", "/d.html", "PUT", "", "Deny")}, alice, false},
		{"user allow beats member deny", []string{rule("alice@example.com", "/d.html", "PUT", "", "Deny"), rule("sub-alice", "/d.html", "PUT", "", "Allow")}, alice, true},
		{"email is exact", []string{rule("Alice@Example.com", "/d.html", "PUT", "", "Allow")}, alice, false},
		{"unverified email never matches", []string{rule("alice@example.com", "/d.html", "PUT", "", "Allow")}, mallory, false},
		{"display name is not a user name", []string{rule("Alice", "/d.html", "PUT", "", "Allow")}, &identity.Principal{Authenticated: true, Sub: "x", Name: "Alice"}, false},
		{"username token is user tier", []string{rule(":username", "/d.html", "PUT", "", "Deny"), rule("sub-alice", "/d.html", "PUT", "", "Allow")}, alice, false},
		{"username token beats users deny", []string{rule("users", "/d.html", "PUT", "", "Deny"), rule(":username", "/d.html", "PUT", "", "Allow")}, alice, true},
		{"username token needs a principal", []string{rule("${request.auth.username}", "/d.html", "PUT", "", "Allow")}, anon, false},
		{"oidc role as bare actor", []string{rule("admins", "/d.html", "PUT", "", "Allow")}, ops, true},
		{"role: form", []string{rule("role:staff", "/d.html", "PUT", "", "Allow")}, ops, true},
		{"role: form includes users", []string{rule("role:users", "/d.html", "PUT", "", "Allow")}, bob, true},
		{"several actors OR", []string{rule("sub-x|sub-bob", "/d.html", "PUT", "", "Allow")}, bob, true},
		{"templated actor is literal, not *", []string{rule("${request.headers.x-actor}", "/d.html", "PUT", "", "Allow")}, anon, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := policyOf(t, true, tc.rules...)
			req := Request{Principal: tc.pr, Method: "PUT", Path: "/d.html", Header: http.Header{"X-Actor": {"*"}}}
			if got := p.Decide(req, nil).Allowed; got != tc.want {
				t.Errorf("allowed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOrderIndependent(t *testing.T) {
	a := rule("users", "/d.html", "PUT", "", "Allow")
	d := rule("users", "/d.html", "PUT", "", "Deny")
	if allowed(policyOf(t, true, a, d), alice, "PUT", "/d.html") || allowed(policyOf(t, true, d, a), alice, "PUT", "/d.html") {
		t.Fatal("deny must win regardless of order")
	}
}

func TestGroups(t *testing.T) {
	grp := `<div itemscope itemtype="https://pagelove.org/Group"><meta itemprop="name" content="editors">
		<meta itemprop="member" content="alice@example.com"></div>`
	grp2 := `<div itemscope itemtype="https://pagelove.org/Group"><meta itemprop="name" content="editors">
		<meta itemprop="member" content="bob@example.com"></div>`
	blank := `<div itemscope itemtype="https://pagelove.org/Group"><meta itemprop="name" content="  ">
		<meta itemprop="member" content="alice@example.com"></div>`
	legacy := `<table><tr itemscope itemtype="https://pagelove.org/GroupMembership"><td itemprop="actor">alice@example.com</td><td itemprop="group">admins</td></tr></table>`
	p := policyOf(t, true, grp, grp2, blank, legacy,
		rule("editors", "/d.html", "PUT", "", "Allow"),
		rule("admins", "/a.html", "PUT", "", "Allow"),
		rule("  ", "/d.html", "DELETE", "", "Allow"))
	if !allowed(p, alice, "PUT", "/d.html") || !allowed(p, bob, "PUT", "/d.html") {
		t.Error("members of either editors item must be granted (union)")
	}
	if allowed(p, mallory, "PUT", "/d.html") {
		t.Error("unverified email must not be a member")
	}
	if allowed(p, alice, "PUT", "/a.html") {
		t.Error("GroupMembership items are not a membership source")
	}
	if allowed(p, alice, "DELETE", "/d.html") {
		t.Error("blank actor/name must not match")
	}
	if got, want := p.RoleList(alice), []string{"alice@example.com", "editors", "users"}; !reflect.DeepEqual(got, want) {
		t.Errorf("RoleList = %v, want %v", got, want)
	}
	if got := p.RoleList(anon); len(got) != 0 {
		t.Errorf("anonymous RoleList = %v", got)
	}
	// A schema hook binds includes().
	p.Groups = append(p.Groups, Group{Name: "example-staff", Includes: func(e string) bool { return strings.HasSuffix(e, "@example.com") }})
	p.Groups = append(p.Groups, Group{Name: "broken", Includes: func(string) bool { panic("boom") }})
	p.Rules = append(p.Rules, policyOf(t, true, rule("example-staff", "/s.html", "PUT", "", "Allow"), rule("broken", "/b.html", "PUT", "", "Allow")).Rules...)
	if !allowed(p, bob, "PUT", "/s.html") || allowed(p, mallory, "PUT", "/s.html") {
		t.Error("includes() must decide membership for verified emails only")
	}
	if allowed(p, bob, "PUT", "/b.html") {
		t.Error("a failing includes() must fail closed")
	}
}

func TestDefaultGetAndMethods(t *testing.T) {
	p := policyOf(t, true, rule("*", "/g.html", "GET", "", "Deny"), rule("*", "/h.html", "HEAD", "", "Allow"), rule("*", "/w.html", "*", "", "Allow"))
	if !allowed(p, anon, "GET", "/free.html") || !allowed(p, anon, "HEAD", "/free.html") {
		t.Error("default-GET allow must grant unmatched reads")
	}
	if allowed(p, anon, "PUT", "/free.html") || allowed(p, anon, "MOVE", "/free.html") || allowed(p, anon, "QUERY", "/free.html") {
		t.Error("default-GET must never grant other methods")
	}
	if p.Decide(Request{Principal: anon, Method: "GET", Path: "/free.html", Subscribe: true}, nil).Allowed {
		t.Error("SSE subscriptions are never default-granted")
	}
	if allowed(p, anon, "HEAD", "/g.html") {
		t.Error("GET rules cover HEAD")
	}
	if !allowed(p, anon, "HEAD", "/h.html") {
		t.Error("HEAD rule grants HEAD")
	}
	for _, m := range []string{"GET", "PUT", "MOVE", "PATCH", "QUERY", "PROPFIND"} {
		if !allowed(p, anon, m, "/w.html") {
			t.Errorf("* must grant %s", stdhtml.EscapeString(m))
		}
	}
	strict := policyOf(t, false)
	if allowed(strict, anon, "GET", "/free.html") {
		t.Error("default-GET deny must refuse unmatched reads")
	}
}

func TestExtractionForms(t *testing.T) {
	table := `<table itemscope itemtype="https://pagelove.org/AuthorizationRule">
	<thead><tr><th>Actor</th><th>Resource</th><th>Method</th><th>Selector</th><th>Action</th></tr></thead>
	<tbody>
	<tr><td itemprop="actor">*</td><td itemprop="resource">/d.html</td><td itemprop="method">PUT</td><td itemprop="selector">h1</td><td itemprop="action">Allow</td></tr>
	<tr><td itemprop="actor">*</td><td itemprop="resource">/d.html</td><td><ul><li itemprop="method">DELETE</li><li itemprop="method">POST</li></ul></td><td itemprop="selector">.item</td><td itemprop="action">allow</td></tr>
	</tbody></table>`
	p := policyOf(t, true, table)
	if len(p.Rules) != 2 {
		t.Fatalf("table form: %d rules, want 2", len(p.Rules))
	}
	if got := p.Rules[1].Methods; !reflect.DeepEqual(got, []string{"DELETE", "POST"}) {
		t.Errorf("row 2 methods = %v", got)
	}
	doc := mustParse(t, `<h1>T</h1><p class="item">x</p>`)
	h1, item := find(doc, "h1"), find(doc, ".item")
	put := Request{Principal: anon, Method: "PUT", Path: "/d.html"}
	if !p.Decide(put, h1).Allowed || p.Decide(put, item).Allowed {
		t.Error("rows must not merge")
	}

	odd := policyOf(t, true,
		rule("*", "/c.html", "GET, PUT", "", "allow"),   // comma list is one bogus token
		rule("*", "/m.html", "PUT", "", "maybe"),        // unknown action
		rule("*", "  /t.html  ", "  put ", "", "ALLOW"), // trimmed, case-insensitive (the action is not trimmed)
		`<div itemscope itemtype="https://pagelove.org/AuthorizationRule"><meta itemprop="actor" content="*"><meta itemprop="resource" content="/two.html"><meta itemprop="method" content="PUT"><meta itemprop="action" content="allow"><meta itemprop="action" content="deny"></div>`,
		`<div itemscope itemtype="http://pagelove.org/AuthorizationRule"><meta itemprop="actor" content="*"><meta itemprop="resource" content="/http.html"><meta itemprop="method" content="PUT"><meta itemprop="action" content="allow"></div>`,
		`<template>`+rule("*", "/tpl.html", "PUT", "", "allow")+`</template>`,
	)
	for path, want := range map[string]bool{"/c.html": false, "/m.html": false, "/t.html": true, "/two.html": false, "/http.html": false, "/tpl.html": false} {
		if got := allowed(odd, anon, "PUT", path); got != want {
			t.Errorf("PUT %s = %v, want %v", path, got, want)
		}
	}
}

func TestMoveNormalization(t *testing.T) {
	p := policyOf(t, true,
		rule("*", "/a.html", "MOVE|PUT", ".card", "Allow"), // discarded in full
		rule("*", "/b.html", "MOVE|PUT", ".card", "Deny"),  // widened, all methods
		rule("*", "/b.html", "PUT|MOVE", "", "Allow"),
		rule("*", "/c.html", "*", ".card", "Allow"), // * is not MOVE
	)
	doc := mustParse(t, `<article class="card">x</article>`)
	card := find(doc, ".card")
	if p.Decide(Request{Principal: anon, Method: "PUT", Path: "/a.html"}, card).Allowed {
		t.Error("an Allow MOVE rule with a selector must grant nothing")
	}
	if p.Decide(Request{Principal: anon, Method: "PUT", Path: "/b.html"}, nil).Allowed || allowed(p, anon, "MOVE", "/b.html") {
		t.Error("a Deny MOVE rule with a selector must refuse the whole document")
	}
	if !p.Decide(Request{Principal: anon, Method: "PUT", Path: "/c.html"}, card).Allowed {
		t.Error("* with a selector is a valid element rule")
	}
}

func TestSelectorScope(t *testing.T) {
	doc := mustParse(t, `<ul id="todo-list"><li class="t"><input type="checkbox"></li></ul><p class="secret">s</p><p id="pub">p</p>`)
	li, input, secret, pub := find(doc, "li"), find(doc, "input"), find(doc, ".secret"), find(doc, "#pub")
	p := policyOf(t, true,
		rule("*", "/d.html", "PUT", "#todo-list li", "Allow"),
		rule("*", "/d.html", "GET", ".secret", "Deny"),
		rule("*", "/x.html", "PUT", "", "Deny"),
		rule("*", "/x.html", "PUT", "#pub", "Allow"),
		rule("*", "/bad.html", "PUT", "h1[", "Allow"),
		rule("*", "/bad.html", "DELETE", "h1[", "Deny"),
		rule("*", "/bad.html", "DELETE", "", "Allow"),
	)
	req := func(m, path string) Request { return Request{Principal: anon, Method: m, Path: path} }
	if !p.Decide(req("PUT", "/d.html"), li).Allowed {
		t.Error("rule selector evaluated in document context")
	}
	if p.Decide(req("PUT", "/d.html"), input).Allowed {
		t.Error("an element inside a matching element does not match")
	}
	if p.Decide(req("PUT", "/d.html"), nil).Allowed {
		t.Error("a selector rule never covers the whole document")
	}
	if !p.Decide(req("GET", "/d.html"), nil).Allowed {
		t.Error("a selector-scoped GET deny does not affect whole-document reads")
	}
	if !p.DecideAll(req("GET", "/d.html"), []*html.Node{pub}).Allowed || p.DecideAll(req("GET", "/d.html"), []*html.Node{pub, secret}).Allowed {
		t.Error("all-matches reads are all-or-nothing")
	}
	if p.Decide(req("PUT", "/x.html"), pub).Allowed {
		t.Error("resource-level deny and selector allow share one candidate set")
	}
	if p.Decide(req("PUT", "/bad.html"), pub).Allowed {
		t.Error("an Allow with an unparseable selector grants nothing")
	}
	if p.Decide(req("DELETE", "/bad.html"), nil).Allowed {
		t.Error("a Deny with an unparseable selector is widened")
	}
}

func TestTemplates(t *testing.T) {
	p := policyOf(t, true,
		rule("*", "/q/${request.query.slot}.html", "PUT", "", "allow"),
		rule("*", "/h/${request.headers.x-name}.html", "PUT", "", "allow"),
		rule("users", "/u/:username/*", "PUT", "", "allow"),
		rule("users", "/r/${request.auth.roles}x.html", "PUT", "", "allow"),
		rule("users", "/e/${auth.claims.email}.html", "PUT", "", "allow"),
		rule("*", "/l/{{ request.auth.username }}.html", "PUT", "", "allow"),
		rule("*", "/p/doc.html", "PUT|DELETE", `[data-m="${request.method}"]`, "allow"),
	)
	do := func(pr *identity.Principal, method, path string, q url.Values, h http.Header, target *html.Node) bool {
		return p.Decide(Request{Principal: pr, Method: method, Path: path, Query: q, Header: h}, target).Allowed
	}
	if !do(anon, "PUT", "/q/a.html", url.Values{"slot": {"a"}}, nil, nil) || do(anon, "PUT", "/q/a.html", url.Values{"slot": {"b"}}, nil, nil) {
		t.Error("query lookup")
	}
	if do(anon, "PUT", "/h/zz.html", nil, http.Header{"X-Name": {"*"}}, nil) {
		t.Error("a substituted * must be literal in a resource")
	}
	if !do(anon, "PUT", "/h/*.html", nil, http.Header{"X-Name": {"*"}}, nil) {
		t.Error("a substituted * matches itself")
	}
	if !do(anon, "PUT", "/h/a/b.html", nil, http.Header{"X-Name": {"a/b"}}, nil) {
		t.Error("a substituted slash spans segments")
	}
	claims := &identity.Principal{Authenticated: true, Sub: "a*", Email: "alice@example.com", EmailVerified: true, Roles: []string{"staff"},
		Claims: map[string]any{"email": "alice@example.com"}}
	if !do(claims, "PUT", "/u/a*/doc.html", nil, nil, nil) || do(claims, "PUT", "/u/abc/doc.html", nil, nil, nil) {
		t.Error(":username is literal inside a resource")
	}
	if !do(claims, "PUT", "/r/x.html", nil, nil, nil) || do(claims, "PUT", "/r/staffx.html", nil, nil, nil) {
		t.Error("a list lookup renders empty")
	}
	if !do(claims, "PUT", "/e/alice@example.com.html", nil, nil, nil) {
		t.Error("bare auth.claims spelling")
	}
	if do(anon, "PUT", "/l/.html", nil, nil, nil) || !do(anon, "PUT", "/l/{{ request.auth.username }}.html", nil, nil, nil) {
		t.Error("Liquid braces are literal")
	}
	doc := mustParse(t, `<p id="p1" data-m="PUT">a</p><p id="p2" data-m="DELETE">b</p>`)
	if !do(anon, "PUT", "/p/doc.html", nil, nil, find(doc, "#p1")) || do(anon, "PUT", "/p/doc.html", nil, nil, find(doc, "#p2")) || !do(anon, "DELETE", "/p/doc.html", nil, nil, find(doc, "#p2")) {
		t.Error("request.method in a selector")
	}
}

func TestLookup(t *testing.T) {
	req := &Request{Principal: alice, Method: "GET", HTTPMethod: "QUERY", Path: "/p.html", RawQuery: "b=2&a=1",
		Query: url.Values{"a": {"1"}}, Header: http.Header{"X-Multi": {"a", "b"}}}
	for expr, want := range map[string]string{
		"request.method": "QUERY", "method": "QUERY", "request.path": "/p.html", "path": "/p.html",
		"request.query_string": "b=2&a=1", "request.query.a": "1", "query.a": "1", "request.query.zz": "",
		"request.headers.x-multi": "a, b", "request.headers": "", "request.auth.sub": "sub-alice",
		"request.auth.username": "sub-alice", "username": "sub-alice", "request.auth.email": "alice@example.com",
		"request.auth.roles": "", "request.auth": "", "nope": "",
	} {
		if got := Lookup(expr, req); got != want {
			t.Errorf("Lookup(%q) = %q, want %q", expr, got, want)
		}
	}
	if got := Lookup("request.auth.email", &Request{Principal: anon}); got != "" {
		t.Errorf("anonymous auth lookup = %q", got)
	}
}

func TestDirectoryAndOptions(t *testing.T) {
	p := policyOf(t, true,
		rule("*", "/app/", "PUT", "h1", "allow"),
		rule("*", "/b/index.html", "PUT", "", "allow"),
		rule("sub-alice", "/o.html", "PUT|POST", "ul  >  li", "allow"),
		rule("*", "/o.html", "GET", "", "allow"),
	)
	doc := mustParse(t, `<h1>x</h1>`)
	// A rule on the directory URL grants nothing on its index document
	// (live 2026-09-28); a Deny written that way still refuses it.
	if p.Decide(Request{Principal: anon, Method: "PUT", Path: "/app/index.html"}, find(doc, "h1")).Allowed {
		t.Error("an Allow on the directory URL granted its index document")
	}
	deny := policyOf(t, true, rule("*", "/d/*", "GET", "", "allow"), rule("*", "/d/", "GET", "", "deny"))
	if allowed(deny, anon, "GET", "/d/index.html") {
		t.Error("a Deny on the directory URL must refuse its index document")
	}
	// Padded cells are trimmed except the action: a padded "allow" grants
	// nothing (as on PageLove), a padded "deny" still refuses (fail closed).
	padded := policyOf(t, false,
		rule("  *\n", "\n /pa.html ", " PUT\n", "", "\n allow\n"),
		rule("*", "/pd.html", "PUT", "", "Allow"),
		rule("*", "/pd.html", "PUT", "", "\n deny\n"),
		rule("  *\n", "\n /ok.html ", " PUT\n", "", "allow"))
	if allowed(padded, anon, "PUT", "/pa.html") || allowed(padded, anon, "PUT", "/pd.html") || !allowed(padded, anon, "PUT", "/ok.html") {
		t.Error("padded action handling")
	}
	// "/x/*" also governs the bare name "/x" (live 2026-09-28).
	bare := policyOf(t, false, rule("*", "/x/*", "PUT", "", "allow"))
	if !allowed(bare, anon, "PUT", "/x") || allowed(bare, anon, "PUT", "/xy.html") || !allowed(bare, anon, "PUT", "/x/a.html") {
		t.Error("/x/* must cover /x and /x/a.html but not /xy.html")
	}
	if !allowed(p, anon, "PUT", "/b/index.html") {
		t.Error("index rule")
	}
	if got := p.SelectorRules(Request{Principal: anon, Method: "GET", Path: "/o.html"}); len(got) != 0 {
		t.Errorf("anonymous must not see alice's selector rules: %v", got)
	}
	if got := p.SelectorRules(Request{Principal: alice, Method: "GET", Path: "/o.html"}); !reflect.DeepEqual(got, []string{"ul  >  li"}) {
		t.Errorf("SelectorRules = %v", got)
	}
	if !p.DecideText(Request{Principal: alice, Method: "POST", Path: "/o.html"}, "ul > li").Allowed {
		t.Error("OPTIONS selector text equality normalizes whitespace")
	}
	if p.DecideText(Request{Principal: alice, Method: "POST", Path: "/o.html"}, "").Allowed {
		t.Error("document-level OPTIONS ignores selector rules")
	}
}

// TestHookContributesGroupSubtype plays the schema package's part: items of
// a Group subtype become groups, one with includes() bound.
func TestHookContributesGroupSubtype(t *testing.T) {
	type host struct{ root *html.Node }
	RegisterHook("authz-test-subtypes", func(h any, p *Policy) {
		hs, ok := h.(*host)
		if !ok {
			return
		}
		for _, it := range microdataOfType(hs.root, "https://example.com/TeamGroup") {
			g, ok := GroupFromItem("/groups.html", it)
			if !ok {
				continue
			}
			domain := trimASCII(it.Get("domain"))
			g.Includes = func(email string) bool { return strings.HasSuffix(email, domain) }
			p.Groups = append(p.Groups, g)
		}
	})
	root := mustParse(t, `<div itemscope itemtype="https://example.com/TeamGroup"><span itemprop="name">example-staff</span>
		<meta itemprop="domain" content="@example.com"></div>`+rule("example-staff", "/d.html", "PUT", "", "Allow"))
	rules, groups := ExtractRules("/x.html", root)
	p := &Policy{Rules: rules, Groups: groups}
	p.RunHooks(&host{root})
	if !allowed(p, alice, "PUT", "/d.html") || allowed(p, mallory, "PUT", "/d.html") {
		t.Error("subtype includes() must grant verified emails at the domain only")
	}
}

func mustParse(t *testing.T, body string) *html.Node {
	t.Helper()
	root, err := dom.Parse([]byte("<!DOCTYPE html><html><body>" + body + "</body></html>"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func find(root *html.Node, sel string) *html.Node {
	return selector.MustCompile(sel).MatchFirst(root)
}

func microdataOfType(root *html.Node, t string) []*microdata.Item { return microdata.OfType(root, t) }
