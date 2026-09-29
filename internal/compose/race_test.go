package compose_test

import (
	"sync"
	"testing"
)

// Concurrent reads share the snapshot's parse and cached regions; nothing
// they share is written (run with -race).
func TestConcurrentReads(t *testing.T) {
	p := newPlane(t)
	p.allow("GET")
	p.author("/data/a.html", `<html><body><article id="a" itemscope itemtype="urn:t:R"><h1 itemprop="t">A</h1></article></body></html>`)
	p.author("/page.html", `<html xmlns:p="https://pagelove.org/1.0" xmlns:e="https://pagelove.org/Binding/Sessel" xmlns:r="https://pagelove.org/Binding/CSS">
<body e:rec="${[itemtype='urn:t:R']}.first()"><main><p:stamp rec></p:stamp></main>
<p:include selector="#a h1" resource="/data/*"></p:include>
<ul r:rs="[itemtype='urn:t:R']" p:template="text/liquid" p:paginate="1">{% for x in rs %}<li>{{ x.t }}</li><li>2</li>{% endfor %}</ul></body></html>`)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		client := *p
		client.cookies = map[string]string{}
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				r := client.do("GET", "/page.html?paginate:page=2", "", nil)
				if r.status != 200 {
					t.Errorf("status %d", r.status)
					return
				}
			}
		}()
	}
	wg.Wait()
}
