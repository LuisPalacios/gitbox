package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/ops"
	"github.com/LuisPalacios/gitbox/pkg/pathkey"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// pathGate serializes operations on the same physical clone.
type pathGate struct {
	mu      sync.Mutex
	entries map[string]*pathGateEntry
}

type pathGateEntry struct {
	slot chan struct{}
	refs int
}

func (g *pathGate) lock(path string) func() {
	unlock, _ := g.acquire(context.Background(), path)
	return unlock
}

func (g *pathGate) acquire(ctx context.Context, path string) (func(), error) {
	key := pathkey.Compare(path)
	g.mu.Lock()
	if g.entries == nil {
		g.entries = make(map[string]*pathGateEntry)
	}
	entry := g.entries[key]
	if entry == nil {
		entry = &pathGateEntry{slot: make(chan struct{}, 1)}
		g.entries[key] = entry
	}
	entry.refs++
	g.mu.Unlock()

	drop := func() {
		g.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(g.entries, key)
		}
		g.mu.Unlock()
	}
	select {
	case entry.slot <- struct{}{}:
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
	unlock := func() {
		<-entry.slot
		drop()
	}
	if err := ctx.Err(); err != nil {
		unlock()
		return nil, err
	}
	return unlock, nil
}

var errCloneTransferActive = errors.New("a Git operation is still using the clone or a folder within it")

// The deletion caller holds a.mu and maintenanceMu. A new transfer cannot
// pass its config check until deletion finishes. Refuse existing transfers
// without waiting on their gate while holding either lock.
func (a *App) refuseBusyDeletion(path string) error {
	g := &a.repairPaths
	target := pathkey.Compare(path)
	g.mu.Lock()
	defer g.mu.Unlock()
	for active := range g.entries {
		rel, err := filepath.Rel(target, active)
		if err == nil && filepath.IsLocal(rel) {
			return fmt.Errorf("%s: %w", path, errCloneTransferActive)
		}
	}
	return nil
}

var errCloneSettingsChanged = errors.New("repository settings changed during refresh")

// prepareClone heals under the clone gate and returns its unlock function. The
// account maintenance lock protects heal, then lets a new account revision
// persist while the old transfer finishes. Its follow-up heal must still
// wait for the clone gate.
func (a *App) prepareClone(ctx context.Context, cfg *config.Config, ref repoRef, path string, started func(), precheck func() error) (heal.Report, func(), error) {
	for {
		unlock, err := a.repairPaths.acquire(ctx, path)
		if err != nil {
			return heal.Report{}, nil, err
		}
		if !a.maintenanceMu.TryRLock() {
			// Account edits hold the write lock before taking a clone
			// gate. Yield this gate before waiting for an edit to finish.
			unlock()
			a.maintenanceMu.RLock()
			current := a.sameRefreshRepo(cfg, ref.Source, ref.Repo)
			a.maintenanceMu.RUnlock()
			if err := ctx.Err(); err != nil {
				return heal.Report{}, nil, err
			}
			if !current {
				return heal.Report{}, nil, errCloneSettingsChanged
			}
			continue
		}
		defer a.maintenanceMu.RUnlock()
		if !a.sameRefreshRepo(cfg, ref.Source, ref.Repo) {
			unlock()
			return heal.Report{}, nil, errCloneSettingsChanged
		}
		if precheck != nil {
			if err := precheck(); err != nil {
				unlock()
				return heal.Report{}, nil, err
			}
		}
		if started != nil {
			started()
		}
		return a.healLocked(ctx, cfg, ref.Source, ref.Repo), unlock, nil
	}
}

// The caller supplies a detached config for the entire repair.
func (a *App) repairClone(cfg *config.Config, source, repo string) (heal.Report, bool) {
	path := status.RepoPath(cfg, source, repo, cfg.Sources[source].Repos[repo])
	report, unlock, err := a.prepareClone(a.refreshContext(), cfg, repoRef{source, repo}, path, nil, nil)
	if err != nil {
		return report, false
	}
	unlock()
	return report, true
}

// Account edits already hold the write side of maintenanceMu. Taking its
// read side recursively would deadlock.
func (a *App) repairUnderMaintenance(cfg *config.Config, source, repo string) (heal.Report, bool) {
	path := status.RepoPath(cfg, source, repo, cfg.Sources[source].Repos[repo])
	unlock := a.repairPaths.lock(path)
	defer unlock()
	if !a.sameRefreshRepo(cfg, source, repo) {
		return heal.Report{}, false
	}
	return a.healLocked(a.refreshContext(), cfg, source, repo), true
}

func (a *App) healLocked(ctx context.Context, cfg *config.Config, source, repo string) heal.Report {
	report := a.healRepoContext(ctx, cfg, source, repo)
	key := statusRepoKey{source, repo}
	a.mu.Lock()
	if a.sameRefreshRepoLocked(cfg, source, repo) {
		if identity, ok := statusRepoIdentity(cfg, key); ok && len(report.Warnings) > 0 {
			if a.pullRepairs == nil {
				a.pullRepairs = make(map[statusRepoKey]statusIdentity)
			}
			a.pullRepairs[key] = identity
		} else {
			delete(a.pullRepairs, key)
		}
	}
	a.mu.Unlock()
	for i, warning := range report.Warnings {
		report.Warnings[i] = userText(warning)
	}
	for i, fix := range report.Fixed {
		report.Fixed[i] = userText(fix)
	}
	return report
}

// Reconfigure from a snapshot after saving the account, with maintenanceMu
// held exclusively. Other state readers may continue while git runs.
func (a *App) reconfigureClones(account string) int {
	cfg := a.snapshotRefreshConfig()
	reports := ops.ReconfigureClonesWith(cfg, account, func(cfg *config.Config, source, repo string) heal.Report {
		report, _ := a.repairUnderMaintenance(cfg, source, repo)
		return report
	})
	return len(reports)
}
