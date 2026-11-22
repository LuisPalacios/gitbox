package main

import (
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

type statusRepoKey struct {
	source string
	repo   string
}

type statusIdentity struct {
	path     string
	account  string
	provider string
	server   string
	username string
}

type statusNetworkChecks struct {
	identity  statusIdentity
	refs      time.Time
	existence time.Time
}

func statusRepoIdentity(cfg *config.Config, key statusRepoKey) (statusIdentity, bool) {
	if cfg == nil {
		return statusIdentity{}, false
	}
	source, sourceOK := cfg.Sources[key.source]
	repo, repoOK := source.Repos[key.repo]
	account, accountOK := cfg.Accounts[source.Account]
	if !sourceOK || !repoOK || !accountOK {
		return statusIdentity{}, false
	}
	return statusIdentity{
		path:     status.RepoPath(cfg, key.source, key.repo, repo),
		account:  source.Account,
		provider: account.Provider,
		server:   account.URL,
		username: account.Username,
	}, true
}

// networkChecksFor returns the checks for a live identity. Caller holds mu.
func (a *App) networkChecksFor(key statusRepoKey, identity statusIdentity) statusNetworkChecks {
	if a.statusNetwork == nil {
		a.statusNetwork = make(map[statusRepoKey]statusNetworkChecks)
	}
	check := a.statusNetwork[key]
	if check.identity != identity {
		return statusNetworkChecks{identity: identity}
	}
	return check
}

// recordFetchStatus records a definitive result for the checked clone.
func (a *App) recordFetchStatus(snapshot *config.Config, source, repo string, gone bool, checkedAt time.Time) {
	key := statusRepoKey{source, repo}
	expected, ok := statusRepoIdentity(snapshot, key)
	if !ok {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if live, ok := statusRepoIdentity(a.cfg, key); !ok || live != expected {
		return
	}

	a.markUpstreamGoneLocked(source, repo, gone)
	check := a.networkChecksFor(key, expected)
	check.existence = checkedAt
	if !gone {
		check.refs = checkedAt
	}
	a.statusNetwork[key] = check
}

// markUpstreamGoneLocked updates the marker and invalidates overlapping
// provider probes. Caller holds mu.
func (a *App) markUpstreamGoneLocked(source, repo string, gone bool) {
	if a.upstreamGone == nil {
		a.upstreamGone = make(map[string]bool)
	}
	key := source + "/" + repo
	if a.upstreamRevision == nil {
		a.upstreamRevision = make(map[string]uint64)
	}
	a.upstreamRevision[key]++
	if gone {
		a.upstreamGone[key] = true
	} else {
		delete(a.upstreamGone, key)
	}
}

// networkCheckedAt reports which network result the row describes. Caller
// holds mu. A fetch newer than the sweep could not have informed every row
// in that sweep, so wait for the next local status pass.
func (a *App) networkCheckedAt(row StatusResult) string {
	check := a.statusNetwork[statusRepoKey{row.Source, row.Repo}]
	if check.identity.path != row.Path || check.identity.account != row.Account {
		return ""
	}

	var at time.Time
	switch row.State {
	case status.Behind.String(), status.Diverged.String():
		if !check.refs.After(row.sweepStarted) {
			at = check.refs
		}
	case status.NotCloned.String(), status.UpstreamGone.String():
		at = check.existence
	}
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}
