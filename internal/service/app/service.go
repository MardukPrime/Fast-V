package app

import (
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/buildinfo"
	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/platform"
)

// AppService exposes application metadata to the frontend.
type AppService struct{}

func New() *AppService {
	return &AppService{}
}

func (s *AppService) ServiceName() string {
	return "AppService"
}

func (s *AppService) Info() model.AppInfo {
	return model.AppInfo{
		Name:         buildinfo.ProductName,
		Version:      buildinfo.Version,
		Commit:       buildinfo.Commit,
		BuildDate:    buildinfo.BuildDate,
		Platform:     runtime.GOOS,
		Arch:         runtime.GOARCH,
		WailsVersion: buildinfo.WailsVersion,
	}
}

func (s *AppService) IsElevated() bool {
	return platform.IsElevated()
}

func (s *AppService) RestartAsAdmin() error {
	if platform.IsElevated() {
		return nil
	}
	if err := platform.LaunchElevatedRestart(); err != nil {
		return err
	}
	go func() {
		time.Sleep(700 * time.Millisecond)
		if app := application.Get(); app != nil {
			app.Quit()
		}
	}()
	return nil
}
