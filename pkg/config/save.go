package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Save writes the configuration to the given file path as indented JSON.
// It creates the parent directory if it doesn't exist.
func Save(cfg *Config, path string) error {
	if err := EnsureDir(path); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := Marshal(cfg)
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// Marshal serializes the configuration to indented JSON bytes.
func Marshal(cfg *Config) ([]byte, error) {
	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return nil, fmt.Errorf("marshalling config: %w", err)
	}
	// Append a trailing newline for clean file output.
	data = append(data, '\n')
	return data, nil
}
