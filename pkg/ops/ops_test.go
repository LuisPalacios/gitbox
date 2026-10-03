package ops

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// isolate points every file gitbox and git might touch at a temp dir:
// the gitbox config root (credential files), git's global config, and the
// SSH folder used by the returned config. Nothing on the real host changes.
func isolate(t *testing.T) (string, *config.Config) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	globalCfg := filepath.Join(root, "gitconfig")
	if err := os.WriteFile(globalCfg, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalCfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_TOKEN", "")

	cfg := &config.Config{
		Version: config.CurrentVersion,
		Global: config.GlobalConfig{
			Folder:        filepath.Join(root, "git"),
			CredentialSSH: &config.SSHGlobal{SSHFolder: filepath.Join(root, "ssh")},
		},
		Accounts: map[string]config.Account{},
		Sources:  map[string]config.Source{},
	}
	return root, cfg
}

func testAccount(credType string) config.Account {
	return config.Account{
		Provider:              "github",
		URL:                   "https://github.com",
		Username:              "alice",
		Name:                  "Alice",
		Email:                 "alice@example.com",
		DefaultCredentialType: credType,
	}
}

// gitRun runs git in dir and fails the test on error.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(git.GitBin(), args...)
	cmd.Dir = dir
	cmd.Env = git.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// makeClone creates a git repo at the clone path of sourceKey/repoKey with
// the given origin URL and returns its path.
func makeClone(t *testing.T, cfg *config.Config, sourceKey, repoKey, origin string) string {
	t.Helper()
	plan, err := PlanClone(cfg, sourceKey, repoKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(plan.Dest, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, plan.Dest, "init", "-q")
	gitRun(t, plan.Dest, "remote", "add", "origin", origin)
	return plan.Dest
}

func TestAddAccount_DefaultsAndSource(t *testing.T) {
	_, cfg := isolate(t)

	if err := AddAccount(cfg, "gh", testAccount("")); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	acct := cfg.Accounts["gh"]
	if acct.DefaultCredentialType != "gcm" || acct.GCM == nil || acct.GCM.Provider != "github" {
		t.Errorf("default credential not applied: %+v", acct)
	}
	if src, ok := cfg.Sources["gh"]; !ok || src.Account != "gh" {
		t.Errorf("matching source missing: %+v", cfg.Sources)
	}

	if err := AddAccount(cfg, "gh-ssh", testAccount("ssh")); err != nil {
		t.Fatalf("AddAccount ssh: %v", err)
	}
	ssh := cfg.Accounts["gh-ssh"].SSH
	if ssh == nil || ssh.Host != "gitbox-gh-ssh" || ssh.Hostname != "github.com" || ssh.KeyType != "ed25519" {
		t.Errorf("ssh sub-object = %+v", ssh)
	}
}

func TestRenameAccount_MigratesEverything(t *testing.T) {
	_, cfg := isolate(t)
	if err := AddAccount(cfg, "old", testAccount("ssh")); err != nil {
		t.Fatal(err)
	}
	if err := cfg.AddRepo("old", "alice/repo", config.Repo{}); err != nil {
		t.Fatal(err)
	}

	sshFolder := credential.SSHFolder(cfg)
	oldKey := credential.SSHKeyPath(sshFolder, "old")
	if err := os.MkdirAll(sshFolder, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{oldKey, oldKey + ".pub"} {
		if err := os.WriteFile(f, []byte("key"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := credential.StoreToken("old", "pat-123"); err != nil {
		t.Fatal(err)
	}
	clone := makeClone(t, cfg, "old", "alice/repo", "git@gitbox-old:alice/repo.git")

	if err := RenameAccount(cfg, "old", "new"); err != nil {
		t.Fatalf("RenameAccount: %v", err)
	}

	if _, ok := cfg.Accounts["new"]; !ok {
		t.Fatal("account not renamed")
	}
	if src, ok := cfg.Sources["new"]; !ok || src.Account != "new" {
		t.Errorf("source not renamed: %+v", cfg.Sources)
	}
	if got := cfg.Accounts["new"].SSH.Host; got != "gitbox-new" {
		t.Errorf("ssh host = %q, want gitbox-new", got)
	}
	newKey := credential.SSHKeyPath(sshFolder, "new")
	if _, err := os.Stat(newKey); err != nil {
		t.Errorf("ssh key not renamed: %v", err)
	}
	if found, _ := credential.FindSSHConfigEntry(sshFolder, "gitbox-new"); !found {
		t.Error("ssh config entry for the new alias missing")
	}
	if tok, err := credential.GetToken("new"); err != nil || tok != "pat-123" {
		t.Errorf("token not migrated: %q, %v", tok, err)
	}
	if _, err := credential.GetToken("old"); err == nil {
		t.Error("old token still present")
	}

	// The default source folder moved with the key.
	movedClone := strings.Replace(clone, string(os.PathSeparator)+"old"+string(os.PathSeparator),
		string(os.PathSeparator)+"new"+string(os.PathSeparator), 1)
	if !git.IsRepo(movedClone) {
		t.Fatalf("clone not found at %s", movedClone)
	}

	// Reconfiguring after the rename points origin at the new SSH alias.
	ReconfigureClones(cfg, "new")
	if got := gitRun(t, movedClone, "remote", "get-url", "origin"); got != "git@gitbox-new:alice/repo.git" {
		t.Errorf("origin after reconfigure = %q", got)
	}
}

func TestRenameAccount_Rejects(t *testing.T) {
	_, cfg := isolate(t)
	if err := AddAccount(cfg, "a", testAccount("gcm")); err != nil {
		t.Fatal(err)
	}
	if err := AddAccount(cfg, "b", testAccount("gcm")); err != nil {
		t.Fatal(err)
	}
	if err := RenameAccount(cfg, "a", "b"); err == nil {
		t.Error("expected error renaming onto an existing key")
	}
	if err := RenameAccount(cfg, "a", "bad key"); err == nil {
		t.Error("expected error for an invalid key")
	}
	if err := RenameAccount(cfg, "missing", "c"); err == nil {
		t.Error("expected error for a missing account")
	}
	if err := RenameAccount(cfg, "a", "a"); err != nil {
		t.Errorf("same-key rename should be a no-op, got %v", err)
	}
}

func TestChangeCredentialType_TokenToSSH(t *testing.T) {
	_, cfg := isolate(t)
	if err := AddAccount(cfg, "gh", testAccount("token")); err != nil {
		t.Fatal(err)
	}
	if err := credential.StoreToken("gh", "pat-xyz"); err != nil {
		t.Fatal(err)
	}

	if err := ChangeCredentialType(cfg, "gh", "ssh"); err != nil {
		t.Fatalf("ChangeCredentialType: %v", err)
	}
	acct := cfg.Accounts["gh"]
	if acct.DefaultCredentialType != "ssh" || acct.SSH == nil || acct.GCM != nil {
		t.Errorf("account after switch = %+v", acct)
	}
	if _, err := credential.GetToken("gh"); err == nil {
		t.Error("token artifact survived the switch")
	}
	if err := ChangeCredentialType(cfg, "gh", "carrier-pigeon"); err == nil {
		t.Error("expected error for an unknown credential type")
	}
}

// GCM accounts may hold a companion PAT for API discovery. Deleting the
// credential must remove it too (behavior carried over from the TUI).
func TestDeleteCredential_GCMRemovesCompanionPAT(t *testing.T) {
	_, cfg := isolate(t)
	acct := testAccount("gcm")
	acct.URL = "https://git.invalid"
	acct.Username = "nobody"
	if err := AddAccount(cfg, "gh", acct); err != nil {
		t.Fatal(err)
	}
	if err := credential.StoreToken("gh", "companion"); err != nil {
		t.Fatal(err)
	}

	msgs, err := DeleteCredential(cfg, "gh")
	if err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}
	if _, err := credential.GetToken("gh"); err == nil {
		t.Error("companion PAT survived credential deletion")
	}
	if !strings.Contains(strings.Join(msgs, "\n"), "companion PAT") {
		t.Errorf("messages don't mention the PAT: %v", msgs)
	}
	got := cfg.Accounts["gh"]
	if got.DefaultCredentialType != "" || got.GCM != nil {
		t.Errorf("credential config not cleared: %+v", got)
	}

	msgs, err = DeleteCredential(cfg, "gh")
	if err != nil || len(msgs) != 1 || msgs[0] != "No credential configured" {
		t.Errorf("second delete = %v, %v", msgs, err)
	}
}

func TestDeleteAccount_RemovesClonesAndCascades(t *testing.T) {
	_, cfg := isolate(t)
	if err := AddAccount(cfg, "gh", testAccount("gcm")); err != nil {
		t.Fatal(err)
	}
	if err := cfg.AddRepo("gh", "alice/repo", config.Repo{}); err != nil {
		t.Fatal(err)
	}
	clone := makeClone(t, cfg, "gh", "alice/repo", "https://alice@github.com/alice/repo.git")
	if CountClonedRepos(cfg, "gh") != 1 {
		t.Fatalf("CountClonedRepos = %d, want 1", CountClonedRepos(cfg, "gh"))
	}

	if err := DeleteAccount(cfg, "gh"); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if _, err := os.Stat(clone); err == nil {
		t.Error("clone folder still on disk")
	}
	if _, ok := cfg.Accounts["gh"]; ok {
		t.Error("account still in config")
	}
	if _, ok := cfg.Sources["gh"]; ok {
		t.Error("source still in config")
	}
	if err := DeleteAccount(cfg, "gh"); err == nil {
		t.Error("expected error deleting a missing account")
	}
}

func TestDeleteRepo(t *testing.T) {
	_, cfg := isolate(t)
	if err := AddAccount(cfg, "gh", testAccount("gcm")); err != nil {
		t.Fatal(err)
	}
	if err := cfg.AddRepo("gh", "alice/repo", config.Repo{}); err != nil {
		t.Fatal(err)
	}
	clone := makeClone(t, cfg, "gh", "alice/repo", "https://alice@github.com/alice/repo.git")

	if err := DeleteRepo(cfg, "gh", "alice/repo"); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
	if _, err := os.Stat(clone); err == nil {
		t.Error("clone folder still on disk")
	}
	if _, ok := cfg.Sources["gh"].Repos["alice/repo"]; ok {
		t.Error("repo still in config")
	}
}

func TestPlanClone(t *testing.T) {
	_, cfg := isolate(t)
	if err := AddAccount(cfg, "tok", testAccount("token")); err != nil {
		t.Fatal(err)
	}
	if err := AddAccount(cfg, "ssh", testAccount("ssh")); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"tok", "ssh"} {
		if err := cfg.AddRepo(k, "alice/repo", config.Repo{}); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GITBOX_TOKEN_TOK", "s3cret")

	plan, err := PlanClone(cfg, "tok", "alice/repo")
	if err != nil {
		t.Fatalf("PlanClone token: %v", err)
	}
	if plan.URL != "https://alice:s3cret@github.com/alice/repo.git" {
		t.Errorf("token clone URL = %q", plan.URL)
	}
	if len(plan.Opts.ConfigArgs) != 1 || plan.Opts.ConfigArgs[0] != "credential.helper=" {
		t.Errorf("token clone must cancel the global helper, got %v", plan.Opts.ConfigArgs)
	}
	wantDest := filepath.Join(cfg.Global.Folder, "tok", "alice", "repo")
	if plan.Dest != wantDest {
		t.Errorf("dest = %q, want %q", plan.Dest, wantDest)
	}

	plan, err = PlanClone(cfg, "ssh", "alice/repo")
	if err != nil {
		t.Fatalf("PlanClone ssh: %v", err)
	}
	if plan.URL != "git@gitbox-ssh:alice/repo.git" || len(plan.Opts.ConfigArgs) != 0 {
		t.Errorf("ssh plan = %+v", plan)
	}

	if _, err := PlanClone(cfg, "nope", "alice/repo"); err == nil {
		t.Error("expected error for a missing source")
	}
	if _, err := PlanClone(cfg, "ssh", "alice/nope"); err == nil {
		t.Error("expected error for a missing repo")
	}
}

func TestReconfigureClones_HealsIdentityAndOrigin(t *testing.T) {
	_, cfg := isolate(t)
	if err := AddAccount(cfg, "gh", testAccount("gcm")); err != nil {
		t.Fatal(err)
	}
	if err := cfg.AddRepo("gh", "alice/repo", config.Repo{}); err != nil {
		t.Fatal(err)
	}
	clone := makeClone(t, cfg, "gh", "alice/repo", "https://github.com/wrong/place.git")

	reports := ReconfigureClones(cfg, "gh")
	if len(reports) != 1 {
		t.Fatalf("reports = %+v, want one", reports)
	}
	if got := gitRun(t, clone, "remote", "get-url", "origin"); got != "https://alice@github.com/alice/repo.git" {
		t.Errorf("origin = %q", got)
	}
	if got := gitRun(t, clone, "config", "user.email"); got != "alice@example.com" {
		t.Errorf("user.email = %q", got)
	}

	// A second pass has nothing left to fix.
	for _, r := range ReconfigureClones(cfg, "gh") {
		if len(r.Fixed) > 0 {
			t.Errorf("second pass fixed again: %+v", r)
		}
	}
}

func TestAddDiscoveredRepos(t *testing.T) {
	_, cfg := isolate(t)
	if err := AddAccount(cfg, "gh", testAccount("gcm")); err != nil {
		t.Fatal(err)
	}
	// Point the account at a differently named source to exercise lookup.
	if err := cfg.RenameSource("gh", "gh-src"); err != nil {
		t.Fatal(err)
	}

	if err := AddDiscoveredRepos(cfg, "gh", []string{"alice/a", "alice/b"}); err != nil {
		t.Fatalf("AddDiscoveredRepos by account: %v", err)
	}
	if err := AddDiscoveredRepos(cfg, "gh-src", []string{"alice/a", "alice/c"}); err != nil {
		t.Fatalf("AddDiscoveredRepos by source with a duplicate: %v", err)
	}
	if n := len(cfg.Sources["gh-src"].Repos); n != 3 {
		t.Errorf("repos = %d, want 3", n)
	}
	if err := AddDiscoveredRepos(cfg, "nobody", []string{"x/y"}); err == nil {
		t.Error("expected error for an unknown key")
	}
}

func TestHostnameFromURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com":            "github.com",
		"https://git.example.com:3000/": "git.example.com",
		"http://forgejo.local":          "forgejo.local",
		"github.com":                    "github.com",
	}
	for in, want := range cases {
		if got := HostnameFromURL(in); got != want {
			t.Errorf("HostnameFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGCMProviderFor(t *testing.T) {
	for in, want := range map[string]string{"github": "github", "gitlab": "gitlab", "bitbucket": "bitbucket", "forgejo": "generic", "gitea": "generic"} {
		if got := GCMProviderFor(in); got != want {
			t.Errorf("GCMProviderFor(%q) = %q, want %q", in, got, want)
		}
	}
}

// A failed folder move must abort the rename before anything else changes,
// so config, credentials and disk stay consistent.
func TestRenameAccount_FolderMoveFailureChangesNothing(t *testing.T) {
	setup := func(t *testing.T) *config.Config {
		t.Helper()
		_, cfg := isolate(t)
		if err := AddAccount(cfg, "old", testAccount("token")); err != nil {
			t.Fatal(err)
		}
		if err := cfg.AddRepo("old", "alice/repo", config.Repo{}); err != nil {
			t.Fatal(err)
		}
		if err := credential.StoreToken("old", "pat-123"); err != nil {
			t.Fatal(err)
		}
		makeClone(t, cfg, "old", "alice/repo", "https://alice@github.com/alice/repo.git")
		return cfg
	}
	assertUnchanged := func(t *testing.T, cfg *config.Config) {
		t.Helper()
		if _, ok := cfg.Accounts["old"]; !ok {
			t.Error("account was renamed despite the failed folder move")
		}
		if _, ok := cfg.Sources["old"]; !ok {
			t.Error("source was renamed despite the failed folder move")
		}
		if tok, err := credential.GetToken("old"); err != nil || tok != "pat-123" {
			t.Errorf("token moved despite the failed folder move: %q, %v", tok, err)
		}
	}

	t.Run("destination exists", func(t *testing.T) {
		cfg := setup(t)
		if err := os.MkdirAll(filepath.Join(cfg.Global.Folder, "new"), 0o755); err != nil {
			t.Fatal(err)
		}
		err := RenameAccount(cfg, "old", "new")
		if err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("err = %v, want an 'already exists' error", err)
		}
		assertUnchanged(t, cfg)
	})

	t.Run("folder in use", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("only Windows blocks renaming a folder with open handles inside")
		}
		cfg := setup(t)
		f, err := os.Open(filepath.Join(cfg.Global.Folder, "old", "alice", "repo", ".git", "HEAD"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := RenameAccount(cfg, "old", "new"); err == nil {
			t.Fatal("expected an error while a file inside the folder is open")
		}
		assertUnchanged(t, cfg)
	})
}

func TestRenameAccount_NoFolderYet(t *testing.T) {
	_, cfg := isolate(t)
	if err := AddAccount(cfg, "old", testAccount("gcm")); err != nil {
		t.Fatal(err)
	}
	if err := RenameAccount(cfg, "old", "new"); err != nil {
		t.Fatalf("rename without a folder on disk: %v", err)
	}
	if _, ok := cfg.Sources["new"]; !ok {
		t.Error("source not renamed")
	}
}
