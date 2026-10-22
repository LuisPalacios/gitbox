package reconcile

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// initRepo creates a minimal git repo at path with the given origin remote.
func initRepo(t *testing.T, path, remoteURL string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = path
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir())
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if remoteURL != "" {
		run("remote", "add", "origin", remoteURL)
	}
}

// testConfig has one GitHub account with a source and a tracked repo that
// isn't cloned, and a Gitea account without any source.
func testConfig(roots ...string) *config.Config {
	return &config.Config{
		Version: config.CurrentVersion,
		Global:  config.GlobalConfig{Roots: roots},
		Accounts: map[string]config.Account{
			"github-me": {Provider: "github", URL: "https://github.com", Username: "me", Name: "Me", Email: "me@e"},
			"gitea-me":  {Provider: "gitea", URL: "https://git.example.org", Username: "me", Name: "Me", Email: "me@e"},
		},
		Sources: map[string]config.Source{
			"github-me": {Account: "github-me", Repos: map[string]config.Repo{"me/tracked": {}}},
		},
	}
}

func planAndApply(t *testing.T, cfg *config.Config) Result {
	t.Helper()
	res, err := Plan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	Apply(cfg, res)
	return res
}

func reasons(res Result) map[string]string {
	out := map[string]string{}
	for _, u := range res.Unmatched {
		out[filepath.Base(u.Path)] = u.Reason
	}
	return out
}

func TestPlan_AdoptsConfidentClonesInPlace(t *testing.T) {
	r1, r2 := t.TempDir(), t.TempDir()
	cfg := testConfig(r1, r2)
	initRepo(t, filepath.Join(r1, "github-me", "me", "home"), "https://github.com/me/home.git")
	initRepo(t, filepath.Join(r2, "github-me", "me", "second"), "https://github.com/me/second.git")
	elsewhere := filepath.Join(r1, "scratch", "loose")
	initRepo(t, elsewhere, "https://github.com/me/loose.git")
	initRepo(t, filepath.Join(r1, "git.example.org", "me", "forge"), "https://git.example.org/me/forge.git")

	res := planAndApply(t, cfg)
	if len(res.Adopt) != 4 || len(res.Unmatched) != 0 {
		t.Fatalf("adopt=%d unmatched=%v", len(res.Adopt), reasons(res))
	}
	repos := cfg.Sources["github-me"].Repos
	if r := repos["me/home"]; r.Root != "" || r.CloneFolder != "" {
		t.Errorf("default-root clone = %+v, want no location fields", r)
	}
	if r := repos["me/second"]; r.Root != r2 || r.CloneFolder != "" {
		t.Errorf("second-root clone = %+v, want root %s", r, r2)
	}
	if r := repos["me/loose"]; r.CloneFolder != elsewhere {
		t.Errorf("loose clone = %+v, want clone_folder %s", r, elsewhere)
	}
	if src, ok := cfg.Sources["gitea-me"]; !ok || src.Account != "gitea-me" || len(src.Repos) != 1 {
		t.Errorf("gitea source = %+v, want one created for the account", src)
	}
	// Every adopted clone resolves back to where it was found.
	for sk, src := range cfg.Sources {
		for rk, repo := range src.Repos {
			if rk == "me/tracked" {
				continue
			}
			if p := status.RepoPath(cfg, sk, rk, repo); !git.IsRepo(p) {
				t.Errorf("%s/%s resolves to %s, which isn't the clone", sk, rk, p)
			}
		}
	}
}

func TestPlan_LeavesUndecidedClonesForMe(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	cfg.Accounts["github-work"] = config.Account{Provider: "github", URL: "https://github.com", Username: "work", Name: "W", Email: "w@e"}
	initRepo(t, filepath.Join(root, "scratch", "linux"), "https://github.com/torvalds/linux.git")
	initRepo(t, filepath.Join(root, "scratch", "lab"), "https://gitlab.com/someone/lab.git")
	initRepo(t, filepath.Join(root, "scratch", "local"), "")

	res := planAndApply(t, cfg)
	want := map[string]string{"linux": ReasonAmbiguous, "lab": ReasonUnknown, "local": ReasonNoRemote}
	if got := reasons(res); len(res.Adopt) != 0 || !equal(got, want) {
		t.Errorf("unmatched = %v, want %v (adopted %d)", got, want, len(res.Adopt))
	}

	// With a single GitHub account the third-party repo matches on the host
	// alone: still mine to decide.
	delete(cfg.Accounts, "github-work")
	if got := reasons(planAndApply(t, cfg)); got["linux"] != ReasonHostOnly {
		t.Errorf("single account: linux = %q, want %q", got["linux"], ReasonHostOnly)
	}
}

func TestPlan_RelinksAMovedClone(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	moved := filepath.Join(root, "renamed-folder")
	initRepo(t, moved, "https://github.com/me/tracked.git")

	res := planAndApply(t, cfg)
	if len(res.Relink) != 1 || len(res.Adopt) != 0 || len(res.Unmatched) != 0 {
		t.Fatalf("relink=%v adopt=%d unmatched=%v", res.Relink, len(res.Adopt), reasons(res))
	}
	if got := status.RepoPath(cfg, "github-me", "me/tracked", cfg.Sources["github-me"].Repos["me/tracked"]); got != moved {
		t.Errorf("tracked repo resolves to %s, want %s", got, moved)
	}
	if len(cfg.Sources["github-me"].Repos) != 1 {
		t.Errorf("repos = %v, want the one tracked repo, not a duplicate", cfg.Sources["github-me"].Repos)
	}
}

func TestPlan_KeepsADeletedCloneTracked(t *testing.T) {
	cfg := testConfig(t.TempDir())
	res := planAndApply(t, cfg)
	if len(res.Relink)+len(res.Adopt)+len(res.Unmatched) != 0 {
		t.Fatalf("nothing on disk, yet %+v", res)
	}
	if _, ok := cfg.Sources["github-me"].Repos["me/tracked"]; !ok {
		t.Error("the not-cloned repo was dropped")
	}
}

func TestPlan_LeavesReposOfAnOfflineRootAlone(t *testing.T) {
	online := t.TempDir()
	offline := filepath.Join(t.TempDir(), "unplugged")
	cfg := testConfig(online, offline)
	src := cfg.Sources["github-me"]
	src.Repos["me/tracked"] = config.Repo{Root: offline}
	cfg.Sources["github-me"] = src
	// A second checkout of the same repo on the online root must not steal
	// the entry while its drive is away.
	initRepo(t, filepath.Join(online, "copy"), "https://github.com/me/tracked.git")

	res := planAndApply(t, cfg)
	if len(res.Relink) != 0 || reasons(res)["copy"] != ReasonTracked {
		t.Errorf("relink=%v unmatched=%v, want the copy left as already tracked", res.Relink, reasons(res))
	}
	if cfg.Sources["github-me"].Repos["me/tracked"].Root != offline {
		t.Error("the offline repo's entry changed")
	}
}

func TestPlan_SkipsIgnoredClones(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	path := filepath.Join(root, "scratch", "linux")
	initRepo(t, path, "https://github.com/torvalds/linux.git")
	cfg.IgnoreClone(path, true)

	res := planAndApply(t, cfg)
	if len(res.Ignored) != 1 || len(res.Unmatched) != 0 {
		t.Errorf("ignored=%d unmatched=%v", len(res.Ignored), reasons(res))
	}
}

func TestAdopt_IntoAPickedAccount(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	path := filepath.Join(root, "scratch", "linux")
	initRepo(t, path, "https://github.com/torvalds/linux.git")
	res, err := Plan(cfg)
	if err != nil || len(res.Unmatched) != 1 {
		t.Fatalf("plan: %v %v", err, reasons(res))
	}
	source, err := Adopt(cfg, res.Unmatched[0].OrphanRepo, "github-me")
	if err != nil || source != "github-me" {
		t.Fatalf("Adopt = %q, %v", source, err)
	}
	if r := cfg.Sources["github-me"].Repos["torvalds/linux"]; r.CloneFolder != path {
		t.Errorf("adopted repo = %+v, want clone_folder %s", r, path)
	}
}

func equal(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestPlan_NeverAdoptsALinkedWorktree(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	main := filepath.Join(root, "scratch", "main")
	initRepo(t, main, "https://github.com/me/untracked.git")
	cmd := exec.Command("git", "-C", main, "-c", "user.name=t", "-c", "user.email=t@e", "commit", "-q", "--allow-empty", "-m", "first")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	tree := filepath.Join(root, "scratch", "main-feature")
	if out, err := exec.Command("git", "-C", main, "worktree", "add", "-q", tree).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}

	res := planAndApply(t, cfg)
	if got := reasons(res)["main-feature"]; got != ReasonWorktree {
		t.Errorf("worktree reason = %q, want %q (unmatched %v)", got, ReasonWorktree, reasons(res))
	}
	for _, o := range res.Adopt {
		if o.Path == tree {
			t.Error("the linked worktree was adopted")
		}
	}
}
