package identity

import (
	"sync"
	"time"
)

// Throttle bounds failed sign-in attempts per key (a site and client
// address) in a fixed window, limiting online password guessing without
// letting anyone lock a named account out.
type Throttle struct {
	Max    int
	Window time.Duration

	mu    sync.Mutex
	fails map[string]*window
}

type window struct {
	start time.Time
	n     int
}

// maxThrottleKeys bounds memory; beyond it expired windows are dropped and,
// if that is not enough, the table restarts.
const maxThrottleKeys = 10000

// NewThrottle returns a throttle allowing max failures per window.
func NewThrottle(max int, per time.Duration) *Throttle {
	return &Throttle{Max: max, Window: per, fails: map[string]*window{}}
}

// Allow reports whether key may attempt a sign-in now.
func (t *Throttle) Allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	w := t.fails[key]
	return w == nil || time.Since(w.start) > t.Window || w.n < t.Max
}

// Fail records a failed attempt.
func (t *Throttle) Fail(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	w := t.fails[key]
	if w == nil || now.Sub(w.start) > t.Window {
		if len(t.fails) >= maxThrottleKeys {
			for k, v := range t.fails {
				if now.Sub(v.start) > t.Window {
					delete(t.fails, k)
				}
			}
			if len(t.fails) >= maxThrottleKeys {
				t.fails = map[string]*window{}
			}
		}
		w = &window{start: now}
		t.fails[key] = w
	}
	w.n++
}

// Reset forgets key's failures (after a successful sign-in).
func (t *Throttle) Reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.fails, key)
}
