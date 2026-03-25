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
		got := EnvVarName(tt.accountKey, nil)
		if got != tt.want {
			t.Errorf("EnvVarName(%q, nil) = %q, want %q", tt.accountKey, got, tt.want)
		}
	}
}

func TestEnvVarName_CustomOverride(t *testing.T) {
	tokenCfg := &config.TokenConfig{EnvVar: "MY_CUSTOM_TOKEN"}
	got := EnvVarName("git-example", tokenCfg)
	if got != "MY_CUSTOM_TOKEN" {
		t.Errorf("EnvVarName with custom = %q, want %q", got, "MY_CUSTOM_TOKEN")
	}
}

func TestEnvVarName_EmptyCustom(t *testing.T) {
	tokenCfg := &config.TokenConfig{EnvVar: ""}
	got := EnvVarName("git-example", tokenCfg)
	if got != "GITBOX_TOKEN_GIT_EXAMPLE" {
		t.Errorf("EnvVarName with empty custom = %q, want default", got)
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

func TestResolveToken_FromCustomEnvVar(t *testing.T) {
	acct := config.Account{
		URL:      "https://git.example.org",
		Username: "myuser",
		Token:    &config.TokenConfig{EnvVar: "MY_FORGEJO_PAT"},
	}

	t.Setenv("MY_FORGEJO_PAT", "custom-token-value")

	token, source, err := ResolveToken(acct, "test-acct")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "custom-token-value" {
		t.Errorf("token = %q, want %q", token, "custom-token-value")
	}
	if source != "environment variable MY_FORGEJO_PAT" {
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

