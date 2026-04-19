//go:build !windows

package git

import "os/exec"

// NewConsole is a no-op on non-Windows platforms.
func NewConsole(_ *exec.Cmd) {}
