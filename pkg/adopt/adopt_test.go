package adopt

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// initBareRepo creates a minimal git repo at path with the given origin remote.
func initBareRepo(t *testing.T, path, remoteURL string) {
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
	run("init")
	run("config", "user.name", "test")
	run("config", "user.email", "test@test.com")
	if remoteURL != "" {
		run("remote", "add", "origin", remoteURL)
	}
}

func testConfig(parentFolder string) *config.Config {
	return &config.Config{
		Version: 2,
		Global:  config.GlobalConfig{Folder: parentFolder},
		Accounts: map[string]config.Account{
			"github-luis": {
				Provider: "github",
				URL:      "https://github.com",
				Username: "LuisPalacios",
				Name:     "Luis",
				Email:    "luis@test.com",
			},
			"gitea-luis": {
				Provider: "gitea",
				URL:      "https://git.parchis.org",
				Username: "luis",
				Name:     "Luis",
				Email:    "luis@test.com",
				SSH:      &config.SSHConfig{Host: "gitbox-gitea-luis"},
			},
		},
		Sources: map[string]config.Source{
			"github-luis": {
				Account: "github-luis",
				Repos: map[string]config.Repo{
					"LuisPalacios/tracked-repo": {},
				},
			},
			"gitea-luis": {
				Account: "gitea-luis",
				Repos:   map[string]config.Repo{},
			},
		},
		SourceOrder: []string{"github-luis", "gitea-luis"},
	}
}

func TestFindOrphans_Basic(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)

	// Create a tracked repo (should NOT appear as orphan).
	tracked := filepath.Join(root, "github-luis", "LuisPalacios", "tracked-repo")
	initBareRepo(t, tracked, "https://github.com/LuisPalacios/tracked-repo.git")

	// Create an orphan repo with a matching account.
	orphan1 := filepath.Join(root, "random", "orphan-repo")
	initBareRepo(t, orphan1, "https://github.com/LuisPalacios/orphan-repo.git")

	// Create an orphan with no matching account.
	orphan2 := filepath.Join(root, "unknown", "mystery")
	initBareRepo(t, orphan2, "https://gitlab.com/somebody/mystery.git")

	// Create a local-only repo (no remote).
	local := filepath.Join(root, "scratch", "experiment")
	initBareRepo(t, local, "")

	orphans, err := FindOrphans(cfg)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}

	if len(orphans) != 3 {
		t.Fatalf("expected 3 orphans, got %d: %+v", len(orphans), orphans)
	}

	// Build a map for easier assertions.
	byRelPath := make(map[string]OrphanRepo)
	for _, o := range orphans {
		byRelPath[o.RelPath] = o
	}

	// Orphan 1: matched to github-luis.
	o1 := byRelPath["random/orphan-repo"]
	if o1.MatchedAccount != "github-luis" {
		t.Errorf("orphan1: matched account = %q, want github-luis", o1.MatchedAccount)
	}
	if o1.MatchedSource != "github-luis" {
		t.Errorf("orphan1: matched source = %q, want github-luis", o1.MatchedSource)
	}
	if o1.RepoKey != "LuisPalacios/orphan-repo" {
		t.Errorf("orphan1: repo key = %q, want LuisPalacios/orphan-repo", o1.RepoKey)
	}
	if !o1.NeedsRelocate {
		t.Error("orphan1: expected NeedsRelocate=true")
	}
	if o1.LocalOnly {
		t.Error("orphan1: should not be local-only")
	}

	// Orphan 2: unknown account.
	o2 := byRelPath["unknown/mystery"]
	if o2.MatchedAccount != "" {
		t.Errorf("orphan2: matched account = %q, want empty", o2.MatchedAccount)
	}
	if o2.LocalOnly {
		t.Error("orphan2: should not be local-only")
	}

	// Local-only.
	o3 := byRelPath["scratch/experiment"]
	if !o3.LocalOnly {
		t.Error("local repo: expected LocalOnly=true")
	}
}

func TestFindOrphans_SSHHostAlias(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)

	// Create orphan with SSH alias remote matching gitea-luis.
	orphan := filepath.Join(root, "some", "homelab")
	initBareRepo(t, orphan, "git@gitbox-gitea-luis:luis/homelab.git")

	orphans, err := FindOrphans(cfg)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}

	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan, got %d", len(orphans))
	}
	if orphans[0].MatchedAccount != "gitea-luis" {
		t.Errorf("matched account = %q, want gitea-luis", orphans[0].MatchedAccount)
	}
}

func TestFindOrphans_InPlace(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)

	// Create orphan at the exact expected path (no relocation needed).
	orphan := filepath.Join(root, "github-luis", "LuisPalacios", "new-repo")
	initBareRepo(t, orphan, "https://github.com/LuisPalacios/new-repo.git")

	orphans, err := FindOrphans(cfg)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}

	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan, got %d", len(orphans))
	}
	if orphans[0].NeedsRelocate {
		t.Error("expected NeedsRelocate=false for repo already at expected path")
	}
}

func TestFindOrphans_Empty(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)

	// Create only tracked repos.
	tracked := filepath.Join(root, "github-luis", "LuisPalacios", "tracked-repo")
	initBareRepo(t, tracked, "https://github.com/LuisPalacios/tracked-repo.git")

	orphans, err := FindOrphans(cfg)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("expected 0 orphans, got %d: %+v", len(orphans), orphans)
	}
}

func TestMatchAccount(t *testing.T) {
	cfg := testConfig(t.TempDir())

	tests := []struct {
		name      string
		host      string
		owner     string
		wantAcct  string
		wantSrc   string
	}{
		{"github direct match", "github.com", "LuisPalacios", "github-luis", "github-luis"},
		{"github owner mismatch (still matches host)", "github.com", "other-user", "github-luis", "github-luis"},
		{"gitea via hostname", "git.parchis.org", "luis", "gitea-luis", "gitea-luis"},
		{"gitea via SSH alias", "gitbox-gitea-luis", "luis", "gitea-luis", "gitea-luis"},
		{"no match", "bitbucket.org", "someone", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acct, src := MatchAccount(cfg, tt.host, tt.owner)
			if acct != tt.wantAcct {
				t.Errorf("account = %q, want %q", acct, tt.wantAcct)
			}
			if src != tt.wantSrc {
				t.Errorf("source = %q, want %q", src, tt.wantSrc)
			}
		})
	}
}

// twoGithubConfig returns a config with two github.com accounts and a
// Forgejo account — the configuration shape that triggers the #35 bug.
func twoGithubConfig(parentFolder string) *config.Config {
	return &config.Config{
		Version: 2,
		Global:  config.GlobalConfig{Folder: parentFolder},
		Accounts: map[string]config.Account{
			"github-personal": {
				Provider: "github",
				URL:      "https://github.com",
				Username: "user-a",
				Name:     "User A",
				Email:    "a@test.com",
			},
			"github-work": {
				Provider: "github",
				URL:      "https://github.com",
				Username: "user-b",
				Name:     "User B",
				Email:    "b@test.com",
			},
			"forgejo-misc": {
				Provider: "forgejo",
				URL:      "https://forgejo.example.com",
				Username: "luis",
			},
		},
		Sources: map[string]config.Source{
			"github-personal": {Account: "github-personal", Repos: map[string]config.Repo{}},
			"github-work":     {Account: "github-work", Repos: map[string]config.Repo{}},
			"forgejo-misc":    {Account: "forgejo-misc", Repos: map[string]config.Repo{}},
		},
		SourceOrder: []string{"github-personal", "github-work", "forgejo-misc"},
	}
}

func TestMatchAccountEx_EmbeddedURLUser(t *testing.T) {
	root := t.TempDir()
	cfg := twoGithubConfig(root)

	// Orphan at ~/root/github-personal/org-a/repo-a with remote embedding user-b
	// (the Username of github-work). Without the URL-user signal this would
	// tie on host alone and pick the wrong account.
	orphanPath := filepath.Join(root, "github-personal", "org-a", "repo-a")
	if err := os.MkdirAll(orphanPath, 0o755); err != nil {
		t.Fatal(err)
	}

	acct, _, amb := MatchAccountEx(cfg, MatchContext{
		Host:         "github.com",
		Owner:        "org-a",
		RemoteURL:    "https://user-b@github.com/org-a/repo-a.git",
		RepoPath:     orphanPath,
		ParentFolder: root,
	})
	if acct != "github-work" {
		t.Errorf("account = %q, want github-work (URL-user signal should win)", acct)
	}
	if len(amb) != 0 {
		t.Errorf("expected unambiguous match, got candidates %v", amb)
	}
}

func TestMatchAccountEx_CredentialUsername(t *testing.T) {
	root := t.TempDir()
	cfg := twoGithubConfig(root)

	orphanPath := filepath.Join(root, "misc", "repo")
	initBareRepo(t, orphanPath, "https://github.com/org-a/repo.git")

	// Set credential.<url>.username in the repo — simulates what `git clone`
	// with a specific account leaves behind.
	cmd := exec.Command("git", "config", "credential.https://github.com.username", "user-b")
	cmd.Dir = orphanPath
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config: %v\n%s", err, out)
	}

	acct, _, amb := MatchAccountEx(cfg, MatchContext{
		Host:         "github.com",
		Owner:        "org-a",
		RemoteURL:    "https://github.com/org-a/repo.git",
		RepoPath:     orphanPath,
		ParentFolder: root,
	})
	if acct != "github-work" {
		t.Errorf("account = %q, want github-work (credential username signal)", acct)
	}
	if len(amb) != 0 {
		t.Errorf("expected unambiguous match, got candidates %v", amb)
	}
}

func TestMatchAccountEx_ParentFolderMatch(t *testing.T) {
	root := t.TempDir()
	cfg := twoGithubConfig(root)

	// Orphan sits under the github-work source folder — that alone should
	// break the tie when no stronger signal fires.
	orphanPath := filepath.Join(root, "github-work", "org-a", "repo")
	if err := os.MkdirAll(orphanPath, 0o755); err != nil {
		t.Fatal(err)
	}

	acct, _, amb := MatchAccountEx(cfg, MatchContext{
		Host:         "github.com",
		Owner:        "org-a",
		RemoteURL:    "https://github.com/org-a/repo.git",
		RepoPath:     orphanPath,
		ParentFolder: root,
	})
	if acct != "github-work" {
		t.Errorf("account = %q, want github-work (parent-folder signal)", acct)
	}
	if len(amb) != 0 {
		t.Errorf("expected unambiguous match, got candidates %v", amb)
	}
}

func TestMatchAccountEx_Ambiguous(t *testing.T) {
	root := t.TempDir()
	cfg := twoGithubConfig(root)

	// Orphan lives off to the side (no parent-folder signal) with a plain
	// remote URL (no embedded user, no credential helper) and an org owner
	// that matches neither Username. Both github accounts tie at host=1.
	orphanPath := filepath.Join(root, "elsewhere", "org-c", "repo")
	if err := os.MkdirAll(orphanPath, 0o755); err != nil {
		t.Fatal(err)
	}

	acct, src, amb := MatchAccountEx(cfg, MatchContext{
		Host:         "github.com",
		Owner:        "org-c",
		RemoteURL:    "https://github.com/org-c/repo.git",
		RepoPath:     orphanPath,
		ParentFolder: root,
	})
	if acct != "" || src != "" {
		t.Errorf("expected empty acct/src on ambiguous match, got %q/%q", acct, src)
	}
	if len(amb) != 2 {
		t.Fatalf("expected 2 tied candidates, got %d: %v", len(amb), amb)
	}
	seen := map[string]bool{}
	for _, c := range amb {
		seen[c] = true
	}
	if !seen["github-personal"] || !seen["github-work"] {
		t.Errorf("expected github-personal and github-work in candidates, got %v", amb)
	}
}

func TestMatchAccountEx_UnambiguousWhenOnlyOneHostMatches(t *testing.T) {
	// Regression guard: with exactly one host-matching account, score=1 alone
	// is enough (no tie possible).
	root := t.TempDir()
	cfg := twoGithubConfig(root)
	orphanPath := filepath.Join(root, "x", "y", "z")
	acct, _, amb := MatchAccountEx(cfg, MatchContext{
		Host:         "forgejo.example.com",
		Owner:        "some-org",
		RemoteURL:    "https://forgejo.example.com/some-org/repo.git",
		RepoPath:     orphanPath,
		ParentFolder: root,
	})
	if acct != "forgejo-misc" {
		t.Errorf("account = %q, want forgejo-misc", acct)
	}
	if len(amb) != 0 {
		t.Errorf("expected no ambiguity, got %v", amb)
	}
}

func TestHostnameFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com", "github.com"},
		{"https://git.parchis.org/", "git.parchis.org"},
		{"https://gitlab.com:8443", "gitlab.com"},
		{"not-a-url", "not-a-url"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := HostnameFromURL(tt.url)
			if got != tt.want {
				t.Errorf("HostnameFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
