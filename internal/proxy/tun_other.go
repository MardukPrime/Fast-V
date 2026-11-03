//go:build !windows

package proxy

import "errors"

func preflightTun(corePath string) error {
	return errors.New("TUN mode is currently implemented on Windows only")
}

func setupTunRoutes(tunName string, proxyIP string) (func() error, error) {
	return nil, errors.New("TUN mode is currently implemented on Windows only")
}
