//go:build !windows

package git

import "syscall"

// processAlive reports whether pid names a running process.
func processAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// endProcess kills a process a test left behind on purpose.
func endProcess(pid int) { _ = syscall.Kill(pid, syscall.SIGKILL) }
