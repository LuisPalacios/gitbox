package main

import (
	"os"
	"path/filepath"
	"reflect"
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
		Global:  config.GlobalConfig{Roots: []string{dir}},
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

func TestSetCollapsed_SidebarAndView(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)

	if err := a.SetCollapsed(config.CollapsedState{Sidebar: true, View: "account:github-alice"}); err != nil {
		t.Fatalf("SetCollapsed: %v", err)
	}
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c := reloaded.Global.Collapsed; c == nil || !c.Sidebar || c.View != "account:github-alice" {
		t.Errorf("persisted Collapsed = %+v, want sidebar and the account view", c)
	}
	if got := a.GetCollapsed(); !got.Sidebar || got.View != "account:github-alice" {
		t.Errorf("GetCollapsed() = %+v, want sidebar and the account view", got)
	}

	// A view naming a missing account is pruned on save.
	if err := a.SetCollapsed(config.CollapsedState{View: "account:github-gone"}); err != nil {
		t.Fatalf("SetCollapsed stale view: %v", err)
	}
	reloaded, _ = config.Load(cfgPath)
	if reloaded.Global.Collapsed != nil {
		t.Errorf("Collapsed = %+v, want nil after a stale view", reloaded.Global.Collapsed)
	}
}

// containerClone makes team/frontend a clone on disk holding the given
// workspace files, and returns its path.
func containerClone(t *testing.T, a *App, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(a.cfg.Global.Roots[0], "github-alice", "team", "frontend")
	initClone(t, dir, "git@github.com:team/frontend.git")
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// A workspace file appearing in a clone makes it a container on the next
// pass, which also adopts its nested clone and lists the missing folder.
func TestReconcile_WorkspaceFileMakesContainer(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)
	dir := containerClone(t, a, map[string]string{
		"front.code-workspace": `{"folders": [{"path": "."}, {"path": "libs/fresh"}, {"path": "libs/todo"}]}`,
	})
	initClone(t, filepath.Join(dir, "libs", "fresh"), "git@github.com:alice/fresh.git")

	res := a.Reconcile()
	if res.Error != "" || !res.WorkspacesChanged || res.Adopted != 1 {
		t.Fatalf("reconcile = %+v", res)
	}
	want := []WorkspaceFolderDTO{{Parent: "github-alice/team/frontend", Name: "todo", Path: "libs/todo"}}
	if !reflect.DeepEqual(res.Placeholders, want) {
		t.Errorf("placeholders = %+v, want %+v", res.Placeholders, want)
	}
	reloaded, _ := config.Load(cfgPath)
	repos := reloaded.Sources["github-alice"].Repos
	if r := repos["team/frontend"]; !r.Container || r.Workspace != "front.code-workspace" {
		t.Errorf("container = %+v", r)
	}
	if _, ok := repos["alice/fresh"]; !ok {
		t.Error("nested clone not adopted")
	}
	if res := a.Reconcile(); res.WorkspacesChanged {
		t.Error("second pass changed workspaces again")
	}
}

func TestReconcile_SeveralWorkspaceFilesAskThenSetRepoWorkspace(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)
	containerClone(t, a, map[string]string{"a.code-workspace": "{}", "b.code-workspace": "{}"})

	res := a.Reconcile()
	want := []WorkspaceChoiceDTO{{Source: "github-alice", Repo: "team/frontend", Files: []string{"a.code-workspace", "b.code-workspace"}}}
	if !reflect.DeepEqual(res.WorkspaceChoices, want) {
		t.Fatalf("choices = %+v, want %+v", res.WorkspaceChoices, want)
	}
	if got := a.RepoWorkspaceFiles("github-alice", "team/frontend"); !reflect.DeepEqual(got, want[0].Files) {
		t.Errorf("RepoWorkspaceFiles = %v", got)
	}
	if err := a.SetRepoWorkspace("github-alice", "team/frontend", "c.code-workspace"); err == nil {
		t.Error("SetRepoWorkspace accepted a file that is not at the root")
	}
	if err := a.SetRepoWorkspace("github-alice", "team/frontend", "b.code-workspace"); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := config.Load(cfgPath)
	if r := reloaded.Sources["github-alice"].Repos["team/frontend"]; !r.Container || r.Workspace != "b.code-workspace" {
		t.Errorf("repo = %+v", r)
	}
	if res := a.Reconcile(); len(res.WorkspaceChoices) != 0 {
		t.Errorf("asked again after the pick: %+v", res.WorkspaceChoices)
	}
}
