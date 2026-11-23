package config

import (
	"errors"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/pathkey"
)

// Roots (issue #149): global.roots is the ordered list of folders gitbox
// clones into and scans. The first one is the default. A source remembers
// the root its account clones into (Source.Root, empty = the default) and a
// repo records its own root only when it differs from its source's
// (Repo.Root). Every mutator below pins the roots a clone already resolves
// through before changing a default, so no clone's path ever moves.

// ErrRootInUse reports that a root still holds tracked clones.
var ErrRootInUse = errors.New("root still holds tracked clones")

// ErrLastRoot reports an attempt to remove the only root.
var ErrLastRoot = errors.New("gitbox needs at least one root")

// SameRoot reports whether two root paths name the same folder.
func SameRoot(a, b string) bool {
	return pathkey.Compare(a) == pathkey.Compare(b)
}

// DefaultRoot returns the first root, tilde-expanded, or "" when none is set.
func (c *Config) DefaultRoot() string {
	if len(c.Global.Roots) == 0 {
		return ""
	}
	return ExpandTilde(c.Global.Roots[0])
}

// SourceRoot returns the root a source clones into, tilde-expanded.
func (c *Config) SourceRoot(sourceKey string) string {
	if src, ok := c.Sources[sourceKey]; ok && src.Root != "" {
		return ExpandTilde(src.Root)
	}
	return c.DefaultRoot()
}

// RepoRoot returns the root a repo resolves under, tilde-expanded.
func (c *Config) RepoRoot(sourceKey string, repo Repo) string {
	if repo.Root != "" {
		return ExpandTilde(repo.Root)
	}
	return c.SourceRoot(sourceKey)
}

// HasRoot reports whether path is one of the configured roots.
func (c *Config) HasRoot(path string) bool {
	return c.rootIndex(path) >= 0
}

func (c *Config) rootIndex(path string) int {
	want := pathkey.Compare(path)
	for i, r := range c.Global.Roots {
		if pathkey.Compare(r) == want {
			return i
		}
	}
	return -1
}

// RootRefs returns the "source/repo" keys whose clones resolve under path.
func (c *Config) RootRefs(path string) []string {
	var refs []string
	for _, sk := range c.OrderedSourceKeys() {
		src := c.Sources[sk]
		for _, rk := range src.OrderedRepoKeys() {
			repo, ok := src.Repos[rk]
			if ok && SameRoot(c.RepoRoot(sk, repo), path) {
				refs = append(refs, sk+"/"+rk)
			}
		}
	}
	return refs
}

// AddRoot appends path unless an equivalent root is already listed. Empty
// input is a no-op. Returns true when the list changed.
func (c *Config) AddRoot(path string) bool {
	changed, _ := c.AddRootChecked(path)
	return changed
}

// AddRootChecked reports a collision when a different configured spelling already owns the root.
func (c *Config) AddRootChecked(path string) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, nil
	}
	if listed, err := c.checkRoot(path); err != nil || listed {
		return false, err
	}
	c.Global.Roots = append(c.Global.Roots, path)
	return true, nil
}

// SetSourceRoot makes root the one a source clones into next. Repos that
// followed the source's previous root are pinned to it first, so they stay
// where they are on disk. root is added to the list when missing.
func (c *Config) SetSourceRoot(sourceKey, root string) error {
	src, ok := c.Sources[sourceKey]
	if !ok || strings.TrimSpace(root) == "" {
		return nil
	}
	if _, err := c.AddRootChecked(root); err != nil {
		return err
	}
	if SameRoot(c.SourceRoot(sourceKey), root) {
		return nil
	}
	prev := c.ListedRoot(c.SourceRoot(sourceKey))
	for rk, repo := range src.Repos {
		if repo.Root == "" {
			repo.Root = prev
			src.Repos[rk] = repo
		}
	}
	src.Root = c.ListedRoot(root)
	if SameRoot(root, c.DefaultRoot()) {
		src.Root = ""
	}
	// A pin that now matches the source's root is redundant.
	for rk, repo := range src.Repos {
		if repo.Root != "" && SameRoot(repo.Root, root) {
			repo.Root = ""
			src.Repos[rk] = repo
		}
	}
	c.Sources[sourceKey] = src
	return nil
}

// SetDefaultRoot moves path to the front of the list. Sources that followed
// the previous default are pinned to it first, so nothing moves on disk.
func (c *Config) SetDefaultRoot(path string) {
	i := c.rootIndex(path)
	if i <= 0 {
		return
	}
	prev := c.Global.Roots[0]
	for sk, src := range c.Sources {
		if src.Root == "" {
			src.Root = prev
			c.Sources[sk] = src
		}
	}
	chosen := c.Global.Roots[i]
	rest := append([]string{}, c.Global.Roots[:i]...)
	c.Global.Roots = append(append([]string{chosen}, rest...), c.Global.Roots[i+1:]...)
	// A source now pinned to the new default no longer needs the pin.
	for sk, src := range c.Sources {
		if src.Root != "" && SameRoot(src.Root, chosen) {
			src.Root = ""
			c.Sources[sk] = src
		}
	}
}

// RemoveRoot drops path from the list. The last root can't go. When clones
// still resolve under it, it refuses with ErrRootInUse unless forget is set;
// then it removes those repo entries from the config (never from disk) and
// returns their keys. Sources that pointed at the root fall back to the
// default.
func (c *Config) RemoveRoot(path string, forget bool) ([]string, error) {
	i := c.rootIndex(path)
	if i < 0 {
		return nil, nil
	}
	// An ambiguous legacy root must never forget another entry's clones.
	if _, err := c.checkRoot(c.Global.Roots[i]); errors.Is(err, ErrPathCollision) {
		return nil, err
	}
	if len(c.Global.Roots) == 1 {
		return nil, ErrLastRoot
	}
	refs := c.RootRefs(path)
	if len(refs) > 0 && !forget {
		return refs, ErrRootInUse
	}
	for _, key := range refs {
		sk, rk, _ := strings.Cut(key, "/")
		_ = c.DeleteRepo(sk, rk)
	}
	// Removing the default promotes the next root; nothing resolved under
	// the removed one anymore, so no other clone moves.
	c.Global.Roots = append(c.Global.Roots[:i:i], c.Global.Roots[i+1:]...)
	for sk, src := range c.Sources {
		if src.Root != "" && SameRoot(src.Root, path) {
			src.Root = ""
			c.Sources[sk] = src
		}
	}
	return refs, nil
}

// ListedRoot returns the list's own spelling of path (keeping "~"), or path.
func (c *Config) ListedRoot(path string) string {
	if i := c.rootIndex(path); i >= 0 {
		return c.Global.Roots[i]
	}
	return path
}

// PlaceRepo makes a repo of a source clone into root, the root the person
// picked: the source remembers it for its next clone (SetSourceRoot) and the
// repo drops any pin so it follows. Returns true when the config changed.
func (c *Config) PlaceRepo(sourceKey, repoKey, root string) bool {
	changed, _ := c.PlaceRepoChecked(sourceKey, repoKey, root)
	return changed
}

// PlaceRepoChecked returns the reason a requested placement was refused.
func (c *Config) PlaceRepoChecked(sourceKey, repoKey, root string) (bool, error) {
	src, ok := c.Sources[sourceKey]
	if !ok {
		return false, nil
	}
	if _, ok := src.Repos[repoKey]; !ok || strings.TrimSpace(root) == "" {
		return false, nil
	}
	dest := ResolveRepoPath(ExpandTilde(root), src.EffectiveFolder(sourceKey), repoKey, src.Repos[repoKey])
	if err := c.CheckClonePath(sourceKey, repoKey, dest); err != nil {
		return false, err
	}
	if _, err := c.checkRoot(root); err != nil {
		return false, err
	}
	changed := false
	if !SameRoot(c.SourceRoot(sourceKey), root) {
		if err := c.SetSourceRoot(sourceKey, root); err != nil {
			return false, err
		}
		changed = true
	}
	src = c.Sources[sourceKey]
	if repo := src.Repos[repoKey]; repo.Root != "" {
		repo.Root = ""
		src.Repos[repoKey] = repo
		c.Sources[sourceKey] = src
		changed = true
	}
	return changed, nil
}
