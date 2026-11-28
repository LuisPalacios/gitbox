package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/heal"
)

func TestIdentityProofVerificationAndRepair(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "global"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	var checks int
	path := filepath.Join(root, "clone")
	cfg := &config.Config{
		Version: config.CurrentVersion,
		Global:  config.GlobalConfig{Roots: []string{root}},
		Accounts: map[string]config.Account{
			"acct": {Provider: "gitea", URL: "https://host.example.invalid", Username: "alice", Name: "Alice", Email: "alice@example.invalid", DefaultCredentialType: "ssh"},
		},
		Sources: map[string]config.Source{
			"source": {Account: "acct", Repos: map[string]config.Repo{"org/repo": {CloneFolder: path}}},
		},
	}
	run := func(dir string, args ...string) {
		t.Helper()
		if _, err := git.Run(t.Context(), git.Request{Dir: dir, Args: args}); err != nil {
			t.Fatal(err)
		}
	}
	run(root, "init", "-q", path)
	run(path, "remote", "add", "origin", "git@wrong.example.invalid:org/repo.git")
	run(path, "config", "user.name", "Wrong")
	run(path, "config", "user.email", "wrong@example.invalid")
	run(path, "config", "--add", "credential.helper", "")
	gitPath := filepath.Join(path, ".git", "config")
	cfgPath := filepath.Join(root, "gitbox.json")
	if err := config.Save(cfg, cfgPath); err != nil {
		t.Fatal(err)
	}
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	beforeGit, beforeApp := read(gitPath), read(cfgPath)

	a := newTestApp(
		withCfg(cfg),
		withCfgPath(cfgPath),
		withCfgLoaded(),
		withRefreshHooks(&refreshHooks{
			credential: func(context.Context, config.Account, string, *config.Config) credential.StatusResult {
				checks++
				return refreshCredentialOK()
			},
		}),
	)
	defer a.stopRefresh()
	proof := func() IdentityProofDTO {
		t.Helper()
		p, err := a.GetIdentityProof("source", "org/repo")
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if p := proof(); p.Verdict != "unsafe" || p.LastVerified != nil || checks != 0 {
		t.Fatalf("before explicit actions: %+v (credential checks: %d)", p, checks)
	}
	if !bytes.Equal(beforeGit, read(gitPath)) || !bytes.Equal(beforeApp, read(cfgPath)) {
		t.Fatal("proof changed config bytes")
	}
	if err := a.RepairRepoIdentity("source", "org/repo"); err != nil {
		t.Fatal(err)
	}
	if p := proof(); p.Verdict != "warning" || p.Reason != "unverified" || checks != 0 {
		t.Fatalf("after repair: %+v (credential checks: %d)", p, checks)
	}
	if got := a.CredentialVerify("acct"); got.Primary != "ok" {
		t.Fatalf("credential verification = %+v", got)
	}
	if p := proof(); p.Verdict != "safe" || p.LastVerified == nil || checks != 1 {
		t.Fatalf("after verification: %+v (credential checks: %d)", p, checks)
	}
	a.refreshHooks.credential = func(context.Context, config.Account, string, *config.Config) credential.StatusResult {
		checks++
		return credential.StatusResult{Primary: credential.StatusError, Overall: credential.StatusError}
	}
	_ = a.CredentialVerify("acct")
	if p := proof(); p.Verdict != "warning" || p.Reason != "checkFailed" || p.LastVerified == nil || checks != 2 {
		t.Fatalf("after failed check: %+v (credential checks: %d)", p, checks)
	}
	a.invalidateCredential("acct")
	if p := proof(); p.Verdict != "warning" || p.LastVerified != nil || checks != 2 {
		t.Fatalf("invalidated proof: %+v (credential checks: %d)", p, checks)
	}
	configured := heal.ExpectedOriginURL(cfg.Accounts["acct"], "org/repo", "ssh")
	if origin, err := git.ResolvedOrigin(t.Context(), path); err != nil || origin != configured {
		t.Fatalf("repair origin = %q (%v), want %q", origin, err, configured)
	}
	if !bytes.Equal(beforeApp, read(cfgPath)) {
		t.Fatal("proof and repair changed gitbox.json")
	}

	if err := a.OpenRepoConfigInEditor("source", "../unknown"); err == nil {
		t.Fatal("untracked repo was accepted for editing")
	}
	h := startHostile(t, nil)
	if err := h.app.OpenRepoConfigInEditor("source", "org/repo"); !codeIs(err, "not_available_remote") {
		t.Fatalf("remote editor guard = %v", err)
	}
}
