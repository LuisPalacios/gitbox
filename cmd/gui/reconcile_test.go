package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
