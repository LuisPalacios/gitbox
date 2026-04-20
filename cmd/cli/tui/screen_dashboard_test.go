package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDashboard_Empty(t *testing.T) {
	cfg := newTestConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	if m.screen != screenDashboard {
		t.Fatalf("expected dashboard, got %d", m.screen)
	}

	view := m.View()
	// With no accounts, the dashboard should still render without panicking.
	if strings.TrimSpace(view) == "" {
		t.Error("expected non-empty View for empty dashboard")
	}
}

func TestDashboard_WithAccounts(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	if m.screen != screenDashboard {
		t.Fatalf("expected dashboard, got %d", m.screen)
	}

	view := m.View()
	// Both account keys should appear somewhere in the rendered view.
	for _, key := range []string{"github-alice", "forgejo-bob"} {
		if !strings.Contains(view, key) {
			t.Errorf("dashboard View missing account %q", key)
		}
	}
}

func TestDashboard_TabSwitch(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	if m.dashboard.activeTab != tabAccounts {
		t.Fatalf("expected initial tab=Accounts, got %d", m.dashboard.activeTab)
	}

	// Press Tab → should switch to Mirrors tab.
	m.dashboard, _ = m.dashboard.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.dashboard.activeTab != tabMirrors {
		t.Errorf("expected Mirrors tab after Tab, got %d", m.dashboard.activeTab)
	}

	// Press Tab again → Workspaces tab (third tab added in PR #52).
	m.dashboard, _ = m.dashboard.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.dashboard.activeTab != tabWorkspaces {
		t.Errorf("expected Workspaces tab after second Tab, got %d", m.dashboard.activeTab)
	}

	// Press Tab again → rotates back to Accounts.
	m.dashboard, _ = m.dashboard.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.dashboard.activeTab != tabAccounts {
		t.Errorf("expected Accounts tab after third Tab, got %d", m.dashboard.activeTab)
	}
}

func TestDashboard_CardNavigation(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	if m.dashboard.cardCursor != 0 {
		t.Fatalf("expected initial cardCursor=0, got %d", m.dashboard.cardCursor)
	}

	// Press right → should move to next card (we have 2 accounts).
	m.dashboard, _ = m.dashboard.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("right")})
	// The key is "right" string, not runes. Use the arrow key type.
	m.dashboard, _ = m.dashboard.Update(tea.KeyMsg{Type: tea.KeyRight})
	// Reset and try properly.
	m.dashboard.cardCursor = 0
	m.dashboard, _ = m.dashboard.Update(tea.KeyMsg{Type: tea.KeyRight})
	if m.dashboard.cardCursor != 1 {
		t.Errorf("expected cardCursor=1 after Right, got %d", m.dashboard.cardCursor)
	}
}

func TestDashboard_AddAccountShortcut(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	// Press 'a' on accounts tab → should produce switchScreenMsg to accountAdd.
	_, cmd := m.dashboard.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if cmd == nil {
		t.Fatal("expected command from 'a' key")
	}
	msg := cmd()
	sw, ok := msg.(switchScreenMsg)
	if !ok {
		t.Fatalf("expected switchScreenMsg, got %T", msg)
	}
	if sw.screen != screenAccountAdd {
		t.Errorf("expected screenAccountAdd, got %d", sw.screen)
	}
}

func TestDashboard_SettingsShortcut(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	_, cmd := m.dashboard.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if cmd == nil {
		t.Fatal("expected command from 's' key")
	}
	msg := cmd()
	sw, ok := msg.(switchScreenMsg)
	if !ok {
		t.Fatalf("expected switchScreenMsg, got %T", msg)
	}
	if sw.screen != screenSettings {
		t.Errorf("expected screenSettings, got %d", sw.screen)
	}
}
