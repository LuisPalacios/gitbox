package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func collapsedTestConfig() *Config {
	return &Config{
		Version: CurrentVersion,
		Global:  GlobalConfig{Folder: "~/test"},
		Accounts: map[string]Account{
			"a": {Provider: "github", URL: "https://github.com", Username: "u", Name: "N", Email: "e@e"},
			"b": {Provider: "github", URL: "https://github.com", Username: "v", Name: "M", Email: "f@f"},
		},
		Sources: map[string]Source{
			"src-a": {Account: "a", Repos: map[string]Repo{"org/top": {}, "org/mid": {}}},
			"src-b": {Account: "b", Repos: map[string]Repo{"org/other": {}}},
		},
	}
}

func TestPruneCollapsed_DropsStaleKeys(t *testing.T) {
	cfg := collapsedTestConfig()
	cfg.Global.Collapsed = &CollapsedState{
		Sources: []string{"src-b", "gone", "src-a", "src-b"},
		Repos:   []string{"src-a/org/top", "src-a/org/deleted", "gone/org/x", "src-a/org/mid", "src-a/org/top", "noslash"},
	}
	cfg.PruneCollapsed()

	got := cfg.Global.Collapsed
	if got == nil {
		t.Fatal("Collapsed = nil, want surviving keys")
	}
	if want := []string{"src-a", "src-b"}; !slices.Equal(got.Sources, want) {
		t.Errorf("Sources = %v, want %v", got.Sources, want)
	}
	if want := []string{"src-a/org/mid", "src-a/org/top"}; !slices.Equal(got.Repos, want) {
		t.Errorf("Repos = %v, want %v", got.Repos, want)
	}
}

func TestPruneCollapsed_EmptyBecomesNil(t *testing.T) {
	cfg := collapsedTestConfig()
	cfg.Global.Collapsed = &CollapsedState{Sources: []string{"gone"}, Repos: []string{"src-a/org/deleted"}}
	cfg.PruneCollapsed()
	if cfg.Global.Collapsed != nil {
		t.Errorf("Collapsed = %+v, want nil", cfg.Global.Collapsed)
	}
}

// The sidebar rail and the last view are layout state on their own: they
// keep the struct alive with nothing folded.
func TestPruneCollapsed_KeepsSidebarAndView(t *testing.T) {
	cfg := collapsedTestConfig()
	cfg.Global.Collapsed = &CollapsedState{Sources: []string{"gone"}, Sidebar: true}
	cfg.PruneCollapsed()
	if got := cfg.Global.Collapsed; got == nil || !got.Sidebar || len(got.Sources) != 0 {
		t.Errorf("Collapsed = %+v, want only Sidebar", got)
	}

	for _, view := range []string{"attention", "all", "mirrors", "workspaces", "account:a"} {
		cfg.Global.Collapsed = &CollapsedState{View: view}
		cfg.PruneCollapsed()
		if got := cfg.Global.Collapsed; got == nil || got.View != view {
			t.Errorf("view %q: Collapsed = %+v, want the view kept", view, got)
		}
	}
}

// A view that names a deleted account is cleared, so the GUI falls back to
// Needs attention; the rest of the state survives.
func TestPruneCollapsed_DropsViewOfMissingAccount(t *testing.T) {
	cfg := collapsedTestConfig()
	cfg.Global.Collapsed = &CollapsedState{Sources: []string{"src-a"}, View: "account:gone"}
	cfg.PruneCollapsed()
	got := cfg.Global.Collapsed
	if got == nil || got.View != "" || !slices.Equal(got.Sources, []string{"src-a"}) {
		t.Errorf("Collapsed = %+v, want src-a and no view", got)
	}

	cfg.Global.Collapsed = &CollapsedState{View: "account:gone"}
	cfg.PruneCollapsed()
	if cfg.Global.Collapsed != nil {
		t.Errorf("Collapsed = %+v, want nil once the stale view is dropped", cfg.Global.Collapsed)
	}
}

func TestSave_PrunesCollapsedAndOmitsWhenEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gitbox.json")
	cfg := collapsedTestConfig()
	cfg.Global.Collapsed = &CollapsedState{Repos: []string{"src-a/org/top", "src-a/org/deleted"}}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Global.Collapsed == nil || !slices.Equal(reloaded.Global.Collapsed.Repos, []string{"src-a/org/top"}) {
		t.Errorf("reloaded Collapsed = %+v, want only src-a/org/top", reloaded.Global.Collapsed)
	}

	// Deleting the only collapsed repo leaves no "collapsed" key on disk.
	if err := cfg.DeleteRepo("src-a", "org/top"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"collapsed"`) {
		t.Errorf("saved config still holds a collapsed key:\n%s", data)
	}
}

// Every chevron click saves the config. Like window position, that must not
// rotate the genuine backups out of the ring.
func TestBackupSkippedForCollapsedOnlyChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")
	cfg := collapsedTestConfig()
	if err := Save(cfg, path); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	cfg.Global.Collapsed = &CollapsedState{Sources: []string{"src-a"}, Repos: []string{"src-b/org/other"}}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("collapse Save: %v", err)
	}
	cfg.Global.Collapsed = &CollapsedState{Sidebar: true, View: "account:a"}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("layout Save: %v", err)
	}
	cfg.Global.Collapsed = nil
	if err := Save(cfg, path); err != nil {
		t.Fatalf("expand Save: %v", err)
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "gitbox-????????-??????.json"))
	if len(matches) != 0 {
		t.Errorf("collapse and layout-only changes should not create backups, got %d", len(matches))
	}
}
