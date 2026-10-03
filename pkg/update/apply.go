package update

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Apply extracts the downloaded artifact and replaces the running binary.
// It installs only the running executable's own entry from the zip, never
// its siblings: the CLI stays on the v1 line while the GUI may already be on
// v2, so letting a CLI update rewrite GitboxApp would downgrade it. The GUI
// updates itself through ExtractUpdate + InstallExtracted.
// For AppImage artifacts it replaces the AppImage file directly.
func Apply(artifactPath string) error {
	if strings.HasSuffix(artifactPath, ".AppImage") {
		return applyAppImage(artifactPath)
	}
	extractDir, installDir, err := ExtractUpdate(artifactPath)
	if err != nil {
		return err
	}
	defer os.RemoveAll(extractDir)

	self, err := selfPath()
	if err != nil {
		return err
	}
	own := filepath.Base(self)
	return installEntries(extractDir, installDir, func(name string) bool {
		return name == own
	})
}

// ExtractUpdate extracts a zip artifact and resolves the install directory.
// Returns (extractDir, installDir, error). The caller owns extractDir cleanup.
func ExtractUpdate(zipPath string) (string, string, error) {
	self, err := selfPath()
	if err != nil {
		return "", "", err
	}
	installDir := installTarget(self)

	extractDir, err := extractZip(zipPath)
	if err != nil {
		return "", "", fmt.Errorf("extracting update: %w", err)
	}
	return extractDir, installDir, nil
}

// InstallExtracted replaces binaries from extractDir into installDir.
// It only replaces entries that already exist in installDir: an update
// refreshes what is installed and never adds components. That keeps a macOS
// GUI update from dropping the CLI into /Applications, and a CLI update from
// creating a GitboxApp.app next to ~/bin/gitbox.
func InstallExtracted(extractDir, installDir string) error {
	return installEntries(extractDir, installDir, func(name string) bool {
		_, err := os.Lstat(filepath.Join(installDir, name))
		return err == nil
	})
}

// installEntries copies the zip entries accepted by keep into installDir.
// Files replace executables in place; .app directories (macOS bundles)
// replace the whole bundle.
func installEntries(extractDir, installDir string, keep func(name string) bool) error {
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return fmt.Errorf("reading extracted files: %w", err)
	}

	installed := 0
	for _, entry := range entries {
		if !keep(entry.Name()) {
			continue
		}
		src := filepath.Join(extractDir, entry.Name())
		dst := filepath.Join(installDir, entry.Name())

		if entry.IsDir() {
			if !strings.HasSuffix(entry.Name(), ".app") {
				continue
			}
			if err := replaceBundle(src, dst); err != nil {
				return fmt.Errorf("replacing %s: %w", entry.Name(), err)
			}
			installed++
			continue
		}

		if err := replaceExecutable(src, dst); err != nil {
			return fmt.Errorf("replacing %s: %w", entry.Name(), err)
		}
		installed++
	}

	if installed == 0 {
		return fmt.Errorf("update contains nothing to install in %s", installDir)
	}
	return nil
}

// selfPath returns the running executable with symlinks resolved.
func selfPath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("finding current executable: %w", err)
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("resolving symlinks: %w", err)
	}
	return p, nil
}

// installTarget returns the directory an update for exePath installs into.
// For an executable inside a macOS bundle (…/X.app/Contents/MacOS/exe) that
// is the directory holding the bundle, so the whole .app is replaced.
// Otherwise it is the executable's own directory.
func installTarget(exePath string) string {
	macOSDir := filepath.Dir(exePath)
	contents := filepath.Dir(macOSDir)
	bundle := filepath.Dir(contents)
	if filepath.Base(macOSDir) == "MacOS" &&
		filepath.Base(contents) == "Contents" &&
		strings.HasSuffix(filepath.Base(bundle), ".app") {
		return filepath.Dir(bundle)
	}
	return macOSDir
}

// replaceBundle swaps the bundle at dst for src. The new bundle is staged
// next to dst and renamed into place, so a failed copy never leaves a
// half-written app behind.
func replaceBundle(src, dst string) error {
	staged := dst + ".new"
	old := dst + ".old"
	os.RemoveAll(staged)
	os.RemoveAll(old)

	if err := copyDir(src, staged); err != nil {
		os.RemoveAll(staged)
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			os.RemoveAll(staged)
			return err
		}
	}
	if err := os.Rename(staged, dst); err != nil {
		// Put the previous bundle back.
		os.Rename(old, dst)
		return err
	}
	os.RemoveAll(old)
	return nil
}

func applyAppImage(newAppImage string) error {
	// The current AppImage path is in $APPIMAGE env var.
	if !runningFromAppImage() {
		return fmt.Errorf("not running from an AppImage — cannot determine current AppImage path")
	}
	currentPath := os.Getenv("APPIMAGE")

	return replaceExecutable(newAppImage, currentPath)
}

func extractZip(zipPath string) (string, error) {
	extractDir, err := os.MkdirTemp("", "gitbox-extract-*")
	if err != nil {
		return "", err
	}

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		os.RemoveAll(extractDir)
		return "", fmt.Errorf("opening zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		destPath := filepath.Join(extractDir, f.Name)

		// Guard against zip slip.
		if !strings.HasPrefix(filepath.Clean(destPath), filepath.Clean(extractDir)+string(os.PathSeparator)) {
			continue
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(destPath, f.Mode())
			continue
		}

		// Ensure parent directory exists.
		os.MkdirAll(filepath.Dir(destPath), 0o755)

		outFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			os.RemoveAll(extractDir)
			return "", fmt.Errorf("creating %s: %w", f.Name, err)
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			os.RemoveAll(extractDir)
			return "", fmt.Errorf("opening %s in zip: %w", f.Name, err)
		}

		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()
		if err != nil {
			os.RemoveAll(extractDir)
			return "", fmt.Errorf("extracting %s: %w", f.Name, err)
		}
	}

	return extractDir, nil
}

// copyDir recursively copies a directory tree.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(src, path)
		destPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode())
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

// runningFromAppImage reports whether this executable runs from inside an
// AppImage. $APPIMAGE alone isn't enough: terminals opened from the GUI
// inherit it, and a separately installed CLI started there must not replace
// the GUI's AppImage. When $APPDIR is set, the executable must live under it.
func runningFromAppImage() bool {
	if os.Getenv("APPIMAGE") == "" {
		return false
	}
	appDir := os.Getenv("APPDIR")
	if appDir == "" {
		return true
	}
	self, err := selfPath()
	if err != nil {
		return false
	}
	return isWithin(self, appDir)
}

// isWithin reports whether path is dir or lies below it.
func isWithin(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}
