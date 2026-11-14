package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

func TestGetRepoDetail_IncomingAndLocalFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(dir, "global.gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_COUNT", "0")
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(git.GitBin(), args...)
		cmd.Dir = dir
		cmd.Env = git.Environ()
		git.HideWindow(cmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "--initial-branch=main")
	run("config", "user.name", "Test Author")
	run("config", "user.email", "test@example.invalid")
	run("commit", "--allow-empty", "-m", "Shared base")
	run("remote", "add", "origin", "https://example.invalid/repository.git")
	run("switch", "-c", "remote-work")
	run("commit", "--allow-empty", "-m", "Incoming change")
	hash := run("rev-parse", "HEAD")
	run("update-ref", "refs/remotes/origin/release", "HEAD")
	run("switch", "main")
	run("branch", "--set-upstream-to=origin/release", "main")
	if err := os.WriteFile(filepath.Join(dir, "pending.txt"), []byte("local work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{
		cfg: &config.Config{Sources: map[string]config.Source{
			"account-a": {Repos: map[string]config.Repo{"org/repo": {CloneFolder: dir}}},
		}},
		// I disable provider probing so this binding test uses local Git only.
		refreshClosed: true,
	}
	detail := a.GetRepoDetail("account-a", "org/repo")
	if detail.Error != "" || detail.IncomingError != "" || detail.Behind != 1 {
		t.Fatalf("detail = %#v", detail)
	}
	if len(detail.Incoming) != 1 || detail.Incoming[0].Hash != hash || detail.Incoming[0].Author != "Test Author" || detail.Incoming[0].Subject != "Incoming change" {
		t.Fatalf("incoming = %#v", detail.Incoming)
	}
	if len(detail.Untracked) != 1 || detail.Untracked[0] != "pending.txt" {
		t.Fatalf("local files = %#v", detail.Untracked)
	}

	// I keep file details when Git cannot render the history.
	run("config", "log.showRoot", "invalid-boolean")
	detail = a.GetRepoDetail("account-a", "org/repo")
	if detail.Error != "" || detail.IncomingError == "" || len(detail.Untracked) != 1 {
		t.Fatalf("history failure lost local detail: %#v", detail)
	}
	run("config", "--unset", "log.showRoot")

	run("merge", "--ff-only", "origin/release")
	detail = a.GetRepoDetail("account-a", "org/repo")
	if detail.Error != "" || detail.IncomingError != "" || detail.Behind != 0 || len(detail.Incoming) != 0 || len(detail.Untracked) != 1 {
		t.Fatalf("detail after update = %#v", detail)
	}
	run("branch", "--unset-upstream")
	detail = a.GetRepoDetail("account-a", "org/repo")
	if detail.Error != "" || detail.IncomingError != "" || len(detail.Incoming) != 0 {
		t.Fatalf("detail without upstream = %#v", detail)
	}
}
