package main

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestUIMode_DefaultsToAdvanced(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)
	if got := a.GetUIMode(); got != "advanced" {
		t.Errorf("GetUIMode() = %q, want advanced for a config without ui_mode", got)
	}
}

func TestSetUIMode_PersistsAndValidates(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)

	if err := a.SetUIMode("simple"); err != nil {
		t.Fatalf("SetUIMode: %v", err)
	}
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Global.UIMode != "simple" {
		t.Errorf("persisted ui_mode = %q, want simple", reloaded.Global.UIMode)
	}
	if got := a.GetUIMode(); got != "simple" {
		t.Errorf("GetUIMode() = %q, want simple", got)
	}

	if err := a.SetUIMode("expert"); err == nil {
		t.Error("SetUIMode(expert) = nil, want an error")
	}
	if got := a.GetUIMode(); got != "simple" {
		t.Errorf("GetUIMode() after a rejected value = %q, want simple", got)
	}
}

func TestSetTheme_PersistsAndValidates(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)

	for _, tc := range []struct{ choice, stored string }{
		{"dark", "dark"},
		{"light", "light"},
		{"system", ""},
	} {
		if err := a.SetTheme(tc.choice); err != nil {
			t.Fatalf("SetTheme(%s): %v", tc.choice, err)
		}
		reloaded, err := config.Load(cfgPath)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if reloaded.Global.Theme != tc.stored {
			t.Errorf("SetTheme(%s): persisted theme = %q, want %q", tc.choice, reloaded.Global.Theme, tc.stored)
		}
	}

	if err := a.SetTheme("dark"); err != nil {
		t.Fatalf("SetTheme(dark): %v", err)
	}
	if err := a.SetTheme("sepia"); err == nil {
		t.Error("SetTheme(sepia) = nil, want an error")
	}
	if a.cfg.Global.Theme != "dark" {
		t.Errorf("theme after a rejected value = %q, want dark", a.cfg.Global.Theme)
	}
}

// Onboarding finishes in SetGlobalFolder: a new install starts in Simple,
// while changing the folder later never touches the chosen mode.
func TestSetGlobalFolder_NewInstallStartsSimple(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "gitbox.json")
	a := &App{
		cfg:     &config.Config{Version: config.CurrentVersion, Accounts: map[string]config.Account{}, Sources: map[string]config.Source{}},
		cfgPath: cfgPath,
		mu:      sync.Mutex{},
	}

	if err := a.SetGlobalFolder(filepath.Join(dir, "projects")); err != nil {
		t.Fatalf("SetGlobalFolder: %v", err)
	}
	if got := a.GetUIMode(); got != "simple" {
		t.Errorf("GetUIMode() after onboarding = %q, want simple", got)
	}

	if err := a.SetUIMode("advanced"); err != nil {
		t.Fatalf("SetUIMode: %v", err)
	}
	if err := a.SetGlobalFolder(filepath.Join(dir, "elsewhere")); err != nil {
		t.Fatalf("SetGlobalFolder again: %v", err)
	}
	if got := a.GetUIMode(); got != "advanced" {
		t.Errorf("GetUIMode() after changing the folder = %q, want advanced", got)
	}
}

func TestSetGlobalFolder_ExistingConfigKeepsAdvanced(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)
	if err := a.SetGlobalFolder(filepath.Join(t.TempDir(), "moved")); err != nil {
		t.Fatalf("SetGlobalFolder: %v", err)
	}
	if got := a.GetUIMode(); got != "advanced" {
		t.Errorf("GetUIMode() = %q, want advanced for an existing config", got)
	}
}
