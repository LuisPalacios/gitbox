package ops

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

func deletionFixture(t *testing.T) (*config.Config, string) {
	t.Helper()
	_, cfg := isolate(t)
	if err := AddAccount(cfg, "gh", testAccount("token")); err != nil {
		t.Fatal(err)
	}
	if err := credential.StoreToken("gh", "fixture-token-value"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.AddRepo("gh", "alice/repo", config.Repo{}); err != nil {
		t.Fatal(err)
	}
	path := makeClone(t, cfg, "gh", "alice/repo", "https://alice@github.com/alice/repo.git")
	return cfg, path
}

func TestStopTrackingRepoPreservesClone(t *testing.T) {
	cfg, path := deletionFixture(t)
	if err := StopTrackingRepo(cfg, "gh", "alice/repo"); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Sources["gh"].Repos["alice/repo"]; ok {
		t.Fatal("repo is still tracked")
	}
	if !git.IsRepo(path) || !cfg.IsIgnoredClone(path) {
		t.Fatal("clone must stay on disk and be ignored")
	}
	if err := StopTrackingRepo(cfg, "gh", "alice/repo"); !errors.Is(err, config.ErrNotFound) || len(cfg.Global.IgnoredClones) != 1 {
		t.Fatalf("duplicate stop tracking: %v, ignored=%v", err, cfg.Global.IgnoredClones)
	}
}

func TestDeleteAccountDefaultsToPreservingFolders(t *testing.T) {
	cfg, path := deletionFixture(t)
	second := filepath.Join(t.TempDir(), "override")
	if err := cfg.AddRepo("gh", "alice/second", config.Repo{CloneFolder: second}); err != nil {
		t.Fatal(err)
	}
	makeClone(t, cfg, "gh", "alice/second", "https://github.com/alice/second.git")
	removed, errs := DeleteAccount(cfg, "gh", false)
	if len(removed) != 0 || len(errs) != 0 || len(cfg.Accounts) != 0 || len(cfg.Sources) != 0 {
		t.Fatalf("account cascade: removed=%v, errs=%v, accounts=%v, sources=%v", removed, errs, cfg.Accounts, cfg.Sources)
	}
	for _, clone := range []string{path, second} {
		if !git.IsRepo(clone) || !cfg.IsIgnoredClone(clone) {
			t.Errorf("%s: clone must stay on disk and be ignored", clone)
		}
	}
}

func TestDeleteRepoClonePreservesUnsafeFolders(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		setup        func(t *testing.T, cfg *config.Config, path string)
	}{
		{"origin mismatch", "origin no longer matches", func(t *testing.T, _ *config.Config, path string) {
			gitRun(t, path, "remote", "set-url", "origin", "https://github.com/alice/unrelated.git")
		}},
		{"nested repo", "nested Git repository", func(t *testing.T, _ *config.Config, path string) {
			gitRun(t, path, "init", "--quiet", filepath.Join(path, "nested"))
		}},
		{"configured source folder", "overlaps a configured folder", func(t *testing.T, cfg *config.Config, path string) {
			folder, err := filepath.Rel(cfg.DefaultRoot(), path)
			if err != nil {
				t.Fatal(err)
			}
			changed := cfg.Sources["gh"]
			changed.Folder = folder
			cfg.Sources["gh"] = changed
			if err := cfg.UpdateRepo("gh", "alice/repo", config.Repo{CloneFolder: path}); err != nil {
				t.Fatal(err)
			}
		}},
		{"escaped source folder", "outside its expected location", func(t *testing.T, _ *config.Config, path string) {
			dir := filepath.Dir(path)
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.Rename(dir, outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, dir); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, path := deletionFixture(t)
			tc.setup(t, cfg, path)
			err := DeleteRepoClone(cfg, "gh", "alice/repo")
			pathErr, typed := errors.AsType[*os.PathError](err)
			if !typed || pathErr.Path != path || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("want a path error for %s: %v", tc.reason, err)
			}
			if _, ok := cfg.Sources["gh"].Repos["alice/repo"]; !ok || !git.IsRepo(path) {
				t.Fatal("clone and config entry must survive")
			}
		})
	}
}

func TestDeleteRepoCloneRetainsEntryWhenRootOffline(t *testing.T) {
	cfg, path := deletionFixture(t)
	root := cfg.DefaultRoot()
	if err := os.Rename(root, root+"-offline"); err != nil {
		t.Fatal(err)
	}
	err := DeleteRepoClone(cfg, "gh", "alice/repo")
	if pathErr, ok := errors.AsType[*os.PathError](err); !ok || pathErr.Path != path || !strings.Contains(err.Error(), "root is offline") {
		t.Fatalf("want a path error for the offline clone: %v", err)
	}
	if _, ok := cfg.Sources["gh"].Repos["alice/repo"]; !ok {
		t.Fatal("repo removed while its drive was offline")
	}
}

func TestDeleteRepoCloneReportsPermissionFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix directory permissions")
	}
	cfg, path := deletionFixture(t)
	parent := filepath.Dir(path)
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	// Root or hosts with permissive filesystem ACLs cannot exercise this case.
	if err := os.WriteFile(filepath.Join(parent, "permission-probe"), nil, 0o600); err == nil {
		t.Skip("read-only parent still permits writing")
	}
	err := DeleteRepoClone(cfg, "gh", "alice/repo")
	if pathErr, ok := errors.AsType[*os.PathError](err); !ok || !errors.Is(pathErr, os.ErrPermission) {
		t.Fatalf("want a typed permission failure: %v", err)
	}
	if _, ok := cfg.Sources["gh"].Repos["alice/repo"]; !ok {
		t.Fatal("repo removed from config despite failed filesystem deletion")
	}
}

func TestDeleteAccountCollectsFailuresAndCanRetry(t *testing.T) {
	cfg, valid := deletionFixture(t)
	folders := make(map[string]string)
	for _, key := range []string{"alice/unrelated-a", "alice/unrelated-b"} {
		if err := cfg.AddRepo("gh", key, config.Repo{}); err != nil {
			t.Fatal(err)
		}
		path := status.RepoPath(cfg, "gh", key, config.Repo{})
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		folders[key] = path
	}
	loose := filepath.Join(filepath.Dir(valid), "keep.txt")
	if err := os.WriteFile(loose, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, errs := DeleteAccount(cfg, "gh", true)
	if len(removed) != 1 || removed[0] != valid || len(errs) != len(folders) || len(cfg.Accounts) != 1 {
		t.Fatalf("partial delete: removed=%v, errors=%v, accounts=%v", removed, errs, cfg.Accounts)
	}
	if _, err := credential.GetToken("gh"); err != nil {
		t.Fatalf("credential lost on partial failure: %v", err)
	}
	for key, path := range folders {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
		if err := StopTrackingRepo(cfg, "gh", key); err != nil {
			t.Fatal(err)
		}
	}
	if removed, errs := DeleteAccount(cfg, "gh", true); len(removed) != 0 || len(errs) != 0 || len(cfg.Accounts) != 0 {
		t.Fatalf("retry: removed=%v, errors=%v, accounts=%v", removed, errs, cfg.Accounts)
	}
	if b, err := os.ReadFile(loose); err != nil || string(b) != "keep" {
		t.Fatalf("untracked sibling changed: %q, %v", b, err)
	}
}
