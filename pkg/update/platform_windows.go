//go:build windows

package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// ErrNeedElevation signals that the operation failed due to insufficient
// privileges and should be retried with UAC elevation.
var ErrNeedElevation = errors.New("administrator privileges required")

// replaceExecutable replaces the binary at dst with src.
// On Windows, a running exe cannot be deleted but can be renamed.
// Strategy: rename dst → dst.old, then copy src → dst.
func replaceExecutable(src, dst string) error {
	oldPath := dst + ".old"

	// If dst exists, rename it out of the way.
	if _, err := os.Stat(dst); err == nil {
		// Remove any previous .old file first.
		os.Remove(oldPath)
		if err := os.Rename(dst, oldPath); err != nil {
			if isPermissionError(err) {
				return fmt.Errorf("%w: cannot write to %s", ErrNeedElevation, filepath.Dir(dst))
			}
			return fmt.Errorf("renaming %s to .old: %w", filepath.Base(dst), err)
		}
	}

	// Copy new binary to dst (can't use Rename across volumes).
	if err := copyFile(src, dst); err != nil {
		// Try to restore the old binary on failure.
		os.Rename(oldPath, dst)
		if isPermissionError(err) {
			return fmt.Errorf("%w: cannot write to %s", ErrNeedElevation, filepath.Dir(dst))
		}
		return fmt.Errorf("writing new %s: %w", filepath.Base(dst), err)
	}

	return nil
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

// ApplyElevated re-applies a previously extracted update using a UAC-elevated
// helper process. It writes a small batch script to a temp directory, launches
// it via ShellExecuteW with the "runas" verb (which triggers the UAC prompt),
// and returns immediately. The caller should quit the application right after.
//
// extractDir must contain the already-extracted binaries (same layout Apply
// would have processed). installDir is the target (e.g. C:\Program Files\gitbox).
func ApplyElevated(extractDir, installDir string) error {
	// Build xcopy lines for each entry.
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return fmt.Errorf("reading extracted files: %w", err)
	}

	var copyLines []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		src := filepath.Join(extractDir, e.Name())
		dst := filepath.Join(installDir, e.Name())
		// Use copy /Y (overwrite) for each file.
		copyLines = append(copyLines, fmt.Sprintf(`copy /Y "%s" "%s"`, src, dst))
	}
	if len(copyLines) == 0 {
		return fmt.Errorf("no files to install")
	}

	// Write the helper batch script.
	scriptPath := filepath.Join(extractDir, "gitbox-update.cmd")
	script := "@echo off\r\n"
	script += "title gitbox update\r\n"
	script += "echo Waiting for gitbox to exit...\r\n"
	script += "timeout /t 2 /nobreak >nul\r\n"
	for _, line := range copyLines {
		script += line + "\r\n"
	}
	script += "echo.\r\n"
	script += "echo Update complete. You can restart gitbox now.\r\n"
	script += "timeout /t 3\r\n"
	// Clean up the temp extraction directory (including this script).
	script += fmt.Sprintf("(rd /s /q \"%s\") 2>nul\r\n", extractDir)

	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		return fmt.Errorf("writing update script: %w", err)
	}

	// Launch elevated via ShellExecuteW "runas".
	return shellExecuteRunas(scriptPath)
}

func shellExecuteRunas(script string) error {
	shell32 := syscall.NewLazyDLL("shell32.dll")
	shellExecute := shell32.NewProc("ShellExecuteW")

	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(script)
	params, _ := syscall.UTF16PtrFromString("")
	dir, _ := syscall.UTF16PtrFromString(filepath.Dir(script))

	ret, _, _ := shellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(params)),
		uintptr(unsafe.Pointer(dir)),
		1, // SW_SHOWNORMAL
	)

	// ShellExecuteW returns >32 on success.
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW failed (code %d) — try running gitbox update from an admin terminal", ret)
	}
	return nil
}

// CleanupOldBinary removes .old files left from a previous update.
// Call this early at startup.
func CleanupOldBinary() {
	selfPath, err := os.Executable()
	if err != nil {
		return
	}
	selfPath, _ = filepath.EvalSymlinks(selfPath)
	dir := filepath.Dir(selfPath)

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".old") {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}
