package adopt

import (
	"os"
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
	t.Run("each listed folder has its own depth budget", func(t *testing.T) {
		repo.Workspace = "fleet.code-workspace"
		src.Repos["LuisPalacios/tracked-repo"] = repo
		cfg.Global.NestedScanDepth = 1
		if err := os.WriteFile(filepath.Join(containerPath, repo.Workspace), []byte(`{"folders":[{"path":"deep/listed"}]}`), 0600); err != nil {
			t.Fatal(err)
		}
		member := filepath.Join(containerPath, "deep", "listed")
		initBareRepo(t, filepath.Join(member, "visible"), "https://github.com/LuisPalacios/visible.git")
		initBareRepo(t, filepath.Join(member, "layer", "hidden"), "https://github.com/LuisPalacios/hidden.git")
		found, skipped, err := FindOrphansContext(t.Context(), cfg, nil)
		if err != nil || skipped == 0 {
			t.Fatalf("coverage: %d skipped, %v", skipped, err)
		}
		var visible bool
		for _, o := range found {
			if o.Repo == "visible" {
				visible = o.Nested
			}
			if o.Repo == "hidden" {
				t.Fatal("discovered a clone beyond the workspace limit")
			}
		}
		if !visible {
			t.Fatal("clone one level below a deep workspace folder was not discovered")
		}
	})

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

func TestFindOrphansIn_ExplicitNestedRoot(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	parent := filepath.Join(root, "github-me", "LuisPalacios", "tracked-repo")
	initBareRepo(t, parent, "https://github.com/LuisPalacios/tracked-repo.git")
	child := filepath.Join(parent, "explicit-child")
	initBareRepo(t, child, "https://github.com/LuisPalacios/explicit-child.git")
	for _, configured := range []bool{false, true} {
		cfg.Global.Roots = cfg.Global.Roots[:1:1]
		roots := []string{child}
		if configured {
			cfg.Global.Roots = append(cfg.Global.Roots, roots...)
			roots = nil
		}
		found, err := FindOrphansIn(cfg, roots)
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 1 || found[0].Path != child {
			t.Fatalf("configured=%t: explicit nested clone lost: %v", configured, found)
		}
	}
}

// TestFindOrphans_CanonicalCloneInSecondRootAdoptsInPlace verifies a clone at
// the standard layout spot of a second configured root is adopted where it
// is, under that root (#149), instead of being moved to the default one.
func TestFindOrphans_CanonicalCloneInSecondRootAdoptsInPlace(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	second := t.TempDir()
	cfg.AddRoot(second)
	clone := filepath.Join(second, "github-me", "LuisPalacios", "side-project")
	initBareRepo(t, clone, "https://github.com/LuisPalacios/side-project.git")

	orphans, err := FindOrphans(cfg)
	if err != nil {
		t.Fatalf("FindOrphans: %v", err)
	}
	if len(orphans) != 1 {
		t.Fatalf("orphans = %+v, want the one clone", orphans)
	}
	o := orphans[0]
	if o.MatchedSource != "github-me" || o.NeedsRelocate || o.ExpectedRoot != second || o.ExpectedPath != clone {
		t.Errorf("orphan = %+v, want github-me in place under %s", o, second)
	}
}

// TestFindOrphans_ConfidenceNeedsMoreThanTheHost verifies a third-party repo
// matched on host alone is not confident, while one owned by the account, or
// placed under its source folder, is (#149).
func TestFindOrphans_ConfidenceNeedsMoreThanTheHost(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	initBareRepo(t, filepath.Join(root, "scratch", "linux"), "https://github.com/torvalds/linux.git")
	initBareRepo(t, filepath.Join(root, "scratch", "mine"), "https://github.com/LuisPalacios/mine.git")
	initBareRepo(t, filepath.Join(root, "github-me", "torvalds", "subsurface"), "https://github.com/torvalds/subsurface.git")

	orphans, err := FindOrphans(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, o := range orphans {
		if o.MatchedAccount != "github-me" || len(o.Candidates) == 0 {
			t.Errorf("%s: matched %q candidates %v", o.RepoKey, o.MatchedAccount, o.Candidates)
		}
		got[o.RepoKey] = o.Confident
	}
	want := map[string]bool{"torvalds/linux": false, "LuisPalacios/mine": true, "torvalds/subsurface": true}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s confident = %v, want %v", k, got[k], v)
		}
	}
}
