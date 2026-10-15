package update

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// WaitPIDFlag carries the PID of the process a relaunched GitboxApp
// replaces. The new process waits for that PID to exit before it starts,
// so it never meets the old instance's single-instance lock.
const WaitPIDFlag = "--wait-pid="

// Relaunch starts exe detached, as the new version of the running app, and
// tells it to wait for pid to exit first. exe is the path captured before
// the update replaced the files. args are passed through, minus any
// earlier WaitPIDFlag. The caller quits right after a nil return.
func Relaunch(exe string, pid int, args []string) error {
	name, argv := relaunchCommand(runtime.GOOS, exe, pid, args)
	cmd := exec.Command(name, argv...)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting %s: %w", name, err)
	}
	return cmd.Process.Release()
}

// relaunchCommand returns the program and arguments that start exe again.
// On macOS an executable inside a bundle relaunches through `open -n` on
// the bundle, so LaunchServices starts it like a Finder launch; -n starts a
// new instance even while the old one is still running.
func relaunchCommand(goos, exe string, pid int, args []string) (string, []string) {
	argv := []string{fmt.Sprintf("%s%d", WaitPIDFlag, pid)}
	for _, a := range args {
		if !strings.HasPrefix(a, WaitPIDFlag) {
			argv = append(argv, a)
		}
	}
	if goos == "darwin" {
		if bundle := bundleOf(exe); bundle != "" {
			return "/usr/bin/open", append([]string{"-n", bundle, "--args"}, argv...)
		}
	}
	return exe, argv
}
