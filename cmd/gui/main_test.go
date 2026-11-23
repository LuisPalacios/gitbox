package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMain gives the tests a home folder of their own. The SSH setup writes
// keys and Host entries into ~/.ssh unless the config names another folder
// (#244), and other flows fall back to the home folder the same way, so no
// test may reach the real one. The Go caches keep their real places:
// TestGitboxServerBinary builds the server.
func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	out, err := exec.Command("go", "env", "GOCACHE", "GOMODCACHE", "GOPATH").Output()
	if err == nil {
		values := strings.Split(strings.TrimSpace(string(out)), "\n")
		for i, name := range []string{"GOCACHE", "GOMODCACHE", "GOPATH"} {
			if i < len(values) && os.Getenv(name) == "" {
				_ = os.Setenv(name, strings.TrimSpace(values[i]))
			}
		}
	}
	home, err := os.MkdirTemp("", "gitbox-gui-home")
	if err != nil {
		fmt.Fprintln(os.Stderr, "test home:", err)
		return 1
	}
	defer os.RemoveAll(home)
	for _, name := range []string{"HOME", "USERPROFILE"} {
		_ = os.Setenv(name, home)
	}
	return m.Run()
}

// TestHomeIsTheTestsOwn: the home folder the tests see is the one TestMain
// made, not the user's.
func TestHomeIsTheTestsOwn(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || !strings.Contains(home, "gitbox-gui-home") {
		t.Errorf("os.UserHomeDir() = %q, %v; want the test home", home, err)
	}
}
