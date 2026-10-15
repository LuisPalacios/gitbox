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
	"os"
	"path/filepath"
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
	RepoKey  string
	Path     string
	Fixed    []string // human-readable descriptions of each fix applied
	Warnings []string // soft failures (e.g. token resolve failed)
	Skipped  string   // non-empty when the repo was intentionally not healed
}

// HasWork reports whether the heal touched anything — either a fix or
// a warning. Useful for deciding when to log or emit a UI event.
func (r Report) HasWork() bool {
	return len(r.Fixed) > 0 || len(r.Warnings) > 0
}

// Repo heals a single cloned repository to match the gitbox config.
// Returns a report describing what changed. Cheap when nothing drifts
// — reads are two batched config lookups and no writes.
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

	if !git.IsRepo(path) && !isLinkedWorktree(path) {
		r.Skipped = "not cloned"
		return r
	}

	snapshot, err := git.ReadConfigSnapshot(path)
	if err != nil {
		r.Warnings = append(r.Warnings, fmt.Sprintf("reading repo config: %v", err))
		return r
	}
	credType := repo.EffectiveCredentialType(&acct)
	if credType != "ssh" && credType != "token" && credType != "gcm" {
		r.Warnings = append(r.Warnings, fmt.Sprintf("credential config: unknown credential type %q", credType))
		return r
	}
	// --- identity ------------------------------------------------
	wantName, wantEmail := identity.ResolveIdentity(repo, acct)
	if wantName != "" || wantEmail != "" {
		for _, setting := range []struct{ key, want string }{{"user.name", wantName}, {"user.email", wantEmail}} {
			if snapshot.Get(setting.key) == setting.want {
				continue
			}
			if err := git.ConfigSet(path, setting.key, setting.want); err != nil {
				r.Warnings = append(r.Warnings, fmt.Sprintf("identity: %v", err))
			} else {
				r.Fixed = append(r.Fixed, fmt.Sprintf("set %s=%q", setting.key, setting.want))
			}
		}
	}

	// --- origin URL ---------------------------------------------
	wantURL := ExpectedOriginURL(acct, repoKey, credType)
	curURL := ""
	if urls := snapshot.All("remote.origin.url"); len(urls) > 0 {
		curURL = urls[0]
	}
	if curURL != "" && wantURL != "" && curURL != wantURL {
		if err := git.SetRemoteURL(path, "origin", wantURL); err != nil {
			r.Warnings = append(r.Warnings, fmt.Sprintf("origin URL: %v", err))
		} else {
			r.Fixed = append(r.Fixed, fmt.Sprintf("rewrote origin URL %q → %q", redactURL(curURL), wantURL))
		}
	} else if curURL == "" {
		// A clone without an origin remote is highly unusual; surface
		// it so the operator can investigate.
		r.Warnings = append(r.Warnings, "origin URL is missing")
	}

	// --- credential config ---------------------------------------
	if err := credential.ConfigureRepoCredentialFromSnapshot(path, acct, src.Account, credType, cfg.Global, snapshot); err != nil {
		r.Warnings = append(r.Warnings, fmt.Sprintf("credential config: %v", err))
	}

	if len(r.Fixed) > 0 {
		// A later include or worktree config may override a successful local
		// write. Surface that instead of fetching with another identity/origin.
		after, err := git.ReadConfigSnapshot(path)
		if err != nil {
			r.Warnings = append(r.Warnings, fmt.Sprintf("validating repo config: %v", err))
		} else {
			if (wantName != "" || wantEmail != "") && (after.Get("user.name") != wantName || after.Get("user.email") != wantEmail) {
				r.Warnings = append(r.Warnings, "identity is overridden by included or worktree config")
			}
			if urls := after.All("remote.origin.url"); len(urls) == 0 || urls[0] != wantURL {
				r.Warnings = append(r.Warnings, "origin URL is overridden by included or worktree config")
			}
		}
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

// Linked worktrees use a .git pointer file rather than a directory.
func isLinkedWorktree(path string) bool {
	data, err := os.ReadFile(filepath.Join(path, ".git"))
	return err == nil && strings.HasPrefix(string(data), "gitdir: ")
}
