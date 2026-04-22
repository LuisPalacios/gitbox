// Package move relocates a repository from one configured account /
// provider to another. It orchestrates a phased flow: preflight →
// fetch → create-destination → push-mirror → rewire-origin → optional
// delete-source-remote → optional delete-local-clone → update-config.
//
// Strictness: phases 1-5 are fatal on failure; phases 6-7 are
// best-effort and failures are captured as warnings so the caller can
// still report a successful move with caveats.
package move

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// Request carries everything Move needs. Callers resolve account /
// source keys before invoking.
type Request struct {
	SourceAccountKey string
	SourceSourceKey  string
	SourceRepoKey    string // "owner/repo"
	SourceRepoPath   string // local clone path

	DestAccountKey string
	DestOwner      string // username or org slug on the destination provider
	DestRepoName   string // new name (without owner/)
	DestPrivate    bool

	DeleteSourceRemote bool
	DeleteLocalClone   bool
}

// Phase identifies a move step for the progress callback.
type Phase string

const (
	PhasePreflight    Phase = "preflight"
	PhaseFetch        Phase = "fetch"
	PhaseCreateDest   Phase = "create_dest"
	PhasePushMirror   Phase = "push_mirror"
	PhaseRewireOrigin Phase = "rewire_origin"
	PhaseDeleteSource Phase = "delete_source"
	PhaseDeleteLocal  Phase = "delete_local"
	PhaseUpdateConfig Phase = "update_config"
	PhaseDone         Phase = "done"
)

// Progress is delivered to the caller on every phase transition.
// Err is non-nil for best-effort phases that failed but did not abort
// the overall move (i.e. phases 6-7).
type Progress struct {
	Phase   Phase
	Message string
	Err     error
}

// Result summarizes the move outcome. Err is set only when a fatal
// phase (1-5) failed. Best-effort failures populate Warnings.
type Result struct {
	NewOrigin           string
	DestRepoCreated     bool
	SourceRemoteDeleted bool
	LocalCloneDeleted   bool
	Warnings            []string
	Err                 error
}

// Move runs the full flow. onProgress is called synchronously before
// each phase starts; pass nil if no progress reporting is needed.
func Move(ctx context.Context, cfg *config.Config, cfgPath string, req Request, onProgress func(Progress)) Result {
	report := func(phase Phase, msg string, err error) {
		if onProgress != nil {
			onProgress(Progress{Phase: phase, Message: msg, Err: err})
		}
	}
	result := Result{}

	// --- Phase 1: preflight ----------------------------------------
	report(PhasePreflight, "Checking clone status and credentials...", nil)
	plan, err := Preflight(ctx, cfg, req)
	if err != nil {
		result.Err = fmt.Errorf("preflight: %w", err)
		return result
	}

	// --- Phase 2: fetch --------------------------------------------
	report(PhaseFetch, "Fetching tags and pruning stale refs...", nil)
	if err := git.FetchTagsAndPrune(req.SourceRepoPath); err != nil {
		result.Err = fmt.Errorf("fetch: %w", err)
		return result
	}

	// --- Phase 3: create destination -------------------------------
	report(PhaseCreateDest, fmt.Sprintf("Creating %s/%s on %s...", req.DestOwner, req.DestRepoName, plan.destAcct.Provider), nil)
	apiOwner := req.DestOwner
	if apiOwner == plan.destAcct.Username {
		apiOwner = ""
	}
	if err := plan.destCreator.CreateRepo(ctx, plan.destAcct.URL, plan.destAPIToken, plan.destAcct.Username, apiOwner, req.DestRepoName, "", req.DestPrivate); err != nil {
		result.Err = fmt.Errorf("create destination: %w", err)
		return result
	}
	result.DestRepoCreated = true

	// --- Phase 4: push --mirror to dest ----------------------------
	report(PhasePushMirror, "Pushing every ref and tag to the destination...", nil)
	destPlainURL := buildHTTPSCloneURL(plan.destAcct, req.DestOwner, req.DestRepoName)
	destAuthURL, pushExtraConfig, err := buildDestPushURL(plan.destAcct, req.DestAccountKey, req.DestOwner, req.DestRepoName, destPlainURL)
	if err != nil {
		result.Err = fmt.Errorf("build destination push URL: %w", err)
		return result
	}
	if _, err := git.PushMirror(req.SourceRepoPath, destAuthURL, pushExtraConfig); err != nil {
		result.Err = fmt.Errorf("push --mirror: %w", err)
		return result
	}

	// --- Phase 5: rewire origin ------------------------------------
	report(PhaseRewireOrigin, "Rewiring local origin to the destination...", nil)
	result.NewOrigin = destPlainURL
	if err := git.SetRemoteURLCaptured(req.SourceRepoPath, "origin", destPlainURL); err != nil {
		result.Err = fmt.Errorf("rewire origin: %w", err)
		return result
	}
	// Refresh remote refs + set upstream for the current branch so the
	// next `git status` reflects the new origin. Both failures are
	// non-fatal — the repo is already on the new origin, user can fix
	// upstream tracking manually if needed.
	if err := git.FetchTagsAndPrune(req.SourceRepoPath); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("fetch after rewire: %v", err))
	} else if branch, err := git.CurrentBranch(req.SourceRepoPath); err == nil && branch != "" && branch != "(detached)" {
		if err := git.SetUpstream(req.SourceRepoPath, branch, "origin"); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("set upstream for %s: %v", branch, err))
		}
	}

	// --- Phase 6: delete source remote (best-effort) ---------------
	if req.DeleteSourceRemote {
		report(PhaseDeleteSource, "Deleting source remote repository...", nil)
		if plan.sourceDeleter == nil {
			warn := fmt.Sprintf("source provider %s does not support repository deletion via API — delete it manually", plan.sourceAcct.Provider)
			result.Warnings = append(result.Warnings, warn)
			report(PhaseDeleteSource, warn, fmt.Errorf("not supported"))
		} else {
			srcOwner, srcName := splitRepoKey(req.SourceRepoKey)
			if err := plan.sourceDeleter.DeleteRepo(ctx, plan.sourceAcct.URL, plan.sourceAPIToken, plan.sourceAcct.Username, srcOwner, srcName); err != nil {
				warn := fmt.Sprintf("delete source remote: %v", err)
				result.Warnings = append(result.Warnings, warn)
				report(PhaseDeleteSource, warn, err)
			} else {
				result.SourceRemoteDeleted = true
			}
		}
	}

	// --- Phase 7: delete local clone (best-effort) -----------------
	if req.DeleteLocalClone {
		report(PhaseDeleteLocal, "Removing local clone folder...", nil)
		if err := os.RemoveAll(req.SourceRepoPath); err != nil {
			warn := fmt.Sprintf("delete local clone: %v", err)
			result.Warnings = append(result.Warnings, warn)
			report(PhaseDeleteLocal, warn, err)
		} else {
			result.LocalCloneDeleted = true
		}
	}

	// --- Phase 8: update config ------------------------------------
	report(PhaseUpdateConfig, "Updating gitbox config...", nil)
	if err := updateConfig(cfg, cfgPath, req, result.LocalCloneDeleted); err != nil {
		// Rare. Config integrity error AFTER a successful move is a
		// warning, not a fatal — the filesystem/remote state is
		// already correct, the user can repair via the GUI.
		result.Warnings = append(result.Warnings, fmt.Sprintf("update config: %v", err))
	}

	report(PhaseDone, "Move complete.", nil)
	return result
}

// buildHTTPSCloneURL returns the public HTTPS clone URL for a repo on
// an account. It is independent of credential type — embedded-auth
// handling lives in buildDestPushURL.
func buildHTTPSCloneURL(acct config.Account, owner, repoName string) string {
	base := strings.TrimRight(acct.URL, "/")
	full := owner + "/" + repoName
	// If the base URL is already an HTTPS URL, prefer that; otherwise
	// fall back to a hostname-only form (SSH accounts don't usually
	// store a web URL at all, so the resulting value may be empty —
	// callers are expected to have validated this in Preflight).
	if _, err := url.Parse(base); err == nil {
		return fmt.Sprintf("%s/%s.git", base, full)
	}
	return fmt.Sprintf("https://%s/%s.git", base, full)
}

// buildDestPushURL builds the URL passed to `git push --mirror` for
// the destination, plus any `-c key=value` config args git should be
// invoked with. The shape depends on the destination account's
// credential type:
//
//   - ssh:   "git@<host>:<owner>/<name>.git" (no embedded creds; SSH
//            agent / ssh config handles auth). No extraConfig.
//   - token: "https://<username>:<PAT>@<host>/<owner>/<name>.git" with
//            extraConfig = ["credential.helper="] so a GCM helper
//            configured for the same host doesn't override the
//            embedded credential.
//   - gcm:   "https://<username>@<host>/<owner>/<name>.git" with no
//            embedded PAT — GCM must have valid cached creds for the
//            host/username (the GUI has GCM_INTERACTIVE=never so there's
//            no prompt fallback).
//
// For Forgejo/Gitea, using the account's actual username (not the
// literal "token") matters: some Forgejo versions reject Basic auth
// when the username is "token" even with a valid PAT.
func buildDestPushURL(acct config.Account, accountKey, owner, repoName, plainHTTPSURL string) (string, []string, error) {
	credType := acct.DefaultCredentialType
	switch credType {
	case "ssh":
		host := acct.URL
		if acct.SSH != nil && acct.SSH.Host != "" {
			host = acct.SSH.Host
		} else {
			host = stripScheme(host)
		}
		return fmt.Sprintf("git@%s:%s/%s.git", host, owner, repoName), nil, nil
	case "token":
		tok, _, err := credential.ResolveToken(acct, accountKey)
		if err != nil {
			return "", nil, fmt.Errorf("resolving destination token: %w", err)
		}
		u, err := url.Parse(plainHTTPSURL)
		if err != nil {
			return "", nil, fmt.Errorf("parsing destination URL: %w", err)
		}
		username := acct.Username
		if username == "" {
			username = "token"
		}
		u.User = url.UserPassword(username, tok)
		return u.String(), []string{"credential.helper="}, nil
	default:
		// GCM (or unset — treat as GCM).
		u, err := url.Parse(plainHTTPSURL)
		if err != nil {
			return "", nil, fmt.Errorf("parsing destination URL: %w", err)
		}
		if acct.Username != "" {
			u.User = url.User(acct.Username)
		}
		return u.String(), nil, nil
	}
}

// stripScheme removes "https://" or "http://" from a URL so the host
// can be used in an SSH-form remote. Mirrors the helper in cmd/cli/tui.
func stripScheme(raw string) string {
	for _, p := range []string{"https://", "http://"} {
		if strings.HasPrefix(raw, p) {
			return raw[len(p):]
		}
	}
	return raw
}

// splitRepoKey splits "owner/repo" → ("owner", "repo").
// Returns ("", key) if no slash is present.
func splitRepoKey(key string) (owner, name string) {
	if i := strings.IndexByte(key, '/'); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
}

// updateConfig moves the repo entry from the source-account source to a
// destination-account source, creating the latter if needed. When the
// local clone was already deleted in phase 7, just remove the entry.
// The destination source's key defaults to the destination account key.
func updateConfig(cfg *config.Config, cfgPath string, req Request, localCloneDeleted bool) error {
	// Step 7 already moved the physical files off-disk AND the user
	// opted out of keeping local state; just drop the source config
	// entry.
	if localCloneDeleted {
		_ = cfg.DeleteRepo(req.SourceSourceKey, req.SourceRepoKey)
		return config.Save(cfg, cfgPath)
	}

	srcBlock, ok := cfg.Sources[req.SourceSourceKey]
	if !ok {
		return fmt.Errorf("source %q missing", req.SourceSourceKey)
	}
	repoEntry, ok := srcBlock.Repos[req.SourceRepoKey]
	if !ok {
		return fmt.Errorf("repo %q missing in source %q", req.SourceRepoKey, req.SourceSourceKey)
	}

	// Preserve per-repo overrides (credential_type, identity, folders).
	newRepoKey := req.DestOwner + "/" + req.DestRepoName

	// If the same account owns both old and new sources, it's a
	// rename within the same block — avoid creating a duplicate
	// source entry.
	destSourceKey := req.DestAccountKey
	if existing, ok := cfg.Sources[destSourceKey]; ok && existing.Account != req.DestAccountKey {
		// Very unlikely: a source keyed by the destination account
		// name but owned by a different account. Pick a fresh key.
		destSourceKey = req.DestAccountKey + "-moved"
	}
	dstBlock, ok := cfg.Sources[destSourceKey]
	if !ok {
		dstBlock = config.Source{
			Account: req.DestAccountKey,
			Repos:   make(map[string]config.Repo),
		}
	}
	if dstBlock.Repos == nil {
		dstBlock.Repos = make(map[string]config.Repo)
	}

	// Refuse to overwrite an existing dest entry. Preflight should
	// have caught this, but double-check to avoid data loss if the
	// config was edited between steps.
	if _, dup := dstBlock.Repos[newRepoKey]; dup && destSourceKey == req.SourceSourceKey && newRepoKey == req.SourceRepoKey {
		// same entry — fine
	} else if _, dup := dstBlock.Repos[newRepoKey]; dup {
		return fmt.Errorf("destination config already has repo %q under source %q", newRepoKey, destSourceKey)
	}

	// Remove source entry (if we're moving across sources, or if the
	// repo key changed inside the same source).
	if destSourceKey != req.SourceSourceKey || newRepoKey != req.SourceRepoKey {
		delete(srcBlock.Repos, req.SourceRepoKey)
		cfg.Sources[req.SourceSourceKey] = srcBlock
	}
	dstBlock.Repos[newRepoKey] = repoEntry
	cfg.Sources[destSourceKey] = dstBlock
	return config.Save(cfg, cfgPath)
}

// resolvedPlan holds everything Move() computes once up front so each
// phase can grab what it needs without re-running token resolution.
type resolvedPlan struct {
	sourceAcct     config.Account
	destAcct       config.Account
	sourceAPIToken string
	destAPIToken   string
	destCreator    provider.RepoCreator
	sourceDeleter  provider.RepoDeleter // may be nil if source provider has no delete API
}

// --- Preflight ---

// Preflight validates everything before the caller commits to the move.
// Returns a plan on success and an error describing the first failing
// check otherwise. Safe to call from a UI to surface problems BEFORE
// the user types the repo name to confirm.
func Preflight(ctx context.Context, cfg *config.Config, req Request) (plan *resolvedPlan, err error) {
	if req.SourceAccountKey == "" || req.DestAccountKey == "" {
		return nil, fmt.Errorf("source and destination account keys required")
	}
	if req.SourceSourceKey == "" || req.SourceRepoKey == "" {
		return nil, fmt.Errorf("source repo identifier required")
	}
	if req.DestOwner == "" || req.DestRepoName == "" {
		return nil, fmt.Errorf("destination owner and repo name required")
	}
	if req.SourceRepoPath == "" {
		return nil, fmt.Errorf("source repo path required")
	}

	// Reject obvious no-ops: same account+owner+name as the source.
	if req.SourceAccountKey == req.DestAccountKey {
		srcOwner, srcName := splitRepoKey(req.SourceRepoKey)
		if srcOwner == req.DestOwner && srcName == req.DestRepoName {
			return nil, fmt.Errorf("destination is identical to source")
		}
	}

	srcAcct, ok := cfg.Accounts[req.SourceAccountKey]
	if !ok {
		return nil, fmt.Errorf("source account %q not found", req.SourceAccountKey)
	}
	dstAcct, ok := cfg.Accounts[req.DestAccountKey]
	if !ok {
		return nil, fmt.Errorf("destination account %q not found", req.DestAccountKey)
	}

	// Local state: must be cloned + clean + fully synced.
	if !git.IsRepo(req.SourceRepoPath) {
		return nil, fmt.Errorf("source repo is not cloned at %s", req.SourceRepoPath)
	}
	rs := status.Check(req.SourceRepoPath)
	switch rs.State {
	case status.Clean:
		// OK
	case status.NoUpstream:
		return nil, fmt.Errorf("source repo has no upstream — cannot safely move")
	case status.NotCloned:
		return nil, fmt.Errorf("source repo is not cloned")
	default:
		return nil, fmt.Errorf("source repo is not clean (state: %s) — commit/push/fetch first", rs.State)
	}
	if rs.Ahead > 0 || rs.Behind > 0 {
		return nil, fmt.Errorf("source repo is %d ahead, %d behind — sync with origin first", rs.Ahead, rs.Behind)
	}

	// Resolve tokens.
	srcTok, _, err := credential.ResolveAPIToken(srcAcct, req.SourceAccountKey)
	if err != nil {
		return nil, fmt.Errorf("source credentials: %w", err)
	}
	dstTok, _, err := credential.ResolveAPIToken(dstAcct, req.DestAccountKey)
	if err != nil {
		return nil, fmt.Errorf("destination credentials: %w", err)
	}

	// Destination provider must support repo creation.
	dstProv, err := provider.ByName(dstAcct.Provider)
	if err != nil {
		return nil, fmt.Errorf("destination provider: %w", err)
	}
	dstCreator, ok := dstProv.(provider.RepoCreator)
	if !ok {
		return nil, fmt.Errorf("destination provider %q does not support repository creation", dstAcct.Provider)
	}
	// Dest repo must not already exist.
	apiOwner := req.DestOwner
	if apiOwner == dstAcct.Username {
		apiOwner = ""
	}
	exists, err := dstCreator.RepoExists(ctx, dstAcct.URL, dstTok, dstAcct.Username, req.DestOwner, req.DestRepoName)
	if err != nil {
		return nil, fmt.Errorf("checking destination repo: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("destination repo %s/%s already exists", req.DestOwner, req.DestRepoName)
	}

	// Source deleter is optional — only required when the user opted in.
	var srcDeleter provider.RepoDeleter
	if srcProv, err := provider.ByName(srcAcct.Provider); err == nil {
		if d, ok := srcProv.(provider.RepoDeleter); ok {
			srcDeleter = d
		}
	}
	if req.DeleteSourceRemote && srcDeleter == nil {
		return nil, fmt.Errorf("source provider %q does not support repository deletion via API — uncheck \"Delete source repository\" or delete it manually", srcAcct.Provider)
	}

	// Local clone path sanity: must be a real directory under the
	// global folder, to guard against surprise rm -rf. We don't block
	// the move if it's outside the managed folder, but we refuse to
	// delete it in phase 7 unless the path resolves to an absolute
	// directory. This protects against empty/relative path bugs.
	if req.DeleteLocalClone {
		if !filepath.IsAbs(req.SourceRepoPath) {
			return nil, fmt.Errorf("internal error: local clone path is not absolute (%q)", req.SourceRepoPath)
		}
	}

	return &resolvedPlan{
		sourceAcct:     srcAcct,
		destAcct:       dstAcct,
		sourceAPIToken: srcTok,
		destAPIToken:   dstTok,
		destCreator:    dstCreator,
		sourceDeleter:  srcDeleter,
	}, nil
}
