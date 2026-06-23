package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/status"
	tea "github.com/charmbracelet/bubbletea"
)

// newWorkspaceConfig extends newDummyConfig with one discovered workspace so
// tests can cover the read-only list / open paths.
func newWorkspaceConfig(t *testing.T, gitFolder string) *config.Config {
	t.Helper()
	cfg := newDummyConfig(t, gitFolder)
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {
			Name:       "Feature X",
			Discovered: true,
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
	if !strings.Contains(view, "1 member") {
		t.Errorf("Workspaces view missing member count:\n%s", view)
	}
}

func TestDashboard_MultiSelectStillWorks(t *testing.T) {
	cfg := newDummyConfig(t, "/tmp/test-git")
	env := setupTestEnvWithConfig(t, cfg)
	m := newTestModel(t, env.CfgPath)
	m = initModel(t, m)

	m.dashboard.statuses = []status.RepoStatus{
		{Source: "alice-repos", Repo: "alice/hello-world", State: status.Clean},
		{Source: "bob-repos", Repo: "bob/my-project", State: status.Clean},
	}
	m.dashboard.activeTab = tabAccounts
	m.dashboard.focus = focusList
	m.dashboard.listCursor = 0

	m = sendKey(m, " ")
	if len(m.dashboard.selectedClones) != 1 {
		t.Fatalf("after space, selected = %d, want 1", len(m.dashboard.selectedClones))
	}
	m = sendKey(m, "A")
	if len(m.dashboard.selectedClones) != 2 {
		t.Errorf("after A, selected = %d, want 2", len(m.dashboard.selectedClones))
	}
}

func TestDiscoverWorkspacesCmd_RefreshesCacheAndPersists(t *testing.T) {
	env := setupTestEnv(t)
	cfg := newDummyConfig(t, env.GitFolder)
	if err := config.Save(cfg, env.CfgPath); err != nil {
		t.Fatalf("save cfg: %v", err)
	}

	// Stage a clone path that resolves to (alice-repos, alice/hello-world)
	// under the git folder, then drop a *.code-workspace next to it.
	cloneDir := filepath.Join(env.GitFolder, "alice-repos", "alice", "hello-world")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	wsFile := filepath.Join(env.GitFolder, "disc.code-workspace")
	if err := os.WriteFile(wsFile, []byte(`{"folders":[{"path":"`+filepath.ToSlash(cloneDir)+`"}]}`), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	cmd := discoverWorkspacesCmd(cfg, env.CfgPath)
	msg := cmd()
	done, ok := msg.(workspaceDiscoverDoneMsg)
	if !ok {
		t.Fatalf("got %T, want workspaceDiscoverDoneMsg", msg)
	}
	if !done.changed || done.err != nil {
		t.Fatalf("changed=%v err=%v, want changed/no-err", done.changed, done.err)
	}
	reloaded, err := config.Load(env.CfgPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.Workspaces["disc"]; !ok {
		t.Error("workspace 'disc' not persisted")
	}
}
