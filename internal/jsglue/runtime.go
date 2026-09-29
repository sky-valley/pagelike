package jsglue

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/sky-valley/pagelike/internal/budget"
	"github.com/sky-valley/pagelike/internal/jsrt"
)

var (
	workers atomic.Int64
	rtMu    sync.Mutex
	rt      *jsrt.Runtime
)

// SetWorkers sets the size of the process-wide worker pool (0 = GOMAXPROCS).
// It takes effect when the runtime starts (the first JavaScript evaluation
// of the process); later calls are ignored.
func SetWorkers(n int) {
	if n < 0 {
		n = 0
	}
	workers.Store(int64(n))
}

// Runtime returns the process-wide JavaScript runtime, creating it on first
// use.
func Runtime() *jsrt.Runtime {
	rtMu.Lock()
	defer rtMu.Unlock()
	if rt == nil {
		rt = jsrt.New(jsrt.Options{Size: int(workers.Load())})
	}
	return rt
}

// Close stops the runtime's workers (process shutdown); a later evaluation
// starts a new runtime.
func Close() error {
	rtMu.Lock()
	defer rtMu.Unlock()
	if rt == nil {
		return nil
	}
	err := rt.Close()
	rt = nil
	return err
}

// callBudget is the budget of one evaluation: what the request has left
// (R-JS-55/56). An exhausted budget is reported as the failure the
// evaluation would have ended with.
func callBudget(b *budget.Request) (jsrt.Budget, *jsrt.Failure) {
	rem := b.Remaining()
	if rem <= 0 {
		return jsrt.Budget{}, &jsrt.Failure{Variant: jsrt.VariantTimeout, Message: "the request's time budget is exhausted"}
	}
	_, used, _ := b.Consumed()
	mem := int64(budget.DefaultMemory) - used
	if mem <= 0 {
		return jsrt.Budget{}, &jsrt.Failure{Variant: jsrt.VariantOutOfMemory, Message: "the request's memory budget is exhausted"}
	}
	if mem > jsrt.DefaultMemory {
		mem = jsrt.DefaultMemory
	}
	return jsrt.Budget{Time: rem, Memory: mem}, nil
}

// charge reports an evaluation's work to the request budget (R-JS-59).
func charge(b *budget.Request, st jsrt.Stats) {
	mem := st.MemoryUsed
	if mem < 0 {
		mem = 0
	}
	b.AddJS(int64(1+st.DOMOps+st.HostCalls), mem)
}

// callModule evaluates a module binding under the request budget.
func callModule(ctx context.Context, req jsrt.CallRequest) (*jsrt.Result, *jsrt.Failure) {
	b := budget.From(ctx)
	bud, f := callBudget(b)
	if f != nil {
		return nil, f
	}
	req.Budget = bud
	res, fail := Runtime().CallModule(ctx, req)
	if res != nil {
		charge(b, res.Stats)
	} else if fail != nil {
		charge(b, fail.Stats)
	}
	return res, fail
}

// evalExpression evaluates a j: expression under the request budget.
func evalExpression(ctx context.Context, req jsrt.ExprRequest) (*jsrt.Result, *jsrt.Failure) {
	b := budget.From(ctx)
	bud, f := callBudget(b)
	if f != nil {
		return nil, f
	}
	req.Budget = bud
	res, fail := Runtime().EvalExpression(ctx, req)
	if res != nil {
		charge(b, res.Stats)
	} else if fail != nil {
		charge(b, fail.Stats)
	}
	return res, fail
}
