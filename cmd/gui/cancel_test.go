package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/git/testutil"
	"github.com/LuisPalacios/gitbox/pkg/rpc"
)

// Closing the app while a fetch cycle runs stops the fetches in flight and
// starts none of the clones still waiting for a turn.
func TestClosingStopsTheFetchQueue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const clones = 30
		a, _ := refreshTestApp(t, clones)
		var fetches, stopped atomic.Int32
		a.refreshHooks.fetch = func(ctx context.Context, _ string) (string, error) {
			fetches.Add(1)
			<-ctx.Done()
			stopped.Add(1)
			return "", ctx.Err()
		}
		done := make(chan error, 1)
		go func() { done <- a.FetchAllRepos() }()
		synctest.Wait()
		if got := fetches.Load(); got != fetchPerHostConcurrency {
			t.Fatalf("%d fetches in flight, want the %d slots", got, fetchPerHostConcurrency)
		}

		started := time.Now()
		a.stopRefresh()
		if spent := time.Since(started); spent >= shutdownWait {
			t.Errorf("closing took %v although every fetch stopped when asked", spent)
		}
		if err := <-done; err == nil {
			t.Error("the interrupted cycle must report a failure")
		}
		if fetches.Load() != fetchPerHostConcurrency || stopped.Load() != fetchPerHostConcurrency {
			t.Errorf("%d fetches started and %d stopped; the %d clones waiting for a turn must not start",
				fetches.Load(), stopped.Load(), clones-fetchPerHostConcurrency)
		}
	})
}

// A fetch that neither finishes nor fails is cut at its bound.
func TestFetchIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, events := refreshTestApp(t, 1)
		defer a.stopRefresh()
		a.refreshHooks.fetch = func(ctx context.Context, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}
		started := time.Now()
		if err := a.FetchAllRepos(); err == nil {
			t.Fatal("a fetch cut at its bound must fail the cycle")
		}
		if spent := time.Since(started); spent < fetchTimeout || spent > fetchTimeout+10*time.Second {
			t.Errorf("the cycle took %v, want the fetch cut at %v", spent, fetchTimeout)
		}
		done := events.named("fetch:done")
		if len(done) != 1 || done[0].args[0].(fetchDoneDTO).Error == "" {
			t.Errorf("fetch:done = %v, want one event with an error", done)
		}
	})
}

// A pull that neither finishes nor fails is cut at its bound.
func TestPullIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, events := refreshTestApp(t, 1)
		defer a.stopRefresh()
		a.refreshHooks.pull = func(ctx context.Context, _ string) error {
			<-ctx.Done()
			return ctx.Err()
		}
		a.PullRepo("source-a", "org/repo-0")
		time.Sleep(pullTimeout - time.Second)
		synctest.Wait()
		if len(events.named("pull:done")) != 0 {
			t.Fatal("the pull ended before its bound")
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		done := events.named("pull:done")
		if len(done) != 1 || done[0].args[0].(pullDoneDTO).Error == "" {
			t.Errorf("pull:done = %v, want one event with an error once the bound passed", done)
		}
	})
}

// Closing the app never waits more than shutdownWait, even for work that
// ignores the cancel.
func TestClosingDoesNotWaitForStuckWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _ := refreshTestApp(t, 1)
		release := make(chan struct{})
		a.refreshHooks.fetch = func(context.Context, string) (string, error) {
			<-release // ignores its context: stuck outside git
			return "", nil
		}
		done := make(chan error, 1)
		go func() { done <- a.FetchAllRepos() }()
		synctest.Wait()

		started := time.Now()
		a.stopRefresh()
		if spent := time.Since(started); spent != shutdownWait {
			t.Errorf("closing took %v, want exactly the %v cap", spent, shutdownWait)
		}

		close(release)
		<-done
		synctest.Wait()
	})
}

// Once the app began to close, no pull starts.
func TestNoPullStartsAfterClosing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, events := refreshTestApp(t, 1)
		var pulls atomic.Int32
		a.refreshHooks.pull = func(context.Context, string) error { pulls.Add(1); return nil }
		a.stopRefresh()
		a.PullRepo("source-a", "org/repo-0")
		synctest.Wait()
		if pulls.Load() != 0 || len(events.named("pull:done")) != 0 {
			t.Errorf("%d pulls ran and %d events were sent after closing", pulls.Load(), len(events.named("pull:done")))
		}
	})
}

// TestCancelReachesBackgroundWork: a clone a remote window started runs
// inside its request on the server (#295), so cancelling the request from
// the client ($/cancelRequest), or dropping the connection, stops git and
// the processes it started on the host. The clone's transport is a shell
// that records its pid and sleeps, a real child of git. Either way the
// window hears clone:done once: from the server after a cancel, from its
// own backend after a disconnect.
func TestCancelReachesBackgroundWork(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hanging transport is a POSIX shell")
	}
	for _, how := range []string{"cancel", "disconnect", "bulk cancel", "bulk disconnect"} {
		t.Run(how, func(t *testing.T) {
			bulk := strings.HasPrefix(how, "bulk ")
			cancelRequest := strings.HasSuffix(how, "cancel")
			method, event, args := "CloneRepo", "clone:done", []any{"acct", "org/fresh", ""}
			if bulk {
				method, event, args = "PullAll", "pull:alldone", []any{"cancelled-fleet"}
			}
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ln.Close() })
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("GITBOX_TOKEN_ACCT", "contract-test-token-value")
			side := repoHost(t, "http://"+ln.Addr().String())

			pidFile := filepath.Join(t.TempDir(), "pid")
			hang := "ext::sh -c echo% $$% >% " + pidFile + ";% exec% sleep% 60"
			real := git.Default
			git.SetRunnerForTest(t, testutil.RunnerFunc(func(ctx context.Context, req git.Request) (git.Result, error) {
				if len(req.Args) > 2 && req.Args[0] == "clone" {
					args := slices.Clone(req.Args)
					args[len(args)-2] = hang
					req.Args = append([]string{"-c", "protocol.ext.allow=always"}, args...)
				}
				return real.Run(ctx, req)
			}))

			link := serveRemote(t, side.path, nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if cancelRequest {
				go func() {
					_ = link.Remote.current().Call(ctx, method, args, nil, rpc.WithRequestID(newRequestID()))
				}()
			} else if bulk {
				link.Remote.PullAll("cancelled-fleet")
			} else {
				link.Remote.CloneRepo("acct", "org/fresh", "")
			}
			var pid int
			deadline := time.Now().Add(15 * time.Second)
			for pid == 0 && time.Now().Before(deadline) {
				if data, err := os.ReadFile(pidFile); err == nil {
					pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
				}
				time.Sleep(20 * time.Millisecond)
			}
			if pid == 0 {
				t.Fatal("the clone's transport never started on the host")
			}
			alive := func() bool {
				p, err := os.FindProcess(pid)
				return err == nil && p.Signal(syscall.Signal(0)) == nil
			}
			if !alive() {
				t.Fatal("the transport is not running: the test proves nothing")
			}

			if cancelRequest {
				cancel()
			} else {
				link.Remote.Close()
			}
			for deadline := time.Now().Add(10 * time.Second); alive() && time.Now().Before(deadline); {
				time.Sleep(20 * time.Millisecond)
			}
			if alive() {
				t.Fatalf("after the %s, git's child %d still runs on the host", how, pid)
			}
			// The window hears how the clone ended, from the server or, once
			// the connection is gone, from its own backend.
			for deadline := time.Now().Add(10 * time.Second); len(link.Events.named(event)) == 0 && time.Now().Before(deadline); {
				time.Sleep(20 * time.Millisecond)
			}
			done := link.Events.named(event)
			if len(done) != 1 {
				t.Fatalf("%s arrived %d times, want once", event, len(done))
			}
			if payload, _ := done[0].args[0].(map[string]any); payload["error"] == "" || payload["error"] == nil {
				t.Errorf("%s = %v, want the cancellation error", event, done[0].args)
			}
		})
	}
}
