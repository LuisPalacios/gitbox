package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// ---------------------------------------------------------------------------
// Integration tests — require test-gitbox.json with real account credentials.
// Self-skip when test-gitbox.json is missing or -short is set.
// ---------------------------------------------------------------------------

func TestIntegration_CLI_CredentialVerify(t *testing.T) {
	fixture := requireCLIIntegration(t)

	ghKey, _, _, ok := fixture.firstAccountWithRepos()
	if !ok {
		t.Skip("no account with repos and token in test fixture")
	}

	env := setupCLIEnv(t)
	cfg := newCLIIntegrationConfig(env.GitFolder, fixture)
	cfg.Accounts[ghKey] = fixture.Config.Accounts[ghKey]
	config.Save(cfg, env.CfgPath)

	result := env.run(t, "account", "credential", "verify", ghKey)
	if result.ExitCode != 0 {
		t.Fatalf("credential verify failed (exit %d): %s\nstdout: %s", result.ExitCode, result.Stderr, result.Stdout)
	}
}

func TestIntegration_CLI_Discover(t *testing.T) {
	fixture := requireCLIIntegration(t)

	ghKey, _, _, ok := fixture.firstAccountWithRepos()
	if !ok {
		t.Skip("no account with repos and token in test fixture")
	}

	env := setupCLIEnv(t)
	cfg := newCLIIntegrationConfig(env.GitFolder, fixture)
	cfg.Accounts[ghKey] = fixture.Config.Accounts[ghKey]
	cfg.Sources[ghKey] = config.Source{Account: ghKey, Repos: map[string]config.Repo{}}
	config.Save(cfg, env.CfgPath)

	var discovered map[string]any
	result := env.runJSON(t, &discovered, "account", "discover", ghKey)
	if result.ExitCode != 0 {
		t.Fatalf("discover failed (exit %d): %s", result.ExitCode, result.Stderr)
	}
	if disc, ok := discovered["discovered"]; ok {
		if arr, ok := disc.([]any); ok {
			t.Logf("discovered %d repos", len(arr))
		}
	}
}

func TestIntegration_CLI_Clone(t *testing.T) {
	fixture := requireCLIIntegration(t)

	ghKey, srcKey, repo, ok := fixture.firstAccountWithRepos()
	if !ok {
		t.Skip("no account with repos and token in test fixture")
	}
	acct := fixture.Config.Accounts[ghKey]

	env := setupCLIEnv(t)
	cfg := newCLIIntegrationConfig(env.GitFolder, fixture)
	cfg.Accounts[ghKey] = acct
	cfg.Sources[srcKey] = config.Source{
		Account: ghKey,
		Repos:   map[string]config.Repo{repo: {}},
	}
	config.Save(cfg, env.CfgPath)

	result := env.run(t, "clone")
	if result.ExitCode != 0 {
		t.Fatalf("clone failed (exit %d): %s\nstdout: %s", result.ExitCode, result.Stderr, result.Stdout)
	}

	// External verification: clone should exist on disk.
	parts := strings.SplitN(repo, "/", 2)
	repoPath := filepath.Join(env.GitFolder, srcKey, parts[0], parts[1])
	gitDir := filepath.Join(repoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Errorf("expected clone at %s (no .git found)", repoPath)
	}

	// Verify git identity was set.
	if nameOut := gitConfigValue(t, repoPath, "user.name"); nameOut != acct.Name {
		t.Errorf("user.name: got %q, want %q", nameOut, acct.Name)
	}
	if emailOut := gitConfigValue(t, repoPath, "user.email"); emailOut != acct.Email {
		t.Errorf("user.email: got %q, want %q", emailOut, acct.Email)
	}
}

func TestIntegration_CLI_Status(t *testing.T) {
	fixture := requireCLIIntegration(t)

	ghKey, srcKey, repo, ok := fixture.firstAccountWithRepos()
	if !ok {
		t.Skip("no account with repos and token in test fixture")
	}

	env := setupCLIEnv(t)
	cfg := newCLIIntegrationConfig(env.GitFolder, fixture)
	cfg.Accounts[ghKey] = fixture.Config.Accounts[ghKey]
	cfg.Sources[srcKey] = config.Source{
		Account: ghKey,
		Repos:   map[string]config.Repo{repo: {}},
	}
	config.Save(cfg, env.CfgPath)

	env.run(t, "clone")

	var statuses []map[string]any
	result := env.runJSON(t, &statuses, "status")
	if result.ExitCode != 0 {
		t.Fatalf("status failed: %s", result.Stderr)
	}
	if len(statuses) == 0 {
		t.Fatal("expected at least 1 status entry")
	}
}

func TestIntegration_CLI_Pull(t *testing.T) {
	fixture := requireCLIIntegration(t)

	ghKey, srcKey, repo, ok := fixture.firstAccountWithRepos()
	if !ok {
		t.Skip("no account with repos and token in test fixture")
	}

	env := setupCLIEnv(t)
	cfg := newCLIIntegrationConfig(env.GitFolder, fixture)
	cfg.Accounts[ghKey] = fixture.Config.Accounts[ghKey]
	cfg.Sources[srcKey] = config.Source{
		Account: ghKey,
		Repos:   map[string]config.Repo{repo: {}},
	}
	config.Save(cfg, env.CfgPath)

	env.run(t, "clone")
	result := env.run(t, "pull")
	if result.ExitCode != 0 {
		t.Fatalf("pull failed (exit %d): %s", result.ExitCode, result.Stderr)
	}
}

func TestIntegration_CLI_Fetch(t *testing.T) {
	fixture := requireCLIIntegration(t)

	ghKey, srcKey, repo, ok := fixture.firstAccountWithRepos()
	if !ok {
		t.Skip("no account with repos and token in test fixture")
	}

	env := setupCLIEnv(t)
	cfg := newCLIIntegrationConfig(env.GitFolder, fixture)
	cfg.Accounts[ghKey] = fixture.Config.Accounts[ghKey]
	cfg.Sources[srcKey] = config.Source{
		Account: ghKey,
		Repos:   map[string]config.Repo{repo: {}},
	}
	config.Save(cfg, env.CfgPath)

	env.run(t, "clone")
	result := env.run(t, "fetch")
	if result.ExitCode != 0 {
		t.Fatalf("fetch failed (exit %d): %s", result.ExitCode, result.Stderr)
	}
}

// ---------------------------------------------------------------------------
// Git helper
// ---------------------------------------------------------------------------

func gitConfigValue(t *testing.T, repoPath, key string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoPath, "config", key).Output()
	if err != nil {
		t.Fatalf("git config %s in %s: %v", key, repoPath, err)
	}
	return strings.TrimSpace(string(out))
}
