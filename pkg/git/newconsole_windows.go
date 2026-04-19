package git

import (
	"os/exec"
	"syscall"
)

// createNewConsole is the Win32 CreateProcess flag CREATE_NEW_CONSOLE (0x10).
// It gives the child process its own fresh console window rather than
// inheriting (a likely broken) stdio from a GUI parent.
const createNewConsole = 0x00000010

// NewConsole configures cmd to launch in a new Windows console window.
// Required when a GUI process (no console) spawns console apps like cmd.exe,
// powershell.exe, pwsh.exe, or wsl.exe — without this flag their inherited
// stdio is detached and they exit immediately.
func NewConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNewConsole,
	}
}
