//go:build !windows

package proxy

import "errors"

// preflightSingBoxTun reports that the sing-box TUN mode is Windows-only in
// FastV; the desktop TUN management code (routes, elevation) exists only for
// Windows.
func preflightSingBoxTun() error {
	return errors.New("sing-box TUN 模式当前仅支持 Windows")
}

// cleanupSingBoxTun is a no-op outside Windows, where the sing-box TUN mode is
// not supported by FastV.
func cleanupSingBoxTun() error { return nil }
