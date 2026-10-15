//go:build windows

package update

import (
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// detach starts the relaunched process in its own process group. GitboxApp
// is a GUI-subsystem binary, so no console can flash. HideWindow is left
// off on purpose: it would make the new app's first ShowWindow call hide
// its main window.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// WaitForExit blocks until process pid has exited or timeout passes, and
// reports whether it exited. A PID that can't be opened is already gone.
func WaitForExit(pid int, timeout time.Duration) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, uint32(timeout.Milliseconds()))
	return err == nil && ev == windows.WAIT_OBJECT_0
}
