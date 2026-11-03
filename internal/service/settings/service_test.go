package settings

import (
	"path/filepath"
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
		Theme:                 "dark",
		Language:              "zh-CN",
		UpdateIntervalMinutes: 60,
		ProxyMode:             "rule",
		CoreType:              "xray",
		XrayLocalPort:         10808,
		XrayLogLevel:          "warning",
		XrayMuxConcurrency:    8,
		TestURL:               "https://www.gstatic.com/generate_204",
		XrayAPIEnabled:        true,
		XrayAPIPort:           10085,
	}
}

func TestSaveAndReset(t *testing.T) {
	service := newService(t)

	defaults := service.Get()
	if defaults.Theme != "dark" || defaults.UpdateIntervalMinutes != 360 || defaults.CoreType != "xray" || defaults.XrayLocalPort != 10808 || defaults.XrayAPIPort != 10085 {
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
	if reset.Theme != "dark" || reset.UpdateIntervalMinutes != 360 || reset.CoreType != "xray" || reset.TunMode {
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
	if _, err := service.Save(invalidLevel); err == nil {
		t.Fatal("expected invalid Xray log level error")
	}
}
