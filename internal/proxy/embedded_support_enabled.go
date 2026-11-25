//go:build embeddedcore || android

package proxy

import (
	"github.com/MardukPrime/Fast-V/internal/proxy/embedded"
)

func init() {
	embeddedStart = func(config []byte, sink func(level, message string)) (embeddedInstance, error) {
		if sink == nil {
			return embedded.Start(config)
		}
		return embedded.StartWithSink(config, embedded.Sink(sink))
	}
}
