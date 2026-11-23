package update

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestExtractUpdateLimits(t *testing.T) {
	for _, tc := range []struct {
		name, entry string
		mode        os.FileMode
		count       int
		size        uint64
	}{
		{"parent", "../outside", 0600, 1, 0},
		{"symlink", "link", os.ModeSymlink | 0777, 1, 0},
		{"entries", "file", 0600, maxArchiveEntries + 1, 0},
		{"size", "large", 0600, 1, 600 << 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := os.Create(filepath.Join(t.TempDir(), "update.zip"))
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(file)
			for i := 0; i < tc.count; i++ {
				// Raw headers let the size-limit test run without allocating the advertised payload.
				h := &zip.FileHeader{Name: fmt.Sprintf("%s%d", tc.entry, i), UncompressedSize64: tc.size, Method: zip.Store}
				h.SetMode(tc.mode)
				if _, err := z.CreateRaw(h); err != nil {
					t.Fatal(err)
				}
			}
			if err := errors.Join(z.Close(), file.Close()); err != nil {
				t.Fatal(err)
			}
			dir, err := extractZip(file.Name())
			if !errors.Is(err, ErrInvalidArchive) || dir != "" {
				t.Fatalf("extract = %q, %v", dir, err)
			}
		})
	}
}

func installFixture(t *testing.T, bundle bool) (exe, dst, cfg string) {
	t.Helper()
	install, extract, cfg := t.TempDir(), t.TempDir(), t.TempDir()
	name := "GitboxApp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	inner := ""
	if bundle {
		name = "GitboxApp.app"
		inner = filepath.Join("Contents", "MacOS", "GitboxApp")
	}
	dst = filepath.Join(install, name)
	exe = filepath.Join(dst, inner)
	writeFile(t, exe, "old version")
	writeFile(t, filepath.Join(extract, name, inner), "new version")
	if err := InstallVersion(extract, install, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	return exe, dst, cfg
}

func TestRecoveryInterruptedLaunchRestoresBackup(t *testing.T) {
	for _, bundle := range []bool{false, true} {
		t.Run(fmt.Sprintf("bundle=%v", bundle), func(t *testing.T) {
			exe, dst, cfg := installFixture(t, bundle)
			r, err := BeginRecovery(exe, cfg)
			if err != nil || r == nil || r.OfferedVersion() != "" {
				t.Fatalf("first launch: %v, %v", r, err)
			}
			again, err := BeginRecovery(exe, cfg)
			if err != nil || again.OfferedVersion() != "v1.2.3" {
				t.Fatalf("interrupted launch: %v, %v", again, err)
			}
			if err := again.Finish(true); err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, exe); got != "old version" {
				t.Fatalf("restored %q", got)
			}
			if _, err := os.Stat(dst + ".update-state.json"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("remaining record: %v", err)
			}
		})
	}
}

func TestRecoveryHealthyLaunchAndExpiry(t *testing.T) {
	for _, expiry := range []bool{false, true} {
		t.Run(fmt.Sprintf("expired=%v", expiry), func(t *testing.T) {
			exe, dst, cfg := installFixture(t, false)
			state, err := readUpdateState(dst)
			if err != nil {
				t.Fatal(err)
			}
			if expiry {
				state.Created = time.Now().Add(-8 * 24 * time.Hour)
				if err := os.Remove(dst + ".update-state.json"); err != nil {
					t.Fatal(err)
				}
				if err := writeUpdateState(dst+".update-state.json", state); err != nil {
					t.Fatal(err)
				}
			}
			r, err := BeginRecovery(exe, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !expiry {
				if err := r.Confirm(); err != nil {
					t.Fatal(err)
				}
				r, err = BeginRecovery(exe, cfg)
				if err != nil {
					t.Fatal(err)
				}
			}
			if r.OfferedVersion() != "" || !r.NeedsCleanup() {
				t.Fatal("expected cleanup without rollback offer")
			}
			if err := r.Finish(false); err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, exe); got != "new version" {
				t.Fatalf("installed %q", got)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(dst), state.Old)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("remaining backup: %v", err)
			}
		})
	}
}

func TestRecoveryRejectsChangedBackup(t *testing.T) {
	exe, dst, cfg := installFixture(t, false)
	if _, err := BeginRecovery(exe, cfg); err != nil {
		t.Fatal(err)
	}
	r, err := BeginRecovery(exe, cfg)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(filepath.Dir(dst), r.state.Old), "different backup")
	if err := r.Finish(true); err == nil {
		t.Fatal("restored a changed backup")
	}
	if got := readFile(t, exe); got != "new version" {
		t.Fatalf("replaced install: %q", got)
	}
}

func TestRecoveryIncompleteTransactions(t *testing.T) {
	for _, phase := range []string{"prepared", "linked", "cleanup", "foreign"} {
		t.Run(phase, func(t *testing.T) {
			exe, dst, cfg := installFixture(t, false)
			st, err := readUpdateState(dst)
			if err != nil {
				t.Fatal(err)
			}
			old := filepath.Join(filepath.Dir(dst), st.Old)
			if phase == "prepared" || phase == "linked" {
				writeFile(t, exe, "old version")
			}
			switch phase {
			case "prepared", "cleanup":
				if err := os.Remove(old); err != nil {
					t.Fatal(err)
				}
			case "foreign":
				writeFile(t, old, "unrelated")
			}
			r, err := BeginRecovery(exe, cfg)
			if phase == "foreign" {
				if err == nil || readFile(t, old) != "unrelated" {
					t.Fatal("touched unrelated backup")
				}
				return
			}
			if err != nil || r != nil {
				t.Fatalf("reconcile: %v, %v", r, err)
			}
			if _, err := os.Stat(dst + ".update-state.json"); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("left obsolete marker")
			}
			if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("left interrupted backup")
			}
		})
	}
}
