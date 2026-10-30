package terminals

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

const fakeWSLSentinelEnv = "GITBOX_TEST_FAKE_WSL_SENTINEL"

// TestMain turns the test binary into a fake wsl.exe when a test runs a copy of
// it, so the tests can tell whether detection executed the file.
func TestMain(m *testing.M) {
	if sentinel := os.Getenv(fakeWSLSentinelEnv); sentinel != "" {
		_ = os.WriteFile(sentinel, nil, 0o644)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// setupFakeWSL puts a runnable wsl.exe alone on PATH, makes wslDistros return
// distros, and returns the sentinel file that appears if anything runs wsl.exe.
func setupFakeWSL(t *testing.T, distros []string) string {
	t.Helper()
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(filepath.Join(dir, "wsl.exe"), os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(t.TempDir(), "wsl-was-run")
	t.Setenv(fakeWSLSentinelEnv, sentinel)
	t.Setenv("PATH", dir)
	previous := wslDistros
	wslDistros = func() []string { return distros }
	t.Cleanup(func() { wslDistros = previous })
	return sentinel
}

func findShell(shells []config.ShellEntry, id string) (config.ShellEntry, bool) {
	for _, s := range shells {
		if s.ID == id {
			return s, true
		}
	}
	return config.ShellEntry{}, false
}

// The freshly installed PC: wsl.exe resolves, but it is the install stub.
// Running it opens the install prompt, and listing it offers a dead shell.
func TestDetectShells_InstallStubIsNotAShellAndNeverRuns(t *testing.T) {
	sentinel := setupFakeWSL(t, nil)

	shells := DetectShells("windows")

	if s, ok := findShell(shells, "wsl"); ok {
		t.Errorf("install stub listed as a shell: %+v", s)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Error("detection executed wsl.exe")
	}
}

func TestDetectShells_RegisteredDistrosGetRowsWithoutRunningWSL(t *testing.T) {
	sentinel := setupFakeWSL(t, []string{"Ubuntu-24.04"})

	shells := DetectShells("windows")

	if _, ok := findShell(shells, "wsl"); !ok {
		t.Errorf("bare wsl row missing: %+v", shells)
	}
	distro, ok := findShell(shells, "wsl-ubuntu-24-04")
	if !ok {
		t.Fatalf("distro row missing: %+v", shells)
	}
	if len(distro.Args) != 2 || distro.Args[0] != "-d" || distro.Args[1] != "Ubuntu-24.04" {
		t.Errorf("distro args = %v", distro.Args)
	}
	if _, err := os.Stat(sentinel); err == nil {
		t.Error("detection executed wsl.exe")
	}
}

// Control for the sentinel: a copy of the test binary named wsl.exe does run
// and does leave the file, so its absence above is meaningful.
func TestFakeWSLSentinelControl(t *testing.T) {
	sentinel := setupFakeWSL(t, nil)
	path, ok := probeBinary("wsl.exe")()
	if !ok {
		t.Fatal("fake wsl.exe not resolved")
	}
	if err := exec.Command(path).Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("fake wsl.exe left no sentinel: %v", err)
	}
}

func TestLookPathWithBrewPATH_PreservesEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executable uses Unix executable permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "gitbox-test-shell")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := lookPathWithBrewPATH("gitbox-test-shell")
	if err != nil || got != path {
		t.Fatalf("lookup = %q, %v; want %q", got, err, path)
	}
	if os.Getenv("PATH") != dir {
		t.Fatal("lookup changed process PATH")
	}
}
