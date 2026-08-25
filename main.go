package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/appicon.png
var appIcon []byte

func main() {
	// WebKitGTK's DMA-BUF renderer crashes on Wayland with "Error 71 (Protocol error)"
	// on many compositor/driver combos when hardware acceleration is enabled.
	// Disabling just the DMA-BUF path keeps GPU compositing (needed for <video>) working.
	os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")

	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "AlphaRing Launcher",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		Linux: &linux.Options{
			Icon: appIcon,
			// Must match the Icon= name and Exec= basename in the .desktop
			// file install.sh installs, so window managers that resolve the
			// taskbar icon via desktop-file matching (e.g. KDE Plasma) find
			// it instead of falling back to a generic binary icon.
			ProgramName:      "alpharing",
			WebviewGpuPolicy: linux.WebviewGpuPolicyAlways,
		},
		OnStartup: app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
