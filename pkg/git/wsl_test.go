package git

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestIsWSLAvailable_NonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows-specific behavior covered by other tests")
	}
	if IsWSLAvailable() {
		t.Fatalf("IsWSLAvailable() = true on %s, want false", runtime.GOOS)
	}
}

func TestWSLPath_NonWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows-specific behavior covered by other tests")
	}
	if _, err := WSLPath("~/.tmuxinator"); err == nil {
		t.Fatalf("WSLPath() succeeded on %s, want error", runtime.GOOS)
	}
	if _, err := WSLHome(); err == nil {
		t.Fatalf("WSLHome() succeeded on %s, want error", runtime.GOOS)
	}
}

// TestIsWSLAvailable_Windows runs only when the harness explicitly opts in via
// GITBOX_TEST_WSL=1. WSL availability on a CI runner is flaky and the install
// can be heavyweight, so we keep the gate explicit.
func TestIsWSLAvailable_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}
	if os.Getenv("GITBOX_TEST_WSL") != "1" {
		t.Skip("set GITBOX_TEST_WSL=1 to exercise WSL probing")
	}
	if !IsWSLAvailable() {
		t.Skip("WSL not installed on this host")
	}
	home, err := WSLHome()
	if err != nil {
		t.Fatalf("WSLHome: %v", err)
	}
	if !strings.HasPrefix(home, "/") {
		t.Fatalf("WSLHome() = %q, want a Linux-style absolute path", home)
	}
	winPath, err := WSLPath(home)
	if err != nil {
		t.Fatalf("WSLPath(%q): %v", home, err)
	}
	if !strings.Contains(winPath, "\\") {
		t.Fatalf("WSLPath() = %q, want a Windows-style path", winPath)
	}
}
