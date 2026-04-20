package config

import (
	"os"
	"path/filepath"
	"strings"
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

const v2MirrorsJSON = `{
    "version": 2,
    "global": { "folder": "~/00.git" },
    "accounts": {
        "forgejo-luis": {
            "provider": "forgejo",
            "url": "https://forge.home.lan",
            "username": "luis",
            "name": "Luis",
            "email": "luis@home.lan",
            "default_credential_type": "token"
        },
        "github-luis": {
            "provider": "github",
            "url": "https://github.com",
            "username": "LuisUser",
            "name": "Luis",
            "email": "luis@example.com",
            "default_credential_type": "token"
        }
    },
    "sources": {},
    "mirrors": {
        "forgejo-github": {
            "account_src": "forgejo-luis",
            "account_dst": "github-luis",
            "repos": {
                "personal/my-project": {
                    "direction": "push",
                    "origin": "src"
                },
                "LuisUser/dotfiles": {
                    "direction": "pull",
                    "origin": "dst"
                }
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

// --- Mirror parse tests ---

func TestParseV2WithMirrors(t *testing.T) {
	cfg, err := Parse([]byte(v2MirrorsJSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(cfg.Mirrors) != 1 {
		t.Fatalf("mirrors count = %d, want 1", len(cfg.Mirrors))
	}

	m := cfg.Mirrors["forgejo-github"]
	if m.AccountSrc != "forgejo-luis" {
		t.Errorf("account_a = %q", m.AccountSrc)
	}
	if m.AccountDst != "github-luis" {
		t.Errorf("account_b = %q", m.AccountDst)
	}
	if len(m.Repos) != 2 {
		t.Fatalf("repos count = %d, want 2", len(m.Repos))
	}

	pushRepo := m.Repos["personal/my-project"]
	if pushRepo.Direction != "push" || pushRepo.Origin != "src" {
		t.Errorf("push repo: direction=%q origin=%q", pushRepo.Direction, pushRepo.Origin)
	}
	// Method and Status fields were removed — mirror status is derived from live checks.

	pullRepo := m.Repos["LuisUser/dotfiles"]
	if pullRepo.Direction != "pull" || pullRepo.Origin != "dst" {
		t.Errorf("pull repo: direction=%q origin=%q", pullRepo.Direction, pullRepo.Origin)
	}
}

func TestParseV2MirrorKeyOrder(t *testing.T) {
	cfg, err := Parse([]byte(v2MirrorsJSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// MirrorOrder should have one key.
	if len(cfg.MirrorOrder) != 1 || cfg.MirrorOrder[0] != "forgejo-github" {
		t.Errorf("MirrorOrder = %v", cfg.MirrorOrder)
	}

	// RepoOrder should preserve insertion order.
	m := cfg.Mirrors["forgejo-github"]
	if len(m.RepoOrder) != 2 {
		t.Fatalf("RepoOrder len = %d, want 2", len(m.RepoOrder))
	}
	if m.RepoOrder[0] != "personal/my-project" || m.RepoOrder[1] != "LuisUser/dotfiles" {
		t.Errorf("RepoOrder = %v", m.RepoOrder)
	}
}

func TestParseV2WithoutMirrors(t *testing.T) {
	// v2JSON has no mirrors — should parse fine.
	cfg, err := Parse([]byte(v2JSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Mirrors) != 0 {
		t.Errorf("mirrors = %d, want 0", len(cfg.Mirrors))
	}
}

func TestSaveLoadRoundTripWithMirrors(t *testing.T) {
	cfg, err := Parse([]byte(v2MirrorsJSON))
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
	if len(cfg2.Mirrors) != 1 {
		t.Fatalf("mirrors count = %d, want 1", len(cfg2.Mirrors))
	}
	m := cfg2.Mirrors["forgejo-github"]
	if len(m.Repos) != 2 {
		t.Errorf("repos count = %d, want 2", len(m.Repos))
	}
	if m.Repos["personal/my-project"].Direction != "push" {
		t.Error("direction lost in round-trip")
	}
}

func TestParseV2MirrorValidation(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{"missing account_a", `{"version":2,"global":{"folder":"~/x"},"accounts":{"a":{"provider":"github","url":"u","username":"u","name":"n","email":"e@e"},"b":{"provider":"github","url":"u","username":"u","name":"n","email":"e@e"}},"sources":{},"mirrors":{"m":{"account_src":"","account_dst":"b","repos":{}}}}`},
		{"missing account_b", `{"version":2,"global":{"folder":"~/x"},"accounts":{"a":{"provider":"github","url":"u","username":"u","name":"n","email":"e@e"}},"sources":{},"mirrors":{"m":{"account_src":"a","account_dst":"","repos":{}}}}`},
		{"same accounts", `{"version":2,"global":{"folder":"~/x"},"accounts":{"a":{"provider":"github","url":"u","username":"u","name":"n","email":"e@e"}},"sources":{},"mirrors":{"m":{"account_src":"a","account_dst":"a","repos":{}}}}`},
		{"unknown account_a", `{"version":2,"global":{"folder":"~/x"},"accounts":{"b":{"provider":"github","url":"u","username":"u","name":"n","email":"e@e"}},"sources":{},"mirrors":{"m":{"account_src":"nope","account_dst":"b","repos":{}}}}`},
		{"bad direction", `{"version":2,"global":{"folder":"~/x"},"accounts":{"a":{"provider":"github","url":"u","username":"u","name":"n","email":"e@e"},"b":{"provider":"github","url":"u","username":"u","name":"n","email":"e@e"}},"sources":{},"mirrors":{"m":{"account_src":"a","account_dst":"b","repos":{"org/repo":{"direction":"bad","origin":"src"}}}}}`},
		{"bad origin", `{"version":2,"global":{"folder":"~/x"},"accounts":{"a":{"provider":"github","url":"u","username":"u","name":"n","email":"e@e"},"b":{"provider":"github","url":"u","username":"u","name":"n","email":"e@e"}},"sources":{},"mirrors":{"m":{"account_src":"a","account_dst":"b","repos":{"org/repo":{"direction":"push","origin":"c"}}}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse([]byte(tt.json)); err == nil {
				t.Error("expected error")
			}
		})
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

// Ensure global.terminals parses and round-trips through Save, and that the
// EditorEntry schema is preserved alongside it — the two share a GlobalConfig.
func TestParseV2WithTerminals(t *testing.T) {
	js := `{
        "version": 2,
        "global": {
            "folder": "~/x",
            "editors": [ { "name": "VS Code", "command": "code" } ],
            "terminals": [
                { "name": "Windows Terminal", "command": "wt.exe", "args": ["-d", "{path}"] },
                { "name": "Terminal",         "command": "open",    "args": ["-a", "Terminal"] },
                { "name": "Plain",            "command": "/usr/bin/plainterm" }
            ]
        },
        "accounts": {
            "A": {
                "provider": "github", "url": "https://github.com",
                "username": "u", "name": "n", "email": "e@e"
            }
        },
        "sources": {}
    }`
	cfg, err := Parse([]byte(js))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := len(cfg.Global.Terminals); got != 3 {
		t.Fatalf("terminals count = %d, want 3", got)
	}
	wt := cfg.Global.Terminals[0]
	if wt.Name != "Windows Terminal" || wt.Command != "wt.exe" {
		t.Errorf("first terminal = %+v", wt)
	}
	if len(wt.Args) != 2 || wt.Args[0] != "-d" || wt.Args[1] != "{path}" {
		t.Errorf("first terminal args = %v", wt.Args)
	}
	// Editors alongside terminals still parses.
	if len(cfg.Global.Editors) != 1 || cfg.Global.Editors[0].Name != "VS Code" {
		t.Error("editors should coexist with terminals")
	}

	// Round-trip through Save/Load.
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")
	if err := Save(cfg, path); err != nil {
		t.Fatalf("save: %v", err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(reloaded.Global.Terminals) != 3 {
		t.Fatalf("reloaded terminals = %d, want 3", len(reloaded.Global.Terminals))
	}
	plain := reloaded.Global.Terminals[2]
	if plain.Name != "Plain" || plain.Command != "/usr/bin/plainterm" || len(plain.Args) != 0 {
		t.Errorf("plain terminal round-trip = %+v", plain)
	}
}

// Empty global.terminals should be omitted from marshalled JSON (omitempty).
func TestTerminalsOmitEmpty(t *testing.T) {
	cfg, err := Parse([]byte(v2JSON))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.Global.Terminals) != 0 {
		t.Fatalf("fixture should have no terminals; got %d", len(cfg.Global.Terminals))
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")
	if err := Save(cfg, path); err != nil {
		t.Fatalf("save: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(b); strings.Contains(s, `"terminals"`) {
		t.Errorf("marshalled JSON should not include empty terminals array:\n%s", s)
	}
}

// Unset PR badge flags must default to "on" so installs created before
// issue #29 get the feature enabled automatically.
func TestPRBadgesDefaultOn(t *testing.T) {
	var g GlobalConfig
	if !g.PRBadgesOn() {
		t.Error("PRBadgesOn should default to true when field is nil")
	}
	if !g.PRDraftsIncluded() {
		t.Error("PRDraftsIncluded should default to true when field is nil")
	}
	f := false
	g.PRBadgesEnabled = &f
	g.PRIncludeDrafts = &f
	if g.PRBadgesOn() || g.PRDraftsIncluded() {
		t.Error("pointer-to-false must disable the feature")
	}
}
