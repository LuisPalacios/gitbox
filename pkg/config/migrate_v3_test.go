package config

import "testing"

// TestMigrateV2ToV3 verifies that a real-world v2 config (with the old
// workspace section and legacy fields) loads, bumps to the current version,
// and discards the workspace section while preserving accounts/sources.
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
	// v3→v4: the folder becomes the only root.
	if len(cfg.Global.Roots) != 1 || cfg.Global.Roots[0] != "~/git" {
		t.Errorf("roots = %v, want [~/git]", cfg.Global.Roots)
	}
}

func TestParse_RejectsUnknownVersion(t *testing.T) {
	if _, err := Parse([]byte(`{"version": 99, "global": {"folder": "/x"}, "accounts": {}, "sources": {}}`)); err == nil {
		t.Error("expected error for unsupported version 99")
	}
}
