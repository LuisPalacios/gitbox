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
// repos — enough for workspace CLI tests to exercise add / add-member / generate.
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

func cliAssertConfigHasWorkspace(t *testing.T, cfgPath, key string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if _, ok := cfg.Workspaces[key]; !ok {
		t.Errorf("expected workspace %q in config", key)
	}
}

func cliAssertConfigNoWorkspace(t *testing.T, cfgPath, key string) {
	t.Helper()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if _, ok := cfg.Workspaces[key]; ok {
		t.Errorf("expected workspace %q to be absent", key)
	}
}

func TestCLI_WorkspaceAdd(t *testing.T) {
	env := setupCLIEnvWithConfig(t, newCLIWorkspaceConfig(filepath.Join(t.TempDir(), "git")))

	r := env.run(t, "workspace", "add", "feat-x",
		"--type", "codeWorkspace",
		"--member", "github-test/team/frontend",
	)
	if r.ExitCode != 0 {
		t.Fatalf("workspace add failed: %s", r.Stderr)
	}
	cliAssertConfigHasWorkspace(t, env.CfgPath, "feat-x")

	cfg, _ := config.Load(env.CfgPath)
	ws := cfg.Workspaces["feat-x"]
	if ws.Type != config.WorkspaceTypeCode {
		t.Errorf("type = %q, want codeWorkspace", ws.Type)
	}
	if len(ws.Members) != 1 || ws.Members[0].Repo != "team/frontend" {
		t.Errorf("members = %+v, want one team/frontend member", ws.Members)
	}
}

func TestCLI_WorkspaceAddInvalidType(t *testing.T) {
	env := setupCLIEnvWithConfig(t, newCLIWorkspaceConfig(filepath.Join(t.TempDir(), "git")))

	r := env.run(t, "workspace", "add", "feat-x", "--type", "bogus")
	if r.ExitCode == 0 {
		t.Error("expected non-zero exit for invalid type")
	}
}

func TestCLI_WorkspaceAddUnknownSource(t *testing.T) {
	env := setupCLIEnvWithConfig(t, newCLIWorkspaceConfig(filepath.Join(t.TempDir(), "git")))

	r := env.run(t, "workspace", "add", "feat-x",
		"--type", "codeWorkspace",
		"--member", "nope/team/frontend",
	)
	if r.ExitCode == 0 {
		t.Error("expected non-zero exit for unknown source")
	}
}

func TestCLI_WorkspaceList(t *testing.T) {
	cfg := newCLIWorkspaceConfig(filepath.Join(t.TempDir(), "git"))
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {Type: config.WorkspaceTypeCode, Members: []config.WorkspaceMember{
			{Source: "github-test", Repo: "team/frontend"},
		}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	r := env.run(t, "workspace", "list")
	if r.ExitCode != 0 {
		t.Fatalf("list failed: %s", r.Stderr)
	}
	if !strings.Contains(r.Stdout, "feat-x") {
		t.Errorf("stdout missing workspace key:\n%s", r.Stdout)
	}
}

func TestCLI_WorkspaceListEmpty(t *testing.T) {
	env := setupCLIEnvWithConfig(t, newCLIWorkspaceConfig(filepath.Join(t.TempDir(), "git")))

	r := env.run(t, "workspace", "list")
	if r.ExitCode != 0 {
		t.Fatalf("list failed: %s", r.Stderr)
	}
	if !strings.Contains(r.Stdout, "No workspaces") {
		t.Errorf("stdout = %q, want empty-state message", r.Stdout)
	}
}

func TestCLI_WorkspaceListJSON(t *testing.T) {
	cfg := newCLIWorkspaceConfig(filepath.Join(t.TempDir(), "git"))
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {Type: config.WorkspaceTypeCode, Members: []config.WorkspaceMember{
			{Source: "github-test", Repo: "team/frontend"},
		}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	var out map[string]any
	r := env.runJSON(t, &out, "workspace", "list")
	if r.ExitCode != 0 {
		t.Fatalf("list --json failed: %s", r.Stderr)
	}
	if _, ok := out["feat-x"]; !ok {
		t.Errorf("json output missing feat-x: %v", out)
	}
}

func TestCLI_WorkspaceShow(t *testing.T) {
	cfg := newCLIWorkspaceConfig(filepath.Join(t.TempDir(), "git"))
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {Type: config.WorkspaceTypeCode, Name: "Feature X", Members: []config.WorkspaceMember{
			{Source: "github-test", Repo: "team/frontend"},
		}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	r := env.run(t, "workspace", "show", "feat-x")
	if r.ExitCode != 0 {
		t.Fatalf("show failed: %s", r.Stderr)
	}
	if !strings.Contains(r.Stdout, "Feature X") || !strings.Contains(r.Stdout, "team/frontend") {
		t.Errorf("show output missing expected content:\n%s", r.Stdout)
	}
}

func TestCLI_WorkspaceShowMissing(t *testing.T) {
	env := setupCLIEnvWithConfig(t, newCLIWorkspaceConfig(filepath.Join(t.TempDir(), "git")))

	r := env.run(t, "workspace", "show", "nope")
	if r.ExitCode == 0 {
		t.Error("expected non-zero exit for missing workspace")
	}
}

func TestCLI_WorkspaceDelete(t *testing.T) {
	cfg := newCLIWorkspaceConfig(filepath.Join(t.TempDir(), "git"))
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {Type: config.WorkspaceTypeCode, Members: []config.WorkspaceMember{
			{Source: "github-test", Repo: "team/frontend"},
		}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	r := env.run(t, "workspace", "delete", "feat-x")
	if r.ExitCode != 0 {
		t.Fatalf("delete failed: %s", r.Stderr)
	}
	cliAssertConfigNoWorkspace(t, env.CfgPath, "feat-x")
}

func TestCLI_WorkspaceAddMember(t *testing.T) {
	cfg := newCLIWorkspaceConfig(filepath.Join(t.TempDir(), "git"))
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {Type: config.WorkspaceTypeCode, Members: []config.WorkspaceMember{
			{Source: "github-test", Repo: "team/frontend"},
		}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	r := env.run(t, "workspace", "add-member", "feat-x", "github-test/team/backend")
	if r.ExitCode != 0 {
		t.Fatalf("add-member failed: %s", r.Stderr)
	}
	cfg2, _ := config.Load(env.CfgPath)
	if len(cfg2.Workspaces["feat-x"].Members) != 2 {
		t.Errorf("members = %d, want 2", len(cfg2.Workspaces["feat-x"].Members))
	}
}

func TestCLI_WorkspaceDeleteMember(t *testing.T) {
	cfg := newCLIWorkspaceConfig(filepath.Join(t.TempDir(), "git"))
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {Type: config.WorkspaceTypeCode, Members: []config.WorkspaceMember{
			{Source: "github-test", Repo: "team/frontend"},
			{Source: "github-test", Repo: "team/backend"},
		}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	r := env.run(t, "workspace", "delete-member", "feat-x", "github-test/team/frontend")
	if r.ExitCode != 0 {
		t.Fatalf("delete-member failed: %s", r.Stderr)
	}
	cfg2, _ := config.Load(env.CfgPath)
	if len(cfg2.Workspaces["feat-x"].Members) != 1 {
		t.Errorf("members = %d, want 1", len(cfg2.Workspaces["feat-x"].Members))
	}
}

func TestCLI_WorkspaceGenerateDryRun(t *testing.T) {
	cfg := newCLIWorkspaceConfig(filepath.Join(t.TempDir(), "git"))
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {Type: config.WorkspaceTypeCode, Members: []config.WorkspaceMember{
			{Source: "github-test", Repo: "team/frontend"},
		}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	r := env.run(t, "workspace", "generate", "feat-x", "--dry-run")
	if r.ExitCode != 0 {
		t.Fatalf("generate --dry-run failed: %s", r.Stderr)
	}
	if !strings.Contains(r.Stdout, "\"folders\"") {
		t.Errorf("dry-run output missing JSON body:\n%s", r.Stdout)
	}
	// The on-disk file MUST NOT be written on dry-run.
	cfg2, _ := config.Load(env.CfgPath)
	if ws := cfg2.Workspaces["feat-x"]; ws.File != "" {
		if _, err := os.Stat(ws.File); err == nil {
			t.Errorf("dry-run should not create %s", ws.File)
		}
	}
}

func TestCLI_WorkspaceGenerateWritesFile(t *testing.T) {
	cfg := newCLIWorkspaceConfig(filepath.Join(t.TempDir(), "git"))
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {Type: config.WorkspaceTypeCode, Members: []config.WorkspaceMember{
			{Source: "github-test", Repo: "team/frontend"},
		}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	r := env.run(t, "workspace", "generate", "feat-x")
	if r.ExitCode != 0 {
		t.Fatalf("generate failed: %s", r.Stderr)
	}

	cfg2, _ := config.Load(env.CfgPath)
	ws := cfg2.Workspaces["feat-x"]
	if ws.File == "" {
		t.Fatal("workspace.File not persisted after generate")
	}
	data, err := os.ReadFile(ws.File)
	if err != nil {
		t.Fatalf("reading generated file: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("generated file is not valid JSON: %v", err)
	}
	if _, ok := parsed["folders"]; !ok {
		t.Error("generated file missing folders array")
	}
}

func TestCLI_WorkspaceOpenWithoutEditor(t *testing.T) {
	cfg := newCLIWorkspaceConfig(filepath.Join(t.TempDir(), "git"))
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {Type: config.WorkspaceTypeCode, Members: []config.WorkspaceMember{
			{Source: "github-test", Repo: "team/frontend"},
		}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	// No editors configured — open should fail with a clear error AFTER the
	// file has been generated (generate runs first in the open flow).
	r := env.run(t, "workspace", "open", "feat-x")
	if r.ExitCode == 0 {
		t.Error("expected non-zero exit when no editors are configured")
	}
	if !strings.Contains(r.Stderr, "editor") {
		t.Errorf("stderr should mention editor configuration; got: %s", r.Stderr)
	}
}
