package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// networkState returns the payload of the last network:state event.
func networkState(t *testing.T, events *refreshTestEvents) (offline bool, hosts []string, count int) {
	t.Helper()
	all := events.named("network:state")
	if len(all) == 0 {
		return false, nil, 0
	}
	payload := all[len(all)-1].args[0].(map[string]any)
	return payload["offline"].(bool), payload["hosts"].([]string), len(all)
}

func TestServerAddr(t *testing.T) {
	tests := map[string]string{
		"https://github.com":             "github.com:443",
		"https://git.example.org:8443/x": "git.example.org:8443",
		"http://gitea.lan":               "gitea.lan:80",
		"http://gitea.lan:3000":          "gitea.lan:3000",
		"https://[::1]:3000":             "[::1]:3000",
		"":                               "",
		"not a url":                      "",
	}
	direct := func(*http.Request) (*url.URL, error) { return nil, nil }
	for in, want := range tests {
		if got := serverAddrVia(config.Account{URL: in}, direct); got != want {
			t.Errorf("serverAddr(%q) = %q, want %q", in, got, want)
		}
	}

	// Behind a proxy, the proxy is what must answer.
	proxy := func(r *http.Request) (*url.URL, error) {
		if r.URL.Hostname() == "gitea.lan" {
			return nil, nil // not proxied
		}
		return url.Parse("http://proxy.corp:3128")
	}
	if got := serverAddrVia(config.Account{URL: "https://github.com"}, proxy); got != "proxy.corp:3128" {
		t.Errorf("proxied account = %q, want the proxy", got)
	}
	if got := serverAddrVia(config.Account{URL: "http://gitea.lan:3000"}, proxy); got != "gitea.lan:3000" {
		t.Errorf("account outside the proxy = %q, want its own server", got)
	}
}

// With no network, a cycle over many clones must stop at once: one connect
// decides, no fetch and no credential check goes out, every clone reports
// back as skipped, and the window is told which server is down.
func TestFetchAllStopsWhenTheServerIsUnreachable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const clones = 36
		a, events := refreshTestApp(t, clones)
		defer a.stopRefresh()
		var dials, fetches, checks atomic.Int32
		a.refreshHooks.dial = func(context.Context, string) error {
			dials.Add(1)
			return errors.New("network is unreachable")
		}
		a.refreshHooks.fetch = func(context.Context, string) (string, error) {
			fetches.Add(1)
			return "", nil
		}
		a.refreshHooks.credential = func(context.Context, config.Account, string, *config.Config) credential.StatusResult {
			checks.Add(1)
			return refreshCredentialOK()
		}

		started := time.Now()
		err := a.FetchAllRepos()
		if err == nil {
			t.Fatal("a cycle that reached no server must report a failure")
		}
		if spent := time.Since(started); spent > time.Second {
			t.Errorf("the cycle took %v; it must not wait on anything", spent)
		}
		if dials.Load() != 1 || fetches.Load() != 0 || checks.Load() != 0 {
			t.Errorf("dials %d, fetches %d, credential checks %d; want 1, 0, 0", dials.Load(), fetches.Load(), checks.Load())
		}
		done := events.named("fetch:done")
		if len(done) != clones || len(events.named("fetch:start")) != 0 {
			t.Fatalf("%d fetch:done and %d fetch:start events, want %d and 0", len(done), len(events.named("fetch:start")), clones)
		}
		for _, event := range done {
			payload := event.args[0].(map[string]any)
			if payload["skipped"] != true || payload["code"] != "git.offline" || payload["host"] != "example.invalid" {
				t.Fatalf("skipped clone payload = %v", payload)
			}
		}
		offline, hosts, count := networkState(t, events)
		if !offline || len(hosts) != 1 || hosts[0] != "example.invalid" || count != 1 {
			t.Errorf("network:state = offline %v, hosts %v, sent %d times; want one event for example.invalid", offline, hosts, count)
		}
		creds := events.named("credentials:updated")
		if len(creds) != 1 || creds[0].args[0].(map[string]CredentialStatus)["account-a"].Status != "offline" {
			t.Errorf("the account must read offline without a check: %v", creds)
		}
		if len(events.named("fetch:alldone")) != 1 {
			t.Errorf("the cycle must end: %d fetch:alldone events", len(events.named("fetch:alldone")))
		}
	})
}

// While a server is down the watcher retries calmly. When it answers, the
// window is told and the fetch that the outage stopped runs by itself.
func TestNetworkWatcherResumesWhenTheServerIsBack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, events := refreshTestApp(t, 3)
		defer a.stopRefresh()
		var up atomic.Bool
		var dials, fetches atomic.Int32
		a.refreshHooks.dial = func(context.Context, string) error {
			dials.Add(1)
			if up.Load() {
				return nil
			}
			return errors.New("network is unreachable")
		}
		a.refreshHooks.fetch = func(context.Context, string) (string, error) {
			fetches.Add(1)
			return "", nil
		}
		if err := a.FetchAllRepos(); err == nil {
			t.Fatal("the first cycle must fail")
		}
		synctest.Wait()
		before := dials.Load()

		// Still down: the watcher connects once per interval and nothing else.
		time.Sleep(3*networkWatchInterval + time.Second)
		synctest.Wait()
		if got := dials.Load() - before; got != 3 || fetches.Load() != 0 {
			t.Errorf("while down: %d watcher connects and %d fetches in three intervals, want 3 and 0", got, fetches.Load())
		}
		if offline, _, count := networkState(t, events); !offline || count != 1 {
			t.Errorf("while down the window must hear nothing new: offline %v, %d events", offline, count)
		}

		up.Store(true)
		time.Sleep(networkWatchInterval)
		synctest.Wait()
		if offline, hosts, _ := networkState(t, events); offline || len(hosts) != 0 {
			t.Errorf("after recovery: offline %v, hosts %v; want the state cleared", offline, hosts)
		}
		if fetches.Load() != 3 {
			t.Errorf("after recovery %d clones were fetched, want all 3 without anyone asking", fetches.Load())
		}

		// Up again: the watcher has stopped.
		settled := dials.Load()
		time.Sleep(4 * networkWatchInterval)
		synctest.Wait()
		if dials.Load() != settled {
			t.Errorf("the watcher kept connecting after the server came back")
		}

		// A second outage starts exactly one watcher again.
		up.Store(false)
		if err := a.FetchAllRepos(); err == nil {
			t.Fatal("the cycle during the second outage must fail")
		}
		synctest.Wait()
		again := dials.Load()
		time.Sleep(2*networkWatchInterval + time.Second)
		synctest.Wait()
		if got := dials.Load() - again; got != 2 {
			t.Errorf("second outage: %d watcher connects in two intervals, want 2 (one watcher)", got)
		}
	})
}

// Retry is a new cycle: it tests the network afresh instead of trusting what
// it knew, and clears the state when the server answers.
func TestFetchAllRetriesTheNetwork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, events := refreshTestApp(t, 2)
		defer a.stopRefresh()
		var up atomic.Bool
		var fetches atomic.Int32
		a.refreshHooks.dial = func(context.Context, string) error {
			if up.Load() {
				return nil
			}
			return errors.New("network is unreachable")
		}
		a.refreshHooks.fetch = func(context.Context, string) (string, error) {
			fetches.Add(1)
			return "", nil
		}
		if err := a.FetchAllRepos(); err == nil {
			t.Fatal("the first cycle must fail")
		}
		up.Store(true)
		if err := a.FetchAllRepos(); err != nil {
			t.Fatalf("the retry must succeed: %v", err)
		}
		if offline, _, count := networkState(t, events); offline || count != 2 || fetches.Load() != 2 {
			t.Errorf("after the retry: offline %v, %d state events, %d fetches; want cleared, 2, 2", offline, count, fetches.Load())
		}
	})
}

// A server that accepts the connect but then fails every fetch as offline
// costs one round of fetches, not one per clone.
func TestFetchAllSkipsTheQueueOfALostServer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const clones = 40
		a, events := refreshTestApp(t, clones)
		defer a.stopRefresh()
		var fetches atomic.Int32
		a.refreshHooks.fetch = func(context.Context, string) (string, error) {
			fetches.Add(1)
			time.Sleep(time.Second)
			return "", &git.GitError{Op: "fetch", Code: git.CodeOffline}
		}
		if err := a.FetchAllRepos(); err == nil {
			t.Fatal("the cycle must fail")
		}
		if got := fetches.Load(); got < 1 || got > fetchAllConcurrency {
			t.Errorf("%d fetches went out, want at most the %d that were already running", got, fetchAllConcurrency)
		}
		skipped := 0
		for _, event := range events.named("fetch:done") {
			if event.args[0].(map[string]any)["skipped"] == true {
				skipped++
			}
		}
		if len(events.named("fetch:done")) != clones || skipped != clones-int(fetches.Load()) {
			t.Errorf("%d fetch:done events, %d skipped, %d attempted; every clone must report back once", len(events.named("fetch:done")), skipped, fetches.Load())
		}
	})
}

// A failure that is not about the connection stops nothing.
func TestFetchAllKeepsGoingOnOtherFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _ := refreshTestApp(t, 12)
		defer a.stopRefresh()
		var fetches atomic.Int32
		a.refreshHooks.fetch = func(context.Context, string) (string, error) {
			fetches.Add(1)
			return "", &git.GitError{Op: "fetch", Code: git.CodeAuthFailed}
		}
		_ = a.FetchAllRepos()
		if fetches.Load() != 12 {
			t.Errorf("%d of 12 clones were fetched; an authentication failure must not stop the queue", fetches.Load())
		}
	})
}

// Callers that ask about one server at the same time share one connect, and
// an answer is reused instead of connecting per clone.
func TestReachableSharesOneConnect(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _ := refreshTestApp(t, 1)
		defer a.stopRefresh()
		var dials atomic.Int32
		a.refreshHooks.dial = func(context.Context, string) error {
			dials.Add(1)
			time.Sleep(100 * time.Millisecond)
			return nil
		}
		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() {
				if !a.reachable(t.Context(), "example.invalid:443") {
					t.Error("the server must read reachable")
				}
			})
		}
		wg.Wait()
		if !a.reachable(t.Context(), "example.invalid:443") || dials.Load() != 1 {
			t.Errorf("%d connects for 21 questions about one server, want 1", dials.Load())
		}
		time.Sleep(networkFresh + time.Second)
		a.reachable(t.Context(), "example.invalid:443")
		if dials.Load() != 2 {
			t.Errorf("an old answer must be checked again: %d connects, want 2", dials.Load())
		}
		if !a.reachable(t.Context(), "") {
			t.Error("an account without a readable URL is never held back")
		}
	})
}

// The network can die while work is in flight. The monitor notices within
// its interval, the fetches that were running are cancelled, nothing else
// starts, and every clone reports back: the run ends in seconds, not after
// one timeout per clone.
func TestFetchAllStopsWhenTheNetworkDiesMidway(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const clones = 50
		a, events := refreshTestApp(t, clones)
		defer a.stopRefresh()
		var up atomic.Bool
		up.Store(true)
		var fetches, cancelled atomic.Int32
		a.refreshHooks.dial = func(context.Context, string) error {
			if up.Load() {
				return nil
			}
			return errors.New("network is unreachable")
		}
		// Every fetch hangs, as git does on a dead connection, until it is
		// cancelled.
		a.refreshHooks.fetch = func(ctx context.Context, _ string) (string, error) {
			if fetches.Add(1) == 1 {
				up.Store(false) // the network dies as the first fetch starts
			}
			<-ctx.Done()
			cancelled.Add(1)
			return "", ctx.Err()
		}

		started := time.Now()
		if err := a.FetchAllRepos(); err == nil {
			t.Fatal("the cycle must fail")
		}
		if spent := time.Since(started); spent > networkMonitorInterval+time.Second {
			t.Errorf("the cycle took %v; it must stop within one monitor interval", spent)
		}
		if got := fetches.Load(); got != fetchAllConcurrency || cancelled.Load() != got {
			t.Errorf("%d fetches started and %d were cancelled; want the %d in flight, all cancelled", got, cancelled.Load(), fetchAllConcurrency)
		}
		done := events.named("fetch:done")
		if len(done) != clones {
			t.Fatalf("%d of %d clones reported back", len(done), clones)
		}
		for _, event := range done {
			if payload := event.args[0].(map[string]any); payload["skipped"] != true || payload["code"] != "git.offline" {
				t.Fatalf("a clone stopped by the outage must read unreachable: %v", payload)
			}
		}
		if offline, hosts, _ := networkState(t, events); !offline || len(hosts) != 1 {
			t.Errorf("the window must be told: offline %v, hosts %v", offline, hosts)
		}
	})
}

// Pull is network work like any other: it does not start without a server,
// and one in flight stops when the server goes away.
func TestPullStopsWithoutNetwork(t *testing.T) {
	pullDone := func(t *testing.T, events *refreshTestEvents) map[string]any {
		t.Helper()
		synctest.Wait()
		done := events.named("pull:done")
		if len(done) != 1 {
			t.Fatalf("%d pull:done events, want 1", len(done))
		}
		return done[0].args[0].(map[string]any)
	}

	t.Run("not started", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, events := refreshTestApp(t, 1)
			defer a.stopRefresh()
			var pulls atomic.Int32
			a.refreshHooks.dial = func(context.Context, string) error { return errors.New("network is unreachable") }
			a.refreshHooks.pull = func(context.Context, string) error { pulls.Add(1); return nil }

			a.PullRepo("source-a", "org/repo-0")
			payload := pullDone(t, events)
			if pulls.Load() != 0 || payload["skipped"] != true || payload["code"] != "git.offline" || payload["host"] != "example.invalid" {
				t.Errorf("%d pulls ran; payload %v", pulls.Load(), payload)
			}
			if offline, _, _ := networkState(t, events); !offline {
				t.Error("the window must be told that the server is down")
			}
		})
	})

	t.Run("stopped in flight", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, events := refreshTestApp(t, 1)
			defer a.stopRefresh()
			var up atomic.Bool
			up.Store(true)
			a.refreshHooks.dial = func(context.Context, string) error {
				if up.Load() {
					return nil
				}
				return errors.New("network is unreachable")
			}
			a.refreshHooks.pull = func(ctx context.Context, _ string) error {
				up.Store(false)
				<-ctx.Done()
				return ctx.Err()
			}
			a.PullRepo("source-a", "org/repo-0")
			synctest.Wait()
			if len(events.named("pull:done")) != 0 {
				t.Fatal("the pull must still be in flight")
			}
			// One monitor interval later the outage has been noticed.
			time.Sleep(networkMonitorInterval + 100*time.Millisecond)
			payload := pullDone(t, events)
			if payload["skipped"] != true || payload["code"] != "git.offline" {
				t.Errorf("payload %v, want the pull reported as unreachable", payload)
			}
		})
	})

	t.Run("with network", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, events := refreshTestApp(t, 1)
			defer a.stopRefresh()
			var pulls atomic.Int32
			a.refreshHooks.pull = func(context.Context, string) error { pulls.Add(1); return nil }
			a.PullRepo("source-a", "org/repo-0")
			if payload := pullDone(t, events); pulls.Load() != 1 || payload["error"] != nil {
				t.Errorf("%d pulls; payload %v", pulls.Load(), payload)
			}
		})
	})
}
