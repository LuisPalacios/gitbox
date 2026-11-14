package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBackupCreatedOnSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")

	// Create an initial config file with known content.
	os.WriteFile(path, []byte(`{"version":2,"original":true}`), 0o644)

	cfg := &Config{
		Version:  CurrentVersion,
		Global:   GlobalConfig{Roots: []string{"~/test"}},
		Accounts: make(map[string]Account),
		Sources:  make(map[string]Source),
	}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A dated backup should exist with the original content.
	matches, _ := filepath.Glob(filepath.Join(dir, "gitbox-????????-??????*.json"))
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
		Version:  CurrentVersion,
		Global:   GlobalConfig{Roots: []string{"~/test"}},
		Accounts: make(map[string]Account),
		Sources:  make(map[string]Source),
	}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// No backup should exist — file didn't exist before save.
	matches, _ := filepath.Glob(filepath.Join(dir, "gitbox-????????-??????*.json"))
	if len(matches) != 0 {
		t.Errorf("expected 0 backups on first save, got %d", len(matches))
	}
}

func TestBackupPruning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")
	files := fixedConfigFiles()
	previous := files.now().Add(-time.Second)
	for i := range maxBackups {
		name := "gitbox-" + previous.Format("20060102-150405")
		if i > 0 {
			name += fmt.Sprintf("-%d", i)
		}
		writeSaveFixture(t, filepath.Join(dir, name+".json"), []byte(fmt.Sprint(i)))
	}
	writeSaveFixture(t, path, []byte(`{}`))
	if err := saveWithFiles(saveTestConfig(), path, files); err != nil {
		t.Fatal(err)
	}
	backups, err := ListBackups(path)
	if err != nil || len(backups) != maxBackups {
		t.Fatalf("backups = %v, error = %v", backups, err)
	}
	for i, backup := range backups {
		want := "{}"
		if i > 0 {
			want = fmt.Sprint(maxBackups - i)
		}
		assertSaveBytes(t, backup.Path, []byte(want))
	}
	oldest := filepath.Join(dir, "gitbox-"+previous.Format("20060102-150405")+".json")
	if _, err := os.Stat(oldest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oldest unsuffixed backup still exists: %v", err)
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
		Version: CurrentVersion,
		Global: GlobalConfig{
			Roots:  []string{"~/00.git"},
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
	matches, _ := filepath.Glob(filepath.Join(dir, "gitbox-????????-??????*.json"))
	if len(matches) != 0 {
		t.Fatalf("first save should not create a backup (no prior file), got %d", len(matches))
	}

	// Change only window position — must NOT create a backup.
	cfg.Global.Window.X = 500
	cfg.Global.Window.Y = 250
	if err := Save(cfg, path); err != nil {
		t.Fatalf("window-only Save: %v", err)
	}
	matches, _ = filepath.Glob(filepath.Join(dir, "gitbox-????????-??????*.json"))
	if len(matches) != 0 {
		t.Errorf("window-only change should not create a backup, got %d", len(matches))
	}

	// Add a compact_window entry only — still no backup.
	cfg.Global.CompactWindow = &WindowState{X: 50, Y: 50, Width: 220, Height: 400}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("compact-window-only Save: %v", err)
	}
	matches, _ = filepath.Glob(filepath.Join(dir, "gitbox-????????-??????*.json"))
	if len(matches) != 0 {
		t.Errorf("compact-window-only change should not create a backup, got %d", len(matches))
	}

	// Cycle the theme only — still no backup, so theme clicks never push
	// real rollback points out of the ring.
	for _, theme := range []string{"light", "dark", ""} {
		cfg.Global.Theme = theme
		if err := Save(cfg, path); err != nil {
			t.Fatalf("theme-only Save (%q): %v", theme, err)
		}
	}
	matches, _ = filepath.Glob(filepath.Join(dir, "gitbox-????????-??????*.json"))
	if len(matches) != 0 {
		t.Errorf("theme-only change should not create a backup, got %d", len(matches))
	}

	// Fold the sidebar and pick a view — layout only, still no backup.
	cfg.Global.Collapsed = &CollapsedState{Sidebar: true, View: "account:a1"}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("layout-only Save: %v", err)
	}
	matches, _ = filepath.Glob(filepath.Join(dir, "gitbox-????????-??????*.json"))
	if len(matches) != 0 {
		t.Errorf("layout-only change should not create a backup, got %d", len(matches))
	}

	// Change real content (periodic_sync) alongside a window update — must
	// create a backup: we need a rollback point for the real change.
	cfg.Global.PeriodicSync = "5m"
	cfg.Global.Window.X = 900
	if err := Save(cfg, path); err != nil {
		t.Fatalf("content Save: %v", err)
	}
	matches, _ = filepath.Glob(filepath.Join(dir, "gitbox-????????-??????*.json"))
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
		Version: CurrentVersion,
		Global: GlobalConfig{
			Roots:  []string{"~/00.git"},
			Window: &WindowState{X: 1, Y: 1, Width: 10, Height: 10},
		},
		Accounts: map[string]Account{},
		Sources:  map[string]Source{},
	}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "gitbox-????????-??????*.json"))
	if len(matches) != 1 {
		t.Errorf("expected 1 backup of the unparseable file, got %d", len(matches))
	}
}

func TestBackupFailureDoesNotBlockSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gitbox.json")
	writeSaveFixture(t, path, []byte(`{}`))
	files := fixedConfigFiles()
	files.link = func(_, _ string) error { return errors.New("hard links unsupported") }
	cfg := saveTestConfig()
	if err := saveWithFiles(cfg, path, files); err != nil {
		t.Fatalf("Save should succeed even if backup fails: %v", err)
	}
	want, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	assertSaveBytes(t, path, want)
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != "gitbox.json" {
		t.Fatalf("failed backup left files: %v, error = %v", entries, err)
	}
}

// TestBackupTakenForOlderVersionOnDisk verifies the first save after an
// upgrade keeps the old-version file even when only window state changed:
// loading migrates it, so it would otherwise compare equal (#149).
func TestBackupTakenForOlderVersionOnDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")
	v3 := `{"version": 3, "global": {"folder": "~/test"}, "accounts": {}, "sources": {}}`
	if err := os.WriteFile(path, []byte(v3), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Global.Window = &WindowState{X: 1, Y: 2, Width: 800, Height: 600}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "gitbox-????????-??????*.json"))
	if len(matches) != 1 {
		t.Fatalf("expected 1 backup of the v3 file, got %d", len(matches))
	}
	if data, _ := os.ReadFile(matches[0]); string(data) != v3 {
		t.Errorf("backup = %s, want the v3 file", data)
	}
}

func saveTestConfig() *Config {
	return &Config{Version: CurrentVersion, Global: GlobalConfig{Roots: []string{"~/test"}}, Accounts: map[string]Account{}, Sources: map[string]Source{}}
}

func fixedConfigFiles() configFileOps {
	files := configFiles()
	files.now = func() time.Time { return time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC) }
	return files
}

func writeSaveFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertSaveBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%s = %q, error = %v; want %q", path, got, err, want)
	}
}

var errSaveInjected = errors.New("injected file failure")

type failingConfigFile struct {
	file      *os.File
	operation string
}

func (f *failingConfigFile) Name() string { return f.file.Name() }

func (f *failingConfigFile) Write(data []byte) (int, error) {
	if f.operation == "write" {
		n, _ := f.file.Write(data[:len(data)/2])
		return n, errSaveInjected
	}
	return f.file.Write(data)
}

func (f *failingConfigFile) Chmod(mode os.FileMode) error {
	if f.operation == "chmod" {
		return errSaveInjected
	}
	return f.file.Chmod(mode)
}

func (f *failingConfigFile) Sync() error {
	if f.operation == "sync" {
		return errSaveInjected
	}
	return f.file.Sync()
}

func (f *failingConfigFile) Close() error {
	err := f.file.Close()
	if f.operation == "close" {
		return errSaveInjected
	}
	return err
}

func TestSave_PreReplaceFailuresPreserveOriginal(t *testing.T) {
	for _, operation := range []string{"create", "write", "chmod", "sync", "close", "rename"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "gitbox.json")
			original := []byte(`{"original":"unchanged"}`)
			writeSaveFixture(t, path, original)
			files := fixedConfigFiles()
			files.createTemp = func(dir, pattern string) (stagedFile, error) {
				if operation == "create" {
					return nil, errSaveInjected
				}
				file, err := os.CreateTemp(dir, pattern)
				if err != nil {
					return nil, err
				}
				return &failingConfigFile{file: file, operation: operation}, nil
			}
			if operation == "rename" {
				files.rename = func(tmp, target string) error {
					data, err := os.ReadFile(tmp)
					if err != nil || !json.Valid(data) {
						t.Fatalf("replacement not ready: %q, %v", data, err)
					}
					return errSaveInjected
				}
			}
			if err := saveWithFiles(saveTestConfig(), path, files); !errors.Is(err, errSaveInjected) {
				t.Fatalf("Save error = %v", err)
			}
			assertSaveBytes(t, path, original)
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".gitbox-") {
					t.Errorf("temporary file left behind: %s", entry.Name())
				}
				assertSaveBytes(t, filepath.Join(filepath.Dir(path), entry.Name()), original)
			}
			if operation == "rename" && len(entries) != 2 {
				t.Fatalf("expected original plus completed backup, got %v", entries)
			}
		})
	}
}

func TestSave_SameSecondBackupsPreserveEveryVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gitbox.json")
	files := fixedConfigFiles()
	cfg := saveTestConfig()
	var versions [][]byte
	for i := range maxBackups + 1 {
		cfg.Global.PeriodicSync = fmt.Sprintf("%dm", i+1)
		if err := saveWithFiles(cfg, path, files); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		versions = append(versions, data)
	}
	backups, err := ListBackups(path)
	if err != nil || len(backups) != maxBackups {
		t.Fatalf("backups = %v, error = %v", backups, err)
	}
	for i, backup := range backups {
		assertSaveBytes(t, backup.Path, versions[maxBackups-1-i])
	}
	// Exhaustion keeps all snapshots intact and still saves the live config.
	cfg.Global.PeriodicSync = "30m"
	if err := saveWithFiles(cfg, path, files); err != nil {
		t.Fatal(err)
	}
	want, _ := Marshal(cfg)
	assertSaveBytes(t, path, want)
	for i, backup := range backups {
		assertSaveBytes(t, backup.Path, versions[maxBackups-1-i])
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != maxBackups+1 {
		t.Fatalf("unexpected leftover files: %v, %v", entries, err)
	}
}

func TestCopyBackup_ConcurrentPublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gitbox.json")
	original := []byte(`{"original":true}`)
	writeSaveFixture(t, path, original)
	files := fixedConfigFiles()
	var wg sync.WaitGroup
	for range maxBackups {
		wg.Go(func() {
			if err := copyBackup(path, files); err != nil {
				t.Errorf("copyBackup: %v", err)
			}
		})
	}
	wg.Wait()
	backups, err := ListBackups(path)
	if err != nil || len(backups) != maxBackups {
		t.Fatalf("backups = %v, error = %v", backups, err)
	}
	for _, backup := range backups {
		assertSaveBytes(t, backup.Path, original)
	}
	assertSaveBytes(t, path, original)
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != maxBackups+1 {
		t.Fatalf("unexpected leftover files: %v, %v", entries, err)
	}
}

func TestListBackups_OnlyRecognizedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gitbox.json")
	for _, name := range []string{"gitbox-20260401-120000.json", "gitbox-20260401-120000-1.json", "gitbox-20260401-120000-9.json", "gitbox-20260402-120000.json", "gitbox-20260401-120000-10.json", "gitbox-20260401-120000-0.json", "gitbox-20269999-120000.json", "gitbox-20260401-120000-copy.json", ".gitbox-123.tmp", "other-20260401-120000.json"} {
		writeSaveFixture(t, filepath.Join(filepath.Dir(path), name), []byte(`{}`))
	}
	if err := os.Mkdir(filepath.Join(filepath.Dir(path), "gitbox-20260403-120000.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	backups, err := ListBackups(path)
	if err != nil || len(backups) != 4 {
		t.Fatalf("backups = %v, error = %v", backups, err)
	}
	for i, want := range []string{"gitbox-20260402-120000.json", "gitbox-20260401-120000-9.json", "gitbox-20260401-120000-1.json", "gitbox-20260401-120000.json"} {
		if filepath.Base(backups[i].Path) != want {
			t.Errorf("backup %d = %s, want %s", i, backups[i].Path, want)
		}
	}
}

func TestSave_PreservesPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support Unix permission bits")
	}
	path := filepath.Join(t.TempDir(), "gitbox.json")
	writeSaveFixture(t, path, []byte(`{}`))
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(saveTestConfig(), path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions changed: %v, %v", info, err)
	}
	backups, err := ListBackups(path)
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups = %v, error = %v", backups, err)
	}
	info, err = os.Stat(backups[0].Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup permissions changed: %v, %v", info, err)
	}
	assertSaveBytes(t, backups[0].Path, []byte(`{}`))
	want, _ := Marshal(saveTestConfig())
	assertSaveBytes(t, path, want)
}

func TestCopyBackup_StagingFailuresLeaveNoSnapshot(t *testing.T) {
	for _, operation := range []string{"write", "chmod", "sync", "close"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "gitbox.json")
			original := []byte(`{"original":true}`)
			writeSaveFixture(t, path, original)
			files := fixedConfigFiles()
			files.createTemp = func(dir, pattern string) (stagedFile, error) {
				file, err := os.CreateTemp(dir, pattern)
				if err != nil {
					return nil, err
				}
				return &failingConfigFile{file: file, operation: operation}, nil
			}
			if err := copyBackup(path, files); !errors.Is(err, errSaveInjected) {
				t.Fatalf("copyBackup error = %v", err)
			}
			assertSaveBytes(t, path, original)
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 1 || entries[0].Name() != "gitbox.json" {
				t.Fatalf("failed backup left files: %v, %v", entries, err)
			}
		})
	}
}

func TestSave_RealRenameFailureCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, "keep")
	writeSaveFixture(t, marker, []byte("unchanged"))
	if err := Save(saveTestConfig(), path); err == nil {
		t.Fatal("Save succeeded over a directory")
	}
	assertSaveBytes(t, marker, []byte("unchanged"))
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "gitbox.json" {
		t.Fatalf("failed save left files: %v, %v", entries, err)
	}
}
