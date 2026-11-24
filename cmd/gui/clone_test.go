package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/git/testutil"
)

func TestFailedCloneError_PreservesDestination(t *testing.T) {
	for _, kind := range []string{"repo", "directory", "symlink", "missing"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "clone")
			if kind == "missing" {
				cloneErr := errors.New("git clone failed")
				if err := failedCloneError(dest, cloneErr); err != cloneErr {
					t.Fatalf("unexpected destination report: %v", err)
				}
				if _, err := os.Lstat(dest); !os.IsNotExist(err) {
					t.Fatalf("created a destination: %v", err)
				}
				return
			}
			realDest := dest
			if kind == "symlink" {
				realDest = filepath.Join(root, "other-repo")
			}
			if err := os.Mkdir(realDest, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			gitCmd := func(args ...string) {
				t.Helper()
				cmd := exec.Command(git.GitBin(), args...)
				cmd.Dir = realDest
				cmd.Env = git.Environ()
				git.HideWindow(cmd)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			var before []byte
			configPath := filepath.Join(realDest, ".git", "config")
			if kind != "directory" {
				gitCmd("init", "-q")
				gitCmd("remote", "add", "origin", "https://example.com/other/repo.git")
				var err error
				before, err = os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
			}
			if kind == "symlink" {
				if err := os.Symlink(realDest, dest); err != nil {
					if runtime.GOOS == "windows" {
						t.Skipf("symlink unavailable: %v", err)
					}
					t.Fatal(err)
				}
			}
			marker := filepath.Join(realDest, "keep.txt")
			if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			cloneErr := errors.New("git clone failed")
			err := failedCloneError(dest, cloneErr)
			if !errors.Is(err, cloneErr) || !strings.Contains(err.Error(), "clone destination remains at "+dest) {
				t.Fatalf("unexpected clone error: %v", err)
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
				t.Fatalf("destination contents changed: %q, %v", data, err)
			}
			if before != nil {
				if after, err := os.ReadFile(configPath); err != nil || !bytes.Equal(before, after) {
					t.Fatalf("repository config changed: %v", err)
				}
			}
			if kind == "symlink" {
				if target, err := os.Readlink(dest); err != nil || target != realDest {
					t.Fatalf("symlink changed: %q, %v", target, err)
				}
			}
		})
	}
}

func TestCloneCompletionPublishesDirtyWorkingTree(t *testing.T) {
	isolateSchedule(t)
	a, _ := newWorkspaceTestApp(t)
	defer a.stopRefresh()
	a.refreshHooks = &refreshHooks{dial: func(context.Context, string) error { return nil }}
	events := &recordingSink{}
	a.events = events

	const source, repo = "github-alice", "team/frontend"
	origin := t.TempDir()
	pullFixtureGit(t, origin, "init", "-b", "main")
	pullFixtureGit(t, origin, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "initial")
	templates := t.TempDir()
	if err := os.Mkdir(filepath.Join(templates, "hooks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templates, "hooks", "post-checkout"),
		[]byte("#!/bin/sh\nprintf 'created by checkout\\n' > checkout-extra.txt\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	realGit := git.Default
	git.SetRunnerForTest(t, testutil.RunnerFunc(func(ctx context.Context, req git.Request) (git.Result, error) {
		if req.Args[0] == "clone" {
			// Exercise real Git and a real post-checkout hook, with the
			// origin and hook template isolated from the user's config.
			req.Args = []string{"clone", "--template=" + templates, origin, req.Args[len(req.Args)-1]}
		}
		return realGit.Run(ctx, req)
	}))

	a.CloneRepo(source, repo, "")
	a.refreshWG.Wait()
	done, checked := events.named("clone:done"), events.named("status:one")
	if len(done) != 1 || len(checked) != 1 {
		t.Fatalf("clone completions = %v; status publishes = %v", done, checked)
	}
	if transfer := done[0].args[0].(cloneDoneDTO); transfer.Error != "" {
		t.Fatalf("clone failed: %+v", transfer)
	}
	if done[0].meta.OperationID != checked[0].meta.OperationID || done[0].meta.Sequence >= checked[0].meta.Sequence {
		t.Fatalf("completion and check are not one ordered operation: %+v, %+v", done[0].meta, checked[0].meta)
	}
	if row := checked[0].args[0].(StatusResult); row.State != "dirty" || row.Untracked != 1 {
		t.Fatalf("post-clone status = %+v, want dirty with one untracked file", row)
	}
	snapshot, err := a.GetStartupSnapshot()
	if err != nil || snapshot.EventWatermark != checked[0].meta.Sequence || snapshot.Config.Revision != checked[0].meta.ConfigRevision {
		t.Fatalf("snapshot watermark = %d, revision = %d, error = %v", snapshot.EventWatermark, snapshot.Config.Revision, err)
	}
}
