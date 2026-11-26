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
	"github.com/LuisPalacios/gitbox/pkg/git"
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

// Use a configured credential type so adoption exercises the real repair.
func newAdoptionTestApp(t *testing.T) (*App, string) {
	t.Helper()
	a, path := newWorkspaceTestApp(t)
	acct := a.cfg.Accounts["github-alice"]
	acct.DefaultCredentialType = "ssh"
	a.cfg.Accounts["github-alice"] = acct
	if err := config.Save(a.cfg, path); err != nil {
		t.Fatal(err)
	}
	return a, path
}

func TestReconcile_AdoptsConfidentCloneAndSaves(t *testing.T) {
	a, cfgPath := newAdoptionTestApp(t)
	clone := filepath.Join(a.cfg.Global.Roots[0], "github-alice", "alice", "fresh")
	initClone(t, clone, "git@github.com:alice/fresh.git")

	res := a.Reconcile()
	if res.Error != "" || res.Adopted != 1 || len(res.Unmatched) != 0 || len(res.Repairs) != 1 || len(res.Repairs[0].Fixed) == 0 || len(res.Repairs[0].Warnings) != 0 {
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

	for _, mode := range []string{"automatic", "manual"} {
		t.Run(mode+" repair failure preserves adoption", func(t *testing.T) {
			a, path := newAdoptionTestApp(t)
			t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			acct := a.cfg.Accounts["github-alice"]
			owner := "alice"
			if mode == "manual" {
				owner = "other"
			}
			repo := owner + "/locked"
			clone := filepath.Join(a.cfg.Global.Roots[0], "scratch", "locked")
			initClone(t, clone, "git@github.com:"+repo+".git")
			// Only user.email needs repair. Git's config lock refuses this write
			// on every OS, even when the test runs as an administrator.
			for key, value := range map[string]string{"user.name": acct.Name, "user.email": "old@example.invalid", "credential.helper": ""} {
				if err := git.ConfigSet(clone, key, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(clone, ".git", "config.lock"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			var result ReconcileDTO
			if mode == "manual" {
				result = a.AdoptClones([]AdoptRequestDTO{{Path: clone, Account: "github-alice"}})
			} else {
				result = a.Reconcile()
			}
			if result.Error != "" || result.Adopted != 1 || len(result.Repairs) != 1 {
				t.Fatalf("adoption = %+v", result)
			}
			repair := result.Repairs[0]
			if repair.Source != "github-alice" || repair.Repo != repo || len(repair.Warnings) != 1 || len(repair.Fixed) != 0 {
				t.Fatalf("repair = %+v", repair)
			}
			saved, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := saved.Sources[repair.Source].Repos[repair.Repo]; !ok {
				t.Fatal("the repair failure discarded the saved adoption")
			}
			if got := gitConfigGet(t, clone, "user.email"); got != "old@example.invalid" {
				t.Fatalf("email = %q after a refused repair", got)
			}
		})
	}
	t.Run("a reassigned source uses no stale repair inputs", func(t *testing.T) {
		a, _ := newAdoptionTestApp(t)
		const source = "github-alice"
		const repo = "team/frontend"
		clone := filepath.Join(a.cfg.Global.Roots[0], source, "team", "frontend")
		origin := "https://github.com/team/frontend.git"
		initClone(t, clone, origin)
		if err := git.ConfigSet(clone, "user.name", "Before"); err != nil {
			t.Fatal(err)
		}
		acct := a.cfg.Accounts[source]
		snap := a.snapshotRefreshConfig()
		replacement := acct
		replacement.Name = "Replacement"
		a.cfg.Accounts["other"] = replacement
		src := a.cfg.Sources[source]
		src.Account = "other"
		a.cfg.Sources[source] = src
		rows := a.repairAdoptions(snap, []reconcile.Placed{{Source: source, Repo: repo, Account: source, Path: clone}})
		if len(rows) != 1 || len(rows[0].Warnings) != 1 || len(rows[0].Fixed) != 0 {
			t.Fatalf("stale repair: %+v", rows)
		}
		if got := gitConfigGet(t, clone, "user.name"); got != "Before" {
			t.Fatalf("stale repair set user.name=%q", got)
		}
	})
	t.Run("a refused save leaves the clone configuration alone", func(t *testing.T) {
		a, path := newAdoptionTestApp(t)
		if _, err := a.reloadConfigLocal(); err != nil {
			t.Fatal(err)
		}
		clone := filepath.Join(a.cfg.Global.Roots[0], "github-alice", "alice", "conflict")
		origin := "git@github.com:alice/conflict.git"
		initClone(t, clone, origin)
		a.reconcileScan = func(snap *config.Config) (reconcile.Result, error) {
			result, err := reconcile.Plan(snap)
			if err != nil {
				return result, err
			}
			disk, err := config.Load(path)
			if err != nil {
				return result, err
			}
			if err := disk.AddRepo("github-alice", "team/external", config.Repo{}); err != nil {
				return result, err
			}
			return result, config.Save(disk, path)
		}
		result := a.Reconcile()
		if result.Error == "" || result.Adopted+result.Relinked != 0 || gitConfigGet(t, clone, "remote.origin.url") != origin {
			t.Fatalf("refused save applied changes: %+v", result)
		}
		disk, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, current := range []*config.Config{a.cfg, disk} {
			repos := current.Sources["github-alice"].Repos
			if _, ok := repos["team/external"]; !ok {
				t.Fatal("external change lost")
			}
			if _, ok := repos["alice/conflict"]; ok {
				t.Fatal("rejected adoption survived")
			}
		}
	})
}

func TestReconcile_AdoptsDuringUnrelatedEdits(t *testing.T) {
	a, cfgPath := newAdoptionTestApp(t)
	clone := filepath.Join(a.cfg.Global.Roots[0], "github-alice", "alice", "fresh")
	initClone(t, clone, "https://github.com/alice/fresh.git")
	synctest.Test(t, func(t *testing.T) {
		defer a.stopRefresh()
		calls := 0
		a.reconcileScan = func(snap *config.Config) (reconcile.Result, error) {
			calls++
			res, err := reconcile.Plan(snap)
			a.mu.Lock()
			defer a.mu.Unlock()
			if err := a.cfg.AddRepo("github-alice", fmt.Sprintf("team/edited%d", calls), config.Repo{}); err != nil {
				t.Fatal(err)
			}
			if err := a.saveConfig(); err != nil {
				t.Fatal(err)
			}
			return res, err
		}
		result := a.Reconcile()
		synctest.Wait()
		if calls != 1 || result.Adopted != 1 || len(result.Skipped) != 0 || result.Error != "" {
			t.Fatalf("scans=%d result=%+v", calls, result)
		}
		reloaded, err := config.Load(cfgPath)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"alice/fresh", "team/edited1"} {
			if _, ok := reloaded.Sources["github-alice"].Repos[key]; !ok {
				t.Errorf("missing %s on disk", key)
			}
		}
	})
	t.Run("a concurrent adoption skips one entry and queues one rescan", func(t *testing.T) {
		a, cfgPath := newAdoptionTestApp(t)
		root := a.cfg.Global.Roots[0]
		first := filepath.Join(root, "github-alice", "alice", "first")
		second := filepath.Join(root, "github-alice", "alice", "second")
		initClone(t, first, "https://github.com/alice/first.git")
		initClone(t, second, "https://github.com/alice/second.git")
		synctest.Test(t, func(t *testing.T) {
			defer a.stopRefresh()
			done := make(chan struct{})
			a.statusRun = &statusFlight{done: done}
			scans := 0
			a.reconcileScan = func(snap *config.Config) (reconcile.Result, error) {
				scans++
				result, err := reconcile.Plan(snap)
				if scans == 1 {
					a.mu.Lock()
					addErr := a.cfg.AddRepo("github-alice", "alice/first", config.Repo{Name: "Keep"})
					a.mu.Unlock()
					if addErr != nil {
						return reconcile.Result{}, addErr
					}
				}
				return result, err
			}
			result := a.Reconcile()
			close(done)
			synctest.Wait()
			if result.Error != "" || result.Adopted != 1 || scans != 2 || len(result.Skipped) != 1 ||
				result.Skipped[0] != (reconcile.Skip{Path: first, Reason: "pathCollision"}) {
				t.Fatalf("scans=%d result=%+v", scans, result)
			}
			reloaded, err := config.Load(cfgPath)
			if err != nil {
				t.Fatal(err)
			}
			repos := reloaded.Sources["github-alice"].Repos
			if _, ok := repos["alice/second"]; !ok || repos["alice/first"].Name != "Keep" {
				t.Fatalf("current intent missing from disk: %+v", repos)
			}
			if got := gitConfigGet(t, second, "user.email"); got != "a@e" {
				t.Fatalf("applied clone identity = %q", got)
			}
		})
	})
}

func TestAdoptClonesAndIgnoreClone(t *testing.T) {
	a, cfgPath := newAdoptionTestApp(t)
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
	t.Run("failed save preserves ignored clone in memory and on disk", func(t *testing.T) {
		a, path := newAdoptionTestApp(t)
		clone := filepath.Join(a.cfg.Global.Roots[0], "scratch", "new")
		initClone(t, clone, "https://github.com/other/new.git")
		if err := a.IgnoreClone(clone, true); err != nil {
			t.Fatal(err)
		}
		a.readOnly = true
		result := a.AdoptClones([]AdoptRequestDTO{{Path: clone, Account: "github-alice"}})
		if result.Error == "" || result.Adopted != 0 || len(result.Repairs) != 0 {
			t.Fatalf("refused save: %+v", result)
		}
		saved, err := config.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, cfg := range []*config.Config{a.cfg, saved} {
			if _, ok := cfg.Sources["github-alice"].Repos["other/new"]; ok || !cfg.IsIgnoredClone(clone) {
				t.Fatal("rejected adoption changed tracked or ignored clones")
			}
		}
		if got := gitConfigGet(t, clone, "remote.origin.url"); got != "https://github.com/other/new.git" {
			t.Fatalf("unsaved clone origin = %q", got)
		}
	})

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
	res, _, err := a.scanConfig()
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
				_, _, err := a.scanConfig()
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
		a.cfg.Global.Collapsed = &config.CollapsedState{Sidebar: true}
		a.cfg.Global.Theme = "dark"
		a.mu.Unlock()
		return reconcile.Result{}, nil
	}
	_, _, err := a.scanConfig()
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
		freshPath := filepath.Join(a.cfg.Global.Roots[0], "fresh")
		initClone(t, freshPath, "https://github.com/team/frontend.git")
		var scans atomic.Int32
		a.reconcileScan = func(snap *config.Config) (reconcile.Result, error) {
			if scans.Add(1) <= 2 {
				a.mu.Lock()
				a.cfg.Global.NestedScanDepth++
				a.mu.Unlock()
			}
			return reconcile.Plan(snap)
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
