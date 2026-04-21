package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadWithRepair_DanglingMirror covers the issue #60 scenario: a mirror
// whose account_src points to a now-deleted account. Strict Load must reject
// it; LoadWithRepair must drop the mirror and report a single repair.
func TestLoadWithRepair_DanglingMirror(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")
	content := `{
    "version": 2,
    "global": {"folder": "~/00.git"},
    "accounts": {
        "a1": {"provider": "github", "url": "https://github.com", "username": "u", "name": "N", "email": "e@e"},
        "a2": {"provider": "github", "url": "https://github.com", "username": "u", "name": "N", "email": "e@e"}
    },
    "sources": {
        "a1": {"account": "a1", "repos": {}}
    },
    "mirrors": {
        "m1": {"account_src": "ghost", "account_dst": "a2", "repos": {}}
    }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("strict Load should have rejected dangling mirror")
	}

	cfg, repairs, err := LoadWithRepair(path)
	if err != nil {
		t.Fatalf("LoadWithRepair: %v", err)
	}
	if len(repairs) != 1 {
		t.Fatalf("repairs = %d, want 1: %+v", len(repairs), repairs)
	}
	if repairs[0].Kind != "dangling_mirror" || repairs[0].Subject != "m1" {
		t.Errorf("repair = %+v, want kind=dangling_mirror subject=m1", repairs[0])
	}
	if _, ok := cfg.Mirrors["m1"]; ok {
		t.Error("dangling mirror m1 should have been dropped")
	}
	if len(cfg.Accounts) != 2 {
		t.Errorf("accounts = %d, want 2", len(cfg.Accounts))
	}
}

func TestLoadWithRepair_DanglingBoth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")
	content := `{
    "version": 2,
    "global": {"folder": "~/00.git"},
    "accounts": {
        "a1": {"provider": "github", "url": "https://github.com", "username": "u", "name": "N", "email": "e@e"}
    },
    "sources": {
        "a1": {"account": "a1", "repos": {}}
    },
    "mirrors": {
        "m1": {"account_src": "ghost1", "account_dst": "ghost2", "repos": {}}
    }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, repairs, err := LoadWithRepair(path)
	if err != nil {
		t.Fatalf("LoadWithRepair: %v", err)
	}
	if len(repairs) != 1 {
		t.Fatalf("repairs = %d, want 1", len(repairs))
	}
	// Repair detail should name both missing accounts so the user can see
	// why the entry was dropped.
	if !strings.Contains(repairs[0].Detail, "ghost1") || !strings.Contains(repairs[0].Detail, "ghost2") {
		t.Errorf("detail %q should mention both missing accounts", repairs[0].Detail)
	}
}

func TestLoadWithRepair_NoRepairNeeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")
	content := `{
    "version": 2,
    "global": {"folder": "~/00.git"},
    "accounts": {
        "a1": {"provider": "github", "url": "https://github.com", "username": "u", "name": "N", "email": "e@e"}
    },
    "sources": {
        "a1": {"account": "a1", "repos": {}}
    }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, repairs, err := LoadWithRepair(path)
	if err != nil {
		t.Fatalf("LoadWithRepair: %v", err)
	}
	if len(repairs) != 0 {
		t.Errorf("repairs = %d, want 0", len(repairs))
	}
	if len(cfg.Accounts) != 1 {
		t.Errorf("accounts = %d, want 1", len(cfg.Accounts))
	}
}

func TestLoadWithRepair_UnrecoverableError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")
	// Missing global.folder is a strict validation failure and not a repair
	// target — LoadWithRepair must surface the error.
	content := `{
    "version": 2,
    "global": {},
    "accounts": {},
    "sources": {}
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, err := LoadWithRepair(path)
	if err == nil {
		t.Fatal("expected error for missing global.folder")
	}
	if !strings.Contains(err.Error(), "folder") {
		t.Errorf("error = %v, want folder mention", err)
	}
}

func TestLoadWithRepair_DanglingSourceNotRepaired(t *testing.T) {
	// A source pointing to an unknown account is deliberately NOT auto-repaired
	// — dropping the source would silently discard clone-folder configuration.
	// Surface the error so the user decides.
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")
	content := `{
    "version": 2,
    "global": {"folder": "~/00.git"},
    "accounts": {
        "a1": {"provider": "github", "url": "https://github.com", "username": "u", "name": "N", "email": "e@e"}
    },
    "sources": {
        "ghost-src": {"account": "ghost", "repos": {}}
    }
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _, err := LoadWithRepair(path)
	if err == nil {
		t.Fatal("expected error for dangling source ref")
	}
}
