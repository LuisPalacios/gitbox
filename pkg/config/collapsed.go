package config

import (
	"slices"
	"strings"
)

// CollapsedState holds the GUI's layout state. Sources and Repos list what
// the full view shows folded. They record collapsed items (not expanded
// ones) so new sources and clones appear expanded by default. Sidebar
// records the sidebar folded to its icon rail, and View the last selected
// view ("attention", "all", "mirrors", "workspaces" or "account:<key>").
type CollapsedState struct {
	Sources []string `json:"sources,omitempty"` // source keys
	Repos   []string `json:"repos,omitempty"`   // "source/repo" keys
	Sidebar bool     `json:"sidebar,omitempty"` // sidebar folded to the rail
	View    string   `json:"view,omitempty"`    // last selected view
}

// AccountViewPrefix starts a View that shows one account: "account:<key>".
const AccountViewPrefix = "account:"

// PruneCollapsed drops collapse keys whose source or repo no longer exists,
// dedupes and sorts the rest, clears a View that names a missing account,
// and clears the field when nothing is left.
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
	view := cs.View
	if key, ok := strings.CutPrefix(view, AccountViewPrefix); ok {
		if _, exists := c.Accounts[key]; !exists {
			view = ""
		}
	}
	if len(sources) == 0 && len(repos) == 0 && !cs.Sidebar && view == "" {
		c.Global.Collapsed = nil
		return
	}
	c.Global.Collapsed = &CollapsedState{Sources: sources, Repos: repos, Sidebar: cs.Sidebar, View: view}
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
