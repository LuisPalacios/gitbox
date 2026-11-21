//go:build !windows

package git

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// procTree stops a command together with everything it started. Git runs
// credential helpers, ssh and hooks as children; stopping only git would
// leave them alive, holding its output open.
//
// On Unix the command runs in its own process group, so one signal reaches
// the whole tree.
type procTree struct{}

// prepare makes cmd the leader of a new process group. Call it before Start.
func (procTree) prepare(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// attach has nothing to do on Unix: the group exists from the start.
func (procTree) attach(*exec.Cmd) {}

// terminate asks the whole tree to stop. SIGTERM lets git remove its lock
// files before it exits.
func (procTree) terminate(cmd *exec.Cmd) error { return signalGroup(cmd, syscall.SIGTERM) }

// kill stops whatever is left of the tree at once.
func (procTree) kill(cmd *exec.Cmd) { _ = signalGroup(cmd, syscall.SIGKILL) }

// release has nothing to free on Unix.
func (procTree) release() {}

func signalGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd.Process == nil {
		return nil
	}
	// A negative pid addresses the process group that cmd leads.
	err := syscall.Kill(-cmd.Process.Pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
