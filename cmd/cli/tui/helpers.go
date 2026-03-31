package tui

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/status"
)

// cloneURL builds the clone URL for a repo based on credential type.
func cloneURL(acct config.Account, repoKey string, credType string) string {
	switch credType {
	case "ssh":
		host := acct.URL
		if acct.SSH != nil && acct.SSH.Host != "" {
			host = acct.SSH.Host
		} else {
			host = stripScheme(acct.URL)
		}
		return fmt.Sprintf("git@%s:%s.git", host, repoKey)
	default:
		u, err := url.Parse(acct.URL)
		if err == nil && acct.Username != "" {
			u.User = url.User(acct.Username)
			return fmt.Sprintf("%s/%s.git", u.String(), repoKey)
		}
		return fmt.Sprintf("%s/%s.git", acct.URL, repoKey)
	}
}

// stripScheme removes https:// or http:// from a URL.
func stripScheme(rawURL string) string {
	for _, prefix := range []string{"https://", "http://"} {
		if len(rawURL) > len(prefix) && rawURL[:len(prefix)] == prefix {
			return rawURL[len(prefix):]
		}
	}
	return rawURL
}

// removeCredentialArtifacts cleans up OS-level credential artifacts for an
// account's current credential type. Returns informational messages.
func removeCredentialArtifacts(cfg *config.Config, accountKey string) []string {
	acct := cfg.Accounts[accountKey]

	var msgs []string
	switch acct.DefaultCredentialType {
	case "token":
		if err := credential.DeleteToken(accountKey); err == nil {
			msgs = append(msgs, "Token removed from credential file")
		}
		if err := credential.RemoveCredentialFile(accountKey); err == nil {
			msgs = append(msgs, "Credential store file removed")
		}
	case "gcm":
		host := hostnameFromURL(acct.URL)
		input := fmt.Sprintf("protocol=https\nhost=%s\nusername=%s\n", host, acct.Username)
		homeDir, _ := os.UserHomeDir()
		cmd := exec.Command(git.GitBin(), "credential", "reject")
		cmd.Dir = homeDir // Avoid repo-local .git/config credential overrides.
		cmd.Env = git.Environ()
		cmd.Stdin = strings.NewReader(input)
		if err := cmd.Run(); err == nil {
			msgs = append(msgs, fmt.Sprintf("GCM credential removed for %s@%s", acct.Username, host))
		}
		if err := credential.DeleteToken(accountKey); err == nil {
			msgs = append(msgs, "Removed companion PAT from credential file")
		}
	case "ssh":
		sshFolder := credential.SSHFolder(cfg)
		hostAlias := credential.SSHHostAlias(accountKey)
		keyPath := credential.SSHKeyPath(sshFolder, accountKey)
		if err := os.Remove(keyPath); err == nil {
			msgs = append(msgs, fmt.Sprintf("Removed SSH key: %s", keyPath))
		}
		if err := os.Remove(keyPath + ".pub"); err == nil {
			msgs = append(msgs, fmt.Sprintf("Removed SSH public key: %s.pub", keyPath))
		}
		if err := credential.RemoveSSHConfigEntry(sshFolder, hostAlias); err == nil {
			msgs = append(msgs, fmt.Sprintf("Removed Host %s from ~/.ssh/config", hostAlias))
		}
		if err := credential.DeleteToken(accountKey); err == nil {
			msgs = append(msgs, "Removed discovery PAT from credential file")
		}
	}
	return msgs
}

// reconfigureClones updates remote URLs and credential config of every cloned
// repo belonging to the given account. Returns number of repos updated.
func reconfigureClones(cfg *config.Config, accountKey string) int {
	acct, ok := cfg.Accounts[accountKey]
	if !ok {
		return 0
	}
	globalFolder := config.ExpandTilde(cfg.Global.Folder)
	count := 0
	for sourceKey, src := range cfg.Sources {
		if src.Account != accountKey {
			continue
		}
		sourceFolder := src.EffectiveFolder(sourceKey)
		for repoKey, repo := range src.Repos {
			path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
			if !git.IsRepo(path) {
				continue
			}
			credType := repo.EffectiveCredentialType(&acct)
			newURL := cloneURL(acct, repoKey, credType)
			_ = git.SetRemoteURL(path, "origin", newURL)
			_ = credential.ConfigureRepoCredential(path, acct, accountKey, credType, cfg.Global)
			count++
		}
	}
	return count
}

// migrateKeyringToken moves a token from oldKey to newKey in the credential file.
func migrateKeyringToken(oldKey, newKey string) {
	tok, err := credential.GetToken(oldKey)
	if err != nil || tok == "" {
		return
	}
	if err := credential.StoreToken(newKey, tok); err != nil {
		return
	}
	_ = credential.DeleteToken(oldKey)
}

// countClonedRepos returns the number of locally cloned repos for an account.
func countClonedRepos(cfg *config.Config, accountKey string) int {
	globalFolder := config.ExpandTilde(cfg.Global.Folder)
	count := 0
	for sourceKey, src := range cfg.Sources {
		if src.Account != accountKey {
			continue
		}
		sourceFolder := src.EffectiveFolder(sourceKey)
		for repoKey, repo := range src.Repos {
			path := status.ResolveRepoPath(globalFolder, sourceFolder, repoKey, repo)
			if git.IsRepo(path) {
				count++
			}
		}
	}
	return count
}


// renameAccount performs a full account rename including credential migration,
// source folder rename, and config update. Returns an error or nil.
func renameAccount(cfg *config.Config, cfgPath, oldKey, newKey string) error {
	if newKey == "" {
		return fmt.Errorf("new key cannot be empty")
	}
	if oldKey == newKey {
		return nil
	}
	if !validAccountKey.MatchString(newKey) {
		return fmt.Errorf("invalid key %q: use lowercase letters, numbers, and hyphens", newKey)
	}

	acct, ok := cfg.Accounts[oldKey]
	if !ok {
		return fmt.Errorf("account %q not found", oldKey)
	}
	if _, exists := cfg.Accounts[newKey]; exists {
		return fmt.Errorf("account %q already exists", newKey)
	}

	// Credential migration.
	switch acct.DefaultCredentialType {
	case "token":
		migrateKeyringToken(oldKey, newKey)

	case "ssh":
		sshFolder := credential.SSHFolder(cfg)
		oldAlias := credential.SSHHostAlias(oldKey)
		newAlias := credential.SSHHostAlias(newKey)
		oldKeyPath := credential.SSHKeyPath(sshFolder, oldKey)
		newKeyPath := credential.SSHKeyPath(sshFolder, newKey)

		_ = os.Rename(oldKeyPath, newKeyPath)
		_ = os.Rename(oldKeyPath+".pub", newKeyPath+".pub")

		_ = credential.RemoveSSHConfigEntry(sshFolder, oldAlias)
		hostname := hostnameFromURL(acct.URL)
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
		}
		migrateKeyringToken(oldKey, newKey)

	case "gcm":
		migrateKeyringToken(oldKey, newKey)
	}

	// Source key + folder rename.
	if _, srcExists := cfg.Sources[oldKey]; srcExists {
		if _, conflict := cfg.Sources[newKey]; !conflict {
			src := cfg.Sources[oldKey]
			if src.Folder == "" {
				globalFolder := config.ExpandTilde(cfg.Global.Folder)
				oldPath := filepath.Join(globalFolder, oldKey)
				newPath := filepath.Join(globalFolder, newKey)
				_ = os.Rename(oldPath, newPath)
			}
			_ = cfg.RenameSource(oldKey, newKey)
		}
	}

	// Update SSH host in the account before renaming.
	if acct.SSH != nil {
		cfg.Accounts[oldKey] = acct
	}
	if err := cfg.RenameAccount(oldKey, newKey); err != nil {
		return err
	}

	return config.Save(cfg, cfgPath)
}

// changeCredentialType switches an account to a different credential type,
// cleaning up old artifacts and reconfiguring clones.
func changeCredentialType(cfg *config.Config, cfgPath, accountKey, newType string) ([]string, error) {
	msgs := removeCredentialArtifacts(cfg, accountKey)

	acct, ok := cfg.Accounts[accountKey]
	if !ok {
		return msgs, fmt.Errorf("account %q not found", accountKey)
	}

	acct.DefaultCredentialType = newType
	acct.GCM = nil
	acct.SSH = nil

	switch newType {
	case "gcm":
		acct.GCM = &config.GCMConfig{
			Provider:    inferGCMProvider(acct.Provider),
			UseHTTPPath: false,
		}
	case "ssh":
		acct.SSH = &config.SSHConfig{
			Host:     credential.SSHHostAlias(accountKey),
			Hostname: hostnameFromURL(acct.URL),
			KeyType:  "ed25519",
		}
	}

	if err := cfg.UpdateAccount(accountKey, acct); err != nil {
		return msgs, err
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		return msgs, err
	}

	n := reconfigureClones(cfg, accountKey)
	if n > 0 {
		msgs = append(msgs, fmt.Sprintf("%d clone(s) reconfigured", n))
	}
	return msgs, nil
}

// deleteCredential removes all credential artifacts and clears the credential
// config for an account. Returns informational messages.
func deleteCredential(cfg *config.Config, cfgPath, accountKey string) ([]string, error) {
	acct, ok := cfg.Accounts[accountKey]
	if !ok {
		return nil, fmt.Errorf("account %q not found", accountKey)
	}
	if acct.DefaultCredentialType == "" {
		return []string{"No credential configured"}, nil
	}

	msgs := removeCredentialArtifacts(cfg, accountKey)

	// For SSH, remind the user to remove the public key from the provider.
	if acct.DefaultCredentialType == "ssh" {
		msgs = append(msgs, fmt.Sprintf("Remember to remove the SSH public key from your provider:\n  %s",
			credential.SSHPublicKeyURL(acct.Provider, acct.URL)))
	}

	nClones := countClonedRepos(cfg, accountKey)

	acct.DefaultCredentialType = ""
	acct.GCM = nil
	acct.SSH = nil
	if err := cfg.UpdateAccount(accountKey, acct); err != nil {
		return msgs, err
	}
	if err := config.Save(cfg, cfgPath); err != nil {
		return msgs, err
	}

	if nClones > 0 {
		msgs = append(msgs, fmt.Sprintf("%d clone(s) will be reconfigured when a new credential is set up", nClones))
	}
	return msgs, nil
}

// regenerateSSH deletes old SSH keys and config, then returns messages.
// The caller should navigate to credential setup screen after this.
func regenerateSSH(cfg *config.Config, accountKey string) []string {
	sshFolder := credential.SSHFolder(cfg)
	hostAlias := credential.SSHHostAlias(accountKey)
	keyPath := credential.SSHKeyPath(sshFolder, accountKey)

	var msgs []string
	_ = os.Remove(keyPath)
	_ = os.Remove(keyPath + ".pub")
	if err := credential.RemoveSSHConfigEntry(sshFolder, hostAlias); err == nil {
		msgs = append(msgs, fmt.Sprintf("Removed old Host %s from ~/.ssh/config", hostAlias))
	}
	msgs = append(msgs, "Old SSH keys removed. Set up new credential now.")
	return msgs
}
