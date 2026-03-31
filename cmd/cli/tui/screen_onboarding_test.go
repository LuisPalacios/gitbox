package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestOnboarding_Render(t *testing.T) {
	env := setupTestEnv(t)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m) // no config → onboarding

	if m.screen != screenOnboarding {
		t.Fatalf("expected onboarding screen, got %d", m.screen)
	}

	view := m.View()
	for _, want := range []string{"Welcome to gitbox", "Root folder"} {
		if !strings.Contains(view, want) {
			t.Errorf("onboarding View missing %q", want)
		}
	}
}

func TestOnboarding_SubmitFolder(t *testing.T) {
	env := setupTestEnv(t)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	if m.screen != screenOnboarding {
		t.Fatalf("expected onboarding, got %d", m.screen)
	}

	// The default folder is "~/00.git". Clear it and type our test path.
	// First, select all existing text, then type new value.
	m.onboarding.folderInput.SetValue(env.GitFolder)

	// Press Enter to submit.
	updated, cmd := m.onboarding.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.onboarding = updated

	if m.onboarding.errMsg != "" {
		t.Fatalf("unexpected error: %s", m.onboarding.errMsg)
	}
	if !m.onboarding.done {
		t.Error("expected onboarding.done=true after Enter")
	}

	// The command should produce a switchScreenMsg to dashboard.
	if cmd != nil {
		msg := cmd()
		if sw, ok := msg.(switchScreenMsg); ok {
			if sw.screen != screenDashboard {
				t.Errorf("expected switch to dashboard, got screen %d", sw.screen)
			}
		}
	}

	// External verification: config file should exist with the folder set.
	assertConfigGlobalFolder(t, env.CfgPath, env.GitFolder)
}
