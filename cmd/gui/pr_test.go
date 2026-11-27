package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/provider"
)

type prListerFunc func(context.Context, string, string, string, bool) (provider.AccountPRs, error)

func (f prListerFunc) ListAccountPRs(ctx context.Context, url, token, user string, drafts bool) (provider.AccountPRs, error) {
	return f(ctx, url, token, user, drafts)
}

func prTestApp(t *testing.T, count int, lister prListerFunc) (*App, *recordingSink) {
	t.Helper()
	dir := t.TempDir()
	events := &recordingSink{}
	a := newTestApp(
		withCfg(&config.Config{
			Version:  config.CurrentVersion,
			Global:   config.GlobalConfig{Roots: []string{dir}},
			Accounts: make(map[string]config.Account),
			Sources:  make(map[string]config.Source),
		}),
		withCfgPath(filepath.Join(dir, "gitbox.json")),
		withCfgLoaded(),
		withEvents(events),
		withPRs(prRefreshState{lister: lister}),
		withRefreshHooks(&refreshHooks{dial: func(context.Context, string) error { return nil }}),
	)
	for i := range count {
		key := fmt.Sprintf("account-%d", i)
		a.cfg.Accounts[key] = config.Account{Provider: "github", URL: "https://example.invalid", Username: key, Name: key, Email: key + "@example.invalid", DefaultCredentialType: "token"}
		a.cfg.Sources[key] = config.Source{Account: key, Repos: map[string]config.Repo{"org/repo": {}}}
	}
	return a, events
}

func testAccountPRs(number int) provider.AccountPRs {
	return provider.AccountPRs{Complete: true, ByRepo: map[string]provider.PRSummary{
		"Org/Repo": {Authored: []provider.PullRequest{{Number: number}}},
	}}
}

func TestPRRefreshSharesRequestsAndBoundsWorkers(t *testing.T) {
	t.Setenv("GIT_TOKEN", "test-token")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		started := make(chan string, 16)
		a, events := prTestApp(t, 5, func(ctx context.Context, _, _ string, user string, _ bool) (provider.AccountPRs, error) {
			started <- user
			select {
			case <-release:
				return testAccountPRs(1), nil
			case <-ctx.Done():
				return provider.AccountPRs{}, ctx.Err()
			}
		})
		defer a.stopRefresh()

		a.be().RefreshAllPRs()
		synctest.Wait()
		for range 5 {
			a.be().RefreshAllPRs()
		}
		synctest.Wait()
		if got := len(started); got != 2 {
			t.Fatalf("requests started while workers wait: %d, want 2", got)
		}

		close(release)
		synctest.Wait()
		if got := events.named("pr:refreshed"); len(got) != 5 {
			t.Errorf("got %d account updates, want 5", len(got))
		}
		calls := make(map[string]int)
		for len(started) != 0 {
			calls[<-started]++
		}
		if len(calls) != 5 {
			t.Errorf("called %d distinct accounts, want 5", len(calls))
		}
		for account, n := range calls {
			if n != 1 {
				t.Errorf("%s: %d calls, want 1", account, n)
			}
		}
	})
}

func TestPRRefreshKeepsLastGoodDataPerApp(t *testing.T) {
	t.Setenv("GIT_TOKEN", "test-token")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		a, events := prTestApp(t, 1, func(context.Context, string, string, string, bool) (provider.AccountPRs, error) {
			calls++
			if calls == 1 {
				data := testAccountPRs(17)
				data.Complete = false
				return data, nil
			}
			return provider.AccountPRs{}, &provider.ProviderError{Status: 429, Class: provider.ClassRateLimited, Detail: "rate limited"}
		})
		defer a.stopRefresh()
		a.be().RefreshAllPRs()
		synctest.Wait()
		a.be().RefreshAllPRs()
		synctest.Wait()

		updates := events.named("pr:refreshed")
		if len(updates) != 2 || updates[1].args[0].(PRAccountUpdateDTO).Error == "" {
			t.Fatalf("wanted one successful update and one error, got %+v", updates)
		}
		summary := a.be().GetPRsForRepo("account-0", "org/repo")
		if !updates[0].args[0].(PRAccountUpdateDTO).ByRepo["org/repo"].Partial {
			t.Error("incremental update lost the provider's incomplete listing state")
		}
		if len(summary.Authored) != 1 || summary.Authored[0].Number != 17 || !summary.Partial {
			t.Errorf("last good PR dropped: %+v", summary)
		}
		other, _ := prTestApp(t, 1, nil)
		if got := other.be().GetPRsForRepo("account-0", "org/repo"); len(got.Authored) != 0 {
			t.Errorf("another App can see cached PRs: %+v", got)
		}
	})
}

func TestPRRefreshRenameTransfersCacheAndDropsOldFlight(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_TOKEN", "test-token")
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		started := make(chan struct{}, 2)
		calls := 0
		a, events := prTestApp(t, 1, func(ctx context.Context, _, _, _ string, _ bool) (provider.AccountPRs, error) {
			calls++
			started <- struct{}{}
			n := calls
			if n == 2 {
				select {
				case <-release:
				case <-ctx.Done():
					return provider.AccountPRs{}, ctx.Err()
				}
			}
			return testAccountPRs(n), nil
		})
		defer a.stopRefresh()
		a.be().RefreshAllPRs()
		synctest.Wait()
		a.be().RefreshAllPRs()
		synctest.Wait()
		if len(started) != 2 {
			t.Fatal("second request is not in flight")
		}
		if report, err := a.RenameAccount("account-0", "account-new"); err != nil || !report.OK {
			t.Fatalf("rename: %+v (%v)", report, err)
		}
		close(release)
		synctest.Wait()
		if updates := events.named("pr:refreshed"); len(updates) != 1 {
			t.Fatalf("late update escaped the rename: %+v", updates)
		}
		summary := a.be().GetPRsForRepo("account-new", "org/repo")
		if len(summary.Authored) != 1 || summary.Authored[0].Number != 1 {
			t.Errorf("rename did not transfer last good PR: %+v", summary)
		}
	})
}

func TestPRRefreshDropsStaleCredentialsDraftsAndDeletedAccounts(t *testing.T) {
	t.Setenv("GIT_TOKEN", "test-token")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, tc := range []struct {
		name   string
		change func(*App) error
	}{
		{"credential revision", func(a *App) error { a.invalidateCredential("account-0"); return nil }},
		{"drafts round trip", func(a *App) error {
			if err := a.SetPRIncludeDrafts(false); err != nil {
				return err
			}
			return a.SetPRIncludeDrafts(true)
		}},
		{"deleted account", func(a *App) error { _, err := a.DeleteAccount("account-0", false); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				release := make(chan struct{})
				started := make(chan struct{}, 1)
				a, events := prTestApp(t, 1, func(ctx context.Context, _, _, _ string, _ bool) (provider.AccountPRs, error) {
					started <- struct{}{}
					select {
					case <-release:
						return testAccountPRs(42), nil
					case <-ctx.Done():
						return provider.AccountPRs{}, ctx.Err()
					}
				})
				defer a.stopRefresh()
				a.be().RefreshAllPRs()
				synctest.Wait()
				if len(started) != 1 {
					t.Fatal("request is not in flight")
				}
				if err := tc.change(a); err != nil {
					t.Fatal(err)
				}
				close(release)
				synctest.Wait()
				if got := events.named("pr:refreshed"); len(got) != 0 {
					t.Errorf("published stale updates: %+v", got)
				}
				if got := a.be().GetPRsForRepo("account-0", "org/repo"); len(got.Authored) != 0 {
					t.Errorf("cached data from stale request: %+v", got)
				}
			})
		})
	}
}

func TestPRRefreshShutdownCancelsListerAndDiscardsQueuedWork(t *testing.T) {
	t.Setenv("GIT_TOKEN", "test-token")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{}, 5)
		cancelled := make(chan struct{}, 5)
		a, events := prTestApp(t, 5, func(ctx context.Context, _, _, _ string, _ bool) (provider.AccountPRs, error) {
			started <- struct{}{}
			<-ctx.Done()
			cancelled <- struct{}{}
			return provider.AccountPRs{}, ctx.Err()
		})
		a.be().RefreshAllPRs()
		synctest.Wait()
		before := time.Now()
		a.stopRefresh()
		a.be().RefreshAllPRs()
		synctest.Wait()
		if elapsed := time.Since(before); elapsed != 0 {
			t.Errorf("shutdown waited %s instead of cancelling requests immediately", elapsed)
		}
		if len(started) != 2 || len(cancelled) != 2 {
			t.Errorf("wanted two started and two cancelled calls, got %d and %d", len(started), len(cancelled))
		}
		if got := events.named("pr:refreshed"); len(got) != 0 {
			t.Errorf("emitted after shutdown: %+v", got)
		}
	})
}

func TestPRRefreshDropsResultsAfterExternalTokenReplacement(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	const key = "account-0"
	t.Setenv("GIT_TOKEN", "")
	t.Setenv(credential.EnvVarName(key), "")
	if err := credential.StoreToken(key, "fixture-one"); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		started := make(chan struct{}, 1)
		a, events := prTestApp(t, 1, func(ctx context.Context, _, _, _ string, _ bool) (provider.AccountPRs, error) {
			started <- struct{}{}
			select {
			case <-release:
				return testAccountPRs(42), nil
			case <-ctx.Done():
				return provider.AccountPRs{}, ctx.Err()
			}
		})
		defer a.stopRefresh()
		a.be().RefreshAllPRs()
		synctest.Wait()
		if len(started) != 1 {
			t.Fatal("request is not in flight")
		}
		if err := credential.StoreToken(key, "fixture-two"); err != nil {
			t.Fatal(err)
		}
		close(release)
		synctest.Wait()
		if got := events.named("pr:refreshed"); len(got) != 0 {
			t.Fatalf("published an update from the replaced credential: %+v", got)
		}
		<-started
		a.be().RefreshAllPRs()
		synctest.Wait()
		if got := events.named("pr:refreshed"); len(got) != 1 {
			t.Errorf("wanted an update from the replacement credential, got %+v", got)
		}
	})
}

func TestRemotePRRefreshPreservesOptionalPartialField(t *testing.T) {
	for _, field := range []string{"", `,"partial":true`} {
		raw := json.RawMessage(`{"accountKey":"account","byRepo":{"org/repo":{"authored":[],"reviewRequested":[]` + field + `}}}`)
		got, err := relayedEvents["pr:refreshed"](nil, raw)
		if err != nil {
			t.Fatal(err)
		}
		decoded := got.(map[string]any)["byRepo"].(map[string]any)["org/repo"].(map[string]any)
		partial, present := decoded["partial"]
		if present != (field != "") || present && partial != true {
			t.Fatalf("relayed summary = %+v; input field = %q", decoded, field)
		}
	}
}
