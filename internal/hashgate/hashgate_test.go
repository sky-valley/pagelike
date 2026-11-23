package hashgate

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/sky-valley/pagelike/internal/errdoc"
)

func busy(t *testing.T, err error) {
	t.Helper()
	var ed *errdoc.Error
	if !errors.As(err, &ed) || ed.Status != http.StatusServiceUnavailable || ed.Headers.Get("Retry-After") != "1" {
		t.Fatalf("want 503 with Retry-After: 1, got %v", err)
	}
}

func TestSiteShareIsHalfAndRefusedAtOnce(t *testing.T) {
	g := New(4)
	a := WithSite(context.Background(), "a")
	far := time.Now().Add(time.Hour)
	for range 2 {
		if _, err := g.Acquire(a, 0, 0, far); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	_, err := g.Acquire(a, 0, 0, far)
	busy(t, err)
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("a site at its share must be refused at once, not wait")
	}
	// Another site still gets the free slots.
	b := WithSite(context.Background(), "b")
	if _, err := g.Acquire(b, 0, 0, far); err != nil {
		t.Fatal(err)
	}
}

func TestOneSlotServerGivesEachSiteOne(t *testing.T) {
	g := New(1)
	rel, err := g.Acquire(WithSite(context.Background(), "a"), 0, 0, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Site b waits for the slot and gets it once a releases.
	done := make(chan error, 1)
	go func() {
		r, err := g.Acquire(WithSite(context.Background(), "b"), 0, 0, time.Now().Add(time.Hour))
		if r != nil {
			r()
		}
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	rel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWaitsOnlyWhileTimeCoversTheHash(t *testing.T) {
	g := New(2)
	other := WithSite(context.Background(), "o")
	for _, s := range []string{"x", "y"} {
		if _, err := g.Acquire(WithSite(context.Background(), s), 0, 0, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	_, err := g.Acquire(other, 0, 40*time.Millisecond, time.Now().Add(100*time.Millisecond))
	busy(t, err)
	if el := time.Since(start); el < 40*time.Millisecond || el > 90*time.Millisecond {
		t.Fatalf("waited %v; want about 60ms (time left minus the estimate)", el)
	}
}

func TestArgon2MemoryCaps(t *testing.T) {
	g := New(8)
	a := WithSite(context.Background(), "a")
	far := time.Now().Add(time.Hour)
	if _, err := g.Acquire(a, MaxSiteMemory, 0, far); err != nil {
		t.Fatal(err)
	}
	_, err := g.Acquire(a, 8, 0, far)
	busy(t, err) // past the site's 512 MiB
	if _, err := g.Acquire(WithSite(context.Background(), "b"), MaxSiteMemory, 0, far); err != nil {
		t.Fatal(err)
	}
	_, err = g.Acquire(WithSite(context.Background(), "c"), 8, 0, far)
	busy(t, err) // past the process's 1 GiB
}

func TestReleaseReturnsCapacity(t *testing.T) {
	g := New(2)
	a := WithSite(context.Background(), "a")
	rel, err := g.Acquire(a, MaxSiteMemory, 0, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	rel()
	rel() // idempotent
	if _, err := g.Acquire(a, MaxSiteMemory, 0, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}
