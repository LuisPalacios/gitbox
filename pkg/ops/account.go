// Package ops holds gitbox's application-level operations: account
// lifecycle, credential switching, clone planning and discovery. Each
// operation works on a *config.Config plus the files it owns on disk
// (clones, SSH keys, credential stores).
//
// Operations never save the config and never lock: the caller serializes
// access to cfg and persists it afterwards. Operations that touch every
// clone of an account (ReconfigureClones) are separate so the caller can
// run them after a successful save.
package ops

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/heal"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// validAccountKey matches alphanumeric keys with inner hyphens.
var validAccountKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`)

// ValidateAccountKey reports whether key is usable as an account key.
func ValidateAccountKey(key string) error {
	if key == "" {
		return fmt.Errorf("account key cannot be empty")
	}
	if !validAccountKey.MatchString(key) {
		return fmt.Errorf("invalid key %q: use letters, numbers, and hyphens", key)
	}
	return nil
}

// AddAccount adds an account plus a matching source (same key) so repos can
// be added right away. An empty DefaultCredentialType becomes "gcm", and the
// credential sub-object for that type is filled in.
func AddAccount(cfg *config.Config, key string, acct config.Account) error {
	if acct.DefaultCredentialType == "" {
		acct.DefaultCredentialType = "gcm"
	}
	applyCredentialType(&acct, key, acct.DefaultCredentialType)

	if err := cfg.AddAccount(key, acct); err != nil {
		return err
	}
	src := config.Source{Account: key, Repos: make(map[string]config.Repo)}
	if err := cfg.AddSource(key, src); err != nil {
		_ = cfg.DeleteAccount(key)
		return err
	}
	return nil
}

// RenameAccount renames an account key and migrates everything tied to it:
// stored tokens, SSH key files and ~/.ssh/config entry, the source of the
// same key and its default on-disk folder, and every config reference.
// Run ReconfigureClones(cfg, newKey) after saving so existing clones pick up
// the new SSH alias and credential-store path.
func RenameAccount(cfg *config.Config, oldKey, newKey string) error {
	if oldKey == newKey {
		return nil
	}
	if err := ValidateAccountKey(newKey); err != nil {
		return err
	}
	acct, ok := cfg.Accounts[oldKey]
	if !ok {
		return fmt.Errorf("account %q not found", oldKey)
	}
	if _, exists := cfg.Accounts[newKey]; exists {
		return fmt.Errorf("account %q already exists", newKey)
	}

	// Any credential type may carry a stored token (token accounts as the
	// primary credential, SSH/GCM accounts as the companion API PAT).
	migrateStoredToken(oldKey, newKey)

	if acct.DefaultCredentialType == "ssh" {
		sshFolder := credential.SSHFolder(cfg)
		newAlias := credential.SSHHostAlias(newKey)
		oldKeyPath := credential.SSHKeyPath(sshFolder, oldKey)
		newKeyPath := credential.SSHKeyPath(sshFolder, newKey)

		// Missing key files are fine: the account may not be set up yet.
		_ = os.Rename(oldKeyPath, newKeyPath)
		_ = os.Rename(oldKeyPath+".pub", newKeyPath+".pub")

		_ = credential.RemoveSSHConfigEntry(sshFolder, credential.SSHHostAlias(oldKey))
		hostname := HostnameFromURL(acct.URL)
		if acct.SSH != nil && acct.SSH.Hostname != "" {
			hostname = acct.SSH.Hostname
		}
		_ = credential.WriteSSHConfigEntry(sshFolder, credential.SSHConfigEntryOpts{
			Host:     newAlias,
			Hostname: hostname,
			KeyFile:  newKeyPath,
			Username: acct.Username,
			Name:     acct.Name,
			Email:    acct.Email,
			URL:      acct.URL,
		})

		if acct.SSH != nil {
			acct.SSH.Host = newAlias
			cfg.Accounts[oldKey] = acct
		}
	}

	// Rename the same-key source and, when it uses the default folder (the
	// source key), its directory on disk.
	if src, srcExists := cfg.Sources[oldKey]; srcExists {
		if _, conflict := cfg.Sources[newKey]; !conflict {
			if src.Folder == "" {
				globalFolder := config.ExpandTilde(cfg.Global.Folder)
				_ = os.Rename(filepath.Join(globalFolder, oldKey), filepath.Join(globalFolder, newKey))
			}
			_ = cfg.RenameSource(oldKey, newKey)
		}
	}

	return cfg.RenameAccount(oldKey, newKey)
}

// DeleteAccount removes an account together with every clone folder and
// source directory it owns on disk, then cascades through every source,
// mirror and workspace member referencing it. Leaving a dangling mirror
// reference behind would corrupt the config (see issue #60).
func DeleteAccount(cfg *config.Config, accountKey string) error {
	if _, ok := cfg.Accounts[accountKey]; !ok {
		return fmt.Errorf("account %q not found", accountKey)
	}

	globalFolder := config.ExpandTilde(cfg.Global.Folder)
	for sourceKey, src := range cfg.Sources {
		if src.Account != accountKey {
			continue
		}
		sourceFolder := src.EffectiveFolder(sourceKey)
		for repoKey, repo := range src.Repos {
			path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
			if git.IsRepo(path) {
				_ = os.RemoveAll(path)
			}
		}
		_ = os.RemoveAll(filepath.Join(globalFolder, sourceFolder))
	}

	_, err := cfg.CascadeDeleteAccount(accountKey)
	return err
}

// DeleteRepo removes a repo from the config and deletes its clone folder.
func DeleteRepo(cfg *config.Config, sourceKey, repoKey string) error {
	src, ok := cfg.Sources[sourceKey]
	if !ok {
		return fmt.Errorf("source %q not found", sourceKey)
	}
	repo, ok := src.Repos[repoKey]
	if !ok {
		return fmt.Errorf("repo %q not found in source %q", repoKey, sourceKey)
	}

	globalFolder := config.ExpandTilde(cfg.Global.Folder)
	path := status.ResolveRepoPath(globalFolder, src.EffectiveFolder(sourceKey), repoKey, repo)
	if git.IsRepo(path) {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("deleting folder %s: %w", path, err)
		}
	}
	return cfg.DeleteRepo(sourceKey, repoKey)
}

// ClonedRepoPaths returns the on-disk path of every cloned repo that belongs
// to the account, keyed by "sourceKey/repoKey".
func ClonedRepoPaths(cfg *config.Config, accountKey string) map[string]string {
	paths := map[string]string{}
	globalFolder := config.ExpandTilde(cfg.Global.Folder)
	for sourceKey, src := range cfg.Sources {
		if src.Account != accountKey {
			continue
		}
		sourceFolder := src.EffectiveFolder(sourceKey)
		for repoKey, repo := range src.Repos {
			path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
			if git.IsRepo(path) {
				paths[sourceKey+"/"+repoKey] = path
			}
		}
	}
	return paths
}

// CountClonedRepos returns how many of the account's repos are cloned.
func CountClonedRepos(cfg *config.Config, accountKey string) int {
	return len(ClonedRepoPaths(cfg, accountKey))
}

// ReconfigureClones heals every cloned repo of the account so its identity,
// origin URL and credential config match the current account settings.
// Returns one report per clone that changed or warned.
func ReconfigureClones(cfg *config.Config, accountKey string) []heal.Report {
	var reports []heal.Report
	for _, sourceKey := range cfg.OrderedSourceKeys() {
		src := cfg.Sources[sourceKey]
		if src.Account != accountKey {
			continue
		}
		for _, repoKey := range src.OrderedRepoKeys() {
			if r := heal.Repo(cfg, sourceKey, repoKey); r.HasWork() {
				reports = append(reports, r)
			}
		}
	}
	return reports
}

// migrateStoredToken moves a stored token from oldKey to newKey.
func migrateStoredToken(oldKey, newKey string) {
	tok, err := credential.GetToken(oldKey)
	if err != nil || tok == "" {
		return
	}
	if err := credential.StoreToken(newKey, tok); err != nil {
		return
	}
	_ = credential.DeleteToken(oldKey)
}

// HostnameFromURL extracts the hostname from a provider URL. A value without
// a parseable host is returned with any http(s) scheme stripped.
func HostnameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(rawURL, prefix) {
			return strings.TrimPrefix(rawURL, prefix)
		}
	}
	return rawURL
}
