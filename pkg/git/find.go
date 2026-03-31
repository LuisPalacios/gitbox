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
