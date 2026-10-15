package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

type refreshTestEvent struct {
	name string
	args []any
}

type refreshTestEvents struct {
	mu     sync.Mutex
	events []refreshTestEvent
}

func (e *refreshTestEvents) emit(name string, args ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, refreshTestEvent{name, args})
}

func (e *refreshTestEvents) named(name string) []refreshTestEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	var found []refreshTestEvent
	for _, event := range e.events {
		if event.name == name {
			found = append(found, event)
		}
	}
	return found
}

func refreshTestApp(t *testing.T, count int) (*App, *refreshTestEvents) {
	t.Helper()
	root := t.TempDir()
	repos := make(map[string]config.Repo, count)
	order := make([]string, 0, count)
	for i := range count {
		name := fmt.Sprintf("org/repo-%d", i)
		path := filepath.Join(root, fmt.Sprintf("clone-%d", i))
		if err := os.MkdirAll(filepath.Join(path, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		repos[name] = config.Repo{CloneFolder: path}
		order = append(order, name)
	}
	cfg := &config.Config{
		Version: config.CurrentVersion,
		Global:  config.GlobalConfig{Folder: root},
		Accounts: map[string]config.Account{
			"account-a": {DefaultCredentialType: "token", URL: "https://example.invalid"},
		},
		Sources: map[string]config.Source{
			"source-a": {Account: "account-a", Repos: repos, RepoOrder: order},
		},
		SourceOrder: []string{"source-a"},
	}
	events := &refreshTestEvents{}
	a := &App{cfg: cfg, refreshHooks: &refreshHooks{
		status: func(cfg *config.Config) []status.RepoStatus {
			var results []status.RepoStatus
			for source, src := range cfg.Sources {
				for repo := range src.Repos {
					results = append(results, status.RepoStatus{Source: source, Repo: repo, State: status.Clean})
				}
			}
			return results
		},
		fetch: func(context.Context, string) (string, error) { return "", nil },
		heal:  func(*config.Config, string, string) heal.Report { return heal.Report{} },
		credential: func(context.Context, config.Account, string, *config.Config) credential.StatusResult {
			return refreshCredentialOK()
		},
		emit: events.emit,
	}}
	return a, events
}

func refreshCredentialOK() credential.StatusResult {
	return credential.StatusResult{Primary: credential.StatusOK, PAT: credential.StatusOK, Overall: credential.StatusOK}
}

func TestRefresh_ConcurrentFetchAllSharesCompleteCycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, events := refreshTestApp(t, 1)
		defer a.stopRefresh()
		release := make(chan struct{})
		var fetches, checks atomic.Int32
		a.refreshHooks.fetch = func(context.Context, string) (string, error) {
			fetches.Add(1)
			<-release
			return "", nil
		}
		a.refreshHooks.credential = func(context.Context, config.Account, string, *config.Config) credential.StatusResult {
			checks.Add(1)
			return refreshCredentialOK()
		}
		results := make(chan error, 20)
		for range 20 {
			go func() { results <- a.FetchAllRepos() }()
		}
		synctest.Wait()
		if fetches.Load() != 1 || len(events.named("fetch:allstart")) != 1 || len(events.named("fetch:alldone")) != 0 {
			t.Fatalf("overlapping callers did not share active work: fetches=%d events=%v", fetches.Load(), events.events)
		}
		close(release)
		synctest.Wait()
		for range 20 {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
		if fetches.Load() != 1 || checks.Load() != 1 || len(events.named("fetch:alldone")) != 1 || len(events.named("credentials:updated")) != 1 {
			t.Fatalf("shared cycle repeated work: fetches=%d credential checks=%d events=%v", fetches.Load(), checks.Load(), events.events)
		}
	})
}

func TestRefresh_SingleRepoAndFetchAllShareTransfer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _ := refreshTestApp(t, 1)
		defer a.stopRefresh()
		release := make(chan struct{})
		var fetches, repairs atomic.Int32
		a.refreshHooks.heal = func(*config.Config, string, string) heal.Report {
			repairs.Add(1)
			return heal.Report{}
		}
		a.refreshHooks.fetch = func(context.Context, string) (string, error) {
			fetches.Add(1)
			<-release
			return "", nil
		}
		done := make(chan error, 1)
		go func() { done <- a.FetchAllRepos() }()
		synctest.Wait()
		a.FetchRepo("source-a", "org/repo-0")
		synctest.Wait()
		if fetches.Load() != 1 || repairs.Load() != 1 {
			t.Fatalf("same clone ran concurrent maintenance/transfers: fetches=%d repairs=%d", fetches.Load(), repairs.Load())
		}
		close(release)
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if fetches.Load() != 1 || repairs.Load() != 1 {
			t.Fatalf("joining a transfer queued a redundant follow-up: fetches=%d repairs=%d", fetches.Load(), repairs.Load())
		}
	})
}

func TestRefresh_StatusReadersAndInvalidationBurstCoalesce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, events := refreshTestApp(t, 1)
		defer a.stopRefresh()
		release := make(chan struct{})
		var checks atomic.Int32
		a.refreshHooks.status = func(*config.Config) []status.RepoStatus {
			if checks.Add(1) == 1 {
				<-release
			}
			return []status.RepoStatus{{Source: "source-a", Repo: "org/repo-0", State: status.Clean}}
		}
		results := make(chan []StatusResult, 20)
		for range 20 {
			go func() { results <- a.GetAllStatus() }()
		}
		synctest.Wait()
		if checks.Load() != 1 {
			t.Fatalf("concurrent readers started %d status sweeps", checks.Load())
		}
		for range 20 {
			a.RefreshStatus()
		}
		synctest.Wait()
		close(release)
		synctest.Wait()
		for range 20 {
			if got := <-results; len(got) != 1 || got[0].State != "clean" {
				t.Fatalf("shared reader received %v", got)
			}
		}
		if checks.Load() != 2 || len(events.named("status:updated")) != 1 {
			t.Fatalf("invalidation burst needs one follow-up: checks=%d events=%v", checks.Load(), events.events)
		}
		if got := a.GetAllStatus(); len(got) != 1 || checks.Load() != 3 {
			t.Fatalf("a later independent read reused completed status: results=%v checks=%d", got, checks.Load())
		}
	})
}

func TestRefresh_MixedSingleAndFullFetchesRespectGlobalWorkerLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _ := refreshTestApp(t, 12)
		defer a.stopRefresh()
		release := make(chan struct{})
		var active, maximum atomic.Int32
		a.refreshHooks.fetch = func(context.Context, string) (string, error) {
			n := active.Add(1)
			defer active.Add(-1)
			for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
			}
			<-release
			return "", nil
		}
		done := make(chan error, 1)
		go func() { done <- a.FetchAllRepos() }()
		synctest.Wait()
		for i := range 12 {
			a.FetchRepo("source-a", fmt.Sprintf("org/repo-%d", i))
		}
		synctest.Wait()
		if got := active.Load(); got != fetchAllConcurrency {
			t.Fatalf("mixed requests started %d simultaneous transfers, want %d", got, fetchAllConcurrency)
		}
		close(release)
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if active.Load() != 0 || maximum.Load() > fetchAllConcurrency {
			t.Fatalf("transfer limit or cleanup failed: active=%d peak=%d", active.Load(), maximum.Load())
		}
	})
}

func TestRefresh_EmptyFleetReturnsJSONArray(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _ := refreshTestApp(t, 0)
		defer a.stopRefresh()
		data, err := json.Marshal(a.GetAllStatus())
		if err != nil || string(data) != "[]" {
			t.Fatalf("empty status must cross the Wails bridge as an array: %s, %v", data, err)
		}
	})
}

func TestRefresh_TokenRotationDoesNotShareOrPublishOldCredentials(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, events := refreshTestApp(t, 0)
		defer a.stopRefresh()
		oldDone, newDone := make(chan struct{}), make(chan struct{})
		var checks atomic.Int32
		a.refreshHooks.credential = func(context.Context, config.Account, string, *config.Config) credential.StatusResult {
			if checks.Add(1) == 1 {
				<-oldDone
				return credential.StatusResult{Primary: credential.StatusError, PAT: credential.StatusError, Overall: credential.StatusError, PrimaryDetail: "old token rejected"}
			}
			<-newDone
			return refreshCredentialOK()
		}
		bulkDone := make(chan struct{})
		go func() {
			a.verifyRefreshCredentials(a.snapshotRefreshConfig())
			close(bulkDone)
		}()
		synctest.Wait()
		a.invalidateCredential("account-a")
		fresh := make(chan CredentialStatus, 1)
		go func() { fresh <- a.CredentialVerify("account-a") }()
		synctest.Wait()
		if checks.Load() != 2 {
			t.Fatalf("rotated credentials joined old verification: checks=%d", checks.Load())
		}
		close(oldDone)
		synctest.Wait()
		if len(events.named("credentials:updated")) != 0 || checks.Load() != 2 {
			t.Fatalf("old check published stale credentials or failed to join current work: checks=%d events=%v", checks.Load(), events.events)
		}
		close(newDone)
		synctest.Wait()
		<-bulkDone
		if got := <-fresh; got.Status != "ok" {
			t.Fatalf("fresh check received obsolete status: %+v", got)
		}
		for _, event := range events.named("credentials:updated") {
			if got := event.args[0].(map[string]CredentialStatus)["account-a"]; got.Status != "ok" {
				t.Fatalf("bulk check published obsolete token status: %+v", got)
			}
		}
	})
}

func TestRefresh_ChangedConfigRejectsOldStatusBeforePublishing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, events := refreshTestApp(t, 1)
		defer a.stopRefresh()
		release := make(chan struct{})
		var checks atomic.Int32
		a.refreshHooks.status = func(cfg *config.Config) []status.RepoStatus {
			if checks.Add(1) == 1 {
				<-release
			}
			state := status.Dirty
			if cfg.Sources["source-a"].Repos["org/repo-0"].Name == "new identity" {
				state = status.Clean
			}
			return []status.RepoStatus{{Source: "source-a", Repo: "org/repo-0", State: state}}
		}
		a.RefreshStatus()
		synctest.Wait()
		a.mu.Lock()
		src := a.cfg.Sources["source-a"]
		repo := src.Repos["org/repo-0"]
		repo.Name = "new identity"
		src.Repos["org/repo-0"] = repo
		a.mu.Unlock()
		close(release)
		synctest.Wait()
		published := events.named("status:updated")
		if checks.Load() != 2 || len(published) != 1 {
			t.Fatalf("config mutation published stale work: checks=%d events=%v", checks.Load(), published)
		}
		if got := published[0].args[0].([]StatusResult); len(got) != 1 || got[0].State != "clean" {
			t.Fatalf("published status from an obsolete config: %v", got)
		}
	})
}

func TestRefresh_LocalStatusPrecedesRemoteChecksAndCompletionWaitsForBoth(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, events := refreshTestApp(t, 1)
		defer a.stopRefresh()
		a.cfg.Accounts["account-b"] = config.Account{DefaultCredentialType: "token"}
		providerDone := make(chan struct{})
		a.probeFlight = &upstreamProbeFlight{done: providerDone}
		credentialsDone := make(chan struct{})
		var mu sync.Mutex
		checks := make(map[string]int)
		a.refreshHooks.credential = func(_ context.Context, _ config.Account, key string, _ *config.Config) credential.StatusResult {
			mu.Lock()
			checks[key]++
			mu.Unlock()
			<-credentialsDone
			return refreshCredentialOK()
		}
		done := make(chan error, 1)
		go func() { done <- a.FetchAllRepos() }()
		synctest.Wait()
		if len(events.named("status:updated")) != 1 || len(events.named("fetch:alldone")) != 0 || len(events.named("credentials:updated")) != 0 {
			t.Fatalf("slow remote checks blocked local status or finished early: %v", events.events)
		}
		close(credentialsDone)
		synctest.Wait()
		if len(events.named("credentials:updated")) != 1 || len(events.named("fetch:alldone")) != 0 {
			t.Fatalf("completion did not wait for the active provider probe: %v", events.events)
		}
		close(providerDone)
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if checks["account-a"] != 1 || checks["account-b"] != 1 || len(events.named("fetch:alldone")) != 1 {
			t.Fatalf("wrong cycle completion/check counts: checks=%v events=%v", checks, events.events)
		}
	})
}

func TestRefresh_ShutdownCancelsWorkersAndRejectsNewWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _ := refreshTestApp(t, 1)
		var active atomic.Int32
		a.refreshHooks.fetch = func(ctx context.Context, _ string) (string, error) {
			active.Add(1)
			defer active.Add(-1)
			<-ctx.Done()
			return "", ctx.Err()
		}
		a.refreshHooks.credential = func(ctx context.Context, _ config.Account, _ string, _ *config.Config) credential.StatusResult {
			active.Add(1)
			defer active.Add(-1)
			<-ctx.Done()
			return credential.StatusResult{Overall: credential.StatusOffline}
		}
		done := make(chan error, 1)
		go func() { done <- a.FetchAllRepos() }()
		synctest.Wait()
		if active.Load() != 2 {
			t.Fatalf("test did not reach both blocked workers: %d", active.Load())
		}
		a.stopRefresh()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("shutdown fetch returned %v", err)
		}
		if active.Load() != 0 || a.fetchRun != nil || len(a.repoFetches) != 0 || len(a.credentialRuns) != 0 {
			t.Fatal("shutdown returned while refresh workers were still active")
		}
		if err := a.FetchAllRepos(); !errors.Is(err, context.Canceled) {
			t.Fatalf("fetch after shutdown returned %v", err)
		}
		a.FetchRepo("source-a", "org/repo-0")
		if got := a.GetAllStatus(); len(got) != 0 {
			t.Fatalf("status after shutdown returned %v", got)
		}
	})
}

func TestRefresh_MaintenanceFailureSkipsNetworkAndSurfacesError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, events := refreshTestApp(t, 1)
		defer a.stopRefresh()
		var transfers atomic.Int32
		a.refreshHooks.heal = func(*config.Config, string, string) heal.Report {
			return heal.Report{Warnings: []string{"credential isolation repair failed"}}
		}
		a.refreshHooks.fetch = func(context.Context, string) (string, error) {
			transfers.Add(1)
			return "", nil
		}
		err := a.FetchAllRepos()
		if err == nil || !strings.Contains(err.Error(), "credential isolation repair failed") || transfers.Load() != 0 {
			t.Fatalf("unsafe maintenance allowed network work or lost its error: transfers=%d err=%v", transfers.Load(), err)
		}
		perRepo, allDone := events.named("fetch:done"), events.named("fetch:alldone")
		if len(perRepo) != 1 || len(allDone) != 1 {
			t.Fatalf("missing or repeated failure completion events: %v", events.events)
		}
		if perRepo[0].args[0].(map[string]any)["error"] == nil || allDone[0].args[0].(map[string]string)["error"] == "" {
			t.Fatal("completion events hid the maintenance error")
		}
	})
}

func TestRefresh_CredentialVerificationSharesOnlyActiveWork(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _ := refreshTestApp(t, 0)
		defer a.stopRefresh()
		release := make(chan struct{})
		var checks atomic.Int32
		a.refreshHooks.credential = func(context.Context, config.Account, string, *config.Config) credential.StatusResult {
			call := checks.Add(1)
			<-release
			if call == 1 {
				return refreshCredentialOK()
			}
			return credential.StatusResult{Overall: credential.StatusWarning, Primary: credential.StatusWarning}
		}
		results := make(chan CredentialStatus, 20)
		for range 20 {
			go func() { results <- a.CredentialVerify("account-a") }()
		}
		synctest.Wait()
		if checks.Load() != 1 {
			t.Fatalf("concurrent credential checks spawned %d checks", checks.Load())
		}
		close(release)
		synctest.Wait()
		for range 20 {
			if got := <-results; got.Status != "ok" {
				t.Fatalf("shared credential check returned %+v", got)
			}
		}
		if got := a.CredentialVerify("account-a"); got.Status != "warning" || checks.Load() != 2 {
			t.Fatalf("explicit recheck reused a completed result: status=%+v checks=%d", got, checks.Load())
		}
	})
}

func TestRefresh_ShutdownJoinsStandaloneCredentialVerification(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _ := refreshTestApp(t, 0)
		drain := make(chan struct{})
		cancelled := make(chan struct{})
		a.refreshHooks.credential = func(ctx context.Context, _ config.Account, _ string, _ *config.Config) credential.StatusResult {
			<-ctx.Done()
			close(cancelled)
			<-drain
			return credential.StatusResult{Overall: credential.StatusOffline}
		}
		checked := make(chan CredentialStatus, 1)
		go func() { checked <- a.CredentialVerify("account-a") }()
		synctest.Wait()
		stopped := make(chan struct{})
		go func() {
			a.stopRefresh()
			close(stopped)
		}()
		synctest.Wait()
		select {
		case <-cancelled:
		default:
			t.Fatal("shutdown did not cancel the standalone credential check")
		}
		premature := false
		select {
		case <-stopped:
			premature = true
		default:
		}
		close(drain)
		synctest.Wait()
		<-stopped
		<-checked
		if premature {
			t.Fatal("shutdown returned before the standalone credential worker exited")
		}
	})
}

func TestRefresh_RetriesAConnectRefusedRightAway(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, events := refreshTestApp(t, 1)
		defer a.stopRefresh()
		var calls atomic.Int32
		a.refreshHooks.fetch = func(context.Context, string) (string, error) {
			if calls.Add(1) == 1 {
				return "", errors.New("git fetch: exit status 128: fatal: unable to access 'https://h.lan/a.git/': Failed to connect to h.lan port 443 after 5 ms: Couldn't connect to server")
			}
			return "", nil
		}
		if err := a.FetchAllRepos(); err != nil {
			t.Fatalf("FetchAllRepos() = %v, want the retry to succeed", err)
		}
		if calls.Load() != 2 {
			t.Fatalf("fetch calls = %d, want 2 (one retry)", calls.Load())
		}
		done := events.named("fetch:done")
		if len(done) != 1 || done[0].args[0].(map[string]any)["error"] != nil {
			t.Fatalf("want one clean fetch:done, got %v", done)
		}
	})
}

func TestRefresh_DoesNotRetryOtherFetchErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _ := refreshTestApp(t, 1)
		defer a.stopRefresh()
		var calls atomic.Int32
		a.refreshHooks.fetch = func(context.Context, string) (string, error) {
			calls.Add(1)
			return "", errors.New("git fetch: exit status 128: fatal: Authentication failed")
		}
		if err := a.FetchAllRepos(); err == nil {
			t.Fatal("FetchAllRepos() = nil, want the authentication error")
		}
		if calls.Load() != 1 {
			t.Fatalf("fetch calls = %d, want 1 (no retry)", calls.Load())
		}
	})
}
