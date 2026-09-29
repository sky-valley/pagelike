// Package sessel implements Sessel, PageLove's expression and query
// language (docs/spec/sessel.md): lexer, parser for the reconstructed
// grammar (R-SESSEL-30), a tree-walking evaluator, the standard library of
// every value type, Temporal, element construction, schema classes and the
// application/sessel+json encoding.
//
// The package is a language runtime: it imports only leaf packages (dom,
// htmlser, selector, microdata) and declares what it needs from the host as
// the Host interface. Programs are compiled once and cached by source text.
//
//	v, err := sessel.Eval(ctx, src, &sessel.Env{Host: h, Self: root, HasSelf: true})
//
// Errors are distinguishable: *ParseError (rejected before running),
// *Error with Type TypeError/RuntimeError/Error (runtime; Thrown for a user
// throw), and ResponseOf(err) for a thrown HTTPResponse.
package sessel

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	_ "time/tzdata" // Temporal time zones must not depend on the host OS

	"github.com/sky-valley/pagelike/internal/dom"
)

// Built-in type URLs (R-SESSEL-280).
const (
	URLContext      = "https://pagelove.org/Context"
	URLHTTPResponse = "https://pagelove.org/HTTPResponse"
	URLPair         = "https://pagelove.org/Pair"
	URLPlatform     = "https://pagelove.org/1.0"
	URLSessel       = "https://pagelove.org/Sessel"
	URLSelector     = "https://pagelove.org/Selector"
	URLInstance     = "https://pagelove.org/Instance"
	URLSchema       = "https://pagelove.org/Schema"
	URLProperty     = "https://pagelove.org/Property"
	URLMethod       = "https://pagelove.org/Method"
	URLParameter    = "https://pagelove.org/Parameter"
	URLLambda       = "https://pagelove.org/Sessel/Lambda"
)

// Program is a compiled Sessel program; it is immutable and safe to run
// concurrently.
type Program struct{ p *program }

// Source returns the program text.
func (p *Program) Source() string { return p.p.src }

// Declarations returns the program's @schema/@namespace declarations as
// name → URL pairs, in order.
func (p *Program) Declarations() [][3]string {
	out := make([][3]string, 0, len(p.p.decls))
	for _, d := range p.p.decls {
		out = append(out, [3]string{d.kind, d.name, d.url})
	}
	return out
}

type cacheEntry struct {
	prog *program
	err  error
}

var (
	progCache sync.Map // source → *cacheEntry
	progCount atomic.Int64
)

const progCacheMax = 8192

func compile(src string) (*program, error) {
	if e, ok := progCache.Load(src); ok {
		ce := e.(*cacheEntry)
		return ce.prog, ce.err
	}
	if len(src) > MaxProgramBytes {
		return nil, &ParseError{Msg: "program exceeds 256 KiB", Line: 1, Col: 1}
	}
	prog, err := parseProgram(src)
	var ce *cacheEntry
	if err != nil {
		ce = &cacheEntry{err: err}
	} else {
		ce = &cacheEntry{prog: prog}
	}
	if progCount.Add(1) > progCacheMax {
		progCache.Range(func(k, _ any) bool { progCache.Delete(k); return true })
		progCount.Store(0)
	}
	progCache.Store(src, ce)
	return ce.prog, ce.err
}

// Compile parses src (cached by source text). The error is a *ParseError.
func Compile(src string) (*Program, error) {
	p, err := compile(src)
	if err != nil {
		return nil, err
	}
	return &Program{p: p}, nil
}

// Eval compiles (cached) and evaluates src in env.
func Eval(ctx context.Context, src string, env *Env) (Value, error) {
	p, err := compile(src)
	if err != nil {
		return nil, err
	}
	return (&Program{p: p}).Eval(ctx, env)
}

// Eval runs the program in env. An internal failure of the interpreter is
// reported as a RuntimeError rather than crashing the host.
func (p *Program) Eval(ctx context.Context, env *Env) (v Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			v, err = nil, &Error{Type: RuntimeErrorType, Message: fmt.Sprintf("internal error: %v", r)}
		}
	}()
	if env == nil {
		env = &Env{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	b := env.Budget
	if b == nil {
		b = NewBudget()
		env.Budget = b
	}
	if env.Context == nil {
		env.Context = NewContext(env.Request)
	} else if env.Request != nil {
		if _, ok := env.Context.Get("request"); !ok {
			env.Context.Set("request", env.Request)
		}
	}
	if env.Mutable {
		// Working copies: a still-immutable element is detached first so a
		// shared snapshot is never modified; the host reads the results back
		// through the same *Element values.
		mark := func(el *Element) {
			if !el.Mutable {
				el.Node = dom.Clone(el.Node)
				el.Mutable = true
			}
		}
		if el, ok := env.Self.(*Element); ok {
			mark(el)
		}
		if l, ok := env.Self.(List); ok {
			for _, it := range l {
				if el, ok := it.(*Element); ok {
					mark(el)
				}
			}
		}
	}
	r := &run{ctx: ctx, budget: b, host: env.Host, cctx: env.Context}
	ev := newEvaluator(r, env, p.p)
	return ev.runProgram()
}

// now returns the host clock (or the wall clock).
func (ev *evaluator) now() time.Time {
	if ev.r.host != nil {
		return ev.r.host.Now()
	}
	return time.Now()
}
