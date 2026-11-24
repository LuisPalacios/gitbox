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
	summary, err := PullStatusContext(ctx, repoPath)
	if err != nil {
		return "", RepoStatus{}, err
	}
	if summary.Branch == "" {
		return "", RepoStatus{}, newGitErrorContext(ctx, "status", errMalformedStatus, nil)
	}
	if PullWorkReason(summary, false) == PullConflict {
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
	inProgress, err := pullInProgress(ctx, dir)
	if err != nil {
		return "", RepoStatus{}, newGitErrorContext(ctx, "status", err, nil)
	}
	return PullWorkReason(summary, inProgress), summary, nil
}

// PullStatusContext reads the working tree flags required for pull eligibility.
func PullStatusContext(ctx context.Context, repoPath string) (RepoStatus, error) {
	st, err := readStatus(ctx, repoPath, "--untracked-files=normal", "--ignore-submodules=none", "--ahead-behind")
	if err != nil {
		return RepoStatus{}, err
	}
	return st.summary(), nil
}

// PullWorkReason classifies working-tree evidence shared by status and pull.
func PullWorkReason(st RepoStatus, inProgress bool) PullSkipReason {
	switch {
	case st.Conflicts > 0:
		return PullConflict
	case inProgress:
		return PullInProgress
	case st.Changed > 0 || st.Untracked > 0:
		return PullDirty
	default:
		return ""
	}
}

// StatusPullWorkReason augments a status read with operation markers, without
// starting another Git process.
func StatusPullWorkReason(ctx context.Context, repoPath string, st RepoStatus) (PullSkipReason, error) {
	dir := filepath.Join(repoPath, ".git")
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		pointer, err := os.ReadFile(dir)
		if err != nil {
			return "", err
		}
		target, ok := strings.CutPrefix(strings.TrimSpace(string(pointer)), "gitdir: ")
		if !ok || target == "" {
			return "", errors.New("invalid Git directory")
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(repoPath, target)
		}
		dir = target
	}
	inProgress, err := pullInProgress(ctx, dir)
	if err != nil {
		return "", err
	}
	return PullWorkReason(st, inProgress), nil
}

func pullInProgress(ctx context.Context, dir string) (bool, error) {
	for _, marker := range [...]string{"MERGE_HEAD", "REBASE_HEAD", "rebase-apply", "rebase-merge", "CHERRY_PICK_HEAD", "sequencer", "BISECT_LOG"} {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		_, err := os.Lstat(filepath.Join(dir, marker))
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}
