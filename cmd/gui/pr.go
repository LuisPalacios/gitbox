package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
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
}

// PRSettingsDTO exposes the user-controlled PR badge toggles.
type PRSettingsDTO struct {
	Enabled        bool `json:"enabled"`
	IncludeDrafts  bool `json:"includeDrafts"`
}

// PRAccountUpdateDTO is emitted per account on the "pr:refreshed" event so
// the frontend can merge results incrementally instead of waiting for all
// accounts to finish.
type PRAccountUpdateDTO struct {
	AccountKey string                  `json:"accountKey"`
	ByRepo     map[string]PRSummaryDTO `json:"byRepo"`
	Error      string                  `json:"error,omitempty"`
}

// prCache is a process-local cache of the most recent PR data per account.
// The frontend owns its own copy in a Svelte store; this cache is only
// consulted by direct GetPRsForRepo calls (e.g. popover on a clone row
// before the first refresh has propagated).
var (
	prCacheMu sync.RWMutex
	prCache   = map[string]provider.AccountPRs{} // accountKey → AccountPRs
)

// GetPRSettings returns the current PR badge feature flags.
func (a *App) GetPRSettings() PRSettingsDTO {
	a.mu.Lock()
	defer a.mu.Unlock()
	return PRSettingsDTO{
		Enabled:       a.cfg.Global.PRBadgesOn(),
		IncludeDrafts: a.cfg.Global.PRDraftsIncluded(),
	}
}

// SetPRBadgesEnabled toggles the feature and persists the config.
// When disabled the frontend should clear its store; this function does not
// evict the process cache (cheap and silent).
func (a *App) SetPRBadgesEnabled(enabled bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := enabled
	a.cfg.Global.PRBadgesEnabled = &v
	return config.Save(a.cfg, a.cfgPath)
}

// SetPRIncludeDrafts toggles counting draft PRs as "authored" and persists.
func (a *App) SetPRIncludeDrafts(enabled bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := enabled
	a.cfg.Global.PRIncludeDrafts = &v
	return config.Save(a.cfg, a.cfgPath)
}

// GetPRsForRepo returns the cached PR summary for a specific clone.
// sourceKey maps to one account; repoKey is the "owner/repo" string used in
// config.Source.Repos. Returns an empty summary when nothing is cached or
// when the feature is disabled.
func (a *App) GetPRsForRepo(sourceKey, repoKey string) PRSummaryDTO {
	a.mu.Lock()
	if !a.cfg.Global.PRBadgesOn() {
		a.mu.Unlock()
		return PRSummaryDTO{}
	}
	src, ok := a.cfg.Sources[sourceKey]
	a.mu.Unlock()
	if !ok {
		return PRSummaryDTO{}
	}

	prCacheMu.RLock()
	entry, ok := prCache[src.Account]
	prCacheMu.RUnlock()
	if !ok {
		return PRSummaryDTO{}
	}
	return lookupRepoSummary(entry, repoKey)
}

// RefreshAllPRs kicks off PR fetches for every account that has a PRLister
// implementation. Per-account errors are silent (they surface only on the
// emitted event's Error field). Emits "pr:refreshed" once per account, so
// the frontend can merge results incrementally.
func (a *App) RefreshAllPRs() {
	a.mu.Lock()
	enabled := a.cfg.Global.PRBadgesOn()
	includeDrafts := a.cfg.Global.PRDraftsIncluded()
	accounts := make([]accountSnapshot, 0, len(a.cfg.Accounts))
	for key, acct := range a.cfg.Accounts {
		accounts = append(accounts, accountSnapshot{key: key, acct: acct})
	}
	a.mu.Unlock()

	if !enabled {
		return
	}

	go func() {
		for _, snap := range accounts {
			upd := fetchPRsForAccount(snap, includeDrafts)
			if a.ctx != nil {
				wailsrt.EventsEmit(a.ctx, "pr:refreshed", upd)
			}
		}
	}()
}

type accountSnapshot struct {
	key  string
	acct config.Account
}

// fetchPRsForAccount looks up a PRLister for the account's provider, fetches
// PRs, updates the process cache, and returns a DTO ready to emit.
func fetchPRsForAccount(snap accountSnapshot, includeDrafts bool) PRAccountUpdateDTO {
	dto := PRAccountUpdateDTO{AccountKey: snap.key, ByRepo: map[string]PRSummaryDTO{}}

	prov, err := provider.ByName(snap.acct.Provider)
	if err != nil {
		dto.Error = err.Error()
		return dto
	}
	lister, ok := prov.(provider.PRLister)
	if !ok {
		// Provider does not support PR listing (e.g. Bitbucket) — silent.
		return dto
	}

	token, _, err := credential.ResolveAPIToken(snap.acct, snap.key)
	if err != nil {
		dto.Error = fmt.Sprintf("no API token: %v", err)
		return dto
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := lister.ListAccountPRs(ctx, snap.acct.URL, token, snap.acct.Username, includeDrafts)
	if err != nil {
		dto.Error = err.Error()
		return dto
	}

	prCacheMu.Lock()
	prCache[snap.key] = res
	prCacheMu.Unlock()

	for repoFull, sum := range res.ByRepo {
		dto.ByRepo[strings.ToLower(repoFull)] = toPRSummaryDTO(sum)
	}
	return dto
}

// lookupRepoSummary case-insensitively finds a repo inside an AccountPRs.
func lookupRepoSummary(entry provider.AccountPRs, repoKey string) PRSummaryDTO {
	needle := strings.ToLower(strings.TrimSpace(repoKey))
	for full, sum := range entry.ByRepo {
		if strings.ToLower(full) == needle {
			return toPRSummaryDTO(sum)
		}
	}
	return PRSummaryDTO{}
}

func toPRSummaryDTO(s provider.PRSummary) PRSummaryDTO {
	return PRSummaryDTO{
		Authored:        convertPRList(s.Authored),
		ReviewRequested: convertPRList(s.ReviewRequested),
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
