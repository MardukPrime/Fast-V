//go:build embeddedcore || android

package coremode

// Enabled reports whether the embedded (in-process) Xray-core is used. Android
// has no way to exec a downloaded core binary from app data, so the core is
// linked into the app. The `embeddedcore` tag enables the same path on desktop
// for development and verification.
func Enabled() bool { return true }

// Version is the embedded core version reported to the UI.
func Version() string { return "embedded-xray" }
