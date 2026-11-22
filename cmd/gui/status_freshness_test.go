package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/status"
)

func TestStatusFreshness_FetchedRefsNeedACompatibleStatusSweep(t *testing.T) {
	app, _ := refreshTestApp(t, 1)
	defer app.stopRefresh()
	const source, repo = "source-a", "org/repo-0"
	snapshot := app.snapshotRefreshConfig()
	path := status.RepoPath(snapshot, source, repo, snapshot.Sources[source].Repos[repo])
	checked := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []StatusResult{{Source: source, Repo: repo, Account: "account-a", Path: path, State: "behind"}}

	app.recordFetchStatus(snapshot, source, repo, false, checked)
	rows[0].sweepStarted = checked.Add(-time.Second)
	if got := app.overlayStatus(rows)[0].CheckedAt; got != "" {
		t.Fatalf("a fetch newer than the sweep gave the older status a timestamp: %q", got)
	}

	rows[0].sweepStarted = checked.Add(time.Second)
	want := checked.Format(time.RFC3339)
	if got := app.overlayStatus(rows)[0].CheckedAt; got != want {
		t.Fatalf("timestamp = %q, want %q", got, want)
	}

	app.refreshHooks.fetch = func(context.Context, string) (string, error) {
		return "", errors.New("fetch did not complete")
	}
	if err := app.performFetch(snapshot, source, repo, path); err == nil {
		t.Fatal("expected fetch failure")
	}
	if got := app.overlayStatus(rows)[0].CheckedAt; got != want {
		t.Fatalf("a failed fetch replaced the last checked timestamp with %q", got)
	}

	app.mu.Lock()
	account := app.cfg.Accounts["account-a"]
	account.URL = "https://other.example.invalid"
	app.cfg.Accounts["account-a"] = account
	app.mu.Unlock()
	app.recordFetchStatus(snapshot, source, repo, false, checked.Add(time.Second))
	if got := app.overlayStatus(rows)[0].CheckedAt; got != "" {
		t.Fatalf("status inherited an obsolete provider timestamp: %q", got)
	}
}

func TestStatusFreshness_ConfirmedProbeDatesOnlyExistence(t *testing.T) {
	app, _ := refreshTestApp(t, 1)
	const source, repo = "source-a", "org/repo-0"
	snapshot := app.snapshotRefreshConfig()
	path := status.RepoPath(snapshot, source, repo, snapshot.Sources[source].Repos[repo])
	result := upstreamProbeResult{
		accountKey: "account-a",
		repoKey:    repo,
		exists:     true,
		checkedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if !app.applyUpstreamProbe(snapshot, []upstreamProbeResult{result}, nil) {
		t.Fatal("an existence timestamp should cause a status update")
	}
	row := StatusResult{Source: source, Repo: repo, Account: "account-a", Path: path}
	for _, state := range []string{"not cloned", "behind", "diverged", "conflict"} {
		row.State = state
		want := ""
		if state == "not cloned" {
			want = result.checkedAt.Format(time.RFC3339)
		}
		if got := app.overlayStatus([]StatusResult{row})[0].CheckedAt; got != want {
			t.Errorf("%s: checkedAt = %q, want %q", state, got, want)
		}
	}

	result.exists = false
	result.checkedAt = result.checkedAt.Add(time.Second)
	app.applyUpstreamProbe(snapshot, []upstreamProbeResult{result}, nil)
	got := app.overlayStatus([]StatusResult{row})[0]
	if got.State != "upstream gone" || got.CheckedAt != result.checkedAt.Format(time.RFC3339) {
		t.Fatalf("confirmed missing project did not include its upstream check: %#v", got)
	}
}

func TestStatusResult_DistinguishesLocalBranchFromUnknownDefault(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input status.RepoStatus
		want  string
	}{
		{"default", status.RepoStatus{IsDefault: true, DefaultKnown: true}, "true"},
		{"feature", status.RepoStatus{IsDefault: false, DefaultKnown: true}, "false"},
		{"unknown", status.RepoStatus{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bytes, err := json.Marshal(toStatusResult(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(bytes, &fields); err != nil {
				t.Fatal(err)
			}
			if got := string(fields["isDefault"]); got != tc.want {
				t.Fatalf("isDefault = %q, want %q", got, tc.want)
			}
		})
	}
}
