package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/provider"
)

// ─── PR / review indicators ──────────────────────────────────────

// PullRequestDTO mirrors provider.PullRequest for the frontend.
type PullRequestDTO struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Author   string `json:"author"`
	Updated  string `json:"updated"` // RFC3339, empty if unknown
	IsDraft  bool   `json:"isDraft"`
	RepoFull string `json:"repoFull"`
}

// PRSummaryDTO holds the per-repo badge counts plus the raw PR lists
// used to render the popover.
type PRSummaryDTO struct {
	Authored        []PullRequestDTO `json:"authored"`
	ReviewRequested []PullRequestDTO `json:"reviewRequested"`
	Partial         bool             `json:"partial,omitempty"`
}

// PRSettingsDTO exposes the user-controlled PR badge toggles.
type PRSettingsDTO struct {
	Enabled       bool `json:"enabled"`
	IncludeDrafts bool `json:"includeDrafts"`
}

// PRAccountUpdateDTO is emitted per account on the "pr:refreshed" event so
// the frontend can merge results incrementally instead of waiting for all
// accounts to finish.
type PRAccountUpdateDTO struct {
	AccountKey string                  `json:"accountKey"`
	ByRepo     map[string]PRSummaryDTO `json:"byRepo"`
	Error      string                  `json:"error,omitempty"`
}

const prRefreshConcurrency = 2

// prAccount describes the account fields used for listing and authenticating PRs.
type prAccount struct {
	provider, url, username, credentialType string
}

func prAccountFor(account config.Account) prAccount {
	return prAccount{account.Provider, account.URL, account.Username, account.DefaultCredentialType}
}

type prRefresh struct {
	account            prAccount
	credentialRevision uint64
	settingsRevision   uint64
	includeDrafts      bool
}

type prEntry struct {
	account       prAccount
	includeDrafts bool
	data          provider.AccountPRs
}

// A scheduler caller waits for every worker in the pass to finish. It also
// needs an updated settings request if the user changes a filter mid-pass.
type prPass struct {
	scheduled bool
	done      chan struct{}
	err       error
}

// prRefreshState belongs to one App. The App mutex guards its queue,
// in-flight requests, cache and settings revision.
type prRefreshState struct {
	pass             *prPass
	cache            map[string]prEntry
	pending          map[string]prRefresh
	active           map[string]prRefresh
	workers          int
	settingsRevision uint64
	// Tests can supply a lister while using the real refresh and token paths.
	lister provider.PRLister
}

// GetPRSettings returns the current PR badge feature flags.
func (a *App) GetPRSettings() PRSettingsDTO { return a.be().GetPRSettings() }

// getPRSettingsLocal is the local backend's GetPRSettings.
func (a *App) getPRSettingsLocal() PRSettingsDTO {
	a.mu.Lock()
	defer a.mu.Unlock()
	return PRSettingsDTO{
		Enabled:       a.cfg.Global.PRBadgesOn(),
		IncludeDrafts: a.cfg.Global.PRDraftsIncluded(),
	}
}

// SetPRBadgesEnabled toggles the feature and persists the config.
// When disabled the frontend clears its store and in-flight results are dropped.
func (a *App) SetPRBadgesEnabled(enabled bool) error { return a.be().SetPRBadgesEnabled(enabled) }

// setPRBadgesEnabledLocal is the local backend's SetPRBadgesEnabled.
func (a *App) setPRBadgesEnabledLocal(enabled bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if enabled != a.cfg.Global.PRBadgesOn() {
		a.prs.settingsRevision++
	}
	v := enabled
	a.cfg.Global.PRBadgesEnabled = &v
	if enabled && a.prs.pass != nil && a.prs.pass.scheduled {
		a.queuePRAccountsLocked()
	}
	return a.saveConfig()
}

// SetPRIncludeDrafts toggles counting draft PRs as "authored" and persists.
func (a *App) SetPRIncludeDrafts(enabled bool) error { return a.be().SetPRIncludeDrafts(enabled) }

// setPRIncludeDraftsLocal is the local backend's SetPRIncludeDrafts.
func (a *App) setPRIncludeDraftsLocal(enabled bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if enabled != a.cfg.Global.PRDraftsIncluded() {
		a.prs.settingsRevision++
	}
	v := enabled
	a.cfg.Global.PRIncludeDrafts = &v
	if a.cfg.Global.PRBadgesOn() && a.prs.pass != nil && a.prs.pass.scheduled {
		a.queuePRAccountsLocked()
	}
	return a.saveConfig()
}

// GetPRsForRepo returns the cached PR summary for a specific clone.
// sourceKey maps to one account; repoKey is the "owner/repo" string used in
// config.Source.Repos. Returns an empty summary when nothing is cached or
// when the feature is disabled.
func (a *App) GetPRsForRepo(sourceKey, repoKey string) PRSummaryDTO {
	return a.be().GetPRsForRepo(sourceKey, repoKey)
}

// getPRsForRepoLocal is the local backend's GetPRsForRepo.
func (a *App) getPRsForRepoLocal(sourceKey, repoKey string) PRSummaryDTO {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.cfg.Global.PRBadgesOn() {
		return PRSummaryDTO{}
	}
	src, ok := a.cfg.Sources[sourceKey]
	if !ok {
		return PRSummaryDTO{}
	}
	account, ok := a.cfg.Accounts[src.Account]
	entry, cached := a.prs.cache[src.Account]
	if !ok || !cached || entry.account != prAccountFor(account) || entry.includeDrafts != a.cfg.Global.PRDraftsIncluded() {
		return PRSummaryDTO{}
	}
	return lookupRepoSummary(entry.data, repoKey)
}

// RefreshAllPRs schedules an incremental refresh for each account.
func (a *App) RefreshAllPRs() { a.be().RefreshAllPRs() }

// refreshAllPRsLocal is the local backend's RefreshAllPRs.
func (a *App) refreshAllPRsLocal() { a.queuePRs(false) }

func (a *App) queuePRs(scheduled bool) *prPass {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	if a.refreshClosed {
		return nil
	}
	ctx := a.refreshContextLocked()

	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.cfg.Global.PRBadgesOn() {
		return nil
	}
	if a.prs.pending == nil {
		a.prs.pending = make(map[string]prRefresh)
		a.prs.active = make(map[string]prRefresh)
	}
	a.queuePRAccountsLocked()
	if len(a.prs.pending) > 0 && a.prs.pass == nil {
		a.prs.pass = &prPass{done: make(chan struct{})}
	}
	if scheduled && a.prs.pass != nil {
		a.prs.pass.scheduled = true
	}
	for a.prs.workers < min(prRefreshConcurrency, len(a.prs.pending)+len(a.prs.active)) {
		a.prs.workers++
		a.refreshWG.Go(func() { a.refreshPRAccounts(ctx) })
	}
	return a.prs.pass
}

// A settings change during a fleet check supersedes each old request in its
// existing queue. The busy check stays one pass, including the new settings.
func (a *App) queuePRAccountsLocked() {
	for key, account := range a.cfg.Accounts {
		req := a.prRefreshLocked(key, account)
		if active, ok := a.prs.active[key]; ok && active == req {
			continue
		}
		a.prs.pending[key] = req
	}
}

func (a *App) prRefreshLocked(key string, account config.Account) prRefresh {
	return prRefresh{
		account:            prAccountFor(account),
		credentialRevision: a.credentialRevision[key],
		settingsRevision:   a.prs.settingsRevision,
		includeDrafts:      a.cfg.Global.PRDraftsIncluded(),
	}
}

func (a *App) prRefreshIsCurrentLocked(key string, req prRefresh) bool {
	account, ok := a.cfg.Accounts[key]
	return ok && a.cfg.Global.PRBadgesOn() && a.prRefreshLocked(key, account) == req
}

func (a *App) nextPRRefresh(ctx context.Context) (string, prRefresh, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ctx.Err() != nil {
		clear(a.prs.pending)
	} else {
		for key, req := range a.prs.pending {
			if !a.prRefreshIsCurrentLocked(key, req) {
				delete(a.prs.pending, key)
				continue
			}
			if _, busy := a.prs.active[key]; !busy {
				delete(a.prs.pending, key)
				a.prs.active[key] = req
				return key, req, true
			}
		}
	}
	a.prs.workers--
	if a.prs.workers == 0 && a.prs.pass != nil {
		close(a.prs.pass.done)
		a.prs.pass = nil
	}
	return "", prRefresh{}, false
}

func (a *App) refreshPRAccounts(ctx context.Context) {
	for {
		key, req, ok := a.nextPRRefresh(ctx)
		if !ok {
			return
		}
		data, upd, publish := a.fetchPRsForAccount(ctx, key, req)
		// Credential changes bracket their work with revision bumps under the
		// maintenance lock. Wait for the last bump before checking the result.
		a.maintenanceMu.RLock()
		a.mu.Lock()
		current := a.prRefreshIsCurrentLocked(key, req)
		_, retry := a.prs.pending[key]
		if !retry && (!publish || upd.Error != "" || !data.Complete || !current) && a.prs.pass != nil {
			a.prs.pass.err = errTaskPartial
		}
		if publish && ctx.Err() == nil && current {
			if upd.Error == "" {
				if a.prs.cache == nil {
					a.prs.cache = make(map[string]prEntry)
				}
				a.prs.cache[key] = prEntry{req.account, req.includeDrafts, data}
			}
			a.sink().Emit("pr:refreshed", upd)
		}
		delete(a.prs.active, key)
		a.mu.Unlock()
		a.maintenanceMu.RUnlock()
	}
}

// fetchPRsForAccount does network and credential work outside the App lock.
func (a *App) fetchPRsForAccount(ctx context.Context, key string, req prRefresh) (data provider.AccountPRs, upd PRAccountUpdateDTO, publish bool) {
	acct := config.Account{
		Provider: req.account.provider, URL: req.account.url, Username: req.account.username,
		DefaultCredentialType: req.account.credentialType,
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Unreachable accounts retain their last known PRs.
	if !a.reachable(ctx, serverAddr(acct)) {
		return data, upd, false
	}
	upd = PRAccountUpdateDTO{AccountKey: key, ByRepo: map[string]PRSummaryDTO{}}
	lister := a.prs.lister
	if lister == nil {
		prov, err := provider.ByName(acct.Provider)
		if err != nil {
			upd.Error = userMsg(err)
			return data, upd, true
		}
		var supported bool
		lister, supported = prov.(provider.PRLister)
		if !supported {
			data.Complete = true
			return data, upd, true
		}
	}
	path := credential.CredentialFilePath(key)
	before, _ := os.Stat(path)
	token, _, err := credential.ResolveAPITokenContext(ctx, acct, key)
	if err != nil {
		upd.Error = userText("no API token")
		return data, upd, true
	}
	ctx = provider.WithPRRetry(provider.WithAccountCache(ctx, key, req.credentialRevision))
	data, err = lister.ListAccountPRs(ctx, acct.URL, token, acct.Username, req.includeDrafts)
	after, _ := os.Stat(path)
	if !samePRCredentialFile(before, after) {
		return data, upd, false
	}
	if err != nil {
		upd.Error = userMsg(err)
		return data, upd, true
	}
	for repoFull, sum := range data.ByRepo {
		upd.ByRepo[strings.ToLower(repoFull)] = toPRSummaryDTO(sum, !data.Complete)
	}
	return data, upd, true
}

// samePRCredentialFile checks for updates from outside the App without
// reading or retaining the token. Atomic writes may replace the file even
// when its size and timestamp match.
func samePRCredentialFile(before, after os.FileInfo) bool {
	if before == nil || after == nil {
		return before == nil && after == nil
	}
	return os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
}

// refreshPRConfigLocked applies changes loaded from disk to PR state before
// replacing a.cfg. Unrelated changes leave active requests in place.
func (a *App) refreshPRConfigLocked(next *config.Config) {
	if a.cfg == nil {
		return
	}
	if a.cfg.Global.PRBadgesOn() != next.Global.PRBadgesOn() ||
		a.cfg.Global.PRDraftsIncluded() != next.Global.PRDraftsIncluded() {
		a.prs.settingsRevision++
	}
	for key, old := range a.cfg.Accounts {
		account, exists := next.Accounts[key]
		if !exists || prAccountFor(old) != prAccountFor(account) {
			a.bumpCredentialRevisionLocked(key)
			delete(a.prs.cache, key)
		}
	}
}

// lookupRepoSummary case-insensitively finds a repo inside an AccountPRs.
func lookupRepoSummary(entry provider.AccountPRs, repoKey string) PRSummaryDTO {
	needle := strings.ToLower(strings.TrimSpace(repoKey))
	for full, sum := range entry.ByRepo {
		if strings.ToLower(full) == needle {
			return toPRSummaryDTO(sum, !entry.Complete)
		}
	}
	return PRSummaryDTO{Partial: !entry.Complete}
}

func toPRSummaryDTO(s provider.PRSummary, partial bool) PRSummaryDTO {
	return PRSummaryDTO{
		Authored:        convertPRList(s.Authored),
		ReviewRequested: convertPRList(s.ReviewRequested),
		Partial:         partial,
	}
}

func convertPRList(prs []provider.PullRequest) []PullRequestDTO {
	out := make([]PullRequestDTO, len(prs))
	for i, pr := range prs {
		updated := ""
		if !pr.Updated.IsZero() {
			updated = pr.Updated.UTC().Format(time.RFC3339)
		}
		out[i] = PullRequestDTO{
			Number:   pr.Number,
			Title:    pr.Title,
			URL:      pr.URL,
			Author:   pr.Author,
			Updated:  updated,
			IsDraft:  pr.IsDraft,
			RepoFull: pr.RepoFull,
		}
	}
	return out
}
