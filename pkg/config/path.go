package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// V2ConfigDir is the directory name under the user's config root.
	V2ConfigDir = "gitbox"
	// V2ConfigFile is the configuration file name.
	V2ConfigFile = "gitbox.json"

	// V1ConfigDir is the v1 config directory name.
	V1ConfigDir = "git-config-repos"
	// V1ConfigFile is the v1 config file name.
	V1ConfigFile = "git-config-repos.json"
)

// DefaultV2Path returns the default path to the v2 configuration file.
// On all platforms: ~/.config/gitbox/gitbox.json
func DefaultV2Path() string {
	return filepath.Join(configRoot(), V2ConfigDir, V2ConfigFile)
}

// DefaultV1Path returns the default path to the v1 configuration file.
// On all platforms: ~/.config/git-config-repos/git-config-repos.json
func DefaultV1Path() string {
	return filepath.Join(configRoot(), V1ConfigDir, V1ConfigFile)
}

// configRoot returns the base config directory.
// Uses XDG_CONFIG_HOME if set, otherwise ~/.config.
func configRoot() string {
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
func ExpandTilde(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}

// EnsureDir creates the directory for the given file path if it doesn't exist.
func EnsureDir(filePath string) error {
	dir := filepath.Dir(filePath)
	return os.MkdirAll(dir, 0o755)
}

// NormalizePath cleans a path and converts to forward slashes on Windows
// for consistent cross-platform handling.
func NormalizePath(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		// Keep forward slashes for consistency with git
		path = filepath.ToSlash(path)
	}
	return path
}
