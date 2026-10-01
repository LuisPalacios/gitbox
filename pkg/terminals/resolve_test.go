package terminals

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/launch"
)

// isolateUserTerminalConfig points every wezterm.lua / WT settings.json
// candidate at an empty temp dir so LookupForLaunch misses deterministically
// regardless of the host's real config.
func isolateUserTerminalConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("WEZTERM_CONFIG_FILE", filepath.Join(dir, "absent.lua"))
	resetLookupCachesForTest()
	return dir
}

func wtApp() config.TerminalApp {
	return config.TerminalApp{ID: "wt", Name: "Windows Terminal", Command: `C:\wt\wt.exe`,
		ArgsTemplate: []string{"-d", launch.TokenPath, launch.TokenShellCommand, launch.TokenShellArgs}}
}

func pwshShell() config.ShellEntry {
	return config.ShellEntry{ID: "pwsh", Name: "PowerShell 7", Command: `C:\pwsh\pwsh.exe`}
}

func gitBashShell() config.ShellEntry {
	return config.ShellEntry{ID: "git-bash", Name: "Git Bash", Command: `C:\Git\bin\bash.exe`, Args: []string{"--login", "-i"}}
}

const claudeCmd = `C:\Users\me\AppData\Roaming\npm\claude.cmd`

func TestDefaultLaunchProfile(t *testing.T) {
	g := config.GlobalConfig{TerminalProfiles: []config.TerminalProfile{
		{ID: "hidden", Hidden: true, Default: true},
		{ID: "first"},
		{ID: "def", Default: true},
	}}
	p, ok := DefaultLaunchProfile(g)
	if !ok || p.ID != "def" {
		t.Errorf("want visible Default 'def', got %q ok=%v", p.ID, ok)
	}
	g.TerminalProfiles[2].Default = false
	p, ok = DefaultLaunchProfile(g)
	if !ok || p.ID != "first" {
		t.Errorf("want first visible 'first', got %q ok=%v", p.ID, ok)
	}
	if _, ok := DefaultLaunchProfile(config.GlobalConfig{}); ok {
		t.Errorf("empty profile list must report !ok")
	}
}

func TestResolveLaunch_WTGenericTemplate(t *testing.T) {
	isolateUserTerminalConfig(t)
	g := config.GlobalConfig{
		TerminalApps:     []config.TerminalApp{wtApp()},
		Shells:           []config.ShellEntry{pwshShell(), gitBashShell()},
		TerminalProfiles: []config.TerminalProfile{{ID: "wt+pwsh", Name: "WT — PowerShell 7", TerminalID: "wt", ShellID: "pwsh"}, {ID: "wt+git-bash", TerminalID: "wt", ShellID: "git-bash"}},
	}

	t.Run("plain launch unchanged", func(t *testing.T) {
		l, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "wt+pwsh", Path: `C:\repo`, GOOS: "windows"})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"-d", `C:\repo`, `C:\pwsh\pwsh.exe`}
		if l.Command != `C:\wt\wt.exe` || !reflect.DeepEqual(l.Args, want) || l.IsConsole || l.AppleScript != "" {
			t.Errorf("got %+v, want args %v", l, want)
		}
	})
	t.Run("harness wraps pwsh with encoded command", func(t *testing.T) {
		l, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "wt+pwsh", Path: `C:\repo`, GOOS: "windows", HarnessArgv: []string{claudeCmd}})
		if err != nil {
			t.Fatal(err)
		}
		if len(l.Args) != 6 || l.Args[0] != "-d" || l.Args[2] != `C:\pwsh\pwsh.exe` || l.Args[3] != "-NoExit" || l.Args[4] != "-EncodedCommand" {
			t.Fatalf("args = %q", l.Args)
		}
	})
	t.Run("harness wraps git-bash with bare name", func(t *testing.T) {
		l, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "wt+git-bash", Path: `C:\repo`, GOOS: "windows", HarnessArgv: []string{claudeCmd, "--flag"}})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"-d", `C:\repo`, `C:\Git\bin\bash.exe`, "--login", "-i", "-c", `'claude' '--flag'; exec 'C:\Git\bin\bash.exe' '--login' '-i'`}
		if !reflect.DeepEqual(l.Args, want) {
			t.Errorf("args =\n  %q\nwant\n  %q", l.Args, want)
		}
	})
}

func TestResolveLaunch_WTGenericTemplate_EncodedArgsCount(t *testing.T) {
	isolateUserTerminalConfig(t)
	g := config.GlobalConfig{
		TerminalApps:     []config.TerminalApp{wtApp()},
		Shells:           []config.ShellEntry{pwshShell()},
		TerminalProfiles: []config.TerminalProfile{{ID: "wt+pwsh", TerminalID: "wt", ShellID: "pwsh"}},
	}
	l, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "wt+pwsh", Path: `C:\repo`, GOOS: "windows", HarnessArgv: []string{claudeCmd}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-d", `C:\repo`, `C:\pwsh\pwsh.exe`, "-NoExit", "-EncodedCommand"}
	if len(l.Args) != 6 || !reflect.DeepEqual(l.Args[:5], want) {
		t.Fatalf("args = %q", l.Args)
	}
	script, err := launch.PSDecode(l.Args[5])
	if err != nil || script != `& '`+claudeCmd+`'` {
		t.Errorf("script = %q err=%v", script, err)
	}
}

func TestResolveLaunch_WTOverrideAppendsCommandline(t *testing.T) {
	dir := isolateUserTerminalConfig(t)
	settings := filepath.Join(dir, "Microsoft", "Windows Terminal", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"profiles":{"list":[{"name":"PowerShell 7","commandline":"pwsh.exe"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	g := config.GlobalConfig{
		TerminalApps:     []config.TerminalApp{wtApp()},
		Shells:           []config.ShellEntry{pwshShell()},
		TerminalProfiles: []config.TerminalProfile{{ID: "wt+pwsh", TerminalID: "wt", ShellID: "pwsh"}},
	}
	plain, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "wt+pwsh", Path: `C:\repo`, GOOS: "windows"})
	if err != nil {
		t.Fatal(err)
	}
	wantPlain := []string{"-w", "0", "nt", "--profile", "PowerShell 7", "-d", `C:\repo`}
	if !reflect.DeepEqual(plain.Args, wantPlain) {
		t.Errorf("plain args = %q, want %q", plain.Args, wantPlain)
	}
	h, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "wt+pwsh", Path: `C:\repo`, GOOS: "windows", HarnessArgv: []string{claudeCmd}})
	if err != nil {
		t.Fatal(err)
	}
	// The wrapped shell rides as WT's trailing commandline: args[10] is the
	// -EncodedCommand payload (asserted by the generic-template test).
	if len(h.Args) != 11 || !reflect.DeepEqual(h.Args[:7], wantPlain) || h.Args[7] != `C:\pwsh\pwsh.exe` || h.Args[8] != "-NoExit" || h.Args[9] != "-EncodedCommand" {
		t.Errorf("harness args = %q", h.Args)
	}
}

func TestResolveLaunch_WeztermOverrideUsesLaunchMenuShell(t *testing.T) {
	dir := isolateUserTerminalConfig(t)
	lua := filepath.Join(dir, "wezterm.lua")
	if err := os.WriteFile(lua, []byte(`
config.launch_menu = {
  { label = "Git Bash", args = { "C:/Git/bin/bash.exe", "--login" }, set_environment_variables = { FOO = "bar" } },
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEZTERM_CONFIG_FILE", lua)
	resetLookupCachesForTest()
	g := config.GlobalConfig{
		TerminalApps: []config.TerminalApp{{ID: "wezterm", Name: "WezTerm", Command: `C:\wez\wezterm-gui.exe`,
			ArgsTemplate: []string{"start", "--cwd", launch.TokenPath, "--", launch.TokenShellCommand, launch.TokenShellArgs}}},
		Shells:           []config.ShellEntry{gitBashShell()},
		TerminalProfiles: []config.TerminalProfile{{ID: "wezterm+git-bash", TerminalID: "wezterm", ShellID: "git-bash"}},
	}
	plain, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "wezterm+git-bash", Path: `C:\repo`, GOOS: "windows"})
	if err != nil {
		t.Fatal(err)
	}
	wantPlain := []string{"start", "--cwd", `C:\repo`, "--", "C:/Git/bin/bash.exe", "--login"}
	if !reflect.DeepEqual(plain.Args, wantPlain) || plain.Env["FOO"] != "bar" {
		t.Errorf("plain = %+v, want args %q env FOO=bar", plain, wantPlain)
	}
	h, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "wezterm+git-bash", Path: `C:\repo`, GOOS: "windows", HarnessArgv: []string{claudeCmd}})
	if err != nil {
		t.Fatal(err)
	}
	// launch_menu shell (--login, no -i) wins over the gitbox shell entry.
	want := []string{"start", "--cwd", `C:\repo`, "--", "C:/Git/bin/bash.exe", "--login", "-i", "-c", `'claude'; exec 'C:/Git/bin/bash.exe' '--login'`}
	if !reflect.DeepEqual(h.Args, want) || h.Env["FOO"] != "bar" {
		t.Errorf("harness =\n  %q\nwant\n  %q", h.Args, want)
	}
}

func TestResolveLaunch_UnixProfileFallsBackToDefaultShell(t *testing.T) {
	isolateUserTerminalConfig(t)
	g := config.GlobalConfig{
		TerminalApps: []config.TerminalApp{{ID: "gnome-terminal", Name: "GNOME Terminal", Command: "/usr/bin/gnome-terminal",
			ArgsTemplate: []string{"--working-directory=" + launch.TokenPath, "--", launch.TokenShellCommand, launch.TokenShellArgs}}},
		TerminalProfiles: []config.TerminalProfile{{ID: "gnome-terminal", TerminalID: "gnome-terminal"}},
	}
	plain, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "gnome-terminal", Path: "/r", GOOS: "linux", DefaultShell: "/bin/zsh"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--working-directory=/r", "--"}; !reflect.DeepEqual(plain.Args, want) {
		t.Errorf("plain args = %q, want %q", plain.Args, want)
	}
	h, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "gnome-terminal", Path: "/r", GOOS: "linux", DefaultShell: "/bin/zsh", HarnessArgv: []string{"/usr/local/bin/claude"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--working-directory=/r", "--", "/bin/zsh", "-l", "-i", "-c", `'/usr/local/bin/claude'; exec '/bin/zsh' '-l'`}
	if !reflect.DeepEqual(h.Args, want) {
		t.Errorf("harness args =\n  %q\nwant\n  %q", h.Args, want)
	}
	h2, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "gnome-terminal", Path: "/r", GOOS: "linux", HarnessArgv: []string{"claude"}})
	if err != nil {
		t.Fatal(err)
	}
	if h2.Args[2] != "/bin/sh" {
		t.Errorf("empty DefaultShell must fall back to /bin/sh, got %q", h2.Args)
	}
}

func TestResolveLaunch_SingleSlotTemplateJoinsInvocation(t *testing.T) {
	isolateUserTerminalConfig(t)
	g := config.GlobalConfig{
		TerminalApps: []config.TerminalApp{{ID: "tilda", Name: "Tilda", Command: "/usr/bin/tilda",
			ArgsTemplate: []string{"--working-dir=" + launch.TokenPath, "--command", launch.TokenShellCommand}}},
		TerminalProfiles: []config.TerminalProfile{{ID: "tilda", TerminalID: "tilda"}},
	}
	h, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "tilda", Path: "/r", GOOS: "linux", DefaultShell: "/bin/bash", HarnessArgv: []string{"claude"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--working-dir=/r", "--command", `'/bin/bash' '-l' '-i' '-c' ''\''claude'\''; exec '\''/bin/bash'\'' '\''-l'\'''`}
	if !reflect.DeepEqual(h.Args, want) {
		t.Errorf("args =\n  %q\nwant\n  %q", h.Args, want)
	}
}

func TestResolveLaunch_DirectProfileHostsHarness(t *testing.T) {
	isolateUserTerminalConfig(t)
	g := config.GlobalConfig{
		Shells:           []config.ShellEntry{pwshShell()},
		TerminalProfiles: []config.TerminalProfile{{ID: "bare-pwsh", TerminalID: "", ShellID: "pwsh"}},
	}
	plain, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "bare-pwsh", Path: `C:\repo`, GOOS: "windows"})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Command != `C:\pwsh\pwsh.exe` || len(plain.Args) != 0 || !plain.IsConsole {
		t.Errorf("plain = %+v", plain)
	}
	h, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "bare-pwsh", Path: `C:\repo`, GOOS: "windows", HarnessArgv: []string{claudeCmd}})
	if err != nil {
		t.Fatal(err)
	}
	if h.Command != `C:\pwsh\pwsh.exe` || !h.IsConsole || len(h.Args) != 3 || h.Args[0] != "-NoExit" || h.Args[1] != "-EncodedCommand" {
		t.Errorf("harness = %+v", h)
	}
}

func TestResolveLaunch_MacTerminalAppUsesAppleScript(t *testing.T) {
	isolateUserTerminalConfig(t)
	g := config.GlobalConfig{
		TerminalApps: []config.TerminalApp{
			{ID: "terminal", Name: "Terminal", Command: "open", ArgsTemplate: []string{"-a", "Terminal"}},
			{ID: "iterm", Name: "iTerm2", Command: "open", ArgsTemplate: []string{"-a", "iTerm"}},
			{ID: "warp", Name: "Warp", Command: "open", ArgsTemplate: []string{"-a", "Warp"}},
			{ID: "wezterm", Name: "WezTerm", Command: "open", ArgsTemplate: []string{"-n", "-a", "WezTerm", "--args", "start", "--cwd", launch.TokenPath, launch.TokenCommand}},
		},
		TerminalProfiles: []config.TerminalProfile{
			{ID: "terminal", TerminalID: "terminal"}, {ID: "iterm", TerminalID: "iterm"}, {ID: "warp", TerminalID: "warp"}, {ID: "wezterm", TerminalID: "wezterm"},
		},
	}
	plain, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "terminal", Path: "/r", GOOS: "darwin"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"-a", "Terminal", "/r"}; plain.Command != "open" || !reflect.DeepEqual(plain.Args, want) {
		t.Errorf("plain = %+v, want open %q", plain, want)
	}
	for _, id := range []string{"terminal", "iterm"} {
		h, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: id, Path: "/r", GOOS: "darwin", HarnessArgv: []string{"claude"}})
		if err != nil {
			t.Fatal(err)
		}
		if h.AppleScript == "" || !strings.Contains(h.AppleScript, `cd '/r' && 'claude'`) || h.Command != "" {
			t.Errorf("%s: want AppleScript launch, got %+v", id, h)
		}
	}
	if _, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "warp", Path: "/r", GOOS: "darwin", HarnessArgv: []string{"claude"}}); err == nil || !strings.Contains(err.Error(), "cannot run a command") {
		t.Errorf("Warp must refuse a harness launch, got %v", err)
	}
	wez, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "wezterm", Path: "/r", GOOS: "darwin", DefaultShell: "/bin/zsh", HarnessArgv: []string{"claude"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-n", "-a", "WezTerm", "--args", "start", "--cwd", "/r", "/bin/zsh", "-l", "-i", "-c", `'claude'; exec '/bin/zsh' '-l'`}
	if !reflect.DeepEqual(wez.Args, want) {
		t.Errorf("wezterm args =\n  %q\nwant\n  %q", wez.Args, want)
	}
	wezPlain, _ := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "wezterm", Path: "/r", GOOS: "darwin"})
	if want := []string{"-n", "-a", "WezTerm", "--args", "start", "--cwd", "/r"}; !reflect.DeepEqual(wezPlain.Args, want) {
		t.Errorf("wezterm plain args = %q, want %q", wezPlain.Args, want)
	}
}

func TestResolveLaunch_Errors(t *testing.T) {
	isolateUserTerminalConfig(t)
	g := config.GlobalConfig{TerminalProfiles: []config.TerminalProfile{{ID: "orphan", TerminalID: "gone"}}}
	if _, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "nope", Path: "/r"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown profile: %v", err)
	}
	if _, err := ResolveLaunch(LaunchRequest{Global: g, ProfileID: "orphan", Path: "/r"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing terminal app: %v", err)
	}
}
