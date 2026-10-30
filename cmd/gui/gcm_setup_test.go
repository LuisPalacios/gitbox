package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// gcmSetupTestApp builds an App whose only credential helper is a fake GCM in
// an isolated global gitconfig. The fake writes to stderr during an
// interactive fill, as the real one can, and fails it when FAKE_GCM_FAIL is
// set. os.Stderr is closed for the test: a GUI launched without a console has
// no valid stderr handle, so any write to it fails.
func gcmSetupTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	// Without this, git opens a real password dialog when the helper fails.
	askpass := filepath.Join(dir, "askpass")
	if err := os.WriteFile(askpass, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_ASKPASS", filepath.ToSlash(askpass))

	stored := filepath.ToSlash(filepath.Join(dir, "stored"))
	helper := filepath.Join(dir, "git-credential-manager")
	script := fmt.Sprintf(`#!/bin/sh
cat >/dev/null
creds() { printf 'username=alice\npassword=secret\n'; }
case "$1" in
get)
	if [ -f '%[1]s' ]; then creds; exit 0; fi
	[ "$GCM_INTERACTIVE" = never ] && exit 0
	echo 'fake-gcm: opening the browser' >&2
	[ -n "$FAKE_GCM_FAIL" ] && exit 1
	creds
	;;
store) : > '%[1]s' ;;
esac
`, stored)
	if err := os.WriteFile(helper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	gitconfig := filepath.Join(dir, "gitconfig")
	body := fmt.Sprintf("[credential]\n\thelper = %s\n", filepath.ToSlash(helper))
	if err := os.WriteFile(gitconfig, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)

	closed, err := os.CreateTemp(dir, "stderr")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = closed
	t.Cleanup(func() { os.Stderr = saved })

	cfg := &config.Config{
		Version: config.CurrentVersion,
		Global:  config.GlobalConfig{Roots: []string{dir}},
		Accounts: map[string]config.Account{
			"github-alice": {Provider: "github", URL: "https://github.com",
				Username: "alice", Name: "Alice", Email: "a@e", DefaultCredentialType: "gcm"},
		},
		Sources: map[string]config.Source{},
	}
	return &App{cfg: cfg, cfgPath: filepath.Join(dir, "gitbox.json"), mu: sync.Mutex{}}
}

// GCM writing to stderr must not fail the sign-in when the GUI has no stderr.
func TestCredentialSetupGCMSurvivesInvalidStderr(t *testing.T) {
	a := gcmSetupTestApp(t)
	r := a.CredentialSetupGCM("github-alice")
	if !r.OK {
		t.Fatalf("CredentialSetupGCM failed: %s", r.Message)
	}
}

// A real failure reports what GCM said, not the failed write to stderr.
func TestCredentialSetupGCMReportsHelperStderr(t *testing.T) {
	a := gcmSetupTestApp(t)
	t.Setenv("FAKE_GCM_FAIL", "1")
	r := a.CredentialSetupGCM("github-alice")
	if r.OK {
		t.Fatalf("CredentialSetupGCM succeeded, want a failure: %s", r.Message)
	}
	if !strings.Contains(r.Message, "fake-gcm: opening the browser") {
		t.Errorf("Message = %q, want the helper's stderr", r.Message)
	}
}
