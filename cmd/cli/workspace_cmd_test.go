package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// newCLIWorkspaceConfig seeds a config with one account, one source, and two
// repos under a real on-disk git folder so discovery can resolve members.
func newCLIWorkspaceConfig(gitFolder string) *config.Config {
	cfg := newCLITestConfig(gitFolder)
	cfg.Accounts["github-test"] = config.Account{
		Provider: "github",
		URL:      "https://github.com",
		Username: "testuser",
		Name:     "Test",
		Email:    "t@t",
	}
	cfg.Sources["github-test"] = config.Source{
		Account: "github-test",
		Repos: map[string]config.Repo{
			"team/frontend": {},
			"team/backend":  {},
		},
	}
	return cfg
}

// seedCodeWorkspace writes a .code-workspace under the git folder referencing
// the given on-disk folders, and creates those folders so member resolution
// can map them back to configured repos.
func seedCodeWorkspace(t *testing.T, gitFolder, name string, repoDirs ...string) string {
	t.Helper()
	body := struct {
		Folders []map[string]string `json:"folders"`
	}{}
	for _, d := range repoDirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
		body.Folders = append(body.Folders, map[string]string{"path": d})
	}
	buf, _ := json.MarshalIndent(body, "", "  ")
	file := filepath.Join(gitFolder, name+".code-workspace")
	if err := os.MkdirAll(gitFolder, 0o755); err != nil {
		t.Fatalf("mkdir git folder: %v", err)
	}
	if err := os.WriteFile(file, buf, 0o644); err != nil {
		t.Fatalf("write workspace: %v", err)
	}
	return file
}

func TestCLI_WorkspaceDiscoverPopulatesCache(t *testing.T) {
	gitFolder := filepath.Join(t.TempDir(), "git")
	cfg := newCLIWorkspaceConfig(gitFolder)
	env := setupCLIEnvWithConfig(t, cfg)

	frontend := filepath.Join(gitFolder, "github-test", "team", "frontend")
	seedCodeWorkspace(t, gitFolder, "feature-x", frontend)

	r := env.run(t, "workspace", "discover")
	if r.ExitCode != 0 {
		t.Fatalf("workspace discover failed: %s", r.Stderr)
	}

	cfg2, err := config.Load(env.CfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	ws, ok := cfg2.Workspaces["feature-x"]
	if !ok {
		t.Fatalf("expected feature-x in cache, got %v", cfg2.Workspaces)
	}
	if !ws.Discovered {
		t.Error("discovered workspace should be marked Discovered")
	}
	if len(ws.Members) != 1 || ws.Members[0].Repo != "team/frontend" {
		t.Errorf("members = %+v, want one team/frontend", ws.Members)
	}
}

func TestCLI_WorkspaceListShowsCache(t *testing.T) {
	gitFolder := filepath.Join(t.TempDir(), "git")
	cfg := newCLIWorkspaceConfig(gitFolder)
	cfg.Workspaces = map[string]config.Workspace{
		"feat": {Name: "feat", File: filepath.Join(gitFolder, "feat.code-workspace"), Discovered: true},
	}
	cfg.WorkspaceOrder = []string{"feat"}
	env := setupCLIEnvWithConfig(t, cfg)

	r := env.run(t, "workspace", "list")
	if r.ExitCode != 0 {
		t.Fatalf("workspace list failed: %s", r.Stderr)
	}
	if !strings.Contains(r.Stdout, "feat") {
		t.Errorf("list output missing feat: %s", r.Stdout)
	}
}
