package main

import (
	"sync"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/reconcile"
	"github.com/LuisPalacios/gitbox/pkg/workspace"
)

// localSession holds the state of the machine this process runs on: the
// config core and every field mu guards (#353). App embeds it, so the fields
// are promoted and the receivers stay on *App until they move here. No field
// name may appear in both structs, or App's would shadow the promoted one
// (TestAppAndLocalSessionShareNoFieldNames).
type localSession struct {
	// mu guards the config core and every field below it.
	mu      sync.Mutex
	cfg     *config.Config
	cfgPath string
	// cfgLoaded is true if the config was loaded from disk (safe to save back).
	cfgLoaded bool
	// cfgLoadError is non-empty if the config exists but failed to parse.
	cfgLoadError string
	// cfgBase identifies the config bytes observed at the last load or save.
	cfgBase        config.FileRevision
	configRevision uint64

	sweepPreview       *pendingSweep
	credentialFindings []credential.Finding // cached metadata-only audit
	reconcilePending   bool                 // one asynchronous stale-plan retry

	// upstreamGone tracks repos discovered to be missing from the provider
	// (deleted, archived private, lost access). Populated by fetch errors
	// matching git.IsUpstreamGoneError and by provider.RepoExists probes.
	// Keyed by "<sourceKey>/<repoKey>". Cleared when a fetch succeeds or
	// a RepoExists probe returns true. In-memory only — we re-discover on
	// the next periodic sync rather than persist a potentially-stale fact.
	upstreamGone       map[string]bool
	upstreamRevision   map[string]uint64
	credentialRevision map[string]uint64
	statusNetwork      map[statusRepoKey]statusNetworkChecks
	pullRepairs        map[statusRepoKey]statusIdentity
	prs                prRefreshState

	// Cache collision checks for the configuration shown in Needs attention.
	pathAuditBase config.FileRevision
	pathConflicts []config.PathConflict
	pathsAudited  bool

	// reconcileMu serializes reconcile passes and manual adoptions (#149)
	// and guards the workspace state below. Take it before mu.
	// reconcileScan and reconcileClone replace reconcile.Plan and
	// Config.Clone in tests.
	reconcileMu      sync.Mutex
	reconcileScan    func(*config.Config) (reconcile.Result, error)
	reconcileClone   func(*config.Config) (*config.Config, error)
	workspaceSync    workspace.SyncState // guarded by reconcileMu
	workspaceUnknown []string            // guarded by reconcileMu
}
