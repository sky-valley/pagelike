package jsrt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestPagelikeBinaryAsWorker builds cmd/pagelike and uses it as the worker
// executable, through the pool (environment marker) and through the
// explicit "pagelike jsrt-worker" subcommand.
func TestPagelikeBinaryAsWorker(t *testing.T) {
	if testing.Short() {
		t.Skip("builds cmd/pagelike")
	}
	bin := filepath.Join(t.TempDir(), "pagelike")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/sky-valley/pagelike/cmd/pagelike").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	r := New(Options{Size: 1, Executable: bin})
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, f := r.CallModule(ctx, CallRequest{Slot: SlotRead, Source: `export default (v) => v.toUpperCase();`, Args: []Value{"ok"}})
	if f != nil || res.Value != "OK" {
		t.Fatalf("%v %v", f, res)
	}
	// The subcommand speaks the protocol on stdin/stdout.
	cmd := exec.Command(bin, WorkerCommand)
	cmd.Env = []string{}
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	c := newConn(stdout, stdin)
	fr, err := c.recv()
	if err != nil || fr.T != "ready" {
		t.Fatalf("ready: %v %+v", err, fr)
	}
	if err := c.send(&frame{T: "call", Call: &callMsg{Source: `export default () => 6 * 7;`, Kind: "read", ArgCount: 1, Args: `[null]`, TimeoutNS: int64(time.Second), Memory: DefaultMemory}}); err != nil {
		t.Fatal(err)
	}
	fr, err = c.recv()
	if err != nil || fr.T != "result" || fr.Result.Outcome != "ok" || fr.Result.Value != "42" {
		t.Fatalf("result: %v %+v", err, fr.Result)
	}
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("worker exit: %v", err)
	}
}
