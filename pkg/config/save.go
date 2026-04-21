package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// maxBackups is the number of dated backup files to keep. A rolling 10-slot
// window strikes a balance between recovery headroom (so the last few saves
// before a corruption are still on disk even after several "noisy" launches
// that each take a new snapshot) and leaving the config directory tidy.
const maxBackups = 10

// Save writes the configuration to the given file path as indented JSON.
// It creates the parent directory if it doesn't exist.
// Before overwriting, it creates a dated backup (best-effort, rolling last 10
// saves) — unless the only difference between on-disk and in-memory is
// window-position state, which happens on every GUI close and would rotate
// meaningful backups out of the ring otherwise.
func Save(cfg *Config, path string) error {
	if err := EnsureDir(path); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Best-effort backup — errors logged to stderr, never block saving.
	backupBeforeSave(path, cfg)

	data, err := Marshal(cfg)
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// backupBeforeSave copies the existing config file to a timestamped backup
// (e.g., gitbox-20260401-143025.json) and prunes old backups beyond maxBackups.
// If the file doesn't exist yet, no backup is created.
//
// Skips the backup when the only difference between the on-disk file and the
// next cfg is window-position state (global.window / global.compact_window).
// Every GUI session writes fresh window coordinates on close, and without
// this filter a few unrelated launches would roll out the genuine
// pre-corruption copies before the user ever notices anything is wrong. If
// the on-disk file is unparseable (really stale or bug-mangled), the snapshot
// is taken regardless — we want a copy of the broken state.
func backupBeforeSave(path string, next *Config) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return // file doesn't exist or is a directory — nothing to back up
	}

	// If we can parse the on-disk config and the in-memory one is identical
	// modulo window state, there's nothing worth preserving.
	if next != nil {
		if current, err := Load(path); err == nil {
			if configsEqualIgnoringWindow(current, next) {
				return
			}
		}
	}

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, ".json")
	now := time.Now().Format("20060102-150405")
	backupName := fmt.Sprintf("%s-%s.json", name, now)
	backupPath := filepath.Join(dir, backupName)

	if err := copyFile(path, backupPath); err != nil {
		fmt.Fprintf(os.Stderr, "warning: config backup failed: %v\n", err)
		return
	}

	pruneBackups(dir, name)
}

// configsEqualIgnoringWindow reports whether two configs are content-equal
// ignoring window-position state. Compares via canonical JSON so nested
// maps/slices are handled correctly (Go map iteration order doesn't matter
// because json.Marshal sorts map keys).
func configsEqualIgnoringWindow(a, b *Config) bool {
	if a == nil || b == nil {
		return a == b
	}
	ac := *a
	bc := *b
	ac.Global.Window = nil
	ac.Global.CompactWindow = nil
	bc.Global.Window = nil
	bc.Global.CompactWindow = nil
	da, err := json.Marshal(&ac)
	if err != nil {
		return false
	}
	db, err := json.Marshal(&bc)
	if err != nil {
		return false
	}
	return bytes.Equal(da, db)
}

// copyFile copies src to dst, overwriting dst if it exists.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// pruneBackups removes the oldest dated backup files beyond maxBackups.
// Matches files like "gitbox-2026-04-01.json" in the config directory.
func pruneBackups(dir, baseName string) {
	pattern := filepath.Join(dir, baseName+"-????????-??????.json")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) <= maxBackups {
		return
	}

	// Sort lexicographically — ISO dates sort correctly.
	sort.Strings(matches)

	// Remove the oldest files beyond the limit.
	for _, old := range matches[:len(matches)-maxBackups] {
		os.Remove(old)
	}
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
