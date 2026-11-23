package main

import (
	"crypto/rand"
	"os"
	"slices"

	"github.com/LuisPalacios/gitbox/pkg/git"
)

// pendingSweep binds one displayed selection to the clone and tips that
// produced it. A new preview or a server reconnect expires the selection.
type pendingSweep struct {
	source, repo, id string
	folder           os.FileInfo
	result           git.SweepResult
}

// SweepPreviewDTO holds the read-only preview of stale branches.
type SweepPreviewDTO struct {
	ID       string   `json:"id"`
	Merged   []string `json:"merged"`
	Gone     []string `json:"gone"`
	Squashed []string `json:"squashed"`
	Error    string   `json:"error,omitempty"`
}

// SweepDeleteDTO holds the result of actually deleting branches.
type SweepDeleteDTO struct {
	Skipped []string `json:"skipped"`
	Deleted []string `json:"deleted"`
	Error   string   `json:"error,omitempty"`
}

// PreviewSweep scans for stale branches without deleting anything.
func (a *App) PreviewSweep(sourceKey, repoKey string) SweepPreviewDTO {
	return a.be().PreviewSweep(sourceKey, repoKey)
}

// previewSweepLocal is the local backend's PreviewSweep.
func (a *App) previewSweepLocal(sourceKey, repoKey string) SweepPreviewDTO {
	// Expire the previous request before scanning, even if this one fails.
	request := &pendingSweep{source: sourceKey, repo: repoKey}
	a.mu.Lock()
	a.sweepPreview = request
	a.mu.Unlock()
	path, err := a.resolveRepoPath(sourceKey, repoKey)
	if err != nil {
		return SweepPreviewDTO{Error: userMsg(err)}
	}
	folder, err := os.Stat(path)
	if err != nil {
		return SweepPreviewDTO{Error: userMsg(err)}
	}
	result, err := git.SweepBranches(path)
	if err != nil {
		return SweepPreviewDTO{Error: userMsg(err)}
	}
	preview := SweepPreviewDTO{Merged: slices.Clone(result.Merged), Gone: slices.Clone(result.Gone), Squashed: slices.Clone(result.Squashed)}
	if len(result.Tips) > 0 {
		a.mu.Lock()
		if a.sweepPreview == request {
			preview.ID = rand.Text()
			request.id, request.result, request.folder = preview.ID, result, folder
		}
		a.mu.Unlock()
	}
	return preview
}

// ConfirmSweep deletes the stale branches found by PreviewSweep.
func (a *App) ConfirmSweep(sourceKey, repoKey string, selection SweepPreviewDTO) SweepDeleteDTO {
	return a.be().ConfirmSweep(sourceKey, repoKey, selection)
}

// confirmSweepLocal is the local backend's ConfirmSweep.
func (a *App) confirmSweepLocal(sourceKey, repoKey string, selection SweepPreviewDTO) SweepDeleteDTO {
	path, err := a.resolveRepoPath(sourceKey, repoKey)
	if err != nil {
		return SweepDeleteDTO{Error: userMsg(err)}
	}
	folder, err := os.Stat(path)
	if err != nil {
		return SweepDeleteDTO{Error: userMsg(err)}
	}
	a.mu.Lock()
	pending := a.sweepPreview
	if selection.ID == "" || pending == nil || pending.id != selection.ID || pending.source != sourceKey || pending.repo != repoKey || !os.SameFile(pending.folder, folder) ||
		!slices.Equal(pending.result.Gone, selection.Gone) || !slices.Equal(pending.result.Merged, selection.Merged) || !slices.Equal(pending.result.Squashed, selection.Squashed) {
		a.mu.Unlock()
		return SweepDeleteDTO{Skipped: slices.Concat(selection.Gone, selection.Merged, selection.Squashed)}
	}
	a.sweepPreview = nil
	a.mu.Unlock()
	deleted, skipped, errs := git.DeletePreviewedBranches(path, pending.result)
	response := SweepDeleteDTO{Deleted: deleted, Skipped: skipped}
	if len(errs) > 0 {
		response.Error = userMsg(errs[0])
	}
	return response
}
