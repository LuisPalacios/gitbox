package git

import (
	"os"
	"path/filepath"
	"testing"
)

// mkGitClone creates dir with a .git subdirectory so FindNestedRepos sees a clone.
func mkGitClone(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func TestFindNestedRepos_Depth1FindsDirectChildren(t *testing.T) {
	parent := t.TempDir()
	mkGitClone(t, parent) // parent is itself a clone; its own .git is ignored
	mkGitClone(t, filepath.Join(parent, "child-a"))
	mkGitClone(t, filepath.Join(parent, "child-b"))
	// A nested-deeper clone that depth 1 must NOT find.
	mkGitClone(t, filepath.Join(parent, "vendorgroup", "deep"))

	got := FindNestedRepos(parent, 1)
	if len(got) != 2 {
		t.Fatalf("depth 1 found %d (%v), want 2 direct children", len(got), got)
	}
}

func TestFindNestedRepos_Depth2FindsGrandchildren(t *testing.T) {
	parent := t.TempDir()
	mkGitClone(t, filepath.Join(parent, "child-a"))
	// A non-clone subdir holding a clone at depth 2 (like sumwall.z-vendor/kltv.kombine).
	mkGitClone(t, filepath.Join(parent, "vendorgroup", "deep"))

	got := FindNestedRepos(parent, 2)
	if len(got) != 2 {
		t.Fatalf("depth 2 found %d (%v), want 2", len(got), got)
	}
}

func TestFindNestedRepos_SkipsVendorAndHiddenDirs(t *testing.T) {
	parent := t.TempDir()
	mkGitClone(t, filepath.Join(parent, "node_modules", "pkg")) // vendor — skipped
	mkGitClone(t, filepath.Join(parent, ".cache", "thing"))     // hidden — skipped
	mkGitClone(t, filepath.Join(parent, "real"))               // kept

	got := FindNestedRepos(parent, 3)
	if len(got) != 1 || filepath.Base(got[0]) != "real" {
		t.Fatalf("found %v, want only [real]", got)
	}
}

func TestFindNestedRepos_DoesNotDescendIntoFoundClone(t *testing.T) {
	parent := t.TempDir()
	// A clone that itself contains another clone — the inner one must be ignored.
	mkGitClone(t, filepath.Join(parent, "outer"))
	mkGitClone(t, filepath.Join(parent, "outer", "inner"))

	got := FindNestedRepos(parent, 5)
	if len(got) != 1 || filepath.Base(got[0]) != "outer" {
		t.Fatalf("found %v, want only [outer] (no descent into found clone)", got)
	}
}
