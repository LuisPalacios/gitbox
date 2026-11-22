package update

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestWindowsExecutable_StageFailureKeepsCurrentFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		copy func(io.Writer, io.Reader) (int64, error)
	}{
		{"partial", func(dst io.Writer, _ io.Reader) (int64, error) {
			n, _ := io.WriteString(dst, "part")
			return int64(n), errReplaceInjected
		}},
		{"wrong-size", func(dst io.Writer, _ io.Reader) (int64, error) {
			_, err := io.WriteString(dst, "short")
			return int64(len("new version")), err
		}},
		{"wrong-hash", func(dst io.Writer, _ io.Reader) (int64, error) {
			n, err := io.WriteString(dst, "bad version")
			return int64(n), err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "GitboxApp.exe")
			src := filepath.Join(t.TempDir(), "GitboxApp.exe")
			writeFile(t, dst, "old version")
			writeFile(t, src, "new version")
			ops := replaceOps{
				copyFile: tc.copy,
				moveFile: func(_, _ string) error {
					t.Fatal("staging failure attempted to move the installed binary")
					return nil
				},
			}
			if err := replaceWindowsExecutable(src, dst, ops); err == nil {
				t.Fatal("replace succeeded with an incomplete or corrupt copy")
			}
			if got := readFile(t, dst); got != "old version" {
				t.Fatalf("previous version changed to %q", got)
			}
			if got := names(t, filepath.Dir(dst)); !slices.Equal(got, []string{"GitboxApp.exe"}) {
				t.Fatalf("staging left files behind: %v", got)
			}
		})
	}
}

func TestWindowsExecutable_RollbackPreservesRunnableBackup(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "GitboxApp.exe")
	src := filepath.Join(t.TempDir(), "GitboxApp.exe")
	writeFile(t, dst, "old version")
	writeFile(t, src, "new version")
	ops := replaceOps{moveFile: func(from, to string) error {
		if to == dst {
			return errReplaceInjected
		}
		return os.Rename(from, to)
	}}
	err := replaceWindowsExecutable(src, dst, ops)
	if !errors.Is(err, errUpdateRollback) || !errors.Is(err, errReplaceInjected) {
		t.Fatalf("replace error = %v, want both install and restore failures", err)
	}
	if got := readFile(t, dst+".old"); got != "old version" {
		t.Fatalf("backup has %q, want the old version", got)
	}
	cleanupWindowsBackup(dst)
	if got := names(t, filepath.Dir(dst)); !slices.Equal(got, []string{"GitboxApp.exe.old", "GitboxApp.exe.update-state.json"}) {
		t.Fatalf("rollback evidence changed: %v", got)
	}
}

func TestWindowsExecutable_ExclusiveUpdateSlots(t *testing.T) {
	for _, tail := range []string{".new", ".update-state.json"} {
		t.Run(tail, func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "GitboxApp.exe")
			src := filepath.Join(t.TempDir(), "GitboxApp.exe")
			writeFile(t, dst, "old version")
			writeFile(t, src, "new version")
			writeFile(t, dst+tail, "existing file")
			ops := replaceOps{moveFile: func(_, _ string) error {
				t.Fatal("replacing an executable with an occupied update slot")
				return nil
			}}
			if err := replaceWindowsExecutable(src, dst, ops); err == nil {
				t.Fatal("replace succeeded with an occupied update slot")
			}
			if got := readFile(t, dst+tail); got != "existing file" {
				t.Errorf("occupied slot holds %q, want existing file", got)
			}
			if got := readFile(t, dst); got != "old version" {
				t.Fatalf("previous version changed to %q", got)
			}
		})
	}
}

func TestWindowsExecutable_CleanupHonorsTheRecordedBackup(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "GitboxApp.exe")
	src := filepath.Join(t.TempDir(), "GitboxApp.exe")
	other := filepath.Join(filepath.Dir(dst), "other.old")
	marker := dst + ".update-state.json"
	writeFile(t, dst, "old version")
	writeFile(t, src, "new version")
	writeFile(t, other, "unrelated")
	if err := replaceWindowsExecutable(src, dst, replaceOps{moveFile: os.Rename}); err != nil {
		t.Fatal(err)
	}
	original := readFile(t, marker)
	writeFile(t, dst, "unexpected running version")
	cleanupWindowsBackup(dst)
	if got := readFile(t, dst+".old"); got != "old version" {
		t.Fatalf("unexpected version changed backup to %q", got)
	}
	writeFile(t, dst, "new version")
	writeFile(t, marker, strings.Replace(original, "GitboxApp.exe.old", "../other.old", 1))
	cleanupWindowsBackup(dst)
	if got := readFile(t, dst+".old"); got != "old version" {
		t.Fatalf("invalid record changed backup to %q", got)
	}
	writeFile(t, marker, original)
	writeFile(t, dst+".old", "substituted backup")
	cleanupWindowsBackup(dst)
	if got := readFile(t, dst+".old"); got != "substituted backup" {
		t.Fatalf("cleanup touched a different backup: %q", got)
	}
	writeFile(t, dst+".old", "old version")
	writeFile(t, dst+".new", "another update in progress")
	cleanupWindowsBackup(dst)
	if got := readFile(t, dst+".old"); got != "old version" {
		t.Fatalf("cleanup interfered with another update and changed the backup to %q", got)
	}
	if err := os.Remove(dst + ".new"); err != nil {
		t.Fatal(err)
	}
	cleanupWindowsBackup(dst)
	cleanupWindowsBackup(dst)
	if got := names(t, filepath.Dir(dst)); !slices.Equal(got, []string{"GitboxApp.exe", "other.old"}) {
		t.Fatalf("cleanup left or removed the wrong files: %v", got)
	}
	if got := readFile(t, other); got != "unrelated" {
		t.Fatalf("unrelated file changed to %q", got)
	}
}

func TestWindowsExecutable_PreservesLegacyBackup(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "GitboxApp.exe")
	src := filepath.Join(t.TempDir(), "GitboxApp.exe")
	old := dst + ".old"
	marker := dst + ".update-state.json"
	writeFile(t, dst, "installed version")
	writeFile(t, src, "new version")
	writeFile(t, old, "legacy version")

	if err := replaceWindowsExecutable(src, dst, replaceOps{moveFile: os.Rename}); err != nil {
		t.Fatal(err)
	}
	var state windowsUpdateState
	if err := json.Unmarshal([]byte(readFile(t, marker)), &state); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(state.Old, filepath.Base(old)+".") {
		t.Fatalf("backup record chose %q, want a distinct sibling", state.Old)
	}
	recorded := filepath.Join(filepath.Dir(dst), state.Old)
	if got := readFile(t, recorded); got != "installed version" {
		t.Fatalf("recorded backup contains %q, want installed version", got)
	}
	if got := readFile(t, dst); got != "new version" {
		t.Fatalf("install contains %q, want new version", got)
	}
	cleanupWindowsBackup(dst)
	if got := names(t, filepath.Dir(dst)); !slices.Equal(got, []string{"GitboxApp.exe", "GitboxApp.exe.old"}) {
		t.Fatalf("cleanup left or removed the wrong files: %v", got)
	}
	if got := readFile(t, old); got != "legacy version" {
		t.Fatalf("unrecorded backup contains %q, want legacy version", got)
	}
}
