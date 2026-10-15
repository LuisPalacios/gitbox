package heal

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// gitRun runs a git command in dir, returning git's output on failure.
func gitRun(dir string, args ...string) error {
	cmd := exec.Command(git.GitBin(), args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %v: %w: %s", args, err, out)
	}
	return nil
}

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
	if err := gitRun(repoDir, "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := gitRun(repoDir, "remote", "add", "origin", "https://alice@github.com/alice/hello.git"); err != nil {
		t.Fatalf("git remote add: %v", err)
	}

	cfg := &config.Config{
		Global: config.GlobalConfig{Folder: tmp},
		Accounts: map[string]config.Account{
			"alice": {
				URL:                   "https://github.com",
				Username:              "alice",
				Name:                  "Alice Example",
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

	r := Repo(cfg, "alice-repos", "alice/hello")

	// Confirm the identity lines are now present.
	snapshot, err := git.ReadConfigSnapshot(repoDir)
	if err != nil {
		t.Fatalf("reading user.name: %v", err)
	}
	if name := snapshot.Get("user.name"); name != "Alice Example" {
		t.Errorf("user.name = %q, want %q", name, "Alice Example")
	}
	if email := snapshot.Get("user.email"); email != "alice@example.com" {
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
	if err := gitRun(repoDir, "init"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	leakyURL := "https://alice:secret-token@github.com/alice/hello.git"
	if err := gitRun(repoDir, "remote", "add", "origin", leakyURL); err != nil {
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

func TestRepo_UnchangedUsesTwoReadsAndNoWrites(t *testing.T) {
	for _, typ := range []string{"ssh", "token", "gcm"} {
		t.Run(typ, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
			t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "global"))
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("GIT_TOKEN", "")
			t.Setenv("GITBOX_TOKEN_ACCOUNT_A", "fixture-token")
			path := filepath.Join(root, "repos", "team", "project")
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := gitRun(path, "init"); err != nil {
				t.Fatal(err)
			}
			acct := config.Account{URL: "https://example.org", Username: "account-a", Name: "Example", Email: "example@example.org", DefaultCredentialType: typ}
			if err := gitRun(path, "remote", "add", "origin", ExpectedOriginURL(acct, "team/project", typ)); err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{Global: config.GlobalConfig{Folder: root}, Accounts: map[string]config.Account{"account-a": acct}, Sources: map[string]config.Source{"repos": {Account: "account-a", Repos: map[string]config.Repo{"team/project": {}}}}}
			first := Repo(cfg, "repos", "team/project")
			if len(first.Warnings) != 0 {
				t.Fatalf("initial repair: %+v", first)
			}
			old := time.Unix(1_000_000_000, 0)
			configPath := filepath.Join(path, ".git", "config")
			if err := os.Chtimes(configPath, old, old); err != nil {
				t.Fatal(err)
			}
			trace := filepath.Join(root, "trace.json")
			t.Setenv("GIT_TRACE2_EVENT", trace)
			got := Repo(cfg, "repos", "team/project")
			if got.HasWork() || got.Skipped != "" {
				t.Fatalf("unchanged repo reported work: %+v", got)
			}
			data, err := os.ReadFile(trace)
			if err != nil {
				t.Fatal(err)
			}
			starts := 0
			for line := range strings.SplitSeq(string(data), "\n") {
				var event struct {
					Event string   `json:"event"`
					Argv  []string `json:"argv"`
				}
				if json.Unmarshal([]byte(line), &event) == nil && event.Event == "start" {
					starts++
					if !slices.Contains(event.Argv, "--list") {
						t.Errorf("unexpected Git command: %v", event.Argv)
					}
				}
			}
			if starts != 2 {
				t.Fatalf("spawned %d Git processes, want two batched reads", starts)
			}
			info, err := os.Stat(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if !info.ModTime().Equal(old) {
				t.Fatal("unchanged repo config was rewritten")
			}
		})
	}
}

func healFixture(t *testing.T) (*config.Config, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "global"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	path := filepath.Join(root, "repos", "team", "project")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := gitRun(path, "init"); err != nil {
		t.Fatal(err)
	}
	acct := config.Account{URL: "https://example.org", Username: "account-a", Name: "Example", Email: "example@example.org", DefaultCredentialType: "ssh"}
	if err := gitRun(path, "remote", "add", "origin", ExpectedOriginURL(acct, "team/project", "ssh")); err != nil {
		t.Fatal(err)
	}
	return &config.Config{Global: config.GlobalConfig{Folder: root}, Accounts: map[string]config.Account{"account-a": acct}, Sources: map[string]config.Source{"repos": {Account: "account-a", Repos: map[string]config.Repo{"team/project": {}}}}}, path
}

func TestRepo_ReportsShadowedIdentity(t *testing.T) {
	cfg, path := healFixture(t)
	if err := git.ConfigSet(path, "user.name", "old name"); err != nil {
		t.Fatal(err)
	}
	included := filepath.Join(t.TempDir(), "included")
	if err := os.WriteFile(included, []byte("[user]\n\tname = overriding name\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := git.ConfigSet(path, "include.path", included); err != nil {
		t.Fatal(err)
	}
	r := Repo(cfg, "repos", "team/project")
	if !strings.Contains(strings.Join(r.Warnings, " "), "identity is overridden") {
		t.Fatalf("overridden identity was reported as repaired: %+v", r)
	}
}

func TestRepo_LinkedWorktreeAndUnknownCredential(t *testing.T) {
	cfg, path := healFixture(t)
	if err := gitRun(path, "-c", "user.name=Example", "-c", "user.email=example@example.org", "commit", "--allow-empty", "-m", "initial"); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	if err := gitRun(path, "worktree", "add", "-b", "linked", linked); err != nil {
		t.Fatal(err)
	}
	cfg.Sources["repos"].Repos["team/project"] = config.Repo{CloneFolder: linked}
	r := Repo(cfg, "repos", "team/project")
	if r.Skipped != "" || len(r.Warnings) != 0 {
		t.Fatalf("linked worktree not healed: %+v", r)
	}
	if r = Repo(cfg, "repos", "team/project"); r.HasWork() || r.Skipped != "" {
		t.Fatalf("unchanged worktree reported drift: %+v", r)
	}
	configPath := filepath.Join(path, ".git", "config")
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	acct := cfg.Accounts["account-a"]
	acct.DefaultCredentialType, acct.Name = "unknown", "different name"
	cfg.Accounts["account-a"] = acct
	r = Repo(cfg, "repos", "team/project")
	if len(r.Warnings) == 0 {
		t.Fatal("unknown credential type accepted")
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("unknown type changed repo config")
	}
}

func TestRepo_FixesDriftedIdentity(t *testing.T) {
	cfg, path := healFixture(t)
	for _, setting := range [][2]string{{"user.name", "Wrong"}, {"user.email", "wrong@example.org"}} {
		if err := git.ConfigSet(path, setting[0], setting[1]); err != nil {
			t.Fatal(err)
		}
	}
	r := Repo(cfg, "repos", "team/project")
	if len(r.Warnings) != 0 {
		t.Fatalf("identity repair failed: %+v", r)
	}
	snapshot, err := git.ReadConfigSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	acct := cfg.Accounts["account-a"]
	if snapshot.Get("user.name") != acct.Name || snapshot.Get("user.email") != acct.Email {
		t.Fatal("drifted identity not repaired")
	}
}
