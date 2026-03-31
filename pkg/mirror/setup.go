package mirror

import (
	"context"
	"fmt"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/provider"
)

// SetupMirror configures a single mirror repo via provider APIs.
// It resolves tokens, creates the target repo if needed, and sets up the mirror.
func SetupMirror(ctx context.Context, cfg *config.Config, mirrorKey, repoKey string) SetupResult {
	m, ok := cfg.Mirrors[mirrorKey]
	if !ok {
		return SetupResult{RepoKey: repoKey, Error: fmt.Sprintf("mirror %q not found", mirrorKey)}
	}
	mr, ok := m.Repos[repoKey]
	if !ok {
		return SetupResult{RepoKey: repoKey, Error: fmt.Sprintf("repo %q not found in mirror %q", repoKey, mirrorKey)}
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
		return SetupResult{RepoKey: repoKey, Error: fmt.Sprintf("origin account %q not found", originKey)}
	}
	backupAcct, ok := cfg.Accounts[backupKey]
	if !ok {
		return SetupResult{RepoKey: repoKey, Error: fmt.Sprintf("backup account %q not found", backupKey)}
	}

	// Resolve API tokens (for local API calls — GCM OAuth is fine).
	originAPIToken, _, err := credential.ResolveAPIToken(originAcct, originKey)
	if err != nil {
		return SetupResult{RepoKey: repoKey, Error: fmt.Sprintf("origin token: %v", err)}
	}
	backupAPIToken, _, err := credential.ResolveAPIToken(backupAcct, backupKey)
	if err != nil {
		return SetupResult{RepoKey: repoKey, Error: fmt.Sprintf("backup token: %v", err)}
	}

	// Resolve mirror tokens (portable PATs for remote server use).
	// Push: backup token is sent to origin server for pushing to backup.
	// Pull: origin token is sent to backup server for pulling from origin.
	var remoteMirrorToken string
	switch mr.Direction {
	case "push":
		tok, _, err := credential.ResolveMirrorToken(backupAcct, backupKey)
		if err != nil {
			return SetupResult{RepoKey: repoKey, Error: fmt.Sprintf("backup mirror token: %v", err)}
		}
		remoteMirrorToken = tok
	case "pull":
		tok, _, err := credential.ResolveMirrorToken(originAcct, originKey)
		if err != nil {
			return SetupResult{RepoKey: repoKey, Error: fmt.Sprintf("origin mirror token: %v", err)}
		}
		remoteMirrorToken = tok
	}

	// Resolve target repo name.
	targetRepoName := repoKey
	if mr.TargetRepo != "" {
		targetRepoName = mr.TargetRepo
	}

	// Get providers.
	originProv, err := provider.ByName(originAcct.Provider)
	if err != nil {
		return SetupResult{RepoKey: repoKey, Error: fmt.Sprintf("origin provider: %v", err)}
	}
	backupProv, err := provider.ByName(backupAcct.Provider)
	if err != nil {
		return SetupResult{RepoKey: repoKey, Error: fmt.Sprintf("backup provider: %v", err)}
	}

	switch mr.Direction {
	case "push":
		return setupPushMirror(ctx, originProv, backupProv, originAcct, backupAcct, originAPIToken, backupAPIToken, remoteMirrorToken, repoKey, targetRepoName)
	case "pull":
		return setupPullMirror(ctx, originProv, backupProv, originAcct, backupAcct, originAPIToken, backupAPIToken, remoteMirrorToken, repoKey, targetRepoName)
	default:
		return SetupResult{RepoKey: repoKey, Error: fmt.Sprintf("unknown direction %q", mr.Direction)}
	}
}

// setupPushMirror: origin server pushes to backup.
// 1. Create target repo on backup (if supported and needed)
// 2. Create push mirror on origin (if supported)
func setupPushMirror(ctx context.Context, originProv, backupProv provider.Provider, originAcct, backupAcct config.Account, originToken, backupToken, remoteMirrorToken, repoKey, targetRepoName string) SetupResult {
	result := SetupResult{RepoKey: repoKey, Method: "api"}

	// Create target repo on backup if possible.
	if rc, ok := backupProv.(provider.RepoCreator); ok {
		targetName := repoNameOnly(targetRepoName)
		targetOwner := repoOwner(targetRepoName)

		exists, err := rc.RepoExists(ctx, backupAcct.URL, backupToken, backupAcct.Username, targetOwner, targetName)
		if err != nil {
			result.Error = fmt.Sprintf("checking target repo: %v", err)
			return result
		}
		if !exists {
			if err := rc.CreateRepo(ctx, backupAcct.URL, backupToken, backupAcct.Username, targetOwner, targetName, "", true); err != nil {
				// 422 "already exists" is not an error — race condition or previous attempt.
				if !strings.Contains(err.Error(), "already exists") {
					result.Error = fmt.Sprintf("creating target repo: %v", err)
					return result
				}
			} else {
				result.Created = true
			}
		}
	}

	// Set up push mirror on origin.
	pm, ok := originProv.(provider.PushMirrorProvider)
	if !ok {
		// Provider doesn't support push mirrors — return manual guide.
		result.Method = "manual"
		result.Instructions = ManualSetupGuide(originAcct.Provider, "push")
		return result
	}

	owner := repoOwner(repoKey)
	name := repoNameOnly(repoKey)
	// Target URL uses the backup account's username as owner, not the origin's org.
	// e.g., "https://github.com/LuisPalacios/migra-forgejo.git" not ".../infra/migra-forgejo.git"
	targetRepoOnly := repoNameOnly(targetRepoName)
	targetCloneURL := fmt.Sprintf("%s/%s/%s.git", strings.TrimRight(backupAcct.URL, "/"), backupAcct.Username, targetRepoOnly)

	if err := pm.CreatePushMirror(ctx, originAcct.URL, originToken, originAcct.Username, owner, name, targetCloneURL, remoteMirrorToken); err != nil {
		result.Error = fmt.Sprintf("creating push mirror: %v", err)
		return result
	}

	result.Mirrored = true
	return result
}

// setupPullMirror: backup server pulls from origin.
// Uses the migrate/mirror API on the backup side.
func setupPullMirror(ctx context.Context, originProv, backupProv provider.Provider, originAcct, backupAcct config.Account, originToken, backupToken, remoteMirrorToken, repoKey, targetRepoName string) SetupResult {
	result := SetupResult{RepoKey: repoKey, Method: "api"}

	pm, ok := backupProv.(provider.PullMirrorProvider)
	if !ok {
		result.Method = "manual"
		result.Instructions = ManualSetupGuide(backupAcct.Provider, "pull")
		return result
	}

	sourceCloneURL := fmt.Sprintf("%s/%s.git", strings.TrimRight(originAcct.URL, "/"), repoKey)

	targetName := repoNameOnly(targetRepoName)
	if err := pm.CreatePullMirror(ctx, backupAcct.URL, backupToken, backupAcct.Username, targetName, sourceCloneURL, remoteMirrorToken, true); err != nil {
		result.Error = fmt.Sprintf("creating pull mirror: %v", err)
		return result
	}

	result.Created = true
	result.Mirrored = true
	return result
}

// SetupAll runs setup for all repos in a mirror group.
func SetupAll(ctx context.Context, cfg *config.Config, mirrorKey string) []SetupResult {
	m, ok := cfg.Mirrors[mirrorKey]
	if !ok {
		return nil
	}
	var results []SetupResult
	for repoKey := range m.Repos {
		results = append(results, SetupMirror(ctx, cfg, mirrorKey, repoKey))
	}
	return results
}

// repoOwner extracts "org" from "org/repo".
func repoOwner(fullName string) string {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

// repoNameOnly extracts "repo" from "org/repo".
func repoNameOnly(fullName string) string {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return fullName
}

