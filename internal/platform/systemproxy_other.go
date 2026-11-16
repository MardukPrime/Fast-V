//go:build !windows

package platform

// EnableSystemProxy is a no-op outside Windows.
func EnableSystemProxy(address string) (func() error, error) {
	return func() error { return nil }, nil
}
