package settings

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/model"
	"github.com/MardukPrime/Fast-V/internal/store"
)

func newService(t *testing.T) *SettingsService {
	t.Helper()
	st, err := store.NewStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	return New(st)
}

func validSettings() model.Settings {
	return model.Settings{
		Theme:                     "dark",
		Language:                  "zh-CN",
		LayoutMode:                "sidebar",
		SystemProxyMode:           "system",
		DockTransparency:          28,
		UpdateIntervalMinutes:     60,
		ProxyMode:                 "rule",
		CoreType:                  "xray",
		XrayLocalPort:             10808,
		XrayLogLevel:              "warning",
		XrayDNSStrategy:           "UseIP",
		XrayDNSServers:            "1.1.1.1,8.8.8.8",
		XrayDomainStrategy:        "IPIfNonMatch",
		XrayTunMTU:                1500,
		XrayMuxConcurrency:        8,
		TestURL:                   "https://www.gstatic.com/generate_204",
		XrayAPIEnabled:            true,
		XrayAPIPort:               10085,
		AutoSelectIntervalMinutes: 10,
		AutoSelectToleranceMS:     80,
	}
}

func TestSaveAndReset(t *testing.T) {
	service := newService(t)

	defaults := service.Get()
	if defaults.Theme != "light" || defaults.UpdateIntervalMinutes != 360 || defaults.CoreType != "xray" || defaults.XrayLocalPort != 10808 || defaults.XrayAPIPort != 10085 || defaults.LayoutMode != "sidebar" || defaults.XrayDNSStrategy != "UseIP" || defaults.XrayDNSServers == "" || defaults.XrayTunMTU != 1500 || defaults.DockTransparency != 28 || defaults.SystemProxyMode != "system" {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}

	input := validSettings()
	input.Theme = "light"
	input.AutoUpdate = true
	input.ProxyMode = "global"
	input.XrayMuxEnabled = true
	input.XraySniffing = true
	input.TunMode = true
	saved, err := service.Save(input)
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if saved.Theme != "light" || !saved.AutoUpdate || saved.ProxyMode != "global" || !saved.TunMode {
		t.Fatalf("unexpected saved settings: %+v", saved)
	}

	reset, err := service.Reset()
	if err != nil {
		t.Fatalf("reset failed: %v", err)
	}
	if reset.Theme != "light" || reset.UpdateIntervalMinutes != 360 || reset.CoreType != "xray" || reset.TunMode {
		t.Fatalf("unexpected reset settings: %+v", reset)
	}
}

func TestRejectsInvalidValues(t *testing.T) {
	service := newService(t)

	invalidTheme := validSettings()
	invalidTheme.Theme = "neon"
	if _, err := service.Save(invalidTheme); err == nil {
		t.Fatal("expected invalid theme error")
	}

	invalidPort := validSettings()
	invalidPort.XrayLocalPort = 70000
	if _, err := service.Save(invalidPort); err == nil {
		t.Fatal("expected invalid Xray port error")
	}

	invalidLevel := validSettings()
	invalidLevel.XrayLogLevel = "verbose"
	invalidTransportJSON := validSettings()
	invalidTransportJSON.XrayFinalMaskJSON = "[]"
	if _, err := service.Save(invalidTransportJSON); err == nil {
		t.Fatal("expected invalid finalmask JSON error")
	}

	if _, err := service.Save(invalidLevel); err == nil {
		t.Fatal("expected invalid Xray log level error")
	}
}

func TestSmartRoutingValues(t *testing.T) {
	service := newService(t)

	// The defaults come from the store and are valid.
	defaults := service.Get()
	if defaults.AutoSelectIntervalMinutes != 10 || defaults.AutoSelectToleranceMS != 80 {
		t.Fatalf("unexpected smart routing defaults: %+v", defaults)
	}

	enabled := validSettings()
	enabled.AutoSelectEnabled = true
	enabled.AutoSelectIntervalMinutes = 30
	enabled.AutoSelectToleranceMS = 0
	enabled.FailoverEnabled = true
	saved, err := service.Save(enabled)
	if err != nil {
		t.Fatalf("expected tolerance 0 to be accepted: %v", err)
	}
	if !saved.AutoSelectEnabled || !saved.FailoverEnabled || saved.AutoSelectIntervalMinutes != 30 || saved.AutoSelectToleranceMS != 0 {
		t.Fatalf("unexpected saved smart routing settings: %+v", saved)
	}

	invalidIntervalLow := validSettings()
	invalidIntervalLow.AutoSelectIntervalMinutes = 0
	if _, err := service.Save(invalidIntervalLow); err == nil || !strings.Contains(err.Error(), "自动选路间隔") {
		t.Fatalf("expected a Chinese interval error, got %v", err)
	}

	invalidIntervalHigh := validSettings()
	invalidIntervalHigh.AutoSelectIntervalMinutes = 1441
	if _, err := service.Save(invalidIntervalHigh); err == nil || !strings.Contains(err.Error(), "自动选路间隔") {
		t.Fatalf("expected a Chinese interval error, got %v", err)
	}

	invalidToleranceLow := validSettings()
	invalidToleranceLow.AutoSelectToleranceMS = -1
	if _, err := service.Save(invalidToleranceLow); err == nil || !strings.Contains(err.Error(), "自动选路切换容差") {
		t.Fatalf("expected a Chinese tolerance error, got %v", err)
	}

	invalidToleranceHigh := validSettings()
	invalidToleranceHigh.AutoSelectToleranceMS = 5001
	if _, err := service.Save(invalidToleranceHigh); err == nil || !strings.Contains(err.Error(), "自动选路切换容差") {
		t.Fatalf("expected a Chinese tolerance error, got %v", err)
	}
}

func TestInboundAuthValidation(t *testing.T) {
	service := newService(t)

	valid := validSettings()
	valid.AllowLan = true
	valid.InboundAuthEnabled = true
	valid.InboundUsername = "  fastv  "
	valid.InboundPassword = "  secret123  "
	saved, err := service.Save(valid)
	if err != nil {
		t.Fatalf("expected valid inbound credentials to be accepted: %v", err)
	}
	if saved.InboundUsername != "fastv" || saved.InboundPassword != "secret123" {
		t.Fatalf("inbound credentials were not trimmed: %+v", saved)
	}
	if !saved.AllowLan || !saved.InboundAuthEnabled {
		t.Fatalf("inbound flags were not persisted: %+v", saved)
	}
	stored := service.Get()
	if stored.InboundUsername != "fastv" || stored.InboundPassword != "secret123" || !stored.InboundAuthEnabled {
		t.Fatalf("settings were not persisted: %+v", stored)
	}

	cases := []struct {
		name   string
		mutate func(*model.Settings)
		want   string
	}{
		{"empty username", func(settings *model.Settings) { settings.InboundUsername = "" }, "用户名不能为空"},
		{"blank username", func(settings *model.Settings) { settings.InboundUsername = "   " }, "用户名不能为空"},
		{"username with space", func(settings *model.Settings) { settings.InboundUsername = "fast v" }, "不能包含空白字符"},
		{"username with tab", func(settings *model.Settings) { settings.InboundUsername = "fast\tv" }, "不能包含空白字符"},
		{"empty password", func(settings *model.Settings) { settings.InboundPassword = "" }, "密码不能为空"},
		{"blank password", func(settings *model.Settings) { settings.InboundPassword = "   " }, "密码不能为空"},
		{"short password", func(settings *model.Settings) { settings.InboundPassword = "12345" }, "密码长度不能少于 6 位"},
		{"short Chinese password", func(settings *model.Settings) { settings.InboundPassword = "密码密码" }, "密码长度不能少于 6 位"},
	}
	for _, tc := range cases {
		input := validSettings()
		input.InboundAuthEnabled = true
		input.InboundUsername = "fastv"
		input.InboundPassword = "secret123"
		tc.mutate(&input)
		_, err := service.Save(input)
		if err == nil {
			t.Fatalf("%s: expected validation error", tc.name)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected error containing %q, got %v", tc.name, tc.want, err)
		}
	}

	// A rejected save must leave the previously stored settings untouched.
	if got := service.Get(); got.InboundUsername != "fastv" || got.InboundPassword != "secret123" {
		t.Fatalf("rejected settings changed the stored values: %+v", got)
	}

	// Six characters (including non-ASCII ones) are acceptable.
	accepted := validSettings()
	accepted.InboundAuthEnabled = true
	accepted.InboundUsername = "密码用户"
	accepted.InboundPassword = "密码密码密码"
	if _, err := service.Save(accepted); err != nil {
		t.Fatalf("expected a six-character password to be accepted: %v", err)
	}

	// When inbound authentication is disabled the credentials are not validated.
	disabled := validSettings()
	disabled.InboundAuthEnabled = false
	disabled.InboundUsername = ""
	disabled.InboundPassword = "123"
	if _, err := service.Save(disabled); err != nil {
		t.Fatalf("disabled inbound auth must not validate credentials: %v", err)
	}

	// AllowLan does not add any constraint of its own.
	lan := validSettings()
	lan.AllowLan = true
	if _, err := service.Save(lan); err != nil {
		t.Fatalf("AllowLan must not require credentials: %v", err)
	}
}
