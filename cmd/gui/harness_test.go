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

func TestSyncAIHarnessesReordersToMatchKnownList(t *testing.T) {
	// knownAIHarnesses is derived from pkg/harness/tools-directory.md. When
	// the user reorders that markdown table, the next SyncAIHarnesses pass
	// should reorder their persisted global.ai_harnesses to match — carrying
	// their Command/Args customizations along, and leaving any user-added
	// custom entries (not in the known list) at the end.
	if len(knownAIHarnesses) < 2 {
		t.Skip("need at least 2 known harnesses for order test")
	}
	first := knownAIHarnesses[0]
	second := knownAIHarnesses[1]

	dir := t.TempDir()
	cfg := &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder: dir,
			// User has the two harnesses in the REVERSE of the markdown order,
			// plus a custom entry not in knownAIHarnesses.
			AIHarnesses: []config.AIHarnessEntry{
				{Name: second.Name, Command: "/custom/" + second.Command, Args: []string{"--user-flag"}},
				{Name: first.Name, Command: "/custom/" + first.Command},
				{Name: "My Private Bot", Command: "/opt/mybot"},
			},
		},
		Accounts: map[string]config.Account{
			"A": {Provider: "github", URL: "https://github.com",
				Username: "u", Name: "n", Email: "e@e"},
		},
		Sources: map[string]config.Source{},
	}
	a := &App{cfg: cfg, cfgPath: filepath.Join(dir, "gitbox.json"), mu: sync.Mutex{}}
	a.SyncAIHarnesses()

	// Known entries must now be in markdown order, customizations preserved,
	// custom entries last.
	if len(cfg.Global.AIHarnesses) < 3 {
		t.Fatalf("expected at least 3 entries, got %d: %+v", len(cfg.Global.AIHarnesses), cfg.Global.AIHarnesses)
	}
	if cfg.Global.AIHarnesses[0].Name != first.Name {
		t.Errorf("entry[0] should be %q after reorder, got %q", first.Name, cfg.Global.AIHarnesses[0].Name)
	}
	if cfg.Global.AIHarnesses[1].Name != second.Name {
		t.Errorf("entry[1] should be %q after reorder, got %q", second.Name, cfg.Global.AIHarnesses[1].Name)
	}
	// Customizations preserved on the reordered entries.
	if cfg.Global.AIHarnesses[0].Command != "/custom/"+first.Command {
		t.Errorf("%s command customization lost: %+v", first.Name, cfg.Global.AIHarnesses[0])
	}
	if cfg.Global.AIHarnesses[1].Command != "/custom/"+second.Command {
		t.Errorf("%s command customization lost: %+v", second.Name, cfg.Global.AIHarnesses[1])
	}
	if !reflect.DeepEqual(cfg.Global.AIHarnesses[1].Args, []string{"--user-flag"}) {
		t.Errorf("%s args customization lost: %+v", second.Name, cfg.Global.AIHarnesses[1])
	}
	// Custom entry still present at the tail.
	last := cfg.Global.AIHarnesses[len(cfg.Global.AIHarnesses)-1]
	if last.Name != "My Private Bot" {
		t.Errorf("user-added custom entry lost or misplaced: last = %+v", last)
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

// appWithProfilesAndHarnesses seeds an App with v2.1 Terminal Profiles (the
// model AI harness launches resolve against since #80) plus harness entries.
// Account "github-alice" exists in config but its folder is NOT created.
func appWithProfilesAndHarnesses(t *testing.T, apps []config.TerminalApp, shells []config.ShellEntry, profiles []config.TerminalProfile, harnesses []config.AIHarnessEntry) *App {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder:           dir,
			TerminalApps:     apps,
			Shells:           shells,
			TerminalProfiles: profiles,
			AIHarnesses:      harnesses,
		},
		Accounts: map[string]config.Account{
			"github-alice": {Provider: "github", URL: "https://github.com",
				Username: "alice", Name: "Alice", Email: "a@e"},
		},
		Sources: map[string]config.Source{},
	}
	return &App{cfg: cfg, cfgPath: filepath.Join(dir, "gitbox.json"), mu: sync.Mutex{}}
}

// warpOnlyProfiles is a profile set whose only (default) terminal is a macOS
// `open -a Warp` app: launchable as a plain terminal, but unable to host a
// command. Used to prove the harness path fails before exec with the
// resolver's error instead of silently opening a bare terminal.
func warpOnlyProfiles() ([]config.TerminalApp, []config.ShellEntry, []config.TerminalProfile) {
	return []config.TerminalApp{{ID: "warp", Name: "Warp", Command: "open", ArgsTemplate: []string{"-a", "Warp"}}},
		nil,
		[]config.TerminalProfile{{ID: "warp", Name: "Warp", TerminalID: "warp", Default: true}}
}

func TestHarnessProfileID_PicksLauncherDefault(t *testing.T) {
	apps := []config.TerminalApp{{ID: "wt", Name: "Windows Terminal", Command: "wt.exe"}}
	profiles := []config.TerminalProfile{
		{ID: "hidden-default", TerminalID: "wt", Hidden: true, Default: true},
		{ID: "first-visible", TerminalID: "wt"},
		{ID: "the-default", TerminalID: "wt", Default: true},
	}
	a := appWithProfilesAndHarnesses(t, apps, nil, profiles, nil)
	id, err := a.harnessProfileID()
	if err != nil || id != "the-default" {
		t.Errorf("want visible Default profile, got %q err=%v", id, err)
	}
	a = appWithProfilesAndHarnesses(t, apps, nil, nil, nil)
	if _, err := a.harnessProfileID(); err == nil || !strings.Contains(err.Error(), "Configure a terminal profile") {
		t.Errorf("no profiles must error with an actionable message, got %v", err)
	}
}

func TestOpenInAIHarness_ErrorPaths(t *testing.T) {
	harness := config.AIHarnessEntry{Name: "Claude Code", Command: "claude"}
	apps, shells, profiles := warpOnlyProfiles()

	t.Run("empty command errors", func(t *testing.T) {
		a := appWithProfilesAndHarnesses(t, apps, shells, profiles, []config.AIHarnessEntry{harness})
		err := a.OpenInAIHarness("/any", "", nil)
		if err == nil || !strings.Contains(err.Error(), "command is required") {
			t.Errorf("expected 'command is required' error, got %v", err)
		}
	})
	t.Run("no profile configured errors before exec", func(t *testing.T) {
		a := appWithProfilesAndHarnesses(t, nil, nil, nil, []config.AIHarnessEntry{harness})
		err := a.OpenInAIHarness("/any", "claude", nil)
		if err == nil || !strings.Contains(err.Error(), "Configure a terminal profile") {
			t.Errorf("expected profile-missing error, got %v", err)
		}
	})
	t.Run("default profile that cannot host a command errors before exec", func(t *testing.T) {
		if !isDarwin() {
			t.Skip("open -a is the macOS launcher shape; the resolver only routes it on darwin")
		}
		a := appWithProfilesAndHarnesses(t, apps, shells, profiles, []config.AIHarnessEntry{harness})
		err := a.OpenInAIHarness("/any", "claude", nil)
		if err == nil || !strings.Contains(err.Error(), "cannot run a command") {
			t.Errorf("expected 'cannot run a command' error, got %v", err)
		}
	})
}

func TestOpenAccountInAIHarness_ErrorPaths(t *testing.T) {
	harness := config.AIHarnessEntry{Name: "Claude Code", Command: "claude"}
	apps, shells, profiles := warpOnlyProfiles()

	t.Run("empty command errors", func(t *testing.T) {
		a := appWithProfilesAndHarnesses(t, apps, shells, profiles, []config.AIHarnessEntry{harness})
		err := a.OpenAccountInAIHarness("github-alice", "", nil)
		if err == nil || !strings.Contains(err.Error(), "command is required") {
			t.Errorf("expected 'command is required' error, got %v", err)
		}
	})
	t.Run("unknown account errors with 'not found'", func(t *testing.T) {
		a := appWithProfilesAndHarnesses(t, apps, shells, profiles, []config.AIHarnessEntry{harness})
		err := a.OpenAccountInAIHarness("nope", "claude", nil)
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' error, got %v", err)
		}
	})
	t.Run("account folder missing errors with 'does not exist'", func(t *testing.T) {
		// App is seeded with account "github-alice" but the folder isn't created.
		a := appWithProfilesAndHarnesses(t, apps, shells, profiles, []config.AIHarnessEntry{harness})
		err := a.OpenAccountInAIHarness("github-alice", "claude", nil)
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("expected 'does not exist' error, got %v", err)
		}
	})
}
