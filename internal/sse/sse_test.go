package sse

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/sky-valley/pagelike/internal/store"
)

// Echo suppression (docs/spec/sse.md R-SSE-37..41).
func TestSuppressed(t *testing.T) {
	tab := &Subscriber{Session: "s1", Conn: "tokA"}
	for _, c := range []struct {
		name string
		ev   store.Event
		want bool
	}{
		{"same session, no token: every tab skipped", store.Event{OriginSession: "s1"}, true},
		{"same session, this tab's token", store.Event{OriginSession: "s1", OriginConn: "tokA"}, true},
		{"same session, another tab's token", store.Event{OriginSession: "s1", OriginConn: "tokB"}, false},
		{"same session, unknown token", store.Event{OriginSession: "s1", OriginConn: "bogus"}, false},
		// A token names its stream whoever presents it (live 2026-09-28).
		{"other session presenting this tab's token", store.Event{OriginSession: "s2", OriginConn: "tokA"}, true},
		{"sessionless write presenting this tab's token", store.Event{OriginConn: "tokA"}, true},
		{"other session", store.Event{OriginSession: "s2"}, false},
		{"no session (authoring, server-originated)", store.Event{}, false},
	} {
		if got := Suppressed(tab, c.ev); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMutationArticle(t *testing.T) {
	m := Mutation{Method: "POST", Selector: `main > ul[data-x="a&b"]`, ETag: `"abc"`, Path: "/p.html", Host: "t.example",
		Body: `<tr id="r"><td>x</td></tr>`, Placement: "append"}
	got := m.Render()
	want := `<article itemscope itemtype="https://pagelove.org/Mutation">
  <span itemprop="method">POST</span>
  <span itemprop="selector">main > ul[data-x="a&amp;b"]</span>
  <span itemprop="etag">"abc"</span>
  <span itemprop="path">/p.html</span>
  <span itemprop="host">t.example</span>
  <div itemprop="body"><tr id="r"><td>x</td></tr></div>
  <span itemprop="placement">append</span>
</article>`
	if got != want {
		t.Fatalf("article:\n%s\nwant:\n%s", got, want)
	}
	// The body is the last div, as pagelove-polls slices it.
	if !regexp.MustCompile(`(?s)</tr></div>(\s*<span itemprop="[a-z]+">[^<]*</span>)*\s*</article>$`).MatchString(got) {
		t.Error("only spans may follow the body")
	}
	whole := Mutation{Method: "DELETE", Path: "/p.html"}.Render()
	if !WholeDocument(whole) || strings.Contains(whole, "etag") || WholeDocument(got) {
		t.Errorf("whole-document detection:\n%s", whole)
	}
}

func TestWriteEventFraming(t *testing.T) {
	var b bytes.Buffer
	WriteEvent(&b, "17-3", "mutation", "a\r\nb\rc\n")
	if want := "id: 17-3\nevent: mutation\ndata: a\ndata: b\ndata: c\ndata: \n\n"; b.String() != want {
		t.Errorf("framing %q, want %q", b.String(), want)
	}
	b.Reset()
	WriteEvent(&b, "", "pagelove-connection", NewToken())
	if !regexp.MustCompile(`^event: pagelove-connection\ndata: [0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\n\n$`).MatchString(b.String()) {
		t.Errorf("connection event %q", b.String())
	}
	if NewToken() == NewToken() {
		t.Error("tokens repeat")
	}
}

// Event ids have PageLove's shape; other shapes are not placed.
func TestEventIDs(t *testing.T) {
	ev := store.Event{Path: "/doc.html", TimeMS: 1790634867705, Seq: 7}
	id := EventID(ev)
	if !regexp.MustCompile(`^v1~[0-9a-f]{12}\.1790634867705-7$`).MatchString(id) {
		t.Fatalf("id %q", id)
	}
	if ms, seq, ok := ParseEventID(id); !ok || ms != 1790634867705 || seq != 7 {
		t.Errorf("ParseEventID(%q) = %d %d %v", id, ms, seq, ok)
	}
	for _, bad := range []string{"1000-0", "not-an-event-id", "v1~xyz.1-2", "v1~0123456789ab", "v1~0123456789ab.x-1"} {
		if _, _, ok := ParseEventID(bad); ok {
			t.Errorf("ParseEventID(%q) placed it", bad)
		}
	}
}

func TestSlowSubscriberDropped(t *testing.T) {
	b := NewBroker()
	slow := b.SubscribeN("/p", "s", 2)
	fast := b.SubscribeN("/p", "t", 8)
	for i := 0; i < 3; i++ {
		b.Publish([]store.Event{{Path: "/p", Seq: int64(i), Data: "x"}})
		fast.Received(<-fast.C)
	}
	select {
	case <-slow.Dropped:
	default:
		t.Fatal("a full subscriber must be dropped")
	}
	select {
	case <-fast.Dropped:
		t.Fatal("a draining subscriber was dropped")
	default:
	}
	// The byte bound counts too.
	big := b.SubscribeN("/q", "s", 8)
	b.Publish([]store.Event{{Path: "/q", Data: strings.Repeat("x", BufferBytes-10)}, {Path: "/q", Data: strings.Repeat("y", 20)}})
	select {
	case <-big.Dropped:
	default:
		t.Fatal("the byte bound was not enforced")
	}
}
