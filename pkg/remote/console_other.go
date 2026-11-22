//go:build !windows

package remote

// StartConsole starts a process in a console window of its own. Only a
// Windows client needs it (console_windows.go); elsewhere a terminal
// emulator takes the argv.
func StartConsole(argv []string, dir string, env []string) error {
	return newError(CodeNotAvailablePath, "a console of its own is only started on Windows")
}
