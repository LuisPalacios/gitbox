//go:build !gitboxserver

package main

import (
	"context"
	"testing"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// TestPickFolderRefusedInRemote: a remote window never opens the native
// folder dialog, which would pick a folder of this computer instead of the
// host's; a local window still does (#241).
func TestPickFolderRefusedInRemote(t *testing.T) {
	opened := 0
	prev := openDirectoryDialog
	openDirectoryDialog = func(context.Context, wailsrt.OpenDialogOptions) (string, error) {
		opened++
		return "/picked", nil
	}
	t.Cleanup(func() { openDirectoryDialog = prev })

	h := startHostile(t, nil)
	if got := h.app.PickFolder("x"); got != "" || opened != 0 {
		t.Errorf("a remote window picked %q after %d dialogs", got, opened)
	}
	local := &App{}
	if got := local.PickFolder("x"); got != "/picked" || opened != 1 {
		t.Errorf("a local window picked %q after %d dialogs", got, opened)
	}
}
