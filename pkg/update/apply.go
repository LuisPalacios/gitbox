package update

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ExtractUpdate extracts a zip artifact and resolves the install directory.
// Returns (extractDir, installDir, error). The caller owns extractDir cleanup.
func ExtractUpdate(zipPath string) (string, string, error) {
	self, err := SelfPath()
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
	return installVersion(extractDir, installDir, "")
}

// InstallVersion retains the current version for a possible rollback.
func InstallVersion(extractDir, installDir, previousVersion string) error {
	if previousVersion == "" {
		return InstallExtracted(extractDir, installDir)
	}
	return installVersion(extractDir, installDir, previousVersion)
}

func installVersion(extractDir, installDir, previousVersion string) error {
	return installEntries(extractDir, installDir, previousVersion, func(name string) bool {
		_, err := os.Lstat(filepath.Join(installDir, name))
		return err == nil
	})
}

// installEntries copies the zip entries accepted by keep into installDir.
// Files replace executables in place; .app directories (macOS bundles)
// replace the whole bundle.
func installEntries(extractDir, installDir, previousVersion string, keep func(name string) bool) error {
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return fmt.Errorf("reading extracted files: %w", err)
	}

	ops := realReplaceOps()
	ops.previousVersion = previousVersion
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
			if err := replaceBundle(src, dst, ops); err != nil {
				return fmt.Errorf("replacing %s: %w", entry.Name(), err)
			}
			installed++
			continue
		}

		if err := replaceExecutable(src, dst, ops); err != nil {
			return fmt.Errorf("replacing %s: %w", entry.Name(), err)
		}
		installed++
	}

	if installed == 0 {
		return fmt.Errorf("update contains nothing to install in %s", installDir)
	}
	return nil
}

// SelfPath returns the running executable with symlinks resolved.
func SelfPath() (string, error) {
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
	if bundle := bundleOf(exePath); bundle != "" {
		return filepath.Dir(bundle)
	}
	return filepath.Dir(exePath)
}

// bundleOf returns the macOS bundle holding exePath
// (…/X.app/Contents/MacOS/exe → …/X.app), or "" when it isn't in one.
func bundleOf(exePath string) string {
	macOSDir := filepath.Dir(exePath)
	contents := filepath.Dir(macOSDir)
	bundle := filepath.Dir(contents)
	if filepath.Base(macOSDir) == "MacOS" &&
		filepath.Base(contents) == "Contents" &&
		strings.HasSuffix(filepath.Base(bundle), ".app") {
		return bundle
	}
	return ""
}

// replaceOps are the file operations of the replace steps. They are passed
// in, with the real ones by default, so a test can make one step fail and
// check that the installed app is still there.
type replaceOps struct {
	previousVersion string
	exchange        func(string, string) error
	rename          func(oldpath, newpath string) error
	copyDir         func(src, dst string) error
	// Windows stages the replacement before moving either executable.
	copyFile func(dst io.Writer, src io.Reader) (int64, error)
	moveFile func(src, dst string) error
}

func realReplaceOps() replaceOps {
	return replaceOps{rename: os.Rename, copyDir: copyDir, exchange: bundleExchange()}
}

// replaceBundle swaps the bundle at dst for src. The new bundle is staged
// next to dst and renamed into place, so a failed copy never leaves a
// half-written app behind.
func replaceBundle(src, dst string, ops replaceOps) error {
	staged, err := os.MkdirTemp(filepath.Dir(dst), ".gitbox-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged)
	if err := ops.copyDir(src, staged); err != nil {
		return err
	}
	if err := os.Chmod(staged, 0755); err != nil {
		return err
	}
	return installPrevious(staged, dst, ops)
}

// ErrInvalidArchive identifies an update that cannot be extracted safely.
var ErrInvalidArchive = errors.New("invalid update archive")

const (
	maxArchiveEntries = 2000
	maxArchiveBytes   = 512 << 20
)

func extractZip(zipPath string) (string, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		if r != nil {
			_ = r.Close()
		}
		return "", fmt.Errorf("%w: opening zip: %w", ErrInvalidArchive, err)
	}
	defer r.Close()
	if len(r.File) > maxArchiveEntries {
		return "", fmt.Errorf("%w: too many entries", ErrInvalidArchive)
	}
	var total uint64
	for _, f := range r.File {
		if _, err := archivePath(f); err != nil {
			return "", err
		}
		if f.UncompressedSize64 > maxArchiveBytes-total {
			return "", fmt.Errorf("%w: size limit exceeded", ErrInvalidArchive)
		}
		total += f.UncompressedSize64
	}

	dir, err := os.MkdirTemp("", "gitbox-extract-*")
	if err != nil {
		return "", err
	}
	for _, f := range r.File {
		name, _ := archivePath(f)
		if err := extractEntry(f, filepath.Join(dir, name)); err != nil {
			_ = os.RemoveAll(dir)
			return "", err
		}
	}
	return dir, nil
}

func archivePath(f *zip.File) (string, error) {
	name := strings.TrimSuffix(f.Name, "/")
	if name == "." || !fs.ValidPath(name) || strings.ContainsAny(name, `\:`) ||
		(f.Mode().Type() != 0 && f.Mode().Type() != fs.ModeDir) || (f.Mode().IsDir() && f.UncompressedSize64 != 0) {
		return "", fmt.Errorf("%w: unsupported entry", ErrInvalidArchive)
	}
	local, err := filepath.Localize(name)
	if err != nil {
		return "", fmt.Errorf("%w: invalid path", ErrInvalidArchive)
	}
	return local, nil
}

func extractEntry(f *zip.File, dst string) error {
	if f.FileInfo().IsDir() {
		return os.MkdirAll(dst, 0755)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := f.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, f.Mode().Perm())
	if err != nil {
		return err
	}
	size := int64(f.UncompressedSize64) // bounded in extractZip
	n, copyErr := io.Copy(out, io.LimitReader(in, size+1))
	if err := errors.Join(copyErr, out.Close()); err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("%w: entry size mismatch", ErrInvalidArchive)
	}
	return nil
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
// inherit it, and a separately installed binary started there must not be
// mistaken for the AppImage build. When $APPDIR is set, the executable must
// live under it.
func runningFromAppImage() bool {
	if os.Getenv("APPIMAGE") == "" {
		return false
	}
	appDir := os.Getenv("APPDIR")
	if appDir == "" {
		return true
	}
	self, err := SelfPath()
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
