package update

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const windowsBackupSuffixBytes = 8

var errInstalledUpdateChanged = errors.New("the installed executable changed before this update could be applied")

var errUpdateRollback = errors.New("could not restore the installed executable")

// windowsUpdateState identifies one backup and the exact binary that replaced it.
type windowsUpdateState struct {
	Old             string `json:"old"`
	OldSHA256       string `json:"oldSHA256"`
	InstalledSHA256 string `json:"installedSHA256"`
}

func replaceWindowsExecutable(src, dst string, ops replaceOps) error {
	return replaceWindowsExecutableWith(dst, "", ops, func(staged string) (string, error) {
		return stageUpdateExecutable(src, staged, ops.copyFile)
	})
}

func replaceWindowsExecutableWith(dst, expectedOld string, ops replaceOps, stage func(string) (string, error)) error {
	staged, old, marker := dst+".new", dst+".old", dst+".update-state.json"
	newHash, err := stage(staged)
	if err != nil {
		return fmt.Errorf("staging update: %w", err)
	}
	stagedMoved := false
	defer func() {
		if !stagedMoved {
			_ = os.Remove(staged)
		}
	}()

	oldHash, err := regularFileHash(dst)
	if err != nil {
		return fmt.Errorf("checking installed executable: %w", err)
	}
	if expectedOld != "" && oldHash != expectedOld {
		return errInstalledUpdateChanged
	}
	if oldHash == newHash {
		return nil
	}
	if expectedOld != "" {
		// The elevated transaction already holds the exclusive .new slot.
		removeRecordedWindowsBackup(dst, marker, oldHash)
	}

	if _, err := os.Lstat(old); err == nil {
		// A legacy updater may leave an unrecorded backup. Keep it and
		// let the checked move reserve a fresh sibling filename.
		var suffix [windowsBackupSuffixBytes]byte
		rand.Read(suffix[:])
		old += "." + hex.EncodeToString(suffix[:])
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking update backup: %w", err)
	}

	state := windowsUpdateState{
		Old: filepath.Base(old), OldSHA256: oldHash, InstalledSHA256: newHash,
	}
	if err := writeUpdateState(marker, state); err != nil {
		return fmt.Errorf("recording update backup: %w", err)
	}
	keepRecord := false
	defer func() {
		if !keepRecord {
			_ = os.Remove(marker)
		}
	}()

	if err := ops.moveFile(dst, old); err != nil {
		return fmt.Errorf("moving installed executable to its backup: %w", err)
	}
	if err := ops.moveFile(staged, dst); err != nil {
		if restoreErr := ops.moveFile(old, dst); restoreErr != nil {
			keepRecord = true
			return fmt.Errorf("installing update: %w. %w from %s: %w", err, errUpdateRollback, filepath.Base(old), restoreErr)
		}
		return fmt.Errorf("installing update: %w", err)
	}
	stagedMoved = true
	keepRecord = true
	return nil
}

// stageUpdateExecutable checks the extracted source and verifies the copy
// after flushing it to disk. The exclusive create also serializes installs
// into the same executable slot.
func stageUpdateExecutable(src, staged string, copyFile func(io.Writer, io.Reader) (int64, error)) (string, error) {
	from, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer from.Close()
	info, err := from.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("the update source is not a regular file")
	}
	expected, err := regularFileHash(src)
	if err != nil {
		return "", err
	}

	return stageExecutable(from, staged, info.Size(), info.Mode().Perm(), expected, copyFile)
}

// stageExecutable also accepts bytes from the signed archive in the elevated helper.
func stageExecutable(from io.Reader, staged string, size int64, mode os.FileMode, expected string, copyFile func(io.Writer, io.Reader) (int64, error)) (string, error) {
	out, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return "", err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(staged)
		}
	}()
	if copyFile == nil {
		copyFile = io.Copy
	}
	written, copyErr := copyFile(out, from)
	var syncErr error
	if copyErr == nil {
		syncErr = out.Sync()
	}
	if err := errors.Join(copyErr, syncErr, out.Close()); err != nil {
		return "", err
	}

	stagedInfo, err := os.Lstat(staged)
	if err != nil {
		return "", err
	}
	actual, err := regularFileHash(staged)
	if err != nil {
		return "", err
	}
	if written != size || stagedInfo.Size() != size || actual != expected {
		return "", errors.New("staged executable does not match the extracted update")
	}
	keep = true
	return expected, nil
}

func writeUpdateState(path string, state windowsUpdateState) error {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	encodeErr := json.NewEncoder(out).Encode(state)
	var syncErr error
	if encodeErr == nil {
		syncErr = out.Sync()
	}
	if err := errors.Join(encodeErr, syncErr, out.Close()); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// cleanupWindowsBackup accepts only a backup name derived from the running
// executable. The record must identify the current executable and its backup.
func cleanupWindowsBackup(exe string) {
	marker := exe + ".update-state.json"
	if _, err := os.Lstat(marker); err != nil {
		return
	}

	// Use the same exclusive slot as installation so concurrent startups
	// cannot remove a marker that a different update just wrote.
	slot, err := os.OpenFile(exe+".new", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return
	}
	defer os.Remove(slot.Name())
	if err := slot.Close(); err != nil {
		return
	}
	activeHash, err := regularFileHash(exe)
	if err == nil {
		removeRecordedWindowsBackup(exe, marker, activeHash)
	}
}

func removeRecordedWindowsBackup(exe, marker, activeHash string) {
	info, err := os.Lstat(marker)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		return
	}
	var state windowsUpdateState
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	canonical := filepath.Base(exe) + ".old"
	if state.Old != canonical {
		suffix, ok := strings.CutPrefix(state.Old, canonical+".")
		if !ok || len(suffix) != hex.EncodedLen(windowsBackupSuffixBytes) {
			return
		}
		if _, err := hex.DecodeString(suffix); err != nil {
			return
		}
	}
	old := filepath.Join(filepath.Dir(exe), state.Old)
	if activeHash != state.InstalledSHA256 {
		return
	}
	oldHash, err := regularFileHash(old)
	if !errors.Is(err, os.ErrNotExist) {
		if err != nil || oldHash != state.OldSHA256 {
			return
		}
		if err := os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
			return
		}
	}
	_ = os.Remove(marker)
}

func regularFileHash(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("expected a regular executable")
	}
	return fileHash(path)
}
