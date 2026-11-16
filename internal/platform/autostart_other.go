//go:build !windows

package platform

import "errors"

var errAutostartUnsupported = errors.New("仅支持 Windows")

// SetAutostart always returns an unsupported-platform error outside Windows.
// Windows is the only supported autostart target because FastV is a
// Windows-first client.
func SetAutostart(enabled bool) error {
	return errAutostartUnsupported
}

// AutostartEnabled always reports an error outside Windows.
func AutostartEnabled() (bool, error) {
	return false, errAutostartUnsupported
}

// IsEnabled reports whether FastV starts with the operating system. It is an
// alias of AutostartEnabled kept for callers that prefer the shorter name.
func IsEnabled() (bool, error) {
	return AutostartEnabled()
}
