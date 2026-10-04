package config

import (
	"os"
	"path/filepath"
	"strings"
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
