package config

import (
	"encoding/json"
	"testing"
)

// TestMigrateV4ToV5_DropsWorkspaces verifies that a v4 file's workspaces
// cache is dropped on load and never written back, while repos, their
// container flag and the new repo.workspace survive.
func TestMigrateV4ToV5_DropsWorkspaces(t *testing.T) {
	v4 := `{
  "version": 4,
  "global": {"roots": ["/tmp"]},
  "accounts": {"a": {"provider": "github", "url": "https://github.com", "username": "u", "name": "n", "email": "e@e"}},
  "sources": {"a": {"account": "a", "repos": {"o/r": {"container": true, "workspace": "r.code-workspace"}}}},
  "workspaces": {"w": {"discovered": true, "file": "/tmp/w.code-workspace", "members": []}}
}`
	cfg, err := Parse([]byte(v4))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Version != CurrentVersion {
		t.Errorf("version = %d, want %d", cfg.Version, CurrentVersion)
	}
	repo := cfg.Sources["a"].Repos["o/r"]
	if !repo.Container || repo.Workspace != "r.code-workspace" {
		t.Errorf("repo = %+v, want container with its workspace file", repo)
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var probe map[string]json.RawMessage
	_ = json.Unmarshal(out, &probe)
	if _, ok := probe["workspaces"]; ok {
		t.Error("workspaces section written back after migration")
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
