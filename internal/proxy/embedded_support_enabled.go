//go:build embeddedcore || android

package proxy

import (
	"github.com/MardukPrime/Fast-V/internal/proxy/embedded"
)

func init() {
	embeddedStart = func(config []byte) (embeddedInstance, error) {
		return embedded.Start(config)
	}
}
