package terminals

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func writeWeztermLua(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "wezterm.lua")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write wezterm.lua: %v", err)
	}
	return p
}

func writeWTSettings(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}
	return p
}

func TestLookupWeztermEntry_HitWithEnv(t *testing.T) {
	resetLookupCachesForTest()
	path := writeWeztermLua(t, `
config.launch_menu = {
  {
    label = "PowerShell 7",
    args = { "pwsh.exe", "-NoLogo" },
    set_environment_variables = { OMP = "/etc/omp" },
  },
  {
    label = "Bash",
    args = { "bash", "-l" },
  },
}
`)

	got, ok := lookupWeztermEntry("pwsh", "PowerShell 7", []string{path})
	if !ok {
		t.Fatalf("expected hit for pwsh")
	}
	wantArgv := []string{"start", "--cwd", "{path}", "--", "pwsh.exe", "-NoLogo"}
	if !reflect.DeepEqual(got.Argv, wantArgv) {
		t.Errorf("Argv = %v, want %v", got.Argv, wantArgv)
	}
	if got.Env["OMP"] != "/etc/omp" {
		t.Errorf("Env[OMP] = %q, want /etc/omp", got.Env["OMP"])
	}
}

func TestLookupWeztermEntry_MissDoesNotFallthroughToNextCandidate(t *testing.T) {
	// When the user's first wezterm.lua exists but has no matching entry,
	// stop scanning. Otherwise a stale fallback path (e.g. an old
	// ~/.wezterm.lua from a previous setup) could shadow the user's actual
	// config.
	resetLookupCachesForTest()
	first := writeWeztermLua(t, `
config.launch_menu = {
  { label = "Bash", args = { "bash" } },
}
`)
	second := writeWeztermLua(t, `
config.launch_menu = {
  { label = "PowerShell 7", args = { "pwsh.exe" } },
}
`)
	_, ok := lookupWeztermEntry("pwsh", "PowerShell 7", []string{first, second})
	if ok {
		t.Errorf("expected miss when first file exists but has no match; second candidate must not be consulted")
	}
}

func TestLookupWeztermEntry_NoFile(t *testing.T) {
	resetLookupCachesForTest()
	_, ok := lookupWeztermEntry("pwsh", "PowerShell 7", []string{filepath.Join(t.TempDir(), "missing.lua")})
	if ok {
		t.Errorf("expected miss when no file exists")
	}
}

func TestLookupWeztermEntry_FuzzyMatchByPattern(t *testing.T) {
	// Entry labelled "pwsh" (the gitbox shell ID style) should match the
	// "pwsh" gitbox shell via the pattern fallback even though the gitbox
	// display name is "PowerShell 7".
	resetLookupCachesForTest()
	path := writeWeztermLua(t, `
config.launch_menu = {
  { label = "pwsh", args = { "pwsh.exe" } },
}
`)
	_, ok := lookupWeztermEntry("pwsh", "PowerShell 7", []string{path})
	if !ok {
		t.Errorf("expected fuzzy hit on label 'pwsh'")
	}
}

func TestLookupWeztermEntry_MalformedConfigFallsBackToMiss(t *testing.T) {
	// A wezterm.lua with a broken launch_menu shouldn't surface as an
	// error to the launch path — fall through to "no override" so the
	// generic template still works.
	resetLookupCachesForTest()
	path := writeWeztermLua(t, `config.launch_menu = { broken`)
	_, ok := lookupWeztermEntry("pwsh", "PowerShell 7", []string{path})
	if ok {
		t.Errorf("expected miss on malformed config")
	}
}

func TestLookupWeztermEntry_CacheInvalidatesOnMtime(t *testing.T) {
	// Editing the file should be picked up on the next call without
	// requiring a process restart.
	resetLookupCachesForTest()
	dir := t.TempDir()
	path := filepath.Join(dir, "wezterm.lua")
	v1 := `config.launch_menu = { { label = "Bash", args = { "bash" } } }`
	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatalf("write v1: %v", err)
	}

	if _, ok := lookupWeztermEntry("pwsh", "PowerShell 7", []string{path}); ok {
		t.Errorf("v1 should miss on pwsh")
	}

	// Bump mtime forward to dodge filesystem timestamp granularity (FAT/
	// some Windows configurations only have 2 s resolution).
	v2 := `config.launch_menu = { { label = "PowerShell 7", args = { "pwsh.exe" } } }`
	if err := os.WriteFile(path, []byte(v2), 0o644); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if _, ok := lookupWeztermEntry("pwsh", "PowerShell 7", []string{path}); !ok {
		t.Errorf("v2 should hit on pwsh after re-read")
	}
}

func TestLookupWTProfile_Hit(t *testing.T) {
	resetLookupCachesForTest()
	path := writeWTSettings(t, `{
  "profiles": {
    "list": [
      { "name": "PowerShell 7", "commandline": "pwsh.exe" },
      { "name": "Command Prompt", "commandline": "cmd.exe" }
    ]
  }
}`)
	got, ok := lookupWTProfile("pwsh", "PowerShell 7", []string{path})
	if !ok {
		t.Fatalf("expected WT hit on PowerShell 7")
	}
	// `-w 0 nt` pins the launch to the most-recent existing WT window
	// (or a new one when none exists) so `firstWindowPreference:
	// persistedWindowLayout` doesn't spawn a second window beside ours.
	wantArgv := []string{"-w", "0", "nt", "--profile", "PowerShell 7", "-d", "{path}"}
	if !reflect.DeepEqual(got.Argv, wantArgv) {
		t.Errorf("Argv = %v, want %v", got.Argv, wantArgv)
	}
	if len(got.Env) != 0 {
		t.Errorf("WT override should not carry Env; got %v", got.Env)
	}
}

func TestLookupWTProfile_HiddenSkipped(t *testing.T) {
	resetLookupCachesForTest()
	path := writeWTSettings(t, `{
  "profiles": {
    "list": [
      { "name": "PowerShell 7", "hidden": true, "commandline": "pwsh.exe" }
    ]
  }
}`)
	if _, ok := lookupWTProfile("pwsh", "PowerShell 7", []string{path}); ok {
		t.Errorf("hidden profile must not match")
	}
}

func TestLookupWTProfile_DisabledSourceSkipped(t *testing.T) {
	resetLookupCachesForTest()
	path := writeWTSettings(t, `{
  "disabledProfileSources": ["Windows.Terminal.Wsl"],
  "profiles": {
    "list": [
      { "name": "Ubuntu-24.04", "source": "Windows.Terminal.Wsl" }
    ]
  }
}`)
	if _, ok := lookupWTProfile("wsl-ubuntu-24-04", "WSL — Ubuntu-24.04", []string{path}); ok {
		t.Errorf("profile with disabled source must not match")
	}
}

func TestLookupForLaunch_BareShellSkipped(t *testing.T) {
	if _, ok := LookupForLaunch("", "pwsh", "PowerShell 7"); ok {
		t.Errorf("bare-shell DIRECT profile (terminalID=='') must not consult user config")
	}
	if _, ok := LookupForLaunch("wt", "", ""); ok {
		t.Errorf("missing shellID must not consult user config")
	}
}

func TestLookupForLaunch_UnknownTerminal(t *testing.T) {
	if _, ok := LookupForLaunch("alacritty", "pwsh", "PowerShell 7"); ok {
		t.Errorf("unknown terminal id must yield miss")
	}
}

func TestMatchesShell(t *testing.T) {
	cases := []struct {
		desc      string
		entryName string
		shellID   string
		shellName string
		want      bool
	}{
		{"exact PowerShell 7", "PowerShell 7", "pwsh", "PowerShell 7", true},
		{"exact pwsh fallback", "pwsh", "pwsh", "PowerShell 7", true},
		{"PowerShell 5 vs pwsh", "PowerShell 5", "pwsh", "PowerShell 7", false},
		{"PowerShell 5 hits", "PowerShell 5", "powershell", "PowerShell 5", true},
		{"Windows PowerShell pattern", "Windows PowerShell", "powershell", "PowerShell 5", true},
		{"command prompt", "Command Prompt", "cmd", "Command Prompt", true},
		{"git bash", "Git Bash", "git-bash", "Git Bash", true},
		{"WSL exact w/ em-dash", "WSL — Ubuntu-24.04", "wsl-ubuntu-24-04", "WSL — Ubuntu-24.04", true},
		{"WSL suffix-only", "Ubuntu-24.04", "wsl-ubuntu-24-04", "WSL — Ubuntu-24.04", true},
		{"WSL bare distro slug", "Ubuntu 24 04", "wsl-ubuntu-24-04", "WSL — Ubuntu-24.04", true},
		{"WSL wrong distro", "Debian", "wsl-ubuntu-24-04", "WSL — Ubuntu-24.04", false},
		{"empty entry name", "", "pwsh", "PowerShell 7", false},
		{"unrelated entry", "iTerm2", "pwsh", "PowerShell 7", false},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			got := matchesShell(c.entryName, c.shellID, c.shellName)
			if got != c.want {
				t.Errorf("matchesShell(%q, %q, %q) = %v; want %v",
					c.entryName, c.shellID, c.shellName, got, c.want)
			}
		})
	}
}

func TestNormalizeName(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		"PowerShell 7":      "powershell 7",
		"powershell-7":      "powershell 7",
		"powershell  7":     "powershell 7",
		"WSL — Ubuntu-24":   "wsl ubuntu 24",
		"Command Prompt":    "command prompt",
		"  spaced   stuff ": "spaced stuff",
	}
	for in, want := range cases {
		if got := normalizeName(in); got != want {
			t.Errorf("normalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}
