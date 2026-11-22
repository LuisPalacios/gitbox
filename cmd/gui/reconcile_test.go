package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/reconcile"
)

// initClone creates a git repo at path with the given origin, isolated from
// the user's git config.
func initClone(t *testing.T, path, origin string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", origin}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = path
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir())
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func gitConfigGet(t *testing.T, path, key string) string {
	t.Helper()
	out, _ := exec.Command("git", "-C", path, "config", "--get", key).Output()
	return strings.TrimSpace(string(out))
}

func TestReconcile_AdoptsConfidentCloneAndSaves(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)
	clone := filepath.Join(a.cfg.Global.Roots[0], "github-alice", "alice", "fresh")
	initClone(t, clone, "git@github.com:alice/fresh.git")

	res := a.Reconcile()
	if res.Error != "" || res.Adopted != 1 || len(res.Unmatched) != 0 {
		t.Fatalf("reconcile = %+v", res)
	}
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Sources["github-alice"].Repos["alice/fresh"]; !ok {
		t.Error("adopted repo not saved")
	}
	if got := gitConfigGet(t, clone, "user.email"); got != "a@e" {
		t.Errorf("identity = %q, want the account's", got)
	}
}

func TestReconcile_RetriesWhenTheConfigChangesDuringTheScan(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)
	calls := 0
	a.reconcileScan = func(snap *config.Config) (reconcile.Result, error) {
		calls++
		if calls == 1 {
			// An edit lands while the first scan runs.
			a.mu.Lock()
			_ = a.cfg.AddRepo("github-alice", "team/edited", config.Repo{})
			_ = a.saveConfig()
			a.mu.Unlock()
		}
		return reconcile.Result{}, nil
	}
	a.Reconcile()
	if calls != 2 {
		t.Errorf("scans = %d, want a retry after the edit", calls)
	}
	reloaded, _ := config.Load(cfgPath)
	if _, ok := reloaded.Sources["github-alice"].Repos["team/edited"]; !ok {
		t.Error("the concurrent edit was lost")
	}
}

func TestAdoptClonesAndIgnoreClone(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)
	root := a.cfg.Global.Roots[0]
	linux := filepath.Join(root, "scratch", "linux")
	initClone(t, linux, "https://github.com/torvalds/linux.git")
	other := filepath.Join(root, "scratch", "other")
	initClone(t, other, "https://github.com/someone/other.git")

	res := a.Reconcile()
	if len(res.Unmatched) != 2 || res.Unmatched[0].Reason != reconcile.ReasonHostOnly {
		t.Fatalf("unmatched = %+v, want two host-only clones", res.Unmatched)
	}

	res = a.AdoptClones([]AdoptRequestDTO{{Path: linux, Account: "github-alice"}})
	if res.Error != "" || len(res.Unmatched) != 1 {
		t.Fatalf("after adopt = %+v", res)
	}
	reloaded, _ := config.Load(cfgPath)
	if r, ok := reloaded.Sources["github-alice"].Repos["torvalds/linux"]; !ok || r.CloneFolder != linux {
		t.Errorf("adopted repo = %+v, %v", r, ok)
	}

	if err := a.IgnoreClone(other, true); err != nil {
		t.Fatal(err)
	}
	res = a.Reconcile()
	if len(res.Unmatched) != 0 || len(res.Ignored) != 1 {
		t.Errorf("after ignore: unmatched=%d ignored=%d", len(res.Unmatched), len(res.Ignored))
	}
	reloaded, _ = config.Load(cfgPath)
	if !reloaded.IsIgnoredClone(other) {
		t.Error("ignore not saved")
	}

	// An ignored clone can still be adopted; it stops being ignored.
	res = a.AdoptClones([]AdoptRequestDTO{{Path: other, Account: "github-alice"}})
	if res.Error != "" || len(res.Ignored) != 0 {
		t.Fatalf("adopt from ignored = %+v", res)
	}
	reloaded, _ = config.Load(cfgPath)
	if _, ok := reloaded.Sources["github-alice"].Repos["someone/other"]; !ok || reloaded.IsIgnoredClone(other) {
		t.Errorf("adopted=%v still ignored=%v", ok, reloaded.IsIgnoredClone(other))
	}
}

func TestScanConfig_RejectsStalePlanAfterTwoAttempts(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)
	calls := 0
	a.reconcileScan = func(*config.Config) (reconcile.Result, error) {
		calls++
		a.mu.Lock()
		a.cfg.Global.NestedScanDepth++
		a.mu.Unlock()
		return reconcile.Result{Relink: []reconcile.Relink{{Source: "github-alice", Repo: "team/frontend", Path: "stale"}}}, nil
	}
	res, err := a.scanConfig()
	defer a.mu.Unlock()
	if !errors.Is(err, ErrStalePlan) || calls != 2 || len(res.Relink) != 0 {
		t.Fatalf("scans=%d result=%+v error=%v", calls, res, err)
	}
	if a.mu.TryLock() {
		a.mu.Unlock()
		t.Fatal("scanConfig returned without the config lock")
	}
}

func TestScanConfig_CloneFailureReturnsWithLock(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			a, _ := newWorkspaceTestApp(t)
			want := errors.New("clone failed")
			calls := 0
			a.reconcileClone = func(cfg *config.Config) (*config.Config, error) {
				calls++
				if calls == failAt {
					return nil, want
				}
				return cfg.Clone()
			}
			a.reconcileScan = func(*config.Config) (reconcile.Result, error) { return reconcile.Result{}, nil }
			done := make(chan error, 1)
			go func() {
				_, err := a.scanConfig()
				if a.mu.TryLock() {
					err = errors.New("scanConfig returned without the config lock")
				}
				a.mu.Unlock()
				done <- err
			}()
			select {
			case err := <-done:
				if !errors.Is(err, want) || calls != failAt {
					t.Fatalf("clones=%d error=%v", calls, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("clone failure did not terminate")
			}
		})
	}
}

func TestScanConfig_IgnoresOnlyUIState(t *testing.T) {
	a, _ := newWorkspaceTestApp(t)
	calls := 0
	a.reconcileScan = func(*config.Config) (reconcile.Result, error) {
		calls++
		a.mu.Lock()
		a.cfg.Global.Window = &config.WindowState{Width: calls}
		a.cfg.Global.CompactWindow = &config.WindowState{Height: calls}
		a.cfg.Global.Collapsed = &config.CollapsedState{Sidebar: true}
		a.cfg.Global.Theme = "dark"
		a.mu.Unlock()
		return reconcile.Result{}, nil
	}
	_, err := a.scanConfig()
	a.mu.Unlock()
	if err != nil || calls != 1 {
		t.Fatalf("scans=%d error=%v", calls, err)
	}
}

func TestReconcile_StalePlanCoalescesOneRetryAfterRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, cfgPath := newWorkspaceTestApp(t)
		defer a.stopRefresh()
		before, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		events := &refreshTestEvents{}
		a.events = events
		fetchDone, statusDone := make(chan struct{}), make(chan struct{})
		a.fetchRun = &fetchFlight{done: fetchDone}
		a.statusRun = &statusFlight{done: statusDone}
		var scans atomic.Int32
		a.reconcileScan = func(*config.Config) (reconcile.Result, error) {
			scans.Add(1)
			a.mu.Lock()
			a.cfg.Global.NestedScanDepth++
			a.mu.Unlock()
			return reconcile.Result{Relink: []reconcile.Relink{{Source: "github-alice", Repo: "team/frontend", Path: "stale"}}}, nil
		}
		var callers sync.WaitGroup
		for range 3 {
			callers.Go(func() {
				if res := a.Reconcile(); res.Error != ErrStalePlan.Error() || res.Relinked != 0 {
					t.Errorf("stale result = %+v", res)
				}
			})
		}
		callers.Wait()
		synctest.Wait()
		if scans.Load() != 6 {
			t.Fatalf("retry ran during fetch: %d scans", scans.Load())
		}
		close(fetchDone)
		synctest.Wait()
		if scans.Load() != 6 {
			t.Fatal("retry ran during status")
		}
		close(statusDone)
		synctest.Wait()
		if scans.Load() != 8 {
			t.Fatalf("scans=%d, want six requested and two in one follow-up", scans.Load())
		}
		a.mu.Lock()
		pending := a.reconcilePending
		repo := a.cfg.Sources["github-alice"].Repos["team/frontend"]
		a.mu.Unlock()
		if pending || repo.CloneFolder != "" {
			t.Fatalf("pending=%v repo=%+v", pending, repo)
		}
		after, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatal("stale plan changed the config on disk")
		}
		if len(events.named("reconcile:done")) != 0 {
			t.Fatal("stale pass emitted completion")
		}
	})
}

func TestReconcile_RetryCancelsOnShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _ := newWorkspaceTestApp(t)
		a.statusRun = &statusFlight{done: make(chan struct{})}
		var scans atomic.Int32
		a.reconcileScan = func(*config.Config) (reconcile.Result, error) {
			scans.Add(1)
			return reconcile.Result{}, nil
		}
		a.retryReconcileAfterRefresh()
		synctest.Wait()
		a.stopRefresh()
		a.retryReconcileAfterRefresh()
		synctest.Wait()
		if scans.Load() != 0 || a.reconcilePending {
			t.Fatal("retry survived shutdown")
		}
	})
}

func TestReconcile_RetryAppliesFreshPlanAndEmitsOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, cfgPath := newWorkspaceTestApp(t)
		defer a.stopRefresh()
		events := &refreshTestEvents{}
		a.events = events
		done := make(chan struct{})
		a.statusRun = &statusFlight{done: done}
		freshPath := filepath.Join(t.TempDir(), "fresh")
		var scans atomic.Int32
		a.reconcileScan = func(*config.Config) (reconcile.Result, error) {
			if scans.Add(1) <= 2 {
				a.mu.Lock()
				a.cfg.Global.NestedScanDepth++
				a.mu.Unlock()
			}
			return reconcile.Result{Relink: []reconcile.Relink{{Source: "github-alice", Repo: "team/frontend", Path: freshPath}}}, nil
		}
		if res := a.Reconcile(); res.Error != ErrStalePlan.Error() {
			t.Fatalf("result = %+v", res)
		}
		close(done)
		synctest.Wait()
		if scans.Load() != 3 {
			t.Fatalf("scans=%d, want two stale and one fresh", scans.Load())
		}
		reloaded, err := config.Load(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		if reloaded.Sources["github-alice"].Repos["team/frontend"].CloneFolder != freshPath {
			t.Fatal("fresh retry did not persist the relink")
		}
		if len(events.named("reconcile:done")) != 1 {
			t.Fatal("fresh retry did not emit exactly one completion")
		}
	})
}

func TestReconcile_WorkspaceReadFailurePreservesSavedWorkspace(t *testing.T) {
	a, cfgPath := newWorkspaceTestApp(t)
	a.reconcileScan = func(*config.Config) (reconcile.Result, error) { return reconcile.Result{}, nil }
	dir := containerClone(t, a, map[string]string{"saved.code-workspace": "{}"})
	if err := a.SetRepoWorkspace("github-alice", "team/frontend", "saved.code-workspace"); err != nil {
		t.Fatal(err)
	}

	moved := dir + ".offline"
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	events := &refreshTestEvents{}
	a.events = events
	res := a.reconcilePass(false)
	if res.WorkspacesChanged || !reflect.DeepEqual(res.WorkspaceUnknown, []string{"github-alice/team/frontend"}) {
		t.Fatalf("workspace scan = %+v, want stale workspace and no changes", res)
	}
	if len(events.named("reconcile:done")) != 1 {
		t.Fatal("background pass did not report the first stale workspace")
	}
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	repo := reloaded.Sources["github-alice"].Repos["team/frontend"]
	if !repo.Container || repo.Workspace != "saved.code-workspace" || repo.WorkspacePreferred != "saved.code-workspace" {
		t.Fatalf("saved workspace changed: %+v", repo)
	}

	if err := os.Rename(moved, dir); err != nil {
		t.Fatal(err)
	}
	res = a.reconcilePass(false)
	if res.WorkspacesChanged || res.WorkspaceUnknown == nil || len(res.WorkspaceUnknown) != 0 || len(events.named("reconcile:done")) != 2 {
		t.Fatalf("workspace scan = %+v, want a recovery event without a config change", res)
	}
}
