package credential

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var credentialFileMu sync.Mutex

// writeCredentialFileAtomic serializes writers and replaces a credential only
// after its complete contents reach a closed, synced temporary file. The
// temporary lives beside the destination; replacement uses the host's rename
// semantics and never removes the previous credential first.
func writeCredentialFileAtomic(path string, content []byte) error {
	return writeCredentialFileWithReplace(path, content, os.Rename)
}

func writeCredentialFileWithReplace(path string, content []byte, replace func(string, string) error) error {
	credentialFileMu.Lock()
	defer credentialFileMu.Unlock()
	data, readErr := os.ReadFile(path)
	if readErr == nil && bytes.Equal(data, content) {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			return os.Chmod(path, 0o600)
		}
		return nil
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("reading credential file: %w", readErr)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating credentials directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".credential-*")
	if err != nil {
		return fmt.Errorf("creating credential file: %w", err)
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("setting credential permissions: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("writing credential file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("syncing credential file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing credential file: %w", err)
	}
	if err := replace(file.Name(), path); err != nil {
		return fmt.Errorf("replacing credential file: %w", err)
	}
	return nil
}
