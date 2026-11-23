package credential

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// WriteTempStoreFile writes token for acct into a git-credential-store file of
// its own, mode 0600 in a fresh 0700 directory, for one git command that must
// authenticate without the token on its command line (#242). The caller runs
// git with credential.helper=StoreHelper(path) and calls cleanup when git is
// done; cleanup removes the file and its directory and is safe to call twice.
func WriteTempStoreFile(acct config.Account, token string) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "gitbox-push-")
	if err != nil {
		return "", func() {}, fmt.Errorf("creating a temporary credentials directory: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	if err := os.Chmod(dir, 0o700); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("setting temporary credentials directory permissions: %w", err)
	}
	path = filepath.Join(dir, "credentials")
	if err := writeCredentialFileAtomic(path, credentialLine(acct, token)); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}
