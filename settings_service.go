package main

import (
	"errors"
	"fmt"
	"strings"
)

// SettingsService manages basic application settings.
type SettingsService struct {
	store *Store
}

func NewSettingsService(store *Store) *SettingsService {
	return &SettingsService{store: store}
}

func (s *SettingsService) ServiceName() string {
	return "SettingsService"
}

func (s *SettingsService) Get() Settings {
	return s.store.Settings()
}

func (s *SettingsService) Save(input Settings) (Settings, error) {
	input.Theme = strings.TrimSpace(input.Theme)
	input.Language = strings.TrimSpace(input.Language)
	input.ProxyMode = strings.TrimSpace(input.ProxyMode)
	input.CoreType = strings.TrimSpace(input.CoreType)
	input.XrayLogLevel = strings.TrimSpace(input.XrayLogLevel)

	if input.Theme != "dark" && input.Theme != "light" && input.Theme != "system" {
		return Settings{}, errors.New("invalid theme")
	}
	if input.Language != "zh-CN" && input.Language != "en-US" {
		return Settings{}, errors.New("invalid language")
	}
	if input.ProxyMode != "rule" && input.ProxyMode != "global" && input.ProxyMode != "direct" {
		return Settings{}, errors.New("invalid proxy mode")
	}
	if input.CoreType != "xray" && input.CoreType != "singbox" {
		return Settings{}, errors.New("invalid core type")
	}
	if input.XrayLocalPort < 1 || input.XrayLocalPort > 65535 {
		return Settings{}, errors.New("invalid Xray local port")
	}
	if input.XrayLogLevel != "debug" && input.XrayLogLevel != "info" && input.XrayLogLevel != "warning" && input.XrayLogLevel != "error" && input.XrayLogLevel != "none" {
		return Settings{}, errors.New("invalid Xray log level")
	}
	if input.XrayMuxConcurrency < 1 || input.XrayMuxConcurrency > 128 {
		return Settings{}, fmt.Errorf("Xray mux concurrency must be between 1 and 128")
	}
	if input.UpdateIntervalMinutes < 15 || input.UpdateIntervalMinutes > 1440 {
		return Settings{}, fmt.Errorf("update interval must be between 15 and 1440 minutes")
	}

	if err := s.store.SaveSettings(input); err != nil {
		return Settings{}, err
	}
	return input, nil
}

func (s *SettingsService) Reset() (Settings, error) {
	defaults := defaultSettings()
	if err := s.store.SaveSettings(defaults); err != nil {
		return Settings{}, err
	}
	return defaults, nil
}
