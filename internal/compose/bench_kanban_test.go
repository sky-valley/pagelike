package compose_test

import (
	"fmt"
	"strings"
	"testing"
)

// BenchmarkComposeKanbanBoard composes a ~1 MB board document shaped like
// the kanban app's (docs/spec/apps.md ACC-KB-13, R-APPS-17): one Liquid
// directive under a non-`p` prefix and 400 cards with descriptions.
func BenchmarkComposeKanbanBoard(b *testing.B) {
	p := newPlane(b)
	p.allow("GET")
	var cards strings.Builder
	desc := strings.Repeat("lorem ipsum dolor sit amet ", 85)
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&cards, `<article class="card" id="C-%d" itemscope itemtype="urn:board:Card" draggable="true">
  <div class="card-title" data-f="card-title" itemprop="name">Card %d</div>
  <meta data-f="card-home" content=""><meta data-f="card-labels" content=""><meta data-f="card-due" content="">
  <div class="card-desc" data-f="card-desc" hidden>%s</div>
</article>
`, i, i, desc)
	}
	p.author("/board.html", `<!DOCTYPE html>
<html lang="en" xmlns:pagelove="https://pagelove.org/1.0"><head><title>Big</title></head>
<body class="board" itemscope itemtype="urn:board:Board">
<div id="whoami-server" hidden pagelove:template="text/liquid">{{ request.auth.username }}|{{ request.auth.claims.name }}|{% for r in request.auth.role %}{{ r }} {% endfor %}</div>
<main id="lists"><section class="list" id="L-1"><div class="cards" id="L-1-cards">`+cards.String()+`</div></section></main>
</body></html>`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if r := p.do("GET", "/board.html", "", nil); r.status != 200 || !strings.Contains(r.body, `id="C-399"`) {
			b.Fatalf("%d", r.status)
		}
	}
}

// BenchmarkSelectorPutKanbanBoard replaces one card title on the same ~1 MB
// board (ACC-KB-13's selector write).
func BenchmarkSelectorPutKanbanBoard(b *testing.B) {
	p := newPlane(b)
	p.allow("GET", "PUT")
	var cards strings.Builder
	desc := strings.Repeat("lorem ipsum dolor sit amet ", 85)
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&cards, `<article class="card" id="C-%d" itemscope itemtype="urn:board:Card"><div class="card-title" data-f="card-title" itemprop="name">Card %d</div><div class="card-desc" data-f="card-desc" hidden>%s</div></article>
`, i, i, desc)
	}
	p.author("/board.html", `<!DOCTYPE html>
<html lang="en" xmlns:pagelove="https://pagelove.org/1.0"><head><title>Big</title></head>
<body class="board" itemscope itemtype="urn:board:Board">
<div id="whoami-server" hidden pagelove:template="text/liquid">{{ request.auth.username }}</div>
<main id="lists"><section class="list" id="L-1"><div class="cards" id="L-1-cards">`+cards.String()+`</div></section></main>
</body></html>`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sel := fmt.Sprintf(`#C-%d [data-f="card-title"]`, i%400)
		body := fmt.Sprintf(`<div class="card-title" data-f="card-title" itemprop="name">Renamed %d</div>`, i)
		if r := p.do("PUT", "/board.html", body, map[string]string{"Range": "selector=" + sel, "Content-Type": "text/html"}); r.status >= 300 {
			b.Fatalf("%d %s", r.status, r.body)
		}
	}
}
