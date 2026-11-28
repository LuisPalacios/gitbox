package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/diag"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

const pullSettingsChanged git.PullSkipReason = "settingsChanged"

var errPullPlanChanged = errors.New("project changed since preview")

// PullEligibility describes whether the checkout can fast-forward using its
// current upstream status. Reason explains any local work that blocked it.
type PullEligibility struct {
	Eligible bool
	Reason   git.PullSkipReason
	Behind   int
}

func pullEligibility(st git.RepoStatus, reason git.PullSkipReason) PullEligibility {
	return PullEligibility{
		Eligible: reason == "" && st.Ahead == 0 && st.Behind > 0,
		Reason:   reason,
		Behind:   st.Behind,
	}
}

// checkPullEligibility reads the checkout again at execution time.
func (a *App) checkPullEligibility(ctx context.Context, path string) (PullEligibility, git.RepoStatus, error) {
	check := git.PullPreflightContext
	if a.refreshHooks != nil && a.refreshHooks.pullCheck != nil {
		check = a.refreshHooks.pullCheck
	}
	reason, st, err := check(ctx, path)
	eligibility := pullEligibility(st, reason)
	eligibility.Eligible = err == nil && eligibility.Eligible
	return eligibility, st, err
}

type pullAllStartDTO struct {
	Run    string    `json:"run"`
	PlanID string    `json:"planID,omitempty"`
	Pulls  []repoRef `json:"pulls"`
	Clones []repoRef `json:"clones"`
}

type pullAllDoneDTO struct {
	Run                 string `json:"run"`
	Error               string `json:"error,omitempty"`
	PlanID              string `json:"planID,omitempty"`
	SkippedSincePreview int    `json:"skippedSincePreview,omitempty"`
	PlanExpired         bool   `json:"planExpired,omitempty"`
}

func (a *App) pullRepoLocal(source, repo string) {
	a.localSession.goTracked(func() { a.pullRepoRun(a.localSession.refreshContext(), source, repo) })
}

func (a *App) pullRepoRun(ctx context.Context, source, repo string) {
	cfg, revision := a.localSession.refreshSnapshot()
	a.runSafePull(ctx, cfg, revision, repoRef{source, repo}, "", pullTarget{})
}

func (a *App) runSafePull(ctx context.Context, cfg *config.Config, revision uint64, ref repoRef, run string, target pullTarget) pullDoneDTO {
	op := a.newOperation().atRevision(revision)
	if _, ok := cfg.Sources[ref.Source].Repos[ref.Repo]; ok {
		emit(op, "pull:start", repoRunDTO{Source: ref.Source, Repo: ref.Repo, Run: run})
	}
	result := a.safePull(ctx, cfg, ref.Source, ref.Repo, run == "", target)
	result.Run = run
	op = a.localSession.currentRepoOperation(op, cfg, ref.Source, ref.Repo)
	emit(op, "pull:done", result)
	if result.Error == "" && result.Skipped.IsZero() && !result.Unchanged {
		path := status.RepoPath(cfg, ref.Source, ref.Repo, cfg.Sources[ref.Source].Repos[ref.Repo])
		a.localSession.publishCompletionStatus(ctx, op, cfg, ref.Source, ref.Repo, path)
	}
	return result
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
func (a *App) safePull(ctx context.Context, cfg *config.Config, source, repo string, requested bool, target pullTarget) pullDoneDTO {
	result := pullDoneDTO{Source: source, Repo: repo}
	addr, err := a.pullUnderGate(ctx, cfg, source, repo, requested, target, &result)
	if errors.Is(err, errServerUnreachable) {
		result.Error, result.Code, result.Host = unreachableDetails(addr)
		result.Skipped = skipReason{v: true}
	} else if errors.Is(err, errPullPlanChanged) {
		if result.Skipped.IsZero() && !result.Unchanged {
			result.Skipped = skipReason{v: pullSettingsChanged}
		}
	} else if err != nil {
		result.Error = userMsg(err)
		result.Code = toUserError(err).Code
		result.UnsafeConfig = unsafeConfigOf(err)
	}
	return result
}

func (a *App) pullUnderGate(ctx context.Context, cfg *config.Config, source, repo string, requested bool, target pullTarget, result *pullDoneDTO) (address string, err error) {
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
	eligible := func(ctx context.Context) (bool, error) {
		if target.branch != "" && (status.RepoRootOffline(cfg, source, r) || git.IsIncompleteClone(path) ||
			(!git.IsRepo(path) && !git.IsLinkedWorktree(path))) {
			return false, errPullPlanChanged
		}
		eligibility, st, err := a.checkPullEligibility(ctx, path)
		if err != nil {
			return false, err
		}
		result.BehindBefore = eligibility.Behind
		if !a.localSession.sameRefreshRepo(cfg, source, repo) ||
			(target.branch != "" && (target.branch != st.Branch || target.upstream != st.Upstream)) {
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
	var precheck func() error
	if target.branch != "" {
		precheck = func() error {
			checkCtx, cancel := context.WithTimeout(ctx, pullTimeout)
			defer cancel()
			ready, err := eligible(checkCtx)
			if err == nil && !ready {
				return errPullPlanChanged
			}
			return err
		}
	}

	report, unlock, err := a.prepareClone(ctx, cfg, repoRef{source, repo}, path, nil, precheck)
	if errors.Is(err, errCloneSettingsChanged) {
		result.Skipped = skipReason{v: pullSettingsChanged}
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer unlock()
	if report.Skipped == "" && !a.readOnly {
		result.RepairWarnings = append([]string{}, report.OtherWarnings()...)
	}
	if len(report.Trust) > 0 {
		return "", report.Trust[0].Err()
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
	if ready, err := eligible(ctx); err != nil || !ready {
		return "", err
	}
	addr := serverAddr(account)
	return addr, a.localSession.onServerCtx(ctx, addr, func(ctx context.Context) error {
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

func (a *App) pullAllLocal(run, planID string) {
	a.localSession.goTracked(func() { a.pullAllRun(a.localSession.refreshContext(), run, planID) })
}

func (a *App) pullAllRun(ctx context.Context, run, planID string) {
	cfg, revision := a.localSession.refreshSnapshot()
	op := a.newOperation().atRevision(revision)
	if run == "" || identifier(run) != run {
		emit(op.currentRevision(), "pull:alldone", pullAllDoneDTO{Run: run, PlanID: planID, Error: "Invalid pull run identifier"})
		return
	}
	scope := pullAllStartDTO{Run: run, PlanID: planID, Pulls: []repoRef{}, Clones: []repoRef{}}
	var targets map[repoRef]pullTarget
	if planID == "" {
		scope.Pulls, scope.Clones = livePullScope(cfg)
	} else {
		plan, ok := a.localSession.takePullPlan(planID)
		if !ok {
			emit(op.currentRevision(), "pull:alldone", pullAllDoneDTO{Run: run, PlanID: planID, PlanExpired: true})
			return
		}
		cfg, targets = plan.cfg, plan.targets
		scope.Pulls = append(scope.Pulls, plan.pulls...)
		scope.Clones = append(scope.Clones, plan.clones...)
	}
	emit(op, "pull:allstart", scope)
	var skipped atomic.Int64
	dispatchByHost(ctx, cfg, scope.Pulls, func(ref repoRef) {
		result := a.runSafePull(ctx, cfg, revision, ref, run, targets[ref])
		if planID != "" && result.Error == "" && (!result.Skipped.IsZero() || result.Unchanged) {
			skipped.Add(1)
		}
	})
	dispatchByHost(ctx, cfg, scope.Clones, func(ref repoRef) {
		if a.cloneRepoForRun(ctx, ref.Source, ref.Repo, "", cfg, run, planID != "") {
			skipped.Add(1)
		}
	})
	done := pullAllDoneDTO{Run: run, PlanID: planID, SkippedSincePreview: int(skipped.Load())}
	if err := ctx.Err(); err != nil {
		done.Error = userMsg(err)
	}
	emit(op.currentRevision(), "pull:alldone", done)
}

func livePullScope(cfg *config.Config) (pulls, clones []repoRef) {
	pulls, clones = []repoRef{}, []repoRef{}
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
					pulls = append(pulls, ref)
				}
			} else if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
				clones = append(clones, ref)
			}
		}
	}
	return pulls, clones
}
