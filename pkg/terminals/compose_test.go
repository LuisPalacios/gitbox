package terminals

import (
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/launch"
)

// fakeWTApp is a stand-in for a modern Windows Terminal that hosts a shell.
var fakeWTApp = config.TerminalApp{
	ID: "wt", Name: "Windows Terminal", Command: "wt.exe",
	ArgsTemplate: []string{"-d", launch.TokenPath, launch.TokenShellCommand, launch.TokenShellArgs},
}

// fakeBareShellApp is a stand-in for a non-modern terminal (no shell tokens).
var fakeBareShellApp = config.TerminalApp{
	ID: "git-bash", Name: "Git Bash", Command: "git-bash.exe",
	ArgsTemplate: []string{"--cd=" + launch.TokenPath},
}

var fakePwshShell = config.ShellEntry{
	ID: "pwsh", Name: "PowerShell 7", Command: "pwsh.exe",
}
var fakeBashShell = config.ShellEntry{
	ID: "bash", Name: "Bash", Command: "bash", Args: []string{"-l"},
}

// fakeMacApp is a stand-in for a macOS `open -a Terminal` entry — no shell
// tokens, just the bundle reference.
var fakeMacApp = config.TerminalApp{
	ID: "terminal", Name: "Terminal", Command: "open",
	ArgsTemplate: []string{"-a", "Terminal"},
}

func TestComposeWindowsProducesTimesShell(t *testing.T) {
	apps := []config.TerminalApp{fakeWTApp}
	shells := []config.ShellEntry{fakePwshShell, fakeBashShell}
	got := ComposeProfiles(apps, shells, "windows")
	// Expect 2 T×S pairs PLUS 1 DIRECT bare-pwsh Profile (Hidden=true).
	// fakeBashShell has ID="bash" which is not in windowsDirectShellIDs, so
	// only pwsh gets a DIRECT Profile.
	if len(got) != 3 {
		t.Fatalf("expected 2 T×S + 1 DIRECT, got %d: %+v", len(got), got)
	}
	if got[0].TerminalID != "wt" || got[0].ShellID != "pwsh" {
		t.Errorf("got[0] = (term=%q, shell=%q), want (wt, pwsh)", got[0].TerminalID, got[0].ShellID)
	}
	if got[0].Source != "detected" {
		t.Errorf("expected Source=detected, got %q", got[0].Source)
	}
	// Last entry should be the DIRECT bare-pwsh Profile, Hidden=true.
	last := got[len(got)-1]
	if last.TerminalID != "" || last.ShellID != "pwsh" || !last.Hidden {
		t.Errorf("expected last to be hidden bare-pwsh, got %+v", last)
	}
}

func TestComposeWindowsSkipsBareShellTerminalsWhenModernPresent(t *testing.T) {
	// Both a modern terminal AND a bare-shell-as-terminal entry are present.
	// Composition only emits T×S pairs for the modern one, plus the DIRECT
	// shortcut for pwsh (which is in windowsDirectShellIDs).
	apps := []config.TerminalApp{fakeWTApp, fakeBareShellApp}
	shells := []config.ShellEntry{fakePwshShell}
	got := ComposeProfiles(apps, shells, "windows")
	if len(got) != 2 {
		t.Fatalf("expected 1 modern-only pair + 1 DIRECT, got %d: %+v", len(got), got)
	}
	if got[0].TerminalID != "wt" {
		t.Errorf("expected wt, got terminal=%q", got[0].TerminalID)
	}
	if got[1].TerminalID != "" || !got[1].Hidden {
		t.Errorf("expected DIRECT hidden bare-pwsh as second entry, got %+v", got[1])
	}
}

// TestComposeWindowsDirectShellsAreHidden — issue #71: the 4 directly-
// launchable Windows shells (pwsh, powershell, cmd, wsl) get Hidden=true
// bare-shell Profiles when at least one modern Terminal is installed.
func TestComposeWindowsDirectShellsAreHidden(t *testing.T) {
	apps := []config.TerminalApp{fakeWTApp}
	shells := []config.ShellEntry{
		{ID: "pwsh", Name: "PowerShell 7"},
		{ID: "powershell", Name: "PowerShell 5"},
		{ID: "cmd", Name: "Command Prompt"},
		{ID: "wsl", Name: "WSL (default distro)"},
		{ID: "git-bash", Name: "Git Bash"}, // NOT in DIRECT list — no bare Profile
	}
	got := ComposeProfiles(apps, shells, "windows")
	hiddenBare := 0
	for _, p := range got {
		if p.TerminalID == "" && p.Hidden {
			hiddenBare++
		}
	}
	if hiddenBare != 4 {
		t.Errorf("expected 4 Hidden DIRECT bare-shell Profiles (pwsh/powershell/cmd/wsl), got %d", hiddenBare)
	}
}

func TestComposeWindowsFallsBackToBareShellsWhenNoModern(t *testing.T) {
	// No modern terminal at all — fall back to one Profile per shell so the
	// user isn't stranded.
	apps := []config.TerminalApp{fakeBareShellApp}
	shells := []config.ShellEntry{fakePwshShell, fakeBashShell}
	got := ComposeProfiles(apps, shells, "windows")
	if len(got) != 2 {
		t.Fatalf("expected 2 bare-shell fallback profiles, got %d: %+v", len(got), got)
	}
	for _, p := range got {
		if p.TerminalID != "" {
			t.Errorf("expected empty TerminalID in bare-shell fallback, got %q", p.TerminalID)
		}
		if p.ShellID == "" {
			t.Errorf("expected non-empty ShellID in bare-shell fallback")
		}
	}
}

func TestComposeUnixIsTerminalOnly(t *testing.T) {
	apps := []config.TerminalApp{fakeMacApp}
	shells := []config.ShellEntry{fakeBashShell}
	got := ComposeProfiles(apps, shells, "darwin")
	if len(got) != 1 {
		t.Fatalf("expected 1 Terminal-only Profile, got %d: %+v", len(got), got)
	}
	if got[0].TerminalID != "terminal" {
		t.Errorf("got terminal=%q, want %q", got[0].TerminalID, "terminal")
	}
	if got[0].ShellID != "" {
		t.Errorf("expected empty ShellID on macOS, got %q", got[0].ShellID)
	}
}

func TestComposeUnixIgnoresShells(t *testing.T) {
	// Multiple shells installed must NOT produce multiple Profiles per Terminal.
	apps := []config.TerminalApp{fakeMacApp}
	shells := []config.ShellEntry{fakeBashShell, fakePwshShell, {ID: "fish", Name: "Fish", Command: "fish"}}
	got := ComposeProfiles(apps, shells, "linux")
	if len(got) != 1 {
		t.Fatalf("expected 1 Profile (Terminal-only on Linux), got %d", len(got))
	}
}

func TestHasModernTerminal(t *testing.T) {
	if !HasModernTerminal([]config.TerminalApp{fakeWTApp}) {
		t.Error("expected HasModernTerminal=true for WT")
	}
	if HasModernTerminal([]config.TerminalApp{fakeBareShellApp}) {
		t.Error("expected HasModernTerminal=false for bare-shell entry")
	}
	if HasModernTerminal([]config.TerminalApp{fakeMacApp}) {
		t.Error("expected HasModernTerminal=false for macOS open -a entry")
	}
	if HasModernTerminal(nil) {
		t.Error("expected HasModernTerminal=false for empty input")
	}
}
