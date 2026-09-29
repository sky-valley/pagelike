//go:build !unix

package jsrt

import (
	"os"
	"os/exec"
	"time"
)

// Non-Unix platforms get the engine limits and the parent deadline only.
const rssPollInterval = time.Second

func setProcAttr(*exec.Cmd) {}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func peakRSS() int64 { return 0 }

func processRSS(int) int64 { return -1 }

func lockdown(workerConfig) []string {
	os.Clearenv()
	return []string{"rlimits:unsupported"}
}
