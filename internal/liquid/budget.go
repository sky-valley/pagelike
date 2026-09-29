package liquid

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ErrBudget is the budget-exhaustion error. A render that exhausts any axis
// fails as a whole, never degrading inline and never returning a partial
// page (R-LIQ-212); composition answers 503 BudgetExceeded.
var ErrBudget = errors.New("budget exhausted")

// Limits are the axes of a request budget (R-LIQ-210 … R-LIQ-213). A zero
// field takes the default from DefaultLimits.
type Limits struct {
	// Work is the number of work units: at least one per executed tag,
	// output and filter call, one per loop iteration, and one per element a
	// filter walks.
	Work int64
	// Memory is the number of bytes of template output plus values stored
	// by assign and capture.
	Memory int64
	// Time is the wall time for the whole request, measured from NewBudget.
	Time time.Duration
}

// DefaultLimits are pagelike's per-request defaults (R-LIQ-213). They make
// no compatibility claim; PageLove's numbers are unknown.
func DefaultLimits() Limits {
	return Limits{Work: 10_000_000, Memory: 16 << 20, Time: 2 * time.Second}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.Work == 0 {
		l.Work = d.Work
	}
	if l.Memory == 0 {
		l.Memory = d.Memory
	}
	if l.Time == 0 {
		l.Time = d.Time
	}
	return l
}

// Usage is what a budget has consumed, for the X-Budget-Consumed-* response
// headers (R-LIQ-214).
type Usage struct {
	Work    int64
	Memory  int64
	Elapsed time.Duration
}

// Budget is one request's allowance. Composition creates one per request
// and passes it to every Render of that request, so all of a page's
// templates draw on one allowance (R-LIQ-210). Other runtimes (Sessel,
// bindings) may charge the same Budget through Charge and ChargeMemory.
//
// The first render that uses a Budget also fixes the request instant that
// "now" and "today" mean, so every template of a request agrees on it
// (R-LIQ-170). A Budget is safe for concurrent use.
type Budget struct {
	limits Limits
	start  time.Time

	work atomic.Int64
	mem  atomic.Int64
	err  atomic.Pointer[error]

	nowOnce sync.Once
	now     time.Time
}

// NewBudget starts a request budget; the time axis runs from now.
func NewBudget(l Limits) *Budget {
	return &Budget{limits: l.withDefaults(), start: time.Now()}
}

// Limits returns the budget's limits (defaults filled in).
func (b *Budget) Limits() Limits { return b.limits }

// Charge spends work units and checks the time limit.
func (b *Budget) Charge(units int64) error {
	if err := b.Err(); err != nil {
		return err
	}
	if w := b.work.Add(units); w > b.limits.Work {
		return b.fail(fmt.Errorf("%w: work limit of %d units exceeded", ErrBudget, b.limits.Work))
	}
	if time.Since(b.start) > b.limits.Time {
		return b.fail(fmt.Errorf("%w: time limit of %s exceeded", ErrBudget, b.limits.Time))
	}
	return nil
}

// ChargeMemory spends bytes of the memory axis.
func (b *Budget) ChargeMemory(bytes int64) error {
	if err := b.Err(); err != nil {
		return err
	}
	if m := b.mem.Add(bytes); m > b.limits.Memory {
		return b.fail(fmt.Errorf("%w: memory limit of %d bytes exceeded", ErrBudget, b.limits.Memory))
	}
	return nil
}

// Err returns the exhaustion error, or nil while the budget lasts. Once
// exhausted, a budget stays exhausted.
func (b *Budget) Err() error {
	if p := b.err.Load(); p != nil {
		return *p
	}
	return nil
}

func (b *Budget) fail(err error) error {
	b.err.CompareAndSwap(nil, &err)
	return b.Err()
}

// Usage reports consumption so far.
func (b *Budget) Usage() Usage {
	return Usage{Work: b.work.Load(), Memory: b.mem.Load(), Elapsed: time.Since(b.start)}
}

// requestNow fixes the request instant on first use.
func (b *Budget) requestNow(clock func() time.Time) time.Time {
	b.nowOnce.Do(func() { b.now = clock().UTC() })
	return b.now
}
