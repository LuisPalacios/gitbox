package ops

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/git"
)

// GCMProviderFor maps a gitbox provider to the provider name Git Credential
// Manager expects.
func GCMProviderFor(prov string) string {
	switch prov {
	case "github", "gitlab", "bitbucket":
		return prov
	default:
		return "generic"
	}
}

// applyCredentialType sets the account's credential type and replaces its
// credential sub-objects with the defaults for that type.
func applyCredentialType(acct *config.Account, accountKey, credType string) {
	acct.DefaultCredentialType = credType
	acct.GCM = nil
	acct.SSH = nil
	switch credType {
	case "gcm":
		acct.GCM = &config.GCMConfig{Provider: GCMProviderFor(acct.Provider)}
	case "ssh":
		acct.SSH = &config.SSHConfig{
			Host:     credential.SSHHostAlias(accountKey),
			Hostname: HostnameFromURL(acct.URL),
			KeyType:  "ed25519",
		}
	}
}

// RemoveCredentialArtifacts deletes the OS-level artifacts of the account's
// current credential type: stored tokens and credential-store files, cached
// GCM credentials, SSH key pairs and their ~/.ssh/config entry, plus any
// companion PAT. The config is left unchanged. Returns one message per
// artifact removed.
func RemoveCredentialArtifacts(cfg *config.Config, accountKey string) []string {
	acct := cfg.Accounts[accountKey]

	var msgs []string
	switch acct.DefaultCredentialType {
	case "token":
		if removeStoredToken(accountKey) {
			msgs = append(msgs, "Token and credential store file removed")
		}
	case "gcm":
		host := HostnameFromURL(acct.URL)
		if err := rejectGCMCredential(host, acct.Username); err == nil {
			msgs = append(msgs, fmt.Sprintf("GCM credential removed for %s@%s", acct.Username, host))
		}
		if removeStoredToken(accountKey) {
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
		if removeStoredToken(accountKey) {
			msgs = append(msgs, "Removed companion PAT from credential file")
		}
	}
	return msgs
}

// removeStoredToken deletes the account's credential file and reports
// whether there was one to delete.
func removeStoredToken(accountKey string) bool {
	if _, err := os.Stat(credential.CredentialFilePath(accountKey)); err != nil {
		return false
	}
	return credential.DeleteToken(accountKey) == nil
}

// rejectGCMCredential asks git's credential helpers to forget the cached
// credential for username@host.
func rejectGCMCredential(host, username string) error {
	input := fmt.Sprintf("protocol=https\nhost=%s\nusername=%s\n", host, username)
	cmd := exec.Command(git.GitBin(), "credential", "reject")
	// Run from home to avoid repo-local .git/config credential overrides.
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	cmd.Env = git.Environ()
	git.HideWindow(cmd)
	cmd.Stdin = strings.NewReader(input)
	return cmd.Run()
}

// ChangeCredentialType removes the account's current credential artifacts
// and switches it to newType with default settings. Run ReconfigureClones
// after saving so existing clones follow the new credential type.
func ChangeCredentialType(cfg *config.Config, accountKey, newType string) error {
	acct, ok := cfg.Accounts[accountKey]
	if !ok {
		return fmt.Errorf("account %q not found", accountKey)
	}
	switch newType {
	case "gcm", "ssh", "token":
	default:
		return fmt.Errorf("unknown credential type %q", newType)
	}

	RemoveCredentialArtifacts(cfg, accountKey)
	applyCredentialType(&acct, accountKey, newType)
	return cfg.UpdateAccount(accountKey, acct)
}

// DeleteCredential removes the account's credential artifacts and clears its
// credential configuration so it can be set up from scratch. Returns
// messages describing what was removed and what the user still has to do.
func DeleteCredential(cfg *config.Config, accountKey string) ([]string, error) {
	acct, ok := cfg.Accounts[accountKey]
	if !ok {
		return nil, fmt.Errorf("account %q not found", accountKey)
	}
	if acct.DefaultCredentialType == "" {
		return []string{"No credential configured"}, nil
	}

	msgs := RemoveCredentialArtifacts(cfg, accountKey)
	if acct.DefaultCredentialType == "ssh" {
		msgs = append(msgs, fmt.Sprintf("Remember to remove the SSH public key from your provider:\n  %s",
			credential.SSHPublicKeyURL(acct.Provider, acct.URL)))
	}

	acct.DefaultCredentialType = ""
	acct.GCM = nil
	acct.SSH = nil
	if err := cfg.UpdateAccount(accountKey, acct); err != nil {
		return msgs, err
	}

	if n := CountClonedRepos(cfg, accountKey); n > 0 {
		msgs = append(msgs, fmt.Sprintf("%d clone(s) will be reconfigured when a new credential is set up", n))
	}
	return msgs, nil
}
