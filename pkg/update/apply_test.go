package update

import (
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
