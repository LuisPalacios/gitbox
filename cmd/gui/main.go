package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// Build-time variables (set via -ldflags).
var (
	version = "dev"
	commit  = "none"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Disable DirectComposition to prevent other desktop windows from flickering
	// when WebView2 initializes its GPU swapchain (Windows 11 DWM interaction).
	os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "--disable-direct-composition --disable-features=DCompPresenter")

	app := NewApp()

	err := wails.Run(&options.App{
		Title:  "gitbox",
		Width:  900,
		Height: 700,
		MinWidth: 640,
		MinHeight: 480,
		StartHidden: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 9, G: 9, B: 11, A: 255},
		OnStartup:  app.Startup,
		OnShutdown: app.Shutdown,
		OnDomReady: app.DomReady,
		Windows: &windows.Options{
			WebviewGpuIsDisabled: true,
		},
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
