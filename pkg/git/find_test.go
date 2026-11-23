package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"testing"
)

func TestFindRepos(t *testing.T) {
	root := t.TempDir()

	// Create a normal repo.
	repo1 := filepath.Join(root, "repo1")
	os.MkdirAll(filepath.Join(repo1, ".git"), 0o755)

	// Create a nested repo (org/repo structure).
	repo2 := filepath.Join(root, "org", "repo2")
	os.MkdirAll(filepath.Join(repo2, ".git"), 0o755)

	// Create a hidden directory (should be skipped).
	os.MkdirAll(filepath.Join(root, ".hidden", "secret", ".git"), 0o755)

	// Create a plain directory (not a repo).
	os.MkdirAll(filepath.Join(root, "not-a-repo", "subdir"), 0o755)

	repos, err := FindRepos(root)
	if err != nil {
		t.Fatalf("FindRepos error: %v", err)
	}

	sort.Strings(repos)
	want := []string{repo1, repo2}
	sort.Strings(want)

	if len(repos) != len(want) {
		t.Fatalf("found %d repos, want %d: %v", len(repos), len(want), repos)
	}
	for i := range want {
		if repos[i] != want[i] {
			t.Errorf("repos[%d] = %q, want %q", i, repos[i], want[i])
		}
	}
}

func TestFindRepos_Empty(t *testing.T) {
	root := t.TempDir()
	repos, err := FindRepos(root)
	if err != nil {
		t.Fatalf("FindRepos error: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("expected 0 repos, got %d", len(repos))
	}
}

func TestFindRepos_StopsAtCloneBoundary(t *testing.T) {
	for _, gitFile := range []bool{false, true} {
		for _, rootClone := range []bool{false, true} {
			t.Run(fmt.Sprintf("file=%t/root=%t", gitFile, rootClone), func(t *testing.T) {
				root := t.TempDir()
				clone := root
				if !rootClone {
					clone = filepath.Join(root, "parent")
				}
				if err := os.MkdirAll(filepath.Join(clone, "child", ".git"), 0o755); err != nil {
					t.Fatal(err)
				}
				if gitFile {
					if err := os.WriteFile(filepath.Join(clone, ".git"), []byte("gitdir: elsewhere"), 0o644); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Mkdir(filepath.Join(clone, ".git"), 0o755); err != nil {
					t.Fatal(err)
				}
				got, err := FindRepos(root)
				if err != nil || len(got) != 1 || got[0] != clone {
					t.Fatalf("FindRepos = %v, %v; want [%s]", got, err, clone)
				}
			})
		}
	}
}

func TestFindReposContextCoverage(t *testing.T) {
	root := t.TempDir()
	found := filepath.Join(root, "found")
	skipped := filepath.Join(root, "group", "deeper")
	for _, repo := range []string{found, skipped} {
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	c, err := FindReposContext(t.Context(), root, 1)
	if err != nil || c.Complete || !slices.Equal(c.Skipped, []string{skipped}) || !slices.Equal(c.Repos, []string{found}) {
		t.Fatalf("depth-limited walk = %+v, %v", c, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c, err = FindReposContext(ctx, root, 5)
	if !errors.Is(err, context.Canceled) || c.Complete || len(c.Repos) != 0 {
		t.Fatalf("cancelled walk = %+v, %v", c, err)
	}
	t.Run("symlink root", func(t *testing.T) {
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(root, alias); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		c, err := FindReposContext(t.Context(), alias, 8)
		if err != nil || !c.Complete || len(c.Repos) != 0 {
			t.Fatalf("symlink root: %+v, %v", c, err)
		}
	})
	t.Run("permission denied", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Unix mode bits")
		}
		denied := filepath.Join(t.TempDir(), "denied")
		if err := os.Mkdir(denied, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(denied, 0700) })
		if _, err := os.ReadDir(denied); err == nil {
			t.Skip("privileged reader")
		}
		c, err := FindReposContext(t.Context(), filepath.Dir(denied), 8)
		if err != nil || c.Complete || !slices.Equal(c.Skipped, []string{denied}) || len(c.Errors) != 1 || !errors.Is(c.Errors[0], os.ErrPermission) {
			t.Fatalf("unreadable directory = %+v, %v", c, err)
		}
	})
}
