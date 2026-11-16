package app

import (
	"context"
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

// ServiceStartup only probes the current autostart state. Startup must never
// rewrite HKCU\...\Run: the registry value is changed exclusively by an explicit
// SetAutostart call from the user.
func (s *AppService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	_, _ = platform.AutostartEnabled()
	return nil
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

// AutostartEnabled reports whether FastV is registered to start with Windows.
func (s *AppService) AutostartEnabled() (bool, error) {
	return platform.AutostartEnabled()
}

// SetAutostart enables or disables launching FastV when the user logs in and
// returns the state that is in effect afterwards.
func (s *AppService) SetAutostart(enabled bool) (bool, error) {
	if err := platform.SetAutostart(enabled); err != nil {
		return false, err
	}
	return platform.AutostartEnabled()
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
