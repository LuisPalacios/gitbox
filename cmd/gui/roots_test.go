package main

import (
	"path/filepath"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestRoots_AddDefaultRemove(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)
	first := a.cfg.Global.Roots[0]
	second := filepath.Join(t.TempDir(), "second")

	if err := a.AddRoot(second); err != nil {
		t.Fatalf("AddRoot: %v", err)
	}
	if err := a.AddRoot(second + "/"); err != nil {
		t.Fatalf("AddRoot dup: %v", err)
	}
	roots := a.ListRoots()
	if len(roots) != 2 || !roots[0].IsDefault || roots[0].Repos != 2 || !roots[1].Exists || roots[1].Repos != 0 {
		t.Fatalf("roots = %+v", roots)
	}

	// The default moves, the clones don't.
	if err := a.SetDefaultRoot(second); err != nil {
		t.Fatalf("SetDefaultRoot: %v", err)
	}
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.DefaultRoot() != second || reloaded.SourceRoot("github-alice") != first {
		t.Errorf("default = %q, source root = %q", reloaded.DefaultRoot(), reloaded.SourceRoot("github-alice"))
	}

	// The first root still holds both repos: removing it asks first.
	res, err := a.RemoveRoot(first, false)
	if err != nil || res.InUse != 2 || len(a.cfg.Global.Roots) != 2 {
		t.Fatalf("in use: %+v, %v, roots=%v", res, err, a.cfg.Global.Roots)
	}
	res, err = a.RemoveRoot(first, true)
	if err != nil || len(res.Forgotten) != 2 {
		t.Fatalf("forget: %+v, %v", res, err)
	}
	reloaded, _ = config.Load(cfgPath)
	if len(reloaded.Global.Roots) != 1 || len(reloaded.Sources["github-alice"].Repos) != 0 {
		t.Errorf("after forget: roots=%v repos=%v", reloaded.Global.Roots, reloaded.Sources["github-alice"].Repos)
	}
}

func TestAccountFolder_UsesSourceRootAndFolder(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{Roots: []string{"/r1", "/r2"}},
		Sources: map[string]config.Source{
			"acct":  {Account: "acct", Folder: "mine", Root: "/r2"},
			"other": {Account: "lone"},
		},
	}
	for _, c := range []struct{ account, want string }{
		{"acct", filepath.Join("/r2", "mine")},
		{"lone", filepath.Join("/r1", "other")},
		{"none", filepath.Join("/r1", "none")},
	} {
		if got := accountFolder(cfg, c.account); got != c.want {
			t.Errorf("accountFolder(%s) = %q, want %q", c.account, got, c.want)
		}
	}
}
