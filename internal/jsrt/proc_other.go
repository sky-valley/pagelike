//go:build unix && !linux

package jsrt

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Without /proc the parent polls with ps(1), which costs a process start,
// so it polls less often; the worker's own getrusage check (10 ms) is the
// fast path. macOS cannot bound a Go process's address space
// (setrlimit(RLIMIT_AS) fails), so the RSS ceiling is the memory backstop.
const rssPollInterval = 200 * time.Millisecond

func platformLockdown(workerConfig) []string { return nil }

func processRSS(pid int) int64 {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return -1
	}
	kb, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return -1
	}
	return kb * 1024
}
