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

// VmHWM belongs to the current executable's address space. getrusage retains
// the parent's pre-exec peak on Linux and can kill a fresh, small worker merely
// because the host used more memory before starting it (getrusage(2), NOTES).
func peakRSS() int64 {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return processRSS(os.Getpid())
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "VmHWM:" && fields[2] == "kB" {
			kb, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil {
				return kb * 1024
			}
		}
	}
	return processRSS(os.Getpid())
}

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
