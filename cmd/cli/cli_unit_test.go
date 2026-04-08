package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// ---------------------------------------------------------------------------
// Smoke tests
// ---------------------------------------------------------------------------

func TestCLI_Version(t *testing.T) {
	env := setupCLIEnv(t)
	result := env.run(t, "version")
	if result.ExitCode != 0 {
		t.Fatalf("version failed: %s", result.Stderr)
	}
	if !strings.Contains(result.Stdout, "gitbox") {
		t.Errorf("version output missing 'gitbox': %s", result.Stdout)
	}
}

func TestCLI_Help(t *testing.T) {
	env := setupCLIEnv(t)
	result := env.run(t, "--help")
	if result.ExitCode != 0 {
		t.Fatalf("help failed: %s", result.Stderr)
	}
	if !strings.Contains(result.Stdout, "gitbox") {
		t.Errorf("help output missing 'gitbox': %s", result.Stdout)
	}
}

// ---------------------------------------------------------------------------
// Global settings
// ---------------------------------------------------------------------------

func TestCLI_GlobalShow(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	env := setupCLIEnvWithConfig(t, cfg)

	var out map[string]any
	result := env.runJSON(t, &out, "global", "show")
	if result.ExitCode != 0 {
		t.Fatalf("global show failed: %s", result.Stderr)
	}
	if out["folder"] != "/tmp/test-git" {
		t.Errorf("expected folder=/tmp/test-git, got %v", out["folder"])
	}
}

func TestCLI_GlobalUpdate(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "global", "update", "--folder", "/tmp/new-folder")
	if result.ExitCode != 0 {
		t.Fatalf("global update failed: %s", result.Stderr)
	}

	// Verify on disk.
	loaded, _ := config.Load(env.CfgPath)
	if loaded.Global.Folder != "/tmp/new-folder" {
		t.Errorf("expected folder=/tmp/new-folder, got %s", loaded.Global.Folder)
	}
}

// ---------------------------------------------------------------------------
// Account CRUD
// ---------------------------------------------------------------------------

func TestCLI_AccountAdd(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "account", "add", "test-gh",
		"--provider", "github",
		"--url", "https://github.com",
		"--username", "testuser",
		"--name", "Test User",
		"--email", "test@example.com",
		"--default-credential-type", "token",
	)
	if result.ExitCode != 0 {
		t.Fatalf("account add failed: %s", result.Stderr)
	}
	cliAssertConfigHasAccount(t, env.CfgPath, "test-gh")
}

func TestCLI_AccountList(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["my-acct"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u", Name: "N", Email: "e@e.com",
	}
	env := setupCLIEnvWithConfig(t, cfg)

	// JSON output is a map keyed by account key.
	var accounts map[string]any
	result := env.runJSON(t, &accounts, "account", "list")
	if result.ExitCode != 0 {
		t.Fatalf("account list failed: %s", result.Stderr)
	}
	if _, ok := accounts["my-acct"]; !ok {
		t.Errorf("account 'my-acct' not in list output: %v", accounts)
	}
}

func TestCLI_AccountShow(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["show-acct"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "showuser", Name: "Show User", Email: "show@example.com",
	}
	env := setupCLIEnvWithConfig(t, cfg)

	var out map[string]any
	result := env.runJSON(t, &out, "account", "show", "show-acct")
	if result.ExitCode != 0 {
		t.Fatalf("account show failed: %s", result.Stderr)
	}
	if out["username"] != "showuser" {
		t.Errorf("expected username=showuser, got %v", out["username"])
	}
}

func TestCLI_AccountUpdate(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["upd-acct"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u", Name: "N", Email: "old@example.com",
	}
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "account", "update", "upd-acct", "--email", "new@example.com")
	if result.ExitCode != 0 {
		t.Fatalf("account update failed: %s", result.Stderr)
	}

	loaded, _ := config.Load(env.CfgPath)
	if loaded.Accounts["upd-acct"].Email != "new@example.com" {
		t.Errorf("expected email=new@example.com, got %s", loaded.Accounts["upd-acct"].Email)
	}
}

func TestCLI_AccountDelete(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["del-acct"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u", Name: "N", Email: "e@e.com",
	}
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "account", "delete", "del-acct")
	if result.ExitCode != 0 {
		t.Fatalf("account delete failed: %s", result.Stderr)
	}
	cliAssertConfigNoAccount(t, env.CfgPath, "del-acct")
}

// ---------------------------------------------------------------------------
// Source CRUD
// ---------------------------------------------------------------------------

func TestCLI_SourceAdd(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["src-acct"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u", Name: "N", Email: "e@e.com",
	}
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "source", "add", "my-source", "--account", "src-acct")
	if result.ExitCode != 0 {
		t.Fatalf("source add failed: %s", result.Stderr)
	}
	cliAssertConfigHasSource(t, env.CfgPath, "my-source")
}

func TestCLI_SourceList(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["a"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u", Name: "N", Email: "e@e.com",
	}
	cfg.Sources["test-src"] = config.Source{Account: "a"}
	env := setupCLIEnvWithConfig(t, cfg)

	var sources map[string]any
	result := env.runJSON(t, &sources, "source", "list")
	if result.ExitCode != 0 {
		t.Fatalf("source list failed: %s", result.Stderr)
	}
	if _, ok := sources["test-src"]; !ok {
		t.Errorf("source 'test-src' not in list output: %v", sources)
	}
}

func TestCLI_SourceDelete(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["a"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u", Name: "N", Email: "e@e.com",
	}
	cfg.Sources["del-src"] = config.Source{Account: "a"}
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "source", "delete", "del-src")
	if result.ExitCode != 0 {
		t.Fatalf("source delete failed: %s", result.Stderr)
	}
	cliAssertConfigNoSource(t, env.CfgPath, "del-src")
}

// ---------------------------------------------------------------------------
// Repo CRUD
// ---------------------------------------------------------------------------

func TestCLI_RepoAdd(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["a"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u", Name: "N", Email: "e@e.com",
	}
	cfg.Sources["s"] = config.Source{Account: "a", Repos: map[string]config.Repo{}}
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "repo", "add", "s", "org/myrepo")
	if result.ExitCode != 0 {
		t.Fatalf("repo add failed: %s", result.Stderr)
	}
	cliAssertConfigHasRepo(t, env.CfgPath, "s", "org/myrepo")
}

func TestCLI_RepoList(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["a"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u", Name: "N", Email: "e@e.com",
	}
	cfg.Sources["s"] = config.Source{
		Account: "a",
		Repos:   map[string]config.Repo{"org/repo1": {}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	// Repo list JSON is a map of sources → source objects.
	var sources map[string]any
	result := env.runJSON(t, &sources, "repo", "list")
	if result.ExitCode != 0 {
		t.Fatalf("repo list failed: %s", result.Stderr)
	}
	if len(sources) == 0 {
		t.Fatal("expected at least 1 source in repo list")
	}
}

func TestCLI_RepoDelete(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["a"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u", Name: "N", Email: "e@e.com",
	}
	cfg.Sources["s"] = config.Source{
		Account: "a",
		Repos:   map[string]config.Repo{"org/del-repo": {}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "repo", "delete", "s", "org/del-repo")
	if result.ExitCode != 0 {
		t.Fatalf("repo delete failed: %s", result.Stderr)
	}
	cliAssertConfigNoRepo(t, env.CfgPath, "s", "org/del-repo")
}

// ---------------------------------------------------------------------------
// Mirror CRUD
// ---------------------------------------------------------------------------

func TestCLI_MirrorAdd(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["src-a"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u1", Name: "N1", Email: "e1@e.com",
	}
	cfg.Accounts["dst-a"] = config.Account{
		Provider: "forgejo", URL: "https://forge.example.com",
		Username: "u2", Name: "N2", Email: "e2@e.com",
	}
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "mirror", "add", "m1", "--account-src", "src-a", "--account-dst", "dst-a")
	if result.ExitCode != 0 {
		t.Fatalf("mirror add failed: %s", result.Stderr)
	}
	cliAssertConfigHasMirror(t, env.CfgPath, "m1")
}

func TestCLI_MirrorAddRepo(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["src-a"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u1", Name: "N1", Email: "e1@e.com",
	}
	cfg.Accounts["dst-a"] = config.Account{
		Provider: "forgejo", URL: "https://forge.example.com",
		Username: "u2", Name: "N2", Email: "e2@e.com",
	}
	cfg.Mirrors = map[string]config.Mirror{
		"m1": {AccountSrc: "src-a", AccountDst: "dst-a", Repos: map[string]config.MirrorRepo{}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "mirror", "add-repo", "m1", "org/repo", "--direction", "push", "--origin", "src")
	if result.ExitCode != 0 {
		t.Fatalf("mirror add-repo failed: %s", result.Stderr)
	}
	cliAssertConfigHasMirrorRepo(t, env.CfgPath, "m1", "org/repo")
}

func TestCLI_MirrorDeleteRepo(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["src-a"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u1", Name: "N1", Email: "e1@e.com",
	}
	cfg.Accounts["dst-a"] = config.Account{
		Provider: "forgejo", URL: "https://forge.example.com",
		Username: "u2", Name: "N2", Email: "e2@e.com",
	}
	cfg.Mirrors = map[string]config.Mirror{
		"m1": {
			AccountSrc: "src-a", AccountDst: "dst-a",
			Repos: map[string]config.MirrorRepo{
				"org/repo": {Direction: "push", Origin: "src"},
			},
		},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "mirror", "delete-repo", "m1", "org/repo")
	if result.ExitCode != 0 {
		t.Fatalf("mirror delete-repo failed: %s", result.Stderr)
	}
	cliAssertConfigNoMirrorRepo(t, env.CfgPath, "m1", "org/repo")
}

func TestCLI_MirrorDelete(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["src-a"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u1", Name: "N1", Email: "e1@e.com",
	}
	cfg.Accounts["dst-a"] = config.Account{
		Provider: "forgejo", URL: "https://forge.example.com",
		Username: "u2", Name: "N2", Email: "e2@e.com",
	}
	cfg.Mirrors = map[string]config.Mirror{
		"m1": {AccountSrc: "src-a", AccountDst: "dst-a", Repos: map[string]config.MirrorRepo{}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "mirror", "delete", "m1")
	if result.ExitCode != 0 {
		t.Fatalf("mirror delete failed: %s", result.Stderr)
	}
	cliAssertConfigNoMirror(t, env.CfgPath, "m1")
}

// ---------------------------------------------------------------------------
// Mirror list
// ---------------------------------------------------------------------------

func TestCLI_MirrorList(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["src-a"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u1", Name: "N1", Email: "e1@e.com",
	}
	cfg.Accounts["dst-a"] = config.Account{
		Provider: "forgejo", URL: "https://forge.example.com",
		Username: "u2", Name: "N2", Email: "e2@e.com",
	}
	cfg.Mirrors = map[string]config.Mirror{
		"m1": {AccountSrc: "src-a", AccountDst: "dst-a", Repos: map[string]config.MirrorRepo{}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	var mirrors map[string]any
	result := env.runJSON(t, &mirrors, "mirror", "list")
	if result.ExitCode != 0 {
		t.Fatalf("mirror list failed: %s", result.Stderr)
	}
	if _, ok := mirrors["m1"]; !ok {
		t.Errorf("mirror 'm1' not in list output: %v", mirrors)
	}
}

// ---------------------------------------------------------------------------
// Status (no clones — should still work)
// ---------------------------------------------------------------------------

func TestCLI_Status(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["a"] = config.Account{
		Provider: "github", URL: "https://github.com",
		Username: "u", Name: "N", Email: "e@e.com",
		DefaultCredentialType: "token",
	}
	cfg.Sources["s"] = config.Source{
		Account: "a",
		Repos:   map[string]config.Repo{"org/repo": {}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	var statuses []map[string]any
	result := env.runJSON(t, &statuses, "status")
	if result.ExitCode != 0 {
		t.Fatalf("status failed: %s", result.Stderr)
	}
	// Should report the repo as not cloned.
	if len(statuses) == 0 {
		t.Fatal("expected at least 1 status entry")
	}
}

// ---------------------------------------------------------------------------
// Error cases
// ---------------------------------------------------------------------------

func TestCLI_AccountAdd_MissingFlags(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	env := setupCLIEnvWithConfig(t, cfg)

	// Missing required flags should fail.
	result := env.run(t, "account", "add", "incomplete")
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit for missing flags")
	}
}

func TestCLI_AccountShow_NotFound(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "account", "show", "nonexistent")
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit for missing account")
	}
}

func TestCLI_SourceAdd_NoAccount(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "source", "add", "orphan-src", "--account", "nonexistent")
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit for nonexistent account")
	}
}

// ---------------------------------------------------------------------------
// Browse
// ---------------------------------------------------------------------------

func TestCLI_Browse_MissingRepo(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "browse")
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit when --repo is missing")
	}
}

func TestCLI_Browse_RepoNotFound(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["github-test"] = config.Account{
		Provider:              "github",
		URL:                   "https://github.com",
		Username:              "testuser",
		Name:                  "Test User",
		Email:                 "test@example.com",
		DefaultCredentialType: "token",
	}
	cfg.Sources["test-src"] = config.Source{
		Account: "github-test",
		Repos:   map[string]config.Repo{"testuser/exists": {}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "browse", "--repo", "testuser/nonexistent")
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit for nonexistent repo")
	}
}

func TestCLI_Browse_JSON(t *testing.T) {
	cfg := newCLITestConfig("/tmp/test-git")
	cfg.Accounts["github-test"] = config.Account{
		Provider:              "github",
		URL:                   "https://github.com",
		Username:              "testuser",
		Name:                  "Test User",
		Email:                 "test@example.com",
		DefaultCredentialType: "token",
	}
	cfg.Sources["test-src"] = config.Source{
		Account: "github-test",
		Repos:   map[string]config.Repo{"testuser/myrepo": {}},
	}
	env := setupCLIEnvWithConfig(t, cfg)

	result := env.run(t, "browse", "--repo", "testuser/myrepo", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("browse --json failed: %s", result.Stderr)
	}
	if !strings.Contains(result.Stdout, "https://github.com/testuser/myrepo") {
		t.Errorf("browse --json output missing URL: %s", result.Stdout)
	}
}

// Ensure json is imported.
var _ = json.Marshal
