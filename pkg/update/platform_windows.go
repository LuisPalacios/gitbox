//go:build windows

package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

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

// ApplyElevated re-applies a previously extracted update using a UAC-elevated
// helper process. It writes a small batch script to a temp directory, launches
// it via ShellExecuteW with the "runas" verb (which triggers the UAC prompt),
// and returns immediately. The caller should quit the application right after.
//
// extractDir must contain the already-extracted binaries (same layout Apply
// would have processed). installDir is the target (e.g. C:\Program Files\gitbox).
// relaunch, when not empty, is the app the script starts once the files are
// copied.
func ApplyElevated(extractDir, installDir, relaunch string) error {
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
	script := elevatedScript(copyLines, extractDir, relaunch)

	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		return fmt.Errorf("writing update script: %w", err)
	}

	// Launch elevated via ShellExecuteW "runas".
	return shellExecuteRunas(scriptPath)
}

// elevatedScript returns the batch script ApplyElevated runs elevated. It
// waits for gitbox to exit, copies the files and, when relaunch is set,
// starts the new version through explorer.exe: a program explorer starts
// runs as the signed-in user, so the new GitboxApp is not elevated.
func elevatedScript(copyLines []string, extractDir, relaunch string) string {
	var script strings.Builder
	script.WriteString("@echo off\r\n")
	script.WriteString("title gitbox update\r\n")
	script.WriteString("echo Waiting for gitbox to exit...\r\n")
	script.WriteString("timeout /t 2 /nobreak >nul\r\n")
	for _, line := range copyLines {
		script.WriteString(line + "\r\n")
	}
	script.WriteString("echo.\r\n")
	if relaunch != "" {
		script.WriteString("echo Update complete. Restarting gitbox...\r\n")
		script.WriteString(fmt.Sprintf("explorer.exe \"%s\"\r\n", relaunch))
	} else {
		script.WriteString("echo Update complete. You can restart gitbox now.\r\n")
		script.WriteString("timeout /t 3\r\n")
	}
	// Clean up the temp extraction directory (including this script).
	script.WriteString(fmt.Sprintf("(rd /s /q \"%s\") 2>nul\r\n", extractDir))
	return script.String()
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
		return fmt.Errorf("ShellExecuteW failed (code %d) — try running the update again as Administrator", ret)
	}
	return nil
}

// CleanupOldBinary removes the backup for the running version after startup.
func CleanupOldBinary() {
	self, err := SelfPath()
	if err == nil {
		cleanupWindowsBackup(self)
	}
}
