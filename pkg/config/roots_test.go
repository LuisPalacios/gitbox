package config

import (
	"errors"
	"reflect"
	"testing"
)

// rootsConfig has two roots, a source on the default and one pinned to the
// second, each with a repo.
func rootsConfig() *Config {
	return &Config{
		Version: CurrentVersion,
		Global:  GlobalConfig{Roots: []string{"/r1", "/r2"}},
		Accounts: map[string]Account{
			"a": {Provider: "github", URL: "https://github.com", Username: "u", Name: "N", Email: "e@e"},
		},
		Sources: map[string]Source{
			"one": {Account: "a", Repos: map[string]Repo{"org/x": {}, "org/y": {}}},
			"two": {Account: "a", Root: "/r2", Repos: map[string]Repo{"org/z": {}}},
		},
	}
}

func repoRoots(c *Config) map[string]string {
	out := map[string]string{}
	for sk, src := range c.Sources {
		for rk, repo := range src.Repos {
			out[sk+"/"+rk] = c.RepoRoot(sk, repo)
		}
	}
	return out
}

func TestRoots_Resolution(t *testing.T) {
	c := rootsConfig()
	if got := c.DefaultRoot(); got != "/r1" {
		t.Errorf("default = %q", got)
	}
	if got := c.SourceRoot("two"); got != "/r2" {
		t.Errorf("source root = %q", got)
	}
	if got := c.RepoRoot("one", Repo{Root: "/r2"}); got != "/r2" {
		t.Errorf("repo root = %q", got)
	}
	if got := c.RootRefs("/r2"); !reflect.DeepEqual(got, []string{"two/org/z"}) {
		t.Errorf("refs = %v", got)
	}
}

func TestRoots_AddDedup(t *testing.T) {
	c := rootsConfig()
	if c.AddRoot("/r1/") || c.AddRoot("  ") {
		t.Error("an equivalent or blank root must not be added")
	}
	if !c.AddRoot("/r3") || len(c.Global.Roots) != 3 {
		t.Errorf("roots = %v", c.Global.Roots)
	}
}

func TestRoots_SetSourceRootKeepsEveryClonePath(t *testing.T) {
	c := rootsConfig()
	before := repoRoots(c)
	c.SetSourceRoot("one", "/r3")
	if !reflect.DeepEqual(repoRoots(c), before) {
		t.Errorf("paths moved: %v, want %v", repoRoots(c), before)
	}
	if c.SourceRoot("one") != "/r3" || !c.HasRoot("/r3") {
		t.Errorf("source root = %q, roots = %v", c.SourceRoot("one"), c.Global.Roots)
	}
	// Back to the default: pins matching it go away.
	c.SetSourceRoot("one", "/r1")
	if src := c.Sources["one"]; src.Root != "" || src.Repos["org/x"].Root != "" {
		t.Errorf("redundant pins kept: %+v", src)
	}
}

func TestRoots_SetDefaultRootKeepsEveryClonePath(t *testing.T) {
	c := rootsConfig()
	before := repoRoots(c)
	c.SetDefaultRoot("/r2")
	if !reflect.DeepEqual(c.Global.Roots, []string{"/r2", "/r1"}) {
		t.Errorf("roots = %v", c.Global.Roots)
	}
	if !reflect.DeepEqual(repoRoots(c), before) {
		t.Errorf("paths moved: %v, want %v", repoRoots(c), before)
	}
	if c.Sources["two"].Root != "" {
		t.Error("a source on the new default keeps no pin")
	}
}

func TestRoots_Remove(t *testing.T) {
	c := rootsConfig()
	refs, err := c.RemoveRoot("/r2", false)
	if !errors.Is(err, ErrRootInUse) || len(refs) != 1 || len(c.Global.Roots) != 2 {
		t.Fatalf("in use: refs=%v err=%v roots=%v", refs, err, c.Global.Roots)
	}
	refs, err = c.RemoveRoot("/r2", true)
	if err != nil || !reflect.DeepEqual(refs, []string{"two/org/z"}) {
		t.Fatalf("forget: refs=%v err=%v", refs, err)
	}
	if len(c.Sources["two"].Repos) != 0 || c.Sources["two"].Root != "" || !reflect.DeepEqual(c.Global.Roots, []string{"/r1"}) {
		t.Errorf("after forget: %+v roots=%v", c.Sources["two"], c.Global.Roots)
	}
	if _, err := c.RemoveRoot("/r1", true); !errors.Is(err, ErrLastRoot) {
		t.Errorf("last root: err=%v", err)
	}
}

func TestMigrateV3ToV4_FolderAndExtraFoldersBecomeRoots(t *testing.T) {
	v3 := `{
  "version": 3,
  "global": {"folder": "~/git", "extra_folders": ["/work", "~/git/", "/more"]},
  "accounts": {"gh": {"provider": "github", "url": "https://github.com", "username": "u", "name": "N", "email": "e@e"}},
  "sources": {"gh": {"account": "gh", "repos": {"org/repo": {}}}},
  "workspaces": {"w": {"name": "W", "discovered": true, "file": "/work/w.code-workspace", "members": []}}
}`
	cfg, err := Parse([]byte(v3))
	if err != nil {
		t.Fatalf("Parse v3: %v", err)
	}
	if cfg.Version != CurrentVersion {
		t.Errorf("version = %d", cfg.Version)
	}
	if want := []string{"~/git", "/work", "/more"}; !reflect.DeepEqual(cfg.Global.Roots, want) {
		t.Errorf("roots = %v, want %v", cfg.Global.Roots, want)
	}
	if len(cfg.Workspaces) != 1 {
		t.Errorf("v3 workspaces must survive, got %v", cfg.Workspaces)
	}
}

func TestParse_ListsRootsSourcesAndReposName(t *testing.T) {
	v4 := `{
  "version": 4,
  "global": {"roots": ["/r1", ""]},
  "accounts": {"gh": {"provider": "github", "url": "https://github.com", "username": "u", "name": "N", "email": "e@e"}},
  "sources": {"gh": {"account": "gh", "root": "/r2", "repos": {"org/repo": {"root": "/r3"}}}}
}`
	cfg, err := Parse([]byte(v4))
	if err != nil {
		t.Fatalf("Parse v4: %v", err)
	}
	if want := []string{"/r1", "/r2", "/r3"}; !reflect.DeepEqual(cfg.Global.Roots, want) {
		t.Errorf("roots = %v, want %v", cfg.Global.Roots, want)
	}
}

func TestParse_RequiresARoot(t *testing.T) {
	if _, err := Parse([]byte(`{"version": 4, "global": {"roots": []}, "accounts": {}, "sources": {}}`)); err == nil {
		t.Error("expected an error without roots")
	}
}

func TestRoots_PlaceRepo(t *testing.T) {
	c := rootsConfig()
	if !c.PlaceRepo("one", "org/x", "/r2") {
		t.Fatal("placing into another root must change the config")
	}
	if got := c.RepoRoot("one", c.Sources["one"].Repos["org/x"]); got != "/r2" {
		t.Errorf("placed repo root = %q", got)
	}
	if got := c.RepoRoot("one", c.Sources["one"].Repos["org/y"]); got != "/r1" {
		t.Errorf("sibling moved to %q", got)
	}
	// Placing a pinned sibling into the source's new root drops its pin.
	if !c.PlaceRepo("one", "org/y", "/r2") || c.Sources["one"].Repos["org/y"].Root != "" {
		t.Errorf("pin kept: %+v", c.Sources["one"].Repos["org/y"])
	}
	if c.PlaceRepo("one", "org/y", "/r2") {
		t.Error("placing again must be a no-op")
	}
}
