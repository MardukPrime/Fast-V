// Package vpn implements the Android packet dataplane for FastV's system-wide
// proxy mode (Phase 2, mechanism M3).
//
// An Android VpnService hands us a tun file descriptor whose traffic has already
// been captured by the operating system. This package terminates that traffic in
// userspace: a gVisor netstack answers the TCP/UDP flows, and every accepted
// flow is relayed through the embedded core's local SOCKS5 inbound, so packets
// leaving the tun end up on the proxy the user selected.
//
// The package is deliberately free of Android (or cgo) imports: the device is
// any io.ReadWriter carrying raw IP packets, which keeps the dataplane testable
// on a desktop host and lets the same code serve both ABIs.
package vpn
