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
func (s *localSession) networkChecksFor(key statusRepoKey, identity statusIdentity) statusNetworkChecks {
	if s.statusNetwork == nil {
		s.statusNetwork = make(map[statusRepoKey]statusNetworkChecks)
	}
	check := s.statusNetwork[key]
	if check.identity != identity {
		return statusNetworkChecks{identity: identity}
	}
	return check
}

// recordFetchStatus records a definitive result for the checked clone.
func (s *localSession) recordFetchStatus(snapshot *config.Config, source, repo string, gone bool, checkedAt time.Time) {
	key := statusRepoKey{source, repo}
	expected, ok := statusRepoIdentity(snapshot, key)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if live, ok := statusRepoIdentity(s.cfg, key); !ok || live != expected {
		return
	}

	s.markUpstreamGoneLocked(source, repo, gone)
	check := s.networkChecksFor(key, expected)
	check.existence = checkedAt
	if !gone {
		check.refs = checkedAt
	}
	s.statusNetwork[key] = check
}

// markUpstreamGoneLocked updates the marker and invalidates overlapping
// provider probes. Caller holds mu.
func (s *localSession) markUpstreamGoneLocked(source, repo string, gone bool) {
	if s.upstreamGone == nil {
		s.upstreamGone = make(map[string]bool)
	}
	key := source + "/" + repo
	if s.upstreamRevision == nil {
		s.upstreamRevision = make(map[string]uint64)
	}
	s.upstreamRevision[key]++
	if gone {
		s.upstreamGone[key] = true
	} else {
		delete(s.upstreamGone, key)
	}
}

// networkCheckedAt reports which network result the row describes. Caller
// holds mu. A fetch newer than the sweep could not have informed every row
// in that sweep, so wait for the next local status pass.
func (s *localSession) networkCheckedAt(row StatusResult) string {
	check := s.statusNetwork[statusRepoKey{row.Source, row.Repo}]
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
