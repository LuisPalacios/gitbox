package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/git/testutil"
	"github.com/LuisPalacios/gitbox/pkg/reconcile"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

func isolateSchedule(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func TestSchedule_RenameDuringReconcile(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		isolateSchedule(t)
		a, path := newWorkspaceTestApp(t)
		defer a.stopRefresh()
		const old, renamed, repo = "github-alice", "github-renamed", "team/frontend"
		events := &recordingSink{}
		a.events = events
		scans := 0
		stalePath := filepath.Join(a.cfg.Global.Roots[0], "stale")
		scan := testutil.NewBarrier()
		defer scan.Release()
		a.reconcileScan = func(cfg *config.Config) (reconcile.Result, error) {
			scans++
			if _, stillOld := cfg.Sources[old]; !stillOld {
				return reconcile.Result{}, nil
			}
			if err := scan.Arrive(a.refreshContext()); err != nil {
				return reconcile.Result{}, err
			}
			return reconcile.Result{Relink: []reconcile.Relink{{Source: old, Repo: repo, Path: stalePath}}}, nil
		}

		done := make(chan ReconcileDTO, 1)
		go func() { done <- a.Reconcile() }()
		<-scan.Entered
		err := a.RenameAccount(old, renamed)
		scan.Release()
		result := <-done
		if err != nil {
			t.Fatal(err)
		}
		if scans != 2 || result.Error != "" || result.Relinked != 0 || len(events.named("reconcile:done")) != 0 {
			t.Fatalf("reconcile completed %d scans with result %+v, want one retry and no stale result", scans, result)
		}
		saved, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := saved.Sources[old]; exists {
			t.Fatal("stale scan restored the old source")
		}
		if actual, ok := saved.Sources[renamed].Repos[repo]; !ok || actual.CloneFolder != "" {
			t.Fatalf("rename lost repository intent: found=%v, repo=%+v", ok, actual)
		}
	})
}

func TestSchedule_DeleteDuringClone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		isolateSchedule(t)
		a, path := newWorkspaceTestApp(t)
		defer a.stopRefresh()
		const source, repo = "github-alice", "team/frontend"
		a.refreshHooks = &refreshHooks{}
		events := &recordingSink{}
		a.events = events
		dest := status.RepoPath(a.cfg, source, repo, a.cfg.Sources[source].Repos[repo])
		clone := testutil.NewBarrier()
		defer clone.Release()
		git.SetRunnerForTest(t, testutil.RunnerFunc(func(ctx context.Context, request git.Request) (git.Result, error) {
			if len(request.Args) == 0 || request.Args[0] != "clone" || request.Args[len(request.Args)-1] != dest {
				t.Errorf("unexpected Git operation: %v", request.Args)
				return git.Result{}, errors.New("unexpected Git operation")
			}
			if err := clone.Arrive(ctx); err != nil {
				return git.Result{}, err
			}
			// The in-flight Git transfer can finish writing after deletion.
			return git.Result{}, os.MkdirAll(filepath.Join(dest, ".git"), 0o700)
		}))

		a.CloneRepo(source, repo, "")
		<-clone.Entered
		err := a.DeleteRepo(source, repo)
		clone.Release()
		a.refreshWG.Wait()
		if err != nil {
			t.Fatal(err)
		}
		saved, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := saved.Sources[source].Repos[repo]; exists {
			t.Fatal("clone completion restored the deleted repository")
		}
		if _, exists := a.cfg.Sources[source].Repos[repo]; exists {
			t.Fatal("clone completion restored the deleted repository in memory")
		}
		if !git.IsRepo(dest) {
			t.Fatal("clone completion removed the Git transfer its caller no longer owns")
		}
		if done := events.named("clone:done"); len(done) != 1 {
			t.Fatalf("clone completion events = %d, want 1", len(done))
		}
	})
}

func TestSchedule_FetchDoesNotChangeUpstreamAfterCredentialSwitchOrCancel(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		switchCredential, cancel bool
	}{
		{name: "successful transfer after credential switch", switchCredential: true},
		{name: "cancelled transfer", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				isolateSchedule(t)
				a, events := refreshTestApp(t, 1)
				defer a.stopRefresh()
				const source, repo, account = "source-a", "org/repo-0", "account-a"
				acct := a.cfg.Accounts[account]
				acct.Provider, acct.Username, acct.Name, acct.Email = "github", "fixture", "Fixture", "fixture@example.invalid"
				a.cfg.Accounts[account] = acct
				a.cfgPath = filepath.Join(a.cfg.Global.Roots[0], "gitbox.json")
				a.cfgLoaded = true
				if err := config.Save(a.cfg, a.cfgPath); err != nil {
					t.Fatal(err)
				}
				a.recordFetchStatus(a.snapshotRefreshConfig(), source, repo, true, time.Now())
				before := a.snapshotProbeRevisions()
				fetch := testutil.NewBarrier()
				defer fetch.Release()
				a.refreshHooks.fetch = nil
				git.SetRunnerForTest(t, testutil.RunnerFunc(func(ctx context.Context, request git.Request) (git.Result, error) {
					if slices.Equal(request.Args, []string{"fetch", "--prune", "--", "origin"}) {
						return git.Result{}, fetch.Arrive(ctx)
					}
					// The credential switches use Git for local clone configuration.
					return git.Result{}, nil
				}))
				snapshot := a.snapshotRefreshConfig()
				path := status.RepoPath(snapshot, source, repo, snapshot.Sources[source].Repos[repo])
				done := make(chan error, 1)
				go func() { done <- a.performFetch(snapshot, source, repo, path) }()
				<-fetch.Entered
				var err error
				if tc.switchCredential {
					err = a.ChangeCredentialType(account, "gcm")
				}
				if tc.cancel {
					a.refreshCancel()
				}
				fetch.Release()
				fetchErr := <-done
				if err != nil {
					t.Fatal(err)
				}
				if tc.cancel != errors.Is(fetchErr, context.Canceled) || !tc.cancel && fetchErr != nil {
					t.Fatalf("unexpected fetch result: %v", fetchErr)
				}
				if after := a.snapshotProbeRevisions(); after.upstream[source+"/"+repo] != before.upstream[source+"/"+repo] || !a.upstreamGone[source+"/"+repo] {
					t.Fatal("obsolete or cancelled fetch changed the confirmed upstream state")
				}
				if tc.switchCredential {
					saved, err := config.Load(a.cfgPath)
					if err != nil {
						t.Fatal(err)
					}
					if got := saved.Accounts[account].DefaultCredentialType; got != "gcm" {
						t.Fatalf("saved credential type = %q, want gcm", got)
					}
				}
				if done := events.named("fetch:done"); len(done) != 1 {
					t.Fatalf("fetch completion events = %d, want 1", len(done))
				}
			})
		})
	}
}
