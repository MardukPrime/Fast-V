// Package embedded runs Xray-core in-process for platforms where spawning an
// external core binary is not possible (Android, or the `embeddedcore` developer
// tag used to verify the integration on desktop).
//
// The implementation lives behind a build tag so the desktop build keeps using
// the external core binary and does not link the (large) Xray-core library.
package embedded
