package main

import (
	"embed"

	"github.com/LuisPalacios/gitbox/pkg/config"
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
	app := NewApp()

	// Pre-load config to restore saved window dimensions.
	width, height := 900, 700
	if cfg, err := config.Load(config.DefaultV2Path()); err == nil && cfg.Global.Window != nil {
		w := cfg.Global.Window
		if w.Width >= 640 {
			width = w.Width
		}
		if w.Height >= 480 {
			height = w.Height
		}
		app.savedWindowPos = w
	}

	err := wails.Run(&options.App{
		Title:     "gitbox",
		Width:     width,
		Height:    height,
		MinWidth:  640,
		MinHeight: 480,
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
