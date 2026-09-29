//go:build linux

package jsrt

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const rssPollInterval = 20 * time.Millisecond

// platformLockdown bounds the address space and sets no_new_privs, both
// installable from pure Go.
func platformLockdown(cfg workerConfig) []string {
	var notes []string
	as := uint64(cfg.addressSpace)
	err := syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{Cur: as, Max: as})
	notes = append(notes, fmt.Sprintf("AS=%d:%v", as, err == nil))
	const prSetNoNewPrivs = 38
	_, _, errno := syscall.RawSyscall6(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0, 0, 0, 0)
	notes = append(notes, fmt.Sprintf("no_new_privs:%v", errno == 0))
	return notes
}

// processRSS reads a process's current RSS from /proc.
func processRSS(pid int) int64 {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm")
	if err != nil {
		return -1
	}
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return -1
	}
	pages, err := strconv.ParseInt(f[1], 10, 64)
	if err != nil {
		return -1
	}
	return pages * int64(os.Getpagesize())
}
