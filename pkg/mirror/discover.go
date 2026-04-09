package mirror

import (
	"context"
	"net/url"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/provider"
)

// DiscoveredMirror represents a single mirror relationship found by scanning.
type DiscoveredMirror struct {
	RepoKey    string // "org/repo" on the origin side
	TargetRepo string // name on the backup side (if different from RepoKey)
	Direction  string // "push" or "pull"
	Origin     string // "src" or "dst"
	Confidence string // "confirmed", "likely", "possible"
}

// DiscoveryResult groups discovered mirrors for one account pair.
type DiscoveryResult struct {
	MirrorKey  string             // suggested key for the mirror group
	AccountSrc string             // first account (source side)
	AccountDst string             // second account (destination side)
	Discovered []DiscoveredMirror // found mirror relationships
}

// DiscoverProgress reports scanning progress to a caller.
type DiscoverProgress struct {
	Phase   string // "listing" or "analyzing"
	Account string // account key being listed/analyzed
	Current int    // current repo index (analyzing phase)
	Total   int    // total repos in this account (analyzing phase)
}

// DiscoverMirrors scans all account pairs to detect push/pull mirror relationships.
// It uses three detection methods with decreasing confidence:
//  1. Push mirror API (confirmed) — ListPushMirrors on Forgejo/Gitea
//  2. Mirror flag (likely) — repo.Mirror=true on Forgejo/Gitea + name match
//  3. Name match (possible) — same repo name on both sides
//
// If onProgress is non-nil it is called with progress updates during scanning.
func DiscoverMirrors(ctx context.Context, cfg *config.Config, onProgress func(DiscoverProgress)) ([]DiscoveryResult, error) {
	keys := make([]string, 0, len(cfg.Accounts))
	for k := range cfg.Accounts {
		keys = append(keys, k)
	}

	var results []DiscoveryResult

	// Check all unique pairs.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			result, err := discoverPair(ctx, cfg, keys[i], keys[j], onProgress)
			if err != nil {
				continue // skip pair on error, don't abort everything
			}
			if len(result.Discovered) > 0 {
				results = append(results, result)
			}
		}
	}
	return results, nil
}

func discoverPair(ctx context.Context, cfg *config.Config, keyA, keyB string, onProgress func(DiscoverProgress)) (DiscoveryResult, error) {
	acctA := cfg.Accounts[keyA]
	acctB := cfg.Accounts[keyB]

	progress := func(p DiscoverProgress) {
		if onProgress != nil {
			onProgress(p)
		}
	}

	tokenA, _, errA := credential.ResolveAPIToken(acctA, keyA)
	if errA != nil {
		return DiscoveryResult{}, errA
	}
	tokenB, _, errB := credential.ResolveAPIToken(acctB, keyB)
	if errB != nil {
		return DiscoveryResult{}, errB
	}

	provA, errA := provider.ByName(acctA.Provider)
	if errA != nil {
		return DiscoveryResult{}, errA
	}
	provB, errB := provider.ByName(acctB.Provider)
	if errB != nil {
		return DiscoveryResult{}, errB
	}

	progress(DiscoverProgress{Phase: "listing", Account: keyA})
	reposA, errA := provA.ListRepos(ctx, acctA.URL, tokenA, acctA.Username)
	if errA != nil {
		return DiscoveryResult{}, errA
	}

	progress(DiscoverProgress{Phase: "listing", Account: keyB})
	reposB, errB := provB.ListRepos(ctx, acctB.URL, tokenB, acctB.Username)
	if errB != nil {
		return DiscoveryResult{}, errB
	}

	// Build indexes by short name for matching.
	indexA := buildRepoIndex(reposA)
	indexB := buildRepoIndex(reposB)

	result := DiscoveryResult{
		MirrorKey:  keyA + "-" + keyB,
		AccountSrc: keyA,
		AccountDst: keyB,
	}

	// Track which repos have been detected (to avoid duplicates).
	detected := make(map[string]bool) // key: "src:fullname" or "dst:fullname"

	// 1. Push mirror detection (confirmed) — check A→B and B→A.
	totalRepos := len(reposA) + len(reposB)
	detectPushMirrorsWithProgress(ctx, provA, acctA, acctB, tokenA, keyA, reposA, &result, detected, "src", func(i int) {
		progress(DiscoverProgress{Phase: "analyzing", Account: keyA, Current: i + 1, Total: totalRepos})
	})
	detectPushMirrorsWithProgress(ctx, provB, acctB, acctA, tokenB, keyB, reposB, &result, detected, "dst", func(i int) {
		progress(DiscoverProgress{Phase: "analyzing", Account: keyB, Current: len(reposA) + i + 1, Total: totalRepos})
	})

	// 2. Pull mirror detection (likely) — repos with Mirror=true on one side.
	detectPullMirrors(reposA, indexB, &result, detected, "src")
	detectPullMirrors(reposB, indexA, &result, detected, "dst")

	// 3. Name match fallback (possible) — same short name on both sides.
	detectNameMatches(reposA, indexB, &result, detected)

	return result, nil
}

// detectPushMirrorsWithProgress checks repos on originSide for push mirrors pointing to the other account.
// onRepo is called with the loop index for each repo processed (may be nil).
func detectPushMirrorsWithProgress(ctx context.Context, prov provider.Provider, originAcct, targetAcct config.Account, token, originKey string, repos []provider.RemoteRepo, result *DiscoveryResult, detected map[string]bool, originSide string, onRepo func(int)) {
	pm, ok := prov.(provider.PushMirrorProvider)
	if !ok {
		// Still report progress for each repo even if provider doesn't support push mirrors.
		if onRepo != nil {
			for i := range repos {
				onRepo(i)
			}
		}
		return
	}

	for i, repo := range repos {
		if onRepo != nil {
			onRepo(i)
		}
		owner := repoOwner(repo.FullName)
		name := repoNameOnly(repo.FullName)
		if owner == "" {
			continue
		}

		mirrors, err := pm.ListPushMirrors(ctx, originAcct.URL, token, originAcct.Username, owner, name)
		if err != nil {
			continue
		}

		for _, m := range mirrors {
			host, mOwner, mRepo, err := git.ParseRemoteURL(m.RemoteURL)
			if err != nil {
				continue
			}
			// Match against target account URL and username.
			// Both host and owner must match — multiple accounts can share a host.
			targetHost := extractHost(targetAcct.URL)
			if !strings.EqualFold(host, targetHost) {
				continue
			}
			if !strings.EqualFold(mOwner, targetAcct.Username) {
				continue
			}

			dm := DiscoveredMirror{
				RepoKey:    repo.FullName,
				TargetRepo: mRepo, // the name on the target side
				Direction:  "push",
				Origin:     originSide,
				Confidence: "confirmed",
			}
			detKey := originSide + ":" + repo.FullName
			if !detected[detKey] {
				result.Discovered = append(result.Discovered, dm)
				detected[detKey] = true
			}
		}
	}
}

// detectPullMirrors finds repos with Mirror=true on one side and a matching name on the other.
func detectPullMirrors(mirrorRepos []provider.RemoteRepo, otherIndex map[string]provider.RemoteRepo, result *DiscoveryResult, detected map[string]bool, mirrorSide string) {
	for _, repo := range mirrorRepos {
		if !repo.Mirror {
			continue
		}
		shortName := repoNameOnly(repo.FullName)
		if _, ok := otherIndex[shortName]; !ok {
			continue
		}

		// This repo is a mirror and a same-named repo exists on the other side.
		// The mirror side is the backup; the other side is the origin.
		originSide := "dst"
		if mirrorSide == "dst" {
			originSide = "src"
		}

		// Use the other side's full name as the RepoKey (origin's perspective).
		otherRepo := otherIndex[shortName]
		dm := DiscoveredMirror{
			RepoKey:    otherRepo.FullName,
			TargetRepo: repo.FullName,
			Direction:  "pull",
			Origin:     originSide,
			Confidence: "likely",
		}
		detKey := originSide + ":" + otherRepo.FullName
		if !detected[detKey] {
			result.Discovered = append(result.Discovered, dm)
			detected[detKey] = true
		}
	}
}

// detectNameMatches finds repos with the same short name on both sides (lowest confidence).
func detectNameMatches(reposA []provider.RemoteRepo, indexB map[string]provider.RemoteRepo, result *DiscoveryResult, detected map[string]bool) {
	for _, repoA := range reposA {
		shortName := repoNameOnly(repoA.FullName)
		repoB, ok := indexB[shortName]
		if !ok {
			continue
		}
		// Skip if already detected by push/pull methods.
		if detected["src:"+repoA.FullName] || detected["dst:"+repoB.FullName] {
			continue
		}

		dm := DiscoveredMirror{
			RepoKey:    repoA.FullName,
			TargetRepo: repoB.FullName,
			Direction:  "push", // guess — can't determine without more info
			Origin:     "src",  // guess
			Confidence: "possible",
		}
		result.Discovered = append(result.Discovered, dm)
		detected["src:"+repoA.FullName] = true
	}
}

// buildRepoIndex indexes repos by their short name (part after /).
func buildRepoIndex(repos []provider.RemoteRepo) map[string]provider.RemoteRepo {
	idx := make(map[string]provider.RemoteRepo, len(repos))
	for _, r := range repos {
		short := repoNameOnly(r.FullName)
		idx[short] = r
	}
	return idx
}

// extractHost returns the hostname from a base URL.
func extractHost(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	return u.Hostname()
}

// ApplyDiscovery merges discovered mirrors into the config.
// Creates mirror groups if needed, adds repos that aren't already present.
// Returns (added, updated) counts.
func ApplyDiscovery(cfg *config.Config, results []DiscoveryResult) (int, int) {
	added := 0

	for _, r := range results {
		// Find or create mirror group.
		mirrorKey := findExistingMirrorKey(cfg, r.AccountSrc, r.AccountDst)
		if mirrorKey == "" {
			mirrorKey = r.MirrorKey
			m := config.Mirror{
				AccountSrc: r.AccountSrc,
				AccountDst: r.AccountDst,
			}
			if err := cfg.AddMirror(mirrorKey, m); err != nil {
				continue
			}
		}

		for _, dm := range r.Discovered {
			// Skip if already in config.
			if _, exists := cfg.Mirrors[mirrorKey].Repos[dm.RepoKey]; exists {
				continue
			}

			repo := config.MirrorRepo{
				Direction: dm.Direction,
				Origin:    dm.Origin,
			}
			if dm.TargetRepo != "" && dm.TargetRepo != dm.RepoKey {
				repo.TargetRepo = dm.TargetRepo
			}

			if err := cfg.AddMirrorRepo(mirrorKey, dm.RepoKey, repo); err != nil {
				continue
			}
			added++
		}
	}
	return added, 0
}

// findExistingMirrorKey finds an existing mirror group for the given account pair
// (in either direction).
func findExistingMirrorKey(cfg *config.Config, acctA, acctB string) string {
	for key, m := range cfg.Mirrors {
		if (m.AccountSrc == acctA && m.AccountDst == acctB) ||
			(m.AccountSrc == acctB && m.AccountDst == acctA) {
			return key
		}
	}
	return ""
}
