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
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
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
	// Host is the server of the remote, for adding a missing account.
	Host string `json:"host,omitempty"`
	// MainClone is the clone a worktree row belongs to, when known.
	MainClone string `json:"mainClone,omitempty"`
	// TrackedPath is where an already-tracked repository lives.
	TrackedPath string `json:"trackedPath,omitempty"`
}

// ReconcileDTO answers a reconcile pass.
type ReconcileDTO struct {
	Adopted        int             `json:"adopted"`
	Repairs        []RepairDTO     `json:"repairs,omitempty"`
	Relinked       int             `json:"relinked"`
	Unmatched      []OrphanRepoDTO `json:"unmatched"`
	Ignored        []OrphanRepoDTO `json:"ignored"`
	SkippedFolders int             `json:"skippedFolders,omitempty"`
	// SkippedScans says which folders the scans skipped and why, grouped.
	SkippedScans []SkippedScanDTO `json:"skippedScans,omitempty"`
	Skipped      []reconcile.Skip `json:"skipped,omitempty"`
	Background   bool             `json:"background,omitempty"` // completion of a coalesced retry
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

// SkippedScanDTO groups the folders one scan skipped for one cause: below
// its depth limit, or unreadable.
type SkippedScanDTO struct {
	Kind       string `json:"kind"`  // adopt.ScanRoot, ScanContainer or ScanWorkspace
	Under      string `json:"under"` // the root, container or workspace folder
	Depth      int    `json:"depth"` // the scan's depth limit
	Unreadable bool   `json:"unreadable"`
	Count      int    `json:"count"`
	// Folders are relative to Under, the first maxSkippedListed of Count.
	Folders []string `json:"folders"`
}

// maxSkippedListed bounds the folders one group lists.
const maxSkippedListed = 50

// groupSkipped groups skipped folders by scan and cause, in scan order.
func groupSkipped(folders []adopt.SkippedFolder) []SkippedScanDTO {
	var out []SkippedScanDTO
	index := map[string]int{}
	for _, f := range folders {
		key := fmt.Sprintf("%s\x00%s\x00%t", f.Kind, f.Under, f.Unreadable)
		i, ok := index[key]
		if !ok {
			i = len(out)
			index[key] = i
			out = append(out, SkippedScanDTO{Kind: f.Kind, Under: f.Under, Depth: f.Depth, Unreadable: f.Unreadable, Folders: []string{}})
		}
		g := &out[i]
		g.Count++
		if len(g.Folders) < maxSkippedListed {
			rel, err := filepath.Rel(f.Under, f.Path)
			if err != nil || strings.HasPrefix(rel, "..") {
				rel = f.Path
			}
			g.Folders = append(g.Folders, rel)
		}
	}
	return out
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
		Host: o.Host,
	}
}

// toUnmatchedDTO adds the row's what-to-do details to its orphan DTO.
func toUnmatchedDTO(u reconcile.Unmatched) OrphanRepoDTO {
	dto := toOrphanDTO(u.OrphanRepo, u.Reason)
	dto.MainClone, dto.TrackedPath = u.MainClone, u.TrackedPath
	return dto
}

// RepairDTO describes the .git/config repair of a saved adoption.
type RepairDTO struct {
	Source   string   `json:"source"`
	Repo     string   `json:"repo"`
	Fixed    []string `json:"fixed,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// reconcileServices are the App services a reconcile pass still calls,
// passed in until refresh (#369), clone repair (#370) and events (#371)
// move to the session. Each one runs where the code on *App called it.
type reconcileServices struct {
	// refreshContext is App.refreshContext, read when a scan starts.
	refreshContext func() context.Context
	// retry is App.retryReconcileAfterRefresh.
	retry func()
	// save is App.saveConfig and snapshot App.refreshConfigLocked, both
	// called under s.mu.
	save     func() error
	snapshot func() *config.Config
	// repair is App.repairAdoptions, called without s.mu.
	repair func(*config.Config, []reconcile.Placed) []RepairDTO
	// publish emits "reconcile:done".
	publish func(ReconcileDTO)
}

// reconcileServices binds the services a reconcile pass needs to a.
func (a *App) reconcileServices() reconcileServices {
	return reconcileServices{
		refreshContext: a.refreshContext,
		retry:          a.retryReconcileAfterRefresh,
		save:           a.saveConfig,
		snapshot:       a.refreshConfigLocked,
		repair:         a.repairAdoptions,
		publish:        func(out ReconcileDTO) { emit(a.newOperation(), "reconcile:done", out) },
	}
}

// The caller holds s.mu. Capture the inputs for heal only after a successful
// save and only when there are new clones to repair.
func (s *localSession) savePlacementsLocked(before *config.Config, placed []reconcile.Placed, svc reconcileServices) (*config.Config, error) {
	if err := svc.save(); err != nil {
		if !isConfigChanged(err) {
			*s.cfg = *before
		}
		return nil, err
	}
	if len(placed) == 0 {
		return nil, nil
	}
	return svc.snapshot(), nil
}

func (a *App) repairAdoptions(snap *config.Config, placed []reconcile.Placed) []RepairDTO {
	var repairs []RepairDTO
	for _, p := range placed {
		report, current := a.repairClone(snap, p.Source, p.Repo)
		row := RepairDTO{Source: p.Source, Repo: p.Repo, Fixed: report.Fixed, Warnings: report.Warnings}
		if !current {
			row.Warnings = []string{"repository settings changed before repair"}
		} else if report.Skipped != "" {
			row.Warnings = append(row.Warnings, userText(report.Skipped))
		}
		repairs = append(repairs, row)
	}
	return repairs
}

// ErrStalePlan means neither scan could safely use the new configuration.
var ErrStalePlan = errors.New("reconcile plan is stale")

// scanConfig snapshots the config and runs the reconcile scan without the
// lock. Changes to the scan scope require a fresh scan. Other edits are
// validated per entry by Apply under s.mu, which the caller releases. It
// also returns the live pre-apply snapshot for rollback if persistence fails.
// refreshContext is App.refreshContext.
func (s *localSession) scanConfig(refreshContext func() context.Context) (reconcile.Result, *config.Config, error) {
	scan := s.reconcileScan
	if scan == nil {
		scan = func(cfg *config.Config) (reconcile.Result, error) {
			return reconcile.PlanContext(refreshContext(), cfg)
		}
	}
	return s.scanConfigWith(scan, true)
}

func (s *localSession) scanConfigWith(scan func(*config.Config) (reconcile.Result, error), perEntry bool) (reconcile.Result, *config.Config, error) {
	clone := s.reconcileClone
	if clone == nil {
		clone = (*config.Config).Clone
	}
	for attempt := range 2 {
		s.mu.Lock()
		snap, err := clone(s.cfg)
		s.mu.Unlock()
		if err != nil {
			s.mu.Lock()
			return reconcile.Result{}, nil, err
		}
		res, err := scan(snap)
		s.mu.Lock()
		if err != nil {
			return reconcile.Result{}, nil, err
		}
		cur, err := clone(s.cfg)
		if err != nil {
			return reconcile.Result{}, nil, err
		}
		if config.EqualIgnoringUIState(cur, snap) || (perEntry && res.SameScanScope(cur)) {
			return res, cur, nil
		}
		if attempt == 0 {
			s.mu.Unlock()
		}
	}
	return reconcile.Result{}, nil, ErrStalePlan
}

// Reconcile runs one pass: re-link moved clones, adopt confident ones in
// place, and return what needs me. Emits "reconcile:done" when the config
// or workspace availability changed.
func (a *App) Reconcile() ReconcileDTO { return a.be().Reconcile() }

// reconcileLocal is the local backend's Reconcile.
func (s *localSession) reconcileLocal(svc reconcileServices) ReconcileDTO {
	return s.runReconcilePass(true, svc)
}

// reconcilePass drives one pass of the session with this App's services;
// the refresh retry calls it.
func (a *App) reconcilePass(retryStale bool) ReconcileDTO {
	return a.localSession.runReconcilePass(retryStale, a.reconcileServices())
}

func (s *localSession) runReconcilePass(retryStale bool, svc reconcileServices) ReconcileDTO {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	if !retryStale && svc.refreshContext().Err() != nil {
		return emptyReconcile("")
	}

	s.mu.Lock()
	ready := s.cfg != nil && s.cfgLoaded
	s.mu.Unlock()
	if !ready {
		return emptyReconcile("")
	}

	choices, unknown, wsChanged, wsSkipped, wsErr := s.syncWorkspaces(svc.save)
	unknownChanged := unknown != nil && !slices.Equal(s.workspaceUnknown, unknown)
	if unknownChanged {
		s.workspaceUnknown = unknown
	}
	if retryStale && wsSkipped {
		svc.retry()
	}
	res, before, err := s.scanConfig(svc.refreshContext)
	if err != nil {
		s.mu.Unlock()
		if retryStale && errors.Is(err, ErrStalePlan) {
			svc.retry()
		}
		out := emptyReconcile(userMsg(err))
		out.WorkspaceUnknown = unknown
		if unknownChanged {
			svc.publish(out)
		}
		return out
	}
	applied := reconcile.Apply(s.cfg, res)
	placed := applied.Adopted
	changed := len(placed)+applied.Relinked > 0
	var snap *config.Config
	saveErr := wsErr
	if changed {
		snap, err = s.savePlacementsLocked(before, placed, svc)
		if err != nil {
			saveErr = err
			placed, applied.Relinked, changed = nil, 0, false
		}
	}
	s.mu.Unlock()

	out := emptyReconcile("")
	out.Repairs = svc.repair(snap, placed)
	out.Adopted, out.Relinked, out.WorkspacesChanged = len(placed), applied.Relinked, wsChanged
	out.SkippedFolders = len(res.SkippedFolders)
	out.SkippedScans = groupSkipped(res.SkippedFolders)
	out.Skipped, out.Background = applied.Skipped, !retryStale
	if retryStale && len(applied.Skipped) > 0 {
		svc.retry()
	}
	out.WorkspaceUnknown = unknown
	for _, c := range choices {
		out.WorkspaceChoices = append(out.WorkspaceChoices, WorkspaceChoiceDTO{Source: c.Source, Repo: c.Repo, Files: c.Files})
	}
	out.Placeholders, out.WorkspaceMembers, out.WorkspaceDiagnostics = s.workspaceTree()
	for _, u := range res.Unmatched {
		out.Unmatched = append(out.Unmatched, toUnmatchedDTO(u))
	}
	for _, u := range res.Ignored {
		out.Ignored = append(out.Ignored, toUnmatchedDTO(u))
	}
	for _, c := range res.Incomplete {
		out.Incomplete = append(out.Incomplete, IncompleteCloneDTO{Source: c.Source, Repo: c.Repo, Path: c.Path})
	}
	if saveErr != nil {
		out.Error = userMsg(saveErr)
	}
	if changed || wsChanged || unknownChanged || out.Background || len(out.Skipped) > 0 {
		svc.publish(out)
	}
	return out
}

// syncWorkspaces runs workspace.Sync on a snapshot without the lock, then
// applies its changes to the live config and saves with save, under s.mu.
// The caller holds s.reconcileMu.
func (s *localSession) syncWorkspaces(save func() error) (choices []workspace.Choice, unknown []string, changed, skipped bool, err error) {
	s.mu.Lock()
	snap, err := s.cfg.Clone()
	s.mu.Unlock()
	if err != nil {
		return nil, nil, false, false, err
	}
	ws := workspace.Sync(snap)
	s.mu.Lock()
	defer s.mu.Unlock()
	changed, stale := s.workspaceSync.Apply(s.cfg, ws.Changes)
	unknown = []string{}
	for _, u := range ws.Unknown {
		repo, ok := s.cfg.Sources[u.Source].Repos[u.Repo]
		if ok && (repo.Workspace != "" || repo.WorkspacePreferred != "") &&
			status.RepoPath(s.cfg, u.Source, u.Repo, repo) == u.Path {
			unknown = append(unknown, u.Source+"/"+u.Repo)
		}
	}
	for _, choice := range ws.Choices {
		if !slices.Contains(stale, status.RepoRef{Source: choice.Source, Repo: choice.Repo}) {
			choices = append(choices, choice)
		}
	}
	if changed {
		err = save()
	}
	return choices, unknown, changed, len(stale) > 0, err
}

// workspaceTree inspects selected workspaces without holding the session lock.
func (s *localSession) workspaceTree() ([]WorkspaceFolderDTO, []string, []WorkspaceDiagnosticDTO) {
	placeholders, members := []WorkspaceFolderDTO{}, []string{}
	s.mu.Lock()
	snap, err := s.cfg.Clone()
	s.mu.Unlock()
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

// adoptClonesLocal is the local backend's AdoptClones. Its fresh pass is the
// local backend's Reconcile.
func (s *localSession) adoptClonesLocal(reqs []AdoptRequestDTO, svc reconcileServices) ReconcileDTO {
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
	s.reconcileMu.Lock()
	res, workspaceRows, before, err := s.scanAdoptionRequests(reqs)
	if err != nil {
		s.mu.Unlock()
		s.reconcileMu.Unlock()
		return emptyReconcile(userMsg(err))
	}
	var placed []reconcile.Placed
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
			source, err := reconcile.Adopt(s.cfg, u.OrphanRepo, req.Account)
			if err != nil {
				errs = append(errs, userMsg(err))
				break
			}
			s.cfg.IgnoreClone(u.Path, false)
			placed = append(placed, reconcile.Placed{Account: req.Account, Source: source, Repo: u.RepoKey, Path: u.Path})
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
	var snap *config.Config
	if len(placed) > 0 {
		snap, err = s.savePlacementsLocked(before, placed, svc)
	}
	s.mu.Unlock()
	s.reconcileMu.Unlock()
	if err != nil {
		return emptyReconcile(userMsg(err))
	}

	repairs := svc.repair(snap, placed)
	out := s.reconcileLocal(svc)
	out.Adopted += len(placed)
	out.Repairs = append(repairs, out.Repairs...)
	if len(errs) > 0 && out.Error == "" {
		out.Error = errs[0]
	}
	return out
}

// scanAdoptionRequests revalidates deliberate workspace searches outside the
// config lock, under the same snapshot check as ordinary adoption.
func (s *localSession) scanAdoptionRequests(reqs []AdoptRequestDTO) (reconcile.Result, map[string][]reconcile.Unmatched, *config.Config, error) {
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
		res, before, err := s.scanConfigWith(reconcile.Plan, false)
		return res, nil, before, err
	}
	var external map[string][]reconcile.Unmatched
	res, before, err := s.scanConfigWith(func(cfg *config.Config) (reconcile.Result, error) {
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
	return res, external, before, err
}

// IgnoreClone remembers (or forgets) that I don't want the clone at path
// tracked, and persists.
func (a *App) IgnoreClone(path string, ignore bool) error { return a.be().IgnoreClone(path, ignore) }

// ignoreCloneLocal is the local backend's IgnoreClone.
func (s *localSession) ignoreCloneLocal(path string, ignore bool, save func() error) error {
	path, err := hostPath(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.IgnoreClone(path, ignore) {
		return save()
	}
	return nil
}
