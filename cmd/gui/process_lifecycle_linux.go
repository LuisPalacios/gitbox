//go:build linux

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// configureChildLifetime arms PR_SET_PDEATHSIG before the child execs so
// the kernel sends SIGKILL to the Manager subprocess the moment the
// parent gitbox process disappears. Works regardless of whether the
// parent's Shutdown handler fires — covers crashes, kill -9, dropped
// terminals, etc. (issue #69 — orphan Manager survived parent close).
func configureChildLifetime(c *exec.Cmd) {
	if c.SysProcAttr == nil {
		c.SysProcAttr = &syscall.SysProcAttr{}
	}
	c.SysProcAttr.Pdeathsig = syscall.SIGKILL
}

// linkChildLifetimeToParent is a Windows-only Job Object hook. No-op on
// Linux; Pdeathsig handles the binding before the child even reaches main.
func linkChildLifetimeToParent(_ *os.Process) {}
