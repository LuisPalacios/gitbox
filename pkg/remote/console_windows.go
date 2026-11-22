//go:build windows

package remote

import (
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// StartConsole starts argv in a console window of its own, with dir as its
// working folder and env as its whole environment (#243). It is how a
// Windows client opens a remote terminal: ssh.exe runs directly in the new
// console, with no cmd.exe in between, so the only parser of the command
// line is ssh.exe's own, which ComposeCommandLine matches exactly.
//
// It calls CreateProcess itself because os/exec always sets
// STARTF_USESTDHANDLES: a GUI parent has no console, so the child would get
// NUL for its input and ssh -t would end at once. Without that flag the new
// console gives the child its own handles. The window is the terminal the
// user sees, so it is not hidden, and there is no intermediate process that
// could flash.
func StartConsole(argv []string, dir string, env []string) error {
	if len(argv) == 0 || argv[0] == "" {
		return newError(CodeSSHMissing, "no command to start")
	}
	for _, a := range argv {
		if strings.ContainsRune(a, 0) {
			return newError(CodeNotAvailablePath, "the command holds a NUL")
		}
	}
	app, err := windows.UTF16PtrFromString(argv[0])
	if err != nil {
		return newError(CodeNotAvailablePath, "the command cannot be encoded")
	}
	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(argv))
	if err != nil {
		return newError(CodeNotAvailablePath, "the command line cannot be encoded")
	}
	var cwd *uint16
	if dir != "" {
		if cwd, err = windows.UTF16PtrFromString(dir); err != nil {
			return newError(CodeNotAvailablePath, "the working folder cannot be encoded")
		}
	}
	block, err := envBlock(env)
	if err != nil {
		return newError(CodeNotAvailablePath, "the environment cannot be encoded")
	}
	si := &windows.StartupInfo{}
	si.Cb = uint32(unsafe.Sizeof(*si))
	var pi windows.ProcessInformation
	flags := uint32(windows.CREATE_NEW_CONSOLE | windows.CREATE_UNICODE_ENVIRONMENT)
	if err := windows.CreateProcess(app, line, nil, nil, false, flags, block, cwd, si, &pi); err != nil {
		return newError(CodeSSHFailed, "the terminal could not be started")
	}
	_ = windows.CloseHandle(pi.Thread)
	_ = windows.CloseHandle(pi.Process)
	return nil
}

// ConsoleCommandLine is the command line StartConsole gives CreateProcess.
func ConsoleCommandLine(argv []string) string { return windows.ComposeCommandLine(argv) }

// envBlock encodes env as a CreateProcess environment block: each
// KEY=VALUE in UTF-16 followed by a NUL, then one more NUL.
func envBlock(env []string) (*uint16, error) {
	var block []uint16
	for _, kv := range env {
		if strings.ContainsRune(kv, 0) {
			return nil, windows.ERROR_INVALID_PARAMETER
		}
		block = append(block, utf16.Encode([]rune(kv))...)
		block = append(block, 0)
	}
	if len(block) == 0 {
		block = append(block, 0)
	}
	block = append(block, 0)
	return &block[0], nil
}
