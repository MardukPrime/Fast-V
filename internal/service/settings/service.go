package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// SettingsService manages basic application settings.
type SettingsService struct {
	store *store.Store
}

func New(store *store.Store) *SettingsService {
	return &SettingsService{store: store}
}

func (s *SettingsService) ServiceName() string {
	return "SettingsService"
}

func (s *SettingsService) Get() model.Settings {
	return s.store.Settings()
}

func (s *SettingsService) Save(input model.Settings) (model.Settings, error) {
	input.Theme = strings.TrimSpace(input.Theme)
	input.Language = strings.TrimSpace(input.Language)
	input.ProxyMode = strings.TrimSpace(input.ProxyMode)
	input.CoreType = strings.TrimSpace(input.CoreType)
	input.XrayLogLevel = strings.TrimSpace(input.XrayLogLevel)
	input.TestURL = strings.TrimSpace(input.TestURL)
	if input.LayoutMode != "sidebar" && input.LayoutMode != "dock" {
		return model.Settings{}, errors.New("invalid layout mode")
	}

	if input.DockTransparency < 5 || input.DockTransparency > 90 {
		return model.Settings{}, errors.New("dock transparency must be between 5 and 90")
	}
	if input.Theme != "dark" && input.Theme != "light" && input.Theme != "system" {
		return model.Settings{}, errors.New("invalid theme")
	}
	if input.Language != "zh-CN" && input.Language != "en-US" {
		return model.Settings{}, errors.New("invalid language")
	}
	if input.ProxyMode != "rule" && input.ProxyMode != "global" && input.ProxyMode != "direct" {
		return model.Settings{}, errors.New("invalid proxy mode")
	}
	if input.CoreType != "xray" && input.CoreType != "singbox" {
		return model.Settings{}, errors.New("invalid core type")
	}
	if input.XrayLocalPort < 1 || input.XrayLocalPort > 65535 {
		return model.Settings{}, errors.New("invalid Xray local port")
	}
	if input.XrayLogLevel != "debug" && input.XrayLogLevel != "info" && input.XrayLogLevel != "warning" && input.XrayLogLevel != "error" && input.XrayLogLevel != "none" {
		return model.Settings{}, errors.New("invalid Xray log level")
	}
	testURL, err := url.Parse(input.TestURL)
	if err != nil || testURL.Hostname() == "" || (testURL.Scheme != "http" && testURL.Scheme != "https") {
		return model.Settings{}, errors.New("invalid proxy test URL")
	}
	input.XraySockoptJSON = strings.TrimSpace(input.XraySockoptJSON)
	input.XrayFinalMaskJSON = strings.TrimSpace(input.XrayFinalMaskJSON)
	switch input.XraySockoptDomainStrategy {
	case "", "AsIs", "UseIP", "UseIPv4", "UseIPv6":
	default:
		return model.Settings{}, errors.New("invalid sockopt domain strategy")
	}
	if input.XraySockoptKeepIdle < 0 || input.XraySockoptKeepIntvl < 0 || input.XraySockoptUserTimeout < 0 {
		return model.Settings{}, errors.New("sockopt keepalive values must not be negative")
	}
	if err := validateJSONObject(input.XraySockoptJSON); err != nil {
		return model.Settings{}, fmt.Errorf("invalid sockopt JSON: %w", err)
	}
	if err := validateJSONObject(input.XrayFinalMaskJSON); err != nil {
		return model.Settings{}, fmt.Errorf("invalid finalmask JSON: %w", err)
	}
	input.XrayDNSServers = strings.TrimSpace(input.XrayDNSServers)
	switch input.XrayDNSStrategy {
	case "UseIP", "UseIPv4", "UseIPv6", "AsIs":
	default:
		return model.Settings{}, errors.New("invalid Xray DNS query strategy")
	}
	switch input.XrayDomainStrategy {
	case "AsIs", "IPIfNonMatch", "IPOnDemand":
	default:
		return model.Settings{}, errors.New("invalid Xray routing domain strategy")
	}
	if input.XrayDNSServers == "" {
		return model.Settings{}, errors.New("Xray DNS servers are required")
	}
	if input.XrayTunMTU < 576 || input.XrayTunMTU > 9000 {
		return model.Settings{}, errors.New("invalid TUN MTU")
	}
	if input.XrayAPIPort < 1 || input.XrayAPIPort > 65535 {
		return model.Settings{}, errors.New("invalid Xray API port")
	}
	if input.XrayAPIEnabled && input.XrayAPIPort == input.XrayLocalPort {
		return model.Settings{}, errors.New("Xray API port must differ from local proxy port")
	}
	if input.XrayMuxConcurrency < 1 || input.XrayMuxConcurrency > 128 {
		return model.Settings{}, fmt.Errorf("Xray mux concurrency must be between 1 and 128")
	}
	if input.UpdateIntervalMinutes < 15 || input.UpdateIntervalMinutes > 1440 {
		return model.Settings{}, fmt.Errorf("update interval must be between 15 and 1440 minutes")
	}

	if err := s.store.SaveSettings(input); err != nil {
		return model.Settings{}, err
	}
	return input, nil
}

func (s *SettingsService) Reset() (model.Settings, error) {
	defaults := store.DefaultSettings()
	if err := s.store.SaveSettings(defaults); err != nil {
		return model.Settings{}, err
	}
	return defaults, nil
}

func validateJSONObject(value string) error {
	if value == "" {
		return nil
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(value), &object); err != nil {
		return err
	}
	if object == nil {
		return errors.New("value must be a JSON object")
	}
	return nil
}
