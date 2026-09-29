package reactions

import (
	"strings"
	"testing"

	"github.com/sky-valley/pagelike/internal/dom"
	"github.com/sky-valley/pagelike/internal/site"
	"github.com/sky-valley/pagelike/internal/store"
)

func snapshotOf(t *testing.T, docs map[string]string) *site.Snapshot {
	t.Helper()
	snap := &site.Snapshot{Docs: map[string]*site.ParsedDoc{}}
	for p, body := range docs {
		snap.Docs[p] = site.Parse(&store.Document{Path: p, ContentType: "text/html", Body: []byte(body)})
		snap.Paths = append(snap.Paths, p)
	}
	sortStrings(snap.Paths)
	return snap
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func TestDiscovery(t *testing.T) {
	ix := buildIndex(snapshotOf(t, map[string]string{
		"/b.html": `<html><body>
<div itemscope itemtype="https://pagelove.org/Trigger"><meta itemprop="resource" content="/x/*"><div itemprop="action" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source">1</script></div></div>
<div itemscope itemtype="https://pagelove.org/Trigger"><meta itemprop="resource" content="/y/*"></div>
<template><div itemscope itemtype="https://pagelove.org/Trigger"><div itemprop="action" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source">1</script></div></div></template>
</body></html>`,
		"/a.html": `<html><body>
<div hidden itemscope itemtype="https://pagelove.org/Schema"><meta itemprop="type" content="https://t.test/Guard"><meta itemprop="parent" content="https://pagelove.org/Trigger"></div>
<div itemscope itemtype="https://t.test/Guard"><div itemprop="otherwise" itemscope itemtype="https://pagelove.org/Sessel"><script itemprop="source">1</script></div><meta itemprop="when" content="true"></div>
<div itemscope itemtype="https://t.test/Guard"><div itemprop="action" itemscope itemtype="https://pagelove.org/HTTPRequest"><meta itemprop="url" content="/hook"></div></div>
<div itemscope itemtype="https://pagelove.org/TransitionHandler"><meta itemprop="selector" content=".order, [itemtype='https://t.test/Order']"><meta itemprop="property" content="status"><meta itemprop="becomes" content="processing"><div itemprop="action" itemscope itemtype="https://pagelove.org/HttpRequest"><meta itemprop="url" content="/x"></div></div>
<div itemscope itemtype="https://pagelove.org/TransitionHandler"><meta itemprop="selector" content=".order"><meta itemprop="property" content="status"><meta itemprop="becomes" content="processing"><div itemprop="action" itemscope itemtype="https://pagelove.org/HttpRequest"><meta itemprop="url" content="/x"></div></div>
</body></html>`,
	}))
	if len(ix.triggers) != 2 {
		t.Fatalf("triggers: %d (malformed %d)", len(ix.triggers), ix.malformed)
	}
	if ix.triggers[0].path != "/a.html" || ix.triggers[1].path != "/b.html" {
		t.Fatalf("order: %s, %s", ix.triggers[0].path, ix.triggers[1].path)
	}
	if ix.triggers[0].actions[0].lang != "http" {
		t.Fatalf("the HTTPRequest alias was not recognized")
	}
	if len(ix.handlers) != 1 || len(ix.handlers[0].branches) != 1 {
		t.Fatalf("handlers: %d", len(ix.handlers))
	}
}

func TestTypeBranch(t *testing.T) {
	for _, c := range []struct {
		sel string
		ok  bool
	}{
		{`[itemtype='https://x.test/Order']`, true},
		{`[itemtype="https://x.test/Order"]`, true},
		{`:isa('https://x.test/Order')`, true},
		{`[itemtype='https://x.test/Order']:isa('https://x.test/Base')`, true},
		{`[itemtype*='Order']`, false},
		{`[itemtype='https://x.test/Order' i]`, false},
		{`.order`, false},
		{`div[itemtype='https://x.test/Order']`, false},
		{`body [itemtype='https://x.test/Order']`, false},
	} {
		if got := typeBranch(c.sel); got != c.ok {
			t.Errorf("typeBranch(%q) = %v, want %v", c.sel, got, c.ok)
		}
	}
}

func TestHandlerAccepts(t *testing.T) {
	snap := snapshotOf(t, map[string]string{"/h.html": `<html><body><div itemscope itemtype="https://pagelove.org/TransitionHandler">
<meta itemprop="selector" content="[itemtype='https://t.test/Order']"><meta itemprop="property" content="status"><meta itemprop="becomes" content="processing">
<div itemprop="action" itemscope itemtype="https://pagelove.org/HttpRequest"><meta itemprop="url" content="http://127.0.0.1:1/payments"><meta itemprop="retry" content="3"></div></div></body></html>`})
	ix := buildIndex(snap)
	if len(ix.handlers) != 1 {
		t.Fatalf("handlers: %d (malformed %d)", len(ix.handlers), ix.malformed)
	}
	root, _ := dom.Parse([]byte(`<html><body><div itemscope itemtype="https://t.test/Order"><meta itemprop="status" content="processing"></div></body></html>`))
	items := docItems(root)
	if len(items) != 1 || !ix.handlers[0].accepts(items[0]) {
		t.Fatalf("handler does not accept the order")
	}
	doc, _ := transitionDocument("/o.html", items[0], ix.types)
	if !strings.Contains(doc, `content="[itemtype=&quot;https://t.test/Order&quot;]"`) {
		t.Fatalf("selector: %s", doc)
	}
}

func TestBackoff(t *testing.T) {
	want := []int{2, 4, 8, 16, 30, 30}
	for i, w := range want {
		if got := backoff(i + 2).Seconds(); int(got) != w {
			t.Errorf("backoff(%d) = %v, want %ds", i+2, got, w)
		}
	}
}
