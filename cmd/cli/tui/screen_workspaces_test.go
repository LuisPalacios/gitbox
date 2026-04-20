package tui

import (
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/status"
	tea "github.com/charmbracelet/bubbletea"
)

// newWorkspaceConfig extends newDummyConfig with one pre-configured
// workspace so tests can cover list / delete / regenerate paths without
// boilerplate.
func newWorkspaceConfig(t *testing.T, gitFolder string) *config.Config {
	t.Helper()
	cfg := newDummyConfig(t, gitFolder)
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {
			Type: config.WorkspaceTypeCode,
			Name: "Feature X",
			Members: []config.WorkspaceMember{
				{Source: "alice-repos", Repo: "alice/hello-world"},
			},
		},
	}
	cfg.WorkspaceOrder = []string{"feat-x"}
	return cfg
}

func TestDashboard_ThirdTabIsWorkspaces(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	// Rotate: Accounts → Mirrors → Workspaces.
	m = sendSpecialKey(m, tea.KeyTab)
	if m.dashboard.activeTab != tabMirrors {
		t.Fatalf("after 1 Tab, activeTab = %d, want tabMirrors", m.dashboard.activeTab)
	}
	m = sendSpecialKey(m, tea.KeyTab)
	if m.dashboard.activeTab != tabWorkspaces {
		t.Fatalf("after 2 Tabs, activeTab = %d, want tabWorkspaces", m.dashboard.activeTab)
	}
	m = sendSpecialKey(m, tea.KeyTab)
	if m.dashboard.activeTab != tabAccounts {
		t.Fatalf("after 3 Tabs, activeTab = %d, want tabAccounts (rotation)", m.dashboard.activeTab)
	}
}

func TestDashboard_GlobalWKeyJumpsToWorkspaces(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	if m.dashboard.activeTab != tabAccounts {
		t.Fatalf("pre: expected Accounts tab, got %d", m.dashboard.activeTab)
	}
	m = sendKey(m, "w")
	if m.dashboard.activeTab != tabWorkspaces {
		t.Errorf("after 'w', activeTab = %d, want tabWorkspaces", m.dashboard.activeTab)
	}
}

func TestDashboard_WorkspacesTab_EmptyStateMessage(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git") // no workspaces
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m.dashboard.activeTab = tabWorkspaces
	view := m.View()
	if !strings.Contains(view, "No workspaces configured.") {
		t.Errorf("empty Workspaces tab missing empty-state hint:\n%s", view)
	}
}

func TestDashboard_WorkspacesTab_RendersList(t *testing.T) {
	cfg := newWorkspaceConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m.dashboard.activeTab = tabWorkspaces
	view := m.View()
	if !strings.Contains(view, "Feature X") {
		t.Errorf("Workspaces view missing workspace name:\n%s", view)
	}
	if !strings.Contains(view, "[code]") {
		t.Errorf("Workspaces view missing type tag:\n%s", view)
	}
	if !strings.Contains(view, "1 member") {
		t.Errorf("Workspaces view missing member count:\n%s", view)
	}
}

func TestDashboard_WorkspaceDeletePromptsAndCancels(t *testing.T) {
	cfg := newWorkspaceConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m.dashboard.activeTab = tabWorkspaces
	m.dashboard.focus = focusList
	m.dashboard.listCursor = 0

	// 'd' asks to confirm; config must still contain the workspace.
	m = sendKey(m, "d")
	if m.dashboard.workspaceDeleteConfirm != "feat-x" {
		t.Fatalf("expected delete confirm pending on feat-x, got %q", m.dashboard.workspaceDeleteConfirm)
	}
	// 'n' cancels.
	m = sendKey(m, "n")
	if m.dashboard.workspaceDeleteConfirm != "" {
		t.Errorf("expected cancelled confirm, still pending on %q", m.dashboard.workspaceDeleteConfirm)
	}
	if _, ok := m.dashboard.cfg.Workspaces["feat-x"]; !ok {
		t.Error("workspace removed by n cancel — should still be present")
	}
}

func TestDashboard_MultiSelectAndWorkspaceShortcut(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	// Seed a small status list aligned with the dummy sources so the
	// list cursor maps deterministically onto a repo key.
	m.dashboard.statuses = []status.RepoStatus{
		{Source: "alice-repos", Repo: "alice/hello-world", State: status.Clean},
		{Source: "bob-repos", Repo: "bob/my-project", State: status.Clean},
	}
	m.dashboard.activeTab = tabAccounts
	m.dashboard.focus = focusList
	m.dashboard.listCursor = 0

	// Space toggles selection.
	m = sendKey(m, " ")
	if len(m.dashboard.selectedClones) != 1 {
		t.Fatalf("after space, selected = %d, want 1", len(m.dashboard.selectedClones))
	}
	m = sendKey(m, " ")
	if len(m.dashboard.selectedClones) != 0 {
		t.Errorf("after second space, selected = %d, want 0", len(m.dashboard.selectedClones))
	}

	// 'A' selects all visible.
	m = sendKey(m, "A")
	if len(m.dashboard.selectedClones) != 2 {
		t.Errorf("after A, selected = %d, want 2", len(m.dashboard.selectedClones))
	}

	// 'w' with a selection returns a switchScreenMsg command; pump it
	// through Update by running the returned cmd and dispatching its
	// message back.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	m = updated.(model)
	if cmd == nil {
		t.Fatal("expected command returned by w with selection, got nil")
	}
	msg := cmd()
	m = sendMsg(m, msg)
	if m.screen != screenWorkspaceAdd {
		t.Errorf("after w with selection, screen = %d, want screenWorkspaceAdd", m.screen)
	}
	if len(m.dashboard.selectedClones) != 0 {
		t.Errorf("selection should be cleared after w, got %d entries", len(m.dashboard.selectedClones))
	}
}
