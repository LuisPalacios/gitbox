package main

import (
	"path/filepath"
	"reflect"
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
			name: "empty args uses path as sole arg",
			args: nil,
			path: "/tmp/x",
			want: []string{"/tmp/x"},
		},
		{
			name: "multiple placeholders all substituted",
			args: []string{"{path}", "--log", "{path}.log"},
			path: "/a",
			want: []string{"/a", "--log", "/a.log"},
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

func TestSyncTerminalsPreservesUserEdits(t *testing.T) {
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
