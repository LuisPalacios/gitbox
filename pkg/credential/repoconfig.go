package credential

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
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
	// Clear any existing credential.helper entries (multi-value key).
	_ = git.ConfigUnsetAll(repoPath, "credential.helper")

	// All types: add the empty helper line to cancel global/system helpers.
	if err := git.ConfigAdd(repoPath, "credential.helper", ""); err != nil {
		return fmt.Errorf("setting empty credential.helper: %w", err)
	}

	switch credType {
	case "token":
		return configureToken(repoPath, acct, accountKey)
	case "gcm":
		return configureGCM(repoPath, acct, globalCfg)
	case "ssh":
		// Empty helper cancellation is enough. Clean up leftover per-host config.
		return cleanupPerHostCredential(repoPath, acct)
	default:
		return fmt.Errorf("unknown credential type %q", credType)
	}
}

// configureToken sets up git's built-in credential-store helper pointing to a
// gitbox-managed credentials file.
func configureToken(repoPath string, acct config.Account, accountKey string) error {
	credFile := CredentialFilePath(accountKey)

	// Write the credential file with the token.
	if err := WriteCredentialFile(credFile, acct, accountKey); err != nil {
		return fmt.Errorf("writing credential file: %w", err)
	}

	// Use forward slashes for the path in git config (git requires this on all platforms).
	gitPath := filepath.ToSlash(credFile)
	helper := fmt.Sprintf("store --file %s", gitPath)
	if err := git.ConfigAdd(repoPath, "credential.helper", helper); err != nil {
		return fmt.Errorf("setting credential.helper for token: %w", err)
	}

	// Clean up any leftover per-host credential config from a previous type.
	return cleanupPerHostCredential(repoPath, acct)
}

// configureGCM sets up Git Credential Manager as the per-repo helper with
// all necessary config (credentialStore, per-host provider hints).
func configureGCM(repoPath string, acct config.Account, globalCfg config.GlobalConfig) error {
	// Set helper = manager.
	helper := "manager"
	if globalCfg.CredentialGCM != nil && globalCfg.CredentialGCM.Helper != "" {
		helper = globalCfg.CredentialGCM.Helper
	}
	if err := git.ConfigAdd(repoPath, "credential.helper", helper); err != nil {
		return fmt.Errorf("setting credential.helper for GCM: %w", err)
	}

	// Set credentialStore (wincredman / keychain / secretservice).
	if globalCfg.CredentialGCM != nil && globalCfg.CredentialGCM.CredentialStore != "" {
		_ = git.ConfigSet(repoPath, "credential.credentialStore", globalCfg.CredentialGCM.CredentialStore)
	}

	// Per-host credential config: username, provider, useHttpPath.
	host := strings.TrimSuffix(acct.URL, "/")
	section := fmt.Sprintf("credential.%s", host)

	_ = git.ConfigSet(repoPath, section+".username", acct.Username)

	if acct.GCM != nil && acct.GCM.Provider != "" {
		_ = git.ConfigSet(repoPath, section+".provider", acct.GCM.Provider)
	}
	_ = git.ConfigSet(repoPath, section+".useHttpPath", "false")

	return nil
}

// cleanupPerHostCredential removes per-host credential config that may be left
// over from a previous credential type (e.g. GCM → Token switch).
func cleanupPerHostCredential(repoPath string, acct config.Account) error {
	host := strings.TrimSuffix(acct.URL, "/")
	section := fmt.Sprintf("credential.%s", host)
	_ = git.ConfigUnset(repoPath, section+".username")
	_ = git.ConfigUnset(repoPath, section+".provider")
	_ = git.ConfigUnset(repoPath, section+".useHttpPath")
	_ = git.ConfigUnset(repoPath, "credential.credentialStore")
	return nil
}

// CredentialFilePath returns the path to the git-credential-store file for an account.
// Location: <configRoot>/gitbox/credentials/<accountKey>
// Respects XDG_CONFIG_HOME for test isolation.
func CredentialFilePath(accountKey string) string {
	return filepath.Join(config.ConfigRoot(), "gitbox", "credentials", accountKey)
}

// WriteCredentialFile writes a git-credential-store format file for token auth.
// Format: https://username:token@hostname (one line).
// Sets file permissions to 0600.
func WriteCredentialFile(filePath string, acct config.Account, accountKey string) error {
	// Resolve the token from env vars / OS keyring.
	token, _, err := ResolveToken(acct, accountKey)
	if err != nil {
		return fmt.Errorf("resolving token for credential file: %w", err)
	}

	// Build the credential line in git-credential-store format.
	hostname := extractHostname(acct.URL)
	username := acct.Username
	if acct.Provider == "gitlab" {
		username = "oauth2"
	}
	line := fmt.Sprintf("https://%s:%s@%s\n", url.PathEscape(username), url.PathEscape(token), hostname)

	// Ensure the directory exists.
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return fmt.Errorf("creating credentials directory: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(line), 0o600); err != nil {
		return fmt.Errorf("writing credential file: %w", err)
	}
	return nil
}

// RemoveCredentialFile removes the credential file for an account.
// Same as DeleteToken — both operate on the same file.
func RemoveCredentialFile(accountKey string) error {
	return DeleteToken(accountKey)
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
