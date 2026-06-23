// Package workspace discovers VS Code .code-workspace files under the
// gitbox-managed folders and exposes them read-only. gitbox never creates,
// edits, generates, or deletes workspace files — users own them. Discover
// returns what is on disk; RefreshCache mirrors that into cfg.Workspaces as a
// lightweight cache shown instantly at startup and refreshed in the background.
// BuildOpenCommand launches an existing .code-workspace in the user's editor.
package workspace

import (
	"sort"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// Found describes one discovered .code-workspace file resolved against config.
type Found struct {
	Key     string                   // stable workspace key (filename stem, sanitised, de-duplicated)
	Name    string                   // display name (filename stem)
	File    string                   // absolute path to the .code-workspace file
	Members []config.WorkspaceMember // member clones that resolved to known repos
}

// RefreshCache rescans the standard and extra folders for .code-workspace files
// and rebuilds the read-only workspace cache (cfg.Workspaces / WorkspaceOrder).
// It reports whether the cache changed, so callers persist and notify only when
// something is different. The caller is responsible for saving cfg.
func RefreshCache(cfg *config.Config) (changed bool, err error) {
	found, err := Discover(cfg)
	if err != nil {
		return false, err
	}
	newWS := make(map[string]config.Workspace, len(found))
	order := make([]string, 0, len(found))
	for _, f := range found {
		newWS[f.Key] = config.Workspace{
			Name:       f.Name,
			File:       f.File,
			Members:    append([]config.WorkspaceMember(nil), f.Members...),
			Discovered: true,
		}
		order = append(order, f.Key)
	}
	if workspacesEqual(cfg.Workspaces, newWS) {
		return false, nil
	}
	cfg.Workspaces = newWS
	cfg.WorkspaceOrder = order
	return true, nil
}

// TentativeContainers returns managed repos that have a .code-workspace file at
// their clone root but are not yet flagged Container. These are surfaced as
// suggestions ("this looks like a multi-repo parent — mark as container?");
// confirming sets Repo.Container and unlocks nested-clone discovery.
func TentativeContainers(cfg *config.Config) []status.RepoRef {
	globalFolder := config.ExpandTilde(cfg.Global.Folder)
	// Map each managed clone's normalized root path to its RepoRef.
	rootToRef := make(map[string]status.RepoRef)
	containerByRoot := make(map[string]bool)
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		sourceFolder := src.EffectiveFolder(srcKey)
		for _, repoKey := range src.OrderedRepoKeys() {
			repo := src.Repos[repoKey]
			p := normPath(status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo))
			rootToRef[p] = status.RepoRef{Source: srcKey, Repo: repoKey}
			containerByRoot[p] = repo.Container
		}
	}

	files, err := findCodeWorkspaces(scanRoots(cfg))
	if err != nil {
		return nil
	}
	seen := make(map[status.RepoRef]bool)
	var out []status.RepoRef
	for _, f := range files {
		dir := normPath(parentDir(f))
		ref, ok := rootToRef[dir]
		if !ok || containerByRoot[dir] || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Repo < out[j].Repo
	})
	return out
}

// workspacesEqual reports whether two workspace caches are equivalent for the
// purpose of change detection (keys, file paths, names, and members match).
func workspacesEqual(a, b map[string]config.Workspace) bool {
	if len(a) != len(b) {
		return false
	}
	for k, wa := range a {
		wb, ok := b[k]
		if !ok || wa.Name != wb.Name || wa.File != wb.File || len(wa.Members) != len(wb.Members) {
			return false
		}
		for i := range wa.Members {
			if wa.Members[i] != wb.Members[i] {
				return false
			}
		}
	}
	return true
}
