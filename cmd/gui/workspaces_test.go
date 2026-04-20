package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// newWorkspaceTestApp builds an App with a populated config and a real
// on-disk cfgPath so mutating bindings can round-trip through config.Save.
func newWorkspaceTestApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gitbox.json")
	cfg := &config.Config{
		Version: 2,
		Global:  config.GlobalConfig{Folder: dir},
		Accounts: map[string]config.Account{
			"github-alice": {Provider: "github", URL: "https://github.com",
				Username: "alice", Name: "Alice", Email: "a@e"},
		},
		Sources: map[string]config.Source{
			"github-alice": {
				Account: "github-alice",
				Repos: map[string]config.Repo{
					"team/frontend": {},
					"team/backend":  {},
				},
			},
		},
	}
	// Write the initial config so Save's backup flow is a no-op on first save.
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatalf("seeding config: %v", err)
	}
	return &App{cfg: cfg, cfgPath: cfgPath, mu: sync.Mutex{}}, cfgPath
}

func TestCreateWorkspace_Persists(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)

	err := a.CreateWorkspace(WorkspaceCreateRequest{
		Key:  "feat-x",
		Type: config.WorkspaceTypeCode,
		Name: "Feature X",
		Members: []WorkspaceMemberDTO{
			{Source: "github-alice", Repo: "team/frontend"},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	// Round-trip through disk — the binding must persist.
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	ws, ok := reloaded.Workspaces["feat-x"]
	if !ok {
		t.Fatal("workspace not persisted to disk")
	}
	if ws.Name != "Feature X" || len(ws.Members) != 1 {
		t.Errorf("workspace round-trip mismatch: %+v", ws)
	}
}

func TestCreateWorkspace_InvalidType(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)

	err := a.CreateWorkspace(WorkspaceCreateRequest{
		Key:  "feat-x",
		Type: "bogus",
	})
	if err == nil || !strings.Contains(err.Error(), "type") {
		t.Errorf("expected validation error on type, got %v", err)
	}
}

func TestCreateWorkspace_UnknownMember(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)

	err := a.CreateWorkspace(WorkspaceCreateRequest{
		Key:  "feat-x",
		Type: config.WorkspaceTypeCode,
		Members: []WorkspaceMemberDTO{
			{Source: "nope", Repo: "whatever"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown source") {
		t.Errorf("expected unknown-source error, got %v", err)
	}
}

func TestListWorkspaces_ReturnsOrder(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)
	a.CreateWorkspace(WorkspaceCreateRequest{Key: "bravo", Type: config.WorkspaceTypeCode})
	a.CreateWorkspace(WorkspaceCreateRequest{Key: "alpha", Type: config.WorkspaceTypeCode})
	// Simulate having been reloaded from disk with an explicit order.
	a.cfg.WorkspaceOrder = []string{"bravo", "alpha"}

	out := a.ListWorkspaces()
	if len(out.Workspaces) != 2 {
		t.Errorf("workspaces = %d, want 2", len(out.Workspaces))
	}
	if len(out.Order) != 2 || out.Order[0] != "bravo" || out.Order[1] != "alpha" {
		t.Errorf("order = %v, want [bravo alpha]", out.Order)
	}
}

func TestGetWorkspace_NotFound(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)
	_, err := a.GetWorkspace("nope")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got %v", err)
	}
}

func TestUpdateWorkspace_ReplacesEditableFields(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)
	a.CreateWorkspace(WorkspaceCreateRequest{
		Key:  "feat-x",
		Type: config.WorkspaceTypeCode,
		Name: "Old Name",
		Members: []WorkspaceMemberDTO{
			{Source: "github-alice", Repo: "team/frontend"},
		},
	})

	err := a.UpdateWorkspace("feat-x", WorkspaceUpdateRequest{
		Name: "New Name",
		Members: []WorkspaceMemberDTO{
			{Source: "github-alice", Repo: "team/frontend"},
			{Source: "github-alice", Repo: "team/backend"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateWorkspace: %v", err)
	}

	got, _ := a.GetWorkspace("feat-x")
	if got.Name != "New Name" {
		t.Errorf("name = %q, want New Name", got.Name)
	}
	if len(got.Members) != 2 {
		t.Errorf("members = %d, want 2", len(got.Members))
	}
}

func TestUpdateWorkspace_NilMembersLeavesListUntouched(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)
	a.CreateWorkspace(WorkspaceCreateRequest{
		Key:  "feat-x",
		Type: config.WorkspaceTypeCode,
		Members: []WorkspaceMemberDTO{
			{Source: "github-alice", Repo: "team/frontend"},
		},
	})

	err := a.UpdateWorkspace("feat-x", WorkspaceUpdateRequest{
		Name: "Renamed",
		// Members: nil — should preserve existing members.
	})
	if err != nil {
		t.Fatalf("UpdateWorkspace: %v", err)
	}
	got, _ := a.GetWorkspace("feat-x")
	if len(got.Members) != 1 {
		t.Errorf("nil Members dropped list: got %d, want 1", len(got.Members))
	}
}

func TestDeleteWorkspace(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)
	a.CreateWorkspace(WorkspaceCreateRequest{
		Key:  "feat-x",
		Type: config.WorkspaceTypeCode,
	})
	if err := a.DeleteWorkspace("feat-x"); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	reloaded, _ := config.Load(cfgPath)
	if _, ok := reloaded.Workspaces["feat-x"]; ok {
		t.Error("workspace not removed from disk")
	}
}

func TestAddAndRemoveWorkspaceMember(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)
	a.CreateWorkspace(WorkspaceCreateRequest{
		Key:  "feat-x",
		Type: config.WorkspaceTypeCode,
		Members: []WorkspaceMemberDTO{
			{Source: "github-alice", Repo: "team/frontend"},
		},
	})

	if err := a.AddWorkspaceMember("feat-x", WorkspaceMemberDTO{
		Source: "github-alice", Repo: "team/backend",
	}); err != nil {
		t.Fatalf("AddWorkspaceMember: %v", err)
	}
	got, _ := a.GetWorkspace("feat-x")
	if len(got.Members) != 2 {
		t.Fatalf("after add, members = %d, want 2", len(got.Members))
	}

	if err := a.RemoveWorkspaceMember("feat-x", "github-alice", "team/frontend"); err != nil {
		t.Fatalf("RemoveWorkspaceMember: %v", err)
	}
	got, _ = a.GetWorkspace("feat-x")
	if len(got.Members) != 1 || got.Members[0].Repo != "team/backend" {
		t.Errorf("after remove, members = %+v", got.Members)
	}
}

func TestGenerateWorkspace_WritesFileAndPersistsPath(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)
	a.CreateWorkspace(WorkspaceCreateRequest{
		Key:  "feat-x",
		Type: config.WorkspaceTypeCode,
		Members: []WorkspaceMemberDTO{
			{Source: "github-alice", Repo: "team/frontend"},
		},
	})

	res, err := a.GenerateWorkspace("feat-x")
	if err != nil {
		t.Fatalf("GenerateWorkspace: %v", err)
	}
	if res.File == "" || res.Size == 0 {
		t.Errorf("result = %+v", res)
	}
	if _, err := os.Stat(res.File); err != nil {
		t.Errorf("generated file missing on disk: %v", err)
	}
	// The chosen path must be persisted so Open finds it next time.
	reloaded, _ := config.Load(cfgPath)
	if reloaded.Workspaces["feat-x"].File != res.File {
		t.Errorf("file path not persisted to config")
	}
}

func TestDiscoverWorkspaces_AdoptsAndPersists(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)

	// Stage a clone path that resolves to (github-alice, team/frontend) under
	// the test's global folder, then drop a *.code-workspace next to it.
	frontend := filepath.Join(a.cfg.Global.Folder, "github-alice", "team", "frontend")
	if err := os.MkdirAll(frontend, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	wsFile := filepath.Join(a.cfg.Global.Folder, "feat-discover.code-workspace")
	if err := os.WriteFile(wsFile, []byte(`{"folders":[{"path":"`+filepath.ToSlash(frontend)+`"}]}`), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	res, err := a.DiscoverWorkspaces()
	if err != nil {
		t.Fatalf("DiscoverWorkspaces: %v", err)
	}
	if res.NewCount != 1 {
		t.Errorf("NewCount = %d, want 1", res.NewCount)
	}
	if len(res.Adopted) != 1 || res.Adopted[0] != "feat-discover" {
		t.Errorf("Adopted = %v, want [feat-discover]", res.Adopted)
	}

	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	ws, ok := reloaded.Workspaces["feat-discover"]
	if !ok {
		t.Fatal("workspace not persisted")
	}
	if !ws.Discovered {
		t.Error("Discovered should be true on adopted workspace")
	}
	if len(ws.Members) != 1 {
		t.Errorf("members = %d, want 1", len(ws.Members))
	}
}

func TestDiscoverWorkspaces_Idempotent(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)

	frontend := filepath.Join(a.cfg.Global.Folder, "github-alice", "team", "frontend")
	if err := os.MkdirAll(frontend, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	wsFile := filepath.Join(a.cfg.Global.Folder, "feat-once.code-workspace")
	if err := os.WriteFile(wsFile, []byte(`{"folders":[{"path":"`+filepath.ToSlash(frontend)+`"}]}`), 0o644); err != nil {
		t.Fatalf("write workspace file: %v", err)
	}

	if _, err := a.DiscoverWorkspaces(); err != nil {
		t.Fatalf("first DiscoverWorkspaces: %v", err)
	}
	res, err := a.DiscoverWorkspaces()
	if err != nil {
		t.Fatalf("second DiscoverWorkspaces: %v", err)
	}
	if len(res.Adopted) != 0 {
		t.Errorf("second pass adopted %v, want []", res.Adopted)
	}
	if res.NewCount != 0 {
		t.Errorf("second pass NewCount = %d, want 0", res.NewCount)
	}
}

func TestOpenWorkspace_NoEditorConfigured(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)
	a.CreateWorkspace(WorkspaceCreateRequest{
		Key:  "feat-x",
		Type: config.WorkspaceTypeCode,
		Members: []WorkspaceMemberDTO{
			{Source: "github-alice", Repo: "team/frontend"},
		},
	})

	err := a.OpenWorkspace("feat-x")
	if err == nil || !strings.Contains(err.Error(), "editor") {
		t.Errorf("expected editor error, got %v", err)
	}
}
