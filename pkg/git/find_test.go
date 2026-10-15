package git

import (
	"fmt"
	"os"
	"path/filepath"
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
