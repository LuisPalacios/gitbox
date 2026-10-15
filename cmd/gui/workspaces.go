package main

// Wails bindings for discovered VS Code workspaces. Workspaces are read-only:
// gitbox discovers *.code-workspace files, caches them, lists them, and opens
// them. It never creates, edits, generates, or deletes them. The git.HideWindow
// rule applies to any exec.Command spawned here.

import (
	"fmt"
	"reflect"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/workspace"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// WorkspaceDTO exposes a discovered workspace to the frontend.
type WorkspaceDTO struct {
	Name       string               `json:"name,omitempty"`
	File       string               `json:"file,omitempty"`
	Members    []WorkspaceMemberDTO `json:"members"`
	Discovered bool                 `json:"discovered,omitempty"`
}

// WorkspaceMemberDTO is the frontend-facing view of a workspace member.
type WorkspaceMemberDTO struct {
	Source string `json:"source"`
	Repo   string `json:"repo"`
}

// WorkspaceListResult wraps the workspace map plus its insertion-ordered key
// list so the frontend can render deterministically.
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
		Name:       w.Name,
		File:       w.File,
		Members:    members,
		Discovered: w.Discovered,
	}
}

// ─── Bindings ────────────────────────────────────────────────────────────

// ListWorkspaces returns all discovered workspaces plus their deterministic
// key order.
func (a *App) ListWorkspaces() WorkspaceListResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	return WorkspaceListResult{
		Workspaces: buildWorkspacesDTO(a.cfg),
		Order:      a.cfg.OrderedWorkspaceKeys(),
	}
}

// GetWorkspace returns a single discovered workspace by key.
func (a *App) GetWorkspace(key string) (WorkspaceDTO, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	w, ok := a.cfg.Workspaces[key]
	if !ok {
		return WorkspaceDTO{}, fmt.Errorf("workspace %q not found", key)
	}
	return toWorkspaceDTO(w), nil
}

// ─── Discovery ──────────────────────────────────────────────────────────

// DiscoverWorkspacesResult reports a read-only discovery refresh. Changed is
// true when the cache moved; Count is the number of discovered workspaces.
type DiscoverWorkspacesResult struct {
	Changed bool `json:"changed"`
	Count   int  `json:"count"`
}

// DiscoverWorkspaces rescans for *.code-workspace files, refreshes the
// read-only cache, persists only when it changed, and emits
// `workspaces:discovered` so the frontend reloads. Safe to call repeatedly;
// idempotent when nothing on disk changed. Intended to run in the DomReady
// background goroutine so the cached list shows instantly and refreshes after.
func (a *App) DiscoverWorkspaces() (DiscoverWorkspacesResult, error) {
	return a.discoverWorkspaces(workspace.Discover)
}

// discoverWorkspaces serializes scans without holding the shared config lock.
// A changed path or clone set invalidates the result and triggers a fresh scan.
func (a *App) discoverWorkspaces(scan func(*config.Config) ([]workspace.Found, error)) (DiscoverWorkspacesResult, error) {
	a.workspaceDiscoveryMu.Lock()
	defer a.workspaceDiscoveryMu.Unlock()
	for {
		a.mu.Lock()
		snapshot := workspace.DiscoverySnapshot(a.cfg)
		a.mu.Unlock()
		found, err := scan(snapshot)
		if err != nil {
			return DiscoverWorkspacesResult{}, err
		}
		a.mu.Lock()
		if !reflect.DeepEqual(snapshot, workspace.DiscoverySnapshot(a.cfg)) {
			a.mu.Unlock()
			continue
		}
		out := DiscoverWorkspacesResult{Changed: workspace.UpdateCache(a.cfg, found), Count: len(found)}
		if out.Changed {
			if err := a.saveConfig(); err != nil {
				a.mu.Unlock()
				return out, fmt.Errorf("saving workspace cache: %w", err)
			}
		}
		a.mu.Unlock()
		if out.Changed && a.ctx != nil {
			wailsrt.EventsEmit(a.ctx, "workspaces:discovered", out)
		}
		return out, nil
	}
}

// OpenWorkspace launches a discovered .code-workspace file in the first
// configured editor. Applies git.HideWindow to the spawned command on Windows
// to avoid a console flash.
func (a *App) OpenWorkspace(key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

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
