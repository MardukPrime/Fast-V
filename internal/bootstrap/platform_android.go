//go:build android

package bootstrap

import (
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// prepareMobileDataDir points the store at the Android app's private files
// directory before any store is opened. os.UserConfigDir/UserCacheDir are not
// usable on Android, so the path has to come from the Java bridge.
func prepareMobileDataDir() {
	if os.Getenv("FASTV_DATA_DIR") != "" {
		return
	}
	base := application.Android.StoragePath()
	if base == "" {
		return
	}
	dir := filepath.Join(base, "FastV")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	_ = os.Setenv("FASTV_DATA_DIR", dir)
}

// desktopIntegrationSupported reports whether tray icons, global hotkeys,
// floating windows and protocol handlers can be registered. Android has none of
// these: the app is fullscreen and only the first window is displayed.
func desktopIntegrationSupported() bool {
	return false
}
