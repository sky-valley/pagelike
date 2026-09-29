//go:build unix

package jsrt

import (
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWorkerCrashIsRecovered(t *testing.T) {
	r := New(Options{Size: 1})
	defer r.Close()
	pid := alive(t, r)
	// Killed while idle: the next call gets a fresh worker.
	_ = syscall.Kill(pid, syscall.SIGKILL)
	time.Sleep(100 * time.Millisecond)
	pid2 := alive(t, r)
	if pid2 == pid {
		t.Fatal("expected a new worker")
	}
	// Killed during a call: that call fails, the next one works.
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = syscall.Kill(pid2, syscall.SIGKILL)
	}()
	f, _ := hostile(t, r, `export default () => { for (;;) {} };`, Budget{Time: 10 * time.Second})
	if f.Variant != VariantInternal || !strings.Contains(f.Message, "died") {
		t.Fatalf("got %s: %s", f.Variant, f.Message)
	}
	if alive(t, r) == pid2 {
		t.Fatal("expected a new worker")
	}
}
