package ops

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
)

// SSHSetup is what setting up an account's SSH key found and made: the
// Host alias of its ~/.ssh/config entry, the values of the account's SSH
// block, the public key to add on the provider and whether the provider
// already accepts it. The private key stays in its file.
type SSHSetup struct {
	Host      string
	Hostname  string
	KeyType   string
	PublicKey string
	AddURL    string
	Verified  bool
}

// SetupSSHCredential makes sure the account has its SSH key and Host entry
// in the SSH folder of cfg, then tests the connection to the provider. An
// existing key or entry is kept, so a second call only tests again (#244).
// The machine that runs it keeps the key: on a remote host, the host.
//
// It works on files and processes only; the caller saves the account's SSH
// block (ApplySSHSetup) and reconfigures its clones.
func SetupSSHCredential(ctx context.Context, cfg *config.Config, accountKey string) (SSHSetup, error) {
	acct, ok := cfg.Accounts[accountKey]
	if !ok {
		return SSHSetup{}, fmt.Errorf("account %q: %w", accountKey, config.ErrNotFound)
	}
	s := SSHSetup{
		Host:     credential.SSHHostAlias(accountKey),
		Hostname: HostnameFromURL(acct.URL),
		KeyType:  "ed25519",
	}
	if acct.SSH != nil && acct.SSH.KeyType != "" {
		s.KeyType = acct.SSH.KeyType
	}
	if acct.SSH != nil && acct.SSH.Hostname != "" {
		s.Hostname = acct.SSH.Hostname
	}

	sshFolder := credential.SSHFolder(cfg)
	if err := os.MkdirAll(sshFolder, 0o700); err != nil {
		return SSHSetup{}, fmt.Errorf("creating %s: %w", sshFolder, err)
	}
	keyPath := credential.SSHKeyPath(sshFolder, accountKey)
	if found, _ := credential.FindSSHConfigEntry(sshFolder, s.Host); !found {
		opts := credential.SSHConfigEntryOpts{
			Host:     s.Host,
			Hostname: s.Hostname,
			KeyFile:  keyPath,
			Username: acct.Username,
			Name:     acct.Name,
			Email:    acct.Email,
			URL:      acct.URL,
		}
		if err := credential.WriteSSHConfigEntry(sshFolder, opts); err != nil {
			return SSHSetup{}, fmt.Errorf("writing SSH config: %w", err)
		}
	}
	if _, err := os.Stat(keyPath); err != nil {
		if _, err := credential.GenerateSSHKey(sshFolder, accountKey, s.KeyType); err != nil {
			return SSHSetup{}, fmt.Errorf("generating SSH key: %w", err)
		}
	}

	s.PublicKey, _ = credential.ReadPublicKey(keyPath)
	s.AddURL = credential.SSHPublicKeyURL(acct.Provider, acct.URL)
	_, err := credential.TestSSHConnectionContext(ctx, sshFolder, s.Host)
	s.Verified = err == nil
	return s, nil
}

// RegenerateSSHCredential removes the account's key pair and Host entry and
// sets them up again: a new key the provider does not know yet.
func RegenerateSSHCredential(ctx context.Context, cfg *config.Config, accountKey string) (SSHSetup, error) {
	if _, ok := cfg.Accounts[accountKey]; !ok {
		return SSHSetup{}, fmt.Errorf("account %q: %w", accountKey, config.ErrNotFound)
	}
	if _, err := removeSSHKey(cfg, accountKey); err != nil {
		return SSHSetup{}, err
	}
	return SetupSSHCredential(ctx, cfg, accountKey)
}

// ApplySSHSetup points the account's SSH block at the setup and reports
// whether it changed. The block is replaced by a new value, never written
// through the pointer a copy of the account may share (#173).
func ApplySSHSetup(acct *config.Account, s SSHSetup) bool {
	if acct.SSH != nil && acct.SSH.Host == s.Host {
		return false
	}
	var ssh config.SSHConfig
	if acct.SSH != nil {
		ssh = *acct.SSH
	}
	ssh.Host = s.Host
	ssh.Hostname = s.Hostname
	ssh.KeyType = s.KeyType
	acct.SSH = &ssh
	return true
}

// removeSSHKey deletes the account's key pair and its Host entry from the
// SSH folder of cfg, and returns one message per thing removed. What is
// already gone is not an error.
func removeSSHKey(cfg *config.Config, accountKey string) ([]string, error) {
	sshFolder := credential.SSHFolder(cfg)
	hostAlias := credential.SSHHostAlias(accountKey)
	keyPath := credential.SSHKeyPath(sshFolder, accountKey)
	var msgs []string
	for _, f := range []struct{ path, msg string }{
		{keyPath, "Removed SSH key: " + keyPath},
		{keyPath + ".pub", "Removed SSH public key: " + keyPath + ".pub"},
	} {
		switch err := os.Remove(f.path); {
		case err == nil:
			msgs = append(msgs, f.msg)
		case !errors.Is(err, fs.ErrNotExist):
			return msgs, fmt.Errorf("removing the SSH key: %w", err)
		}
	}
	if found, _ := credential.FindSSHConfigEntry(sshFolder, hostAlias); found {
		if err := credential.RemoveSSHConfigEntry(sshFolder, hostAlias); err != nil {
			return msgs, fmt.Errorf("removing Host %s from the SSH config: %w", hostAlias, err)
		}
		msgs = append(msgs, fmt.Sprintf("Removed Host %s from ~/.ssh/config", hostAlias))
	}
	return msgs, nil
}
