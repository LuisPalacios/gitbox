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
			"forgejo-test": {
				Provider: "forgejo", URL: "https://forge.home.lan",
				Username: "testuser", Name: "Test", Email: "test@home.lan",
				DefaultCredentialType: "token",
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
	if len(cfg.Accounts) != 3 {
		t.Errorf("accounts = %d, want 3", len(cfg.Accounts))
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
	if len(cfg.Accounts) != 1 {
		t.Errorf("accounts = %d, want 1", len(cfg.Accounts))
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

// --- Mirror CRUD tests ---

func TestAddMirror(t *testing.T) {
	cfg := newTestConfig()
	m := Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"}

	if err := cfg.AddMirror("forgejo-github", m); err != nil {
		t.Fatalf("AddMirror: %v", err)
	}
	if len(cfg.Mirrors) != 1 {
		t.Errorf("mirrors = %d, want 1", len(cfg.Mirrors))
	}
	// Repos should be initialized.
	if cfg.Mirrors["forgejo-github"].Repos == nil {
		t.Error("repos should be initialized")
	}
}

func TestAddMirrorDuplicate(t *testing.T) {
	cfg := newTestConfig()
	m := Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"}
	cfg.AddMirror("m1", m)

	if err := cfg.AddMirror("m1", m); err == nil {
		t.Error("expected error for duplicate")
	}
}

func TestAddMirrorValidation(t *testing.T) {
	cfg := newTestConfig()

	if err := cfg.AddMirror("", Mirror{}); err == nil {
		t.Error("expected error for empty key")
	}
	if err := cfg.AddMirror("m1", Mirror{AccountSrc: "", AccountDst: "github-test"}); err == nil {
		t.Error("expected error for missing account_a")
	}
	if err := cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: ""}); err == nil {
		t.Error("expected error for missing account_b")
	}
	if err := cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "forgejo-test"}); err == nil {
		t.Error("expected error for same accounts")
	}
	if err := cfg.AddMirror("m1", Mirror{AccountSrc: "nonexistent", AccountDst: "github-test"}); err == nil {
		t.Error("expected error for unknown account_a")
	}
	if err := cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "nonexistent"}); err == nil {
		t.Error("expected error for unknown account_b")
	}
}

func TestUpdateMirror(t *testing.T) {
	cfg := newTestConfig()
	cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"})

	updated := cfg.Mirrors["m1"]
	updated.AccountSrc = "github-test"
	updated.AccountDst = "forgejo-test"
	if err := cfg.UpdateMirror("m1", updated); err != nil {
		t.Fatalf("UpdateMirror: %v", err)
	}
	if cfg.Mirrors["m1"].AccountSrc != "github-test" {
		t.Error("account_a not updated")
	}
}

func TestUpdateMirrorNotFound(t *testing.T) {
	cfg := newTestConfig()
	if err := cfg.UpdateMirror("nope", Mirror{}); err == nil {
		t.Error("expected error")
	}
}

func TestDeleteMirror(t *testing.T) {
	cfg := newTestConfig()
	cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"})

	if err := cfg.DeleteMirror("m1"); err != nil {
		t.Fatalf("DeleteMirror: %v", err)
	}
	if len(cfg.Mirrors) != 0 {
		t.Errorf("mirrors = %d, want 0", len(cfg.Mirrors))
	}
}

func TestDeleteMirrorNotFound(t *testing.T) {
	cfg := newTestConfig()
	if err := cfg.DeleteMirror("nope"); err == nil {
		t.Error("expected error")
	}
}

func TestRenameMirror(t *testing.T) {
	cfg := newTestConfig()
	cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"})

	if err := cfg.RenameMirror("m1", "m-renamed"); err != nil {
		t.Fatalf("RenameMirror: %v", err)
	}
	if _, ok := cfg.Mirrors["m-renamed"]; !ok {
		t.Error("renamed mirror not found")
	}
	if _, ok := cfg.Mirrors["m1"]; ok {
		t.Error("old mirror key should be gone")
	}
}

func TestRenameMirrorErrors(t *testing.T) {
	cfg := newTestConfig()
	cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"})
	cfg.AddMirror("m2", Mirror{AccountSrc: "github-test", AccountDst: "forgejo-test"})

	if err := cfg.RenameMirror("m1", ""); err == nil {
		t.Error("expected error for empty new key")
	}
	if err := cfg.RenameMirror("nope", "m3"); err == nil {
		t.Error("expected error for nonexistent source")
	}
	if err := cfg.RenameMirror("m1", "m2"); err == nil {
		t.Error("expected error for duplicate target")
	}
}

func TestDeleteAccountBlockedByMirror(t *testing.T) {
	cfg := newTestConfig()
	cfg.DeleteSource("github-test") // remove source ref first
	cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"})

	if err := cfg.DeleteAccount("github-test"); err == nil {
		t.Error("expected error: account referenced by mirror")
	}
	if err := cfg.DeleteAccount("forgejo-test"); err == nil {
		t.Error("expected error: account referenced by mirror")
	}
}

func TestRenameAccountUpdatesMirrors(t *testing.T) {
	cfg := newTestConfig()
	cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"})

	// Add new account to rename to.
	cfg.AddAccount("forgejo-renamed", Account{
		Provider: "forgejo", URL: "https://forge.home.lan",
		Username: "testuser", Name: "Test", Email: "test@home.lan",
	})

	if err := cfg.RenameAccount("forgejo-test", "forgejo-new"); err != nil {
		t.Fatalf("RenameAccount: %v", err)
	}
	m := cfg.Mirrors["m1"]
	if m.AccountSrc != "forgejo-new" {
		t.Errorf("mirror account_src = %q, want forgejo-new", m.AccountSrc)
	}
}

// --- Mirror Repo CRUD tests ---

func TestAddMirrorRepo(t *testing.T) {
	cfg := newTestConfig()
	cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"})

	repo := MirrorRepo{Direction: "push", Origin: "src"}
	if err := cfg.AddMirrorRepo("m1", "org/repo", repo); err != nil {
		t.Fatalf("AddMirrorRepo: %v", err)
	}
	repos, _ := cfg.ListMirrorRepos("m1")
	if len(repos) != 1 {
		t.Errorf("repos = %d, want 1", len(repos))
	}
}

func TestAddMirrorRepoDuplicate(t *testing.T) {
	cfg := newTestConfig()
	cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"})
	cfg.AddMirrorRepo("m1", "org/repo", MirrorRepo{Direction: "push", Origin: "src"})

	if err := cfg.AddMirrorRepo("m1", "org/repo", MirrorRepo{Direction: "push", Origin: "src"}); err == nil {
		t.Error("expected error for duplicate repo")
	}
}

func TestAddMirrorRepoValidation(t *testing.T) {
	cfg := newTestConfig()
	cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"})

	if err := cfg.AddMirrorRepo("m1", "", MirrorRepo{Direction: "push", Origin: "src"}); err == nil {
		t.Error("expected error for empty repo key")
	}
	if err := cfg.AddMirrorRepo("m1", "org/repo", MirrorRepo{Direction: "bad", Origin: "src"}); err == nil {
		t.Error("expected error for bad direction")
	}
	if err := cfg.AddMirrorRepo("m1", "org/repo", MirrorRepo{Direction: "push", Origin: "c"}); err == nil {
		t.Error("expected error for bad origin")
	}
	if err := cfg.AddMirrorRepo("nope", "org/repo", MirrorRepo{Direction: "push", Origin: "src"}); err == nil {
		t.Error("expected error for bad mirror key")
	}
}

func TestUpdateMirrorRepo(t *testing.T) {
	cfg := newTestConfig()
	cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"})
	cfg.AddMirrorRepo("m1", "org/repo", MirrorRepo{Direction: "push", Origin: "src"})

	if err := cfg.UpdateMirrorRepo("m1", "org/repo", MirrorRepo{Direction: "pull", Origin: "dst"}); err != nil {
		t.Fatalf("UpdateMirrorRepo: %v", err)
	}
	repos, _ := cfg.ListMirrorRepos("m1")
	if repos["org/repo"].Direction != "pull" {
		t.Error("direction not updated")
	}
}

func TestUpdateMirrorRepoNotFound(t *testing.T) {
	cfg := newTestConfig()
	cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"})

	if err := cfg.UpdateMirrorRepo("m1", "nope", MirrorRepo{Direction: "push", Origin: "src"}); err == nil {
		t.Error("expected error")
	}
}

func TestDeleteMirrorRepo(t *testing.T) {
	cfg := newTestConfig()
	cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"})
	cfg.AddMirrorRepo("m1", "org/repo", MirrorRepo{Direction: "push", Origin: "src"})

	if err := cfg.DeleteMirrorRepo("m1", "org/repo"); err != nil {
		t.Fatalf("DeleteMirrorRepo: %v", err)
	}
	repos, _ := cfg.ListMirrorRepos("m1")
	if len(repos) != 0 {
		t.Errorf("repos = %d, want 0", len(repos))
	}
}

func TestDeleteMirrorRepoNotFound(t *testing.T) {
	cfg := newTestConfig()
	cfg.AddMirror("m1", Mirror{AccountSrc: "forgejo-test", AccountDst: "github-test"})

	if err := cfg.DeleteMirrorRepo("m1", "nope"); err == nil {
		t.Error("expected error")
	}
}

func TestListMirrorReposBadMirror(t *testing.T) {
	cfg := newTestConfig()
	if _, err := cfg.ListMirrorRepos("nope"); err == nil {
		t.Error("expected error")
	}
}
