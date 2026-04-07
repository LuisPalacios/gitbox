//go:build windows

package update

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

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
			return fmt.Errorf("renaming %s to .old: %w", filepath.Base(dst), err)
		}
	}

	// Copy new binary to dst (can't use Rename across volumes).
	if err := copyFile(src, dst); err != nil {
		// Try to restore the old binary on failure.
		os.Rename(oldPath, dst)
		return fmt.Errorf("writing new %s: %w", filepath.Base(dst), err)
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
