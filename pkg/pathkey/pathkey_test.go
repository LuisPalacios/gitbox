package pathkey

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathIdentity(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "Fleet"), filepath.Join(dir, "fleet")
	if err := os.Mkdir(a, 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(a)
	if err != nil {
		t.Fatal(err)
	}
	alt, err := os.Stat(b)
	folds := err == nil && os.SameFile(info, alt)
	if !folds {
		if err := os.Mkdir(b, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if (Compare(a) == Compare(b)) != folds {
		t.Fatalf("case comparison disagrees with filesystem (folds=%t)", folds)
	}
	if key := Of(a); key.Original != a || key.Access != Exists || !filepath.IsAbs(key.Cmp) {
		t.Fatalf("existing = %+v", key)
	}
	child := filepath.Join(a, "future", "new")
	if key := Of(child); key.Original != child || key.Access != Missing || key.Cmp == "" {
		t.Fatalf("missing = %+v", key)
	}
	if (Compare(child) == Compare(filepath.Join(b, "future", "new"))) != folds {
		t.Fatalf("missing child policy disagrees with its base")
	}
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	if Compare("~/Fleet") != Compare(a) {
		t.Fatal("home prefix mismatch")
	}
	t.Run("symlinked existing prefix", func(t *testing.T) {
		link := filepath.Join(dir, "alias")
		if err := os.Symlink(a, link); err != nil && runtime.GOOS == "windows" {
			t.Skip(err)
		} else if err != nil {
			t.Fatal(err)
		}
		if Compare(filepath.Join(link, "future", "new")) != Compare(child) {
			t.Fatal("missing tail through a link has another identity")
		}
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(dir, "absent"), link); err != nil {
			t.Fatal(err)
		}
		if key := Of(filepath.Join(link, "future")); key.Access != Unknown {
			t.Fatalf("dangling prefix = %+v", key)
		}
	})
}
