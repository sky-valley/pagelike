package compose_test

import (
	"fmt"
	"strings"
	"testing"
)

// BenchmarkComposeLargePage reads a 5,000-row page with an include, a
// binding, a stamp and a template: the walk copies the untouched rows.
func BenchmarkComposeLargePage(b *testing.B) {
	p := newPlane(b)
	p.allow("GET")
	p.author("/nav.html", `<html><body><nav id="nav">Nav</nav></body></html>`)
	var rows strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&rows, "<tr id=\"r%d\"><td>%d</td><td>row &amp; %d</td></tr>\n", i, i, i)
	}
	p.author("/big.html", `<!DOCTYPE html><html xmlns:p="https://pagelove.org/1.0" xmlns:e="https://pagelove.org/Binding/Sessel"><body e:n="'N'">
<p:include selector="#nav" resource="/nav.html"></p:include><h1><p:stamp n></p:stamp></h1>
<p p:template="text/liquid">{{ 2 | plus: 3 }}</p>
<table><tbody>`+rows.String()+`</tbody></table></body></html>`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if r := p.do("GET", "/big.html", "", nil); r.status != 200 || !strings.Contains(r.body, "row &amp; 4999") {
			b.Fatalf("%d", r.status)
		}
	}
}
