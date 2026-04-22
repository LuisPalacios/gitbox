// Package heal verifies and repairs the repo-local .git/config of a
// cloned repository so it matches what the gitbox config says it
// should be. Idempotent: safe to call repeatedly. Cheap: compares
// current values first and only writes when drift is detected.
//
// Three areas are healed:
//   - user.name / user.email (per-repo identity)
//   - origin URL (credential-type-specific shape; no embedded secrets)
//   - credential.helper + per-host credential.* (delegated to
//     pkg/credential.ConfigureRepoCredential)
package heal

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/identity"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// Report summarizes what a single Repo heal did. Zero-value means
// either the repo wasn't cloned or nothing drifted.
type Report struct {
	RepoKey   string
	Path      string
	Fixed     []string // human-readable descriptions of each fix applied
	Warnings  []string // soft failures (e.g. token resolve failed)
	Skipped   string   // non-empty when the repo was intentionally not healed
}

// HasWork reports whether the heal touched anything — either a fix or
// a warning. Useful for deciding when to log or emit a UI event.
func (r Report) HasWork() bool {
	return len(r.Fixed) > 0 || len(r.Warnings) > 0
}

// Repo heals a single cloned repository to match the gitbox config.
// Returns a report describing what changed. Cheap when nothing drifts
// — reads are ~3-4 git config lookups and no writes.
//
// Preconditions (caller must validate):
//   - cfg.Sources[sourceKey] exists and contains repoKey.
//   - The clone's local path is computable from the config.
//
// Heal skips gracefully when:
//   - The repo is not cloned on disk (Skipped: "not cloned").
//   - The clone path is not a git repo (Skipped: "not a git repo").
func Repo(cfg *config.Config, sourceKey, repoKey string) Report {
	r := Report{RepoKey: sourceKey + "/" + repoKey}

	src, ok := cfg.Sources[sourceKey]
	if !ok {
		r.Warnings = append(r.Warnings, fmt.Sprintf("source %q not found in config", sourceKey))
		return r
	}
	repo, ok := src.Repos[repoKey]
	if !ok {
		r.Warnings = append(r.Warnings, fmt.Sprintf("repo %q not found in source %q", repoKey, sourceKey))
		return r
	}
	acct, ok := cfg.Accounts[src.Account]
	if !ok {
		r.Warnings = append(r.Warnings, fmt.Sprintf("account %q not found", src.Account))
		return r
	}

	globalFolder := config.ExpandTilde(cfg.Global.Folder)
	sourceFolder := src.EffectiveFolder(sourceKey)
	path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
	r.Path = path

	if !git.IsRepo(path) {
		r.Skipped = "not cloned"
		return r
	}

	// --- identity ------------------------------------------------
	wantName, wantEmail := identity.ResolveIdentity(repo, acct)
	// Only heal identity when both values are resolvable. An account
	// with no name/email configured shouldn't cause us to wipe a
	// user-set repo identity — be conservative.
	if wantName != "" || wantEmail != "" {
		fixedName, fixedEmail, err := identity.EnsureRepoIdentity(path, wantName, wantEmail)
		switch {
		case err != nil:
			r.Warnings = append(r.Warnings, fmt.Sprintf("identity: %v", err))
		case fixedName && fixedEmail:
			r.Fixed = append(r.Fixed, fmt.Sprintf("set user.name=%q, user.email=%q", wantName, wantEmail))
		case fixedName:
			r.Fixed = append(r.Fixed, fmt.Sprintf("set user.name=%q", wantName))
		case fixedEmail:
			r.Fixed = append(r.Fixed, fmt.Sprintf("set user.email=%q", wantEmail))
		}
	}

	// --- origin URL ---------------------------------------------
	credType := repo.EffectiveCredentialType(&acct)
	wantURL := ExpectedOriginURL(acct, repoKey, credType)
	curURL, urlErr := git.RemoteURL(path)
	if urlErr == nil && wantURL != "" && curURL != wantURL {
		if err := git.SetRemoteURL(path, "origin", wantURL); err != nil {
			r.Warnings = append(r.Warnings, fmt.Sprintf("origin URL: %v", err))
		} else {
			r.Fixed = append(r.Fixed, fmt.Sprintf("rewrote origin URL %q → %q", redactURL(curURL), wantURL))
		}
	} else if urlErr != nil {
		// A clone without an origin remote is highly unusual; surface
		// it so the operator can investigate.
		r.Warnings = append(r.Warnings, fmt.Sprintf("origin URL read failed: %v", urlErr))
	}

	// --- credential config ---------------------------------------
	// ConfigureRepoCredential is idempotent: it unsets and re-adds the
	// credential helpers every call. That's slightly more writes than
	// a compare-first flow, but the value space (helper, credentialStore,
	// per-host username/provider/useHttpPath) is wide enough that an
	// equality check would duplicate most of ConfigureRepoCredential's
	// logic. Just call it and report one line.
	//
	// Token accounts with no token file yet would fail inside
	// configureToken via ResolveToken — catch that as a warning rather
	// than a fatal fix.
	if err := credential.ConfigureRepoCredential(path, acct, src.Account, credType, cfg.Global); err != nil {
		r.Warnings = append(r.Warnings, fmt.Sprintf("credential config: %v", err))
	} else {
		// We can't easily tell if this call actually changed anything
		// without reading every key first. Don't add to Fixed unless
		// we detected drift above — avoid noise.
	}

	return r
}

// ExpectedOriginURL returns the canonical origin URL for a repo given
// its account and credential type. This is what heal (and clone)
// should ensure is stored as `remote.origin.url`. No embedded
// credentials — even for the token type, the secret lives in the
// credential-store file, not the URL.
func ExpectedOriginURL(acct config.Account, repoKey, credType string) string {
	switch credType {
	case "ssh":
		host := acct.URL
		if acct.SSH != nil && acct.SSH.Host != "" {
			host = acct.SSH.Host
		} else {
			host = stripScheme(host)
		}
		return fmt.Sprintf("git@%s:%s.git", host, repoKey)
	default:
		// HTTPS form with username@host (no password). For token
		// accounts the credential-store file supplies the password
		// at git-auth time; for GCM it's fetched from the OS keyring.
		if acct.Username == "" {
			return fmt.Sprintf("%s/%s.git", strings.TrimRight(acct.URL, "/"), repoKey)
		}
		u, err := url.Parse(acct.URL)
		if err != nil {
			return fmt.Sprintf("%s/%s.git", strings.TrimRight(acct.URL, "/"), repoKey)
		}
		u.User = url.User(acct.Username)
		return fmt.Sprintf("%s/%s.git", strings.TrimRight(u.String(), "/"), repoKey)
	}
}

// All heals every cloned repo in the config. Returns a list of reports
// filtered to those that actually did work (HasWork) — callers that
// want to see skipped entries should use Repo() per key.
func All(cfg *config.Config) []Report {
	var reports []Report
	for _, sourceKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[sourceKey]
		for _, repoKey := range src.OrderedRepoKeys() {
			r := Repo(cfg, sourceKey, repoKey)
			if r.HasWork() {
				reports = append(reports, r)
			}
		}
	}
	return reports
}

// --- Helpers ------------------------------------------------------

// stripScheme removes "https://" or "http://" from a URL so it can be
// used in the SSH-form remote.
func stripScheme(raw string) string {
	for _, p := range []string{"https://", "http://"} {
		if strings.HasPrefix(raw, p) {
			return raw[len(p):]
		}
	}
	return raw
}

// redactURL masks a password component for safe logging.
func redactURL(raw string) string {
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.User != nil {
		if _, hasPW := u.User.Password(); hasPW {
			u.User = url.UserPassword(u.User.Username(), "***")
		}
	}
	return u.String()
}
