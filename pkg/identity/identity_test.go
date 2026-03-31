package identity

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// initBareRepo creates a git repo in a temp dir and returns its path.
func initBareRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	cmd := exec.Command(git.GitBin(), "init", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

func TestResolveIdentity_AccountFallback(t *testing.T) {
	acct := config.Account{Name: "Alice", Email: "alice@example.com"}
	repo := config.Repo{} // no overrides

	name, email := ResolveIdentity(repo, acct)
	if name != "Alice" || email != "alice@example.com" {
		t.Errorf("expected Alice/alice@example.com, got %s/%s", name, email)
	}
}

func TestResolveIdentity_RepoOverride(t *testing.T) {
	acct := config.Account{Name: "Alice", Email: "alice@example.com"}
	repo := config.Repo{Name: "Bob", Email: "bob@corp.com"}

	name, email := ResolveIdentity(repo, acct)
	if name != "Bob" || email != "bob@corp.com" {
		t.Errorf("expected Bob/bob@corp.com, got %s/%s", name, email)
	}
}

func TestResolveIdentity_PartialOverride(t *testing.T) {
	acct := config.Account{Name: "Alice", Email: "alice@example.com"}
	repo := config.Repo{Name: "Bob"} // email falls back to account

	name, email := ResolveIdentity(repo, acct)
	if name != "Bob" || email != "alice@example.com" {
		t.Errorf("expected Bob/alice@example.com, got %s/%s", name, email)
	}
}

func TestEnsureRepoIdentity_SetsWhenMissing(t *testing.T) {
	repoPath := initBareRepo(t)

	fixedName, fixedEmail, err := EnsureRepoIdentity(repoPath, "Alice", "alice@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fixedName || !fixedEmail {
		t.Errorf("expected both fixed, got name=%v email=%v", fixedName, fixedEmail)
	}

	// Verify values were written
	name, _ := git.ConfigGet(repoPath, "user.name")
	email, _ := git.ConfigGet(repoPath, "user.email")
	if name != "Alice" || email != "alice@example.com" {
		t.Errorf("expected Alice/alice@example.com, got %s/%s", name, email)
	}
}

func TestEnsureRepoIdentity_FixesWhenWrong(t *testing.T) {
	repoPath := initBareRepo(t)

	// Set wrong values
	git.ConfigSet(repoPath, "user.name", "Wrong")
	git.ConfigSet(repoPath, "user.email", "wrong@example.com")

	fixedName, fixedEmail, err := EnsureRepoIdentity(repoPath, "Right", "right@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fixedName || !fixedEmail {
		t.Errorf("expected both fixed, got name=%v email=%v", fixedName, fixedEmail)
	}

	name, _ := git.ConfigGet(repoPath, "user.name")
	email, _ := git.ConfigGet(repoPath, "user.email")
	if name != "Right" || email != "right@example.com" {
		t.Errorf("expected Right/right@example.com, got %s/%s", name, email)
	}
}

func TestEnsureRepoIdentity_NoopWhenCorrect(t *testing.T) {
	repoPath := initBareRepo(t)

	// Set correct values
	git.ConfigSet(repoPath, "user.name", "Alice")
	git.ConfigSet(repoPath, "user.email", "alice@example.com")

	fixedName, fixedEmail, err := EnsureRepoIdentity(repoPath, "Alice", "alice@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fixedName || fixedEmail {
		t.Errorf("expected no fixes, got name=%v email=%v", fixedName, fixedEmail)
	}
}

func TestCheckGlobalIdentity(t *testing.T) {
	// Just verify it doesn't panic — result depends on the machine's ~/.gitconfig
	s := CheckGlobalIdentity()
	_ = s.HasName
	_ = s.HasEmail
}
