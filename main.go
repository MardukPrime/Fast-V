package main

import (
	"embed"
	"log"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	store, err := NewStore()
	if err != nil {
		log.Fatal(err)
	}

	appService := NewAppService()
	coreService := NewCoreService(store)
	dashboardService := NewDashboardService(store, coreService)
	subscriptionService := NewSubscriptionService(store)
	settingsService := NewSettingsService(store)

	app := application.New(application.Options{
		Name:        productName,
		Description: "A modern proxy client built with Wails v3",
		Services: []application.Service{
			application.NewService(appService),
			application.NewService(coreService),
			application.NewService(dashboardService),
			application.NewService(subscriptionService),
			application.NewService(settingsService),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: productID,
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath:   filepath.Join(filepath.Dir(store.path), "webview2"),
			AdditionalBrowserArgs: []string{"--disable-gpu"},
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            productName,
		Width:            1280,
		Height:           800,
		MinWidth:         960,
		MinHeight:        640,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 18, 25),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
