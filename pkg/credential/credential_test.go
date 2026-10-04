package credential

import (
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
