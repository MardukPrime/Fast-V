//go:build !android

package bootstrap

func prepareMobileDataDir() {}

func desktopIntegrationSupported() bool {
	return true
}
