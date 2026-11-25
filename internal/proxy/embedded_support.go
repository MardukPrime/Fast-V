package proxy

// embeddedInstance is implemented by the in-process Xray-core wrapper (see the
// embeddedcore/android build-tagged files).
type embeddedInstance interface {
	Close() error
}

// embeddedStart is non-nil when this build can start Xray-core in-process. The
// second argument receives the core's own log lines; pass nil to let the core
// keep logging to stdout.
var embeddedStart func(config []byte, sink func(level, message string)) (embeddedInstance, error)

// embeddedCoreEnabled reports whether this build should run the core in-process
// instead of executing the external binary.
func embeddedCoreEnabled() bool { return embeddedStart != nil }
