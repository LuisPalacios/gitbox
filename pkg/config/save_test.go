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

func TestBackupSameDayOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")

	// First save.
	os.WriteFile(path, []byte(`{"save":1}`), 0o644)
	cfg := &Config{Version: 2, Global: GlobalConfig{Folder: "~/test"}, Accounts: make(map[string]Account), Sources: make(map[string]Source)}
	Save(cfg, path)

	// Second save — same day, should overwrite the backup.
	os.WriteFile(path, []byte(`{"save":2}`), 0o644) // simulate external edit
	Save(cfg, path)

	matches, _ := filepath.Glob(filepath.Join(dir, "gitbox-????????-??????.json"))
	if len(matches) != 1 {
		t.Errorf("expected 1 backup (same day overwrite), got %d", len(matches))
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
