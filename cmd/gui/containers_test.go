package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/workspace"
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
	return newTestApp(withCfg(cfg), withCfgPath(cfgPath), withCfgLoaded()), cfgPath
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
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.be().AddDiscoveredReposToFolder("github-alice", []string{"team/newrepo", "team/another"}, custom); !errors.Is(err, config.ErrPathCollision) {
		t.Fatalf("shared custom destination = %v", err)
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil || string(before) != string(after) || len(a.cfg.Sources["github-alice"].Repos) != 2 {
		t.Fatal("refused batch changed the config")
	}
	if err := a.be().AddDiscoveredReposToFolder("github-alice", []string{"team/newrepo"}, custom); err != nil {
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
	cfg, err := a.GetConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Global.Collapsed; got != nil && (len(got.Sources) != 0 || len(got.Repos) != 0) {
		t.Errorf("GetConfig collapsed = %+v, want empty", got)
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
	if cfg, err := a.GetConfig(); err != nil {
		t.Fatal(err)
	} else if got := cfg.Global.Collapsed; got == nil || !got.Sidebar || got.View != "account:github-alice" {
		t.Errorf("GetConfig collapsed = %+v, want sidebar and the account view", got)
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
	if want := []string{"github-alice/alice/fresh"}; !reflect.DeepEqual(res.WorkspaceMembers, want) {
		t.Errorf("members = %v, want %v", res.WorkspaceMembers, want)
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

func TestReconcile_ReportsOversizedWorkspace(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)
	containerClone(t, a, map[string]string{
		"large.code-workspace": `{"folders":[{"path":"missing"}]}` + strings.Repeat(" ", 2<<20),
	})
	res := a.Reconcile()
	want := []WorkspaceDiagnosticDTO{{Parent: "github-alice/team/frontend", File: "large.code-workspace", Problem: workspace.TooLarge}}
	if res.Error != "" || len(res.Placeholders) != 0 || !reflect.DeepEqual(res.WorkspaceDiagnostics, want) {
		t.Fatalf("reconcile = %+v, want oversized diagnostic and no workspace folders", res)
	}
}

func TestReconcile_ExternalWorkspaceCloneRequiresExplicitAdoption(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)
	dir := containerClone(t, a, map[string]string{
		"front.code-workspace": `{"folders":[{"path":"external/outside-clone"}]}`,
	})
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remoteClone := filepath.Join(outside, "outside-clone")
	initClone(t, remoteClone, "git@github.com:alice/outside-clone.git")
	link := filepath.Join(dir, "external")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" && (errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.Errno(1314))) {
			t.Skipf("creating symlinks requires privileges: %v", err)
		}
		t.Fatal(err)
	}
	other := t.TempDir()
	unrelated := filepath.Join(other, "unrelated")
	initClone(t, unrelated, "git@github.com:alice/unrelated.git")
	res := a.Reconcile()
	want := []WorkspaceFolderDTO{{Parent: "github-alice/team/frontend", Name: "outside-clone", Path: "external/outside-clone", OutsidePath: remoteClone}}
	if res.Error != "" || res.Adopted != 0 || len(res.Unmatched) != 0 || len(res.WorkspaceMembers) != 0 || !reflect.DeepEqual(res.Placeholders, want) {
		t.Fatalf("automatic reconcile = %+v, want only the outside workspace row", res)
	}
	found := a.ScanFolderForClones(remoteClone)
	if len(found) != 1 || found[0].Path != remoteClone || found[0].WorkspaceRoot != remoteClone || found[0].MatchedAccount != "github-alice" {
		t.Fatalf("explicit scan = %+v, want the external clone", found)
	}
	for _, req := range []AdoptRequestDTO{
		{Path: remoteClone, Account: "github-alice"},
		{Path: unrelated, Account: "github-alice", WorkspaceRoot: remoteClone},
		{Path: unrelated, Account: "github-alice", WorkspaceRoot: other},
		{Path: remoteClone, Account: "another-account", WorkspaceRoot: remoteClone},
	} {
		if res := a.AdoptClones([]AdoptRequestDTO{req}); res.Error == "" || res.Adopted != 0 {
			t.Fatalf("unverified adoption %+v = %+v", req, res)
		}
	}
	req := AdoptRequestDTO{Path: remoteClone, Account: "github-alice", WorkspaceRoot: remoteClone}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if res := a.AdoptClones([]AdoptRequestDTO{req}); res.Error == "" || res.Adopted != 0 {
		t.Fatalf("stale workspace search authorized adoption: %+v", res)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if res := a.AdoptClones([]AdoptRequestDTO{req}); res.Error != "" || res.Adopted != 1 {
		t.Fatalf("explicit adoption = %+v, want one adoption", res)
	}
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if repo := reloaded.Sources["github-alice"].Repos["alice/outside-clone"]; repo.CloneFolder != remoteClone {
		t.Fatalf("persisted clone = %+v, want absolute original location %q", repo, remoteClone)
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
	if r := reloaded.Sources["github-alice"].Repos["team/frontend"]; !r.Container || r.Workspace != "b.code-workspace" || r.WorkspacePreferred != "b.code-workspace" {
		t.Errorf("repo = %+v", r)
	}
	if res := a.Reconcile(); len(res.WorkspaceChoices) != 0 {
		t.Errorf("asked again after the pick: %+v", res.WorkspaceChoices)
	}
	if err := a.SetRepoWorkspace("github-alice", "team/frontend", ""); err != nil {
		t.Fatal(err)
	}
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if repo := reloaded.Sources["github-alice"].Repos["team/frontend"]; !repo.Container || repo.Workspace != "" || repo.WorkspacePreferred != "" {
		t.Errorf("clearing workspace preference saved %+v", repo)
	}
}
