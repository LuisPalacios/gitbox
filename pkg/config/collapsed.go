package config

import (
	"slices"
	"strings"
)

// CollapsedState lists what the GUI shows folded in the full view. It records
// collapsed items (not expanded ones) so new sources and clones appear
// expanded by default.
type CollapsedState struct {
	Sources []string `json:"sources,omitempty"` // source keys
	Repos   []string `json:"repos,omitempty"`   // "source/repo" keys
}

// PruneCollapsed drops collapse keys whose source or repo no longer exists,
// dedupes and sorts the rest, and clears the field when nothing is left.
// Save calls it so renames, deletes and moves never leave stale keys behind.
func (c *Config) PruneCollapsed() {
	cs := c.Global.Collapsed
	if cs == nil {
		return
	}
	var sources, repos []string
	for _, key := range cs.Sources {
		if _, ok := c.Sources[key]; ok {
			sources = append(sources, key)
		}
	}
	for _, key := range cs.Repos {
		if c.collapsedRepoExists(key) {
			repos = append(repos, key)
		}
	}
	slices.Sort(sources)
	slices.Sort(repos)
	sources = slices.Compact(sources)
	repos = slices.Compact(repos)
	if len(sources) == 0 && len(repos) == 0 {
		c.Global.Collapsed = nil
		return
	}
	c.Global.Collapsed = &CollapsedState{Sources: sources, Repos: repos}
}

// collapsedRepoExists resolves a "source/repo" key. Repo keys usually hold a
// slash themselves ("org/repo"), so the source is the first segment only.
func (c *Config) collapsedRepoExists(key string) bool {
	sourceKey, repoKey, ok := strings.Cut(key, "/")
	if !ok {
		return false
	}
	src, ok := c.Sources[sourceKey]
	if !ok {
		return false
	}
	_, ok = src.Repos[repoKey]
	return ok
}
