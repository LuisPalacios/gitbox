package credential

import (
	"runtime"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func TestEnvVarName_Default(t *testing.T) {
	tests := []struct {
		accountKey string
		want       string
	}{
		{"git-example", "GITBOX_TOKEN_GIT_EXAMPLE"},
		{"github-MyGitHubUser", "GITBOX_TOKEN_GITHUB_MYGITHUBUSER"},
		{"github-myorg", "GITBOX_TOKEN_GITHUB_MYORG"},
		{"my.server", "GITBOX_TOKEN_MY_SERVER"},
		{"simple", "GITBOX_TOKEN_SIMPLE"},
	}

	for _, tt := range tests {
		got := EnvVarName(tt.accountKey)
		if got != tt.want {
			t.Errorf("EnvVarName(%q) = %q, want %q", tt.accountKey, got, tt.want)
		}
	}
}

func TestResolveToken_FromEnvVar(t *testing.T) {
	acct := config.Account{
		URL:      "https://git.example.org",
		Username: "myuser",
	}

	t.Setenv("GITBOX_TOKEN_TEST_ACCT", "my-secret-token")

	token, source, err := ResolveToken(acct, "test-acct")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "my-secret-token" {
		t.Errorf("token = %q, want %q", token, "my-secret-token")
	}
	if source != "environment variable GITBOX_TOKEN_TEST_ACCT" {
		t.Errorf("source = %q, unexpected", source)
	}
}

func TestResolveToken_FromGitToken(t *testing.T) {
	acct := config.Account{
		URL:      "https://git.example.org",
		Username: "myuser",
	}

	t.Setenv("GIT_TOKEN", "generic-ci-token")

	token, source, err := ResolveToken(acct, "no-specific-env")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "generic-ci-token" {
		t.Errorf("token = %q, want %q", token, "generic-ci-token")
	}
	if source != "environment variable GIT_TOKEN" {
		t.Errorf("source = %q, unexpected", source)
	}
}

// ---------------------------------------------------------------------------
// CanOpenBrowser
// ---------------------------------------------------------------------------

func TestCanOpenBrowser_NoSSH(t *testing.T) {
	// Clear SSH env vars to simulate a local desktop session.
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("SSH_TTY", "")

	if !CanOpenBrowser() {
		t.Error("CanOpenBrowser() = false on local session, want true")
	}
}

func TestCanOpenBrowser_SSHWithDisplay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SSH env vars have no effect on Windows (always returns true)")
	}

	t.Setenv("SSH_CLIENT", "192.168.1.1 12345 22")
	t.Setenv("DISPLAY", ":0")
	t.Setenv("WAYLAND_DISPLAY", "")

	// macOS: always true even via SSH (can use `open`).
	// Linux with DISPLAY: true (X11 forwarding).
	if !CanOpenBrowser() {
		t.Error("CanOpenBrowser() = false with SSH_CLIENT+DISPLAY set, want true")
	}
}

func TestCanOpenBrowser_SSHNoDisplay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SSH env vars have no effect on Windows (always returns true)")
	}

	t.Setenv("SSH_CLIENT", "192.168.1.1 12345 22")
	t.Setenv("SSH_TTY", "/dev/pts/0")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	got := CanOpenBrowser()
	// macOS SSH without display can still open browser via `open` command.
	if runtime.GOOS == "darwin" {
		if !got {
			t.Error("CanOpenBrowser() = false on macOS SSH, want true (macOS can use `open`)")
		}
	} else {
		// Linux SSH without display server → can't open browser.
		if got {
			t.Error("CanOpenBrowser() = true on Linux SSH without display, want false")
		}
	}
}

// ---------------------------------------------------------------------------
// EnsureGlobalGCMConfig
// ---------------------------------------------------------------------------

func TestEnsureGlobalGCMConfig_NilGCM(t *testing.T) {
	// Should not panic with nil CredentialGCM.
	EnsureGlobalGCMConfig(config.GlobalConfig{CredentialGCM: nil})
}

func TestEnsureGlobalGCMConfig_EmptyStrings(t *testing.T) {
	// Should not panic with empty strings (no-op).
	EnsureGlobalGCMConfig(config.GlobalConfig{
		CredentialGCM: &config.GCMGlobal{
			Helper:          "",
			CredentialStore: "",
		},
	})
}

