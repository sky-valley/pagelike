package jsrt

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
)

// TestMain lets the pool start this test binary as a worker.
func TestMain(m *testing.M) {
	MaybeRunWorker()
	code := m.Run()
	if testRT != nil {
		testRT.Close()
	}
	os.Exit(code)
}

var (
	testRT     *Runtime
	testRTOnce sync.Once
)

func rt(t testing.TB) *Runtime {
	t.Helper()
	testRTOnce.Do(func() { testRT = New(Options{Size: 4}) })
	return testRT
}

func call(t testing.TB, req CallRequest) (*Result, *Failure) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return rt(t).CallModule(ctx, req)
}

func mustCall(t testing.TB, req CallRequest) *Result {
	t.Helper()
	res, f := call(t, req)
	if f != nil {
		t.Fatalf("unexpected failure %s: %s\n%s", f.Variant, f.Message, f.Stack)
	}
	return res
}

func TestSmoke(t *testing.T) {
	res := mustCall(t, CallRequest{Source: `export default (v) => v.trim().toLowerCase();`, Slot: SlotRead, Args: []Value{"  Hello "}})
	if res.Value != "hello" {
		t.Fatalf("got %#v", res.Value)
	}
}

func osGetpid() int { return os.Getpid() }
