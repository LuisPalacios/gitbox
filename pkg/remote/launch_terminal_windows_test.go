//go:build windows

package remote

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/launch"
	"github.com/LuisPalacios/gitbox/pkg/terminals"
)

// The Windows catalog templates of the argv-safe terminals (#286). They are
// written out here so a catalog change that would stop them being argv-safe
// fails this test as well as terminals.ProgramArgvSafe.
var argvSafeTemplates = map[string][]string{
	"wezterm":   {"start", "--cwd", launch.TokenPath, "--", launch.TokenShellCommand, launch.TokenShellArgs},
	"alacritty": {"--working-directory", launch.TokenPath, "-e", launch.TokenShellCommand, launch.TokenShellArgs},
}

// TestWindowsTerminalArgvRoundTrip: a remote terminal in a WezTerm or
// Alacritty profile crosses two command lines on a Windows client (#286).
// Go composes the terminal's, which the terminal parses with the
// CommandLineToArgvW rules; the terminal then composes ssh.exe's from the
// program part. With the stub standing in for the terminal and then for
// ssh, both hops must give back the argv built, for every hostile folder.
func TestWindowsTerminalArgvRoundTrip(t *testing.T) {
	stub := buildStub(t)
	work := t.TempDir()
	for id, tmpl := range argvSafeTemplates {
		g := config.GlobalConfig{
			TerminalApps:     []config.TerminalApp{{ID: id, Name: id, Command: stub, ArgsTemplate: tmpl}},
			TerminalProfiles: []config.TerminalProfile{{ID: "p", Name: "p", TerminalID: id, Default: true}},
		}
		if !terminals.ProgramArgvSafe(g, "p", "windows") {
			t.Fatalf("%s with its catalog template is not argv-safe", id)
		}
		progAt := slices.Index(tmpl, launch.TokenShellCommand)
		for i, h := range hostileNames {
			remoteCmd, err := RemoteCommand(FamilyPOSIX, "/srv/"+h.name, nil)
			if err != nil {
				t.Fatal(err)
			}
			argv, err := TerminalArgv(`C:\Windows\System32\OpenSSH\ssh.exe`, "my-host", remoteCmd)
			if err != nil {
				t.Fatal(err)
			}
			l, err := terminals.ResolveProgramLaunch(terminals.LaunchRequest{Global: g, ProfileID: "p", Path: work, GOOS: "windows"}, argv, "")
			if err != nil {
				t.Fatal(err)
			}

			// Hop 1: started the way openTerminalRawAt starts a GUI terminal.
			out := filepath.Join(work, fmt.Sprintf("%s-%d-terminal.json", id, i))
			cmd := exec.Command(l.Command, l.Args...)
			cmd.Dir = work
			cmd.Env = append(os.Environ(), "GBX_STUB_OUT="+out)
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v %s", id, err, b)
			}
			got := waitArgv(t, out)
			if !reflect.DeepEqual(got, l.Args) {
				t.Errorf("%s, %q: the terminal received %q, want %q", id, h.name, got, l.Args)
				continue
			}
			prog := got[progAt:]
			if !reflect.DeepEqual(prog, argv) {
				t.Errorf("%s, %q: the program part is %q, want %q", id, h.name, prog, argv)
				continue
			}

			// Hop 2: the terminal starts ssh.exe with a line composed from
			// that program part.
			out2 := filepath.Join(work, fmt.Sprintf("%s-%d-ssh.json", id, i))
			ssh := exec.Command(stub)
			ssh.SysProcAttr = &syscall.SysProcAttr{CmdLine: windows.ComposeCommandLine(append([]string{stub}, prog[1:]...))}
			ssh.Env = append(os.Environ(), "GBX_STUB_OUT="+out2)
			if b, err := ssh.CombinedOutput(); err != nil {
				t.Fatalf("%s: ssh stub: %v %s", id, err, b)
			}
			if got := waitArgv(t, out2); !reflect.DeepEqual(got, argv[1:]) {
				t.Errorf("%s, %q: ssh received %q, want %q", id, h.name, got, argv[1:])
			}
		}
	}
	noMarker(t, work, "the Windows terminal round trip")
}

// TestWindowsTerminalRealWezTerm runs the same round trip through the real
// WezTerm when it is installed, with the stub as ssh. WezTerm needs a window
// station it can draw on; in a session without one (an SSH session of the
// gate) the stub never runs and the test skips, leaving the check to the
// Windows smoke. --always-new-process keeps the launch in this process tree
// instead of handing it to a running WezTerm.
func TestWindowsTerminalRealWezTerm(t *testing.T) {
	wez, err := exec.LookPath("wezterm")
	if err != nil {
		t.Skip("WezTerm is not installed on this host")
	}
	stub := buildStub(t)
	work := t.TempDir()
	for i, name := range []string{"it's here", "a;rm -rf ~", "%PATH%", "$(touch M)", "ünïcödé ✓"} {
		remoteCmd, err := RemoteCommand(FamilyPOSIX, "/srv/"+name, nil)
		if err != nil {
			t.Fatal(err)
		}
		argv, err := TerminalArgv(stub, "my-host", remoteCmd)
		if err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(work, fmt.Sprintf("wezterm-%d.json", i))
		cmd := exec.Command(wez, append([]string{"start", "--always-new-process", "--cwd", work, "--"}, argv...)...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(), "GBX_STUB_OUT="+out)
		if err := cmd.Start(); err != nil {
			t.Fatalf("starting WezTerm: %v", err)
		}
		got, ok := readArgvWithin(t, out, 20*time.Second)
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		if !ok {
			t.Skip("WezTerm opened no window in this session; the Windows smoke covers it")
		}
		if !reflect.DeepEqual(got, argv[1:]) {
			t.Errorf("%q: ssh received %q through WezTerm, want %q", name, got, argv[1:])
		}
	}
	noMarker(t, work, "the real WezTerm round trip")
}

// readArgvWithin is waitArgv without the failure: ok is false when the stub did
// not write its arguments within d.
func readArgvWithin(t *testing.T, out string, d time.Duration) ([]string, bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(out); err == nil && len(data) > 0 {
			var argv []string
			if err := json.Unmarshal(data, &argv); err != nil {
				t.Fatalf("stub output %q: %v", data, err)
			}
			return argv, true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, false
}
