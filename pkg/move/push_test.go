package move

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/git/testutil"
)

// TestMirrorPushKeepsTheTokenOffArgv: the push --mirror of a move to a token
// destination names the plain URL and reads the token from a store file of
// its own, 0600, which is gone once the move ends (#242).
func TestMirrorPushKeepsTheTokenOffArgv(t *testing.T) {
	cfg, req, _ := moveFixture(t)
	real := git.Default
	var pushArgs []string
	var storeFile, stored string
	git.SetRunnerForTest(t, testutil.RunnerFunc(func(ctx context.Context, r git.Request) (git.Result, error) {
		if !slices.Contains(r.Args, "--mirror") {
			if len(r.Args) > 0 && r.Args[0] == "fetch" {
				return git.Result{}, nil
			}
			return real.Run(ctx, r)
		}
		pushArgs = slices.Clone(r.Args)
		for _, arg := range r.Args {
			if path, ok := strings.CutPrefix(arg, "credential.helper=store --file '"); ok {
				storeFile = strings.TrimSuffix(path, "'")
			}
		}
		data, err := os.ReadFile(filepath.FromSlash(storeFile))
		if err != nil {
			t.Errorf("the store file is not there during the push: %v", err)
		}
		stored = string(data)
		if info, err := os.Stat(filepath.FromSlash(storeFile)); err == nil && runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Errorf("store file mode %o, want 600", info.Mode().Perm())
		}
		return git.Result{}, nil
	}))
	res := Move(t.Context(), cfg, req, noCommit, nil)
	if res.Err != nil {
		t.Fatalf("move: %v", res.Err)
	}
	if pushArgs == nil {
		t.Fatal("no push --mirror ran")
	}
	if joined := strings.Join(pushArgs, " "); strings.Contains(joined, "dst-token") || strings.Contains(joined, "@dst.example.invalid") {
		t.Errorf("push argv carries the credential: %q", pushArgs)
	}
	if !slices.Contains(pushArgs, "https://dst.example.invalid/alice/widget.git") || !slices.Contains(pushArgs, "credential.helper=") {
		t.Errorf("push argv = %q, want the plain URL after a reset helper list", pushArgs)
	}
	if stored != "https://alice:dst-token@dst.example.invalid\n" {
		t.Errorf("store file = %q", stored)
	}
	if _, err := os.Stat(filepath.Dir(filepath.FromSlash(storeFile))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the temporary store is still on disk after the move: %v", err)
	}
}
