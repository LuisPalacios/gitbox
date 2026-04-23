package mirror

import (
	"context"
	"fmt"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/provider"
)

// CheckStatus queries live mirror status for a single repo by comparing HEAD
// commits on both the origin and backup sides.
func CheckStatus(ctx context.Context, cfg *config.Config, mirrorKey, repoKey string) StatusResult {
	m, ok := cfg.Mirrors[mirrorKey]
	if !ok {
		return StatusResult{RepoKey: repoKey, Error: fmt.Sprintf("mirror %q not found", mirrorKey)}
	}
	mr, ok := m.Repos[repoKey]
	if !ok {
		return StatusResult{RepoKey: repoKey, Error: fmt.Sprintf("repo %q not found in mirror %q", repoKey, mirrorKey)}
	}

	// Resolve origin and backup accounts.
	var originKey, backupKey string
	if mr.Origin == "src" {
		originKey, backupKey = m.AccountSrc, m.AccountDst
	} else {
		originKey, backupKey = m.AccountDst, m.AccountSrc
	}

	originAcct, ok := cfg.Accounts[originKey]
	if !ok {
		return StatusResult{RepoKey: repoKey, Error: fmt.Sprintf("origin account %q not found", originKey)}
	}
	backupAcct, ok := cfg.Accounts[backupKey]
	if !ok {
		return StatusResult{RepoKey: repoKey, Error: fmt.Sprintf("backup account %q not found", backupKey)}
	}

	// Always populate account keys so the UI can use them for navigation.
	base := StatusResult{RepoKey: repoKey, OriginAcct: originKey, BackupAcct: backupKey}

	originToken, _, err := credential.ResolveAPIToken(originAcct, originKey)
	if err != nil {
		base.Error = fmt.Sprintf("missing API token in %s", originKey)
		return base
	}
	backupToken, _, err := credential.ResolveAPIToken(backupAcct, backupKey)
	if err != nil {
		base.Error = fmt.Sprintf("missing API token in %s", backupKey)
		return base
	}

	originProv, err := provider.ByName(originAcct.Provider)
	if err != nil {
		base.Error = fmt.Sprintf("origin provider: %v", err)
		return base
	}
	backupProv, err := provider.ByName(backupAcct.Provider)
	if err != nil {
		base.Error = fmt.Sprintf("backup provider: %v", err)
		return base
	}

	// Get origin repo info.
	originInfo, ok := originProv.(provider.RepoInfoProvider)
	if !ok {
		base.Error = "origin provider does not support repo info"
		return base
	}
	originOwner := repoOwner(repoKey)
	originName := repoNameOnly(repoKey)
	srcInfo, err := originInfo.GetRepoInfo(ctx, originAcct.URL, originToken, originAcct.Username, originOwner, originName)
	if err != nil {
		base.Error = fmt.Sprintf("origin repo info: %v", err)
		return base
	}

	// Get backup repo info. Owner depends on direction:
	// - push mirror: target was created under backup username (CreateRepo uses repoNameOnly)
	// - pull mirror: migrate API creates under backup username
	backupInfo, ok := backupProv.(provider.RepoInfoProvider)
	if !ok {
		base.Error = "backup provider does not support repo info"
		return base
	}
	backupOwner := backupAcct.Username
	targetName := repoKey
	if mr.TargetRepo != "" {
		targetName = mr.TargetRepo
	}
	backupName := repoNameOnly(targetName)
	dstInfo, err := backupInfo.GetRepoInfo(ctx, backupAcct.URL, backupToken, backupAcct.Username, backupOwner, backupName)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			// A 404 on the backup side means one of two things:
			//   1. The user has never completed setup for this row yet — the
			//      target is supposed to be missing. Render as neutral
			//      "needs setup", not a red error.
			//   2. Setup was done previously and the target was since deleted
			//      on the provider — a genuine error the user should see.
			//
			// mr.LastSync is populated on successful setup (see cmd/gui/app.go
			// SetupMirrorRepo); an empty value means branch 1. Legacy configs
			// created before LastSync was written will land in branch 1 as
			// well — the false-neutral state self-heals on the first click of
			// Setup, which is acceptable for that rare case.
			if mr.LastSync == "" {
				base.NeedsSetup = true
				return base
			}
			base.Error = "target repo does not exist on " + backupKey
			return base
		}
		base.Error = fmt.Sprintf("backup repo info: %v", err)
		return base
	}

	// Check visibility — backup repos should always be private.
	result := base
	result.Active = true
	result.Direction = mr.Direction
	if !dstInfo.Private {
		result.Warning = "backup repo is PUBLIC"
	}

	// Compare HEAD commits.
	if srcInfo.HeadCommit != "" && dstInfo.HeadCommit != "" {
		if srcInfo.HeadCommit == dstInfo.HeadCommit {
			result.SyncStatus = "synced"
			result.HeadCommit = srcInfo.HeadCommit[:minLen(len(srcInfo.HeadCommit), 7)]
		} else if srcInfo.CommitTime > 0 && dstInfo.CommitTime > 0 {
			// Use timestamps to determine which side is newer.
			if srcInfo.CommitTime > dstInfo.CommitTime {
				// Origin is newer → backup is behind.
				result.SyncStatus = "behind"
			} else {
				// Backup is newer than origin → unexpected, flag as ahead.
				result.SyncStatus = "ahead"
			}
			result.OriginHead = srcInfo.HeadCommit[:minLen(len(srcInfo.HeadCommit), 7)]
			result.BackupHead = dstInfo.HeadCommit[:minLen(len(dstInfo.HeadCommit), 7)]
		} else {
			// No timestamps available — just report different.
			result.SyncStatus = "behind"
			result.OriginHead = srcInfo.HeadCommit[:minLen(len(srcInfo.HeadCommit), 7)]
			result.BackupHead = dstInfo.HeadCommit[:minLen(len(dstInfo.HeadCommit), 7)]
		}
	}
	return result
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// CheckAllMirrors queries live status for all repos in all mirror groups.
func CheckAllMirrors(ctx context.Context, cfg *config.Config) map[string][]StatusResult {
	results := make(map[string][]StatusResult)
	for mirrorKey, m := range cfg.Mirrors {
		for repoKey := range m.Repos {
			results[mirrorKey] = append(results[mirrorKey], CheckStatus(ctx, cfg, mirrorKey, repoKey))
		}
	}
	return results
}

// MirrorSummary summarizes mirror status counts for display.
type MirrorSummary struct {
	MirrorKey  string
	AccountSrc string
	AccountDst string
	Active    int
	Unchecked int
	Error     int
	Total     int
}

// Summarize returns a summary of all mirror groups.
// If liveResults is provided, counts are derived from live StatusResult data.
// Otherwise all repos are counted as Unchecked.
func Summarize(cfg *config.Config, liveResults map[string][]StatusResult) []MirrorSummary {
	var summaries []MirrorSummary
	for _, mirrorKey := range cfg.OrderedMirrorKeys() {
		m := cfg.Mirrors[mirrorKey]
		s := MirrorSummary{
			MirrorKey:  mirrorKey,
			AccountSrc: m.AccountSrc,
			AccountDst: m.AccountDst,
			Total:     len(m.Repos),
		}
		results := liveResults[mirrorKey]
		if len(results) > 0 {
			// Build lookup from live results.
			checked := make(map[string]StatusResult, len(results))
			for _, sr := range results {
				checked[sr.RepoKey] = sr
			}
			for repoKey := range m.Repos {
				sr, ok := checked[repoKey]
				if !ok {
					s.Unchecked++
				} else if sr.Error != "" {
					s.Error++
				} else if sr.Active {
					s.Active++
				} else {
					s.Unchecked++
				}
			}
		} else {
			s.Unchecked = s.Total
		}
		summaries = append(summaries, s)
	}
	return summaries
}
