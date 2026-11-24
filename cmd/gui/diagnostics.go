//go:build !gitboxserver

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/diag"
	"github.com/LuisPalacios/gitbox/pkg/git"
	"github.com/LuisPalacios/gitbox/pkg/pathkey"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

var saveDiagnosticsFile = wailsrt.SaveFileDialog

func (a *App) PreviewDiagnostics() (string, error) {
	if a.remoteWindow() {
		return "", errors.New("diagnostics.localOnly")
	}
	ctx, cancel := context.WithTimeout(a.refreshContext(), 2*time.Second)
	defer cancel()
	gitResult, err := git.Run(ctx, git.Request{Args: []string{"--version"}})
	var gitVersion string
	if err == nil {
		gitVersion = string(gitResult.Stdout)
	}

	now := time.Now()
	state := diag.State{
		AppVersion: version,
		Commit:     commit,
		GitVersion: gitVersion,
		Activity:   &a.diagnostics,
		StartupMS:  a.GetStartupTiming(),
	}
	for _, task := range a.GetScheduledTasks() {
		snapshot := diag.Task{Name: task.Name, InFlight: task.InFlight, LastResult: task.LastResult}
		if task.NextAt != 0 {
			next := max(0, task.NextAt-now.UnixMilli())
			snapshot.NextInMS = &next
		}
		state.Scheduler = append(state.Scheduler, snapshot)
	}
	a.mu.Lock()
	state.Config = a.cfg
	bundle := diag.Collect(state, now)
	a.mu.Unlock()
	document, err := bundle.Document()
	a.diagnosticsMu.Lock()
	a.diagnosticsPreview = document
	a.diagnosticsMu.Unlock()
	return document, err
}

// ExportDiagnostics saves the preview already held by this window. New
// background activity cannot change the bytes while the user reviews them.
func (a *App) ExportDiagnostics(reviewed string) (bool, error) {
	if a.remoteWindow() {
		return false, errors.New("diagnostics.localOnly")
	}
	a.diagnosticsMu.Lock()
	document := a.diagnosticsPreview
	a.diagnosticsMu.Unlock()
	if document == "" || document != reviewed {
		return false, errors.New("diagnostics.previewRequired")
	}
	path, err := saveDiagnosticsFile(a.ctx, wailsrt.SaveDialogOptions{
		DefaultFilename: "gitbox-diagnostics.json",
		Filters:         recipeFilter,
	})
	if err != nil {
		return false, errors.New("diagnostics.fileError")
	}
	if path == "" {
		return false, nil
	}
	if pathkey.Compare(path) == pathkey.Compare(a.cfgPath) {
		return false, errors.New("diagnostics.fileError")
	}
	if target, err := os.Stat(path); err == nil {
		if active, err := os.Stat(a.cfgPath); err == nil && os.SameFile(target, active) {
			return false, errors.New("diagnostics.fileError")
		}
	}
	if err := writeDiagnostics(path, document); err != nil {
		return false, errors.New("diagnostics.fileError")
	}
	return true, nil
}

func writeDiagnostics(path, document string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".gitbox-diagnostics-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := io.WriteString(f, document)
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
