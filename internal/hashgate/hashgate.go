// Package hashgate shares the server's capacity for deliberately slow
// password hashes (bcrypt and Argon2) between sites, so one site cannot
// take every core or all the memory (R-LIQ-184).
//
// PageLove documents the policy (docs snapshot 2026-11-23, Hashing and
// security filters §How long hashing takes):
//   - the server runs as many hashes at once as it has processor cores;
//   - one site may use at most half of those slots, and at least one; a
//     site already at its share is refused at once;
//   - otherwise a hash waits for a free slot only while the time left still
//     covers the hash, and is refused when none comes free in time or the
//     site reaches its share while waiting;
//   - Argon2 memory in flight is capped at 1 GiB per process and 512 MiB per
//     site; a hash past either is refused;
//   - a refusal answers 503 with Retry-After: 1, and the page does not
//     render.
//
// Liquid's filters and Sessel's methods share one gate.
package hashgate

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/sky-valley/pagelike/internal/errdoc"
)

// Documented limits.
const (
	// NoBudgetWait is the time one hash may use when the request has no
	// time budget.
	NoBudgetWait = 5 * time.Second
	// MaxMemory and MaxSiteMemory cap Argon2 memory in flight, in KiB.
	MaxMemory     = 1 << 20 // 1 GiB
	MaxSiteMemory = 1 << 19 // 512 MiB
)

// Gate is a pool of hashing slots shared by sites.
type Gate struct {
	slots, share       int
	maxMem, maxSiteMem int64
	mu                 sync.Mutex
	used               int
	perSite            map[any]int
	mem                int64
	siteMem            map[any]int64
	freed              chan struct{} // closed and replaced on every release
}

// New returns a gate with n slots (n < 1 is one slot).
func New(n int) *Gate {
	if n < 1 {
		n = 1
	}
	return &Gate{slots: n, share: max(n/2, 1), maxMem: MaxMemory, maxSiteMem: MaxSiteMemory,
		perSite: map[any]int{}, siteMem: map[any]int64{}, freed: make(chan struct{})}
}

var defaultGate = New(runtime.NumCPU())

// Default is the process-wide gate.
func Default() *Gate { return defaultGate }

const busyKind = "ServiceUnavailable"

type siteKey struct{}

// WithSite names the site a request's hashes are charged to: any comparable
// value that identifies it (the server uses the *site.Site).
func WithSite(ctx context.Context, site any) context.Context {
	return context.WithValue(ctx, siteKey{}, site)
}

func siteOf(ctx context.Context) any {
	if ctx == nil {
		return nil
	}
	return ctx.Value(siteKey{})
}

// Busy is the refusal: 503 with Retry-After: 1. It is an *errdoc.Error so
// every runtime passes it up unchanged and no try/catch can swallow it.
func Busy(why string) *errdoc.Error {
	return errdoc.New(http.StatusServiceUnavailable, busyKind, "password hashing is busy (%s); try again", why).
		WithHeader("Retry-After", "1")
}

// Refused returns the refusal in err's chain, if any. Runtimes that wrap
// their failures check it first so the 503 reaches the client unchanged.
func Refused(err error) *errdoc.Error {
	var ed *errdoc.Error
	if errors.As(err, &ed) && ed.Kind == busyKind && ed.Headers.Get("Retry-After") == "1" {
		return ed
	}
	return nil
}

// Acquire takes a slot for a hash estimated to take est, using memKiB of
// Argon2 memory (0 for bcrypt), on behalf of the site named in ctx. A slot
// is waited for only while the time left before deadline still covers est
// (a zero deadline allows NoBudgetWait). The returned release must be
// called once the hash is done.
func (g *Gate) Acquire(ctx context.Context, memKiB int64, est time.Duration, deadline time.Time) (func(), error) {
	site := siteOf(ctx)
	if deadline.IsZero() {
		deadline = time.Now().Add(NoBudgetWait)
	}
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		g.mu.Lock()
		if g.perSite[site] >= g.share {
			g.mu.Unlock()
			return nil, Busy("this site is using its share of hashing slots")
		}
		if g.siteMem[site]+memKiB > g.maxSiteMem || g.mem+memKiB > g.maxMem {
			g.mu.Unlock()
			return nil, Busy("Argon2 memory in use")
		}
		if g.used < g.slots {
			g.used++
			g.perSite[site]++
			g.mem += memKiB
			g.siteMem[site] += memKiB
			g.mu.Unlock()
			return g.releaser(site, memKiB), nil
		}
		freed := g.freed
		g.mu.Unlock()
		wait := time.Until(deadline) - est
		if wait <= 0 {
			return nil, Busy("no slot came free in time")
		}
		if timer == nil {
			timer = time.NewTimer(wait)
		} else {
			timer.Reset(wait)
		}
		select {
		case <-freed:
		case <-timer.C:
			return nil, Busy("no slot came free in time")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (g *Gate) releaser(site any, memKiB int64) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.used--
			if g.perSite[site]--; g.perSite[site] <= 0 {
				delete(g.perSite, site)
			}
			g.mem -= memKiB
			if g.siteMem[site] -= memKiB; g.siteMem[site] <= 0 {
				delete(g.siteMem, site)
			}
			close(g.freed)
			g.freed = make(chan struct{})
			g.mu.Unlock()
		})
	}
}

// BcryptEstimate is how long bcrypt at cost takes: about 50 ms at cost 10,
// doubling with each step.
func BcryptEstimate(cost int) time.Duration {
	if cost <= 10 {
		return 50 * time.Millisecond >> (10 - cost)
	}
	return 50 * time.Millisecond << (cost - 10)
}

// Argon2Estimate is how long argon2id with memKiB and passes takes: about a
// microsecond per KiB per pass.
func Argon2Estimate(memKiB, passes int64) time.Duration {
	return time.Duration(memKiB*passes) * time.Microsecond
}
