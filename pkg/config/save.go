package config

import (
	"bytes"
	"encoding/json"
	"errors"
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
// It first prunes collapse keys that no longer resolve (PruneCollapsed).
// Before overwriting, it creates a dated backup (best-effort, rolling last 10
// saves) — unless the only difference between on-disk and in-memory is GUI
// state (window position, collapsed groups), which changes on every GUI close
// or chevron click and would rotate meaningful backups out of the ring otherwise.
func Save(cfg *Config, path string) error {
	return saveWithFiles(cfg, path, configFiles())
}

// I pass file operations per save so failure tests never replace global hooks.
type stagedFile interface {
	io.Writer
	Name() string
	Chmod(os.FileMode) error
	Sync() error
	Close() error
}

type configFileOps struct {
	createTemp func(string, string) (stagedFile, error)
	rename     func(string, string) error
	link       func(string, string) error
	now        func() time.Time
}

func configFiles() configFileOps {
	return configFileOps{
		createTemp: func(dir, pattern string) (stagedFile, error) { return os.CreateTemp(dir, pattern) },
		rename:     os.Rename,
		link:       os.Link,
		now:        time.Now,
	}
}

func saveWithFiles(cfg *Config, path string, files configFileOps) error {
	cfg.PruneCollapsed()

	if err := EnsureDir(path); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := Marshal(cfg)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := stageConfigFile(filepath.Dir(path), bytes.NewReader(data), mode, files)
	if err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	defer os.Remove(tmp)

	// I snapshot the old file only after the replacement is ready.
	backupBeforeSave(path, cfg, files)
	if err := files.rename(tmp, path); err != nil {
		return fmt.Errorf("replacing config: %w", err)
	}
	return nil
}

// backupBeforeSave copies the existing config file to a timestamped backup
// (e.g., gitbox-20260401-143025.json) and prunes old backups beyond maxBackups.
// If the file doesn't exist yet, no backup is created.
//
// Skips the backup when the only difference between the on-disk file and the
// next cfg is GUI state (global.window / global.compact_window /
// global.collapsed). Every GUI session writes fresh window coordinates on
// close, every chevron click writes collapse state, and without this filter a few unrelated launches would roll out the genuine
// pre-corruption copies before the user ever notices anything is wrong. If
// the on-disk file is unparseable (really stale or bug-mangled), the snapshot
// is taken regardless — we want a copy of the broken state.
func backupBeforeSave(path string, next *Config, files configFileOps) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return // file doesn't exist or is a directory — nothing to back up
	}

	// If we can parse the on-disk config and the in-memory one is identical
	// modulo GUI state, there's nothing worth preserving. A file written by
	// an older config version is always kept: loading migrates it, so it
	// would compare equal and the last copy an older gitbox can read would
	// be overwritten (#149).
	if next != nil && onDiskVersion(path) == CurrentVersion {
		if current, err := Load(path); err == nil {
			if configsEqualIgnoringUIState(current, next) {
				return
			}
		}
	}

	if err := copyBackup(path, files); err != nil {
		fmt.Fprintf(os.Stderr, "warning: config backup failed: %v\n", err)
		return
	}
	pruneBackups(path)
}

// onDiskVersion returns the "version" field of the config file at path,
// or 0 when it can't be read.
func onDiskVersion(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var probe struct {
		Version int `json:"version"`
	}
	_ = json.Unmarshal(data, &probe)
	return probe.Version
}

// configsEqualIgnoringUIState reports whether two configs are content-equal
// ignoring GUI state (window position, collapsed groups and theme). Compares via canonical JSON so nested
// maps/slices are handled correctly (Go map iteration order doesn't matter
// because json.Marshal sorts map keys).
func configsEqualIgnoringUIState(a, b *Config) bool {
	if a == nil || b == nil {
		return a == b
	}
	ac := *a
	bc := *b
	ac.Global.Window = nil
	ac.Global.CompactWindow = nil
	ac.Global.Collapsed = nil
	ac.Global.Theme = ""
	bc.Global.Window = nil
	bc.Global.CompactWindow = nil
	bc.Global.Collapsed = nil
	bc.Global.Theme = ""
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

// stageConfigFile finishes the bytes and metadata before exposing a final name.
func stageConfigFile(dir string, src io.Reader, mode os.FileMode, files configFileOps) (name string, err error) {
	out, err := files.createTemp(dir, ".gitbox-*.tmp")
	if err != nil {
		return "", err
	}
	name = out.Name()
	closed := false
	defer func() {
		if !closed {
			_ = out.Close()
		}
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	if _, err = io.Copy(out, src); err != nil {
		return name, err
	}
	if err = out.Chmod(mode); err != nil {
		return name, err
	}
	if err = out.Sync(); err != nil {
		return name, err
	}
	err = out.Close()
	closed = true
	return name, err
}

// copyBackup publishes only complete files. Link refuses an existing name,
// unlike Rename, without exposing an empty reservation to recovery readers.
func copyBackup(path string, files configFileOps) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := stageConfigFile(dir, in, info.Mode().Perm(), files)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	base := strings.TrimSuffix(filepath.Base(path), ".json")
	prefix := base + "-" + files.now().Format("20060102-150405")
	for sequence := range maxBackups {
		name := prefix
		if sequence > 0 {
			name += fmt.Sprintf("-%d", sequence)
		}
		err = files.link(tmp, filepath.Join(dir, name+".json"))
		if !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	return fmt.Errorf("all %d backup names for %s already exist: %w", maxBackups, prefix, err)
}

// Backup describes a completed config snapshot.
type Backup struct {
	Path      string
	Timestamp time.Time
	sequence  int
}

// ListBackups returns recognized backup files, newest first. A numbered suffix
// orders same-second snapshots after the original unsuffixed name.
func ListBackups(path string) ([]Backup, error) {
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	prefix := strings.TrimSuffix(filepath.Base(path), ".json") + "-"
	var backups []Backup
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		stamp := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), prefix), ".json")
		sequence := 0
		if len(stamp) == 17 && stamp[15] == '-' && stamp[16] >= '1' && stamp[16] <= '9' {
			sequence = int(stamp[16] - '0')
			stamp = stamp[:15]
		}
		timestamp, err := time.Parse("20060102-150405", stamp)
		if err != nil {
			continue
		}
		backups = append(backups, Backup{Path: filepath.Join(dir, entry.Name()), Timestamp: timestamp, sequence: sequence})
	}
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].Timestamp.Equal(backups[j].Timestamp) {
			return backups[i].sequence > backups[j].sequence
		}
		return backups[i].Timestamp.After(backups[j].Timestamp)
	})
	return backups, nil
}

// pruneBackups removes the oldest completed snapshots beyond maxBackups.
func pruneBackups(path string) {
	backups, err := ListBackups(path)
	if err != nil || len(backups) <= maxBackups {
		return
	}
	for _, old := range backups[maxBackups:] {
		_ = os.Remove(old.Path)
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
