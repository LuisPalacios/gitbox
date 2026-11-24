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
	"runtime"
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
	if a.windowMode == "" && !a.testMode {
		if exe, err := update.SelfPath(); err == nil {
			a.updateRecovery, a.updateRecoveryErr = update.BeginRecovery(exe, filepath.Dir(config.DefaultV2Path()))
		}
	}
	a.be().Init(ctx)
	if r := a.updateRecovery; r != nil && r.Remaining() > 0 {
		a.recoveryExpiry = time.AfterFunc(r.Remaining(), func() {
			a.recoveryMu.Lock()
			defer a.recoveryMu.Unlock()
			if a.updateRecovery == r && a.finishUpdateRecovery(false) == nil {
				emit(a.newOperation(), "update:recoveryExpired", any(nil))
			}
		})
	}
}

// Shutdown is called by Wails when the app is closing.
func (a *App) Shutdown(_ context.Context) {
	a.tasks.stop()
	if a.recoveryExpiry != nil {
		a.recoveryExpiry.Stop()
	}
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
	// A remote window stops its server: closing its input is the signal.
	if a.remote != nil {
		a.remote.disconnect()
	}
	// Stop the backend's workers before test cleanup removes the temp
	// config dir, so a late pass can't recreate it via config.Save.
	a.be().Close()
	if a.remote != nil {
		a.tasks.wg.Wait()
	}
	a.toolsWG.Wait()
	if a.testCleanup != nil {
		a.testCleanup()
	}
}

// BeforeClose is called while the window is still alive, before it is destroyed.
// We capture and persist the window position and size to the active view mode slot.
//
// The store refuses to save over a config it failed to load.
func (a *App) BeforeClose(_ context.Context) bool {
	if x, y, w, h, ok := a.geometry(); ok {
		ws := &config.WindowState{X: x, Y: y, Width: w, Height: h}
		_ = a.clientPrefs().Update(func(p *config.ClientPrefs) {
			// A remote window has no compact view yet (#239).
			p.SetWindow(a.remoteAlias, p.ViewMode == "compact" && a.windowMode != "remote", ws)
		})
	}
	return false // don't prevent closing
}

// geometry returns the window position and size, from the test seam when
// one is set.
func (a *App) geometry() (x, y, w, h int, ok bool) {
	if a.windowGeom != nil {
		return a.windowGeom()
	}
	x, y = wailsrt.WindowGetPosition(a.ctx)
	w, h = wailsrt.WindowGetSize(a.ctx)
	return x, y, w, h, true
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
	// A remote window (#239) shows itself at once and connects in the
	// background: Startup did no network work, so the window is never
	// held up by ssh. It runs none of the local window's workers.
	if a.windowMode == "remote" {
		if w := a.savedWindowPos; w != nil && a.isPositionOnScreen(w.X, w.Y, w.Width, w.Height) {
			wailsrt.WindowSetPosition(a.ctx, w.X, w.Y)
		} else {
			wailsrt.WindowCenter(a.ctx)
		}
		wailsrt.WindowShow(a.ctx)
		a.ensureWindowTasks()
		a.remote.start()
		return
	}
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
		// The saved size is the whole window, title bar included, but on
		// macOS Wails creates the window with that size as its content, so
		// it came back one title bar taller on every launch (#256). Setting
		// the size again makes the window what was saved. Compact mode
		// fits its own height from the frontend.
		if runtime.GOOS == "darwin" && a.savedViewMode != "compact" && w.Width >= 640 && w.Height >= 480 {
			wailsrt.WindowSetSize(a.ctx, min(w.Width, maxWindowWidth), w.Height)
		}
		if a.isPositionOnScreen(w.X, w.Y, w.Width, w.Height) {
			wailsrt.WindowSetPosition(a.ctx, w.X, w.Y)
		} else {
			wailsrt.WindowCenter(a.ctx)
		}
	}
	a.toolsOnce.Do(func() {
		a.toolsWG.Go(func() {
			a.syncDetected()
			emit(a.newOperation(), "tools:updated", any(nil))
		})
	})

	// Remove the obsolete update throttle used by older releases.
	_ = os.Remove(filepath.Join(config.ConfigRoot(), config.V2ConfigDir, ".update-check"))

	// Start the window-owned registry after the renderer can receive events.
	a.ensureWindowTasks()

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

// CheckUpdateRecovery runs only after a bridge call from the main frontend.
// A failed previous launch leaves the choice with the user.
func (a *App) CheckUpdateRecovery(started bool) (string, error) {
	a.recoveryMu.Lock()
	defer a.recoveryMu.Unlock()
	if a.updateRecoveryErr != nil {
		return "", errors.New("could not verify the retained update")
	}
	r := a.updateRecovery
	if r == nil {
		return "", nil
	}
	if offered := r.OfferedVersion(); offered != "" {
		return offered, nil
	}
	if !started {
		return "", nil
	}
	if r.NeedsCleanup() {
		return "", a.finishUpdateRecovery(false)
	}
	if a.configHealthy() {
		if err := r.Confirm(); err != nil {
			return "", errors.New("could not confirm update startup")
		}
		return "", a.finishUpdateRecovery(false)
	}
	return "", nil
}

func (a *App) configHealthy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfgLoaded && a.cfgLoadError == ""
}

// ResolveUpdateRecovery acts only on the local, previously offered record.
func (a *App) ResolveUpdateRecovery(rollback bool) error {
	a.recoveryMu.Lock()
	defer a.recoveryMu.Unlock()
	r := a.updateRecovery
	if r == nil || r.OfferedVersion() == "" {
		return errors.New("no retained update is available")
	}
	if !rollback {
		if !a.configHealthy() {
			return errors.New("restore the configuration before confirming this version")
		}
		if err := r.Confirm(); err != nil {
			return errors.New("could not confirm update startup")
		}
	}
	return a.finishUpdateRecovery(rollback)
}

func (a *App) finishUpdateRecovery(rollback bool) error {
	var exe string
	if rollback {
		var err error
		exe, err = update.SelfPath()
		if err != nil {
			return errors.New("cannot locate the running application")
		}
	}
	if err := a.updateRecovery.Finish(rollback); err != nil {
		return errors.New("could not finish update recovery")
	}
	a.updateRecovery = nil
	if rollback {
		if update.Relaunch(exe, os.Getpid(), os.Args[1:]) != nil {
			return errors.New("rollback succeeded; restart GitboxApp")
		}
		wailsrt.Quit(a.ctx)
	}
	return nil
}

// ApplyUpdate downloads and applies the latest release, then restarts the
// app on the new version. On Windows, if the install directory requires
// admin privileges (e.g. Program Files), it falls back to a UAC-elevated
// helper process that verifies and installs the update.
func (a *App) ApplyUpdate() error {
	if !update.SelfUpdateSupported() {
		return errors.New("this build can't update itself; download the new version from the release page")
	}

	a.recoveryMu.Lock()
	pending := a.updateRecovery != nil || a.updateRecoveryErr != nil
	a.recoveryMu.Unlock()
	if pending {
		return errors.New("finish recovering the previous update first")
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

	op := a.newOperation()
	emit(op, "update:progress", "Downloading...")

	zipPath, err := update.DownloadRelease(ctx, result.Release, opts)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	defer os.RemoveAll(filepath.Dir(zipPath))

	emit(op, "update:progress", "Applying...")

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

	if err := update.InstallVersion(extractDir, installDir, version); err != nil {
		if errors.Is(err, update.ErrNeedElevation) {
			os.RemoveAll(extractDir)
			emit(op, "update:progress", "Requesting admin privileges...")
			if elevErr := update.ApplyElevated(zipPath, result.Release.TagName, exe != ""); elevErr != nil {
				return fmt.Errorf("elevated update: %w", elevErr)
			}
			// The helper holds the verified payload and waits for this process to exit.
			emit(op, "update:quit", result.Latest)
			return nil
		}
		os.RemoveAll(extractDir)
		return fmt.Errorf("applying: %w", err)
	}

	os.RemoveAll(extractDir)

	// Start the new version, which waits for this process to exit, then
	// quit. Quit runs BeforeClose, so the window state is saved as usual.
	if exe != "" && update.Relaunch(exe, os.Getpid(), os.Args[1:]) == nil {
		emit(op, "update:restarting", result.Latest)
		go func() {
			time.Sleep(time.Second) // let the pill show "Restarting…"
			wailsrt.Quit(a.ctx)
		}()
		return nil
	}
	emit(op, "update:done", result.Latest)
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

// openDirectoryDialog is the native folder picker; tests replace it.
var openDirectoryDialog = wailsrt.OpenDirectoryDialog

// PickFolder opens the native OS directory picker and returns the selected path.
// Returns empty string if the user cancels. A remote window never opens it:
// it would pick a folder of this computer, not of the host, so the window
// takes a typed path instead (ValidateRemotePath, #241).
func (a *App) PickFolder(title string) string {
	if a.remoteWindow() {
		return ""
	}
	dir, err := openDirectoryDialog(a.ctx, wailsrt.OpenDialogOptions{
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
	x, y, w, h, ok := a.geometry()

	var target *config.WindowState
	_ = a.clientPrefs().Update(func(p *config.ClientPrefs) {
		// Save current window position to the slot we're leaving.
		if ok {
			p.SetWindow(a.remoteAlias, p.ViewMode == "compact", &config.WindowState{X: x, Y: y, Width: w, Height: h})
		}
		if mode == "compact" {
			p.ViewMode = "compact"
		} else {
			p.ViewMode = ""
		}
		// Return the target mode's saved window state.
		full, compact := p.WindowFor(a.remoteAlias)
		if mode == "compact" {
			target = compact
		} else {
			target = full
		}
	})
	if target == nil {
		return nil
	}
	return &WindowStateDTO{X: target.X, Y: target.Y, Width: target.Width, Height: target.Height}
}
