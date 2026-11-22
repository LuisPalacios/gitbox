//go:build windows

package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// ErrNeedElevation signals that the operation failed due to insufficient
// privileges and should be retried with UAC elevation.
var ErrNeedElevation = errors.New("administrator privileges required")

// replaceExecutable stages and verifies the update before moving the running binary.
func replaceExecutable(src, dst string, ops replaceOps) error {
	if ops.moveFile == nil {
		ops.moveFile = moveUpdateFile
	}
	err := replaceWindowsExecutable(src, dst, ops)
	if isPermissionError(err) && !errors.Is(err, errUpdateRollback) {
		return fmt.Errorf("%w: cannot write to %s", ErrNeedElevation, filepath.Dir(dst))
	}
	return err
}

// moveUpdateFile changes names in the install directory without replacing a
// file owned by another process and waits for Windows to finish the move.
func moveUpdateFile(src, dst string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}

// isPermissionError returns true for access-denied / privilege errors.
func isPermissionError(err error) bool {
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		return true
	}
	return false
}

// CleanupOldBinary removes the backup for the running version after startup.
func CleanupOldBinary() {
	self, err := SelfPath()
	if err == nil {
		cleanupWindowsBackup(self)
	}
}
