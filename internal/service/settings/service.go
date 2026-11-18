package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/MardukPrime/Fast-V/internal/hotkey"
	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

// accentColors lists the accent color presets shared with the frontend theme.
var accentColors = map[string]struct{}{
	"sky":     {},
	"violet":  {},
	"emerald": {},
	"amber":   {},
	"rose":    {},
}

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

// Save validates, normalizes and persists the user-editable settings, then
// broadcasts the change. The validation rules live in normalizeSettings so
// derived operations can reuse the exact same path.
func (s *SettingsService) Save(input model.Settings) (model.Settings, error) {
	normalized, err := normalizeSettings(input)
	if err != nil {
		return model.Settings{}, err
	}
	if err := s.store.SaveSettings(normalized); err != nil {
		return model.Settings{}, err
	}
	emitSettingsChanged(normalized)
	return normalized, nil
}

// TunFallback disables TUN mode and switches to the fixed system proxy so a
// user whose TUN adapter cannot start stays online after restarting the proxy.
// The current settings are re-validated and persisted through the same path as
// Save, and the resulting update is broadcast like any other settings change.
func (s *SettingsService) TunFallback() (model.Settings, error) {
	current := s.store.Settings()
	current.TunMode = false
	current.SystemProxyMode = "system"
	return s.Save(current)
}

// normalizeSettings applies the trimming/defaulting rules and validates every
// field of the user-editable settings. It has no side effects.
func normalizeSettings(input model.Settings) (model.Settings, error) {
	input.Theme = strings.TrimSpace(input.Theme)
	input.Language = strings.TrimSpace(input.Language)
	input.AccentColor = strings.TrimSpace(input.AccentColor)
	input.ProxyMode = strings.TrimSpace(input.ProxyMode)
	input.CoreType = strings.TrimSpace(input.CoreType)
	input.XrayLogLevel = strings.TrimSpace(input.XrayLogLevel)
	input.TestURL = strings.TrimSpace(input.TestURL)
	input.InboundUsername = strings.TrimSpace(input.InboundUsername)
	input.InboundPassword = strings.TrimSpace(input.InboundPassword)
	// Global shortcuts are optional: an empty value means the action has no
	// shortcut (disabled). Non-empty values must be valid, unique combinations;
	// they are rewritten into the canonical "Ctrl+Alt+Shift+Win+Key" form so the
	// stored settings stay stable and comparable.
	if err := hotkey.ValidateSettingsCombos(input); err != nil {
		return model.Settings{}, err
	}
	normalizedHotkeys, err := hotkey.NormalizeSettingsCombos(input)
	if err != nil {
		return model.Settings{}, err
	}
	input = normalizedHotkeys
	if input.LayoutMode != "sidebar" && input.LayoutMode != "dock" {
		return model.Settings{}, errors.New("invalid layout mode")
	}

	input.SystemProxyMode = strings.TrimSpace(input.SystemProxyMode)
	switch input.SystemProxyMode {
	case "system", "off", "pac":
	default:
		return model.Settings{}, errors.New("系统代理模式必须是 system、off 或 pac")
	}
	if input.DockTransparency < 5 || input.DockTransparency > 90 {
		return model.Settings{}, errors.New("dock transparency must be between 5 and 90")
	}
	if input.Theme != "dark" && input.Theme != "light" && input.Theme != "system" {
		return model.Settings{}, errors.New("invalid theme")
	}
	// Only the fixed preset list is a valid accent color; the frontend theme
	// layer has no palette for anything else.
	if _, ok := accentColors[input.AccentColor]; !ok {
		return model.Settings{}, errors.New("不支持的强调色")
	}
	if input.Language != "zh-CN" && input.Language != "en-US" {
		return model.Settings{}, errors.New("invalid language")
	}
	if input.ProxyMode != "rule" && input.ProxyMode != "global" && input.ProxyMode != "direct" {
		return model.Settings{}, errors.New("invalid proxy mode")
	}
	// Load balancing only supports the strategies implemented by Xray's balancer.
	// An empty value keeps the default leastPing strategy; LoadBalanceEnabled has
	// no validation of its own.
	input.LoadBalanceMode = strings.TrimSpace(input.LoadBalanceMode)
	switch input.LoadBalanceMode {
	case "":
		input.LoadBalanceMode = "leastPing"
	case "leastPing", "leastLoad", "roundRobin":
	default:
		return model.Settings{}, errors.New("负载均衡策略必须是 leastPing、leastLoad 或 roundRobin")
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
	// Config override is a user-supplied JSON object deep-merged into the
	// generated Xray configuration. Only a top-level object is meaningful for
	// merging; the size cap keeps a pasted document from bloating data.json.
	input.ConfigOverride = strings.TrimSpace(input.ConfigOverride)
	if len(input.ConfigOverride) > 128*1024 {
		return model.Settings{}, errors.New("配置覆写过大（最多 128 KB）")
	}
	if err := validateJSONObject(input.ConfigOverride); err != nil {
		return model.Settings{}, fmt.Errorf("配置覆写不是合法的 JSON 对象: %w", err)
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
	if input.AutoSelectIntervalMinutes < 1 || input.AutoSelectIntervalMinutes > 1440 {
		return model.Settings{}, errors.New("自动选路间隔必须在 1-1440 分钟之间")
	}
	if input.AutoSelectToleranceMS < 0 || input.AutoSelectToleranceMS > 5000 {
		return model.Settings{}, errors.New("自动选路切换容差必须在 0-5000 毫秒之间")
	}
	// Inbound authentication only needs credentials when it is enabled; AllowLan
	// has no additional constraints.
	if input.InboundAuthEnabled {
		if input.InboundUsername == "" {
			return model.Settings{}, errors.New("入站认证用户名不能为空")
		}
		for _, r := range input.InboundUsername {
			if unicode.IsSpace(r) {
				return model.Settings{}, errors.New("入站认证用户名不能包含空白字符")
			}
		}
		if input.InboundPassword == "" {
			return model.Settings{}, errors.New("入站认证密码不能为空")
		}
		if utf8.RuneCountInString(input.InboundPassword) < 6 {
			return model.Settings{}, errors.New("入站认证密码长度不能少于 6 位")
		}
	}

	return input, nil
}

func (s *SettingsService) Reset() (model.Settings, error) {
	defaults := store.DefaultSettings()
	if err := s.store.SaveSettings(defaults); err != nil {
		return model.Settings{}, err
	}
	emitSettingsChanged(defaults)
	return defaults, nil
}

// emitSettingsChanged broadcasts a successful settings mutation so the tray
// menu, the hotkey wiring and the frontend can follow changes made from another
// surface. The application may not exist yet (unit tests, early startup), in
// which case there is nobody to notify.
func emitSettingsChanged(settings model.Settings) {
	app := application.Get()
	if app == nil {
		return
	}
	app.Event.Emit("settings:changed", settings)
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
