package app

import (
	"runtime"

	"github.com/MardukPrime/Fast-V/internal/buildinfo"
	"github.com/MardukPrime/Fast-V/internal/model"
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
