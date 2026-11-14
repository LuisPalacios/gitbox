package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListConfigBackups_SameSecondSnapshots(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"gitbox-20260401-120000.json", "gitbox-20260401-120000-1.json", "gitbox-20260401-120000-9.json", ".gitbox-staging.tmp"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{cfgPath: filepath.Join(dir, "gitbox.json")}
	backups := a.ListConfigBackups()
	if len(backups) != 3 {
		t.Fatalf("backups = %v", backups)
	}
	for i, name := range []string{"gitbox-20260401-120000-9.json", "gitbox-20260401-120000-1.json", "gitbox-20260401-120000.json"} {
		backup := backups[i]
		if backup.Filename != name || backup.Path != filepath.Join(dir, name) || backup.Timestamp != "2026-04-01T12:00:00Z" || backup.SizeBytes != 2 {
			t.Errorf("backup %d = %+v", i, backup)
		}
		if data, err := os.ReadFile(backup.Path); err != nil || string(data) != "{}" {
			t.Fatalf("backup contents = %q, %v", data, err)
		}
	}
}
