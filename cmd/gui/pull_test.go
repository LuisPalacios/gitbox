package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

func pullFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	result, err := git.Default.Run(t.Context(), git.Request{Dir: dir, Args: args})
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, result.Stderr)
	}
	return strings.TrimSuffix(string(result.Stdout), "\n")
}

func TestPullRepo_ChecksLocalWorkBeforeFastForward(t *testing.T) {
	for _, tc := range []struct {
		name        string
		skip        git.PullSkipReason
		state       status.State
		prepare     func(*testing.T, string)
		payloadPath string
	}{
		{
			name: "unstaged", skip: git.PullDirty, state: status.Dirty,
			prepare: func(t *testing.T, work string) {
				if err := os.WriteFile(filepath.Join(work, "local.txt"), []byte("my changes"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			payloadPath: "local.txt",
		},
		{
			name: "staged", skip: git.PullDirty, state: status.Dirty,
			prepare: func(t *testing.T, work string) {
				if err := os.WriteFile(filepath.Join(work, "local.txt"), []byte("my changes"), 0o600); err != nil {
					t.Fatal(err)
				}
				pullFixtureGit(t, work, "add", "local.txt")
			},
			payloadPath: "local.txt",
		},
		{
			name: "untracked hidden by Git config", skip: git.PullDirty, state: status.Dirty,
			prepare: func(t *testing.T, work string) {
				pullFixtureGit(t, work, "config", "status.showUntrackedFiles", "no")
				if err := os.WriteFile(filepath.Join(work, "untracked.txt"), []byte("my changes"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			payloadPath: "untracked.txt",
		},
		{
			name: "merge in progress", skip: git.PullInProgress, state: status.Behind,
			prepare: func(t *testing.T, work string) {
				dir := pullFixtureGit(t, work, "rev-parse", "--absolute-git-dir")
				head := pullFixtureGit(t, work, "rev-parse", "HEAD")
				if err := os.WriteFile(filepath.Join(dir, "MERGE_HEAD"), []byte(head+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "conflict", skip: git.PullConflict, state: status.Conflict,
			prepare: func(t *testing.T, work string) {
				pullFixtureGit(t, work, "switch", "-c", "conflict")
				if err := os.WriteFile(filepath.Join(work, "local.txt"), []byte("other change"), 0o600); err != nil {
					t.Fatal(err)
				}
				pullFixtureGit(t, work, "commit", "-am", "other change")
				pullFixtureGit(t, work, "switch", "main")
				if err := os.WriteFile(filepath.Join(work, "local.txt"), []byte("my changes"), 0o600); err != nil {
					t.Fatal(err)
				}
				pullFixtureGit(t, work, "commit", "-am", "my changes")
				result, err := git.Default.Run(t.Context(), git.Request{Dir: work, Args: []string{"merge", "conflict"}})
				if err == nil || result.ExitCode != 1 {
					t.Fatalf("expected merge conflict, got %v: %s", err, result.Stderr)
				}
			},
		},
		{name: "clean"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "isolated.gitconfig"))
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("GIT_AUTHOR_NAME", "Fixture")
			t.Setenv("GIT_AUTHOR_EMAIL", "fixture@example.invalid")
			t.Setenv("GIT_COMMITTER_NAME", "Fixture")
			t.Setenv("GIT_COMMITTER_EMAIL", "fixture@example.invalid")
			bare := filepath.Join(root, "origin.git")
			pullFixtureGit(t, root, "init", "--bare", "--initial-branch=main", bare)
			writer := filepath.Join(root, "writer")
			pullFixtureGit(t, root, "clone", bare, writer)
			if err := os.WriteFile(filepath.Join(writer, "local.txt"), []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			pullFixtureGit(t, writer, "add", "local.txt")
			pullFixtureGit(t, writer, "commit", "-m", "original")
			pullFixtureGit(t, writer, "push", "origin", "main")

			a, events := refreshTestApp(t, 1)
			t.Cleanup(a.stopRefresh)
			a.refreshHooks.pullCheck = nil
			work := filepath.Join(root, "work")
			pullFixtureGit(t, root, "clone", bare, work)
			src := a.cfg.Sources["source-a"]
			src.Repos["org/repo-0"] = config.Repo{CloneFolder: work}
			pullFixtureGit(t, writer, "commit", "--allow-empty", "-m", "advance origin")
			pullFixtureGit(t, writer, "push", "origin", "main")
			pullFixtureGit(t, work, "fetch", "origin")
			if tc.prepare != nil {
				tc.prepare(t, work)
			}
			head := pullFixtureGit(t, work, "rev-parse", "HEAD")
			a.PullRepo("source-a", "org/repo-0")
			a.refreshWG.Wait()
			done := events.named("pull:done")
			if len(done) != 1 {
				t.Fatalf("pull:done = %v, want one completion", done)
			}
			payload := done[0].args[0].(map[string]any)
			if payload["error"] != nil {
				t.Fatalf("pull failed: %v", payload)
			}
			gotHead := pullFixtureGit(t, work, "rev-parse", "HEAD")
			if tc.skip == "" {
				if want := pullFixtureGit(t, writer, "rev-parse", "HEAD"); gotHead != want || payload["skipped"] != nil {
					t.Fatalf("git HEAD = %s, want %s; pull:done = %v", gotHead, want, payload)
				}
				return
			}
			if gotHead != head || payload["skipped"] != tc.skip || payload["state"] != tc.state.String() {
				t.Fatalf("git HEAD changed: %t; pull:done = %v, want skipped = %q, state = %q", gotHead != head, payload, tc.skip, tc.state)
			}
			if tc.payloadPath != "" {
				if got, err := os.ReadFile(filepath.Join(work, tc.payloadPath)); err != nil || string(got) != "my changes" {
					t.Fatalf("local file = %q, %v; want original work", got, err)
				}
			}
		})
	}
}

func TestPullRepo_SkipsWhenSettingsChangeDuringPreflight(t *testing.T) {
	a, events := refreshTestApp(t, 1)
	t.Cleanup(a.stopRefresh)
	a.refreshHooks.pullCheck = func(context.Context, string) (git.PullSkipReason, git.RepoStatus, error) {
		a.mu.Lock()
		delete(a.cfg.Sources["source-a"].Repos, "org/repo-0")
		a.mu.Unlock()
		return "", git.RepoStatus{}, nil
	}
	a.refreshHooks.pull = func(context.Context, string) error {
		t.Error("pull must not run after repository settings change")
		return nil
	}
	a.PullRepo("source-a", "org/repo-0")
	a.refreshWG.Wait()
	done := events.named("pull:done")
	if len(done) != 1 || done[0].args[0].(map[string]any)["skipped"] != pullSettingsChanged {
		t.Fatalf("pull:done = %v, want settingsChanged skip", done)
	}
}
