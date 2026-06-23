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
        "language": "es",
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
        "forgejo-me": {
            "provider": "forgejo",
            "url": "https://forge.home.lan",
            "username": "me",
            "name": "Me",
            "email": "me@home.lan",
            "default_credential_type": "token"
        },
        "github-me": {
            "provider": "github",
            "url": "https://github.com",
            "username": "MyUser",
            "name": "Me",
            "email": "me@example.com",
            "default_credential_type": "token"
        }
    },
    "sources": {},
    "mirrors": {
        "forgejo-github": {
            "account_src": "forgejo-me",
            "account_dst": "github-me",
            "repos": {
                "personal/my-project": {
                    "direction": "push",
                    "origin": "src"
                },
                "MyUser/dotfiles": {
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

	// A v2 file is transparently migrated to the current version on load.
	if cfg.Version != CurrentVersion {
		t.Errorf("version = %d, want %d (migrated)", cfg.Version, CurrentVersion)
	}
	if cfg.Global.CredentialGCM.CredentialStore != "wincredman" {
		t.Error("credential_store should be wincredman")
	}
	if cfg.Global.Language != "es" {
		t.Errorf("language = %q, want es", cfg.Global.Language)
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
		{"invalid global.language", `{"version":2,"global":{"folder":"~/x","language":"fr"},"accounts":{},"sources":{}}`},
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
	if m.AccountSrc != "forgejo-me" {
		t.Errorf("account_a = %q", m.AccountSrc)
	}
	if m.AccountDst != "github-me" {
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

	pullRepo := m.Repos["MyUser/dotfiles"]
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
	if m.RepoOrder[0] != "personal/my-project" || m.RepoOrder[1] != "MyUser/dotfiles" {
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

// Ensure a legacy v2.0 global.terminals[] block parses, gets auto-migrated
// into the v2.1 three-array shape (terminal_apps + shells + terminal_profiles),
// and round-trips cleanly through Save → Load. EditorEntry is preserved
// alongside it — the two share GlobalConfig.
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
	// Legacy field is cleared after migration.
	if got := len(cfg.Global.Terminals); got != 0 {
		t.Fatalf("legacy terminals not cleared after migration; got %d", got)
	}
	// Three Profiles emitted, first marked as default.
	if got := len(cfg.Global.TerminalProfiles); got != 3 {
		t.Fatalf("profiles count = %d, want 3", got)
	}
	if !cfg.Global.TerminalProfiles[0].Default {
		t.Error("first migrated profile should be default")
	}
	if cfg.Global.TerminalProfiles[0].Name != "Windows Terminal" {
		t.Errorf("first profile name = %q", cfg.Global.TerminalProfiles[0].Name)
	}
	if cfg.Global.TerminalProfiles[0].TerminalID != "wt" {
		t.Errorf("first profile TerminalID = %q, want wt", cfg.Global.TerminalProfiles[0].TerminalID)
	}
	// Editors alongside terminals still parses.
	if len(cfg.Global.Editors) != 1 || cfg.Global.Editors[0].Name != "VS Code" {
		t.Error("editors should coexist with terminals")
	}

	// Round-trip through Save/Load — the new shape persists, no second
	// migration runs (idempotent).
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox.json")
	if err := Save(cfg, path); err != nil {
		t.Fatalf("save: %v", err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(reloaded.Global.Terminals) != 0 {
		t.Errorf("reloaded legacy terminals should stay empty; got %d", len(reloaded.Global.Terminals))
	}
	if len(reloaded.Global.TerminalProfiles) != 3 {
		t.Fatalf("reloaded profiles = %d, want 3", len(reloaded.Global.TerminalProfiles))
	}
	plain := reloaded.Global.TerminalProfiles[2]
	if plain.Name != "Plain" || plain.TerminalID != "legacy-plainterm" {
		t.Errorf("plain profile round-trip = %+v", plain)
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

// Configs persisted by an older buggy code path (or hand-edited) may carry
// the same workspace member twice. Load must collapse those silently so the
// UI never renders a duplicate.
func TestParseV2_DedupesWorkspaceMembers(t *testing.T) {
	in := `{
  "version": 3,
  "global": {"folder": "/tmp"},
  "accounts": {"a": {"provider": "github", "url": "https://github.com", "username": "u", "name": "n", "email": "e@e"}},
  "sources": {"a": {"account": "a", "repos": {"o/r": {}}}},
  "workspaces": {
    "dup": {
      "discovered": true,
      "members": [
        {"source": "a", "repo": "o/r"},
        {"source": "a", "repo": "o/r"},
        {"source": "a", "repo": "o/r"}
      ]
    }
  }
}`
	cfg, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	w := cfg.Workspaces["dup"]
	if got := len(w.Members); got != 1 {
		t.Errorf("members after Load = %d, want 1 (deduped)", got)
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

func TestCheckGlobalGitignore_DefaultEnabledOnFreshConfig(t *testing.T) {
	var g GlobalConfig
	if !g.ShouldCheckGlobalGitignore() {
		t.Error("ShouldCheckGlobalGitignore must default to true when field is nil")
	}
	if g.CheckGlobalGitignore != nil {
		t.Errorf("raw field must remain nil, got %v", g.CheckGlobalGitignore)
	}
}

func TestCheckGlobalGitignore_ExplicitFalseHonored(t *testing.T) {
	f := false
	g := GlobalConfig{CheckGlobalGitignore: &f}
	if g.ShouldCheckGlobalGitignore() {
		t.Error("pointer-to-false must disable the automatic check")
	}
}

func TestCheckGlobalGitignore_RoundTripExplicitFalse(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "gitbox.json")

	f := false
	cfg := &Config{
		Schema:   "https://raw.githubusercontent.com/LuisPalacios/gitbox/main/json/gitbox.schema.json",
		Version:  2,
		Global:   GlobalConfig{Folder: "~/git", CheckGlobalGitignore: &f},
		Accounts: map[string]Account{},
		Sources:  map[string]Source{},
	}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Global.CheckGlobalGitignore == nil {
		t.Fatal("round-tripped field unexpectedly nil")
	}
	if *loaded.Global.CheckGlobalGitignore != false {
		t.Errorf("round-trip lost explicit false, got %v", *loaded.Global.CheckGlobalGitignore)
	}
	if loaded.Global.ShouldCheckGlobalGitignore() {
		t.Error("ShouldCheckGlobalGitignore must return false after round-trip")
	}
}

func TestCheckGlobalGitignore_RoundTripExplicitTrue(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "gitbox.json")

	tru := true
	cfg := &Config{
		Schema:   "https://raw.githubusercontent.com/LuisPalacios/gitbox/main/json/gitbox.schema.json",
		Version:  2,
		Global:   GlobalConfig{Folder: "~/git", CheckGlobalGitignore: &tru},
		Accounts: map[string]Account{},
		Sources:  map[string]Source{},
	}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Global.CheckGlobalGitignore == nil {
		t.Fatal("round-tripped field unexpectedly nil")
	}
	if *loaded.Global.CheckGlobalGitignore != true {
		t.Errorf("round-trip lost explicit true, got %v", *loaded.Global.CheckGlobalGitignore)
	}
	if !loaded.Global.ShouldCheckGlobalGitignore() {
		t.Error("ShouldCheckGlobalGitignore must return true after round-trip")
	}
}
