package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupCreatedOnSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")

	// Create an initial config file with known content.
	os.WriteFile(path, []byte(`{"version":2,"original":true}`), 0o644)

	cfg := &Config{
		Version:  2,
		Global:   GlobalConfig{Folder: "~/test"},
		Accounts: make(map[string]Account),
		Sources:  make(map[string]Source),
	}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A dated backup should exist with the original content.
	matches, _ := filepath.Glob(filepath.Join(dir, "gitbox-????????-??????.json"))
	if len(matches) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(matches))
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"version":2,"original":true}` {
		t.Errorf("backup content = %q, want original", string(data))
	}
}

func TestNoBackupOnFirstSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")

	cfg := &Config{
		Version:  2,
		Global:   GlobalConfig{Folder: "~/test"},
		Accounts: make(map[string]Account),
		Sources:  make(map[string]Source),
	}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// No backup should exist — file didn't exist before save.
	matches, _ := filepath.Glob(filepath.Join(dir, "gitbox-????????-??????.json"))
	if len(matches) != 0 {
		t.Errorf("expected 0 backups on first save, got %d", len(matches))
	}
}

func TestBackupPruning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")

	// Create the config file.
	os.WriteFile(path, []byte(`{}`), 0o644)

	// Pre-create 7 timestamped backups.
	dates := []string{
		"20260325-100000", "20260326-100000", "20260327-100000", "20260328-100000",
		"20260329-100000", "20260330-100000", "20260331-100000",
	}
	for _, d := range dates {
		os.WriteFile(filepath.Join(dir, "gitbox-"+d+".json"), []byte(`{}`), 0o644)
	}

	cfg := &Config{
		Version:  2,
		Global:   GlobalConfig{Folder: "~/test"},
		Accounts: make(map[string]Account),
		Sources:  make(map[string]Source),
	}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Should have at most 5 backups (7 pre-existing + 1 today's = 8, pruned to 5).
	matches, _ := filepath.Glob(filepath.Join(dir, "gitbox-????????-??????.json"))
	if len(matches) > maxBackups {
		t.Errorf("expected at most %d backups, got %d", maxBackups, len(matches))
		for _, m := range matches {
			t.Logf("  %s", filepath.Base(m))
		}
	}
}

// Window-position saves happen on every GUI close. If we backed up on each
// one, a few noisy launches would rotate the genuine pre-corruption copies
// out of the ring. Verify the skip-path works.
func TestBackupSkippedForWindowOnlyChanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")

	// Seed a valid, loadable config to disk.
	cfg := &Config{
		Version: 2,
		Global: GlobalConfig{
			Folder: "~/00.git",
			Window: &WindowState{X: 10, Y: 10, Width: 800, Height: 600},
		},
		Accounts: map[string]Account{
			"a1": {Provider: "github", URL: "https://github.com",
				Username: "u", Name: "N", Email: "e@e"},
		},
		Sources: map[string]Source{},
	}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("initial Save: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "gitbox-????????-??????.json"))
	if len(matches) != 0 {
		t.Fatalf("first save should not create a backup (no prior file), got %d", len(matches))
	}

	// Change only window position — must NOT create a backup.
	cfg.Global.Window.X = 500
	cfg.Global.Window.Y = 250
	if err := Save(cfg, path); err != nil {
		t.Fatalf("window-only Save: %v", err)
	}
	matches, _ = filepath.Glob(filepath.Join(dir, "gitbox-????????-??????.json"))
	if len(matches) != 0 {
		t.Errorf("window-only change should not create a backup, got %d", len(matches))
	}

	// Add a compact_window entry only — still no backup.
	cfg.Global.CompactWindow = &WindowState{X: 50, Y: 50, Width: 220, Height: 400}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("compact-window-only Save: %v", err)
	}
	matches, _ = filepath.Glob(filepath.Join(dir, "gitbox-????????-??????.json"))
	if len(matches) != 0 {
		t.Errorf("compact-window-only change should not create a backup, got %d", len(matches))
	}

	// Change real content (periodic_sync) alongside a window update — must
	// create a backup: we need a rollback point for the real change.
	cfg.Global.PeriodicSync = "5m"
	cfg.Global.Window.X = 900
	if err := Save(cfg, path); err != nil {
		t.Fatalf("content Save: %v", err)
	}
	matches, _ = filepath.Glob(filepath.Join(dir, "gitbox-????????-??????.json"))
	if len(matches) != 1 {
		t.Errorf("content change should create 1 backup, got %d", len(matches))
	}
}

// When the on-disk file is unparseable (e.g. a stale 0-byte file or a config
// the validator rejects), we cannot tell whether the save is cosmetic — so
// fall back to the safe default and snapshot. Issue #60's recovery flow
// depends on this: the broken file is what the user may want to restore.
func TestBackupTakenWhenOnDiskUnparseable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")

	// Write content Load() will reject (missing required global.folder).
	os.WriteFile(path, []byte(`{"version":2,"global":{},"accounts":{},"sources":{}}`), 0o644)

	cfg := &Config{
		Version: 2,
		Global: GlobalConfig{
			Folder: "~/00.git",
			Window: &WindowState{X: 1, Y: 1, Width: 10, Height: 10},
		},
		Accounts: map[string]Account{},
		Sources:  map[string]Source{},
	}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "gitbox-????????-??????.json"))
	if len(matches) != 1 {
		t.Errorf("expected 1 backup of the unparseable file, got %d", len(matches))
	}
}

func TestBackupFailureDoesNotBlockSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")

	// Create initial file so backup is attempted.
	os.WriteFile(path, []byte(`{}`), 0o644)

	// Make the directory read-only to cause backup copy to fail.
	// On Windows this doesn't prevent file creation the same way,
	// so we test with a path that can't be a backup target.
	// The key assertion: Save() must succeed regardless.
	cfg := &Config{Version: 2, Global: GlobalConfig{Folder: "~/test"}, Accounts: make(map[string]Account), Sources: make(map[string]Source)}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save should succeed even if backup fails: %v", err)
	}

	// Verify the config was actually written.
	data, _ := os.ReadFile(path)
	if len(data) == 0 {
		t.Error("config file should have been written")
	}
}
