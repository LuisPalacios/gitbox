package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"

	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/diag"
	"github.com/LuisPalacios/gitbox/pkg/doctor"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/gitignore"
	"github.com/LuisPalacios/gitbox/pkg/harness"
	"github.com/LuisPalacios/gitbox/pkg/i18n"
	"github.com/LuisPalacios/gitbox/pkg/identity"
	"github.com/LuisPalacios/gitbox/pkg/mirror"
	"github.com/LuisPalacios/gitbox/pkg/ops"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/LuisPalacios/gitbox/pkg/reconcile"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/LuisPalacios/gitbox/pkg/terminals"
	"github.com/LuisPalacios/gitbox/pkg/update"
	"github.com/LuisPalacios/gitbox/pkg/workspace"
)

// App is the Wails application struct. All exported methods become
// frontend bindings via window.go.main.App.<Method>().
type App struct {
	sweepPreview      *pendingSweep // guarded by mu
	ctx               context.Context
	recoveryMu        sync.Mutex
	updateRecovery    *update.Recovery
	updateRecoveryErr error
	recoveryExpiry    *time.Timer
	// events carries every event to the frontend; nil means nopSink (see
	// events.go). startup installs the Wails sink.
	events      EventSink
	eventStream eventStream
	// backend does the work; nil means localBackend{a} (see backend.go).
	backend Backend
	// prefs holds the client preferences; nil means sharedPrefs{a}, the
	// client keys of cfg (see prefs.go).
	prefs prefsStore
	// cfgBase identifies the config bytes observed at the last load or save.
	// Guarded by mu.
	cfgBase        config.FileRevision
	configRevision uint64
	timing         startupTiming

	diagnostics        diag.Activity
	diagnosticsMu      sync.Mutex
	diagnosticsPreview string // guarded by diagnosticsMu
	// readOnly makes an App that changes nothing it reads: saveConfig
	// refuses and fetch skips the repair of a clone's .git/config. No
	// gitbox-server sets it since the mutations are served (#294, #295);
	// the contract tests set it on the side that answers only reads, to
	// prove a read never saves.
	readOnly bool
	// hostLog is where a gitbox-server writes for a human; nil elsewhere.
	hostLog io.Writer
	// remoteAlias names the remote this window shows; empty is the local
	// window. It selects where the window geometry is stored.
	remoteAlias string
	// remote is the connection of a remote window (remote.go); nil in the
	// local window.
	remote *remoteSession
	// checkAlias replaces ssh -G in tests; nil runs it.
	checkAlias func(ctx context.Context, alias string) error
	// windowGeom reads the window position and size; nil asks Wails. Tests
	// set it to run without a Wails runtime.
	windowGeom         func() (x, y, w, h int, ok bool)
	cfg                *config.Config
	cfgPath            string
	cfgLoaded          bool // true if config was loaded from disk (safe to save back)
	credentialAuditMu  sync.Mutex
	credentialFindings []credential.Finding // cached metadata-only audit, guarded by mu
	cfgLoadError       string               // non-empty if config exists but failed to parse
	testFirstRun       bool                 // explicitly empty test fixture for the first-launch wizard
	testMode           bool                 // true when launched with --test-mode
	testCleanup        func()               // cleanup function for test-mode temp dir
	// windowMode selects which UI the frontend should render. Empty (or
	// "main") = the full gitbox app. "terminals" = the standalone Profile
	// editor sub-process spawned by OpenTerminalsManagerWindow (issue #69
	// — keeps the editor in its own OS window so the parent stays
	// interactive and the editor scrolls on its own).
	windowMode string

	// remotesIdle ends the Remote hosts window's process once it has been
	// hidden for remotesIdleTimeout (#330); see touchRemotesWindow.
	remotesIdleMu sync.Mutex
	remotesIdle   *time.Timer

	// terminalsCmd tracks the Profile-editor subprocess spawned by
	// OpenTerminalsManagerWindow (issue #69). Captured so the parent's
	// shutdown can tear it down — otherwise the editor window survives the
	// main app and an orphaned binary keeps holding the
	// com.luispalacios.gitbox.terminals SingleInstanceLock, which would
	// confuse a fresh launch. Guarded by terminalsMu to keep concurrent
	// click handlers safe.
	terminalsMu  sync.Mutex
	terminalsCmd *exec.Cmd

	toolsOnce sync.Once
	toolsWG   sync.WaitGroup
	// reconcileMu serializes reconcile passes and manual adoptions (#149);
	// reconcileScan replaces reconcile.Plan in tests.
	reconcileMu    sync.Mutex
	reconcileScan  func(*config.Config) (reconcile.Result, error)
	reconcileClone func(*config.Config) (*config.Config, error)
	// discoverMirrors replaces mirror.DiscoverMirrors in tests.
	discoverMirrors  func(context.Context, *config.Config, func(mirror.DiscoverProgress)) ([]mirror.DiscoveryResult, error)
	workspaceSync    workspace.SyncState // guarded by reconcileMu
	workspaceUnknown []string            // guarded by reconcileMu
	reconcilePending bool                // guarded by mu; one asynchronous stale-plan retry
	mu               sync.Mutex
	savedWindowPos   *config.WindowState // full-mode window state pre-loaded from config
	savedCompactPos  *config.WindowState // compact-mode window state pre-loaded from config
	savedViewMode    string              // "full" or "compact" pre-loaded from config

	// upstreamGone tracks repos discovered to be missing from the provider
	// (deleted, archived private, lost access). Populated by fetch errors
	// matching git.IsUpstreamGoneError and by provider.RepoExists probes.
	// Keyed by "<sourceKey>/<repoKey>". Cleared when a fetch succeeds or
	// a RepoExists probe returns true. In-memory only — we re-discover on
	// the next periodic sync rather than persist a potentially-stale fact.
	upstreamGone       map[string]bool
	upstreamRevision   map[string]uint64
	statusNetwork      map[statusRepoKey]statusNetworkChecks // guarded by mu
	pullRepairs        map[statusRepoKey]statusIdentity      // guarded by mu
	credentialRevision map[string]uint64
	proofChecks        map[string]proofVerification // guarded by refreshMu
	prs                prRefreshState

	// probeMu serializes upstream-existence probes so a burst of
	// RefreshStatus calls from the frontend doesn't spawn N concurrent
	// API sweeps. Separate from a.mu because probing takes seconds and
	// would block every other state read.
	probeMu       sync.Mutex
	probeFlight   *upstreamProbeFlight
	lastProbeAt   time.Time
	maintenanceMu sync.RWMutex
	repairPaths   pathGate
	refreshMu     sync.Mutex
	// network knows which servers are reachable: see network.go.
	network            networkGuard
	refreshCtx         context.Context
	refreshCancel      context.CancelFunc
	refreshClosed      bool
	refreshWG          sync.WaitGroup
	refreshHooks       *refreshHooks
	statusRun          *statusFlight
	cachedStatus       []StatusResult
	cachedStatusConfig *config.Config
	fetchRun           *fetchFlight
	fetches            fetchDispatcher
	repoFetches        map[string]*repoFetchFlight
	credentialRuns     map[string]*credentialFlight

	pullPlans []pendingPullPlan // guarded by refreshMu

	// Cache collision checks for the configuration shown in Needs attention.
	pathAuditBase config.FileRevision
	pathConflicts []config.PathConflict
	pathsAudited  bool

	// tasks owns the periodic checks for this window or headless host.
	tasks             taskRegistry
	windowTasksOnce   sync.Once
	taskPrefsMu       sync.Mutex
	harnessMu         sync.Mutex
	lastHarnessSyncAt time.Time
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{upstreamGone: make(map[string]bool), timing: startupTiming{start: time.Now()}}
}

// ─── Lifecycle ────────────────────────────────────────────────

// initLocal loads the config and the credential audit. It is the local
// backend's Init: the GUI calls it from startup, gitbox-server from main.
func (a *App) initLocal(ctx context.Context) {
	a.ctx = ctx
	if a.cfgPath == "" {
		a.cfgPath = config.DefaultV2Path()
	}
	cfg, _, base, err := config.LoadRevision(a.cfgPath, false)
	a.setRevision(base)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			// Config exists but failed to parse — store error for the frontend to display.
			a.cfgLoadError = fmt.Sprintf("Failed to load %s:\n%v", a.cfgPath, err)
			fmt.Fprintf(os.Stderr, "warning: %s\n", a.cfgLoadError)
		}
		// Config doesn't exist or is unreadable — start with empty config for onboarding.
		cfg = &config.Config{
			Schema:   "https://raw.githubusercontent.com/LuisPalacios/gitbox/main/json/gitbox.schema.json",
			Version:  config.CurrentVersion,
			Accounts: make(map[string]config.Account),
			Sources:  make(map[string]config.Source),
		}
	} else {
		a.cfgLoaded = true
		a.timing.mark("decode")
		if a.testFirstRun {
			cfg.Global.Roots = nil
			a.testFirstRun = false
		}
	}
	a.cfg = cfg
	a.refreshCredentialAudit()
}

// closeLocal stops the local backend's background workers.
func (a *App) closeLocal() {
	a.tasks.stop()
	a.stopRefresh()
	a.tasks.wg.Wait()
}

// saveConfig persists the in-memory config to disk, with the client
// preferences that are on disk (mergeDiskPrefs). A conflicting file revision
// reloads the file and returns config.ChangedError. It refuses to save on a
// read-only server, and when the config was not successfully loaded — the
// guard prevents the empty default fabricated in startup() from being
// written over a real file we failed to parse, which would destroy the
// user's data (see issue #60).
//
// Caller must hold a.mu.
func (a *App) saveConfig() error {
	if err := a.saveRefused(); err != nil {
		return err
	}
	h, err := a.cfgBase.Save(a.cfg, a.cfgPath, a.mergeDiskPrefs)
	if err != nil {
		if isConfigChanged(err) {
			a.reloadAfterConflict()
		}
		return err
	}
	a.setRevision(h)
	return nil
}

// isConfigChanged reports whether a save was refused because another
// process saved the config meanwhile (#241).
func isConfigChanged(err error) bool {
	_, ok := errors.AsType[*config.ChangedError](err)
	return ok
}

// reloadAfterConflict replaces the config in memory with the file, keeping
// the pointer, after a save was refused (#241): the change that conflicted
// is dropped, and the window shows what the other process saved. If the file no longer loads,
// clear the pending mutation and block edits until recovery. Caller holds a.mu.
func (a *App) reloadAfterConflict() {
	fresh, _, base, err := config.LoadRevision(a.cfgPath, false)
	a.setRevision(base)
	if err != nil {
		a.cfgLoaded = false
		a.cfgLoadError = userMsg(err)
		*a.cfg = config.Config{Version: config.CurrentVersion, Accounts: make(map[string]config.Account), Sources: make(map[string]config.Source)}
		return
	}
	a.cfgLoaded, a.cfgLoadError = true, ""
	a.refreshPRConfigLocked(fresh)
	*a.cfg = *fresh
}

// commitConfig applies change to the live config and saves it through
// saveConfig, all under a.mu, and returns a copy of the result (#173). It is
// how work that ran on a snapshot outside the lock writes its outcome: the
// change re-checks what it needs against the live config. A failing change
// or a refused save restores the config as it was, keeping the pointer.
func (a *App) commitConfig(change func(*config.Config) error) (*config.Config, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.saveRefused(); err != nil {
		return nil, err
	}
	before, err := a.cfg.Clone()
	if err != nil {
		return nil, err
	}
	if err := change(a.cfg); err != nil {
		*a.cfg = *before
		return nil, err
	}
	if err := a.saveConfig(); err != nil {
		if !isConfigChanged(err) {
			// A conflict already reloaded the file; anything else restores.
			*a.cfg = *before
		}
		return nil, err
	}
	return a.cfg.Clone()
}

// snapshotLocked is a copy of the config for work that runs after a.mu is
// released (#173): nothing may keep the live pointer, or a map inside it,
// once the lock is gone. A copy that cannot be made (the config does not
// encode) is logged and replaced by an empty config, so the work finds
// nothing instead of touching live state. Caller holds a.mu.
func (a *App) snapshotLocked() *config.Config {
	snap, err := a.cfg.Clone()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: config snapshot failed: %v\n", err)
		return &config.Config{Accounts: map[string]config.Account{}, Sources: map[string]config.Source{}, Mirrors: map[string]config.Mirror{}}
	}
	return snap
}

// savePrefs changes the client preferences and saves (#240). It starts
// from the preferences on disk, not from the ones in memory, so it changes
// only what change changes. When saving is refused, the change still
// applies in memory and the refusal is returned. Caller holds a.mu.
func (a *App) savePrefs(change func(*config.ClientPrefs) error) error {
	apply := func() error {
		p := config.ExtractPrefs(a.cfg)
		if err := change(&p); err != nil {
			return err
		}
		config.ApplyPrefs(a.cfg, p)
		return nil
	}
	if refused := a.saveRefused(); refused != nil {
		if err := apply(); err != nil {
			return err
		}
		return refused
	}
	save := func() (config.FileRevision, error) {
		return a.cfgBase.Save(a.cfg, a.cfgPath, func(onDisk *config.Config) error {
			_ = a.mergeDiskPrefs(onDisk)
			return apply()
		})
	}
	h, err := save()
	if err != nil {
		var ce *config.ChangedError
		if !errors.As(err, &ce) || ce.Locked {
			return err
		}
		// Another process saved a domain change meanwhile. A preference
		// change does not touch the domain, so it applies on top of the
		// file once reloaded (#241).
		a.reloadAfterConflict()
		if !a.cfgLoaded {
			return err
		}
		if h, err = save(); err != nil {
			if isConfigChanged(err) {
				a.reloadAfterConflict()
			}
			return err
		}
	}
	a.setRevision(h)
	return nil
}

// saveRefused is why saveConfig would refuse, or nil. Caller holds a.mu.
func (a *App) saveRefused() error {
	if a.readOnly {
		return &remoteError{remoteReadOnly, "the remote is read-only: nothing is saved on the host"}
	}
	if !a.cfgLoaded {
		if a.cfgLoadError != "" {
			return fmt.Errorf("refusing to save: config not loaded (%s)", a.cfgLoadError)
		}
		return fmt.Errorf("refusing to save: config not loaded")
	}
	return nil
}

// mergeDiskPrefs keeps client settings saved by other windows. Caller holds a.mu.
func (a *App) mergeDiskPrefs(onDisk *config.Config) error {
	if onDisk != nil {
		config.PreserveClientPrefs(a.cfg, onDisk)
	}
	return nil
}

// ─── Update ──────────────────────────────────────────────────

// UpdateInfo is the frontend-friendly update check result.
type UpdateInfo struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`
	// SelfUpdate is false for builds that only notify (the AppImage):
	// the frontend then opens URL instead of calling ApplyUpdate.
	SelfUpdate bool `json:"selfUpdate"`
}

func (a *App) updateOpts() update.Options {
	return update.Options{
		CurrentVersion: update.ResolveVersion(version),
		Repo:           "LuisPalacios/gitbox",
	}
}

// checkForUpdate emits "update:available" if a newer version exists.
// Network errors and rate limits stay silent: no pill, no error.
func (a *App) checkForUpdate(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	result, err := update.CheckLatest(ctx, a.updateOpts())
	if err != nil || !result.Available {
		return err
	}

	info := UpdateInfo{
		Available:  true,
		Current:    result.Current,
		Latest:     result.Latest,
		SelfUpdate: update.SelfUpdateSupported(),
	}
	if result.Release != nil {
		info.URL = result.Release.HTMLURL
	}
	emit(a.newOperation(), "update:available", info)
	return nil
}

// GetOS returns the GOOS of the machine that shows the window, for its
// shortcut labels and wording: "darwin", "windows", "linux". The host's own
// is StartupSnapshot.ServerOS; Backend.GetOS is what a server answers.
func (a *App) GetOS() string { return a.getOSLocal() }

// remoteWindow reports whether this window shows another host.
func (a *App) remoteWindow() bool {
	_, ok := a.be().(*RemoteBackend)
	return ok
}

// getOSLocal is the local backend's GetOS.
func (a *App) getOSLocal() string {
	return runtime.GOOS
}

// getAppVersion formats the version of this application build.
func (a *App) getAppVersion() string {
	v, c := version, commit

	// If not set by ldflags, try to detect from git at runtime.
	if v == "dev" {
		// git.Inherit keeps Homebrew dirs on PATH for macOS.
		if res, err := git.Run(context.Background(), git.Request{Args: []string{"describe", "--tags", "--always"}, Env: git.Inherit}); err == nil {
			if tag := strings.TrimSpace(string(res.Stdout)); tag != "" {
				v = tag + "-dev"
			}
		}
	}
	if c == "none" {
		if res, err := git.Run(context.Background(), git.Request{Args: []string{"rev-parse", "--short", "HEAD"}, Env: git.Inherit}); err == nil {
			if sha := strings.TrimSpace(string(res.Stdout)); sha != "" {
				c = sha
			}
		}
	}

	return formatAppVersion(v, c)
}

func formatAppVersion(v, c string) string {
	if v == "dev" {
		return fmt.Sprintf("dev-%s", c)
	}
	return fmt.Sprintf("%s (%s)", v, c[:minInt(7, len(c))])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// setRevision advances the status snapshot when saved domain data changes.
func (a *App) setRevision(next config.FileRevision) {
	if !a.cfgBase.SameDomain(next) {
		a.eventStream.mu.Lock()
		a.configRevision++
		a.eventStream.revision = a.configRevision
		a.eventStream.mu.Unlock()
	}
	a.cfgBase = next
}

// ─── Config ───────────────────────────────────────────────────

// ConfigDTO is the JSON-friendly config sent to the frontend.
type ConfigDTO struct {
	Revision uint64                    `json:"revision"`
	Version  int                       `json:"version"`
	Global   config.GlobalConfig       `json:"global"`
	Accounts map[string]config.Account `json:"accounts"`
	Sources  map[string]SourceDTO      `json:"sources"`
	Mirrors  map[string]MirrorDTO      `json:"mirrors"`

	// PathConflicts lists configured entries that share a folder on this host.
	PathConflicts []config.PathConflict `json:"pathConflicts,omitempty"`
}

// SourceDTO mirrors config.Source but exposes repos as a map.
type SourceDTO struct {
	Account   string                 `json:"account"`
	Folder    string                 `json:"folder,omitempty"`
	Repos     map[string]config.Repo `json:"repos"`
	RepoOrder []string               `json:"repoOrder"`
}

// MirrorDTO exposes a mirror group to the frontend.
type MirrorDTO struct {
	AccountSrc string                       `json:"account_src"`
	AccountDst string                       `json:"account_dst"`
	Repos      map[string]config.MirrorRepo `json:"repos"`
	RepoOrder  []string                     `json:"repoOrder"`
}

// MirrorStatusResult is the per-repo live mirror status sent to the frontend.
type MirrorStatusResult struct {
	MirrorKey  string `json:"mirrorKey"`
	RepoKey    string `json:"repoKey"`
	Direction  string `json:"direction"`
	OriginAcct string `json:"originAcct"`
	BackupAcct string `json:"backupAcct"`
	SyncStatus string `json:"syncStatus"`
	HeadCommit string `json:"headCommit"`
	OriginHead string `json:"originHead"`
	BackupHead string `json:"backupHead"`
	Warning    string `json:"warning"`
	NeedsSetup bool   `json:"needsSetup"`
	Error      string `json:"error"`
}

// MirrorSetupResult is sent to the frontend after a mirror setup attempt.
type MirrorSetupResult struct {
	MirrorKey    string `json:"mirrorKey"`
	RepoKey      string `json:"repoKey"`
	Created      bool   `json:"created"`
	Mirrored     bool   `json:"mirrored"`
	Method       string `json:"method"`
	Instructions string `json:"instructions"`
	Error        string `json:"error"`
}

// MirrorCredentialCheck reports whether an account has tokens needed for mirrors.
type MirrorCredentialCheck struct {
	AccountKey     string `json:"accountKey"`
	HasMirrorToken bool   `json:"hasMirrorToken"`
	NeedsPAT       bool   `json:"needsPat"`
	Message        string `json:"message"`
}

// GetConfig returns the full configuration for the frontend.
func (a *App) GetConfig() (ConfigDTO, error) {
	dto, err := a.be().GetConfig()
	if err != nil {
		return ConfigDTO{}, err
	}
	a.overlayPrefs(&dto)
	return dto, nil
}

// getConfigLocal is the local backend's GetConfig.
func (a *App) getConfigLocal() (ConfigDTO, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.configDTO()
}

// configDTO detaches the response from mutable config before Wails serializes it.
// Caller holds a.mu.
func (a *App) configDTO() (ConfigDTO, error) {
	if !a.pathsAudited || a.pathAuditBase != a.cfgBase {
		a.auditClonePaths()
	}
	sources := make(map[string]SourceDTO, len(a.cfg.Sources))
	for key, src := range a.cfg.Sources {
		sources[key] = SourceDTO{Account: src.Account, Folder: src.Folder, Repos: src.Repos, RepoOrder: src.OrderedRepoKeys()}
	}
	dto := ConfigDTO{Revision: a.configRevision, Version: a.cfg.Version, Global: a.cfg.Global, Accounts: a.cfg.Accounts,
		Sources: sources, Mirrors: buildMirrorsDTO(a.cfg)}
	dto.PathConflicts = a.pathConflicts
	data, err := json.Marshal(dto)
	if err != nil {
		return ConfigDTO{}, fmt.Errorf("copying config: %w", err)
	}
	var detached ConfigDTO
	if err := json.Unmarshal(data, &detached); err != nil {
		return ConfigDTO{}, fmt.Errorf("copying config: %w", err)
	}
	return detached, nil
}

// StartupSnapshot contains only information available without disk scanning,
// subprocesses or provider requests.
type StartupSnapshot struct {
	EventWatermark     uint64               `json:"eventWatermark"`
	Config             ConfigDTO            `json:"config"`
	Nesting            map[string]string    `json:"nesting"`
	ConfigPath         string               `json:"configPath"`
	AppVersion         string               `json:"appVersion"`
	OS                 string               `json:"os"`
	CredentialFindings []credential.Finding `json:"credentialFindings"`
	// OS is the system of the machine that shows the window, ServerOS that
	// of the host whose data it shows. Both are runtime.GOOS in a local
	// window.
	ServerOS string `json:"serverOS"`
	// ServerVersion is the build of the gitbox-server this window talks
	// to, as it stated it in the handshake. Empty in a local window.
	ServerVersion string `json:"serverVersion,omitempty"`
	// RemoteAlias is the remote this window shows. Empty in a local window.
	RemoteAlias string `json:"remoteAlias,omitempty"`
}

// GetStartupSnapshot keeps preferences, configured rows and nesting consistent
// in the first frontend update. Pending status never delays this response.
func (a *App) GetStartupSnapshot() (StartupSnapshot, error) {
	snap, err := a.be().GetStartupSnapshot()
	if err != nil {
		return StartupSnapshot{}, err
	}
	a.overlayPrefs(&snap.Config)
	a.syncTaskInterval()
	snap.ServerOS = snap.OS
	if remote, ok := a.be().(*RemoteBackend); ok {
		snap.AppVersion = a.getAppVersion()
		snap.ServerVersion = remote.ServerVersion()
		snap.ServerOS = identifier(snap.OS)
		snap.OS = a.getOSLocal()
	}
	a.timing.mark("snapshot")
	snap.RemoteAlias = a.remoteAlias
	return snap, nil
}

// getStartupSnapshotLocal is the local backend's GetStartupSnapshot.
func (a *App) getStartupSnapshotLocal() (StartupSnapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cfg, err := a.configDTO()
	if err != nil {
		return StartupSnapshot{}, err
	}
	nesting := make(map[string]string)
	for child, parent := range status.ComputeNesting(a.cfg).ParentOf {
		nesting[child.Source+"/"+child.Repo] = parent.Source + "/" + parent.Repo
	}
	a.eventStream.mu.Lock()
	watermark := a.eventStream.sequence
	a.eventStream.mu.Unlock()
	return StartupSnapshot{EventWatermark: watermark, Config: cfg, Nesting: nesting, ConfigPath: a.cfgPath, AppVersion: formatAppVersion(version, commit), OS: runtime.GOOS, CredentialFindings: slices.Clone(a.credentialFindings)}, nil
}

// ReloadConfig re-reads the config from disk. Call after external edits
// or when the window regains focus.
func (a *App) ReloadConfig() (ConfigDTO, error) {
	dto, err := a.be().ReloadConfig()
	if err != nil {
		return ConfigDTO{}, err
	}
	// A store with its own file re-reads it too; the shared one was just
	// reloaded with the backend's config.
	if r, ok := a.clientPrefs().(interface{ Reload() }); ok {
		r.Reload()
	}
	a.overlayPrefs(&dto)
	a.syncTaskInterval()
	return dto, nil
}

// reloadConfigLocal is the local backend's ReloadConfig.
func (a *App) reloadConfigLocal() (ConfigDTO, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfg, _, base, err := config.LoadRevision(a.cfgPath, false)
	if err != nil {
		return ConfigDTO{}, fmt.Errorf("reloading config: %w", err)
	}
	a.refreshPRConfigLocked(cfg)
	a.cfg = cfg
	a.setRevision(base)
	a.cfgLoaded, a.cfgLoadError = true, ""
	a.pathsAudited = false

	return a.configDTO()
}

// auditClonePaths records changes to the collisions visible in Needs attention.
func (a *App) auditClonePaths() {
	conflicts := a.cfg.PathConflicts()
	if len(conflicts) > 0 && !slices.Equal(conflicts, a.pathConflicts) {
		out := a.hostLog
		if out == nil {
			out = os.Stderr
		}
		for _, conflict := range conflicts {
			fmt.Fprintf(out, "warning: folder collision between %q and %q; see Needs attention\n", userText(conflict.First), userText(conflict.Second))
		}
	}
	a.pathConflicts, a.pathAuditBase, a.pathsAudited = conflicts, a.cfgBase, true
}

func buildMirrorsDTO(cfg *config.Config) map[string]MirrorDTO {
	mirrors := make(map[string]MirrorDTO, len(cfg.Mirrors))
	for key, m := range cfg.Mirrors {
		mirrors[key] = MirrorDTO{
			AccountSrc: m.AccountSrc,
			AccountDst: m.AccountDst,
			Repos:      m.Repos,
			RepoOrder:  m.OrderedRepoKeys(),
		}
	}
	return mirrors
}

// GetConfigPath returns the path to the config file.
func (a *App) GetConfigPath() string { return a.be().GetConfigPath() }

// getConfigPathLocal is the local backend's GetConfigPath.
func (a *App) getConfigPathLocal() string {
	return a.cfgPath
}

// OpenFileInEditor opens a file with the OS default application.
// On Windows we route through `cmd /c start`; HideWindow hides the cmd.exe
// host without affecting the grandchild GUI (start → ShellExecute spawns
// the target with its own STARTUPINFO, so SW_HIDE does not cascade).
//
// A remote window refuses it: the file is the host's (#243).
func (a *App) OpenFileInEditor(path string) error {
	if err := a.refuseInRemote(); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch {
	case isWindows():
		cmd = exec.Command("cmd", "/c", "start", "", path)
		git.HideWindow(cmd)
	case isDarwin():
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// openRepoGitConfig opens an extensionless Git config as text.
func (a *App) openRepoGitConfig(configPath string) error {
	var cmd *exec.Cmd
	switch {
	case isWindows():
		cmd = exec.Command("notepad.exe", configPath)
	case isDarwin():
		cmd = exec.Command("open", "-t", configPath)
	default:
		return a.OpenFileInEditor(configPath)
	}
	git.HideWindow(cmd)
	return cmd.Start()
}

// OpenInExplorer reveals a folder in the OS file manager. A remote window
// refuses it: there is no local file manager for the host (#243).
func (a *App) OpenInExplorer(path string) error {
	if err := a.refuseInRemote(); err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch {
	case isWindows():
		native := filepath.FromSlash(path)
		cmd = exec.Command("explorer", native)
	case isDarwin():
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// OpenInBrowser opens a link in the default browser through the funnel
// (external.go): https at once, http only when confirmed, nothing else. An
// unconfirmed http link comes back as NeedsConfirm, and the frontend asks.
func (a *App) OpenInBrowser(url string, confirmed bool) (ExternalOpenDTO, error) {
	return openExternalDTO(url, confirmed)
}

// resolveAccountFolder returns the absolute path to the account's parent
// folder: <root>/<source folder> of the account's source (the one keyed like
// the account, else the first in order). Returns an error if the account key
// is unknown or the folder does not exist on disk.
func (a *App) resolveAccountFolder(accountKey string) (string, error) {
	a.mu.Lock()
	_, ok := a.cfg.Accounts[accountKey]
	path := accountFolder(a.cfg, accountKey)
	a.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("account %q not found", accountKey)
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("account folder does not exist: %s", path)
		}
		return "", fmt.Errorf("account folder %s: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("account folder is not a directory: %s", path)
	}
	return path, nil
}

// OpenAccountFolder reveals the account's parent folder in the OS file
// manager. Errors if the folder does not exist yet (no repos cloned). A
// remote window refuses it, like OpenInExplorer.
func (a *App) OpenAccountFolder(accountKey string) error {
	if err := a.refuseInRemote(); err != nil {
		return err
	}
	return a.be().OpenAccountFolder(accountKey)
}

// accountFolderLocal is the local backend's AccountFolder.
func (a *App) accountFolderLocal(accountKey string) (string, error) {
	return a.resolveAccountFolder(accountKey)
}

// openAccountFolderLocal is the local backend's OpenAccountFolder.
func (a *App) openAccountFolderLocal(accountKey string) error {
	path, err := a.resolveAccountFolder(accountKey)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	switch {
	case isWindows():
		native := filepath.FromSlash(path)
		cmd = exec.Command("explorer", native)
	case isDarwin():
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// OpenAccountInApp opens the account's parent folder in a specific editor.
// In a remote window the host resolves the folder and this computer opens
// it in an editor that supports remote folders (#243).
func (a *App) OpenAccountInApp(accountKey string, command string) error {
	if a.remoteWindow() {
		dir, err := a.remoteAccountFolder(accountKey)
		if err != nil {
			return err
		}
		return a.openRemoteEditor(dir, command)
	}
	return a.be().OpenAccountInApp(accountKey, command)
}

// openAccountInAppLocal is the local backend's OpenAccountInApp.
func (a *App) openAccountInAppLocal(accountKey string, command string) error {
	if command == "" {
		return fmt.Errorf("command is required")
	}
	path, err := a.resolveAccountFolder(accountKey)
	if err != nil {
		return err
	}
	cmd := exec.Command(command, path)
	cmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
	git.HideWindow(cmd)
	return cmd.Start()
}

// OpenAccountInBrowser opens the provider profile page for the account. The
// backend, which may be on another host, resolves the link; this machine
// opens it through the funnel.
func (a *App) OpenAccountInBrowser(accountKey string, confirmed bool) (ExternalOpenDTO, error) {
	url, err := a.be().OpenAccountInBrowser(accountKey)
	if err != nil {
		return ExternalOpenDTO{}, err
	}
	return openExternalDTO(url, confirmed)
}

// openAccountInBrowserLocal is the local backend's OpenAccountInBrowser: it
// resolves the link and opens nothing.
func (a *App) openAccountInBrowserLocal(accountKey string) (string, error) {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("account %q not found", accountKey)
	}
	return git.AccountProfileURL(acct.URL, acct.Username), nil
}

// repoWebURL resolves the provider web page for a configured repo from the
// authoritative in-memory config: <account url>/<owner>/<name>, the same
// shape git.RepoWebURL produces (#79).
func (a *App) repoWebURL(sourceKey, repoKey string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	src, ok := a.cfg.Sources[sourceKey]
	if !ok {
		return "", fmt.Errorf("source %q not found", sourceKey)
	}
	if _, ok := src.Repos[repoKey]; !ok {
		return "", fmt.Errorf("repo %q not found in source %q", repoKey, sourceKey)
	}
	acct, ok := a.cfg.Accounts[src.Account]
	if !ok {
		return "", fmt.Errorf("account %q not found", src.Account)
	}
	return git.RepoWebURL(acct.URL, repoKey), nil
}

// OpenRepoInBrowser opens the provider page for a configured repo. The URL
// is resolved by the backend so a stale frontend store can never turn the
// action into a silent no-op; lookup failures come back as errors the UI
// shows in a dialog. This machine opens the link through the funnel.
func (a *App) OpenRepoInBrowser(sourceKey, repoKey string, confirmed bool) (ExternalOpenDTO, error) {
	url, err := a.be().OpenRepoInBrowser(sourceKey, repoKey)
	if err != nil {
		return ExternalOpenDTO{}, err
	}
	return openExternalDTO(url, confirmed)
}

// openRepoInBrowserLocal is the local backend's OpenRepoInBrowser: it
// resolves the link and opens nothing.
func (a *App) openRepoInBrowserLocal(sourceKey, repoKey string) (string, error) {
	return a.repoWebURL(sourceKey, repoKey)
}

// resolveRepoPath looks up a repo's local path from config. Returns ("", error) if not found.
func (a *App) resolveRepoPath(sourceKey, repoKey string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	src, ok := a.cfg.Sources[sourceKey]
	if !ok {
		return "", fmt.Errorf("source %q not found", sourceKey)
	}
	repo, ok := src.Repos[repoKey]
	if !ok {
		return "", fmt.Errorf("repo %q not found", repoKey)
	}
	return status.RepoPath(a.cfg, sourceKey, repoKey, repo), nil
}

// OpenInApp opens a folder in a specific application (e.g. VS Code).
// On Windows, editor launchers like code.cmd/cursor.cmd are batch wrappers;
// HideWindow prevents the hosting cmd.exe from flashing before the GUI spawns.
//
// In a remote window the path is the host's: an editor that supports remote
// folders opens it over ssh, and any other is refused (#243).
func (a *App) OpenInApp(path string, command string) error {
	if command == "" {
		return fmt.Errorf("command is required")
	}
	if a.remoteWindow() {
		return a.openRemoteEditor(path, command)
	}
	return startEditor([]string{command, path})
}

// startEditor starts an editor's argv, with the Homebrew-augmented PATH on
// macOS and no console window on Windows.
func startEditor(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = git.Environ() // Homebrew PATH for macOS — do not remove.
	git.HideWindow(cmd)
	return cmd.Start()
}

// lookPathWithBrewPATH resolves a command using the Homebrew-augmented PATH.
// On macOS, GUI apps inherit a minimal PATH that excludes /opt/homebrew/bin
// and /usr/local/bin, so editors installed via Homebrew (code, cursor, zed)
// are invisible to exec.LookPath. This helper sets the augmented env for the
// lookup, falling back to the standard LookPath on non-macOS platforms.
//
// When PATH still misses, macOS and Linux also probe ~/.local/bin: the
// native Claude Code and Antigravity CLI installers drop their binaries
// there, and a GUI launched from the Dock or a desktop menu doesn't inherit
// the shell rc that adds it to PATH. I use the shared resolver without
// changing the process environment while background detection runs.
func lookPathWithBrewPATH(command string) (string, error) {
	if path := doctor.Lookup(command); path != "" {
		return path, nil
	}
	return "", &exec.Error{Name: command, Err: exec.ErrNotFound}
}

// knownEditors lists editors to auto-detect on PATH.
//
// Cursor is deliberately absent — it's detected as an AI harness via
// pkg/harness/tools-directory.md so it surfaces under the AI tools menu
// instead of the editor menu. Same rationale for Windsurf and Cline.
// Editor entries whose name collides with an AI harness name are pruned
// on every syncEditors run (see the claimedByHarness set below).
var knownEditors = []config.EditorEntry{
	{Name: "VS Code", Command: "code"},
	{Name: "Zed", Command: "zed"},
}

// syncDetected merges detected editors and terminal profiles into the config
// after the first render. syncProfiles fills the terminal apps, shells
// and profiles that the Terminals manager and the launcher menus render; it
// saves only when something changed.
func (a *App) syncDetected() {
	a.syncEditors()
	a.syncProfiles()
}

// syncEditors merges detected editors into the config's global.editors array.
// - Deduplicates existing entries by name (keeps the first occurrence)
// - Appends any detected editor not already present (matched by name)
// - Writes the full resolved path so users see a concrete example
// - Saves config only if changes were made
func (a *App) syncEditors() {
	detected := make([]config.EditorEntry, 0, len(knownEditors))
	for _, known := range knownEditors {
		if fullPath, err := lookPathWithBrewPATH(known.Command); err == nil {
			detected = append(detected, config.EditorEntry{Name: known.Name, Command: fullPath})
		}
	}
	store := a.clientPrefs()
	if _, changed := mergeEditors(store.Get().Editors, detected); !changed {
		return
	}
	_ = store.Update(func(p *config.ClientPrefs) {
		p.Editors, _ = mergeEditors(p.Editors, detected)
	})
}

// mergeEditors returns current with duplicates and harness-claimed names
// removed and the missing detected editors appended, and whether that
// differs from current.
func mergeEditors(current, detected []config.EditorEntry) (merged []config.EditorEntry, changed bool) {
	// Names claimed by AI harnesses (Cursor, Windsurf, Cline, …). Any editor
	// entry with one of these names is pruned — those tools surface under
	// the AI harness menu instead, and duplicating them as editors was
	// confusing (see #23 feedback).
	claimedByHarness := make(map[string]bool, len(knownAIHarnesses))
	for _, h := range knownAIHarnesses {
		claimedByHarness[h.Name] = true
	}

	// Step 1: deduplicate existing entries by name (keep first occurrence),
	// dropping any entry whose name is claimed by the harness list.
	seenName := make(map[string]bool)
	for _, e := range current {
		if claimedByHarness[e.Name] {
			continue
		}
		if seenName[e.Name] {
			continue
		}
		seenName[e.Name] = true
		merged = append(merged, e)
	}

	changed = len(merged) != len(current)

	// Step 2: append any detected editor not already present. Skip any
	// candidate whose name overlaps the harness list — defence in depth;
	// knownEditors already excludes them, but new contributors editing the
	// directory shouldn't have to remember to also edit knownEditors.
	for _, editor := range detected {
		if claimedByHarness[editor.Name] || seenName[editor.Name] {
			continue
		}
		merged = append(merged, editor)
		seenName[editor.Name] = true
		changed = true
	}
	if !changed {
		return current, false
	}
	return merged, true
}

// sanitizeWindowsTerminalEnv scrubs MSYS / Git-Bash artefacts from the env
// before handing it to a child terminal. When the GUI itself is launched
// from Git Bash (dev workflow), env vars like LOCALAPPDATA, APPDATA, TEMP
// etc. are inherited in posix form ("/c/Users/..."), which breaks Windows-
// native tools like oh-my-posh in the spawned shell. Production launches
// (from Explorer / Start Menu) are unaffected — sanitisation is a no-op on
// a clean env.
//
// The drop list also covers Unix-convention env vars that native Windows
// shells never set (HOME, SHELL, OSTYPE, HOSTNAME, LOGNAME) plus MinGW /
// Git-Bash-specific scaffolding (MINGW_CHOST, MINGW_PACKAGE_PREFIX,
// MINGW_PREFIX, EXEPATH, MSYSCON). Their mere presence is what oh-my-posh
// uses to detect "I'm running in a Unix-ish shell" and switch into the
// code path that invokes `cygpath` to canonicalize paths. Since `cygpath`
// lives in Git's `usr\bin` (not on the standard Windows PATH), every such
// detection then fails with
//
//	Failed to convert Cygwin path due to exec:
//	cygpath: executable file not found in %PATH%
//
// at $PROFILE time. Dropping those vars restores Windows-native behaviour
// regardless of how gitbox itself was started. TERM / TERM_PROGRAM are
// intentionally NOT dropped — host terminals (WezTerm, WT, …) set them on
// the spawned pane after creation, and that's legitimate.
func sanitizeWindowsTerminalEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		key, val, ok := strings.Cut(e, "=")
		if !ok {
			out = append(out, e)
			continue
		}
		switch strings.ToUpper(key) {
		case
			// MSYS / MSYS2 markers.
			"MSYSTEM", "MSYS", "MSYS2_PATH_TYPE", "MSYS_NO_PATHCONV",
			"MSYSCON",
			// Unix-convention env vars Windows-native shells never set.
			"HOME", "SHELL", "OSTYPE", "HOSTNAME", "LOGNAME",
			// MinGW / Git-Bash scaffolding.
			"MINGW_CHOST", "MINGW_PACKAGE_PREFIX", "MINGW_PREFIX",
			"EXEPATH":
			continue
		case "LOCALAPPDATA", "APPDATA", "USERPROFILE", "HOMEPATH",
			"TEMP", "TMP", "PROGRAMFILES", "PROGRAMDATA":
			val = msysToWindowsPath(val)
		}
		out = append(out, key+"="+val)
	}
	return out
}

// msysToWindowsPath converts a single MSYS path ("/c/Users/foo") to Windows
// form ("C:\\Users\\foo"). Values already in Windows form or not matching
// the /<drive>/... shape are returned unchanged.
func msysToWindowsPath(p string) string {
	if len(p) < 2 || p[0] != '/' {
		return p
	}
	c := p[1]
	if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
		return p
	}
	if len(p) > 2 && p[2] != '/' {
		return p
	}
	rest := ""
	if len(p) > 2 {
		rest = p[2:]
	}
	return strings.ToUpper(string(c)) + ":" + strings.ReplaceAll(rest, "/", `\`)
}

// ─── AI Harnesses ──────────────────────────────────────────────────────────

// AIHarnessInfo describes an available AI CLI harness for the frontend.
type AIHarnessInfo struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// knownAIHarnesses is the embedded catalog of auto-detectable harnesses,
// read once at process start from pkg/harness/tools-directory.md. The
// GUI only needs the names here (syncEditors uses them to
// keep Cursor out of the editor list); detection itself lives in
// harness.Sync.
//
// To add or remove an auto-detected harness, edit
// pkg/harness/tools-directory.md rather than this file.
var knownAIHarnesses = harness.KnownTools()

// harnessLookupFn resolves a harness command to a path. Package-level so
// tests can swap in a deterministic fake instead of probing the real host.
var harnessLookupFn harness.LookupFunc = harness.DefaultLookup

// syncAIHarnesses reconciles config's global.ai_harnesses with the embedded
// catalog and the host (see pkg/harness.Sync for the rules: catalog order,
// missing flag instead of deletion, user entries untouched). The host probe
// runs outside a.mu on a snapshot so PATH lookups never block other state
// reads; the result is applied only if nobody replaced the list meanwhile
// (ReloadConfig / RepairConfig / RestoreFromBackup swap a.cfg wholesale —
// in that case the next pass simply re-syncs the new config). Returns
// whether config changed and was saved.
func (a *App) syncAIHarnesses() bool { return a.be().SyncAIHarnesses() }

// syncAIHarnessesLocal is the local backend's SyncAIHarnesses.
func (a *App) syncAIHarnessesLocal() bool {
	a.mu.Lock()
	if a.cfg == nil {
		a.mu.Unlock()
		return false
	}
	snapshot := cloneHarnesses(a.cfg.Global.AIHarnesses)
	a.mu.Unlock()

	result, changed := harness.Sync(snapshot, harnessLookupFn)
	if !changed {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg == nil {
		return false
	}
	if !harness.Equal(a.cfg.Global.AIHarnesses, snapshot) {
		return false // config replaced while probing; retry next pass
	}
	a.cfg.Global.AIHarnesses = result
	_ = a.saveConfig()
	return true
}

// cloneHarnesses deep-copies a harness list (Args included) so the probe
// can run on it without holding a.mu.
func cloneHarnesses(in []config.AIHarnessEntry) []config.AIHarnessEntry {
	if in == nil {
		return nil
	}
	out := make([]config.AIHarnessEntry, len(in))
	for i, h := range in {
		out[i] = h
		if len(h.Args) > 0 {
			out[i].Args = append([]string(nil), h.Args...)
		}
	}
	return out
}

// detectHarnesses publishes a changed catalog after the host probe finishes.
func (a *App) detectHarnesses(ctx context.Context) error {
	a.harnessMu.Lock()
	defer a.harnessMu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	changed := a.syncAIHarnesses()
	a.mu.Lock()
	a.lastHarnessSyncAt = time.Now()
	var list []config.AIHarnessEntry
	if changed && a.cfg != nil {
		list = cloneHarnesses(a.cfg.Global.AIHarnesses)
	}
	a.mu.Unlock()
	if changed && ctx.Err() == nil {
		emit(a.newOperation(), "harnesses:updated", list)
	}
	return ctx.Err()
}

// RefreshAIHarnesses rechecks the host catalog on focus, subject to a cooldown.
func (a *App) RefreshAIHarnesses() { a.be().RefreshAIHarnesses() }

func (a *App) refreshAIHarnessesLocal() {
	a.mu.Lock()
	due := time.Since(a.lastHarnessSyncAt) >= harnessMinInterval
	a.mu.Unlock()
	if due {
		a.goTracked(func() {
			if _, err := a.tasks.runNow(taskHarness); errors.Is(err, errTaskStopped) && a.refreshContext().Err() == nil {
				// A host without a periodic worker (e.g. a backend embedded on its own)
				// can still honor an explicit refresh.
				_ = a.detectHarnesses(a.refreshContext())
			}
		})
	}
}

// buildHarnessArgv returns the harness argv: the harness binary followed by
// its args. An empty command returns nil so the profile resolver treats the
// request as a plain launch instead of silently misfiring.
func buildHarnessArgv(command string, args []string) []string {
	if command == "" {
		return nil
	}
	argv := make([]string, 0, 1+len(args))
	argv = append(argv, command)
	argv = append(argv, args...)
	return argv
}

// harnessProfileID picks the Terminal Profile that hosts AI harness launches:
// the launcher's default profile (first visible Default, else first visible),
// so "Open with <harness>" opens in the same terminal as ">_ Open in
// <default profile>". Errors when no profile exists at all.
func (a *App) harnessProfileID() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg == nil {
		return "", errors.New("configure a terminal profile first (Settings → Terminals → Manager)")
	}
	p, ok := terminals.DefaultLaunchProfile(a.cfg.Global)
	if !ok {
		return "", errors.New("configure a terminal profile first (Settings → Terminals → Manager)")
	}
	return p.ID, nil
}

// OpenInAIHarness launches the given AI harness in the clone folder inside
// the default Terminal Profile's shell (issue #80). Takes the harness
// command + args directly (rather than looking up by ID) so both
// auto-detected and config-stored entries call the same contract. Errors are
// actionable strings the frontend can surface verbatim.
//
// In a remote window the harness runs on the host, in the folder, through a
// local terminal that runs ssh -t; only a catalog harness runs (#243).
func (a *App) OpenInAIHarness(path string, command string, args []string) error {
	if command == "" {
		return fmt.Errorf("AI harness command is required")
	}
	if a.remoteWindow() {
		return a.openRemoteHarness(path, command, args)
	}
	profileID, err := a.harnessProfileID()
	if err != nil {
		return err
	}
	return a.launchProfile(path, profileID, buildHarnessArgv(command, args))
}

// OpenAccountInAIHarness launches the given AI harness in the account's
// parent folder (<global.folder>/<accountKey>) inside the default Terminal
// Profile's shell. Errors out when the account is unknown or the folder is
// missing, matching OpenAccountProfile.
func (a *App) OpenAccountInAIHarness(accountKey string, command string, args []string) error {
	if a.remoteWindow() {
		if command == "" {
			return fmt.Errorf("AI harness command is required")
		}
		dir, err := a.remoteAccountFolder(accountKey)
		if err != nil {
			return err
		}
		return a.openRemoteHarness(dir, command, args)
	}
	return a.be().OpenAccountInAIHarness(accountKey, command, args)
}

// openAccountInAIHarnessLocal is the local backend's OpenAccountInAIHarness.
func (a *App) openAccountInAIHarnessLocal(accountKey string, command string, args []string) error {
	if command == "" {
		return fmt.Errorf("AI harness command is required")
	}
	path, err := a.resolveAccountFolder(accountKey)
	if err != nil {
		return err
	}
	profileID, err := a.harnessProfileID()
	if err != nil {
		return err
	}
	return a.launchProfile(path, profileID, buildHarnessArgv(command, args))
}

// ─── Platform helpers ─────────────────────────────────────────────────────

func isWindows() bool {
	return os.PathSeparator == '\\' || strings.Contains(strings.ToLower(os.Getenv("OS")), "windows")
}

func isDarwin() bool {
	if strings.Contains(strings.ToLower(os.Getenv("OSTYPE")), "darwin") {
		return true
	}
	cmd := exec.Command("uname")
	git.HideWindow(cmd)
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "Darwin"
}

// ─── First run ────────────────────────────────────────────────

// IsFirstRun returns true if no root is configured (fresh install).
func (a *App) IsFirstRun() bool { return a.be().IsFirstRun() }

// isFirstRunLocal is the local backend's IsFirstRun.
func (a *App) isFirstRunLocal() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.cfg.Global.Roots) == 0
}

// GetConfigLoadError returns a non-empty string if the config file existed but
// failed to parse. The frontend should show this to the user and offer
// Repair / Start fresh / Exit. Returns "" if config loaded successfully or
// didn't exist.
func (a *App) GetConfigLoadError() string { return a.be().GetConfigLoadError() }

// getConfigLoadErrorLocal is the local backend's GetConfigLoadError.
func (a *App) getConfigLoadErrorLocal() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfgLoadError
}

// AcknowledgeConfigError clears the pending cfgLoadError so the user can
// proceed through onboarding and overwrite the broken file. Called when the
// user chooses "Start fresh" from the corruption modal — the backup taken
// on the next Save preserves the pre-overwrite copy.
func (a *App) AcknowledgeConfigError() { a.be().AcknowledgeConfigError() }

// acknowledgeConfigErrorLocal is the local backend's AcknowledgeConfigError.
func (a *App) acknowledgeConfigErrorLocal() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cfgLoadError = ""
}

// ConfigRepairResult is the frontend-friendly outcome of a RepairConfig call.
// On success, the in-memory config has been replaced with the repaired copy
// and saved back to disk (the existing Save() path writes a dated backup
// first, so the pre-repair file is preserved).
type ConfigRepairResult struct {
	Success bool     `json:"success"`
	Error   string   `json:"error,omitempty"`
	Repairs []string `json:"repairs,omitempty"`
}

// RepairConfig runs LoadWithRepair against the on-disk config, adopts the
// repaired copy into the live App state, and saves it back. Intended as the
// user-confirmed "Auto-repair" action when startup detected cfgLoadError —
// drops dangling references and keeps the rest of the file intact. Use
// RestoreFromBackup instead when the file is unrecoverable and a pre-
// corruption backup is available.
//
// The normal Save() backup sweep covers the pre-repair file, so users who
// regret the repair can roll back manually from ~/.config/gitbox/gitbox-*.json.
func (a *App) RepairConfig() ConfigRepairResult { return a.be().RepairConfig() }

// repairConfigLocal is the local backend's RepairConfig.
func (a *App) repairConfigLocal() ConfigRepairResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.readOnly {
		return ConfigRepairResult{Success: false, Error: userMsg(a.saveRefused())}
	}

	cfg, repairs, base, err := config.LoadRevision(a.cfgPath, true)
	if err != nil {
		return ConfigRepairResult{Success: false, Error: userMsg(err)}
	}
	if err := a.saveReplacement(cfg, base); err != nil {
		return ConfigRepairResult{Success: false, Error: userMsg(err)}
	}

	details := make([]string, 0, len(repairs))
	for _, r := range repairs {
		details = append(details, r.Detail)
	}
	return ConfigRepairResult{Success: true, Repairs: details}
}

// ConfigBackupInfo describes one dated backup beside the active gitbox.json.
// Timestamp is RFC3339 parsed from the filename (empty if the filename
// doesn't match the Save() naming pattern).
type ConfigBackupInfo struct {
	Path      string `json:"path"`
	Filename  string `json:"filename"`
	Timestamp string `json:"timestamp"`
	SizeBytes int64  `json:"size_bytes"`
}

// ListConfigBackups returns the dated backup files beside gitbox.json,
// newest first, including numbered same-second snapshots from Save.
// Safe to call while the config
// is unloaded — reads only filesystem state.
func (a *App) ListConfigBackups() []ConfigBackupInfo { return a.be().ListConfigBackups() }

// listConfigBackupsLocal is the local backend's ListConfigBackups.
// listedBackup reports whether path is one of the backups of cfgPath,
// matched by name in the config folder.
func listedBackup(cfgPath, path string) bool {
	backups, err := config.ListBackups(cfgPath)
	if err != nil {
		return false
	}
	for _, b := range backups {
		if filepath.Base(b.Path) == filepath.Base(path) && filepath.Dir(b.Path) == filepath.Dir(path) {
			return true
		}
	}
	return false
}

func (a *App) listConfigBackupsLocal() []ConfigBackupInfo {
	a.mu.Lock()
	cfgPath := a.cfgPath
	a.mu.Unlock()

	backups, err := config.ListBackups(cfgPath)
	if err != nil {
		return []ConfigBackupInfo{}
	}
	infos := make([]ConfigBackupInfo, 0, len(backups))
	for _, backup := range backups {
		fi, err := os.Stat(backup.Path)
		if err != nil || fi.IsDir() {
			continue
		}
		infos = append(infos, ConfigBackupInfo{
			Path:      backup.Path,
			Filename:  filepath.Base(backup.Path),
			Timestamp: backup.Timestamp.Format(time.RFC3339),
			SizeBytes: fi.Size(),
		})
	}
	return infos
}

// RestoreFromBackup replaces the active gitbox.json with the selected
// backup. Validates the backup loads cleanly first so we never overwrite
// with another broken file. The current (broken) file is snapshot-backed
// up by config.Save's backupBeforeSave pass, so the user can still roll
// back a wrong restore manually.
//
// The backup path must live in the same directory as gitbox.json; any
// other path is rejected to prevent a rogue frontend from pointing at an
// arbitrary file.
func (a *App) RestoreFromBackup(backupPath string) ConfigRepairResult {
	return a.be().RestoreFromBackup(backupPath)
}

// restoreFromBackupLocal is the local backend's RestoreFromBackup.
func (a *App) restoreFromBackupLocal(backupPath string) ConfigRepairResult {
	a.mu.Lock()
	defer a.mu.Unlock()

	cfgDir := filepath.Dir(a.cfgPath)
	absBackup, err := filepath.Abs(backupPath)
	if err != nil {
		return ConfigRepairResult{Success: false, Error: userText(fmt.Sprintf("resolving backup path: %v", err))}
	}
	if filepath.Dir(absBackup) != cfgDir {
		return ConfigRepairResult{Success: false, Error: fmt.Sprintf("backup must live under %s", cfgDir)}
	}
	if absBackup == a.cfgPath {
		return ConfigRepairResult{Success: false, Error: "cannot restore the active config onto itself"}
	}
	// Only a file the backup list names is restored (#241).
	if !listedBackup(a.cfgPath, absBackup) {
		return ConfigRepairResult{Success: false, Error: "that file is not one of the config backups"}
	}

	// Strict-validate the backup. Refuse to adopt another broken file.
	cfg, err := config.Load(absBackup)
	if err != nil {
		return ConfigRepairResult{Success: false, Error: fmt.Sprintf("backup %s is also invalid: %v", filepath.Base(absBackup), err)}
	}

	if err := a.saveReplacement(cfg, a.cfgBase); err != nil {
		return ConfigRepairResult{Success: false, Error: userMsg(err)}
	}

	return ConfigRepairResult{
		Success: true,
		Repairs: []string{fmt.Sprintf("Restored from %s", filepath.Base(absBackup))},
	}
}

// saveReplacement adopts a repair or restore only once the file it replaces
// has been checked and the replacement written. Caller holds a.mu.
func (a *App) saveReplacement(cfg *config.Config, base config.FileRevision) error {
	next, err := base.Save(cfg, a.cfgPath, func(disk *config.Config) error {
		if disk != nil {
			config.PreserveClientPrefs(cfg, disk)
		}
		return nil
	})
	if err != nil {
		if isConfigChanged(err) {
			a.reloadAfterConflict()
		}
		return err
	}
	a.refreshPRConfigLocked(cfg)
	a.setRevision(next)
	a.cfg, a.cfgLoaded, a.cfgLoadError = cfg, true, ""
	return nil
}

// GetPeriodicSync returns the current periodic sync interval from config.
//
// The window owns the interval even when its fleet lives on another host.
func (a *App) GetPeriodicSync() string {
	if v := a.clientPrefs().Get().PeriodicSync; v != "" {
		return v
	}
	return "off"
}

// getPeriodicSyncLocal is the local backend's GetPeriodicSync.
func (a *App) getPeriodicSyncLocal() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.Global.PeriodicSync == "" {
		return "off"
	}
	return a.cfg.Global.PeriodicSync
}

const (
	uiModeSimple   = "simple"
	uiModeAdvanced = "advanced"
)

// GetUIMode returns the GUI experience, "simple" or "advanced". An unset
// mode (configs from before the setting) reads as advanced.
func (a *App) GetUIMode() string {
	if a.clientPrefs().Get().UIMode == uiModeSimple {
		return uiModeSimple
	}
	return uiModeAdvanced
}

// SetUIMode saves the GUI experience.
func (a *App) SetUIMode(mode string) error {
	if mode != uiModeSimple && mode != uiModeAdvanced {
		return fmt.Errorf("invalid UI mode: %s", mode)
	}
	return a.clientPrefs().Update(func(p *config.ClientPrefs) { p.UIMode = mode })
}

// SetShowPullPlan saves an explicit preference, or clears it to follow the mode.
func (a *App) SetShowPullPlan(choice string) error {
	var show *bool
	switch choice {
	case "auto":
	case "on", "off":
		enabled := choice == "on"
		show = &enabled
	default:
		return fmt.Errorf("invalid pull plan preference")
	}
	return a.clientPrefs().Update(func(p *config.ClientPrefs) { p.ShowPullPlan = show })
}

// SetTheme saves the GUI colour theme: "system", "light" or "dark".
// "system" is stored as an empty value, so the app follows the OS.
func (a *App) SetTheme(choice string) error {
	switch choice {
	case "system":
		choice = ""
	case "light", "dark":
	default:
		return fmt.Errorf("invalid theme: %s", choice)
	}
	return a.clientPrefs().Update(func(p *config.ClientPrefs) { p.Theme = choice })
}

// GetLanguage returns the configured human-facing UI language.
func (a *App) GetLanguage() string {
	return i18n.Normalize(a.clientPrefs().Get().Language)
}

// SetLanguage saves the human-facing UI language to config.
func (a *App) SetLanguage(lang string) error {
	lang = i18n.Normalize(lang)
	return a.clientPrefs().Update(func(p *config.ClientPrefs) { p.Language = lang })
}

// getCollapsedLocal is the local backend's GetCollapsed.
func (a *App) getCollapsedLocal() config.CollapsedState {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg == nil || a.cfg.Global.Collapsed == nil {
		return config.CollapsedState{}
	}
	return *a.cfg.Global.Collapsed
}

// SetCollapsed saves the folded account groups ("source") and parent clones
// ("source/repo"). Save prunes keys that no longer exist.
// The sidebar rail goes to the client preferences, and only when it changed;
// the backend stores the rest and ignores it.
func (a *App) SetCollapsed(state config.CollapsedState) error {
	store := a.clientPrefs()
	if store.Get().SidebarCollapsed != state.Sidebar {
		err := store.Update(func(p *config.ClientPrefs) { p.SidebarCollapsed = state.Sidebar })
		if err != nil {
			return err
		}
	}
	if a.remoteWindow() {
		// The host half (sources, repos) is saved on the host from P5; until
		// then it lives in this window only.
		return nil
	}
	return a.be().SetCollapsed(state)
}

// setCollapsedLocal is the local backend's SetCollapsed.
func (a *App) setCollapsedLocal(state config.CollapsedState) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg == nil {
		return fmt.Errorf("config not loaded")
	}
	// Folding is pure layout. Apply it once more against the new accounts
	// after an external domain change; pruning drops obsolete keys.
	save := func() error {
		state.Sidebar = a.cfg.Global.Collapsed != nil && a.cfg.Global.Collapsed.Sidebar
		a.cfg.Global.Collapsed = &state
		return a.saveConfig()
	}
	err := save()
	if changed, ok := errors.AsType[*config.ChangedError](err); ok && !changed.Locked && a.cfgLoaded {
		return save()
	}
	return err
}

// SetPeriodicSync saves the periodic sync interval to config.
func (a *App) SetPeriodicSync(interval string) error {
	if !validPeriodicSync(interval) {
		return fmt.Errorf("invalid periodic sync interval: %s", interval)
	}
	if interval == "off" {
		interval = ""
	}
	err := a.clientPrefs().Update(func(p *config.ClientPrefs) { p.PeriodicSync = interval })
	a.syncTaskInterval()
	return err
}

func validPeriodicSync(interval string) bool {
	switch interval {
	case "off", "5m", "15m", "30m":
		return true
	}
	return false
}

// setPeriodicSyncLocal is the local backend's SetPeriodicSync.
func (a *App) setPeriodicSyncLocal(interval string) error {
	if !validPeriodicSync(interval) {
		return fmt.Errorf("invalid periodic sync interval: %s", interval)
	}
	if interval == "off" {
		interval = ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.savePrefs(func(p *config.ClientPrefs) error {
		p.PeriodicSync = interval
		return nil
	})
}

// WindowStateDTO is the frontend-friendly window geometry.
type WindowStateDTO struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ─── Status ───────────────────────────────────────────────────

// StatusResult is the frontend-friendly repo status.
type StatusResult struct {
	Source       string      `json:"source"`
	Repo         string      `json:"repo"`
	Account      string      `json:"account"`
	Path         string      `json:"path"`
	State        string      `json:"state"`
	Ahead        int         `json:"ahead"`
	Behind       int         `json:"behind"`
	Modified     int         `json:"modified"`
	Untracked    int         `json:"untracked"`
	Conflicts    int         `json:"conflicts"`
	Error        string      `json:"error,omitempty"`
	Problem      *ProblemDTO `json:"problem,omitempty"`
	Branch       string      `json:"branch,omitempty"`
	IsDefault    *bool       `json:"isDefault,omitempty"`
	CheckedAt    string      `json:"checkedAt,omitempty"`
	sweepStarted time.Time
	pullEvidence status.PullEvidence
}

func toStatusResult(rs status.RepoStatus) StatusResult {
	var isDefault *bool
	if rs.DefaultKnown {
		isDefault = &rs.IsDefault
	}
	return StatusResult{
		Source:       rs.Source,
		Repo:         rs.Repo,
		Account:      rs.Account,
		Path:         rs.Path,
		State:        rs.State.String(),
		Ahead:        rs.Ahead,
		Behind:       rs.Behind,
		Modified:     rs.Modified,
		Untracked:    rs.Untracked,
		Conflicts:    rs.Conflicts,
		Error:        userText(rs.ErrorMsg),
		Problem:      problem(rs.Failure),
		Branch:       rs.Branch,
		IsDefault:    isDefault,
		pullEvidence: rs.Pull,
	}
}

// ─── Clone ────────────────────────────────────────────────────

// CloneRepo clones a single repo. Emits clone:progress and clone:done events.
// A non-empty root is the root the person picked: the repo clones there and
// its account remembers it for the next clone (#149).
func (a *App) CloneRepo(sourceKey, repoKey, root string) { a.be().CloneRepo(sourceKey, repoKey, root) }

// cloneRepoLocal is the local backend's CloneRepo: the clone runs in the
// background until the app closes.
func (a *App) cloneRepoLocal(sourceKey, repoKey, root string) {
	a.goTracked(func() { a.cloneRepoRun(a.refreshContext(), sourceKey, repoKey, root) })
}

// cloneRepoRun clones one repo and emits clone:progress and clone:done. It
// stops when ctx is cancelled, and git with it. What git leaves behind is
// never removed (#161): a clone git is asked to stop removes its own
// folder, and one killed outright leaves a folder that Reconcile reports as
// incomplete. Its origin is already the token-free URL the plan cloned
// from, so nothing needs resetting.
func (a *App) cloneRepoRun(ctx context.Context, sourceKey, repoKey, root string) {
	a.cloneRepoForRun(ctx, sourceKey, repoKey, root, nil, "", false)
}

func (a *App) cloneRepoForRun(ctx context.Context, sourceKey, repoKey, root string, expected *config.Config, run string, confirmed bool) bool {
	op := a.newOperation()
	started := time.Now()
	var err error
	var plan ops.ClonePlan
	var queueDepth *int
	defer func() { a.diagnostics.Record(diag.Clone, started, time.Now(), queueDepth, plan.Dest, err) }()
	if root != "" {
		_, err = hostPath(root)
	}
	a.mu.Lock()
	if expected != nil {
		if !a.sameRefreshRepoLocked(expected, sourceKey, repoKey) {
			err = errCloneSettingsChanged
		} else {
			repo := expected.Sources[sourceKey].Repos[repoKey]
			path := status.RepoPath(expected, sourceKey, repoKey, repo)
			if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) ||
				status.RepoRootOffline(expected, sourceKey, repo) {
				err = errPullPlanChanged
			}
		}
	}
	if err == nil && root != "" {
		var changed bool
		changed, err = a.cfg.PlaceRepoChecked(sourceKey, repoKey, root)
		if changed && err == nil {
			err = a.saveConfig()
		}
	}
	var cfg *config.Config
	if err == nil {
		plan, err = ops.PlanClone(a.cfg, sourceKey, repoKey)
		if err == nil {
			cfg = a.refreshConfigLocked()
		}
	}
	account := a.cfg.Accounts[a.cfg.Sources[sourceKey].Account]
	op = op.atRevision(a.configRevision)
	a.mu.Unlock()
	if err != nil {
		if confirmed && (errors.Is(err, errCloneSettingsChanged) || errors.Is(err, errPullPlanChanged)) {
			emit(op, "clone:done", cloneDoneDTO{Run: run, Source: sourceKey, Repo: repoKey, Skipped: true})
			return true
		}
		emit(op, "clone:done", cloneDoneDTO{Run: run, Source: sourceKey, Repo: repoKey, Error: userMsg(err)})
		return false
	}
	emit(op, "clone:start", repoRunDTO{Run: run, Source: sourceKey, Repo: repoKey})
	// A clone does not start against a server that does not answer.
	addr := serverAddr(account)
	if !a.reachable(ctx, addr) {
		err = errServerUnreachable
		errorText, code, host := unreachableDetails(addr)
		emit(op, "clone:done", cloneDoneDTO{Run: run, Source: sourceKey, Repo: repoKey, Error: errorText, Code: code, Host: host, Skipped: true})
		return false
	}

	// The git transfer runs without the config lock.
	var release func()
	if run != "" {
		var depth int
		release, depth, err = a.fetches.acquire(ctx, fetchHostname(account.URL))
		queueDepth = &depth
	}
	if err == nil {
		err = a.transferClone(ctx, cfg, sourceKey, repoKey, plan, confirmed, func(p git.CloneProgress) {
			emit(op, "clone:progress", cloneProgressDTO{Run: run, Source: sourceKey, Repo: repoKey, Phase: p.Phase, Percent: p.Percent})
		})
	}
	if release != nil {
		release()
	}

	if confirmed && (errors.Is(err, errCloneSettingsChanged) || errors.Is(err, errPullPlanChanged)) {
		op = a.currentRepoOperation(op, cfg, sourceKey, repoKey)
		emit(op, "clone:done", cloneDoneDTO{Run: run, Source: sourceKey, Repo: repoKey, Skipped: true})
		return true
	}

	// "path" lets the frontend fill repoStates[key].path for rows cloned
	// in-session (create-repo, Bring Local). Until the next full status
	// refresh the row otherwise has no path and every path-based kebab
	// action silently no-ops (#79).
	result := cloneDoneDTO{Run: run, Source: sourceKey, Repo: repoKey, Path: plan.Dest}
	if err != nil {
		result.Error = userMsg(failedCloneError(plan.Dest, err))
		result.Code = toUserError(err).Code
	} else {
		// heal.RepoContext sets user.name/user.email, normalizes the origin
		// URL, and configures per-repo credential isolation. Warnings are
		// returned instead of panicking so a partial heal (e.g. token
		// file missing) still reports clone success.
		report, current := a.repairClone(cfg, sourceKey, repoKey)
		if !current {
			report.Warnings = []string{"Repository settings changed before clone repair"}
		}
		if len(report.Warnings) > 0 {
			result.Warnings = report.Warnings
		}
		if len(report.Fixed) > 0 {
			result.Fixes = report.Fixed
		}
	}
	op = a.currentRepoOperation(op, cfg, sourceKey, repoKey)
	emit(op, "clone:done", result)
	if err == nil {
		a.publishCompletionStatus(ctx, op, cfg, sourceKey, repoKey, plan.Dest)
	}
	return false
}

// Protect the destination only during the transfer; repair acquires the
// same gate separately. A queued clone rechecks that it is still tracked.
func (a *App) transferClone(ctx context.Context, cfg *config.Config, source, repo string, plan ops.ClonePlan, confirmed bool, progress func(git.CloneProgress)) error {
	unlock, err := a.repairPaths.acquire(ctx, plan.Dest)
	if err != nil {
		return err
	}
	defer unlock()
	if !a.sameRefreshRepo(cfg, source, repo) {
		return errCloneSettingsChanged
	}
	if confirmed {
		item := cfg.Sources[source].Repos[repo]
		if _, err := os.Lstat(plan.Dest); !errors.Is(err, os.ErrNotExist) || status.RepoRootOffline(cfg, source, item) {
			return errPullPlanChanged
		}
	}
	return plan.Run(ctx, progress)
}

// failedCloneError reports a remaining destination without changing it. A path
// absent before cloning could belong to another process by the time Git fails.
func failedCloneError(dest string, cloneErr error) error {
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("%w; clone destination remains at %s", cloneErr, dest)
	}
	return cloneErr
}

// ─── Pull ─────────────────────────────────────────────────────

// PullRepo pulls a single repo. Emits pull:done event.
func (a *App) PullRepo(sourceKey, repoKey string) { a.be().PullRepo(sourceKey, repoKey) }

// PullAll updates eligible clones, then downloads configured missing clones.
func (a *App) PullAll(run, planID string) { a.be().PullAll(run, planID) }

// PreviewPullAll summarizes the cached status without touching the checkouts.
func (a *App) PreviewPullAll() (PullPlanDTO, error) { return a.be().PreviewPullAll() }

// RepoDetail holds human-readable status information for a single repo.
type RepoDetail struct {
	Branch          string               `json:"branch"`
	Ahead           int                  `json:"ahead"`
	Behind          int                  `json:"behind"`
	Changed         []git.FileChange     `json:"changed"`
	Untracked       []string             `json:"untracked"`
	Incoming        []git.IncomingCommit `json:"incoming"`
	Problem         *ProblemDTO          `json:"problem,omitempty"`
	IncomingProblem *ProblemDTO          `json:"incomingProblem,omitempty"`
	IncomingError   string               `json:"incomingError,omitempty"`
	Error           string               `json:"error,omitempty"`
	UpstreamGone    bool                 `json:"upstreamGone,omitempty"`
	UpstreamError   string               `json:"upstreamError,omitempty"`
}

// GetRepoDetail returns local files, incoming commits and the latest upstream flag.
// I share the bounded background probe without delaying expanded details.
func (a *App) GetRepoDetail(sourceKey, repoKey string) RepoDetail {
	return a.be().GetRepoDetail(sourceKey, repoKey)
}

// getRepoDetailLocal is the local backend's GetRepoDetail.
func (a *App) getRepoDetailLocal(sourceKey, repoKey string) RepoDetail {
	a.mu.Lock()
	src, ok := a.cfg.Sources[sourceKey]
	if !ok {
		a.mu.Unlock()
		return RepoDetail{Error: fmt.Sprintf("source %q not found", sourceKey)}
	}
	repo, ok := src.Repos[repoKey]
	if !ok {
		a.mu.Unlock()
		return RepoDetail{Error: fmt.Sprintf("repo %q not found", repoKey)}
	}
	path := status.RepoPath(a.cfg, sourceKey, repoKey, repo)
	a.mu.Unlock()

	// I show local details immediately and share the bounded background probe.
	a.probeUpstreamExistenceAsync()
	a.mu.Lock()
	goneFlag := a.upstreamGone[sourceKey+"/"+repoKey]
	a.mu.Unlock()

	branch, ahead, behind, changed, untracked, err := git.DetailedStatus(path)
	if err != nil {
		return RepoDetail{
			Error:        userMsg(err),
			Problem:      problem(err),
			UpstreamGone: goneFlag,
		}
	}
	detail := RepoDetail{
		Branch:       branch,
		Ahead:        ahead,
		Behind:       behind,
		Changed:      changed,
		Untracked:    untracked,
		UpstreamGone: goneFlag,
	}
	if behind > 0 {
		detail.Incoming, err = git.IncomingCommits(path)
		if err != nil {
			detail.IncomingError = userMsg(err)
			detail.IncomingProblem = problem(err)
		}
	}
	return detail
}

// FetchRepo runs git fetch on a single repo and emits fetch:done + refreshes status.
func (a *App) FetchRepo(sourceKey, repoKey string) { a.be().FetchRepo(sourceKey, repoKey) }

// fetchRepoLocal is the local backend's FetchRepo.
func (a *App) fetchRepoLocal(sourceKey, repoKey string) {
	a.refreshMu.Lock()
	if a.refreshClosed {
		a.refreshMu.Unlock()
		return
	}
	a.refreshWG.Go(func() {
		cfg, revision := a.refreshSnapshot()
		_ = a.fetchOne(cfg, revision, sourceKey, repoKey, nil)
		a.RefreshStatus()
	})
	a.refreshMu.Unlock()
}

// ─── Account CRUD ─────────────────────────────────────────────

// AddAccountRequest is the frontend payload for creating an account.
type AddAccountRequest struct {
	Key            string `json:"key"`
	Provider       string `json:"provider"`
	URL            string `json:"url"`
	Username       string `json:"username"`
	Name           string `json:"name"`
	Email          string `json:"email"`
	CredentialType string `json:"credentialType"`
}

// SuggestAccountKey returns an unused account key such as "github-alice"
// for the guided setup, which never asks for one.
func (a *App) SuggestAccountKey(provider, username string) string {
	return a.be().SuggestAccountKey(provider, username)
}

// suggestAccountKeyLocal is the local backend's SuggestAccountKey.
func (a *App) suggestAccountKeyLocal(provider, username string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return ops.SuggestAccountKey(a.cfg, provider, username)
}

// DiscardAccount removes a just-added account (no repos yet) from the config
// so the guided setup can start over. It never touches the disk.
func (a *App) DiscardAccount(accountKey string) error { return a.be().DiscardAccount(accountKey) }

// discardAccountLocal is the local backend's DiscardAccount.
func (a *App) discardAccountLocal(accountKey string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ops.DiscardAccount(a.cfg, accountKey); err != nil {
		return err
	}
	a.bumpCredentialRevisionLocked(accountKey)
	delete(a.prs.cache, accountKey)
	return a.saveConfig()
}

// AddAccount creates a new account and a matching source, then saves.
// Populates the credential sub-objects (GCM/SSH/Token) based on credential type.
func (a *App) AddAccount(req AddAccountRequest) error { return a.be().AddAccount(req) }

// addAccountLocal is the local backend's AddAccount.
func (a *App) addAccountLocal(req AddAccountRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	acct := config.Account{
		Provider:              req.Provider,
		URL:                   req.URL,
		Username:              req.Username,
		Name:                  req.Name,
		Email:                 req.Email,
		DefaultCredentialType: req.CredentialType,
	}
	if err := ops.AddAccount(a.cfg, req.Key, acct); err != nil {
		return err
	}
	return a.saveConfig()
}

// UpdateAccountRequest is the frontend payload for editing an account.
type UpdateAccountRequest struct {
	Key      string `json:"key"`
	Provider string `json:"provider"`
	URL      string `json:"url"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

// UpdateAccount updates the editable fields of an existing account and saves.
func (a *App) UpdateAccount(req UpdateAccountRequest) error { return a.be().UpdateAccount(req) }

// updateAccountLocal is the local backend's UpdateAccount.
func (a *App) updateAccountLocal(req UpdateAccountRequest) error {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()
	a.invalidateCredential(req.Key)
	defer a.invalidateCredential(req.Key)
	a.mu.Lock()

	acct, ok := a.cfg.Accounts[req.Key]
	if !ok {
		a.mu.Unlock()
		return fmt.Errorf("account %q not found", req.Key)
	}

	if acct.CredentialTransition != nil {
		a.mu.Unlock()
		return ops.ErrCredentialTransitionPending
	}

	if req.Provider != "" {
		acct.Provider = req.Provider
	}
	acct.URL = req.URL
	acct.Username = req.Username
	acct.Name = req.Name
	acct.Email = req.Email

	if err := a.cfg.UpdateAccount(req.Key, acct); err != nil {
		a.mu.Unlock()
		return err
	}
	if err := a.saveConfig(); err != nil {
		a.mu.Unlock()
		return err
	}

	// Bring existing clones in line: identity, origin URL, credential config.
	a.mu.Unlock()
	a.reconfigureClones(req.Key)
	return nil
}

// RenameAccount changes an account's key and preserves its clone paths.
func (a *App) RenameAccount(oldKey, newKey string) (ops.StepReport, error) {
	return a.be().RenameAccount(oldKey, newKey)
}

// ─── Identity ────────────────────────────────────────────────

// CheckGlobalIdentity returns whether ~/.gitconfig has user.name/user.email.
func (a *App) CheckGlobalIdentity() identity.GlobalIdentityStatus {
	return a.be().CheckGlobalIdentity()
}

// checkGlobalIdentityLocal is the local backend's CheckGlobalIdentity.
func (a *App) checkGlobalIdentityLocal() identity.GlobalIdentityStatus {
	return identity.CheckGlobalIdentity()
}

// RemoveGlobalIdentity removes user.name/user.email from ~/.gitconfig.
func (a *App) RemoveGlobalIdentity() error { return a.be().RemoveGlobalIdentity() }

// removeGlobalIdentityLocal is the local backend's RemoveGlobalIdentity.
func (a *App) removeGlobalIdentityLocal() error {
	return identity.RemoveGlobalIdentity()
}

// IsGlobalGCMConfigNeeded reports whether the "Global gitconfig" warning
// should be evaluated at all — true when at least one account uses GCM.
// Frontends gate the banner render on this so pure SSH/token users never
// see it.
func (a *App) IsGlobalGCMConfigNeeded() bool { return a.be().IsGlobalGCMConfigNeeded() }

// isGlobalGCMConfigNeededLocal is the local backend's IsGlobalGCMConfigNeeded.
func (a *App) isGlobalGCMConfigNeededLocal() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return credential.IsGlobalGCMConfigNeeded(a.cfg)
}

// CheckGlobalGCMConfig returns the current state of ~/.gitconfig vs. what
// gitbox expects for GCM-backed fills (credential.helper,
// credential.credentialStore). The frontend surfaces a yellow warning
// banner whenever NeedsFix is true AND IsGlobalGCMConfigNeeded() is true.
func (a *App) CheckGlobalGCMConfig() credential.GlobalGCMConfigStatus {
	return a.be().CheckGlobalGCMConfig()
}

// checkGlobalGCMConfigLocal is the local backend's CheckGlobalGCMConfig.
func (a *App) checkGlobalGCMConfigLocal() credential.GlobalGCMConfigStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return credential.CheckGlobalGCMConfig(a.cfg)
}

// FixGlobalGCMConfig writes the expected credential.helper and
// credential.credentialStore to ~/.gitconfig and backfills OS defaults
// into gitbox.json when the persisted config predates them.
func (a *App) FixGlobalGCMConfig() error { return a.be().FixGlobalGCMConfig() }

// fixGlobalGCMConfigLocal is the local backend's FixGlobalGCMConfig.
func (a *App) fixGlobalGCMConfigLocal() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fixGlobalGCMConfigLocked()
}

// fixGlobalGCMConfigLocked repairs the global gitconfig for GCM and saves
// the defaults it backfilled into the config (#173). The backfill is saved
// even when the gitconfig write fails, so a retry succeeds; a refused save
// takes the backfill back out. Caller holds a.mu.
func (a *App) fixGlobalGCMConfigLocked() error {
	prev := a.cfg.Global.CredentialGCM
	changed, err := credential.FixGlobalGCMConfig(a.cfg)
	if changed {
		if serr := a.saveConfig(); serr != nil {
			if !isConfigChanged(serr) {
				// A conflict already reloaded the file; anything else
				// takes the backfill back out.
				a.cfg.Global.CredentialGCM = prev
			}
			if err == nil {
				err = serr
			}
		}
	}
	return err
}

// ─── Global Gitignore ────────────────────────────────────────

// CheckGlobalGitignore reports whether the recommended global gitignore
// block is installed and core.excludesfile is set. The frontend calls
// this on startup so it can surface a notification when
// status.NeedsAction is true.
func (a *App) CheckGlobalGitignore() (gitignore.Status, error) { return a.be().CheckGlobalGitignore() }

// checkGlobalGitignoreLocal is the local backend's CheckGlobalGitignore.
func (a *App) checkGlobalGitignoreLocal() (gitignore.Status, error) {
	return gitignore.Check()
}

// InstallGlobalGitignore installs or refreshes the recommended block in
// ~/.gitignore_global, creating a timestamped backup of any existing
// file and pointing core.excludesfile at it. Idempotent: when the block
// is already up to date and the config key is already set, this is a
// no-op (no rewrite, no backup).
func (a *App) InstallGlobalGitignore() (gitignore.InstallResult, error) {
	return a.be().InstallGlobalGitignore()
}

// installGlobalGitignoreLocal is the local backend's InstallGlobalGitignore.
func (a *App) installGlobalGitignoreLocal() (gitignore.InstallResult, error) {
	return gitignore.Install()
}

// recommendedGlobalGitignoreBodyLocal is the local backend's RecommendedGlobalGitignoreBody.
func (a *App) recommendedGlobalGitignoreBodyLocal() string {
	return gitignore.RecommendedBody()
}

// GetCheckGlobalGitignore returns the user's preference for whether
// the GUI runs an automatic startup check of ~/.gitignore_global.
// Defaults to true when the field is missing from gitbox.json.
func (a *App) GetCheckGlobalGitignore() bool { return a.be().GetCheckGlobalGitignore() }

// getCheckGlobalGitignoreLocal is the local backend's GetCheckGlobalGitignore.
func (a *App) getCheckGlobalGitignoreLocal() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Global.ShouldCheckGlobalGitignore()
}

// SetCheckGlobalGitignore persists the user's preference into the
// global section of gitbox.json. The setting only gates the automatic
// startup check; an explicit Install click always runs.
func (a *App) SetCheckGlobalGitignore(enabled bool) error {
	return a.be().SetCheckGlobalGitignore(enabled)
}

// setCheckGlobalGitignoreLocal is the local backend's SetCheckGlobalGitignore.
func (a *App) setCheckGlobalGitignoreLocal(enabled bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := enabled
	a.cfg.Global.CheckGlobalGitignore = &v
	return a.saveConfig()
}

// ─── Autostart ───────────────────────────────────────────────

// GetAutostart returns whether the app is set to run at OS login.
func (a *App) GetAutostart() (bool, error) {
	return autostartEnabled()
}

// SetAutostart enables or disables run-at-login for the app.
func (a *App) SetAutostart(enable bool) error {
	return autostartSet(enable)
}

// ─── Credential Setup ─────────────────────────────────────────

// CredentialSetupResult is the outcome of a credential setup step.
type CredentialSetupResult struct {
	OK           bool   `json:"ok"`
	Message      string `json:"message"`
	NeedsPAT     bool   `json:"needsPAT,omitempty"`     // true when SSH works but discovery needs a PAT
	SSHPublicKey string `json:"sshPublicKey,omitempty"` // public key to copy (SSH setup)
	SSHAddURL    string `json:"sshAddURL,omitempty"`    // provider URL to add key (SSH setup)
	SSHVerified  bool   `json:"sshVerified,omitempty"`  // true when SSH connection test passed
	// Findings are what the credential audit reports right after a token is
	// stored (#242): a file or folder others can read. Paths and problems
	// only, never the file's contents.
	Findings []credential.Finding `json:"findings,omitempty"`
}

// gcmSignInTimeout bounds the interactive GCM sign-in: long enough to type a
// password or finish a browser flow, short enough that a GCM waiting on a
// prompt nobody can see never hangs the GUI. A var so tests can shorten it.
var gcmSignInTimeout = 5 * time.Minute

// gcmSignInEnv is the environment for the interactive sign-in. GitboxApp has
// no visible console, so a terminal prompt from GCM or git waits forever on a
// hidden one. Per-user variables such as GCM_GUI_PROMPT=false or
// GCM_INTERACTIVE=never cause exactly that, so the sign-in forces GCM's own
// windows and turns git's terminal fallback off. Later entries win, so these
// override the inherited values. git.Environ keeps the Homebrew PATH on macOS.
func gcmSignInEnv() []string {
	return append(git.Environ(),
		"GCM_GUI_PROMPT=true",
		"GCM_INTERACTIVE=always",
		"GIT_TERMINAL_PROMPT=0",
	)
}

// gcmSignInTimeoutMessage explains a sign-in that never finished, naming any
// GCM_* variables in the environment because they are the usual cause.
func gcmSignInTimeoutMessage(username, host string) string {
	msg := fmt.Sprintf("GCM sign-in for %s@%s did not finish within %s, so gitbox stopped waiting. Close any open GCM window and try again.",
		username, host, gcmSignInTimeout)
	var vars []string
	for _, kv := range os.Environ() {
		if name, _, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(strings.ToUpper(name), "GCM_") {
			vars = append(vars, name)
		}
	}
	if len(vars) > 0 {
		sort.Strings(vars)
		msg += " Your environment sets " + strings.Join(vars, ", ") + ", which changes how GCM behaves; see Troubleshooting in docs/credentials.md."
	}
	return msg
}

// CredentialSetupGCM triggers GCM browser authentication for an account.
func (a *App) CredentialSetupGCM(accountKey string) CredentialSetupResult {
	return a.be().CredentialSetupGCM(accountKey)
}

// credentialSetupGCMLocal is the local backend's CredentialSetupGCM.
func (a *App) credentialSetupGCMLocal(accountKey string) CredentialSetupResult {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()
	a.invalidateCredential(accountKey)
	defer a.invalidateCredential(accountKey)
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()

	if !ok {
		return CredentialSetupResult{OK: false, Message: "Account not found"}
	}
	if acct.CredentialTransition != nil {
		return CredentialSetupResult{OK: false, Message: userMsg(ops.ErrCredentialTransitionPending)}
	}

	// Ensure global git config has the GCM credential sections BEFORE
	// any fill/approve — git needs to know the credential helper to store it.
	// FixGlobalGCMConfig also backfills OS defaults into gitbox.json when the
	// config predates onboarding — belt-and-braces for users who dismissed
	// the top-level "Global gitconfig needs fixing" banner.
	a.mu.Lock()
	fixErr := a.fixGlobalGCMConfigLocked()
	a.mu.Unlock()
	if fixErr != nil {
		return CredentialSetupResult{OK: false, Message: userText(fmt.Sprintf("Failed to repair global gitconfig: %v", fixErr))}
	}

	realUsername, err := signInGCM(acct, nil)
	if err != nil {
		return CredentialSetupResult{OK: false, Message: userMsg(err)}
	}
	if realUsername != acct.Username {
		_, err := a.commitConfig(func(cfg *config.Config) error {
			cur := cfg.Accounts[accountKey]
			cur.Username = realUsername
			return cfg.UpdateAccount(accountKey, cur)
		})
		if err != nil {
			return CredentialSetupResult{OK: false, Message: userMsg(err)}
		}
	}
	// Reconfigure existing clones to use the current credential type.
	n := a.reconfigureClones(accountKey)
	msg := fmt.Sprintf("GCM credential stored for %s@%s", realUsername, ops.HostnameFromURL(acct.URL))
	if n > 0 {
		msg += fmt.Sprintf("\n%d clone(s) reconfigured", n)
	}
	return CredentialSetupResult{OK: true, Message: msg}
}

// signInGCM prepares GCM's helper entry without changing the account.
func signInGCM(acct config.Account, managed *config.GlobalConfig) (string, error) {
	host := ops.HostnameFromURL(acct.URL)
	resolve := credential.ResolveGCMToken
	var settings []string
	if managed != nil {
		settings = credential.GCMHelperArgs(acct, *managed)
		resolve = func(accountURL, username string) (string, string, error) {
			target := acct
			target.URL, target.Username = accountURL, username
			return credential.ResolveConfiguredGCMToken(context.Background(), target, *managed)
		}
	}

	// Check if GCM already has a stored credential.
	_, _, err := resolve(acct.URL, acct.Username)
	if err == nil {
		return acct.Username, nil
	}

	// Trigger interactive git credential fill — GCM opens its sign-in window
	// (password dialog or browser).
	scheme := "https"
	gitHost := host
	if managed != nil {
		if u, err := url.Parse(acct.URL); err == nil && u.Host != "" {
			scheme, gitHost = u.Scheme, u.Host
		}
	}
	input := fmt.Sprintf("protocol=%s\nhost=%s\nusername=%s\n\n", scheme, gitHost, acct.Username)

	// Run from home dir so repo-local .git/config credential overrides
	// don't interfere with GCM. Home has ~/.gitconfig with per-host settings.
	homeDir, _ := os.UserHomeDir()
	fillCtx, cancelFill := context.WithTimeout(context.Background(), gcmSignInTimeout)
	defer cancelFill()
	// The runner captures the output (a GUI launched without a console has
	// no valid stderr handle to forward to) and stops waiting two seconds
	// after a timeout: GCM may outlive the killed git and hold the pipes open.
	fill, err := git.Run(fillCtx, git.Request{Dir: homeDir, Args: append(slices.Clone(settings), "credential", "fill"),
		EnvExact: gcmSignInEnv(), Stdin: strings.NewReader(input)})
	out := fill.Stdout
	if err != nil {
		if fillCtx.Err() != nil {
			return "", errors.New(gcmSignInTimeoutMessage(acct.Username, host))
		}
		// What the helper printed is external text: scrub it like any error.
		detail := userText(string(fill.Stderr))
		if detail == "" {
			detail = userMsg(err)
		}
		return "", fmt.Errorf("GCM authentication failed for %s@%s: %s", acct.Username, host, detail)
	}

	// Parse the fill output — extract password and the actual username GCM used.
	gotPassword := false
	realUsername := acct.Username
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if pw, ok := strings.CutPrefix(line, "password="); ok && pw != "" {
			gotPassword = true
		}
		if u, ok := strings.CutPrefix(line, "username="); ok && u != "" {
			realUsername = u
		}
	}
	if !gotPassword {
		return "", fmt.Errorf("GCM returned no credential for %s@%s", acct.Username, host)
	}

	// Approve so git stores it persistently. Best-effort — modern GCM may
	// already persist during fill. We verify storage below.
	approveCtx, cancelApprove := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelApprove()
	_, _ = git.Run(approveCtx, git.Request{Dir: homeDir, Args: append(slices.Clone(settings), "credential", "approve"),
		EnvExact: gcmSignInEnv(), Stdin: bytes.NewReader(out)})

	// Verify the credential was actually stored.
	if _, _, err := resolve(acct.URL, realUsername); err != nil {
		return "", fmt.Errorf("GCM authentication completed but credential was not stored for %s@%s. Try running: git credential approve manually", realUsername, host)
	}

	return realUsername, nil
}

// CredentialStoreToken stores a PAT in the credential file for an account.
func (a *App) CredentialStoreToken(accountKey, token string) CredentialSetupResult {
	return a.be().CredentialStoreToken(accountKey, token)
}

// credentialStoreTokenLocal is the local backend's CredentialStoreToken.
func (a *App) credentialStoreTokenLocal(accountKey, token string) CredentialSetupResult {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()
	a.invalidateCredential(accountKey)
	defer a.invalidateCredential(accountKey)
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()

	if !ok {
		return CredentialSetupResult{OK: false, Message: "Account not found"}
	}
	if acct.CredentialTransition != nil {
		return CredentialSetupResult{OK: false, Message: userMsg(ops.ErrCredentialTransitionPending)}
	}

	if strings.TrimSpace(token) == "" {
		return CredentialSetupResult{OK: false, Message: "Token cannot be empty"}
	}

	if err := credential.StoreTokenWithAccount(accountKey, strings.TrimSpace(token), acct); err != nil {
		return CredentialSetupResult{OK: false, Message: userMsg(err)}
	}

	// Reconfigure existing clones to use the current credential type.
	n := a.reconfigureClones(accountKey)
	msg := "Token stored in credential file"
	if n > 0 {
		msg += fmt.Sprintf("\n%d clone(s) reconfigured", n)
	}
	return CredentialSetupResult{OK: true, Message: msg, Findings: a.refreshCredentialAudit()}
}

// TokenGuideInfo holds where to create an account's PAT and the scopes it needs.
type TokenGuideInfo struct {
	CreationURL string `json:"creationURL"`
	Scopes      string `json:"scopes"`
}

// GetTokenGuide returns provider-specific PAT creation info.
func (a *App) GetTokenGuide(accountKey string) TokenGuideInfo {
	return a.be().GetTokenGuide(accountKey)
}

// getTokenGuideLocal is the local backend's GetTokenGuide.
func (a *App) getTokenGuideLocal(accountKey string) TokenGuideInfo {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()

	if !ok {
		return TokenGuideInfo{}
	}
	return TokenGuideInfo{
		CreationURL: provider.TokenCreationURL(acct.Provider, acct.URL),
		Scopes:      provider.TokenRequiredScopes(acct.Provider),
	}
}

// CredentialSetupSSH generates an SSH key and configures ~/.ssh/config.
func (a *App) CredentialSetupSSH(accountKey string) CredentialSetupResult {
	return a.be().CredentialSetupSSH(accountKey)
}

// credentialSetupSSHLocal is the local backend's CredentialSetupSSH.
func (a *App) credentialSetupSSHLocal(accountKey string) CredentialSetupResult {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()
	return a.setupSSH(accountKey)
}

func (a *App) setupSSH(accountKey string) CredentialSetupResult {
	return a.runSSHSetup(accountKey, ops.SetupSSHCredential)
}

// runSSHSetup runs setup (pkg/ops, #244) on a snapshot of the config, then
// points the account's SSH block at the result, saves, and reconfigures the
// account's clones. On a remote host all of it happens on the host: the key
// stays there and only its public half comes back.
func (a *App) runSSHSetup(accountKey string, setup func(context.Context, *config.Config, string) (ops.SSHSetup, error)) CredentialSetupResult {
	a.invalidateCredential(accountKey)
	defer a.invalidateCredential(accountKey)
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	cfg := a.snapshotLocked()
	a.mu.Unlock()

	if !ok {
		return CredentialSetupResult{OK: false, Message: "Account not found"}
	}
	if acct.CredentialTransition != nil {
		return CredentialSetupResult{OK: false, Message: userMsg(ops.ErrCredentialTransitionPending)}
	}

	s, err := setup(context.Background(), cfg, accountKey)
	if err != nil {
		return CredentialSetupResult{OK: false, Message: userMsg(err)}
	}

	// The account is read again under the lock, so a change made meanwhile
	// is kept.
	a.mu.Lock()
	if cur, ok := a.cfg.Accounts[accountKey]; ok && ops.ApplySSHSetup(&cur, s) {
		_ = a.cfg.UpdateAccount(accountKey, cur)
		_ = a.saveConfig()
	}
	a.mu.Unlock()

	n := a.reconfigureClones(accountKey)

	keyName := fmt.Sprintf("gitbox-%s-sshkey", accountKey)
	msg := fmt.Sprintf("SSH Host %s and %s available.", s.Host, keyName)
	if n > 0 {
		msg += fmt.Sprintf("\n%d clone(s) reconfigured.", n)
	}

	// Check whether a PAT exists for API discovery.
	_, _, apiErr := credential.ResolveToken(acct, accountKey)

	return CredentialSetupResult{
		OK:           true,
		Message:      msg,
		NeedsPAT:     apiErr != nil,
		SSHPublicKey: s.PublicKey,
		SSHAddURL:    s.AddURL,
		SSHVerified:  s.Verified,
	}
}

// CredentialRegenerateSSH deletes the existing SSH key and config entry,
// then runs CredentialSetupSSH to create fresh ones.
func (a *App) CredentialRegenerateSSH(accountKey string) CredentialSetupResult {
	return a.be().CredentialRegenerateSSH(accountKey)
}

// credentialRegenerateSSHLocal is the local backend's CredentialRegenerateSSH.
func (a *App) credentialRegenerateSSHLocal(accountKey string) CredentialSetupResult {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()
	return a.runSSHSetup(accountKey, ops.RegenerateSSHCredential)
}

// SSHAgentReportDTO compares an SSH account's key with the keys of the ssh
// agent the machine that runs git sees (#244): on a remote host, the
// client's agent when the connection forwards it. Fingerprints only, never
// key material.
type SSHAgentReportDTO struct {
	KeyFile            string   `json:"keyFile"`
	KeyFileFingerprint string   `json:"keyFileFingerprint"`
	AgentFingerprints  []string `json:"agentFingerprints"`
	AgentForwarded     bool     `json:"agentForwarded"`
	Match              bool     `json:"match"`
	IdentitiesOnly     bool     `json:"identitiesOnly"`
}

// CredentialSSHAgentReport reports which key git uses for an SSH account:
// its own key file or one of the agent's.
func (a *App) CredentialSSHAgentReport(accountKey string) SSHAgentReportDTO {
	return a.be().CredentialSSHAgentReport(accountKey)
}

// credentialSSHAgentReportLocal is the local backend's
// CredentialSSHAgentReport. A missing account reports nothing.
func (a *App) credentialSSHAgentReportLocal(accountKey string) SSHAgentReportDTO {
	a.mu.Lock()
	_, ok := a.cfg.Accounts[accountKey]
	cfg := a.snapshotLocked()
	a.mu.Unlock()
	if !ok {
		return SSHAgentReportDTO{AgentFingerprints: []string{}}
	}
	r := credential.SSHAgentReport(context.Background(), cfg, accountKey)
	if r.AgentFingerprints == nil {
		r.AgentFingerprints = []string{}
	}
	return SSHAgentReportDTO{
		KeyFile:            r.KeyFile,
		KeyFileFingerprint: r.KeyFileFingerprint,
		AgentFingerprints:  r.AgentFingerprints,
		AgentForwarded:     r.AgentForwarded,
		Match:              r.Match,
		IdentitiesOnly:     r.IdentitiesOnly,
	}
}

// ─── Delete ───────────────────────────────────────────────────

// StopTrackingRepo forgets a repo and leaves its folder intact.
func (a *App) StopTrackingRepo(sourceKey, repoKey string) error {
	return a.be().StopTrackingRepo(sourceKey, repoKey)
}

// DeleteRepoFolder deletes the clone folder when the Advanced window confirms.
func (a *App) DeleteRepoFolder(sourceKey, repoKey string) error {
	if a.GetUIMode() == uiModeSimple {
		return errDeleteFoldersAdvanced
	}
	return a.be().DeleteRepoFolder(sourceKey, repoKey)
}

var errDeleteFoldersAdvanced = errors.New("deleting clone folders requires Advanced mode")

func (a *App) stopTrackingRepoLocal(sourceKey, repoKey string) error {
	return a.changeRepoTracking(sourceKey, repoKey, ops.StopTrackingRepo)
}

func (a *App) deleteRepoFolderLocal(sourceKey, repoKey string) error {
	return a.changeRepoTracking(sourceKey, repoKey, func(cfg *config.Config, source, repo string) error {
		if r, ok := cfg.Sources[source].Repos[repo]; ok {
			if err := a.refuseBusyDeletion(status.RepoPath(cfg, source, repo, r)); err != nil {
				return err
			}
		}
		return ops.DeleteRepoClone(cfg, source, repo)
	})
}

func (a *App) changeRepoTracking(sourceKey, repoKey string, change func(*config.Config, string, string) error) error {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.saveDeletion(func(cfg *config.Config) error { return change(cfg, sourceKey, repoKey) })
}

// saveDeletion checks the disk revision before any filesystem changes
// and keeps the config lock until the resulting config has been saved.
// The caller holds a.mu and a.maintenanceMu.
func (a *App) saveDeletion(change func(*config.Config) error) error {
	if err := a.saveRefused(); err != nil {
		return err
	}
	next, err := a.cfg.Clone()
	if err != nil {
		return err
	}
	base, err := a.cfgBase.Save(next, a.cfgPath, func(onDisk *config.Config) error {
		if onDisk != nil {
			config.PreserveClientPrefs(next, onDisk)
		}
		return change(next)
	})
	if err != nil {
		if isConfigChanged(err) {
			a.reloadAfterConflict()
		}
		return err
	}
	*a.cfg = *next
	a.setRevision(base)
	return nil
}

// AccountDeletionImpactDTO describes what a DeleteAccount call would remove.
// Lists are sorted for stable rendering.
type AccountDeletionImpactDTO struct {
	Account    string   `json:"account"`
	Sources    []string `json:"sources"`
	Mirrors    []string `json:"mirrors"`
	RepoCount  int      `json:"repo_count"`
	CloneCount int      `json:"clone_count"`
}

// AccountDeletionImpact reports the cascade effect of deleting the given
// account — every source and mirror that references it,
// plus the total repo count and how many are currently cloned on disk.
// Read-only.
func (a *App) AccountDeletionImpact(accountKey string) AccountDeletionImpactDTO {
	return a.be().AccountDeletionImpact(accountKey)
}

// accountDeletionImpactLocal is the local backend's AccountDeletionImpact.
func (a *App) accountDeletionImpactLocal(accountKey string) AccountDeletionImpactDTO {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.cfg.AccountDeletionImpact(accountKey)
	dto := AccountDeletionImpactDTO{
		Account:    accountKey,
		Sources:    r.Sources,
		Mirrors:    r.Mirrors,
		RepoCount:  r.RepoCount,
		CloneCount: ops.CountClonedRepos(a.cfg, accountKey),
	}
	if dto.Sources == nil {
		dto.Sources = []string{}
	}
	if dto.Mirrors == nil {
		dto.Mirrors = []string{}
	}
	return dto
}

// DeleteAccount removes the account and credentials. The Advanced window
// may explicitly opt in to removing tracked clone folders as well.
func (a *App) DeleteAccount(accountKey string, removeFolders bool) ([]string, error) {
	if removeFolders && a.GetUIMode() == uiModeSimple {
		return nil, errDeleteFoldersAdvanced
	}
	return a.be().DeleteAccount(accountKey, removeFolders)
}

func (a *App) deleteAccountLocal(accountKey string, removeFolders bool) ([]string, error) {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()
	a.invalidateCredential(accountKey)
	defer a.invalidateCredential(accountKey)
	a.mu.Lock()
	defer a.mu.Unlock()

	var failures []string
	err := a.saveDeletion(func(cfg *config.Config) error {
		if removeFolders {
			for _, source := range cfg.AccountDeletionImpact(accountKey).Sources {
				for repo, r := range cfg.Sources[source].Repos {
					if err := a.refuseBusyDeletion(status.RepoPath(cfg, source, repo, r)); err != nil {
						return err
					}
				}
			}
		}
		_, errs := ops.DeleteAccount(cfg, accountKey, removeFolders)
		for _, err := range errs {
			if _, ok := errors.AsType[*ops.CloneFolderError](err); !ok {
				return err
			}
			failures = append(failures, userMsg(err))
		}
		return errors.Join(errs...)
	})
	if len(failures) != 0 {
		return failures, nil
	}
	if err != nil {
		return nil, err
	}
	delete(a.prs.cache, accountKey)
	return []string{}, nil
}

// ChangeCredentialType verifies a replacement before switching the account.
func (a *App) ChangeCredentialType(accountKey, newType, token string) (ops.StepReport, error) {
	return a.be().ChangeCredentialType(accountKey, newType, token)
}

// CredentialDelete removes the credential artifacts for an account and clears
// its credential configuration so it can be set up from scratch.
func (a *App) CredentialDelete(accountKey string) CredentialSetupResult {
	return a.be().CredentialDelete(accountKey)
}

// credentialDeleteLocal is the local backend's CredentialDelete.
func (a *App) credentialDeleteLocal(accountKey string) CredentialSetupResult {
	a.maintenanceMu.Lock()
	defer a.maintenanceMu.Unlock()
	a.invalidateCredential(accountKey)
	defer a.invalidateCredential(accountKey)
	a.mu.Lock()
	defer a.mu.Unlock()

	msgs, err := ops.DeleteCredential(a.cfg, accountKey)
	if err != nil {
		return CredentialSetupResult{OK: false, Message: userMsg(err)}
	}
	if err := a.saveConfig(); err != nil {
		return CredentialSetupResult{OK: false, Message: userMsg(err)}
	}
	return CredentialSetupResult{OK: true, Message: strings.Join(msgs, "\n")}
}

// ─── Discovery ────────────────────────────────────────────────

// DiscoverResult is a repo found during discovery.
type DiscoverResult struct {
	FullName    string `json:"fullName"`
	Description string `json:"description"`
	Private     bool   `json:"private"`
	Fork        bool   `json:"fork"`
	Archived    bool   `json:"archived"`
}

// Discover lists remote repos for an account. Emits discover:done event.
func (a *App) Discover(accountKey string) { a.be().Discover(accountKey) }

// discoverLocal is the local backend's Discover.
func (a *App) discoverLocal(accountKey string) {
	go func() {
		a.mu.Lock()
		acct, ok := a.cfg.Accounts[accountKey]
		op := a.newOperation().atRevision(a.configRevision)
		a.mu.Unlock()

		if !ok {
			emit(op, "discover:done", discoverDoneDTO{AccountKey: accountKey, Error: fmt.Sprintf("account %q not found", accountKey)})
			return
		}

		var repos []provider.RemoteRepo
		err := errServerUnreachable
		if a.reachable(a.refreshContext(), serverAddr(acct)) {
			repos, err = ops.ListRemoteRepos(context.Background(), acct, accountKey)
		}
		if err != nil {
			emit(op, "discover:done", discoverDoneDTO{AccountKey: accountKey, Error: userMsg(err), Problem: problem(err)})
			return
		}

		results := make([]DiscoverResult, len(repos))
		for i, r := range repos {
			results[i] = DiscoverResult{
				FullName:    r.FullName,
				Description: r.Description,
				Private:     r.Private,
				Fork:        r.Fork,
				Archived:    r.Archived,
			}
		}

		emit(op, "discover:done", discoverDoneDTO{AccountKey: accountKey, Repos: results})
	}()
}

// AddDiscoveredRepos adds discovered repos to the config and saves.
// The key can be either a source key or an account key — if it's an account
// key, the first source referencing that account is used.
func (a *App) AddDiscoveredRepos(key string, repoNames []string) error {
	return a.be().AddDiscoveredRepos(key, repoNames)
}

// addDiscoveredReposLocal is the local backend's AddDiscoveredRepos.
func (a *App) addDiscoveredReposLocal(key string, repoNames []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := ops.AddDiscoveredRepos(a.cfg, key, repoNames); err != nil {
		return err
	}
	return a.saveConfig()
}

// ─── Credential Verification ──────────────────────────────────

// CredentialStatus is the result of verifying an account's credentials.
type CredentialStatus struct {
	revision   uint64
	Problem    *ProblemDTO `json:"problem,omitempty"`
	Status     string      `json:"status"` // overall: "ok", "warning", "error", "none"
	Message    string      `json:"message"`
	Primary    string      `json:"primary"` // primary credential status
	PrimaryMsg string      `json:"primaryMsg"`
	PAT        string      `json:"pat"` // companion PAT status (for SSH/GCM)
	PATMsg     string      `json:"patMsg"`
	ExpiresAt  string      `json:"expiresAt,omitempty"` // RFC 3339; absent when unknown or never
}

// ─── Helpers ──────────────────────────────────────────────────

// ─── Mirror methods ──────────────────────────────────────────

// GetMirrorStatus checks live sync status for all repos in a mirror group.
// Runs async; emits "mirror:status" event with []MirrorStatusResult.
func (a *App) GetMirrorStatus(mirrorKey string) { a.be().GetMirrorStatus(mirrorKey) }

// getMirrorStatusLocal is the local backend's GetMirrorStatus.
func (a *App) getMirrorStatusLocal(mirrorKey string) {
	a.goTracked(func() {
		a.mu.Lock()
		cfg := a.snapshotLocked()
		a.mu.Unlock()
		_ = a.checkMirrorStatus(a.refreshContext(), cfg, mirrorKey)
	})
}

func (a *App) checkMirrorStatus(ctx context.Context, cfg *config.Config, mirrorKey string) error {
	m, ok := cfg.Mirrors[mirrorKey]
	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// A mirror needs both of its servers: without one, every repo reads
	// unreachable at once instead of timing out one by one.
	online := a.reachable(ctx, serverAddr(cfg.Accounts[m.AccountSrc])) && a.reachable(ctx, serverAddr(cfg.Accounts[m.AccountDst]))
	var results []MirrorStatusResult
	var err error
	for repoKey := range m.Repos {
		if !online {
			err = errTaskPartial
			results = append(results, MirrorStatusResult{MirrorKey: mirrorKey, RepoKey: repoKey, Error: userMsg(errServerUnreachable)})
			continue
		}
		r := mirror.CheckStatus(ctx, cfg, mirrorKey, repoKey)
		if r.Error != "" {
			err = errTaskPartial
		}
		results = append(results, MirrorStatusResult{
			MirrorKey:  mirrorKey,
			RepoKey:    r.RepoKey,
			Direction:  r.Direction,
			OriginAcct: r.OriginAcct,
			BackupAcct: r.BackupAcct,
			SyncStatus: r.SyncStatus,
			HeadCommit: r.HeadCommit,
			OriginHead: r.OriginHead,
			BackupHead: r.BackupHead,
			Warning:    r.Warning,
			NeedsSetup: r.NeedsSetup,
			Error:      userText(r.Error),
		})
	}
	emit(a.newOperation(), "mirror:status", results)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

// SetupMirrorRepo runs API setup for a single mirror repo.
// Runs async; emits "mirror:setup:done" event with MirrorSetupResult.
// On success, stamps MirrorRepo.LastSync so CheckStatus can tell "never set up"
// from "set up and target is genuinely missing" (issue #59).
func (a *App) SetupMirrorRepo(mirrorKey, repoKey string) { a.be().SetupMirrorRepo(mirrorKey, repoKey) }

// setupMirrorRepoLocal is the local backend's SetupMirrorRepo: the setup
// runs in the background until the app closes.
func (a *App) setupMirrorRepoLocal(mirrorKey, repoKey string) {
	a.goTracked(func() { a.setupMirrorRepoRun(a.refreshContext(), mirrorKey, repoKey) })
}

// setupMirrorRepoRun sets up one mirror repo and emits mirror:setup:done.
// It stops when ctx is cancelled, and after mirrorSetupTimeout.
func (a *App) setupMirrorRepoRun(ctx context.Context, mirrorKey, repoKey string) {
	a.mu.Lock()
	cfg := a.snapshotLocked()
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, mirrorSetupTimeout)
	defer cancel()

	r := mirror.SetupMirror(ctx, cfg, mirrorKey, repoKey)
	result := MirrorSetupResult{
		MirrorKey:    mirrorKey,
		RepoKey:      r.RepoKey,
		Created:      r.Created,
		Mirrored:     r.Mirrored,
		Method:       r.Method,
		Instructions: r.Instructions,
		Error:        userText(r.Error),
	}

	if r.Error == "" {
		a.mu.Lock()
		if m, ok := a.cfg.Mirrors[mirrorKey]; ok {
			if mr, ok := m.Repos[repoKey]; ok {
				mr.LastSync = time.Now().UTC().Format(time.RFC3339)
				m.Repos[repoKey] = mr
				a.cfg.Mirrors[mirrorKey] = m
				_ = a.saveConfig()
			}
		}
		a.mu.Unlock()
	}

	emit(a.newOperation(), "mirror:setup:done", result)
}

// AddMirrorGroup creates a new mirror group pairing two accounts.
func (a *App) AddMirrorGroup(key, accountSrc, accountDst string) error {
	return a.be().AddMirrorGroup(key, accountSrc, accountDst)
}

// addMirrorGroupLocal is the local backend's AddMirrorGroup.
func (a *App) addMirrorGroupLocal(key, accountSrc, accountDst string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	m := config.Mirror{
		AccountSrc: accountSrc,
		AccountDst: accountDst,
	}
	if err := a.cfg.AddMirror(key, m); err != nil {
		return err
	}
	return a.saveConfig()
}

// DeleteMirrorGroup removes a mirror group and all its repos.
func (a *App) DeleteMirrorGroup(key string) error { return a.be().DeleteMirrorGroup(key) }

// deleteMirrorGroupLocal is the local backend's DeleteMirrorGroup.
func (a *App) deleteMirrorGroupLocal(key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.cfg.DeleteMirror(key); err != nil {
		return err
	}
	return a.saveConfig()
}

// AddMirrorRepo adds a repo to a mirror group.
func (a *App) AddMirrorRepo(mirrorKey, repoKey, direction, origin string) error {
	return a.be().AddMirrorRepo(mirrorKey, repoKey, direction, origin)
}

// addMirrorRepoLocal is the local backend's AddMirrorRepo.
func (a *App) addMirrorRepoLocal(mirrorKey, repoKey, direction, origin string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	repo := config.MirrorRepo{
		Direction: direction,
		Origin:    origin,
	}
	if err := a.cfg.AddMirrorRepo(mirrorKey, repoKey, repo); err != nil {
		return err
	}
	return a.saveConfig()
}

// DeleteMirrorRepo removes a repo from a mirror group.
func (a *App) DeleteMirrorRepo(mirrorKey, repoKey string) error {
	return a.be().DeleteMirrorRepo(mirrorKey, repoKey)
}

// deleteMirrorRepoLocal is the local backend's DeleteMirrorRepo.
func (a *App) deleteMirrorRepoLocal(mirrorKey, repoKey string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.cfg.DeleteMirrorRepo(mirrorKey, repoKey); err != nil {
		return err
	}
	return a.saveConfig()
}

// ListRemoteRepos returns the repo list for an account, for use in mirror repo pickers.
func (a *App) ListRemoteRepos(accountKey string) []DiscoverResult {
	return a.be().ListRemoteRepos(accountKey)
}

// listRemoteReposLocal is the local backend's ListRemoteRepos.
func (a *App) listRemoteReposLocal(accountKey string) []DiscoverResult {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()
	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repos, err := ops.ListRemoteRepos(ctx, acct, accountKey)
	if err != nil {
		return nil
	}

	results := make([]DiscoverResult, len(repos))
	for i, r := range repos {
		results[i] = DiscoverResult{
			FullName:    r.FullName,
			Description: r.Description,
			Private:     r.Private,
			Fork:        r.Fork,
			Archived:    r.Archived,
		}
	}
	return results
}

// ListAccountOrgs returns the organizations/namespaces available for an account.
// The first entry is always the personal username.
func (a *App) ListAccountOrgs(accountKey string) ([]string, error) {
	return a.be().ListAccountOrgs(accountKey)
}

// listAccountOrgsLocal is the local backend's ListAccountOrgs.
func (a *App) listAccountOrgsLocal(accountKey string) ([]string, error) {
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("account %q not found", accountKey)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return ops.ListAccountOrgs(ctx, acct, accountKey)
}

// CreateNewRepo creates a repository on the provider and optionally adds it
// to the config and clones it locally.
func (a *App) CreateNewRepo(accountKey, owner, repoName, description string, private, cloneAfter bool, root string) error {
	return a.be().CreateNewRepo(accountKey, owner, repoName, description, private, cloneAfter, root)
}

// createNewRepoLocal is the local backend's CreateNewRepo.
func (a *App) createNewRepoLocal(accountKey, owner, repoName, description string, private, cloneAfter bool, root string) error {
	// Checked before the repository is created on the provider (#241).
	if root != "" {
		if _, err := hostPath(root); err != nil {
			return err
		}
	}
	a.mu.Lock()
	acct, ok := a.cfg.Accounts[accountKey]
	a.mu.Unlock()
	if !ok {
		return fmt.Errorf("account %q not found", accountKey)
	}

	ctx, cancel := context.WithTimeout(context.Background(), createRepoTimeout)
	defer cancel()

	if err := ops.CreateRemoteRepo(ctx, acct, accountKey, owner, repoName, description, private); err != nil {
		return err
	}

	if cloneAfter {
		// Build the full repo key: owner/name.
		repoKey := owner + "/" + repoName

		// Add to config as a source and save.
		a.mu.Lock()
		sourceKey := accountKey
		src, srcOK := a.cfg.Sources[sourceKey]
		if !srcOK {
			src = config.Source{
				Account: accountKey,
				Repos:   make(map[string]config.Repo),
			}
		}
		src.Repos[repoKey] = config.Repo{}
		a.cfg.Sources[sourceKey] = src
		saveErr := a.saveConfig()
		a.mu.Unlock()
		if saveErr != nil {
			return fmt.Errorf("saving config: %w", saveErr)
		}

		// Trigger clone (reuses existing async clone flow).
		a.CloneRepo(sourceKey, repoKey, root)
	}

	return nil
}

// DiscoverMirrors scans all account pairs to detect mirror relationships.
// Runs async; emits "mirror:discover:done" event with results.
func (a *App) DiscoverMirrors() { a.be().DiscoverMirrors() }

// discoverMirrorsLocal is the local backend's DiscoverMirrors.
func (a *App) discoverMirrorsLocal() {
	go func() {
		a.mu.Lock()
		cfg := a.snapshotLocked()
		op := a.newOperation().atRevision(a.configRevision)
		a.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		if !a.anyServerReachable(ctx, cfg) {
			emit(op, "mirror:discover:done", mirrorDiscoverDoneDTO{Error: userMsg(errServerUnreachable)})
			return
		}
		results, err := mirror.DiscoverMirrors(ctx, cfg, func(p mirror.DiscoverProgress) {
			emit(op, "mirror:discover:progress", mirrorDiscoverProgressDTO(p))
		})
		if err != nil {
			emit(op, "mirror:discover:done", mirrorDiscoverDoneDTO{Error: userMsg(err)})
			return
		}
		emit(op, "mirror:discover:done", mirrorDiscoverDoneDTO{Results: results})
	}()
}

// ApplyDiscoveredMirrors merges discovered mirrors into the config.
func (a *App) ApplyDiscoveredMirrors() error { return a.be().ApplyDiscoveredMirrors() }

// applyDiscoveredMirrorsLocal is the local backend's ApplyDiscoveredMirrors.
func (a *App) applyDiscoveredMirrorsLocal() error {
	// The discovery talks to every server for up to two minutes: it runs on
	// a snapshot without the lock (#173). ApplyDiscovery only adds, so it is
	// safe against a config that changed meanwhile.
	a.mu.Lock()
	snap := a.snapshotLocked()
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), discoverMirrorsTimeout)
	defer cancel()

	discover := a.discoverMirrors
	if discover == nil {
		discover = mirror.DiscoverMirrors
	}
	results, err := discover(ctx, snap, nil)
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	added, _ := mirror.ApplyDiscovery(a.cfg, results)
	if added > 0 {
		return a.saveConfig()
	}
	return nil
}

// CheckMirrorCredentials checks whether an account has the portable PAT
// needed for mirror operations (remote servers can't use GCM OAuth tokens).
func (a *App) CheckMirrorCredentials(accountKey string) MirrorCredentialCheck {
	return a.be().CheckMirrorCredentials(accountKey)
}

// checkMirrorCredentialsLocal is the local backend's CheckMirrorCredentials.
func (a *App) checkMirrorCredentialsLocal(accountKey string) MirrorCredentialCheck {
	a.mu.Lock()
	acct, ok := a.cfg.GetAccountByKey(accountKey)
	a.mu.Unlock()

	if !ok {
		return MirrorCredentialCheck{AccountKey: accountKey, Message: "account not found"}
	}

	_, _, err := credential.ResolveMirrorToken(acct, accountKey)
	if err != nil {
		needsPAT := acct.DefaultCredentialType == "gcm"
		msg := "No portable PAT found."
		if needsPAT {
			msg = "GCM accounts need a separate PAT for mirrors. Store one with the token setup."
		}
		return MirrorCredentialCheck{
			AccountKey:     accountKey,
			HasMirrorToken: false,
			NeedsPAT:       needsPAT,
			Message:        msg,
		}
	}
	return MirrorCredentialCheck{
		AccountKey:     accountKey,
		HasMirrorToken: true,
		Message:        "Mirror token available.",
	}
}

// ── Doctor: external-tool detection ────────────────────────────────────────

// DoctorToolDTO mirrors one doctor.Result in a JSON-friendly shape for the
// Svelte frontend. Includes a "required" flag derived from the current
// config so the UI can highlight missing required tools.
type DoctorToolDTO struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Purpose     string `json:"purpose"`
	Found       bool   `json:"found"`
	Path        string `json:"path,omitempty"`
	Version     string `json:"version,omitempty"`
	Required    bool   `json:"required"`
	RequiredFor string `json:"requiredFor,omitempty"`
	InstallHint string `json:"installHint,omitempty"`
}

// DoctorReport is the full "System check" result: the tool list plus a
// rolled-up status for required tools and credential permissions.
type DoctorReport struct {
	CredentialFindings []credential.Finding `json:"credentialFindings"`
	Tools              []DoctorToolDTO      `json:"tools"`
	AllOK              bool                 `json:"allOk"`      // true when required tools and credential permissions pass
	MissingReq         int                  `json:"missingReq"` // count of required tools that are missing
	MissingOpt         int                  `json:"missingOpt"` // count of optional tools that are missing
}

// DoctorPrecheckDTO is the point-of-use answer for "is credential type X
// installable on this host?".
type DoctorPrecheckDTO struct {
	OK      bool            `json:"ok"`
	Summary string          `json:"summary,omitempty"`
	Hint    string          `json:"hint,omitempty"`
	Missing []DoctorToolDTO `json:"missing,omitempty"`
}

// DoctorRun probes every tool gitbox knows about and annotates each with
// whether it's required for the current config. Used by the GUI Settings
// "System check" row.
func (a *App) DoctorRun() DoctorReport { return a.be().DoctorRun() }

// doctorRunLocal is the local backend's DoctorRun.
func (a *App) doctorRunLocal() DoctorReport {
	a.mu.Lock()
	cfg := a.snapshotLocked()
	a.mu.Unlock()

	results := doctor.Check(doctor.StandardTools())
	required := gatherRequiredTools(cfg)

	findings := a.refreshCredentialAudit()
	return makeDoctorReport(results, required, findings)
}

func makeDoctorReport(results []doctor.Result, required map[string]string, findings []credential.Finding) DoctorReport {
	report := DoctorReport{Tools: make([]DoctorToolDTO, 0, len(results)), CredentialFindings: findings, AllOK: len(findings) == 0}
	for _, r := range results {
		reason, isRequired := required[r.Tool.Name]
		dto := DoctorToolDTO{
			Name:        r.Tool.Name,
			DisplayName: r.Tool.DisplayName,
			Purpose:     r.Tool.Purpose,
			Found:       r.Found,
			Path:        r.Path,
			Version:     r.Version,
			Required:    isRequired,
			RequiredFor: reason,
			InstallHint: r.InstallHint(),
		}
		report.Tools = append(report.Tools, dto)
		if !r.Found {
			if isRequired {
				report.AllOK = false
				report.MissingReq++
			} else {
				report.MissingOpt++
			}
		}
	}
	return report
}

// DoctorPrecheck runs a point-of-use check for a single credential type.
// Called by the GUI add-account / change-credential-type flows BEFORE the
// user commits, so they learn about a missing dependency with an install
// command instead of hitting a cryptic error at auth time.
func (a *App) DoctorPrecheck(credentialType string) DoctorPrecheckDTO {
	return a.be().DoctorPrecheck(credentialType)
}

// doctorPrecheckLocal is the local backend's DoctorPrecheck.
func (a *App) doctorPrecheckLocal(credentialType string) DoctorPrecheckDTO {
	pc := doctor.PrecheckForCredentialType(credentialType)
	out := DoctorPrecheckDTO{OK: pc.OK, Summary: pc.Summary, Hint: pc.Hint}
	for _, m := range pc.Missing {
		out.Missing = append(out.Missing, DoctorToolDTO{
			Name:        m.Tool.Name,
			DisplayName: m.Tool.DisplayName,
			Purpose:     m.Tool.Purpose,
			Found:       false,
			InstallHint: m.InstallHint(),
		})
	}
	return out
}

// gatherRequiredTools reads the config and returns, for each tool name,
// the reason it is required (or not in the map at all if purely optional).
func gatherRequiredTools(cfg *config.Config) map[string]string {
	required := map[string]string{
		"git": "always — core dependency",
	}
	if cfg == nil {
		return required
	}
	var anyGCM, anySSH bool
	for _, acct := range cfg.Accounts {
		switch acct.DefaultCredentialType {
		case "gcm":
			anyGCM = true
		case "ssh":
			anySSH = true
		}
	}
	if anyGCM {
		required["git-credential-manager"] = "you have accounts using the gcm credential type"
	}
	if anySSH {
		required["ssh"] = "you have accounts using the ssh credential type"
		required["ssh-keygen"] = "you have accounts using the ssh credential type"
		required["ssh-add"] = "you have accounts using the ssh credential type"
	}
	return required
}
