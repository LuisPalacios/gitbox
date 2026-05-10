//go:build darwin

package main

import (
	"os"
	"syscall"
	"time"
)

// watchParent runs in the Manager subprocess on macOS as a defense-in-
// depth fallback for the parent-side Shutdown Kill. macOS lacks both
// PR_SET_PDEATHSIG and Job Objects, so without this watcher a parent
// that dies without firing Shutdown (crash, kill -9) would orphan the
// Manager. Polls every second via signal 0 — cheap, no
// platform-specific syscalls.
func watchParent(ppid int) {
	if ppid <= 0 {
		return
	}
	for {
		time.Sleep(time.Second)
		if syscall.Kill(ppid, 0) != nil {
			os.Exit(0)
		}
	}
}
