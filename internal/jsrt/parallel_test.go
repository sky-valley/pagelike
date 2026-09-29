package jsrt

import (
	"context"
	"testing"
	"time"
)

// TestParallelPrepare runs DOM and non-DOM evaluations back to back on a
// worker that prepares contexts on a second goroutine.
func TestParallelPrepare(t *testing.T) {
	r := New(Options{Size: 1, ParallelPrepare: true})
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for i := 0; i < 20; i++ {
		src := `export default (v) => v + 1;`
		var doc *Document
		if i%3 == 0 {
			src = `export default (v) => document.querySelectorAll("li").length + v;`
			doc = &Document{HTML: "<ul><li>1</li><li>2</li></ul>"}
		}
		res, f := r.CallModule(ctx, CallRequest{Slot: SlotRead, Source: src, Args: []Value{i}, Document: doc})
		if f != nil {
			t.Fatal(f)
		}
		want := int64(i + 1)
		if doc != nil {
			want = int64(i + 2)
		}
		if res.Value != want {
			t.Fatalf("call %d: got %v", i, res.Value)
		}
	}
}
