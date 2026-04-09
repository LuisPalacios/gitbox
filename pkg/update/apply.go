package update

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Apply extracts the downloaded artifact and replaces the running binaries.
// For zip files: extracts and replaces CLI + GUI binaries next to the running binary.
// For AppImage files: replaces the AppImage file directly.
func Apply(artifactPath string) error {
	if strings.HasSuffix(artifactPath, ".AppImage") {
		return applyAppImage(artifactPath)
	}
	extractDir, installDir, err := ExtractUpdate(artifactPath)
	if err != nil {
		return err
	}
	defer os.RemoveAll(extractDir)
	return InstallExtracted(extractDir, installDir)
}

// ExtractUpdate extracts a zip artifact and resolves the install directory.
// Returns (extractDir, installDir, error). The caller owns extractDir cleanup.
func ExtractUpdate(zipPath string) (string, string, error) {
	selfPath, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("finding current executable: %w", err)
	}
	selfPath, err = filepath.EvalSymlinks(selfPath)
	if err != nil {
		return "", "", fmt.Errorf("resolving symlinks: %w", err)
	}
	installDir := filepath.Dir(selfPath)

	extractDir, err := extractZip(zipPath)
	if err != nil {
		return "", "", fmt.Errorf("extracting update: %w", err)
	}
	return extractDir, installDir, nil
}

// InstallExtracted replaces binaries from extractDir into installDir.
func InstallExtracted(extractDir, installDir string) error {
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return fmt.Errorf("reading extracted files: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Handle .app bundles (macOS) — copy the entire directory.
			if strings.HasSuffix(entry.Name(), ".app") {
				dst := filepath.Join(installDir, entry.Name())
				os.RemoveAll(dst)
				if err := copyDir(filepath.Join(extractDir, entry.Name()), dst); err != nil {
					return fmt.Errorf("replacing %s: %w", entry.Name(), err)
				}
			}
			continue
		}

		src := filepath.Join(extractDir, entry.Name())
		dst := filepath.Join(installDir, entry.Name())

		if err := replaceExecutable(src, dst); err != nil {
			return fmt.Errorf("replacing %s: %w", entry.Name(), err)
		}
	}

	return nil
}

func applyAppImage(newAppImage string) error {
	// The current AppImage path is in $APPIMAGE env var.
	currentPath := os.Getenv("APPIMAGE")
	if currentPath == "" {
		return fmt.Errorf("$APPIMAGE not set — cannot determine current AppImage path")
	}

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
