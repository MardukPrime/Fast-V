package main

import "runtime"

const (
	productName  = "FastV"
	productID    = "com.mardukprime.fastv"
	wailsVersion = "3.1.0"
)

var (
	version   = "0.1.0"
	commit    = "dev"
	buildDate = "unknown"
)

// AppService exposes application metadata to the frontend.
type AppService struct{}

func NewAppService() *AppService {
	return &AppService{}
}

func (s *AppService) ServiceName() string {
	return "AppService"
}

func (s *AppService) Info() AppInfo {
	return AppInfo{
		Name:         productName,
		Version:      version,
		Commit:       commit,
		BuildDate:    buildDate,
		Platform:     runtime.GOOS,
		Arch:         runtime.GOARCH,
		WailsVersion: wailsVersion,
	}
}
