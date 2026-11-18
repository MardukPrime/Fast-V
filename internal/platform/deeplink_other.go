//go:build !windows

package platform

import "errors"

// RegisterURLScheme is only implemented on Windows; other platforms register
// URL handlers through their own application bundle metadata.
func RegisterURLScheme(scheme string, exePath string) error {
	return errors.New("仅支持 Windows")
}
