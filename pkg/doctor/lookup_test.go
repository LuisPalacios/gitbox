package doctor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// writeExe drops an executable file named name (plus ext) under dir and
// returns its path.
func writeExe(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// exeName returns the on-disk file name for a bare command on this OS.
func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// isolateHome points HOME/USERPROFILE at a temp dir and empties PATH so
// only explicit directories can satisfy a lookup.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", "")
	// Keep the Windows Git fallback quiet.
	t.Setenv("ProgramFiles", "")
	t.Setenv("ProgramFiles(x86)", "")
	t.Setenv("LOCALAPPDATA", "")
	return home
}

func TestLookupInExtraDirsBeforePATH(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	want := writeExe(t, dir, exeName("harnessx"))
	if got := LookupIn("harnessx", []string{dir}); got != want {
		t.Errorf("LookupIn = %q, want %q", got, want)
	}
}

func TestLookupInSkipsUnsetAndExpandsVariables(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	want := writeExe(t, dir, exeName("harnessy"))
	t.Setenv("GITBOX_TEST_DIR", dir)
	t.Setenv("GITBOX_TEST_UNSET", "")

	cases := []string{
		"%GITBOX_TEST_DIR%",
		"$GITBOX_TEST_DIR",
		"${GITBOX_TEST_DIR}",
	}
	for _, c := range cases {
		extra := []string{"%GITBOX_TEST_UNSET%/nope", "$GITBOX_TEST_UNSET/nope", c}
		if got := LookupIn("harnessy", extra); got != want {
			t.Errorf("LookupIn with %q = %q, want %q", c, got, want)
		}
	}
}

func TestLookupInExpandsTilde(t *testing.T) {
	home := isolateHome(t)
	binDir := filepath.Join(home, ".amp", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := writeExe(t, binDir, exeName("amp"))
	if got := LookupIn("amp", []string{"~/.amp/bin"}); got != want {
		t.Errorf("LookupIn(~/.amp/bin) = %q, want %q", got, want)
	}
}

func TestLookupInUserLocalBinFallbackOnEveryOS(t *testing.T) {
	home := isolateHome(t)
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := writeExe(t, binDir, exeName("claude"))
	if got := LookupIn("claude", nil); got != want {
		t.Errorf("LookupIn fell through ~/.local/bin: got %q, want %q", got, want)
	}
}

func TestLookupInStoredPath(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	p := writeExe(t, dir, exeName("codex"))
	if got := LookupIn(p, nil); got != p {
		t.Errorf("absolute path lookup = %q, want %q", got, p)
	}
	missing := filepath.Join(dir, exeName("gone"))
	if got := LookupIn(missing, nil); got != "" {
		t.Errorf("absolute path to missing file should be \"\", got %q", got)
	}
	// A stored path must never be rescued by extraDirs or PATH.
	other := t.TempDir()
	writeExe(t, other, exeName("gone"))
	if got := LookupIn(missing, []string{other}); got != "" {
		t.Errorf("stored path must not fall back to extraDirs, got %q", got)
	}
}

func TestLookupInStoredTildePath(t *testing.T) {
	home := isolateHome(t)
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := writeExe(t, binDir, exeName("agy"))
	if got := LookupIn("~/.local/bin/"+exeName("agy"), nil); got != want {
		t.Errorf("tilde stored path = %q, want %q", got, want)
	}
}

func TestLookupInEmptyName(t *testing.T) {
	if got := LookupIn("", []string{t.TempDir()}); got != "" {
		t.Errorf("empty name should yield \"\", got %q", got)
	}
}

func TestStatExecutableWindowsShims(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("npm .cmd shims are a Windows concern")
	}
	dir := t.TempDir()
	want := writeExe(t, dir, "claude.cmd")
	if got := statExecutable(dir, "claude"); got != want {
		t.Errorf("statExecutable did not find .cmd shim: got %q, want %q", got, want)
	}
	dir2 := t.TempDir()
	want2 := writeExe(t, dir2, "tool.bat")
	if got := statExecutable(dir2, "tool"); got != want2 {
		t.Errorf("statExecutable did not find .bat shim: got %q, want %q", got, want2)
	}
}

func TestStatExecutableRequiresExecBitOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no exec bit on Windows")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "plain")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := statExecutable(dir, "plain"); got != "" {
		t.Errorf("non-executable file must not resolve, got %q", got)
	}
}

func TestStatExecutableIgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := statExecutable(dir, "cursor"); got != "" {
		t.Errorf("directory must not resolve as executable, got %q", got)
	}
}
