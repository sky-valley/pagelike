//go:build linux

package jsrt

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// A worker's memory limit concerns its own executable image, not memory used
// by the parent before exec. Linux getrusage retains that older high-water mark.
func TestPeakRSSStartsWithNewExecutable(t *testing.T) {
	if os.Getenv("PAGELIKE_TEST_RSS_CHILD") == "1" {
		fmt.Println(peakRSS())
		os.Exit(0)
	}
	allocation := make([]byte, 96<<20)
	for i := 0; i < len(allocation); i += os.Getpagesize() {
		allocation[i] = 1
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestPeakRSSStartsWithNewExecutable$")
	cmd.Env = append(os.Environ(), "PAGELIKE_TEST_RSS_CHILD=1")
	out, err := cmd.CombinedOutput()
	runtime.KeepAlive(allocation)
	if err != nil {
		t.Fatalf("child: %v: %s", err, out)
	}
	rss, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || rss <= 0 || rss >= 64<<20 {
		t.Fatalf("fresh image RSS = %q, want a positive value below the parent's allocation", out)
	}
}
