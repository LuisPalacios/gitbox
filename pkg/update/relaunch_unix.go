//go:build !windows

package update

import (
	"os/exec"
	"syscall"
	"time"
)

// detach puts the relaunched process in its own session, so it outlives
// the app that starts it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// WaitForExit blocks until process pid has exited or timeout passes, and
// reports whether it exited. Signal 0 probes the PID without touching it;
// EPERM still means the process exists.
func WaitForExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if err := syscall.Kill(pid, 0); err != nil && err != syscall.EPERM {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
