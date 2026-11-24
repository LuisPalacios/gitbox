package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/pathkey"
)

type renameArtifacts struct {
	managedSSH bool
}

func RenameAccount(ctx context.Context, cfg *config.Config, oldKey, newKey string, owner TransitionOwner) (StepReport, error) {
	report := StepReport{Steps: []Step{}}
	if err := ValidateAccountKey(oldKey); err != nil {
		return report, err
	}
	if err := ValidateAccountKey(newKey); err != nil {
		return report, err
	}
	if oldKey == newKey {
		report.OK = true
		return report, nil
	}
	acct, oldExists := cfg.Accounts[oldKey]
	if current, exists := cfg.Accounts[newKey]; exists {
		if !oldExists && current.CredentialTransition != nil && current.CredentialTransition.PreviousKey == oldKey {
			return finishCredentialTransition(ctx, cfg, newKey, owner, report), nil
		}
		return report, config.ErrExists
	}
	if !oldExists {
		return report, config.ErrNotFound
	}
	if pending := acct.CredentialTransition; pending != nil {
		if pending.RenameTo != newKey || pending.PreviousKey != "" || pending.PreviousType != "" ||
			pending.ManagedSSH || pending.StagedID != "" {
			return report, ErrCredentialTransitionPending
		}
	} else {
		if reservedCredentialKey(cfg, newKey) {
			return report, ErrCredentialTransitionPending
		}
		if err := checkRenameEnvironment(oldKey, newKey); err != nil {
			report.record(StepPrepare, err)
			return report, nil
		}

		if !report.record(StepPrepare, inspectCredentialDestination(cfg, oldKey, newKey)) {
			return report, nil
		}
		reserved := acct
		reserved.CredentialTransition = &config.CredentialTransition{RenameTo: newKey}
		saved, err := owner.Commit(func(current *config.Config) error {
			if !sameTransitionAccount(current, oldKey, acct) || reservedCredentialKey(current, newKey) {
				return ErrCredentialTransitionPending
			}
			if _, exists := current.Accounts[newKey]; exists {
				return config.ErrExists
			}
			return current.UpdateAccount(oldKey, reserved)
		})
		if !report.record(StepReserve, err) {
			return report, nil
		}
		cfg, acct = saved, reserved
	}

	if err := checkRenameEnvironment(oldKey, newKey); err != nil {
		report.record(StepPrepare, err)
		return report, nil
	}

	artifacts, err := prepareRename(cfg, oldKey, newKey, acct)
	if !report.record(StepPrepare, err) {
		return report, nil
	}
	next := acct
	if artifacts.managedSSH {
		ssh := *acct.SSH
		ssh.Host = credential.SSHHostAlias(newKey)
		next.SSH = &ssh
	}
	next.CredentialTransition = &config.CredentialTransition{PreviousKey: oldKey, ManagedSSH: artifacts.managedSSH}
	saved, err := owner.Commit(func(current *config.Config) error {
		if !sameTransitionAccount(current, oldKey, acct) {
			return ErrCredentialTransitionPending
		}
		if err := current.RenameAccount(oldKey, newKey); err != nil {
			return err
		}
		// A source's implicit folder is its key. Pin the original folder
		// before changing the key so every repo keeps its physical path.
		if src, exists := current.Sources[oldKey]; exists && src.Account == newKey {
			if _, occupied := current.Sources[newKey]; !occupied {
				src.Folder = src.EffectiveFolder(oldKey)
				if err := current.UpdateSource(oldKey, src); err != nil {
					return err
				}
				if err := current.RenameSource(oldKey, newKey); err != nil {
					return err
				}
			}
		}
		return current.UpdateAccount(newKey, next)
	})
	if !report.record(StepSave, err) {
		return report, nil
	}
	return finishCredentialTransition(ctx, saved, newKey, owner, report), nil
}

// reservedCredentialKey keeps an account from inheriting artifacts that an
// unfinished rename still uses.
func reservedCredentialKey(cfg *config.Config, key string) bool {
	for _, acct := range cfg.Accounts {
		if pending := acct.CredentialTransition; pending != nil &&
			(pending.PreviousKey != "" && strings.EqualFold(pending.PreviousKey, key) ||
				pending.RenameTo != "" && strings.EqualFold(pending.RenameTo, key)) {
			return true
		}
	}
	return false
}

// Check for unrelated files before recording intent. Retries allow matching
// files prepared by the reserved rename and recheck before any overwrite.
func inspectCredentialDestination(cfg *config.Config, oldKey, newKey string) error {
	for other := range cfg.Accounts {
		if other != oldKey && strings.EqualFold(other, newKey) {
			return config.ErrExists
		}
	}
	sshFolder := credential.SSHFolder(cfg)
	oldSSH, newSSH := credential.SSHKeyPath(sshFolder, oldKey), credential.SSHKeyPath(sshFolder, newKey)
	for _, pair := range [][2]string{
		{credential.CredentialFilePath(oldKey), credential.CredentialFilePath(newKey)},
		{oldSSH, newSSH},
		{oldSSH + ".pub", newSSH + ".pub"},
	} {
		if pathkey.Compare(pair[0]) == pathkey.Compare(pair[1]) && strings.EqualFold(oldKey, newKey) {
			continue
		}
		if _, err := os.Lstat(pair[1]); !os.IsNotExist(err) {
			return errors.New("destination credential already exists or is inaccessible")
		}
	}
	if !strings.EqualFold(oldKey, newKey) {
		found, err := credential.FindSSHConfigEntry(sshFolder, credential.SSHHostAlias(newKey))
		if found || err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("destination SSH alias already exists or is inaccessible")
		}
	}
	return nil
}

func prepareRename(cfg *config.Config, oldKey, newKey string, acct config.Account) (renameArtifacts, error) {
	var artifacts renameArtifacts
	if err := migrateStoredToken(oldKey, newKey, acct); err != nil {
		return artifacts, err
	}
	if acct.SSH == nil || acct.SSH.Host != credential.SSHHostAlias(oldKey) {
		return artifacts, nil // Custom and shared identities stay where they are.
	}
	for key, other := range cfg.Accounts {
		if key != oldKey && other.SSH != nil && strings.EqualFold(other.SSH.Host, acct.SSH.Host) {
			return artifacts, nil
		}
	}
	folder := credential.SSHFolder(cfg)
	oldPath := credential.SSHKeyPath(folder, oldKey)
	newPath := credential.SSHKeyPath(folder, newKey)
	if strings.EqualFold(oldKey, newKey) && pathkey.Compare(oldPath) == pathkey.Compare(newPath) {
		return artifacts, nil // Case-only rename on a case-insensitive volume.
	}
	found, err := credential.FindSSHConfigEntry(folder, acct.SSH.Host)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return artifacts, err
	}
	if found {
		configured, err := credential.FindSSHKey(folder, acct.SSH.Host, acct.SSH.KeyType)
		if err != nil {
			return artifacts, err
		}
		if pathkey.Compare(configured) != pathkey.Compare(oldPath) {
			return artifacts, nil
		}
	} else if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return artifacts, nil // The account was never set up.
	} else if err != nil {
		return artifacts, err
	}
	for _, suffix := range []string{"", ".pub"} {
		if err := copyCredentialFile(oldPath+suffix, newPath+suffix); err != nil {
			return artifacts, err
		}
	}
	newAlias := credential.SSHHostAlias(newKey)
	found, err = credential.FindSSHConfigEntry(folder, newAlias)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return artifacts, err
	}
	if found {
		configured, err := credential.FindSSHKey(folder, newAlias, acct.SSH.KeyType)
		if err != nil || pathkey.Compare(configured) != pathkey.Compare(newPath) {
			return artifacts, errors.New("destination SSH alias already exists")
		}
	} else {
		hostname := acct.SSH.Hostname
		if hostname == "" {
			hostname = HostnameFromURL(acct.URL)
		}
		if err := credential.WriteSSHConfigEntry(folder, credential.SSHConfigEntryOpts{
			Host: newAlias, Hostname: hostname, KeyFile: newPath, Username: acct.Username,
			Name: acct.Name, Email: acct.Email, URL: acct.URL,
		}); err != nil {
			return artifacts, err
		}
	}
	artifacts.managedSSH = true
	return artifacts, nil
}

func checkRenameEnvironment(oldKey, newKey string) error {
	old, _, oldEnv := credential.EnvironmentToken(oldKey)
	new, _, newEnv := credential.EnvironmentToken(newKey)
	if !oldEnv && !newEnv {
		return nil
	}
	if !oldEnv {
		var err error
		old, err = credential.GetToken(oldKey)
		if err != nil {
			return err
		}
	}
	if !newEnv || old != new {
		return errors.New("the new account key resolves to a different environment token")
	}
	return nil
}

func migrateStoredToken(oldKey, newKey string, acct config.Account) error {
	oldPath := credential.CredentialFilePath(oldKey)
	newPath := credential.CredentialFilePath(newKey)
	if strings.EqualFold(oldKey, newKey) && pathkey.Compare(oldPath) == pathkey.Compare(newPath) {
		return nil
	}
	tok, err := credential.GetToken(oldKey)
	existing, destErr := credential.GetToken(newKey)
	if destErr == nil && (err != nil || tok != existing) || destErr != nil && !errors.Is(destErr, os.ErrNotExist) {
		return errors.New("destination PAT already exists or is inaccessible")
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if destErr == nil {
		return nil
	}
	return credential.StoreTokenIfAbsent(newKey, tok, acct)
}

// Copy only an ordinary file. Exclusive creation prevents overwriting a
// credential that somebody else has placed at the destination.
func copyCredentialFile(from, to string) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("credential is not a regular file")
	}
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		return compareCredentialFiles(from, to)
	}
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return errors.Join(err, os.Remove(to))
	}
	return nil
}

func compareCredentialFiles(oldPath, newPath string) error {
	for _, path := range []string{oldPath, newPath} {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("credential is not a regular file")
		}
	}
	old, err := os.ReadFile(oldPath)
	if err != nil {
		return err
	}
	new, err := os.ReadFile(newPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(old, new) {
		return errors.New("destination credential already exists")
	}
	return nil
}

func retireRenamedCredentials(cfg *config.Config, key string, pending *config.CredentialTransition) error {
	old := pending.PreviousKey
	if err := ValidateAccountKey(old); err != nil {
		return err
	}
	if _, exists := cfg.Accounts[old]; exists {
		return ErrCredentialTransitionPending
	}
	if pathkey.Compare(credential.CredentialFilePath(old)) != pathkey.Compare(credential.CredentialFilePath(key)) {
		before, err := credential.GetToken(old)
		if err == nil {
			after, err := credential.GetToken(key)
			if err != nil || before != after {
				return fmt.Errorf("old and new PAT no longer match")
			}
			if err := credential.DeleteToken(old); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if pending.ManagedSSH {
		folder := credential.SSHFolder(cfg)
		for account, acct := range cfg.Accounts {
			if account != key && acct.SSH != nil && strings.EqualFold(acct.SSH.Host, credential.SSHHostAlias(old)) {
				return nil
			}
		}
		oldPath, newPath := credential.SSHKeyPath(folder, old), credential.SSHKeyPath(folder, key)
		if pathkey.Compare(oldPath) != pathkey.Compare(newPath) {
			for _, suffix := range []string{"", ".pub"} {
				if _, err := os.Lstat(oldPath + suffix); os.IsNotExist(err) {
					continue
				} else if err != nil {
					return err
				}
				if err := compareCredentialFiles(oldPath+suffix, newPath+suffix); err != nil {
					return err
				}
			}
			_, err := removeSSHKey(cfg, old)
			return err
		}
	}
	return nil
}
