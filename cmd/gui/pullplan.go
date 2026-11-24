package main

import (
	"crypto/rand"
	"encoding/hex"
	"slices"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

type PullPlanDTO struct {
	PlanID         string         `json:"planID"`
	ConfigRevision uint64         `json:"configRevision"`
	Stale          bool           `json:"stale"`
	WillClone      []repoRef      `json:"willClone"`
	WillUpdate     []PullPlanItem `json:"willUpdate"`
	Skipped        []PullPlanItem `json:"skipped"`
	Offline        []repoRef      `json:"offline"`
	RepairsPending []repoRef      `json:"repairsPending"`
}

type PullPlanItem struct {
	repoRef
	Behind int                `json:"behind,omitempty"`
	Reason git.PullSkipReason `json:"reason,omitempty"`
}

type pullTarget struct {
	branch, upstream string
}

type pendingPullPlan struct {
	id      string
	expires time.Time
	cfg     *config.Config
	pulls   []repoRef
	clones  []repoRef
	targets map[repoRef]pullTarget
}

const (
	pullPlanLifetime = 15 * time.Minute
	maxPullPlans     = 8
)

func (a *App) previewPullAllLocal() PullPlanDTO {
	var token [16]byte
	_, _ = rand.Read(token[:])
	now := time.Now()
	// A status pass publishes in this lock order. Keep the revision, rows,
	// and network overlay from the same instant.
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg := a.refreshConfigLocked()
	plan := PullPlanDTO{
		PlanID: hex.EncodeToString(token[:]), ConfigRevision: a.configRevision,
		WillClone: []repoRef{}, WillUpdate: []PullPlanItem{}, Skipped: []PullPlanItem{},
		Offline: []repoRef{}, RepairsPending: []repoRef{},
	}
	pending := pendingPullPlan{id: plan.PlanID, expires: now.Add(pullPlanLifetime), cfg: cfg, targets: make(map[repoRef]pullTarget)}
	rows := make(map[repoRef]StatusResult, len(a.cachedStatus))
	for _, row := range a.cachedStatus {
		rows[repoRef{Source: row.Source, Repo: row.Repo}] = row
	}
	plan.Stale = a.cachedStatusConfig == nil
	for _, source := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[source]
		for _, repo := range src.OrderedRepoKeys() {
			ref := repoRef{Source: source, Repo: repo}
			key := statusRepoKey{source, repo}
			row, ok := rows[ref]
			// An unrelated config edit can advance its revision while
			// leaving a checkout's settings and local evidence intact.
			if !ok || a.cachedStatusConfig == nil || !a.sameRefreshRepoLocked(a.cachedStatusConfig, source, repo) {
				plan.Stale = true
				plan.Skipped = append(plan.Skipped, PullPlanItem{repoRef: ref, Reason: "unknown"})
				continue
			}
			check := a.statusNetwork[key]
			identity, identified := statusRepoIdentity(cfg, key)
			if identified && check.identity == identity && check.refs.After(row.sweepStarted) {
				plan.Stale = true
			}
			evidence := row.pullEvidence
			repair, knownRepair := a.pullRepairs[key]
			switch {
			case row.State == status.RootOffline.String():
				plan.Offline = append(plan.Offline, ref)
			case evidence.CloneObstructed || (identified && knownRepair && repair == identity && !evidence.CloneMissing):
				plan.RepairsPending = append(plan.RepairsPending, ref)
			case a.upstreamGone[source+"/"+repo]:
				plan.Skipped = append(plan.Skipped, PullPlanItem{repoRef: ref, Reason: "upstreamGone"})
			case evidence.CloneMissing:
				plan.WillClone = append(plan.WillClone, ref)
				pending.clones = append(pending.clones, ref)
			default:
				eligibility := pullEligibility(git.RepoStatus{Ahead: row.Ahead, Behind: row.Behind}, evidence.Blocked)
				reason := eligibility.Reason
				switch {
				case evidence.Unknown || row.Branch == "":
					reason = "unknown"
				case eligibility.Eligible:
					plan.WillUpdate = append(plan.WillUpdate, PullPlanItem{repoRef: ref, Behind: eligibility.Behind})
					pending.pulls = append(pending.pulls, ref)
					pending.targets[ref] = pullTarget{branch: row.Branch, upstream: evidence.Upstream}
					continue
				case reason != "":
				case row.Ahead > 0 && row.Behind > 0:
					reason = "diverged"
				case row.Ahead > 0:
					reason = "ahead"
				case evidence.Upstream == "":
					reason = "noUpstream"
				default:
					reason = "current"
				}
				plan.Skipped = append(plan.Skipped, PullPlanItem{repoRef: ref, Reason: reason})
			}
		}
	}
	a.pullPlans = slices.DeleteFunc(a.pullPlans, func(p pendingPullPlan) bool { return !now.Before(p.expires) })
	if len(a.pullPlans) >= maxPullPlans {
		a.pullPlans = slices.Delete(a.pullPlans, 0, 1)
	}
	a.pullPlans = append(a.pullPlans, pending)
	return plan
}

func (a *App) takePullPlan(id string) (pendingPullPlan, bool) {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	for i, p := range a.pullPlans {
		if p.id == id {
			a.pullPlans = slices.Delete(a.pullPlans, i, i+1)
			return p, time.Now().Before(p.expires)
		}
	}
	return pendingPullPlan{}, false
}
