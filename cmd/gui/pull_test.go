package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

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
		name            string
		skip            git.PullSkipReason
		state           status.State
		prepare         func(*testing.T, string)
		payloadPath     string
		uncached        bool
		writeDuringPull bool
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
		{name: "clean with new upstream commits", uncached: true},
		{name: "competing write with autostash configured", skip: git.PullDirty, state: status.Dirty,
			payloadPath: "local.txt", writeDuringPull: true},
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
			if err := os.WriteFile(filepath.Join(writer, "local.txt"), []byte("upstream changes"), 0o600); err != nil {
				t.Fatal(err)
			}
			pullFixtureGit(t, writer, "commit", "-am", "advance origin")
			pullFixtureGit(t, writer, "push", "origin", "main")
			if !tc.uncached {
				pullFixtureGit(t, work, "fetch", "origin")
			}
			if tc.writeDuringPull {
				pullFixtureGit(t, work, "config", "pull.autostash", "true")
				a.refreshHooks.pull = func(ctx context.Context, path string) error {
					if err := os.WriteFile(filepath.Join(path, "local.txt"), []byte("my changes"), 0o600); err != nil {
						return err
					}
					return git.PullQuietContext(ctx, path)
				}
			}
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
			payload := done[0].args[0].(pullDoneDTO)
			if payload.Error != "" {
				t.Fatalf("pull failed: %v", payload)
			}
			gotHead := pullFixtureGit(t, work, "rev-parse", "HEAD")
			if tc.skip == "" {
				if want := pullFixtureGit(t, writer, "rev-parse", "HEAD"); gotHead != want || !payload.Skipped.IsZero() {
					t.Fatalf("git HEAD = %s, want %s; pull:done = %v", gotHead, want, payload)
				}
				return
			}
			if gotHead != head || payload.Skipped.v != tc.skip || payload.State != tc.state.String() {
				t.Fatalf("git HEAD changed: %t; pull:done = %v, want skipped = %q, state = %q", gotHead != head, payload, tc.skip, tc.state)
			}
			if tc.writeDuringPull && pullFixtureGit(t, work, "stash", "list") != "" {
				t.Fatal("pull stashed local work")
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
	if len(done) != 1 || done[0].args[0].(pullDoneDTO).Skipped.v != pullSettingsChanged {
		t.Fatalf("pull:done = %v, want settingsChanged skip", done)
	}
}

func TestPullAll_UpdatesOnlyEligibleClonesThenDownloadsMissing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "isolated.gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Fixture")
	t.Setenv("GIT_AUTHOR_EMAIL", "fixture@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Fixture")
	t.Setenv("GIT_COMMITTER_EMAIL", "fixture@example.invalid")
	up := filepath.Join(root, "up")
	bare := filepath.Join(up, "org", "download.git")
	pullFixtureGit(t, root, "init", "--bare", "--initial-branch=main", bare)
	writer := filepath.Join(root, "writer")
	pullFixtureGit(t, root, "clone", bare, writer)
	pullFixtureGit(t, writer, "commit", "--allow-empty", "-m", "first")
	pullFixtureGit(t, writer, "push", "origin", "main")
	a, events := refreshTestApp(t, 10)
	t.Cleanup(a.stopRefresh)
	a.refreshHooks.pullCheck = nil
	upPath := filepath.ToSlash(up)
	if !strings.HasPrefix(upPath, "/") {
		upPath = "/" + upPath
	}
	account := a.cfg.Accounts["account-a"]
	account.URL = (&url.URL{Scheme: "file", Path: upPath}).String()
	a.cfg.Accounts["account-a"] = account
	src := a.cfg.Sources["source-a"]
	paths := make(map[string]string)
	beforeHeads := make(map[string]string)
	for i := range 10 {
		name := fmt.Sprintf("org/repo-%d", i)
		path := filepath.Join(root, fmt.Sprintf("clone-%d", i))
		if i == 3 {
			pullFixtureGit(t, paths["org/repo-0"], "worktree", "add", "-b", "linked", path)
			pullFixtureGit(t, path, "branch", "--set-upstream-to=origin/main")
		} else {
			pullFixtureGit(t, root, "clone", bare, path)
		}
		src.Repos[name] = config.Repo{CloneFolder: path}
		paths[name] = path
		switch i {
		case 5, 6:
			pullFixtureGit(t, path, "commit", "--allow-empty", "-m", "local commit")
		case 7:
			pullFixtureGit(t, path, "branch", "--unset-upstream")
		}
		beforeHeads[name] = pullFixtureGit(t, path, "rev-parse", "HEAD")
		if i < 3 {
			if err := os.WriteFile(filepath.Join(path, "untracked.txt"), []byte("local work"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	pullFixtureGit(t, writer, "commit", "--allow-empty", "-m", "second")
	pullFixtureGit(t, writer, "push", "origin", "main")
	after := pullFixtureGit(t, writer, "rev-parse", "HEAD")
	for i := range 5 {
		pullFixtureGit(t, paths[fmt.Sprintf("org/repo-%d", i)], "fetch", "origin")
	}
	pullFixtureGit(t, paths["org/repo-6"], "fetch", "origin")
	const missing = "org/download"
	download := filepath.Join(root, "download")
	src.Repos[missing] = config.Repo{CloneFolder: download, CredentialType: "gcm"}
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	src.Repos["org/blocked"] = config.Repo{CloneFolder: blocked}
	src.RepoOrder = append(src.RepoOrder, missing, "org/blocked")
	a.cfg.Sources["source-a"] = src

	a.PullAll("fixture")
	a.refreshWG.Wait()
	results := events.named("pull:done")
	if len(results) != 10 {
		t.Fatalf("got %d pull results, want 10", len(results))
	}
	seen := make(map[string]bool)
	var updated, skipped, unchanged int
	for _, event := range results {
		result := event.args[0].(pullDoneDTO)
		repo := result.Repo
		if seen[repo] || paths[repo] == "" || result.Run != "fixture" || result.Error != "" {
			t.Fatalf("unexpected result: %v", result)
		}
		seen[repo] = true
		wantHead := beforeHeads[repo]
		switch repo {
		case "org/repo-0", "org/repo-1", "org/repo-2":
			if result.Skipped.v != git.PullDirty || result.BehindBefore != 1 {
				t.Errorf("%s: want dirty skip and behind count: %v", repo, result)
			}
			skipped++
		case "org/repo-3", "org/repo-4":
			if result.Skipped.v != nil || result.Unchanged || result.BehindBefore != 1 {
				t.Errorf("%s: want eligible pull: %v", repo, result)
			}
			wantHead = after
			updated++
		default:
			wantState, wantBehind := status.Clean, 0
			switch repo {
			case "org/repo-5":
				wantState = status.Ahead
			case "org/repo-6":
				wantState, wantBehind = status.Diverged, 1
			case "org/repo-7":
				wantState = status.NoUpstream
			}
			if !result.Unchanged || result.State != wantState.String() || result.BehindBefore != wantBehind {
				t.Errorf("%s: want no-op with status %s and behind=%d: %v", repo, wantState, wantBehind, result)
			}
			unchanged++
		}
		if head := pullFixtureGit(t, paths[repo], "rev-parse", "HEAD"); head != wantHead {
			t.Errorf("%s: HEAD = %s, want %s", repo, head, wantHead)
		}
	}
	if updated != 2 || skipped != 3 || unchanged != 5 {
		t.Fatalf("%d updates, %d skips, %d unchanged", updated, skipped, unchanged)
	}
	cloned := events.named("clone:done")
	if len(cloned) != 1 || cloned[0].args[0].(cloneDoneDTO).Error != "" ||
		cloned[0].args[0].(cloneDoneDTO).Run != "fixture" {
		t.Fatalf("download result = %v, want one successful clone", cloned)
	}
	if head := pullFixtureGit(t, download, "rev-parse", "HEAD"); head != after {
		t.Errorf("download HEAD = %s, want %s", head, after)
	}
	checks := events.named("status:one")
	if len(checks) != 3 {
		t.Fatalf("status:one count = %d, want exactly two pulls and one clone", len(checks))
	}
	success := map[string]recordedEvent{missing: cloned[0]}
	for _, e := range results {
		r := e.args[0].(pullDoneDTO)
		if r.Error == "" && r.Skipped.IsZero() && !r.Unchanged {
			success[r.Repo] = e
		}
	}
	for _, check := range checks {
		row := check.args[0].(StatusResult)
		done := success[row.Repo]
		if row.State != "clean" || done.meta.OperationID != check.meta.OperationID ||
			done.meta.Sequence >= check.meta.Sequence {
			t.Errorf("status:one for %s is not the authoritative completion: %+v", row.Repo, check)
		}
		delete(success, row.Repo)
	}
	started, completed := events.named("pull:allstart"), events.named("pull:alldone")
	if len(started) != 1 || len(completed) != 1 || started[0].meta.OperationID != completed[0].meta.OperationID ||
		checks[len(checks)-1].meta.Sequence >= completed[0].meta.Sequence {
		t.Fatal("expected one bulk operation with a terminal event after the row checks")
	}
	var pullsBeforeDownloads int
	events.mu.Lock()
	defer events.mu.Unlock()
	for _, event := range events.events {
		switch event.name {
		case "pull:done":
			pullsBeforeDownloads++
		case "clone:start":
			if pullsBeforeDownloads != 10 {
				t.Errorf("download started after %d pulls, want 10", pullsBeforeDownloads)
			}
		}
	}
}

func TestPullAll_SerializesTransferWithFetchAll(t *testing.T) {
	for _, first := range []string{"pull", "fetch"} {
		t.Run(first, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, events := refreshTestApp(t, 1)
				defer a.stopRefresh()
				release := make(chan struct{})
				var pullEntered, fetchEntered bool
				a.refreshHooks.pull = func(context.Context, string) error {
					pullEntered = true
					if first == "pull" {
						<-release
					}
					return nil
				}
				a.refreshHooks.fetch = func(context.Context, string) (string, error) {
					fetchEntered = true
					if first == "fetch" {
						<-release
					}
					return "", nil
				}
				startFetch := func() { go func() { _ = a.FetchAllRepos() }() }
				if first == "pull" {
					a.PullAll("serialized")
					synctest.Wait()
					startFetch()
				} else {
					startFetch()
					synctest.Wait()
					a.PullAll("serialized")
				}
				synctest.Wait()
				earlyPull, earlyFetch := pullEntered, fetchEntered
				close(release)
				synctest.Wait()
				if earlyPull != (first == "pull") || earlyFetch != (first == "fetch") {
					t.Errorf("while %s transfers: pull started=%t fetch started=%t", first, earlyPull, earlyFetch)
				}
				if !pullEntered || !fetchEntered || len(events.named("pull:done")) != 1 || len(events.named("fetch:done")) != 1 {
					t.Fatalf("pull and fetch did not both finish after releasing the path")
				}
			})
		})
	}
}

func TestPathGate_CancelQueuedOperation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var gate pathGate
		path := "fixture"
		unlock := gate.lock(path)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		queued := make(chan error, 1)
		go func() {
			release, err := gate.acquire(ctx, path)
			if release != nil {
				release()
			}
			queued <- err
		}()
		if err := <-queued; err != context.DeadlineExceeded {
			t.Errorf("got %v, want deadline exceeded", err)
		}
		unlock()
		if len(gate.entries) != 0 {
			t.Error("path gate retained the cancelled waiter")
		}
	})
}
