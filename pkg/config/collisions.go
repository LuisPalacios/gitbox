package config

import (
	"errors"
	"path/filepath"

	"github.com/LuisPalacios/gitbox/pkg/pathkey"
)

// ErrPathCollision identifies a placement that would give two configured
// entries the same folder. No paths are included in the error text.
var ErrPathCollision = errors.New("another entry resolves to the same folder on this filesystem")

// ErrPathUnavailable means the target could not be inspected safely.
var ErrPathUnavailable = errors.New("the folder cannot be inspected")

// PathConflict identifies two entries that refer to one folder.
type PathConflict struct {
	First  string `json:"first"`
	Second string `json:"second"`
}

// PathConflicts reports collisions in existing config without changing it.
func (c *Config) PathConflicts() []PathConflict {
	var conflicts []PathConflict
	add := func(owners map[string]string, owner, path string) {
		key := pathkey.Compare(path)
		if prev, ok := owners[key]; ok {
			conflicts = append(conflicts, PathConflict{First: prev, Second: owner})
		} else {
			owners[key] = owner
		}
	}
	roots := map[string]string{}
	for _, r := range c.Global.Roots {
		add(roots, r, r)
	}
	repos := map[string]string{}
	for _, sk := range c.OrderedSourceKeys() {
		src := c.Sources[sk]
		for _, rk := range src.OrderedRepoKeys() {
			add(repos, sk+"/"+rk, RepoPath(c, sk, rk, src.Repos[rk]))
		}
	}
	return conflicts
}

// CheckClonePath verifies a placement against every other tracked clone.
func (c *Config) CheckClonePath(source, repo, path string) error {
	target := pathkey.Of(path)
	if target.Access == pathkey.Unknown {
		return ErrPathUnavailable
	}
	for sk, src := range c.Sources {
		for rk, r := range src.Repos {
			if sk == source && rk == repo {
				continue
			}
			if pathkey.Compare(RepoPath(c, sk, rk, r)) == target.Cmp {
				return ErrPathCollision
			}
		}
	}
	return nil
}

// checkRoot accepts an existing root with the same lexical spelling. A
// different spelling that aliases it must not silently select that root.
func (c *Config) checkRoot(path string) (bool, error) {
	target := pathkey.Of(path)
	if target.Access == pathkey.Unknown {
		return false, ErrPathUnavailable
	}
	spelling := filepath.Clean(ExpandTilde(path))
	listed := false
	for _, r := range c.Global.Roots {
		if filepath.Clean(ExpandTilde(r)) == spelling {
			listed = true
			continue
		}
		if pathkey.Compare(r) == target.Cmp {
			return false, ErrPathCollision
		}
	}
	return listed, nil
}
