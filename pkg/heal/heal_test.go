package heal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// TestExpectedOriginURL_SSH verifies the SSH-form URL uses the SSH
// host override when present and falls back to the account URL otherwise.
func TestExpectedOriginURL_SSH(t *testing.T) {
	acct := config.Account{URL: "https://github.com", Username: "alice"}
	got := ExpectedOriginURL(acct, "alice/dotfiles", "ssh")
	want := "git@github.com:alice/dotfiles.git"
	if got != want {
		t.Errorf("no SSH override: got %q, want %q", got, want)
	}

	acct.SSH = &config.SSHConfig{Host: "github-alice"}
	got = ExpectedOriginURL(acct, "alice/dotfiles", "ssh")
	want = "git@github-alice:alice/dotfiles.git"
	if got != want {
		t.Errorf("with SSH override: got %q, want %q", got, want)
	}
}

// TestExpectedOriginURL_HTTPS_UsernameEmbedded verifies the HTTPS
// variant contains the account username (but no password).
func TestExpectedOriginURL_HTTPS_UsernameEmbedded(t *testing.T) {
	acct := config.Account{URL: "https://github.com", Username: "alice"}
	got := ExpectedOriginURL(acct, "alice/dotfiles", "gcm")
	want := "https://alice@github.com/alice/dotfiles.git"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestExpectedOriginURL_HTTPS_NoUsername falls back cleanly when the
// account doesn't have a username on file (unusual but possible).
func TestExpectedOriginURL_HTTPS_NoUsername(t *testing.T) {
	acct := config.Account{URL: "https://git.example.com"}
	got := ExpectedOriginURL(acct, "team/project", "token")
	want := "https://git.example.com/team/project.git"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRedactURL masks the password so tokens don't leak into logs.
func TestRedactURL(t *testing.T) {
	in := "https://alice:super-secret-token@github.com/alice/repo.git"
	got := redactURL(in)
	if strings.Contains(got, "super-secret-token") {
		t.Errorf("password leaked: %q", got)
	}
	if !strings.Contains(got, "alice") {
		t.Errorf("username stripped: %q", got)
	}
}

// TestRepo_NotClonedSkipsCleanly: if the clone isn't on disk, heal
// returns a Skipped report without warnings — this is the normal
// state for a freshly added repo before its first clone.
func TestRepo_NotClonedSkipsCleanly(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{
		Global: config.GlobalConfig{Folder: tmp},
		Accounts: map[string]config.Account{
			"a": {URL: "https://github.com", Username: "alice"},
		},
		Sources: map[string]config.Source{
			"a": {
				Account: "a",
				Repos:   map[string]config.Repo{"alice/missing": {}},
			},
		},
	}
	r := Repo(cfg, "a", "alice/missing")
	if r.Skipped != "not cloned" {
		t.Errorf("Skipped = %q, want %q", r.Skipped, "not cloned")
	}
	if len(r.Warnings) > 0 {
		t.Errorf("unexpected warnings: %v", r.Warnings)
	}
	if len(r.Fixed) > 0 {
		t.Errorf("unexpected fixes on missing clone: %v", r.Fixed)
	}
}

// TestRepo_FixesMissingIdentity: create a real empty git repo on
// disk, resolve it against a config with name/email set, confirm
// heal writes user.name/user.email into .git/config.
func TestRepo_FixesMissingIdentity(t *testing.T) {
	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "alice-repos", "alice", "hello")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := git.Run(repoDir, "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := git.Run(repoDir, "remote", "add", "origin", "https://alice@github.com/alice/hello.git"); err != nil {
		t.Fatalf("git remote add: %v", err)
	}

	cfg := &config.Config{
		Global: config.GlobalConfig{Folder: tmp},
		Accounts: map[string]config.Account{
			"alice": {
				URL:      "https://github.com",
				Username: "alice",
				Name:     "Alice Example",
				Email:    "alice@example.com",
				DefaultCredentialType: "gcm",
			},
		},
		Sources: map[string]config.Source{
			"alice-repos": {
				Account: "alice",
				Repos:   map[string]config.Repo{"alice/hello": {}},
			},
		},
	}

	r := Repo(cfg, "alice-repos", "alice/hello")

	// Confirm the identity lines are now present.
	name, err := git.ConfigGet(repoDir, "user.name")
	if err != nil {
		t.Fatalf("reading user.name: %v", err)
	}
	if name != "Alice Example" {
		t.Errorf("user.name = %q, want %q", name, "Alice Example")
	}
	email, err := git.ConfigGet(repoDir, "user.email")
	if err != nil {
		t.Fatalf("reading user.email: %v", err)
	}
	if email != "alice@example.com" {
		t.Errorf("user.email = %q, want %q", email, "alice@example.com")
	}

	// Heal should have reported the identity fix.
	joined := strings.Join(r.Fixed, "; ")
	if !strings.Contains(joined, "user.name") || !strings.Contains(joined, "user.email") {
		t.Errorf("Fixed did not mention identity: %v", r.Fixed)
	}
}

// TestRepo_StripsEmbeddedTokenFromOrigin: if origin carries a token
// in the URL, heal rewrites it to the canonical username-only form.
func TestRepo_StripsEmbeddedTokenFromOrigin(t *testing.T) {
	tmp := t.TempDir()
	repoDir := filepath.Join(tmp, "alice-repos", "alice", "hello")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := git.Run(repoDir, "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	leakyURL := "https://alice:secret-token@github.com/alice/hello.git"
	if err := git.Run(repoDir, "remote", "add", "origin", leakyURL); err != nil {
		t.Fatalf("git remote add: %v", err)
	}

	cfg := &config.Config{
		Global: config.GlobalConfig{Folder: tmp},
		Accounts: map[string]config.Account{
			"alice": {
				URL:                   "https://github.com",
				Username:              "alice",
				Name:                  "Alice",
				Email:                 "alice@example.com",
				DefaultCredentialType: "gcm",
			},
		},
		Sources: map[string]config.Source{
			"alice-repos": {
				Account: "alice",
				Repos:   map[string]config.Repo{"alice/hello": {}},
			},
		},
	}

	_ = Repo(cfg, "alice-repos", "alice/hello")

	got, err := git.RemoteURL(repoDir)
	if err != nil {
		t.Fatalf("remote get-url: %v", err)
	}
	want := "https://alice@github.com/alice/hello.git"
	if got != want {
		t.Errorf("origin URL not sanitized: got %q, want %q", got, want)
	}
}
