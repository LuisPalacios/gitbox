package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
)

func probeTestConfig(baseURL string, count int) *config.Config {
	repos := make(map[string]config.Repo, count)
	for i := range count {
		repos[fmt.Sprintf("org/repo-%d", i)] = config.Repo{}
	}
	return &config.Config{
		Accounts: map[string]config.Account{
			"account-a": {Provider: "github", URL: baseURL, Username: "user", DefaultCredentialType: "token"},
		},
		Sources: map[string]config.Source{
			"source-a": {Account: "account-a", Repos: repos},
		},
	}
}

func probeTestToken(context.Context, config.Account, string) (string, string, error) {
	return "test-token", "test", nil
}

func TestUpstreamProbe_BoundedConcurrencyAndSharedAccount(t *testing.T) {
	var active, maximum, calls atomic.Int32
	started := make(chan struct{}, 12)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for previous := maximum.Load(); current > previous && !maximum.CompareAndSwap(previous, current); previous = maximum.Load() {
		}
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			_, _ = w.Write([]byte(`{}`))
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	cfg := probeTestConfig(srv.URL, 12)
	// The duplicate source must not repeat token lookup or HTTP requests.
	cfg.Sources["source-b"] = cfg.Sources["source-a"]
	var tokenCalls int
	resolve := func(ctx context.Context, acct config.Account, key string) (string, string, error) {
		tokenCalls++
		return probeTestToken(ctx, acct, key)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	finished := make(chan []upstreamProbeResult, 1)
	go func() { finished <- runUpstreamProbe(ctx, cfg, resolve, time.Second) }()
	for range probeConcurrency {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("workers did not reach the configured concurrency")
		}
	}
	if got := calls.Load(); got != probeConcurrency {
		t.Errorf("started %d requests while first workers blocked, want %d", got, probeConcurrency)
	}
	close(release)
	results := <-finished
	if len(results) != 12 || calls.Load() != 12 || tokenCalls != 1 {
		t.Fatalf("results=%d requests=%d token lookups=%d, want 12, 12, 1", len(results), calls.Load(), tokenCalls)
	}
	if maximum.Load() != probeConcurrency {
		t.Fatalf("peak concurrency=%d, want %d", maximum.Load(), probeConcurrency)
	}
	t.Logf("12 upstreams in two sources: requests=%d credential lookups=%d peak requests=%d", calls.Load(), tokenCalls, maximum.Load())
	app := &App{cfg: cfg}
	for i := range results {
		results[i].exists = false
	}
	if !app.applyUpstreamProbe(cfg, results, nil) || len(app.upstreamGone) != 24 {
		t.Fatalf("one upstream result must update both sources: %v", app.upstreamGone)
	}
}

func TestUpstreamProbe_SharedFlightAndThrottle(t *testing.T) {
	t.Setenv(credential.EnvVarName("account-a"), "test-token")
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			_, _ = w.Write([]byte(`{}`))
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	app := &App{cfg: probeTestConfig(srv.URL, 1)}
	app.probeUpstreamExistenceAsync()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not start")
	}
	flight, owner := app.beginUpstreamProbe()
	if flight == nil || owner {
		t.Fatal("concurrent caller did not share the active sweep")
	}
	for range 20 {
		app.probeUpstreamExistenceAsync()
		shared, owns := app.beginUpstreamProbe()
		if owns || shared != flight {
			t.Fatal("concurrent caller created another sweep")
		}
	}
	close(release)
	select {
	case <-flight.done:
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not complete")
	}
	if next, owner := app.beginUpstreamProbe(); next != nil || owner {
		t.Fatal("completed sweep did not throttle a startup/fetch follower")
	}
	if calls.Load() != 1 {
		t.Fatalf("requests=%d, want 1", calls.Load())
	}
	t.Logf("20 overlapping async triggers plus startup/fetch followers: provider requests=%d", calls.Load())
}

func TestUpstreamProbe_CancellationAndErrorsPreserveGoneState(t *testing.T) {
	for _, scenario := range []string{"request timeout", "sweep timeout", "auth error", "server error"} {
		t.Run(scenario, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				switch scenario {
				case "auth error":
					w.WriteHeader(http.StatusUnauthorized)
				case "server error":
					w.WriteHeader(http.StatusInternalServerError)
				default:
					<-r.Context().Done()
				}
			}))
			defer srv.Close()
			cfg := probeTestConfig(srv.URL, 12)
			app := &App{cfg: cfg, upstreamGone: map[string]bool{"source-a/org/repo-0": true}}
			budget, requestTimeout := time.Second, 20*time.Millisecond
			if scenario == "sweep timeout" {
				budget, requestTimeout = 20*time.Millisecond, time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			start := time.Now()
			results := runUpstreamProbe(ctx, cfg, probeTestToken, requestTimeout)
			t.Logf("12 upstreams: elapsed=%s requests=%d request timeout=%s sweep budget=%s", time.Since(start), requests.Load(), requestTimeout, budget)
			if len(results) != 0 || app.applyUpstreamProbe(cfg, results, nil) {
				t.Fatalf("unconfirmed response changed state: %+v", results)
			}
			if !app.upstreamGone["source-a/org/repo-0"] || len(app.upstreamGone) != 1 {
				t.Fatal("a timeout/auth error changed an existing mark")
			}
			if scenario == "sweep timeout" && requests.Load() > probeConcurrency {
				t.Fatalf("sweep continued queued requests after cancellation: %d", requests.Load())
			}
		})
	}
}

func TestUpstreamProbe_CredentialLookupSharesSweepDeadline(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	resolve := func(ctx context.Context, _ config.Account, _ string) (string, string, error) {
		<-ctx.Done()
		return "", "", ctx.Err()
	}
	results := runUpstreamProbe(ctx, probeTestConfig(srv.URL, 4), resolve, time.Second)
	if len(results) != 0 || requests.Load() != 0 || ctx.Err() == nil {
		t.Fatalf("credential cancellation should stop HTTP: results=%d requests=%d error=%v", len(results), requests.Load(), ctx.Err())
	}
}

func TestUpstreamProbe_DiscardsStaleConfig(t *testing.T) {
	for _, change := range []string{"account removed", "provider", "credential", "repo removed", "repo config", "source account", "source folder", "global folder", "global credential"} {
		t.Run(change, func(t *testing.T) {
			app := &App{cfg: probeTestConfig("https://example.invalid", 1)}
			snapshot := app.snapshotRefreshConfig()
			acct := app.cfg.Accounts["account-a"]
			src := app.cfg.Sources["source-a"]
			switch change {
			case "account removed":
				delete(app.cfg.Accounts, "account-a")
			case "provider":
				acct.Provider = "gitlab"
				app.cfg.Accounts["account-a"] = acct
			case "credential":
				acct.GCM = &config.GCMConfig{Provider: "github", UseHTTPPath: true}
				app.cfg.Accounts["account-a"] = acct
			case "repo removed":
				delete(src.Repos, "org/repo-0")
			case "repo config":
				src.Repos["org/repo-0"] = config.Repo{CredentialType: "ssh"}
			case "source account":
				src.Account = "account-b"
				app.cfg.Sources["source-a"] = src
			case "source folder":
				src.Folder = "elsewhere"
				app.cfg.Sources["source-a"] = src
			case "global folder":
				app.cfg.Global.Folder = "elsewhere"
			case "global credential":
				app.cfg.Global.CredentialGCM = &config.GCMGlobal{CredentialStore: "different"}
			}
			results := []upstreamProbeResult{{accountKey: "account-a", repoKey: "org/repo-0", exists: false}}
			if app.applyUpstreamProbe(snapshot, results, nil) || len(app.upstreamGone) != 0 {
				t.Fatal("result from stale configuration was published")
			}
		})
	}
}

func TestUpstreamProbe_DefinitiveResultsRestoreAndMark(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/repos/org/repo-0" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cfg := probeTestConfig(srv.URL, 2)
	app := &App{cfg: cfg, upstreamGone: map[string]bool{"source-a/org/repo-1": true}}
	results := runUpstreamProbe(context.Background(), cfg, probeTestToken, time.Second)
	if !app.applyUpstreamProbe(cfg, results, nil) {
		t.Fatal("confirmed provider results did not change marks")
	}
	if len(app.upstreamGone) != 1 || !app.upstreamGone["source-a/org/repo-0"] {
		t.Fatalf("unexpected confirmed marks: %v", app.upstreamGone)
	}
	if app.applyUpstreamProbe(cfg, results, nil) {
		t.Fatal("identical results signaled another status event")
	}
}

func TestUpstreamProbe_NewerFetchObservationWins(t *testing.T) {
	for _, fetchGone := range []bool{false, true} {
		t.Run(fmt.Sprint(fetchGone), func(t *testing.T) {
			cfg := probeTestConfig("https://example.invalid", 1)
			app := &App{cfg: cfg}
			revisions := app.snapshotProbeRevisions()
			// A successful fetch is a fresh observation even when no mark flips.
			app.markUpstreamGone("source-a", "org/repo-0", fetchGone)
			results := []upstreamProbeResult{{accountKey: "account-a", repoKey: "org/repo-0", exists: fetchGone}}
			if app.applyUpstreamProbe(cfg, results, &revisions) {
				t.Fatal("older provider response replaced the newer fetch result")
			}
			if app.upstreamGone["source-a/org/repo-0"] != fetchGone {
				t.Fatalf("newer fetch mark lost: %v", app.upstreamGone)
			}
		})
	}
}

func TestUpstreamProbe_RotatedTokenDiscardsOldResult(t *testing.T) {
	cfg := probeTestConfig("https://example.invalid", 1)
	app := &App{cfg: cfg}
	revisions := app.snapshotProbeRevisions()
	// Token contents live outside config; their revision catches same-type rotation.
	app.credentialRevision = map[string]uint64{"account-a": 1}
	results := []upstreamProbeResult{{accountKey: "account-a", repoKey: "org/repo-0", exists: false}}
	if app.applyUpstreamProbe(cfg, results, &revisions) || len(app.upstreamGone) != 0 {
		t.Fatal("result using a rotated token was published")
	}
}

func TestUpstreamProbe_ShutdownCancelsAndJoinsWorker(t *testing.T) {
	t.Setenv(credential.EnvVarName("account-a"), "test-token")
	started := make(chan struct{}, 1)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer srv.Close()
	app := &App{cfg: probeTestConfig(srv.URL, 1)}
	app.probeUpstreamExistenceAsync()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not start")
	}
	done := make(chan struct{})
	go func() {
		app.stopRefresh()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not cancel and join the probe")
	}
	if next, owner := app.beginUpstreamProbe(); next != nil || owner {
		t.Fatal("shutdown allowed a new synchronous probe")
	}
	app.probeUpstreamExistenceAsync()
	if calls.Load() != 1 || len(app.upstreamGone) != 0 {
		t.Fatalf("shutdown started another request or changed marks: requests=%d marks=%v", calls.Load(), app.upstreamGone)
	}
}

func TestUpstreamProbe_CredentialChangeRetainsThrottleInvalidation(t *testing.T) {
	t.Setenv(credential.EnvVarName("account-a"), "test-token")
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
			w.WriteHeader(http.StatusNotFound)
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	app := &App{cfg: probeTestConfig(srv.URL, 1)}
	app.probeUpstreamExistenceAsync()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not start")
	}
	flight, _ := app.beginUpstreamProbe()
	app.invalidateCredential("account-a")
	close(release)
	select {
	case <-flight.done:
	case <-time.After(3 * time.Second):
		t.Fatal("old probe did not finish")
	}
	if len(app.upstreamGone) != 0 {
		t.Fatal("old-token provider response was applied")
	}
	next, owner := app.beginUpstreamProbe()
	if !owner || next == nil {
		t.Fatal("old-token sweep overwrote the credential invalidation throttle")
	}
	app.finishUpstreamProbe(next)
	if !app.upstreamGone["source-a/org/repo-0"] {
		t.Fatal("replacement probe did not publish its confirmed result")
	}
}
