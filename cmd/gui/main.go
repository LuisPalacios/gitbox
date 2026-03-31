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

	// Check for --test-mode before Wails starts (Wails has no CLI arg support).
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
			break
		}
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
