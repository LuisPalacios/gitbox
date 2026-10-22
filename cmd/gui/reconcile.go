package main

// Wails bindings for the disk reconcile (issue #149): keep gitbox.json in
// step with the clones under the roots. A pass snapshots the config, scans
// without the lock (pkg/reconcile.Plan), re-checks the config and applies
// the decisions under the lock, the same pattern as workspace discovery.
// Confident clones are adopted and moved clones re-linked silently; the rest
// comes back as the Unmatched list for me to adopt or ignore.

import (
	"fmt"
	"reflect"

	"github.com/LuisPalacios/gitbox/pkg/adopt"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/reconcile"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
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
}

// ReconcileDTO answers a reconcile pass.
type ReconcileDTO struct {
	Adopted   int             `json:"adopted"`
	Relinked  int             `json:"relinked"`
	Unmatched []OrphanRepoDTO `json:"unmatched"`
	Ignored   []OrphanRepoDTO `json:"ignored"`
	Error     string          `json:"error,omitempty"`
}

// AdoptRequestDTO asks to adopt the clone at Path into Account.
type AdoptRequestDTO struct {
	Path    string `json:"path"`
	Account string `json:"account"`
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
	return repoSetup{path: p.Path, accountKey: p.Account, repoKey: p.Repo, credType: repo.EffectiveCredentialType(&acct), acct: acct, global: cfg.Global}
}

// run points origin, the credential helper and the identity at the account,
// as adoption always did.
func (s repoSetup) run() {
	_ = git.SetRemoteURL(s.path, "origin", adopt.PlainRemoteURL(s.acct, s.repoKey, s.credType))
	_ = credential.ConfigureRepoCredential(s.path, s.acct, s.accountKey, s.credType, s.global)
	_ = git.ConfigSet(s.path, "user.name", s.acct.Name)
	_ = git.ConfigSet(s.path, "user.email", s.acct.Email)
}

// scanConfig snapshots the config and runs the reconcile scan without the
// lock, retrying when the config changed meanwhile. It returns with a.mu
// held and the plan for the live config; the caller unlocks.
func (a *App) scanConfig() (reconcile.Result, error) {
	scan := a.reconcileScan
	if scan == nil {
		scan = reconcile.Plan
	}
	for attempt := 0; ; attempt++ {
		a.mu.Lock()
		snap, err := a.cfg.Clone()
		a.mu.Unlock()
		if err != nil {
			a.mu.Lock()
			return reconcile.Result{}, err
		}
		res, err := scan(snap)
		a.mu.Lock()
		if err != nil {
			return reconcile.Result{}, err
		}
		if cur, cerr := a.cfg.Clone(); cerr == nil && (reflect.DeepEqual(cur, snap) || attempt >= 2) {
			return res, nil
		}
		a.mu.Unlock()
	}
}

// Reconcile runs one pass: re-link moved clones, adopt confident ones in
// place, and return what needs me. Emits "reconcile:done" when the config
// changed.
func (a *App) Reconcile() ReconcileDTO {
	a.reconcileMu.Lock()
	defer a.reconcileMu.Unlock()

	a.mu.Lock()
	ready := a.cfg != nil && a.cfgLoaded
	a.mu.Unlock()
	if !ready {
		return ReconcileDTO{Unmatched: []OrphanRepoDTO{}, Ignored: []OrphanRepoDTO{}}
	}

	res, err := a.scanConfig()
	if err != nil {
		a.mu.Unlock()
		return ReconcileDTO{Unmatched: []OrphanRepoDTO{}, Ignored: []OrphanRepoDTO{}, Error: err.Error()}
	}
	placed, changed := reconcile.Apply(a.cfg, res)
	var saveErr error
	if changed {
		saveErr = a.saveConfig()
	}
	setups := make([]repoSetup, 0, len(placed))
	for _, p := range placed {
		setups = append(setups, setupFor(a.cfg, p))
	}
	a.mu.Unlock()

	for _, s := range setups {
		s.run()
	}
	out := ReconcileDTO{Adopted: len(placed), Relinked: len(res.Relink), Unmatched: []OrphanRepoDTO{}, Ignored: []OrphanRepoDTO{}}
	for _, u := range res.Unmatched {
		out.Unmatched = append(out.Unmatched, toOrphanDTO(u.OrphanRepo, u.Reason))
	}
	for _, u := range res.Ignored {
		out.Ignored = append(out.Ignored, toOrphanDTO(u.OrphanRepo, u.Reason))
	}
	if saveErr != nil {
		out.Error = saveErr.Error()
	}
	if changed && a.ctx != nil {
		wailsrt.EventsEmit(a.ctx, "reconcile:done", out)
	}
	return out
}

// AdoptClones adopts unmatched or ignored clones into the accounts I
// picked, in place (an ignored one stops being ignored), then runs a fresh
// pass and returns it.
func (a *App) AdoptClones(reqs []AdoptRequestDTO) ReconcileDTO {
	a.reconcileMu.Lock()
	res, err := a.scanConfig()
	if err != nil {
		a.mu.Unlock()
		a.reconcileMu.Unlock()
		return ReconcileDTO{Unmatched: []OrphanRepoDTO{}, Ignored: []OrphanRepoDTO{}, Error: err.Error()}
	}
	var setups []repoSetup
	var errs []string
	for _, req := range reqs {
		found := false
		for _, u := range append(append([]reconcile.Unmatched{}, res.Unmatched...), res.Ignored...) {
			if !config.SamePath(u.Path, req.Path) {
				continue
			}
			found = true
			source, err := reconcile.Adopt(a.cfg, u.OrphanRepo, req.Account)
			if err != nil {
				errs = append(errs, err.Error())
				break
			}
			a.cfg.IgnoreClone(u.Path, false)
			setups = append(setups, setupFor(a.cfg, reconcile.Placed{Account: req.Account, Source: source, Repo: u.RepoKey, Path: u.Path}))
			break
		}
		if !found {
			errs = append(errs, fmt.Sprintf("%s is no longer waiting to be adopted", req.Path))
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
		errs = append(errs, saveErr.Error())
	}
	if len(errs) > 0 && out.Error == "" {
		out.Error = errs[0]
	}
	return out
}

// IgnoreClone remembers (or forgets) that I don't want the clone at path
// tracked, and persists.
func (a *App) IgnoreClone(path string, ignore bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.IgnoreClone(path, ignore) {
		return a.saveConfig()
	}
	return nil
}
