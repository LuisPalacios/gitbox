package config

import "testing"

// TestMigrateV2ToV3 verifies that a real-world v2 config (with the old
// workspace section and legacy fields) loads, bumps to v3, and discards the
// regenerable workspace cache while preserving accounts/sources untouched.
func TestMigrateV2ToV3(t *testing.T) {
	v2 := `{
  "version": 2,
  "global": {
    "folder": "~/git",
    "terminals": [{"name": "pwsh", "command": "wt.exe", "args": ["pwsh"]}]
  },
  "accounts": {
    "gh": {"provider": "github", "url": "https://github.com", "username": "u", "name": "N", "email": "e@e"}
  },
  "sources": {
    "gh": {"account": "gh", "repos": {"org/repo": {}}}
  },
  "workspaces": {
    "old-code":  {"type": "codeWorkspace", "name": "Old", "discovered": true, "members": []},
    "old-tmux":  {"type": "tmuxinator", "layout": "splitPanes", "members": []},
    "old-manual":{"type": "codeWorkspace", "members": []}
  }
}`
	cfg, err := Parse([]byte(v2))
	if err != nil {
		t.Fatalf("Parse v2: %v", err)
	}
	if cfg.Version != CurrentVersion {
		t.Errorf("version = %d, want %d", cfg.Version, CurrentVersion)
	}
	// Workspaces are dropped wholesale on migration — they rediscover.
	if len(cfg.Workspaces) != 0 {
		t.Errorf("workspaces after migration = %v, want none (cache is regenerable)", cfg.Workspaces)
	}
	if len(cfg.WorkspaceOrder) != 0 {
		t.Errorf("workspace order after migration = %v, want none", cfg.WorkspaceOrder)
	}
	// Accounts and sources are preserved.
	if _, ok := cfg.Accounts["gh"]; !ok {
		t.Error("account gh lost in migration")
	}
	if _, ok := cfg.Sources["gh"].Repos["org/repo"]; !ok {
		t.Error("repo org/repo lost in migration")
	}
	// New v3 fields default cleanly.
	if cfg.Global.NestedScanDepthOrDefault() != DefaultNestedScanDepth {
		t.Error("nested scan depth should default")
	}
	if len(cfg.Global.ExtraFolders) != 0 {
		t.Error("extra folders should be empty after migration")
	}
}

func TestParse_RejectsUnknownVersion(t *testing.T) {
	if _, err := Parse([]byte(`{"version": 99, "global": {"folder": "/x"}, "accounts": {}, "sources": {}}`)); err == nil {
		t.Error("expected error for unsupported version 99")
	}
}
