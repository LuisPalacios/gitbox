package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/update"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Build-time variables (set via -ldflags).
var (
	version = "dev"
	commit  = "none"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	update.CleanupOldBinary()

	app := NewApp()

	// Check CLI flags before Wails starts (Wails has no CLI arg support of
	// its own). We support two side-modes:
	//   --test-mode         : route config through a temp dir for the test fixture.
	//   --terminals-window  : run as the dedicated Terminals editor sub-process
	//                         (issue #69 — opens the Profile manager in its
	//                         own OS window so the editor can scroll on its
	//                         own and the parent window stays interactive).
	for _, arg := range os.Args[1:] {
		if arg == "--test-mode" {
			cfgPath, cleanup, err := config.SetupTestMode()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			app.cfgPath = cfgPath
			app.testMode = true
			app.testCleanup = cleanup
		}
		if arg == "--terminals-window" {
			app.windowMode = "terminals"
		}
	}

	if app.windowMode == "terminals" {
		runTerminalsWindow(app)
		return
	}

	// Pre-load config to restore saved window dimensions and view mode.
	width, height := 900, 700
	minWidth, minHeight := 640, 480
	preloadPath := config.DefaultV2Path()
	if app.cfgPath != "" {
		preloadPath = app.cfgPath
	}
	if cfg, err := config.Load(preloadPath); err == nil {
		app.savedViewMode = cfg.Global.ViewMode
		if cfg.Global.Window != nil {
			app.savedWindowPos = cfg.Global.Window
		}
		if cfg.Global.CompactWindow != nil {
			app.savedCompactPos = cfg.Global.CompactWindow
		}
		if cfg.Global.ViewMode == "compact" {
			minWidth, minHeight = 200, 200
			if cw := cfg.Global.CompactWindow; cw != nil {
				if cw.Width >= 200 {
					width = cw.Width
				}
				if cw.Height >= 200 {
					height = cw.Height
				}
			} else {
				width, height = 220, 400
			}
		} else if fw := cfg.Global.Window; fw != nil {
			if fw.Width >= 640 {
				width = fw.Width
			}
			if fw.Height >= 480 {
				height = fw.Height
			}
		}
	}

	windowTitle := "gitbox"
	if app.testMode {
		windowTitle = "gitbox [test]"
	}

	// Single-instance lock: a second launch (e.g. another dock click while
	// the app is already running) exits immediately and focuses the first
	// window instead of spawning a duplicate process. Test mode uses a
	// distinct lock id so it can coexist with a production instance.
	lockID := "com.luispalacios.gitbox"
	if app.testMode {
		lockID = "com.luispalacios.gitbox.test"
	}

	err := wails.Run(&options.App{
		Title:     windowTitle,
		Width:     width,
		Height:    height,
		MinWidth:  minWidth,
		MinHeight: minHeight,
		StartHidden: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 9, G: 9, B: 11, A: 255},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: lockID,
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				if app.ctx == nil {
					return
				}
				wailsrt.WindowUnminimise(app.ctx)
				wailsrt.WindowShow(app.ctx)
			},
		},
		OnStartup:     app.Startup,
		OnShutdown:    app.Shutdown,
		OnBeforeClose: app.BeforeClose,
		OnDomReady:    app.DomReady,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}

// runTerminalsWindow runs the dedicated Terminals manager subprocess
// (issue #69). It reuses the same App struct + frontend bundle as the main
// window, but:
//   - Uses a distinct SingleInstanceLock id so it can coexist with the main
//     gitbox process (and so launching a second Manager focuses the first
//     instead of opening a duplicate).
//   - Skips the default-window bootstrapping in DomReady (no editor sync,
//     no workspace discovery, no probe loops) — App.DomReady checks
//     a.windowMode and short-circuits when set to "terminals".
//   - Opens a smaller window sized for the editor.
//
// The frontend reads a.GetWindowMode() on mount and renders only the
// TerminalsModal contents in this mode (everything else is gated off).
func runTerminalsWindow(app *App) {
	const (
		w, h       = 980, 720
		minW, minH = 700, 480
	)
	err := wails.Run(&options.App{
		Title:     "gitbox · Terminals",
		Width:     w,
		Height:    h,
		MinWidth:  minW,
		MinHeight: minH,
		// StartHidden prevents the dark-empty-webview flash users see before
		// Svelte mounts and paints the editor (issue #69 user feedback).
		// DomReady reveals the window once SyncProfiles has populated the
		// detected lists and the DOM is ready to render.
		StartHidden: true,
		// HideWindowOnClose makes the X button hide the editor instead of
		// quitting the subprocess. Re-opening the Manager from the parent
		// then takes the SingleInstanceLock fast-path
		// (OnSecondInstanceLaunch → WindowShow) and appears instantly,
		// instead of paying the full Wails+webview cold-start every time
		// (issue #69 user feedback — close-then-reopen on Win/Linux
		// flashed an init window before the real window finally arrived
		// 1-2s later). The parent's Shutdown still kills the subprocess
		// so the user can't end up with an orphan when they quit gitbox.
		HideWindowOnClose: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 9, G: 9, B: 11, A: 255},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.luispalacios.gitbox.terminals",
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				if app.ctx == nil {
					return
				}
				// Re-detect before showing so a Manager window that
				// was hidden 10 minutes ago doesn't show stale data
				// after the user installed a new shell or edited
				// wezterm.lua. Then nudge the frontend to reload from
				// disk and reveal the window.
				app.SyncProfiles()
				wailsrt.EventsEmit(app.ctx, "profiles:reloaded")
				wailsrt.WindowUnminimise(app.ctx)
				wailsrt.WindowShow(app.ctx)
			},
		},
		OnStartup:  app.Startup,
		OnShutdown: app.Shutdown,
		OnDomReady: app.DomReady,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "terminals window: %v\n", err)
	}
}
