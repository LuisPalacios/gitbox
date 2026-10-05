package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// newWorkspaceTestApp builds an App with a populated config and a real on-disk
// cfgPath so bindings can round-trip through config.Save.
func newWorkspaceTestApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gitbox.json")
	cfg := &config.Config{
		Version: config.CurrentVersion,
		Global:  config.GlobalConfig{Folder: dir},
		Accounts: map[string]config.Account{
			"github-alice": {Provider: "github", URL: "https://github.com",
				Username: "alice", Name: "Alice", Email: "a@e"},
		},
		Sources: map[string]config.Source{
			"github-alice": {
				Account: "github-alice",
				Repos: map[string]config.Repo{
					"team/frontend": {},
					"team/backend":  {},
				},
			},
		},
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("seeding config: %v", err)
	}
	return &App{cfg: cfg, cfgPath: cfgPath, cfgLoaded: true, mu: sync.Mutex{}}, cfgPath
}

func TestDiscoverWorkspaces_RefreshesCache(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)

	// Stage a clone path resolving to (github-alice, team/frontend) plus a
	// .code-workspace referencing it.
	cloneDir := filepath.Join(a.cfg.Global.Folder, "github-alice", "team", "frontend")
	if err := os.MkdirAll(cloneDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body, _ := json.Marshal(map[string]any{
		"folders": []map[string]string{{"path": cloneDir}},
	})
	wsFile := filepath.Join(a.cfg.Global.Folder, "feat.code-workspace")
	if err := os.WriteFile(wsFile, body, 0o644); err != nil {
		t.Fatalf("write workspace: %v", err)
	}

	res, err := a.DiscoverWorkspaces()
	if err != nil {
		t.Fatalf("DiscoverWorkspaces: %v", err)
	}
	if !res.Changed || res.Count != 1 {
		t.Fatalf("result = %+v, want changed/count 1", res)
	}
	reloaded, _ := config.Load(cfgPath)
	if _, ok := reloaded.Workspaces["feat"]; !ok {
		t.Error("workspace 'feat' not persisted")
	}
}

func TestSetRepoContainer_Persists(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)

	if err := a.SetRepoContainer("github-alice", "team/frontend", true); err != nil {
		t.Fatalf("SetRepoContainer: %v", err)
	}
	reloaded, _ := config.Load(cfgPath)
	if !reloaded.Sources["github-alice"].Repos["team/frontend"].Container {
		t.Error("container flag not persisted")
	}

	if err := a.SetRepoContainer("github-alice", "team/frontend", false); err != nil {
		t.Fatalf("SetRepoContainer off: %v", err)
	}
	reloaded, _ = config.Load(cfgPath)
	if reloaded.Sources["github-alice"].Repos["team/frontend"].Container {
		t.Error("container flag not cleared")
	}
}

func TestExtraFolders_AddRemove(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)

	if err := a.AddExtraFolder("/work/extra"); err != nil {
		t.Fatalf("AddExtraFolder: %v", err)
	}
	// Adding the same folder again is a no-op (deduped).
	if err := a.AddExtraFolder("/work/extra"); err != nil {
		t.Fatalf("AddExtraFolder dup: %v", err)
	}
	reloaded, _ := config.Load(cfgPath)
	if len(reloaded.Global.ExtraFolders) != 1 {
		t.Fatalf("extra folders = %v, want one", reloaded.Global.ExtraFolders)
	}

	if err := a.RemoveExtraFolder("/work/extra"); err != nil {
		t.Fatalf("RemoveExtraFolder: %v", err)
	}
	reloaded, _ = config.Load(cfgPath)
	if len(reloaded.Global.ExtraFolders) != 0 {
		t.Errorf("extra folders = %v, want none", reloaded.Global.ExtraFolders)
	}
}

func TestSetNestedScanDepth_Validates(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)

	if err := a.SetNestedScanDepth(0); err == nil {
		t.Error("expected error for depth 0")
	}
	if err := a.SetNestedScanDepth(2); err != nil {
		t.Fatalf("SetNestedScanDepth: %v", err)
	}
	reloaded, _ := config.Load(cfgPath)
	if reloaded.Global.NestedScanDepth != 2 {
		t.Errorf("depth = %d, want 2", reloaded.Global.NestedScanDepth)
	}
}

func TestAddDiscoveredReposToFolder_StoresAbsoluteCloneFolder(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)

	custom := filepath.Join(t.TempDir(), "custom-loc")
	if err := a.AddDiscoveredReposToFolder("github-alice", []string{"team/newrepo"}, custom); err != nil {
		t.Fatalf("AddDiscoveredReposToFolder: %v", err)
	}
	reloaded, _ := config.Load(cfgPath)
	repo, ok := reloaded.Sources["github-alice"].Repos["team/newrepo"]
	if !ok {
		t.Fatal("repo not added")
	}
	if repo.CloneFolder != custom {
		t.Errorf("clone_folder = %q, want %q", repo.CloneFolder, custom)
	}
}

func TestCollapsed_DefaultEmpty(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)
	got := a.GetCollapsed()
	if len(got.Sources) != 0 || len(got.Repos) != 0 {
		t.Errorf("GetCollapsed() = %+v, want empty", got)
	}
}

func TestSetCollapsed_PersistsAndPrunes(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)

	err := a.SetCollapsed(config.CollapsedState{
		Sources: []string{"github-alice", "github-gone"},
		Repos:   []string{"github-alice/team/frontend", "github-alice/team/deleted"},
	})
	if err != nil {
		t.Fatalf("SetCollapsed: %v", err)
	}
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c := reloaded.Global.Collapsed
	if c == nil || len(c.Sources) != 1 || c.Sources[0] != "github-alice" ||
		len(c.Repos) != 1 || c.Repos[0] != "github-alice/team/frontend" {
		t.Errorf("persisted Collapsed = %+v, want only the existing keys", c)
	}

	// Expanding everything removes the key from disk.
	if err := a.SetCollapsed(config.CollapsedState{}); err != nil {
		t.Fatalf("SetCollapsed empty: %v", err)
	}
	reloaded, _ = config.Load(cfgPath)
	if reloaded.Global.Collapsed != nil {
		t.Errorf("Collapsed = %+v, want nil after expanding everything", reloaded.Global.Collapsed)
	}
}
