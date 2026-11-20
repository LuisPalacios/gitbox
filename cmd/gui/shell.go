//go:build !gitboxserver

package main

// The local shell: everything on *App that only makes sense on the machine
// that shows the window. These methods call the Wails runtime (window,
// dialogs, quit) and the Wails lifecycle hooks, so this file is excluded
// from the gitbox-server build (-tags gitboxserver). They never go through
// Backend. See classification.go for the full LOCAL list; LOCAL methods
// that need no Wails runtime stay in their original files.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/update"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Startup is called by Wails at application start. It also runs in the
// terminals subprocess.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.events = wailsSink{ctx}
	a.be().Init(ctx)
}

// Shutdown is called by Wails when the app is closing.
func (a *App) Shutdown(_ context.Context) {
	// Tear down the Profile-editor subprocess (issue #69) so the user can't
	// end up with an orphaned terminals window after closing the main app.
	// Only fires in the parent process — the subprocess never spawns its
	// own subprocess, so terminalsCmd stays nil there.
	a.terminalsMu.Lock()
	cmd := a.terminalsCmd
	a.terminalsCmd = nil
	a.terminalsMu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		// Reap to release the OS handle; ignore the error from a process we
		// just killed.
		_, _ = cmd.Process.Wait()
	}
	// Stop the backend's workers before test cleanup removes the temp
	// config dir, so a late pass can't recreate it via config.Save.
	a.be().Close()
	a.toolsWG.Wait()
	if a.testCleanup != nil {
		a.testCleanup()
	}
}

// BeforeClose is called while the window is still alive, before it is destroyed.
// We capture and persist the window position and size to the active view mode slot.
func (a *App) BeforeClose(_ context.Context) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.cfgLoaded {
		return false // don't overwrite a config we failed to load
	}
	x, y := wailsrt.WindowGetPosition(a.ctx)
	w, h := wailsrt.WindowGetSize(a.ctx)
	ws := &config.WindowState{X: x, Y: y, Width: w, Height: h}
	if a.cfg.Global.ViewMode == "compact" {
		a.cfg.Global.CompactWindow = ws
	} else {
		a.cfg.Global.Window = ws
	}
	_ = a.saveConfig()
	return false // don't prevent closing
}

// DomReady is called after the frontend DOM is ready.
// We start hidden, restore saved position, then show the window to prevent flickering.
// If the saved position is off-screen (e.g. monitor disconnected), we center instead.
func (a *App) DomReady(_ context.Context) {
	// Terminals subprocess (issue #69 — see runTerminalsWindow in main.go)
	// only renders the Profile editor and shouldn't spin up the full app's
	// background workers. SyncProfiles still runs so the editor sees a
	// fresh terminal/shell list, but upstream probing,
	// update checks and editor/terminal sync are skipped.
	if a.windowMode == "terminals" {
		// Refresh the detected terminal/shell list before painting so the
		// editor opens with a real table on first show.
		a.SyncProfiles()
		// runTerminalsWindow sets StartHidden so the user doesn't see a
		// dark-empty-webview flash before Svelte paints. We *don't*
		// reveal the window here — DomReady fires before the browser
		// has actually rasterised the first frame, so calling
		// WindowShow here still leaks a flash on Windows + Linux (the
		// macOS WebKit happens to paint synchronously and so it's fine
		// there). Instead, App.svelte calls bridge.showWindow() from
		// onMount after tick() + two requestAnimationFrames, which is
		// the canonical "after paint" point.
		//
		// Safety net: if the frontend bundle ever fails to load (JS
		// error, hung script), the user would be stranded with an
		// invisible window and no way to dismiss it. After 1.5s we
		// reveal anyway — by then either the frontend has already
		// shown it (no-op) or we accept a brief flash to recover from
		// a broken state.
		go func(ctx context.Context) {
			time.Sleep(1500 * time.Millisecond)
			wailsrt.WindowShow(ctx)
		}(a.ctx)
		return
	}

	// Pick the right saved position based on view mode.
	w := a.savedWindowPos
	if a.savedViewMode == "compact" && a.savedCompactPos != nil {
		w = a.savedCompactPos
	}
	if w != nil {
		if a.isPositionOnScreen(w.X, w.Y, w.Width, w.Height) {
			wailsrt.WindowSetPosition(a.ctx, w.X, w.Y)
		} else {
			wailsrt.WindowCenter(a.ctx)
		}
	}
	a.toolsOnce.Do(func() {
		a.toolsWG.Go(func() {
			a.syncDetected()
			a.sink().Emit("tools:updated")
		})
	})
	// Start the backend's background workers (harness watcher, upstream
	// existence probe).
	a.be().Start()

	// Check for updates now and every updateCheckInterval.
	a.startUpdateChecks()

	// In compact mode, delay WindowShow — the frontend calls ShowWindow()
	// after fitCompactHeight() to avoid flickering during resize.
	if a.savedViewMode != "compact" {
		wailsrt.WindowShow(a.ctx)
	}
}

// ShowWindow makes the window visible. Called by the frontend after
// compact mode layout is measured and the window is correctly sized.
func (a *App) ShowWindow() {
	wailsrt.WindowShow(a.ctx)
}

// ApplyUpdate downloads and applies the latest release, then restarts the
// app on the new version. On Windows, if the install directory requires
// admin privileges (e.g. Program Files), it falls back to a UAC-elevated
// helper process, which restarts the app once it has copied the files.
func (a *App) ApplyUpdate() error {
	if !update.SelfUpdateSupported() {
		return errors.New("this build can't update itself; download the new version from the release page")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	opts := a.updateOpts()
	result, err := update.CheckLatest(ctx, opts)
	if err != nil {
		return fmt.Errorf("checking for update: %w", err)
	}
	if !result.Available {
		return fmt.Errorf("no update available")
	}

	a.sink().Emit("update:progress", "Downloading...")

	zipPath, err := update.DownloadRelease(ctx, result.Release, opts)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	defer os.RemoveAll(filepath.Dir(zipPath))

	a.sink().Emit("update:progress", "Applying...")

	// Extract first so we can retry with elevation if the in-place
	// install fails due to permissions (e.g. Program Files on Windows).
	extractDir, installDir, err := update.ExtractUpdate(zipPath)
	if err != nil {
		return fmt.Errorf("extracting: %w", err)
	}

	// Capture what to relaunch before the install replaces the files.
	// Without it the update still installs; the user restarts by hand.
	exe, err := update.SelfPath()
	if err != nil {
		exe = ""
	}

	if err := update.InstallExtracted(extractDir, installDir); err != nil {
		if errors.Is(err, update.ErrNeedElevation) {
			// Don't clean extractDir — the elevated script needs it.
			a.sink().Emit("update:progress", "Requesting admin privileges...")
			if elevErr := update.ApplyElevated(extractDir, installDir, exe); elevErr != nil {
				os.RemoveAll(extractDir)
				return fmt.Errorf("elevated update: %w", elevErr)
			}
			// Elevated script will copy files after we exit.
			a.sink().Emit("update:quit", result.Latest)
			return nil
		}
		os.RemoveAll(extractDir)
		return fmt.Errorf("applying: %w", err)
	}

	os.RemoveAll(extractDir)

	// Start the new version, which waits for this process to exit, then
	// quit. Quit runs BeforeClose, so the window state is saved as usual.
	if exe != "" && update.Relaunch(exe, os.Getpid(), os.Args[1:]) == nil {
		a.sink().Emit("update:restarting", result.Latest)
		go func() {
			time.Sleep(time.Second) // let the pill show "Restarting…"
			wailsrt.Quit(a.ctx)
		}()
		return nil
	}
	a.sink().Emit("update:done", result.Latest)
	return nil
}

// isPositionOnScreen checks whether the saved window position would be visible
// on any connected monitor. On Windows, uses the virtual desktop rectangle
// (covers all monitors). Falls back to Wails primary-screen check on other platforms.
func (a *App) isPositionOnScreen(x, y, w, h int) bool {
	const margin = 100

	// Try platform-specific virtual desktop bounds (all monitors combined).
	if vx, vy, vw, vh, ok := virtualDesktopBounds(); ok {
		// At least margin pixels of the window must overlap the virtual desktop.
		return x+w > vx+margin && y+h > vy+margin && x < vx+vw-margin && y < vy+vh-margin
	}

	// Fallback: use Wails ScreenGetAll (no position info, only sizes).
	screens, err := wailsrt.ScreenGetAll(a.ctx)
	if err != nil || len(screens) == 0 {
		return x >= 0 && y >= 0
	}
	var sw, sh int
	for _, s := range screens {
		if s.IsPrimary {
			sw, sh = s.Size.Width, s.Size.Height
			break
		}
	}
	if sw == 0 || sh == 0 {
		sw, sh = screens[0].Size.Width, screens[0].Size.Height
	}
	return x+w > margin && y+h > margin && x < sw-margin && y < sh-margin
}

// IsPositionOnScreen is the exported binding so the frontend can validate
// a window position before calling WindowSetPosition.
func (a *App) IsPositionOnScreen(x, y, w, h int) bool {
	return a.isPositionOnScreen(x, y, w, h)
}

// PickFolder opens the native OS directory picker and returns the selected path.
// Returns empty string if the user cancels.
func (a *App) PickFolder(title string) string {
	dir, err := wailsrt.OpenDirectoryDialog(a.ctx, wailsrt.OpenDialogOptions{
		Title: title,
	})
	if err != nil {
		return ""
	}
	return dir
}

// SetViewMode saves the view mode and current window position/size to the
// appropriate slot, then switches to the target mode. Returns the target
// mode's previously saved window state (or nil if none was persisted).
func (a *App) SetViewMode(mode string) *WindowStateDTO {
	// Save current window position to the slot we're leaving.
	x, y := wailsrt.WindowGetPosition(a.ctx)
	w, h := wailsrt.WindowGetSize(a.ctx)
	ws := &config.WindowState{X: x, Y: y, Width: w, Height: h}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cfg.Global.ViewMode == "compact" {
		a.cfg.Global.CompactWindow = ws
	} else {
		a.cfg.Global.Window = ws
	}

	if mode == "compact" {
		a.cfg.Global.ViewMode = "compact"
	} else {
		a.cfg.Global.ViewMode = ""
	}
	_ = a.saveConfig()

	// Return the target mode's saved window state.
	var target *config.WindowState
	if mode == "compact" {
		target = a.cfg.Global.CompactWindow
	} else {
		target = a.cfg.Global.Window
	}
	if target == nil {
		return nil
	}
	return &WindowStateDTO{X: target.X, Y: target.Y, Width: target.Width, Height: target.Height}
}
