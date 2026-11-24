package git

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// PullSkipReason describes a working-tree condition that prevents a pull.
type PullSkipReason string

const (
	PullDirty      PullSkipReason = "dirty"
	PullConflict   PullSkipReason = "conflict"
	PullInProgress PullSkipReason = "inProgress"
)

// PullPreflightContext checks local work immediately before a pull. It reports
// the status it observed so the caller can explain a skip without rescanning.
func PullPreflightContext(ctx context.Context, repoPath string) (PullSkipReason, RepoStatus, error) {
	st, err := readStatus(ctx, repoPath, "--untracked-files=normal", "--ignore-submodules=none", "--ahead-behind")
	if err != nil {
		return "", RepoStatus{}, err
	}
	if st.branch.Branch == "" {
		return "", RepoStatus{}, newGitErrorContext(ctx, "status", errMalformedStatus, nil)
	}
	summary := st.summary()
	if summary.Conflicts > 0 {
		return PullConflict, summary, nil
	}

	// Linked worktrees keep in-progress state in their private Git directory.
	out, err := outputContext(ctx, repoPath, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", RepoStatus{}, err
	}
	dir, ok := strings.CutSuffix(out, "\n")
	if !ok || !filepath.IsAbs(dir) {
		return "", RepoStatus{}, newGitErrorContext(ctx, "rev-parse", errors.New("invalid Git directory"), nil)
	}
	for _, marker := range [...]string{"MERGE_HEAD", "REBASE_HEAD", "rebase-apply", "rebase-merge", "CHERRY_PICK_HEAD", "sequencer", "BISECT_LOG"} {
		if err := ctx.Err(); err != nil {
			return "", RepoStatus{}, newGitErrorContext(ctx, "status", err, nil)
		}
		_, err := os.Lstat(filepath.Join(dir, marker))
		if err == nil {
			return PullInProgress, summary, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", RepoStatus{}, newGitErrorContext(ctx, "status", err, nil)
		}
	}
	if len(st.records) > 0 {
		return PullDirty, summary, nil
	}
	return "", summary, nil
}
