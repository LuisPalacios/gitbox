package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestResolveTerminalArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		path string
		want []string
	}{
		{
			name: "placeholder replaced and path not appended",
			args: []string{"-d", "{path}"},
			path: `C:\repos\x`,
			want: []string{"-d", `C:\repos\x`},
		},
		{
			name: "placeholder inside arg",
			args: []string{"--cd={path}"},
			path: `C:\foo bar`,
			want: []string{`--cd=C:\foo bar`},
		},
		{
			name: "no placeholder appends path as final arg",
			args: []string{"-a", "Terminal"},
			path: "/Users/me/code",
			want: []string{"-a", "Terminal", "/Users/me/code"},
		},
		{
			// Empty args must stay empty — caller sets cmd.Dir instead.
			name: "empty args preserved (cmd.Dir handles path)",
			args: nil,
			path: "/tmp/x",
			want: nil,
		},
		{
			name: "multiple placeholders all substituted",
			args: []string{"{path}", "--log", "{path}.log"},
			path: "/a",
			want: []string{"/a", "--log", "/a.log"},
		},
		{
			// {command} with nil harnessArgv splices 0 items — safe no-op for
			// terminal-only launches. Path substitution still runs.
			name: "command token with no harness splices nothing",
			args: []string{"--profile", "X", "-d", "{path}", "{command}"},
			path: "/a",
			want: []string{"--profile", "X", "-d", "/a"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveTerminalArgs(tc.args, tc.path)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("resolveTerminalArgs(%v, %q) = %v, want %v",
					tc.args, tc.path, got, tc.want)
			}
		})
	}
}

func TestResolveTerminalArgsWithCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		path        string
		harnessArgv []string
		want        []string
	}{
		{
			name:        "single-item harness splices as one argv entry",
			args:        []string{"-d", "{path}", "{command}"},
			path:        "/r",
			harnessArgv: []string{"claude"},
			want:        []string{"-d", "/r", "claude"},
		},
		{
			name:        "multi-arg harness splices each entry verbatim",
			args:        []string{"--working-directory", "{path}", "-e", "{command}"},
			path:        "/r",
			harnessArgv: []string{"aider", "--model", "claude-sonnet"},
			want:        []string{"--working-directory", "/r", "-e", "aider", "--model", "claude-sonnet"},
		},
		{
			name:        "command as sole token (no path)",
			args:        []string{"{command}"},
			path:        "/ignored",
			harnessArgv: []string{"codex"},
			want:        []string{"codex"},
		},
		{
			name:        "missing {command} splices nothing (harness launch with terminal that doesn't support it — validated upstream)",
			args:        []string{"-d", "{path}"},
			path:        "/r",
			harnessArgv: []string{"codex"},
			want:        []string{"-d", "/r"},
		},
		{
			name:        "multiple {command} tokens splice into each",
			args:        []string{"{command}", "--", "{command}"},
			path:        "/r",
			harnessArgv: []string{"aider", "--yes"},
			want:        []string{"aider", "--yes", "--", "aider", "--yes"},
		},
		{
			name:        "empty args preserved even with harness argv",
			args:        nil,
			path:        "/r",
			harnessArgv: []string{"claude"},
			want:        nil,
		},
		{
			name:        "no {path} and harness set: path is NOT appended",
			args:        []string{"-e", "{command}"},
			path:        "/r",
			harnessArgv: []string{"codex"},
			want:        []string{"-e", "codex"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveTerminalArgsWithCommand(tc.args, tc.path, tc.harnessArgv)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("resolveTerminalArgsWithCommand(%v, %q, %v) = %v, want %v",
					tc.args, tc.path, tc.harnessArgv, got, tc.want)
			}
		})
	}
}

func TestTerminalIDSlugification(t *testing.T) {
	tests := map[string]string{
		"Windows Terminal": "windows-terminal",
		"PowerShell 7":     "powershell-7",
		"iTerm":            "iterm",
		"Xfce Terminal":    "xfce-terminal",
		"   Spaced   Out ": "spaced-out",
		"":                 "",
	}
	for in, want := range tests {
		if got := terminalID(in); got != want {
			t.Errorf("terminalID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSyncTerminalsDedupByName(t *testing.T) {
	// Force the WT-discovery path to fail so the legacy dedup-and-append
	// branch runs and the seeded "Custom" duplicates are exercised. On
	// Windows hosts with a real WT install, the rebuild path would otherwise
	// drop both entries because neither matches a visible WT profile.
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gitbox.json")

	cfg := &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder: "~/x",
			Terminals: []config.TerminalEntry{
				{Name: "Custom", Command: "/bin/foo", Args: []string{"--cd", "{path}"}},
				{Name: "Custom", Command: "/bin/bar"},
			},
		},
		Accounts: map[string]config.Account{
			"A": {Provider: "github", URL: "https://github.com",
				Username: "u", Name: "n", Email: "e@e"},
		},
		Sources: map[string]config.Source{},
	}

	a := &App{cfg: cfg, cfgPath: cfgPath, mu: sync.Mutex{}}
	a.SyncTerminals()

	// Duplicate "Custom" entry must be dropped; the first one kept.
	if n := countByName(cfg.Global.Terminals, "Custom"); n != 1 {
		t.Fatalf("duplicate Custom should collapse to 1; got %d", n)
	}
	kept := findByName(cfg.Global.Terminals, "Custom")
	if kept == nil || kept.Command != "/bin/foo" {
		t.Errorf("first duplicate should be kept; got %+v", kept)
	}
}

func TestSyncTerminalsUpgradesLegacyNonWindowsEntries(t *testing.T) {
	// Users on non-Windows platforms who launched gitbox before {command}
	// templates landed have entries like:
	//   {"GNOME Terminal", "gnome-terminal", ["--working-directory={path}"]}
	// SyncTerminals must upgrade those to the new templates with {command}
	// so harness launches succeed without manual edits. Customized entries
	// (different flags, different command path) are left alone.
	if isWindows() {
		t.Skip("upgrade path is non-Windows only (Windows has its own WT-driven rebuild)")
	}
	if len(knownTerminals) == 0 {
		t.Skip("no known terminals on this platform")
	}

	// Find a candidate whose current template has {command} and whose
	// legacy shape (stripped of {command}) is distinguishable.
	var cand *knownTerminalCandidate
	var candCmd string
	var candArgs []string
	for i := range knownTerminals {
		c, args, ok := knownTerminals[i].Resolve()
		if !ok {
			continue
		}
		stripped := stripCommandTokens(args)
		if len(stripped) == len(args) {
			continue
		}
		cand = &knownTerminals[i]
		candCmd = c
		candArgs = args
		break
	}
	if cand == nil {
		t.Skip("no {command}-aware terminal candidate resolved on this host")
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gitbox.json")
	legacy := stripCommandTokens(candArgs)
	cfg := &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder: "~/x",
			Terminals: []config.TerminalEntry{
				{Name: cand.Name, Command: candCmd, Args: legacy},
			},
		},
		Accounts: map[string]config.Account{
			"A": {Provider: "github", URL: "https://github.com",
				Username: "u", Name: "n", Email: "e@e"},
		},
		Sources: map[string]config.Source{},
	}
	a := &App{cfg: cfg, cfgPath: cfgPath, mu: sync.Mutex{}}
	a.SyncTerminals()

	got := findByName(cfg.Global.Terminals, cand.Name)
	if got == nil {
		t.Fatalf("%s disappeared after sync", cand.Name)
	}
	if !reflect.DeepEqual(got.Args, candArgs) {
		t.Errorf("legacy %s args should be upgraded:\n got  %v\n want %v", cand.Name, got.Args, candArgs)
	}
}

func TestStripCommandTokens(t *testing.T) {
	tests := []struct {
		in   []string
		want []string
	}{
		{[]string{"-d", "{path}", "{command}"}, []string{"-d", "{path}"}},
		{[]string{"{command}"}, []string{}},
		{[]string{"-d", "{path}"}, []string{"-d", "{path}"}},
		{[]string{}, []string{}},
		{[]string{"{command}", "--", "{command}"}, []string{"--"}},
	}
	for _, tc := range tests {
		got := stripCommandTokens(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("stripCommandTokens(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSyncTerminalsPreservesUserEdits(t *testing.T) {
	// Same reason as TestSyncTerminalsDedupByName: force WT discovery to fail
	// so the legacy candidate-merge path is what's under test here.
	t.Setenv("LOCALAPPDATA", t.TempDir())
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gitbox.json")

	// Seed config with a user-edited copy of a known terminal name — use the
	// first platform candidate's name so the sync loop would otherwise want to
	// overwrite it. We verify the existing Command/Args are NOT clobbered.
	if len(knownTerminals) == 0 {
		t.Skip("no known terminals for this platform")
	}
	known := knownTerminals[0].Name
	userCommand := "/custom/path/to/launcher"
	userArgs := []string{"--custom", "{path}"}

	cfg := &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder: "~/x",
			Terminals: []config.TerminalEntry{
				{Name: known, Command: userCommand, Args: userArgs},
			},
		},
		Accounts: map[string]config.Account{
			"A": {Provider: "github", URL: "https://github.com",
				Username: "u", Name: "n", Email: "e@e"},
		},
		Sources: map[string]config.Source{},
	}

	a := &App{cfg: cfg, cfgPath: cfgPath, mu: sync.Mutex{}}
	a.SyncTerminals()

	got := findByName(cfg.Global.Terminals, known)
	if got == nil {
		t.Fatalf("terminal %q disappeared after sync", known)
	}
	if got.Command != userCommand {
		t.Errorf("user-edited command clobbered: got %q, want %q", got.Command, userCommand)
	}
	if !reflect.DeepEqual(got.Args, userArgs) {
		t.Errorf("user-edited args clobbered: got %v, want %v", got.Args, userArgs)
	}
}

func TestDetectTerminalsIncludesConfigEntries(t *testing.T) {
	// Disable WT discovery so DetectTerminals goes through the legacy merge
	// path that surfaces user-defined config entries alongside detected
	// platform terminals.
	t.Setenv("LOCALAPPDATA", t.TempDir())
	cfg := &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder: "~/x",
			Terminals: []config.TerminalEntry{
				{Name: "MyShell", Command: "/bin/myshell", Args: []string{"-C", "{path}"}},
			},
		},
		Accounts: map[string]config.Account{
			"A": {Provider: "github", URL: "https://github.com",
				Username: "u", Name: "n", Email: "e@e"},
		},
		Sources: map[string]config.Source{},
	}
	a := &App{cfg: cfg}
	found := a.DetectTerminals()

	var match *TerminalInfo
	for i := range found {
		if found[i].Name == "MyShell" {
			match = &found[i]
			break
		}
	}
	if match == nil {
		t.Fatalf("config-defined terminal should appear in DetectTerminals output")
	}
	if match.ID != "myshell" || match.Command != "/bin/myshell" {
		t.Errorf("unexpected DTO: %+v", match)
	}
	if !reflect.DeepEqual(match.Args, []string{"-C", "{path}"}) {
		t.Errorf("args should round-trip verbatim; got %v", match.Args)
	}
}

func TestMsysToWindowsPath(t *testing.T) {
	tests := map[string]string{
		`/c/Users/luis/AppData/Local`: `C:\Users\luis\AppData\Local`,
		`/d/code/repo`:                `D:\code\repo`,
		`/c`:                          `C:`,
		`C:\already\windows`:          `C:\already\windows`,
		`/not/a/drive/path`:           `/not/a/drive/path`,
		``:                            ``,
		`/`:                           `/`,
	}
	for in, want := range tests {
		if got := msysToWindowsPath(in); got != want {
			t.Errorf("msysToWindowsPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeWindowsTerminalEnv(t *testing.T) {
	in := []string{
		"MSYSTEM=MINGW64",
		"MSYS_NO_PATHCONV=1",
		"LOCALAPPDATA=/c/Users/luis/AppData/Local",
		"APPDATA=/c/Users/luis/AppData/Roaming",
		"USERPROFILE=/c/Users/luis",
		"TEMP=/c/Users/luis/AppData/Local/Temp",
		"PATH=/usr/bin:/mingw64/bin",  // not normalised (deliberate)
		"FOO=/c/not-normalised",       // unknown key kept as-is
		"PS1=> ",
	}
	out := sanitizeWindowsTerminalEnv(in)

	// MSYSTEM / MSYS_NO_PATHCONV must be dropped.
	for _, e := range out {
		if strings.HasPrefix(e, "MSYSTEM=") || strings.HasPrefix(e, "MSYS_NO_PATHCONV=") {
			t.Errorf("MSYS marker should be dropped; still present: %q", e)
		}
	}
	// Known Windows vars should be normalised.
	wantNormalised := map[string]string{
		"LOCALAPPDATA": `C:\Users\luis\AppData\Local`,
		"APPDATA":      `C:\Users\luis\AppData\Roaming`,
		"USERPROFILE":  `C:\Users\luis`,
		"TEMP":         `C:\Users\luis\AppData\Local\Temp`,
	}
	for k, want := range wantNormalised {
		found := false
		for _, e := range out {
			if strings.HasPrefix(e, k+"=") {
				found = true
				if got := strings.TrimPrefix(e, k+"="); got != want {
					t.Errorf("%s = %q, want %q", k, got, want)
				}
			}
		}
		if !found {
			t.Errorf("%s missing from sanitised env", k)
		}
	}
	// Unknown keys kept verbatim.
	for _, e := range out {
		if e == "FOO=/c/not-normalised" {
			return
		}
	}
	t.Error("unknown key FOO was not preserved verbatim")
}

func TestStripJSONComments(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no comments",
			in:   `{"a": 1}`,
			want: `{"a": 1}`,
		},
		{
			name: "line comment",
			in:   "{\n  // pick a profile\n  \"a\": 1\n}",
			want: "{\n  \n  \"a\": 1\n}",
		},
		{
			name: "block comment",
			in:   `{ /* hello */ "a": 1 }`,
			want: `{  "a": 1 }`,
		},
		{
			name: "comment markers inside string preserved",
			in:   `{"name": "// not a comment", "p": "/* still string */"}`,
			want: `{"name": "// not a comment", "p": "/* still string */"}`,
		},
		{
			name: "escaped quote inside string",
			in:   `{"name": "say \"hi\" //x"}`,
			want: `{"name": "say \"hi\" //x"}`,
		},
		{
			name: "block comment with stars inside",
			in:   `a /* one ** two */ b`,
			want: `a  b`,
		},
		{
			name: "trailing line comment without newline",
			in:   `{"a": 1} // tail`,
			want: `{"a": 1} `,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(stripJSONComments([]byte(tc.in)))
			if got != tc.want {
				t.Errorf("stripJSONComments(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseWTProfiles(t *testing.T) {
	const wtCmd = `C:\Users\test\AppData\Local\Microsoft\WindowsApps\wt.exe`

	tests := []struct {
		name      string
		settings  string
		wantNames []string
		wantErr   bool
	}{
		{
			name: "all profiles visible",
			settings: `{
				"profiles": {
					"list": [
						{"name": "PowerShell"},
						{"name": "Command Prompt"},
						{"name": "Git Bash"}
					]
				}
			}`,
			wantNames: []string{"PowerShell", "Command Prompt", "Git Bash"},
		},
		{
			name: "hidden profile skipped",
			settings: `{
				"profiles": {
					"list": [
						{"name": "PowerShell"},
						{"name": "Azure", "hidden": true},
						{"name": "Ubuntu"}
					]
				}
			}`,
			wantNames: []string{"PowerShell", "Ubuntu"},
		},
		{
			name: "hidden=false treated as visible",
			settings: `{
				"profiles": {
					"list": [
						{"name": "Foo", "hidden": false}
					]
				}
			}`,
			wantNames: []string{"Foo"},
		},
		{
			name: "JSONC comments stripped before parse",
			settings: `{
				// top-level comment
				"profiles": {
					"list": [
						/* block comment */
						{"name": "PowerShell"},
						{"name": "Ubuntu"} // trailing
					]
				}
			}`,
			wantNames: []string{"PowerShell", "Ubuntu"},
		},
		{
			name: "non-ASCII profile names round-trip",
			settings: `{
				"profiles": {
					"list": [
						{"name": "Símbolo del sistema"},
						{"name": "终端"}
					]
				}
			}`,
			wantNames: []string{"Símbolo del sistema", "终端"},
		},
		{
			name:     "missing profiles.list",
			settings: `{"defaultProfile": "{abc}"}`,
			wantErr:  true,
		},
		{
			name:     "malformed JSON",
			settings: `{"profiles": {`,
			wantErr:  true,
		},
		{
			name:     "all profiles hidden",
			settings: `{"profiles": {"list": [{"name": "Foo", "hidden": true}]}}`,
			wantErr:  true,
		},
		{
			name:     "empty name skipped",
			settings: `{"profiles": {"list": [{"name": ""}, {"name": "Real"}]}}`,
			wantNames: []string{"Real"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseWTProfiles([]byte(tc.settings), wtCmd)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got profiles %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.wantNames) {
				t.Fatalf("got %d profiles, want %d (%+v)", len(got), len(tc.wantNames), got)
			}
			for i, p := range got {
				if p.Name != tc.wantNames[i] {
					t.Errorf("profile[%d].Name = %q, want %q", i, p.Name, tc.wantNames[i])
				}
				if p.Command != wtCmd {
					t.Errorf("profile[%d].Command = %q, want %q", i, p.Command, wtCmd)
				}
				wantArgs := []string{"--profile", tc.wantNames[i], "-d", "{path}", "{command}"}
				if !reflect.DeepEqual(p.Args, wantArgs) {
					t.Errorf("profile[%d].Args = %v, want %v", i, p.Args, wantArgs)
				}
			}
		})
	}
}

func TestParseWTProfilesDisabledSources(t *testing.T) {
	// Profiles whose `source` is in `disabledProfileSources` must be skipped
	// even when their `hidden` flag is absent — this matches WT's own menu
	// rendering rules (e.g. Visual Studio dynamic profiles disabled wholesale).
	settings := `{
		"disabledProfileSources": ["Windows.Terminal.VisualStudio", "Windows.Terminal.Azure"],
		"profiles": {
			"list": [
				{"name": "PowerShell"},
				{"name": "DevPS", "source": "Windows.Terminal.VisualStudio"},
				{"name": "Azure", "source": "Windows.Terminal.Azure"},
				{"name": "Ubuntu", "source": "CanonicalGroupLimited.Ubuntu"}
			]
		}
	}`
	got, err := parseWTProfiles([]byte(settings), "wt.exe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantNames := []string{"PowerShell", "Ubuntu"}
	if len(got) != len(wantNames) {
		t.Fatalf("got %d profiles, want %d (%+v)", len(got), len(wantNames), got)
	}
	for i, p := range got {
		if p.Name != wantNames[i] {
			t.Errorf("profile[%d].Name = %q, want %q", i, p.Name, wantNames[i])
		}
	}
}

func TestMergeWTProfilesPreservesUserCustomization(t *testing.T) {
	profiles := []config.TerminalEntry{
		{Name: "PowerShell", Command: "wt.exe", Args: []string{"--profile", "PowerShell", "-d", "{path}", "{command}"}},
		{Name: "Ubuntu", Command: "wt.exe", Args: []string{"--profile", "Ubuntu", "-d", "{path}", "{command}"}},
	}
	cfg := &config.Config{
		Global: config.GlobalConfig{
			Terminals: []config.TerminalEntry{
				// User customized "PowerShell" with extra flag.
				{Name: "PowerShell", Command: "wt.exe", Args: []string{"--profile", "PowerShell", "--maximized", "-d", "{path}"}},
				// Stale legacy entry — must be dropped.
				{Name: "Git Bash", Command: "C:\\Program Files\\Git\\git-bash.exe", Args: []string{"--cd={path}"}},
			},
		},
	}
	got := mergeWTProfilesWithConfig(profiles, cfg)

	if len(got) != 2 {
		t.Fatalf("merge should produce exactly the profile count; got %d (%+v)", len(got), got)
	}
	if got[0].Name != "PowerShell" || got[1].Name != "Ubuntu" {
		t.Errorf("WT order must be preserved; got %v", []string{got[0].Name, got[1].Name})
	}
	// User customization preserved for "PowerShell" (args differ from legacy
	// template, so the seamless upgrade heuristic does NOT apply).
	if !reflect.DeepEqual(got[0].Args, []string{"--profile", "PowerShell", "--maximized", "-d", "{path}"}) {
		t.Errorf("PowerShell user args clobbered; got %v", got[0].Args)
	}
	// Default WT entry used for "Ubuntu" (no prior customization).
	if !reflect.DeepEqual(got[1].Args, []string{"--profile", "Ubuntu", "-d", "{path}", "{command}"}) {
		t.Errorf("Ubuntu should use default args; got %v", got[1].Args)
	}
}

func TestMergeWTProfilesUpgradesLegacyTemplate(t *testing.T) {
	// Users who had the pre-{command} auto-generated template in their config
	// should be seamlessly upgraded so harness launches work without manual
	// edits. Only the exact legacy shape is rewritten; anything custom stays.
	profiles := []config.TerminalEntry{
		{Name: "PowerShell", Command: "wt.exe", Args: []string{"--profile", "PowerShell", "-d", "{path}", "{command}"}},
	}
	cfg := &config.Config{
		Global: config.GlobalConfig{
			Terminals: []config.TerminalEntry{
				// Exact legacy shape.
				{Name: "PowerShell", Command: "wt.exe", Args: []string{"--profile", "PowerShell", "-d", "{path}"}},
			},
		},
	}
	got := mergeWTProfilesWithConfig(profiles, cfg)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry; got %d", len(got))
	}
	wantArgs := []string{"--profile", "PowerShell", "-d", "{path}", "{command}"}
	if !reflect.DeepEqual(got[0].Args, wantArgs) {
		t.Errorf("legacy template should be upgraded; got %v, want %v", got[0].Args, wantArgs)
	}
}

func TestSyncTerminalsRebuildsFromWT(t *testing.T) {
	// Stage a fake LOCALAPPDATA with a WT settings.json so discoverWTProfiles
	// succeeds without depending on a real WT install. Tests must run on any
	// platform; isWindows() gates the rebuild path so on non-Windows hosts
	// this test exercises the legacy merge path instead — short-circuit there.
	if !isWindows() {
		t.Skip("WT rebuild path is Windows-only")
	}
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)

	// Lay down the WindowsApps\wt.exe alias so wtExePath() succeeds.
	wtDir := filepath.Join(local, "Microsoft", "WindowsApps")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatalf("mkdir WindowsApps: %v", err)
	}
	wtPath := filepath.Join(wtDir, "wt.exe")
	if err := os.WriteFile(wtPath, []byte{}, 0o644); err != nil {
		t.Fatalf("write wt.exe stub: %v", err)
	}

	// Lay down a Store-build settings.json with three profiles, one hidden.
	settingsDir := filepath.Join(local, "Packages", "Microsoft.WindowsTerminal_8wekyb3d8bbwe", "LocalState")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings: %v", err)
	}
	settings := `{
		"profiles": {
			"list": [
				{"name": "Git Bash"},
				{"name": "PowerShell 7"},
				{"name": "Hidden Thing", "hidden": true},
				{"name": "Ubuntu"}
			]
		}
	}`
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "gitbox.json")
	cfg := &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder: "~/x",
			Terminals: []config.TerminalEntry{
				// Legacy bare-binary entries that must be pruned.
				{Name: "Windows Terminal", Command: "C:\\wt.exe", Args: []string{"-d", "{path}"}},
				{Name: "PowerShell 5", Command: "C:\\powershell.exe"},
				// Stale entry for a now-hidden profile — must be pruned.
				{Name: "Hidden Thing", Command: "C:\\old\\wt.exe", Args: []string{"--profile", "Hidden Thing"}},
				// User-customized entry matching a current visible profile —
				// command/args must be preserved verbatim (differ from the
				// legacy auto-template in a meaningful way, so the seamless
				// upgrade heuristic does NOT apply).
				{Name: "PowerShell 7", Command: wtPath, Args: []string{"--profile", "PowerShell 7", "--maximized", "-d", "{path}"}},
			},
		},
		Accounts: map[string]config.Account{
			"A": {Provider: "github", URL: "https://github.com",
				Username: "u", Name: "n", Email: "e@e"},
		},
		Sources: map[string]config.Source{},
	}

	a := &App{cfg: cfg, cfgPath: cfgPath, mu: sync.Mutex{}}
	a.SyncTerminals()

	// Result must match WT order (visible only): Git Bash, PowerShell 7, Ubuntu.
	wantNames := []string{"Git Bash", "PowerShell 7", "Ubuntu"}
	if len(cfg.Global.Terminals) != len(wantNames) {
		t.Fatalf("got %d terminals, want %d: %+v", len(cfg.Global.Terminals), len(wantNames), cfg.Global.Terminals)
	}
	for i, t0 := range cfg.Global.Terminals {
		if t0.Name != wantNames[i] {
			t.Errorf("terminal[%d].Name = %q, want %q", i, t0.Name, wantNames[i])
		}
	}

	// PowerShell 7 customization preserved.
	ps7 := findByName(cfg.Global.Terminals, "PowerShell 7")
	if ps7 == nil {
		t.Fatal("PowerShell 7 missing")
	}
	wantPS7Args := []string{"--profile", "PowerShell 7", "--maximized", "-d", "{path}"}
	if !reflect.DeepEqual(ps7.Args, wantPS7Args) {
		t.Errorf("PowerShell 7 customization lost: got %v, want %v", ps7.Args, wantPS7Args)
	}
}

func TestDiscoverWTProfilesFileMissing(t *testing.T) {
	// Point LOCALAPPDATA at an empty temp dir so all candidate paths are absent
	// and wt.exe alias lookup fails. The function must return an error so the
	// caller falls back to bare-binary candidates.
	t.Setenv("LOCALAPPDATA", t.TempDir())
	_, err := discoverWTProfiles()
	if err == nil {
		t.Error("expected error when no WT install present")
	}
}

func countByName(entries []config.TerminalEntry, name string) int {
	n := 0
	for _, e := range entries {
		if e.Name == name {
			n++
		}
	}
	return n
}

func findByName(entries []config.TerminalEntry, name string) *config.TerminalEntry {
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i]
		}
	}
	return nil
}
