package status

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func initTestRepo(t *testing.T) (clone, bare string) {
	t.Helper()
	dir := t.TempDir()
	bare = filepath.Join(dir, "remote.git")
	runGit(t, dir, "init", "--bare", bare)
	clone = filepath.Join(dir, "clone")
	runGit(t, dir, "clone", bare, clone)
	runGit(t, clone, "config", "user.name", "Test")
	runGit(t, clone, "config", "user.email", "test@test.com")
	writeFile(t, filepath.Join(clone, "README.md"), "# test\n")
	runGit(t, clone, "add", "README.md")
	runGit(t, clone, "commit", "-m", "initial")
	runGit(t, clone, "push", "origin", "master")
	return clone, bare
}

func TestCheckClean(t *testing.T) {
	clone, _ := initTestRepo(t)
	rs := Check(clone)
	if rs.State != Clean {
		t.Errorf("state = %v, want Clean", rs.State)
	}
}

func TestCheck_BranchPopulated(t *testing.T) {
	clone, _ := initTestRepo(t)
	rs := Check(clone)
	if rs.Branch == "" {
		t.Error("expected Branch to be populated")
	}
	// initTestRepo creates a repo with master as default.
	if rs.Branch != "master" {
		t.Errorf("branch = %q, want %q", rs.Branch, "master")
	}
}

func TestCheck_IsDefault(t *testing.T) {
	clone, _ := initTestRepo(t)
	rs := Check(clone)
	if !rs.IsDefault {
		t.Error("expected IsDefault = true for default branch")
	}
}

func TestCheck_FeatureBranch(t *testing.T) {
	clone, _ := initTestRepo(t)
	runGit(t, clone, "checkout", "-b", "feature-xyz")
	rs := Check(clone)
	if rs.Branch != "feature-xyz" {
		t.Errorf("branch = %q, want %q", rs.Branch, "feature-xyz")
	}
	if rs.IsDefault {
		t.Error("expected IsDefault = false for feature branch")
	}
	// Local branch with no upstream → NoUpstream state.
	if rs.State != NoUpstream {
		t.Errorf("state = %v, want NoUpstream", rs.State)
	}
}

func TestCheck_DetachedHead(t *testing.T) {
	clone, _ := initTestRepo(t)
	// Detach HEAD at the current commit.
	runGit(t, clone, "checkout", "--detach", "HEAD")
	rs := Check(clone)
	if rs.Branch != "(detached)" {
		t.Errorf("branch = %q, want %q", rs.Branch, "(detached)")
	}
	if rs.IsDefault {
		t.Error("expected IsDefault = false for detached HEAD")
	}
}

func TestCheckDirty(t *testing.T) {
	clone, _ := initTestRepo(t)
	writeFile(t, filepath.Join(clone, "README.md"), "# changed\n")
	rs := Check(clone)
	if rs.State != Dirty {
		t.Errorf("state = %v, want Dirty", rs.State)
	}
	if rs.Modified == 0 {
		t.Error("expected modified > 0")
	}
}

func TestCheckNotCloned(t *testing.T) {
	rs := Check(filepath.Join(t.TempDir(), "nonexistent"))
	if rs.State != NotCloned {
		t.Errorf("state = %v, want NotCloned", rs.State)
	}
}

func TestCheckBehind(t *testing.T) {
	clone, bare := initTestRepo(t)

	// Create a second clone, push a commit.
	dir := t.TempDir()
	clone2 := filepath.Join(dir, "clone2")
	runGit(t, dir, "clone", bare, clone2)
	runGit(t, clone2, "config", "user.name", "Test")
	runGit(t, clone2, "config", "user.email", "test@test.com")
	writeFile(t, filepath.Join(clone2, "new.txt"), "new\n")
	runGit(t, clone2, "add", "new.txt")
	runGit(t, clone2, "commit", "-m", "new commit")
	runGit(t, clone2, "push", "origin", "master")

	// Fetch in original clone.
	runGit(t, clone, "fetch", "--all")

	rs := Check(clone)
	if rs.State != Behind {
		t.Errorf("state = %v, want Behind", rs.State)
	}
	if rs.Behind != 1 {
		t.Errorf("behind = %d, want 1", rs.Behind)
	}
}

func TestCheckAhead(t *testing.T) {
	clone, _ := initTestRepo(t)
	writeFile(t, filepath.Join(clone, "local.txt"), "local\n")
	runGit(t, clone, "add", "local.txt")
	runGit(t, clone, "commit", "-m", "local")

	rs := Check(clone)
	if rs.State != Ahead {
		t.Errorf("state = %v, want Ahead", rs.State)
	}
	if rs.Ahead != 1 {
		t.Errorf("ahead = %d, want 1", rs.Ahead)
	}
}

func TestCheckAll(t *testing.T) {
	clone, _ := initTestRepo(t)
	dir := filepath.Dir(clone)

	// Build a config pointing to our test repo.
	cfg := &config.Config{
		Version: 2,
		Global: config.GlobalConfig{
			Folder: dir,
		},
		Accounts: map[string]config.Account{
			"TestAccount": {
				Provider: "generic",
				URL:      "https://example.com",
				Username: "test",
				Name:     "Test",
				Email:    "test@test.com",
			},
		},
		Sources: map[string]config.Source{
			"TestSource": {
				Account: "TestAccount",
				Folder:  ".", // repos directly under dir
				Repos: map[string]config.Repo{
					"clone":       {CredentialType: "gcm"},
					"nonexistent": {CredentialType: "gcm"},
				},
			},
		},
	}

	results := CheckAll(cfg)
	if len(results) != 2 {
		t.Fatalf("results count = %d, want 2", len(results))
	}

	// Find each result.
	states := map[string]State{}
	for _, r := range results {
		states[r.Repo] = r.State
	}
	if states["clone"] != Clean {
		t.Errorf("clone state = %v, want Clean", states["clone"])
	}
	if states["nonexistent"] != NotCloned {
		t.Errorf("nonexistent state = %v, want NotCloned", states["nonexistent"])
	}
}

// TestCheckAll_Ordered verifies that CheckAll's parallel worker pool
// preserves the iteration order of OrderedSourceKeys × OrderedRepoKeys.
// Regression guard for the parallelization introduced in issue #57.
func TestCheckAll_Ordered(t *testing.T) {
	dir := t.TempDir()

	// Build a config with multiple sources, each with multiple repos.
	// None of the repos exist on disk — Check returns NotCloned quickly,
	// which is enough to test ordering without spawning real git processes.
	sourceOrder := []string{"src-a", "src-b", "src-c"}
	var repoOrder []string
	for i := 0; i < 10; i++ {
		repoOrder = append(repoOrder, fmt.Sprintf("org/repo-%02d", i))
	}

	cfg := &config.Config{
		Version:     2,
		Global:      config.GlobalConfig{Folder: dir},
		SourceOrder: sourceOrder,
		Accounts: map[string]config.Account{
			"acctA": {Provider: "github", URL: "https://example.com", Username: "a"},
		},
		Sources: map[string]config.Source{},
	}
	for _, s := range sourceOrder {
		repos := map[string]config.Repo{}
		for _, r := range repoOrder {
			repos[r] = config.Repo{CredentialType: "gcm"}
		}
		// Copy repoOrder so every source has an independent slice.
		ro := append([]string(nil), repoOrder...)
		cfg.Sources[s] = config.Source{
			Account:   "acctA",
			Folder:    s,
			Repos:     repos,
			RepoOrder: ro,
		}
	}

	results := CheckAll(cfg)
	if got, want := len(results), len(sourceOrder)*len(repoOrder); got != want {
		t.Fatalf("results count = %d, want %d", got, want)
	}

	var expected []string
	for _, s := range sourceOrder {
		for _, r := range repoOrder {
			expected = append(expected, s+"|"+r)
		}
	}
	for i, r := range results {
		got := r.Source + "|" + r.Repo
		if got != expected[i] {
			t.Errorf("results[%d] = %q, want %q", i, got, expected[i])
		}
		if r.State != NotCloned {
			t.Errorf("results[%d] state = %v, want NotCloned", i, r.State)
		}
		if r.Account != "acctA" {
			t.Errorf("results[%d] account = %q, want %q", i, r.Account, "acctA")
		}
	}
}

func TestResolveRepoPath(t *testing.T) {
	sep := string(filepath.Separator)
	tests := []struct {
		name         string
		globalFolder string
		sourceFolder string
		repoName     string
		repo         config.Repo
		wantSuffix   string
	}{
		{
			name:         "default: org/repo → nested dirs",
			globalFolder: "/home/user/git",
			sourceFolder: "github",
			repoName:     "MyGitHubUser/my-repo",
			wantSuffix:   sep + "github" + sep + "MyGitHubUser" + sep + "my-repo",
		},
		{
			name:         "id_folder override: changes 2nd level",
			globalFolder: "/home/user/git",
			sourceFolder: "github",
			repoName:     "MyOrg/myorg.web",
			repo:         config.Repo{IdFolder: "myorg-rest"},
			wantSuffix:   sep + "github" + sep + "myorg-rest" + sep + "myorg.web",
		},
		{
			name:         "clone_folder override: changes 3rd level",
			globalFolder: "/home/user/git",
			sourceFolder: "github",
			repoName:     "MyOrg/myorg.github.io",
			repo:         config.Repo{CloneFolder: "website"},
			wantSuffix:   sep + "github" + sep + "MyOrg" + sep + "website",
		},
		{
			name:         "both overrides",
			globalFolder: "/home/user/git",
			sourceFolder: "github",
			repoName:     "MyOrg/myorg.github.io",
			repo:         config.Repo{IdFolder: "sw-rest", CloneFolder: "website"},
			wantSuffix:   sep + "github" + sep + "sw-rest" + sep + "website",
		},
		{
			name:         "absolute clone_folder replaces everything",
			globalFolder: "/home/user/git",
			sourceFolder: "github",
			repoName:     "org/my-repo",
			repo:         config.Repo{CloneFolder: "/opt/repos/special"},
			wantSuffix:   "/opt/repos/special",
		},
		{
			name:         "tilde clone_folder replaces everything",
			globalFolder: "/home/user/git",
			sourceFolder: "github",
			repoName:     "org/my-config",
			repo:         config.Repo{CloneFolder: "~/.config/my-config"},
			wantSuffix:   ".config" + sep + "my-config", // partial check, tilde expanded
		},
		{
			name:         "no slash in repo name",
			globalFolder: "/home/user/git",
			sourceFolder: "github",
			repoName:     "simple-repo",
			wantSuffix:   sep + "github" + sep + "simple-repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveRepoPath(tt.globalFolder, tt.sourceFolder, tt.repoName, tt.repo)
			if runtime.GOOS == "windows" && tt.repo.CloneFolder != "" && (filepath.IsAbs(config.ExpandTilde(tt.repo.CloneFolder))) {
				if !filepath.IsAbs(got) {
					t.Errorf("expected absolute path, got %q", got)
				}
				return
			}
			if !containsSuffix(got, tt.wantSuffix) {
				t.Errorf("ResolveRepoPath() = %q, want suffix %q", got, tt.wantSuffix)
			}
		})
	}
}

func containsSuffix(s, suffix string) bool {
	// Normalize separators for cross-platform.
	s = filepath.ToSlash(s)
	suffix = filepath.ToSlash(suffix)
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{Clean, "clean"},
		{Dirty, "dirty"},
		{Behind, "behind"},
		{Ahead, "ahead"},
		{Diverged, "diverged"},
		{Conflict, "conflict"},
		{NotCloned, "not cloned"},
		{Error, "error"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}
