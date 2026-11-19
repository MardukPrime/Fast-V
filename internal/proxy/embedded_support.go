package proxy

// embeddedInstance is implemented by the in-process Xray-core wrapper (see the
// embeddedcore/android build-tagged files).
type embeddedInstance interface {
	Close() error
}

// embeddedStart is non-nil when this build can start Xray-core in-process.
var embeddedStart func(config []byte) (embeddedInstance, error)

// embeddedCoreEnabled reports whether this build should run the core in-process
// instead of executing the external binary.
func embeddedCoreEnabled() bool { return embeddedStart != nil }
