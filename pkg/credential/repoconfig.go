package credential

import (
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// ConfigureRepoCredential sets per-repo .git/config entries that make this
// clone credential-independent from global git config. An empty credential.helper
// line cancels all inherited (global/system) helpers, then the type-specific
// helper is appended.
//
// Token → store --file <credentials-file>
// GCM   → manager (with credentialStore + per-host hints)
// SSH   → empty cancellation only (auth via ~/.ssh/config)
func ConfigureRepoCredential(repoPath string, acct config.Account, accountKey, credType string, globalCfg config.GlobalConfig) error {
	if err := validateCredentialType(credType); err != nil {
		return err
	}
	snapshot, err := git.ReadConfigSnapshot(repoPath)
	if err != nil {
		return fmt.Errorf("reading credential config: %w", err)
	}
	return ConfigureRepoCredentialFromSnapshot(repoPath, acct, accountKey, credType, globalCfg, snapshot)
}

// ConfigureRepoCredentialFromSnapshot shares a caller's config read with identity
// and origin checks. The snapshot must belong to this clone and this operation.
func ConfigureRepoCredentialFromSnapshot(repoPath string, acct config.Account, accountKey, credType string, globalCfg config.GlobalConfig, snapshot git.ConfigSnapshot) error {
	if err := validateCredentialType(credType); err != nil {
		return err
	}
	wantHelpers := RepoHelpers(accountKey, credType, globalCfg)
	host := strings.TrimSuffix(acct.URL, "/")
	section := fmt.Sprintf("credential.%s", host)
	want := map[string]string{}
	remove := []string{}
	switch credType {
	case "token":
		credFile := CredentialFilePath(accountKey)
		if err := WriteCredentialFile(credFile, acct, accountKey); err != nil {
			return fmt.Errorf("writing credential file: %w", err)
		}
	case "gcm":
		if gcm := globalCfg.CredentialGCM; gcm != nil && gcm.CredentialStore != "" {
			want["credential.credentialStore"] = gcm.CredentialStore
		}
		want[section+".username"] = acct.Username
		want[section+".useHttpPath"] = "false"
		if acct.GCM != nil && acct.GCM.Provider != "" {
			want[section+".provider"] = acct.GCM.Provider
		}
	}
	if credType != "gcm" {
		remove = []string{section + ".username", section + ".provider", section + ".useHttpPath", "credential.credentialStore"}
	}

	// An empty helper clears the inherited chain. Compare the effective suffix
	// as well as direct local entries, so an included helper cannot silently
	// restore another account's credential provider after our cancellation.
	changed := false
	if !slices.Equal(snapshot.LocalAll("credential.helper"), wantHelpers) || !slices.Equal(EffectiveHelperChain(snapshot.All("credential.helper")), wantHelpers) {
		if err := git.ConfigUnsetAll(repoPath, "credential.helper"); err != nil {
			return fmt.Errorf("clearing credential.helper: %w", err)
		}
		changed = true
		for _, helper := range wantHelpers {
			if err := git.ConfigAdd(repoPath, "credential.helper", helper); err != nil {
				return fmt.Errorf("setting credential.helper: %w", err)
			}
		}
	}
	for key, value := range want {
		if snapshot.Get(key) == value && slices.Equal(snapshot.LocalAll(key), []string{value}) {
			continue
		}
		changed = true
		if err := git.ConfigUnsetAll(repoPath, key); err != nil {
			return fmt.Errorf("clearing %s: %w", key, err)
		}
		if err := git.ConfigAdd(repoPath, key, value); err != nil {
			return fmt.Errorf("setting %s: %w", key, err)
		}
	}
	for _, key := range remove {
		if len(snapshot.LocalAll(key)) > 0 {
			changed = true
			if err := git.ConfigUnsetAll(repoPath, key); err != nil {
				return fmt.Errorf("clearing %s: %w", key, err)
			}
		}
	}
	if changed {
		// Includes or worktree-specific config can follow the shared config and
		// override a successful write. Never fetch with a different helper chain
		// or username while reporting that account isolation was repaired.
		after, err := git.ReadConfigSnapshot(repoPath)
		if err != nil {
			return fmt.Errorf("validating credential config: %w", err)
		}
		if !slices.Equal(EffectiveHelperChain(after.All("credential.helper")), wantHelpers) {
			return fmt.Errorf("credential.helper is overridden by included or worktree config")
		}
		for key, value := range want {
			if after.Get(key) != value {
				return fmt.Errorf("%s is overridden by included or worktree config", key)
			}
		}
	}
	return nil
}

// RepoHelpers describes the helper reset and helper configured for a clone.
func RepoHelpers(accountKey, credType string, global config.GlobalConfig) []string {
	switch credType {
	case "token":
		return []string{"", StoreHelper(CredentialFilePath(accountKey))}
	case "gcm":
		if gcm := global.CredentialGCM; gcm != nil && gcm.Helper != "" {
			return []string{"", gcm.Helper}
		}
		return []string{"", "manager"}
	default:
		return []string{""}
	}
}

// EffectiveHelperChain retains the final reset and helpers Git tries after it.
func EffectiveHelperChain(helpers []string) []string {
	for i, helper := range slices.Backward(helpers) {
		if helper == "" {
			return helpers[i:]
		}
	}
	return helpers
}

func validateCredentialType(credType string) error {
	switch credType {
	case "token", "gcm", "ssh":
		return nil
	default:
		return fmt.Errorf("unknown credential type %q", credType)
	}
}

// StoreHelper returns a credential helper command with its file path quoted for
// Git's shell. Forward slashes also work with the shell bundled with Git for Windows.
func StoreHelper(filePath string) string {
	return "store --file '" + strings.ReplaceAll(filepath.ToSlash(filePath), "'", `'\''`) + "'"
}

// CredentialFilePath returns the path to the git-credential-store file for an account.
// Location: <configRoot>/gitbox/credentials/<accountKey>
// Respects XDG_CONFIG_HOME for test isolation.
func CredentialFilePath(accountKey string) string {
	return filepath.Join(config.ConfigRoot(), "gitbox", "credentials", accountKey)
}

// WriteCredentialFile writes a git-credential-store format file for token auth.
// Format: scheme://username:token@hostname (one line).
// Sets file permissions to 0600.
func WriteCredentialFile(filePath string, acct config.Account, accountKey string) error {
	// Resolve the token from env vars / the gitbox credential file.
	token, _, err := ResolveToken(acct, accountKey)
	if err != nil {
		return fmt.Errorf("resolving token for credential file: %w", err)
	}

	return writeCredentialFileAtomic(filePath, credentialLine(acct, token))
}

func credentialLine(acct config.Account, token string) []byte {
	hostname := extractHostname(acct.URL)
	scheme := "https"
	if u, err := url.Parse(acct.URL); err == nil && u.Scheme == "http" {
		scheme = "http"
	}
	username := acct.Username
	if acct.Provider == "gitlab" && username == "" {
		username = "oauth2"
	}
	return fmt.Appendf(nil, "%s://%s:%s@%s\n", scheme, url.PathEscape(username), url.PathEscape(token), hostname)
}

// extractHostname extracts the hostname from a URL string, stripping scheme and path.
func extractHostname(rawURL string) string {
	s := rawURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return s
}
