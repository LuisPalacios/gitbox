package main

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/diag"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

const (
	probeSweepTimeout   = 30 * time.Second
	probeRequestTimeout = 10 * time.Second
	probeConcurrency    = 4
)

type upstreamProbeFlight struct {
	done    chan struct{}
	changed bool
}

type upstreamProbeRevisions struct {
	upstream    map[string]uint64
	credentials map[string]uint64
}

func (s *localSession) snapshotProbeRevisions() upstreamProbeRevisions {
	s.mu.Lock()
	defer s.mu.Unlock()
	return upstreamProbeRevisions{maps.Clone(s.upstreamRevision), maps.Clone(s.credentialRevision)}
}

// beginUpstreamProbe shares an active sweep and throttles completed sweeps.
// I keep the network work outside both config and coordination locks.
func (s *localSession) beginUpstreamProbe() (*upstreamProbeFlight, bool) {
	if s.refreshContext().Err() != nil {
		return nil, false
	}
	return s.shareUpstreamProbe()
}

func (s *localSession) shareUpstreamProbe() (*upstreamProbeFlight, bool) {
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	if s.probeFlight != nil {
		return s.probeFlight, false
	}
	if time.Since(s.lastProbeAt) < probeMinInterval {
		return nil, false
	}
	flight := &upstreamProbeFlight{done: make(chan struct{})}
	s.probeFlight = flight
	return flight, true
}

func (s *localSession) finishUpstreamProbe(flight *upstreamProbeFlight) {
	ctx, cancel := context.WithTimeout(s.refreshContext(), probeSweepTimeout)
	defer cancel()
	cfg := s.snapshotRefreshConfig()
	revisions := s.snapshotProbeRevisions()
	// An account whose server is unreachable is left out of the sweep: its
	// requests would only time out.
	resolve := func(ctx context.Context, acct config.Account, key string) (string, string, error) {
		if !s.reachable(ctx, serverAddr(acct)) {
			return "", "", errServerUnreachable
		}
		return credential.ResolveAPITokenContext(ctx, acct, key)
	}
	results := s.runUpstreamProbe(ctx, cfg, resolve, probeRequestTimeout)
	flight.changed = s.applyUpstreamProbe(cfg, results, &revisions)
	active := s.refreshContext().Err() == nil
	// Credential edits clear the throttle. I retain that invalidation if an
	// old sweep finishes after the edit, including a same-type token rotation.
	s.mu.Lock()
	s.probeMu.Lock()
	if active && maps.Equal(s.credentialRevision, revisions.credentials) {
		s.lastProbeAt = time.Now()
	}
	s.probeFlight = nil
	close(flight.done)
	s.probeMu.Unlock()
	s.mu.Unlock()
}

// probeUpstreamExistence joins an existing sweep instead of bypassing its guard.
func (s *localSession) probeUpstreamExistence() bool {
	flight, owner := s.beginUpstreamProbe()
	if flight == nil {
		return false
	}
	if owner {
		s.finishUpstreamProbe(flight)
	} else {
		select {
		case <-flight.done:
		case <-s.refreshContext().Done():
			return false
		}
	}
	return flight.changed
}

// probeUpstreamExistenceAsync publishes confirmed changes from the local cache.
// I avoid another git status sweep when only provider metadata changes.
func (s *localSession) probeUpstreamExistenceAsync() {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.refreshClosed {
		return
	}
	flight, owner := s.shareUpstreamProbe()
	if !owner {
		return
	}
	s.refreshWG.Go(func() {
		s.finishUpstreamProbe(flight)
		if flight.changed {
			s.emitCachedStatus()
		}
	})
}

type upstreamProbeJob struct {
	accountKey string
	repoKey    string
	paths      []string
	account    config.Account
	token      string
	creator    provider.RepoCreator
}

type upstreamProbeResult struct {
	accountKey string
	repoKey    string
	exists     bool
	checkedAt  time.Time
}

type probeTokenResolver func(context.Context, config.Account, string) (string, string, error)

func (s *localSession) runUpstreamProbe(ctx context.Context, cfg *config.Config, resolveToken probeTokenResolver, requestTimeout time.Duration) []upstreamProbeResult {
	if cfg == nil || ctx.Err() != nil {
		return nil
	}
	// Multiple sources may share an account or the same upstream repository.
	// I resolve credentials once per account and probe each upstream once.
	accountRepos := make(map[string]map[string][]string)
	for sourceKey, src := range cfg.Sources {
		if accountRepos[src.Account] == nil {
			accountRepos[src.Account] = make(map[string][]string)
		}
		for repoKey, repo := range src.Repos {
			path := status.RepoPath(cfg, sourceKey, repoKey, repo)
			accountRepos[src.Account][repoKey] = append(accountRepos[src.Account][repoKey], path)
		}
	}
	var jobs []upstreamProbeJob
	for accountKey, repoKeys := range accountRepos {
		if ctx.Err() != nil {
			break
		}
		acct, ok := cfg.Accounts[accountKey]
		if !ok || len(repoKeys) == 0 {
			continue
		}
		prov, err := provider.ByName(acct.Provider)
		if err != nil {
			continue
		}
		creator, ok := prov.(provider.RepoCreator)
		if !ok {
			continue
		}
		token, _, err := resolveToken(ctx, acct, accountKey)
		if err != nil {
			continue
		}
		for repoKey, paths := range repoKeys {
			jobs = append(jobs, upstreamProbeJob{accountKey, repoKey, paths, acct, token, creator})
		}
	}
	queue := make(chan upstreamProbeJob)
	resultCh := make(chan upstreamProbeResult, len(jobs))
	var workers sync.WaitGroup
	for range min(probeConcurrency, len(jobs)) {
		workers.Go(func() {
			for job := range queue {
				if ctx.Err() != nil {
					return
				}
				owner, name := splitRepoKeyForProbe(job.repoKey)
				requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
				started := time.Now()
				exists, err := job.creator.RepoExists(requestCtx, job.account.URL, job.token, job.account.Username, owner, name)
				finished := time.Now()
				// A response racing cancellation must not change gone state.
				valid := err == nil && requestCtx.Err() == nil
				cancel()
				if err == nil {
					s.app.recordActivity(diag.RepoProbe, started, finished, nil, "", nil)
				} else {
					for _, path := range job.paths {
						s.app.recordActivity(diag.RepoProbe, started, finished, nil, path, err)
					}
				}
				if valid {
					resultCh <- upstreamProbeResult{job.accountKey, job.repoKey, exists, time.Now()}
				}
			}
		})
	}
dispatch:
	for _, job := range jobs {
		select {
		case queue <- job:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(queue)
	workers.Wait()
	close(resultCh)
	results := make([]upstreamProbeResult, 0, len(jobs))
	for result := range resultCh {
		results = append(results, result)
	}
	return results
}

func (s *localSession) applyUpstreamProbe(snapshot *config.Config, results []upstreamProbeResult, revisions *upstreamProbeRevisions) bool {
	if snapshot == nil || len(results) == 0 {
		return false
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == nil {
		return false
	}
	if !reflect.DeepEqual(s.cfg.Global.CredentialGCM, snapshot.Global.CredentialGCM) ||
		!reflect.DeepEqual(s.cfg.Global.CredentialSSH, snapshot.Global.CredentialSSH) ||
		!reflect.DeepEqual(s.cfg.Global.CredentialToken, snapshot.Global.CredentialToken) {
		return false
	}
	if s.upstreamGone == nil {
		s.upstreamGone = make(map[string]bool)
	}
	// Compare timestamp changes on the same local status cache the
	// asynchronous probe will publish.
	cachedRows := make(map[statusRepoKey]StatusResult)
	if reflect.DeepEqual(s.cachedStatusConfig, snapshot) {
		for _, row := range s.cachedStatus {
			cachedRows[statusRepoKey{row.Source, row.Repo}] = row
		}
	}
	changed := false
	for _, result := range results {
		if revisions != nil && s.credentialRevision[result.accountKey] != revisions.credentials[result.accountKey] {
			continue
		}
		account, ok := s.cfg.Accounts[result.accountKey]
		if !ok || !reflect.DeepEqual(account, snapshot.Accounts[result.accountKey]) {
			continue
		}
		for sourceKey, oldSource := range snapshot.Sources {
			oldRepo, oldOK := oldSource.Repos[result.repoKey]
			source, sourceOK := s.cfg.Sources[sourceKey]
			repo, repoOK := source.Repos[result.repoKey]
			if oldSource.Account != result.accountKey || !oldOK || !sourceOK || !repoOK ||
				source.Account != oldSource.Account || source.Folder != oldSource.Folder || source.Root != oldSource.Root || repo != oldRepo ||
				!slices.Equal(s.cfg.Global.Roots, snapshot.Global.Roots) {
				continue
			}
			key := sourceKey + "/" + result.repoKey
			if revisions != nil && s.upstreamRevision[key] != revisions.upstream[key] {
				continue
			}
			gone := !result.exists
			if s.upstreamGone[key] != gone {
				changed = true
			}
			checkKey := statusRepoKey{sourceKey, result.repoKey}
			if identity, ok := statusRepoIdentity(s.cfg, checkKey); ok && !result.checkedAt.IsZero() {
				check := s.networkChecksFor(checkKey, identity)
				if result.checkedAt.After(check.existence) {
					row, cached := cachedRows[checkKey]
					if gone {
						row.State = status.UpstreamGone.String()
					}
					before := s.networkCheckedAt(row)
					check.existence = result.checkedAt
					s.statusNetwork[checkKey] = check
					changed = changed || (cached && before != s.networkCheckedAt(row))
				}
			}
			if gone {
				s.upstreamGone[key] = true
			} else {
				delete(s.upstreamGone, key)
			}
		}
	}
	return changed
}

func splitRepoKeyForProbe(repoKey string) (string, string) {
	if before, after, ok := strings.Cut(repoKey, "/"); ok {
		return before, after
	}
	return "", repoKey
}
