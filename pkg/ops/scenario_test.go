package ops

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// TestScenario_FullLifecycle drives the library through the whole account
// lifecycle against a real provider: accounts → credential check → discover
// → clone → status → pull → fetch → edit → mirrors → re-clone → rename →
// delete. Every step saves and reloads the config, the same way the GUI
// persists after each action.
//
// Requires test-gitbox.json with real account credentials.
func TestScenario_FullLifecycle(t *testing.T) {
	fixture := requireIntegration(t)
	acctKey, srcKey, repoKey, ok := fixture.firstAccountWithRepos()
	if !ok {
		t.Skip("no account with repos and token in test fixture")
	}
	acct := fixture.Config.Accounts[acctKey]

	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	cfgPath := filepath.Join(tmp, "gitbox", "gitbox.json")
	cfg := &config.Config{
		Version: config.CurrentVersion,
		Global: config.GlobalConfig{
			Folder:        filepath.Join(tmp, "git"),
			CredentialSSH: fixture.Config.Global.CredentialSSH,
		},
		Accounts: map[string]config.Account{},
		Sources:  map[string]config.Source{},
		Mirrors:  map[string]config.Mirror{},
	}

	// persist saves and reloads, so each step also proves the config
	// round-trips through disk.
	persist := func(t *testing.T) {
		t.Helper()
		if err := config.Save(cfg, cfgPath); err != nil {
			t.Fatalf("save: %v", err)
		}
		loaded, err := config.Load(cfgPath)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		cfg = loaded
	}
	clonePath := func(t *testing.T, source string) string {
		t.Helper()
		plan, err := PlanClone(cfg, source, repoKey)
		if err != nil {
			t.Fatal(err)
		}
		return plan.Dest
	}
	// cloneRepo mirrors the GUI clone flow: plan, run, then heal.
	cloneRepo := func(t *testing.T) (string, heal.Report) {
		t.Helper()
		plan, err := PlanClone(cfg, srcKey, repoKey)
		if err != nil {
			t.Fatalf("plan clone: %v", err)
		}
		if err := plan.Run(nil); err != nil {
			t.Fatalf("clone: %v", err)
		}
		return plan.Dest, heal.Repo(cfg, srcKey, repoKey)
	}
	ctx := func() context.Context {
		c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		t.Cleanup(cancel)
		return c
	}

	t.Run("01_add_accounts", func(t *testing.T) {
		// The real account keeps the fixture's credential settings (SSH
		// host aliases must match the fixture's ssh config).
		if err := cfg.AddAccount(acctKey, acct); err != nil {
			t.Fatal(err)
		}
		if err := cfg.AddSource(srcKey, config.Source{Account: acctKey, Repos: map[string]config.Repo{}}); err != nil {
			t.Fatal(err)
		}
		dummy := config.Account{
			Provider: "gitlab", URL: "https://gitlab.com", Username: "demo",
			Name: "Demo User", Email: "demo@example.com", DefaultCredentialType: "token",
		}
		if err := AddAccount(cfg, "dummy-gitlab", dummy); err != nil {
			t.Fatal(err)
		}
		persist(t)
		if _, ok := cfg.Accounts["dummy-gitlab"]; !ok {
			t.Fatal("dummy account lost on reload")
		}
	})

	t.Run("02_credential_check", func(t *testing.T) {
		res := credential.Check(cfg.Accounts[acctKey], acctKey, cfg)
		if res.Overall == credential.StatusError {
			t.Fatalf("credential check failed: %s", res.PrimaryDetail)
		}
	})

	t.Run("03_discover", func(t *testing.T) {
		repos, err := ListRemoteRepos(ctx(), cfg.Accounts[acctKey], acctKey)
		if err != nil {
			t.Fatalf("discover: %v", err)
		}
		if len(repos) == 0 {
			t.Fatal("discover returned no repos")
		}
		if _, err := ListAccountOrgs(ctx(), cfg.Accounts[acctKey], acctKey); err != nil {
			t.Fatalf("list orgs: %v", err)
		}
	})

	t.Run("04_add_repo", func(t *testing.T) {
		if err := AddDiscoveredRepos(cfg, srcKey, []string{repoKey}); err != nil {
			t.Fatal(err)
		}
		persist(t)
	})

	t.Run("05_clone", func(t *testing.T) {
		dest, report := cloneRepo(t)
		if !git.IsRepo(dest) {
			t.Fatalf("no clone at %s", dest)
		}
		if len(report.Warnings) > 0 {
			t.Logf("heal warnings: %v", report.Warnings)
		}
		if name, _ := git.ConfigGet(dest, "user.name"); name != acct.Name {
			t.Errorf("user.name = %q, want %q", name, acct.Name)
		}
		origin, _ := git.RemoteURL(dest)
		if _, hasPW := parseUserinfoPassword(origin); hasPW {
			t.Errorf("origin URL kept a secret: %s", origin)
		}
	})

	t.Run("06_status", func(t *testing.T) {
		statuses := status.CheckAll(cfg)
		if len(statuses) == 0 {
			t.Fatal("no status entries")
		}
		for _, s := range statuses {
			if s.State == status.Error {
				t.Errorf("status error for %s: %s", s.Path, s.ErrorMsg)
			}
		}
	})

	t.Run("07_pull_and_fetch", func(t *testing.T) {
		dest := clonePath(t, srcKey)
		if err := git.PullQuiet(dest); err != nil {
			t.Errorf("pull: %v", err)
		}
		if out, err := git.FetchCaptured(dest); err != nil {
			t.Errorf("fetch: %v\n%s", err, out)
		}
	})

	t.Run("08_update_account_reconfigures_clones", func(t *testing.T) {
		a := cfg.Accounts[acctKey]
		a.Email = "updated@example.com"
		if err := cfg.UpdateAccount(acctKey, a); err != nil {
			t.Fatal(err)
		}
		persist(t)
		ReconfigureClones(cfg, acctKey)
		if email, _ := git.ConfigGet(clonePath(t, srcKey), "user.email"); email != "updated@example.com" {
			t.Errorf("user.email after update = %q", email)
		}
	})

	t.Run("09_mirror_crud", func(t *testing.T) {
		if err := cfg.AddMirror("test-mirror", config.Mirror{AccountSrc: acctKey, AccountDst: "dummy-gitlab"}); err != nil {
			t.Fatal(err)
		}
		if err := cfg.AddMirrorRepo("test-mirror", repoKey, config.MirrorRepo{Direction: "push", Origin: "src"}); err != nil {
			t.Fatal(err)
		}
		persist(t)
		if _, ok := cfg.Mirrors["test-mirror"].Repos[repoKey]; !ok {
			t.Fatal("mirror repo lost on reload")
		}
		if err := cfg.DeleteMirrorRepo("test-mirror", repoKey); err != nil {
			t.Fatal(err)
		}
		if err := cfg.DeleteMirror("test-mirror"); err != nil {
			t.Fatal(err)
		}
		persist(t)
	})

	t.Run("10_reclone", func(t *testing.T) {
		dest := clonePath(t, srcKey)
		if err := os.RemoveAll(dest); err != nil {
			t.Fatal(err)
		}
		cloneRepo(t)
		if !git.IsRepo(dest) {
			t.Fatal("re-clone not on disk")
		}
	})

	t.Run("11_rename_account", func(t *testing.T) {
		// Renaming moves the same-key source and its folder; the fixture's
		// source key may differ from the account key, so only the account
		// key is asserted here.
		newKey := acctKey + "-renamed"
		if err := RenameAccount(cfg, acctKey, newKey); err != nil {
			t.Fatalf("rename: %v", err)
		}
		persist(t)
		ReconfigureClones(cfg, newKey)
		if cfg.Sources[srcKey].Account != newKey && cfg.Sources[newKey].Account != newKey {
			t.Errorf("source not re-pointed at %s", newKey)
		}
		acctKey = newKey
	})

	t.Run("12_delete_everything", func(t *testing.T) {
		sk, err := ResolveSourceKey(cfg, acctKey)
		if err != nil {
			t.Fatal(err)
		}
		dest := clonePath(t, sk)
		if err := DeleteRepo(cfg, sk, repoKey); err != nil {
			t.Fatalf("delete repo: %v", err)
		}
		if _, err := os.Stat(dest); err == nil {
			t.Error("clone still on disk after DeleteRepo")
		}
		for _, k := range []string{acctKey, "dummy-gitlab"} {
			if err := DeleteAccount(cfg, k); err != nil {
				t.Fatalf("delete account %s: %v", k, err)
			}
		}
		persist(t)
		if len(cfg.Accounts) != 0 || len(cfg.Sources) != 0 || len(cfg.Mirrors) != 0 {
			t.Errorf("config not empty: %d accounts, %d sources, %d mirrors",
				len(cfg.Accounts), len(cfg.Sources), len(cfg.Mirrors))
		}
	})
}

// parseUserinfoPassword reports the password embedded in an http(s) URL.
func parseUserinfoPassword(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return "", false
	}
	return u.User.Password()
}
