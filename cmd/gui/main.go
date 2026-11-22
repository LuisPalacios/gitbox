//go:build !gitboxserver

package main

import (
	"embed"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/LuisPalacios/gitbox/cmd/gui/serverbin"
	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/update"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

// maxWindowWidth stops the main window where the content stops (#148):
// the 220px sidebar, the 1100px view content, its 2×24px padding and a
// 16px scrollbar. A wider window would only add an empty band; folding
// the sidebar hands its width to the content instead (style.css).
const maxWindowWidth = 220 + 1100 + 48 + 16

func main() {
	// After an update, the new version starts with --wait-pid=<old PID> and
	// waits for the old process to exit and release the single-instance lock.
	for _, arg := range os.Args[1:] {
		if after, ok := strings.CutPrefix(arg, update.WaitPIDFlag); ok {
			if pid, err := strconv.Atoi(after); err == nil {
				update.WaitForExit(pid, 30*time.Second)
			}
		}
	}

	app := NewApp()

	// Check CLI flags before Wails starts (Wails has no CLI arg support of
	// its own). We support two side-modes:
	//   --test-mode         : route config through a temp dir for the test fixture.
	//   --terminals-window  : run as the dedicated Terminals editor sub-process
	//                         (issue #69 — opens the Profile manager in its
	//                         own OS window so the editor can scroll on its
	//                         own and the parent window stays interactive).
	parentPID := 0
	experimentalRemote := false
	// --remote <alias> runs this process as the window of one remote
	// (#239); see runRemoteWindow.
	remoteArgs, remoteErr := parseRemoteFlags(os.Args[1:])
	for _, arg := range os.Args[1:] {
		// --version prints the build version and exits without opening a
		// window, so scripts can smoke-test an installed GitboxApp.
		if arg == "--version" {
			fmt.Printf("GitboxApp %s\n", app.GetAppVersion())
			return
		}
		// --list-server-targets prints the gitbox-server binaries this
		// build carries. It fails when the build cannot run remote mode
		// with them, or when one does not match its pin. Build scripts
		// check their artifact with it.
		if arg == "--list-server-targets" {
			build := thisServerBuild()
			build.list(os.Stdout)
			err := build.check()
			if err == nil {
				err = build.verify(serverbin.Binary)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %s\n", userMsg(err))
				os.Exit(1)
			}
			return
		}
		if remoteErr != nil {
			fmt.Fprintf(os.Stderr, "error: %s\n", remoteErr)
			os.Exit(2)
		}
		// A remote window in test mode reads the remotes of the test
		// window that started it (--prefs-file) instead of a fixture.
		if arg == "--test-mode" && remoteArgs.alias == "" {
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
		// --experimental-remote opts in to remote mode (#233): it shows
		// the Remotes section of Settings, like the experimental_remote
		// key, and a remote window needs it.
		if arg == "--experimental-remote" {
			experimentalRemote = true
			app.experimentalRemote = true
		}
		// --parent-pid=<PID> tells the Manager subprocess which gitbox.exe
		// to watch. When that PID disappears, the watcher self-terminates
		// — the safety net so the Manager can never outlive its parent
		// regardless of how the parent died (issue #69).
		if after, ok := strings.CutPrefix(arg, "--parent-pid="); ok {
			if v, err := strconv.Atoi(after); err == nil {
				parentPID = v
			}
		}
	}

	if remoteArgs.alias != "" {
		runRemoteWindow(app, remoteArgs)
		return
	}

	if app.windowMode == "terminals" {
		// Watcher fires within ms of parent death on Windows
		// (WaitForSingleObject) and within ~1s on macOS (signal-0 poll).
		// Linux relies on PR_SET_PDEATHSIG set by the parent when it
		// spawned us; this watcher is a no-op there.
		go watchParent(parentPID)
		runTerminalsWindow(app)
		return
	}

	// Pre-load config to restore saved window dimensions and view mode.
	width, height := 1040, 700
	minWidth, minHeight := 640, 480
	preloadPath := config.DefaultV2Path()
	if app.cfgPath != "" {
		preloadPath = app.cfgPath
	}
	if cfg, err := config.Load(preloadPath); err == nil {
		if !app.testMode {
			update.CleanupOldBinary()
		}
		// The geometry is a client preference, stored per destination: the
		// local window's own, or that of the remote this window shows.
		prefs := config.ExtractPrefs(cfg)
		fw, cw := prefs.WindowFor(app.remoteAlias)
		experimentalRemote = experimentalRemote || prefs.ExperimentalRemote
		app.savedViewMode = prefs.ViewMode
		if fw != nil {
			app.savedWindowPos = fw
		}
		if cw != nil {
			app.savedCompactPos = cw
		}
		if prefs.ViewMode == "compact" {
			minWidth, minHeight = 200, 200
			if cw != nil {
				if cw.Width >= 200 {
					width = cw.Width
				}
				if cw.Height >= 200 {
					height = cw.Height
				}
			} else {
				width, height = 220, 400
			}
		} else if fw != nil {
			if fw.Width >= 640 {
				width = min(fw.Width, maxWindowWidth)
			}
			if fw.Height >= 480 {
				height = fw.Height
			}
		}
	}

	// Decide the backend after the preload. The terminals subprocess
	// returned above and always runs the local backend.
	app.backend = selectBackend(app, experimentalRemote)

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
		Title:       windowTitle,
		Width:       width,
		Height:      height,
		MinWidth:    minWidth,
		MinHeight:   minHeight,
		MaxWidth:    maxWindowWidth,
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
		Bind: []any{
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
				app.sink().Emit("profiles:reloaded")
				wailsrt.WindowUnminimise(app.ctx)
				wailsrt.WindowShow(app.ctx)
			},
		},
		OnStartup:  app.Startup,
		OnShutdown: app.Shutdown,
		OnDomReady: app.DomReady,
		Bind: []any{
			app,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "terminals window: %v\n", err)
	}
}

// runRemoteWindow runs the window of one remote (#239): its own process,
// with a single-instance lock per alias, so a second Connect focuses it.
// It loads only the client preferences, from this machine's gitbox.json
// (the test window's, in test mode), and connects from DomReady.
func runRemoteWindow(app *App, f remoteFlags) {
	path := f.prefsFile
	if path == "" {
		path = config.DefaultV2Path()
	}
	prefs := newFilePrefs(path)
	if _, ok := findRemote(prefs.Get().Remotes, f.alias); !ok {
		fmt.Fprintf(os.Stderr, "error: no remote named %s is configured; add it in Settings, Remotes (experimental)\n", f.alias)
		os.Exit(2)
	}
	app.prefs = prefs
	app.cfgPath = path
	app.testMode = f.testMode
	app.experimentalRemote = true
	app.remoteAlias = f.alias
	app.windowMode = "remote"
	app.remote = newRemoteSession(app, f.alias)
	app.backend = selectBackend(app, true)

	// The window shows the host's data in the full view (#240), so it
	// opens at the main window's size; it has no compact view yet.
	width, height := 1040, 700
	p := prefs.Get()
	if fw, _ := p.WindowFor(f.alias); fw != nil {
		app.savedWindowPos = fw
		if fw.Width >= 640 {
			width = min(fw.Width, maxWindowWidth)
		}
		if fw.Height >= 480 {
			height = fw.Height
		}
	}
	title, lockID := "gitbox · "+f.alias, "com.luispalacios.gitbox.remote."+f.alias
	if f.testMode {
		title += " [test]"
		lockID += ".test"
	}
	err := wails.Run(&options.App{
		Title:       title,
		Width:       width,
		Height:      height,
		MinWidth:    640,
		MinHeight:   480,
		MaxWidth:    maxWindowWidth,
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
		Bind: []any{
			app,
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "remote window: %v\n", err)
	}
}
