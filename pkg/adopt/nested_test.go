package adopt

import (
	"path/filepath"
	"testing"
)

// TestFindOrphans_NestedClonesUnderContainer verifies the sumwall.project shape:
// a managed container clone holds user-cloned siblings inside its working tree,
// and FindOrphans surfaces them as nested orphans (never filtered as submodules).
func TestFindOrphans_NestedClonesUnderContainer(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)

	// The container is a tracked repo flagged Container.
	containerPath := filepath.Join(root, "github-me", "LuisPalacios", "tracked-repo")
	initBareRepo(t, containerPath, "https://github.com/LuisPalacios/tracked-repo.git")
	src := cfg.Sources["github-me"]
	repo := src.Repos["LuisPalacios/tracked-repo"]
	repo.Container = true
	src.Repos["LuisPalacios/tracked-repo"] = repo
	cfg.Sources["github-me"] = src

	// Two clones nested INSIDE the container's working tree.
	nestedA := filepath.Join(containerPath, "nested-a")
	initBareRepo(t, nestedA, "https://github.com/LuisPalacios/nested-a.git")
	nestedB := filepath.Join(containerPath, "nested-b")
	initBareRepo(t, nestedB, "https://github.com/LuisPalacios/nested-b.git")

	orphans, err := FindOrphans(cfg)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}

	byKey := map[string]OrphanRepo{}
	for _, o := range orphans {
		byKey[o.RepoKey] = o
	}

	for _, key := range []string{"LuisPalacios/nested-a", "LuisPalacios/nested-b"} {
		o, ok := byKey[key]
		if !ok {
			t.Fatalf("nested clone %q not discovered; orphans=%+v", key, orphans)
		}
		if !o.Nested {
			t.Errorf("%q should be marked Nested", key)
		}
		if o.MatchedAccount != "github-me" {
			t.Errorf("%q matched account = %q, want github-me", key, o.MatchedAccount)
		}
		// A nested clone lives outside the standard layout, so it needs an
		// absolute clone_folder on adoption.
		if !o.NeedsRelocate {
			t.Errorf("%q should report NeedsRelocate (path differs from standard)", key)
		}
	}
}

// TestFindOrphansIn_ExtraRoot verifies a clone in a caller-supplied folder is
// discovered and reported as needing an absolute clone_folder.
func TestFindOrphansIn_ExtraRoot(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)

	extra := t.TempDir()
	clone := filepath.Join(extra, "some-project")
	initBareRepo(t, clone, "https://github.com/LuisPalacios/some-project.git")

	orphans, err := FindOrphansIn(cfg, []string{extra})
	if err != nil {
		t.Fatalf("FindOrphansIn: %v", err)
	}
	var found *OrphanRepo
	for i := range orphans {
		if orphans[i].RepoKey == "LuisPalacios/some-project" {
			found = &orphans[i]
		}
	}
	if found == nil {
		t.Fatalf("clone in extra root not discovered; orphans=%+v", orphans)
	}
	if found.MatchedAccount != "github-me" || !found.NeedsRelocate {
		t.Errorf("extra-root clone = %+v, want matched github-me + NeedsRelocate", found)
	}
}
