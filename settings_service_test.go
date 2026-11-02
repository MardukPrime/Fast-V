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
	if defaults.Theme != "dark" || defaults.UpdateIntervalMinutes != 360 {
		t.Fatalf("unexpected defaults: %+v", defaults)
	}

	saved, err := service.Save(Settings{
		Theme:                 "light",
		Language:              "zh-CN",
		AutoUpdate:            true,
		UpdateIntervalMinutes: 60,
		ProxyMode:             "global",
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
	if reset.Theme != "dark" || reset.UpdateIntervalMinutes != 360 {
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
	})
	if err == nil {
		t.Fatal("expected invalid theme error")
	}
}
