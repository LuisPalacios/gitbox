package config

import "testing"

func newTestConfig() *Config {
	return &Config{
		Version: 2,
		Global:  GlobalConfig{Folder: "~/00.git"},
		Accounts: map[string]Account{
			"github-test": {
				Provider: "github", URL: "https://github.com",
				Username: "testuser", Name: "Test", Email: "test@test.com",
				DefaultCredentialType: "gcm",
			},
		},
		Sources: map[string]Source{
			"github-test": {
				Account: "github-test",
				Repos: map[string]Repo{
					"testuser/repo-a": {},
				},
			},
		},
	}
}

// --- Account CRUD tests ---

func TestAddAccount(t *testing.T) {
	cfg := newTestConfig()
	acct := Account{Provider: "gitlab", URL: "https://gitlab.com", Username: "u", Name: "N", Email: "e@e"}

	if err := cfg.AddAccount("gitlab-new", acct); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	if len(cfg.Accounts) != 2 {
		t.Errorf("accounts = %d, want 2", len(cfg.Accounts))
	}
}

func TestAddAccountDuplicate(t *testing.T) {
	cfg := newTestConfig()
	acct := Account{Provider: "github", URL: "https://github.com", Username: "u", Name: "N", Email: "e@e"}

	if err := cfg.AddAccount("github-test", acct); err == nil {
		t.Error("expected error for duplicate key")
	}
}

func TestAddAccountValidation(t *testing.T) {
	cfg := newTestConfig()

	if err := cfg.AddAccount("", Account{}); err == nil {
		t.Error("expected error for empty key")
	}
	if err := cfg.AddAccount("x", Account{}); err == nil {
		t.Error("expected error for missing fields")
	}
}

func TestUpdateAccount(t *testing.T) {
	cfg := newTestConfig()
	acct := cfg.Accounts["github-test"]
	acct.Name = "Updated Name"

	if err := cfg.UpdateAccount("github-test", acct); err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if cfg.Accounts["github-test"].Name != "Updated Name" {
		t.Error("name not updated")
	}
}

func TestUpdateAccountNotFound(t *testing.T) {
	cfg := newTestConfig()
	if err := cfg.UpdateAccount("nonexistent", Account{}); err == nil {
		t.Error("expected error for nonexistent account")
	}
}

func TestDeleteAccount(t *testing.T) {
	cfg := newTestConfig()

	// Should fail — source references it.
	if err := cfg.DeleteAccount("github-test"); err == nil {
		t.Error("expected error: account referenced by source")
	}

	// Delete source first, then account.
	cfg.DeleteSource("github-test")
	if err := cfg.DeleteAccount("github-test"); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if len(cfg.Accounts) != 0 {
		t.Errorf("accounts = %d, want 0", len(cfg.Accounts))
	}
}

func TestDeleteAccountNotFound(t *testing.T) {
	cfg := newTestConfig()
	if err := cfg.DeleteAccount("nope"); err == nil {
		t.Error("expected error")
	}
}

func TestGetAccountByKey(t *testing.T) {
	cfg := newTestConfig()

	acct, ok := cfg.GetAccountByKey("github-test")
	if !ok {
		t.Fatal("expected to find account")
	}
	if acct.Username != "testuser" {
		t.Errorf("username = %q", acct.Username)
	}

	_, ok = cfg.GetAccountByKey("nope")
	if ok {
		t.Error("expected not found")
	}
}

// --- Source CRUD tests ---

func TestAddSource(t *testing.T) {
	cfg := newTestConfig()
	src := Source{Account: "github-test"}

	if err := cfg.AddSource("new-source", src); err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if len(cfg.Sources) != 2 {
		t.Errorf("sources = %d, want 2", len(cfg.Sources))
	}
	// Repos should be initialized.
	if cfg.Sources["new-source"].Repos == nil {
		t.Error("repos should be initialized")
	}
}

func TestAddSourceDuplicate(t *testing.T) {
	cfg := newTestConfig()
	if err := cfg.AddSource("github-test", Source{Account: "github-test"}); err == nil {
		t.Error("expected error for duplicate")
	}
}

func TestAddSourceBadAccount(t *testing.T) {
	cfg := newTestConfig()
	if err := cfg.AddSource("bad", Source{Account: "nonexistent"}); err == nil {
		t.Error("expected error for bad account ref")
	}
}

func TestDeleteSource(t *testing.T) {
	cfg := newTestConfig()
	if err := cfg.DeleteSource("github-test"); err != nil {
		t.Fatalf("DeleteSource: %v", err)
	}
	if len(cfg.Sources) != 0 {
		t.Errorf("sources = %d, want 0", len(cfg.Sources))
	}
}

// --- Repo CRUD tests ---

func TestAddRepo(t *testing.T) {
	cfg := newTestConfig()
	if err := cfg.AddRepo("github-test", "testuser/repo-b", Repo{}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	repos, _ := cfg.ListRepos("github-test")
	if len(repos) != 2 {
		t.Errorf("repos = %d, want 2", len(repos))
	}
}

func TestAddRepoDuplicate(t *testing.T) {
	cfg := newTestConfig()
	if err := cfg.AddRepo("github-test", "testuser/repo-a", Repo{}); err == nil {
		t.Error("expected error for duplicate repo")
	}
}

func TestAddRepoBadSource(t *testing.T) {
	cfg := newTestConfig()
	if err := cfg.AddRepo("nonexistent", "org/repo", Repo{}); err == nil {
		t.Error("expected error for bad source")
	}
}

func TestUpdateRepo(t *testing.T) {
	cfg := newTestConfig()
	if err := cfg.UpdateRepo("github-test", "testuser/repo-a", Repo{CredentialType: "ssh"}); err != nil {
		t.Fatalf("UpdateRepo: %v", err)
	}
	repos, _ := cfg.ListRepos("github-test")
	if repos["testuser/repo-a"].CredentialType != "ssh" {
		t.Error("credential_type not updated")
	}
}

func TestUpdateRepoNotFound(t *testing.T) {
	cfg := newTestConfig()
	if err := cfg.UpdateRepo("github-test", "nope/nope", Repo{}); err == nil {
		t.Error("expected error")
	}
}

func TestDeleteRepo(t *testing.T) {
	cfg := newTestConfig()
	if err := cfg.DeleteRepo("github-test", "testuser/repo-a"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
	repos, _ := cfg.ListRepos("github-test")
	if len(repos) != 0 {
		t.Errorf("repos = %d, want 0", len(repos))
	}
}

func TestDeleteRepoNotFound(t *testing.T) {
	cfg := newTestConfig()
	if err := cfg.DeleteRepo("github-test", "nope"); err == nil {
		t.Error("expected error")
	}
}

func TestListReposBadSource(t *testing.T) {
	cfg := newTestConfig()
	if _, err := cfg.ListRepos("nope"); err == nil {
		t.Error("expected error")
	}
}
