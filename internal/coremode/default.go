//go:build !embeddedcore && !android

// Package coremode reports whether Xray-core runs inside the app process.
package coremode

// Enabled reports whether the embedded (in-process) Xray-core is used. Desktop
// builds keep the external core binary, so this is false by default.
func Enabled() bool { return false }

// Version is the embedded core version reported to the UI. Empty on desktop.
func Version() string { return "" }
