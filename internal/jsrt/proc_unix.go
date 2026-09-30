//go:build unix

package jsrt

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// setProcAttr puts a worker in its own process group, so a kill reaches
// anything it might have started.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}

// setLimit assigns an rlimit field, whose type is uint64 on most systems
// and int64 on the BSDs.
func setLimit[T int64 | uint64](p *T, v uint64) { *p = T(v) }

// lockdown applies the OS limits a worker sets on itself (decision 0001):
// no core dumps, no regular-file writes (pipes are unaffected), a small
// descriptor table, a lifetime CPU bound, an empty environment and / as the
// working directory. platformLockdown adds the Linux-only limits.
func lockdown(cfg workerConfig) []string {
	var notes []string
	set := func(name string, res int, cur, max uint64) {
		var r syscall.Rlimit
		setLimit(&r.Cur, cur)
		setLimit(&r.Max, max)
		err := syscall.Setrlimit(res, &r)
		notes = append(notes, fmt.Sprintf("%s=%d:%v", name, cur, err == nil))
	}
	set("CORE", syscall.RLIMIT_CORE, 0, 0)
	set("FSIZE", syscall.RLIMIT_FSIZE, 0, 0)
	set("NOFILE", syscall.RLIMIT_NOFILE, 32, 32)
	set("CPU", syscall.RLIMIT_CPU, 600, 620)
	notes = append(notes, platformLockdown(cfg)...)
	os.Clearenv()
	if err := os.Chdir("/"); err != nil {
		notes = append(notes, "chdir:false")
	}
	return notes
}
