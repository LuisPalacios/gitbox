package tui

import (
	"os"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	tea "github.com/charmbracelet/bubbletea"
)

func TestInit_FirstRun(t *testing.T) {
	env := setupTestEnv(t)
	// Don't create a config file — simulates first run.
	m := newTestModel(t, env.CfgPath)

	// Simulate Init: run the command and dispatch the result.
	m = initModel(t, m)

	if m.screen != screenOnboarding {
		t.Errorf("expected screenOnboarding, got %d", m.screen)
	}
	if !m.firstRun {
		t.Error("expected firstRun=true")
	}
}

func TestInit_ExistingConfig(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)

	m = initModel(t, m)

	if m.screen != screenDashboard {
		t.Errorf("expected screenDashboard, got %d", m.screen)
	}
	if m.firstRun {
		t.Error("expected firstRun=false")
	}
	if m.cfg == nil {
		t.Fatal("expected cfg to be loaded")
	}
	if len(m.cfg.Accounts) != 2 {
		t.Errorf("expected 2 accounts, got %d", len(m.cfg.Accounts))
	}
}

func TestInit_ExistingConfig_NoFolder(t *testing.T) {
	// Config exists but global.folder is empty → treated as first run.
	cfg := &config.Config{
		Version:  2,
		Global:   config.GlobalConfig{}, // no folder set
		Accounts: map[string]config.Account{},
		Sources:  map[string]config.Source{},
	}
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)

	m = initModel(t, m)

	if m.screen != screenOnboarding {
		t.Errorf("expected screenOnboarding (no folder set), got %d", m.screen)
	}
	if !m.firstRun {
		t.Error("expected firstRun=true when folder is empty")
	}
}

func TestInit_InvalidConfig(t *testing.T) {
	env := setupTestEnv(t)
	// Write invalid JSON to the config path.
	if err := os.WriteFile(env.CfgPath, []byte("{invalid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t, env.CfgPath)

	m = initModel(t, m)

	if m.cfgErr == "" {
		t.Error("expected cfgErr to be set for invalid JSON")
	}
	if m.screen != screenOnboarding {
		t.Errorf("expected screenOnboarding on error, got %d", m.screen)
	}
}

func TestQuit_CtrlC(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m = sendMsg(m, tea.KeyMsg{Type: tea.KeyCtrlC})

	if !m.quitting {
		t.Error("expected quitting=true after ctrl+c")
	}
}

func TestQuit_Esc(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	// On the dashboard, Esc should quit (via Keys.Back binding).
	if m.screen != screenDashboard {
		t.Fatalf("expected dashboard, got %d", m.screen)
	}

	// Dashboard handles Keys.Back (esc) → tea.Quit.
	// Dispatch through the dashboard's Update since it handles the key.
	updated, cmd := m.dashboard.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m.dashboard = updated

	// The command should be tea.Quit (returns a quit msg).
	if cmd == nil {
		t.Error("expected quit command from Esc on dashboard")
	}
}

func TestWindowResize(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)

	m = sendWindowSize(m, 120, 40)

	if m.width != 120 || m.height != 40 {
		t.Errorf("expected 120x40, got %dx%d", m.width, m.height)
	}
}

func TestNavigation_SwitchScreens(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	tests := []struct {
		name   string
		msg    switchScreenMsg
		expect screenID
	}{
		{"account", switchScreenMsg{screen: screenAccount, accountKey: "github-alice"}, screenAccount},
		{"accountAdd", switchScreenMsg{screen: screenAccountAdd}, screenAccountAdd},
		{"settings", switchScreenMsg{screen: screenSettings}, screenSettings},
		{"identity", switchScreenMsg{screen: screenIdentity}, screenIdentity},
		{"dashboard", switchScreenMsg{screen: screenDashboard}, screenDashboard},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updated := sendMsg(m, tt.msg)
			if updated.screen != tt.expect {
				t.Errorf("expected screen %d, got %d", tt.expect, updated.screen)
			}
		})
	}
}

func TestView_NotEmpty(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	view := m.View()
	if strings.TrimSpace(view) == "" {
		t.Error("expected non-empty View output")
	}
}
