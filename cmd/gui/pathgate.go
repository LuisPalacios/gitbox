package main

import (
	"sync"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/ops"
	"github.com/LuisPalacios/gitbox/pkg/pathkey"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// pathGate queues repairs of the same physical clone. Each caller waits
// for its predecessor and wakes its successor when it releases the path.
type pathGate struct {
	mu    sync.Mutex
	tails map[string]chan struct{}
}

func (g *pathGate) lock(path string) func() {
	key := pathkey.Compare(path)
	done := make(chan struct{})
	g.mu.Lock()
	if g.tails == nil {
		g.tails = make(map[string]chan struct{})
	}
	previous := g.tails[key]
	g.tails[key] = done
	g.mu.Unlock()
	if previous != nil {
		<-previous
	}
	return func() {
		g.mu.Lock()
		close(done)
		if g.tails[key] == done {
			delete(g.tails, key)
		}
		g.mu.Unlock()
	}
}

// Lock order: account maintenance, clone path, then the short config check.
// The caller supplies a detached config for the entire repair.
func (a *App) repairClone(cfg *config.Config, source, repo string) (heal.Report, bool) {
	a.maintenanceMu.RLock()
	defer a.maintenanceMu.RUnlock()
	return a.repairUnderMaintenance(cfg, source, repo, nil)
}

// Account edits already hold the write side of maintenanceMu. Taking its
// read side recursively would deadlock.
func (a *App) repairUnderMaintenance(cfg *config.Config, source, repo string, started func()) (heal.Report, bool) {
	path := status.RepoPath(cfg, source, repo, cfg.Sources[source].Repos[repo])
	unlock := a.repairPaths.lock(path)
	defer unlock()
	if !a.sameRefreshRepo(cfg, source, repo) {
		return heal.Report{}, false
	}
	if started != nil {
		started()
	}
	report := a.healRepo(cfg, source, repo)
	for i, warning := range report.Warnings {
		report.Warnings[i] = userText(warning)
	}
	for i, fix := range report.Fixed {
		report.Fixed[i] = userText(fix)
	}
	return report, true
}

// Reconfigure from a snapshot after saving the account, with maintenanceMu
// held exclusively. Other state readers may continue while git runs.
func (a *App) reconfigureClones(account string) int {
	cfg := a.snapshotRefreshConfig()
	reports := ops.ReconfigureClonesWith(cfg, account, func(cfg *config.Config, source, repo string) heal.Report {
		report, _ := a.repairUnderMaintenance(cfg, source, repo, nil)
		return report
	})
	return len(reports)
}
