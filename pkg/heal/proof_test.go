package heal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/identity"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

func TestProofMixedFleet(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "global"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	const canary = "gbx-proof-canary"
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	recent := Verification{CheckedAt: now.Add(-time.Hour)}
	accounts := map[string]config.Account{
		"ssh":   {URL: "https://ssh.example.invalid", Username: "alice", Name: "Alice", Email: "alice@example.invalid", DefaultCredentialType: "ssh"},
		"token": {URL: "https://git.example.invalid", Username: "bob", Name: "Bob", Email: "bob@example.invalid", DefaultCredentialType: "token"},
		"gcm":   {URL: "https://git.example.invalid", Username: "carol", Name: "Carol", Email: "carol@example.invalid", DefaultCredentialType: "gcm"},
	}
	cfg := &config.Config{Global: config.GlobalConfig{Roots: []string{root}}, Accounts: accounts, Sources: map[string]config.Source{}}
	for key := range accounts {
		cfg.Sources[key] = config.Source{Account: key, Repos: make(map[string]config.Repo)}
	}

	file := credential.CredentialFilePath("token")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(canary), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, account, verdict, reason string
		verification                   Verification
		wantKey, wantScope             string
	}{
		{name: "ssh-safe", account: "ssh", verdict: "safe", verification: recent},
		{name: "project-name", account: "ssh", verdict: "safe", verification: recent},
		{name: "wrong-host", account: "token", verdict: "unsafe", reason: "mismatch", verification: recent, wantKey: "remote.origin.url", wantScope: "local"},
		{name: "include-helper", account: "token", verdict: "unsafe", reason: "mismatch", verification: recent, wantKey: "credential.helper", wantScope: "include"},
		{name: "scoped-helper", account: "token", verdict: "unsafe", reason: "mismatch", verification: recent, wantKey: "credential.*.helper", wantScope: "local"},
		{name: "other-host-helper", account: "gcm", verdict: "safe", verification: recent},
		{name: "scoped-reset", account: "gcm", verdict: "safe", verification: recent},
		{name: "rewritten-origin", account: "gcm", verdict: "unsafe", reason: "mismatch", verification: recent, wantKey: "url.*.insteadOf"},
		{name: "stale", account: "ssh", verdict: "warning", reason: "stale", verification: Verification{CheckedAt: now.Add(-25 * time.Hour)}},
		{name: "expiring", account: "token", verdict: "warning", reason: "expires", verification: Verification{CheckedAt: now, ExpiresAt: now.Add(14 * 24 * time.Hour)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			acct := cfg.Accounts[tc.account]
			repoKey := "team/" + tc.name
			entry := config.Repo{}
			if tc.name == "project-name" {
				entry.Name = "Project author"
			}
			cfg.Sources[tc.account].Repos[repoKey] = entry
			path := status.RepoPath(cfg, tc.account, repoKey, entry)
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
			run := func(args ...string) {
				t.Helper()
				if err := gitRun(path, args...); err != nil {
					t.Fatal(err)
				}
			}
			run("init", "-q")
			origin := ExpectedOriginURL(acct, repoKey, acct.DefaultCredentialType)
			run("remote", "add", "origin", origin)
			name, email := identity.ResolveIdentity(entry, acct)
			run("config", "user.name", name)
			run("config", "user.email", email)
			for _, helper := range credential.RepoHelpers(tc.account, acct.DefaultCredentialType, cfg.Global) {
				run("config", "--add", "credential.helper", helper)
			}
			switch tc.name {
			case "wrong-host":
				run("remote", "set-url", "origin", fmt.Sprintf("https://%s:%s@wrong.example.invalid/team/%s.git?secret=%s#%s", canary, canary, canary, canary, canary))
			case "include-helper":
				included := filepath.Join(path, "override.gitconfig")
				if err := os.WriteFile(included, []byte("[credential]\n  helper = !"+canary+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				run("config", "--add", "include.path", included)
			case "scoped-helper":
				run("config", "--add", "credential.https://*.example.invalid/team.helper", "!"+canary)
				run("config", "--add", "credential.https://bob@git.example.invalid/team.helper", "!"+canary)
			case "other-host-helper":
				run("config", "--add", "credential.https://other.example.invalid.helper", "!"+canary)
				run("config", "--add", "credential.other.example.invalid.helper", "!"+canary)
				run("config", "--add", "credential.https://other@git.example.invalid.helper", "!"+canary)
			case "scoped-reset":
				key := "credential.https://carol@git.example.invalid.helper"
				run("config", "--add", key, "!"+canary)
				run("config", "--add", key, "")
				run("config", "--add", key, "manager")
			case "rewritten-origin":
				// A rewrite can redirect a well-formed stored origin.
				run("config", "url.https://wrong.example.invalid/.insteadOf", "https://carol@git.example.invalid/")
			}

			configPath := filepath.Join(path, ".git", "config")
			before, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			proof := Proof(t.Context(), cfg, tc.account, repoKey, tc.verification, now)
			if proof.Verdict != tc.verdict || proof.Reason != tc.reason {
				t.Errorf("proof = %+v, want %s/%s", proof, tc.verdict, tc.reason)
			}
			if tc.wantKey != "" && !slices.Contains(proof.Overrides, ProofOverride{Scope: tc.wantScope, Key: tc.wantKey, Value: "redacted"}) {
				t.Errorf("overrides = %+v, want %q in %q", proof.Overrides, tc.wantKey, tc.wantScope)
			}
			if tc.name == "project-name" && (proof.NameSource != "repo" || proof.EmailSource != "account") {
				t.Errorf("source = %s/%s", proof.NameSource, proof.EmailSource)
			}
			if tc.name == "scoped-helper" && !slices.Equal(proof.HelperChain, []string{"reset", "store", "redacted", "redacted"}) {
				t.Errorf("ordered helper chain = %v", proof.HelperChain)
			}
			if tc.name == "expiring" && (proof.CredentialFile != file || proof.LastVerified == nil || !proof.LastVerified.Equal(now)) {
				t.Errorf("missing verification or credential path: %+v", proof)
			}
			if tc.name == "wrong-host" && (!strings.Contains(proof.OriginURL, "wrong.example.invalid") || !strings.Contains(proof.OriginURL, "%E2%80%A6")) {
				t.Errorf("redacted origin = %q", proof.OriginURL)
			}
			encoded, err := json.Marshal(proof)
			if err != nil || bytes.Contains(encoded, []byte(canary)) {
				t.Fatalf("proof serialization exposed the canary or failed: %v", err)
			}
			after, err := os.ReadFile(configPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("identity proof modified the clone config")
			}
			got, err := os.ReadFile(file)
			if err != nil || string(got) != canary {
				t.Fatal("identity proof modified the credential file")
			}
		})
	}
	if p := Proof(t.Context(), cfg, "ssh", "team/ssh-safe", Verification{}, now); p.Verdict != "warning" || p.Reason != "unverified" {
		t.Errorf("unverified clone = %+v", p)
	}
	if p := Proof(t.Context(), cfg, "ssh", "../elsewhere", Verification{}, now); p.Reason != "unavailable" || p.Account != "" {
		t.Errorf("untracked clone = %+v", p)
	}
}

func TestProofWorktreeScope(t *testing.T) {
	cfg, path := healFixture(t)
	_ = Repo(cfg, "repos", "team/project")
	run := func(dir string, args ...string) {
		t.Helper()
		if _, err := git.Run(t.Context(), git.Request{Dir: dir, Args: args}); err != nil {
			t.Fatal(err)
		}
	}
	run(path, "commit", "--allow-empty", "-m", "Seed")
	run(path, "config", "extensions.worktreeConfig", "true")
	worktree := filepath.Join(t.TempDir(), "linked")
	run(path, "worktree", "add", "-b", "linked", worktree)
	run(worktree, "config", "--worktree", "user.email", "unexpected")
	run(worktree, "config", "--worktree", "author.name", "unexpected")
	t.Setenv("GIT_COMMITTER_EMAIL", "unexpected")
	cfg.Sources["repos"].Repos["team/project"] = config.Repo{CloneFolder: worktree}
	p := Proof(t.Context(), cfg, "repos", "team/project", Verification{}, time.Now())
	if p.Verdict != "unsafe" ||
		!slices.Contains(p.Overrides, ProofOverride{Scope: "worktree", Key: "user.email", Value: "redacted"}) ||
		!slices.Contains(p.Overrides, ProofOverride{Scope: "worktree", Key: "author.name", Value: "redacted"}) ||
		!slices.Contains(p.Overrides, ProofOverride{Scope: "environment", Key: "GIT_COMMITTER_EMAIL", Value: "redacted"}) {
		t.Errorf("linked worktree proof = %+v", p)
	}
	resolved, err := git.CommonConfigPath(t.Context(), worktree)
	want := filepath.Join(path, ".git", "config")
	// Git can canonicalize the system temp path, e.g. /var to /private/var.
	want, _ = filepath.EvalSymlinks(want)
	resolved, _ = filepath.EvalSymlinks(resolved)
	if err != nil || resolved != want {
		t.Errorf("shared config path = %q, want %q (%v)", resolved, want, err)
	}
}
