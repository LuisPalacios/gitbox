package git

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestStatusPreservesPaths(t *testing.T) {
	dir, _ := initTestRepo(t)
	renamed, untracked := "-renamed file.txt", "-untracked file.txt"
	if runtime.GOOS != "windows" {
		renamed, untracked = " renamed\n\".txt ", "-untracked\n\".txt "
	}
	runGit(t, dir, "config", "status.renames", "true")
	runGit(t, dir, "mv", "--", "README.md", renamed)
	writeFile(t, filepath.Join(dir, untracked), "local changes\n")

	summary, err := StatusContext(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	branch, ahead, behind, changes, paths, err := DetailedStatus(dir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != summary.Branch || ahead != summary.Ahead || behind != summary.Behind ||
		summary.Added != 1 || summary.Untracked != 1 || summary.Modified != 0 || summary.Deleted != 0 || summary.Conflicts != 0 {
		t.Fatalf("unexpected summary %+v or detail headers %q, %d, %d", summary, branch, ahead, behind)
	}
	if want := []FileChange{{Kind: "renamed", Path: "README.md → " + renamed}}; !slices.Equal(changes, want) {
		t.Errorf("changes = %+v, want %+v", changes, want)
	}
	if !slices.Equal(paths, []string{untracked}) {
		t.Errorf("untracked = %q, want %q", paths, []string{untracked})
	}
}

func TestStatusUnknownAheadBehind(t *testing.T) {
	parsed, err := parseStatusZ("# branch.upstream origin/main\x00# branch.ab +? -?\x00")
	if err != nil {
		t.Fatal(err)
	}
	if want := (RepoStatus{Upstream: "origin/main"}); parsed.summary() != want {
		t.Errorf("summary = %+v, want %+v", parsed.summary(), want)
	}
}

func TestStatusRejectsMalformedOutput(t *testing.T) {
	cases := map[string]string{
		"no final delimiter": "? canary-output",
		"truncated rename":   "2 R. N... 100644 100644 100644 a b R100 canary-output\x00",
		"missing path":       "1 .M N... 100644 100644 100644 a b \x00",
		"short XY":           "1 M N... 100644 100644 100644 a b canary-output\x00",
		"broken branch":      "# branch.ab +? -canary-output\x00",
		"unknown record":     "x canary-output\x00",
	}
	for name, stdout := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{Script: func(req Request, _ func() []byte) fakeReply {
				want := []string{"--no-optional-locks", "status", "--porcelain=v2", "--branch", "-z"}
				if !slices.Equal(req.Args, want) {
					t.Errorf("command args = %v, want %v", req.Args, want)
				}
				return fakeReply{Stdout: stdout}
			}}
			SetRunnerForTest(t, runner)
			_, summaryErr := StatusContext(context.Background(), t.TempDir())
			_, _, _, _, _, detailsErr := DetailedStatus(t.TempDir())
			for _, err := range []error{summaryErr, detailsErr} {
				var gitErr *GitError
				if !errors.As(err, &gitErr) || gitErr.Code != CodeUnknown || gitErr.Op != "status" {
					t.Fatalf("expected an unknown status GitError, got %v", err)
				}
				if strings.Contains(err.Error(), "canary-output") || gitErr.stderr != "" {
					t.Fatalf("malformed output reached the error: %v", err)
				}
			}
		})
	}
}
