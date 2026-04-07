//go:build !windows

package update

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// replaceExecutable atomically replaces the binary at dst with src.
// On Unix, rename(2) is atomic within the same filesystem and the OS keeps
// the old inode open for the running process.
func replaceExecutable(src, dst string) error {
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
	srcFile.Close()
	tmp.Close()

	// Preserve executable permissions.
	if err := os.Chmod(tmpPath, srcInfo.Mode()|0o111); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Atomic rename.
	if err := os.Rename(tmpPath, dst); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming to %s: %w", filepath.Base(dst), err)
	}

	return nil
}

// CleanupOldBinary is a no-op on Unix — atomic rename doesn't leave .old files.
func CleanupOldBinary() {}
