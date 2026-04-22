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
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/heal"
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
// DestSourceKey + NewRepoKey identify where the config entry now
// lives so callers can re-clone or navigate to it.
type Result struct {
	NewOrigin           string
	DestRepoCreated     bool
	SourceRemoteDeleted bool
	LocalCloneDeleted   bool
	DestSourceKey       string
	NewRepoKey          string
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
	destAuthURL, pushExtraConfig, err := buildDestPushURL(plan.destAcct, req.DestOwner, req.DestRepoName, destPlainURL, plan.destAPIToken)
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
	if err := git.SetRemoteURL(req.SourceRepoPath, "origin", destPlainURL); err != nil {
		result.Err = fmt.Errorf("rewire origin: %w", err)
		return result
	}
	// Note: intentionally no post-rewire `git fetch` here. The push
	// --mirror above already synchronized every ref to the new origin,
	// so the remote-tracking refs will self-correct on the user's next
	// fetch. Running fetch here would fail against the destination
	// host because the repo-local .git/config still carries the source
	// account's credential helpers — those get reconciled by heal.Repo
	// below, which runs after the config is updated.

	// --- Phase 6: update config ------------------------------------
	// Update before the destructive phases so that even if delete-source
	// or delete-local fail, the gitbox config already reflects the new
	// home of the repo. After this, heal.Repo can apply the destination
	// account's credential/identity spec to the local clone.
	report(PhaseUpdateConfig, "Updating gitbox config...", nil)
	destSourceKey, newRepoKey, cfgErr := updateConfig(cfg, cfgPath, req)
	if cfgErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("update config: %v", cfgErr))
	} else {
		result.DestSourceKey = destSourceKey
		result.NewRepoKey = newRepoKey
		// Reconcile the local clone's .git/config to match the dest
		// account's spec (credential helper for the new host, identity,
		// canonical origin URL). Skip if the user opted to delete the
		// local clone — heal can't do anything useful on a soon-to-be-
		// removed folder.
		if !req.DeleteLocalClone {
			healReport := heal.Repo(cfg, destSourceKey, newRepoKey)
			for _, w := range healReport.Warnings {
				result.Warnings = append(result.Warnings, "post-move heal: "+w)
			}
		}
	}

	// --- Phase 7: delete source remote (best-effort) ---------------
	if req.DeleteSourceRemote {
		report(PhaseDeleteSource, "Deleting source remote repository...", nil)
		if plan.sourceDeleter == nil {
			warn := fmt.Sprintf("Source provider %q doesn't expose a repo-delete API — delete %s manually.", plan.sourceAcct.Provider, req.SourceRepoKey)
			result.Warnings = append(result.Warnings, warn)
			report(PhaseDeleteSource, warn, fmt.Errorf("not supported"))
		} else {
			srcOwner, srcName := splitRepoKey(req.SourceRepoKey)
			if err := plan.sourceDeleter.DeleteRepo(ctx, plan.sourceAcct.URL, plan.sourceAPIToken, plan.sourceAcct.Username, srcOwner, srcName); err != nil {
				shortMsg, fullMsg := humaniseSourceDeleteError(err, plan.sourceAcct.Provider, req.SourceAccountKey)
				result.Warnings = append(result.Warnings, fullMsg)
				report(PhaseDeleteSource, shortMsg, err)
			} else {
				result.SourceRemoteDeleted = true
			}
		}
	}

	// --- Phase 8: delete local clone (best-effort) -----------------
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
// invoked with.
//
//   - ssh:  "git@<host>:<owner>/<name>.git" — no embedded creds; SSH
//           agent / ssh config handles auth. No extraConfig.
//   - other: "https://<username>:<apiToken>@<host>/<owner>/<name>.git"
//           with extraConfig = ["credential.helper="] so a GCM helper
//           configured for the same host doesn't override the
//           embedded credential.
//
// apiToken must be the token already resolved by the preflight
// (plan.destAPIToken). For GCM-only destinations, that token is what
// `git credential fill` returned during the RepoExists / CreateRepo
// API calls — embedding it side-steps the "GCM must be cached at
// push time" problem, because we captured the cred once and reuse
// it inline for the push URL.
//
// Username is acct.Username (not the literal "token"). Some Forgejo
// versions reject Basic auth when the username is "token" even with a
// valid PAT.
func buildDestPushURL(acct config.Account, owner, repoName, plainHTTPSURL, apiToken string) (string, []string, error) {
	if acct.DefaultCredentialType == "ssh" {
		host := acct.URL
		if acct.SSH != nil && acct.SSH.Host != "" {
			host = acct.SSH.Host
		} else {
			host = stripScheme(host)
		}
		return fmt.Sprintf("git@%s:%s/%s.git", host, owner, repoName), nil, nil
	}
	u, err := url.Parse(plainHTTPSURL)
	if err != nil {
		return "", nil, fmt.Errorf("parsing destination URL: %w", err)
	}
	username := acct.Username
	if username == "" {
		username = "token"
	}
	u.User = url.UserPassword(username, apiToken)
	return u.String(), []string{"credential.helper="}, nil
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

// humaniseSourceDeleteError turns a DeleteRepo failure into two
// user-facing strings: a short line for the phase progress list and
// a longer, action-oriented warning with remediation details for
// the warnings section at the bottom of the modal. Splitting the
// two avoids duplicating a long paragraph inside the phase row.
func humaniseSourceDeleteError(err error, providerName, accountKey string) (short, full string) {
	var scopeErr *provider.InsufficientScopesError
	if errors.As(err, &scopeErr) {
		scopes := strings.Join(scopeErr.RequiredScopes, ", ")
		short = fmt.Sprintf("Source repo not deleted — %s PAT needs %q scope (see warnings).", providerName, scopes)
		regenURL := scopeErr.RemediationURL()
		if regenURL != "" {
			full = fmt.Sprintf(
				"Source repo delete refused: your %s PAT is missing the %q scope. Regenerate it at %s (keep existing scopes, add %q), then re-run `gitbox account credential setup %s`.",
				providerName, scopes, regenURL, scopes, accountKey,
			)
			return short, full
		}
		full = fmt.Sprintf(
			"Source repo delete refused: your %s PAT needs the %q scope. Regenerate the PAT (keep existing scopes, add %q) and store it again.",
			providerName, scopes, scopes,
		)
		return short, full
	}
	// Fall-through for non-scope failures: strip the redundant
	// "provider delete repo: " prefix that http.go helpers add, so
	// the surfaced string is compact and readable.
	msg := err.Error()
	for _, prefix := range []string{
		providerName + " delete repo: ",
		"github delete repo: ",
		"gitlab delete repo: ",
		"gitea delete repo: ",
		"forgejo delete repo: ",
		"bitbucket delete repo: ",
	} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	short = "Source repo not deleted (see warnings)."
	full = "Source repo delete failed: " + msg
	return short, full
}

// updateConfig moves the repo entry from the source-account source to
// the destination-account source, creating the latter if needed.
// Always moves the entry — even when the caller will subsequently
// delete the local clone. The destination repo exists on the provider
// regardless, and the user should see it under the destination account
// so they can re-clone it locally when they want.
//
// Returns the (destSourceKey, newRepoKey) that the entry now lives
// under so the caller can run heal.Repo against the fresh spec.
func updateConfig(cfg *config.Config, cfgPath string, req Request) (destSourceKey, newRepoKey string, err error) {
	srcBlock, ok := cfg.Sources[req.SourceSourceKey]
	if !ok {
		return "", "", fmt.Errorf("source %q missing", req.SourceSourceKey)
	}
	repoEntry, ok := srcBlock.Repos[req.SourceRepoKey]
	if !ok {
		return "", "", fmt.Errorf("repo %q missing in source %q", req.SourceRepoKey, req.SourceSourceKey)
	}

	newRepoKey = req.DestOwner + "/" + req.DestRepoName

	// Pick a source key owned by the destination account. Prefer
	// one that already exists; otherwise create a new source keyed
	// by the destination account name. On the unlikely collision
	// (source keyed by the dest account name but owned by a
	// different account), fall back to a `-moved` suffix.
	destSourceKey = req.DestAccountKey
	for sk, s := range cfg.Sources {
		if s.Account == req.DestAccountKey {
			destSourceKey = sk
			break
		}
	}
	if existing, ok := cfg.Sources[destSourceKey]; ok && existing.Account != req.DestAccountKey {
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

	// Refuse to overwrite an existing dest entry unless it's the
	// same slot the source currently lives in (no-op rename).
	if _, dup := dstBlock.Repos[newRepoKey]; dup && !(destSourceKey == req.SourceSourceKey && newRepoKey == req.SourceRepoKey) {
		return "", "", fmt.Errorf("destination config already has repo %q under source %q", newRepoKey, destSourceKey)
	}

	if destSourceKey != req.SourceSourceKey || newRepoKey != req.SourceRepoKey {
		delete(srcBlock.Repos, req.SourceRepoKey)
		cfg.Sources[req.SourceSourceKey] = srcBlock
	}
	dstBlock.Repos[newRepoKey] = repoEntry
	cfg.Sources[destSourceKey] = dstBlock
	if err := config.Save(cfg, cfgPath); err != nil {
		return "", "", err
	}
	return destSourceKey, newRepoKey, nil
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
