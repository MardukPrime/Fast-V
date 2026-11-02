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

	if input.Theme != "dark" && input.Theme != "light" && input.Theme != "system" {
		return Settings{}, errors.New("invalid theme")
	}
	if input.Language != "zh-CN" && input.Language != "en-US" {
		return Settings{}, errors.New("invalid language")
	}
	if input.ProxyMode != "rule" && input.ProxyMode != "global" && input.ProxyMode != "direct" {
		return Settings{}, errors.New("invalid proxy mode")
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
