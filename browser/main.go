package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:     "OpenLink",
		Width:     920,
		Height:    640,
		MinWidth:  640,
		MinHeight: 420,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 16, G: 20, B: 26, A: 1},
		// Only one copy may run: two would fight over UDP 1343.
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "openlink-browser-4f1c2a"},
		OnStartup:          app.startup,
		OnShutdown:         app.shutdown,
		Bind:               []interface{}{app},
	})
	if err != nil {
		println("Error:", err.Error())
	}
}
