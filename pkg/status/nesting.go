package status

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// RepoRef identifies a configured repository by its source and repo keys.
type RepoRef struct {
	Source string
	Repo   string
}

// Nesting describes the parent→child relationships between container repos
// (Repo.Container) and the clones nested inside their working trees, derived
// purely from resolved clone paths. It is the single source of truth the
// CLI/TUI/GUI use to render nested clones indented under their container.
type Nesting struct {
	// ParentOf maps a child repo to its nearest container parent.
	ParentOf map[RepoRef]RepoRef
	// Children maps a container repo to its direct children, in config order.
	Children map[RepoRef][]RepoRef
	// Path holds the resolved absolute clone path for every repo.
	Path map[RepoRef]string
}

// ComputeNesting resolves every repo's clone path and links each repo to the
// deepest container repo whose resolved path is a strict ancestor of it. A repo
// flagged Container is a potential parent; nesting is otherwise inferred from
// paths, so a child onboarded with an absolute clone_folder inside a container
// is grouped correctly regardless of its account/org.
func ComputeNesting(cfg *config.Config) Nesting {
	globalFolder := config.ExpandTilde(cfg.Global.Folder)

	n := Nesting{
		ParentOf: make(map[RepoRef]RepoRef),
		Children: make(map[RepoRef][]RepoRef),
		Path:     make(map[RepoRef]string),
	}

	// First pass: resolve all paths and collect container paths.
	type container struct {
		ref  RepoRef
		path string // normalized
	}
	var containers []container
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		sourceFolder := src.EffectiveFolder(srcKey)
		for _, repoKey := range src.OrderedRepoKeys() {
			repo := src.Repos[repoKey]
			ref := RepoRef{Source: srcKey, Repo: repoKey}
			p := ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
			n.Path[ref] = p
			if repo.Container {
				containers = append(containers, container{ref: ref, path: normNestPath(p)})
			}
		}
	}
	if len(containers) == 0 {
		return n
	}

	// Second pass: assign each repo to its deepest container ancestor.
	for _, srcKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[srcKey]
		for _, repoKey := range src.OrderedRepoKeys() {
			ref := RepoRef{Source: srcKey, Repo: repoKey}
			childPath := normNestPath(n.Path[ref])
			var best container
			bestLen := -1
			for _, c := range containers {
				if c.ref == ref {
					continue
				}
				if isUnder(childPath, c.path) && len(c.path) > bestLen {
					best = c
					bestLen = len(c.path)
				}
			}
			if bestLen >= 0 {
				n.ParentOf[ref] = best.ref
				n.Children[best.ref] = append(n.Children[best.ref], ref)
			}
		}
	}
	return n
}

// isUnder reports whether child is strictly inside parent (not equal).
func isUnder(child, parent string) bool {
	if parent == "" || child == parent {
		return false
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(parent, sep) {
		parent += sep
	}
	return strings.HasPrefix(child, parent)
}

// normNestPath normalizes a path for containment comparison: cleaned, and
// case-folded on case-insensitive filesystems (Windows, macOS default).
func normNestPath(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		p = strings.ToLower(p)
	}
	return p
}
