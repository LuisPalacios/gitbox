// Package workspace generates and manages multi-repo workspace artifacts
// (VS Code .code-workspace files, tmuxinator YAML profiles) bundled from
// gitbox-managed clones.
//
// The package is pure: Generate returns the file path and the bytes that
// should be written; callers decide whether and how to persist them.
// Open builds an exec.Command to launch an already-generated workspace
// via the user's configured editor or terminal; callers apply any
// Windows-specific process attributes (e.g. git.HideWindow) and run it.
package workspace

import (
	"fmt"
	"path/filepath"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// GenerateResult captures what Generate produced.
type GenerateResult struct {
	// File is the absolute path where the generated artifact should live.
	// For codeWorkspace this is chosen from the common ancestor of members
	// (or overridden by Workspace.File if already set).
	// For tmuxinator this is always ~/.tmuxinator/<key>.yml.
	File string

	// Content is the serialized bytes (JSON or YAML) to write to File.
	Content []byte

	// ResolvedPaths is the absolute on-disk path of each member, in member order.
	// Handy for UI display.
	ResolvedPaths []string
}

// Generate builds the workspace artifact for the given key.
// It does not write anything to disk.
func Generate(cfg *config.Config, key string) (GenerateResult, error) {
	w, ok := cfg.Workspaces[key]
	if !ok {
		return GenerateResult{}, fmt.Errorf("workspace %q not found", key)
	}
	if len(w.Members) == 0 {
		return GenerateResult{}, fmt.Errorf("workspace %q has no members", key)
	}

	paths, err := resolveMemberPaths(cfg, w.Members)
	if err != nil {
		return GenerateResult{}, fmt.Errorf("workspace %q: %w", key, err)
	}

	switch w.Type {
	case config.WorkspaceTypeCode:
		return generateCodeWorkspace(key, w, paths)
	case config.WorkspaceTypeTmuxinator:
		return generateTmuxinator(key, w, paths)
	default:
		return GenerateResult{}, fmt.Errorf("workspace %q: unsupported type %q", key, w.Type)
	}
}

// DefaultFilePath returns the file path Generate would pick for a workspace
// when Workspace.File is unset. Useful for previews and CLI dry-runs.
func DefaultFilePath(cfg *config.Config, key string) (string, error) {
	w, ok := cfg.Workspaces[key]
	if !ok {
		return "", fmt.Errorf("workspace %q not found", key)
	}
	paths, err := resolveMemberPaths(cfg, w.Members)
	if err != nil {
		return "", fmt.Errorf("workspace %q: %w", key, err)
	}
	switch w.Type {
	case config.WorkspaceTypeCode:
		return defaultCodeWorkspaceFile(key, paths), nil
	case config.WorkspaceTypeTmuxinator:
		return defaultTmuxinatorFile(key)
	default:
		return "", fmt.Errorf("workspace %q: unsupported type %q", key, w.Type)
	}
}

// resolveMemberPaths maps each WorkspaceMember to its absolute on-disk folder.
func resolveMemberPaths(cfg *config.Config, members []config.WorkspaceMember) ([]string, error) {
	globalFolder := config.ExpandTilde(cfg.Global.Folder)
	out := make([]string, 0, len(members))
	for _, m := range members {
		src, ok := cfg.Sources[m.Source]
		if !ok {
			return nil, fmt.Errorf("unknown source %q", m.Source)
		}
		repo, ok := src.Repos[m.Repo]
		if !ok {
			return nil, fmt.Errorf("unknown repo %q in source %q", m.Repo, m.Source)
		}
		path := status.ResolveRepoPath(globalFolder, src.EffectiveFolder(m.Source), m.Repo, repo)
		out = append(out, filepath.Clean(path))
	}
	return out, nil
}
