//go:build !gitboxserver

package main

import (
	"errors"
	"github.com/LuisPalacios/gitbox/pkg/update"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The AppImage build is notify-only: ApplyUpdate must refuse before any
// network call or download instead of failing inside the zip extractor.
func TestApplyUpdate_AppImageRefuses(t *testing.T) {
	t.Setenv("APPIMAGE", "/home/me/Applications/gitbox-x86_64.AppImage")
	t.Setenv("APPDIR", "")

	a := &App{}
	err := a.ApplyUpdate()
	if err == nil {
		t.Fatal("ApplyUpdate() inside an AppImage succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "release page") {
		t.Errorf("ApplyUpdate() error = %q, want it to point at the release page", err)
	}
}

func TestUpdateStartupConfirmsOnlyAfterConfigLoads(t *testing.T) {
	install, extract, settings := t.TempDir(), t.TempDir(), t.TempDir()
	name := "GitboxApp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	exe := filepath.Join(install, name)
	if err := os.WriteFile(exe, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extract, name), []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := update.InstallVersion(extract, install, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	r, err := update.BeginRecovery(exe, settings)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{updateRecovery: r, cfgLoadError: "unreadable"}
	if offer, err := app.CheckUpdateRecovery(true); err != nil || offer != "" {
		t.Fatalf("first bridge: %q %v", offer, err)
	}
	r, err = update.BeginRecovery(exe, settings)
	if err != nil {
		t.Fatal(err)
	}
	app = &App{updateRecovery: r, cfgLoaded: true}
	if offer, err := app.CheckUpdateRecovery(true); err != nil || offer != "v1.2.3" {
		t.Fatalf("retry: %q %v", offer, err)
	}
	if err := app.ResolveUpdateRecovery(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(exe + ".update-state.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("kept confirmed record")
	}
}
