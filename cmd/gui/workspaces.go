package main

// Wails bindings for dynamic workspaces. Wraps pkg/config and pkg/workspace
// with per-mutation locking, persistence via config.Save, and the
// git.HideWindow rule on any exec.Command it spawns.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/workspace"
)

// WorkspaceDTO exposes a workspace to the frontend. It mirrors
// config.Workspace but uses camelCase JSON keys consistent with the rest
// of the GUI payloads.
type WorkspaceDTO struct {
	Type       string                `json:"type"`
	Name       string                `json:"name,omitempty"`
	File       string                `json:"file,omitempty"`
	Layout     string                `json:"layout,omitempty"`
	Members    []WorkspaceMemberDTO  `json:"members"`
	Discovered bool                  `json:"discovered,omitempty"`
}

// WorkspaceMemberDTO is the frontend-facing view of a workspace member.
type WorkspaceMemberDTO struct {
	Source string `json:"source"`
	Repo   string `json:"repo"`
}

// WorkspaceCreateRequest is the payload for CreateWorkspace.
// File, Layout, and Members are optional at creation time.
type WorkspaceCreateRequest struct {
	Key     string                `json:"key"`
	Type    string                `json:"type"`
	Name    string                `json:"name,omitempty"`
	File    string                `json:"file,omitempty"`
	Layout  string                `json:"layout,omitempty"`
	Members []WorkspaceMemberDTO  `json:"members,omitempty"`
}

// WorkspaceUpdateRequest is the payload for UpdateWorkspace. Members
// replaces the entire member list; omit the field to leave it untouched
// (nil vs. empty slice distinguishes "not provided" from "clear members").
type WorkspaceUpdateRequest struct {
	Name    string                `json:"name"`
	Layout  string                `json:"layout"`
	Members []WorkspaceMemberDTO  `json:"members"`
}

// WorkspaceGenerateResult describes what GenerateWorkspace wrote.
type WorkspaceGenerateResult struct {
	File string `json:"file"`
	Size int    `json:"size"`
}

// WorkspaceListResult wraps the workspace map plus its insertion-ordered
// key list so the frontend can render deterministically.
type WorkspaceListResult struct {
	Workspaces map[string]WorkspaceDTO `json:"workspaces"`
	Order      []string                `json:"order"`
}

// ─── DTO helpers ─────────────────────────────────────────────────────────

func buildWorkspacesDTO(cfg *config.Config) map[string]WorkspaceDTO {
	out := make(map[string]WorkspaceDTO, len(cfg.Workspaces))
	for key, w := range cfg.Workspaces {
		out[key] = toWorkspaceDTO(w)
	}
	return out
}

func toWorkspaceDTO(w config.Workspace) WorkspaceDTO {
	members := make([]WorkspaceMemberDTO, 0, len(w.Members))
	for _, m := range w.Members {
		members = append(members, WorkspaceMemberDTO{Source: m.Source, Repo: m.Repo})
	}
	return WorkspaceDTO{
		Type:       w.Type,
		Name:       w.Name,
		File:       w.File,
		Layout:     w.Layout,
		Members:    members,
		Discovered: w.Discovered,
	}
}

func fromMemberDTO(m WorkspaceMemberDTO) config.WorkspaceMember {
	return config.WorkspaceMember{Source: m.Source, Repo: m.Repo}
}

func fromMemberDTOs(ms []WorkspaceMemberDTO) []config.WorkspaceMember {
	out := make([]config.WorkspaceMember, 0, len(ms))
	for _, m := range ms {
		out = append(out, fromMemberDTO(m))
	}
	return out
}

// workspacesOrder returns the deterministic workspace key order.
func workspacesOrder(cfg *config.Config) []string {
	return cfg.OrderedWorkspaceKeys()
}

// ─── Bindings ────────────────────────────────────────────────────────────

// ListWorkspaces returns all configured workspaces plus the deterministic
// key order so the frontend can render them in insertion order.
func (a *App) ListWorkspaces() WorkspaceListResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	return WorkspaceListResult{
		Workspaces: buildWorkspacesDTO(a.cfg),
		Order:      workspacesOrder(a.cfg),
	}
}

// GetWorkspace returns a single workspace by key.
func (a *App) GetWorkspace(key string) (WorkspaceDTO, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	w, ok := a.cfg.Workspaces[key]
	if !ok {
		return WorkspaceDTO{}, fmt.Errorf("workspace %q not found", key)
	}
	return toWorkspaceDTO(w), nil
}

// CreateWorkspace persists a new workspace entry. Does NOT write the
// generated artifact — call GenerateWorkspace for that.
func (a *App) CreateWorkspace(req WorkspaceCreateRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	ws := config.Workspace{
		Type:    req.Type,
		Name:    req.Name,
		File:    req.File,
		Layout:  req.Layout,
		Members: fromMemberDTOs(req.Members),
	}
	if err := a.cfg.AddWorkspace(req.Key, ws); err != nil {
		return err
	}
	return config.Save(a.cfg, a.cfgPath)
}

// UpdateWorkspace replaces the editable fields (name, layout, members)
// of an existing workspace. Type and File are immutable via this path —
// delete and re-create to change the type.
func (a *App) UpdateWorkspace(key string, req WorkspaceUpdateRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	existing, ok := a.cfg.Workspaces[key]
	if !ok {
		return fmt.Errorf("workspace %q not found", key)
	}
	existing.Name = req.Name
	existing.Layout = req.Layout
	if req.Members != nil {
		existing.Members = fromMemberDTOs(req.Members)
	}
	if err := a.cfg.UpdateWorkspace(key, existing); err != nil {
		return err
	}
	return config.Save(a.cfg, a.cfgPath)
}

// DeleteWorkspace removes the workspace from the config. The generated
// file on disk is left alone so the user decides whether to delete it.
func (a *App) DeleteWorkspace(key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.cfg.DeleteWorkspace(key); err != nil {
		return err
	}
	return config.Save(a.cfg, a.cfgPath)
}

// AddWorkspaceMember appends a member and persists the config.
func (a *App) AddWorkspaceMember(key string, member WorkspaceMemberDTO) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.cfg.AddWorkspaceMember(key, fromMemberDTO(member)); err != nil {
		return err
	}
	return config.Save(a.cfg, a.cfgPath)
}

// RemoveWorkspaceMember drops a member identified by source+repo.
func (a *App) RemoveWorkspaceMember(key, source, repo string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.cfg.DeleteWorkspaceMember(key, source, repo); err != nil {
		return err
	}
	return config.Save(a.cfg, a.cfgPath)
}

// GenerateWorkspace writes the .code-workspace or tmuxinator YAML to
// disk using pkg/workspace. It persists the resolved file path back to
// the workspace entry so later Open calls know where it lives.
func (a *App) GenerateWorkspace(key string) (WorkspaceGenerateResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	res, err := workspace.Generate(a.cfg, key)
	if err != nil {
		return WorkspaceGenerateResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(res.File), 0o755); err != nil {
		return WorkspaceGenerateResult{}, fmt.Errorf("creating parent dir: %w", err)
	}
	if err := os.WriteFile(res.File, res.Content, 0o644); err != nil {
		return WorkspaceGenerateResult{}, fmt.Errorf("writing %s: %w", res.File, err)
	}

	ws := a.cfg.Workspaces[key]
	if ws.File != res.File {
		ws.File = res.File
		if err := a.cfg.UpdateWorkspace(key, ws); err != nil {
			return WorkspaceGenerateResult{}, err
		}
		if err := config.Save(a.cfg, a.cfgPath); err != nil {
			return WorkspaceGenerateResult{}, err
		}
	}
	return WorkspaceGenerateResult{File: res.File, Size: len(res.Content)}, nil
}

// OpenWorkspace generates (so the artifact is current) then launches the
// workspace via the first configured editor (codeWorkspace) or terminal +
// tmuxinator (tmuxinator). Applies git.HideWindow to the spawned command
// on Windows to avoid a console flash.
func (a *App) OpenWorkspace(key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Always regenerate first so the artifact on disk is current.
	res, err := workspace.Generate(a.cfg, key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(res.File), 0o755); err != nil {
		return fmt.Errorf("creating parent dir: %w", err)
	}
	if err := os.WriteFile(res.File, res.Content, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", res.File, err)
	}
	ws := a.cfg.Workspaces[key]
	if ws.File != res.File {
		ws.File = res.File
		if err := a.cfg.UpdateWorkspace(key, ws); err != nil {
			return err
		}
		if err := config.Save(a.cfg, a.cfgPath); err != nil {
			return err
		}
	}

	oc, err := workspace.BuildOpenCommand(a.cfg, key)
	if err != nil {
		return err
	}
	git.HideWindow(oc.Cmd)
	if err := oc.Cmd.Start(); err != nil {
		return fmt.Errorf("launch %s: %w", oc.Description, err)
	}
	// Detach — the launcher owns its own lifecycle.
	return nil
}
