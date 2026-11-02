package services

import (
"runtime"

"github.com/MardukPrime/Fast-V/internal/buildinfo"
"github.com/MardukPrime/Fast-V/internal/transport/wails/dto"
)

// AppService exposes application metadata to the frontend.
type AppService struct{}

func NewAppService() *AppService {
return &AppService{}
}

func (s *AppService) ServiceName() string {
return "AppService"
}

func (s *AppService) Info() dto.AppInfo {
return dto.AppInfo{
Name:         buildinfo.ProductName,
Version:      buildinfo.Version,
Commit:       buildinfo.Commit,
BuildDate:    buildinfo.BuildDate,
Platform:     runtime.GOOS,
Arch:         runtime.GOARCH,
WailsVersion: buildinfo.WailsVersion,
}
}
