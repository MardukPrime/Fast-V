package settings

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MardukPrime/Fast-V/internal/hotkey"
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
		AccentColor:               "sky",
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
	if defaults.Theme != "light" || defaults.UpdateIntervalMinutes != 360 || defaults.CoreType != "xray" || defaults.XrayLocalPort != 10808 || defaults.XrayAPIPort != 10085 || defaults.LayoutMode != "dock" || defaults.XrayDNSStrategy != "UseIP" || defaults.XrayDNSServers == "" || defaults.XrayTunMTU != 1500 || defaults.DockTransparency != 28 || defaults.SystemProxyMode != "system" {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}
	if defaults.AccentColor != "sky" || !defaults.HotkeysEnabled {
		t.Fatalf("unexpected accent color / hotkey defaults: %+v", defaults)
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

// TestSystemProxyModeValidation covers the supported system proxy modes:
// "system" (fixed WinINet proxy), "off" and "pac" (generated PAC script).
func TestSystemProxyModeValidation(t *testing.T) {
	service := newService(t)

	for _, mode := range []string{"system", "off", "pac"} {
		input := validSettings()
		input.SystemProxyMode = mode
		saved, err := service.Save(input)
		if err != nil {
			t.Fatalf("%q: expected the mode to be accepted: %v", mode, err)
		}
		if saved.SystemProxyMode != mode {
			t.Fatalf("%q: saved mode = %q", mode, saved.SystemProxyMode)
		}
	}

	// Surrounding whitespace is normalized away like the other trimmed fields.
	padded := validSettings()
	padded.SystemProxyMode = "  pac  "
	saved, err := service.Save(padded)
	if err != nil {
		t.Fatalf("expected a padded mode to be normalized: %v", err)
	}
	if saved.SystemProxyMode != "pac" {
		t.Fatalf("padded mode saved as %q, want pac", saved.SystemProxyMode)
	}

	for _, mode := range []string{"", "proxy", "PAC", "auto", "system,pac"} {
		input := validSettings()
		input.SystemProxyMode = mode
		if _, err := service.Save(input); err == nil || !strings.Contains(err.Error(), "系统代理模式") {
			t.Fatalf("%q: expected a Chinese system proxy mode error, got %v", mode, err)
		}
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

func TestLoadBalanceModeValidation(t *testing.T) {
	service := newService(t)

	for _, mode := range []string{"leastPing", "leastLoad", "roundRobin"} {
		input := validSettings()
		input.LoadBalanceEnabled = true
		input.LoadBalanceMode = "  " + mode + "  "
		saved, err := service.Save(input)
		if err != nil {
			t.Fatalf("%s: expected the load balancing strategy to be accepted: %v", mode, err)
		}
		if saved.LoadBalanceMode != mode || !saved.LoadBalanceEnabled {
			t.Fatalf("%s: unexpected saved load balancing settings: %+v", mode, saved)
		}
	}

	// An empty strategy keeps the default leastPing strategy.
	empty := validSettings()
	empty.LoadBalanceEnabled = true
	empty.LoadBalanceMode = ""
	saved, err := service.Save(empty)
	if err != nil {
		t.Fatalf("an empty strategy must fall back to leastPing: %v", err)
	}
	if saved.LoadBalanceMode != "leastPing" {
		t.Fatalf("unexpected default load balancing strategy: %q", saved.LoadBalanceMode)
	}

	// Unsupported strategies are rejected with a Chinese error.
	for _, mode := range []string{"leastping", "random", "least_ping", "LeastLoad"} {
		input := validSettings()
		input.LoadBalanceMode = mode
		if _, err := service.Save(input); err == nil || !strings.Contains(err.Error(), "负载均衡策略") {
			t.Fatalf("%q: expected a Chinese load balancing strategy error, got %v", mode, err)
		}
	}

	// LoadBalanceEnabled has no validation of its own: disabled load balancing
	// accepts the stored default strategy unchanged.
	disabled := validSettings()
	disabled.LoadBalanceEnabled = false
	disabled.LoadBalanceMode = "leastPing"
	if _, err := service.Save(disabled); err != nil {
		t.Fatalf("disabled load balancing must be accepted: %v", err)
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

func TestAccentColorValidation(t *testing.T) {
	service := newService(t)

	for _, accent := range []string{"sky", "violet", "emerald", "amber", "rose"} {
		input := validSettings()
		input.AccentColor = "  " + accent + "  "
		saved, err := service.Save(input)
		if err != nil {
			t.Fatalf("%s: expected the accent color to be accepted: %v", accent, err)
		}
		if saved.AccentColor != accent {
			t.Fatalf("%s: the accent color was not trimmed: %q", accent, saved.AccentColor)
		}
		if stored := service.Get(); stored.AccentColor != accent {
			t.Fatalf("%s: the accent color was not persisted: %q", accent, stored.AccentColor)
		}
	}

	// A rejected save must leave the previously stored accent color untouched.
	previous := service.Get().AccentColor
	for _, accent := range []string{"", "   ", "neon", "Sky", "#ff0000", "violet-dark"} {
		input := validSettings()
		input.AccentColor = accent
		if _, err := service.Save(input); err == nil {
			t.Fatalf("%q: expected an accent color error", accent)
		} else if !strings.Contains(err.Error(), "不支持的强调色") {
			t.Fatalf("%q: expected a Chinese accent color error, got %v", accent, err)
		}
	}
	if got := service.Get().AccentColor; got != previous {
		t.Fatalf("a rejected save changed the stored accent color: %q", got)
	}
}

// HotkeysEnabled has no validation of its own: toggling it must simply persist,
// and the default install starts with the hotkeys enabled.
func TestHotkeysEnabledValidation(t *testing.T) {
	service := newService(t)

	if !service.Get().HotkeysEnabled {
		t.Fatal("global hotkeys should be enabled by default")
	}

	disabled := validSettings()
	disabled.HotkeysEnabled = false
	saved, err := service.Save(disabled)
	if err != nil {
		t.Fatalf("disabling global hotkeys must be accepted: %v", err)
	}
	if saved.HotkeysEnabled || service.Get().HotkeysEnabled {
		t.Fatal("disabling global hotkeys was not persisted")
	}

	enabled := validSettings()
	enabled.HotkeysEnabled = true
	saved, err = service.Save(enabled)
	if err != nil {
		t.Fatalf("enabling global hotkeys must be accepted: %v", err)
	}
	if !saved.HotkeysEnabled || !service.Get().HotkeysEnabled {
		t.Fatal("enabling global hotkeys was not persisted")
	}
}

func TestHotkeyCombinationValidation(t *testing.T) {
	service := newService(t)

	// The shipped defaults are valid canonical combinations and describe the
	// same shortcuts as the package level defaults.
	defaults := service.Get()
	if defaults.HotkeyToggleProxy != "Ctrl+Shift+F" || defaults.HotkeyCycleMode != "Ctrl+Shift+M" || defaults.HotkeyNextNode != "Ctrl+Shift+N" {
		t.Fatalf("unexpected default hotkey combinations: %+v", defaults)
	}
	bindings, err := hotkey.BindingsFromSettings(defaults)
	if err != nil {
		t.Fatalf("default hotkey combinations must be valid: %v", err)
	}
	want := hotkey.DefaultBindings()
	if len(bindings) != len(want) {
		t.Fatalf("expected %d bindings from the defaults, got %d", len(want), len(bindings))
	}
	for index := range want {
		if bindings[index] != want[index] {
			t.Fatalf("binding %d: expected %+v, got %+v", index, want[index], bindings[index])
		}
	}

	// A save canonicalizes the spelling into the Ctrl+Alt+Shift+Win+Key order.
	custom := validSettings()
	custom.HotkeyToggleProxy = " ctrl + alt + f "
	custom.HotkeyCycleMode = "shift+control+m"
	custom.HotkeyNextNode = "Win+Shift+N"
	saved, err := service.Save(custom)
	if err != nil {
		t.Fatalf("valid custom combinations must be accepted: %v", err)
	}
	if saved.HotkeyToggleProxy != "Ctrl+Alt+F" || saved.HotkeyCycleMode != "Ctrl+Shift+M" || saved.HotkeyNextNode != "Shift+Win+N" {
		t.Fatalf("custom combinations were not canonicalized: %+v", saved)
	}
	if stored := service.Get(); stored.HotkeyToggleProxy != "Ctrl+Alt+F" || stored.HotkeyCycleMode != "Ctrl+Shift+M" || stored.HotkeyNextNode != "Shift+Win+N" {
		t.Fatalf("custom combinations were not persisted: %+v", stored)
	}

	// Duplicate combinations are rejected with a Chinese error and leave the
	// stored settings untouched.
	duplicated := validSettings()
	duplicated.HotkeyToggleProxy = "Ctrl+Shift+F"
	duplicated.HotkeyCycleMode = "control + shift + f"
	if _, err := service.Save(duplicated); err == nil || !strings.Contains(err.Error(), "快捷键不能重复") {
		t.Fatalf("expected a duplicate combination error, got %v", err)
	}
	if stored := service.Get(); stored.HotkeyToggleProxy != "Ctrl+Alt+F" {
		t.Fatalf("a rejected save changed the stored hotkeys: %+v", stored)
	}

	// Invalid combinations are rejected with a Chinese error.
	invalidCases := []struct {
		name   string
		mutate func(*model.Settings)
		want   string
	}{
		{"unknown key", func(settings *model.Settings) { settings.HotkeyNextNode = "Ctrl+Shift+Nope" }, "不支持的按键"},
		{"missing main key", func(settings *model.Settings) { settings.HotkeyCycleMode = "Ctrl+Shift" }, "缺少主键"},
		{"missing modifier", func(settings *model.Settings) { settings.HotkeyToggleProxy = "F5" }, "缺少修饰键"},
		{"duplicated modifier", func(settings *model.Settings) { settings.HotkeyNextNode = "Ctrl+Ctrl+N" }, "修饰键重复"},
	}
	for _, tc := range invalidCases {
		input := validSettings()
		input.HotkeyToggleProxy = "Ctrl+Alt+F"
		input.HotkeyCycleMode = "Ctrl+Shift+M"
		input.HotkeyNextNode = "Shift+Win+N"
		tc.mutate(&input)
		if _, err := service.Save(input); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: expected an error containing %q, got %v", tc.name, tc.want, err)
		}
	}

	// Every shortcut may be disabled by saving an empty combination.
	disabledCombos := validSettings()
	disabledCombos.HotkeyToggleProxy = "   "
	disabledCombos.HotkeyCycleMode = ""
	disabledCombos.HotkeyNextNode = ""
	saved, err = service.Save(disabledCombos)
	if err != nil {
		t.Fatalf("empty combinations must be accepted as disabled shortcuts: %v", err)
	}
	if saved.HotkeyToggleProxy != "" || saved.HotkeyCycleMode != "" || saved.HotkeyNextNode != "" {
		t.Fatalf("empty combinations were not persisted: %+v", saved)
	}
	if stored := service.Get(); stored.HotkeyToggleProxy != "" || stored.HotkeyCycleMode != "" || stored.HotkeyNextNode != "" {
		t.Fatalf("empty combinations were not stored: %+v", stored)
	}
	bindings, err = hotkey.BindingsFromSettings(service.Get())
	if err != nil {
		t.Fatalf("disabled shortcuts must not fail the bindings conversion: %v", err)
	}
	if len(bindings) != 0 {
		t.Fatalf("expected no bindings for disabled shortcuts, got %+v", bindings)
	}
}

// TestTunFallbackDisablesTunAndEnablesSystemProxy covers the TUN fallback: it
// must switch TUN off, force the fixed system proxy and leave every other
// setting untouched, no matter which proxy mode was active before.
func TestTunFallbackDisablesTunAndEnablesSystemProxy(t *testing.T) {
	service := newService(t)

	for _, originalMode := range []string{"off", "pac", "system"} {
		input := validSettings()
		input.TunMode = true
		input.SystemProxyMode = originalMode
		input.AllowLan = true
		input.AutoUpdate = true
		before, err := service.Save(input)
		if err != nil {
			t.Fatalf("%s: save TUN settings: %v", originalMode, err)
		}

		after, err := service.TunFallback()
		if err != nil {
			t.Fatalf("%s: TunFallback: %v", originalMode, err)
		}
		if after.TunMode {
			t.Fatalf("%s: TunFallback left TUN mode enabled", originalMode)
		}
		if after.SystemProxyMode != "system" {
			t.Fatalf("%s: SystemProxyMode = %q, want system", originalMode, after.SystemProxyMode)
		}

		want := before
		want.TunMode = false
		want.SystemProxyMode = "system"
		if after != want {
			t.Fatalf("%s: TunFallback changed unrelated fields:\n got=%+v\nwant=%+v", originalMode, after, want)
		}
		if stored := service.Get(); stored != after {
			t.Fatalf("%s: fallback settings were not persisted: got=%+v want=%+v", originalMode, stored, after)
		}
	}
}

// TestTunFallbackIsIdempotent makes sure the fallback is safe to repeat once TUN
// is already off.
func TestTunFallbackIsIdempotent(t *testing.T) {
	service := newService(t)

	input := validSettings()
	input.TunMode = false
	input.SystemProxyMode = "system"
	before, err := service.Save(input)
	if err != nil {
		t.Fatalf("save settings: %v", err)
	}
	after, err := service.TunFallback()
	if err != nil {
		t.Fatalf("TunFallback: %v", err)
	}
	if after != before {
		t.Fatalf("repeating the fallback changed settings:\n got=%+v\nwant=%+v", after, before)
	}
}

// TestTunFallbackUsesSaveValidation proves the fallback runs through the same
// validation path as Save: invalid stored settings are rejected and nothing is
// persisted.
func TestTunFallbackUsesSaveValidation(t *testing.T) {
	service := newService(t)

	invalid := validSettings()
	invalid.TunMode = true
	invalid.SystemProxyMode = "off"
	invalid.Theme = "neon" // rejected by Save
	if err := service.store.SaveSettings(invalid); err != nil {
		t.Fatalf("seed invalid settings: %v", err)
	}
	if _, err := service.TunFallback(); err == nil || !strings.Contains(err.Error(), "invalid theme") {
		t.Fatalf("expected the Save validation error, got %v", err)
	}
	stored := service.Get()
	if !stored.TunMode || stored.SystemProxyMode != "off" || stored.Theme != "neon" {
		t.Fatalf("a rejected fallback must not persist changes: %+v", stored)
	}
}
