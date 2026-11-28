package main

import (
	"context"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/diag"
	"github.com/LuisPalacios/gitbox/pkg/heal"
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

	// refreshMu guards the refresh lifecycle and every flight below it: the
	// shared status sweep and its cache, the fetch cycle, the per-clone
	// fetches, the credential checks and their proofs, and the pull plans.
	// Take it before mu.
	refreshMu          sync.Mutex
	refreshCtx         context.Context
	refreshCancel      context.CancelFunc
	refreshClosed      bool
	refreshWG          sync.WaitGroup
	statusRun          *statusFlight
	cachedStatus       []StatusResult
	cachedStatusConfig *config.Config
	fetchRun           *fetchFlight
	repoFetches        map[string]*repoFetchFlight
	credentialRuns     map[string]*credentialFlight
	proofChecks        map[string]proofVerification
	pullPlans          []pendingPullPlan
	// refreshHooks replaces the git, credential and network work in tests.
	refreshHooks *refreshHooks
	// fetches hands out the per-host transfer slots of fetches and pulls.
	fetches fetchDispatcher

	// probeMu serializes upstream-existence probes so a burst of
	// RefreshStatus calls from the frontend doesn't spawn N concurrent
	// API sweeps. Separate from mu because probing takes seconds and
	// would block every other state read. Take mu first when both are held.
	probeMu     sync.Mutex
	probeFlight *upstreamProbeFlight
	lastProbeAt time.Time

	// network knows which servers this session reaches: see network.go.
	network networkGuard

	// app is what session code still borrows from App: see appServices.
	app appServices
}

// appServices are the App services session code still calls, until clone
// maintenance (#370) and events and the shell (#371) move into the session
// and the server builds a session without App (#372). *App implements it;
// every App constructor binds it (TestAppConstructorsAllocateLocalSession).
// Each method runs where the code on *App called it.
type appServices interface {
	// newOperation starts an event operation on the App's event stream.
	newOperation() eventOperation
	// recordActivity adds one operation to the diagnostics activity.
	recordActivity(code diag.OperationCode, started, finished time.Time, queueDepth *int, path string, err error)
	// isReadOnly reports App.readOnly: the session must not repair clones.
	isReadOnly() bool
	// prepareClone heals a clone under its gate and the maintenance lock.
	prepareClone(ctx context.Context, cfg *config.Config, ref repoRef, path string, started func(), precheck func() error) (heal.Report, func(), error)
	// maintenanceLock is the account maintenance lock PR workers read under.
	maintenanceLock() *sync.RWMutex
	// fetchAllRepos is the backend's FetchAllRepos, which the network
	// watcher starts when a server comes back.
	fetchAllRepos() error
	// reconcileServices are the services a reconcile pass still calls.
	reconcileServices() reconcileServices
}

// newLocalSession allocates a session bound to the App services of a.
func newLocalSession(a *App) *localSession { return &localSession{app: a} }

func (a *App) recordActivity(code diag.OperationCode, started, finished time.Time, queueDepth *int, path string, err error) {
	a.diagnostics.Record(code, started, finished, queueDepth, path, err)
}

func (a *App) isReadOnly() bool { return a.readOnly }

func (a *App) maintenanceLock() *sync.RWMutex { return &a.maintenanceMu }
