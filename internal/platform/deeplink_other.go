//go:build !windows

package platform

import "errors"

// errURLSchemeUnsupported marks the URL scheme registry operations that only
// exist on Windows: other platforms register URL handlers through their own
// application bundle metadata instead of the registry.
var errURLSchemeUnsupported = errors.New("仅支持 Windows")

// RegisterURLScheme is only implemented on Windows; other platforms register
// URL handlers through their own application bundle metadata.
func RegisterURLScheme(scheme string, exePath string) error {
	return errors.New("仅支持 Windows")
}

// URLSchemeRegistered is only implemented on Windows.
func URLSchemeRegistered(scheme string) (bool, error) {
	return false, errURLSchemeUnsupported
}

// RemoveURLScheme is only implemented on Windows.
func RemoveURLScheme(scheme string) error {
	return errURLSchemeUnsupported
}
