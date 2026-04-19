package tui

import (
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	tea "github.com/charmbracelet/bubbletea"
)

// fixtureConfigWithLaunchers returns a minimal config carrying two entries in
// each launcher category so tests can exercise both "default" and "submenu"
// paths without reaching for a fixture file.
func fixtureConfigWithLaunchers() *config.Config {
	return &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder: "/tmp/test-git",
			Terminals: []config.TerminalEntry{
				{Name: "Git Bash", Command: "/bin/wt", Args: []string{"--profile", "Git Bash", "-d", "{path}", "{command}"}},
				{Name: "PowerShell", Command: "/bin/wt", Args: []string{"--profile", "PowerShell", "-d", "{path}", "{command}"}},
			},
			Editors: []config.EditorEntry{
				{Name: "VS Code", Command: "/bin/code"},
				{Name: "Cursor", Command: "/bin/cursor"},
			},
			AIHarnesses: []config.AIHarnessEntry{
				{Name: "Claude Code", Command: "/bin/claude"},
				{Name: "Codex", Command: "/bin/codex"},
			},
		},
		Accounts: map[string]config.Account{},
		Sources:  map[string]config.Source{},
	}
}

func TestLauncher_BuildItemsFromConfig(t *testing.T) {
	cfg := fixtureConfigWithLaunchers()
	lo := newLauncherOverlay(cfg)

	// Expected layout: header+2 (terminals), header+2 (editors), header+2 (harnesses) = 9 rows.
	if got, want := len(lo.items), 9; got != want {
		t.Fatalf("items: got %d, want %d", got, want)
	}

	// Verify each group has exactly one header followed by its entries in the
	// order defined in config (first-entry is the convention for defaults).
	expected := []struct {
		kind  launcherItemKind
		label string
	}{
		{launcherRowHeader, "Terminals"},
		{launcherRowTerminal, "Git Bash"},
		{launcherRowTerminal, "PowerShell"},
		{launcherRowHeader, "Editors"},
		{launcherRowEditor, "VS Code"},
		{launcherRowEditor, "Cursor"},
		{launcherRowHeader, "AI Harnesses"},
		{launcherRowHarness, "Claude Code"},
		{launcherRowHarness, "Codex"},
	}
	for i, want := range expected {
		got := lo.items[i]
		if got.kind != want.kind || got.label != want.label {
			t.Errorf("items[%d] = {%d, %q}, want {%d, %q}", i, got.kind, got.label, want.kind, want.label)
		}
	}

	// Cursor lands on the first selectable row (Git Bash at index 1).
	if lo.cursor != 1 {
		t.Errorf("initial cursor = %d, want 1 (first selectable)", lo.cursor)
	}
}

func TestLauncher_OmitsEmptyGroups(t *testing.T) {
	cfg := fixtureConfigWithLaunchers()
	cfg.Global.Editors = nil // remove just the editor group

	lo := newLauncherOverlay(cfg)

	for _, it := range lo.items {
		if it.kind == launcherRowHeader && it.label == "Editors" {
			t.Error("empty editors group should not produce a header row")
		}
		if it.kind == launcherRowEditor {
			t.Error("editor row in items despite empty config.Global.Editors")
		}
	}
}

func TestLauncher_NoLaunchers_NoSelectable(t *testing.T) {
	cfg := &config.Config{Version: 2, Global: config.GlobalConfig{Folder: "/tmp"}}
	lo := newLauncherOverlay(cfg)
	if lo.hasAny() {
		t.Error("hasAny() = true on empty config, want false")
	}
}

func TestLauncher_NavigationSkipsHeaders(t *testing.T) {
	cfg := fixtureConfigWithLaunchers()
	lo := newLauncherOverlay(cfg).activate("/tmp/test-git")

	// Starting at index 1 (Git Bash). Press down → PowerShell (index 2).
	// Press down again → should skip "Editors" header and land on VS Code (index 4).
	lo2, _, handled := lo.update(tea.KeyMsg{Type: tea.KeyDown}, cfg.Global.Terminals)
	if !handled {
		t.Fatal("KeyDown not handled")
	}
	if lo2.cursor != 2 {
		t.Errorf("after 1st down, cursor = %d, want 2", lo2.cursor)
	}

	lo3, _, _ := lo2.update(tea.KeyMsg{Type: tea.KeyDown}, cfg.Global.Terminals)
	if lo3.cursor != 4 {
		t.Errorf("after 2nd down (across header), cursor = %d, want 4", lo3.cursor)
	}

	// Up from VS Code: skip "Editors" header back to PowerShell (index 2).
	lo4, _, _ := lo3.update(tea.KeyMsg{Type: tea.KeyUp}, cfg.Global.Terminals)
	if lo4.cursor != 2 {
		t.Errorf("after up (across header), cursor = %d, want 2", lo4.cursor)
	}
}

func TestLauncher_EnterDispatchesLaunchCmd(t *testing.T) {
	cfg := fixtureConfigWithLaunchers()
	lo := newLauncherOverlay(cfg).activate("/tmp/test-git")

	// Cursor starts at Git Bash. Enter should return a non-nil tea.Cmd and
	// close the overlay. We don't execute the cmd — that would fork a
	// process — just verify the wiring.
	lo2, cmd, handled := lo.update(tea.KeyMsg{Type: tea.KeyEnter}, cfg.Global.Terminals)
	if !handled {
		t.Fatal("Enter not handled")
	}
	if cmd == nil {
		t.Fatal("Enter dispatched nil tea.Cmd (expected launch command)")
	}
	if lo2.active {
		t.Error("overlay still active after Enter; should close")
	}
}

func TestLauncher_EscClosesWithoutLaunch(t *testing.T) {
	cfg := fixtureConfigWithLaunchers()
	lo := newLauncherOverlay(cfg).activate("/tmp/test-git")

	lo2, cmd, handled := lo.update(tea.KeyMsg{Type: tea.KeyEsc}, cfg.Global.Terminals)
	if !handled {
		t.Fatal("Esc not handled")
	}
	if cmd != nil {
		t.Error("Esc produced a tea.Cmd; expected nil (no launch)")
	}
	if lo2.active {
		t.Error("overlay still active after Esc")
	}
}

func TestLauncher_InactiveOverlayIgnoresKeys(t *testing.T) {
	cfg := fixtureConfigWithLaunchers()
	lo := newLauncherOverlay(cfg) // not activated

	_, _, handled := lo.update(tea.KeyMsg{Type: tea.KeyEnter}, cfg.Global.Terminals)
	if handled {
		t.Error("inactive overlay consumed Enter; it should delegate to origin screen")
	}
}

func TestLauncher_ViewRendersGroupsAndHint(t *testing.T) {
	cfg := fixtureConfigWithLaunchers()
	lo := newLauncherOverlay(cfg).activate("/tmp/test-git")
	theme := styles.NewTheme(true)
	out := lo.view(theme, 80, 24)

	for _, want := range []string{"Open in…", "Terminals", "Editors", "AI Harnesses", "Git Bash", "VS Code", "Claude Code", "esc cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestLauncher_LaunchCurrentByKind(t *testing.T) {
	cfg := fixtureConfigWithLaunchers()
	lo := newLauncherOverlay(cfg).activate("/tmp")

	// Cursor on Git Bash (terminal). launchCurrent should return a non-nil cmd.
	if lo.launchCurrent(cfg.Global.Terminals) == nil {
		t.Error("launchCurrent returned nil for a terminal row")
	}

	// Move to VS Code (editor at index 4).
	lo.cursor = 4
	if lo.launchCurrent(cfg.Global.Terminals) == nil {
		t.Error("launchCurrent returned nil for an editor row")
	}

	// Move to Claude Code (harness at index 7).
	lo.cursor = 7
	if lo.launchCurrent(cfg.Global.Terminals) == nil {
		t.Error("launchCurrent returned nil for a harness row")
	}

	// Harness without any configured terminal should still return a cmd,
	// but running it would produce an actionable error. The cmd factory is
	// responsible for the error, not launchCurrent; verify cmd != nil.
	lo.cursor = 7
	if lo.launchCurrent(nil) == nil {
		t.Error("launchCurrent returned nil for a harness row with no terminals; expected cmd that surfaces the error")
	}
}

func TestResolveTerminalArgs_PathSubstitution(t *testing.T) {
	args := []string{"--profile", "Git Bash", "-d", "{path}"}
	got := resolveTerminalArgs(args, "/repo", nil)
	want := []string{"--profile", "Git Bash", "-d", "/repo"}
	if !slicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveTerminalArgs_CommandSplice(t *testing.T) {
	args := []string{"--profile", "X", "-d", "{path}", "{command}"}
	harness := []string{"/bin/claude", "--flag"}
	got := resolveTerminalArgs(args, "/repo", harness)
	want := []string{"--profile", "X", "-d", "/repo", "/bin/claude", "--flag"}
	if !slicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveTerminalArgs_AppendPathWhenNoToken(t *testing.T) {
	// Legacy launcher patterns like `open -a Terminal <path>` have no {path}
	// token; the resolver appends path as the last argv.
	args := []string{"-a", "Terminal"}
	got := resolveTerminalArgs(args, "/repo", nil)
	want := []string{"-a", "Terminal", "/repo"}
	if !slicesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveTerminalArgs_EmptyArgsReturnsNil(t *testing.T) {
	if resolveTerminalArgs(nil, "/repo", nil) != nil {
		t.Error("empty args should return nil (terminal cmd.Dir covers it)")
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
