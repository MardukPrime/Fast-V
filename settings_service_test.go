package main

import (
	"path/filepath"
	"testing"
)

func TestSettingsServiceSaveAndReset(t *testing.T) {
	store, err := newStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewSettingsService(store)

	defaults := service.Get()
	if defaults.Theme != "dark" || defaults.UpdateIntervalMinutes != 360 || defaults.CoreType != "xray" || defaults.XrayLocalPort != 10808 {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}

	saved, err := service.Save(Settings{
		Theme:                 "light",
		Language:              "zh-CN",
		AutoUpdate:            true,
		UpdateIntervalMinutes: 60,
		ProxyMode:             "global",
		CoreType:              "xray",
		XrayLocalPort:         10808,
		XrayLogLevel:          "warning",
		XrayMuxEnabled:        true,
		XrayMuxConcurrency:    8,
		XraySniffing:          true,
	})
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if saved.Theme != "light" || !saved.AutoUpdate || saved.ProxyMode != "global" {
		t.Fatalf("unexpected saved settings: %+v", saved)
	}

	reset, err := service.Reset()
	if err != nil {
		t.Fatalf("reset failed: %v", err)
	}
	if reset.Theme != "dark" || reset.UpdateIntervalMinutes != 360 || reset.CoreType != "xray" {
		t.Fatalf("unexpected reset settings: %+v", reset)
	}
}

func TestSettingsServiceRejectsInvalidValues(t *testing.T) {
	store, err := newStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewSettingsService(store)
	_, err = service.Save(Settings{
		Theme:                 "neon",
		Language:              "zh-CN",
		UpdateIntervalMinutes: 60,
		ProxyMode:             "rule",
		CoreType:              "xray",
		XrayLocalPort:         10808,
		XrayLogLevel:          "warning",
		XrayMuxEnabled:        true,
		XrayMuxConcurrency:    8,
		XraySniffing:          true,
	})
	if err == nil {
		t.Fatal("expected invalid theme error")
	}
}

func TestSettingsServiceRejectsInvalidXrayParams(t *testing.T) {
	store, err := newStoreAt(filepath.Join(t.TempDir(), "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewSettingsService(store)

	base := Settings{
		Theme:                 "dark",
		Language:              "zh-CN",
		UpdateIntervalMinutes: 60,
		ProxyMode:             "rule",
		CoreType:              "xray",
		XrayLocalPort:         10808,
		XrayLogLevel:          "warning",
		XrayMuxConcurrency:    8,
	}

	invalidPort := base
	invalidPort.XrayLocalPort = 70000
	if _, err := service.Save(invalidPort); err == nil {
		t.Fatal("expected invalid Xray port error")
	}

	invalidLevel := base
	invalidLevel.XrayLogLevel = "verbose"
	if _, err := service.Save(invalidLevel); err == nil {
		t.Fatal("expected invalid Xray log level error")
	}
}
