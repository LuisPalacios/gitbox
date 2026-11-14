package status

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

func FuzzRepoPath(f *testing.F) {
	// Native absolute roots keep the seeds active on Windows as well as POSIX.
	base := f.TempDir()
	root, other := filepath.Join(base, "first"), filepath.Join(base, "second")
	for pin := range uint8(3) {
		f.Add(root, other, "", "", "", pin)
		f.Add(root, other, "nested/source", "identity/subgroup", "nested/clone", pin)
		f.Add(root, other, ".", ".", "clone", pin)
		f.Add(root, other, "source", "identity", filepath.Join(base, "outside"), pin)
		f.Add(root, other, "source", "identity", "../outside/clone", pin)
	}
	f.Fuzz(func(t *testing.T, root, other, sourceFolder, idFolder, cloneFolder string, pin uint8) {
		// This is the approved lexical input domain, not config validation:
		// absolute roots and safe relative components, plus the two explicit
		// clone overrides. Arbitrary accepted configs can escape these roots.
		if !filepath.IsAbs(root) || !filepath.IsAbs(other) ||
			strings.ContainsRune(root, 0) || strings.ContainsRune(other, 0) ||
			!fuzzSafeRelative(sourceFolder) || !fuzzSafeRelative(idFolder) {
			return
		}
		absoluteOverride := filepath.IsAbs(cloneFolder)
		parentOverride := strings.HasPrefix(cloneFolder, "../")
		switch {
		case absoluteOverride:
			if strings.ContainsRune(cloneFolder, 0) {
				return
			}
		case parentOverride:
			if !fuzzSafeRelative(strings.TrimPrefix(cloneFolder, "../")) {
				return
			}
		default:
			if !fuzzSafeRelative(cloneFolder) {
				return
			}
		}

		source := config.Source{Folder: sourceFolder}
		repo := config.Repo{IdFolder: idFolder, CloneFolder: cloneFolder}
		selectedRoot := root
		switch pin % 3 {
		case 1:
			source.Root = other
			selectedRoot = other
		case 2:
			source.Root = other
			repo.Root = root
		}
		cfg := &config.Config{
			Global:  config.GlobalConfig{Roots: []string{root, other}},
			Sources: map[string]config.Source{"source": source},
		}
		got := RepoPath(cfg, "source", "org/repo", repo)
		if !filepath.IsAbs(got) {
			t.Fatalf("RepoPath = %q, want an absolute path", got)
		}
		switch {
		case absoluteOverride:
			if got != cloneFolder {
				t.Fatalf("absolute override = %q, want %q", got, cloneFolder)
			}
		case parentOverride:
			// The documented ../ override starts at the selected root's
			// parent, independent of source and identity folders.
			want := filepath.Join(filepath.Dir(filepath.Clean(selectedRoot)), strings.TrimPrefix(cloneFolder, "../"))
			if got != want {
				t.Fatalf("parent override = %q, want %q", got, want)
			}
		default:
			rel, err := filepath.Rel(selectedRoot, got)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
				t.Fatalf("RepoPath %q escaped selected root %q (relative %q, error %v)", got, selectedRoot, rel, err)
			}
		}
	})
}

// fuzzSafeRelative accepts empty overrides and local paths without parent
// segments, backslashes, NUL or a leading tilde. It never inspects the disk.
func fuzzSafeRelative(path string) bool {
	if path == "" {
		return true
	}
	if !filepath.IsLocal(path) || strings.ContainsAny(path, "\\\x00") || strings.HasPrefix(path, "~") {
		return false
	}
	for part := range strings.SplitSeq(path, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}
