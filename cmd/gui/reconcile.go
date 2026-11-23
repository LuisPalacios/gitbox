package main

// Wails bindings for the disk reconcile (issue #149): keep gitbox.json in
// step with the clones under the roots. A pass snapshots the config, scans
// without the lock (pkg/reconcile.Plan), re-checks the config and applies
// the decisions under the lock. Confident clones are adopted and moved clones
// re-linked silently; the rest comes back as the Unmatched list for me to
// adopt or ignore. Each pass first syncs containers with the
// .code-workspace files at their roots (pkg/workspace), so a container that
// just gained one has its nested clones adopted in the same pass.

import (
	"errors"
	"fmt"
	"slices"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/reconcile"
	"github.com/LuisPalacios/gitbox/pkg/status"
	"github.com/LuisPalacios/gitbox/pkg/workspace"
)

// OrphanRepoDTO describes a clone found under a root that the config
// doesn't track.
type OrphanRepoDTO struct {
	Path           string   `json:"path"`
	RelPath        string   `json:"relPath"`
	RemoteURL      string   `json:"remoteURL"`
	RepoKey        string   `json:"repoKey"`
	MatchedAccount string   `json:"matchedAccount"`
	Reason         string   `json:"reason"`               // reconcile.Reason*
	Candidates     []string `json:"candidates,omitempty"` // accounts it could go to, best first
	Nested         bool     `json:"nested"`
	WorkspaceRoot  string   `json:"workspaceRoot,omitempty"`
}

// ReconcileDTO answers a reconcile pass.
type ReconcileDTO struct {
	Adopted    int              `json:"adopted"`
	Relinked   int              `json:"relinked"`
	Unmatched  []OrphanRepoDTO  `json:"unmatched"`
	Ignored    []OrphanRepoDTO  `json:"ignored"`
	Skipped    []reconcile.Skip `json:"skipped,omitempty"`
	Background bool             `json:"background,omitempty"` // completion of a coalesced retry
	// WorkspacesChanged is set when the pass flagged a container or changed
	// a repo's workspace file.
	WorkspacesChanged bool                 `json:"workspacesChanged"`
	WorkspaceChoices  []WorkspaceChoiceDTO `json:"workspaceChoices"`
	Placeholders      []WorkspaceFolderDTO `json:"placeholders"`
	// WorkspaceMembers lists the clones ("source/repo") a container's
	// workspace file lists inside it.
	WorkspaceMembers []string `json:"workspaceMembers"`
	// WorkspaceUnknown lists the saved workspaces whose clone roots could not be read.
	WorkspaceUnknown     []string                 `json:"workspaceUnknown"`
	WorkspaceDiagnostics []WorkspaceDiagnosticDTO `json:"workspaceDiagnostics,omitempty"`
	// Incomplete lists the tracked clones git never finished (#295): a
	// clone killed mid-transfer. Their folders are left as they are.
	Incomplete []IncompleteCloneDTO `json:"incomplete"`
	Error      string               `json:"error,omitempty"`
}

// IncompleteCloneDTO is a tracked repo whose clone git stopped before it
// finished.
type IncompleteCloneDTO struct {
	Source string `json:"source"`
	Repo   string `json:"repo"`
	Path   string `json:"path"`
}

// WorkspaceChoiceDTO is a container whose root holds several workspace
// files and no pick yet.
type WorkspaceChoiceDTO struct {
	Source string   `json:"source"`
	Repo   string   `json:"repo"`
	Files  []string `json:"files"`
}

// WorkspaceFolderDTO describes a waiting or outside workspace folder.
// Parent is the container's "source/repo" key.
type WorkspaceFolderDTO struct {
	Parent      string `json:"parent"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	OutsidePath string `json:"outsidePath,omitempty"`
}

type WorkspaceDiagnosticDTO struct {
	Parent  string `json:"parent"`
	File    string `json:"file"`
	Problem string `json:"problem"`
}

// emptyReconcile is a pass with no results, its lists non-nil for the
// frontend.
func emptyReconcile(errText string) ReconcileDTO {
	return ReconcileDTO{Unmatched: []OrphanRepoDTO{}, Ignored: []OrphanRepoDTO{},
		WorkspaceChoices: []WorkspaceChoiceDTO{}, Placeholders: []WorkspaceFolderDTO{}, WorkspaceMembers: []string{},
		Incomplete: []IncompleteCloneDTO{}, Error: errText}
}

// AdoptRequestDTO asks to adopt the clone at Path into Account.
type AdoptRequestDTO struct {
	Path          string `json:"path"`
	Account       string `json:"account"`
	WorkspaceRoot string `json:"workspaceRoot,omitempty"`
}

func toOrphanDTO(o adopt.OrphanRepo, reason string) OrphanRepoDTO {
	return OrphanRepoDTO{
		Path: o.Path, RelPath: o.RelPath, RemoteURL: o.RemoteURL, RepoKey: o.RepoKey,
		MatchedAccount: o.MatchedAccount, Reason: reason, Candidates: o.Candidates, Nested: o.Nested,
	}
}

// repoSetup is the .git/config work for a clone just added to the config,
// captured under the lock and run after it.
type repoSetup struct {
	path, accountKey, repoKey, credType string
	acct                                config.Account
	global                              config.GlobalConfig
}

func setupFor(cfg *config.Config, p reconcile.Placed) repoSetup {
	acct := cfg.Accounts[p.Account]
	repo := cfg.Sources[p.Source].Repos[p.Repo]
	return repoSetup{path: p.Path, accountKey: p.Account, repoKey: p.Repo, credType: repo.EffectiveCredentialType(&acct), acct: detachAccount(acct), global: detachCredentials(cfg.Global)}
}

// detachAccount copies the credential blocks an account points at, so a
// copy used after a.mu is released shares nothing with the live config
// (#173).
func detachAccount(acct config.Account) config.Account {
	if acct.SSH != nil {
		v := *acct.SSH
		acct.SSH = &v
	}
	if acct.GCM != nil {
		v := *acct.GCM
		acct.GCM = &v
	}
	return acct
}

// detachCredentials does the same for the global credential blocks that
// the .git/config setup reads.
func detachCredentials(g config.GlobalConfig) config.GlobalConfig {
	if g.CredentialSSH != nil {
		v := *g.CredentialSSH
		g.CredentialSSH = &v
	}
	if g.CredentialGCM != nil {
		v := *g.CredentialGCM
		g.CredentialGCM = &v
	}
	if g.CredentialToken != nil {
		v := *g.CredentialToken
		g.CredentialToken = &v
	}
	return g
}

// run points origin, the credential helper and the identity at the account,
// as adoption always did.
func (s repoSetup) run() {
	_ = git.SetRemoteURL(s.path, "origin", adopt.PlainRemoteURL(s.acct, s.repoKey, s.credType))
	_ = credential.ConfigureRepoCredential(s.path, s.acct, s.accountKey, s.credType, s.global)
	_ = git.ConfigSet(s.path, "user.name", s.acct.Name)
	_ = git.ConfigSet(s.path, "user.email", s.acct.Email)
}

// ErrStalePlan means neither scan could safely use the new configuration.
var ErrStalePlan = errors.New("reconcile plan is stale")

// scanConfig snapshots the config and runs the reconcile scan without the
// lock. Changes to the scan scope require a fresh scan. Other edits are
// validated per entry by Apply under a.mu, which the caller releases. It
// also returns the live pre-apply snapshot for rollback if persistence fails.
func (a *App) scanConfig() (reconcile.Result, *config.Config, error) {
	scan := a.reconcileScan
	if scan == nil {
		scan = reconcile.Plan
	}
	return a.scanConfigWith(scan, true)
}

func (a *App) scanConfigWith(scan func(*config.Config) (reconcile.Result, error), perEntry bool) (reconcile.Result, *config.Config, error) {
	clone := a.reconcileClone
	if clone == nil {
		clone = (*config.Config).Clone
	}
	for attempt := range 2 {
		a.mu.Lock()
		snap, err := clone(a.cfg)
		a.mu.Unlock()
		if err != nil {
			a.mu.Lock()
			return reconcile.Result{}, nil, err
		}
		res, err := scan(snap)
		a.mu.Lock()
		if err != nil {
			return reconcile.Result{}, nil, err
		}
		cur, err := clone(a.cfg)
		if err != nil {
			return reconcile.Result{}, nil, err
		}
		if config.EqualIgnoringUIState(cur, snap) || (perEntry && res.SameScanScope(cur)) {
			return res, cur, nil
		}
		if attempt == 0 {
			a.mu.Unlock()
		}
	}
	return reconcile.Result{}, nil, ErrStalePlan
}

// Reconcile runs one pass: re-link moved clones, adopt confident ones in
// place, and return what needs me. Emits "reconcile:done" when the config
// or workspace availability changed.
func (a *App) Reconcile() ReconcileDTO { return a.be().Reconcile() }

// reconcileLocal is the local backend's Reconcile.
func (a *App) reconcileLocal() ReconcileDTO {
	return a.reconcilePass(true)
}

func (a *App) reconcilePass(retryStale bool) ReconcileDTO {
	a.reconcileMu.Lock()
	defer a.reconcileMu.Unlock()
	if !retryStale && a.refreshContext().Err() != nil {
		return emptyReconcile("")
	}

	a.mu.Lock()
	ready := a.cfg != nil && a.cfgLoaded
	a.mu.Unlock()
	if !ready {
		return emptyReconcile("")
	}

	choices, unknown, wsChanged, wsSkipped, wsErr := a.syncWorkspaces()
	unknownChanged := unknown != nil && !slices.Equal(a.workspaceUnknown, unknown)
	if unknownChanged {
		a.workspaceUnknown = unknown
	}
	if retryStale && wsSkipped {
		a.retryReconcileAfterRefresh()
	}
	res, before, err := a.scanConfig()
	if err != nil {
		a.mu.Unlock()
		if retryStale && errors.Is(err, ErrStalePlan) {
			a.retryReconcileAfterRefresh()
		}
		out := emptyReconcile(userMsg(err))
		out.WorkspaceUnknown = unknown
		if unknownChanged {
			a.sink().Emit("reconcile:done", out)
		}
		return out
	}
	applied := reconcile.Apply(a.cfg, res)
	placed := applied.Adopted
	changed := len(placed)+applied.Relinked > 0
	saveErr := wsErr
	if changed {
		if err := a.saveConfig(); err != nil {
			saveErr = err
			if !isConfigChanged(err) {
				*a.cfg = *before
			}
			placed, applied.Relinked, changed = nil, 0, false
		}
	}
	setups := make([]repoSetup, 0, len(placed))
	for _, p := range placed {
		setups = append(setups, setupFor(a.cfg, p))
	}
	a.mu.Unlock()

	for _, s := range setups {
		s.run()
	}
	out := emptyReconcile("")
	out.Adopted, out.Relinked, out.WorkspacesChanged = len(placed), applied.Relinked, wsChanged
	out.Skipped, out.Background = applied.Skipped, !retryStale
	if retryStale && len(applied.Skipped) > 0 {
		a.retryReconcileAfterRefresh()
	}
	out.WorkspaceUnknown = unknown
	for _, c := range choices {
		out.WorkspaceChoices = append(out.WorkspaceChoices, WorkspaceChoiceDTO{Source: c.Source, Repo: c.Repo, Files: c.Files})
	}
	out.Placeholders, out.WorkspaceMembers, out.WorkspaceDiagnostics = a.workspaceTree()
	for _, u := range res.Unmatched {
		out.Unmatched = append(out.Unmatched, toOrphanDTO(u.OrphanRepo, u.Reason))
	}
	for _, u := range res.Ignored {
		out.Ignored = append(out.Ignored, toOrphanDTO(u.OrphanRepo, u.Reason))
	}
	for _, c := range res.Incomplete {
		out.Incomplete = append(out.Incomplete, IncompleteCloneDTO{Source: c.Source, Repo: c.Repo, Path: c.Path})
	}
	if saveErr != nil {
		out.Error = userMsg(saveErr)
	}
	if changed || wsChanged || unknownChanged || out.Background || len(out.Skipped) > 0 {
		a.sink().Emit("reconcile:done", out)
	}
	return out
}

// syncWorkspaces runs workspace.Sync on a snapshot without the lock, then
// applies its changes to the live config and saves.
func (a *App) syncWorkspaces() (choices []workspace.Choice, unknown []string, changed, skipped bool, err error) {
	a.mu.Lock()
	snap, err := a.cfg.Clone()
	a.mu.Unlock()
	if err != nil {
		return nil, nil, false, false, err
	}
	ws := workspace.Sync(snap)
	a.mu.Lock()
	defer a.mu.Unlock()
	changed, stale := a.workspaceSync.Apply(a.cfg, ws.Changes)
	unknown = []string{}
	for _, u := range ws.Unknown {
		repo, ok := a.cfg.Sources[u.Source].Repos[u.Repo]
		if ok && (repo.Workspace != "" || repo.WorkspacePreferred != "") &&
			status.RepoPath(a.cfg, u.Source, u.Repo, repo) == u.Path {
			unknown = append(unknown, u.Source+"/"+u.Repo)
		}
	}
	for _, choice := range ws.Choices {
		if !slices.Contains(stale, status.RepoRef{Source: choice.Source, Repo: choice.Repo}) {
			choices = append(choices, choice)
		}
	}
	if changed {
		err = a.saveConfig()
	}
	return choices, unknown, changed, len(stale) > 0, err
}

// workspaceTree inspects selected workspaces without holding the app lock.
func (a *App) workspaceTree() ([]WorkspaceFolderDTO, []string, []WorkspaceDiagnosticDTO) {
	placeholders, members := []WorkspaceFolderDTO{}, []string{}
	a.mu.Lock()
	snap, err := a.cfg.Clone()
	a.mu.Unlock()
	if err != nil {
		return placeholders, members, nil
	}
	tree := workspace.Inspect(snap)
	for _, p := range tree.Placeholders {
		placeholders = append(placeholders, WorkspaceFolderDTO{Parent: p.Parent.Source + "/" + p.Parent.Repo, Name: p.Name, Path: p.Path, OutsidePath: p.OutsidePath})
	}
	for _, r := range tree.Members {
		members = append(members, r.Source+"/"+r.Repo)
	}
	var issues []WorkspaceDiagnosticDTO
	for _, d := range tree.Diagnostics {
		issues = append(issues, WorkspaceDiagnosticDTO{Parent: d.Parent.Source + "/" + d.Parent.Repo, File: d.File, Problem: d.Problem})
	}
	return placeholders, members, issues
}

// AdoptClones adopts unmatched or ignored clones into the accounts I
// picked, in place (an ignored one stops being ignored), then runs a fresh
// pass and returns it.
func (a *App) AdoptClones(reqs []AdoptRequestDTO) ReconcileDTO { return a.be().AdoptClones(reqs) }

// adoptClonesLocal is the local backend's AdoptClones.
func (a *App) adoptClonesLocal(reqs []AdoptRequestDTO) ReconcileDTO {
	reqs = slices.Clone(reqs)
	for i, req := range reqs {
		path, err := hostPath(req.Path)
		if err != nil {
			return emptyReconcile(userMsg(err))
		}
		reqs[i].Path = path
		if req.WorkspaceRoot != "" {
			root, err := hostPath(req.WorkspaceRoot)
			if err != nil {
				return emptyReconcile(userMsg(err))
			}
			reqs[i].WorkspaceRoot = root
		}
	}
	a.reconcileMu.Lock()
	res, workspaceRows, err := a.scanAdoptionRequests(reqs)
	if err != nil {
		a.mu.Unlock()
		a.reconcileMu.Unlock()
		return emptyReconcile(userMsg(err))
	}
	var setups []repoSetup
	var errs []string
	for _, req := range reqs {
		found := false
		rows := append(append([]reconcile.Unmatched{}, res.Unmatched...), res.Ignored...)
		if req.WorkspaceRoot != "" {
			rows = workspaceRows[req.WorkspaceRoot]
		}
		for _, u := range rows {
			if req.WorkspaceRoot == "" {
				if !config.SamePath(u.Path, req.Path) {
					continue
				}
			} else if u.Path != req.Path || u.Reason == reconcile.ReasonWorktree || !slices.Contains(u.Candidates, req.Account) {
				continue
			}
			found = true
			source, err := reconcile.Adopt(a.cfg, u.OrphanRepo, req.Account)
			if err != nil {
				errs = append(errs, userMsg(err))
				break
			}
			a.cfg.IgnoreClone(u.Path, false)
			setups = append(setups, setupFor(a.cfg, reconcile.Placed{Account: req.Account, Source: source, Repo: u.RepoKey, Path: u.Path}))
			break
		}
		if !found {
			if req.WorkspaceRoot != "" {
				errs = append(errs, "The clone or selected account is no longer available for adoption")
			} else {
				errs = append(errs, fmt.Sprintf("%s is no longer waiting to be adopted", req.Path))
			}
		}
	}
	var saveErr error
	if len(setups) > 0 {
		saveErr = a.saveConfig()
	}
	a.mu.Unlock()
	a.reconcileMu.Unlock()

	for _, s := range setups {
		s.run()
	}
	out := a.Reconcile()
	out.Adopted += len(setups)
	if saveErr != nil {
		errs = append(errs, userMsg(saveErr))
	}
	if len(errs) > 0 && out.Error == "" {
		out.Error = errs[0]
	}
	return out
}

// scanAdoptionRequests revalidates deliberate workspace searches outside the
// config lock, under the same snapshot check as ordinary adoption.
func (a *App) scanAdoptionRequests(reqs []AdoptRequestDTO) (reconcile.Result, map[string][]reconcile.Unmatched, error) {
	roots := map[string]bool{}
	ordinary := len(reqs) == 0
	for _, req := range reqs {
		if req.WorkspaceRoot == "" {
			ordinary = true
		} else {
			roots[req.WorkspaceRoot] = true
		}
	}
	if len(roots) == 0 {
		res, _, err := a.scanConfigWith(reconcile.Plan, false)
		return res, nil, err
	}
	var external map[string][]reconcile.Unmatched
	res, _, err := a.scanConfigWith(func(cfg *config.Config) (reconcile.Result, error) {
		tree := workspace.Inspect(cfg)
		external = make(map[string][]reconcile.Unmatched, len(roots))
		for root := range roots {
			rows, _, err := reconcile.WorkspaceCandidates(cfg, tree, root)
			if err != nil {
				return reconcile.Result{}, err
			}
			external[root] = rows
		}
		if ordinary {
			return reconcile.Plan(cfg)
		}
		return reconcile.Result{}, nil
	}, false)
	return res, external, err
}

// IgnoreClone remembers (or forgets) that I don't want the clone at path
// tracked, and persists.
func (a *App) IgnoreClone(path string, ignore bool) error { return a.be().IgnoreClone(path, ignore) }

// ignoreCloneLocal is the local backend's IgnoreClone.
func (a *App) ignoreCloneLocal(path string, ignore bool) error {
	path, err := hostPath(path)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.IgnoreClone(path, ignore) {
		return a.saveConfig()
	}
	return nil
}
