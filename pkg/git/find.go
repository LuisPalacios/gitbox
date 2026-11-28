package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// DiscoveryDepth bounds an ordinary root scan. A workspace entry starts a new bounded walk.
const DiscoveryDepth = 8

// Coverage holds observed clones and the directory branches a walk could not inspect.
type Coverage struct {
	Repos   []string
	Skipped []string
	// Unreadable lists the skipped folders the scan could not read; the
	// rest of Skipped lay below the depth limit.
	Unreadable []string
	Errors     []error
	Complete   bool
}

// FindReposContext stops at clone boundaries, hidden directories and the requested depth.
func FindReposContext(ctx context.Context, root string, maxDepth int) (Coverage, error) {
	return findRepos(ctx, root, maxDepth, false)
}

// FindNestedReposContext searches the contents of a container and skips vendor directories.
func FindNestedReposContext(ctx context.Context, parent string, maxDepth int) (Coverage, error) {
	return findRepos(ctx, parent, max(1, maxDepth), true)
}

// FindRepos returns the clones found under root and reports incomplete coverage.
func FindRepos(root string) ([]string, error) {
	c, err := FindReposContext(context.Background(), root, DiscoveryDepth)
	if !c.Complete && err == nil && len(c.Errors) == 0 {
		err = errors.New("directory scan reached its depth limit")
	}
	return c.Repos, errors.Join(err, errors.Join(c.Errors...))
}

// FindNestedRepos returns observed clones within the configured container depth.
func FindNestedRepos(parent string, maxDepth int) []string {
	c, _ := FindNestedReposContext(context.Background(), parent, maxDepth)
	return c.Repos
}

var nestedSkipDirs = map[string]bool{"node_modules": true, "vendor": true, "target": true, "dist": true, "build": true, "out": true}

func findRepos(ctx context.Context, root string, maxDepth int, nested bool) (Coverage, error) {
	c := Coverage{Complete: true}
	skip := func(path string, err error) {
		c.Complete = false
		c.Skipped = append(c.Skipped, path)
		if err != nil {
			c.Unreadable = append(c.Unreadable, path)
			c.Errors = append(c.Errors, err)
		}
	}
	var visit func(string, int)
	visit = func(path string, depth int) {
		if ctx.Err() != nil {
			return
		}
		if !nested || depth > 0 {
			_, err := os.Stat(filepath.Join(path, ".git"))
			if err == nil {
				c.Repos = append(c.Repos, path)
				return
			}
			if !errors.Is(err, os.ErrNotExist) {
				skip(path, err)
				return
			}
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			skip(path, err)
			return
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return
			}
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || (nested && nestedSkipDirs[entry.Name()]) {
				continue
			}
			child := filepath.Join(path, entry.Name())
			if depth >= maxDepth {
				skip(child, nil)
				continue
			}
			visit(child, depth+1)
		}
	}
	// An ordinary scan, like WalkDir, never follows a directory symlink
	// supplied as the root. Workspace callers already resolve their entries.
	if !nested && ctx.Err() == nil {
		info, err := os.Lstat(root)
		if err != nil {
			skip(root, err)
			return c, nil
		}
		if !info.IsDir() {
			return c, nil
		}
	}
	visit(root, 0)
	if ctx.Err() != nil {
		skip(root, ctx.Err())
	}
	return c, ctx.Err()
}
