package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/pathkey"
)

const (
	// V2ConfigDir is the directory name under the user's config root.
	V2ConfigDir = "gitbox"
	// V2ConfigFile is the configuration file name.
	V2ConfigFile = "gitbox.json"
)

// DefaultV2Path returns the default path to the v2 configuration file.
// On all platforms: ~/.config/gitbox/gitbox.json
func DefaultV2Path() string {
	return filepath.Join(ConfigRoot(), V2ConfigDir, V2ConfigFile)
}

// ConfigRoot returns the base config directory.
// Uses XDG_CONFIG_HOME if set, otherwise ~/.config.
func ConfigRoot() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return xdg
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".config")
	}
	return filepath.Join(home, ".config")
}

// ExpandTilde expands a leading ~ to the user's home directory.
func ExpandTilde(path string) string { return pathkey.ExpandHome(path) }

// EnsureDir creates the directory for the given file path if it doesn't exist.
func EnsureDir(filePath string) error {
	dir := filepath.Dir(filePath)
	return os.MkdirAll(dir, 0o755)
}

// ResolveRepoPath computes the clone path for a repository.
//
// Path structure: root / sourceFolder / idFolder / cloneFolder
//
// Repo key format: "org/repo" (e.g., "myorg/my-project")
//   - idFolder defaults to "org" (part before /)
//   - cloneFolder defaults to "repo" (part after /)
//
// Overrides:
//   - repo.IdFolder overrides the 2nd level (org) dir
//   - repo.CloneFolder overrides the 3rd level (clone) dir
//   - If cloneFolder is absolute (starts with / or ~), it replaces the entire
//     path; one starting with ../ replaces it too, relative to the root
//
// root is the repo's root, already tilde-expanded (see RepoPath).
func ResolveRepoPath(root, sourceFolder, repoName string, repo Repo) string {
	// If clone_folder is absolute, it replaces everything.
	if repo.CloneFolder != "" {
		expanded := ExpandTilde(repo.CloneFolder)
		if filepath.IsAbs(expanded) || strings.HasPrefix(repo.CloneFolder, "~") {
			return expanded
		}
		if strings.HasPrefix(repo.CloneFolder, "../") {
			return filepath.Join(root, repo.CloneFolder)
		}
	}

	// Split repo key into org and repo parts.
	orgPart, repoPart := splitRepoKey(repoName)

	// Apply overrides.
	if repo.IdFolder != "" {
		orgPart = repo.IdFolder
	}
	if repo.CloneFolder != "" {
		repoPart = repo.CloneFolder
	}

	if orgPart != "" {
		return filepath.Join(root, sourceFolder, orgPart, repoPart)
	}
	return filepath.Join(root, sourceFolder, repoPart)
}

// RepoPath returns where a repo of a source lives on disk: its root, then
// the standard layout and overrides of ResolveRepoPath.
func RepoPath(cfg *Config, sourceKey, repoKey string, repo Repo) string {
	src := cfg.Sources[sourceKey]
	return ResolveRepoPath(cfg.RepoRoot(sourceKey, repo), src.EffectiveFolder(sourceKey), repoKey, repo)
}

// splitRepoKey splits "org/repo" into ("org", "repo").
// If no slash, returns ("", repoName).
func splitRepoKey(repoName string) (string, string) {
	if before, after, ok := strings.Cut(repoName, "/"); ok {
		return before, after
	}
	return "", repoName
}
