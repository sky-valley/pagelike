package jsrt

import (
	"strings"
	"testing"
	"time"
)

// TestGoStackBackstop disables the engine's own stack limit (an absurd slot
// count), so deep recursion exhausts the worker's Go stack instead: the
// worker dies, the parent reports it (as a stack overflow, or as memory when
// the RSS ceiling is reached first), and the pool respawns. The host is
// never affected.
func TestGoStackBackstop(t *testing.T) {
	if testing.Short() {
		t.Skip("grows a worker to its Go stack ceiling")
	}
	r := New(Options{Size: 1, StackSlots: 10_000_000, RSSLimit: 4 << 30})
	defer r.Close()
	pid := alive(t, r)
	f, el := hostile(t, r, `const f = (n) => f(n + 1) + 1; export default () => f(0);`, Budget{Time: 30 * time.Second, Memory: 1 << 30})
	if !(f.Variant == VariantThrew && strings.Contains(f.Message, "stack overflow") || f.Variant == VariantOutOfMemory) {
		t.Fatalf("got %s: %s", f.Variant, f.Message)
	}
	t.Logf("backstop after %v: %s: %s", el, f.Variant, f.Message)
	if alive(t, r) == pid {
		t.Error("expected a new worker")
	}
}
