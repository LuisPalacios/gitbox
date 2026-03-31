package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// TestScenario_CLI_FullLifecycle runs a complete end-to-end lifecycle test
// exercising the full CLI: global → accounts → sources → repos → clone → status →
// pull → fetch → mirrors → delete everything.
//
// Requires test-gitbox.json with real account credentials.
func TestScenario_CLI_FullLifecycle(t *testing.T) {
	fixture := requireCLIIntegration(t)

	ghKey, srcKey, repo, ok := fixture.firstAccountWithRepos()
	if !ok {
		t.Skip("no account with repos and token in test fixture")
	}
	acct := fixture.Config.Accounts[ghKey]

	env := setupCLIEnv(t)

	// Write a minimal config (simulates post-init state).
	// Use fixture's SSH folder so SSH-based accounts work.
	cfg := newCLIIntegrationConfig(env.GitFolder, fixture)
	config.Save(cfg, env.CfgPath)

	// -----------------------------------------------------------------------
	// Step 1: Global settings
	// -----------------------------------------------------------------------
	t.Run("01_global_update", func(t *testing.T) {
		result := env.run(t, "global", "update", "--folder", env.GitFolder)
		if result.ExitCode != 0 {
			t.Fatalf("global update failed: %s", result.Stderr)
		}
		loaded, _ := config.Load(env.CfgPath)
		if loaded.Global.Folder != env.GitFolder {
			t.Errorf("folder mismatch: %s", loaded.Global.Folder)
		}
	})

	// -----------------------------------------------------------------------
	// Step 2: Add accounts
	// -----------------------------------------------------------------------
	t.Run("02_add_real_account", func(t *testing.T) {
		args := []string{"account", "add", ghKey,
			"--provider", acct.Provider,
			"--url", acct.URL,
			"--username", acct.Username,
			"--name", acct.Name,
			"--email", acct.Email,
			"--default-credential-type", acct.DefaultCredentialType,
		}
		// SSH accounts need extra flags.
		if acct.SSH != nil {
			if acct.SSH.Host != "" {
				args = append(args, "--ssh-host", acct.SSH.Host)
			}
			if acct.SSH.Hostname != "" {
				args = append(args, "--ssh-hostname", acct.SSH.Hostname)
			}
			if acct.SSH.KeyType != "" {
				args = append(args, "--ssh-key-type", acct.SSH.KeyType)
			}
		}
		result := env.run(t, args...)
		if result.ExitCode != 0 {
			t.Fatalf("add account failed: %s", result.Stderr)
		}
		cliAssertConfigHasAccount(t, env.CfgPath, ghKey)
	})

	t.Run("02b_add_dummy_account", func(t *testing.T) {
		result := env.run(t, "account", "add", "dummy-gitlab",
			"--provider", "gitlab",
			"--url", "https://gitlab.com",
			"--username", "demo",
			"--name", "Demo User",
			"--email", "demo@example.com",
			"--default-credential-type", "token",
		)
		if result.ExitCode != 0 {
			t.Fatalf("add dummy account failed: %s", result.Stderr)
		}
		cliAssertConfigHasAccount(t, env.CfgPath, "dummy-gitlab")
	})

	// -----------------------------------------------------------------------
	// Step 3: Credential verify (real account)
	// -----------------------------------------------------------------------
	t.Run("03_credential_verify", func(t *testing.T) {
		result := env.run(t, "account", "credential", "verify", ghKey)
		if result.ExitCode != 0 {
			t.Fatalf("credential verify failed: %s", result.Stderr)
		}
	})

	// -----------------------------------------------------------------------
	// Step 4: Add source
	// -----------------------------------------------------------------------
	t.Run("04_add_source", func(t *testing.T) {
		result := env.run(t, "source", "add", srcKey, "--account", ghKey)
		if result.ExitCode != 0 {
			t.Fatalf("add source failed: %s", result.Stderr)
		}
		cliAssertConfigHasSource(t, env.CfgPath, srcKey)
	})

	// -----------------------------------------------------------------------
	// Step 5: Add repo
	// -----------------------------------------------------------------------
	t.Run("05_add_repo", func(t *testing.T) {
		result := env.run(t, "repo", "add", srcKey, repo)
		if result.ExitCode != 0 {
			t.Fatalf("add repo failed: %s", result.Stderr)
		}
		cliAssertConfigHasRepo(t, env.CfgPath, srcKey, repo)
	})

	// -----------------------------------------------------------------------
	// Step 6: Discover repos
	// -----------------------------------------------------------------------
	t.Run("06_discover", func(t *testing.T) {
		var out map[string]any
		result := env.runJSON(t, &out, "account", "discover", ghKey)
		if result.ExitCode != 0 {
			t.Fatalf("discover failed: %s", result.Stderr)
		}
	})

	// -----------------------------------------------------------------------
	// Step 7: Clone
	// -----------------------------------------------------------------------
	t.Run("07_clone", func(t *testing.T) {
		result := env.run(t, "clone")
		if result.ExitCode != 0 {
			t.Fatalf("clone failed: %s\nstdout: %s", result.Stderr, result.Stdout)
		}

		// External verification.
		parts := strings.SplitN(repo, "/", 2)
		repoPath := filepath.Join(env.GitFolder, srcKey, parts[0], parts[1])
		if _, err := os.Stat(filepath.Join(repoPath, ".git")); os.IsNotExist(err) {
			t.Fatalf("clone not found on disk at %s", repoPath)
		}
		if name := gitConfigValue(t, repoPath, "user.name"); name != acct.Name {
			t.Errorf("user.name mismatch: %s", name)
		}
	})

	// -----------------------------------------------------------------------
	// Step 8: Status
	// -----------------------------------------------------------------------
	t.Run("08_status", func(t *testing.T) {
		var statuses []map[string]any
		result := env.runJSON(t, &statuses, "status")
		if result.ExitCode != 0 {
			t.Fatalf("status failed: %s", result.Stderr)
		}
		if len(statuses) == 0 {
			t.Error("expected at least 1 status entry")
		}
	})

	// -----------------------------------------------------------------------
	// Step 9: Pull (no-op)
	// -----------------------------------------------------------------------
	t.Run("09_pull", func(t *testing.T) {
		result := env.run(t, "pull")
		if result.ExitCode != 0 {
			t.Fatalf("pull failed: %s", result.Stderr)
		}
	})

	// -----------------------------------------------------------------------
	// Step 10: Fetch
	// -----------------------------------------------------------------------
	t.Run("10_fetch", func(t *testing.T) {
		result := env.run(t, "fetch")
		if result.ExitCode != 0 {
			t.Fatalf("fetch failed: %s", result.Stderr)
		}
	})

	// -----------------------------------------------------------------------
	// Step 11: Account update (rename email)
	// -----------------------------------------------------------------------
	t.Run("11_account_update", func(t *testing.T) {
		result := env.run(t, "account", "update", ghKey, "--email", "updated@example.com")
		if result.ExitCode != 0 {
			t.Fatalf("account update failed: %s", result.Stderr)
		}
		loaded, _ := config.Load(env.CfgPath)
		if loaded.Accounts[ghKey].Email != "updated@example.com" {
			t.Error("email not updated")
		}
	})

	// -----------------------------------------------------------------------
	// Step 12: Mirror CRUD (config-only, no real mirror setup without Forgejo)
	// -----------------------------------------------------------------------
	t.Run("12_mirror_add", func(t *testing.T) {
		result := env.run(t, "mirror", "add", "test-mirror",
			"--account-src", ghKey,
			"--account-dst", "dummy-gitlab",
		)
		if result.ExitCode != 0 {
			t.Fatalf("mirror add failed: %s", result.Stderr)
		}
		cliAssertConfigHasMirror(t, env.CfgPath, "test-mirror")
	})

	t.Run("12b_mirror_add_repo", func(t *testing.T) {
		result := env.run(t, "mirror", "add-repo", "test-mirror", repo,
			"--direction", "push", "--origin", "src")
		if result.ExitCode != 0 {
			t.Fatalf("mirror add-repo failed: %s", result.Stderr)
		}
		cliAssertConfigHasMirrorRepo(t, env.CfgPath, "test-mirror", repo)
	})

	// -----------------------------------------------------------------------
	// Step 13: Delete clone directory, verify status shows not-cloned
	// -----------------------------------------------------------------------
	t.Run("13_delete_clone", func(t *testing.T) {
		parts := strings.SplitN(repo, "/", 2)
		repoPath := filepath.Join(env.GitFolder, srcKey, parts[0], parts[1])
		os.RemoveAll(repoPath)
		if _, err := os.Stat(repoPath); err == nil {
			t.Fatal("clone still exists after delete")
		}
	})

	// -----------------------------------------------------------------------
	// Step 14: Re-clone
	// -----------------------------------------------------------------------
	t.Run("14_reclone", func(t *testing.T) {
		result := env.run(t, "clone")
		if result.ExitCode != 0 {
			t.Fatalf("reclone failed: %s\nstdout: %s", result.Stderr, result.Stdout)
		}
		parts := strings.SplitN(repo, "/", 2)
		repoPath := filepath.Join(env.GitFolder, srcKey, parts[0], parts[1])
		if _, err := os.Stat(filepath.Join(repoPath, ".git")); os.IsNotExist(err) {
			t.Fatal("reclone not found on disk")
		}
	})

	// -----------------------------------------------------------------------
	// Step 15-20: Cleanup (reverse order)
	// -----------------------------------------------------------------------
	t.Run("15_delete_mirror_repo", func(t *testing.T) {
		result := env.run(t, "mirror", "delete-repo", "test-mirror", repo)
		if result.ExitCode != 0 {
			t.Fatalf("mirror delete-repo failed: %s", result.Stderr)
		}
		cliAssertConfigNoMirrorRepo(t, env.CfgPath, "test-mirror", repo)
	})

	t.Run("16_delete_mirror", func(t *testing.T) {
		result := env.run(t, "mirror", "delete", "test-mirror")
		if result.ExitCode != 0 {
			t.Fatalf("mirror delete failed: %s", result.Stderr)
		}
		cliAssertConfigNoMirror(t, env.CfgPath, "test-mirror")
	})

	t.Run("17_delete_repo", func(t *testing.T) {
		result := env.run(t, "repo", "delete", srcKey, repo)
		if result.ExitCode != 0 {
			t.Fatalf("repo delete failed: %s", result.Stderr)
		}
		cliAssertConfigNoRepo(t, env.CfgPath, srcKey, repo)
	})

	t.Run("18_delete_source", func(t *testing.T) {
		result := env.run(t, "source", "delete", srcKey)
		if result.ExitCode != 0 {
			t.Fatalf("source delete failed: %s", result.Stderr)
		}
		cliAssertConfigNoSource(t, env.CfgPath, srcKey)
	})

	t.Run("19_delete_accounts", func(t *testing.T) {
		for _, key := range []string{ghKey, "dummy-gitlab"} {
			result := env.run(t, "account", "delete", key)
			if result.ExitCode != 0 {
				t.Fatalf("delete account %s failed: %s", key, result.Stderr)
			}
			cliAssertConfigNoAccount(t, env.CfgPath, key)
		}
	})

	t.Run("20_final_verification", func(t *testing.T) {
		loaded, err := config.Load(env.CfgPath)
		if err != nil {
			t.Fatalf("loading final config: %v", err)
		}
		if len(loaded.Accounts) != 0 {
			t.Errorf("expected 0 accounts, got %d", len(loaded.Accounts))
		}
		if len(loaded.Sources) != 0 {
			t.Errorf("expected 0 sources, got %d", len(loaded.Sources))
		}
		if len(loaded.Mirrors) != 0 {
			t.Errorf("expected 0 mirrors, got %d", len(loaded.Mirrors))
		}
	})
}

func mapKeysAny(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
