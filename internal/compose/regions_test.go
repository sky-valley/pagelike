package compose

import (
	"testing"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// PageLove's model does not foster-parent (LO-15), so markup inside
// <table> is walked where it was written and the region keeps its bytes.
func TestTableContentWalkedInPlace(t *testing.T) {
	src := `<html><body><table><div id="f">foster</div><tr><td>a</td></tr></table></body></html>`
	r, err := newDocRegion([]byte(src), "text/html", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.text != src || !walkable(r) {
		t.Fatalf("region text %q walkable=%v", r.text, walkable(r))
	}
}

// A tag the tokenizer sees in text the parser keeps as text (a raw-text
// host's output) and reconstructed formatting elements do not make a
// region unwalkable, so its bytes are kept.
func TestRegionWalkableDespiteUncleanSpans(t *testing.T) {
	for _, src := range []string{
		`<html><body><p id="o">{{ "<a href='x'>" | escape }}</p>` + "\n" + `</body></html>`,
		`<html><body><b>1<i>2</b>3</i></body></html>`,
	} {
		r, err := newDocRegion([]byte(src), "text/html", nil, false)
		if err != nil {
			t.Fatal(err)
		}
		if r.text != src {
			t.Errorf("rewritten: %q -> %q", src, r.text)
		}
	}
	fr, err := fragmentRegion(`var d = ["<b>&"];`, &html.Node{Type: html.ElementNode, Data: "script", DataAtom: atom.Script}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !walkable(fr) {
		t.Error("script output is not walkable")
	}
}

func TestIndexPath(t *testing.T) {
	r, err := newDocRegion([]byte(`<html><body><ul><li>a</li><li id="b">b</li></ul></body></html>`), "text/html", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	var li *html.Node
	for n := range r.spans.By {
		if attr(n, "id") == "b" {
			li = n
		}
	}
	idx := indexPath(r.root, li)
	if got := nodeAt(r.root, idx); got != li {
		t.Fatalf("round trip %v", idx)
	}
	if indexPath(r.root, &html.Node{Type: html.ElementNode}) != nil {
		t.Error("a detached node has no path")
	}
}
