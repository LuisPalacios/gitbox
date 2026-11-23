package status

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// CheckAll is the sweep without cancellation or bounds, for the tests that
// only look at its results.
func CheckAll(cfg *config.Config) []RepoStatus {
	results, _ := CheckAllContext(context.Background(), cfg, 0)
	return results
}

// countingRunner counts the git commands asked of it. hang makes every
// command run until its context is cancelled.
type countingRunner struct {
	calls atomic.Int32
	hang  bool
}

func (r *countingRunner) Run(ctx context.Context, _ git.Request) (git.Result, error) {
	r.calls.Add(1)
	if r.hang {
		<-ctx.Done()
		return git.Result{ExitCode: -1}, ctx.Err()
	}
	return git.Result{Stdout: []byte("# branch.oid abc\n# branch.head main\n# branch.upstream origin/main\n# branch.ab +0 -0\n")}, nil
}

// cloneConfig returns a config with n repos whose folders look like clones.
func cloneConfig(t *testing.T, n int) *config.Config {
	t.Helper()
	root := t.TempDir()
	repos := make(map[string]config.Repo, n)
	order := make([]string, 0, n)
	for i := range n {
		name := fmt.Sprintf("org/repo-%02d", i)
		path := filepath.Join(root, fmt.Sprintf("clone-%02d", i))
		if err := os.MkdirAll(filepath.Join(path, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		repos[name] = config.Repo{CloneFolder: path}
		order = append(order, name)
	}
	return &config.Config{
		Version:     config.CurrentVersion,
		Global:      config.GlobalConfig{Roots: []string{root}},
		Accounts:    map[string]config.Account{"acct": {URL: "https://example.invalid"}},
		Sources:     map[string]config.Source{"src": {Account: "acct", Repos: repos, RepoOrder: order}},
		SourceOrder: []string{"src"},
	}
}

// A sweep whose context is already cancelled starts no git at all.
func TestCheckAllContext_CancelledBeforeStartRunsNoGit(t *testing.T) {
	runner := &countingRunner{}
	git.SetRunnerForTest(t, runner)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	results, err := CheckAllContext(ctx, cloneConfig(t, 40), 0)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want the context's", err)
	}
	if runner.calls.Load() != 0 {
		t.Errorf("%d git commands ran for a sweep that was cancelled before it began", runner.calls.Load())
	}
	if len(results) != 40 || time.Since(started) > 2*time.Second {
		t.Errorf("%d results in %v; want the 40 slots back at once", len(results), time.Since(started))
	}
}

// A sweep cancelled while it runs stops the checks in flight and starts none
// of the ones still waiting for a turn.
func TestCheckAllContext_CancelMidwayStartsNothingElse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runner := &countingRunner{hang: true}
		git.SetRunnerForTest(t, runner)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		cfg := cloneConfig(t, 40)
		done := make(chan error, 1)
		go func() {
			_, err := CheckAllContext(ctx, cfg, 0)
			done <- err
		}()

		// The in-flight Git calls and the dispatcher are blocked.
		synctest.Wait()
		if got := runner.calls.Load(); got != checkAllConcurrency {
			t.Fatalf("%d checks in flight, want the %d slots", got, checkAllConcurrency)
		}

		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("err = %v, want the context's", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the sweep did not return after the cancel")
		}
		if got := runner.calls.Load(); got != checkAllConcurrency {
			t.Errorf("%d git commands ran in all; the %d checks still waiting must not start", got, 40-checkAllConcurrency)
		}
	})
}

// Each check has its own bound: one that hangs ends as an error and the
// sweep goes on to the next.
func TestCheckAllContext_PerRepoBound(t *testing.T) {
	runner := &countingRunner{hang: true}
	git.SetRunnerForTest(t, runner)

	started := time.Now()
	results, err := CheckAllContext(context.Background(), cloneConfig(t, 3), 40*time.Millisecond)
	if err != nil {
		t.Fatalf("a check that ran out of time must not fail the sweep: %v", err)
	}
	if spent := time.Since(started); spent > 5*time.Second {
		t.Errorf("the sweep took %v", spent)
	}
	if runner.calls.Load() != 3 {
		t.Errorf("%d checks ran, want all 3", runner.calls.Load())
	}
	for _, rs := range results {
		if rs.State != Error {
			t.Errorf("%s: state %v, want Error for a check that ran out of time", rs.Repo, rs.State)
		}
	}
}

type slowFleetRunner struct {
	countingRunner
	hold chan struct{}
}

func (r *slowFleetRunner) Run(ctx context.Context, request git.Request) (git.Result, error) {
	if filepath.Base(request.Dir) != "clone-00" {
		select {
		case <-ctx.Done():
			return git.Result{}, ctx.Err()
		case <-r.hold:
		}
	}
	return r.countingRunner.Run(ctx, request)
}

func TestCheckAllBatches(t *testing.T) {
	t.Run("bounded size and stable final order", func(t *testing.T) {
		git.SetRunnerForTest(t, &countingRunner{})
		cfg := cloneConfig(t, 20)
		seen := make(map[string]bool)
		batches := 0
		rows, err := CheckAllBatches(t.Context(), cfg, 0, func(rows []RepoStatus) {
			if len(rows) == 0 || len(rows) > 8 {
				t.Fatalf("batch length = %d", len(rows))
			}
			batches++
			for _, row := range rows {
				if seen[row.Repo] {
					t.Errorf("duplicate row %s", row.Repo)
				}
				seen[row.Repo] = true
			}
		})
		if err != nil || batches < 3 || len(seen) != 20 || len(rows) != 20 {
			t.Fatalf("batches=%d rows=%d err=%v", batches, len(seen), err)
		}
		for i, key := range cfg.Sources["src"].RepoOrder {
			if rows[i].Repo != key {
				t.Errorf("row %d = %s, want %s", i, rows[i].Repo, key)
			}
		}
	})
	t.Run("slow workers publish early and cancel", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := &slowFleetRunner{hold: make(chan struct{})}
			git.SetRunnerForTest(t, r)
			cfg := cloneConfig(t, 20)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			start := time.Now()
			batches := 0
			_, err := CheckAllBatches(ctx, cfg, 0, func(rows []RepoStatus) {
				batches++
				if time.Since(start) != 250*time.Millisecond || len(rows) != 1 || rows[0].Repo != "org/repo-00" {
					t.Errorf("first batch at %v: %+v", time.Since(start), rows)
				}
				cancel()
			})
			if batches != 1 || !errors.Is(err, context.Canceled) {
				t.Fatalf("%d batches, err=%v", batches, err)
			}
		})
	})
}
