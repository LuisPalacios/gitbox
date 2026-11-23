package credential

import (
	"os"
	"path/filepath"
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

// TestGetTokenReadsTheLineGitRewrites: once git approves a credential, its
// store helper rewrites the line with the port's colon escaped. The token
// still reads back, not the whole line (#242).
func TestGetTokenReadsTheLineGitRewrites(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, tc := range []struct{ line, want string }{
		{"http://alice:tok-123@127.0.0.1%3a3000\n", "tok-123"},
		{"https://alice:tok%2F45@forge.example.invalid%3a8443\n", "tok/45"},
		{"https://alice:tok-678@forge.example.invalid\n", "tok-678"},
		{"https://alice:tok-9@forge.example.invalid:8443\nhttps://bob:other@forge.example.invalid:8443\n", "tok-9"},
		{"raw-token-value\n", "raw-token-value"},
	} {
		path := CredentialFilePath("acct")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(tc.line), 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := GetToken("acct"); err != nil || got != tc.want {
			t.Errorf("GetToken over %q = %q, %v; want %q", tc.line, got, err, tc.want)
		}
	}
}
