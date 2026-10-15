package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestRelaunchCommand(t *testing.T) {
	bundleExe := filepath.Join("/Applications", "GitboxApp.app", "Contents", "MacOS", "GitboxApp")
	bundle := filepath.Join("/Applications", "GitboxApp.app")
	plainExe := filepath.Join("/opt", "gitbox", "GitboxApp")

	tests := []struct {
		name     string
		goos     string
		exe      string
		args     []string
		wantName string
		wantArgs []string
	}{
		{
			name:     "macOS bundle relaunches through open -n",
			goos:     "darwin",
			exe:      bundleExe,
			wantName: "/usr/bin/open",
			wantArgs: []string{"-n", bundle, "--args", "--wait-pid=42"},
		},
		{
			name:     "macOS binary outside a bundle runs directly",
			goos:     "darwin",
			exe:      plainExe,
			wantName: plainExe,
			wantArgs: []string{"--wait-pid=42"},
		},
		{
			name:     "Linux runs the binary",
			goos:     "linux",
			exe:      plainExe,
			wantName: plainExe,
			wantArgs: []string{"--wait-pid=42"},
		},
		{
			name:     "Windows runs the exe",
			goos:     "windows",
			exe:      `C:\Users\me\AppData\Local\gitbox\GitboxApp.exe`,
			wantName: `C:\Users\me\AppData\Local\gitbox\GitboxApp.exe`,
			wantArgs: []string{"--wait-pid=42"},
		},
		{
			name:     "args pass through, a stale wait-pid is dropped",
			goos:     "linux",
			exe:      plainExe,
			args:     []string{"--wait-pid=7", "--test-mode"},
			wantName: plainExe,
			wantArgs: []string{"--wait-pid=42", "--test-mode"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, args := relaunchCommand(tc.goos, tc.exe, 42, tc.args)
			if name != tc.wantName || !slices.Equal(args, tc.wantArgs) {
				t.Errorf("relaunchCommand() = %q %q, want %q %q", name, args, tc.wantName, tc.wantArgs)
			}
		})
	}
}

func TestWaitForExit_ProcessExits(t *testing.T) {
	// The test binary with no matching test exits at once.
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting child: %v", err)
	}
	// Reap the child so its PID does not linger as a zombie on Unix.
	go func() { _ = cmd.Wait() }()

	if !WaitForExit(cmd.Process.Pid, 10*time.Second) {
		t.Error("WaitForExit() = false for a process that exited, want true")
	}
}

func TestWaitForExit_TimesOutOnLiveProcess(t *testing.T) {
	start := time.Now()
	if WaitForExit(os.Getpid(), 300*time.Millisecond) {
		t.Error("WaitForExit() = true for the running test process, want false")
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Errorf("WaitForExit() returned after %v, before its timeout", elapsed)
	}
}
