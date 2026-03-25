package config

import (
	"os"
	"path/filepath"
	"testing"
)

// --- Test fixtures ---

const v2JSON = `{
    "$schema": "https://example.com/schema.json",
    "version": 2,
    "global": {
        "folder": "~/00.git",
        "credential_ssh": { "ssh_folder": "~/.ssh" },
        "credential_gcm": { "helper": "manager", "credential_store": "wincredman" }
    },
    "accounts": {
        "GitHub-Test": {
            "provider": "github",
            "url": "https://github.com",
            "username": "TestUser",
            "name": "Test User",
            "email": "test@example.com",
            "default_branch": "main",
            "default_credential_type": "gcm",
            "ssh": { "host": "gh-TestUser", "hostname": "github.com", "key_type": "ed25519" },
            "gcm": { "provider": "github", "useHttpPath": false }
        },
        "Forgejo-Homelab": {
            "provider": "forgejo",
            "url": "https://forge.home.lan",
            "username": "testuser",
            "name": "TestUser",
            "email": "testuser@home.lan",
            "default_credential_type": "ssh"
        }
    },
    "sources": {
        "GitHub-Test": {
            "account": "GitHub-Test",
            "repos": {
                "TestUser/my-repo": {},
                "other-org/cross-repo": {
                    "credential_type": "ssh",
                    "name": "Alt Name",
                    "email": "alt@example.com",
                    "clone_folder": "~/custom/path"
                }
            }
        },
        "Forgejo-Homelab": {
            "account": "Forgejo-Homelab",
            "folder": "forgejo-hl",
            "repos": {
                "infra/homelab": {}
            }
        }
    }
}`

const v1JSON = `{
    "global": {
        "folder": "~/00.git",
        "credential_ssh": { "enabled": "false", "ssh_folder": "~/.ssh" },
        "credential_gcm": { "enabled": "true", "helper": "manager", "credentialStore": "wincredman" }
    },
    "accounts": {
        "GitHub-Test": {
            "url": "https://github.com/TestUser",
            "username": "TestUser",
            "folder": "github-test",
            "name": "Test User",
            "email": "test@example.com",
            "gcm_provider": "github",
            "gcm_useHttpPath": "false",
            "ssh_host": "gh-TestUser",
            "ssh_hostname": "github.com",
            "ssh_type": "ed25519",
            "repos": {
                "my-repo": { "credential_type": "gcm" },
                "cross-repo": {
                    "credential_type": "ssh",
                    "name": "Alt Name",
                    "email": "alt@example.com",
                    "folder": "~/custom/path"
                }
            }
        },
        "Gitea-Generic": {
            "url": "https://git.example.org/testuser",
            "username": "testuser",
            "folder": "gitea-testuser",
            "name": "TestUser",
            "email": "testuser@example.com",
            "repos": {
                "project": { "credential_type": "gcm" }
            }
        }
    }
}`

// --- Parse v2 tests ---

func TestParseV2(t *testing.T) {
	cfg, err := Parse([]byte(v2JSON))
	if err != nil {
		t.Fatalf("Parse v2: %v", err)
	}

	if cfg.Version != 2 {
		t.Errorf("version = %d, want 2", cfg.Version)
	}
	if cfg.Global.CredentialGCM.CredentialStore != "wincredman" {
		t.Error("credential_store should be wincredman")
	}

	// Accounts.
	if len(cfg.Accounts) != 2 {
		t.Fatalf("accounts count = %d, want 2", len(cfg.Accounts))
	}
	ghAcct := cfg.Accounts["GitHub-Test"]
	if ghAcct.DefaultCredentialType != "gcm" {
		t.Errorf("default_credential_type = %q, want gcm", ghAcct.DefaultCredentialType)
	}

	// Sources.
	ghSrc := cfg.Sources["GitHub-Test"]
	if ghSrc.Account != "GitHub-Test" {
		t.Errorf("source account = %q", ghSrc.Account)
	}
	// No folder set → EffectiveFolder should return source key.
	if ghSrc.EffectiveFolder("GitHub-Test") != "GitHub-Test" {
		t.Error("EffectiveFolder should return source key when folder is empty")
	}

	// Forgejo has explicit folder.
	fjSrc := cfg.Sources["Forgejo-Homelab"]
	if fjSrc.EffectiveFolder("Forgejo-Homelab") != "forgejo-hl" {
		t.Errorf("EffectiveFolder = %q, want forgejo-hl", fjSrc.EffectiveFolder("Forgejo-Homelab"))
	}

	// Repo credential inheritance.
	myRepo := ghSrc.Repos["TestUser/my-repo"]
	if myRepo.EffectiveCredentialType(&ghAcct) != "gcm" {
		t.Error("my-repo should inherit gcm from account")
	}
	crossRepo := ghSrc.Repos["other-org/cross-repo"]
	if crossRepo.EffectiveCredentialType(&ghAcct) != "ssh" {
		t.Error("cross-repo should use its own ssh override")
	}

	// GetAccount.
	acct := cfg.GetAccount("GitHub-Test")
	if acct == nil || acct.Username != "TestUser" {
		t.Error("GetAccount should resolve to TestUser")
	}
}

// --- Parse v1 auto-detect ---

func TestParseV1AutoDetect(t *testing.T) {
	cfg, err := Parse([]byte(v1JSON))
	if err != nil {
		t.Fatalf("Parse v1: %v", err)
	}

	if cfg.Version != 2 {
		t.Errorf("version = %d, want 2", cfg.Version)
	}

	// 2 v1 accounts with different (hostname, username) → 2 accounts + 2 sources.
	if len(cfg.Accounts) != 2 {
		t.Fatalf("accounts count = %d, want 2", len(cfg.Accounts))
	}
	if len(cfg.Sources) != 2 {
		t.Fatalf("sources count = %d, want 2", len(cfg.Sources))
	}

	// GitHub-Test account.
	ghAcct := cfg.Accounts["GitHub-Test"]
	if ghAcct.URL != "https://github.com" {
		t.Errorf("URL = %q, want https://github.com", ghAcct.URL)
	}
	if ghAcct.Provider != "github" {
		t.Errorf("provider = %q", ghAcct.Provider)
	}
	// default_credential_type should be detected (gcm is most common).
	if ghAcct.DefaultCredentialType != "gcm" {
		t.Errorf("default_credential_type = %q, want gcm", ghAcct.DefaultCredentialType)
	}

	// Source repos should have org/ prefix.
	ghSrc := cfg.Sources["GitHub-Test"]
	if _, ok := ghSrc.Repos["TestUser/my-repo"]; !ok {
		t.Error("missing TestUser/my-repo (should have org/ prefix)")
	}
	if _, ok := ghSrc.Repos["TestUser/cross-repo"]; !ok {
		t.Error("missing TestUser/cross-repo")
	}

	// Repo that matches default_credential_type should have it stripped.
	myRepo := ghSrc.Repos["TestUser/my-repo"]
	if myRepo.CredentialType != "" {
		t.Errorf("my-repo credential_type = %q, want empty (matches default)", myRepo.CredentialType)
	}
	// Repo with different credential_type should keep it.
	crossRepo := ghSrc.Repos["TestUser/cross-repo"]
	if crossRepo.CredentialType != "ssh" {
		t.Errorf("cross-repo credential_type = %q, want ssh", crossRepo.CredentialType)
	}
}

// --- Real-world dedup test ---

const v1RealWorldJSON = `{
    "global": { "folder": "~/00.git",
        "credential_gcm": { "enabled": "true", "helper": "manager", "credentialStore": "wincredman" }
    },
    "accounts": {
        "git-example-personal": {
            "url": "https://git.example.org/personal",
            "username": "myuser", "folder": "git-example-personal",
            "name": "My Name", "email": "myuser@example.com",
            "gcm_provider": "generic",
            "repos": { "my-project": { "credential_type": "gcm" } }
        },
        "git-example-infra": {
            "url": "https://git.example.org/infra",
            "username": "myuser", "folder": "git-example-infra",
            "name": "My Name", "email": "myuser@example.com",
            "gcm_provider": "generic",
            "repos": {
                "homelab": { "credential_type": "gcm" },
                "migration": { "credential_type": "gcm" }
            }
        },
        "git-example-myuser": {
            "url": "https://git.example.org/myuser",
            "username": "myuser", "folder": "git-example-myuser",
            "name": "My Name", "email": "myuser@example.com",
            "gcm_provider": "generic",
            "repos": {
                "config-json": { "credential_type": "gcm", "folder": "~/.config/git-config-repos" }
            }
        },
        "github-MyGitHubUser": {
            "url": "https://github.com/MyGitHubUser",
            "username": "MyGitHubUser", "folder": "github-myuser",
            "name": "My Name", "email": "myuser@example.com",
            "gcm_provider": "github",
            "ssh_host": "gh-MyGitHubUser", "ssh_hostname": "github.com", "ssh_type": "ed25519",
            "repos": {
                "git-toolkit": { "credential_type": "gcm" },
                "dotfiles": { "credential_type": "ssh" }
            }
        },
        "github-readonly": {
            "url": "https://github.com",
            "username": "MyGitHubUser", "folder": "github-readonly",
            "name": "My Name", "email": "myuser@example.com",
            "gcm_provider": "github",
            "repos": { "external-org/ext-project": { "credential_type": "gcm" } }
        },
        "github-myorg": {
            "url": "https://github.com/MyOrg",
            "username": "OrgUser", "folder": "github-myorg",
            "name": "OrgUser", "email": "orguser@example.com",
            "gcm_provider": "github",
            "repos": {
                "myorg.browser": { "credential_type": "gcm" },
                "myorg.docs": { "credential_type": "gcm" }
            }
        },
        "github-myorg-rest": {
            "url": "https://github.com/MyOrg",
            "username": "OrgUser", "folder": "github-myorg-rest",
            "name": "OrgUser", "email": "orguser@example.com",
            "gcm_provider": "github",
            "repos": { "myorg.github.io": { "credential_type": "gcm" } }
        }
    }
}`

func TestParseV1RealWorldDedup(t *testing.T) {
	cfg, err := Parse([]byte(v1RealWorldJSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// 7 v1 accounts should dedup to 3 accounts + 3 sources:
	// git-example (3 merged), github-MyGitHubUser (2 merged), github-myorg (2 merged)
	if len(cfg.Accounts) != 3 {
		t.Errorf("accounts count = %d, want 3", len(cfg.Accounts))
		for k := range cfg.Accounts {
			t.Logf("  account: %q", k)
		}
	}
	if len(cfg.Sources) != 3 {
		t.Errorf("sources count = %d, want 3", len(cfg.Sources))
		for k := range cfg.Sources {
			t.Logf("  source: %q", k)
		}
	}

	// --- git-example: 3 v1 accounts merged ---
	exampleAcct, ok := cfg.Accounts["git-example"]
	if !ok {
		// Try to find it by iterating.
		for k, a := range cfg.Accounts {
			if a.Username == "myuser" && extractHostFromURL(a.URL) == "git.example.org" {
				t.Logf("example account found as %q", k)
				exampleAcct = a
				ok = true
				break
			}
		}
	}
	if !ok {
		t.Fatal("example account not found")
	}
	if exampleAcct.URL != "https://git.example.org" {
		t.Errorf("example URL = %q", exampleAcct.URL)
	}

	// Find example source.
	var exampleSrc Source
	for k, s := range cfg.Sources {
		if s.Account == "git-example" || (cfg.Accounts[s.Account].Username == "myuser" && extractHostFromURL(cfg.Accounts[s.Account].URL) == "git.example.org") {
			exampleSrc = s
			t.Logf("example source key: %q, repos: %d", k, len(s.Repos))
			break
		}
	}
	// Should have 4 repos: personal/my-project, infra/homelab, infra/migration, myuser/config-json.
	if len(exampleSrc.Repos) != 4 {
		t.Errorf("example repos = %d, want 4", len(exampleSrc.Repos))
		for k := range exampleSrc.Repos {
			t.Logf("  repo: %q", k)
		}
	}
	// Check org/ prefixes.
	for _, expected := range []string{"personal/my-project", "infra/homelab", "infra/migration", "myuser/config-json"} {
		if _, ok := exampleSrc.Repos[expected]; !ok {
			t.Errorf("missing repo %q", expected)
		}
	}
	// CloneFolder override preserved (was explicit folder in v1).
	if r, ok := exampleSrc.Repos["myuser/config-json"]; ok {
		if r.CloneFolder != "~/.config/git-config-repos" {
			t.Errorf("config-json clone_folder = %q", r.CloneFolder)
		}
	}

	// --- github-MyGitHubUser: 2 merged (personal + readonly) ---
	var ghSrc Source
	var ghAcctKey string
	for k, s := range cfg.Sources {
		acct := cfg.Accounts[s.Account]
		if acct.Username == "MyGitHubUser" {
			ghSrc = s
			ghAcctKey = s.Account
			t.Logf("github-MyGitHubUser source key: %q, account: %q, repos: %d", k, s.Account, len(s.Repos))
			break
		}
	}
	ghAcct := cfg.Accounts[ghAcctKey]
	if ghAcct.SSH == nil || ghAcct.SSH.Host != "gh-MyGitHubUser" {
		t.Error("github SSH should be preserved from the account that had it")
	}
	// Should have: MyGitHubUser/git-toolkit, MyGitHubUser/dotfiles, external-org/ext-project.
	if len(ghSrc.Repos) != 3 {
		t.Errorf("github repos = %d, want 3", len(ghSrc.Repos))
		for k := range ghSrc.Repos {
			t.Logf("  repo: %q", k)
		}
	}
	// The readonly repo should keep its org/ prefix (already had it in v1).
	if _, ok := ghSrc.Repos["external-org/ext-project"]; !ok {
		t.Error("missing external-org/ext-project")
	}

	// --- github-myorg: 2 merged (split account) ---
	var swSrc Source
	for _, s := range cfg.Sources {
		acct := cfg.Accounts[s.Account]
		if acct.Username == "OrgUser" {
			swSrc = s
			break
		}
	}
	// Should have 3 repos: MyOrg/myorg.browser, MyOrg/myorg.docs, MyOrg/myorg.github.io.
	if len(swSrc.Repos) != 3 {
		t.Errorf("myorg repos = %d, want 3", len(swSrc.Repos))
		for k := range swSrc.Repos {
			t.Logf("  repo: %q", k)
		}
	}
	// The "rest" repo should NOT have a folder override — new layout uses nested dirs.
	if r, ok := swSrc.Repos["MyOrg/myorg.github.io"]; ok {
		if r.CloneFolder != "" {
			t.Errorf("myorg.github.io should not have clone_folder override, got %q", r.CloneFolder)
		}
	}
}

// --- Validation tests ---

func TestParseV2MissingRequired(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"missing global.folder", `{"version":2,"global":{},"accounts":{},"sources":{}}`},
		{"missing account.provider", `{"version":2,"global":{"folder":"~/x"},"accounts":{"A":{"url":"u","username":"u","name":"n","email":"e@e"}},"sources":{}}`},
		{"unknown account ref", `{"version":2,"global":{"folder":"~/x"},"accounts":{},"sources":{"S":{"account":"nope","repos":{}}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse([]byte(tt.json)); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestParseV2EmptySources(t *testing.T) {
	j := `{"version":2,"global":{"folder":"~/x"},"accounts":{},"sources":{}}`
	cfg, err := Parse([]byte(j))
	if err != nil {
		t.Fatalf("should be valid: %v", err)
	}
	if len(cfg.Sources) != 0 {
		t.Errorf("sources = %d", len(cfg.Sources))
	}
}

func TestParseInvalidJSON(t *testing.T) {
	if _, err := Parse([]byte(`{bad`)); err == nil {
		t.Error("expected error")
	}
}

func TestParseWrongVersion(t *testing.T) {
	if _, err := Parse([]byte(`{"version":99,"global":{"folder":"~/x"},"accounts":{},"sources":{}}`)); err == nil {
		t.Error("expected error")
	}
}

// --- Round-trip test ---

func TestSaveLoadRoundTrip(t *testing.T) {
	cfg, err := Parse([]byte(v2JSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "test.json")
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cfg2, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg2.Accounts) != len(cfg.Accounts) {
		t.Error("accounts count mismatch")
	}
	if len(cfg2.Sources) != len(cfg.Sources) {
		t.Error("sources count mismatch")
	}
	ghAcct := cfg2.Accounts["GitHub-Test"]
	if ghAcct.DefaultCredentialType != "gcm" {
		t.Error("default_credential_type lost")
	}
}

// --- Migrate file test ---

func TestMigrateV1ToV2(t *testing.T) {
	dir := t.TempDir()
	v1Path := filepath.Join(dir, "v1.json")
	os.WriteFile(v1Path, []byte(v1JSON), 0o644)
	v2Path := filepath.Join(dir, "v2.json")

	cfg, err := Migrate(v1Path, v2Path)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if cfg.Version != 2 {
		t.Error("version should be 2")
	}
	if _, err := os.Stat(v2Path); err != nil {
		t.Fatal("v2 file not created")
	}
	cfg2, err := Load(v2Path)
	if err != nil {
		t.Fatalf("Load migrated: %v", err)
	}
	if len(cfg2.Accounts) != 2 {
		t.Errorf("accounts = %d, want 2", len(cfg2.Accounts))
	}
}

func TestMigrateDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	v1Path := filepath.Join(dir, "v1.json")
	v2Path := filepath.Join(dir, "v2.json")
	os.WriteFile(v1Path, []byte(v1JSON), 0o644)
	os.WriteFile(v2Path, []byte("x"), 0o644)
	if _, err := Migrate(v1Path, v2Path); err == nil {
		t.Error("expected error")
	}
}

// --- Path tests ---

func TestExpandTilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	if ExpandTilde("~/foo") != filepath.Join(home, "foo") {
		t.Error("tilde expansion failed")
	}
	if ExpandTilde("/abs") != "/abs" {
		t.Error("absolute path should not change")
	}
}

func TestEnsureDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "f.json")
	if err := EnsureDir(path); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(dir, "a", "b")); err != nil || !info.IsDir() {
		t.Error("directory not created")
	}
}
