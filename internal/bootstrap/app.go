package bootstrap

import (
	"embed"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/buildinfo"
	"github.com/MardukPrime/Fast-V/internal/platform"
	appservice "github.com/MardukPrime/Fast-V/internal/service/app"
	backupservice "github.com/MardukPrime/Fast-V/internal/service/backup"
	"github.com/MardukPrime/Fast-V/internal/service/core"
	"github.com/MardukPrime/Fast-V/internal/service/dashboard"
	diagnosticsservice "github.com/MardukPrime/Fast-V/internal/service/diagnostics"
	"github.com/MardukPrime/Fast-V/internal/service/node"
	proxyservice "github.com/MardukPrime/Fast-V/internal/service/proxy"
	routingservice "github.com/MardukPrime/Fast-V/internal/service/routing"
	"github.com/MardukPrime/Fast-V/internal/service/settings"
	"github.com/MardukPrime/Fast-V/internal/service/subscription"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// Run assembles the application, services and window, then starts the Wails event loop.
func Run(assets embed.FS) error {
	st, err := store.NewStore()
	if err != nil {
		return err
	}

	if st.Settings().TunMode && !platform.IsElevated() {
		if err := platform.LaunchElevatedRestart(); err == nil {
			return nil
		}
	}

	appSvc := appservice.New(st)
	coreSvc := core.NewCoreService(st)
	proxySvc := proxyservice.New(st, coreSvc)
	diagnosticsSvc := diagnosticsservice.New(proxySvc)
	dashboardSvc := dashboard.New(st, coreSvc)
	subSvc := subscription.New(st)
	nodeSvc := node.New(st, coreSvc)
	settingsSvc := settings.New(st)
	routingSvc := routingservice.New(st)
	backupSvc := backupservice.New(st)

	app := application.New(application.Options{
		Name:        buildinfo.ProductName,
		Description: "A modern proxy client built with Wails v3",
		Services: []application.Service{
			application.NewService(appSvc),
			application.NewService(coreSvc),
			application.NewService(proxySvc),
			application.NewService(diagnosticsSvc),
			application.NewService(dashboardSvc),
			application.NewService(subSvc),
			application.NewService(nodeSvc),
			application.NewService(settingsSvc),
			application.NewService(routingSvc),
			application.NewService(backupSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: buildinfo.ProductID,
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: filepath.Join(st.DataDir(), "webview2"),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            buildinfo.ProductName,
		Width:            1280,
		Height:           800,
		MinWidth:         960,
		MinHeight:        640,
		Frameless:        true,
		BackgroundColour: application.NewRGB(238, 242, 247),
		URL:              "/",
	})

	// The tray and the close/minimize-to-tray behavior are wired after the
	// window exists and before the event loop starts; Wails creates the tray
	// icon itself once the application is running.
	newTray(app, st, window, proxySvc, appSvc)

	return app.Run()
}
