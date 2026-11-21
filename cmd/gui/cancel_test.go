package main

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
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
		if got := fetches.Load(); got != fetchAllConcurrency {
			t.Fatalf("%d fetches in flight, want the %d slots", got, fetchAllConcurrency)
		}

		started := time.Now()
		a.stopRefresh()
		if spent := time.Since(started); spent >= shutdownWait {
			t.Errorf("closing took %v although every fetch stopped when asked", spent)
		}
		if err := <-done; err == nil {
			t.Error("the interrupted cycle must report a failure")
		}
		if fetches.Load() != fetchAllConcurrency || stopped.Load() != fetchAllConcurrency {
			t.Errorf("%d fetches started and %d stopped; the %d clones waiting for a turn must not start",
				fetches.Load(), stopped.Load(), clones-fetchAllConcurrency)
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
		if len(done) != 1 || done[0].args[0].(map[string]any)["error"] == nil {
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
		if len(done) != 1 || done[0].args[0].(map[string]any)["error"] == nil {
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
