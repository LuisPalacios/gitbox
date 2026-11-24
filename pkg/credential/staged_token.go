package credential

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func stagedTokenPath(id string) (string, error) {
	if decoded, err := hex.DecodeString(id); err != nil || len(decoded) != 16 {
		return "", errors.New("invalid staged credential identifier")
	}
	return filepath.Join(filepath.Dir(CredentialFilePath("placeholder")), ".transition-"+id), nil
}

// StoreTokenIfAbsent writes a new credential file without overwriting an
// existing one. The sibling temporary is already synced when it is linked.
func StoreTokenIfAbsent(key, token string, account config.Account) error {
	return writeCredentialFileWithReplace(CredentialFilePath(key), credentialLine(account, token), os.Link)
}

// StageToken prepares a verified PAT in the normal protected store format.
func StageToken(id, token string, account config.Account) error {
	path, err := stagedTokenPath(id)
	if err != nil {
		return err
	}
	return writeCredentialFileAtomic(path, credentialLine(account, token))
}

// ActivateStagedToken moves the verified candidate into the active path
// atomically. It returns false when no staged file remains.
func ActivateStagedToken(id, key string) (bool, error) {
	path, err := stagedTokenPath(id)
	if err != nil {
		return false, err
	}
	credentialFileMu.Lock()
	defer credentialFileMu.Unlock()
	if info, err := os.Lstat(path); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	} else if !info.Mode().IsRegular() {
		return false, errors.New("staged credential is not a regular file")
	}
	if err := os.Rename(path, CredentialFilePath(key)); err != nil {
		return false, fmt.Errorf("activating staged credential: %w", err)
	}
	return true, nil
}

func DiscardStagedToken(id string) error {
	path, err := stagedTokenPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); !os.IsNotExist(err) {
		return err
	}
	return nil
}
