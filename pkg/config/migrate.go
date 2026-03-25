package config

import (
	"fmt"
	"os"
)

// Migrate reads a v1 configuration file, converts it to v2, and writes it to the v2 path.
// The original v1 file is never modified.
// Returns the v2 config and the path it was written to.
func Migrate(v1Path, v2Path string) (*Config, error) {
	// Check v1 file exists.
	if _, err := os.Stat(v1Path); err != nil {
		return nil, fmt.Errorf("v1 config not found at %s: %w", v1Path, err)
	}

	// Check v2 file doesn't already exist (don't overwrite).
	if _, err := os.Stat(v2Path); err == nil {
		return nil, fmt.Errorf("v2 config already exists at %s — remove it first or use a different path", v2Path)
	}

	// Load v1 (auto-converts to v2 structs in memory).
	cfg, err := Load(v1Path)
	if err != nil {
		return nil, fmt.Errorf("loading v1 config: %w", err)
	}

	// Set the v2 schema reference.
	cfg.Schema = "https://raw.githubusercontent.com/LuisPalacios/gitbox/main/gitbox.schema.json"

	// Save as v2.
	if err := Save(cfg, v2Path); err != nil {
		return nil, fmt.Errorf("saving v2 config: %w", err)
	}

	return cfg, nil
}

// MigrateDryRun loads a v1 config and returns the v2 conversion without writing anything.
func MigrateDryRun(v1Path string) (*Config, error) {
	cfg, err := Load(v1Path)
	if err != nil {
		return nil, fmt.Errorf("loading v1 config: %w", err)
	}
	cfg.Schema = "https://raw.githubusercontent.com/LuisPalacios/gitbox/main/gitbox.schema.json"
	return cfg, nil
}
