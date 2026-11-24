package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/diag"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

const pullSettingsChanged git.PullSkipReason = "settingsChanged"

// PullEligibility describes whether the checkout can fast-forward using its
// current upstream status. Reason explains any local work that blocked it.
type PullEligibility struct {
	Eligible bool
	Reason   git.PullSkipReason
	Behind   int
}

// checkPullEligibility uses the same checkout inspection for one pull,
// a fleet pull, or a preview. A preview does not acquire the clone gate.
func (a *App) checkPullEligibility(ctx context.Context, path string) (PullEligibility, git.RepoStatus, error) {
	check := git.PullPreflightContext
	if a.refreshHooks != nil && a.refreshHooks.pullCheck != nil {
		check = a.refreshHooks.pullCheck
	}
	reason, st, err := check(ctx, path)
	return PullEligibility{
		Eligible: err == nil && reason == "" && st.Ahead == 0 && st.Behind > 0,
		Reason:   reason,
		Behind:   st.Behind,
	}, st, err
}

type pullAllStartDTO struct {
	Run    string    `json:"run"`
	Pulls  []repoRef `json:"pulls"`
	Clones []repoRef `json:"clones"`
}

type pullAllDoneDTO struct {
	Run   string `json:"run"`
	Error string `json:"error,omitempty"`
}

func (a *App) pullRepoLocal(source, repo string) {
	a.goTracked(func() { a.pullRepoRun(a.refreshContext(), source, repo) })
}

func (a *App) pullRepoRun(ctx context.Context, source, repo string) {
	cfg, revision := a.refreshSnapshot()
	a.runSafePull(ctx, cfg, revision, repoRef{source, repo}, "")
}

func (a *App) runSafePull(ctx context.Context, cfg *config.Config, revision uint64, ref repoRef, run string) {
	op := a.newOperation().atRevision(revision)
	if _, ok := cfg.Sources[ref.Source].Repos[ref.Repo]; ok {
		emit(op, "pull:start", repoRunDTO{Source: ref.Source, Repo: ref.Repo, Run: run})
	}
	result := a.safePull(ctx, cfg, ref.Source, ref.Repo, run == "")
	result.Run = run
	op = a.currentRepoOperation(op, cfg, ref.Source, ref.Repo)
	emit(op, "pull:done", result)
	if result.Error == "" && result.Skipped.IsZero() && !result.Unchanged {
		path := status.RepoPath(cfg, ref.Source, ref.Repo, cfg.Sources[ref.Source].Repos[ref.Repo])
		a.publishCompletionStatus(ctx, op, cfg, ref.Source, ref.Repo, path)
	}
}

func describePullStatus(result *pullDoneDTO, st git.RepoStatus) {
	result.State = status.Classify(st).String()
	result.Modified = &st.Changed
	result.Untracked = &st.Untracked
	result.Ahead = &st.Ahead
	result.Behind = &st.Behind
}

// safePull rechecks the configuration and the working tree under the same
// clone gate that protects fetch and repair.
func (a *App) safePull(ctx context.Context, cfg *config.Config, source, repo string, requested bool) pullDoneDTO {
	result := pullDoneDTO{Source: source, Repo: repo}
	addr, err := a.pullUnderGate(ctx, cfg, source, repo, requested, &result)
	if errors.Is(err, errServerUnreachable) {
		result.Error, result.Code, result.Host = unreachableDetails(addr)
		result.Skipped = skipReason{v: true}
	} else if err != nil {
		result.Error = userMsg(err)
		result.Code = toUserError(err).Code
	}
	return result
}

func (a *App) pullUnderGate(ctx context.Context, cfg *config.Config, source, repo string, requested bool, result *pullDoneDTO) (address string, err error) {
	started := time.Now()
	src, ok := cfg.Sources[source]
	if !ok {
		return "", fmt.Errorf("source %q not found", source)
	}
	r, ok := src.Repos[repo]
	if !ok {
		return "", fmt.Errorf("repo %q not found in source %q", repo, source)
	}
	path := status.RepoPath(cfg, source, repo, r)
	var queueDepth *int
	defer func() { a.diagnostics.Record(diag.Pull, started, time.Now(), queueDepth, path, err) }()
	account := cfg.Accounts[src.Account]
	report, unlock, err := a.prepareClone(ctx, cfg, repoRef{source, repo}, path, nil)
	if errors.Is(err, errCloneSettingsChanged) {
		result.Skipped = skipReason{v: pullSettingsChanged}
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer unlock()
	if report.Skipped == "" && !a.readOnly {
		result.RepairWarnings = append([]string{}, report.Warnings...)
	}
	if len(report.Warnings) > 0 {
		return "", fmt.Errorf("repository maintenance: %s", strings.Join(report.Warnings, "; "))
	}

	pull := git.PullQuietContext
	if a.refreshHooks != nil && a.refreshHooks.pull != nil {
		pull = a.refreshHooks.pull
	}
	ctx, cancel := context.WithTimeout(ctx, pullTimeout)
	defer cancel()
	eligible := func(ctx context.Context) (bool, error) {
		eligibility, st, err := a.checkPullEligibility(ctx, path)
		if err != nil {
			return false, err
		}
		result.BehindBefore = eligibility.Behind
		if !a.sameRefreshRepo(cfg, source, repo) {
			result.Skipped = skipReason{v: pullSettingsChanged}
			return false, nil
		}
		// A direct pull can discover commits beyond a locally current
		// tracking ref. Fleet pulls use the locally known behind count.
		tryCurrent := requested && eligibility.Reason == "" && st.Ahead == 0 && st.Upstream != ""
		if eligibility.Eligible || tryCurrent {
			return true, nil
		}
		describePullStatus(result, st)
		if eligibility.Reason != "" {
			result.Skipped = skipReason{v: eligibility.Reason}
		} else {
			result.Unchanged = true
		}
		return false, nil
	}
	if ready, err := eligible(ctx); err != nil || !ready {
		return "", err
	}
	addr := serverAddr(account)
	return addr, a.onServerCtx(ctx, addr, func(ctx context.Context) error {
		release, depth, err := a.fetches.acquire(ctx, fetchHostname(account.URL))
		queueDepth = &depth
		if err != nil {
			return err
		}
		defer release()
		// Recheck local work after waiting for the server and transfer
		// slot so the final inspection is adjacent to Git.
		ready, err := eligible(ctx)
		if err != nil || !ready {
			return err
		}
		err = pull(ctx, path)
		// An editor can write after preflight. If Git refused the transfer,
		// report any new local work rather than interpreting its error text.
		if err != nil && ctx.Err() == nil {
			if eligibility, now, statusErr := a.checkPullEligibility(ctx, path); statusErr == nil && eligibility.Reason != "" {
				describePullStatus(result, now)
				result.Skipped = skipReason{v: eligibility.Reason}
				return nil
			}
		}
		return err
	})
}

func (a *App) pullAllLocal(run string) {
	a.goTracked(func() { a.pullAllRun(a.refreshContext(), run) })
}

func (a *App) pullAllRun(ctx context.Context, run string) {
	cfg, revision := a.refreshSnapshot()
	op := a.newOperation().atRevision(revision)
	if run == "" || identifier(run) != run {
		emit(op.currentRevision(), "pull:alldone", pullAllDoneDTO{Run: run, Error: "Invalid pull run identifier"})
		return
	}
	scope := pullAllStartDTO{Run: run, Pulls: []repoRef{}, Clones: []repoRef{}}
	for _, source := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[source]
		for _, repo := range src.OrderedRepoKeys() {
			item := src.Repos[repo]
			if status.RepoRootOffline(cfg, source, item) {
				continue
			}
			ref := repoRef{Source: source, Repo: repo}
			path := status.RepoPath(cfg, source, repo, item)
			if git.IsRepo(path) || git.IsLinkedWorktree(path) {
				if !git.IsIncompleteClone(path) {
					scope.Pulls = append(scope.Pulls, ref)
				}
			} else if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
				scope.Clones = append(scope.Clones, ref)
			}
		}
	}
	emit(op, "pull:allstart", scope)
	dispatchByHost(ctx, cfg, scope.Pulls, func(ref repoRef) {
		a.runSafePull(ctx, cfg, revision, ref, run)
	})
	dispatchByHost(ctx, cfg, scope.Clones, func(ref repoRef) {
		a.cloneRepoForRun(ctx, ref.Source, ref.Repo, "", cfg, run)
	})
	done := pullAllDoneDTO{Run: run}
	if err := ctx.Err(); err != nil {
		done.Error = userMsg(err)
	}
	emit(op.currentRevision(), "pull:alldone", done)
}
