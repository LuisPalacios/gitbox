package move

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/git/testutil"
	"github.com/LuisPalacios/gitbox/pkg/provider"
)

// fakeGitea answers the provider calls of a move: no destination yet, a
// create and a delete that succeed. It records the methods it saw.
type fakeGitea struct {
	mu      sync.Mutex
	methods []string
}

func (f *fakeGitea) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.methods = append(f.methods, req.Method)
	f.mu.Unlock()
	status, body := http.StatusOK, `{"login":"alice","username":"alice"}`
	switch {
	case req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/api/v1/repos/"):
		status, body = http.StatusNotFound, `{}`
	case req.Method == http.MethodPost:
		status, body = http.StatusCreated, `{}`
	case req.Method == http.MethodDelete:
		status, body = http.StatusNoContent, ``
	}
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func (f *fakeGitea) saw(method string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.methods, method)
}

// moveFixture is a clean clone with an upstream, two token accounts and a
// git runner that answers the fetch and the push --mirror itself: the
// destination does not exist outside the test.
func moveFixture(t *testing.T) (*config.Config, Request, *fakeGitea) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GITBOX_TOKEN_SRC", "src-token")
	t.Setenv("GITBOX_TOKEN_DST", "dst-token")

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	gitRun := func(args ...string) {
		t.Helper()
		if _, err := git.Default.Run(t.Context(), git.Request{Dir: dir, Args: args}); err != nil {
			t.Fatalf("git %s: %v", strings.Join(args, " "), err)
		}
	}
	gitRun("init", "-q", "-b", "main", src)
	if err := os.WriteFile(filepath.Join(src, "README"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun("-C", src, "add", "README")
	gitRun("-C", src, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "one")
	gitRun("clone", "-q", "--bare", src, filepath.Join(dir, "up.git"))
	gitRun("-C", src, "remote", "add", "origin", filepath.Join(dir, "up.git"))
	gitRun("-C", src, "fetch", "-q", "origin")
	gitRun("-C", src, "branch", "-q", "-u", "origin/main")

	real := git.Default
	git.SetRunnerForTest(t, testutil.RunnerFunc(func(ctx context.Context, req git.Request) (git.Result, error) {
		if slices.Contains(req.Args, "--mirror") || (len(req.Args) > 0 && req.Args[0] == "fetch") {
			return git.Result{}, ctx.Err()
		}
		return real.Run(ctx, req)
	}))
	fake := &fakeGitea{}
	provider.SetDoerForTest(t, fake)

	cfg := &config.Config{
		Accounts: map[string]config.Account{
			"src": {Provider: "gitea", URL: "https://src.example.invalid", Username: "alice", DefaultCredentialType: "token"},
			"dst": {Provider: "gitea", URL: "https://dst.example.invalid", Username: "alice", DefaultCredentialType: "token"},
		},
		Sources: map[string]config.Source{
			"src": {Account: "src", Repos: map[string]config.Repo{"alice/widget": {}}},
		},
	}
	req := Request{
		SourceAccountKey: "src", SourceSourceKey: "src", SourceRepoKey: "alice/widget", SourceRepoPath: src,
		DestAccountKey: "dst", DestOwner: "alice", DestRepoName: "widget",
		DeleteSourceRemote: true, DeleteLocalClone: true,
	}
	return cfg, req, fake
}

func noCommit(change func(*config.Config) error) (*config.Config, error) {
	cfg := &config.Config{Accounts: map[string]config.Account{}, Sources: map[string]config.Source{}}
	return cfg, change(cfg)
}

// TestMoveCancelledStopsBeforeDeleting: a move cancelled once its origin
// points at the destination still records the new home, but never deletes
// the source repository or the local clone.
func TestMoveCancelledStopsBeforeDeleting(t *testing.T) {
	cfg, req, fake := moveFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var phases []Phase
	res := Move(ctx, cfg, req, noCommit, func(p Progress) {
		phases = append(phases, p.Phase)
		if p.Phase == PhaseUpdateConfig {
			cancel()
		}
	})
	if !errors.Is(res.Err, context.Canceled) {
		t.Fatalf("Err = %v, want the cancellation", res.Err)
	}
	if !res.DestRepoCreated || res.NewOrigin == "" {
		t.Errorf("the phases before the cancel did not complete: %+v", res)
	}
	if fake.saw(http.MethodDelete) {
		t.Error("a cancelled move deleted the source repository")
	}
	if res.SourceRemoteDeleted || res.LocalCloneDeleted || !git.IsRepo(req.SourceRepoPath) {
		t.Errorf("a cancelled move deleted something: %+v", res)
	}
	if slices.Contains(phases, PhaseDone) {
		t.Errorf("phases %v: a cancelled move reported done", phases)
	}
}

// TestMoveCancelledBeforeCreating: a move cancelled as the destination is
// about to be created creates nothing.
func TestMoveCancelledBeforeCreating(t *testing.T) {
	cfg, req, fake := moveFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	res := Move(ctx, cfg, req, noCommit, func(p Progress) {
		if p.Phase == PhaseCreateDest {
			cancel()
		}
	})
	if !errors.Is(res.Err, context.Canceled) {
		t.Fatalf("Err = %v, want the cancellation", res.Err)
	}
	if res.DestRepoCreated || fake.saw(http.MethodPost) {
		t.Errorf("a move cancelled before create_dest created the destination: %+v", res)
	}
}
