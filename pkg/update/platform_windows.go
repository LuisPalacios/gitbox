//go:build windows

package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

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

func removeReplaced(r *Recovery, dir, exe string) (bool, error) {
	if os.RemoveAll(dir) == nil {
		return false, nil
	}
	old := filepath.Join(filepath.Dir(r.dst), r.state.Old)
	if err := moveUpdateFile(exe, old); err != nil {
		return false, err
	}
	_ = os.Remove(dir)
	state := updateState{Old: r.state.Old, OldSHA256: r.state.InstalledSHA256, InstalledSHA256: r.state.OldSHA256, Created: time.Now().Add(-retention)}
	out, err := os.CreateTemp(filepath.Dir(r.dst), ".gitbox-rollback-record-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(out.Name())
	writeErr := json.NewEncoder(out).Encode(state)
	if err := errors.Join(writeErr, out.Sync(), out.Close()); err != nil {
		return false, err
	}
	if err := os.Rename(out.Name(), r.dst+".update-state.json"); err != nil {
		return false, err
	}
	return true, nil
}

func lockInstall(dst string) (func(), error) {
	slot, err := os.OpenFile(dst+".new", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if err := slot.Close(); err != nil {
		_ = os.Remove(slot.Name())
		return nil, err
	}
	return func() { _ = os.Remove(slot.Name()) }, nil
}

func publishRecord(tmp, path string) error { return moveUpdateFile(tmp, path) }

// CleanupOldBinary confirms startup for callers of the legacy Windows API.
// The GUI uses BeginRecovery after installing its lifecycle handlers.
func CleanupOldBinary() {
	if exe, err := SelfPath(); err == nil {
		cleanupWindowsBackup(exe, true)
	}
}

func canReplaceRunningFile(dst string) bool { return false }
