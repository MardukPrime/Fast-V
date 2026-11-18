//go:build !windows

package platform

import "errors"

// EnableSystemProxy is a no-op outside Windows.
func EnableSystemProxy(address string) (func() error, error) {
	return func() error { return nil }, nil
}

// EnableSystemProxyPAC is unsupported outside Windows: the WinINet automatic
// proxy configuration is a Windows registry setting only.
func EnableSystemProxyPAC(pacFilePath string) (func() error, error) {
	return nil, errors.New("仅支持 Windows")
}
