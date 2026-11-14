package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/git"
)

func TestFailedCloneError_PreservesDestination(t *testing.T) {
	for _, kind := range []string{"repo", "directory", "symlink", "missing"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "clone")
			if kind == "missing" {
				cloneErr := errors.New("git clone failed")
				if err := failedCloneError(dest, cloneErr); err != cloneErr {
					t.Fatalf("unexpected destination report: %v", err)
				}
				if _, err := os.Lstat(dest); !os.IsNotExist(err) {
					t.Fatalf("created a destination: %v", err)
				}
				return
			}
			realDest := dest
			if kind == "symlink" {
				realDest = filepath.Join(root, "other-repo")
			}
			if err := os.Mkdir(realDest, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(root, "gitconfig"))
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			gitCmd := func(args ...string) {
				t.Helper()
				cmd := exec.Command(git.GitBin(), args...)
				cmd.Dir = realDest
				cmd.Env = git.Environ()
				git.HideWindow(cmd)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, out)
				}
			}
			var before []byte
			configPath := filepath.Join(realDest, ".git", "config")
			if kind != "directory" {
				gitCmd("init", "-q")
				gitCmd("remote", "add", "origin", "https://example.com/other/repo.git")
				var err error
				before, err = os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
			}
			if kind == "symlink" {
				if err := os.Symlink(realDest, dest); err != nil {
					if runtime.GOOS == "windows" {
						t.Skipf("symlink unavailable: %v", err)
					}
					t.Fatal(err)
				}
			}
			marker := filepath.Join(realDest, "keep.txt")
			if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			cloneErr := errors.New("git clone failed")
			err := failedCloneError(dest, cloneErr)
			if !errors.Is(err, cloneErr) || !strings.Contains(err.Error(), "clone destination remains at "+dest) {
				t.Fatalf("unexpected clone error: %v", err)
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "keep" {
				t.Fatalf("destination contents changed: %q, %v", data, err)
			}
			if before != nil {
				if after, err := os.ReadFile(configPath); err != nil || !bytes.Equal(before, after) {
					t.Fatalf("repository config changed: %v", err)
				}
			}
			if kind == "symlink" {
				if target, err := os.Readlink(dest); err != nil || target != realDest {
					t.Fatalf("symlink changed: %q, %v", target, err)
				}
			}
		})
	}
}
