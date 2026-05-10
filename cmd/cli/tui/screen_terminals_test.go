package tui

import (
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/cmd/cli/tui/styles"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/i18n"
	tea "github.com/charmbracelet/bubbletea"
)

// terminalsTestConfig builds a minimal but realistic v2.1 config — two
// detected terminal apps, two shells, a couple of profiles spanning two
// source kinds, plus a pre-existing user profile so delete is testable.
func terminalsTestConfig(t *testing.T) (*config.Config, string) {
	t.Helper()
	env := setupTestEnv(t)
	cfg := newTestConfig(t, env.GitFolder)
	cfg.Global.TerminalApps = []config.TerminalApp{
		{ID: "wt", Name: "Windows Terminal", Command: "C:/Windows/System32/wt.exe",
			ArgsTemplate: []string{"-d", "{path}", "{shell_command}", "{shell_args}"}},
		{ID: "wezterm", Name: "WezTerm", Command: "C:/Program Files/WezTerm/wezterm.exe",
			ArgsTemplate: []string{"start", "--cwd", "{path}", "--", "{shell_command}", "{shell_args}"}},
	}
	cfg.Global.Shells = []config.ShellEntry{
		{ID: "cmd", Name: "Command Prompt", Command: "C:/Windows/System32/cmd.exe"},
		{ID: "pwsh", Name: "PowerShell 7", Command: "C:/Program Files/PowerShell/7/pwsh.exe"},
	}
	cfg.Global.TerminalProfiles = []config.TerminalProfile{
		{ID: "wt+cmd", Name: "Windows Terminal — cmd", TerminalID: "wt", ShellID: "cmd",
			Source: "detected", Default: true},
		{ID: "wt+pwsh", Name: "Windows Terminal — pwsh", TerminalID: "wt", ShellID: "pwsh",
			Source: "detected"},
		{ID: "user-1", Name: "My custom shell", TerminalID: "wezterm", ShellID: "cmd",
			Source: "user"},
	}
	if err := config.Save(cfg, env.CfgPath); err != nil {
		t.Fatalf("save fixture: %v", err)
	}
	return cfg, env.CfgPath
}

func newTestTerminalsScreen(t *testing.T, cfg *config.Config, cfgPath string) terminalsScreen {
	t.Helper()
	return newTerminalsScreen(cfg, cfgPath, styles.NewTheme(true), i18n.New("en"), 100, 30)
}

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestTerminalsScreen_RendersAllSections(t *testing.T) {
	cfg, cfgPath := terminalsTestConfig(t)
	s := newTestTerminalsScreen(t, cfg, cfgPath)

	view := s.View()
	for _, want := range []string{
		"Terminal profiles",
		"Detected terminals",
		"Detected shells",
		"Profiles",
		"Windows Terminal",
		"WezTerm",
		"Command Prompt",
		"PowerShell 7",
		"Windows Terminal — cmd",
		"Windows Terminal — pwsh",
		"My custom shell",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("View missing %q", want)
		}
	}
}

func TestTerminalsScreen_EmptyShowsHelp(t *testing.T) {
	env := setupTestEnv(t)
	cfg := newTestConfig(t, env.GitFolder)
	if err := config.Save(cfg, env.CfgPath); err != nil {
		t.Fatalf("save: %v", err)
	}
	s := newTestTerminalsScreen(t, cfg, env.CfgPath)
	view := s.View()
	if !strings.Contains(view, "No terminal apps detected") {
		t.Errorf("empty view missing apps-help string:\n%s", view)
	}
	if !strings.Contains(view, "No shells detected") {
		t.Errorf("empty view missing shells-help string:\n%s", view)
	}
	if !strings.Contains(view, "No profiles yet") {
		t.Errorf("empty view missing profiles-help string:\n%s", view)
	}
}

func TestTerminalsScreen_TogglePreferred(t *testing.T) {
	cfg, cfgPath := terminalsTestConfig(t)
	s := newTestTerminalsScreen(t, cfg, cfgPath)

	// Cursor starts at index 0 (first profile after sort = wt+cmd which
	// is the default & detected one). Press p to mark it preferred.
	target := s.profiles[s.cursor].ID
	updated, _ := s.Update(keyMsg("p"))
	s = updated

	// Verify in-memory state.
	found := false
	for _, p := range s.cfg.Global.TerminalProfiles {
		if p.ID == target {
			found = true
			if !p.Preferred {
				t.Errorf("profile %s: expected Preferred=true after p, got false", target)
			}
		}
	}
	if !found {
		t.Errorf("profile %s missing from cfg after toggle", target)
	}

	// Verify it persisted to disk.
	loaded := loadConfigFromDisk(t, cfgPath)
	for _, p := range loaded.Global.TerminalProfiles {
		if p.ID == target && !p.Preferred {
			t.Errorf("disk: profile %s Preferred=false after save", target)
		}
	}
}

func TestTerminalsScreen_SetDefault(t *testing.T) {
	cfg, cfgPath := terminalsTestConfig(t)
	s := newTestTerminalsScreen(t, cfg, cfgPath)

	// Move cursor to a non-default profile, press d, and verify exclusivity.
	s.cursor = 1 // wt+pwsh under default sort
	target := s.profiles[s.cursor].ID
	updated, _ := s.Update(keyMsg("d"))
	s = updated

	defaults := 0
	for _, p := range s.cfg.Global.TerminalProfiles {
		if p.Default {
			defaults++
			if p.ID != target {
				t.Errorf("expected only %s to be default, but %s is also default", target, p.ID)
			}
		}
	}
	if defaults != 1 {
		t.Errorf("expected exactly 1 default profile, got %d", defaults)
	}
}

func TestTerminalsScreen_DeleteUserOnly(t *testing.T) {
	cfg, cfgPath := terminalsTestConfig(t)
	s := newTestTerminalsScreen(t, cfg, cfgPath)

	// Find and target the user profile.
	var userIdx int = -1
	for i, p := range s.profiles {
		if p.Source == "user" {
			userIdx = i
			break
		}
	}
	if userIdx < 0 {
		t.Fatal("test fixture missing a user profile")
	}
	s.cursor = userIdx
	updated, _ := s.Update(keyMsg("x"))
	s = updated

	for _, p := range s.cfg.Global.TerminalProfiles {
		if p.Source == "user" {
			t.Errorf("user profile %s should have been deleted", p.ID)
		}
	}

	// Now try to delete a detected profile — should refuse with a status
	// message and leave the row in place.
	for i, p := range s.profiles {
		if p.Source == "detected" {
			s.cursor = i
			break
		}
	}
	originalLen := len(s.cfg.Global.TerminalProfiles)
	updated, _ = s.Update(keyMsg("x"))
	s = updated
	if len(s.cfg.Global.TerminalProfiles) != originalLen {
		t.Errorf("detected profile should not be deletable (count %d → %d)",
			originalLen, len(s.cfg.Global.TerminalProfiles))
	}
	if !strings.Contains(s.errMsg, "Only user-added") && !strings.Contains(s.errMsg, "user-added") {
		t.Errorf("expected error message about user-only delete, got %q", s.errMsg)
	}
}

func TestTerminalsScreen_AddProfileFlow(t *testing.T) {
	cfg, cfgPath := terminalsTestConfig(t)
	s := newTestTerminalsScreen(t, cfg, cfgPath)
	originalCount := len(s.cfg.Global.TerminalProfiles)

	// Open the add form.
	updated, _ := s.Update(keyMsg("a"))
	s = updated
	if s.view != terminalsViewAdd {
		t.Fatalf("expected viewAdd after pressing a, got %v", s.view)
	}

	// Type the name. textinput consumes runes one at a time; send the
	// whole string as a single KeyRunes message which textinput handles.
	updated, _ = s.Update(keyMsg("Test profile"))
	s = updated

	// Submit with Enter.
	updated, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	s = updated

	if s.view != terminalsViewList {
		t.Errorf("expected viewList after submit, got %v", s.view)
	}
	if len(s.cfg.Global.TerminalProfiles) != originalCount+1 {
		t.Errorf("expected %d profiles after add, got %d",
			originalCount+1, len(s.cfg.Global.TerminalProfiles))
	}
	loaded := loadConfigFromDisk(t, cfgPath)
	added := false
	for _, p := range loaded.Global.TerminalProfiles {
		if p.Name == "Test profile" && p.Source == "user" {
			added = true
		}
	}
	if !added {
		t.Errorf("disk: new user profile 'Test profile' not persisted")
	}
}

func TestTerminalsScreen_AddRequiresName(t *testing.T) {
	cfg, cfgPath := terminalsTestConfig(t)
	s := newTestTerminalsScreen(t, cfg, cfgPath)

	updated, _ := s.Update(keyMsg("a"))
	s = updated
	// Submit without typing anything.
	updated, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	s = updated

	if s.view != terminalsViewAdd {
		t.Errorf("empty-name submit should keep us in viewAdd, got %v", s.view)
	}
	if s.errMsg == "" {
		t.Error("expected an error message about required name")
	}
}

func TestTerminalsScreen_BackGoesToSettings(t *testing.T) {
	cfg, cfgPath := terminalsTestConfig(t)
	s := newTestTerminalsScreen(t, cfg, cfgPath)

	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("ESC should produce a switchScreen cmd")
	}
	msg := cmd()
	switchMsg, ok := msg.(switchScreenMsg)
	if !ok {
		t.Fatalf("expected switchScreenMsg, got %T", msg)
	}
	if switchMsg.screen != screenSettings {
		t.Errorf("expected screenSettings, got %v", switchMsg.screen)
	}
}

func TestSettingsTerminalsRowOpensScreen(t *testing.T) {
	env := setupTestEnv(t)
	cfg := newTestConfig(t, env.GitFolder)
	if err := config.Save(cfg, env.CfgPath); err != nil {
		t.Fatalf("save: %v", err)
	}
	m := newSettingsModel(cfg, env.CfgPath, styles.NewTheme(true), i18n.New("en"), 100, 30)
	m.active = settingsTerminals

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on Terminals row should produce a switchScreen cmd")
	}
	msg := cmd()
	switchMsg, ok := msg.(switchScreenMsg)
	if !ok {
		t.Fatalf("expected switchScreenMsg, got %T", msg)
	}
	if switchMsg.screen != screenTerminals {
		t.Errorf("expected screenTerminals, got %v", switchMsg.screen)
	}
}
