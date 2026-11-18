//go:build !windows

package platform

import "errors"

// SystemDNSServers is unsupported outside Windows: the resolver configuration
// is read from the Windows registry.
func SystemDNSServers() ([]string, error) {
	return nil, errors.New("仅支持 Windows")
}
