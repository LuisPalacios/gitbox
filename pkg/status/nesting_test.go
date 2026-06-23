package status

import (
	"path/filepath"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestComputeNesting_GroupsChildrenUnderContainer(t *testing.T) {
	root := t.TempDir() // real absolute path so absolute clone_folder is honored
	parentPath := filepath.Join(root, "acct", "Org", "project")

	cfg := &config.Config{
		Version: 2,
		Global:  config.GlobalConfig{Folder: root},
		Sources: map[string]config.Source{
			"acct": {
				Account: "acct",
				Repos: map[string]config.Repo{
					// The container parent at the standard path.
					"Org/project": {Container: true},
					// Two nested children with absolute clone_folder inside the parent.
					"Org/browser":  {CloneFolder: filepath.Join(parentPath, "browser")},
					"Org/services": {CloneFolder: filepath.Join(parentPath, "services")},
					// An unrelated repo elsewhere — not a child.
					"Org/other": {},
				},
				RepoOrder: []string{"Org/project", "Org/browser", "Org/services", "Org/other"},
			},
		},
		SourceOrder: []string{"acct"},
	}

	n := ComputeNesting(cfg)
	parent := RepoRef{Source: "acct", Repo: "Org/project"}
	browser := RepoRef{Source: "acct", Repo: "Org/browser"}
	other := RepoRef{Source: "acct", Repo: "Org/other"}

	if len(n.Children[parent]) != 2 {
		t.Fatalf("children = %v, want 2 under project", n.Children[parent])
	}
	if n.ParentOf[browser] != parent {
		t.Errorf("ParentOf[browser] = %v, want project", n.ParentOf[browser])
	}
	if _, isChild := n.ParentOf[other]; isChild {
		t.Errorf("Org/other should not be nested, got parent %v", n.ParentOf[other])
	}
	if _, isChild := n.ParentOf[parent]; isChild {
		t.Error("the container itself must not be its own child")
	}
}

func TestComputeNesting_NoContainersIsEmpty(t *testing.T) {
	cfg := &config.Config{
		Version: 2,
		Global:  config.GlobalConfig{Folder: "/x"},
		Sources: map[string]config.Source{
			"acct": {Account: "acct", Repos: map[string]config.Repo{"o/r": {}}, RepoOrder: []string{"o/r"}},
		},
		SourceOrder: []string{"acct"},
	}
	n := ComputeNesting(cfg)
	if len(n.ParentOf) != 0 || len(n.Children) != 0 {
		t.Errorf("no containers should yield empty nesting, got %+v", n)
	}
}
