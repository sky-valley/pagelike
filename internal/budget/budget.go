// Package budget holds one request's resource budget, shared by every
// language runtime that runs while the request is served (Sessel, Liquid,
// server JavaScript). PageLove documents a single per-request budget with
// ops, memory and time axes, and reports consumption in response headers
// (live-observed 2026-09-28):
//
//	X-Budget-Consumed-Ops: 2
//	X-Budget-Consumed-Memory: 215
//	X-Budget-Consumed-Time: 93
//
// The Request type owns the shared deadline and hands each runtime its own
// accounting object bound to that deadline; Headers sums what they used.
package budget

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/sky-valley/pagelike/internal/liquid"
	"github.com/sky-valley/pagelike/internal/sessel"
)

// Defaults for one request.
const (
	DefaultTime   = 2 * time.Second
	DefaultOps    = sessel.DefaultMaxOps
	DefaultMemory = sessel.DefaultMaxMem
)

// Request is one request's budget.
type Request struct {
	Start    time.Time
	Deadline time.Time

	mu     sync.Mutex
	sessel *sessel.Budget
	liquid *liquid.Budget
	jsOps  int64 // ops and memory reported back by JS workers
	jsMem  int64
}

// New starts a request budget with the default limits.
func New() *Request {
	now := time.Now()
	return &Request{Start: now, Deadline: now.Add(DefaultTime)}
}

type ctxKey struct{}

// With attaches b to ctx.
func With(ctx context.Context, b *Request) context.Context {
	return context.WithValue(ctx, ctxKey{}, b)
}

// From returns the request budget in ctx, creating a detached one when the
// caller runs outside a request (background work, tests).
func From(ctx context.Context) *Request {
	if b, ok := ctx.Value(ctxKey{}).(*Request); ok && b != nil {
		return b
	}
	return New()
}

// Sessel returns the request's Sessel budget (shared by every Sessel
// evaluation in the request).
func (b *Request) Sessel() *sessel.Budget {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessel == nil {
		s := sessel.NewBudget()
		s.Deadline = b.Deadline
		b.sessel = s
	}
	return b.sessel
}

// Liquid returns the request's Liquid budget.
func (b *Request) Liquid() *liquid.Budget {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.liquid == nil {
		l := liquid.DefaultLimits()
		if rem := time.Until(b.Deadline); rem > 0 && rem < l.Time {
			l.Time = rem
		}
		b.liquid = liquid.NewBudget(l)
	}
	return b.liquid
}

// Remaining is the time left before the shared deadline (JS calls use it as
// their per-call deadline).
func (b *Request) Remaining() time.Duration {
	if d := time.Until(b.Deadline); d > 0 {
		return d
	}
	return 0
}

// AddJS records work reported by a JS worker call.
func (b *Request) AddJS(ops, mem int64) {
	b.mu.Lock()
	b.jsOps += ops
	b.jsMem += mem
	b.mu.Unlock()
}

// Consumed returns the ops, memory (bytes) and time (ms) used so far.
func (b *Request) Consumed() (ops, mem, ms int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ops, mem = b.jsOps, b.jsMem
	if b.sessel != nil {
		ops += b.sessel.Ops
		mem += b.sessel.Mem
	}
	if b.liquid != nil {
		u := b.liquid.Usage()
		ops += u.Work
		mem += u.Memory
	}
	return ops, mem, time.Since(b.Start).Milliseconds()
}

// SetHeaders writes the X-Budget-Consumed-* headers.
func (b *Request) SetHeaders(h http.Header) {
	ops, mem, ms := b.Consumed()
	h.Set("X-Budget-Consumed-Ops", strconv.FormatInt(ops, 10))
	h.Set("X-Budget-Consumed-Memory", strconv.FormatInt(mem, 10))
	h.Set("X-Budget-Consumed-Time", strconv.FormatInt(ms, 10))
}
