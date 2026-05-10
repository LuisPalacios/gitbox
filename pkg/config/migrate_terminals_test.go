package config

import (
	"reflect"
	"testing"
)

func TestMigrateLegacyTerminals_NoOpOnEmpty(t *testing.T) {
	g := &GlobalConfig{}
	if MigrateLegacyTerminals(g) {
		t.Fatal("expected no migration when Terminals is empty")
	}
	if len(g.TerminalApps) != 0 || len(g.Shells) != 0 || len(g.TerminalProfiles) != 0 {
		t.Fatalf("expected new fields to remain empty; got apps=%d shells=%d profiles=%d",
			len(g.TerminalApps), len(g.Shells), len(g.TerminalProfiles))
	}
}

func TestMigrateLegacyTerminals_DropsLegacyWhenAlreadyMigrated(t *testing.T) {
	g := &GlobalConfig{
		Terminals: []TerminalEntry{
			{Name: "Stale WT", Command: "wt.exe", Args: []string{"-d", "{path}"}},
		},
		TerminalProfiles: []TerminalProfile{
			{ID: "wt-pwsh", Name: "Windows Terminal — PowerShell 7", TerminalID: "wt", ShellID: "pwsh"},
		},
	}
	if !MigrateLegacyTerminals(g) {
		t.Fatal("expected migration to report true (legacy block dropped)")
	}
	if len(g.Terminals) != 0 {
		t.Fatalf("expected legacy Terminals to be cleared; got %d entries", len(g.Terminals))
	}
	if len(g.TerminalProfiles) != 1 || g.TerminalProfiles[0].ID != "wt-pwsh" {
		t.Fatalf("expected existing TerminalProfiles to be preserved; got %+v", g.TerminalProfiles)
	}
}

func TestMigrateLegacyTerminals_WindowsTerminalProfiles(t *testing.T) {
	originalArgs := [][]string{
		{"--profile", "PowerShell 7", "-d", "{path}", "{command}"},
		{"--profile", "Git Bash", "-d", "{path}", "{command}"},
		{"--profile", "Ubuntu 24.04.1 LTS", "-d", "{path}", "{command}"},
	}
	g := &GlobalConfig{
		Terminals: []TerminalEntry{
			{Name: "PowerShell 7", Command: `C:\path\wt.exe`, Args: originalArgs[0]},
			{Name: "Git Bash", Command: `C:\path\wt.exe`, Args: originalArgs[1]},
			{Name: "Ubuntu 24.04.1 LTS", Command: `C:\path\wt.exe`, Args: originalArgs[2]},
		},
	}
	if !MigrateLegacyTerminals(g) {
		t.Fatal("expected migration to run")
	}
	if len(g.Terminals) != 0 {
		t.Fatalf("legacy Terminals should be cleared; got %d", len(g.Terminals))
	}
	if len(g.TerminalApps) != 1 {
		t.Fatalf("expected exactly one TerminalApp (wt) for three WT-profile entries; got %d", len(g.TerminalApps))
	}
	if g.TerminalApps[0].ID != "wt" || g.TerminalApps[0].Name != "Windows Terminal" {
		t.Fatalf("unexpected app emitted: %+v", g.TerminalApps[0])
	}
	if len(g.TerminalProfiles) != 3 {
		t.Fatalf("expected 3 migrated profiles; got %d", len(g.TerminalProfiles))
	}
	if !g.TerminalProfiles[0].Default {
		t.Error("first migrated profile should be default")
	}
	if g.TerminalProfiles[1].Default || g.TerminalProfiles[2].Default {
		t.Error("only the first migrated profile should be default")
	}
	for i, p := range g.TerminalProfiles {
		if p.TerminalID != "wt" {
			t.Errorf("profile[%d] TerminalID = %q, want wt", i, p.TerminalID)
		}
		if p.ShellID != "" {
			t.Errorf("profile[%d] ShellID should be empty for WT-profile-derived entries; got %q", i, p.ShellID)
		}
		if p.Source != "migrated" {
			t.Errorf("profile[%d] Source = %q, want migrated", i, p.Source)
		}
		if !reflect.DeepEqual(p.Args, originalArgs[i]) {
			t.Errorf("profile[%d] Args not preserved verbatim: got %v, want %v", i, p.Args, originalArgs[i])
		}
	}
}

func TestMigrateLegacyTerminals_MacOSTerminalApp(t *testing.T) {
	g := &GlobalConfig{
		Terminals: []TerminalEntry{
			{Name: "Terminal", Command: "open", Args: []string{"-a", "Terminal"}},
			{Name: "iTerm", Command: "open", Args: []string{"-a", "iTerm"}},
		},
	}
	if !MigrateLegacyTerminals(g) {
		t.Fatal("expected migration to run")
	}
	if len(g.TerminalApps) != 2 {
		t.Fatalf("expected 2 mac apps; got %d (%+v)", len(g.TerminalApps), g.TerminalApps)
	}
	apps := map[string]TerminalApp{}
	for _, a := range g.TerminalApps {
		apps[a.ID] = a
	}
	for _, want := range []string{"macapp-terminal", "macapp-iterm"} {
		if _, ok := apps[want]; !ok {
			t.Errorf("expected app %q in result; got %v", want, apps)
		}
	}
}

func TestMigrateLegacyTerminals_LinuxGnomeTerminal(t *testing.T) {
	g := &GlobalConfig{
		Terminals: []TerminalEntry{
			{Name: "GNOME Terminal", Command: "gnome-terminal", Args: []string{"--working-directory={path}", "--", "{command}"}},
		},
	}
	if !MigrateLegacyTerminals(g) {
		t.Fatal("expected migration to run")
	}
	if len(g.TerminalApps) != 1 || g.TerminalApps[0].ID != "gnome-terminal" {
		t.Fatalf("expected gnome-terminal app; got %+v", g.TerminalApps)
	}
	if g.TerminalApps[0].Name != "GNOME Terminal" {
		t.Errorf("unexpected display name: %q", g.TerminalApps[0].Name)
	}
}

func TestMigrateLegacyTerminals_BareShells(t *testing.T) {
	g := &GlobalConfig{
		Terminals: []TerminalEntry{
			{Name: "PowerShell 7", Command: "pwsh.exe"},
			{Name: "Bash", Command: "/bin/bash", Args: []string{"-l"}},
		},
	}
	if !MigrateLegacyTerminals(g) {
		t.Fatal("expected migration to run")
	}
	if len(g.Shells) != 2 {
		t.Fatalf("expected 2 shells inferred from bare commands; got %d (%+v)", len(g.Shells), g.Shells)
	}
	shells := map[string]ShellEntry{}
	for _, s := range g.Shells {
		shells[s.ID] = s
	}
	if _, ok := shells["pwsh"]; !ok {
		t.Errorf("expected pwsh shell entry; got %v", shells)
	}
	if _, ok := shells["bash"]; !ok {
		t.Errorf("expected bash shell entry; got %v", shells)
	}
	for i, p := range g.TerminalProfiles {
		if p.ShellID == "" {
			t.Errorf("profile[%d] (%s) should have a ShellID for a bare-shell entry", i, p.Name)
		}
	}
}

func TestMigrateLegacyTerminals_WSLDistro(t *testing.T) {
	g := &GlobalConfig{
		Terminals: []TerminalEntry{
			{Name: "Ubuntu", Command: "wsl.exe", Args: []string{"-d", "Ubuntu-24.04", "--cd", "{path}"}},
		},
	}
	if !MigrateLegacyTerminals(g) {
		t.Fatal("expected migration to run")
	}
	if len(g.Shells) != 1 {
		t.Fatalf("expected 1 shell (wsl-ubuntu-24-04); got %d (%+v)", len(g.Shells), g.Shells)
	}
	if g.Shells[0].ID != "wsl-ubuntu-24-04" {
		t.Errorf("expected slugified WSL distro id; got %q", g.Shells[0].ID)
	}
	if g.TerminalProfiles[0].ShellID != "wsl-ubuntu-24-04" {
		t.Errorf("profile should reference the WSL distro shell; got %q", g.TerminalProfiles[0].ShellID)
	}
}

func TestMigrateLegacyTerminals_UnknownTerminalFallback(t *testing.T) {
	g := &GlobalConfig{
		Terminals: []TerminalEntry{
			{Name: "Custom Term", Command: "/opt/custom/myterm", Args: []string{"--cwd", "{path}"}},
		},
	}
	if !MigrateLegacyTerminals(g) {
		t.Fatal("expected migration to run")
	}
	if len(g.TerminalApps) != 1 {
		t.Fatalf("expected 1 fallback app; got %d", len(g.TerminalApps))
	}
	if g.TerminalApps[0].ID != "legacy-myterm" {
		t.Errorf("expected legacy- prefix on unknown commands; got %q", g.TerminalApps[0].ID)
	}
	// Unknown apps don't get an ArgsTemplate — the Profile's Args carries
	// the legacy argv verbatim and the launcher uses that directly.
	if len(g.TerminalApps[0].ArgsTemplate) != 0 {
		t.Errorf("unknown app should have empty ArgsTemplate; got %v", g.TerminalApps[0].ArgsTemplate)
	}
	if len(g.TerminalProfiles[0].Args) == 0 {
		t.Errorf("profile should preserve legacy argv on unknown apps")
	}
}

// TestCrossPlatformBase locks the helper that fixes the Linux/macOS CI
// regression where filepath.Base(`C:\path\wt.exe`) returned the whole
// string and the migrator's switch on `wt.exe` never matched. The helper
// must accept both separators on every host.
func TestCrossPlatformBase(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`C:\path\wt.exe`, "wt.exe"},
		{`C:\Program Files\WezTerm\wezterm-gui.exe`, "wezterm-gui.exe"},
		{"/usr/bin/gnome-terminal", "gnome-terminal"},
		{"/Applications/iTerm.app/Contents/MacOS/iTerm2", "iTerm2"},
		{"open", "open"},
		{"wt.exe", "wt.exe"},
		{"", ""},
		// Mixed separators — Windows config sync'd to Unix sometimes ends
		// up with both. Still resolves to the last segment.
		{`/mnt/c\Windows\wt.exe`, "wt.exe"},
	}
	for _, c := range cases {
		if got := crossPlatformBase(c.in); got != c.want {
			t.Errorf("crossPlatformBase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"PowerShell 7", "powershell-7"},
		{"Ubuntu 24.04.1 LTS", "ubuntu-24-04-1-lts"},
		{"   Trim Me   ", "trim-me"},
		{"Símbolo del sistema", "s-mbolo-del-sistema"}, // non-ASCII collapses
		{"", ""},
	}
	for _, c := range cases {
		if got := slugify(c.in); got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
