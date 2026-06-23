package git

import (
	"os"
	"path/filepath"
	"strings"
)

// FindRepos walks the directory tree from root and returns absolute paths
// to all git repositories (directories containing a .git subdirectory).
// Hidden directories (other than .git itself) are skipped.
// Descending stops once a .git directory is found (no nested repos).
func FindRepos(root string) ([]string, error) {
	var repos []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip inaccessible directories.
		}
		if !d.IsDir() {
			return nil
		}

		name := d.Name()

		// Skip hidden directories (except .git itself).
		if strings.HasPrefix(name, ".") && name != ".git" {
			return filepath.SkipDir
		}

		// If this directory is .git, its parent is a repo.
		if name == ".git" {
			repos = append(repos, filepath.Dir(path))
			return filepath.SkipDir
		}

		return nil
	})

	return repos, err
}

// nestedSkipDirs are directory names ignored when discovering nested clones,
// to keep noise (vendored trees, build output) out of the results.
var nestedSkipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
	"build":        true,
	"out":          true,
}

// FindNestedRepos returns absolute paths to git clones nested inside the working
// tree of a container repo at parent. Unlike FindRepos (which stops at the first
// .git boundary), this descends past parent itself to find user-cloned siblings,
// up to maxDepth directory levels below parent — maxDepth 1 inspects only
// parent's immediate children. The parent's own .git is ignored, gitbox does not
// descend into a discovered nested clone, and hidden + common vendor directories
// are skipped to limit noise. Best-effort: unreadable directories are skipped.
func FindNestedRepos(parent string, maxDepth int) []string {
	if maxDepth < 1 {
		maxDepth = 1
	}
	var repos []string
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return // Skip inaccessible directories.
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") || nestedSkipDirs[name] {
				continue
			}
			child := filepath.Join(dir, name)
			if isGitClone(child) {
				repos = append(repos, child)
				continue // Don't descend into a discovered nested clone.
			}
			if depth < maxDepth {
				walk(child, depth+1)
			}
		}
	}
	walk(parent, 1)
	return repos
}

// isGitClone reports whether dir is a git clone — i.e. contains a .git entry
// (a directory for a normal clone, or a file for a worktree/submodule).
func isGitClone(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}
