package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestSyncEditorsPrunesHarnessClaimedNames(t *testing.T) {
	// User upgrades from a pre-#23 build where Cursor was auto-added as an
	// editor. After the upgrade, Cursor is an AI harness instead — the
	// editor entry must be pruned on the next SyncEditors run so the kebab
	// doesn't show "Open in Cursor" under both sections.
	dir := t.TempDir()
	cfg := &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder: dir,
			Editors: []config.EditorEntry{
				{Name: "VS Code", Command: "/usr/bin/code"},
				{Name: "Cursor", Command: "/usr/bin/cursor"},
				{Name: "Zed", Command: "/usr/bin/zed"},
			},
		},
		Accounts: map[string]config.Account{
			"A": {Provider: "github", URL: "https://github.com",
				Username: "u", Name: "n", Email: "e@e"},
		},
		Sources: map[string]config.Source{},
	}
	a := &App{cfg: cfg, cfgPath: filepath.Join(dir, "gitbox.json"), mu: sync.Mutex{}}
	a.SyncEditors()

	for _, e := range cfg.Global.Editors {
		if e.Name == "Cursor" {
			t.Errorf("Cursor should have been pruned from global.editors: %+v", cfg.Global.Editors)
		}
	}
}

func TestDetectEditorsExcludesHarnessClaimedNames(t *testing.T) {
	cfg := &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder: "~/x",
			Editors: []config.EditorEntry{
				{Name: "Cursor", Command: "/usr/bin/cursor"},
			},
		},
		Accounts: map[string]config.Account{
			"A": {Provider: "github", URL: "https://github.com",
				Username: "u", Name: "n", Email: "e@e"},
		},
		Sources: map[string]config.Source{},
	}
	a := &App{cfg: cfg}
	for _, e := range a.DetectEditors() {
		if e.Name == "Cursor" {
			t.Errorf("DetectEditors should skip Cursor (claimed by harness): %+v", e)
		}
	}
}

func TestKnownAIHarnessesWiredFromEmbed(t *testing.T) {
	// Proves the embed + parser chain produced a non-empty list at package
	// init. The pkg/harness package has its own parser unit tests; this one
	// is a smoke check that the cmd/gui side assembled its candidate list.
	if len(knownAIHarnesses) == 0 {
		t.Fatal("knownAIHarnesses is empty — embed or parser chain broke")
	}
	// Every candidate must have both a display name and an identifier-shaped command.
	for _, h := range knownAIHarnesses {
		if h.Name == "" {
			t.Errorf("candidate has empty Name: %+v", h)
		}
		if h.Command == "" {
			t.Errorf("candidate %q has empty Command", h.Name)
		}
	}
}

func TestHarnessIDSlugification(t *testing.T) {
	tests := map[string]string{
		"Claude Code":  "claude-code",
		"Codex":        "codex",
		"Cursor Agent": "cursor-agent",
		"OpenCode":     "opencode",
		"":             "",
	}
	for in, want := range tests {
		if got := harnessID(in); got != want {
			t.Errorf("harnessID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildHarnessArgv(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		want    []string
	}{
		{
			name:    "command only",
			command: "claude",
			want:    []string{"claude"},
		},
		{
			name:    "command with args",
			command: "aider",
			args:    []string{"--yes", "--model", "sonnet"},
			want:    []string{"aider", "--yes", "--model", "sonnet"},
		},
		{
			name:    "empty command returns nil",
			command: "",
			want:    nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildHarnessArgv(tc.command, tc.args)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildHarnessArgv(%q, %v) = %v, want %v", tc.command, tc.args, got, tc.want)
			}
		})
	}
}

func TestDetectAIHarnessesIncludesConfigEntries(t *testing.T) {
	// A config-defined harness must appear in DetectAIHarnesses output so the
	// GUI can render custom entries users added by hand (harnesses not in
	// knownAIHarnesses or not installed on PATH at the moment).
	cfg := &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder: "~/x",
			AIHarnesses: []config.AIHarnessEntry{
				{Name: "MyCustomBot", Command: "/opt/bots/mybot", Args: []string{"--chatty"}},
			},
		},
		Accounts: map[string]config.Account{
			"A": {Provider: "github", URL: "https://github.com",
				Username: "u", Name: "n", Email: "e@e"},
		},
		Sources: map[string]config.Source{},
	}
	a := &App{cfg: cfg}
	found := a.DetectAIHarnesses()

	var match *AIHarnessInfo
	for i := range found {
		if found[i].Name == "MyCustomBot" {
			match = &found[i]
			break
		}
	}
	if match == nil {
		t.Fatalf("config-defined harness should appear in DetectAIHarnesses output; got %+v", found)
	}
	if match.ID != "mycustombot" || match.Command != "/opt/bots/mybot" {
		t.Errorf("unexpected DTO: %+v", match)
	}
	if !reflect.DeepEqual(match.Args, []string{"--chatty"}) {
		t.Errorf("args should round-trip verbatim; got %v", match.Args)
	}
}

func TestSyncAIHarnessesDedupByName(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gitbox.json")
	cfg := &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder: "~/x",
			AIHarnesses: []config.AIHarnessEntry{
				{Name: "Duplicated", Command: "/bin/first"},
				{Name: "Duplicated", Command: "/bin/second"},
			},
		},
		Accounts: map[string]config.Account{
			"A": {Provider: "github", URL: "https://github.com",
				Username: "u", Name: "n", Email: "e@e"},
		},
		Sources: map[string]config.Source{},
	}
	a := &App{cfg: cfg, cfgPath: cfgPath, mu: sync.Mutex{}}
	a.SyncAIHarnesses()

	n := 0
	for _, h := range cfg.Global.AIHarnesses {
		if h.Name == "Duplicated" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("duplicate should collapse to 1; got %d (%+v)", n, cfg.Global.AIHarnesses)
	}
	// First occurrence should be kept.
	for _, h := range cfg.Global.AIHarnesses {
		if h.Name == "Duplicated" && h.Command != "/bin/first" {
			t.Errorf("first duplicate should be kept; got command %q", h.Command)
		}
	}
}

// appWithTerminalsAndHarnesses returns an App seeded with a config that has
// the given terminals and harnesses. Used by the Open*InAIHarness tests.
func appWithTerminalsAndHarnesses(t *testing.T, terms []config.TerminalEntry, harnesses []config.AIHarnessEntry) *App {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder:      dir,
			Terminals:   terms,
			AIHarnesses: harnesses,
		},
		Accounts: map[string]config.Account{
			"github-alice": {Provider: "github", URL: "https://github.com",
				Username: "alice", Name: "Alice", Email: "a@e"},
		},
		Sources: map[string]config.Source{},
	}
	return &App{cfg: cfg, cfgPath: filepath.Join(dir, "gitbox.json"), mu: sync.Mutex{}}
}

func TestShellQuotePOSIX(t *testing.T) {
	tests := map[string]string{
		"":                            "''",
		"plain":                       "'plain'",
		"/a/b c":                      "'/a/b c'",
		"has'quote":                   `'has'\''quote'`,
		"multi 'one' 'two'":           `'multi '\''one'\'' '\''two'\'''`,
		`\\backslashes\\`:             `'\\backslashes\\'`,
		`mixed "double" 'single'`:     `'mixed "double" '\''single'\'''`,
	}
	for in, want := range tests {
		if got := shellQuotePOSIX(in); got != want {
			t.Errorf("shellQuotePOSIX(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAppleScriptEscape(t *testing.T) {
	tests := map[string]string{
		"":            "",
		"plain":       "plain",
		`say "hi"`:    `say \"hi\"`,
		`back\slash`:  `back\\slash`,
		`both "\"`:    `both \"\\\"`,
	}
	for in, want := range tests {
		if got := appleScriptEscape(in); got != want {
			t.Errorf("appleScriptEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildMacHarnessShellLine(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		argv    []string
		want    string
	}{
		{
			name: "simple path + single harness arg",
			path: "/Users/me/code/project",
			argv: []string{"claude"},
			want: `cd '/Users/me/code/project' && 'claude'`,
		},
		{
			name: "path with space + multi-arg harness",
			path: "/a b/c",
			argv: []string{"aider", "--model", "sonnet-4.6"},
			want: `cd '/a b/c' && 'aider' '--model' 'sonnet-4.6'`,
		},
		{
			name: "path with single quote",
			path: "/a/it's here",
			argv: []string{"claude"},
			want: `cd '/a/it'\''s here' && 'claude'`,
		},
		{
			name: "empty harness argv still cd's",
			path: "/a",
			argv: nil,
			want: `cd '/a'`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildMacHarnessShellLine(tc.path, tc.argv); got != tc.want {
				t.Errorf("buildMacHarnessShellLine:\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

func TestBuildMacAppleScript(t *testing.T) {
	t.Run("Terminal uses do script", func(t *testing.T) {
		got := buildMacAppleScript("Terminal", "/a", []string{"claude"})
		if !strings.Contains(got, `tell application "Terminal"`) {
			t.Errorf("missing Terminal tell block: %q", got)
		}
		if !strings.Contains(got, `do script "cd '/a' && 'claude'"`) {
			t.Errorf("missing do-script line with expected shell command: %q", got)
		}
	})
	t.Run("iTerm creates a new window and writes text", func(t *testing.T) {
		got := buildMacAppleScript("iTerm", "/a", []string{"claude"})
		if !strings.Contains(got, `tell application "iTerm"`) {
			t.Errorf("missing iTerm tell block: %q", got)
		}
		if !strings.Contains(got, "create window with default profile") {
			t.Errorf("missing new-window directive: %q", got)
		}
		if !strings.Contains(got, `write text "cd '/a' && 'claude'"`) {
			t.Errorf("missing write-text line: %q", got)
		}
	})
	t.Run("unsupported app returns empty", func(t *testing.T) {
		if got := buildMacAppleScript("Warp", "/a", []string{"claude"}); got != "" {
			t.Errorf("expected empty for unsupported app, got %q", got)
		}
	})
	t.Run("escape round-trip for quotes and backslashes", func(t *testing.T) {
		// Path with a double quote and a backslash. POSIX single-quote
		// wrapping handles both at the shell layer (they're literal inside
		// single quotes), AppleScript escaping handles the outer double
		// quotes. We just check nothing unescaped leaks through.
		got := buildMacAppleScript("Terminal", `/a"b\c`, []string{"x"})
		// The shell line was: cd '/a"b\c' && 'x'. After AppleScript
		// escaping the outer double quotes in 'cd' aren't double quotes
		// (POSIX single-quotes are bare "), so only a raw " from the path
		// survives and gets escaped.
		if !strings.Contains(got, `cd '/a\"b\\c' && 'x'`) {
			t.Errorf("escape chain produced unexpected output: %q", got)
		}
	})
}

func TestMacAppleScriptTerminalApp(t *testing.T) {
	if !isDarwin() {
		t.Skip("macOS-only detection")
	}
	tests := []struct {
		name    string
		term    config.TerminalEntry
		wantApp string
		wantOK  bool
	}{
		{
			name:    "Terminal.app",
			term:    config.TerminalEntry{Name: "Terminal", Command: "open", Args: []string{"-a", "Terminal"}},
			wantApp: "Terminal",
			wantOK:  true,
		},
		{
			name:    "iTerm",
			term:    config.TerminalEntry{Name: "iTerm", Command: "open", Args: []string{"-a", "iTerm"}},
			wantApp: "iTerm",
			wantOK:  true,
		},
		{
			name:   "Warp falls through",
			term:   config.TerminalEntry{Name: "Warp", Command: "open", Args: []string{"-a", "Warp"}},
			wantOK: false,
		},
		{
			name:   "non-open command falls through",
			term:   config.TerminalEntry{Name: "Alacritty", Command: "alacritty", Args: []string{"-e", "{command}"}},
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app, ok := macAppleScriptTerminalApp(tc.term)
			if ok != tc.wantOK || app != tc.wantApp {
				t.Errorf("macAppleScriptTerminalApp(%+v) = (%q, %v), want (%q, %v)",
					tc.term, app, ok, tc.wantApp, tc.wantOK)
			}
		})
	}
}

func TestResolveFirstHarnessTerminalAcceptsMacAppleScriptEntry(t *testing.T) {
	if !isDarwin() {
		t.Skip("macOS-only path")
	}
	a := appWithTerminalsAndHarnesses(t,
		[]config.TerminalEntry{{Name: "Terminal", Command: "open", Args: []string{"-a", "Terminal"}}},
		[]config.AIHarnessEntry{{Name: "Claude Code", Command: "claude"}},
	)
	got, err := a.resolveFirstHarnessTerminal()
	if err != nil {
		t.Fatalf("Terminal.app should be accepted as a harness target on mac, got error: %v", err)
	}
	if got.Name != "Terminal" {
		t.Errorf("unexpected terminal returned: %+v", got)
	}
}

func TestResolveFirstHarnessTerminal(t *testing.T) {
	t.Run("no terminals errors", func(t *testing.T) {
		a := appWithTerminalsAndHarnesses(t, nil, nil)
		_, err := a.resolveFirstHarnessTerminal()
		if err == nil || !strings.Contains(err.Error(), "global.terminals is empty") {
			t.Errorf("expected 'global.terminals is empty' error, got %v", err)
		}
	})
	t.Run("terminal without {command} errors with actionable message", func(t *testing.T) {
		a := appWithTerminalsAndHarnesses(t, []config.TerminalEntry{
			{Name: "Git Bash", Command: "git-bash.exe", Args: []string{"--cd={path}"}},
		}, nil)
		_, err := a.resolveFirstHarnessTerminal()
		if err == nil {
			t.Fatal("expected error for terminal without {command}, got nil")
		}
		for _, want := range []string{"Git Bash", "{command}", "global.terminals"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error should mention %q; got %q", want, err.Error())
			}
		}
	})
	t.Run("terminal with {command} returns the entry", func(t *testing.T) {
		term := config.TerminalEntry{
			Name:    "Windows Terminal",
			Command: "wt.exe",
			Args:    []string{"--profile", "PowerShell", "-d", "{path}", "{command}"},
		}
		a := appWithTerminalsAndHarnesses(t, []config.TerminalEntry{term}, nil)
		got, err := a.resolveFirstHarnessTerminal()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != term.Name {
			t.Errorf("got %q, want %q", got.Name, term.Name)
		}
	})
}

func TestOpenInAIHarness_ErrorPaths(t *testing.T) {
	harness := config.AIHarnessEntry{Name: "Claude Code", Command: "claude"}

	t.Run("empty command errors", func(t *testing.T) {
		a := appWithTerminalsAndHarnesses(t,
			[]config.TerminalEntry{{Name: "WT", Command: "wt.exe", Args: []string{"-d", "{path}", "{command}"}}},
			[]config.AIHarnessEntry{harness},
		)
		err := a.OpenInAIHarness("/any", "", nil)
		if err == nil || !strings.Contains(err.Error(), "command is required") {
			t.Errorf("expected 'command is required' error, got %v", err)
		}
	})
	t.Run("no terminal configured errors before exec", func(t *testing.T) {
		a := appWithTerminalsAndHarnesses(t, nil, []config.AIHarnessEntry{harness})
		err := a.OpenInAIHarness("/any", "claude", nil)
		if err == nil || !strings.Contains(err.Error(), "Configure a terminal first") {
			t.Errorf("expected terminal-missing error, got %v", err)
		}
	})
	t.Run("terminal lacks {command} errors before exec", func(t *testing.T) {
		a := appWithTerminalsAndHarnesses(t,
			[]config.TerminalEntry{{Name: "Bare pwsh", Command: "pwsh.exe"}},
			[]config.AIHarnessEntry{harness},
		)
		err := a.OpenInAIHarness("/any", "claude", nil)
		if err == nil || !strings.Contains(err.Error(), "{command}") {
			t.Errorf("expected {command}-missing error, got %v", err)
		}
	})
}

func TestOpenAccountInAIHarness_ErrorPaths(t *testing.T) {
	harness := config.AIHarnessEntry{Name: "Claude Code", Command: "claude"}

	t.Run("empty command errors", func(t *testing.T) {
		a := appWithTerminalsAndHarnesses(t,
			[]config.TerminalEntry{{Name: "WT", Command: "wt.exe", Args: []string{"-d", "{path}", "{command}"}}},
			[]config.AIHarnessEntry{harness},
		)
		err := a.OpenAccountInAIHarness("github-alice", "", nil)
		if err == nil || !strings.Contains(err.Error(), "command is required") {
			t.Errorf("expected 'command is required' error, got %v", err)
		}
	})
	t.Run("unknown account errors with 'not found'", func(t *testing.T) {
		a := appWithTerminalsAndHarnesses(t,
			[]config.TerminalEntry{{Name: "WT", Command: "wt.exe", Args: []string{"-d", "{path}", "{command}"}}},
			[]config.AIHarnessEntry{harness},
		)
		err := a.OpenAccountInAIHarness("nope", "claude", nil)
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' error, got %v", err)
		}
	})
	t.Run("account folder missing errors with 'does not exist'", func(t *testing.T) {
		// App is seeded with account "github-alice" but the folder isn't created.
		a := appWithTerminalsAndHarnesses(t,
			[]config.TerminalEntry{{Name: "WT", Command: "wt.exe", Args: []string{"-d", "{path}", "{command}"}}},
			[]config.AIHarnessEntry{harness},
		)
		err := a.OpenAccountInAIHarness("github-alice", "claude", nil)
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("expected 'does not exist' error, got %v", err)
		}
	})
}
