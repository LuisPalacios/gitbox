package main

import (
	"context"
	"maps"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/provider"
)

const (
	probeMinInterval    = 60 * time.Second
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

func (a *App) snapshotProbeRevisions() upstreamProbeRevisions {
	a.mu.Lock()
	defer a.mu.Unlock()
	return upstreamProbeRevisions{maps.Clone(a.upstreamRevision), maps.Clone(a.credentialRevision)}
}

// beginUpstreamProbe shares an active sweep and throttles completed sweeps.
// I keep the network work outside both config and coordination locks.
func (a *App) beginUpstreamProbe() (*upstreamProbeFlight, bool) {
	if a.refreshContext().Err() != nil {
		return nil, false
	}
	return a.shareUpstreamProbe()
}

func (a *App) shareUpstreamProbe() (*upstreamProbeFlight, bool) {
	a.probeMu.Lock()
	defer a.probeMu.Unlock()
	if a.probeFlight != nil {
		return a.probeFlight, false
	}
	if time.Since(a.lastProbeAt) < probeMinInterval {
		return nil, false
	}
	flight := &upstreamProbeFlight{done: make(chan struct{})}
	a.probeFlight = flight
	return flight, true
}

func (a *App) finishUpstreamProbe(flight *upstreamProbeFlight) {
	ctx, cancel := context.WithTimeout(a.refreshContext(), probeSweepTimeout)
	defer cancel()
	cfg := a.snapshotRefreshConfig()
	revisions := a.snapshotProbeRevisions()
	results := runUpstreamProbe(ctx, cfg, credential.ResolveAPITokenContext, probeRequestTimeout)
	flight.changed = a.applyUpstreamProbe(cfg, results, &revisions)
	active := a.refreshContext().Err() == nil
	// Credential edits clear the throttle. I retain that invalidation if an
	// old sweep finishes after the edit, including a same-type token rotation.
	a.mu.Lock()
	a.probeMu.Lock()
	if active && maps.Equal(a.credentialRevision, revisions.credentials) {
		a.lastProbeAt = time.Now()
	}
	a.probeFlight = nil
	close(flight.done)
	a.probeMu.Unlock()
	a.mu.Unlock()
}

// probeUpstreamExistence joins an existing sweep instead of bypassing its guard.
func (a *App) probeUpstreamExistence() bool {
	flight, owner := a.beginUpstreamProbe()
	if flight == nil {
		return false
	}
	if owner {
		a.finishUpstreamProbe(flight)
	} else {
		select {
		case <-flight.done:
		case <-a.refreshContext().Done():
			return false
		}
	}
	return flight.changed
}

// probeUpstreamExistenceAsync publishes confirmed changes from the local cache.
// I avoid another git status sweep when only provider metadata changes.
func (a *App) probeUpstreamExistenceAsync() {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	if a.refreshClosed {
		return
	}
	flight, owner := a.shareUpstreamProbe()
	if !owner {
		return
	}
	a.refreshWG.Go(func() {
		a.finishUpstreamProbe(flight)
		if flight.changed {
			a.emitCachedStatus()
		}
	})
}

type upstreamProbeJob struct {
	accountKey string
	repoKey    string
	account    config.Account
	token      string
	creator    provider.RepoCreator
}

type upstreamProbeResult struct {
	accountKey string
	repoKey    string
	exists     bool
}

type probeTokenResolver func(context.Context, config.Account, string) (string, string, error)

func runUpstreamProbe(ctx context.Context, cfg *config.Config, resolveToken probeTokenResolver, requestTimeout time.Duration) []upstreamProbeResult {
	if cfg == nil || ctx.Err() != nil {
		return nil
	}
	// Multiple sources may share an account or the same upstream repository.
	// I resolve credentials once per account and probe each upstream once.
	accountRepos := make(map[string]map[string]bool)
	for _, src := range cfg.Sources {
		if accountRepos[src.Account] == nil {
			accountRepos[src.Account] = make(map[string]bool)
		}
		for repoKey := range src.Repos {
			accountRepos[src.Account][repoKey] = true
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
		for repoKey := range repoKeys {
			jobs = append(jobs, upstreamProbeJob{accountKey, repoKey, acct, token, creator})
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
				exists, err := job.creator.RepoExists(requestCtx, job.account.URL, job.token, job.account.Username, owner, name)
				// A response racing cancellation must not change gone state.
				valid := err == nil && requestCtx.Err() == nil
				cancel()
				if valid {
					resultCh <- upstreamProbeResult{job.accountKey, job.repoKey, exists}
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

func (a *App) applyUpstreamProbe(snapshot *config.Config, results []upstreamProbeResult, revisions *upstreamProbeRevisions) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if snapshot == nil || a.cfg == nil {
		return false
	}
	if !reflect.DeepEqual(a.cfg.Global.CredentialGCM, snapshot.Global.CredentialGCM) ||
		!reflect.DeepEqual(a.cfg.Global.CredentialSSH, snapshot.Global.CredentialSSH) ||
		!reflect.DeepEqual(a.cfg.Global.CredentialToken, snapshot.Global.CredentialToken) {
		return false
	}
	if a.upstreamGone == nil {
		a.upstreamGone = make(map[string]bool)
	}
	changed := false
	for _, result := range results {
		if revisions != nil && a.credentialRevision[result.accountKey] != revisions.credentials[result.accountKey] {
			continue
		}
		account, ok := a.cfg.Accounts[result.accountKey]
		if !ok || !reflect.DeepEqual(account, snapshot.Accounts[result.accountKey]) {
			continue
		}
		for sourceKey, oldSource := range snapshot.Sources {
			oldRepo, oldOK := oldSource.Repos[result.repoKey]
			source, sourceOK := a.cfg.Sources[sourceKey]
			repo, repoOK := source.Repos[result.repoKey]
			if oldSource.Account != result.accountKey || !oldOK || !sourceOK || !repoOK ||
				source.Account != oldSource.Account || source.Folder != oldSource.Folder || repo != oldRepo ||
				a.cfg.Global.Folder != snapshot.Global.Folder {
				continue
			}
			key := sourceKey + "/" + result.repoKey
			if revisions != nil && a.upstreamRevision[key] != revisions.upstream[key] {
				continue
			}
			gone := !result.exists
			if a.upstreamGone[key] != gone {
				changed = true
			}
			if gone {
				a.upstreamGone[key] = true
			} else {
				delete(a.upstreamGone, key)
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
