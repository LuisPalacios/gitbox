package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

func trackedAccountClone(t *testing.T) (string, string) {
	t.Helper()
	path := contractConfig(t)
	cfg := loadConfig(t, path)
	clone := cfg.Sources["acct"].Repos["org/repo"].CloneFolder
	if _, err := git.Run(t.Context(), git.Request{Dir: clone, Args: []string{"remote", "add", "origin", "https://github.com/org/repo.git"}}); err != nil {
		t.Fatal(err)
	}
	return path, clone
}

func TestAccountDeletionLocalAndRemote(t *testing.T) {
	for _, mode := range []string{"local", "remote"} {
		t.Run(mode, func(t *testing.T) {
			backend := func(t *testing.T, path string) Backend {
				t.Helper()
				if mode == "remote" {
					return serveRemote(t, path, nil).Remote
				}
				return fileBackend(t, path)
			}
			t.Run("keep folder", func(t *testing.T) {
				path, clone := trackedAccountClone(t)
				if failures, err := backend(t, path).DeleteAccount("acct", false); err != nil || len(failures) != 0 {
					t.Fatalf("delete account: %v, %v", failures, err)
				}
				cfg := loadConfig(t, path)
				if len(cfg.Accounts) != 0 || !cfg.IsIgnoredClone(clone) || !git.IsRepo(clone) {
					t.Fatal("keep-folders deletion must forget the account and preserve its ignored clone")
				}
			})
			t.Run("remove folder", func(t *testing.T) {
				path, clone := trackedAccountClone(t)
				if failures, err := backend(t, path).DeleteAccount("acct", true); err != nil || len(failures) != 0 {
					t.Fatalf("delete account: %v, %v", failures, err)
				}
				if _, err := os.Stat(clone); !os.IsNotExist(err) || len(loadConfig(t, path).Accounts) != 0 {
					t.Fatalf("opt-in must remove both the folder and the account: %v", err)
				}
			})
			t.Run("report failures", func(t *testing.T) {
				path, clone := trackedAccountClone(t)
				canary := "ghp_NOTAREALTOKEN0000000000000000000000"
				other := filepath.Join(filepath.Dir(clone), canary)
				if err := os.MkdirAll(other, 0o700); err != nil {
					t.Fatal(err)
				}
				cfg := loadConfig(t, path)
				if err := cfg.AddRepo("acct", "org/other", config.Repo{CloneFolder: other}); err != nil {
					t.Fatal(err)
				}
				if err := config.Save(cfg, path); err != nil {
					t.Fatal(err)
				}
				if _, err := git.Run(t.Context(), git.Request{Dir: clone, Args: []string{"init", "-q", filepath.Join(clone, "nested")}}); err != nil {
					t.Fatal(err)
				}
				b := backend(t, path)
				failures, err := b.DeleteAccount("acct", true)
				if err != nil || len(failures) != 2 {
					t.Fatalf("expected both folder failures: %v, %v", failures, err)
				}
				joined := strings.Join(failures, "\n")
				if strings.Contains(joined, canary) || !strings.Contains(joined, "[redacted]") ||
					!strings.Contains(joined, "nested Git repository") || !strings.Contains(joined, "not a standalone Git clone directory") {
					t.Fatalf("expected both scrubbed reasons: %q", joined)
				}
				if len(loadConfig(t, path).Accounts) != 1 || !git.IsRepo(clone) {
					t.Fatal("failed account deletion must preserve the account and its protected clone")
				}
				if err := b.DeleteRepoFolder("acct", "org/repo"); err == nil {
					t.Fatal("protected clone deletion must reject")
				}
			})
			if mode == "remote" {
				t.Run("legacy calls cannot delete", func(t *testing.T) {
					path, clone := trackedAccountClone(t)
					link := serveRemote(t, path, nil)
					client := link.Remote.current()
					if err := client.Call(t.Context(), "DeleteRepo", []any{"acct", "org/repo"}, nil); !rpc.IsCode(err, rpc.ErrCodeUnsupported) {
						t.Fatalf("obsolete destructive method should be unserved: %v", err)
					}
					if err := client.Call(t.Context(), "DeleteAccount", []any{"acct"}, nil, rpc.WithRequestID(newRequestID())); !rpc.IsCode(err, rpc.ErrCodeInvalidParams) {
						t.Fatalf("account deletion requires an explicit argument: %v", err)
					}
					if !git.IsRepo(clone) || len(loadConfig(t, path).Accounts) != 1 {
						t.Fatal("legacy request changed the fixture")
					}
				})
			}
		})
	}
}

func TestDeleteFolderBindingsRefuseSimpleMode(t *testing.T) {
	path, clone := trackedAccountClone(t)
	cfg := loadConfig(t, path)
	cfg.Global.UIMode = "simple"
	if err := config.Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	app := newServerApp(path)
	app.be().Init(context.Background())
	t.Cleanup(app.be().Close)
	if err := app.DeleteRepoFolder("acct", "org/repo"); !errors.Is(err, errDeleteFoldersAdvanced) {
		t.Fatalf("want Advanced mode requirement, got %v", err)
	}
	if _, err := app.DeleteAccount("acct", true); !errors.Is(err, errDeleteFoldersAdvanced) {
		t.Fatalf("want Advanced mode requirement, got %v", err)
	}
	if !git.IsRepo(clone) || len(loadConfig(t, path).Accounts) != 1 {
		t.Fatal("Simple mode destructive attempts changed the fixture")
	}
}

func TestDeleteFolderChecksConfigRevisionBeforeTouchingDisk(t *testing.T) {
	path, clone := trackedAccountClone(t)
	app := newServerApp(path)
	app.be().Init(context.Background())
	t.Cleanup(app.be().Close)
	if _, err := config.UpdateFile(path, func(c *config.Config) error {
		return c.AddRepo("acct", "org/new", config.Repo{})
	}); err != nil {
		t.Fatal(err)
	}

	if err := app.DeleteRepoFolder("acct", "org/repo"); !isConfigChanged(err) {
		t.Fatalf("want stale config error, got %v", err)
	}
	if !git.IsRepo(clone) {
		t.Fatal("stale config allowed the clone to be deleted")
	}
}
