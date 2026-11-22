package main

// Wails bindings for non-standard clone locations: nested-scan depth, the multi-repo container flag, on-demand folder scanning,
// and adopting a discovered repo into a custom destination folder.

import (
	"fmt"
	"slices"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/reconcile"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/LuisPalacios/gitbox/pkg/workspace"
)

// ─── Nested depth ──────────────────────────────────────────────────────────

// GetNestedScanDepth returns the effective nested-scan depth (default applied).
func (a *App) GetNestedScanDepth() int { return a.be().GetNestedScanDepth() }

// getNestedScanDepthLocal is the local backend's GetNestedScanDepth.
func (a *App) getNestedScanDepthLocal() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Global.NestedScanDepthOrDefault()
}

// SetNestedScanDepth sets the nested-scan depth (>=1) and persists.
func (a *App) SetNestedScanDepth(depth int) error { return a.be().SetNestedScanDepth(depth) }

// setNestedScanDepthLocal is the local backend's SetNestedScanDepth.
func (a *App) setNestedScanDepthLocal(depth int) error {
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
	return a.be().SetRepoContainer(sourceKey, repoKey, container)
}

// setRepoContainerLocal is the local backend's SetRepoContainer.
func (a *App) setRepoContainerLocal(sourceKey, repoKey string, container bool) error {
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
func (a *App) RepoNesting() map[string]string { return a.be().RepoNesting() }

// repoNestingLocal is the local backend's RepoNesting.
func (a *App) repoNestingLocal() map[string]string {
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
// it opens with, remembered as my preferred one, and persists. The file must
// be at the clone root.
func (a *App) SetRepoWorkspace(sourceKey, repoKey, file string) error {
	return a.be().SetRepoWorkspace(sourceKey, repoKey, file)
}

// setRepoWorkspaceLocal is the local backend's SetRepoWorkspace.
func (a *App) setRepoWorkspaceLocal(sourceKey, repoKey, file string) error {
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
	if file != "" {
		files, err := workspace.RootFiles(status.RepoPath(a.cfg, sourceKey, repoKey, repo))
		if err != nil {
			return fmt.Errorf("cannot read the workspace directory for %s", repoKey)
		}
		if !slices.Contains(files, file) {
			return fmt.Errorf("%s is not a workspace file at the root of %s", file, repoKey)
		}
		repo.Container = true
	}
	repo.Workspace, repo.WorkspacePreferred = file, file
	src.Repos[repoKey] = repo
	return a.saveConfig()
}

// RepoWorkspaceFiles lists the .code-workspace files at a clone's root.
func (a *App) RepoWorkspaceFiles(sourceKey, repoKey string) []string {
	return a.be().RepoWorkspaceFiles(sourceKey, repoKey)
}

// repoWorkspaceFilesLocal is the local backend's RepoWorkspaceFiles.
func (a *App) repoWorkspaceFilesLocal(sourceKey, repoKey string) []string {
	a.mu.Lock()
	repo, ok := a.cfg.Sources[sourceKey].Repos[repoKey]
	dir := status.RepoPath(a.cfg, sourceKey, repoKey, repo)
	a.mu.Unlock()
	if !ok {
		return []string{}
	}
	if files, err := workspace.RootFiles(dir); err == nil && files != nil {
		return files
	}
	return []string{}
}

// ─── On-demand folder scan + custom-destination adopt ──────────────────────

// ScanFolderForClones runs the orphan scan over an additional caller-picked
// folder (plus the standard + extra roots and container nested clones) and
// returns adoptable orphans for the frontend to confirm.
func (a *App) ScanFolderForClones(path string) []OrphanRepoDTO {
	return a.be().ScanFolderForClones(path)
}

// scanFolderForClonesLocal is the local backend's ScanFolderForClones.
func (a *App) scanFolderForClonesLocal(path string) []OrphanRepoDTO {
	a.mu.Lock()
	cfg, err := a.cfg.Clone()
	a.mu.Unlock()
	if err != nil {
		return nil
	}

	if path != "" {
		rows, found, err := reconcile.WorkspaceCandidates(cfg, workspace.Inspect(cfg), path)
		if err != nil {
			return nil
		}
		if found {
			out := make([]OrphanRepoDTO, 0, len(rows))
			for _, row := range rows {
				dto := toOrphanDTO(row.OrphanRepo, row.Reason)
				dto.WorkspaceRoot = path
				out = append(out, dto)
			}
			return out
		}
	}

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
	return a.be().AddDiscoveredReposToFolder(key, repoNames, cloneFolder)
}

// addDiscoveredReposToFolderLocal is the local backend's AddDiscoveredReposToFolder.
func (a *App) addDiscoveredReposToFolderLocal(key string, repoNames []string, cloneFolder string) error {
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
