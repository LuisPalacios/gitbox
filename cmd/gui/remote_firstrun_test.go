package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// firstRunHost is a host with no ~/.config/gitbox/ at all, served by the
// same code gitbox-server runs (#349). It returns the link, the host's home
// and the config path the server uses.
func firstRunHost(t *testing.T) (*remoteLink, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	cfgPath := filepath.Join(home, ".config", "gitbox", "gitbox.json")
	return serveRemote(t, cfgPath, nil), home, cfgPath
}

// suggestedRoot is the first-run preview of the default root, which the
// onboarding fills in; it must resolve on the host, under its home.
func suggestedRoot(t *testing.T, link *remoteLink, home string) OnboardingRootPreviewDTO {
	t.Helper()
	if !link.Remote.IsFirstRun() {
		t.Fatalf("a host without a config is not a first run: %v", link.Remote.LastError())
	}
	preview, err := link.Remote.PreviewRoot("")
	if err != nil {
		t.Fatalf("PreviewRoot: %v", err)
	}
	if want := filepath.Join(home, "00.git"); preview.Abs != want {
		t.Errorf("the suggested root resolved to %q, want %q on the host", preview.Abs, want)
	}
	if !preview.Writable || preview.Exists {
		t.Errorf("preview = %+v, want a writable folder that does not exist yet", preview)
	}
	return preview
}

func assertNoConfigDir(t *testing.T, home string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(home, ".config", "gitbox")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the host has a ~/.config/gitbox/ before onboarding saved anything: %v", err)
	}
}

// TestRemoteFirstRunDisconnectLeavesNothing: the onboarding reads the host
// until the folder step saves, so a window that disconnects before then
// leaves no config, and no config folder, on the host.
func TestRemoteFirstRunDisconnectLeavesNothing(t *testing.T) {
	link, home, _ := firstRunHost(t)
	suggestedRoot(t, link, home)
	assertNoConfigDir(t, home)

	link.Remote.Close()
	select {
	case <-link.served:
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not stop after the client disconnected")
	}
	assertNoConfigDir(t, home)
	if _, err := os.Lstat(filepath.Join(home, "00.git")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the root was created before onboarding saved it: %v", err)
	}
}

// TestRemoteFirstRunSavesOnTheHost: the folder step creates the root and a
// version 5 gitbox.json on the host, with its missing parent folders, and
// the same connection then serves the host's data.
func TestRemoteFirstRunSavesOnTheHost(t *testing.T) {
	link, home, cfgPath := firstRunHost(t)
	preview := suggestedRoot(t, link, home)

	if err := link.Remote.InitRoot(preview.Abs); err != nil {
		t.Fatalf("InitRoot: %v", err)
	}
	cfg := loadConfig(t, cfgPath)
	if cfg.Version != config.CurrentVersion || config.CurrentVersion != 5 {
		t.Errorf("the host's config has version %d, want 5", cfg.Version)
	}
	if !slices.Equal(cfg.Global.Roots, []string{preview.Abs}) {
		t.Errorf("the host's roots = %v, want [%s]", cfg.Global.Roots, preview.Abs)
	}
	if info, err := os.Stat(preview.Abs); err != nil || !info.IsDir() {
		t.Errorf("the root folder is not on the host: %v", err)
	}
	if link.Remote.IsFirstRun() {
		t.Error("the host is still a first run after onboarding saved its root")
	}
	snap, err := link.Remote.GetStartupSnapshot()
	if err != nil {
		t.Fatalf("GetStartupSnapshot: %v", err)
	}
	if !slices.Equal(snap.Config.Global.Roots, []string{preview.Abs}) {
		t.Errorf("the window loads roots %v, want [%s]", snap.Config.Global.Roots, preview.Abs)
	}
}
