package credential

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
)

// TestWriteTempStoreFile: the temporary store holds the one credential line,
// 0600 in a 0700 directory, and cleanup removes both.
func TestWriteTempStoreFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	acct := config.Account{Provider: "gitea", URL: "https://forge.example.invalid", Username: "alice"}
	path, cleanup, err := WriteTempStoreFile(acct, "tok/en")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "https://alice:tok%2Fen@forge.example.invalid\n" {
		t.Fatalf("store file = %q, %v", data, err)
	}
	if runtime.GOOS != "windows" {
		for p, mode := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
			info, err := os.Stat(p)
			if err != nil || info.Mode().Perm() != mode {
				t.Errorf("%s: mode %v (%v), want %o", p, info.Mode().Perm(), err, mode)
			}
		}
	}
	cleanup()
	cleanup()
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the temporary directory is still there after cleanup: %v", err)
	}
}
