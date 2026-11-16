package store

import "testing"

func TestMergeStoreDataMigratesLegacyDarkTheme(t *testing.T) {
	migrated := mergeStoreData(storeData{
		Version:  1,
		Settings: Settings{Theme: "dark"},
	})

	if migrated.Settings.Theme != "light" {
		t.Fatalf("expected legacy dark theme to migrate to light, got %q", migrated.Settings.Theme)
	}
	if migrated.Version != storeVersion {
		t.Fatalf("expected version %d after migration, got %d", storeVersion, migrated.Version)
	}
}

func TestMergeStoreDataKeepsExplicitTheme(t *testing.T) {
	kept := mergeStoreData(storeData{
		Version:  storeVersion,
		Settings: Settings{Theme: "dark"},
	})

	if kept.Settings.Theme != "dark" {
		t.Fatalf("expected an explicit theme to be preserved, got %q", kept.Settings.Theme)
	}
}

func TestDefaultSettingsUseLightTheme(t *testing.T) {
	if got := DefaultSettings().Theme; got != "light" {
		t.Fatalf("expected light default theme, got %q", got)
	}
}
