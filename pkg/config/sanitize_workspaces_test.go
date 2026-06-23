package config

import "testing"

// TestParseV2_SanitizesWorkspaceCache verifies the read-only-cache invariant:
// only discovered codeWorkspace entries survive; legacy tmuxinator and
// user-created (non-discovered) entries are dropped on load.
func TestParseV2_SanitizesWorkspaceCache(t *testing.T) {
	in := `{
  "version": 2,
  "global": {"folder": "/tmp"},
  "accounts": {"a": {"provider": "github", "url": "https://github.com", "username": "u", "name": "n", "email": "e@e"}},
  "sources": {"a": {"account": "a", "repos": {"o/r": {}}}},
  "workspaces": {
    "keep":  {"type": "codeWorkspace", "discovered": true, "file": "/tmp/keep.code-workspace", "members": []},
    "tmux":  {"type": "tmuxinator", "discovered": true, "members": []},
    "manual":{"type": "codeWorkspace", "members": []}
  }
}`
	cfg, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Workspaces) != 1 {
		t.Fatalf("workspaces = %d (%v), want only 'keep'", len(cfg.Workspaces), cfg.Workspaces)
	}
	if _, ok := cfg.Workspaces["keep"]; !ok {
		t.Error("discovered codeWorkspace 'keep' should survive")
	}
	if _, ok := cfg.Workspaces["tmux"]; ok {
		t.Error("tmuxinator workspace must be dropped")
	}
	if _, ok := cfg.Workspaces["manual"]; ok {
		t.Error("non-discovered workspace must be dropped")
	}
	// Order slice must be pruned to surviving keys only.
	for _, k := range cfg.WorkspaceOrder {
		if _, ok := cfg.Workspaces[k]; !ok {
			t.Errorf("WorkspaceOrder references dropped key %q", k)
		}
	}
}

func TestNestedScanDepthOrDefault(t *testing.T) {
	g := GlobalConfig{}
	if d := g.NestedScanDepthOrDefault(); d != DefaultNestedScanDepth {
		t.Errorf("unset depth = %d, want default %d", d, DefaultNestedScanDepth)
	}
	g.NestedScanDepth = 3
	if d := g.NestedScanDepthOrDefault(); d != 3 {
		t.Errorf("depth = %d, want 3", d)
	}
}

func TestExtraFolders_AddRemoveDedup(t *testing.T) {
	g := &GlobalConfig{}
	if !g.AddExtraFolder("/work/x") {
		t.Fatal("first add should return true")
	}
	if g.AddExtraFolder("/work/x") {
		t.Error("duplicate add should return false")
	}
	if len(g.ExtraFolders) != 1 {
		t.Fatalf("extra folders = %v, want one", g.ExtraFolders)
	}
	if !g.RemoveExtraFolder("/work/x") {
		t.Error("remove should return true")
	}
	if len(g.ExtraFolders) != 0 {
		t.Errorf("after remove = %v, want none", g.ExtraFolders)
	}
}
