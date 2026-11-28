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

// prRefreshState belongs to one localSession. Its mu guards the queue,
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
func (s *localSession) getPRSettingsLocal() PRSettingsDTO {
	s.mu.Lock()
	defer s.mu.Unlock()
	return PRSettingsDTO{
		Enabled:       s.cfg.Global.PRBadgesOn(),
		IncludeDrafts: s.cfg.Global.PRDraftsIncluded(),
	}
}

// SetPRBadgesEnabled toggles the feature and persists the config.
// When disabled the frontend clears its store and in-flight results are dropped.
func (a *App) SetPRBadgesEnabled(enabled bool) error { return a.be().SetPRBadgesEnabled(enabled) }

// setPRBadgesEnabledLocal is the local backend's SetPRBadgesEnabled. save
// persists the config under s.mu; it is App.saveConfig until the save moves
// into the session.
func (s *localSession) setPRBadgesEnabledLocal(enabled bool, save func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enabled != s.cfg.Global.PRBadgesOn() {
		s.prs.settingsRevision++
	}
	v := enabled
	s.cfg.Global.PRBadgesEnabled = &v
	if enabled && s.prs.pass != nil && s.prs.pass.scheduled {
		s.queuePRAccountsLocked()
	}
	return save()
}

// SetPRIncludeDrafts toggles counting draft PRs as "authored" and persists.
func (a *App) SetPRIncludeDrafts(enabled bool) error { return a.be().SetPRIncludeDrafts(enabled) }

// setPRIncludeDraftsLocal is the local backend's SetPRIncludeDrafts; save
// works as in setPRBadgesEnabledLocal.
func (s *localSession) setPRIncludeDraftsLocal(enabled bool, save func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enabled != s.cfg.Global.PRDraftsIncluded() {
		s.prs.settingsRevision++
	}
	v := enabled
	s.cfg.Global.PRIncludeDrafts = &v
	if s.cfg.Global.PRBadgesOn() && s.prs.pass != nil && s.prs.pass.scheduled {
		s.queuePRAccountsLocked()
	}
	return save()
}

// getPRsForRepoLocal returns the cached PR summary for a clone.
func (s *localSession) getPRsForRepoLocal(sourceKey, repoKey string) PRSummaryDTO {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cfg.Global.PRBadgesOn() {
		return PRSummaryDTO{}
	}
	src, ok := s.cfg.Sources[sourceKey]
	if !ok {
		return PRSummaryDTO{}
	}
	account, ok := s.cfg.Accounts[src.Account]
	entry, cached := s.prs.cache[src.Account]
	if !ok || !cached || entry.account != prAccountFor(account) || entry.includeDrafts != s.cfg.Global.PRDraftsIncluded() {
		return PRSummaryDTO{}
	}
	return lookupRepoSummary(entry.data, repoKey)
}

// refreshAllPRsLocal is the local backend's RefreshAllPRs.
func (s *localSession) refreshAllPRsLocal() { s.queuePRs(false) }

// queuePRs starts a PR pass on the refresh flight.
func (s *localSession) queuePRs(scheduled bool) *prPass {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	if s.refreshClosed {
		return nil
	}
	ctx := s.refreshContextLocked()
	return s.startPRPass(scheduled, func() {
		s.refreshWG.Go(func() { s.refreshPRAccounts(ctx) })
	})
}

// startPRPass queues every account and calls startWorker, under s.mu, once
// for each worker the pass still needs. The caller holds the refresh lock.
func (s *localSession) startPRPass(scheduled bool, startWorker func()) *prPass {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cfg.Global.PRBadgesOn() {
		return nil
	}
	if s.prs.pending == nil {
		s.prs.pending = make(map[string]prRefresh)
		s.prs.active = make(map[string]prRefresh)
	}
	s.queuePRAccountsLocked()
	if len(s.prs.pending) > 0 && s.prs.pass == nil {
		s.prs.pass = &prPass{done: make(chan struct{})}
	}
	if scheduled && s.prs.pass != nil {
		s.prs.pass.scheduled = true
	}
	for s.prs.workers < min(prRefreshConcurrency, len(s.prs.pending)+len(s.prs.active)) {
		s.prs.workers++
		startWorker()
	}
	return s.prs.pass
}

// A settings change during a fleet check supersedes each old request in its
// existing queue. The busy check stays one pass, including the new settings.
func (s *localSession) queuePRAccountsLocked() {
	for key, account := range s.cfg.Accounts {
		req := s.prRefreshLocked(key, account)
		if active, ok := s.prs.active[key]; ok && active == req {
			continue
		}
		s.prs.pending[key] = req
	}
}

func (s *localSession) prRefreshLocked(key string, account config.Account) prRefresh {
	return prRefresh{
		account:            prAccountFor(account),
		credentialRevision: s.credentialRevision[key],
		settingsRevision:   s.prs.settingsRevision,
		includeDrafts:      s.cfg.Global.PRDraftsIncluded(),
	}
}

func (s *localSession) prRefreshIsCurrentLocked(key string, req prRefresh) bool {
	account, ok := s.cfg.Accounts[key]
	return ok && s.cfg.Global.PRBadgesOn() && s.prRefreshLocked(key, account) == req
}

func (s *localSession) nextPRRefresh(ctx context.Context) (string, prRefresh, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		clear(s.prs.pending)
	} else {
		for key, req := range s.prs.pending {
			if !s.prRefreshIsCurrentLocked(key, req) {
				delete(s.prs.pending, key)
				continue
			}
			if _, busy := s.prs.active[key]; !busy {
				delete(s.prs.pending, key)
				s.prs.active[key] = req
				return key, req, true
			}
		}
	}
	s.prs.workers--
	if s.prs.workers == 0 && s.prs.pass != nil {
		close(s.prs.pass.done)
		s.prs.pass = nil
	}
	return "", prRefresh{}, false
}

// refreshPRAccounts is one PR worker. The maintenance lock and the event
// stream stay on App until they move into the session (#370, #371).
func (s *localSession) refreshPRAccounts(ctx context.Context) {
	maintenance := s.app.maintenanceLock()
	for {
		key, req, ok := s.nextPRRefresh(ctx)
		if !ok {
			return
		}
		data, upd, publish := s.fetchPRsForAccount(ctx, key, req)
		// Credential changes bracket their work with revision bumps under the
		// maintenance lock. Wait for the last bump before checking the result.
		maintenance.RLock()
		s.completePRRefresh(ctx, key, req, data, upd, publish, func(upd PRAccountUpdateDTO) {
			emit(s.app.newOperation(), "pr:refreshed", upd)
		})
		maintenance.RUnlock()
	}
}

// completePRRefresh records one account's result and calls publish, under
// s.mu, when the result is still current. The caller holds the maintenance
// lock for reading.
func (s *localSession) completePRRefresh(ctx context.Context, key string, req prRefresh, data provider.AccountPRs, upd PRAccountUpdateDTO, fetched bool, publish func(PRAccountUpdateDTO)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.prRefreshIsCurrentLocked(key, req)
	_, retry := s.prs.pending[key]
	if !retry && (!fetched || upd.Error != "" || !data.Complete || !current) && s.prs.pass != nil {
		s.prs.pass.err = errTaskPartial
	}
	if fetched && ctx.Err() == nil && current {
		if upd.Error == "" {
			if s.prs.cache == nil {
				s.prs.cache = make(map[string]prEntry)
			}
			s.prs.cache[key] = prEntry{req.account, req.includeDrafts, data}
		}
		publish(upd)
	}
	delete(s.prs.active, key)
}

// fetchPRsForAccount does network and credential work outside s.mu.
func (s *localSession) fetchPRsForAccount(ctx context.Context, key string, req prRefresh) (data provider.AccountPRs, upd PRAccountUpdateDTO, publish bool) {
	acct := config.Account{
		Provider: req.account.provider, URL: req.account.url, Username: req.account.username,
		DefaultCredentialType: req.account.credentialType,
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Unreachable accounts retain their last known PRs.
	if !s.reachable(ctx, serverAddr(acct)) {
		return data, upd, false
	}
	upd = PRAccountUpdateDTO{AccountKey: key, ByRepo: map[string]PRSummaryDTO{}}
	lister := s.prs.lister
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
// replacing s.cfg. Unrelated changes leave active requests in place.
// Caller holds s.mu.
func (s *localSession) refreshPRConfigLocked(next *config.Config) {
	if s.cfg == nil {
		return
	}
	if s.cfg.Global.PRBadgesOn() != next.Global.PRBadgesOn() ||
		s.cfg.Global.PRDraftsIncluded() != next.Global.PRDraftsIncluded() {
		s.prs.settingsRevision++
	}
	for key, old := range s.cfg.Accounts {
		account, exists := next.Accounts[key]
		if !exists || prAccountFor(old) != prAccountFor(account) {
			s.bumpCredentialRevisionLocked(key)
			delete(s.prs.cache, key)
		}
	}
}

// forgetPRAccountLocked drops the cached PRs of an account that is gone.
// Caller holds s.mu.
func (s *localSession) forgetPRAccountLocked(key string) {
	delete(s.prs.cache, key)
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
