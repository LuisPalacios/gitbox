package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPullPreflight_UsesLinkedWorktreeState(t *testing.T) {
	clone, _ := initTestRepo(t)
	work := filepath.Join(t.TempDir(), "linked")
	runGit(t, clone, "worktree", "add", "-b", "linked", work)
	out, err := outputContext(t.Context(), work, "rev-parse", "--absolute-git-dir")
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(strings.TrimSpace(out), "rebase-merge")
	if err := os.Mkdir(marker, 0o700); err != nil {
		t.Fatal(err)
	}
	if skip, _, err := PullPreflightContext(t.Context(), work); err != nil || skip != PullInProgress {
		t.Fatalf("linked worktree preflight = %q, %v; want %q", skip, err, PullInProgress)
	}
	if skip, _, err := PullPreflightContext(t.Context(), clone); err != nil || skip != "" {
		t.Fatalf("main worktree preflight = %q, %v; want clear", skip, err)
	}
}

func TestPullPreflight_RejectsIncompleteStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stdout string
	}{
		{"missing status", ""},
		{"partial record", "# branch.head main\x00? uncertain"},
		{"bounded output", "# branch.head main\x00" + strings.Repeat("! ignored\x00", DefaultMaxOutput/10+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{Script: func(req Request, _ func() []byte) fakeReply {
				if req.Args[0] != "--no-optional-locks" {
					t.Fatalf("ran another Git command after invalid status: %v", req.Args)
				}
				return fakeReply{Stdout: tc.stdout}
			}}
			SetRunnerForTest(t, runner)
			skip, _, err := PullPreflightContext(t.Context(), t.TempDir())
			var ge *GitError
			if skip != "" || !errors.As(err, &ge) || ge.Op != "status" {
				t.Fatalf("preflight = %q, %v; want status GitError", skip, err)
			}
		})
	}
}
