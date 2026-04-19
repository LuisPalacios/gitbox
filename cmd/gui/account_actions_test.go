package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestResolveAccountFolder(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder: dir,
		},
		Accounts: map[string]config.Account{
			"github-alice": {Provider: "github", URL: "https://github.com",
				Username: "alice", Name: "Alice", Email: "a@e"},
		},
		Sources: map[string]config.Source{},
	}
	a := &App{cfg: cfg, cfgPath: filepath.Join(dir, "gitbox.json"), mu: sync.Mutex{}}

	t.Run("unknown account errors", func(t *testing.T) {
		if _, err := a.resolveAccountFolder("nope"); err == nil || !strings.Contains(err.Error(), "not found") {
			t.Errorf("expected 'not found' error, got %v", err)
		}
	})

	t.Run("missing folder errors", func(t *testing.T) {
		_, err := a.resolveAccountFolder("github-alice")
		if err == nil {
			t.Fatal("expected error for missing folder, got nil")
		}
		if !strings.Contains(err.Error(), "does not exist") {
			t.Errorf("expected 'does not exist' in error, got %v", err)
		}
	})

	t.Run("existing folder returns path", func(t *testing.T) {
		want := filepath.Join(dir, "github-alice")
		if err := os.MkdirAll(want, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		got, err := a.resolveAccountFolder("github-alice")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("path exists but is a file errors", func(t *testing.T) {
		cfg.Accounts["file-acct"] = config.Account{Provider: "github",
			URL: "https://github.com", Username: "f", Name: "F", Email: "f@e"}
		filePath := filepath.Join(dir, "file-acct")
		if err := os.WriteFile(filePath, []byte("not a dir"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
		if _, err := a.resolveAccountFolder("file-acct"); err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Errorf("expected 'not a directory' error, got %v", err)
		}
	})
}

func TestOpenAccountInBrowser_UnknownAccount(t *testing.T) {
	cfg := &config.Config{
		Version:  2,
		Global:   config.GlobalConfig{Folder: t.TempDir()},
		Accounts: map[string]config.Account{},
	}
	a := &App{cfg: cfg, cfgPath: "", mu: sync.Mutex{}}
	err := a.OpenAccountInBrowser("nope")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got %v", err)
	}
}
