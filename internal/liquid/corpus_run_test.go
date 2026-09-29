package liquid

import (
	"context"
	"testing"
	"time"
)

// newTestEngine renders with the corpus clock. The work limit is the
// spike's 2,000,000 units rather than the 10,000,000 default, so the
// budget cases fail fast.
func newTestEngine() *Engine {
	return New(Options{Now: func() time.Time { return FixedNow }, Limits: Limits{Work: 2_000_000}})
}

func TestCorpus(t *testing.T) {
	e := newTestEngine()
	corpus := Corpus()
	if len(corpus) != 190 {
		t.Fatalf("corpus has %d cases, want 190", len(corpus))
	}
	seen := map[string]bool{}
	for _, ex := range corpus {
		if seen[ex.ID] {
			t.Fatalf("duplicate case %s", ex.ID)
		}
		seen[ex.ID] = true
		t.Run(ex.ID, func(t *testing.T) {
			data := M{}
			if ex.Data != nil {
				data = ex.Data()
			}
			out, err := e.Render(context.Background(), ex.Tpl, data, nil)
			if verr := ex.Verify(out, err); verr != nil {
				t.Error(verr)
			}
		})
	}
}
