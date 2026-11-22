package terminals

import (
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/launch"
)

// windowsCatalogApp is the catalog entry of a Windows terminal as detection
// stores it: its ID, a command and the catalog template.
func windowsCatalogApp(t *testing.T, id string) config.TerminalApp {
	t.Helper()
	for _, c := range windowsTerminals {
		if c.ID == id {
			return config.TerminalApp{ID: c.ID, Name: c.Name, Command: `C:\bin\` + id + ".exe", ArgsTemplate: c.ArgsTemplate}
		}
	}
	t.Fatalf("no Windows catalog terminal %q", id)
	return config.TerminalApp{}
}

// TestProgramArgvSafe: on a Windows client only WezTerm and Alacritty with
// their catalog template run a remote terminal as their program (#286);
// Windows Terminal, Tabby, a user-added terminal, a DIRECT shell profile, a
// customised template and every other system fall back to the console.
func TestProgramArgvSafe(t *testing.T) {
	custom := config.TerminalApp{ID: "my-term", Name: "My term", Command: `C:\bin\my.exe`, ArgsTemplate: []string{"-e", launch.TokenShellCommand, launch.TokenShellArgs}}
	cases := []struct {
		name   string
		goos   string
		app    config.TerminalApp
		args   []string
		direct bool
		want   bool
	}{
		{name: "WezTerm", goos: "windows", app: windowsCatalogApp(t, "wezterm"), want: true},
		{name: "Alacritty", goos: "windows", app: windowsCatalogApp(t, "alacritty"), want: true},
		{name: "WezTerm with its template in the profile", goos: "windows", app: windowsCatalogApp(t, "wezterm"),
			args: windowsCatalogApp(t, "wezterm").ArgsTemplate, want: true},
		{name: "WezTerm with a customised template", goos: "windows", app: windowsCatalogApp(t, "wezterm"),
			args: []string{"start", "--", "cmd.exe", "/k", launch.TokenShellCommand, launch.TokenShellArgs}},
		{name: "WezTerm app with an edited template", goos: "windows",
			app: config.TerminalApp{ID: "wezterm", Name: "WezTerm", Command: `C:\bin\wezterm.exe`, ArgsTemplate: []string{"start", "--", "pwsh", "-c", launch.TokenShellCommand}}},
		{name: "Windows Terminal", goos: "windows", app: windowsCatalogApp(t, "wt")},
		{name: "Tabby", goos: "windows", app: windowsCatalogApp(t, "tabby")},
		{name: "a user-added terminal", goos: "windows", app: custom},
		{name: "a DIRECT shell profile", goos: "windows", direct: true},
		{name: "WezTerm on macOS", goos: "darwin", app: windowsCatalogApp(t, "wezterm")},
		{name: "Alacritty on Linux", goos: "linux", app: windowsCatalogApp(t, "alacritty")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := config.GlobalConfig{Shells: []config.ShellEntry{pwshShell()}}
			p := config.TerminalProfile{ID: "p", Name: "P", ShellID: "pwsh", Args: tc.args}
			if !tc.direct {
				g.TerminalApps = []config.TerminalApp{tc.app}
				p.TerminalID = tc.app.ID
			}
			g.TerminalProfiles = []config.TerminalProfile{p}
			if got := ProgramArgvSafe(g, "p", tc.goos); got != tc.want {
				t.Errorf("ProgramArgvSafe = %v, want %v", got, tc.want)
			}
		})
	}
	if ProgramArgvSafe(config.GlobalConfig{}, "missing", "windows") {
		t.Error("an unknown profile is argv-safe")
	}
}
