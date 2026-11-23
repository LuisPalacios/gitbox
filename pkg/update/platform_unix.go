//go:build !windows

package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// ErrNeedElevation signals that the operation failed due to insufficient
// privileges. On Unix this is defined for API compatibility; the updater
// does not attempt automatic elevation there.
var ErrNeedElevation = errors.New("administrator privileges required")

// errElevationUnsupported is what ApplyElevated returns on Unix. A package
// variable rather than an inline error keeps the caller's shared
// `if err != nil` from reading as always-true to static analysis.
var errElevationUnsupported = errors.New("elevated update not supported on this platform — run with sudo")

// ApplyElevated is not supported on Unix — return a clear message.
func ApplyElevated(archive, tag string, relaunch bool) error {
	return errElevationUnsupported
}

// RunElevatedUpdate is only available to Windows applications.
func RunElevatedUpdate(_ string, _ []string) error { return errElevationUnsupported }

// RestartAfterUpdate is only available to Windows applications.
func RestartAfterUpdate(_ []string) error { return errElevationUnsupported }

// replaceExecutable atomically replaces the binary at dst with src.
// On Unix, rename(2) is atomic within the same filesystem and the OS keeps
// the old inode open for the running process.
func replaceExecutable(src, dst string, ops replaceOps) error {
	// os.Rename fails across filesystem boundaries (e.g. /tmp → /usr/bin).
	// Copy to a temp file in the same directory first, then rename.
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".gitbox-update-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()

	srcFile, err := os.Open(src)
	if err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("opening source: %w", err)
	}

	srcInfo, err := srcFile.Stat()
	if err != nil {
		srcFile.Close()
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}

	if _, err := io.Copy(tmp, srcFile); err != nil {
		srcFile.Close()
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("copying binary: %w", err)
	}
	if err := errors.Join(srcFile.Close(), tmp.Sync(), tmp.Close()); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	// Preserve executable permissions.
	if err := os.Chmod(tmpPath, srcInfo.Mode()|0o111); err != nil {
		os.Remove(tmpPath)
		return err
	}

	defer os.Remove(tmpPath)
	return installPrevious(tmpPath, dst, ops)
}

func finishRecovery(r *Recovery, rollback bool) error         { return finishFiles(r, rollback, os.Rename) }
func removeReplaced(_ *Recovery, dir, _ string) (bool, error) { return false, os.RemoveAll(dir) }

func RunRecoveryHelper(_ []string) error { return errElevationUnsupported }

// Lock the install directory across prepare, replacement and cleanup. The
// kernel releases the lock even when the process exits during an update.
func lockInstall(dst string) (func(), error) {
	f, err := os.Open(filepath.Dir(dst))
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}

func publishRecord(tmp, path string) error { return os.Link(tmp, path) }

// CleanupOldBinary is the legacy Windows startup confirmation.
func CleanupOldBinary() {}

func canReplaceRunningFile(dst string) bool {
	info, err := os.Lstat(dst)
	return err == nil && info.Mode().IsRegular()
}
