package workspace

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// newTestConfig builds a minimal config with two sources under a known
// global folder, so path resolution is deterministic.
func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	return &config.Config{
		Version: 2,
		Global:  config.GlobalConfig{Folder: root},
		Accounts: map[string]config.Account{
			"github-test": {Provider: "github", URL: "https://github.com", Username: "u", Name: "N", Email: "e@e"},
			"forgejo-test": {Provider: "forgejo", URL: "https://f.local", Username: "u", Name: "N", Email: "e@e"},
		},
		Sources: map[string]config.Source{
			"github-test": {
				Account: "github-test",
				Repos: map[string]config.Repo{
					"team/frontend": {},
					"team/backend":  {},
				},
			},
			"forgejo-test": {
				Account: "forgejo-test",
				Folder:  "work",
				Repos: map[string]config.Repo{
					"team/infra": {},
				},
			},
		},
	}
}

func TestGenerate_UnknownWorkspace(t *testing.T) {
	cfg := newTestConfig(t)
	if _, err := Generate(cfg, "nope"); err == nil {
		t.Error("expected error for unknown workspace")
	}
}

func TestGenerate_EmptyMembers(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {Type: config.WorkspaceTypeCode, Members: nil},
	}
	if _, err := Generate(cfg, "feat-x"); err == nil {
		t.Error("expected error for empty members")
	}
}

func TestGenerate_CodeWorkspaceBasic(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {
			Type: config.WorkspaceTypeCode,
			Name: "Feature X",
			Members: []config.WorkspaceMember{
				{Source: "github-test", Repo: "team/frontend"},
				{Source: "github-test", Repo: "team/backend"},
			},
		},
	}

	res, err := Generate(cfg, "feat-x")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !strings.HasSuffix(res.File, "feat-x.code-workspace") {
		t.Errorf("file = %q, want suffix feat-x.code-workspace", res.File)
	}

	var parsed struct {
		Folders []struct {
			Path string `json:"path"`
			Name string `json:"name"`
		} `json:"folders"`
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(res.Content, &parsed); err != nil {
		t.Fatalf("parse generated JSON: %v", err)
	}
	if len(parsed.Folders) != 2 {
		t.Fatalf("folders = %d, want 2", len(parsed.Folders))
	}
	if parsed.Folders[0].Name != "frontend" {
		t.Errorf("folder[0].name = %q, want frontend", parsed.Folders[0].Name)
	}
	if parsed.Folders[1].Name != "backend" {
		t.Errorf("folder[1].name = %q, want backend", parsed.Folders[1].Name)
	}

	// Paths should be relative (members share a common ancestor with the file).
	for _, f := range parsed.Folders {
		if filepath.IsAbs(f.Path) {
			t.Errorf("path %q should be relative to the workspace file", f.Path)
		}
	}

	// Default settings block must contain the multi-repo git detection keys.
	wantKeys := []string{
		"git.autoRepositoryDetection",
		"git.repositoryScanMaxDepth",
		"git.openRepositoryInParentFolders",
	}
	for _, k := range wantKeys {
		if _, ok := parsed.Settings[k]; !ok {
			t.Errorf("settings missing key %q", k)
		}
	}
}

func TestGenerate_CodeWorkspaceCrossSourceAbsolutePath(t *testing.T) {
	cfg := newTestConfig(t)
	// Cross-source members should still share the global folder as common
	// ancestor, so paths can remain relative. Confirm we resolve to an
	// existing shared prefix and the generated file lives there.
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {
			Type: config.WorkspaceTypeCode,
			Members: []config.WorkspaceMember{
				{Source: "github-test", Repo: "team/frontend"},
				{Source: "forgejo-test", Repo: "team/infra"},
			},
		},
	}

	res, err := Generate(cfg, "feat-x")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if filepath.Dir(res.File) != cfg.Global.Folder {
		t.Errorf("file %q should live at common ancestor %q", res.File, cfg.Global.Folder)
	}
}

func TestGenerate_CodeWorkspaceExplicitFile(t *testing.T) {
	cfg := newTestConfig(t)
	explicit := filepath.Join(t.TempDir(), "custom.code-workspace")
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {
			Type: config.WorkspaceTypeCode,
			File: explicit,
			Members: []config.WorkspaceMember{
				{Source: "github-test", Repo: "team/frontend"},
			},
		},
	}
	res, err := Generate(cfg, "feat-x")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if res.File != explicit {
		t.Errorf("file = %q, want %q (explicit override)", res.File, explicit)
	}
}

func TestGenerate_TmuxinatorWindowsPerRepo(t *testing.T) {
	if !tmuxinatorSupported() {
		t.Skipf("tmuxinator not supported on %s", runtime.GOOS)
	}
	cfg := newTestConfig(t)
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {
			Type: config.WorkspaceTypeTmuxinator,
			Name: "Feature X",
			Members: []config.WorkspaceMember{
				{Source: "github-test", Repo: "team/frontend"},
				{Source: "github-test", Repo: "team/backend"},
			},
		},
	}
	res, err := Generate(cfg, "feat-x")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body := string(res.Content)
	if !strings.Contains(body, "name: Feature X") {
		t.Errorf("body missing name line; got:\n%s", body)
	}
	if !strings.Contains(body, "frontend") || !strings.Contains(body, "backend") {
		t.Errorf("body missing window names; got:\n%s", body)
	}
	if !strings.Contains(body, "root:") {
		t.Errorf("body missing root directives; got:\n%s", body)
	}
}

func TestGenerate_TmuxinatorSplitPanes(t *testing.T) {
	if !tmuxinatorSupported() {
		t.Skipf("tmuxinator not supported on %s", runtime.GOOS)
	}
	cfg := newTestConfig(t)
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {
			Type:   config.WorkspaceTypeTmuxinator,
			Layout: config.WorkspaceLayoutSplit,
			Members: []config.WorkspaceMember{
				{Source: "github-test", Repo: "team/frontend"},
				{Source: "github-test", Repo: "team/backend"},
			},
		},
	}
	res, err := Generate(cfg, "feat-x")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body := string(res.Content)
	if !strings.Contains(body, "layout: tiled") {
		t.Errorf("split-panes output missing layout: tiled; got:\n%s", body)
	}
	if !strings.Contains(body, "panes:") {
		t.Errorf("body missing panes directive")
	}
}

func TestGenerate_TmuxinatorUnsupportedPlatform(t *testing.T) {
	if tmuxinatorSupported() {
		t.Skipf("platform %s supports tmuxinator; this negative test is moot", runtime.GOOS)
	}
	cfg := newTestConfig(t)
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {
			Type: config.WorkspaceTypeTmuxinator,
			Members: []config.WorkspaceMember{
				{Source: "github-test", Repo: "team/frontend"},
			},
		},
	}
	if _, err := Generate(cfg, "feat-x"); err == nil {
		t.Error("expected error for unsupported platform")
	}
}

func TestBuildOpenCommand_CodeWorkspace(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Global.Editors = []config.EditorEntry{{Name: "VS Code", Command: "code"}}
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {
			Type: config.WorkspaceTypeCode,
			File: "/tmp/feat-x.code-workspace",
			Members: []config.WorkspaceMember{
				{Source: "github-test", Repo: "team/frontend"},
			},
		},
	}
	oc, err := BuildOpenCommand(cfg, "feat-x")
	if err != nil {
		t.Fatalf("BuildOpenCommand: %v", err)
	}
	if oc.Cmd.Path == "" && oc.Cmd.Args[0] != "code" {
		t.Errorf("unexpected cmd: %v", oc.Cmd.Args)
	}
	if len(oc.Cmd.Args) < 2 || !strings.HasSuffix(oc.Cmd.Args[1], "feat-x.code-workspace") {
		t.Errorf("cmd args = %v, want [code .../feat-x.code-workspace]", oc.Cmd.Args)
	}
}

func TestBuildOpenCommand_NoEditor(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {
			Type: config.WorkspaceTypeCode,
			File: "/tmp/feat-x.code-workspace",
			Members: []config.WorkspaceMember{
				{Source: "github-test", Repo: "team/frontend"},
			},
		},
	}
	if _, err := BuildOpenCommand(cfg, "feat-x"); err == nil {
		t.Error("expected error when no editors configured")
	}
}

func TestBuildOpenCommand_NoFileRecorded(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.Global.Editors = []config.EditorEntry{{Name: "VS Code", Command: "code"}}
	cfg.Workspaces = map[string]config.Workspace{
		"feat-x": {
			Type: config.WorkspaceTypeCode,
			Members: []config.WorkspaceMember{
				{Source: "github-test", Repo: "team/frontend"},
			},
		},
	}
	if _, err := BuildOpenCommand(cfg, "feat-x"); err == nil {
		t.Error("expected error when file is not yet generated")
	}
}

func TestCommonAncestor(t *testing.T) {
	// Build absolute paths in a platform-neutral way.
	base := filepath.Join(string(filepath.Separator)+"a", "b")
	p1 := filepath.Join(base, "c", "d")
	p2 := filepath.Join(base, "c", "e")
	p3 := filepath.Join(base, "f")

	got := commonAncestor([]string{p1, p2})
	want := filepath.Join(base, "c")
	if got != want {
		t.Errorf("commonAncestor(%q, %q) = %q, want %q", p1, p2, got, want)
	}

	got = commonAncestor([]string{p1, p2, p3})
	if got != base {
		t.Errorf("three-way ancestor = %q, want %q", got, base)
	}

	got = commonAncestor([]string{p1})
	if got != filepath.Join(base, "c") {
		t.Errorf("single-path ancestor = %q, want %q", got, filepath.Join(base, "c"))
	}
}

func TestExpandTerminalArgs(t *testing.T) {
	got := expandTerminalArgs([]string{"--title", "ws", "{command}"}, []string{"tmuxinator", "start", "feat-x"})
	want := []string{"--title", "ws", "tmuxinator", "start", "feat-x"}
	if !equalSlices(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Trailing template with no {command} should append.
	got = expandTerminalArgs([]string{"--new-tab"}, []string{"tmuxinator", "start", "feat-x"})
	want = []string{"--new-tab", "tmuxinator", "start", "feat-x"}
	if !equalSlices(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// {path} is dropped for workspace launches.
	got = expandTerminalArgs([]string{"--cwd", "{path}", "{command}"}, []string{"tmuxinator", "start", "feat-x"})
	want = []string{"--cwd", "tmuxinator", "start", "feat-x"}
	if !equalSlices(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
