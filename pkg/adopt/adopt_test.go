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
