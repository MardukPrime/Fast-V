package bootstrap

import (
"embed"

"github.com/wailsapp/wails/v3/pkg/application"

"github.com/MardukPrime/Fast-V/internal/buildinfo"
"github.com/MardukPrime/Fast-V/internal/transport/wails/services"
)

// Run assembles the Wails application and starts the desktop event loop.
func Run(assets embed.FS) error {
appService := services.NewAppService()

app := application.New(application.Options{
Name:        buildinfo.ProductName,
Description: "A modern proxy client built with Wails v3",
Services: []application.Service{
application.NewService(appService),
},
Assets: application.AssetOptions{
Handler: application.AssetFileServerFS(assets),
},
SingleInstance: &application.SingleInstanceOptions{
UniqueID: buildinfo.ProductID,
},
Mac: application.MacOptions{
ApplicationShouldTerminateAfterLastWindowClosed: true,
},
})

app.Window.NewWithOptions(application.WebviewWindowOptions{
Title:            buildinfo.ProductName,
Width:            1280,
Height:           800,
MinWidth:         960,
MinHeight:        640,
BackgroundColour: application.NewRGB(15, 18, 25),
URL:              "/",
})

return app.Run()
}
