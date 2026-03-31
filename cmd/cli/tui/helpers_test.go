package tui

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// ---------------------------------------------------------------------------
// cloneURL
// ---------------------------------------------------------------------------

func TestCloneURL_Token(t *testing.T) {
	acct := config.Account{
		URL:      "https://github.com",
		Username: "alice",
	}
	got := cloneURL(acct, "alice/myrepo", "token")
	want := "https://alice@github.com/alice/myrepo.git"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCloneURL_SSH_WithHost(t *testing.T) {
	acct := config.Account{
		URL: "https://github.com",
		SSH: &config.SSHConfig{Host: "gitbox-github-alice", Hostname: "github.com"},
	}
	got := cloneURL(acct, "alice/myrepo", "ssh")
	want := "git@gitbox-github-alice:alice/myrepo.git"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCloneURL_SSH_NoHost(t *testing.T) {
	acct := config.Account{
		URL: "https://github.com",
	}
	got := cloneURL(acct, "alice/myrepo", "ssh")
	want := "git@github.com:alice/myrepo.git"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCloneURL_GCM(t *testing.T) {
	acct := config.Account{
		URL:      "https://github.com",
		Username: "alice",
	}
	got := cloneURL(acct, "org/repo", "gcm")
	want := "https://alice@github.com/org/repo.git"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// stripScheme
// ---------------------------------------------------------------------------

func TestStripScheme(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://github.com", "github.com"},
		{"http://gitlab.com", "gitlab.com"},
		{"github.com", "github.com"},
		{"https://forge.example.com:3000", "forge.example.com:3000"},
	}
	for _, tt := range tests {
		got := stripScheme(tt.input)
		if got != tt.want {
			t.Errorf("stripScheme(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// reconfigureClones
// ---------------------------------------------------------------------------

func TestReconfigureClones(t *testing.T) {
	env := setupTestEnv(t)

	cfg := newTestConfig(t, env.GitFolder)
	cfg.Accounts["github-alice"] = config.Account{
		Provider:              "github",
		URL:                   "https://github.com",
		Username:              "alice",
		DefaultCredentialType: "token",
	}
	cfg.Sources["alice-repos"] = config.Source{
		Account: "github-alice",
		Repos: map[string]config.Repo{
			"alice/testrepo": {},
		},
	}

	// Create a real git clone in the expected path
	repoPath := filepath.Join(env.GitFolder, "alice-repos", "alice", "testrepo")
	cmd := exec.Command(git.GitBin(), "init", repoPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	// Set an initial remote
	cmd = exec.Command(git.GitBin(), "-C", repoPath, "remote", "add", "origin", "https://old-url.com/alice/testrepo.git")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}

	count := reconfigureClones(cfg, "github-alice")
	if count != 1 {
		t.Errorf("expected 1 repo reconfigured, got %d", count)
	}

	// Verify the remote URL was changed
	url, err := git.RemoteURL(repoPath)
	if err != nil {
		t.Fatalf("getting remote url: %v", err)
	}
	want := "https://alice@github.com/alice/testrepo.git"
	if url != want {
		t.Errorf("remote URL = %q, want %q", url, want)
	}
}

// ---------------------------------------------------------------------------
// countClonedRepos
// ---------------------------------------------------------------------------

func TestCountClonedRepos(t *testing.T) {
	env := setupTestEnv(t)

	cfg := newTestConfig(t, env.GitFolder)
	cfg.Accounts["github-alice"] = config.Account{
		Provider: "github",
		URL:      "https://github.com",
		Username: "alice",
	}
	cfg.Sources["alice-repos"] = config.Source{
		Account: "github-alice",
		Repos: map[string]config.Repo{
			"alice/repo1": {},
			"alice/repo2": {},
			"alice/repo3": {},
		},
	}

	// Create only 2 of 3 as git repos
	for _, name := range []string{"repo1", "repo2"} {
		repoPath := filepath.Join(env.GitFolder, "alice-repos", "alice", name)
		cmd := exec.Command(git.GitBin(), "init", repoPath)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init %s: %v\n%s", name, err, out)
		}
	}

	count := countClonedRepos(cfg, "github-alice")
	if count != 2 {
		t.Errorf("expected 2 cloned repos, got %d", count)
	}
}
