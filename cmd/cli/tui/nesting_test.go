package tui

import (
	"path/filepath"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// TestAccountRepoRows_NestsChildrenUnderContainer verifies a container parent is
// immediately followed by its nested children (indent 1), matching the GUI.
func TestAccountRepoRows_NestsChildrenUnderContainer(t *testing.T) {
	root := t.TempDir()
	parentPath := filepath.Join(root, "acct", "Org", "project")

	cfg := &config.Config{
		Version: config.CurrentVersion,
		Global:  config.GlobalConfig{Folder: root},
		Sources: map[string]config.Source{
			"acct": {
				Account: "acct",
				Repos: map[string]config.Repo{
					"Org/project":  {Container: true},
					"Org/browser":  {CloneFolder: filepath.Join(parentPath, "browser")},
					"Org/services": {CloneFolder: filepath.Join(parentPath, "services")},
					"Org/zzz":      {}, // standard location, not nested
				},
				RepoOrder: []string{"Org/project", "Org/browser", "Org/services", "Org/zzz"},
			},
		},
		SourceOrder: []string{"acct"},
	}

	m := dashboardModel{
		cfg: cfg,
		statuses: []status.RepoStatus{
			{Source: "acct", Repo: "Org/project", State: status.Clean},
			{Source: "acct", Repo: "Org/browser", State: status.Clean},
			{Source: "acct", Repo: "Org/services", State: status.Clean},
			{Source: "acct", Repo: "Org/zzz", State: status.Clean},
		},
	}

	rows := m.accountRepoRows()
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	// Expected order: project (container, indent 0), browser (1), services (1),
	// zzz (0, sorts after project's children).
	want := []struct {
		repo   string
		indent int
	}{
		{"Org/project", 0},
		{"Org/browser", 1},
		{"Org/services", 1},
		{"Org/zzz", 0},
	}
	for i, w := range want {
		if rows[i].st.Repo != w.repo || rows[i].indent != w.indent {
			t.Errorf("row %d = (%s, indent %d), want (%s, indent %d)",
				i, rows[i].st.Repo, rows[i].indent, w.repo, w.indent)
		}
	}

	if !m.isContainerRepo("acct", "Org/project") {
		t.Error("Org/project should be a container")
	}
	if m.isContainerRepo("acct", "Org/browser") {
		t.Error("Org/browser should not be a container")
	}
}
