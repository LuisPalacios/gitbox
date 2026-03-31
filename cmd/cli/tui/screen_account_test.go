package tui

import (
	"strings"
	"testing"
)

func TestAccount_Render(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	// Navigate to account detail for github-alice.
	m = sendMsg(m, switchScreenMsg{screen: screenAccount, accountKey: "github-alice"})
	if m.screen != screenAccount {
		t.Fatalf("expected screenAccount, got %d", m.screen)
	}

	view := m.View()
	// Should show account details.
	for _, want := range []string{"github-alice", "github", "alice"} {
		if !strings.Contains(view, want) {
			t.Errorf("account View missing %q", want)
		}
	}
}

func TestAccount_BackToDashboard(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	// Navigate to account screen.
	m = sendMsg(m, switchScreenMsg{screen: screenAccount, accountKey: "github-alice"})

	// Press Esc → should go back to dashboard.
	m = sendMsg(m, switchScreenMsg{screen: screenDashboard})
	if m.screen != screenDashboard {
		t.Errorf("expected dashboard after back, got %d", m.screen)
	}
}
