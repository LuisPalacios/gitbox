package main

// Wails bindings for non-standard clone locations: extra scan-root folders,
// nested-scan depth, the multi-repo container flag, on-demand folder scanning,
// and adopting a discovered repo into a custom destination folder.

import (
	"fmt"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/workspace"
)

// ─── Extra scan folders + nested depth ─────────────────────────────────────

// ListExtraFolders returns the configured extra scan-root folders.
func (a *App) ListExtraFolders() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.cfg.Global.ExtraFolders...)
}

// AddExtraFolder appends a scan-root folder (deduped) and persists.
func (a *App) AddExtraFolder(path string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.Global.AddExtraFolder(path) {
		return a.saveConfig()
	}
	return nil
}

// RemoveExtraFolder drops a scan-root folder and persists.
func (a *App) RemoveExtraFolder(path string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.Global.RemoveExtraFolder(path) {
		return a.saveConfig()
	}
	return nil
}

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

// TentativeContainerDTO names a managed repo that has a .code-workspace at its
// root but is not yet flagged a container — a suggestion for the user.
type TentativeContainerDTO struct {
	Source string `json:"source"`
	Repo   string `json:"repo"`
}

// TentativeContainers returns repos that look like multi-repo parents (they
// hold a .code-workspace) but are not yet flagged Container.
func (a *App) TentativeContainers() []TentativeContainerDTO {
	a.mu.Lock()
	defer a.mu.Unlock()

	refs := workspace.TentativeContainers(a.cfg)
	out := make([]TentativeContainerDTO, 0, len(refs))
	for _, r := range refs {
		out = append(out, TentativeContainerDTO{Source: r.Source, Repo: r.Repo})
	}
	return out
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
		dtos[i] = OrphanRepoDTO{
			Path:                o.Path,
			RelPath:             o.RelPath,
			RemoteURL:           o.RemoteURL,
			RepoKey:             o.RepoKey,
			MatchedAccount:      o.MatchedAccount,
			MatchedSource:       o.MatchedSource,
			ExpectedPath:        o.ExpectedPath,
			NeedsRelocate:       o.NeedsRelocate,
			LocalOnly:           o.LocalOnly,
			AmbiguousCandidates: o.AmbiguousCandidates,
		}
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
