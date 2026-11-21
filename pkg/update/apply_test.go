package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallTarget(t *testing.T) {
	bundled := filepath.Join(string(os.PathSeparator)+"Applications", "GitboxApp.app", "Contents", "MacOS", "GitboxApp")
	if got, want := installTarget(bundled), filepath.Join(string(os.PathSeparator)+"Applications"); got != want {
		t.Errorf("bundled exe: got %q, want %q", got, want)
	}

	plain := filepath.Join("home", "me", "bin", "gitbox")
	if got, want := installTarget(plain), filepath.Join("home", "me", "bin"); got != want {
		t.Errorf("plain exe: got %q, want %q", got, want)
	}

	// A MacOS directory that isn't inside a .app bundle is just a folder.
	fake := filepath.Join("opt", "Contents", "MacOS", "tool")
	if got, want := installTarget(fake), filepath.Join("opt", "Contents", "MacOS"); got != want {
		t.Errorf("non-bundle MacOS dir: got %q, want %q", got, want)
	}
}

func TestIsWithin(t *testing.T) {
	root := filepath.Join("tmp", ".mount_gitbox")
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(root, "usr", "bin", "gitbox"), true},
		{root, true},
		{filepath.Join("tmp", ".mount_gitboxOther", "gitbox"), false},
		{filepath.Join("home", "me", "bin", "gitbox"), false},
	}
	for _, c := range cases {
		if got := isWithin(c.path, root); got != c.want {
			t.Errorf("isWithin(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// writeFile creates a file with content, making parent dirs.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallExtracted_ReplacesOnlyExisting(t *testing.T) {
	extract := t.TempDir()
	install := t.TempDir()

	writeFile(t, filepath.Join(extract, "gitbox"), "new-cli")
	writeFile(t, filepath.Join(extract, "GitboxApp.app", "Contents", "MacOS", "GitboxApp"), "new-gui")

	// Only the bundle is installed here (macOS GUI in /Applications).
	writeFile(t, filepath.Join(install, "GitboxApp.app", "Contents", "MacOS", "GitboxApp"), "old-gui")
	writeFile(t, filepath.Join(install, "GitboxApp.app", "Contents", "Stale"), "stale")

	if err := InstallExtracted(extract, install); err != nil {
		t.Fatalf("InstallExtracted: %v", err)
	}
	if got := readFile(t, filepath.Join(install, "GitboxApp.app", "Contents", "MacOS", "GitboxApp")); got != "new-gui" {
		t.Errorf("bundle exe = %q, want new-gui", got)
	}
	if _, err := os.Stat(filepath.Join(install, "GitboxApp.app", "Contents", "Stale")); err == nil {
		t.Error("stale file from the old bundle survived the replacement")
	}
	if _, err := os.Stat(filepath.Join(install, "gitbox")); err == nil {
		t.Error("CLI was added next to the bundle; updates must not add components")
	}
	for _, leftover := range []string{"GitboxApp.app.new", "GitboxApp.app.old"} {
		if _, err := os.Stat(filepath.Join(install, leftover)); err == nil {
			t.Errorf("%s left behind", leftover)
		}
	}
}

func TestInstallExtracted_NothingToInstall(t *testing.T) {
	extract := t.TempDir()
	install := t.TempDir()
	writeFile(t, filepath.Join(extract, "gitbox"), "new-cli")

	if err := InstallExtracted(extract, install); err == nil {
		t.Error("expected an error when no installed entry matches the update")
	}
}

func TestInstallEntries_SelfOnly(t *testing.T) {
	extract := t.TempDir()
	install := t.TempDir()

	writeFile(t, filepath.Join(extract, "gitbox"), "new-cli")
	writeFile(t, filepath.Join(extract, "GitboxApp"), "v1-gui")
	writeFile(t, filepath.Join(install, "gitbox"), "old-cli")
	writeFile(t, filepath.Join(install, "GitboxApp"), "v2-gui")

	err := installEntries(extract, install, func(name string) bool { return name == "gitbox" })
	if err != nil {
		t.Fatalf("installEntries: %v", err)
	}
	if got := readFile(t, filepath.Join(install, "gitbox")); got != "new-cli" {
		t.Errorf("cli = %q, want new-cli", got)
	}
	if got := readFile(t, filepath.Join(install, "GitboxApp")); got != "v2-gui" {
		t.Errorf("gui = %q, want v2-gui (a CLI update must not touch the GUI)", got)
	}
}

var errReplaceInjected = errors.New("injected replace failure")

// failingReplaceOps fails the step that puts the new version in place: the
// rename into dst on Unix and for bundles, the copy on Windows. Restoring
// the previous version still works.
func failingReplaceOps(dst string) replaceOps {
	ops := realReplaceOps()
	ops.rename = func(oldpath, newpath string) error {
		if newpath == dst && oldpath != dst+".old" {
			return errReplaceInjected
		}
		return os.Rename(oldpath, newpath)
	}
	ops.copyFile = func(_, to string) error {
		// Leave a half-written file behind, as a copy cut short does.
		_ = os.WriteFile(to, []byte("partial"), 0o755)
		return errReplaceInjected
	}
	return ops
}

// names returns the entries of dir, sorted.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// A replace that fails at its last step must leave the installed binary as
// it was, with nothing half-written next to it.
func TestReplaceExecutable_FailureKeepsThePreviousBinary(t *testing.T) {
	install, extracted := t.TempDir(), t.TempDir()
	dst := filepath.Join(install, "GitboxApp")
	src := filepath.Join(extracted, "GitboxApp")
	if err := os.WriteFile(dst, []byte("old version"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("new version"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := replaceExecutable(src, dst, failingReplaceOps(dst)); !errors.Is(err, errReplaceInjected) {
		t.Fatalf("replaceExecutable = %v, want the injected failure", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "old version" {
		t.Errorf("installed binary = %q, want the previous version untouched", got)
	}
	if got := names(t, install); len(got) != 1 || got[0] != "GitboxApp" {
		t.Errorf("install folder holds %v, want only the previous binary", got)
	}

	// The real operations still replace it.
	if err := replaceExecutable(src, dst, realReplaceOps()); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "new version" {
		t.Errorf("installed binary = %q after a clean replace", got)
	}
}

func TestReplaceBundle_FailureKeepsThePreviousBundle(t *testing.T) {
	install, extracted := t.TempDir(), t.TempDir()
	dst := filepath.Join(install, "GitboxApp.app")
	src := filepath.Join(extracted, "GitboxApp.app")
	for path, content := range map[string]string{
		filepath.Join(dst, "Contents", "version"): "old version",
		filepath.Join(src, "Contents", "version"): "new version",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	version := filepath.Join(dst, "Contents", "version")

	if err := replaceBundle(src, dst, failingReplaceOps(dst)); !errors.Is(err, errReplaceInjected) {
		t.Fatalf("replaceBundle = %v, want the injected failure", err)
	}
	if got, _ := os.ReadFile(version); string(got) != "old version" {
		t.Errorf("installed bundle holds %q, want the previous version back in place", got)
	}
	if got := names(t, install); len(got) != 1 || got[0] != "GitboxApp.app" {
		t.Errorf("install folder holds %v, want only the previous bundle", got)
	}

	// A failed staging copy leaves the installed bundle alone too.
	ops := realReplaceOps()
	ops.copyDir = func(_, to string) error {
		_ = os.MkdirAll(to, 0o755)
		return errReplaceInjected
	}
	if err := replaceBundle(src, dst, ops); !errors.Is(err, errReplaceInjected) {
		t.Fatalf("replaceBundle = %v, want the injected copy failure", err)
	}
	if got := names(t, install); len(got) != 1 {
		t.Errorf("install folder holds %v after a failed staging copy", got)
	}

	if err := replaceBundle(src, dst, realReplaceOps()); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(version); string(got) != "new version" {
		t.Errorf("installed bundle holds %q after a clean replace", got)
	}
}
