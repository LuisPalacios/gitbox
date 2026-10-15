package credential

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

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
	wantHelpers := []string{""}
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
		wantHelpers = append(wantHelpers, fmt.Sprintf("store --file %s", filepath.ToSlash(credFile)))
	case "gcm":
		helper := "manager"
		if globalCfg.CredentialGCM != nil {
			if globalCfg.CredentialGCM.Helper != "" {
				helper = globalCfg.CredentialGCM.Helper
			}
			if globalCfg.CredentialGCM.CredentialStore != "" {
				want["credential.credentialStore"] = globalCfg.CredentialGCM.CredentialStore
			}
		}
		wantHelpers = append(wantHelpers, helper)
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
	effective := snapshot.All("credential.helper")
	lastEmpty := -1
	for i, helper := range effective {
		if helper == "" {
			lastEmpty = i
		}
	}
	changed := false
	if !slices.Equal(snapshot.LocalAll("credential.helper"), wantHelpers) || lastEmpty < 0 || !slices.Equal(effective[lastEmpty:], wantHelpers) {
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
		helpers := after.All("credential.helper")
		lastEmpty := -1
		for i, helper := range helpers {
			if helper == "" {
				lastEmpty = i
			}
		}
		if lastEmpty < 0 || !slices.Equal(helpers[lastEmpty:], wantHelpers) {
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

func validateCredentialType(credType string) error {
	switch credType {
	case "token", "gcm", "ssh":
		return nil
	default:
		return fmt.Errorf("unknown credential type %q", credType)
	}
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

	// Serialize the compare and atomic replacement across fetch workers sharing
	// an account. Readers see either complete version, never a truncated file.
	credentialFileMu.Lock()
	defer credentialFileMu.Unlock()
	data, readErr := os.ReadFile(filePath)
	if readErr == nil && bytes.Equal(data, []byte(line)) {
		info, err := os.Stat(filePath)
		if err != nil {
			return err
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			return os.Chmod(filePath, 0o600)
		}
		return nil
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("reading credential file: %w", readErr)
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		return fmt.Errorf("creating credentials directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(filePath), ".credential-*")
	if err != nil {
		return fmt.Errorf("creating credential file: %w", err)
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(line); err != nil {
		_ = file.Close()
		return fmt.Errorf("writing credential file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing credential file: %w", err)
	}
	if err := os.Rename(file.Name(), filePath); err != nil {
		return fmt.Errorf("replacing credential file: %w", err)
	}
	return nil
}

var credentialFileMu sync.Mutex

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
