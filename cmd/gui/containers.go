package main

// Wails bindings for non-standard clone locations: nested-scan depth, the multi-repo container flag, on-demand folder scanning,
// and adopting a discovered repo into a custom destination folder.

import (
	"fmt"
	"slices"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/LuisPalacios/gitbox/pkg/workspace"
)

// ─── Nested depth ──────────────────────────────────────────────────────────

// GetNestedScanDepth returns the effective nested-scan depth (default applied).
func (a *App) GetNestedScanDepth() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Global.NestedScanDepthOrDefault()
}

// SetNestedScanDepth sets the nested-scan depth (>=1) and persists.
func (a *App) SetNestedScanDepth(depth int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if depth < 1 {
		return fmt.Errorf("nested scan depth must be >= 1")
	}
	a.cfg.Global.NestedScanDepth = depth
	return a.saveConfig()
}

// ─── Container flag ────────────────────────────────────────────────────────

// SetRepoContainer flags (or clears) a managed repo as a multi-repo container,
// then persists. When set, 'gitbox adopt' / ScanFolderForClones descend into its
// working tree to discover nested clones.
func (a *App) SetRepoContainer(sourceKey, repoKey string, container bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	src, ok := a.cfg.Sources[sourceKey]
	if !ok {
		return fmt.Errorf("unknown source %q", sourceKey)
	}
	repo, ok := src.Repos[repoKey]
	if !ok {
		return fmt.Errorf("unknown repo %q in source %q", repoKey, sourceKey)
	}
	repo.Container = container
	src.Repos[repoKey] = repo
	a.cfg.Sources[sourceKey] = src
	return a.saveConfig()
}

// RepoNesting returns child→parent links (keys are "source/repo") derived from
// resolved clone paths, so the GUI can render nested clones indented under their
// container parent. A repo appears as a key only when it lives inside a
// container repo's working tree.
func (a *App) RepoNesting() map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()

	n := status.ComputeNesting(a.cfg)
	out := make(map[string]string, len(n.ParentOf))
	for child, parent := range n.ParentOf {
		out[child.Source+"/"+child.Repo] = parent.Source + "/" + parent.Repo
	}
	return out
}

// SetRepoWorkspace records which .code-workspace file at a container's root
// it opens with, and persists. The file must be at the clone root.
func (a *App) SetRepoWorkspace(sourceKey, repoKey, file string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	src, ok := a.cfg.Sources[sourceKey]
	if !ok {
		return fmt.Errorf("unknown source %q", sourceKey)
	}
	repo, ok := src.Repos[repoKey]
	if !ok {
		return fmt.Errorf("unknown repo %q in source %q", repoKey, sourceKey)
	}
	if !slices.Contains(workspace.RootFiles(status.RepoPath(a.cfg, sourceKey, repoKey, repo)), file) {
		return fmt.Errorf("%s is not a workspace file at the root of %s", file, repoKey)
	}
	repo.Container, repo.Workspace = true, file
	src.Repos[repoKey] = repo
	return a.saveConfig()
}

// RepoWorkspaceFiles lists the .code-workspace files at a clone's root.
func (a *App) RepoWorkspaceFiles(sourceKey, repoKey string) []string {
	a.mu.Lock()
	repo, ok := a.cfg.Sources[sourceKey].Repos[repoKey]
	dir := status.RepoPath(a.cfg, sourceKey, repoKey, repo)
	a.mu.Unlock()
	if !ok {
		return []string{}
	}
	if files := workspace.RootFiles(dir); files != nil {
		return files
	}
	return []string{}
}

// ─── On-demand folder scan + custom-destination adopt ──────────────────────

// ScanFolderForClones runs the orphan scan over an additional caller-picked
// folder (plus the standard + extra roots and container nested clones) and
// returns adoptable orphans for the frontend to confirm.
func (a *App) ScanFolderForClones(path string) []OrphanRepoDTO {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	var extra []string
	if path != "" {
		extra = append(extra, config.ExpandTilde(path))
	}
	orphans, err := adopt.FindOrphansIn(cfg, extra)
	if err != nil {
		return nil
	}
	dtos := make([]OrphanRepoDTO, len(orphans))
	for i, o := range orphans {
		dtos[i] = toOrphanDTO(o, "")
	}
	return dtos
}

// AddDiscoveredReposToFolder adds discovered repos into a custom destination
// folder, stored as an absolute clone_folder so the clone lands there. When
// cloneFolder is empty it behaves like AddDiscoveredRepos (standard layout).
func (a *App) AddDiscoveredReposToFolder(key string, repoNames []string, cloneFolder string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	sourceKey := key
	if _, ok := a.cfg.Sources[sourceKey]; !ok {
		found := false
		for sk, src := range a.cfg.Sources {
			if src.Account == key {
				sourceKey = sk
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("no source found for key %q", key)
		}
	}

	for _, name := range repoNames {
		repo := config.Repo{}
		if cloneFolder != "" {
			// A single custom folder hosts one repo; store the absolute path.
			repo.CloneFolder = cloneFolder
		}
		if err := a.cfg.AddRepo(sourceKey, name, repo); err != nil {
			return err
		}
	}
	return a.saveConfig()
}
