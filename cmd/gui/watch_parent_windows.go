//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// watchParent runs in the Manager subprocess. It opens a handle to the
// parent gitbox.exe and blocks on WaitForSingleObject — when the parent
// process exits (graceful, crash, taskkill, anything), the kernel
// signals the handle, the wait returns, and we self-terminate. Single
// kernel syscall, no polling, fires within milliseconds of parent
// death. Independent of Wails' Shutdown handler firing or any Job
// Object setup, both of which proved unreliable for this case
// (issue #69).
func watchParent(ppid int) {
	if ppid <= 0 {
		return
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(ppid))
	if err != nil {
		fmt.Fprintf(os.Stderr, "watchParent: OpenProcess(%d): %v\n", ppid, err)
		return
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, windows.INFINITE)
	os.Exit(0)
}
